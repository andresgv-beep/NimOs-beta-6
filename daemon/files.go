package main

import (
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// ═══════════════════════════════════
// File Manager HTTP handlers
// ═══════════════════════════════════

func handleFilesRoutes(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path
	method := r.Method

	// Upload and download are special (binary, streaming)
	if urlPath == "/api/files/upload" && method == "POST" {
		handleFileUpload(w, r)
		return
	}
	if strings.HasPrefix(urlPath, "/api/files/download") && method == "GET" {
		handleFileDownload(w, r)
		return
	}

	session := requireAuth(w, r)
	if session == nil {
		return
	}

	switch {
	case strings.HasPrefix(urlPath, "/api/files") && method == "GET":
		filesBrowse(w, r, session)
	case urlPath == "/api/files/mkdir" && method == "POST":
		filesMkdir(w, r, session)
	case urlPath == "/api/files/delete" && method == "POST":
		filesDelete(w, r, session)
	case urlPath == "/api/files/rename" && method == "POST":
		filesRename(w, r, session)
	case urlPath == "/api/files/paste" && method == "POST":
		filesPaste(w, r, session)
	default:
		jsonError(w, 404, "Not found")
	}
}

// ═══════════════════════════════════
// Permission helpers
// ═══════════════════════════════════

func getSharePermission(session map[string]interface{}, share map[string]interface{}) string {
	// Remote shares: admin gets rw (NFS mount is already authenticated)
	if isRemote, _ := share["_remote"].(bool); isRemote {
		if role, _ := session["role"].(string); role == "admin" {
			return "rw"
		}
		return "ro"
	}
	if role, _ := session["role"].(string); role == "admin" {
		return "rw"
	}
	username, _ := session["username"].(string)
	if perms, ok := share["permissions"].(map[string]string); ok {
		if p, ok := perms[username]; ok {
			return p
		}
	}
	return "none"
}

// resolveShare looks up a share first in the local DB, then in remote_mounts.
// Returns a share-like map with at least "name" and "path" fields.
func resolveShare(name string) (map[string]interface{}, error) {
	// Try local DB first
	share, err := dbSharesGet(name)
	if err == nil && share != nil {
		return share, nil
	}

	// Try remote mounts — name format: "remote:<device>/<share>"
	// or just the shareName if it matches a remote mount
	if strings.HasPrefix(name, "remote:") {
		parts := strings.SplitN(strings.TrimPrefix(name, "remote:"), "/", 2)
		if len(parts) == 2 {
			// Look up by device name + share name
			rows, err := db.Query(`SELECT rm.mount_point, rm.share_name, bd.name
				FROM remote_mounts rm JOIN backup_devices bd ON rm.device_id = bd.id`)
			if err == nil {
				defer rows.Close()
				for rows.Next() {
					var mountPoint, shareName, devName string
					rows.Scan(&mountPoint, &shareName, &devName)
					safeDev := regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(devName, "_")
					if safeDev == parts[0] && shareName == parts[1] {
						return map[string]interface{}{
							"name":        name,
							"displayName": fmt.Sprintf("%s (%s)", shareName, devName),
							"path":        mountPoint,
							"pool":        "remote",
							"_remote":     true,
						}, nil
					}
				}
			}
		}
	}

	return nil, fmt.Errorf("share not found: %s", name)
}

func validatePathWithinShare(sharePath, subPath string) (string, error) {
	normalized := filepath.Clean(subPath)
	// Remove leading ..
	for strings.HasPrefix(normalized, "..") {
		normalized = strings.TrimPrefix(normalized, "..")
		normalized = strings.TrimPrefix(normalized, string(filepath.Separator))
	}
	full := filepath.Join(sharePath, normalized)
	resolved, _ := filepath.Abs(full)
	shareResolved, _ := filepath.Abs(sharePath)
	if !strings.HasPrefix(resolved, shareResolved) {
		return "", fmt.Errorf("invalid path: access denied")
	}
	return resolved, nil
}

// isPathOnMountedPool checks that the path is actually on a mounted pool,
// not on the root filesystem. This prevents writes to the system disk
// when a pool is destroyed but shares still exist in the DB.
func isPathOnMountedPool(path string) bool {
	if path == "" {
		return false
	}
	// Must be under /nimbus/pools/
	if !strings.HasPrefix(path, nimbusPoolsDir+"/") {
		return false
	}
	// Check that the path is on a different mount than /
	out, ok := run(fmt.Sprintf("findmnt -n -o SOURCE --target %s 2>/dev/null", path))
	if !ok || out == "" {
		return false
	}
	rootSource, _ := run("findmnt -n -o SOURCE --target / 2>/dev/null")
	// If the path's mount source is the same as /, it's writing to system disk
	if strings.TrimSpace(out) == strings.TrimSpace(rootSource) {
		return false
	}
	return true
}

// requireShareMounted checks if a share's pool is mounted, returns error response if not
func requireShareMounted(w http.ResponseWriter, share map[string]interface{}) bool {
	// Remote shares: quick check — try to stat the directory (non-blocking)
	// Don't use mountpoint command which can hang on dead NFS
	if isRemote, _ := share["_remote"].(bool); isRemote {
		mountPoint, _ := share["path"].(string)
		// Use os.Stat with a deadline via goroutine — 2s max
		done := make(chan bool, 1)
		go func() {
			_, err := os.Stat(mountPoint)
			done <- (err == nil)
		}()
		select {
		case ok := <-done:
			if ok {
				return true
			}
		case <-time.After(2 * time.Second):
			// Timed out — NFS is dead
		}
		jsonError(w, 503, "Remote share not available — device may be offline")
		return false
	}
	sharePath, _ := share["path"].(string)
	if !isPathOnMountedPool(sharePath) {
		jsonError(w, 503, "Storage pool not mounted — cannot access files")
		return false
	}
	return true
}

// ═══════════════════════════════════
// GET /api/files?share=name&path=/subdir
// ═══════════════════════════════════

func filesBrowse(w http.ResponseWriter, r *http.Request, session map[string]interface{}) {
	shareName := r.URL.Query().Get("share")
	subPath := r.URL.Query().Get("path")
	if subPath == "" {
		subPath = "/"
	}

	if shareName == "" {
		// Return list of accessible shares (local + remote)
		shares, _ := dbSharesList()
		username, _ := session["username"].(string)
		role, _ := session["role"].(string)
		var accessible []map[string]interface{}
		for _, s := range shares {
			perm := "none"
			if role == "admin" {
				perm = "rw"
			} else if perms, ok := s["permissions"].(map[string]string); ok {
				perm = perms[username]
			}
			if perm == "rw" || perm == "ro" {
				accessible = append(accessible, map[string]interface{}{
					"name":        s["name"],
					"displayName": s["displayName"],
					"description": s["description"],
					"permission":  perm,
				})
			}
		}

		// Add remote mounted shares (admin only for now)
		// NEVER run mountpoint checks here — NFS timeouts would block the entire listing.
		// Just list what's in the DB. Actual mount status is checked when browsing.
		if role == "admin" {
			rows, qerr := db.Query(`SELECT rm.device_id, rm.share_name, rm.mount_point, bd.name
				FROM remote_mounts rm JOIN backup_devices bd ON rm.device_id = bd.id`)
			if qerr == nil {
				defer rows.Close()
				for rows.Next() {
					var devID, shareName, mountPoint, devName string
					rows.Scan(&devID, &shareName, &mountPoint, &devName)
					safeDev := regexp.MustCompile(`[^a-zA-Z0-9_-]`).ReplaceAllString(devName, "_")
					accessible = append(accessible, map[string]interface{}{
						"name":        fmt.Sprintf("remote:%s/%s", safeDev, shareName),
						"displayName": fmt.Sprintf("%s (%s)", shareName, devName),
						"description": "Carpeta remota",
						"permission":  "rw",
						"remote":      true,
						"deviceName":  devName,
					})
				}
			}
		}

		if accessible == nil {
			accessible = []map[string]interface{}{}
		}
		jsonOk(w, map[string]interface{}{"shares": accessible})
		return
	}

	share, err := resolveShare(shareName)
	if err != nil || share == nil {
		jsonError(w, 404, "Shared folder not found")
		return
	}
	if !requireShareMounted(w, share) {
		return
	}

	perm := getSharePermission(session, share)
	if perm == "none" {
		jsonError(w, 403, "Access denied")
		return
	}

	sharePath, _ := share["path"].(string)
	fullPath, err := validatePathWithinShare(sharePath, subPath)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}

	entries, err := os.ReadDir(fullPath)
	if err != nil {
		jsonError(w, 400, "Cannot read directory")
		return
	}

	var files []map[string]interface{}
	for _, e := range entries {
		info, err := e.Info()
		size := int64(0)
		var modified interface{}
		modified = nil
		if err == nil {
			size = info.Size()
			modified = info.ModTime().UTC().Format("2006-01-02T15:04:05.000Z")
		}
		files = append(files, map[string]interface{}{
			"name":        e.Name(),
			"isDirectory": e.IsDir(),
			"size":        size,
			"modified":    modified,
		})
	}

	// Sort: directories first, then alphabetical
	sort.Slice(files, func(i, j int) bool {
		iDir := files[i]["isDirectory"].(bool)
		jDir := files[j]["isDirectory"].(bool)
		if iDir != jDir {
			return iDir
		}
		return strings.ToLower(files[i]["name"].(string)) < strings.ToLower(files[j]["name"].(string))
	})

	if files == nil {
		files = []map[string]interface{}{}
	}
	jsonOk(w, map[string]interface{}{
		"files":      files,
		"path":       subPath,
		"share":      shareName,
		"permission": perm,
	})
}

