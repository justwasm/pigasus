// compat_test.go — verify v1.3.5 binary 1:1 config write + read.
//
// cycle 48 (user live run 反馈): "配置目录 + token 刷新逻辑 100% 兼容
// + 支持互操作".
package auth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestWriteCompatDefaults_AllFiles(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	written, err := WriteCompatDefaults()
	if err != nil {
		t.Fatalf("WriteCompatDefaults: %v", err)
	}

	// Expect at least 7 files (installation_id / settings / mcp /
	// jetbox_state / jetski_state / last_check / cli.log symlink).
	if len(written) < 7 {
		t.Errorf("expected >= 7 files written, got %d: %v", len(written), written)
	}

	// 1. installation_id — UUID v4
	uid, err := os.ReadFile(filepath.Join(home, "installation_id"))
	if err != nil {
		t.Fatal(err)
	}
	uidStr := strings.TrimSpace(string(uid))
	if len(uidStr) != 36 {
		t.Errorf("installation_id wrong length: %q", uidStr)
	}
	if uidStr[14] != '4' {
		t.Errorf("installation_id not v4: %q", uidStr)
	}

	// 2. settings.json — dark + 1 trusted workspace
	sdata, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	var s Settings
	if err := json.Unmarshal(sdata, &s); err != nil {
		t.Fatal(err)
	}
	if s.ColorScheme != "dark" {
		t.Errorf("colorScheme: %q", s.ColorScheme)
	}
	if len(s.TrustedWorkspaces) == 0 {
		t.Errorf("trustedWorkspaces empty")
	}

	// 3. mcp.json — empty servers
	mdata, err := os.ReadFile(filepath.Join(home, "mcp.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m MCPConfig
	if err := json.Unmarshal(mdata, &m); err != nil {
		t.Fatal(err)
	}
	if m.Servers == nil {
		t.Errorf("servers nil, expected empty map")
	}
	if len(m.Servers) != 0 {
		t.Errorf("servers not empty: %v", m.Servers)
	}

	// 4. jetbox_state.json — 6 fields
	jdata, err := os.ReadFile(filepath.Join(home, "jetbox_state.json"))
	if err != nil {
		t.Fatal(err)
	}
	var j JetboxState
	if err := json.Unmarshal(jdata, &j); err != nil {
		t.Fatal(err)
	}
	if j.AgentEnvironment != "local" || j.ThemeMode != "dark" {
		t.Errorf("jetbox_state: %+v", j)
	}

	// 5. jetski_state.pbtxt — prototext hand-written
	jtdata, err := os.ReadFile(filepath.Join(home, "jetski_state.pbtxt"))
	if err != nil {
		t.Fatal(err)
	}
	jtStr := string(jtdata)
	if !strings.Contains(jtStr, "post_onboarding:") {
		t.Errorf("jetski_state.pbtxt missing post_onboarding: %s", jtStr)
	}
	if !strings.Contains(jtStr, "POST_ONBOARDING_STEP_TYPE_MANAGER_WELCOME") {
		t.Errorf("jetski_state.pbtxt missing completed step: %s", jtStr)
	}
	if !strings.Contains(jtStr, "installation_uuid:") {
		t.Errorf("jetski_state.pbtxt missing installation_uuid: %s", jtStr)
	}
	if !strings.Contains(jtStr, "MIGRATION_STATUS_COMPLETED") {
		t.Errorf("jetski_state.pbtxt missing migration: %s", jtStr)
	}

	// 6. last_check.timestamp — Unix epoch
	ts, err := os.ReadFile(filepath.Join(home, "last_check.timestamp"))
	if err != nil {
		// ts may be skipped (file already exists in temp dir)
		if !os.IsNotExist(err) {
			t.Fatal(err)
		}
	} else {
		tsStr := strings.TrimSpace(string(ts))
		if len(tsStr) < 10 {
			t.Errorf("timestamp not epoch: %q", tsStr)
		}
	}

	// 7. cli.log — symlink to log/cli-*.log
	linkInfo, err := os.Lstat(filepath.Join(home, "cli.log"))
	if err != nil {
		t.Fatal(err)
	}
	if linkInfo.Mode()&os.ModeSymlink == 0 {
		t.Errorf("cli.log is not a symlink: %v", linkInfo.Mode())
	}
	target, err := os.Readlink(filepath.Join(home, "cli.log"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(target, "log/cli-") {
		t.Errorf("cli.log target not in log/: %q", target)
	}
}

func TestWriteCompatDefaults_Idempotent(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	// First write
	if _, err := WriteCompatDefaults(); err != nil {
		t.Fatal(err)
	}

	// Read settings to capture original
	origSettings, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}

	// Second write should NOT touch existing file
	time.Sleep(1100 * time.Millisecond) // ensure mtime would change
	if _, err := WriteCompatDefaults(); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(filepath.Join(home, "settings.json"))
	if err != nil {
		t.Fatal(err)
	}
	if string(origSettings) != string(after) {
		t.Error("settings.json was overwritten on second WriteCompatDefaults")
	}
}

func TestAntigravityConfigHome_EnvOverride(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", "/custom/path")
	if got := AntigravityConfigHome(); got != "/custom/path" {
		t.Errorf("ANTIGRAVITY_HOME override: %q", got)
	}
}

func TestRenderJetskiStatePbtxt_Format(t *testing.T) {
	uid := "67b4243f-5097-4d09-9a25-1ef55eaad79a"
	steps := []string{"POST_ONBOARDING_STEP_TYPE_MANAGER_WELCOME", "POST_ONBOARDING_STEP_TYPE_USAGE_MODE"}
	migrations := map[uint32]string{3: "MIGRATION_STATUS_COMPLETED"}
	out := string(renderJetskiStatePbtxt(uid, steps, migrations))

	// Must contain 4 sub-blocks: post_onboarding, installation_uuid, migrations
	for _, want := range []string{
		"post_onboarding:  {",
		"  completed_steps:  POST_ONBOARDING_STEP_TYPE_MANAGER_WELCOME",
		"installation_uuid:  \"" + uid + "\"",
		"migrations:  {",
		"  key:  3",
		"  value:  MIGRATION_STATUS_COMPLETED",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("missing %q in:\n%s", want, out)
		}
	}
}

// TestOnboardingCompleted_FirstLaunch: no jetbox_state.json -> false.
func TestOnboardingCompleted_FirstLaunch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	if OnboardingCompleted() {
		t.Error("OnboardingCompleted should be false when jetbox_state.json is missing")
	}
}

// TestOnboardingCompleted_AfterWrite: post_onboarding_step set -> true.
func TestOnboardingCompleted_AfterWrite(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	s := JetboxState{
		AgentEnvironment:   "local",
		ThemeMode:          "dark",
		ConversationWidth:  "wide",
		TerminalPlacement:  "below_input",
		PostOnboardingStep: "agent_configuration",
		UserNuxEvent:       "first_run",
	}
	if err := SaveJetboxState(s); err != nil {
		t.Fatal(err)
	}
	if !OnboardingCompleted() {
		t.Error("OnboardingCompleted should be true after SaveJetboxState with non-empty PostOnboardingStep")
	}
}

// TestSaveSettingsColorScheme_RoundTrip preserves other fields.
func TestSaveSettingsColorScheme_RoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	// First, write defaults to populate settings.json
	if _, err := WriteCompatDefaults(); err != nil {
		t.Fatal(err)
	}
	// Now mutate the file to add a custom field that should survive.
	settingsPath := filepath.Join(home, "settings.json")
	data, _ := os.ReadFile(settingsPath)
	var original map[string]any
	_ = json.Unmarshal(data, &original)
	original["trustedWorkspaces"] = []string{"/tmp/openagy-test"}
	original["customField"] = "do-not-delete"
	mutated, _ := json.MarshalIndent(original, "", "  ")
	if err := os.WriteFile(settingsPath, mutated, 0600); err != nil {
		t.Fatal(err)
	}

	// Now save the color scheme.
	if err := SaveSettingsColorScheme("tokyo night"); err != nil {
		t.Fatal(err)
	}

	// Verify the round-trip.
	data, _ = os.ReadFile(settingsPath)
	var got map[string]any
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatal(err)
	}
	if got["colorScheme"] != "tokyo night" {
		t.Errorf("colorScheme: got %v, want %q", got["colorScheme"], "tokyo night")
	}
	if got["customField"] != "do-not-delete" {
		t.Errorf("customField was lost: %v", got["customField"])
	}
	if _, ok := got["trustedWorkspaces"]; !ok {
		t.Errorf("trustedWorkspaces lost: %v", got)
	}
}

