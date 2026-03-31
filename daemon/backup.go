package main

// ═══════════════════════════════════════════════════════════════════════════════
// NimOS Backup — Device Pairing, Backup Jobs & Sync
// Handles ZFS/Btrfs send/receive, device management, scheduling, and history.
// ═══════════════════════════════════════════════════════════════════════════════

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// ─── Database Tables ────────────────────────────────────────────────────────

func createBackupTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS backup_devices (
		id           TEXT PRIMARY KEY,
		name         TEXT NOT NULL,
		addr         TEXT NOT NULL,
		type         TEXT NOT NULL DEFAULT 'nas',
		purposes     TEXT DEFAULT '[]',
		sync_pairs   TEXT DEFAULT '[]',
		wg_active    INTEGER DEFAULT 0,
		wg_public_key TEXT DEFAULT '',
		wg_endpoint  TEXT DEFAULT '',
		wg_allowed_ips TEXT DEFAULT '',
		wg_local_ip  TEXT DEFAULT '',
		created_at   TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS backup_jobs (
		id           TEXT PRIMARY KEY,
		name         TEXT NOT NULL,
		device_id    TEXT NOT NULL,
		fs_type      TEXT NOT NULL,
		source       TEXT NOT NULL,
		dest         TEXT NOT NULL,
		schedule     TEXT NOT NULL DEFAULT 'daily 02:00',
		retention    TEXT NOT NULL DEFAULT '30d',
		status       TEXT NOT NULL DEFAULT 'ok',
		last_run     TEXT DEFAULT '',
		next_run     TEXT DEFAULT '',
		last_size    INTEGER DEFAULT 0,
		last_snap    TEXT DEFAULT '',
		enabled      INTEGER DEFAULT 1,
		created_at   TEXT NOT NULL,
		FOREIGN KEY (device_id) REFERENCES backup_devices(id) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS backup_history (
		id           TEXT PRIMARY KEY,
		job_id       TEXT NOT NULL,
		job_name     TEXT NOT NULL,
		device_id    TEXT NOT NULL,
		dest         TEXT NOT NULL,
		ok           INTEGER NOT NULL,
		bytes        INTEGER DEFAULT 0,
		duration     INTEGER DEFAULT 0,
		error        TEXT DEFAULT '',
		time         TEXT NOT NULL
	);

	CREATE INDEX IF NOT EXISTS idx_backup_jobs_device ON backup_jobs(device_id);
	CREATE INDEX IF NOT EXISTS idx_backup_history_job ON backup_history(job_id);
	CREATE INDEX IF NOT EXISTS idx_backup_history_device ON backup_history(device_id);
	CREATE INDEX IF NOT EXISTS idx_backup_history_time ON backup_history(time DESC);

	CREATE TABLE IF NOT EXISTS remote_mounts (
		device_id    TEXT NOT NULL,
		share_name   TEXT NOT NULL,
		remote_path  TEXT NOT NULL,
		mount_point  TEXT NOT NULL,
		device_addr  TEXT NOT NULL,
		created_at   TEXT NOT NULL,
		PRIMARY KEY (device_id, share_name)
	);
	`
	_, err := db.Exec(schema)
	return err
}

// ─── ID Generation ──────────────────────────────────────────────────────────

func backupID(prefix string) string {
	return fmt.Sprintf("%s_%d", prefix, time.Now().UnixNano()/1e6)
}

// ─── Device DB Operations ───────────────────────────────────────────────────

func dbBackupDeviceCreate(dev map[string]interface{}) error {
	id, _ := dev["id"].(string)
	name, _ := dev["name"].(string)
	addr, _ := dev["addr"].(string)
	devType, _ := dev["type"].(string)
	if devType == "" {
		devType = "nas"
	}

	purposesJSON := "[]"
	if p, ok := dev["purposes"]; ok {
		if b, err := json.Marshal(p); err == nil {
			purposesJSON = string(b)
		}
	}

	syncPairsJSON := "[]"
	if sp, ok := dev["syncPairs"]; ok {
		if b, err := json.Marshal(sp); err == nil {
			syncPairsJSON = string(b)
		}
	}

	_, err := db.Exec(`INSERT INTO backup_devices (id, name, addr, type, purposes, sync_pairs, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		id, name, addr, devType, purposesJSON, syncPairsJSON,
		time.Now().UTC().Format(time.RFC3339))
	return err
}

func dbBackupDeviceList() ([]map[string]interface{}, error) {
	rows, err := db.Query(`SELECT id, name, addr, type, purposes, sync_pairs, wg_active,
		wg_public_key, wg_endpoint, wg_allowed_ips, wg_local_ip, created_at FROM backup_devices ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var devices []map[string]interface{}
	for rows.Next() {
		var id, name, addr, devType, purposesJSON, syncPairsJSON string
		var wgActive int
		var wgPub, wgEndpoint, wgAllowed, wgLocal, createdAt string

		if err := rows.Scan(&id, &name, &addr, &devType, &purposesJSON, &syncPairsJSON,
			&wgActive, &wgPub, &wgEndpoint, &wgAllowed, &wgLocal, &createdAt); err != nil {
			continue
		}

		dev := map[string]interface{}{
			"id":        id,
			"name":      name,
			"addr":      addr,
			"type":      devType,
			"createdAt": createdAt,
		}

		// Parse purposes JSON
		var purposes []string
		if json.Unmarshal([]byte(purposesJSON), &purposes) == nil {
			dev["purposes"] = purposes
		} else {
			dev["purposes"] = []string{}
		}

		// Parse sync pairs JSON
		var syncPairs []interface{}
		if json.Unmarshal([]byte(syncPairsJSON), &syncPairs) == nil {
			dev["syncPairs"] = syncPairs
		} else {
			dev["syncPairs"] = []interface{}{}
		}

		// WireGuard info (only if active)
		if wgActive == 1 {
			dev["wireguard"] = map[string]interface{}{
				"active":     true,
				"publicKey":  wgPub,
				"endpoint":   wgEndpoint,
				"allowedIPs": wgAllowed,
				"localIP":    wgLocal,
			}
		}

		devices = append(devices, dev)
	}

	if devices == nil {
		devices = []map[string]interface{}{}
	}
	return devices, nil
}

func dbBackupDeviceGet(id string) (map[string]interface{}, error) {
	devices, err := dbBackupDeviceList()
	if err != nil {
		return nil, err
	}
	for _, d := range devices {
		if d["id"] == id {
			return d, nil
		}
	}
	return nil, fmt.Errorf("device not found: %s", id)
}

func dbBackupDeviceDelete(id string) error {
	res, err := db.Exec(`DELETE FROM backup_devices WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("device not found")
	}
	// Cascade: also remove jobs and history for this device
	db.Exec(`DELETE FROM backup_jobs WHERE device_id = ?`, id)
	db.Exec(`DELETE FROM backup_history WHERE device_id = ?`, id)
	// Remove WireGuard peer if exists
	removeWGPeer(id) // Errors are non-fatal — peer may not exist
	return nil
}

func dbBackupDeviceUpdatePurposes(id string, purposes []string) error {
	b, err := json.Marshal(purposes)
	if err != nil {
		return err
	}
	res, err := db.Exec(`UPDATE backup_devices SET purposes = ? WHERE id = ?`, string(b), id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("device not found")
	}
	return nil
}

func dbBackupDeviceUpdateSyncPairs(id string, pairs interface{}) error {
	b, err := json.Marshal(pairs)
	if err != nil {
		return err
	}
	_, err = db.Exec(`UPDATE backup_devices SET sync_pairs = ? WHERE id = ?`, string(b), id)
	return err
}

func dbBackupDeviceUpdate(id, field, value string) error {
	// Only allow safe fields to be updated
	allowed := map[string]bool{"addr": true, "name": true, "type": true}
	if !allowed[field] {
		return fmt.Errorf("field %s not updatable", field)
	}
	_, err := db.Exec(fmt.Sprintf(`UPDATE backup_devices SET %s = ? WHERE id = ?`, field), value, id)
	return err
}

// ─── Job DB Operations ──────────────────────────────────────────────────────

func dbBackupJobCreate(job map[string]interface{}) error {
	id, _ := job["id"].(string)
	name, _ := job["name"].(string)
	deviceID, _ := job["deviceId"].(string)
	fsType, _ := job["fsType"].(string)
	source, _ := job["source"].(string)
	dest, _ := job["dest"].(string)
	schedule, _ := job["schedule"].(string)
	retention, _ := job["retention"].(string)

	if schedule == "" {
		schedule = "daily 02:00"
	}
	if retention == "" {
		retention = "30d"
	}

	nextRun := computeNextRun(schedule)

	_, err := db.Exec(`INSERT INTO backup_jobs (id, name, device_id, fs_type, source, dest, schedule, retention, status, next_run, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, 'ok', ?, ?)`,
		id, name, deviceID, fsType, source, dest, schedule, retention,
		nextRun, time.Now().UTC().Format(time.RFC3339))
	return err
}

func dbBackupJobList() ([]map[string]interface{}, error) {
	rows, err := db.Query(`SELECT id, name, device_id, fs_type, source, dest, schedule, retention,
		status, last_run, next_run, last_size, last_snap, enabled, created_at FROM backup_jobs ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []map[string]interface{}
	for rows.Next() {
		var id, name, deviceID, fsType, source, dest, schedule, retention string
		var status, lastRun, nextRun, lastSnap, createdAt string
		var lastSize int64
		var enabled int

		if err := rows.Scan(&id, &name, &deviceID, &fsType, &source, &dest, &schedule, &retention,
			&status, &lastRun, &nextRun, &lastSize, &lastSnap, &enabled, &createdAt); err != nil {
			continue
		}

		jobs = append(jobs, map[string]interface{}{
			"id":        id,
			"name":      name,
			"deviceId":  deviceID,
			"fsType":    fsType,
			"source":    source,
			"dest":      dest,
			"schedule":  schedule,
			"retention": retention,
			"status":    status,
			"lastRun":   lastRun,
			"nextRun":   nextRun,
			"lastSize":  lastSize,
			"lastSnap":  lastSnap,
			"enabled":   enabled == 1,
			"createdAt": createdAt,
		})
	}

	if jobs == nil {
		jobs = []map[string]interface{}{}
	}
	return jobs, nil
}

func dbBackupJobGet(id string) (map[string]interface{}, error) {
	jobs, err := dbBackupJobList()
	if err != nil {
		return nil, err
	}
	for _, j := range jobs {
		if j["id"] == id {
			return j, nil
		}
	}
	return nil, fmt.Errorf("job not found: %s", id)
}

func dbBackupJobUpdate(id string, fields map[string]interface{}) error {
	// Build dynamic UPDATE — only update fields that are provided
	sets := []string{}
	args := []interface{}{}

	allowed := map[string]string{
		"name": "name", "schedule": "schedule", "retention": "retention",
		"source": "source", "dest": "dest", "status": "status",
		"lastRun": "last_run", "nextRun": "next_run", "lastSize": "last_size",
		"lastSnap": "last_snap", "enabled": "enabled",
	}

	for jsonKey, dbCol := range allowed {
		if v, ok := fields[jsonKey]; ok {
			sets = append(sets, dbCol+" = ?")
			// Handle bool → int for enabled
			if jsonKey == "enabled" {
				if b, ok := v.(bool); ok {
					if b {
						args = append(args, 1)
					} else {
						args = append(args, 0)
					}
					continue
				}
			}
			args = append(args, v)
		}
	}

	if len(sets) == 0 {
		return fmt.Errorf("no valid fields to update")
	}

	args = append(args, id)
	query := fmt.Sprintf("UPDATE backup_jobs SET %s WHERE id = ?", strings.Join(sets, ", "))
	res, err := db.Exec(query, args...)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("job not found")
	}
	return nil
}

func dbBackupJobDelete(id string) error {
	res, err := db.Exec(`DELETE FROM backup_jobs WHERE id = ?`, id)
	if err != nil {
		return err
	}
	n, _ := res.RowsAffected()
	if n == 0 {
		return fmt.Errorf("job not found")
	}
	return nil
}

// ─── History DB Operations ──────────────────────────────────────────────────

func dbBackupHistoryAdd(entry map[string]interface{}) error {
	id, _ := entry["id"].(string)
	jobID, _ := entry["jobId"].(string)
	jobName, _ := entry["jobName"].(string)
	deviceID, _ := entry["deviceId"].(string)
	dest, _ := entry["dest"].(string)
	ok := false
	if v, exists := entry["ok"]; exists {
		ok, _ = v.(bool)
	}
	var bytes int64
	if v, exists := entry["bytes"]; exists {
		switch b := v.(type) {
		case float64:
			bytes = int64(b)
		case int64:
			bytes = b
		case int:
			bytes = int64(b)
		}
	}
	duration := 0
	if v, exists := entry["duration"]; exists {
		switch d := v.(type) {
		case float64:
			duration = int(d)
		case int:
			duration = d
		}
	}
	errMsg, _ := entry["error"].(string)

	okInt := 0
	if ok {
		okInt = 1
	}

	_, err := db.Exec(`INSERT INTO backup_history (id, job_id, job_name, device_id, dest, ok, bytes, duration, error, time)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		id, jobID, jobName, deviceID, dest, okInt, bytes, duration, errMsg,
		time.Now().UTC().Format(time.RFC3339))
	return err
}

func dbBackupHistoryList(deviceID string, limit int) ([]map[string]interface{}, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}

	query := `SELECT id, job_id, job_name, device_id, dest, ok, bytes, duration, error, time
		FROM backup_history ORDER BY time DESC LIMIT ?`
	args := []interface{}{limit}

	if deviceID != "" {
		query = `SELECT id, job_id, job_name, device_id, dest, ok, bytes, duration, error, time
			FROM backup_history WHERE device_id = ? ORDER BY time DESC LIMIT ?`
		args = []interface{}{deviceID, limit}
	}

	rows, err := db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var history []map[string]interface{}
	for rows.Next() {
		var id, jobID, jobName, devID, dest, errMsg, ts string
		var ok int
		var bytes int64
		var duration int

		if err := rows.Scan(&id, &jobID, &jobName, &devID, &dest, &ok, &bytes, &duration, &errMsg, &ts); err != nil {
			continue
		}

		history = append(history, map[string]interface{}{
			"id":       id,
			"jobId":    jobID,
			"jobName":  jobName,
			"deviceId": devID,
			"dest":     dest,
			"ok":       ok == 1,
			"bytes":    bytes,
			"duration": duration,
			"error":    errMsg,
			"time":     ts,
		})
	}

	if history == nil {
		history = []map[string]interface{}{}
	}
	return history, nil
}

