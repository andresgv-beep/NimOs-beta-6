package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// ═══════════════════════════════════
// Shares HTTP handlers
// ═══════════════════════════════════

func handleSharesRoutes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	method := r.Method

	// GET /api/shares — list all shared folders
	if path == "/api/shares" && method == "GET" {
		sharesListHTTP(w, r)
		return
	}

	// POST /api/shares — create shared folder
	if path == "/api/shares" && method == "POST" {
		sharesCreateHTTP(w, r)
		return
	}

	// Match /api/shares/:name
	shareMatch := regexp.MustCompile(`^/api/shares/([a-zA-Z0-9_-]+)$`)
	matches := shareMatch.FindStringSubmatch(path)
	if matches == nil {
		jsonError(w, 404, "Not found")
		return
	}
	target := matches[1]

	switch method {
	case "PUT":
		sharesUpdateHTTP(w, r, target)
	case "DELETE":
		sharesDeleteHTTP(w, r, target)
	default:
		jsonError(w, 405, "Method not allowed")
	}
}

// GET /api/shares
func sharesListHTTP(w http.ResponseWriter, r *http.Request) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}

	shares, err := dbSharesList()
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}

	// Enrich shares with real quota from filesystem
	enrichSharesWithQuota(shares)

	role, _ := session["role"].(string)
	username, _ := session["username"].(string)

	if role != "admin" {
		// Filter: only shares where this user has permission
		var filtered []map[string]interface{}
		for _, s := range shares {
			perms, _ := s["permissions"].(map[string]string)
			if perm, ok := perms[username]; ok && (perm == "rw" || perm == "ro") {
				s["myPermission"] = perm
				filtered = append(filtered, s)
			}
		}
		if filtered == nil {
			filtered = []map[string]interface{}{}
		}
		jsonOk(w, filtered)
		return
	}

	jsonOk(w, shares)
}

