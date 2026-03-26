package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// ═══════════════════════════════════
// Docker config (JSON file)
// ═══════════════════════════════════

const dockerConfigFile = "/var/lib/nimbusos/config/docker.json"

func getDockerConfigGo() map[string]interface{} {
	data, err := os.ReadFile(dockerConfigFile)
	if err != nil {
		return map[string]interface{}{"installed": false, "path": nil, "permissions": []interface{}{}, "appPermissions": map[string]interface{}{}, "installedAt": nil, "containers": []interface{}{}}
	}
	var conf map[string]interface{}
	if json.Unmarshal(data, &conf) != nil {
		return map[string]interface{}{"installed": false, "path": nil, "permissions": []interface{}{}, "appPermissions": map[string]interface{}{}}
	}
	if conf["appPermissions"] == nil {
		conf["appPermissions"] = map[string]interface{}{}
	}
	if conf["permissions"] == nil {
		conf["permissions"] = []interface{}{}
	}
	return conf
}

func saveDockerConfigGo(config map[string]interface{}) {
	data, _ := json.MarshalIndent(config, "", "  ")
	os.WriteFile(dockerConfigFile, data, 0644)
}

// getDockerPath returns the docker data path on the pool.
// Priority: docker.json path → primary pool → first pool → error
// NEVER returns /var/lib/docker — data must live on a pool.
func getDockerPath() (string, error) {
	// 1. Try docker.json config
	conf := getDockerConfigGo()
	if p, _ := conf["path"].(string); p != "" && strings.HasPrefix(p, "/nimbus/pools/") {
		return p, nil
	}

	// 2. Try to find from storage pools
	storageConf := getStorageConfigFull()
	confPools, _ := storageConf["pools"].([]interface{})
	if len(confPools) == 0 {
		return "", fmt.Errorf("no storage pools available")
	}

	// Find primary pool or first pool
	primaryPool, _ := storageConf["primaryPool"].(string)
	for _, p := range confPools {
		pm, _ := p.(map[string]interface{})
		name, _ := pm["name"].(string)
		mountPoint, _ := pm["mountPoint"].(string)
		if name == primaryPool && mountPoint != "" {
			dockerPath := filepath.Join(mountPoint, "docker")
			// Save to docker.json for next time
			conf["path"] = dockerPath
			saveDockerConfigGo(conf)
			return dockerPath, nil
		}
	}

	// Use first pool
	pm, _ := confPools[0].(map[string]interface{})
	if mountPoint, _ := pm["mountPoint"].(string); mountPoint != "" {
		dockerPath := filepath.Join(mountPoint, "docker")
		conf["path"] = dockerPath
		saveDockerConfigGo(conf)
		return dockerPath, nil
	}

	return "", fmt.Errorf("no pool with valid mount point found")
}

func isDockerInstalledGo() bool {
	_, ok := run("docker --version 2>/dev/null")
	return ok
}

func getRealContainersGo() []map[string]interface{} {
	out, ok := run(`docker ps -a --format "{{.ID}}|{{.Names}}|{{.Image}}|{{.Status}}|{{.Ports}}|{{.State}}" 2>/dev/null`)
	if !ok || out == "" {
		return []map[string]interface{}{}
	}
	var containers []map[string]interface{}
	for _, line := range strings.Split(out, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 6)
		if len(parts) < 6 {
			continue
		}
		ports := "—"
		if parts[4] != "" {
			ports = parts[4]
		}
		containers = append(containers, map[string]interface{}{
			"id": parts[0], "name": parts[1], "image": parts[2],
			"status": parts[3], "ports": ports, "state": parts[5],
		})
	}
	if containers == nil {
		return []map[string]interface{}{}
	}
	return containers
}

func sanitizeDockerNameGo(name string) string {
	if name == "" {
		return ""
	}
	re := regexp.MustCompile(`[^a-zA-Z0-9_.\-/:]+`)
	sanitized := re.ReplaceAllString(name, "")
	if sanitized == "" || len(sanitized) > 256 || strings.Contains(sanitized, "..") {
		return ""
	}
	return sanitized
}

func hasDockerPermission(session map[string]interface{}) bool {
	if role, _ := session["role"].(string); role == "admin" {
		return true
	}
	username, _ := session["username"].(string)
	conf := getDockerConfigGo()
	perms, _ := conf["permissions"].([]interface{})
	for _, p := range perms {
		if ps, _ := p.(string); ps == username {
			return true
		}
	}
	return false
}

// ═══════════════════════════════════
// Known app metadata
// ═══════════════════════════════════

var knownDockerApps = map[string][3]string{
	"jellyfin":       {"Jellyfin", "🎞️", "#00A4DC"},
	"plex":           {"Plex", "🎬", "#E5A00D"},
	"nextcloud":      {"Nextcloud", "☁️", "#0082C9"},
	"immich":         {"Immich", "📸", "#4250AF"},
	"syncthing":      {"Syncthing", "🔄", "#0891B2"},
	"transmission":   {"Transmission", "⬇️", "#B50D0D"},
	"qbittorrent":    {"qBittorrent", "📥", "#2F67BA"},
	"homeassistant":  {"Home Assistant", "🏠", "#18BCF2"},
	"home-assistant": {"Home Assistant", "🏠", "#18BCF2"},
	"vaultwarden":    {"Vaultwarden", "🔐", "#175DDC"},
	"portainer":      {"Portainer", "📊", "#13BEF9"},
	"gitea":          {"Gitea", "🦊", "#609926"},
	"pihole":         {"Pi-hole", "🛡️", "#96060C"},
	"adguard":        {"AdGuard Home", "🛡️", "#68BC71"},
	"nginx":          {"Nginx", "🌐", "#009639"},
	"mariadb":        {"MariaDB", "🗄️", "#003545"},
	"postgres":       {"PostgreSQL", "🐘", "#336791"},
	"redis":          {"Redis", "🔴", "#DC382D"},
	"grafana":        {"Grafana", "📈", "#F46800"},
	"sonarr":         {"Sonarr", "📺", "#35C5F4"},
	"radarr":         {"Radarr", "🎥", "#FFC230"},
}

