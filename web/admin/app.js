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

// v2:排班。schedData 是 GET /api/schedule 的整包;schedEmpId 是目前編排哪位員工。
let schedData = null;
let schedEmpId = null;
let schedGrid = null;
let schedAvail = new Set();   // 目前員工在該店的可上格 key 集合
let assignBrush = 1;          // 排班筆刷:1=指派 / 0=取消

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
  if (!res.ok) {
    const err = new Error((data && data.error) || ("HTTP " + res.status));
    err.body = data; // 讓呼叫端能讀 4xx 的額外欄位(如 publish 409 的 validation)
    throw err;
  }
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
  $("scheduleSection").hidden = !show;
  if (show) { loadStoreSettings(); loadSchedule(); }
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
  // 一律畫「營業時段內的完整網格」,後端回的格子(有需求或有人可上)疊上去。
  // 這樣即使還沒設需求,員工填的「可上人數(供給)」也會即時顯示——對齊 when2meet 供給先行。
  const idx = {}; // key(wd,hr) -> cell
  cov.cells.forEach((c) => { idx[c.weekday * 100 + c.hour] = c; });

  let html = "<div class='grid'><table class='cov'><thead><tr><th>時段</th>";
  WEEKDAYS.forEach((d) => (html += `<th>${d}</th>`));
  html += "</tr></thead><tbody>";
  for (let hr = cov.open_hour; hr < cov.close_hour; hr++) {
    const hh = String(hr).padStart(2, "0") + ":00";
    html += `<tr><th>${hh}</th>`;
    for (let wd = 0; wd < 7; wd++) {
      const c = idx[wd * 100 + hr];
      if (!c) { html += "<td></td>"; continue; } // 無需求也無人可上 → 留白
      // 有需求 → 顯示「可上/需求」並依缺口上色;沒需求只顯示供給人數(藍,供給先行)。
      const label = c.required > 0 ? `${c.available}/${c.required}` : `${c.available}`;
      const cls = c.required > 0 ? coverageClass(c) : (c.available > 0 ? "supply" : "");
      html += `<td class='${cls}' title='非常想上 ${c.want}・可配合 ${c.ok}'>${label}</td>`;
    }
    html += "</tr>";
  }
  html += "</tbody></table></div>";

  if (cov.not_filled && cov.not_filled.length) {
    html += `<p class='muted'>⚠️ 尚未填寫(已是門市成員):${cov.not_filled.map((e) => e.name).join("、")}</p>`;
  } else {
    html += `<p class='muted'>✓ 這間店的成員都提交了(或尚未指派成員)。</p>`;
  }
  $("grid").innerHTML = html;
}

$("refreshCoverage").addEventListener("click", () => { if (storeId) loadCoverage(); });

// 切回這個分頁時自動重抓缺口:員工在別的裝置/分頁填完,老闆切回來就看到更新,免手動按。
window.addEventListener("focus", () => { if (storeId) loadCoverage(); });

// --- 6. 排班(v2:逐小時指派 + Rule Engine + 發布)---

const skey = (wd, hr) => wd * 100 + hr;

// 載入排班草稿:員工下拉、需求/指派、驗證、問題標記。
async function loadSchedule() {
  try {
    schedData = await api("GET", `/api/schedule?store_id=${storeId}`);
    renderEmpSelect();
    renderAssignBrushBar();
    renderSchedGrid();
    renderValidation();
    renderConfirmations();
    renderIssues();
  } catch (e) { showStatus(e.message, true); }
}

function renderEmpSelect() {
  const sel = $("schedEmp");
  sel.innerHTML = '<option value="">-- 選要排的員工 --</option>';
  schedData.employees.forEach((e) => {
    const o = document.createElement("option");
    o.value = e.id;
    o.textContent = `${e.name}(上限 ${e.max_weekly_hours}h)`;
    sel.appendChild(o);
  });
  if (schedEmpId && schedData.employees.some((e) => e.id === schedEmpId)) sel.value = schedEmpId;
  else schedEmpId = null;
}

function renderAssignBrushBar() {
  const bar = $("assignBrush");
  bar.innerHTML = "";
  [{ v: 1, t: "指派" }, { v: 0, t: "取消" }].forEach((b) => {
    const btn = document.createElement("button");
    btn.textContent = b.t;
    btn.dataset.v = b.v;
    btn.classList.toggle("active", b.v === assignBrush);
    btn.addEventListener("click", () => {
      assignBrush = b.v;
      bar.querySelectorAll("button").forEach((x) =>
        x.classList.toggle("active", parseInt(x.dataset.v, 10) === assignBrush));
    });
    bar.appendChild(btn);
  });
}

