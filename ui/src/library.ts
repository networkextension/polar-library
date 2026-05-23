// /library.html — knowledge-base admin UI (P-library-0a + 0b).
//
// 3 tabs: Devices / Firmwares / Functions. Tab state lives in
// localStorage so a refresh + nav return to the same view.

import {
  deleteDevice,
  deleteFirmware,
  deleteFunction,
  listDevices,
  listFirmwares,
  listFunctions,
  lookupByAddress,
  lookupBySymbol,
  matchingFirmwares,
  recentDevices,
  searchFunctions,
  upsertDevice,
  uploadFirmware,
} from "./api/library.js";
import { logout } from "@networkextension/polar-ui-common/api/session";
import { byId } from "@networkextension/polar-ui-common/lib/dom";
import { hydrateSiteBrand, renderSidebarFoot } from "@networkextension/polar-ui-common/lib/site";
import { bindThemeSync, initStoredTheme } from "@networkextension/polar-ui-common/lib/theme";
import type { RevDevice, RevFirmware, RevFunction } from "./types/library.js";

initStoredTheme();
bindThemeSync();

const TAB_KEY = "polar_library_tab";

// ---------- DOM ----------

const tabs = ["devices", "firmwares", "functions"] as const;
type TabName = (typeof tabs)[number];

const tabButtons: Record<TabName, HTMLButtonElement> = {
  devices: byId<HTMLButtonElement>("tabDevices"),
  firmwares: byId<HTMLButtonElement>("tabFirmwares"),
  functions: byId<HTMLButtonElement>("tabFunctions"),
};
const panes: Record<TabName, HTMLElement> = {
  devices: byId<HTMLElement>("paneDevices"),
  firmwares: byId<HTMLElement>("paneFirmwares"),
  functions: byId<HTMLElement>("paneFunctions"),
};

const devicesTable = byId<HTMLTableElement>("devicesTable");
const devicesTbody = byId<HTMLTableSectionElement>("devicesTbody");
const devicesEmpty = byId<HTMLElement>("devicesEmpty");
const devicesSummary = byId<HTMLElement>("devicesSummary");

const firmwaresTable = byId<HTMLTableElement>("firmwaresTable");
const firmwaresTbody = byId<HTMLTableSectionElement>("firmwaresTbody");
const firmwaresEmpty = byId<HTMLElement>("firmwaresEmpty");
const firmwaresSummary = byId<HTMLElement>("firmwaresSummary");

const functionsTable = byId<HTMLTableElement>("functionsTable");
const functionsTbody = byId<HTMLTableSectionElement>("functionsTbody");
const functionsEmpty = byId<HTMLElement>("functionsEmpty");
const functionsSummary = byId<HTMLElement>("functionsSummary");
const functionsResult = byId<HTMLElement>("functionsResult");

// ---------- helpers ----------

