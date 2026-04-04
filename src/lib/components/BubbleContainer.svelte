<script>
  import { fly } from 'svelte/transition';
  import { notifications, hideBubble } from '$lib/stores/notifications.js';
  import { uploadTasks, removeTask } from '$lib/stores/uploadTasks.js';
  import { openWindow } from '$lib/stores/windows.js';

  $: activeTasks = $uploadTasks.filter(t => t.status === 'uploading' || t.status === 'done');

  const DURATION = 5000;
  const MAX = 3;

  $: bubbles = $notifications.filter(n => n.showBubble).slice(0, MAX);

  // Persistent types: warning, error, security — no auto-hide
  const PERSISTENT_TYPES = new Set(['warning', 'error', 'security']);

  const ICONS = {
    success:  '<polyline points="20 6 9 17 4 12"/>',
    error:    '<line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>',
    warning:  '<path d="M10.29 3.86L1.82 18a2 2 0 0 0 1.71 3h16.94a2 2 0 0 0 1.71-3L13.71 3.86a2 2 0 0 0-3.42 0z"/><line x1="12" y1="9" x2="12" y2="13"/><line x1="12" y1="17" x2="12.01" y2="17"/>',
    info:     '<circle cx="12" cy="12" r="10"/><line x1="12" y1="16" x2="12" y2="12"/><line x1="12" y1="8" x2="12.01" y2="8"/>',
    security: '<path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>',
  };

  function getIcon(type) { return ICONS[type] || ICONS.info; }

  function fmtTime(iso) {
    const diff = Math.floor((Date.now() - new Date(iso)) / 1000);
    if (diff < 60) return 'ahora';
    if (diff < 3600) return `hace ${Math.floor(diff/60)}m`;
    return `hace ${Math.floor(diff/3600)}h`;
  }

  // auto-hide action — only for non-persistent types
  function autoHide(node, { id, type }) {
    if (PERSISTENT_TYPES.has(type)) return { destroy() {} };
    const t = setTimeout(() => hideBubble(id), DURATION);
    return { destroy() { clearTimeout(t); } };
  }

  // Click on bubble body — open relevant app
  function onBubbleClick(n) {
    // SMART / disk / storage related
    if (n.category === 'system' && (n.title?.includes('Disco') || n.title?.includes('SMART') || n.title?.includes('Verificación') || n.message?.includes('disco'))) {
      openWindow('storage');
      hideBubble(n.id);
    }
  }
</script>

