<script>
  import { onMount } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  export let activeTab = 'disks';

  let loading = true;
  let pools = [];
  let eligible = [];
  let provisioned = [];
  let nvme = [];
  let selectedDisk = null;

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

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

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
  $: usedPct    = totalBytes > 0 ? (usedBytes / totalBytes) * 100 : 0;

  function fmt(bytes) {
    if (!bytes) return '—';
    const tb = bytes / 1e12;
    if (tb >= 1) return tb.toFixed(1) + ' TB';
    return (bytes / 1e9).toFixed(1) + ' GB';
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

    {:else if activeTab === 'disks'}

      <!-- HDD section -->
      <div class="disk-section">
        <div class="disk-section-label">Discos · HDD / SSD</div>
        <div class="disk-slots-wrap">
          {#each hddSlots as disk, i}
            {#if disk}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="disk-slot" class:selected={selectedDisk?.name === disk.name} on:click={() => selectDisk(disk)}>
                <svg width="58" height="130" viewBox="0 0 58 130" fill="none">
                  <defs>
                    <linearGradient id="hdd{i}bg" x1="0" y1="0" x2="0" y2="130" gradientUnits="userSpaceOnUse">
                      <stop offset="0%" stop-color="#8b7fff"/>
                      <stop offset="100%" stop-color="#5a48dd"/>
                    </linearGradient>
                  </defs>
                  <rect x="0" y="0" width="58" height="130" rx="10" fill="url(#hdd{i}bg)"/>
                  <rect x="4" y="4" width="50" height="104" rx="7" fill="rgba(0,0,0,0.45)"/>
                  <text x="11" y="20" font-size="12" font-weight="700" font-family="DM Sans,sans-serif" fill="rgba(255,255,255,0.85)">{i+1}</text>
                  <text x="29" y="100" text-anchor="middle" font-size="9" font-weight="600" font-family="DM Sans,sans-serif" fill="rgba(255,255,255,0.55)">{fmt(disk.size)}</text>
                  <circle cx="29" cy="120" r="4" fill="#4ade80" style="animation:ledBlink 2s ease-in-out {i*0.5}s infinite"/>
                  <circle cx="29" cy="120" r="7" fill="rgba(74,222,128,0.18)" style="animation:ledBlink 2s ease-in-out {i*0.5}s infinite"/>
                </svg>
                <div class="disk-label">{disk.name}</div>
              </div>
            {:else}
              <div class="disk-slot empty">
                <svg width="58" height="130" viewBox="0 0 58 130" fill="none">
                  <rect x="0" y="0" width="58" height="130" rx="10" fill="rgba(128,128,128,0.14)"/>
                  <rect x="0.75" y="0.75" width="56.5" height="128.5" rx="9.5" stroke="rgba(255,255,255,0.12)" stroke-width="1.5" fill="none"/>
                  <rect x="4" y="4" width="50" height="104" rx="7" fill="rgba(0,0,0,0.20)"/>
                  <text x="11" y="20" font-size="12" font-weight="700" font-family="DM Sans,sans-serif" fill="rgba(255,255,255,0.35)">{i+1}</text>
                  <circle cx="29" cy="120" r="4" fill="rgba(255,255,255,0.12)"/>
                </svg>
                <div class="disk-label empty-label">vacío</div>
              </div>
            {/if}
          {/each}

          <!-- Info panel -->
          <div class="disk-info-panel">
            {#if selectedDisk}
              <div class="di-name">{selectedDisk.model || selectedDisk.name}</div>
              <div class="di-serial">{selectedDisk.serial || '—'}</div>
              <div class="di-row"><span>Dispositivo</span><span>{selectedDisk.name}</span></div>
              <div class="di-row"><span>Capacidad</span><span>{fmt(selectedDisk.size)}</span></div>
              <div class="di-row"><span>Tipo</span><span>{selectedDisk.rota ? 'HDD' : 'SSD'}</span></div>
              {#if selectedDisk.transport}
                <div class="di-row"><span>Interfaz</span><span>{selectedDisk.transport.toUpperCase()}</span></div>
              {/if}
              <div class="di-tags">
                {#if selectedDisk.provisioned}
                  <span class="di-tag green">En pool</span>
                {:else}
                  <span class="di-tag">Libre</span>
                {/if}
              </div>
            {:else}
              <div class="di-empty">
                <div class="di-empty-icon">⊙</div>
                <div>Selecciona un disco</div>
              </div>
            {/if}
          </div>
        </div>
      </div>

      <!-- NVMe section -->
      <div class="disk-section" style="margin-top:18px">
        <div class="disk-section-label">NVMe · M.2</div>
        <div class="nvme-slots-wrap">
          {#each nvmeSlots as disk, i}
            {#if disk}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="disk-slot" class:selected={selectedDisk?.name === disk.name} on:click={() => selectDisk(disk)}>
                <svg width="42" height="130" viewBox="0 0 42 130" fill="none">
                  <defs>
                    <linearGradient id="nv{i}bg" x1="0" y1="0" x2="0" y2="130" gradientUnits="userSpaceOnUse">
                      <stop offset="0%" stop-color="#2e2a4a"/>
                      <stop offset="100%" stop-color="#1e1a36"/>
                    </linearGradient>
                    <linearGradient id="nv{i}slot" x1="0" y1="0" x2="0" y2="1">
                      <stop offset="0%" stop-color="rgba(124,111,255,0.22)"/>
                      <stop offset="100%" stop-color="rgba(124,111,255,0.08)"/>
                    </linearGradient>
                  </defs>
                  <rect x="0" y="0" width="42" height="130" rx="8" fill="url(#nv{i}bg)"/>
                  <rect x="0.75" y="0.75" width="40.5" height="128.5" rx="7.5" stroke="rgba(124,111,255,0.35)" stroke-width="1.5" fill="none"/>
                  <rect x="10" y="3" width="22" height="5" rx="1.5" fill="rgba(255,255,255,0.10)"/>
                  <rect x="13" y="3" width="3" height="3" rx="0.5" fill="rgba(255,255,255,0.22)"/>
                  <rect x="18" y="3" width="3" height="3" rx="0.5" fill="rgba(255,255,255,0.22)"/>
                  <rect x="23" y="3" width="3" height="3" rx="0.5" fill="rgba(255,255,255,0.22)"/>
                  <text x="21" y="22" text-anchor="middle" font-size="10" font-weight="700" font-family="DM Sans,sans-serif" fill="rgba(255,255,255,0.75)">{String.fromCharCode(65+i)}</text>
                  <rect x="6" y="28" width="30" height="18" rx="3" fill="url(#nv{i}slot)" stroke="rgba(124,111,255,0.25)" stroke-width="0.75"/>
                  <rect x="6" y="52" width="30" height="18" rx="3" fill="url(#nv{i}slot)" stroke="rgba(124,111,255,0.25)" stroke-width="0.75"/>
                  <rect x="6" y="76" width="30" height="18" rx="3" fill="url(#nv{i}slot)" stroke="rgba(124,111,255,0.25)" stroke-width="0.75"/>
                  <text x="21" y="112" text-anchor="middle" font-size="8" font-weight="600" font-family="DM Mono,monospace" fill="rgba(255,255,255,0.45)">{fmt(disk.size)}</text>
                  <rect x="8" y="119" width="26" height="4" rx="2" fill="rgba(74,222,128,0.12)"/>
                  <rect x="8" y="119" width="26" height="4" rx="2" fill="#4ade80" style="animation:ledBlink 2.2s ease-in-out {i*0.6}s infinite"/>
                </svg>
                <div class="disk-label">{disk.name}</div>
              </div>
            {:else}
              <div class="disk-slot empty">
                <svg width="42" height="130" viewBox="0 0 42 130" fill="none">
                  <rect x="0" y="0" width="42" height="130" rx="8" fill="rgba(128,128,128,0.12)"/>
                  <rect x="0.75" y="0.75" width="40.5" height="128.5" rx="7.5" stroke="rgba(255,255,255,0.12)" stroke-width="1.5" stroke-dasharray="5 4" fill="none"/>
                  <rect x="10" y="3" width="22" height="5" rx="1.5" fill="rgba(255,255,255,0.08)"/>
                  <text x="21" y="22" text-anchor="middle" font-size="10" font-weight="700" font-family="DM Sans,sans-serif" fill="rgba(255,255,255,0.30)">{String.fromCharCode(65+i)}</text>
                  <rect x="6" y="28" width="30" height="18" rx="3" fill="rgba(255,255,255,0.05)" stroke="rgba(255,255,255,0.10)" stroke-width="0.75"/>
                  <rect x="6" y="52" width="30" height="18" rx="3" fill="rgba(255,255,255,0.05)" stroke="rgba(255,255,255,0.10)" stroke-width="0.75"/>
                  <rect x="6" y="76" width="30" height="18" rx="3" fill="rgba(255,255,255,0.05)" stroke="rgba(255,255,255,0.10)" stroke-width="0.75"/>
                  <rect x="8" y="119" width="26" height="4" rx="2" fill="rgba(255,255,255,0.08)"/>
                </svg>
                <div class="disk-label empty-label">vacío</div>
              </div>
            {/if}
          {/each}
        </div>
      </div>

      <!-- Storage bar -->
      <div class="storage-bar-section">
        <div class="sbs-meta">
          <span class="sbs-label">Capacidad total · {eligible.length + nvme.length} discos</span>
          <span class="sbs-value">{fmt(usedBytes)} / {fmt(totalBytes)} · {usedPct.toFixed(0)}%</span>
        </div>
        <div class="sbs-track">
          <div class="sbs-fill" style="width:{Math.max(0.5, usedPct)}%"></div>
        </div>
      </div>

      <!-- Legend -->
      <div class="disk-legend">
        <div class="dl-item"><div class="dl-dot" style="background:var(--green)"></div>Sano</div>
        <div class="dl-item"><div class="dl-dot" style="background:var(--amber)"></div>Degradado</div>
        <div class="dl-item"><div class="dl-dot" style="background:var(--red)"></div>Error</div>
        <div class="dl-item"><div class="dl-dot" style="background:rgba(128,128,128,0.3)"></div>Vacío</div>
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
      <div class="section-label">Estado de salud</div>
      {#if pools.length > 0}
        {#each pools as pool}
          <div class="pool-row">
            <div class="pool-led" class:healthy={pool.status === 'active'}></div>
            <div class="pool-info">
              <div class="pool-name">{pool.name}</div>
              <div class="pool-meta">{pool.raidLevel || '—'} · {pool.status || '—'} · {pool.usagePercent ?? 0}% usado</div>
            </div>
            <div class="pool-badge" class:green={pool.status === 'active'}>{pool.status || '—'}</div>
          </div>
        {/each}
      {:else}
        <p class="coming-soon">No hay pools para monitorizar</p>
      {/if}

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
    {/if}

  </div>
</div>

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
</style>
