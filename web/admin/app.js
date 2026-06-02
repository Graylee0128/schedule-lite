// schedule-lite 管理台前端邏輯。
// 純 JS,透過 fetch 打同源 /api/*(前後端分離 / CSR)。
"use strict";

const $ = (id) => document.getElementById(id);
const WEEKDAYS = ["週日", "週一", "週二", "週三", "週四", "週五", "週六"];

// 目前選定的 org / store。注意:這裡用 localStorage 只記「這台瀏覽器上次看哪一個」
// 這種 UI 選擇偏好;真資料一律回 DB 重抓。沒有店長登入前,選擇偏好不適合進 DB。
const LS_ORG = "sl_org";
const LS_STORE = "sl_store";
let orgId = null;
let storeId = null;
let orgStores = []; // 目前組織的所有門市(給員工門市指派 checkbox 用)

// v1.5 階段 B:目前門市的營業時段 + 需求網格(逐小時)。
let openHour = 9, closeHour = 22;
let reqGrid = null;  // 需求 PaintGrid
let reqBrush = 1;    // 需求筆刷(要幾人;0=清除)

// --- 共用工具 ---

function showStatus(msg, isError) {
  const el = $("status");
  el.textContent = msg;
  el.className = "status " + (isError ? "error" : "ok");
  el.hidden = false;
  clearTimeout(showStatus._t);
  showStatus._t = setTimeout(() => { el.hidden = true; }, 4000);
}

// api 封裝 fetch:自動帶 JSON header、解析回應、把後端的 {"error"} 轉成例外。
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

// --- 1. 組織(可選既有 / 可新建)---

async function loadOrganizations() {
  const orgs = await api("GET", "/api/organizations");
  const sel = $("orgSelect");
  sel.innerHTML = '<option value="">-- 選擇既有組織 --</option>';
  orgs.forEach((o) => {
    const opt = document.createElement("option");
    opt.value = o.id;
    opt.textContent = o.name;
    sel.appendChild(opt);
  });
}

// selectOrg 切換目前組織:載入它的門市/員工,並嘗試還原上次看的門市。
async function selectOrg(id) {
  orgId = id || null;
  $("storeSection").hidden = !orgId;
  $("employeeSection").hidden = !orgId;
  if (!orgId) {
    $("orgId").textContent = "尚未選擇";
    localStorage.removeItem(LS_ORG);
    selectStore("");
    return;
  }
  localStorage.setItem(LS_ORG, orgId);
  const sel = $("orgSelect");
  $("orgId").textContent = sel.options[sel.selectedIndex] ? sel.options[sel.selectedIndex].text : "(已選)";
  await Promise.all([loadStores(), loadEmployees()]);

  // 還原上次門市:只有它屬於這個 org(出現在下拉)時才還原,否則清掉。
  const savedStore = localStorage.getItem(LS_STORE);
  const ssel = $("storeSelect");
  if (savedStore && [...ssel.options].some((o) => o.value === savedStore)) {
    ssel.value = savedStore;
    selectStore(savedStore);
  } else {
    selectStore("");
  }
}

$("orgSelect").addEventListener("change", (e) => selectOrg(e.target.value));

$("createOrg").addEventListener("click", async () => {
  const name = $("orgName").value.trim();
  if (!name) return showStatus("請輸入組織名稱", true);
  try {
    const org = await api("POST", "/api/organizations", { name });
    $("orgName").value = "";
    await loadOrganizations();
    $("orgSelect").value = org.id;
    await selectOrg(org.id);
    showStatus("組織已建立:" + org.name);
  } catch (e) { showStatus(e.message, true); }
});

// --- 2. 門市 ---

async function loadStores() {
  const stores = await api("GET", `/api/stores?organization_id=${orgId}`);
  orgStores = stores; // 留給員工門市指派用
  const sel = $("storeSelect");
  sel.innerHTML = '<option value="">-- 選擇門市 --</option>';
  stores.forEach((s) => {
    const o = document.createElement("option");
    o.value = s.id;
    o.textContent = s.name;
    sel.appendChild(o);
  });
}

$("createStore").addEventListener("click", async () => {
  const name = $("storeName").value.trim();
  if (!name) return showStatus("請輸入門市名稱", true);
  try {
    const s = await api("POST", "/api/stores", { organization_id: orgId, name });
    $("storeName").value = "";
    showStatus("門市已建立:" + s.name);
    await loadStores();
    $("storeSelect").value = s.id;
    selectStore(s.id);
  } catch (e) { showStatus(e.message, true); }
});

$("storeSelect").addEventListener("change", (e) => selectStore(e.target.value));

function selectStore(id) {
  storeId = id || null;
  if (storeId) localStorage.setItem(LS_STORE, storeId);
  else localStorage.removeItem(LS_STORE);
  const show = !!storeId;
  $("settingsSection").hidden = !show;
  $("gridSection").hidden = !show;
  if (show) loadStoreSettings();
}

// --- 3. 員工 ---

