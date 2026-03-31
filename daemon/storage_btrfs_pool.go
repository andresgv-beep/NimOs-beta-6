package main

// ═══════════════════════════════════════════════════════════════════════════════
// NimOS Storage — BTRFS Pool Create & Destroy
// ═══════════════════════════════════════════════════════════════════════════════

import (
	"fmt"
	"os"
	"regexp"
	"strings"
	"time"
)

// ─── Create Pool BTRFS ───────────────────────────────────────────────────────

func createPoolBtrfs(body map[string]interface{}) map[string]interface{} {
	name := bodyStr(body, "name")
	profile := bodyStr(body, "profile")
	if profile == "" {
		profile = "single"
	}

	// Validate name
	if name == "" || !regexp.MustCompile(`^[a-zA-Z0-9-]{1,32}$`).MatchString(name) {
		return map[string]interface{}{"error": "Invalid pool name. Use alphanumeric + hyphens, max 32 chars."}
	}
	reserved := map[string]bool{"system": true, "config": true, "temp": true, "swap": true, "root": true, "boot": true}
	if reserved[strings.ToLower(name)] {
		return map[string]interface{}{"error": fmt.Sprintf(`"%s" is a reserved name.`, name)}
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

	// Validate profile vs disk count
	minDisks := map[string]int{"single": 1, "raid1": 2, "raid10": 4}
	if min, ok := minDisks[profile]; ok {
		if len(disks) < min {
			return map[string]interface{}{"error": fmt.Sprintf("%s requires at least %d disks.", profile, min)}
		}
	}

	// Pre-flight check on all disks
	for _, d := range disks {
		if err := preFlightCheck(d); err != nil {
			return map[string]interface{}{"error": fmt.Sprintf("Disk %s: %s", d, err.Error())}
		}
	}

	mountPoint := nimbusPoolsDir + "/" + name
	label := "nimos-" + name
	opts := CmdOptions{Timeout: 120 * time.Second}
	optsShort := CmdOptions{Timeout: 15 * time.Second}

	op := JournalOp{
		ID:   "create-btrfs-" + name,
		Type: "create_pool",
		Data: map[string]string{"name": name, "type": "btrfs", "profile": profile},
	}

	// Take exclusive lock
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

		// 1. Create BTRFS filesystem (with retry + module reset fallback)
		{Name: "mkfs_btrfs", Policy: FailFast, Do: func() error {
			args := []string{"-f", "-L", label}

			// Set data and metadata profiles
			switch profile {
			case "raid1":
				args = append(args, "-d", "raid1", "-m", "raid1")
			case "raid10":
				args = append(args, "-d", "raid10", "-m", "raid10")
			default: // single
				if len(disks) > 1 {
					args = append(args, "-d", "single", "-m", "raid1")
				}
			}

			args = append(args, disks...)

			// Retry loop — BTRFS kernel module may hold device references
			for attempt := 0; attempt < 3; attempt++ {
				runCmd("btrfs", []string{"device", "scan", "--forget"}, optsShort)
				runCmd("udevadm", []string{"settle", "--timeout=3"}, optsShort)
				time.Sleep(time.Duration(500+attempt*500) * time.Millisecond)

				logMsg("BTRFS: mkfs attempt %d/3 — mkfs.btrfs %s", attempt+1, strings.Join(args, " "))
				_, err := runCmd("mkfs.btrfs", args, opts)
				if err == nil {
					return nil
				}

				if strings.Contains(err.Error(), "Device or resource busy") {
					logMsg("BTRFS: device busy, retrying...")
					time.Sleep(time.Duration(1+attempt) * time.Second)
					continue
				}
				return err
			}

			// Fallback: reset BTRFS kernel module (safe only if nothing mounted)
			logMsg("BTRFS: devices still busy after 3 attempts — attempting module reset")

			// Safety check: no BTRFS mounts active
			mountOut, _ := runCmd("mount", nil, optsShort)
			if strings.Contains(mountOut.Stdout, "type btrfs") {
				return fmt.Errorf("mkfs.btrfs failed: devices busy and BTRFS mounts still active — cannot reset module")
			}

			// Safety check: no BTRFS filesystems registered
			fsShow, _ := runCmd("btrfs", []string{"filesystem", "show"}, optsShort)
			hasBtrfsActive := false
			for _, line := range strings.Split(fsShow.Stdout, "\n") {
				if strings.Contains(line, "/dev/") {
					hasBtrfsActive = true
					break
				}
			}

			if hasBtrfsActive {
				// Forget first, then check again
				runCmd("btrfs", []string{"device", "scan", "--forget"}, optsShort)
				time.Sleep(1 * time.Second)
				fsShow2, _ := runCmd("btrfs", []string{"filesystem", "show"}, optsShort)
				if strings.Contains(fsShow2.Stdout, "/dev/") {
					return fmt.Errorf("mkfs.btrfs failed: devices busy and BTRFS filesystems still registered")
				}
			}

			// Safe to reset module
			logMsg("BTRFS: no active mounts or filesystems — resetting kernel module")
			_, err := runCmd("modprobe", []string{"-r", "btrfs"}, optsShort)
			if err != nil {
				return fmt.Errorf("mkfs.btrfs failed: cannot unload BTRFS module: %s", err)
			}
			time.Sleep(1 * time.Second)
			runCmd("modprobe", []string{"btrfs"}, optsShort)
			time.Sleep(1 * time.Second)

			// Final attempt after module reset
			logMsg("BTRFS: module reset complete — final mkfs attempt")
			_, err = runCmd("mkfs.btrfs", args, opts)
			if err != nil {
				return fmt.Errorf("mkfs.btrfs failed even after module reset: %s", err)
			}
			return nil
		}, Undo: func() error {
			runCmd("btrfs", []string{"device", "scan", "--forget"}, optsShort)
			for _, d := range disks {
				runCmd("wipefs", []string{"-af", d}, optsShort)
			}
			return nil
		}},

		// 2. Get UUID and mount filesystem
		{Name: "get_uuid_and_mount", Policy: FailFast, Do: func() error {
			// Scan for BTRFS multi-device members — required before mount
			runCmd("btrfs", []string{"device", "scan"}, optsShort)
			time.Sleep(1 * time.Second)

			// Get UUID from first disk
			res, err := runCmd("blkid", []string{"-s", "UUID", "-o", "value", disks[0]}, optsShort)
			if err != nil || strings.TrimSpace(res.Stdout) == "" {
				// Try second disk
				if len(disks) > 1 {
					res, err = runCmd("blkid", []string{"-s", "UUID", "-o", "value", disks[1]}, optsShort)
				}
				if err != nil || strings.TrimSpace(res.Stdout) == "" {
					return fmt.Errorf("failed to get BTRFS UUID from any disk")
				}
			}
			uuid := strings.TrimSpace(res.Stdout)

			// Create mount point
			os.MkdirAll(mountPoint, 0755)

			// Mount with device path (more reliable than UUID for fresh BTRFS)
			_, err = runCmd("mount", []string{"-t", "btrfs", "-o", "noatime,compress=zstd", disks[0], mountPoint}, opts)
			if err != nil {
				// Retry with UUID
				logMsg("BTRFS mount by device failed, trying UUID %s", uuid)
				_, err = runCmd("mount", []string{"-t", "btrfs", "-o", "noatime,compress=zstd", "UUID=" + uuid, mountPoint}, opts)
				if err != nil {
					return fmt.Errorf("mount failed: %w", err)
				}
			}

			// VERIFY mount is real with findmnt
			verifyRes, _ := runCmd("findmnt", []string{"-n", "-o", "TARGET", mountPoint}, optsShort)
			if strings.TrimSpace(verifyRes.Stdout) == "" {
				return fmt.Errorf("mount command succeeded but %s is not mounted", mountPoint)
			}

			// Store UUID — fstab write happens LATER to avoid systemd interference
			op.Data["uuid"] = uuid
			return nil
		}, Undo: func() error {
			runCmd("umount", []string{"-f", mountPoint}, optsShort)
			os.RemoveAll(mountPoint)
			return nil
		}},

		// 3. Verify mount is real (double check)
		{Name: "verify_mount", Policy: FailFast, Do: func() error {
			verifyRes, _ := runCmd("findmnt", []string{"-n", "-o", "SOURCE", mountPoint}, optsShort)
			if strings.TrimSpace(verifyRes.Stdout) == "" {
				return fmt.Errorf("pool created but mount verification failed at %s", mountPoint)
			}
			logMsg("BTRFS pool '%s' mount verified at %s (source: %s)", name, mountPoint, strings.TrimSpace(verifyRes.Stdout))

			// Enable BTRFS quotas — required for per-folder quota limits
			runCmd("btrfs", []string{"quota", "enable", mountPoint}, optsShort)
			logMsg("BTRFS quotas enabled on %s", mountPoint)
			return nil
		}},

		// 4. Create standard directories + identity + save config
		{Name: "create_dirs_and_config", Policy: FailFast, Do: func() error {
			// Create standard dirs
			createPoolDirs(mountPoint)

			// Write identity file
			writePoolIdentity(mountPoint, name, "btrfs", profile, disks)

			// Save to storage.json
			conf := getStorageConfigFull()
			confPools, _ := conf["pools"].([]interface{})
			isFirst := len(confPools) == 0

			confPools = append(confPools, map[string]interface{}{
				"name":       name,
				"type":       "btrfs",
				"profile":    profile,
				"mountPoint": mountPoint,
				"uuid":       op.Data["uuid"],
				"disks":      disksRaw,
				"createdAt":  time.Now().UTC().Format(time.RFC3339),
			})
			conf["pools"] = confPools
			if isFirst {
				conf["primaryPool"] = name
				conf["configuredAt"] = time.Now().UTC().Format(time.RFC3339)
			}
			saveStorageConfigFull(conf)

			// Write fstab LAST — nofail means it won't block boot if mount fails
			// Do NOT call systemctl daemon-reload here — it restarts the daemon
			// and loses the mount. fstab with nofail is read at next boot.
			appendFstab(op.Data["uuid"], mountPoint, "btrfs")

			logMsg("BTRFS pool '%s' saved to config (primary: %v)", name, isFirst)
			return nil
		}},
	}

	if err := runSteps(op, steps); err != nil {
		return map[string]interface{}{"error": err.Error()}
	}

	logMsg("BTRFS pool '%s' created successfully (%s, %d disks)", name, profile, len(disks))
	updateTorrentConfig()
	return map[string]interface{}{
		"ok":          true,
		"pool":        map[string]interface{}{"name": name, "type": "btrfs", "profile": profile, "mountPoint": mountPoint},
		"isFirstPool": len(confPools) == 1,
	}
}

