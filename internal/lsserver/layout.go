// Package lsserver — layout.go: the reference's on-disk layout.
//
// The reference keeps three roots under one `-gemini_dir`:
//
//	<gemini_dir>/<config_dir>/    the user configuration (default "config")
//	<gemini_dir>/<app_data_dir>/  the application data (default "antigravity-ide")
//
// A fresh run creates:
//
//	<config_dir>/.migrated        the migration marker (empty file)
//	<config_dir>/mcp_config.json  the MCP server list (empty file)
//	<app_data_dir>/installation_id  the persistent UUID (no trailing newline)
//	<app_data_dir>/{crashes,conversations,brain,daemon}/
//	<app_data_dir>/builtin/skills/ the nine bundled skills
//	<app_data_dir>/builtin/.checksum  the bundle marker
//	<app_data_dir>/builtin/keep.txt    the keep list (empty file)
//
// and, in persistent mode, the discovery file:
//
//	<app_data_dir>/daemon/ls_<sha256(workspace_id)[:16]>.json
//
// Evidence grade: 一手 — captured from the reference binary on this
// machine (out/langserver/layout.txt).
package lsserver

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/google/uuid"

	"github.com/justwasm/pigasus/internal/builtinskills"
)

// Layout is the reference's resolved on-disk layout.
type Layout struct {
	// GeminiDir is the root the other two live under.
	GeminiDir string
	// ConfigDir is the user configuration root.
	ConfigDir string
	// AppDataDir is the application data root.
	AppDataDir string
}

// ResolveLayout resolves the three roots from the flag values.
//
// The reference's own rule: `-gemini_dir` is used as given when it is
// absolute and resolved against the home directory when it is
// relative; `-config_dir` and `-app_data_dir` are relative to the
// gemini dir.
func ResolveLayout(f *Flags) (Layout, error) {
	gemini, err := resolveAgainstHome(f.GeminiDir)
	if err != nil {
		return Layout{}, err
	}
	config, err := resolveUnder(gemini, f.ConfigDir)
	if err != nil {
		return Layout{}, err
	}
	appData, err := resolveUnder(gemini, f.AppDataDir)
	if err != nil {
		return Layout{}, err
	}
	return Layout{GeminiDir: gemini, ConfigDir: config, AppDataDir: appData}, nil
}

// resolveAgainstHome resolves a possibly relative path against the
// user's home directory.
func resolveAgainstHome(path string) (string, error) {
	if path == "" {
		path = ".gemini"
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("lsserver: resolve %q: %w", path, err)
	}
	return filepath.Join(home, path), nil
}

// resolveUnder resolves a path against a root. An absolute path wins
// over the root.
func resolveUnder(root, path string) (string, error) {
	if path == "" {
		return "", fmt.Errorf("lsserver: empty path under %q", root)
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path), nil
	}
	return filepath.Join(root, path), nil
}

// MigrationMarker is the path of the config migration marker.
func (l Layout) MigrationMarker() string {
	return filepath.Join(l.ConfigDir, ".migrated")
}

// DaemonDir is the directory the discovery file lives in.
func (l Layout) DaemonDir() string {
	return filepath.Join(l.AppDataDir, "daemon")
}

// Prepare creates the layout the reference creates on a fresh run.
//
// The migration marker is written when it is missing — the reference
// logs the two migration lines when it migrates and stays silent when
// the marker is already there.
func (l Layout) Prepare(log *Logger, thread int) error {
	if err := l.prepareConfigDir(log, thread); err != nil {
		return err
	}
	if err := l.prepareAppDataDir(); err != nil {
		return err
	}
	return nil
}

// prepareConfigDir creates the config root, the MCP server list and
// the migration marker.
func (l Layout) prepareConfigDir(log *Logger, thread int) error {
	if err := os.MkdirAll(l.ConfigDir, 0o755); err != nil {
		return fmt.Errorf("lsserver: create %s: %w", l.ConfigDir, err)
	}
	mcp := filepath.Join(l.ConfigDir, "mcp_config.json")
	if _, err := os.Stat(mcp); err != nil {
		if err := os.WriteFile(mcp, nil, 0o644); err != nil {
			return fmt.Errorf("lsserver: write %s: %w", mcp, err)
		}
	}
	return l.writeMigrationMarker(log, thread)
}

// writeMigrationMarker writes the empty migration marker, logging the
// reference's two migration lines when the marker was missing.
func (l Layout) writeMigrationMarker(log *Logger, thread int) error {
	marker := l.MigrationMarker()
	if _, err := os.Stat(marker); err == nil {
		return nil
	}
	log.Infof(thread, locMigration, "Migration marker %s does not exist. Attempting migration from AppDataDir %s.", marker, l.AppDataDir)
	if err := os.WriteFile(marker, nil, 0o644); err != nil {
		return fmt.Errorf("lsserver: write %s: %w", marker, err)
	}
	log.Infof(thread, locMigrationDone, "Migration complete, marked with %s", marker)
	return nil
}

