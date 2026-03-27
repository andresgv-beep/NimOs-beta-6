<script>
  import '../app.css';
</script>

<svelte:head>
  <!-- Aplicar tema base antes del primer render para evitar flash -->
  <!-- Se sobreescribe en milisegundos cuando loadPrefs aplica el tema real -->
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
