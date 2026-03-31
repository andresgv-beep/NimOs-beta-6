<script>
  import { createEventDispatcher } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  export let pools = [];
  export let users = [];
  export let editingShare = null;
  export let saving = false;

  const dispatch = createEventDispatcher();
  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  let wizardStep = 1;
  let savingShare = false;
  let shareMsg = '';
  let shareMsgError = false;
  let quotaGB = '';

  $: isNew = editingShare?._isNew ?? true;
  $: totalSteps = isNew ? 3 : 1;
  $: selectedPool = pools.find(p => p.name === editingShare?.pool);
  $: isZfs = selectedPool?.type === 'zfs' || selectedPool?.filesystem === 'zfs';
  $: poolTotalGB = selectedPool ? Math.floor((selectedPool.total || 0) / 1e9) : 0;

  function close() { dispatch('close'); }

  function nextStep() {
    if (wizardStep === 1) {
      if (!editingShare.name.trim()) { shareMsg = 'Nombre requerido'; shareMsgError = true; return; }
      shareMsg = ''; wizardStep = 2;
    } else if (wizardStep === 2) {
      if (quotaGB !== '' && (isNaN(Number(quotaGB)) || Number(quotaGB) <= 0)) {
        shareMsg = 'Introduce una cantidad válida o deja en blanco'; shareMsgError = true; return;
      }
      if (quotaGB !== '' && poolTotalGB > 0 && Number(quotaGB) > poolTotalGB) {
        shareMsg = `El pool solo tiene ${poolTotalGB} GB disponibles`; shareMsgError = true; return;
      }
      shareMsg = ''; wizardStep = 3;
    }
  }

  function save() {
    const quotaBytes = quotaGB !== '' ? Math.round(Number(quotaGB) * 1e9) : 0;
    dispatch('save', { ...editingShare, quotaBytes });
  }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="modal-overlay" on:click|self={close}></div>
