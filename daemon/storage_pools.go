package main

// ═══════════════════════════════════════════════════════════════════════════════
// NimOS Storage — Pool Operations (Plan v2)
// Create, destroy, and manage ZFS and BTRFS pools.
// Based on TrueNAS middleware pool architecture.
// ═══════════════════════════════════════════════════════════════════════════════

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ─── Helpers ─────────────────────────────────────────────────────────────────

// partitionName returns the correct partition 1 name.
// SATA/USB: sda → sda1. NVMe: nvme0n1 → nvme0n1p1.
func partitionName(diskName string) string {
	if strings.HasPrefix(diskName, "nvme") {
		return diskName + "p1"
	}
	return diskName + "1"
}

// waitForDevice waits for a device file to appear in /dev/
func waitForDevice(path string, timeout time.Duration) error {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); err == nil {
			return nil
		}
		time.Sleep(200 * time.Millisecond)
	}
	return fmt.Errorf("timeout waiting for %s", path)
}

// writePoolIdentity writes the .nimbus-pool.json identity file
func writePoolIdentity(mountPoint, name, poolType, vdevType string, disks []string) {
	identity := map[string]interface{}{
		"name":          name,
		"type":          poolType,
		"vdevType":      vdevType,
		"disks":         disks,
		"createdAt":     time.Now().UTC().Format(time.RFC3339),
		"nimbusVersion": "5.0.0-beta",
	}
	data, _ := json.MarshalIndent(identity, "", "  ")
	os.WriteFile(filepath.Join(mountPoint, ".nimbus-pool.json"), data, 0644)
}

// ─── Create Pool ZFS ─────────────────────────────────────────────────────────

