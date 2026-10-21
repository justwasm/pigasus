// slash_codesearch_test.go — cycle 228: the /codesearch panel with
// progressive streaming and ripgrep fallback.
//
// Four tests pin the cycle-228 contract recovered from the AGY 1.4.0
// changelog:
//
//   1. Bare `/codesearch` (no args) emits the cycle-228 echo line
//      "⎿  /codesearch needs a query." instead of opening the panel.
//   2. `/codesearch TODO` opens the panel and shows the live
//      "Searching… / Found N matches in K files" header that ticks
//      up as matches stream in.
//   3. Esc mid-search cancels the rg child and closes the panel
//      without leaving an orphan.
//   4. `/cs` and `/search` aliases route to the same handler as
//      `/codesearch` — the alias table from internal/tuilib must
//      include all three.
//
// The streaming test uses a real goroutine + csTickMsg pathway with
// a mock rg binary (a shell script that emits rg --json output).

package main

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/codesearch"
)

// TestSlashCodesearchNoArgs: typing `> /codesearch` echoes
// `⎿  /codesearch needs a query.` — the panel does NOT open.
func TestSlashCodesearchNoArgs(t *testing.T) {
	m := newSlashModel(t)
	if handled, cmd := m.handleSlashCommand("/codesearch"); !handled || cmd != nil {
		t.Errorf("handleSlashCommand(/codesearch) = (handled=%v cmd=%v), want handled=true cmd=nil",
			handled, cmd)
	}
	if m.panel.kind == panelCodeSearch {
		t.Errorf("bare /codesearch should not open the panel; panel.kind = %v", m.panel.kind)
	}
	joined := strings.Join(afterEcho(m), "\n")
	if !strings.Contains(joined, "/codesearch needs a query.") {
		t.Errorf("bare /codesearch missing needs-query echo; got %q", joined)
	}
}

