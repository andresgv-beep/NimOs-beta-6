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
	"time"
)

// runLong executes a command with a custom timeout in seconds (no retry)
func runLong(command string, timeoutSecs int) (string, bool) {
	ctx := exec.Command("sh", "-c", command)
	done := make(chan struct{})
	var out []byte
	var err error
	go func() {
		out, err = ctx.CombinedOutput()
		close(done)
	}()
	select {
	case <-done:
		return strings.TrimSpace(string(out)), err == nil
	case <-time.After(time.Duration(timeoutSecs) * time.Second):
		ctx.Process.Kill()
		return "timeout", false
	}
}

// ═══════════════════════════════════
// Config files
// ═══════════════════════════════════

const (
	ddnsConfigFile         = "/var/lib/nimbusos/config/ddns.json"
	ddnsLogFile            = "/var/lib/nimbusos/config/ddns.log"
	remoteAccessConfigFile = "/var/lib/nimbusos/config/remote-access.json"
	smbConfigFile          = "/var/lib/nimbusos/config/smb.json"
	proxyConfigFile        = "/var/lib/nimbusos/config/proxy-rules.json"
	webdavConfigFile       = "/var/lib/nimbusos/config/webdav.json"
)

func readJSONConfig(path string, defaults map[string]interface{}) map[string]interface{} {
	data, err := os.ReadFile(path)
	if err != nil {
		return defaults
	}
	var conf map[string]interface{}
	if json.Unmarshal(data, &conf) != nil {
		return defaults
	}
	return conf
}

func writeJSONConfig(path string, conf interface{}) {
	data, _ := json.MarshalIndent(conf, "", "  ")
	os.MkdirAll(filepath.Dir(path), 0755)
	os.WriteFile(path, data, 0644)
}

// ═══════════════════════════════════
// DDNS
// ═══════════════════════════════════

func handleDdnsRoutes(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	path := r.URL.Path
	method := r.Method

	if path == "/api/ddns/status" && method == "GET" {
		conf := readJSONConfig(ddnsConfigFile, map[string]interface{}{"enabled": false})
		extIp, _ := run("curl -fsSL --connect-timeout 5 https://api.ipify.org 2>/dev/null")
		if extIp == "" {
			extIp, _ = run("curl -fsSL --connect-timeout 5 https://ifconfig.me 2>/dev/null")
		}
		lastLog := ""
		if data, err := os.ReadFile(ddnsLogFile); err == nil {
			lines := strings.Split(strings.TrimSpace(string(data)), "\n")
			if len(lines) > 0 {
				lastLog = lines[len(lines)-1]
			}
		}
		jsonOk(w, map[string]interface{}{"config": conf, "externalIp": strings.TrimSpace(extIp), "lastLog": lastLog})
		return
	}

	if path == "/api/ddns/config" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" {
			jsonError(w, 403, "Admin required"); return
		}
		body, _ := readBody(r)
		writeJSONConfig(ddnsConfigFile, body)
		jsonOk(w, map[string]interface{}{"ok": true})
		return
	}

	if path == "/api/ddns/test" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" {
			jsonError(w, 403, "Admin required"); return
		}
		body, _ := readBody(r)
		jsonOk(w, ddnsUpdateGo(body))
		return
	}

	if path == "/api/ddns/logs" && method == "GET" {
		log := ""
		if data, err := os.ReadFile(ddnsLogFile); err == nil {
			log = string(data)
		}
		jsonOk(w, map[string]interface{}{"log": log})
		return
	}

	jsonError(w, 404, "Not found")
}

