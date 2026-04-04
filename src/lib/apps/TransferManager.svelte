<script>
  import { uploadTasks, activeTasks, cancelTask, removeTask, clearDone, pauseTask, resumeTask } from '$lib/stores/uploadTasks.js';

  let activeTab = 'active'; // 'active' | 'done' | 'error'

  $: tabActive = $uploadTasks.filter(t => t.status === 'uploading' || t.status === 'queued' || t.status === 'paused');
  $: tabDone   = $uploadTasks.filter(t => t.status === 'done');
  $: tabError  = $uploadTasks.filter(t => t.status === 'error');

  $: current = activeTab === 'active' ? tabActive : activeTab === 'done' ? tabDone : tabError;

  function fmtSize(bytes) {
    if (!bytes) return '—';
    if (bytes >= 1e9) return (bytes / 1e9).toFixed(1) + ' GB';
    if (bytes >= 1e6) return (bytes / 1e6).toFixed(0) + ' MB';
    return (bytes / 1e3).toFixed(0) + ' KB';
  }

  function fmtPct(pct) { return Math.round(pct) + '%'; }

  function fmtSpeed(bps) {
    if (!bps || bps <= 0) return '';
    if (bps >= 1e6) return (bps / 1e6).toFixed(1) + ' MB/s';
    if (bps >= 1e3) return (bps / 1e3).toFixed(0) + ' KB/s';
    return Math.round(bps) + ' B/s';
  }
</script>