// prepareAppDataDir creates the application data root and the
// directories and files the reference keeps there.
func (l Layout) prepareAppDataDir() error {
	if err := os.MkdirAll(l.AppDataDir, 0o755); err != nil {
		return fmt.Errorf("lsserver: create %s: %w", l.AppDataDir, err)
	}
	for _, dir := range []string{"crashes", "conversations", "brain", "daemon"} {
		if err := os.MkdirAll(filepath.Join(l.AppDataDir, dir), 0o755); err != nil {
			return fmt.Errorf("lsserver: create %s: %w", dir, err)
		}
	}
	if err := l.writeInstallationID(); err != nil {
		return err
	}
	return l.installBuiltinSkills()
}

// writeInstallationID writes the persistent installation UUID. The
// reference keeps the file's bytes to the UUID alone — no trailing
// newline.
func (l Layout) writeInstallationID() error {
	path := filepath.Join(l.AppDataDir, "installation_id")
	if _, err := os.Stat(path); err == nil {
		return nil
	}
	if err := os.WriteFile(path, []byte(uuid.NewString()), 0o644); err != nil {
		return fmt.Errorf("lsserver: write %s: %w", path, err)
	}
	return nil
}

// installBuiltinSkills unpacks the bundled skills and writes the
// bundle marker.
//
// The reference writes two files next to the skills: `keep.txt` (an
// empty keep list) and `.checksum` (`builtin:<sha256>`), which is how
// it notices that the bundle changed and re-unpacks it. The clone's
// checksum is computed over its own installed tree — the reference's
// input is its embedded archive, which does not survive the
// unpacking, so the two values differ by design.
func (l Layout) installBuiltinSkills() error {
	if _, err := builtinskills.Install(l.AppDataDir); err != nil {
		return fmt.Errorf("lsserver: install builtin skills: %w", err)
	}
	builtin := filepath.Join(l.AppDataDir, "builtin")
	if err := os.WriteFile(filepath.Join(builtin, "keep.txt"), nil, 0o644); err != nil {
		return fmt.Errorf("lsserver: write keep.txt: %w", err)
	}
	sum, err := builtinChecksum(filepath.Join(builtin, "skills"))
	if err != nil {
		return err
	}
	marker := filepath.Join(builtin, ".checksum")
	if err := os.WriteFile(marker, []byte("builtin:"+sum+"\n"), 0o644); err != nil {
		return fmt.Errorf("lsserver: write %s: %w", marker, err)
	}
	return nil
}

// builtinChecksum hashes the installed skill tree.
//
// The record is one line per file — the tree-relative path, a NUL,
// the file's own sha256 and a newline — in path order. The order is
// the sort of the paths, so the value does not depend on the order
// the files were written in.
func builtinChecksum(root string) (string, error) {
	var records []string
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		body, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(body)
		records = append(records, filepath.ToSlash(rel)+"\x00"+hex.EncodeToString(sum[:])+"\n")
		return nil
	})
	if err != nil {
		return "", fmt.Errorf("lsserver: hash %s: %w", root, err)
	}
	sort.Strings(records)
	sum := sha256.Sum256([]byte(strings.Join(records, "")))
	return hex.EncodeToString(sum[:]), nil
}

// Discovery is the reference's discovery file.
type Discovery struct {
	PID       int    `json:"pid"`
	HTTPSPort int    `json:"httpsPort"`
	HTTPPort  int    `json:"httpPort"`
	LSPPort   int    `json:"lspPort"`
	LSVersion string `json:"lsVersion"`
	CSRFToken string `json:"csrfToken"`
}

// WriteDiscovery writes the discovery file and returns its path.
//
// The file's name carries the workspace id's digest — the first
// sixteen hex characters of its sha256 — so two workspaces share one
// app data dir without colliding.
//
// Evidence grade: 一手 — the reference's name is
// `ls_e3b0c44298fc1c14.json` for an empty workspace id and
// `ls_edf8f80b8c4011f2.json` for "myworkspace"; both are
// sha256(workspace_id)[:16].
func (l Layout) WriteDiscovery(workspaceID string, d Discovery) (string, error) {
	dir := l.DaemonDir()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", fmt.Errorf("lsserver: create %s: %w", dir, err)
	}
	body, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return "", fmt.Errorf("lsserver: marshal discovery: %w", err)
	}
	path := filepath.Join(dir, "ls_"+workspaceDigest(workspaceID)+".json")
	if err := os.WriteFile(path, body, 0o644); err != nil {
		return "", fmt.Errorf("lsserver: write %s: %w", path, err)
	}
	return path, nil
}

// workspaceDigest is the digest the discovery file's name carries.
func workspaceDigest(workspaceID string) string {
	sum := sha256.Sum256([]byte(workspaceID))
	return hex.EncodeToString(sum[:])[:16]
}
