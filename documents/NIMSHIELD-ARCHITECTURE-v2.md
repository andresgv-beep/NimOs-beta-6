# NimShield — Architecture & Design Document
## NimOS Integrated Security Module
### Version 2.0 — March 2026

---

## 1. Visión

NimShield es el módulo de seguridad activa de NimOS. No es un firewall estático ni un antivirus — es un sistema de defensa en profundidad que opera en múltiples capas: kernel, red, aplicación y contenedores. Detecta, clasifica, reacciona y aprende.

Filosofía: un NAS expuesto a internet debe comportarse como una fortaleza con múltiples anillos de defensa. Si un anillo falla, el siguiente lo contiene. NimShield no confía en ninguna capa individual.

### 1.1 Principios de Diseño

- **Defense in Depth**: Mínimo 3 capas entre un atacante e internet y los datos del usuario
- **Zero-dependency core**: El motor de reglas y bloqueo funciona sin software externo. Las capas avanzadas (eBPF, seccomp) se activan si el kernel lo soporta
- **Opt-in granular**: Cada función se activa/desactiva independientemente
- **Cero falsos positivos destructivos**: NimShield nunca puede bloquear al admin legítimo sin mecanismo de recuperación
- **Observable**: Todo lo que NimShield hace es visible, explicable y reversible
- **Adaptativo**: No solo reglas fijas — aprende el baseline de tráfico normal y alerta anomalías
- **Fail-open safe**: Si NimShield crashea, el daemon sigue funcionando. Seguridad degradada, no servicio muerto

### 1.2 Modelo de Amenazas

NimShield protege contra estos escenarios ordenados por probabilidad:

| Escenario | Probabilidad | Impacto | Capa de defensa |
|-----------|-------------|---------|-----------------|
| Brute force SSH/HTTP | Alta | Medio | L3: Rate limit + auto-block |
| Vulnerability scanner | Alta | Bajo | L3: UA detect + throttle |
| Path traversal | Media | Alto | L3: Input validation + block |
| SQL/Command injection | Media | Crítico | L3: Pattern detect + block |
| Port scanning | Media | Bajo | L2: nftables rate limit |
| Container escape | Baja | Crítico | L1: seccomp + AppArmor + L2: network isolation |
| Credential stuffing | Media | Alto | L3: Multi-user detect + GeoIP |
| Slow-rate attack | Baja | Alto | L3: Anomaly detection (behavioral) |
| Stolen token replay | Baja | Alto | L3: Session binding + anomaly |
| Insider / compromised admin | Muy baja | Crítico | L3: Audit trail + change detection |

---

## 2. Arquitectura — Defense in Depth

NimShield opera en 3 capas concurrentes. Un ataque debe superar TODAS las capas para tener éxito.

```
═══════════════════════════════════════════════════════════
                    INTERNET / LAN
═══════════════════════════════════════════════════════════
                         │
                         ▼
┌─────────────────────────────────────────────────────────┐
│              LAYER 1 — KERNEL HARDENING                 │
│                                                         │
│  ┌─────────┐  ┌──────────┐  ┌────────┐  ┌───────────┐ │
│  │ sysctl  │  │ seccomp  │  │AppArmor│  │   eBPF    │ │
│  │ params  │  │ profiles │  │profiles│  │  probes   │ │
│  │         │  │ per-svc  │  │per-svc │  │ (optional)│ │
│  └─────────┘  └──────────┘  └────────┘  └───────────┘ │
│  • SYN flood protection    • Container sandboxing      │
│  • ICMP hardening          • Syscall whitelist          │
│  • Shared memory protect   • File access control        │
│  • ASLR enforced           • Network namespace          │
└───────────────────────────┬─────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│              LAYER 2 — NETWORK FIREWALL                 │
│                                                         │
│  ┌─────────────────────────────────────────────────┐   │
│  │              nftables (primary)                   │   │
│  │                                                   │   │
│  │  ┌──────────┐  ┌──────────┐  ┌───────────────┐  │   │
│  │  │ nimshield│  │ nimshield│  │  nimshield    │  │   │
│  │  │ _input   │  │ _forward │  │  _ratelimit   │  │   │
│  │  │ (allow/  │  │ (docker  │  │  (per-IP      │  │   │
│  │  │  deny)   │  │  egress) │  │   throttle)   │  │   │
│  │  └──────────┘  └──────────┘  └───────────────┘  │   │
│  └─────────────────────────────────────────────────┘   │
│  • Per-IP connection limits    • SYN proxy              │
│  • Container egress control    • GeoIP pre-filter       │
│  • Port knocking (optional)    • Rate limit per-subnet  │
│  UFW compatibility: nftables backend, ufw as alias      │
└───────────────────────────┬─────────────────────────────┘
                            │
                            ▼
┌─────────────────────────────────────────────────────────┐
│              LAYER 3 — APPLICATION SHIELD                │
│                                                         │
│  ┌──────────────────────────────────────────────────┐  │
│  │                 nimos-daemon                       │  │
│  │                                                    │  │
│  │  ┌────────────────────────────────────────────┐   │  │
│  │  │              NimShield Engine                │   │  │
│  │  │                                              │   │  │
│  │  │  Collector → Analyzer → Reactor → Notifier   │   │  │
│  │  │       │          │          │          │     │   │  │
│  │  │    events    patterns    actions    alerts    │   │  │
│  │  │               + ML       + L2 sync           │   │  │
│  │  │            baseline                          │   │  │
│  │  └────────────────────────────────────────────┘   │  │
│  │                                                    │  │
│  │  ┌──────┐ ┌──────┐ ┌───────┐ ┌────────┐ ┌─────┐ │  │
│  │  │ Auth │ │Files │ │Docker │ │Network │ │ ... │ │  │
│  │  └──────┘ └──────┘ └───────┘ └────────┘ └─────┘ │  │
│  └──────────────────────────────────────────────────┘  │
│                                                         │
│  ┌──────────────────────────────────────────────────┐  │
│  │              SQLite (nimos.db)                     │  │
│  │  shield_events │ shield_blocks │ shield_baseline  │  │
│  └──────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────┘
```

