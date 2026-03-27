package main

// ═══════════════════════════════════════════════════════════════════════════════
// NimOS Storage — Wipe Module (Plan v2)
// Based on TrueNAS middleware disk wipe architecture.
// Reviewed by GPT (structure) and Gemini (rollback, serial check, mutex).
//
// Principles:
//   1. Verify after every operation — never trust exit codes alone
//   2. Unmount clean first, fuser as fallback
//   3. Zero start + end of disk (GPT backup table)
//   4. Exclusive lock — one storage operation at a time
//   5. Journal — know where we are if daemon crashes
// ═══════════════════════════════════════════════════════════════════════════════

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"time"
)

// ─── Command Executor ────────────────────────────────────────────────────────

type CmdOptions struct {
	Timeout   time.Duration
	Retries   int
	RetryWait time.Duration
}

type CmdResult struct {
	Stdout string
	Stderr string
	Code   int
	OK     bool
}

func runCmd(cmd string, args []string, opts CmdOptions) (CmdResult, error) {
	if opts.Timeout == 0 {
		opts.Timeout = 30 * time.Second
	}

	var lastErr error

	for attempt := 0; attempt <= opts.Retries; attempt++ {
		if attempt > 0 {
			time.Sleep(opts.RetryWait)
		}

		ctx, cancel := context.WithTimeout(context.Background(), opts.Timeout)
		c := exec.CommandContext(ctx, cmd, args...)

		var out, errb bytes.Buffer
		c.Stdout = &out
		c.Stderr = &errb

		err := c.Run()
		cancel()

		res := CmdResult{
			Stdout: out.String(),
			Stderr: errb.String(),
		}

		if err == nil {
			res.OK = true
			return res, nil
		}

		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			res.Code = exitErr.ExitCode()
			lastErr = fmt.Errorf("%s failed (code %d): %s", cmd, res.Code, res.Stderr)
		} else {
			lastErr = err
		}
	}

	return CmdResult{}, lastErr
}

// ─── Storage Errors ──────────────────────────────────────────────────────────

type StorageError struct {
	Code string
	Msg  string
}

func (e StorageError) Error() string {
	return e.Msg
}

var (
	ErrNotEligible = StorageError{"NOT_ELIGIBLE", "disk not eligible for this operation"}
	ErrIsBoot      = StorageError{"IS_BOOT", "cannot operate on boot disk"}
	ErrBusy        = StorageError{"BUSY", "device or resource busy"}
	ErrWipeFail    = StorageError{"WIPE_FAIL", "wipe verification failed — partitions still present"}
)

// ─── Journal ─────────────────────────────────────────────────────────────────

type OpStatus string

const (
	OpPending OpStatus = "pending"
	OpDone    OpStatus = "done"
	OpFailed  OpStatus = "failed"
)

type StepPhase string

const (
	PhaseStarted   StepPhase = "started"
	PhaseCompleted StepPhase = "completed"
)

const journalPath = "/var/lib/nimbusos/storage-journal.json"

type JournalOp struct {
	ID        string            `json:"id"`
	Type      string            `json:"type"`
	Step      int               `json:"step"`
	Phase     StepPhase         `json:"phase"`
	Status    OpStatus          `json:"status"`
	Data      map[string]string `json:"data"`
	Timestamp string            `json:"ts"`
}

var journalMu sync.Mutex

func journalSave(op JournalOp) error {
	journalMu.Lock()
	defer journalMu.Unlock()

	op.Timestamp = time.Now().UTC().Format(time.RFC3339)
	data, err := json.MarshalIndent(op, "", "  ")
	if err != nil {
		return err
	}

	// Atomic write: tmp → fsync → rename
	tmpPath := journalPath + ".tmp"
	f, err := os.OpenFile(tmpPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	f.Close()
	return os.Rename(tmpPath, journalPath)
}

func journalClear() {
	os.Remove(journalPath)
}

func journalRecover() {
	tryStorageLockOrWarn()
	data, err := os.ReadFile(journalPath)
	if err != nil {
		return
	}
	var op JournalOp
	if json.Unmarshal(data, &op) != nil {
		logMsg("WARNING: storage journal corrupted — deleting")
		journalClear()
		return
	}
	if op.Status == OpDone {
		journalClear()
		return
	}
	logMsg("WARNING: interrupted storage operation '%s' at step %d (phase: %s) — clearing",
		op.ID, op.Step, op.Phase)
	journalClear()
}

var storageLockFile *os.File

func tryStorageLockOrWarn() {
	lockPath := journalPath + ".lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return
	}
	err = syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if err != nil {
		f.Close()
		logMsg("WARNING: storage lock already held — another daemon instance may be running")
		return
	}
	storageLockFile = f
}

