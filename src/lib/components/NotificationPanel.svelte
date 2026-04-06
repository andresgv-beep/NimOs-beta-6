<script>
  import { notifications, unreadCount, dismissNotification, clearCategory, markAllRead } from '$lib/stores/notifications.js';
  import { uploadTasks, removeTask, clearDone, cancelTask } from '$lib/stores/uploadTasks.js';

  export let open = false;

  let activeTab = 'notification'; // 'notification' | 'system' | 'tasks'

  $: general = $notifications.filter(n => n.category === 'notification');
  $: system  = $notifications.filter(n => n.category === 'system');
  $: systemAlerts = system.filter(n => n.type === 'error' || n.type === 'warning');
  $: current = activeTab === 'notification' ? general : activeTab === 'system' ? system : [];

  const ICONS = {
    success:  '<polyline points="20 6 9 17 4 12"/>',
    error:    '<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>',
    warning:  '<path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>',
    info:     '<circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/>',
    security: '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>',
  };

  function getIcon(type) { return ICONS[type] || ICONS.info; }

  function fmtTime(iso) {
    const diff = Math.floor((Date.now() - new Date(iso)) / 1000);
    if (diff < 60) return 'ahora';
    if (diff < 3600) return `hace ${Math.floor(diff/60)}m`;
    if (diff < 86400) return `hace ${Math.floor(diff/3600)}h`;
    return `hace ${Math.floor(diff/86400)}d`;
  }

  function clearCurrent() {
    if (activeTab === 'tasks') clearDone();
    else clearCategory(activeTab);
  }

  function fmtSize(b) {
    if (!b) return '';
    if (b >= 1e9) return (b/1e9).toFixed(1) + ' GB';
    if (b >= 1e6) return (b/1e6).toFixed(0) + ' MB';
    return (b/1e3).toFixed(0) + ' KB';
  }
</script>

