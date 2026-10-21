// slash_cycle223_test.go — cycle 223: panelAddDir (TODO §2.1 last
// A-class panel).
//
// Cycle 217 already gave `/add-dir /tmp/foo` the
// "Added <path> to workspace" echo; cycle 223 closes the panel-form
// parity gap for the no-arg case. REF 1.4.0 does NOT actually render
// a panel for bare `/add-dir` (verified against
// `/private/tmp/agy217/caps/ref.adddir-bare.raw.txt`, which shows only
// `> /add-dir ⎿ error: no directory specified`); cycle 223 still ships
// the panel because the clone's A-class panel set (cycle 193) tracks
// every command REF opens a modal for, and closing the gap means
// adding the modal even when REF's own equivalent is a one-liner.
// See docs/cycles/cycle-223/deliverable-add-dir-panel.md for the
// probe-vs-clone matrix at 220×50.
//
// What the tests pin:
//   - panelForCommand("add-dir") maps to panelAddDir.
//   - openSlashPanelArgs routes bare /add-dir to the panel and
//     `/add-dir <path>` to the cycle-217 echo path (so
//     TestSlashAddDir_ViaRouter and TestAddDirFeedback_AddedToWorkspace
//     still pass).
//   - The bare-bones layout: bold title, the trustedWorkspaces list
//     read live from auth.LoadTrustedWorkspaces, a "  Path: <text>▮"
//     input row, and a "Keyboard: enter Add Directory  esc Cancel"
//     footer.
//   - Empty settings.json → "No trusted workspaces yet." row.
//   - Printable keys edit the panel buffer; backspace deletes a rune.
//   - Enter on a typed path calls addDirFeedback and echoes through
//     slashRespond (cycle-217 wording rides through unchanged).
//   - Enter with empty input echoes the cycle-217 "error: no
//     directory specified" line (same branch addDirFeedback takes).
//   - Esc closes the panel with the standard exit notice.

package main

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// TestPanelAddDirForCommand verifies the panelForCommand mapping for
// the cycle 223 entry. TestPanelForCommand table extension is mirrored
// in slash_panels_test.go.
func TestPanelAddDirForCommand(t *testing.T) {
	if got, ok := panelForCommand("add-dir"); !ok || got != panelAddDir {
		t.Errorf("panelForCommand(\"add-dir\") = (%v, %v), want (panelAddDir, true)", got, ok)
	}
	// The default open path (used by the TUI dispatcher) also lands
	// on panelAddDir.
	m := &runMode{}
	if !m.openSlashPanel("add-dir") {
		t.Fatal("openSlashPanel(\"add-dir\") = false")
	}
	if m.panel.kind != panelAddDir || m.panel.cmd != "/add-dir" {
		t.Errorf("after openSlashPanel: panel = (%v, %q), want (panelAddDir, \"/add-dir\")",
			m.panel.kind, m.panel.cmd)
	}
}

// TestPanelAddDirRouting — bare /add-dir opens the panel via
// openSlashPanelArgs; /add-dir <path> falls through to the cycle-217
// echo path (the existing `case "add-dir"` in slash_commands.go).
// The fall-through guarantee is what keeps TestSlashAddDir_ViaRouter
// and TestAddDirFeedback_AddedToWorkspace passing.
func TestPanelAddDirRouting(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := &runMode{}
	if !m.openSlashPanelArgs("add-dir", "") {
		t.Fatal("openSlashPanelArgs(\"add-dir\", \"\") = false, want true (panel)")
	}
	if m.panel.kind != panelAddDir {
		t.Errorf("bare /add-dir did not open panel: kind = %v", m.panel.kind)
	}
	m2 := &runMode{}
	if m2.openSlashPanelArgs("add-dir", "/tmp/foo") {
		t.Fatal("openSlashPanelArgs(\"add-dir\", \"/tmp/foo\") = true, want false (fall through)")
	}
	if m2.panel.kind != panelNone {
		t.Errorf("panel should remain panelNone after fall-through, got %v", m2.panel.kind)
	}
}