// ─── Schedule Parsing ───────────────────────────────────────────────────────

// computeNextRun parses a schedule string and returns the next run time as ISO 8601.
// Supported formats:
//   - "daily HH:MM"       → every day at HH:MM UTC
//   - "weekly DAY HH:MM"  → every week on DAY at HH:MM UTC (mon, tue, wed, thu, fri, sat, sun)
//   - "hourly"            → every hour at :00
//   - "every Nh"          → every N hours from now
//   - "every Nm"          → every N minutes from now
func computeNextRun(schedule string) string {
	now := time.Now().UTC()
	parts := strings.Fields(strings.ToLower(schedule))

	if len(parts) == 0 {
		return now.Add(24 * time.Hour).Format(time.RFC3339)
	}

	switch parts[0] {
	case "daily":
		if len(parts) >= 2 {
			hm := strings.Split(parts[1], ":")
			if len(hm) == 2 {
				h := parseInt(hm[0], 2)
				m := parseInt(hm[1], 0)
				next := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, time.UTC)
				if next.Before(now) {
					next = next.Add(24 * time.Hour)
				}
				return next.Format(time.RFC3339)
			}
		}
		// Default: next day same time
		return now.Add(24 * time.Hour).Format(time.RFC3339)

	case "weekly":
		if len(parts) >= 3 {
			dayMap := map[string]time.Weekday{
				"mon": time.Monday, "tue": time.Tuesday, "wed": time.Wednesday,
				"thu": time.Thursday, "fri": time.Friday, "sat": time.Saturday, "sun": time.Sunday,
			}
			targetDay, ok := dayMap[parts[1]]
			if !ok {
				return now.Add(7 * 24 * time.Hour).Format(time.RFC3339)
			}
			hm := strings.Split(parts[2], ":")
			h, m := 2, 0
			if len(hm) == 2 {
				h = parseInt(hm[0], 2)
				m = parseInt(hm[1], 0)
			}
			daysUntil := int(targetDay-now.Weekday()+7) % 7
			if daysUntil == 0 {
				candidate := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, time.UTC)
				if candidate.Before(now) {
					daysUntil = 7
				}
			}
			next := time.Date(now.Year(), now.Month(), now.Day()+daysUntil, h, m, 0, 0, time.UTC)
			return next.Format(time.RFC3339)
		}
		return now.Add(7 * 24 * time.Hour).Format(time.RFC3339)

	case "hourly":
		next := now.Truncate(time.Hour).Add(time.Hour)
		return next.Format(time.RFC3339)

	case "every":
		if len(parts) >= 2 {
			s := parts[1]
			if strings.HasSuffix(s, "h") {
				n := parseInt(strings.TrimSuffix(s, "h"), 1)
				return now.Add(time.Duration(n) * time.Hour).Format(time.RFC3339)
			}
			if strings.HasSuffix(s, "m") {
				n := parseInt(strings.TrimSuffix(s, "m"), 60)
				return now.Add(time.Duration(n) * time.Minute).Format(time.RFC3339)
			}
		}
		return now.Add(24 * time.Hour).Format(time.RFC3339)
	}

	// Fallback: try "HH:MM" as daily
	if len(parts) == 1 && strings.Contains(parts[0], ":") {
		hm := strings.Split(parts[0], ":")
		if len(hm) == 2 {
			h := parseInt(hm[0], 2)
			m := parseInt(hm[1], 0)
			next := time.Date(now.Year(), now.Month(), now.Day(), h, m, 0, 0, time.UTC)
			if next.Before(now) {
				next = next.Add(24 * time.Hour)
			}
			return next.Format(time.RFC3339)
		}
	}

	return now.Add(24 * time.Hour).Format(time.RFC3339)
}

// parseInt parses a string to int, returning fallback on failure.
func parseInt(s string, fallback int) int {
	n := 0
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int(c-'0')
		}
	}
	if n == 0 {
		return fallback
	}
	return n
}

// ─── Retention Parsing ──────────────────────────────────────────────────────

// parseRetention converts retention strings like "30d", "12", "7d", "4w" into a duration.
// If it's just a number, it's treated as count of snapshots to keep (handled elsewhere).
// Returns the max age as duration, or 0 if count-based.
func parseRetention(retention string) (time.Duration, int) {
	s := strings.TrimSpace(strings.ToLower(retention))
	if s == "" {
		return 30 * 24 * time.Hour, 0 // default 30 days
	}

	if strings.HasSuffix(s, "d") {
		n := parseInt(strings.TrimSuffix(s, "d"), 30)
		return time.Duration(n) * 24 * time.Hour, 0
	}
	if strings.HasSuffix(s, "w") {
		n := parseInt(strings.TrimSuffix(s, "w"), 4)
		return time.Duration(n) * 7 * 24 * time.Hour, 0
	}
	if strings.HasSuffix(s, "m") {
		n := parseInt(strings.TrimSuffix(s, "m"), 1)
		return time.Duration(n) * 30 * 24 * time.Hour, 0
	}

	// Pure number → count-based retention
	n := parseInt(s, 30)
	return 0, n
}

// ─── Backup Execution ───────────────────────────────────────────────────────

// backupRunningJobs tracks currently running jobs to prevent double execution
var (
	backupRunningJobs   = map[string]bool{}
	backupRunningJobsMu sync.Mutex
)