// ─── Operations Engine ───────────────────────────────────────────────────────

var storageMu sync.Mutex

type StepErrorPolicy int

const (
	FailFast StepErrorPolicy = iota
	Continue
	Ignore
)

type Step struct {
	Name   string
	Do     func() error
	Undo   func() error
	Policy StepErrorPolicy
}

func runSteps(op JournalOp, steps []Step) error {
	var completed []int

	for i := op.Step; i < len(steps); i++ {
		op.Step = i
		op.Phase = PhaseStarted
		op.Status = OpPending
		journalSave(op)

		logMsg("storage op '%s': step %d/%d — %s", op.ID, i+1, len(steps), steps[i].Name)

		if err := steps[i].Do(); err != nil {
			switch steps[i].Policy {
			case FailFast:
				op.Status = OpFailed
				journalSave(op)
				logMsg("storage op '%s': step %d FAILED — %s — rolling back", op.ID, i, err)

				for j := len(completed) - 1; j >= 0; j-- {
					idx := completed[j]
					if steps[idx].Undo != nil {
						logMsg("storage op '%s': rollback step %d — %s", op.ID, idx, steps[idx].Name)
						steps[idx].Undo()
					}
				}
				journalClear()
				return fmt.Errorf("step %d (%s) failed: %w", i, steps[i].Name, err)

			case Continue:
				logMsg("storage op '%s': step %d warning — %s — continuing", op.ID, i, err)

			case Ignore:
				// silent
			}
		}

		completed = append(completed, i)
		op.Phase = PhaseCompleted
		journalSave(op)
	}

	op.Status = OpDone
	journalSave(op)
	journalClear()
	return nil
}

// ─── Pre-flight Check ────────────────────────────────────────────────────────

func preFlightCheck(diskPath string) error {
	diskName := strings.TrimPrefix(diskPath, "/dev/")

	// Boot disk?
	lsblkRaw, _ := run("lsblk -J -b -o NAME,MOUNTPOINT,TYPE 2>/dev/null")
	rootDisk := findRootDiskGo(lsblkRaw)
	if diskName == rootDisk {
		return ErrIsBoot
	}

	// Kernel holders? (LVM, dm, RAID)
	holdersPath := fmt.Sprintf("/sys/block/%s/holders", diskName)
	entries, err := os.ReadDir(holdersPath)
	if err == nil && len(entries) > 0 {
		names := []string{}
		for _, e := range entries {
			names = append(names, e.Name())
		}
		return fmt.Errorf("disk %s has active holders: %s", diskPath, strings.Join(names, ", "))
	}

	// Disk exists?
	if _, err := os.Stat(diskPath); err != nil {
		return fmt.Errorf("disk %s not found", diskPath)
	}

	return nil
}

// ─── WIPE ────────────────────────────────────────────────────────────────────

// wipeDiskGo is the real wipe implementation based on TrueNAS.
// Replaces the stub. Called from handleStorageRoutes.
//
// Order of operations:
//   0. Pre-flight safety check (boot disk, holders)
//   1. Unmount partitions cleanly
//   2. Kill processes using the disk (fuser fallback)
//   3. Clear ZFS labels
//   4. Zero first 32MB (MBR, GPT, superblocks)
//   5. Zero last 32MB (GPT backup, ZFS tail labels)
//   6. Destroy GPT with sgdisk
//   7. Clear remaining signatures with wipefs
//   8. Force kernel to re-read partition table
//   9. VERIFY: lsblk must show zero partitions
func wipeDiskGo(diskPath string) map[string]interface{} {
	storageMu.Lock()
	defer storageMu.Unlock()

	return wipeDiskInternal(diskPath)
}

