package main

// ═══════════════════════════════════════════════════════════════════════════════
// NimOS Backup — WireGuard Tunnel Management
// Manages WireGuard interfaces for encrypted NAS-to-NAS connections over WAN.
// Used by NimBackup when devices are not on the same LAN.
// ═══════════════════════════════════════════════════════════════════════════════

import (
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"
)

// ─── Constants ──────────────────────────────────────────────────────────────

const (
	wgConfigDir  = "/etc/wireguard"
	wgInterface  = "wg-nimbackup"
	wgListenPort = 51820
	wgSubnet     = "10.10.0"        // WG peers get 10.10.0.x/24
)

// ─── Key Generation ─────────────────────────────────────────────────────────

// generateWGKeyPair generates a WireGuard private/public key pair using wg cli.
func generateWGKeyPair() (privateKey, publicKey string, err error) {
	// Generate private key
	privOut, ok := run("wg genkey")
	if !ok || privOut == "" {
		return "", "", fmt.Errorf("wg genkey failed: %s", privOut)
	}
	privateKey = strings.TrimSpace(privOut)

	// Derive public key
	pubOut, ok := run(fmt.Sprintf("echo '%s' | wg pubkey", privateKey))
	if !ok || pubOut == "" {
		return "", "", fmt.Errorf("wg pubkey failed: %s", pubOut)
	}
	publicKey = strings.TrimSpace(pubOut)

	return privateKey, publicKey, nil
}

// ─── Config Management ─────────────────────────────────────────────────────

// WGPeerConfig represents a single WireGuard peer entry.
type WGPeerConfig struct {
	PublicKey  string `json:"publicKey"`
	Endpoint  string `json:"endpoint"`  // host:port
	AllowedIPs string `json:"allowedIPs"` // e.g., "10.10.0.2/32"
	DeviceID  string `json:"deviceId"`  // NimBackup device ID for tracking
}

// wgState holds the local WireGuard state for NimBackup.
// Persisted as JSON in the config directory.
type wgState struct {
	PrivateKey string         `json:"privateKey"`
	PublicKey  string         `json:"publicKey"`
	ListenPort int            `json:"listenPort"`
	LocalIP    string         `json:"localIP"` // e.g., "10.10.0.1/24"
	Peers      []WGPeerConfig `json:"peers"`
	NextPeerIP int            `json:"nextPeerIP"` // tracks next available .x
}

const wgStatePath = "/var/lib/nimbusos/config/wireguard-state.json"

func loadWGState() (*wgState, error) {
	data, err := os.ReadFile(wgStatePath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // No state yet — first time
		}
		return nil, err
	}
	var state wgState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	return &state, nil
}

func saveWGState(state *wgState) error {
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(wgStatePath, data, 0600)
}

// initWGState initializes WireGuard state if it doesn't exist yet.
// Generates keys and assigns the local IP as .1 in the subnet.
func initWGState() (*wgState, error) {
	state, err := loadWGState()
	if err != nil {
		return nil, fmt.Errorf("load wg state: %v", err)
	}
	if state != nil {
		return state, nil // Already initialized
	}

	// Generate new key pair
	privKey, pubKey, err := generateWGKeyPair()
	if err != nil {
		return nil, fmt.Errorf("generate keys: %v", err)
	}

	state = &wgState{
		PrivateKey: privKey,
		PublicKey:  pubKey,
		ListenPort: wgListenPort,
		LocalIP:    fmt.Sprintf("%s.1/24", wgSubnet),
		Peers:      []WGPeerConfig{},
		NextPeerIP: 2,
	}

	if err := saveWGState(state); err != nil {
		return nil, fmt.Errorf("save wg state: %v", err)
	}

	logMsg("wireguard: initialized — pubkey=%s, local=%s", pubKey[:8]+"...", state.LocalIP)
	return state, nil
}

// ─── Config File Generation ─────────────────────────────────────────────────