func getAppMeta(image, containerName string) (string, string, string) {
	lower := strings.ToLower(containerName)
	for key, meta := range knownDockerApps {
		if strings.Contains(lower, key) {
			return meta[0], meta[1], meta[2]
		}
	}
	lower = strings.ToLower(image)
	for key, meta := range knownDockerApps {
		if strings.Contains(lower, key) {
			return meta[0], meta[1], meta[2]
		}
	}
	return containerName, "📦", "#78706A"
}

// ═══════════════════════════════════
// Docker HTTP handlers
// ═══════════════════════════════════

func handleDockerRoutes(w http.ResponseWriter, r *http.Request) {
	urlPath := r.URL.Path
	method := r.Method

	switch {
	case urlPath == "/api/docker/status" && method == "GET":
		dockerStatus(w, r)
	case urlPath == "/api/docker/permissions" && method == "GET":
		dockerPermissionsGet(w, r)
	case urlPath == "/api/docker/permissions" && method == "PUT":
		dockerPermissionsSet(w, r)
	case urlPath == "/api/docker/app-permissions" && method == "GET":
		dockerAppPermissions(w, r)
	case urlPath == "/api/docker/containers" && method == "GET":
		dockerContainersList(w, r)
	case urlPath == "/api/docker/installed-apps" && method == "GET":
		dockerInstalledApps(w, r)
	case urlPath == "/api/docker/install" && method == "POST":
		dockerInstall(w, r)
	case urlPath == "/api/docker/uninstall" && method == "POST":
		dockerUninstall(w, r)
	case urlPath == "/api/docker/uninstall" && method == "DELETE":
		dockerUninstallConfig(w, r)
	case urlPath == "/api/docker/container" && method == "POST":
		dockerContainerCreate(w, r)
	case urlPath == "/api/docker/stack" && method == "POST":
		dockerStackDeploy(w, r)
	case urlPath == "/api/permissions/matrix" && method == "GET":
		permissionsMatrix(w, r)
	case urlPath == "/api/firewall/add-rule" && method == "POST":
		firewallAddRule(w, r)
	case urlPath == "/api/firewall/remove-rule" && method == "POST":
		firewallRemoveRule(w, r)
	case urlPath == "/api/firewall/toggle" && method == "POST":
		firewallToggle(w, r)
	case urlPath == "/api/hardware/install-driver" && method == "POST":
		hardwareInstallDriver(w, r)
	case strings.HasPrefix(urlPath, "/api/hardware/driver-log/") && method == "GET":
		hardwareDriverLog(w, r)
	default:
		// Regex routes
		if handleDockerRegexRoutes(w, r) {
			return
		}
		jsonError(w, 404, "Not found")
	}
}

func handleDockerRegexRoutes(w http.ResponseWriter, r *http.Request) bool {
	urlPath := r.URL.Path
	method := r.Method

	// PUT /api/docker/app-permissions/:appId
	reAppPerm := regexp.MustCompile(`^/api/docker/app-permissions/([a-zA-Z0-9_-]+)$`)
	if m := reAppPerm.FindStringSubmatch(urlPath); m != nil && method == "PUT" {
		dockerAppPermUpdate(w, r, m[1])
		return true
	}

	// GET /api/docker/app-access/:appId
	reAppAccess := regexp.MustCompile(`^/api/docker/app-access/([a-zA-Z0-9_-]+)$`)
	if m := reAppAccess.FindStringSubmatch(urlPath); m != nil && method == "GET" {
		dockerAppAccess(w, r, m[1])
		return true
	}

	// GET /api/docker/app-folders/:appId
	reAppFolders := regexp.MustCompile(`^/api/docker/app-folders/([a-zA-Z0-9_-]+)$`)
	if m := reAppFolders.FindStringSubmatch(urlPath); m != nil && method == "GET" {
		dockerAppFolders(w, r, m[1])
		return true
	}

	// POST /api/docker/container/:id/:action
	reAction := regexp.MustCompile(`^/api/docker/container/([a-zA-Z0-9_.-]+)/(start|stop|restart)$`)
	if m := reAction.FindStringSubmatch(urlPath); m != nil && method == "POST" {
		dockerContainerAction(w, r, m[1], m[2])
		return true
	}

	// DELETE /api/docker/container/:id
	reDelete := regexp.MustCompile(`^/api/docker/container/([a-zA-Z0-9_.-]+)$`)
	if m := reDelete.FindStringSubmatch(urlPath); m != nil && method == "DELETE" {
		dockerContainerDelete(w, r, m[1])
		return true
	}

	// GET /api/docker/container/:id/mounts
	reMounts := regexp.MustCompile(`^/api/docker/container/([a-zA-Z0-9_-]+)/mounts$`)
	if m := reMounts.FindStringSubmatch(urlPath); m != nil && method == "GET" {
		dockerContainerMounts(w, r, m[1])
		return true
	}

	// POST /api/docker/container/:id/rebuild
	reRebuild := regexp.MustCompile(`^/api/docker/container/([a-zA-Z0-9_-]+)/rebuild$`)
	if m := reRebuild.FindStringSubmatch(urlPath); m != nil && method == "POST" {
		dockerContainerRebuild(w, r, m[1])
		return true
	}

	// DELETE /api/docker/stack/:id
	reStack := regexp.MustCompile(`^/api/docker/stack/([a-zA-Z0-9_-]+)$`)
	if m := reStack.FindStringSubmatch(urlPath); m != nil && method == "DELETE" {
		dockerStackDelete(w, r, m[1])
		return true
	}

	// GET /api/docker/pull/:image
	if strings.HasPrefix(urlPath, "/api/docker/pull/") && method == "GET" {
		dockerPull(w, r)
		return true
	}

	return false
}

// ═══════════════════════════════════
// Handlers
// ═══════════════════════════════════

func dockerStatus(w http.ResponseWriter, r *http.Request) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	// Allow admin or users with Docker permission
	role, _ := session["role"].(string)
	hasPerm := hasDockerPermission(session)
	if role != "admin" && !hasPerm {
		jsonError(w, 403, "No permission")
		return
	}
	conf := getDockerConfigGo()
	dockerRunning := isDockerInstalledGo()

	if dockerRunning && conf["installed"] != true {
		conf["installed"] = true
		conf["installedAt"] = time.Now().UTC().Format(time.RFC3339)
		saveDockerConfigGo(conf)
	}

	var containers []map[string]interface{}
	if dockerRunning && hasPerm {
		containers = getRealContainersGo()
	} else {
		containers = []map[string]interface{}{}
	}

	jsonOk(w, map[string]interface{}{
		"installed":     conf["installed"],
		"path":          conf["path"],
		"hasPermission": hasPerm,
		"installedAt":   conf["installedAt"],
		"containers":    containers,
		"dockerRunning": dockerRunning,
	})
}

