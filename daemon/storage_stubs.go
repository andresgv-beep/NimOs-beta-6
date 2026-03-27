package main

// ═══════════════════════════════════════════════════════════════════════════════
// NimOS Storage — Stubs temporales
// Estos stubs permiten compilar el daemon mientras se reescriben los módulos.
// Se reemplazan uno a uno con la implementación real del plan v2.
// ═══════════════════════════════════════════════════════════════════════════════

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

// ─── Constants ───────────────────────────────────────────────────────────────

const nimbusPoolsDir = "/nimbus/pools"
// storageConfigFile is declared in shares.go

// ─── Global vars ─────────────────────────────────────────────────────────────

var hasBtrfs bool
// hasZfs is declared in hardware.go
var storageAlertsGo []map[string]interface{}

// ─── Config read/write (needed by docker.go, shares.go) ─────────────────────

func getStorageConfigFull() map[string]interface{} {
	data, err := os.ReadFile(storageConfigFile)
	if err != nil {
		return map[string]interface{}{"pools": []interface{}{}, "primaryPool": nil}
	}
	var conf map[string]interface{}
	if json.Unmarshal(data, &conf) != nil {
		return map[string]interface{}{"pools": []interface{}{}, "primaryPool": nil}
	}
	return conf
}

func saveStorageConfigFull(config map[string]interface{}) {
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(storageConfigFile, data, 0644)
}

// ─── Pool queries (needed by various files) ──────────────────────────────────

func hasPoolGo() bool {
	conf := getStorageConfigFull()
	pools, _ := conf["pools"].([]interface{})
	if len(pools) == 0 {
		return false
	}
	// Verify at least one pool is actually mounted
	for _, poolRaw := range pools {
		pm, _ := poolRaw.(map[string]interface{})
		if mp, _ := pm["mountPoint"].(string); mp != "" {
			if isPathOnMountedPool(mp) {
				return true
			}
		}
	}
	return false
}

func getStoragePoolsGo() []map[string]interface{} {
	conf := getStorageConfigFull()
	var pools []map[string]interface{}
	confPools, _ := conf["pools"].([]interface{})
	primaryPool, _ := conf["primaryPool"].(string)

	for _, poolRaw := range confPools {
		poolConf, _ := poolRaw.(map[string]interface{})
		if poolConf == nil {
			continue
		}
		poolType, _ := poolConf["type"].(string)
		switch poolType {
		case "zfs":
			pools = append(pools, getZfsPoolInfo(poolConf, primaryPool))
		case "btrfs":
			pools = append(pools, getBtrfsPoolInfo(poolConf, primaryPool))
		}
	}
	if pools == nil {
		pools = []map[string]interface{}{}
	}
	return pools
}

// ─── JSON helpers (used across storage) ──────────────────────────────────────

func jsonToInt64(v interface{}) int64 {
	switch val := v.(type) {
	case float64:
		return int64(val)
	case string:
		return parseInt64(val)
	case json.Number:
		n, _ := val.Int64()
		return n
	}
	return 0
}

func jsonToBool(v interface{}) bool {
	switch val := v.(type) {
	case bool:
		return val
	case string:
		return val == "1" || val == "true"
	case float64:
		return val == 1
	}
	return false
}

// ─── Startup functions (called from main.go) ────────────────────────────────

func zfsAutoImportOnStartup() {
	if !hasZfs {
		return
	}
	// Import all known ZFS pools
	run("zpool import -a -N 2>/dev/null || true")

	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	for _, poolRaw := range confPools {
		pm, _ := poolRaw.(map[string]interface{})
		poolType, _ := pm["type"].(string)
		if poolType != "zfs" {
			continue
		}
		zpoolName, _ := pm["zpoolName"].(string)
		mountPoint, _ := pm["mountPoint"].(string)
		if zpoolName == "" || mountPoint == "" {
			continue
		}
		// Check if pool is imported
		if out, _ := run(fmt.Sprintf("zpool list -H -o name %s 2>/dev/null", zpoolName)); strings.TrimSpace(out) == "" {
			run(fmt.Sprintf("zpool import %s 2>/dev/null || true", zpoolName))
		}
		// Set mount point and mount
		run(fmt.Sprintf("zfs set mountpoint=%s %s 2>/dev/null", mountPoint, zpoolName))
		run("zfs mount -a 2>/dev/null || true")
	}
	logMsg("ZFS auto-import completed")
}