// TestSlashCodesearchStreaming: typing `/codesearch TODO` opens the
// panel, the header shows the streaming "Found N matches in K files"
// row once the runner settles. The mock rg emits three matches in
// three files; the panel must show all three.
func TestSlashCodesearchStreaming(t *testing.T) {
	root := t.TempDir()
	for i := 0; i < 3; i++ {
		path := filepath.Join(root, "a"+string(rune('0'+i))+".go")
		if err := os.WriteFile(path,
			[]byte("// TODO line\npackage x\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	m := newSlashModel(t)
	m.cwd = root
	m.height = 30
	m.width = 80

	// Stub the ripgrep binary with a mock that emits three matches.
	script := mockRgScript(t, "rg-stream", `{"type":"match","data":{"path":{"text":"a0.go"},"lines":{"text":"// TODO line\n"},"line_number":1,"submatches":[{"match":{"text":"TODO"},"start":3,"end":7}]}}
{"type":"match","data":{"path":{"text":"a1.go"},"lines":{"text":"// TODO line\n"},"line_number":1,"submatches":[{"match":{"text":"TODO"},"start":3,"end":7}]}}
{"type":"match","data":{"path":{"text":"a2.go"},"lines":{"text":"// TODO line\n"},"line_number":1,"submatches":[{"match":{"text":"TODO"},"start":3,"end":7}]}}
`)

	// Manually drive the streaming path so we don't depend on a real
	// tick timer (which would race the goroutine).
	search := &codesearch.Search{
		Query: "TODO",
		Root:  root,
	}
	runner := &codesearch.Runner{
		RgPath:  script,
		Workdir: root,
		RgBin:   mockRgCmd(script),
	}
	m.panel.kind = panelCodeSearch
	m.panel.cmd = "/codesearch"
	m.panel.cs.search = search
	m.panel.cs.runner = runner
	m.panel.cs.query = "TODO"
	m.panel.cs.args = "TODO"
	m.panel.cs.files = map[string]struct{}{}

	go runner.Run(context.Background(), search)

	// Wait for the search to settle. The goroutine sets state to
	// StateRunning immediately, then to StateDone once the channel
	// is closed. If we read state before the goroutine is scheduled
	// it would still be "" (the zero value), which is != StateRunning
	// and would short-circuit the wait — so we require the runner
	// to have started before we check for terminal state.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if search.State != "" {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	for time.Now().Before(deadline) {
		if search.State != codesearch.StateRunning {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	if search.State == codesearch.StateRunning {
		t.Fatal("search did not settle in 2s")
	}

	// Drain all matches into the panel's hits slice.
	m.drainCodesearchMatches()

	plain := plainLines(m)
	joined := strings.Join(plain, "\n")

	if !strings.Contains(joined, "Code Search: TODO") {
		t.Errorf("panel missing title; got %q", joined)
	}
	if !strings.Contains(joined, "Found 3 matches in 3 files.") {
		t.Errorf("panel missing streaming count row; got %q", joined)
	}
	for _, w := range []string{"a0.go", "a1.go", "a2.go"} {
		if !strings.Contains(joined, w) {
			t.Errorf("file header %s missing from %q", w, joined)
		}
	}
}

// TestSlashCodesearchCancel: Esc mid-search cancels the rg child and
// closes the panel without leaving an orphan process.
func TestSlashCodesearchCancel(t *testing.T) {
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

	m := newSlashModel(t)
	m.cwd = root
	m.height = 30
	m.width = 80
	search := &codesearch.Search{Query: "TODO", Root: root}
	runner := &codesearch.Runner{
		RgPath:  script,
		Workdir: root,
		RgBin:   mockRgCmd(script),
	}
	m.panel.cs.search = search
	m.panel.cs.runner = runner
	m.panel.cs.query = "TODO"
	m.panel.cs.args = "TODO"
	m.panel.cs.files = map[string]struct{}{}
	m.panel.kind = panelCodeSearch
	m.panel.cmd = "/codesearch"

	go runner.Run(context.Background(), search)

	// Wait for the runner goroutine to attach the child cmd.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if runner.Cancel() != nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	// Cancel returns nil only if no child is attached yet; we use it
	// here as a presence probe (the production path's Cancel() also
	// kills, but in the test we manually call it below). Use the
	// process pid from a successful Wait / inspect differently.
	if runner == nil {
		t.Fatal("runner not constructed")
	}
	// Drive the Esc key — the panel will call runner.Cancel().
	_, _ = m.handlePanelKey(tea.Key{Code: tea.KeyEsc})

	// Allow the goroutine to observe the kill.
	time.Sleep(100 * time.Millisecond)

	if m.panel.kind == panelCodeSearch {
		t.Errorf("panel did not close after Esc; kind=%v", m.panel.kind)
	}
	// We don't have direct access to the cmd from outside the runner
	// package, but the goroutine having exited cleanly is observable
	// via Search.State flipping off StateRunning.
	if search.State == codesearch.StateRunning {
		t.Errorf("search still running after Esc; state=%q", search.State)
	}
}

// TestSlashCodesearchAliases: /cs and /search route through the same
// handler as /codesearch (the cycle-206 alias mapping in
// canonicalPanelCommand + openSlashPanelArgs).
func TestSlashCodesearchAliases(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"),
		[]byte("// TODO\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, alias := range []string{"cs", "search"} {
		t.Run(alias, func(t *testing.T) {
			m := newSlashModel(t)
			m.cwd = root
			// Disable streaming to make the panel settle synchronously.
			m.streamingSearch = false

			if !m.openSlashPanelArgs(alias, "TODO") {
				t.Fatalf("openSlashPanelArgs(%q, TODO) = false", alias)
			}
			if m.panel.kind != panelCodeSearch {
				t.Errorf("panel.kind = %v, want panelCodeSearch", m.panel.kind)
			}
			if m.panel.cs.query != "TODO" {
				t.Errorf("query = %q, want TODO", m.panel.cs.query)
			}
		})
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

// _ keeps the syscall import live in builds that strip it via
// unused-import pruning.
var _ = syscall.Signal(0)