// writeWGConfig writes the wg-nimbackup.conf file from current state.
func writeWGConfig(state *wgState) error {
	os.MkdirAll(wgConfigDir, 0700)

	var sb strings.Builder
	sb.WriteString("[Interface]\n")
	sb.WriteString(fmt.Sprintf("PrivateKey = %s\n", state.PrivateKey))
	sb.WriteString(fmt.Sprintf("ListenPort = %d\n", state.ListenPort))
	sb.WriteString(fmt.Sprintf("Address = %s\n", state.LocalIP))
	sb.WriteString("\n")

	for _, peer := range state.Peers {
		sb.WriteString("[Peer]\n")
		sb.WriteString(fmt.Sprintf("PublicKey = %s\n", peer.PublicKey))
		if peer.Endpoint != "" {
			sb.WriteString(fmt.Sprintf("Endpoint = %s\n", peer.Endpoint))
		}
		sb.WriteString(fmt.Sprintf("AllowedIPs = %s\n", peer.AllowedIPs))
		sb.WriteString("PersistentKeepalive = 25\n")
		sb.WriteString("\n")
	}

	confPath := fmt.Sprintf("%s/%s.conf", wgConfigDir, wgInterface)
	if err := os.WriteFile(confPath, []byte(sb.String()), 0600); err != nil {
		return fmt.Errorf("write wg config: %v", err)
	}

	logMsg("wireguard: config written to %s (%d peers)", confPath, len(state.Peers))
	return nil
}

// ─── Interface Control ──────────────────────────────────────────────────────

// wgUp brings up the WireGuard interface.
func wgUp() error {
	// Check if already up
	if out, ok := run(fmt.Sprintf("ip link show %s 2>/dev/null", wgInterface)); ok && out != "" {
		// Already up — just reload config
		_, ok := run(fmt.Sprintf("wg syncconf %s %s/%s.conf", wgInterface, wgConfigDir, wgInterface))
		if !ok {
			return fmt.Errorf("wg syncconf failed")
		}
		logMsg("wireguard: config reloaded (interface was already up)")
		return nil
	}

	// Bring up with wg-quick
	out, ok := run(fmt.Sprintf("wg-quick up %s", wgInterface))
	if !ok {
		return fmt.Errorf("wg-quick up failed: %s", out)
	}
	logMsg("wireguard: interface %s is up", wgInterface)
	return nil
}

// wgDown takes down the WireGuard interface.
func wgDown() error {
	out, ok := run(fmt.Sprintf("wg-quick down %s 2>/dev/null", wgInterface))
	if !ok {
		// Not a fatal error — interface might not be up
		logMsg("wireguard: wg-quick down: %s", out)
	}
	logMsg("wireguard: interface %s is down", wgInterface)
	return nil
}

// wgIsUp checks if the WireGuard interface is currently up.
func wgIsUp() bool {
	out, ok := run(fmt.Sprintf("ip link show %s 2>/dev/null", wgInterface))
	return ok && out != "" && strings.Contains(out, "UP")
}

// ─── Peer Management ────────────────────────────────────────────────────────

// addWGPeer adds a new peer to the WireGuard configuration and brings up/reloads the tunnel.
// Returns the assigned IP for the remote peer.
func addWGPeer(deviceID, remotePublicKey, remoteEndpoint string) (assignedIP string, err error) {
	state, err := initWGState()
	if err != nil {
		return "", err
	}

	// Check if peer already exists for this device
	for _, p := range state.Peers {
		if p.DeviceID == deviceID {
			return strings.TrimSuffix(p.AllowedIPs, "/32"), nil
		}
	}

	// Assign next IP
	peerIP := fmt.Sprintf("%s.%d", wgSubnet, state.NextPeerIP)
	state.NextPeerIP++

	peer := WGPeerConfig{
		PublicKey:  remotePublicKey,
		Endpoint:   remoteEndpoint,
		AllowedIPs: peerIP + "/32",
		DeviceID:   deviceID,
	}
	state.Peers = append(state.Peers, peer)

	if err := saveWGState(state); err != nil {
		return "", fmt.Errorf("save state: %v", err)
	}

	if err := writeWGConfig(state); err != nil {
		return "", fmt.Errorf("write config: %v", err)
	}

	if err := wgUp(); err != nil {
		return "", fmt.Errorf("bring up interface: %v", err)
	}

	// Update the device record in the database with WG info
	localIP := state.LocalIP
	db.Exec(`UPDATE backup_devices SET wg_active = 1, wg_public_key = ?, wg_endpoint = ?,
		wg_allowed_ips = ?, wg_local_ip = ? WHERE id = ?`,
		remotePublicKey, remoteEndpoint, peerIP+"/32", localIP, deviceID)

	logMsg("wireguard: peer added — device=%s, ip=%s, endpoint=%s", deviceID, peerIP, remoteEndpoint)
	return peerIP, nil
}

