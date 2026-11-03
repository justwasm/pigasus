// panel_test.go — the /codesearch panel's own tests.
//
// The panel is the subject here: it is built directly, with the seams
// a test can answer, and the assertions read its own rows. The host's
// wiring (the panel's rebuild on open, the tick the host arms, the
// argument tail the host hands over) is tested in internal/cliapp.

package codesearchpanel

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/tools/codesearch"
)

// sgrRe matches the SGR spans the panel's rows carry.
var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plain strips the SGR spans from a rendered row.
func plain(s string) string {
	return sgrRe.ReplaceAllString(s, "")
}

// plainLines strips the SGR spans from every row.
func plainLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = plain(l)
	}
	return out
}

// panelTree builds a self-contained workspace under t.TempDir() with the
// files the assertions below need. Returns the root path.
func panelTree(t *testing.T) string {
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
	must("sub/c.txt", "alpha zwk beta\nneedle\n")
	return root
}

// testPanel builds a panel over the given root, on the synchronous path.
func testPanel(t *testing.T, root string) *Panel {
	t.Helper()
	return New(Seams{
		Root:      func() string { return root },
		Size:      func() (int, int) { return 80, 24 },
		Streaming: func() bool { return false },
	})
}

// TestPanelUsageForm: a panel with no query shows the reference's usage
// line.
func TestPanelUsageForm(t *testing.T) {
	p := testPanel(t, t.TempDir())
	lines := plainLines(p.Lines())
	if !strings.Contains(lines[0], "Code Search") {
		t.Errorf("first row must be the title; got %q", lines[0])
	}
	found := false
	for _, l := range lines {
		if strings.Contains(l, "Usage: /codesearch") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("usage row missing; got %q", lines)
	}
}

// TestPanelNoResults: a search that matched nothing shows the
// reference's "No results" row.
func TestPanelNoResults(t *testing.T) {
	p := testPanel(t, t.TempDir())
	p.Start("nevergonnamatch")
	if len(p.Hits()) != 0 {
		t.Fatalf("hits = %d, want 0", len(p.Hits()))
	}
	found := false
	for _, l := range plainLines(p.Lines()) {
		if strings.Contains(l, "No results") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected `No results` row in %q", plainLines(p.Lines()))
	}
}

// TestPanelPatternError: an unbalanced bracket yields a user-facing
// message instead of a result list.
func TestPanelPatternError(t *testing.T) {
	p := testPanel(t, t.TempDir())
	p.Start("(unclosed")
	if p.Err() == "" {
		t.Error("expected the panel to carry a pattern error")
	}
	found := false
	for _, l := range plainLines(p.Lines()) {
		if strings.Contains(l, "Error") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected `Error:` row in %q", plainLines(p.Lines()))
	}
}

// TestPanelResultList: the title echoes the argument tail and the
// status row counts the matches and the files.
func TestPanelResultList(t *testing.T) {
	root := panelTree(t)
	p := New(Seams{
		Root:      func() string { return root },
		Size:      func() (int, int) { return 80, 120 },
		Streaming: func() bool { return false },
	})
	p.Start("needle")
	if p.Query() != "needle" {
		t.Errorf("query = %q, want needle", p.Query())
	}
	if len(p.Hits()) != 5 {
		t.Fatalf("hits = %d, want 5; raw=%+v", len(p.Hits()), p.Hits())
	}
	joined := strings.Join(plainLines(p.Lines()), "\n")
	if !strings.Contains(joined, "Code Search: needle") {
		t.Errorf("panel missing title; got %q", joined)
	}
	if !strings.Contains(joined, "Found 5 matches in 4 files.") {
		t.Errorf("panel missing the count row; got %q", joined)
	}
	for _, want := range []string{"a.txt", "d.txt", "dbl.txt", "sub/c.txt"} {
		if !strings.Contains(joined, want) {
			t.Errorf("file header %s missing from %q", want, joined)
		}
	}
}