func ddnsUpdateGo(cfg map[string]interface{}) map[string]interface{} {
	provider := bodyStr(cfg, "provider")
	domain := strings.TrimSpace(bodyStr(cfg, "domain"))
	token := strings.TrimSpace(bodyStr(cfg, "token"))

	var curlURL string
	switch provider {
	case "duckdns":
		subdomain := strings.Replace(domain, ".duckdns.org", "", 1)
		curlURL = fmt.Sprintf("https://www.duckdns.org/update?domains=%s&token=%s&ip=", subdomain, token)
	case "noip":
		curlURL = fmt.Sprintf("https://dynupdate.no-ip.com/nic/update?hostname=%s", domain)
	case "dynu":
		curlURL = fmt.Sprintf("https://api.dynu.com/nic/update?hostname=%s&password=%s", domain, token)
	case "freedns":
		curlURL = fmt.Sprintf("https://freedns.afraid.org/dynamic/update.php?%s", token)
	default:
		return map[string]interface{}{"ok": false, "error": "Unknown provider"}
	}

	result, ok := run(fmt.Sprintf(`curl -fsSL "%s" 2>&1`, curlURL))
	if ok {
		return map[string]interface{}{"ok": true, "response": strings.TrimSpace(result)}
	}
	return map[string]interface{}{"ok": false, "error": result}
}

// ═══════════════════════════════════
// Remote Access
// ═══════════════════════════════════