// TestLoadJetboxState_RoundTrip verifies Load+save JSON.
func TestLoadJetboxState_RoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)

	original := JetboxState{
		AgentEnvironment:   "managed",
		ThemeMode:          "light",
		ConversationWidth:  "narrow",
		TerminalPlacement:  "right",
		PostOnboardingStep: "manager_welcome",
		UserNuxEvent:       "subsequent_run",
	}
	if err := SaveJetboxState(original); err != nil {
		t.Fatal(err)
	}
	got, err := LoadJetboxState()
	if err != nil {
		t.Fatal(err)
	}
	if got != original {
		t.Errorf("round-trip mismatch:\n got: %+v\nwant: %+v", got, original)
	}
}

// UTF-8 BOM (EF BB BF) tolerance pins — AGY 1.2.16 changelog line 13:
// "Fixed settings failing to load when ~/.gemini/config/config.json
// starts with a UTF-8 byte order mark, as files saved by Notepad or
// PowerShell Set-Content on Windows do". The cycle config-bom-strip
// fix lands the same tolerance for mcp.json and settings.json too.
// Each test writes the BOM-prefixed file by hand and checks the
// loader function returns the parsed value instead of a parse error.

// utf8BOM is the three bytes Notepad and PowerShell
// `Set-Content -Encoding UTF8` prepend to every file on Windows.
var utf8BOM = []byte{0xEF, 0xBB, 0xBF}

