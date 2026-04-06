<script>
  import { onMount, onDestroy } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  let view = 'dashboard'; // 'dashboard' | 'detail'
  let services = [];
  let selectedService = null;
  let filter = 'all';
  let loading = false;
  let stopping = {};

  // System metrics
  let cpu = { percent: 0, cores: 0, load: 0 };
  let ram = { used: 0, total: 0, percent: 0 };
  let diskIO = { read: 0, write: 0 };
  let netIO = { rx: 0, tx: 0 };
  let cpuHistory = Array(12).fill(0);
  let ramHistory = Array(12).fill(0);
  let diskHistory = Array(12).fill(0);
  let netHistory = Array(12).fill(0);

  // Detail view
  let detailLogs = [];
  let detailStats = null;

  // Polling
  let pollInterval;

  async function loadServices() {
    try {
      const r = await fetch('/api/services', { headers: hdrs() });
      const d = await r.json();
      services = d.services || [];
    } catch { services = []; }
  }

  async function loadMetrics() {
    try {
      const r = await fetch('/api/hardware/stats', { headers: hdrs() });
      const d = await r.json();
      if (d.cpu) {
        cpu = { percent: Math.round(d.cpu.percent || 0), cores: d.cpu.cores || 0, load: (d.cpu.load1 || 0).toFixed(1) };
        cpuHistory = [...cpuHistory.slice(1), cpu.percent];
      }
      if (d.memory) {
        ram = { used: d.memory.used || 0, total: d.memory.total || 0, percent: Math.round((d.memory.used / d.memory.total) * 100) || 0 };
        ramHistory = [...ramHistory.slice(1), ram.percent];
      }
      if (d.disk) {
        diskIO = { read: d.disk.readSpeed || 0, write: d.disk.writeSpeed || 0 };
        diskHistory = [...diskHistory.slice(1), Math.min(100, Math.round((diskIO.read + diskIO.write) / 1048576))];
      }
      if (d.network) {
        netIO = { rx: d.network.rxSpeed || 0, tx: d.network.txSpeed || 0 };
        netHistory = [...netHistory.slice(1), Math.min(100, Math.round((netIO.rx + netIO.tx) / 1048576))];
      }
    } catch {}
  }

  async function loadDetail(svc) {
    selectedService = svc;
    view = 'detail';
    detailLogs = [];
    detailStats = null;
    await loadLogs(svc);
  }

  function goBack() {
    view = 'dashboard';
    selectedService = null;
    detailLogs = [];
  }

  async function loadLogs(svc) {
    try {
      const r = await fetch(`/api/services/${svc.id}/logs?n=50`, { headers: hdrs() });
      const d = await r.json();
      detailLogs = d.logs || [];
    } catch { detailLogs = []; }
  }

  async function doAction(svc, action) {
    const key = svc.id + ':' + action;
    stopping = { ...stopping, [key]: true };
    try {
      await fetch(`/api/services/${svc.id}/${action}`, { method: 'POST', headers: hdrs() });
      await loadServices();
      if (selectedService?.id === svc.id) {
        selectedService = services.find(s => s.id === svc.id) || selectedService;
        await loadLogs(selectedService);
      }
    } catch {}
    stopping = { ...stopping, [key]: false };
  }

  function fmtBytes(b) {
    if (!b || b === 0) return '0 B';
    if (b >= 1e12) return (b / 1e12).toFixed(1) + ' TB';
    if (b >= 1e9) return (b / 1e9).toFixed(1) + ' GB';
    if (b >= 1e6) return (b / 1e6).toFixed(1) + ' MB';
    if (b >= 1e3) return (b / 1e3).toFixed(0) + ' KB';
    return b + ' B';
  }

  function fmtSpeed(b) {
    if (!b) return '0';
    if (b >= 1e6) return (b / 1e6).toFixed(0);
    if (b >= 1e3) return (b / 1e3).toFixed(0);
    return '0';
  }

  // Filter services (only backend services, not UI apps)
  $: filteredServices = services.filter(s => {
    if (filter === 'all') return true;
    if (filter === 'running') return s.status === 'running';
    if (filter === 'error') return s.status === 'error' || s.status === 'failed';
    if (filter === 'alert') return s.status === 'error' || s.health === 'degraded' || s.health === 'unreachable';
    return true;
  });

  $: runningCount = services.filter(s => s.status === 'running').length;
  $: stoppedCount = services.filter(s => s.status === 'stopped').length;
  $: errorCount = services.filter(s => s.status === 'error' || s.status === 'failed').length;
  $: alertCount = services.filter(s => s.health === 'degraded' || s.health === 'unreachable').length;
  $: topCpu = services.find(s => s.status === 'running');

  // Pool list from services
  $: pools = [...new Set(services.map(s => s.poolName).filter(Boolean))];

  function statusDotClass(s) {
    if (s.status === 'running') return 'dot-running';
    if (s.status === 'stopped') return 'dot-stopped';
    if (s.status === 'error' || s.status === 'failed') return 'dot-error';
    if (s.status === 'starting' || s.status === 'stopping') return 'dot-starting';
    return 'dot-stopped';
  }

  function healthPillClass(s) {
    if (s.health === 'healthy') return 'hp-healthy';
    if (s.health === 'degraded') return 'hp-degraded';
    if (s.health === 'unreachable') return 'hp-unreachable';
    return 'hp-unknown';
  }

  function svcRowClass(s) {
    if (s.status === 'running' && s.health === 'healthy') return 'running';
    if (s.status === 'error' || s.status === 'failed') return 'error';
    if (s.health === 'degraded') return 'degraded';
    if (s.status === 'stopped') return 'stopped';
    if (s.status === 'starting' || s.status === 'stopping') return 'starting';
    return '';
  }

  function icoClass(s) {
    const appId = s.appId || '';
    if (appId === 'containers') return 'ico-docker';
    if (appId === 'nimtorrent' || appId === 'nimbackup') return 'ico-daemon';
    return 'ico-system';
  }

  function statusbarDotClass() {
    if (errorCount > 0) return 'dot-err';
    if (alertCount > 0) return 'dot-warn';
    return 'dot-ok';
  }

  onMount(() => {
    loadServices();
    loadMetrics();
    pollInterval = setInterval(() => {
      loadServices();
      loadMetrics();
    }, 5000);
  });

  onDestroy(() => {
    if (pollInterval) clearInterval(pollInterval);
  });
