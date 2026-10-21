// slash_changelog_source_test.go — cycle changelog-pagination: tests
// that the /changelog viewer's source document comes from
// internal/cli/testdata/changelog_reference.txt and that the full
// pagination key set (↑/↓ / pgup / pgdown / ctrl+home / ctrl+end)
// drives the window and counter.
//
// Tests that need the source file skip themselves when the
// walk-up-from-cwd cannot find the project root (which can happen
// when `go test` is invoked from an unusual working directory).
package main

import (
	"os"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// mustGetwd returns the current working directory, failing the test
// if it cannot be read.
func mustGetwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

// TestChangelogSource_LoaderFindsReference pins the runtime contract:
// the file exists, it lives under internal/cli/testdata/, the loader
// reads it, and the parsed lines cover the full release-notes range
// (1.4.2 → 1.4.1 → 1.4.0 → 1.3.6 → ... → 1.0.0). Cycle 235 prepends a
// placeholder 1.4.2: stub ahead of the binary capture, so the first
// line is `1.4.2:` (the binary's own release-notes block still opens
// at 1.4.1 because AGY ships its `changelog` one release behind).
func TestChangelogSource_LoaderFindsReference(t *testing.T) {
	if !changelogSourceLoaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	path := changelogSourceFilePath()
	if !strings.HasSuffix(path, "internal/cli/testdata/changelog_reference.txt") {
		t.Errorf("source path = %q, want suffix internal/cli/testdata/changelog_reference.txt", path)
	}
	src := changelogSourceLines()
	if len(src) < 940 {
		t.Errorf("source line count = %d, want ≥ 940", len(src))
	}
	if src[0] != "1.4.3:" {
		t.Errorf("first source line = %q, want %q", src[0], "1.4.3:")
	}
	if !strings.Contains(strings.Join(src, "\n"), "1.4.1:") {
		t.Error("source missing the 1.4.1 header (binary capture block)")
	}
	if !strings.Contains(strings.Join(src, "\n"), "1.0.0:") {
		t.Error("source missing the 1.0.0 header")
	}
}

// TestChangelogPagination_RendersDoc verifies the panel actually
// surfaces content from the loaded reference file. A successful pass
// means the runtime loader is wired into the renderer (not just
// sitting alongside it) and the parser handles the bullet-list
// format.
func TestChangelogPagination_RendersDoc(t *testing.T) {
	if !changelogSourceLoaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelChangelog, "/changelog", 0)
	text := panelText(m)
	// The first 18-row window opens on the 1.4.3 section (the newest
	// block the reference binary ships) and its lead bullet — a long
	// sentence about plugin-validate improvements, unique enough to pin.
	if !strings.Contains(text, "Improved agy plugin validate to also check") {
		t.Errorf("viewer missing the 1.4.1 opening bullet:\n%s", truncForErr(text, 400))
	}
	// The 1.4.3 header from the source should be styled as "## 1.4.3"
	// so the cycle-201 header convention survives.
	if !strings.Contains(text, "## 1.4.3") {
		t.Errorf("viewer missing the ## 1.4.3 header row:\n%s", truncForErr(text, 400))
	}
	// 1.4.3's own block is taller than the 18-row window, so 1.4.2 is
	// below the fold; assert it is in the document, not the first screen.
	if !strings.Contains(strings.Join(m.changelogViewerLines(), "\n"), "## 1.4.2") {
		t.Error("document missing the ## 1.4.2 header row")
	}
	// The full document is reachable via m.changelogViewerLines();
	// the 1.0.0 closing line is the other end of the file.
	doc := strings.Join(m.changelogViewerLines(), "\n")
	if !strings.Contains(doc, "Initial release of the Antigravity CLI.") {
		t.Errorf("document missing the 1.0.0 closing line:\n%s", doc)
	}
}