func handleRemoteAccessRoutes(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil {
		return
	}
	urlPath := r.URL.Path
	method := r.Method

	if urlPath == "/api/remote-access/status" && method == "GET" {
		cfg := readJSONConfig(remoteAccessConfigFile, map[string]interface{}{
			"ddns": map[string]interface{}{"enabled": false}, "ssl": map[string]interface{}{"enabled": false},
			"https": map[string]interface{}{"enabled": false, "port": float64(5009)},
		})
		status := getRemoteAccessStatusGo(cfg)
		status["config"] = cfg
		jsonOk(w, status)
		return
	}

	if urlPath == "/api/remote-access/configure" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" {
			jsonError(w, 403, "Admin required"); return
		}
		body, _ := readBody(r)
		cfg := readJSONConfig(remoteAccessConfigFile, map[string]interface{}{})
		if ddns, ok := body["ddns"].(map[string]interface{}); ok {
			existing, _ := cfg["ddns"].(map[string]interface{})
			if existing == nil { existing = map[string]interface{}{} }
			for k, v := range ddns { existing[k] = v }
			cfg["ddns"] = existing
		}
		if ssl, ok := body["ssl"].(map[string]interface{}); ok {
			existing, _ := cfg["ssl"].(map[string]interface{})
			if existing == nil { existing = map[string]interface{}{} }
			for k, v := range ssl { existing[k] = v }
			cfg["ssl"] = existing
		}
		if https, ok := body["https"].(map[string]interface{}); ok {
			existing, _ := cfg["https"].(map[string]interface{})
			if existing == nil { existing = map[string]interface{}{} }
			for k, v := range https { existing[k] = v }
			cfg["https"] = existing
		}
		writeJSONConfig(remoteAccessConfigFile, cfg)
		jsonOk(w, map[string]interface{}{"ok": true})
		return
	}

	if urlPath == "/api/remote-access/test-ddns" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" {
			jsonError(w, 403, "Admin required"); return
		}
		body, _ := readBody(r)
		jsonOk(w, ddnsUpdateGo(body))
		return
	}

	if urlPath == "/api/remote-access/request-ssl" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" {
			jsonError(w, 403, "Admin required"); return
		}
		body, _ := readBody(r)
		domain := bodyStr(body, "domain")
		email := bodyStr(body, "email")
		certMethod := bodyStr(body, "method")
		provider := bodyStr(body, "provider")
		dnsToken := bodyStr(body, "dnsToken")
		if domain == "" || email == "" {
			jsonError(w, 400, "Domain and email required"); return
		}

		cmd := fmt.Sprintf(`sudo certbot certonly --non-interactive --agree-tos -m "%s"`, email)
		if certMethod == "dns" && provider == "duckdns" {
			subdomain := strings.Replace(domain, ".duckdns.org", "", 1)
			hookDir := filepath.Join(configDir, "certbot-hooks")
			os.MkdirAll(hookDir, 0755)
			authHook := filepath.Join(hookDir, "duckdns-auth.sh")
			os.WriteFile(authHook, []byte(fmt.Sprintf("#!/bin/bash\ncurl -s \"https://www.duckdns.org/update?domains=%s&token=%s&txt=$CERTBOT_VALIDATION\" > /dev/null\nsleep 60\n", subdomain, dnsToken)), 0755)
			cleanupHook := filepath.Join(hookDir, "duckdns-cleanup.sh")
			os.WriteFile(cleanupHook, []byte(fmt.Sprintf("#!/bin/bash\ncurl -s \"https://www.duckdns.org/update?domains=%s&token=%s&txt=removed&clear=true\" > /dev/null\n", subdomain, dnsToken)), 0755)
			cmd += fmt.Sprintf(` --manual --preferred-challenges dns --manual-auth-hook "%s" --manual-cleanup-hook "%s" -d "%s"`, authHook, cleanupHook, domain)
		} else if certMethod == "standalone" {
			cmd += fmt.Sprintf(` --standalone -d "%s"`, domain)
		} else {
			cmd += fmt.Sprintf(` --webroot -w /var/www/html -d "%s"`, domain)
		}

		// Certbot needs long timeout (DNS propagation = 60s+)
		log, ok := runLong(cmd+" 2>&1", 180)
		if ok {
			cfg := readJSONConfig(remoteAccessConfigFile, map[string]interface{}{})
			ssl, _ := cfg["ssl"].(map[string]interface{})
			if ssl == nil { ssl = map[string]interface{}{} }
			ssl["enabled"] = true
			ssl["domain"] = domain
			cfg["ssl"] = ssl
			writeJSONConfig(remoteAccessConfigFile, cfg)
			jsonOk(w, map[string]interface{}{"ok": true, "log": log})
		} else {
			jsonError(w, 500, "Certificate request failed")
		}
		return
	}

	if urlPath == "/api/remote-access/enable-https" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" {
			jsonError(w, 403, "Admin required"); return
		}
		body, _ := readBody(r)
		domain := bodyStr(body, "domain")
		portF, _ := body["port"].(float64)
		httpsPort := int(portF)
		if httpsPort == 0 { httpsPort = 5009 }
		enabled, _ := body["enabled"].(bool)

		cfg := readJSONConfig(remoteAccessConfigFile, map[string]interface{}{})

		if enabled {
			certDir := fmt.Sprintf("/etc/letsencrypt/live/%s", domain)
			if _, err := os.Stat(certDir + "/fullchain.pem"); err != nil {
				jsonError(w, 400, fmt.Sprintf("No certificate found for %s", domain)); return
			}
			nginxConf := fmt.Sprintf(`server {
    listen %d ssl http2;
    listen [::]:%d ssl http2;
    server_name %s;
    ssl_certificate %s/fullchain.pem;
    ssl_certificate_key %s/privkey.pem;
    ssl_protocols TLSv1.2 TLSv1.3;
    ssl_ciphers HIGH:!aNULL:!MD5;
    add_header Strict-Transport-Security "max-age=31536000; includeSubDomains" always;
    location / {
        proxy_pass http://127.0.0.1:5000;
        proxy_set_header Host $host;
        proxy_set_header X-Real-IP $remote_addr;
        proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
        proxy_set_header X-Forwarded-Proto $scheme;
        proxy_http_version 1.1;
        proxy_set_header Upgrade $http_upgrade;
        proxy_set_header Connection "upgrade";
        proxy_buffering off;
    }
}`, httpsPort, httpsPort, domain, certDir, certDir)
			os.WriteFile("/etc/nginx/sites-available/nimbusos-https.conf", []byte(nginxConf), 0644)
			run("ln -sf /etc/nginx/sites-available/nimbusos-https.conf /etc/nginx/sites-enabled/nimbusos-https.conf")
			run(fmt.Sprintf("sudo ufw allow %d/tcp 2>/dev/null", httpsPort))
			run("sudo nginx -t 2>/dev/null && sudo systemctl reload nginx")
			https := map[string]interface{}{"enabled": true, "port": httpsPort}
			cfg["https"] = https
			writeJSONConfig(remoteAccessConfigFile, cfg)
			jsonOk(w, map[string]interface{}{"ok": true, "message": fmt.Sprintf("HTTPS enabled on port %d", httpsPort)})
		} else {
			run("rm -f /etc/nginx/sites-enabled/nimbusos-https.conf")
			run("sudo systemctl reload nginx")
			cfg["https"] = map[string]interface{}{"enabled": false, "port": httpsPort}
			writeJSONConfig(remoteAccessConfigFile, cfg)
			jsonOk(w, map[string]interface{}{"ok": true, "message": "HTTPS disabled"})
		}
		return
	}

	jsonError(w, 404, "Not found")
}

