package library

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/lib/pq"
)

// Knowledge-base store layer (P-library-0a). See
// doc/jtag-skill-design.md §3.5.
//
// All three tables are GLOBAL (not workspace-scoped) — the design's
// "network effect across operators" is the point. Writes are gated
// at the HTTP layer by AdminMiddleware; reads are open to any
// authenticated user.

// ---------- rev_devices ----------

type RevDevice struct {
	ID              int64           `json:"id"`
	ECID            *int64          `json:"ecid,omitempty"`
	CPID            int             `json:"cpid"`
	BDID            *int            `json:"bdid,omitempty"`
	CPRV            *int            `json:"cprv,omitempty"`
	ChipName        string          `json:"chip_name,omitempty"`
	BoardName       string          `json:"board_name,omitempty"`
	OSRunning       string          `json:"os_running,omitempty"`
	OSKernelVersion string          `json:"os_kernel_version,omitempty"`
	Notes           string          `json:"notes,omitempty"`
	AddedAt         time.Time       `json:"added_at"`
	LastSeenAt      *time.Time      `json:"last_seen_at,omitempty"`
	MetadataJSON    json.RawMessage `json:"metadata_json,omitempty"`
}

func scanRevDevice(rs scanner) (*RevDevice, error) {
	var d RevDevice
	var ecid sql.NullInt64
	var bdid, cprv sql.NullInt64
	var chipName, boardName, osRunning, osKernel sql.NullString
	var lastSeen sql.NullTime
	var metaRaw sql.NullString
	if err := rs.Scan(&d.ID, &ecid, &d.CPID, &bdid, &cprv, &chipName, &boardName, &osRunning, &osKernel, &d.Notes, &d.AddedAt, &lastSeen, &metaRaw); err != nil {
		return nil, err
	}
	if ecid.Valid {
		v := ecid.Int64
		d.ECID = &v
	}
	if bdid.Valid {
		v := int(bdid.Int64)
		d.BDID = &v
	}
	if cprv.Valid {
		v := int(cprv.Int64)
		d.CPRV = &v
	}
	d.ChipName = chipName.String
	d.BoardName = boardName.String
	d.OSRunning = osRunning.String
	d.OSKernelVersion = osKernel.String
	if lastSeen.Valid {
		t := lastSeen.Time
		d.LastSeenAt = &t
	}
	if metaRaw.Valid {
		d.MetadataJSON = json.RawMessage(metaRaw.String)
	}
	return &d, nil
}

type scanner interface {
	Scan(dest ...any) error
}

const revDeviceCols = `id, ecid, cpid, bdid, cprv, chip_name, board_name, os_running, os_kernel_version, notes, added_at, last_seen_at, metadata_json`

