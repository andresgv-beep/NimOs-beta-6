package main

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
)

// ═══════════════════════════════════
// Constants
// ═══════════════════════════════════

const (
	configDir      = "/var/lib/nimbusos/config"
	appsFile       = "/var/lib/nimbusos/config/installed-apps.json"
	nativeAppsFile = "/var/lib/nimbusos/config/native-apps.json"
	nimbusRoot     = "/var/lib/nimbusos"
)

// ═══════════════════════════════════
// Known native apps catalog
// ═══════════════════════════════════

type nativeAppDef struct {
	Name             string
	Description      string
	Category         string
	CheckCommand     string
	InstallCommand   string
	UninstallCommand string
	Port             int
	Icon             string
	Color            string
	IsNativeApp      bool
	IsDesktop        bool
	NimbusApp        string
}

var knownNativeApps = map[string]nativeAppDef{
	"virtualization": {
		Name:             "Virtual Machines (KVM)",
		Description:      "Full virtualization with QEMU/KVM. Create and manage virtual machines.",
		Category:         "system",
		CheckCommand:     "which virsh 2>/dev/null && which qemu-system-x86_64 2>/dev/null",
		InstallCommand:   "sudo apt install -y qemu-kvm libvirt-daemon-system libvirt-clients bridge-utils virt-install virtinst && sudo systemctl enable libvirtd && sudo systemctl start libvirtd && sudo mkdir -p /var/lib/nimbusos/vms /var/lib/nimbusos/isos",
		UninstallCommand: "sudo apt remove -y qemu-kvm libvirt-daemon-system libvirt-clients virt-install virtinst",
		Port:             0,
		Icon:             "/app-icons/virtualization.svg",
		Color:            "#7C4DFF",
		IsNativeApp:      true,
		NimbusApp:        "vms",
	},
	"transmission": {
		Name:             "Transmission",
		Description:      "Lightweight BitTorrent client with web interface and RPC API.",
		Category:         "downloads",
		CheckCommand:     "which transmission-daemon 2>/dev/null",
		InstallCommand:   "sudo apt install -y transmission-daemon && sudo systemctl stop transmission-daemon && sudo mkdir -p /etc/transmission-daemon /nimbus/downloads && sudo usermod -a -G debian-transmission nimbus 2>/dev/null; sudo systemctl enable transmission-daemon",
		UninstallCommand: "sudo systemctl stop transmission-daemon; sudo systemctl disable transmission-daemon; sudo apt remove -y transmission-daemon",
		Port:             9091,
		Icon:             "/app-icons/transmission.svg",
		Color:            "#B50D0D",
		IsNativeApp:      true,
		NimbusApp:        "downloads",
	},
	"amule": {
		Name:             "aMule",
		Description:      "eMule-compatible P2P client for ed2k and Kademlia networks.",
		Category:         "downloads",
		CheckCommand:     "systemctl is-active amuled || which amuled 2>/dev/null",
		InstallCommand:   "sudo apt install -y amule-daemon amule-utils && sudo systemctl enable amuled 2>/dev/null",
		UninstallCommand: "sudo systemctl stop amuled; sudo apt remove -y amule-daemon amule-utils",
		Port:             4711,
		Icon:             "/app-icons/amule.svg",
		Color:            "#0078D4",
		IsNativeApp:      true,
	},
	"onlyoffice": {
		Name:         "OnlyOffice",
		CheckCommand: "which onlyoffice-desktopeditors || snap list onlyoffice-desktopeditors 2>/dev/null || ls /snap/bin/onlyoffice* 2>/dev/null || flatpak list 2>/dev/null | grep -i onlyoffice",
		Icon:         "/app-icons/onlyoffice.svg",
		Color:        "#FF6F3D",
		IsDesktop:    true,
	},
	"samba": {
		Name:           "Samba (SMB)",
		CheckCommand:   "systemctl is-active smbd",
		InstallCommand: "sudo apt install -y samba",
		Port:           445,
		Icon:           "📁",
		Color:          "#4A90A4",
	},
	"libreoffice": {
		Name:         "LibreOffice",
		CheckCommand: "which libreoffice || snap list libreoffice 2>/dev/null",
		Icon:         "/app-icons/libreoffice.svg",
		Color:        "#18A303",
		IsDesktop:    true,
	},
}

// ═══════════════════════════════════
// Installed apps (JSON file)
// ═══════════════════════════════════

