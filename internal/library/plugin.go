// Package library is the reverse-engineering knowledge base plugin
// (rev_devices, rev_firmwares, rev_functions). Used by polar-agent's
// MCP library adapter to look up known firmware functions during RE
// sessions.
//
// Phase 2-W2 skeleton: DB pool + heartbeat + /healthz only.
// Handler PR moves the 4 dock files (rev_handlers.go,
// rev_firmware_upload.go, rev_agent_handlers.go, rev_store.go) across.
//
// Library data is workspace-agnostic — global knowledge shared by
// all callers. Cross-domain user lookups (for "added_by" display)
// go through dock SDK like the other plugins, but no per-row FK.
package library

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log"
	"net/http"
	"runtime"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"

	"github.com/networkextension/polar-sdk"
)

type Plugin struct {
	DB         *sql.DB
	Dock       *sdk.Client
	Name       string
	Listen     string
	Ver        string
	BlobDir    string // $POLAR_LIBRARY_BLOB_DIR — firmware blob root
	MetricsTok string

	metrics   *libraryMetrics
	startedAt time.Time
}

type Config struct {
	DBDSN        string
	DockBase     string
	PluginName   string
	PluginToken  string
	Listen       string
	BuildVersion string
	BlobDir      string
	MetricsToken string
}

func New(ctx context.Context, cfg Config) (*Plugin, error) {
	cfg.PluginName = strings.TrimSpace(cfg.PluginName)
	if cfg.PluginName == "" {
		cfg.PluginName = "library"
	}
	if strings.TrimSpace(cfg.DBDSN) == "" {
		return nil, errors.New("library.New: DBDSN required")
	}
	if strings.TrimSpace(cfg.DockBase) == "" {
		return nil, errors.New("library.New: DockBase required")
	}
	if strings.TrimSpace(cfg.PluginToken) == "" {
		return nil, errors.New("library.New: PluginToken required")
	}
	if strings.TrimSpace(cfg.BlobDir) == "" {
		return nil, errors.New("library.New: BlobDir required")
	}

	db, err := sql.Open("postgres", cfg.DBDSN)
	if err != nil {
		return nil, fmt.Errorf("open polar_library: %w", err)
	}
	db.SetMaxOpenConns(10)
	db.SetMaxIdleConns(2)
	db.SetConnMaxLifetime(30 * time.Minute)
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("ping polar_library: %w", err)
	}

	dock := sdk.NewClient(cfg.DockBase, cfg.PluginName, sdk.DeriveHMACKey(cfg.PluginToken))
	resp, err := dock.Do(http.MethodGet, "/internal/v1/ping", nil)
	if err != nil {
		_ = db.Close()
		return nil, fmt.Errorf("dock ping: %w", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		_ = db.Close()
		return nil, fmt.Errorf("dock /ping rejected: HTTP %d", resp.StatusCode)
	}

	p := &Plugin{
		DB:         db,
		Dock:       dock,
		Name:       cfg.PluginName,
		Listen:     cfg.Listen,
		Ver:        cfg.BuildVersion,
		BlobDir:    cfg.BlobDir,
		MetricsTok: cfg.MetricsToken,
		metrics:    newLibraryMetrics(),
		startedAt:  time.Now(),
	}
	// Assets migration: ensure rev_firmwares.asset_id exists. Non-fatal.
	if err := p.ensureFirmwareAssetColumn(); err != nil {
		log.Printf("library: ensure asset_id column: %v", err)
	}
	return p, nil
}