</script>

<div class="health-root">
  <!-- ═ SIDEBAR ═ -->
  <div class="sidebar">
    <div class="sb-header">
      <svg viewBox="0 0 24 24" fill="none" stroke="var(--accent)" stroke-width="2" stroke-linecap="round" style="width:16px;height:16px;flex-shrink:0"><path d="M22 12h-4l-3 9L9 3l-3 9H2"/></svg>
      <span class="title">NimHealth</span>
    </div>

    <div class="sb-section">Vista</div>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={filter==='all'} on:click={() => { filter='all'; if(view!=='dashboard') goBack(); }}>
      <svg viewBox="0 0 24 24"><rect x="3" y="3" width="7" height="7" rx="1"/><rect x="14" y="3" width="7" height="7" rx="1"/><rect x="3" y="14" width="7" height="7" rx="1"/><rect x="14" y="14" width="7" height="7" rx="1"/></svg>
      Todos {#if services.length > 0}<span class="sb-badge">{services.length}</span>{/if}
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={filter==='running'} on:click={() => { filter='running'; if(view!=='dashboard') goBack(); }}>
      <svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><polyline points="10 8 16 12 10 16 10 8"/></svg>
      Activos {#if runningCount > 0}<span class="sb-badge">{runningCount}</span>{/if}
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={filter==='error'} on:click={() => { filter='error'; if(view!=='dashboard') goBack(); }}>
      <svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><line x1="12" y1="8" x2="12" y2="12"/><line x1="12" y1="16" x2="12.01" y2="16"/></svg>
      Errores {#if errorCount > 0}<span class="sb-badge red">{errorCount}</span>{/if}
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={filter==='alert'} on:click={() => { filter='alert'; if(view!=='dashboard') goBack(); }}>
      <svg viewBox="0 0 24 24"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
      Alertas {#if alertCount > 0}<span class="sb-badge amber">{alertCount}</span>{/if}
    </div>

    {#if pools.length > 0}
      <div class="sb-section" style="margin-top:6px">Por pool</div>
      {#each pools as pool}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="sb-item" on:click={() => { filter='all'; if(view!=='dashboard') goBack(); }}>
          <svg viewBox="0 0 24 24"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>
          {pool}
        </div>
      {/each}
    {/if}
  </div>

  <!-- ═ INNER WRAP ═ -->
  <div class="inner-wrap">
    <div class="inner">

      {#if view === 'dashboard'}
        <!-- ══ DASHBOARD ══ -->
        <div class="inner-titlebar">
          <span class="tb-title">NimHealth</span>
          <span class="tb-sub">— System Overview</span>
          <div class="tb-right">
            <button class="icon-btn" title="Refrescar" on:click={() => { loadServices(); loadMetrics(); }}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.18-5.4"/></svg>
            </button>
          </div>
        </div>

        <div class="content">
          <!-- Metrics -->
          <div class="metrics-row">
            <div class="metric-card">
              <div class="mc-label">CPU</div>
              <div class="mc-val">{cpu.percent}%</div>
              <div class="mc-sub">{cpu.cores} cores · load {cpu.load}</div>
              <div class="mc-graph">
                {#each cpuHistory as v}<div class="mc-bar" style="height:{Math.max(3,Math.round(v/100*24))}px;background:var(--accent)"></div>{/each}
              </div>
            </div>
            <div class="metric-card">
              <div class="mc-label">RAM</div>
              <div class="mc-val">{fmtBytes(ram.used)}</div>
              <div class="mc-sub">de {fmtBytes(ram.total)} · {ram.percent}%</div>
              <div class="mc-graph">
                {#each ramHistory as v}<div class="mc-bar" style="height:{Math.max(3,Math.round(v/100*24))}px;background:var(--blue)"></div>{/each}
              </div>
            </div>
            <div class="metric-card">
              <div class="mc-label">Disco I/O</div>
              <div class="mc-val">{fmtSpeed(diskIO.read + diskIO.write)} MB/s</div>
              <div class="mc-sub">↑ {fmtSpeed(diskIO.write)} ↓ {fmtSpeed(diskIO.read)} MB/s</div>
              <div class="mc-graph">
                {#each diskHistory as v}<div class="mc-bar" style="height:{Math.max(3,Math.round(v/100*24))}px;background:var(--amber)"></div>{/each}
              </div>
            </div>
            <div class="metric-card">
              <div class="mc-label">Red</div>
              <div class="mc-val">{fmtSpeed(netIO.rx + netIO.tx)} MB/s</div>
              <div class="mc-sub">↑ {fmtSpeed(netIO.tx)} ↓ {fmtSpeed(netIO.rx)} MB/s</div>
              <div class="mc-graph">
                {#each netHistory as v}<div class="mc-bar" style="height:{Math.max(3,Math.round(v/100*24))}px;background:var(--green)"></div>{/each}
              </div>
            </div>
          </div>

          <!-- Services -->
          <div class="section-label">Servicios</div>
          <div class="svc-list">
            {#each filteredServices as svc}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="svc-row {svcRowClass(svc)}" on:click={() => loadDetail(svc)}>
                <div class="svc-ico {icoClass(svc)}">
                  {#if svc.appId === 'containers'}
                    <svg viewBox="0 0 24 24"><rect x="2" y="7" width="20" height="14" rx="2"/><path d="M16 7V5a2 2 0 0 0-2-2h-4a2 2 0 0 0-2 2v2"/></svg>
                  {:else if svc.appId === 'nimtorrent'}
                    <svg viewBox="0 0 24 24"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
                  {:else if svc.appId === 'nimbackup'}
                    <svg viewBox="0 0 24 24"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                  {:else}
                    <svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14"/><path d="M4.93 4.93a10 10 0 0 0 0 14.14"/></svg>
                  {/if}
                </div>
                <span class="svc-name">{svc.appName || svc.appId}</span>
                <span class="svc-pool">{svc.id}</span>
                <div class="status-dot {statusDotClass(svc)}"></div>
                <span class="status-lbl">{svc.status}</span>
                <span class="health-pill {healthPillClass(svc)}">{svc.health}</span>
                <div class="svc-chevron"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="9 18 15 12 9 6"/></svg></div>
              </div>
            {:else}
              <div class="empty-hint">No hay servicios registrados</div>
            {/each}
          </div>
        </div>

        <div class="statusbar">
          <div class="status-dot {statusbarDotClass()}"></div>
          <span>{runningCount} activos</span><span class="st-sep">·</span>
          <span>{stoppedCount} detenidos</span>
          {#if errorCount > 0}<span class="st-sep">·</span><span style="color:var(--red)">{errorCount} errores</span>{/if}
          {#if alertCount > 0}<span class="st-sep">·</span><span style="color:var(--amber)">{alertCount} alertas</span>{/if}
        </div>

      {:else if view === 'detail' && selectedService}
        <!-- ══ DETAIL ══ -->
        <div class="inner-titlebar">
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <span class="nav-back" on:click={goBack}>‹</span>
          <span class="tb-title">{selectedService.appName || selectedService.appId}</span>
          <div class="tb-right">
            <button class="icon-btn" title="Refrescar" on:click={() => { loadServices(); if(selectedService) selectedService = services.find(s => s.id === selectedService.id) || selectedService; }}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.18-5.4"/></svg>
            </button>
          </div>
        </div>

        <div class="content">
          <!-- Hero -->
          <div class="detail-hero">
            <div class="svc-ico {icoClass(selectedService)}" style="width:42px;height:42px;border-radius:11px">
              {#if selectedService.appId === 'containers'}
                <svg viewBox="0 0 24 24" style="width:18px;height:18px"><rect x="2" y="7" width="20" height="14" rx="2"/><path d="M16 7V5a2 2 0 0 0-2-2h-4a2 2 0 0 0-2 2v2"/></svg>
              {:else if selectedService.appId === 'nimtorrent'}
                <svg viewBox="0 0 24 24" style="width:18px;height:18px"><polyline points="16 18 22 12 16 6"/><polyline points="8 6 2 12 8 18"/></svg>
              {:else if selectedService.appId === 'nimbackup'}
                <svg viewBox="0 0 24 24" style="width:18px;height:18px"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
              {:else}
                <svg viewBox="0 0 24 24" style="width:18px;height:18px"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14"/><path d="M4.93 4.93a10 10 0 0 0 0 14.14"/></svg>
              {/if}
            </div>
            <div class="hero-info">
              <div class="hero-name">{selectedService.appName || selectedService.appId}</div>
              <div class="hero-id">{selectedService.id}</div>
              <div class="hero-status">
                <div class="status-dot {statusDotClass(selectedService)}"></div>
                <span class="status-lbl">{selectedService.status}</span>
                <span class="health-pill {healthPillClass(selectedService)}">{selectedService.health}</span>
              </div>
            </div>
          </div>

          <!-- State -->
          <div class="d-block">
            <div class="d-block-title">Estado</div>
            <div class="d-row"><span class="d-key">Pool</span><span class="d-val">{selectedService.poolName}</span></div>
            <div class="d-row"><span class="d-key">Path</span><span class="d-val" style="font-size:9px">{selectedService.path}</span></div>
            <div class="d-row"><span class="d-key">Owner</span><span class="d-val">{selectedService.owner || 'system'}</span></div>
          </div>

          <!-- Dependencies -->
          {#if selectedService.dependencies?.length > 0}
            <div class="d-block">
              <div class="d-block-title">Dependencias</div>
              <div class="deps-list">
                {#each selectedService.dependencies as dep}
                  <div class="dep-row">
                    <span class="dep-type dep-{dep.depType}">{dep.depType}</span>
                    <span class="dep-target">{dep.target}</span>
                    <span class="dep-req">{dep.required}</span>
                  </div>
                {/each}
              </div>
            </div>
          {/if}

          <!-- Actions -->
          <div class="actions-row">
            {#if selectedService.status === 'running' || selectedService.status === 'starting'}
              <button class="act-btn act-stop" disabled={stopping[selectedService.id+':stop']} on:click={() => doAction(selectedService, 'stop')}>
                {stopping[selectedService.id+':stop'] ? 'Deteniendo...' : 'Detener'}
              </button>
              <button class="act-btn act-restart" disabled={stopping[selectedService.id+':restart']} on:click={() => doAction(selectedService, 'restart')}>
                {stopping[selectedService.id+':restart'] ? 'Reiniciando...' : 'Reiniciar'}
              </button>
            {:else}
              <button class="act-btn act-start" disabled={stopping[selectedService.id+':start'] || selectedService.status === 'error'} on:click={() => doAction(selectedService, 'start')}>
                {stopping[selectedService.id+':start'] ? 'Iniciando...' : 'Iniciar'}
              </button>
            {/if}
          </div>

          <!-- Logs -->
          {#if detailLogs.length > 0}
            <div class="d-block">
              <div class="d-block-title">
                Logs recientes
                <button class="log-refresh" on:click={() => loadLogs(selectedService)}>
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="width:10px;height:10px"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.18-5.4"/></svg>
                </button>
              </div>
              <div class="log-wrap">
                {#each detailLogs as log}
                  <div class="log-line"><span class="log-ts">{log.timestamp}</span>{log.message}</div>
                {/each}
              </div>
            </div>
          {/if}
        </div>

        <div class="statusbar">
          <div class="status-dot {selectedService.status === 'running' ? 'dot-ok' : selectedService.status === 'error' ? 'dot-err' : 'dot-warn'}"></div>
          <span>{selectedService.status} · {selectedService.health}</span>
        </div>
      {/if}

    </div>
  </div>
</div>

<style>
  .health-root { width:100%; height:100%; display:flex; overflow:hidden; font-family:'Inter',-apple-system,sans-serif; color:var(--text-1); }

  /* Sidebar */
  .sidebar { width:190px; flex-shrink:0; display:flex; flex-direction:column; gap:2px; padding:12px 8px; overflow-y:auto; }
  .sidebar::-webkit-scrollbar { width:3px; } .sidebar::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.2); border-radius:2px; }
  .sb-header { display:flex; align-items:center; gap:8px; padding:32px 8px 12px; }
  .title { font-size:15px; font-weight:600; color:var(--text-1); }
  .sb-section { font-size:9px; font-weight:700; letter-spacing:.1em; text-transform:uppercase; color:var(--text-3); padding:10px 8px 3px; }
  .sb-item { display:flex; align-items:center; gap:8px; padding:6px 10px; border-radius:8px; cursor:pointer; font-size:12px; color:var(--text-2); border:1px solid transparent; transition:all .15s; }
  .sb-item svg { width:13px; height:13px; flex-shrink:0; opacity:.6; stroke:currentColor; fill:none; stroke-width:2; stroke-linecap:round; }
  .sb-item:hover { background:rgba(128,128,128,0.10); color:var(--text-1); }
  .sb-item.active { background:var(--active-bg); color:var(--text-1); border-color:var(--border-hi); }
  .sb-item.active svg { opacity:1; }
  .sb-badge { margin-left:auto; font-size:9px; font-weight:700; background:var(--active-bg); color:var(--accent); border-radius:5px; padding:1px 5px; }
  .sb-badge.red { background:rgba(248,113,113,0.15); color:var(--red); }
  .sb-badge.amber { background:rgba(251,191,36,0.12); color:var(--amber); }

  /* Inner wrap */
  .inner-wrap { flex:1; padding:8px; display:flex; }
  .inner { flex:1; border-radius:10px; border:1px solid var(--border); background:var(--bg-inner); display:flex; flex-direction:column; overflow:hidden; }

  /* Titlebar */
  .inner-titlebar { display:flex; align-items:center; gap:8px; padding:10px 14px 9px; background:var(--bg-bar); flex-shrink:0; border-bottom:1px solid var(--border); }
  .tb-title { font-size:12px; font-weight:600; color:var(--text-1); }
  .tb-sub { font-size:11px; color:var(--text-3); }
  .tb-right { margin-left:auto; display:flex; align-items:center; gap:6px; }
  .nav-back { font-size:18px; cursor:pointer; color:var(--text-2); padding:0 4px; border-radius:6px; transition:all .15s; line-height:1; }
  .nav-back:hover { background:var(--ibtn-bg); color:var(--text-1); }

  /* Content */
  .content { flex:1; overflow-y:auto; padding:14px; display:flex; flex-direction:column; gap:10px; min-height:0; }
  .content::-webkit-scrollbar { width:3px; } .content::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }

  /* Metrics */
  .metrics-row { display:grid; grid-template-columns:repeat(4,1fr); gap:8px; }
  .metric-card { background:rgba(255,255,255,0.025); border:1px solid var(--border); border-radius:10px; padding:10px 12px; }
  .mc-label { font-size:9px; font-weight:600; color:var(--text-3); letter-spacing:.07em; text-transform:uppercase; margin-bottom:4px; }
  .mc-val { font-size:18px; font-weight:600; color:var(--text-1); line-height:1; }
  .mc-sub { font-size:9px; color:var(--text-3); margin-top:2px; font-family:'DM Mono',monospace; }
  .mc-graph { height:24px; display:flex; align-items:flex-end; gap:2px; margin-top:6px; }
  .mc-bar { flex:1; border-radius:2px; min-height:3px; transition:height .3s; }

  /* Section */
  .section-label { font-size:9px; font-weight:700; color:var(--text-3); text-transform:uppercase; letter-spacing:.08em; }

  /* Service rows */
  .svc-list { display:flex; flex-direction:column; gap:4px; }
  .svc-row { display:flex; align-items:center; gap:10px; padding:9px 12px; background:rgba(255,255,255,0.025); border:1px solid var(--border); border-radius:9px; cursor:pointer; transition:all .12s; border-left:3px solid transparent; }
  .svc-row:hover { border-color:var(--border-hi); }
  .svc-row.running  { border-left-color:var(--green); }
  .svc-row.stopped  { border-left-color:var(--border); }
  .svc-row.error    { border-left-color:var(--red); }
  .svc-row.starting, .svc-row.degraded { border-left-color:var(--amber); }
  .svc-ico { width:30px; height:30px; border-radius:8px; display:flex; align-items:center; justify-content:center; flex-shrink:0; }
  .svc-ico svg { width:13px; height:13px; stroke:currentColor; fill:none; stroke-width:2; stroke-linecap:round; }
  .ico-daemon { background:rgba(124,111,255,0.12); color:var(--accent); }
  .ico-docker { background:rgba(96,165,250,0.12); color:var(--blue); }
  .ico-system { background:rgba(251,191,36,0.12); color:var(--amber); }
  .svc-name { font-size:12px; font-weight:600; color:var(--text-1); min-width:100px; }
  .svc-pool { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .status-dot { width:6px; height:6px; border-radius:50%; flex-shrink:0; }
  .dot-running, .dot-ok { background:var(--green); box-shadow:0 0 5px var(--green); }
  .dot-stopped { background:var(--text-3); }
  .dot-error, .dot-err { background:var(--red); }
  .dot-starting, .dot-warn { background:var(--amber); animation:pulse .8s ease-in-out infinite; }
  @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:.35} }
  .status-lbl { font-size:10px; color:var(--text-2); font-family:'DM Mono',monospace; min-width:56px; }
  .health-pill { font-size:9px; font-weight:600; padding:2px 6px; border-radius:5px; min-width:60px; text-align:center; }
  .hp-healthy { background:rgba(74,222,128,0.12); color:var(--green); }
  .hp-degraded { background:rgba(251,191,36,0.12); color:var(--amber); }
  .hp-unknown { background:rgba(255,255,255,0.06); color:var(--text-3); }
  .hp-unreachable { background:rgba(248,113,113,0.12); color:var(--red); }
  .svc-chevron { color:var(--text-3); flex-shrink:0; }
  .svc-chevron svg { width:12px; height:12px; }

  /* Detail hero */
  .detail-hero { display:flex; align-items:center; gap:12px; padding:12px; background:rgba(255,255,255,0.025); border:1px solid var(--border); border-radius:10px; }
  .hero-info { flex:1; }
  .hero-name { font-size:15px; font-weight:600; color:var(--text-1); }
  .hero-id { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; margin-top:2px; }
  .hero-status { display:flex; align-items:center; gap:6px; margin-top:5px; }

  /* Detail blocks */
  .d-block { background:rgba(255,255,255,0.025); border:1px solid var(--border); border-radius:10px; padding:12px 14px; }
  .d-block-title { font-size:9px; font-weight:600; color:var(--text-3); letter-spacing:.07em; text-transform:uppercase; margin-bottom:8px; }
  .d-row { display:flex; align-items:center; justify-content:space-between; padding:3px 0; }
  .d-row + .d-row { border-top:1px solid var(--border); }
  .d-key { font-size:11px; color:var(--text-2); }
  .d-val { font-size:11px; color:var(--text-1); font-family:'DM Mono',monospace; }

  /* Dependencies */
  .deps-list { display:flex; flex-direction:column; gap:5px; }
  .dep-row { display:flex; align-items:center; gap:8px; padding:7px 10px; background:var(--ibtn-bg); border:1px solid var(--border); border-radius:7px; }
  .dep-type { font-size:9px; font-weight:700; padding:2px 6px; border-radius:4px; min-width:36px; text-align:center; }
  .dep-pool { background:rgba(124,111,255,0.12); color:var(--accent); }
  .dep-share { background:rgba(251,191,36,0.12); color:var(--amber); }
  .dep-path { background:rgba(96,165,250,0.12); color:var(--blue); }
  .dep-target { font-size:10px; color:var(--text-2); font-family:'DM Mono',monospace; flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .dep-req { font-size:9px; color:var(--text-3); margin-left:auto; white-space:nowrap; }

  /* Actions */
  .actions-row { display:flex; gap:8px; }
  .act-btn { flex:1; padding:9px; border:none; border-radius:8px; font-family:inherit; font-size:12px; font-weight:600; cursor:pointer; transition:opacity .15s; }
  .act-btn:hover { opacity:.82; }
  .act-btn:disabled { opacity:.3; cursor:not-allowed; }
  .act-stop { background:rgba(248,113,113,0.12); color:var(--red); }
  .act-restart { background:var(--ibtn-bg); color:var(--text-2); }
  .act-start { background:rgba(74,222,128,0.12); color:var(--green); }

  /* Statusbar */
  .statusbar { display:flex; align-items:center; gap:8px; padding:9px 14px; border-top:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; font-size:10px; color:var(--text-3); border-radius:0 0 10px 10px; font-family:'DM Mono',monospace; }
  .st-sep { color:rgba(255,255,255,0.1); }

  /* Icon btn */
  .icon-btn { width:27px; height:27px; background:var(--ibtn-bg); border:1px solid var(--border); border-radius:6px; display:flex; align-items:center; justify-content:center; cursor:pointer; color:var(--text-2); transition:all .15s; }
  .icon-btn svg { width:13px; height:13px; }
  .icon-btn:hover { background:rgba(124,111,255,0.15); color:var(--text-1); }

  /* Empty */
  .empty-hint { text-align:center; padding:28px; border:1px dashed var(--border); border-radius:9px; color:var(--text-3); font-size:11px; }

  /* Logs */
  .log-wrap { background:rgba(0,0,0,0.2); border-radius:7px; padding:10px; font-family:'DM Mono',monospace; font-size:9px; color:var(--text-2); line-height:1.7; max-height:140px; overflow-y:auto; }
  .log-wrap::-webkit-scrollbar { width:2px; }
  .log-wrap::-webkit-scrollbar-thumb { background:var(--border); border-radius:2px; }
  .log-line { white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .log-ts { color:var(--text-3); margin-right:8px; }
  .log-refresh { background:none; border:none; cursor:pointer; color:var(--text-3); padding:2px; margin-left:6px; vertical-align:middle; transition:color .15s; }
  .log-refresh:hover { color:var(--text-1); }
</style>
