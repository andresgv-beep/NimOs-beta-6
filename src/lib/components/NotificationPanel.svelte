<script>
  import { notifications, unreadCount, dismissNotification, clearCategory, markAllRead } from '$lib/stores/notifications.js';

  export let open = false;

  let activeTab = 'notification'; // 'notification' | 'system'

  $: general = $notifications.filter(n => n.category === 'notification');
  $: system  = $notifications.filter(n => n.category === 'system');
  $: current = activeTab === 'notification' ? general : system;

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
    clearCategory(activeTab);
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
      <span class="np-tab" class:on={activeTab === 'system'} on:click={() => activeTab = 'system'}>Sistema</span>
    </div>

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
    background: var(--taskbar-bg, rgba(17,16,40,0.82));
    backdrop-filter: blur(20px) saturate(1.4);
    -webkit-backdrop-filter: blur(20px) saturate(1.4);
    border: 1px solid var(--taskbar-border, rgba(255,255,255,0.08));
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
  .t-info    { --tc:var(--accent);} .t-info .np-ico    { background:rgba(124,111,255,0.12); } .t-info .np-ico svg    { stroke:var(--accent); }
  .t-security{ --tc:var(--red);   } .t-security .np-ico{ background:rgba(248,113,113,0.12); } .t-security .np-ico svg{ stroke:var(--red); }

  .np-body { flex:1; min-width:0; }
  .np-ititle { font-size:11px; font-weight:700; color:var(--text-1); }
  .np-imsg { font-size:10px; color:var(--text-2); margin-top:2px; line-height:1.4; overflow:hidden; text-overflow:ellipsis; display:-webkit-box; -webkit-line-clamp:2; -webkit-box-orient:vertical; }
  .np-imsg.solo { font-weight:600; color:var(--text-1); margin-top:0; font-size:11px; }
  .np-itime { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; margin-top:4px; }

  .np-x { width:16px; height:16px; display:flex; align-items:center; justify-content:center; flex-shrink:0; cursor:pointer; color:var(--text-3); border-radius:4px; transition:color .15s; margin-top:1px; }
  .np-x:hover { color:var(--red); }
  .np-x svg { width:10px; height:10px; }

  .np-empty { text-align:center; padding:32px; color:var(--text-3); font-size:11px; }
</style>
