<script>
  import { onMount, onDestroy } from 'svelte';
  import { prefs, setPref } from '$lib/stores/theme.js';
  import { getToken } from '$lib/stores/auth.js';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  const CELL = 80;
  const GAP  = 10;
  const MARGIN = 16;

  const WIDGET_TYPES = {
    clock:   { name: 'Reloj',    icon: '🕐', cols: 3, rows: 2 },
    sysmon:  { name: 'Sistema',  icon: '📊', cols: 3, rows: 3 },
    storage: { name: 'Storage',  icon: '💾', cols: 3, rows: 2 },
    network: { name: 'Red',      icon: '🌐', cols: 2, rows: 2 },
  };

  const SIZE_PRESETS = {
    clock:   [{ label: 'Pequeño', cols:2, rows:2 }, { label: 'Normal', cols:3, rows:2 }, { label: 'Grande', cols:4, rows:2 }],
    sysmon:  [{ label: '1×1', cols:2, rows:2 }, { label: '1×2', cols:4, rows:2 }, { label: '2×2', cols:4, rows:4 }],
    storage: [{ label: 'Normal',  cols:3, rows:2 }, { label: 'Grande', cols:4, rows:3 }],
    network: [{ label: '1×1', cols:2, rows:2 }, { label: '1×2', cols:4, rows:2 }],
  };

  const DEFAULT_LAYOUT = [
    { id: 'w1', type: 'clock',   col: 0,  row: 0, cols: 3, rows: 2 },
    { id: 'w2', type: 'sysmon',  col: 0,  row: 2, cols: 4, rows: 4 },
    { id: 'w3', type: 'storage', col: -3, row: 0, cols: 3, rows: 2 },
    { id: 'w4', type: 'network', col: -3, row: 2, cols: 3, rows: 2 },
  ];

  let widgets = [];
  let gridCols = 0;
  let gridRows = 0;
  let layoutLoaded = false;

  // ── Context menu ──
  let activeMenu = null; // { widgetId, x, y, sub: null | 'size' | 'add' }

  function openMenu(e, widgetId, widget) {
    e.stopPropagation();
    if (activeMenu?.widgetId === widgetId) { activeMenu = null; return; }
    // Position menu above the widget, aligned to its right edge
    const wLeft = cellX(widget.col);
    const wTop  = cellY(widget.row);
    const wWidth = cellW(widget.cols);
    const menuW = 210;
    // Default: right-align with widget
    let x = wLeft + wWidth - menuW;
    // If goes off left edge, left-align instead
    if (x < 8) x = wLeft;
    // If goes off right edge, clamp
    if (x + menuW > window.innerWidth - 8) x = window.innerWidth - menuW - 8;
    const y = wTop; // appears above widget
    activeMenu = { widgetId, x, y, sub: null };
  }

  function openAddMenu(e) {
    e.stopPropagation();
    activeMenu = activeMenu?.widgetId === '_add' ? null : { widgetId: '_add', x: e.clientX, y: e.clientY, sub: null };
  }

  function closeMenu() { activeMenu = null; }

  // ── Grid ──
  function getZoom() { return parseFloat(document.documentElement.style.zoom) || 1; }

  function getTbInfo() {
    const tbH = parseInt(getComputedStyle(document.documentElement).getPropertyValue('--taskbar-height')) || 48;
    const tbPos = document.documentElement.getAttribute('data-taskbar-pos') || 'bottom';
    return { tbH, tbPos };
  }

  function computeGrid() {
    const z = getZoom();
    const vw = window.innerWidth / z;
    const vh = window.innerHeight / z;
    const { tbH, tbPos } = getTbInfo();
    let areaW = vw - MARGIN * 2;
    let areaH = vh - tbH - MARGIN * 2;
    if (tbPos === 'left') { areaW = vw - tbH - MARGIN * 2; areaH = vh - MARGIN * 2; }
    gridCols = Math.floor((areaW + GAP) / (CELL + GAP));
    gridRows = Math.floor((areaH + GAP) / (CELL + GAP));
  }

  function resolveLayout(layout) {
    computeGrid();
    return layout.map(w => {
      let col = w.col < 0 ? gridCols + w.col : w.col;
      col = Math.max(0, Math.min(gridCols - w.cols, col));
      const row = Math.max(0, Math.min(gridRows - w.rows, w.row));
      return { ...w, col, row };
    });
  }

  function loadLayout() {
    const saved = $prefs.widgetLayout;
    widgets = resolveLayout(saved?.length ? saved : DEFAULT_LAYOUT);
    layoutLoaded = true;
  }

  function saveLayout() {
    if (!layoutLoaded) return;
    setPref('widgetLayout', widgets.map(({ id, type, col, row, cols, rows }) => ({ id, type, col, row, cols, rows })));
  }

  let prevLayoutJson = '';
  $: {
    const lj = JSON.stringify($prefs.widgetLayout);
    if (lj !== prevLayoutJson && !dragging) {
      prevLayoutJson = lj;
      if (typeof window !== 'undefined') loadLayout();
    }
  }

  onMount(() => { computeGrid(); loadLayout(); startPolling(); window.addEventListener('resize', onResize); });
  onDestroy(() => { stopPolling(); window.removeEventListener('resize', onResize); });

  function onResize() {
    computeGrid();
    widgets = widgets.map(w => ({
      ...w,
      col: Math.max(0, Math.min(gridCols - w.cols, w.col)),
      row: Math.max(0, Math.min(gridRows - w.rows, w.row)),
    }));
  }

  function cellX(col) {
    const { tbH, tbPos } = getTbInfo();
    return (tbPos === 'left' ? tbH : 0) + MARGIN + col * (CELL + GAP);
  }
  function cellY(row) {
    const { tbH, tbPos } = getTbInfo();
    return (tbPos === 'top' ? tbH : 0) + MARGIN + row * (CELL + GAP);
  }
  function cellW(cols) { return cols * CELL + (cols - 1) * GAP; }
  function cellH(rows) { return rows * CELL + (rows - 1) * GAP; }

  // ── Drag ──
  let dragging = null;
  let dragPreviewCol = 0;
  let dragPreviewRow = 0;
  let dragWidget = null;

  function onDragStart(e, widget) {
    if (e.target.closest('.wm-btn')) return;
    e.preventDefault();
    closeMenu();
    const z = getZoom();
    dragging = { id: widget.id, startMx: e.clientX / z, startMy: e.clientY / z, origCol: widget.col, origRow: widget.row };
    dragWidget = widget;
    dragPreviewCol = widget.col;
    dragPreviewRow = widget.row;
    window.addEventListener('mousemove', onDragMove);
    window.addEventListener('mouseup', onDragEnd);
  }

  function onDragMove(e) {
    if (!dragging || !dragWidget) return;
    const z = getZoom();
    const dx = (e.clientX / z) - dragging.startMx;
    const dy = (e.clientY / z) - dragging.startMy;
    dragPreviewCol = Math.max(0, Math.min(gridCols - dragWidget.cols, dragging.origCol + Math.round(dx / (CELL + GAP))));
    dragPreviewRow = Math.max(0, Math.min(gridRows - dragWidget.rows, dragging.origRow + Math.round(dy / (CELL + GAP))));
  }

  function onDragEnd() {
    if (dragging && dragWidget) {
      const idx = widgets.findIndex(w => w.id === dragging.id);
      if (idx >= 0) {
        // Only move if no collision at new position
        if (!hasCollision(dragPreviewCol, dragPreviewRow, dragWidget.cols, dragWidget.rows, dragging.id)) {
          widgets[idx].col = dragPreviewCol;
          widgets[idx].row = dragPreviewRow;
        }
        // else: revert to original position (no change)
        widgets = widgets;
        saveLayout();
      }
    }
    dragging = null; dragWidget = null;
    window.removeEventListener('mousemove', onDragMove);
    window.removeEventListener('mouseup', onDragEnd);
  }

  // ── Widget actions ──
  function removeWidget(id) { widgets = widgets.filter(w => w.id !== id); saveLayout(); closeMenu(); }

  function hasCollision(col, row, cols, rows, excludeId = null) {
    return widgets.some(w => {
      if (w.id === excludeId) return false;
      return col < w.col + w.cols && col + cols > w.col &&
             row < w.row + w.rows && row + rows > w.row;
    });
  }

  function addWidget(type) {
    const meta = WIDGET_TYPES[type];
    const id = 'w' + Date.now();
    let placed = false;
    // Try every grid position
    outer: for (let r = 0; r <= gridRows - meta.rows; r++) {
      for (let c = 0; c <= gridCols - meta.cols; c++) {
        if (!hasCollision(c, r, meta.cols, meta.rows)) {
          widgets = [...widgets, { id, type, col: c, row: r, cols: meta.cols, rows: meta.rows }];
          placed = true;
          break outer;
        }
      }
    }
    // Fallback: place at 0,0 even if overlap
    if (!placed) widgets = [...widgets, { id, type, col: 0, row: 0, cols: meta.cols, rows: meta.rows }];
    saveLayout(); closeMenu();
  }

  function resizeWidget(id, cols, rows) {
    const idx = widgets.findIndex(w => w.id === id);
    if (idx < 0) { closeMenu(); return; }
    const w = widgets[idx];
    // Check if resize causes collision
    if (!hasCollision(w.col, w.row, cols, rows, id)) {
      widgets[idx] = { ...w, cols, rows,
        col: Math.min(w.col, gridCols - cols),
        row: Math.min(w.row, gridRows - rows),
      };
      widgets = widgets;
      saveLayout();
    }
    closeMenu();
  }

  function resetLayout() { widgets = resolveLayout(DEFAULT_LAYOUT); saveLayout(); closeMenu(); }

  // ── Data polling ──
  let pollTimer;
  let sysData = {};
  let storageData = {};
  let netData = {};

  async function fetchData() {
    try {
      const [sys, stor, net] = await Promise.all([
        fetch('/api/system',         { headers: hdrs() }).then(r => r.json()).catch(() => ({})),
        fetch('/api/storage/status', { headers: hdrs() }).then(r => r.json()).catch(() => ({})),
        fetch('/api/network',        { headers: hdrs() }).then(r => r.json()).catch(() => ({})),
      ]);
      sysData = sys || {}; storageData = stor || {}; netData = net || {};
      updateNetCharts();
    } catch {}
  }
  function startPolling() { fetchData(); pollTimer = setInterval(fetchData, 5000); }
  function stopPolling()  { if (pollTimer) clearInterval(pollTimer); }

  // ── Clock ──
  let clockTime = '';
  let clockDate = '';
  function updateClock() {
    const now = new Date();
    clockTime = now.toLocaleTimeString('es-ES', { hour: '2-digit', minute: '2-digit', second: '2-digit' });
    clockDate = now.toLocaleDateString('es-ES', { weekday: 'long', day: 'numeric', month: 'long' });
    if (typeof updateLcdClocks === 'function') updateLcdClocks();
  }
  updateClock();
  const clockInterval = setInterval(updateClock, 1000);
  onDestroy(() => clearInterval(clockInterval));

  function fmtBytes(b) {
    if (!b || b <= 0) return '—';
    if (b >= 1e12) return (b / 1e12).toFixed(1) + ' TB';
    if (b >= 1e9)  return (b / 1e9).toFixed(1)  + ' GB';
    return (b / 1e6).toFixed(0) + ' MB';
  }


  // ── LCD Clock ──
  const LCD_DIGITS = [
    [1,1,1,1,1,1,0], // 0
    [0,1,1,0,0,0,0], // 1
    [1,1,0,1,1,0,1], // 2
    [1,1,1,1,0,0,1], // 3
    [0,1,1,0,0,1,1], // 4
    [1,0,1,1,0,1,1], // 5
    [1,0,1,1,1,1,1], // 6
    [1,1,1,0,0,0,0], // 7
    [1,1,1,1,1,1,1], // 8
    [1,1,1,1,0,1,1], // 9
  ];

  function drawLcdPair(canvas, val) {
    if (!canvas) return;
    const dpr = window.devicePixelRatio || 1;
    // Digit dimensions
    const DW = 32, DH = 56, S = 5, GAP = 8, PAD = 6;
    const cw = PAD*2 + DW*2 + GAP;
    const ch = PAD*2 + DH;
    canvas.width  = cw * dpr;
    canvas.height = ch * dpr;
    canvas.style.width  = cw + 'px';
    canvas.style.height = ch + 'px';
    const ctx = canvas.getContext('2d');
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, cw, ch);

    const theme = document.documentElement.getAttribute('data-theme') || 'midnight';
    const ON  = theme === 'light' ? 'rgba(15,15,15,0.85)'  : 'rgba(240,240,240,0.90)';
    const OFF = theme === 'light' ? 'rgba(0,0,0,0.06)'     : 'rgba(255,255,255,0.06)';

    function seg(ctx, x, y, isOn, type) {
      // type: 'h' horizontal, 'v-top' vertical top half, 'v-bot' vertical bottom half
      ctx.fillStyle = isOn ? ON : OFF;
      ctx.beginPath();
      const r = 2;
      let rx, ry, rw, rh;
      if (type === 'h') { rx=x; ry=y; rw=DW-S*2; rh=S; }
      else              { rx=x; ry=y; rw=S; rh=(DH-S*3)/2; }
      rw = Math.max(rw, 2); rh = Math.max(rh, 2);
      ctx.moveTo(rx+r, ry);
      ctx.lineTo(rx+rw-r, ry); ctx.quadraticCurveTo(rx+rw, ry, rx+rw, ry+r);
      ctx.lineTo(rx+rw, ry+rh-r); ctx.quadraticCurveTo(rx+rw, ry+rh, rx+rw-r, ry+rh);
      ctx.lineTo(rx+r, ry+rh); ctx.quadraticCurveTo(rx, ry+rh, rx, ry+rh-r);
      ctx.lineTo(rx, ry+r); ctx.quadraticCurveTo(rx, ry, rx+r, ry);
      ctx.closePath(); ctx.fill();
    }

    function drawDigit(n, ox, oy) {
      const s = LCD_DIGITS[n] || LCD_DIGITS[0];
      const hh = (DH - S*3) / 2;
      // a top
      seg(ctx, ox+S, oy, s[0], 'h');
      // b top-right
      seg(ctx, ox+DW-S, oy+S, s[1], 'v');
      // c bot-right
      seg(ctx, ox+DW-S, oy+S*2+hh, s[2], 'v');
      // d bottom
      seg(ctx, ox+S, oy+DH-S, s[3], 'h');
      // e bot-left
      seg(ctx, ox, oy+S*2+hh, s[4], 'v');
      // f top-left
      seg(ctx, ox, oy+S, s[5], 'v');
      // g middle
      seg(ctx, ox+S, oy+S+hh, s[6], 'h');
    }

    drawDigit(Math.floor(val/10), PAD, PAD);
    drawDigit(val%10, PAD + DW + GAP, PAD);
  }

  function updateLcdClocks() {
    const now = new Date();
    const DAYS   = ['SUN','MON','TUE','WED','THU','FRI','SAT'];
    const MONTHS = ['JAN','FEB','MAR','APR','MAY','JUN','JUL','AUG','SEP','OCT','NOV','DEC'];
    const todayIdx = now.getDay();

    document.querySelectorAll('.wg-lcd-canvas').forEach(canvas => {
      const role = canvas.dataset.role;
      drawLcdPair(canvas, role === 'hours' ? now.getHours() : now.getMinutes());
    });

    document.querySelectorAll('[id^="wg-days-"]').forEach(el => {
      el.innerHTML = '';
      DAYS.forEach((d, i) => {
        const span = document.createElement('span');
        span.textContent = d;
        span.className = 'wg-day-item' + (i === todayIdx ? ' today' : '');
        el.appendChild(span);
      });
    });

    document.querySelectorAll('[id^="wg-datenum-"]').forEach(el => el.textContent = now.getDate());
    document.querySelectorAll('[id^="wg-datemon-"]').forEach(el => el.textContent = MONTHS[now.getMonth()]);
  }

  // Hook into existing clock interval
  onMount(() => {
    setTimeout(updateLcdClocks, 100);
  });

  // ── Ring gauge helpers ──
  function ringDash(r) { return (2 * Math.PI * r).toFixed(1); }
  function ringOffset(r, pct) { return (2 * Math.PI * r * (1 - Math.min(100, pct) / 100)).toFixed(1); }

  // ── Arc gauge helper ──
  function describeArc(pct, r, cx, cy) {
    // C-shape: starts at 210deg, sweeps 240deg (bottom-left to bottom-right)
    const startAngle = 210;
    const sweepAngle = 240;
    const angle = startAngle + (pct / 100) * sweepAngle;
    const toRad = (d) => (d - 90) * Math.PI / 180;
    const x1 = cx + r * Math.cos(toRad(startAngle));
    const y1 = cy + r * Math.sin(toRad(startAngle));
    const x2 = cx + r * Math.cos(toRad(angle));
    const y2 = cy + r * Math.sin(toRad(angle));
    const largeArc = (angle - startAngle) > 180 ? 1 : 0;
    return `M ${x1} ${y1} A ${r} ${r} 0 ${largeArc} 1 ${x2} ${y2}`;
  }

  function arcColor(pct) {
    if (pct < 60) return '#4ade80';  // green
    if (pct < 80) return '#fbbf24';  // amber
    return '#f87171';                 // red
  }

  function arcBgPath(r, cx, cy) {
    const startAngle = 210;
    const sweepAngle = 240;
    const toRad = (d) => (d - 90) * Math.PI / 180;
    const x1 = cx + r * Math.cos(toRad(startAngle));
    const y1 = cy + r * Math.sin(toRad(startAngle));
    const endAngle = startAngle + sweepAngle;
    const x2 = cx + r * Math.cos(toRad(endAngle));
    const y2 = cy + r * Math.sin(toRad(endAngle));
    return `M ${x1} ${y1} A ${r} ${r} 0 1 1 ${x2} ${y2}`;
  }

  // CPU history for chart
  let cpuHistory = Array.from({length:40}, () => 0);
  let cpuHistoryMax = 40;
  $: {
    if (cpuPct > 0) {
      cpuHistory = [...cpuHistory.slice(-cpuHistoryMax + 1), cpuPct];
    }
  }

  $: chartPts = cpuHistory.map((v,i) => [
    (i / (cpuHistory.length - 1 || 1)) * 300,
    40 - (Math.min(100, v) / 100) * 36 - 2
  ]);
  $: chartLine = chartPts.map((p,i) => (i===0 ? `M ${p[0].toFixed(1)} ${p[1].toFixed(1)}` : `L ${p[0].toFixed(1)} ${p[1].toFixed(1)}`)).join(' ');
  $: chartArea = chartLine + ' L 300 40 L 0 40 Z';

  $: cpuPct   = sysData.cpu?.percent    ?? sysData.cpuPercent ?? 0;
  $: memPct   = sysData.memory?.percent ?? sysData.memPercent ?? 0;
  $: memUsed  = sysData.memory?.used    ?? 0;
  $: memTotal = sysData.memory?.total   ?? 0;
  $: cpuTemp  = sysData.temps?.cpu      ?? sysData.cpuTemp    ?? null;
  $: pools    = storageData.pools       || [];
  $: netIfaces   = Array.isArray(netData) ? netData : (netData.interfaces || []);
  $: primaryIface = netIfaces.find(i => i.ip && i.ip !== '127.0.0.1') || netIfaces[0] || {};

  // Network speed history
  let netRxSpeed = 0, netTxSpeed = 0;
  const NET_HIST = 30;
  let dlHist = Array(NET_HIST).fill(0);
  let ulHist = Array(NET_HIST).fill(0);

  function fmtNetSpeed(bps) {
    if (!bps || bps <= 0) return '0 KB/s';
    const mbps = bps / 1e6;
    if (mbps >= 1) return mbps.toFixed(1) + ' MB/s';
    return (bps / 1e3).toFixed(0) + ' KB/s';
  }

  function updateNetCharts() {
    // rxRate/txRate come directly from daemon in bytes/s
    netRxSpeed = primaryIface?.rxRate || 0;
    netTxSpeed = primaryIface?.txRate || 0;

    dlHist = [...dlHist.slice(-NET_HIST+1), netRxSpeed];
    ulHist = [...ulHist.slice(-NET_HIST+1), netTxSpeed];

    drawNetCharts();
  }

  function drawNetChart(canvas, history, color) {
    if (!canvas) return;
    const dpr = window.devicePixelRatio || 1;
    const W = canvas.parentElement?.offsetWidth || 120;
    const H = canvas.classList.contains('tall') ? 56 : 36;
    canvas.width  = W * dpr;
    canvas.height = H * dpr;
    canvas.style.width  = W + 'px';
    canvas.style.height = H + 'px';
    const ctx = canvas.getContext('2d');
    ctx.scale(dpr, dpr);
    ctx.clearRect(0, 0, W, H);
    const max = Math.max(...history, 1000);
    const pts = history.map((v,i) => [
      (i / (history.length-1)) * W,
      H - (v/max) * (H-3) - 1
    ]);
    const grad = ctx.createLinearGradient(0, 0, 0, H);
    grad.addColorStop(0, color + '44');
    grad.addColorStop(1, color + '08');
    ctx.beginPath();
    pts.forEach(([x,y],i) => i===0 ? ctx.moveTo(x,y) : ctx.lineTo(x,y));
    ctx.lineTo(W,H); ctx.lineTo(0,H); ctx.closePath();
    ctx.fillStyle = grad; ctx.fill();
    ctx.beginPath();
    pts.forEach(([x,y],i) => i===0 ? ctx.moveTo(x,y) : ctx.lineTo(x,y));
    ctx.strokeStyle = color; ctx.lineWidth = 1.5; ctx.lineJoin = 'round'; ctx.stroke();
  }

  function drawNetCharts() {
    document.querySelectorAll('.wg-net-canvas').forEach(canvas => {
      const role = canvas.dataset.role;
      if (role === 'dl') drawNetChart(canvas, dlHist, '#3b82f6');
      else               drawNetChart(canvas, ulHist, '#a855f7');
    });
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="widget-layer" on:click={closeMenu}>

  {#if $prefs.showWidgets}
    {#each widgets as widget (widget.id)}
      {@const isDrag = dragging?.id === widget.id}
      {@const dc = isDrag ? dragPreviewCol : widget.col}
      {@const dr = isDrag ? dragPreviewRow : widget.row}
      {@const menuOpen = activeMenu?.widgetId === widget.id}

      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div
        class="wc"
        class:is-dragging={isDrag}
        class:menu-open={menuOpen}
        style="left:{cellX(dc)}px; top:{cellY(dr)}px; width:{cellW(widget.cols)}px; height:{cellH(widget.rows)}px;"
        on:mousedown={(e) => onDragStart(e, widget)}
      >
        <!-- 3-dot menu button -->
        <button class="wm-btn" on:click={(e) => openMenu(e, widget.id, widget)} title="Opciones">
          <span class="wm-dot"></span>
          <span class="wm-dot"></span>
          <span class="wm-dot"></span>
        </button>

        <!-- Widget content -->
        <div class="wc-body">
          {#if widget.type === 'clock'}
            <div class="wg-lcd-wrap">
              <div class="wg-lcd-left">
                <!-- Hours -->
                <div class="wg-lcd-row">
                  <canvas class="wg-lcd-canvas" data-role="hours" width="90" height="70"></canvas>
                </div>
                <!-- Minutes -->
                <div class="wg-lcd-row">
                  <canvas class="wg-lcd-canvas" data-role="minutes" width="90" height="70"></canvas>
                </div>
              </div>
              <div class="wg-lcd-right">
                <div class="wg-lcd-days" id="wg-days-{widget.id}"></div>
                <div class="wg-lcd-date">
                  <span class="wg-lcd-datenum" id="wg-datenum-{widget.id}"></span>
                  <span class="wg-lcd-datemon" id="wg-datemon-{widget.id}"></span>
                </div>
              </div>
            </div>

          {:else if widget.type === 'sysmon'}
            {@const is1x1 = widget.cols <= 2}
            {@const is1x2 = widget.cols >= 4 && widget.rows <= 2}
            {@const is2x2 = widget.cols >= 4 && widget.rows >= 4}

            {#if is1x1}
              <!-- ── 1×1: double ring CPU outer, RAM inner ── -->
              <div class="wg-double-ring">
                <svg viewBox="0 0 160 160" class="wg-ring-svg">
                  <defs>
                    <linearGradient id="cpu-grad-{widget.id}" x1="0%" y1="0%" x2="100%" y2="100%">
                      <stop offset="0%" stop-color="#f97316"/>
                      <stop offset="100%" stop-color="#ef4444"/>
                    </linearGradient>
                  </defs>
                  <!-- CPU outer -->
                  <circle cx="80" cy="80" r="64" fill="none" class="ring-bg" stroke-width="13"/>
                  <circle cx="80" cy="80" r="64" fill="none" stroke="url(#cpu-grad-{widget.id})" stroke-width="13"
                    stroke-linecap="round" transform="rotate(-90 80 80)"
                    stroke-dasharray={ringDash(64)}
                    stroke-dashoffset={ringOffset(64, cpuPct)}
                    style="transition:stroke-dashoffset .6s ease; stroke:{arcColor(cpuPct)}"/>
                  <!-- RAM inner -->
                  <circle cx="80" cy="80" r="44" fill="none" class="ring-bg" stroke-width="12"/>
                  <circle cx="80" cy="80" r="44" fill="none" stroke="#3b82f6" stroke-width="12"
                    stroke-linecap="round" transform="rotate(-90 80 80)"
                    stroke-dasharray={ringDash(44)}
                    stroke-dashoffset={ringOffset(44, memPct)}
                    style="transition:stroke-dashoffset .6s ease;"/>
                  <!-- CPU % -->
                  <text x="80" y="73" text-anchor="middle" dominant-baseline="middle" class="ring-pct">{cpuPct.toFixed(0)}%</text>
                  <!-- RAM % -->
                  <text x="80" y="91" text-anchor="middle" class="ring-sub">{memPct.toFixed(0)}%</text>
                </svg>
              </div>

            {:else if is1x2}
              <!-- ── 1×2 horizontal: two rings side by side ── -->
              <div class="wg-two-rings">
                <!-- CPU -->
                <div class="wg-ring-wrap">
                  <svg viewBox="0 0 130 130" class="wg-ring-svg-lg">
                    <defs>
                      <linearGradient id="cpu-grad2-{widget.id}" x1="0%" y1="0%" x2="100%" y2="100%">
                        <stop offset="0%" stop-color="#f97316"/>
                        <stop offset="100%" stop-color="#ef4444"/>
                      </linearGradient>
                    </defs>
                    <circle cx="65" cy="65" r="52" fill="none" class="ring-bg" stroke-width="12"/>
                    <circle cx="65" cy="65" r="52" fill="none" stroke="url(#cpu-grad2-{widget.id})" stroke-width="12"
                      stroke-linecap="round" transform="rotate(-90 65 65)"
                      stroke-dasharray={ringDash(52)}
                      stroke-dashoffset={ringOffset(52, cpuPct)}
                      style="transition:stroke-dashoffset .6s ease; stroke:{arcColor(cpuPct)}"/>
                    <text x="65" y="60" text-anchor="middle" dominant-baseline="middle" class="ring-pct">{cpuPct.toFixed(0)}%</text>
                    <text x="65" y="77" text-anchor="middle" class="ring-label" style="fill:{arcColor(cpuPct)}">CPU</text>
                  </svg>
                </div>
                <div class="wg-ring-divider"></div>
                <!-- RAM -->
                <div class="wg-ring-wrap">
                  <svg viewBox="0 0 130 130" class="wg-ring-svg-lg">
                    <circle cx="65" cy="65" r="52" fill="none" class="ring-bg" stroke-width="12"/>
                    <circle cx="65" cy="65" r="52" fill="none" stroke="#3b82f6" stroke-width="12"
                      stroke-linecap="round" transform="rotate(-90 65 65)"
                      stroke-dasharray={ringDash(52)}
                      stroke-dashoffset={ringOffset(52, memPct)}
                      style="transition:stroke-dashoffset .6s ease;"/>
                    <text x="65" y="60" text-anchor="middle" dominant-baseline="middle" class="ring-pct">{memPct.toFixed(0)}%</text>
                    <text x="65" y="77" text-anchor="middle" class="ring-label" style="fill:#3b82f6">RAM</text>
                  </svg>
                </div>
              </div>

            {:else}
              <!-- ── 2×2: rings + info + chart ── -->
              <div class="wg-header">System Resources</div>
              <div class="wg-two-rings" style="flex:1">
                <div class="wg-ring-wrap">
                  <svg viewBox="0 0 112 112" class="wg-ring-svg-lg">
                    <defs>
                      <linearGradient id="cpu-grad3-{widget.id}" x1="0%" y1="0%" x2="100%" y2="100%">
                        <stop offset="0%" stop-color="#f97316"/>
                        <stop offset="100%" stop-color="#ef4444"/>
                      </linearGradient>
                    </defs>
                    <circle cx="56" cy="56" r="44" fill="none" class="ring-bg" stroke-width="9"/>
                    <circle cx="56" cy="56" r="44" fill="none" stroke="url(#cpu-grad3-{widget.id})" stroke-width="9"
                      stroke-linecap="round" transform="rotate(-90 56 56)"
                      stroke-dasharray={ringDash(44)}
                      stroke-dashoffset={ringOffset(44, cpuPct)}
                      style="transition:stroke-dashoffset .6s ease; stroke:{arcColor(cpuPct)}"/>
                    <text x="56" y="51" text-anchor="middle" dominant-baseline="middle" class="ring-pct">{cpuPct.toFixed(0)}%</text>
                    <text x="56" y="66" text-anchor="middle" class="ring-label" style="fill:{arcColor(cpuPct)}">CPU</text>
                  </svg>
                  <div class="wg-ring-info">
                    <div class="wg-kv"><span>Cores</span><span>8</span></div>
                    <div class="wg-kv"><span>Load</span><span>{(cpuPct/100*8*0.9).toFixed(2)}</span></div>
                  </div>
                </div>
                <div class="wg-ring-divider"></div>
                <div class="wg-ring-wrap">
                  <svg viewBox="0 0 112 112" class="wg-ring-svg-lg">
                    <circle cx="56" cy="56" r="44" fill="none" class="ring-bg" stroke-width="9"/>
                    <circle cx="56" cy="56" r="44" fill="none" stroke="#3b82f6" stroke-width="9"
                      stroke-linecap="round" transform="rotate(-90 56 56)"
                      stroke-dasharray={ringDash(44)}
                      stroke-dashoffset={ringOffset(44, memPct)}
                      style="transition:stroke-dashoffset .6s ease;"/>
                    <text x="56" y="51" text-anchor="middle" dominant-baseline="middle" class="ring-pct">{memPct.toFixed(0)}%</text>
                    <text x="56" y="66" text-anchor="middle" class="ring-label" style="fill:#3b82f6">RAM</text>
                  </svg>
                  <div class="wg-ring-info">
                    <div class="wg-kv"><span>Used</span><span>{fmtBytes(memUsed)}</span></div>
                    <div class="wg-kv"><span>Total</span><span>{fmtBytes(memTotal)}</span></div>
                  </div>
                </div>
              </div>
              <!-- Chart -->
              <div class="wg-chart-wrap">
                <div class="wg-chart-label">CPU Activity</div>
                <svg class="wg-chart-svg" viewBox="0 0 300 40" preserveAspectRatio="none">
                  <defs>
                    <linearGradient id="chart-grad-{widget.id}" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stop-color="#f97316" stop-opacity="0.45"/>
                      <stop offset="100%" stop-color="#f97316" stop-opacity="0"/>
                    </linearGradient>
                  </defs>
                  <path d={chartArea} fill="url(#chart-grad-{widget.id})"/>
                  <path d={chartLine} fill="none" stroke="#f97316" stroke-width="1.5" stroke-linejoin="round"/>
                </svg>
              </div>
            {/if}

          {:else if widget.type === 'storage'}
            <div class="wg-header">Storage</div>
            {#if pools.length > 0}
              {#each pools.slice(0,2) as pool}
                <div class="wg-bar-row">
                  <span class="wg-label">{pool.name}</span>
                  <div class="wg-track"><div class="wg-fill sto" style="width:{pool.usagePercent||0}%"></div></div>
                  <span class="wg-val">{pool.usagePercent||0}%</span>
                </div>
                <div class="wg-sub">{pool.usedFormatted||'—'} / {pool.totalFormatted||'—'}</div>
              {/each}
            {:else}
              <div class="wg-empty">Sin pools configurados</div>
            {/if}

          {:else if widget.type === 'network'}
            {@const is1x2net = widget.cols >= 4}
            <div class="wg-header">Red</div>
            <div class="wg-net-wrap">
              <!-- Download -->
              <div class="wg-net-row">
                <div class="wg-net-label">
                  <span class="wg-net-arrow dl">↓</span>
                  <span class="wg-net-val">{fmtNetSpeed(netRxSpeed)}</span>
                </div>
                <canvas class="wg-net-canvas" data-netid="{widget.id}" data-role="dl" class:tall={is1x2net}></canvas>
              </div>
              <!-- Upload -->
              <div class="wg-net-row">
                <div class="wg-net-label">
                  <span class="wg-net-arrow ul">↑</span>
                  <span class="wg-net-val">{fmtNetSpeed(netTxSpeed)}</span>
                </div>
                <canvas class="wg-net-canvas" data-netid="{widget.id}" data-role="ul" class:tall={is1x2net}></canvas>
              </div>
            </div>
          {/if}
        </div>

      </div>

      <!-- Drag ghost — sibling of widget, not child -->
      {#if isDrag}
        <div class="snap-ghost"
          style="left:{cellX(dragPreviewCol)}px; top:{cellY(dragPreviewRow)}px; width:{cellW(widget.cols)}px; height:{cellH(widget.rows)}px;">
        </div>
      {/if}

      <!-- Context menu for this widget -->
      {#if menuOpen}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="ctx-menu" style="left:{activeMenu.x}px; top:{activeMenu.y}px; transform:translateY(-100%) translateY(-8px);"
          on:click|stopPropagation>

          {#if activeMenu.sub === 'size'}
            <!-- Size submenu -->
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="ctx-back" on:click={() => activeMenu = { ...activeMenu, sub: null }}>
              ‹ Volver
            </div>
            <div class="ctx-divider"></div>
            {#each SIZE_PRESETS[widget.type] || [] as preset}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="ctx-item"
                class:active={widget.cols === preset.cols && widget.rows === preset.rows}
                on:click={() => resizeWidget(widget.id, preset.cols, preset.rows)}>
                <span class="ctx-ico">◻</span>
                {preset.label}
                <span class="ctx-size-hint">{preset.cols}×{preset.rows}</span>
              </div>
            {/each}

          {:else if activeMenu.sub === 'add'}
            <!-- Add widget submenu -->
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="ctx-back" on:click={() => activeMenu = { ...activeMenu, sub: null }}>
              ‹ Volver
            </div>
            <div class="ctx-divider"></div>
            {#each Object.entries(WIDGET_TYPES) as [type, meta]}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="ctx-item" on:click={() => addWidget(type)}>
                <span class="ctx-ico">{meta.icon}</span>
                {meta.name}
              </div>
            {/each}

          {:else}
            <!-- Main menu -->
            <div class="ctx-header">
              <span>{WIDGET_TYPES[widget.type]?.icon}</span>
              <span>{WIDGET_TYPES[widget.type]?.name}</span>
            </div>
            <div class="ctx-divider"></div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="ctx-item" on:click={() => activeMenu = { ...activeMenu, sub: 'add' }}>
              <span class="ctx-ico">＋</span> Añadir widget ›
            </div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="ctx-item" on:click={() => activeMenu = { ...activeMenu, sub: 'size' }}>
              <span class="ctx-ico">◻</span> Cambiar tamaño ›
            </div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="ctx-item" on:click={resetLayout}>
              <span class="ctx-ico">⊞</span> Restablecer grid
            </div>
            <div class="ctx-divider"></div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="ctx-item danger" on:click={() => removeWidget(widget.id)}>
              <span class="ctx-ico">✕</span> Eliminar
            </div>
          {/if}
        </div>
      {/if}
    {/each}

    <!-- Floating add button -->
    <div class="wa-wrap">
      <button class="wa-btn" on:click={openAddMenu} title="Añadir widget">
        <svg width="14" height="14" viewBox="0 0 14 14" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M7 1v12M1 7h12"/>
        </svg>
      </button>

      {#if activeMenu?.widgetId === '_add'}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="wa-menu" on:click|stopPropagation>
          <div class="ctx-header-label">Añadir widget</div>
          {#each Object.entries(WIDGET_TYPES) as [type, meta]}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="wa-item" on:click={() => addWidget(type)}>
              <span class="wa-ico">{meta.icon}</span>
              <div class="wa-item-info">
                <span class="wa-item-name">{meta.name}</span>
              </div>
            </div>
          {/each}
        </div>
      {/if}
    </div>
  {/if}
</div>

<style>
  .widget-layer {
    position: fixed; inset: 0; z-index: 1;
    pointer-events: none;
  }

  /* ── WIDGET CARD ── */
  .wc {
    position: absolute; pointer-events: auto;
    border-radius: 14px; overflow: visible;
    background: var(--widget-bg, rgba(17,16,40,0.52));
    backdrop-filter: blur(20px) saturate(1.4);
    -webkit-backdrop-filter: blur(20px) saturate(1.4);
    border: 1px solid var(--border);
    box-shadow: 0 4px 24px rgba(0,0,0,0.18);
    cursor: grab; user-select: none;
    transition: left .22s cubic-bezier(.25,1,.5,1), top .22s cubic-bezier(.25,1,.5,1), box-shadow .2s, border-color .2s;
  }
  .wc:hover { border-color: var(--border-hi); box-shadow: 0 8px 32px rgba(0,0,0,0.25); }
  .wc.is-dragging { cursor: grabbing; opacity: .88; transition: none; z-index: 10; box-shadow: 0 16px 48px rgba(0,0,0,0.40); }
  .wc.menu-open { border-color: var(--border-hi); }

  /* Theme variants */
  :global([data-theme="dark"]) .wc { background: rgba(22,22,22,0.58); }
  :global([data-theme="light"]) .wc {
    background: rgba(240,240,244,0.72);
    border-color: var(--border);
    box-shadow: 0 4px 20px rgba(0,0,0,0.08);
  }

  /* ── 3-DOT BUTTON ── */
  .wm-btn {
    position: absolute; top: 8px; right: 8px;
    width: 22px; height: 22px; border-radius: 6px;
    border: none; background: transparent;
    display: flex; flex-direction: column; align-items: center; justify-content: center; gap: 2.5px;
    cursor: pointer; opacity: 0; transition: opacity .15s, background .15s;
    z-index: 5; padding: 0;
  }
  .wc:hover .wm-btn, .wc.menu-open .wm-btn { opacity: 1; }
  .wm-btn:hover { background: var(--ibtn-bg); }
  .wm-dot {
    width: 3px; height: 3px; border-radius: 50%;
    background: var(--text-2); flex-shrink: 0;
  }

  /* ── WIDGET BODY ── */
  .wc-body {
    width: 100%; height: 100%;
    padding: 12px 14px;
    display: flex; flex-direction: column;
    overflow: hidden;
  }

  .wg-header {
    font-size: 9px; font-weight: 600; color: var(--text-3);
    text-transform: uppercase; letter-spacing: .08em;
    margin-bottom: 10px; flex-shrink: 0;
  }

  /* Clock */
  .wg-clock {
    display: flex; flex-direction: column;
    align-items: center; justify-content: center;
    height: 100%; gap: 4px;
  }
  .wg-clock-t {
    font-size: 30px; font-weight: 300; letter-spacing: -.02em;
    color: var(--text-1); font-family: 'DM Mono', monospace; line-height: 1;
  }
  .wg-clock-d {
    font-size: 10px; color: var(--text-3); text-transform: capitalize;
    font-family: 'DM Sans', sans-serif;
  }

  /* Bars */
  .wg-bars { display: flex; flex-direction: column; gap: 9px; flex: 1; }
  .wg-bar-row { display: flex; align-items: center; gap: 8px; }
  .wg-label { font-size: 10px; font-weight: 600; color: var(--text-2); width: 30px; flex-shrink: 0; }
  .wg-track {
    flex: 1; height: 5px; border-radius: 3px;
    background: var(--ibtn-bg); overflow: hidden;
  }
  .wg-fill { height: 100%; border-radius: 3px; transition: width .6s ease; }
  .wg-fill.cpu { background: linear-gradient(90deg, var(--accent), var(--accent2, #c054f0)); }
  .wg-fill.ram { background: linear-gradient(90deg, #60a5fa, #818cf8); }
  .wg-fill.sto { background: linear-gradient(90deg, #4ade80, #22d3ee); }
  .wg-val { font-size: 10px; color: var(--text-2); font-family: 'DM Mono', monospace; width: 32px; text-align: right; flex-shrink: 0; }

  .wg-footer {
    display: flex; gap: 12px; margin-top: auto;
    padding-top: 8px; border-top: 1px solid var(--border);
    font-size: 10px; color: var(--text-3); font-family: 'DM Mono', monospace;
    flex-shrink: 0;
  }

  .wg-sub { font-size: 9px; color: var(--text-3); font-family: 'DM Mono', monospace; margin: 1px 0 4px 38px; }
  .wg-empty { font-size: 11px; color: var(--text-3); flex: 1; display: flex; align-items: center; }

  /* KV */
  .wg-kv {
    display: flex; justify-content: space-between; align-items: center;
    padding: 5px 0; border-bottom: 1px solid var(--border);
  }
  .wg-kv:last-child { border-bottom: none; }
  .wg-kv span:first-child { font-size: 10px; color: var(--text-3); }
  .wg-kv span:last-child  { font-size: 11px; color: var(--text-1); font-family: 'DM Mono', monospace; }

  /* Drag ghost */
  .snap-ghost {
    position: fixed; border-radius: 14px;
    border: 2px dashed var(--accent);
    background: rgba(124,111,255,0.06);
    pointer-events: none; z-index: 0;
    transition: left .12s ease, top .12s ease;
  }

  /* ── CONTEXT MENU ── */
  .ctx-menu {
    position: fixed; z-index: 9999;
    min-width: 200px;
    background: var(--bg-inner);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: 0 16px 40px rgba(0,0,0,0.4), 0 2px 8px rgba(0,0,0,0.2);
    overflow: hidden;
    animation: ctxIn .12s cubic-bezier(0.16,1,0.3,1) both;
    pointer-events: auto;
    /* Position set via JS — no transform needed */
  }
  @keyframes ctxIn {
    from { opacity: 0; transform: scale(.97); }
    to   { opacity: 1; transform: scale(1); }
  }
  .ctx-header {
    display: flex; align-items: center; gap: 8px;
    padding: 10px 12px 8px;
    font-size: 12px; font-weight: 600; color: var(--text-1);
  }
  .ctx-header-label {
    padding: 10px 12px 6px;
    font-size: 9px; font-weight: 600; color: var(--text-3);
    text-transform: uppercase; letter-spacing: .08em;
  }
  .ctx-divider { height: 1px; background: var(--border); }
  .ctx-item {
    display: flex; align-items: center; gap: 8px;
    padding: 8px 12px; font-size: 12px; color: var(--text-2);
    cursor: pointer; transition: all .1s;
  }
  .ctx-item:hover { background: var(--active-bg); color: var(--text-1); }
  .ctx-item.active { color: var(--accent); }
  .ctx-item.danger { color: var(--red); }
  .ctx-item.danger:hover { background: rgba(248,113,113,0.10); }
  .ctx-ico { font-size: 11px; width: 14px; text-align: center; color: var(--text-3); flex-shrink: 0; }
  .ctx-item.danger .ctx-ico { color: var(--red); }
  .ctx-size-hint { margin-left: auto; font-size: 9px; color: var(--text-3); font-family: 'DM Mono', monospace; }
  .ctx-back {
    display: flex; align-items: center; gap: 6px;
    padding: 8px 12px; font-size: 11px; color: var(--accent);
    cursor: pointer; transition: opacity .15s;
  }
  .ctx-back:hover { opacity: .7; }

  /* ── NETWORK WIDGET ── */
  .wg-net-wrap {
    display: flex; flex-direction: column;
    gap: 8px; flex: 1; width: 100%;
  }
  .wg-net-wrap.horizontal {
    flex-direction: row; gap: 12px;
  }
  .wg-net-row {
    display: flex; flex-direction: column; gap: 3px; flex: 1;
  }
  .wg-net-label {
    display: flex; justify-content: space-between; align-items: baseline;
  }
  .wg-net-arrow {
    font-size: 11px; font-weight: 700;
  }
  .wg-net-arrow.dl { color: #3b82f6; }
  .wg-net-arrow.ul { color: #a855f7; }
  .wg-net-val {
    font-size: 11px; color: var(--text-1);
    font-family: 'DM Mono', monospace;
  }
  .wg-net-canvas { display: block; width: 100%; border-radius: 4px; }

  /* ── LCD CLOCK ── */
  .wg-lcd-wrap {
    width: 100%; height: 100%;
    display: flex; align-items: center; justify-content: center;
    gap: 8px; padding: 4px;
  }
  .wg-lcd-left {
    display: flex; flex-direction: column;
    align-items: center; justify-content: center;
    gap: 4px; flex: 1;
  }
  .wg-lcd-row { display: flex; align-items: center; justify-content: center; }
  .wg-lcd-canvas { display: block; }
  .wg-lcd-right {
    display: flex; flex-direction: column;
    align-items: center; justify-content: space-between;
    height: 100%; padding: 6px 0;
  }
  .wg-lcd-days {
    display: flex; flex-direction: column;
    align-items: center; gap: 2px;
  }
  :global(.wg-day-item) {
    font-size: 8px; font-family: 'DM Sans', sans-serif;
    letter-spacing: .05em; color: var(--text-3);
    padding: 1px 5px; border-radius: 10px;
    border: 1px solid transparent;
    transition: all .2s;
  }
  :global(.wg-day-item.today) {
    color: var(--text-1); font-weight: 600;
    background: var(--ibtn-bg);
    border-color: var(--border-hi);
  }
  .wg-lcd-date {
    display: flex; flex-direction: column;
    align-items: center; gap: 1px;
  }
  .wg-lcd-datenum {
    font-size: 18px; font-weight: 500;
    color: var(--text-1); font-family: 'DM Sans', sans-serif;
    line-height: 1;
  }
  .wg-lcd-datemon {
    font-size: 8px; color: var(--text-3);
    font-family: 'DM Sans', sans-serif;
    letter-spacing: .06em; text-transform: uppercase;
  }

  /* ── RING GAUGES ── */
  .ring-bg { stroke: var(--ibtn-bg); }
  :global([data-theme="light"]) .ring-bg { stroke: rgba(0,0,0,0.08); }

  .ring-pct {
    font-size: 17px; font-weight: 500;
    fill: var(--text-1); font-family: 'DM Sans', sans-serif;
  }
  .ring-sub {
    font-size: 12px; fill: var(--text-3);
    font-family: 'DM Sans', sans-serif;
  }
  .ring-label {
    font-size: 9px; font-weight: 600;
    text-transform: uppercase; letter-spacing: .08em;
    font-family: 'DM Sans', sans-serif;
  }

  /* 1×1 double ring */
  .wg-double-ring {
    width: 100%; height: 100%;
    display: flex; align-items: center; justify-content: center;
  }
  .wg-ring-svg { width: 100%; max-width: 140px; }

  /* 1×2 and 2×2 side by side */
  .wg-two-rings {
    display: flex; align-items: center;
    justify-content: space-around; gap: 8px;
    width: 100%; overflow: hidden;
  }
  .wg-ring-wrap {
    display: flex; flex-direction: column;
    align-items: center; justify-content: center; gap: 4px; flex: 1; min-width: 0;
  }
  .wg-ring-svg-lg { width: 100%; max-width: 130px; }
  .wg-ring-divider { width: 1px; height: 80px; background: var(--border); flex-shrink: 0; }
  .wg-ring-info { display: flex; flex-direction: column; gap: 4px; width: 100%; }

  /* 2×2 chart */
  .wg-chart-wrap {
    border-top: 1px solid var(--border);
    padding-top: 8px; flex-shrink: 0;
  }
  .wg-chart-label {
    font-size: 9px; font-weight: 600; color: var(--text-3);
    text-transform: uppercase; letter-spacing: .06em; margin-bottom: 5px;
  }
  .wg-chart-svg { width: 100%; height: 42px; display: block; }

  /* ── ARC GAUGES (legacy) ── */
  .wg-gauges {
    display: flex; gap: 8px; flex: 1;
    align-items: center; justify-content: center;
  }
  .wg-gauge { flex: 1; display: flex; align-items: center; justify-content: center; }
  .wg-arc-svg { width: 100%; max-width: 110px; overflow: visible; }
  .arc-bg { stroke: var(--ibtn-bg); }
  :global([data-theme="light"]) .arc-bg { stroke: rgba(0,0,0,0.08); }
  .arc-pct {
    font-size: 18px; font-weight: 700;
    fill: var(--text-1);
    font-family: 'DM Mono', monospace;
  }
  .arc-label {
    font-size: 9px; font-weight: 600;
    fill: var(--text-3);
    text-transform: uppercase; letter-spacing: .06em;
    font-family: 'DM Sans', sans-serif;
  }
  .arc-temp {
    font-size: 8px; fill: var(--text-3);
    font-family: 'DM Mono', monospace;
  }
  .wa-wrap {
    position: fixed; bottom: 68px; right: 16px;
    z-index: 2; pointer-events: auto;
  }
  .wa-btn {
    width: 34px; height: 34px; border-radius: 50%;
    border: 1px solid var(--border);
    background: var(--bg-inner);
    backdrop-filter: blur(12px); -webkit-backdrop-filter: blur(12px);
    color: var(--text-2); cursor: pointer;
    display: flex; align-items: center; justify-content: center;
    transition: all .15s; box-shadow: 0 4px 12px rgba(0,0,0,0.2);
  }
  .wa-btn:hover { background: var(--active-bg); color: var(--text-1); border-color: var(--border-hi); }

  .wa-menu {
    position: absolute; bottom: 42px; right: 0;
    min-width: 180px;
    background: var(--bg-inner); border: 1px solid var(--border);
    border-radius: 10px; overflow: hidden;
    box-shadow: 0 12px 36px rgba(0,0,0,0.35);
    animation: ctxIn .12s ease both;
  }
  .wa-item {
    display: flex; align-items: center; gap: 10px;
    padding: 9px 14px; cursor: pointer;
    border: none; background: none; width: 100%;
    font-family: inherit; transition: all .1s; text-align: left;
    color: var(--text-2);
  }
  .wa-item:hover { background: var(--active-bg); color: var(--text-1); }
  .wa-ico { font-size: 16px; flex-shrink: 0; }
  .wa-item-name { font-size: 12px; font-weight: 500; color: var(--text-1); }
</style>
