<script>
  import { completeSetup } from '$lib/stores/auth.js';

  let step = 0;
  let username = '';
  let password = '';
  let confirmPw = '';
  let error = '';
  let loading = false;

  async function handleCreate() {
    error = '';
    if (!username.trim()) { error = 'Username is required'; return; }
    if (username.trim().length < 3) { error = 'Username must be at least 3 characters'; return; }
    if (/[^a-zA-Z0-9_.-]/.test(username.trim())) { error = 'Only letters, numbers, -, _, .'; return; }
    if (!password) { error = 'Password is required'; return; }
    if (password.length < 4) { error = 'Password must be at least 4 characters'; return; }
    if (password !== confirmPw) { error = 'Passwords do not match'; return; }

    loading = true;
    try {
      await completeSetup(username.trim(), password);
    } catch (err) {
      error = err.message;
      loading = false;
    }
  }

  function onKey(e) {
    if (e.key === 'Enter') {
      if (step === 0) step = 1;
      else handleCreate();
    }
  }
</script>

<div class="overlay">
  <div class="container">
    <div class="logo">
      <svg viewBox="0 0 24 24" width="40" height="40" fill="none" stroke="currentColor" stroke-width="1.5">
        <path d="M19 11H5m14 0a2 2 0 012 2v6a2 2 0 01-2 2H5a2 2 0 01-2-2v-6a2 2 0 012-2m14 0V9a2 2 0 00-2-2M5 11V9a2 2 0 012-2m0 0V5a2 2 0 012-2h6a2 2 0 012 2v2M7 7h10"/>
      </svg>
    </div>
    <h2 class="title">Welcome to NimOS</h2>

    {#if step === 0}
      <p class="subtitle">
        Let's set up your NAS. First, create an administrator account.
      </p>
      <button class="btn" on:click={() => step = 1}>Get Started →</button>
    {:else}
      <input class="input" type="text" placeholder="Username" bind:value={username} on:keydown={onKey} />
      <input class="input" type="password" placeholder="Password" bind:value={password} on:keydown={onKey} />
      <input class="input" type="password" placeholder="Confirm Password" bind:value={confirmPw} on:keydown={onKey} />

      {#if error}<div class="error">{error}</div>{/if}

      <button class="btn" on:click={handleCreate} disabled={loading}>
        {loading ? 'Creating...' : 'Create Account'}
      </button>
    {/if}
  </div>
</div>

<style>
  .overlay {
    position: fixed; inset: 0;
    display: flex; align-items: center; justify-content: center;
    background: linear-gradient(140deg, #1a1030 0%, #0d0d1a 100%);
  }
  .container {
    display: flex; flex-direction: column; align-items: center;
    gap: 14px; width: 380px; padding: 40px 30px;
  }
  .logo { color: var(--accent, #E95420); margin-bottom: 4px; }
  .title { color: white; font-size: 22px; font-weight: 600; }
  .subtitle { color: rgba(255,255,255,0.5); font-size: 13px; text-align: center; line-height: 1.5; }
  .input {
    width: 100%; padding: 12px 16px;
    background: rgba(255,255,255,0.06); border: 1px solid rgba(255,255,255,0.12);
    border-radius: 10px; color: white; font-size: 14px; font-family: inherit; outline: none;
  }
  .input:focus { border-color: var(--accent, #E95420); }
  .input::placeholder { color: rgba(255,255,255,0.3); }
  .error { color: #f87171; font-size: 12px; }
  .btn {
    width: 100%; padding: 12px;
    background: var(--accent, #E95420); border: none; border-radius: 10px;
    color: white; font-size: 14px; font-weight: 600; cursor: pointer; font-family: inherit;
  }
  .btn:hover { opacity: 0.9; }
  .btn:disabled { opacity: 0.5; }
</style>
