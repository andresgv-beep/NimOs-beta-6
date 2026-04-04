<script>
  import { windowList } from '$lib/stores/windows.js';
  import { prefs } from '$lib/stores/theme.js';
  import { logout } from '$lib/stores/auth.js';
  import Taskbar from './Taskbar.svelte';
  import WindowFrame from './WindowFrame.svelte';
  import WidgetLayer from './WidgetLayer.svelte';
  import BubbleContainer from '$lib/components/BubbleContainer.svelte';
  import { loadNotifications, notifications } from '$lib/stores/notifications.js';
  import { onMount } from 'svelte';

  onMount(() => {
    loadNotifications();

    // Check for critical SMART alerts on login
    checkSmartOnLogin();

    // Poll for new notifications every 30s so SMART warnings show in real time
    const interval = setInterval(pollNotifications, 30000);
    return () => clearInterval(interval);
  });

  let lastKnownCount = 0;

  async function pollNotifications() {
    const prevIds = new Set($notifications.map(n => n.id));
    await loadNotifications();
    // Find genuinely new notifications
    const newOnes = $notifications.filter(n => !prevIds.has(n.id) && !n.read);
    if (newOnes.length > 0) {
      notifications.update(all => all.map(n => {
        if (newOnes.some(ne => ne.id === n.id)) {
          return { ...n, showBubble: true };
        }
        return n;
      }));
    }
  }

  async function checkSmartOnLogin() {
    try {
      const token = localStorage.getItem('nimbusos_token') || '';
      const r = await fetch('/api/disks/smart/summary', {
        headers: { 'Authorization': `Bearer ${token}` }
      });
      const d = await r.json();
      if (d.worstStatus === 'critical' || d.worstStatus === 'warning') {
        // Show a persistent bubble on login
        const badDisks = (d.disks || []).filter(dk => dk.status !== 'ok');
        const names = badDisks.map(dk => dk.name).join(', ');
        const isCritical = d.worstStatus === 'critical';
        notifications.update(n => [{
          id: 'smart-login-' + Date.now(),
          type: isCritical ? 'error' : 'warning',
          category: 'system',
          title: isCritical ? 'Disco en riesgo de fallo' : 'Disco requiere atención',
          message: `SMART detecta problemas en: ${names}. Revisa Almacenamiento → Salud.`,
          timestamp: new Date().toISOString(),
          read: false,
          showBubble: true,
        }, ...n]);
      }
    } catch {}
  }
</script>

<div class="desktop" style={$prefs.wallpaper ? `background-image:url('${$prefs.wallpaper}');background-size:cover;background-position:center` : ''}>
  <!-- Widgets (below windows) -->
  <WidgetLayer />

  <!-- Windows -->
  {#each $windowList as win (win.id)}
    {#if !win.minimized}
      <WindowFrame {win} />
    {/if}
  {/each}

  <!-- Taskbar -->
  <Taskbar />

  <!-- Notifications -->
  <BubbleContainer />
</div>

<style>
  .desktop {
    position: fixed; inset: 0;
    background: var(--wallpaper);
    background-size: cover;
    background-position: center;
    overflow: hidden;
  }
</style>
