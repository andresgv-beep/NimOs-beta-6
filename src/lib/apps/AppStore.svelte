<script>
  import { onMount } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  const CATALOG_URL = 'https://raw.githubusercontent.com/andresgv-beep/NimOs-appstore/main/catalog.json';
  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  let catalog = null;
  let installed = {};
  let loading = true;
  let error = null;
  let activeCategory = 'all';
  let search = '';
  let selectedApp = null;
  let installing = {};
  let removing = {};

  // Docker state
  let dockerReady = false;
  let dockerInstalling = false;
  let dockerError = null;
  let hasPool = false;
  let dockerPath = '';

  function generatePassword() {
    const chars = 'abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789';
    return Array.from({ length: 24 }, () => chars[Math.floor(Math.random() * chars.length)]).join('');
  }

  async function checkDocker() {
    try {
      const poolRes = await fetch('/api/storage/status', { headers: hdrs() });
      const poolData = await poolRes.json();
      const pools = poolData.pools || [];
      hasPool = poolData.hasPool === true || pools.length > 0;

      if (!hasPool) { loading = false; return; }

      // Find primary pool or first pool — Docker will install here
      const primaryPool = pools.find(p => p.isPrimary) || pools[0];
      const poolName = primaryPool?.name || '';

      const res = await fetch('/api/docker/status', { headers: hdrs() });
      if (!res.ok && res.status === 403) {
        // Non-admin without Docker permission — just try to load catalog
        dockerReady = true;
        await loadCatalog();
        return;
      }
      const data = await res.json();
      dockerPath = data.path || '';

      if (data.installed && data.dockerRunning) {
        dockerReady = true;
        await loadCatalog();
      } else {
        await installDocker(poolName);
      }
    } catch(e) {
      console.error('[AppStore] Docker check failed:', e);
      dockerReady = true;
      await loadCatalog();
    }
  }

  async function installDocker(poolName = '') {
    dockerInstalling = true;
    dockerError = null;
    try {
      const res = await fetch('/api/docker/install', {
        method: 'POST',
        headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ pool: poolName }),
      });
      const data = await res.json();
      if (data.ok && data.dockerAvailable) {
        dockerPath = data.path || dockerPath;
        dockerReady = true;
        await loadCatalog();
      } else {
        dockerError = data.error || 'Docker installation failed';
        loading = false;
      }
    } catch(e) {
      dockerError = 'Failed to install Docker: ' + e.message;
      loading = false;
    }
    dockerInstalling = false;
  }

  async function loadCatalog() {
    try {
      const [catRes, appsRes] = await Promise.all([
        fetch(CATALOG_URL),
        fetch('/api/docker/installed-apps', { headers: hdrs() }).catch(() => ({ ok: false })),
      ]);
      catalog = await catRes.json();
      if (appsRes.ok) {
        const data = await appsRes.json();
        const apps = data.apps || [];
        for (const app of apps) {
          const id = app.id || app.name;
          if (catalog.apps[id]) {
            installed[id] = { status: 'running', port: catalog.apps[id].port };
          }
        }
        installed = installed;
      }
    } catch(e) { error = 'No se pudo cargar el catálogo'; console.error(e); }
    loading = false;
  }

  async function installApp(appId) {
    const app = catalog.apps[appId];
    if (!app) return;
    if (!app.compose) { alert('Esta app no tiene configuración de instalación'); return; }
    installing = { ...installing, [appId]: true };
    try {
      // Process env variables — same as beta 4
      const configPath = `${dockerPath}/containers/${appId}`;
      const processedEnv = {
        CONFIG_PATH: configPath,
        TZ: Intl.DateTimeFormat().resolvedOptions().timeZone || 'Europe/Madrid',
        HOST_IP: window.location.hostname,
        DOWNLOADS_PATH: `${configPath}/downloads`,
        MEDIA_PATH: `${configPath}/media`,
        DATA_PATH: `${configPath}/data`,
        MUSIC_PATH: `${configPath}/music`,
        PROJECTS_PATH: `${configPath}/projects`,
        UPLOAD_LOCATION: `${configPath}/upload`,
        DB_DATA_LOCATION: `${configPath}/postgres`,
      };

      // Add app-specific env (overrides defaults)
      if (app.env) {
        for (const [key, value] of Object.entries(app.env)) {
          let v = String(value);
          if (v === '{RANDOM}') v = generatePassword();
          v = v.replace(/\$\{CONFIG_PATH\}/g, configPath);
          processedEnv[key] = v;
        }
      }

      const res = await fetch('/api/docker/stack', {
        method: 'POST',
        headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({
          id: appId,
          name: app.name,
          compose: app.compose,
          env: processedEnv,
          icon: app.icon,
          color: app.color || '#607D8B',
          port: app.port,
          external: app.external || false,
        }),
      });
      const d = await res.json();
      if (d.ok) installed = { ...installed, [appId]: { status: 'running', port: app.port } };
      else alert(d.error || 'Installation failed');
    } catch(e) { console.error(e); }
    installing = { ...installing, [appId]: false };
  }

  async function removeApp(appId) {
    if (!confirm(`¿Eliminar ${catalog.apps[appId]?.name}?`)) return;
    removing = { ...removing, [appId]: true };
    try {
      await fetch(`/api/docker/stack/${appId}`, { method: 'DELETE', headers: hdrs() });
      const next = { ...installed }; delete next[appId]; installed = next;
    } catch(e) { console.error(e); }
    removing = { ...removing, [appId]: false };
  }

  onMount(checkDocker);

  const CAT_ICONS = { media:'🎬', cloud:'☁', downloads:'⬇', homelab:'🏠', development:'⌨', security:'🔒', monitoring:'📊' };

  $: categories = catalog ? [
    { id: 'all', label: 'Todas', icon: '⊟' },
    ...Object.entries(catalog.categories).map(([id, label]) => ({ id, label, icon: CAT_ICONS[id] || '●' }))
  ] : [];

  $: filteredApps = catalog ? Object.entries(catalog.apps)
    .filter(([id, app]) => {
      if (activeCategory === '_installed') return !!installed[id];
      const matchCat = activeCategory === 'all' || app.category === activeCategory;
      const q = search.toLowerCase();
      const matchSearch = !q || app.name.toLowerCase().includes(q) || app.description.toLowerCase().includes(q);
      return matchCat && matchSearch;
    })
    .map(([id, app]) => ({ id, ...app }))
  : [];

  $: installedCount = Object.keys(installed).length;

  // Auto-screenshot: tries screenshots/{id}.png, falls back to icon on error
  const SCREENSHOTS_BASE = 'https://raw.githubusercontent.com/andresgv-beep/NimOs-appstore/main/screenshots';
  let failedScreenshots = new Set();
  function screenshotUrl(id) { return `${SCREENSHOTS_BASE}/${id}/preview.png`; }
  function onScreenshotError(id) { failedScreenshots.add(id); failedScreenshots = failedScreenshots; }
  function hasScreenshot(id) { return !failedScreenshots.has(id); }
