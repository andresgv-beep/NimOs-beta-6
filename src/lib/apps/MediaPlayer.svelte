<script>
  import { onMount, onDestroy } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });
  const token = getToken();

  // ── Player state ──
  let playerEl;
  let isVideo = false;
  let playing = false;
  let currentFile = null;
  let currentSrc = '';
  let duration = 0;
  let currentTime = 0;
  let volume = 0.8;
  let muted = false;
  let playlist = [];
  let playlistIdx = -1;
  let controlsVisible = true;
  let hideTimer = null;

  // ── Modal explorador ──
  let showModal = false;
  let modalShares = [];
  let modalShare = '';
  let modalPath = '/';
  let modalFiles = [];
  let modalLoading = false;

  const AUDIO_EXT = ['mp3','wav','flac','aac','m4a','ogg','opus','wma'];
  const VIDEO_EXT = ['mp4','webm','mkv','avi','mov','ogv'];
  const MEDIA_EXT = [...AUDIO_EXT, ...VIDEO_EXT];

  function getExt(n) { const d = n.lastIndexOf('.'); return d >= 0 ? n.slice(d+1).toLowerCase() : ''; }
  function isMedia(n)     { return MEDIA_EXT.includes(getExt(n)); }
  function isVideoFile(n) { return VIDEO_EXT.includes(getExt(n)); }
  function streamUrl(share, path) {
    return `/api/files/download?share=${encodeURIComponent(share)}&path=${encodeURIComponent(path)}&token=${encodeURIComponent(token)}`;
  }
  function fmtTime(s) {
    if (!s || isNaN(s)) return '0:00';
    return `${Math.floor(s/60)}:${Math.floor(s%60).toString().padStart(2,'0')}`;
  }
  function fmtSize(b) {
    if (!b) return '';
    if (b >= 1e9) return (b/1e9).toFixed(1)+' GB';
    if (b >= 1e6) return (b/1e6).toFixed(1)+' MB';
    return (b/1e3).toFixed(0)+' KB';
  }

  // ── Modal ──
  async function openModal() {
    showModal = true;
    modalPath = '/';
    modalFiles = [];
    modalLoading = true;
    try {
      const res = await fetch('/api/files', { headers: hdrs() });
      const data = await res.json();
      modalShares = data.shares || [];
      if (modalShares.length > 0 && !modalShare) {
        modalShare = modalShares[0].name;
        await loadModalFiles();
      }
    } catch {}
    modalLoading = false;
  }

  async function loadModalFiles() {
    if (!modalShare) return;
    modalLoading = true;
    try {
      const res = await fetch(`/api/files?share=${encodeURIComponent(modalShare)}&path=${encodeURIComponent(modalPath)}`, { headers: hdrs() });
      const data = await res.json();
      modalFiles = data.files || [];
    } catch {}
    modalLoading = false;
  }

  function modalEnterFolder(name) {
    modalPath = modalPath === '/' ? '/' + name : modalPath + '/' + name;
    loadModalFiles();
  }

  function modalGoUp() {
    if (modalPath === '/') return;
    const parts = modalPath.split('/').filter(Boolean); parts.pop();
    modalPath = parts.length ? '/' + parts.join('/') : '/';
    loadModalFiles();
  }

  function modalSelectShare(name) { modalShare = name; modalPath = '/'; loadModalFiles(); }

  function addToPlaylist(file) {
    const path = modalPath === '/' ? '/' + file.name : modalPath + '/' + file.name;
    const entry = { ...file, _share: modalShare, _path: path };
    if (!playlist.find(p => p._share === entry._share && p._path === entry._path)) {
      playlist = [...playlist, entry];
    }
    if (!currentFile) playItem(entry);
  }

  function addAllMedia() {
    const mediaFiles = modalFiles.filter(f => !f.isDirectory && isMedia(f.name));
    for (const f of mediaFiles) addToPlaylist(f);
  }

  // ── Reproducción ──
  function playItem(item) {
    currentFile = item;
    currentSrc = streamUrl(item._share, item._path);
    isVideo = isVideoFile(item.name);
    playing = true;
    playlistIdx = playlist.findIndex(p => p._share === item._share && p._path === item._path);
    if (playerEl) { playerEl.src = currentSrc; playerEl.load(); playerEl.play().catch(() => {}); }
    scheduleHide();
  }

  function playNext() {
    if (!playlist.length) return;
    playlistIdx = (playlistIdx + 1) % playlist.length;
    playItem(playlist[playlistIdx]);
  }

  function playPrev() {
    if (!playlist.length) return;
    if (currentTime > 3) { if (playerEl) playerEl.currentTime = 0; return; }
    playlistIdx = (playlistIdx - 1 + playlist.length) % playlist.length;
    playItem(playlist[playlistIdx]);
  }

  function removeFromPlaylist(i) {
    playlist = playlist.filter((_, idx) => idx !== i);
    if (playlistIdx >= playlist.length) playlistIdx = playlist.length - 1;
  }

  function togglePlay() {
    if (!playerEl) return;
    if (playerEl.paused) { playerEl.play().catch(() => {}); scheduleHide(); }
    else { playerEl.pause(); showControls(); }
  }

  function seek(e) {
    if (!playerEl || !duration) return;
    const rect = e.currentTarget.getBoundingClientRect();
    playerEl.currentTime = ((e.clientX - rect.left) / rect.width) * duration;
  }

  function setVol(e) {
    const rect = e.currentTarget.getBoundingClientRect();
    volume = Math.max(0, Math.min(1, (e.clientX - rect.left) / rect.width));
    if (playerEl) playerEl.volume = volume;
    muted = volume === 0;
  }

  function toggleMute() { muted = !muted; if (playerEl) playerEl.muted = muted; }

  function toggleFullscreen() {
    const el = document.querySelector('.mp-inner');
    if (!el) return;
    if (!document.fullscreenElement) el.requestFullscreen().catch(() => {});
    else document.exitFullscreen();
  }

  function showControls() { clearTimeout(hideTimer); controlsVisible = true; }
  function scheduleHide() {
    clearTimeout(hideTimer); controlsVisible = true;
    hideTimer = setTimeout(() => { if (playing) controlsVisible = false; }, 3000);
  }
  function onMouseMove() { if (playing) scheduleHide(); else showControls(); }

  $: modalBreadcrumbs = modalPath === '/' ? [] : modalPath.split('/').filter(Boolean);

  onDestroy(() => clearTimeout(hideTimer));