// removeWGPeer removes a peer associated with a device.
func removeWGPeer(deviceID string) error {
	state, err := loadWGState()
	if err != nil || state == nil {
		return fmt.Errorf("no wireguard state")
	}

	found := false
	var filtered []WGPeerConfig
	for _, p := range state.Peers {
		if p.DeviceID == deviceID {
			found = true
			continue
		}
		filtered = append(filtered, p)
	}

	if !found {
		return fmt.Errorf("peer not found for device %s", deviceID)
	}

	state.Peers = filtered

	if err := saveWGState(state); err != nil {
		return err
	}

	if err := writeWGConfig(state); err != nil {
		return err
	}

	// Clear WG info in device record
	db.Exec(`UPDATE backup_devices SET wg_active = 0, wg_public_key = '', wg_endpoint = '',
		wg_allowed_ips = '', wg_local_ip = '' WHERE id = ?`, deviceID)

	// If no more peers, take down the interface
	if len(state.Peers) == 0 {
		wgDown()
		logMsg("wireguard: last peer removed, interface down")
	} else {
		wgUp() // Reload config
		logMsg("wireguard: peer removed for device %s, %d peers remain", deviceID, len(state.Peers))
	}

	return nil
}

// ─── Key Exchange (Pairing Flow) ────────────────────────────────────────────

// initiateWGPairing performs the WireGuard key exchange with a remote NimOS device.
// Called during the pairing process when the connection is WAN (not LAN).
//
// Flow:
//  1. Initialize local WG state (generate keys if first time)
//  2. Send our public key to the remote device via authenticated API
//  3. Remote generates its key pair and returns its public key
//  4. Both sides configure their wg interfaces
//  5. Verify connectivity through the tunnel
func initiateWGPairing(deviceID, remoteAddr, remoteToken string) (map[string]interface{}, error) {
	// 1. Init local state
	state, err := initWGState()
	if err != nil {
		return nil, fmt.Errorf("init local wg: %v", err)
	}

	// 2. Send our public key to the remote
	client := &http.Client{Timeout: 15 * time.Second}
	payload := map[string]interface{}{
		"publicKey":  state.PublicKey,
		"listenPort": state.ListenPort,
		"localIP":    state.LocalIP,
	}
	payloadJSON, _ := json.Marshal(payload)

	req, _ := http.NewRequest("POST",
		fmt.Sprintf("https://%s:5009/api/backup/pair/wg-exchange", remoteAddr),
		strings.NewReader(string(payloadJSON)))
	req.Header.Set("Authorization", "Bearer "+remoteToken)
	req.Header.Set("Content-Type", "application/json")

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("wg exchange request failed: %v", err)
	}
	defer resp.Body.Close()

	var result map[string]interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, fmt.Errorf("wg exchange response parse error: %v", err)
	}

	if errMsg, ok := result["error"].(string); ok && errMsg != "" {
		return nil, fmt.Errorf("remote wg exchange error: %s", errMsg)
	}

	remotePublicKey, _ := result["publicKey"].(string)
	remoteEndpoint := fmt.Sprintf("%s:%d", remoteAddr, wgListenPort)

	if remotePublicKey == "" {
		return nil, fmt.Errorf("remote did not provide a public key")
	}

	// 3. Add the remote as a peer locally
	assignedIP, err := addWGPeer(deviceID, remotePublicKey, remoteEndpoint)
	if err != nil {
		return nil, fmt.Errorf("add peer: %v", err)
	}

	// 4. Verify tunnel connectivity (ping through WG)
	tunnelOk := false
	for attempt := 0; attempt < 5; attempt++ {
		time.Sleep(time.Duration(500+attempt*500) * time.Millisecond)
		if out, ok := run(fmt.Sprintf("ping -c 1 -W 2 %s 2>/dev/null", assignedIP)); ok && strings.Contains(out, "1 received") {
			tunnelOk = true
			break
		}
	}

	return map[string]interface{}{
		"ok":              true,
		"tunnelVerified":  tunnelOk,
		"localPublicKey":  state.PublicKey,
		"remotePublicKey": remotePublicKey,
		"assignedIP":      assignedIP,
		"localIP":         state.LocalIP,
	}, nil
}

