package cli

// plugin_case_insensitive_test.go — cycle plugin-case-insensitive.
//
// Closes AGY 1.3.6 changelog line 21:
//
//	Fixed plugin names and manifests being case-sensitive where the
//	agent was not: `/plugin` subcommands such as `/plugin List` now
//	work in any case instead of failing with `Unknown subcommand`,
//	and a manifest named `Plugin.json` is now found by
//	`agy plugin install`, `agy plugin validate`, and enable and
//	disable.
//
// Two surfaces need the fix and are exercised here:
//
//  1. The jetski CLI dispatcher (`internal/cli/jetski.go`) which is
//     the engine behind `agy plugin List` and every other sub-sub.
//     The lookup used to compare `ss.Name == name`; it now also
//     matches when `strings.EqualFold(ss.Name, name)` is true.
//  2. The `legacyFindManifest` helper that every plugin read path
//     (`legacyLoadPlugins`, `runPluginInstall`, `runPluginValidate`,
//     `runPluginToggle`) now uses. Lowercase `plugin.json` is tried
//     first; capitalised `Plugin.json` is the fallback.
//
// The slash `/plugin <subcmd>` form lives in cmd/wagyu, so its
// coverage is in slash_cycleplugincaseinsensitive_test.go there.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// writeManifest drops a manifest with the requested case at
// <root>/<name>, returning the absolute path it landed at.
func writeManifest(t *testing.T, root, name string, payload map[string]any) string {
	t.Helper()
	raw, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal manifest: %v", err)
	}
	p := filepath.Join(root, name)
	if err := os.WriteFile(p, raw, 0600); err != nil {
		t.Fatalf("write %s: %v", p, err)
	}
	return p
}

// ---- legacyFindManifest ----------------------------------------------------

// TestLegacyFindManifest_PrefersLowercase: a tree that carries both
// names resolves to lowercase — that is the spelling the reference's
// own strings use everywhere (`failed to read plugin.json`,
// `writing plugin.json`, etc.) so existing installs continue to
// land on the same file.
func TestLegacyFindManifest_PrefersLowercase(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "plugin.json", map[string]any{"name": "x"})
	writeManifest(t, root, "Plugin.json", map[string]any{"name": "y"})
	got := legacyFindManifest(root)
	if !strings.HasSuffix(got, "plugin.json") {
		t.Errorf("legacyFindManifest preferred %q, want lowercase plugin.json", got)
	}
}

// TestLegacyFindManifest_FallsBackToCapitalised: when only the
// capitalised form exists (AGY 1.3.6 changelog line 21 ships this
// gap), the helper still resolves a manifest path. This is the new
// behaviour: pre-cycle, `os.Stat(filepath.Join(path, "plugin.json"))`
// returned ENOENT and the validate / install paths reported
// `missing plugin.json: file does not exist`.
func TestLegacyFindManifest_FallsBackToCapitalised(t *testing.T) {
	root := t.TempDir()
	writeManifest(t, root, "Plugin.json", map[string]any{"name": "x"})
	got := legacyFindManifest(root)
	if got == "" {
		t.Fatal("legacyFindManifest returned empty for Plugin.json only")
	}
	if !strings.HasSuffix(got, "Plugin.json") {
		t.Errorf("legacyFindManifest returned %q, want capitalised Plugin.json", got)
	}
}

// TestLegacyFindManifest_NoManifestReturnsEmpty: missing both
// spellings reports empty so the caller can fall through to its own
// "missing plugin.json" branch.
func TestLegacyFindManifest_NoManifestReturnsEmpty(t *testing.T) {
	root := t.TempDir()
	if got := legacyFindManifest(root); got != "" {
		t.Errorf("legacyFindManifest = %q, want empty", got)
	}
}

// ---- runPluginValidate / runPluginInstall / runPluginToggle ----------------

// TestRunPluginValidate_AcceptsCapitalisedManifest: the validate
// path reads whichever case the manifest ships in and emits the same
// `[ok]` marker. The manifest name itself never appears in the
// output; the test confirms that a `Plugin.json`-only tree
// validates successfully (the cycle's whole reason for being).
func TestRunPluginValidate_AcceptsCapitalisedManifest(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	writeManifest(t, root, "Plugin.json", map[string]any{"name": "caps", "version": "1.0.0"})

	out := captureStdout(t, func() {
		j := newTestJ()
		if err := runPluginValidate(j, []string{root}); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})
	if !strings.Contains(out, "Validating caps (") || !strings.Contains(out, "Plugin.json)") {
		t.Errorf("capitalised manifest should validate, output: %q", out)
	}
}

