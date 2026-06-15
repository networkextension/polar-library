package library

// Assets glue (doc/arch/blob-storage-to-assets-migration.md in
// polar-dock): firmware blobs live exclusively in the central
// polar-assets catalog (single-write). Platform-owned (WorkspaceID=nil),
// private (download is member-gated by the svc; the svc itself fetches
// via the internal HMAC client). This file holds the asset_id column +
// the assets read path (the transitional backfill was removed at cutover).

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"
	sdk "github.com/networkextension/polar-sdk"
)

func firmwareRandHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ensureFirmwareAssetColumn adds rev_firmwares.asset_id if missing.
// Idempotent; called from New().
func (p *Plugin) ensureFirmwareAssetColumn() error {
	_, err := p.DB.Exec(`ALTER TABLE rev_firmwares ADD COLUMN IF NOT EXISTS asset_id BIGINT`)
	return err
}

func (p *Plugin) setFirmwareAssetID(id, assetID int64) error {
	_, err := p.DB.Exec(`UPDATE rev_firmwares SET asset_id = $2 WHERE id = $1`, id, assetID)
	return err
}

func (p *Plugin) getFirmwareAssetID(id int64) (int64, bool, error) {
	var a sql.NullInt64
	if err := p.DB.QueryRow(`SELECT asset_id FROM rev_firmwares WHERE id = $1`, id).Scan(&a); err != nil {
		return 0, false, err
	}
	if !a.Valid {
		return 0, false, nil
	}
	return a.Int64, true, nil
}

// streamFirmwareFromAssets serves the firmware from the assets catalog.
// Returns false (no body written) when there's no asset_id or the fetch
// fails, so the caller can emit an error.
func (p *Plugin) streamFirmwareFromAssets(c *gin.Context, f *RevFirmware) bool {
	assetID, ok, err := p.getFirmwareAssetID(f.ID)
	if err != nil || !ok {
		return false
	}
	resp, err := p.Dock.AssetDownload(&sdk.AssetMeta{ID: assetID})
	if err != nil || resp == nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		return false
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s_%s.bin"`,
		sanitizeFilename(f.Kind), sanitizeFilename(f.Version)))
	c.DataFromReader(http.StatusOK, resp.ContentLength, "application/octet-stream", resp.Body, nil)
	return true
}