func btrfsAutoMountOnStartup() {
	if !hasBtrfs {
		return
	}
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	for _, poolRaw := range confPools {
		pm, _ := poolRaw.(map[string]interface{})
		poolType, _ := pm["type"].(string)
		if poolType != "btrfs" {
			continue
		}
		mountPoint, _ := pm["mountPoint"].(string)
		if mountPoint == "" {
			continue
		}
		// Try mount from fstab
		run(fmt.Sprintf("mount %s 2>/dev/null || true", mountPoint))
	}
	logMsg("Btrfs auto-mount completed")
}

func startupStorage() {
	logMsg("startup: Storage initialization...")
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	if len(confPools) == 0 {
		logMsg("startup: No pools configured")
		return
	}
	// Verify pools are mounted and create dirs if needed
	for _, poolRaw := range confPools {
		pm, _ := poolRaw.(map[string]interface{})
		mountPoint, _ := pm["mountPoint"].(string)
		poolName, _ := pm["name"].(string)
		if mountPoint == "" {
			continue
		}
		if isPathOnMountedPool(mountPoint) {
			logMsg("startup: Pool '%s' mounted at %s", poolName, mountPoint)
			createPoolDirs(mountPoint)
		} else {
			logMsg("startup: WARNING — Pool '%s' NOT mounted at %s", poolName, mountPoint)
		}
	}
	logMsg("startup: Storage initialization complete")
}

func startStorageMonitoring() {
	go func() {
		for {
			time.Sleep(5 * time.Minute)
			checkStorageHealthGo()
		}
	}()
}

func startZfsScheduler() {
	// TODO: reimplement with new storage_health.go
	logMsg("ZFS scheduler: stub (pending rewrite)")
}

// ─── Detection (called from hardware.go) ─────────────────────────────────────

func detectBtrfs() {
	if _, ok := run("which mkfs.btrfs 2>/dev/null"); ok {
		hasBtrfs = true
		logMsg("Btrfs: available")
	} else {
		logMsg("Btrfs: not available")
	}
}

// ─── Disk detection ──────────────────────────────────────────────────────────

