<script>
  import { onMount } from 'svelte';
  import { appState, init } from '$lib/stores/auth.js';
  import { loadPrefs } from '$lib/stores/theme.js';
  import Login from '$lib/components/Login.svelte';
  import SetupWizard from '$lib/components/SetupWizard.svelte';
  import Desktop from '$lib/components/Desktop.svelte';

  onMount(async () => {
    await init();
    await loadPrefs();
  });
</script>

{#if $appState === 'loading'}
  <div class="loading">
    <div class="spinner"></div>
  </div>
{:else if $appState === 'wizard'}
  <SetupWizard />
{:else if $appState === 'login'}
  <Login />
{:else if $appState === 'desktop'}
  <Desktop />
{/if}

<style>
  .loading {
    width: 100%; height: 100vh;
    display: flex; align-items: center; justify-content: center;
    background: #111;
  }
  .spinner {
    width: 32px; height: 32px;
    border: 3px solid rgba(255,255,255,0.1);
    border-top-color: var(--accent, #E95420);
    border-radius: 50%;
    animation: spin 0.8s linear infinite;
  }
  @keyframes spin { to { transform: rotate(360deg); } }
</style>
