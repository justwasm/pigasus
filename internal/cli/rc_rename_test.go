// rc_rename_test.go — the hidden `remote-control rename` subcommand.
//
// The rename is the clone's own: the reference has no such
// subcommand — it renames the instance from the Web UI's settings
// panel, and the language server watches the configuration file for
// the change.
//
// 【反编译】 the reference's own model of the change: the CLI writes
// the name into the config proto
// (jetski/cli/remotecontrol.(*Daemon).SetInstanceName at 0x102d35320 →
// config.ApplyProtoUpdate) and the language server reads it back and
// renames the instance over the API
// (remotecontrol.UpdateInstanceMetadata at 0x1025aad50).
//
// The clone keeps the same shape: the configuration file is the source
// of truth, and a running daemon picks the change up.
package cli

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// readConfiguredHostname reads the configured instance name out of the
// configuration file.
func readConfiguredHostname(t *testing.T, configHome string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(configHome, "config.json"))
	if err != nil {
		t.Fatalf("read the config: %v", err)
	}
	var doc struct {
		UserSettings struct {
			Hostname string `json:"cliRemoteControlHostname"`
		} `json:"userSettings"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse the config: %v", err)
	}
	return doc.UserSettings.Hostname
}

// TestRemoteControlRename_WritesTheConfiguration pins the rename.
func TestRemoteControlRename_WritesTheConfiguration(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", configHome)

	out, errb, code := runJetski(t, "remote-control", "rename", "my-machine")
	if code != 0 {
		t.Fatalf("rename exit code = %d, want 0; stderr=%q", code, errb)
	}
	if !strings.Contains(out, "my-machine") {
		t.Errorf("stdout = %q, want it to name the instance", out)
	}
	if got := readConfiguredHostname(t, configHome); got != "my-machine" {
		t.Errorf("the configured hostname is %q, want my-machine", got)
	}
}

// TestRemoteControlRename_KeepsTheConfigurationIntact pins that the
// rename does not drop the file's other members.
func TestRemoteControlRename_KeepsTheConfigurationIntact(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", configHome)
	path := filepath.Join(configHome, "config.json")
	if err := os.WriteFile(path, []byte(`{"userSettings":{"theme":"dark"},"other":1}`), 0o644); err != nil {
		t.Fatalf("seed the config: %v", err)
	}

	if _, errb, code := runJetski(t, "remote-control", "rename", "my-machine"); code != 0 {
		t.Fatalf("rename exit code = %d, want 0; stderr=%q", code, errb)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read the config: %v", err)
	}
	var doc map[string]any
	if err := json.Unmarshal(data, &doc); err != nil {
		t.Fatalf("parse the config: %v", err)
	}
	if _, ok := doc["other"]; !ok {
		t.Errorf("the rename dropped a member: %s", data)
	}
	userSettings, _ := doc["userSettings"].(map[string]any)
	if userSettings["theme"] != "dark" {
		t.Errorf("the rename dropped userSettings.theme: %s", data)
	}
	if userSettings["cliRemoteControlHostname"] != "my-machine" {
		t.Errorf("the configured hostname is %v, want my-machine", userSettings["cliRemoteControlHostname"])
	}
}

// TestRemoteControlRename_RefusesAnEmptyName pins the guard.
func TestRemoteControlRename_RefusesAnEmptyName(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", configHome)

	if _, errb, code := runJetski(t, "remote-control", "rename"); code == 0 {
		t.Errorf("rename without a name exited 0; stderr=%q", errb)
	}
	if _, errb, code := runJetski(t, "remote-control", "rename", "a", "b"); code == 0 {
		t.Errorf("rename with two names exited 0; stderr=%q", errb)
	}
}

// TestRemoteControlRename_RefusesAnInvalidName pins the wording.
//
// The shared validator's own message names the `--name` flag; this
// subcommand takes the name positionally, so it states the rule in its
// own words.
func TestRemoteControlRename_RefusesAnInvalidName(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", configHome)

	_, errb, code := runJetski(t, "remote-control", "rename", "bad name!")
	if code == 0 {
		t.Fatalf("rename with an invalid name exited 0; stderr=%q", errb)
	}
	if !strings.Contains(errb, `invalid instance name "bad name!"`) {
		t.Errorf("stderr = %q, want it to name the instance name", errb)
	}
	if strings.Contains(errb, "--name") {
		t.Errorf("stderr = %q, want it not to name the --name flag", errb)
	}
}

// TestRemoteControlRename_IsHidden pins the visibility rule: the
// reference's `remote-control --help` lists start, status and stop
// only, and that block is the reference's, byte for byte.
//
// The same rule covers the clone's own `serve` and `run`: accepted,
// not advertised.
func TestRemoteControlRename_IsHidden(t *testing.T) {
	_, errb, code := runJetski(t, "remote-control", "--help")
	if code != 0 {
		t.Fatalf("remote-control --help exit code = %d, want 0", code)
	}
	for _, sub := range []string{"start", "status", "stop"} {
		if !strings.Contains(errb, sub) {
			t.Errorf("the usage block is missing %q: %q", sub, errb)
		}
	}
	for _, sub := range []string{"rename", "serve", "run"} {
		if strings.Contains(errb, sub) {
			t.Errorf("the usage block advertises %q, which the reference does not: %q", sub, errb)
		}
	}
}