func detectStorageDisksGo() map[string]interface{} {
	// TODO: rewrite with storage_disks.go from plan v2
	// For now: minimal implementation that works
	result := map[string]interface{}{
		"eligible":    []interface{}{},
		"nvme":        []interface{}{},
		"usb":         []interface{}{},
		"provisioned": []interface{}{},
	}

	lsblkRaw, ok := run("lsblk -J -b -o NAME,SIZE,TYPE,ROTA,MOUNTPOINT,MODEL,SERIAL,TRAN,RM,FSTYPE,LABEL,PKNAME 2>/dev/null")
	if !ok || lsblkRaw == "" {
		return result
	}

	var data struct {
		BlockDevices []json.RawMessage `json:"blockdevices"`
	}
	if json.Unmarshal([]byte(lsblkRaw), &data) != nil {
		return result
	}

	rootDisk := findRootDiskGo(lsblkRaw)
	confPools := getStorageConfigFull()
	poolDisks := map[string]bool{}
	if pools, ok := confPools["pools"].([]interface{}); ok {
		for _, p := range pools {
			pm, _ := p.(map[string]interface{})
			if disks, ok := pm["disks"].([]interface{}); ok {
				for _, d := range disks {
					if ds, _ := d.(string); ds != "" {
						poolDisks[ds] = true
					}
				}
			}
		}
	}

	var eligible, nvme, usb, provisioned []interface{}

	for _, raw := range data.BlockDevices {
		var dev map[string]interface{}
		json.Unmarshal(raw, &dev)

		devType, _ := dev["type"].(string)
		if devType != "disk" {
			continue
		}
		devName, _ := dev["name"].(string)

		// Whitelist: only sd*, nvme*, vd*
		validPrefix := false
		for _, prefix := range []string{"sd", "nvme", "vd"} {
			if strings.HasPrefix(devName, prefix) {
				validPrefix = true
				break
			}
		}
		if !validPrefix {
			continue
		}

		size := jsonToInt64(dev["size"])
		if size < 1024*1024*1024 { // < 1GB
			continue
		}

		transport, _ := dev["tran"].(string)
		model, _ := dev["model"].(string)
		serial, _ := dev["serial"].(string)
		rotaBool := jsonToBool(dev["rota"])
		removableBool := jsonToBool(dev["rm"])

		diskInfo := map[string]interface{}{
			"name":          devName,
			"path":          "/dev/" + devName,
			"model":         strings.TrimSpace(model),
			"serial":        strings.TrimSpace(serial),
			"size":          size,
			"sizeFormatted": formatBytes(size),
			"transport":     transport,
			"rotational":    rotaBool,
			"removable":     removableBool,
			"isBoot":        devName == rootDisk,
			"partitions":    []interface{}{},
		}

		// Parse partitions
		var partitions []interface{}
		if children, ok := dev["children"].([]interface{}); ok {
			for _, child := range children {
				cm, ok := child.(map[string]interface{})
				if !ok {
					continue
				}
				partSize := jsonToInt64(cm["size"])
				partitions = append(partitions, map[string]interface{}{
					"name":       cm["name"],
					"path":       "/dev/" + fmt.Sprintf("%v", cm["name"]),
					"size":       partSize,
					"fstype":     cm["fstype"],
					"label":      cm["label"],
					"mountpoint": cm["mountpoint"],
				})
			}
		}
		if partitions == nil {
			partitions = []interface{}{}
		}
		diskInfo["partitions"] = partitions
		diskInfo["hasExistingData"] = len(partitions) > 0

		// Classify
		if devName == rootDisk {
			continue // boot disk — never show
		}

		if poolDisks["/dev/"+devName] {
			diskInfo["classification"] = "provisioned"
			provisioned = append(provisioned, diskInfo)
			continue
		}

		// USB pendrive: USB + removable + < 10GB
		if transport == "usb" && removableBool && size < 10*1024*1024*1024 {
			diskInfo["classification"] = "usb"
			usb = append(usb, diskInfo)
			continue
		}

		// NVMe that isn't boot
		if strings.HasPrefix(devName, "nvme") {
			diskInfo["classification"] = "nvme"
			nvme = append(nvme, diskInfo)
			continue
		}

		// Everything else is eligible
		diskInfo["classification"] = "eligible"
		eligible = append(eligible, diskInfo)
	}

	if eligible == nil { eligible = []interface{}{} }
	if nvme == nil { nvme = []interface{}{} }
	if usb == nil { usb = []interface{}{} }
	if provisioned == nil { provisioned = []interface{}{} }

	result["eligible"] = eligible
	result["nvme"] = nvme
	result["usb"] = usb
	result["provisioned"] = provisioned
	return result
}

func findRootDiskGo(lsblkJSON string) string {
	var data struct {
		BlockDevices []struct {
			Name     string `json:"name"`
			Children []struct {
				Mountpoint interface{} `json:"mountpoint"`
			} `json:"children"`
			Mountpoint interface{} `json:"mountpoint"`
		} `json:"blockdevices"`
	}
	json.Unmarshal([]byte(lsblkJSON), &data)
	for _, dev := range data.BlockDevices {
		for _, child := range dev.Children {
			if mp, _ := child.Mountpoint.(string); mp == "/" {
				return dev.Name
			}
		}
		if mp, _ := dev.Mountpoint.(string); mp == "/" {
			return dev.Name
		}
	}
	return ""
}

// ─── Pool dirs ───────────────────────────────────────────────────────────────