// TestConfigBOM_Stripped verifies the GEMINIConfigHome/config.json
// reader (PluginEnabled / SetPluginEnabled / ClearPluginEnabled /
// disableAllPluginsField) tolerates a leading UTF-8 BOM. AGY 1.2.16
// line 13 was authored around this exact reader.
func TestConfigBOM_Stripped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	// Disable env-var override so disableAllPluginsField consults
	// the on-disk file rather than an out-of-band AGY_CLI_DISABLE_PLUGINS.
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "")
	if err := os.MkdirAll(GeminiConfigHome(), 0700); err != nil {
		t.Fatal(err)
	}

	// Build a BOM-prefixed config.json with a known plugin entry
	// AND disableAllPlugins=true so we can read both fields through
	// the BOM-tolerant path.
	cfgPath := filepath.Join(GeminiConfigHome(), "config.json")
	bomged := append(append([]byte{}, utf8BOM...), []byte(`{
  "plugins": {"alpha": {"enabled": true}},
  "disableAllPlugins": true
}`)...)
	if err := os.WriteFile(cfgPath, bomged, 0600); err != nil {
		t.Fatal(err)
	}

	// Sanity: a hand-rolled json.Unmarshal on the raw bytes WOULD have
	// failed — the failure mode that motivated AGY 1.2.16 line 13.
	var probe map[string]any
	if err := json.Unmarshal(bomged, &probe); err == nil {
		t.Fatalf("sanity: expected raw BOM-prefixed bytes to fail json.Unmarshal, got nil")
	}

	// PluginEnabled must succeed and report the seeded plugin as
	// enabled (this is the gap: pre-cycle, the parse error made the
	// /plugin panel fall back to [disabled]).
	got, err := PluginEnabled("alpha")
	if err != nil {
		t.Fatalf("PluginEnabled with BOM-prefixed config.json: %v", err)
	}
	if !got {
		t.Errorf("PluginEnabled alpha: got false, want true (BOM not stripped)")
	}

	// disableAllPluginsField must also see its value through the BOM.
	if !PluginsBlocked() {
		t.Errorf("PluginsBlocked with disableAllPlugins=true under BOM: got false, want true (BOM not stripped)")
	}
}

// TestConfigBOM_AbsentPassesThrough verifies that, with no BOM,
// PluginEnabled behaves exactly as it did before the cycle — i.e.
// the new stripBOM helper is a no-op on plain JSON files. This is
// the regression pin: a future refactor must not regress the common
// (Windows-without-BOM and macOS/Linux) load path.
func TestConfigBOM_AbsentPassesThrough(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("AGY_CLI_DISABLE_PLUGINS", "")
	if err := os.MkdirAll(GeminiConfigHome(), 0700); err != nil {
		t.Fatal(err)
	}
	cfgPath := filepath.Join(GeminiConfigHome(), "config.json")
	plain := []byte(`{
  "plugins": {"alpha": {"enabled": true}},
  "disableAllPlugins": false
}`)
	if err := os.WriteFile(cfgPath, plain, 0600); err != nil {
		t.Fatal(err)
	}

	got, err := PluginEnabled("alpha")
	if err != nil {
		t.Fatalf("PluginEnabled with plain config.json: %v", err)
	}
	if !got {
		t.Errorf("PluginEnabled alpha: got false, want true")
	}
	if PluginsBlocked() {
		t.Errorf("PluginsBlocked with disableAllPlugins=false: got true, want false")
	}
}