<div class="modal">

  <div class="modal-header">
    <div class="modal-title">{isNew ? 'Nueva carpeta compartida' : `Editar: ${editingShare.displayName || editingShare.name}`}</div>
    {#if isNew}
      <div class="modal-steps">
        {#each Array(totalSteps) as _, i}
          {#if i > 0}<div class="modal-step-line" class:done={wizardStep > i}></div>{/if}
          <div class="modal-step" class:active={wizardStep === i+1} class:done={wizardStep > i+1}>{i+1}</div>
        {/each}
      </div>
    {/if}
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-close" on:click={close}>✕</div>
  </div>

  <div class="modal-body">

    {#if isNew && wizardStep === 1}
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
            <option value={pool.name}>{pool.name} — {pool.totalFormatted || '—'} ({pool.raidLevel || pool.profile || '—'})</option>
          {/each}
        </select>
      </div>

    {:else if isNew && wizardStep === 2}
      <div class="modal-step-label">Espacio asignado</div>
      <div class="quota-pool-info">
        <div class="quota-pool-name">{editingShare.pool}</div>
        {#if poolTotalGB > 0}
          <div class="quota-pool-size">{poolTotalGB} GB disponibles</div>
        {/if}
        {#if isZfs}
          <div class="quota-zfs-badge">ZFS · quota nativa</div>
        {/if}
      </div>
      <div class="form-field">
        <label class="form-label">Capacidad asignada</label>
        <div class="quota-input-wrap">
          <input class="form-input quota-input" type="number" min="1" placeholder="Sin límite" bind:value={quotaGB} />
          <span class="quota-unit">GB</span>
        </div>
        <div class="quota-hint">
          {#if quotaGB === ''}
            La carpeta usará todo el espacio disponible del pool
          {:else}
            Se asignarán <strong>{quotaGB} GB</strong> a esta carpeta compartida
          {/if}
        </div>
      </div>
      {#if quotaGB !== '' && poolTotalGB > 0}
        {@const pct = Math.min(100, (Number(quotaGB) / poolTotalGB) * 100)}
        <div class="quota-bar-wrap">
          <div class="quota-bar-track">
            <div class="quota-bar-fill" style="width:{pct}%;background:{pct>85?'var(--red)':pct>60?'var(--amber)':'var(--accent)'}"></div>
          </div>
          <span class="quota-bar-pct">{pct.toFixed(0)}%</span>
        </div>
      {/if}

    {:else if (isNew && wizardStep === 3) || !isNew}
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
      {#if isNew}
        <div class="modal-summary">
          <div class="summary-label">Resumen</div>
          <div class="summary-row"><span>Nombre</span><span>{editingShare.name}</span></div>
          {#if editingShare.description}<div class="summary-row"><span>Descripción</span><span>{editingShare.description}</span></div>{/if}
          <div class="summary-row"><span>Pool</span><span>{editingShare.pool}</span></div>
          <div class="summary-row"><span>Espacio</span><span>{quotaGB !== '' ? `${quotaGB} GB` : 'Máximo disponible'}</span></div>
        </div>
      {/if}
    {/if}

    {#if shareMsg}<div class="share-msg" class:error={shareMsgError}>{shareMsg}</div>{/if}
  </div>

  <div class="modal-footer">
    {#if wizardStep > 1}
      <button class="btn-secondary" on:click={() => { wizardStep--; shareMsg = ''; }}>← Anterior</button>
    {:else}
      <button class="btn-secondary" on:click={close}>Cancelar</button>
    {/if}
    {#if isNew && wizardStep < totalSteps}
      <button class="btn-accent" on:click={nextStep}>Siguiente →</button>
    {:else}
      <button class="btn-accent" on:click={save} disabled={saving || savingShare}>
        {saving || savingShare ? 'Guardando...' : isNew ? 'Crear carpeta' : 'Guardar cambios'}
      </button>
    {/if}
  </div>
</div>

<style>
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
  .modal-body { padding:18px 20px; overflow-y:auto; max-height:420px; display:flex; flex-direction:column; gap:14px; }
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
  .form-field { display:flex; flex-direction:column; gap:4px; }
  .form-label { font-size:10px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; }
  .form-input, .form-select { padding:8px 12px; border-radius:8px; background:rgba(255,255,255,0.04); border:1px solid var(--border); color:var(--text-1); font-size:12px; font-family:'Inter',sans-serif; outline:none; transition:border-color .2s; }
  .form-input:focus, .form-select:focus { border-color:var(--accent); }
  .form-input::placeholder { color:var(--text-3); }
  .form-select { cursor:pointer; -webkit-appearance:none; appearance:none; background-image:url("data:image/svg+xml,%3Csvg width='10' height='6' viewBox='0 0 10 6' fill='none' xmlns='http://www.w3.org/2000/svg'%3E%3Cpath d='M1 1l4 4 4-4' stroke='%23666' stroke-width='1.5' stroke-linecap='round'/%3E%3C/svg%3E"); background-repeat:no-repeat; background-position:right 12px center; padding-right:32px; }
  .form-select option { background:var(--bg-inner); color:var(--text-1); }
  .share-msg { font-size:11px; padding:4px 0; color:var(--green); }
  .share-msg.error { color:var(--red); }
  .quota-pool-info { display:flex; align-items:center; gap:8px; padding:10px 12px; border-radius:8px; border:1px solid var(--border); background:var(--ibtn-bg); }
  .quota-pool-name { font-size:12px; font-weight:600; color:var(--text-1); font-family:'DM Mono',monospace; }
  .quota-pool-size { font-size:11px; color:var(--text-3); margin-left:auto; }
  .quota-zfs-badge { font-size:9px; font-weight:600; padding:2px 7px; border-radius:4px; background:rgba(124,111,255,0.10); border:1px solid rgba(124,111,255,0.25); color:var(--accent); }
  .quota-input-wrap { position:relative; display:flex; align-items:center; }
  .quota-input { flex:1; padding-right:40px; }
  .quota-unit { position:absolute; right:12px; font-size:11px; color:var(--text-3); font-family:'DM Mono',monospace; pointer-events:none; }
  .quota-hint { font-size:11px; color:var(--text-3); margin-top:2px; }
  .quota-hint strong { color:var(--text-1); }
  .quota-bar-wrap { display:flex; align-items:center; gap:10px; }
  .quota-bar-track { flex:1; height:5px; background:rgba(128,128,128,0.12); border-radius:3px; overflow:hidden; }
  .quota-bar-fill  { height:100%; border-radius:3px; transition:width .3s, background .3s; }
  .quota-bar-pct   { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; flex-shrink:0; width:32px; text-align:right; }
  .perm-table { display:flex; flex-direction:column; gap:2px; }
  .perm-header { display:flex; align-items:center; padding:4px 8px; font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; }
  .perm-row { display:flex; align-items:center; gap:8px; padding:7px 8px; border-radius:6px; border:1px solid var(--border); background:var(--ibtn-bg); }
  .perm-col-user { display:flex; align-items:center; gap:8px; flex:1; min-width:0; }
  .perm-col-perm { flex-shrink:0; }
  .perm-avatar { width:22px; height:22px; border-radius:5px; flex-shrink:0; background:linear-gradient(135deg,var(--accent),var(--accent2)); display:flex; align-items:center; justify-content:center; font-size:9px; font-weight:700; color:#fff; }
  .perm-name { font-size:11px; font-weight:600; color:var(--text-1); }
  .perm-admin-tag { font-size:8px; font-weight:600; text-transform:uppercase; letter-spacing:.04em; padding:1px 5px; border-radius:3px; background:rgba(124,111,255,0.12); color:var(--accent); }
  .perm-select { padding:5px 28px 5px 8px; font-size:10px; min-width:140px; }
  .btn-accent { padding:8px 16px; border-radius:8px; border:none; background:linear-gradient(135deg,var(--accent),var(--accent2)); color:#fff; font-size:11px; font-weight:600; cursor:pointer; font-family:inherit; transition:opacity .15s; }
  .btn-accent:hover { opacity:.88; }
  .btn-accent:disabled { opacity:.5; cursor:not-allowed; }
  .btn-secondary { padding:8px 16px; border-radius:8px; border:1px solid var(--border); background:var(--ibtn-bg); color:var(--text-2); font-size:11px; font-weight:500; cursor:pointer; font-family:inherit; transition:all .15s; }
  .btn-secondary:hover { color:var(--text-1); border-color:var(--border-hi); }
</style>
