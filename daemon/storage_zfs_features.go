package main

// ═══════════════════════════════════════════════════════════════════════════════
// NimOS Storage — ZFS Features (Snapshots, Scrub, Datasets)
// Endpoints match Sonnet's UI contract exactly.
// ═══════════════════════════════════════════════════════════════════════════════

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"
)

// ─── Resolve pool name to zpool name ─────────────────────────────────────────

func resolveZpoolName(poolName string) string {
	conf := getStorageConfigFull()
	confPools, _ := conf["pools"].([]interface{})
	for _, p := range confPools {
		pm, _ := p.(map[string]interface{})
		if n, _ := pm["name"].(string); n == poolName {
			if zn, _ := pm["zpoolName"].(string); zn != "" {
				return zn
			}
			return "nimos-" + poolName
		}
	}
	return ""
}

// ─── SNAPSHOTS ───────────────────────────────────────────────────────────────

// listSnapshots returns all snapshots for a pool
// GET /api/storage/snapshots?pool=NAME
func listSnapshots(poolName string) map[string]interface{} {
	zpoolName := resolveZpoolName(poolName)
	if zpoolName == "" {
		return map[string]interface{}{"snapshots": []interface{}{}}
	}

	opts := CmdOptions{Timeout: 15 * time.Second}
	res, err := runCmd("zfs", []string{
		"list", "-H", "-t", "snapshot",
		"-o", "name,used,refer,creation",
		"-r", zpoolName,
	}, opts)
	if err != nil || res.Stdout == "" {
		return map[string]interface{}{"snapshots": []interface{}{}}
	}

	var snaps []interface{}
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 4 {
			continue
		}
		fullName := parts[0]
		if !strings.Contains(fullName, "@") {
			continue
		}

		used := parseZfsSize(parts[1])
		refer := parseZfsSize(parts[2])
		// Creation is the rest of the fields joined (e.g. "Thu Mar 26 19:30 2026")
		created := strings.Join(parts[3:], " ")

		snaps = append(snaps, map[string]interface{}{
			"name":    fullName,
			"pool":    poolName,
			"created": created,
			"used":    used,
			"refer":   refer,
		})
	}

	if snaps == nil {
		snaps = []interface{}{}
	}
	return map[string]interface{}{"snapshots": snaps}
}

// createSnapshot creates a new ZFS snapshot
// POST /api/storage/snapshot { pool, name }
func createSnapshot(body map[string]interface{}) map[string]interface{} {
	pool := bodyStr(body, "pool")
	name := bodyStr(body, "name")

	zpoolName := resolveZpoolName(pool)
	if zpoolName == "" {
		return map[string]interface{}{"ok": false, "error": "Pool not found"}
	}

	if name == "" {
		name = "manual-" + time.Now().Format("20060102-150405")
	}

	// Snapshot the main pool dataset — includes all children recursively
	fullSnap := zpoolName + "@" + name
	opts := CmdOptions{Timeout: 30 * time.Second}
	_, err := runCmd("zfs", []string{"snapshot", "-r", fullSnap}, opts)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("snapshot failed: %s", err)}
	}

	logMsg("ZFS snapshot created: %s", fullSnap)
	return map[string]interface{}{"ok": true}
}

// deleteSnapshot deletes a ZFS snapshot
// DELETE /api/storage/snapshot { snapshot: "pool@name" }
func deleteSnapshot(body map[string]interface{}) map[string]interface{} {
	snapshot := bodyStr(body, "snapshot")
	if snapshot == "" || !strings.Contains(snapshot, "@") {
		return map[string]interface{}{"ok": false, "error": "Invalid snapshot name (need pool@name)"}
	}

	opts := CmdOptions{Timeout: 30 * time.Second}
	_, err := runCmd("zfs", []string{"destroy", "-r", snapshot}, opts)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("delete failed: %s", err)}
	}

	logMsg("ZFS snapshot deleted: %s", snapshot)
	return map[string]interface{}{"ok": true}
}