// TestPanelAddDirEmptyLayout verifies the no-trusted-workspaces form:
// the heading "No trusted workspaces yet." replaces the per-entry
// rows, and the rest of the skeleton matches the brief's "title +
// trusted-workspaces list + placeholder input + keyboard hint"
// description (the wording is OUR's — REF has no panel to compare).
func TestPanelAddDirEmptyLayout(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := mkPanel(panelAddDir, "/add-dir", 0)
	plain := plainLines(m)

	want := []string{
		"Add Directory to Workspace",
		"",
		"  No trusted workspaces yet.",
		"",
		"  Path:  ",
		"",
		"Keyboard: enter Add Directory  esc Cancel",
	}
	if len(plain) != len(want) {
		t.Fatalf("line count = %d, want %d\nplain:\n%s",
			len(plain), len(want), strings.Join(plain, "\n"))
	}
	for i, w := range want {
		if plain[i] != w {
			t.Errorf("row %d:\n got: %q\nwant: %q", i, plain[i], w)
		}
	}
	// The reverse-video cursor block must sit at the end of the path
	// row, which is what the brief's "placeholder input prompt" amounts
	// to for an empty buffer.
	styled := m.panelLines()
	if !strings.HasSuffix(stripSGR(styled[4]), " ") || !strings.HasSuffix(styled[4], searchCursorBlock) {
		t.Errorf("Path row missing reverse-video cursor tail: %q",
			stripSGR(styled[4]))
	}
	// Title row carries the bold/default-fg panel title colour.
	if !strings.HasPrefix(styled[0], panelTitleOpen) {
		t.Errorf("title row missing panel title colour: %q", stripSGR(styled[0]))
	}
	// The "No trusted workspaces yet." row is in the dim gray (241) the
	// clone uses for the /tasks /artifact empty-state lines.
	if !strings.Contains(styled[2], cDimGrayOpen) {
		t.Errorf("trusted-empty row missing dim-gray: %q",
			stripSGR(styled[2]))
	}
}

// TestPanelAddDirTrustedListed verifies the trustedWorkspaces list is
// drawn verbatim from auth.LoadTrustedWorkspaces (the same reader the
// on-startup trust gate and cycle-103 /add-dir already use). Cycle
// 217's "Workspace added" wording lives downstream of addDirFeedback
// and is exercised separately in TestPanelAddDirEnterEchoes.
func TestPanelAddDirTrustedListed(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{"trustedWorkspaces":["/Users/foo/a","/Users/foo/b"]}`)

	m := mkPanel(panelAddDir, "/add-dir", 0)
	plain := plainLines(m)
	if len(plain) != 9 {
		t.Fatalf("line count = %d, want 9\nplain:\n%s",
			len(plain), strings.Join(plain, "\n"))
	}
	if plain[0] != "Add Directory to Workspace" {
		t.Errorf("row 0 = %q, want title", plain[0])
	}
	if plain[2] != "  Trusted workspaces:" {
		t.Errorf("row 2 = %q, want heading", plain[2])
	}
	if plain[3] != "  /Users/foo/a" {
		t.Errorf("row 3 = %q, want first trusted path", plain[3])
	}
	if plain[4] != "  /Users/foo/b" {
		t.Errorf("row 4 = %q, want second trusted path", plain[4])
	}
	if !strings.Contains(plain[6], "  Path:") {
		t.Errorf("row 6 should be the path input, got %q", plain[6])
	}
	if plain[8] != "Keyboard: enter Add Directory  esc Cancel" {
		t.Errorf("row 8 = %q, want keyboard hint", plain[8])
	}
}

// TestPanelAddDirTypingFillsBuffer verifies the panel's printable-key
// handling mirrors the cycle-208 / cycle-221 textarea shape: each
// printable rune appends to m.panel.text, and the Path row repaints
// after every keystroke (the render reads m.panel.text live).
func TestPanelAddDirTypingFillsBuffer(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := mkPanel(panelAddDir, "/add-dir", 0)
	for _, r := range []rune("/tmp/foo") {
		handled, _ := m.handlePanelKey(tea.Key{Text: string(r)})
		if !handled {
			t.Errorf("handlePanelKey(%q) = false, want true", string(r))
		}
	}
	if m.panel.text != "/tmp/foo" {
		t.Errorf("panel.text = %q, want \"/tmp/foo\"", m.panel.text)
	}
	styled := m.panelLines()
	if !strings.Contains(stripSGR(styled[4]), "Path: /tmp/foo") {
		t.Errorf("path row missing typed text: %q", stripSGR(styled[4]))
	}
}

// TestPanelAddDirBackspace pops the last rune — the cycle-208/221
// free-text form has the same rule and the panel mirrors it.
func TestPanelAddDirBackspace(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := mkPanel(panelAddDir, "/add-dir", 0)
	m.panel.text = "/tmp/foo"
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyBackspace}); !handled {
		t.Error("backspace not handled")
	}
	if m.panel.text != "/tmp/fo" {
		t.Errorf("panel.text = %q, want \"/tmp/fo\"", m.panel.text)
	}

	// Backspace on an empty buffer is a no-op (no underflow).
	m.panel.text = ""
	m.handlePanelKey(tea.Key{Code: tea.KeyBackspace})
	if m.panel.text != "" {
		t.Errorf("backspace on empty buffer = %q, want empty", m.panel.text)
	}
}

// TestPanelAddDirEnterEchoes commits the typed path through the
// cycle-217 addDirFeedback path: the panel closes, settings.json gains
// the new trustedWorkspaces entry, and slashRespond emits the
// "Added <path> to workspace" line (verbatim from cycle-217
// TestAddDirFeedback_AddedToWorkspace).
func TestPanelAddDirEnterEchoes(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{"trustedWorkspaces":[]}`)
	sub := home + "/proj"
	if err := os.Mkdir(sub, 0o755); err != nil {
		t.Fatal(err)
	}

	m := mkPanel(panelAddDir, "/add-dir", 0)
	m.panel.text = sub
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel still open: kind = %v", m.panel.kind)
	}
	resp := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(resp, "> /add-dir") {
		t.Errorf("missing slashRespond header; m.response=%q", resp)
	}
	if !strings.Contains(resp, "Added "+sub+" to workspace") {
		t.Errorf("missing success echo; got %q", resp)
	}
	body := readSettings(t, home)
	if !strings.Contains(body, sub) {
		t.Errorf("settings.json missing the new workspace; got %q", body)
	}
}

