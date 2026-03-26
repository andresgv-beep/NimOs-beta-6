<script>
  import { onMount, onDestroy } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  let torrents = [];
  let activeTab = 'all';
  let loading = true;
  let pollInterval;

  // Add torrent state
  let showAddModal = false;
  let addMode = 'file';
  let magnetLink = '';
  let selectedFile = null;
  let savePath = '/data/torrents';
  let shares = [];
  let addMsg = '';
  let addMsgError = false;
  let adding = false;

  // Delete
  let showDeleteConfirm = null;
  let deleteWithFiles = false;
  let selectedTorrent = null;

  async function fetchTorrents() {
    try {
      const r = await fetch('/api/torrent/torrents', { headers: hdrs() });
      const d = await r.json();
      const raw = Array.isArray(d) ? d : (d.torrents || []);
      // Normalize torrentd fields
      torrents = raw.map(t => ({
        ...t,
        progress:      (t.progress != null && t.progress <= 1) ? t.progress * 100 : (t.progress || 0),
        downloaded:    t.total_done    ?? t.downloaded ?? 0,
        size:          t.total_wanted  ?? t.size ?? t.totalSize ?? 0,
        dlSpeed:       t.download_rate ?? t.dlSpeed ?? t.downloadSpeed ?? 0,
        ulSpeed:       t.upload_rate   ?? t.ulSpeed ?? t.uploadSpeed ?? 0,
        numPeers:      t.peers         ?? t.numPeers ?? 0,
        numSeeds:      t.seeds         ?? t.numSeeds ?? 0,
        status:        t.paused ? 'paused' : (t.state || t.status || 'unknown'),
        savePath:      t.save_path     ?? t.savePath ?? '',
      }));
    } catch { torrents = []; }
    loading = false;
  }

  async function fetchShares() {
    try {
      const r = await fetch('/api/shares', { headers: hdrs() });
      const d = await r.json();
      shares = d.shares || d || [];
    } catch { shares = []; }
  }

  onMount(() => {
    fetchTorrents();
    fetchShares();
    pollInterval = setInterval(fetchTorrents, 4000);
  });
  onDestroy(() => clearInterval(pollInterval));

  $: active  = torrents.filter(t => t.status === 'downloading' || (t.progress < 100 && t.status !== 'paused' && t.status !== 'stopped'));
  $: done    = torrents.filter(t => t.status === 'seeding'     || t.progress >= 100);
  $: stopped = torrents.filter(t => t.status === 'paused'      || t.status === 'stopped');
  $: filtered = activeTab === 'all' ? torrents : activeTab === 'active' ? active : activeTab === 'done' ? done : stopped;

  // ── Add torrent ──
  function openAddModal() {
    showAddModal = true; addMode = 'file'; magnetLink = ''; selectedFile = null;
    savePath = '/data/torrents'; addMsg = ''; addMsgError = false;
  }

  function pickFile() {
    const input = document.createElement('input');
    input.type = 'file'; input.accept = '.torrent';
    input.onchange = (e) => { selectedFile = e.target.files[0] || null; };
    input.click();
  }

  async function doAdd() {
    if (addMode === 'file' && !selectedFile) { addMsg = 'Selecciona un archivo .torrent'; addMsgError = true; return; }
    if (addMode === 'magnet' && !magnetLink.trim()) { addMsg = 'Introduce un magnet link'; addMsgError = true; return; }
    adding = true; addMsg = '';
    try {
      if (addMode === 'file') {
        const fd = new FormData();
        fd.append('torrent', selectedFile);
        fd.append('save_path', savePath);
        const r = await fetch('/api/torrent/upload', { method: 'POST', headers: { 'Authorization': `Bearer ${getToken()}` }, body: fd });
        const d = await r.json();
        if (d.error) { addMsg = d.error; addMsgError = true; adding = false; return; }
      } else {
        const r = await fetch('/api/torrent/add', {
          method: 'POST', headers: { ...hdrs(), 'Content-Type': 'application/json' },
          body: JSON.stringify({ magnet: magnetLink.trim(), save_path: savePath }),
        });
        const d = await r.json();
        if (d.error) { addMsg = d.error; addMsgError = true; adding = false; return; }
      }
      showAddModal = false; fetchTorrents();
    } catch { addMsg = 'Error de conexión'; addMsgError = true; }
    adding = false;
  }

  // ── Actions ──
  async function pauseTorrent(hash)  { await fetch('/api/torrent/pause',  { method:'POST', headers:{ ...hdrs(), 'Content-Type':'application/json' }, body:JSON.stringify({ hash }) }); fetchTorrents(); }
  async function resumeTorrent(hash) { await fetch('/api/torrent/resume', { method:'POST', headers:{ ...hdrs(), 'Content-Type':'application/json' }, body:JSON.stringify({ hash }) }); fetchTorrents(); }

  async function deleteTorrent(hash) {
    await fetch('/api/torrent/remove', { method:'POST', headers:{ ...hdrs(), 'Content-Type':'application/json' }, body:JSON.stringify({ hash, delete_files:deleteWithFiles }) });
    showDeleteConfirm = null; deleteWithFiles = false;
    if ((selectedTorrent?.hash || selectedTorrent?.id) === hash) selectedTorrent = null;
    fetchTorrents();
  }

  async function pauseAll()  { for (const t of active)  await fetch(`/api/torrent/pause/${t.hash||t.id}`,  { method:'POST', headers:hdrs() }); fetchTorrents(); }
  async function resumeAll() { for (const t of stopped) await fetch(`/api/torrent/resume/${t.hash||t.id}`, { method:'POST', headers:hdrs() }); fetchTorrents(); }

  // Formatting
  function fmtSize(bytes) {
    if (!bytes) return '—';
    if (bytes >= 1e12) return (bytes/1e12).toFixed(1) + ' TB';
    if (bytes >= 1e9)  return (bytes/1e9).toFixed(1)  + ' GB';
    if (bytes >= 1e6)  return (bytes/1e6).toFixed(1)  + ' MB';
    return (bytes/1e3).toFixed(0) + ' KB';
  }
  function fmtSpeed(bytes) {
    if (!bytes || bytes < 100) return '';
    if (bytes >= 1e6) return (bytes/1e6).toFixed(1) + ' MB/s';
    return (bytes/1e3).toFixed(0) + ' KB/s';
  }
  function fmtEta(seconds) {
    if (!seconds || seconds <= 0) return '';
    if (seconds >= 86400) return Math.floor(seconds/86400) + 'd ' + Math.floor((seconds%86400)/3600) + 'h';
    if (seconds >= 3600)  return Math.floor(seconds/3600) + 'h ' + Math.floor((seconds%3600)/60) + 'm';
    if (seconds >= 60)    return Math.floor(seconds/60) + 'm ' + (seconds%60) + 's';
    return seconds + 's';
  }

  function isDownloading(t) { return t.status === 'downloading' || (t.progress < 100 && t.status !== 'paused' && t.status !== 'stopped'); }
  function isPaused(t)      { return t.status === 'paused' || t.status === 'stopped'; }
  function isDone(t)        { return t.status === 'seeding' || t.progress >= 100; }
  function selectTorrent(t) { selectedTorrent = (selectedTorrent?.hash === t.hash && selectedTorrent?.id === t.id) ? null : t; }

  $: dlSpeed = torrents.reduce((a, t) => a + (t.dlSpeed || t.downloadSpeed || 0), 0);
  $: ulSpeed = torrents.reduce((a, t) => a + (t.ulSpeed || t.uploadSpeed   || 0), 0);
