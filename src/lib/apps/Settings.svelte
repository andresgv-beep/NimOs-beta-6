<script>
  import { onMount } from 'svelte';
  import { prefs, setPref, THEMES, ACCENT_COLORS } from '$lib/stores/theme.js';
  import TabNav from '$lib/components/TabNav.svelte';
  import { user } from '$lib/stores/auth.js';
  import { getToken } from '$lib/stores/auth.js';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  // ── Navegación ──
  let activeView = 'permissions';

  const sidebarSections = [
    {
      label: 'Sistema',
      items: [
        { id: 'monitor',     label: 'Monitor',     icon: 'monitor'  },
        { id: 'users',       label: 'Users',       icon: 'users'    },
        { id: 'permissions', label: 'Permissions', icon: 'folder'   },
        { id: 'portal',      label: 'Portal',      icon: 'portal'   },
        { id: 'updates',     label: 'Updates',     icon: 'updates'  },
      ]
    },
    {
      label: 'Preferencias',
      items: [
        { id: 'appearance',  label: 'Apariencia',  icon: 'eye'      },
        { id: 'about',       label: 'Acerca de',   icon: 'info'     },
      ]
    }
  ];

  $: userName = $user?.username || 'User';
  $: userRole = $user?.role     || 'user';

  // ── Permissions state ──
  let shares      = [];
  let users       = [];
  let pools       = [];
  let loading     = false;
  let editingShare = null;
  let wizardStep  = 1;
  let savingShare = false;
  let shareMsg    = '';
  let shareMsgError = false;
  let permsSub    = 'sharefolders'; // sharefolders | apppermissions

  // ── App Permissions state ──
  let appPermUsers = [];
  let appPermApps = [];
  let appPermGrants = [];  // all grants from DB
  let appPermLoading = false;
  let appPermSelectedUser = null;
  let appPermMsg = '';
  let appPermMsgError = false;

  // ── Users state ──
  let usersList    = [];
  let editingUser  = null;
  let savingUser   = false;
  let userMsg      = '';
  let userMsgError = false;

  // ── Updates state ──
  let updateData     = {};
  let checking       = false;
  let applying       = false;
  let updateMsg      = '';
  let updateMsgError = false;
  let updatePollId   = null;

  // ── Appearance ──
  let appearanceTab = 'tema';

  // ── Portal / 2FA state ──
  let twofa = { enabled: false, loading: true };
  let twofaSetup = null;   // { secret, uri } when setup in progress
  let twofaQrSvg = '';
  let twofaCode = '';
  let twofaMsg = '';
  let twofaMsgError = false;
  let twofaSaving = false;
  let twofaDisablePassword = '';
  let twofaBackupCodes = null; // string[] after enable
  let showDisableConfirm = false;

  // Wallpapers built-in
  const BUILTIN_WALLPAPERS = [
    '/wallpapers/nim_walpaper_01.jpeg',
    '/wallpapers/nim_walpaper_02.png',
  ];

  // Wallpapers añadidos por el usuario (guardados en prefs)
  $: userWallpapers = $prefs.userWallpapers || [];
  $: allWallpapers = [...BUILTIN_WALLPAPERS, ...userWallpapers.filter(w => !BUILTIN_WALLPAPERS.includes(w))];
  $: currentWallpaper = $prefs.wallpaper || '';

  function selectWallpaper(url) {
    setPref('wallpaper', url === currentWallpaper ? '' : url);
  }

  function addWallpaper() {
    const input = document.createElement('input');
    input.type = 'file';
    input.accept = 'image/*';
    input.onchange = (e) => {
      const file = e.target.files[0];
      if (!file) return;
      const reader = new FileReader();
      reader.onload = (ev) => {
        const url = ev.target.result;
        const existing = $prefs.userWallpapers || [];
        if (!existing.includes(url)) {
          setPref('userWallpapers', [...existing, url]);
        }
        setPref('wallpaper', url);
      };
      reader.readAsDataURL(file);
    };
    input.click();
  }
  $: currentTheme  = $prefs.theme       || 'midnight';
  $: currentAccent = $prefs.accentColor || 'orange';
  const themeLabels = { midnight: 'Midnight', dark: 'Dark', light: 'Light' };

  // ── Load data según vista ──
  async function loadView(view) {
    loading = true;
    try {
      if (view === 'permissions') {
        const [sr, ur, pr] = await Promise.all([
          fetch('/api/shares',        { headers: hdrs() }),
          fetch('/api/users',         { headers: hdrs() }),
          fetch('/api/storage/pools', { headers: hdrs() }),
        ]);
        const sd = await sr.json(); shares = sd.shares || sd || [];
        const ud = await ur.json(); users  = ud.users  || ud || [];
        const pd = await pr.json(); pools  = Array.isArray(pd) ? pd : (pd.pools || []);
      } else if (view === 'users') {
        const r = await fetch('/api/users', { headers: hdrs() });
        const d = await r.json(); usersList = d.users || d || [];
      } else if (view === 'updates') {
        const r = await fetch('/api/system/update/status', { headers: hdrs() });
        updateData = await r.json();
      } else if (view === 'portal') {
        twofa.loading = true;
        const r = await fetch('/api/auth/2fa/status', { headers: hdrs() });
        const d = await r.json();
        twofa = { enabled: d.enabled || false, loading: false };
        // Reset setup state when entering portal
        twofaSetup = null; twofaQrSvg = ''; twofaCode = '';
        twofaMsg = ''; twofaMsgError = false; twofaBackupCodes = null;
        showDisableConfirm = false; twofaDisablePassword = '';
      }
    } catch(e) { console.error('[Settings2] load failed', e); }
    loading = false;
  }

  $: loadView(activeView);

  // ── Shares ──
  function startNewShare() {
    editingShare = { _isNew: true, name: '', description: '', pool: pools[0]?.name || '', _perms: {} };
    for (const u of users) { if (u.role === 'admin') editingShare._perms[u.username] = 'rw'; }
    shareMsg = ''; wizardStep = 1;
  }

  function startEditShare(s) {
    const perms = {};
    if (s.permissions) for (const [u, p] of Object.entries(s.permissions)) perms[u] = p;
    editingShare = { _isNew: false, name: s.name, displayName: s.displayName, description: s.description || '', pool: s.pool, _perms: perms };
    shareMsg = ''; wizardStep = 1;
  }

  async function saveShare() {
    savingShare = true; shareMsg = '';
    try {
      if (editingShare._isNew) {
        if (!editingShare.name.trim()) { shareMsg = 'Nombre requerido'; shareMsgError = true; savingShare = false; return; }
        const res  = await fetch('/api/shares', { method: 'POST', headers: { ...hdrs(), 'Content-Type': 'application/json' }, body: JSON.stringify({ name: editingShare.name.trim(), description: editingShare.description, pool: editingShare.pool }) });
        const data = await res.json();
        if (!data.ok) { shareMsg = data.error || 'Error al crear'; shareMsgError = true; savingShare = false; return; }
        await fetch(`/api/shares/${data.name}`, { method: 'PUT', headers: { ...hdrs(), 'Content-Type': 'application/json' }, body: JSON.stringify({ permissions: editingShare._perms }) });
      } else {
        const res  = await fetch(`/api/shares/${editingShare.name}`, { method: 'PUT', headers: { ...hdrs(), 'Content-Type': 'application/json' }, body: JSON.stringify({ description: editingShare.description, permissions: editingShare._perms }) });
        const data = await res.json();
        if (!data.ok) { shareMsg = data.error || 'Error al guardar'; shareMsgError = true; savingShare = false; return; }
      }
      editingShare = null;
      loadView('permissions');
    } catch (e) { shareMsg = 'Error de conexión'; shareMsgError = true; }
    savingShare = false;
  }

  async function deleteShare(name) {
    if (!confirm(`¿Eliminar "${name}"? Los archivos se conservan.`)) return;
    try {
      const res = await fetch(`/api/shares/${name}`, { method: 'DELETE', headers: hdrs() });
      const d   = await res.json();
      if (d.ok) loadView('permissions'); else alert(d.error || 'Error');
    } catch { alert('Error de conexión'); }
  }

  // ── Users ──
  function startNewUser() { editingUser = { _isNew: true, username: '', password: '', role: 'user', description: '' }; userMsg = ''; }
  function startEditUser(u) { editingUser = { _isNew: false, username: u.username, password: '', role: u.role || 'user', description: u.description || '' }; userMsg = ''; }

  async function saveUser() {
    savingUser = true; userMsg = '';
    try {
      if (editingUser._isNew) {
        if (!editingUser.username.trim()) { userMsg = 'Nombre requerido'; userMsgError = true; savingUser = false; return; }
        if (!editingUser.password)        { userMsg = 'Contraseña requerida'; userMsgError = true; savingUser = false; return; }
        const res = await fetch('/api/users', { method: 'POST', headers: { ...hdrs(), 'Content-Type': 'application/json' }, body: JSON.stringify({ username: editingUser.username, password: editingUser.password, role: editingUser.role, description: editingUser.description }) });
        const d   = await res.json();
        if (d.error) { userMsg = d.error; userMsgError = true; savingUser = false; return; }
      } else {
        const body = { role: editingUser.role, description: editingUser.description };
        if (editingUser.password) body.password = editingUser.password;
        const res = await fetch(`/api/users/${editingUser.username}`, { method: 'PUT', headers: { ...hdrs(), 'Content-Type': 'application/json' }, body: JSON.stringify(body) });
        const d   = await res.json();
        if (d.error) { userMsg = d.error; userMsgError = true; savingUser = false; return; }
      }
      editingUser = null; loadView('users');
    } catch { userMsg = 'Error de conexión'; userMsgError = true; }
    savingUser = false;
  }

  async function deleteUser(username) {
    if (!confirm(`¿Eliminar el usuario "${username}"?`)) return;
    try {
      const res = await fetch(`/api/users/${username}`, { method: 'DELETE', headers: hdrs() });
      const d   = await res.json();
      if (d.ok) loadView('users'); else alert(d.error || 'Error');
    } catch { alert('Error de conexión'); }
  }

  // ── Updates ──
  async function checkForUpdates() {
    checking = true; updateMsg = '';
    try {
      const r = await fetch('/api/system/update/check', { headers: hdrs() });
      const d = await r.json();
      updateData = { ...updateData, ...d };
      updateMsg = d.updateAvailable ? `Versión ${d.latestVersion} disponible` : 'Ya estás en la última versión';
      updateMsgError = false;
    } catch { updateMsg = 'Error comprobando'; updateMsgError = true; }
    checking = false;
  }

  async function applyUpdate() {
    applying = true; updateMsg = 'Aplicando actualización...';
    try {
      const r = await fetch('/api/system/update/apply', { method: 'POST', headers: hdrs() });
      const d = await r.json();
      if (d.ok) {
        updatePollId = setInterval(async () => {
          try {
            const sr = await fetch('/api/system/update/status', { headers: hdrs() });
            const sd = await sr.json();
            if (sd.done) {
              clearInterval(updatePollId); applying = false;
              updateMsg = sd.type === 'error' ? (sd.error || 'Error') : `Actualizado. Recarga el navegador.`;
              updateMsgError = sd.type === 'error';
              updateData = sd;
            }
          } catch {}
        }, 3000);
      } else { updateMsg = d.error || 'Error'; updateMsgError = true; applying = false; }
    } catch { updateMsg = 'Error de conexión'; updateMsgError = true; applying = false; }
  }

  // ── Portal / 2FA ──
  async function twofa_startSetup() {
    twofaSaving = true; twofaMsg = '';
    try {
      const r = await fetch('/api/auth/2fa/setup', { method: 'POST', headers: hdrs() });
      const d = await r.json();
      if (d.error) { twofaMsg = d.error; twofaMsgError = true; twofaSaving = false; return; }
      twofaSetup = { secret: d.secret, uri: d.uri };
      // Get QR SVG
      const qr = await fetch('/api/auth/2fa/qr', {
        method: 'POST',
        headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ text: d.uri }),
      });
      const qd = await qr.json();
      twofaQrSvg = qd.svg || '';
    } catch { twofaMsg = 'Error iniciando configuración'; twofaMsgError = true; }
    twofaSaving = false;
  }

  async function twofa_verify() {
    if (!twofaCode || twofaCode.length !== 6) { twofaMsg = 'Introduce el código de 6 dígitos'; twofaMsgError = true; return; }
    twofaSaving = true; twofaMsg = '';
    try {
      const r = await fetch('/api/auth/2fa/verify', {
        method: 'POST',
        headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ code: twofaCode }),
      });
      const d = await r.json();
      if (d.error) { twofaMsg = d.error; twofaMsgError = true; twofaSaving = false; return; }
      twofa.enabled = true;
      twofaBackupCodes = d.backupCodes || null;
      twofaSetup = null; twofaQrSvg = ''; twofaCode = '';
      twofaMsg = '2FA activado correctamente'; twofaMsgError = false;
    } catch { twofaMsg = 'Error verificando código'; twofaMsgError = true; }
    twofaSaving = false;
  }

  async function twofa_disable() {
    if (!twofaDisablePassword) { twofaMsg = 'Introduce tu contraseña'; twofaMsgError = true; return; }
    twofaSaving = true; twofaMsg = '';
    try {
      const r = await fetch('/api/auth/2fa/disable', {
        method: 'POST',
        headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ password: twofaDisablePassword }),
      });
      const d = await r.json();
      if (d.error) { twofaMsg = d.error; twofaMsgError = true; twofaSaving = false; return; }
      twofa.enabled = false;
      showDisableConfirm = false; twofaDisablePassword = '';
      twofaMsg = '2FA desactivado'; twofaMsgError = false;
    } catch { twofaMsg = 'Error desactivando 2FA'; twofaMsgError = true; }
    twofaSaving = false;
  }

  // ── App Permissions ──
  async function loadAppPermissions() {
    appPermLoading = true; appPermMsg = '';
    try {
      const [ur, ar, gr] = await Promise.all([
        fetch('/api/users',            { headers: hdrs() }),
        fetch('/api/app-access/apps',  { headers: hdrs() }),
        fetch('/api/app-access',       { headers: hdrs() }),
      ]);
      const ud = await ur.json(); appPermUsers = (ud.users || ud || []).filter(u => u.role !== 'admin');
      const ad = await ar.json(); appPermApps  = (ad.apps || []).filter(a => !a.public && !a.adminOnly);
      const gd = await gr.json(); appPermGrants = gd.grants || [];
    } catch(e) { console.error('[AppPerms] load failed', e); }
    appPermLoading = false;
  }

  // Check if user has grant for app
  function hasGrant(username, appId) {
    return appPermGrants.some(g => g.username === username && g.appId === appId);
  }

  function getGrantPerm(username, appId) {
    const g = appPermGrants.find(g => g.username === username && g.appId === appId);
    return g?.permission || '';
  }

  async function toggleAppAccess(username, appId) {
    appPermMsg = '';
    try {
      if (hasGrant(username, appId)) {
        await fetch('/api/app-access', {
          method: 'DELETE',
          headers: { ...hdrs(), 'Content-Type': 'application/json' },
          body: JSON.stringify({ username, appId }),
        });
      } else {
        await fetch('/api/app-access', {
          method: 'POST',
          headers: { ...hdrs(), 'Content-Type': 'application/json' },
          body: JSON.stringify({ username, appId, permission: 'use' }),
        });
      }
      // Reload grants
      const gr = await fetch('/api/app-access', { headers: hdrs() });
      const gd = await gr.json();
      appPermGrants = gd.grants || [];
    } catch { appPermMsg = 'Error actualizando permiso'; appPermMsgError = true; }
  }

  // Reactive: load app perms when switching to that sub-tab
  $: if (permsSub === 'apppermissions' && appPermApps.length === 0) loadAppPermissions();
