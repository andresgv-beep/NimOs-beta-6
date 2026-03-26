<script>
  import { onMount } from 'svelte';
  import { APP_META } from '$lib/apps.js';
  import { openWindow } from '$lib/stores/windows.js';
  import { getToken, user } from '$lib/stores/auth.js';

  export let visible = false;

  const systemApps = Object.entries(APP_META).map(([id, meta]) => ({ id, ...meta, isSystem: true }));
  let dockerApps = [];
  let allowedApps = null; // null = not loaded yet, 'all' = admin, string[] = user apps

  $: if (visible) { loadDockerApps(); loadMyApps(); }

  async function loadMyApps() {
    try {
      const res = await fetch('/api/my-apps', {
        headers: { 'Authorization': `Bearer ${getToken()}` }
      });
      const data = await res.json();
      allowedApps = data.apps; // 'all' for admin, string[] for users
    } catch { allowedApps = 'all'; }
  }

  async function loadDockerApps() {
    try {
      const res = await fetch('/api/docker/installed-apps', {
        headers: { 'Authorization': `Bearer ${getToken()}` }
      });
      const data = await res.json();
      if (data.apps && Array.isArray(data.apps)) {
        dockerApps = data.apps.map(app => ({
          id: app.id,
          name: app.name,
          icon: app.icon || '📦',
          color: app.color || '#607D8B',
          port: app.port,
          isWebApp: true,
          external: app.external || false,
        }));
      }
    } catch {}
  }

  function canAccess(appId) {
    if (allowedApps === 'all') return true;
    if (Array.isArray(allowedApps)) return allowedApps.includes(appId);
    return true; // Loading, show all temporarily
  }

  $: allApps = (() => {
    const seen = new Set();
    return [...systemApps, ...dockerApps].filter(app => {
      if (seen.has(app.id)) return false;
      seen.add(app.id);
      return canAccess(app.id);
    });
  })();

  function launch(app) {
    visible = false;
    if (app.isWebApp) {
      if (app.external) {
        window.open(`http://${window.location.hostname}:${app.port}`, '_blank');
        return;
      }
      openWindow(app.id, { width: 1100, height: 700 }, {
        isWebApp: true,
        port: app.port,
        appName: app.name,
      });
    } else {
      const meta = APP_META[app.id];
      openWindow(app.id, { width: meta?.width || 800, height: meta?.height || 520 });
    }
  }

  function isIconUrl(icon) {
    return icon && (icon.startsWith('http') || icon.startsWith('/'));
  }
</script>

{#if visible}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="overlay" on:click={() => visible = false}>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="launcher" on:click|stopPropagation>
      <div class="launcher-title">Apps</div>
      <div class="app-grid">
        {#each allApps as app, i}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="app-item" on:click={() => launch(app)} style="animation-delay:{i * 0.03}s">
            <div class="app-icon">
              {#if isIconUrl(app.icon)}
                <img src={app.icon} alt={app.name} class="icon-img" on:error={(e) => e.target.style.display='none'} />
              {:else}
                <span class="icon-emoji">{app.icon}</span>
              {/if}
            </div>
            <div class="app-name">{app.name}</div>
            {#if app.isWebApp}
              <div class="web-dot"></div>
            {/if}
          </div>
        {/each}
      </div>
    </div>
  </div>
{/if}

<svelte:window on:keydown={(e) => { if (e.key === 'Escape' && visible) visible = false; }} />

<style>
  .overlay {
    position: fixed; inset: 0; z-index: 8500;
    background: rgba(0,0,0,0.25);
    backdrop-filter: blur(4px);
  }
  .launcher {
    position: fixed;
    bottom: calc(var(--taskbar-height, 48px) + 12px);
    left: 50%;
    transform: translateX(-50%);
    width: 520px; max-height: 70vh; overflow-y: auto;
    background: var(--bg-frame, #111028);
    border: 1px solid var(--window-border, rgba(255,255,255,0.12));
    border-radius: 16px;
    box-shadow: var(--window-shadow, 0 32px 90px rgba(0,0,0,0.60));
    padding: 20px;
    animation: launchIn 0.25s cubic-bezier(0.16,1,0.3,1) both;
  }
  .launcher::-webkit-scrollbar { width: 3px; }
  .launcher::-webkit-scrollbar-thumb { background: rgba(128,128,128,0.2); border-radius: 2px; }
  @keyframes launchIn {
    from { opacity: 0; transform: translateX(-50%) translateY(10px) scale(0.96); }
    to { opacity: 1; transform: translateX(-50%) translateY(0) scale(1); }
  }
  .launcher-title {
    font-size: 13px; font-weight: 600; color: var(--text-1);
    margin-bottom: 16px; padding: 0 4px;
    font-family: 'DM Sans', sans-serif;
  }
  .app-grid {
    display: grid; grid-template-columns: repeat(5, 1fr); gap: 6px;
  }
  .app-item {
    display: flex; flex-direction: column; align-items: center; gap: 6px;
    padding: 12px 6px 10px; border-radius: 10px;
    cursor: pointer; border: 1px solid transparent;
    transition: all 0.15s; position: relative;
    animation: fadeUp 0.3s ease both;
  }
  .app-item:hover { background: rgba(128,128,128,0.08); border-color: var(--border); }
  @keyframes fadeUp {
    from { opacity: 0; transform: translateY(6px); }
    to { opacity: 1; transform: translateY(0); }
  }
  .app-icon {
    width: 44px; height: 44px;
    display: flex; align-items: center; justify-content: center;
    filter: drop-shadow(0 2px 6px rgba(0,0,0,0.4));
    transition: transform 0.15s;
  }
  .app-item:hover .app-icon { transform: scale(1.1) translateY(-2px); }
  .icon-emoji { font-size: 32px; line-height: 1; }
  .icon-img { width: 48px; height: 48px; border-radius: 12px; object-fit: contain; mix-blend-mode: screen; }
  .app-name {
    font-size: 10px; color: var(--text-2);
    text-align: center; font-family: 'DM Sans', sans-serif;
    max-width: 70px; overflow: hidden; text-overflow: ellipsis; white-space: nowrap;
  }
  .web-dot {
    position: absolute; top: 8px; right: 8px;
    width: 5px; height: 5px; border-radius: 50%;
    background: var(--accent, #7c6fff);
    box-shadow: 0 0 4px var(--accent, #7c6fff);
  }
</style>