// executeBackupJob runs a backup job synchronously.
// It creates a snapshot, sends incremental data to the remote, and records history.
func executeBackupJob(job map[string]interface{}) map[string]interface{} {
	jobID, _ := job["id"].(string)
	jobName, _ := job["name"].(string)
	deviceID, _ := job["deviceId"].(string)
	fsType, _ := job["fsType"].(string)
	source, _ := job["source"].(string)
	dest, _ := job["dest"].(string)
	lastSnap, _ := job["lastSnap"].(string)

	// Prevent double execution
	backupRunningJobsMu.Lock()
	if backupRunningJobs[jobID] {
		backupRunningJobsMu.Unlock()
		return map[string]interface{}{"error": "Job is already running"}
	}
	backupRunningJobs[jobID] = true
	backupRunningJobsMu.Unlock()
	defer func() {
		backupRunningJobsMu.Lock()
		delete(backupRunningJobs, jobID)
		backupRunningJobsMu.Unlock()
	}()

	// Update status → running
	dbBackupJobUpdate(jobID, map[string]interface{}{"status": "running"})

	// Get device address for SSH
	device, err := dbBackupDeviceGet(deviceID)
	if err != nil {
		recordBackupFailure(jobID, jobName, deviceID, dest, "device not found: "+err.Error())
		return map[string]interface{}{"error": "Device not found"}
	}

	remoteAddr, _ := device["addr"].(string)
	startTime := time.Now()

	// Determine the right transport address
	// If WireGuard is active, use the WG local IP of the remote
	if wg, ok := device["wireguard"].(map[string]interface{}); ok {
		if active, _ := wg["active"].(bool); active {
			if wgIP, _ := wg["localIP"].(string); wgIP != "" {
				// Strip CIDR notation if present
				if idx := strings.Index(wgIP, "/"); idx > 0 {
					wgIP = wgIP[:idx]
				}
				remoteAddr = wgIP
			}
		}
	}

	timestamp := time.Now().UTC().Format("20060102-150405")
	var cmdStr string
	var snapName string

	switch fsType {
	case "zfs":
		snapName = fmt.Sprintf("nimbackup-%s", timestamp)
		fullSnap := fmt.Sprintf("%s@%s", source, snapName)

		// 1. Create local snapshot
		if out, ok := run(fmt.Sprintf("zfs snapshot %s", fullSnap)); !ok {
			recordBackupFailure(jobID, jobName, deviceID, dest, "snapshot failed: "+out)
			return map[string]interface{}{"error": "Failed to create snapshot: " + out}
		}

		// 2. Send (incremental if previous snapshot exists)
		if lastSnap != "" {
			cmdStr = fmt.Sprintf("zfs send -i %s@%s %s | ssh -o StrictHostKeyChecking=no -o ConnectTimeout=30 root@%s 'zfs receive -F %s'",
				source, lastSnap, fullSnap, remoteAddr, dest)
		} else {
			cmdStr = fmt.Sprintf("zfs send %s | ssh -o StrictHostKeyChecking=no -o ConnectTimeout=30 root@%s 'zfs receive -F %s'",
				fullSnap, remoteAddr, dest)
		}

	case "btrfs":
		snapName = fmt.Sprintf("nimbackup-%s", timestamp)
		snapPath := fmt.Sprintf("%s/.snapshots/%s", source, snapName)

		// 1. Ensure .snapshots directory exists
		run(fmt.Sprintf("mkdir -p %s/.snapshots", source))

		// 2. Create readonly snapshot
		if out, ok := run(fmt.Sprintf("btrfs subvolume snapshot -r %s %s", source, snapPath)); !ok {
			recordBackupFailure(jobID, jobName, deviceID, dest, "snapshot failed: "+out)
			return map[string]interface{}{"error": "Failed to create snapshot: " + out}
		}

		// 3. Send (incremental if previous snapshot exists)
		if lastSnap != "" {
			lastSnapPath := fmt.Sprintf("%s/.snapshots/%s", source, lastSnap)
			cmdStr = fmt.Sprintf("btrfs send -p %s %s | ssh -o StrictHostKeyChecking=no -o ConnectTimeout=30 root@%s 'btrfs receive %s'",
				lastSnapPath, snapPath, remoteAddr, dest)
		} else {
			cmdStr = fmt.Sprintf("btrfs send %s | ssh -o StrictHostKeyChecking=no -o ConnectTimeout=30 root@%s 'btrfs receive %s'",
				snapPath, remoteAddr, dest)
		}

	default:
		recordBackupFailure(jobID, jobName, deviceID, dest, "unsupported filesystem: "+fsType)
		return map[string]interface{}{"error": "Unsupported filesystem type: " + fsType}
	}

	// Execute the send/receive
	logMsg("backup: executing job %s → %s", jobName, remoteAddr)
	out, ok := run(cmdStr)

	elapsed := int(time.Since(startTime).Seconds())

	if !ok {
		recordBackupFailure(jobID, jobName, deviceID, dest, "send/receive failed: "+out)
		return map[string]interface{}{"error": "Backup failed: " + out}
	}

	// Estimate bytes transferred (from zfs/btrfs send output or fallback)
	var transferredBytes int64
	if fsType == "zfs" {
		// Try to get size from the snapshot
		if sizeOut, ok := run(fmt.Sprintf("zfs get -Hp -o value used %s@%s", source, snapName)); ok {
			transferredBytes = parseByteSize(sizeOut)
		}
	} else {
		// Btrfs: estimate from snapshot exclusive size
		snapPath := fmt.Sprintf("%s/.snapshots/%s", source, snapName)
		if sizeOut, ok := run(fmt.Sprintf("btrfs subvolume show %s 2>/dev/null | grep 'Exclusive' | awk '{print $NF}'", snapPath)); ok {
			transferredBytes = parseByteSize(sizeOut)
		}
	}

	// Record success
	schedule, _ := job["schedule"].(string)
	nextRun := computeNextRun(schedule)

	dbBackupJobUpdate(jobID, map[string]interface{}{
		"status":   "ok",
		"lastRun":  time.Now().UTC().Format(time.RFC3339),
		"nextRun":  nextRun,
		"lastSize": transferredBytes,
		"lastSnap": snapName,
	})

	dbBackupHistoryAdd(map[string]interface{}{
		"id":       backupID("hist"),
		"jobId":    jobID,
		"jobName":  jobName,
		"deviceId": deviceID,
		"dest":     dest,
		"ok":       true,
		"bytes":    transferredBytes,
		"duration": elapsed,
	})

	// Apply retention policy (clean old snapshots)
	go applyRetention(job)

	logMsg("backup: job %s completed in %ds, %d bytes", jobName, elapsed, transferredBytes)
	return map[string]interface{}{
		"ok":       true,
		"bytes":    transferredBytes,
		"duration": elapsed,
		"snapshot": snapName,
	}
}

func recordBackupFailure(jobID, jobName, deviceID, dest, errMsg string) {
	logMsg("backup: job %s failed: %s", jobName, errMsg)
	dbBackupJobUpdate(jobID, map[string]interface{}{
		"status":  "error",
		"lastRun": time.Now().UTC().Format(time.RFC3339),
	})
	dbBackupHistoryAdd(map[string]interface{}{
		"id":       backupID("hist"),
		"jobId":    jobID,
		"jobName":  jobName,
		"deviceId": deviceID,
		"dest":     dest,
		"ok":       false,
		"error":    errMsg,
	})
}

func parseByteSize(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" {
		return 0
	}
	var n int64
	for _, c := range s {
		if c >= '0' && c <= '9' {
			n = n*10 + int64(c-'0')
		}
	}
	// Handle suffixes (K, M, G, T)
	su := strings.ToUpper(s)
	if strings.HasSuffix(su, "K") || strings.HasSuffix(su, "KIB") {
		n *= 1024
	} else if strings.HasSuffix(su, "M") || strings.HasSuffix(su, "MIB") {
		n *= 1024 * 1024
	} else if strings.HasSuffix(su, "G") || strings.HasSuffix(su, "GIB") {
		n *= 1024 * 1024 * 1024
	} else if strings.HasSuffix(su, "T") || strings.HasSuffix(su, "TIB") {
		n *= 1024 * 1024 * 1024 * 1024
	}
	return n
}

// ─── Retention ──────────────────────────────────────────────────────────────

func applyRetention(job map[string]interface{}) {
	fsType, _ := job["fsType"].(string)
	source, _ := job["source"].(string)
	retention, _ := job["retention"].(string)

	maxAge, maxCount := parseRetention(retention)

	switch fsType {
	case "zfs":
		applyRetentionZFS(source, maxAge, maxCount)
	case "btrfs":
		applyRetentionBtrfs(source, maxAge, maxCount)
	}
}

func applyRetentionZFS(dataset string, maxAge time.Duration, maxCount int) {
	// List nimbackup snapshots for this dataset, sorted oldest first
	out, ok := run(fmt.Sprintf("zfs list -t snapshot -H -o name,creation -s creation %s 2>/dev/null | grep nimbackup", dataset))
	if !ok || out == "" {
		return
	}

	lines := strings.Split(out, "\n")
	var nimSnapshots []string
	for _, line := range lines {
		fields := strings.Fields(line)
		if len(fields) >= 1 && strings.Contains(fields[0], "nimbackup") {
			nimSnapshots = append(nimSnapshots, fields[0])
		}
	}

	if len(nimSnapshots) <= 1 {
		return // Always keep at least one
	}

	toDelete := []string{}

	if maxAge > 0 {
		// Time-based: delete snapshots older than maxAge
		cutoff := time.Now().UTC().Add(-maxAge)
		for _, snap := range nimSnapshots[:len(nimSnapshots)-1] { // never delete the latest
			ts := extractTimestamp(snap)
			if !ts.IsZero() && ts.Before(cutoff) {
				toDelete = append(toDelete, snap)
			}
		}
	} else if maxCount > 0 {
		// Count-based: keep only the last maxCount snapshots
		if len(nimSnapshots) > maxCount {
			toDelete = nimSnapshots[:len(nimSnapshots)-maxCount]
		}
	}

	for _, snap := range toDelete {
		logMsg("backup: retention cleanup — destroying %s", snap)
		run(fmt.Sprintf("zfs destroy %s", snap))
	}
}

func applyRetentionBtrfs(source string, maxAge time.Duration, maxCount int) {
	snapDir := fmt.Sprintf("%s/.snapshots", source)
	out, ok := run(fmt.Sprintf("ls -1 %s 2>/dev/null | grep nimbackup | sort", snapDir))
	if !ok || out == "" {
		return
	}

	snaps := strings.Split(strings.TrimSpace(out), "\n")
	if len(snaps) <= 1 {
		return
	}

	toDelete := []string{}

	if maxAge > 0 {
		cutoff := time.Now().UTC().Add(-maxAge)
		for _, snap := range snaps[:len(snaps)-1] {
			ts := extractTimestamp(snap)
			if !ts.IsZero() && ts.Before(cutoff) {
				toDelete = append(toDelete, snap)
			}
		}
	} else if maxCount > 0 {
		if len(snaps) > maxCount {
			toDelete = snaps[:len(snaps)-maxCount]
		}
	}

	for _, snap := range toDelete {
		snapPath := fmt.Sprintf("%s/%s", snapDir, snap)
		logMsg("backup: retention cleanup — deleting subvolume %s", snapPath)
		run(fmt.Sprintf("btrfs subvolume delete %s", snapPath))
	}
}

// extractTimestamp parses "nimbackup-YYYYMMDD-HHMMSS" from a snapshot name.
func extractTimestamp(name string) time.Time {
	re := regexp.MustCompile(`nimbackup-(\d{8}-\d{6})`)
	m := re.FindStringSubmatch(name)
	if len(m) < 2 {
		return time.Time{}
	}
	t, err := time.Parse("20060102-150405", m[1])
	if err != nil {
		return time.Time{}
	}
	return t
}

// ─── LAN Scanner ────────────────────────────────────────────────────────────

// DiscoveredDevice represents a NimOS device found on the LAN.
type DiscoveredDevice struct {
	Addr    string `json:"addr"`
	Name    string `json:"name"`
	Version string `json:"version"`
}

// scanLANForNimOS scans the given subnet for NimOS devices (port 5000).
// If subnet is empty, autodetects from the first non-loopback interface.
func scanLANForNimOS(subnet string) []DiscoveredDevice {
	if subnet == "" {
		subnet = detectSubnet()
	}
	if subnet == "" {
		return nil
	}

	// Parse subnet (we support /24 only for speed)
	base := subnet
	if idx := strings.LastIndex(base, "."); idx > 0 {
		base = base[:idx]
	}
	base = strings.TrimSuffix(base, "/24")

	var (
		mu      sync.Mutex
		results []DiscoveredDevice
		wg      sync.WaitGroup
	)

	for i := 1; i <= 254; i++ {
		addr := fmt.Sprintf("%s.%d", base, i)
		wg.Add(1)
		go func(addr string) {
			defer wg.Done()
			dev := probeNimOS(addr)
			if dev != nil {
				mu.Lock()
				results = append(results, *dev)
				mu.Unlock()
			}
		}(addr)
	}
	wg.Wait()

	// Sort by IP for consistent ordering
	sort.Slice(results, func(i, j int) bool {
		return results[i].Addr < results[j].Addr
	})

	return results
}

