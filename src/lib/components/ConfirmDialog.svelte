<script>
  import { createEventDispatcher } from 'svelte';
  import { fly } from 'svelte/transition';

  const dispatch = createEventDispatcher();

  export let open = false;
  export let variant = 'default'; // 'default' | 'warning' | 'danger'
  export let title = '';
  export let message = '';
  export let confirmText = 'Confirmar';
  export let cancelText = 'Cancelar';
  export let requireInput = false; // require typing "confirmar"
  export let services = []; // [{ name, status }]
  export let loading = false;

  let inputValue = '';

  $: inputValid = inputValue.trim().toLowerCase() === 'confirmar';
  $: canConfirm = requireInput ? inputValid : true;

  function onConfirm() {
    if (!canConfirm || loading) return;
    dispatch('confirm');
  }

  function onCancel() {
    inputValue = '';
    dispatch('cancel');
  }

  function onKeydown(e) {
    if (e.key === 'Escape') onCancel();
    if (e.key === 'Enter' && canConfirm && !loading) onConfirm();
  }
</script>

<svelte:window on:keydown={onKeydown} />

{#if open}
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div class="cd-overlay" on:click|self={onCancel} transition:fly={{ duration: 150 }}>
    <div class="cd {variant}">
      <div class="cd-header">
        <div class="cd-icon">
          {#if variant === 'danger'}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <polyline points="3 6 5 6 21 6"/><path d="M19 6l-1 14a2 2 0 01-2 2H8a2 2 0 01-2-2L5 6"/><path d="M10 11v6m4-6v6"/><path d="M9 6V4a1 1 0 011-1h4a1 1 0 011 1v2"/>
            </svg>
          {:else if variant === 'warning'}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <path d="M10.29 3.86L1.82 18a2 2 0 001.71 3h16.94a2 2 0 001.71-3L13.71 3.86a2 2 0 00-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>
            </svg>
          {:else}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2">
              <circle cx="12" cy="12" r="10"/><path d="M12 8v4m0 4h.01"/>
            </svg>
          {/if}
        </div>
        <div class="cd-title">{title}</div>
      </div>

      <div class="cd-body">
        <p class="cd-message">{@html message}</p>

        {#if services.length > 0}
          <div class="cd-services">
            <div class="cd-services-header">
              <span class="cd-services-label">Servicios activos en este volumen</span>
            </div>
            {#each services as svc}
              <div class="cd-service-row">
                <span class="cd-service-name"><span class="cd-service-dot"></span>{svc.name || svc.appName || svc.appId}</span>
                <span class="cd-service-status">{svc.status === 'running' ? 'activo' : svc.status}</span>
              </div>
            {/each}
          </div>
        {/if}
      </div>

      {#if requireInput}
        <div class="cd-confirm-field">
          <p class="cd-confirm-label">Escribe <span>confirmar</span> para continuar</p>
          <input
            class="cd-confirm-input"
            class:valid={inputValid}
            type="text"
            placeholder="confirmar"
            autocomplete="off"
            spellcheck="false"
            bind:value={inputValue}
          />
        </div>
      {/if}

      <div class="cd-actions">
        <button class="cd-btn cd-btn-cancel" on:click={onCancel}>{cancelText}</button>
        <button
          class="cd-btn cd-btn-confirm cd-btn-{variant}"
          disabled={!canConfirm || loading}
          on:click={onConfirm}
        >
          {loading ? 'Procesando...' : confirmText}
        </button>
      </div>
    </div>
  </div>
{/if}

<style>
  .cd-overlay {
    position: fixed; inset: 0; z-index: 10000;
    background: rgba(0,0,0,0.5);
    display: flex; align-items: center; justify-content: center;
  }

  .cd {
    width: 420px; max-width: 90vw;
    background: var(--glass-bg, rgba(30,32,48,0.72));
    backdrop-filter: blur(24px); -webkit-backdrop-filter: blur(24px);
    border: 1px solid var(--glass-border, rgba(255,255,255,0.08));
    border-radius: 14px;
    padding: 28px 28px 24px;
    box-shadow: 0 24px 64px rgba(0,0,0,0.5), 0 0 0 1px rgba(255,255,255,0.04) inset;
    animation: cdIn 220ms cubic-bezier(0.34, 1.2, 0.64, 1) both;
  }
  @keyframes cdIn { from { opacity:0; transform:scale(0.92) translateY(6px); } to { opacity:1; transform:none; } }

  .cd.danger  { border-top: 2px solid var(--red); }
  .cd.warning { border-top: 2px solid var(--amber); }

  .cd-header { display:flex; align-items:center; gap:12px; margin-bottom:12px; }
  .cd-icon {
    width:36px; height:36px; border-radius:10px;
    display:flex; align-items:center; justify-content:center; flex-shrink:0;
  }
  .cd-icon svg { width:18px; height:18px; stroke-linecap:round; stroke-linejoin:round; }
  .cd.danger  .cd-icon { background:rgba(248,113,113,0.12); color:var(--red); }
  .cd.warning .cd-icon { background:rgba(251,191,36,0.12); color:var(--amber); }
  .cd.default .cd-icon { background:rgba(124,111,255,0.12); color:var(--accent); }

  .cd-title { font-size:15px; font-weight:600; color:var(--text-1); line-height:1.3; }

  .cd-body { padding-left:48px; }
  .cd-message { font-size:13px; color:var(--text-2); line-height:1.6; }

  .cd-services {
    margin-top:10px; border-radius:9px;
    border:1px solid rgba(255,255,255,0.07);
    background:rgba(0,0,0,0.2); overflow:hidden;
  }
  .cd-services-header {
    display:flex; align-items:center; justify-content:space-between;
    padding:8px 12px; border-bottom:1px solid rgba(255,255,255,0.06);
  }
  .cd-services-label {
    font-size:10px; font-weight:600; letter-spacing:.07em;
    text-transform:uppercase; color:rgba(255,255,255,0.3);
  }
  .cd-service-row {
    display:flex; align-items:center; justify-content:space-between;
    padding:8px 12px; font-size:12px;
  }
  .cd-service-row + .cd-service-row { border-top:1px solid rgba(255,255,255,0.04); }
  .cd-service-name { display:flex; align-items:center; gap:7px; color:var(--text-1); }
  .cd-service-dot {
    width:6px; height:6px; border-radius:50%; flex-shrink:0;
    background:var(--green); box-shadow:0 0 5px rgba(74,222,128,0.6);
  }
  .cd-service-status { font-size:11px; color:rgba(255,255,255,0.28); }

  .cd-confirm-field { margin-top:14px; padding-left:48px; }
  .cd-confirm-label { font-size:11px; color:var(--text-2); margin-bottom:6px; line-height:1.5; }
  .cd-confirm-label span {
    font-family:'DM Mono',monospace; font-size:11px;
    padding:1px 6px; border-radius:4px;
  }
  .cd.danger  .cd-confirm-label span { background:rgba(248,113,113,0.12); color:var(--red); }
  .cd.warning .cd-confirm-label span { background:rgba(251,191,36,0.12); color:var(--amber); }
  .cd.default .cd-confirm-label span { background:rgba(124,111,255,0.12); color:var(--accent); }

  .cd-confirm-input {
    width:100%; background:rgba(0,0,0,0.3);
    border:1px solid rgba(255,255,255,0.1); border-radius:8px;
    padding:9px 12px; font-size:13px;
    font-family:'DM Mono',monospace; color:var(--text-1);
    outline:none; transition:border-color .15s, box-shadow .15s;
  }
  .cd-confirm-input::placeholder { color:rgba(255,255,255,0.18); }
  .cd-confirm-input:focus { border-color:rgba(255,255,255,0.2); box-shadow:0 0 0 3px rgba(255,255,255,0.04); }
  .cd.danger  .cd-confirm-input.valid { border-color:var(--red);   box-shadow:0 0 0 3px rgba(248,113,113,0.12); }
  .cd.warning .cd-confirm-input.valid { border-color:var(--amber); box-shadow:0 0 0 3px rgba(251,191,36,0.12); }

  .cd-actions { display:flex; justify-content:flex-end; gap:8px; margin-top:20px; }

  .cd-btn {
    padding:8px 18px; border-radius:9px; font-size:13px; font-weight:500;
    cursor:pointer; border:1px solid transparent; transition:all .12s;
    outline:none; font-family:inherit;
  }
  .cd-btn:disabled { opacity:.28; cursor:not-allowed; }

  .cd-btn-cancel {
    background:rgba(255,255,255,0.06); border-color:rgba(255,255,255,0.08);
    color:var(--text-2);
  }
  .cd-btn-cancel:hover { background:rgba(255,255,255,0.1); color:var(--text-1); }

  .cd-btn-default { background:var(--accent); color:#fff; }
  .cd-btn-default:hover:not(:disabled) { filter:brightness(0.9); }

  .cd-btn-danger { background:var(--red); color:#fff; }
  .cd-btn-danger:hover:not(:disabled) { filter:brightness(0.85); }

  .cd-btn-warning { background:var(--amber); color:#1a1200; }
  .cd-btn-warning:hover:not(:disabled) { filter:brightness(0.85); }
</style>
