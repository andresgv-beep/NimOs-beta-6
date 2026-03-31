package main

// ═══════════════════════════════════════════════════════════════════════════════
// NimOS Models — Typed structs for database entities
//
// Each struct has a ToMap() method for backward compatibility with code
// that still uses map[string]interface{}. Migrate consumers one by one,
// then remove ToMap() when no longer needed.
// ═══════════════════════════════════════════════════════════════════════════════

// ─── User ────────────────────────────────────────────────────────────────────

type DBUser struct {
	Username    string
	Password    string
	Role        string
	Description string
	TotpSecret  string
	TotpEnabled bool
	BackupCodes []interface{}
	CreatedAt   string
	UpdatedAt   string
}

func (u DBUser) ToMap() map[string]interface{} {
	m := map[string]interface{}{
		"username":    u.Username,
		"password":    u.Password,
		"role":        u.Role,
		"description": u.Description,
		"totpSecret":  u.TotpSecret,
		"totpEnabled": u.TotpEnabled,
		"created":     u.CreatedAt,
	}
	if u.BackupCodes != nil {
		m["backupCodes"] = u.BackupCodes
	}
	return m
}

// DBUserSummary is the lightweight version returned by list operations
type DBUserSummary struct {
	Username    string
	Role        string
	Description string
	TotpEnabled bool
	CreatedAt   string
}

func (u DBUserSummary) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"username":    u.Username,
		"role":        u.Role,
		"description": u.Description,
		"totpEnabled": u.TotpEnabled,
		"created":     u.CreatedAt,
	}
}

// ─── Session ─────────────────────────────────────────────────────────────────

type DBSession struct {
	Username  string
	Role      string
	CreatedAt int64
	ExpiresAt int64
	IP        string
}

func (s DBSession) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"username": s.Username,
		"role":     s.Role,
		"created":  s.CreatedAt,
		"expires":  s.ExpiresAt,
		"ip":       s.IP,
	}
}

// ─── Share ────────────────────────────────────────────────────────────────────

type DBShare struct {
	Name           string
	DisplayName    string
	Description    string
	Path           string
	Volume         string
	Pool           string
	RecycleBin     bool
	CreatedBy      string
	CreatedAt      string
	Permissions    map[string]string
	AppPermissions []AppPermission
}

type AppPermission struct {
	AppId      string
	Uid        int
	Permission string
}

func (s DBShare) ToMap() map[string]interface{} {
	appPerms := make([]map[string]interface{}, 0, len(s.AppPermissions))
	for _, ap := range s.AppPermissions {
		appPerms = append(appPerms, map[string]interface{}{
			"appId":      ap.AppId,
			"uid":        ap.Uid,
			"permission": ap.Permission,
		})
	}

	return map[string]interface{}{
		"name":           s.Name,
		"displayName":    s.DisplayName,
		"description":    s.Description,
		"path":           s.Path,
		"volume":         s.Volume,
		"pool":           s.Pool,
		"recycleBin":     s.RecycleBin,
		"createdBy":      s.CreatedBy,
		"created":        s.CreatedAt,
		"permissions":    s.Permissions,
		"appPermissions": appPerms,
	}
}

// ShareView is the enriched version of DBShare with runtime data from the filesystem.
// Built by buildShareViews() — never mutated after construction.
type ShareView struct {
	DBShare
	PoolType   string
	MountPoint string
	Quota      int64
	Used       int64
	Available  int64
	FileStats  map[string]int64
}

func (v ShareView) ToMap() map[string]interface{} {
	m := v.DBShare.ToMap()
	m["poolType"] = v.PoolType
	m["mountPoint"] = v.MountPoint
	m["quota"] = v.Quota
	m["used"] = v.Used
	m["available"] = v.Available
	m["fileStats"] = v.FileStats
	return m
}

// ─── App Access Grant ────────────────────────────────────────────────────────

type DBAppGrant struct {
	Username   string
	AppId      string
	Permission string
	GrantedBy  string
	GrantedAt  string
}

func (g DBAppGrant) ToMap() map[string]interface{} {
	return map[string]interface{}{
		"username":   g.Username,
		"appId":      g.AppId,
		"permission": g.Permission,
		"grantedBy":  g.GrantedBy,
		"grantedAt":  g.GrantedAt,
	}
}