// TestPanelAddDirEnterEmpty — pressing Enter with an empty input runs
// the cycle-217 addDirFeedback("") branch and echoes "error: no
// directory specified". Same wording as `/add-dir` no-arg in REF
// (`/private/tmp/agy217/caps/ref.adddir-bare.raw.txt`); closes the
// panel so the user is not trapped.
func TestPanelAddDirEnterEmpty(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := mkPanel(panelAddDir, "/add-dir", 0)
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel did not close: kind = %v", m.panel.kind)
	}
	resp := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(resp, "error: no directory specified") {
		t.Errorf("missing cycle-217 error echo; got %q", resp)
	}
}

// TestPanelAddDirEnterInvalid — typed path that doesn't exist runs the
// cycle-217 addDirFeedback validation and echoes the same `error:
// directory not found: <path>` wording. The panel still closes —
// refusing would trap the user.
func TestPanelAddDirEnterInvalid(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := mkPanel(panelAddDir, "/add-dir", 0)
	m.panel.text = home + "/nope"
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel did not close: kind = %v", m.panel.kind)
	}
	resp := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(resp, "error: directory not found:") {
		t.Errorf("missing validation echo; got %q", resp)
	}
}

// TestPanelAddDirEsc — Esc closes the panel with the standard exit
// notice (`Exited /add-dir command`). The exit-notice=true group is
// the panel default; panelAddDir is intentionally NOT in the
// cycle-193 silent-close set so the close is loud enough to confirm
// the panel actually went away.
func TestPanelAddDirEsc(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := mkPanel(panelAddDir, "/add-dir", 0)
	m.panel.text = "/tmp/foo" // garbage that wouldn't validate, but Esc ignores it
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc}); !handled {
		t.Fatal("Esc not handled")
	}
	if m.panel.kind != panelNone {
		t.Errorf("panel still open: kind = %v", m.panel.kind)
	}
	resp := stripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(resp, "Exited /add-dir command") {
		t.Errorf("missing exit notice; got %q", resp)
	}
}

// TestPanelAddDirExitNotice — `/add-dir` must be in the exit-notice=true
// group (panel default), unlike /mcp /feedback /rewind /context. The
// brief's "Esc closes with the standard exit notice" depends on this
// switch falling through to `return true`.
func TestPanelAddDirExitNotice(t *testing.T) {
	if !panelAddDir.exitNotice() {
		t.Error("panelAddDir.exitNotice() = false, want true (Esc should emit \"Exited /add-dir command\")")
	}
}