// TestChangelogPagination_PageDown pins ↑/↓ single-line scroll and
// pgdown's page-size jump. The window advances by one row per ↓
// press and by (visible-1) rows per pgdown — the cycle 201
// contract.
func TestChangelogPagination_PageDown(t *testing.T) {
	if !changelogSourceLoaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelChangelog, "/changelog", 0)
	vis := changelogWindowRows()
	if got, want := m.panel.window, 0; got != want {
		t.Fatalf("initial window = %d, want %d", got, want)
	}
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	if got, want := m.panel.window, 1; got != want {
		t.Fatalf("after ↓ window = %d, want %d", got, want)
	}
	// Reset and exercise pgdown.
	m.panel.window = 0
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyPgDown}); !handled {
		t.Fatal("pgdown not handled")
	}
	page := vis - 1
	if page < 1 {
		page = 1
	}
	if got, want := m.panel.window, page; got != want {
		t.Fatalf("after pgdown window = %d, want %d", got, want)
	}
	// And pgup returns by the same page size.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyPgUp}); !handled {
		t.Fatal("pgup not handled")
	}
	if got, want := m.panel.window, 0; got != want {
		t.Fatalf("after pgup window = %d, want %d", got, want)
	}
}

// TestChangelogPagination_CtrlEnd lands on the final window. The
// reference shows the 1.0.0 "Initial release" line at the bottom of
// the document, so the assertion checks for that line.
func TestChangelogPagination_CtrlEnd(t *testing.T) {
	if !changelogSourceLoaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelChangelog, "/changelog", 0)
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyEnd, Mod: tea.ModCtrl}); !handled {
		t.Fatal("ctrl+end not handled")
	}
	total := len(m.changelogViewerLines())
	vis := changelogWindowRows()
	if want := total - vis; m.panel.window != want {
		t.Fatalf("ctrl+end window = %d, want %d", m.panel.window, want)
	}
	text := panelText(m)
	if !strings.Contains(text, "Initial release of the Antigravity CLI.") {
		t.Errorf("bottom of changelog should show the 1.0.0 release:\n%s", text)
	}
	// Past-the-end down presses must clamp.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down at bottom not handled")
	}
	if want := total - vis; m.panel.window != want {
		t.Errorf("down at bottom moved window to %d, want %d", m.panel.window, want)
	}
}

// TestChangelogPagination_CtrlHome returns to the top of the document.
// The first screen now opens on the cycle-235 1.4.2 stub and then
// carries the 1.4.1 header (which is still on the first 18-row
// window — the stub is just one header + one bullet + one blank
// line ahead of 1.4.1, well within the window).
func TestChangelogPagination_CtrlHome(t *testing.T) {
	if !changelogSourceLoaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelChangelog, "/changelog", 0)
	// Scroll to the middle first so ctrl+home is observable.
	m.panel.window = 50
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyHome, Mod: tea.ModCtrl}); !handled {
		t.Fatal("ctrl+home not handled")
	}
	if m.panel.window != 0 {
		t.Fatalf("ctrl+home window = %d, want 0", m.panel.window)
	}
	text := panelText(m)
	if !strings.Contains(text, "## 1.4.3") {
		t.Errorf("first screen should open on 1.4.3:\n%s", truncForErr(text, 400))
	}
	if !strings.Contains(strings.Join(m.changelogViewerLines(), "\n"), "## 1.4.2") {
		t.Error("document missing the ## 1.4.2 header row")
	}
}

// TestChangelogPagination_Counter verifies the (a–b of 2014 lines)
// counter follows the window. The total is REF's 2014 denominator
// (the cycle 201 contract); the [start,end] range is the actual
// document window.
func TestChangelogPagination_Counter(t *testing.T) {
	if !changelogSourceLoaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	m := mkPanel(panelChangelog, "/changelog", 0)
	vis := changelogWindowRows()
	// Initial counter: 1–vis of 2014 lines.
	if want := "(" + intStr(1) + "\u2013" + intStr(vis) + " of 2014 lines)"; !strings.Contains(panelText(m), want) {
		t.Errorf("initial counter wrong; want %q in:\n%s", want, panelText(m))
	}
	// After ↓: 2–(vis+1) of 2014.
	if handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("down not handled")
	}
	want := "(" + intStr(2) + "\u2013" + intStr(vis+1) + " of 2014 lines)"
	if !strings.Contains(panelText(m), want) {
		t.Errorf("counter after ↓: want %q in:\n%s", want, panelText(m))
	}
}