---

## 3. Layer 1 — Kernel Hardening

NimShield configura el kernel del sistema como primera línea de defensa. Esto protege incluso si la aplicación tiene un bug que no hemos descubierto.

### 3.1 Sysctl Hardening

Fichero: `/etc/sysctl.d/99-nimshield.conf`

NimShield genera y aplica este fichero al activarse:

```ini
# ── Network stack hardening ──
# SYN flood protection
net.ipv4.tcp_syncookies = 1
net.ipv4.tcp_max_syn_backlog = 4096
net.ipv4.tcp_synack_retries = 2

# Prevent IP spoofing
net.ipv4.conf.all.rp_filter = 1
net.ipv4.conf.default.rp_filter = 1

# Ignore ICMP redirects (MITM prevention)
net.ipv4.conf.all.accept_redirects = 0
net.ipv4.conf.default.accept_redirects = 0
net.ipv4.conf.all.send_redirects = 0
net.ipv6.conf.all.accept_redirects = 0

# Ignore source-routed packets
net.ipv4.conf.all.accept_source_route = 0
net.ipv6.conf.all.accept_source_route = 0

# Log martian packets (impossible source addresses)
net.ipv4.conf.all.log_martians = 1

# Ignore ICMP broadcasts (smurf attack prevention)
net.ipv4.icmp_echo_ignore_broadcasts = 1

# ── Memory protection ──
# Restrict core dumps
fs.suid_dumpable = 0

# ASLR maximum
kernel.randomize_va_space = 2

# Restrict kernel pointers in logs
kernel.kptr_restrict = 2

# Restrict dmesg to root
kernel.dmesg_restrict = 1

# ── Shared memory hardening ──
kernel.yama.ptrace_scope = 2

# ── File system ──
# Prevent hardlink/symlink attacks
fs.protected_hardlinks = 1
fs.protected_symlinks = 1
fs.protected_fifos = 2
fs.protected_regular = 2
```

Implementación en Go:
```go
func applyKernelHardening() error {
    params := map[string]string{
        "net.ipv4.tcp_syncookies": "1",
        // ... all params above
    }
    for key, val := range params {
        if err := os.WriteFile(
            "/proc/sys/" + strings.ReplaceAll(key, ".", "/"),
            []byte(val), 0644,
        ); err != nil {
            logMsg("shield: sysctl %s failed: %v", key, err)
        }
    }
    return nil
}
```

### 3.2 Seccomp Profiles

NimShield genera seccomp profiles restrictivos para cada servicio:

**Profile: nimos-daemon (el propio daemon)**
```json
{
  "defaultAction": "SCMP_ACT_ERRNO",
  "syscalls": [
    { "names": ["read","write","open","close","stat","fstat","lstat",
                "poll","lseek","mmap","mprotect","munmap","brk",
                "ioctl","access","pipe","select","sched_yield",
                "socket","connect","accept","sendto","recvfrom",
                "bind","listen","getsockname","getpeername",
                "clone","execve","wait4","kill","getpid","getuid",
                "getgid","epoll_create","epoll_wait","epoll_ctl",
                "openat","mkdirat","unlinkat","renameat","fchownat",
                "futex","set_robust_list","nanosleep","clock_gettime"],
      "action": "SCMP_ACT_ALLOW" }
  ]
}
```

**Profile: docker-containers (aplicado via Docker --security-opt)**

Cada container lanzado por el AppStore hereda un seccomp profile restrictivo:
```go
func containerSeccompArgs(appId string) []string {
    profile := getContainerSeccompProfile(appId)
    return []string{
        "--security-opt", "seccomp=" + profile,
        "--security-opt", "no-new-privileges:true",
    }
}
```

