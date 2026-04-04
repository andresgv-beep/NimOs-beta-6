<script>
  import { prefs, pinnedApps, setPref } from '$lib/stores/theme.js';
  import { windowList, openWindow, focusWindow, restoreWindow, minimizeWindow, closeWindow } from '$lib/stores/windows.js';
  import { logout } from '$lib/stores/auth.js';
  import { APP_META } from '$lib/apps.js';
  import Launcher from './Launcher.svelte';
  import NotificationPanel from '$lib/components/NotificationPanel.svelte';
  import { unreadCount } from '$lib/stores/notifications.js';
  import { uploadTasks, activeTasks } from '$lib/stores/uploadTasks.js';

  let showLauncher = false;
  let showNotifPanel = false;
  let showTransferManager = false;

  function openTransferManager() {
    openWindow('transfermanager');
  }

  function isIconUrl(icon) { return icon && (icon.startsWith('/') || icon.startsWith('http')); }

  // ── Context menu ──
  let ctxMenu = null; // { appId, x, y, win }

  function openCtxMenu(e, appId, win = null) {
    e.preventDefault();
    e.stopPropagation();
    const menuW = 210, menuH = 250;
    const x = Math.min(e.clientX, window.innerWidth - menuW - 8);
    const y = Math.max(8, e.clientY - menuH);
    ctxMenu = { appId, x, y, win };
  }

  function closeCtxMenu() { ctxMenu = null; }

  function isPinned(appId) { return $pinnedApps.includes(appId); }

  function togglePin(appId) {
    if (isPinned(appId)) {
      setPref('pinnedApps', $pinnedApps.filter(id => id !== appId));
    } else {
      setPref('pinnedApps', [...$pinnedApps, appId]);
    }
    closeCtxMenu();
  }

  function handleAppClick(appId) {
    const meta = APP_META[appId];
    const existing = $windowList.find(w => w.appId === appId);
    if (existing) {
      if (existing.minimized) restoreWindow(existing.id);
      else focusWindow(existing.id);
    } else {
      openWindow(appId, { width: meta?.width || 800, height: meta?.height || 520 });
    }
  }

  function toggleMinimize(win) {
    if (win.minimized) restoreWindow(win.id);
    else minimizeWindow(win.id);
  }

  // clock removed
  function updateClock_DISABLED() {
    const now = new Date();
    const h = String(now.getHours()).padStart(2,'0');
    const m = String(now.getMinutes()).padStart(2,'0');
    time = `${h}:${m}`;
    date = now.toLocaleDateString('es-ES', { weekday:'short', day:'numeric', month:'short' });
  }
  updateClock();
  setInterval(updateClock, 10000);

  $: mode     = $prefs.taskbarMode     || 'classic';
  $: position = $prefs.taskbarPosition || 'bottom';
  $: size     = $prefs.taskbarSize     || 'medium';
  $: isDock   = mode === 'dock';

  // Open windows not pinned
  $: openUnpinned = $windowList.filter(w => !$pinnedApps.includes(w.appId));
</script>

<Launcher bind:visible={showLauncher} />

<div
  class="taskbar"
  class:dock={isDock}
  class:classic={!isDock}
  data-position={position}
  data-size={size}
