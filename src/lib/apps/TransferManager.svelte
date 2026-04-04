<script>
  import { uploadTasks, activeTasks, cancelTask, removeTask, clearDone, pauseTask, resumeTask } from '$lib/stores/uploadTasks.js';

  let activeTab = 'uploads'; // 'uploads' | 'downloads' | 'done' | 'errors'
  let selected = new Set();

  $: tabUploads   = $uploadTasks.filter(t => t.status === 'uploading' || t.status === 'paused' || t.status === 'queued');
  $: tabDownloads = []; // Future: download tasks
  $: tabDone      = $uploadTasks.filter(t => t.status === 'done');
  $: tabErrors    = $uploadTasks.filter(t => t.status === 'error');

  $: current = activeTab === 'uploads' ? tabUploads :
               activeTab === 'downloads' ? tabDownloads :
               activeTab === 'done' ? tabDone : tabErrors;

  $: activeCount = $uploadTasks.filter(t => t.status === 'uploading').length;
  $: queuedCount = $uploadTasks.filter(t => t.status === 'queued').length;
  $: totalSpeed  = $uploadTasks.reduce((s, t) => s + (t.status === 'uploading' ? (t.speed || 0) : 0), 0);

  function fmtSize(bytes) {
    if (!bytes) return '—';
    if (bytes >= 1e9) return (bytes / 1e9).toFixed(1) + ' GB';
    if (bytes >= 1e6) return (bytes / 1e6).toFixed(0) + ' MB';
    return (bytes / 1e3).toFixed(0) + ' KB';
  }

  function fmtSpeed(bps) {
    if (!bps || bps <= 0) return '—';
    if (bps >= 1e6) return (bps / 1e6).toFixed(1) + ' MB/s';
    if (bps >= 1e3) return (bps / 1e3).toFixed(0) + ' KB/s';
    return Math.round(bps) + ' B/s';
  }

  function fmtEta(task) {
    if (task.status !== 'uploading' || !task.speed || task.speed <= 0) return '—';
    const remaining = task.size * (1 - task.progress / 100);
    const secs = remaining / task.speed;
    if (secs > 3600) return Math.floor(secs / 3600) + 'h ' + Math.floor((secs % 3600) / 60) + 'm';
    if (secs > 60) return Math.floor(secs / 60) + 'm ' + Math.floor(secs % 60) + 's';
    return Math.floor(secs) + 's';
  }

  function toggleSelect(id) {
    if (selected.has(id)) selected.delete(id);
    else selected.add(id);
    selected = new Set(selected); // trigger reactivity
  }

  function pauseSelected() {
    selected.forEach(id => {
      const t = $uploadTasks.find(x => x.id === id);
      if (t?.status === 'uploading') pauseTask(id);
    });
  }

  function resumeSelected() {
    selected.forEach(id => {
      const t = $uploadTasks.find(x => x.id === id);
      if (t?.status === 'paused') resumeTask(id);
    });
  }

  function cancelSelected() {
    selected.forEach(id => cancelTask(id));
    selected = new Set();
  }

  function handleClearDone() {
    clearDone();
    selected = new Set();
  }

  function barClass(status) {
    const map = { uploading:'upload', done:'done', error:'error', paused:'paused', queued:'upload' };
    return map[status] || 'upload';
  }

  function pillClass(status) {
    const map = { uploading:'sp-uploading', paused:'sp-paused', done:'sp-done', error:'sp-error', queued:'sp-queue' };
    return map[status] || 'sp-queue';
  }

  function pillLabel(status) {
    const map = { uploading:'subiendo', paused:'pausado', done:'completado', error:'error', queued:'en cola' };
    return map[status] || status;
  }
</script>

<div class="tm">

