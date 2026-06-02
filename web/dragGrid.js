// 共用的「拖曳塗選」網格元件(when2meet 式)。員工填班與老闆設需求都重用它。
// 列 = 小時、欄 = 星期;按住拖過一段格子,就用呼叫端目前的「筆刷」塗上去。
// 用 Pointer Events 同時支援滑鼠與觸控;靠 elementFromPoint 抓拖到哪一格,
// 不受 touch 隱式指標捕捉影響。格子的 CSS 需設 touch-action:none 以免拖曳時頁面捲動。
"use strict";

const WEEKDAY_LABELS = ["週日", "週一", "週二", "週三", "週四", "週五", "週六"];

// createPaintGrid 建立網格並接好拖曳塗選。回傳 { cellAt(wd, hr) }。
// opts:
//   container    掛載點(會被清空)
//   hours        要顯示的小時陣列(列),例如 [9,10,...,21]
//   hourLabel    (hr) => 列標題字串(預設 "HH:00")
//   initCell     (td, wd, hr) 初始化每格外觀(呼叫端設 dataset.val / class / text)
//   onPaint      (td, wd, hr) 把目前筆刷塗到該格(呼叫端讀自己的 brush 狀態)
//   onColHeader  (wd) 點某天表頭時(整欄套用筆刷),可省略
//   onRowHeader  (hr) 點某小時列頭時(整列套用筆刷),可省略
function createPaintGrid(opts) {
  const hourLabel = opts.hourLabel || ((hr) => String(hr).padStart(2, "0") + ":00");
  const cells = {}; // key "wd_hr" -> td
  const key = (wd, hr) => `${wd}_${hr}`;

  const table = document.createElement("table");
  table.className = "grid paint-grid";

  // 表頭:左上角空白 + 7 天
  const thead = document.createElement("thead");
  const htr = document.createElement("tr");
  htr.appendChild(document.createElement("th")); // 角落
  for (let wd = 0; wd < 7; wd++) {
    const th = document.createElement("th");
    th.textContent = WEEKDAY_LABELS[wd];
    if (opts.onColHeader) {
      th.className = "hdr-click";
      th.addEventListener("click", () => opts.onColHeader(wd));
    }
    htr.appendChild(th);
  }
  thead.appendChild(htr);
  table.appendChild(thead);

  // 內容:每小時一列
  const tbody = document.createElement("tbody");
  opts.hours.forEach((hr) => {
    const tr = document.createElement("tr");
    const rh = document.createElement("th");
    rh.textContent = hourLabel(hr);
    if (opts.onRowHeader) {
      rh.className = "hdr-click";
      rh.addEventListener("click", () => opts.onRowHeader(hr));
    }
    tr.appendChild(rh);
    for (let wd = 0; wd < 7; wd++) {
      const td = document.createElement("td");
      td.className = "paint";
      td.dataset.wd = wd;
      td.dataset.hr = hr;
      opts.initCell(td, wd, hr);
      cells[key(wd, hr)] = td;
      tr.appendChild(td);
    }
    tbody.appendChild(tr);
  });
  table.appendChild(tbody);

  opts.container.innerHTML = "";
  opts.container.appendChild(table);

  // --- 拖曳塗選 ---
  let painting = false;
  const paintAt = (x, y) => {
    const el = document.elementFromPoint(x, y);
    const td = el && el.closest && el.closest("td.paint");
    if (td && opts.container.contains(td)) {
      opts.onPaint(td, parseInt(td.dataset.wd, 10), parseInt(td.dataset.hr, 10));
    }
  };

  table.addEventListener("pointerdown", (e) => {
    const td = e.target.closest && e.target.closest("td.paint");
    if (!td) return;
    e.preventDefault();
    // 釋放隱式指標捕捉,讓 pointermove 能持續抓到游標下的其他格子。
    if (td.releasePointerCapture && td.hasPointerCapture && td.hasPointerCapture(e.pointerId)) {
      td.releasePointerCapture(e.pointerId);
    }
    painting = true;
    paintAt(e.clientX, e.clientY);
  });
  document.addEventListener("pointermove", (e) => {
    if (!painting) return;
    e.preventDefault();
    paintAt(e.clientX, e.clientY);
  });
  const stop = () => { painting = false; };
  document.addEventListener("pointerup", stop);
  document.addEventListener("pointercancel", stop);

  return {
    cellAt: (wd, hr) => cells[key(wd, hr)] || null,
  };
}

window.createPaintGrid = createPaintGrid;
window.WEEKDAY_LABELS = WEEKDAY_LABELS;