// rollbackSnapshot rolls back to a ZFS snapshot
// POST /api/storage/snapshot/rollback { snapshot: "pool@name" }
func rollbackSnapshot(body map[string]interface{}) map[string]interface{} {
	snapshot := bodyStr(body, "snapshot")
	if snapshot == "" || !strings.Contains(snapshot, "@") {
		return map[string]interface{}{"ok": false, "error": "Invalid snapshot name"}
	}

	// -r destroys newer snapshots to allow rollback
	opts := CmdOptions{Timeout: 60 * time.Second}
	_, err := runCmd("zfs", []string{"rollback", "-r", snapshot}, opts)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("rollback failed: %s", err)}
	}

	logMsg("ZFS rollback to: %s", snapshot)
	return map[string]interface{}{"ok": true}
}

// ─── SCRUB ───────────────────────────────────────────────────────────────────

// startScrub starts a ZFS or BTRFS integrity check
// POST /api/storage/scrub { pool }
func startScrub(body map[string]interface{}) map[string]interface{} {
	pool := bodyStr(body, "pool")

	// Try ZFS first
	zpoolName := resolveZpoolName(pool)
	if zpoolName != "" {
		opts := CmdOptions{Timeout: 15 * time.Second}
		_, err := runCmd("zpool", []string{"scrub", zpoolName}, opts)
		if err != nil {
			return map[string]interface{}{"ok": false, "error": fmt.Sprintf("scrub failed: %s", err)}
		}
		logMsg("ZFS scrub started on %s", zpoolName)
		addNotification("info", "system", "Verificación iniciada",
			fmt.Sprintf("Verificación de integridad iniciada en volumen %s", pool))
		return map[string]interface{}{"ok": true, "type": "zfs"}
	}

	// Try BTRFS
	mountPoint := nimbusPoolsDir + "/" + pool
	if _, err := runCmd("btrfs", []string{"filesystem", "show", mountPoint}, CmdOptions{Timeout: 5 * time.Second}); err == nil {
		// BTRFS scrub runs in background by default
		_, err := runCmd("btrfs", []string{"scrub", "start", mountPoint}, CmdOptions{Timeout: 15 * time.Second})
		if err != nil {
			return map[string]interface{}{"ok": false, "error": fmt.Sprintf("btrfs scrub failed: %s", err)}
		}
		logMsg("BTRFS scrub started on %s", mountPoint)
		addNotification("info", "system", "Verificación iniciada",
			fmt.Sprintf("Verificación de integridad iniciada en volumen %s", pool))
		return map[string]interface{}{"ok": true, "type": "btrfs"}
	}

	return map[string]interface{}{"ok": false, "error": "Pool not found or unsupported filesystem"}
}

// getScrubStatus returns detailed scrub/integrity check status
// GET /api/storage/scrub/status?pool=NAME
//
// Returns:
//   status:        "idle" | "scrubbing" | "done" | "canceled" | "error"
//   progress:      0-100 (percentage)
//   errors:        number of errors found
//   repaired:      bytes repaired (string like "0B" or "128K")
//   duration:      how long the scrub took/is taking (string like "00:12:34")
//   lastScrub:     ISO timestamp of last completed scrub (or null)
//   lastDuration:  duration of last completed scrub
//   lastErrors:    errors found in last scrub
//   scanned:       bytes scanned so far (string)
//   totalSize:     total bytes to scan (string)
//   speed:         current scan speed (string like "123M/s")
//   timeRemaining: estimated time remaining (string like "01:23:45")
//   dataErrors:    string from "errors:" line (e.g. "No known data errors")
//   poolState:     pool state from zpool status (ONLINE, DEGRADED, etc.)
//   disks:         array of disk states [{name, state, read, write, cksum}]
//   filesystem:    "zfs" | "btrfs"
func getScrubStatus(poolName string) map[string]interface{} {
	// Try ZFS
	zpoolName := resolveZpoolName(poolName)
	if zpoolName != "" {
		return getZfsScrubStatus(zpoolName, poolName)
	}

	// Try BTRFS
	mountPoint := nimbusPoolsDir + "/" + poolName
	if _, err := runCmd("btrfs", []string{"filesystem", "show", mountPoint}, CmdOptions{Timeout: 5 * time.Second}); err == nil {
		return getBtrfsScrubStatus(mountPoint, poolName)
	}

	return map[string]interface{}{"status": "error", "error": "Pool not found", "filesystem": "unknown"}
}