func dockerPermissionsGet(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	conf := getDockerConfigGo()
	users, _ := dbUsersList()
	perms, _ := conf["permissions"].([]interface{})

	var userList []map[string]interface{}
	for _, u := range users {
		username, _ := u["username"].(string)
		role, _ := u["role"].(string)
		hasAccess := role == "admin"
		if !hasAccess {
			for _, p := range perms {
				if ps, _ := p.(string); ps == username {
					hasAccess = true
					break
				}
			}
		}
		userList = append(userList, map[string]interface{}{
			"username": username, "role": role, "hasAccess": hasAccess,
		})
	}
	jsonOk(w, map[string]interface{}{"users": userList, "permissions": perms})
}

func dockerPermissionsSet(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	body, _ := readBody(r)
	permsRaw, ok := body["permissions"].([]interface{})
	if !ok {
		jsonError(w, 400, "Invalid permissions format")
		return
	}
	conf := getDockerConfigGo()
	conf["permissions"] = permsRaw
	saveDockerConfigGo(conf)
	jsonOk(w, map[string]interface{}{"ok": true, "permissions": permsRaw})
}

func dockerAppPermissions(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	conf := getDockerConfigGo()
	users, _ := dbUsersList()
	shares, _ := dbSharesList()

	var installedApps []map[string]interface{}
	containers := getRealContainersGo()
	for _, c := range containers {
		installedApps = append(installedApps, map[string]interface{}{"id": c["name"], "name": c["name"], "type": "container", "image": c["image"]})
	}

	// Check stacks
	dockerPath, _ := conf["path"].(string)
	if dockerPath == "" {
		if dp, err := getDockerPath(); err == nil {
			dockerPath = dp
		}
	}
	stacksPath := filepath.Join(dockerPath, "stacks")
	if entries, err := os.ReadDir(stacksPath); err == nil {
		for _, e := range entries {
			if _, err := os.Stat(filepath.Join(stacksPath, e.Name(), "docker-compose.yml")); err == nil {
				installedApps = append(installedApps, map[string]interface{}{"id": e.Name(), "name": e.Name(), "type": "stack"})
			}
		}
	}

	var userList []map[string]interface{}
	for _, u := range users {
		userList = append(userList, map[string]interface{}{"username": u["username"], "role": u["role"]})
	}

	var shareList []map[string]interface{}
	for _, s := range shares {
		shareList = append(shareList, map[string]interface{}{"name": s["name"], "displayName": s["displayName"], "permissions": s["permissions"]})
	}

	jsonOk(w, map[string]interface{}{
		"users":             userList,
		"apps":              installedApps,
		"shares":            shareList,
		"appPermissions":    conf["appPermissions"],
		"dockerPermissions": conf["permissions"],
	})
}

func dockerAppPermUpdate(w http.ResponseWriter, r *http.Request, appId string) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	body, _ := readBody(r)
	allowedUsers, ok := body["users"].([]interface{})
	if !ok {
		jsonError(w, 400, "Invalid format")
		return
	}
	conf := getDockerConfigGo()
	appPerms, _ := conf["appPermissions"].(map[string]interface{})
	if appPerms == nil {
		appPerms = map[string]interface{}{}
	}
	appPerms[appId] = allowedUsers
	conf["appPermissions"] = appPerms
	saveDockerConfigGo(conf)
	jsonOk(w, map[string]interface{}{"ok": true, "appId": appId, "users": allowedUsers})
}

func dockerAppAccess(w http.ResponseWriter, r *http.Request, appId string) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	if role, _ := session["role"].(string); role == "admin" {
		jsonOk(w, map[string]interface{}{"hasAccess": true, "appId": appId})
		return
	}
	conf := getDockerConfigGo()
	appPerms, _ := conf["appPermissions"].(map[string]interface{})
	users, _ := appPerms[appId].([]interface{})
	username, _ := session["username"].(string)
	hasAccess := false
	for _, u := range users {
		if us, _ := u.(string); us == username {
			hasAccess = true
			break
		}
	}
	jsonOk(w, map[string]interface{}{"hasAccess": hasAccess, "appId": appId})
}

func dockerAppFolders(w http.ResponseWriter, r *http.Request, appId string) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	shares, _ := dbSharesList()
	var folders []map[string]interface{}
	for _, s := range shares {
		appPerms, _ := s["appPermissions"].([]map[string]interface{})
		for _, ap := range appPerms {
			if aid, _ := ap["appId"].(string); aid == appId {
				folders = append(folders, map[string]interface{}{"name": s["name"], "displayName": s["displayName"], "path": s["path"]})
				break
			}
		}
	}
	if folders == nil {
		folders = []map[string]interface{}{}
	}
	jsonOk(w, map[string]interface{}{"appId": appId, "folders": folders})
}

func dockerContainersList(w http.ResponseWriter, r *http.Request) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	if !isDockerInstalledGo() {
		jsonOk(w, map[string]interface{}{"installed": false, "containers": []interface{}{}})
		return
	}
	if !hasDockerPermission(session) {
		jsonError(w, 403, "No permission to manage Docker")
		return
	}
	jsonOk(w, map[string]interface{}{"installed": true, "containers": getRealContainersGo()})
}

