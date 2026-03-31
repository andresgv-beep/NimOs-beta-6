<script>
  import { login as doLogin, user } from '$lib/stores/auth.js';
  import { onMount } from 'svelte';
  import { fade, scale } from 'svelte/transition';

  let username = $user?.username || '';
  let password = '';
  let totpCode = '';
  let needs2FA = false;
  let error = '';
  let loading = false;

  // Typewriter
  let typed = '';
  const greeting = 'Hola de nuevo';
  let showAvatar = false;
  let showFields = false;
  let hasTyped = !!($user?.username);

  onMount(() => {
    if (username.trim()) {
      hasTyped = true;
      showAvatar = true;
      typed = '';
      showFields = true;
      return;
    }
    // animate greeting
    let i = 0;
    const t = setInterval(() => {
      typed = greeting.slice(0, ++i);
      if (i >= greeting.length) clearInterval(t);
    }, 55);
    // show fields
    setTimeout(() => showFields = true, 700);
    return () => clearInterval(t);
  });

  $: greetingHidden = hasTyped;
  $: avatarLetter = username ? username[0].toUpperCase() : '?';

  function onUsernameInput() {
    if (username.trim() && !hasTyped) {
      hasTyped = true;
      setTimeout(() => showAvatar = true, 80);
    } else if (!username.trim() && hasTyped) {
      hasTyped = false;
      showAvatar = false;
    }
  }

  async function handleSubmit() {
    if (!username.trim() || !password) { error = 'Introduce usuario y contraseña'; return; }
    if (needs2FA && !totpCode) { error = 'Introduce el código de 6 dígitos'; return; }

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
      error = err.message || 'Error al iniciar sesión';
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
  <div class="card">

    {#if !needs2FA}
      <div in:scale={{ duration:300, start:0.95, opacity:0 }} out:scale={{ duration:250, start:0.95, opacity:0 }}>
      <!-- ── TOP AREA ── -->
      <div class="top-area">
        <!-- Greeting -->
        <div class="greeting" class:hidden={greetingHidden}>
          {#each typed.split('') as ch, i}
            <span style="animation-delay:{i * 55}ms">{ch === ' ' ? '\u00a0' : ch}</span>
          {/each}
        </div>
        <!-- Avatar -->
        <div class="avatar" class:show={showAvatar}>{avatarLetter}</div>
        <!-- Sub -->
        <div class="login-sub" class:show={showAvatar}>Introduce tus credenciales</div>
      </div>

      <!-- ── FIELDS ── -->
      <div class="fields" class:show={showFields}>
        <input
          class="inp"
          type="text"
          placeholder="Usuario"
          bind:value={username}
          on:input={onUsernameInput}
          on:keydown={onKey}
        />
        <input
          class="inp"
          type="password"
          placeholder="Contraseña"
          bind:value={password}
          on:keydown={onKey}
        />
      </div>

      {#if error}
        <div class="error">{error}</div>
      {/if}

      <button class="login-btn" on:click={handleSubmit} disabled={loading}>
        {loading ? 'Iniciando...' : 'Iniciar sesión'}
      </button>

      </div>
    {:else}
      <!-- ── 2FA VIEW ── -->
      <div class="tfa-wrap" in:scale={{ duration:300, start:0.95, opacity:0 }} out:scale={{ duration:250, start:0.95, opacity:0 }}>
        <div class="shield">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round" stroke-linejoin="round">
            <path d="M12 22s8-4 8-10V5l-8-3-8 3v7c0 6 8 10 8 10z"/>
          </svg>
        </div>
        <div class="tfa-title">Verificación 2FA</div>
        <div class="tfa-sub">Introduce el código de 6 dígitos<br>de tu app de autenticación</div>

        <input
          class="inp totp"
          type="text"
          placeholder="000000"
          maxlength="6"
          bind:value={totpCode}
          on:keydown={onKey}
          on:input={() => { totpCode = totpCode.replace(/\D/g, ''); if (error) error = ''; }}
        />

        {#if error}
          <div class="error">{error}</div>
        {/if}

        <button class="login-btn tfa-btn" on:click={handleSubmit} disabled={loading}>
          {loading ? 'Verificando...' : 'Verificar'}
        </button>
        <button class="back-link" on:click={() => { needs2FA = false; totpCode = ''; error = ''; }}>
          ← Volver al inicio
        </button>
      </div>
    {/if}

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

  .card {
    width: 340px;
    min-height: 380px;
    background: rgba(255,255,255,0.05);
    backdrop-filter: blur(28px) saturate(1.5);
    -webkit-backdrop-filter: blur(28px) saturate(1.5);
    border: 1px solid rgba(255,255,255,0.09);
    border-radius: 22px;
    padding: 36px 28px 28px;
    display: flex; flex-direction: column; align-items: center;
    position: relative; overflow: hidden;
  }

  /* ── TOP AREA ── */
  .top-area {
    width: 100%; display: flex; flex-direction: column; align-items: center;
    min-height: 96px; margin-bottom: 20px; position: relative;
  }

  .greeting {
    font-size: 24px; font-weight: 700; color: rgba(255,255,255,0.92);
    letter-spacing: -0.3px;
    display: flex; flex-wrap: wrap; justify-content: center;
    position: absolute; top: 0;
    transition: opacity .3s ease, transform .3s ease;
  }
  .greeting.hidden { opacity: 0; transform: scale(0.9) translateY(-6px); pointer-events: none; }
  .greeting span {
    display: inline-block; opacity: 0; filter: blur(8px);
    animation: fadeIn .5s ease forwards;
  }
  @keyframes fadeIn { to { opacity: 1; filter: blur(0); } }

  .avatar {
    width: 68px; height: 68px; border-radius: 50%;
    background: linear-gradient(135deg, #e95420, #c040d0);
    display: flex; align-items: center; justify-content: center;
    font-size: 26px; font-weight: 700; color: #fff;
    opacity: 0; transform: scale(0.5);
    transition: opacity .35s cubic-bezier(0.16,1,0.3,1), transform .35s cubic-bezier(0.16,1,0.3,1);
    position: absolute; top: 14px;
  }
  .avatar.show { opacity: 1; transform: scale(1); }

  .login-sub {
    font-size: 12px; color: rgba(255,255,255,0.28);
    position: absolute; top: 74px;
    opacity: 0; transition: opacity .3s ease .1s;
    white-space: nowrap;
  }
  .login-sub.show { opacity: 1; }

  /* ── FIELDS ── */
  .fields {
    width: 100%; display: flex; flex-direction: column; gap: 10px;
    margin-bottom: 14px;
    opacity: 0; transform: translateY(10px);
    transition: opacity .4s ease, transform .4s ease;
  }
  .fields.show { opacity: 1; transform: translateY(0); }

  .inp {
    width: 100%; padding: 11px 14px;
    background: rgba(255,255,255,0.07);
    border: 1px solid rgba(255,255,255,0.10);
    border-radius: 10px; color: rgba(255,255,255,0.9);
    font-size: 13px; font-family: inherit; outline: none;
    transition: border-color .2s, background .2s;
  }
  .inp:focus { border-color: #e95420; background: rgba(255,255,255,0.10); }
  .inp::placeholder { color: rgba(255,255,255,0.22); }
  .inp.totp {
    text-align: center; font-size: 26px; letter-spacing: 10px;
    font-family: 'DM Mono', monospace; font-weight: 500;
    margin-bottom: 4px;
  }
  .inp.totp:focus { border-color: #7c6fff; background: rgba(124,111,255,0.08); }

  /* ── 2FA ── */
  .tfa-wrap {
    width: 100%; display: flex; flex-direction: column; align-items: center; gap: 0;
    animation: fadeIn .4s ease;
  }
  .shield {
    width: 72px; height: 72px; border-radius: 50%;
    background: rgba(124,111,255,0.12);
    border: 1px solid rgba(124,111,255,0.25);
    display: flex; align-items: center; justify-content: center;
    margin-bottom: 16px;
    animation: fadeIn .5s ease .05s both;
  }
  .shield svg { width: 30px; height: 30px; stroke: #7c6fff; }
  .tfa-title {
    font-size: 20px; font-weight: 700; color: rgba(255,255,255,0.92);
    margin-bottom: 6px;
    animation: fadeIn .4s ease .15s both;
  }
  .tfa-sub {
    font-size: 12px; color: rgba(255,255,255,0.28);
    text-align: center; line-height: 1.5; margin-bottom: 20px;
    animation: fadeIn .4s ease .2s both;
  }
  .tfa-wrap .inp { animation: fadeIn .4s ease .3s both; width: 100%; margin-bottom: 12px; }
  .tfa-btn { background: #7c6fff !important; animation: fadeIn .4s ease .4s both; }

  /* ── SHARED ── */
  .login-btn {
    width: 100%; padding: 12px;
    background: #e95420; border: none; border-radius: 10px;
    color: #fff; font-size: 14px; font-weight: 600;
    cursor: pointer; font-family: inherit;
    transition: opacity .15s; margin-bottom: 4px;
  }
  .login-btn:hover { opacity: .88; }
  .login-btn:disabled { opacity: .5; cursor: not-allowed; }

  .error { color: #f87171; font-size: 12px; text-align: center; margin-bottom: 8px; }

  .back-link {
    background: none; border: none; color: rgba(255,255,255,0.3);
    font-size: 12px; cursor: pointer; font-family: inherit;
    transition: color .15s; margin-top: 8px;
    animation: fadeIn .4s ease .5s both;
  }
  .back-link:hover { color: rgba(255,255,255,0.7); }

  .footer {
    margin-top: 18px; font-size: 10px;
    color: rgba(255,255,255,0.12); letter-spacing: .1em;
  }
</style>
