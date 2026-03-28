<script>
  import { onMount } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';
  import NimLink from '$lib/apps/NimLink.svelte';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}`, 'Content-Type': 'application/json' });

  let view = 'resumen';
  let devices = [];
  let jobs = [];
  let history = [];
  let activeDevice = null;
  let showWizard = false;
  let wizardMode = 'pair';
  let configPane = null;
  let remoteShares = [];
  let sharesLoading = false;

  function isLocal(a) { return a.startsWith('192.168.') || a.startsWith('10.') || a.startsWith('172.') || a === 'localhost'; }

  async function loadDevices() { try { const r = await fetch('/api/backup/devices', { headers: hdrs() }); const d = await r.json(); devices = d.devices || []; } catch { devices = []; } }
  async function loadJobs() { try { const r = await fetch('/api/backup/jobs', { headers: hdrs() }); const d = await r.json(); jobs = d.jobs || []; } catch { jobs = []; } }
  async function loadHistory() { try { const r = await fetch('/api/backup/history', { headers: hdrs() }); const d = await r.json(); history = d.history || []; } catch { history = []; } }
  async function runJob(id) { try { await fetch(`/api/backup/run/${id}`, { method:'POST', headers:hdrs() }); await loadJobs(); } catch {} }
  async function removeDevice(id) { if (!confirm('¿Desemparejar este dispositivo?')) return; try { await fetch(`/api/backup/devices/${id}`, { method:'DELETE', headers:hdrs() }); devices = devices.filter(d => d.id !== id); activeDevice = null; view = 'resumen'; } catch {} }
  async function savePurposes(id, p) { try { await fetch(`/api/backup/devices/${id}/purposes`, { method:'POST', headers:hdrs(), body:JSON.stringify({ purposes:p }) }); } catch {} }
  async function loadRemoteShares(id) { sharesLoading = true; try { const r = await fetch(`/api/backup/devices/${id}/remote-shares`, { headers:hdrs() }); const d = await r.json(); remoteShares = d.shares || []; } catch { remoteShares = []; } sharesLoading = false; }
  async function mountShare(id, s) { s._m = true; remoteShares = [...remoteShares]; try { const r = await fetch(`/api/backup/devices/${id}/mount`, { method:'POST', headers:hdrs(), body:JSON.stringify({ shareName:s.name, remotePath:s.path }) }); const d = await r.json(); if (d.ok) { s.mounted = true; s.mountPoint = d.mountPoint; } } catch {} s._m = false; remoteShares = [...remoteShares]; }
  async function unmountShare(id, s) { s._m = true; remoteShares = [...remoteShares]; try { const r = await fetch(`/api/backup/devices/${id}/unmount`, { method:'POST', headers:hdrs(), body:JSON.stringify({ shareName:s.name }) }); const d = await r.json(); if (d.ok) { s.mounted = false; s.mountPoint = ''; } } catch {} s._m = false; remoteShares = [...remoteShares]; }

  function togglePurpose(key) {
    if (!activeDevice) return;
    const p = activeDevice.purposes || [];
    activeDevice.purposes = p.includes(key) ? p.filter(x => x !== key) : [...p, key];
    activeDevice = {...activeDevice};
    savePurposes(activeDevice.id, activeDevice.purposes);
  }
  function openConfig(type) { configPane = type; if (type === 'share' && activeDevice) loadRemoteShares(activeDevice.id); }

  function fmtTime(iso) { if (!iso) return '—'; const d = new Date(iso); const now = new Date(); const diff = Math.floor((now - d) / 1000); if (diff < 3600) return `hace ${Math.floor(diff/60)}m`; if (diff < 86400) return `hace ${Math.floor(diff/3600)}h`; return `hace ${Math.floor(diff/86400)}d`; }
  function fmtSize(b) { if (!b) return '—'; if (b >= 1e9) return (b/1e9).toFixed(1)+' GB'; if (b >= 1e6) return (b/1e6).toFixed(0)+' MB'; return (b/1e3).toFixed(0)+' KB'; }

  $: onlineCount = devices.filter(d => d.online).length;
  $: jobsOk = jobs.filter(j => j.status === 'ok').length;
  $: nextJob = jobs.filter(j => j.nextRun).sort((a,b) => new Date(a.nextRun) - new Date(b.nextRun))[0];
  $: deviceJobs = activeDevice ? jobs.filter(j => j.deviceId === activeDevice.id) : [];
  $: mountedShares = remoteShares.filter(s => s.mounted);

  const SERVICES = [
    { key:'share', name:'Share remota', desc:'Carpetas de este NAS visibles en Files', color:'var(--green)', bg:'rgba(74,222,128,0.12)', icon:'folder' },
    { key:'backup_dest', name:'Backup destino', desc:'Este NAS recibe tus backups', color:'var(--blue)', bg:'rgba(96,165,250,0.12)', icon:'down' },
    { key:'backup_src', name:'Backup origen', desc:'Este NAS hace backup al tuyo', color:'var(--accent)', bg:'rgba(124,111,255,0.12)', icon:'up' },
    { key:'sync', name:'Sincronización', desc:'Carpetas espejo entre los dos NAS', color:'var(--amber)', bg:'rgba(251,191,36,0.12)', icon:'sync' },
  ];

  onMount(() => { loadDevices(); loadJobs(); loadHistory(); });
</script>

<div class="backup-root">
  <!-- ═ SIDEBAR ═ -->
  <div class="sidebar">
    <div class="sb-header">
      <svg viewBox="0 0 24 24" fill="none" stroke="var(--accent)" stroke-width="2" stroke-linecap="round" style="width:16px;height:16px;flex-shrink:0"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
      <span class="title">NimBackup</span>
    </div>
    <div class="sb-section">General</div>
    <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={view==='resumen'&&!activeDevice} on:click={()=>{view='resumen';activeDevice=null;configPane=null;}}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="3" width="7" height="7"/><rect x="14" y="3" width="7" height="7"/><rect x="3" y="14" width="7" height="7"/><rect x="14" y="14" width="7" height="7"/></svg>
      <span>Resumen</span>
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={view==='historial'} on:click={()=>{view='historial';activeDevice=null;configPane=null;loadHistory();}}>
      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
      <span>Historial</span>
    </div>
    <div class="sb-section" style="margin-top:6px">Dispositivos</div>
    {#each devices as dev}
      <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="sb-item" class:active={activeDevice?.id===dev.id} on:click={()=>{activeDevice=dev;view='device';configPane=null;loadRemoteShares(dev.id);}}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8"><ellipse cx="12" cy="5" rx="9" ry="3"/><path d="M21 12c0 1.66-4 3-9 3s-9-1.34-9-3"/><path d="M3 5v14c0 1.66 4 3 9 3s9-1.34 9-3V5"/></svg>
        <span>{dev.name}</span>
        {#if dev.online}<div class="sb-dot"></div>{/if}
      </div>
    {/each}
    {#if devices.length===0}<div style="font-size:11px;color:var(--text-3);padding:8px 10px">Sin dispositivos</div>{/if}
    <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-add" on:click={()=>{wizardMode='pair';showWizard=true;}}>
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

  <!-- ═ INNER WRAP ═ -->
  <div class="inner-wrap">
    <div class="inner">

      <!-- === RESUMEN === -->
      {#if view==='resumen'&&!activeDevice}
        <div class="inner-titlebar">
          <span class="tb-title">Resumen</span>
          <span class="tb-sub">— {onlineCount} de {devices.length} dispositivos online</span>
        </div>
        <div class="content">
          <div class="stats-row">
            <div class="stat-card"><div class="stat-lbl">Dispositivos</div><div class="stat-val" style="color:var(--green)">{onlineCount}/{devices.length}</div><div class="stat-sub">online</div></div>
            <div class="stat-card"><div class="stat-lbl">Trabajos OK</div><div class="stat-val" style="color:var(--accent)">{jobsOk}/{jobs.length}</div><div class="stat-sub">activos</div></div>
            <div class="stat-card"><div class="stat-lbl">Último backup</div><div class="stat-val" style="font-size:13px">{history.length>0?fmtTime(history[0]?.time):'—'}</div><div class="stat-sub">{history[0]?.jobName||'—'}</div></div>
          </div>
          {#if jobs.length>0}
            <div class="section-label">Trabajos activos</div>
            {#each jobs as job}
              <div class="row">
                <div class="row-icon" style="background:{job.fsType==='btrfs'?'rgba(74,222,128,0.1)':'rgba(96,165,250,0.1)'}">
                  <svg viewBox="0 0 24 24" fill="none" stroke={job.fsType==='btrfs'?'var(--green)':'var(--blue)'} stroke-width="1.8" stroke-linecap="round" style="width:14px;height:14px"><rect x="2" y="3" width="20" height="8" rx="2"/><circle cx="18" cy="7" r="1" fill="currentColor" stroke="none"/></svg>
                </div>
                <div class="row-info"><div class="row-name">{job.name}</div><div class="row-meta">{job.fsType} · {job.schedule} · {fmtTime(job.lastRun)}</div></div>
                <div class="dot" class:dot-on={job.status==='ok'} class:dot-err={job.status==='error'}></div>
                <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
                <button class="btn-secondary" style="padding:3px 8px;font-size:10px" on:click={()=>runJob(job.id)}>▶</button>
              </div>
            {/each}
          {:else}<div class="empty-hint">Sin trabajos configurados. Empareja un dispositivo para empezar.</div>{/if}
        </div>
        <div class="statusbar"><div class="status-dot"></div><span>{onlineCount} online</span><span class="st-sep">·</span><span>{jobs.length} trabajos</span></div>

      <!-- === HISTORIAL === -->
      {:else if view==='historial'}
        <div class="inner-titlebar">
          <span class="tb-title">Historial</span>
          <span class="tb-sub">— {history.length} ejecuciones</span>
        </div>
        <div class="content">
          {#each history as h}
            <div class="row">
              <div class="row-icon" style="background:{h.ok?'rgba(74,222,128,0.1)':'rgba(248,113,113,0.1)'}">
                {#if h.ok}<svg viewBox="0 0 24 24" fill="none" stroke="var(--green)" stroke-width="2.5" stroke-linecap="round" style="width:12px;height:12px"><polyline points="20 6 9 17 4 12"/></svg>
                {:else}<svg viewBox="0 0 24 24" fill="none" stroke="var(--red)" stroke-width="2.5" stroke-linecap="round" style="width:12px;height:12px"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>{/if}
              </div>
              <div class="row-info"><div class="row-name" style="color:{h.ok?'var(--text-1)':'var(--red)'}">{h.jobName}</div><div class="row-meta">{h.dest} · {fmtSize(h.bytes)}</div></div>
              <span class="row-meta">{fmtTime(h.time)}</span>
            </div>
          {/each}
          {#if history.length===0}<div class="empty-hint">Sin historial todavía.</div>{/if}
        </div>
        <div class="statusbar"><div class="status-dot"></div><span>{history.length} ejecuciones</span></div>

      <!-- === DEVICE === -->
      {:else if view==='device'&&activeDevice}
        {#if configPane===null}
        <div class="inner-titlebar">
          <span class="tb-title">{activeDevice.name}</span>
          <span class="tb-sub">— {activeDevice.addr} · {isLocal(activeDevice.addr)?'LAN':'WAN'} · {activeDevice.ping||'—'}</span>
          <div class="tb-right">
            <div class="dev-badge" class:offline={!activeDevice.online}><div class="dev-badge-dot"></div>{activeDevice.online?'Online':'Offline'}</div>
            <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
            <button class="icon-btn" style="color:var(--red)" on:click={()=>removeDevice(activeDevice.id)}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          </div>
        </div>
        {/if}

        <div class="slider" class:show-config={configPane!==null}>
          <!-- PANE 1: overview -->
          <div class="pane">
            <div class="content">
              <div class="stats-row">
                <div class="stat-card"><div class="stat-lbl">Latencia</div><div class="stat-val" style="color:var(--green)">{activeDevice.ping||'—'}</div><div class="stat-sub">{isLocal(activeDevice.addr)?'LAN directa':'WireGuard'}</div></div>
                <div class="stat-card"><div class="stat-lbl">Espacio libre</div><div class="stat-val" style="color:var(--blue)">{activeDevice.freeSpace||'—'}</div><div class="stat-sub">disponible</div></div>
                <div class="stat-card"><div class="stat-lbl">Versión</div><div class="stat-val" style="font-size:12px;margin-top:2px">{activeDevice.version||'—'}</div><div class="stat-sub">NimOS</div></div>
              </div>
              {#if mountedShares.length>0}
                <div class="section-label">Carpetas compartidas</div>
                {#each mountedShares as share}
                  <div class="donut-card">
                    <div class="donut-wrap">
                      <svg viewBox="0 0 72 72"><circle cx="36" cy="36" r="28" fill="none" stroke="var(--border)" stroke-width="9"/><circle cx="36" cy="36" r="28" fill="none" stroke="var(--green)" stroke-width="9" stroke-dasharray="28 148" stroke-linecap="round" transform="rotate(-90 36 36)"/></svg>
                      <div class="donut-center"><span style="font-size:14px;font-weight:700;color:var(--green)">—</span></div>
                    </div>
                    <div class="donut-info">
                      <div style="font-size:13px;font-weight:600;color:var(--text-1)">{share.displayName||share.name}</div>
                      <div style="font-size:10px;color:var(--text-3);font-family:'DM Mono',monospace">{share.path}</div>
                      {#if share.mountPoint}<div class="mount-badge">
                        <svg viewBox="0 0 24 24" fill="none" stroke="var(--green)" stroke-width="2" style="width:10px;height:10px"><polyline points="20 6 9 17 4 12"/></svg>
                        Montada en Files
                      </div>{/if}
                    </div>
                  </div>
                {/each}
              {/if}
              <div class="section-label">Servicios</div>
              {#each SERVICES as svc}
                <div class="row">
                  <div class="row-icon" style="background:{svc.bg}">
                    {#if svc.icon==='folder'}<svg viewBox="0 0 24 24" fill="none" stroke={svc.color} stroke-width="1.8" stroke-linecap="round" style="width:14px;height:14px"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
                    {:else if svc.icon==='down'}<svg viewBox="0 0 24 24" fill="none" stroke={svc.color} stroke-width="1.8" stroke-linecap="round" style="width:14px;height:14px"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                    {:else if svc.icon==='up'}<svg viewBox="0 0 24 24" fill="none" stroke={svc.color} stroke-width="1.8" stroke-linecap="round" style="width:14px;height:14px"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                    {:else}<svg viewBox="0 0 24 24" fill="none" stroke={svc.color} stroke-width="1.8" stroke-linecap="round" style="width:14px;height:14px"><polyline points="17 1 21 5 17 9"/><path d="M3 11V9a4 4 0 0 1 4-4h14"/><polyline points="7 23 3 19 7 15"/><path d="M21 13v2a4 4 0 0 1-4 4H3"/></svg>
                    {/if}
                  </div>
                  <div class="row-info"><div class="row-name">{svc.name}</div><div class="row-meta">{svc.desc}</div></div>
                  <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
                  <button class="icon-btn" on:click={()=>openConfig(svc.key)}>
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M4.93 4.93a10 10 0 0 0 0 14.14"/></svg>
                  </button>
                  <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
                  <div class="toggle" class:on={activeDevice.purposes?.includes(svc.key)} on:click={()=>togglePurpose(svc.key)}><div class="toggle-dot"></div></div>
                </div>
              {/each}
            </div>
          </div>

          <!-- PANE 2: config detail -->
          <div class="pane">
            <div class="cfg-header">
              <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
              <button class="icon-btn" on:click={()=>configPane=null}><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="15 18 9 12 15 6"/></svg></button>
              <div><div class="tb-title" style="font-size:13px">{SERVICES.find(s=>s.key===configPane)?.name||''}</div><div class="tb-sub" style="font-size:10px">{activeDevice.name}</div></div>
              <div class="tb-right">
                <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
                <button class="btn-secondary" on:click={()=>{if(configPane==='share')loadRemoteShares(activeDevice.id);else if(configPane==='sync'){wizardMode='sync';showWizard=true;}else{wizardMode='job';showWizard=true;}}}>
                  {#if configPane==='share'}
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="width:12px;height:12px"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.18-5.4"/></svg>
                    Refrescar
                  {:else if configPane==='sync'}
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" style="width:11px;height:11px"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
                    Añadir par
                  {:else}
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" style="width:11px;height:11px"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
                    Nuevo trabajo
                  {/if}
                </button>
                <div class="dev-badge" class:offline={!activeDevice.online}><div class="dev-badge-dot"></div>{activeDevice.online?'Online':'Offline'}</div>
              </div>
            </div>
            <div class="content">
              {#if configPane==='share'}
                {#if sharesLoading}<div class="empty-hint">Cargando shares...</div>
                {:else if remoteShares.length===0}<div class="empty-hint">No se encontraron carpetas compartidas.</div>
                {:else}
                  {#each remoteShares as share}
                    <div class="row">
                      <div class="row-icon" style="background:rgba(124,111,255,0.1)"><svg viewBox="0 0 24 24" fill="none" stroke="var(--accent)" stroke-width="1.8" stroke-linecap="round" style="width:13px;height:13px"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg></div>
                      <div class="row-info"><div class="row-name">{share.displayName||share.name}</div><div class="row-meta">{share.path}</div></div>
                      {#if share.mounted}
                        <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
                        <span class="pill pill-danger" on:click={()=>unmountShare(activeDevice.id,share)}>{share._m?'...':'Desmontar'}</span>
                        <span class="pill pill-on">Montada</span>
                      {:else}
                        <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
                        <span class="pill pill-off" on:click={()=>mountShare(activeDevice.id,share)}>{share._m?'...':'Montar'}</span>
                      {/if}
                    </div>
                  {/each}
                {/if}
              {:else if configPane==='backup_dest'||configPane==='backup_src'}
                {#each deviceJobs as job}
                  <div class="row">
                    <div class="row-icon" style="background:rgba(96,165,250,0.1)"><svg viewBox="0 0 24 24" fill="none" stroke="var(--blue)" stroke-width="1.8" stroke-linecap="round" style="width:13px;height:13px"><rect x="2" y="3" width="20" height="8" rx="2"/><circle cx="18" cy="7" r="1" fill="currentColor" stroke="none"/></svg></div>
                    <div class="row-info"><div class="row-name">{job.name}</div><div class="row-meta">{job.source} → {job.dest} · {job.schedule}</div></div>
                    <div class="dot" class:dot-on={job.status==='ok'} class:dot-err={job.status==='error'}></div>
                    <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
                    <button class="btn-secondary" style="padding:3px 8px;font-size:10px" on:click={()=>runJob(job.id)}>▶</button>
                  </div>
                {/each}
                {#if deviceJobs.length===0}<div class="empty-hint">Sin trabajos configurados.</div>{/if}
              {:else if configPane==='sync'}
                {#each (activeDevice.syncPairs||[]) as pair}
                  <div class="row">
                    <div class="row-icon" style="background:rgba(251,191,36,0.1)"><svg viewBox="0 0 24 24" fill="none" stroke="var(--amber)" stroke-width="1.8" stroke-linecap="round" style="width:13px;height:13px"><polyline points="17 1 21 5 17 9"/><path d="M3 11V9a4 4 0 0 1 4-4h14"/><polyline points="7 23 3 19 7 15"/><path d="M21 13v2a4 4 0 0 1-4 4H3"/></svg></div>
                    <div class="row-info"><div class="row-name">{pair.local}</div><div class="row-meta">↔ {pair.remote}</div></div>
                    <span class="pill pill-on">{pair.status==='synced'?'Sync':'Pendiente'}</span>
                  </div>
                {/each}
                {#if (activeDevice.syncPairs||[]).length===0}<div class="empty-hint">Sin pares de sincronización.</div>{/if}
              {/if}
            </div>
          </div>
        </div>

        <div class="statusbar">
          <div class="status-dot" class:offline={!activeDevice.online}></div>
          <span>{isLocal(activeDevice.addr)?'LAN · Puerto 5000':'WAN · Puerto 5009'}</span>
          <span class="st-sep">·</span><span>{deviceJobs.length} trabajos</span>
          <span class="st-sep">·</span><span>{mountedShares.length} shares montadas</span>
        </div>
      {/if}
    </div>
  </div>

  {#if showWizard}
    <NimLink mode={wizardMode} device={activeDevice}
      on:close={()=>{showWizard=false;}}
      on:paired={()=>{showWizard=false;loadDevices();}}
      on:created={()=>{showWizard=false;loadJobs();loadDevices();}} />
  {/if}
</div>

<style>
  .backup-root { width:100%; height:100%; display:flex; overflow:hidden; font-family:'Inter',-apple-system,sans-serif; color:var(--text-1); }

  /* Sidebar — matches FileManager/NimTorrent */
  .sidebar { width:190px; flex-shrink:0; display:flex; flex-direction:column; gap:2px; padding:12px 8px; overflow-y:auto; }
  .sidebar::-webkit-scrollbar { width:3px; } .sidebar::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.2); border-radius:2px; }
  .sb-header { display:flex; align-items:center; gap:8px; padding:32px 8px 12px; }
  .title { font-size:15px; font-weight:600; color:var(--text-1); }
  .sb-section { font-size:9px; font-weight:700; letter-spacing:.1em; text-transform:uppercase; color:var(--text-3); padding:10px 8px 3px; }
  .sb-item { display:flex; align-items:center; gap:8px; padding:6px 10px; border-radius:8px; cursor:pointer; font-size:12px; color:var(--text-2); border:1px solid transparent; transition:all .15s; }
  .sb-item svg { width:13px; height:13px; flex-shrink:0; opacity:.6; }
  .sb-item:hover { background:rgba(128,128,128,0.10); color:var(--text-1); }
  .sb-item.active { background:var(--active-bg); color:var(--text-1); border-color:var(--border-hi); }
  .sb-item.active svg { opacity:1; }
  .sb-dot { width:7px; height:7px; border-radius:50%; background:var(--green); margin-left:auto; flex-shrink:0; box-shadow:0 0 5px rgba(74,222,128,.4); }
  .sb-add { display:flex; align-items:center; gap:7px; padding:7px 10px; border-radius:8px; font-size:11px; color:var(--text-3); cursor:pointer; border:1px dashed rgba(255,255,255,0.1); transition:all .15s; margin-top:4px; }
  .sb-add:hover { color:var(--accent); border-color:rgba(124,111,255,.3); }
  .sb-add svg { width:11px; height:11px; }
  .sb-next { margin-top:auto; padding:9px 10px; background:rgba(255,255,255,0.04); border:1px solid var(--border); border-radius:9px; }
  .sn-label { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; margin-bottom:3px; }
  .sn-name { font-size:10px; color:var(--text-2); }
  .sn-time { font-size:13px; font-weight:600; color:var(--accent); margin-top:2px; }

  /* Inner wrap — the NimOS app frame pattern */
  .inner-wrap { flex:1; padding:8px; display:flex; }
  .inner { flex:1; border-radius:10px; border:1px solid var(--border); background:var(--bg-inner); display:flex; flex-direction:column; overflow:hidden; }

  /* Titlebar */
  .inner-titlebar { display:flex; align-items:center; gap:8px; padding:10px 14px 9px; background:var(--bg-bar); flex-shrink:0; border-bottom:1px solid var(--border); }
  .tb-title { font-size:12px; font-weight:600; color:var(--text-1); }
  .tb-sub { font-size:11px; color:var(--text-3); }
  .tb-right { margin-left:auto; display:flex; align-items:center; gap:6px; }

  /* Badge */
  .dev-badge { display:flex; align-items:center; gap:5px; font-size:10px; color:var(--green); background:rgba(74,222,128,0.1); border:1px solid rgba(74,222,128,0.2); padding:3px 9px; border-radius:20px; }
  .dev-badge.offline { color:var(--text-3); background:rgba(255,255,255,0.04); border-color:var(--border); }
  .dev-badge-dot { width:6px; height:6px; border-radius:50%; background:currentColor; }

  /* Content */
  .content { flex:1; overflow-y:auto; padding:16px; display:flex; flex-direction:column; gap:14px; }
  .content::-webkit-scrollbar { width:3px; } .content::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }

  /* Stats */
  .stats-row { display:grid; grid-template-columns:repeat(3,1fr); gap:8px; }
  .stat-card { background:rgba(255,255,255,0.025); border:1px solid var(--border); border-radius:9px; padding:11px 13px; }
  .stat-lbl { font-size:9px; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; margin-bottom:4px; }
  .stat-val { font-size:15px; font-weight:600; color:var(--text-1); }
  .stat-sub { font-size:9px; color:var(--text-3); margin-top:2px; font-family:'DM Mono',monospace; }

  .section-label { font-size:9px; font-weight:700; color:var(--text-3); text-transform:uppercase; letter-spacing:.08em; }

  /* Rows */
  .row { display:flex; align-items:center; gap:10px; padding:9px 4px; border-bottom:1px solid var(--border); transition:background .12s; }
  .row:first-of-type { border-top:1px solid var(--border); }
  .row:hover { background:rgba(255,255,255,0.02); }
  .row-icon { width:28px; height:28px; border-radius:7px; flex-shrink:0; display:flex; align-items:center; justify-content:center; }
  .row-info { flex:1; min-width:0; }
  .row-name { font-size:12px; font-weight:600; color:var(--text-1); }
  .row-meta { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; margin-top:1px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }

  /* Donut card */
  .donut-card { background:rgba(255,255,255,0.025); border:1px solid var(--border); border-radius:10px; padding:14px 16px; display:flex; align-items:center; gap:16px; }
  .donut-wrap { position:relative; width:72px; height:72px; flex-shrink:0; }
  .donut-wrap svg { width:72px; height:72px; }
  .donut-center { position:absolute; inset:0; display:flex; align-items:center; justify-content:center; }
  .donut-info { flex:1; display:flex; flex-direction:column; gap:3px; }
  .mount-badge { display:inline-flex; align-items:center; gap:4px; font-size:10px; background:rgba(74,222,128,0.1); color:var(--green); border:1px solid rgba(74,222,128,0.2); border-radius:5px; padding:2px 7px; margin-top:4px; }

  /* Buttons */
  .icon-btn { width:27px; height:27px; background:var(--ibtn-bg); border:1px solid var(--border); border-radius:6px; display:flex; align-items:center; justify-content:center; cursor:pointer; color:var(--text-2); transition:all .15s; }
  .icon-btn svg { width:13px; height:13px; }
  .icon-btn:hover { background:rgba(124,111,255,0.15); color:var(--text-1); }
  .btn-secondary { display:inline-flex; align-items:center; gap:5px; padding:5px 10px; background:var(--ibtn-bg); border:1px solid var(--border); border-radius:6px; color:var(--text-2); font-family:inherit; font-size:11px; font-weight:500; cursor:pointer; transition:all .15s; }
  .btn-secondary svg { width:11px; height:11px; }
  .btn-secondary:hover { color:var(--text-1); border-color:var(--border-hi); }

  /* Toggle */
  .toggle { width:30px; height:17px; border-radius:9px; background:rgba(255,255,255,0.1); border:none; cursor:pointer; position:relative; transition:background .2s; flex-shrink:0; }
  .toggle.on { background:var(--accent); }
  .toggle-dot { position:absolute; top:2px; left:2px; width:13px; height:13px; border-radius:50%; background:#fff; transition:transform .2s; opacity:.6; }
  .toggle.on .toggle-dot { transform:translateX(13px); opacity:1; }

  /* Pills */
  .pill { font-size:10px; border-radius:5px; padding:2px 8px; flex-shrink:0; cursor:pointer; }
  .pill-on { color:var(--green); background:rgba(74,222,128,0.1); border:1px solid rgba(74,222,128,0.2); }
  .pill-off { color:var(--text-3); background:rgba(255,255,255,0.04); border:1px solid var(--border); }
  .pill-off:hover { color:var(--accent); border-color:var(--border-hi); }
  .pill-danger { color:var(--red); background:rgba(248,113,113,0.1); border:1px solid rgba(248,113,113,0.2); }

  /* Dots */
  .dot { width:7px; height:7px; border-radius:50%; flex-shrink:0; background:rgba(255,255,255,0.15); }
  .dot-on { background:var(--green); box-shadow:0 0 5px rgba(74,222,128,.4); }
  .dot-err { background:var(--red); }

  /* Slider */
  .slider { display:flex; width:200%; transition:transform .3s cubic-bezier(0.4,0,0.2,1); flex:1; overflow:hidden; }
  .slider.show-config { transform:translateX(-50%); }
  .pane { width:50%; flex-shrink:0; display:flex; flex-direction:column; overflow:hidden; }

  /* Config header */
  .cfg-header { display:flex; align-items:center; gap:10px; padding:10px 14px; border-bottom:1px solid var(--border); flex-shrink:0; background:var(--bg-bar); }

  /* Statusbar */
  .statusbar { display:flex; align-items:center; gap:8px; padding:9px 14px; border-top:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; font-size:10px; color:var(--text-3); border-radius:0 0 10px 10px; font-family:'DM Mono',monospace; }
  .status-dot { width:6px; height:6px; border-radius:50%; background:var(--green); box-shadow:0 0 4px rgba(74,222,128,.5); }
  .status-dot.offline { background:rgba(255,255,255,0.15); box-shadow:none; }
  .st-sep { color:rgba(255,255,255,0.1); }

  /* Empty */
  .empty-hint { text-align:center; padding:28px; border:1px dashed var(--border); border-radius:9px; color:var(--text-3); font-size:11px; line-height:1.6; }
</style>