// TestPanelKeyNavigation drives ↓ / ↑ on a populated list and asserts
// the cursor moves within bounds, the window re-anchors, and ← / →
// jumps between files.
func TestPanelKeyNavigation(t *testing.T) {
	p := testPanel(t, panelTree(t))
	p.Start("needle")
	if len(p.Hits()) < 2 {
		t.Fatalf("need ≥2 hits, got %d", len(p.Hits()))
	}
	if p.Cursor() != 0 {
		t.Errorf("Start must reset the cursor to 0, got %d", p.Cursor())
	}
	// Down ×3.
	for i := 0; i < 3; i++ {
		handled, _, ok := p.HandleKey(tea.Key{Code: tea.KeyDown})
		if !handled || !ok {
			t.Fatalf("Down must be handled by the panel")
		}
	}
	if p.Cursor() != 3 {
		t.Errorf("after Down ×3 cursor = %d, want 3", p.Cursor())
	}
	// Clamp at the bottom.
	for i := 0; i < 999; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyDown})
	}
	if p.Cursor() != len(p.Hits())-1 {
		t.Errorf("Down-clamp at %d, want %d", p.Cursor(), len(p.Hits())-1)
	}
	// Clamp at the top.
	for i := 0; i < 999; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyUp})
	}
	if p.Cursor() != 0 {
		t.Errorf("Up-clamp at %d, want 0", p.Cursor())
	}

	// ← / → file jumps: stand on a.txt (file index 0), press Right, then
	// verify the cursor now sits in another file.
	p.HandleKey(tea.Key{Code: tea.KeyRight})
	if f := p.Hits()[p.Cursor()].File; f == "a.txt" {
		t.Errorf("Right should leave a.txt, cursor file = %s", f)
	}
	p.HandleKey(tea.Key{Code: tea.KeyLeft})
	if f := p.Hits()[p.Cursor()].File; f != "a.txt" {
		t.Errorf("Left must return the cursor to a.txt, got %s", f)
	}
}

// TestPanelKeyInertOnEmpty: the usage form owns no key — the host's
// generic list handling takes it, so Ctrl+C still leaves the panel.
func TestPanelKeyInertOnEmpty(t *testing.T) {
	p := testPanel(t, t.TempDir())
	handled, cmd, ok := p.HandleKey(tea.Key{Code: tea.KeyDown})
	if handled || cmd != nil || ok {
		t.Errorf("Down on the usage form: handled=%v cmd=%v ok=%v, want all false", handled, cmd, ok)
	}
	if p.Cursor() != 0 {
		t.Errorf("the cursor must stay at 0; got %d", p.Cursor())
	}
}

// TestPanelKeyEscCloses: Esc is not the panel's key once the search has
// settled — the host closes the panel the way it closes every other.
func TestPanelKeyEscCloses(t *testing.T) {
	p := testPanel(t, panelTree(t))
	p.Start("needle")
	handled, _, ok := p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if handled || ok {
		t.Errorf("Esc on a settled search must fall through: handled=%v ok=%v", handled, ok)
	}
}

// TestPanelCounterRow: when the result list overflows the row budget the
// panel shows the `[first-last of N items]` counter.
func TestPanelCounterRow(t *testing.T) {
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
	p := New(Seams{
		Root:      func() string { return root },
		Size:      func() (int, int) { return 80, 24 },
		Streaming: func() bool { return false },
	})
	p.Start("needle")
	joined := strings.Join(plainLines(p.Lines()), "\n")
	if !strings.Contains(joined, "items]") {
		t.Errorf("expected a counter row for the overflowing list; got %q", joined)
	}
}

