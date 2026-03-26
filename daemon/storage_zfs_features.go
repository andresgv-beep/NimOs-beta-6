package main

// ═══════════════════════════════════════════════════════════════════════════════
// NimOS Storage — ZFS Features (Snapshots, Scrub, Datasets)
// Endpoints match Sonnet's UI contract exactly.
// ═══════════════════════════════════════════════════════════════════════════════

import (
	"fmt"
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

// startScrub starts a ZFS scrub
// POST /api/storage/scrub { pool }
func startScrub(body map[string]interface{}) map[string]interface{} {
	pool := bodyStr(body, "pool")
	zpoolName := resolveZpoolName(pool)
	if zpoolName == "" {
		return map[string]interface{}{"ok": false, "error": "Pool not found"}
	}

	opts := CmdOptions{Timeout: 15 * time.Second}
	_, err := runCmd("zpool", []string{"scrub", zpoolName}, opts)
	if err != nil {
		return map[string]interface{}{"ok": false, "error": fmt.Sprintf("scrub failed: %s", err)}
	}

	logMsg("ZFS scrub started on %s", zpoolName)
	return map[string]interface{}{"ok": true}
}

// getScrubStatus returns scrub progress
// GET /api/storage/scrub/status?pool=NAME
func getScrubStatus(poolName string) map[string]interface{} {
	zpoolName := resolveZpoolName(poolName)
	if zpoolName == "" {
		return map[string]interface{}{"status": "error", "error": "Pool not found"}
	}

	opts := CmdOptions{Timeout: 10 * time.Second}
	res, _ := runCmd("zpool", []string{"status", zpoolName}, opts)
	output := res.Stdout

	result := map[string]interface{}{
		"status":   "idle",
		"progress": 0,
		"errors":   0,
	}

	if strings.Contains(output, "scan: scrub in progress") {
		result["status"] = "scrubbing"
		// Parse progress percentage
		for _, line := range strings.Split(output, "\n") {
			line = strings.TrimSpace(line)
			if strings.Contains(line, "% done") {
				// Extract percentage like "42.50% done"
				for _, word := range strings.Fields(line) {
					if strings.HasSuffix(word, "%") {
						pct, _ := strconv.ParseFloat(strings.TrimSuffix(word, "%"), 64)
						result["progress"] = int(pct)
						break
					}
				}
			}
		}
	} else if strings.Contains(output, "scan: scrub repaired") {
		result["status"] = "done"
		if strings.Contains(output, "with 0 errors") {
			result["errors"] = 0
		} else {
			// Try to parse error count
			for _, line := range strings.Split(output, "\n") {
				if strings.Contains(line, "errors") {
					for _, word := range strings.Fields(line) {
						if n, err := strconv.Atoi(word); err == nil && n > 0 {
							result["errors"] = n
							break
						}
					}
				}
			}
		}
	} else if strings.Contains(output, "scan: scrub canceled") {
		result["status"] = "idle"
	}

	return result
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