func getZfsScrubStatus(zpoolName, poolName string) map[string]interface{} {
	opts := CmdOptions{Timeout: 10 * time.Second}
	res, _ := runCmd("zpool", []string{"status", zpoolName}, opts)
	output := res.Stdout

	result := map[string]interface{}{
		"status":        "idle",
		"progress":      0,
		"errors":        0,
		"repaired":      "0B",
		"duration":      "—",
		"lastScrub":     nil,
		"lastDuration":  nil,
		"lastErrors":    nil,
		"scanned":       "—",
		"totalSize":     "—",
		"speed":         "—",
		"timeRemaining": "—",
		"dataErrors":    "—",
		"poolState":     "UNKNOWN",
		"disks":         []map[string]interface{}{},
		"filesystem":    "zfs",
	}

	// Parse pool state
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "state:") {
			result["poolState"] = strings.TrimSpace(strings.TrimPrefix(line, "state:"))
		}
	}

	// Parse errors line
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "errors:") {
			result["dataErrors"] = strings.TrimSpace(strings.TrimPrefix(line, "errors:"))
		}
	}

	// Parse disk states from config section
	var disks []map[string]interface{}
	inConfig := false
	for _, line := range strings.Split(output, "\n") {
		if strings.Contains(line, "NAME") && strings.Contains(line, "STATE") && strings.Contains(line, "READ") {
			inConfig = true
			continue
		}
		if inConfig && strings.TrimSpace(line) == "" {
			break
		}
		if inConfig {
			fields := strings.Fields(line)
			if len(fields) >= 5 {
				name := fields[0]
				// Skip pool-level and vdev-level entries, only show actual disks
				if strings.HasPrefix(name, "ata-") || strings.HasPrefix(name, "scsi-") ||
					strings.HasPrefix(name, "wwn-") || strings.HasPrefix(name, "sd") ||
					strings.HasPrefix(name, "nvme") {
					disks = append(disks, map[string]interface{}{
						"name":  name,
						"state": fields[1],
						"read":  fields[2],
						"write": fields[3],
						"cksum": fields[4],
					})
				}
			}
		}
	}
	if disks == nil {
		disks = []map[string]interface{}{}
	}
	result["disks"] = disks

	// Parse scan line — this contains all the scrub info
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)

		// Scrub in progress:
		// "scan: scrub in progress since Thu Apr  3 19:30:00 2026"
		// "    1.23G scanned at 123M/s, 456M issued at 45.6M/s, 1.80T total"
		// "    0B repaired, 25.30% done, 01:23:45 to go"
		if strings.Contains(line, "scan: scrub in progress") {
			result["status"] = "scrubbing"
			// Extract start time
			if idx := strings.Index(line, "since "); idx >= 0 {
				timeStr := strings.TrimSpace(line[idx+6:])
				if t, err := time.Parse("Mon Jan  2 15:04:05 2006", timeStr); err == nil {
					result["duration"] = formatDuration(time.Since(t))
				} else if t, err := time.Parse("Mon Jan 2 15:04:05 2006", timeStr); err == nil {
					result["duration"] = formatDuration(time.Since(t))
				}
			}
		}

		// Scanned/speed/total line
		if strings.Contains(line, "scanned") && strings.Contains(line, "total") {
			fields := strings.Fields(line)
			for i, f := range fields {
				if f == "scanned" && i > 0 {
					result["scanned"] = fields[i-1]
				}
				if f == "total" && i > 0 {
					result["totalSize"] = fields[i-1]
				}
			}
			// Speed: "at 123M/s"
			for i, f := range fields {
				if f == "at" && i+1 < len(fields) && strings.Contains(fields[i+1], "/s") {
					result["speed"] = fields[i+1]
					break
				}
			}
		}

		// Progress/repaired/time remaining line
		if strings.Contains(line, "% done") {
			fields := strings.Fields(line)
			for i, f := range fields {
				if f == "repaired," && i > 0 {
					result["repaired"] = fields[i-1]
				}
				if strings.HasSuffix(f, "%") {
					pct, _ := strconv.ParseFloat(strings.TrimSuffix(f, "%"), 64)
					result["progress"] = int(math.Round(pct))
				}
			}
			// Time remaining: "01:23:45 to go"
			if idx := strings.Index(line, "to go"); idx > 0 {
				parts := strings.Fields(line[:idx])
				if len(parts) > 0 {
					result["timeRemaining"] = parts[len(parts)-1]
				}
			}
		}

		// Scrub completed:
		// "scan: scrub repaired 0B in 00:12:34 with 0 errors on Thu Apr  3 19:30:00 2026"
		if strings.Contains(line, "scan: scrub repaired") {
			result["status"] = "done"

			fields := strings.Fields(line)
			// "repaired 0B"
			for i, f := range fields {
				if f == "repaired" && i+1 < len(fields) {
					result["repaired"] = fields[i+1]
					result["lastRepaired"] = fields[i+1]
				}
			}
			// "in 00:12:34"
			for i, f := range fields {
				if f == "in" && i+1 < len(fields) && strings.Contains(fields[i+1], ":") {
					result["duration"] = fields[i+1]
					result["lastDuration"] = fields[i+1]
				}
			}
			// "with N errors"
			for i, f := range fields {
				if f == "with" && i+1 < len(fields) {
					n, _ := strconv.Atoi(fields[i+1])
					result["errors"] = n
					result["lastErrors"] = n
				}
			}
			// "on Thu Apr  3 19:30:00 2026" — date at end
			if idx := strings.Index(line, " on "); idx >= 0 {
				timeStr := strings.TrimSpace(line[idx+4:])
				// Try multiple date formats ZFS uses
				for _, layout := range []string{
					"Mon Jan  2 15:04:05 2006",
					"Mon Jan 2 15:04:05 2006",
					"Mon 2 Jan 15:04:05 2006",
				} {
					if t, err := time.Parse(layout, timeStr); err == nil {
						result["lastScrub"] = t.Format(time.RFC3339)
						break
					}
				}
			}
		}

		// Scrub canceled
		if strings.Contains(line, "scan: scrub canceled") {
			result["status"] = "canceled"
		}

		// No scrub ever run
		if strings.Contains(line, "scan: none requested") {
			result["status"] = "never"
			result["lastScrub"] = nil
		}
	}

	return result
}

