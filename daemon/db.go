package main

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

// ═══════════════════════════════════
// Database
// ═══════════════════════════════════

var db *sql.DB

const dbPath = "/var/lib/nimbusos/config/nimos.db"

func openDB() error {
	dir := filepath.Dir(dbPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return fmt.Errorf("cannot create db directory: %v", err)
	}

	var err error
	db, err = sql.Open("sqlite", dbPath+"?_journal_mode=WAL&_busy_timeout=5000&_foreign_keys=ON")
	if err != nil {
		return fmt.Errorf("cannot open database: %v", err)
	}

	// Allow multiple readers, WAL handles concurrency
	db.SetMaxOpenConns(4)
	db.SetMaxIdleConns(2)

	if err := createTables(); err != nil {
		return fmt.Errorf("cannot create tables: %v", err)
	}

	return nil
}

func createTables() error {
	schema := `
	CREATE TABLE IF NOT EXISTS users (
		username     TEXT PRIMARY KEY,
		password     TEXT NOT NULL,
		role         TEXT NOT NULL DEFAULT 'user',
		description  TEXT DEFAULT '',
		totp_secret  TEXT DEFAULT '',
		totp_enabled INTEGER DEFAULT 0,
		backup_codes TEXT DEFAULT '',
		created_at   TEXT NOT NULL,
		updated_at   TEXT
	);

	CREATE TABLE IF NOT EXISTS sessions (
		token        TEXT PRIMARY KEY,
		username     TEXT NOT NULL,
		role         TEXT NOT NULL,
		created_at   INTEGER NOT NULL,
		expires_at   INTEGER NOT NULL,
		ip           TEXT DEFAULT '',
		FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS shares (
		name         TEXT PRIMARY KEY,
		display_name TEXT NOT NULL,
		description  TEXT DEFAULT '',
		path         TEXT NOT NULL UNIQUE,
		volume       TEXT NOT NULL,
		pool         TEXT NOT NULL,
		recycle_bin  INTEGER DEFAULT 1,
		created_by   TEXT NOT NULL,
		created_at   TEXT NOT NULL
	);

	CREATE TABLE IF NOT EXISTS share_permissions (
		share_name   TEXT NOT NULL,
		username     TEXT NOT NULL,
		permission   TEXT NOT NULL,
		PRIMARY KEY (share_name, username),
		FOREIGN KEY (share_name) REFERENCES shares(name) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS app_permissions (
		share_name   TEXT NOT NULL,
		app_id       TEXT NOT NULL,
		uid          INTEGER NOT NULL,
		permission   TEXT NOT NULL,
		PRIMARY KEY (share_name, app_id),
		FOREIGN KEY (share_name) REFERENCES shares(name) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS user_app_access (
		username     TEXT NOT NULL,
		app_id       TEXT NOT NULL,
		permission   TEXT NOT NULL DEFAULT 'use',
		granted_by   TEXT NOT NULL,
		granted_at   TEXT NOT NULL,
		PRIMARY KEY (username, app_id),
		FOREIGN KEY (username) REFERENCES users(username) ON DELETE CASCADE
	);

	CREATE TABLE IF NOT EXISTS preferences (
		username     TEXT NOT NULL,
		key          TEXT NOT NULL,
		value        TEXT NOT NULL,
		PRIMARY KEY (username, key)
	);

	CREATE INDEX IF NOT EXISTS idx_sessions_username ON sessions(username);
	CREATE INDEX IF NOT EXISTS idx_sessions_expires ON sessions(expires_at);
	CREATE INDEX IF NOT EXISTS idx_share_perms_user ON share_permissions(username);
	CREATE INDEX IF NOT EXISTS idx_preferences_user ON preferences(username);
	`
	_, err := db.Exec(schema)
	if err != nil {
		return err
	}

	// Migration: add backup_codes column if it doesn't exist
	db.Exec(`ALTER TABLE users ADD COLUMN backup_codes TEXT DEFAULT ''`)

	return nil
}

// ═══════════════════════════════════
// Migration from JSON files
// ═══════════════════════════════════

type jsonUser struct {
	Username    string `json:"username"`
	Password    string `json:"password"`
	Role        string `json:"role"`
	Description string `json:"description"`
	TotpSecret  string `json:"totpSecret"`
	TotpEnabled bool   `json:"totpEnabled"`
	Created     string `json:"created"`
}