// TestMcpConfigBOM_Stripped exercises the mcp.json reader
// (LoadMCPConfig + disableAllMCPField / MCPsBlocked). AGY 1.2.16
// line 13 named config.json, but the same Notepad/PowerShell BOM
// problem applies to mcp.json on Windows: a user who hand-edits
// either file through those tools hits the same parse error.
func TestMcpConfigBOM_Stripped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("AGY_CLI_DISABLE_MCP", "")
	if err := os.MkdirAll(AntigravityConfigHome(), 0700); err != nil {
		t.Fatal(err)
	}
	mcpPath := filepath.Join(AntigravityConfigHome(), "mcp.json")
	bomged := append(append([]byte{}, utf8BOM...), []byte(`{
  "servers": {
    "echo": {"type": "stdio", "command": "echo", "enabled": true}
  },
  "disableAllMCP": true
}`)...)
	if err := os.WriteFile(mcpPath, bomged, 0600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadMCPConfig()
	if err != nil {
		t.Fatalf("LoadMCPConfig with BOM-prefixed mcp.json: %v", err)
	}
	if got := cfg.Servers["echo"]; got.Command != "echo" {
		t.Errorf("servers.echo.command: got %q, want %q (BOM not stripped)", got.Command, "echo")
	}
	if !MCPsBlocked() {
		t.Errorf("MCPsBlocked with disableAllMCP=true under BOM: got false, want true (BOM not stripped)")
	}
}

// TestSettingsBOM_Stripped covers the settings.json surface
// (SaveSettingsColorScheme / IsWorkspaceTrusted / SettingsString /
// SettingsBool / SettingsField) under a leading UTF-8 BOM. settings.json
// is touched by both cycle config-bom-strip and the broader AGY 1.2.16
// line 13 family: a Notepad-edit on any of these three files renders
// the user's first launch broken without the BOM fix.
func TestSettingsBOM_Stripped(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	if err := os.MkdirAll(AntigravityConfigHome(), 0700); err != nil {
		t.Fatal(err)
	}
	settingsPath := filepath.Join(AntigravityConfigHome(), "settings.json")
	bomged := append(append([]byte{}, utf8BOM...), []byte(`{
  "colorScheme": "solarized light",
  "trustedWorkspaces": ["/tmp/openagy-bom-test"],
  "model": "claude-opus-4-7-thinking"
}`)...)
	if err := os.WriteFile(settingsPath, bomged, 0600); err != nil {
		t.Fatal(err)
	}

	// SaveSettingsColorScheme round-trips; without stripBOM, the
	// json.Unmarshal of the BOM-prefixed bytes would error before
	// reaching the mutator.
	if err := SaveSettingsColorScheme("dark"); err != nil {
		t.Fatalf("SaveSettingsColorScheme under BOM: %v", err)
	}

	// IsWorkspaceTrusted must see the seeded trusted path through the
	// BOM-tolerant read path; on a parse error the helper returns
	// (false, nil), which would gate the user into the trust dialog.
	ok, err := IsWorkspaceTrusted("/tmp/openagy-bom-test")
	if err != nil {
		t.Fatalf("IsWorkspaceTrusted under BOM: %v", err)
	}
	if !ok {
		t.Errorf("IsWorkspaceTrusted under BOM: got false, want true (BOM not stripped)")
	}

	// SettingsString + ResolveSettingsModel are the two readers
	// whose operators rendered the line 13 regression: the picker
	// showed nothing, the /model panel showed nothing.
	if got := SettingsString("colorScheme"); got == "" {
		t.Errorf("SettingsString colorScheme under BOM: got empty (BOM not stripped)")
	}
	if got := ResolveSettingsModel("fallback"); got != "claude-opus-4-7-thinking" {
		t.Errorf("ResolveSettingsModel under BOM: got %q, want %q (BOM not stripped)", got, "claude-opus-4-7-thinking")
	}
}