func getInstalledApps() []map[string]interface{} {
	data, err := os.ReadFile(appsFile)
	if err != nil {
		return []map[string]interface{}{}
	}
	var apps []map[string]interface{}
	if json.Unmarshal(data, &apps) != nil {
		return []map[string]interface{}{}
	}
	return apps
}

func saveInstalledApps(apps []map[string]interface{}) {
	data, _ := json.MarshalIndent(apps, "", "  ")
	os.WriteFile(appsFile, data, 0644)
}

// ═══════════════════════════════════
// Native apps detection
// ═══════════════════════════════════

func detectNativeApp(appId string) (installed bool, running bool) {
	def, ok := knownNativeApps[appId]
	if !ok {
		return false, false
	}
	out, ok := run(def.CheckCommand)
	if !ok {
		return false, false
	}
	isActive := out == "active" || len(out) > 0
	return true, isActive
}

func detectAllNativeApps() []map[string]interface{} {
	var results []map[string]interface{}
	for id, def := range knownNativeApps {
		installed, running := detectNativeApp(id)
		if installed {
			entry := map[string]interface{}{
				"id":        id,
				"name":      def.Name,
				"icon":      def.Icon,
				"color":     def.Color,
				"type":      "native",
				"isDesktop": def.IsDesktop,
				"running":   running,
			}
			if def.Port > 0 {
				entry["port"] = def.Port
			}
			if def.NimbusApp != "" {
				entry["nimbusApp"] = def.NimbusApp
			}
			results = append(results, entry)
		}
	}
	if results == nil {
		results = []map[string]interface{}{}
	}
	return results
}

func getNativeAppsConfig() []map[string]interface{} {
	data, err := os.ReadFile(nativeAppsFile)
	if err != nil {
		return []map[string]interface{}{}
	}
	var apps []map[string]interface{}
	json.Unmarshal(data, &apps)
	return apps
}

func saveNativeAppsConfig(apps []map[string]interface{}) {
	data, _ := json.MarshalIndent(apps, "", "  ")
	os.WriteFile(nativeAppsFile, data, 0644)
}

// ═══════════════════════════════════
// Native Apps HTTP handlers
// ═══════════════════════════════════

func handleNativeAppsRoutes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	method := r.Method

	// GET /api/native-apps
	if path == "/api/native-apps" && method == "GET" {
		session := requireAuth(w, r)
		if session == nil {
			return
		}
		jsonOk(w, map[string]interface{}{"apps": detectAllNativeApps()})
		return
	}

	// GET /api/native-apps/available
	if path == "/api/native-apps/available" && method == "GET" {
		session := requireAuth(w, r)
		if session == nil {
			return
		}
		var available []map[string]interface{}
		for id, def := range knownNativeApps {
			installed, running := detectNativeApp(id)
			entry := map[string]interface{}{
				"id":           id,
				"name":         def.Name,
				"description":  def.Description,
				"category":     def.Category,
				"icon":         def.Icon,
				"color":        def.Color,
				"installed":    installed,
				"running":      running,
				"isDesktop":    def.IsDesktop,
				"isNativeApp":  def.IsNativeApp,
			}
			if def.Port > 0 {
				entry["port"] = def.Port
			}
			if def.InstallCommand != "" {
				entry["installCommand"] = def.InstallCommand
			}
			if def.UninstallCommand != "" {
				entry["uninstallCommand"] = def.UninstallCommand
			}
			if def.NimbusApp != "" {
				entry["nimbusApp"] = def.NimbusApp
			}
			if def.Category == "" {
				entry["category"] = "system"
			}
			available = append(available, entry)
		}
		jsonOk(w, map[string]interface{}{"apps": available})
		return
	}

	// Regex routes: /api/native-apps/:id/action
	appActionRegex := regexp.MustCompile(`^/api/native-apps/([a-zA-Z0-9_-]+)/(start|stop|install|install-status|uninstall|status)$`)
	matches := appActionRegex.FindStringSubmatch(path)
	if matches == nil {
		jsonError(w, 404, "Not found")
		return
	}

	appId := matches[1]
	action := matches[2]

	switch action {
	case "start":
		nativeAppStart(w, r, appId)
	case "stop":
		nativeAppStop(w, r, appId)
	case "install":
		nativeAppInstall(w, r, appId)
	case "install-status":
		nativeAppInstallStatus(w, r, appId)
	case "uninstall":
		nativeAppUninstall(w, r, appId)
	case "status":
		nativeAppStatus(w, r, appId)
	}
}