async function loadEmployees() {
  const emps = await api("GET", `/api/employees?organization_id=${orgId}`);
  const ul = $("empList");
  ul.innerHTML = "";
  emps.forEach((e) => {
    const li = document.createElement("li");
    const span = document.createElement("span");
    span.textContent = e.name + (e.phone ? ` (${e.phone})` : "") + "　";
    const linkBtn = document.createElement("button");
    linkBtn.textContent = "發填班連結";
    linkBtn.addEventListener("click", () => makeLink(e.id, e.name));
    const memBtn = document.createElement("button");
    memBtn.textContent = "門市";
    memBtn.style.marginLeft = "0.3rem";
    const panel = document.createElement("div");
    panel.className = "muted";
    panel.style.margin = "0.3rem 0 0.6rem";
    panel.hidden = true;
    memBtn.addEventListener("click", () => toggleMemberships(e.id, panel));
    li.append(span, linkBtn, memBtn, panel);
    ul.appendChild(li);
  });
}

// makeLink 為某員工產生 magic-link(v1.5:綁員工、不綁門市,一人一條),顯示完整連結供複製。
async function makeLink(empId, empName) {
  try {
    const res = await api("POST", "/api/access-links", { employee_id: empId });
    const full = location.origin + res.url;
    const out = $("linkUrl");
    out.value = full;
    $("linkOut").hidden = false;
    out.focus();
    out.select();
    try { await navigator.clipboard.writeText(full); showStatus(`已複製 ${empName} 的填班連結`); }
    catch { showStatus(`已產生 ${empName} 的連結,請手動複製`); }
  } catch (e) { showStatus(e.message, true); }
}

// toggleMemberships 展開/收合某員工的門市指派:勾選 = 屬於該店、可填該店班。
async function toggleMemberships(empId, panel) {
  if (!panel.hidden) { panel.hidden = true; return; }
  try {
    const member = await api("GET", `/api/memberships?employee_id=${empId}`);
    const memberIds = new Set(member.map((s) => s.id));
    panel.textContent = "可填門市:";
    orgStores.forEach((s) => {
      const label = document.createElement("label");
      label.style.marginRight = "0.6rem";
      const cb = document.createElement("input");
      cb.type = "checkbox";
      cb.checked = memberIds.has(s.id);
      cb.addEventListener("change", () => setMembership(empId, s.id, cb.checked));
      label.append(cb, document.createTextNode(" " + s.name));
      panel.appendChild(label);
    });
    panel.hidden = false;
  } catch (e) { showStatus(e.message, true); }
}

async function setMembership(empId, sid, on) {
  try {
    if (on) await api("POST", "/api/memberships", { employee_id: empId, store_id: sid });
    else await api("DELETE", `/api/memberships?employee_id=${empId}&store_id=${sid}`);
    showStatus("已更新門市歸屬");
  } catch (e) { showStatus(e.message, true); }
}

$("createEmp").addEventListener("click", async () => {
  const name = $("empName").value.trim();
  if (!name) return showStatus("請輸入姓名", true);
  const phone = $("empPhone").value.trim();
  try {
    await api("POST", "/api/employees", { organization_id: orgId, name, phone: phone || null });
    $("empName").value = "";
    $("empPhone").value = "";
    showStatus("員工已建立:" + name);
    await loadEmployees();
  } catch (e) { showStatus(e.message, true); }
});

// --- 4. 營業時段 + 逐小時需求人數(v1.5 階段 B,取代固定 4 班別)---

const hoursRange = () => {
  const out = [];
  for (let hr = openHour; hr < closeHour; hr++) out.push(hr);
  return out;
};

// 載入門市設定:先取營業時段,再依時段畫需求網格,最後抓缺口。
async function loadStoreSettings() {
  try {
    const h = await api("GET", `/api/store-hours?store_id=${storeId}`);
    openHour = h.open_hour;
    closeHour = h.close_hour;
    $("openHour").value = openHour;
    $("closeHour").value = closeHour;
    await loadRequirements();
    await loadCoverage();
  } catch (e) { showStatus(e.message, true); }
}

$("saveHours").addEventListener("click", async () => {
  const o = parseInt($("openHour").value, 10);
  const c = parseInt($("closeHour").value, 10);
  if (!(o < c)) return showStatus("開店時間需早於關店時間", true);
  try {
    const h = await api("PUT", `/api/store-hours?store_id=${storeId}`, { open_hour: o, close_hour: c });
    openHour = h.open_hour;
    closeHour = h.close_hour;
    showStatus(`營業時段已更新:${openHour}:00–${closeHour}:00`);
    await loadRequirements();
    await loadCoverage();
  } catch (e) { showStatus(e.message, true); }
});

// 一格需求外觀:0 = 空白、>0 = 顯示人數並上色。
function applyReqCell(td, n) {
  td.dataset.val = String(n);
  td.className = "paint " + (n > 0 ? "req-set" : "pref-empty");
  td.textContent = n > 0 ? n : "";
}