// handleWGExchange handles the remote side of the WireGuard key exchange.
// Called on the remote NAS when the initiating NAS sends its public key.
func handleWGExchange(body map[string]interface{}) map[string]interface{} {
	remotePubKey := bodyStr(body, "publicKey")
	if remotePubKey == "" {
		return map[string]interface{}{"error": "publicKey required"}
	}

	// Init our state
	state, err := initWGState()
	if err != nil {
		return map[string]interface{}{"error": "init wg failed: " + err.Error()}
	}

	// We'll be assigned as a peer too — but we don't know the device ID yet on this side.
	// Use the public key as temporary identifier.
	peerIP := fmt.Sprintf("%s.%d", wgSubnet, state.NextPeerIP)
	state.NextPeerIP++

	// Get remote endpoint from the request context (not available here — caller must provide)
	remoteEndpoint := bodyStr(body, "endpoint")

	peer := WGPeerConfig{
		PublicKey:  remotePubKey,
		Endpoint:   remoteEndpoint,
		AllowedIPs: peerIP + "/32",
		DeviceID:   "pending-" + remotePubKey[:8],
	}
	state.Peers = append(state.Peers, peer)

	if err := saveWGState(state); err != nil {
		return map[string]interface{}{"error": "save state: " + err.Error()}
	}

	if err := writeWGConfig(state); err != nil {
		return map[string]interface{}{"error": "write config: " + err.Error()}
	}

	if err := wgUp(); err != nil {
		return map[string]interface{}{"error": "bring up interface: " + err.Error()}
	}

	return map[string]interface{}{
		"ok":         true,
		"publicKey":  state.PublicKey,
		"listenPort": state.ListenPort,
		"assignedIP": peerIP,
	}
}

// ─── Status ─────────────────────────────────────────────────────────────────

// getWGStatus returns the current WireGuard status for the NimBackup interface.
func getWGStatus() map[string]interface{} {
	state, err := loadWGState()
	if err != nil || state == nil {
		return map[string]interface{}{
			"configured": false,
			"active":     false,
			"peers":      0,
		}
	}

	active := wgIsUp()

	peers := []map[string]interface{}{}
	if active {
		// Get live peer stats from wg show
		out, ok := run(fmt.Sprintf("wg show %s dump 2>/dev/null", wgInterface))
		if ok && out != "" {
			peerStats := parseWGDump(out)
			for _, p := range state.Peers {
				peerInfo := map[string]interface{}{
					"publicKey":  p.PublicKey[:8] + "...",
					"allowedIPs": p.AllowedIPs,
					"deviceId":   p.DeviceID,
					"endpoint":   p.Endpoint,
				}
				// Merge live stats
				if stats, ok := peerStats[p.PublicKey]; ok {
					peerInfo["lastHandshake"] = stats["lastHandshake"]
					peerInfo["rxBytes"] = stats["rxBytes"]
					peerInfo["txBytes"] = stats["txBytes"]
					peerInfo["connected"] = stats["connected"]
				}
				peers = append(peers, peerInfo)
			}
		}
	} else {
		for _, p := range state.Peers {
			peers = append(peers, map[string]interface{}{
				"publicKey":  p.PublicKey[:8] + "...",
				"allowedIPs": p.AllowedIPs,
				"deviceId":   p.DeviceID,
				"connected":  false,
			})
		}
	}

	return map[string]interface{}{
		"configured": true,
		"active":     active,
		"publicKey":  state.PublicKey[:8] + "...",
		"localIP":    state.LocalIP,
		"listenPort": state.ListenPort,
		"peerCount":  len(state.Peers),
		"peers":      peers,
	}
}