// ═══════════════════════════════════
// POST /api/files/mkdir
// ═══════════════════════════════════

func filesMkdir(w http.ResponseWriter, r *http.Request, session map[string]interface{}) {
	body, _ := readBody(r)
	shareName := bodyStr(body, "share")
	dirPath := bodyStr(body, "path")
	dirName := bodyStr(body, "name")

	if shareName == "" || dirName == "" {
		jsonError(w, 400, "Missing share or name")
		return
	}
	if strings.Contains(dirName, "..") || strings.Contains(dirName, "/") || strings.Contains(dirName, "\\") {
		jsonError(w, 400, "Invalid directory name")
		return
	}

	share, _ := resolveShare(shareName)
	if share == nil {
		jsonError(w, 404, "Shared folder not found")
		return
	}
	if !requireShareMounted(w, share) {
		return
	}
	if getSharePermission(session, share) != "rw" {
		jsonError(w, 403, "Write access denied")
		return
	}

	sharePath, _ := share["path"].(string)
	fullPath, err := validatePathWithinShare(sharePath, filepath.Join(dirPath, dirName))
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}

	if err := os.MkdirAll(fullPath, 0755); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOk(w, map[string]interface{}{"ok": true})
}

// ═══════════════════════════════════
// POST /api/files/delete
// ═══════════════════════════════════