func getBtrfsScrubStatus(mountPoint, poolName string) map[string]interface{} {
	opts := CmdOptions{Timeout: 10 * time.Second}
	res, _ := runCmd("btrfs", []string{"scrub", "status", mountPoint}, opts)
	output := res.Stdout

	result := map[string]interface{}{
		"status":        "idle",
		"progress":      0,
		"errors":        0,
		"duration":      "—",
		"lastScrub":     nil,
		"lastDuration":  nil,
		"lastErrors":    nil,
		"dataErrors":    "—",
		"filesystem":    "btrfs",
	}

	if strings.Contains(output, "no stats available") || strings.Contains(output, "not started") {
		result["status"] = "never"
		return result
	}

	// BTRFS scrub status output:
	// "Scrub started:    Thu Apr  3 19:30:00 2026"
	// "Status:           running" or "finished"
	// "Duration:         0:12:34"
	// "Total to scrub:   1.80TiB"
	// "Rate:             123.45MiB/s"
	// "Error summary:    no errors found"
	// or "Error summary:    csum=3"

	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)

		if strings.HasPrefix(line, "Status:") {
			status := strings.TrimSpace(strings.TrimPrefix(line, "Status:"))
			if status == "running" {
				result["status"] = "scrubbing"
			} else if status == "finished" {
				result["status"] = "done"
			} else if status == "aborted" {
				result["status"] = "canceled"
			}
		}

		if strings.HasPrefix(line, "Scrub started:") {
			timeStr := strings.TrimSpace(strings.TrimPrefix(line, "Scrub started:"))
			for _, layout := range []string{
				"Mon Jan  2 15:04:05 2006",
				"Mon Jan 2 15:04:05 2006",
				"2006-01-02 15:04:05",
			} {
				if t, err := time.Parse(layout, timeStr); err == nil {
					result["lastScrub"] = t.Format(time.RFC3339)
					break
				}
			}
		}

		if strings.HasPrefix(line, "Duration:") {
			dur := strings.TrimSpace(strings.TrimPrefix(line, "Duration:"))
			result["duration"] = dur
			result["lastDuration"] = dur
		}

		if strings.HasPrefix(line, "Rate:") {
			result["speed"] = strings.TrimSpace(strings.TrimPrefix(line, "Rate:"))
		}

		if strings.HasPrefix(line, "Error summary:") {
			errStr := strings.TrimSpace(strings.TrimPrefix(line, "Error summary:"))
			result["dataErrors"] = errStr
			if strings.Contains(errStr, "no errors") {
				result["errors"] = 0
				result["lastErrors"] = 0
			} else {
				// Count errors from "csum=3" or similar
				totalErrs := 0
				for _, part := range strings.Split(errStr, " ") {
					if strings.Contains(part, "=") {
						kv := strings.SplitN(part, "=", 2)
						if len(kv) == 2 {
							n, _ := strconv.Atoi(kv[1])
							totalErrs += n
						}
					}
				}
				result["errors"] = totalErrs
				result["lastErrors"] = totalErrs
			}
		}

		if strings.HasPrefix(line, "Total to scrub:") {
			result["totalSize"] = strings.TrimSpace(strings.TrimPrefix(line, "Total to scrub:"))
		}
	}

	return result
}

