import { writable, derived } from 'svelte/store';

const API = '/api/auth';
const TOKEN_KEY = 'nimbusos_token';

// Core state
export const appState = writable('loading'); // 'loading' | 'wizard' | 'login' | 'desktop'
export const user = writable(null);
export const token = writable('');

// Derived
export const isLoggedIn = derived(appState, $s => $s === 'desktop');
export const isAdmin = derived(user, $u => $u?.role === 'admin');

// Leer localStorage de forma segura (evita SSR y race conditions)
function readToken() {
  try { return localStorage.getItem(TOKEN_KEY) || ''; } catch { return ''; }
}

function saveToken(t) {
  token.set(t);
  try {
    if (t) localStorage.setItem(TOKEN_KEY, t);
    else localStorage.removeItem(TOKEN_KEY);
  } catch {}
}

// Get current token value synchronously
let currentToken = '';
token.subscribe(t => currentToken = t);
export function getToken() { return currentToken; }

// Initialize — check status + restore session
export async function init() {
  // Leer el token aquí, dentro de onMount, cuando localStorage ya está disponible
  const storedToken = readToken();
  if (storedToken) {
    token.set(storedToken);
  }

  try {
    const statusRes = await fetch(`${API}/status`);
    const status = await statusRes.json();

    if (!status.setup) {
      appState.set('wizard');
      return;
    }

    if (storedToken) {
      const meRes = await fetch(`${API}/me`, {
        headers: { 'Authorization': `Bearer ${storedToken}` },
      });
      const me = await meRes.json();
      if (me.user) {
        user.set(me.user);
        appState.set('desktop');
        return;
      } else {
        // Token inválido o expirado — limpiar
        saveToken('');
      }
    }

    appState.set('login');
  } catch {
    appState.set('login');
  }
}

export async function completeSetup(username, password) {
  const res = await fetch(`${API}/setup`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password }),
  });
  const data = await res.json();
  if (data.error) throw new Error(data.error);
  saveToken(data.token);
  user.set(data.user);
  appState.set('desktop');
  return data;
}

export async function login(username, password, totpCode) {
  const res = await fetch(`${API}/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ username, password, totpCode }),
  });
  const data = await res.json();
  if (data.requires2FA) return data;
  if (data.error) throw new Error(data.error);
  saveToken(data.token);
  user.set(data.user);
  appState.set('desktop');
  return data;
}

export async function logout() {
  if (currentToken) {
    try {
      await fetch(`${API}/logout`, {
        method: 'POST',
        headers: { 'Authorization': `Bearer ${currentToken}` },
      });
    } catch {}
  }
  saveToken('');
  user.set(null);
  appState.set('login');
}

export function lock() {
  appState.set('login');
}
