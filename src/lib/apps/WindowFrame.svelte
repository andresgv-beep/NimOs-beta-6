<script>
  import { onMount, tick } from 'svelte';
  import { closeWindow, focusWindow, minimizeWindow, maximizeWindow, updateWindowPos, getWindowPos } from '$lib/stores/windows.js';
  import { APP_META } from '$lib/apps.js';

  export let win;

  $: meta = APP_META[win.appId] || { name: win.appId, icon: '📦' };

  // Reactive position state for the style binding
  let x = 0, y = 0, w = 800, h = 520;

  onMount(async () => {
    await tick();
    const p = getWindowPos(win.id);
    x = p.x; y = p.y; w = p.width; h = p.height;
  });

  // Drag
  let dragging = false;
  let dragOffset = { x: 0, y: 0 };

  function getZoom() {
    return parseFloat(document.documentElement.style.zoom) || 1;
  }

  function onTitleMouseDown(e) {
    if (e.target.closest('.wf-btn')) return;
    if (win.maximized) return;
    focusWindow(win.id);
    dragging = true;
    const z = getZoom();
    dragOffset = { x: e.clientX / z - x, y: e.clientY / z - y };
    window.addEventListener('mousemove', onDrag);
    window.addEventListener('mouseup', onDragEnd);
  }

  function onDrag(e) {
    if (!dragging) return;
    const z = getZoom();
    x = e.clientX / z - dragOffset.x;
    y = Math.max(0, e.clientY / z - dragOffset.y);
    updateWindowPos(win.id, { x, y });
  }

  function onDragEnd() {
    dragging = false;
    window.removeEventListener('mousemove', onDrag);
    window.removeEventListener('mouseup', onDragEnd);
  }

  // Resize
  let resizing = false;
  let resizeStart = { mx: 0, my: 0, w: 0, h: 0 };

  function onResizeMouseDown(e) {
    if (win.maximized) return;
    e.stopPropagation();
    resizing = true;
    const z = getZoom();
    resizeStart = { mx: e.clientX / z, my: e.clientY / z, w, h };
    window.addEventListener('mousemove', onResize);
    window.addEventListener('mouseup', onResizeEnd);
  }

  function onResize(e) {
    if (!resizing) return;
    const z = getZoom();
    w = Math.max(400, resizeStart.w + (e.clientX / z - resizeStart.mx));
    h = Math.max(300, resizeStart.h + (e.clientY / z - resizeStart.my));
    updateWindowPos(win.id, { width: w, height: h });
  }

  function onResizeEnd() {
    resizing = false;
    window.removeEventListener('mousemove', onResize);
    window.removeEventListener('mouseup', onResizeEnd);
  }

  // Maximize
  function doMaximize() {
    maximizeWindow(win.id);
    tick().then(() => {
      const p = getWindowPos(win.id);
      x = p.x; y = p.y; w = p.width; h = p.height;
    });
  }
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div
  class="window"
  class:maximized={win.maximized}
  class:dragging
  style="z-index:{win.zIndex}; left:{x}px; top:{y}px; width:{w}px; height:{h}px;"
  on:mousedown={() => focusWindow(win.id)}
>
  <!-- Drag zone — invisible bar at top for dragging -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="drag-zone" on:mousedown={onTitleMouseDown}></div>

  <!-- Window controls — three colored lines -->
  <div class="wf-controls">
    <button class="wf-btn close" on:click={() => closeWindow(win.id)}>
      <div class="wf-line red"></div>
    </button>
    <button class="wf-btn minimize" on:click={() => minimizeWindow(win.id)}>
      <div class="wf-line yellow"></div>
    </button>
    <button class="wf-btn maximize" on:click={doMaximize}>
      <div class="wf-line green"></div>
    </button>
  </div>

  <!-- App content — fills entire window -->
  <div class="content">
    {#if win.isWebApp && win.webAppPort}
      {#await import('$lib/apps/WebApp.svelte') then module}
        <svelte:component this={module.default} appId={win.appId} port={win.webAppPort} name={win.webAppName} />
      {/await}
    {:else if win.appId === 'files'}
      {#await import('$lib/apps/FileManager.svelte') then module}
        <svelte:component this={module.default} />
      {/await}
    {:else if win.appId === 'nimsettings'}
      {#await import('$lib/apps/Settings.svelte') then module}
        <svelte:component this={module.default} />
      {/await}
    {:else if win.appId === 'nimtorrent'}
      {#await import('$lib/apps/NimTorrent.svelte') then module}
        <svelte:component this={module.default} />
      {/await}
    {:else if win.appId === 'appstore'}
      {#await import('$lib/apps/AppStore.svelte') then module}
        <svelte:component this={module.default} />
      {/await}
    {:else}
      <div class="placeholder">
        <span style="font-size:48px">{meta.icon}</span>
        <p>{meta.name}</p>
        <small>Coming soon</small>
      </div>
    {/if}
  </div>

  {#if !win.maximized}
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="resize-handle" on:mousedown={onResizeMouseDown}></div>
  {/if}
</div>

<style>
  .window {
    position: fixed;
    border-radius: 16px;
    overflow: hidden;
    border: 1px solid var(--window-border);
    box-shadow: var(--window-shadow);
    display: flex; flex-direction: column;
    background: var(--bg-frame, #111028);
    animation: winIn 0.42s cubic-bezier(0.16,1,0.3,1) both;
  }
  .window.dragging { user-select: none; }
  .window.maximized {
    border-radius: 0 !important; border: none !important;
    box-shadow: none !important;
    left: 0 !important; top: 0 !important;
    width: 100vw !important;
    height: calc(100vh - var(--taskbar-height, 48px)) !important;
  }
  @keyframes winIn {
    from { opacity: 0; transform: scale(0.96) translateY(10px); }
    to { opacity: 1; transform: scale(1) translateY(0); }
  }

  .drag-zone {
    position: absolute; top: 0; left: 0; right: 0;
    height: 38px; z-index: 1;
    cursor: default; user-select: none;
  }

  .wf-controls {
    position: absolute; top: 10px; left: 12px;
    display: flex; gap: 8px; z-index: 10;
    opacity: 0.35;
    transition: opacity 0.2s;
  }
  .window:hover .wf-controls { opacity: 1; }
  .wf-btn {
    width: 14px; height: 14px;
    border: none; background: none; padding: 0;
    color: rgba(255,255,255,0.55);
    cursor: pointer; display: flex; align-items: center; justify-content: center;
    transition: color 0.15s;
  }
  .wf-btn:hover .wf-line { transform: scaleX(1.2); }
  .wf-line {
    width: 24px; height: 3px; border-radius: 2px;
    transition: all 0.15s;
  }
  .wf-line.red { background: #ff5f57; }
  .wf-line.yellow { background: #febc2e; }
  .wf-line.green { background: #28c840; }

  .content { flex: 1; overflow: hidden; }

  .placeholder {
    width: 100%; height: 100%;
    display: flex; flex-direction: column;
    align-items: center; justify-content: center;
    gap: 8px; color: rgba(255,255,255,0.3);
    background: var(--bg-inner, #1c1b3a);
  }
  .placeholder p { font-size: 14px; font-weight: 500; }
  .placeholder small { font-size: 11px; }

  .resize-handle {
    position: absolute; bottom: 0; right: 0;
    width: 16px; height: 16px;
    cursor: nwse-resize; z-index: 10;
  }
</style>