func probeNimOS(addr string) *DiscoveredDevice {
	// TCP connect with 400ms timeout
	conn, err := net.DialTimeout("tcp", addr+":5000", 400*time.Millisecond)
	if err != nil {
		return nil
	}
	conn.Close()

	// Verify it's NimOS by hitting /api/auth/status
	client := &http.Client{Timeout: 2 * time.Second}
	resp, err := client.Get(fmt.Sprintf("http://%s:5000/api/auth/status", addr))
	if err != nil {
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil
	}

	var data map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil
	}

	// NimOS /api/auth/status returns either:
	//   { "initialized": true/false, ... }   (newer versions)
	//   { "setup": true/false, ... }          (current/older versions)
	// Accept both — if either field exists, it's a NimOS device.
	_, hasInitialized := data["initialized"]
	_, hasSetup := data["setup"]
	_, hasHostname := data["hostname"]
	if !hasInitialized && !hasSetup && !hasHostname {
		return nil
	}

	name := "NimOS"
	if n, ok := data["hostname"].(string); ok && n != "" {
		name = n
	}
	version := "unknown"
	if v, ok := data["version"].(string); ok && v != "" {
		version = v
	}

	return &DiscoveredDevice{
		Addr:    addr,
		Name:    name,
		Version: version,
	}
}

// ─── Auto-Discovery Service ─────────────────────────────────────────────────

// discoveredDevices holds the latest LAN scan results, updated periodically.
var (
	discoveredDevices   []DiscoveredDevice
	discoveredDevicesMu sync.RWMutex
	discoveryCancel     context.CancelFunc
)

// startAutoDiscovery runs a background goroutine that scans the LAN every 60s
// for NimOS devices and keeps the list in memory.
func startAutoDiscovery() {
	ctx, cancel := context.WithCancel(context.Background())
	discoveryCancel = cancel

	// Run an initial scan immediately
	go func() {
		runDiscoveryScan()

		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logMsg("discovery: auto-discovery stopped")
				return
			case <-ticker.C:
				runDiscoveryScan()
			}
		}
	}()

	logMsg("discovery: auto-discovery started (60s interval)")
}

func stopAutoDiscovery() {
	if discoveryCancel != nil {
		discoveryCancel()
	}
}

// ─── Device Status Cache ────────────────────────────────────────────────────

// deviceStatusCache holds the latest status for each paired device, keyed by device ID.
var (
	deviceStatusCache   = map[string]map[string]interface{}{}
	deviceStatusCacheMu sync.RWMutex
)

// refreshPairedDeviceStatus pings all paired devices and caches their status.
func refreshPairedDeviceStatus() {
	devices, err := dbBackupDeviceList()
	if err != nil || len(devices) == 0 {
		return
	}

	var wg sync.WaitGroup
	var mu sync.Mutex
	newCache := map[string]map[string]interface{}{}

	for _, dev := range devices {
		dev := dev // capture
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, _ := dev["id"].(string)
			status := checkDeviceStatus(dev)
			mu.Lock()
			newCache[id] = status
			mu.Unlock()
		}()
	}
	wg.Wait()

	deviceStatusCacheMu.Lock()
	deviceStatusCache = newCache
	deviceStatusCacheMu.Unlock()
}

// getDeviceStatusCached returns cached status for a device, or a default offline status.
func getDeviceStatusCached(id string) map[string]interface{} {
	deviceStatusCacheMu.RLock()
	defer deviceStatusCacheMu.RUnlock()
	if s, ok := deviceStatusCache[id]; ok {
		return s
	}
	return map[string]interface{}{"online": false, "ping": "—"}
}

// enrichDevicesWithStatus adds online/ping/freeSpace/version to each device from cache.
func enrichDevicesWithStatus(devices []map[string]interface{}) {
	for _, dev := range devices {
		id, _ := dev["id"].(string)
		status := getDeviceStatusCached(id)
		for k, v := range status {
			dev[k] = v
		}
	}
}

func runDiscoveryScan() {
	// Get our own local addresses to exclude ourselves
	localAddrs := getLocalAddrs()

	devices := scanLANForNimOS("")

	// Filter out ourselves
	var filtered []DiscoveredDevice
	for _, d := range devices {
		if !localAddrs[d.Addr] {
			filtered = append(filtered, d)
		}
	}
	if filtered == nil {
		filtered = []DiscoveredDevice{}
	}

	discoveredDevicesMu.Lock()
	discoveredDevices = filtered
	discoveredDevicesMu.Unlock()

	if len(filtered) > 0 {
		names := make([]string, len(filtered))
		for i, d := range filtered {
			names[i] = fmt.Sprintf("%s(%s)", d.Name, d.Addr)
		}
		logMsg("discovery: found %d NimOS device(s): %s", len(filtered), strings.Join(names, ", "))
	}

	// Refresh paired device status in a separate goroutine so it doesn't
	// block the discovery cycle or hold DB connections during network timeouts
	go refreshPairedDeviceStatus()
}

func getDiscoveredDevices() []DiscoveredDevice {
	discoveredDevicesMu.RLock()
	defer discoveredDevicesMu.RUnlock()
	result := make([]DiscoveredDevice, len(discoveredDevices))
	copy(result, discoveredDevices)
	return result
}

func getLocalAddrs() map[string]bool {
	result := map[string]bool{"127.0.0.1": true, "localhost": true}
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return result
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
			result[ipnet.IP.String()] = true
		}
	}
	return result
}

func detectSubnet() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return ""
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			ip := ipnet.IP.To4()
			return fmt.Sprintf("%d.%d.%d.0/24", ip[0], ip[1], ip[2])
		}
	}
	return ""
}

// ─── Device Status (Ping) ───────────────────────────────────────────────────

func checkDeviceStatus(device map[string]interface{}) map[string]interface{} {
	addr, _ := device["addr"].(string)
	if addr == "" {
		return map[string]interface{}{"online": false, "ping": "—"}
	}

	// Check WireGuard address first
	if wg, ok := device["wireguard"].(map[string]interface{}); ok {
		if active, _ := wg["active"].(bool); active {
			if wgIP, _ := wg["localIP"].(string); wgIP != "" {
				if idx := strings.Index(wgIP, "/"); idx > 0 {
					wgIP = wgIP[:idx]
				}
				addr = wgIP
			}
		}
	}

	start := time.Now()
	conn, err := net.DialTimeout("tcp", addr+":5000", 3*time.Second)
	if err != nil {
		return map[string]interface{}{"online": false, "ping": "—"}
	}
	conn.Close()
	ping := time.Since(start)

	// Also get free space and version from remote
	client := &http.Client{Timeout: 3 * time.Second}
	result := map[string]interface{}{
		"online": true,
		"ping":   fmt.Sprintf("%.0fms", float64(ping.Microseconds())/1000.0),
	}

	resp, err := client.Get(fmt.Sprintf("http://%s:5000/api/auth/status", addr))
	if err == nil {
		defer resp.Body.Close()
		var data map[string]interface{}
		if json.NewDecoder(resp.Body).Decode(&data) == nil {
			if v, ok := data["version"].(string); ok {
				result["version"] = v
			}
			if h, ok := data["hostname"].(string); ok {
				result["hostname"] = h
			}
		}
	}

	// Get free space from remote storage endpoint (if available)
	resp2, err2 := client.Get(fmt.Sprintf("http://%s:5000/api/storage/status", addr))
	if err2 == nil {
		defer resp2.Body.Close()
		var sdata map[string]interface{}
		if json.NewDecoder(resp2.Body).Decode(&sdata) == nil {
			if pools, ok := sdata["pools"].([]interface{}); ok {
				var totalFree int64
				for _, p := range pools {
					if pm, ok := p.(map[string]interface{}); ok {
						if free, ok := pm["free"].(float64); ok {
							totalFree += int64(free)
						}
					}
				}
				if totalFree > 0 {
					result["freeSpace"] = formatBytes(totalFree)
				}
			}
		}
	}

	return result
}

// formatBytes is defined in hardware.go — reused here

// ─── Scheduler ──────────────────────────────────────────────────────────────

var backupSchedulerCancel context.CancelFunc

func startBackupScheduler() {
	ctx, cancel := context.WithCancel(context.Background())
	backupSchedulerCancel = cancel

	go func() {
		logMsg("backup: scheduler started")
		ticker := time.NewTicker(60 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				logMsg("backup: scheduler stopped")
				return
			case <-ticker.C:
				checkAndRunDueJobs()
			}
		}
	}()
}

func stopBackupScheduler() {
	if backupSchedulerCancel != nil {
		backupSchedulerCancel()
	}
}

func checkAndRunDueJobs() {
	jobs, err := dbBackupJobList()
	if err != nil {
		return
	}

	now := time.Now().UTC()

	for _, job := range jobs {
		enabled, _ := job["enabled"].(bool)
		if !enabled {
			continue
		}

		nextRunStr, _ := job["nextRun"].(string)
		if nextRunStr == "" {
			continue
		}

		nextRun, err := time.Parse(time.RFC3339, nextRunStr)
		if err != nil {
			continue
		}

		if now.After(nextRun) {
			// Time to run this job
			go executeBackupJob(job)
		}
	}
}

// ─── HTTP Route Handler ─────────────────────────────────────────────────────

