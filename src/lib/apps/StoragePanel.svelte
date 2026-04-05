<script>
  import { onMount } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  export let activeTab = 'disks';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  let loading = true;
  let pools = [];
  let detailPool = null;  // selected pool for detail view
  let poolServices = [];  // services for the selected pool
  let showDestroy = false;
  let destroyInput = '';
  let destroyDeps = [];
  let destroying = false;
  let stoppingService = {};
  let eligible = [];
  let provisioned = [];
  let nvme = [];
  let selectedDisk = null;
  let expandedDisk = null;
  let smartData = {};  // keyed by disk name
  let smartLoading = {};
  let smartLastLoad = 0;  // timestamp of last full load

  async function loadSmartData(diskName) {
    smartLoading = { ...smartLoading, [diskName]: true };
    try {
      const r = await fetch(`/api/disks/smart?disk=${encodeURIComponent(diskName)}`, { headers: hdrs() });
      const d = await r.json();
      smartData = { ...smartData, [diskName]: d };
    } catch {
      smartData = { ...smartData, [diskName]: { error: 'No se pudo cargar', status: 'ok', attributes: [] } };
    }
    smartLoading = { ...smartLoading, [diskName]: false };
  }

  function toggleDiskExpand(diskName) {
    if (expandedDisk === diskName) {
      expandedDisk = null;
    } else {
      expandedDisk = diskName;
      loadSmartData(diskName);
    }
  }

  // Storage capabilities
  let capabilities = { zfs: false, btrfs: false, mdadm: false, recommended: 'btrfs' };

  // Create pool state
  let newPool = { name: '', type: 'btrfs', profile: 'single', disks: [] };
  let creating = false;
  let poolMsg = '';
  let poolMsgError = false;
  let showCreatePool = false;
  let wiping = null;
  let wipeMsg = '';
  let wipeMsgError = false;

  // Restore pool state
  let restorable = [];
  let restorableScanned = false;
  let scanning = false;
  let restoring = false;
  let restoreMsg = '';
  let restoreMsgError = false;

  // ── ZFS: Snapshots ──────────────────────────────────────────────────────────
  let snapshots = [];
  let snapsLoading = false;
  let snapPool = '';
  let newSnapName = '';
  let snapMsg = ''; let snapMsgError = false;

  async function loadSnapshots(pool) {
    if (!pool) return;
    snapsLoading = true;
    try {
      const res = await fetch(`/api/storage/snapshots?pool=${encodeURIComponent(pool)}`, { headers: hdrs() });
      const data = await res.json();
      snapshots = data.snapshots || [];
    } catch { snapshots = []; }
    snapsLoading = false;
  }

  async function createSnap() {
    snapMsg = '';
    const res = await fetch('/api/storage/snapshot', {
      method: 'POST',
      headers: { ...hdrs(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ pool: snapPool, name: newSnapName || undefined }),
    });
    const data = await res.json();
    if (data.ok) { snapMsg = 'Punto de restauración creado'; snapMsgError = false; newSnapName = ''; loadSnapshots(snapPool); }
    else { snapMsg = data.error || 'Error'; snapMsgError = true; }
  }

  let snapCreating = {};  // poolName -> 'loading' | 'done' | 'error'

  async function quickSnapshot(poolName) {
    snapCreating = { ...snapCreating, [poolName]: 'loading' };
    try {
      const res = await fetch('/api/storage/snapshot', {
        method: 'POST',
        headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ pool: poolName }),
      });
      const data = await res.json();
      if (data.ok) {
        snapCreating = { ...snapCreating, [poolName]: 'done' };
        loadRecentActivity();
      } else {
        snapCreating = { ...snapCreating, [poolName]: 'error' };
      }
    } catch {
      snapCreating = { ...snapCreating, [poolName]: 'error' };
    }
    setTimeout(() => { snapCreating = { ...snapCreating, [poolName]: null }; }, 3000);
  }

  async function deleteSnap(snapshot) {
    if (!confirm(`¿Borrar snapshot ${snapshot}?`)) return;
    const res = await fetch('/api/storage/snapshot', {
      method: 'DELETE',
      headers: { ...hdrs(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ snapshot }),
    });
    const data = await res.json();
    if (data.ok) loadSnapshots(snapPool);
    else alert(data.error || 'Error');
  }

  async function rollbackSnap(snapshot) {
    if (!confirm(`¿Rollback a ${snapshot}? Se perderán los cambios posteriores.`)) return;
    const res = await fetch('/api/storage/snapshot/rollback', {
      method: 'POST',
      headers: { ...hdrs(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ snapshot }),
    });
    const data = await res.json();
    if (data.ok) { snapMsg = 'Rollback completado'; snapMsgError = false; loadSnapshots(snapPool); }
    else { snapMsg = data.error || 'Error en rollback'; snapMsgError = true; }
  }

  // ── ZFS: Scrub ──────────────────────────────────────────────────────────────
  let scrubPool = '';
  let scrubStatus = { status: 'idle', progress: 0, errors: 0 };
  let scrubLoading = false;
  let scrubMsg = ''; let scrubMsgError = false;
  let scrubInterval = null;
  let scrubSchedule = { frequency: 'off', hour: 2, minute: 0, dayOfWeek: 0, dayOfMonth: 1 };
  let savingSchedule = false;

  async function loadScrubStatus(pool) {
    if (!pool) return;
    try {
      const res = await fetch(`/api/storage/scrub/status?pool=${encodeURIComponent(pool)}`, { headers: hdrs() });
      scrubStatus = await res.json();
    } catch { scrubStatus = { status: 'idle', progress: 0, errors: 0 }; }
  }

  async function startScrub() {
    scrubMsg = '';
    const res = await fetch('/api/storage/scrub', {
      method: 'POST',
      headers: { ...hdrs(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ pool: scrubPool }),
    });
    const data = await res.json();
    if (data.ok) {
      scrubMsg = 'Verificación iniciada'; scrubMsgError = false;
      if (scrubInterval) clearInterval(scrubInterval);
      scrubInterval = setInterval(() => loadScrubStatus(scrubPool), 3000);
      loadScrubStatus(scrubPool);
    } else { scrubMsg = data.error || 'Error'; scrubMsgError = true; }
  }

  async function startScrubForPool(poolName) {
    const res = await fetch('/api/storage/scrub', {
      method: 'POST',
      headers: { ...hdrs(), 'Content-Type': 'application/json' },
      body: JSON.stringify({ pool: poolName }),
    });
    const data = await res.json();
    if (data.ok) {
      scrubPool = poolName;
      if (scrubInterval) clearInterval(scrubInterval);
      scrubInterval = setInterval(() => loadScrubStatus(poolName), 3000);
      loadScrubStatus(poolName);
      activeTab = 'health';
    } else {
      alert(data.error || 'Error al iniciar verificación');
    }
  }

  async function loadScrubSchedule(poolName) {
    if (!poolName) return;
    try {
      const r = await fetch(`/api/storage/scrub/schedule?pool=${encodeURIComponent(poolName)}`, { headers: hdrs() });
      const d = await r.json();
      scrubSchedule = {
        frequency: d.frequency || 'off',
        hour: d.hour ?? 2,
        minute: d.minute ?? 0,
        dayOfWeek: d.dayOfWeek ?? 0,
        dayOfMonth: d.dayOfMonth ?? 1,
        lastRun: d.lastRun || null,
        nextRun: d.nextRun || null,
      };
    } catch {
      scrubSchedule = { frequency: 'off', hour: 2, minute: 0, dayOfWeek: 0, dayOfMonth: 1 };
    }
  }

  async function saveScrubSchedule() {
    savingSchedule = true;
    try {
      const r = await fetch('/api/storage/scrub/schedule', {
        method: 'POST',
        headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({
          pool: scrubPool,
          frequency: scrubSchedule.frequency,
          hour: scrubSchedule.hour,
          minute: scrubSchedule.minute,
          dayOfWeek: scrubSchedule.dayOfWeek,
          dayOfMonth: scrubSchedule.dayOfMonth,
        }),
      });
      const d = await r.json();
      if (d.ok && d.nextRun) scrubSchedule.nextRun = d.nextRun;
    } catch {}
    savingSchedule = false;
  }

  const dayNames = ['Domingo','Lunes','Martes','Miércoles','Jueves','Viernes','Sábado'];

  // ── Reactive: load ZFS data when tab changes ────────────────────────────────
  $: if (activeTab === 'snapshots' && pools.length > 0) {
    if (!snapPool) snapPool = pools[0]?.name || '';
    loadSnapshots(snapPool);
  }
  $: if ((activeTab === 'health' || activeTab === 'scrub') && pools.length > 0) {
    if (!scrubPool) scrubPool = pools[0]?.name || '';
    loadScrubStatus(scrubPool);
    loadScrubSchedule(scrubPool);
    loadAllDisksSmart();
  }

  let allDisksSmartLoaded = false;

  async function loadAllDisksSmart() {
    const now = Date.now();
    // Refresh every 5 minutes, or if never loaded
    if (allDisksSmartLoaded && (now - smartLastLoad) < 300000) return;
    const disksToCheck = [...provisioned, ...eligible].map(d => d.name);
    await Promise.all(disksToCheck.map(name => loadSmartData(name)));
    allDisksSmartLoaded = true;
    smartLastLoad = now;
  }

  // Compute worst SMART status across all disks
  $: worstSmartStatus = (() => {
    let worst = 'ok';
    for (const d of [...provisioned, ...eligible]) {
      const sd = smartData[d.name];
      if (!sd) continue;
      if (sd.status === 'critical') return 'critical';
      if (sd.status === 'warning') worst = 'warning';
    }
    return worst;
  })();

  // Get list of disks with problems for the warning banner
  $: problemDisks = [...provisioned, ...eligible].filter(d => {
    const sd = smartData[d.name];
    return sd && (sd.status === 'warning' || sd.status === 'critical');
  }).map(d => ({ name: d.name, model: d.model, status: smartData[d.name]?.status, details: smartData[d.name] }));
  $: if (snapPool && activeTab === 'snapshots') loadSnapshots(snapPool);
  $: if (scrubPool && (activeTab === 'health' || activeTab === 'scrub')) loadScrubStatus(scrubPool);

  function fmtDate(raw) {
    if (!raw) return '—';
    // ZFS gives "Thu Mar 26 19:30 2026" — try to parse
    const d = new Date(raw);
    if (!isNaN(d)) return d.toLocaleString('es-ES', { day:'2-digit', month:'short', year:'numeric', hour:'2-digit', minute:'2-digit' });
    return raw;
  }


  async function load() {
    loading = true;
    try {
      const [statusRes, disksRes, capRes] = await Promise.all([
        fetch('/api/storage/status', { headers: hdrs() }),
        fetch('/api/storage/disks',  { headers: hdrs() }),
        fetch('/api/storage/capabilities', { headers: hdrs() }),
      ]);
      const status = await statusRes.json();
      const disks  = await disksRes.json();
      const caps   = await capRes.json();
      pools       = status.pools       || [];
      eligible    = disks.eligible     || [];
      provisioned = disks.provisioned  || [];
      nvme        = disks.nvme         || [];
      capabilities = caps;
      // Set default pool type from recommended
      if (caps.recommended) newPool.type = caps.recommended;
    } catch (e) {
      console.error('[Storage] load failed', e);
    }
    loading = false;
  }

  onMount(load);

  $: totalBytes = [...eligible, ...provisioned, ...nvme].reduce((a, d) => a + (d.size || 0), 0);
  $: usedBytes  = pools.reduce((a, p) => a + (p.used || 0), 0);
  $: totalPoolBytes = pools.reduce((a, p) => a + (p.total || p.size || 0), 0);
  $: usedPct    = totalPoolBytes > 0 ? (usedBytes / totalPoolBytes) * 100 : 0;

  // All physical disks (for resumen)
  $: allDisks = [...provisioned.filter(d => !d.name?.startsWith('nvme')), ...eligible, ...nvme.filter(d => d.name)];

  // Sort pools: degraded/error first
  $: sortedPools = [...pools].sort((a, b) => {
    const order = { 'FAULTED': 0, 'DEGRADED': 1, 'ONLINE': 2, 'active': 2 };
    return (order[a.status] ?? 3) - (order[b.status] ?? 3);
  });

  function poolUsedPct(pool) {
    const total = pool.total || pool.size || 0;
    if (total === 0) return 0;
    return Math.round((pool.used || 0) / total * 100);
  }

  function translateProtection(profile) {
    const map = { mirror: 'Espejo', raidz1: 'Protección simple', raidz2: 'Protección doble', stripe: 'Sin protección', single: 'Disco único', raid1: 'Espejo' };
    return map[profile?.toLowerCase()] || profile || '—';
  }

  // Real activity from notifications
  let recentActivity = [];

  async function loadRecentActivity() {
    try {
      const r = await fetch(`/api/notifications?category=system&limit=4`, { headers: hdrs() });
      const d = await r.json();
      const notifs = d.notifications || [];
      recentActivity = notifs.map(n => {
        const colorMap = { info: 'var(--blue, #60a5fa)', success: 'var(--green)', warning: 'var(--amber, #f59e0b)', error: 'var(--red, #ef4444)' };
        let timeAgo = '—';
        if (n.timestamp) {
          const diff = Date.now() - new Date(n.timestamp).getTime();
          const mins = Math.floor(diff / 60000);
          if (mins < 1) timeAgo = 'Ahora';
          else if (mins < 60) timeAgo = `${mins}m`;
          else if (mins < 1440) timeAgo = `${Math.floor(mins / 60)}h`;
          else timeAgo = `${Math.floor(mins / 1440)}d`;
        }
        return { time: timeAgo, color: colorMap[n.type] || 'var(--text-3)', message: n.title || n.message };
      });
    } catch { recentActivity = []; }
  }

  // Load activity when resumen tab is active
  $: if (activeTab === 'resumen' && !loading) {
    loadRecentActivity();
    loadAllDisksSmart();
  }

  function fmt(bytes) {
    if (!bytes) return '—';
    const tb = bytes / 1e12;
    if (tb >= 1) return tb.toFixed(1) + ' TB';
    return (bytes / 1e9).toFixed(1) + ' GB';
  }

  function openDetail(pool) {
    detailPool = pool;
    activeTab = 'detalle';
    loadPoolServices(pool.name);
  }

  function closeDetail() {
    detailPool = null;
    activeTab = 'resumen';
  }

  async function loadPoolServices(poolName) {
    try {
      const r = await fetch(`/api/services?pool=${encodeURIComponent(poolName)}`, { headers: hdrs() });
      const d = await r.json();
      poolServices = d.services || [];
    } catch { poolServices = []; }
  }

  // Get disks that belong to a specific pool
  function poolDisks(pool) {
    if (!pool.disks || pool.disks.length === 0) return [];
    return pool.disks.map(d => {
      if (typeof d === 'object' && d.name) {
        return d; // Backend already provides { name, model, size, smartStatus }
      }
      // Legacy fallback: plain string
      const n = typeof d === 'string' ? d.replace('/dev/', '') : String(d);
      const found = provisioned.find(p => p.name === n);
      return { name: n, model: found?.model || '—', size: found?.size || 0, smartStatus: 'unknown' };
    });
  }

  async function openDestroy() {
    if (!detailPool) return;
    destroyInput = '';
    destroying = false;
    stoppingService = {};
    try {
      const r = await fetch(`/api/services/dependencies?pool=${encodeURIComponent(detailPool.name)}`, { headers: hdrs() });
      const d = await r.json();
      destroyDeps = d.dependencies || [];
    } catch { destroyDeps = []; }
    showDestroy = true;
  }

  async function stopServiceForDestroy(svc) {
    stoppingService = { ...stoppingService, [svc.id]: true };
    try {
      await fetch(`/api/services/${svc.id}/stop`, { method: 'POST', headers: hdrs() });
      svc.status = 'stopped';
      destroyDeps = [...destroyDeps];
    } catch {}
    stoppingService = { ...stoppingService, [svc.id]: false };
  }

  $: allDepsStopped = destroyDeps.every(d => d.status !== 'running' && d.status !== 'starting');
  $: canDestroy = destroyInput === 'ELIMINAR' && allDepsStopped;

  async function doDestroy() {
    if (!canDestroy || !detailPool) return;
    destroying = true;
    try {
      const r = await fetch('/api/storage/pool/destroy', {
        method: 'POST',
        headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ name: detailPool.name }),
      });
      const d = await r.json();
      if (d.ok) {
        showDestroy = false;
        detailPool = null;
        activeTab = 'resumen';
        await load();
      } else {
        alert(d.error || 'Error al destruir el volumen');
      }
    } catch (e) {
      alert('Error: ' + e.message);
    }
    destroying = false;
  }

  function selectDisk(d) {
    selectedDisk = selectedDisk?.name === d.name ? null : d;
  }

  function toggleDiskSelect(path) {
    if (newPool.disks.includes(path)) {
      newPool.disks = newPool.disks.filter(p => p !== path);
    } else {
      newPool.disks = [...newPool.disks, path];
    }
  }

  async function createPool() {
    if (!newPool.name.trim()) { poolMsg = 'Introduce un nombre'; poolMsgError = true; return; }
    if (newPool.disks.length === 0) { poolMsg = 'Selecciona al menos un disco'; poolMsgError = true; return; }
    creating = true; poolMsg = '';
    try {
      const body = {
        name: newPool.name.trim(),
        type: newPool.type,
        disks: newPool.disks,
      };
      // Add type-specific params
      if (newPool.type === 'btrfs') {
        body.profile = newPool.profile;
      } else if (newPool.type === 'zfs') {
        body.vdevType = newPool.profile;
      } else {
        body.level = newPool.profile;
        body.filesystem = 'ext4';
      }
      const res = await fetch('/api/storage/pool', {
        method: 'POST',
        headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify(body),
      });
      const data = await res.json();
      if (data.ok) {
        poolMsg = `Pool "${newPool.name}" creado correctamente`; poolMsgError = false;
        newPool = { name: '', type: capabilities.recommended || 'btrfs', profile: 'single', disks: [] };
        showCreatePool = false;
        load();
      } else {
        poolMsg = data.error || 'Error al crear pool'; poolMsgError = true;
      }
    } catch (e) { poolMsg = 'Error de conexión'; poolMsgError = true; }
    creating = false;
  }

  async function scanRestorable() {
    scanning = true; restoreMsg = '';
    try {
      const res = await fetch('/api/storage/restorable', { headers: hdrs() });
      const data = await res.json();
      restorable = data.pools || [];
      restorableScanned = true;
    } catch (e) { restoreMsg = 'Error escaneando'; restoreMsgError = true; }
    scanning = false;
  }

  async function restorePool(name) {
    restoring = true; restoreMsg = '';
    try {
      const res = await fetch('/api/storage/pool/restore', {
        method: 'POST',
        headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      });
      const data = await res.json();
      if (data.ok) { restoreMsg = `Pool "${name}" restaurado`; restoreMsgError = false; load(); }
      else { restoreMsg = data.error || 'Error restaurando'; restoreMsgError = true; }
    } catch (e) { restoreMsg = 'Error de conexión'; restoreMsgError = true; }
    restoring = false;
  }

  async function wipeDisk(name) {
    if (!confirm(`¿Wipear /dev/${name}? Se borrarán TODAS las particiones.`)) return;
    wiping = name; wipeMsg = '';
    try {
      const res = await fetch('/api/storage/wipe', {
        method: 'POST',
        headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ disk: `/dev/${name}` }),
      });
      const data = await res.json();
      if (data.ok === true) { wipeMsg = `${name} wipeado correctamente`; wipeMsgError = false; await load(); }
      else { wipeMsg = data.error || 'Error desconocido al wipear'; wipeMsgError = true; }
    } catch (e) { wipeMsg = 'Error de conexión'; wipeMsgError = true; }
    wiping = null;
  }

  async function destroyPool(name) {
    if (!confirm(`¿Destruir pool "${name}"? Esta acción no se puede deshacer.`)) return;
    try {
      const res = await fetch('/api/storage/pool/destroy', {
        method: 'POST',
        headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ name }),
      });
      const data = await res.json();
      if (data.ok) { load(); } else { alert(data.error || 'Error'); }
    } catch (e) { alert('Error de conexión'); }
  }

  $: allHddDisks = [...provisioned.filter(d => !d.name?.startsWith('nvme')), ...eligible];
  $: hddSlots  = Array.from({ length: Math.max(4, allHddDisks.length) }, (_, i) => allHddDisks[i] || null);
  $: nvmeSlots = Array.from({ length: 2 }, (_, i) => nvme[i]      || null);
