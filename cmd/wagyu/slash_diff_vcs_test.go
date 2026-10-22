// slash_cycle207_test.go — cycle 207 regression tests for the re-diffed
// /usage, /changelog and /diff viewers, plus byte pinning for /rewind and
// /credits (which already matched REF 1.4.0 and must keep matching).
//
// The expected strings are transcribed from the 2026-10-17 `agy207` probe
// captures under /private/var/folders/.../opencode/agy207/. Where a capture
// shows a tmux-normalised 256-colour index (38;5;69 / 38;2;101;123;131) the test
// uses the recovered truecolour constant (cBlueOpen / cDimGrayOpen), which
// renders to the same cell.
package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// ---------------------------------------------------------------- /usage

// usageDoc is REF's 20-line document at a normal width: two groups of six
// lines separated by two blanks, then the four-line wrapped note.
func TestCycle207UsageDocGeometry(t *testing.T) {
	doc := usageDoc(220)
	if len(doc) != 20 {
		t.Fatalf("usageDoc(220) = %d lines, want 20", len(doc))
	}
	if doc[0] != "\x1b[1mGEMINI MODELS\x1b[0m" {
		t.Errorf("doc[0] = %q", doc[0])
	}
	if doc[3] != "  \x1b[1mWeekly Limit Remaining\x1b[0m" {
		t.Errorf("doc[3] = %q, want a 2-space-indented header", doc[3])
	}
	// Group separation: two blanks between the groups and before the note.
	if doc[6] != "" || doc[7] != "" {
		t.Errorf("doc[6:8] = %q, want two blanks", doc[6:8])
	}
	if doc[14] != "" || doc[15] != "" {
		t.Errorf("doc[14:16] = %q, want two blanks before the note", doc[14:16])
	}
	for i, want := range []string{
		"Within each group, models share a weekly limit. Quota is consumed",
		"proportionally to the cost of the tokens. Thus, limits will last longer with",
		"shorter tasks or using more cost-effective models. Your weekly limit is tied",
		"directly to your individual tier.",
	} {
		line := doc[16+i]
		if !strings.HasPrefix(line, "  \x1b[38;2;238;232;213m│\x1b[38;2;101;123;131m") {
			t.Fatalf("note line %d prefix = %q", i, line)
		}
		if !strings.Contains(line, want) {
			t.Errorf("note line %d = %q, want %q", i, line, want)
		}
		if !strings.HasSuffix(line, "\x1b[39m") {
			t.Errorf("note line %d should end with a reset: %q", i, line)
		}
	}
	// A narrow terminal wraps the note further, growing the document; the
	// counter's total follows the document length, not a constant.
	if n := len(usageDoc(60)); n != 21 {
		t.Errorf("usageDoc(60) = %d lines, want 21", n)
	}
}

// usageBar renders REF's "[green fill|gray remainder] NN.NN%" bar.
func TestCycle207UsageBar(t *testing.T) {
	// 66.65% of 50 cells rounds to 33 — exactly ref_usage_220x50.raw.txt row 21.
	got := usageBar(66.65, 50)
	want := "    [\x1b[32m" + strings.Repeat("█", 33) + "\x1b[38;2;101;123;131m" + strings.Repeat("░", 17) +
		"\x1b[39m]\x1b[38;2;101;123;131m 66.65%\x1b[39m"
	if got != want {
		t.Errorf("usageBar(66.65,50):\n got %q\nwant %q", got, want)
	}
	// 0% emits no green SGR at all (row 29).
	got0 := usageBar(0, 50)
	want0 := "    [\x1b[38;2;101;123;131m" + strings.Repeat("░", 50) + "\x1b[39m]\x1b[38;2;101;123;131m 0.00%\x1b[39m"
	if got0 != want0 {
		t.Errorf("usageBar(0,50):\n got %q\nwant %q", got0, want0)
	}
	// 100% emits no gray remainder SGR.
	got100 := usageBar(100, 50)
	want100 := "    [\x1b[32m" + strings.Repeat("█", 50) + "\x1b[39m]\x1b[38;2;101;123;131m 100.00%\x1b[39m"
	if got100 != want100 {
		t.Errorf("usageBar(100,50):\n got %q\nwant %q", got100, want100)
	}
}

// usagePage is height-13: 11 rows at h=24, 15 at h=28, 20 at h=33.
func TestCycle207UsagePage(t *testing.T) {
	for _, tc := range []struct{ h, want int }{{24, 11}, {28, 15}, {33, 20}, {40, 27}, {50, 37}} {
		if got := usagePage(tc.h); got != tc.want {
			t.Errorf("usagePage(%d) = %d, want %d", tc.h, got, tc.want)
		}
	}
}