func createPoolZfs(body map[string]interface{}) map[string]interface{} {
	name := bodyStr(body, "name")
	vdevType := bodyStr(body, "vdevType")
	if vdevType == "" {
		vdevType = bodyStr(body, "level")
		if vdevType == "" {
			vdevType = bodyStr(body, "profile")
		}
	}

	// Validate name
	if name == "" || !regexp.MustCompile(`^[a-zA-Z0-9-]{1,32}$`).MatchString(name) {
		return map[string]interface{}{"error": "Invalid pool name. Use alphanumeric + hyphens, max 32 chars."}
	}
	reserved := map[string]bool{"system": true, "config": true, "temp": true, "swap": true, "root": true, "boot": true, "rpool": true}
	if reserved[strings.ToLower(name)] {
		return map[string]interface{}{"error": fmt.Sprintf(`"%s" is a reserved name.`, name)}
	}

	// Check if zpool already exists
	if out, _ := run(fmt.Sprintf("zpool list -H -o name %s 2>/dev/null", "nimos-"+name)); strings.TrimSpace(out) != "" {
		return map[string]interface{}{"error": fmt.Sprintf(`ZFS pool "nimos-%s" already exists.`, name)}
	}

	// Check storage.json
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	for _, p := range confPools {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == name {
			return map[string]interface{}{"error": fmt.Sprintf(`Pool "%s" already exists in config.`, name)}
		}
	}

	// Parse disks
	disksRaw, _ := body["disks"].([]interface{})
	if len(disksRaw) < 1 {
		return map[string]interface{}{"error": "At least 1 disk required."}
	}
	var disks []string
	for _, d := range disksRaw {
		if ds, ok := d.(string); ok {
			if !strings.HasPrefix(ds, "/dev/") {
				ds = "/dev/" + ds
			}
			disks = append(disks, ds)
		}
	}

	// Validate vdev type vs disk count
	minDisks := map[string]int{"stripe": 1, "single": 1, "mirror": 2, "raidz1": 3, "raidz2": 4, "raidz3": 5}
	if min, ok := minDisks[vdevType]; ok {
		if len(disks) < min {
			return map[string]interface{}{"error": fmt.Sprintf("%s requires at least %d disks.", vdevType, min)}
		}
	}

	// Pre-flight check on all disks
	for _, d := range disks {
		if err := preFlightCheck(d); err != nil {
			return map[string]interface{}{"error": fmt.Sprintf("Disk %s: %s", d, err.Error())}
		}
	}

	zpoolName := "nimos-" + name
	mountPoint := nimbusPoolsDir + "/" + name
	opts := CmdOptions{Timeout: 60 * time.Second}
	optsShort := CmdOptions{Timeout: 10 * time.Second}

	op := JournalOp{
		ID:   "create-zfs-" + name,
		Type: "create_pool",
		Data: map[string]string{"name": name, "type": "zfs", "vdevType": vdevType},
	}

	// Take exclusive lock for the entire pool creation
	storageMu.Lock()
	defer storageMu.Unlock()

	steps := []Step{
		// 0. Wipe all disks
		{Name: "wipe_disks", Policy: FailFast, Do: func() error {
			for _, d := range disks {
				result := wipeDiskInternal(d)
				if errMsg, ok := result["error"].(string); ok && errMsg != "" {
					return fmt.Errorf("wipe %s: %s", d, errMsg)
				}
			}
			return nil
		}},

		// 1. Partition disks (BF01 for ZFS — like TrueNAS)
		{Name: "partition_disks", Policy: FailFast, Do: func() error {
			for _, d := range disks {
				_, err := runCmd("sgdisk", []string{"-n", "1:0:0", "-t", "1:BF01", d}, opts)
				if err != nil {
					return fmt.Errorf("partition %s: %w", d, err)
				}
			}
			runCmd("udevadm", []string{"settle", "--timeout=5"}, optsShort)
			time.Sleep(1 * time.Second)

			// Wait for partitions to appear
			for _, d := range disks {
				pName := "/dev/" + partitionName(strings.TrimPrefix(d, "/dev/"))
				if err := waitForDevice(pName, 5*time.Second); err != nil {
					return fmt.Errorf("partition %s not ready: %w", pName, err)
				}
			}
			return nil
		}, Undo: func() error {
			for _, d := range disks {
				runCmd("sgdisk", []string{"-Z", d}, optsShort)
				runCmd("wipefs", []string{"-af", d}, optsShort)
			}
			return nil
		}},

		// 2. Create zpool
		{Name: "zpool_create", Policy: FailFast, Do: func() error {
			args := []string{"create", "-f", "-o", "ashift=12", "-m", mountPoint, zpoolName}

			if vdevType != "" && vdevType != "stripe" && vdevType != "single" {
				args = append(args, vdevType)
			}

			// Pass PARTITIONS, not whole disks (like TrueNAS)
			for _, d := range disks {
				pName := partitionName(strings.TrimPrefix(d, "/dev/"))
				args = append(args, pName)
			}

			logMsg("ZFS: zpool %s", strings.Join(args, " "))
			_, err := runCmd("zpool", args, opts)
			return err
		}, Undo: func() error {
			runCmd("zpool", []string{"destroy", "-f", zpoolName}, CmdOptions{Timeout: 30 * time.Second})
			return nil
		}},

		// 3. Set pool properties
		{Name: "set_properties", Policy: Continue, Do: func() error {
			props := map[string]string{
				"compression": "lz4",
				"atime":       "off",
				"xattr":       "sa",
				"acltype":     "posixacl",
			}
			for k, v := range props {
				runCmd("zfs", []string{"set", k + "=" + v, zpoolName}, optsShort)
			}
			return nil
		}},

		// 4. Create standard datasets
		{Name: "create_datasets", Policy: FailFast, Do: func() error {
			for _, ds := range []string{"shares", "system-backup"} {
				_, err := runCmd("zfs", []string{"create", zpoolName + "/" + ds}, optsShort)
				if err != nil {
					return fmt.Errorf("create dataset %s: %w", ds, err)
				}
			}
			return nil
		}},

		// 5. Verify mount is real (not system disk)
		{Name: "verify_mount", Policy: FailFast, Do: func() error {
			if !isPathOnMountedPool(mountPoint) {
				return fmt.Errorf("pool created but mount verification failed at %s", mountPoint)
			}
			logMsg("ZFS pool '%s' mount verified at %s", name, mountPoint)
			return nil
		}},

		// 6. Save config + write identity file
		{Name: "save_config", Policy: FailFast, Do: func() error {
			// Write identity
			writePoolIdentity(mountPoint, name, "zfs", vdevType, disks)

			// Create standard dirs
			createPoolDirs(mountPoint)

			// Save to storage.json
			conf := getStorageConfigFull()
			confPools, _ := conf["pools"].([]interface{})
			isFirst := len(confPools) == 0

			confPools = append(confPools, map[string]interface{}{
				"name":       name,
				"type":       "zfs",
				"zpoolName":  zpoolName,
				"mountPoint": mountPoint,
				"vdevType":   vdevType,
				"disks":      disksRaw,
				"createdAt":  time.Now().UTC().Format(time.RFC3339),
			})
			conf["pools"] = confPools
			if isFirst {
				conf["primaryPool"] = name
				conf["configuredAt"] = time.Now().UTC().Format(time.RFC3339)
			}
			saveStorageConfigFull(conf)
			logMsg("ZFS pool '%s' saved to config (primary: %v)", name, isFirst)
			return nil
		}},
	}

	if err := runSteps(op, steps); err != nil {
		return map[string]interface{}{"error": err.Error()}
	}

	logMsg("ZFS pool '%s' created successfully (%s, %d disks)", name, vdevType, len(disks))
	return map[string]interface{}{
		"ok":          true,
		"pool":        map[string]interface{}{"name": name, "type": "zfs", "zpoolName": zpoolName, "mountPoint": mountPoint, "vdevType": vdevType},
		"isFirstPool": len(confPools) == 1,
	}
}

// ─── Destroy Pool ZFS ────────────────────────────────────────────────────────