// 已排人數(全員,來自最近一次載入;存檔後刷新)。
function assignedCounts() {
  const m = {};
  (schedData.assignments || []).forEach((a) => { m[skey(a.weekday, a.hour)] = (m[skey(a.weekday, a.hour)] || 0) + 1; });
  return m;
}
function requiredMap() {
  const m = {};
  (schedData.requirements || []).forEach((r) => { m[skey(r.weekday, r.hour)] = r.headcount; });
  return m;
}

function applyAssignCell(td, wd, hr, counts, reqs) {
  const key = skey(wd, hr);
  const assigned = td.dataset.assigned === "1";
  const avail = schedAvail.has(key);
  const req = reqs[key] || 0;
  const cnt = counts[key] || 0;
  td.className = "paint" + (assigned ? " assign-on" : "") + (!avail && schedEmpId ? " assign-warn" : "");
  td.textContent = assigned ? "✓" : (req > 0 ? `${cnt}/${req}` : "");
}

function renderSchedGrid() {
  const hours = [];
  for (let hr = schedData.open_hour; hr < schedData.close_hour; hr++) hours.push(hr);
  const counts = assignedCounts();
  const reqs = requiredMap();
  const empAssigned = new Set(
    (schedData.assignments || []).filter((a) => a.employee_id === schedEmpId).map((a) => skey(a.weekday, a.hour)));

  schedGrid = createPaintGrid({
    container: $("schedGrid"),
    hours,
    initCell: (td, wd, hr) => {
      td.dataset.assigned = empAssigned.has(skey(wd, hr)) ? "1" : "0";
      applyAssignCell(td, wd, hr, counts, reqs);
    },
    onPaint: (td, wd, hr) => {
      if (!schedEmpId) return;
      td.dataset.assigned = assignBrush === 1 ? "1" : "0";
      applyAssignCell(td, wd, hr, counts, reqs);
    },
    onColHeader: (wd) => hours.forEach((hr) => schedEmpId && paintAssign(wd, hr, counts, reqs)),
    onRowHeader: (hr) => { for (let wd = 0; wd < 7; wd++) schedEmpId && paintAssign(wd, hr, counts, reqs); },
  });
}
function paintAssign(wd, hr, counts, reqs) {
  const td = schedGrid.cellAt(wd, hr);
  td.dataset.assigned = assignBrush === 1 ? "1" : "0";
  applyAssignCell(td, wd, hr, counts, reqs);
}

async function selectSchedEmp(id) {
  schedEmpId = id || null;
  schedAvail = new Set();
  if (schedEmpId) {
    try {
      const slots = await api("GET", `/api/employee-availability?store_id=${storeId}&employee_id=${schedEmpId}`);
      slots.forEach((s) => schedAvail.add(skey(s.weekday, s.hour)));
    } catch (e) { showStatus(e.message, true); }
  }
  renderSchedGrid();
}
$("schedEmp").addEventListener("change", (e) => selectSchedEmp(e.target.value));

$("saveAssign").addEventListener("click", async () => {
  if (!schedEmpId) return showStatus("請先選一位員工", true);
  const slots = [];
  for (let hr = schedData.open_hour; hr < schedData.close_hour; hr++)
    for (let wd = 0; wd < 7; wd++)
      if (schedGrid.cellAt(wd, hr).dataset.assigned === "1") slots.push({ weekday: wd, hour: hr });
  try {
    const res = await api("PUT", `/api/schedule/assignments?store_id=${storeId}`, { employee_id: schedEmpId, slots });
    schedData.assignments = res.assignments;
    schedData.validation = res.validation;
    renderSchedGrid();
    renderValidation();
    showStatus(`已存 ${slots.length} 格班`);
  } catch (e) { showStatus(e.message, true); }
});

$("autofillSched").addEventListener("click", async () => {
  if (!confirm("會用系統建議覆蓋整張草稿(你目前手動排的會被取代)。\n建議只排「員工有標可上」的格,保證無硬衝突,排不滿的留缺口。\n確定要產生建議排班?")) return;
  try {
    const res = await api("POST", `/api/schedule/autofill?store_id=${storeId}`);
    showStatus(`已產生建議排班,共排 ${res.suggested} 格(可再微調後發布)`);
    schedEmpId = null;          // 重排後清掉選取,讓老闆重新挑人檢視
    await loadSchedule();
  } catch (e) { showStatus(e.message, true); }
});

