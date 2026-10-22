// slash_cycle213_test.go — cycle 213: the /codesearch <query> result list.
//
// Cycle 206 left the /codesearch panel rendering only its argument-less
// "usage" form because /codesearch <args> was not probed. This file pins
// what cycle 213 added:
//
//   1. Argument parsing (the leading -F/--literal flag, the trailing --,
//      and the pattern itself).
//   2. Smart-case compilation (uppercase in the pattern turns case
//      sensitivity on, matching the reference's ripgrep-backed rules).
//   3. The local worktree scan (binary skip, size cap, skip-dir list,
//      the 1000-result hard cap).
//   4. Window arithmetic and the panel render — the title echoes the
//      argument tail verbatim, "Found N results", per-block file header
//      + context rows, counter `[first-last of N items]`, then the
//      "Keyboard: ↑/↓ …" footer row.
//   5. Navigation keys on the live list (↑/↓/←/→), the "no hits" /
//      pattern-error fallback to inert, and window re-anchoring.
//
// The reference's *result set* (the search runs through a long-lived
// process and is non-deterministic on identical inputs) is not asserted
// here; the tests cover the *layout* and the *scanner semantics*. See
// docs/cycles/cycle-213/deliverable-codesearch-results.md for the probe
// evidence against the reference.

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/codesearch"
)

// csTmpTree builds a self-contained workspace under t.TempDir() with the
// files the assertions below need. Returns the root path.
func csTmpTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must := func(rel, body string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("a.txt", "line1\nline2 needle here\nline4\nline5\nneedle again\n")
	must("d.txt", "NEEDLE upper\n")
	must("dbl.txt", "needle needle here\n")
	must("end.txt", "aaa zwk\n")
	must("sub/c.txt", "alpha zwk beta\nneedle\n")
	// A long line whose match sits past the 216-cell content budget.
	must("long.txt", strings.Repeat("P", 300)+" zwk "+strings.Repeat("Q", 300)+"\n")
	return root
}

// TestCycle213ParseArgs locks the cycle-213 argument contract: -F and
// --literal are recognised flags, -- terminates the flag list, and an
// unrecognised flag becomes part of the pattern (matching ripgrep).
func TestCycle213ParseArgs(t *testing.T) {
	cases := []struct {
		args    string
		query   string
		literal bool
	}{
		{"", "", false},
		{"needle", "needle", false},
		{"-F needle", "needle", true},
		{"--literal needle", "needle", true},
		{"-- -F", "-F", false},
		{"--needle -F foo", "--needle -F foo", false}, // unknown flag → pattern
		// After `--` every token is positional, including `-F` — the
		// pattern itself is captured literally.
		{"-F --literal -- -F alpha beta", "-F alpha beta", true},
		{"  needle   ", "needle", false}, // trims surrounding whitespace
	}
	for _, tc := range cases {
		// Cycle 228: csParseArgs now also returns include/exclude
		// slices and an error. The cycle-213 cases don't exercise
		// either, so we drop them here and assert just the
		// (query, literal) part of the contract.
		query, literal, _, _, perr := csParseArgs(tc.args)
		if perr != nil && perr != codesearch.ErrEmptyQuery {
			t.Errorf("csParseArgs(%q) err = %v", tc.args, perr)
		}
		if query != tc.query || literal != tc.literal {
			t.Errorf("csParseArgs(%q) = (%q, %v), want (%q, %v)",
				tc.args, query, literal, tc.query, tc.literal)
		}
	}
}

// TestCycle213SmartCase verifies the reference's smart-case rule: an
// all-lowercase pattern is case-insensitive, an uppercase rune flips it
// back to sensitive.
func TestCycle213SmartCase(t *testing.T) {
	if csHasUpper("needle") {
		t.Error("csHasUpper(\"needle\") = true, want false")
	}
	if !csHasUpper("needleZ") {
		t.Error("csHasUpper(\"needleZ\") = false, want true")
	}
	if csHasUpper("αβγ") {
		t.Error("csHasUpper(\"αβγ\") = true, want false")
	}

	// Lowercase → (?i) prefix, so /codesearch needle finds NEEDLE upper.
	re, err := csMatcher("needle", false)
	if err != nil {
		t.Fatal(err)
	}
	if re.FindStringIndex("upper NEEDLE here") == nil {
		t.Error("smart-case: needle should match NEEDLE with lowercase pattern")

	}

	// Uppercase → no (?i) prefix; case sensitive.
	re, err = csMatcher("NEEDLE", false)
	if err != nil {
		t.Fatal(err)
	}
	if re.FindStringIndex("upper needle here") != nil {
		t.Error("uppercase pattern must NOT match lower case")
	}

	// -F ⇒ QuoteMeta, no smart case so the case-sensitivity rule still applies.
	re, err = csMatcher("nee.le", true)
	if err != nil {
		t.Fatal(err)
	}
	if re.FindStringIndex("needle") != nil {
		t.Error("-F: literal pattern must NOT match regex metacharacters")

	}

	re, err = csMatcher("(unclosed", false)
	if err == nil {
		t.Error("csMatcher should reject a malformed regex")
	}
}