func destroyPoolZfs(poolName string) map[string]interface{} {
	storageMu.Lock()
	defer storageMu.Unlock()

	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})

	// Find pool in config
	var poolConf map[string]interface{}
	var poolIdx int
	for i, p := range confPools {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == poolName {
			poolConf = pm
			poolIdx = i
			break
		}
	}
	if poolConf == nil {
		return map[string]interface{}{"error": fmt.Sprintf(`Pool "%s" not found`, poolName)}
	}

	zpoolName, _ := poolConf["zpoolName"].(string)
	if zpoolName == "" {
		zpoolName = "nimos-" + poolName
	}
	mountPoint, _ := poolConf["mountPoint"].(string)

	logMsg("Destroying ZFS pool '%s' (zpool: %s, mount: %s)", poolName, zpoolName, mountPoint)
	opts := CmdOptions{Timeout: 30 * time.Second}

	// 1. Delete shares from DB
	shares, _ := dbSharesList()
	for _, s := range shares {
		sharPool, _ := s["pool"].(string)
		sharVolume, _ := s["volume"].(string)
		sharPath, _ := s["path"].(string)
		sharName, _ := s["name"].(string)
		if sharPool == poolName || sharVolume == poolName || (mountPoint != "" && strings.HasPrefix(sharPath, mountPoint)) {
			handleOp(Request{Op: "share.delete", ShareName: sharName})
			dbSharesDelete(sharName)
		}
	}

	// 2. Unmount submounts (children first) — NO fuser on the pool mount point
	// fuser -km kills EVERYTHING with an open fd there, including nginx
	if mountPoint != "" {
		mountsOut, _ := runCmd("findmnt", []string{"-rn", "-o", "TARGET", mountPoint}, opts)
		mounts := strings.Split(strings.TrimSpace(mountsOut.Stdout), "\n")
		for i := len(mounts) - 1; i >= 0; i-- {
			m := strings.TrimSpace(mounts[i])
			if m != "" && m != mountPoint {
				runCmd("umount", []string{"-f", "-l", m}, opts)
			}
		}
	}

	// 3. Force-unmount all ZFS datasets (deepest first)
	datasetsOut, _ := runCmd("zfs", []string{"list", "-H", "-o", "name", "-r", zpoolName}, opts)
	if datasetsOut.Stdout != "" {
		datasets := strings.Split(strings.TrimSpace(datasetsOut.Stdout), "\n")
		for i := len(datasets) - 1; i >= 0; i-- {
			ds := strings.TrimSpace(datasets[i])
			if ds != "" {
				runCmd("zfs", []string{"unmount", "-f", ds}, opts)
			}
		}
	}
	time.Sleep(1 * time.Second)

	// 4. Destroy zpool
	_, err := runCmd("zpool", []string{"destroy", "-f", zpoolName}, opts)
	if err != nil {
		// Retry with export first
		logMsg("zpool destroy failed, trying export+reimport+destroy")
		runCmd("zpool", []string{"export", "-f", zpoolName}, opts)
		time.Sleep(1 * time.Second)
		runCmd("zpool", []string{"import", "-f", zpoolName}, opts)
		_, err = runCmd("zpool", []string{"destroy", "-f", zpoolName}, opts)
		if err != nil {
			// Last resort: just export
			runCmd("zpool", []string{"export", "-f", zpoolName}, opts)
			logMsg("WARNING: Could not destroy %s, force-exported", zpoolName)
		}
	}

	// 5. Clean up mount point
	if mountPoint != "" && strings.HasPrefix(mountPoint, nimbusPoolsDir) {
		os.RemoveAll(mountPoint)
	}

	// 6. Remove from storage.json
	confPools = append(confPools[:poolIdx], confPools[poolIdx+1:]...)
	conf["pools"] = confPools
	if primary, _ := conf["primaryPool"].(string); primary == poolName {
		if len(confPools) > 0 {
			if first, ok := confPools[0].(map[string]interface{}); ok {
				conf["primaryPool"] = first["name"]
			}
		} else {
			conf["primaryPool"] = nil
			conf["configuredAt"] = nil
		}
	}
	saveStorageConfigFull(conf)

	// 7. Rescan
	runCmd("partprobe", nil, opts)
	rescanSCSIBuses()

	logMsg("ZFS pool '%s' destroyed", poolName)
	return map[string]interface{}{"ok": true}
}

// ─── Create Pool BTRFS ───────────────────────────────────────────────────────

func createPoolBtrfs(body map[string]interface{}) map[string]interface{} {
	// TODO: implement with same Step pattern as ZFS
	return map[string]interface{}{"error": "BTRFS pool creation pending implementation"}
}

// ─── Destroy Pool BTRFS ──────────────────────────────────────────────────────

func destroyPoolBtrfs(poolName string) map[string]interface{} {
	// TODO: implement
	return map[string]interface{}{"error": "BTRFS pool destroy pending implementation"}
}