</script>

<div class="s2-root">

  <!-- ── SIDEBAR ── -->
  <div class="sidebar">
    <div class="sb-header">
      <svg width="18" height="18" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">
        <rect x="2" y="3" width="9" height="9" rx="2"/><rect x="13" y="3" width="9" height="9" rx="2"/>
        <rect x="2" y="13" width="9" height="9" rx="2"/><rect x="13" y="13" width="9" height="9" rx="2"/>
      </svg>
      <span class="sb-title">Settings</span>
    </div>

    {#each sidebarSections as section}
      <div class="sb-section">{section.label}</div>
      {#each section.items as item}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="sb-item" class:active={activeView === item.id} on:click={() => activeView = item.id}>
          <span class="sb-ico">
            {#if item.icon === 'monitor'}
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><rect x="2" y="3" width="20" height="14" rx="2"/><line x1="8" y1="21" x2="16" y2="21"/><line x1="12" y1="17" x2="12" y2="21"/></svg>
            {:else if item.icon === 'users'}
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/><path d="M23 21v-2a4 4 0 0 0-3-3.87M16 3.13a4 4 0 0 1 0 7.75"/></svg>
            {:else if item.icon === 'folder'}
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
            {:else if item.icon === 'portal'}
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="12" r="10"/><line x1="2" y1="12" x2="22" y2="12"/><path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/></svg>
            {:else if item.icon === 'updates'}
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-2.12-9.36L23 10"/></svg>
            {:else if item.icon === 'eye'}
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M1 12s4-8 11-8 11 8 11 8-4 8-11 8-11-8-11-8z"/><circle cx="12" cy="12" r="3"/></svg>
            {:else if item.icon === 'info'}
              <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/></svg>
            {/if}
          </span>
          {item.label}
        </div>
      {/each}
    {/each}

    <div class="sb-bottom">
      <div class="sb-user">
        <div class="sb-avatar">{userName[0].toUpperCase()}</div>
        <div class="sb-user-info">
          <div class="sb-user-name">{userName}</div>
          <div class="sb-user-role">{userRole}</div>
        </div>
      </div>
    </div>
  </div>

  <!-- ── INNER ── -->
  <div class="inner-wrap">
    <div class="inner">

      <!-- Titlebar -->
      <div class="inner-titlebar">
        <span class="tb-title">Settings</span>
        <span class="tb-sub">— {sidebarSections.flatMap(s => s.items).find(i => i.id === activeView)?.label || ''}</span>
        {#if activeView === 'appearance'}
          <div class="tb-tabs">
            <TabNav tabs={[
              { id:'tema',    label:'Tema'    },
              { id:'taskbar', label:'Taskbar' },
              { id:'escala',  label:'Escala'  },
            ]} bind:active={appearanceTab} />
          </div>
        {/if}
      </div>

      <!-- Content -->
      <div class="inner-content">

        {#if loading}
          <div class="s-loading"><div class="spinner"></div></div>

        {:else if activeView === 'monitor'}
          <div class="section-label">Monitor del sistema</div>
          <p class="coming-soon">Dashboard — coming soon</p>

        {:else if activeView === 'users'}
          <div class="section-label">Usuarios</div>
          {#if usersList.length === 0}
            <p class="coming-soon">No hay usuarios</p>
          {:else}
            <div class="user-list">
              {#each usersList as u}
                <div class="user-row">
                  <div class="user-avatar">{(u.username || '?')[0].toUpperCase()}</div>
                  <span class="user-name">{u.username}</span>
                  <span class="user-role-label">{u.role || 'user'}</span>
                  <div class="user-badge" class:admin={u.role === 'admin'}>{u.role || 'user'}</div>
                  <div class="row-actions">
                    <!-- svelte-ignore a11y_click_events_have_key_events -->
                    <!-- svelte-ignore a11y_no_static_element_interactions -->
                    <button class="action-btn" on:click={() => startEditUser(u)} title="Editar">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M11 4H4a2 2 0 0 0-2 2v14a2 2 0 0 0 2 2h14a2 2 0 0 0 2-2v-7"/><path d="M18.5 2.5a2.121 2.121 0 0 1 3 3L12 15l-4 1 1-4z"/></svg>
                    </button>
                    <!-- svelte-ignore a11y_click_events_have_key_events -->
                    <!-- svelte-ignore a11y_no_static_element_interactions -->
                    <button class="action-btn danger" on:click={() => deleteUser(u.username)} title="Eliminar">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14H6L5 6"/><path d="M10 11v6M14 11v6"/><path d="M9 6V4h6v2"/></svg>
                    </button>
                  </div>
                </div>
              {/each}
            </div>
          {/if}
          <button class="btn-accent" style="margin-top:14px" on:click={startNewUser}>+ Nuevo usuario</button>

        {:else if activeView === 'permissions'}
          <!-- Sub-tabs -->
          <div class="sub-tabs">
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="sub-tab" class:active={permsSub === 'sharefolders'} on:click={() => permsSub = 'sharefolders'}>Shared Folders</div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="sub-tab" class:active={permsSub === 'apppermissions'} on:click={() => permsSub = 'apppermissions'}>App Permissions</div>
          </div>

          {#if permsSub === 'sharefolders'}
            {#if shares.length > 0}
              <div class="share-list">
                {#each shares as s}
                  <div class="share-row">
                    <div class="share-folder-icon">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
                    </div>
                    <span class="share-name">{s.displayName || s.name}</span>
                    <span class="share-pool">{s.pool || '—'}</span>
                    <div class="share-protocols">
                      <span class="proto smb" class:on={s.smb}>SMB</span>
                      <span class="proto nfs" class:on={s.nfs}>NFS</span>
                      <span class="proto ftp" class:on={s.ftp}>FTP</span>
                    </div>
                    <span class="share-users">
                      {Object.keys(s.permissions || {}).length} usuario{Object.keys(s.permissions || {}).length !== 1 ? 's' : ''}
                    </span>
                    <!-- svelte-ignore a11y_click_events_have_key_events -->
                    <!-- svelte-ignore a11y_no_static_element_interactions -->
                    <button class="btn-more" on:click={() => startEditShare(s)} title="Opciones">···</button>
                  </div>
                {/each}
              </div>
            {:else}
              <div class="empty-state">
                <div class="empty-icon">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
                </div>
                <div class="empty-title">Sin carpetas compartidas</div>
                <div class="empty-desc">Crea un pool de almacenamiento primero para poder compartir carpetas.</div>
              </div>
            {/if}
            {#if pools.length > 0}
              <button class="btn-accent" style="margin-top:16px" on:click={startNewShare}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" style="width:11px;height:11px"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
                Nueva carpeta compartida
              </button>
            {/if}

          {:else}
            <div class="section-label">Permisos de aplicaciones</div>
            <p class="appperm-desc">Controla qué aplicaciones puede usar cada usuario. Los administradores siempre tienen acceso total. Files y Media Player son públicas por defecto.</p>

            {#if appPermLoading}
              <div class="s-loading"><div class="spinner"></div></div>
            {:else if appPermUsers.length === 0}
              <div class="empty-state">
                <div class="empty-icon">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M17 21v-2a4 4 0 0 0-4-4H5a4 4 0 0 0-4 4v2"/><circle cx="9" cy="7" r="4"/></svg>
                </div>
                <div class="empty-title">Sin usuarios</div>
                <div class="empty-desc">Crea usuarios no-admin para gestionar sus permisos de aplicaciones.</div>
              </div>
            {:else}
              <div class="appperm-grid">
                <!-- Header row -->
                <div class="appperm-header">
                  <div class="appperm-user-col">Usuario</div>
                  {#each appPermApps as app}
                    <div class="appperm-app-col" title={app.name}>{app.name}</div>
                  {/each}
                </div>
                <!-- User rows -->
                {#each appPermUsers as u}
                  <div class="appperm-row">
                    <div class="appperm-user-col">
                      <span class="appperm-avatar">{(u.username || '?')[0].toUpperCase()}</span>
                      <span class="appperm-username">{u.username}</span>
                    </div>
                    {#each appPermApps as app}
                      <div class="appperm-app-col">
                        <!-- svelte-ignore a11y_click_events_have_key_events -->
                        <!-- svelte-ignore a11y_no_static_element_interactions -->
                        <div class="appperm-toggle" class:on={hasGrant(u.username, app.id)} on:click={() => toggleAppAccess(u.username, app.id)} title={hasGrant(u.username, app.id) ? 'Revocar acceso' : 'Dar acceso'}>
                          <div class="appperm-toggle-dot"></div>
                        </div>
                      </div>
                    {/each}
                  </div>
                {/each}
              </div>
              {#if appPermMsg}
                <div class="appperm-msg" class:error={appPermMsgError}>{appPermMsg}</div>
              {/if}
              <button class="btn-secondary" style="margin-top:12px" on:click={loadAppPermissions}>↻ Recargar</button>
            {/if}
          {/if}

        {:else if activeView === 'portal'}
          <div class="section-label">Autenticación en dos pasos (2FA)</div>

          {#if twofa.loading}
            <div class="s-loading"><div class="spinner"></div></div>

          {:else if twofaBackupCodes}
            <!-- Backup codes shown after enabling -->
            <div class="twofa-success">
              <div class="twofa-success-icon">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><polyline points="20 6 9 17 4 12"/></svg>
              </div>
              <div class="twofa-success-title">2FA activado</div>
              <p class="twofa-success-desc">Guarda estos códigos de recuperación en un lugar seguro. Son de un solo uso y te permitirán acceder si pierdes tu dispositivo.</p>
              <div class="backup-codes-grid">
                {#each twofaBackupCodes as code}
                  <div class="backup-code">{code}</div>
                {/each}
              </div>
              <button class="btn-secondary" style="margin-top:12px" on:click={() => { twofaBackupCodes = null; }}>
                Ya los he guardado
              </button>
            </div>

          {:else if twofaSetup}
            <!-- Setup wizard: scan QR + verify -->
            <div class="twofa-setup">
              <div class="twofa-step-label">1. Escanea el código QR con tu app de autenticación</div>
              <p class="twofa-hint">Usa Google Authenticator, Authy, o cualquier app compatible con TOTP.</p>

              <div class="twofa-qr-wrap">
                {#if twofaQrSvg}
                  <div class="twofa-qr">{@html twofaQrSvg}</div>
                {:else}
                  <div class="twofa-qr-placeholder"><div class="spinner"></div></div>
                {/if}
              </div>

              <div class="twofa-secret-row">
                <span class="twofa-secret-label">Clave manual:</span>
                <code class="twofa-secret-value">{twofaSetup.secret}</code>
              </div>

              <div class="twofa-step-label" style="margin-top:20px">2. Introduce el código de 6 dígitos</div>
              <div class="twofa-verify-row">
                <input
                  class="form-input twofa-code-input"
                  type="text"
                  placeholder="000000"
                  maxlength="6"
                  bind:value={twofaCode}
                  on:input={() => twofaCode = twofaCode.replace(/\D/g, '')}
                  on:keydown={(e) => { if (e.key === 'Enter') twofa_verify(); }}
                />
                <button class="btn-accent" on:click={twofa_verify} disabled={twofaSaving}>
                  {twofaSaving ? 'Verificando...' : 'Verificar y activar'}
                </button>
              </div>

              <button class="btn-link" on:click={() => { twofaSetup = null; twofaQrSvg = ''; twofaCode = ''; twofaMsg = ''; }}>
                Cancelar
              </button>
            </div>

          {:else if twofa.enabled}
            <!-- 2FA is active -->
            <div class="twofa-status-card enabled">
              <div class="twofa-status-icon enabled">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
              </div>
              <div class="twofa-status-info">
                <div class="twofa-status-title">2FA activado</div>
                <div class="twofa-status-desc">Tu cuenta está protegida con autenticación en dos pasos.</div>
              </div>
              <div class="twofa-status-badge enabled">Activo</div>
            </div>

            {#if !showDisableConfirm}
              <button class="btn-danger-outline" style="margin-top:14px" on:click={() => showDisableConfirm = true}>
                Desactivar 2FA
              </button>
            {:else}
              <div class="twofa-disable-form">
                <div class="twofa-step-label">Confirma tu contraseña para desactivar 2FA</div>
                <div class="twofa-verify-row">
                  <input
                    class="form-input"
                    type="password"
                    placeholder="Tu contraseña"
                    bind:value={twofaDisablePassword}
                    on:keydown={(e) => { if (e.key === 'Enter') twofa_disable(); }}
                  />
                  <button class="btn-accent" style="background:var(--red)" on:click={twofa_disable} disabled={twofaSaving}>
                    {twofaSaving ? '...' : 'Confirmar'}
                  </button>
                  <button class="btn-secondary" on:click={() => { showDisableConfirm = false; twofaDisablePassword = ''; twofaMsg = ''; }}>
                    Cancelar
                  </button>
                </div>
              </div>
            {/if}

          {:else}
            <!-- 2FA not enabled -->
            <div class="twofa-status-card">
              <div class="twofa-status-icon">
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 9.9-1"/></svg>
              </div>
              <div class="twofa-status-info">
                <div class="twofa-status-title">2FA desactivado</div>
                <div class="twofa-status-desc">Añade una capa extra de seguridad a tu cuenta con Google Authenticator u otra app TOTP compatible.</div>
              </div>
              <div class="twofa-status-badge">Inactivo</div>
            </div>

            <button class="btn-accent" style="margin-top:14px" on:click={twofa_startSetup} disabled={twofaSaving}>
              {twofaSaving ? 'Configurando...' : 'Configurar 2FA'}
            </button>
          {/if}

          {#if twofaMsg}
            <div class="twofa-msg" class:error={twofaMsgError}>{twofaMsg}</div>
          {/if}

          <div class="portal-sep"></div>
          <div class="section-label">Sesión</div>
          <p class="coming-soon">Auto-logout por inactividad — coming soon</p>

        {:else if activeView === 'updates'}
          <div class="section-label">Actualizaciones</div>
          <div class="field-group">
            <div class="field-row"><span class="field-label">Versión actual</span><span class="field-value">{updateData.currentVersion || updateData.current || updateData.version || '—'}</span></div>
            <div class="field-row"><span class="field-label">Última versión</span><span class="field-value">{updateData.latestVersion || updateData.latest || '—'}</span></div>
            <div class="field-row">
              <span class="field-label">Estado</span>
              <span class="field-value" style="color:{updateData.updateAvailable ? 'var(--amber)' : 'var(--green)'}">
                {updateData.updateAvailable ? 'Actualización disponible' : 'Al día'}
              </span>
            </div>
          </div>
          <div class="update-actions">
            <button class="btn-secondary" on:click={checkForUpdates} disabled={checking || applying}>{checking ? 'Comprobando...' : 'Comprobar actualizaciones'}</button>
            {#if updateData.updateAvailable}
              <button class="btn-accent" on:click={applyUpdate} disabled={applying}>{applying ? 'Actualizando...' : 'Aplicar actualización'}</button>
            {/if}
          </div>
          {#if updateMsg}<div class="update-msg" class:error={updateMsgError}>{updateMsg}</div>{/if}
          {#if applying}<div class="update-progress"><div class="spinner" style="width:16px;height:16px"></div><span>No cierres el navegador</span></div>{/if}

        {:else if activeView === 'appearance'}
          {#if appearanceTab === 'tema'}
            <div class="section-label">Tema del sistema</div>
            <div class="theme-row">
              {#each ['midnight', 'dark', 'light'] as t}
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <div class="theme-card" class:active={currentTheme === t} on:click={() => setPref('theme', t)}>
                  <div class="theme-preview {t}">
                    <div class="tp-sidebar"></div>
                    <div class="tp-content"><div class="tp-bar"></div><div class="tp-line"></div><div class="tp-line short"></div></div>
                  </div>
                  <div class="theme-label">{themeLabels[t]}</div>
                </div>
              {/each}
            </div>
            <div class="section-label" style="margin-top:24px">Color de acento</div>
            <div class="accent-row">
              {#each Object.entries(ACCENT_COLORS) as [name, color]}
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <div class="accent-dot" class:active={currentAccent === name} style="background:{color}" on:click={() => setPref('accentColor', name)} title={name}></div>
              {/each}
            </div>

            <div class="wall-header" style="margin-top:24px">
              <div class="section-label" style="margin:0">Fondo de escritorio</div>
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <button class="wall-add-btn" on:click={addWallpaper}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="12" y1="5" x2="12" y2="19"/><line x1="5" y1="12" x2="19" y2="12"/></svg>
                Añadir imagen...
              </button>
            </div>
            <div class="wall-grid">
              <!-- Ninguno -->
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="wall-item" class:active={!currentWallpaper} on:click={() => selectWallpaper('')}>
                <div class="wall-none">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" style="width:20px;height:20px;opacity:.4"><rect x="3" y="3" width="18" height="18" rx="2"/><line x1="3" y1="3" x2="21" y2="21"/></svg>
                </div>
                {#if !currentWallpaper}
                  <div class="wall-check">
                    <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round"><polyline points="20 6 9 17 4 12"/></svg>
                  </div>
                {/if}
                <div class="wall-label">Ninguno</div>
              </div>
              {#each allWallpapers as wp}
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <div class="wall-item" class:active={currentWallpaper === wp} on:click={() => selectWallpaper(wp)}>
                  <img src={wp} alt="wallpaper" class="wall-thumb" loading="lazy" />
                  {#if currentWallpaper === wp}
                    <div class="wall-check">
                      <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="3" stroke-linecap="round"><polyline points="20 6 9 17 4 12"/></svg>
                    </div>
                  {/if}
                </div>
              {/each}
            </div>

          {:else if appearanceTab === 'taskbar'}
            <div class="section-label">Estilo</div>
            <div class="setting-row">
              <span class="setting-label">Modo</span>
              <div class="setting-options">
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <button class="opt-btn" class:active={$prefs.taskbarMode === 'classic'} on:click={() => setPref('taskbarMode', 'classic')}>Clásico</button>
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <button class="opt-btn" class:active={$prefs.taskbarMode === 'dock'} on:click={() => setPref('taskbarMode', 'dock')}>Dock</button>
              </div>
            </div>
            <div class="section-label" style="margin-top:16px">Posición</div>
            <div class="setting-row">
              <span class="setting-label">Posición</span>
              <div class="setting-options">
                {#each ['bottom', 'top', 'left'] as pos}
                  <!-- svelte-ignore a11y_click_events_have_key_events -->
                  <!-- svelte-ignore a11y_no_static_element_interactions -->
                  <button class="opt-btn"
                    class:active={$prefs.taskbarPosition === pos}
                    disabled={$prefs.taskbarMode === 'dock' && pos === 'left'}
                    on:click={() => setPref('taskbarPosition', pos)}>
                    {pos === 'bottom' ? 'Abajo' : pos === 'top' ? 'Arriba' : 'Izquierda'}
                  </button>
                {/each}
              </div>
            </div>
            <div class="setting-row">
              <span class="setting-label">Tamaño</span>
              <div class="setting-options">
                {#each ['small', 'medium', 'large'] as size}
                  <!-- svelte-ignore a11y_click_events_have_key_events -->
                  <!-- svelte-ignore a11y_no_static_element_interactions -->
                  <button class="opt-btn" class:active={$prefs.taskbarSize === size} on:click={() => setPref('taskbarSize', size)}>
                    {size === 'small' ? 'Pequeño' : size === 'medium' ? 'Medio' : 'Grande'}
                  </button>
                {/each}
              </div>
            </div>

          {:else if appearanceTab === 'escala'}
            <div class="section-label">Escala de interfaz</div>
            <div class="setting-row">
              <span class="setting-label">Escala UI</span>
              <div class="setting-options">
                {#each [{v:'auto',l:'Auto'},{v:85,l:'85%'},{v:100,l:'100%'},{v:115,l:'115%'},{v:125,l:'125%'},{v:150,l:'150%'}] as opt}
                  <!-- svelte-ignore a11y_click_events_have_key_events -->
                  <!-- svelte-ignore a11y_no_static_element_interactions -->
                  <button class="opt-btn" class:active={$prefs.uiScale === opt.v} on:click={() => setPref('uiScale', opt.v)}>
                    {opt.l}
                  </button>
                {/each}
              </div>
            </div>
            <div style="font-size:10px;color:var(--text-3);margin-top:8px;font-family:'DM Mono',monospace">
              Pantalla: {typeof window !== 'undefined' ? `${window.screen.width}×${window.screen.height}` : '—'} · DPR: {typeof window !== 'undefined' ? window.devicePixelRatio?.toFixed(2) : '—'} · CSS: {typeof window !== 'undefined' ? `${window.innerWidth}×${window.innerHeight}` : '—'}
            </div>

          {/if}

        {:else if activeView === 'about'}
          <div class="section-label">Acerca de NimOS</div>
          <p style="color:var(--text-2);font-size:12px">NimOS Beta 5 — NAS Operating System</p>
          <p style="color:var(--text-3);font-size:11px;margin-top:4px">Backend: Go · Frontend: SvelteKit · License: MIT</p>
        {/if}

      </div>

      <div class="statusbar">
        <div class="status-dot"></div>
        <span>NimOS Beta 5</span>
      </div>
    </div>
  </div>
</div>

<!-- ══ MODAL — Carpeta compartida ══ -->
{#if editingShare}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-overlay" on:click|self={() => editingShare = null}></div>
  <div class="modal">
    <div class="modal-header">
      <div class="modal-title">{editingShare._isNew ? 'Nueva carpeta compartida' : `Editar: ${editingShare.displayName || editingShare.name}`}</div>
      <div class="modal-steps">
        <div class="modal-step" class:active={wizardStep === 1} class:done={wizardStep > 1}>1</div>
        {#if editingShare._isNew}
          <div class="modal-step-line" class:done={wizardStep > 1}></div>
          <div class="modal-step" class:active={wizardStep === 2}>2</div>
        {/if}
      </div>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="modal-close" on:click={() => editingShare = null}>✕</div>
    </div>
    <div class="modal-body">
      {#if wizardStep === 1}
        {#if editingShare._isNew}
          <div class="modal-step-label">Información básica</div>
          <div class="form-field">
            <label class="form-label">Nombre <span style="color:var(--red)">*</span></label>
            <input class="form-input" type="text" placeholder="documentos" bind:value={editingShare.name} autofocus />
          </div>
          <div class="form-field">
            <label class="form-label">Descripción</label>
            <input class="form-input" type="text" placeholder="Opcional" bind:value={editingShare.description} />
          </div>
          <div class="form-field">
            <label class="form-label">Pool de almacenamiento</label>
            <select class="form-select" bind:value={editingShare.pool}>
              {#each pools as pool}
                <option value={pool.name}>{pool.name} — {pool.totalFormatted || '—'} ({pool.raidLevel})</option>
              {/each}
            </select>
          </div>
        {:else}
          <div class="modal-step-label">Permisos de usuario</div>
          <div class="perm-table">
            <div class="perm-header"><span class="perm-col-user">Usuario</span><span class="perm-col-perm">Permiso</span></div>
            {#each users as u}
              <div class="perm-row">
                <div class="perm-col-user">
                  <span class="perm-avatar">{(u.username || '?')[0].toUpperCase()}</span>
                  <span class="perm-name">{u.username}</span>
                  {#if u.role === 'admin'}<span class="perm-admin-tag">admin</span>{/if}
                </div>
                <div class="perm-col-perm">
                  <select class="form-select perm-select"
                    value={editingShare._perms[u.username] || 'none'}
                    on:change={(e) => { editingShare._perms[u.username] = e.target.value; editingShare = editingShare; }}>
                    <option value="none">Sin acceso</option>
                    <option value="ro">Solo lectura</option>
                    <option value="rw">Lectura / Escritura</option>
                  </select>
                </div>
              </div>
            {/each}
          </div>
        {/if}
      {:else if wizardStep === 2}
        <div class="modal-step-label">Permisos de usuario</div>
        <div class="perm-table">
          <div class="perm-header"><span class="perm-col-user">Usuario</span><span class="perm-col-perm">Permiso</span></div>
          {#each users as u}
            <div class="perm-row">
              <div class="perm-col-user">
                <span class="perm-avatar">{(u.username || '?')[0].toUpperCase()}</span>
                <span class="perm-name">{u.username}</span>
                {#if u.role === 'admin'}<span class="perm-admin-tag">admin</span>{/if}
              </div>
              <div class="perm-col-perm">
                <select class="form-select perm-select"
                  value={editingShare._perms[u.username] || 'none'}
                  on:change={(e) => { editingShare._perms[u.username] = e.target.value; editingShare = editingShare; }}>
                  <option value="none">Sin acceso</option>
                  <option value="ro">Solo lectura</option>
                  <option value="rw">Lectura / Escritura</option>
                </select>
              </div>
            </div>
          {/each}
        </div>
        <div class="modal-summary">
          <div class="summary-label">Resumen</div>
          <div class="summary-row"><span>Nombre</span><span>{editingShare.name}</span></div>
          {#if editingShare.description}<div class="summary-row"><span>Descripción</span><span>{editingShare.description}</span></div>{/if}
          <div class="summary-row"><span>Pool</span><span>{editingShare.pool}</span></div>
        </div>
      {/if}
      {#if shareMsg}<div class="share-msg" class:error={shareMsgError}>{shareMsg}</div>{/if}
    </div>
    <div class="modal-footer">
      {#if wizardStep === 2}
        <button class="btn-secondary" on:click={() => wizardStep = 1}>← Anterior</button>
      {:else}
        <button class="btn-secondary" on:click={() => editingShare = null}>Cancelar</button>
      {/if}
      {#if editingShare._isNew && wizardStep === 1}
        <button class="btn-accent" on:click={() => { if (!editingShare.name.trim()) { shareMsg = 'Nombre requerido'; shareMsgError = true; return; } shareMsg = ''; wizardStep = 2; }}>Siguiente →</button>
      {:else}
        <button class="btn-accent" on:click={saveShare} disabled={savingShare}>{savingShare ? 'Guardando...' : editingShare._isNew ? 'Crear carpeta' : 'Guardar cambios'}</button>
      {/if}
    </div>
  </div>
{/if}

<!-- ══ MODAL — Usuario ══ -->
{#if editingUser}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="modal-overlay" on:click|self={() => editingUser = null}></div>
  <div class="modal">
    <div class="modal-header">
      <div class="modal-title">{editingUser._isNew ? 'Nuevo usuario' : `Editar: ${editingUser.username}`}</div>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="modal-close" on:click={() => editingUser = null}>✕</div>
    </div>
    <div class="modal-body">
      <div class="form-field">
        <label class="form-label">Usuario <span style="color:var(--red)">*</span></label>
        <input class="form-input" type="text" placeholder="nombre_usuario" bind:value={editingUser.username} disabled={!editingUser._isNew} />
      </div>
      <div class="form-field">
        <label class="form-label">{editingUser._isNew ? 'Contraseña' : 'Nueva contraseña'} {#if editingUser._isNew}<span style="color:var(--red)">*</span>{/if}</label>
        <input class="form-input" type="password" placeholder={editingUser._isNew ? 'Mínimo 8 caracteres' : 'Dejar vacío para no cambiar'} bind:value={editingUser.password} />
      </div>
      <div class="form-field">
        <label class="form-label">Rol</label>
        <select class="form-select" bind:value={editingUser.role}>
          <option value="user">Usuario</option>
          <option value="admin">Administrador</option>
        </select>
      </div>
      <div class="form-field">
        <label class="form-label">Descripción</label>
        <input class="form-input" type="text" placeholder="Opcional" bind:value={editingUser.description} />
      </div>
      {#if userMsg}<div class="share-msg" class:error={userMsgError}>{userMsg}</div>{/if}
    </div>
    <div class="modal-footer">
      <button class="btn-secondary" on:click={() => editingUser = null}>Cancelar</button>
      <button class="btn-accent" on:click={saveUser} disabled={savingUser}>{savingUser ? 'Guardando...' : editingUser._isNew ? 'Crear usuario' : 'Guardar cambios'}</button>
    </div>
  </div>
{/if}

<style>
  /* ── Root ── */
  .s2-root {
    width:100%; height:100%;
    display:flex; overflow:hidden;
    font-family:'Inter',-apple-system,sans-serif;
    color:var(--text-1);
  }

  /* ── Sidebar ── */
  .sidebar {
    width:200px; flex-shrink:0;
    display:flex; flex-direction:column;
    padding:12px 8px;
    background:var(--bg-sidebar);
  }
  .sb-header {
    display:flex; align-items:center; gap:8px;
    padding:28px 10px 16px;
    color:var(--text-1);
  }
  .sb-title { font-size:15px; font-weight:600; color:var(--text-1); }
  .sb-section {
    font-size:9px; font-weight:600; color:var(--text-3);
    text-transform:uppercase; letter-spacing:.08em;
    padding:8px 10px 4px; margin-top:4px;
  }
  .sb-item {
    display:flex; align-items:center; gap:8px;
    padding:7px 10px; border-radius:8px; cursor:pointer;
    font-size:12px; color:var(--text-2);
    border:1px solid transparent; transition:all .15s;
  }
  .sb-item:hover { background:rgba(128,128,128,0.08); color:var(--text-1); }
  .sb-item.active { background:var(--active-bg); color:var(--text-1); border-color:var(--border-hi); }
  .sb-ico { width:15px; height:15px; flex-shrink:0; opacity:.6; display:flex; align-items:center; }
  .sb-ico svg { width:100%; height:100%; }
  .sb-item.active .sb-ico { opacity:1; }
  .sb-bottom { margin-top:auto; border-top:1px solid var(--border); padding-top:8px; }
  .sb-user { display:flex; align-items:center; gap:10px; padding:10px; }
  .sb-avatar {
    width:28px; height:28px; border-radius:7px; flex-shrink:0;
    background:linear-gradient(135deg,var(--accent),var(--accent2));
    display:flex; align-items:center; justify-content:center;
    font-size:11px; font-weight:700; color:#fff;
  }
  .sb-user-name { font-size:12px; font-weight:600; color:var(--text-1); }
  .sb-user-role { font-size:10px; color:var(--text-3); text-transform:uppercase; letter-spacing:.04em; }

  /* ── Inner ── */
  .inner-wrap { flex:1; padding:8px; display:flex; }
  .inner {
    flex:1; border-radius:10px; border:1px solid var(--border);
    background:var(--bg-inner); display:flex; flex-direction:column; overflow:hidden;
  }
  .inner-titlebar {
    display:flex; align-items:center; gap:6px;
    padding:14px 18px 13px;
    background:var(--bg-bar); flex-shrink:0;
    border-bottom:1px solid var(--border);
  }
  .tb-title { font-size:13px; font-weight:600; color:var(--text-1); }
  .tb-sub   { font-size:11px; color:var(--text-3); }
  .inner-content { flex:1; overflow-y:auto; padding:20px; }
  .inner-content::-webkit-scrollbar { width:3px; }
  .inner-content::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }

  /* ── Loading / Empty ── */
  .s-loading { display:flex; align-items:center; justify-content:center; height:120px; }
  .spinner { width:22px; height:22px; border-radius:50%; border:2px solid rgba(255,255,255,0.08); border-top-color:var(--accent); animation:spin .7s linear infinite; }
  @keyframes spin { to { transform:rotate(360deg); } }
  .section-label { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.08em; margin-bottom:12px; }
  .coming-soon { font-size:12px; color:var(--text-3); }
  .empty-state { display:flex; flex-direction:column; align-items:center; gap:8px; padding:40px 0; }
  .empty-icon { width:38px; height:38px; border-radius:9px; background:rgba(124,111,255,0.08); border:1px solid rgba(124,111,255,0.12); display:flex; align-items:center; justify-content:center; }
  .empty-icon svg { width:19px; height:19px; color:var(--accent); opacity:.5; }
  .empty-title { font-size:12px; font-weight:600; color:var(--text-2); }
  .empty-desc  { font-size:11px; color:var(--text-3); text-align:center; max-width:200px; }

  /* ── Sub-tabs ── */
  .sub-tabs { display:flex; border-bottom:1px solid var(--border); margin-bottom:18px; }
  .sub-tab { padding:8px 14px; font-size:11px; font-weight:500; color:var(--text-3); cursor:pointer; border-bottom:2px solid transparent; margin-bottom:-1px; transition:all .15s; }
  .sub-tab:hover { color:var(--text-2); }
  .sub-tab.active { color:var(--accent); border-bottom-color:var(--accent); }

  /* ── Share list ── */
  .share-list { display:flex; flex-direction:column; }
  .share-row {
    display:flex; align-items:center; gap:12px;
    padding:10px 4px; border-bottom:1px solid var(--border);
    transition:background .12s;
  }
  .share-row:first-child { border-top:1px solid var(--border); }
  .share-row:hover { background:rgba(255,255,255,0.025); }
  .share-folder-icon { width:28px; height:28px; flex-shrink:0; border-radius:7px; background:rgba(124,111,255,0.10); display:flex; align-items:center; justify-content:center; }
  .share-folder-icon svg { width:14px; height:14px; color:var(--accent); }
  .share-name { font-size:12px; font-weight:600; color:var(--text-1); min-width:80px; }
  .share-pool { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; flex:1; }
  .share-protocols { display:flex; gap:3px; flex-shrink:0; }
  .proto { padding:2px 6px; border-radius:4px; font-size:9px; font-weight:700; letter-spacing:.04em; color:var(--text-3); background:rgba(255,255,255,0.04); border:1px solid rgba(255,255,255,0.08); transition:all .15s; }
  .proto.smb.on { color:#60a5fa; background:rgba(96,165,250,0.10); border-color:rgba(96,165,250,0.22); }
  .proto.nfs.on { color:#4ade80; background:rgba(74,222,128,0.10); border-color:rgba(74,222,128,0.22); }
  .proto.ftp.on { color:#fbbf24; background:rgba(251,191,36,0.10); border-color:rgba(251,191,36,0.22); }
  .share-users { font-size:10px; color:var(--text-3); flex-shrink:0; min-width:58px; text-align:right; }
  .btn-more { width:30px; height:30px; flex-shrink:0; border-radius:7px; border:1px solid var(--border); background:transparent; color:var(--text-3); cursor:pointer; display:flex; align-items:center; justify-content:center; font-size:15px; font-weight:700; padding-bottom:2px; transition:all .15s; }
  .btn-more:hover { color:var(--text-1); border-color:var(--border-hi); background:var(--active-bg); }

  /* ── Users ── */
  .user-list { display:flex; flex-direction:column; }
  .user-row { display:flex; align-items:center; gap:12px; padding:10px 4px; border-bottom:1px solid var(--border); transition:background .12s; }
  .user-row:first-child { border-top:1px solid var(--border); }
  .user-row:hover { background:rgba(255,255,255,0.025); }
  .user-avatar { width:28px; height:28px; border-radius:7px; flex-shrink:0; background:linear-gradient(135deg,var(--accent),var(--accent2)); display:flex; align-items:center; justify-content:center; font-size:11px; font-weight:700; color:#fff; }
  .user-name { font-size:12px; font-weight:600; color:var(--text-1); min-width:80px; }
  .user-role-label { font-size:10px; color:var(--text-3); text-transform:uppercase; letter-spacing:.04em; flex:1; }
  .user-badge { padding:2px 7px; border-radius:4px; font-size:9px; font-weight:600; text-transform:uppercase; background:var(--ibtn-bg); border:1px solid var(--border); color:var(--text-3); flex-shrink:0; }
  .user-badge.admin { background:rgba(124,111,255,0.12); border-color:rgba(124,111,255,0.30); color:var(--accent); }
  .row-actions { display:flex; gap:3px; opacity:0; transition:opacity .15s; }
  .user-row:hover .row-actions { opacity:1; }
  .action-btn { width:26px; height:26px; border-radius:6px; border:1px solid var(--border); background:transparent; color:var(--text-3); cursor:pointer; display:flex; align-items:center; justify-content:center; transition:all .15s; }
  .action-btn svg { width:12px; height:12px; }
  .action-btn:hover { color:var(--text-1); border-color:var(--border-hi); background:var(--ibtn-bg); }
  .action-btn.danger:hover { color:var(--red); border-color:rgba(248,113,113,0.25); }

  /* ── Buttons ── */
  .btn-accent { display:inline-flex; align-items:center; gap:6px; padding:7px 13px; border-radius:8px; border:none; background:linear-gradient(135deg,var(--accent),var(--accent2)); color:#fff; font-size:11px; font-weight:600; cursor:pointer; font-family:inherit; transition:opacity .15s; }
  .btn-accent:hover { opacity:.88; }
  .btn-accent:disabled { opacity:.5; cursor:not-allowed; }
  .btn-secondary { padding:7px 13px; border-radius:8px; border:1px solid var(--border); background:var(--ibtn-bg); color:var(--text-2); font-size:11px; font-weight:500; cursor:pointer; font-family:inherit; transition:all .15s; }
  .btn-secondary:hover { color:var(--text-1); border-color:var(--border-hi); }
  .btn-secondary:disabled { opacity:.5; cursor:not-allowed; }

  /* ── Updates ── */
  .field-group { display:flex; flex-direction:column; }
  .field-row { display:flex; align-items:center; justify-content:space-between; padding:8px 0; border-bottom:1px solid var(--border); }
  .field-label { font-size:11px; color:var(--text-2); }
  .field-value { font-size:11px; color:var(--text-1); font-family:'DM Mono',monospace; }
  .update-actions { display:flex; gap:8px; margin-top:16px; }
  .update-msg { font-size:11px; margin-top:10px; color:var(--green); }
  .update-msg.error { color:var(--red); }
  .update-progress { display:flex; align-items:center; gap:10px; margin-top:12px; font-size:11px; color:var(--text-2); }

  /* ── Appearance ── */
  .theme-row { display:flex; gap:12px; }
  .theme-card { cursor:pointer; display:flex; flex-direction:column; align-items:center; gap:8px; padding:8px; border-radius:10px; border:2px solid transparent; transition:all .2s; }
  .theme-card:hover { border-color:var(--border); }
  .theme-card.active { border-color:var(--accent); }
  .theme-preview { width:150px; height:90px; border-radius:7px; overflow:hidden; display:flex; border:1px solid rgba(128,128,128,0.15); }
  .theme-preview.midnight { background:#111028; } .theme-preview.dark { background:#181818; } .theme-preview.light { background:#ebebef; }
  .tp-sidebar { width:30%; height:100%; }
  .theme-preview.midnight .tp-sidebar { background:#0d0b20; } .theme-preview.dark .tp-sidebar { background:#141414; } .theme-preview.light .tp-sidebar { background:#e0e0e4; }
  .tp-content { flex:1; padding:8px; display:flex; flex-direction:column; gap:4px; }
  .tp-bar { height:4px; border-radius:2px; width:60%; }
  .theme-preview.midnight .tp-bar { background:rgba(124,111,255,0.4); } .theme-preview.dark .tp-bar { background:rgba(124,111,255,0.35); } .theme-preview.light .tp-bar { background:rgba(91,79,240,0.3); }
  .tp-line { height:3px; border-radius:2px; width:80%; }
  .tp-line.short { width:50%; }
  .theme-preview.midnight .tp-line { background:rgba(255,255,255,0.08); } .theme-preview.dark .tp-line { background:rgba(255,255,255,0.07); } .theme-preview.light .tp-line { background:rgba(0,0,0,0.06); }
  .theme-label { font-size:11px; font-weight:500; color:var(--text-2); }
  .theme-card.active .theme-label { color:var(--text-1); }
  .accent-row { display:flex; gap:10px; }
  .accent-dot { width:26px; height:26px; border-radius:50%; cursor:pointer; border:2px solid transparent; transition:all .15s; box-shadow:0 2px 8px rgba(0,0,0,0.3); }
  .accent-dot:hover { transform:scale(1.15); }
  .accent-dot.active { border-color:var(--text-1); transform:scale(1.15); }

  /* ── Modal ── */
  .modal-overlay { position:fixed; inset:0; z-index:200; background:rgba(0,0,0,0.60); backdrop-filter:blur(3px); }
  .modal { position:fixed; top:50%; left:50%; transform:translate(-50%,-50%); z-index:201; width:460px; max-width:90%; background:var(--bg-inner); border-radius:12px; border:1px solid var(--border); box-shadow:0 24px 60px rgba(0,0,0,0.5); display:flex; flex-direction:column; overflow:hidden; animation:modalIn .2s cubic-bezier(0.16,1,0.3,1) both; }
  @keyframes modalIn { from{opacity:0;transform:translate(-50%,-48%) scale(0.97)} to{opacity:1;transform:translate(-50%,-50%) scale(1)} }
  .modal-header { display:flex; align-items:center; gap:12px; padding:14px 18px; border-bottom:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; }
  .modal-title { font-size:13px; font-weight:600; color:var(--text-1); flex:1; }
  .modal-steps { display:flex; align-items:center; gap:6px; }
  .modal-step { width:20px; height:20px; border-radius:50%; display:flex; align-items:center; justify-content:center; font-size:10px; font-weight:700; background:var(--ibtn-bg); border:1px solid var(--border); color:var(--text-3); transition:all .2s; }
  .modal-step.active { background:var(--accent); border-color:var(--accent); color:#fff; }
  .modal-step.done   { background:var(--green);  border-color:var(--green);  color:#fff; }
  .modal-step-line { width:18px; height:1px; background:var(--border); transition:background .2s; }
  .modal-step-line.done { background:var(--green); }
  .modal-close { width:24px; height:24px; border-radius:6px; cursor:pointer; display:flex; align-items:center; justify-content:center; color:var(--text-3); font-size:11px; background:var(--ibtn-bg); transition:all .15s; }
  .modal-close:hover { color:var(--text-1); }
  .modal-body { padding:18px 20px; overflow-y:auto; max-height:380px; display:flex; flex-direction:column; gap:14px; }
  .modal-body::-webkit-scrollbar { width:3px; }
  .modal-body::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }
  .modal-step-label { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.08em; }
  .modal-footer { display:flex; align-items:center; justify-content:flex-end; gap:8px; padding:12px 18px; border-top:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; }
  .modal-summary { padding:12px 14px; border-radius:8px; border:1px solid var(--border); background:rgba(128,128,128,0.04); }
  .summary-label { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; margin-bottom:8px; }
  .summary-row { display:flex; justify-content:space-between; padding:5px 0; border-bottom:1px solid var(--border); font-size:11px; }
  .summary-row span:first-child { color:var(--text-3); }
  .summary-row span:last-child  { color:var(--text-1); font-family:'DM Mono',monospace; }

  /* ── Forms ── */
  .form-field { display:flex; flex-direction:column; gap:4px; }
  .form-label { font-size:10px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; }
  .form-input, .form-select { padding:8px 12px; border-radius:8px; background:rgba(255,255,255,0.04); border:1px solid var(--border); color:var(--text-1); font-size:12px; font-family:'Inter',sans-serif; outline:none; transition:border-color .2s; }
  .form-input:focus, .form-select:focus { border-color:var(--accent); }
  .form-input::placeholder { color:var(--text-3); }
  .form-select { cursor:pointer; -webkit-appearance:none; appearance:none; background-image:url("data:image/svg+xml,%3Csvg width='10' height='6' viewBox='0 0 10 6' fill='none' xmlns='http://www.w3.org/2000/svg'%3E%3Cpath d='M1 1l4 4 4-4' stroke='%23666' stroke-width='1.5' stroke-linecap='round'/%3E%3C/svg%3E"); background-repeat:no-repeat; background-position:right 12px center; padding-right:32px; }
  .form-select option { background:var(--bg-inner); color:var(--text-1); }
  .share-msg { font-size:11px; padding:4px 0; color:var(--green); }
  .share-msg.error { color:var(--red); }

  /* ── Perm table ── */
  .perm-table { display:flex; flex-direction:column; gap:2px; }
  .perm-header { display:flex; align-items:center; padding:4px 8px; font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; }
  .perm-row { display:flex; align-items:center; gap:8px; padding:7px 8px; border-radius:6px; border:1px solid var(--border); background:var(--ibtn-bg); }
  .perm-col-user { display:flex; align-items:center; gap:8px; flex:1; min-width:0; }
  .perm-col-perm { flex-shrink:0; }
  .perm-avatar { width:22px; height:22px; border-radius:5px; flex-shrink:0; background:linear-gradient(135deg,var(--accent),var(--accent2)); display:flex; align-items:center; justify-content:center; font-size:9px; font-weight:700; color:#fff; }
  .perm-name { font-size:11px; font-weight:600; color:var(--text-1); }
  .perm-admin-tag { font-size:8px; font-weight:600; text-transform:uppercase; letter-spacing:.04em; padding:1px 5px; border-radius:3px; background:rgba(124,111,255,0.12); color:var(--accent); }
  .perm-select { padding:5px 28px 5px 8px; font-size:10px; min-width:140px; }

  /* ── Statusbar ── */
  .statusbar { display:flex; align-items:center; gap:10px; padding:8px 16px; border-top:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; font-size:10px; color:var(--text-3); border-radius:0 0 10px 10px; font-family:'DM Mono',monospace; }
  .status-dot { width:6px; height:6px; border-radius:50%; background:var(--green); box-shadow:0 0 4px rgba(74,222,128,0.6); }

  /* ── Appearance settings ── */
  .setting-row { display:flex; align-items:center; justify-content:space-between; padding:10px 0; border-bottom:1px solid var(--border); }
  .setting-label { font-size:12px; color:var(--text-2); }
  .setting-options { display:flex; gap:4px; flex-wrap:wrap; }
  .opt-btn { padding:5px 12px; border-radius:6px; font-size:11px; border:1px solid var(--border); background:var(--ibtn-bg); color:var(--text-2); cursor:pointer; font-family:inherit; transition:all .15s; }
  .opt-btn:hover { color:var(--text-1); }
  .opt-btn.active { background:var(--active-bg); border-color:var(--border-hi); color:var(--text-1); }
  .opt-btn:disabled { opacity:.4; cursor:not-allowed; }
  .tb-tabs { margin-left:auto; }

  /* ── Wallpapers ── */
  .wall-header { display:flex; align-items:center; justify-content:space-between; margin-bottom:12px; }
  .wall-add-btn {
    display:inline-flex; align-items:center; gap:5px;
    padding:5px 10px; border-radius:6px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    color:var(--text-2); font-size:11px; font-weight:500;
    cursor:pointer; font-family:inherit; transition:all .15s;
  }
  .wall-add-btn svg { width:10px; height:10px; }
  .wall-add-btn:hover { color:var(--text-1); border-color:var(--border-hi); }

  .wall-grid {
    display:grid;
    grid-template-columns:repeat(auto-fill, minmax(180px, 180px));
    gap:10px;
    max-height:320px; overflow-y:auto;
    padding-right:4px;
  }
  .wall-grid::-webkit-scrollbar { width:3px; }
  .wall-grid::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }

  .wall-item {
    position:relative; border-radius:8px; overflow:hidden;
    cursor:pointer;
    border:2px solid transparent;
    transition:all .15s;
    aspect-ratio:16/10;
  }
  .wall-item:hover { border-color:rgba(255,255,255,0.2); }
  .wall-item.active { border-color:var(--accent); }

  .wall-thumb {
    display:block; width:100%; height:100%;
    object-fit:cover;
    position:absolute; inset:0;
  }
  .wall-none { position:absolute; inset:0; display:flex; align-items:center; justify-content:center; background:rgba(255,255,255,0.04); }

  .wall-none {
    width:100%; height:100%;
    background:rgba(255,255,255,0.04);
    display:flex; align-items:center; justify-content:center;
  }

  .wall-check {
    position:absolute; bottom:5px; right:5px;
    width:20px; height:20px; border-radius:50%;
    background:var(--accent);
    display:flex; align-items:center; justify-content:center;
    box-shadow:0 2px 6px rgba(0,0,0,0.4);
  }
  .wall-check svg { width:10px; height:10px; color:#fff; }

  .wall-label {
    position:absolute; bottom:5px; left:6px;
    font-size:9px; color:var(--text-3); font-weight:500;
  }

  /* ── Portal / 2FA ── */
  .twofa-status-card {
    display:flex; align-items:center; gap:14px;
    padding:14px 16px; border-radius:10px;
    border:1px solid var(--border); background:var(--ibtn-bg);
  }
  .twofa-status-card.enabled { border-color:rgba(74,222,128,0.25); background:rgba(74,222,128,0.04); }
  .twofa-status-icon {
    width:36px; height:36px; border-radius:9px; flex-shrink:0;
    background:rgba(128,128,128,0.08); border:1px solid var(--border);
    display:flex; align-items:center; justify-content:center;
  }
  .twofa-status-icon svg { width:18px; height:18px; color:var(--text-3); }
  .twofa-status-icon.enabled { background:rgba(74,222,128,0.10); border-color:rgba(74,222,128,0.25); }
  .twofa-status-icon.enabled svg { color:var(--green); }
  .twofa-status-info { flex:1; min-width:0; }
  .twofa-status-title { font-size:12px; font-weight:600; color:var(--text-1); }
  .twofa-status-desc { font-size:11px; color:var(--text-3); margin-top:2px; }
  .twofa-status-badge {
    padding:3px 9px; border-radius:20px; font-size:9px; font-weight:600;
    background:var(--ibtn-bg); border:1px solid var(--border); color:var(--text-3);
    flex-shrink:0; text-transform:uppercase; letter-spacing:.04em;
  }
  .twofa-status-badge.enabled { background:rgba(74,222,128,0.10); border-color:rgba(74,222,128,0.25); color:var(--green); }

  .twofa-setup { max-width:420px; }
  .twofa-step-label { font-size:11px; font-weight:600; color:var(--text-2); margin-bottom:6px; }
  .twofa-hint { font-size:10px; color:var(--text-3); margin-bottom:14px; }

  .twofa-qr-wrap { display:flex; justify-content:center; margin-bottom:14px; }
  .twofa-qr {
    width:180px; height:180px; padding:10px;
    background:white; border-radius:10px;
    display:flex; align-items:center; justify-content:center;
  }
  .twofa-qr :global(svg) { width:100%; height:100%; }
  .twofa-qr-placeholder {
    width:180px; height:180px; border-radius:10px;
    background:rgba(255,255,255,0.04); border:1px solid var(--border);
    display:flex; align-items:center; justify-content:center;
  }

  .twofa-secret-row {
    display:flex; align-items:center; gap:8px;
    padding:8px 12px; border-radius:8px;
    background:rgba(255,255,255,0.03); border:1px solid var(--border);
  }
  .twofa-secret-label { font-size:10px; color:var(--text-3); flex-shrink:0; }
  .twofa-secret-value {
    font-size:11px; color:var(--text-1); font-family:'DM Mono',monospace;
    letter-spacing:.06em; word-break:break-all; user-select:all;
  }

  .twofa-verify-row { display:flex; align-items:center; gap:8px; }
  .twofa-code-input {
    width:120px; text-align:center; font-size:1.2em;
    letter-spacing:4px; font-family:'DM Mono',monospace;
  }

  .twofa-msg { font-size:11px; padding:8px 0; color:var(--green); }
  .twofa-msg.error { color:var(--red); }

  .twofa-success { display:flex; flex-direction:column; align-items:center; gap:10px; max-width:400px; }
  .twofa-success-icon {
    width:42px; height:42px; border-radius:50%;
    background:rgba(74,222,128,0.12); border:1px solid rgba(74,222,128,0.25);
    display:flex; align-items:center; justify-content:center;
  }
  .twofa-success-icon svg { width:20px; height:20px; color:var(--green); }
  .twofa-success-title { font-size:14px; font-weight:600; color:var(--text-1); }
  .twofa-success-desc { font-size:11px; color:var(--text-3); text-align:center; }
  .backup-codes-grid {
    display:grid; grid-template-columns:repeat(2, 1fr); gap:4px;
    width:100%; margin-top:6px;
  }
  .backup-code {
    padding:6px 10px; border-radius:6px; text-align:center;
    font-family:'DM Mono',monospace; font-size:12px; font-weight:600;
    color:var(--text-1); background:rgba(255,255,255,0.04);
    border:1px solid var(--border); user-select:all;
  }

  .twofa-disable-form { margin-top:12px; max-width:400px; }

  .btn-danger-outline {
    padding:7px 13px; border-radius:8px;
    border:1px solid rgba(248,113,113,0.25); background:rgba(248,113,113,0.06);
    color:var(--red); font-size:11px; font-weight:600;
    cursor:pointer; font-family:inherit; transition:all .15s;
  }
  .btn-danger-outline:hover { background:rgba(248,113,113,0.12); }

  .btn-link {
    background:none; border:none; color:var(--text-3);
    font-size:11px; cursor:pointer; font-family:inherit;
    padding:6px 0; margin-top:8px; transition:color .15s;
  }
  .btn-link:hover { color:var(--text-1); }

  .portal-sep { height:1px; background:var(--border); margin:20px 0; }

  /* ── App Permissions ── */
  .appperm-desc { font-size:11px; color:var(--text-3); margin-bottom:16px; line-height:1.5; }
  .appperm-grid { border:1px solid var(--border); border-radius:8px; overflow:hidden; overflow-x:auto; }
  .appperm-header {
    display:flex; align-items:center;
    background:var(--bg-bar); border-bottom:1px solid var(--border);
    padding:8px 0; min-width:fit-content;
  }
  .appperm-header .appperm-user-col {
    font-size:9px; font-weight:600; color:var(--text-3);
    text-transform:uppercase; letter-spacing:.06em;
  }
  .appperm-header .appperm-app-col {
    font-size:9px; font-weight:600; color:var(--text-3);
    text-transform:uppercase; letter-spacing:.04em;
    text-align:center;
  }
  .appperm-row {
    display:flex; align-items:center;
    padding:8px 0; border-bottom:1px solid var(--border);
    transition:background .12s; min-width:fit-content;
  }
  .appperm-row:last-child { border-bottom:none; }
  .appperm-row:hover { background:rgba(255,255,255,0.02); }
  .appperm-user-col {
    width:140px; flex-shrink:0; padding:0 12px;
    display:flex; align-items:center; gap:8px;
  }
  .appperm-app-col {
    width:90px; flex-shrink:0;
    display:flex; align-items:center; justify-content:center;
  }
  .appperm-avatar {
    width:22px; height:22px; border-radius:5px; flex-shrink:0;
    background:linear-gradient(135deg, var(--accent), var(--accent2));
    display:flex; align-items:center; justify-content:center;
    font-size:9px; font-weight:700; color:#fff;
  }
  .appperm-username { font-size:11px; font-weight:600; color:var(--text-1); }

  .appperm-toggle {
    width:32px; height:18px; border-radius:10px; cursor:pointer;
    background:rgba(128,128,128,0.20); border:1px solid var(--border);
    position:relative; transition:all .2s;
  }
  .appperm-toggle.on {
    background:rgba(74,222,128,0.25); border-color:rgba(74,222,128,0.40);
  }
  .appperm-toggle-dot {
    position:absolute; top:2px; left:2px;
    width:12px; height:12px; border-radius:50%;
    background:var(--text-3); transition:all .2s;
  }
  .appperm-toggle.on .appperm-toggle-dot {
    left:16px; background:var(--green);
  }

  .appperm-msg { font-size:11px; margin-top:8px; color:var(--green); }
  .appperm-msg.error { color:var(--red); }
</style>