func getRemoteAccessStatusGo(cfg map[string]interface{}) map[string]interface{} {
	result := map[string]interface{}{
		"ddns":  map[string]interface{}{"working": false, "externalIp": nil},
		"ssl":   map[string]interface{}{"valid": false},
		"https": map[string]interface{}{"running": false, "enabled": false},
	}
	extIp, _ := run("curl -fsSL --connect-timeout 5 https://api.ipify.org 2>/dev/null")
	ddnsConf, _ := cfg["ddns"].(map[string]interface{})
	if ddnsConf != nil {
		result["ddns"] = ddnsConf
		ddnsMap := result["ddns"].(map[string]interface{})
		ddnsMap["externalIp"] = strings.TrimSpace(extIp)
	}

	// Check SSL certificate on disk
	fullDomain := ""
	if ddnsConf != nil {
		if fd, ok := ddnsConf["fullDomain"].(string); ok && fd != "" {
			fullDomain = fd
		} else if d, ok := ddnsConf["domain"].(string); ok {
			fullDomain = d
		}
	}
	if sslConf, _ := cfg["ssl"].(map[string]interface{}); sslConf != nil {
		if d, ok := sslConf["domain"].(string); ok && d != "" {
			fullDomain = d
		}
	}
	if fullDomain != "" {
		certDir := fmt.Sprintf("/etc/letsencrypt/live/%s", fullDomain)
		certPath := certDir + "/fullchain.pem"
		if _, err := os.Stat(certPath); err == nil {
			// Cert exists — check expiry
			certInfo, _ := run(fmt.Sprintf("openssl x509 -in %s -noout -enddate 2>/dev/null", certPath))
			daysLeft := -1
			expiry := ""
			if certInfo != "" {
				reExpiry := regexp.MustCompile(`notAfter=(.+)`)
				if m := reExpiry.FindStringSubmatch(certInfo); m != nil {
					expiry = strings.TrimSpace(m[1])
					if t, err := time.Parse("Jan  2 15:04:05 2006 MST", expiry); err == nil {
						daysLeft = int(time.Until(t).Hours() / 24)
					} else if t, err := time.Parse("Jan 2 15:04:05 2006 MST", expiry); err == nil {
						daysLeft = int(time.Until(t).Hours() / 24)
					}
				}
			}
			result["ssl"] = map[string]interface{}{
				"valid":    daysLeft > 0,
				"domain":   fullDomain,
				"expiry":   expiry,
				"daysLeft": daysLeft,
				"certPath": certPath,
				"keyPath":  certDir + "/privkey.pem",
			}
		}
	}

	// Check HTTPS
	httpsConf, _ := cfg["https"].(map[string]interface{})
	if httpsConf != nil {
		result["https"] = httpsConf
		if enabled, _ := httpsConf["enabled"].(bool); enabled {
			portF, _ := httpsConf["port"].(float64)
			port := int(portF)
			if port == 0 { port = 5009 }
			listening, _ := run(fmt.Sprintf("ss -tlnp 2>/dev/null | grep ':%d '", port))
			httpsMap := result["https"].(map[string]interface{})
			httpsMap["running"] = listening != ""
		}
	}

	// Local IP
	if lip, ok := run("hostname -I 2>/dev/null | awk '{print $1}'"); ok {
		result["localIp"] = strings.TrimSpace(lip)
	}
	result["nimbusPort"] = 5000
	return result
}

