# NimOS ZFS — Frontend Implementation Guide (para Sonnet)

## Estado actual
El backend ZFS está completo y funcionando. Falta conectar el frontend.

---

## API: Capabilities (detectar qué mostrar)

```
GET /api/storage/capabilities
→ { zfs: true, mdadm: true, arch: "x86_64", ramGB: 15, recommended: "zfs" }
```

Si `zfs: false` → no mostrar opción ZFS en el formulario de crear pool.
Si `zfs: true` → mostrar ZFS como opción (con badge "Recomendado" si `recommended: "zfs"`).

---

## API: Pools

### Crear pool
```
POST /api/storage/pool
Body ZFS:   { type: "zfs",   name: "volume", vdevType: "mirror", disks: ["/dev/sda","/dev/sdb"] }
Body mdadm: { type: "mdadm", name: "volume", level: "1",         disks: ["/dev/sda","/dev/sdb"], filesystem: "ext4" }
```

vdevType opciones ZFS: `"mirror"` (≥2 discos), `"raidz1"` (≥3), `"raidz2"` (≥4), `"stripe"` (≥1)

### Listar pools (ya funciona, ahora incluye tipo)
```
GET /api/storage/pools
→ [{ name, type: "zfs"|"mdadm", status, mountPoint, raidLevel, vdevType, total, used, available, 
      totalFormatted, usedFormatted, availableFormatted, usagePercent, isPrimary, health, scrub }]
```

### Destruir pool (dispatch automático por tipo)
```
POST /api/storage/pool/destroy
Body: { name: "volume" }
```

### Pools ZFS importables (recovery tras reinstalar)
```
GET /api/storage/zfs/importable
→ { pools: [{ zpoolName: "nimos-volume", status: "ONLINE", nimosName: "volume", isNimosPool: true }] }
```

### Importar pool ZFS
```
POST /api/storage/zfs/import
Body: { zpoolName: "nimos-volume", name: "volume" }  (name es opcional, se auto-detecta)
```

---

## API: Datasets (solo ZFS)

### Listar
```
GET /api/storage/datasets?pool=volume
→ { datasets: [{ name: "shares/documentos", fullName: "nimos-volume/shares/documentos", 
      used: "1.2G", available: "48.8G", quota: "50G", compression: "lz4", mountPoint: "/nimbus/pools/volume/shares/documentos" }] }
```

### Crear
```
POST /api/storage/dataset
Body: { pool: "volume", name: "documentos", quota: "50G", compression: "lz4" }
```
quota: `"50G"`, `"100G"`, `"none"` (ilimitado)
compression: `"lz4"` (default), `"zstd"`, `"off"`

### Editar
```
PUT /api/storage/dataset
Body: { dataset: "nimos-volume/shares/documentos", quota: "100G", compression: "zstd" }
```

### Eliminar
```
DELETE /api/storage/dataset
Body: { dataset: "nimos-volume/shares/documentos" }
```

---

## API: Snapshots (solo ZFS)

### Listar
```
GET /api/storage/snapshots?dataset=nimos-volume/shares/documentos
→ { snapshots: [{ name: "manual-20260316-120000", fullName: "nimos-volume/shares/documentos@manual-20260316-120000",
      dataset: "nimos-volume/shares/documentos", used: "128K", created: "Mon Mar 16 12:00 2026", isAuto: false }] }
```

### Crear snapshot manual
```
POST /api/storage/snapshot
Body: { dataset: "nimos-volume/shares/documentos", name: "pre-update" }
(name opcional, se auto-genera si vacío)
```

### Rollback
```
POST /api/storage/snapshot/rollback
Body: { snapshot: "nimos-volume/shares/documentos@pre-update" }
⚠️ Requiere confirmación en UI: "Esto deshará todos los cambios desde el snapshot"
```

### Borrar snapshot
```
DELETE /api/storage/snapshot
Body: { snapshot: "nimos-volume/shares/documentos@pre-update" }
```

### Configurar snapshots automáticos
```
PUT /api/storage/snapshots/schedule
Body: { enabled: true, schedule: "hourly", retention: { hourly: 24, daily: 30, weekly: 12 } }
```
schedule: `"hourly"`, `"daily"`, `"weekly"`

---

## API: Scrub (solo ZFS)

### Iniciar scrub manual
```
POST /api/storage/scrub
Body: { pool: "volume" }
```

El estado del scrub viene en el pool listing: `pool.scrub = { status: "running"|"completed"|"none", progress: 45.2, lastRun: "...", errors: 0 }`

---

## Cambios requeridos en el Frontend

### 1. Storage Manager → Tab "Storage Manager" (crear pool)
- Llamar a `GET /api/storage/capabilities` al cargar
- Si `zfs: true`: mostrar selector de tipo "ZFS (Recomendado)" / "mdadm (Legacy)"
- Si ZFS seleccionado: opciones vdev → Mirror, RAIDZ1, RAIDZ2, Stripe
- Si mdadm seleccionado: opciones RAID → 0, 1, 5, 6, 10 (como ahora)
- Enviar `type: "zfs"` o no enviar type (default mdadm)

### 2. Pool listing (ya funciona, campo nuevo `type`)
- Mostrar badge "ZFS" o "mdadm" junto al nombre del pool
- Para pools ZFS: mostrar health (ONLINE/DEGRADED) y scrub status

### 3. Tab "Datasets" (NUEVO, solo visible para pools ZFS)
- Lista de datasets con barras de uso
- Crear: nombre + quota (input GB o "Ilimitado") + compresión (lz4/zstd/off)
- Editar inline: quota y compresión
- Eliminar con confirmación

### 4. Tab "Snapshots" (NUEVO, solo visible para pools ZFS)
- Selector de dataset arriba
- Lista de snapshots (nombre, tamaño, fecha, badge auto/manual)
- Botón "Snapshot ahora"
- Botón "Rollback" por snapshot (con modal de confirmación)
- Botón "Eliminar" por snapshot
- Sección de schedule: toggle on/off, selector frecuencia, retención

### 5. Tab "Restore Pool" (extender)
- Llamar a `GET /api/storage/zfs/importable` además del scan actual
- Mostrar pools ZFS detectados con botón "Importar"

### 6. Tab "Health" (extender para ZFS)
- Mostrar scrub status por pool ZFS
- Botón "Iniciar scrub"
- Progreso de scrub en curso

---

## Notas de diseño

- Seguir el mismo estilo visual que las secciones existentes (tokens CSS, DM Sans, glass-morphism)
- Los tabs nuevos (Datasets, Snapshots) solo aparecen si hay al menos un pool ZFS
- Los pools mdadm existentes no se ven afectados — todo sigue funcionando igual
- El campo `pool.type` ("zfs" o "mdadm") determina qué opciones mostrar por pool