// formatDuration converts a time.Duration to "HH:MM:SS"
func formatDuration(d time.Duration) string {
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	s := int(d.Seconds()) % 60
	return fmt.Sprintf("%02d:%02d:%02d", h, m, s)
}

// ─── DATASETS ────────────────────────────────────────────────────────────────

// listDatasets returns all datasets for a pool
// GET /api/storage/datasets?pool=NAME
func listDatasets(poolName string) map[string]interface{} {
	zpoolName := resolveZpoolName(poolName)
	if zpoolName == "" {
		return map[string]interface{}{"datasets": []interface{}{}}
	}

	opts := CmdOptions{Timeout: 15 * time.Second}
	res, err := runCmd("zfs", []string{
		"list", "-H",
		"-o", "name,used,avail,quota,mountpoint,type",
		"-r", zpoolName,
	}, opts)
	if err != nil || res.Stdout == "" {
		return map[string]interface{}{"datasets": []interface{}{}}
	}

	var datasets []interface{}
	for _, line := range strings.Split(strings.TrimSpace(res.Stdout), "\n") {
		parts := strings.Fields(line)
		if len(parts) < 6 {
			continue
		}
		fullName := parts[0]
		// Skip the root dataset
		if fullName == zpoolName {
			continue
		}

		used := parseZfsSize(parts[1])
		avail := parseZfsSize(parts[2])
		quota := int64(0)
		if parts[3] != "none" && parts[3] != "-" {
			quota = parseZfsSize(parts[3])
		}
		mountpoint := parts[4]
		dsType := parts[5]

		datasets = append(datasets, map[string]interface{}{
			"name":       fullName,
			"pool":       poolName,
			"used":       used,
			"avail":      avail,
			"quota":      quota,
			"mountpoint": mountpoint,
			"type":       dsType,
		})
	}

	if datasets == nil {
		datasets = []interface{}{}
	}
	return map[string]interface{}{"datasets": datasets}
}