func dockerInstalledApps(w http.ResponseWriter, r *http.Request) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	if !isDockerInstalledGo() {
		jsonOk(w, map[string]interface{}{"apps": []interface{}{}})
		return
	}

	registeredApps := getInstalledApps()

	// Get running containers
	out, _ := run(`docker ps --format '{{.Names}}|{{.Image}}|{{.Ports}}|{{.Status}}' 2>/dev/null`)
	runningContainers := map[string]map[string]interface{}{}
	if out != "" {
		for _, line := range strings.Split(out, "\n") {
			parts := strings.SplitN(line, "|", 4)
			if len(parts) < 4 {
				continue
			}
			var port interface{}
			if parts[2] != "" {
				re := regexp.MustCompile(`0\.0\.0\.0:(\d+)`)
				if m := re.FindStringSubmatch(parts[2]); m != nil {
					port = parseIntDefault(m[1], 0)
				}
			}
			status := "stopped"
			if strings.Contains(parts[3], "Up") {
				status = "running"
			}
			runningContainers[parts[0]] = map[string]interface{}{"image": parts[1], "port": port, "status": status}
		}
	}

	var apps []interface{}
	addedIds := map[string]bool{}

	for _, reg := range registeredApps {
		regId, _ := reg["id"].(string)
		regType, _ := reg["type"].(string)
		isStack := regType == "stack"

		containerStatus := "unknown"
		if isStack {
			for _, suffix := range []string{"_server", "-server", "_app", "-app", ""} {
				if c, ok := runningContainers[regId+suffix]; ok {
					containerStatus, _ = c["status"].(string)
					break
				}
			}
			if containerStatus == "unknown" {
				for name, c := range runningContainers {
					if strings.HasPrefix(name, regId+"_") || strings.HasPrefix(name, regId+"-") {
						containerStatus, _ = c["status"].(string)
						break
					}
				}
			}
		} else {
			if c, ok := runningContainers[regId]; ok {
				containerStatus, _ = c["status"].(string)
			}
		}

		apps = append(apps, map[string]interface{}{
			"id": regId, "name": reg["name"], "icon": reg["icon"],
			"color": reg["color"], "port": reg["port"], "image": reg["image"],
			"status": containerStatus, "category": "installed", "isStack": isStack,
			"external": reg["external"],
		})
		addedIds[regId] = true
	}

	// Add unregistered containers with ports
	for name, c := range runningContainers {
		if addedIds[name] || c["port"] == nil {
			continue
		}
		// Skip stack sub-containers
		skip := false
		for _, sub := range []string{"_redis", "_postgres", "_ml"} {
			if strings.Contains(name, sub) {
				skip = true
				break
			}
		}
		if skip {
			continue
		}
		for id := range addedIds {
			if strings.HasPrefix(name, id+"_") || strings.HasPrefix(name, id+"-") {
				skip = true
				break
			}
		}
		if skip {
			continue
		}

		dispName, icon, color := getAppMeta(c["image"].(string), name)
		apps = append(apps, map[string]interface{}{
			"id": name, "name": dispName, "icon": icon, "color": color,
			"port": c["port"], "image": c["image"], "status": c["status"], "category": "installed",
		})
	}

	if apps == nil {
		apps = []interface{}{}
	}
	jsonOk(w, map[string]interface{}{"apps": apps})
}

func dockerInstall(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	body, _ := readBody(r)

	// ── 1. Find the target pool ──
	storageConf := getStorageConfigFull()
	confPools, _ := storageConf["pools"].([]interface{})
	if len(confPools) == 0 {
		jsonError(w, 400, "No storage pools available. Create a pool in Storage Manager first.")
		return
	}

	poolName := bodyStr(body, "pool")
	primaryPool, _ := storageConf["primaryPool"].(string)
	var targetPool map[string]interface{}
	for _, p := range confPools {
		pm, _ := p.(map[string]interface{})
		name, _ := pm["name"].(string)
		if poolName != "" && name == poolName {
			targetPool = pm
			break
		}
		if name == primaryPool {
			targetPool = pm
		}
	}
	if targetPool == nil {
		pm, _ := confPools[0].(map[string]interface{})
		targetPool = pm
	}

	mountPoint, _ := targetPool["mountPoint"].(string)
	if mountPoint == "" {
		jsonError(w, 400, "Pool has no mount point configured")
		return
	}

	// ── 2. Verify pool is REALLY mounted ──
	mountSrc, _ := run(fmt.Sprintf("findmnt -n -o SOURCE %s 2>/dev/null", mountPoint))
	rootSrc, _ := run("findmnt -n -o SOURCE / 2>/dev/null")
	if strings.TrimSpace(mountSrc) == "" || strings.TrimSpace(mountSrc) == strings.TrimSpace(rootSrc) {
		jsonError(w, 400, "Storage pool is not mounted. Check Storage Manager.")
		return
	}

	dockerPath := filepath.Join(mountPoint, "docker")
	dockerDataPath := filepath.Join(dockerPath, "data")

	// ── 3. Create ALL directories on the pool FIRST ──
	for _, dir := range []string{"data", "containers", "stacks", "volumes"} {
		if err := os.MkdirAll(filepath.Join(dockerPath, dir), 0755); err != nil {
			jsonError(w, 500, fmt.Sprintf("Failed to create directory: %v", err))
			return
		}
	}
	log.Printf("Docker directories created at %s", dockerPath)

	// ── 4. Create daemon.json BEFORE Docker ever starts ──
	os.MkdirAll("/etc/docker", 0755)
	daemonConf := map[string]interface{}{"data-root": dockerDataPath}
	daemonData, _ := json.MarshalIndent(daemonConf, "", "  ")
	if err := os.WriteFile("/etc/docker/daemon.json", daemonData, 0644); err != nil {
		jsonError(w, 500, fmt.Sprintf("Failed to write daemon.json: %v", err))
		return
	}
	log.Printf("Docker daemon.json → data-root=%s", dockerDataPath)

	// ── 5. Install Docker if not present ──
	dockerAvailable := isDockerInstalledGo()
	if !dockerAvailable {
		log.Println("Docker not found, installing...")
		run("systemctl stop docker.socket docker containerd 2>/dev/null || true")

		ctx, cancel := context.WithTimeout(context.Background(), 300*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "bash", "-c", "curl -fsSL https://get.docker.com | sh")
		cmd.Env = append(os.Environ(), "DEBIAN_FRONTEND=noninteractive")
		installOut, err := cmd.CombinedOutput()
		if err != nil {
			log.Printf("Docker install failed: %v\nOutput: %s", err, string(installOut))
			jsonError(w, 500, "Docker installation failed. Check system logs.")
			return
		}
		log.Println("Docker engine installed")
		run("usermod -aG docker nimbus 2>/dev/null || true")
		run("usermod -aG docker nimos 2>/dev/null || true")
		dockerAvailable = true
		// Stop whatever the installer started — we restart with our config
		run("systemctl stop docker.socket docker containerd 2>/dev/null || true")
	} else {
		// Docker exists — stop to reconfigure
		run("systemctl stop docker.socket docker containerd 2>/dev/null || true")
	}

	// ── 6. Kill /var/lib/docker — pool only ──
	run("rm -rf /var/lib/docker 2>/dev/null || true")

	// ── 7. Start Docker with correct config ──
	if dockerAvailable {
		run("systemctl enable docker.service docker.socket 2>/dev/null || true")
		run("systemctl start docker 2>/dev/null || true")
		time.Sleep(2 * time.Second)

		// Verify
		rootDir, _ := run("docker info --format '{{.DockerRootDir}}' 2>/dev/null")
		rootDir = strings.TrimSpace(rootDir)
		if rootDir != "" && rootDir != dockerDataPath {
			log.Printf("WARNING: Docker Root Dir=%s expected=%s", rootDir, dockerDataPath)
		} else {
			log.Printf("Docker Root Dir confirmed: %s", dockerDataPath)
		}

		// ── 8. Permissions for FileManager ──
		run(fmt.Sprintf(`chmod 755 "%s"`, dockerPath))
		run(fmt.Sprintf(`chmod 755 "%s"`, filepath.Join(dockerPath, "containers")))
		run(fmt.Sprintf(`chmod 755 "%s"`, filepath.Join(dockerPath, "stacks")))
		run(fmt.Sprintf(`chmod 755 "%s"`, filepath.Join(dockerPath, "volumes")))

		// ── 9. Create docker-apps share ──
		dockerSharePath := filepath.Join(dockerPath, "containers")
		existingShare, _ := dbSharesGet("docker-apps")
		if existingShare == nil {
			pName := ""
			if targetPool != nil {
				pName, _ = targetPool["name"].(string)
			}
			shareGroup := "nimos-share-docker-apps"
			run(fmt.Sprintf("groupadd -f %s", shareGroup))
			run(fmt.Sprintf(`chown root:%s "%s"`, shareGroup, dockerSharePath))
			run(fmt.Sprintf(`chmod 2775 "%s"`, dockerSharePath))
			run(fmt.Sprintf(`setfacl -d -m g:%s:rwx "%s" 2>/dev/null || true`, shareGroup, dockerSharePath))
			run(fmt.Sprintf("usermod -aG %s nimbus 2>/dev/null || true", shareGroup))
			run(fmt.Sprintf("usermod -aG %s nimos 2>/dev/null || true", shareGroup))
			dbSharesCreate("docker-apps", "Docker Apps", "Application data for Docker containers", dockerSharePath, pName, pName, "system")
			if users, err := dbUsersList(); err == nil {
				for _, u := range users {
					role, _ := u["role"].(string)
					username, _ := u["username"].(string)
					if role == "admin" && username != "" {
						dbShareSetPermission("docker-apps", username, "rw")
						run(fmt.Sprintf("usermod -aG docker %s 2>/dev/null || true", username))
						run(fmt.Sprintf("usermod -aG %s %s 2>/dev/null || true", shareGroup, username))
					}
				}
			}
			log.Println("Docker share 'docker-apps' created at", dockerSharePath)
		}
	}

	// ── 10. Save config ──
	conf := getDockerConfigGo()
	conf["installed"] = true
	conf["dockerAvailable"] = dockerAvailable
	conf["path"] = dockerPath
	if perms, ok := body["permissions"].([]interface{}); ok {
		conf["permissions"] = perms
	}
	conf["installedAt"] = time.Now().UTC().Format(time.RFC3339)
	saveDockerConfigGo(conf)

	jsonOk(w, map[string]interface{}{"ok": true, "path": dockerPath, "dockerAvailable": dockerAvailable})
}