func nativeAppStart(w http.ResponseWriter, r *http.Request, appId string) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	if _, ok := knownNativeApps[appId]; !ok {
		jsonError(w, 404, "Unknown app")
		return
	}
	// Try multiple service name patterns
	cmd := fmt.Sprintf("sudo systemctl start %s-daemon 2>/dev/null || sudo systemctl start %sd 2>/dev/null || sudo systemctl start %s 2>/dev/null", appId, appId, appId)
	if _, ok := run(cmd); !ok {
		jsonError(w, 500, "Failed to start service")
		return
	}
	jsonOk(w, map[string]interface{}{"ok": true, "appId": appId})
}

func nativeAppStop(w http.ResponseWriter, r *http.Request, appId string) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	if _, ok := knownNativeApps[appId]; !ok {
		jsonError(w, 404, "Unknown app")
		return
	}
	cmd := fmt.Sprintf("sudo systemctl stop %s-daemon 2>/dev/null || sudo systemctl stop %sd 2>/dev/null || sudo systemctl stop %s 2>/dev/null", appId, appId, appId)
	run(cmd)
	jsonOk(w, map[string]interface{}{"ok": true, "appId": appId})
}

func nativeAppInstall(w http.ResponseWriter, r *http.Request, appId string) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	def, ok := knownNativeApps[appId]
	if !ok {
		jsonError(w, 404, "Unknown app")
		return
	}
	if def.InstallCommand == "" {
		jsonError(w, 400, "No install command defined")
		return
	}

	logDir := "/var/log/nimbusos"
	os.MkdirAll(logDir, 0755)
	statusFile := filepath.Join(logDir, fmt.Sprintf("install-%s.json", appId))

	// Mark as installing
	statusData, _ := json.Marshal(map[string]interface{}{
		"status":    "installing",
		"appId":     appId,
		"startedAt": fmt.Sprintf("%v", os.Getpid()), // timestamp placeholder
	})
	os.WriteFile(statusFile, statusData, 0644)

	// Run install asynchronously
	go func() {
		logFile := filepath.Join(logDir, fmt.Sprintf("install-%s.log", appId))
		out, err := exec.Command("bash", "-c", def.InstallCommand).CombinedOutput()
		os.WriteFile(logFile, out, 0644)

		if err == nil {
			// Register native app
			apps := getNativeAppsConfig()
			filtered := make([]map[string]interface{}, 0)
			for _, a := range apps {
				if aid, _ := a["id"].(string); aid != appId {
					filtered = append(filtered, a)
				}
			}
			filtered = append(filtered, map[string]interface{}{
				"id":    appId,
				"name":  def.Name,
				"icon":  def.Icon,
				"color": def.Color,
				"type":  "native",
			})
			saveNativeAppsConfig(filtered)

			statusData, _ := json.Marshal(map[string]interface{}{"status": "done", "appId": appId, "code": 0})
			os.WriteFile(statusFile, statusData, 0644)
		} else {
			statusData, _ := json.Marshal(map[string]interface{}{"status": "error", "appId": appId, "code": 1})
			os.WriteFile(statusFile, statusData, 0644)
		}
	}()

	jsonOk(w, map[string]interface{}{
		"ok":      true,
		"appId":   appId,
		"async":   true,
		"message": "Installation started",
	})
}

func nativeAppInstallStatus(w http.ResponseWriter, r *http.Request, appId string) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	statusFile := filepath.Join("/var/log/nimbusos", fmt.Sprintf("install-%s.json", appId))
	data, err := os.ReadFile(statusFile)
	if err != nil {
		jsonOk(w, map[string]interface{}{"status": "unknown"})
		return
	}
	var status map[string]interface{}
	json.Unmarshal(data, &status)
	jsonOk(w, status)
}

func nativeAppUninstall(w http.ResponseWriter, r *http.Request, appId string) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	def, ok := knownNativeApps[appId]
	if !ok {
		jsonError(w, 404, "Unknown app")
		return
	}
	if def.UninstallCommand != "" {
		if _, ok := run(def.UninstallCommand); !ok {
			jsonError(w, 500, "Uninstall failed")
			return
		}
	}
	// Remove from native apps config
	apps := getNativeAppsConfig()
	filtered := make([]map[string]interface{}, 0)
	for _, a := range apps {
		if aid, _ := a["id"].(string); aid != appId {
			filtered = append(filtered, a)
		}
	}
	saveNativeAppsConfig(filtered)
	jsonOk(w, map[string]interface{}{"ok": true, "appId": appId})
}

