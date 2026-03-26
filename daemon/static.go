package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ═══════════════════════════════════
// Static file serving + Torrent proxy
// ═══════════════════════════════════

const (
	installDir = "/opt/nimbusos"
	distDir    = "/opt/nimbusos/dist"
	publicDir  = "/opt/nimbusos/public"
)

var mimeTypes = map[string]string{
	".html": "text/html", ".js": "application/javascript", ".css": "text/css",
	".json": "application/json", ".png": "image/png", ".jpg": "image/jpeg",
	".jpeg": "image/jpeg", ".gif": "image/gif", ".svg": "image/svg+xml",
	".ico": "image/x-icon", ".woff": "font/woff", ".woff2": "font/woff2",
	".ttf": "font/ttf", ".webp": "image/webp", ".mp4": "video/mp4",
	".webm": "video/webm", ".map": "application/json",
}

// Serve static files from dist/ (React/Svelte frontend)
func serveStatic(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path

	// App icons from public/
	if strings.HasPrefix(urlPath, "/app-icons/") {
		iconName := filepath.Base(urlPath)
		if matched, _ := regexp.MatchString(`^[a-zA-Z0-9_-]+\.(svg|png|jpg|jpeg|webp|ico)$`, iconName); !matched {
			http.Error(w, "Invalid", 400)
			return
		}
		iconPath := filepath.Join(publicDir, "app-icons", iconName)
		if data, err := os.ReadFile(iconPath); err == nil {
			ext := strings.ToLower(filepath.Ext(iconName))
			w.Header().Set("Content-Type", mimeTypes[ext])
			w.Header().Set("Cache-Control", "public, max-age=86400")
			w.Write(data)
			return
		}
		http.Error(w, "Not found", 404)
		return
	}

	// Try dist/ first
	if _, err := os.Stat(distDir); err == nil {
		filePath := filepath.Join(distDir, urlPath)
		if urlPath == "/" {
			filePath = filepath.Join(distDir, "index.html")
		}

		// Security: prevent path traversal
		absFile, _ := filepath.Abs(filePath)
		absDist, _ := filepath.Abs(distDir)
		if !strings.HasPrefix(absFile, absDist) {
			http.Error(w, "Forbidden", 403)
			return
		}

		// If file doesn't exist or is directory, serve index.html (SPA routing)
		info, err := os.Stat(filePath)
		if err != nil || info.IsDir() {
			filePath = filepath.Join(distDir, "index.html")
		}

		if data, err := os.ReadFile(filePath); err == nil {
			ext := strings.ToLower(filepath.Ext(filePath))
			ct := mimeTypes[ext]
			if ct == "" {
				ct = "application/octet-stream"
			}
			cacheControl := "public, max-age=31536000, immutable"
			if ext == ".html" {
				cacheControl = "no-cache"
			}
			w.Header().Set("Content-Type", ct)
			w.Header().Set("Cache-Control", cacheControl)
			w.Write(data)
			return
		}
	}

	// Public dir fallback
	pubFile := filepath.Join(publicDir, urlPath)
	absPub, _ := filepath.Abs(pubFile)
	absPublic, _ := filepath.Abs(publicDir)
	if strings.HasPrefix(absPub, absPublic) {
		if data, err := os.ReadFile(pubFile); err == nil {
			ext := strings.ToLower(filepath.Ext(pubFile))
			ct := mimeTypes[ext]
			if ct == "" {
				ct = "application/octet-stream"
			}
			w.Header().Set("Content-Type", ct)
			w.Write(data)
			return
		}
	}

	http.Error(w, "Not found", 404)
}

// ═══════════════════════════════════
// Torrent proxy to NimTorrent daemon (:9091)
// ═══════════════════════════════════

func handleTorrentProxy(w http.ResponseWriter, r *http.Request) {
	// Auth check — torrent needs app access
	session := requireAppAccess(w, r, "nimtorrent")
	if session == nil {
		return
	}

	urlPath := r.URL.Path

	// Special: torrent file upload (multipart)
	if urlPath == "/api/torrent/upload" && r.Method == "POST" {
		handleTorrentUploadGo(w, r)
		return
	}

	// Regular proxy to NimTorrent
	daemonPath := strings.Replace(urlPath, "/api/torrent", "", 1)
	if daemonPath == "" || daemonPath == "/" {
		daemonPath = "/torrents"
	}
	// torrentd expects /torrent/add, /torrent/pause, etc.
	// but /torrents and /stats are root-level
	if daemonPath != "/torrents" && daemonPath != "/stats" && daemonPath != "/settings" && daemonPath != "/save" && !strings.HasPrefix(daemonPath, "/torrent/") {
		daemonPath = "/torrent" + daemonPath
	}

	// Read body
	body, _ := io.ReadAll(io.LimitReader(r.Body, 1*1024*1024))

	// Proxy to NimTorrent
	client := &http.Client{Timeout: 30 * time.Second}
	var proxyReq *http.Request
	var err error

	targetURL := fmt.Sprintf("http://127.0.0.1:9091%s", daemonPath)
	if len(body) > 0 {
		proxyReq, err = http.NewRequest(r.Method, targetURL, strings.NewReader(string(body)))
	} else {
		proxyReq, err = http.NewRequest(r.Method, targetURL, nil)
	}
	if err != nil {
		jsonError(w, 500, "Proxy error")
		return
	}
	proxyReq.Header.Set("Content-Type", r.Header.Get("Content-Type"))

	resp, err := client.Do(proxyReq)
	if err != nil {
		jsonError(w, 503, "Torrent daemon not running")
		return
	}
	defer resp.Body.Close()

	// Forward response
	w.Header().Set("Content-Type", resp.Header.Get("Content-Type"))
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(resp.StatusCode)
	io.Copy(w, resp.Body)
}

// Torrent file upload: parse multipart, save .torrent to disk, forward as JSON to NimTorrent
func handleTorrentUploadGo(w http.ResponseWriter, r *http.Request) {
	// Parse multipart (max 50MB)
	if err := r.ParseMultipartForm(50 << 20); err != nil {
		jsonError(w, 400, "Failed to parse upload")
		return
	}

	savePath := r.FormValue("save_path")
	if savePath == "" {
		savePath = "/data/torrents"
	}

	file, header, err := r.FormFile("torrent")
	if err != nil {
		jsonError(w, 400, "No .torrent file found")
		return
	}
	defer file.Close()

	// Save to temp
	tmpDir := "/var/cache/nimos-torrents"
	os.MkdirAll(tmpDir, 0755)
	safeName := regexp.MustCompile(`[^a-zA-Z0-9._-]`).ReplaceAllString(header.Filename, "_")
	tmpPath := filepath.Join(tmpDir, fmt.Sprintf("%d-%s", time.Now().UnixMilli(), safeName))

	dst, err := os.Create(tmpPath)
	if err != nil {
		jsonError(w, 500, "Failed to save temp file")
		return
	}
	io.Copy(dst, file)
	dst.Close()

	// Forward to NimTorrent as JSON
	postData, _ := json.Marshal(map[string]string{
		"file":      tmpPath,
		"save_path": savePath,
	})

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Post("http://127.0.0.1:9091/torrent/add", "application/json", strings.NewReader(string(postData)))

	// Cleanup temp file
	defer os.Remove(tmpPath)

	if err != nil {
		jsonError(w, 503, "Torrent daemon not running")
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.WriteHeader(200)
	if len(respBody) > 0 {
		w.Write(respBody)
	} else {
		w.Write([]byte(`{"ok":true}`))
	}
}