Syscalls bloqueados en todos los containers:
- `mount`, `umount2` — No montar filesystems
- `pivot_root`, `chroot` — No cambiar root
- `reboot`, `kexec_load` — No reiniciar el host
- `create_module`, `init_module`, `delete_module` — No cargar módulos kernel
- `ptrace` — No debuggear otros procesos
- `keyctl` — No acceder al kernel keyring
- `bpf` — No cargar programas eBPF (excepto el propio NimShield)

### 3.3 AppArmor Profiles

Si AppArmor está disponible (Ubuntu, Debian), NimShield genera profiles:

**Profile: nimos-daemon**
```
#include <tunables/global>

profile nimos-daemon /opt/nimbusos/daemon/nimos-daemon {
  #include <abstractions/base>
  #include <abstractions/nameservice>

  # Read-only system access
  /etc/hostname r,
  /etc/hosts r,
  /etc/resolv.conf r,
  /etc/ssl/** r,
  /etc/docker/daemon.json rw,
  /etc/ufw/** r,
  /usr/sbin/ufw Ux,

  # NimOS data
  /var/lib/nimbusos/** rw,
  /var/log/nimbusos/** w,
  /run/nimos-daemon.sock rw,

  # Pool access
  /nimbus/pools/** rw,

  # Docker socket
  /var/run/docker.sock rw,

  # Deny everything else write
  deny /boot/** w,
  deny /usr/** w,
  deny /sbin/** w,
  deny /root/** rw,
}
```

**Profile: docker-app (template para containers)**
```
profile docker-app-{ID} flags=(attach_disconnected,mediate_deleted) {
  # Allow container normal operation
  file,
  network,

  # Block host filesystem access
  deny /etc/shadow r,
  deny /etc/passwd r,
  deny /root/** rw,
  deny /home/** rw,
  deny /var/lib/nimbusos/** rw,

  # Block dangerous binaries
  deny /usr/bin/nsenter x,
  deny /usr/bin/mount x,
  deny /usr/bin/umount x,
}
```

### 3.4 eBPF Probes (Opcional — requiere kernel 5.8+)

Si el kernel lo soporta, NimShield carga probes eBPF para monitorización a nivel kernel sin overhead:

**Probe: conexiones de red de containers**
```
Attach point: kprobe/tcp_v4_connect
Función: Monitorizar TODAS las conexiones TCP salientes de containers Docker.
         Si un container intenta conectar a IPs internas (192.168.x.x) o
         puertos sospechosos (22, 25, 445), alertar.
```

**Probe: acceso a ficheros sensibles**
```
Attach point: kprobe/vfs_open
Función: Alertar si cualquier proceso (especialmente containers) intenta
         abrir /etc/shadow, /etc/passwd, /var/lib/nimbusos/config/*.
```

**Probe: escalación de privilegios**
```
Attach point: kprobe/commit_creds
Función: Detectar cambios de UID (especialmente uid=0) en procesos
         que no son root. Indicador de container escape.
```

Implementación: NimShield incluye los programas eBPF pre-compilados como byte arrays en Go. Se cargan con `cilium/ebpf` library si el kernel es compatible. Si no, se ignoran silenciosamente.

```go
func loadEBPFProbes() {
    if !kernelSupportseBPF() {
        logMsg("shield: eBPF not available (kernel < 5.8), skipping probes")
        return
    }
    // Load pre-compiled probes
    if err := loadProbe("tcp_connect_monitor", tcpConnectBPF); err != nil {
        logMsg("shield: eBPF tcp probe failed: %v", err)
    }
    // ... more probes
}
```

---

## 4. Layer 2 — Network Firewall (nftables)

### 4.1 Por qué nftables en vez de UFW

UFW es un frontend de iptables/nftables. Depender solo de UFW tiene problemas:
- UFW no tiene rate limiting per-IP nativo
- UFW no puede filtrar por GeoIP
- UFW no tiene contadores atómicos para detección
- Si alguien ejecuta `ufw disable`, todo se cae

NimShield usa **nftables directamente** con sus propias tablas y chains. UFW sigue funcionando en paralelo para el usuario (las reglas manuales del user van por UFW, las automáticas de NimShield por nftables directo). No se pisan.

### 4.2 Estructura nftables

