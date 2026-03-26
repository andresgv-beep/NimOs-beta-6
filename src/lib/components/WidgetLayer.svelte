<script>
  import { onMount, onDestroy } from 'svelte';
  import { prefs, setPref } from '$lib/stores/theme.js';
  import { getToken } from '$lib/stores/auth.js';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  // ── Widget config ──
  const WIDGET_TYPES = {
    clock:   { name: 'Reloj',   icon: '🕐', cols: 2, rows: 2 },
    sysmon:  { name: 'Sistema', icon: '📊', cols: 2, rows: 2 },
    network: { name: 'Red',     icon: '🌐', cols: 2, rows: 2 },
    storage: { name: 'Storage', icon: '💾', cols: 2, rows: 2 },
  };

  const SIZE_PRESETS = {
    clock:   [{ label: '1×1', cols:2, rows:2 }],
    sysmon:  [{ label: '1×1', cols:2, rows:2 }, { label: '1×2', cols:4, rows:2 }, { label: '2×2', cols:4, rows:4 }],
    network: [{ label: '1×1', cols:2, rows:2 }, { label: '1×2', cols:4, rows:2 }],
    storage: [{ label: '1×1', cols:2, rows:2 }, { label: '1×2', cols:4, rows:2 }, { label: '2×2', cols:4, rows:4 }],
  };

  const DEFAULT_LAYOUT = [
    { id: 'w1', type: 'clock',   col: 0,  row: 0, cols: 2, rows: 2 },
    { id: 'w2', type: 'sysmon',  col: 0,  row: 2, cols: 2, rows: 2 },
    { id: 'w3', type: 'network', col: -2, row: 0, cols: 2, rows: 2 },
    { id: 'w4', type: 'storage', col: -3, row: 2, cols: 3, rows: 2 },
  ];

  const CELL = 80, GAP = 10, MARGIN = 16;

  // ── Grid ──
  let widgets = [], gridCols = 0, gridRows = 0, layoutLoaded = false;

  function getZoom() { return parseFloat(document.documentElement.style.zoom) || 1; }
  function getTbInfo() {
    const tbH = parseInt(getComputedStyle(document.documentElement).getPropertyValue('--taskbar-height')) || 48;
    const tbPos = document.documentElement.getAttribute('data-taskbar-pos') || 'bottom';
    return { tbH, tbPos };
  }
  function computeGrid() {
    const z = getZoom(), vw = window.innerWidth/z, vh = window.innerHeight/z;
    const { tbH, tbPos } = getTbInfo();
    let aW = vw - MARGIN*2, aH = vh - tbH - MARGIN*2;
    if (tbPos === 'left') { aW = vw - tbH - MARGIN*2; aH = vh - MARGIN*2; }
    gridCols = Math.floor((aW + GAP) / (CELL + GAP));
    gridRows = Math.floor((aH + GAP) / (CELL + GAP));
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
  $: { const lj = JSON.stringify($prefs.widgetLayout); if (lj !== prevLayoutJson && !dragging) { prevLayoutJson = lj; if (typeof window !== 'undefined') loadLayout(); } }

  onMount(() => { computeGrid(); loadLayout(); startPolling(); window.addEventListener('resize', onResize); });
  onDestroy(() => { stopPolling(); window.removeEventListener('resize', onResize); clearInterval(clockInterval); });

  function onResize() {
    computeGrid();
    widgets = widgets.map(w => ({ ...w, col: Math.max(0, Math.min(gridCols-w.cols, w.col)), row: Math.max(0, Math.min(gridRows-w.rows, w.row)) }));
  }

  function cellX(col) { const { tbH, tbPos } = getTbInfo(); return (tbPos==='left'?tbH:0) + MARGIN + col*(CELL+GAP); }
  function cellY(row) { const { tbH, tbPos } = getTbInfo(); return (tbPos==='top'?tbH:0) + MARGIN + row*(CELL+GAP); }
  function cellW(cols) { return cols*CELL + (cols-1)*GAP; }
  function cellH(rows) { return rows*CELL + (rows-1)*GAP; }

  // ── Drag ──
  let dragging = null, dragPreviewCol = 0, dragPreviewRow = 0, dragWidget = null;

  function hasCollision(col, row, cols, rows, excludeId = null) {
    return widgets.some(w => {
      if (w.id === excludeId) return false;
      return col < w.col+w.cols && col+cols > w.col && row < w.row+w.rows && row+rows > w.row;
    });
  }

  function onDragStart(e, widget) {
    if (e.target.closest('.wm-btn')) return;
    e.preventDefault(); closeMenu();
    const z = getZoom();
    dragging = { id: widget.id, startMx: e.clientX/z, startMy: e.clientY/z, origCol: widget.col, origRow: widget.row };
    dragWidget = widget; dragPreviewCol = widget.col; dragPreviewRow = widget.row;
    window.addEventListener('mousemove', onDragMove);
    window.addEventListener('mouseup', onDragEnd);
  }
  function onDragMove(e) {
    if (!dragging || !dragWidget) return;
    const z = getZoom();
    dragPreviewCol = Math.max(0, Math.min(gridCols-dragWidget.cols, dragging.origCol + Math.round((e.clientX/z - dragging.startMx)/(CELL+GAP))));
    dragPreviewRow = Math.max(0, Math.min(gridRows-dragWidget.rows, dragging.origRow + Math.round((e.clientY/z - dragging.startMy)/(CELL+GAP))));
  }
  function onDragEnd() {
    if (dragging && dragWidget) {
      const idx = widgets.findIndex(w => w.id === dragging.id);
      if (idx >= 0 && !hasCollision(dragPreviewCol, dragPreviewRow, dragWidget.cols, dragWidget.rows, dragging.id)) {
        widgets[idx].col = dragPreviewCol; widgets[idx].row = dragPreviewRow;
      }
      widgets = widgets; saveLayout();
    }
    dragging = null; dragWidget = null;
    window.removeEventListener('mousemove', onDragMove);
    window.removeEventListener('mouseup', onDragEnd);
  }

  // ── Context menu ──
  let activeMenu = null;

  function openMenu(e, widgetId, widget) {
    e.stopPropagation();
    if (activeMenu?.widgetId === widgetId) { activeMenu = null; return; }
    const wLeft = cellX(widget.col), wTop = cellY(widget.row), wWidth = cellW(widget.cols);
    const menuW = 210;
    let x = wLeft + wWidth - menuW;
    if (x < 8) x = wLeft;
    if (x + menuW > window.innerWidth - 8) x = window.innerWidth - menuW - 8;
    activeMenu = { widgetId, x, y: wTop, sub: null };
  }
  function closeMenu() { activeMenu = null; }

  function removeWidget(id) { widgets = widgets.filter(w => w.id !== id); saveLayout(); closeMenu(); }

  function addWidget(type) {
    const meta = WIDGET_TYPES[type]; const id = 'w' + Date.now(); let placed = false;
    outer: for (let r = 0; r <= gridRows - meta.rows; r++)
      for (let c = 0; c <= gridCols - meta.cols; c++)
        if (!hasCollision(c, r, meta.cols, meta.rows)) { widgets = [...widgets, { id, type, col:c, row:r, cols:meta.cols, rows:meta.rows }]; placed=true; break outer; }
    if (!placed) widgets = [...widgets, { id, type, col:0, row:0, cols:meta.cols, rows:meta.rows }];
    saveLayout(); closeMenu();
  }

  function resizeWidget(id, cols, rows) {
    const idx = widgets.findIndex(w => w.id === id);
    if (idx >= 0 && !hasCollision(widgets[idx].col, widgets[idx].row, cols, rows, id)) {
      widgets[idx] = { ...widgets[idx], cols, rows, col: Math.min(widgets[idx].col, gridCols-cols), row: Math.min(widgets[idx].row, gridRows-rows) };
      widgets = widgets; saveLayout();
    }
    closeMenu();
  }

  function resetLayout() { widgets = resolveLayout(DEFAULT_LAYOUT); saveLayout(); closeMenu(); }

  // ── Data polling ──
  let pollTimer, sysData = {}, storageData = {}, netData = {};

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
  function startPolling() { fetchData(); pollTimer = setInterval(fetchData, 3000); }
  function stopPolling()  { if (pollTimer) clearInterval(pollTimer); }

  // ── Clock ──
  let clockTime = '', clockDate = '';
  function updateClock() {
    const now = new Date();
    clockTime = now.toLocaleTimeString('es-ES', { hour:'2-digit', minute:'2-digit', second:'2-digit' });
    clockDate = now.toLocaleDateString('es-ES', { weekday:'long', day:'numeric', month:'long' });
    updateLcdClocks();
  }
  updateClock();
  const clockInterval = setInterval(updateClock, 1000);

  // ── LCD Clock ──
  const LCD_DIGITS = [[1,1,1,1,1,1,0],[0,1,1,0,0,0,0],[1,1,0,1,1,0,1],[1,1,1,1,0,0,1],[0,1,1,0,0,1,1],[1,0,1,1,0,1,1],[1,0,1,1,1,1,1],[1,1,1,0,0,0,0],[1,1,1,1,1,1,1],[1,1,1,1,0,1,1]];

  function drawLcdPair(canvas, val) {
    if (!canvas) return;
    const dpr = window.devicePixelRatio || 1;
    const DW=32, DH=56, S=5, GAP_D=8, PAD=6;
    const cw = PAD*2 + DW*2 + GAP_D, ch = PAD*2 + DH;
    canvas.width = cw*dpr; canvas.height = ch*dpr;
    canvas.style.width = cw+'px'; canvas.style.height = ch+'px';
    const ctx = canvas.getContext('2d');
    ctx.scale(dpr, dpr); ctx.clearRect(0,0,cw,ch);
    const theme = document.documentElement.getAttribute('data-theme') || 'midnight';
    const ON  = theme==='light' ? 'rgba(15,15,15,0.85)'  : 'rgba(240,240,240,0.90)';
    const OFF = theme==='light' ? 'rgba(0,0,0,0.07)'     : 'rgba(255,255,255,0.06)';

    function seg(x,y,isOn,horiz) {
      ctx.fillStyle = isOn ? ON : OFF;
      const r=2, rw = horiz ? DW-S*2 : S, rh = horiz ? S : (DH-S*3)/2;
      ctx.beginPath();
      ctx.moveTo(x+r,y); ctx.lineTo(x+rw-r,y); ctx.quadraticCurveTo(x+rw,y,x+rw,y+r);
      ctx.lineTo(x+rw,y+rh-r); ctx.quadraticCurveTo(x+rw,y+rh,x+rw-r,y+rh);
      ctx.lineTo(x+r,y+rh); ctx.quadraticCurveTo(x,y+rh,x,y+rh-r);
      ctx.lineTo(x,y+r); ctx.quadraticCurveTo(x,y,x+r,y);
      ctx.closePath(); ctx.fill();
    }

    function digit(n, ox, oy) {
      const d = LCD_DIGITS[n]||LCD_DIGITS[0], hh=(DH-S*3)/2;
      seg(ox+S,   oy,          d[0], true);  // top
      seg(ox+DW-S,oy+S,        d[1], false); // top-right
      seg(ox+DW-S,oy+S*2+hh,  d[2], false); // bot-right
      seg(ox+S,   oy+DH-S,     d[3], true);  // bottom
      seg(ox,     oy+S*2+hh,   d[4], false); // bot-left
      seg(ox,     oy+S,        d[5], false); // top-left
      seg(ox+S,   oy+S+hh,     d[6], true);  // middle
    }

    digit(Math.floor(val/10), PAD, PAD);
    digit(val%10, PAD+DW+GAP_D, PAD);
  }

  function updateLcdClocks() {
    if (typeof document === 'undefined') return;
    const now = new Date();
    const DAYS   = ['SUN','MON','TUE','WED','THU','FRI','SAT'];
    const MONTHS = ['JAN','FEB','MAR','APR','MAY','JUN','JUL','AUG','SEP','OCT','NOV','DEC'];
    const todayIdx = now.getDay();

    document.querySelectorAll('.wg-lcd-canvas').forEach(canvas => {
      drawLcdPair(canvas, canvas.dataset.role==='hours' ? now.getHours() : now.getMinutes());
    });
    document.querySelectorAll('[id^="wg-days-"]').forEach(el => {
      el.innerHTML = '';
      DAYS.forEach((d,i) => {
        const s = document.createElement('span');
        s.textContent = d;
        s.className = 'wg-day-item' + (i===todayIdx ? ' today' : '');
        el.appendChild(s);
      });
    });
    document.querySelectorAll('[id^="wg-datenum-"]').forEach(el => el.textContent = now.getDate());
    document.querySelectorAll('[id^="wg-datemon-"]').forEach(el => el.textContent = MONTHS[now.getMonth()]);
  }

  // ── Ring gauge helpers ──
  function stoColor(pct) { return pct < 70 ? '#4ade80' : pct < 85 ? '#fbbf24' : '#f87171'; }

  function ringDash(r)       { return (2*Math.PI*r).toFixed(1); }
  function ringOffset(r,pct) { return (2*Math.PI*r*(1-Math.min(100,pct)/100)).toFixed(1); }
  function arcColor(pct)     { return pct<60?'#22c55e':pct<80?'#f97316':'#ef4444'; }

  // ── CPU history for 2×2 chart ──
  let cpuHistory = Array(40).fill(0);

  // ── Reactive data ──
  $: cpuPct   = sysData.cpu?.percent    ?? sysData.cpuPercent ?? 0;
  $: memPct   = sysData.memory?.percent ?? sysData.memPercent ?? 0;
  $: memUsed  = sysData.memory?.used    ?? 0;
  $: memTotal = sysData.memory?.total   ?? 0;
  $: cpuTemp  = sysData.temps?.cpu      ?? sysData.cpuTemp    ?? null;
  $: pools    = storageData.pools       || [];
  $: { if (cpuPct > 0) { cpuHistory = [...cpuHistory.slice(-39), cpuPct]; } }
  $: chartPts  = cpuHistory.map((v,i) => [(i/(cpuHistory.length-1||1))*300, 40-(Math.min(100,v)/100)*36-2]);
  $: chartLine = chartPts.map((p,i) => (i===0?`M ${p[0].toFixed(1)} ${p[1].toFixed(1)}`:`L ${p[0].toFixed(1)} ${p[1].toFixed(1)}`)).join(' ');
  $: chartArea = chartLine + ' L 300 40 L 0 40 Z';

  $: netIfaces    = Array.isArray(netData) ? netData : (netData.interfaces || []);
  $: primaryIface = netIfaces.find(i => i.ip && i.ip !== '127.0.0.1') || netIfaces[0] || {};

  // ── Network charts ──
  let netRxSpeed = 0, netTxSpeed = 0;
  const NET_HIST = 30;
  let dlHist = Array(NET_HIST).fill(0);
  let ulHist = Array(NET_HIST).fill(0);

  function fmtNetSpeed(bps) {
    if (!bps || bps <= 0) return '0 KB/s';
    if (bps >= 1e6) return (bps/1e6).toFixed(1) + ' MB/s';
    return (bps/1e3).toFixed(0) + ' KB/s';
  }

  function fmtBytes(b) {
    if (!b || b<=0) return '—';
    if (b>=1e12) return (b/1e12).toFixed(1)+' TB';
    if (b>=1e9)  return (b/1e9).toFixed(1)+' GB';
    return (b/1e6).toFixed(0)+' MB';
  }

  function updateNetCharts() {
    netRxSpeed = primaryIface?.rxRate || 0;
    netTxSpeed = primaryIface?.txRate || 0;
    dlHist = [...dlHist.slice(-(NET_HIST-1)), netRxSpeed];
    ulHist = [...ulHist.slice(-(NET_HIST-1)), netTxSpeed];
    drawNetCharts();
  }

  function drawNetChart(canvas, history, color) {
    if (!canvas) return;
    const dpr = window.devicePixelRatio || 1;
    const W = canvas.parentElement?.offsetWidth || 120;
    const H = 38;
    canvas.width = W*dpr; canvas.height = H*dpr;
    canvas.style.width = W+'px'; canvas.style.height = H+'px';
    const ctx = canvas.getContext('2d');
    ctx.scale(dpr, dpr); ctx.clearRect(0,0,W,H);
    const max = Math.max(...history, 1000);
    const pts = history.map((v,i) => [(i/(history.length-1))*W, H-(v/max)*(H-3)-1]);
    const grad = ctx.createLinearGradient(0,0,0,H);
    grad.addColorStop(0, color+'44'); grad.addColorStop(1, color+'08');
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
      drawNetChart(canvas, role==='dl' ? dlHist : ulHist, role==='dl' ? '#3b82f6' : '#a855f7');
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
      <div class="wc" class:is-dragging={isDrag} class:menu-open={menuOpen}
        style="left:{cellX(dc)}px; top:{cellY(dr)}px; width:{cellW(widget.cols)}px; height:{cellH(widget.rows)}px;"
        on:mousedown={(e) => onDragStart(e, widget)}>

        <button class="wm-btn" on:click={(e) => openMenu(e, widget.id, widget)}>
          <span class="wm-dot"></span><span class="wm-dot"></span><span class="wm-dot"></span>
        </button>

        <div class="wc-body">

          {#if widget.type === 'clock'}
            <!-- ── LCD CLOCK 1×1 ── -->
            <div class="wg-lcd-wrap">
              <div class="wg-lcd-left">
                <canvas class="wg-lcd-canvas" data-role="hours"></canvas>
                <canvas class="wg-lcd-canvas" data-role="minutes"></canvas>
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
            {@const is1x1 = widget.cols <= 2 && widget.rows <= 2}
            {@const is1x2 = widget.cols >= 4 && widget.rows <= 2}

            {#if is1x1}
              <!-- ── SYSMON 1×1: double ring ── -->
              <div class="wg-double-ring">
                <svg viewBox="0 0 160 160" class="wg-ring-svg">
                  <defs>
                    <linearGradient id="cg-{widget.id}" x1="0%" y1="0%" x2="100%" y2="100%">
                      <stop offset="0%" stop-color="#f97316"/><stop offset="100%" stop-color="#ef4444"/>
                    </linearGradient>
                  </defs>
                  <circle cx="80" cy="80" r="64" fill="none" class="ring-bg" stroke-width="13"/>
                  <circle cx="80" cy="80" r="64" fill="none" stroke-width="13" stroke-linecap="round"
                    transform="rotate(-90 80 80)"
                    stroke-dasharray={ringDash(64)} stroke-dashoffset={ringOffset(64,cpuPct)}
                    style="stroke:{arcColor(cpuPct)};transition:stroke-dashoffset .6s ease"/>
                  <circle cx="80" cy="80" r="44" fill="none" class="ring-bg" stroke-width="12"/>
                  <circle cx="80" cy="80" r="44" fill="none" stroke="#3b82f6" stroke-width="12" stroke-linecap="round"
                    transform="rotate(-90 80 80)"
                    stroke-dasharray={ringDash(44)} stroke-dashoffset={ringOffset(44,memPct)}
                    style="transition:stroke-dashoffset .6s ease"/>
                  <text x="80" y="73" text-anchor="middle" dominant-baseline="middle" class="ring-pct">{cpuPct.toFixed(0)}%</text>
                  <text x="80" y="91" text-anchor="middle" class="ring-sub">{memPct.toFixed(0)}%</text>
                </svg>
              </div>

            {:else if is1x2}
              <!-- ── SYSMON 1×2: two rings side by side ── -->
              <div class="wg-two-rings">
                <div class="wg-ring-wrap">
                  <svg viewBox="0 0 130 130" class="wg-ring-svg-lg">
                    <defs>
                      <linearGradient id="cg2-{widget.id}" x1="0%" y1="0%" x2="100%" y2="100%">
                        <stop offset="0%" stop-color="#f97316"/><stop offset="100%" stop-color="#ef4444"/>
                      </linearGradient>
                    </defs>
                    <circle cx="65" cy="65" r="52" fill="none" class="ring-bg" stroke-width="12"/>
                    <circle cx="65" cy="65" r="52" fill="none" stroke-width="12" stroke-linecap="round"
                      transform="rotate(-90 65 65)"
                      stroke-dasharray={ringDash(52)} stroke-dashoffset={ringOffset(52,cpuPct)}
                      style="stroke:{arcColor(cpuPct)};transition:stroke-dashoffset .6s ease"/>
                    <text x="65" y="60" text-anchor="middle" dominant-baseline="middle" class="ring-pct">{cpuPct.toFixed(0)}%</text>
                    <text x="65" y="77" text-anchor="middle" class="ring-label" style="fill:{arcColor(cpuPct)}">CPU</text>
                  </svg>
                </div>
                <div class="wg-ring-divider"></div>
                <div class="wg-ring-wrap">
                  <svg viewBox="0 0 130 130" class="wg-ring-svg-lg">
                    <circle cx="65" cy="65" r="52" fill="none" class="ring-bg" stroke-width="12"/>
                    <circle cx="65" cy="65" r="52" fill="none" stroke="#3b82f6" stroke-width="12" stroke-linecap="round"
                      transform="rotate(-90 65 65)"
                      stroke-dasharray={ringDash(52)} stroke-dashoffset={ringOffset(52,memPct)}
                      style="transition:stroke-dashoffset .6s ease"/>
                    <text x="65" y="60" text-anchor="middle" dominant-baseline="middle" class="ring-pct">{memPct.toFixed(0)}%</text>
                    <text x="65" y="77" text-anchor="middle" class="ring-label" style="fill:#3b82f6">RAM</text>
                  </svg>
                </div>
              </div>

            {:else}
              <!-- ── SYSMON 2×2: rings + info + chart ── -->
              <div class="wg-header">System Resources</div>
              <div class="wg-two-rings" style="flex:1">
                <div class="wg-ring-wrap">
                  <svg viewBox="0 0 112 112" class="wg-ring-svg-lg">
                    <defs>
                      <linearGradient id="cg3-{widget.id}" x1="0%" y1="0%" x2="100%" y2="100%">
                        <stop offset="0%" stop-color="#f97316"/><stop offset="100%" stop-color="#ef4444"/>
                      </linearGradient>
                    </defs>
                    <circle cx="56" cy="56" r="44" fill="none" class="ring-bg" stroke-width="9"/>
                    <circle cx="56" cy="56" r="44" fill="none" stroke-width="9" stroke-linecap="round"
                      transform="rotate(-90 56 56)"
                      stroke-dasharray={ringDash(44)} stroke-dashoffset={ringOffset(44,cpuPct)}
                      style="stroke:{arcColor(cpuPct)};transition:stroke-dashoffset .6s ease"/>
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
                    <circle cx="56" cy="56" r="44" fill="none" stroke="#3b82f6" stroke-width="9" stroke-linecap="round"
                      transform="rotate(-90 56 56)"
                      stroke-dasharray={ringDash(44)} stroke-dashoffset={ringOffset(44,memPct)}
                      style="transition:stroke-dashoffset .6s ease"/>
                    <text x="56" y="51" text-anchor="middle" dominant-baseline="middle" class="ring-pct">{memPct.toFixed(0)}%</text>
                    <text x="56" y="66" text-anchor="middle" class="ring-label" style="fill:#3b82f6">RAM</text>
                  </svg>
                  <div class="wg-ring-info">
                    <div class="wg-kv"><span>Used</span><span>{fmtBytes(memUsed)}</span></div>
                    <div class="wg-kv"><span>Total</span><span>{fmtBytes(memTotal)}</span></div>
                  </div>
                </div>
              </div>
              <div class="wg-chart-wrap">
                <div class="wg-chart-label">CPU Activity</div>
                <svg class="wg-chart-svg" viewBox="0 0 300 40" preserveAspectRatio="none">
                  <defs>
                    <linearGradient id="ag-{widget.id}" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stop-color="#f97316" stop-opacity="0.45"/>
                      <stop offset="100%" stop-color="#f97316" stop-opacity="0"/>
                    </linearGradient>
                  </defs>
                  <path d={chartArea} fill="url(#ag-{widget.id})"/>
                  <path d={chartLine} fill="none" stroke="#f97316" stroke-width="1.5" stroke-linejoin="round"/>
                </svg>
              </div>
            {/if}

          {:else if widget.type === 'network'}
            <!-- ── NETWORK: DL + UL charts ── -->
            <div class="wg-header">Red</div>
            <div class="wg-net-wrap">
              <div class="wg-net-row">
                <div class="wg-net-label">
                  <span class="wg-net-arrow dl">↓</span>
                  <span class="wg-net-val">{fmtNetSpeed(netRxSpeed)}</span>
                </div>
                <canvas class="wg-net-canvas" data-role="dl" data-netid="{widget.id}"></canvas>
              </div>
              <div class="wg-net-row">
                <div class="wg-net-label">
                  <span class="wg-net-arrow ul">↑</span>
                  <span class="wg-net-val">{fmtNetSpeed(netTxSpeed)}</span>
                </div>
                <canvas class="wg-net-canvas" data-role="ul" data-netid="{widget.id}"></canvas>
              </div>
            </div>

          {:else if widget.type === 'storage'}
            {@const is1x1sto  = widget.cols <= 2 && widget.rows <= 2}
            {@const is1x2sto  = widget.cols >= 4 && widget.rows <= 2}
            {@const stoPoolId = widget.storageTarget || null}
            {@const stoPool   = stoPoolId ? pools.find(p => p.name === stoPoolId) : pools[0]}

            {#if is1x1sto}
              <!-- ── STORAGE 1×1: single ring ── -->
              <div class="wg-sto-select-wrap">
                <!-- svelte-ignore a11y_change_events_have_key_events -->
                <select class="wg-sto-select"
                  on:change={(e) => { const idx = widgets.findIndex(w => w.id === widget.id); if(idx>=0){ widgets[idx].storageTarget = e.target.value; widgets=widgets; saveLayout(); } }}>
                  {#each pools as p}
                    <option value={p.name} selected={stoPoolId === p.name || (!stoPoolId && p === pools[0])}>{p.name}</option>
                  {/each}
                </select>
              </div>
              {#if stoPool}
                <div class="wg-double-ring" style="margin-top:4px">
                  <svg viewBox="0 0 110 110" class="wg-ring-svg">
                    <circle cx="55" cy="55" r="44" fill="none" class="ring-bg" stroke-width="11"/>
                    <circle cx="55" cy="55" r="44" fill="none" stroke-width="11" stroke-linecap="round"
                      transform="rotate(-90 55 55)"
                      stroke-dasharray={ringDash(44)}
                      stroke-dashoffset={ringOffset(44, stoPool.usagePercent||0)}
                      style="stroke:{stoColor(stoPool.usagePercent||0)};transition:stroke-dashoffset .6s ease"/>
                    <text x="55" y="50" text-anchor="middle" dominant-baseline="middle" class="ring-pct">{stoPool.usagePercent||0}%</text>
                    <text x="55" y="67" text-anchor="middle" class="ring-sub" style="font-size:8px">{stoPool.usedFormatted||'—'} / {stoPool.totalFormatted||'—'}</text>
                  </svg>
                </div>
              {:else}
                <div class="wg-empty">Sin pools</div>
              {/if}

            {:else if is1x2sto}
              <!-- ── STORAGE 1×2: bars ── -->
              <div class="wg-header">Storage</div>
              {#if pools.length > 0}
                <div class="wg-sto-bars">
                  {#each pools as pool}
                    <div class="wg-sto-bar-row">
                      <div class="wg-sto-bar-info">
                        <span class="wg-sto-bar-name">{pool.name}</span>
                        <span class="wg-sto-bar-size">{pool.usedFormatted||'—'} / {pool.totalFormatted||'—'}</span>
                      </div>
                      <div class="wg-sto-track">
                        <div class="wg-sto-fill" style="width:{pool.usagePercent||0}%;background:{stoColor(pool.usagePercent||0)}"></div>
                      </div>
                    </div>
                  {/each}
                </div>
              {:else}
                <div class="wg-empty">Sin pools</div>
              {/if}

            {:else}
              <!-- ── STORAGE 2×2: cards + total ── -->
              <div class="wg-header">Storage</div>
              {#if pools.length > 0}
                <div class="wg-sto-cards">
                  {#each pools as pool}
                    <div class="wg-sto-card">
                      <div class="wg-sto-card-top">
                        <div>
                          <div class="wg-sto-card-name">{pool.name}</div>
                          <div class="wg-sto-card-raid">{pool.raidLevel || pool.type || '—'}</div>
                        </div>
                        <span class="wg-sto-card-pct" style="color:{stoColor(pool.usagePercent||0)}">{pool.usagePercent||0}%</span>
                      </div>
                      <div class="wg-sto-track">
                        <div class="wg-sto-fill" style="width:{pool.usagePercent||0}%;background:{stoColor(pool.usagePercent||0)}"></div>
                      </div>
                      <div class="wg-sto-card-meta">{pool.usedFormatted||'—'} usado · {pool.availableFormatted||'—'} libre</div>
                    </div>
                  {/each}
                </div>
              {:else}
                <div class="wg-empty">Sin pools</div>
              {/if}
            {/if}
          {/if}

        </div>
      </div>

      {#if isDrag}
        <div class="snap-ghost" style="left:{cellX(dragPreviewCol)}px; top:{cellY(dragPreviewRow)}px; width:{cellW(widget.cols)}px; height:{cellH(widget.rows)}px;"></div>
      {/if}

      {#if menuOpen}
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="ctx-menu" style="left:{activeMenu.x}px; top:{activeMenu.y}px; transform:translateY(-100%) translateY(-8px);" on:click|stopPropagation>
          {#if activeMenu.sub === 'size'}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <div class="ctx-back" on:click={() => activeMenu = { ...activeMenu, sub:null }}>‹ Volver</div>
            <div class="ctx-divider"></div>
            {#each SIZE_PRESETS[widget.type]||[] as preset}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <div class="ctx-item" class:active={widget.cols===preset.cols&&widget.rows===preset.rows}
                on:click={() => resizeWidget(widget.id, preset.cols, preset.rows)}>
                <span class="ctx-ico">◻</span>{preset.label}
                <span class="ctx-size-hint">{preset.cols}×{preset.rows}</span>
              </div>
            {/each}
          {:else if activeMenu.sub === 'add'}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <div class="ctx-back" on:click={() => activeMenu = { ...activeMenu, sub:null }}>‹ Volver</div>
            <div class="ctx-divider"></div>
            {#each Object.entries(WIDGET_TYPES) as [type, meta]}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <div class="ctx-item" on:click={() => addWidget(type)}>
                <span class="ctx-ico">{meta.icon}</span>{meta.name}
              </div>
            {/each}
          {:else}
            <div class="ctx-header">
              <span>{WIDGET_TYPES[widget.type]?.icon}</span>
              <span>{WIDGET_TYPES[widget.type]?.name}</span>
            </div>
            <div class="ctx-divider"></div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <div class="ctx-item" on:click={() => activeMenu = { ...activeMenu, sub:'add' }}>
              <span class="ctx-ico">＋</span>Añadir widget ›
            </div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <div class="ctx-item" on:click={() => activeMenu = { ...activeMenu, sub:'size' }}>
              <span class="ctx-ico">◻</span>Cambiar tamaño ›
            </div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <div class="ctx-item" on:click={resetLayout}>
              <span class="ctx-ico">⊞</span>Restablecer grid
            </div>
            <div class="ctx-divider"></div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <div class="ctx-item danger" on:click={() => removeWidget(widget.id)}>
              <span class="ctx-ico">✕</span>Eliminar
            </div>
          {/if}
        </div>
      {/if}
    {/each}

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

  /* ── STORAGE WIDGET ── */
  .wg-sto-select-wrap { width:100%; flex-shrink:0; }
  .wg-sto-select {
    width:100%; font-size:9px; padding:3px 8px; border-radius:6px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    color:var(--text-2); font-family:'DM Sans',sans-serif;
    cursor:pointer; appearance:none; text-align:center; outline:none;
  }
  .wg-sto-select:focus { border-color:var(--border-hi); }
  .wg-sto-bars { display:flex; flex-direction:column; gap:10px; flex:1; }
  .wg-sto-bar-row { display:flex; flex-direction:column; gap:5px; }
  .wg-sto-bar-info { display:flex; justify-content:space-between; align-items:baseline; }
  .wg-sto-bar-name { font-size:11px; font-weight:500; color:var(--text-1); }
  .wg-sto-bar-size { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; }
  .wg-sto-track { height:8px; border-radius:4px; background:var(--ibtn-bg); overflow:hidden; }
  :global([data-theme="light"]) .wg-sto-track { background:rgba(0,0,0,0.07); }
  .wg-sto-fill { height:100%; border-radius:4px; transition:width .5s ease; }
  .wg-sto-cards { display:flex; flex-direction:column; gap:8px; flex:1; }
  .wg-sto-card { border:1px solid var(--border); border-radius:8px; padding:9px 12px; display:flex; flex-direction:column; gap:6px; }
  .wg-sto-card-top { display:flex; justify-content:space-between; align-items:center; }
  .wg-sto-card-name { font-size:12px; font-weight:500; color:var(--text-1); }
  .wg-sto-card-raid { font-size:9px; color:var(--text-3); letter-spacing:.04em; margin-top:1px; }
  .wg-sto-card-pct  { font-size:14px; font-weight:500; font-family:'DM Mono',monospace; }
  .wg-sto-card-meta { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; }

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

/* ── NETWORK ── */
  .wg-net-wrap { display:flex; flex-direction:column; gap:8px; flex:1; width:100%; }
  .wg-net-row { display:flex; flex-direction:column; gap:3px; flex:1; }
  .wg-net-label { display:flex; justify-content:space-between; align-items:baseline; }
  .wg-net-arrow { font-size:12px; font-weight:700; }
  .wg-net-arrow.dl { color:#3b82f6; }
  .wg-net-arrow.ul { color:#a855f7; }
  .wg-net-val { font-size:11px; color:var(--text-1); font-family:'DM Mono',monospace; }
  .wg-net-canvas { display:block; width:100%; border-radius:4px; }

  /* ── LCD CLOCK ── */
  .wg-lcd-wrap { width:100%; height:100%; display:flex; align-items:center; justify-content:center; gap:8px; padding:4px; }
  .wg-lcd-left { display:flex; flex-direction:column; align-items:center; justify-content:center; gap:6px; flex:1; }
  .wg-lcd-right { display:flex; flex-direction:column; align-items:center; justify-content:space-between; height:100%; padding:6px 0; }
  .wg-lcd-days { display:flex; flex-direction:column; align-items:center; gap:2px; }
  :global(.wg-day-item) { font-size:8px; font-family:'DM Sans',sans-serif; letter-spacing:.05em; color:var(--text-3); padding:1px 5px; border-radius:10px; border:1px solid transparent; }
  :global(.wg-day-item.today) { color:var(--text-1); font-weight:600; background:var(--ibtn-bg); border-color:var(--border-hi); }
  .wg-lcd-date { display:flex; flex-direction:column; align-items:center; gap:1px; }
  .wg-lcd-datenum { font-size:18px; font-weight:500; color:var(--text-1); font-family:'DM Sans',sans-serif; line-height:1; }
  .wg-lcd-datemon { font-size:8px; color:var(--text-3); font-family:'DM Sans',sans-serif; letter-spacing:.06em; text-transform:uppercase; }
</style>