func handleBackupRoutes(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path
	method := r.Method

	// Public endpoint: paired devices can list shares without full auth
	// (verified by checking if requester IP is a paired device)
	if urlPath == "/api/backup/public-shares" && method == "GET" {
		result := getPublicShares(r)
		if errMsg, ok := result["error"].(string); ok && errMsg != "" {
			jsonError(w, 403, errMsg)
			return
		}
		jsonOk(w, result)
		return
	}

	// Public endpoint: paired devices request NFS export of a share path
	if urlPath == "/api/backup/nfs-export" && method == "POST" {
		handleNFSExport(w, r)
		return
	}

	// All other backup routes require admin
	session := requireAdmin(w, r)
	if session == nil {
		return
	}

	// ── GET routes ──
	if method == "GET" {
		switch {
		// GET /api/backup/devices
		case urlPath == "/api/backup/devices":
			devices, err := dbBackupDeviceList()
			if err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			enrichDevicesWithStatus(devices)
			jsonOk(w, map[string]interface{}{"devices": devices})

		// GET /api/backup/devices/:id/status
		case strings.HasSuffix(urlPath, "/status") && strings.HasPrefix(urlPath, "/api/backup/devices/"):
			id := extractPathSegment(urlPath, "/api/backup/devices/", "/status")
			dev, err := dbBackupDeviceGet(id)
			if err != nil {
				jsonError(w, 404, err.Error())
				return
			}
			status := checkDeviceStatus(dev)
			jsonOk(w, status)

		// GET /api/backup/jobs
		case urlPath == "/api/backup/jobs":
			jobs, err := dbBackupJobList()
			if err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			jsonOk(w, map[string]interface{}{"jobs": jobs})

		// GET /api/backup/jobs/:id/status
		case strings.HasSuffix(urlPath, "/status") && strings.HasPrefix(urlPath, "/api/backup/jobs/"):
			id := extractPathSegment(urlPath, "/api/backup/jobs/", "/status")
			job, err := dbBackupJobGet(id)
			if err != nil {
				jsonError(w, 404, err.Error())
				return
			}
			// Return current status + running state
			backupRunningJobsMu.Lock()
			running := backupRunningJobs[id]
			backupRunningJobsMu.Unlock()
			result := map[string]interface{}{
				"status":  job["status"],
				"lastRun": job["lastRun"],
				"nextRun": job["nextRun"],
				"running": running,
			}
			jsonOk(w, result)

		// GET /api/backup/history
		case urlPath == "/api/backup/history":
			deviceID := r.URL.Query().Get("deviceId")
			limitStr := r.URL.Query().Get("limit")
			limit := 50
			if limitStr != "" {
				limit = parseInt(limitStr, 50)
			}
			history, err := dbBackupHistoryList(deviceID, limit)
			if err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			jsonOk(w, map[string]interface{}{"history": history})

		// GET /api/backup/snapshots
		case urlPath == "/api/backup/snapshots":
			pool := r.URL.Query().Get("pool")
			jsonOk(w, listBackupSnapshots(pool))

		// GET /api/backup/discovered — auto-discovered NimOS devices on LAN
		case urlPath == "/api/backup/discovered":
			devices := getDiscoveredDevices()
			jsonOk(w, map[string]interface{}{"devices": devices})

		// GET /api/backup/devices/:id/remote-shares — list shares available on remote device
		case strings.HasSuffix(urlPath, "/remote-shares") && strings.HasPrefix(urlPath, "/api/backup/devices/"):
			id := extractPathSegment(urlPath, "/api/backup/devices/", "/remote-shares")
			dev, err := dbBackupDeviceGet(id)
			if err != nil {
				jsonError(w, 404, err.Error())
				return
			}
			shares, err := fetchRemoteShares(dev)
			if err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			// Enrich with mount status
			mounted := listMountedRemoteShares(id)
			mountedMap := map[string]map[string]interface{}{}
			for _, m := range mounted {
				if sn, ok := m["shareName"].(string); ok {
					mountedMap[sn] = m
				}
			}
			for _, s := range shares {
				name, _ := s["name"].(string)
				if m, ok := mountedMap[name]; ok {
					s["mounted"] = m["mounted"]
					s["mountPoint"] = m["mountPoint"]
				} else {
					s["mounted"] = false
				}
			}
			jsonOk(w, map[string]interface{}{"shares": shares})

		// GET /api/backup/devices/:id/mounts — list currently mounted remote shares
		case strings.HasSuffix(urlPath, "/mounts") && strings.HasPrefix(urlPath, "/api/backup/devices/"):
			id := extractPathSegment(urlPath, "/api/backup/devices/", "/mounts")
			mounts := listMountedRemoteShares(id)
			jsonOk(w, map[string]interface{}{"mounts": mounts})

		// GET /api/backup/wg/* — WireGuard routes
		case strings.HasPrefix(urlPath, "/api/backup/wg/"):
			handleWGRoutes(w, r, urlPath, nil)

		default:
			jsonError(w, 404, "Not found")
		}
		return
	}

	// ── POST / PUT / DELETE routes ──
	if method == "POST" || method == "PUT" || method == "DELETE" {
		body, _ := readBody(r)

		switch {
		// POST /api/backup/devices — add paired device
		case urlPath == "/api/backup/devices" && method == "POST":
			name := bodyStr(body, "name")
			addr := bodyStr(body, "addr")
			devType := bodyStr(body, "type")

			// Clean addr: strip protocol, port, trailing slashes
			addr = strings.TrimSpace(addr)
			addr = strings.TrimPrefix(addr, "https://")
			addr = strings.TrimPrefix(addr, "http://")
			addr = strings.TrimRight(addr, "/")
			// Strip port if present (e.g., "nimosbarraca.duckdns.org:5009" → "nimosbarraca.duckdns.org")
			if idx := strings.LastIndex(addr, ":"); idx > 0 {
				portPart := addr[idx+1:]
				if _, err := strconv.Atoi(portPart); err == nil {
					addr = addr[:idx]
				}
			}

			if name == "" || addr == "" {
				jsonError(w, 400, "Name and addr are required")
				return
			}
			id := backupID("dev")
			dev := map[string]interface{}{
				"id":   id,
				"name": name,
				"addr": addr,
				"type": devType,
			}
			if purposes, ok := body["purposes"]; ok {
				dev["purposes"] = purposes
			}
			if err := dbBackupDeviceCreate(dev); err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			jsonOk(w, map[string]interface{}{"ok": true, "id": id})

		// DELETE /api/backup/devices/:id
		case strings.HasPrefix(urlPath, "/api/backup/devices/") && method == "DELETE":
			id := strings.TrimPrefix(urlPath, "/api/backup/devices/")
			id = strings.TrimSuffix(id, "/")
			if err := dbBackupDeviceDelete(id); err != nil {
				jsonError(w, 404, err.Error())
				return
			}
			jsonOk(w, map[string]interface{}{"ok": true})

		// POST /api/backup/devices/:id/purposes
		case strings.HasSuffix(urlPath, "/purposes") && strings.HasPrefix(urlPath, "/api/backup/devices/") && method == "POST":
			id := extractPathSegment(urlPath, "/api/backup/devices/", "/purposes")
			purposesRaw, ok := body["purposes"]
			if !ok {
				jsonError(w, 400, "Purposes array required")
				return
			}
			// Convert to []string
			var purposes []string
			if arr, ok := purposesRaw.([]interface{}); ok {
				for _, v := range arr {
					if s, ok := v.(string); ok {
						purposes = append(purposes, s)
					}
				}
			}
			if err := dbBackupDeviceUpdatePurposes(id, purposes); err != nil {
				jsonError(w, 404, err.Error())
				return
			}
			jsonOk(w, map[string]interface{}{"ok": true})

		// POST /api/backup/devices/:id/sync-pairs
		case strings.HasSuffix(urlPath, "/sync-pairs") && strings.HasPrefix(urlPath, "/api/backup/devices/") && method == "POST":
			id := extractPathSegment(urlPath, "/api/backup/devices/", "/sync-pairs")
			pairs, ok := body["syncPairs"]
			if !ok {
				jsonError(w, 400, "syncPairs required")
				return
			}
			if err := dbBackupDeviceUpdateSyncPairs(id, pairs); err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			jsonOk(w, map[string]interface{}{"ok": true})

		// POST /api/backup/jobs — create job
		case urlPath == "/api/backup/jobs" && method == "POST":
			name := bodyStr(body, "name")
			deviceID := bodyStr(body, "deviceId")
			fsType := bodyStr(body, "fsType")
			source := bodyStr(body, "source")
			dest := bodyStr(body, "dest")

			if name == "" || deviceID == "" || fsType == "" || source == "" || dest == "" {
				jsonError(w, 400, "name, deviceId, fsType, source, and dest are required")
				return
			}

			// Validate device exists
			if _, err := dbBackupDeviceGet(deviceID); err != nil {
				jsonError(w, 404, "Device not found")
				return
			}

			// Validate fsType
			if fsType != "zfs" && fsType != "btrfs" {
				jsonError(w, 400, "fsType must be 'zfs' or 'btrfs'")
				return
			}

			id := backupID("job")
			job := map[string]interface{}{
				"id":        id,
				"name":      name,
				"deviceId":  deviceID,
				"fsType":    fsType,
				"source":    source,
				"dest":      dest,
				"schedule":  bodyStr(body, "schedule"),
				"retention": bodyStr(body, "retention"),
			}
			if err := dbBackupJobCreate(job); err != nil {
				jsonError(w, 500, err.Error())
				return
			}
			jsonOk(w, map[string]interface{}{"ok": true, "id": id})

		// PUT /api/backup/jobs/:id — edit job
		case strings.HasPrefix(urlPath, "/api/backup/jobs/") && method == "PUT":
			id := strings.TrimPrefix(urlPath, "/api/backup/jobs/")
			id = strings.TrimSuffix(id, "/")
			if err := dbBackupJobUpdate(id, body); err != nil {
				jsonError(w, 404, err.Error())
				return
			}
			// Recalculate next run if schedule changed
			if _, ok := body["schedule"]; ok {
				schedule := bodyStr(body, "schedule")
				if schedule != "" {
					dbBackupJobUpdate(id, map[string]interface{}{"nextRun": computeNextRun(schedule)})
				}
			}
			jsonOk(w, map[string]interface{}{"ok": true})

		// DELETE /api/backup/jobs/:id
		case strings.HasPrefix(urlPath, "/api/backup/jobs/") && method == "DELETE":
			id := strings.TrimPrefix(urlPath, "/api/backup/jobs/")
			id = strings.TrimSuffix(id, "/")
			if err := dbBackupJobDelete(id); err != nil {
				jsonError(w, 404, err.Error())
				return
			}
			jsonOk(w, map[string]interface{}{"ok": true})

		// POST /api/backup/run/:id — execute job manually
		case strings.HasPrefix(urlPath, "/api/backup/run/") && method == "POST":
			id := strings.TrimPrefix(urlPath, "/api/backup/run/")
			id = strings.TrimSuffix(id, "/")
			job, err := dbBackupJobGet(id)
			if err != nil {
				jsonError(w, 404, err.Error())
				return
			}
			// Run in background, return immediately
			go executeBackupJob(job)
			jsonOk(w, map[string]interface{}{"ok": true, "message": "Backup started"})

		// POST /api/backup/pair/scan — scan LAN for NimOS devices (also refreshes auto-discovery)
		case urlPath == "/api/backup/pair/scan" && method == "POST":
			subnet := bodyStr(body, "subnet")
			localAddrs := getLocalAddrs()
			devices := scanLANForNimOS(subnet)
			// Filter out ourselves
			var filtered []DiscoveredDevice
			for _, d := range devices {
				if !localAddrs[d.Addr] {
					filtered = append(filtered, d)
				}
			}
			if filtered == nil {
				filtered = []DiscoveredDevice{}
			}
			// Update discovery cache
			discoveredDevicesMu.Lock()
			discoveredDevices = filtered
			discoveredDevicesMu.Unlock()
			jsonOk(w, map[string]interface{}{"devices": filtered})

		// POST /api/backup/pair/connect — initiate pairing with remote device
		case urlPath == "/api/backup/pair/connect" && method == "POST":
			addr := bodyStr(body, "addr")
			username := bodyStr(body, "username")
			password := bodyStr(body, "password")
			totpCode := bodyStr(body, "totpCode")

			// Clean addr: strip protocol, port, trailing slashes
			addr = strings.TrimSpace(addr)
			addr = strings.TrimPrefix(addr, "https://")
			addr = strings.TrimPrefix(addr, "http://")
			addr = strings.TrimRight(addr, "/")
			if idx := strings.LastIndex(addr, ":"); idx > 0 {
				portPart := addr[idx+1:]
				if _, err := strconv.Atoi(portPart); err == nil {
					addr = addr[:idx]
				}
			}

			if addr == "" || username == "" || password == "" {
				jsonError(w, 400, "addr, username, and password are required")
				return
			}
			result := pairWithRemote(addr, username, password, totpCode)
			if errMsg, ok := result["error"].(string); ok && errMsg != "" {
				jsonError(w, 400, errMsg)
				return
			}
			jsonOk(w, result)

		// POST /api/backup/pair/update-addr — remote tells us to use tunnel IP
		case urlPath == "/api/backup/pair/update-addr" && method == "POST":
			tunnelAddr := bodyStr(body, "tunnelAddr")
			if tunnelAddr == "" {
				jsonError(w, 400, "tunnelAddr required")
				return
			}
			// Find the device by the request's source IP and update its addr
			remoteIP := r.RemoteAddr
			if idx := strings.LastIndex(remoteIP, ":"); idx > 0 {
				remoteIP = remoteIP[:idx]
			}
			remoteIP = strings.Trim(remoteIP, "[]")

			devices, _ := dbBackupDeviceList()
			updated := false
			for _, d := range devices {
				dAddr, _ := d["addr"].(string)
				dID, _ := d["id"].(string)
				// Match by current addr (could be DDNS or IP)
				if dAddr == remoteIP || dAddr == tunnelAddr {
					dbBackupDeviceUpdate(dID, "addr", tunnelAddr)
					logMsg("wireguard: updated device %s addr to tunnel IP %s (requested by remote)", dID, tunnelAddr)
					updated = true
					break
				}
			}
			if !updated {
				// Try matching by any addr that isn't a local 192.168/10./172. addr
				for _, d := range devices {
					dAddr, _ := d["addr"].(string)
					dID, _ := d["id"].(string)
					if !isLocalAddr(dAddr) {
						dbBackupDeviceUpdate(dID, "addr", tunnelAddr)
						logMsg("wireguard: updated WAN device %s addr to tunnel IP %s", dID, tunnelAddr)
						updated = true
						break
					}
				}
			}
			jsonOk(w, map[string]interface{}{"ok": true, "updated": updated})

		// POST /api/backup/snapshots — create manual backup snapshot
		case urlPath == "/api/backup/snapshots" && method == "POST":
			source := bodyStr(body, "source")
			fsType := bodyStr(body, "fsType")
			if source == "" || fsType == "" {
				jsonError(w, 400, "source and fsType required")
				return
			}
			result := createBackupSnapshot(source, fsType)
			jsonOk(w, result)

		// DELETE /api/backup/snapshots/:name
		case strings.HasPrefix(urlPath, "/api/backup/snapshots/") && method == "DELETE":
			name := strings.TrimPrefix(urlPath, "/api/backup/snapshots/")
			name = strings.TrimSuffix(name, "/")
			fsType := bodyStr(body, "fsType")
			source := bodyStr(body, "source")
			result := deleteBackupSnapshot(name, fsType, source)
			jsonOk(w, result)

		// POST /api/backup/devices/:id/mount — mount a remote share
		case strings.HasSuffix(urlPath, "/mount") && strings.HasPrefix(urlPath, "/api/backup/devices/") && method == "POST":
			id := extractPathSegment(urlPath, "/api/backup/devices/", "/mount")
			dev, err := dbBackupDeviceGet(id)
			if err != nil {
				jsonError(w, 404, err.Error())
				return
			}
			shareName := bodyStr(body, "shareName")
			remotePath := bodyStr(body, "remotePath")
			if shareName == "" || remotePath == "" {
				jsonError(w, 400, "shareName and remotePath required")
				return
			}
			devName, _ := dev["name"].(string)
			devAddr, _ := dev["addr"].(string)
			result := mountRemoteShare(id, devName, devAddr, shareName, remotePath)
			if errMsg, ok := result["error"].(string); ok && errMsg != "" {
				jsonError(w, 500, errMsg)
				return
			}
			jsonOk(w, result)

		// POST /api/backup/devices/:id/unmount — unmount a remote share
		case strings.HasSuffix(urlPath, "/unmount") && strings.HasPrefix(urlPath, "/api/backup/devices/") && method == "POST":
			id := extractPathSegment(urlPath, "/api/backup/devices/", "/unmount")
			shareName := bodyStr(body, "shareName")
			if shareName == "" {
				jsonError(w, 400, "shareName required")
				return
			}
			result := unmountRemoteShare(id, shareName)
			if errMsg, ok := result["error"].(string); ok && errMsg != "" {
				jsonError(w, 500, errMsg)
				return
			}
			jsonOk(w, result)

		// POST/DELETE /api/backup/wg/* and /api/backup/pair/wg-* — WireGuard routes
		case strings.HasPrefix(urlPath, "/api/backup/wg/") || strings.HasPrefix(urlPath, "/api/backup/pair/wg-"):
			handleWGRoutes(w, r, urlPath, body)

		default:
			jsonError(w, 404, "Not found")
		}
		return
	}

	jsonError(w, 405, "Method not allowed")
}