func dockerUninstall(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	run("docker stop $(docker ps -aq) 2>/dev/null || true")
	run("docker rm $(docker ps -aq) 2>/dev/null || true")
	run("systemctl stop docker 2>/dev/null || true")
	run("systemctl disable docker 2>/dev/null || true")
	run("apt-get purge -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin 2>/dev/null || true")
	run("rm -f /etc/docker/daemon.json 2>/dev/null || true")

	conf := getDockerConfigGo()
	conf["installed"] = false
	conf["dockerAvailable"] = false
	conf["path"] = nil
	conf["permissions"] = []interface{}{}
	conf["installedAt"] = nil
	saveDockerConfigGo(conf)
	jsonOk(w, map[string]interface{}{"ok": true})
}

func dockerUninstallConfig(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	conf := getDockerConfigGo()
	conf["installed"] = false
	conf["path"] = nil
	conf["permissions"] = []interface{}{}
	conf["installedAt"] = nil
	saveDockerConfigGo(conf)
	jsonOk(w, map[string]interface{}{"ok": true})
}

func dockerContainerCreate(w http.ResponseWriter, r *http.Request) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	if !hasDockerPermission(session) {
		jsonError(w, 403, "No permission to manage Docker")
		return
	}
	if !isDockerInstalledGo() {
		jsonError(w, 400, "Docker not installed")
		return
	}

	body, _ := readBody(r)
	rawId := bodyStr(body, "id")
	rawName := bodyStr(body, "name")
	rawImage := bodyStr(body, "image")
	id := sanitizeDockerNameGo(rawId)
	name := sanitizeDockerNameGo(rawName)
	image := sanitizeDockerNameGo(rawImage)
	if id == "" || name == "" || image == "" {
		jsonError(w, 400, "Missing container info")
		return
	}
	// Reject if sanitization changed the input — means malicious chars were present
	if id != rawId || name != rawName || image != rawImage {
		jsonError(w, 400, "Container name, id, or image contains invalid characters")
		return
	}

	conf := getDockerConfigGo()
	dockerPath, _ := conf["path"].(string)
	if dockerPath == "" {
		dp, err := getDockerPath()
		if err != nil {
			jsonError(w, 400, "Docker not configured — install Docker from App Store first")
			return
		}
		dockerPath = dp
	}

	cmd := fmt.Sprintf("docker run -d --name %s --restart unless-stopped", id)

	// Ports — validate strictly
	if ports, ok := body["ports"].(map[string]interface{}); ok {
		portRegex := regexp.MustCompile(`^\d{1,5}$`)
		for host, container := range ports {
			containerStr := fmt.Sprintf("%v", container)
			if !portRegex.MatchString(host) || !portRegex.MatchString(containerStr) {
				jsonError(w, 400, "Invalid port mapping (must be numeric)")
				return
			}
			cmd += fmt.Sprintf(" -p %s:%s", host, containerStr)
		}
	}

	// Config volume
	containerDataPath := filepath.Join(dockerPath, "containers", id)
	os.MkdirAll(containerDataPath, 0775)
	// Set group ownership for FileManager access
	run(fmt.Sprintf(`chown root:nimos-share-docker-apps "%s" 2>/dev/null || true`, containerDataPath))
	run(fmt.Sprintf(`chmod 2775 "%s" 2>/dev/null || true`, containerDataPath))
	cmd += fmt.Sprintf(" -v %s:/config", containerDataPath)

	// Shared folder mounts
	shares, _ := dbSharesList()
	var mountedShares []string
	for _, s := range shares {
		appPerms, _ := s["appPermissions"].([]map[string]interface{})
		for _, ap := range appPerms {
			if aid, _ := ap["appId"].(string); aid == id {
				sharePath, _ := s["path"].(string)
				shareName, _ := s["name"].(string)
				if sharePath != "" {
					cmd += fmt.Sprintf(` -v "%s":"/media/%s":ro`, sharePath, shareName)
					mountedShares = append(mountedShares, shareName)
				}
				break
			}
		}
	}

	// Env vars
	if env, ok := body["env"].(map[string]interface{}); ok {
		for key, val := range env {
			valStr := fmt.Sprintf("%v", val)
			if matched, _ := regexp.MatchString(`^[a-zA-Z_][a-zA-Z0-9_]*$`, key); matched {
				safeVal := regexp.MustCompile("[`$\\\\;\"'|&<>]").ReplaceAllString(valStr, "")
				cmd += fmt.Sprintf(` -e %s="%s"`, key, safeVal)
			}
		}
	}

	cmd += " " + image

	out, ok := run(cmd)
	if !ok {
		jsonError(w, 500, "Failed to create container")
		return
	}

	// Register app
	var appPort interface{}
	if ports, ok := body["ports"].(map[string]interface{}); ok {
		for p := range ports {
			appPort = parseIntDefault(p, 0)
			break
		}
	}
	apps := getInstalledApps()
	filtered := make([]map[string]interface{}, 0)
	for _, a := range apps {
		if aid, _ := a["id"].(string); aid != id {
			filtered = append(filtered, a)
		}
	}
	filtered = append(filtered, map[string]interface{}{
		"id": id, "name": name, "icon": bodyStr(body, "icon"), "port": appPort,
		"image": image, "type": "container", "color": bodyStr(body, "color"),
		"installedBy": session["username"],
	})
	saveInstalledApps(filtered)

	jsonOk(w, map[string]interface{}{
		"ok": true, "containerId": strings.TrimSpace(out),
		"container":    map[string]interface{}{"id": id, "name": name, "image": image, "status": "running"},
		"mountedShares": mountedShares,
	})
}