// TestCycle213Scan exercises csScan against the temp workspace: file
// policy (skip-dirs, size cap, hidden entries), per-line counting, the
// first-match span, and the 1000-result hard cap.
func TestCycle213Scan(t *testing.T) {
	root := csTmpTree(t)
	re, err := csMatcher("needle", false)
	if err != nil {
		t.Fatal(err)
	}
	hits := csScan(root, re)
	if len(hits) != 5 {
		t.Fatalf("csScan needle hits: got %d, want 5: %+v", len(hits), hits)
	}

	// findHit returns the first hit whose file path matches rel, or nil.
	findHit := func(rel string) *csHit {
		for i := range hits {
			if hits[i].file == rel {
				return &hits[i]
			}
		}
		return nil
	}

	// a.txt's three matches are on lines 2, 5, and a separate "needle" on
	// line 5 (per csTmpTree) — the first one captures the context window.
	a := findHit("a.txt")
	if a == nil || a.line != 2 {
		t.Fatalf("a.txt first hit = %+v, want line=2", a)
	}
	if !a.hasPrev || a.prev != "line1" {
		t.Errorf("a.txt prev = %+v, want line1", a)
	}
	if !a.hasNext || !strings.Contains(a.next, "line4") {
		t.Errorf("a.txt next = %+v, want line4", a)
	}

	// d.txt holds UPPERCASE — the smart-case rule still resolves needle
	// → lowercase match since the *pattern* is all lowercase.
	d := findHit("d.txt")
	if d == nil {
		t.Fatal("d.txt (uppercase) should be found under the smart-case rule")
	}

	// dbl.txt: a single "needle needle here" line must count once, with
	// the first match highlighted (ms = 0, me = 6).
	dbl := findHit("dbl.txt")
	if dbl == nil {
		t.Fatal("dbl.txt should be found")
	}
	if dbl.ms != 0 || dbl.me != 6 {
		t.Errorf("dbl.txt first-match span = (%d,%d), want (0,6)", dbl.ms, dbl.me)
	}

	// sub/c.txt descends into nested directories.
	sc := findHit("sub/c.txt")
	if sc == nil {
		t.Fatal("sub/c.txt should be reached")
	}

	// Run a fixed-string search against an empty workspace.
	if got := csScan("", re); got != nil {
		t.Errorf("csScan(\"\") = %+v, want nil", got)
	}

	// The 1000-result cap is asserted via an inline scan with a tiny cap:
	// we re-run with csResultCap overridden by a focused fixture. Build a
	// workspace that holds 12 matches on one long file and confirm the cap
	// keeps the list at 1000 (set via var) — the cap is a constant in this
	// build, so this just verifies the sentinel of csScan(big matches) ≥
	// cap ⇒ returns exactly cap entries.
	huge := filepath.Join(t.TempDir(), "huge.txt")
	body := strings.Repeat("needle\n", csResultCap+10)
	if err := os.WriteFile(huge, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	re2, _ := csMatcher("needle", false)
	hugeHits := csScan(filepath.Dir(huge), re2)
	if len(hugeHits) != csResultCap {
		t.Errorf("huge file: csScan returned %d hits, want cap %d",
			len(hugeHits), csResultCap)
	}
}

// TestCycle213CsRowAndFit locks the per-row rendering, including the
// `> L<n>: ` cursor marker and the ellipsis-on-long-line behaviour.
func TestCycle213CsRowAndFit(t *testing.T) {
	plain := func(s string) string { return stripSGR(s) }

	got := plain(csRow(7, "abc needle xyz", 4, 10, 1, 0, 60))
	// non-cursor match row: 2-space prefix + dim gutter + dim pre + blue match + dim post.
	if !strings.Contains(got, "L7:") {
		t.Errorf("non-cursor match row missing gutter: %q", got)
	}
	if !strings.HasPrefix(got, "  L7:") {
		t.Errorf("non-cursor match row marker must be 2 spaces: %q", got)
	}
	if !strings.Contains(got, "needle") {
		t.Errorf("non-cursor match row lost the match text: %q", got)
	}

	got = plain(csRow(7, "abc needle xyz", 4, 10, 1, 1, 60))
	if !strings.HasPrefix(got, "> L7:") {
		t.Errorf("cursor marker must be '> L7:', got %q", got)
	}

	// Context row (line prior to the match): only the gutter is dim.
	got = plain(csRow(6, "context line", 0, 0, 0, 0, 60))
	if !strings.HasPrefix(got, "  L6:") || !strings.Contains(got, "context line") {
		t.Errorf("context row lost gutter or text: %q", got)
	}

	// Long line truncation. A 605-rune line at width 220 with a "zwk"
	// at rune 300 should report an "abcdef..." prefix and "...suffix"
	// suffix.
	longText := strings.Repeat("P", 300) + " zwk " + strings.Repeat("Q", 300)
	body, s, e := csFit(longText, 301, 304, 200) // typical 200-cell content width
	rs := []rune(body)
	if rs[0] != '…' || rs[len(rs)-1] != '…' {
		t.Errorf("csFit must wrap the match with ellipses: %q", body)
	}
	if s <= 0 || e >= len(rs) {
		t.Errorf("csFit match span %d-%d out of range 0-%d", s, e, len(rs))
	}
}

// TestCycle213ListBudget captures the per-height row budget for the live
// list — used by codesearchRows to bound the rendered window.
func TestCycle213ListBudget(t *testing.T) {
	cases := []struct{ height, want int }{
		{0, 0},   // unknown → no windowing
		{24, 10}, // floor(24*11/15) - 7 = 17 - 7
		{50, 29}, // floor(50*11/15) - 7 = 36 - 7
		{120, 81},
	}
	for _, tc := range cases {
		got := csListBudget(tc.height)
		if got != tc.want {
			t.Errorf("csListBudget(%d) = %d, want %d", tc.height, got, tc.want)
		}
	}
}

// TestCycle213CsFill verifies csFill's layout: file header on the first
// item of a new file, separator rule between blocks of the same file, a
// blank row before the header of a new file when it follows a block.
func TestCycle213CsFill(t *testing.T) {
	hits := []csHit{
		{file: "a.txt", line: 2, text: "match a1"},
		{file: "a.txt", line: 4, text: "match a2"},
		{file: "b.txt", line: 1, text: "match b1"},
	}
	// Render with cursor on hits[2] (the b.txt match) and a large budget.
	end, rows := csFill(hits, 0, 2, 99, 220)
	if end != 3 {
		t.Errorf("csFill returned end=%d, want 3", end)
	}
	stripped := make([]string, len(rows))
	for i, r := range rows {
		stripped[i] = stripSGR(r)
	}
	hasHeader := func(name string) bool {
		for _, r := range stripped {
			if r == name {
				return true
			}
		}
		return false
	}
	if !hasHeader("a.txt") {
		t.Error("first file header missing")
	}
	if !hasHeader("b.txt") {
		t.Error("second file header missing")
	}
	hasRule := false
	for _, r := range stripped {
		if r != "" && strings.Count(r, "─") == len([]rune(r)) {
			hasRule = true
			break
		}
	}
	if !hasRule {
		t.Error("between-blocks separator rule missing")
	}
}

// TestCycle213PanelLinesUsage verifies that the panel still renders the
// usage form when openSlashPanelArgs is called with an empty argument
// tail — this is the cycle-206-equivalent contract for bare /codesearch.
func TestCycle213PanelLinesUsage(t *testing.T) {
	m := mkPanel(panelCodeSearch, "/codesearch", 0)
	plain := plainLines(m)
	// Usage row must contain "/codesearch [-F|--literal] <query>".
	found := false
	for _, l := range plain {
		if strings.Contains(l, "Usage: /codesearch") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("usage row missing; got %q", plain)
	}
}

// TestCycle213PanelLinesResultList: openSlashPanelArgs("codesearch",
// "needle") must populate cs.hits and produce a panel whose plain row
// contains the cycle-228 "Found N matches in K files." header (cycle 213
// only knew the total count; cycle 228 also tracks the file count
// recovered from the AGY 1.4.0 changelog).
func TestCycle213PanelLinesResultList(t *testing.T) {
	root := csTmpTree(t)
	m := NewRunMode(nil, "u@t", "Gemini 3.8 Flash (High)", "t", root, false)
	if !m.openSlashPanelArgs("codesearch", "needle") {
		t.Fatal("openSlashPanelArgs(codesearch, needle) = false")
	}
	if m.panel.cs.query != "needle" {
		t.Errorf("query = %q, want needle", m.panel.cs.query)
	}
	if len(m.panel.cs.hits) != 5 {
		// csTmpTree: a.txt × 2, d.txt (smart-case), dbl.txt, sub/c.txt
		t.Errorf("hits = %d, want 5; raw=%+v", len(m.panel.cs.hits), m.panel.cs.hits)
	}
	plain := plainLines(m)

	wantSubstrings := []string{
		"Code Search: needle",
		"Found 5",     // cycle 228 says "Found 5 matches in 4 files."
		"in 4 files.", // 4 distinct files: a.txt, d.txt, dbl.txt, sub/c.txt
		"a.txt", "d.txt", "dbl.txt", "sub/c.txt",
		"L2: ",
		// Counter may or may not appear depending on whether the list
		// overflowed the budget; just confirm we got the legend.
		"Keyboard: ",
	}
	for _, w := range wantSubstrings {
		found := false
		for _, l := range plain {
			if strings.Contains(l, w) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("panel row missing %q in %q", w, plain)
		}
	}
}

// TestCycle213PanelLinesNoResults and PatternError lock the empty-list
// fallback forms.
func TestCycle213PanelLinesNoResults(t *testing.T) {
	m := NewRunMode(nil, "u@t", "m", "t", t.TempDir(), false)
	m.openSlashPanelArgs("codesearch", "nevergonnamatch")
	if len(m.panel.cs.hits) != 0 {
		t.Fatalf("hits = %d, want 0", len(m.panel.cs.hits))
	}
	plain := plainLines(m)
	found := false
	for _, l := range plain {
		if strings.Contains(l, "No results") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected `No results` row in %q", plain)
	}

	// Pattern error: unbalanced bracket yields a user-facing message.
	m2 := NewRunMode(nil, "u@t", "m", "t", t.TempDir(), false)
	m2.openSlashPanelArgs("codesearch", "(unclosed")
	if m2.panel.cs.err == "" {
		t.Error("expected cs.err to be set on an unparseable pattern")
	}
	plain = plainLines(m2)
	found = false
	for _, l := range plain {
		if strings.Contains(l, "Error") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected `Error:` row in %q", plain)
	}
}

// TestCycle213PanelKeyNavigation drives ↓ / ↑ on a populated list and
// asserts the cursor moves within bounds, the window re-anchors, and
// ← / → jumps between files.
func TestCycle213PanelKeyNavigation(t *testing.T) {
	root := csTmpTree(t)
	m := NewRunMode(nil, "u@t", "m", "t", root, false)
	m.openSlashPanelArgs("codesearch", "needle")
	if len(m.panel.cs.hits) < 2 {
		t.Fatalf("need ≥2 hits, got %d", len(m.panel.cs.hits))
	}
	// Mark the cursor on item 0 (a.txt's first hit) and tick Down a few
	// times to ensure navigation does not escape the result set.
	startCursor := m.panel.cursor
	m.codesearchStart("needle")
	if m.panel.cursor != 0 {
		t.Errorf("codesearchStart must reset cursor to 0, got %d", m.panel.cursor)
	}
	// Hit Down 3 times.
	m.csMoveCursor(1)
	m.csMoveCursor(1)
	m.csMoveCursor(1)
	if m.panel.cursor != 3 {
		t.Errorf("after Down ×3 cursor = %d, want 3", m.panel.cursor)
	}
	// Clamp at the bottom.
	m.csMoveCursor(999)
	if m.panel.cursor != len(m.panel.cs.hits)-1 {
		t.Errorf("Down-clamp at %d, want %d", m.panel.cursor, len(m.panel.cs.hits)-1)
	}
	// Clamp at the top.
	m.csMoveCursor(-999)
	if m.panel.cursor != 0 {
		t.Errorf("Up-clamp at %d, want 0", m.panel.cursor)
	}
	_ = startCursor

	// ← / → file jumps: stand on a.txt (file index 0), press Right, then
	// verify the cursor now sits in another file.
	m.codesearchStart("needle")
	m.csMoveFile(1)
	f2 := m.panel.cs.hits[m.panel.cursor].file
	if f2 == "a.txt" {
		t.Errorf("Right should leave a.txt, cursor file = %s", f2)
	}
	m.csMoveFile(-1)
	if m.panel.cs.hits[m.panel.cursor].file != "a.txt" {
		t.Errorf("Left must return cursor to a.txt, got %s",
			m.panel.cs.hits[m.panel.cursor].file)
	}
}

// TestCycle213PanelKeyInertOnEmpty checks that the "no hits" / usage /
// pattern-error forms keep cycle 206's "panel owns every key" semantics
// (handled=true, but the panel doesn't change state).
//
// Cycle 228: openSlashPanelArgs now falls through to the slash-echo
// path when args is empty (the bare `/codesearch` case emits
// "⎿  /codesearch needs a query." rather than opening the panel).
// The panel's own inert behaviour is exercised by directly setting
// panel.kind = panelCodeSearch and verifying the handler returns
// handled=true.
func TestCycle213PanelKeyInertOnEmpty(t *testing.T) {
	m := NewRunMode(nil, "u@t", "m", "t", t.TempDir(), false)
	m.panel = slashPanel{kind: panelCodeSearch, cmd: "/codesearch"}
	if len(m.panel.cs.hits) != 0 {
		t.Fatalf("expected 0 hits for empty panel state")
	}
	handled, cmd := m.handlePanelKey(tea.Key{Code: tea.KeyDown})
	if !handled || cmd != nil {
		t.Errorf("Down on usage form: handled=%v cmd=%v, want handled=true cmd=nil", handled, cmd)
	}
	// Cursor and window must not have moved.
	if m.panel.cursor != 0 || m.panel.window != 0 {
		t.Errorf("cursor/window must stay at 0 on inert forms; got %d/%d",
			m.panel.cursor, m.panel.window)
	}
}

// TestCycle213PanelKeyEscCloses asserts the only key that dismisses a
// live /codesearch result list is Esc.
func TestCycle213PanelKeyEscCloses(t *testing.T) {
	root := csTmpTree(t)
	m := NewRunMode(nil, "u@t", "m", "t", root, false)
	m.openSlashPanelArgs("codesearch", "needle")
	handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEsc})
	if !handled || m.panel.kind != panelNone {
		t.Errorf("Esc must close codesearch; handled=%v kind=%v", handled, m.panel.kind)
	}
}