// POST /api/shares
func sharesCreateHTTP(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}

	body, err := readBody(r)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}

	name := strings.TrimSpace(bodyStr(body, "name"))
	description := bodyStr(body, "description")
	poolName := bodyStr(body, "pool")
	quotaBytes := int64(0)
	if qb, ok := body["quotaBytes"].(float64); ok {
		quotaBytes = int64(qb)
	}

	if name == "" {
		jsonError(w, 400, "Folder name required")
		return
	}
	if len(name) > 64 {
		jsonError(w, 400, "Folder name too long (max 64 characters)")
		return
	}
	if matched, _ := regexp.MatchString(`[^a-zA-Z0-9_\- ]`, name); matched {
		jsonError(w, 400, "Name can only contain letters, numbers, spaces, -, _")
		return
	}

	safeName := strings.ToLower(strings.ReplaceAll(strings.TrimSpace(name), " ", "-"))

	// Check if share already exists
	if existing, _ := dbSharesGet(safeName); existing != nil {
		jsonError(w, 400, "Shared folder already exists")
		return
	}

	// Determine target pool from storage config
	targetPool := findTargetPool(poolName)
	if targetPool == nil {
		jsonError(w, 400, "No storage pool available. Create a pool in Storage Manager first.")
		return
	}

	mountPoint, _ := targetPool["mountPoint"].(string)
	poolType, _ := targetPool["type"].(string)
	zpoolName, _ := targetPool["zpoolName"].(string)

	// Verify the pool is actually mounted — not just a leftover directory on the system disk
	if !isPathOnMountedPool(mountPoint) {
		jsonError(w, 503, "Storage pool is not mounted. Check Storage Manager for pool status.")
		return
	}

	folderPath := filepath.Join(mountPoint, "shares", safeName)
	volumeName, _ := targetPool["name"].(string)

	// ── ZFS: create dataset instead of mkdir ──
	// Each shared folder becomes its own ZFS dataset under pool/shares/
	// This gives each folder its own quota, snapshots, and compression settings.
	if poolType == "zfs" && zpoolName != "" {
		datasetName := zpoolName + "/shares/" + safeName
		opts := CmdOptions{Timeout: 15 * time.Second}

		// Check if dataset already exists
		existing, _ := runCmd("zfs", []string{"list", "-H", "-o", "name", datasetName}, opts)
		if strings.TrimSpace(existing.Stdout) == "" {
			// Create dataset — it auto-mounts at folderPath
			_, err := runCmd("zfs", []string{"create", datasetName}, opts)
			if err != nil {
				jsonError(w, 500, fmt.Sprintf("Failed to create ZFS dataset: %s", err))
				return
			}
			logMsg("Created ZFS dataset '%s' for share '%s'", datasetName, safeName)

			// Set quota if specified
			if quotaBytes > 0 {
				runCmd("zfs", []string{"set", fmt.Sprintf("quota=%d", quotaBytes), datasetName}, opts)
				logMsg("Set quota %d bytes on dataset '%s'", quotaBytes, datasetName)
			}
		}
	}

	// ── BTRFS: create subvolume with qgroup quota ──
	// Each shared folder becomes a BTRFS subvolume under pool/shares/
	// Quotas use qgroups (must be enabled on the pool first).
	if poolType == "btrfs" {
		subvolPath := filepath.Join(mountPoint, "shares", safeName)
		opts := CmdOptions{Timeout: 15 * time.Second}

		// Check if subvolume already exists
		existing, _ := runCmd("btrfs", []string{"subvolume", "show", subvolPath}, opts)
		if existing.Stdout == "" || existing.Code != 0 {
			// Create subvolume
			_, err := runCmd("btrfs", []string{"subvolume", "create", subvolPath}, opts)
			if err != nil {
				jsonError(w, 500, fmt.Sprintf("Failed to create BTRFS subvolume: %s", err))
				return
			}
			logMsg("Created BTRFS subvolume '%s' for share '%s'", subvolPath, safeName)

			// Set quota if specified
			if quotaBytes > 0 {
				quotaStr := fmt.Sprintf("%d", quotaBytes)
				runCmd("btrfs", []string{"qgroup", "limit", quotaStr, subvolPath}, opts)
				logMsg("Set BTRFS quota %d bytes on subvolume '%s'", quotaBytes, subvolPath)
			}
		}
	}

	// Call daemon ops to create share with proper filesystem permissions
	daemonResult := handleOp(Request{
		Op:        "share.create",
		ShareName: safeName,
		PoolPath:  mountPoint,
	})

	if !daemonResult.Ok {
		jsonError(w, 500, fmt.Sprintf("Failed to create share: %s", daemonResult.Error))
		return
	}

	// Register in DB
	username := session["username"].(string)
	if err := dbSharesCreate(safeName, name, description, folderPath, volumeName, volumeName, username); err != nil {
		jsonError(w, 500, err.Error())
		return
	}

	// Set admin as rw
	dbShareSetPermission(safeName, username, "rw")

	jsonOk(w, map[string]interface{}{
		"ok":   true,
		"name": safeName,
		"path": folderPath,
		"pool": volumeName,
	})
}