func dockerStackDeploy(w http.ResponseWriter, r *http.Request) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	if !hasDockerPermission(session) {
		jsonError(w, 403, "No permission to manage Docker")
		return
	}
	if !isDockerInstalledGo() {
		jsonError(w, 400, "Docker not installed")
		return
	}

	body, _ := readBody(r)
	id := sanitizeDockerNameGo(bodyStr(body, "id"))
	compose := bodyStr(body, "compose")
	if id == "" || compose == "" {
		jsonError(w, 400, "Missing stack info")
		return
	}

	conf := getDockerConfigGo()
	dockerPath, _ := conf["path"].(string)
	if dockerPath == "" {
		dp, err := getDockerPath()
		if err != nil {
			jsonError(w, 400, "Docker not configured — install Docker from App Store first")
			return
		}
		dockerPath = dp
	}
	stackPath := filepath.Join(dockerPath, "stacks", id)
	os.MkdirAll(stackPath, 0755)

	// Create container config directory (used by CONFIG_PATH in compose)
	containerPath := filepath.Join(dockerPath, "containers", id)
	os.MkdirAll(containerPath, 0775)
	// Set permissions so admin can read/write configs
	run(fmt.Sprintf(`chmod -R 775 "%s"`, containerPath))

	// Write compose file
	composePath := filepath.Join(stackPath, "docker-compose.yml")
	os.WriteFile(composePath, []byte(compose), 0644)

	// Write .env
	if env, ok := body["env"].(map[string]interface{}); ok {
		var lines []string
		for k, v := range env {
			lines = append(lines, fmt.Sprintf("%s=%v", k, v))
		}
		os.WriteFile(filepath.Join(stackPath, ".env"), []byte(strings.Join(lines, "\n")), 0644)
	}

	// Deploy
	cmd := exec.Command("docker", "compose", "-f", composePath, "up", "-d")
	cmd.Dir = stackPath
	if out, err := cmd.CombinedOutput(); err != nil {
		jsonError(w, 500, fmt.Sprintf("Failed to deploy stack: %s", string(out)))
		return
	}

	// Fix permissions on container config dir after deploy
	// Set group to nimos-share-docker-apps so FileManager can browse
	run(fmt.Sprintf(`chown -R root:nimos-share-docker-apps "%s" 2>/dev/null || true`, containerPath))
	run(fmt.Sprintf(`chmod -R 2775 "%s" 2>/dev/null || true`, containerPath))
	run(fmt.Sprintf(`setfacl -R -d -m g:nimos-share-docker-apps:rwx "%s" 2>/dev/null || true`, containerPath))
	run(fmt.Sprintf(`chmod -R 775 "%s" 2>/dev/null || true`, stackPath))

	// Register
	apps := getInstalledApps()
	filtered := make([]map[string]interface{}, 0)
	for _, a := range apps {
		if aid, _ := a["id"].(string); aid != id {
			filtered = append(filtered, a)
		}
	}
	filtered = append(filtered, map[string]interface{}{
		"id": id, "name": bodyStr(body, "name"), "icon": bodyStr(body, "icon"),
		"port": body["port"], "image": "stack", "type": "stack",
		"color": bodyStr(body, "color"), "external": body["external"],
		"installedBy": session["username"],
	})
	saveInstalledApps(filtered)

	jsonOk(w, map[string]interface{}{"ok": true, "stack": id, "path": stackPath})
}