</script>

<div class="store-root">
  {#if !hasPool && !loading}
    <!-- No storage pool -->
    <div class="setup-screen">
      <div class="setup-icon">💾</div>
      <h2>Storage Required</h2>
      <p>The App Store needs a storage pool to install Docker apps.<br>Open <strong>NimSettings → Storage</strong> to create one.</p>
    </div>
  {:else if dockerInstalling}
    <!-- Installing Docker -->
    <div class="setup-screen">
      <div class="spinner"></div>
      <h2>Installing Docker...</h2>
      <p>This may take a few minutes. Please wait.</p>
    </div>
  {:else if dockerError}
    <!-- Docker install failed -->
    <div class="setup-screen">
      <div class="setup-icon">⚠️</div>
      <h2>Docker Installation Failed</h2>
      <p>{dockerError}</p>
      <button class="btn-retry" on:click={installDocker}>Retry</button>
    </div>
  {:else if !dockerReady && !loading}
    <!-- Docker not ready -->
    <div class="setup-screen">
      <div class="setup-icon">🐳</div>
      <h2>Setting up Docker...</h2>
      <p>Preparing the container engine.</p>
    </div>
  {:else}
  <div class="sidebar">
    <div class="sb-header">
      <svg class="sb-logo" width="20" height="20" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <rect x="2" y="3" width="20" height="14" rx="2"/><path d="M8 21h8M12 17v4"/>
      </svg>
      <div>
        <div class="sb-title">AppStore</div>
        <div class="sb-sub">Docker apps</div>
      </div>
    </div>
    <input class="sb-search" type="text" placeholder="⌕  Buscar..." bind:value={search} />
    <div class="sb-section">Categorías</div>
    {#each categories as cat}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="sb-item" class:active={activeCategory === cat.id}
        on:click={() => { activeCategory = cat.id; selectedApp = null; }}>
        <span class="sb-ico">{cat.icon}</span>{cat.label}
      </div>
    {/each}
    {#if installedCount > 0}
      <div class="sb-section" style="margin-top:8px">Mis apps</div>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="sb-item" class:active={activeCategory === '_installed'}
        on:click={() => { activeCategory = '_installed'; selectedApp = null; }}>
        <span class="sb-ico">✓</span>Instaladas
        <span class="sb-badge">{installedCount}</span>
      </div>
    {/if}
  </div>

  <div class="inner-wrap">
    <div class="inner">
      <div class="inner-titlebar">
        <div class="tb-title">AppStore</div>
        <div class="tb-sub">— {categories.find(c => c.id === activeCategory)?.label || 'Instaladas'}</div>
        {#if !loading && catalog}<div class="tb-count">{filteredApps.length} apps</div>{/if}
      </div>
      <div class="store-body">
        {#if loading}
          <div class="store-empty"><div class="spinner"></div><span>Cargando catálogo...</span></div>
        {:else if error}
          <div class="store-empty">
            <div style="font-size:32px;opacity:.3">⚠</div>
            <span>{error}</span>
            <button class="btn-secondary" on:click={loadCatalog}>Reintentar</button>
          </div>
        {:else if filteredApps.length === 0}
          <div class="store-empty"><div style="font-size:32px;opacity:.3">⊘</div><span>No hay apps</span></div>
        {:else}
          <div class="app-grid">
            {#each filteredApps as app, i}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="app-card" class:is-installed={!!installed[app.id]}
                style="animation-delay:{i * 0.025}s"
                on:click={() => selectedApp = app}>
                <div class="card-media">
                  {#if hasScreenshot(app.id)}
                    <img class="card-screenshot" src={screenshotUrl(app.id)} alt={app.name}
                      on:error={() => onScreenshotError(app.id)} />
                    <div class="card-screenshot-fade"></div>
                  {:else}
                    <div class="card-no-screenshot">
                      <div class="card-icon-blur-bg" style="background-image:url({app.icon})"></div>
                      <img class="card-icon-center" src={app.icon} alt="" />
                    </div>
                  {/if}
                  {#if installed[app.id]}
                    <div class="card-inst-pill"><div class="inst-dot"></div>Instalada</div>
                  {/if}
                  {#if app.official}
                    <div class="card-official">✓</div>
                  {/if}
                </div>
                <div class="card-body">
                  <div class="card-row">
                    <img class="card-icon" src={app.icon} alt={app.name} on:error={(e) => e.target.style.opacity='0'} />
                    <div>
                      <div class="card-name">{app.name}</div>
                      <div class="card-cat">{catalog.categories[app.category] || app.category}</div>
                    </div>
                  </div>
                  <div class="card-desc">{app.description}</div>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      </div>
      <div class="statusbar">
        <div class="status-dot"></div>
        <span>nimbusos-appstore</span>
        <div class="status-sep"></div>
        <span>{catalog ? `v${catalog.version} · ${catalog.updated}` : '—'}</span>
        <span style="margin-left:auto">{installedCount} instaladas</span>
      </div>
    </div>
  </div>
  {/if}
</div>

{#if selectedApp}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-overlay" on:click|self={() => selectedApp = null}></div>
  <div class="modal">
    <div class="modal-hero">
      {#if hasScreenshot(selectedApp.id)}
        <img class="modal-hero-img" src={screenshotUrl(selectedApp.id)} alt={selectedApp.name}
          on:error={() => onScreenshotError(selectedApp.id)} />
        <div class="modal-hero-fade"></div>
      {:else}
        <div class="modal-hero-empty">
          <div class="modal-hero-bg" style="background-image:url({selectedApp.icon})"></div>
          <img class="modal-hero-icon" src={selectedApp.icon} alt={selectedApp.name} />
        </div>
      {/if}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="modal-close" on:click={() => selectedApp = null}>✕</div>
    </div>
    <div class="modal-body">
      <div class="modal-header">
        <img class="modal-icon" src={selectedApp.icon} alt={selectedApp.name} on:error={(e) => e.target.style.opacity='0'} />
        <div class="modal-info">
          <div class="modal-name">{selectedApp.name}</div>
          <div class="modal-cat">{catalog.categories[selectedApp.category] || selectedApp.category}</div>
          <div class="modal-tags">
            {#if selectedApp.port}<span class="tag">Puerto {selectedApp.port}</span>{/if}
            {#if selectedApp.official}<span class="tag accent">Oficial</span>{/if}
            {#if selectedApp.isStack}<span class="tag">Multi-servicio</span>{/if}
          </div>
        </div>
        <div class="modal-cta">
          {#if installed[selectedApp.id]}
            <div class="modal-inst-row"><div class="inst-dot"></div><span>Instalada</span></div>
            {#if selectedApp.port}
              <a class="btn-open" href="http://localhost:{selectedApp.port}" target="_blank">Abrir →</a>
            {/if}
            <button class="btn-danger"
              on:click={() => { removeApp(selectedApp.id); selectedApp = null; }}
              disabled={removing[selectedApp.id]}>
              {removing[selectedApp.id] ? 'Eliminando...' : 'Eliminar'}
            </button>
          {:else}
            <button class="btn-install"
              on:click={() => { installApp(selectedApp.id); selectedApp = null; }}
              disabled={installing[selectedApp.id]}>
              {installing[selectedApp.id] ? 'Instalando...' : '↓ Instalar'}
            </button>
          {/if}
        </div>
      </div>
      <p class="modal-desc">{selectedApp.description}</p>
      {#if selectedApp.image}
        <div class="modal-field-row">
          <span class="mf-label">Imagen Docker</span>
          <span class="mf-value">{selectedApp.image}</span>
        </div>
      {/if}
      {#if selectedApp.credentials}
        <div class="modal-section-label">Credenciales por defecto</div>
        {#if selectedApp.credentials.username}
          <div class="modal-field-row"><span class="mf-label">Usuario</span><span class="mf-value">{selectedApp.credentials.username}</span></div>
        {/if}
        {#if selectedApp.credentials.password}
          <div class="modal-field-row"><span class="mf-label">Contraseña</span><span class="mf-value">{selectedApp.credentials.password}</span></div>
        {/if}
      {/if}
    </div>
  </div>
{/if}

<style>
  .store-root{width:100%;height:100%;display:flex;overflow:hidden;font-family:'DM Sans',sans-serif;color:var(--text-1)}
  .sidebar{width:186px;flex-shrink:0;display:flex;flex-direction:column;padding:12px 8px;background:var(--bg-sidebar)}
  .sb-header{display:flex;align-items:center;gap:9px;padding:14px 8px 14px}
  .sb-logo{color:var(--accent);flex-shrink:0}
  .sb-title{font-size:13px;font-weight:700;color:var(--text-1)}
  .sb-sub{font-size:9px;color:var(--text-3);text-transform:uppercase;letter-spacing:.04em}
  .sb-search{width:100%;padding:5px 10px;border-radius:8px;margin-bottom:10px;border:1px solid var(--border);background:var(--ibtn-bg);color:var(--text-1);font-size:11px;font-family:inherit;outline:none;transition:border-color .15s}
  .sb-search:focus{border-color:var(--border-hi)}
  .sb-search::placeholder{color:var(--text-3)}
  .sb-section{font-size:9px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.08em;padding:0 10px 4px;margin-top:4px}
  .sb-item{display:flex;align-items:center;gap:7px;padding:7px 10px;border-radius:8px;cursor:pointer;font-size:12px;color:var(--text-2);border:1px solid transparent;transition:all .15s}
  .sb-item:hover{background:rgba(128,128,128,0.10);color:var(--text-1)}
  .sb-item.active{background:var(--active-bg);color:var(--text-1);border-color:var(--border-hi)}
  .sb-ico{font-size:12px;width:16px;text-align:center;flex-shrink:0}
  .sb-badge{margin-left:auto;font-size:9px;font-weight:700;font-family:'DM Mono',monospace;padding:1px 6px;border-radius:8px;background:rgba(74,222,128,0.10);border:1px solid rgba(74,222,128,0.25);color:var(--green)}
  .inner-wrap{flex:1;padding:8px;display:flex}
  .inner{flex:1;border-radius:10px;border:1px solid var(--border);background:var(--bg-inner);display:flex;flex-direction:column;overflow:hidden}
  .inner-titlebar{display:flex;align-items:center;gap:8px;padding:10px 16px 9px;background:var(--bg-bar);flex-shrink:0}
  .tb-title{font-size:13px;font-weight:600;color:var(--text-1)}
  .tb-sub{font-size:11px;color:var(--text-3)}
  .tb-count{margin-left:auto;font-size:10px;color:var(--text-3);font-family:'DM Mono',monospace}
  .store-body{flex:1;overflow-y:auto;padding:14px}
  .store-body::-webkit-scrollbar{width:3px}
  .store-body::-webkit-scrollbar-thumb{background:rgba(128,128,128,0.15);border-radius:2px}
  .store-empty{height:100%;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:10px;color:var(--text-3);font-size:12px}
  .spinner{width:24px;height:24px;border-radius:50%;border:2px solid var(--border);border-top-color:var(--accent);animation:spin .7s linear infinite}
  @keyframes spin{to{transform:rotate(360deg)}}
  .app-grid{display:grid;grid-template-columns:repeat(auto-fill,minmax(175px,1fr));gap:12px}
  .app-card{border-radius:12px;overflow:hidden;cursor:pointer;border:1px solid var(--border);background:var(--ibtn-bg);transition:all .2s;animation:fadeUp .3s ease both;display:flex;flex-direction:column}
  .app-card:hover{transform:translateY(-2px);border-color:var(--border-hi);box-shadow:0 10px 28px rgba(0,0,0,0.22)}
  .app-card.is-installed{border-color:rgba(74,222,128,0.35)}
  @keyframes fadeUp{from{opacity:0;transform:translateY(8px)}to{opacity:1;transform:none}}
  .card-media{height:112px;position:relative;overflow:hidden;flex-shrink:0;background:var(--bg-frame)}
  .card-screenshot{width:100%;height:100%;object-fit:cover;display:block}
  .card-screenshot-fade{position:absolute;inset:0;background:linear-gradient(180deg,transparent 55%,rgba(0,0,0,0.45) 100%)}
  .card-no-screenshot{width:100%;height:100%;display:flex;align-items:center;justify-content:center;position:relative;overflow:hidden}
  .card-icon-blur-bg{position:absolute;inset:-10px;background-size:cover;background-position:center;filter:blur(20px) saturate(1.5);opacity:.2;transform:scale(1.1)}
  .card-icon-center{width:48px;height:48px;object-fit:contain;position:relative;z-index:1;filter:drop-shadow(0 4px 10px rgba(0,0,0,0.3))}
  .card-inst-pill{position:absolute;top:7px;left:7px;display:flex;align-items:center;gap:4px;padding:3px 8px;border-radius:20px;font-size:9px;font-weight:600;background:rgba(0,0,0,0.55);backdrop-filter:blur(6px);color:#4ade80}
  .inst-dot{width:5px;height:5px;border-radius:50%;background:var(--green);box-shadow:0 0 5px rgba(74,222,128,0.9);flex-shrink:0}
  .card-official{position:absolute;top:7px;right:7px;width:18px;height:18px;border-radius:50%;background:rgba(124,111,255,0.85);backdrop-filter:blur(4px);display:flex;align-items:center;justify-content:center;font-size:8px;font-weight:700;color:#fff}
  .card-body{padding:10px 12px 12px;display:flex;flex-direction:column;gap:6px}
  .card-row{display:flex;align-items:center;gap:8px}
  .card-icon{width:28px;height:28px;border-radius:7px;object-fit:contain;flex-shrink:0}
  .card-name{font-size:12px;font-weight:600;color:var(--text-1)}
  .card-cat{font-size:9px;color:var(--text-3)}
  .card-desc{font-size:10px;color:var(--text-3);line-height:1.4;display:-webkit-box;-webkit-line-clamp:2;-webkit-box-orient:vertical;overflow:hidden}
  .modal-overlay{position:fixed;inset:0;z-index:200;background:rgba(0,0,0,0.65);backdrop-filter:blur(6px)}
  .modal{position:fixed;top:50%;left:50%;transform:translate(-50%,-50%);z-index:201;width:520px;max-width:92%;max-height:88vh;background:var(--bg-inner);border-radius:16px;border:1px solid var(--border);box-shadow:0 40px 100px rgba(0,0,0,0.65);overflow:hidden;display:flex;flex-direction:column;animation:modalIn .22s cubic-bezier(0.16,1,0.3,1) both}
  @keyframes modalIn{from{opacity:0;transform:translate(-50%,-47%) scale(0.96)}to{opacity:1;transform:translate(-50%,-50%) scale(1)}}
  .modal-hero{height:210px;position:relative;overflow:hidden;flex-shrink:0;background:var(--bg-frame)}
  .modal-hero-img{width:100%;height:100%;object-fit:cover;display:block}
  .modal-hero-fade{position:absolute;inset:0;background:linear-gradient(180deg,rgba(0,0,0,0.05) 0%,rgba(0,0,0,0.55) 100%)}
  .modal-hero-empty{width:100%;height:100%;display:flex;align-items:center;justify-content:center;position:relative;overflow:hidden}
  .modal-hero-bg{position:absolute;inset:-20px;background-size:cover;background-position:center;filter:blur(30px) saturate(1.4);opacity:.3;transform:scale(1.1)}
  .modal-hero-icon{width:72px;height:72px;object-fit:contain;position:relative;z-index:1;filter:drop-shadow(0 8px 20px rgba(0,0,0,0.4))}
  .modal-close{position:absolute;top:10px;right:10px;width:28px;height:28px;border-radius:50%;background:rgba(0,0,0,0.50);backdrop-filter:blur(4px);display:flex;align-items:center;justify-content:center;color:rgba(255,255,255,0.85);font-size:11px;cursor:pointer;transition:all .15s;z-index:2}
  .modal-close:hover{background:rgba(0,0,0,0.80);color:#fff}
  .modal-body{padding:18px 20px 22px;overflow-y:auto;display:flex;flex-direction:column;gap:12px}
  .modal-body::-webkit-scrollbar{width:3px}
  .modal-body::-webkit-scrollbar-thumb{background:rgba(128,128,128,0.15);border-radius:2px}
  .modal-header{display:flex;align-items:flex-start;gap:14px}
  .modal-icon{width:56px;height:56px;border-radius:13px;object-fit:contain;flex-shrink:0}
  .modal-info{flex:1;min-width:0}
  .modal-name{font-size:20px;font-weight:700;color:var(--text-1)}
  .modal-cat{font-size:11px;color:var(--text-3);margin-top:2px;margin-bottom:7px}
  .modal-tags{display:flex;gap:5px;flex-wrap:wrap}
  .tag{font-size:9px;font-weight:600;padding:2px 7px;border-radius:4px;background:var(--ibtn-bg);border:1px solid var(--border);color:var(--text-2);font-family:'DM Mono',monospace}
  .tag.accent{background:rgba(124,111,255,0.12);border-color:rgba(124,111,255,0.30);color:var(--accent)}
  .modal-cta{display:flex;flex-direction:column;gap:6px;align-items:flex-end;flex-shrink:0}
  .modal-inst-row{display:flex;align-items:center;gap:5px;font-size:11px;font-weight:600;color:var(--green)}
  .modal-desc{font-size:12px;color:var(--text-2);line-height:1.65}
  .modal-field-row{display:flex;align-items:baseline;justify-content:space-between;gap:12px;padding:7px 0;border-bottom:1px solid var(--border)}
  .mf-label{font-size:10px;color:var(--text-3);flex-shrink:0}
  .mf-value{font-size:10px;color:var(--text-1);font-family:'DM Mono',monospace;word-break:break-all;text-align:right}
  .modal-section-label{font-size:9px;font-weight:600;color:var(--text-3);text-transform:uppercase;letter-spacing:.08em;margin-top:4px}
  .btn-install{padding:10px 22px;border-radius:9px;border:none;cursor:pointer;background:linear-gradient(135deg,var(--accent),var(--accent2));color:#fff;font-size:13px;font-weight:600;font-family:inherit;transition:opacity .15s}
  .btn-install:hover{opacity:.88}
  .btn-install:disabled{opacity:.45;cursor:not-allowed}
  .btn-open{padding:7px 14px;border-radius:7px;font-size:11px;font-weight:600;background:var(--ibtn-bg);border:1px solid var(--border);color:var(--text-1);text-decoration:none;text-align:center;transition:all .15s}
  .btn-open:hover{border-color:var(--border-hi)}
  .btn-danger{padding:7px 14px;border-radius:7px;font-size:11px;font-weight:600;background:rgba(248,113,113,0.10);border:1px solid rgba(248,113,113,0.25);color:var(--red);cursor:pointer;font-family:inherit;transition:opacity .15s}
  .btn-danger:hover{opacity:.8}
  .btn-danger:disabled{opacity:.4;cursor:not-allowed}
  .btn-secondary{padding:7px 14px;border-radius:7px;border:1px solid var(--border);background:var(--ibtn-bg);color:var(--text-2);font-size:11px;cursor:pointer;font-family:inherit}
  .statusbar{display:flex;align-items:center;gap:10px;padding:7px 16px;border-top:1px solid var(--border);background:var(--bg-bar);flex-shrink:0;font-size:10px;color:var(--text-3);border-radius:0 0 10px 10px;font-family:'DM Mono',monospace}
  .status-dot{width:6px;height:6px;border-radius:50%;background:var(--green);box-shadow:0 0 4px rgba(74,222,128,0.6)}
  .status-sep{width:1px;height:10px;background:var(--border)}
  .setup-screen{width:100%;height:100%;display:flex;flex-direction:column;align-items:center;justify-content:center;gap:14px;text-align:center;padding:40px}
  .setup-screen h2{font-size:18px;font-weight:600;color:var(--text-1)}
  .setup-screen p{font-size:13px;color:var(--text-2);line-height:1.6;max-width:400px}
  .setup-icon{font-size:48px}
  .btn-retry{padding:10px 22px;border-radius:9px;border:none;cursor:pointer;background:var(--accent);color:#fff;font-size:13px;font-weight:600;font-family:inherit}
</style>