// The /usage panel shows a counter only while the document is taller than
// the window, and the counter sits directly under the window.
func TestCycle207UsageViewerHeight(t *testing.T) {
	// h=50: the 20-line document fits in 37 rows — no counter, 26 rows total.
	m := mkPanel(panelUsage, "/usage", 0)
	m.width, m.height = 220, 50
	lines := m.panelLines()
	if len(lines) != 26 {
		t.Fatalf("h=50 panel = %d rows, want 26", len(lines))
	}
	for _, l := range lines {
		if strings.Contains(stripSGR(l), "of 20 lines") {
			t.Errorf("h=50 must not show a counter: %q", l)
		}
	}
	if !strings.Contains(stripSGR(strings.Join(lines, "\n")), "directly to your individual tier.") {
		t.Error("h=50 should show the whole note")
	}

	// h=24: window 11 of 20 — counter (1–11 of 20 lines), 18 rows total.
	m2 := mkPanel(panelUsage, "/usage", 0)
	m2.width, m2.height = 220, 24
	lines2 := m2.panelLines()
	if len(lines2) != 18 {
		t.Fatalf("h=24 panel = %d rows, want 18", len(lines2))
	}
	if !strings.Contains(stripSGR(strings.Join(lines2, "\n")), "(1–11 of 20 lines)") {
		t.Errorf("h=24 counter wrong:\n%s", strings.Join(plainLines(m2), "\n"))
	}

	// Scrolling follows the window: one Down moves the counter to (2–12).
	if handled, _ := m2.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	if m2.panel.window != 1 {
		t.Fatalf("window = %d, want 1", m2.panel.window)
	}
	if !strings.Contains(stripSGR(strings.Join(m2.panelLines(), "\n")), "(2–12 of 20 lines)") {
		t.Errorf("counter did not follow:\n%s", strings.Join(plainLines(m2), "\n"))
	}
	// ctrl+end lands on the last window (offset 9 → (10–20 of 20 lines)).
	m2.handlePanelKey(tea.Key{Code: tea.KeyEnd, Mod: tea.ModCtrl})
	if m2.panel.window != 9 {
		t.Fatalf("ctrl+end window = %d, want 9", m2.panel.window)
	}
	if !strings.Contains(stripSGR(strings.Join(m2.panelLines(), "\n")), "(10–20 of 20 lines)") {
		t.Errorf("bottom counter wrong:\n%s", strings.Join(plainLines(m2), "\n"))
	}
}