```nft
table inet nimshield {
    # ── Sets dinámicos (actualizados por el daemon) ──

    set blocked_ips {
        type ipv4_addr
        flags timeout
        # IPs se añaden con timeout automático
        # Ej: nft add element inet nimshield blocked_ips { 1.2.3.4 timeout 3600s }
    }

    set whitelisted_ips {
        type ipv4_addr
        # IPs que NUNCA se bloquean
        elements = { 127.0.0.1 }
    }

    set ratelimit_ips {
        type ipv4_addr
        flags dynamic,timeout
    }

    # ── GeoIP set (opcional, cargado desde base de datos) ──
    set geo_blocked {
        type ipv4_addr
        flags interval
        # Se carga con rangos CIDR por país
    }

    # ── Chain principal de entrada ──
    chain input {
        type filter hook input priority -10; policy accept;

        # 1. Whitelist siempre pasa
        ip saddr @whitelisted_ips accept

        # 2. Blocked IPs drop silencioso
        ip saddr @blocked_ips drop

        # 3. GeoIP pre-filter (si activo)
        ip saddr @geo_blocked drop

        # 4. Rate limit: max 30 nuevas conexiones/min per IP al puerto del NAS
        tcp dport 5000 ct state new meter nimshield_ratelimit \
            { ip saddr limit rate over 30/minute burst 10 packets } \
            add @ratelimit_ips { ip saddr timeout 300s } drop

        # 5. SYN flood: limitar SYN packets globales
        tcp flags syn limit rate 100/second burst 50 accept
        tcp flags syn drop

        # 6. Invalid packets drop
        ct state invalid drop
    }

    # ── Chain para tráfico de containers (Docker FORWARD) ──
    chain forward {
        type filter hook forward priority 0; policy accept;

        # Containers no pueden contactar el host en puertos internos
        # (previene container→host pivoting)
        iifname "docker*" ip daddr 127.0.0.1 drop
        iifname "docker*" ip daddr { 192.168.0.0/16, 10.0.0.0/8, 172.16.0.0/12 } \
            tcp dport { 22, 5000 } drop

        # Containers con política restrictiva: solo HTTP/HTTPS saliente
        # (aplicado per-container via metadata)
    }

    # ── Chain de logging ──
    chain log_and_drop {
        log prefix "nimshield_drop: " group 1
        drop
    }
}
```

### 4.3 Sincronización daemon ↔ nftables

```go
// Añadir IP a la blocklist con timeout
func nftBlockIP(ip string, duration time.Duration) error {
    secs := int(duration.Seconds())
    cmd := fmt.Sprintf("nft add element inet nimshield blocked_ips { %s timeout %ds }", ip, secs)
    _, ok := runStrict(cmd)
    if !ok {
        // Fallback a UFW si nftables no disponible
        return ufwBlockIP(ip)
    }
    return nil
}

// Quitar IP de la blocklist
func nftUnblockIP(ip string) error {
    cmd := fmt.Sprintf("nft delete element inet nimshield blocked_ips { %s }", ip)
    _, ok := runStrict(cmd)
    if !ok {
        return ufwUnblockIP(ip)
    }
    return nil
}

// Cargar GeoIP ranges
func nftLoadGeoBlock(cidrs []string) error {
    // Flush and reload
    runStrict("nft flush set inet nimshield geo_blocked")
    batch := "nft add element inet nimshield geo_blocked { "
    batch += strings.Join(cidrs, ", ")
    batch += " }"
    _, ok := runStrict(batch)
    return ok
}
```

### 4.4 Fallback chain

Si nftables no está disponible (kernel muy viejo), NimShield cae a:
1. **iptables** directo (sin UFW)
2. Si tampoco hay iptables → **solo bloqueo a nivel aplicación** (L3)

```go
func initFirewall() FirewallBackend {
    if hasNftables() {
        return &NftablesBackend{}
    }
    if hasIptables() {
        logMsg("shield: nftables not available, falling back to iptables")
        return &IptablesBackend{}
    }
    logMsg("shield: WARNING — no kernel firewall available, using app-level blocking only")
    return &AppLevelBackend{}
}
```

### 4.5 Container Network Policies

Cada container instalado por AppStore recibe una política de red:

| Política | Descripción | Containers |
|----------|-------------|------------|
| **full** | Sin restricciones de red | Explícitamente seleccionado por admin |
| **standard** | HTTP/HTTPS saliente permitido, no puede contactar LAN | Default para la mayoría |
| **isolated** | Solo comunicación con otros containers del mismo stack | Databases, Redis |
| **none** | Sin red | Herramientas offline |

Implementación via Docker network + nftables rules por bridge interface.

---

## 5. Layer 3 — Application Shield

### 5.1 Collector

Idéntico a v1 — canal Go buffered, instrumentación en todos los módulos.

```go
var shieldEvents = make(chan ShieldEvent, 2000)

type ShieldEvent struct {
    Timestamp time.Time
    Category  string
    Severity  string
    SourceIP  string
    UserAgent string
    Endpoint  string
    Username  string
    Method    string
    Status    int
    Details   map[string]interface{}
}
```

### 5.2 Analyzer — Rules + Anomaly Detection

El Analyzer tiene DOS motores que trabajan en paralelo:

#### Motor 1: Rule Engine (determinístico)

18 reglas predefinidas — idénticas a v1. Respuesta inmediata y predecible.

