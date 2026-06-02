// 員工看自己班表頁(v2)。token 從 /s/{token} 取出 → /api/me 選門市 → /api/my-schedule。
// 點自己被排到的格子可 POST /api/my-schedule/issues 回報問題(簡化雙階段)。
"use strict";

const $ = (id) => document.getElementById(id);
const WEEKDAYS = ["週日", "週一", "週二", "週三", "週四", "週五", "週六"];

const token = decodeURIComponent(location.pathname.split("/s/")[1] || "");

let stores = [];
let currentStoreId = null;
let ctx = null; // 目前門市的 /api/my-schedule 回應

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

const key = (wd, hr) => wd * 100 + hr;

function showFatal(msg) {
  $("loading").hidden = true;
  $("pickerSection").hidden = true;
  $("schedSection").hidden = true;
  const f = $("fatal");
  f.className = "status error";
  f.textContent = msg;
  f.hidden = false;
}

async function load() {
  if (!token) return showFatal("連結不完整,請向店長索取新的連結。");
  try {
    const me = await api("GET", `/api/me?token=${encodeURIComponent(token)}`);
    $("empNameTop").textContent = me.employee.name;
    stores = me.stores || [];
    $("loading").hidden = true;
    if (stores.length === 0) return showFatal("你還沒被指派任何門市,請聯絡店長。");
    renderStorePicker();
    $("pickerSection").hidden = false;
    if (stores.length === 1) selectStore(stores[0].id);
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
    ctx = await api("GET", `/api/my-schedule?token=${encodeURIComponent(token)}&store_id=${storeId}`);
    $("storeName").textContent = ctx.store.name;
    $("schedSection").hidden = false;
    $("noPublish").hidden = ctx.published;
    renderGrid();
    renderConfirmBar();
  } catch (e) { showStatus(e.message, true); }
}

// 兩階段:顯示我的確認狀態 + 截止,提供「接受整週」按鈕(已鎖定/已確認則不顯示)。
function renderConfirmBar() {
  const bar = $("confirmBar");
  bar.innerHTML = "";
  if (!ctx.published) return;
  const status = ctx.my_status;
  const label = document.createElement("span");
  label.className = "muted";
  if (ctx.locked) {
    label.textContent = "🔒 班表已定案。";
  } else if (status === "confirmed") {
    label.textContent = "✅ 你已確認這週班表。";
  } else if (status === "declined") {
    label.textContent = "⚠️ 你已回報問題(視為回絕),店長會處理。";
  } else {
    label.textContent = "請確認你的班表" + (ctx.deadline ? `(建議於 ${new Date(ctx.deadline).toLocaleString()} 前)` : "") + ":";
  }
  bar.appendChild(label);

  // 未鎖定、且尚未確認 → 給「接受整週」按鈕。
  if (!ctx.locked && status !== "confirmed") {
    const btn = document.createElement("button");
    btn.textContent = "✅ 接受整週班表";
    btn.style.marginLeft = "0.5rem";
    btn.addEventListener("click", confirmWeek);
    bar.appendChild(btn);
  }
}

async function confirmWeek() {
  try {
    await api("POST", `/api/my-schedule/confirm?token=${encodeURIComponent(token)}&store_id=${currentStoreId}`);
    ctx.my_status = "confirmed";
    renderConfirmBar();
    showStatus("已確認,感謝!");
  } catch (e) { showStatus(e.message, true); }
}

function renderGrid() {
  const assigned = new Set((ctx.assignments || []).map((c) => key(c.weekday, c.hour)));
  const issues = new Map((ctx.issues || []).map((c) => [key(c.weekday, c.hour), c.note || ""]));

  const table = document.createElement("table");
  table.className = "grid";
  const thead = document.createElement("thead");
  const htr = document.createElement("tr");
  htr.innerHTML = "<th>時段</th>" + WEEKDAYS.map((d) => `<th>${d}</th>`).join("");
  thead.appendChild(htr);
  table.appendChild(thead);

  const tbody = document.createElement("tbody");
  for (let hr = ctx.open_hour; hr < ctx.close_hour; hr++) {
    const tr = document.createElement("tr");
    const th = document.createElement("th");
    th.textContent = String(hr).padStart(2, "0") + ":00";
    tr.appendChild(th);
    for (let wd = 0; wd < 7; wd++) {
      const td = document.createElement("td");
      const k = key(wd, hr);
      if (assigned.has(k)) {
        const hasIssue = issues.has(k);
        td.className = "my-cell " + (hasIssue ? "my-issue" : "my-on");
        td.textContent = hasIssue ? "⚠" : "✓";
        if (hasIssue && issues.get(k)) td.title = issues.get(k);
        td.style.cursor = "pointer";
        td.addEventListener("click", () => flagIssue(wd, hr));
      }
      tr.appendChild(td);
    }
    tbody.appendChild(tr);
  }
  table.appendChild(tbody);
  $("schedGrid").innerHTML = "";
  $("schedGrid").appendChild(table);
}

async function flagIssue(wd, hr) {
  if (ctx.locked) return showStatus("班表已定案,無法回報", true);
  const note = prompt(`回報 ${WEEKDAYS[wd]} ${String(hr).padStart(2, "0")}:00 的問題(= 回絕這格,可留空理由):`);
  if (note === null) return; // 取消
  try {
    await api("POST", `/api/my-schedule/issues?token=${encodeURIComponent(token)}&store_id=${currentStoreId}`,
      { weekday: wd, hour: hr, note });
    // 本地標記,免重抓
    ctx.issues = (ctx.issues || []).filter((c) => !(c.weekday === wd && c.hour === hr));
    ctx.issues.push({ weekday: wd, hour: hr, note });
    ctx.my_status = "declined"; // 回報 = 回絕
    renderGrid();
    renderConfirmBar();
    showStatus("已回報(視為回絕),店長會看到");
  } catch (e) { showStatus(e.message, true); }
}

load();
