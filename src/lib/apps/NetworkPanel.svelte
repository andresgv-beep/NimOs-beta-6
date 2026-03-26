<script>
  import { onMount } from 'svelte';
  import { getToken } from '$lib/stores/auth.js';

  export let activeTab = 'interfaces';
  export let activeSub = 'interfaces';

  const hdrs = () => ({ 'Authorization': `Bearer ${getToken()}` });

  // ── Data ──
  let netIfaces   = [];
  let dnsData     = {};
  let smbData     = {};
  let sshData     = {};
  let ftpData     = {};
  let nfsData     = {};
  let webdavData  = {};
  let firewallData = { ports: [], rules: [] };
  let ddnsData    = {};
  let proxyData   = { rules: [] };
  let certData    = {};
  let certEmail   = '';
  let certRequesting = false;
  let certMsg     = '';
  let certMsgError = false;
  let httpsPort   = 5009;
  let httpsSaving = false;
  let loading     = false;

  // Sync HTTPS port from loaded data
  $: if (certData.config?.https?.port) httpsPort = certData.config.https.port;
  $: httpsEnabled = certData.https?.running || false;
  $: httpsConfig = certData.config?.https || {};
  $: sslValid = certData.ssl?.valid || false;
  $: certDomain = ddnsData.config?.domain || certData.config?.ddns?.domain || '';
  $: localIp = certData.ddns?.externalIp || ddnsData.externalIp || '';

  async function toggleHttps(enable) {
    httpsSaving = true;
    try {
      await fetch('/api/remote-access/enable-https', {
        method: 'POST', headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ domain: certDomain, port: httpsPort, enabled: enable }),
      });
      loadTab('remoteaccess');
    } catch (e) { console.error('HTTPS toggle failed', e); }
    httpsSaving = false;
  }

  // ── DDNS form ──
  let ddnsForm = { provider: '', domain: '', token: '', username: '', password: '' };
  let ddnsSaving = false;
  let ddnsTesting = false;
  let ddnsMsg = '';
  let ddnsMsgError = false;
  let ddnsEditing = false;

  // Sync form from loaded data
  $: if (ddnsData.config) {
    if (!ddnsForm.provider && ddnsData.config.provider) ddnsForm.provider = ddnsData.config.provider;
    if (!ddnsForm.domain && ddnsData.config.domain) ddnsForm.domain = ddnsData.config.domain;
    if (!ddnsForm.token && ddnsData.config.token) ddnsForm.token = ddnsData.config.token;
  }

  // Show form if no config yet
  $: if (!ddnsData.config?.enabled) ddnsEditing = false;

  async function saveDdns() {
    const p = ddnsForm.provider;
    if (!p) { ddnsMsg = 'Selecciona un proveedor'; ddnsMsgError = true; return; }
    if (p === 'freedns' && !ddnsForm.token) { ddnsMsg = 'Introduce el update key'; ddnsMsgError = true; return; }
    if (p !== 'freedns' && !ddnsForm.domain) { ddnsMsg = 'Introduce el dominio'; ddnsMsgError = true; return; }
    if (p === 'noip' && (!ddnsForm.username || !ddnsForm.password)) { ddnsMsg = 'Introduce email y contraseña'; ddnsMsgError = true; return; }
    if ((p === 'duckdns' || p === 'dynu') && !ddnsForm.token) { ddnsMsg = 'Introduce el token'; ddnsMsgError = true; return; }
    ddnsSaving = true; ddnsMsg = '';
    try {
      const res = await fetch('/api/ddns/config', {
        method: 'POST', headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...ddnsForm, enabled: true }),
      });
      const data = await res.json();
      if (data.ok) {
        ddnsMsg = ''; ddnsMsgError = false; ddnsEditing = false;
        loadTab('remoteaccess');
      } else { ddnsMsg = data.error || 'Error'; ddnsMsgError = true; }
    } catch (e) { ddnsMsg = 'Error de conexión'; ddnsMsgError = true; }
    ddnsSaving = false;
  }

  async function testDdns() {
    ddnsTesting = true; ddnsMsg = '';
    try {
      const res = await fetch('/api/ddns/test', {
        method: 'POST', headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify(ddnsForm),
      });
      const data = await res.json();
      if (data.ok) { ddnsMsg = 'Conexión exitosa: ' + (data.result || 'OK'); ddnsMsgError = false; }
      else { ddnsMsg = data.error || 'Falló la prueba'; ddnsMsgError = true; }
    } catch (e) { ddnsMsg = 'Error de conexión'; ddnsMsgError = true; }
    ddnsTesting = false;
  }

  async function disableDdns() {
    try {
      await fetch('/api/ddns/config', {
        method: 'POST', headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ enabled: false }),
      });
      ddnsMsg = 'DDNS desactivado'; ddnsMsgError = false;
      loadTab('remoteaccess');
    } catch (e) { ddnsMsg = 'Error'; ddnsMsgError = true; }
  }

  async function toggleAutoUpdate() {
    const current = ddnsData.config?.autoUpdate !== false;
    try {
      await fetch('/api/ddns/config', {
        method: 'POST', headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({ ...ddnsData.config, autoUpdate: !current }),
      });
      loadTab('remoteaccess');
    } catch (e) { console.error('Toggle auto-update failed', e); }
  }

  async function requestCert() {
    const domain = ddnsData.config?.domain || certData.config?.ddns?.domain || '';
    if (!domain) { certMsg = 'Configura un dominio DDNS primero'; certMsgError = true; return; }
    if (!certEmail) { certMsg = 'Introduce un email para Let\'s Encrypt'; certMsgError = true; return; }
    certRequesting = true; certMsg = '';
    try {
      const provider = ddnsData.config?.provider || '';
      const dnsToken = ddnsData.config?.token || '';
      const useDns = provider === 'duckdns' && dnsToken;

      const res = await fetch('/api/remote-access/request-ssl', {
        method: 'POST', headers: { ...hdrs(), 'Content-Type': 'application/json' },
        body: JSON.stringify({
          domain,
          email: certEmail,
          method: useDns ? 'dns' : 'standalone',
          provider: useDns ? 'duckdns' : '',
          dnsToken: useDns ? dnsToken : '',
        }),
      });
      const data = await res.json();
      if (data.ok) { certMsg = 'Certificado obtenido correctamente'; certMsgError = false; loadTab('remoteaccess'); }
      else { certMsg = data.error || 'Error al solicitar certificado'; certMsgError = true; }
    } catch (e) { certMsg = 'Error de conexión'; certMsgError = true; }
    certRequesting = false;
  }

  async function loadTab(tab) {
    loading = true;
    try {
      if (tab === 'interfaces') {
        const [ni, dns] = await Promise.all([
          fetch('/api/network',     { headers: hdrs() }).then(r => r.json()),
          fetch('/api/dns/status',  { headers: hdrs() }).then(r => r.json()),
        ]);
        netIfaces = Array.isArray(ni) ? ni : [];
        dnsData   = dns || {};
      } else if (tab === 'services') {
        const [smb, ssh, ftp, nfs, wdav] = await Promise.all([
          fetch('/api/smb/status',    { headers: hdrs() }).then(r => r.json()),
          fetch('/api/ssh/status',    { headers: hdrs() }).then(r => r.json()),
          fetch('/api/ftp/status',    { headers: hdrs() }).then(r => r.json()),
          fetch('/api/nfs/status',    { headers: hdrs() }).then(r => r.json()),
          fetch('/api/webdav/status', { headers: hdrs() }).then(r => r.json()),
        ]);
        smbData    = smb    || {};
        sshData    = ssh    || {};
        ftpData    = ftp    || {};
        nfsData    = nfs    || {};
        webdavData = wdav   || {};
      } else if (tab === 'remoteaccess') {
        const [ddns, proxy, certs] = await Promise.all([
          fetch('/api/ddns/status',   { headers: hdrs() }).then(r => r.json()),
          fetch('/api/proxy/status',  { headers: hdrs() }).then(r => r.json()),
          fetch('/api/remote-access/status', { headers: hdrs() }).then(r => r.json()).catch(() => ({})),
        ]);
        ddnsData  = ddns  || {};
        proxyData = proxy || { rules: [] };
        certData  = certs || {};
      } else if (tab === 'security') {
        const fw = await fetch('/api/firewall', { headers: hdrs() }).then(r => r.json());
        firewallData = fw || { ports: [], rules: [] };
      }
    } catch(e) { console.error('[Network] load failed', e); }
    loading = false;
  }

  $: loadTab(activeTab);

  function statusColor(running) { return running ? 'var(--green)' : 'var(--text-3)'; }
  function statusLabel(running) { return running ? 'Activo' : 'Inactivo'; }