| ID | Nombre | Trigger | Acción |
|----|--------|---------|--------|
| `AUTH-001` | Brute Force Login | 5+ login fail / 5min / IP | Block 30min |
| `AUTH-002` | Credential Stuffing | 3+ users fail / 2min / IP | Block 1h |
| `AUTH-003` | Token Spray | 10+ tokens inválidos / 1min / IP | Block 1h |
| `AUTH-004` | 2FA Brute Force | 5+ 2FA fail / 5min / user | Lock user 30min |
| `TRAV-001` | Path Traversal Scan | 3+ traversal / 1min / IP | Block 2h |
| `TRAV-002` | Config File Probe | Intento de leer config files | Block 4h + notify |
| `INJ-001` | SQL Injection | 3+ SQLi / 5min / IP | Block 2h |
| `INJ-002` | Command Injection | Cualquier cmd injection | Block 24h + notify |
| `INJ-003` | XSS Attack | 5+ XSS / 5min / IP | Block 1h |
| `SCAN-001` | Port Scan | 10+ 404s / 1min / IP | Block 30min |
| `SCAN-002` | API Enumeration | 20+ endpoints / 2min / IP | Throttle |
| `SCAN-003` | Vuln Scanner UA | nikto, sqlmap, nmap UA | Block 24h |
| `NET-001` | Geo-Anomaly | Login desde país nuevo | Notify |
| `NET-002` | Tor Exit Node | IP en lista Tor | Configurable |
| `DOCK-001` | Container Escape Attempt | Syscall violation (seccomp) | Kill container + notify |
| `DOCK-002` | Malicious Compose | Host mounts peligrosos | Reject + notify |
| `SYS-001` | Rapid Config Change | 5+ changes / 5min | Notify |
| `SYS-002` | Admin Lockout Risk | Último admin desactivándose | Prevent |

#### Motor 2: Behavioral Baseline (adaptativo)

Esto es lo que faltaba en v1. No es ML pesado — es estadística simple pero efectiva.

**Concepto**: NimShield aprende el patrón de uso "normal" del NAS durante 7 días. Después, cualquier desviación significativa genera alerta.

```go
type Baseline struct {
    // Calculado sobre ventana de 7 días rolling
    AvgRequestsPerHour    float64
    StdDevRequestsPerHour float64
    AvgUniqueIPsPerDay    int
    StdDevUniqueIPsPerDay float64
    NormalEndpoints       map[string]float64  // endpoint → avg hits/hour
    NormalUserAgents      map[string]bool     // UAs vistos en los últimos 7 días
    NormalGeoCountries    map[string]bool     // Países vistos en los últimos 7 días
    NormalLoginHours      [24]float64         // Distribución horaria de logins
    NormalIPSubnets       map[string]bool     // /24 subnets normales
    LastUpdated           time.Time
}

// Se recalcula cada hora con datos de los últimos 7 días
func (b *Baseline) Update(events []ShieldEvent) {
    // ... statistical calculations
}

// Detectar anomalías
func (b *Baseline) CheckAnomaly(event ShieldEvent) *Anomaly {
    anomalies := []string{}

    // 1. Spike de tráfico: >3 desviaciones estándar sobre la media
    currentHourRate := getCurrentHourRequestRate()
    if currentHourRate > b.AvgRequestsPerHour + 3*b.StdDevRequestsPerHour {
        anomalies = append(anomalies, "traffic_spike")
    }

    // 2. IP de subnet nunca vista
    subnet := extractSubnet24(event.SourceIP)
    if !b.NormalIPSubnets[subnet] {
        anomalies = append(anomalies, "new_subnet")
    }

    // 3. User-Agent nunca visto
    if !b.NormalUserAgents[event.UserAgent] && event.UserAgent != "" {
        anomalies = append(anomalies, "new_useragent")
    }

    // 4. Login fuera de horario habitual
    hour := event.Timestamp.Hour()
    if b.NormalLoginHours[hour] < 0.01 && event.Category == "auth" {
        anomalies = append(anomalies, "unusual_hour")
    }

    // 5. Endpoint no habitual con alta frecuencia
    if rate, ok := b.NormalEndpoints[event.Endpoint]; ok {
        if getCurrentEndpointRate(event.Endpoint) > rate*5 {
            anomalies = append(anomalies, "endpoint_spike")
        }
    } else {
        // Endpoint nunca visto
        anomalies = append(anomalies, "new_endpoint")
    }

    // 6. País nuevo
    if event.GeoCountry != "" && !b.NormalGeoCountries[event.GeoCountry] {
        anomalies = append(anomalies, "new_country")
    }

    if len(anomalies) == 0 {
        return nil
    }
    return &Anomaly{Types: anomalies, Score: len(anomalies) * 15}
}
```

**El atacante lento**: Un atacante que hace 1 intento cada 10 minutos no triggerará AUTH-001 (que busca 5 en 5min). Pero el Behavioral Baseline detectará:
- UA nuevo que nunca se vio antes
- Subnet nueva
- Patrón sostenido de logins fallidos (aunque sean pocos por ventana)
- Horario inusual

La respuesta para anomalías no es block inmediato sino **incrementar la sensibilidad de las reglas para esa IP**: reducir thresholds a la mitad, activar logging verbose, y notificar al admin.