// ─── URL Helpers ────────────────────────────────────────────────────────────

// extractPathSegment extracts the segment between prefix and suffix from a URL path.
// E.g., extractPathSegment("/api/backup/devices/dev_123/status", "/api/backup/devices/", "/status") → "dev_123"
func extractPathSegment(path, prefix, suffix string) string {
	s := strings.TrimPrefix(path, prefix)
	s = strings.TrimSuffix(s, suffix)
	s = strings.TrimSuffix(s, "/")
	return s
}

// ─── Pairing ────────────────────────────────────────────────────────────────

func pairWithRemote(addr, username, password, totpCode string) map[string]interface{} {
	// Determine protocol + port
	proto := "http"
	port := "5000"
	if !isLocalAddr(addr) {
		proto = "https"
		port = "5009"
	}

	baseURL := fmt.Sprintf("%s://%s:%s", proto, addr, port)
	client := &http.Client{Timeout: 10 * time.Second}

	// Step 1: Authenticate with remote
	loginBody := map[string]string{
		"username": username,
		"password": password,
	}
	if totpCode != "" {
		loginBody["totpCode"] = totpCode
	}

	loginJSON, _ := json.Marshal(loginBody)
	resp, err := client.Post(baseURL+"/api/auth/login", "application/json", strings.NewReader(string(loginJSON)))
	if err != nil {
		return map[string]interface{}{"error": "Cannot reach remote device: " + err.Error()}
	}
	defer resp.Body.Close()

	var loginResp map[string]interface{}
	json.NewDecoder(resp.Body).Decode(&loginResp)

	// Check if 2FA is required
	if requires2FA, _ := loginResp["requires2FA"].(bool); requires2FA {
		return map[string]interface{}{
			"requires2FA": true,
			"message":     "Enter TOTP code to continue",
		}
	}

	// Check for errors
	if errMsg, _ := loginResp["error"].(string); errMsg != "" {
		return map[string]interface{}{"error": "Authentication failed: " + errMsg}
	}

	token, _ := loginResp["token"].(string)
	if token == "" {
		return map[string]interface{}{"error": "No token received from remote"}
	}

	// Step 2: Get remote device info
	req, _ := http.NewRequest("GET", baseURL+"/api/auth/status", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	infoResp, err := client.Do(req)
	if err != nil {
		return map[string]interface{}{"error": "Cannot get remote info: " + err.Error()}
	}
	defer infoResp.Body.Close()

	var info map[string]interface{}
	json.NewDecoder(infoResp.Body).Decode(&info)

	remoteName := "NimOS"
	if h, ok := info["hostname"].(string); ok && h != "" {
		remoteName = h
	}
	remoteVersion := "unknown"
	if v, ok := info["version"].(string); ok && v != "" {
		remoteVersion = v
	}

	// Step 3: Register device locally
	id := backupID("dev")
	dev := map[string]interface{}{
		"id":   id,
		"name": remoteName,
		"addr": addr,
		"type": "nas",
	}
	if err := dbBackupDeviceCreate(dev); err != nil {
		return map[string]interface{}{"error": "Failed to save device: " + err.Error()}
	}

	// Step 3b: Register ourselves on the remote NAS (mutual pairing)
	localName := getLocalHostname()
	localAddr := getLocalLANAddr(addr)
	remoteDevPayload, _ := json.Marshal(map[string]interface{}{
		"name": localName,
		"addr": localAddr,
		"type": "nas",
	})
	regReq, _ := http.NewRequest("POST", baseURL+"/api/backup/devices", strings.NewReader(string(remoteDevPayload)))
	regReq.Header.Set("Authorization", "Bearer "+token)
	regReq.Header.Set("Content-Type", "application/json")
	regResp, regErr := client.Do(regReq)
	if regErr != nil {
		logMsg("backup: mutual pairing failed: %v (one-way pairing still valid)", regErr)
	} else {
		regResp.Body.Close()
		logMsg("backup: mutual pairing OK — registered '%s' on remote %s", localName, addr)
	}

	result := map[string]interface{}{
		"ok":      true,
		"id":      id,
		"name":    remoteName,
		"addr":    addr,
		"version": remoteVersion,
	}

	// Step 4: If WAN connection, set up WireGuard tunnel
	if !isLocalAddr(addr) {
		wgResult, err := initiateWGPairing(id, addr, token)
		if err != nil {
			logMsg("wireguard: pairing failed for %s: %v (device saved without WG)", addr, err)
			result["wireguard"] = map[string]interface{}{"error": err.Error()}
		} else {
			result["wireguard"] = wgResult

			// Update local device addr to the remote's tunnel IP
			// (so we use the tunnel to reach them from now on)
			if remoteIP, ok := wgResult["remoteIP"].(string); ok && remoteIP != "" {
				dbBackupDeviceUpdate(id, "addr", remoteIP)
				result["addr"] = remoteIP
				logMsg("wireguard: updated device %s addr to tunnel IP %s", id, remoteIP)
			}

			// Tell the remote to update their record of us to our tunnel IP
			if localIP, ok := wgResult["localIP"].(string); ok && localIP != "" {
				updatePayload, _ := json.Marshal(map[string]interface{}{
					"tunnelAddr": localIP,
				})
				updReq, _ := http.NewRequest("POST", baseURL+"/api/backup/pair/update-addr", strings.NewReader(string(updatePayload)))
				updReq.Header.Set("Authorization", "Bearer "+token)
				updReq.Header.Set("Content-Type", "application/json")
				if updResp, err := client.Do(updReq); err == nil {
					updResp.Body.Close()
					logMsg("wireguard: notified remote to use tunnel IP %s for us", localIP)
				}
			}
		}
	}

	return result
}

func isLocalAddr(addr string) bool {
	return strings.HasPrefix(addr, "192.168.") ||
		strings.HasPrefix(addr, "10.") ||
		strings.HasPrefix(addr, "172.") ||
		addr == "localhost" ||
		addr == "127.0.0.1"
}

// getLocalHostname returns this machine's hostname.
func getLocalHostname() string {
	if out, ok := run("hostname"); ok && out != "" {
		return strings.TrimSpace(out)
	}
	return "NimOS"
}

// getLocalLANAddr returns our IP address that's on the same subnet as the remote addr.
// E.g., if remote is 192.168.1.131, returns our 192.168.1.x address.
func getLocalLANAddr(remoteAddr string) string {
	// Extract remote subnet prefix (first 3 octets)
	parts := strings.Split(remoteAddr, ".")
	if len(parts) < 3 {
		return detectOwnIP()
	}
	remotePrefix := parts[0] + "." + parts[1] + "." + parts[2] + "."

	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return detectOwnIP()
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && ipnet.IP.To4() != nil {
			ip := ipnet.IP.String()
			if strings.HasPrefix(ip, remotePrefix) {
				return ip
			}
		}
	}
	return detectOwnIP()
}