func createPoolDirs(mountPoint string) {
	dirs := []string{"shares", "system-backup/config", "system-backup/snapshots"}
	for _, d := range dirs {
		os.MkdirAll(filepath.Join(mountPoint, d), 0755)
	}
}

// ─── Health ──────────────────────────────────────────────────────────────────

func checkStorageHealthGo() []map[string]interface{} {
	var alerts []map[string]interface{}
	pools := getStoragePoolsGo()
	for _, pool := range pools {
		pct, _ := pool["usagePercent"].(int)
		name, _ := pool["name"].(string)
		if pct >= 95 {
			alerts = append(alerts, map[string]interface{}{"severity": "critical", "pool": name, "message": fmt.Sprintf("Pool %s is %d%% full", name, pct)})
		} else if pct >= 85 {
			alerts = append(alerts, map[string]interface{}{"severity": "warning", "pool": name, "message": fmt.Sprintf("Pool %s is %d%% full", name, pct)})
		}
	}
	if alerts == nil { alerts = []map[string]interface{}{} }
	storageAlertsGo = alerts
	return alerts
}

// ─── Wipe (implemented in storage_wipe.go) ──────────────────────────────────

// ─── Scan / Restore (stubs) ──────────────────────────────────────────────────

func rescanSCSIBuses() {
	entries, err := os.ReadDir("/sys/class/scsi_host")
	if err != nil {
		return
	}
	for _, e := range entries {
		scanPath := filepath.Join("/sys/class/scsi_host", e.Name(), "scan")
		os.WriteFile(scanPath, []byte("- - -"), 0200)
	}
	run("udevadm settle --timeout=5 2>/dev/null || true")
}

func scanForRestorablePoolsGo() []map[string]interface{} {
	return []map[string]interface{}{}
}

func restorePoolGo(device, poolName string) map[string]interface{} {
	return map[string]interface{}{"error": "Restore not yet reimplemented"}
}

func backupConfigToPoolGo() {
	// TODO: reimplement
}

func runExec(name string, args ...string) {
	cmd := exec.Command(name, args...)
	cmd.Run()
}

func appendFstab(uuid, mountPoint, filesystem string) {
	existing, _ := os.ReadFile("/etc/fstab")
	if strings.Contains(string(existing), mountPoint) {
		return
	}
	opts := "defaults,nofail,noatime"
	if filesystem == "btrfs" {
		opts = "defaults,nofail,noatime,compress=zstd"
	}
	entry := fmt.Sprintf("UUID=%s %s %s %s 0 2\n", uuid, mountPoint, filesystem, opts)
	f, err := os.OpenFile("/etc/fstab", os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return
	}
	defer f.Close()
	f.WriteString(entry)
	log.Printf("appendFstab: added %s", mountPoint)
}

// ─── ZFS Pool Info (needed by getStoragePoolsGo) ────────────────────────────

func getZfsPoolInfo(poolConf map[string]interface{}, primaryPool string) map[string]interface{} {
	poolName, _ := poolConf["name"].(string)
	zpoolName, _ := poolConf["zpoolName"].(string)
	mountPoint, _ := poolConf["mountPoint"].(string)
	vdevType, _ := poolConf["vdevType"].(string)
	createdAt, _ := poolConf["createdAt"].(string)

	if zpoolName == "" {
		zpoolName = "nimos-" + poolName
	}

	// Get status from zpool
	total, used, available := int64(0), int64(0), int64(0)
	poolStatus := "offline"
	health := "UNKNOWN"

	out, ok := run(fmt.Sprintf("zpool list -Hp -o name,size,alloc,free,health %s 2>/dev/null", zpoolName))
	if ok && out != "" {
		parts := strings.Fields(strings.TrimSpace(out))
		if len(parts) >= 5 {
			total = parseInt64(parts[1])
			used = parseInt64(parts[2])
			available = parseInt64(parts[3])
			health = parts[4]
			switch strings.ToUpper(health) {
			case "ONLINE":
				poolStatus = "active"
			case "DEGRADED":
				poolStatus = "degraded"
			case "FAULTED":
				poolStatus = "faulted"
			default:
				poolStatus = strings.ToLower(health)
			}
		}
	}

	var disks []interface{}
	if d, ok := poolConf["disks"].([]interface{}); ok {
		disks = d
	}
	if disks == nil {
		disks = []interface{}{}
	}

	usagePct := 0
	if total > 0 {
		usagePct = int(float64(used) / float64(total) * 100)
	}

	return map[string]interface{}{
		"name":               poolName,
		"type":               "zfs",
		"zpoolName":          zpoolName,
		"mountPoint":         mountPoint,
		"raidLevel":          vdevType,
		"vdevType":           vdevType,
		"filesystem":         "zfs",
		"createdAt":          createdAt,
		"disks":              disks,
		"status":             poolStatus,
		"health":             health,
		"total":              total,
		"used":               used,
		"available":          available,
		"totalFormatted":     formatBytes(total),
		"usedFormatted":      formatBytes(used),
		"availableFormatted": formatBytes(available),
		"usagePercent":       usagePct,
		"isPrimary":          poolName == primaryPool,
	}
}

