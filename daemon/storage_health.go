package main

// ═══════════════════════════════════════════════════════════════════════════════
// NimOS Storage Health — Diagnostic Layer + State Reducer
//
// Architecture:
//   Raw data (zpool status, btrfs stats, SMART, lsblk)
//       → CollectDiagnostics()  → []Diagnostic  (all signals, no priority)
//       → ComputePoolHealth()   → PoolHealth     (reduced, deterministic)
//       → API response          → poolHealth in each pool
//
// This file contains:
//   - parseZpoolDiskStatus()     — parse zpool status per-disk state + IO errors
//   - parseBtrfsDeviceStats()    — parse btrfs device stats per-disk IO errors
//   - getSmartDetailsForDisk()   — extract SmartDetails from getDiskSmart()
//   - CollectDiagnostics()       — generate all diagnostic signals
//   - ComputePoolHealth()        — reduce diagnostics to final state
//   - enrichDisksComplete()      — full per-disk enrichment (replaces enrichDisksWithSmart)
// ═══════════════════════════════════════════════════════════════════════════════

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// ─── parseZpoolDiskStatus ────────────────────────────────────────────────────
//
// Parses `zpool status -P <pool>` to extract per-disk state and IO error counts.
// Uses -P flag for full paths to avoid alias ambiguity.
//
// Output format from zpool status:
//   NAME                                 STATE     READ WRITE CKSUM
//   nimos-valume1                        ONLINE       0     0     0
//     mirror-0                           ONLINE       0     0     0
//       /dev/sda1                        ONLINE       0     0     0
//       /dev/sdb1                        ONLINE       0     0     0
//
// Returns map[diskName]DiskStatus where diskName is "sda", "sdb", etc.
// ─────────────────────────────────────────────────────────────────────────────

func parseZpoolDiskStatus(zpoolName string) (map[string]DiskStatus, error) {
	out, ok := run(fmt.Sprintf("zpool status -P %s 2>/dev/null", zpoolName))
	if !ok || out == "" {
		return nil, fmt.Errorf("cannot read zpool status for %s", zpoolName)
	}

	result := map[string]DiskStatus{}
	inConfig := false

	for _, line := range strings.Split(out, "\n") {
		trimmed := strings.TrimSpace(line)

		// Detect start of vdev table
		if strings.HasPrefix(trimmed, "NAME") && strings.Contains(trimmed, "STATE") {
			inConfig = true
			continue
		}
		if !inConfig || trimmed == "" {
			continue
		}
		// End of table
		if strings.HasPrefix(trimmed, "errors:") {
			break
		}

		fields := strings.Fields(trimmed)
		if len(fields) < 2 {
			continue
		}

		name := fields[0]
		state := fields[1]
		readErr, writeErr, cksumErr := 0, 0, 0
		if len(fields) >= 5 {
			readErr, _ = strconv.Atoi(fields[2])
			writeErr, _ = strconv.Atoi(fields[3])
			cksumErr, _ = strconv.Atoi(fields[4])
		}

		// Only interested in real disks (contain /dev/ or sdX or nvmeX patterns)
		diskName := extractDiskName(name)
		if diskName == "" {
			continue
		}

		result[diskName] = DiskStatus{
			State:          state,
			ReadErrors:     readErr,
			WriteErrors:    writeErr,
			ChecksumErrors: cksumErr,
		}
	}
	return result, nil
}

// extractDiskName extracts the base disk name from a zpool status entry.
// "/dev/sda1" → "sda", "sda1" → "sda", "/dev/nvme0n1p1" → "nvme0n1"
// Returns "" for vdev names (mirror-0, raidz1-0, pool name, etc.)
func extractDiskName(name string) string {
	// Strip /dev/ prefix
	name = strings.TrimPrefix(name, "/dev/")

	// Must start with sd, nvme, or vd to be a real disk
	isSd := strings.HasPrefix(name, "sd")
	isNvme := strings.HasPrefix(name, "nvme")
	isVd := strings.HasPrefix(name, "vd")

	if !isSd && !isNvme && !isVd {
		return ""
	}

	// Strip partition suffix
	if isNvme {
		// nvme0n1p1 → nvme0n1
		if idx := strings.LastIndex(name, "p"); idx > 0 {
			// Make sure what's after 'p' is a number (partition number)
			suffix := name[idx+1:]
			if _, err := strconv.Atoi(suffix); err == nil {
				return name[:idx]
			}
		}
		return name
	}

	// sda1 → sda, vdb2 → vdb
	// Strip trailing digits
	trimmed := strings.TrimRight(name, "0123456789")
	if trimmed == "" {
		return ""
	}
	return trimmed
}