```go
// Slow-rate attack detection
type SlowRateTracker struct {
    mu      sync.Mutex
    history map[string]*SlowRateProfile  // IP → profile
}

type SlowRateProfile struct {
    FailedLogins    int
    FirstSeen       time.Time
    LastSeen        time.Time
    TotalDuration   time.Duration
    // Si acumula 10+ login fails en 24h, block aunque nunca haya
    // superado el threshold de 5/5min
}
```

### 5.3 Threat Score (mejorado)

```
Score = RuleScore + AnomalyScore + HistoryScore

RuleScore: Puntos fijos por regla triggered (ej: AUTH-001 = +30, INJ-002 = +60)
AnomalyScore: Puntos por anomalías detectadas (ej: new_country = +15)
HistoryScore: +10 por cada bloqueo previo en los últimos 30 días

Decay: -1 punto por hora, mínimo 0
Escalation:
  0-20  → Log only
  21-40 → Throttle + increased sensitivity
  41-70 → Block temporal (L2 nftables + L3 app)
  71-90 → Block 24h + session kill
  91-100 → Ban permanente + nftables permanent + notify
```

### 5.4 Session Binding

Para prevenir token replay desde otra IP:

```go
type BoundSession struct {
    Token       string
    Username    string
    BoundIP     string    // IP del login original
    BoundUA     string    // User-Agent del login original
    Fingerprint string    // Hash(IP + UA + Accept-Language)
}

// En cada request autenticado:
func validateSessionBinding(session, r) bool {
    currentFP := hashFingerprint(clientIP(r), r.UserAgent, r.Header.Get("Accept-Language"))
    if session.Fingerprint != currentFP {
        // Posible token stolen — no bloquear inmediatamente pero alertar
        shieldLog("auth", "high", clientIP(r), r.URL.Path, map[string]interface{}{
            "type": "session_anomaly",
            "original_ip": session.BoundIP,
            "current_ip": clientIP(r),
        })
        // En modo Strict: invalidar sesión inmediatamente
        if getShieldMode() >= ModeStrict {
            return false
        }
    }
    return true
}
```

### 5.5 Reactor

Tres niveles — idéntico a v1 pero con integración L2:

```go
func blockIP(ip string, duration time.Duration, reason string) {
    // L3: Application level block (inmediato)
    addToBlocklist(ip, duration, reason)

    // L2: Firewall level block (más profundo)
    firewall.BlockIP(ip, duration)  // nftables → iptables → app-only

    // Kill active sessions from this IP
    if getShieldConfig("kill_sessions_on_block") {
        dbSessionsDeleteByIP(ip)
    }

    // Notify
    notifyBlock(ip, duration, reason)
}
```

### 5.6 Notifier

Idéntico a v1: Desktop push (WebSocket), Email (SMTP), Webhook (HTTP POST), Log.

Añadido: **Notification deduplication** — Si la misma regla se trigerea 100 veces en 1 minuto para la misma IP, enviar UNA notificación con count=100, no 100 emails.

---

## 6. Docker Security (Deep)

### 6.1 Container Launch Hardening

Cada container lanzado por AppStore o manualmente hereda:

```go
func buildSecureDockerArgs(appId string, policy ContainerPolicy) []string {
    args := []string{
        "--security-opt", "no-new-privileges:true",
        "--security-opt", "seccomp=" + policy.SeccompProfile,
        "--cap-drop=ALL",
        "--read-only",           // Root filesystem read-only
        "--tmpfs", "/tmp:size=100M",
        "--pids-limit", "256",   // Max 256 processes
        "--memory", policy.MemoryLimit,
        "--cpus", policy.CPULimit,
    }

    // Add back only needed capabilities per app
    for _, cap := range policy.AllowedCaps {
        args = append(args, "--cap-add="+cap)
    }

    // Network policy
    if policy.Network == "isolated" {
        args = append(args, "--network=nimshield_isolated")
    }

    // AppArmor if available
    if hasAppArmor() {
        args = append(args, "--security-opt", "apparmor=docker-app-"+appId)
    }

    return args
}
```

### 6.2 Compose Sanitization

Antes de deploy, NimShield analiza el docker-compose.yml:

```go
type ComposeSanitizer struct{}

func (s *ComposeSanitizer) Check(compose string) []SecurityIssue {
    issues := []SecurityIssue{}
    parsed := parseCompose(compose)

    for _, service := range parsed.Services {
        // 1. Host volume mounts peligrosos
        for _, vol := range service.Volumes {
            if isDangerousMount(vol.Source) {
                issues = append(issues, SecurityIssue{
                    Severity: "critical",
                    Message: fmt.Sprintf("Dangerous host mount: %s", vol.Source),
                })
            }
        }

        // 2. Privileged mode
        if service.Privileged {
            issues = append(issues, SecurityIssue{
                Severity: "critical",
                Message: "Container runs in privileged mode",
            })
        }

        // 3. Host network
        if service.NetworkMode == "host" {
            issues = append(issues, SecurityIssue{
                Severity: "high",
                Message: "Container uses host network",
            })
        }

        // 4. Docker socket mount
        for _, vol := range service.Volumes {
            if strings.Contains(vol.Source, "docker.sock") {
                issues = append(issues, SecurityIssue{
                    Severity: "critical",
                    Message: "Docker socket mounted — container can control host",
                })
            }
        }

        // 5. SYS_ADMIN capability
        for _, cap := range service.CapAdd {
            if cap == "SYS_ADMIN" || cap == "ALL" {
                issues = append(issues, SecurityIssue{
                    Severity: "critical",
                    Message: fmt.Sprintf("Dangerous capability: %s", cap),
                })
            }
        }
    }
    return issues
}

func isDangerousMount(source string) bool {
    dangerous := []string{
        "/", "/etc", "/root", "/home", "/var/lib/nimbusos",
        "/boot", "/usr", "/sbin", "/bin", "/proc", "/sys",
        "/dev", "/run", "/var/run/docker.sock",
    }
    for _, d := range dangerous {
        if source == d || strings.HasPrefix(source, d+"/") {
            return true
        }
    }
    return false
}
```

