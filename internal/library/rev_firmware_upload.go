package library

import (
	"errors"
	"fmt"
	"log"
	"net/http"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	sdk "github.com/networkextension/polar-sdk"
)

// Firmware blob upload (P-library-0b complement to the metadata-only
// POST /api/library/firmwares).
//
// Storage layout (content-addressed under uploadDir):
//   <uploadDir>/firmwares/<sha256[:2]>/<sha256>
// Reuses the existing uploadDir (where chat attachments live) — one
// less env var to plumb. The sha256 doubles as both filename and
// dedup key, so repeated uploads of the same blob are O(1).
//
// Endpoints (wired in app.go):
//   POST /api/library/firmwares/upload   — multipart form, admin-only
//   GET  /api/library/firmwares/:id/download — streams the blob,
//                                              member-readable

func (p *Plugin) firmwareBlobDir() string {
	return filepath.Join(p.BlobDir, "firmwares")
}

// handleRevFirmwareUpload — multipart upload. Form fields:
//   file              (required) — the firmware binary blob
//   kind              (required) — e.g. "iboot" / "wifi" / "sep"
//   version           (required) — e.g. "iBoot-7459.40.10"
//   vendor            (optional)
//   chip_id_compat    (optional, comma-separated decimal ints, e.g. "33025,33027")
//   board_id_compat   (optional, comma-separated decimal ints)
//   format            (optional) — "img4" / "raw" / "elf" / etc
//   extracted_from    (optional) — source IPSW / OTA / artifact label
func (p *Plugin) handleRevFirmwareUpload(c *gin.Context) {
	userID, _ := c.Get("user_id")
	userIDStr, _ := userID.(string)

	header, err := c.FormFile("file")
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is required (multipart field 'file')"})
		return
	}
	kind := strings.TrimSpace(c.PostForm("kind"))
	version := strings.TrimSpace(c.PostForm("version"))
	if kind == "" || version == "" {
		c.JSON(http.StatusBadRequest, gin.H{"error": "kind and version are required"})
		return
	}

	src, err := header.Open()
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "open uploaded file: " + err.Error()})
		return
	}
	defer src.Close()

	// Single-write: stream straight into the central assets catalog. No
	// library-svc-local copy. The catalog Name is unique per upload; the
	// bytes are content-addressed (deduped) by sha256 inside assets.
	meta, err := p.Dock.AssetUpload(sdk.AssetUploadInput{
		Kind:       "package",
		Name:       "firmwares/" + sanitizeFilename(kind) + "/" + sanitizeFilename(version) + "-" + firmwareRandHex(4),
		Version:    "v1",
		Visibility: "private",
		Mime:       "application/octet-stream",
		Metadata:   map[string]any{"firmware_kind": kind, "firmware_version": version},
	}, src)
	if err != nil {
		c.JSON(http.StatusBadGateway, gin.H{"error": "assets upload failed: " + err.Error()})
		return
	}
	if meta.SizeBytes == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "file is empty"})
		return
	}
	sum := meta.SHA256

	f := &RevFirmware{
		Kind:          kind,
		Vendor:        strings.TrimSpace(c.PostForm("vendor")),
		Version:       version,
		ChipIDCompat:  parseIntCSV(c.PostForm("chip_id_compat")),
		BoardIDCompat: parseIntCSV(c.PostForm("board_id_compat")),
		BlobURI:       "asset://" + strconv.FormatInt(meta.ID, 10),
		BlobSHA256:    sum,
		SizeBytes:     meta.SizeBytes,
		Format:        strings.TrimSpace(c.PostForm("format")),
		ExtractedFrom: strings.TrimSpace(c.PostForm("extracted_from")),
		AddedBy:       userIDStr,
	}

	out, err := p.insertRevFirmware(f, time.Now().UTC())
	if err != nil {
		if strings.Contains(err.Error(), "ux_rev_firmwares_sha") || strings.Contains(err.Error(), "duplicate key") {
			c.JSON(http.StatusConflict, gin.H{
				"error":       "firmware with this sha256 already exists in the library",
				"blob_sha256": sum,
			})
			return
		}
		c.JSON(http.StatusInternalServerError, gin.H{"error": "db: " + err.Error()})
		return
	}
	if err := p.setFirmwareAssetID(out.ID, meta.ID); err != nil {
		log.Printf("library: firmware %s/%s set asset_id=%d: %v", kind, version, meta.ID, err)
	}
	c.JSON(http.StatusOK, gin.H{"firmware": out})
}

// handleRevFirmwareDownload streams the blob back to the caller.
// Member-readable. Cache-friendly: content-addressed URL means
// firmware contents never change for a given (id, sha) pair.
func (p *Plugin) handleRevFirmwareDownload(c *gin.Context) {
	id, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || id <= 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "无效的 id"})
		return
	}
	f, err := p.getRevFirmware(id)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "服务器错误"})
		return
	}
	if f == nil {
		c.JSON(http.StatusNotFound, gin.H{"error": "not found"})
		return
	}
	// Dual-read: prefer the central assets catalog; fall back to the
	// local blob for rows not yet migrated (or if assets is down).
	if p.streamFirmwareFromAssets(c, f) {
		return
	}
	abs, err := p.resolveFirmwareBlobPath(f)
	if err != nil {
		c.JSON(http.StatusGone, gin.H{"error": err.Error()})
		return
	}
	c.Header("Cache-Control", "public, max-age=31536000, immutable")
	c.Header("Content-Disposition", fmt.Sprintf(`attachment; filename="%s_%s.bin"`,
		sanitizeFilename(f.Kind), sanitizeFilename(f.Version)))
	c.File(abs)
}

// resolveFirmwareBlobPath inspects the row's blob_uri and returns the
// absolute on-disk path IF it's a local file:// URI under the
// configured uploadDir. Refuses to follow arbitrary file:// paths —
// the URI must point inside uploadDir/firmwares/ to defend against
// a hand-crafted DB row pointing at /etc/passwd.
func (p *Plugin) resolveFirmwareBlobPath(f *RevFirmware) (string, error) {
	if !strings.HasPrefix(f.BlobURI, "file://") {
		return "", errors.New("blob is not locally stored (remote URI)")
	}
	abs := strings.TrimPrefix(f.BlobURI, "file://")
	// Canonicalize + confine. abs must live under firmwareBlobDir().
	canon, err := filepath.EvalSymlinks(abs)
	if err != nil {
		// File missing → treat as gone.
		return "", errors.New("firmware blob file missing on disk")
	}
	if !strings.HasPrefix(canon, p.firmwareBlobDir()+string(filepath.Separator)) {
		return "", errors.New("blob path outside the firmware storage dir")
	}
	return canon, nil
}

func parseIntCSV(s string) []int {
	s = strings.TrimSpace(s)
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]int, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p == "" {
			continue
		}
		if n, err := parseFlexibleInt64(p); err == nil {
			out = append(out, int(n))
		}
	}
	return out
}

// sanitizeFilename is provided by handler_helpers.go; reuse it for the
// content-disposition header.
