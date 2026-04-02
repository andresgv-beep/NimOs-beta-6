# NimOS Beta 6

NimOS is a custom NAS operating system built from scratch with a Go daemon backend and SvelteKit frontend. Designed for personal use on bare-metal Linux machines.

![NimOS Desktop](docs/screenshots/screenshot-desktop.png)

---

## Install

```bash
curl -fsSL https://raw.githubusercontent.com/andresgv-beep/NimOs-beta-6/main/install.sh | sudo bash
```

## Update

From the UI: **Settings → Updates → Apply update**

Or manually:

```bash
cd /opt/nimbusos
bash scripts/update.sh
```

---

## Screenshots

### Desktop & Widgets

![Desktop with widgets](docs/screenshots/screenshot-widgets.png)

Live desktop widgets showing CPU, RAM, network traffic and storage usage at a glance.

### App Launcher

![App launcher](docs/screenshots/screenshot-launcher.png)

### Storage Management

![Storage — Disks](docs/screenshots/screenshot-storage.png)

Visual disk layout with HDD/SSD and NVMe slots. Real-time health indicators.

### Shared Folders

![Shared folders with detail panel](docs/screenshots/screenshot-shares.png)

Per-folder usage donut, quota info, mountpoint, pool type and file type breakdown.

### Security — 2FA

![2FA setup](docs/screenshots/screenshot-2fa.png)

TOTP-based two-factor authentication via Google Authenticator, Authy or any compatible app.

---

## Architecture