</script>

<div class="net-root">
  <div class="net-content">
    {#if loading}
      <div class="n-loading"><div class="spinner"></div></div>

    {:else}

      <!-- Sub-tabs row -->
      {#if activeTab === 'interfaces'}
        <div class="sub-tabs">
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="sub-tab" class:active={activeSub === 'interfaces'} on:click={() => activeSub = 'interfaces'}>Network Interfaces</div>
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="sub-tab" class:active={activeSub === 'dns'} on:click={() => activeSub = 'dns'}>DNS</div>
        </div>
      {:else if activeTab === 'services'}
        <div class="sub-tabs">
          {#each [['smb','SMB / CIFS'],['ssh','SSH'],['ftp','FTP / SFTP'],['nfs','NFS'],['webdav','WebDAV']] as [id, label]}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="sub-tab" class:active={activeSub === id} on:click={() => activeSub = id}>{label}</div>
          {/each}
        </div>
      {:else if activeTab === 'remoteaccess'}
        <div class="sub-tabs">
          {#each [['ports','Port Exposure'],['ddns','DDNS'],['proxy','Reverse Proxy'],['certs','Certificates']] as [id, label]}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="sub-tab" class:active={activeSub === id} on:click={() => activeSub = id}>{label}</div>
          {/each}
        </div>
      {:else if activeTab === 'security'}
        <div class="sub-tabs">
          {#each [['firewall','Firewall'],['fail2ban','Fail2ban']] as [id, label]}
            <!-- svelte-ignore a11y_click_events_have_key_events -->
            <!-- svelte-ignore a11y_no_static_element_interactions -->
            <div class="sub-tab" class:active={activeSub === id} on:click={() => activeSub = id}>{label}</div>
          {/each}
        </div>
      {/if}

      <!-- ══ Content ══ -->

      {#if activeSub === 'interfaces'}
        <div class="section-label">Interfaces de red</div>
        {#if netIfaces.length === 0}
          <p class="empty-msg">No se detectaron interfaces</p>
        {:else}
          <div class="iface-list">
            {#each netIfaces as iface}
              <div class="iface-card">
                <div class="iface-header">
                  <div class="iface-led" style="background:{iface.up ? 'var(--green)' : 'var(--text-3)'}; box-shadow:{iface.up ? '0 0 5px rgba(74,222,128,0.6)' : 'none'}"></div>
                  <div class="iface-name">{iface.name || iface.interface}</div>
                  <div class="iface-type">{iface.type || (iface.name?.startsWith('w') ? 'WiFi' : 'Ethernet')}</div>
                  <div class="iface-status" style="color:{statusColor(iface.up)}">{iface.up ? 'UP' : 'DOWN'}</div>
                </div>
                <div class="iface-body">
                  <div class="iface-row"><span>IP</span><span>{iface.ip || iface.address || '—'}</span></div>
                  {#if iface.ip6 || iface.ipv6}
                    <div class="iface-row"><span>IPv6</span><span>{iface.ip6 || iface.ipv6}</span></div>
                  {/if}
                  <div class="iface-row"><span>MAC</span><span>{iface.mac || iface.hwaddr || '—'}</span></div>
                  {#if iface.speed}
                    <div class="iface-row"><span>Velocidad</span><span>{iface.speed}</span></div>
                  {/if}
                </div>
              </div>
            {/each}
          </div>
        {/if}

      {:else if activeSub === 'dns'}
        <div class="section-label">DNS y Hostname</div>
        <div class="field-group">
          <div class="field-row"><span class="field-label">Hostname</span><span class="field-value">{dnsData.hostname || '—'}</span></div>
          <div class="field-row"><span class="field-label">DNS primario</span><span class="field-value">{dnsData.nameservers?.[0] || dnsData.dns1 || '—'}</span></div>
          <div class="field-row"><span class="field-label">DNS secundario</span><span class="field-value">{dnsData.nameservers?.[1] || dnsData.dns2 || '—'}</span></div>
          {#if dnsData.domain}
            <div class="field-row"><span class="field-label">Dominio</span><span class="field-value">{dnsData.domain}</span></div>
          {/if}
        </div>

      {:else if activeSub === 'smb'}
        <div class="service-header">
          <div class="section-label">SMB / CIFS</div>
          <div class="svc-status" style="color:{statusColor(smbData.running)}">
            <div class="svc-dot" style="background:{statusColor(smbData.running)}"></div>
            {statusLabel(smbData.running)}
          </div>
        </div>
        <div class="field-group">
          {#if smbData.workgroup}
            <div class="field-row"><span class="field-label">Workgroup</span><span class="field-value">{smbData.workgroup}</span></div>
          {/if}
          {#if smbData.serverString}
            <div class="field-row"><span class="field-label">Server string</span><span class="field-value">{smbData.serverString}</span></div>
          {/if}
          {#if smbData.shares?.length}
            <div class="section-label" style="margin-top:14px">Shares activos</div>
            {#each smbData.shares as share}
              <div class="share-row">
                <span class="share-name">{share.name}</span>
                <span class="share-path">{share.path}</span>
              </div>
            {/each}
          {/if}
        </div>

      {:else if activeSub === 'ssh'}
        <div class="service-header">
          <div class="section-label">SSH</div>
          <div class="svc-status" style="color:{statusColor(sshData.running)}">
            <div class="svc-dot" style="background:{statusColor(sshData.running)}"></div>
            {statusLabel(sshData.running)}
          </div>
        </div>
        <div class="field-group">
          <div class="field-row"><span class="field-label">Puerto</span><span class="field-value">{sshData.port || 22}</span></div>
          <div class="field-row"><span class="field-label">Auth por clave</span><span class="field-value">{sshData.keyAuth ? 'Sí' : 'No'}</span></div>
          <div class="field-row"><span class="field-label">Root login</span><span class="field-value">{sshData.rootLogin ? 'Permitido' : 'Denegado'}</span></div>
        </div>

      {:else if activeSub === 'ftp'}
        <div class="service-header">
          <div class="section-label">FTP / SFTP</div>
          <div class="svc-status" style="color:{statusColor(ftpData.running)}">
            <div class="svc-dot" style="background:{statusColor(ftpData.running)}"></div>
            {statusLabel(ftpData.running)}
          </div>
        </div>
        <div class="field-group">
          <div class="field-row"><span class="field-label">Puerto FTP</span><span class="field-value">{ftpData.port || 21}</span></div>
          <div class="field-row"><span class="field-label">Modo pasivo</span><span class="field-value">{ftpData.passive ? 'Sí' : 'No'}</span></div>
        </div>

      {:else if activeSub === 'nfs'}
        <div class="service-header">
          <div class="section-label">NFS</div>
          <div class="svc-status" style="color:{statusColor(nfsData.running)}">
            <div class="svc-dot" style="background:{statusColor(nfsData.running)}"></div>
            {statusLabel(nfsData.running)}
          </div>
        </div>
        <div class="field-group">
          {#if nfsData.exports?.length}
            {#each nfsData.exports as exp}
              <div class="share-row">
                <span class="share-name">{exp.path}</span>
                <span class="share-path">{exp.clients || '*'}</span>
              </div>
            {/each}
          {:else}
            <p class="empty-msg">Sin exports configurados</p>
          {/if}
        </div>

      {:else if activeSub === 'webdav'}
        <div class="service-header">
          <div class="section-label">WebDAV</div>
          <div class="svc-status" style="color:{statusColor(webdavData.running)}">
            <div class="svc-dot" style="background:{statusColor(webdavData.running)}"></div>
            {statusLabel(webdavData.running)}
          </div>
        </div>
        <div class="field-group">
          <div class="field-row"><span class="field-label">Puerto</span><span class="field-value">{webdavData.port || 5005}</span></div>
          {#if webdavData.path}
            <div class="field-row"><span class="field-label">Path</span><span class="field-value">{webdavData.path}</span></div>
          {/if}
        </div>

      {:else if activeSub === 'ports'}
        <div class="ports-status-bar">
          <span class="ports-status-label">HTTPS Server</span>
          <div class="https-state" style="color:{httpsEnabled ? 'var(--green)' : 'var(--text-3)'}">
            <div class="ddns-dot" style="background:{httpsEnabled ? 'var(--green)' : 'var(--text-3)'}"></div>
            {httpsEnabled ? 'Running' : 'Stopped'}
          </div>
        </div>

        <div class="section-label" style="margin-top:14px">HTTPS PORT</div>
        <div class="port-inline-row">
          <input class="form-input port-input" type="number" bind:value={httpsPort} placeholder="5009" />
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="https-toggle inline" on:click={() => toggleHttps(!httpsEnabled)}>
            <div class="toggle-track" class:on={httpsEnabled}>
              <div class="toggle-thumb"></div>
            </div>
            <span class="toggle-label">
              {httpsEnabled ? `HTTPS activo en puerto ${httpsPort}` : `Activar HTTPS en puerto ${httpsPort}`}
            </span>
          </div>
        </div>
        <span style="font-size:9px;color:var(--text-3);margin-top:4px;display:block">Default: 5009</span>

        <div class="section-label" style="margin-top:24px">Detalles de conexión</div>
        <div class="cert-details">
          <div class="cert-row"><span class="cert-label">Local</span><span class="cert-value">http://{localIp || '—'}:5000</span></div>
          {#if certDomain}
            <div class="cert-row"><span class="cert-label">Remote (HTTP)</span><span class="cert-value">http://{certDomain}:5000</span></div>
            {#if sslValid}
              <div class="cert-row"><span class="cert-label">Remote (HTTPS)</span><span class="cert-value" style="color:var(--green)">https://{certDomain}:{httpsPort}</span></div>
            {/if}
          {/if}
        </div>

      {:else if activeSub === 'ddns'}
        <div class="section-label">Dynamic DNS</div>
        {#if ddnsData.config?.enabled && ddnsData.config?.domain && !ddnsEditing}
          <div class="ddns-info-cards">
            <div class="ddns-info-card">
              <span class="ddns-info-label">External IP</span>
              <span class="ddns-info-value">{ddnsData.externalIp || '—'}</span>
            </div>
            <div class="ddns-info-card">
              <span class="ddns-info-label">Provider</span>
              <span class="ddns-info-value">{ddnsData.config.provider || '—'}</span>
            </div>
            <div class="ddns-info-card">
              <span class="ddns-info-label">Last Update</span>
              <span class="ddns-info-value small">{ddnsData.lastLog || '—'}</span>
            </div>
          </div>
          <div class="ddns-active">
            <div class="ddns-domain-row">
              <span class="ddns-domain-name">{ddnsData.config.domain}</span>
              <span class="ddns-domain-provider">{ddnsData.config.provider}</span>
              <div class="ddns-domain-right">
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <div class="ddns-auto-toggle" on:click={toggleAutoUpdate}>
                  <div class="toggle-track mini" class:on={ddnsData.config?.autoUpdate !== false}>
                    <div class="toggle-thumb"></div>
                  </div>
                  <span class="ddns-auto-label">Auto</span>
                </div>
                <!-- svelte-ignore a11y_click_events_have_key_events -->
                <!-- svelte-ignore a11y_no_static_element_interactions -->
                <span class="ddns-refresh" on:click={testDdns}>
                  {#if ddnsTesting}
                    <span class="cert-spinner"></span>
                  {:else}
                    <svg width="13" height="13" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                      <path d="M1 4v6h6"/><path d="M23 20v-6h-6"/>
                      <path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/>
                    </svg>
                  {/if}
                </span>
                <div class="ddns-domain-state">
                  <div class="ddns-dot" style="background:var(--green)"></div>
                  Activo
                </div>
              </div>
            </div>
          </div>
          {#if ddnsMsg}
            <div class="ddns-msg" class:error={ddnsMsgError} style="margin-top:6px">{ddnsMsg}</div>
          {/if}
          <!-- svelte-ignore a11y_click_events_have_key_events -->
          <!-- svelte-ignore a11y_no_static_element_interactions -->
          <div class="ddns-add-btn" on:click={() => { ddnsEditing = true; ddnsForm = { provider:'', domain:'', token:'', username:'', password:'' }; }}>
            + Añadir dominio
          </div>
        {:else}
          <div class="ddns-form">
            <div class="form-field">
              <label class="form-label">Proveedor</label>
              <select class="form-select" bind:value={ddnsForm.provider}>
                <option value="">Seleccionar...</option>
                <option value="duckdns">DuckDNS</option>
                <option value="noip">No-IP</option>
                <option value="dynu">Dynu</option>
                <option value="freedns">FreeDNS</option>
              </select>
            </div>
            {#if ddnsForm.provider === 'duckdns'}
              <div class="form-field">
                <label class="form-label">Subdominio</label>
                <input class="form-input" type="text" placeholder="midominio.duckdns.org" bind:value={ddnsForm.domain} />
              </div>
              <div class="form-field">
                <label class="form-label">Token</label>
                <input class="form-input" type="password" placeholder="Token de DuckDNS" bind:value={ddnsForm.token} />
              </div>
            {:else if ddnsForm.provider === 'noip'}
              <div class="form-field">
                <label class="form-label">Hostname</label>
                <input class="form-input" type="text" placeholder="midominio.ddns.net" bind:value={ddnsForm.domain} />
              </div>
              <div class="form-field">
                <label class="form-label">Email</label>
                <input class="form-input" type="email" placeholder="tu@email.com" bind:value={ddnsForm.username} />
              </div>
              <div class="form-field">
                <label class="form-label">Contraseña</label>
                <input class="form-input" type="password" bind:value={ddnsForm.password} />
              </div>
            {:else if ddnsForm.provider === 'dynu'}
              <div class="form-field">
                <label class="form-label">Hostname</label>
                <input class="form-input" type="text" placeholder="midominio.dynu.net" bind:value={ddnsForm.domain} />
              </div>
              <div class="form-field">
                <label class="form-label">Password</label>
                <input class="form-input" type="password" bind:value={ddnsForm.token} />
              </div>
            {:else if ddnsForm.provider === 'freedns'}
              <div class="form-field">
                <label class="form-label">Update Key</label>
                <input class="form-input" type="text" bind:value={ddnsForm.token} />
              </div>
            {/if}
            {#if ddnsForm.provider}
              <div class="form-actions">
                <button class="btn-accent" on:click={saveDdns} disabled={ddnsSaving}>{ddnsSaving ? 'Guardando...' : 'Guardar'}</button>
                <button class="btn-secondary" on:click={testDdns} disabled={ddnsTesting}>{ddnsTesting ? 'Probando...' : 'Probar'}</button>
                {#if ddnsData.config?.enabled}
                  <button class="btn-secondary" on:click={() => ddnsEditing = false}>Cancelar</button>
                {/if}
              </div>
            {/if}
            {#if ddnsMsg}
              <div class="ddns-msg" class:error={ddnsMsgError}>{ddnsMsg}</div>
            {/if}
          </div>
        {/if}
        {#if ddnsData.lastLog}
          <div class="ddns-log"><span class="ddns-log-text">{ddnsData.lastLog}</span></div>
        {/if}

      {:else if activeSub === 'proxy'}
        <div class="section-label">Reverse Proxy</div>
        {#if !proxyData.rules?.length}
          <p class="empty-msg">Sin reglas configuradas</p>
        {:else}
          <div class="proxy-list">
            {#each proxyData.rules as rule}
              <div class="proxy-row">
                <div class="proxy-from">{rule.from || rule.source}</div>
                <div class="proxy-arrow">→</div>
                <div class="proxy-to">{rule.to || rule.target}</div>
              </div>
            {/each}
          </div>
        {/if}

      {:else if activeSub === 'certs'}
        {#if !certData.ssl?.valid}
          <div class="section-label">SSL Certificate</div>
          {#if certDomain}
            <div class="cert-domain-preview">
              <span class="cert-label">Dominio</span>
              <span class="cert-domain-val">{certDomain}</span>
              <span class="cert-domain-src">desde DDNS</span>
            </div>
            <div class="form-field" style="margin-top:14px;max-width:360px">
              <label class="form-label">Email (Let's Encrypt)</label>
              <input class="form-input" type="email" placeholder="admin@example.com" bind:value={certEmail} />
            </div>
            <button class="btn-accent" style="margin-top:12px" on:click={requestCert} disabled={certRequesting}>
              {certRequesting ? 'Solicitando...' : 'Solicitar certificado'}
            </button>
          {:else}
            <p class="empty-msg">Configura un dominio DDNS primero.</p>
          {/if}
          {#if certMsg}
            <div class="ddns-msg" class:error={certMsgError} style="margin-top:10px">{certMsg}</div>
          {/if}
        {/if}
        {#if certData.ssl?.valid}
          <div class="section-label">Certificado activo</div>
          <div class="cert-card">
            <div class="cert-card-header">
              <div class="cert-dot" style="background:var(--green)"></div>
              <span class="cert-card-status">Válido · {certData.ssl?.daysLeft ?? '?'} días restantes</span>
              <!-- svelte-ignore a11y_click_events_have_key_events -->
              <!-- svelte-ignore a11y_no_static_element_interactions -->
              <span class="cert-refresh" on:click={requestCert}>
                {#if certRequesting}
                  <span class="cert-spinner"></span>
                {:else}
                  <svg width="14" height="14" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="2" stroke-linecap="round">
                    <path d="M1 4v6h6"/><path d="M23 20v-6h-6"/>
                    <path d="M20.49 9A9 9 0 0 0 5.64 5.64L1 10m22 4l-4.64 4.36A9 9 0 0 1 3.51 15"/>
                  </svg>
                {/if}
              </span>
            </div>
            <div class="cert-card-grid">
              <div class="cert-cell"><span class="cert-cell-label">Dominio</span><span class="cert-cell-value">{certData.ssl?.domain || certDomain}</span></div>
              <div class="cert-cell"><span class="cert-cell-label">Expira</span><span class="cert-cell-value">{certData.ssl?.expiry || '—'}</span></div>
              <div class="cert-cell"><span class="cert-cell-label">Emisor</span><span class="cert-cell-value">Let's Encrypt</span></div>
              <div class="cert-cell"><span class="cert-cell-label">Renovación</span><span class="cert-cell-value">Automática</span></div>
            </div>
          </div>
          {#if certMsg}
            <div class="ddns-msg" class:error={certMsgError} style="margin-top:10px">{certMsg}</div>
          {/if}
        {/if}

      {:else if activeSub === 'firewall'}
        <div class="section-label">Firewall</div>
        {#if firewallData.ports?.length}
          <div class="section-label" style="margin-bottom:8px">Puertos abiertos</div>
          <div class="port-list">
            {#each firewallData.ports as port}
              <div class="port-tag">{port.port || port}/{port.proto || 'tcp'}</div>
            {/each}
          </div>
        {/if}
        {#if firewallData.rules?.length}
          <div class="section-label" style="margin-top:14px;margin-bottom:8px">Reglas</div>
          <div class="rule-list">
            {#each firewallData.rules as rule}
              <div class="rule-row">
                <span class="rule-action" class:allow={rule.action === 'ACCEPT'} class:deny={rule.action === 'DROP' || rule.action === 'REJECT'}>{rule.action}</span>
                <span class="rule-desc">{rule.source || 'any'} → {rule.dest || 'any'} : {rule.port || '*'}</span>
              </div>
            {/each}
          </div>
        {:else if !firewallData.ports?.length}
          <p class="empty-msg">Sin reglas activas</p>
        {/if}

      {:else if activeSub === 'fail2ban'}
        <div class="section-label">Fail2ban</div>
        <p class="empty-msg">Protección contra fuerza bruta — coming soon</p>

      {/if}
    {/if}
  </div>
</div>

<style>
  .net-root { width:100%; height:100%; display:flex; overflow:hidden; }
  .net-content { flex:1; overflow-y:auto; padding:18px 20px; }
  .net-content::-webkit-scrollbar { width:3px; }
  .net-content::-webkit-scrollbar-thumb { background:rgba(128,128,128,0.15); border-radius:2px; }

  .n-loading { display:flex; align-items:center; justify-content:center; height:100%; }
  .spinner {
    width:24px; height:24px; border-radius:50%;
    border:2px solid rgba(255,255,255,0.08);
    border-top-color:var(--accent);
    animation:spin .7s linear infinite;
  }
  @keyframes spin { to { transform:rotate(360deg); } }

  .section-label {
    font-size:9px; font-weight:600; color:var(--text-3);
    text-transform:uppercase; letter-spacing:.08em; margin-bottom:10px;
  }
  .empty-msg { font-size:12px; color:var(--text-3); }

  /* ── INTERFACES ── */
  .iface-list { display:flex; flex-direction:column; gap:8px; }
  .iface-card {
    border:1px solid var(--border); border-radius:8px;
    background:var(--ibtn-bg); overflow:hidden;
  }
  .iface-header {
    display:flex; align-items:center; gap:8px;
    padding:9px 12px; border-bottom:1px solid var(--border);
  }
  .iface-led { width:7px; height:7px; border-radius:50%; flex-shrink:0; }
  .iface-name { font-size:12px; font-weight:600; color:var(--text-1); font-family:'DM Mono',monospace; }
  .iface-type { font-size:10px; color:var(--text-3); margin-left:2px; }
  .iface-status { margin-left:auto; font-size:10px; font-weight:600; font-family:'DM Mono',monospace; }
  .iface-body { padding:8px 12px; display:flex; flex-direction:column; gap:4px; }
  .iface-row { display:flex; justify-content:space-between; font-size:10px; }
  .iface-row span:first-child { color:var(--text-3); }
  .iface-row span:last-child  { color:var(--text-1); font-family:'DM Mono',monospace; }

  /* ── FIELDS ── */
  .field-group { display:flex; flex-direction:column; gap:0; }
  .field-row {
    display:flex; align-items:center; justify-content:space-between;
    padding:8px 0; border-bottom:1px solid var(--border);
  }
  .field-label { font-size:11px; color:var(--text-2); }
  .field-value { font-size:11px; color:var(--text-1); font-family:'DM Mono',monospace; }

  /* ── SERVICES ── */
  .service-header { display:flex; align-items:center; justify-content:space-between; margin-bottom:10px; }
  .svc-status { display:flex; align-items:center; gap:5px; font-size:10px; font-weight:600; }
  .svc-dot { width:6px; height:6px; border-radius:50%; }

  /* ── SHARES ── */
  .share-row {
    display:flex; gap:10px; padding:6px 0;
    border-bottom:1px solid var(--border);
    font-size:11px;
  }
  .share-name { font-weight:600; color:var(--text-1); min-width:80px; }
  .share-path { color:var(--text-3); font-family:'DM Mono',monospace; }

  /* ── PROXY ── */
  .proxy-list { display:flex; flex-direction:column; gap:6px; }
  .proxy-row {
    display:flex; align-items:center; gap:8px;
    padding:8px 10px; border-radius:7px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    font-size:11px; font-family:'DM Mono',monospace;
  }
  .proxy-from { color:var(--text-1); }
  .proxy-arrow { color:var(--text-3); }
  .proxy-to { color:var(--accent); }

  /* ── FIREWALL ── */
  .port-list { display:flex; flex-wrap:wrap; gap:6px; }
  .port-tag {
    padding:3px 9px; border-radius:5px; font-size:10px; font-weight:600;
    background:var(--ibtn-bg); border:1px solid var(--border);
    color:var(--text-2); font-family:'DM Mono',monospace;
  }
  .rule-list { display:flex; flex-direction:column; gap:5px; }
  .rule-row {
    display:flex; align-items:center; gap:8px;
    padding:7px 10px; border-radius:7px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    font-size:11px;
  }
  .rule-action {
    padding:2px 7px; border-radius:4px; font-size:9px; font-weight:700;
    font-family:'DM Mono',monospace;
    background:var(--ibtn-bg); color:var(--text-3);
  }
  .rule-action.allow { background:rgba(74,222,128,0.12); border:1px solid rgba(74,222,128,0.25); color:var(--green); }
  .rule-action.deny  { background:rgba(248,113,113,0.12); border:1px solid rgba(248,113,113,0.25); color:var(--red); }
  .rule-desc { font-size:10px; color:var(--text-2); font-family:'DM Mono',monospace; }

  /* ── DDNS ── */
  .ddns-status {
    display:flex; align-items:center; justify-content:space-between;
    margin-bottom:20px;
  }
  .ddns-ip-label { font-size:10px; color:var(--text-3); display:block; margin-bottom:2px; }
  .ddns-ip-value { font-size:18px; font-weight:600; color:var(--text-1); font-family:'DM Mono',monospace; }

  .ddns-active { margin-bottom:8px; }
  .ddns-domain-row {
    display:flex; align-items:center; gap:10px;
    padding:10px 0; border-bottom:1px solid var(--border);
  }
  .ddns-domain-name { font-size:12px; font-weight:500; color:var(--text-1); font-family:'DM Mono',monospace; }
  .ddns-domain-provider { font-size:10px; color:var(--text-3); text-transform:uppercase; }
  .ddns-domain-state { display:flex; align-items:center; gap:5px; font-size:10px; font-weight:600; color:var(--green); }
  .ddns-domain-right { display:flex; align-items:center; gap:12px; margin-left:auto; }
  .ddns-dot { width:6px; height:6px; border-radius:50%; }

  .ddns-auto-toggle { display:flex; align-items:center; gap:5px; cursor:pointer; }
  .ddns-auto-label { font-size:9px; color:var(--text-3); text-transform:uppercase; letter-spacing:.04em; }
  .toggle-track.mini { width:28px; height:14px; border-radius:7px; }
  .toggle-track.mini .toggle-thumb { width:10px; height:10px; }
  .toggle-track.mini.on .toggle-thumb { left:16px; }

  .ddns-refresh {
    cursor:pointer; color:var(--text-3); display:flex; align-items:center;
    transition:color .15s;
  }
  .ddns-refresh:hover { color:var(--text-1); }

  .ddns-add-btn {
    font-size:11px; color:var(--accent); cursor:pointer;
    padding:8px 0; transition:opacity .15s;
  }
  .ddns-add-btn:hover { opacity:.7; }

  .ddns-info-cards {
    display:grid; grid-template-columns:repeat(3,1fr); gap:8px;
    margin:14px 0;
  }
  .ddns-info-card {
    padding:10px 12px; border-radius:8px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    display:flex; flex-direction:column; gap:3px;
  }
  .ddns-info-label { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; }
  .ddns-info-value { font-size:13px; font-weight:600; color:var(--text-1); font-family:'DM Mono',monospace; }
  .ddns-info-value.small { font-size:10px; font-weight:400; line-height:1.4; }

  .ddns-form { display:flex; flex-direction:column; gap:14px; max-width:420px; }
  .form-field { display:flex; flex-direction:column; gap:4px; }
  .form-label { font-size:10px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.06em; }
  .form-input, .form-select {
    padding:9px 12px; border-radius:8px;
    background:rgba(255,255,255,0.04); border:1px solid var(--border);
    color:var(--text-1); font-size:12px; font-family:'DM Sans',sans-serif;
    outline:none; transition:border-color .2s;
  }
  .form-input:focus, .form-select:focus { border-color:var(--accent); }
  .form-input::placeholder { color:var(--text-3); }
  .form-select { cursor:pointer; -webkit-appearance:none; appearance:none;
    background-image:url("data:image/svg+xml,%3Csvg width='10' height='6' viewBox='0 0 10 6' fill='none' xmlns='http://www.w3.org/2000/svg'%3E%3Cpath d='M1 1l4 4 4-4' stroke='%23666' stroke-width='1.5' stroke-linecap='round'/%3E%3C/svg%3E");
    background-repeat:no-repeat; background-position:right 12px center;
    padding-right:32px;
  }
  .form-select option { background:var(--bg-inner); color:var(--text-1); }

  .form-actions { display:flex; gap:8px; margin-top:4px; }
  .btn-accent {
    padding:8px 16px; border-radius:8px; border:none;
    background:linear-gradient(135deg, var(--accent), var(--accent2));
    color:#fff; font-size:11px; font-weight:600; cursor:pointer;
    font-family:inherit; transition:opacity .15s;
  }
  .btn-accent:hover { opacity:.88; }
  .btn-accent:disabled { opacity:.5; cursor:not-allowed; }
  .btn-secondary {
    padding:8px 16px; border-radius:8px;
    border:1px solid var(--border); background:var(--ibtn-bg);
    color:var(--text-2); font-size:11px; font-weight:500; cursor:pointer;
    font-family:inherit; transition:all .15s;
  }
  .btn-secondary:hover { color:var(--text-1); border-color:var(--border-hi); }
  .btn-secondary:disabled { opacity:.5; cursor:not-allowed; }
  .btn-danger {
    padding:8px 16px; border-radius:8px;
    border:1px solid rgba(248,113,113,0.25); background:rgba(248,113,113,0.08);
    color:var(--red); font-size:11px; font-weight:500; cursor:pointer;
    font-family:inherit; transition:opacity .15s;
  }
  .btn-danger:hover { opacity:.8; }

  .ddns-msg {
    font-size:11px; color:var(--green); padding:6px 0;
  }
  .ddns-msg.error { color:var(--red); }

  .ddns-log { margin-top:8px; }
  .ddns-log-text { font-size:10px; color:var(--text-3); font-family:'DM Mono',monospace; display:block; margin-top:2px; }

  /* ── CERTS ── */
  .cert-card {
    border-radius:8px; overflow:hidden;
    border:1px solid var(--border); background:var(--ibtn-bg);
  }
  .cert-card-header {
    display:flex; align-items:center; gap:8px;
    padding:10px 14px; border-bottom:1px solid var(--border);
  }
  .cert-dot { width:7px; height:7px; border-radius:50%; box-shadow:0 0 5px rgba(74,222,128,0.5); flex-shrink:0; }
  .cert-card-status { font-size:11px; font-weight:600; color:var(--green); font-family:'DM Mono',monospace; }
  .cert-refresh {
    margin-left:auto; cursor:pointer; color:var(--text-3);
    transition:color .15s; display:flex; align-items:center;
  }
  .cert-refresh:hover { color:var(--text-1); }
  .cert-spinner {
    width:14px; height:14px; border-radius:50%;
    border:2px solid rgba(255,255,255,0.08); border-top-color:var(--accent);
    animation:spin .7s linear infinite; display:inline-block;
  }
  .cert-card-grid {
    display:grid; grid-template-columns:1fr 1fr;
    padding:12px 14px; gap:12px;
  }
  .cert-cell { display:flex; flex-direction:column; gap:2px; }
  .cert-cell-label { font-size:9px; color:var(--text-3); text-transform:uppercase; letter-spacing:.04em; }
  .cert-cell-value { font-size:11px; color:var(--text-1); font-family:'DM Mono',monospace; }

  .cert-domain-preview {
    display:flex; align-items:baseline; gap:10px; margin-bottom:4px;
  }
  .cert-domain-val { font-size:14px; font-weight:500; color:var(--text-1); font-family:'DM Mono',monospace; }
  .cert-domain-src { font-size:9px; color:var(--text-3); }

  /* ── HTTPS ── */
  .https-status-row { display:flex; align-items:center; justify-content:space-between; }
  .https-title { font-size:13px; font-weight:600; color:var(--text-1); }
  .https-subtitle { font-size:10px; color:var(--text-3); margin-top:1px; }
  .https-state { display:flex; align-items:center; gap:6px; font-size:11px; font-weight:600; }

  .https-toggle { display:flex; align-items:center; gap:10px; cursor:pointer; }
  .toggle-track {
    width:38px; height:20px; border-radius:10px;
    background:rgba(128,128,128,0.25); position:relative;
    transition:background .2s;
  }
  .toggle-track.on { background:var(--accent); }
  .toggle-thumb {
    width:16px; height:16px; border-radius:50%;
    background:white; position:absolute; top:2px; left:2px;
    transition:left .2s;
  }
  .toggle-track.on .toggle-thumb { left:20px; }
  .toggle-label { font-size:11px; color:var(--text-2); }

  .https-url {
    font-size:12px; font-weight:500; color:var(--green);
    font-family:'DM Mono',monospace;
  }

  /* ── CERT DETAILS (Port Exposure connection info) ── */
  .cert-details { display:flex; flex-direction:column; gap:0; }
  .cert-row {
    display:flex; align-items:baseline; justify-content:space-between;
    padding:7px 0; border-bottom:1px solid var(--border); gap:12px;
  }
  .cert-label { font-size:10px; color:var(--text-3); flex-shrink:0; min-width:120px; }
  .cert-value { font-size:11px; color:var(--text-1); font-family:'DM Mono',monospace; word-break:break-all; }

  /* ── SUB-TABS ── */
  .sub-tabs {
    display:flex; gap:4px; margin-bottom:16px;
    padding-bottom:12px; border-bottom:1px solid var(--border);
    flex-wrap:wrap;
  }
  .sub-tab {
    padding:5px 12px; border-radius:6px; cursor:pointer;
    font-size:11px; font-weight:500; color:var(--text-3);
    border:1px solid transparent; transition:all .15s;
  }
  .sub-tab:hover { color:var(--text-2); background:rgba(128,128,128,0.06); }
  .sub-tab.active {
    background:var(--active-bg); color:var(--text-1);
    border-color:var(--border-hi);
  }


  /* ── PORT EXPOSURE ── */
  .ports-status-bar {
    display:flex; align-items:center; justify-content:space-between;
    margin-bottom:4px;
  }
  .ports-status-label { font-size:9px; font-weight:600; color:var(--text-3); text-transform:uppercase; letter-spacing:.08em; }
  .port-inline-row { display:flex; align-items:center; gap:12px; margin-top:4px; }
  .port-input { max-width:140px; }
  .https-toggle.inline { margin-top:0 !important; }

</style>