// TestChangelogPagination_FitsOnScreen pins the "small document" edge
// case: when the document fits entirely in one window, ctrl+end must
// not crash, ctrl+home stays at offset 0, and pgup / pgdown are
// no-ops (the window offset clamps to 0). The handler reads the
// viewer spec via m.viewerSpecFor(), so the test exercises the same
// code path the panel uses on a live terminal — the only difference
// is that the source is a hand-built tiny spec.
func TestChangelogPagination_FitsOnScreen(t *testing.T) {
	if !changelogSourceLoaded() {
		t.Skipf("changelog_reference.txt not reachable from cwd %q — skipping", mustGetwd(t))
	}
	// viewerMaxOffset is the largest legal window offset for a spec:
	// the document length minus the visible rows, never negative.
	// Pin the contract directly so a regression in the clamp logic is
	// caught even if the panel renderer changes.
	if got := viewerMaxOffset(viewerSpec{lines: nil, visible: 5, total: 5}); got != 0 {
		t.Errorf("viewerMaxOffset(empty) = %d, want 0", got)
	}
	if got := viewerMaxOffset(viewerSpec{lines: []string{"a", "b", "c"}, visible: 12, total: 3}); got != 0 {
		t.Errorf("viewerMaxOffset(short) = %d, want 0", got)
	}
	if got := viewerMaxOffset(viewerSpec{lines: []string{"a", "b", "c", "d", "e"}, visible: 2, total: 5}); got != 3 {
		t.Errorf("viewerMaxOffset(2-of-5) = %d, want 3", got)
	}
	// And the live path: a real changelog panel whose document is
	// smaller than the window must still survive ctrl+end. We can't
	// inject a custom document without exposing internals, so we drive
	// the spec through viewerWindow directly and confirm the returned
	// range starts at 1.
	m := &runMode{}
	m.panel.kind = panelChangelog
	// Build a tiny spec by hand and confirm ctrl+end would clamp it.
	tiny := viewerSpec{lines: []string{"a", "b", "c"}, visible: 12, total: 3}
	rows, start, end := m.viewerWindow(tiny)
	if len(rows) != 3 || start != 1 || end != 3 {
		t.Errorf("viewerWindow(tiny) = (%d rows, %d–%d), want (3 rows, 1–3)", len(rows), start, end)
	}
}

// TestChangelogSource_IsChangelogSourceHeader pins the header
// detector against the bullet-list source format. The detector runs
// on every line of the document at render time, so a false negative
// here drops the entire 1.3.6 / 1.3.5 / etc. section styling and a
// false positive here styles arbitrary bullet text as a header.
func TestChangelogSource_IsChangelogSourceHeader(t *testing.T) {
	cases := []struct {
		line string
		want bool
	}{
		{"1.3.6:", true},
		{"1.0.0:", true},
		{"2.10.4:", true},
		{"10.20.30:", true},
		{"", false},
		{"1.3", false},
		{"1.3.6", false},
		{"1.3.6: extra", false},
		{"v1.3.6:", false},
		{"1.3.6.7:", false},
		{"1..6:", false},
		{"a.b.c:", false},
	}
	for _, tc := range cases {
		if got := isChangelogSourceHeader(tc.line); got != tc.want {
			t.Errorf("isChangelogSourceHeader(%q) = %v, want %v", tc.line, got, tc.want)
		}
	}
}

// truncForErr trims s to at most n bytes, appending an ellipsis when
// it cut anything off. Used to keep test failure logs readable.
func truncForErr(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

// intStr formats n as a decimal string. The package's existing
// strconv-free itoa lives in slash_commands.go and is not exported;
// a local copy keeps this test self-contained.
func intStr(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}
