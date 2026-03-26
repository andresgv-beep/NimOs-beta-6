package main

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ═══════════════════════════════════
// Startup detection
// ═══════════════════════════════════

var (
	hasSmartctl bool
	hasSensors  bool
	hasDocker   bool
	hasNvidia   bool
	hasAmdDrm   bool
	hasZfs      bool
	systemArch  string
	systemRamGB int
)

func detectHardwareTools() {
	_, hasSmartctl = run("which smartctl 2>/dev/null")
	_, hasSensors = run("which sensors 2>/dev/null")
	_, hasDocker = run("which docker 2>/dev/null")
	_, hasNvidia = run("which nvidia-smi 2>/dev/null")
	hasAmdDrm = detectAmdDrm()

	// Storage backends
	if zpoolOut, ok := run("which zpool 2>/dev/null"); ok && zpoolOut != "" {
		// Verify ZFS module is loaded
		if _, modOk := run("lsmod 2>/dev/null | grep -q '^zfs '"); modOk {
			hasZfs = true
		} else {
			// Try loading it
			run("modprobe zfs 2>/dev/null || true")
			_, hasZfs = run("lsmod 2>/dev/null | grep -q '^zfs '")
		}
	}

	// Btrfs detection
	detectBtrfs()

	// System info
	archOut, _ := run("uname -m 2>/dev/null")
	systemArch = strings.TrimSpace(archOut)
	if memInfo, ok := run("awk '/MemTotal/{printf \"%d\", $2/1024/1024}' /proc/meminfo"); ok {
		systemRamGB = parseIntDefault(strings.TrimSpace(memInfo), 0)
	}

	if hasZfs {
		logMsg("ZFS available (arch=%s, ram=%dGB)", systemArch, systemRamGB)
	} else if hasBtrfs {
		logMsg("Btrfs available, ZFS not available (arch=%s, ram=%dGB)", systemArch, systemRamGB)
	} else {
		logMsg("WARNING: No supported storage backend (arch=%s, ram=%dGB) — install zfsutils-linux or btrfs-progs", systemArch, systemRamGB)
	}
}

func detectAmdDrm() bool {
	entries, err := os.ReadDir("/sys/class/drm")
	if err != nil {
		return false
	}
	for _, e := range entries {
		if matched, _ := regexp.MatchString(`^card\d$`, e.Name()); matched {
			if data := readFileStr(fmt.Sprintf("/sys/class/drm/%s/device/gpu_busy_percent", e.Name())); data != "" {
				return true
			}
		}
	}
	return false
}

// ═══════════════════════════════════
// Helpers
// ═══════════════════════════════════

