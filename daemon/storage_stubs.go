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
	"strconv"
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

// enrichDisksWithSmart takes a flat disk name list and returns enriched objects
// with SMART status from the cached monitor data. Does NOT run smartctl — only
// reads from smartHistory to avoid false positives from stale or slow queries.
// The pool-level status/health is NEVER modified by this function.
func enrichDisksWithSmart(diskNames []interface{}) []interface{} {
	smartMu.Lock()
	defer smartMu.Unlock()

	enriched := make([]interface{}, 0, len(diskNames))
	for _, d := range diskNames {
		raw, _ := d.(string)
		if raw == "" {
			continue
		}

		// Strip /dev/ prefix — config stores "/dev/sda", smartHistory uses "sda"
		name := strings.TrimPrefix(raw, "/dev/")

		// Check if disk physically exists
		model := ""
		sizeStr := ""
		diskExists := false
		if out, ok := run(fmt.Sprintf("lsblk -d -n -o MODEL,SIZE /dev/%s 2>/dev/null", name)); ok && out != "" {
			diskExists = true
			parts := strings.Fields(strings.TrimSpace(out))
			if len(parts) >= 2 {
				sizeStr = parts[len(parts)-1]
				model = strings.Join(parts[:len(parts)-1], " ")
			} else if len(parts) == 1 {
				sizeStr = parts[0]
			}
		}

		// Determine status
		smartStatus := "unknown"
		if !diskExists {
			smartStatus = "missing"
		} else if s, ok := smartHistory[name]; ok {
			smartStatus = s
		}

		enriched = append(enriched, map[string]interface{}{
			"name":        name,
			"model":       model,
			"size":        sizeStr,
			"smartStatus": smartStatus, // "ok" | "warning" | "critical" | "missing" | "unknown"
		})
	}
	return enriched
}

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

	var rawDisks []interface{}
	if d, ok := poolConf["disks"].([]interface{}); ok {
		rawDisks = d
	}
	if rawDisks == nil {
		rawDisks = []interface{}{}
	}
	disks := enrichDisksWithSmart(rawDisks)

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

	var rawDisks []interface{}
	if d, ok := poolConf["disks"].([]interface{}); ok {
		rawDisks = d
	}
	if rawDisks == nil {
		rawDisks = []interface{}{}
	}
	disks := enrichDisksWithSmart(rawDisks)

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
		case "/api/storage/resilver/status":
			pool := r.URL.Query().Get("pool")
			jsonOk(w, getResilverStatus(pool))
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
		case "/api/storage/pool/replace-disk":
			jsonOk(w, handleReplaceDisk(body))
		case "/api/storage/pool/resilver-status":
			poolName := bodyStr(body, "pool")
			if poolName == "" {
				jsonError(w, 400, "Provide pool name")
			} else {
				jsonOk(w, getResilverStatus(poolName))
			}
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

// ═══════════════════════════════════════════════════════════════════════════════
// Disk Replacement — Replace a disk in a ZFS or BTRFS pool
// ═══════════════════════════════════════════════════════════════════════════════

// findPoolConfig returns the pool config map by pool name
func findPoolConfig(poolName string) (map[string]interface{}, string) {
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	for _, p := range confPools {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == poolName {
			poolType, _ := pm["type"].(string)
			return pm, poolType
		}
	}
	return nil, ""
}

// POST /api/storage/pool/replace-disk
// Body: { pool: "valume1", oldDisk: "sdb", newDisk: "sdc" }
func handleReplaceDisk(body map[string]interface{}) map[string]interface{} {
	poolName := bodyStr(body, "pool")
	oldDisk := bodyStr(body, "oldDisk")
	newDisk := bodyStr(body, "newDisk")

	if poolName == "" || oldDisk == "" || newDisk == "" {
		return map[string]interface{}{"error": "Missing pool, oldDisk, or newDisk"}
	}

	poolConf, poolType := findPoolConfig(poolName)
	if poolConf == nil {
		return map[string]interface{}{"error": "Pool not found"}
	}

	// Ensure old disk belongs to the pool
	disks, _ := poolConf["disks"].([]interface{})
	found := false
	for _, d := range disks {
		ds, _ := d.(string)
		if strings.TrimPrefix(ds, "/dev/") == oldDisk {
			found = true
			break
		}
	}
	if !found {
		return map[string]interface{}{"error": fmt.Sprintf("Disk %s is not part of pool %s", oldDisk, poolName)}
	}

	// Ensure new disk is not already in a pool
	conf := getStorageConfigFull()
	allPools, _ := conf["pools"].([]interface{})
	for _, p := range allPools {
		pm, _ := p.(map[string]interface{})
		pDisks, _ := pm["disks"].([]interface{})
		for _, d := range pDisks {
			ds, _ := d.(string)
			if strings.TrimPrefix(ds, "/dev/") == newDisk {
				pn, _ := pm["name"].(string)
				return map[string]interface{}{"error": fmt.Sprintf("Disk %s is already in pool %s", newDisk, pn)}
			}
		}
	}

	// Pre-flight check on new disk
	newDiskPath := "/dev/" + newDisk
	if err := preFlightCheck(newDiskPath); err != nil {
		return map[string]interface{}{"error": fmt.Sprintf("New disk %s: %s", newDisk, err.Error())}
	}

	switch poolType {
	case "zfs":
		return replaceDiskZfs(poolConf, oldDisk, newDisk)
	case "btrfs":
		return replaceDiskBtrfs(poolConf, oldDisk, newDisk)
	default:
		return map[string]interface{}{"error": fmt.Sprintf("Unsupported pool type: %s", poolType)}
	}
}

