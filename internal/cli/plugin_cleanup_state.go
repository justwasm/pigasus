// plugin_cleanup_state.go — process registry state management for AGY 1.2.7
// plugin process lifecycle tracking.
package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"syscall"
	"time"
)

// PluginProcessRecord is one row of the plugin process registry. The
// schema is intentionally minimal: the only required field is PID, the
// others are diagnostic and survive a registry rewrite even when the
// process itself is long gone.
type PluginProcessRecord struct {
	PID        int    `json:"pid"`
	PluginName string `json:"plugin"`
	ServerName string `json:"server"`
	InstallDir string `json:"install_dir"`
	StartedAt  int64  `json:"started_at"`
}

// PluginProcessRegistryPath returns the on-disk location of the registry.
// The directory is created lazily by callers (RegisterPluginProcess,
// SavePluginProcessRegistry) so this getter never touches the filesystem.
func PluginProcessRegistryPath(antigravityHome string) string {
	return filepath.Join(antigravityHome, "state", "plugin-processes.json")
}

// LoadPluginProcessRegistry reads the registry file. A missing file is
// not an error: a fresh install has never recorded a plugin process
// and the registry is created on the first RegisterPluginProcess call.
// Parse errors and unreadable files yield (nil, err) so callers can
// decide whether the corruption is fatal or merely informational.
func LoadPluginProcessRegistry(path string) ([]PluginProcessRecord, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	var doc struct {
		Records []PluginProcessRecord `json:"records"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, err
	}
	return doc.Records, nil
}

// SavePluginProcessRegistry writes the registry atomically (write to
// <path>.tmp, rename) so a concurrent read by KillPluginProcessesForInstall
// never sees a half-written file. The parent directory is created with
// 0700 perms.
func SavePluginProcessRegistry(path string, records []PluginProcessRecord) error {
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	doc := struct {
		Records []PluginProcessRecord `json:"records"`
	}{Records: records}
	buf, err := json.MarshalIndent(doc, "", "  ")
	if err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, buf, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// RegisterPluginProcess appends rec to the registry at path. Duplicates
// (matching PID) are silently dropped — a process that is already
// tracked should not be tracked twice. A missing registry is created.
func RegisterPluginProcess(path string, rec PluginProcessRecord) error {
	records, err := LoadPluginProcessRegistry(path)
	if err != nil {
		return err
	}
	for _, existing := range records {
		if existing.PID == rec.PID {
			return nil
		}
	}
	records = append(records, rec)
	return SavePluginProcessRegistry(path, records)
}

// UnregisterPluginProcess removes every record with the given PID from
// the registry. Missing PIDs are a no-op (the caller's intent is
// satisfied either way).
func UnregisterPluginProcess(path string, pid int) error {
	records, err := LoadPluginProcessRegistry(path)
	if err != nil {
		return err
	}
	out := records[:0]
	changed := false
	for _, r := range records {
		if r.PID == pid {
			changed = true
			continue
		}
		out = append(out, r)
	}
	if !changed {
		return nil
	}
	return SavePluginProcessRegistry(path, out)
}

// signalProcess sends SIGTERM to pid and ignores ESRCH (the process
// has already exited). SIGKILL is not escalated here because the caller
// controls the lifecycle: the next startup's PruneOrphanPluginProcesses
// picks up anything that ignored SIGTERM, and the upgrade flow does
// not block on the kill.
func signalProcess(pid int) error {
	if pid <= 0 {
		return fmt.Errorf("invalid pid %d", pid)
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	if err := proc.Signal(syscall.SIGTERM); err != nil {
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) {
			return nil
		}
		return err
	}
	return nil
}

// dirExists reports whether path is an existing directory. Symlinks
// are followed. A non-existent path returns false, not an error.
func dirExists(path string) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	info, err := os.Stat(path)
	if err != nil {
		return false
	}
	return info.IsDir()
}

// canonicalInstallDir resolves symlinks and cleans installDir so two
// paths that differ only by an intermediate symlink compare equal.
// On any error it falls back to filepath.Clean — the registry row
// still matches as long as the caller and the writer agreed on the
// same string form.
func canonicalInstallDir(p string) string {
	if real, err := filepath.EvalSymlinks(p); err == nil {
		return real
	}
	return filepath.Clean(p)
}

// PluginProcessRecordNow is a small helper for tests and callers that
// want to record a freshly-started process: it stamps StartedAt with
// the current Unix time so the registry has a stable sort order and
// operators can age out stale rows later.
func PluginProcessRecordNow(pid int, plugin, server, installDir string) PluginProcessRecord {
	return PluginProcessRecord{
		PID:        pid,
		PluginName: plugin,
		ServerName: server,
		InstallDir: installDir,
		StartedAt:  time.Now().Unix(),
	}
}

// SortedPluginProcessRecords returns records sorted by (PluginName,
// ServerName, PID). Tests rely on the stable order; the registry
// itself does not require sorting.
func SortedPluginProcessRecords(in []PluginProcessRecord) []PluginProcessRecord {
	out := append([]PluginProcessRecord(nil), in...)
	sort.Slice(out, func(i, k int) bool {
		if out[i].PluginName != out[k].PluginName {
			return out[i].PluginName < out[k].PluginName
		}
		if out[i].ServerName != out[k].ServerName {
			return out[i].ServerName < out[k].ServerName
		}
		return out[i].PID < out[k].PID
	})
	return out
}