// ═══════════════════════════════════
// SSH
// ═══════════════════════════════════

func handleSshRoutes(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil { return }
	switch {
	case r.URL.Path == "/api/ssh/status" && r.Method == "GET":
		running, _ := run("systemctl is-active sshd 2>/dev/null || systemctl is-active ssh 2>/dev/null")
		version, _ := run("ssh -V 2>&1 | head -1")
		jsonOk(w, map[string]interface{}{"running": strings.TrimSpace(running) == "active", "version": version})
	case r.URL.Path == "/api/ssh/start" && r.Method == "POST":
		run("sudo systemctl enable ssh sshd 2>/dev/null; sudo systemctl start sshd 2>/dev/null || sudo systemctl start ssh 2>/dev/null")
		jsonOk(w, map[string]interface{}{"ok": true})
	case r.URL.Path == "/api/ssh/stop" && r.Method == "POST":
		run("sudo systemctl stop sshd ssh 2>/dev/null; sudo systemctl disable ssh sshd 2>/dev/null")
		jsonOk(w, map[string]interface{}{"ok": true})
	default:
		jsonError(w, 404, "Not found")
	}
}

// ═══════════════════════════════════
// FTP
// ═══════════════════════════════════

func handleFtpRoutes(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil { return }
	switch {
	case r.URL.Path == "/api/ftp/status" && r.Method == "GET":
		_, installed := run("which vsftpd 2>/dev/null || test -x /usr/sbin/vsftpd && echo yes")
		running1, _ := run("systemctl is-active vsftpd 2>/dev/null")
		running := strings.TrimSpace(running1) == "active"
		jsonOk(w, map[string]interface{}{"installed": installed, "running": running})
	case r.URL.Path == "/api/ftp/start" && r.Method == "POST":
		run("sudo systemctl enable vsftpd 2>/dev/null; sudo systemctl start vsftpd 2>/dev/null")
		jsonOk(w, map[string]interface{}{"ok": true})
	case r.URL.Path == "/api/ftp/stop" && r.Method == "POST":
		run("sudo systemctl stop vsftpd 2>/dev/null; sudo systemctl disable vsftpd 2>/dev/null")
		jsonOk(w, map[string]interface{}{"ok": true})
	default:
		jsonError(w, 404, "Not found")
	}
}

// ═══════════════════════════════════
// NFS
// ═══════════════════════════════════

func handleNfsRoutes(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil { return }
	switch {
	case r.URL.Path == "/api/nfs/status" && r.Method == "GET":
		_, installed := run("dpkg -l nfs-kernel-server 2>/dev/null | grep -q '^ii' && echo yes")
		running1, _ := run("systemctl is-active nfs-server 2>/dev/null")
		running := strings.TrimSpace(running1) == "active"
		exports := readFileStr("/etc/exports")
		jsonOk(w, map[string]interface{}{"installed": installed, "running": running, "exports": exports})
	case r.URL.Path == "/api/nfs/start" && r.Method == "POST":
		run("sudo systemctl enable nfs-server 2>/dev/null; sudo systemctl start nfs-server 2>/dev/null")
		jsonOk(w, map[string]interface{}{"ok": true})
	case r.URL.Path == "/api/nfs/stop" && r.Method == "POST":
		run("sudo systemctl stop nfs-server 2>/dev/null; sudo systemctl disable nfs-server 2>/dev/null")
		jsonOk(w, map[string]interface{}{"ok": true})
	default:
		jsonError(w, 404, "Not found")
	}
}

// ═══════════════════════════════════
// DNS
// ═══════════════════════════════════

