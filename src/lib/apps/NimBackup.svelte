<script>
  import { onMount, onDestroy } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';
  import NimLink from '$lib/apps/NimLink.svelte';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}`, 'Content-Type': 'application/json' });

  // ── Estado ──
  let view = 'resumen';
  let devices = [];
  let jobs = [];
  let history = [];
  let activeDevice = null;
  let loading = false;

  // ── Wizard modals ──
  let showWizard = false;
  let wizardMode = 'pair';

  // ── Slide config panel ──
  let configPane = null;

  // ── Remote Shares ──
  let remoteShares = [];
  let sharesLoading = false;

  function isLocal(addr) {
    return addr.startsWith('192.168.') || addr.startsWith('10.') || addr.startsWith('172.') || addr === 'localhost';
  }

  // ── API calls ──
  async function loadDevices() {
    try { const r = await fetch('/api/backup/devices', { headers: hdrs() }); const d = await r.json(); devices = d.devices || []; } catch { devices = []; }
  }
  async function loadJobs() {
    try { const r = await fetch('/api/backup/jobs', { headers: hdrs() }); const d = await r.json(); jobs = d.jobs || []; } catch { jobs = []; }
  }
  async function loadHistory() {
    try { const r = await fetch('/api/backup/history', { headers: hdrs() }); const d = await r.json(); history = d.history || []; } catch { history = []; }
  }
  async function runJob(jobId) {
    try { await fetch(`/api/backup/run/${jobId}`, { method: 'POST', headers: hdrs() }); await loadJobs(); } catch {}
  }
  async function removeDevice(id) {
    if (!confirm('¿Desemparejar este dispositivo?')) return;
    try { await fetch(`/api/backup/devices/${id}`, { method: 'DELETE', headers: hdrs() }); devices = devices.filter(d => d.id !== id); activeDevice = null; view = 'resumen'; } catch {}
  }
  async function savePurposes(deviceId, purposes) {
    try { await fetch(`/api/backup/devices/${deviceId}/purposes`, { method: 'POST', headers: hdrs(), body: JSON.stringify({ purposes }) }); } catch {}
  }
  async function loadRemoteShares(deviceId) {
    sharesLoading = true;
    try { const r = await fetch(`/api/backup/devices/${deviceId}/remote-shares`, { headers: hdrs() }); const d = await r.json(); remoteShares = d.shares || []; } catch { remoteShares = []; }
    sharesLoading = false;
  }
  async function mountShare(deviceId, share) {
    share._mounting = true; remoteShares = [...remoteShares];
    try { const r = await fetch(`/api/backup/devices/${deviceId}/mount`, { method: 'POST', headers: hdrs(), body: JSON.stringify({ shareName: share.name, remotePath: share.path }) }); const d = await r.json(); if (d.ok) { share.mounted = true; share.mountPoint = d.mountPoint; } } catch {}
    share._mounting = false; remoteShares = [...remoteShares];
  }
  async function unmountShare(deviceId, share) {
    share._mounting = true; remoteShares = [...remoteShares];
    try { const r = await fetch(`/api/backup/devices/${deviceId}/unmount`, { method: 'POST', headers: hdrs(), body: JSON.stringify({ shareName: share.name }) }); const d = await r.json(); if (d.ok) { share.mounted = false; share.mountPoint = ''; } } catch {}
    share._mounting = false; remoteShares = [...remoteShares];
  }

  function togglePurpose(key) {
    if (!activeDevice) return;
    const purposes = activeDevice.purposes || [];
    activeDevice.purposes = purposes.includes(key) ? purposes.filter(x => x !== key) : [...purposes, key];
    activeDevice = {...activeDevice};
    savePurposes(activeDevice.id, activeDevice.purposes);
  }

  function openConfig(type) {
    configPane = type;
    if (type === 'share' && activeDevice) loadRemoteShares(activeDevice.id);
  }

  function fmtTime(iso) {
    if (!iso) return '—';
    const d = new Date(iso); const now = new Date(); const diff = Math.floor((now - d) / 1000);
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
  $: deviceJobs = activeDevice ? jobs.filter(j => j.deviceId === activeDevice.id) : [];
  $: mountedShares = remoteShares.filter(s => s.mounted);

  const SERVICES = [
    { key: 'share',       name: 'Share remota',    desc: 'Carpetas de este NAS visibles en Files', color: '#4ade80', bg: 'rgba(74,222,128,0.12)', icon: 'folder' },
    { key: 'backup_dest', name: 'Backup destino',  desc: 'Este NAS recibe tus backups',            color: '#3b82f6', bg: 'rgba(59,130,246,0.12)', icon: 'down' },
    { key: 'backup_src',  name: 'Backup origen',   desc: 'Este NAS hace backup al tuyo',           color: '#e95420', bg: 'rgba(233,84,32,0.12)',  icon: 'up' },
    { key: 'sync',        name: 'Sincronización',  desc: 'Carpetas espejo entre los dos NAS',      color: '#a855f7', bg: 'rgba(168,85,247,0.12)', icon: 'sync' },
  ];

  onMount(() => { loadDevices(); loadJobs(); loadHistory(); });
</script>

<div class="backup-root">
  <!-- ══ SIDEBAR ══ -->
  <div class="sidebar">
    <div class="sb-title">
      <svg viewBox="0 0 24 24" fill="none" stroke="#e95420" stroke-width="2" stroke-linecap="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
      NimBackup
    </div>
    <div class="sb-section">General</div>
    <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={view === 'resumen' && !activeDevice} on:click={() => { view = 'resumen'; activeDevice = null; configPane = null; }}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/></svg>
      Resumen
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={view === 'historial'} on:click={() => { view = 'historial'; activeDevice = null; configPane = null; loadHistory(); }}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
      Historial
    </div>
    <div class="sb-section" style="margin-top:6px">Dispositivos</div>
    {#each devices as dev}
      <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="sb-item" class:active={activeDevice?.id === dev.id} on:click={() => { activeDevice = dev; view = 'device'; configPane = null; loadRemoteShares(dev.id); }}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>
        {dev.name}
        {#if dev.online}<div class="sb-dot"></div>{/if}
      </div>
    {/each}
    {#if devices.length === 0}<div style="font-size:11px;color:var(--text-3);padding:8px">Sin dispositivos</div>{/if}
    <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-add" on:click={() => { wizardMode = 'pair'; showWizard = true; }}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
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

  <!-- ══ MAIN ══ -->
  <div class="main">
    <!-- RESUMEN -->
    {#if view === 'resumen' && !activeDevice}
      <div class="dev-header">
        <div class="dev-ico"><svg viewBox="0 0 24 24" fill="none" stroke="#a89fff" stroke-width="1.8"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/></svg></div>
        <div><div class="dev-name">Resumen</div><div class="dev-addr">{onlineCount} de {devices.length} dispositivos online</div></div>
      </div>
      <div class="content">
        <div class="row">
          <div class="stat-card"><div class="stat-lbl">Dispositivos</div><div class="stat-val" style="color:var(--green)">{onlineCount}/{devices.length}</div><div class="stat-sub">online</div></div>
          <div class="stat-card"><div class="stat-lbl">Trabajos OK</div><div class="stat-val" style="color:var(--accent)">{jobsOk}/{jobs.length}</div><div class="stat-sub">activos</div></div>
          <div class="stat-card"><div class="stat-lbl">Último backup</div><div class="stat-val" style="font-size:13px">{history.length > 0 ? fmtTime(history[0]?.time) : '—'}</div><div class="stat-sub">{history[0]?.jobName || '—'}</div></div>
        </div>
        {#if jobs.length > 0}
          <div><div class="section-lbl">Trabajos activos</div>
            {#each jobs as job}
              <div class="svc-row">
                <div class="svc-ico" style="background:{job.fsType === 'btrfs' ? 'rgba(74,222,128,0.12)' : 'rgba(59,130,246,0.12)'}">
                  <svg viewBox="0 0 24 24" fill="none" stroke="{job.fsType === 'btrfs' ? '#4ade80' : '#3b82f6'}" stroke-width="1.8" stroke-linecap="round" style="width:14px;height:14px"><rect x="2" y="3" width="20" height="8" rx="2"/><circle cx="18" cy="7" r="1" fill="currentColor" stroke="none"/></svg>
                </div>
                <div class="svc-info"><div class="svc-name">{job.name}</div><div class="svc-desc">{job.fsType} · {job.schedule} · {fmtTime(job.lastRun)}</div></div>
                <div class="dot" class:dot-on={job.status === 'ok'} class:dot-err={job.status === 'error'}></div>
                <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
                <button class="cfg-btn" on:click={() => runJob(job.id)}>▶</button>
              </div>
            {/each}
          </div>
        {:else}<div class="empty-hint">Sin trabajos configurados. Empareja un dispositivo para empezar.</div>{/if}
      </div>

    <!-- HISTORIAL -->
    {:else if view === 'historial'}
      <div class="dev-header">
        <div class="dev-ico"><svg viewBox="0 0 24 24" fill="none" stroke="#a89fff" stroke-width="1.8"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg></div>
        <div><div class="dev-name">Historial</div><div class="dev-addr">{history.length} ejecuciones</div></div>
      </div>
      <div class="content">
        {#each history as h}
          <div class="svc-row" style="padding:8px 12px">
            <div class="svc-ico" style="background:{h.ok ? 'rgba(74,222,128,0.1)' : 'rgba(248,113,113,0.1)'}">
              {#if h.ok}<svg viewBox="0 0 24 24" fill="none" stroke="#4ade80" stroke-width="2.5" stroke-linecap="round" style="width:12px;height:12px"><polyline points="20 6 9 17 4 12"/></svg>
              {:else}<svg viewBox="0 0 24 24" fill="none" stroke="#f87171" stroke-width="2.5" stroke-linecap="round" style="width:12px;height:12px"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>{/if}
            </div>
            <div class="svc-info"><div class="svc-name" style="color:{h.ok ? 'var(--text-1)' : 'var(--red)'}">{h.jobName}</div><div class="svc-desc">{h.dest} · {fmtSize(h.bytes)}</div></div>
            <span style="font-size:10px;color:var(--text-3);font-family:'DM Mono',monospace">{fmtTime(h.time)}</span>
          </div>
        {/each}
        {#if history.length === 0}<div class="empty-hint">Sin historial todavía.</div>{/if}
      </div>

    <!-- DEVICE -->
    {:else if view === 'device' && activeDevice}
      <div class="dev-header">
        <div class="dev-ico"><svg viewBox="0 0 24 24" fill="none" stroke="#a89fff" stroke-width="1.8"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg></div>
        <div><div class="dev-name">{activeDevice.name}</div><div class="dev-addr">{activeDevice.addr} · {isLocal(activeDevice.addr) ? 'LAN' : 'WAN'} · {activeDevice.ping || '—'}</div></div>
        {#if activeDevice.online}
          <div class="dev-badge"><div class="dev-badge-dot"></div> Online</div>
        {:else}
          <div class="dev-badge offline"><div class="dev-badge-dot"></div> Offline</div>
        {/if}
        <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
        <button class="cfg-btn" style="margin-left:8px;color:var(--red)" on:click={() => removeDevice(activeDevice.id)}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="width:13px;height:13px"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
        </button>
      </div>

      <div class="slider" class:show-config={configPane !== null}>
        <!-- PANE 1: overview -->
        <div class="pane">
          <div class="content">
            <div class="row">
              <div class="stat-card"><div class="stat-lbl">Latencia</div><div class="stat-val" style="color:var(--green)">{activeDevice.ping || '—'}</div><div class="stat-sub">{isLocal(activeDevice.addr) ? 'LAN directa' : 'WireGuard'}</div></div>
              <div class="stat-card"><div class="stat-lbl">Espacio libre</div><div class="stat-val" style="color:#3b82f6">{activeDevice.freeSpace || '—'}</div><div class="stat-sub">disponible</div></div>
              <div class="stat-card"><div class="stat-lbl">Versión</div><div class="stat-val" style="font-size:12px;margin-top:3px">{activeDevice.version || '—'}</div><div class="stat-sub">NimOS</div></div>
            </div>
            {#if mountedShares.length > 0}
              <div><div class="section-lbl">Carpetas compartidas</div>
                {#each mountedShares as share}
                  <div class="donut-card">
                    <div class="donut-wrap">
                      <svg viewBox="0 0 72 72"><circle cx="36" cy="36" r="28" fill="none" stroke="rgba(255,255,255,0.07)" stroke-width="9"/><circle cx="36" cy="36" r="28" fill="none" stroke="#4ade80" stroke-width="9" stroke-dasharray="28 148" stroke-linecap="round" transform="rotate(-90 36 36)"/></svg>
                      <div class="donut-center"><div class="donut-pct">—</div></div>
                    </div>
                    <div class="donut-info">
                      <div class="donut-share">{share.displayName || share.name}</div>
                      <div class="donut-path">{share.path}</div>
                      {#if share.mountPoint}<div class="mount-badge"><svg viewBox="0 0 24 24" style="width:10px;height:10px;stroke:#4ade80;fill:none;stroke-width:2"><polyline points="20 6 9 17 4 12"/></svg> Montada en Files</div>{/if}
                    </div>
                  </div>
                {/each}
              </div>
            {/if}
            <div><div class="section-lbl">Servicios</div>
              <div class="services-list">
                {#each SERVICES as svc}
                  <div class="svc-row">
                    <div class="svc-ico" style="background:{svc.bg}">
                      {#if svc.icon === 'folder'}<svg viewBox="0 0 24 24" fill="none" stroke={svc.color} stroke-width="1.8" stroke-linecap="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
                      {:else if svc.icon === 'down'}<svg viewBox="0 0 24 24" fill="none" stroke={svc.color} stroke-width="1.8" stroke-linecap="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                      {:else if svc.icon === 'up'}<svg viewBox="0 0 24 24" fill="none" stroke={svc.color} stroke-width="1.8" stroke-linecap="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                      {:else}<svg viewBox="0 0 24 24" fill="none" stroke={svc.color} stroke-width="1.8" stroke-linecap="round"><polyline points="17 1 21 5 17 9"/><path d="M3 11V9a4 4 0 0 1 4-4h14"/><polyline points="7 23 3 19 7 15"/><path d="M21 13v2a4 4 0 0 1-4 4H3"/></svg>
                      {/if}
                    </div>
                    <div class="svc-info"><div class="svc-name">{svc.name}</div><div class="svc-desc">{svc.desc}</div></div>
                    <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
                    <div class="cfg-btn" on:click={() => openConfig(svc.key)}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M4.93 4.93a10 10 0 0 0 0 14.14"/></svg></div>
                    <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
                    <div class="toggle" class:on={activeDevice.purposes?.includes(svc.key)} on:click={() => togglePurpose(svc.key)}><div class="toggle-dot"></div></div>
                  </div>
                {/each}
              </div>
            </div>
          </div>
        </div>

        <!-- PANE 2: config -->
        <div class="pane">
          <div class="cfg-header">
            <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="cfg-back" on:click={() => configPane = null}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="15 18 9 12 15 6"/></svg></div>
            <div><div class="cfg-title">{SERVICES.find(s => s.key === configPane)?.name || ''}</div><div class="cfg-subtitle">{activeDevice.name}</div></div>
            <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="cfg-add" on:click={() => { if (configPane === 'share') loadRemoteShares(activeDevice.id); else if (configPane === 'sync') { wizardMode = 'sync'; showWizard = true; } else { wizardMode = 'job'; showWizard = true; } }}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
              {#if configPane === 'share'}Refrescar{:else if configPane === 'sync'}Añadir par{:else}Nuevo trabajo{/if}
            </div>
          </div>
          <div class="cfg-content">
            {#if configPane === 'share'}
              {#if sharesLoading}<div class="empty-hint">Cargando shares...</div>
              {:else if remoteShares.length === 0}<div class="empty-hint">No se encontraron carpetas compartidas.</div>
              {:else}
                {#each remoteShares as share}
                  <div class="share-row">
                    <div class="share-ico"><svg viewBox="0 0 24 24" fill="none" stroke="#a89fff" stroke-width="1.8" stroke-linecap="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg></div>
                    <div style="flex:1;min-width:0"><div class="share-name">{share.displayName || share.name}</div><div class="share-path">{share.path}</div></div>
                    {#if share.mounted}
                      <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
                      <div class="pill-on" on:click={() => unmountShare(activeDevice.id, share)}>{share._mounting ? '...' : 'Montada'}</div>
                    {:else}
                      <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
                      <div class="pill-off" on:click={() => mountShare(activeDevice.id, share)}>{share._mounting ? '...' : 'Montar'}</div>
                    {/if}
                  </div>
                {/each}
              {/if}
            {:else if configPane === 'backup_dest' || configPane === 'backup_src'}
              {#each deviceJobs as job}
                <div class="share-row">
                  <div class="share-ico" style="background:{job.fsType === 'btrfs' ? 'rgba(74,222,128,0.12)' : 'rgba(59,130,246,0.12)'}"><svg viewBox="0 0 24 24" fill="none" stroke="{job.fsType === 'btrfs' ? '#4ade80' : '#3b82f6'}" stroke-width="1.8" stroke-linecap="round"><rect x="2" y="3" width="20" height="8" rx="2"/><circle cx="18" cy="7" r="1" fill="currentColor" stroke="none"/></svg></div>
                  <div style="flex:1;min-width:0"><div class="share-name">{job.name}</div><div class="share-path">{job.source} → {job.dest} · {job.schedule}</div></div>
                  <div class="dot" class:dot-on={job.status === 'ok'} class:dot-err={job.status === 'error'}></div>
                  <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
                  <button class="cfg-btn" on:click={() => runJob(job.id)}>▶</button>
                </div>
              {/each}
              {#if deviceJobs.length === 0}<div class="empty-hint">Sin trabajos configurados.</div>{/if}
            {:else if configPane === 'sync'}
              {#each (activeDevice.syncPairs || []) as pair}
                <div class="share-row">
                  <div class="share-ico" style="background:rgba(168,85,247,0.12)"><svg viewBox="0 0 24 24" fill="none" stroke="#a855f7" stroke-width="1.8" stroke-linecap="round"><polyline points="17 1 21 5 17 9"/><path d="M3 11V9a4 4 0 0 1 4-4h14"/><polyline points="7 23 3 19 7 15"/><path d="M21 13v2a4 4 0 0 1-4 4H3"/></svg></div>
                  <div style="flex:1;min-width:0"><div class="share-name">{pair.local}</div><div class="share-path">↔ {pair.remote}</div></div>
                  <div class="pill-on" style="color:{pair.status === 'synced' ? '#4ade80' : '#fbbf24'}">{pair.status === 'synced' ? 'Sync' : 'Pendiente'}</div>
                </div>
              {/each}
              {#if (activeDevice.syncPairs || []).length === 0}<div class="empty-hint">Sin pares de sincronización.</div>{/if}
            {/if}
          </div>
        </div>
      </div>

      <div class="statusbar">
        <div class="sb-online" class:offline={!activeDevice.online}></div>
        <span>{isLocal(activeDevice.addr) ? 'LAN · Puerto 5000' : 'WAN · Puerto 5009'}</span>
        <span>·</span><span>{deviceJobs.length} trabajos</span>
        <span>·</span><span>{mountedShares.length} shares montadas</span>
      </div>
    {/if}
  </div>

  {#if showWizard}
    <NimLink mode={wizardMode} device={activeDevice}
      on:close={() => { showWizard = false; }}
      on:paired={() => { showWizard = false; loadDevices(); }}
      on:created={() => { showWizard = false; loadJobs(); loadDevices(); }} />
  {/if}
</div>

<style>
  .backup-root { width:100%; height:100%; display:flex; overflow:hidden; font-family:'DM Sans',system-ui,sans-serif; color:var(--text-1); }
  .sidebar { width:200px; flex-shrink:0; border-right:1px solid var(--border); display:flex; flex-direction:column; padding:16px 10px; gap:4px; overflow-y:auto; }
  .sidebar::-webkit-scrollbar { width:3px; } .sidebar::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.2); border-radius:2px; }
  .sb-title { display:flex; align-items:center; gap:8px; padding:24px 8px 16px; font-size:14px; font-weight:700; color:var(--text-1); }
  .sb-title svg { width:18px; height:18px; flex-shrink:0; }
  .sb-section { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.08em; padding:8px 8px 4px; }
  .sb-item { display:flex; align-items:center; gap:8px; padding:7px 8px; border-radius:8px; font-size:12px; color:var(--text-2); cursor:pointer; border:1px solid transparent; transition:all .15s; }
  .sb-item svg { width:14px; height:14px; flex-shrink:0; } .sb-item:hover { background:var(--ibtn-bg); color:rgba(255,255,255,0.8); }
  .sb-item.active { background:var(--active-bg); color:var(--text-1); border-color:var(--border-hi); }
  .sb-dot { width:7px; height:7px; border-radius:50%; background:var(--green); margin-left:auto; flex-shrink:0; }
  .sb-add { display:flex; align-items:center; gap:7px; padding:7px 8px; border-radius:8px; font-size:11px; color:var(--text-3); cursor:pointer; margin-top:4px; border:1px dashed var(--border); transition:all .15s; }
  .sb-add:hover { color:rgba(255,255,255,0.6); border-color:rgba(255,255,255,0.2); } .sb-add svg { width:13px; height:13px; }
  .sb-next { margin-top:auto; padding:9px 10px; background:var(--ibtn-bg); border:1px solid var(--border); border-radius:9px; }
  .sn-label { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; margin-bottom:3px; }
  .sn-name { font-size:10px; color:var(--text-2); } .sn-time { font-size:13px; font-weight:600; color:var(--accent); margin-top:2px; }
  .main { flex:1; display:flex; flex-direction:column; overflow:hidden; }
  .dev-header { display:flex; align-items:center; gap:12px; padding:16px 20px 14px; border-bottom:1px solid var(--border); flex-shrink:0; }
  .dev-ico { width:36px; height:36px; border-radius:9px; background:var(--active-bg); border:1px solid rgba(124,111,255,0.2); display:flex; align-items:center; justify-content:center; flex-shrink:0; }
  .dev-ico svg { width:18px; height:18px; } .dev-name { font-size:15px; font-weight:700; color:var(--text-1); }
  .dev-addr { font-size:11px; color:var(--text-3); font-family:'DM Mono',monospace; margin-top:1px; }
  .dev-badge { margin-left:auto; display:flex; align-items:center; gap:5px; font-size:11px; color:var(--green); background:rgba(74,222,128,0.1); border:1px solid rgba(74,222,128,0.2); padding:3px 9px; border-radius:20px; flex-shrink:0; }
  .dev-badge.offline { color:var(--text-3); background:var(--ibtn-bg); border-color:rgba(255,255,255,0.1); }
  .dev-badge-dot { width:6px; height:6px; border-radius:50%; background:currentColor; }
  .content { flex:1; overflow-y:auto; padding:20px; display:flex; flex-direction:column; gap:18px; }
  .content::-webkit-scrollbar { width:3px; } .content::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }
  .row { display:flex; gap:12px; } .section-lbl { font-size:10px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.07em; margin-bottom:8px; }
  .stat-card { flex:1; background:var(--ibtn-bg); border:1px solid var(--border); border-radius:10px; padding:12px 14px; }
  .stat-lbl { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.07em; margin-bottom:5px; }
  .stat-val { font-size:18px; font-weight:700; color:var(--text-1); } .stat-sub { font-size:10px; color:var(--text-3); margin-top:3px; }
  .donut-card { background:var(--ibtn-bg); border:1px solid var(--border); border-radius:10px; padding:14px 16px; display:flex; align-items:center; gap:16px; margin-bottom:6px; }
  .donut-wrap { position:relative; width:72px; height:72px; flex-shrink:0; } .donut-wrap svg { width:72px; height:72px; }
  .donut-center { position:absolute; inset:0; display:flex; align-items:center; justify-content:center; }
  .donut-pct { font-size:14px; font-weight:700; color:var(--green); }
  .donut-info { flex:1; display:flex; flex-direction:column; gap:3px; } .donut-share { font-size:13px; font-weight:600; color:var(--text-1); }
  .donut-path { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; }
  .mount-badge { display:inline-flex; align-items:center; gap:4px; font-size:10px; background:rgba(74,222,128,0.1); color:var(--green); border:1px solid rgba(74,222,128,0.2); border-radius:5px; padding:2px 7px; margin-top:4px; }
  .services-list { display:flex; flex-direction:column; gap:4px; }
  .svc-row { display:flex; align-items:center; gap:10px; padding:11px 14px; border-radius:9px; background:var(--ibtn-bg); border:1px solid var(--border); }
  .svc-ico { width:30px; height:30px; border-radius:8px; display:flex; align-items:center; justify-content:center; flex-shrink:0; }
  .svc-ico svg { width:14px; height:14px; } .svc-info { flex:1; min-width:0; } .svc-name { font-size:12px; font-weight:600; color:var(--text-1); }
  .svc-desc { font-size:10px; color:var(--text-3); margin-top:1px; }
  .toggle { width:36px; height:20px; border-radius:10px; background:var(--ibtn-bg); border:1px solid var(--border); position:relative; cursor:pointer; transition:background .2s; flex-shrink:0; }
  .toggle.on { background:var(--accent); border-color:var(--accent); }
  .toggle-dot { position:absolute; top:2px; left:2px; width:14px; height:14px; border-radius:50%; background:rgba(255,255,255,0.4); transition:transform .2s, background .2s; }
  .toggle.on .toggle-dot { transform:translateX(16px); background:#fff; }
  .cfg-btn { width:28px; height:28px; border-radius:7px; background:var(--ibtn-bg); border:1px solid var(--border); display:flex; align-items:center; justify-content:center; cursor:pointer; transition:all .15s; flex-shrink:0; color:var(--text-2); font-size:11px; font-family:inherit; }
  .cfg-btn:hover { background:rgba(255,255,255,0.12); color:var(--text-1); } .cfg-btn svg { width:13px; height:13px; }
  .slider { display:flex; width:200%; height:100%; transition:transform .3s cubic-bezier(0.4,0,0.2,1); flex:1; overflow:hidden; }
  .slider.show-config { transform:translateX(-50%); }
  .pane { width:50%; flex-shrink:0; display:flex; flex-direction:column; overflow:hidden; }
  .cfg-header { display:flex; align-items:center; gap:10px; padding:14px 20px; border-bottom:1px solid var(--border); flex-shrink:0; }
  .cfg-back { width:28px; height:28px; border-radius:7px; background:var(--ibtn-bg); border:1px solid var(--border); display:flex; align-items:center; justify-content:center; cursor:pointer; flex-shrink:0; }
  .cfg-back:hover { background:rgba(255,255,255,0.12); } .cfg-back svg { width:14px; height:14px; stroke:rgba(255,255,255,0.6); }
  .cfg-title { font-size:13px; font-weight:600; color:var(--text-1); } .cfg-subtitle { font-size:10px; color:var(--text-3); margin-top:1px; }
  .cfg-add { margin-left:auto; display:flex; align-items:center; gap:5px; font-size:11px; color:var(--accent); background:rgba(233,84,32,0.1); border:1px solid rgba(233,84,32,0.2); border-radius:6px; padding:4px 10px; cursor:pointer; flex-shrink:0; }
  .cfg-add:hover { background:rgba(233,84,32,0.18); } .cfg-add svg { width:11px; height:11px; }
  .cfg-content { flex:1; overflow-y:auto; padding:12px 20px; display:flex; flex-direction:column; gap:4px; }
  .cfg-content::-webkit-scrollbar { width:3px; } .cfg-content::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }
  .share-row { display:flex; align-items:center; gap:10px; padding:10px 12px; border-radius:9px; background:var(--ibtn-bg); border:1px solid var(--border); }
  .share-ico { width:28px; height:28px; border-radius:7px; background:var(--active-bg); display:flex; align-items:center; justify-content:center; flex-shrink:0; }
  .share-ico svg { width:13px; height:13px; } .share-name { font-size:12px; font-weight:500; color:var(--text-1); }
  .share-path { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; margin-top:1px; }
  .pill-on { font-size:10px; color:var(--green); background:rgba(74,222,128,0.1); border:1px solid rgba(74,222,128,0.2); border-radius:5px; padding:2px 8px; margin-left:auto; flex-shrink:0; cursor:pointer; }
  .pill-off { font-size:10px; color:var(--text-3); background:var(--ibtn-bg); border:1px solid var(--border); border-radius:5px; padding:2px 8px; margin-left:auto; cursor:pointer; flex-shrink:0; transition:all .15s; }
  .pill-off:hover { color:var(--accent); border-color:rgba(233,84,32,0.3); background:rgba(233,84,32,0.08); }
  .dot { width:7px; height:7px; border-radius:50%; flex-shrink:0; background:rgba(255,255,255,0.15); }
  .dot-on { background:var(--green); box-shadow:0 0 5px rgba(74,222,128,.4); } .dot-err { background:#f87171; }
  .statusbar { display:flex; align-items:center; gap:12px; padding:8px 20px; border-top:1px solid var(--border); font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; flex-shrink:0; }
  .sb-online { width:6px; height:6px; border-radius:50%; background:var(--green); flex-shrink:0; } .sb-online.offline { background:rgba(255,255,255,0.15); }
  .empty-hint { text-align:center; padding:24px; border:1px dashed var(--border); border-radius:9px; color:var(--text-3); font-size:11px; line-height:1.6; }
</style>
