// Typed fetch wrappers for /api/library/...

import { requestJson } from "./http.js";
import type {
  RevDeviceListResponse,
  RevDeviceResponse,
  RevFirmwareListResponse,
  RevFirmwareResponse,
  RevFunctionListResponse,
  RevFunctionResponse,
  RevDevice,
} from "../types/library.js";

// ---- devices ----

export async function listDevices(opts: { cpid?: number; bdid?: number; limit?: number } = {}) {
  const qs = new URLSearchParams();
  if (opts.cpid != null) qs.set("cpid", String(opts.cpid));
  if (opts.bdid != null) qs.set("bdid", String(opts.bdid));
  if (opts.limit != null) qs.set("limit", String(opts.limit));
  const q = qs.toString();
  return requestJson<RevDeviceListResponse>("/api/library/devices" + (q ? "?" + q : ""));
}

export async function recentDevices(days = 7, limit = 100) {
  return requestJson<RevDeviceListResponse>(`/api/library/devices/recent?days=${days}&limit=${limit}`);
}

export async function upsertDevice(d: Partial<RevDevice>) {
  return requestJson<RevDeviceResponse>("/api/library/devices", {
    method: "POST",
    body: d,
  });
}

export async function deleteDevice(id: number) {
  return requestJson<{ ok: boolean; error?: string }>(`/api/library/devices/${id}`, { method: "DELETE" });
}

// ---- firmwares ----

export async function listFirmwares(opts: { kind?: string; version?: string; cpid?: number; limit?: number } = {}) {
  const qs = new URLSearchParams();
  if (opts.kind) qs.set("kind", opts.kind);
  if (opts.version) qs.set("version", opts.version);
  if (opts.cpid != null) qs.set("cpid", String(opts.cpid));
  if (opts.limit != null) qs.set("limit", String(opts.limit));
  const q = qs.toString();
  return requestJson<RevFirmwareListResponse>("/api/library/firmwares" + (q ? "?" + q : ""));
}

export async function matchingFirmwares(cpid: number, bdid?: number) {
  const qs = new URLSearchParams({ cpid: String(cpid) });
  if (bdid != null) qs.set("bdid", String(bdid));
  return requestJson<RevFirmwareListResponse>("/api/library/firmwares/matching?" + qs.toString());
}

export async function deleteFirmware(id: number) {
  return requestJson<{ ok: boolean; error?: string }>(`/api/library/firmwares/${id}`, { method: "DELETE" });
}

// uploadFirmware uses raw fetch (not requestJson) because multipart
// upload doesn't fit the JSON body shape.
export async function uploadFirmware(form: FormData): Promise<{ ok: boolean; status: number; data: RevFirmwareResponse }> {
  const resp = await fetch("/api/library/firmwares/upload", {
    method: "POST",
    body: form,
    headers: {
      // Browser sets multipart Content-Type with boundary on its own
      // when body is FormData — DO NOT set it manually here.
      "X-Workspace-Id": localStorage.getItem("polar_active_workspace_id") || "",
    },
    credentials: "include",
  });
  const data: RevFirmwareResponse = await resp.json().catch(() => ({} as RevFirmwareResponse));
  return { ok: resp.ok, status: resp.status, data };
}

// ---- functions ----

export async function listFunctions(firmwareID: number, limit = 500) {
  return requestJson<RevFunctionListResponse>(`/api/library/functions?firmware_id=${firmwareID}&limit=${limit}`);
}

export async function lookupByAddress(firmwareID: number, address: string | number) {
  return requestJson<RevFunctionResponse>(
    `/api/library/functions/lookup-by-address?firmware_id=${firmwareID}&address=${encodeURIComponent(String(address))}`,
  );
}

export async function lookupBySymbol(firmwareID: number, name: string) {
  return requestJson<RevFunctionResponse>(
    `/api/library/functions/lookup-by-symbol?firmware_id=${firmwareID}&name=${encodeURIComponent(name)}`,
  );
}

export async function searchFunctions(q: string, limit = 50) {
  return requestJson<RevFunctionListResponse>(
    `/api/library/functions/search?q=${encodeURIComponent(q)}&limit=${limit}`,
  );
}

export async function deleteFunction(id: number) {
  return requestJson<{ ok: boolean; error?: string }>(`/api/library/functions/${id}`, { method: "DELETE" });
}
