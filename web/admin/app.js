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
let templates = [];

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

const hhmm = (t) => (t || "").slice(0, 5); // "06:00:00" -> "06:00"

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
  $("templateSection").hidden = !show;
  $("gridSection").hidden = !show;
  if (show) loadTemplates();
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
    const btn = document.createElement("button");
    btn.textContent = "發填班連結";
    btn.addEventListener("click", () => makeLink(e.id, e.name));
    li.append(span, btn);
    ul.appendChild(li);
  });
}

// makeLink 為某員工 + 目前選定門市產生 magic-link,顯示完整連結供複製。
async function makeLink(empId, empName) {
  if (!storeId) return showStatus("請先在上方「門市」選一間店(連結會綁定該店班別)", true);
  try {
    const res = await api("POST", "/api/access-links", { employee_id: empId, store_id: storeId });
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

// --- 4. 班別模板 ---

async function loadTemplates() {
  templates = await api("GET", `/api/shift-templates?store_id=${storeId}`);
  const tb = $("templateTable").querySelector("tbody");
  tb.innerHTML = "";
  templates.forEach((t) => {
    const tr = document.createElement("tr");
    tr.innerHTML =
      `<td>${t.name}</td>` +
      `<td>${hhmm(t.start_local)}</td>` +
      `<td>${hhmm(t.end_local)}</td>` +
      `<td><input type="number" min="0" value="${t.required_headcount}" data-id="${t.id}" style="width:4em"></td>` +
      `<td><button data-save="${t.id}">儲存</button></td>`;
    tb.appendChild(tr);
  });
  tb.querySelectorAll("button[data-save]").forEach((btn) =>
    btn.addEventListener("click", () => saveTemplate(btn.getAttribute("data-save")))
  );
  await loadCoverage(); // 需求人數改了會影響缺口,重抓一次
}

async function saveTemplate(id) {
  const t = templates.find((x) => x.id === id);
  const headcount = parseInt(document.querySelector(`input[data-id="${id}"]`).value, 10);
  try {
    await api("PUT", `/api/shift-templates/${id}`, {
      name: t.name,
      start_local: hhmm(t.start_local),
      end_local: hhmm(t.end_local),
      required_headcount: headcount,
      required_skills: t.required_skills,
    });
    showStatus("已更新:" + t.name);
    await loadTemplates();
  } catch (e) { showStatus(e.message, true); }
}

// --- 5. 週缺口 heatmap(需求 vs 可上)---

async function loadCoverage() {
  try {
    const cov = await api("GET", `/api/coverage?store_id=${storeId}`);
    renderCoverage(cov);
  } catch (e) { showStatus(e.message, true); }
}

// 顏色:需求 0 或 可上≥需求 → 綠;有人但不夠 → 黃;0 人可上 → 紅。
function coverageClass(c) {
  if (c.required === 0 || c.available >= c.required) return "cov-ok";
  if (c.available === 0) return "cov-none";
  return "cov-short";
}

function renderCoverage(cov) {
  const rows = [...cov.templates].sort((a, b) => a.start_local.localeCompare(b.start_local));
  const idx = {};
  cov.cells.forEach((c) => { idx[`${c.shift_template_id}_${c.weekday}`] = c; });

  let html = "<table class='grid cov'><thead><tr><th>班別</th>";
  WEEKDAYS.forEach((d) => (html += `<th>${d}</th>`));
  html += "</tr></thead><tbody>";
  rows.forEach((t) => {
    html += `<tr><th>${t.name}<br><small>${hhmm(t.start_local)}-${hhmm(t.end_local)}</small></th>`;
    for (let wd = 0; wd < 7; wd++) {
      const c = idx[`${t.id}_${wd}`] || { required: t.required_headcount, available: 0, want: 0, ok: 0 };
      html += `<td class='${coverageClass(c)}' title='非常想上 ${c.want}・可配合 ${c.ok}'>${c.available}/${c.required}</td>`;
    }
    html += "</tr>";
  });
  html += "</tbody></table>";

  // 未填名單(發了連結但一格都沒填)
  if (cov.not_filled && cov.not_filled.length) {
    html += `<p class='muted'>⚠️ 尚未填寫(已發連結):${cov.not_filled.map((e) => e.name).join("、")}</p>`;
  } else {
    html += `<p class='muted'>✓ 已發連結的員工都填了(或尚未發連結)。</p>`;
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