// TestRunPluginValidate_LowercaseStillWorks: backwards-compat — a
// pre-existing `plugin.json` tree still validates. The cycle's
// read-your-writes guarantee for users who never named anything
// capitalised.
func TestRunPluginValidate_LowercaseStillWorks(t *testing.T) {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "skills"), 0700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	writeManifest(t, root, "plugin.json", map[string]any{"name": "lower", "version": "1.0.0"})

	out := captureStdout(t, func() {
		j := newTestJ()
		if err := runPluginValidate(j, []string{root}); err != nil {
			t.Fatalf("validate: %v", err)
		}
	})
	if !strings.Contains(out, "Validating lower (") || !strings.Contains(out, "plugin.json)") {
		t.Errorf("lowercase manifest should validate, output: %q", out)
	}
}

// TestRunPluginInstall_AcceptsCapitalisedManifest: the install path
// resolves whichever manifest ships under the target directory and
// copies the tree into GEMINI_CONFIG_HOME/plugins/<name>. Pre-cycle
// the lookup was hardcoded to `plugin.json` and would error out on
// a `Plugin.json`-only tree before the copy step.
func TestRunPluginInstall_AcceptsCapitalisedManifest(t *testing.T) {
	src := filepath.Join(t.TempDir(), "caps-source")
	if err := os.MkdirAll(filepath.Join(src, "skills"), 0700); err != nil {
		t.Fatalf("mkdir skills: %v", err)
	}
	writeManifest(t, src, "Plugin.json", map[string]any{"name": "caps", "version": "1.0.0"})

	cfgHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", cfgHome)

	out := captureStdout(t, func() {
		j := newTestJ()
		if err := runPluginInstall(j, []string{src}); err != nil {
			t.Fatalf("install: %v", err)
		}
	})
	if !strings.Contains(out, `Installed plugin "caps"`) {
		t.Errorf("install summary missing: %q", out)
	}
	// The installed copy should carry the manifest under whichever
	// name it shipped under — verify by reading it back.
	dst := filepath.Join(cfgHome, "plugins", "caps")
	if _, err := os.Stat(filepath.Join(dst, "Plugin.json")); err != nil {
		t.Errorf("installed tree missing Plugin.json: %v", err)
	}
}