Si se detectan issues `critical`, el deploy se **rechaza** con explicación. El admin puede forzar override con una flag explícita (`"force_unsafe": true`) que se logea como evento `SYS-003`.

### 6.3 Container Runtime Monitoring

NimShield monitoriza containers en ejecución:

```go
func monitorContainers() {
    ticker := time.NewTicker(30 * time.Second)
    for range ticker.C {
        containers := getRealContainersGo()
        for _, c := range containers {
            id := c["id"].(string)

            // 1. Check resource usage
            stats := getContainerStats(id)
            if stats.CPUPercent > 95 {
                shieldLog("docker", "medium", "", "", map[string]interface{}{
                    "type": "resource_abuse", "container": id,
                    "cpu": stats.CPUPercent,
                })
            }

            // 2. Check network connections (outbound)
            conns := getContainerConnections(id)
            for _, conn := range conns {
                if isInternalIP(conn.RemoteAddr) && conn.RemotePort == 22 {
                    shieldLog("docker", "critical", "", "", map[string]interface{}{
                        "type": "suspicious_connection",
                        "container": id,
                        "target": conn.RemoteAddr + ":22",
                    })
                }
            }

            // 3. Check for new processes
            procs := getContainerProcesses(id)
            for _, p := range procs {
                if isSuspiciousProcess(p) {
                    shieldLog("docker", "high", "", "", map[string]interface{}{
                        "type": "suspicious_process",
                        "container": id,
                        "process": p.Command,
                    })
                }
            }
        }
    }
}

func isSuspiciousProcess(p Process) bool {
    suspicious := []string{
        "nc ", "ncat", "netcat", "nmap", "curl.*evil",
        "/bin/sh -i", "python.*-c.*import socket",
        "perl.*socket", "ruby.*socket", "wget.*-O-",
    }
    for _, pattern := range suspicious {
        if matched, _ := regexp.MatchString(pattern, p.Command); matched {
            return true
        }
    }
    return false
}
```

---

## 7. Modos de Protección

### 7.1 Off
NimShield desactivado. Solo logging básico. L1 y L2 no se tocan.

### 7.2 Normal (default)
- L1: Sysctl hardening applied
- L2: nftables basic rules (rate limit, SYN protection)
- L3: Rule engine ON (AUTH-*, TRAV-*, INJ-*). Behavioral baseline learning
- Docker: seccomp default, no-new-privileges, cap-drop
- Notifications: high+ only

### 7.3 Strict
- L1: Sysctl + seccomp profiles + AppArmor (si disponible)
- L2: nftables full (rate limit + GeoIP + container egress control)
- L3: All rules ON + behavioral anomaly detection active + session binding
- Docker: seccomp strict, read-only rootfs, pids-limit, memory-limit
- Notifications: medium+
- eBPF probes if kernel supports

### 7.4 Lockdown
- L1: Everything + eBPF mandatory (fail if not available)
- L2: nftables deny-all except whitelist
- L3: All sessions killed except current admin. Only whitelisted IPs
- Docker: All containers paused. Admin confirmation to resume
- All remote access methods disabled except SSH from whitelist
- Recovery: physical access, local console, or `.shield-disable` file

---

## 8. Auto-Pentest Integrado

NimShield incluye un self-test que el admin puede ejecutar desde el UI:

```go
func runAutopentest() PentestReport {
    report := PentestReport{StartedAt: time.Now()}

    tests := []PentestTest{
        // Network exposure
        {"open_ports", checkOpenPorts},
        {"tls_config", checkTLSConfiguration},
        {"hsts_header", checkHSTSHeader},
        {"csp_header", checkCSPHeader},

        // Auth
        {"default_credentials", checkDefaultCredentials},
        {"session_expiry", checkSessionExpiry},
        {"2fa_available", check2FAAvailable},
        {"password_policy", checkPasswordPolicy},

        // Firewall
        {"firewall_active", checkFirewallActive},
        {"nimshield_active", checkNimShieldActive},
        {"ssh_hardened", checkSSHHardened},

        // Docker
        {"docker_socket_protected", checkDockerSocket},
        {"containers_hardened", checkContainerHardening},
        {"no_privileged_containers", checkNoPrivileged},

        // Kernel
        {"sysctl_hardened", checkSysctlParams},
        {"aslr_enabled", checkASLR},
        {"seccomp_available", checkSeccomp},

        // Data
        {"db_permissions", checkDBPermissions},
        {"config_permissions", checkConfigPermissions},
        {"pool_encryption", checkPoolEncryption},

        // Updates
        {"system_updated", checkSystemUpToDate},
        {"docker_images_updated", checkDockerImagesAge},
    }

    for _, t := range tests {
        result := t.Fn()
        report.Results = append(report.Results, result)
    }

    report.Score = calculateSecurityScore(report.Results)
    report.FinishedAt = time.Now()
    return report
}
```

El resultado es un "Security Score" de 0-100 con recomendaciones accionables.

---

## 9. Implementación por Fases (revisado)

### Fase 1 — Foundation (Beta 5.1)
- `shield.go`: Collector, DB tables, ring buffer, block/unblock
- `shield_rules.go`: AUTH-001/002, TRAV-001, INJ-002
- Middleware integration: check blocks, instrument events
- Sysctl hardening (automated)
- API: status, events, blocks, config
- UI: Dashboard básico + blocked IPs table
- **Entregable**: El NAS detecta y bloquea ataques obvios

### Fase 2 — Firewall + Docker Hardening (Beta 5.2)
- `shield_firewall.go`: nftables integration con fallback
- `shield_docker.go`: seccomp profiles, compose sanitizer, cap-drop
- Container network policies (standard/isolated/none)
- All 18 rules active
- API: rules management, threats
- UI: Rules panel, Docker security settings
- **Entregable**: Defense in depth L1+L2+L3 funcional

### Fase 3 — Intelligence (Beta 5.3)
- `shield_analyzer.go`: Behavioral baseline, anomaly detection
- `shield_notifier.go`: Email, webhook, desktop push
- `shield_geo.go`: Offline GeoIP + nftables integration
- Session binding + slow-rate detection
- Threat scoring con baseline
- Live monitor SSE
- Telemetry dashboard
- **Entregable**: El NAS aprende y se adapta

### Fase 4 — Hardening Total (Beta 5.4)
- `shield_ebpf.go`: eBPF probes para kernel monitoring
- AppArmor profile generation
- Auto-pentest integrado
- Container runtime monitoring
- Lockdown mode con whitelist
- Certificate transparency monitoring
- Security Score dashboard
- Export/import config
- **Entregable**: Hardened como un bunker

---

## 10. Métricas de Éxito

NimShield se considera exitoso cuando:

1. El pentest-v2 (218 tests) pasa con 0 FAIL, 0 CRIT
2. Un ataque de brute force se bloquea en <10 segundos
3. Un port scan se detecta en <30 segundos
4. Un atacante lento (1 req/10min) se detecta en <4 horas
5. Un container escape attempt se mata en <1 segundo (seccomp)
6. El overhead es <2% CPU y <30MB RAM en Raspberry Pi 4
7. Un admin bloqueado puede recuperar acceso local en <2 minutos
8. El Security Score de una instalación default es >70/100
9. Zero false-positive blocks en uso normal durante 30 días

---

## 11. Diferenciadores vs Competencia (actualizado)

| Feature | Synology DSM | TrueNAS | NimOS + NimShield |
|---------|-------------|---------|-------------------|
| Rate limit login | ✅ Básico | ❌ | ✅ Per-IP + per-user |
| Auto-block IPs | ✅ fail2ban | ❌ | ✅ nftables + app |
| Kernel hardening | Parcial | ❌ | ✅ sysctl + seccomp + AppArmor |
| Network firewall | UFW/iptables | ❌ | ✅ nftables dedicado |
| Container sandboxing | ❌ | N/A | ✅ seccomp + caps + read-only |
| Compose sanitization | ❌ | N/A | ✅ Pre-deploy analysis |
| eBPF monitoring | ❌ | ❌ | ✅ Opcional |
| Anomaly detection | ❌ | ❌ | ✅ Behavioral baseline |
| Slow-rate attack detect | ❌ | ❌ | ✅ 24h accumulator |
| Session binding | ❌ | ❌ | ✅ IP+UA fingerprint |
| Threat scoring | ❌ | ❌ | ✅ Multi-signal |
| GeoIP blocking | ✅ Addon | ❌ | ✅ nftables native |
| Real-time monitor | ❌ | ❌ | ✅ SSE stream |
| Auto-pentest | ❌ | ❌ | ✅ Integrado |
| Security Score | ❌ | ❌ | ✅ 0-100 |
| Defense layers | 1 (app) | 0-1 | 3 (kernel+net+app) |
| Container net policy | ❌ | N/A | ✅ Per-container |

---

*NimShield v2 — Three walls between the attacker and your data.*
