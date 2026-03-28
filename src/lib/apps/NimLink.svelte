<script>
  import { createEventDispatcher, onMount, onDestroy } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  export let mode = 'pair'; // 'pair' | 'job' | 'sync'
  export let device = null; // for job/sync modes — the target device
  export let pools = [];    // available local pools for job creation

  const dispatch = createEventDispatcher();
  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}`, 'Content-Type': 'application/json' });

  // ── Wizard state ──
  let step = 1;
  let loading = false;
  let msg = '';
  let msgError = false;

  // ── Pair mode state ──
  let discovered = [];
  let scanning = false;
  let selectedAddr = '';
  let manualAddr = '';
  let useManual = false;
  let pairUser = '';
  let pairPass = '';
  let pairTotp = '';
  let needs2FA = false;
  let pairResult = null;

  // ── Job mode state ──
  let jobName = '';
  let jobFsType = 'zfs';
  let jobSource = '';
  let jobDest = '';
  let jobScheduleType = 'daily';
  let jobScheduleTime = '02:00';
  let jobScheduleDay = 'mon';
  let jobRetention = '30d';

  // ── Sync mode state ──
  let syncLocal = '';
  let syncRemote = '';

  $: totalSteps = mode === 'pair' ? 3 : mode === 'job' ? 2 : 2;
  $: selectedDevice = discovered.find(d => d.addr === selectedAddr);
  $: targetAddr = useManual ? manualAddr : selectedAddr;

  function close() { dispatch('close'); }

  // ── Discovery ──
  let discoveryInterval;

  onMount(() => {
    if (mode === 'pair') {
      loadDiscovered();
      discoveryInterval = setInterval(loadDiscovered, 10000);
    }
  });

  onDestroy(() => {
    if (discoveryInterval) clearInterval(discoveryInterval);
  });

  async function loadDiscovered() {
    try {
      const r = await fetch('/api/backup/discovered', { headers: hdrs() });
      const d = await r.json();
      discovered = d.devices || [];
    } catch { /* silent */ }
  }

  async function rescan() {
    scanning = true;
    msg = '';
    try {
      const r = await fetch('/api/backup/pair/scan', {
        method: 'POST', headers: hdrs(), body: '{}'
      });
      const d = await r.json();
      discovered = d.devices || [];
      if (discovered.length === 0) {
        msg = 'No se encontraron dispositivos NimOS en la red';
        msgError = true;
      }
    } catch {
      msg = 'Error al escanear la red';
      msgError = true;
    }
    scanning = false;
  }

  // ── Pairing ──
  async function doPair() {
    if (!targetAddr) { msg = 'Selecciona un dispositivo'; msgError = true; return; }
    if (!pairUser || !pairPass) { msg = 'Credenciales requeridas'; msgError = true; return; }

    loading = true; msg = '';
    try {
      const body = { addr: targetAddr, username: pairUser, password: pairPass };
      if (pairTotp) body.totpCode = pairTotp;

      const r = await fetch('/api/backup/pair/connect', {
        method: 'POST', headers: hdrs(), body: JSON.stringify(body)
      });
      const d = await r.json();

      if (d.requires2FA) {
        needs2FA = true;
        msg = 'El dispositivo requiere código 2FA';
        msgError = false;
        loading = false;
        return;
      }

      if (d.error) {
        msg = d.error;
        msgError = true;
        loading = false;
        return;
      }

      pairResult = d;
      step = 3;
      msg = '';
    } catch (e) {
      msg = 'Error de conexión: ' + e.message;
      msgError = true;
    }
    loading = false;
  }

  // ── Job creation ──
  function buildSchedule() {
    if (jobScheduleType === 'daily') return `daily ${jobScheduleTime}`;
    if (jobScheduleType === 'weekly') return `weekly ${jobScheduleDay} ${jobScheduleTime}`;
    if (jobScheduleType === 'hourly') return 'hourly';
    return `daily ${jobScheduleTime}`;
  }

  async function createJob() {
    if (!jobName.trim()) { msg = 'Nombre requerido'; msgError = true; return; }
    if (!jobSource.trim()) { msg = 'Origen requerido'; msgError = true; return; }
    if (!jobDest.trim()) { msg = 'Destino requerido'; msgError = true; return; }

    loading = true; msg = '';
    try {
      const r = await fetch('/api/backup/jobs', {
        method: 'POST', headers: hdrs(),
        body: JSON.stringify({
          name: jobName,
          deviceId: device.id,
          fsType: jobFsType,
          source: jobSource,
          dest: jobDest,
          schedule: buildSchedule(),
          retention: jobRetention
        })
      });
      const d = await r.json();
      if (d.error) { msg = d.error; msgError = true; }
      else { dispatch('created', d); close(); }
    } catch (e) { msg = e.message; msgError = true; }
    loading = false;
  }

  // ── Sync pair ──
  async function createSyncPair() {
    if (!syncLocal.trim() || !syncRemote.trim()) {
      msg = 'Ambas rutas son requeridas'; msgError = true; return;
    }
    loading = true; msg = '';
    try {
      const existing = device.syncPairs || [];
      const newPairs = [...existing, {
        id: 'pair_' + Date.now(),
        local: syncLocal,
        remote: syncRemote,
        status: 'pending'
      }];
      const r = await fetch(`/api/backup/devices/${device.id}/sync-pairs`, {
        method: 'POST', headers: hdrs(),
        body: JSON.stringify({ syncPairs: newPairs })
      });
      const d = await r.json();
      if (d.error) { msg = d.error; msgError = true; }
      else { dispatch('created', d); close(); }
    } catch (e) { msg = e.message; msgError = true; }
    loading = false;
  }

  // ── Navigation ──
  function nextStep() {
    msg = ''; msgError = false;
    if (mode === 'pair') {
      if (step === 1) {
        if (!targetAddr) { msg = 'Selecciona un dispositivo o introduce una dirección'; msgError = true; return; }
        step = 2;
      } else if (step === 2) {
        doPair();
        return;
      }
    } else if (mode === 'job') {
      if (step === 1) {
        if (!jobName.trim() || !jobSource.trim() || !jobDest.trim()) {
          msg = 'Todos los campos son obligatorios'; msgError = true; return;
        }
        step = 2;
      }
    } else if (mode === 'sync') {
      if (step === 1) {
        if (!syncLocal.trim() || !syncRemote.trim()) {
          msg = 'Ambas rutas son requeridas'; msgError = true; return;
        }
        step = 2;
      }
    }
  }

  function prevStep() {
    msg = ''; msgError = false;
    if (step > 1) step--;
    if (mode === 'pair' && step === 1) { needs2FA = false; pairTotp = ''; }
  }

  function finish() {
    if (mode === 'pair') {
      dispatch('paired', pairResult);
      close();
    } else if (mode === 'job') {
      createJob();
    } else if (mode === 'sync') {
      createSyncPair();
    }
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="modal-overlay" on:click|self={close}></div>
<div class="modal">

  <div class="modal-header">
    <div class="modal-title">
      {#if mode === 'pair'}Emparejar dispositivo
      {:else if mode === 'job'}Nuevo trabajo de backup
      {:else}Nuevo par de sincronización
      {/if}
    </div>
    <div class="modal-steps">
      {#each Array(totalSteps) as _, i}
        {#if i > 0}<div class="modal-step-line" class:done={step > i}></div>{/if}
        <div class="modal-step" class:active={step === i+1} class:done={step > i+1}>{i+1}</div>
      {/each}
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-close" on:click={close}>✕</div>
  </div>

  <div class="modal-body">

    <!-- ═══ PAIR MODE ═══ -->
    {#if mode === 'pair'}

      <!-- Step 1: Select device -->
      {#if step === 1}
        <div class="modal-step-label">Seleccionar dispositivo</div>

        {#if discovered.length > 0}
          <div class="device-list">
            {#each discovered as dev}
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="device-option" class:selected={selectedAddr === dev.addr && !useManual}
                on:click={() => { selectedAddr = dev.addr; useManual = false; }}>
                <div class="dev-opt-icon">
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><rect x="2" y="3" width="20" height="8" rx="2"/><rect x="2" y="13" width="20" height="8" rx="2"/><circle cx="18" cy="7" r="1" fill="currentColor" stroke="none"/><circle cx="18" cy="17" r="1" fill="currentColor" stroke="none"/></svg>
                </div>
                <div class="dev-opt-info">
                  <div class="dev-opt-name">{dev.name}</div>
                  <div class="dev-opt-addr">{dev.addr}{dev.version !== 'unknown' ? ` · ${dev.version}` : ''}</div>
                </div>
                <div class="dev-opt-dot"></div>
              </div>
            {/each}
          </div>
        {:else}
          <div class="empty-box">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round" style="width:28px;height:28px;color:var(--text-3);margin-bottom:6px"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
            <span>{scanning ? 'Escaneando red...' : 'Buscando dispositivos NimOS...'}</span>
          </div>
        {/if}

        <button class="btn-scan" on:click={rescan} disabled={scanning}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="width:12px;height:12px"><polyline points="23 4 23 10 17 10"/><path d="M20.49 15a9 9 0 1 1-.18-5.4"/></svg>
          {scanning ? 'Escaneando...' : 'Escanear red'}
        </button>

        <div class="separator"><span>o introduce una dirección</span></div>

        <div class="form-field">
          <input class="form-input" type="text" placeholder="192.168.1.100 o mi-nas.duckdns.org"
            bind:value={manualAddr}
            on:focus={() => { useManual = true; selectedAddr = ''; }}
            on:input={() => useManual = true} />
        </div>

      <!-- Step 2: Credentials -->
      {:else if step === 2}
        <div class="modal-step-label">Autenticación</div>
        <div class="target-info">
          <div class="dev-opt-icon small">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><rect x="2" y="3" width="20" height="8" rx="2"/><rect x="2" y="13" width="20" height="8" rx="2"/><circle cx="18" cy="7" r="1" fill="currentColor" stroke="none"/><circle cx="18" cy="17" r="1" fill="currentColor" stroke="none"/></svg>
          </div>
          <span class="target-addr">{selectedDevice?.name || targetAddr}</span>
          <span class="target-sub">{targetAddr}</span>
        </div>

        <div class="form-field">
          <label class="form-label">Usuario del NAS remoto</label>
          <input class="form-input" type="text" placeholder="admin" bind:value={pairUser} autofocus />
        </div>
        <div class="form-field">
          <label class="form-label">Contraseña</label>
          <input class="form-input" type="password" placeholder="••••••••" bind:value={pairPass}
            on:keydown={(e) => e.key === 'Enter' && nextStep()} />
        </div>

        {#if needs2FA}
          <div class="totp-box">
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="width:16px;height:16px;color:var(--accent);flex-shrink:0"><rect x="3" y="11" width="18" height="11" rx="2"/><path d="M7 11V7a5 5 0 0 1 10 0v4"/></svg>
            <div class="totp-info">
              <div class="totp-label">Código 2FA requerido</div>
              <input class="form-input totp-input" type="text" placeholder="123456" maxlength="6"
                bind:value={pairTotp}
                on:keydown={(e) => e.key === 'Enter' && nextStep()} />
            </div>
          </div>
        {/if}

      <!-- Step 3: Confirmation -->
      {:else if step === 3 && pairResult}
        <div class="modal-step-label">Emparejamiento exitoso</div>
        <div class="success-icon">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" style="width:32px;height:32px"><polyline points="20 6 9 17 4 12"/></svg>
        </div>
        <div class="modal-summary">
          <div class="summary-label">Dispositivo emparejado</div>
          <div class="summary-row"><span>Nombre</span><span>{pairResult.name}</span></div>
          <div class="summary-row"><span>Dirección</span><span>{pairResult.addr}</span></div>
          <div class="summary-row"><span>Versión</span><span>{pairResult.version || '—'}</span></div>
          {#if pairResult.wireguard?.ok}
            <div class="summary-row"><span>Túnel</span><span style="color:var(--green)">WireGuard activo</span></div>
          {/if}
        </div>
      {/if}

    <!-- ═══ JOB MODE ═══ -->
    {:else if mode === 'job'}

      {#if step === 1}
        <div class="modal-step-label">Configuración del backup</div>
        <div class="form-field">
          <label class="form-label">Nombre del trabajo</label>
          <input class="form-input" type="text" placeholder="Backup documentos → {device?.name || 'remoto'}"
            bind:value={jobName} autofocus />
        </div>
        <div class="form-row">
          <div class="form-field" style="flex:1">
            <label class="form-label">Filesystem</label>
            <select class="form-select" bind:value={jobFsType}>
              <option value="zfs">ZFS</option>
              <option value="btrfs">Btrfs</option>
            </select>
          </div>
          <div class="form-field" style="flex:1">
            <label class="form-label">Retención</label>
            <select class="form-select" bind:value={jobRetention}>
              <option value="7d">7 días</option>
              <option value="14d">14 días</option>
              <option value="30d">30 días</option>
              <option value="90d">90 días</option>
              <option value="12">12 snapshots</option>
              <option value="24">24 snapshots</option>
            </select>
          </div>
        </div>
        <div class="form-field">
          <label class="form-label">Dataset / ruta origen (local)</label>
          <input class="form-input mono" type="text"
            placeholder={jobFsType === 'zfs' ? 'nimos-volume1/data' : '/nimbus/pools/volume1/data'}
            bind:value={jobSource} />
        </div>
        <div class="form-field">
          <label class="form-label">Destino en {device?.name || 'remoto'}</label>
          <input class="form-input mono" type="text"
            placeholder={jobFsType === 'zfs' ? 'backup/data' : '/nimbus/backups/data'}
            bind:value={jobDest} />
        </div>

      {:else if step === 2}
        <div class="modal-step-label">Programación</div>
        <div class="form-field">
          <label class="form-label">Frecuencia</label>
          <select class="form-select" bind:value={jobScheduleType}>
            <option value="daily">Diario</option>
            <option value="weekly">Semanal</option>
            <option value="hourly">Cada hora</option>
          </select>
        </div>

        {#if jobScheduleType !== 'hourly'}
          <div class="form-row">
            {#if jobScheduleType === 'weekly'}
              <div class="form-field" style="flex:1">
                <label class="form-label">Día</label>
                <select class="form-select" bind:value={jobScheduleDay}>
                  <option value="mon">Lunes</option>
                  <option value="tue">Martes</option>
                  <option value="wed">Miércoles</option>
                  <option value="thu">Jueves</option>
                  <option value="fri">Viernes</option>
                  <option value="sat">Sábado</option>
                  <option value="sun">Domingo</option>
                </select>
              </div>
            {/if}
            <div class="form-field" style="flex:1">
              <label class="form-label">Hora (UTC)</label>
              <input class="form-input" type="time" bind:value={jobScheduleTime} />
            </div>
          </div>
        {/if}

        <div class="modal-summary" style="margin-top:8px">
          <div class="summary-label">Resumen del trabajo</div>
          <div class="summary-row"><span>Nombre</span><span>{jobName}</span></div>
          <div class="summary-row"><span>Tipo</span><span>{jobFsType.toUpperCase()} incremental</span></div>
          <div class="summary-row"><span>Origen</span><span>{jobSource}</span></div>
          <div class="summary-row"><span>Destino</span><span>{jobDest}</span></div>
          <div class="summary-row"><span>Programa</span><span>{buildSchedule()}</span></div>
          <div class="summary-row"><span>Retención</span><span>{jobRetention}</span></div>
        </div>
      {/if}

    <!-- ═══ SYNC MODE ═══ -->
    {:else if mode === 'sync'}

      {#if step === 1}
        <div class="modal-step-label">Carpetas a sincronizar</div>
        <div class="form-field">
          <label class="form-label">Carpeta local</label>
          <input class="form-input mono" type="text" placeholder="/nimbus/pools/volume1/documentos"
            bind:value={syncLocal} autofocus />
        </div>
        <div class="sync-arrow-center">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="width:18px;height:18px"><polyline points="17 1 21 5 17 9"/><path d="M3 11V9a4 4 0 0 1 4-4h14"/><polyline points="7 23 3 19 7 15"/><path d="M21 13v2a4 4 0 0 1-4 4H3"/></svg>
        </div>
        <div class="form-field">
          <label class="form-label">Carpeta en {device?.name || 'remoto'}</label>
          <input class="form-input mono" type="text" placeholder="/nimbus/pools/volume1/documentos"
            bind:value={syncRemote} />
        </div>

      {:else if step === 2}
        <div class="modal-step-label">Confirmar</div>
        <div class="modal-summary">
          <div class="summary-label">Par de sincronización</div>
          <div class="summary-row"><span>Local</span><span>{syncLocal}</span></div>
          <div class="summary-row"><span>Remoto ({device?.name})</span><span>{syncRemote}</span></div>
          <div class="summary-row"><span>Modo</span><span>Bidireccional</span></div>
        </div>
      {/if}

    {/if}

    {#if msg}<div class="share-msg" class:error={msgError}>{msg}</div>{/if}
  </div>

  <div class="modal-footer">
    {#if step > 1 && !(mode === 'pair' && step === 3)}
      <button class="btn-secondary" on:click={prevStep}>← Anterior</button>
    {:else if mode === 'pair' && step === 3}
      <div></div>
    {:else}
      <button class="btn-secondary" on:click={close}>Cancelar</button>
    {/if}

    {#if mode === 'pair' && step === 3}
      <button class="btn-accent" on:click={finish}>Cerrar</button>
    {:else if step < totalSteps}
      <button class="btn-accent" on:click={nextStep} disabled={loading}>
        {loading ? 'Conectando...' : 'Siguiente →'}
      </button>
    {:else}
      <button class="btn-accent" on:click={finish} disabled={loading}>
        {#if loading}Guardando...
        {:else if mode === 'pair'}Emparejar
        {:else if mode === 'job'}Crear trabajo
        {:else}Crear par sync
        {/if}
      </button>
    {/if}
  </div>
</div>

<style>
  .modal-overlay { position:fixed; inset:0; z-index:200; background:rgba(0,0,0,0.60); backdrop-filter:blur(3px); }
  .modal { position:fixed; top:50%; left:50%; transform:translate(-50%,-50%); z-index:201; width:480px; max-width:90%; background:var(--bg-inner); border-radius:12px; border:1px solid var(--border); box-shadow:0 24px 60px rgba(0,0,0,0.5); display:flex; flex-direction:column; overflow:hidden; animation:modalIn .2s cubic-bezier(0.16,1,0.3,1) both; }
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

  .modal-body { padding:18px 20px; overflow-y:auto; max-height:440px; display:flex; flex-direction:column; gap:14px; }
  .modal-body::-webkit-scrollbar { width:3px; }
  .modal-body::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }
  .modal-step-label { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.08em; }

  .modal-footer { display:flex; align-items:center; justify-content:flex-end; gap:8px; padding:12px 18px; border-top:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; }

  .modal-summary { padding:12px 14px; border-radius:8px; border:1px solid var(--border); background:rgba(128,128,128,0.04); }
  .summary-label { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; margin-bottom:8px; }
  .summary-row { display:flex; justify-content:space-between; padding:5px 0; border-bottom:1px solid var(--border); font-size:11px; }
  .summary-row:last-child { border-bottom:none; }
  .summary-row span:first-child { color:var(--text-3); }
  .summary-row span:last-child  { color:var(--text-1); font-family:'DM Mono',monospace; }

  /* Forms */
  .form-field { display:flex; flex-direction:column; gap:4px; }
  .form-label { font-size:10px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; }
  .form-input, .form-select { padding:8px 12px; border-radius:8px; background:rgba(255,255,255,0.04); border:1px solid var(--border); color:var(--text-1); font-size:12px; font-family:'Inter',sans-serif; outline:none; transition:border-color .2s; }
  .form-input:focus, .form-select:focus { border-color:var(--accent); }
  .form-input::placeholder { color:var(--text-3); }
  .form-input.mono { font-family:'DM Mono',monospace; font-size:11px; }
  .form-select { cursor:pointer; -webkit-appearance:none; appearance:none; background-image:url("data:image/svg+xml,%3Csvg width='10' height='6' viewBox='0 0 10 6' fill='none' xmlns='http://www.w3.org/2000/svg'%3E%3Cpath d='M1 1l4 4 4-4' stroke='%23666' stroke-width='1.5' stroke-linecap='round'/%3E%3C/svg%3E"); background-repeat:no-repeat; background-position:right 12px center; padding-right:32px; }
  .form-select option { background:var(--bg-inner); color:var(--text-1); }
  .form-row { display:flex; gap:10px; }

  /* Device list */
  .device-list { display:flex; flex-direction:column; gap:4px; }
  .device-option { display:flex; align-items:center; gap:10px; padding:10px 12px; border-radius:9px; border:1px solid var(--border); background:rgba(255,255,255,0.02); cursor:pointer; transition:all .15s; }
  .device-option:hover { border-color:rgba(255,255,255,0.12); background:rgba(255,255,255,0.04); }
  .device-option.selected { border-color:var(--accent); background:rgba(124,111,255,0.08); }
  .dev-opt-icon { width:32px; height:32px; border-radius:8px; flex-shrink:0; display:flex; align-items:center; justify-content:center; background:linear-gradient(135deg,rgba(124,111,255,0.15),rgba(192,84,240,0.15)); border:1px solid rgba(124,111,255,0.2); color:var(--text-2); }
  .dev-opt-icon :global(svg) { width:15px; height:15px; }
  .dev-opt-icon.small { width:24px; height:24px; border-radius:6px; }
  .dev-opt-icon.small :global(svg) { width:12px; height:12px; }
  .dev-opt-info { flex:1; min-width:0; }
  .dev-opt-name { font-size:12px; font-weight:600; color:var(--text-1); }
  .dev-opt-addr { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; }
  .dev-opt-dot { width:8px; height:8px; border-radius:50%; background:var(--green); box-shadow:0 0 5px rgba(74,222,128,.4); flex-shrink:0; }

  .empty-box { display:flex; flex-direction:column; align-items:center; justify-content:center; padding:24px; border:1px dashed rgba(255,255,255,0.08); border-radius:9px; color:var(--text-3); font-size:11px; gap:4px; }

  .btn-scan { display:flex; align-items:center; justify-content:center; gap:6px; padding:7px 14px; border-radius:7px; border:1px solid var(--border); background:var(--ibtn-bg); color:var(--text-2); font-family:inherit; font-size:11px; font-weight:500; cursor:pointer; transition:all .15s; align-self:center; }
  .btn-scan:hover { color:var(--text-1); border-color:var(--border-hi); }
  .btn-scan:disabled { opacity:.5; cursor:not-allowed; }

  .separator { display:flex; align-items:center; gap:12px; color:var(--text-3); font-size:10px; }
  .separator::before, .separator::after { content:''; flex:1; height:1px; background:var(--border); }

  /* Target info bar */
  .target-info { display:flex; align-items:center; gap:8px; padding:10px 12px; border-radius:8px; background:rgba(124,111,255,0.06); border:1px solid rgba(124,111,255,0.15); }
  .target-addr { font-size:12px; font-weight:600; color:var(--text-1); }
  .target-sub  { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; margin-left:auto; }

  /* 2FA box */
  .totp-box { display:flex; align-items:flex-start; gap:10px; padding:12px 14px; border-radius:8px; border:1px solid rgba(124,111,255,0.2); background:rgba(124,111,255,0.06); }
  .totp-info { flex:1; display:flex; flex-direction:column; gap:6px; }
  .totp-label { font-size:11px; font-weight:600; color:var(--text-1); }
  .totp-input { font-family:'DM Mono',monospace; font-size:16px; letter-spacing:4px; text-align:center; padding:8px; }

  /* Success */
  .success-icon { display:flex; align-items:center; justify-content:center; width:56px; height:56px; border-radius:50%; background:rgba(74,222,128,0.12); border:2px solid rgba(74,222,128,0.3); color:var(--green); align-self:center; margin:4px 0; }

  /* Sync arrow */
  .sync-arrow-center { display:flex; align-items:center; justify-content:center; color:var(--accent); padding:2px 0; }

  /* Messages */
  .share-msg { font-size:11px; padding:4px 0; color:var(--green); }
  .share-msg.error { color:var(--red); }

  /* Buttons */
  .btn-accent { padding:8px 16px; border-radius:8px; border:none; background:linear-gradient(135deg,var(--accent),var(--accent2)); color:#fff; font-size:11px; font-weight:600; cursor:pointer; font-family:inherit; transition:opacity .15s; }
  .btn-accent:hover { opacity:.88; }
  .btn-accent:disabled { opacity:.5; cursor:not-allowed; }
  .btn-secondary { padding:8px 16px; border-radius:8px; border:1px solid var(--border); background:var(--ibtn-bg); color:var(--text-2); font-size:11px; font-weight:500; cursor:pointer; font-family:inherit; transition:all .15s; }
  .btn-secondary:hover { color:var(--text-1); border-color:var(--border-hi); }
</style>