func filesDelete(w http.ResponseWriter, r *http.Request, session map[string]interface{}) {
	body, _ := readBody(r)
	shareName := bodyStr(body, "share")
	filePath := bodyStr(body, "path")

	if shareName == "" || filePath == "" {
		jsonError(w, 400, "Missing share or path")
		return
	}

	share, _ := resolveShare(shareName)
	if share == nil {
		jsonError(w, 404, "Shared folder not found")
		return
	}
	if !requireShareMounted(w, share) {
		return
	}
	if getSharePermission(session, share) != "rw" {
		jsonError(w, 403, "Write access denied")
		return
	}

	sharePath, _ := share["path"].(string)
	fullPath, err := validatePathWithinShare(sharePath, filePath)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}

	shareResolved, _ := filepath.Abs(sharePath)
	if fullPath == shareResolved {
		jsonError(w, 400, "Cannot delete share root")
		return
	}

	if _, serr := os.Stat(fullPath); serr != nil {
		jsonError(w, 404, "File not found")
		return
	}
	if err := os.RemoveAll(fullPath); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOk(w, map[string]interface{}{"ok": true})
}

// ═══════════════════════════════════
// POST /api/files/rename
// ═══════════════════════════════════

func filesRename(w http.ResponseWriter, r *http.Request, session map[string]interface{}) {
	body, _ := readBody(r)
	shareName := bodyStr(body, "share")
	oldPath := bodyStr(body, "oldPath")
	newPath := bodyStr(body, "newPath")

	if shareName == "" || oldPath == "" || newPath == "" {
		jsonError(w, 400, "Missing params")
		return
	}

	share, _ := resolveShare(shareName)
	if share == nil {
		jsonError(w, 404, "Shared folder not found")
		return
	}
	if !requireShareMounted(w, share) {
		return
	}
	if getSharePermission(session, share) != "rw" {
		jsonError(w, 403, "Write access denied")
		return
	}

	sharePath, _ := share["path"].(string)
	fullOld, err := validatePathWithinShare(sharePath, oldPath)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	fullNew, err := validatePathWithinShare(sharePath, newPath)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}

	if err := os.Rename(fullOld, fullNew); err != nil {
		jsonError(w, 500, err.Error())
		return
	}
	jsonOk(w, map[string]interface{}{"ok": true})
}

