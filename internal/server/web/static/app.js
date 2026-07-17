/* html-site 后台 · 前端交互（纯原生，无依赖）
   功能：主题切换 / 侧边栏折叠 / Toast / 确认弹窗 / 复制 / 表格多选批量 /
        拖拽上传 / HTML 在线编辑实时预览 / SVG 图表渲染 */
(function () {
  "use strict";

  /* ---------- 图标小工具：把 <svg class="ic"> 自动带上 use ---------- */
  function icon(name, cls) {
    return '<svg class="' + (cls || "ic") + '"><use href="/static/icons.svg#icon-' + name + '"/></svg>';
  }

  /* ---------- 主题 ---------- */
  var THEME_KEY = "hs-theme";
  function applyTheme(t) {
    document.documentElement.setAttribute("data-theme", t);
    localStorage.setItem(THEME_KEY, t);
    var btn = document.querySelector("[data-theme-toggle]");
    if (btn) btn.innerHTML = icon(t === "dark" ? "sun" : "moon");
  }
  function initTheme() {
    var saved = localStorage.getItem(THEME_KEY);
    if (!saved) saved = window.matchMedia && window.matchMedia("(prefers-color-scheme: dark)").matches ? "dark" : "light";
    applyTheme(saved);
    document.addEventListener("click", function (e) {
      var t = e.target.closest("[data-theme-toggle]");
      if (!t) return;
      var cur = document.documentElement.getAttribute("data-theme") || "light";
      applyTheme(cur === "dark" ? "light" : "dark");
    });
  }

  /* ---------- 侧边栏（移动端折叠） ---------- */
  function initSidebar() {
    document.addEventListener("click", function (e) {
      var t = e.target.closest("[data-menu-toggle]");
      if (!t) return;
      var sb = document.querySelector(".sidebar");
      if (sb) sb.classList.toggle("open");
    });
    // 点遮罩关闭
    document.addEventListener("click", function (e) {
      if (e.target.classList && e.target.classList.contains("sidebar")) {
        e.target.classList.remove("open");
      }
    });
  }

  /* ---------- Toast ---------- */
  function ensureToastWrap() {
    var w = document.getElementById("toast-wrap");
    if (!w) { w = document.createElement("div"); w.id = "toast-wrap"; document.body.appendChild(w); }
    return w;
  }
  function toast(title, msg, kind, sticky) {
    var w = ensureToastWrap();
    var el = document.createElement("div");
    el.className = "toast " + (kind || "");
    el.innerHTML = '<div class="t-title">' + escapeHtml(title || "") + "</div>" +
      (msg ? '<div class="t-msg">' + escapeHtml(msg) + "</div>" : "");
    w.appendChild(el);
    if (!sticky) setTimeout(function () { removeToast(el); }, 3600);
    return el;
  }
  function removeToast(el) {
    if (!el || !el.parentNode) return;
    el.classList.add("fade");
    setTimeout(function () { if (el.parentNode) el.parentNode.removeChild(el); }, 300);
  }

  /* ---------- 确认弹窗（替代原生 confirm） ---------- */
  function confirmDialog(opts) {
    return new Promise(function (resolve) {
      var mask = document.createElement("div");
      mask.className = "modal-mask show";
      var danger = opts.danger ? " btn-danger" : " btn-ghost";
      var cancelCls = opts.danger ? "btn btn-ghost" : "btn btn-ghost";
      mask.innerHTML =
        '<div class="modal">' +
        '<div class="modal-head">' + escapeHtml(opts.title || "确认操作") + "</div>" +
        '<div class="modal-body">' + escapeHtml(opts.body || "确定要执行此操作吗？") + "</div>" +
        '<div class="modal-foot">' +
        '<button class="' + cancelCls + '" data-act="cancel">取消</button>' +
        '<button class="btn ' + (opts.danger ? "btn-danger" : "") + '" data-act="ok">' + escapeHtml(opts.okText || "确定") + "</button>" +
        "</div></div>";
      document.body.appendChild(mask);
      mask.addEventListener("click", function (e) {
        var act = e.target.getAttribute("data-act");
        if (act === "ok") { cleanup(); resolve(true); }
        else if (act === "cancel" || e.target === mask) { cleanup(); resolve(false); }
      });
      function cleanup() { document.body.removeChild(mask); }
    });
  }

  /* ---------- 拦截危险提交，走自定义确认 ---------- */
  function initConfirmForms() {
    document.addEventListener("submit", function (e) {
      var f = e.target;
      if (f.tagName !== "FORM" || !f.hasAttribute("data-confirm")) return;
      if (f.getAttribute("data-confirmed") === "1") return; // 已确认，放行
      e.preventDefault();
      var msg = f.getAttribute("data-confirm") || "确定要执行此操作吗？";
      var danger = f.hasAttribute("data-danger");
      confirmDialog({ title: danger ? "危险操作" : "请确认", body: msg, danger: danger, okText: danger ? "确认删除" : "确定" })
        .then(function (ok) {
          if (ok) { f.setAttribute("data-confirmed", "1"); f.submit(); }
        });
    });
  }

  /* ---------- 复制按钮 ---------- */
  function initCopy() {
    document.addEventListener("click", function (e) {
      var btn = e.target.closest("[data-copy]");
      if (!btn) return;
      var val = btn.getAttribute("data-copy");
      copyText(val).then(function () {
        toast("已复制", val.length > 48 ? val.slice(0, 48) + "…" : val, "ok");
      });
    });
  }
  function copyText(t) {
    if (navigator.clipboard && navigator.clipboard.writeText) return navigator.clipboard.writeText(t);
    return new Promise(function (resolve) {
      var ta = document.createElement("textarea");
      ta.value = t; document.body.appendChild(ta); ta.select();
      try { document.execCommand("copy"); } catch (e) {}
      document.body.removeChild(ta); resolve();
    });
  }

  /* ---------- 表格多选 + 批量栏 ---------- */
  function initTableSelect() {
    var table = document.querySelector("[data-select-table]");
    if (!table) return;
    var form = table.closest("form");
    var bar = document.querySelector("[data-batch-bar]");
    var all = table.querySelector("[data-check-all]");
    var boxes = table.querySelectorAll('input[type=checkbox][data-check-row]');
    var selCountEl = bar ? bar.querySelector("[data-sel-count]") : null;

    function checked() { return Array.prototype.filter.call(boxes, function (b) { return b.checked; }); }
    function updateBar() {
      var n = checked().length;
      if (bar) bar.classList.toggle("show", n > 0);
      if (selCountEl) selCountEl.textContent = n;
    }
    if (all) all.addEventListener("change", function () {
      Array.prototype.forEach.call(boxes, function (b) { b.checked = all.checked; });
      updateBar();
    });
    Array.prototype.forEach.call(boxes, function (b) { b.addEventListener("change", updateBar); });
    updateBar();

    // 批量表单提交：把表格中选中的 id 注入到正在提交的表单
    document.addEventListener("submit", function (e) {
      var f = e.target;
      if (!f.hasAttribute("data-batch-form")) return;
      // 移除上次注入的 ids
      f.querySelectorAll('input[name=ids]').forEach(function (el) { el.remove(); });
      checked().forEach(function (b) {
        var h = document.createElement("input");
        h.type = "hidden"; h.name = "ids"; h.value = b.value;
        f.appendChild(h);
      });
      if (checked().length === 0) { e.preventDefault(); toast("未选择", "请先勾选要操作的页面", "err"); }
    }, true);
  }

  /* ---------- 拖拽上传 ---------- */
  function initDropzone() {
    var dz = document.querySelector("[data-dropzone]");
    if (!dz) return;
    var input = document.querySelector("[data-drop-input]");
    var nameEl = document.querySelector("[data-drop-name]");
    function setFile(file) {
      if (!file) return;
      var dt = new DataTransfer(); dt.items.add(file);
      if (input) { input.files = dt.files; }
      if (nameEl) nameEl.textContent = file.name + " · " + humanSize(file.size);
      dz.classList.add("drag");
    }
    dz.addEventListener("click", function () { if (input) input.click(); });
    if (input) input.addEventListener("change", function () { if (input.files[0]) setFile(input.files[0]); });
    ["dragenter", "dragover"].forEach(function (ev) {
      dz.addEventListener(ev, function (e) { e.preventDefault(); dz.classList.add("drag"); });
    });
    ["dragleave", "drop"].forEach(function (ev) {
      dz.addEventListener(ev, function (e) { e.preventDefault(); if (ev === "dragleave") dz.classList.remove("drag"); });
    });
    dz.addEventListener("drop", function (e) {
      var f = e.dataTransfer.files[0];
      if (f) setFile(f);
    });
  }

  /* ---------- HTML 在线编辑 + 实时预览 ---------- */
  function initEditor() {
    var src = document.getElementById("htmlSrc");
    if (!src) return;
    var frame = document.getElementById("previewFrame");
    var timer;
    function render() {
      if (frame && frame.contentWindow) {
        var doc = frame.contentWindow.document;
        doc.open(); doc.write(src.value || ""); doc.close();
      }
    }
    src.addEventListener("input", function () { clearTimeout(timer); timer = setTimeout(render, 300); });
    render();
    // textarea 支持 Tab 缩进
    src.addEventListener("keydown", function (e) {
      if (e.key === "Tab") {
        e.preventDefault();
        var s = src.selectionStart, en = src.selectionEnd;
        src.value = src.value.slice(0, s) + "  " + src.value.slice(en);
        src.selectionStart = src.selectionEnd = s + 2;
        render();
      }
    });
  }

  /* ---------- SVG 图表渲染 ---------- */
  var Chart = {
    // 折线/面积图：data=[{label, pv, uv}]
    line: function (el, data, opt) {
      opt = opt || {};
      var w = opt.w || 640, h = opt.h || 220, pad = { l: 36, r: 14, t: 16, b: 26 };
      var iw = w - pad.l - pad.r, ih = h - pad.t - pad.b;
      var max = 1;
      data.forEach(function (d) { ["pv", "uv"].forEach(function (k) { if (+d[k] > max) max = +d[k]; }); });
      var n = data.length;
      var x = function (i) { return pad.l + (n <= 1 ? iw / 2 : (iw * i) / (n - 1)); };
      var y = function (v) { return pad.t + ih - (ih * (+v)) / max; };
      var mkPath = function (key) {
        return data.map(function (d, i) { return (i ? "L" : "M") + x(i).toFixed(1) + " " + y(d[key]).toFixed(1); }).join(" ");
      };
      // 网格线
      var grid = "";
      for (var g = 0; g <= 4; g++) {
        var gy = pad.t + (ih * g) / 4;
        grid += '<line class="grid-line" x1="' + pad.l + '" y1="' + gy.toFixed(1) + '" x2="' + (w - pad.r) + '" y2="' + gy.toFixed(1) + '"/>';
      }
      // x 轴标签
      var labels = data.map(function (d, i) {
        if (n > 10 && i % Math.ceil(n / 8) !== 0 && i !== n - 1) return "";
        return '<text class="axis-label" x="' + x(i).toFixed(1) + '" y="' + (h - 8) + '" text-anchor="middle">' + d.label.slice(5) + "</text>";
      }).join("");
      var areaPath = mkPath("pv") + " L " + x(n - 1).toFixed(1) + " " + (pad.t + ih) + " L " + x(0).toFixed(1) + " " + (pad.t + ih) + " Z";
      var dots = data.map(function (d, i) {
        return '<circle cx="' + x(i).toFixed(1) + '" cy="' + y(d.pv).toFixed(1) + '" r="3" fill="var(--primary)"><title>' + d.label + " · PV " + d.pv + "</title></circle>";
      }).join("");
      el.innerHTML =
        '<svg class="svg-chart" viewBox="0 0 ' + w + " " + h + '">' +
        '<defs><linearGradient id="areaGrad" x1="0" y1="0" x2="0" y2="1">' +
        '<stop offset="0%" stop-color="var(--primary)" stop-opacity="0.28"/>' +
        '<stop offset="100%" stop-color="var(--primary)" stop-opacity="0"/></linearGradient></defs>' +
        grid +
        '<path d="' + areaPath + '" fill="url(#areaGrad)"/>' +
        '<path d="' + mkPath("uv") + '" fill="none" stroke="var(--accent)" stroke-width="2" stroke-dasharray="4 3"/>' +
        '<path d="' + mkPath("pv") + '" fill="none" stroke="var(--primary)" stroke-width="2.5"/>' +
        dots + labels +
        "</svg>";
    },
    // 环形图：seg=[{label,val,color}]
    donut: function (el, segs, opt) {
      opt = opt || {};
      var total = segs.reduce(function (s, d) { return s + +d.val; }, 0) || 1;
      var size = opt.size || 160, sw = opt.stroke || 20, r = (size - sw) / 2, cx = size / 2;
      var circ = 2 * Math.PI * r;
      var offset = 0;
      var rings = segs.map(function (d) {
        var frac = (+d.val) / total;
        var dash = (frac * circ).toFixed(2);
        var ring = '<circle cx="' + cx + '" cy="' + cx + '" r="' + r + '" fill="none" stroke="' + d.color +
          '" stroke-width="' + sw + '" stroke-dasharray="' + dash + " " + (circ - dash).toFixed(2) +
          '" stroke-dashoffset="' + (-offset).toFixed(2) + '" transform="rotate(-90 ' + cx + " " + cx + ')">' +
          "<title>" + d.label + ": " + d.val + "</title></circle>";
        offset += frac * circ;
        return ring;
      }).join("");
      el.innerHTML =
        '<svg class="svg-chart" viewBox="0 0 ' + size + " " + size + '" style="max-width:' + size + 'px;margin:0 auto">' +
        '<circle cx="' + cx + '" cy="' + cx + '" r="' + r + '" fill="none" stroke="var(--bg-2)" stroke-width="' + sw + '"/>' +
        rings +
        '<text class="donut-center" x="' + cx + '" y="' + (cx - 2) + '" text-anchor="middle" font-size="24" font-weight="700">' + total + "</text>" +
        '<text class="axis-label" x="' + cx + '" y="' + (cx + 16) + '" text-anchor="middle">总计</text>' +
        "</svg>";
    }
  };
  // 把容器上的 data-chart 数据渲染出来
  function initCharts() {
    document.querySelectorAll("[data-chart-line]").forEach(function (el) {
      try { Chart.line(el, JSON.parse(el.getAttribute("data-chart-line"))); } catch (e) {}
    });
    document.querySelectorAll("[data-chart-donut]").forEach(function (el) {
      try { Chart.donut(el, JSON.parse(el.getAttribute("data-chart-donut"))); } catch (e) {}
    });
  }

  /* ---------- 工具 ---------- */
  function escapeHtml(s) {
    return String(s).replace(/[&<>"']/g, function (c) {
      return { "&": "&amp;", "<": "&lt;", ">": "&gt;", '"': "&quot;", "'": "&#39;" }[c];
    });
  }
  function humanSize(n) {
    if (n < 1024) return n + " B";
    var u = ["KB", "MB", "GB"], i = -1; do { n /= 1024; i++; } while (n >= 1024 && i < u.length - 1);
    return n.toFixed(1) + " " + u[i];
  }

  /* ---------- flash -> toast（页面加载后把服务端 flash 转成 toast） ---------- */
  function flashToToast() {
    document.querySelectorAll("[data-flash]").forEach(function (el) {
      var kind = el.getAttribute("data-flash");
      toast(kind === "ok" ? "操作成功" : "提示", el.textContent.trim(), kind, kind !== "ok");
      el.style.display = "none";
    });
  }

  /* ---------- boot ---------- */
  /* ---------- 通用 toggle（data-toggle="选择器" 切换元素显隐） ---------- */
  function initToggle() {
    document.addEventListener("click", function (e) {
      var t = e.target.closest("[data-toggle]");
      if (!t) return;
      e.preventDefault();
      var sel = t.getAttribute("data-toggle");
      var target = document.querySelector(sel);
      if (!target) return;
      var hidden = target.style.display === "none";
      target.style.display = hidden ? "" : "none";
      // 显示时自动聚焦第一个输入框
      if (hidden) {
        var inp = target.querySelector("input[type=text], input:not([type])");
        if (inp) inp.focus();
      }
    });
  }

  document.addEventListener("DOMContentLoaded", function () {
    initTheme();
    initSidebar();
    initConfirmForms();
    initCopy();
    initTableSelect();
    initDropzone();
    initEditor();
    initCharts();
    initToggle();
    setTimeout(flashToToast, 100);
  });

  // 暴露给模板内联脚本用
  window.HS = { toast: toast, confirm: confirmDialog, icon: icon };
})();