</script>

<div class="nt-root">

  <!-- SIDEBAR -->
  <div class="sidebar">
    <div class="sb-header">
      <div class="sb-logo-wrap">
        <svg width="14" height="14" viewBox="0 0 14 14" fill="none">
          <path d="M7 1v9M3.5 7l3.5 4 3.5-4" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"/>
        </svg>
        <div class="sb-logo-line"></div>
      </div>
      <span class="sb-title">NimTorrent</span>
    </div>

    <div class="sb-search">⌕ Buscar…</div>

    <div class="sb-section">Vistas</div>
    <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={activeTab === 'all'} on:click={() => activeTab = 'all'}>
      <span class="sb-ico">⊟</span> Panel
      {#if torrents.length > 0}<span class="sb-badge">{torrents.length}</span>{/if}
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={activeTab === 'active'} on:click={() => activeTab = 'active'}>
      <span class="sb-ico">↓</span> Descargas
      {#if active.length > 0}<span class="sb-badge blue">{active.length}</span>{/if}
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={activeTab === 'done'} on:click={() => activeTab = 'done'}>
      <span class="sb-ico">✓</span> Completados
      {#if done.length > 0}<span class="sb-badge green">{done.length}</span>{/if}
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="sb-item" class:active={activeTab === 'stopped'} on:click={() => activeTab = 'stopped'}>
      <span class="sb-ico">⏹</span> Parados
      {#if stopped.length > 0}<span class="sb-badge">{stopped.length}</span>{/if}
    </div>

    <div class="sb-section" style="margin-top:8px">Trackers</div>
    <div class="sb-item"><span class="sb-ico">⬡</span> Todos los trackers</div>

    <!-- Detail panel -->
    {#if selectedTorrent}
      <div class="sb-detail">
        <div class="sb-detail-header">
          <div class="sb-detail-name">{selectedTorrent.name}</div>
          <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="sb-detail-close" on:click={() => selectedTorrent = null}>✕</div>
        </div>
        <div class="sb-detail-row"><span>Estado</span><span class="sb-detail-val">{selectedTorrent.status || '—'}</span></div>
        <div class="sb-detail-row"><span>Progreso</span><span class="sb-detail-val">{(selectedTorrent.progress ?? 0).toFixed(1)}%</span></div>
        <div class="sb-detail-row"><span>Tamaño</span><span class="sb-detail-val">{fmtSize(selectedTorrent.size || selectedTorrent.totalSize)}</span></div>
        <div class="sb-detail-row"><span>Descargado</span><span class="sb-detail-val">{fmtSize(selectedTorrent.downloaded)}</span></div>
        {#if selectedTorrent.save_path || selectedTorrent.savePath}
          <div class="sb-detail-row"><span>Destino</span><span class="sb-detail-val path">{selectedTorrent.save_path || selectedTorrent.savePath}</span></div>
        {/if}
        {#if selectedTorrent.numPeers || selectedTorrent.peers}
          <div class="sb-detail-row"><span>Peers</span><span class="sb-detail-val">{selectedTorrent.numPeers || selectedTorrent.peers || 0}</span></div>
        {/if}
        {#if selectedTorrent.numSeeds || selectedTorrent.seeds}
          <div class="sb-detail-row"><span>Seeds</span><span class="sb-detail-val">{selectedTorrent.numSeeds || selectedTorrent.seeds || 0}</span></div>
        {/if}
        <div class="sb-detail-actions">
          {#if isPaused(selectedTorrent)}
            <button class="btn-sm" on:click={() => resumeTorrent(selectedTorrent.hash || selectedTorrent.id)}>▶ Reanudar</button>
          {:else if !isDone(selectedTorrent)}
            <button class="btn-sm" on:click={() => pauseTorrent(selectedTorrent.hash || selectedTorrent.id)}>⏸ Pausar</button>
          {/if}
          <button class="btn-sm danger" on:click={() => { showDeleteConfirm = selectedTorrent.hash || selectedTorrent.id; deleteWithFiles = false; }}>✕ Eliminar</button>
        </div>
      </div>
    {/if}
  </div>

  <!-- INNER -->
  <div class="inner-wrap">
    <div class="inner">

      <div class="inner-titlebar">
        <div class="tabs">
          <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="tab" class:active-tab={activeTab === 'all'} on:click={() => activeTab = 'all'}>
            <div class="tab-dot"></div> Activos
            {#if active.length > 0}<span class="tab-count">{active.length}</span>{/if}
          </div>
          <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="tab done-tab" class:active={activeTab === 'done'} on:click={() => activeTab = 'done'}>
            <div class="tab-dot"></div> Finalizado
            {#if done.length > 0}<span class="tab-count">{done.length}</span>{/if}
          </div>
          <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="tab stopped-tab" class:active={activeTab === 'stopped'} on:click={() => activeTab = 'stopped'}>
            <div class="tab-dot"></div> Parado
            {#if stopped.length > 0}<span class="tab-count">{stopped.length}</span>{/if}
          </div>
        </div>
        <div class="tb-actions">
          <button class="tb-btn" title="Pausar todo" on:click={pauseAll}>⏸</button>
          <button class="tb-btn" title="Reanudar todo" on:click={resumeAll}>▶</button>
          <button class="btn-accent" on:click={openAddModal}>+ Añadir</button>
        </div>
      </div>

      <div class="torrent-list">
        {#if loading}
          <div class="t-empty"><div class="spinner"></div></div>
        {:else if filtered.length === 0}
          <div class="t-empty">
            <div class="t-empty-icon">⬇</div>
            <div>Sin torrents</div>
            <button class="btn-accent" style="margin-top:10px" on:click={openAddModal}>Añadir torrent</button>
          </div>
        {:else}
          {#each filtered as t (t.hash || t.id)}
            <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="torrent-row" class:selected={(selectedTorrent?.hash||selectedTorrent?.id) === (t.hash||t.id)} on:click={() => selectTorrent(t)}>
              <div class="t-icon">
                {#if isDone(t)}
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="20 6 9 17 4 12"/></svg>
                {:else if isPaused(t)}
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="6" y="4" width="4" height="16"/><rect x="14" y="4" width="4" height="16"/></svg>
                {:else}
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M12 2v10M8 8l4 4 4-4"/><path d="M20 16v2a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-2"/></svg>
                {/if}
              </div>
              <div class="t-main">
                <div class="t-name">{t.name}</div>
                <div class="t-meta">
                  <span>{fmtSize(t.downloaded || 0)} / {fmtSize(t.size || t.totalSize)}</span>
                  {#if isDownloading(t) && fmtSpeed(t.dlSpeed || t.downloadSpeed)}<span class="t-dl">↓ {fmtSpeed(t.dlSpeed || t.downloadSpeed)}</span>{/if}
                  {#if (isDone(t) || isDownloading(t)) && fmtSpeed(t.ulSpeed || t.uploadSpeed)}<span class="t-ul">↑ {fmtSpeed(t.ulSpeed || t.uploadSpeed)}</span>{/if}
                  {#if isDownloading(t) && (t.eta || t.timeRemaining)}<span class="t-eta">ETA {fmtEta(t.eta || t.timeRemaining)}</span>{/if}
                </div>
              </div>
              <div class="t-progress">
                <div class="t-bar-bg"><div class="t-bar-fill" style="width:{t.progress ?? 0}%" class:done={isDone(t)} class:paused={isPaused(t)}></div></div>
                <span class="t-pct">{(t.progress ?? 0).toFixed(0)}%</span>
              </div>
              <div class="t-actions">
                {#if isPaused(t)}
                  <button class="t-action" title="Reanudar" on:click|stopPropagation={() => resumeTorrent(t.hash || t.id)}>▶</button>
                {:else if !isDone(t)}
                  <button class="t-action" title="Pausar" on:click|stopPropagation={() => pauseTorrent(t.hash || t.id)}>⏸</button>
                {/if}
                <button class="t-action danger" title="Eliminar" on:click|stopPropagation={() => { showDeleteConfirm = t.hash || t.id; deleteWithFiles = false; }}>✕</button>
              </div>
            </div>
          {/each}
        {/if}
      </div>

      <div class="statusbar">
        <div class="status-dot"></div>
        <span>{torrents.length} torrents</span>
        <div class="status-sep"></div>
        {#if dlSpeed > 100}<span>↓ {fmtSpeed(dlSpeed)}</span><div class="status-sep"></div>{/if}
        {#if ulSpeed > 100}<span>↑ {fmtSpeed(ulSpeed)}</span><div class="status-sep"></div>{/if}
        <span style="margin-left:auto">{active.length} activos · {done.length} completados</span>
      </div>
    </div>
  </div>
</div>

<!-- ══ ADD TORRENT MODAL ══ -->
{#if showAddModal}
  <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-overlay" on:click|self={() => showAddModal = false}></div>
  <div class="modal">
    <div class="modal-header">
      <div class="modal-title">Añadir torrent</div>
      <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="modal-close" on:click={() => showAddModal = false}>✕</div>
    </div>
    <div class="modal-body">
      <div class="add-tabs">
        <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="add-tab" class:active={addMode === 'file'} on:click={() => addMode = 'file'}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
          Archivo .torrent
        </div>
        <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="add-tab" class:active={addMode === 'magnet'} on:click={() => addMode = 'magnet'}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M6 2v6a6 6 0 0 0 12 0V2"/><path d="M2 2h4M18 2h4M2 8h4M18 8h4"/></svg>
          Magnet link
        </div>
      </div>

      {#if addMode === 'file'}
        <div class="form-field">
          <label class="form-label">Archivo</label>
          <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="file-picker" on:click={pickFile}>
            {#if selectedFile}
              <span class="file-name">{selectedFile.name}</span>
              <span class="file-size">{fmtSize(selectedFile.size)}</span>
            {:else}
              <span class="file-placeholder">Seleccionar archivo .torrent</span>
            {/if}
          </div>
        </div>
      {:else}
        <div class="form-field">
          <label class="form-label">Magnet link</label>
          <input class="form-input" type="text" placeholder="magnet:?xt=urn:btih:..." bind:value={magnetLink} />
        </div>
      {/if}

      <div class="form-field">
        <label class="form-label">Carpeta de destino</label>
        <div class="dest-options">
          <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="dest-option" class:active={savePath === '/data/torrents'} on:click={() => savePath = '/data/torrents'}>
            <div class="dest-icon">📥</div>
            <div class="dest-info"><div class="dest-name">Descargas</div><div class="dest-path">/data/torrents</div></div>
          </div>
          {#each shares as share}
            <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="dest-option" class:active={savePath === (share.path || `/pool/${share.pool}/${share.name}`)}
              on:click={() => savePath = share.path || `/pool/${share.pool}/${share.name}`}>
              <div class="dest-icon">📁</div>
              <div class="dest-info">
                <div class="dest-name">{share.displayName || share.name}</div>
                <div class="dest-path">{share.path || `/pool/${share.pool}/${share.name}`}</div>
              </div>
            </div>
          {/each}
        </div>
        <div class="dest-custom">
          <label class="form-label" style="margin-top:8px">O ruta personalizada</label>
          <input class="form-input" type="text" placeholder="/ruta/personalizada" bind:value={savePath} />
        </div>
      </div>

      {#if addMsg}<div class="add-msg" class:error={addMsgError}>{addMsg}</div>{/if}
    </div>
    <div class="modal-footer">
      <button class="btn-secondary" on:click={() => showAddModal = false}>Cancelar</button>
      <button class="btn-accent" on:click={doAdd} disabled={adding}>{adding ? 'Añadiendo...' : 'Añadir torrent'}</button>
    </div>
  </div>
{/if}

<!-- ══ DELETE CONFIRM ══ -->
{#if showDeleteConfirm}
  <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-overlay" on:click|self={() => showDeleteConfirm = null}></div>
  <div class="modal modal-sm">
    <div class="modal-header">
      <div class="modal-title">Eliminar torrent</div>
      <!-- svelte-ignore a11y_click_events_have_key_events --><!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="modal-close" on:click={() => showDeleteConfirm = null}>✕</div>
    </div>
    <div class="modal-body">
      <p style="font-size:12px;color:var(--text-2);margin:0">¿Eliminar este torrent de la lista?</p>
      <label class="delete-check"><input type="checkbox" bind:checked={deleteWithFiles} /><span>También eliminar archivos descargados</span></label>
    </div>
    <div class="modal-footer">
      <button class="btn-secondary" on:click={() => showDeleteConfirm = null}>Cancelar</button>
      <button class="btn-accent" style="background:var(--red)" on:click={() => deleteTorrent(showDeleteConfirm)}>Eliminar</button>
    </div>
  </div>
{/if}

<style>
  .nt-root { width:100%; height:100%; display:flex; overflow:hidden; font-family:'DM Sans',sans-serif; color:var(--text-1); }

  .sidebar { width:190px; flex-shrink:0; display:flex; flex-direction:column; padding:12px 8px; background:var(--bg-sidebar); overflow-y:auto; }
  .sidebar::-webkit-scrollbar { width:3px; } .sidebar::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }
  .sb-header { display:flex; align-items:center; gap:9px; padding:32px 8px 20px; }
  .sb-logo-wrap { display:flex; flex-direction:column; align-items:center; gap:3px; flex-shrink:0; color:var(--text-1); }
  .sb-logo-line { width:16px; height:3px; border-radius:2px; background:rgba(255,255,255,0.55); }
  .sb-title { font-size:14px; font-weight:700; color:var(--text-1); }
  .sb-search { display:flex; align-items:center; gap:6px; padding:4px 10px; border-radius:8px; margin-bottom:10px; border:1px solid var(--border); background:var(--ibtn-bg); font-size:11px; color:var(--text-3); }
  .sb-section { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.08em; padding:0 10px 4px; margin-top:4px; }
  .sb-item { display:flex; align-items:center; gap:8px; padding:7px 10px; border-radius:8px; cursor:pointer; font-size:12px; color:var(--text-2); border:1px solid transparent; transition:all .15s; }
  .sb-item:hover { background:rgba(128,128,128,0.10); color:var(--text-1); }
  .sb-item.active { background:var(--active-bg); color:var(--text-1); border-color:var(--border-hi); }
  .sb-ico { font-size:12px; width:14px; text-align:center; flex-shrink:0; }
  .sb-badge { margin-left:auto; padding:1px 6px; border-radius:10px; font-size:9px; font-weight:700; font-family:'DM Mono',monospace; background:var(--ibtn-bg); border:1px solid var(--border); color:var(--text-3); }
  .sb-badge.blue  { background:rgba(96,165,250,0.12); border-color:rgba(96,165,250,0.25); color:rgba(96,165,250,0.9); }
  .sb-badge.green { background:rgba(74,222,128,0.10); border-color:rgba(74,222,128,0.22); color:rgba(74,222,128,0.85); }

  .sb-detail { margin-top:auto; padding:10px; border-top:1px solid var(--border); display:flex; flex-direction:column; gap:4px; }
  .sb-detail-header { display:flex; align-items:flex-start; gap:6px; margin-bottom:4px; }
  .sb-detail-name { font-size:11px; font-weight:600; color:var(--text-1); flex:1; word-break:break-all; line-height:1.3; }
  .sb-detail-close { font-size:10px; color:var(--text-3); cursor:pointer; padding:2px; } .sb-detail-close:hover { color:var(--text-1); }
  .sb-detail-row { display:flex; justify-content:space-between; font-size:10px; color:var(--text-3); padding:2px 0; }
  .sb-detail-val { color:var(--text-2); font-family:'DM Mono',monospace; font-size:9px; text-align:right; max-width:100px; }
  .sb-detail-val.path { word-break:break-all; font-size:8px; }
  .sb-detail-actions { display:flex; gap:4px; margin-top:6px; }
  .btn-sm { padding:4px 8px; border-radius:6px; border:1px solid var(--border); background:var(--ibtn-bg); color:var(--text-2); font-size:9px; font-weight:600; cursor:pointer; font-family:inherit; transition:all .15s; }
  .btn-sm:hover { color:var(--text-1); border-color:var(--border-hi); }
  .btn-sm.danger { border-color:rgba(248,113,113,0.25); color:var(--red); } .btn-sm.danger:hover { background:rgba(248,113,113,0.10); }

  .inner-wrap { flex:1; padding:8px; display:flex; }
  .inner { flex:1; border-radius:10px; border:1px solid var(--border); background:var(--bg-inner); display:flex; flex-direction:column; overflow:hidden; }
  .inner-titlebar { display:flex; align-items:center; gap:8px; padding:10px 14px 9px; background:var(--bg-bar); flex-shrink:0; }
  .tabs { display:flex; gap:4px; flex:1; }
  .tab { display:flex; align-items:center; gap:5px; padding:5px 10px; border-radius:6px; cursor:pointer; font-size:11px; font-weight:500; color:var(--text-3); border:1px solid transparent; transition:all .15s; }
  .tab:hover { color:var(--text-2); }
  .tab.active-tab { background:rgba(96,165,250,0.10); border-color:rgba(96,165,250,0.30); color:rgba(96,165,250,0.90); }
  .tab.done-tab.active { background:rgba(74,222,128,0.08); border-color:rgba(74,222,128,0.25); color:rgba(74,222,128,0.85); }
  .tab.stopped-tab.active { background:rgba(148,163,184,0.08); border-color:rgba(148,163,184,0.22); color:rgba(148,163,184,0.75); }
  .tab-dot { width:5px; height:5px; border-radius:50%; background:rgba(128,128,128,0.3); }
  .active-tab .tab-dot { background:rgba(96,165,250,0.90); box-shadow:0 0 4px rgba(96,165,250,0.6); }
  .done-tab.active .tab-dot { background:rgba(74,222,128,0.85); box-shadow:0 0 4px rgba(74,222,128,0.5); }
  .stopped-tab.active .tab-dot { background:rgba(148,163,184,0.70); }
  .tab-count { font-size:9px; font-weight:700; padding:1px 5px; border-radius:8px; background:rgba(255,255,255,0.07); color:var(--text-3); font-family:'DM Mono',monospace; }
  .tb-actions { display:flex; align-items:center; gap:6px; }
  .tb-btn { width:34px; height:34px; border-radius:8px; border:none; background:transparent; color:var(--text-2); cursor:pointer; font-size:15px; display:flex; align-items:center; justify-content:center; transition:all .15s; }
  .tb-btn:hover { color:var(--text-1); background:rgba(128,128,128,0.10); }
  .btn-accent { padding:5px 12px; border-radius:7px; border:none; cursor:pointer; background:linear-gradient(135deg, var(--accent), var(--accent2)); color:#fff; font-size:11px; font-weight:600; font-family:inherit; transition:all .15s; }
  .btn-accent:hover { opacity:.88; } .btn-accent:disabled { opacity:.5; cursor:not-allowed; }

  .torrent-list { flex:1; overflow-y:auto; padding:8px; }
  .torrent-list::-webkit-scrollbar { width:3px; } .torrent-list::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }
  .t-empty { height:100%; display:flex; flex-direction:column; align-items:center; justify-content:center; gap:8px; color:var(--text-3); font-size:12px; }
  .t-empty-icon { font-size:28px; opacity:.3; }

  .torrent-row { display:flex; align-items:center; gap:10px; padding:8px 12px; border-radius:8px; margin-bottom:2px; border:1px solid transparent; transition:all .15s; cursor:pointer; animation:fadeUp .25s ease both; }
  .torrent-row:hover { background:rgba(128,128,128,0.06); border-color:var(--border); }
  .torrent-row.selected { background:var(--active-bg); border-color:var(--border-hi); }
  @keyframes fadeUp { from{opacity:0;transform:translateY(4px)} to{opacity:1;transform:none} }
  .t-icon { width:18px; height:18px; flex-shrink:0; opacity:.5; } .t-icon svg { width:100%; height:100%; }
  .torrent-row:hover .t-icon, .torrent-row.selected .t-icon { opacity:.9; }
  .t-main { flex:1; min-width:0; }
  .t-name { font-size:12px; font-weight:500; color:var(--text-1); white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .t-meta { display:flex; gap:8px; margin-top:2px; }
  .t-meta span { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; }
  .t-dl { color:#4ade80 !important; } .t-ul { color:#60a5fa !important; } .t-eta { opacity:.7; }
  .t-progress { display:flex; align-items:center; gap:6px; flex-shrink:0; width:140px; }
  .t-bar-bg { flex:1; height:3px; background:rgba(128,128,128,0.15); border-radius:2px; overflow:hidden; }
  .t-bar-fill { height:100%; border-radius:2px; background:linear-gradient(90deg, var(--accent), var(--accent2)); transition:width .4s; }
  .t-bar-fill.done { background:linear-gradient(90deg, #4ade80, #22d3ee); } .t-bar-fill.paused { background:rgba(128,128,128,0.3); }
  .t-pct { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; flex-shrink:0; width:28px; text-align:right; }
  .t-actions { display:flex; gap:4px; flex-shrink:0; opacity:0; transition:opacity .15s; }
  .torrent-row:hover .t-actions { opacity:1; }
  .t-action { width:24px; height:24px; border-radius:6px; border:none; background:transparent; color:var(--text-2); cursor:pointer; font-size:11px; display:flex; align-items:center; justify-content:center; transition:all .15s; }
  .t-action:hover { background:rgba(128,128,128,0.10); color:var(--text-1); }
  .t-action.danger:hover { background:rgba(248,113,113,0.12); color:var(--red); }

  .statusbar { display:flex; align-items:center; gap:10px; padding:7px 14px; border-top:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; font-size:10px; color:var(--text-3); border-radius:0 0 10px 10px; font-family:'DM Mono',monospace; }
  .status-dot { width:6px; height:6px; border-radius:50%; background:var(--green); box-shadow:0 0 4px rgba(74,222,128,0.6); flex-shrink:0; }
  .status-sep { width:1px; height:10px; background:var(--border); }
  .spinner { width:24px; height:24px; border-radius:50%; border:2px solid rgba(255,255,255,0.08); border-top-color:var(--accent); animation:spin .7s linear infinite; }
  @keyframes spin { to { transform:rotate(360deg); } }

  /* Modal */
  .modal-overlay { position:fixed; inset:0; z-index:200; background:rgba(0,0,0,0.60); backdrop-filter:blur(3px); }
  .modal { position:fixed; top:50%; left:50%; transform:translate(-50%,-50%); z-index:201; width:480px; max-width:90%; background:var(--bg-inner); border-radius:12px; border:1px solid var(--border); box-shadow:0 24px 60px rgba(0,0,0,0.5); display:flex; flex-direction:column; overflow:hidden; animation:modalIn .2s cubic-bezier(0.16,1,0.3,1) both; }
  .modal.modal-sm { width:360px; }
  @keyframes modalIn { from{opacity:0;transform:translate(-50%,-48%) scale(0.97)} to{opacity:1;transform:translate(-50%,-50%) scale(1)} }
  .modal-header { display:flex; align-items:center; gap:12px; padding:14px 18px; border-bottom:1px solid var(--border); background:var(--bg-bar); }
  .modal-title { font-size:13px; font-weight:600; color:var(--text-1); flex:1; }
  .modal-close { width:24px; height:24px; border-radius:6px; cursor:pointer; display:flex; align-items:center; justify-content:center; color:var(--text-3); font-size:11px; background:var(--ibtn-bg); transition:all .15s; } .modal-close:hover { color:var(--text-1); }
  .modal-body { padding:18px 20px; overflow-y:auto; max-height:440px; display:flex; flex-direction:column; gap:14px; }
  .modal-body::-webkit-scrollbar { width:3px; } .modal-body::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }
  .modal-footer { display:flex; align-items:center; justify-content:flex-end; gap:8px; padding:12px 18px; border-top:1px solid var(--border); background:var(--bg-bar); }

  .add-tabs { display:flex; gap:6px; }
  .add-tab { display:flex; align-items:center; gap:6px; padding:8px 14px; border-radius:8px; cursor:pointer; font-size:11px; font-weight:500; color:var(--text-3); border:1px solid var(--border); background:var(--ibtn-bg); transition:all .15s; }
  .add-tab svg { width:14px; height:14px; } .add-tab:hover { color:var(--text-2); border-color:var(--border-hi); }
  .add-tab.active { color:var(--text-1); border-color:var(--accent); background:rgba(124,111,255,0.06); }
  .form-field { display:flex; flex-direction:column; gap:4px; }
  .form-label { font-size:10px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; }
  .form-input { padding:8px 12px; border-radius:8px; background:rgba(255,255,255,0.04); border:1px solid var(--border); color:var(--text-1); font-size:12px; font-family:'DM Sans',sans-serif; outline:none; transition:border-color .2s; }
  .form-input:focus { border-color:var(--accent); } .form-input::placeholder { color:var(--text-3); }
  .file-picker { display:flex; align-items:center; justify-content:space-between; padding:10px 14px; border-radius:8px; cursor:pointer; border:1px dashed var(--border); background:rgba(255,255,255,0.02); transition:all .15s; }
  .file-picker:hover { border-color:var(--accent); background:rgba(124,111,255,0.04); }
  .file-name { font-size:12px; color:var(--text-1); font-weight:500; } .file-size { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; }
  .file-placeholder { font-size:12px; color:var(--text-3); }
  .dest-options { display:flex; flex-direction:column; gap:4px; }
  .dest-option { display:flex; align-items:center; gap:10px; padding:8px 12px; border-radius:8px; cursor:pointer; border:1px solid var(--border); background:var(--ibtn-bg); transition:all .15s; }
  .dest-option:hover { border-color:var(--border-hi); } .dest-option.active { border-color:var(--accent); background:rgba(124,111,255,0.06); }
  .dest-icon { font-size:18px; flex-shrink:0; }
  .dest-info { min-width:0; } .dest-name { font-size:11px; font-weight:600; color:var(--text-1); } .dest-path { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; }
  .add-msg { font-size:11px; color:var(--green); } .add-msg.error { color:var(--red); }
  .delete-check { display:flex; align-items:center; gap:8px; margin-top:10px; font-size:11px; color:var(--text-2); cursor:pointer; }
  .delete-check input { accent-color:var(--red); }
  .btn-secondary { padding:7px 13px; border-radius:8px; border:1px solid var(--border); background:var(--ibtn-bg); color:var(--text-2); font-size:11px; font-weight:500; cursor:pointer; font-family:inherit; transition:all .15s; }
  .btn-secondary:hover { color:var(--text-1); border-color:var(--border-hi); }
</style>