// wipeDiskInternal does the actual wipe — called with lock already held
func wipeDiskInternal(diskPath string) map[string]interface{} {

	// Pre-flight
	if err := preFlightCheck(diskPath); err != nil {
		return map[string]interface{}{"error": err.Error()}
	}

	opts := CmdOptions{Timeout: 30 * time.Second, Retries: 1, RetryWait: 1 * time.Second}
	optsNoFail := CmdOptions{Timeout: 15 * time.Second}
	diskBase := filepath.Base(diskPath)

	op := JournalOp{
		ID:   "wipe-" + diskBase,
		Type: "wipe",
		Data: map[string]string{"disk": diskPath},
	}

	steps := []Step{
		// 0. Unmount partitions cleanly first
		{Name: "unmount_clean", Policy: Continue, Do: func() error {
			res, _ := runCmd("lsblk", []string{"-ln", "-o", "NAME,MOUNTPOINT", diskPath}, optsNoFail)
			for _, line := range strings.Split(res.Stdout, "\n") {
				fields := strings.Fields(line)
				if len(fields) >= 2 && fields[1] != "" {
					runCmd("umount", []string{"-f", fields[1]}, optsNoFail)
				}
			}
			runCmd("umount", []string{"-f", diskPath}, optsNoFail)
			return nil
		}},

		// 1. Kill processes using the disk (fallback)
		{Name: "fuser_kill", Policy: Continue, Do: func() error {
			runCmd("fuser", []string{"-km", diskPath}, optsNoFail)
			partsOut, _ := runCmd("lsblk", []string{"-ln", "-o", "NAME", diskPath}, optsNoFail)
			for _, line := range strings.Split(partsOut.Stdout, "\n") {
				p := strings.TrimSpace(line)
				if p != "" && p != diskBase {
					runCmd("fuser", []string{"-km", "/dev/" + p}, optsNoFail)
				}
			}
			time.Sleep(500 * time.Millisecond)
			return nil
		}},

		// 2. Clear ZFS labels on disk AND each partition
		{Name: "zpool_labelclear", Policy: Continue, Do: func() error {
			if hasZfs {
				runCmd("zpool", []string{"labelclear", "-f", diskPath}, optsNoFail)
				partsOut, _ := runCmd("lsblk", []string{"-ln", "-o", "NAME", diskPath}, optsNoFail)
				for _, line := range strings.Split(partsOut.Stdout, "\n") {
					p := strings.TrimSpace(line)
					if p != "" && p != diskBase {
						runCmd("zpool", []string{"labelclear", "-f", "/dev/" + p}, optsNoFail)
					}
				}
			}
			return nil
		}},

		// 3. Wipe signatures on EACH PARTITION first, then remove partitions from kernel
		// This must happen BEFORE zeroing the disk or destroying GPT
		{Name: "wipe_partitions", Policy: Continue, Do: func() error {
			partsOut, _ := runCmd("lsblk", []string{"-ln", "-o", "NAME", diskPath}, optsNoFail)
			for _, line := range strings.Split(partsOut.Stdout, "\n") {
				p := strings.TrimSpace(line)
				if p != "" && p != diskBase {
					partDev := "/dev/" + p
					// Wipe filesystem signatures on the partition
					runCmd("wipefs", []string{"-af", partDev}, optsNoFail)
					// Zero first 1MB of partition (superblock area)
					runCmd("dd", []string{"if=/dev/zero", "of=" + partDev, "bs=1M", "count=1", "conv=fsync,notrunc"}, optsNoFail)
				}
			}
			// Remove partitions from kernel cache
			runCmd("partx", []string{"-d", diskPath}, optsNoFail)
			time.Sleep(500 * time.Millisecond)
			return nil
		}},

		// 4. Zero first 32MB (MBR, GPT primary, superblocks)
		{Name: "zero_start", Policy: FailFast, Do: func() error {
			_, err := runCmd("dd", []string{
				"if=/dev/zero", "of=" + diskPath,
				"bs=1M", "count=32", "conv=fsync,notrunc",
			}, opts)
			return err
		}},

		// 5. Zero last 32MB (GPT backup table, ZFS tail labels)
		{Name: "zero_end", Policy: Continue, Do: func() error {
			res, err := runCmd("blockdev", []string{"--getsize64", diskPath}, optsNoFail)
			if err != nil {
				return nil
			}
			var size int64
			fmt.Sscanf(strings.TrimSpace(res.Stdout), "%d", &size)
			if size > 64*1024*1024 {
				seekMB := (size / (1024 * 1024)) - 32
				runCmd("dd", []string{
					"if=/dev/zero", "of=" + diskPath,
					"bs=1M", "count=32",
					fmt.Sprintf("seek=%d", seekMB),
					"conv=fsync,notrunc",
				}, opts)
			}
			return nil
		}},

		// 6. Destroy GPT/MBR structures
		{Name: "sgdisk_zap", Policy: Continue, Do: func() error {
			runCmd("sgdisk", []string{"-Z", diskPath}, optsNoFail)
			return nil
		}},

		// 7. Final wipefs on disk itself
		{Name: "wipefs_disk", Policy: Continue, Do: func() error {
			runCmd("wipefs", []string{"-af", diskPath}, optsNoFail)
			return nil
		}},

		// 8. Force kernel to re-read partition table
		{Name: "reread_partitions", Policy: Continue, Do: func() error {
			runCmd("partx", []string{"-d", diskPath}, optsNoFail)
			runCmd("blockdev", []string{"--rereadpt", diskPath}, optsNoFail)
			runCmd("partprobe", []string{diskPath}, optsNoFail)
			runCmd("udevadm", []string{"settle", "--timeout=5"}, optsNoFail)
			time.Sleep(2 * time.Second)
			return nil
		}},

		// 9. VERIFY — partitions must be gone. If not, aggressive retry.
		{Name: "verify_clean", Policy: FailFast, Do: func() error {
			for attempt := 0; attempt < 3; attempt++ {
				res, _ := runCmd("lsblk", []string{"-ln", "-o", "NAME", diskPath}, optsNoFail)
				lines := strings.Split(strings.TrimSpace(res.Stdout), "\n")
				partCount := 0
				for _, line := range lines {
					line = strings.TrimSpace(line)
					if line != "" && line != diskBase {
						partCount++
					}
				}
				if partCount == 0 {
					logMsg("Wipe verified: %s is clean (0 partitions)", diskPath)
					return nil
				}

				if attempt < 2 {
					logMsg("Wipe verify attempt %d: %d partitions remain on %s — retrying aggressively", attempt+1, partCount, diskPath)
					// Aggressive retry: wipe remaining partitions, re-zero, re-partx
					retryParts, _ := runCmd("lsblk", []string{"-ln", "-o", "NAME", diskPath}, optsNoFail)
					for _, line := range strings.Split(retryParts.Stdout, "\n") {
						p := strings.TrimSpace(line)
						if p != "" && p != diskBase {
							runCmd("wipefs", []string{"-af", "/dev/" + p}, optsNoFail)
							runCmd("dd", []string{"if=/dev/zero", "of=/dev/" + p, "bs=1M", "count=1", "conv=fsync,notrunc"}, optsNoFail)
						}
					}
					runCmd("partx", []string{"-d", diskPath}, optsNoFail)
					runCmd("sgdisk", []string{"-Z", diskPath}, optsNoFail)
					runCmd("wipefs", []string{"-af", diskPath}, optsNoFail)
					runCmd("blockdev", []string{"--rereadpt", diskPath}, optsNoFail)
					runCmd("partprobe", []string{diskPath}, optsNoFail)
					runCmd("udevadm", []string{"settle", "--timeout=5"}, optsNoFail)
					time.Sleep(3 * time.Second)
				}
			}
			return fmt.Errorf("wipe verification failed: partitions still on %s after 3 attempts", diskPath)
		}},
	}

	if err := runSteps(op, steps); err != nil {
		return map[string]interface{}{"error": err.Error()}
	}

	return map[string]interface{}{"ok": true, "disk": diskPath}
}