// listRevDevices returns up to `limit` devices filtered by optional
// cpid + bdid. Pass cpid=0 to skip the chip filter; bdid=-1 to skip
// board.
func (p *Plugin) listRevDevices(cpid int, bdid int, limit int) ([]RevDevice, error) {
	if limit <= 0 || limit > 1000 {
		limit = 200
	}
	q := `SELECT ` + revDeviceCols + ` FROM rev_devices WHERE 1=1`
	args := []any{}
	if cpid > 0 {
		args = append(args, cpid)
		q += fmt.Sprintf(` AND cpid = $%d`, len(args))
	}
	if bdid >= 0 {
		args = append(args, bdid)
		q += fmt.Sprintf(` AND bdid = $%d`, len(args))
	}
	args = append(args, limit)
	q += fmt.Sprintf(` ORDER BY COALESCE(last_seen_at, added_at) DESC LIMIT $%d`, len(args))

	rows, err := p.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RevDevice, 0)
	for rows.Next() {
		d, err := scanRevDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

func (p *Plugin) getRevDevice(id int64) (*RevDevice, error) {
	row := p.DB.QueryRow(`SELECT `+revDeviceCols+` FROM rev_devices WHERE id = $1`, id)
	d, err := scanRevDevice(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return d, nil
}

// upsertRevDevice creates by ECID if absent; updates the same row if
// ECID matches an existing entry. ECID NULL → always insert (the row
// has no natural key in that case). Touches last_seen_at to now() on
// every upsert.
func (p *Plugin) upsertRevDevice(d *RevDevice, now time.Time) (*RevDevice, error) {
	if d == nil {
		return nil, errors.New("nil device")
	}
	d.LastSeenAt = &now
	var metaArg any
	if len(d.MetadataJSON) > 0 {
		metaArg = string(d.MetadataJSON)
	}
	if d.ECID != nil {
		err := p.DB.QueryRow(
			`INSERT INTO rev_devices (ecid, cpid, bdid, cprv, chip_name, board_name, os_running, os_kernel_version, notes, added_at, last_seen_at, metadata_json)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $10, $11)
			 ON CONFLICT (ecid) DO UPDATE SET
			    cpid              = EXCLUDED.cpid,
			    bdid              = COALESCE(EXCLUDED.bdid, rev_devices.bdid),
			    cprv              = COALESCE(EXCLUDED.cprv, rev_devices.cprv),
			    chip_name         = COALESCE(NULLIF(EXCLUDED.chip_name, ''), rev_devices.chip_name),
			    board_name        = COALESCE(NULLIF(EXCLUDED.board_name, ''), rev_devices.board_name),
			    os_running        = COALESCE(NULLIF(EXCLUDED.os_running, ''), rev_devices.os_running),
			    os_kernel_version = COALESCE(NULLIF(EXCLUDED.os_kernel_version, ''), rev_devices.os_kernel_version),
			    notes             = COALESCE(NULLIF(EXCLUDED.notes, ''), rev_devices.notes),
			    last_seen_at      = EXCLUDED.last_seen_at,
			    metadata_json     = COALESCE(EXCLUDED.metadata_json, rev_devices.metadata_json)
			 RETURNING id`,
			d.ECID, d.CPID, d.BDID, d.CPRV, d.ChipName, d.BoardName, d.OSRunning, d.OSKernelVersion, d.Notes, now, metaArg,
		).Scan(&d.ID)
		if err != nil {
			return nil, err
		}
	} else {
		err := p.DB.QueryRow(
			`INSERT INTO rev_devices (cpid, bdid, cprv, chip_name, board_name, os_running, os_kernel_version, notes, added_at, last_seen_at, metadata_json)
			 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9, $10)
			 RETURNING id`,
			d.CPID, d.BDID, d.CPRV, d.ChipName, d.BoardName, d.OSRunning, d.OSKernelVersion, d.Notes, now, metaArg,
		).Scan(&d.ID)
		if err != nil {
			return nil, err
		}
	}
	d.AddedAt = now
	return p.getRevDevice(d.ID)
}

func (p *Plugin) deleteRevDevice(id int64) (bool, error) {
	res, err := p.DB.Exec(`DELETE FROM rev_devices WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

func (p *Plugin) recentRevDevices(days int, limit int) ([]RevDevice, error) {
	if days <= 0 {
		days = 7
	}
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	rows, err := p.DB.Query(
		`SELECT `+revDeviceCols+` FROM rev_devices
		  WHERE last_seen_at >= NOW() - ($1 || ' days')::INTERVAL
		  ORDER BY last_seen_at DESC LIMIT $2`,
		days, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RevDevice, 0)
	for rows.Next() {
		d, err := scanRevDevice(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *d)
	}
	return out, rows.Err()
}

// ---------- rev_firmwares ----------

type RevFirmware struct {
	ID             int64           `json:"id"`
	Kind           string          `json:"kind"`
	Vendor         string          `json:"vendor,omitempty"`
	Version        string          `json:"version"`
	ChipIDCompat   []int           `json:"chip_id_compat,omitempty"`
	BoardIDCompat  []int           `json:"board_id_compat,omitempty"`
	BlobURI        string          `json:"blob_uri"`
	BlobSHA256     string          `json:"blob_sha256"`
	SizeBytes      int64           `json:"size_bytes"`
	Format         string          `json:"format,omitempty"`
	ExtractedFrom  string          `json:"extracted_from,omitempty"`
	AddedAt        time.Time       `json:"added_at"`
	AddedBy        string          `json:"added_by,omitempty"`
	MetadataJSON   json.RawMessage `json:"metadata_json,omitempty"`
}

const revFirmwareCols = `id, kind, vendor, version, chip_id_compat, board_id_compat, blob_uri, blob_sha256, size_bytes, format, extracted_from, added_at, added_by, metadata_json`

func scanRevFirmware(rs scanner) (*RevFirmware, error) {
	var f RevFirmware
	var chipCompat, boardCompat pq.Int64Array
	var metaRaw sql.NullString
	if err := rs.Scan(&f.ID, &f.Kind, &f.Vendor, &f.Version, &chipCompat, &boardCompat, &f.BlobURI, &f.BlobSHA256, &f.SizeBytes, &f.Format, &f.ExtractedFrom, &f.AddedAt, &f.AddedBy, &metaRaw); err != nil {
		return nil, err
	}
	for _, v := range chipCompat {
		f.ChipIDCompat = append(f.ChipIDCompat, int(v))
	}
	for _, v := range boardCompat {
		f.BoardIDCompat = append(f.BoardIDCompat, int(v))
	}
	if metaRaw.Valid {
		f.MetadataJSON = json.RawMessage(metaRaw.String)
	}
	return &f, nil
}

func (p *Plugin) listRevFirmwares(kind, version string, cpid int, limit int) ([]RevFirmware, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT ` + revFirmwareCols + ` FROM rev_firmwares WHERE 1=1`
	args := []any{}
	if kind != "" {
		args = append(args, strings.TrimSpace(kind))
		q += fmt.Sprintf(` AND kind = $%d`, len(args))
	}
	if version != "" {
		args = append(args, strings.TrimSpace(version))
		q += fmt.Sprintf(` AND version = $%d`, len(args))
	}
	if cpid > 0 {
		args = append(args, cpid)
		q += fmt.Sprintf(` AND (chip_id_compat IS NULL OR $%d = ANY(chip_id_compat))`, len(args))
	}
	args = append(args, limit)
	q += fmt.Sprintf(` ORDER BY added_at DESC LIMIT $%d`, len(args))

	rows, err := p.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RevFirmware, 0)
	for rows.Next() {
		f, err := scanRevFirmware(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

func (p *Plugin) getRevFirmware(id int64) (*RevFirmware, error) {
	row := p.DB.QueryRow(`SELECT `+revFirmwareCols+` FROM rev_firmwares WHERE id = $1`, id)
	f, err := scanRevFirmware(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return f, nil
}

func (p *Plugin) insertRevFirmware(f *RevFirmware, now time.Time) (*RevFirmware, error) {
	if f == nil {
		return nil, errors.New("nil firmware")
	}
	if strings.TrimSpace(f.Kind) == "" || strings.TrimSpace(f.Version) == "" || strings.TrimSpace(f.BlobURI) == "" || strings.TrimSpace(f.BlobSHA256) == "" {
		return nil, errors.New("kind, version, blob_uri, blob_sha256 are required")
	}
	chipCompat := pq.Array(intSliceToInt64(f.ChipIDCompat))
	boardCompat := pq.Array(intSliceToInt64(f.BoardIDCompat))
	var metaArg any
	if len(f.MetadataJSON) > 0 {
		metaArg = string(f.MetadataJSON)
	}
	err := p.DB.QueryRow(
		`INSERT INTO rev_firmwares (kind, vendor, version, chip_id_compat, board_id_compat, blob_uri, blob_sha256, size_bytes, format, extracted_from, added_at, added_by, metadata_json)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 RETURNING id`,
		f.Kind, f.Vendor, f.Version, chipCompat, boardCompat, f.BlobURI, f.BlobSHA256, f.SizeBytes, f.Format, f.ExtractedFrom, now, f.AddedBy, metaArg,
	).Scan(&f.ID)
	if err != nil {
		return nil, err
	}
	return p.getRevFirmware(f.ID)
}

func (p *Plugin) deleteRevFirmware(id int64) (bool, error) {
	res, err := p.DB.Exec(`DELETE FROM rev_firmwares WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// matchingRevFirmwares — "what firmwares fit this (cpid, bdid)?"
// pre-flight before flashing. cpid=0 / bdid<0 disables the filter.
func (p *Plugin) matchingRevFirmwares(cpid, bdid int, limit int) ([]RevFirmware, error) {
	if limit <= 0 || limit > 500 {
		limit = 100
	}
	q := `SELECT ` + revFirmwareCols + ` FROM rev_firmwares WHERE 1=1`
	args := []any{}
	if cpid > 0 {
		args = append(args, cpid)
		q += fmt.Sprintf(` AND (chip_id_compat IS NULL OR cardinality(chip_id_compat) = 0 OR $%d = ANY(chip_id_compat))`, len(args))
	}
	if bdid >= 0 {
		args = append(args, bdid)
		q += fmt.Sprintf(` AND (board_id_compat IS NULL OR cardinality(board_id_compat) = 0 OR $%d = ANY(board_id_compat))`, len(args))
	}
	args = append(args, limit)
	q += fmt.Sprintf(` ORDER BY added_at DESC LIMIT $%d`, len(args))

	rows, err := p.DB.Query(q, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RevFirmware, 0)
	for rows.Next() {
		f, err := scanRevFirmware(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *f)
	}
	return out, rows.Err()
}

// ---------- rev_functions ----------

type RevFunction struct {
	ID                int64           `json:"id"`
	FirmwareID        int64           `json:"firmware_id"`
	Address           int64           `json:"address"`
	Symbol            string          `json:"symbol,omitempty"`
	Prototype         string          `json:"prototype,omitempty"`
	Purpose           string          `json:"purpose,omitempty"`
	CallingConvention string          `json:"calling_convention,omitempty"`
	ByteSignatureHex  string          `json:"byte_signature_hex,omitempty"`
	IDASignature      string          `json:"ida_signature,omitempty"`
	Confidence        string          `json:"confidence"`
	Source            string          `json:"source,omitempty"`
	AddedAt           time.Time       `json:"added_at"`
	AddedBy           string          `json:"added_by,omitempty"`
	MetadataJSON      json.RawMessage `json:"metadata_json,omitempty"`
}

const revFunctionCols = `id, firmware_id, address, symbol, prototype, purpose, calling_convention, byte_signature, ida_signature, confidence, source, added_at, added_by, metadata_json`

func scanRevFunction(rs scanner) (*RevFunction, error) {
	var fn RevFunction
	var byteSig []byte
	var metaRaw sql.NullString
	if err := rs.Scan(&fn.ID, &fn.FirmwareID, &fn.Address, &fn.Symbol, &fn.Prototype, &fn.Purpose, &fn.CallingConvention, &byteSig, &fn.IDASignature, &fn.Confidence, &fn.Source, &fn.AddedAt, &fn.AddedBy, &metaRaw); err != nil {
		return nil, err
	}
	if len(byteSig) > 0 {
		fn.ByteSignatureHex = bytesToHex(byteSig)
	}
	if metaRaw.Valid {
		fn.MetadataJSON = json.RawMessage(metaRaw.String)
	}
	return &fn, nil
}

func (p *Plugin) listRevFunctions(firmwareID int64, limit int) ([]RevFunction, error) {
	if limit <= 0 || limit > 5000 {
		limit = 500
	}
	rows, err := p.DB.Query(
		`SELECT `+revFunctionCols+` FROM rev_functions WHERE firmware_id = $1
		   ORDER BY address LIMIT $2`,
		firmwareID, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RevFunction, 0)
	for rows.Next() {
		fn, err := scanRevFunction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *fn)
	}
	return out, rows.Err()
}

func (p *Plugin) getRevFunction(id int64) (*RevFunction, error) {
	row := p.DB.QueryRow(`SELECT `+revFunctionCols+` FROM rev_functions WHERE id = $1`, id)
	fn, err := scanRevFunction(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return fn, nil
}

// lookupRevFunctionByAddress — the MOST-used tool in the debug loop.
// Exact address match within the given firmware. Returns nil when no
// row matches.
func (p *Plugin) lookupRevFunctionByAddress(firmwareID int64, address int64) (*RevFunction, error) {
	row := p.DB.QueryRow(
		`SELECT `+revFunctionCols+` FROM rev_functions
		   WHERE firmware_id = $1 AND address = $2 LIMIT 1`,
		firmwareID, address,
	)
	fn, err := scanRevFunction(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return fn, nil
}

func (p *Plugin) lookupRevFunctionBySymbol(firmwareID int64, symbol string) (*RevFunction, error) {
	row := p.DB.QueryRow(
		`SELECT `+revFunctionCols+` FROM rev_functions
		   WHERE firmware_id = $1 AND symbol = $2 LIMIT 1`,
		firmwareID, strings.TrimSpace(symbol),
	)
	fn, err := scanRevFunction(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return fn, nil
}

// searchRevFunctions — fuzzy name+purpose ILIKE search across all
// firmwares. Limit defensively because the table can grow large.
func (p *Plugin) searchRevFunctions(query string, limit int) ([]RevFunction, error) {
	q := "%" + strings.TrimSpace(query) + "%"
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := p.DB.Query(
		`SELECT `+revFunctionCols+` FROM rev_functions
		   WHERE symbol  ILIKE $1
		      OR purpose ILIKE $1
		   ORDER BY id DESC LIMIT $2`,
		q, limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RevFunction, 0)
	for rows.Next() {
		fn, err := scanRevFunction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *fn)
	}
	return out, rows.Err()
}

// matchRevFunctionSignature — cross-firmware lookup by byte
// signature. v1: exact prefix match on byte_signature (operator
// passes the leading N bytes). Future Phase: fuzzy with one-byte
// wildcards à la IDA.
func (p *Plugin) matchRevFunctionSignature(prefixHex string, limit int) ([]RevFunction, error) {
	prefix, err := hexToBytes(strings.TrimSpace(prefixHex))
	if err != nil || len(prefix) == 0 {
		return nil, fmt.Errorf("invalid prefix_hex")
	}
	if limit <= 0 || limit > 200 {
		limit = 20
	}
	rows, err := p.DB.Query(
		`SELECT `+revFunctionCols+` FROM rev_functions
		   WHERE byte_signature IS NOT NULL
		     AND substring(byte_signature for $2) = $1
		   ORDER BY id DESC LIMIT $3`,
		prefix, len(prefix), limit,
	)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := make([]RevFunction, 0)
	for rows.Next() {
		fn, err := scanRevFunction(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *fn)
	}
	return out, rows.Err()
}

func (p *Plugin) insertRevFunction(fn *RevFunction, now time.Time) (*RevFunction, error) {
	if fn == nil {
		return nil, errors.New("nil function")
	}
	if fn.FirmwareID <= 0 {
		return nil, errors.New("firmware_id required")
	}
	if fn.Confidence == "" {
		fn.Confidence = "speculative"
	}
	var byteSig []byte
	if fn.ByteSignatureHex != "" {
		b, err := hexToBytes(fn.ByteSignatureHex)
		if err != nil {
			return nil, fmt.Errorf("invalid byte_signature_hex: %w", err)
		}
		byteSig = b
	}
	var metaArg any
	if len(fn.MetadataJSON) > 0 {
		metaArg = string(fn.MetadataJSON)
	}
	err := p.DB.QueryRow(
		`INSERT INTO rev_functions (firmware_id, address, symbol, prototype, purpose, calling_convention, byte_signature, ida_signature, confidence, source, added_at, added_by, metadata_json)
		 VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
		 RETURNING id`,
		fn.FirmwareID, fn.Address, fn.Symbol, fn.Prototype, fn.Purpose, fn.CallingConvention, byteSig, fn.IDASignature, fn.Confidence, fn.Source, now, fn.AddedBy, metaArg,
	).Scan(&fn.ID)
	if err != nil {
		return nil, err
	}
	return p.getRevFunction(fn.ID)
}

func (p *Plugin) deleteRevFunction(id int64) (bool, error) {
	res, err := p.DB.Exec(`DELETE FROM rev_functions WHERE id = $1`, id)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// ---------- helpers ----------

func intSliceToInt64(in []int) []int64 {
	if len(in) == 0 {
		return nil
	}
	out := make([]int64, len(in))
	for i, v := range in {
		out[i] = int64(v)
	}
	return out
}

func bytesToHex(b []byte) string {
	const hex = "0123456789abcdef"
	out := make([]byte, len(b)*2)
	for i, c := range b {
		out[i*2] = hex[c>>4]
		out[i*2+1] = hex[c&0x0f]
	}
	return string(out)
}

func hexToBytes(s string) ([]byte, error) {
	s = strings.ReplaceAll(s, " ", "")
	s = strings.ReplaceAll(s, ":", "")
	if len(s)%2 != 0 {
		return nil, errors.New("odd-length hex")
	}
	out := make([]byte, len(s)/2)
	for i := 0; i < len(s); i += 2 {
		hi, err1 := hexNibble(s[i])
		lo, err2 := hexNibble(s[i+1])
		if err1 != nil || err2 != nil {
			return nil, errors.New("invalid hex char")
		}
		out[i/2] = byte(hi<<4 | lo)
	}
	return out, nil
}

func hexNibble(c byte) (int, error) {
	switch {
	case c >= '0' && c <= '9':
		return int(c - '0'), nil
	case c >= 'a' && c <= 'f':
		return int(c-'a') + 10, nil
	case c >= 'A' && c <= 'F':
		return int(c-'A') + 10, nil
	}
	return 0, errors.New("invalid hex char")
}