// mockRgScript writes a one-shot shell script that emits the rg --json
// protocol verbatim. name is the basename (so multiple mocks can run
// concurrently); emit is one JSON object per line.
func mockRgScript(t *testing.T, name, emit string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("mock rg script requires a POSIX shell")
	}
	dir := t.TempDir()
	path := filepath.Join(dir, name)
	body := "#!/bin/sh\n" +
		"cat <<'EOF'\n" + emit + "\nEOF\n"
	if err := os.WriteFile(path, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

// mockRgCmd is the closure the tests pass as Runner.RgBin. It
// substitutes the script path for the rg binary so the runner's
// exec.Cmd spawns the mock, not the real rg.
func mockRgCmd(scriptPath string) func(ctx context.Context, root string, a codesearch.Args) *exec.Cmd {
	return func(ctx context.Context, root string, a codesearch.Args) *exec.Cmd {
		args := []string{"--json", "--no-heading", "--no-messages"}
		if a.Literal {
			args = append(args, "--fixed-strings")
		}
		for _, g := range a.Include {
			args = append(args, "-g", g)
		}
		for _, g := range a.Exclude {
			args = append(args, "-g", "!"+g)
		}
		args = append(args, "-e", a.Query)
		cmd := exec.CommandContext(ctx, scriptPath, args...)
		cmd.Dir = root
		return cmd
	}
}

// TestPanelStreaming: the progressive path drains the runner's channel
// into the panel and the rows carry every match.
func TestPanelStreaming(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		path := filepath.Join(root, "a"+string(rune('0'+i))+".go")
		if err := os.WriteFile(path,
			[]byte("// TODO line\npackage x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	// Stub the ripgrep binary with a mock that emits three matches.
	script := mockRgScript(t, "rg-stream", `{"type":"match","data":{"path":{"text":"a0.go"},"lines":{"text":"// TODO line\n"},"line_number":1,"submatches":[{"match":{"text":"TODO"},"start":3,"end":7}]}}
{"type":"match","data":{"path":{"text":"a1.go"},"lines":{"text":"// TODO line\n"},"line_number":1,"submatches":[{"match":{"text":"TODO"},"start":3,"end":7}]}}
{"type":"match","data":{"path":{"text":"a2.go"},"lines":{"text":"// TODO line\n"},"line_number":1,"submatches":[{"match":{"text":"TODO"},"start":3,"end":7}]}}
`)
	runner := &codesearch.Runner{
		RgPath:  script,
		Workdir: root,
		RgBin:   mockRgCmd(script),
	}
	p := New(Seams{
		Root:      func() string { return root },
		Size:      func() (int, int) { return 80, 30 },
		Streaming: func() bool { return true },
		NewRunner: func(string) *codesearch.Runner { return runner },
	})
	p.Start("TODO")

	// Wait for the search to settle. The runner sets the state to
	// StateRunning immediately, then to StateDone once the channel is
	// closed. If we read the state before the goroutine is scheduled it
	// would still be "" (the zero value), which is != StateRunning and
	// would short-circuit the wait — so we require the runner to have
	// started before we check for a terminal state.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.Search() != nil && p.Search().CurrentState() != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for time.Now().Before(deadline) {
		if p.Search().CurrentState() != codesearch.StateRunning {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if p.Search().CurrentState() == codesearch.StateRunning {
		t.Fatal("search did not settle in 2s")
	}

	// Drain all the matches into the panel's hit set.
	p.Drain()

	joined := strings.Join(plainLines(p.Lines()), "\n")
	if !strings.Contains(joined, "Code Search: TODO") {
		t.Errorf("panel missing title; got %q", joined)
	}
	if !strings.Contains(joined, "Found 3 matches in 3 files.") {
		t.Errorf("panel missing the streaming count row; got %q", joined)
	}
	for _, w := range []string{"a0.go", "a1.go", "a2.go"} {
		if !strings.Contains(joined, w) {
			t.Errorf("file header %s missing from %q", w, joined)
		}
	}
}

// TestPanelCancel: Esc mid-search cancels the runner and leaves the
// panel, without leaving an orphan process.
func TestPanelCancel(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("cancel test requires POSIX shell")
	}
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"),
		[]byte("// TODO\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	// Build a blocking mock rg: emits one match, then waits on stdin
	// so the test can cancel before EOF.
	script := filepath.Join(t.TempDir(), "rg-cancel")
	body := "#!/bin/sh\n" +
		"cat <<'EOF'\n" +
		`{"type":"match","data":{"path":{"text":"a.go"},"lines":{"text":"// TODO\n"},"line_number":1,"submatches":[{"match":{"text":"TODO"},"start":3,"end":7}]}}
` +
		"EOF\n" +
		// Block until stdin closes; this lets us hold the child
		// alive long enough to cancel.
		"read x\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	// The child's stdin is a pipe that stays open, so the mock's
	// `read x` blocks and the search stays running until the panel
	// cancels it.
	pr, pw, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer pw.Close()
	base := mockRgCmd(script)
	runner := &codesearch.Runner{
		RgPath:  script,
		Workdir: root,
		RgBin: func(ctx context.Context, root string, a codesearch.Args) *exec.Cmd {
			cmd := base(ctx, root, a)
			cmd.Stdin = pr
			return cmd
		},
	}
	closed := false
	p := New(Seams{
		Root:      func() string { return root },
		Size:      func() (int, int) { return 80, 30 },
		Streaming: func() bool { return true },
		NewRunner: func(string) *codesearch.Runner { return runner },
		Close:     func() tea.Cmd { closed = true; return nil },
	})
	p.Start("TODO")
	// Wait for the runner goroutine to attach the child cmd.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if p.Search() != nil && p.Search().CurrentState() == codesearch.StateRunning {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	// Drain the one match, so the panel owns the Esc.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		p.Drain()
		if len(p.Hits()) > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(p.Hits()) == 0 {
		t.Fatal("the mock ripgrep's match never reached the panel")
	}

	handled, _, ok := p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled || !ok {
		t.Fatalf("Esc during a live search must be handled by the panel")
	}
	if !closed {
		t.Error("Esc during a live search must close the panel")
	}

	// Allow the goroutine to observe the kill.
	time.Sleep(100 * time.Millisecond)
	if p.Search().CurrentState() == codesearch.StateRunning {
		t.Errorf("search still running after Esc; state=%q", p.Search().CurrentState())
	}
}