// wrapWords greedily wraps on spaces at the requested width.
func TestCycle207WrapWords(t *testing.T) {
	got := wrapWords(usageNoteText, 76)
	want := []string{
		"Within each group, models share a weekly limit. Quota is consumed",
		"proportionally to the cost of the tokens. Thus, limits will last longer with",
		"shorter tasks or using more cost-effective models. Your weekly limit is tied",
		"directly to your individual tier.",
	}
	if len(got) != len(want) {
		t.Fatalf("wrapWords(.,76) = %d lines, want %d: %q", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("line %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// ---------------------------------------------------------------- /diff

// diffAllChangesLines reproduces REF's All Changes list byte for byte
// (modulo the colour constants): the fixture is repo3, i.e. the data behind
// diff3.ref.raw.txt.
func TestCycle207DiffAllChanges(t *testing.T) {
	tracked := []diffChange{
		{path: "del.txt", deleted: 2},
		{path: "mod.txt", added: 2, deleted: 1},
	}
	untracked := []diffChange{
		{path: "sub/deep.txt", added: 1, untracked: true},
		{path: "untracked.txt", added: 3, untracked: true},
	}
	lines := diffAllChangesLines(tracked, untracked, 0)

	wantPlain := []string{
		"Diff (git)  All Changes  Per Turn  Commit Tree",
		"  4 file(s) changed",
		"",
		"> del.txt  -2",
		"  mod.txt  +2 -1",
		"",
		"  ? [Untracked] sub/deep.txt  +1",
		"  ? [Untracked] untracked.txt  +3",
		"",
		"  ←/→ switch view · ↑/↓/j/k navigate · enter view diff · esc dismiss",
	}
	if len(lines) != len(wantPlain) {
		t.Fatalf("lines = %d, want %d:\n%q", len(lines), len(wantPlain), lines)
	}
	for i, want := range wantPlain {
		if got := stripSGR(lines[i]); got != want {
			t.Errorf("line %d: got %q, want %q", i, got, want)
		}
	}
	// Exact SGR on the cursor row, the untracked badge and the counts.
	if !strings.HasPrefix(lines[0], "\x1b[1mDiff (git)\x1b[0m  "+cBlueOpen+"All Changes"+cBlueClose) {
		t.Errorf("title row = %q", lines[0])
	}
	if lines[3] != cBlueOpen+"> del.txt"+cBlueClose+"  "+cToolBulletErrOpen+"-2\x1b[39m" {
		t.Errorf("cursor row = %q", lines[3])
	}
	if !strings.Contains(lines[6], "\x1b[38;5;172m? [Untracked] \x1b[39m") {
		t.Errorf("untracked badge = %q", lines[6])
	}
	// Moving the cursor to the first untracked row re-colours it (diff3_dd).
	cursorLine := diffAllChangesLines(tracked, untracked, 2)
	if !strings.Contains(cursorLine[6], cBlueOpen+"> "+"\x1b[38;5;172m? [Untracked] "+cBlueOpen+"sub/deep.txt"+cBlueClose) {
		t.Errorf("untracked cursor row = %q", cursorLine[6])
	}
}

// The clean All Changes / Per Turn bodies are the indented empty states with
// the short legend.
func TestCycle207DiffEmptyStates(t *testing.T) {
	clean := diffAllChangesLines(nil, nil, 0)
	wantClean := []string{
		" Diff (git)  All Changes  Per Turn  Commit Tree",
		"",
		"   No uncommitted changes",
		"",
		"   ←/→ switch view · esc dismiss",
	}
	if len(clean) != len(wantClean) {
		t.Fatalf("clean = %d lines, want %d: %q", len(clean), len(wantClean), clean)
	}
	for i, want := range wantClean {
		if got := stripSGR(clean[i]); got != want {
			t.Errorf("clean line %d: got %q, want %q", i, got, want)
		}
	}
	// The clean title carries the one-space margin REF shows.
	if !strings.HasPrefix(clean[0], " \x1b[1mDiff (git)\x1b[0m") {
		t.Errorf("clean title = %q", clean[0])
	}
	// Per Turn goes through panelDiffLinesVCS.
	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd, m.panel.tab = "/tmp/nowhere-cycle207", 1
	pt := m.panelDiffLinesVCS()
	if got := stripSGR(pt[2]); got != "   No file changes in conversation" {
		t.Errorf("per-turn empty = %q", got)
	}
}

// diffMoveCursor clamps over the flattened tracked+untracked list.
func TestCycle207DiffMoveCursor(t *testing.T) {
	// Non-git cwd: inert.
	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = "/tmp/nowhere-cycle207"
	m.diffMoveCursor(1)
	if m.panel.cursor != 0 {
		t.Errorf("cursor moved in a non-repo: %d", m.panel.cursor)
	}
	// Per Turn tab: inert.
	m.panel.tab = 1
	m.diffMoveCursor(1)
	if m.panel.cursor != 0 {
		t.Errorf("cursor moved on Per Turn: %d", m.panel.cursor)
	}
}

// gitDiffChanges parses a real repo the way REF's list needs it: tracked
// files with numstat counts, untracked files with their line counts.
func TestCycle207DiffGitChanges(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	git := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	write := func(rel, content string) {
		t.Helper()
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	git("init", "-q")
	write("del.txt", "a\nb\n")
	write("mod.txt", "line1\nline2\n")
	git("add", "-A")
	git("commit", "-qm", "init")

	if err := os.Remove(filepath.Join(dir, "del.txt")); err != nil {
		t.Fatal(err)
	}
	write("mod.txt", "line1\nCHANGED\n")
	write("sub/deep.txt", "d\n")
	write("untracked.txt", "u1\nu2\nu3\n")

	tracked, untracked, ok := gitDiffChanges(dir)
	if !ok {
		t.Fatal("gitDiffChanges reported not-a-repo")
	}
	if len(tracked) != 2 || len(untracked) != 2 {
		t.Fatalf("tracked=%v untracked=%v", tracked, untracked)
	}
	if tracked[0].path != "del.txt" || tracked[0].deleted != 2 || tracked[0].added != 0 {
		t.Errorf("tracked[0] = %+v, want del.txt -2", tracked[0])
	}
	if tracked[1].path != "mod.txt" || tracked[1].added != 1 || tracked[1].deleted != 1 {
		t.Errorf("tracked[1] = %+v, want mod.txt +1 -1", tracked[1])
	}
	if untracked[0].path != "sub/deep.txt" || untracked[0].added != 1 || !untracked[0].untracked {
		t.Errorf("untracked[0] = %+v, want sub/deep.txt +1", untracked[0])
	}
	if untracked[1].path != "untracked.txt" || untracked[1].added != 3 {
		t.Errorf("untracked[1] = %+v, want untracked.txt +3", untracked[1])
	}
}

// A non-repo cwd keeps the no-VCS fallback; the commit tree renders the
// single-commit shape captured for REF.
func TestCycle207DiffCommitTree(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	dir := t.TempDir()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=t", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=t", "GIT_COMMITTER_EMAIL=t@t")
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init", "-q")
	if err := os.WriteFile(filepath.Join(dir, "f.txt"), []byte("x\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "-A")
	run("commit", "-qm", "init")

	m := mkPanel(panelDiff, "/diff", 0)
	m.cwd = dir
	m.panel.tab = 2
	lines := m.panelDiffLinesVCS()
	plain := make([]string, len(lines))
	for i, l := range lines {
		plain[i] = stripSGR(l)
	}
	if plain[1] != "   1 commit(s) available" {
		t.Errorf("commit count row = %q", plain[1])
	}
	if plain[3] != " ▌ *  Working Copy (Uncommitted Changes)" {
		t.Errorf("working-copy row = %q", plain[3])
	}
	if !strings.HasPrefix(plain[5], "   * ") || !strings.HasSuffix(plain[5], " init") {
		t.Errorf("commit row = %q", plain[5])
	}
}

// ------------------------------------------------------------ /changelog

// The /changelog title is indented and carries REF's 48;5;63 background,
// followed by a blank row.
func TestCycle207ChangelogTitle(t *testing.T) {
	excerpt := changelogExcerptRows()
	want := "  \x1b[1m\x1b[38;5;228m\x1b[48;5;63m Pigasus CLI Changelog \x1b[0m"
	if excerpt[0] != want {
		t.Errorf("title = %q, want %q", excerpt[0], want)
	}
	if excerpt[1] != "" {
		t.Errorf("excerpt[1] = %q, want a blank row", excerpt[1])
	}
	if !strings.Contains(stripSGR(excerpt[2]), "The terminal-first surface") {
		t.Errorf("excerpt[2] = %q", excerpt[2])
	}
}

// A sized /changelog window is height-4 lines of the document.
func TestCycle207ChangelogHeight(t *testing.T) {
	m := mkPanel(panelChangelog, "/changelog", 0)
	m.height = 24
	spec, ok := m.viewerSpecFor()
	if !ok {
		t.Fatal("changelog is not a viewer")
	}
	if spec.visible != 20 {
		t.Errorf("visible = %d, want 20 (h-4)", spec.visible)
	}
	if spec.total != 2014 {
		t.Errorf("total = %d, want 2014", spec.total)
	}
	if !strings.Contains(stripSGR(strings.Join(m.panelLines(), "\n")), "(1–20 of 2014 lines)") {
		t.Errorf("counter wrong:\n%s", strings.Join(plainLines(m), "\n"))
	}
}

// ------------------------------------------------ /rewind, /credits pin

// /rewind and /credits already matched REF 1.4.0 byte for byte; these pin
// the exact rows so a later edit cannot silently regress them.
func TestCycle207RewindCreditsRegression(t *testing.T) {
	rewind := mkPanel(panelRewind, "/rewind", 0).panelLines()
	assertLines(t, rewind, []string{
		"\x1b[1m\x1b[39mRewind Conversation\x1b[0m",
		"",
		cBlueOpen + "> (current)" + cBlueClose,
		"",
		panelKeyboardHint("↑/↓", "Navigate", "enter", "Select", "esc", "Cancel"),
	})

	credits := mkPanel(panelCredits, "/credits", 0).panelLines()
	assertLines(t, credits, []string{
		cBlueOpen + "Pigasus CLI" + cBlueClose + "  " +
			"\x1b[1m" + panelGray238 + "credits" + "\x1b[0m",
		"\x1b[1m" + panelGray238 + "Model AI Credits" + "\x1b[0m",
		"",
		"\x1b[1mRemaining AI Credits\x1b[0m: " +
			"\x1b[1m" + cBlueOpen + "AI Credits not enabled (enable in /settings)" + "\x1b[0m",
		"",
		"\x1b[1m" + panelGray238 + "Actions" + "\x1b[0m",
		"",
		cBlueOpen + "> Get More AI Credits" + cBlueClose,
		"  See Activity",
		"",
		panelKeyboardHint("↑/↓", "Navigate", "enter", "Select / Toggle", "esc", "Close"),
	})
}
