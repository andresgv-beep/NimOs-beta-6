<script>
  export let tabs = [];
  export let active = '';
</script>

<div class="tab-nav">
  {#each tabs as tab}
    <!-- svelte-ignore a11y_click_events_have_key_events -->
    <!-- svelte-ignore a11y_no_static_element_interactions -->
    <div class="tab" class:active={active === tab.id} on:click={() => active = tab.id}>
      <span class="tab-text">{tab.label}</span>
      {#if active === tab.id}
        <div class="tab-line"></div>
      {/if}
    </div>
  {/each}
</div>

<style>
  .tab-nav {
    display: flex;
    gap: 24px;
    padding: 0;
  }

  .tab {
    position: relative;
    cursor: pointer;
    padding: 8px 6px 10px;
  }

  .tab-text {
    font-size: 13px;
    font-weight: 600;
    color: var(--text-3);
    transition: color 0.2s;
    font-family: 'DM Sans', sans-serif;
  }

  .tab:hover .tab-text { color: var(--text-2); }
  .tab.active .tab-text { color: var(--text-1); }

  .tab-line {
    position: absolute;
    bottom: 0;
    left: -4px;
    right: -4px;
    height: 2.5px;
    border-radius: 2px;
    background: linear-gradient(90deg, var(--accent), var(--accent2));
    opacity: 0.8;
  }
</style>