// ─── BTRFS Pool Info (needed by getStoragePoolsGo) ──────────────────────────

func getBtrfsPoolInfo(poolConf map[string]interface{}, primaryPool string) map[string]interface{} {
	poolName, _ := poolConf["name"].(string)
	mountPoint, _ := poolConf["mountPoint"].(string)
	profile, _ := poolConf["profile"].(string)
	createdAt, _ := poolConf["createdAt"].(string)

	total, used, available := int64(0), int64(0), int64(0)
	poolStatus := "offline"

	// Check if mounted
	mountSrc, _ := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint))
	if strings.TrimSpace(mountSrc) != "" {
		rootSrc, _ := run("findmnt -n -o SOURCE / 2>/dev/null")
		if strings.TrimSpace(mountSrc) != strings.TrimSpace(rootSrc) {
			poolStatus = "active"
			if dfOut, ok := run(fmt.Sprintf("df -B1 --output=size,used,avail %s 2>/dev/null", mountPoint)); ok {
				lines := strings.Split(strings.TrimSpace(dfOut), "\n")
				if len(lines) > 1 {
					parts := strings.Fields(lines[1])
					if len(parts) >= 3 {
						total = parseInt64(parts[0])
						used = parseInt64(parts[1])
						available = parseInt64(parts[2])
					}
				}
			}
		}
	}

	var disks []interface{}
	if d, ok := poolConf["disks"].([]interface{}); ok {
		disks = d
	}
	if disks == nil {
		disks = []interface{}{}
	}

	usagePct := 0
	if total > 0 {
		usagePct = int(float64(used) / float64(total) * 100)
	}

	return map[string]interface{}{
		"name":               poolName,
		"type":               "btrfs",
		"profile":            profile,
		"mountPoint":         mountPoint,
		"raidLevel":          profile,
		"filesystem":         "btrfs",
		"createdAt":          createdAt,
		"disks":              disks,
		"status":             poolStatus,
		"total":              total,
		"used":               used,
		"available":          available,
		"totalFormatted":     formatBytes(total),
		"usedFormatted":      formatBytes(used),
		"availableFormatted": formatBytes(available),
		"usagePercent":       usagePct,
		"isPrimary":          poolName == primaryPool,
	}
}

// ─── HTTP Routes (called from http.go) ───────────────────────────────────────