// PUT /api/shares/:name
func sharesUpdateHTTP(w http.ResponseWriter, r *http.Request, target string) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}

	share, err := dbSharesGet(target)
	if err != nil || share == nil {
		jsonError(w, 404, "Shared folder not found")
		return
	}

	body, _ := readBody(r)

	// Update simple fields
	updates := map[string]interface{}{}
	if desc, ok := body["description"]; ok {
		updates["description"] = desc
	}
	if rb, ok := body["recycleBin"]; ok {
		updates["recycleBin"] = rb
	}
	if len(updates) > 0 {
		dbSharesUpdate(target, updates)
	}

	// Handle quota change (ZFS and BTRFS)
	if quotaRaw, ok := body["quota"]; ok {
		quotaBytes := int64(0)
		if qb, ok := quotaRaw.(float64); ok {
			quotaBytes = int64(qb)
		}

		sharPool, _ := share["pool"].(string)
		if sharPool == "" {
			sharPool, _ = share["volume"].(string)
		}
		targetPool := findTargetPool(sharPool)
		if targetPool != nil {
			poolType, _ := targetPool["type"].(string)
			zpoolName, _ := targetPool["zpoolName"].(string)
			mountPoint, _ := targetPool["mountPoint"].(string)
			opts := CmdOptions{Timeout: 10 * time.Second}

			if poolType == "zfs" && zpoolName != "" {
				datasetName := zpoolName + "/shares/" + target
				if quotaBytes > 0 {
					runCmd("zfs", []string{"set", fmt.Sprintf("quota=%d", quotaBytes), datasetName}, opts)
					logMsg("Updated quota to %d bytes on dataset '%s'", quotaBytes, datasetName)
				} else {
					runCmd("zfs", []string{"set", "quota=none", datasetName}, opts)
					logMsg("Removed quota on dataset '%s'", datasetName)
				}
			} else if poolType == "btrfs" && mountPoint != "" {
				subvolPath := filepath.Join(mountPoint, "shares", target)
				if quotaBytes > 0 {
					runCmd("btrfs", []string{"qgroup", "limit", fmt.Sprintf("%d", quotaBytes), subvolPath}, opts)
					logMsg("Updated BTRFS quota to %d bytes on '%s'", quotaBytes, subvolPath)
				} else {
					runCmd("btrfs", []string{"qgroup", "limit", "none", subvolPath}, opts)
					logMsg("Removed BTRFS quota on '%s'", subvolPath)
				}
			}
		}
	}

	// Handle permission changes
	if permsRaw, ok := body["permissions"]; ok {
		if newPermsMap, ok := permsRaw.(map[string]interface{}); ok {
			// Get current permissions
			oldPerms, _ := share["permissions"].(map[string]string)
			if oldPerms == nil {
				oldPerms = map[string]string{}
			}

			// Collect all users
			allUsers := map[string]bool{}
			for u := range oldPerms {
				allUsers[u] = true
			}
			for u := range newPermsMap {
				allUsers[u] = true
			}

			for username := range allUsers {
				oldPerm := oldPerms[username]
				newPerm := ""
				if v, ok := newPermsMap[username]; ok {
					newPerm, _ = v.(string)
				}
				if newPerm == "" {
					newPerm = "none"
				}
				if oldPerm == newPerm {
					continue
				}

				switch newPerm {
				case "none":
					handleOp(Request{Op: "share.remove_user", ShareName: target, Username: username})
				case "rw":
					handleOp(Request{Op: "share.add_user_rw", ShareName: target, Username: username})
				case "ro":
					handleOp(Request{Op: "share.add_user_ro", ShareName: target, Username: username})
				}

				// Update DB
				dbShareSetPermission(target, username, newPerm)
			}
		}
	}

	// Handle app permission changes
	if appsRaw, ok := body["appPermissions"]; ok {
		if newApps, ok := appsRaw.([]interface{}); ok {
			// Get current app permissions
			oldApps, _ := share["appPermissions"].([]map[string]interface{})

			// Remove old apps not in new list
			for _, oldApp := range oldApps {
				uid, _ := oldApp["uid"]
				appId, _ := oldApp["appId"].(string)
				found := false
				for _, na := range newApps {
					if naMap, ok := na.(map[string]interface{}); ok {
						if naMap["uid"] == uid {
							found = true
							break
						}
					}
				}
				if !found {
					if uidNum, err := checkUid(uid); err == nil {
						handleOp(Request{Op: "share.remove_app", ShareName: target, AppId: appId, Uid: uidNum})
					}
				}
			}

			// Add/update new apps
			for _, na := range newApps {
				if naMap, ok := na.(map[string]interface{}); ok {
					perm, _ := naMap["permission"].(string)
					appId, _ := naMap["appId"].(string)
					if uid, err := checkUid(naMap["uid"]); err == nil && perm != "" {
						handleOp(Request{Op: "share.add_app", ShareName: target, AppId: appId, Uid: uid, Permission: perm})
					}
				}
			}
		}
	}

	jsonOk(w, map[string]interface{}{"ok": true})
}