// ═══════════════════════════════════
// POST /api/files/paste (copy or move)
// ═══════════════════════════════════

func filesPaste(w http.ResponseWriter, r *http.Request, session map[string]interface{}) {
	body, _ := readBody(r)
	srcShareName := bodyStr(body, "srcShare")
	srcPath := bodyStr(body, "srcPath")
	destShareName := bodyStr(body, "destShare")
	destPath := bodyStr(body, "destPath")
	action := bodyStr(body, "action")

	if srcShareName == "" || srcPath == "" || destShareName == "" || destPath == "" {
		jsonError(w, 400, "Missing params")
		return
	}

	srcShare, _ := resolveShare(srcShareName)
	destShare, _ := resolveShare(destShareName)
	if srcShare == nil || destShare == nil {
		jsonError(w, 404, "Share not found")
		return
	}
	if !requireShareMounted(w, destShare) {
		return
	}

	if getSharePermission(session, destShare) != "rw" {
		jsonError(w, 403, "Write access denied on destination")
		return
	}
	srcPerm := getSharePermission(session, srcShare)
	if srcPerm == "none" {
		jsonError(w, 403, "Read access denied on source")
		return
	}

	srcSharePath, _ := srcShare["path"].(string)
	destSharePath, _ := destShare["path"].(string)
	fullSrc, err := validatePathWithinShare(srcSharePath, srcPath)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}
	fullDest, err := validatePathWithinShare(destSharePath, destPath)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}

	if action == "cut" {
		if err := os.Rename(fullSrc, fullDest); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
	} else {
		// Copy recursively
		if _, ok := run(fmt.Sprintf(`cp -r "%s" "%s"`, fullSrc, fullDest)); !ok {
			jsonError(w, 500, "Copy failed")
			return
		}
	}
	jsonOk(w, map[string]interface{}{"ok": true})
}

// ═══════════════════════════════════
// POST /api/files/upload (multipart)
// ═══════════════════════════════════