// ─── parseBtrfsDeviceStats ───────────────────────────────────────────────────
//
// Parses `btrfs device stats <mountPoint>` to extract per-disk IO error counts.
//
// Output format:
//   [/dev/sda].write_io_errs    0
//   [/dev/sda].read_io_errs     0
//   [/dev/sda].flush_io_errs    0
//   [/dev/sda].corruption_errs  0
//   [/dev/sda].generation_errs  0
//
// Returns map[diskName]DiskStatus where diskName is "sda", etc.
// For BTRFS, State is always "" (BTRFS doesn't have per-device state like ZFS).
// ─────────────────────────────────────────────────────────────────────────────

func parseBtrfsDeviceStats(mountPoint string) (map[string]DiskStatus, error) {
	out, ok := run(fmt.Sprintf("btrfs device stats %s 2>/dev/null", mountPoint))
	if !ok || out == "" {
		return nil, fmt.Errorf("cannot read btrfs device stats for %s", mountPoint)
	}

	// Accumulate errors per device
	type devErrors struct {
		read       int
		write      int
		corruption int
		flush      int
		generation int
	}
	devMap := map[string]*devErrors{}

	for _, line := range strings.Split(out, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}

		// Parse "[/dev/sda].write_io_errs    0"
		// Find device name between [ and ]
		bracketStart := strings.Index(line, "[")
		bracketEnd := strings.Index(line, "]")
		if bracketStart < 0 || bracketEnd < 0 || bracketEnd <= bracketStart {
			continue
		}

		devPath := line[bracketStart+1 : bracketEnd]
		diskName := strings.TrimPrefix(devPath, "/dev/")
		// Strip partition number for BTRFS too (though BTRFS usually uses whole device)
		baseName := strings.TrimRight(diskName, "0123456789")
		if baseName == "" {
			continue
		}

		// Parse the stat name and value
		rest := line[bracketEnd+1:]
		rest = strings.TrimPrefix(rest, ".")
		parts := strings.Fields(rest)
		if len(parts) < 2 {
			continue
		}
		statName := parts[0]
		statVal, _ := strconv.Atoi(parts[1])

		if devMap[baseName] == nil {
			devMap[baseName] = &devErrors{}
		}
		de := devMap[baseName]

		switch statName {
		case "read_io_errs":
			de.read = statVal
		case "write_io_errs":
			de.write = statVal
		case "corruption_errs":
			de.corruption = statVal
		case "flush_io_errs":
			de.flush = statVal
		case "generation_errs":
			de.generation = statVal
		}
	}

	result := map[string]DiskStatus{}
	for name, de := range devMap {
		result[name] = DiskStatus{
			State:          "",
			ReadErrors:     de.read,
			WriteErrors:    de.write,
			ChecksumErrors: de.corruption + de.flush + de.generation,
		}
	}
	return result, nil
}

// ─── getSmartDetailsForDisk ──────────────────────────────────────────────────
//
// Returns SMART status and details for a disk using ONLY the cached data.
// NEVER calls smartctl directly — that runs in the background monitor every 30min.
// This function is called on every API request (pool listing), so it must be fast.
// ─────────────────────────────────────────────────────────────────────────────

func getSmartDetailsForDisk(diskName string) (smartStatus string, details SmartDetails) {
	// Read from cache only — no smartctl calls
	smartMu.Lock()
	cachedStatus, hasCached := smartHistory[diskName]
	cachedData, hasData := smartDetailsCache[diskName]
	smartMu.Unlock()

	if hasCached {
		smartStatus = cachedStatus
	} else {
		smartStatus = "unknown"
	}

	if hasData {
		details = cachedData
	}

	return smartStatus, details
}

// ─── CollectDiagnostics ──────────────────────────────────────────────────────
//
// Generates ALL diagnostic signals for a pool without prioritizing.
// Checks: disk existence, pool status per disk, SMART status, temperature,
// IO errors, and pool-level faults.
//
// Parameters:
//   poolType    — "zfs" or "btrfs"
//   vdevType    — "mirror", "raidz1", "raidz2", "raidz3", "single", "stripe",
//                 "raid1", "raid10" (BTRFS profiles mapped)
//   configDisks — disk names from config (e.g., ["/dev/sda", "/dev/sdb"])
//   zpoolName   — ZFS pool name (empty for BTRFS)
//   mountPoint  — mount point (used for BTRFS device stats)
//   zpoolHealth — raw ZFS health string from `zpool list` (e.g., "ONLINE", "FAULTED")
// ─────────────────────────────────────────────────────────────────────────────

