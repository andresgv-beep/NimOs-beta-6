package main

// ═══════════════════════════════════════════════════════════════════════════════
// NimOS Storage — Common helpers shared by ZFS and BTRFS pool operations
// ═══════════════════════════════════════════════════════════════════════════════

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// ─── Device Helpers ──────────────────────────────────────────────────────────

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

// ─── Pool Identity ───────────────────────────────────────────────────────────

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

// ─── Config Helpers ──────────────────────────────────────────────────────────

// removePoolFromConfig removes a pool by name from storage.json and updates primaryPool.
// Returns the updated config and the removed pool's config (or nil if not found).
func removePoolFromConfig(poolName string) (conf map[string]interface{}, poolConf map[string]interface{}, poolIdx int) {
	conf = getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})

	for i, p := range confPools {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == poolName {
			poolConf = pm
			poolIdx = i

			// Remove from slice
			confPools = append(confPools[:i], confPools[i+1:]...)
			conf["pools"] = confPools

			// Update primary pool
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
			return
		}
	}
	return conf, nil, -1
}

// deleteSharesForPool removes all shares associated with a pool from the DB.
func deleteSharesForPool(poolName, mountPoint string) {
	shares, _ := dbSharesListRaw()
	for _, s := range shares {
		if s.Pool == poolName || s.Volume == poolName || (mountPoint != "" && strings.HasPrefix(s.Path, mountPoint)) {
			handleOp(Request{Op: "share.delete", ShareName: s.Name})
			dbSharesDelete(s.Name)
		}
	}
}

// ─── Fstab ───────────────────────────────────────────────────────────────────

// removeFstabEntry removes a mount point entry from /etc/fstab
func removeFstabEntry(mountPoint string) {
	if mountPoint == "" {
		return
	}
	data, err := os.ReadFile("/etc/fstab")
	if err != nil {
		return
	}
	var kept []string
	for _, line := range strings.Split(string(data), "\n") {
		if strings.Contains(line, mountPoint) {
			logMsg("Removing fstab entry: %s", strings.TrimSpace(line))
			continue
		}
		kept = append(kept, line)
	}
	os.WriteFile("/etc/fstab", []byte(strings.Join(kept, "\n")), 0644)
}

// ─── Orphan Cleanup ──────────────────────────────────────────────────────────

// cleanOrphanPoolDirs removes directories in /nimbus/pools/ that are not
// associated with any configured pool and have nothing mounted on them.
// Safe to call AFTER pool operations (destroy, create), never at startup
// before ZFS has mounted.
func cleanOrphanPoolDirs() {
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})

	// Build set of known mount points
	knownMounts := map[string]bool{}
	for _, poolRaw := range confPools {
		pm, _ := poolRaw.(map[string]interface{})
		if mp, _ := pm["mountPoint"].(string); mp != "" {
			knownMounts[mp] = true
		}
	}

	entries, err := os.ReadDir(nimbusPoolsDir)
	if err != nil {
		return
	}

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		dirPath := filepath.Join(nimbusPoolsDir, e.Name())

		// Skip known pools
		if knownMounts[dirPath] {
			continue
		}

		// Skip if something real is mounted here
		if isPathOnMountedPool(dirPath) {
			continue
		}

		// Orphan on system disk — safe to remove
		os.RemoveAll(dirPath)
		logMsg("Cleaned orphan directory: %s", dirPath)
	}
}

// ─── Torrent Config ──────────────────────────────────────────────────────────

// updateTorrentConfig updates NimTorrent's download_dir to point to the primary
// pool's shares directory. Called after create/destroy pool.
// Without this, NimTorrent writes to the system disk.
const torrentConfPath = "/etc/nimos/torrent.conf"

func updateTorrentConfig() {
	conf := getStorageConfigFull()
	primaryPool, _ := conf["primaryPool"].(string)

	newDir := ""
	if primaryPool != "" {
		confPools, _ := conf["pools"].([]interface{})
		for _, p := range confPools {
			pm, _ := p.(map[string]interface{})
			if n, _ := pm["name"].(string); n == primaryPool {
				mp, _ := pm["mountPoint"].(string)
				if mp != "" {
					newDir = filepath.Join(mp, "shares")
				}
				break
			}
		}
	}

	// Read current config
	data, err := os.ReadFile(torrentConfPath)
	if err != nil {
		// No torrent config — nothing to update
		return
	}

	// Replace download_dir line
	var lines []string
	found := false
	for _, line := range strings.Split(string(data), "\n") {
		if strings.HasPrefix(line, "download_dir=") {
			if newDir != "" {
				lines = append(lines, "download_dir="+newDir)
			} else {
				lines = append(lines, "download_dir=")
			}
			found = true
		} else {
			lines = append(lines, line)
		}
	}
	if !found && newDir != "" {
		lines = append(lines, "download_dir="+newDir)
	}

	os.WriteFile(torrentConfPath, []byte(strings.Join(lines, "\n")), 0644)

	// Restart torrentd to pick up new config
	runCmd("systemctl", []string{"restart", "nimos-torrentd"}, CmdOptions{Timeout: 10 * time.Second})

	if newDir != "" {
		logMsg("Updated NimTorrent download_dir to %s", newDir)
	} else {
		logMsg("Cleared NimTorrent download_dir (no pools)")
	}
}