func handleFileUpload(w http.ResponseWriter, r *http.Request) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}

	// Parse multipart (max 500MB)
	if err := r.ParseMultipartForm(500 << 20); err != nil {
		jsonError(w, 400, "Failed to parse upload")
		return
	}

	shareName := r.FormValue("share")
	uploadPath := r.FormValue("path")

	file, header, err := r.FormFile("file")
	if err != nil {
		jsonError(w, 400, "No file in upload")
		return
	}
	defer file.Close()

	if shareName == "" {
		jsonError(w, 400, "Missing share")
		return
	}

	share, _ := resolveShare(shareName)
	if share == nil {
		jsonError(w, 404, "Share not found")
		return
	}
	if !requireShareMounted(w, share) {
		return
	}
	if getSharePermission(session, share) != "rw" {
		jsonError(w, 403, "Write access denied")
		return
	}

	// Reject filenames with path traversal attempts in the raw input
	rawFilename := header.Filename
	if strings.Contains(rawFilename, "..") || strings.Contains(rawFilename, "/") || strings.Contains(rawFilename, "\\") {
		jsonError(w, 400, "Invalid filename")
		return
	}

	// Sanitize filename
	fileName := sanitizeFileName(rawFilename)
	if fileName == "" || len(fileName) > 255 {
		jsonError(w, 400, "Invalid filename")
		return
	}

	// Reject path traversal in upload path
	if strings.Contains(uploadPath, "..") {
		jsonError(w, 400, "Invalid upload path")
		return
	}

	sharePath, _ := share["path"].(string)
	fullPath, err := validatePathWithinShare(sharePath, filepath.Join(uploadPath, fileName))
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}

	// Check available space before writing
	availableBytes := getAvailableBytes(sharePath)
	fileSize := header.Size

	// Reject if we know the file is too big
	if fileSize > 0 && availableBytes > 0 && fileSize > availableBytes {
		jsonError(w, 507, fmt.Sprintf("Not enough space. File: %s, Available: %s",
			fmtSizeFiles(fileSize), fmtSizeFiles(availableBytes)))
		return
	}

	// Even if header.Size is unknown/zero, cap at available space
	maxWrite := availableBytes
	if maxWrite <= 0 {
		maxWrite = 500 * 1024 * 1024 // fallback 500MB if df fails
	}

	// Ensure parent dir exists
	os.MkdirAll(filepath.Dir(fullPath), 0755)

	dst, err := os.Create(fullPath)
	if err != nil {
		jsonError(w, 500, err.Error())
		return
	}

	// Write with size limit — never write more than available space
	written, copyErr := io.CopyN(dst, file, maxWrite)
	dst.Close()

	if copyErr != nil && copyErr != io.EOF {
		// Write failed — clean up partial file
		os.Remove(fullPath)
		jsonError(w, 507, "Write failed — disk full or quota exceeded")
		return
	}

	// Check if the file was truncated (more data remains but we hit the limit)
	if copyErr != io.EOF {
		// We wrote maxWrite bytes but there's more data — file was too big
		os.Remove(fullPath)
		jsonError(w, 507, fmt.Sprintf("File too large for available space. Written: %s, Available: %s",
			fmtSizeFiles(written), fmtSizeFiles(availableBytes)))
		return
	}

	jsonOk(w, map[string]interface{}{"ok": true, "name": fileName})
}

func sanitizeFileName(name string) string {
	// Extract only the base filename — strip any directory path components
	name = filepath.Base(name)
	// Reject . and .. explicitly
	if name == "." || name == ".." || name == "" {
		return ""
	}
	// Remove dangerous characters
	re := regexp.MustCompile(`[\/\\:*?"<>|]`)
	name = re.ReplaceAllString(name, "_")
	name = strings.ReplaceAll(name, "..", "")
	// Remove null bytes
	name = strings.ReplaceAll(name, "\x00", "")
	// Trim leading dots (hidden files on Linux)
	// This is optional — uncomment if you want to prevent hidden file creation
	// name = strings.TrimLeft(name, ".")
	if name == "" {
		return ""
	}
	return name
}

// ═══════════════════════════════════
// GET /api/files/download?share=...&path=...&token=...
// ═══════════════════════════════════