// detectOwnIP returns the first non-loopback IPv4 address.
func detectOwnIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "127.0.0.1"
	}
	for _, a := range addrs {
		if ipnet, ok := a.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return "127.0.0.1"
}

// ─── Backup Snapshots ───────────────────────────────────────────────────────

func listBackupSnapshots(pool string) map[string]interface{} {
	var allSnaps []map[string]interface{}

	// ZFS snapshots
	zfsCmd := "zfs list -t snapshot -H -o name,used,creation 2>/dev/null | grep nimbackup"
	if pool != "" {
		zfsCmd = fmt.Sprintf("zfs list -t snapshot -H -o name,used,creation -r nimos-%s 2>/dev/null | grep nimbackup", pool)
	}
	if out, ok := run(zfsCmd); ok && out != "" {
		for _, line := range strings.Split(out, "\n") {
			fields := strings.Fields(line)
			if len(fields) >= 2 {
				allSnaps = append(allSnaps, map[string]interface{}{
					"name":   fields[0],
					"used":   fields[1],
					"type":   "zfs",
					"time":   extractTimestamp(fields[0]).Format(time.RFC3339),
				})
			}
		}
	}

	// Btrfs snapshots (search in known pool mounts)
	btrfsCmd := "find /nimbus/pools -path '*/.snapshots/nimbackup-*' -maxdepth 4 -type d 2>/dev/null"
	if out, ok := run(btrfsCmd); ok && out != "" {
		for _, path := range strings.Split(strings.TrimSpace(out), "\n") {
			if path == "" {
				continue
			}
			parts := strings.Split(path, "/")
			name := parts[len(parts)-1]
			allSnaps = append(allSnaps, map[string]interface{}{
				"name": name,
				"path": path,
				"type": "btrfs",
				"time": extractTimestamp(name).Format(time.RFC3339),
			})
		}
	}

	if allSnaps == nil {
		allSnaps = []map[string]interface{}{}
	}

	return map[string]interface{}{"snapshots": allSnaps}
}

func createBackupSnapshot(source, fsType string) map[string]interface{} {
	timestamp := time.Now().UTC().Format("20060102-150405")
	snapName := fmt.Sprintf("nimbackup-%s", timestamp)

	switch fsType {
	case "zfs":
		fullSnap := fmt.Sprintf("%s@%s", source, snapName)
		if out, ok := run(fmt.Sprintf("zfs snapshot %s", fullSnap)); !ok {
			return map[string]interface{}{"error": "Failed: " + out}
		}
		return map[string]interface{}{"ok": true, "name": fullSnap, "type": "zfs"}

	case "btrfs":
		snapPath := fmt.Sprintf("%s/.snapshots/%s", source, snapName)
		run(fmt.Sprintf("mkdir -p %s/.snapshots", source))
		if out, ok := run(fmt.Sprintf("btrfs subvolume snapshot -r %s %s", source, snapPath)); !ok {
			return map[string]interface{}{"error": "Failed: " + out}
		}
		return map[string]interface{}{"ok": true, "name": snapName, "path": snapPath, "type": "btrfs"}
	}

	return map[string]interface{}{"error": "Unsupported fsType: " + fsType}
}

func deleteBackupSnapshot(name, fsType, source string) map[string]interface{} {
	switch fsType {
	case "zfs":
		// name should be like "pool/dataset@nimbackup-XXXXXXXX-XXXXXX"
		if !strings.Contains(name, "@") {
			return map[string]interface{}{"error": "Invalid ZFS snapshot name"}
		}
		if out, ok := run(fmt.Sprintf("zfs destroy %s", name)); !ok {
			return map[string]interface{}{"error": "Failed: " + out}
		}
		return map[string]interface{}{"ok": true}

	case "btrfs":
		snapPath := fmt.Sprintf("%s/.snapshots/%s", source, name)
		if out, ok := run(fmt.Sprintf("btrfs subvolume delete %s", snapPath)); !ok {
			return map[string]interface{}{"error": "Failed: " + out}
		}
		return map[string]interface{}{"ok": true}
	}

	return map[string]interface{}{"error": "Unsupported fsType"}
}

// ─── Remote Shares (NFS mount) ──────────────────────────────────────────────

const remoteMountBase = "/nimbus/remote"

