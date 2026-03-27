<script>
  import { createEventDispatcher } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  export let pools = [];
  export let users = [];
  export let editingShare = null; // null = new, object = edit

  const dispatch = createEventDispatcher();
  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  let wizardStep = 1;
  let savingShare = false;
  let shareMsg = '';
  let shareMsgError = false;

  $: isNew = editingShare?._isNew ?? true;

  function close() { dispatch('close'); }
  function done()  { dispatch('done'); }
</script>

<!-- svelte-ignore a11y_click_events_have_key_events -->
<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="modal-overlay" on:click|self={close}></div>
<div class="modal">
  <div class="modal-header">
    <div class="modal-title">{isNew ? 'Nueva carpeta compartida' : `Editar: ${editingShare.displayName || editingShare.name}`}</div>
    <div class="modal-steps">
      <div class="modal-step" class:active={wizardStep === 1} class:done={wizardStep > 1}>1</div>
      {#if isNew}
        <div class="modal-step-line" class:done={wizardStep > 1}></div>
        <div class="modal-step" class:active={wizardStep === 2}>2</div>
      {/if}
    </div>
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="modal-close" on:click={close}>✕</div>
  </div>

  <div class="modal-body">
    {#if wizardStep === 1}
      {#if isNew}
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
      <button class="btn-secondary" on:click={close}>Cancelar</button>
    {/if}
    {#if isNew && wizardStep === 1}
      <button class="btn-accent" on:click={() => {
        if (!editingShare.name.trim()) { shareMsg = 'Nombre requerido'; shareMsgError = true; return; }
        shareMsg = ''; wizardStep = 2;
      }}>Siguiente →</button>
    {:else}
      <button class="btn-accent" on:click={() => dispatch('save', editingShare)} disabled={savingShare}>
        {savingShare ? 'Guardando...' : isNew ? 'Crear carpeta' : 'Guardar cambios'}
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
  .form-field { display:flex; flex-direction:column; gap:4px; }
  .form-label { font-size:10px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; }
  .form-input, .form-select { padding:8px 12px; border-radius:8px; background:rgba(255,255,255,0.04); border:1px solid var(--border); color:var(--text-1); font-size:12px; font-family:'Inter',sans-serif; outline:none; transition:border-color .2s; }
  .form-input:focus, .form-select:focus { border-color:var(--accent); }
  .form-input::placeholder { color:var(--text-3); }
  .form-select { cursor:pointer; -webkit-appearance:none; appearance:none; background-image:url("data:image/svg+xml,%3Csvg width='10' height='6' viewBox='0 0 10 6' fill='none' xmlns='http://www.w3.org/2000/svg'%3E%3Cpath d='M1 1l4 4 4-4' stroke='%23666' stroke-width='1.5' stroke-linecap='round'/%3E%3C/svg%3E"); background-repeat:no-repeat; background-position:right 12px center; padding-right:32px; }
  .form-select option { background:var(--bg-inner); color:var(--text-1); }
  .share-msg { font-size:11px; padding:4px 0; color:var(--green); }
  .share-msg.error { color:var(--red); }
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