func handleFileDownload(w http.ResponseWriter, r *http.Request) {
	// Auth via query param token (for direct browser downloads)
	token := r.URL.Query().Get("token")
	if token == "" {
		token = getBearerToken(r)
	}
	if token == "" {
		jsonError(w, 401, "Not authenticated")
		return
	}
	hashed := sha256Hex(token)
	session, err := dbSessionGet(hashed)
	if err != nil {
		jsonError(w, 401, "Not authenticated")
		return
	}

	shareName := r.URL.Query().Get("share")
	filePath := r.URL.Query().Get("path")
	if shareName == "" || filePath == "" {
		jsonError(w, 400, "Missing params")
		return
	}

	share, _ := resolveShare(shareName)
	if share == nil {
		jsonError(w, 404, "Share not found")
		return
	}
	if getSharePermission(session, share) == "none" {
		jsonError(w, 403, "Access denied")
		return
	}

	sharePath, _ := share["path"].(string)
	fullPath, err := validatePathWithinShare(sharePath, filePath)
	if err != nil {
		jsonError(w, 400, err.Error())
		return
	}

	stat, err := os.Stat(fullPath)
	if err != nil {
		jsonError(w, 404, "File not found")
		return
	}

	fileName := filepath.Base(fullPath)
	ext := strings.ToLower(filepath.Ext(fileName))
	if ext != "" {
		ext = ext[1:] // remove dot
	}

	mimeTypes := map[string]string{
		"jpg": "image/jpeg", "jpeg": "image/jpeg", "png": "image/png", "gif": "image/gif",
		"webp": "image/webp", "svg": "image/svg+xml", "bmp": "image/bmp", "ico": "image/x-icon",
		"mp4": "video/mp4", "webm": "video/webm", "ogg": "video/ogg", "mov": "video/quicktime",
		"mkv": "video/x-matroska", "avi": "video/x-msvideo", "ogv": "video/ogg",
		"mp3": "audio/mpeg", "wav": "audio/wav", "flac": "audio/flac", "aac": "audio/aac",
		"m4a": "audio/mp4", "wma": "audio/x-ms-wma", "opus": "audio/opus",
		"pdf": "application/pdf",
		"txt": "text/plain", "md": "text/plain", "log": "text/plain", "csv": "text/plain",
		"json": "application/json", "xml": "text/xml", "yml": "text/yaml", "yaml": "text/yaml",
		"js": "text/javascript", "jsx": "text/javascript", "ts": "text/javascript",
		"py": "text/plain", "sh": "text/plain", "css": "text/css", "html": "text/html",
		"c": "text/plain", "cpp": "text/plain", "h": "text/plain", "java": "text/plain",
		"rs": "text/plain", "go": "text/plain", "rb": "text/plain", "php": "text/plain",
		"sql": "text/plain", "toml": "text/plain", "ini": "text/plain", "conf": "text/plain",
		"srt": "text/plain", "sub": "text/plain", "ass": "text/plain", "vtt": "text/vtt",
		"zip": "application/zip", "tar": "application/x-tar", "gz": "application/gzip",
		"7z": "application/x-7z-compressed", "rar": "application/x-rar-compressed",
	}

	contentType := "application/octet-stream"
	if ct, ok := mimeTypes[ext]; ok {
		contentType = ct
	}
	isDownload := contentType == "application/octet-stream"

	// Range request support (audio/video seeking)
	rangeHeader := r.Header.Get("Range")
	if rangeHeader != "" {
		re := regexp.MustCompile(`bytes=(\d+)-(\d*)`)
		m := re.FindStringSubmatch(rangeHeader)
		if m != nil {
			start, _ := strconv.ParseInt(m[1], 10, 64)
			end := stat.Size() - 1
			if m[2] != "" {
				end, _ = strconv.ParseInt(m[2], 10, 64)
			}
			chunkSize := end - start + 1

			f, err := os.Open(fullPath)
			if err != nil {
				jsonError(w, 500, "Cannot open file")
				return
			}
			defer f.Close()
			f.Seek(start, 0)

			w.Header().Set("Content-Type", contentType)
			w.Header().Set("Content-Range", fmt.Sprintf("bytes %d-%d/%d", start, end, stat.Size()))
			w.Header().Set("Accept-Ranges", "bytes")
			w.Header().Set("Content-Length", fmt.Sprintf("%d", chunkSize))
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.WriteHeader(206)
			io.CopyN(w, f, chunkSize)
			return
		}
	}

	// Full file
	w.Header().Set("Content-Type", contentType)
	w.Header().Set("Content-Length", fmt.Sprintf("%d", stat.Size()))
	w.Header().Set("Accept-Ranges", "bytes")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	if isDownload {
		w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s"`, fileName))
	}
	w.WriteHeader(200)

	f, err := os.Open(fullPath)
	if err != nil {
		return
	}
	defer f.Close()
	io.Copy(w, f)
}