<div class="bubble-container">
  {#each bubbles as n (n.id)}
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div
      class="bubble b-{n.type}" class:persistent={PERSISTENT_TYPES.has(n.type)}
      in:fly={{ x: 100, duration: 300 }}
      out:fly={{ x: 100, duration: 220 }}
      use:autoHide={{ id: n.id, type: n.type }}
      on:click={() => onBubbleClick(n)}
    >
      <div class="b-stripe"></div>
      <div class="b-ico">
        <svg viewBox="0 0 24 24" fill="none" stroke-width="2.5" stroke-linecap="round">
          {@html getIcon(n.type)}
        </svg>
      </div>
      <div class="b-body">
        {#if n.title}<div class="b-title">{n.title}</div>{/if}
        <div class="b-msg" class:solo={!n.title}>{n.message}</div>
        {#if !PERSISTENT_TYPES.has(n.type)}
          <div class="b-prog"><div class="b-bar" style="animation-duration:{DURATION}ms"></div></div>
        {/if}
      </div>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="b-close" on:click|stopPropagation={() => hideBubble(n.id)}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round">
          <line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/>
        </svg>
      </div>
    </div>
  {/each}

  {#each $uploadTasks as task (task.id)}
    <div class="bubble upload-bubble" class:done={task.status === 'done'} class:error={task.status === 'error'}
      in:fly={{ x: 100, duration: 300 }}
      out:fly={{ x: 100, duration: 220 }}>
      <div class="b-stripe" class:b-success={task.status === 'done'} class:b-error={task.status === 'error'} class:b-info={task.status === 'uploading'}></div>
      <div class="b-ico" class:b-success={task.status === 'done'} class:b-error={task.status === 'error'} class:b-info={task.status === 'uploading'}>
        {#if task.status === 'done'}
          <svg viewBox="0 0 24 24" fill="none" stroke-width="2.5" stroke-linecap="round"><polyline points="20 6 9 17 4 12"/></svg>
        {:else if task.status === 'error'}
          <svg viewBox="0 0 24 24" fill="none" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
        {:else}
          <svg viewBox="0 0 24 24" fill="none" stroke-width="2.5" stroke-linecap="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="17 8 12 3 7 8"/><line x1="12" y1="3" x2="12" y2="15"/></svg>
        {/if}
      </div>
      <div class="b-body">
        <div class="b-title">{task.name}</div>
        {#if task.status === 'uploading'}
          <div class="up-track"><div class="up-fill" style="width:{task.progress}%"></div></div>
          <div class="up-pct">{task.progress}%</div>
        {:else if task.status === 'done'}
          <div class="b-msg" style="color:var(--green)">Subido correctamente</div>
        {:else}
          <div class="b-msg" style="color:var(--red)">{task.error || 'Error al subir'}</div>
        {/if}
      </div>
      {#if task.status !== 'uploading'}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="b-close" on:click={() => removeTask(task.id)}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
        </div>
      {/if}
    </div>
  {/each}
</div>

<style>
  .bubble-container { position:fixed; top:16px; right:16px; z-index:9999; display:flex; flex-direction:column; gap:8px; pointer-events:none; align-items:flex-end; }

  .bubble { width:310px; background:var(--glass-bg); backdrop-filter:blur(20px) saturate(1.4); -webkit-backdrop-filter:blur(20px) saturate(1.4); border:2px solid var(--glass-border); border-radius:11px; padding:11px 12px 0; display:flex; gap:9px; align-items:flex-start; pointer-events:auto; position:relative; overflow:hidden; cursor:pointer; }
  .bubble.persistent { padding-bottom:11px; }

  .b-stripe { position:absolute; left:0; top:8px; bottom:8px; width:3px; border-radius:0 2px 2px 0; }
  .b-success .b-stripe  { background:var(--green); }
  .b-error .b-stripe    { background:var(--red); }
  .b-warning .b-stripe  { background:var(--amber); }
  .b-info .b-stripe     { background:var(--accent); }
  .b-security .b-stripe { background:var(--red); }

  .b-ico { width:24px; height:24px; border-radius:6px; display:flex; align-items:center; justify-content:center; flex-shrink:0; margin-left:6px; margin-top:1px; }
  .b-ico svg { width:11px; height:11px; fill:none; stroke-width:2.5; stroke-linecap:round; }
  .b-success .b-ico  { background:rgba(74,222,128,0.12); } .b-success .b-ico svg  { stroke:var(--green); }
  .b-error .b-ico    { background:rgba(248,113,113,0.12); } .b-error .b-ico svg    { stroke:var(--red); }
  .b-warning .b-ico  { background:rgba(251,191,36,0.12); } .b-warning .b-ico svg  { stroke:var(--amber); }
  .b-info .b-ico     { background:rgba(124,111,255,0.12); } .b-info .b-ico svg     { stroke:var(--accent); }
  .b-security .b-ico { background:rgba(248,113,113,0.12); } .b-security .b-ico svg { stroke:var(--red); }

  .b-body { flex:1; min-width:0; padding-bottom:10px; }
  .b-title { font-size:11px; font-weight:700; color:var(--text-1); }
  .b-msg { font-size:11px; color:var(--text-2); margin-top:2px; line-height:1.4; }
  .b-msg.solo { font-weight:600; color:var(--text-1); margin-top:0; }

  .b-prog { height:2px; background:var(--border); position:absolute; left:0; right:0; bottom:0; overflow:hidden; }
  .b-bar { height:100%; width:100%; animation:shrink linear forwards; }
  @keyframes shrink { from{width:100%} to{width:0} }
  .b-success .b-bar  { background:var(--green); }
  .b-error .b-bar    { background:var(--red); }
  .b-warning .b-bar  { background:var(--amber); }
  .b-info .b-bar     { background:var(--accent); }
  .b-security .b-bar { background:var(--red); }

  .b-close { width:16px; height:16px; flex-shrink:0; display:flex; align-items:center; justify-content:center; cursor:pointer; color:var(--text-3); border-radius:4px; transition:color .15s; margin-top:1px; }
  .b-close:hover { color:var(--red); }
  .b-close svg { width:10px; height:10px; }
  .upload-bubble { animation: none; }
  .upload-bubble.done { border-left:2px solid var(--green); }
  .upload-bubble.error { border-left:2px solid var(--red); }
  .up-track { height:3px; background:var(--border); border-radius:2px; overflow:hidden; margin-top:6px; }
  .up-fill { height:100%; background:var(--accent); border-radius:2px; transition:width .3s ease; }
  .up-pct { font-size:9px; color:var(--text-3); font-family:"DM Mono",monospace; margin-top:3px; }
</style>
