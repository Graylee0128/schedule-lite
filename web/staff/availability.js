// 員工填班頁邏輯。token 從網址 /a/{token} 取出,再用它打 /api/availability。
"use strict";

const $ = (id) => document.getElementById(id);
const WEEKDAYS = ["週日", "週一", "週二", "週三", "週四", "週五", "週六"];

// 四種狀態,點一下循環切換(手機友善,免下拉)。順序:白→綠→黃→紅→白。
// level 為空字串代表「未填」(不送出);其餘對應 preference_level 2/1/0。
const STATES = [
  { level: "",  label: "未填",     cls: "pref-empty" },
  { level: "2", label: "非常想上", cls: "pref-2" }, // 綠
  { level: "1", label: "可配合",   cls: "pref-1" }, // 黃
  { level: "0", label: "絕對不行", cls: "pref-0" }, // 紅
];

// 由現有 level 找到它在 STATES 的索引(找不到當未填)。
const stateIndexByLevel = (lvl) => {
  const i = STATES.findIndex((s) => s.level === lvl);
  return i < 0 ? 0 : i;
};

// 從 /a/{token} 路徑取出 token。
const token = decodeURIComponent(location.pathname.split("/a/")[1] || "");

let templates = [];

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

const hhmm = (t) => (t || "").slice(0, 5);
const cellId = (templateId, weekday) => `c_${templateId}_${weekday}`;

// 建一個可點擊的色塊格子;點一下就循環到下一個狀態。
// 目前狀態存在 button.dataset.level(""/"2"/"1"/"0"),送出時讀它。
function prefCell(templateId, weekday, level) {
  const cell = document.createElement("button");
  cell.type = "button";
  cell.id = cellId(templateId, weekday);
  let idx = stateIndexByLevel(level);

  const apply = () => {
    const s = STATES[idx];
    cell.dataset.level = s.level;
    cell.textContent = s.label;
    cell.className = "pref " + s.cls;
  };
  apply();

  cell.addEventListener("click", () => {
    idx = (idx + 1) % STATES.length; // 循環:白→綠→黃→紅→白
    apply();
  });
  return cell;
}

// 把後端回來的 slots 轉成 { "templateId_weekday": "level字串" } 方便查。
function indexSlots(slots) {
  const m = {};
  slots.forEach((s) => { m[`${s.shift_template_id}_${s.weekday}`] = String(s.preference_level); });
  return m;
}

// 手機友善:把「天」做成列、班別做成欄(最多 4 欄,塞得進手機寬度),
// 由上往下捲就能填完一週,不必橫向捲動 7 欄。
function renderGrid(slotMap) {
  const cols = [...templates].sort((a, b) => a.start_local.localeCompare(b.start_local));
  const table = document.createElement("table");
  table.className = "grid";

  const thead = document.createElement("thead");
  const htr = document.createElement("tr");
  htr.innerHTML = "<th>星期</th>" +
    cols.map((t) => `<th>${t.name}<br><small>${hhmm(t.start_local)}-${hhmm(t.end_local)}</small></th>`).join("");
  thead.appendChild(htr);
  table.appendChild(thead);

  const tbody = document.createElement("tbody");
  for (let wd = 0; wd < 7; wd++) {
    const tr = document.createElement("tr");
    const th = document.createElement("th");
    th.textContent = WEEKDAYS[wd];
    tr.appendChild(th);
    cols.forEach((t) => {
      const td = document.createElement("td");
      const selected = slotMap[`${t.id}_${wd}`] || "";
      td.appendChild(prefCell(t.id, wd, selected));
      tr.appendChild(td);
    });
    tbody.appendChild(tr);
  }
  table.appendChild(tbody);

  $("fillGrid").innerHTML = "";
  $("fillGrid").appendChild(table);
}

// showFatal 顯示「無法繼續」的常駐錯誤(不像 toast 會消失),並收起表單與載入字樣。
function showFatal(msg) {
  $("loading").hidden = true;
  $("meSection").hidden = true;
  const f = $("fatal");
  f.className = "status error";
  f.textContent = msg;
  f.hidden = false;
}

async function load() {
  if (!token) return showFatal("連結不完整,請向店長索取新的填班連結。");
  try {
    const ctx = await api("GET", `/api/availability?token=${encodeURIComponent(token)}`);
    $("empName").textContent = ctx.employee.name;
    $("storeName").textContent = ctx.store.name;
    templates = ctx.templates || [];
    renderGrid(indexSlots(ctx.slots || []));
    $("loading").hidden = true;
    $("meSection").hidden = false;
  } catch (e) {
    showFatal(e.message + "(連結可能已失效,請向店長索取新連結)");
  }
}

// 蒐集所有非「未填」的格子,整批送出(後端整批覆寫)。
function collectSlots() {
  const slots = [];
  templates.forEach((t) => {
    for (let wd = 0; wd < 7; wd++) {
      const lvl = $(cellId(t.id, wd)).dataset.level;
      if (lvl !== "") {
        slots.push({ shift_template_id: t.id, weekday: wd, preference_level: parseInt(lvl, 10) });
      }
    }
  });
  return slots;
}

$("save").addEventListener("click", async () => {
  try {
    const slots = collectSlots();
    const res = await api("PUT", `/api/availability?token=${encodeURIComponent(token)}`, { slots });
    showStatus(`已儲存,共 ${res.saved} 格時段。感謝填寫!`);
  } catch (e) {
    showStatus(e.message, true);
  }
});

load();