func (p *Plugin) RegisterRoutes(r gin.IRouter) {
	r.GET("/healthz", p.handleHealthz)
	r.GET("/metrics", p.handleMetricsExposition)

	api := r.Group("/api")
	{
		authed := api.Group("", p.requireAuthViaDock())
		{
			authed.GET("/library/devices", p.handleRevDeviceList)
			authed.GET("/library/devices/recent", p.handleRevDeviceRecent)
			authed.GET("/library/devices/:id", p.handleRevDeviceGet)
			authed.GET("/library/firmwares", p.handleRevFirmwareList)
			authed.GET("/library/firmwares/matching", p.handleRevFirmwareMatching)
			authed.GET("/library/firmwares/:id", p.handleRevFirmwareGet)
			authed.GET("/library/firmwares/:id/download", p.handleRevFirmwareDownload)
			authed.GET("/library/functions", p.handleRevFunctionList)
			authed.GET("/library/functions/lookup-by-address", p.handleRevFunctionLookupByAddress)
			authed.GET("/library/functions/lookup-by-symbol", p.handleRevFunctionLookupBySymbol)
			authed.GET("/library/functions/search", p.handleRevFunctionSearch)
			authed.GET("/library/functions/match-signature", p.handleRevFunctionMatchSignature)
			authed.GET("/library/functions/:id", p.handleRevFunctionGet)
		}
		admin := api.Group("", p.requireAdminViaDock())
		{
			admin.POST("/library/devices", p.handleRevDeviceUpsert)
			admin.DELETE("/library/devices/:id", p.handleRevDeviceDelete)
			admin.POST("/library/firmwares", p.handleRevFirmwareCreate)
			admin.POST("/library/firmwares/upload", p.handleRevFirmwareUpload)
			admin.DELETE("/library/firmwares/:id", p.handleRevFirmwareDelete)
			admin.POST("/library/functions", p.handleRevFunctionCreate)
			admin.DELETE("/library/functions/:id", p.handleRevFunctionDelete)
		}
	}
}

func (p *Plugin) Start(ctx context.Context) {
	go p.heartbeatLoop(ctx)
	go p.backfillFirmwareAssetsOnce() // self-migrate any local-only firmware blobs to assets
}

func (p *Plugin) Close() error {
	if p.DB != nil {
		return p.DB.Close()
	}
	return nil
}

func (p *Plugin) handleHealthz(c *gin.Context) {
	dbOK := true
	if err := p.DB.PingContext(c.Request.Context()); err != nil {
		dbOK = false
	}
	status := http.StatusOK
	if !dbOK {
		status = http.StatusServiceUnavailable
	}
	c.JSON(status, gin.H{
		"plugin":         p.Name,
		"version":        p.Ver,
		"uptime_seconds": int64(time.Since(p.startedAt).Seconds()),
		"db_ok":          dbOK,
		"blob_dir":       p.BlobDir,
		"go":             runtime.Version(),
	})
}

func (p *Plugin) handleMetricsExposition(c *gin.Context) {
	if p.MetricsTok == "" {
		c.AbortWithStatus(http.StatusNotFound)
		return
	}
	if c.GetHeader("Authorization") != "Bearer "+p.MetricsTok {
		c.Header("WWW-Authenticate", `Bearer realm="metrics"`)
		c.AbortWithStatus(http.StatusUnauthorized)
		return
	}
	promhttp.HandlerFor(p.metrics.registry, promhttp.HandlerOpts{}).ServeHTTP(c.Writer, c.Request)
}

func (p *Plugin) heartbeatLoop(ctx context.Context) {
	p.beat(ctx)
	t := time.NewTicker(60 * time.Second)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p.beat(ctx)
		}
	}
}

// libraryUIRoutes — sidebar entries this plugin contributes. Heartbeated
// up to dock; aggregated into /api/plugin-ui-routes for polar-dock-ui's
// dynamic sidebar. See task #196.
var libraryUIRoutes = []sdk.UIRoute{
	{Path: "/library.html", Label: "Library", Icon: "library", Order: 60},
}

func (p *Plugin) beat(_ context.Context) {
	err := p.Dock.Heartbeat(sdk.HeartbeatOpts{
		Version:       p.Ver,
		Endpoint:      p.Listen,
		UptimeSeconds: int64(time.Since(p.startedAt).Seconds()),
		UIRoutes:      libraryUIRoutes,
	})
	if err != nil {
		log.Printf("library: heartbeat failed: %v", err)
	}
}

type libraryMetrics struct {
	registry *prometheus.Registry
	upGauge  prometheus.Gauge
}

func newLibraryMetrics() *libraryMetrics {
	m := &libraryMetrics{registry: prometheus.NewRegistry()}
	m.upGauge = prometheus.NewGauge(prometheus.GaugeOpts{
		Name: "polar_library_up",
		Help: "Always 1 while library-svc is serving. Phase 2-W2 placeholder.",
	})
	m.registry.MustRegister(m.upGauge)
	m.upGauge.Set(1)
	return m
}
