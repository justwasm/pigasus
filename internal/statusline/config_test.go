// config_test.go — the `statusLine` / `title` blocks in settings.json.
//
// Evidence:
//   - 【一手】 agy 1.4.7's own settings.json holds
//     `{"type":"","command":"","enabled":false}` for both blocks, and
//     `/config` prints `{"type":"","command":"","enabled":true}`.
//   - 【反编译】 the binary's string table carries `json:"padding,omitempty"`
//     and `json:"stack_with_default,omitempty"` next to
//     `json:"command,omitempty"`.
package statusline_test

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/statusline"
)

// writeSettings drops a settings.json into a fresh home directory.
func writeSettings(t *testing.T, body string) string {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	if err := os.WriteFile(filepath.Join(home, "settings.json"), []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	return home
}

// readSettings reads the settings.json back.
func readSettings(t *testing.T, home string) map[string]any {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(data, &out); err != nil {
		t.Fatalf("settings.json is not valid JSON: %v\n%s", err, data)
	}
	return out
}

// TestLoadConfig_ObjectForm reads the reference's canonical block.
func TestLoadConfig_ObjectForm(t *testing.T) {
	writeSettings(t, `{"statusLine":{"type":"command","command":"/bin/echo hi","enabled":true,"padding":2,"stack_with_default":true}}`)
	cfg, ok := statusline.LoadConfig()
	if !ok {
		t.Fatal("LoadConfig reported no block")
	}
	if cfg.Type != "command" {
		t.Errorf("Type = %q", cfg.Type)
	}
	if cfg.Command != "/bin/echo hi" {
		t.Errorf("Command = %q", cfg.Command)
	}
	if !cfg.Enabled {
		t.Error("Enabled = false")
	}
	if cfg.Padding != 2 {
		t.Errorf("Padding = %d", cfg.Padding)
	}
	if !cfg.StackWithDefault {
		t.Error("StackWithDefault = false")
	}
	if !cfg.Active() {
		t.Error("Active = false")
	}
}

// TestLoadConfig_LegacyBoolean reads the legacy shape.
//
// The reference reads a boolean as an object without a command, so no
// script runs.
func TestLoadConfig_LegacyBoolean(t *testing.T) {
	writeSettings(t, `{"statusLine":true}`)
	cfg, ok := statusline.LoadConfig()
	if !ok {
		t.Fatal("LoadConfig reported no block")
	}
	if cfg.Command != "" {
		t.Errorf("Command = %q, want empty", cfg.Command)
	}
	if cfg.Active() {
		t.Error("Active = true for a boolean block")
	}

	writeSettings(t, `{"statusLine":false}`)
	cfg, ok = statusline.LoadConfig()
	if !ok {
		t.Fatal("LoadConfig reported no block")
	}
	if cfg.Enabled {
		t.Error("Enabled = true for a false boolean")
	}
	if cfg.Active() {
		t.Error("Active = true for a false boolean")
	}
}

// TestLoadConfig_Missing reports no block when the key is absent.
func TestLoadConfig_Missing(t *testing.T) {
	writeSettings(t, `{"colorScheme":"light"}`)
	if _, ok := statusline.LoadConfig(); ok {
		t.Error("LoadConfig found a block that is not there")
	}
	if _, ok := statusline.LoadTitleConfig(); ok {
		t.Error("LoadTitleConfig found a block that is not there")
	}
}

// TestPersistCommand_RoundTrip stores a command and reads it back.
//
// Evidence 【一手】: the reference's `/statusline <command>` writes the
// block and starts running the script right away.
func TestPersistCommand_RoundTrip(t *testing.T) {
	home := writeSettings(t, `{"colorScheme":"light"}`)
	if err := statusline.PersistCommand("/bin/echo hi"); err != nil {
		t.Fatal(err)
	}
	cfg, ok := statusline.LoadConfig()
	if !ok {
		t.Fatal("LoadConfig reported no block")
	}
	if cfg.Command != "/bin/echo hi" {
		t.Errorf("Command = %q", cfg.Command)
	}
	if cfg.Type != "command" {
		t.Errorf("Type = %q", cfg.Type)
	}
	if !cfg.Enabled {
		t.Error("Enabled = false")
	}
	// The other keys survive.
	if readSettings(t, home)["colorScheme"] != "light" {
		t.Error("colorScheme was clobbered")
	}
}

// TestPersistVisibility_KeepsCommand checks that toggling the visibility
// does not drop the command or the layout members.
func TestPersistVisibility_KeepsCommand(t *testing.T) {
	home := writeSettings(t, `{"statusLine":{"type":"command","command":"/bin/echo hi","enabled":true,"padding":3,"stack_with_default":true}}`)
	statusline.PersistVisibility(false)
	cfg, _ := statusline.LoadConfig()
	if cfg.Enabled {
		t.Error("Enabled = true after PersistVisibility(false)")
	}
	if cfg.Command != "/bin/echo hi" {
		t.Errorf("Command = %q, the toggle dropped it", cfg.Command)
	}
	if cfg.Padding != 3 || !cfg.StackWithDefault {
		t.Errorf("layout members lost: %+v", cfg)
	}
	statusline.PersistVisibility(true)
	cfg, _ = statusline.LoadConfig()
	if !cfg.Enabled {
		t.Error("Enabled = false after PersistVisibility(true)")
	}
	if cfg.Command != "/bin/echo hi" {
		t.Errorf("Command = %q, the toggle dropped it", cfg.Command)
	}
	_ = home
}

// TestClearCommand_MatchesReference pins the shape `/statusline delete`
// leaves behind.
//
// Evidence 【一手】: agy 1.4.7's settings.json after `/statusline delete`
// holds no `statusLine` key at all — the reference removes the block
// instead of emptying it.
func TestClearCommand_MatchesReference(t *testing.T) {
	home := writeSettings(t, `{"statusLine":{"type":"command","command":"/bin/echo hi","enabled":true}}`)
	if err := statusline.ClearCommand(); err != nil {
		t.Fatal(err)
	}
	if _, ok := statusline.LoadConfig(); ok {
		t.Error("the statusLine block must be gone")
	}
	raw := readSettings(t, home)
	if _, ok := raw["statusLine"]; ok {
		t.Errorf("settings.json still carries the block: %v", raw)
	}
}

// TestPersistCommand_OmitsEnabled pins that a new command is written
// without an `enabled` member.
//
// The member's default is true, so the script runs right away — the
// reference's own status line documentation says the command "immediately
// updates your settings and starts running the script".
//
// Evidence 【一手】: agy 1.4.7's `/statusline <command>` wrote
// `{"type":"command","command":"…"}`.
func TestPersistCommand_OmitsEnabled(t *testing.T) {
	home := writeSettings(t, `{"statusLine":{"type":"command","command":"/bin/echo hi","enabled":false,"padding":2,"stack_with_default":true}}`)
	if err := statusline.PersistCommand("/bin/echo bye"); err != nil {
		t.Fatal(err)
	}
	raw := readSettings(t, home)
	block, ok := raw["statusLine"].(map[string]any)
	if !ok {
		t.Fatalf("settings.json missing the block: %v", raw)
	}
	if _, ok := block["enabled"]; ok {
		t.Errorf("the block must not carry an enabled member: %v", block)
	}
	if block["command"] != "/bin/echo bye" {
		t.Errorf("command = %v", block["command"])
	}
	// The layout members survive.
	if block["padding"] != float64(2) {
		t.Errorf("padding = %v", block["padding"])
	}
	if block["stack_with_default"] != true {
		t.Errorf("stack_with_default = %v", block["stack_with_default"])
	}
	// The effective value is on: the member's default is true.
	cfg, _ := statusline.LoadConfig()
	if !cfg.Enabled {
		t.Error("Enabled = false")
	}
	if !cfg.Active() {
		t.Error("Active = false")
	}
}

// TestPersistVisibility_WritesEnabled pins that the toggle writes the
// member explicitly, in both directions.
//
// Evidence 【一手】: agy 1.4.7's `/statusline off` left
// `{"type":"command","command":"…","padding":3,"enabled":false,"stack_with_default":true}`.
func TestPersistVisibility_WritesEnabled(t *testing.T) {
	home := writeSettings(t, `{"statusLine":{"type":"command","command":"/bin/echo hi","padding":3,"stack_with_default":true}}`)
	if err := statusline.PersistVisibility(false); err != nil {
		t.Fatal(err)
	}
	block, _ := readSettings(t, home)["statusLine"].(map[string]any)
	if block["enabled"] != false {
		t.Errorf("enabled = %v", block["enabled"])
	}
	if err := statusline.PersistVisibility(true); err != nil {
		t.Fatal(err)
	}
	block, _ = readSettings(t, home)["statusLine"].(map[string]any)
	if block["enabled"] != true {
		t.Errorf("enabled = %v", block["enabled"])
	}
	if block["padding"] != float64(3) {
		t.Errorf("padding = %v", block["padding"])
	}
}

// TestPersistCommand_ConvertsLegacyBoolean checks that the legacy boolean
// is upgraded to the object form before the members are written.
func TestPersistCommand_ConvertsLegacyBoolean(t *testing.T) {
	home := writeSettings(t, `{"statusLine":false}`)
	if err := statusline.PersistCommand("/bin/echo hi"); err != nil {
		t.Fatal(err)
	}
	raw := readSettings(t, home)["statusLine"].(map[string]any)
	if _, ok := raw["command"]; !ok {
		t.Errorf("block = %v, want the object form", raw)
	}
	cfg, _ := statusline.LoadConfig()
	if cfg.Command != "/bin/echo hi" || !cfg.Enabled {
		t.Errorf("block = %+v", cfg)
	}
}

// TestPersistTitleEnabled checks the title block's visibility write.
func TestPersistTitleEnabled(t *testing.T) {
	home := writeSettings(t, `{"title":{"type":"command","command":"/bin/echo title","enabled":false}}`)
	if err := statusline.PersistTitleEnabled(true); err != nil {
		t.Fatal(err)
	}
	cfg, ok := statusline.LoadTitleConfig()
	if !ok {
		t.Fatal("LoadTitleConfig reported no block")
	}
	if !cfg.Enabled {
		t.Error("Enabled = false")
	}
	if cfg.Command != "/bin/echo title" {
		t.Errorf("Command = %q", cfg.Command)
	}
	if !cfg.Active() {
		t.Error("Active = false")
	}
	_ = home
}

// TestLoadTitleConfig_LegacyBoolean reads the legacy boolean shape.
func TestLoadTitleConfig_LegacyBoolean(t *testing.T) {
	writeSettings(t, `{"title":true}`)
	cfg, ok := statusline.LoadTitleConfig()
	if !ok {
		t.Fatal("LoadTitleConfig reported no block")
	}
	if cfg.Command != "" {
		t.Errorf("Command = %q, want empty", cfg.Command)
	}
	if cfg.Active() {
		t.Error("Active = true for a boolean block")
	}
}