// replaceDiskZfs runs: zpool replace <pool> <old> <new>
func replaceDiskZfs(poolConf map[string]interface{}, oldDisk, newDisk string) map[string]interface{} {
	poolName, _ := poolConf["name"].(string)
	zpoolName, _ := poolConf["zpoolName"].(string)
	if zpoolName == "" {
		zpoolName = "nimos-" + poolName
	}

	newDiskPath := "/dev/" + newDisk
	newPart := partitionName(newDiskPath)
	opts := CmdOptions{Timeout: 60 * time.Second}
	optsShort := CmdOptions{Timeout: 15 * time.Second}

	// Wipe and partition new disk
	runCmd("wipefs", []string{"-a", newDiskPath}, opts)
	runCmd("sgdisk", []string{"-Z", newDiskPath}, optsShort)
	runCmd("sgdisk", []string{"-n", "1:0:0", "-t", "1:BF01", newDiskPath}, opts)
	runCmd("udevadm", []string{"settle", "--timeout=5"}, optsShort)
	time.Sleep(time.Second)
	waitForDevice(newPart, 10*time.Second)

	// Find the old partition in the pool
	oldPart := partitionName("/dev/" + oldDisk)

	// zpool replace — this starts resilver automatically
	res, err := runCmd("zpool", []string{"replace", "-f", zpoolName, oldPart, newPart}, CmdOptions{Timeout: 30 * time.Second})
	if err != nil || !res.OK {
		errMsg := res.Stderr
		if errMsg == "" {
			errMsg = res.Stdout
		}
		return map[string]interface{}{"error": fmt.Sprintf("zpool replace failed: %s", errMsg)}
	}

	// Update config: replace old disk with new
	updatePoolConfigDisk(poolName, oldDisk, newDisk)

	addNotification("info", "system",
		fmt.Sprintf("Reemplazo de disco iniciado en %s", poolName),
		fmt.Sprintf("Reemplazando %s por %s. El resilver puede tardar horas según el tamaño.", oldDisk, newDisk))

	logMsg("DISK REPLACE: pool %s, %s -> %s (ZFS resilver started)", poolName, oldDisk, newDisk)

	return map[string]interface{}{"ok": true, "message": "Resilver started"}
}

