<script>
  import { onMount, onDestroy } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  let shares = [];
  let currentShare = null;
  let currentPath = '/';
  let files = [];
  let loading = false;
  let selected = new Set();
  let storageInfo = null;

  // ── Clipboard ──
  let clipboard = null; // { file, share, path, op: 'copy'|'cut' }

  // ── Context menu ──
  let ctxMenu = null; // { x, y, file, idx } | null
  let ctxTarget = null; // el archivo al que se hizo clic derecho

  // ── Modals ──
  let renameModal = null; // { file, newName }
  let infoModal = null;   // file

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}`, 'Content-Type': 'application/json' });

  function filePath(file) {
    return currentPath === '/' ? `/${file.name}` : `${currentPath}/${file.name}`;
  }

  async function fetchShares() {
    try { const r = await fetch('/api/files', { headers: hdrs() }); const d = await r.json(); if (d.shares) shares = d.shares; } catch {}
  }
  async function fetchFiles() {
    if (!currentShare) { files = []; return; }
    loading = true;
    try { const r = await fetch(`/api/files?share=${currentShare}&path=${encodeURIComponent(currentPath)}`, { headers: hdrs() }); const d = await r.json(); files = d.files || []; } catch { files = []; }
    selected = new Set(); loading = false;
  }
  async function fetchStorage() {
    try { const r = await fetch('/api/storage/status', { headers: hdrs() }); const d = await r.json(); if (d.pools?.length) storageInfo = d.pools[0]; } catch {}
  }

  let gridEl;

  onMount(() => {
    fetchShares();
    fetchStorage();

    const handleCtx = (e) => {
      if (!gridEl || !gridEl.contains(e.target)) return;
      const item = e.target.closest('.f-item');
      e.preventDefault();
      if (!item) {
        if (clipboard && currentShare) {
          const p = calcMenuPos(e); ctxMenu = { x: p.x, y: p.y, file: null, idx: -1 };
        }
        return;
      }
      const idx = parseInt(item.dataset.idx);
      const file = sorted[idx];
      if (!file) return;
      if (!selected.has(idx)) selected = new Set([idx]);
      const p = calcMenuPos(e); ctxMenu = { x: p.x, y: p.y, file, idx };
    };

    const handleMouseDown = (e) => {
      if (e.button === 2) return;
      if (!e.target.closest('.ctx-menu')) closeCtx();
    };

    gridEl.addEventListener('contextmenu', handleCtx);
    document.addEventListener('mousedown', handleMouseDown);

    return () => {
      gridEl.removeEventListener('contextmenu', handleCtx);
      document.removeEventListener('mousedown', handleMouseDown);
    };
  });
  $: if (currentShare !== undefined || currentPath) fetchFiles();

  function navigate(share, path) { currentShare = share; currentPath = path; closeCtx(); }
  function goBack() {
    if (currentPath !== '/') currentPath = currentPath.split('/').slice(0, -1).join('/') || '/';
    else if (currentShare) { currentShare = null; currentPath = '/'; }
    closeCtx();
  }
  function openItem(file) {
    closeCtx();
    if (file.isDirectory) { currentPath = currentPath === '/' ? `/${file.name}` : `${currentPath}/${file.name}`; return; }
    const fp = filePath(file);
    window.open(`/api/files/download?share=${currentShare}&path=${encodeURIComponent(fp)}&token=${getToken()}`, '_blank');
  }
  function toggleSelect(i, e) {
    if (e?.ctrlKey || e?.metaKey) { const n = new Set(selected); n.has(i) ? n.delete(i) : n.add(i); selected = n; }
    else selected = new Set([i]);
  }
  function uploadFiles() {
    const input = document.createElement('input'); input.type = 'file'; input.multiple = true;
    input.onchange = async (e) => {
      for (const f of e.target.files) {
        const fd = new FormData(); fd.append('file', f); fd.append('share', currentShare); fd.append('path', currentPath);
        await fetch('/api/files/upload', { method: 'POST', headers: { 'Authorization': `Bearer ${getToken()}` }, body: fd });
      }
      fetchFiles();
    };
    input.click();
  }

  // ── Context menu ──
  function onContextMenu(e, file, idx) {
    e.preventDefault();
    e.stopPropagation();
    ctxTarget = file;
    // Seleccionar el archivo si no está seleccionado
    if (!selected.has(idx)) selected = new Set([idx]);
    const p = calcMenuPos(e); ctxMenu = { x: p.x, y: p.y, file, idx };
  }

  function onGridContextMenu(e) {
    // Click derecho en fondo vacío — solo mostrar pegar si hay clipboard
    if (e.target.closest('.f-item')) return;
    e.preventDefault();
    if (!clipboard || !currentShare) return;
    ctxTarget = null;
    const p = calcMenuPos(e); ctxMenu = { x: p.x, y: p.y, file: null, idx: -1 };
  }

  function getZoom() { return parseFloat(document.documentElement.style.zoom) || 1; }

  function calcMenuPos(e, menuW = 200, menuH = 290) {
    const z = getZoom();
    const root = document.querySelector('.files-root');
    const rect = root ? root.getBoundingClientRect() : { left: 0, top: 0, width: window.innerWidth, height: window.innerHeight };
    // Coordinates relative to the container (since ctx-menu is position:absolute inside files-root)
    const x = (e.clientX - rect.left) / z;
    const y = (e.clientY - rect.top) / z;
    const maxX = rect.width / z - menuW - 8;
    const maxY = rect.height / z - menuH - 8;
    return {
      x: Math.max(0, Math.min(x, maxX)),
      y: Math.max(0, Math.min(y, maxY)),
    };
  }

  function closeCtx() { ctxMenu = null; ctxTarget = null; }

  function onWindowClick(e) {
    if (e.button === 2) return; // ignorar click derecho
    if (ctxMenu && !e.target.closest('.ctx-menu')) closeCtx();
    if (renameModal && !e.target.closest('.modal')) return;
  }

  // ── Acciones ──
  async function deleteFile(file) {
    closeCtx();
    if (!confirm(`¿Eliminar "${file.name}"? Esta acción no se puede deshacer.`)) return;
    const fp = filePath(file);
    const res = await fetch('/api/files/delete', {
      method: 'POST', headers: hdrs(),
      body: JSON.stringify({ share: currentShare, path: fp })
    });
    const d = await res.json();
    if (d.ok) fetchFiles();
    else alert(d.error || 'Error al eliminar');
  }

  function copyFile(file) {
    clipboard = { file, share: currentShare, path: filePath(file), op: 'copy' };
    closeCtx();
  }

  function cutFile(file) {
    clipboard = { file, share: currentShare, path: filePath(file), op: 'cut' };
    closeCtx();
  }

  async function pasteFile() {
    if (!clipboard || !currentShare) return;
    closeCtx();
    const destPath = currentPath === '/'
      ? `/${clipboard.file.name}`
      : `${currentPath}/${clipboard.file.name}`;
    const res = await fetch('/api/files/paste', {
      method: 'POST', headers: hdrs(),
      body: JSON.stringify({
        srcShare: clipboard.share,
        srcPath: clipboard.path,
        destShare: currentShare,
        destPath,
        action: clipboard.op
      })
    });
    const d = await res.json();
    if (d.ok) {
      if (clipboard.op === 'cut') clipboard = null;
      fetchFiles();
    } else alert(d.error || 'Error al pegar');
  }

  function startRename(file) {
    closeCtx();
    renameModal = { file, newName: file.name };
    // Focus el input en el siguiente tick
    setTimeout(() => document.getElementById('rename-input')?.select(), 50);
  }

  async function confirmRename() {
    if (!renameModal || !renameModal.newName.trim() || renameModal.newName === renameModal.file.name) {
      renameModal = null; return;
    }
    const oldPath = filePath(renameModal.file);
    const newPath = currentPath === '/'
      ? `/${renameModal.newName.trim()}`
      : `${currentPath}/${renameModal.newName.trim()}`;
    const res = await fetch('/api/files/rename', {
      method: 'POST', headers: hdrs(),
      body: JSON.stringify({ share: currentShare, oldPath, newPath })
    });
    const d = await res.json();
    renameModal = null;
    if (d.ok) fetchFiles();
    else alert(d.error || 'Error al renombrar');
  }

  function showInfo(file) {
    closeCtx();
    infoModal = file;
  }

  function fmtSize(b) {
    if (!b) return '—';
    if (b >= 1e9) return (b/1e9).toFixed(2) + ' GB';
    if (b >= 1e6) return (b/1e6).toFixed(2) + ' MB';
    if (b >= 1e3) return (b/1e3).toFixed(0) + ' KB';
    return b + ' B';
  }

  $: sorted = [...files].sort((a,b) => (a.isDirectory?-1:1) - (b.isDirectory?-1:1) || a.name.localeCompare(b.name));
  $: shareInfo = shares.find(s => s.name === currentShare);
  $: pathParts = currentPath === '/' ? [] : currentPath.split('/').filter(Boolean);

  function fIcon(file) {
    if (file.isDirectory) return '📁';
    const e = file.name.split('.').pop().toLowerCase();
    return {mp4:'🎬',mkv:'🎬',avi:'🎬',mov:'🎬',mp3:'🎵',wav:'🎵',flac:'🎵',jpg:'🖼️',jpeg:'🖼️',png:'🖼️',gif:'🖼️',svg:'🎨',pdf:'📕',doc:'📝',zip:'📦',rar:'📦',js:'💻',py:'💻',go:'💻',txt:'📄',md:'📄',json:'📄',html:'📄',css:'🅰',iso:'💿',sh:'🔧'}[e] || '📄';
  }
  function fDate(iso) {
    if (!iso) return '—';
    const d = new Date(iso);
    return `${String(d.getDate()).padStart(2,'0')}/${String(d.getMonth()+1).padStart(2,'0')}/${d.getFullYear()} ${String(d.getHours()).padStart(2,'0')}:${String(d.getMinutes()).padStart(2,'0')}`;
  }
  function fExt(name) {
    const p = name.lastIndexOf('.');
    return p >= 0 ? name.slice(p+1).toUpperCase() : '—';
  }
</script>

<svelte:window on:keydown={(e) => {
  if (e.key === 'Escape') { closeCtx(); renameModal = null; infoModal = null; }
  if (e.key === 'Enter' && renameModal) confirmRename();
}} />

<div class="files-root">
  <!-- SIDEBAR -->
  <div class="sidebar">
    <div class="sb-header">
      <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
      <span class="title">Files</span>
    </div>

    <div class="sb-section">Carpetas</div>
    {#each shares as share}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="sb-item" class:active={currentShare === share.name} on:click={() => navigate(share.name, '/')}>
        <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
        {share.displayName || share.name}
      </div>
    {/each}

    {#if storageInfo}
      <div class="sb-storage">
        <div class="ss-labels"><span>{storageInfo.name}</span><strong>{storageInfo.totalFormatted}</strong></div>
        <div class="ss-bar"><div class="ss-fill" style="width:{storageInfo.usagePercent}%"></div></div>
      </div>
    {/if}
  </div>

  <!-- INNER WRAP -->
  <div class="inner-wrap">
    <div class="inner">
      <!-- TITLEBAR -->
      <div class="inner-titlebar">
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <button class="nav-btn" on:click={goBack}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="15 18 9 12 15 6"/></svg>
        </button>
        <span class="inner-title">
          {#if !currentShare}Shared Folders{:else}{shareInfo?.displayName || currentShare} <small>{sorted.length} items</small>{/if}
        </span>
        <div class="tb-right">
          {#if clipboard}
            <div class="clipboard-badge" class:cut={clipboard.op === 'cut'}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="width:10px;height:10px"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
              {clipboard.op === 'cut' ? 'Cortado' : 'Copiado'}: {clipboard.file.name}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <span class="cb-clear" on:click={() => clipboard = null}>✕</span>
            </div>
          {/if}
          {#if currentShare}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <button class="btn-import" on:click={uploadFiles}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="width:11px;height:11px"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
              Subir
            </button>
          {/if}
        </div>
      </div>

      <!-- FILE GRID -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="file-grid" bind:this={gridEl}>
        {#if !currentShare}
          {#each shares as share}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="f-item" on:dblclick={() => navigate(share.name, '/')}>
              <div class="f-icon">📁</div>
              <div class="f-name">{share.displayName || share.name}</div>
            </div>
          {/each}
        {:else if loading}
          <div class="f-loading"><div class="spinner"></div></div>
        {:else}
          {#each sorted as file, i}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div
              class="f-item"
              class:sel={selected.has(i)}
              class:cut={clipboard?.op === 'cut' && clipboard?.path === filePath(file)}
              data-idx={i}
              on:click={(e) => toggleSelect(i, e)}
              on:dblclick={() => openItem(file)}
            >
              <div class="f-icon">{fIcon(file)}</div>
              <div class="f-name">{file.name}</div>
              <div class="f-date">{fDate(file.modified)}</div>
            </div>
          {/each}
          {#if sorted.length === 0}
            <div class="f-empty">Carpeta vacía</div>
          {/if}
        {/if}
      </div>

      <!-- STATUSBAR -->
      <div class="statusbar">
        <div class="path">
          {#if currentShare}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <span class="path-part" on:click={() => navigate(currentShare, '/')}>{shareInfo?.displayName || currentShare}</span>
            {#each pathParts as part, i}
              <span class="psep">/</span>
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <span class="path-part" on:click={() => { currentPath = '/' + pathParts.slice(0,i+1).join('/'); fetchFiles(); }}>
                {part}
              </span>
            {/each}
          {:else}
            <span>NimOS Storage</span>
          {/if}
        </div>
        {#if selected.size > 0}
          <div class="sel-count"><span>{selected.size} seleccionado{selected.size !== 1 ? 's' : ''}</span></div>
        {/if}
      </div>
    </div>
  </div>

  <!-- ══ CONTEXT MENU ══ -->
  {#if ctxMenu}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="ctx-menu" style="left:{ctxMenu.x}px;top:{ctxMenu.y}px"
      on:contextmenu|preventDefault>

    {#if ctxMenu.file}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="ctx-item" on:click={() => openItem(ctxMenu.file)}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polygon points="5 3 19 12 5 21 5 3"/></svg>
        Abrir
      </div>
      <div class="ctx-sep"></div>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="ctx-item" on:click={() => copyFile(ctxMenu.file)}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
        Copiar
      </div>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="ctx-item" on:click={() => cutFile(ctxMenu.file)}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="6" cy="20" r="2"/><circle cx="6" cy="4" r="2"/><line x1="6" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="21" y2="21"/><line x1="6" y1="18" x2="21" y2="3"/></svg>
        Cortar
      </div>
      {#if clipboard}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="ctx-item" on:click={pasteFile}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"/><rect x="8" y="2" width="8" height="4" rx="1"/></svg>
          Pegar
        </div>
      {/if}
      <div class="ctx-sep"></div>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="ctx-item" on:click={() => startRename(ctxMenu.file)}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4z"/></svg>
        Renombrar
      </div>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="ctx-item" on:click={() => showInfo(ctxMenu.file)}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>
        Información
      </div>
      <div class="ctx-sep"></div>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="ctx-item danger" on:click={() => deleteFile(ctxMenu.file)}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14H6L5 6"/><path d="M10 11v6M14 11v6"/><path d="M9 6V4h6v2"/></svg>
        Eliminar
      </div>

    {:else}
      <!-- Solo pegar (click derecho en fondo) -->
      {#if clipboard}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="ctx-item" on:click={pasteFile}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M16 4h2a2 2 0 0 1 2 2v14a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2V6a2 2 0 0 1 2-2h2"/><rect x="8" y="2" width="8" height="4" rx="1"/></svg>
          Pegar "{clipboard.file.name}"
        </div>
      {/if}
    {/if}
  </div>
{/if}

<!-- ══ MODAL RENOMBRAR ══ -->
{#if renameModal}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-overlay" on:click|self={() => renameModal = null}></div>
  <div class="modal">
    <div class="modal-header">
      <div class="modal-title">Renombrar</div>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="modal-close" on:click={() => renameModal = null}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
      </div>
    </div>
    <div class="modal-body">
      <div class="form-field">
        <label class="form-label">Nuevo nombre</label>
        <input id="rename-input" class="form-input" type="text" bind:value={renameModal.newName} autofocus />
      </div>
    </div>
    <div class="modal-footer">
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <button class="btn-secondary" on:click={() => renameModal = null}>Cancelar</button>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <button class="btn-accent" on:click={confirmRename}>Renombrar</button>
    </div>
  </div>
{/if}

<!-- ══ MODAL INFO ══ -->
{#if infoModal}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-overlay" on:click|self={() => infoModal = null}></div>
  <div class="modal">
    <div class="modal-header">
      <div class="modal-title">Información</div>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="modal-close" on:click={() => infoModal = null}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
      </div>
    </div>
    <div class="modal-body">
      <div class="info-icon">{fIcon(infoModal)}</div>
      <div class="info-rows">
        <div class="info-row"><span>Nombre</span><span>{infoModal.name}</span></div>
        <div class="info-row"><span>Tipo</span><span>{infoModal.isDirectory ? 'Carpeta' : fExt(infoModal.name)}</span></div>
        {#if !infoModal.isDirectory}
          <div class="info-row"><span>Tamaño</span><span>{fmtSize(infoModal.size)}</span></div>
        {/if}
        <div class="info-row"><span>Modificado</span><span>{fDate(infoModal.modified)}</span></div>
        <div class="info-row"><span>Ruta</span><span>{currentShare}{filePath(infoModal)}</span></div>
      </div>
    </div>
    <div class="modal-footer">
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <button class="btn-accent" on:click={() => infoModal = null}>Cerrar</button>
    </div>
  </div>
{/if}
</div>

<style>
  .files-root { width:100%; height:100%; display:flex; overflow:hidden; position:relative; background:var(--bg-frame); font-family:'Inter',-apple-system,sans-serif; color:var(--text-1); }

  /* Sidebar */
  .sidebar { width:190px; flex-shrink:0; display:flex; flex-direction:column; gap:2px; padding:12px 8px; overflow-y:auto; background:var(--bg-sidebar); }
  .sidebar::-webkit-scrollbar { width:3px; }
  .sidebar::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.2); border-radius:2px; }
  .sb-header { display:flex; align-items:center; gap:8px; padding:32px 8px 12px; color:var(--text-1); }
  .title { font-size:15px; font-weight:600; }
  .sb-section { font-size:9px; font-weight:700; letter-spacing:.1em; text-transform:uppercase; color:var(--text-3); padding:10px 8px 3px; }
  .sb-item { display:flex; align-items:center; gap:8px; padding:6px 10px; border-radius:8px; cursor:pointer; font-size:12px; color:var(--text-2); border:1px solid transparent; transition:all .15s; }
  .sb-item svg { flex-shrink:0; opacity:.6; }
  .sb-item:hover { background:rgba(128,128,128,0.10); color:var(--text-1); }
  .sb-item.active { background:var(--active-bg); color:var(--text-1); border-color:var(--border-hi); }
  .sb-item.active svg { opacity:1; }
  .sb-storage { margin-top:auto; padding:9px 10px; background:var(--storage-bg); border:1px solid var(--border); border-radius:9px; }
  .ss-labels { display:flex; justify-content:space-between; font-size:10px; color:var(--text-2); margin-bottom:6px; }
  .ss-labels strong { color:var(--text-1); font-weight:500; }
  .ss-bar { height:3px; background:rgba(128,128,128,0.15); border-radius:2px; overflow:hidden; }
  .ss-fill { height:100%; background:linear-gradient(90deg,var(--accent),var(--accent2)); }

  /* Inner */
  .inner-wrap { flex:1; padding:8px; display:flex; }
  .inner { flex:1; border-radius:10px; border:1px solid var(--border); background:var(--bg-inner); display:flex; flex-direction:column; overflow:hidden; }

  /* Titlebar */
  .inner-titlebar { display:flex; align-items:center; gap:8px; padding:10px 14px 9px; background:var(--bg-bar); flex-shrink:0; border-bottom:1px solid var(--border); }
  .nav-btn { background:none; border:none; cursor:pointer; color:var(--text-2); padding:4px; border-radius:6px; line-height:1; transition:all .15s; display:flex; align-items:center; }
  .nav-btn svg { width:16px; height:16px; }
  .nav-btn:hover { background:rgba(128,128,128,0.10); color:var(--text-1); }
  .inner-title { font-size:12px; font-weight:600; color:var(--text-1); }
  .inner-title small { color:var(--text-3); font-weight:400; font-size:11px; margin-left:4px; }
  .tb-right { margin-left:auto; display:flex; align-items:center; gap:8px; }
  .clipboard-badge { display:flex; align-items:center; gap:5px; padding:3px 8px 3px 6px; border-radius:5px; font-size:10px; color:var(--text-2); background:var(--ibtn-bg); border:1px solid var(--border); max-width:180px; overflow:hidden; white-space:nowrap; text-overflow:ellipsis; }
  .clipboard-badge.cut { color:var(--amber); border-color:rgba(251,191,36,0.25); background:rgba(251,191,36,0.06); }
  .cb-clear { cursor:pointer; color:var(--text-3); font-size:10px; margin-left:2px; flex-shrink:0; }
  .cb-clear:hover { color:var(--text-1); }
  .btn-import { display:flex; align-items:center; gap:5px; padding:5px 12px; background:linear-gradient(135deg,var(--accent),var(--accent2)); border:none; border-radius:6px; color:#fff; font-family:inherit; font-size:11px; font-weight:600; cursor:pointer; transition:opacity .15s; }
  .btn-import:hover { opacity:.88; }

  /* File grid */
  .file-grid { flex:1; overflow-y:auto; padding:14px 12px; display:grid; grid-template-columns:repeat(auto-fill,minmax(90px,1fr)); gap:3px; align-content:start; }
  .file-grid::-webkit-scrollbar { width:3px; }
  .file-grid::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }
  .f-item { display:flex; flex-direction:column; align-items:center; gap:6px; padding:11px 6px 8px; border-radius:9px; cursor:pointer; border:1px solid transparent; transition:all .15s; animation:fadeUp .35s ease both; }
  .f-item:hover { background:rgba(128,128,128,0.08); border-color:var(--border); }
  .f-item.sel { background:var(--active-bg); border-color:var(--border-hi); }
  .f-item.cut { opacity:.45; }
  @keyframes fadeUp { from{opacity:0;transform:translateY(7px)} to{opacity:1;transform:translateY(0)} }
  .f-icon { font-size:36px; line-height:1; filter:drop-shadow(0 2px 6px rgba(0,0,0,0.4)); transition:transform .15s; }
  .f-item:hover .f-icon { transform:scale(1.07) translateY(-2px); }
  .f-name { font-size:10px; color:var(--text-1); text-align:center; line-height:1.3; max-width:80px; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .f-date { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; }
  .f-empty { grid-column:1/-1; text-align:center; padding:40px; color:var(--text-3); font-size:13px; }
  .f-loading { grid-column:1/-1; display:flex; justify-content:center; padding:40px; }
  .spinner { width:20px; height:20px; border-radius:50%; border:2px solid rgba(255,255,255,0.08); border-top-color:var(--accent); animation:spin .7s linear infinite; }
  @keyframes spin { to{transform:rotate(360deg)} }

  /* Statusbar */
  .statusbar { display:flex; align-items:center; gap:8px; padding:9px 14px; border-top:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; font-size:10px; color:var(--text-3); border-radius:0 0 10px 10px; }
  .path { display:flex; align-items:center; gap:4px; font-family:'DM Mono',monospace; }
  .path-part { color:var(--text-2); cursor:pointer; transition:color .1s; }
  .path-part:hover { color:var(--text-1); }
  .psep { color:var(--text-3); margin:0 1px; }
  .sel-count { margin-left:auto; color:var(--accent); font-weight:500; }

  /* ── Context menu ── */
  .ctx-menu {
    position:absolute; z-index:500;
    background:var(--bg-bar);
    border:1px solid var(--border);
    border-radius:9px;
    padding:4px;
    min-width:180px;
    box-shadow:0 8px 32px rgba(0,0,0,0.5), 0 0 0 1px rgba(255,255,255,0.04);
    animation:ctxIn .12s ease both;
  }
  @keyframes ctxIn { from{opacity:0;transform:scale(0.96) translateY(-4px)} to{opacity:1;transform:scale(1) translateY(0)} }
  .ctx-item {
    display:flex; align-items:center; gap:9px;
    padding:7px 10px; border-radius:6px;
    font-size:12px; color:var(--text-2);
    cursor:pointer; transition:all .1s;
  }
  .ctx-item svg { width:13px; height:13px; flex-shrink:0; opacity:.7; }
  .ctx-item:hover { background:var(--active-bg); color:var(--text-1); }
  .ctx-item:hover svg { opacity:1; }
  .ctx-item.danger { color:var(--red); }
  .ctx-item.danger svg { color:var(--red); opacity:.8; }
  .ctx-item.danger:hover { background:rgba(248,113,113,0.10); color:var(--red); }
  .ctx-sep { height:1px; background:var(--border); margin:3px 4px; }

  /* ── Modals ── */
  .modal-overlay { position:fixed; inset:0; z-index:200; background:rgba(0,0,0,0.60); backdrop-filter:blur(3px); }
  .modal { position:fixed; top:50%; left:50%; transform:translate(-50%,-50%); z-index:201; width:420px; max-width:92%; background:var(--bg-inner); border-radius:12px; border:1px solid var(--border); box-shadow:0 24px 60px rgba(0,0,0,0.5); display:flex; flex-direction:column; overflow:hidden; animation:modalIn .2s cubic-bezier(0.16,1,0.3,1) both; }
  @keyframes modalIn { from{opacity:0;transform:translate(-50%,-48%) scale(0.97)} to{opacity:1;transform:translate(-50%,-50%) scale(1)} }
  .modal-header { display:flex; align-items:center; justify-content:space-between; padding:14px 18px; border-bottom:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; }
  .modal-title { font-size:13px; font-weight:600; color:var(--text-1); }
  .modal-close { width:24px; height:24px; border-radius:6px; cursor:pointer; display:flex; align-items:center; justify-content:center; color:var(--text-3); background:var(--ibtn-bg); transition:all .15s; }
  .modal-close svg { width:12px; height:12px; }
  .modal-close:hover { color:var(--text-1); }
  .modal-body { padding:18px 20px; display:flex; flex-direction:column; gap:14px; }
  .modal-footer { display:flex; align-items:center; justify-content:flex-end; gap:8px; padding:12px 18px; border-top:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; }

  /* Info modal */
  .info-icon { font-size:48px; text-align:center; line-height:1; margin-bottom:4px; }
  .info-rows { display:flex; flex-direction:column; }
  .info-row { display:flex; justify-content:space-between; align-items:center; padding:8px 0; border-bottom:1px solid var(--border); font-size:11px; gap:12px; }
  .info-row:last-child { border-bottom:none; }
  .info-row span:first-child { color:var(--text-3); flex-shrink:0; }
  .info-row span:last-child { color:var(--text-1); font-family:'DM Mono',monospace; font-size:10px; text-align:right; word-break:break-all; }

  /* Form */
  .form-field { display:flex; flex-direction:column; gap:4px; }
  .form-label { font-size:10px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; }
  .form-input { padding:9px 12px; border-radius:8px; background:rgba(255,255,255,0.04); border:1px solid var(--border); color:var(--text-1); font-size:12px; font-family:'Inter',sans-serif; outline:none; transition:border-color .2s; }
  .form-input:focus { border-color:var(--accent); }

  /* Buttons */
  .btn-accent { display:inline-flex; align-items:center; gap:6px; padding:7px 14px; border-radius:8px; border:none; background:linear-gradient(135deg,var(--accent),var(--accent2)); color:#fff; font-size:11px; font-weight:600; cursor:pointer; font-family:inherit; transition:opacity .15s; }
  .btn-accent:hover { opacity:.88; }
  .btn-secondary { padding:7px 14px; border-radius:8px; border:1px solid var(--border); background:var(--ibtn-bg); color:var(--text-2); font-size:11px; font-weight:500; cursor:pointer; font-family:inherit; transition:all .15s; }
  .btn-secondary:hover { color:var(--text-1); border-color:var(--border-hi); }
</style>
