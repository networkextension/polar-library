-- ============================================================
-- polar_library schema — end-state.
-- Reverse-engineering knowledge base (rev_devices/firmwares/functions).
--
-- Apply:
--   CREATE DATABASE polar_library OWNER ideamesh;
--   psql -d polar_library -f scripts/migrate/library-schema.sql
--
-- No cross-DB refs — library tables are workspace-agnostic (global
-- knowledge base shared across all workspaces). `added_by` is TEXT
-- (TEXT pointer to a user-id; lookups go via dock SDK if/when needed).
-- ============================================================

CREATE TABLE IF NOT EXISTS rev_devices (
    id BIGSERIAL PRIMARY KEY,
    ecid BIGINT UNIQUE,
    cpid INTEGER NOT NULL,
    bdid INTEGER,
    cprv INTEGER,
    chip_name TEXT,
    board_name TEXT,
    os_running TEXT,
    os_kernel_version TEXT,
    notes TEXT NOT NULL DEFAULT '',
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ,
    metadata_json JSONB
);
CREATE INDEX IF NOT EXISTS ix_rev_devices_chip ON rev_devices(cpid, bdid);
CREATE INDEX IF NOT EXISTS ix_rev_devices_last_seen ON rev_devices(last_seen_at DESC NULLS LAST);

CREATE TABLE IF NOT EXISTS rev_firmwares (
    id BIGSERIAL PRIMARY KEY,
    kind TEXT NOT NULL,
    vendor TEXT NOT NULL DEFAULT '',
    version TEXT NOT NULL,
    chip_id_compat INTEGER[],
    board_id_compat INTEGER[],
    blob_uri TEXT NOT NULL,
    blob_sha256 TEXT NOT NULL,
    size_bytes BIGINT NOT NULL DEFAULT 0,
    format TEXT NOT NULL DEFAULT '',
    extracted_from TEXT NOT NULL DEFAULT '',
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    added_by TEXT NOT NULL DEFAULT '',
    metadata_json JSONB
);
CREATE INDEX IF NOT EXISTS ix_rev_firmwares_kind_version ON rev_firmwares(kind, version);
CREATE UNIQUE INDEX IF NOT EXISTS ux_rev_firmwares_sha ON rev_firmwares(blob_sha256);

CREATE TABLE IF NOT EXISTS rev_functions (
    id BIGSERIAL PRIMARY KEY,
    firmware_id BIGINT NOT NULL REFERENCES rev_firmwares(id) ON DELETE CASCADE,
    address BIGINT NOT NULL,
    symbol TEXT NOT NULL DEFAULT '',
    prototype TEXT NOT NULL DEFAULT '',
    purpose TEXT NOT NULL DEFAULT '',
    calling_convention TEXT NOT NULL DEFAULT '',
    byte_signature BYTEA,
    ida_signature TEXT NOT NULL DEFAULT '',
    confidence TEXT NOT NULL DEFAULT 'speculative',
    source TEXT NOT NULL DEFAULT '',
    added_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    added_by TEXT NOT NULL DEFAULT '',
    metadata_json JSONB
);
CREATE INDEX IF NOT EXISTS ix_rev_functions_fw ON rev_functions(firmware_id);
CREATE INDEX IF NOT EXISTS ix_rev_functions_symbol ON rev_functions(symbol);
CREATE INDEX IF NOT EXISTS ix_rev_functions_addr ON rev_functions(firmware_id, address);