type DiagnosticInput struct {
	PoolType    string
	VdevType    string
	ConfigDisks []string // raw disk paths from config, e.g. "/dev/sda"
	ZpoolName   string
	MountPoint  string
	ZpoolHealth string // raw health string from zpool list
}

func CollectDiagnostics(input DiagnosticInput) []Diagnostic {
	var diagnostics []Diagnostic

	// Normalize disk names: strip /dev/ prefix
	diskNames := make([]string, 0, len(input.ConfigDisks))
	for _, d := range input.ConfigDisks {
		name := strings.TrimPrefix(d, "/dev/")
		if name != "" {
			diskNames = append(diskNames, name)
		}
	}

	// ── Get per-disk pool status and IO errors ──

	var diskStatuses map[string]DiskStatus

	switch input.PoolType {
	case "zfs":
		if input.ZpoolName != "" {
			diskStatuses, _ = parseZpoolDiskStatus(input.ZpoolName)
		}
	case "btrfs":
		if input.MountPoint != "" {
			diskStatuses, _ = parseBtrfsDeviceStats(input.MountPoint)
		}
	}
	if diskStatuses == nil {
		diskStatuses = map[string]DiskStatus{}
	}

	// ── Check each configured disk ──

	for _, name := range diskNames {
		devPath := "/dev/" + name

		// 1. Does the disk physically exist?
		diskExists := false
		if _, err := os.Stat(devPath); err == nil {
			diskExists = true
		}

		if !diskExists {
			diagnostics = append(diagnostics, Diagnostic{
				Code:     "disk_missing",
				Severity: 3,
				Disk:     name,
				Detail:   fmt.Sprintf("Disco %s no encontrado en /dev/", name),
			})
			continue // No point checking SMART/IO on a missing disk
		}

		// 1b. Disk exists physically but is NOT in the pool?
		// For ZFS: if we have pool status data and this disk isn't in it,
		// the device at /dev/X is a different physical disk (not part of this pool)
		if len(diskStatuses) > 0 {
			if _, inPool := diskStatuses[name]; !inPool {
				diagnostics = append(diagnostics, Diagnostic{
					Code:     "disk_missing",
					Severity: 3,
					Disk:     name,
					Detail:   fmt.Sprintf("Disco %s: el dispositivo no pertenece a este pool", name),
				})
				continue
			}
		}

		// 2. Pool status per disk (ZFS only — BTRFS doesn't have per-device state)
		if ds, ok := diskStatuses[name]; ok {
			switch strings.ToUpper(ds.State) {
			case "FAULTED":
				diagnostics = append(diagnostics, Diagnostic{
					Code:     "disk_faulted",
					Severity: 4,
					Disk:     name,
					Detail:   fmt.Sprintf("Disco %s en estado FAULTED", name),
				})
			case "UNAVAIL":
				diagnostics = append(diagnostics, Diagnostic{
					Code:     "disk_unavailable",
					Severity: 4,
					Disk:     name,
					Detail:   fmt.Sprintf("Disco %s inaccesible (UNAVAIL)", name),
				})
			case "REMOVED":
				diagnostics = append(diagnostics, Diagnostic{
					Code:     "disk_removed",
					Severity: 2,
					Disk:     name,
					Detail:   fmt.Sprintf("Disco %s fue desconectado (REMOVED)", name),
				})
			case "OFFLINE":
				diagnostics = append(diagnostics, Diagnostic{
					Code:     "disk_removed",
					Severity: 2,
					Disk:     name,
					Detail:   fmt.Sprintf("Disco %s offline (deshabilitado manualmente)", name),
				})
			}

			// 3. IO errors
			if ds.ReadErrors > 0 || ds.WriteErrors > 0 || ds.ChecksumErrors > 0 {
				diagnostics = append(diagnostics, Diagnostic{
					Code:     "io_errors",
					Severity: 3,
					Disk:     name,
					Detail: fmt.Sprintf("Disco %s: %d errores lectura, %d escritura, %d checksum",
						name, ds.ReadErrors, ds.WriteErrors, ds.ChecksumErrors),
				})
			}
		}

		// 4. SMART status
		smartStatus, smartDetails := getSmartDetailsForDisk(name)

		switch smartStatus {
		case "critical":
			diagnostics = append(diagnostics, Diagnostic{
				Code:     "smart_critical",
				Severity: 4,
				Disk:     name,
				Detail:   fmt.Sprintf("Disco %s: SMART indica riesgo de fallo (uncorrectable=%d)", name, smartDetails.Uncorrectable),
			})
		case "warning":
			diagnostics = append(diagnostics, Diagnostic{
				Code:     "smart_warning",
				Severity: 2,
				Disk:     name,
				Detail:   fmt.Sprintf("Disco %s: SMART con alertas (reallocated=%d, pending=%d)", name, smartDetails.ReallocatedSectors, smartDetails.PendingSectors),
			})
		}

		// 5. Temperature
		if smartDetails.Temperature > 55 {
			diagnostics = append(diagnostics, Diagnostic{
				Code:     "temp_high",
				Severity: 2,
				Disk:     name,
				Detail:   fmt.Sprintf("Disco %s a %d°C (umbral: 55°C)", name, smartDetails.Temperature),
			})
		}
	}

	// ── Pool-level checks ──

	if input.PoolType == "zfs" && strings.ToUpper(input.ZpoolHealth) == "FAULTED" {
		diagnostics = append(diagnostics, Diagnostic{
			Code:     "pool_faulted",
			Severity: 4,
			Disk:     "",
			Detail:   "Pool ZFS en estado FAULTED — inaccesible",
		})
	}

	return diagnostics
}

