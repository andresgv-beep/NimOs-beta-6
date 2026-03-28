<script>
  import { onMount, onDestroy } from 'svelte';
  import { user, appState } from '$lib/stores/auth.js';
  import { prefs, setPref } from '$lib/stores/theme.js';
  import { getToken } from '$lib/stores/auth.js';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  // ── Nav ──
  let activeTab = 'home';
  function switchTab(tab) { activeTab = tab; }

  // ── Theme ──
  let theme = 'auto'; // auto | dark | light
  $: isDark = theme === 'auto'
    ? window.matchMedia('(prefers-color-scheme: dark)').matches
    : theme === 'dark';

  // ── Data ──
  let sysData = {}, storageData = {}, netData = {}, torrentData = { torrents: [], dlSpeed: 0, ulSpeed: 0 };
  let shares = [];
  let loading = true;
  let pollTimer;

  async function fetchAll() {
    try {
      const [sys, stor, net] = await Promise.all([
        fetch('/api/system',         { headers: hdrs() }).then(r => r.json()).catch(() => ({})),
        fetch('/api/storage/status', { headers: hdrs() }).then(r => r.json()).catch(() => ({})),
        fetch('/api/network',        { headers: hdrs() }).then(r => r.json()).catch(() => ({})),
      ]);
      sysData = sys || {}; storageData = stor || {}; netData = net || {};
    } catch {}
    try {
      const d = await fetch('/api/torrent/torrents', { headers: hdrs() }).then(r => r.json());
      const raw = Array.isArray(d) ? d : (d.torrents || []);
      const list = raw.map(t => ({
        name:     t.name || '—',
        progress: (t.progress != null && t.progress <= 1) ? t.progress * 100 : (t.progress || 0),
        dlSpeed:  t.download_rate ?? t.dlSpeed ?? 0,
        ulSpeed:  t.upload_rate   ?? t.ulSpeed ?? 0,
        status:   t.status || '',
      }));
      torrentData = { torrents: list, dlSpeed: list.reduce((a,t)=>a+t.dlSpeed,0), ulSpeed: list.reduce((a,t)=>a+t.ulSpeed,0) };
    } catch {}
    try {
      const d = await fetch('/api/shares', { headers: hdrs() }).then(r => r.json());
      shares = Array.isArray(d) ? d : (d.shares || d || []);
    } catch {}
    loading = false;
  }

  onMount(() => { fetchAll(); pollTimer = setInterval(fetchAll, 5000); });
  onDestroy(() => clearInterval(pollTimer));

  // ── Computed ──
  $: cpuPct  = sysData.cpu?.percent    ?? sysData.cpuPercent ?? 0;
  $: memPct  = sysData.memory?.percent ?? sysData.memPercent ?? 0;
  $: memUsed = sysData.memory?.used    ?? 0;
  $: memTotal= sysData.memory?.total   ?? 0;
  $: pools   = storageData.pools       || [];
  $: userName = $user?.username || 'User';

  $: activeTorrents = torrentData.torrents.filter(t => t.progress < 100);
  $: doneTorrents   = torrentData.torrents.filter(t => t.progress >= 100);

  function fmtSpeed(bps) {
    if (!bps || bps <= 0) return '0 KB/s';
    if (bps >= 1e6) return (bps/1e6).toFixed(1) + ' MB/s';
    return (bps/1e3).toFixed(0) + ' KB/s';
  }
  function fmtBytes(b) {
    if (!b) return '—';
    if (b >= 1e12) return (b/1e12).toFixed(1) + ' TB';
    if (b >= 1e9)  return (b/1e9).toFixed(1) + ' GB';
    if (b >= 1e6)  return (b/1e6).toFixed(0) + ' MB';
    return (b/1e3).toFixed(0) + ' KB';
  }
  function arcColor(pct) { return pct < 60 ? '#4ade80' : pct < 80 ? '#fbbf24' : '#f87171'; }

  function logout() {
    import('$lib/stores/auth.js').then(m => m.logout?.());
    appState.set('login');
  }
</script>

