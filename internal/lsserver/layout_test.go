package lsserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestResolveLayoutFollowsTheFlags checks the three roots the
// reference resolves from its flags.
func TestResolveLayoutFollowsTheFlags(t *testing.T) {
	flags := &Flags{GeminiDir: "/tmp/lsroot", ConfigDir: "config", AppDataDir: "antigravity-ide"}
	layout, err := ResolveLayout(flags)
	if err != nil {
		t.Fatalf("ResolveLayout: %v", err)
	}
	if layout.GeminiDir != "/tmp/lsroot" {
		t.Errorf("GeminiDir = %q", layout.GeminiDir)
	}
	if layout.ConfigDir != "/tmp/lsroot/config" {
		t.Errorf("ConfigDir = %q", layout.ConfigDir)
	}
	if layout.AppDataDir != "/tmp/lsroot/antigravity-ide" {
		t.Errorf("AppDataDir = %q", layout.AppDataDir)
	}
}

// TestPrepareCreatesTheReferenceLayout checks the files and the
// directories a fresh run creates.
func TestPrepareCreatesTheReferenceLayout(t *testing.T) {
	root := t.TempDir()
	flags := &Flags{GeminiDir: root, ConfigDir: "config", AppDataDir: "antigravity-ide"}
	layout, err := ResolveLayout(flags)
	if err != nil {
		t.Fatalf("ResolveLayout: %v", err)
	}
	var log strings.Builder
	if err := layout.Prepare(NewLogger(&log), 1); err != nil {
		t.Fatalf("Prepare: %v", err)
	}
	for _, dir := range []string{"config", "antigravity-ide", "antigravity-ide/crashes",
		"antigravity-ide/conversations", "antigravity-ide/brain", "antigravity-ide/daemon",
		"antigravity-ide/builtin/skills"} {
		if _, err := os.Stat(filepath.Join(root, dir)); err != nil {
			t.Errorf("%s is missing: %v", dir, err)
		}
	}
	for _, file := range []string{"config/.migrated", "config/mcp_config.json",
		"antigravity-ide/installation_id", "antigravity-ide/builtin/keep.txt",
		"antigravity-ide/builtin/.checksum"} {
		if _, err := os.Stat(filepath.Join(root, file)); err != nil {
			t.Errorf("%s is missing: %v", file, err)
		}
	}
	id, err := os.ReadFile(filepath.Join(root, "antigravity-ide/installation_id"))
	if err != nil {
		t.Fatalf("installation_id: %v", err)
	}
	if strings.HasSuffix(string(id), "\n") {
		t.Error("the installation id carries a trailing newline")
	}
}

// TestWriteDiscoveryNameAndBytes checks the discovery file's name and
// its bytes.
//
// 【一手】 the reference's name is `ls_e3b0c44298fc1c14.json` for an
// empty workspace id — sha256("")[:16].
func TestWriteDiscoveryNameAndBytes(t *testing.T) {
	root := t.TempDir()
	layout := Layout{GeminiDir: root, ConfigDir: filepath.Join(root, "config"), AppDataDir: filepath.Join(root, "antigravity-ide")}
	path, err := layout.WriteDiscovery("", Discovery{PID: 42, HTTPSPort: 1, HTTPPort: 2, LSVersion: "1.11.0"})
	if err != nil {
		t.Fatalf("WriteDiscovery: %v", err)
	}
	if filepath.Base(path) != "ls_e3b0c44298fc1c14.json" {
		t.Errorf("name = %q", filepath.Base(path))
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if strings.HasSuffix(string(body), "\n") {
		t.Error("the discovery file carries a trailing newline")
	}
	if !strings.Contains(string(body), `"pid": 42`) {
		t.Errorf("body = %s", body)
	}
}

// TestWorkspaceDigest checks the digest the file's name carries.
func TestWorkspaceDigest(t *testing.T) {
	cases := map[string]string{
		"":            "e3b0c44298fc1c14",
		"myworkspace": "edf8f80b8c4011f2",
	}
	for id, want := range cases {
		if got := workspaceDigest(id); got != want {
			t.Errorf("workspaceDigest(%q) = %q, want %q", id, got, want)
		}
	}
}