function renderReqBrushBar() {
  const bar = $("reqBrush");
  bar.innerHTML = "";
  [0, 1, 2, 3].forEach((n) => {
    const btn = document.createElement("button");
    btn.textContent = n === 0 ? "清除" : `${n} 人`;
    btn.dataset.n = n;
    btn.classList.toggle("active", n === reqBrush);
    btn.addEventListener("click", () => {
      reqBrush = n;
      bar.querySelectorAll("button").forEach((x) =>
        x.classList.toggle("active", parseInt(x.dataset.n, 10) === reqBrush));
    });
    bar.appendChild(btn);
  });
}

async function loadRequirements() {
  const reqs = await api("GET", `/api/requirements?store_id=${storeId}`);
  const filled = {};
  reqs.forEach((r) => { filled[`${r.weekday}_${r.hour}`] = r.headcount; });

  renderReqBrushBar();
  reqGrid = createPaintGrid({
    container: $("reqGrid"),
    hours: hoursRange(),
    initCell: (td, wd, hr) => applyReqCell(td, filled[`${wd}_${hr}`] || 0),
    onPaint: (td) => applyReqCell(td, reqBrush),
    onColHeader: (wd) => hoursRange().forEach((hr) => applyReqCell(reqGrid.cellAt(wd, hr), reqBrush)),
    onRowHeader: (hr) => { for (let wd = 0; wd < 7; wd++) applyReqCell(reqGrid.cellAt(wd, hr), reqBrush); },
  });
}

$("saveReq").addEventListener("click", async () => {
  const requirements = [];
  for (const hr of hoursRange())
    for (let wd = 0; wd < 7; wd++) {
      const n = parseInt(reqGrid.cellAt(wd, hr).dataset.val, 10);
      if (n > 0) requirements.push({ weekday: wd, hour: hr, headcount: n });
    }
  try {
    const res = await api("PUT", `/api/requirements?store_id=${storeId}`, { requirements });
    showStatus(`需求已儲存,共 ${res.saved} 個時段。`);
    await loadCoverage();
  } catch (e) { showStatus(e.message, true); }
});

// --- 5. 逐小時缺口 heatmap(需求 vs 可上)---

async function loadCoverage() {
  try {
    const cov = await api("GET", `/api/coverage?store_id=${storeId}`);
    renderCoverage(cov);
  } catch (e) { showStatus(e.message, true); }
}

// 顏色:可上≥需求(含需求 0)→ 綠;有人但不夠 → 黃;需要人卻 0 人可上 → 紅。
function coverageClass(c) {
  if (c.available >= c.required) return "cov-ok";
  if (c.available === 0) return "cov-none";
  return "cov-short";
}

function renderCoverage(cov) {
  // 只畫後端回的格子(有需求或有人可上的時段),依小時列出。
  const byHour = {}; // hr -> {wd -> cell}
  cov.cells.forEach((c) => { (byHour[c.hour] = byHour[c.hour] || {})[c.weekday] = c; });
  const hrs = Object.keys(byHour).map(Number).sort((a, b) => a - b);

  if (hrs.length === 0) {
    $("grid").innerHTML = "<p class='muted'>這間店還沒設需求、也還沒有人填可上時段。先到上方設定營業時段與需求人數。</p>";
    return;
  }

  let html = "<div class='grid'><table class='cov'><thead><tr><th>時段</th>";
  WEEKDAYS.forEach((d) => (html += `<th>${d}</th>`));
  html += "</tr></thead><tbody>";
  hrs.forEach((hr) => {
    const hh = String(hr).padStart(2, "0") + ":00";
    html += `<tr><th>${hh}</th>`;
    for (let wd = 0; wd < 7; wd++) {
      const c = byHour[hr][wd];
      if (!c) { html += "<td></td>"; continue; }
      html += `<td class='${coverageClass(c)}' title='非常想上 ${c.want}・可配合 ${c.ok}'>${c.available}/${c.required}</td>`;
    }
    html += "</tr>";
  });
  html += "</tbody></table></div>";

  if (cov.not_filled && cov.not_filled.length) {
    html += `<p class='muted'>⚠️ 尚未填寫(已是門市成員):${cov.not_filled.map((e) => e.name).join("、")}</p>`;
  } else {
    html += `<p class='muted'>✓ 這間店的成員都提交了(或尚未指派成員)。</p>`;
  }
  $("grid").innerHTML = html;
}

$("refreshCoverage").addEventListener("click", () => { if (storeId) loadCoverage(); });

// --- 啟動:載入既有組織清單,還原上次選的組織 ---

async function init() {
  try {
    await loadOrganizations();
    const savedOrg = localStorage.getItem(LS_ORG);
    const sel = $("orgSelect");
    if (savedOrg && [...sel.options].some((o) => o.value === savedOrg)) {
      sel.value = savedOrg;
      await selectOrg(savedOrg);
    }
  } catch (e) { showStatus(e.message, true); }
}

init();