func handleDnsRoutes(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil { return }
	if r.URL.Path == "/api/dns/status" && r.Method == "GET" {
		servers := []string{}
		resolv := readFileStr("/etc/resolv.conf")
		for _, line := range strings.Split(resolv, "\n") {
			if strings.HasPrefix(strings.TrimSpace(line), "nameserver") {
				parts := strings.Fields(line)
				if len(parts) >= 2 { servers = append(servers, parts[1]) }
			}
		}
		jsonOk(w, map[string]interface{}{"servers": servers})
		return
	}
	jsonError(w, 404, "Not found")
}

// ═══════════════════════════════════
// Certificates
// ═══════════════════════════════════

func handleCertsRoutes(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil { return }
	urlPath := r.URL.Path
	method := r.Method

	if urlPath == "/api/certs/status" && method == "GET" {
		_, certbotInstalled := run("which certbot 2>/dev/null")
		certs := []interface{}{}
		if certList, ok := run("sudo certbot certificates 2>/dev/null"); ok {
			// Parse certbot output
			_ = certList // simplified — return basic info
		}
		jsonOk(w, map[string]interface{}{"certbotInstalled": certbotInstalled, "certificates": certs})
		return
	}

	if urlPath == "/api/certs/request" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" { jsonError(w, 403, "Admin required"); return }
		body, _ := readBody(r)
		domain := bodyStr(body, "domain")
		email := bodyStr(body, "email")
		certMethod := bodyStr(body, "method")
		cmd := fmt.Sprintf(`sudo certbot certonly --non-interactive --agree-tos -m "%s"`, email)
		if certMethod == "standalone" {
			cmd += fmt.Sprintf(` --standalone -d "%s"`, domain)
		} else {
			cmd += fmt.Sprintf(` --webroot -w /var/www/html -d "%s"`, domain)
		}
		log, ok := run(cmd + " 2>&1")
		if ok { jsonOk(w, map[string]interface{}{"ok": true, "log": log}) } else { jsonError(w, 500, "Certificate request failed") }
		return
	}

	if urlPath == "/api/certs/renew" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" { jsonError(w, 403, "Admin required"); return }
		body, _ := readBody(r)
		domain := bodyStr(body, "domain")
		log, _ := run(fmt.Sprintf(`sudo certbot renew --cert-name "%s" --force-renewal 2>&1`, domain))
		jsonOk(w, map[string]interface{}{"ok": true, "log": log})
		return
	}

	if urlPath == "/api/certs/delete" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" { jsonError(w, 403, "Admin required"); return }
		body, _ := readBody(r)
		domain := bodyStr(body, "domain")
		run(fmt.Sprintf(`sudo certbot delete --cert-name "%s" --non-interactive 2>/dev/null`, domain))
		jsonOk(w, map[string]interface{}{"ok": true})
		return
	}

	jsonError(w, 404, "Not found")
}

// ═══════════════════════════════════
// Proxy (nginx reverse proxy rules)
// ═══════════════════════════════════

func handleProxyRoutes(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil { return }
	urlPath := r.URL.Path
	method := r.Method

	if urlPath == "/api/proxy/status" && method == "GET" {
		_, installed := run("which nginx 2>/dev/null")
		running1, _ := run("systemctl is-active nginx 2>/dev/null")
		running := strings.TrimSpace(running1) == "active"
		var rules []interface{}
		if data, err := os.ReadFile(proxyConfigFile); err == nil { json.Unmarshal(data, &rules) }
		if rules == nil { rules = []interface{}{} }
		jsonOk(w, map[string]interface{}{"installed": installed, "running": running, "rules": rules})
		return
	}

	if urlPath == "/api/proxy/rules" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" { jsonError(w, 403, "Admin required"); return }
		body, _ := readBody(r)
		rules, _ := body["rules"].([]interface{})
		writeJSONConfig(proxyConfigFile, rules)
		run("sudo nginx -t 2>/dev/null && sudo systemctl reload nginx 2>/dev/null")
		jsonOk(w, map[string]interface{}{"ok": true})
		return
	}

	jsonError(w, 404, "Not found")
}

