// 員工填班頁(v1.5 階段 B:逐小時 + when2meet 拖曳塗選)。
// token 從網址 /a/{token} 取出,先打 /api/me 拿可填門市,選店後打 /api/availability。
"use strict";

const $ = (id) => document.getElementById(id);

// 筆刷:塗格子時套用的意願。預設「可配合」。清除 = 改回絕對不行(不落 DB)。
// level 對應 preference_level:2=非常想上 / 1=可配合 / 0=清除(不送出)。
const BRUSHES = [
  { level: 2, label: "非常想上", cls: "pref-2" },
  { level: 1, label: "可配合",   cls: "pref-1" },
  { level: 0, label: "清除",     cls: "pref-empty" },
];
let brush = 1; // 預設筆刷:可配合

const token = decodeURIComponent(location.pathname.split("/a/")[1] || "");

let stores = [];        // 我能填的門市(membership)
let currentStoreId = null;
let openHour = 9, closeHour = 22;
let grid = null;        // 目前的 PaintGrid

function showStatus(msg, isError) {
  const el = $("status");
  el.textContent = msg;
  el.className = "status " + (isError ? "error" : "ok");
  el.hidden = false;
  clearTimeout(showStatus._t);
  showStatus._t = setTimeout(() => { el.hidden = true; }, 4000);
}

async function api(method, path, body) {
  const opts = { method, headers: {} };
  if (body !== undefined) {
    opts.headers["Content-Type"] = "application/json";
    opts.body = JSON.stringify(body);
  }
  const res = await fetch(path, opts);
  const text = await res.text();
  const data = text ? JSON.parse(text) : null;
  if (!res.ok) throw new Error((data && data.error) || ("HTTP " + res.status));
  return data;
}

// --- 一格的外觀:依 dataset.val(0/1/2)上色與標字 ---
function applyCell(td, level) {
  const b = BRUSHES.find((x) => x.level === level) || BRUSHES[2];
  td.dataset.val = String(level);
  td.className = "paint " + (level === 0 ? "pref-empty" : b.cls);
}

// 用目前筆刷塗一格(清除 = 0)。
function paintCell(td) {
  applyCell(td, brush);
}

// --- 筆刷選擇器 ---
function renderBrushBar() {
  const bar = $("brushBar");
  bar.innerHTML = "";
  BRUSHES.forEach((b) => {
    const btn = document.createElement("button");
    btn.textContent = b.label;
    btn.className = "brush " + b.cls;
    btn.dataset.level = b.level;
    btn.classList.toggle("active", b.level === brush);
    btn.addEventListener("click", () => {
      brush = b.level;
      bar.querySelectorAll("button").forEach((x) =>
        x.classList.toggle("active", parseInt(x.dataset.level, 10) === brush));
    });
    bar.appendChild(btn);
  });
}

// --- 渲染填班網格(rows=營業小時、cols=7 天)---
function renderGrid(slots) {
  // 把已塗時段索引成 map["wd_hr"] = level
  const filled = {};
  slots.forEach((s) => { filled[`${s.weekday}_${s.hour}`] = s.preference_level; });

  const hours = [];
  for (let hr = openHour; hr < closeHour; hr++) hours.push(hr);

  grid = createPaintGrid({
    container: $("fillGrid"),
    hours,
    initCell: (td, wd, hr) => applyCell(td, filled[`${wd}_${hr}`] || 0),
    onPaint: (td) => paintCell(td),
    // 點某天表頭 = 整天套用目前筆刷;點某小時列頭 = 該小時 7 天都套用。
    onColHeader: (wd) => hours.forEach((hr) => paintCell(grid.cellAt(wd, hr))),
    onRowHeader: (hr) => { for (let wd = 0; wd < 7; wd++) paintCell(grid.cellAt(wd, hr)); },
  });
}

// 蒐集所有塗了正向(1/2)的格子,整批送出(後端整批覆寫,未塗 = 絕對不行)。
function collectSlots() {
  const slots = [];
  for (let hr = openHour; hr < closeHour; hr++) {
    for (let wd = 0; wd < 7; wd++) {
      const td = grid.cellAt(wd, hr);
      const lvl = parseInt(td.dataset.val, 10);
      if (lvl === 1 || lvl === 2) slots.push({ weekday: wd, hour: hr, preference_level: lvl });
    }
  }
  return slots;
}

function showFatal(msg) {
  $("loading").hidden = true;
  $("meSection").hidden = true;
  $("pickerSection").hidden = true;
  const f = $("fatal");
  f.className = "status error";
  f.textContent = msg;
  f.hidden = false;
}

async function load() {
  if (!token) return showFatal("連結不完整,請向店長索取新的填班連結。");
  try {
    const me = await api("GET", `/api/me?token=${encodeURIComponent(token)}`);
    $("empNameTop").textContent = me.employee.name;
    stores = me.stores || [];
    $("loading").hidden = true;
    if (stores.length === 0) {
      showFatal("你還沒被指派任何門市,請聯絡店長。");
      return;
    }
    renderStorePicker();
    $("pickerSection").hidden = false;
    if (stores.length === 1) selectStore(stores[0].id); // 只有一間就直接進
  } catch (e) {
    showFatal(e.message + "(連結可能已失效,請向店長索取新連結)");
  }
}

function renderStorePicker() {
  const box = $("storePicker");
  box.innerHTML = "";
  stores.forEach((s) => {
    const btn = document.createElement("button");
    btn.textContent = s.name;
    btn.dataset.store = s.id;
    btn.addEventListener("click", () => selectStore(s.id));
    box.appendChild(btn);
  });
}

async function selectStore(storeId) {
  currentStoreId = storeId;
  $("storePicker").querySelectorAll("button").forEach((b) =>
    b.classList.toggle("active", b.dataset.store === storeId));
  try {
    const ctx = await api("GET", `/api/availability?token=${encodeURIComponent(token)}&store_id=${storeId}`);
    $("storeName").textContent = ctx.store.name;
    openHour = ctx.open_hour;
    closeHour = ctx.close_hour;
    renderBrushBar();
    renderGrid(ctx.slots || []);
    $("meSection").hidden = false;
  } catch (e) {
    showStatus(e.message, true);
  }
}

$("clearAll").addEventListener("click", () => {
  if (!grid) return;
  for (let hr = openHour; hr < closeHour; hr++)
    for (let wd = 0; wd < 7; wd++) applyCell(grid.cellAt(wd, hr), 0);
});

$("save").addEventListener("click", async () => {
  if (!currentStoreId) return showStatus("請先選一間門市", true);
  try {
    const slots = collectSlots();
    const res = await api("PUT", `/api/availability?token=${encodeURIComponent(token)}&store_id=${currentStoreId}`, { slots });
    showStatus(`已儲存「${$("storeName").textContent}」,共 ${res.saved} 個時段。感謝填寫!`);
  } catch (e) {
    showStatus(e.message, true);
  }
});

load();