$("publishSched").addEventListener("click", async () => {
  try {
    const res = await api("POST", `/api/schedule/publish?store_id=${storeId}`);
    schedData.validation = res.validation;
    renderValidation();
    showStatus("班表已發布 ✅");
    await loadSchedule(); // 發布後會開新 draft(複製自剛發布版)
  } catch (e) {
    // 409:有硬衝突,後端回 validation;標出來。
    if (e.body && e.body.validation) { schedData.validation = e.body.validation; renderValidation(); }
    showStatus(e.message, true);
  }
});

$("exportSched").addEventListener("click", () => {
  window.open(`/api/schedule/export?store_id=${storeId}`, "_blank");
});

function renderValidation() {
  const v = schedData.validation || { hard: [], soft: [], understaffed: [], publishable: true };
  let html = "";
  if (v.hard.length === 0 && v.soft.length === 0 && v.understaffed.length === 0) {
    html = "<span class='ok-text'>✓ 無衝突、無缺口</span>";
  } else {
    if (v.hard.length) html += `<p class='vio-hard'>🔴 硬衝突 ${v.hard.length}(擋發布):` +
      v.hard.map((x) => `${x.employee_name} ${WEEKDAYS[x.weekday]}${x.hour}:00 ${x.message}`).join("；") + "</p>";
    if (v.understaffed.length) html += `<p class='vio-soft'>🟡 缺口 ${v.understaffed.length}:` +
      v.understaffed.map((u) => `${WEEKDAYS[u.weekday]}${u.hour}:00 (${u.assigned}/${u.required})`).join("、") + "</p>";
    if (v.soft.length) html += `<p class='vio-soft'>🟡 軟警告 ${v.soft.length}:` +
      v.soft.map((x) => x.message).join("；") + "</p>";
  }
  html += `<p class='muted'>發布狀態:${v.publishable ? "可發布" : "有硬衝突,不可發布"}</p>`;
  $("validation").innerHTML = html;
}

// 兩階段確認面板:顯示已發布版狀態 + 倒數 + 每位員工確認狀態。
function renderConfirmations() {
  const box = $("confirmPanel");
  const pub = schedData.published;
  if (!pub) { box.innerHTML = "<p class='muted'>尚未發布;發布後員工才會在自己班表頁收到「確認」。</p>"; return; }
  const confs = schedData.confirmations || [];
  const ok = confs.filter((c) => c.status === "confirmed").length;
  const no = confs.filter((c) => c.status === "declined").length;
  const pend = confs.length - ok - no;
  const locked = pub.status === "locked";

  let head = `已發布版本:<strong>${locked ? "🔒 已鎖定(定案)" : "確認中"}</strong>`;
  if (!locked && pub.confirm_deadline) head += ` · 截止 ${new Date(pub.confirm_deadline).toLocaleString()}`;
  head += ` · ✅ ${ok} / ⚠️ ${no} / ⏳ ${pend}`;
  const icon = (s) => (s === "confirmed" ? "✅" : s === "declined" ? "⚠️" : "⏳");
  let html = `<p class='muted'>${head}</p>`;
  if (confs.length) {
    html += "<ul class='list'>" + confs.map((c) =>
      `<li>${icon(c.status)} ${c.employee_name}${c.status === "declined" && c.reason ? `(回絕:${c.reason})` : ""}</li>`).join("") + "</ul>";
  }
  box.innerHTML = html;
}

$("lockSched").addEventListener("click", async () => {
  const confs = (schedData && schedData.confirmations) || [];
  if (!schedData.published) return showStatus("還沒發布,無法鎖定", true);
  const pending = confs.filter((c) => c.status !== "confirmed").length;
  let msg = "鎖定後此版定案、不可再改(要改需重新發布新版)。";
  if (pending > 0) msg += `\n⚠️ 還有 ${pending} 人未確認或已回絕,仍要鎖定?`;
  if (!confirm(msg)) return;
  try {
    await api("POST", `/api/schedule/lock?store_id=${storeId}`);
    showStatus("已鎖定班表 🔒");
    await loadSchedule();
  } catch (e) { showStatus(e.message, true); }
});

function renderIssues() {
  const issues = (schedData && schedData.issues) || [];
  if (!issues.length) { $("issues").innerHTML = "<p class='muted'>目前已發布班表沒有員工回報問題。</p>"; return; }
  $("issues").innerHTML = "<p class='muted'>⚠️ 員工回報(已發布班表):" +
    issues.map((i) => `${i.employee_name} ${WEEKDAYS[i.weekday]}${i.hour}:00${i.note ? "(" + i.note + ")" : ""}`).join("、") + "</p>";
}

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
