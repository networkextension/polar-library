// Mirrors internal/app/dock/rev_store.go.

import type { ErrorResponse } from "./dashboard.js";

export type RevDevice = {
  id: number;
  ecid?: number;
  cpid: number;
  bdid?: number;
  cprv?: number;
  chip_name?: string;
  board_name?: string;
  os_running?: string;
  os_kernel_version?: string;
  notes?: string;
  added_at: string;
  last_seen_at?: string;
  metadata_json?: unknown;
};

export type RevFirmware = {
  id: number;
  kind: string;
  vendor?: string;
  version: string;
  chip_id_compat?: number[];
  board_id_compat?: number[];
  blob_uri: string;
  blob_sha256: string;
  size_bytes: number;
  format?: string;
  extracted_from?: string;
  added_at: string;
  added_by?: string;
  metadata_json?: unknown;
};

export type RevFunction = {
  id: number;
  firmware_id: number;
  address: number;
  symbol?: string;
  prototype?: string;
  purpose?: string;
  calling_convention?: string;
  byte_signature_hex?: string;
  ida_signature?: string;
  confidence: string;
  source?: string;
  added_at: string;
  added_by?: string;
  metadata_json?: unknown;
};

export type RevDeviceListResponse = ErrorResponse & { devices?: RevDevice[]; days?: number };
export type RevDeviceResponse = ErrorResponse & { device?: RevDevice };
export type RevFirmwareListResponse = ErrorResponse & {
  firmwares?: RevFirmware[];
  cpid?: number;
  bdid?: number;
};
export type RevFirmwareResponse = ErrorResponse & { firmware?: RevFirmware };
export type RevFunctionListResponse = ErrorResponse & { functions?: RevFunction[]; q?: string };
export type RevFunctionResponse = ErrorResponse & { function?: RevFunction };