- **Backend**: Go daemon (~17 files, ~10 MB binary, ~2 MB RAM)
- **Frontend**: SvelteKit (compiled static files served by the daemon)
- **Reverse proxy**: Nginx (HTTPS with Let's Encrypt)
- **Torrent**: C++ daemon using libtorrent-rasterbar
- **No external dependencies**: No Docker required for core functionality

---

## What works (production-ready)

### Storage engine (rewritten from scratch in Beta 6)

- **ZFS pools**: Create, destroy, mount on boot — mirror / raidz1 / raidz2 / stripe
- **BTRFS pools**: Create, destroy, mount on boot (fstab) — single / raid1 / raid10
- **Disk wipe**: TrueNAS-style wipe with verification — zeros start+end, sgdisk, wipefs, partition-level cleanup, 3-attempt retry
- **Pre-flight safety**: Boot disk protection, kernel holder check, serial verification
- **ZFS snapshots**: Create, list, delete, rollback (UI included)
- **ZFS scrub**: Start, progress monitoring (UI included)
- **ZFS datasets**: Create, list, delete with quotas
- **BTRFS subvolumes**: Per-share subvolumes with qgroup quotas
- **Quota management**: Set and change quotas live on both ZFS and BTRFS
- **Journal**: Atomic writes (tmp+fsync+rename), phase tracking (started/completed), crash recovery
- **Operations engine**: Step-based with rollback, error policies (FailFast/Continue/Ignore), exclusive mutex lock

### Shared folders

- Create shared folders as ZFS datasets or BTRFS subvolumes (not plain mkdir)
- Per-folder quota with live adjustment
- File stats by category (video / image / audio / document / other)
- Usage monitoring with donut chart in UI
- User permissions (read-only / read-write / no access)
- SMB, NFS, FTP protocol tags

### Networking

- DDNS support (DuckDNS, No-IP, Dynu, FreeDNS)
- HTTPS with Let's Encrypt (auto-renewal)
- Reverse proxy for Docker apps
- Port exposure management
- Remote access portal

### Security

- 2FA/TOTP authentication
- User management with roles (admin / user)
- App permissions system (user × app access grid)
- Session-based auth with Bearer tokens
- Passed external pentest: **0 CRIT / 0 FAIL / 61 PASS** from internet

### Apps & services

- **File Manager**: Browse, upload, download files from shares
- **NimTorrent**: Native C++ BitTorrent client — downloads only to pool shares (no system disk access)
- **Media Player**: Built-in media playback
- **Docker Apps**: Install via App Store, run in iframes with reverse proxy
- **Notes**: Simple note-taking app

### System

- **OTA Updates**: Push to GitHub repo → one-click update from UI (auto-rebuild Go + frontend)
- **Hardware monitoring**: CPU, RAM, disk, temperature, GPU (NVIDIA/AMD)
- **System panel**: Service management, resource monitoring

---

## What's in progress

### Storage (functional but needs polish)

- Orphan directory cleanup (works on destroy, not on startup)
- BTRFS snapshot scheduler (ZFS manual snapshots work)
- Expand pool (add disk to existing pool)
- Import external ZFS pool
- Hardware vs config reconciliation at boot (FOREIGN/MISSING/OK states)
- SMART disk monitoring

### NimTorrent

- Auto-update download_dir when pools change
- Speed optimization (was limited by wrong pool paths before — now fixed)

### NimBackup

- UI shell exists, backend not implemented
- Planned: device pairing, remote folder sync, scheduled backups

---

## Hardware tested on

- **NAS**: Z370 AORUS Ultra Gaming, 15 GB RAM, Intel CPU
  - sda: Toshiba 1.8 TB SATA (HDD)
  - sdb: Seagate 3.6 TB SATA (HDD)
  - sdc: 447 GB SSD (boot)
- **Target platforms**: x86_64 bare-metal, Raspberry Pi (BTRFS only — ZFS needs more RAM)

---

## Project structure

```
NimOs-beta-6/
├── daemon/                       # Go backend
│   ├── main.go                   # Entry point, startup sequence
│   ├── storage_stubs.go          # Storage config, detection, routing, health
│   ├── storage_wipe.go           # Disk wipe with journal + verification
│   ├── storage_pools.go          # Pool create/destroy (ZFS + BTRFS)
│   ├── storage_zfs_features.go   # Snapshots, scrub, datasets
│   ├── shares.go                 # Shared folders with ZFS/BTRFS quota
│   ├── auth.go                   # Authentication, 2FA, sessions
│   ├── files.go                  # File manager operations
│   ├── network.go                # DDNS, HTTPS, reverse proxy
│   ├── docker.go                 # Docker app management
│   ├── hardware.go               # System monitoring, updates
│   ├── http.go                   # HTTP server, CORS, CSP
│   ├── apps.go                   # App store, app installation
│   ├── appproxy.go               # Reverse proxy for Docker apps
│   ├── db.go                     # SQLite database
│   ├── static.go                 # Static file serving
│   └── vms.go                    # VM management (stub)
├── src/lib/                      # SvelteKit frontend
│   ├── apps/
│   │   ├── Settings.svelte       # Main settings panel
│   │   ├── StorageApp.svelte     # Storage app wrapper
│   │   ├── StoragePanel.svelte   # Storage management UI
│   │   ├── FileManager.svelte    # File browser
│   │   ├── NimTorrent.svelte     # Torrent client UI
│   │   └── ...
│   └── components/
│       ├── ShareWizard.svelte    # Share creation wizard with quota
│       └── ...
├── torrentd/                     # C++ torrent daemon
├── scripts/
│   ├── install.sh
│   ├── update.sh
│   ├── uninstall.sh
│   └── nimos-daemon.service
└── package.json
```

---

## Key design decisions

1. **No Docker for core**: Storage, shares, auth, networking — all native Go. Docker only for user-installed apps.
2. **ZFS + BTRFS**: Two filesystem options. ZFS for features (datasets, native quota, snapshots). BTRFS for lightweight setups (Raspberry Pi).
3. **Verification over trust**: Every operation verifies its result. Wipe checks `lsblk`, mount checks `findmnt`, pool create checks `zpool list`. Exit codes are not trusted.
4. **No system disk writes**: Every file operation validates the target is on a mounted pool, not the boot disk.
5. **Crash recovery**: Journal with phase tracking. If the daemon crashes mid-operation, it knows exactly where it stopped.

---

## Credits

- Storage rewrite based on TrueNAS SCALE middleware patterns