// DELETE /api/shares/:name
func sharesDeleteHTTP(w http.ResponseWriter, r *http.Request, target string) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}

	share, _ := dbSharesGet(target)
	if share == nil {
		jsonError(w, 404, "Shared folder not found")
		return
	}

	// Remove group (files preserved on non-ZFS)
	handleOp(Request{Op: "share.delete", ShareName: target})

	// If this share lives on a ZFS pool, destroy the dataset
	sharPool, _ := share["pool"].(string)
	if sharPool == "" {
		sharPool, _ = share["volume"].(string)
	}
	targetPool := findTargetPool(sharPool)
	if targetPool != nil {
		poolType, _ := targetPool["type"].(string)
		zpoolName, _ := targetPool["zpoolName"].(string)
		if poolType == "zfs" && zpoolName != "" {
			datasetName := zpoolName + "/shares/" + target
			opts := CmdOptions{Timeout: 15 * time.Second}
			// Check if dataset exists before destroying
			existing, _ := runCmd("zfs", []string{"list", "-H", "-o", "name", datasetName}, opts)
			if strings.TrimSpace(existing.Stdout) != "" {
				_, err := runCmd("zfs", []string{"destroy", "-r", datasetName}, opts)
				if err != nil {
					logMsg("WARNING: failed to destroy ZFS dataset '%s': %s", datasetName, err)
				} else {
					logMsg("Destroyed ZFS dataset '%s' for share '%s'", datasetName, target)
				}
			}
		}
	}

	// Remove from DB
	dbSharesDelete(target)

	jsonOk(w, map[string]interface{}{"ok": true})
}

// enrichSharesWithQuota reads the real quota from ZFS datasets or BTRFS subvolumes
// and adds it to each share object so the UI can display it.
func enrichSharesWithQuota(shares []map[string]interface{}) {
	opts := CmdOptions{Timeout: 5 * time.Second}
	for i, s := range shares {
		sharPool, _ := s["pool"].(string)
		if sharPool == "" {
			sharPool, _ = s["volume"].(string)
		}
		sharName, _ := s["name"].(string)
		if sharPool == "" || sharName == "" {
			continue
		}

		targetPool := findTargetPool(sharPool)
		if targetPool == nil {
			continue
		}

		poolType, _ := targetPool["type"].(string)
		zpoolName, _ := targetPool["zpoolName"].(string)
		mountPoint, _ := targetPool["mountPoint"].(string)

		shares[i]["poolType"] = poolType
		sharePath := filepath.Join(mountPoint, "shares", sharName)
		shares[i]["mountPoint"] = sharePath

		if poolType == "zfs" && zpoolName != "" {
			datasetName := zpoolName + "/shares/" + sharName
			// Get quota
			res, err := runCmd("zfs", []string{"get", "-Hp", "-o", "value", "quota", datasetName}, opts)
			if err == nil {
				val := strings.TrimSpace(res.Stdout)
				if val != "" && val != "0" && val != "none" {
					var q int64
					fmt.Sscanf(val, "%d", &q)
					shares[i]["quota"] = q
				} else {
					shares[i]["quota"] = int64(0)
				}
			}
			// Get used
			res, err = runCmd("zfs", []string{"get", "-Hp", "-o", "value", "used", datasetName}, opts)
			if err == nil {
				var u int64
				fmt.Sscanf(strings.TrimSpace(res.Stdout), "%d", &u)
				shares[i]["used"] = u
			}
			// Get available
			res, err = runCmd("zfs", []string{"get", "-Hp", "-o", "value", "available", datasetName}, opts)
			if err == nil {
				var a int64
				fmt.Sscanf(strings.TrimSpace(res.Stdout), "%d", &a)
				shares[i]["available"] = a
			}

		} else if poolType == "btrfs" && mountPoint != "" {
			subvolPath := filepath.Join(mountPoint, "shares", sharName)
			// Get quota from subvolume show
			res, err := runCmd("btrfs", []string{"subvolume", "show", subvolPath}, opts)
			if err == nil {
				for _, line := range strings.Split(res.Stdout, "\n") {
					line = strings.TrimSpace(line)
					if strings.HasPrefix(line, "Limit referenced:") {
						valStr := strings.TrimPrefix(line, "Limit referenced:")
						valStr = strings.TrimSpace(valStr)
						if valStr != "-" && valStr != "none" {
							shares[i]["quota"] = parseHumanBytes(valStr)
						} else {
							shares[i]["quota"] = int64(0)
						}
					}
					if strings.HasPrefix(line, "Usage referenced:") {
						valStr := strings.TrimPrefix(line, "Usage referenced:")
						shares[i]["used"] = parseHumanBytes(strings.TrimSpace(valStr))
					}
				}
			}
			// Get available from df
			dfRes, err := runCmd("df", []string{"-B1", "--output=avail", subvolPath}, opts)
			if err == nil {
				lines := strings.Split(strings.TrimSpace(dfRes.Stdout), "\n")
				if len(lines) > 1 {
					var a int64
					fmt.Sscanf(strings.TrimSpace(lines[1]), "%d", &a)
					shares[i]["available"] = a
				}
			}
		}

		// File stats by category — scan the share directory
		shares[i]["fileStats"] = getFileStatsByCategory(sharePath)
	}
}