// fetchRemoteShares queries the shares list from a remote NimOS device.
// Uses the pairing credentials to authenticate.
func fetchRemoteShares(device map[string]interface{}) ([]map[string]interface{}, error) {
	addr, _ := device["addr"].(string)
	if addr == "" {
		return nil, fmt.Errorf("device has no address")
	}

	proto := "http"
	port := "5000"
	if !isLocalAddr(addr) {
		proto = "https"
		port = "5009"
	}

	client := &http.Client{Timeout: 5 * time.Second}

	// Query the remote's public-shares endpoint (verified by paired device IP)
	resp, err := client.Get(fmt.Sprintf("%s://%s:%s/api/backup/public-shares", proto, addr, port))
	if err != nil {
		return nil, fmt.Errorf("cannot reach remote: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("remote returned status %d", resp.StatusCode)
	}

	var data interface{}
	if err := json.NewDecoder(resp.Body).Decode(&data); err != nil {
		return nil, fmt.Errorf("invalid response: %v", err)
	}

	// Response can be an array or { "shares": [...] }
	var shares []map[string]interface{}
	switch v := data.(type) {
	case []interface{}:
		for _, item := range v {
			if m, ok := item.(map[string]interface{}); ok {
				shares = append(shares, map[string]interface{}{
					"name":        m["name"],
					"displayName": m["displayName"],
					"description": m["description"],
					"path":        m["path"],
					"pool":        m["pool"],
					"used":        m["used"],
					"total":       m["total"],
				})
			}
		}
	case map[string]interface{}:
		if arr, ok := v["shares"].([]interface{}); ok {
			for _, item := range arr {
				if m, ok := item.(map[string]interface{}); ok {
					shares = append(shares, map[string]interface{}{
						"name":        m["name"],
						"displayName": m["displayName"],
						"description": m["description"],
						"path":        m["path"],
						"pool":        m["pool"],
						"used":        m["used"],
						"total":       m["total"],
					})
				}
			}
		}
	}

	if shares == nil {
		shares = []map[string]interface{}{}
	}
	return shares, nil
}

// getPublicShares returns this NAS's shares in a simplified format for paired devices.
// No full auth required — but we verify the request comes from a paired device IP.
func getPublicShares(r *http.Request) map[string]interface{} {
	// Verify requester is a paired device
	remoteIP := r.RemoteAddr
	if idx := strings.LastIndex(remoteIP, ":"); idx > 0 {
		remoteIP = remoteIP[:idx]
	}
	remoteIP = strings.Trim(remoteIP, "[]") // IPv6 brackets

	devices, _ := dbBackupDeviceList()
	isPaired := false
	for _, d := range devices {
		if addr, _ := d["addr"].(string); addr == remoteIP {
			isPaired = true
			break
		}
	}

	if !isPaired {
		return map[string]interface{}{"error": "not a paired device"}
	}

	shares, err := dbSharesList()
	if err != nil {
		return map[string]interface{}{"error": err.Error()}
	}

	// Enrich shares with real quota data (ZFS/Btrfs)
	enrichSharesWithQuota(shares)

	// Return simplified share info with disk usage
	var result []map[string]interface{}
	for _, s := range shares {
		path, _ := s["path"].(string)
		entry := map[string]interface{}{
			"name":        s["name"],
			"displayName": s["displayName"],
			"description": s["description"],
			"path":        path,
			"pool":        s["pool"],
		}
		// Quota = total capacity for this share, used = actual usage
		if q, ok := s["quota"].(int64); ok && q > 0 {
			entry["total"] = q
		} else if a, ok := s["available"].(int64); ok {
			// No quota set — use used + available as total
			if u, ok2 := s["used"].(int64); ok2 {
				entry["total"] = u + a
			}
		}
		if u, ok := s["used"].(int64); ok {
			entry["used"] = u
		}
		result = append(result, entry)
	}
	if result == nil {
		result = []map[string]interface{}{}
	}

	return map[string]interface{}{"shares": result}
}

// mountRemoteShare mounts a remote NFS share locally.
// First requests the remote NAS to export the path via NFS for our IP,
// then mounts it locally.
func mountRemoteShare(deviceID, deviceName, deviceAddr, shareName, remotePath string) map[string]interface{} {
	// Sanitize names for filesystem
	safeDev := regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(deviceName, "_")
	safeShare := regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(shareName, "_")

	mountPoint := fmt.Sprintf("%s/%s/%s", remoteMountBase, safeDev, safeShare)

	// Create mount point
	run(fmt.Sprintf("mkdir -p %s", mountPoint))

	// Check if already mounted
	if out, ok := run(fmt.Sprintf("mountpoint -q %s 2>/dev/null && echo yes", mountPoint)); ok && strings.Contains(out, "yes") {
		return map[string]interface{}{"ok": true, "mountPoint": mountPoint, "message": "Already mounted"}
	}

	// Step 1: Ask the remote NAS to export this path for our IP
	ourIP := getLocalLANAddr(deviceAddr)
	proto := "http"
	port := "5000"
	if !isLocalAddr(deviceAddr) {
		proto = "https"
		port = "5009"
	}

	client := &http.Client{Timeout: 10 * time.Second}
	exportPayload, _ := json.Marshal(map[string]string{
		"path":     remotePath,
		"clientIP": ourIP,
	})
	exportResp, exportErr := client.Post(
		fmt.Sprintf("%s://%s:%s/api/backup/nfs-export", proto, deviceAddr, port),
		"application/json",
		strings.NewReader(string(exportPayload)),
	)
	if exportErr != nil {
		logMsg("remote-share: failed to request NFS export from %s: %v", deviceAddr, exportErr)
	} else {
		exportResp.Body.Close()
	}

	// Brief wait for NFS export to take effect
	time.Sleep(500 * time.Millisecond)

	// Step 2: Ensure NFS client is available locally
	run("which mount.nfs >/dev/null 2>&1 || apt-get install -y -qq nfs-common 2>/dev/null")

	// Step 3: Mount via NFS
	nfsCmd := fmt.Sprintf("mount -t nfs -o soft,timeo=50,retrans=3,nolock %s:%s %s",
		deviceAddr, remotePath, mountPoint)

	out, ok := run(nfsCmd)
	if !ok {
		// Fallback: try CIFS/SMB mount
		cifsCmd := fmt.Sprintf("mount -t cifs //%s/%s %s -o guest,vers=3.0,soft",
			deviceAddr, shareName, mountPoint)
		out2, ok2 := run(cifsCmd)
		if !ok2 {
			return map[string]interface{}{
				"error": fmt.Sprintf("NFS failed: %s | SMB failed: %s", out, out2),
			}
		}
	}

	logMsg("remote-share: mounted %s:%s → %s", deviceAddr, remotePath, mountPoint)

	// Save mount info for persistence
	saveMountRecord(deviceID, shareName, remotePath, mountPoint, deviceAddr)

	return map[string]interface{}{
		"ok":         true,
		"mountPoint": mountPoint,
	}
}

// unmountRemoteShare unmounts a remote share.
func unmountRemoteShare(deviceID, shareName string) map[string]interface{} {
	record := getMountRecord(deviceID, shareName)
	if record == nil {
		return map[string]interface{}{"error": "mount not found"}
	}

	mountPoint, _ := record["mountPoint"].(string)
	if mountPoint == "" {
		return map[string]interface{}{"error": "no mount point"}
	}

	out, ok := run(fmt.Sprintf("umount %s 2>&1", mountPoint))
	if !ok {
		// Force unmount
		run(fmt.Sprintf("umount -f %s 2>&1", mountPoint))
	}
	_ = out

	// Clean up empty directory
	run(fmt.Sprintf("rmdir %s 2>/dev/null", mountPoint))

	removeMountRecord(deviceID, shareName)

	logMsg("remote-share: unmounted %s/%s", deviceID, shareName)
	return map[string]interface{}{"ok": true}
}

// listMountedRemoteShares returns all currently mounted remote shares for a device.
func listMountedRemoteShares(deviceID string) []map[string]interface{} {
	deviceStatusCacheMu.RLock()
	defer deviceStatusCacheMu.RUnlock()
	// Read from mount records in DB
	rows, err := db.Query(`SELECT share_name, remote_path, mount_point, device_addr
		FROM remote_mounts WHERE device_id = ?`, deviceID)
	if err != nil {
		return []map[string]interface{}{}
	}
	defer rows.Close()

	var mounts []map[string]interface{}
	for rows.Next() {
		var shareName, remotePath, mountPoint, addr string
		if rows.Scan(&shareName, &remotePath, &mountPoint, &addr) != nil {
			continue
		}
		// Check if still mounted
		mounted := false
		if _, ok := run(fmt.Sprintf("mountpoint -q %s 2>/dev/null && echo yes", mountPoint)); ok {
			mounted = true
		}
		mounts = append(mounts, map[string]interface{}{
			"shareName":  shareName,
			"remotePath": remotePath,
			"mountPoint": mountPoint,
			"mounted":    mounted,
		})
	}
	if mounts == nil {
		mounts = []map[string]interface{}{}
	}
	return mounts
}

// ─── Mount Records (SQLite) ─────────────────────────────────────────────────

func createRemoteMountsTable() error {
	_, err := db.Exec(`
	CREATE TABLE IF NOT EXISTS remote_mounts (
		device_id    TEXT NOT NULL,
		share_name   TEXT NOT NULL,
		remote_path  TEXT NOT NULL,
		mount_point  TEXT NOT NULL,
		device_addr  TEXT NOT NULL,
		created_at   TEXT NOT NULL,
		PRIMARY KEY (device_id, share_name)
	);`)
	return err
}

func saveMountRecord(deviceID, shareName, remotePath, mountPoint, deviceAddr string) {
	db.Exec(`INSERT OR REPLACE INTO remote_mounts (device_id, share_name, remote_path, mount_point, device_addr, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`,
		deviceID, shareName, remotePath, mountPoint, deviceAddr,
		time.Now().UTC().Format(time.RFC3339))
}

func getMountRecord(deviceID, shareName string) map[string]interface{} {
	var remotePath, mountPoint, deviceAddr string
	err := db.QueryRow(`SELECT remote_path, mount_point, device_addr FROM remote_mounts
		WHERE device_id = ? AND share_name = ?`, deviceID, shareName).Scan(&remotePath, &mountPoint, &deviceAddr)
	if err != nil {
		return nil
	}
	return map[string]interface{}{
		"remotePath": remotePath,
		"mountPoint": mountPoint,
		"deviceAddr": deviceAddr,
	}
}

func removeMountRecord(deviceID, shareName string) {
	db.Exec(`DELETE FROM remote_mounts WHERE device_id = ? AND share_name = ?`, deviceID, shareName)
}

// remountAllOnStartup re-mounts all saved remote shares on daemon start.
func remountAllOnStartup() {
	rows, err := db.Query(`SELECT device_id, share_name, remote_path, mount_point, device_addr FROM remote_mounts`)
	if err != nil {
		return
	}
	defer rows.Close()

	for rows.Next() {
		var deviceID, shareName, remotePath, mountPoint, addr string
		if rows.Scan(&deviceID, &shareName, &remotePath, &mountPoint, &addr) != nil {
			continue
		}
		// Only remount if not already mounted
		if out, _ := run(fmt.Sprintf("mountpoint -q %s 2>/dev/null && echo yes", mountPoint)); strings.Contains(out, "yes") {
			continue
		}
		run(fmt.Sprintf("mkdir -p %s", mountPoint))
		if _, ok := run(fmt.Sprintf("mount -t nfs -o soft,timeo=50,retrans=3,nolock %s:%s %s", addr, remotePath, mountPoint)); ok {
			logMsg("remote-share: remounted %s:%s → %s", addr, remotePath, mountPoint)
		}
	}
}

// ─── NFS Export Management ──────────────────────────────────────────────────
// These functions manage /etc/exports so that paired devices can mount our shares.

const exportsFile = "/etc/exports"
const nimosExportMarker = "# NimOS-managed"

// addNFSExport adds a path to /etc/exports for a specific client IP.
// Only adds if not already exported. Runs exportfs -ra to apply.
func addNFSExport(path, clientIP string) error {
	// Ensure NFS server is installed
	run("which exportfs >/dev/null 2>&1 || apt-get install -y -qq nfs-kernel-server 2>/dev/null")

	// Read current exports
	data, err := os.ReadFile(exportsFile)
	if err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("read exports: %v", err)
	}
	content := string(data)

	// Check if this exact export already exists
	exportLine := fmt.Sprintf("%s %s(rw,sync,no_subtree_check,no_root_squash) %s", path, clientIP, nimosExportMarker)
	if strings.Contains(content, fmt.Sprintf("%s %s(", path, clientIP)) {
		// Already exported
		run("exportfs -ra")
		return nil
	}

	// Append the export
	if !strings.HasSuffix(content, "\n") && content != "" {
		content += "\n"
	}
	content += exportLine + "\n"

	if err := os.WriteFile(exportsFile, []byte(content), 0644); err != nil {
		return fmt.Errorf("write exports: %v", err)
	}

	// Apply exports
	out, ok := run("exportfs -ra")
	if !ok {
		logMsg("nfs: exportfs -ra failed: %s", out)
		// Try starting the NFS server
		run("systemctl start nfs-kernel-server 2>/dev/null || service nfs-kernel-server start 2>/dev/null")
		run("exportfs -ra")
	}

	logMsg("nfs: exported %s for %s", path, clientIP)
	return nil
}

// removeNFSExport removes a path from /etc/exports for a specific client IP.
func removeNFSExport(path, clientIP string) {
	data, err := os.ReadFile(exportsFile)
	if err != nil {
		return
	}

	lines := strings.Split(string(data), "\n")
	var kept []string
	for _, line := range lines {
		// Remove lines matching this path + clientIP
		if strings.HasPrefix(strings.TrimSpace(line), path+" ") && strings.Contains(line, clientIP) {
			continue
		}
		kept = append(kept, line)
	}

	os.WriteFile(exportsFile, []byte(strings.Join(kept, "\n")), 0644)
	run("exportfs -ra")
	logMsg("nfs: unexported %s for %s", path, clientIP)
}

// ensureNFSServer makes sure NFS server is running.
func ensureNFSServer() {
	// Check if running
	if out, _ := run("systemctl is-active nfs-kernel-server 2>/dev/null"); strings.TrimSpace(out) == "active" {
		return
	}
	// Start it
	run("systemctl enable nfs-kernel-server 2>/dev/null")
	run("systemctl start nfs-kernel-server 2>/dev/null")
	logMsg("nfs: started nfs-kernel-server")
}

// handleNFSExport handles the /api/backup/nfs-export endpoint.
// Called by a paired device requesting us to export a path for their IP.
// Verifies the requester is a paired device by checking their IP.
func handleNFSExport(w http.ResponseWriter, r *http.Request) {
	// Verify requester is a paired device
	remoteIP := r.RemoteAddr
	if idx := strings.LastIndex(remoteIP, ":"); idx > 0 {
		remoteIP = remoteIP[:idx]
	}
	remoteIP = strings.Trim(remoteIP, "[]")

	devices, _ := dbBackupDeviceList()
	isPaired := false
	for _, d := range devices {
		if addr, _ := d["addr"].(string); addr == remoteIP {
			isPaired = true
			break
		}
	}

	if !isPaired {
		jsonError(w, 403, "not a paired device")
		return
	}

	body, err := readBody(r)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}

	path := bodyStr(body, "path")
	clientIP := bodyStr(body, "clientIP")
	if path == "" || clientIP == "" {
		jsonError(w, 400, "path and clientIP required")
		return
	}

	// Security: verify the path is actually a share we own
	shares, _ := dbSharesList()
	validPath := false
	for _, s := range shares {
		if sp, _ := s["path"].(string); sp == path {
			validPath = true
			break
		}
	}
	if !validPath {
		jsonError(w, 403, "path is not a shared folder")
		return
	}

	ensureNFSServer()

	if err := addNFSExport(path, clientIP); err != nil {
		jsonError(w, 500, err.Error())
		return
	}

	jsonOk(w, map[string]interface{}{"ok": true, "exported": path, "client": clientIP})
}

// ─── Disk Usage Helpers ─────────────────────────────────────────────────────

// getPathDiskUsage returns used and total bytes for the filesystem containing the given path.
// For ZFS datasets it uses zfs get, for others it uses df.
func getPathDiskUsage(path string) (int64, int64) {
	// Try ZFS first — check if path is on a ZFS dataset
	if out, ok := run(fmt.Sprintf("df -B1 --output=used,size %s 2>/dev/null | tail -1", path)); ok && out != "" {
		fields := strings.Fields(out)
		if len(fields) >= 2 {
			used := parseByteSize(fields[0])
			total := parseByteSize(fields[1])
			if total > 0 {
				return used, total
			}
		}
	}
	// Fallback: try du for used + df for total
	var used, total int64
	if out, ok := run(fmt.Sprintf("du -sb %s 2>/dev/null | awk '{print $1}'", path)); ok {
		used = parseByteSize(strings.TrimSpace(out))
	}
	if out, ok := run(fmt.Sprintf("df -B1 --output=size %s 2>/dev/null | tail -1", path)); ok {
		total = parseByteSize(strings.TrimSpace(out))
	}
	return used, total
}
