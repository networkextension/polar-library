package library

// Assets migration (doc/arch/blob-storage-to-assets-migration.md in
// polar-dock): firmware blobs move from library-svc-local disk to the
// central polar-assets catalog (single-write). Platform-owned
// (WorkspaceID=nil), private (download is member-gated by the svc; the
// svc itself fetches via the internal HMAC client). This file holds the
// asset_id column + dual-read + boot-backfill glue.

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"log"
	"net/http"
	"os"

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

// uploadFirmwareToAssets registers a local firmware blob in the assets
// catalog (content-addressed by sha). Used by the backfill.
func (p *Plugin) uploadFirmwareToAssets(f *RevFirmware, localAbs string) (int64, error) {
	fh, err := os.Open(localAbs)
	if err != nil {
		return 0, err
	}
	defer fh.Close()
	meta, err := p.Dock.AssetUpload(sdk.AssetUploadInput{
		Kind:       "package",
		Name:       "firmwares/" + f.BlobSHA256,
		Version:    "v1",
		Visibility: "private",
		Mime:       "application/octet-stream",
		Metadata:   map[string]any{"firmware_kind": f.Kind, "firmware_version": f.Version},
	}, fh)
	if err != nil {
		return 0, err
	}
	return meta.ID, nil
}

// streamFirmwareFromAssets serves the firmware from the assets catalog.
// Returns false (caller falls back to local) when no asset_id, or the
// fetch fails.
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

// backfillFirmwareAssetsOnce migrates any local-only firmware blobs into
// the assets catalog on startup. Idempotent goroutine from Start().
func (p *Plugin) backfillFirmwareAssetsOnce() {
	rows, err := p.DB.Query(`SELECT id, kind, version, blob_uri, blob_sha256 FROM rev_firmwares WHERE asset_id IS NULL`)
	if err != nil {
		log.Printf("library: firmware backfill: query: %v", err)
		return
	}
	var pending []RevFirmware
	for rows.Next() {
		var f RevFirmware
		if err := rows.Scan(&f.ID, &f.Kind, &f.Version, &f.BlobURI, &f.BlobSHA256); err != nil {
			log.Printf("library: firmware backfill: scan: %v", err)
			continue
		}
		pending = append(pending, f)
	}
	rows.Close()

	migrated := 0
	for i := range pending {
		f := &pending[i]
		abs, err := p.resolveFirmwareBlobPath(f)
		if err != nil {
			log.Printf("library: firmware backfill: %s/%s: %v (skip)", f.Kind, f.Version, err)
			continue
		}
		assetID, err := p.uploadFirmwareToAssets(f, abs)
		if err != nil {
			log.Printf("library: firmware backfill: %s/%s: upload: %v", f.Kind, f.Version, err)
			continue
		}
		if err := p.setFirmwareAssetID(f.ID, assetID); err != nil {
			log.Printf("library: firmware backfill: %s/%s: set asset_id: %v", f.Kind, f.Version, err)
			continue
		}
		migrated++
		log.Printf("library: firmware backfill: migrated %s/%s -> asset %d", f.Kind, f.Version, assetID)
	}
	if migrated > 0 {
		log.Printf("library: firmware backfill: migrated %d firmware(s) to assets", migrated)
	}
}
