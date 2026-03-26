<script>
  import { login as doLogin, user } from '$lib/stores/auth.js';

  let username = $user?.username || '';
  let password = '';
  let totpCode = '';
  let needs2FA = false;
  let error = '';
  let loading = false;

  async function handleSubmit() {
    if (!username.trim() || !password) { error = 'Enter username and password'; return; }
    if (needs2FA && !totpCode) { error = 'Enter the 6-digit code'; return; }

    error = '';
    loading = true;
    try {
      const result = await doLogin(username.trim(), password, needs2FA ? totpCode : undefined);
      if (result?.requires2FA) {
        needs2FA = true;
        loading = false;
        return;
      }
    } catch (err) {
      error = err.message || 'Login failed';
      if (needs2FA) totpCode = '';
      loading = false;
    }
  }

  function onKey(e) {
    if (e.key === 'Enter') handleSubmit();
    if (error) error = '';
  }
</script>

<div class="overlay">
  <div class="container">
    <div class="avatar">
      {username ? username[0].toUpperCase() : '?'}
    </div>

    {#if !needs2FA}
      <input
        class="input"
        type="text"
        placeholder="Username"
        bind:value={username}
        on:keydown={onKey}
      />
      <input
        class="input"
        type="password"
        placeholder="Password"
        bind:value={password}
        on:keydown={onKey}
      />
    {:else}
      <div class="totp-label">Enter the 6-digit code from your authenticator app</div>
      <input
        class="input totp"
        type="text"
        placeholder="000000"
        maxlength="6"
        bind:value={totpCode}
        on:keydown={onKey}
        on:input={() => totpCode = totpCode.replace(/\D/g, '')}
      />
      <button class="back-link" on:click={() => { needs2FA = false; totpCode = ''; error = ''; }}>
        Back to login
      </button>
    {/if}

    {#if error}
      <div class="error">{error}</div>
    {/if}

    <button class="login-btn" on:click={handleSubmit} disabled={loading}>
      {loading ? 'Signing in...' : needs2FA ? 'Verify' : 'Sign In'}
    </button>

    <div class="footer">NimOS</div>
  </div>
</div>

<style>
  .overlay {
    position: fixed; inset: 0; z-index: 10000;
    display: flex; align-items: center; justify-content: center;
    background:
      radial-gradient(ellipse 80% 60% at 10% 50%, rgba(80,140,255,0.6) 0%, transparent 55%),
      radial-gradient(ellipse 60% 80% at 90% 20%, rgba(230,80,255,0.5) 0%, transparent 50%),
      linear-gradient(140deg, #1a1030 0%, #0d0d1a 100%);
  }

  .container {
    display: flex; flex-direction: column; align-items: center;
    gap: 12px; width: 320px; padding: 40px 30px 30px;
  }

  .avatar {
    width: 72px; height: 72px; border-radius: 50%;
    background: linear-gradient(135deg, var(--accent, #E95420), #c040d0);
    display: flex; align-items: center; justify-content: center;
    font-size: 28px; font-weight: 600; color: white;
    margin-bottom: 8px;
    box-shadow: 0 4px 20px rgba(233,84,32,0.3);
  }

  .input {
    width: 100%; padding: 12px 16px;
    background: rgba(255,255,255,0.06);
    border: 1px solid rgba(255,255,255,0.12);
    border-radius: 10px; color: white;
    font-size: 14px; font-family: inherit;
    outline: none; transition: border-color 0.2s;
  }
  .input:focus { border-color: var(--accent, #E95420); }
  .input::placeholder { color: rgba(255,255,255,0.3); }
  .input.totp {
    text-align: center; font-size: 1.4em;
    letter-spacing: 6px; font-family: 'DM Mono', monospace;
  }

  .totp-label {
    font-size: 12px; color: rgba(255,255,255,0.5);
    text-align: center;
  }

  .back-link {
    background: none; border: none; color: rgba(255,255,255,0.4);
    font-size: 12px; cursor: pointer; font-family: inherit;
  }
  .back-link:hover { color: rgba(255,255,255,0.7); }

  .error {
    color: #f87171; font-size: 12px; text-align: center;
  }

  .login-btn {
    width: 100%; padding: 12px;
    background: var(--accent, #E95420);
    border: none; border-radius: 10px;
    color: white; font-size: 14px; font-weight: 600;
    cursor: pointer; font-family: inherit;
    transition: opacity 0.15s;
  }
  .login-btn:hover { opacity: 0.9; }
  .login-btn:disabled { opacity: 0.5; cursor: not-allowed; }

  .footer {
    margin-top: 16px; font-size: 11px;
    color: rgba(255,255,255,0.2);
  }
</style>