function escapeHTML(s: string): string {
  return s
    .replace(/&/g, "&amp;")
    .replace(/</g, "&lt;")
    .replace(/>/g, "&gt;")
    .replace(/"/g, "&quot;")
    .replace(/'/g, "&#39;");
}

function fmtBytes(n: number): string {
  if (!Number.isFinite(n) || n <= 0) return "—";
  const units = ["B", "KB", "MB", "GB", "TB"];
  let v = n;
  let i = 0;
  while (v >= 1024 && i < units.length - 1) {
    v /= 1024;
    i++;
  }
  return `${v.toFixed(v >= 100 ? 0 : v >= 10 ? 1 : 2)} ${units[i]}`;
}

function fmtRelative(iso?: string): string {
  if (!iso) return "—";
  const t = new Date(iso).getTime();
  if (!Number.isFinite(t)) return iso;
  const sec = Math.floor((Date.now() - t) / 1000);
  if (sec < 60) return `${sec}s 前`;
  if (sec < 3600) return `${Math.floor(sec / 60)}m 前`;
  if (sec < 86400) return `${Math.floor(sec / 3600)}h 前`;
  return `${Math.floor(sec / 86400)}d 前`;
}

function fmtHex(n: number | undefined): string {
  if (n == null || n === 0) return "—";
  return "0x" + n.toString(16);
}

function parseHexOrDec(s: string): number | undefined {
  const t = s.trim();
  if (t === "") return undefined;
  if (t.startsWith("0x") || t.startsWith("0X")) {
    const n = parseInt(t.slice(2), 16);
    return Number.isFinite(n) ? n : undefined;
  }
  const n = parseInt(t, 10);
  return Number.isFinite(n) ? n : undefined;
}

// ---------- tab switcher ----------

function setTab(name: TabName): void {
  for (const t of tabs) {
    tabButtons[t].classList.toggle("lp-tab-active", t === name);
    panes[t].hidden = t !== name;
  }
  try {
    localStorage.setItem(TAB_KEY, name);
  } catch {
    /* localStorage unavailable */
  }
  if (name === "devices") void refreshDevices();
  if (name === "firmwares") void refreshFirmwares();
}

for (const t of tabs) {
  tabButtons[t].addEventListener("click", () => setTab(t));
}

// ---------- devices ----------

function renderDeviceRow(d: RevDevice): HTMLTableRowElement {
  const tr = document.createElement("tr");
  const cells = [
    d.ecid != null ? `<code>${fmtHex(d.ecid)}</code>` : "—",
    `<code>${fmtHex(d.cpid)}</code>`,
    d.bdid != null ? `<code>${fmtHex(d.bdid)}</code>` : "—",
    escapeHTML(d.chip_name || "—"),
    escapeHTML(d.board_name || "—"),
    escapeHTML(d.os_running || "—"),
    fmtRelative(d.last_seen_at),
    `<button class="btn-inline btn-secondary" data-act="del-dev" data-id="${d.id}">✕</button>`,
  ];
  tr.innerHTML = cells.map((c) => `<td>${c}</td>`).join("");
  return tr;
}

async function refreshDevices(): Promise<void> {
  const cpidStr = (byId<HTMLInputElement>("devCpidFilter")).value;
  const bdidStr = (byId<HTMLInputElement>("devBdidFilter")).value;
  const cpid = parseHexOrDec(cpidStr);
  const bdid = parseHexOrDec(bdidStr);
  devicesSummary.textContent = "加载中...";
  try {
    const { data } = await listDevices({ cpid, bdid, limit: 200 });
    const items = data.devices ?? [];
    devicesTbody.innerHTML = "";
    items.forEach((d) => devicesTbody.appendChild(renderDeviceRow(d)));
    devicesTable.hidden = items.length === 0;
    devicesEmpty.hidden = items.length !== 0;
    devicesSummary.textContent = `${items.length} 台`;
  } catch (e) {
    devicesSummary.textContent = `加载失败: ${(e as Error).message}`;
  }
}

devicesTbody.addEventListener("click", async (e) => {
  const t = e.target as HTMLElement;
  if (t.tagName === "BUTTON" && t.dataset.act === "del-dev") {
    const id = parseInt(t.dataset.id || "", 10);
    if (!id || !confirm("删除该设备？")) return;
    await deleteDevice(id);
    void refreshDevices();
  }
});

byId<HTMLButtonElement>("devicesRefreshBtn").addEventListener("click", () => void refreshDevices());
byId<HTMLButtonElement>("devApplyFilter").addEventListener("click", () => void refreshDevices());

// New-device modal
const deviceModal = byId<HTMLElement>("deviceModal");
byId<HTMLButtonElement>("newDeviceBtn").addEventListener("click", () => {
  (byId<HTMLElement>("deviceModalError")).textContent = "";
  deviceModal.hidden = false;
});
byId<HTMLButtonElement>("deviceModalCloseBtn").addEventListener("click", () => (deviceModal.hidden = true));
byId<HTMLButtonElement>("deviceModalCancelBtn").addEventListener("click", () => (deviceModal.hidden = true));
byId<HTMLButtonElement>("deviceModalSubmitBtn").addEventListener("click", async () => {
  const err = byId<HTMLElement>("deviceModalError");
  err.textContent = "";
  const cpid = parseHexOrDec((byId<HTMLInputElement>("devCpid")).value);
  if (!cpid) {
    err.textContent = "CPID 必填";
    return;
  }
  const payload: Partial<RevDevice> = {
    ecid: parseHexOrDec((byId<HTMLInputElement>("devEcid")).value),
    cpid,
    bdid: parseHexOrDec((byId<HTMLInputElement>("devBdid")).value),
    cprv: parseHexOrDec((byId<HTMLInputElement>("devCprv")).value),
    chip_name: (byId<HTMLInputElement>("devChipName")).value.trim(),
    board_name: (byId<HTMLInputElement>("devBoardName")).value.trim(),
    os_running: (byId<HTMLInputElement>("devOs")).value.trim(),
    notes: (byId<HTMLInputElement>("devNotes")).value.trim(),
  };
  try {
    const { data } = await upsertDevice(payload);
    if (data.error) {
      err.textContent = data.error;
      return;
    }
    deviceModal.hidden = true;
    void refreshDevices();
  } catch (e) {
    err.textContent = (e as Error).message;
  }
});

// ---------- firmwares ----------

function renderFirmwareRow(f: RevFirmware): HTMLTableRowElement {
  const tr = document.createElement("tr");
  const compatStr =
    (f.chip_id_compat ?? []).length > 0 ? (f.chip_id_compat ?? []).map((n) => "0x" + n.toString(16)).join(", ") : "—";
  const shaShort = f.blob_sha256.substring(0, 12) + "…";
  const cells = [
    escapeHTML(f.kind),
    escapeHTML(f.vendor || "—"),
    escapeHTML(f.version),
    `<code style="font-size:11px;">${escapeHTML(compatStr)}</code>`,
    fmtBytes(f.size_bytes),
    `<a href="/api/library/firmwares/${f.id}/download" title="${escapeHTML(f.blob_sha256)}"><code style="font-size:11px;">${escapeHTML(shaShort)}</code> ↓</a>`,
    fmtRelative(f.added_at),
    `<button class="btn-inline btn-secondary" data-act="del-fw" data-id="${f.id}">✕</button>`,
  ];
  tr.innerHTML = cells.map((c) => `<td>${c}</td>`).join("");
  return tr;
}

async function refreshFirmwares(): Promise<void> {
  const kind = (byId<HTMLInputElement>("fwKindFilter")).value.trim();
  const cpid = parseHexOrDec((byId<HTMLInputElement>("fwCpidFilter")).value);
  firmwaresSummary.textContent = "加载中...";
  try {
    const { data } = await listFirmwares({ kind, cpid, limit: 200 });
    const items = data.firmwares ?? [];
    firmwaresTbody.innerHTML = "";
    items.forEach((f) => firmwaresTbody.appendChild(renderFirmwareRow(f)));
    firmwaresTable.hidden = items.length === 0;
    firmwaresEmpty.hidden = items.length !== 0;
    firmwaresSummary.textContent = `${items.length} 个`;
  } catch (e) {
    firmwaresSummary.textContent = `加载失败: ${(e as Error).message}`;
  }
}

firmwaresTbody.addEventListener("click", async (e) => {
  const t = e.target as HTMLElement;
  if (t.tagName === "BUTTON" && t.dataset.act === "del-fw") {
    const id = parseInt(t.dataset.id || "", 10);
    if (!id || !confirm("删除该固件？所有挂在它上面的函数也会级联删除。")) return;
    await deleteFirmware(id);
    void refreshFirmwares();
  }
});

byId<HTMLButtonElement>("firmwaresRefreshBtn").addEventListener("click", () => void refreshFirmwares());
byId<HTMLButtonElement>("fwApplyFilter").addEventListener("click", () => void refreshFirmwares());

// Upload-firmware modal
const fwModal = byId<HTMLElement>("firmwareModal");
byId<HTMLButtonElement>("uploadFirmwareBtn").addEventListener("click", () => {
  (byId<HTMLElement>("firmwareModalError")).textContent = "";
  (byId<HTMLElement>("firmwareModalProgress")).textContent = "";
  fwModal.hidden = false;
});
byId<HTMLButtonElement>("firmwareModalCloseBtn").addEventListener("click", () => (fwModal.hidden = true));
byId<HTMLButtonElement>("firmwareModalCancelBtn").addEventListener("click", () => (fwModal.hidden = true));
byId<HTMLButtonElement>("firmwareModalSubmitBtn").addEventListener("click", async () => {
  const err = byId<HTMLElement>("firmwareModalError");
  const prog = byId<HTMLElement>("firmwareModalProgress");
  err.textContent = "";
  prog.textContent = "";
  const fileInput = byId<HTMLInputElement>("fwFile");
  const file = fileInput.files && fileInput.files[0];
  const kind = (byId<HTMLInputElement>("fwKind")).value.trim();
  const version = (byId<HTMLInputElement>("fwVersion")).value.trim();
  if (!file) {
    err.textContent = "请选文件";
    return;
  }
  if (!kind || !version) {
    err.textContent = "kind + version 必填";
    return;
  }
  const form = new FormData();
  form.append("file", file);
  form.append("kind", kind);
  form.append("version", version);
  form.append("vendor", (byId<HTMLInputElement>("fwVendor")).value);
  form.append("format", (byId<HTMLInputElement>("fwFormat")).value);
  form.append("chip_id_compat", (byId<HTMLInputElement>("fwCpidCompat")).value);
  form.append("board_id_compat", (byId<HTMLInputElement>("fwBdidCompat")).value);
  form.append("extracted_from", (byId<HTMLInputElement>("fwExtracted")).value);
  prog.textContent = `上传中... (${fmtBytes(file.size)})`;
  try {
    const resp = await uploadFirmware(form);
    if (!resp.ok) {
      err.textContent = resp.data?.error || `HTTP ${resp.status}`;
      prog.textContent = "";
      return;
    }
    prog.textContent = "✓ 上传完成";
    setTimeout(() => {
      fwModal.hidden = true;
      void refreshFirmwares();
    }, 600);
  } catch (e) {
    err.textContent = (e as Error).message;
    prog.textContent = "";
  }
});

// ---------- functions ----------

function renderFunctionRow(fn: RevFunction): HTMLTableRowElement {
  const tr = document.createElement("tr");
  const addrHex = "0x" + fn.address.toString(16);
  const cells = [
    `<code>${escapeHTML(addrHex)}</code>`,
    `<code>${escapeHTML(fn.symbol || "—")}</code>`,
    escapeHTML(fn.prototype || "—"),
    escapeHTML(fn.purpose || "—"),
    escapeHTML(fn.confidence || "—"),
    `<button class="btn-inline btn-secondary" data-act="del-fn" data-id="${fn.id}">✕</button>`,
  ];
  tr.innerHTML = cells.map((c) => `<td>${c}</td>`).join("");
  return tr;
}

functionsTbody.addEventListener("click", async (e) => {
  const t = e.target as HTMLElement;
  if (t.tagName === "BUTTON" && t.dataset.act === "del-fn") {
    const id = parseInt(t.dataset.id || "", 10);
    if (!id || !confirm("删除该函数记录？")) return;
    await deleteFunction(id);
    void renderFunctionTableForCurrentFirmware();
  }
});

async function renderFunctionTableForCurrentFirmware(): Promise<void> {
  const fid = parseInt((byId<HTMLInputElement>("fnFwId")).value, 10);
  if (!fid) {
    functionsTable.hidden = true;
    functionsEmpty.hidden = false;
    functionsSummary.textContent = "—";
    return;
  }
  functionsSummary.textContent = "加载中...";
  try {
    const { data } = await listFunctions(fid);
    const items = data.functions ?? [];
    functionsTbody.innerHTML = "";
    items.forEach((fn) => functionsTbody.appendChild(renderFunctionRow(fn)));
    functionsTable.hidden = items.length === 0;
    functionsEmpty.hidden = items.length !== 0;
    functionsSummary.textContent = `${items.length} 条 (firmware #${fid})`;
  } catch (e) {
    functionsSummary.textContent = `加载失败: ${(e as Error).message}`;
  }
}

(byId<HTMLInputElement>("fnFwId")).addEventListener("change", () => void renderFunctionTableForCurrentFirmware());

function showResult(html: string): void {
  functionsResult.innerHTML = html;
  functionsResult.hidden = false;
}

byId<HTMLButtonElement>("fnLookupAddrBtn").addEventListener("click", async () => {
  const fid = parseInt((byId<HTMLInputElement>("fnFwId")).value, 10);
  const addr = (byId<HTMLInputElement>("fnAddr")).value.trim();
  if (!fid || !addr) {
    showResult('<span class="meta-subtitle">需要 firmware_id + address</span>');
    return;
  }
  const { response, data } = await lookupByAddress(fid, addr);
  if (!response.ok || !data.function) {
    showResult(`<span style="color:var(--danger,#c00);">未找到 (HTTP ${response.status})</span>`);
    return;
  }
  const fn = data.function;
  showResult(`<strong>${escapeHTML(fn.symbol || "—")}</strong>
    <div class="meta-subtitle"><code>${escapeHTML(fn.prototype || "")}</code></div>
    <div class="meta-subtitle">purpose: ${escapeHTML(fn.purpose || "—")}</div>
    <div class="meta-subtitle">confidence: ${escapeHTML(fn.confidence)} · source: ${escapeHTML(fn.source || "—")}</div>`);
});

byId<HTMLButtonElement>("fnLookupSymBtn").addEventListener("click", async () => {
  const fid = parseInt((byId<HTMLInputElement>("fnFwId")).value, 10);
  const sym = (byId<HTMLInputElement>("fnSymbol")).value.trim();
  if (!fid || !sym) {
    showResult('<span class="meta-subtitle">需要 firmware_id + symbol</span>');
    return;
  }
  const { response, data } = await lookupBySymbol(fid, sym);
  if (!response.ok || !data.function) {
    showResult(`<span style="color:var(--danger,#c00);">未找到 (HTTP ${response.status})</span>`);
    return;
  }
  const fn = data.function;
  showResult(`<strong>0x${fn.address.toString(16)}</strong> ${escapeHTML(fn.symbol || "")}
    <div class="meta-subtitle"><code>${escapeHTML(fn.prototype || "")}</code></div>
    <div class="meta-subtitle">${escapeHTML(fn.purpose || "")}</div>`);
});

byId<HTMLButtonElement>("fnSearchBtn").addEventListener("click", async () => {
  const q = (byId<HTMLInputElement>("fnSearch")).value.trim();
  if (!q) return;
  const { data } = await searchFunctions(q);
  const items = data.functions ?? [];
  functionsTbody.innerHTML = "";
  items.forEach((fn) => functionsTbody.appendChild(renderFunctionRow(fn)));
  functionsTable.hidden = items.length === 0;
  functionsEmpty.hidden = items.length !== 0;
  functionsSummary.textContent = `search "${q}": ${items.length} 条`;
});

// ---------- bootstrap ----------

byId<HTMLButtonElement>("logoutBtn").addEventListener("click", () => void logout().then(() => (window.location.href = "/login.html")));

async function bootstrap(): Promise<void> {
  await hydrateSiteBrand();
  await renderSidebarFoot();
  const saved = (() => {
    try {
      return localStorage.getItem(TAB_KEY) as TabName | null;
    } catch {
      return null;
    }
  })();
  setTab(saved && tabs.includes(saved) ? saved : "firmwares");
  // Mute the unused-import warning for matchingFirmwares + recentDevices —
  // they're public on the API client for callers that want them (and the
  // future polar-agent watcher will use recentDevices). The page itself
  // only uses listDevices / listFirmwares directly today.
  void matchingFirmwares;
  void recentDevices;
}

void bootstrap();
