<script>
  import { onMount, onDestroy } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });
  const token = getToken();

  let shares = [];
  let openFiles = [];
  let activeIdx = -1;
  let sidebarOpen = true;
  let showMenu = false;
  let activeLine = 1;
  let saving = false;

  // Modal explorador
  let showExplorer = false;
  let explorerShare = '';
  let explorerPath = '/';
  let explorerFiles = [];
  let explorerLoading = false;
  let modalWrapper;

  // Move modal to body to escape WindowFrame overflow:hidden + border-radius stacking context
  function portalAction(node) {
    document.body.appendChild(node);
    return { destroy() { if (node.parentNode) node.parentNode.removeChild(node); } };
  }

  // Árbol proyecto
  let projectShare = null;
  let projectTree = [];

  const LANGS = { py:'Python', js:'JavaScript', ts:'TypeScript', svelte:'Svelte', go:'Go', c:'C', cpp:'C++', rs:'Rust', sh:'Shell', md:'Markdown', json:'JSON', yaml:'YAML', yml:'YAML', html:'HTML', css:'CSS', txt:'Texto' };
  const LANG_COLORS = { py:'#60a5fa', js:'#fbbf24', ts:'#60a5fa', go:'#4ade80', c:'#f97316', cpp:'#f97316', rs:'#f97316', svelte:'#f97316', md:'#c084fc', json:'#e879f9', sh:'#4ade80' };

  function getExt(name) { const d = name.lastIndexOf('.'); return d >= 0 ? name.slice(d+1).toLowerCase() : 'txt'; }
  function getLang(name) { return LANGS[getExt(name)] || 'Texto'; }
  function getLangColor(name) { return LANG_COLORS[getExt(name)] || 'rgba(255,255,255,0.27)'; }
  function streamUrl(share, path) { return `/api/files/download?share=${encodeURIComponent(share)}&path=${encodeURIComponent(path)}&token=${encodeURIComponent(token)}`; }

  async function loadShares() {
    try { const r = await fetch('/api/files', { headers: hdrs() }); const d = await r.json(); shares = d.shares || []; } catch {}
  }

  async function openExplorer() {
    showExplorer = true;
    explorerPath = '/';
    explorerFiles = [];
    if (shares.length > 0) { explorerShare = shares[0].name; await loadExplorerFiles(); }
  }

  async function loadExplorerFiles() {
    if (!explorerShare) return;
    explorerLoading = true;
    try { const r = await fetch(`/api/files?share=${encodeURIComponent(explorerShare)}&path=${encodeURIComponent(explorerPath)}`, { headers: hdrs() }); const d = await r.json(); explorerFiles = d.files || []; } catch {}
    explorerLoading = false;
  }

  function explorerEnter(name) {
    explorerPath = explorerPath === '/' ? '/' + name : explorerPath + '/' + name;
    loadExplorerFiles();
  }

  function explorerUp() {
    if (explorerPath === '/') return;
    const parts = explorerPath.split('/').filter(Boolean); parts.pop();
    explorerPath = parts.length ? '/' + parts.join('/') : '/';
    loadExplorerFiles();
  }

  async function explorerOpenFile(name) {
    const path = explorerPath === '/' ? '/' + name : explorerPath + '/' + name;
    await openFile(explorerShare, path, name);
    showExplorer = false;
  }

  async function openProject(share) {
    projectShare = share;
    try { const r = await fetch(`/api/files?share=${encodeURIComponent(share)}&path=/`, { headers: hdrs() }); const d = await r.json(); projectTree = d.files || []; } catch {}
  }

  async function openFile(share, path, name) {
    const existing = openFiles.findIndex(f => f.share === share && f.path === path);
    if (existing >= 0) { activeIdx = existing; return; }
    try {
      const res = await fetch(streamUrl(share, path));
      const content = await res.text();
      openFiles = [...openFiles, { name, path, share, content, modified: false }];
      activeIdx = openFiles.length - 1;
      activeLine = 1;
    } catch(e) { console.error('Error abriendo:', e); }
  }

  async function saveFile() {
    if (activeIdx < 0 || !openFiles[activeIdx].modified) return;
    saving = true;
    const f = openFiles[activeIdx];
    try {
      const blob = new Blob([f.content], { type: 'text/plain' });
      const fd = new FormData();
      fd.append('file', blob, f.name);
      fd.append('share', f.share);
      fd.append('path', f.path.split('/').slice(0,-1).join('/') || '/');
      const res = await fetch('/api/files/upload', { method:'POST', headers:{ 'Authorization': `Bearer ${token}` }, body: fd });
      if (res.ok) { openFiles[activeIdx].modified = false; openFiles = [...openFiles]; }
    } catch {}
    saving = false;
  }

  function closeFile(idx) {
    if (openFiles[idx].modified && !confirm('¿Cerrar sin guardar?')) return;
    openFiles = openFiles.filter((_, i) => i !== idx);
    if (activeIdx >= openFiles.length) activeIdx = openFiles.length - 1;
  }

  function onContentChange(e) {
    openFiles[activeIdx].content = e.target.value;
    openFiles[activeIdx].modified = true;
    openFiles = [...openFiles];
  }

  function highlight(content, name) {
    const ext = getExt(name);
    let code = content.replace(/&/g,'&amp;').replace(/</g,'&lt;').replace(/>/g,'&gt;').replace(/"/g,'&quot;');
    if (ext === 'py') {
      code = code
        .replace(/\b(import|from|def|class|return|if|elif|else|for|while|try|except|with|as|in|not|and|or|True|False|None|pass|break|continue|raise|yield|lambda|async|await)\b/g,'<span class="kw">$1</span>')
        .replace(/(&quot;[^&]*&quot;|'[^']*')/g,'<span class="str">$1</span>')
        .replace(/(#[^\n]*)/g,'<span class="cmt">$1</span>')
        .replace(/\b(\d+\.?\d*)\b/g,'<span class="num">$1</span>')
        .replace(/\b(print|len|range|type|str|int|float|list|dict|set|tuple|open|input|super)\b/g,'<span class="bi">$1</span>');
    } else if (ext === 'go') {
      code = code
        .replace(/\b(package|import|func|type|struct|interface|var|const|return|if|else|for|range|switch|case|default|go|defer|chan|map|make|new|nil|true|false|break|continue|select)\b/g,'<span class="kw">$1</span>')
        .replace(/(&quot;[^&]*&quot;)/g,'<span class="str">$1</span>')
        .replace(/(\/\/[^\n]*)/g,'<span class="cmt">$1</span>')
        .replace(/\b(int|int64|int32|string|bool|byte|float64|float32|error|any)\b/g,'<span class="tp">$1</span>')
        .replace(/\b(\d+)\b/g,'<span class="num">$1</span>');
    } else if (ext === 'js' || ext === 'ts' || ext === 'svelte') {
      code = code
        .replace(/\b(const|let|var|function|return|if|else|for|while|class|import|export|default|from|async|await|new|this|typeof|instanceof|in|of|try|catch|throw|break|continue|switch|case)\b/g,'<span class="kw">$1</span>')
        .replace(/(`[^`]*`|&quot;[^&]*&quot;|'[^']*')/g,'<span class="str">$1</span>')
        .replace(/(\/\/[^\n]*)/g,'<span class="cmt">$1</span>')
        .replace(/\b(\d+)\b/g,'<span class="num">$1</span>');
    } else if (ext === 'md') {
      code = code
        .replace(/^(#{1,6}\s.*)$/gm,'<span class="fn">$1</span>')
        .replace(/(\*\*[^*]+\*\*)/g,'<span class="str">$1</span>')
        .replace(/^(&gt;.*)$/gm,'<span class="cmt">$1</span>');
    } else if (ext === 'json') {
      code = code
        .replace(/(&quot;[^&]*&quot;)\s*:/g,'<span class="fn">$1</span>:')
        .replace(/:\s*(&quot;[^&]*&quot;)/g,': <span class="str">$1</span>')
        .replace(/\b(true|false|null)\b/g,'<span class="kw">$1</span>')
        .replace(/\b(\d+\.?\d*)\b/g,'<span class="num">$1</span>');
    }
    return code;
  }

  $: activeFile = activeIdx >= 0 ? openFiles[activeIdx] : null;
  $: lines = activeFile ? activeFile.content.split('\n') : [];
  $: highlighted = activeFile ? highlight(activeFile.content, activeFile.name).split('\n') : [];

  function onKeyDown(e) {
    if ((e.ctrlKey || e.metaKey) && e.key === 's') { e.preventDefault(); saveFile(); }
    if (e.key === 'Escape') { showExplorer = false; showMenu = false; }
  }

  onMount(() => { loadShares(); window.addEventListener('keydown', onKeyDown); });
  onDestroy(() => window.removeEventListener('keydown', onKeyDown));
</script>

<!-- svelte-ignore a11y_no_static_element_interactions -->
<div class="notes-root" on:click={() => showMenu = false}>

  <div class="sidebar" class:collapsed={!sidebarOpen}>
    <div class="sb-header">
      <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
      <span class="sb-title">Notes</span>
    </div>

    {#if openFiles.length > 0}
      <div class="sb-section">Abiertos</div>
      {#each openFiles as f, i}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="sb-item" class:active={i === activeIdx} on:click={() => { activeIdx = i; activeLine = 1; }}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="width:13px;height:13px;flex-shrink:0;opacity:{i===activeIdx?1:0.4}"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
          <span class="sb-item-name">{f.name}</span>
          {#if f.modified}<div class="sb-dot"></div>{/if}
          <span class="sb-ext" style="color:{getLangColor(f.name)}">{getExt(f.name)}</span>
        </div>
      {/each}
    {/if}

    {#if projectShare && projectTree.length > 0}
      <div class="sb-section" style="margin-top:10px">Proyecto</div>
      {#each projectTree as item}
        {#if item.isDirectory}
          <div class="sb-folder"><span style="font-size:12px">📁</span><span class="sb-folder-name">{item.name}</span></div>
        {:else}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="sb-item sb-tree-file" on:click={() => openFile(projectShare, '/' + item.name, item.name)}>
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round" style="width:12px;height:12px;flex-shrink:0;opacity:.4"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
            <span class="sb-item-name">{item.name}</span>
            <span class="sb-ext" style="color:{getLangColor(item.name)}">{getExt(item.name)}</span>
          </div>
        {/if}
      {/each}
    {/if}
  </div>

  <div class="inner-wrap">
    <div class="inner">
      <div class="inner-titlebar">
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <button class="icon-btn" on:click|stopPropagation={() => sidebarOpen = !sidebarOpen}>
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="3" y="3" width="18" height="18" rx="2"/><line x1="9" y1="3" x2="9" y2="21"/></svg>
        </button>
        {#if activeFile}
          <span class="tb-filename">{activeFile.name}</span>
          <span class="tb-badge" style="color:{getLangColor(activeFile.name)}">{getLang(activeFile.name)}</span>
          {#if activeFile.modified}<div class="tb-dot"></div>{/if}
        {:else}
          <span class="tb-filename" style="color:var(--text-3)">Sin archivo</span>
        {/if}
        <div class="tb-spacer"></div>
        <button class="icon-btn">
          <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><circle cx="11" cy="11" r="8"/><line x1="21" y1="21" x2="16.65" y2="16.65"/></svg>
        </button>
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="menu-wrap" on:click|stopPropagation>
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <button class="icon-btn" on:click={() => showMenu = !showMenu}>
            <svg viewBox="0 0 24 24" fill="currentColor"><circle cx="5" cy="12" r="1.5"/><circle cx="12" cy="12" r="1.5"/><circle cx="19" cy="12" r="1.5"/></svg>
          </button>
          {#if showMenu}
            <div class="dropdown">
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="drop-item" on:click={() => { openExplorer(); showMenu = false; }}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
                Abrir archivo
              </div>
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <div class="drop-item" on:click={() => { openExplorer(); showMenu = false; }}>
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
                Abrir proyecto
              </div>
              {#if activeFile}
                <div class="drop-sep"></div>
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <div class="drop-item" on:click={() => { window.open(streamUrl(activeFile.share, activeFile.path), '_blank'); showMenu = false; }}>
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M21 15v4a2 2 0 0 1-2 2H5a2 2 0 0 1-2-2v-4"/><polyline points="7 10 12 15 17 10"/><line x1="12" y1="15" x2="12" y2="3"/></svg>
                  Descargar
                </div>
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <div class="drop-item" on:click={() => { navigator.clipboard.writeText(activeFile.path); showMenu = false; }}>
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><rect x="9" y="9" width="13" height="13" rx="2"/><path d="M5 15H4a2 2 0 0 1-2-2V4a2 2 0 0 1 2-2h9a2 2 0 0 1 2 2v1"/></svg>
                  Copiar ruta
                </div>
                <div class="drop-sep"></div>
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <div class="drop-item danger" on:click={() => { closeFile(activeIdx); showMenu = false; }}>
                  <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
                  Cerrar archivo
                </div>
              {/if}
            </div>
          {/if}
        </div>
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <button class="icon-btn save" on:click={saveFile} disabled={!activeFile?.modified || saving}>
          {#if saving}<div class="spinner"></div>{:else}
            <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><path d="M19 21H5a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h11l5 5v11a2 2 0 0 1-2 2z"/><polyline points="17 21 17 13 7 13 7 21"/><polyline points="7 3 7 8 15 8"/></svg>
          {/if}
        </button>
      </div>

      {#if activeFile}
        <div class="editor-wrap">
          <div class="line-numbers">
            {#each lines as _, i}<div class="ln" class:active={i+1 === activeLine}>{i+1}</div>{/each}
          </div>
          <div class="code-wrap">
            <div class="code-highlight" aria-hidden="true">
              {#each highlighted as line, i}
                <div class="code-line" class:active-line={i+1 === activeLine}>{@html line || '&nbsp;'}</div>
              {/each}
            </div>
            <textarea class="code-textarea"
              value={activeFile.content}
              on:input={onContentChange}
              on:click={(e) => { const b=e.target.value.slice(0,e.target.selectionStart); activeLine=(b.match(/\n/g)||[]).length+1; }}
              on:keyup={(e) => { const b=e.target.value.slice(0,e.target.selectionStart); activeLine=(b.match(/\n/g)||[]).length+1; }}
              spellcheck="false" autocomplete="off" autocorrect="off" autocapitalize="off"
            ></textarea>
          </div>
        </div>
      {:else}
        <div class="empty-state">
          <div class="empty-icon"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.5" stroke-linecap="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg></div>
          <div class="empty-title">Sin archivo abierto</div>
          <div class="empty-desc">Usa ··· → Abrir archivo para empezar</div>
        </div>
      {/if}

      <div class="statusbar">
        <div class="status-dot"></div>
        <span>{activeFile ? getLang(activeFile.name) : 'Notes'}</span>
        <span class="st-sep">·</span><span>UTF-8</span>
        {#if activeFile}
          <span class="st-sep">·</span>
          <span style="color:var(--text-3);font-family:'DM Mono',monospace">{activeFile.share}{activeFile.path}</span>
          <div class="st-right"><span>{lines.length} líneas</span><span class="st-sep">·</span><span>Ln {activeLine}</span></div>
        {/if}
      </div>
    </div>
  </div>
</div>

{#if showExplorer}
  <div use:portalAction>
  <!-- svelte-ignore a11y_click_events_have_key_events -->
  <!-- svelte-ignore a11y_no_static_element_interactions -->
  <div style="position:fixed;inset:0;z-index:99998;background:rgba(0,0,0,0.65);backdrop-filter:blur(3px)" on:click|self={() => showExplorer = false}></div>
  <div style="position:fixed;top:50%;left:50%;transform:translate(-50%,-50%);z-index:99999;width:500px;max-width:92%;max-height:75vh;background:var(--bg-inner,#1a1a2e);border-radius:12px;border:1px solid var(--border,#333);box-shadow:0 24px 60px rgba(0,0,0,0.5);display:flex;flex-direction:column;overflow:hidden;font-family:'Inter',-apple-system,sans-serif;color:var(--text-1,#eee)">
    <div class="modal-header">
      <span class="modal-title">Abrir archivo</span>
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <div class="modal-close" on:click={() => showExplorer = false}>
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><line x1="18" y1="6" x2="6" y2="18"/><line x1="6" y1="6" x2="18" y2="18"/></svg>
      </div>
    </div>
    <div class="modal-shares">
      {#each shares as s}
        <!-- svelte-ignore a11y_click_events_have_key_events -->
        <!-- svelte-ignore a11y_no_static_element_interactions -->
        <div class="modal-share-tab" class:active={explorerShare === s.name}
          on:click={() => { explorerShare = s.name; explorerPath = '/'; loadExplorerFiles(); }}>
          {s.displayName || s.name}
        </div>
      {/each}
    </div>
    <div class="modal-breadcrumb">
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <span class="modal-bc-part" on:click={() => { explorerPath='/'; loadExplorerFiles(); }}>{explorerShare}</span>
      {#each explorerPath === '/' ? [] : explorerPath.split('/').filter(Boolean) as crumb}
        <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2.5" stroke-linecap="round" style="width:9px;height:9px;color:var(--text-3)"><polyline points="9 18 15 12 9 6"/></svg>
        <span class="modal-bc-part">{crumb}</span>
      {/each}
    </div>
    <div class="modal-files">
      {#if explorerLoading}
        <div style="display:flex;justify-content:center;padding:24px"><div class="spinner"></div></div>
      {:else}
        {#if explorerPath !== '/'}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="modal-file" on:click={explorerUp}>
            <div class="modal-file-ico"><svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round"><polyline points="15 18 9 12 15 6"/></svg></div>
            <span style="color:var(--text-3);font-size:12px">Volver</span>
          </div>
        {/if}
        {#each explorerFiles as file}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="modal-file" on:click={() => file.isDirectory ? explorerEnter(file.name) : explorerOpenFile(file.name)}>
            <div class="modal-file-ico" class:dir={file.isDirectory}>
              {#if file.isDirectory}
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M22 19a2 2 0 0 1-2 2H4a2 2 0 0 1-2-2V5a2 2 0 0 1 2-2h5l2 3h9a2 2 0 0 1 2 2z"/></svg>
              {:else}
                <svg viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.8" stroke-linecap="round"><path d="M14 2H6a2 2 0 0 0-2 2v16a2 2 0 0 0 2 2h12a2 2 0 0 0 2-2V8z"/><polyline points="14 2 14 8 20 8"/></svg>
              {/if}
            </div>
            <span class="modal-file-name">{file.name}</span>
            {#if !file.isDirectory}
              <span class="modal-file-ext" style="color:{getLangColor(file.name)}">{getExt(file.name)}</span>
            {/if}
          </div>
        {/each}
        {#if explorerFiles.length === 0}
          <div style="text-align:center;padding:24px;font-size:11px;color:var(--text-3)">Carpeta vacía</div>
        {/if}
      {/if}
    </div>
    <div class="modal-footer">
      <!-- svelte-ignore a11y_click_events_have_key_events -->
      <!-- svelte-ignore a11y_no_static_element_interactions -->
      <button class="modal-btn" on:click={() => showExplorer = false}>Cancelar</button>
    </div>
  </div>
  </div>
{/if}

<style>
  .notes-root { width:100%; height:100%; display:flex; overflow:hidden; font-family:'Inter',-apple-system,sans-serif; color:var(--text-1); }
  .sidebar { width:220px; flex-shrink:0; background:var(--bg-sidebar); display:flex; flex-direction:column; overflow:hidden; transition:width .2s cubic-bezier(0.4,0,0.2,1); border-right:1px solid var(--border); }
  .sidebar.collapsed { width:0; border-right:none; }
  .sb-header { display:flex; align-items:center; gap:8px; padding:28px 12px 14px; color:var(--text-1); white-space:nowrap; }
  .sb-title { font-size:15px; font-weight:600; }
  .sb-section { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.08em; padding:6px 12px 3px; white-space:nowrap; }
  .sb-item { display:flex; align-items:center; gap:7px; padding:5px 8px; margin:0 4px; border-radius:7px; font-size:11px; color:var(--text-2); cursor:pointer; transition:all .12s; white-space:nowrap; }
  .sb-item:hover { background:rgba(128,128,128,0.08); color:var(--text-1); }
  .sb-item.active { background:var(--active-bg); color:var(--text-1); }
  .sb-item-name { flex:1; overflow:hidden; text-overflow:ellipsis; }
  .sb-dot { width:6px; height:6px; border-radius:50%; background:var(--amber); flex-shrink:0; }
  .sb-ext { font-size:9px; padding:1px 5px; border-radius:3px; font-family:'DM Mono',monospace; flex-shrink:0; background:rgba(255,255,255,0.06); }
  .sb-tree-file { padding:3px 8px; }
  .sb-folder { display:flex; align-items:center; gap:6px; padding:5px 8px; margin:0 4px; font-size:11px; color:var(--text-2); white-space:nowrap; }
  .sb-folder-name { font-size:11px; }
  .inner-wrap { flex:1; padding:8px; display:flex; min-width:0; }
  .inner { flex:1; border-radius:10px; border:1px solid var(--border); background:var(--bg-inner); display:flex; flex-direction:column; overflow:hidden; min-width:0; }
  .inner-titlebar { display:flex; align-items:center; gap:6px; padding:9px 12px; background:var(--bg-bar); flex-shrink:0; border-bottom:1px solid var(--border); }
  .tb-filename { font-size:13px; font-weight:600; color:var(--text-1); }
  .tb-badge { font-size:10px; font-weight:700; font-family:'DM Mono',monospace; padding:2px 7px; border-radius:4px; background:rgba(255,255,255,0.06); border:1px solid rgba(255,255,255,0.08); }
  .tb-dot { width:7px; height:7px; border-radius:50%; background:var(--amber); flex-shrink:0; }
  .tb-spacer { flex:1; }
  .icon-btn { width:30px; height:30px; border-radius:7px; border:1px solid var(--border); background:transparent; color:var(--text-3); cursor:pointer; display:flex; align-items:center; justify-content:center; transition:all .15s; flex-shrink:0; }
  .icon-btn svg { width:13px; height:13px; }
  .icon-btn:hover { color:var(--text-1); border-color:var(--border-hi); background:var(--ibtn-bg); }
  .icon-btn.save { background:linear-gradient(135deg,var(--accent),var(--accent2)); border:none; color:#fff; }
  .icon-btn.save:hover { opacity:.88; }
  .icon-btn.save:disabled { opacity:.35; cursor:not-allowed; }
  .menu-wrap { position:relative; }
  .dropdown { position:absolute; top:36px; right:0; background:var(--bg-bar); border:1px solid var(--border); border-radius:9px; padding:4px; min-width:170px; box-shadow:0 8px 32px rgba(0,0,0,0.5); z-index:100; animation:menuIn .12s ease both; }
  @keyframes menuIn { from{opacity:0;transform:scale(0.96) translateY(-4px)} to{opacity:1;transform:scale(1) translateY(0)} }
  .drop-item { display:flex; align-items:center; gap:9px; padding:7px 10px; border-radius:6px; font-size:12px; color:var(--text-2); cursor:pointer; transition:all .1s; }
  .drop-item svg { width:13px; height:13px; flex-shrink:0; opacity:.6; }
  .drop-item:hover { background:var(--active-bg); color:var(--text-1); }
  .drop-item.danger { color:var(--red); }
  .drop-item.danger:hover { background:rgba(248,113,113,0.10); }
  .drop-sep { height:1px; background:var(--border); margin:3px 4px; }
  .editor-wrap { flex:1; overflow:auto; display:flex; min-height:0; }
  .editor-wrap::-webkit-scrollbar { width:6px; height:6px; }
  .editor-wrap::-webkit-scrollbar-thumb { background:rgba(255,255,255,0.10); border-radius:3px; }
  .line-numbers { padding:14px 0; background:rgba(0,0,0,0.12); border-right:1px solid var(--border); flex-shrink:0; user-select:none; min-width:52px; text-align:right; }
  .ln { padding:0 14px 0 8px; font-size:12.5px; line-height:1.75; font-family:'DM Mono',monospace; color:var(--text-3); }
  .ln.active { color:var(--accent); }
  .code-wrap { flex:1; position:relative; }
  .code-highlight { padding:14px 20px; pointer-events:none; }
  .code-line { font-size:12.5px; line-height:1.75; font-family:'DM Mono',monospace; color:var(--text-1); white-space:pre; min-height:1.75em; border-radius:3px; margin:0 -4px; padding:0 4px; }
  .code-line.active-line { background:rgba(124,111,255,0.07); }
  .code-textarea { position:absolute; inset:0; padding:14px 20px; font-size:12.5px; line-height:1.75; font-family:'DM Mono',monospace; color:transparent; background:transparent; border:none; outline:none; resize:none; caret-color:var(--accent); white-space:pre; width:100%; height:100%; }
  :global(.kw)  { color:#c084fc; font-style:italic; }
  :global(.str) { color:#4ade80; }
  :global(.cmt) { color:rgba(255,255,255,0.3); font-style:italic; }
  :global(.num) { color:#fbbf24; }
  :global(.fn)  { color:#60a5fa; }
  :global(.tp)  { color:#f97316; }
  :global(.bi)  { color:#e879f9; }
  .empty-state { flex:1; display:flex; flex-direction:column; align-items:center; justify-content:center; gap:12px; }
  .empty-icon { width:56px; height:56px; border-radius:14px; background:rgba(124,111,255,0.08); border:1px solid rgba(124,111,255,0.12); display:flex; align-items:center; justify-content:center; }
  .empty-icon svg { width:26px; height:26px; color:var(--accent); opacity:.4; }
  .empty-title { font-size:13px; font-weight:600; color:var(--text-2); }
  .empty-desc { font-size:11px; color:var(--text-3); }
  .statusbar { display:flex; align-items:center; gap:8px; padding:6px 14px; border-top:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; font-size:10px; color:var(--text-3); border-radius:0 0 10px 10px; font-family:'DM Mono',monospace; }
  .status-dot { width:6px; height:6px; border-radius:50%; background:var(--green); box-shadow:0 0 4px rgba(74,222,128,0.6); }
  .st-sep { color:var(--border); }
  .st-right { margin-left:auto; display:flex; gap:8px; }
  .spinner { width:12px; height:12px; border-radius:50%; border:2px solid rgba(255,255,255,0.3); border-top-color:#fff; animation:spin .7s linear infinite; }
  @keyframes spin { to{transform:rotate(360deg)} }
  .modal-overlay { position:fixed; inset:0; z-index:9998; background:rgba(0,0,0,0.65); backdrop-filter:blur(3px); }
  .modal { position:fixed; top:50%; left:50%; transform:translate(-50%,-50%); z-index:9999; width:500px; max-width:92%; max-height:75vh; background:var(--bg-inner); border-radius:12px; border:1px solid var(--border); box-shadow:0 24px 60px rgba(0,0,0,0.5); display:flex; flex-direction:column; overflow:hidden; animation:menuIn .18s cubic-bezier(0.16,1,0.3,1) both; }
  .modal-header { display:flex; align-items:center; justify-content:space-between; padding:14px 18px; border-bottom:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; }
  .modal-title { font-size:13px; font-weight:600; color:var(--text-1); }
  .modal-close { width:24px; height:24px; border-radius:6px; cursor:pointer; display:flex; align-items:center; justify-content:center; color:var(--text-3); background:var(--ibtn-bg); transition:all .15s; }
  .modal-close svg { width:12px; height:12px; }
  .modal-close:hover { color:var(--text-1); }
  .modal-shares { display:flex; padding:0 14px; border-bottom:1px solid var(--border); flex-shrink:0; }
  .modal-share-tab { padding:9px 10px; font-size:11px; color:var(--text-3); cursor:pointer; border-bottom:2px solid transparent; margin-bottom:-1px; transition:all .15s; }
  .modal-share-tab:hover { color:var(--text-2); }
  .modal-share-tab.active { color:var(--accent); border-bottom-color:var(--accent); }
  .modal-breadcrumb { display:flex; align-items:center; gap:4px; padding:8px 16px; border-bottom:1px solid var(--border); flex-shrink:0; }
  .modal-bc-part { font-size:10px; color:var(--text-3); cursor:pointer; font-family:'DM Mono',monospace; }
  .modal-bc-part:hover { color:var(--text-2); }
  .modal-files { flex:1; overflow-y:auto; padding:6px 10px; }
  .modal-files::-webkit-scrollbar { width:3px; }
  .modal-files::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }
  .modal-file { display:flex; align-items:center; gap:10px; padding:7px 8px; border-radius:7px; font-size:12px; color:var(--text-2); cursor:pointer; transition:all .1s; }
  .modal-file:hover { background:var(--active-bg); color:var(--text-1); }
  .modal-file-ico { width:26px; height:26px; border-radius:6px; flex-shrink:0; display:flex; align-items:center; justify-content:center; background:rgba(255,255,255,0.05); }
  .modal-file-ico svg { width:13px; height:13px; color:var(--text-3); }
  .modal-file-ico.dir { background:rgba(251,191,36,0.10); }
  .modal-file-ico.dir svg { color:var(--amber); }
  .modal-file-name { flex:1; overflow:hidden; text-overflow:ellipsis; white-space:nowrap; }
  .modal-file-ext { font-size:9px; padding:1px 5px; border-radius:3px; background:rgba(255,255,255,0.06); font-family:'DM Mono',monospace; flex-shrink:0; }
  .modal-footer { display:flex; justify-content:flex-end; padding:10px 16px; border-top:1px solid var(--border); background:var(--bg-bar); flex-shrink:0; }
  .modal-btn { padding:6px 14px; border-radius:7px; border:1px solid var(--border); background:var(--ibtn-bg); color:var(--text-2); font-size:11px; cursor:pointer; font-family:inherit; transition:all .15s; }
  .modal-btn:hover { color:var(--text-1); border-color:var(--border-hi); }
</style>