// ═══════════════════════════════════
// Portal (port config)
// ═══════════════════════════════════

func handlePortalRoutes(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil { return }
	if r.URL.Path == "/api/portal/status" && r.Method == "GET" {
		jsonOk(w, map[string]interface{}{"httpPort": 5000, "httpsEnabled": false})
		return
	}
	if r.URL.Path == "/api/portal/config" && r.Method == "POST" {
		if role, _ := session["role"].(string); role != "admin" { jsonError(w, 403, "Admin required"); return }
		body, _ := readBody(r)
		_ = body
		jsonOk(w, map[string]interface{}{"ok": true, "needsRestart": true})
		return
	}
	jsonError(w, 404, "Not found")
}

// ═══════════════════════════════════
// WebDAV
// ═══════════════════════════════════

func handleWebdavRoutes(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil { return }
	urlPath := r.URL.Path
	method := r.Method

	if urlPath == "/api/webdav/status" && method == "GET" {
		_, installed := run("which nginx 2>/dev/null")
		running := false
		if _, err := os.Stat("/etc/nginx/sites-enabled/nimbusos-webdav.conf"); err == nil { running = true }
		jsonOk(w, map[string]interface{}{"installed": installed, "running": running})
		return
	}
	if urlPath == "/api/webdav/start" && method == "POST" {
		run("sudo ln -sf /etc/nginx/sites-available/nimbusos-webdav.conf /etc/nginx/sites-enabled/")
		run("sudo nginx -t 2>/dev/null && sudo systemctl reload nginx 2>/dev/null")
		jsonOk(w, map[string]interface{}{"ok": true}); return
	}
	if urlPath == "/api/webdav/stop" && method == "POST" {
		run("sudo rm -f /etc/nginx/sites-enabled/nimbusos-webdav.conf")
		run("sudo nginx -t 2>/dev/null && sudo systemctl reload nginx 2>/dev/null")
		jsonOk(w, map[string]interface{}{"ok": true}); return
	}
	jsonError(w, 404, "Not found")
}

// ═══════════════════════════════════
// SMB / Samba
// ═══════════════════════════════════

func handleSmbRoutes(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil { return }
	urlPath := r.URL.Path
	method := r.Method

	if urlPath == "/api/smb/status" && method == "GET" {
		_, installed := run("which smbd 2>/dev/null || test -x /usr/sbin/smbd && echo yes")
		running1, _ := run("systemctl is-active smbd 2>/dev/null")
		running := strings.TrimSpace(running1) == "active"
		version, _ := run("smbd --version 2>/dev/null")
		config := readJSONConfig(smbConfigFile, map[string]interface{}{"workgroup": "WORKGROUP", "serverString": "NimOS NAS"})
		jsonOk(w, map[string]interface{}{"installed": installed, "running": running, "version": version, "config": config, "port": 445})
		return
	}

	if urlPath == "/api/smb/config" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" { jsonError(w, 403, "Admin required"); return }
		body, _ := readBody(r)
		current := readJSONConfig(smbConfigFile, map[string]interface{}{})
		for k, v := range body { current[k] = v }
		writeJSONConfig(smbConfigFile, current)
		jsonOk(w, map[string]interface{}{"ok": true, "config": current}); return
	}

	if urlPath == "/api/smb/start" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" { jsonError(w, 403, "Admin required"); return }
		run("sudo systemctl enable smbd nmbd 2>/dev/null; sudo systemctl start smbd nmbd 2>/dev/null")
		jsonOk(w, map[string]interface{}{"ok": true}); return
	}

	if urlPath == "/api/smb/stop" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" { jsonError(w, 403, "Admin required"); return }
		run("sudo systemctl stop smbd nmbd 2>/dev/null; sudo systemctl disable smbd nmbd 2>/dev/null")
		jsonOk(w, map[string]interface{}{"ok": true}); return
	}

	if urlPath == "/api/smb/restart" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" { jsonError(w, 403, "Admin required"); return }
		run("sudo systemctl restart smbd nmbd 2>/dev/null")
		jsonOk(w, map[string]interface{}{"ok": true}); return
	}

	if urlPath == "/api/smb/apply" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" { jsonError(w, 403, "Admin required"); return }
		run("sudo smbcontrol all reload-config 2>/dev/null")
		jsonOk(w, map[string]interface{}{"ok": true}); return
	}

	if urlPath == "/api/smb/set-password" && method == "POST" {
		if role, _ := session["role"].(string); role != "admin" { jsonError(w, 403, "Admin required"); return }
		body, _ := readBody(r)
		username := bodyStr(body, "username")
		password := bodyStr(body, "password")
		if username == "" || password == "" { jsonError(w, 400, "Username and password required"); return }
		handleOp(Request{Op: "user.set_smb_password", Username: username, Password: password})
		jsonOk(w, map[string]interface{}{"ok": true}); return
	}

	// PUT /api/smb/share/:name
	reSmbShare := regexp.MustCompile(`^/api/smb/share/([a-zA-Z0-9_-]+)$`)
	if m := reSmbShare.FindStringSubmatch(urlPath); m != nil && method == "PUT" {
		if role, _ := session["role"].(string); role != "admin" { jsonError(w, 403, "Admin required"); return }
		// Toggle SMB on share — simplified, would need share update
		jsonOk(w, map[string]interface{}{"ok": true, "name": m[1]}); return
	}

	jsonError(w, 404, "Not found")
}