type jsonShare struct {
	Name           string            `json:"name"`
	DisplayName    string            `json:"displayName"`
	Description    string            `json:"description"`
	Path           string            `json:"path"`
	Volume         string            `json:"volume"`
	Pool           string            `json:"pool"`
	RecycleBin     bool              `json:"recycleBin"`
	CreatedBy      string            `json:"createdBy"`
	Created        string            `json:"created"`
	Permissions    map[string]string `json:"permissions"`
	AppPermissions []json.RawMessage `json:"appPermissions"`
}

type jsonSession struct {
	Username string `json:"username"`
	Role     string `json:"role"`
	Created  int64  `json:"created"`
}

func migrateFromJSON() {
	migratedAny := false

	// Migrate users
	if data, err := os.ReadFile(usersFile); err == nil {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM users").Scan(&count)
		if count == 0 {
			var users []jsonUser
			if err := json.Unmarshal(data, &users); err == nil {
				tx, _ := db.Begin()
				for _, u := range users {
					totpEnabled := 0
					if u.TotpEnabled {
						totpEnabled = 1
					}
					tx.Exec(`INSERT OR IGNORE INTO users (username, password, role, description, totp_secret, totp_enabled, created_at)
						VALUES (?, ?, ?, ?, ?, ?, ?)`,
						u.Username, u.Password, u.Role, u.Description, u.TotpSecret, totpEnabled, u.Created)
				}
				tx.Commit()
				logMsg("  migration: imported %d users from JSON", len(users))
				migratedAny = true
			}
		}
	}

	// Migrate shares
	if data, err := os.ReadFile(sharesFile); err == nil {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM shares").Scan(&count)
		if count == 0 {
			var shares []jsonShare
			if err := json.Unmarshal(data, &shares); err == nil {
				tx, _ := db.Begin()
				for _, s := range shares {
					recycleBin := 0
					if s.RecycleBin {
						recycleBin = 1
					}
					tx.Exec(`INSERT OR IGNORE INTO shares (name, display_name, description, path, volume, pool, recycle_bin, created_by, created_at)
						VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
						s.Name, s.DisplayName, s.Description, s.Path, s.Volume, s.Pool, recycleBin, s.CreatedBy, s.Created)

					for username, perm := range s.Permissions {
						tx.Exec(`INSERT OR IGNORE INTO share_permissions (share_name, username, permission)
							VALUES (?, ?, ?)`, s.Name, username, perm)
					}
				}
				tx.Commit()
				logMsg("  migration: imported %d shares from JSON", len(shares))
				migratedAny = true
			}
		}
	}

	// Migrate sessions
	sessionsFile := filepath.Join(filepath.Dir(usersFile), "sessions.json")
	if data, err := os.ReadFile(sessionsFile); err == nil {
		var count int
		db.QueryRow("SELECT COUNT(*) FROM sessions").Scan(&count)
		if count == 0 {
			var sessions map[string]jsonSession
			if err := json.Unmarshal(data, &sessions); err == nil {
				tx, _ := db.Begin()
				imported := 0
				now := time.Now().UnixMilli()
				for token, s := range sessions {
					expiresAt := s.Created + sessionExpiryMs
					if expiresAt > now {
						tx.Exec(`INSERT OR IGNORE INTO sessions (token, username, role, created_at, expires_at)
							VALUES (?, ?, ?, ?, ?)`, token, s.Username, s.Role, s.Created, expiresAt)
						imported++
					}
				}
				tx.Commit()
				logMsg("  migration: imported %d active sessions from JSON", imported)
				migratedAny = true
			}
		}
	}

	// Rename old JSON files — Node.js now reads from SQLite via daemon
	if migratedAny {
		for _, f := range []string{usersFile, sharesFile, sessionsFile} {
			if _, err := os.Stat(f); err == nil {
				os.Rename(f, f+".migrated")
			}
		}
		logMsg("  migration: JSON files renamed to .migrated")
	}
}

// ═══════════════════════════════════
// User operations
// ═══════════════════════════════════

func dbUsersList() ([]map[string]interface{}, error) {
	rows, err := db.Query(`SELECT username, role, description, totp_enabled, created_at FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var users []map[string]interface{}
	for rows.Next() {
		var username, role, desc, created string
		var totpEnabled int
		rows.Scan(&username, &role, &desc, &totpEnabled, &created)
		users = append(users, map[string]interface{}{
			"username":    username,
			"role":        role,
			"description": desc,
			"totpEnabled": totpEnabled == 1,
			"created":     created,
		})
	}
	if users == nil {
		users = []map[string]interface{}{}
	}
	return users, nil
}

func dbUsersGet(username string) (map[string]interface{}, error) {
	var pwd, role, desc, totpSecret, created string
	var backupCodesJSON string
	var totpEnabled int
	var updatedAt sql.NullString
	err := db.QueryRow(`SELECT password, role, description, totp_secret, totp_enabled, backup_codes, created_at, updated_at FROM users WHERE username = ?`, username).
		Scan(&pwd, &role, &desc, &totpSecret, &totpEnabled, &backupCodesJSON, &created, &updatedAt)
	if err != nil {
		return nil, fmt.Errorf("user not found: %s", username)
	}

	result := map[string]interface{}{
		"username":    username,
		"password":    pwd,
		"role":        role,
		"description": desc,
		"totpSecret":  totpSecret,
		"totpEnabled": totpEnabled == 1,
		"created":     created,
	}

	// Parse backup codes JSON array
	if backupCodesJSON != "" {
		var codes []interface{}
		if json.Unmarshal([]byte(backupCodesJSON), &codes) == nil {
			result["backupCodes"] = codes
		}
	}

	return result, nil
}

func dbUsersCreate(username, password, role, description string) error {
	_, err := db.Exec(`INSERT INTO users (username, password, role, description, created_at) VALUES (?, ?, ?, ?, ?)`,
		username, password, role, description, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func dbUsersUpdate(username string, fields map[string]interface{}) error {
	// Build dynamic update
	sets := []string{}
	args := []interface{}{}
	for k, v := range fields {
		col := ""
		switch k {
		case "password":
			col = "password"
		case "role":
			col = "role"
		case "description":
			col = "description"
		case "totpSecret":
			col = "totp_secret"
		case "totpEnabled":
			col = "totp_enabled"
		case "backupCodes":
			col = "backup_codes"
			// Serialize as JSON
			jsonData, _ := json.Marshal(v)
			v = string(jsonData)
		default:
			continue
		}
		sets = append(sets, col+" = ?")
		args = append(args, v)
	}
	if len(sets) == 0 {
		return nil
	}
	sets = append(sets, "updated_at = ?")
	args = append(args, time.Now().UTC().Format(time.RFC3339Nano))
	args = append(args, username)

	query := "UPDATE users SET " + joinStrings(sets, ", ") + " WHERE username = ?"
	_, err := db.Exec(query, args...)
	return err
}

func dbSessionsDeleteByUsername(username string) {
	db.Exec(`DELETE FROM sessions WHERE username = ?`, username)
}

func dbUsersDelete(username string) error {
	_, err := db.Exec(`DELETE FROM users WHERE username = ?`, username)
	return err
}

func dbUsersVerifyPassword(username string) (string, error) {
	var pwd string
	err := db.QueryRow(`SELECT password FROM users WHERE username = ?`, username).Scan(&pwd)
	if err != nil {
		return "", fmt.Errorf("user not found: %s", username)
	}
	return pwd, nil
}

// ═══════════════════════════════════
// Session operations
// ═══════════════════════════════════

const sessionExpiryMs int64 = 7 * 24 * 60 * 60 * 1000 // 7 days

func dbSessionCreate(token, username, role, ip string) error {
	now := time.Now().UnixMilli()
	expires := now + sessionExpiryMs
	_, err := db.Exec(`INSERT OR REPLACE INTO sessions (token, username, role, created_at, expires_at, ip) VALUES (?, ?, ?, ?, ?, ?)`,
		token, username, role, now, expires, ip)
	return err
}

func dbSessionGet(token string) (map[string]interface{}, error) {
	var username, role, ip string
	var createdAt, expiresAt int64
	err := db.QueryRow(`SELECT username, role, created_at, expires_at, ip FROM sessions WHERE token = ?`, token).
		Scan(&username, &role, &createdAt, &expiresAt, &ip)
	if err != nil {
		return nil, fmt.Errorf("session not found")
	}
	if time.Now().UnixMilli() > expiresAt {
		db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
		return nil, fmt.Errorf("session expired")
	}
	return map[string]interface{}{
		"username": username,
		"role":     role,
		"created":  createdAt,
		"expires":  expiresAt,
		"ip":       ip,
	}, nil
}

func dbSessionDelete(token string) error {
	_, err := db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func dbSessionCleanup() int64 {
	now := time.Now().UnixMilli()
	result, _ := db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, now)
	n, _ := result.RowsAffected()
	return n
}

// ═══════════════════════════════════
// Share operations (data layer)
// ═══════════════════════════════════

func dbSharesList() ([]map[string]interface{}, error) {
	rows, err := db.Query(`SELECT name, display_name, description, path, volume, pool, recycle_bin, created_by, created_at FROM shares ORDER BY created_at`)
	if err != nil {
		return nil, err
	}

	// Collect all share names first, then close rows before subqueries
	type shareRow struct {
		name, displayName, desc, path, volume, pool, createdBy, created string
		recycleBin int
	}
	var shareRows []shareRow
	for rows.Next() {
		var s shareRow
		rows.Scan(&s.name, &s.displayName, &s.desc, &s.path, &s.volume, &s.pool, &s.recycleBin, &s.createdBy, &s.created)
		shareRows = append(shareRows, s)
	}
	rows.Close()

	// Now build results with subqueries (rows are closed, no deadlock)
	var shares []map[string]interface{}
	for _, s := range shareRows {
		perms := map[string]string{}
		prows, _ := db.Query(`SELECT username, permission FROM share_permissions WHERE share_name = ?`, s.name)
		if prows != nil {
			for prows.Next() {
				var u, p string
				prows.Scan(&u, &p)
				perms[u] = p
			}
			prows.Close()
		}

		var appPerms []map[string]interface{}
		arows, _ := db.Query(`SELECT app_id, uid, permission FROM app_permissions WHERE share_name = ?`, s.name)
		if arows != nil {
			for arows.Next() {
				var appId, perm string
				var uid int
				arows.Scan(&appId, &uid, &perm)
				appPerms = append(appPerms, map[string]interface{}{"appId": appId, "uid": uid, "permission": perm})
			}
			arows.Close()
		}
		if appPerms == nil {
			appPerms = []map[string]interface{}{}
		}

		shares = append(shares, map[string]interface{}{
			"name":           s.name,
			"displayName":    s.displayName,
			"description":    s.desc,
			"path":           s.path,
			"volume":         s.volume,
			"pool":           s.pool,
			"recycleBin":     s.recycleBin == 1,
			"createdBy":      s.createdBy,
			"created":        s.created,
			"permissions":    perms,
			"appPermissions": appPerms,
		})
	}
	if shares == nil {
		shares = []map[string]interface{}{}
	}
	return shares, nil
}

func dbSharesGet(name string) (map[string]interface{}, error) {
	shares, err := dbSharesList()
	if err != nil {
		return nil, err
	}
	for _, s := range shares {
		if s["name"] == name {
			return s, nil
		}
	}
	return nil, fmt.Errorf("share not found: %s", name)
}

func dbSharesCreate(name, displayName, desc, path, volume, pool, createdBy string) error {
	_, err := db.Exec(`INSERT INTO shares (name, display_name, description, path, volume, pool, created_by, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		name, displayName, desc, path, volume, pool, createdBy, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

func dbSharesUpdate(name string, fields map[string]interface{}) error {
	sets := []string{}
	args := []interface{}{}
	for k, v := range fields {
		col := ""
		switch k {
		case "description":
			col = "description"
		case "recycleBin":
			col = "recycle_bin"
		default:
			continue
		}
		sets = append(sets, col+" = ?")
		args = append(args, v)
	}
	if len(sets) == 0 {
		return nil
	}
	args = append(args, name)
	query := "UPDATE shares SET " + joinStrings(sets, ", ") + " WHERE name = ?"
	_, err := db.Exec(query, args...)
	return err
}

func dbSharesDelete(name string) error {
	_, err := db.Exec(`DELETE FROM shares WHERE name = ?`, name)
	return err
}

func dbShareSetPermission(shareName, username, permission string) error {
	if permission == "none" || permission == "" {
		_, err := db.Exec(`DELETE FROM share_permissions WHERE share_name = ? AND username = ?`, shareName, username)
		return err
	}
	_, err := db.Exec(`INSERT OR REPLACE INTO share_permissions (share_name, username, permission) VALUES (?, ?, ?)`,
		shareName, username, permission)
	return err
}

func dbShareSetAppPermission(shareName, appId string, uid int, permission string) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO app_permissions (share_name, app_id, uid, permission) VALUES (?, ?, ?, ?)`,
		shareName, appId, uid, permission)
	return err
}

func dbShareRemoveAppPermission(shareName, appId string) error {
	_, err := db.Exec(`DELETE FROM app_permissions WHERE share_name = ? AND app_id = ?`, shareName, appId)
	return err
}

// ═══════════════════════════════════
// Preferences operations
// ═══════════════════════════════════

func dbPrefsGet(username string) (map[string]interface{}, error) {
	rows, err := db.Query(`SELECT key, value FROM preferences WHERE username = ?`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	prefs := map[string]interface{}{}
	for rows.Next() {
		var key, value string
		rows.Scan(&key, &value)
		// Try to parse as JSON
		var parsed interface{}
		if json.Unmarshal([]byte(value), &parsed) == nil {
			prefs[key] = parsed
		} else {
			prefs[key] = value
		}
	}
	return prefs, nil
}

func dbPrefsSet(username, key, value string) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO preferences (username, key, value) VALUES (?, ?, ?)`,
		username, key, value)
	return err
}

func dbPrefsDelete(username, key string) error {
	_, err := db.Exec(`DELETE FROM preferences WHERE username = ? AND key = ?`, username, key)
	return err
}

// ═══════════════════════════════════
// User App Access
// ═══════════════════════════════════

// Apps that are always available to all authenticated users (no permission needed)
var publicApps = map[string]bool{
	"files":       true,
	"mediaplayer": true,
}

// Apps that are ALWAYS admin-only (cannot be delegated)
var adminOnlyApps = map[string]bool{
	"storage": true,
	"network": true,
}

// Check if a user has access to an app
// Admin always has access. Public apps are always accessible.
// For everything else, check user_app_access table.
func dbUserHasAppAccess(username, role, appId string) bool {
	if role == "admin" {
		return true
	}
	if publicApps[appId] {
		return true
	}
	if adminOnlyApps[appId] {
		return false
	}
	var count int
	err := db.QueryRow(`SELECT COUNT(*) FROM user_app_access WHERE username = ? AND app_id = ?`, username, appId).Scan(&count)
	if err != nil {
		return false
	}
	return count > 0
}

// Get permission level for a user+app ('use', 'manage', or '')
func dbUserGetAppPermission(username, role, appId string) string {
	if role == "admin" {
		return "manage"
	}
	if publicApps[appId] {
		return "use"
	}
	if adminOnlyApps[appId] {
		return ""
	}
	var perm string
	err := db.QueryRow(`SELECT permission FROM user_app_access WHERE username = ? AND app_id = ?`, username, appId).Scan(&perm)
	if err != nil {
		return ""
	}
	return perm
}

// List all app access for a user
func dbUserListAppAccess(username string) ([]map[string]interface{}, error) {
	rows, err := db.Query(`SELECT app_id, permission, granted_by, granted_at FROM user_app_access WHERE username = ? ORDER BY app_id`, username)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]interface{}
	for rows.Next() {
		var appId, perm, grantedBy, grantedAt string
		rows.Scan(&appId, &perm, &grantedBy, &grantedAt)
		result = append(result, map[string]interface{}{
			"appId":     appId,
			"permission": perm,
			"grantedBy": grantedBy,
			"grantedAt": grantedAt,
		})
	}
	if result == nil {
		result = []map[string]interface{}{}
	}
	return result, nil
}

// List all app access entries (for admin panel)
func dbAppAccessListAll() ([]map[string]interface{}, error) {
	rows, err := db.Query(`SELECT username, app_id, permission, granted_by, granted_at FROM user_app_access ORDER BY username, app_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var result []map[string]interface{}
	for rows.Next() {
		var username, appId, perm, grantedBy, grantedAt string
		rows.Scan(&username, &appId, &perm, &grantedBy, &grantedAt)
		result = append(result, map[string]interface{}{
			"username":   username,
			"appId":      appId,
			"permission": perm,
			"grantedBy":  grantedBy,
			"grantedAt":  grantedAt,
		})
	}
	if result == nil {
		result = []map[string]interface{}{}
	}
	return result, nil
}

// Grant app access
func dbAppAccessGrant(username, appId, permission, grantedBy string) error {
	_, err := db.Exec(`INSERT OR REPLACE INTO user_app_access (username, app_id, permission, granted_by, granted_at) VALUES (?, ?, ?, ?, ?)`,
		username, appId, permission, grantedBy, time.Now().UTC().Format(time.RFC3339Nano))
	return err
}

// Revoke app access
func dbAppAccessRevoke(username, appId string) error {
	_, err := db.Exec(`DELETE FROM user_app_access WHERE username = ? AND app_id = ?`, username, appId)
	return err
}

// Revoke all app access for a user
func dbAppAccessRevokeAll(username string) error {
	_, err := db.Exec(`DELETE FROM user_app_access WHERE username = ?`, username)
	return err
}

// ═══════════════════════════════════
// Helpers
// ═══════════════════════════════════

func joinStrings(parts []string, sep string) string {
	result := ""
	for i, p := range parts {
		if i > 0 {
			result += sep
		}
		result += p
	}
	return result
}