// ─── ComputePoolHealth ───────────────────────────────────────────────────────
//
// Reduces a list of diagnostics to a single PoolHealth state.
// Priority (descending):
//   1. pool_faulted → critical
//   2. effective < 0 → critical (data_loss_likely)
//   3. effective == 0 AND smart_critical → critical (data_loss_risk)
//   4. disksMissing > 0 → degraded
//   5. io_errors → unstable
//   6. smart_warning OR temp_high → at_risk
//   7. else → healthy
//
// Parameters:
//   diagnostics     — from CollectDiagnostics
//   vdevType        — redundancy type
//   totalDisks      — configured disk count
//   resilverActive  — is resilver/rebuild in progress?
//   resilverProgress — 0-100
//   resilverEta     — human-readable ETA
// ─────────────────────────────────────────────────────────────────────────────

func ComputePoolHealth(
	diagnostics []Diagnostic,
	vdevType string,
	totalDisks int,
	resilverActive bool,
	resilverProgress float64,
	resilverEta string,
) PoolHealth {

	// ── Step 1: Analyze diagnostics ──

	hasCodes := map[string]bool{}
	for _, d := range diagnostics {
		hasCodes[d.Code] = true
	}

	// Count disks missing/faulted/unavailable
	disksMissing := 0
	disksWithSmartIssues := 0
	disksWithIoErrors := 0
	for _, d := range diagnostics {
		switch d.Code {
		case "disk_missing", "disk_faulted", "disk_unavailable":
			disksMissing++
		case "smart_warning", "smart_critical":
			disksWithSmartIssues++
		case "io_errors":
			disksWithIoErrors++
		}
	}

	disksOnline := totalDisks - disksMissing

	// ── Step 2: Calculate redundancy ──

	canLose := vdevTypeCanLose(vdevType)
	effective := canLose - disksMissing

	redundancy := PoolRedundancy{
		Type:      normalizeVdevType(vdevType),
		Expected:  totalDisks,
		Current:   disksOnline,
		CanLose:   canLose,
		Effective: effective,
	}

	// ── Step 3: Reduce to status ──

	status := "healthy"
	reasonCode := ""
	reasonMessage := ""

	switch {
	case hasCodes["pool_faulted"]:
		status = "critical"
		reasonCode = "pool_faulted"
		reasonMessage = "Pool inaccesible — estado FAULTED"

	case effective < 0:
		status = "critical"
		reasonCode = "data_loss_likely"
		reasonMessage = fmt.Sprintf("Pérdida de datos probable — faltan %d discos, tolerancia superada", disksMissing)

	case effective == 0 && hasCodes["smart_critical"]:
		status = "critical"
		reasonCode = "data_loss_risk"
		reasonMessage = "Riesgo de pérdida de datos — sin margen y disco restante con SMART crítico"

	case disksMissing > 0:
		status = "degraded"
		switch normalizeVdevType(vdevType) {
		case "mirror":
			reasonCode = "mirror_no_redundancy"
			reasonMessage = fmt.Sprintf("Sin redundancia — %d de %d discos activos", disksOnline, totalDisks)
		default:
			reasonCode = "raidz_reduced"
			reasonMessage = fmt.Sprintf("Degradado — puede perder %d discos más", effective)
		}
		// Single disk pool with the disk missing
		if canLose == 0 && disksMissing > 0 {
			status = "critical"
			reasonCode = "data_loss_likely"
			reasonMessage = fmt.Sprintf("Disco único ausente — datos inaccesibles")
		}

	// Mirror/RAID configured but not enough disks to provide redundancy
	// (e.g., mirror with 1 disk after detach — disk was removed from config)
	case canLose > 0 && totalDisks <= canLose:
		status = "degraded"
		reasonCode = "mirror_no_redundancy"
		redundancy.Effective = 0
		reasonMessage = fmt.Sprintf("Sin redundancia — %d disco(s), se necesitan %d para protección", totalDisks, canLose+1)

	case hasCodes["io_errors"]:
		status = "unstable"
		reasonCode = "io_errors_detected"
		reasonMessage = "Errores de IO detectados — verifica la integridad del pool"

	case hasCodes["smart_warning"] || hasCodes["temp_high"]:
		status = "at_risk"
		if hasCodes["smart_warning"] && hasCodes["temp_high"] {
			reasonCode = "smart_warning"
			reasonMessage = "Alertas SMART y temperatura alta detectadas"
		} else if hasCodes["smart_warning"] {
			reasonCode = "smart_warning"
			// Find the disk with the warning for a useful message
			for _, d := range diagnostics {
				if d.Code == "smart_warning" {
					reasonMessage = fmt.Sprintf("Disco %s con alertas SMART", d.Disk)
					break
				}
			}
		} else {
			reasonCode = "temp_high"
			for _, d := range diagnostics {
				if d.Code == "temp_high" {
					reasonMessage = d.Detail
					break
				}
			}
		}
	}

	// ── Step 4: Build reason (primary + secondary) ──

	var secondary []string
	for code := range hasCodes {
		if code != reasonCode && code != "" {
			secondary = append(secondary, code)
		}
	}

	reason := PoolHealthReason{
		Primary:   reasonCode,
		Message:   reasonMessage,
		Secondary: secondary,
	}

	// ── Step 5: Infer intent ──

	intent := "normal"
	if resilverActive {
		intent = "rebuilding"
	}

	// ── Build final PoolHealth ──

	return PoolHealth{
		Version: 1,

		Status: status,
		Reason: reason,

		Redundancy: redundancy,

		DisksTotal:           totalDisks,
		DisksOnline:          disksOnline,
		DisksMissing:         disksMissing,
		DisksWithSmartIssues: disksWithSmartIssues,
		DisksWithIoErrors:    disksWithIoErrors,

		ResilverActive:   resilverActive,
		ResilverProgress: resilverProgress,
		ResilverEta:      resilverEta,

		Intent: intent,

		Diagnostics: diagnostics,
	}
}

