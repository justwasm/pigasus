package main

// slash_cycle220_test.go — cycle 220: the Discover tab's
// `+ Install from local directory...` prompt, multi-subdirectory Tab
// completion (and the explicit closure of cycle 212's 2nd 存疑).
//
// Every expectation below is pinned to the live reference (Antigravity
// CLI 1.4.0), driven at 220x50 through a private tmux socket; the
// probe scripts and transcripts live in
// /private/var/folders/.../opencode/agy220/ and the cycle report is
// docs/cycles/cycle-220/deliverable-plugin-multi-subdir.md.
//
// What this cycle measures and pins:
//
//   - When the typed leaf has ≥1 matching sibling under its parent, the
//     reference expands to the alphabetically-first match, appended with
//     a trailing slash so the cursor is positioned for the next level.
//     (Probes probe_multi_tab.py and probe_lcp.py: typing
//     `/tmp/agy220dirs/u` resolves to `/tmp/agy220dirs/ugo/`, never
//     `/tmp/agy220dirs/u*` cycling through `umbra` / `unicorn` /
//     `unknown`.)
//
//   - Subsequent Tab presses are no-ops once the path is resolved — the
//     cycle-212 hypothesis that "the reference cycles a list of
//     subdirectories" is RETRACTED. The cycle-220 probes
//     (probe_multi_tab.py, probe_lcp.py, probe_single_match.py,
//     probe_complete.py, probe_diag.py) capture every Tab in 100 ms
//     increments up to 2 s after the press; the prompt never deviates
//     from the alphabetically-first match.
//
//   - The single-match case is "first match + /" — same rule. (probe
//     `probe_single_match.py`: typing `/tmp/agy220single/only` resolves
//     to `/tmp/agy220single/onlyone/`; the cycle-212 test
//     `TestCycle212_PromptKeyLifecycle` is updated to expect that
//     trailing slash.)
//
//   - No `[N/M]` cycling affordance is painted after the path — the
//     reference shows nothing appended to the cursor space the prompt
//     reserves for hints (probe `probe_hint.py`).

import (
	"os"
	"path/filepath"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestCycle220_MultiSubdirTabFirstMatch pins the multi-candidate case:
// typing a leaf that matches several siblings resolves to the first
// alphabetical match with a trailing slash, NOT the longest common
// prefix (cycle 212's incorrect hypothesis).
func TestCycle220_MultiSubdirTabFirstMatch(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"ugo", "umbra", "unicorn", "unknown"} {
		if err := os.Mkdir(filepath.Join(parent, name), 0700); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	got := completeLocalInstallPath(filepath.Join(parent, "u"))
	want := filepath.Join(parent, "ugo") + "/"
	if got != want {
		t.Fatalf("complete = %q, want %q", got, want)
	}
	// A second Tab is a no-op — the prompt is already resolved.
	for i := 0; i < 5; i++ {
		again := completeLocalInstallPath(got)
		if again != got {
			t.Fatalf("Tab #%d changed the path: %q → %q", i+1, got, again)
		}
	}
}

// TestCycle220_LongerPrefixedLeaf keeps the LCP case honest: when
// `/tmp/.../un_al` already has a 5-character prefix and the matches
// disagree past that point (un_alias, un_aliases, un_align), the
// reference still picks the alphabetically-first full match — NOT the
// "un_al" longest common prefix, NOT "un_ali" (LCP of the un_* names).
// Probe `probe_lcp.py`.
func TestCycle220_LongerPrefixedLeaf(t *testing.T) {
	parent := t.TempDir()
	for _, name := range []string{"un_alias", "un_aliases", "un_align"} {
		if err := os.Mkdir(filepath.Join(parent, name), 0700); err != nil {
			t.Fatalf("mkdir %s: %v", name, err)
		}
	}
	got := completeLocalInstallPath(filepath.Join(parent, "un_al"))
	want := filepath.Join(parent, "un_alias") + "/"
	if got != want {
		t.Fatalf("complete = %q, want %q (reference picks first alphabetical match, NOT LCP)",
			got, want)
	}
}

// TestCycle220_SingleMatchAlsoAppendsSlash pins the trailing-slash rule
// for the unique-match case (probe `probe_single_match.py`).
func TestCycle220_SingleMatchAlsoAppendsSlash(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "onlyone"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got := completeLocalInstallPath(filepath.Join(parent, "only"))
	want := filepath.Join(parent, "onlyone") + "/"
	if got != want {
		t.Fatalf("complete = %q, want %q", got, want)
	}
}

// TestCycle220_ExistingDirectoryGetsTrailingSlash verifies the case
// where the typed path already resolves to a directory and there are no
// matching siblings below it: Tab still appends "/" so the cursor is
// positioned for the next level (probe `probe_complete.py`).
func TestCycle220_ExistingDirectoryGetsTrailingSlash(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "leaf"), 0700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	got := completeLocalInstallPath(filepath.Join(parent, "leaf"))
	want := filepath.Join(parent, "leaf") + "/"
	if got != want {
		t.Fatalf("complete = %q, want %q", got, want)
	}
}

// TestCycle220_TabNoOpOnUnresolvablePath verifies that an unknown path
// still does nothing on Tab — the "Directory not found" hint stays
// (probe `probe_existing_dir.py`).
func TestCycle220_TabNoOpOnUnresolvablePath(t *testing.T) {
	got := completeLocalInstallPath("/no/such/path/atall-xyz")
	if got != "/no/such/path/atall-xyz" {
		t.Fatalf("complete = %q, want unchanged", got)
	}
}

// TestCycle220_PromptKeyLifecycleOverTabKeys drives Tab through the
// public key handler end-to-end. After the first Tab, the prompt holds
// the resolved first-match + "/"; subsequent Tab presses leave the
// query alone. Cycle 212's `TestCycle212_PromptKeyLifecycle` covers the
// unique-prefix shape — this test covers the multi-match one.
func TestCycle220_PromptKeyLifecycleOverTabKeys(t *testing.T) {
	cycle212Root(t)
	parent := t.TempDir()
	for _, name := range []string{"alpha_plugin", "alpha_other", "alpha_x"} {
		if err := os.Mkdir(filepath.Join(parent, name), 0700); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
	}

	m := mkPanel(panelPlugin, "/plugin", 0)
	m.panel.pluginTab = panelDiscover
	m.openPluginLocalInstall()
	m.panel.pluginLocalInstall.query = filepath.Join(parent, "alpha")
	// First Tab: alphabetical-first expansion. Of the names above,
	// "alpha_other", "alpha_plugin", "alpha_x" sorted → first is
	// "alpha_other".
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyTab}); !handled {
		t.Fatal("first Tab did not report handled")
	}
	want := filepath.Join(parent, "alpha_other") + "/"
	if got := m.panel.pluginLocalInstall.query; got != want {
		t.Fatalf("Tab #1 = %q, want %q", got, want)
	}

	// Three more Tabs: no cycling. Each must report handled (the prompt
	// owns every keypress while active) but the query must not change.
	for i := 2; i <= 4; i++ {
		if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyTab}); !handled {
			t.Fatalf("Tab #%d did not report handled", i)
		}
		if got := m.panel.pluginLocalInstall.query; got != want {
			t.Fatalf("Tab #%d changed the query: %q → %q", i, want, got)
		}
	}
}