{#if open}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="np-backdrop" on:click={() => open = false}></div>

  <div class="np">
    <div class="np-head">
      <span class="np-title">Notificaciones</span>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <span class="np-clear" on:click={clearCurrent}>Limpiar</span>
    </div>

    <div class="np-tabs">
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <span class="np-tab" class:on={activeTab === 'notification'} on:click={() => activeTab = 'notification'}>General</span>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <span class="np-tab" class:on={activeTab === 'system'} on:click={() => activeTab = 'system'}>Sistema{#if systemAlerts.length > 0} <span class="tab-badge" style="background:var(--amber)">{systemAlerts.length}</span>{/if}</span>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <span class="np-tab" class:on={activeTab === 'tasks'} on:click={() => activeTab = 'tasks'}>Tareas{#if $uploadTasks.length > 0} <span class="tab-badge">{$uploadTasks.length}</span>{/if}</span>
    </div>

    {#if activeTab !== 'tasks'}
    <div class="np-list">
      {#if current.length === 0}
        <div class="np-empty">Sin notificaciones</div>
      {:else}
        {#each current as n (n.id)}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="np-item t-{n.type}" class:unread={!n.read} on:click={() => {}}>
            <div class="np-ico">
              <svg viewBox="0 0 24 24" fill="none" stroke-width="2.5" stroke-linecap="round">
                {@html getIcon(n.type)}
              </svg>
            </div>
            <div class="np-body">
              {#if n.title}<div class="np-ititle">{n.title}</div>{/if}
              <div class="np-imsg" class:solo={!n.title}>{n.message}</div>
              <div class="np-itime">{fmtTime(n.timestamp)}</div>
            </div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <span class="np-x" on:click|stopPropagation={() => dismissNotification(n.id)}>
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
                <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
              </svg>
            </span>
          </div>
        {/each}
      {/if}
    </div>
    {/if}

    {#if activeTab === 'tasks'}
      <div class="np-list">
        {#if $uploadTasks.length === 0}
          <div class="np-empty">Sin tareas activas</div>
        {:else}
          {#each $uploadTasks as task (task.id)}
            <div class="task-item" class:task-done={task.status === 'done'} class:task-error={task.status === 'error'}>
              <div class="task-ico t-{task.status === 'done' ? 'success' : task.status === 'error' ? 'error' : 'info'}">
                {#if task.status === 'done'}
                  <svg viewBox="0 0 24 24" fill="none" stroke-width="2.5" stroke-linecap="round"><polyline points="20 6 9 17 4 12"/></svg>
                {:else if task.status === 'error'}
                  <svg viewBox="0 0 24 24" fill="none" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                {:else}
                  <svg viewBox="0 0 24 24" fill="none" stroke-width="2.5" stroke-linecap="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
                {/if}
              </div>
              <div class="task-body">
                <div class="task-name">{task.name}</div>
                {#if task.status === 'uploading'}
                  <div class="task-track"><div class="task-fill" style="width:{task.progress}%"></div></div>
                  <div class="task-pct">{task.progress}% · {fmtSize(task.size)}</div>
                {:else if task.status === 'done'}
                  <div class="task-meta" style="color:var(--green)">Completado · {fmtSize(task.size)}</div>
                {:else}
                  <div class="task-meta" style="color:var(--red)">{task.error || 'Error'}</div>
                {/if}
              </div>
              {#if task.status === 'uploading'}
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <span class="np-x np-cancel" on:click={() => cancelTask(task.id)} title="Cancelar subida">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><rect x="3" y="3" width="18" height="18" rx="2"/><line x1="9" y1="9" x2="15" y2="15"/><line x1="15" y1="9" x2="9" y2="15"/></svg>
                </span>
              {:else}
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <span class="np-x" on:click={() => removeTask(task.id)}>
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                </span>
              {/if}
            </div>
          {/each}
        {/if}
      </div>
    {/if}
  </div>
{/if}

<style>
  .np-backdrop { position:fixed; inset:0; z-index:498; }

  .np {
    position: fixed;
    bottom: calc(var(--taskbar-height, 48px) + 8px);
    right: 12px;
    width: 340px;
    max-height: 440px;
    background: var(--glass-bg);
    -webkit-backdrop-filter: blur(20px) saturate(1.4); backdrop-filter: blur(20px) saturate(1.4);
    -webkit--webkit-backdrop-filter: blur(20px) saturate(1.4); backdrop-filter: blur(20px) saturate(1.4);
    border: 1px solid var(--glass-border);
    border-radius: 14px;
    box-shadow: 0 20px 60px rgba(0,0,0,0.4);
    display: flex;
    flex-direction: column;
    overflow: hidden;
    z-index: 499;
    animation: npIn .18s ease;
  }
  @keyframes npIn { from{opacity:0;transform:translateY(8px)} to{opacity:1;transform:none} }

  .np-head { display:flex; align-items:center; justify-content:space-between; padding:14px 16px 0; flex-shrink:0; }
  .np-title { font-size:13px; font-weight:700; color:var(--text-1); }
  .np-clear { font-size:10px; color:var(--text-3); cursor:pointer; transition:color .15s; }
  .np-clear:hover { color:var(--red); }

  .np-tabs { display:flex; gap:18px; padding:10px 16px 0; border-bottom:1px solid var(--border); flex-shrink:0; }
  .np-tab { font-size:11px; font-weight:600; color:var(--text-3); cursor:pointer; padding-bottom:8px; border-bottom:2px solid transparent; transition:all .15s; }
  .np-tab:hover:not(.on) { color:var(--text-2); }
  .np-tab.on { color:var(--text-1); border-bottom-color:var(--accent); }

  .np-list { flex:1; overflow-y:auto; padding:4px 0; display:flex; flex-direction:column; }
  .np-list::-webkit-scrollbar { width:2px; }
  .np-list::-webkit-scrollbar-thumb { background:var(--border); border-radius:2px; }

  .np-item { display:flex; align-items:flex-start; gap:10px; padding:11px 14px; cursor:pointer; position:relative; transition:background .12s; border-left:2px solid transparent; }
  .np-item:hover { background:var(--ibtn-bg); }
  .np-item + .np-item { border-top:1px solid var(--border); }
  .np-item.unread { border-left-color:var(--tc); }

  .np-ico { width:24px; height:24px; border-radius:6px; display:flex; align-items:center; justify-content:center; flex-shrink:0; margin-top:1px; }
  .np-ico svg { width:11px; height:11px; fill:none; stroke-width:2.5; stroke-linecap:round; }

  .t-success { --tc:var(--green); } .t-success .np-ico { background:rgba(74,222,128,0.12); } .t-success .np-ico svg { stroke:var(--green); }
  .t-error   { --tc:var(--red);   } .t-error .np-ico   { background:rgba(248,113,113,0.12); } .t-error .np-ico svg   { stroke:var(--red); }
  .t-warning { --tc:var(--amber); } .t-warning .np-ico { background:rgba(251,191,36,0.12); }  .t-warning .np-ico svg { stroke:var(--amber); }
  .t-info    { --tc:var(--blue);} .t-info .np-ico    { background:rgba(96,165,250,0.12); } .t-info .np-ico svg    { stroke:var(--blue); }
  .t-security{ --tc:var(--red);   } .t-security .np-ico{ background:rgba(248,113,113,0.12); } .t-security .np-ico svg{ stroke:var(--red); }

  .np-body { flex:1; min-width:0; }
  .np-ititle { font-size:11px; font-weight:700; color:var(--text-1); }
  .np-imsg { font-size:10px; color:var(--text-2); margin-top:2px; line-height:1.4; overflow:hidden; text-overflow:ellipsis; display:-webkit-box; -webkit-line-clamp:2; -webkit-box-orient:vertical; }
  .np-imsg.solo { font-weight:600; color:var(--text-1); margin-top:0; font-size:11px; }
  .np-itime { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; margin-top:4px; }

  .np-x { width:16px; height:16px; display:flex; align-items:center; justify-content:center; flex-shrink:0; cursor:pointer; color:var(--text-3); border-radius:4px; transition:color .15s; margin-top:1px; }
  .np-x:hover { color:var(--red); }
  .np-x svg { width:10px; height:10px; }
  .np-cancel { color:var(--text-2); }
  .np-cancel:hover { color:var(--red); }

  .np-empty { text-align:center; padding:32px; color:var(--text-3); font-size:11px; }

  .tab-badge { display:inline-flex; align-items:center; justify-content:center; background:var(--accent); color:#fff; font-size:8px; font-weight:700; border-radius:6px; padding:0 4px; min-width:14px; height:14px; margin-left:3px; vertical-align:middle; }

  .task-item { display:flex; align-items:flex-start; gap:10px; padding:11px 14px; border-left:2px solid transparent; transition:background .12s; }
  .task-item + .task-item { border-top:1px solid var(--border); }
  .task-item:hover { background:var(--ibtn-bg); }
  .task-done { border-left-color:var(--green); }
  .task-error { border-left-color:var(--red); }
  .task-ico { width:24px; height:24px; border-radius:6px; display:flex; align-items:center; justify-content:center; flex-shrink:0; margin-top:1px; }
  .task-ico svg { width:11px; height:11px; fill:none; stroke-width:2.5; stroke-linecap:round; }
  .t-success { background:rgba(74,222,128,0.12); } .t-success svg { stroke:var(--green); }
  .t-error   { background:rgba(248,113,113,0.12); } .t-error svg { stroke:var(--red); }
  .t-info    { background:rgba(96,165,250,0.12); } .t-info svg { stroke:var(--blue); }
  .task-body { flex:1; min-width:0; }
  .task-name { font-size:11px; font-weight:600; color:var(--text-1); overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .task-track { height:3px; background:var(--border); border-radius:2px; overflow:hidden; margin-top:5px; }
  .task-fill { height:100%; background:var(--blue); border-radius:2px; transition:width .3s ease; }
  .task-pct { font-size:9px; color:var(--text-3); font-family:"DM Mono",monospace; margin-top:3px; }
  .task-meta { font-size:10px; margin-top:2px; }
</style>