// ─── vdevTypeCanLose ─────────────────────────────────────────────────────────
// Returns how many disks a vdev type can lose before data loss.
func vdevTypeCanLose(vdevType string) int {
	switch normalizeVdevType(vdevType) {
	case "mirror", "raid1":
		return 1
	case "raidz1":
		return 1
	case "raidz2":
		return 2
	case "raidz3":
		return 3
	case "raid10":
		return 1 // per mirror pair — conservative
	default:
		return 0 // single, stripe, none
	}
}

// normalizeVdevType maps various vdev/profile names to canonical types.
func normalizeVdevType(vdevType string) string {
	switch strings.ToLower(vdevType) {
	case "mirror", "raid1":
		return "mirror"
	case "raidz", "raidz1":
		return "raidz1"
	case "raidz2":
		return "raidz2"
	case "raidz3":
		return "raidz3"
	case "raid10":
		return "raid10"
	case "single", "", "stripe":
		return "single"
	default:
		return strings.ToLower(vdevType)
	}
}

// ─── enrichDisksComplete ─────────────────────────────────────────────────────
//
// Full per-disk enrichment replacing enrichDisksWithSmart.
// Returns []EnrichedDisk with physical info, SMART details, pool status,
// and IO error counts for each disk.
//
// Parameters:
//   configDisks  — raw disk paths from pool config (e.g., ["/dev/sda", "/dev/sdb"])
//   diskStatuses — from parseZpoolDiskStatus or parseBtrfsDeviceStats (can be nil)
// ─────────────────────────────────────────────────────────────────────────────