func readFileStr(path string) string {
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func formatBytes(bytes int64) string {
	if bytes == 0 {
		return "0 B"
	}
	sizes := []string{"B", "KB", "MB", "GB", "TB"}
	i := int(math.Floor(math.Log(math.Abs(float64(bytes))) / math.Log(1024)))
	if i >= len(sizes) {
		i = len(sizes) - 1
	}
	return fmt.Sprintf("%.1f %s", float64(bytes)/math.Pow(1024, float64(i)), sizes[i])
}

func parseInt64(s string) int64 {
	n, _ := strconv.ParseInt(strings.TrimSpace(s), 10, 64)
	return n
}

func parseIntDefault(s string, def int) int {
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil {
		return def
	}
	return n
}

// ═══════════════════════════════════
// CPU
// ═══════════════════════════════════

var prevCpuIdle, prevCpuTotal int64

func getCpuUsage() map[string]interface{} {
	stat := readFileStr("/proc/stat")
	cpuCount := 0
	cpuModel := "Unknown"
	cpuInfo := readFileStr("/proc/cpuinfo")
	if cpuInfo != "" {
		for _, line := range strings.Split(cpuInfo, "\n") {
			if strings.HasPrefix(line, "processor") {
				cpuCount++
			}
			if strings.HasPrefix(line, "model name") {
				parts := strings.SplitN(line, ":", 2)
				if len(parts) == 2 {
					cpuModel = strings.TrimSpace(parts[1])
				}
			}
		}
	}
	if cpuCount == 0 {
		cpuCount = 1
	}

	percent := 0
	if stat != "" {
		line := strings.Split(stat, "\n")[0]
		fields := strings.Fields(line)
		if len(fields) >= 8 {
			var values []int64
			for _, f := range fields[1:] {
				values = append(values, parseInt64(f))
			}
			idle := values[3]
			if len(values) > 4 {
				idle += values[4] // iowait
			}
			total := int64(0)
			for _, v := range values {
				total += v
			}

			if prevCpuTotal > 0 {
				diffIdle := idle - prevCpuIdle
				diffTotal := total - prevCpuTotal
				if diffTotal > 0 {
					percent = int(math.Round(float64(diffTotal-diffIdle) / float64(diffTotal) * 100))
				}
			}
			prevCpuIdle = idle
			prevCpuTotal = total
		}
	}

	return map[string]interface{}{
		"percent": percent,
		"cores":   cpuCount,
		"model":   cpuModel,
	}
}

// ═══════════════════════════════════
// Memory
// ═══════════════════════════════════

func getMemory() map[string]interface{} {
	info := readFileStr("/proc/meminfo")
	if info == "" {
		return map[string]interface{}{"total": 0, "used": 0, "percent": 0}
	}

	parse := func(key string) int64 {
		re := regexp.MustCompile(key + `:\s+(\d+)`)
		m := re.FindStringSubmatch(info)
		if m == nil {
			return 0
		}
		return parseInt64(m[1]) * 1024 // kB to bytes
	}

	total := parse("MemTotal")
	available := parse("MemAvailable")
	used := total - available

	return map[string]interface{}{
		"total":   total,
		"used":    used,
		"totalGB": fmt.Sprintf("%.1f", float64(total)/1073741824),
		"usedGB":  fmt.Sprintf("%.1f", float64(used)/1073741824),
		"percent": func() int {
			if total > 0 {
				return int(math.Round(float64(used) / float64(total) * 100))
			}
			return 0
		}(),
	}
}

// ═══════════════════════════════════
// GPU
// ═══════════════════════════════════

func getGpu() []map[string]interface{} {
	var gpus []map[string]interface{}

	if hasNvidia {
		out, ok := run("nvidia-smi --query-gpu=index,name,utilization.gpu,temperature.gpu,memory.used,memory.total --format=csv,noheader,nounits 2>/dev/null")
		if ok && out != "" {
			for _, line := range strings.Split(out, "\n") {
				parts := strings.Split(line, ",")
				if len(parts) >= 6 {
					memUsed := parseIntDefault(strings.TrimSpace(parts[4]), 0)
					memTotal := parseIntDefault(strings.TrimSpace(parts[5]), 0)
					memPct := 0
					if memTotal > 0 {
						memPct = int(math.Round(float64(memUsed) / float64(memTotal) * 100))
					}
					gpus = append(gpus, map[string]interface{}{
						"index":       parseIntDefault(strings.TrimSpace(parts[0]), 0),
						"name":        strings.TrimSpace(parts[1]),
						"utilization": parseIntDefault(strings.TrimSpace(parts[2]), 0),
						"temperature": parseIntDefault(strings.TrimSpace(parts[3]), 0),
						"memUsed":     memUsed,
						"memTotal":    memTotal,
						"memPercent":  memPct,
						"driver":      "nvidia",
					})
				}
			}
		}
	}

	if hasAmdDrm {
		entries, _ := os.ReadDir("/sys/class/drm")
		for _, e := range entries {
			if matched, _ := regexp.MatchString(`^card\d$`, e.Name()); !matched {
				continue
			}
			busy := readFileStr(fmt.Sprintf("/sys/class/drm/%s/device/gpu_busy_percent", e.Name()))
			if busy == "" {
				continue
			}
			// Find temperature
			temp := 0
			hwmonDirs, _ := filepath.Glob(fmt.Sprintf("/sys/class/drm/%s/device/hwmon/hwmon*", e.Name()))
			for _, dir := range hwmonDirs {
				t := readFileStr(filepath.Join(dir, "temp1_input"))
				if t != "" {
					temp = parseIntDefault(t, 0) / 1000
					break
				}
			}
			gpus = append(gpus, map[string]interface{}{
				"index":       len(gpus),
				"name":        fmt.Sprintf("AMD GPU (%s)", e.Name()),
				"utilization": parseIntDefault(busy, 0),
				"temperature": temp,
				"memUsed":     0,
				"memTotal":    0,
				"memPercent":  0,
				"driver":      "amd",
			})
		}
	}

	if gpus == nil {
		gpus = []map[string]interface{}{}
	}
	return gpus
}

// ═══════════════════════════════════
// GPU Driver Info
// ═══════════════════════════════════

func getHardwareGpuInfo() map[string]interface{} {
	result := map[string]interface{}{
		"gpus":             []interface{}{},
		"currentDriver":    nil,
		"driverVersion":    nil,
		"availableDrivers": []interface{}{},
		"kernelModules":    []interface{}{},
	}

	// Detect GPUs via lspci
	var gpuList []interface{}
	lspci, ok := run(`lspci -nn 2>/dev/null | grep -iE "VGA|3D|Display"`)
	if ok && lspci != "" {
		for _, line := range strings.Split(lspci, "\n") {
			if line == "" {
				continue
			}
			lower := strings.ToLower(line)
			vendor := "unknown"
			if strings.Contains(lower, "nvidia") {
				vendor = "nvidia"
			} else if strings.Contains(lower, "amd") || strings.Contains(lower, "ati") {
				vendor = "amd"
			} else if strings.Contains(lower, "intel") {
				vendor = "intel"
			}
			pciId := ""
			if m := regexp.MustCompile(`\[([0-9a-f]{4}:[0-9a-f]{4})\]`).FindStringSubmatch(line); m != nil {
				pciId = m[1]
			}
			desc := line
			if idx := strings.Index(line, " "); idx > 0 {
				desc = strings.TrimSpace(line[idx:])
			}
			gpuList = append(gpuList, map[string]interface{}{
				"description": desc,
				"vendor":      vendor,
				"pciId":       pciId,
			})
		}
	}

	// ARM fallback
	if len(gpuList) == 0 {
		if vcgencmd, ok := run("vcgencmd get_mem gpu 2>/dev/null"); ok && vcgencmd != "" {
			model := readFileStr("/proc/device-tree/model")
			if model == "" {
				model = "Raspberry Pi"
			}
			gpuMem := strings.Replace(strings.Replace(vcgencmd, "gpu=", "", 1), "M", " MB", 1)
			gpuList = append(gpuList, map[string]interface{}{
				"description": fmt.Sprintf("%s — VideoCore (%s)", strings.TrimSpace(model), strings.TrimSpace(gpuMem)),
				"vendor":      "broadcom",
				"pciId":       nil,
			})
			result["currentDriver"] = "v3d"
		}
	}
	if gpuList == nil {
		gpuList = []interface{}{}
	}
	result["gpus"] = gpuList

	// NVIDIA driver
	if hasNvidia {
		if ver, ok := run("nvidia-smi --query-gpu=driver_version --format=csv,noheader,nounits 2>/dev/null"); ok && ver != "" {
			result["currentDriver"] = "nvidia"
			result["driverVersion"] = strings.TrimSpace(strings.Split(ver, "\n")[0])
		}
	}

	// AMD driver
	if out, ok := run("lsmod 2>/dev/null | grep amdgpu"); ok && out != "" {
		if result["currentDriver"] == nil {
			result["currentDriver"] = "amdgpu"
		}
	}

	// Intel driver
	if out, ok := run("lsmod 2>/dev/null | grep i915"); ok && out != "" {
		if result["currentDriver"] == nil {
			result["currentDriver"] = "i915"
		}
	}

	// Kernel modules
	var modules []interface{}
	if mods, ok := run(`lsmod 2>/dev/null | grep -iE "nvidia|amdgpu|radeon|i915|nouveau"`); ok && mods != "" {
		for _, line := range strings.Split(mods, "\n") {
			if line == "" {
				continue
			}
			parts := strings.Fields(line)
			entry := map[string]interface{}{"name": parts[0]}
			if len(parts) > 1 {
				entry["size"] = parts[1]
			}
			if len(parts) > 3 {
				entry["usedBy"] = parts[3]
			}
			modules = append(modules, entry)
		}
	}
	if modules == nil {
		modules = []interface{}{}
	}
	result["kernelModules"] = modules

	return result
}

// ═══════════════════════════════════
// Temperatures
// ═══════════════════════════════════

func getTemps(gpusCache []map[string]interface{}) map[string]interface{} {
	temps := map[string]interface{}{}

	// CPU via /sys/class/thermal
	entries, _ := os.ReadDir("/sys/class/thermal")
	for _, e := range entries {
		if !strings.HasPrefix(e.Name(), "thermal_zone") {
			continue
		}
		typeName := readFileStr(fmt.Sprintf("/sys/class/thermal/%s/type", e.Name()))
		tempStr := readFileStr(fmt.Sprintf("/sys/class/thermal/%s/temp", e.Name()))
		if typeName != "" && tempStr != "" {
			temps[typeName] = parseIntDefault(tempStr, 0) / 1000
		}
	}

	// lm-sensors fallback
	if len(temps) == 0 && hasSensors {
		if out, ok := run("sensors -u 2>/dev/null"); ok {
			re := regexp.MustCompile(`temp1_input:\s+([\d.]+)`)
			if m := re.FindStringSubmatch(out); m != nil {
				temps["cpu"] = int(math.Round(parseFloat(m[1])))
			}
		}
	}

	// GPU temps
	gpus := gpusCache
	if gpus == nil {
		gpus = getGpu()
	}
	for i, g := range gpus {
		if t, ok := g["temperature"].(int); ok && t > 0 {
			temps[fmt.Sprintf("gpu%d", i)] = t
		}
	}

	return temps
}

func parseFloat(s string) float64 {
	f, _ := strconv.ParseFloat(strings.TrimSpace(s), 64)
	return f
}

// ═══════════════════════════════════
// Network
// ═══════════════════════════════════

var (
	prevNetStats   = map[string]netStat{}
	prevNetStatsMu sync.Mutex
)

type netStat struct {
	rx, tx int64
	time   int64
}

func isPhysicalInterface(dev string) bool {
	skip := []string{"lo", "docker", "br-", "veth", "virbr", "tun", "tap"}
	for _, s := range skip {
		if dev == s || strings.HasPrefix(dev, s) {
			return false
		}
	}
	// Check physical device
	if _, err := os.Stat(fmt.Sprintf("/sys/class/net/%s/device", dev)); err == nil {
		return true
	}
	// Allow common naming patterns
	for _, prefix := range []string{"eth", "enp", "eno", "ens", "wl"} {
		if strings.HasPrefix(dev, prefix) {
			return true
		}
	}
	return false
}

func getNetwork() []map[string]interface{} {
	var interfaces []map[string]interface{}

	// Get all IPs
	allIps := map[string]string{}
	if ipOut, ok := run("ip -4 -o addr show 2>/dev/null"); ok {
		for _, line := range strings.Split(ipOut, "\n") {
			re := regexp.MustCompile(`^\d+:\s+(\S+)\s+inet\s+([\d.]+)`)
			if m := re.FindStringSubmatch(line); m != nil {
				allIps[m[1]] = m[2]
			}
		}
	}

	entries, _ := os.ReadDir("/sys/class/net")
	prevNetStatsMu.Lock()
	defer prevNetStatsMu.Unlock()

	now := time.Now().UnixMilli()

	for _, e := range entries {
		dev := e.Name()
		if !isPhysicalInterface(dev) {
			continue
		}

		operstate := readFileStr(fmt.Sprintf("/sys/class/net/%s/operstate", dev))
		if operstate != "up" {
			continue
		}

		speed := readFileStr(fmt.Sprintf("/sys/class/net/%s/speed", dev))
		rxBytes := parseInt64(readFileStr(fmt.Sprintf("/sys/class/net/%s/statistics/rx_bytes", dev)))
		txBytes := parseInt64(readFileStr(fmt.Sprintf("/sys/class/net/%s/statistics/tx_bytes", dev)))
		mac := readFileStr(fmt.Sprintf("/sys/class/net/%s/address", dev))
		isWifi := strings.HasPrefix(dev, "wl")

		var ssid, signal interface{}
		ssid = nil
		signal = nil
		if isWifi {
			if s, ok := run(fmt.Sprintf("iwgetid -r %s 2>/dev/null", dev)); ok && s != "" {
				ssid = strings.TrimSpace(s)
			}
			if sig, ok := run(fmt.Sprintf("iwconfig %s 2>/dev/null | grep -i signal", dev)); ok {
				re := regexp.MustCompile(`Signal level[=:]?\s*(-?\d+)`)
				if m := re.FindStringSubmatch(sig); m != nil {
					signal = parseIntDefault(m[1], 0)
				}
			}
		}

		// Calculate rates
		var rxRate, txRate int64
		if prev, ok := prevNetStats[dev]; ok {
			dt := float64(now-prev.time) / 1000
			if dt > 0 {
				rxRate = int64(math.Round(float64(rxBytes-prev.rx) / dt))
				txRate = int64(math.Round(float64(txBytes-prev.tx) / dt))
			}
		}
		prevNetStats[dev] = netStat{rx: rxBytes, tx: txBytes, time: now}

		speedStr := "—"
		if speed != "" {
			n := parseIntDefault(speed, 0)
			if n > 0 {
				speedStr = fmt.Sprintf("%s Mbps", speed)
			} else if isWifi && ssid != nil {
				speedStr = "WiFi"
			}
		}

		iface := map[string]interface{}{
			"name":            dev,
			"type":            "ethernet",
			"status":          operstate,
			"speed":           speedStr,
			"ip":              allIps[dev],
			"mac":             mac,
			"ssid":            ssid,
			"signal":          signal,
			"rxBytes":         rxBytes,
			"txBytes":         txBytes,
			"rxRate":          rxRate,
			"txRate":          txRate,
			"rxRateFormatted": formatBytes(rxRate) + "/s",
			"txRateFormatted": formatBytes(txRate) + "/s",
		}
		if isWifi {
			iface["type"] = "wifi"
		}
		if _, ok := allIps[dev]; !ok {
			iface["ip"] = "—"
		}
		interfaces = append(interfaces, iface)
	}

	if interfaces == nil {
		interfaces = []map[string]interface{}{}
	}
	return interfaces
}

// ═══════════════════════════════════
// Disks
// ═══════════════════════════════════

var (
	diskCache     map[string]interface{}
	diskCacheTime int64
	diskCacheMu   sync.Mutex
)

func getDisks() map[string]interface{} {
	diskCacheMu.Lock()
	defer diskCacheMu.Unlock()

	now := time.Now().UnixMilli()

	// Cache hardware info for 60s
	if diskCache == nil || (now-diskCacheTime) > 60000 {
		var disks []interface{}
		if lsblk, ok := run("lsblk -Jbdo NAME,SIZE,MODEL,TYPE,TRAN 2>/dev/null"); ok && lsblk != "" {
			var data struct {
				BlockDevices []struct {
					Name  string `json:"name"`
					Size  string `json:"size"`
					Model string `json:"model"`
					Type  string `json:"type"`
					Tran  string `json:"tran"`
				} `json:"blockdevices"`
			}
			if json.Unmarshal([]byte(lsblk), &data) == nil {
				for _, dev := range data.BlockDevices {
					if dev.Type != "disk" {
						continue
					}
					if strings.HasPrefix(dev.Name, "loop") || strings.HasPrefix(dev.Name, "ram") || strings.HasPrefix(dev.Name, "zram") {
						continue
					}
					size := parseInt64(dev.Size)
					if size <= 0 {
						continue
					}

					var temp interface{}
					if hasSmartctl {
						if smart, ok := run(fmt.Sprintf("smartctl -A /dev/%s 2>/dev/null | grep -i temperature | head -1", dev.Name)); ok && smart != "" {
							re := regexp.MustCompile(`(\d+)\s*$`)
							if m := re.FindStringSubmatch(smart); m != nil {
								temp = parseIntDefault(m[1], 0)
							}
						}
					}

					tran := dev.Tran
					if tran == "" {
						tran = "—"
					}
					disks = append(disks, map[string]interface{}{
						"name":          fmt.Sprintf("/dev/%s", dev.Name),
						"model":         strings.TrimSpace(dev.Model),
						"size":          size,
						"sizeFormatted": formatBytes(size),
						"temperature":   temp,
						"transport":     tran,
						"type":          "disk",
					})
				}
			}
		}
		if disks == nil {
			disks = []interface{}{}
		}

		// RAID
		var raids []interface{}
		mdstat := readFileStr("/proc/mdstat")
		if mdstat != "" {
			re := regexp.MustCompile(`(?m)^(md\d+)\s*:\s*active\s+(\w+)\s+(.+)`)
			for _, m := range re.FindAllStringSubmatch(mdstat, -1) {
				raids = append(raids, map[string]interface{}{
					"name": m[1], "type": m[2], "devices": strings.TrimSpace(m[3]),
				})
			}
		}
		if raids == nil {
			raids = []interface{}{}
		}

		diskCache = map[string]interface{}{"disks": disks, "raids": raids}
		diskCacheTime = now
	}

	// df always fresh
	var mounts []interface{}
	if df, ok := run("df -B1 --output=source,size,used,avail,target 2>/dev/null"); ok {
		for _, line := range strings.Split(df, "\n")[1:] {
			parts := strings.Fields(line)
			if len(parts) < 5 || !strings.HasPrefix(parts[0], "/dev/") || strings.Contains(parts[0], "loop") {
				continue
			}
			total := parseInt64(parts[1])
			used := parseInt64(parts[2])
			pct := 0
			if total > 0 {
				pct = int(math.Round(float64(used) / float64(total) * 100))
			}
			mounts = append(mounts, map[string]interface{}{
				"device":         parts[0],
				"total":          total,
				"used":           used,
				"available":      parseInt64(parts[3]),
				"mount":          parts[4],
				"totalFormatted": formatBytes(total),
				"usedFormatted":  formatBytes(used),
				"percent":        pct,
			})
		}
	}
	if mounts == nil {
		mounts = []interface{}{}
	}

	result := map[string]interface{}{}
	for k, v := range diskCache {
		result[k] = v
	}
	result["mounts"] = mounts
	return result
}

// ═══════════════════════════════════
// Uptime
// ═══════════════════════════════════

func getUptime() string {
	raw := readFileStr("/proc/uptime")
	if raw == "" {
		return "—"
	}
	secs := parseFloat(strings.Fields(raw)[0])
	days := int(secs) / 86400
	hours := (int(secs) % 86400) / 3600
	mins := (int(secs) % 3600) / 60
	if days > 0 {
		return fmt.Sprintf("%dd %dh", days, hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dh %dm", hours, mins)
	}
	return fmt.Sprintf("%dm", mins)
}

// ═══════════════════════════════════
// Containers
// ═══════════════════════════════════

var (
	containerCache     []interface{}
	containerCacheTime int64
	containerCacheMu   sync.Mutex
)

func getContainers() []interface{} {
	if !hasDocker {
		return []interface{}{}
	}
	containerCacheMu.Lock()
	defer containerCacheMu.Unlock()

	now := time.Now().UnixMilli()
	if containerCache != nil && (now-containerCacheTime) < 5000 {
		return containerCache
	}

	raw, ok := run(`docker ps -a --format "{{.ID}}|{{.Names}}|{{.Image}}|{{.Status}}|{{.Ports}}|{{.State}}|{{.CreatedAt}}" 2>/dev/null`)
	if !ok || raw == "" {
		return []interface{}{}
	}

	var containers []interface{}
	for _, line := range strings.Split(raw, "\n") {
		if line == "" {
			continue
		}
		parts := strings.SplitN(line, "|", 7)
		if len(parts) < 6 {
			continue
		}
		ports := "—"
		if len(parts) > 4 && parts[4] != "" {
			ports = parts[4]
		}
		c := map[string]interface{}{
			"id": parts[0], "name": parts[1], "image": parts[2],
			"status": parts[3], "ports": ports, "state": parts[5],
		}
		if len(parts) > 6 {
			c["created"] = parts[6]
		}
		containers = append(containers, c)
	}

	// docker stats
	if stats, ok := run(`docker stats --no-stream --format "{{.Name}}|{{.CPUPerc}}|{{.MemUsage}}|{{.MemPerc}}" 2>/dev/null`); ok && stats != "" {
		statMap := map[string][3]string{}
		for _, line := range strings.Split(stats, "\n") {
			p := strings.SplitN(line, "|", 4)
			if len(p) >= 4 {
				statMap[p[0]] = [3]string{p[1], p[2], p[3]}
			}
		}
		for _, c := range containers {
			cm := c.(map[string]interface{})
			if s, ok := statMap[cm["name"].(string)]; ok {
				cm["cpu"] = s[0]
				cm["mem"] = s[1]
				cm["memPct"] = s[2]
			} else {
				cm["cpu"] = "—"
				cm["mem"] = "—"
				cm["memPct"] = "—"
			}
		}
	}

	if containers == nil {
		containers = []interface{}{}
	}
	containerCache = containers
	containerCacheTime = now
	return containers
}

func containerAction(id, action string) map[string]interface{} {
	allowed := map[string]bool{"start": true, "stop": true, "restart": true, "pause": true, "unpause": true}
	if !allowed[action] {
		return map[string]interface{}{"error": "Invalid action"}
	}
	// Sanitize
	re := regexp.MustCompile(`[^a-zA-Z0-9_.\-/:]+`)
	safeId := re.ReplaceAllString(id, "")
	if safeId == "" || len(safeId) > 256 || strings.Contains(safeId, "..") {
		return map[string]interface{}{"error": "Invalid container ID"}
	}
	out, _ := run(fmt.Sprintf("docker %s %s 2>&1", action, safeId))
	return map[string]interface{}{"ok": true, "action": action, "id": safeId, "output": out}
}

// ═══════════════════════════════════
// System Summary
// ═══════════════════════════════════

var (
	systemCache     map[string]interface{}
	systemCacheTime int64
	systemCacheMu   sync.Mutex
)

func getSystemSummary() map[string]interface{} {
	systemCacheMu.Lock()
	defer systemCacheMu.Unlock()

	now := time.Now().UnixMilli()
	if systemCache != nil && (now-systemCacheTime) < 1500 {
		return systemCache
	}

	cpu := getCpuUsage()
	mem := getMemory()
	gpus := getGpu()
	temps := getTemps(gpus)
	network := getNetwork()
	diskInfo := getDisks()
	uptime := getUptime()

	hostname, _ := os.Hostname()

	// Main temp
	var mainTemp interface{}
	for _, key := range []string{"x86_pkg_temp", "cpu", "coretemp"} {
		if v, ok := temps[key]; ok {
			mainTemp = v
			break
		}
	}
	if mainTemp == nil {
		for _, v := range temps {
			mainTemp = v
			break
		}
	}

	// Primary network interface
	var primaryNet interface{}
	for _, n := range network {
		ip, _ := n["ip"].(string)
		status, _ := n["status"].(string)
		if ip != "—" && status == "up" {
			primaryNet = n
			break
		}
	}
	if primaryNet == nil && len(network) > 0 {
		primaryNet = network[0]
	}

	uname, _ := run("uname -sr 2>/dev/null")

	systemCache = map[string]interface{}{
		"cpu":        cpu,
		"memory":     mem,
		"gpus":       gpus,
		"temps":      temps,
		"mainTemp":   mainTemp,
		"network":    network,
		"primaryNet": primaryNet,
		"disks":      diskInfo,
		"uptime":     uptime,
		"hostname":   hostname,
		"platform":   uname,
	}
	systemCacheTime = now
	return systemCache
}

// ═══════════════════════════════════
// Hardware HTTP routes
// ═══════════════════════════════════

func handleHardwareRoutes(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}

	path := r.URL.Path
	switch path {
	case "/api/system":
		jsonOk(w, getSystemSummary())
	case "/api/cpu":
		jsonOk(w, getCpuUsage())
	case "/api/memory":
		jsonOk(w, getMemory())
	case "/api/gpu":
		jsonOk(w, getGpu())
	case "/api/temps":
		jsonOk(w, getTemps(nil))
	case "/api/network":
		jsonOk(w, getNetwork())
	case "/api/disks":
		jsonOk(w, getDisks())
	case "/api/uptime":
		jsonOk(w, map[string]interface{}{"uptime": getUptime()})
	case "/api/containers":
		jsonOk(w, getContainers())
	case "/api/hostname":
		h, _ := os.Hostname()
		jsonOk(w, map[string]interface{}{"hostname": h})
	case "/api/hardware/gpu-info":
		jsonOk(w, getHardwareGpuInfo())
	case "/api/system/info":
		handleSystemInfo(w)
	case "/api/system/update/check":
		handleUpdateCheck(w)
	case "/api/system/update/status":
		handleUpdateStatus(w)
	case "/api/system/reboot", "/api/system/shutdown", "/api/system/reboot-service", "/api/system/update/apply", "/api/terminal":
		// These are POST-only admin routes — reject GET and non-admin
		if r.Method != "POST" {
			jsonError(w, 405, "Method not allowed")
			return
		}
		handleSystemPost(w, r, session)
	default:
		// POST routes need body
		if r.Method == "POST" {
			handleSystemPost(w, r, session)
			return
		}
		jsonError(w, 404, "Not found")
	}
}

func handleSystemPost(w http.ResponseWriter, r *http.Request, session map[string]interface{}) {
	if role, _ := session["role"].(string); role != "admin" {
		jsonError(w, 403, "Unauthorized")
		return
	}

	path := r.URL.Path
	switch path {
	case "/api/system/reboot-service":
		jsonOk(w, map[string]interface{}{"ok": true, "message": "NimOS restarting..."})
		go func() {
			time.Sleep(1 * time.Second)
			run("sudo systemctl restart nimbusos")
		}()
	case "/api/system/reboot":
		jsonOk(w, map[string]interface{}{"ok": true, "message": "System rebooting..."})
		go func() {
			time.Sleep(1 * time.Second)
			run("sudo reboot")
		}()
	case "/api/system/shutdown":
		jsonOk(w, map[string]interface{}{"ok": true, "message": "System shutting down..."})
		go func() {
			time.Sleep(1 * time.Second)
			run("sudo shutdown -h now")
		}()
	case "/api/system/update/apply":
		handleUpdateApply(w)
	case "/api/terminal":
		handleTerminal(w, r)
	default:
		jsonError(w, 404, "Not found")
	}
}

func handleUpdateCheck(w http.ResponseWriter) {
	currentVersion := "0.0.0"
	if data, err := os.ReadFile("/opt/nimbusos/package.json"); err == nil {
		var pkg map[string]interface{}
		if json.Unmarshal(data, &pkg) == nil {
			if v, ok := pkg["version"].(string); ok {
				currentVersion = v
			}
		}
	}
	latestVersion := "0.0.0"
	if out, ok := run(`curl -fsSL "https://raw.githubusercontent.com/andresgv-beep/NimOs-beta-5/main/package.json" 2>/dev/null`); ok {
		var pkg map[string]interface{}
		if json.Unmarshal([]byte(out), &pkg) == nil {
			if v, ok := pkg["version"].(string); ok {
				latestVersion = v
			}
		}
	}
	jsonOk(w, map[string]interface{}{
		"currentVersion":  currentVersion,
		"latestVersion":   latestVersion,
		"updateAvailable": latestVersion != currentVersion,
		"installDir":      "/opt/nimbusos",
	})
}

func handleUpdateApply(w http.ResponseWriter) {
	script := "/opt/nimbusos/scripts/update.sh"
	if _, err := os.Stat(script); err != nil {
		jsonError(w, 400, "Update script not found")
		return
	}
	os.MkdirAll("/var/log/nimbusos", 0755)
	os.Remove("/var/log/nimbusos/update-result.json")

	// Launch update in a fully detached process so it survives daemon restart.
	// The script does "systemctl stop/restart nimos-daemon" which kills us,
	// so the child must be in its own session (setsid) to not receive our SIGTERM.
	cmd := exec.Command("setsid", "bash", script)
	cmd.Dir = "/opt/nimbusos"
	cmd.SysProcAttr = &syscall.SysProcAttr{
		Setpgid: true,
	}
	// Redirect stdout/stderr to log file
	logFile, err := os.OpenFile("/var/log/nimbusos/update.log", os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err == nil {
		cmd.Stdout = logFile
		cmd.Stderr = logFile
	}
	if err := cmd.Start(); err != nil {
		jsonError(w, 500, fmt.Sprintf("Failed to start update: %v", err))
		return
	}
	// Do NOT call cmd.Wait() — let the process run independently
	jsonOk(w, map[string]interface{}{"ok": true, "message": "Update started."})
}

func handleUpdateStatus(w http.ResponseWriter) {
	rf := "/var/log/nimbusos/update-result.json"
	if data, err := os.ReadFile(rf); err == nil {
		var result map[string]interface{}
		if json.Unmarshal(data, &result) == nil {
			result["done"] = true
			jsonOk(w, result)
			return
		}
	}
	jsonOk(w, map[string]interface{}{"done": false})
}

func handleTerminal(w http.ResponseWriter, r *http.Request) {
	body, _ := readBody(r)
	cmd := bodyStr(body, "cmd")
	cwd := bodyStr(body, "cwd")
	if cmd == "" || len(cmd) > 10000 {
		jsonError(w, 400, "Invalid cmd")
		return
	}
	if cwd == "" {
		cwd = "/root"
	}
	stdout, _ := run(fmt.Sprintf("cd '%s' 2>/dev/null && %s 2>&1", cwd, cmd))
	jsonOk(w, map[string]interface{}{"stdout": stdout, "stderr": "", "code": 0, "cwd": cwd})
}

func handleSystemInfo(w http.ResponseWriter) {
	interfaces := getNetwork()
	hostname, _ := os.Hostname()
	gateway, _ := run("ip route | grep default | awk '{print $3}' | head -1")
	if gateway == "" {
		gateway = "—"
	}
	dnsOut, _ := run("cat /etc/resolv.conf 2>/dev/null | grep nameserver | awk '{print $2}'")
	var dnsServers []string
	for _, s := range strings.Split(dnsOut, "\n") {
		if s != "" {
			dnsServers = append(dnsServers, s)
		}
	}
	if dnsServers == nil {
		dnsServers = []string{}
	}

	// Find primary interface name
	primaryName := "eth0"
	for _, n := range interfaces {
		if ip, _ := n["ip"].(string); ip != "—" {
			primaryName, _ = n["name"].(string)
			break
		}
	}
	subnet, _ := run(fmt.Sprintf("ip -4 -o addr show %s 2>/dev/null | awk '{print $4}'", primaryName))
	if subnet == "" {
		subnet = "—"
	}

	jsonOk(w, map[string]interface{}{
		"network": map[string]interface{}{
			"hostname":   hostname,
			"gateway":    gateway,
			"subnet":     subnet,
			"dns":        dnsServers,
			"interfaces": interfaces,
		},
	})
}