<div class="tm">
  <!-- TABS -->
  <div class="tabs">
    <span class="tab" class:on={activeTab==='active'} on:click={() => activeTab='active'}>
      Activas
      {#if tabActive.length > 0}<span class="tab-badge">{tabActive.length}</span>{/if}
    </span>
    <span class="tab" class:on={activeTab==='done'} on:click={() => activeTab='done'}>
      Completadas
      {#if tabDone.length > 0}<span class="tab-badge green">{tabDone.length}</span>{/if}
    </span>
    <span class="tab" class:on={activeTab==='error'} on:click={() => activeTab='error'}>
      Errores
      {#if tabError.length > 0}<span class="tab-badge red">{tabError.length}</span>{/if}
    </span>

    <!-- toolbar right -->
    <div class="tab-actions">
      {#if activeTab === 'done' && tabDone.length > 0}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <span class="clear-btn" on:click={clearDone}>Limpiar</span>
      {/if}
      {#if activeTab === 'error' && tabError.length > 0}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <span class="clear-btn" on:click={() => tabError.forEach(t => removeTask(t.id))}>Limpiar</span>
      {/if}
    </div>
  </div>

  <!-- LIST -->
  <div class="list">
    {#if current.length === 0}
      <div class="empty">
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round">
          <path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/>
          <polyline points="17 8 12 3 7 8"/>
          <line x1="12" y1="3" x2="12" y2="15"/>
        </svg>
        <span>
          {activeTab === 'active' ? 'Sin transferencias activas' :
           activeTab === 'done'   ? 'Sin transferencias completadas' :
                                    'Sin errores'}
        </span>
      </div>
    {:else}
      {#each current as task (task.id)}
        <div class="row" class:row-done={task.status==='done'} class:row-error={task.status==='error'}>

          <!-- Icon -->
          <div class="row-ico">
            {#if task.status === 'uploading'}
              <svg width="16" height="16" viewBox="0 0 24 24" fill="#7c6fff" stroke="none" style="overflow:visible">
                <polygon points="12,2 20,12 15,12 15,22 9,22 9,12 4,12">
                  <animateTransform attributeName="transform" type="translate" values="0,14;0,-14" dur="1.2s" repeatCount="indefinite"/>
                  <animate attributeName="opacity" values="0;1;1;0" keyTimes="0;0.25;0.75;1" dur="1.2s" repeatCount="indefinite"/>
                </polygon>
                <polygon points="12,2 20,12 15,12 15,22 9,22 9,12 4,12">
                  <animateTransform attributeName="transform" type="translate" values="0,14;0,-14" dur="1.2s" begin="-0.6s" repeatCount="indefinite"/>
                  <animate attributeName="opacity" values="0;1;1;0" keyTimes="0;0.25;0.75;1" dur="1.2s" begin="-0.6s" repeatCount="indefinite"/>
                </polygon>
              </svg>
            {:else if task.status === 'paused'}
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--amber)" stroke-width="2.5" stroke-linecap="round">
                <rect x="6" y="4" width="4" height="16" rx="1"/><rect x="14" y="4" width="4" height="16" rx="1"/>
              </svg>
            {:else if task.status === 'queued'}
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--text-3)" stroke-width="2.5" stroke-linecap="round">
                <circle cx="12" cy="12" r="10"/><polyline points="12 6 12 12 16 14"/>
              </svg>
            {:else if task.status === 'done'}
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--green)" stroke-width="2.5" stroke-linecap="round">
                <polyline points="20 6 9 17 4 12"/>
              </svg>
            {:else}
              <svg width="16" height="16" viewBox="0 0 24 24" fill="none" stroke="var(--red)" stroke-width="2.5" stroke-linecap="round">
                <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
              </svg>
            {/if}
          </div>

          <!-- File info -->
          <div class="row-info">
            <div class="row-name">{task.name}</div>
            {#if task.status === 'uploading'}
              <div class="prog-wrap">
                <div class="prog-bar" style="width:{task.progress}%"></div>
              </div>
              <div class="row-meta">{fmtPct(task.progress)} · {fmtSize(task.size)}{#if task.speed} · {fmtSpeed(task.speed)}{/if}</div>
            {:else if task.status === 'paused'}
              <div class="prog-wrap">
                <div class="prog-bar paused" style="width:{task.progress}%"></div>
              </div>
              <div class="row-meta paused-text">Pausado · {fmtPct(task.progress)} · {fmtSize(task.size)}</div>
            {:else if task.status === 'queued'}
              <div class="row-meta queued-text">En cola · {fmtSize(task.size)}</div>
            {:else if task.status === 'done'}
              <div class="row-meta done">Completado · {fmtSize(task.size)}</div>
            {:else}
              <div class="row-meta error">{task.error || 'Error desconocido'}</div>
            {/if}
          </div>

          <!-- Action -->
          <div class="row-actions">
            {#if task.status === 'uploading'}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="action-btn pause" title="Pausar" on:click={() => pauseTask(task.id)}>
                <svg viewBox="0 0 24 24" fill="currentColor" stroke="none">
                  <rect x="6" y="4" width="4" height="16" rx="1"/><rect x="14" y="4" width="4" height="16" rx="1"/>
                </svg>
              </div>
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="action-btn cancel" title="Cancelar" on:click={() => cancelTask(task.id)}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                  <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                </svg>
              </div>
            {:else if task.status === 'paused'}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="action-btn resume" title="Reanudar" on:click={() => resumeTask(task.id)}>
                <svg viewBox="0 0 24 24" fill="currentColor" stroke="none">
                  <polygon points="6,4 20,12 6,20"/>
                </svg>
              </div>
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="action-btn cancel" title="Cancelar" on:click={() => cancelTask(task.id)}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                  <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                </svg>
              </div>
            {:else if task.status === 'queued'}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="action-btn cancel" title="Cancelar" on:click={() => cancelTask(task.id)}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                  <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                </svg>
              </div>
            {:else}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="action-btn remove" title="Eliminar" on:click={() => removeTask(task.id)}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                  <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
                </svg>
              </div>
            {/if}
          </div>

        </div>
      {/each}
    {/if}
  </div>

  <!-- STATUSBAR -->
  <div class="statusbar">
    {#if $activeTasks.length > 0}
      <div class="sb-dot active"></div>
      <span>{$activeTasks.length} subiendo</span>
    {:else}
      <div class="sb-dot idle"></div>
      <span>Sin actividad</span>
    {/if}
    <span class="sb-right">{tabDone.length} completadas · {tabError.length} errores</span>
  </div>
</div>

<style>
  .tm { display:flex; flex-direction:column; height:100%; background:var(--bg-inner); }

  /* TABS */
  .tabs { display:flex; align-items:center; gap:0; padding:0 14px; border-bottom:1px solid var(--border); flex-shrink:0; background:var(--bg-bar); }
  .tab { font-size:11px; font-weight:600; color:var(--text-3); cursor:pointer; padding:10px 0; margin-right:18px; border-bottom:2px solid transparent; transition:all .15s; display:flex; align-items:center; gap:5px; }
  .tab:hover:not(.on) { color:var(--text-2); }
  .tab.on { color:var(--text-1); border-bottom-color:var(--accent); }
  .tab-badge { font-size:9px; font-weight:700; padding:1px 5px; border-radius:5px; background:var(--active-bg); color:var(--accent); }
  .tab-badge.green { background:rgba(74,222,128,0.12); color:var(--green); }
  .tab-badge.red   { background:rgba(248,113,113,0.12); color:var(--red); }
  .tab-actions { margin-left:auto; }
  .clear-btn { font-size:10px; color:var(--text-3); cursor:pointer; transition:color .15s; }
  .clear-btn:hover { color:var(--red); }

  /* LIST */
  .list { flex:1; overflow-y:auto; padding:6px 0; }
  .list::-webkit-scrollbar { width:3px; }
  .list::-webkit-scrollbar-thumb { background:var(--border); border-radius:2px; }

  /* EMPTY */
  .empty { display:flex; flex-direction:column; align-items:center; justify-content:center; gap:10px; height:200px; color:var(--text-3); font-size:12px; }
  .empty svg { width:32px; height:32px; opacity:.4; }

  /* ROW */
  .row { display:flex; align-items:center; gap:12px; padding:11px 16px; border-bottom:1px solid var(--border); transition:background .12s; border-left:3px solid transparent; }
  .row:hover { background:var(--ibtn-bg); }
  .row-done  { border-left-color:var(--green); }
  .row-error { border-left-color:var(--red); }

  .row-ico { width:20px; height:20px; display:flex; align-items:center; justify-content:center; flex-shrink:0; overflow:hidden; }

  .row-info { flex:1; min-width:0; }
  .row-name { font-size:12px; font-weight:500; color:var(--text-1); overflow:hidden; text-overflow:ellipsis; white-space:nowrap; margin-bottom:4px; }
  .row-meta { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; margin-top:3px; }
  .row-meta.done  { color:var(--green); }
  .row-meta.error { color:var(--red); }
  .row-meta.paused-text { color:var(--amber); }
  .row-meta.queued-text { color:var(--text-3); }

  /* PROGRESS */
  .prog-wrap { height:3px; background:var(--border); border-radius:2px; overflow:hidden; }
  .prog-bar  { height:100%; background:var(--accent); border-radius:2px; transition:width .4s ease; }
  .prog-bar.paused { background:var(--amber); }

  /* ACTION */
  .row-actions { display:flex; gap:4px; flex-shrink:0; }
  .action-btn { width:26px; height:26px; border-radius:6px; display:flex; align-items:center; justify-content:center; cursor:pointer; transition:all .15s; flex-shrink:0; }
  .action-btn svg { width:12px; height:12px; }
  .action-btn.pause { color:var(--amber); }
  .action-btn.pause:hover { background:rgba(251,191,36,0.12); }
  .action-btn.resume { color:var(--green); }
  .action-btn.resume:hover { background:rgba(74,222,128,0.12); }
  .action-btn.cancel { color:var(--text-3); }
  .action-btn.cancel:hover { background:rgba(248,113,113,0.1); color:var(--red); }
  .action-btn.remove { color:var(--text-3); }
  .action-btn.remove:hover { background:rgba(248,113,113,0.1); color:var(--red); }

  /* STATUSBAR */
  .statusbar { display:flex; align-items:center; gap:8px; padding:7px 14px; border-top:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; font-size:10px; color:var(--text-3); }
  .sb-dot { width:6px; height:6px; border-radius:50%; flex-shrink:0; }
  .sb-dot.active { background:var(--accent); box-shadow:0 0 5px var(--accent); animation:pulse .8s ease-in-out infinite; }
  .sb-dot.idle   { background:var(--text-3); }
  @keyframes pulse { 0%,100%{opacity:1} 50%{opacity:.4} }
  .sb-right { margin-left:auto; }
</style>