func dockerContainerAction(w http.ResponseWriter, r *http.Request, id, action string) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	if !hasDockerPermission(session) {
		jsonError(w, 403, "No permission to manage Docker")
		return
	}
	safeId := sanitizeDockerNameGo(id)
	if safeId == "" {
		jsonError(w, 400, "Invalid container ID")
		return
	}
	if _, ok := run(fmt.Sprintf("docker %s %s 2>&1", action, safeId)); !ok {
		jsonError(w, 500, fmt.Sprintf("Failed to %s container", action))
		return
	}
	jsonOk(w, map[string]interface{}{"ok": true, "action": action, "containerId": safeId})
}

func dockerContainerDelete(w http.ResponseWriter, r *http.Request, id string) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	if !hasDockerPermission(session) {
		jsonError(w, 403, "No permission to manage Docker")
		return
	}
	safeId := sanitizeDockerNameGo(id)
	if safeId == "" {
		jsonError(w, 400, "Invalid container ID")
		return
	}
	// Unregister immediately
	apps := getInstalledApps()
	filtered := make([]map[string]interface{}, 0)
	for _, a := range apps {
		if aid, _ := a["id"].(string); aid != safeId {
			filtered = append(filtered, a)
		}
	}
	saveInstalledApps(filtered)

	// Remove in background
	go func() {
		run(fmt.Sprintf("docker stop %s 2>/dev/null && docker rm %s 2>/dev/null || docker rm -f %s 2>/dev/null", safeId, safeId, safeId))
	}()
	jsonOk(w, map[string]interface{}{"ok": true, "containerId": safeId})
}

func dockerContainerMounts(w http.ResponseWriter, r *http.Request, id string) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	if !hasDockerPermission(session) {
		jsonError(w, 403, "No permission")
		return
	}
	safeId := sanitizeDockerNameGo(id)
	if safeId == "" {
		jsonError(w, 400, "Invalid container ID")
		return
	}
	out, ok := run(fmt.Sprintf(`docker inspect %s --format '{{range .Mounts}}{{.Source}}|{{.Destination}}|{{.Mode}}{{println}}{{end}}' 2>/dev/null`, safeId))
	if !ok {
		jsonError(w, 500, "Failed to get mounts")
		return
	}
	var mounts []map[string]interface{}
	for _, line := range strings.Split(strings.TrimSpace(out), "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 3)
		if len(parts) >= 2 {
			mode := "rw"
			if len(parts) >= 3 {
				mode = parts[2]
			}
			mounts = append(mounts, map[string]interface{}{"source": parts[0], "destination": parts[1], "mode": mode})
		}
	}
	if mounts == nil {
		mounts = []map[string]interface{}{}
	}
	jsonOk(w, map[string]interface{}{"containerId": safeId, "mounts": mounts})
}

func dockerContainerRebuild(w http.ResponseWriter, r *http.Request, id string) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	if !hasDockerPermission(session) {
		jsonError(w, 403, "No permission")
		return
	}
	safeId := sanitizeDockerNameGo(id)
	if safeId == "" {
		jsonError(w, 400, "Invalid container ID")
		return
	}
	// Get current image
	imgOut, ok := run(fmt.Sprintf("docker inspect %s --format '{{.Config.Image}}' 2>/dev/null", safeId))
	if !ok {
		jsonError(w, 500, "Failed to inspect container")
		return
	}
	image := strings.TrimSpace(imgOut)
	// Validate the image name from docker inspect before reusing
	safeImage := sanitizeDockerNameGo(image)
	if safeImage == "" || safeImage != image {
		jsonError(w, 500, "Container has invalid image name")
		return
	}
	run(fmt.Sprintf("docker stop %s 2>/dev/null", safeId))
	run(fmt.Sprintf("docker rm %s 2>/dev/null", safeId))
	run(fmt.Sprintf("docker run -d --name %s --restart unless-stopped %s", safeId, safeImage))
	jsonOk(w, map[string]interface{}{"ok": true, "containerId": safeId})
}

func dockerStackDelete(w http.ResponseWriter, r *http.Request, id string) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	if !hasDockerPermission(session) {
		jsonError(w, 403, "No permission")
		return
	}
	safeId := sanitizeDockerNameGo(id)
	if safeId == "" {
		jsonError(w, 400, "Invalid stack ID")
		return
	}
	// Unregister immediately
	apps := getInstalledApps()
	filtered := make([]map[string]interface{}, 0)
	for _, a := range apps {
		if aid, _ := a["id"].(string); aid != safeId {
			filtered = append(filtered, a)
		}
	}
	saveInstalledApps(filtered)

	conf := getDockerConfigGo()
	dockerPath, _ := conf["path"].(string)
	if dockerPath == "" {
		if dp, err := getDockerPath(); err == nil {
			dockerPath = dp
		} else {
			jsonOk(w, map[string]interface{}{"ok": true})
			return
		}
	}
	stackPath := filepath.Join(dockerPath, "stacks", safeId)
	composePath := filepath.Join(stackPath, "docker-compose.yml")

	// Cleanup in background
	go func() {
		if _, err := os.Stat(composePath); err == nil {
			cmd := exec.Command("docker", "compose", "-f", composePath, "down", "-v", "--remove-orphans")
			cmd.Dir = stackPath
			cmd.Run()
		}
		os.RemoveAll(stackPath)
		os.RemoveAll(filepath.Join(dockerPath, "containers", safeId))
	}()

	jsonOk(w, map[string]interface{}{"ok": true})
}

func dockerPull(w http.ResponseWriter, r *http.Request) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	if !hasDockerPermission(session) {
		jsonError(w, 403, "No permission")
		return
	}
	rawImage := strings.TrimPrefix(r.URL.Path, "/api/docker/pull/")
	decoded, _ := url.PathUnescape(rawImage)
	image := sanitizeDockerNameGo(decoded)
	if image == "" || image != decoded {
		jsonError(w, 400, "Invalid image name")
		return
	}
	if _, ok := run(fmt.Sprintf("docker pull %s 2>&1", image)); !ok {
		jsonError(w, 500, "Failed to pull image")
		return
	}
	jsonOk(w, map[string]interface{}{"ok": true, "image": image})
}