func handleStorageRoutes(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path
	method := r.Method

	if method == "GET" {
		session := requireAdmin(w, r)
		if session == nil {
			return
		}
		switch urlPath {
		case "/api/storage", "/api/storage/pools":
			jsonOk(w, getStoragePoolsGo())
		case "/api/storage/disks":
			jsonOk(w, detectStorageDisksGo())
		case "/api/storage/status":
			pools := getStoragePoolsGo()
			mountedCount := 0
			for _, p := range pools {
				if s, _ := p["status"].(string); s == "active" || s == "degraded" {
					mountedCount++
				}
			}
			jsonOk(w, map[string]interface{}{
				"pools":        pools,
				"alerts":       storageAlertsGo,
				"hasPool":      hasPoolGo(),
				"mountedPools": mountedCount,
				"totalPools":   len(pools),
			})
		case "/api/storage/alerts":
			jsonOk(w, map[string]interface{}{"alerts": storageAlertsGo})
		case "/api/storage/capabilities":
			jsonOk(w, map[string]interface{}{
				"zfs":   hasZfs,
				"btrfs": hasBtrfs,
				"arch":  systemArch,
				"ramGB": systemRamGB,
			})
		case "/api/storage/health":
			jsonOk(w, checkStorageHealthGo())
		case "/api/storage/restorable":
			jsonOk(w, map[string]interface{}{"pools": scanForRestorablePoolsGo()})
		case "/api/storage/snapshots":
			pool := r.URL.Query().Get("pool")
			jsonOk(w, listSnapshots(pool))
		case "/api/storage/scrub/status":
			pool := r.URL.Query().Get("pool")
			jsonOk(w, getScrubStatus(pool))
		case "/api/storage/datasets":
			pool := r.URL.Query().Get("pool")
			jsonOk(w, listDatasets(pool))
		default:
			jsonError(w, 404, "Not found")
		}
		return
	}

	if method == "POST" || method == "DELETE" || method == "PUT" {
		session := requireAdmin(w, r)
		if session == nil {
			return
		}
		body, _ := readBody(r)

		switch urlPath {
		case "/api/storage/pool":
			poolType := bodyStr(body, "type")
			if poolType == "zfs" || (hasZfs && poolType == "") {
				jsonOk(w, createPoolZfs(body))
			} else if poolType == "btrfs" && hasBtrfs {
				jsonOk(w, createPoolBtrfs(body))
			} else {
				jsonError(w, 400, "No supported filesystem available")
			}
		case "/api/storage/scan":
			rescanSCSIBuses()
			jsonOk(w, map[string]interface{}{"ok": true, "disks": detectStorageDisksGo()})
		case "/api/storage/wipe":
			disk := bodyStr(body, "disk")
			if disk == "" {
				jsonError(w, 400, "Provide disk path")
			} else {
				jsonOk(w, wipeDiskGo(disk))
			}
		case "/api/storage/pool/destroy":
			poolName := bodyStr(body, "name")
			if poolName == "" {
				jsonError(w, 400, "Provide pool name")
			} else {
				conf := getStorageConfigFull()
				confPools, _ := conf["pools"].([]interface{})
				poolType := ""
				for _, p := range confPools {
					pm, _ := p.(map[string]interface{})
					if n, _ := pm["name"].(string); n == poolName {
						poolType, _ = pm["type"].(string)
						break
					}
				}
				switch poolType {
				case "zfs":
					jsonOk(w, destroyPoolZfs(poolName))
				case "btrfs":
					jsonOk(w, destroyPoolBtrfs(poolName))
				default:
					jsonError(w, 400, fmt.Sprintf("Unknown pool type '%s'", poolType))
				}
			}
		case "/api/storage/pool/restore":
			jsonError(w, 503, "Pool restore pending implementation")
		case "/api/storage/backup":
			backupConfigToPoolGo()
			jsonOk(w, map[string]interface{}{"ok": true})
		case "/api/storage/snapshot":
			if method == "POST" {
				jsonOk(w, createSnapshot(body))
			} else if method == "DELETE" {
				jsonOk(w, deleteSnapshot(body))
			}
		case "/api/storage/snapshot/rollback":
			jsonOk(w, rollbackSnapshot(body))
		case "/api/storage/scrub":
			jsonOk(w, startScrub(body))
		case "/api/storage/dataset":
			if method == "POST" {
				jsonOk(w, createDataset(body))
			} else if method == "DELETE" {
				jsonOk(w, deleteDataset(body))
			}
		default:
			jsonError(w, 404, "Not found")
		}
		return
	}

	jsonError(w, 405, "Method not allowed")
}
