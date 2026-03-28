<script>
  import { onMount, onDestroy } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';
  import NimLink from '$lib/apps/NimLink.svelte';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}`, 'Content-Type': 'application/json' });

  // ── Estado ──
  let view = 'resumen';       // 'resumen' | 'historial' | 'device'
  let devices = [];
  let jobs = [];
  let history = [];
  let snapshots = [];
  let activeDevice = null;    // dispositivo seleccionado
  let devTab = 'proposito';   // mantenido por compatibilidad
  let slideView = null;       // null | 'share' | 'backup-dest' | 'backup-src' | 'sync'
  let loading = false;

  // ── Wizard modals ──
  let showWizard = false;
  let wizardMode = 'pair';  // 'pair' | 'job' | 'sync'

  // ── Iconos SVG por tipo de dispositivo ──
  // Fácil de reemplazar — solo cambia el SVG aquí
  const DEVICE_ICONS = {
    nas: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
      <rect x="2" y="3" width="20" height="8" rx="2"/>
      <rect x="2" y="13" width="20" height="8" rx="2"/>
      <circle cx="18" cy="7" r="1" fill="currentColor" stroke="none"/>
      <circle cx="18" cy="17" r="1" fill="currentColor" stroke="none"/>
    </svg>`,
    usb: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
      <rect x="4" y="2" width="16" height="20" rx="2"/>
      <line x1="8" y1="7" x2="16" y2="7"/>
      <line x1="8" y1="11" x2="16" y2="11"/>
      <circle cx="12" cy="17" r="1.5"/>
    </svg>`,
    server: `<svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
      <rect x="2" y="2" width="20" height="5" rx="1"/>
      <rect x="2" y="9" width="20" height="5" rx="1"/>
      <rect x="2" y="16" width="20" height="5" rx="1"/>
      <circle cx="19" cy="4.5" r=".8" fill="currentColor" stroke="none"/>
      <circle cx="19" cy="11.5" r=".8" fill="currentColor" stroke="none"/>
      <circle cx="19" cy="18.5" r=".8" fill="currentColor" stroke="none"/>
    </svg>`,
  };

  // ── Detectar si es local (mismo segmento) o remoto ──
  function isLocal(addr) {
    return addr.startsWith('192.168.') || addr.startsWith('10.') || addr.startsWith('172.') || addr === 'localhost';
  }

  function devicePort(addr) {
    return isLocal(addr) ? 5000 : 5009;
  }

  function deviceProto(addr) {
    return isLocal(addr) ? 'http' : 'https';
  }

  // ── API calls ──
  async function loadDevices() {
    try {
      const r = await fetch('/api/backup/devices', { headers: hdrs() });
      const d = await r.json();
      devices = d.devices || [];
    } catch { devices = []; }
  }

  async function loadJobs() {
    try {
      const r = await fetch('/api/backup/jobs', { headers: hdrs() });
      const d = await r.json();
      jobs = d.jobs || [];
    } catch { jobs = []; }
  }

  async function loadHistory() {
    try {
      const r = await fetch('/api/backup/history', { headers: hdrs() });
      const d = await r.json();
      history = d.history || [];
    } catch { history = []; }
  }

  async function runJob(jobId) {
    try {
      await fetch(`/api/backup/run/${jobId}`, { method: 'POST', headers: hdrs() });
      await loadJobs();
    } catch {}
  }

  async function removeDevice(id) {
    if (!confirm('¿Desemparejar este dispositivo?')) return;
    try {
      await fetch(`/api/backup/devices/${id}`, { method: 'DELETE', headers: hdrs() });
      devices = devices.filter(d => d.id !== id);
      activeDevice = null;
      view = 'resumen';
    } catch {}
  }

  async function savePurposes(deviceId, purposes) {
    try {
      await fetch(`/api/backup/devices/${deviceId}/purposes`, {
        method: 'POST', headers: hdrs(),
        body: JSON.stringify({ purposes })
      });
    } catch {}
  }

  // ── Remote Shares ──
  let remoteShares = [];
  let sharesLoading = false;

  async function loadRemoteShares(deviceId) {
    sharesLoading = true;
    try {
      const r = await fetch(`/api/backup/devices/${deviceId}/remote-shares`, { headers: hdrs() });
      const d = await r.json();
      remoteShares = d.shares || [];
    } catch { remoteShares = []; }
    sharesLoading = false;
  }

  async function mountShare(deviceId, share) {
    share._mounting = true;
    remoteShares = [...remoteShares];
    try {
      const r = await fetch(`/api/backup/devices/${deviceId}/mount`, {
        method: 'POST', headers: hdrs(),
        body: JSON.stringify({ shareName: share.name, remotePath: share.path })
      });
      const d = await r.json();
      if (d.ok) {
        share.mounted = true;
        share.mountPoint = d.mountPoint;
      }
    } catch {}
    share._mounting = false;
    remoteShares = [...remoteShares];
  }

  async function unmountShare(deviceId, share) {
    share._mounting = true;
    remoteShares = [...remoteShares];
    try {
      const r = await fetch(`/api/backup/devices/${deviceId}/unmount`, {
        method: 'POST', headers: hdrs(),
        body: JSON.stringify({ shareName: share.name })
      });
      const d = await r.json();
      if (d.ok) {
        share.mounted = false;
        share.mountPoint = '';
      }
    } catch {}
    share._mounting = false;
    remoteShares = [...remoteShares];
  }

  // ── Helpers ──
  function fmtTime(iso) {
    if (!iso) return '—';
    const d = new Date(iso);
    const now = new Date();
    const diff = Math.floor((now - d) / 1000);
    if (diff < 3600) return `hace ${Math.floor(diff/60)}m`;
    if (diff < 86400) return `hace ${Math.floor(diff/3600)}h`;
    return `hace ${Math.floor(diff/86400)}d`;
  }

  function fmtSize(bytes) {
    if (!bytes) return '—';
    if (bytes >= 1e9) return (bytes/1e9).toFixed(1) + ' GB';
    if (bytes >= 1e6) return (bytes/1e6).toFixed(0) + ' MB';
    return (bytes/1e3).toFixed(0) + ' KB';
  }

  $: onlineCount = devices.filter(d => d.online).length;
  $: jobsOk = jobs.filter(j => j.status === 'ok').length;
  $: nextJob = jobs.filter(j => j.nextRun).sort((a,b) => new Date(a.nextRun) - new Date(b.nextRun))[0];

  onMount(() => {
    loadDevices();
    loadJobs();
    loadHistory();
  });
</script>

<div class="backup-root">

  <!-- ══ SIDEBAR ══ -->
  <div class="sidebar">
    <div class="sb-header">
      <div class="sb-logo">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
          <polyline points="7 10 12 15 17 10"/>
          <line x1="12" y1="15" x2="12" y2="3"/>
        </svg>
      </div>
      <span class="title">NimBackup</span>
    </div>

    <div class="sb-section">General</div>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={view === 'resumen' || view === 'historial' ? view === 'resumen' : false}
      on:click={() => { view = 'resumen'; activeDevice = null; }}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/></svg>
      <span>Resumen</span>
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={view === 'historial'}
      on:click={() => { view = 'historial'; activeDevice = null; loadHistory(); }}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
      <span>Historial</span>
    </div>

    <div class="sb-section" style="margin-top:6px">Dispositivos</div>

    {#each devices as dev}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="sb-device" class:active={activeDevice?.id === dev.id}
        on:click={() => { activeDevice = dev; view = 'device'; devTab = 'proposito'; }}>
        <div class="sb-dev-icon">
          {@html DEVICE_ICONS[dev.type] || DEVICE_ICONS.nas}
        </div>
        <div class="sb-dev-info">
          <div class="sb-dev-name">{dev.name}</div>
          <div class="sb-dev-meta">{dev.addr}</div>
        </div>
        <div class="dot" class:dot-on={dev.online} class:dot-off={!dev.online}></div>
      </div>
    {/each}

    {#if devices.length === 0}
      <div style="font-size:11px;color:var(--text-3);padding:8px 10px">Sin dispositivos</div>
    {/if}

    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-add" on:click={() => { wizardMode = 'pair'; showWizard = true; }}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
      Emparejar dispositivo
    </div>

    {#if nextJob}
      <div class="sb-next">
        <div class="sn-label">Próximo backup</div>
        <div class="sn-name">{nextJob.name}</div>
        <div class="sn-time">{fmtTime(nextJob.nextRun)}</div>
      </div>
    {/if}
  </div>

  <!-- ══ INNER ══ -->
  <div class="inner-wrap">
    <div class="inner">

      <!-- ── RESUMEN ── -->
      {#if view === 'resumen'}
        <div class="inner-titlebar">
          <span class="tb-title">Resumen</span>
          <span class="tb-sub">— {onlineCount} de {devices.length} dispositivos online</span>
          <div class="tb-right">
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <button class="btn-secondary" on:click={() => jobs.forEach(j => runJob(j.id))}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.18-5.4"/></svg>
              Ejecutar todo
            </button>
          </div>
        </div>
        <div class="content">
          <div>
            <div class="section-label">Estado general</div>
            <div class="stats-row">
              <div class="stat-card">
                <div class="stat-label">Dispositivos</div>
                <div class="stat-val" style="color:var(--green)">{onlineCount}/{devices.length}</div>
                <div class="stat-sub">online</div>
              </div>
              <div class="stat-card">
                <div class="stat-label">Trabajos OK</div>
                <div class="stat-val" style="color:var(--accent)">{jobsOk}/{jobs.length}</div>
                <div class="stat-sub">activos</div>
              </div>
              <div class="stat-card">
                <div class="stat-label">Snapshots</div>
                <div class="stat-val" style="color:var(--accent)">{snapshots.length}</div>
                <div class="stat-sub">activos</div>
              </div>
              <div class="stat-card">
                <div class="stat-label">Último backup</div>
                <div class="stat-val" style="font-size:12px">{history.length > 0 ? fmtTime(history[0]?.time) : '—'}</div>
                <div class="stat-sub">{history[0]?.jobName || '—'}</div>
              </div>
            </div>
          </div>
          <div>
            <div class="section-label">Trabajos activos</div>
            {#each jobs as job}
              <div class="row">
                <div class="row-icon" style="background:{job.fsType === 'btrfs' ? 'rgba(74,222,128,0.1)' : 'rgba(96,165,250,0.1)'}">
                  {#if job.fsType === 'btrfs'}
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" style="width:14px;height:14px;color:var(--green)"><path d="M12 2L2 7l10 5 10-5-10-5z"/><path d="M2 17l10 5 10-5"/><path d="M2 12l10 5 10-5"/></svg>
                  {:else}
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" style="width:14px;height:14px;color:var(--blue)"><rect x="2" y="3" width="20" height="8" rx="2"/><circle cx="18" cy="7" r="1" fill="currentColor" stroke="none"/></svg>
                  {/if}
                </div>
                <div class="row-info">
                  <div class="row-name">{job.name}</div>
                  <div class="row-meta">{job.fsType} incremental · {job.schedule} · retención {job.retention}</div>
                </div>
                <div class="row-status">
                  <div class="dot" class:dot-on={job.status === 'ok'} class:dot-warn={job.status === 'warn'} class:dot-err={job.status === 'error'}></div>
                  <span style="color:{job.status === 'ok' ? 'var(--green)' : job.status === 'warn' ? 'var(--amber)' : 'var(--red)'}">
                    {job.status === 'ok' ? 'OK' : job.status === 'warn' ? 'Advertencia' : 'Error'} · {fmtTime(job.lastRun)}
                  </span>
                </div>
                <div class="row-actions">
                  <!-- svelte-ignore a11y_click_events_have_key_events -->
                  <!-- svelte-ignore a11y_no_static_element_interactions -->
                  <button class="btn-secondary" style="padding:3px 8px;font-size:10px" on:click={() => runJob(job.id)}>▶</button>
                </div>
              </div>
            {/each}
            {#if jobs.length === 0}
              <div class="empty-hint">Sin trabajos configurados.<br>Empareja un dispositivo y configura un trabajo de backup.</div>
            {/if}
          </div>
        </div>
        <div class="statusbar">
          <div class="status-dot"></div>
          <span>{onlineCount} dispositivos online</span>
          <span class="st-sep">·</span>
          <span>{jobs.length} trabajos</span>
          {#if nextJob}
            <div class="st-right">Próximo: {fmtTime(nextJob.nextRun)}</div>
          {/if}
        </div>

      <!-- ── HISTORIAL ── -->
      {:else if view === 'historial'}
        <div class="inner-titlebar">
          <span class="tb-title">Historial</span>
          <span class="tb-sub">— Últimas ejecuciones</span>
        </div>
        <div class="content">
          {#each history as h}
            <div class="act-row">
              <div class="act-ico" class:ok={h.ok} class:err={!h.ok}>
                {#if h.ok}
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="20 6 9 17 4 12"/></svg>
                {:else}
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                {/if}
              </div>
              <span class="act-desc" style="color:{h.ok ? 'var(--text-2)' : 'var(--red)'}">{h.jobName}</span>
              <span class="act-dest">{h.dest}</span>
              <span class="act-size">{fmtSize(h.bytes)}</span>
              <span class="act-time">{fmtTime(h.time)}</span>
            </div>
          {/each}
          {#if history.length === 0}
            <div class="empty-hint">Sin historial todavía.</div>
          {/if}
        </div>
        <div class="statusbar">
          <div class="status-dot"></div>
          <span>{history.length} ejecuciones registradas</span>
        </div>

      <!-- ── DISPOSITIVO ── -->
      {:else if view === 'device' && activeDevice}
        <!-- Titlebar -->
        <div class="inner-titlebar">
          <div class="dev-header-icon">
            {@html DEVICE_ICONS[activeDevice.type] || DEVICE_ICONS.nas}
          </div>
          <div>
            <span class="tb-title">{activeDevice.name}</span>
            <span class="tb-sub"> — {activeDevice.addr}:{devicePort(activeDevice.addr)}</span>
          </div>
          <div class="dev-online" class:online={activeDevice.online}>
            <div class="dot" class:dot-on={activeDevice.online} class:dot-off={!activeDevice.online}></div>
            {activeDevice.online ? `Online · ${activeDevice.ping || '—'}` : 'Offline'}
          </div>
          <div class="tb-right">
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <button class="icon-btn" style="color:var(--red)" on:click={() => removeDevice(activeDevice.id)}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M10 13a5 5 0 0 0 7.54.54l3-3a5 5 0 0 0-7.07-7.07l-1.72 1.71"/><path d="M14 11a5 5 0 0 0-7.54-.54l-3 3a5 5 0 0 0 7.07 7.07l1.71-1.71"/><line x1="2" y1="2" x2="22" y2="22" style="opacity:.5"/></svg>
            </button>
          </div>
        </div>

        <!-- Slide container -->
        <div class="dev-slider" class:slide={slideView !== null}>

          <!-- PANE 1: vista principal del dispositivo -->
          <div class="dev-pane">
            <div class="content">

              <!-- Stats remotas -->
              <div>
                <div class="section-label">Conexión</div>
                <div class="stats-row">
                  <div class="stat-card">
                    <div class="stat-label">Latencia</div>
                    <div class="stat-val" style="color:var(--green)">{activeDevice.ping || '—'}</div>
                    <div class="stat-sub">{isLocal(activeDevice.addr) ? 'LAN directa' : 'WireGuard'}</div>
                  </div>
                  <div class="stat-card">
                    <div class="stat-label">Espacio libre</div>
                    <div class="stat-val" style="color:var(--accent)">{activeDevice.freeSpace || '—'}</div>
                    <div class="stat-sub">disponible</div>
                  </div>
                  <div class="stat-card">
                    <div class="stat-label">Protocolo</div>
                    <div class="stat-val" style="font-size:11px;margin-top:2px">{deviceProto(activeDevice.addr).toUpperCase()}</div>
                    <div class="stat-sub">Puerto {devicePort(activeDevice.addr)}</div>
                  </div>
                  <div class="stat-card">
                    <div class="stat-label">NimOS</div>
                    <div class="stat-val" style="font-size:11px;margin-top:2px">{activeDevice.version || '—'}</div>
                    <div class="stat-sub">versión</div>
                  </div>
                </div>
              </div>

              <!-- Carpetas compartidas (donut de la primera montada) -->
              {#if remoteShares.length > 0}
                {@const mountedShare = remoteShares.find(s => s.mounted) || remoteShares[0]}
                <div>
                  <div class="section-label">Carpetas compartidas</div>
                  <div class="share-donut-card">
                    <div class="donut-wrap">
                      <svg viewBox="0 0 72 72" class="donut-svg">
                        <circle cx="36" cy="36" r="28" fill="none" stroke="rgba(255,255,255,0.07)" stroke-width="9"/>
                        <circle cx="36" cy="36" r="28" fill="none"
                          stroke={mountedShare.usagePercent < 70 ? '#4ade80' : mountedShare.usagePercent < 85 ? '#fbbf24' : '#f87171'}
                          stroke-width="9" stroke-linecap="round"
                          stroke-dasharray="{(mountedShare.usagePercent||0)*1.759} 175.9"
                          transform="rotate(-90 36 36)"/>
                      </svg>
                      <div class="donut-center">
                        <span class="donut-pct">{mountedShare.usagePercent||0}%</span>
                        <span class="donut-lbl">usado</span>
                      </div>
                    </div>
                    <div class="donut-info">
                      <div class="donut-name">{mountedShare.displayName || mountedShare.name}</div>
                      <div class="donut-path">{mountedShare.path}</div>
                      <div class="donut-sizes">{mountedShare.usedFormatted||'—'} · {mountedShare.availableFormatted||'—'} libre</div>
                      {#if mountedShare.mounted}
                        <div class="donut-badge">
                          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" style="width:9px;height:9px"><polyline points="20 6 9 17 4 12"/></svg>
                          Montada en Files
                        </div>
                      {/if}
                    </div>
                  </div>
                </div>
              {/if}

              <!-- Servicios activos -->
              <div>
                <div class="section-label">Servicios activos</div>
                <div class="svc-list">

                  <!-- Share remota -->
                  <div class="svc-row">
                    <div class="svc-ico green">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
                    </div>
                    <div class="svc-info">
                      <div class="svc-name">Share remota</div>
                      <div class="svc-desc">Carpetas de este NAS visibles en Files</div>
                    </div>
                    <div class="svc-actions">
                      <!-- svelte-ignore a11y_click_events_have_key_events -->
                      <!-- svelte-ignore a11y_no_static_element_interactions -->
                      <div class="cfg-btn" on:click={() => { slideView = 'share'; loadRemoteShares(activeDevice.id); }}>
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M4.93 4.93a10 10 0 0 0 0 14.14"/></svg>
                      </div>
                      <button class="toggle" class:on={activeDevice.purposes?.includes('share')}
                        on:click|stopPropagation={() => {
                          const purposes = activeDevice.purposes || [];
                          activeDevice.purposes = purposes.includes('share') ? purposes.filter(x => x !== 'share') : [...purposes, 'share'];
                          activeDevice = {...activeDevice};
                          savePurposes(activeDevice.id, activeDevice.purposes);
                        }}>
                      </button>
                    </div>
                  </div>

                  <!-- Backup destino -->
                  <div class="svc-row">
                    <div class="svc-ico blue">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                    </div>
                    <div class="svc-info">
                      <div class="svc-name">Backup destino</div>
                      <div class="svc-desc">Este NAS recibe tus backups</div>
                    </div>
                    <div class="svc-actions">
                      <!-- svelte-ignore a11y_click_events_have_key_events -->
                      <!-- svelte-ignore a11y_no_static_element_interactions -->
                      <div class="cfg-btn" on:click={() => slideView = 'backup-dest'}>
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M4.93 4.93a10 10 0 0 0 0 14.14"/></svg>
                      </div>
                      <button class="toggle" class:on={activeDevice.purposes?.includes('backup_dest')}
                        on:click|stopPropagation={() => {
                          const purposes = activeDevice.purposes || [];
                          activeDevice.purposes = purposes.includes('backup_dest') ? purposes.filter(x => x !== 'backup_dest') : [...purposes, 'backup_dest'];
                          activeDevice = {...activeDevice};
                          savePurposes(activeDevice.id, activeDevice.purposes);
                        }}>
                      </button>
                    </div>
                  </div>

                  <!-- Backup origen -->
                  <div class="svc-row">
                    <div class="svc-ico amber">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 10 12 15 7 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                    </div>
                    <div class="svc-info">
                      <div class="svc-name">Backup origen</div>
                      <div class="svc-desc">Este NAS hace backup al tuyo</div>
                    </div>
                    <div class="svc-actions">
                      <!-- svelte-ignore a11y_click_events_have_key_events -->
                      <!-- svelte-ignore a11y_no_static_element_interactions -->
                      <div class="cfg-btn" on:click={() => slideView = 'backup-src'}>
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M4.93 4.93a10 10 0 0 0 0 14.14"/></svg>
                      </div>
                      <button class="toggle" class:on={activeDevice.purposes?.includes('backup_src')}
                        on:click|stopPropagation={() => {
                          const purposes = activeDevice.purposes || [];
                          activeDevice.purposes = purposes.includes('backup_src') ? purposes.filter(x => x !== 'backup_src') : [...purposes, 'backup_src'];
                          activeDevice = {...activeDevice};
                          savePurposes(activeDevice.id, activeDevice.purposes);
                        }}>
                      </button>
                    </div>
                  </div>

                  <!-- Sincronización -->
                  <div class="svc-row">
                    <div class="svc-ico purple">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><polyline points="17 1 21 5 17 9"/><path d="M3 11V9a4 4 0 0 1 4-4h14"/><polyline points="7 23 3 19 7 15"/><path d="M21 13v2a4 4 0 0 1-4 4H3"/></svg>
                    </div>
                    <div class="svc-info">
                      <div class="svc-name">Sincronización</div>
                      <div class="svc-desc">Carpetas espejo entre los dos NAS</div>
                    </div>
                    <div class="svc-actions">
                      <!-- svelte-ignore a11y_click_events_have_key_events -->
                      <!-- svelte-ignore a11y_no_static_element_interactions -->
                      <div class="cfg-btn" on:click={() => slideView = 'sync'}>
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M4.93 4.93a10 10 0 0 0 0 14.14"/></svg>
                      </div>
                      <button class="toggle" class:on={activeDevice.purposes?.includes('sync')}
                        on:click|stopPropagation={() => {
                          const purposes = activeDevice.purposes || [];
                          activeDevice.purposes = purposes.includes('sync') ? purposes.filter(x => x !== 'sync') : [...purposes, 'sync'];
                          activeDevice = {...activeDevice};
                          savePurposes(activeDevice.id, activeDevice.purposes);
                        }}>
                      </button>
                    </div>
                  </div>

                </div>
              </div>

            </div>
          </div>

          <!-- PANE 2: vista config del servicio -->
          <div class="dev-pane">
            <!-- Header con volver -->
            <div class="cfg-header">
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="cfg-back" on:click={() => slideView = null}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="15 18 9 12 15 6"/></svg>
              </div>
              <div>
                <div class="cfg-title">
                  {slideView === 'share' ? 'Share remota' : slideView === 'backup-dest' ? 'Backup destino' : slideView === 'backup-src' ? 'Backup origen' : 'Sincronización'}
                </div>
                <div class="cfg-subtitle">{activeDevice.name}</div>
              </div>
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="cfg-add-btn" on:click={() => { wizardMode = slideView === 'sync' ? 'sync' : 'job'; showWizard = true; }}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
                {slideView === 'share' ? 'Añadir carpeta' : slideView === 'sync' ? 'Añadir par' : 'Nuevo trabajo'}
              </div>
            </div>

            <div class="content">

              <!-- Share remota: lista de shares -->
              {#if slideView === 'share'}
                {#if sharesLoading}
                  <div class="empty-hint">Cargando carpetas del dispositivo...</div>
                {:else if remoteShares.length === 0}
                  <div class="empty-hint">
                    No se encontraron carpetas compartidas.<br>
                    <!-- svelte-ignore a11y_click_events_have_key_events -->
                    <!-- svelte-ignore a11y_no_static_element_interactions -->
                    <span style="color:var(--accent);cursor:pointer" on:click={() => loadRemoteShares(activeDevice.id)}>Reintentar</span>
                  </div>
                {:else}
                  {#each remoteShares as share}
                    <div class="row">
                      <div class="row-icon" style="background:rgba(74,222,128,0.1)">
                        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" style="width:14px;height:14px;color:var(--green)"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
                      </div>
                      <div class="row-info">
                        <div class="row-name">{share.displayName || share.name}</div>
                        <div class="row-meta">{share.path}</div>
                        {#if share.mounted && share.mountPoint}
                          <div class="row-meta" style="color:var(--green)">→ {share.mountPoint}</div>
                        {/if}
                      </div>
                      <div class="row-actions">
                        {#if share.mounted}
                          <!-- svelte-ignore a11y_click_events_have_key_events -->
                          <!-- svelte-ignore a11y_no_static_element_interactions -->
                          <button class="btn-secondary" style="padding:3px 10px;font-size:10px"
                            disabled={share._mounting}
                            on:click={() => unmountShare(activeDevice.id, share)}>
                            {share._mounting ? '...' : 'Desmontar'}
                          </button>
                        {:else}
                          <!-- svelte-ignore a11y_click_events_have_key_events -->
                          <!-- svelte-ignore a11y_no_static_element_interactions -->
                          <button class="btn-secondary" style="padding:3px 10px;font-size:10px;color:var(--green);border-color:rgba(74,222,128,0.3)"
                            disabled={share._mounting}
                            on:click={() => mountShare(activeDevice.id, share)}>
                            {share._mounting ? '...' : 'Montar'}
                          </button>
                        {/if}
                      </div>
                    </div>
                  {/each}
                {/if}

              <!-- Backup destino/origen: lista de trabajos -->
              {:else if slideView === 'backup-dest' || slideView === 'backup-src'}
                {#each jobs.filter(j => j.deviceId === activeDevice.id) as job}
                  <div class="row">
                    <div class="row-icon" style="background:{job.fsType === 'btrfs' ? 'rgba(74,222,128,0.1)' : 'rgba(96,165,250,0.1)'}">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" style="width:13px;height:13px;color:{job.fsType === 'btrfs' ? 'var(--green)' : 'var(--blue)'}"><rect x="2" y="3" width="20" height="8" rx="2"/><circle cx="18" cy="7" r="1" fill="currentColor" stroke="none"/></svg>
                    </div>
                    <div class="row-info">
                      <div class="row-name">{job.name}</div>
                      <div class="row-meta">{job.fsType} · {job.schedule} · retención {job.retention}</div>
                    </div>
                    <div class="row-status">
                      <div class="dot" class:dot-on={job.status === 'ok'} class:dot-warn={job.status === 'warn'} class:dot-err={job.status === 'error'}></div>
                      <span style="font-size:10px;color:{job.status === 'ok' ? 'var(--green)' : 'var(--amber)'}">{fmtTime(job.lastRun)}</span>
                    </div>
                    <div class="row-actions">
                      <!-- svelte-ignore a11y_click_events_have_key_events -->
                      <!-- svelte-ignore a11y_no_static_element_interactions -->
                      <button class="btn-secondary" style="padding:3px 8px;font-size:10px" on:click={() => runJob(job.id)}>▶</button>
                    </div>
                  </div>
                {/each}
                {#if jobs.filter(j => j.deviceId === activeDevice.id).length === 0}
                  <div class="empty-hint">Sin trabajos configurados.<br>Pulsa "Nuevo trabajo" para crear uno.</div>
                {/if}

              <!-- Sincronización: pares -->
              {:else if slideView === 'sync'}
                {#each (activeDevice.syncPairs || []) as pair}
                  <div class="sync-pair">
                    <div class="sync-folder">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="width:11px;height:11px;flex-shrink:0"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
                      {pair.local}
                    </div>
                    <div class="sync-arrow">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="17 1 21 5 17 9"/><path d="M3 11V9a4 4 0 0 1 4-4h14"/><polyline points="7 23 3 19 7 15"/><path d="M21 13v2a4 4 0 0 1-4 4H3"/></svg>
                    </div>
                    <div class="sync-folder">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="width:11px;height:11px;flex-shrink:0"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
                      {pair.remote}
                    </div>
                    <span style="margin-left:auto;font-size:10px;color:{pair.status === 'synced' ? 'var(--green)' : 'var(--amber)'}">
                      {pair.status === 'synced' ? '● Sync' : '↑ Cambios'}
                    </span>
                  </div>
                {/each}
                {#if (activeDevice.syncPairs || []).length === 0}
                  <div class="empty-hint">Sin pares configurados.</div>
                {/if}
              {/if}

            </div>
          </div>

        </div>

        <div class="statusbar">
          <div class="dot" class:dot-on={activeDevice.online} class:dot-off={!activeDevice.online}></div>
          <span>{isLocal(activeDevice.addr) ? 'LAN · Puerto 5000' : 'WireGuard · Puerto 5009'}</span>
          <span class="st-sep">·</span>
          <span>{jobs.filter(j => j.deviceId === activeDevice.id).length} trabajos</span>
          <span class="st-sep">·</span>
          <span>{(activeDevice.syncPairs || []).length} pares sync</span>
        </div>
      {/if}


    </div>
  </div>

  {#if showWizard}
    <NimLink
      mode={wizardMode}
      device={activeDevice}
      on:close={() => { showWizard = false; }}
      on:paired={() => { showWizard = false; loadDevices(); }}
      on:created={() => { showWizard = false; loadJobs(); loadDevices(); }}
    />
  {/if}
</div>

<style>
  .backup-root { width:100%; height:100%; display:flex; overflow:hidden; background:var(--bg-frame); font-family:'Inter',-apple-system,sans-serif; color:var(--text-1); }

  /* Sidebar */
  .sidebar { width:210px; flex-shrink:0; display:flex; flex-direction:column; gap:2px; padding:12px 8px; overflow-y:auto; background:var(--bg-sidebar); }
  .sidebar::-webkit-scrollbar { width:3px; }
  .sidebar::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.2); border-radius:2px; }
  .sb-header { display:flex; align-items:center; gap:8px; padding:32px 8px 12px; }
  .sb-logo { width:22px; height:22px; color:var(--accent); flex-shrink:0; }
  .sb-logo svg { width:100%; height:100%; }
  .title { font-size:15px; font-weight:600; color:var(--text-1); }
  .sb-section { font-size:9px; font-weight:700; letter-spacing:.1em; text-transform:uppercase; color:var(--text-3); padding:10px 8px 3px; }
  .sb-item { display:flex; align-items:center; gap:8px; padding:6px 10px; border-radius:8px; cursor:pointer; font-size:12px; color:var(--text-2); border:1px solid transparent; transition:all .15s; }
  .sb-item svg { width:13px; height:13px; flex-shrink:0; opacity:.6; }
  .sb-item:hover { background:rgba(128,128,128,0.10); color:var(--text-1); }
  .sb-item.active { background:var(--active-bg); color:var(--text-1); border-color:var(--border-hi); }
  .sb-item.active svg { opacity:1; }

  .sb-device { display:flex; align-items:center; gap:8px; padding:7px 10px; border-radius:8px; cursor:pointer; font-size:12px; color:var(--text-2); border:1px solid transparent; transition:all .15s; }
  .sb-device:hover { background:rgba(128,128,128,0.08); color:var(--text-1); }
  .sb-device.active { background:var(--active-bg); color:var(--text-1); border-color:var(--border-hi); }
  .sb-dev-icon { width:28px; height:28px; border-radius:7px; flex-shrink:0; display:flex; align-items:center; justify-content:center; background:linear-gradient(135deg,rgba(124,111,255,0.15),rgba(192,84,240,0.15)); border:1px solid rgba(124,111,255,0.2); color:var(--text-2); }
  .sb-dev-icon :global(svg) { width:14px; height:14px; }
  .sb-device.active .sb-dev-icon { color:var(--text-1); }
  .sb-dev-info { flex:1; min-width:0; }
  .sb-dev-name { font-size:11px; font-weight:600; color:var(--text-1); overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .sb-dev-meta { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; }

  .sb-add { display:flex; align-items:center; gap:7px; padding:7px 10px; border-radius:8px; font-size:11px; color:var(--text-3); cursor:pointer; border:1px dashed rgba(255,255,255,0.1); transition:all .15s; margin-top:4px; }
  .sb-add:hover { color:var(--accent); border-color:rgba(124,111,255,.3); }
  .sb-add svg { width:11px; height:11px; }

  .sb-next { margin-top:auto; padding:9px 10px; background:rgba(255,255,255,0.04); border:1px solid var(--border); border-radius:9px; }
  .sn-label { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; margin-bottom:3px; }
  .sn-name { font-size:10px; color:var(--text-2); }
  .sn-time { font-size:13px; font-weight:600; color:var(--accent); margin-top:2px; }

  /* Dots */
  .dot { width:7px; height:7px; border-radius:50%; flex-shrink:0; }
  .dot-on   { background:var(--green); box-shadow:0 0 5px rgba(74,222,128,.4); }
  .dot-off  { background:rgba(255,255,255,.15); }
  .dot-warn { background:var(--amber); }
  .dot-err  { background:var(--red); }

  /* Inner */
  .inner-wrap { flex:1; padding:8px; display:flex; }
  .inner { flex:1; border-radius:10px; border:1px solid var(--border); background:var(--bg-inner); display:flex; flex-direction:column; overflow:hidden; }

  /* Titlebar */
  .inner-titlebar { display:flex; align-items:center; gap:8px; padding:10px 14px 9px; background:var(--bg-bar); flex-shrink:0; border-bottom:1px solid var(--border); }
  .tb-title { font-size:12px; font-weight:600; color:var(--text-1); }
  .tb-sub { font-size:11px; color:var(--text-3); }
  .tb-right { margin-left:auto; display:flex; align-items:center; gap:6px; }
  .dev-header-icon { width:22px; height:22px; color:var(--text-2); flex-shrink:0; }
  .dev-header-icon :global(svg) { width:100%; height:100%; }
  .dev-online { display:flex; align-items:center; gap:5px; font-size:10px; color:var(--text-3); margin-left:8px; }
  .dev-online.online { color:var(--green); }
  .icon-btn { width:27px; height:27px; background:var(--ibtn-bg); border:1px solid var(--border); border-radius:6px; display:flex; align-items:center; justify-content:center; cursor:pointer; color:var(--text-2); transition:all .15s; }
  .icon-btn svg { width:13px; height:13px; }
  .icon-btn:hover { background:rgba(124,111,255,0.15); color:var(--text-1); }

  /* Tabs */
  .tab-nav { display:flex; padding:0 2px; border-bottom:1px solid var(--border); flex-shrink:0; }
  .tab { position:relative; cursor:pointer; padding:8px 14px 10px; }
  .tab span { font-size:12px; font-weight:600; color:var(--text-3); transition:color .2s; }
  .tab:hover span { color:var(--text-2); }
  .tab.active span { color:var(--text-1); }
  .tab-line { position:absolute; bottom:0; left:-2px; right:-2px; height:2.5px; border-radius:2px; background:linear-gradient(90deg,var(--accent),var(--accent2)); }

  /* Btns */
  .btn-secondary { display:inline-flex; align-items:center; gap:5px; padding:5px 10px; background:var(--ibtn-bg); border:1px solid var(--border); border-radius:6px; color:var(--text-2); font-family:inherit; font-size:11px; font-weight:500; cursor:pointer; transition:all .15s; }
  .btn-secondary svg { width:11px; height:11px; }
  .btn-secondary:hover { color:var(--text-1); border-color:var(--border-hi); }

  /* Content */
  .content { flex:1; overflow-y:auto; padding:16px; display:flex; flex-direction:column; gap:14px; }
  .content::-webkit-scrollbar { width:3px; }
  .content::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }
  .section-label { font-size:9px; font-weight:700; color:var(--text-3); text-transform:uppercase; letter-spacing:.08em; margin-bottom:8px; }

  /* Stats */
  .stats-row { display:grid; grid-template-columns:repeat(4,1fr); gap:8px; }
  .stat-card { background:rgba(255,255,255,0.025); border:1px solid var(--border); border-radius:9px; padding:11px 13px; }
  .stat-label { font-size:9px; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; margin-bottom:4px; }
  .stat-val { font-size:15px; font-weight:600; color:var(--text-1); }
  .stat-sub { font-size:9px; color:var(--text-3); margin-top:2px; font-family:'DM Mono',monospace; }

  /* Rows */
  .row { display:flex; align-items:center; gap:10px; padding:9px 4px; border-bottom:1px solid var(--border); transition:background .12s; }
  .row:first-of-type { border-top:1px solid var(--border); }
  .row:hover { background:rgba(255,255,255,0.02); }
  .row-icon { width:28px; height:28px; border-radius:7px; flex-shrink:0; display:flex; align-items:center; justify-content:center; }
  .row-info { flex:1; min-width:0; }
  .row-name { font-size:12px; font-weight:600; color:var(--text-1); }
  .row-meta { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; margin-top:1px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .row-status { display:flex; align-items:center; gap:5px; font-size:10px; flex-shrink:0; }
  .row-actions { display:flex; gap:4px; flex-shrink:0; }

  /* Propósitos */
  .purpose-grid { display:grid; grid-template-columns:1fr 1fr; gap:8px; }
  .purpose-card { display:flex; align-items:flex-start; gap:10px; padding:12px 14px; border-radius:9px; border:1px solid var(--border); background:rgba(255,255,255,0.02); cursor:pointer; transition:all .15s; }
  .purpose-card:hover { border-color:rgba(255,255,255,0.12); }
  .purpose-card.on { border-color:var(--border-hi); background:var(--active-bg); }
  .pur-ico { width:32px; height:32px; border-radius:8px; flex-shrink:0; display:flex; align-items:center; justify-content:center; }
  .pur-ico svg { width:15px; height:15px; }
  .pur-ico.blue   { background:rgba(96,165,250,0.12); color:var(--blue); }
  .pur-ico.green  { background:rgba(74,222,128,0.12); color:var(--green); }
  .pur-ico.purple { background:rgba(192,132,252,0.12); color:#c084fc; }
  .pur-ico.amber  { background:rgba(251,191,36,0.12); color:var(--amber); }
  .pur-info { flex:1; }
  .pur-name { font-size:12px; font-weight:600; color:var(--text-1); margin-bottom:2px; }
  .pur-desc { font-size:10px; color:var(--text-3); line-height:1.4; }
  .toggle { width:30px; height:17px; border-radius:9px; background:rgba(255,255,255,0.1); border:none; cursor:pointer; position:relative; transition:background .2s; flex-shrink:0; margin-top:2px; }
  .toggle.on { background:var(--accent); }
  .toggle::after { content:''; position:absolute; top:2px; left:2px; width:13px; height:13px; border-radius:50%; background:#fff; transition:transform .2s; }
  .toggle.on::after { transform:translateX(13px); }

  /* Sync */
  .sync-pair { display:flex; align-items:center; gap:8px; padding:9px 10px; border-radius:8px; background:rgba(255,255,255,0.02); border:1px solid var(--border); font-size:11px; }
  .sync-folder { flex:1; background:rgba(255,255,255,0.04); border-radius:5px; padding:5px 8px; font-family:'DM Mono',monospace; font-size:10px; color:var(--text-2); display:flex; align-items:center; gap:5px; }
  .sync-arrow { color:var(--accent); flex-shrink:0; }
  .sync-arrow svg { width:14px; height:14px; }

  /* Actividad */
  .act-row { display:flex; align-items:center; gap:10px; padding:7px 4px; border-bottom:1px solid var(--border); font-size:11px; }
  .act-ico { width:20px; height:20px; border-radius:5px; display:flex; align-items:center; justify-content:center; flex-shrink:0; }
  .act-ico.ok  { background:rgba(74,222,128,0.10); }
  .act-ico.ok :global(svg) { color:var(--green); width:10px; height:10px; }
  .act-ico.err { background:rgba(248,113,113,0.10); }
  .act-ico.err :global(svg) { color:var(--red); width:10px; height:10px; }
  .act-desc { flex:1; }
  .act-dest { font-family:'DM Mono',monospace; font-size:10px; color:var(--text-3); }
  .act-size { font-family:'DM Mono',monospace; font-size:10px; color:var(--text-3); width:58px; text-align:right; }
  .act-time { font-family:'DM Mono',monospace; font-size:10px; color:var(--text-3); width:70px; text-align:right; }

  .add-row { display:flex; align-items:center; justify-content:center; gap:6px; padding:9px; border-radius:7px; border:1px dashed rgba(255,255,255,0.08); color:var(--text-3); font-size:11px; cursor:pointer; transition:all .15s; margin-top:4px; }
  .add-row:hover { color:var(--accent); border-color:rgba(124,111,255,.3); }
  .add-row svg { width:11px; height:11px; }

  .empty-hint { text-align:center; padding:28px; border:1px dashed rgba(255,255,255,0.08); border-radius:9px; color:var(--text-3); font-size:11px; line-height:1.6; }

  /* Statusbar */
  .statusbar { display:flex; align-items:center; gap:8px; padding:9px 14px; border-top:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; font-size:10px; color:var(--text-3); border-radius:0 0 10px 10px; font-family:'DM Mono',monospace; }
  .status-dot { width:6px; height:6px; border-radius:50%; background:var(--green); box-shadow:0 0 4px rgba(74,222,128,.5); }
  .st-sep { color:rgba(255,255,255,0.1); }
  .st-right { margin-left:auto; color:var(--accent); }

  /* ── Slide device view ── */
  .dev-slider { display:flex; width:200%; flex:1; overflow:hidden; transition:transform .3s ease-in-out; }
  .dev-slider.slide { transform:translateX(-50%); }
  .dev-pane { width:50%; display:flex; flex-direction:column; overflow:hidden; }
  .dev-pane .content { flex:1; overflow-y:auto; padding:16px; display:flex; flex-direction:column; gap:14px; }

  .cfg-header { display:flex; align-items:center; gap:10px; padding:10px 14px; border-bottom:1px solid var(--border); flex-shrink:0; background:var(--bg-bar); }
  .cfg-back { width:27px; height:27px; border-radius:6px; background:var(--ibtn-bg); border:1px solid var(--border); display:flex; align-items:center; justify-content:center; cursor:pointer; flex-shrink:0; transition:all .15s; }
  .cfg-back:hover { background:rgba(124,111,255,0.15); }
  .cfg-back svg { width:14px; height:14px; }
  .cfg-title { font-size:12px; font-weight:600; color:var(--text-1); }
  .cfg-subtitle { font-size:10px; color:var(--text-3); margin-top:1px; }
  .cfg-add-btn { margin-left:auto; display:inline-flex; align-items:center; gap:5px; padding:4px 10px; background:rgba(233,84,32,0.1); border:1px solid rgba(233,84,32,0.25); border-radius:6px; color:var(--accent); font-size:11px; font-weight:500; cursor:pointer; transition:all .15s; flex-shrink:0; }
  .cfg-add-btn:hover { background:rgba(233,84,32,0.18); }
  .cfg-add-btn svg { width:11px; height:11px; }

  .svc-list { display:flex; flex-direction:column; gap:4px; }
  .svc-row { display:flex; align-items:center; gap:10px; padding:10px 12px; border-radius:9px; background:rgba(255,255,255,0.025); border:1px solid var(--border); }
  .svc-ico { width:30px; height:30px; border-radius:8px; display:flex; align-items:center; justify-content:center; flex-shrink:0; }
  .svc-ico :global(svg) { width:14px; height:14px; }
  .svc-ico.green  { background:rgba(74,222,128,0.12);  color:var(--green); }
  .svc-ico.blue   { background:rgba(96,165,250,0.12);  color:var(--blue); }
  .svc-ico.amber  { background:rgba(251,191,36,0.12);  color:var(--amber); }
  .svc-ico.purple { background:rgba(192,132,252,0.12); color:#c084fc; }
  .svc-info { flex:1; min-width:0; }
  .svc-name { font-size:12px; font-weight:600; color:var(--text-1); }
  .svc-desc { font-size:10px; color:var(--text-3); margin-top:1px; }
  .svc-actions { display:flex; align-items:center; gap:6px; flex-shrink:0; }
  .cfg-btn { width:27px; height:27px; border-radius:6px; background:var(--ibtn-bg); border:1px solid var(--border); display:flex; align-items:center; justify-content:center; cursor:pointer; color:var(--text-2); transition:all .15s; }
  .cfg-btn:hover { background:rgba(124,111,255,0.15); color:var(--text-1); border-color:var(--border-hi); }
  .cfg-btn svg { width:13px; height:13px; }

  .share-donut-card { display:flex; align-items:center; gap:16px; padding:14px; background:rgba(255,255,255,0.025); border:1px solid var(--border); border-radius:10px; }
  .donut-wrap { position:relative; width:72px; height:72px; flex-shrink:0; }
  .donut-svg { width:72px; height:72px; display:block; }
  .donut-center { position:absolute; inset:0; display:flex; flex-direction:column; align-items:center; justify-content:center; }
  .donut-pct { font-size:14px; font-weight:700; color:var(--text-1); line-height:1; }
  .donut-lbl { font-size:9px; color:var(--text-3); }
  .donut-info { flex:1; min-width:0; display:flex; flex-direction:column; gap:3px; }
  .donut-name { font-size:13px; font-weight:600; color:var(--text-1); }
  .donut-path { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .donut-sizes { font-size:10px; color:var(--text-2); }
  .donut-badge { display:inline-flex; align-items:center; gap:4px; font-size:10px; color:var(--green); background:rgba(74,222,128,0.1); border:1px solid rgba(74,222,128,0.2); border-radius:5px; padding:2px 7px; margin-top:2px; }
</style>
