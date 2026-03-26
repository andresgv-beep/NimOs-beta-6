<script>
  import { onMount } from 'svelte';

  export let appId = '';
  export let port = null;
  export let name = '';

  let status = 'loading';
  let iframeEl;

  // Use reverse proxy: /app/{appId}/ — same origin, no mixed content, no X-Frame-Options
  $: proxyUrl = `/app/${appId}/`;
  // Direct URL for "open in browser" fallback
  $: directUrl = typeof window !== 'undefined' ? `http://${window.location.hostname}:${port}` : '';

  onMount(() => {
    if (!port || !appId) { status = 'error'; return; }

    const controller = new AbortController();
    const timeout = setTimeout(() => controller.abort(), 10000);

    fetch(proxyUrl, { signal: controller.signal })
      .then((res) => {
        clearTimeout(timeout);
        status = 'ready';
      })
      .catch(() => {
        clearTimeout(timeout);
        status = 'error';
      });

    return () => { clearTimeout(timeout); controller.abort(); };
  });

  function openExternal() {
    window.open(directUrl, '_blank');
  }

  function reload() {
    status = 'loading';
    if (iframeEl) {
      iframeEl.src = 'about:blank';
      setTimeout(() => { iframeEl.src = proxyUrl; status = 'ready'; }, 300);
    } else {
      setTimeout(() => { status = 'ready'; }, 300);
    }
  }
</script>

<div class="webapp">
  {#if status === 'loading'}
    <div class="overlay">
      <div class="spinner"></div>
      <p>Cargando {name || appId}...</p>
    </div>
  {:else if status === 'error'}
    <div class="overlay">
      <div class="error-icon">⚠️</div>
      <h3>No se puede conectar a {name || appId}</h3>
      <p>La app no está corriendo o el puerto {port} no es accesible.</p>
      <div class="actions">
        <button class="btn-secondary" on:click={reload}>Reintentar</button>
        <button class="btn-primary" on:click={openExternal}>Abrir en navegador</button>
      </div>
    </div>
  {:else}
    <iframe
      bind:this={iframeEl}
      class="iframe"
      src={proxyUrl}
      title={name || appId}
      allow="fullscreen; autoplay; clipboard-write"
    ></iframe>
  {/if}
</div>

<style>
  .webapp { width: 100%; height: 100%; position: relative; background: var(--bg-inner, #1c1b3a); }
  .iframe { width: 100%; height: 100%; border: none; display: block; }
  .overlay {
    width: 100%; height: 100%;
    display: flex; flex-direction: column; align-items: center; justify-content: center;
    gap: 12px; color: var(--text-2); text-align: center; padding: 40px;
  }
  .overlay h3 { font-size: 16px; font-weight: 600; color: var(--text-1); }
  .overlay p { font-size: 12px; color: var(--text-3); max-width: 300px; line-height: 1.5; }
  .error-icon { font-size: 40px; }
  .spinner {
    width: 28px; height: 28px; border-radius: 50%;
    border: 2px solid var(--border, rgba(255,255,255,0.1));
    border-top-color: var(--accent, #7c6fff);
    animation: spin 0.7s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
  .actions { display: flex; gap: 8px; margin-top: 8px; }
  .btn-secondary {
    padding: 8px 16px; border-radius: 8px; font-size: 12px; font-weight: 500;
    background: var(--ibtn-bg, rgba(255,255,255,0.06)); border: 1px solid var(--border);
    color: var(--text-1); cursor: pointer; font-family: inherit;
  }
  .btn-primary {
    padding: 8px 16px; border-radius: 8px; font-size: 12px; font-weight: 500;
    background: var(--accent, #7c6fff); border: none;
    color: #fff; cursor: pointer; font-family: inherit;
  }
</style>