>
  {#if !isDock}
    <!-- ── CLASSIC MODE ── -->

    <!-- Launcher -->
    <button class="tb-launcher" on:click={() => showLauncher = !showLauncher} title="Apps"
      class:active={showLauncher}>
      <svg width="16" height="16" viewBox="0 0 18 18" fill="none">
        <rect x="1" y="1" width="6" height="6" rx="1.5" fill="currentColor" opacity="0.9"/>
        <rect x="11" y="1" width="6" height="6" rx="1.5" fill="currentColor" opacity="0.65"/>
        <rect x="1" y="11" width="6" height="6" rx="1.5" fill="currentColor" opacity="0.65"/>
        <rect x="11" y="11" width="6" height="6" rx="1.5" fill="currentColor" opacity="0.4"/>
      </svg>
    </button>

    <div class="sep"></div>

    <!-- Pinned apps -->
    <div class="app-row">
      {#each $pinnedApps as appId}
        {@const meta = APP_META[appId]}
        {#if meta}
          {@const isOpen = $windowList.some(w => w.appId === appId)}
          {@const isMin  = $windowList.find(w => w.appId === appId)?.minimized}
          <button class="tb-btn" class:open={isOpen} class:minimized={isMin}
            title={meta.name}
            on:click={() => handleAppClick(appId)}
            on:contextmenu={(e) => openCtxMenu(e, appId, $windowList.find(w => w.appId === appId))}>
            {#if isIconUrl(meta.icon)}
              <img src={meta.icon} alt={meta.name} class="tb-icon-img" on:error={(e) => e.target.style.opacity='0'}/>
            {:else}
              <span class="tb-emoji">{meta.icon}</span>
            {/if}
            {#if isOpen}<div class="tb-dot"></div>{/if}
          </button>
        {/if}
      {/each}
    </div>

    {#if openUnpinned.length > 0}
      <div class="sep"></div>
      <div class="app-row">
        {#each openUnpinned as win}
          {@const meta = APP_META[win.appId]}
          <button class="tb-btn open" class:minimized={win.minimized}
            title={meta?.name || win.appId}
            on:click={() => toggleMinimize(win)}
            on:contextmenu={(e) => openCtxMenu(e, win.appId, win)}>
            {#if isIconUrl(meta?.icon)}
              <img src={meta.icon} alt={meta?.name} class="tb-icon-img" on:error={(e) => e.target.style.opacity='0'}/>
            {:else}
              <span class="tb-emoji">{meta?.icon || '📦'}</span>
            {/if}
            <div class="tb-dot"></div>
          </button>
        {/each}
      </div>
    {/if}

    <!-- Right -->
    <div class="right">
      <!-- Transfer Manager Icon -->
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="transfer-btn" on:click={openTransferManager} title="Transferencias">
        <svg width="20" height="20" viewBox="0 0 24 24" xmlns="http://www.w3.org/2000/svg">
          <path d="M9 22H15C20 22 22 20 22 15V9C22 4 20 2 15 2H9C4 2 2 4 2 9V15C2 20 4 22 9 22Z" fill="currentColor" opacity="0.9"/>
          <path d="M12.37 8.88H17.62" stroke="var(--bg-frame)" stroke-width="1.8" stroke-linecap="round"/>
          <path d="M6.38 8.88L7.13 9.63L9.38 7.38" stroke="var(--bg-frame)" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" fill="none"/>
          <path d="M12.37 15.88H17.62" stroke="var(--bg-frame)" stroke-width="1.8" stroke-linecap="round"/>
          <path d="M6.38 15.88L7.13 16.63L9.38 14.38" stroke="var(--bg-frame)" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round" fill="none"/>
        </svg>
        {#if $activeTasks.length > 0}
          <svg class="transfer-spinner" width="14" height="14" viewBox="0 0 14 14">
            <circle cx="7" cy="2" r="1.4" fill="currentColor"><animate attributeName="opacity" values="1;0.15;1" dur="1s" begin="0s" repeatCount="indefinite"/></circle>
            <circle cx="9.95" cy="2.95" r="1.2" fill="currentColor"><animate attributeName="opacity" values="1;0.15;1" dur="1s" begin="-0.875s" repeatCount="indefinite"/></circle>
            <circle cx="12" cy="7" r="1.0" fill="currentColor"><animate attributeName="opacity" values="1;0.15;1" dur="1s" begin="-0.75s" repeatCount="indefinite"/></circle>
            <circle cx="9.95" cy="11.05" r="0.9" fill="currentColor"><animate attributeName="opacity" values="1;0.15;1" dur="1s" begin="-0.625s" repeatCount="indefinite"/></circle>
            <circle cx="7" cy="12" r="0.8" fill="currentColor"><animate attributeName="opacity" values="1;0.15;1" dur="1s" begin="-0.5s" repeatCount="indefinite"/></circle>
            <circle cx="4.05" cy="11.05" r="0.7" fill="currentColor"><animate attributeName="opacity" values="1;0.15;1" dur="1s" begin="-0.375s" repeatCount="indefinite"/></circle>
            <circle cx="2" cy="7" r="0.6" fill="currentColor"><animate attributeName="opacity" values="1;0.15;1" dur="1s" begin="-0.25s" repeatCount="indefinite"/></circle>
            <circle cx="4.05" cy="2.95" r="0.5" fill="currentColor"><animate attributeName="opacity" values="1;0.15;1" dur="1s" begin="-0.125s" repeatCount="indefinite"/></circle>
          </svg>
        {/if}
      </div>
      <div class="notif-bell-wrap">
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="notif-bell" class:active={showNotifPanel} on:click={() => showNotifPanel = !showNotifPanel}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
            <path d="M18 8A6 6 0 0 0 6 8c0 7-3 9-3 9h18s-3-2-3-9"/>
            <path d="M13.73 21a2 2 0 0 1-3.46 0"/>
          </svg>
          {#if $unreadCount > 0}
            <div class="notif-badge">{$unreadCount > 9 ? '9+' : $unreadCount}</div>
          {/if}
        </div>
        <NotificationPanel bind:open={showNotifPanel} />
      </div>
      <div class="sep"></div>
      <button class="tb-btn" title="Cerrar sesión" on:click={logout}>
        <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
          <path d="M9 21H5a2 2 0 01-2-2V5a2 2 0 012-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/>
        </svg>
      </button>
    </div>

  {:else}
    <!-- ── DOCK MODE ── -->
    <div class="dock-inner">
      <!-- Launcher -->
      <button class="dock-btn launcher-btn" on:click={() => showLauncher = !showLauncher}
        title="Apps" class:active={showLauncher}>
        <svg width="18" height="18" viewBox="0 0 18 18" fill="none">
          <rect x="1" y="1" width="6" height="6" rx="1.5" fill="currentColor" opacity="0.9"/>
          <rect x="11" y="1" width="6" height="6" rx="1.5" fill="currentColor" opacity="0.65"/>
          <rect x="1" y="11" width="6" height="6" rx="1.5" fill="currentColor" opacity="0.65"/>
          <rect x="11" y="11" width="6" height="6" rx="1.5" fill="currentColor" opacity="0.4"/>
        </svg>
      </button>

      <div class="dock-sep"></div>

      <!-- Pinned -->
      {#each $pinnedApps as appId}
        {@const meta = APP_META[appId]}
        {#if meta}
          {@const isOpen = $windowList.some(w => w.appId === appId)}
          {@const isMin  = $windowList.find(w => w.appId === appId)?.minimized}
          <button class="dock-btn" class:open={isOpen} class:minimized={isMin}
            title={meta.name}
            on:click={() => handleAppClick(appId)}
            on:contextmenu={(e) => openCtxMenu(e, appId, $windowList.find(w => w.appId === appId))}>
            {#if isIconUrl(meta.icon)}
              <img src={meta.icon} alt={meta.name} class="dock-icon-img" on:error={(e) => e.target.style.opacity='0'}/>
            {:else}
              <span class="dock-emoji">{meta.icon}</span>
            {/if}
            {#if isOpen}<div class="dock-dot"></div>{/if}
          </button>
        {/if}
      {/each}

      {#if openUnpinned.length > 0}
        <div class="dock-sep"></div>
        {#each openUnpinned as win}
          {@const meta = APP_META[win.appId]}
          <button class="dock-btn open" class:minimized={win.minimized}
            title={meta?.name || win.appId}
            on:click={() => toggleMinimize(win)}
            on:contextmenu={(e) => openCtxMenu(e, win.appId, win)}>
            {#if isIconUrl(meta?.icon)}
              <img src={meta.icon} alt={meta?.name} class="dock-icon-img" on:error={(e) => e.target.style.opacity='0'}/>
            {:else}
              <span class="dock-emoji">{meta?.icon || '📦'}</span>
            {/if}
            <div class="dock-dot"></div>
          </button>
        {/each}
      {/if}
    </div>
  {/if}
</div>

<!-- ── CONTEXT MENU ── -->
{#if ctxMenu}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="ctx-overlay" on:click={closeCtxMenu} on:contextmenu|preventDefault={closeCtxMenu}></div>

  {@const ctxMeta = APP_META[ctxMenu.appId]}
  <div class="ctx-menu" style="left:{ctxMenu.x}px; top:{ctxMenu.y}px;">
    <!-- Header -->
    <div class="ctx-header">
      {#if isIconUrl(ctxMeta?.icon)}
          <img src={ctxMeta.icon} alt={ctxMeta?.name} class="ctx-icon-img"/>
        {:else}
          <span class="ctx-icon">{ctxMeta?.icon || '📦'}</span>
        {/if}
      <span class="ctx-app-name">{ctxMeta?.name || ctxMenu.appId}</span>
    </div>
    <div class="ctx-divider"></div>

    <!-- Open / Focus -->
    {#if ctxMenu.win}
      {#if ctxMenu.win.minimized}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="ctx-item" on:click={() => { restoreWindow(ctxMenu.win.id); closeCtxMenu(); }}>
          <span class="ctx-ico">◻</span> Restaurar
        </div>
      {:else}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="ctx-item" on:click={() => { focusWindow(ctxMenu.win.id); closeCtxMenu(); }}>
          <span class="ctx-ico">◈</span> Enfocar
        </div>
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="ctx-item" on:click={() => { minimizeWindow(ctxMenu.win.id); closeCtxMenu(); }}>
          <span class="ctx-ico">—</span> Minimizar
        </div>
      {/if}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="ctx-item danger" on:click={() => { closeWindow(ctxMenu.win.id); closeCtxMenu(); }}>
        <span class="ctx-ico">✕</span> Cerrar
      </div>
      <div class="ctx-divider"></div>
    {:else}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="ctx-item" on:click={() => { handleAppClick(ctxMenu.appId); closeCtxMenu(); }}>
        <span class="ctx-ico">▶</span> Abrir
      </div>
      <div class="ctx-divider"></div>
    {/if}

    <!-- Pin / Unpin -->
    {#if isPinned(ctxMenu.appId)}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="ctx-item" on:click={() => togglePin(ctxMenu.appId)}>
        <span class="ctx-ico">◉</span> Desanclar de la barra
      </div>
    {:else}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="ctx-item" on:click={() => togglePin(ctxMenu.appId)}>
        <span class="ctx-ico">◎</span> Anclar a la barra
      </div>
    {/if}
  </div>
{/if}

<style>
  /* ── CLASSIC ── */
  .taskbar.classic {
    position: fixed; left: 0; right: 0;
    height: var(--taskbar-height, 48px);
    display: flex; align-items: center;
    padding: 0 10px; gap: 2px;
    z-index: 9000;
    /* Tokens instead of hardcoded */
    background: var(--taskbar-bg, rgba(17,16,40,0.80));
    backdrop-filter: blur(20px) saturate(1.4);
    -webkit-backdrop-filter: blur(20px) saturate(1.4);
    border-color: var(--taskbar-border, rgba(255,255,255,0.07));
  }
  .taskbar.classic[data-position="bottom"] { bottom: 0; border-top: 1px solid var(--border); }
  .taskbar.classic[data-position="top"]    { top: 0;    border-bottom: 1px solid var(--border); }
  .taskbar.classic[data-position="left"]   {
    left: 0; top: 0; bottom: 0; right: auto;
    width: var(--taskbar-height, 48px); height: auto;
    flex-direction: column; padding: 10px 0;
    border-right: 1px solid var(--border);
  }

  /* Light theme taskbar */
  :global([data-theme="light"]) .taskbar.classic {
    background: rgba(225,225,232,0.88);
    border-color: rgba(0,0,0,0.10);
  }
  :global([data-theme="dark"]) .taskbar.classic {
    background: rgba(24,24,24,0.82);
    border-color: rgba(255,255,255,0.07);
  }

  .tb-launcher {
    width: 40px; height: 40px; border-radius: 8px;
    border: none; background: transparent;
    color: var(--text-2);
    display: flex; align-items: center; justify-content: center;
    cursor: pointer; transition: all .15s; flex-shrink: 0;
  }
  .tb-launcher:hover, .tb-launcher.active {
    background: var(--active-bg); color: var(--text-1);
  }

  .transfer-btn { display:flex; align-items:center; gap:5px; cursor:pointer; padding:4px 6px; border-radius:8px; color:var(--text-2); transition:color .15s, background .15s; }
  .transfer-btn:hover { background:var(--ibtn-bg); color:var(--text-1); }
  .transfer-spinner { color:var(--text-2); flex-shrink:0; }
  .notif-bell-wrap { position:relative; display:flex; align-items:center; }
  .notif-bell { width:34px; height:34px; border-radius:8px; display:flex; align-items:center; justify-content:center; cursor:pointer; position:relative; transition:background .15s; }
  .notif-bell:hover { background:var(--ibtn-bg); }
  .notif-bell.active { background:rgba(124,111,255,0.12); }
  .notif-bell svg { width:15px; height:15px; color:var(--text-2); transition:color .15s; }
  .notif-bell:hover svg, .notif-bell.active svg { color:var(--text-1); }
  .notif-badge { position:absolute; top:5px; right:5px; min-width:13px; height:13px; border-radius:7px; background:var(--red); font-size:8px; font-weight:700; color:#fff; display:flex; align-items:center; justify-content:center; padding:0 3px; }
  .sep {
    width: 1px; height: 22px;
    background: var(--border);
    margin: 0 6px; flex-shrink: 0;
  }
  .taskbar.classic[data-position="left"] .sep {
    width: 22px; height: 1px; margin: 4px 0;
  }

  .app-row { display: flex; align-items: center; gap: 1px; }
  .taskbar.classic[data-position="left"] .app-row { flex-direction: column; }

  .tb-btn {
    width: 44px; height: 44px; border-radius: 10px;
    border: none; background: transparent;
    display: flex; align-items: center; justify-content: center;
    cursor: pointer; transition: all .15s;
    position: relative; flex-shrink: 0;
    flex-direction: column; gap: 2px;
    color: var(--text-1);
  }
  .tb-btn:hover { background: var(--ibtn-bg); }
  .tb-btn.open  { background: var(--ibtn-bg); }
  .tb-btn.minimized { opacity: 0.45; }

  .tb-emoji { font-size: 23px; line-height: 1; }
  .tb-icon-img { width: 28px; height: 28px; object-fit: contain; border-radius: 7px; }
  .dock-icon-img { width: 28px; height: 28px; object-fit: contain; border-radius: 8px; }
  .ctx-icon-img { width: 16px; height: 16px; object-fit: contain; border-radius: 4px; }

  .tb-dot {
    width: 4px; height: 4px; border-radius: 50%;
    background: var(--accent);
    position: absolute; bottom: 3px;
  }

  .right {
    margin-left: auto;
    display: flex; align-items: center; gap: 2px;
  }
  .taskbar.classic[data-position="left"] .right {
    margin-left: 0; margin-top: auto; flex-direction: column;
  }

  .clock-wrap-DISABLED {
    display: flex; flex-direction: column; align-items: center;
    padding: 0 8px; gap: 1px;
  }
  .clock {
    font-size: 12px; font-weight: 600;
    font-family: 'DM Mono', monospace;
    color: var(--text-1);
    line-height: 1;
  }
  .clock-date {
    font-size: 9px; color: var(--text-3);
    font-family: 'DM Mono', monospace;
    text-transform: capitalize;
  }

  /* ── DOCK ── */
  .taskbar.dock {
    position: fixed; left: 50%; transform: translateX(-50%);
    bottom: 10px;
    height: auto; width: auto;
    background: transparent;
    border: none;
    z-index: 9000;
    display: flex; align-items: center; justify-content: center;
  }
  .taskbar.dock[data-position="top"] { bottom: auto; top: 10px; }

  .dock-inner {
    display: flex; align-items: center; gap: 4px;
    padding: 6px 10px;
    border-radius: 18px;
    background: var(--taskbar-bg, rgba(17,16,40,0.80));
    backdrop-filter: blur(24px) saturate(1.6);
    -webkit-backdrop-filter: blur(24px) saturate(1.6);
    border: 1px solid var(--border);
    box-shadow: 0 8px 32px rgba(0,0,0,0.35), 0 2px 8px rgba(0,0,0,0.2);
  }

  :global([data-theme="light"]) .dock-inner {
    background: rgba(220,220,228,0.88);
    box-shadow: 0 8px 32px rgba(0,0,0,0.15), 0 2px 8px rgba(0,0,0,0.08);
  }
  :global([data-theme="dark"]) .dock-inner {
    background: rgba(24,24,24,0.85);
  }

  .dock-btn {
    width: 42px; height: 42px; border-radius: 12px;
    border: none; background: transparent;
    display: flex; flex-direction: column; align-items: center; justify-content: center;
    cursor: pointer; transition: all .18s cubic-bezier(0.34,1.56,0.64,1);
    position: relative; gap: 2px; flex-shrink: 0;
  }
  .dock-btn:hover { transform: translateY(-2px) scale(1.06); background: var(--ibtn-bg); }
  .dock-btn.open  { background: var(--ibtn-bg); }
  .dock-btn.minimized { opacity: 0.4; }
  .dock-btn.active { background: var(--active-bg); }

  .launcher-btn:hover { background: var(--active-bg) !important; }

  .dock-emoji { font-size: 22px; line-height: 1; }

  .dock-dot {
    width: 4px; height: 4px; border-radius: 50%;
    background: var(--accent);
    position: absolute; bottom: 2px;
  }

  .dock-sep {
    width: 1px; height: 28px;
    background: var(--border);
    margin: 0 4px; flex-shrink: 0;
  }

  /* Size variants */
  .taskbar[data-size="small"]  .tb-btn,
  .taskbar[data-size="small"]  .tb-launcher { width: 34px; height: 34px; }
  .taskbar[data-size="small"]  .tb-emoji { font-size: 17px; }
  .taskbar[data-size="large"]  .tb-btn,
  .taskbar[data-size="large"]  .tb-launcher { width: 50px; height: 50px; }
  .taskbar[data-size="large"]  .tb-emoji { font-size: 26px; }

  .taskbar[data-size="small"]  .dock-btn { width: 36px; height: 36px; }
  .taskbar[data-size="small"]  .dock-emoji { font-size: 18px; }
  .taskbar[data-size="large"]  .dock-btn { width: 50px; height: 50px; }
  .taskbar[data-size="large"]  .dock-emoji { font-size: 26px; }

  /* ── CONTEXT MENU ── */
  .ctx-overlay {
    position: fixed; inset: 0; z-index: 9998;
  }
  .ctx-menu {
    position: fixed; z-index: 9999;
    min-width: 200px;
    background: var(--bg-inner);
    border: 1px solid var(--border);
    border-radius: 10px;
    box-shadow: 0 16px 40px rgba(0,0,0,0.45), 0 2px 8px rgba(0,0,0,0.2);
    overflow: hidden;
    animation: ctxIn .12s cubic-bezier(0.16,1,0.3,1) both;
  }
  @keyframes ctxIn {
    from { opacity:0; transform:translateY(6px) scale(0.97); }
    to   { opacity:1; transform:translateY(0) scale(1); }
  }
  .ctx-header {
    display: flex; align-items: center; gap: 8px;
    padding: 10px 12px 8px;
  }
  .ctx-icon { font-size: 16px; }
  .ctx-app-name { font-size: 12px; font-weight: 600; color: var(--text-1); }
  .ctx-divider { height: 1px; background: var(--border); margin: 2px 0; }
  .ctx-item {
    display: flex; align-items: center; gap: 8px;
    padding: 8px 12px; font-size: 12px; color: var(--text-2);
    cursor: pointer; transition: all .1s;
  }
  .ctx-item:hover { background: var(--active-bg); color: var(--text-1); }
  .ctx-item.danger { color: var(--red); }
  .ctx-item.danger:hover { background: rgba(248,113,113,0.10); }
  .ctx-ico { font-size: 11px; width: 14px; text-align: center; color: var(--text-3); flex-shrink: 0; }
  .ctx-item.danger .ctx-ico { color: var(--red); }
</style>