// ─── Destroy Pool BTRFS ──────────────────────────────────────────────────────

func destroyPoolBtrfs(poolName string) map[string]interface{} {
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

	mountPoint, _ := poolConf["mountPoint"].(string)
	opts := CmdOptions{Timeout: 30 * time.Second}

	logMsg("Destroying BTRFS pool '%s' (mount: %s)", poolName, mountPoint)

	// 1. Delete shares from DB
	deleteSharesForPool(poolName, mountPoint)

	// 2. Unmount — verify it actually unmounted
	if mountPoint != "" {
		runCmd("umount", []string{"-f", mountPoint}, opts)
		time.Sleep(1 * time.Second)

		// Verify unmount
		verifyRes, _ := runCmd("findmnt", []string{"-n", "-o", "TARGET", mountPoint}, opts)
		if strings.TrimSpace(verifyRes.Stdout) != "" {
			// Still mounted — try lazy unmount as last resort
			logMsg("WARNING: %s still mounted after umount -f, trying lazy umount", mountPoint)
			runCmd("umount", []string{"-f", "-l", mountPoint}, opts)
			time.Sleep(2 * time.Second)
		}
	}

	// 3. Clean up mount point
	if mountPoint != "" && strings.HasPrefix(mountPoint, nimbusPoolsDir) {
		os.RemoveAll(mountPoint)
	}

	// 4. Remove fstab entry
	removeFstabEntry(mountPoint)

	// 5. Release BTRFS multi-device lock and wipe disks
	runCmd("btrfs", []string{"device", "scan", "--forget"}, opts)
	if disksRaw, ok := poolConf["disks"].([]interface{}); ok {
		for _, d := range disksRaw {
			if ds, ok := d.(string); ok {
				runCmd("wipefs", []string{"-af", ds}, opts)
			}
		}
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

	// 8. Clean orphans
	cleanOrphanPoolDirs()

	logMsg("BTRFS pool '%s' destroyed", poolName)
	updateTorrentConfig()
	return map[string]interface{}{"ok": true}
}