func enrichDisksComplete(configDisks []string, diskStatuses map[string]DiskStatus) []EnrichedDisk {
	if diskStatuses == nil {
		diskStatuses = map[string]DiskStatus{}
	}

	enriched := make([]EnrichedDisk, 0, len(configDisks))

	for _, raw := range configDisks {
		name := strings.TrimPrefix(raw, "/dev/")
		if name == "" {
			continue
		}

		// Physical info from lsblk
		model := ""
		sizeStr := ""
		diskExists := false
		if out, ok := run(fmt.Sprintf("lsblk -d -n -o MODEL,SIZE /dev/%s 2>/dev/null", name)); ok && out != "" {
			diskExists = true
			parts := strings.Fields(strings.TrimSpace(out))
			if len(parts) >= 2 {
				sizeStr = parts[len(parts)-1]
				model = strings.Join(parts[:len(parts)-1], " ")
			} else if len(parts) == 1 {
				sizeStr = parts[0]
			}
		}

		// SMART
		smartStatus := "missing"
		var smartDetails SmartDetails
		if diskExists {
			smartStatus, smartDetails = getSmartDetailsForDisk(name)
		}

		// Pool status
		poolStatus := "missing"
		var ioErrors DiskStatus
		if diskExists {
			if ds, ok := diskStatuses[name]; ok {
				if ds.State != "" {
					poolStatus = strings.ToLower(ds.State)
					// Map ZFS states to NimOS names
					switch poolStatus {
					case "online":
						poolStatus = "online"
					case "degraded":
						poolStatus = "degraded"
					case "faulted":
						poolStatus = "faulted"
					case "offline":
						poolStatus = "offline"
					case "removed":
						poolStatus = "removed"
					case "unavail":
						poolStatus = "unavailable"
					}
				} else {
					// BTRFS — no per-device state, if it exists it's online
					poolStatus = "online"
				}
				ioErrors = ds
			} else if len(diskStatuses) == 0 {
				// No pool status data available (BTRFS without stats, or pool offline)
				poolStatus = "online"
			} else {
				// Pool status data exists but this disk is NOT in it
				// The device at this path is NOT part of the pool
				// (e.g., config says /dev/sdb but that's now a different physical disk)
				poolStatus = "missing"
			}
		}

		enriched = append(enriched, EnrichedDisk{
			Name:        name,
			Model:       model,
			Size:        sizeStr,
			SmartStatus: smartStatus,
			Smart:       smartDetails,
			PoolStatus:  poolStatus,
			IoErrors:    ioErrors,
		})
	}

	return enriched
}

// ─── buildPoolHealth ─────────────────────────────────────────────────────────
//
// Convenience function that ties everything together for a pool.
// Called from getZfsPoolInfo / getBtrfsPoolInfo.
// ─────────────────────────────────────────────────────────────────────────────

func buildPoolHealth(input DiagnosticInput) PoolHealth {
	diagnostics := CollectDiagnostics(input)

	// Get resilver/rebuild status
	resilverActive := false
	resilverProgress := 0.0
	resilverEta := ""

	// Determine pool name for resilver check
	poolName := ""
	switch input.PoolType {
	case "zfs":
		poolName = input.ZpoolName
	case "btrfs":
		// For BTRFS, we check balance status using mount point
		if input.MountPoint != "" {
			out, ok := run(fmt.Sprintf("btrfs balance status %s 2>/dev/null", input.MountPoint))
			if ok && (strings.Contains(out, "in progress") || strings.Contains(out, "running")) {
				resilverActive = true
			}
		}
	}

	if input.PoolType == "zfs" && poolName != "" {
		out, ok := run(fmt.Sprintf("zpool status %s 2>/dev/null", poolName))
		if ok {
			for _, line := range strings.Split(out, "\n") {
				line = strings.TrimSpace(line)
				if strings.Contains(line, "resilver in progress") {
					resilverActive = true
				}
				if strings.Contains(line, "% done") {
					parts := strings.Fields(line)
					for i, p := range parts {
						if p == "done," && i > 0 {
							pctStr := strings.TrimSuffix(parts[i-1], "%")
							pct, _ := strconv.ParseFloat(pctStr, 64)
							resilverProgress = pct
						}
						if p == "go" && i > 0 {
							resilverEta = parts[i-1]
						}
					}
				}
			}
		}
	}

	totalDisks := len(input.ConfigDisks)
	return ComputePoolHealth(diagnostics, input.VdevType, totalDisks, resilverActive, resilverProgress, resilverEta)
}