// parseWGDump parses output of "wg show <iface> dump" into a map keyed by public key.
// Format: <pubkey>\t<preshared>\t<endpoint>\t<allowed-ips>\t<latest-handshake>\t<rx>\t<tx>\t<keepalive>
func parseWGDump(dump string) map[string]map[string]interface{} {
	result := map[string]map[string]interface{}{}
	lines := strings.Split(strings.TrimSpace(dump), "\n")

	for _, line := range lines[1:] { // Skip first line (interface info)
		fields := strings.Split(line, "\t")
		if len(fields) < 7 {
			continue
		}

		pubKey := fields[0]
		handshakeTS := parseInt(fields[4], 0)
		rxBytes := parseInt(fields[5], 0)
		txBytes := parseInt(fields[6], 0)

		connected := false
		lastHandshake := "never"
		if handshakeTS > 0 {
			t := time.Unix(int64(handshakeTS), 0)
			age := time.Since(t)
			if age < 3*time.Minute {
				connected = true
			}
			lastHandshake = fmt.Sprintf("%.0fs ago", age.Seconds())
		}

		result[pubKey] = map[string]interface{}{
			"lastHandshake": lastHandshake,
			"rxBytes":       rxBytes,
			"txBytes":       txBytes,
			"connected":     connected,
		}
	}

	return result
}

// ─── HTTP Route Extensions ──────────────────────────────────────────────────
// These are called from handleBackupRoutes in backup.go via the WG-specific paths.

// handleWGRoutes handles /api/backup/wg/* endpoints.
func handleWGRoutes(w http.ResponseWriter, r *http.Request, urlPath string, body map[string]interface{}) {
	switch {
	// GET /api/backup/wg/status
	case urlPath == "/api/backup/wg/status" && r.Method == "GET":
		jsonOk(w, getWGStatus())

	// POST /api/backup/wg/setup — init WireGuard (generate keys, create config)
	case urlPath == "/api/backup/wg/setup" && r.Method == "POST":
		state, err := initWGState()
		if err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		if err := writeWGConfig(state); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOk(w, map[string]interface{}{
			"ok":        true,
			"publicKey": state.PublicKey,
			"localIP":   state.LocalIP,
		})

	// POST /api/backup/wg/up — bring up interface
	case urlPath == "/api/backup/wg/up" && r.Method == "POST":
		state, err := loadWGState()
		if err != nil || state == nil {
			jsonError(w, 400, "WireGuard not configured. Run setup first.")
			return
		}
		if err := writeWGConfig(state); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		if err := wgUp(); err != nil {
			jsonError(w, 500, err.Error())
			return
		}
		jsonOk(w, map[string]interface{}{"ok": true, "active": true})

	// POST /api/backup/wg/down — take down interface
	case urlPath == "/api/backup/wg/down" && r.Method == "POST":
		wgDown()
		jsonOk(w, map[string]interface{}{"ok": true, "active": false})

	// POST /api/backup/pair/wg-exchange — handle remote key exchange
	case urlPath == "/api/backup/pair/wg-exchange" && r.Method == "POST":
		result := handleWGExchange(body)
		if errMsg, ok := result["error"].(string); ok && errMsg != "" {
			jsonError(w, 500, errMsg)
			return
		}
		jsonOk(w, result)

	// DELETE /api/backup/wg/peer/:deviceId — remove a WG peer
	case strings.HasPrefix(urlPath, "/api/backup/wg/peer/") && r.Method == "DELETE":
		deviceID := strings.TrimPrefix(urlPath, "/api/backup/wg/peer/")
		deviceID = strings.TrimSuffix(deviceID, "/")
		if err := removeWGPeer(deviceID); err != nil {
			jsonError(w, 404, err.Error())
			return
		}
		jsonOk(w, map[string]interface{}{"ok": true})

	default:
		jsonError(w, 404, "Not found")
	}
}