// getFileStatsByCategory scans a directory and returns bytes used per file category
func getFileStatsByCategory(dirPath string) map[string]int64 {
	stats := map[string]int64{
		"video":    0,
		"image":    0,
		"audio":    0,
		"document": 0,
		"other":    0,
	}

	// Use find + stat for efficiency — avoid walking huge trees in Go
	opts := CmdOptions{Timeout: 10 * time.Second}
	res, err := runCmd("find", []string{dirPath, "-type", "f", "-printf", "%s %f\\n"}, opts)
	if err != nil {
		return stats
	}

	videoExts := map[string]bool{"mp4": true, "mkv": true, "avi": true, "mov": true, "wmv": true, "flv": true, "webm": true, "m4v": true, "ts": true}
	imageExts := map[string]bool{"jpg": true, "jpeg": true, "png": true, "gif": true, "bmp": true, "svg": true, "webp": true, "tiff": true, "raw": true, "heic": true}
	audioExts := map[string]bool{"mp3": true, "flac": true, "wav": true, "aac": true, "ogg": true, "wma": true, "m4a": true, "opus": true}
	docExts := map[string]bool{"pdf": true, "doc": true, "docx": true, "xls": true, "xlsx": true, "ppt": true, "pptx": true, "txt": true, "csv": true, "md": true, "rtf": true, "odt": true}

	for _, line := range strings.Split(res.Stdout, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		spaceIdx := strings.IndexByte(line, ' ')
		if spaceIdx < 1 {
			continue
		}
		var size int64
		fmt.Sscanf(line[:spaceIdx], "%d", &size)
		fileName := strings.ToLower(line[spaceIdx+1:])

		ext := ""
		if dotIdx := strings.LastIndexByte(fileName, '.'); dotIdx >= 0 {
			ext = fileName[dotIdx+1:]
		}

		if videoExts[ext] {
			stats["video"] += size
		} else if imageExts[ext] {
			stats["image"] += size
		} else if audioExts[ext] {
			stats["audio"] += size
		} else if docExts[ext] {
			stats["document"] += size
		} else {
			stats["other"] += size
		}
	}

	return stats
}

// parseHumanBytes converts "55.88GiB" or "10.00MiB" to bytes
func parseHumanBytes(s string) int64 {
	s = strings.TrimSpace(s)
	multiplier := int64(1)
	if strings.HasSuffix(s, "GiB") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GiB")
	} else if strings.HasSuffix(s, "MiB") {
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "MiB")
	} else if strings.HasSuffix(s, "KiB") {
		multiplier = 1024
		s = strings.TrimSuffix(s, "KiB")
	} else if strings.HasSuffix(s, "TiB") {
		multiplier = 1024 * 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "TiB")
	}
	val, err := strconv.ParseFloat(strings.TrimSpace(s), 64)
	if err != nil {
		return 0
	}
	return int64(val * float64(multiplier))
}

// ═══════════════════════════════════
// Storage config helper (reads storage.json for pool info)
// ═══════════════════════════════════

const storageConfigFile = "/var/lib/nimbusos/config/storage.json"

type storageConfig struct {
	Pools       []map[string]interface{} `json:"pools"`
	PrimaryPool string                   `json:"primaryPool"`
}

func getStorageConfigGo() *storageConfig {
	data, err := os.ReadFile(storageConfigFile)
	if err != nil {
		return &storageConfig{}
	}
	var conf storageConfig
	json.Unmarshal(data, &conf)
	return &conf
}

func findTargetPool(poolName string) map[string]interface{} {
	conf := getStorageConfigGo()
	if len(conf.Pools) == 0 {
		return nil
	}
	if poolName != "" {
		for _, p := range conf.Pools {
			if n, _ := p["name"].(string); n == poolName {
				return p
			}
		}
	}
	// Return primary pool
	for _, p := range conf.Pools {
		if n, _ := p["name"].(string); n == conf.PrimaryPool {
			return p
		}
	}
	// Return first pool
	return conf.Pools[0]
}