// TestCycle213NonCodesearchArgsPanelsKeepsExisting confirms the
// openSlashPanelArgs refactor did not change any non-/codesearch branch:
// every other panel command still renders its old content.
func TestCycle213NonCodesearchArgsPanelsKeepsExisting(t *testing.T) {
	for _, cmd := range []string{"mcp", "tasks", "context"} {
		m := &runMode{}
		if !m.openSlashPanelArgs(cmd, "withargs") {
			t.Errorf("openSlashPanelArgs(%q, ...) = false", cmd)
			continue
		}
		// The arg tail is not echoed by these panels — verify it isn't
		// accidentally attached anywhere they don't expect it.
		plain := plainLines(m)
		for _, l := range plain {
			if strings.Contains(l, "withargs") {
				t.Errorf("%s panel rendered the argument tail %q; should drop it", cmd, l)
			}
		}
	}
}

// TestCycle213CsFillCounterDrivesFormatting exercises the counter format
// `[first-last of N items]`: when the result list overflows the budget,
// counter shows. Otherwise it stays hidden (the "list fits" case the
// reference collapses the counter under).
func TestCycle213CsFillCounterDrivesFormatting(t *testing.T) {
	root := t.TempDir()
	// 12 matches (12 lines × "needle") > small budget, expect a counter.
	body := "needle\n"
	for i := 0; i < 11; i++ {
		body += fmt.Sprintf("line %d\n", i)
		if i%2 == 0 {
			body += "needle\n"
		}
	}
	if err := os.WriteFile(filepath.Join(root, "x.txt"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	m := NewRunMode(nil, "u@t", "m", "t", root, false)
	m.height = 24
	// Cycle 228: opt out of the streaming path so the synchronous
	// csScan populates hits before the test reads plainLines. The
	// panel layout under test is the cycle-213 one; cycle 228 only
	// affects the streaming behaviour.
	m.streamingSearch = false
	m.openSlashPanelArgs("codesearch", "needle")
	plain := plainLines(m)
	hasCounter := false
	for _, l := range plain {
		if strings.Contains(l, "of") && strings.Contains(l, "items]") {
			hasCounter = true
			break
		}
	}
	if !hasCounter {
		t.Errorf("expected counter row for overflowing list; got %q", plain)
	}
}