func permissionsMatrix(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	users, _ := dbUsersList()
	shares, _ := dbSharesList()
	conf := getDockerConfigGo()
	perms, _ := conf["permissions"].([]interface{})

	var userList []map[string]interface{}
	for _, u := range users {
		username, _ := u["username"].(string)
		role, _ := u["role"].(string)
		hasDock := role == "admin"
		for _, p := range perms {
			if ps, _ := p.(string); ps == username {
				hasDock = true
			}
		}
		userList = append(userList, map[string]interface{}{"username": username, "role": role, "dockerAccess": hasDock})
	}

	var shareList []map[string]interface{}
	for _, s := range shares {
		shareList = append(shareList, map[string]interface{}{
			"name": s["name"], "displayName": s["displayName"],
			"userPermissions": s["permissions"], "appPermissions": s["appPermissions"],
		})
	}

	jsonOk(w, map[string]interface{}{"users": userList, "shares": shareList, "dockerAdmins": perms})
}

// ═══════════════════════════════════
// Firewall
// ═══════════════════════════════════

func firewallAddRule(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	body, _ := readBody(r)
	port := fmt.Sprintf("%v", body["port"])
	protocol := bodyStr(body, "protocol")
	action := bodyStr(body, "action")
	source := bodyStr(body, "source")

	if port == "" || protocol == "" || action == "" {
		jsonError(w, 400, "port, protocol, and action required")
		return
	}

	// Strict validation — prevent command injection
	// Port: digits only, or digits:digits for ranges
	if matched, _ := regexp.MatchString(`^\d{1,5}(:\d{1,5})?$`, port); !matched {
		jsonError(w, 400, "Invalid port format (use number or range like 8000:8100)")
		return
	}
	// Protocol: whitelist only
	if protocol != "tcp" && protocol != "udp" && protocol != "both" {
		jsonError(w, 400, "Invalid protocol (must be tcp, udp, or both)")
		return
	}
	// Action: whitelist only
	if action != "allow" && action != "deny" && action != "reject" && action != "limit" {
		jsonError(w, 400, "Invalid action (must be allow, deny, reject, or limit)")
		return
	}
	// Source: must be a valid IP or CIDR, or empty/any
	if source != "" && source != "any" && source != "Any" {
		if matched, _ := regexp.MatchString(`^\d{1,3}\.\d{1,3}\.\d{1,3}\.\d{1,3}(/\d{1,2})?$`, source); !matched {
			jsonError(w, 400, "Invalid source (must be IP address or CIDR like 192.168.1.0/24)")
			return
		}
	}

	_, hasUfw := run("which ufw 2>/dev/null")
	if hasUfw {
		proto := ""
		if protocol != "both" {
			proto = "/" + protocol
		}
		src := ""
		if source != "" && source != "any" && source != "Any" {
			src = " from " + source
		}
		cmd := fmt.Sprintf("ufw %s %s%s%s 2>&1", action, port, proto, src)
		result, _ := run(cmd)
		jsonOk(w, map[string]interface{}{"ok": true, "command": cmd, "result": result})
	} else {
		jsonError(w, 400, "ufw not installed")
	}
}

func firewallRemoveRule(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	body, _ := readBody(r)
	ruleNum := fmt.Sprintf("%v", body["ruleNum"])
	if ruleNum == "" {
		jsonError(w, 400, "ruleNum required")
		return
	}
	// Strict validation — ruleNum must be a positive integer
	if matched, _ := regexp.MatchString(`^\d{1,5}$`, ruleNum); !matched {
		jsonError(w, 400, "Invalid rule number (must be a positive integer)")
		return
	}
	result, _ := run(fmt.Sprintf(`echo "y" | ufw delete %s 2>&1`, ruleNum))
	jsonOk(w, map[string]interface{}{"ok": true, "result": result})
}

func firewallToggle(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	body, _ := readBody(r)
	enable, _ := body["enable"].(bool)
	if enable {
		result, _ := run(`echo "y" | ufw enable 2>&1`)
		jsonOk(w, map[string]interface{}{"ok": true, "result": result})
	} else {
		result, _ := run("ufw disable 2>&1")
		jsonOk(w, map[string]interface{}{"ok": true, "result": result})
	}
}

// ═══════════════════════════════════
// Hardware driver install
// ═══════════════════════════════════

func hardwareInstallDriver(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	body, _ := readBody(r)
	pkg := bodyStr(body, "package")
	action := bodyStr(body, "action")
	if pkg == "" || action == "" {
		jsonError(w, 400, "package and action required")
		return
	}
	if matched, _ := regexp.MatchString(`^(nvidia-driver-\d+|nvidia-driver-\d+-server|nvidia-driver-\d+-open|xserver-xorg-video-\w+|mesa-\w+|linux-modules-nvidia-\S+)$`, pkg); !matched {
		jsonError(w, 400, "Invalid driver package name")
		return
	}
	if action != "install" && action != "remove" {
		jsonError(w, 400, "action must be install or remove")
		return
	}

	logFile := fmt.Sprintf("/tmp/nimbus-driver-%d.log", time.Now().UnixMilli())
	cmd := fmt.Sprintf("apt-get %s -y %s", action, pkg)
	go func() {
		out, _ := exec.Command("bash", "-c", cmd).CombinedOutput()
		os.WriteFile(logFile, out, 0644)
	}()
	jsonOk(w, map[string]interface{}{"ok": true, "message": fmt.Sprintf("%s %s started", action, pkg), "logFile": logFile})
}

func hardwareDriverLog(w http.ResponseWriter, r *http.Request) {
	session := requireAuth(w, r)
	if session == nil {
		return
	}
	logFile := "/tmp/" + filepath.Base(r.URL.Path)
	if !strings.HasPrefix(logFile, "/tmp/nimbus-driver-") {
		jsonError(w, 400, "Invalid log file")
		return
	}
	content, err := os.ReadFile(logFile)
	if err != nil {
		jsonOk(w, map[string]interface{}{"content": "Waiting...", "done": false, "success": false})
		return
	}
	s := string(content)
	done := strings.Contains(s, "SUCCESS:") || strings.Contains(s, "ERROR:")
	success := strings.Contains(s, "SUCCESS:")
	jsonOk(w, map[string]interface{}{"content": s, "done": done, "success": success})
}