</script>

<div class="mp-root">

  <!-- ── SIDEBAR ── -->
  <div class="mp-sidebar">
    <div class="mp-sidebar-header">
      <svg width="15" height="15" viewBox="0 0 24 24" fill="currentColor"><polygon points="5 3 19 12 5 21 5 3"/></svg>
      <span class="mp-title">Media</span>
    </div>

    <div class="mp-section-header">
      <span class="mp-section-label">Cola</span>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <button class="mp-add-btn" title="Añadir archivos" on:click={openModal}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
      </button>
    </div>

    <div class="mp-playlist">
      {#each playlist as item, i}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="mp-pl-item" class:active={i === playlistIdx} on:click={() => playItem(item)}>
          {#if isVideoFile(item.name)}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="2" y="2" width="20" height="20" rx="2"/><polygon points="10 8 16 12 10 16 10 8"/></svg>
          {:else}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg>
          {/if}
          <span class="mp-pl-name">{item.name}</span>
          {#if i === playlistIdx}
            <div class="mp-pl-playing"><span></span><span></span><span></span></div>
          {:else}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <button class="mp-pl-remove" on:click|stopPropagation={() => removeFromPlaylist(i)}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
            </button>
          {/if}
        </div>
      {/each}
      {#if playlist.length === 0}
        <div class="mp-pl-empty">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg>
          <span>Sin archivos en cola</span>
        </div>
      {/if}
    </div>
  </div>

  <!-- ── INNER ── -->
  <div class="mp-inner-wrap">
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="mp-inner" on:mousemove={onMouseMove}>
      <div class="mp-content">

        {#if currentFile && isVideo && currentSrc}
          <div class="mp-video-wrap">
            <!-- svelte-ignore a11y_media_has_caption -->
            <video bind:this={playerEl} src={currentSrc}
              bind:duration bind:currentTime bind:volume bind:muted
              on:ended={playNext}
              on:play={() => playing = true}
              on:pause={() => playing = false}
              class="mp-video"></video>
          </div>

        {:else if currentFile && !isVideo && currentSrc}
          <div class="mp-audio-screen">
            <div class="mp-audio-art">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1" stroke-linecap="round"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg>
            </div>
            <div class="mp-audio-name">{currentFile.name}</div>
            <div class="mp-audio-path">{currentFile._share}</div>
          </div>
          <!-- svelte-ignore a11y_media_has_caption -->
          <audio bind:this={playerEl} src={currentSrc}
            bind:duration bind:currentTime bind:volume bind:muted
            on:ended={playNext}
            on:play={() => playing = true}
            on:pause={() => playing = false}></audio>

        {:else}
          <div class="mp-empty-screen">
            <div class="mp-empty-icon">
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1" stroke-linecap="round"><polygon points="5 3 19 12 5 21 5 3"/></svg>
            </div>
            <div class="mp-empty-title">Sin reproducción</div>
            <div class="mp-empty-desc">Pulsa + para añadir archivos a la cola</div>
          </div>
        {/if}

        <!-- Controles flotantes -->
        {#if currentFile}
          <div class="mp-controls" class:hidden={!controlsVisible}>
            <div class="mp-progress-row">
              <span class="mp-time">{fmtTime(currentTime)}</span>
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="mp-progress" on:click={seek}>
                <div class="mp-progress-fill" style="width:{duration ? (currentTime/duration)*100 : 0}%">
                  <div class="mp-progress-thumb"></div>
                </div>
              </div>
              <span class="mp-time">{fmtTime(duration)}</span>
            </div>
            <div class="mp-btns-row">
              <div class="mp-now">
                <div class="mp-now-art" class:video={isVideo}>
                  {#if isVideo}
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><rect x="2" y="2" width="20" height="20" rx="2"/><polygon points="10 8 16 12 10 16 10 8"/></svg>
                  {:else}
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg>
                  {/if}
                </div>
                <div class="mp-now-info">
                  <div class="mp-now-name">{currentFile.name}</div>
                  <div class="mp-now-path">{currentFile._share}</div>
                </div>
              </div>
              <div class="mp-transport">
                <button class="mp-btn" on:click={playPrev}>
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="19 20 9 12 19 4 19 20"/><line x1="5" y1="19" x2="5" y2="5"/></svg>
                </button>
                <button class="mp-btn play" on:click={togglePlay}>
                  {#if playing}
                    <svg viewBox="0 0 24 24" fill="currentColor"><rect x="6" y="4" width="4" height="16" rx="1"/><rect x="14" y="4" width="4" height="16" rx="1"/></svg>
                  {:else}
                    <svg viewBox="0 0 24 24" fill="currentColor"><polygon points="5 3 19 12 5 21 5 3"/></svg>
                  {/if}
                </button>
                <button class="mp-btn" on:click={playNext}>
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><polygon points="5 4 15 12 5 20 5 4"/><line x1="19" y1="5" x2="19" y2="19"/></svg>
                </button>
              </div>
              <div class="mp-right">
                <div class="mp-vol-wrap">
                  <button class="mp-btn small" on:click={toggleMute}>
                    {#if muted || volume === 0}
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><line x1="23" y1="9" x2="17" y2="15"/><line x1="17" y1="9" x2="23" y2="15"/></svg>
                    {:else if volume < 0.5}
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><path d="M15.54 8.46a5 5 0 0 1 0 7.07"/></svg>
                    {:else}
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polygon points="11 5 6 9 2 9 2 15 6 15 11 19 11 5"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M15.54 8.46a5 5 0 0 1 0 7.07"/></svg>
                    {/if}
                  </button>
                  <!-- svelte-ignore a11y_click_events_have_key_events -->
                  <!-- svelte-ignore a11y_no_static_element_interactions -->
                  <div class="mp-vol-track" on:click={setVol}>
                    <div class="mp-vol-fill" style="width:{muted ? 0 : volume*100}%"></div>
                  </div>
                </div>
                <button class="mp-btn" on:click={toggleFullscreen} title="Pantalla completa">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round"><path d="M8 3H5a2 2 0 0 0-2 2v3m18 0V5a2 2 0 0 0-2-2h-3m0 18h3a2 2 0 0 0 2-2v-3M3 16v3a2 2 0 0 0 2 2h3"/></svg>
                </button>
              </div>
            </div>
          </div>
        {/if}
      </div>
    </div>
  </div>
</div>

<!-- ══ MODAL EXPLORADOR ══ -->
{#if showModal}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-overlay" on:click|self={() => showModal = false}></div>
  <div class="modal">
    <div class="modal-header">
      <div class="modal-title">Añadir a la cola</div>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="modal-close" on:click={() => showModal = false}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
      </div>
    </div>

    <div class="modal-shares">
      {#each modalShares as s}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="modal-share-tab" class:active={modalShare === s.name} on:click={() => modalSelectShare(s.name)}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
          {s.displayName || s.name}
        </div>
      {/each}
    </div>

    <div class="modal-breadcrumb">
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <span class="modal-bc-root" on:click={() => { modalPath='/'; loadModalFiles(); }}>{modalShare}</span>
      {#each modalBreadcrumbs as crumb, i}
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" style="width:9px;height:9px;color:var(--text-3);flex-shrink:0"><polyline points="9 18 15 12 9 6"/></svg>
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <span class="modal-bc-crumb" on:click={() => { modalPath='/'+modalBreadcrumbs.slice(0,i+1).join('/'); loadModalFiles(); }}>{crumb}</span>
      {/each}
    </div>

    <div class="modal-files">
      {#if modalLoading}
        <div class="modal-loading"><div class="spinner"></div></div>
      {:else}
        {#if modalPath !== '/'}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="modal-file is-dir" on:click={modalGoUp}>
            <div class="modal-file-ico dir"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="15 18 9 12 15 6"/></svg></div>
            <span class="modal-file-name" style="color:var(--text-3)">Volver</span>
          </div>
        {/if}
        {#each modalFiles as file}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="modal-file"
            class:is-dir={file.isDirectory}
            class:is-media={!file.isDirectory && isMedia(file.name)}
            on:click={() => file.isDirectory ? modalEnterFolder(file.name) : null}>
            <div class="modal-file-ico"
              class:dir={file.isDirectory}
              class:video={!file.isDirectory && isVideoFile(file.name)}
              class:audio={!file.isDirectory && !isVideoFile(file.name) && isMedia(file.name)}>
              {#if file.isDirectory}
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
              {:else if isVideoFile(file.name)}
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><rect x="2" y="2" width="20" height="20" rx="2"/><polygon points="10 8 16 12 10 16 10 8"/></svg>
              {:else if isMedia(file.name)}
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M9 18V5l12-2v13"/><circle cx="6" cy="18" r="3"/><circle cx="18" cy="16" r="3"/></svg>
              {:else}
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
              {/if}
            </div>
            <span class="modal-file-name">{file.name}</span>
            <span class="modal-file-size">{file.isDirectory ? '' : fmtSize(file.size)}</span>
            {#if !file.isDirectory && isMedia(file.name)}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <button class="modal-add-file" on:click|stopPropagation={() => addToPlaylist(file)} title="Añadir">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
              </button>
            {/if}
          </div>
        {/each}
        {#if modalFiles.length === 0}
          <div class="modal-empty">Carpeta vacía</div>
        {/if}
      {/if}
    </div>

    <div class="modal-footer">
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <button class="modal-btn-secondary" on:click={() => showModal = false}>Cerrar</button>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <button class="modal-btn-accent" on:click={addAllMedia}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" style="width:11px;height:11px"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
        Añadir todos
      </button>
    </div>
  </div>
{/if}

<style>
  .mp-root { width:100%; height:100%; display:flex; overflow:hidden; font-family:'Inter',-apple-system,sans-serif; color:var(--text-1); }

  /* Sidebar */
  .mp-sidebar { width:200px; flex-shrink:0; padding:12px 8px; background:var(--bg-sidebar); display:flex; flex-direction:column; overflow:hidden; }
  .mp-sidebar-header { display:flex; align-items:center; gap:8px; padding:28px 10px 14px; color:var(--text-1); }
  .mp-title { font-size:15px; font-weight:600; }
  .mp-section-header { display:flex; align-items:center; justify-content:space-between; padding:4px 8px; }
  .mp-section-label { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.08em; }
  .mp-add-btn { width:18px; height:18px; border-radius:4px; border:1px solid var(--border); background:transparent; color:var(--text-3); cursor:pointer; display:flex; align-items:center; justify-content:center; transition:all .15s; }
  .mp-add-btn svg { width:10px; height:10px; }
  .mp-add-btn:hover { color:var(--text-1); border-color:var(--border-hi); background:var(--ibtn-bg); }
  .mp-playlist { display:flex; flex-direction:column; gap:1px; overflow-y:auto; flex:1; margin-top:2px; }
  .mp-playlist::-webkit-scrollbar { width:3px; }
  .mp-playlist::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }
  .mp-pl-item { display:flex; align-items:center; gap:7px; padding:6px 8px; border-radius:6px; font-size:11px; color:var(--text-3); cursor:pointer; transition:all .1s; }
  .mp-pl-item svg { width:11px; height:11px; flex-shrink:0; opacity:.6; }
  .mp-pl-item:hover { background:rgba(128,128,128,0.06); color:var(--text-2); }
  .mp-pl-item.active { color:var(--accent); background:var(--active-bg); }
  .mp-pl-item.active svg { opacity:1; }
  .mp-pl-name { flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .mp-pl-playing { display:flex; align-items:flex-end; gap:2px; height:12px; flex-shrink:0; }
  .mp-pl-playing span { width:2px; border-radius:1px; background:var(--accent); animation:bar-bounce .8s ease-in-out infinite; }
  .mp-pl-playing span:nth-child(1) { height:5px; animation-delay:0s; }
  .mp-pl-playing span:nth-child(2) { height:9px; animation-delay:.2s; }
  .mp-pl-playing span:nth-child(3) { height:6px; animation-delay:.4s; }
  @keyframes bar-bounce { 0%,100%{transform:scaleY(0.4)} 50%{transform:scaleY(1)} }
  .mp-pl-remove { width:16px; height:16px; border:none; background:transparent; color:var(--text-3); cursor:pointer; display:flex; align-items:center; justify-content:center; opacity:0; transition:all .1s; flex-shrink:0; padding:0; }
  .mp-pl-remove svg { width:10px; height:10px; }
  .mp-pl-item:hover .mp-pl-remove { opacity:1; }
  .mp-pl-remove:hover { color:var(--red); }
  .mp-pl-empty { display:flex; flex-direction:column; align-items:center; gap:8px; padding:24px 8px; opacity:.4; }
  .mp-pl-empty svg { width:22px; height:22px; }
  .mp-pl-empty span { font-size:10px; color:var(--text-3); text-align:center; }

  /* Inner */
  .mp-inner-wrap { flex:1; padding:8px; display:flex; }
  .mp-inner { flex:1; border-radius:10px; border:1px solid var(--border); background:var(--bg-inner); display:flex; flex-direction:column; overflow:hidden; position:relative; }
  .mp-content { flex:1; position:relative; overflow:hidden; }

  /* Video */
  .mp-video-wrap { position:absolute; inset:0; display:flex; align-items:center; justify-content:center; background:#000; }
  .mp-video { width:100%; height:100%; object-fit:contain; }

  /* Audio */
  .mp-audio-screen { position:absolute; inset:0; display:flex; flex-direction:column; align-items:center; justify-content:center; gap:14px; padding-bottom:80px; }
  .mp-audio-art { width:120px; height:120px; border-radius:20px; background:rgba(124,111,255,0.12); border:1px solid rgba(124,111,255,0.20); display:flex; align-items:center; justify-content:center; }
  .mp-audio-art svg { width:52px; height:52px; color:var(--accent); opacity:.6; }
  .mp-audio-name { font-size:14px; font-weight:600; color:var(--text-1); max-width:400px; text-align:center; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .mp-audio-path { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; }

  /* Empty */
  .mp-empty-screen { position:absolute; inset:0; display:flex; flex-direction:column; align-items:center; justify-content:center; gap:12px; }
  .mp-empty-icon { width:64px; height:64px; border-radius:16px; background:rgba(124,111,255,0.08); border:1px solid rgba(124,111,255,0.12); display:flex; align-items:center; justify-content:center; }
  .mp-empty-icon svg { width:28px; height:28px; color:var(--accent); opacity:.4; }
  .mp-empty-title { font-size:13px; font-weight:600; color:var(--text-2); }
  .mp-empty-desc { font-size:11px; color:var(--text-3); }

  /* Controles flotantes */
  .mp-controls { position:absolute; bottom:0; left:0; right:0; padding:20px 20px 16px; background:linear-gradient(to top,rgba(0,0,0,0.88) 0%,rgba(0,0,0,0.45) 60%,transparent 100%); display:flex; flex-direction:column; gap:10px; transition:opacity .35s ease; z-index:10; border-radius:0 0 10px 10px; }
  .mp-controls.hidden { opacity:0; pointer-events:none; }
  .mp-progress-row { display:flex; align-items:center; gap:8px; }
  .mp-time { font-size:10px; color:rgba(255,255,255,0.55); font-family:'DM Mono',monospace; min-width:32px; text-align:center; }
  .mp-progress { flex:1; height:3px; border-radius:2px; background:rgba(255,255,255,0.15); cursor:pointer; position:relative; transition:height .15s; }
  .mp-progress:hover { height:5px; }
  .mp-progress-fill { height:100%; border-radius:2px; background:linear-gradient(90deg,var(--accent),var(--accent2)); position:relative; }
  .mp-progress-thumb { position:absolute; right:-5px; top:50%; transform:translateY(-50%) scale(0); width:11px; height:11px; border-radius:50%; background:#fff; transition:transform .15s; }
  .mp-progress:hover .mp-progress-thumb { transform:translateY(-50%) scale(1); }
  .mp-btns-row { display:flex; align-items:center; gap:4px; }
  .mp-now { display:flex; align-items:center; gap:9px; flex:1; min-width:0; }
  .mp-now-art { width:34px; height:34px; border-radius:7px; flex-shrink:0; background:rgba(124,111,255,0.20); border:1px solid rgba(124,111,255,0.30); display:flex; align-items:center; justify-content:center; }
  .mp-now-art svg { width:15px; height:15px; color:var(--accent); }
  .mp-now-art.video { background:rgba(96,165,250,0.15); border-color:rgba(96,165,250,0.25); }
  .mp-now-art.video svg { color:var(--blue); }
  .mp-now-info { overflow:hidden; min-width:0; }
  .mp-now-name { font-size:12px; font-weight:600; color:#fff; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .mp-now-path { font-size:9px; color:rgba(255,255,255,0.4); font-family:'DM Mono',monospace; }
  .mp-transport { display:flex; align-items:center; gap:6px; flex:1; justify-content:center; margin-right:80px; }
  .mp-transport .mp-btn svg { width:17px; height:17px; }
  .mp-btn { width:34px; height:34px; border:none; background:none; color:rgba(255,255,255,0.7); cursor:pointer; border-radius:8px; display:flex; align-items:center; justify-content:center; transition:all .12s; }
  .mp-btn svg { width:15px; height:15px; }
  .mp-btn:hover { background:rgba(255,255,255,0.10); color:#fff; }
  .mp-btn.play { width:42px; height:42px; border-radius:50%; background:rgba(255,255,255,0.15); backdrop-filter:blur(8px); border:1px solid rgba(255,255,255,0.25); color:#fff; }
  .mp-btn.play svg { width:18px; height:18px; }
  .mp-btn.play:hover { background:rgba(255,255,255,0.25); }
  .mp-btn.small { width:28px; height:28px; }
  .mp-btn.small svg { width:13px; height:13px; }
  .mp-right { display:flex; align-items:center; gap:4px; flex-shrink:0; }
  .mp-vol-wrap { display:flex; align-items:center; gap:6px; }
  .mp-vol-track { width:68px; height:3px; border-radius:2px; background:rgba(255,255,255,0.15); cursor:pointer; transition:height .15s; }
  .mp-vol-track:hover { height:5px; }
  .mp-vol-fill { height:100%; border-radius:2px; background:rgba(255,255,255,0.7); }

  /* Modal */
  .modal-overlay { position:fixed; inset:0; z-index:200; background:rgba(0,0,0,0.65); backdrop-filter:blur(3px); }
  .modal { position:fixed; top:50%; left:50%; transform:translate(-50%,-50%); z-index:201; width:520px; max-width:92%; max-height:80vh; background:var(--bg-inner); border-radius:12px; border:1px solid var(--border); box-shadow:0 24px 60px rgba(0,0,0,0.5); display:flex; flex-direction:column; overflow:hidden; animation:modalIn .2s cubic-bezier(0.16,1,0.3,1) both; }
  @keyframes modalIn { from{opacity:0;transform:translate(-50%,-48%) scale(0.97)} to{opacity:1;transform:translate(-50%,-50%) scale(1)} }
  .modal-header { display:flex; align-items:center; justify-content:space-between; padding:14px 18px; border-bottom:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; }
  .modal-title { font-size:13px; font-weight:600; color:var(--text-1); }
  .modal-close { width:24px; height:24px; border-radius:6px; cursor:pointer; display:flex; align-items:center; justify-content:center; color:var(--text-3); background:var(--ibtn-bg); transition:all .15s; }
  .modal-close svg { width:12px; height:12px; }
  .modal-close:hover { color:var(--text-1); }
  .modal-shares { display:flex; gap:0; padding:0 14px; flex-shrink:0; border-bottom:1px solid var(--border); }
  .modal-share-tab { display:flex; align-items:center; gap:6px; padding:10px 10px; font-size:11px; color:var(--text-3); cursor:pointer; border-bottom:2px solid transparent; margin-bottom:-1px; transition:all .15s; }
  .modal-share-tab svg { width:12px; height:12px; flex-shrink:0; }
  .modal-share-tab:hover { color:var(--text-2); }
  .modal-share-tab.active { color:var(--accent); border-bottom-color:var(--accent); }
  .modal-breadcrumb { display:flex; align-items:center; gap:4px; padding:10px 18px; font-size:11px; flex-shrink:0; border-bottom:1px solid var(--border); }
  .modal-bc-root { color:var(--text-2); cursor:pointer; font-weight:500; font-family:'DM Mono',monospace; font-size:11px; transition:color .1s; }
  .modal-bc-root:hover { color:var(--text-1); }
  .modal-bc-crumb { color:var(--text-3); cursor:pointer; font-family:'DM Mono',monospace; font-size:10px; transition:color .1s; }
  .modal-bc-crumb:hover { color:var(--text-2); }
  .modal-files { flex:1; overflow-y:auto; padding:8px 10px; }
  .modal-files::-webkit-scrollbar { width:3px; }
  .modal-files::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }
  .modal-file { display:flex; align-items:center; gap:10px; padding:7px 8px; border-radius:7px; font-size:12px; color:var(--text-2); transition:all .1s; }
  .modal-file.is-dir { cursor:pointer; }
  .modal-file.is-dir:hover { background:rgba(128,128,128,0.06); color:var(--text-1); }
  .modal-file.is-media:hover { background:var(--active-bg); color:var(--text-1); }
  .modal-file-ico { width:28px; height:28px; border-radius:6px; flex-shrink:0; display:flex; align-items:center; justify-content:center; background:rgba(255,255,255,0.05); }
  .modal-file-ico svg { width:14px; height:14px; color:var(--text-3); }
  .modal-file-ico.dir   { background:rgba(251,191,36,0.10); } .modal-file-ico.dir svg   { color:var(--amber); }
  .modal-file-ico.video { background:rgba(124,111,255,0.10); } .modal-file-ico.video svg { color:var(--accent); }
  .modal-file-ico.audio { background:rgba(74,222,128,0.10);  } .modal-file-ico.audio svg { color:var(--green); }
  .modal-file-name { flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .modal-file-size { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; flex-shrink:0; }
  .modal-add-file { width:24px; height:24px; border-radius:5px; border:1px solid var(--border); background:transparent; color:var(--text-3); cursor:pointer; display:flex; align-items:center; justify-content:center; opacity:0; transition:all .1s; flex-shrink:0; }
  .modal-add-file svg { width:10px; height:10px; }
  .modal-file:hover .modal-add-file { opacity:1; }
  .modal-add-file:hover { color:var(--accent); border-color:var(--border-hi); background:var(--active-bg); }
  .modal-empty { font-size:11px; color:var(--text-3); padding:24px 0; text-align:center; }
  .modal-loading { display:flex; justify-content:center; padding:30px; }
  .spinner { width:20px; height:20px; border-radius:50%; border:2px solid rgba(255,255,255,0.08); border-top-color:var(--accent); animation:spin .7s linear infinite; }
  @keyframes spin { to{transform:rotate(360deg)} }
  .modal-footer { display:flex; align-items:center; justify-content:flex-end; gap:8px; padding:12px 18px; border-top:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; }
  .modal-btn-secondary { padding:7px 13px; border-radius:8px; border:1px solid var(--border); background:var(--ibtn-bg); color:var(--text-2); font-size:11px; font-weight:500; cursor:pointer; font-family:inherit; transition:all .15s; }
  .modal-btn-secondary:hover { color:var(--text-1); border-color:var(--border-hi); }
  .modal-btn-accent { display:inline-flex; align-items:center; gap:6px; padding:7px 13px; border-radius:8px; border:none; background:linear-gradient(135deg,var(--accent),var(--accent2)); color:#fff; font-size:11px; font-weight:600; cursor:pointer; font-family:inherit; transition:opacity .15s; }
  .modal-btn-accent:hover { opacity:.88; }
</style>