// ═══════════════════════════════════
// Firewall (GET endpoints)
// ═══════════════════════════════════

func handleFirewallRoutes(w http.ResponseWriter, r *http.Request) {
	session := requireAdmin(w, r)
	if session == nil { return }
	urlPath := r.URL.Path

	if urlPath == "/api/firewall" || urlPath == "/api/firewall/scan" {
		jsonOk(w, getFirewallScanGo()); return
	}
	if urlPath == "/api/firewall/rules" {
		jsonOk(w, getFirewallRulesGo()); return
	}
	if urlPath == "/api/firewall/ports" {
		jsonOk(w, getListeningPortsGo()); return
	}
	jsonError(w, 404, "Not found")
}

func getFirewallRulesGo() map[string]interface{} {
	ufwOut, _ := run("ufw status numbered 2>/dev/null")
	return map[string]interface{}{"rules": ufwOut, "active": strings.Contains(ufwOut, "Status: active")}
}

func getListeningPortsGo() map[string]interface{} {
	out, _ := run("ss -tlnp 2>/dev/null")
	return map[string]interface{}{"ports": out}
}

func getFirewallScanGo() map[string]interface{} {
	rules := getFirewallRulesGo()
	ports := getListeningPortsGo()
	return map[string]interface{}{"firewall": rules, "listening": ports}
}

// ═══════════════════════════════════
// Register all network routes
// ═══════════════════════════════════

func registerNetworkRoutes(mux *http.ServeMux) {
	mux.HandleFunc("/api/ddns/", handleDdnsRoutes)
	mux.HandleFunc("/api/remote-access/", handleRemoteAccessRoutes)
	mux.HandleFunc("/api/ssh/", handleSshRoutes)
	mux.HandleFunc("/api/ftp/", handleFtpRoutes)
	mux.HandleFunc("/api/nfs/", handleNfsRoutes)
	mux.HandleFunc("/api/dns/", handleDnsRoutes)
	mux.HandleFunc("/api/certs/", handleCertsRoutes)
	mux.HandleFunc("/api/proxy/", handleProxyRoutes)
	mux.HandleFunc("/api/portal/", handlePortalRoutes)
	mux.HandleFunc("/api/webdav/", handleWebdavRoutes)
	mux.HandleFunc("/api/smb/", handleSmbRoutes)
	mux.HandleFunc("/api/firewall", handleFirewallRoutes)
	mux.HandleFunc("/api/firewall/", handleFirewallRoutes)
	mux.HandleFunc("/api/vms/", handleVMsRoutes)
}