</script>

<div class="storage-root">
  <div class="s-body">

    {#if loading}
      <div class="s-loading"><div class="spinner"></div></div>

    {:else if activeTab === 'resumen'}

      <!-- ══ RESUMEN ══ -->
      <div class="resumen-scroll">
        {#if pools.length === 0 && eligible.length > 0}
          <!-- Onboarding: no volumes, disks available -->
          <div class="onboard">
            <div class="onboard-icon">💾</div>
            <div class="onboard-title">Configura tu almacenamiento</div>
            <div class="onboard-desc">NimOS ha detectado {eligible.length} disco{eligible.length > 1 ? 's' : ''} disponible{eligible.length > 1 ? 's' : ''}. Crea un volumen para empezar a guardar archivos, instalar apps y hacer copias de seguridad.</div>
            <div class="onboard-disks">
              {#each eligible as d}
                <div class="onboard-disk"><span class="o-dot"></span>{d.name} · {d.model || '—'} · {fmt(d.size)}</div>
              {/each}
            </div>
            <button class="btn-cta" on:click={() => { activeTab = 'disks'; showCreatePool = true; }}>Crear mi primer volumen →</button>
          </div>
        {:else if pools.length === 0}
          <!-- No disks at all -->
          <div class="onboard">
            <div class="onboard-icon">⊘</div>
            <div class="onboard-title">No se detectaron discos</div>
            <div class="onboard-desc">Conecta discos al NAS para empezar a crear volúmenes de almacenamiento.</div>
          </div>
        {:else}
          <!-- Normal resumen with volumes -->
          <!-- Alert banner — dynamic based on SMART + pool status -->
          {#if worstSmartStatus === 'critical'}
            <div class="r-alert r-alert-err">
              <svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>
              Disco en riesgo — {problemDisks.length} disco{problemDisks.length > 1 ? 's' : ''} con errores críticos
            </div>
          {:else if worstSmartStatus === 'warning'}
            <div class="r-alert r-alert-warn">
              <svg viewBox="0 0 24 24"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
              Atención — {problemDisks.length} disco{problemDisks.length > 1 ? 's' : ''} con alertas SMART
            </div>
          {:else if !pools.every(p => p.status === 'active' || p.health === 'ONLINE')}
            <div class="r-alert r-alert-warn">
              <svg viewBox="0 0 24 24"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
              Atención — volumen degradado
            </div>
          {:else}
            <div class="r-alert r-alert-ok">
              <svg viewBox="0 0 24 24"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>
              {pools.length} volumen{pools.length > 1 ? 'es' : ''} activo{pools.length > 1 ? 's' : ''} · {allDisks.length} disco{allDisks.length > 1 ? 's' : ''} sano{allDisks.length > 1 ? 's' : ''}
            </div>
          {/if}

          <!-- Disk warnings card — only if problems -->
          {#if problemDisks.length > 0}
            <div class="r-disk-list" style="border-color:rgba(245,158,11,0.2)">
              {#each problemDisks as pd}
                <div class="r-disk-row">
                  <div class="r-disk-ico" style="background:{pd.status === 'critical' ? 'rgba(239,68,68,0.08)' : 'rgba(245,158,11,0.08)'}">
                    <svg viewBox="0 0 24 24" style="stroke:{pd.status === 'critical' ? 'var(--red)' : 'var(--amber)'}"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="3"/></svg>
                  </div>
                  <div class="r-disk-info">
                    <div class="r-disk-name">{pd.name} · {pd.model || '—'}</div>
                    <div class="r-disk-model" style="color:{pd.status === 'critical' ? 'var(--red)' : 'var(--amber)'}">
                      {#if pd.details?.reallocated > 0}{pd.details.reallocated} sect. reubicados  {/if}
                      {#if pd.details?.pending > 0}{pd.details.pending} pendientes  {/if}
                      {#if pd.details?.uncorrectable > 0}{pd.details.uncorrectable} incorregibles{/if}
                    </div>
                  </div>
                  <span class="r-badge {pd.status === 'critical' ? 'r-badge-err' : 'r-badge-warn'}" style="font-size:10px">
                    {pd.status === 'critical' ? 'Riesgo' : 'Atención'}
                  </span>
                </div>
              {/each}
            </div>
          {/if}

          <div class="r-sec">Volúmenes</div>
          <div class="r-grid">
            <!-- Volume cards -->
            <div class="r-vols">
              {#each sortedPools as pool}
                <div class="r-vol-card {pool.status === 'DEGRADED' ? 'degraded' : pool.status === 'FAULTED' ? 'error' : ''}">
                  <div class="r-vol-top">
                    <div>
                      <div class="r-vol-name">{pool.displayName || pool.name}</div>
                      <div class="r-vol-meta">{translateProtection(pool.profile || pool.vdevType)} · {pool.type?.toUpperCase()} · {pool.disks?.length || '?'} disco{(pool.disks?.length || 0) > 1 ? 's' : ''}</div>
                    </div>
                    <span class="r-badge {pool.status === 'ONLINE' || pool.status === 'active' ? 'r-badge-ok' : pool.status === 'DEGRADED' ? 'r-badge-warn' : 'r-badge-err'}">
                      {pool.status === 'ONLINE' || pool.status === 'active' ? 'Normal' : pool.status === 'DEGRADED' ? 'Degradado' : pool.status || 'Desconocido'}
                    </span>
                  </div>
                  <div class="r-bar"><div class="r-bar-fill" style="width:{poolUsedPct(pool)}%"></div></div>
                  <div class="r-bar-text"><span>{fmt(pool.used || 0)} usados</span><span>{fmt(pool.total || pool.size || 0)} · {poolUsedPct(pool)}%</span></div>
                  <div class="r-vol-info">
                    <span>📁 {pool.shares?.length || 0} carpetas</span>
                  </div>
                  <div class="r-vol-actions">
                    <button class="r-btn" on:click|stopPropagation={() => openDetail(pool)}>Gestionar</button>
                    <button class="r-btn r-btn-primary r-snap-btn" class:loading={snapCreating[pool.name] === 'loading'} class:done={snapCreating[pool.name] === 'done'} class:fail={snapCreating[pool.name] === 'error'} disabled={snapCreating[pool.name] === 'loading'} on:click|stopPropagation={() => quickSnapshot(pool.name)}>
                      {#if snapCreating[pool.name] === 'loading'}
                        <span class="r-snap-spinner"></span> Creando...
                      {:else if snapCreating[pool.name] === 'done'}
                        <span class="r-snap-tick">✓</span> Creado
                      {:else if snapCreating[pool.name] === 'error'}
                        <span class="r-snap-fail">✕</span> Error
                      {:else}
                        + Punto de restauración
                      {/if}
                    </button>
                  </div>
                </div>
              {/each}
            </div>

            <!-- Activity -->
            <div class="r-activity-card">
              <div class="r-sec">Actividad reciente</div>
              {#if recentActivity.length > 0}
                {#each recentActivity.slice(0, 4) as act}
                  <div class="r-act-item">
                    <span class="r-act-time">{act.time}</span>
                    <span class="r-act-dot" style="background:{act.color}"></span>
                    <span class="r-act-msg">{act.message}</span>
                  </div>
                {/each}
              {:else}
                <div class="r-act-item"><span class="r-act-msg" style="color:var(--text-3)">Sin actividad reciente</span></div>
              {/if}
            </div>
          </div>

          <!-- Disks summary -->
          <div class="r-sec" style="margin-top:16px">Discos físicos</div>
          <div class="r-disk-list">
            {#each allDisks as d}
              {@const sd = smartData[d.name]}
              <div class="r-disk-row" on:click={() => activeTab = 'disks'} style="cursor:pointer">
                <div class="r-disk-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="3"/></svg></div>
                <div class="r-disk-info">
                  <div class="r-disk-name">{d.name}</div>
                  <div class="r-disk-model">{d.model || '—'} · {fmt(d.size)}{sd?.temperature ? ' · ' + sd.temperature + '°C' : ''}</div>
                </div>
                {#if sd?.status === 'critical'}
                  <span class="r-badge r-badge-err" style="font-size:10px">Riesgo</span>
                {:else if sd?.status === 'warning'}
                  <span class="r-badge r-badge-warn" style="font-size:10px">Atención</span>
                {:else}
                  <span class="r-badge r-badge-ok" style="font-size:10px">Sano</span>
                {/if}
              </div>
            {/each}
          </div>

          <!-- Capacity -->
          <div class="r-cap" style="margin-top:16px">
            <div class="r-sec">Capacidad total</div>
            <div class="r-bar"><div class="r-bar-fill" style="width:{usedPct.toFixed(0)}%"></div></div>
            <div class="r-bar-text"><span>{fmt(usedBytes)} usados de {fmt(totalPoolBytes)}</span><span>{usedPct.toFixed(0)}%</span></div>
          </div>
        {/if}
      </div>

    {:else if activeTab === 'detalle' && detailPool}

      <!-- ══ DETALLE VOLUMEN ══ -->
      <div class="resumen-scroll">
        <!-- Back button -->
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="r-back" on:click={closeDetail}>← Volver a Resumen</div>

        <!-- Info + Space grid -->
        <div class="r-detail-grid">
          <div class="r-detail-card">
            <div class="r-sec">Información</div>
            <div class="r-detail-rows">
              <div class="r-detail-row">
                <span class="r-detail-key">Estado</span>
                <span class="r-detail-val" style="color:var(--green);font-weight:600">
                  {detailPool.status === 'ONLINE' || detailPool.status === 'active' ? '● Normal' : detailPool.status === 'DEGRADED' ? '● Degradado' : detailPool.status || '—'}
                </span>
              </div>
              <div class="r-detail-row">
                <span class="r-detail-key">Protección</span>
                <span class="r-detail-val">{translateProtection(detailPool.profile || detailPool.vdevType)} ({detailPool.disks?.length || '?'} discos)</span>
              </div>
              <div class="r-detail-row">
                <span class="r-detail-key">Sistema</span>
                <span class="r-detail-val">{detailPool.type?.toUpperCase() || '—'}</span>
              </div>
              <div class="r-detail-row">
                <span class="r-detail-key">Nombre</span>
                <span class="r-detail-val">{detailPool.name}</span>
              </div>
            </div>
          </div>

          <div class="r-detail-card">
            <div class="r-sec">Espacio</div>
            <div class="r-bar"><div class="r-bar-fill" style="width:{poolUsedPct(detailPool)}%"></div></div>
            <div class="r-bar-text"><span>{fmt(detailPool.used || 0)}</span><span>{fmt(detailPool.total || detailPool.size || 0)} · {poolUsedPct(detailPool)}%</span></div>
            {#if detailPool.shares && detailPool.shares.length > 0}
              <div style="margin-top:14px">
                {#each detailPool.shares as share}
                  <div class="r-use-row">
                    <span class="r-use-name">{share.name || share}</span>
                    <span class="r-use-size">{fmt(share.used || 0)}</span>
                  </div>
                {/each}
              </div>
            {/if}
          </div>
        </div>

        <!-- Disks in this volume -->
        <div class="r-detail-card" style="margin-top:14px">
          <div class="r-sec">Discos en este volumen</div>
          {#each poolDisks(detailPool) as d}
            <div class="r-disk-row">
              <div class="r-disk-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="3"/></svg></div>
              <div class="r-disk-info">
                <div class="r-disk-name">{d.name} · {d.model || '—'}</div>
                <div class="r-disk-model">{typeof d.size === 'string' ? d.size : fmt(d.size)}</div>
              </div>
              {#if d.smartStatus === 'missing'}
                <span class="r-badge r-badge-err" style="font-size:10px">No detectado</span>
              {:else if d.smartStatus === 'critical'}
                <span class="r-badge r-badge-err" style="font-size:10px">Riesgo</span>
              {:else if d.smartStatus === 'warning'}
                <span class="r-badge r-badge-warn" style="font-size:10px">Atención</span>
              {:else if d.smartStatus === 'unknown'}
                <span class="r-badge" style="font-size:10px;background:var(--ibtn-bg);color:var(--text-3)">Sin datos</span>
              {:else}
                <span class="r-badge r-badge-ok" style="font-size:10px">Sano</span>
              {/if}
            </div>
          {:else}
            <div style="font-size:11px;color:var(--text-3);padding:8px 0">Información de discos no disponible</div>
          {/each}
        </div>

        <!-- Services -->
        {#if poolServices.length > 0}
          <div class="r-detail-card" style="margin-top:14px">
            <div class="r-sec">Servicios que usan este volumen</div>
            {#each poolServices as svc}
              <div class="r-svc-row">
                <span class="r-svc-dot" style="background:{svc.status === 'running' ? 'var(--green)' : 'var(--text-3)'}"></span>
                <span class="r-svc-name">{svc.appName || svc.appId}</span>
                <span class="r-svc-status">{svc.status === 'running' ? 'activo' : svc.status}</span>
              </div>
            {/each}
          </div>
        {/if}

        <!-- Actions -->
        <div class="r-sec" style="margin-top:14px">Acciones</div>
        <div class="r-actions-row">
          <button class="r-btn" on:click={() => startScrubForPool(detailPool.name)}>Verificar integridad</button>
          <button class="r-btn r-btn-primary r-snap-btn" class:loading={snapCreating[detailPool.name] === 'loading'} class:done={snapCreating[detailPool.name] === 'done'} class:fail={snapCreating[detailPool.name] === 'error'} disabled={snapCreating[detailPool.name] === 'loading'} on:click={() => quickSnapshot(detailPool.name)}>
            {#if snapCreating[detailPool.name] === 'loading'}
              <span class="r-snap-spinner"></span> Creando...
            {:else if snapCreating[detailPool.name] === 'done'}
              <span class="r-snap-tick">✓</span> Creado
            {:else if snapCreating[detailPool.name] === 'error'}
              <span class="r-snap-fail">✕</span> Error
            {:else}
              Crear punto de restauración
            {/if}
          </button>
          <button class="r-btn r-btn-danger" on:click={openDestroy}>Destruir volumen</button>
        </div>
      </div>

    {:else if activeTab === 'disks'}

      <!-- ══ DISCOS — New table view ══ -->
      <div class="resumen-scroll">

        <!-- Disk table -->
        <div class="r-detail-card">
          <div class="r-sec">Discos instalados</div>
          <table class="r-disk-table">
            <thead>
              <tr>
                <th>Disco</th>
                <th>Modelo</th>
                <th>Tamaño</th>
                <th>Temp</th>
                <th>Estado</th>
                <th>Volumen</th>
              </tr>
            </thead>
            <tbody>
              {#each [...provisioned, ...eligible] as disk}
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <tr class="r-disk-tr" class:expanded={expandedDisk === disk.name} on:click={() => toggleDiskExpand(disk.name)}>
                  <td class="r-dt-name">{disk.name}</td>
                  <td class="r-dt-model">{disk.model || '—'}</td>
                  <td>{fmt(disk.size)}</td>
                  <td class="r-dt-mono">{smartData[disk.name]?.temperature ?? '—'}{smartData[disk.name]?.temperature ? '°C' : ''}</td>
                  <td>
                    {#if disk.classification === 'provisioned'}
                      {@const sd = smartData[disk.name]}
                      {#if sd?.status === 'critical'}
                        <span class="r-dt-badge r-dt-err">Riesgo</span>
                      {:else if sd?.status === 'warning'}
                        <span class="r-dt-badge r-dt-warn">Atención</span>
                      {:else if disk.classification === 'provisioned'}
                        <span class="r-dt-badge r-dt-ok">Sano</span>
                      {/if}
                    {:else}
                      <span class="r-dt-badge r-dt-free">Libre</span>
                    {/if}
                  </td>
                  <td class="r-dt-mono">{disk.poolName || (disk.classification === 'provisioned' ? 'En uso' : '—')}</td>
                </tr>
                {#if expandedDisk === disk.name}
                  <tr class="r-disk-detail-tr">
                    <td colspan="6">
                      <div class="r-disk-detail">
                        {#if smartLoading[disk.name]}
                          <div style="font-size:11px;color:var(--text-3);padding:8px 0">Cargando datos SMART...</div>
                        {:else if smartData[disk.name]}
                          {@const sd = smartData[disk.name]}
                          <div class="r-dd-row"><span class="r-dd-key">Serial</span><span class="r-dd-val">{sd.serial || disk.serial || '—'}</span></div>
                          <div class="r-dd-row"><span class="r-dd-key">Tipo</span><span class="r-dd-val">{disk.rota ? 'HDD' : 'SSD'}</span></div>
                          {#if disk.transport}<div class="r-dd-row"><span class="r-dd-key">Interfaz</span><span class="r-dd-val">{disk.transport?.toUpperCase()}</span></div>{/if}
                          <div class="r-dd-row"><span class="r-dd-key">Temperatura</span><span class="r-dd-val">{sd.temperature ?? '—'}{sd.temperature ? '°C' : ''}</span></div>
                          <div class="r-dd-row"><span class="r-dd-key">Horas encendido</span><span class="r-dd-val">{sd.powerOnHours != null ? sd.powerOnHours.toLocaleString() : '—'}</span></div>
                          <div class="r-dd-row"><span class="r-dd-key">Ciclos de encendido</span><span class="r-dd-val">{sd.powerCycles ?? '—'}</span></div>
                          <div class="r-dd-row">
                            <span class="r-dd-key">Sectores reubicados</span>
                            <span class="r-dd-val" style="color:{sd.reallocated > 0 ? 'var(--amber)' : 'var(--green)'}">{sd.reallocated ?? 0}</span>
                          </div>
                          <div class="r-dd-row">
                            <span class="r-dd-key">Sectores pendientes</span>
                            <span class="r-dd-val" style="color:{sd.pending > 0 ? 'var(--amber)' : 'var(--green)'}">{sd.pending ?? 0}</span>
                          </div>
                          <div class="r-dd-row">
                            <span class="r-dd-key">Errores incorregibles</span>
                            <span class="r-dd-val" style="color:{sd.uncorrectable > 0 ? 'var(--red)' : 'var(--green)'}">{sd.uncorrectable ?? 0}</span>
                          </div>
                          {#if sd.firmware}
                            <div class="r-dd-row"><span class="r-dd-key">Firmware</span><span class="r-dd-val">{sd.firmware}</span></div>
                          {/if}

                          {#if sd.attributes && sd.attributes.length > 0}
                            <div class="r-smart-toggle" on:click|stopPropagation={() => { sd._showAll = !sd._showAll; smartData = smartData; }}>
                              {sd._showAll ? '▾ Ocultar atributos SMART' : '▸ Ver todos los atributos SMART'}
                            </div>
                            {#if sd._showAll}
                              <table class="r-smart-table">
                                <thead><tr><th>ID</th><th>Atributo</th><th>Valor</th><th>Peor</th><th>Umbral</th><th>Raw</th><th>Estado</th></tr></thead>
                                <tbody>
                                {#each sd.attributes as attr}
                                  <tr>
                                    <td>{attr.id}</td>
                                    <td>{attr.name}</td>
                                    <td>{attr.value}</td>
                                    <td>{attr.worst}</td>
                                    <td>{attr.thresh || '—'}</td>
                                    <td>{attr.raw}</td>
                                    <td style="color:{attr.status === 'ok' ? 'var(--green)' : attr.status === 'warning' ? 'var(--amber)' : 'var(--red)'}">
                                      {attr.status === 'ok' ? '●' : attr.status === 'warning' ? '⚠' : '✕'}
                                    </td>
                                  </tr>
                                {/each}
                                </tbody>
                              </table>
                            {/if}
                          {/if}

                          {#if sd.error}
                            <div style="font-size:10px;color:var(--text-3);margin-top:6px">{sd.error}</div>
                          {/if}
                        {:else}
                          <div class="r-dd-row"><span class="r-dd-key">Serial</span><span class="r-dd-val">{disk.serial || '—'}</span></div>
                          <div class="r-dd-row"><span class="r-dd-key">Tipo</span><span class="r-dd-val">{disk.rota ? 'HDD' : 'SSD'}</span></div>
                        {/if}
                        {#if disk.classification !== 'provisioned'}
                          <div style="margin-top:8px">
                            <button class="r-btn r-btn-danger" style="font-size:10px;padding:5px 10px" on:click|stopPropagation={() => wipeDisk(disk.name)} disabled={wiping === disk.name}>
                              {wiping === disk.name ? 'Limpiando...' : 'Limpiar disco'}
                            </button>
                          </div>
                        {/if}
                      </div>
                    </td>
                  </tr>
                {/if}
              {/each}
              {#if nvme.length > 0}
                {#each nvme as disk}
                  <tr class="r-disk-tr">
                    <td class="r-dt-name">{disk.name}</td>
                    <td class="r-dt-model">{disk.model || '—'}</td>
                    <td>{fmt(disk.size)}</td>
                    <td class="r-dt-mono">—</td>
                    <td><span class="r-dt-badge r-dt-ok">Sano</span></td>
                    <td class="r-dt-mono">{disk.poolName || '—'}</td>
                  </tr>
                {/each}
              {/if}
              {#if eligible.length === 0 && provisioned.length === 0 && nvme.length === 0}
                <tr><td colspan="6" style="text-align:center;color:var(--text-3);padding:20px">No se detectaron discos</td></tr>
              {/if}
            </tbody>
          </table>

          {#if wipeMsg}
            <div class="pool-msg" class:error={wipeMsgError} style="margin-top:8px">{wipeMsg}</div>
          {/if}
        </div>

        <!-- Create Pool section — only if free disks -->
        {#if eligible.length > 0}
          <div class="r-detail-card" style="margin-top:14px">
            {#if !showCreatePool}
              <div class="r-sec">Crear volumen</div>
              <div style="font-size:11px;color:var(--text-3);margin-bottom:10px">Hay {eligible.length} disco{eligible.length > 1 ? 's' : ''} disponible{eligible.length > 1 ? 's' : ''} para crear un nuevo volumen.</div>
              <button class="r-btn r-btn-primary" on:click={() => showCreatePool = true}>+ Crear volumen</button>
            {:else}
              <div class="r-sec">Nuevo volumen</div>
              <div class="r-create-form">
                <div class="r-form-field">
                  <label class="r-form-label">Nombre</label>
                  <input class="r-form-input" type="text" placeholder="mi-volumen" bind:value={newPool.name}>
                </div>

                <div class="r-form-row">
                  <div class="r-form-field" style="flex:1">
                    <label class="r-form-label">Sistema de archivos</label>
                    <div class="r-sched-btns" style="margin-top:4px">
                      <!-- svelte-ignore a11y_click_events_have_key_events -->
                      <!-- svelte-ignore a11y_no_static_element_interactions -->
                      {#if capabilities.zfs}
                        <div class="r-sched-btn" class:active={newPool.type === 'zfs'} on:click={() => newPool.type = 'zfs'}>ZFS {capabilities.recommended === 'zfs' ? '(rec.)' : ''}</div>
                      {/if}
                      {#if capabilities.btrfs}
                        <div class="r-sched-btn" class:active={newPool.type === 'btrfs'} on:click={() => newPool.type = 'btrfs'}>BTRFS {capabilities.recommended === 'btrfs' ? '(rec.)' : ''}</div>
                      {/if}
                    </div>
                  </div>
                  <div class="r-form-field" style="flex:1">
                    <label class="r-form-label">Protección</label>
                    <div class="r-sched-btns" style="margin-top:4px">
                      <!-- svelte-ignore a11y_click_events_have_key_events -->
                      <!-- svelte-ignore a11y_no_static_element_interactions -->
                      {#if newPool.type === 'zfs'}
                        <div class="r-sched-btn" class:active={newPool.profile === 'mirror'} on:click={() => newPool.profile = 'mirror'}>Espejo</div>
                        <div class="r-sched-btn" class:active={newPool.profile === 'stripe'} on:click={() => newPool.profile = 'stripe'}>Sin protección</div>
                        {#if eligible.length >= 3}<div class="r-sched-btn" class:active={newPool.profile === 'raidz1'} on:click={() => newPool.profile = 'raidz1'}>RAIDZ1</div>{/if}
                      {:else}
                        <div class="r-sched-btn" class:active={newPool.profile === 'raid1'} on:click={() => newPool.profile = 'raid1'}>Espejo</div>
                        <div class="r-sched-btn" class:active={newPool.profile === 'single'} on:click={() => newPool.profile = 'single'}>Sin protección</div>
                      {/if}
                    </div>
                  </div>
                </div>

                <div class="r-form-field">
                  <label class="r-form-label">Seleccionar discos</label>
                  <div class="r-disk-select">
                    {#each eligible as disk}
                      <!-- svelte-ignore a11y_click_events_have_key_events -->
                      <!-- svelte-ignore a11y_no_static_element_interactions -->
                      <div class="r-dsel-row" class:selected={newPool.disks.includes(disk.path)} on:click={() => toggleDiskSelect(disk.path)}>
                        <div class="r-dsel-chk">{newPool.disks.includes(disk.path) ? '✓' : ''}</div>
                        <span class="r-dt-name">{disk.name}</span>
                        <span class="r-dt-model">{disk.model || '—'}</span>
                        <span style="margin-left:auto">{fmt(disk.size)}</span>
                      </div>
                    {/each}
                  </div>
                </div>

                <div class="r-form-actions">
                  <button class="r-btn r-btn-primary" on:click={createPool} disabled={creating}>
                    {creating ? 'Creando...' : 'Crear volumen'}
                  </button>
                  <button class="r-btn" on:click={() => showCreatePool = false}>Cancelar</button>
                </div>

                {#if poolMsg}
                  <div class="pool-msg" class:error={poolMsgError} style="margin-top:8px">{poolMsg}</div>
                {/if}
              </div>
            {/if}
          </div>
        {/if}
      </div>

    {:else if activeTab === 'pools'}

      <!-- Existing pools -->
      {#if pools.length > 0}
        <div class="section-label">Pools activos</div>
        {#each pools as pool}
          <div class="pool-row">
            <div class="pool-led" class:healthy={pool.status === 'active'}></div>
            <div class="pool-info">
              <div class="pool-name">
                {pool.name}
                {#if pool.isPrimary}<span class="pool-primary">(principal)</span>{/if}
              </div>
              <div class="pool-meta">{pool.type || pool.filesystem || 'ext4'} · {pool.raidLevel || pool.profile || 'single'} · {pool.mountPoint || '—'} · {pool.totalFormatted || fmt(pool.total)}</div>
            </div>
            <div class="pool-badge" class:green={pool.status === 'active'}>{pool.status || '—'}</div>
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <span class="pool-destroy" on:click={() => destroyPool(pool.name)} title="Eliminar pool">✕</span>
          </div>
        {/each}
        <div class="pool-sep"></div>
      {/if}

      <!-- Available disks -->
      <div class="section-label">Discos disponibles</div>
      <div class="disk-card-list">
        {#each [...provisioned, ...eligible, ...nvme] as disk}
          <div class="disk-card">
            <div class="disk-card-info">
              <div class="disk-card-led" style="background:{disk.classification === 'provisioned' ? 'var(--green)' : 'var(--text-3)'}"></div>
              <div class="disk-card-name">{disk.name}</div>
              <div class="disk-card-model">{disk.model || '—'}</div>
              <div class="disk-card-size">{fmt(disk.size)}</div>
              <div class="disk-card-status">
                {#if disk.classification === 'provisioned'}
                  <span class="disk-tag green">En pool{disk.poolName ? `: ${disk.poolName}` : ''}</span>
                {:else if disk.partitions?.length > 0}
                  <span class="disk-tag amber">Con particiones</span>
                {:else}
                  <span class="disk-tag">Libre</span>
                {/if}
              </div>
            </div>
            {#if disk.classification !== 'provisioned'}
              <button class="disk-wipe-btn" on:click={() => wipeDisk(disk.name)} disabled={wiping === disk.name}>
                {wiping === disk.name ? '...' : 'Wipe'}
              </button>
            {/if}
          </div>
        {/each}
        {#if eligible.length === 0 && provisioned.length === 0 && nvme.length === 0}
          <p class="coming-soon">No se detectaron discos</p>
        {/if}
      </div>

      {#if wipeMsg}
        <div class="pool-msg" class:error={wipeMsgError} style="margin-top:8px">{wipeMsg}</div>
      {/if}

      <!-- Create Pool — only show if there are free disks -->
      {#if eligible.length > 0}
        <div class="pool-sep"></div>

        {#if !showCreatePool}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="create-pool-btn" on:click={() => showCreatePool = true}>
            + Crear Pool
          </div>
        {:else}
          <div class="section-label">Crear nuevo pool</div>
          <div class="create-form">
            <div class="form-field">
              <label class="form-label">Nombre</label>
              <input class="form-input" type="text" placeholder="main-storage" bind:value={newPool.name} />
            </div>

            <div class="form-row">
              <div class="form-field" style="flex:1">
                <label class="form-label">Filesystem</label>
                <select class="form-select" bind:value={newPool.type}>
                  {#if capabilities.btrfs}
                    <option value="btrfs">Btrfs {capabilities.recommended === 'btrfs' ? '(recomendado)' : ''}</option>
                  {/if}
                  {#if capabilities.zfs}
                    <option value="zfs">ZFS {capabilities.recommended === 'zfs' ? '(recomendado)' : ''}</option>
                  {/if}
                  {#if capabilities.mdadm}
                    <option value="mdadm">ext4 (legacy)</option>
                  {/if}
                </select>
              </div>
              <div class="form-field" style="flex:1">
                <label class="form-label">Protección</label>
                <select class="form-select" bind:value={newPool.profile}>
                  {#if newPool.type === 'btrfs'}
                    <option value="single">Single</option>
                    <option value="raid1">RAID 1 (mirror)</option>
                    <option value="raid0">RAID 0 (stripe)</option>
                    <option value="raid10">RAID 10</option>
                  {:else if newPool.type === 'zfs'}
                    <option value="stripe">Single / Stripe</option>
                    <option value="mirror">Mirror (RAID 1)</option>
                    <option value="raidz1">RAIDZ1 (RAID 5)</option>
                    <option value="raidz2">RAIDZ2 (RAID 6)</option>
                  {:else}
                    <option value="single">Single</option>
                    <option value="0">RAID 0</option>
                    <option value="1">RAID 1</option>
                    <option value="5">RAID 5</option>
                    <option value="6">RAID 6</option>
                    <option value="10">RAID 10</option>
                  {/if}
                </select>
              </div>
            </div>

            <div class="form-field">
              <label class="form-label">Seleccionar discos</label>
              <div class="disk-select-list">
                {#each eligible as disk}
                  <!-- svelte-ignore a11y_click_events_have_key_events -->
                  <!-- svelte-ignore a11y_no_static_element_interactions -->
                  <div class="disk-select-row" class:selected={newPool.disks.includes(disk.path)} on:click={() => toggleDiskSelect(disk.path)}>
                    <div class="dsr-check">{newPool.disks.includes(disk.path) ? '✓' : ''}</div>
                    <div class="dsr-name">{disk.name}</div>
                    <div class="dsr-model">{disk.model || '—'}</div>
                    <div class="dsr-size">{fmt(disk.size)}</div>
                  </div>
                {/each}
              </div>
            </div>

            <div class="form-actions">
              <button class="btn-accent" on:click={createPool} disabled={creating}>
                {creating ? 'Creando...' : 'Crear Pool'}
              </button>
              <button class="btn-secondary" on:click={() => showCreatePool = false}>Cancelar</button>
            </div>

            {#if poolMsg}
              <div class="pool-msg" class:error={poolMsgError}>{poolMsg}</div>
            {/if}
          </div>
        {/if}
      {/if}

    {:else if activeTab === 'health'}

      <!-- ══ SALUD ══ -->
      <div class="resumen-scroll">
        <!-- Health overview hero -->
        {#if scrubStatus.status === 'scrubbing'}
          <div class="r-health-hero r-health-checking">
            <div class="r-hh-icon checking">
              <svg viewBox="0 0 24 24"><path d="M22 12h-4l-3 9L9 3l-3 9H2"/></svg>
            </div>
            <div>
              <div class="r-hh-title">Verificando integridad</div>
              <div class="r-hh-sub">Comprobando datos... {scrubStatus.progress || 0}% completado</div>
            </div>
          </div>
        {:else if worstSmartStatus === 'critical'}
          <div class="r-health-hero r-health-err">
            <div class="r-hh-icon err">
              <svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><line x1="15" y1="9" x2="9" y2="15"/><line x1="9" y1="9" x2="15" y2="15"/></svg>
            </div>
            <div>
              <div class="r-hh-title">Disco en riesgo</div>
              <div class="r-hh-sub">{problemDisks.length} disco{problemDisks.length > 1 ? 's' : ''} con errores críticos — planifica su reemplazo</div>
            </div>
          </div>
        {:else if worstSmartStatus === 'warning' || !pools.every(p => p.status === 'active' || p.health === 'ONLINE')}
          <div class="r-health-hero r-health-warn">
            <div class="r-hh-icon warn">
              <svg viewBox="0 0 24 24"><path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/></svg>
            </div>
            <div>
              <div class="r-hh-title">Atención requerida</div>
              <div class="r-hh-sub">
                {#if problemDisks.length > 0}
                  {problemDisks.length} disco{problemDisks.length > 1 ? 's' : ''} con alertas SMART
                {:else}
                  Uno o más volúmenes necesitan revisión
                {/if}
              </div>
            </div>
          </div>
        {:else}
          <div class="r-health-hero r-health-ok">
            <div class="r-hh-icon">
              <svg viewBox="0 0 24 24"><path d="M22 11.08V12a10 10 0 1 1-5.93-9.14"/><polyline points="22 4 12 14.01 9 11.01"/></svg>
            </div>
            <div>
              <div class="r-hh-title">Todo correcto</div>
              <div class="r-hh-sub">Todos los volúmenes y discos funcionan con normalidad</div>
            </div>
          </div>
        {/if}

        <!-- Disk SMART warnings -->
        {#if problemDisks.length > 0}
          <div class="r-detail-card r-smart-alert">
            <div class="r-sec">Alertas de disco</div>
            {#each problemDisks as pd}
              <div class="r-smart-alert-row">
                <span class="r-dt-badge {pd.status === 'critical' ? 'r-dt-err' : 'r-dt-warn'}">
                  {pd.status === 'critical' ? 'Riesgo' : 'Atención'}
                </span>
                <span class="r-smart-alert-name">{pd.name}</span>
                <span class="r-smart-alert-model">{pd.model || '—'}</span>
                <span class="r-smart-alert-detail">
                  {#if pd.details?.reallocated > 0}{pd.details.reallocated} sect. reubicados{/if}
                  {#if pd.details?.pending > 0} · {pd.details.pending} pendientes{/if}
                  {#if pd.details?.uncorrectable > 0} · {pd.details.uncorrectable} incorregibles{/if}
                </span>
              </div>
            {/each}
          </div>
        {/if}

        <!-- Per-pool scrub status -->
        <div class="r-sec" style="margin-top:4px">Volúmenes</div>
        {#each pools as pool}
          {@const isActive = scrubPool === pool.name}
          {@const st = isActive ? scrubStatus : { status: 'idle' }}
          <div class="r-detail-card">
            <div style="display:flex;justify-content:space-between;align-items:center;margin-bottom:10px">
              <div>
                <div style="font-size:14px;font-weight:600;color:var(--text-1)">{pool.name}</div>
                <div style="font-size:11px;color:var(--text-3);margin-top:2px">{translateProtection(pool.vdevType)} · {pool.disks?.length || '?'} discos · {fmt(pool.used || 0)} / {fmt(pool.total || 0)}</div>
              </div>
              <span class="r-badge {pool.status === 'active' || pool.health === 'ONLINE' ? 'r-badge-ok' : 'r-badge-warn'}">
                {pool.status === 'active' || pool.health === 'ONLINE' ? 'Normal' : pool.status || '—'}
              </span>
            </div>

            <!-- Scrub info -->
            {#if isActive && st.status === 'scrubbing'}
              <!-- Scrub in progress -->
              <div class="r-scrub-active">
                <div class="r-scrub-label">Verificando integridad...</div>
                <div class="r-scrub-progress"><div class="r-scrub-fill" style="width:{st.progress || 0}%"></div></div>
                <div class="r-scrub-stats">
                  <span>{st.progress || 0}%</span>
                  <span>{st.errors || 0} errores</span>
                  {#if st.speed && st.speed !== '—'}<span>{st.speed}</span>{/if}
                  {#if st.timeRemaining && st.timeRemaining !== '—'}<span>~{st.timeRemaining} restante</span>{/if}
                </div>
                {#if st.scanned && st.scanned !== '—'}
                  <div class="r-scrub-detail">Escaneado: {st.scanned} de {st.totalSize || '—'}</div>
                {/if}
              </div>
            {:else if isActive && st.status === 'done'}
              <!-- Last scrub completed -->
              <div class="r-scrub-done">
                <div class="r-scrub-row">
                  <span class="r-scrub-key">Última verificación</span>
                  <span class="r-scrub-val">{st.lastScrub ? fmtDate(st.lastScrub) : '—'}</span>
                </div>
                <div class="r-scrub-row">
                  <span class="r-scrub-key">Resultado</span>
                  <span class="r-scrub-val" style="color:{(st.lastErrors || 0) === 0 ? 'var(--green)' : 'var(--red)'}">
                    {(st.lastErrors || 0) === 0 ? 'Sin errores' : `${st.lastErrors} errores encontrados`}
                  </span>
                </div>
                <div class="r-scrub-row">
                  <span class="r-scrub-key">Duración</span>
                  <span class="r-scrub-val">{st.lastDuration || '—'}</span>
                </div>
                {#if st.repaired && st.repaired !== '0B' && st.repaired !== '0'}
                  <div class="r-scrub-row">
                    <span class="r-scrub-key">Datos reparados</span>
                    <span class="r-scrub-val">{st.repaired}</span>
                  </div>
                {/if}
                {#if st.dataErrors && st.dataErrors !== '—'}
                  <div class="r-scrub-row">
                    <span class="r-scrub-key">Estado de datos</span>
                    <span class="r-scrub-val">{st.dataErrors}</span>
                  </div>
                {/if}
              </div>
              <button class="r-btn" style="margin-top:10px" on:click={() => { scrubPool = pool.name; startScrub(); }} disabled={st.status === 'scrubbing'}>
                Verificar ahora
              </button>
            {:else if isActive && st.status === 'never'}
              <div class="r-scrub-never">Nunca se ha realizado una verificación de integridad en este volumen.</div>
              <button class="r-btn r-btn-primary" style="margin-top:10px" on:click={() => { scrubPool = pool.name; startScrub(); }}>
                Verificar ahora
              </button>
            {:else}
              <!-- Idle / no data loaded yet -->
              <div class="r-scrub-never">Cargando estado de verificación...</div>
              <button class="r-btn" style="margin-top:10px" on:click={() => { scrubPool = pool.name; loadScrubStatus(pool.name).then(() => { if (scrubStatus.status === 'idle' || scrubStatus.status === 'never') startScrub(); }); }}>
                Verificar ahora
              </button>
            {/if}

            <!-- Disk errors from scrub -->
            {#if isActive && st.disks && st.disks.length > 0}
              <div class="r-sec" style="margin-top:14px">Estado de discos</div>
              {#each st.disks as disk}
                <div class="r-scrub-disk-row">
                  <span class="r-scrub-disk-name">{disk.name}</span>
                  <span class="r-scrub-disk-state" style="color:{disk.state === 'ONLINE' ? 'var(--green)' : 'var(--red)'}">{disk.state}</span>
                  <span class="r-scrub-disk-errs">R:{disk.read} W:{disk.write} C:{disk.cksum}</span>
                </div>
              {/each}
            {/if}

            <!-- Schedule -->
            {#if isActive}
              <div class="r-sec" style="margin-top:14px">Programación</div>
              <div class="r-sched-wrap">
                <div class="r-sched-row">
                  <span class="r-sched-label">Frecuencia</span>
                  <div class="r-sched-btns">
                    <!-- svelte-ignore a11y_click_events_have_key_events -->
                    <!-- svelte-ignore a11y_no_static_element_interactions -->
                    {#each [{v:'off',l:'Off'},{v:'daily',l:'Diaria'},{v:'weekly',l:'Semanal'},{v:'monthly',l:'Mensual'}] as opt}
                      <div class="r-sched-btn" class:active={scrubSchedule.frequency === opt.v}
                        on:click={() => { scrubSchedule.frequency = opt.v; saveScrubSchedule(); }}>{opt.l}</div>
                    {/each}
                  </div>
                </div>

                {#if scrubSchedule.frequency !== 'off'}
                  <div class="r-sched-row">
                    <span class="r-sched-label">Hora</span>
                    <div class="r-sched-time-wrap">
                      <input class="r-sched-input" type="number" min="0" max="23"
                        bind:value={scrubSchedule.hour} on:change={saveScrubSchedule}>
                      <span class="r-sched-colon">:</span>
                      <input class="r-sched-input" type="number" min="0" max="59" step="15"
                        bind:value={scrubSchedule.minute} on:change={saveScrubSchedule}>
                    </div>
                  </div>

                  {#if scrubSchedule.frequency === 'weekly'}
                    <div class="r-sched-row">
                      <span class="r-sched-label">Día</span>
                      <div class="r-sched-btns">
                        <!-- svelte-ignore a11y_click_events_have_key_events -->
                        <!-- svelte-ignore a11y_no_static_element_interactions -->
                        {#each ['D','L','M','X','J','V','S'] as d, i}
                          <div class="r-sched-btn r-sched-day" class:active={scrubSchedule.dayOfWeek === i}
                            on:click={() => { scrubSchedule.dayOfWeek = i; saveScrubSchedule(); }}>{d}</div>
                        {/each}
                      </div>
                    </div>
                  {/if}

                  {#if scrubSchedule.frequency === 'monthly'}
                    <div class="r-sched-row">
                      <span class="r-sched-label">Día del mes</span>
                      <input class="r-sched-input r-sched-input-wide" type="number" min="1" max="28"
                        bind:value={scrubSchedule.dayOfMonth} on:change={saveScrubSchedule}>
                    </div>
                  {/if}

                  {#if scrubSchedule.nextRun}
                    <div class="r-sched-row">
                      <span class="r-sched-label">Próxima verificación</span>
                      <span class="r-sched-val">{fmtDate(scrubSchedule.nextRun)}</span>
                    </div>
                  {/if}

                  {#if scrubSchedule.lastRun}
                    <div class="r-sched-row">
                      <span class="r-sched-label">Última ejecución</span>
                      <span class="r-sched-val">{fmtDate(scrubSchedule.lastRun)}</span>
                    </div>
                  {/if}
                {/if}

                {#if savingSchedule}
                  <div class="r-sched-saving">Guardando...</div>
                {/if}
              </div>
            {/if}
          </div>
        {/each}

        <!-- Disks temperature summary -->
        <div class="r-sec" style="margin-top:4px">Discos</div>
        <div class="r-detail-card">
          {#each allDisks as d}
            {@const sd = smartData[d.name]}
            <div class="r-disk-row" style="cursor:default">
              <div class="r-disk-ico"><svg viewBox="0 0 24 24"><circle cx="12" cy="12" r="10"/><circle cx="12" cy="12" r="3"/></svg></div>
              <div class="r-disk-info">
                <div class="r-disk-name">{d.name} · {d.model || '—'}</div>
                <div class="r-disk-model">{sd?.temperature ? sd.temperature + '°C · ' : ''}{sd?.powerOnHours ? sd.powerOnHours.toLocaleString() + 'h' : fmt(d.size)}</div>
              </div>
              {#if sd?.status === 'critical'}
                <span class="r-badge r-badge-err" style="font-size:10px">Riesgo</span>
              {:else if sd?.status === 'warning'}
                <span class="r-badge r-badge-warn" style="font-size:10px">Atención</span>
              {:else}
                <span class="r-badge r-badge-ok" style="font-size:10px">Sano</span>
              {/if}
            </div>
          {/each}
        </div>

        <!-- Explanation -->
        <div class="r-scrub-note">
          La verificación de integridad comprueba que todos los datos almacenados estén correctos y no haya corrupción silenciosa. Se recomienda ejecutarla al menos una vez al mes.
        </div>
      </div>

    {:else if activeTab === 'restore'}
      <div class="section-label">Restaurar pool</div>
      <p style="font-size:11px;color:var(--text-3);margin-bottom:14px">
        Detectar y restaurar pools existentes de discos que ya tenían NimOS configurado.
      </p>

      <button class="btn-secondary" on:click={scanRestorable} disabled={scanning}>
        {scanning ? 'Escaneando...' : 'Escanear discos'}
      </button>

      {#if restorableScanned}
        {#if restorable.length === 0}
          <p class="coming-soon" style="margin-top:12px">No se encontraron pools restaurables</p>
        {:else}
          <div style="margin-top:14px">
            {#each restorable as pool}
              <div class="pool-row">
                <div class="pool-led"></div>
                <div class="pool-info">
                  <div class="pool-name">{pool.name}</div>
                  <div class="pool-meta">{pool.raidLevel || '—'} · {pool.disks?.length || 0} discos · {pool.filesystem || '—'}</div>
                </div>
                <button class="btn-accent" style="margin-left:auto;padding:4px 10px;font-size:10px" on:click={() => restorePool(pool.name)} disabled={restoring}>
                  {restoring ? '...' : 'Restaurar'}
                </button>
              </div>
            {/each}
          </div>
        {/if}
      {/if}

      {#if restoreMsg}
        <div class="pool-msg" class:error={restoreMsgError} style="margin-top:10px">{restoreMsg}</div>
      {/if}

    {:else if activeTab === 'snapshots'}

      <!-- Pool selector -->
      <div class="zfs-toolbar">
        <div class="section-label" style="margin:0">Snapshots ZFS</div>
        <select class="form-select zfs-pool-sel" bind:value={snapPool} on:change={() => loadSnapshots(snapPool)}>
          {#each pools.filter(p => p.type === 'zfs' || p.filesystem === 'zfs') as p}
            <option value={p.name}>{p.name}</option>
          {/each}
          {#if pools.filter(p => p.type === 'zfs' || p.filesystem === 'zfs').length === 0}
            {#each pools as p}<option value={p.name}>{p.name}</option>{/each}
          {/if}
        </select>
        <div class="zfs-create-row">
          <input class="form-input zfs-snap-input" type="text" placeholder="nombre (auto si vacío)" bind:value={newSnapName} />
          <button class="btn-accent zfs-btn" on:click={createSnap}>+ Snapshot</button>
        </div>
      </div>

      {#if snapsLoading}
        <div class="zfs-loading"><div class="spinner"></div></div>
      {:else if snapshots.length === 0}
        <div class="zfs-empty">◈ No hay snapshots en este pool</div>
      {:else}
        <div class="zfs-list">
          {#each snapshots as snap}
            <div class="zfs-row">
              <div class="zfs-row-icon snap-icon">◈</div>
              <div class="zfs-row-info">
                <div class="zfs-row-name">{snap.name.split('@')[1] || snap.name}</div>
                <div class="zfs-row-meta">{snap.name.split('@')[0]} · {fmtDate(snap.created)}</div>
              </div>
              <div class="zfs-row-sizes">
                <span class="zfs-size-badge">usado {fmt(snap.used)}</span>
                <span class="zfs-size-badge refer">ref {fmt(snap.refer)}</span>
              </div>
              <div class="zfs-row-actions">
                <button class="zfs-action-btn rollback" on:click={() => rollbackSnap(snap.name)} title="Rollback">⟲</button>
                <button class="zfs-action-btn del" on:click={() => deleteSnap(snap.name)} title="Borrar">✕</button>
              </div>
            </div>
          {/each}
        </div>
      {/if}
      {#if snapMsg}<div class="pool-msg" class:error={snapMsgError} style="margin-top:10px">{snapMsg}</div>{/if}

    {:else if activeTab === 'scrub'}

      <div class="zfs-toolbar">
        <div class="section-label" style="margin:0">Scrub ZFS</div>
        <select class="form-select zfs-pool-sel" bind:value={scrubPool} on:change={() => loadScrubStatus(scrubPool)}>
          {#each pools.filter(p => p.type === 'zfs' || p.filesystem === 'zfs') as p}
            <option value={p.name}>{p.name}</option>
          {/each}
          {#if pools.filter(p => p.type === 'zfs' || p.filesystem === 'zfs').length === 0}
            {#each pools as p}<option value={p.name}>{p.name}</option>{/each}
          {/if}
        </select>
      </div>

      <div class="scrub-card">
        <div class="scrub-status-row">
          <div class="scrub-status-indicator"
            class:idle={scrubStatus.status==='idle'}
            class:running={scrubStatus.status==='scrubbing'}
            class:done={scrubStatus.status==='done'}
            class:err={scrubStatus.status==='error'}></div>
          <div class="scrub-status-label">
            {#if scrubStatus.status === 'idle'}Inactivo
            {:else if scrubStatus.status === 'scrubbing'}Scrub en progreso…
            {:else if scrubStatus.status === 'done'}Completado
            {:else}Error
            {/if}
          </div>
          {#if scrubStatus.errors !== undefined}
            <div class="scrub-errors" class:has-err={scrubStatus.errors > 0}>
              {scrubStatus.errors} error{scrubStatus.errors !== 1 ? 'es' : ''}
            </div>
          {/if}
        </div>

        {#if scrubStatus.status === 'scrubbing'}
          <div class="scrub-progress-wrap">
            <div class="scrub-progress-track">
              <div class="scrub-progress-fill" style="width:{scrubStatus.progress || 0}%"></div>
            </div>
            <div class="scrub-pct">{scrubStatus.progress || 0}%</div>
          </div>
          {#if scrubStatus.eta}
            <div class="scrub-eta">ETA: {fmtDate(scrubStatus.eta)}</div>
          {/if}
        {/if}
      </div>

      {#if scrubStatus.status !== 'scrubbing'}
        <button class="btn-accent" style="margin-top:12px;width:fit-content" on:click={startScrub}>
          ⌖ Iniciar Scrub
        </button>
      {:else}
        <button class="btn-secondary" style="margin-top:12px;width:fit-content;opacity:.5" disabled>
          Scrub en progreso…
        </button>
      {/if}
      {#if scrubMsg}<div class="pool-msg" class:error={scrubMsgError} style="margin-top:10px">{scrubMsg}</div>{/if}

    {/if}

  </div>
</div>

<!-- ══ DESTROY MODAL ══ -->
{#if showDestroy && detailPool}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="r-modal-overlay" on:click|self={() => showDestroy = false}>
    <div class="r-modal">
      <div class="r-modal-header">
        <span class="r-modal-title">Destruir {detailPool.displayName || detailPool.name}</span>
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <span class="r-modal-close" on:click={() => showDestroy = false}>✕</span>
      </div>
      <div class="r-modal-body">
        <div class="r-destroy-warn">
          Esta acción eliminará permanentemente todos los datos del volumen, incluyendo carpetas compartidas, configuraciones de apps y puntos de restauración.
        </div>

        {#if destroyDeps.length > 0}
          <div class="r-sec" style="margin-top:14px">Servicios que dependen de este volumen</div>
          <div class="r-destroy-deps">
            {#each destroyDeps as dep}
              <div class="r-dep-item">
                <span class="r-dep-dot" style="background:{dep.status === 'running' ? 'var(--green)' : 'var(--text-3)'}"></span>
                <span class="r-dep-name">{dep.app || dep.appId}</span>
                <span class="r-dep-status">{dep.status === 'running' ? 'activo' : dep.status}</span>
                {#if dep.status === 'running' || dep.status === 'starting'}
                  <button class="r-dep-stop" disabled={stoppingService[dep.id]} on:click={() => stopServiceForDestroy(dep)}>
                    {stoppingService[dep.id] ? 'Deteniendo...' : 'Detener'}
                  </button>
                {:else}
                  <span class="r-dep-stopped">Detenido</span>
                {/if}
              </div>
            {/each}
          </div>
          {#if !allDepsStopped}
            <div style="font-size:11px;color:var(--text-3);margin-top:8px">Debes detener todos los servicios antes de destruir el volumen.</div>
          {/if}
        {/if}

        <div style="margin-top:16px">
          <div class="r-sec">Confirmar destrucción</div>
          <div style="font-size:11px;color:var(--text-2);margin-bottom:6px">
            Escribe <strong style="color:var(--red)">ELIMINAR</strong> para confirmar:
          </div>
          <input class="r-confirm-input" bind:value={destroyInput} placeholder="Escribe ELIMINAR">
        </div>
      </div>
      <div class="r-modal-footer">
        <button class="r-btn" on:click={() => showDestroy = false}>Cancelar</button>
        <button class="r-btn r-btn-danger" disabled={!canDestroy || destroying} on:click={doDestroy}>
          {destroying ? 'Destruyendo...' : 'Destruir volumen'}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .storage-root { width:100%; height:100%; display:flex; flex-direction:column; overflow:hidden; }
  .s-body { flex:1; overflow-y:auto; padding:18px 20px; }
  .s-body::-webkit-scrollbar { width:3px; }
  .s-body::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }

  .s-loading { display:flex; align-items:center; justify-content:center; height:100%; }
  .spinner {
    width:28px; height:28px; border-radius:50%;
    border:2.5px solid rgba(255,255,255,0.1);
    border-top-color:var(--accent);
    animation:spin .7s linear infinite;
  }
  @keyframes spin { to { transform:rotate(360deg); } }

  /* ── DISK SLOTS ── */
  .disk-section { }
  .disk-section-label {
    font-size:9px; font-weight:600; color:var(--text-3);
    text-transform:uppercase; letter-spacing:.08em; margin-bottom:10px;
  }
  .disk-slots-wrap { display:flex; gap:8px; align-items:flex-start; }
  .nvme-slots-wrap  { display:flex; gap:8px; align-items:flex-start; }

  .disk-slot { display:flex; flex-direction:column; align-items:center; gap:4px; cursor:pointer; transition:transform .15s; }
  .disk-slot:not(.empty):hover { transform:translateY(-2px); }
  .disk-slot.empty { opacity:.35; cursor:default; pointer-events:none; }
  .disk-slot.selected { transform:translateY(-2px); }
  .disk-slot.selected svg { filter:drop-shadow(0 0 6px rgba(124,111,255,0.5)); }

  .disk-label { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; text-align:center; }
  .empty-label { opacity:.5; }

  @keyframes ledBlink { 0%,100%{opacity:.9} 50%{opacity:.2} }

  /* ── DISK INFO PANEL ── */
  .disk-info-panel {
    flex:1; margin-left:4px;
    padding:12px 14px; border-radius:8px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    display:flex; flex-direction:column; gap:5px;
    justify-content:center; align-self:stretch; min-width:0;
  }
  .di-empty { display:flex; flex-direction:column; align-items:center; gap:6px; color:var(--text-3); font-size:11px; }
  .di-empty-icon { font-size:22px; opacity:.4; }
  .di-name { font-size:12px; font-weight:600; color:var(--text-1); }
  .di-serial { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; }
  .di-row {
    display:flex; justify-content:space-between;
    font-size:10px; color:var(--text-2); border-bottom:1px solid var(--border); padding:3px 0;
  }
  .di-row span:last-child { color:var(--text-1); font-family:'DM Mono',monospace; font-size:9px; }
  .di-tags { display:flex; gap:5px; margin-top:3px; }
  .di-tag {
    padding:2px 7px; border-radius:4px; font-size:9px; font-weight:600;
    background:var(--ibtn-bg); border:1px solid var(--border); color:var(--text-2);
    font-family:'DM Mono',monospace;
  }
  .di-tag.green { background:rgba(74,222,128,0.10); border-color:rgba(74,222,128,0.25); color:var(--green); }

  /* ── STORAGE BAR ── */
  .storage-bar-section { margin-top:16px; width:50%; }
  .sbs-meta { display:flex; justify-content:space-between; margin-bottom:5px; }
  .sbs-label { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; }
  .sbs-value { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; }
  .sbs-track { height:5px; background:rgba(128,128,128,0.12); border-radius:3px; overflow:hidden; }
  .sbs-fill  { height:100%; border-radius:3px; background:linear-gradient(90deg, var(--accent), var(--accent2)); }

  /* ── LEGEND ── */
  .disk-legend { display:flex; gap:14px; margin-top:12px; }
  .dl-item { display:flex; align-items:center; gap:5px; font-size:10px; color:var(--text-3); }
  .dl-dot  { width:7px; height:7px; border-radius:2px; flex-shrink:0; }

  /* ── POOLS TAB ── */
  .section-label { font-size:10px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.08em; margin-bottom:12px; }
  .pool-row {
    display:flex; align-items:center; gap:10px;
    padding:10px 12px; border-radius:8px; margin-bottom:6px;
    border:1px solid var(--border); background:var(--ibtn-bg);
  }
  .pool-led { width:7px; height:7px; border-radius:50%; background:rgba(128,128,128,0.3); flex-shrink:0; }
  .pool-led.healthy { background:var(--green); box-shadow:0 0 5px rgba(74,222,128,0.6); }
  .pool-name { font-size:12px; font-weight:600; color:var(--text-1); }
  .pool-primary { font-size:9px; font-weight:400; color:var(--text-3); margin-left:5px; }
  .pool-meta { font-size:10px; color:var(--text-3); margin-top:1px; }
  .pool-badge { margin-left:auto; padding:3px 8px; border-radius:20px; font-size:9px; font-weight:600; background:var(--ibtn-bg); border:1px solid var(--border); color:var(--text-2); }
  .pool-badge.green { background:rgba(74,222,128,0.10); border-color:rgba(74,222,128,0.25); color:var(--green); }
  .coming-soon { color:var(--text-3); font-size:12px; }

  /* ── DISK CARDS ── */
  .disk-card-list { display:flex; flex-direction:column; gap:4px; }
  .disk-card {
    display:flex; align-items:center; gap:8px;
    padding:9px 12px; border-radius:8px;
    border:1px solid var(--border); background:var(--ibtn-bg);
  }
  .disk-card-info { display:flex; align-items:center; gap:8px; flex:1; min-width:0; }
  .disk-card-led { width:6px; height:6px; border-radius:50%; flex-shrink:0; }
  .disk-card-name { font-size:12px; font-weight:600; color:var(--text-1); font-family:'DM Mono',monospace; flex-shrink:0; }
  .disk-card-model { font-size:10px; color:var(--text-3); white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .disk-card-size { font-size:11px; color:var(--text-2); font-family:'DM Mono',monospace; margin-left:auto; flex-shrink:0; }
  .disk-card-status { flex-shrink:0; }
  .disk-tag {
    padding:2px 7px; border-radius:4px; font-size:9px; font-weight:600;
    background:var(--ibtn-bg); border:1px solid var(--border); color:var(--text-3);
    font-family:'DM Mono',monospace;
  }
  .disk-tag.green { background:rgba(74,222,128,0.10); border-color:rgba(74,222,128,0.25); color:var(--green); }
  .disk-tag.amber { background:rgba(251,191,36,0.10); border-color:rgba(251,191,36,0.25); color:var(--amber); }

  .disk-wipe-btn {
    padding:4px 10px; border-radius:6px; border:1px solid rgba(248,113,113,0.25);
    background:rgba(248,113,113,0.08); color:var(--red);
    font-size:9px; font-weight:600; cursor:pointer; font-family:inherit;
    transition:all .15s; flex-shrink:0;
  }
  .disk-wipe-btn:hover { background:rgba(248,113,113,0.15); }
  .disk-wipe-btn:disabled { opacity:.5; cursor:not-allowed; }

  .create-pool-btn {
    font-size:11px; color:var(--accent); cursor:pointer;
    padding:8px 0; transition:opacity .15s;
  }
  .create-pool-btn:hover { opacity:.7; }

  .pool-destroy {
    cursor:pointer; color:var(--text-3); font-size:12px; margin-left:8px;
    transition:color .15s;
  }
  .pool-destroy:hover { color:var(--red); }

  .form-row { display:flex; gap:10px; }

  /* ── CREATE POOL FORM ── */
  .create-form { display:flex; flex-direction:column; gap:14px; max-width:460px; }
  .form-field { display:flex; flex-direction:column; gap:4px; }
  .form-label { font-size:10px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; }
  .form-input, .form-select {
    padding:9px 12px; border-radius:8px;
    background:rgba(255,255,255,0.04); border:1px solid var(--border);
    color:var(--text-1); font-size:12px; font-family:'DM Sans',sans-serif;
    outline:none; transition:border-color .2s;
  }
  .form-input:focus, .form-select:focus { border-color:var(--accent); }
  .form-input::placeholder { color:var(--text-3); }
  .form-select { cursor:pointer; -webkit-appearance:none; appearance:none;
    background-image:url("data:image/svg+xml,%3Csvg width='10' height='6' viewBox='0 0 10 6' fill='none' xmlns='http://www.w3.org/2000/svg'%3E%3Cpath d='M1 1l4 4 4-4' stroke='%23666' stroke-width='1.5' stroke-linecap='round'/%3E%3C/svg%3E");
    background-repeat:no-repeat; background-position:right 12px center; padding-right:32px;
  }
  .form-select option { background:var(--bg-inner); color:var(--text-1); }

  .disk-select-list { display:flex; flex-direction:column; gap:2px; }
  .disk-select-row {
    display:flex; align-items:center; gap:8px;
    padding:7px 10px; border-radius:6px; cursor:pointer;
    border:1px solid var(--border); transition:all .15s;
    font-size:11px;
  }
  .disk-select-row:hover { border-color:var(--border-hi); }
  .disk-select-row.selected { background:var(--active-bg); border-color:var(--border-hi); }
  .dsr-check { width:16px; font-size:11px; color:var(--accent); text-align:center; }
  .dsr-name { font-weight:600; color:var(--text-1); font-family:'DM Mono',monospace; }
  .dsr-model { color:var(--text-3); flex:1; }
  .dsr-size { color:var(--text-2); font-family:'DM Mono',monospace; margin-left:auto; }

  .form-actions { display:flex; gap:8px; margin-top:4px; }
  .btn-accent {
    padding:8px 16px; border-radius:8px; border:none;
    background:linear-gradient(135deg, var(--accent), var(--accent2));
    color:#fff; font-size:11px; font-weight:600; cursor:pointer;
    font-family:inherit; transition:opacity .15s;
  }
  .btn-accent:hover { opacity:.88; }
  .btn-accent:disabled { opacity:.5; cursor:not-allowed; }
  .btn-secondary {
    padding:8px 16px; border-radius:8px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    color:var(--text-2); font-size:11px; font-weight:500; cursor:pointer;
    font-family:inherit; transition:all .15s;
  }
  .btn-secondary:hover { color:var(--text-1); border-color:var(--border-hi); }
  .btn-secondary:disabled { opacity:.5; cursor:not-allowed; }

  .pool-msg { font-size:11px; color:var(--green); padding:6px 0; }
  .pool-msg.error { color:var(--red); }
  .pool-sep { height:1px; background:var(--border); margin:12px 0; }

  /* ── ZFS SHARED ── */
  .zfs-toolbar {
    display:flex; align-items:center; gap:10px; flex-wrap:wrap;
    margin-bottom:14px;
  }
  .zfs-pool-sel { width:140px; padding:6px 10px; font-size:11px; }
  .zfs-create-row { display:flex; align-items:center; gap:6px; margin-left:auto; }
  .zfs-snap-input { width:180px; padding:6px 10px; font-size:11px; }
  .zfs-quota-input { width:110px; padding:6px 10px; font-size:11px; }
  .zfs-btn { padding:6px 12px; font-size:11px; white-space:nowrap; }
  .zfs-loading { display:flex; align-items:center; justify-content:center; padding:40px; }
  .zfs-empty { font-size:12px; color:var(--text-3); padding:30px 0; text-align:center; }

  /* ── ZFS LIST ROWS ── */
  .zfs-list { display:flex; flex-direction:column; gap:4px; }
  .zfs-row {
    display:flex; align-items:center; gap:10px;
    padding:9px 12px; border-radius:8px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    transition:border-color .15s;
  }
  .zfs-row:hover { border-color:var(--border-hi); }
  .zfs-row-icon { font-size:14px; flex-shrink:0; width:18px; text-align:center; }
  .snap-icon { color:var(--accent); }
  .ds-icon   { color:var(--accent2); }
  .zfs-row-info { flex:1; min-width:0; }
  .zfs-row-name { font-size:12px; font-weight:600; color:var(--text-1); }
  .zfs-row-meta { font-size:10px; color:var(--text-3); margin-top:1px; }
  .zfs-row-sizes { display:flex; gap:5px; flex-shrink:0; }
  .zfs-size-badge {
    padding:2px 7px; border-radius:4px; font-size:9px; font-weight:600;
    background:var(--ibtn-bg); border:1px solid var(--border); color:var(--text-3);
    font-family:'DM Mono',monospace;
  }
  .zfs-size-badge.refer { color:var(--text-2); }
  .zfs-size-badge.quota { color:var(--amber); border-color:rgba(251,191,36,0.25); background:rgba(251,191,36,0.08); }
  .zfs-row-actions { display:flex; gap:5px; flex-shrink:0; }
  .zfs-action-btn {
    width:26px; height:26px; border-radius:6px; border:1px solid var(--border);
    background:var(--ibtn-bg); color:var(--text-3); font-size:11px;
    cursor:pointer; display:flex; align-items:center; justify-content:center;
    transition:all .15s;
  }
  .zfs-action-btn:hover { color:var(--text-1); border-color:var(--border-hi); }
  .zfs-action-btn.del:hover  { color:var(--red);    border-color:rgba(248,113,113,0.35); background:rgba(248,113,113,0.08); }
  .zfs-action-btn.rollback:hover { color:var(--accent); border-color:rgba(124,111,255,0.35); background:rgba(124,111,255,0.08); }

  /* ── DATASET QUOTA BAR ── */
  .ds-quota-bar { width:60px; height:4px; background:rgba(128,128,128,0.12); border-radius:2px; overflow:hidden; flex-shrink:0; }
  .ds-quota-fill { height:100%; border-radius:2px; transition:width .3s; }

  /* ── SCRUB ── */
  .scrub-card {
    padding:16px 18px; border-radius:10px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    max-width:420px; display:flex; flex-direction:column; gap:12px;
  }
  .scrub-status-row { display:flex; align-items:center; gap:10px; }
  .scrub-status-indicator {
    width:10px; height:10px; border-radius:50%; flex-shrink:0;
    background:rgba(128,128,128,0.3);
  }
  .scrub-status-indicator.idle    { background:rgba(128,128,128,0.3); }
  .scrub-status-indicator.running { background:var(--accent); box-shadow:0 0 6px rgba(124,111,255,0.6); animation:ledBlink 1.5s ease-in-out infinite; }
  .scrub-status-indicator.done    { background:var(--green);  box-shadow:0 0 5px rgba(74,222,128,0.5); }
  .scrub-status-indicator.err     { background:var(--red); }
  .scrub-status-label { font-size:13px; font-weight:600; color:var(--text-1); }
  .scrub-errors { margin-left:auto; font-size:10px; font-family:'DM Mono',monospace; color:var(--text-3); }
  .scrub-errors.has-err { color:var(--red); }
  .scrub-progress-wrap { display:flex; align-items:center; gap:10px; }
  .scrub-progress-track { flex:1; height:6px; background:rgba(128,128,128,0.12); border-radius:3px; overflow:hidden; }
  .scrub-progress-fill  { height:100%; border-radius:3px; background:linear-gradient(90deg, var(--accent), var(--accent2)); transition:width .5s; }
  .scrub-pct { font-size:11px; font-family:'DM Mono',monospace; color:var(--text-2); flex-shrink:0; width:36px; text-align:right; }
  .scrub-eta { font-size:10px; color:var(--text-3); }

  /* ── RESUMEN ── */
  .resumen-scroll { flex:1; overflow-y:auto; padding:16px; display:flex; flex-direction:column; gap:14px; }
  .resumen-scroll::-webkit-scrollbar { width:3px; }
  .resumen-scroll::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }

  .r-alert { display:flex; align-items:center; gap:10px; padding:12px 16px; border-radius:10px; font-size:12px; font-weight:500; }
  .r-alert svg { width:16px; height:16px; stroke:currentColor; fill:none; stroke-width:2; stroke-linecap:round; flex-shrink:0; }
  .r-alert-ok { background:rgba(34,197,94,0.06); border:1px solid rgba(34,197,94,0.15); color:var(--green); }
  .r-alert-warn { background:rgba(245,158,11,0.06); border:1px solid rgba(245,158,11,0.15); color:var(--amber); }
  .r-alert-err { background:rgba(239,68,68,0.06); border:1px solid rgba(239,68,68,0.15); color:var(--red); }

  .r-grid { display:grid; grid-template-columns:2fr 1fr; gap:14px; align-items:stretch; }
  .r-vols { display:flex; flex-direction:column; gap:10px; }
  .r-sec { font-size:9px; font-weight:700; letter-spacing:.1em; text-transform:uppercase; color:var(--text-3); margin-bottom:4px; }

  .r-vol-card { background:rgba(255,255,255,0.025); border:1px solid var(--border); border-radius:12px; padding:16px 18px; transition:all .2s; cursor:pointer; display:flex; flex-direction:column; gap:6px; }
  .r-vol-card:hover { border-color:var(--border-hi); }
  .r-vol-card.degraded { border-color:rgba(245,158,11,0.3); }
  .r-vol-card.error { border-color:rgba(239,68,68,0.3); }
  .r-vol-top { display:flex; justify-content:space-between; align-items:flex-start; }
  .r-vol-name { font-size:14px; font-weight:700; color:var(--text-1); }
  .r-vol-meta { font-size:11px; color:var(--text-3); margin-top:2px; }
  .r-vol-info { display:flex; gap:14px; font-size:11px; color:var(--text-2); margin-top:4px; }

  .r-vol-actions { display:flex; gap:8px; margin-top:8px; }
  .r-btn { padding:7px 14px; border-radius:8px; border:1px solid var(--border); background:rgba(255,255,255,0.04); color:var(--text-1); font-family:inherit; font-size:11px; font-weight:600; cursor:pointer; transition:all .15s; }
  .r-btn:hover { border-color:var(--border-hi); background:rgba(124,111,255,0.08); }
  .r-btn-primary { background:linear-gradient(135deg, var(--accent), var(--accent2, #a855f7)); border:none; color:#fff; box-shadow:0 2px 10px rgba(124,111,255,0.2); }
  .r-btn-primary:hover { opacity:.88; }
  .r-btn-primary:disabled { opacity:.6; cursor:not-allowed; }

  .r-snap-btn { display:inline-flex; align-items:center; gap:5px; transition:all .2s; }
  .r-snap-btn.loading { background:rgba(124,111,255,0.3); }
  .r-snap-btn.done { background:rgba(34,197,94,0.3); border-color:rgba(34,197,94,0.4); }
  .r-snap-btn.fail { background:rgba(239,68,68,0.3); border-color:rgba(239,68,68,0.4); }
  .r-snap-spinner { width:12px; height:12px; border:2px solid rgba(255,255,255,0.3); border-top-color:#fff; border-radius:50%; animation:r-spin .6s linear infinite; }
  @keyframes r-spin { to { transform:rotate(360deg); } }
  .r-snap-tick { font-weight:700; color:var(--green); }
  .r-snap-fail { font-weight:700; color:var(--red); }

  .r-badge { padding:4px 12px; border-radius:20px; font-size:10px; font-weight:600; }
  .r-badge-ok { background:rgba(34,197,94,0.10); color:var(--green); border:1px solid rgba(34,197,94,0.25); }
  .r-badge-warn { background:rgba(245,158,11,0.10); color:var(--amber); border:1px solid rgba(245,158,11,0.25); }
  .r-badge-err { background:rgba(239,68,68,0.10); color:var(--red); border:1px solid rgba(239,68,68,0.25); }

  .r-bar { height:7px; border-radius:4px; background:rgba(255,255,255,0.04); overflow:hidden; margin:10px 0 4px; }
  .r-bar-fill { height:100%; border-radius:4px; background:linear-gradient(90deg, var(--accent), var(--accent2)); transition:width .6s ease; }
  .r-bar-text { display:flex; justify-content:space-between; font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; }

  .r-activity-card { background:rgba(255,255,255,0.025); border:1px solid var(--border); border-radius:12px; padding:16px 18px; overflow:hidden; display:flex; flex-direction:column; }
  .r-act-item { display:flex; align-items:center; gap:10px; padding:8px 0; border-bottom:1px solid var(--border); font-size:12px; white-space:nowrap; overflow:hidden; text-overflow:ellipsis; }
  .r-act-item:last-child { border:none; }
  .r-act-time { font-size:9px; color:var(--text-3); font-family:'DM Mono',monospace; min-width:50px; flex-shrink:0; }
  .r-act-dot { width:6px; height:6px; border-radius:50%; flex-shrink:0; }
  .r-act-msg { color:var(--text-2); overflow:hidden; text-overflow:ellipsis; }

  .r-disk-list { background:rgba(255,255,255,0.025); border:1px solid var(--border); border-radius:12px; overflow:hidden; }
  .r-disk-row { display:flex; align-items:center; gap:12px; padding:12px 16px; border-bottom:1px solid var(--border); cursor:pointer; transition:background .1s; }
  .r-disk-row:last-child { border:none; }
  .r-disk-row:hover { background:rgba(255,255,255,0.02); }
  .r-disk-ico { width:32px; height:32px; border-radius:8px; background:rgba(96,165,250,0.08); display:flex; align-items:center; justify-content:center; flex-shrink:0; }
  .r-disk-ico svg { width:14px; height:14px; stroke:var(--blue); fill:none; stroke-width:2; stroke-linecap:round; }
  .r-disk-info { flex:1; }
  .r-disk-name { font-size:13px; font-weight:600; color:var(--text-1); }
  .r-disk-model { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; }

  .r-cap { background:rgba(255,255,255,0.025); border:1px solid var(--border); border-radius:12px; padding:14px 18px; }

  /* Onboarding */
  .onboard { width:100%; height:100%; display:flex; flex-direction:column; align-items:center; justify-content:center; gap:14px; text-align:center; padding:40px; }
  .onboard-icon { font-size:52px; line-height:1; }
  .onboard-title { font-size:20px; font-weight:700; color:var(--text-1); }
  .onboard-desc { font-size:13px; color:var(--text-2); line-height:1.7; max-width:400px; }
  .onboard-disks { display:flex; flex-direction:column; gap:6px; margin:6px 0; }
  .onboard-disk { display:flex; align-items:center; gap:10px; padding:9px 16px; background:rgba(255,255,255,0.03); border:1px solid var(--border); border-radius:8px; font-size:11px; color:var(--text-1); }
  .o-dot { width:6px; height:6px; border-radius:50%; background:var(--green); flex-shrink:0; }
  .btn-cta { padding:12px 28px; border-radius:10px; border:none; cursor:pointer; background:linear-gradient(135deg, var(--accent), var(--accent2)); color:#fff; font-size:14px; font-weight:600; font-family:inherit; margin-top:8px; box-shadow:0 4px 16px rgba(124,111,255,0.25); transition:opacity .15s; }
  .btn-cta:hover { opacity:.88; }

  /* ── DETAIL VIEW ── */
  .r-back { font-size:12px; color:var(--accent); cursor:pointer; padding:4px 0; margin-bottom:4px; }
  .r-back:hover { text-decoration:underline; }

  .r-detail-grid { display:grid; grid-template-columns:1fr 1fr; gap:14px; }
  .r-detail-card { background:rgba(255,255,255,0.025); border:1px solid var(--border); border-radius:12px; padding:16px 18px; }
  .r-detail-rows { display:flex; flex-direction:column; gap:8px; font-size:12px; }
  .r-detail-row { display:flex; justify-content:space-between; align-items:center; padding:3px 0; }
  .r-detail-row + .r-detail-row { border-top:1px solid var(--border); padding-top:8px; }
  .r-detail-key { color:var(--text-3); }
  .r-detail-val { color:var(--text-1); font-family:'DM Mono',monospace; }

  .r-use-row { display:flex; align-items:center; justify-content:space-between; padding:7px 0; border-bottom:1px solid var(--border); font-size:12px; }
  .r-use-row:last-child { border:none; }
  .r-use-name { color:var(--text-2); }
  .r-use-size { font-family:'DM Mono',monospace; font-weight:500; color:var(--text-1); }

  .r-svc-row { display:flex; align-items:center; gap:10px; padding:10px 0; border-bottom:1px solid var(--border); font-size:12px; }
  .r-svc-row:last-child { border:none; }
  .r-svc-dot { width:6px; height:6px; border-radius:50%; flex-shrink:0; }
  .r-svc-name { font-weight:500; color:var(--text-1); }
  .r-svc-status { margin-left:auto; font-size:11px; color:var(--text-3); }

  .r-actions-row { display:flex; gap:8px; margin-top:4px; }
  .r-btn-danger { background:rgba(239,68,68,0.10); border-color:rgba(239,68,68,0.3); color:var(--red); }
  .r-btn-danger:hover { background:rgba(239,68,68,0.18); }
  .r-btn-danger:disabled { opacity:.35; cursor:not-allowed; }

  /* ── DESTROY MODAL ── */
  .r-modal-overlay { position:fixed; inset:0; background:rgba(0,0,0,0.6); backdrop-filter:blur(4px); z-index:200; display:flex; align-items:center; justify-content:center; }
  .r-modal { background:var(--bg-inner, #1c1b3a); border:1px solid var(--border); border-radius:16px; width:520px; max-width:92%; max-height:85vh; overflow-y:auto; box-shadow:0 40px 100px rgba(0,0,0,0.6); animation:r-modalIn .25s ease both; }
  @keyframes r-modalIn { from{opacity:0;transform:scale(0.96) translateY(8px)} to{opacity:1;transform:none} }
  .r-modal-header { padding:18px 22px 14px; border-bottom:1px solid var(--border); display:flex; align-items:center; justify-content:space-between; }
  .r-modal-title { font-size:15px; font-weight:700; color:var(--text-1); }
  .r-modal-close { width:26px; height:26px; border-radius:50%; background:rgba(255,255,255,0.06); display:flex; align-items:center; justify-content:center; cursor:pointer; color:var(--text-3); font-size:13px; transition:.15s; }
  .r-modal-close:hover { background:rgba(255,255,255,0.12); color:var(--text-1); }
  .r-modal-body { padding:18px 22px; }
  .r-modal-footer { padding:14px 22px 18px; border-top:1px solid var(--border); display:flex; gap:8px; justify-content:flex-end; }

  .r-destroy-warn { padding:12px 14px; border-radius:10px; background:rgba(239,68,68,0.06); border:1px solid rgba(239,68,68,0.15); font-size:12px; color:var(--red); line-height:1.6; }
  .r-destroy-deps { display:flex; flex-direction:column; gap:6px; margin-top:8px; }
  .r-dep-item { display:flex; align-items:center; gap:10px; padding:10px 12px; background:rgba(255,255,255,0.03); border:1px solid var(--border); border-radius:8px; font-size:12px; }
  .r-dep-dot { width:6px; height:6px; border-radius:50%; flex-shrink:0; }
  .r-dep-name { font-weight:500; color:var(--text-1); }
  .r-dep-status { font-size:11px; color:var(--text-3); }
  .r-dep-stop { margin-left:auto; padding:4px 10px; border-radius:6px; border:1px solid rgba(239,68,68,0.3); background:rgba(239,68,68,0.08); color:var(--red); font-size:10px; font-weight:600; cursor:pointer; font-family:inherit; transition:.15s; }
  .r-dep-stop:hover { background:rgba(239,68,68,0.15); }
  .r-dep-stop:disabled { opacity:.4; cursor:not-allowed; }
  .r-dep-stopped { margin-left:auto; font-size:10px; font-weight:600; color:var(--green); }
  .r-confirm-input { width:100%; padding:10px 12px; border-radius:8px; border:1px solid var(--border); background:rgba(255,255,255,0.04); color:var(--text-1); font-family:inherit; font-size:13px; outline:none; transition:border-color .15s; }
  .r-confirm-input:focus { border-color:var(--red); }
  .r-confirm-input::placeholder { color:var(--text-3); }

  /* ── SALUD / HEALTH VIEW ── */
  .r-health-hero { display:flex; align-items:center; gap:16px; padding:18px 20px; border-radius:12px; }
  .r-health-ok { background:rgba(34,197,94,0.05); border:1px solid rgba(34,197,94,0.15); }
  .r-health-warn { background:rgba(245,158,11,0.05); border:1px solid rgba(245,158,11,0.15); }
  .r-health-err { background:rgba(239,68,68,0.05); border:1px solid rgba(239,68,68,0.15); }
  .r-health-checking { background:rgba(124,111,255,0.05); border:1px solid rgba(124,111,255,0.20); }
  .r-hh-icon { width:48px; height:48px; border-radius:50%; display:flex; align-items:center; justify-content:center; background:rgba(34,197,94,0.10); flex-shrink:0; }
  .r-hh-icon.warn { background:rgba(245,158,11,0.10); }
  .r-hh-icon.err { background:rgba(239,68,68,0.10); }
  .r-hh-icon.checking { background:rgba(124,111,255,0.12); }
  .r-hh-icon svg { width:20px; height:20px; stroke:var(--green); fill:none; stroke-width:2; stroke-linecap:round; }
  .r-hh-icon.warn svg { stroke:var(--amber); }
  .r-hh-icon.err svg { stroke:var(--red); }
  .r-hh-icon.checking svg { stroke:var(--accent); }

  .r-smart-alert { border-color:rgba(245,158,11,0.2); }
  .r-smart-alert-row { display:flex; align-items:center; gap:10px; padding:9px 0; border-bottom:1px solid var(--border); font-size:12px; }
  .r-smart-alert-row:last-child { border:none; }
  .r-smart-alert-name { font-weight:600; color:var(--text-1); }
  .r-smart-alert-model { color:var(--text-3); font-size:11px; }
  .r-smart-alert-detail { margin-left:auto; font-size:10px; color:var(--amber); font-family:'DM Mono',monospace; }
  .r-hh-title { font-size:16px; font-weight:700; color:var(--text-1); }
  .r-hh-sub { font-size:12px; color:var(--text-3); margin-top:3px; }

  .r-scrub-active { margin-top:8px; }
  .r-scrub-label { font-size:12px; font-weight:600; color:var(--accent); margin-bottom:4px; }
  .r-scrub-progress { height:6px; border-radius:3px; background:rgba(255,255,255,0.06); overflow:hidden; }
  .r-scrub-fill { height:100%; border-radius:3px; background:linear-gradient(90deg, var(--accent), var(--accent2, #a855f7)); transition:width .5s ease; }
  .r-scrub-stats { display:flex; gap:16px; font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; margin-top:6px; }
  .r-scrub-detail { font-size:10px; color:var(--text-3); margin-top:4px; }

  .r-scrub-done { display:flex; flex-direction:column; gap:6px; margin-top:8px; }
  .r-scrub-row { display:flex; justify-content:space-between; align-items:center; font-size:12px; padding:4px 0; border-bottom:1px solid var(--border); }
  .r-scrub-row:last-child { border:none; }
  .r-scrub-key { color:var(--text-3); }
  .r-scrub-val { color:var(--text-1); font-family:'DM Mono',monospace; }

  .r-scrub-never { font-size:11px; color:var(--text-3); margin-top:8px; line-height:1.5; }

  .r-scrub-disk-row { display:flex; align-items:center; gap:10px; padding:6px 0; border-bottom:1px solid var(--border); font-size:11px; }
  .r-scrub-disk-row:last-child { border:none; }
  .r-scrub-disk-name { font-family:'DM Mono',monospace; color:var(--text-2); flex:1; font-size:10px; overflow:hidden; text-overflow:ellipsis; }
  .r-scrub-disk-state { font-weight:600; min-width:55px; }
  .r-scrub-disk-errs { font-family:'DM Mono',monospace; color:var(--text-3); font-size:10px; }

  .r-scrub-note { font-size:11px; color:var(--text-3); line-height:1.6; margin-top:10px; padding:12px 14px; background:rgba(255,255,255,0.02); border:1px solid var(--border); border-radius:9px; }

  /* ── SCHEDULE ── */
  .r-sched-wrap { display:flex; flex-direction:column; gap:8px; }
  .r-sched-row { display:flex; align-items:center; justify-content:space-between; gap:12px; padding:6px 0; border-bottom:1px solid var(--border); font-size:12px; }
  .r-sched-row:last-child { border:none; }
  .r-sched-label { color:var(--text-3); flex-shrink:0; }
  .r-sched-val { color:var(--text-1); font-family:'DM Mono',monospace; font-size:11px; }

  .r-sched-btns { display:flex; gap:4px; }
  .r-sched-btn { padding:5px 11px; border-radius:6px; border:1px solid var(--border); background:rgba(255,255,255,0.03); color:var(--text-3); font-size:10px; font-weight:600; cursor:pointer; transition:all .15s; font-family:inherit; }
  .r-sched-btn:hover { border-color:var(--border-hi); color:var(--text-1); }
  .r-sched-btn.active { background:rgba(124,111,255,0.15); border-color:var(--accent); color:var(--accent); }
  .r-sched-day { width:28px; height:28px; padding:0; display:flex; align-items:center; justify-content:center; border-radius:50%; font-size:10px; }

  .r-sched-time-wrap { display:flex; align-items:center; gap:4px; }
  .r-sched-colon { color:var(--text-3); font-weight:700; font-size:14px; }
  .r-sched-input { width:48px; padding:5px 8px; border-radius:6px; border:1px solid var(--border); background:rgba(255,255,255,0.04); color:var(--text-1); font-family:'DM Mono',monospace; font-size:12px; text-align:center; outline:none; -moz-appearance:textfield; }
  .r-sched-input::-webkit-outer-spin-button, .r-sched-input::-webkit-inner-spin-button { -webkit-appearance:none; margin:0; }
  .r-sched-input:focus { border-color:var(--accent); }
  .r-sched-input-wide { width:56px; }
  .r-sched-saving { font-size:10px; color:var(--accent); padding:4px 0; }

  /* ── DISK TABLE ── */
  .r-disk-table { width:100%; border-collapse:collapse; }
  .r-disk-table th { font-size:10px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; text-align:left; padding:8px 10px; border-bottom:1px solid var(--border); }
  .r-disk-table td { font-size:12px; color:var(--text-1); padding:10px 10px; border-bottom:1px solid var(--border); }
  .r-disk-tr { cursor:pointer; transition:background .1s; }
  .r-disk-tr:hover { background:rgba(255,255,255,0.02); }
  .r-disk-tr.expanded { background:rgba(124,111,255,0.04); }
  .r-dt-name { font-weight:600; }
  .r-dt-model { font-size:11px; color:var(--text-3); font-family:'DM Mono',monospace; }
  .r-dt-mono { font-family:'DM Mono',monospace; color:var(--text-3); }
  .r-dt-badge { padding:3px 10px; border-radius:12px; font-size:10px; font-weight:600; }
  .r-dt-ok { background:rgba(34,197,94,0.10); color:var(--green); }
  .r-dt-free { background:rgba(96,165,250,0.10); color:var(--blue, #60a5fa); }
  .r-dt-warn { background:rgba(245,158,11,0.10); color:var(--amber); }
  .r-dt-err { background:rgba(239,68,68,0.10); color:var(--red); }

  .r-disk-detail-tr td { padding:0 10px 12px !important; border-bottom:1px solid var(--border); }
  .r-disk-detail { background:rgba(0,0,0,0.15); border-radius:8px; padding:10px 14px; }
  .r-dd-row { display:flex; justify-content:space-between; font-size:11px; padding:4px 0; border-bottom:1px solid var(--border); }
  .r-dd-row:last-child { border:none; }
  .r-dd-key { color:var(--text-3); }
  .r-dd-val { color:var(--text-1); font-family:'DM Mono',monospace; }

  .r-smart-toggle { font-size:11px; color:var(--accent); cursor:pointer; margin-top:10px; padding:4px 0; }
  .r-smart-toggle:hover { text-decoration:underline; }
  .r-smart-table { width:100%; border-collapse:collapse; margin-top:6px; font-size:10px; }
  .r-smart-table th { color:var(--text-3); font-weight:600; text-align:left; padding:5px 6px; font-size:9px; text-transform:uppercase; letter-spacing:.04em; border-bottom:1px solid var(--border); }
  .r-smart-table td { color:var(--text-2); padding:4px 6px; border-bottom:1px solid var(--border); font-family:'DM Mono',monospace; font-size:10px; }

  /* ── CREATE POOL FORM ── */
  .r-create-form { display:flex; flex-direction:column; gap:12px; }
  .r-form-field { display:flex; flex-direction:column; gap:4px; }
  .r-form-label { font-size:10px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; }
  .r-form-input { padding:8px 12px; border-radius:8px; border:1px solid var(--border); background:rgba(255,255,255,0.04); color:var(--text-1); font-family:inherit; font-size:12px; outline:none; }
  .r-form-input:focus { border-color:var(--accent); }
  .r-form-input::placeholder { color:var(--text-3); }
  .r-form-row { display:flex; gap:12px; }
  .r-form-actions { display:flex; gap:8px; margin-top:4px; }

  .r-disk-select { display:flex; flex-direction:column; gap:4px; margin-top:4px; }
  .r-dsel-row { display:flex; align-items:center; gap:10px; padding:8px 12px; border:1px solid var(--border); border-radius:8px; cursor:pointer; transition:.15s; font-size:12px; }
  .r-dsel-row:hover { border-color:rgba(124,111,255,0.3); }
  .r-dsel-row.selected { border-color:var(--accent); background:rgba(124,111,255,0.06); }
  .r-dsel-chk { width:18px; height:18px; border-radius:5px; border:2px solid var(--text-3); display:flex; align-items:center; justify-content:center; font-size:10px; font-weight:700; color:var(--accent); flex-shrink:0; }
  .r-dsel-row.selected .r-dsel-chk { background:var(--accent); border-color:var(--accent); color:#fff; }
</style>