// getAvailableBytes returns available bytes for writing to the given path.
// For BTRFS subvolumes with quota, uses btrfs subvolume show (quota limit - usage).
// For ZFS datasets with quota, uses zfs get.
// Falls back to df for other filesystems.
func getAvailableBytes(path string) int64 {
	// Try BTRFS quota first
	if out, ok := run(fmt.Sprintf("btrfs subvolume show %s 2>/dev/null", path)); ok && out != "" {
		var limitBytes, usedBytes int64
		for _, line := range strings.Split(out, "\n") {
			line = strings.TrimSpace(line)
			if strings.HasPrefix(line, "Limit referenced:") {
				val := strings.TrimSpace(strings.TrimPrefix(line, "Limit referenced:"))
				if val != "-" && val != "none" {
					limitBytes = parseHumanBytesFiles(val)
				}
			}
			if strings.HasPrefix(line, "Usage referenced:") {
				val := strings.TrimSpace(strings.TrimPrefix(line, "Usage referenced:"))
				usedBytes = parseHumanBytesFiles(val)
			}
		}
		if limitBytes > 0 {
			avail := limitBytes - usedBytes
			if avail < 0 {
				avail = 0
			}
			return avail
		}
	}

	// Try ZFS quota
	// Find dataset name from path (e.g., /nimbus/pools/volume2/shares/data → nimos-volume2/shares/data)
	if strings.HasPrefix(path, "/nimbus/pools/") {
		parts := strings.Split(strings.TrimPrefix(path, "/nimbus/pools/"), "/")
		if len(parts) >= 1 {
			dataset := "nimos-" + strings.Join(parts, "/")
			if out, ok := run(fmt.Sprintf("zfs get -Hp -o value available %s 2>/dev/null", dataset)); ok && out != "" {
				var n int64
				fmt.Sscanf(strings.TrimSpace(out), "%d", &n)
				if n > 0 {
					return n
				}
			}
		}
	}

	// Fallback to df
	out, ok := run(fmt.Sprintf("df -B1 --output=avail %s 2>/dev/null | tail -1", path))
	if !ok {
		return 0
	}
	s := strings.TrimSpace(out)
	var n int64
	fmt.Sscanf(s, "%d", &n)
	return n
}

// parseHumanBytesFiles parses strings like "4.66GiB", "7.20GiB", "500.00MiB" into bytes.
func parseHumanBytesFiles(s string) int64 {
	s = strings.TrimSpace(s)
	if s == "" || s == "-" || s == "none" {
		return 0
	}

	multiplier := int64(1)
	if strings.HasSuffix(s, "TiB") {
		multiplier = 1024 * 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "TiB")
	} else if strings.HasSuffix(s, "GiB") {
		multiplier = 1024 * 1024 * 1024
		s = strings.TrimSuffix(s, "GiB")
	} else if strings.HasSuffix(s, "MiB") {
		multiplier = 1024 * 1024
		s = strings.TrimSuffix(s, "MiB")
	} else if strings.HasSuffix(s, "KiB") {
		multiplier = 1024
		s = strings.TrimSuffix(s, "KiB")
	} else if strings.HasSuffix(s, "B") {
		s = strings.TrimSuffix(s, "B")
	}

	var val float64
	fmt.Sscanf(strings.TrimSpace(s), "%f", &val)
	return int64(val * float64(multiplier))
}

func fmtSizeFiles(b int64) string {
	if b >= 1e9 {
		return fmt.Sprintf("%.1f GB", float64(b)/1e9)
	}
	if b >= 1e6 {
		return fmt.Sprintf("%.0f MB", float64(b)/1e6)
	}
	return fmt.Sprintf("%.0f KB", float64(b)/1e3)
}