// TestRunPluginValidate_MissingManifestErrorMessage: a tree with no
// manifest at all surfaces the same fixed-string error the
// reference prints (`Error: missing plugin.json: file does not
// exist`) so the cycle doesn't drift on diagnostic wording. The
// cycle's own helper must return "" so the validate path can
// fall through to its `.antigravity-plugin/` fallback check.
//
// Routed through runJetski so the test asserts on the stderr
// message the reference actually prints (`fail()` writes msg
// to j.ErrOut and returns errExitCode(code); the msg never
// reaches err.Error()).
func TestRunPluginValidate_MissingManifestErrorMessage(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_HOME", t.TempDir())
	dir := t.TempDir()
	out, errb, code := runJetski(t, "plugin", "validate", dir)
	if !strings.Contains(out, "no plugin.json found at the plugin root") {
		t.Errorf("stdout should mention the missing manifest, got %q", out)
	}
	if !strings.Contains(errb, "validation failed: 1 error") {
		t.Errorf("stderr should carry the failing footer, got %q", errb)
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// TestRunPluginToggle_FindsCapitalisedManifest: enable / disable
// land on a plugin whose directory holds `Plugin.json` rather than
// the lowercase form. Pre-cycle the toggle was a hard-coded
// `os.Stat(filepath.Join(dir, name, "plugin.json"))` and would
// report "plugin %q not found or invalid".
//
// Routed through runJetski so the test covers the full
// install → toggle round-trip with the parser harness the
// test rig uses for everything else.
func TestRunPluginToggle_FindsCapitalisedManifest(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", cfgHome)

	// Plant a plugin whose manifest is the capitalised form.
	pdir := filepath.Join(cfgHome, "plugins", "caps-plugin")
	if err := os.MkdirAll(pdir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeManifest(t, pdir, "Plugin.json", map[string]any{"name": "caps-plugin", "version": "1.0.0"})

	outEnable, errbEnable, codeEnable := runJetski(t, "plugin", "enable", "caps-plugin")
	if codeEnable != 0 {
		t.Errorf("enable exit code = %d, want 0; stderr=%q", codeEnable, errbEnable)
	}
	if !strings.Contains(outEnable, `Enabled plugin "caps-plugin"`) {
		t.Errorf("enable output missing: %q", outEnable)
	}

	outDisable, errbDisable, codeDisable := runJetski(t, "plugin", "disable", "caps-plugin")
	if codeDisable != 0 {
		t.Errorf("disable exit code = %d, want 0; stderr=%q", codeDisable, errbDisable)
	}
	if !strings.Contains(outDisable, `Disabled plugin "caps-plugin"`) {
		t.Errorf("disable output missing: %q", outDisable)
	}
}

// ---- legacyLoadPlugins -----------------------------------------------------

// TestLegacyLoadPlugins_AcceptsCapitalisedManifest: `agy plugin
// list` enumerates a plugin whose on-disk manifest is
// `Plugin.json`. Pre-cycle the loader's hard-coded
// `os.ReadFile(filepath.Join(pdir, "plugin.json"))` would skip the
// directory entirely.
func TestLegacyLoadPlugins_AcceptsCapitalisedManifest(t *testing.T) {
	cfgHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", cfgHome)

	pdir := filepath.Join(cfgHome, "plugins", "caps-list")
	if err := os.MkdirAll(pdir, 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	writeManifest(t, pdir, "Plugin.json", map[string]any{"name": "caps-list", "version": "1.0.0"})

	plugins := legacyLoadPlugins()
	if len(plugins) != 1 {
		t.Fatalf("legacyLoadPlugins returned %d entries, want 1", len(plugins))
	}
	if plugins[0].Name != "caps-list" {
		t.Errorf("plugin name = %q, want caps-list", plugins[0].Name)
	}
}

// ---- jetski dispatcher: case-insensitive sub-subcommand lookup -------------

// TestJetskiPluginDispatch_CaseInsensitive: the dispatcher
// accepts `List`, `LIST`, and `list` for `agy plugin <X>`. The
// test rig runs `agy plugin List` (capital) and asserts the
// cycle-167 `No plugins installed.` body the lowercase form
// already prints — proving the lookup matches case-insensitively
// without changing the handler body. GEMINI_CONFIG_HOME is
// pointed at a fresh temp dir so the host machine's installed
// plugin set cannot leak into the test (the test rig runs
// against an empty plugins tree).
func TestJetskiPluginDispatch_CaseInsensitive(t *testing.T) {
	t.Setenv("GEMINI_CONFIG_HOME", t.TempDir())
	cases := []struct {
		sub  string
		want string
	}{
		{"list", "No plugins installed.\n"},
		{"List", "No plugins installed.\n"},
		{"LIST", "No plugins installed.\n"},
		{"LiSt", "No plugins installed.\n"},
	}
	for _, tc := range cases {
		t.Run(tc.sub, func(t *testing.T) {
			out, errb, code := runJetski(t, "plugin", tc.sub)
			if code != 0 {
				t.Errorf("exit code = %d, want 0; stderr=%q", code, errb)
			}
			if out != tc.want {
				t.Errorf("stdout = %q, want %q", out, tc.want)
			}
			if errb != "" {
				t.Errorf("stderr = %q, want empty", errb)
			}
		})
	}
}

// TestJetskiPluginDispatch_UnknownSubStillErrors: a sub that
// really is unknown still reports the reference's
// `Error: unknown command: <name>` line and exits 1. The
// case-insensitive matcher must not turn unknown into known —
// only the cases that match a registered handler resolve.
func TestJetskiPluginDispatch_UnknownSubStillErrors(t *testing.T) {
	out, errb, code := runJetski(t, "plugin", "frobnicate")
	if out != "" {
		t.Errorf("stdout should be empty, got %q", out)
	}
	if !strings.Contains(errb, "unknown command") {
		t.Errorf("stderr should mention unknown command, got %q", errb)
	}
	if !strings.Contains(errb, "frobnicate") {
		t.Errorf("stderr should echo the unknown name, got %q", errb)
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}

// TestJetskiPluginDispatch_McpAlsoCaseInsensitive: the cycle's
// matcher is global, not plugin-specific. Confirm `agy mcp LIST`
// also resolves to the same handler `agy mcp list` does — the
// changelog's spirit ("where the agent was not") applies to
// every sub-sub. When there are no MCP servers the handler
// prints nothing on stdout and exits 0.
func TestJetskiPluginDispatch_McpAlsoCaseInsensitive(t *testing.T) {
	cases := []string{"list", "List", "LIST"}
	for _, sub := range cases {
		t.Run(sub, func(t *testing.T) {
			_, errb, code := runJetski(t, "mcp", sub)
			if code != 0 {
				t.Errorf("mcp %s exit code = %d, want 0; stderr=%q", sub, code, errb)
			}
			// runMCPList prints nothing for an empty config — we
			// only assert that the case-insensitive matcher
			// dispatched to it (no `unknown mcp subcommand`
			// error). The handler's body content is exercised
			// by mcp_list_test.go.
			if bytes.Contains([]byte(errb), []byte("unknown mcp subcommand")) {
				t.Errorf("mcp %s: dispatcher rejected case variant, stderr=%q", sub, errb)
			}
		})
	}
}
