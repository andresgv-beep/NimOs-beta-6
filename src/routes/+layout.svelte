<script>
  import '../app.css';
</script>

<svelte:head>
  <meta name="viewport" content="width=device-width, initial-scale=1.0, maximum-scale=1.0, user-scalable=no, viewport-fit=cover" />
  <meta name="mobile-web-app-capable" content="yes" />
  <meta name="apple-mobile-web-app-capable" content="yes" />
  <meta name="apple-mobile-web-app-status-bar-style" content="black-translucent" />
  {@html `<style>
    :root {
      --accent: #E95420;
      --taskbar-height: 48px;
      --text-scale: 1;
      --glow-intensity: 0.5;
    }
    html { background: #0d0d0d; }
  </style>`}
</svelte:head>

<slot />