func nativeAppStatus(w http.ResponseWriter, r *http.Request, appId string) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	def, ok := knownNativeApps[appId]
	if !ok {
		jsonError(w, 404, "Unknown app")
		return
	}
	installed, running := detectNativeApp(appId)
	result := map[string]interface{}{
		"id":        appId,
		"name":      def.Name,
		"installed": installed,
		"running":   running,
	}
	if def.Port > 0 {
		result["port"] = def.Port
	}
	jsonOk(w, result)
}

// ═══════════════════════════════════
// Installed Apps HTTP handlers
// ═══════════════════════════════════

func handleInstalledAppsRoutes(w http.ResponseWriter, r *http.Request) {
	path := r.URL.Path
	method := r.Method

	// GET /api/installed-apps
	if path == "/api/installed-apps" && method == "GET" {
		session := requireAuth(w, r)
		if session == nil {
			return
		}
		jsonOk(w, getInstalledApps())
		return
	}

	// POST /api/installed-apps — register an app
	if path == "/api/installed-apps" && method == "POST" {
		session := requireAdmin(w, r)
		if session == nil {
			return
		}
		body, _ := readBody(r)
		appId := bodyStr(body, "id")
		if appId == "" {
			jsonError(w, 400, "App ID required")
			return
		}

		apps := getInstalledApps()
		// Remove existing
		filtered := make([]map[string]interface{}, 0)
		for _, a := range apps {
			if aid, _ := a["id"].(string); aid != appId {
				filtered = append(filtered, a)
			}
		}

		iconPath := bodyStr(body, "icon")
		if iconPath == "" {
			iconPath = "📦"
		}
		// Icon download from URL is handled if icon starts with http
		if strings.HasPrefix(iconPath, "http") {
			localPath := downloadAppIcon(appId, iconPath)
			if localPath != "" {
				iconPath = localPath
			}
		}

		filtered = append(filtered, map[string]interface{}{
			"id":          appId,
			"name":        bodyStr(body, "name"),
			"icon":        iconPath,
			"port":        body["port"],
			"image":       bodyStr(body, "image"),
			"type":        bodyStr(body, "type"),
			"color":       bodyStr(body, "color"),
			"external":    body["external"],
			"installedAt": fmt.Sprintf("%v", os.Getpid()),
			"installedBy": session["username"],
		})
		saveInstalledApps(filtered)
		jsonOk(w, map[string]interface{}{"ok": true})
		return
	}

	// DELETE /api/installed-apps/:id
	appDelRegex := regexp.MustCompile(`^/api/installed-apps/([a-zA-Z0-9_.-]+)$`)
	if matches := appDelRegex.FindStringSubmatch(path); matches != nil && method == "DELETE" {
		session := requireAdmin(w, r)
		if session == nil {
			return
		}
		appId := matches[1]
		apps := getInstalledApps()
		filtered := make([]map[string]interface{}, 0)
		for _, a := range apps {
			if aid, _ := a["id"].(string); aid != appId {
				filtered = append(filtered, a)
			}
		}
		saveInstalledApps(filtered)
		jsonOk(w, map[string]interface{}{"ok": true})
		return
	}

	jsonError(w, 404, "Not found")
}

func downloadAppIcon(appId, iconUrl string) string {
	// Validate URL format to prevent command injection
	if !strings.HasPrefix(iconUrl, "http://") && !strings.HasPrefix(iconUrl, "https://") {
		return ""
	}
	// Reject URLs with shell-dangerous characters
	if strings.ContainsAny(iconUrl, "\"'`$;|&<>(){}\\") {
		return ""
	}

	// Try common locations
	for _, dir := range []string{
		"/opt/nimbusos/public/app-icons",
		filepath.Join(nimbusRoot, "app-icons"),
	} {
		os.MkdirAll(dir, 0755)
		ext := ".png"
		if strings.Contains(iconUrl, ".svg") {
			ext = ".svg"
		} else if strings.Contains(iconUrl, ".jpg") || strings.Contains(iconUrl, ".jpeg") {
			ext = ".jpg"
		} else if strings.Contains(iconUrl, ".webp") {
			ext = ".webp"
		}
		localPath := filepath.Join(dir, appId+ext)
		// Use exec.Command instead of shell interpolation to avoid injection
		cmd := exec.Command("curl", "-sL", "-o", localPath, "--max-time", "15", iconUrl)
		if err := cmd.Run(); err == nil {
			return "/app-icons/" + appId + ext
		}
	}
	return ""
}