<!-- TABS -->
<div class="tabs">
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="tab" class:active={activeTab==='uploads'} on:click={() => { activeTab='uploads'; selected=new Set(); }}>
    Subidas {#if tabUploads.length > 0}<span class="tab-count">{tabUploads.length}</span>{/if}
  </div>
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="tab" class:active={activeTab==='downloads'} on:click={() => { activeTab='downloads'; selected=new Set(); }}>
    Descargas {#if tabDownloads.length > 0}<span class="tab-count">{tabDownloads.length}</span>{/if}
  </div>
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="tab" class:active={activeTab==='done'} on:click={() => { activeTab='done'; selected=new Set(); }}>
    Completadas {#if tabDone.length > 0}<span class="tab-count">{tabDone.length}</span>{/if}
  </div>
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="tab" class:active={activeTab==='errors'} on:click={() => { activeTab='errors'; selected=new Set(); }}>
    Errores {#if tabErrors.length > 0}<span class="tab-count red">{tabErrors.length}</span>{/if}
  </div>
</div>

<!-- TOOLBAR -->
<div class="toolbar">
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <button class="tool-btn" on:click={pauseSelected} disabled={selected.size===0}>
    <svg viewBox="0 0 24 24"><rect x="6" y="4" width="4" height="16"/><rect x="14" y="4" width="4" height="16"/></svg>
    Pausar
  </button>
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <button class="tool-btn" on:click={resumeSelected} disabled={selected.size===0}>
    <svg viewBox="0 0 24 24"><polygon points="5 3 19 12 5 21 5 3"/></svg>
    Reanudar
  </button>
  <div class="tool-sep"></div>
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <button class="tool-btn danger" on:click={cancelSelected} disabled={selected.size===0}>
    <svg viewBox="0 0 24 24"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
    Cancelar
  </button>
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <button class="tool-btn" on:click={handleClearDone}>
    <svg viewBox="0 0 24 24"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14H6L5 6"/></svg>
    Limpiar
  </button>
</div>

<!-- LIST HEADER -->
<div class="list-header">
  <div></div>
  <div>Archivo</div>
  <div>Tamaño</div>
  <div>Velocidad</div>
  <div>Progreso</div>
  <div>ETA</div>
  <div>Estado</div>
</div>

<!-- LIST -->
<div class="list">
  {#if current.length === 0}
    <div class="empty">
      <svg viewBox="0 0 24 24"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
      <div class="empty-text">Sin transferencias</div>
    </div>
  {:else}
    {#each current as task (task.id)}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="transfer-row" class:selected={selected.has(task.id)} on:click={() => toggleSelect(task.id)}>
        <div class="row-ico">
          {#if task.status === 'uploading'}
            <svg width="16" height="16" viewBox="0 0 24 24" fill="#7c6fff" stroke="none" style="overflow:visible">
              <polygon points="12,2 20,12 15,12 15,22 9,22 9,12 4,12">
                <animateTransform attributeName="transform" type="translate" values="0,16;0,-16" dur="1.2s" repeatCount="indefinite"/>
                <animate attributeName="opacity" values="0;1;1;0" keyTimes="0;0.25;0.75;1" dur="1.2s" repeatCount="indefinite"/>
              </polygon>
              <polygon points="12,2 20,12 15,12 15,22 9,22 9,12 4,12">
                <animateTransform attributeName="transform" type="translate" values="0,16;0,-16" dur="1.2s" begin="-0.6s" repeatCount="indefinite"/>
                <animate attributeName="opacity" values="0;1;1;0" keyTimes="0;0.25;0.75;1" dur="1.2s" begin="-0.6s" repeatCount="indefinite"/>
              </polygon>
            </svg>
          {:else if task.status === 'paused'}
            <svg width="16" height="16" viewBox="0 0 24 24" fill="var(--amber)"><rect x="5" y="4" width="4" height="16" rx="1"/><rect x="15" y="4" width="4" height="16" rx="1"/></svg>
          {:else if task.status === 'queued'}
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--text-3)" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/></svg>
          {:else if task.status === 'done'}
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--green)" stroke-width="2" stroke-linecap="round"><polyline points="20 6 9 17 4 12"/></svg>
          {:else}
            <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--red)" stroke-width="2" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
          {/if}
        </div>

        <div class="row-file">
          <div class="row-name" title={task.name}>{task.name}</div>
          {#if task.error}
            <div class="row-dest">⚠ {task.error}</div>
          {/if}
        </div>

        <div class="row-size">{fmtSize(task.size)}</div>
        <div class="row-speed">{task.status === 'uploading' ? fmtSpeed(task.speed) : '—'}</div>

        <div class="row-progress">
          <div class="prog-wrap"><div class="prog-bar {barClass(task.status)}" style="width:{task.progress}%"></div></div>
          <div class="prog-pct">{Math.round(task.progress)}%</div>
        </div>

        <div class="row-eta">{fmtEta(task)}</div>

        <div class="row-status">
          <span class="status-pill {pillClass(task.status)}">{pillLabel(task.status)}</span>
        </div>
      </div>
    {/each}
  {/if}
</div>

<!-- STATUSBAR -->
<div class="statusbar">
  {#if activeCount > 0}
    <div class="sb-dot green"></div>
    <span>{activeCount} subiendo</span>
    <span class="sb-speed">↑ {fmtSpeed(totalSpeed)}</span>
  {:else}
    <div class="sb-dot grey"></div>
    <span>Sin actividad</span>
  {/if}
  <div class="sb-right">
    {#if queuedCount > 0}<span>{queuedCount} en cola</span>{/if}
    <span>{tabDone.length} completadas · {tabErrors.length} errores</span>
  </div>
</div>
</div>

<style>
  .tm { display:flex; flex-direction:column; height:100%; overflow:hidden; }

  /* TABS */
  .tabs {
    display:flex; gap:0;
    padding:0 14px;
    background:var(--bg-bar);
    border-bottom:1px solid var(--border);
    flex-shrink:0;
  }
  .tab {
    font-size:11px; font-weight:600; color:var(--text-3);
    padding:9px 0; margin-right:20px;
    border-bottom:2px solid transparent;
    cursor:pointer; transition:all .15s;
    display:flex; align-items:center; gap:5px;
  }
  .tab:hover:not(.active) { color:var(--text-2); }
  .tab.active { color:var(--text-1); border-bottom-color:var(--accent); }
  .tab-count {
    font-size:9px; font-weight:700;
    padding:1px 5px; border-radius:5px;
    background:var(--active-bg); color:var(--accent);
  }
  .tab-count.red { background:rgba(248,113,113,0.15); color:var(--red); }

  /* TOOLBAR */
  .toolbar {
    display:flex; align-items:center; gap:6px;
    padding:8px 14px;
    border-bottom:1px solid var(--border);
    flex-shrink:0;
  }
  .tool-btn {
    padding:5px 12px; border-radius:7px; border:none;
    background:var(--ibtn-bg); color:var(--text-2);
    font-family:inherit; font-size:11px; font-weight:500;
    cursor:pointer; transition:all .15s;
    display:flex; align-items:center; gap:5px;
  }
  .tool-btn svg { width:11px; height:11px; stroke:currentColor; fill:none; stroke-width:2; stroke-linecap:round; }
  .tool-btn:hover { background:rgba(124,111,255,0.12); color:var(--text-1); }
  .tool-btn:disabled { opacity:.3; cursor:not-allowed; }
  .tool-btn.danger:hover { background:rgba(248,113,113,0.12); color:var(--red); }
  .tool-sep { width:1px; height:16px; background:var(--border); margin:0 2px; }

  /* LIST HEADER */
  .list-header {
    display:grid;
    grid-template-columns: 24px 1fr 80px 80px 140px 70px 80px;
    gap:0; padding:6px 14px;
    font-size:9px; font-weight:600; color:var(--text-3);
    letter-spacing:.06em; text-transform:uppercase;
    border-bottom:1px solid var(--border);
    background:var(--bg-bar);
    position:sticky; top:0; z-index:1;
  }

  /* LIST */
  .list { flex:1; overflow-y:auto; }
  .list::-webkit-scrollbar { width:3px; }
  .list::-webkit-scrollbar-thumb { background:var(--border); border-radius:2px; }

  /* ROW */
  .transfer-row {
    display:grid;
    grid-template-columns: 24px 1fr 80px 80px 140px 70px 80px;
    gap:0; padding:10px 14px;
    border-bottom:1px solid var(--border);
    align-items:center;
    transition:background .12s;
    cursor:pointer;
  }
  .transfer-row:hover { background:var(--ibtn-bg); }
  .transfer-row.selected { background:var(--active-bg); }

  .row-ico { display:flex; align-items:center; justify-content:center; }
  .row-file { min-width:0; }
  .row-name { font-size:11px; font-weight:500; color:var(--text-1); overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .row-dest { font-size:9px; color:var(--red); font-family:'DM Mono',monospace; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; margin-top:1px; }
  .row-size { font-size:10px; color:var(--text-2); font-family:'DM Mono',monospace; }
  .row-speed { font-size:10px; color:var(--text-2); font-family:'DM Mono',monospace; }

  /* PROGRESS */
  .row-progress { padding-right:12px; }
  .prog-wrap { height:4px; background:var(--border); border-radius:2px; overflow:hidden; margin-bottom:3px; }
  .prog-bar { height:100%; border-radius:2px; transition:width .4s ease; }
  .prog-bar.upload { background:var(--accent); }
  .prog-bar.done { background:var(--green); }
  .prog-bar.error { background:var(--red); }
  .prog-bar.paused { background:var(--amber); }
  .prog-pct { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; }

  .row-eta { font-size:10px; color:var(--text-2); font-family:'DM Mono',monospace; }

  /* STATUS PILL */
  .status-pill {
    font-size:9px; font-weight:600;
    padding:2px 7px; border-radius:5px;
    display:inline-block;
  }
  .sp-uploading { background:rgba(124,111,255,0.12); color:var(--accent); }
  .sp-paused   { background:rgba(251,191,36,0.12); color:var(--amber); }
  .sp-done     { background:rgba(74,222,128,0.12); color:var(--green); }
  .sp-error    { background:rgba(248,113,113,0.12); color:var(--red); }
  .sp-queue    { background:rgba(255,255,255,0.06); color:var(--text-3); }

  /* EMPTY */
  .empty { display:flex; flex-direction:column; align-items:center; justify-content:center; height:200px; gap:8px; }
  .empty svg { width:32px; height:32px; stroke:var(--text-3); fill:none; stroke-width:1.5; stroke-linecap:round; }
  .empty-text { font-size:12px; color:var(--text-3); }

  /* STATUSBAR */
  .statusbar {
    display:flex; align-items:center; gap:10px;
    padding:7px 14px;
    border-top:1px solid var(--border);
    background:var(--bg-bar);
    flex-shrink:0; font-size:10px; color:var(--text-3);
  }
  .sb-speed { font-family:'DM Mono',monospace; }
  .sb-dot { width:6px; height:6px; border-radius:50%; flex-shrink:0; }
  .sb-dot.green { background:var(--green); box-shadow:0 0 5px var(--green); animation:sbPulse .8s ease-in-out infinite; }
  .sb-dot.grey { background:var(--text-3); }
  @keyframes sbPulse { 0%,100%{opacity:1} 50%{opacity:.4} }
  .sb-right { margin-left:auto; display:flex; align-items:center; gap:10px; }
</style>