// createDataset creates a new ZFS dataset
// POST /api/storage/dataset { pool, name, quota }
func createDataset(body map[string]interface{}) map[string]interface{} {
	pool := bodyStr(body, "pool")
	name := bodyStr(body, "name")
	quotaRaw, _ := body["quota"].(float64)
	quota := int64(quotaRaw)

	zpoolName := resolveZpoolName(pool)
	if zpoolName == "" {
		return map[string]interface{}{"ok": false, "error": "Pool not found"}
	}
	if name == "" {
		return map[string]interface{}{"ok": false, "error": "Dataset name required"}
	}

	fullName := zpoolName + "/" + name
	opts := CmdOptions{Timeout: 15 * time.Second}

	// Check if already exists
	existing, _ := runCmd("zfs", []string{"list", "-H", "-o", "name", fullName}, opts)
	if strings.TrimSpace(existing.Stdout) != "" {
		return map[string]interface{}{"ok": false, "error": "Dataset already exists"}
	}

	// Create
	_, err := runCmd("zfs", []string{"create", "-p", fullName}, opts)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("create failed: %s", err)}
	}

	// Set quota if specified (in bytes)
	if quota > 0 {
		runCmd("zfs", []string{"set", fmt.Sprintf("quota=%d", quota), fullName}, opts)
	}

	logMsg("ZFS dataset created: %s (quota: %d)", fullName, quota)
	return map[string]interface{}{"ok": true}
}

// deleteDataset deletes a ZFS dataset
// DELETE /api/storage/dataset { dataset: "pool/name" }
func deleteDataset(body map[string]interface{}) map[string]interface{} {
	dataset := bodyStr(body, "dataset")
	if dataset == "" {
		return map[string]interface{}{"ok": false, "error": "Dataset name required"}
	}

	// Safety: don't delete root or system datasets
	parts := strings.Split(dataset, "/")
	if len(parts) < 2 {
		return map[string]interface{}{"ok": false, "error": "Cannot delete root dataset"}
	}

	opts := CmdOptions{Timeout: 30 * time.Second}

	// Check for children
	childRes, _ := runCmd("zfs", []string{"list", "-H", "-o", "name", "-r", dataset}, opts)
	if childRes.Stdout != "" {
		children := strings.Split(strings.TrimSpace(childRes.Stdout), "\n")
		// First line is the dataset itself, rest are children
		if len(children) > 1 {
			return map[string]interface{}{"ok": false, "error": "dataset has children"}
		}
	}

	_, err := runCmd("zfs", []string{"destroy", dataset}, opts)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("delete failed: %s", err)}
	}

	logMsg("ZFS dataset deleted: %s", dataset)
	return map[string]interface{}{"ok": true}
}

// ─── Helpers ─────────────────────────────────────────────────────────────────

// parseZfsSize converts ZFS human-readable sizes (e.g. "1.5G", "256K", "77.0M") to bytes
func parseZfsSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" || s == "none" {
		return 0
	}

	multiplier := int64(1)
	if strings.HasSuffix(s, "T") {
		multiplier = 1024 * 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "T")
	} else if strings.HasSuffix(s, "G") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "G")
	} else if strings.HasSuffix(s, "M") {
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "M")
	} else if strings.HasSuffix(s, "K") {
		multiplier = 1024
		s = strings.TrimSuffix(s, "K")
	} else if strings.HasSuffix(s, "B") {
		s = strings.TrimSuffix(s, "B")
	}

	val, err := strconv.ParseFloat(s, 64)
	if err != nil {
		return 0
	}
	return int64(val * float64(multiplier))
}
