<script>
  import { user } from '$lib/stores/auth.js';
  import TabNav from '$lib/components/TabNav.svelte';
  import NetworkPanel from '$lib/apps/NetworkPanel.svelte';

  let activeTab = 'interfaces';
  let activeSub = 'interfaces';

  // Subsection defaults per tab (same logic as NimSettings)
  const subDefaults = { interfaces:'interfaces', services:'smb', remoteaccess:'ports', security:'firewall' };

  let prevTab = activeTab;
  $: if (activeTab !== prevTab) { prevTab = activeTab; activeSub = subDefaults[activeTab] ?? activeTab; }

  // Sidebar items
  const sidebarItems = [
    { id: 'interfaces',   label: 'Interfaces',    icon: 'iface'   },
    { id: 'services',     label: 'Services',      icon: 'service' },
    { id: 'remoteaccess', label: 'Remote Access',  icon: 'remote'  },
    { id: 'security',     label: 'Security',      icon: 'shield'  },
  ];

  $: userName = $user?.username || 'User';
  $: userRole = $user?.role     || 'user';

  const tabLabel = { interfaces: 'Interfaces', services: 'Services', remoteaccess: 'Remote Access', security: 'Security' };
</script>

<div class="network-app-root">
  <!-- SIDEBAR -->
  <div class="sidebar">
    <div class="sb-header">
      <svg width="22" height="22" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" stroke-linejoin="round" style="color:var(--text-1);flex-shrink:0">
        <circle cx="12" cy="12" r="10"/>
        <line x1="2" y1="12" x2="22" y2="12"/>
        <path d="M12 2a15.3 15.3 0 0 1 4 10 15.3 15.3 0 0 1-4 10 15.3 15.3 0 0 1-4-10 15.3 15.3 0 0 1 4-10z"/>
      </svg>
      <span class="sb-app-title">Network</span>
    </div>

    <div class="sb-search">⌕ Buscar…</div>

    <div class="sb-section">Red</div>
    {#each sidebarItems as item}
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="sb-item" class:active={activeTab === item.id} on:click={() => activeTab = item.id}>
        <span class="sb-ico">
          {#if item.icon === 'iface'}⬡
          {:else if item.icon === 'service'}⚙
          {:else if item.icon === 'remote'}⇄
          {:else if item.icon === 'shield'}⛨
          {:else}●
          {/if}
        </span>
        {item.label}
      </div>
    {/each}

    <div class="sb-bottom">
      <div class="sb-user-card">
        <div class="sb-avatar">{userName[0].toUpperCase()}</div>
        <div class="sb-user-info">
          <div class="sb-user-name">{userName}</div>
          <div class="sb-user-role">{userRole}</div>
        </div>
      </div>
    </div>
  </div>

  <!-- INNER -->
  <div class="inner-wrap">
    <div class="inner">
      <!-- TITLEBAR -->
      <div class="inner-titlebar">
        <div class="tb-title">Network</div>
        <div class="tb-sub">— {tabLabel[activeTab] || ''}</div>

        <div class="tb-tabs">
          <TabNav tabs={[
            { id:'interfaces',   label:'Interfaces'    },
            { id:'services',     label:'Services'      },
            { id:'remoteaccess', label:'Remote Access' },
            { id:'security',     label:'Security'      },
          ]} bind:active={activeTab} />
        </div>
      </div>

      <!-- CONTENT -->
      <div class="inner-content no-pad">
        <NetworkPanel {activeTab} bind:activeSub={activeSub} />
      </div>

      <div class="statusbar">
        <div class="status-dot"></div>
        <span>NimOS Beta 5</span>
      </div>
    </div>
  </div>
</div>

<style>
  .network-app-root {
    width:100%; height:100%;
    display:flex; overflow:hidden;
    font-family:'DM Sans',-apple-system,sans-serif;
    color:var(--text-1);
  }

  /* ── SIDEBAR ── */
  .sidebar {
    width:200px; flex-shrink:0;
    display:flex; flex-direction:column;
    padding:12px 8px;
    background:var(--bg-sidebar);
  }
  .sb-header {
    display:flex; align-items:center; gap:9px;
    padding:28px 10px 14px;
  }
  .sb-app-title { font-size:16px; font-weight:700; color:var(--text-1); }

  .sb-search {
    display:flex; align-items:center; gap:6px;
    padding:6px 10px; border-radius:8px; margin-bottom:10px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    font-size:11px; color:var(--text-3);
  }
  .sb-section {
    font-size:9px; font-weight:600; color:var(--text-3);
    text-transform:uppercase; letter-spacing:.08em;
    padding:0 10px 4px; margin-top:4px;
  }
  .sb-item {
    display:flex; align-items:center; gap:8px;
    padding:7px 10px; border-radius:8px; cursor:pointer;
    font-size:12px; color:var(--text-2);
    border:1px solid transparent; transition:all .15s;
  }
  .sb-item:hover { background:rgba(128,128,128,0.10); color:var(--text-1); }
  .sb-item.active { background:var(--active-bg); color:var(--text-1); border-color:var(--border-hi); }
  .sb-ico { font-size:13px; width:16px; text-align:center; flex-shrink:0; opacity:0.7; }
  .sb-item.active .sb-ico { opacity:1; }

  .sb-bottom { margin-top:auto; border-top:1px solid var(--border); padding-top:8px; }
  .sb-user-card {
    display:flex; align-items:center; gap:10px;
    padding:10px 10px;
  }
  .sb-avatar {
    width:30px; height:30px; border-radius:8px; flex-shrink:0;
    background:linear-gradient(135deg, var(--accent), var(--accent2));
    display:flex; align-items:center; justify-content:center;
    font-size:12px; font-weight:700; color:#fff;
  }
  .sb-user-name { font-size:12px; font-weight:600; color:var(--text-1); }
  .sb-user-role { font-size:10px; color:var(--text-3); text-transform:uppercase; letter-spacing:.04em; }

  /* ── INNER ── */
  .inner-wrap { flex:1; padding:8px; display:flex; }
  .inner {
    flex:1; border-radius:10px; border:1px solid var(--border);
    background:var(--bg-inner); display:flex; flex-direction:column; overflow:hidden;
  }
  .inner-titlebar {
    display:flex; align-items:center; gap:8px;
    padding:14px 16px 12px; background:var(--bg-bar); flex-shrink:0;
  }
  .tb-title { font-size:13px; font-weight:600; color:var(--text-1); }
  .tb-sub { font-size:11px; color:var(--text-3); margin-left:2px; }
  .tb-tabs { margin-left:auto; }
  .inner-content { flex:1; overflow-y:auto; padding:20px; }
  .inner-content.no-pad { padding:0; overflow:hidden; display:flex; flex-direction:column; }
  .inner-content::-webkit-scrollbar { width:3px; }
  .inner-content::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }

  /* ── STATUSBAR ── */
  .statusbar {
    display:flex; align-items:center; gap:12px;
    padding:8px 16px; border-top:1px solid var(--border);
    background:var(--bg-bar); flex-shrink:0; font-size:10px; color:var(--text-3);
    border-radius:0 0 10px 10px; font-family:'DM Mono',monospace;
  }
  .status-dot { width:6px; height:6px; border-radius:50%; background:var(--green); box-shadow:0 0 4px rgba(74,222,128,0.6); }
</style>