// replaceDiskBtrfs runs: btrfs device add + btrfs device delete
func replaceDiskBtrfs(poolConf map[string]interface{}, oldDisk, newDisk string) map[string]interface{} {
	poolName, _ := poolConf["name"].(string)
	mountPoint, _ := poolConf["mountPoint"].(string)

	if mountPoint == "" {
		return map[string]interface{}{"error": "Pool mount point not found"}
	}

	opts := CmdOptions{Timeout: 60 * time.Second}
	newDiskPath := "/dev/" + newDisk
	oldDiskPath := "/dev/" + oldDisk

	// Wipe new disk
	runCmd("wipefs", []string{"-a", newDiskPath}, opts)

	// Add new disk to the filesystem
	res, err := runCmd("btrfs", []string{"device", "add", "-f", newDiskPath, mountPoint}, opts)
	if err != nil || !res.OK {
		errMsg := res.Stderr
		if errMsg == "" {
			errMsg = res.Stdout
		}
		return map[string]interface{}{"error": fmt.Sprintf("btrfs device add failed: %s", errMsg)}
	}

	// Remove old disk — this triggers automatic rebalance
	// Run in background because it can take a very long time
	go func() {
		res, err := runCmd("btrfs", []string{"device", "delete", oldDiskPath, mountPoint}, CmdOptions{Timeout: 0})
		if err == nil && res.OK {
			updatePoolConfigDisk(poolName, oldDisk, newDisk)
			addNotification("success", "system",
				fmt.Sprintf("Disco reemplazado en %s", poolName),
				fmt.Sprintf("Se ha completado el reemplazo de %s por %s.", oldDisk, newDisk))
			logMsg("DISK REPLACE: pool %s, %s -> %s (BTRFS complete)", poolName, oldDisk, newDisk)
		} else {
			errMsg := res.Stderr
			if errMsg == "" && err != nil {
				errMsg = err.Error()
			}
			addNotification("error", "system",
				fmt.Sprintf("Error al reemplazar disco en %s", poolName),
				fmt.Sprintf("No se pudo eliminar %s: %s", oldDisk, errMsg))
			logMsg("DISK REPLACE FAILED: pool %s, btrfs device delete %s: %s", poolName, oldDisk, errMsg)
		}
	}()

	addNotification("info", "system",
		fmt.Sprintf("Reemplazo de disco iniciado en %s", poolName),
		fmt.Sprintf("Añadido %s, eliminando %s. El rebalanceo puede tardar horas.", newDisk, oldDisk))

	logMsg("DISK REPLACE: pool %s, %s -> %s (BTRFS started)", poolName, oldDisk, newDisk)

	return map[string]interface{}{"ok": true, "message": "Disk replacement started"}
}

// updatePoolConfigDisk updates the stored config replacing old disk with new
func updatePoolConfigDisk(poolName, oldDisk, newDisk string) {
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	for _, p := range confPools {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == poolName {
			disks, _ := pm["disks"].([]interface{})
			for i, d := range disks {
				ds, _ := d.(string)
				if strings.TrimPrefix(ds, "/dev/") == oldDisk {
					disks[i] = "/dev/" + newDisk
					break
				}
			}
			pm["disks"] = disks
			break
		}
	}
	conf["pools"] = confPools
	saveStorageConfigFull(conf)
}

// getResilverStatus returns the current resilver/rebuild progress
// GET /api/storage/resilver/status?pool=valume1
func getResilverStatus(poolName string) map[string]interface{} {
	poolConf, poolType := findPoolConfig(poolName)
	if poolConf == nil {
		return map[string]interface{}{"error": "Pool not found", "active": false}
	}

	switch poolType {
	case "zfs":
		zpoolName, _ := poolConf["zpoolName"].(string)
		if zpoolName == "" {
			zpoolName = "nimos-" + poolName
		}
		out, ok := run(fmt.Sprintf("zpool status %s 2>/dev/null", zpoolName))
		if !ok {
			return map[string]interface{}{"active": false, "error": "Cannot read pool status"}
		}

		result := map[string]interface{}{
			"active":   false,
			"progress": 0,
			"eta":      "",
			"speed":    "",
		}

		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			// Look for: "scan: resilver in progress since..."
			if strings.Contains(line, "resilver in progress") {
				result["active"] = true
			}
			// Look for progress line: "X.XXM scanned at Y.YYM/s, Z.ZZM issued at W.WWM/s, 1.82T total"
			if strings.Contains(line, "issued") && strings.Contains(line, "total") {
				result["detail"] = line
			}
			// Look for: "X.XX% done, HH:MM:SS to go"
			if strings.Contains(line, "% done") {
				parts := strings.Fields(line)
				for i, p := range parts {
					if p == "done," && i > 0 {
						pctStr := strings.TrimSuffix(parts[i-1], "%")
						pct, _ := strconv.ParseFloat(pctStr, 64)
						result["progress"] = pct
					}
					if p == "go" && i > 0 {
						result["eta"] = parts[i-1]
					}
				}
			}
		}
		return result

	case "btrfs":
		mountPoint, _ := poolConf["mountPoint"].(string)
		out, ok := run(fmt.Sprintf("btrfs balance status %s 2>/dev/null", mountPoint))
		if !ok {
			return map[string]interface{}{"active": false}
		}
		active := strings.Contains(out, "in progress") || strings.Contains(out, "running")
		result := map[string]interface{}{
			"active": active,
			"detail": strings.TrimSpace(out),
		}
		// Try to extract percentage
		if active {
			for _, line := range strings.Split(out, "\n") {
				if strings.Contains(line, "% done") || strings.Contains(line, "estimated") {
					result["detail"] = strings.TrimSpace(line)
				}
			}
		}
		return result

	default:
		return map[string]interface{}{"active": false, "error": "Unknown pool type"}
	}
}