<div class="mobile-root" class:dark={isDark} class:light={!isDark}>


  <!-- SCREEN -->
  <div class="screen">

    <!-- HOME -->
    {#if activeTab === 'home'}
      <div class="view">
        <div class="home-header">
          <div class="home-greeting">Bienvenido</div>
          <div class="home-title">{userName}</div>
        </div>

        <div class="section-title">Sistema</div>
        <div class="stats-grid">
          <div class="stat-card">
            <div class="stat-label">CPU</div>
            <div class="stat-val" style="color:{arcColor(cpuPct)}">{cpuPct.toFixed(0)}%</div>
            <div class="stat-bar"><div class="stat-fill" style="width:{cpuPct}%;background:{arcColor(cpuPct)}"></div></div>
            <div class="stat-sub">{sysData.cpu?.cores ?? '—'} cores · load {sysData.cpu?.load?.toFixed(2) ?? '—'}</div>
          </div>
          <div class="stat-card">
            <div class="stat-label">RAM</div>
            <div class="stat-val" style="color:#3b82f6">{memPct.toFixed(0)}%</div>
            <div class="stat-bar"><div class="stat-fill" style="width:{memPct}%;background:#3b82f6"></div></div>
            <div class="stat-sub">{fmtBytes(memUsed)} / {fmtBytes(memTotal)}</div>
          </div>
        </div>

        {#if pools.length > 0}
          <div class="section-title">Storage</div>
          {#each pools as pool}
            <div class="pool-card">
              <div class="pool-row">
                <div class="pool-name">{pool.name}</div>
                <div class="pool-type">{pool.type || pool.filesystem || '—'} · {pool.raidLevel || pool.profile || '—'}</div>
              </div>
              <div class="pool-bar">
                <div class="pool-fill" style="width:{pool.usagePercent||0}%;background:{arcColor(pool.usagePercent||0)}"></div>
              </div>
              <div class="pool-meta">{pool.usedFormatted || '—'} usado · {pool.availableFormatted || pool.totalFormatted || '—'} libre</div>
            </div>
          {/each}
        {/if}

        <div class="section-title">Apps</div>
        <div class="apps-grid">
          <div class="app-btn" on:click={() => switchTab('files')}>
            <div class="app-ico" style="--ico-color:#f59e0b">
              <svg viewBox="0 0 24 24"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
            </div>
            <div class="app-lbl">Files</div>
          </div>
          <div class="app-btn" on:click={() => switchTab('torrents')}>
            <div class="app-ico" style="--ico-color:#5ba8ff">
              <svg viewBox="0 0 24 24"><path d="M12 2v10M8 8l4 4 4-4"/><path d="M20 16v2a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-2"/></svg>
            </div>
            <div class="app-lbl">Torrent</div>
          </div>
          <div class="app-btn">
            <div class="app-ico" style="--ico-color:#a855f7">
              <svg viewBox="0 0 24 24"><rect x="3" y="3" width="18" height="18" rx="2"/><path d="M3 9h18M9 21V9"/></svg>
            </div>
            <div class="app-lbl">Docker</div>
          </div>
          <div class="app-btn" on:click={() => switchTab('more')}>
            <div class="app-ico" style="--ico-color:#4ade80">
              <svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="3"/><path d="M19.07 4.93a10 10 0 0 1 0 14.14M4.93 4.93a10 10 0 0 0 0 14.14"/></svg>
            </div>
            <div class="app-lbl">Settings</div>
          </div>
        </div>
      </div>

    <!-- FILES -->
    {:else if activeTab === 'files'}
      <div class="view">
        <div class="page-header">Archivos</div>
        <div class="section-title">Carpetas compartidas</div>
        {#if shares.length === 0}
          <div class="empty-state">Sin carpetas compartidas</div>
        {:else}
          {#each shares as s}
            <div class="share-item">
              <div class="share-ico">
                <svg viewBox="0 0 24 24"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
              </div>
              <div class="share-info">
                <div class="share-name">{s.displayName || s.name}</div>
                <div class="share-meta">{s.pool || '—'} · {s.mountpoint ? s.mountpoint.split('/').pop() : '—'}</div>
              </div>
              <svg class="share-arrow" viewBox="0 0 24 24"><path d="m9 18 6-6-6-6"/></svg>
            </div>
          {/each}
        {/if}
      </div>

    <!-- TORRENTS -->
    {:else if activeTab === 'torrents'}
      <div class="view">
        <div class="page-header">NimTorrent</div>
        <div class="tor-speeds">
          <div class="tor-spd"><span class="dl-arrow">↓</span> {fmtSpeed(torrentData.dlSpeed)}</div>
          <div class="tor-spd"><span class="ul-arrow">↑</span> {fmtSpeed(torrentData.ulSpeed)}</div>
        </div>

        {#if activeTorrents.length > 0}
          <div class="section-title">Descargando</div>
          {#each activeTorrents as t}
            <div class="tor-item">
              <div class="tor-top">
                <div class="tor-icon">
                  <svg viewBox="0 0 24 24"><path d="M12 2v10M8 8l4 4 4-4"/><path d="M20 16v2a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-2"/></svg>
                </div>
                <div class="tor-name">{t.name}</div>
                <div class="tor-spd-inline">↓ {fmtSpeed(t.dlSpeed)}</div>
              </div>
              <div class="tor-bar"><div class="tor-fill" style="width:{Math.min(100,t.progress||0)}%;background:#5ba8ff"></div></div>
            </div>
          {/each}
        {/if}

        {#if doneTorrents.length > 0}
          <div class="section-title" style="margin-top:8px">Completados</div>
          {#each doneTorrents as t}
            <div class="tor-item">
              <div class="tor-top">
                <div class="tor-icon done">
                  <svg viewBox="0 0 24 24"><polyline points="20 6 9 17 4 12"/></svg>
                </div>
                <div class="tor-name done-name">{t.name}</div>
                <div class="tor-done-label">100%</div>
              </div>
              <div class="tor-bar"><div class="tor-fill" style="width:100%;background:rgba(74,222,128,0.4)"></div></div>
            </div>
          {/each}
        {/if}

        {#if torrentData.torrents.length === 0}
          <div class="empty-state">Sin torrents</div>
        {/if}
      </div>

    <!-- MORE -->
    {:else if activeTab === 'more'}
      <div class="view">
        <div class="page-header">Más</div>
        <div class="more-user">
          <div class="more-avatar">{userName[0].toUpperCase()}</div>
          <div>
            <div class="more-username">{userName}</div>
            <div class="more-role">{$user?.role || 'user'}</div>
          </div>
        </div>

        <div class="section-title">Preferencias</div>
        <div class="more-list">
          <div class="more-item">
            <div class="more-item-ico" style="background:rgba(124,111,255,0.15)">
              <svg viewBox="0 0 24 24" style="stroke:#7c6fff"><circle cx="12" cy="12" r="4"/><path d="M12 2v2M12 20v2M4.93 4.93l1.41 1.41M17.66 17.66l1.41 1.41M2 12h2M20 12h2M4.93 19.07l1.41-1.41M17.66 6.34l1.41-1.41"/></svg>
            </div>
            <div class="more-item-label">Tema</div>
            <div class="theme-picker">
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="theme-opt" class:active={theme==='auto'}   on:click={() => theme='auto'}>Auto</div>
              <div class="theme-opt" class:active={theme==='dark'}   on:click={() => theme='dark'}>Oscuro</div>
              <div class="theme-opt" class:active={theme==='light'}  on:click={() => theme='light'}>Claro</div>
            </div>
          </div>
        </div>

        <div class="section-title">Sistema</div>
        <div class="more-list">
          <div class="more-item">
            <div class="more-item-ico" style="background:rgba(59,130,246,0.15)">
              <svg viewBox="0 0 24 24" style="stroke:#3b82f6"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/></svg>
            </div>
            <div class="more-item-label">Usuarios</div>
            <svg class="more-arrow" viewBox="0 0 24 24"><path d="m9 18 6-6-6-6"/></svg>
          </div>
          <div class="more-item">
            <div class="more-item-ico" style="background:rgba(251,191,36,0.15)">
              <svg viewBox="0 0 24 24" style="stroke:#fbbf24"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
            </div>
            <div class="more-item-label">2FA</div>
            <svg class="more-arrow" viewBox="0 0 24 24"><path d="m9 18 6-6-6-6"/></svg>
          </div>
          <div class="more-item">
            <div class="more-item-ico" style="background:rgba(74,222,128,0.15)">
              <svg viewBox="0 0 24 24" style="stroke:#4ade80"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
            </div>
            <div class="more-item-label">Actualizaciones</div>
            <svg class="more-arrow" viewBox="0 0 24 24"><path d="m9 18 6-6-6-6"/></svg>
          </div>
        </div>

        <div class="section-title">Sesión</div>
        <div class="more-list">
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="more-item" on:click={logout}>
            <div class="more-item-ico" style="background:rgba(248,113,113,0.15)">
              <svg viewBox="0 0 24 24" style="stroke:#f87171"><path d="M9 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h4"/><polyline points="16 17 21 12 16 7"/><line x1="21" y1="12" x2="9" y2="12"/></svg>
            </div>
            <div class="more-item-label" style="color:#f87171">Cerrar sesión</div>
          </div>
        </div>
      </div>
    {/if}

  </div>

  <!-- NAV BAR -->
  <div class="nav-bar">
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="nav-item" class:active={activeTab==='home'} on:click={() => switchTab('home')}>
      <svg viewBox="0 0 24 24"><path d="M3 9l9-7 9 7v11a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2z"/><polyline points="9 22 9 12 15 12 15 22"/></svg>
      <span>Home</span>
    </div>
    <div class="nav-item" class:active={activeTab==='files'} on:click={() => switchTab('files')}>
      <svg viewBox="0 0 24 24"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
      <span>Files</span>
    </div>
    <div class="nav-item" class:active={activeTab==='torrents'} on:click={() => switchTab('torrents')}>
      <svg viewBox="0 0 24 24"><path d="M12 2v10M8 8l4 4 4-4"/><path d="M20 16v2a2 2 0 0 1-2 2H6a2 2 0 0 1-2-2v-2"/></svg>
      <span>Torrents</span>
    </div>
    <div class="nav-item" class:active={activeTab==='more'} on:click={() => switchTab('more')}>
      <svg viewBox="0 0 24 24"><line x1="3" y1="12" x2="21" y2="12"/><line x1="3" y1="6" x2="21" y2="6"/><line x1="3" y1="18" x2="21" y2="18"/></svg>
      <span>Más</span>
    </div>
  </div>

</div>

<style>
  .mobile-root {
    width: 100%; height: 100vh;
    display: flex; flex-direction: column;
    font-family: -apple-system, 'SF Pro Display', 'DM Sans', sans-serif;
    overflow: hidden;
  }
  .dark  { background: #111111; color: #f0f0f0; }
  .light { background: #f2f1f6; color: #1a1a1a; }


  /* ── SCREEN ── */
  .screen { flex:1; overflow-y:auto; overflow-x:hidden; }
  .screen::-webkit-scrollbar { display:none; }
  .view { padding-bottom:8px; }

  /* ── HEADERS ── */
  .home-header { padding:18px 18px 16px; }
  .home-greeting { font-size:11px; margin-bottom:2px; }
  .dark .home-greeting  { color:rgba(255,255,255,0.4); }
  .light .home-greeting { color:rgba(0,0,0,0.4); }
  .home-title { font-size:24px; font-weight:700; }
  .page-header { font-size:24px; font-weight:700; padding:18px 18px 14px; }

  /* ── SECTION TITLE ── */
  .section-title { font-size:11px; font-weight:600; padding:0 18px 8px; text-transform:uppercase; letter-spacing:.06em; }
  .dark .section-title  { color:rgba(255,255,255,0.45); }
  .light .section-title { color:rgba(0,0,0,0.4); }

  /* ── STATS ── */
  .stats-grid { display:grid; grid-template-columns:1fr 1fr; gap:8px; padding:0 18px 16px; }
  .stat-card { border-radius:14px; padding:13px; }
  .dark .stat-card  { background:rgba(255,255,255,0.06); border:1px solid rgba(255,255,255,0.07); }
  .light .stat-card { background:#fff; border:1px solid rgba(0,0,0,0.06); }
  .stat-label { font-size:9px; text-transform:uppercase; letter-spacing:.07em; margin-bottom:5px; }
  .dark .stat-label  { color:rgba(255,255,255,0.4); }
  .light .stat-label { color:rgba(0,0,0,0.4); }
  .stat-val { font-size:22px; font-weight:700; margin-bottom:7px; }
  .stat-bar { height:3px; border-radius:2px; overflow:hidden; margin-bottom:5px; }
  .dark .stat-bar  { background:rgba(255,255,255,0.08); }
  .light .stat-bar { background:rgba(0,0,0,0.07); }
  .stat-fill { height:100%; border-radius:2px; transition:width .6s ease; }
  .stat-sub { font-size:9px; }
  .dark .stat-sub  { color:rgba(255,255,255,0.3); }
  .light .stat-sub { color:rgba(0,0,0,0.35); }

  /* ── POOL ── */
  .pool-card { margin:0 18px 10px; border-radius:14px; padding:13px 14px; }
  .dark .pool-card  { background:rgba(255,255,255,0.06); border:1px solid rgba(255,255,255,0.07); }
  .light .pool-card { background:#fff; border:1px solid rgba(0,0,0,0.06); }
  .pool-row { display:flex; justify-content:space-between; align-items:center; margin-bottom:8px; }
  .pool-name { font-size:13px; font-weight:600; }
  .pool-type { font-size:9px; border-radius:6px; padding:2px 7px; }
  .dark .pool-type  { color:rgba(255,255,255,0.35); background:rgba(255,255,255,0.07); }
  .light .pool-type { color:rgba(0,0,0,0.45); background:rgba(0,0,0,0.06); }
  .pool-bar { height:4px; border-radius:2px; overflow:hidden; margin-bottom:6px; }
  .dark .pool-bar  { background:rgba(255,255,255,0.08); }
  .light .pool-bar { background:rgba(0,0,0,0.07); }
  .pool-fill { height:100%; border-radius:2px; transition:width .6s ease; }
  .pool-meta { font-size:10px; }
  .dark .pool-meta  { color:rgba(255,255,255,0.35); }
  .light .pool-meta { color:rgba(0,0,0,0.4); }

  /* ── APPS GRID ── */
  .apps-grid { display:grid; grid-template-columns:repeat(4,1fr); gap:10px; padding:0 18px 20px; }
  .app-btn { display:flex; flex-direction:column; align-items:center; gap:5px; cursor:pointer; }
  .app-ico { width:52px; height:52px; border-radius:14px; display:flex; align-items:center; justify-content:center; }
  .dark .app-ico  { background:rgba(255,255,255,0.07); border:1px solid rgba(255,255,255,0.08); }
  .light .app-ico { background:#fff; border:1px solid rgba(0,0,0,0.07); }
  .app-ico svg { width:22px; height:22px; fill:none; stroke:var(--ico-color,#fff); stroke-width:1.8; stroke-linecap:round; }
  .app-lbl { font-size:10px; }
  .dark .app-lbl  { color:rgba(255,255,255,0.45); }
  .light .app-lbl { color:rgba(0,0,0,0.45); }

  /* ── FILES ── */
  .share-item { display:flex; align-items:center; gap:12px; padding:14px 18px; cursor:pointer; }
  .dark .share-item  { border-bottom:1px solid rgba(255,255,255,0.05); }
  .light .share-item { border-bottom:1px solid rgba(0,0,0,0.05); }
  .share-ico { width:40px; height:40px; border-radius:10px; background:rgba(124,111,255,0.15); display:flex; align-items:center; justify-content:center; flex-shrink:0; }
  .share-ico svg { width:20px; height:20px; stroke:#7c6fff; fill:none; stroke-width:1.8; }
  .share-info { flex:1; min-width:0; }
  .share-name { font-size:14px; font-weight:500; }
  .share-meta { font-size:11px; margin-top:2px; }
  .dark .share-meta  { color:rgba(255,255,255,0.35); }
  .light .share-meta { color:rgba(0,0,0,0.4); }
  .share-arrow { width:16px; height:16px; fill:none; stroke-width:2; stroke-linecap:round; }
  .dark .share-arrow  { stroke:rgba(255,255,255,0.2); }
  .light .share-arrow { stroke:rgba(0,0,0,0.2); }

  /* ── TORRENTS ── */
  .tor-speeds { display:flex; gap:20px; padding:0 18px 16px; }
  .tor-spd { font-size:15px; font-weight:600; display:flex; align-items:center; gap:5px; }
  .dl-arrow { color:#5ba8ff; }
  .ul-arrow { color:#4ad98a; }
  .tor-item { padding:11px 18px; }
  .dark .tor-item  { border-bottom:1px solid rgba(255,255,255,0.05); }
  .light .tor-item { border-bottom:1px solid rgba(0,0,0,0.05); }
  .tor-top { display:flex; align-items:center; gap:10px; margin-bottom:7px; }
  .tor-icon { width:28px; height:28px; border-radius:8px; background:rgba(91,168,255,0.12); display:flex; align-items:center; justify-content:center; flex-shrink:0; }
  .tor-icon svg { width:14px; height:14px; stroke:#5ba8ff; fill:none; stroke-width:2; }
  .tor-icon.done { background:rgba(74,217,138,0.12); }
  .tor-icon.done svg { stroke:#4ad98a; }
  .tor-name { flex:1; min-width:0; font-size:13px; font-weight:500; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .done-name { opacity:.5; }
  .tor-spd-inline { font-size:11px; color:#5ba8ff; flex-shrink:0; }
  .tor-done-label { font-size:11px; flex-shrink:0; }
  .dark .tor-done-label  { color:rgba(255,255,255,0.3); }
  .light .tor-done-label { color:rgba(0,0,0,0.3); }
  .tor-bar { height:3px; border-radius:2px; overflow:hidden; }
  .dark .tor-bar  { background:rgba(255,255,255,0.07); }
  .light .tor-bar { background:rgba(0,0,0,0.07); }
  .tor-fill { height:100%; border-radius:2px; transition:width .8s ease; }

  /* ── MORE ── */
  .more-user { display:flex; align-items:center; gap:12px; margin:0 18px 20px; padding:14px; border-radius:14px; }
  .dark .more-user  { background:rgba(255,255,255,0.06); border:1px solid rgba(255,255,255,0.07); }
  .light .more-user { background:#fff; border:1px solid rgba(0,0,0,0.06); }
  .more-avatar { width:44px; height:44px; border-radius:12px; background:linear-gradient(135deg,#e95420,#f97316); display:flex; align-items:center; justify-content:center; font-size:18px; font-weight:700; color:#fff; flex-shrink:0; }
  .more-username { font-size:15px; font-weight:600; }
  .more-role { font-size:11px; margin-top:1px; }
  .dark .more-role  { color:rgba(255,255,255,0.4); }
  .light .more-role { color:rgba(0,0,0,0.4); }
  .more-list { margin:0 18px 16px; border-radius:14px; overflow:hidden; }
  .dark .more-list  { background:rgba(255,255,255,0.04); border:1px solid rgba(255,255,255,0.07); }
  .light .more-list { background:#fff; border:1px solid rgba(0,0,0,0.06); }
  .more-item { display:flex; align-items:center; gap:12px; padding:14px 16px; cursor:pointer; }
  .dark .more-item  { border-bottom:1px solid rgba(255,255,255,0.05); }
  .light .more-item { border-bottom:1px solid rgba(0,0,0,0.05); }
  .more-item:last-child { border-bottom:none; }
  .more-item-ico { width:32px; height:32px; border-radius:9px; display:flex; align-items:center; justify-content:center; flex-shrink:0; }
  .more-item-ico svg { width:16px; height:16px; fill:none; stroke-width:1.8; stroke-linecap:round; }
  .more-item-label { flex:1; font-size:14px; }
  .more-arrow { width:14px; height:14px; fill:none; stroke-width:2; stroke-linecap:round; }
  .dark .more-arrow  { stroke:rgba(255,255,255,0.2); }
  .light .more-arrow { stroke:rgba(0,0,0,0.2); }

  /* theme picker */
  .theme-picker { display:flex; gap:4px; }
  .theme-opt { padding:4px 10px; border-radius:8px; font-size:11px; cursor:pointer; transition:all .15s; }
  .dark .theme-opt  { background:rgba(255,255,255,0.06); color:rgba(255,255,255,0.5); border:1px solid transparent; }
  .light .theme-opt { background:rgba(0,0,0,0.05); color:rgba(0,0,0,0.5); border:1px solid transparent; }
  .dark .theme-opt.active  { background:rgba(233,84,32,0.15); color:#e95420; border-color:rgba(233,84,32,0.3); }
  .light .theme-opt.active { background:rgba(233,84,32,0.10); color:#e95420; border-color:rgba(233,84,32,0.3); }

  /* ── EMPTY ── */
  .empty-state { text-align:center; padding:40px 18px; font-size:13px; }
  .dark .empty-state  { color:rgba(255,255,255,0.25); }
  .light .empty-state { color:rgba(0,0,0,0.25); }

  /* ── NAV BAR ── */
  .nav-bar { display:flex; flex-shrink:0; padding:8px 0 max(18px, env(safe-area-inset-bottom)); }
  .dark .nav-bar  { border-top:1px solid rgba(255,255,255,0.06); background:#111111; }
  .light .nav-bar { border-top:1px solid rgba(0,0,0,0.07); background:#ffffff; }
  .nav-item { flex:1; display:flex; flex-direction:column; align-items:center; gap:3px; padding:4px 0; cursor:pointer; }
  .nav-item svg { width:22px; height:22px; fill:none; stroke-width:1.8; stroke-linecap:round; transition:stroke .15s; }
  .nav-item span { font-size:10px; font-weight:500; transition:color .15s; }
  .dark .nav-item svg   { stroke:rgba(255,255,255,0.3); }
  .dark .nav-item span  { color:rgba(255,255,255,0.3); }
  .dark .nav-item.active svg  { stroke:#e95420; }
  .dark .nav-item.active span { color:#e95420; }
  .light .nav-item svg  { stroke:rgba(0,0,0,0.3); }
  .light .nav-item span { color:rgba(0,0,0,0.3); }
  .light .nav-item.active svg  { stroke:#e95420; }
  .light .nav-item.active span { color:#e95420; }
</style>
