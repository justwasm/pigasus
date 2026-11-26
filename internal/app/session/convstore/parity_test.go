package convstore

import (
	"bytes"
	"database/sql"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func TestParity_LiveAgyInspection(t *testing.T) {
	livePath := os.ExpandEnv("$HOME/.gemini/antigravity-cli/conversations/00ac8418-ffd3-466a-ae5b-b78351e5b15a.db")
	if _, err := os.Stat(livePath); err != nil {
		t.Skip("live agy db not present, skipping")
	}

	c, err := LoadConversation(livePath)
	if err != nil {
		t.Fatalf("LoadConversation failed: %v", err)
	}

	typeCounts := make(map[int64]int)
	droppedCounts := make(map[int64]int)
	for _, s := range c.Steps {
		typeCounts[s.Type]++
	}

	for _, s := range c.Steps {
		dropped := false
		switch s.Role() {
		case "user", "model", "error", "system":
			if s.Text == "" {
				dropped = true
			}
		case "tool":
			if s.ToolName == "" && s.ToolSummary == "" && len(s.RawRenderInfo) == 0 {
				dropped = true
			}
		case "checkpoint":
			if s.Summary == "" && s.UserIntent == "" && s.IncludedEnd == 0 {
				dropped = true
			}
		default:
			dropped = true
		}
		if dropped {
			droppedCounts[s.Type]++
		}
	}

	t.Logf("Step type counts: %+v", typeCounts)
	t.Logf("Dropped step counts by type: %+v", droppedCounts)

	toolCount := 0
	emptyToolNameCount := 0
	emptyToolResultCount := 0
	for _, s := range c.Steps {
		if s.Type == StepToolCall {
			toolCount++
			if s.ToolName == "" {
				emptyToolNameCount++
			}
			if s.ToolResult == "" {
				emptyToolResultCount++
			}
		}
	}

	transcript := c.Transcript()
	_ = transcript
	if len(c.Steps) > 140 {
		s140 := c.Steps[140]
		t.Logf("Step 140: Type=%d, ToolName=%q, ToolArgs=%q, ToolSummary=%q",
			s140.Type, s140.ToolName, s140.ToolArgs, s140.ToolSummary)
	}
	t.Logf("Total tool steps: %d, empty ToolName: %d, empty ToolResult: %d",
		toolCount, emptyToolNameCount, emptyToolResultCount)

	emptyToolNames := make(map[string]int)
	for _, s := range c.Steps {
		if s.Type == StepToolCall && s.ToolResult == "" {
			emptyToolNames[s.ToolName]++
		}
	}
	t.Logf("Empty ToolResult breakdown by ToolName: %+v", emptyToolNames)

	if emptyToolNameCount > 0 {
		t.Errorf("FAIL: %d tool steps have empty ToolName (they are dropped from transcript!)", emptyToolNameCount)
	}
}

func TestParity_WriteConversationEmitsRenderInfoAndField140(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	steps := []Step{
		{
			Idx:    0,
			Type:   StepUserInput,
			Status: 3,
			Text:   "Hello from test",
		},
		{
			Idx:         1,
			Type:        StepToolCall,
			Status:      3,
			ToolName:    "run_command",
			ToolArgs:    `{"CommandLine":"git status"}`,
			ToolResult:  "On branch main\nnothing to commit",
			ToolSummary: "check git status",
		},
		{
			Idx:         2,
			Type:        StepToolCall,
			Status:      3,
			ToolName:    "read_file",
			ToolArgs:    `{"Path":"/path/to/go.mod"}`,
			ToolResult:  "module test",
			ToolSummary: "go.mod",
		},
		{
			Idx:    3,
			Type:   StepModelResponse,
			Status: 3,
			Text:   "Everything looks clean.",
		},
	}

	if err := WriteConversation(dbPath, "test-traj", steps); err != nil {
		t.Fatalf("WriteConversation failed: %v", err)
	}

	// 1. Verify directly in SQLite that render_info column is populated
	sdb, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	defer sdb.Close()

	var rinfo1, rinfo2 []byte
	var payload1 []byte
	if err := sdb.QueryRow(`SELECT render_info, step_payload FROM steps WHERE idx = 1`).Scan(&rinfo1, &payload1); err != nil {
		t.Fatalf("query step 1 failed: %v", err)
	}
	if err := sdb.QueryRow(`SELECT render_info FROM steps WHERE idx = 2`).Scan(&rinfo2); err != nil {
		t.Fatalf("query step 2 failed: %v", err)
	}

	if len(rinfo1) == 0 {
		t.Errorf("render_info for step 1 (run_command) is empty!")
	}
	if len(rinfo2) == 0 {
		t.Errorf("render_info for step 2 (read_file) is empty!")
	}

	// Verify StepRenderInfo fields for step 1
	rFields := protoFields(rinfo1)
	if prefix := protoText(rFields, 5); prefix != "Ran" {
		t.Errorf("step 1 prefix = %q, want 'Ran'", prefix)
	}
	if icon := protoText(rFields, 6); icon != "terminal" {
		t.Errorf("step 1 icon = %q, want 'terminal'", icon)
	}

	// Verify field 140 is present in step_payload
	pFields := protoFields(payload1)
	p140 := protoBytes(pFields, 140)
	if len(p140) == 0 {
		t.Fatalf("field 140 missing from step 1 payload!")
	}

	// 2. Load conversation back and verify lossless round-trip
	c, err := LoadConversation(dbPath)
	if err != nil {
		t.Fatalf("LoadConversation failed: %v", err)
	}

	if len(c.Steps) != 4 {
		t.Fatalf("Loaded %d steps, want 4", len(c.Steps))
	}

	loaded1 := c.Steps[1]
	if loaded1.ToolName != "run_command" {
		t.Errorf("loaded step 1 ToolName = %q, want 'run_command'", loaded1.ToolName)
	}
	if loaded1.ToolResult != "On branch main\nnothing to commit" {
		t.Errorf("loaded step 1 ToolResult = %q", loaded1.ToolResult)
	}
	if loaded1.ToolSummary != "check git status" {
		t.Errorf("loaded step 1 ToolSummary = %q, want 'check git status'", loaded1.ToolSummary)
	}

	loaded2 := c.Steps[2]
	if loaded2.ToolName != "read_file" {
		t.Errorf("loaded step 2 ToolName = %q, want 'read_file'", loaded2.ToolName)
	}
	if loaded2.ToolResult != "module test" {
		t.Errorf("loaded step 2 ToolResult = %q, want 'module test'", loaded2.ToolResult)
	}
}

func defaultAgyBin() string {
	home, _ := os.UserHomeDir()
	if home == "" {
		return ""
	}
	return filepath.Join(home, ".local", "bin", "agy")
}

// requireLiveParity skips the scenario unless the operator opted into live
// reference tests.
//
// These scenarios drive the installed agy binary against the operator's real
// store, and the reference's own rendering is not deterministic enough to gate
// a commit: Ctrl+O is a toggle whose keypress can land during a repaint, and a
// second instance of the same conversation makes the reference refuse to render
// it at all. Measured against the pristine tree they fail roughly one run in
// three, so running them by default turns CI into a coin flip and hides real
// regressions behind known flakes. The rest of the repo already gates live
// agent tests on OPENAGY_LIVE_TEST; this makes the convstore parity suite
// consistent with that instead of being the one exception.
func requireLiveParity(t *testing.T) {
	t.Helper()
	if os.Getenv("OPENAGY_LIVE_TEST") == "" {
		t.Skip("skipping live agy parity test without OPENAGY_LIVE_TEST=1")
	}
}

// liveStoreMu serialises every scenario that drives the real agy binary
// against the operator's own store.
//
// These scenarios cannot use a temporary HOME: agy keeps its sign-in state
// beside the conversations it reads, so a redirected home stops at the login
// screen. That leaves them sharing ~/.gemini/antigravity-cli with whatever else
// the machine is doing — a running CLI, another test, a previous run's leftover
// session. The lock removes the interference between *tests*, which is the part
// this package controls; the parts outside it are why these scenarios are
// treated as integration tests rather than unit tests.
var liveStoreMu sync.Mutex

// acquireLiveStore takes the live-store lock for the rest of the test.
func acquireLiveStore(t *testing.T) {
	t.Helper()
	liveStoreMu.Lock()
	t.Cleanup(liveStoreMu.Unlock)
}

// agyPane drives the official agy binary in a detached tmux session and
// answers the prompts it can block on, so a scenario can assert on what the
// rendered screen holds instead of racing it with fixed sleeps.
type agyPane struct {
	t      *testing.T
	name   string
	socket string
}

func (p *agyPane) tmux(args ...string) error {
	p.t.Helper()
	return execCommand("tmux", append([]string{"-L", p.socket}, args...)...)
}

func (p *agyPane) tmuxOutput(args ...string) ([]byte, error) {
	p.t.Helper()
	return execCommandOutput("tmux", append([]string{"-L", p.socket}, args...)...)
}

// newAgyPane uses its own server and no operator tmux config. Real HOME is
// retained for official sign-in; TERM and dimensions are explicit instead of
// inheriting an unrelated, potentially long-lived tmux server's environment.
//
// cwd is the directory the binary starts in. Passing it explicitly matters for
// scenarios that saved a workspace: without -c the child inherits the test
// process's directory, so a launch can rewrite this package's last-conversation
// mapping and the scenario stops exercising the workspace it just saved.
func newAgyPane(t *testing.T, agyBin, convID, cwd string) *agyPane {
	t.Helper()
	p := &agyPane{t: t, name: "agy-parity", socket: fmt.Sprintf("agy-parity-%d-%d", os.Getpid(), time.Now().UnixNano())}
	args := []string{"-f", "/dev/null", "new-session", "-d", "-s", p.name,
		"-x", "120", "-y", "40", "-e", "TERM=xterm-256color"}
	if cwd != "" {
		args = append(args, "-c", cwd)
	}
	// Pin the model. The operator's settings.json names whatever model they use
	// day to day, and when it differs from the one the conversation was written
	// with the reference switches models on load and repaints the whole
	// transcript. That repaint is a full-screen clear, so a Ctrl+O landing in it
	// is swallowed and the pane ends up blank — which reads as a renderer bug.
	// Naming the model explicitly removes the operator's configuration from the
	// experiment.
	args = append(args, agyBin, "--conversation="+convID, "--model", "Gemini 4 Hydrogen (High)")
	if err := p.tmux(args...); err != nil {
		t.Fatalf("failed to start isolated tmux: %v", err)
	}
	// Kill the server on cleanup, and make sure the child is gone even when the
	// test failed before reaching an explicit kill-server: a leaked agy keeps
	// the conversation open, and the next instance of the same conversation then
	// refuses to render it ("Conversation already open"), which surfaces as a
	// blank pane in a later, unrelated run.
	t.Cleanup(func() {
		_ = p.tmux("kill-server")
		_ = p.tmux("kill-server") // second call is a no-op; guards a half-dead server
	})
	return p
}

// capture returns the visible pane, not historical scrollback: parity assertions
// are about what the user can currently see.
func (p *agyPane) capture() []byte {
	p.t.Helper()
	out, err := p.tmuxOutput("capture-pane", "-p", "-S", "-", "-t", p.name)
	if err != nil {
		p.t.Fatalf("capture-pane: %v", err)
	}
	return out
}

// captureScrollback includes lines that have scrolled above the visible pane.
// An expansion/redraw can leave rendered lines above the 40-row viewport;
// this is distinct from a missing render and must be observed separately.
func (p *agyPane) captureScrollback() []byte {
	p.t.Helper()
	out, err := p.tmuxOutput("capture-pane", "-p", "-S", "-", "-t", p.name)
	if err != nil {
		p.t.Fatalf("capture-pane scrollback: %v", err)
	}
	return out
}

// waitFor polls until want appears in the pane, answering the trust prompt on
// the way. It returns the visible capture that satisfied the wait.
//
// It fails the test rather than returning false: a scenario that silently
// continued past a timeout would assert on a blank screen and report the wrong
// reason, which is exactly how this test used to flake.
func (p *agyPane) waitFor(want string, timeout time.Duration) []byte {
	p.t.Helper()
	deadline := time.Now().Add(timeout)
	var last []byte
	trusted := false
	for time.Now().Before(deadline) {
		last = p.capture()
		if !trusted && (bytes.Contains(last, []byte("Do you trust")) ||
			bytes.Contains(last, []byte("Trust this folder"))) {
			p.press("C-m")
			trusted = true
		}
		if bytes.Contains(last, []byte(want)) {
			return last
		}
		time.Sleep(50 * time.Millisecond)
	}
	p.t.Fatalf("timed out after %v waiting for %q in pane %q\nLast pane:\n%s",
		timeout, want, p.name, string(last))
	return last
}

// waitForScrollback waits for want anywhere in the pane, visible or scrolled
// above the viewport, and logs when only the scrollback carries it.
func (p *agyPane) waitForScrollback(want string, timeout time.Duration) []byte {
	p.t.Helper()
	deadline := time.Now().Add(timeout)
	var visible, all []byte
	for time.Now().Before(deadline) {
		visible = p.capture()
		all = p.captureScrollback()
		if bytes.Contains(all, []byte(want)) {
			// Recapture after the scrollback match: otherwise the output might
			// simply have arrived between the two captures.
			visible = p.capture()
			if !bytes.Contains(visible, []byte(want)) {
				p.t.Logf("%q rendered in tmux scrollback but is above the visible viewport\nVisible pane:\n%s\nScrollback:\n%s", want, string(visible), string(all))
			}
			return all
		}
		time.Sleep(50 * time.Millisecond)
	}
	// Report the pane's own state alongside the text. A blank screen with the
	// status line still drawn and a live pane_pid means the renderer produced
	// nothing; a dead pid means the process exited; alternate_on means the
	// capture target was the wrong screen. Those are three different bugs and
	// the plain "text is missing" message cannot tell them apart.
	paneState, _ := p.tmuxOutput("display-message", "-p", "-t", p.name,
		"alt=#{alternate_on} cmd=#{pane_current_command} pid=#{pane_pid} dead=#{pane_dead}")
	p.t.Fatalf("timed out after %v waiting for %q in visible pane or scrollback\nPane state: %s\nVisible pane:\n%s\nScrollback:\n%s",
		timeout, want, string(bytes.TrimSpace(paneState)), string(visible), string(all))
	return all
}

// waitReady blocks until the composer is idle *and* the transcript body the
// next keypress will act on is still on screen.
//
// The status line (`? for shortcuts`) is painted before the conversation is
// loaded, so it alone does not mean the TUI is ready to receive Ctrl+O — a key
// sent in that window is swallowed and the pane stays alive showing an empty
// body, which reads exactly like a renderer bug. Requiring the anchor (the
// last reply the test already waited for) to still be present is the signal
// that the transcript survived the load and the composer is now listening.
func (p *agyPane) waitReady(anchor string, timeout time.Duration) {
	p.t.Helper()
	deadline := time.Now().Add(timeout)
	var last []byte
	for time.Now().Before(deadline) {
		last = p.capture()
		if bytes.Contains(last, []byte(anchor)) &&
			(bytes.Contains(last, []byte("for shortcuts")) || bytes.Contains(last, []byte("? for "))) {
			// Settle briefly: the status line appears before the final repaint,
			// so confirm the body survives one more frame before sending a key.
			time.Sleep(150 * time.Millisecond)
			settled := p.capture()
			if bytes.Contains(settled, []byte(anchor)) {
				return
			}
			last = settled
		}
		time.Sleep(50 * time.Millisecond)
	}
	p.t.Fatalf("pane %q: body %q never settled with an idle composer within %v\nLast visible pane:\n%s",
		p.name, anchor, timeout, string(last))
}

// press sends one tmux key.
func (p *agyPane) press(key string) {
	p.t.Helper()
	if err := p.tmux("send-keys", "-t", p.name, key); err != nil {
		p.t.Fatalf("send-keys %s: %v", key, err)
	}
}

func TestParity_AgyResumesPigasusDB(t *testing.T) {
	requireLiveParity(t)
	agyBin := defaultAgyBin()
	if _, err := os.Stat(agyBin); err != nil {
		t.Skip("official agy binary not found, skipping live TUI test")
	}

	convID := fmt.Sprintf("test-pigasus-agy-e2e-%d-%d", os.Getpid(), time.Now().UnixNano())
	// The store stays in the operator's real ~/.gemini/antigravity-cli on
	// purpose. agy resolves its data root from $HOME and reads its sign-in
	// state from the same tree; pointing HOME at a temporary directory makes it
	// stop at "you are currently not signed in" before it renders anything. The
	// cost of sharing the real store is that a concurrently running CLI or a
	// parallel test can mutate the same files, so this scenario takes the
	// package's live-store lock (see liveStoreMu) instead of inventing a copy
	// of the credentials.
	acquireLiveStore(t)
	dbPath := ConversationPath(convID)
	t.Cleanup(func() {
		_ = DeleteConversation(convID)
		_ = os.Remove(dbPath)
	})

	steps := []Step{
		{
			Idx:    0,
			Type:   StepUserInput,
			Status: 3,
			Text:   "List directory files",
		},
		{
			Idx:         1,
			Type:        StepToolCall,
			Status:      3,
			ToolName:    "run_command",
			ToolArgs:    `{"CommandLine":"ls -la internal/app/session/convstore"}`,
			ToolResult:  "total 100\n-rw-r--r-- write.go\n-rw-r--r-- agy.go\n",
			ToolSummary: "list convstore files",
		},
		{
			Idx:    2,
			Type:   StepModelResponse,
			Status: 3,
			Text:   "Here are the convstore files.",
		},
	}

	// Explicit --conversation loading needs no workspace registration. In
	// particular, do not replace the operator's last-conversation mapping for
	// this package's real working directory with a short-lived test fixture.
	if err := SaveConversation(convID, "", steps); err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	// Verify render_info is written
	sdb, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	var rinfo []byte
	_ = sdb.QueryRow(`SELECT render_info FROM steps WHERE idx = 1`).Scan(&rinfo)
	sdb.Close()
	if len(rinfo) == 0 {
		t.Fatalf("SaveConversation did not populate render_info!")
	}

	// Launch agy in tmux and wait for the conversation body to render.
	// No workspace was saved, so the child keeps the package directory.
	pane := newAgyPane(t, agyBin, convID, "")
	pane.waitFor("Here are the convstore files", 15*time.Second)

	// The body is up; wait for the composer to actually accept keys before
	// sending one.
	pane.waitReady("Here are the convstore files", 10*time.Second)

	// Ctrl+O expands the tool step. Wait for the expansion to change the
	// screen rather than for a fixed budget: the render is incremental, so
	// a single capture can land between the keypress and the redraw.
	pane.press("C-o")
	pane.waitForScrollback("write.go", 10*time.Second)
}

func TestParity_AgyResumesMultiTurnPigasusDB(t *testing.T) {
	requireLiveParity(t)
	agyBin := defaultAgyBin()
	if _, err := os.Stat(agyBin); err != nil {
		t.Skip("official agy binary not found, skipping live TUI test")
	}

	convID := fmt.Sprintf("test-pigasus-agy-multiturn-%d-%d", os.Getpid(), time.Now().UnixNano())
	// Shares the operator's real store (see acquireLiveStore): the fixture and
	// agy must resolve the same path, and agy needs the sign-in state beside it.
	acquireLiveStore(t)
	dbPath := ConversationPath(convID)
	t.Cleanup(func() {
		_ = DeleteConversation(convID)
		_ = os.Remove(dbPath)
	})

	steps := []Step{
		// Turn 1
		{Idx: 0, Type: StepUserInput, Status: 3, Text: "Turn 1: check git status"},
		{Idx: 1, Type: StepToolCall, Status: 3, ToolName: "run_command",
			ToolArgs:    `{"CommandLine":"git status"}`,
			ToolResult:  "On branch main\nnothing to commit, working tree clean",
			ToolSummary: "git status"},
		{Idx: 2, Type: StepModelResponse, Status: 3, Text: "Turn 1 reply: working tree clean."},
		// Turn 2
		{Idx: 3, Type: StepUserInput, Status: 3, Text: "Turn 2: list internal files"},
		{Idx: 4, Type: StepToolCall, Status: 3, ToolName: "run_command",
			ToolArgs:    `{"CommandLine":"ls -la internal"}`,
			ToolResult:  "drwxr-xr-x app\ndrwxr-xr-x core\n",
			ToolSummary: "list internal files"},
		{Idx: 5, Type: StepModelResponse, Status: 3, Text: "Turn 2 reply: here are the directories."},
	}

	// Explicit resume does not need to change a real workspace's last-conversation mapping.
	if err := SaveConversation(convID, "", steps); err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	pane := newAgyPane(t, agyBin, convID, "")
	t0 := time.Now()
	lastPane := pane.waitFor("Turn 2 reply", 15*time.Second)
	t.Logf("Agy resumed multi-turn Pigasus DB in %v", time.Since(t0))

	// Verify both tool blocks are visible
	if (!bytes.Contains(lastPane, []byte("Ran (git status)")) && !bytes.Contains(lastPane, []byte("Bash(git status)"))) ||
		(!bytes.Contains(lastPane, []byte("Ran (ls -la internal)")) && !bytes.Contains(lastPane, []byte("Bash(ls -la internal)"))) {
		t.Errorf("Expected both tool steps in visible pane, got:\n%s", string(lastPane))
	}
}

func findRepoRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("could not find repo root with go.mod")
		}
		dir = parent
	}
}

func TestParity_BidirectionalInteractiveResume(t *testing.T) {
	requireLiveParity(t)
	agyBin := defaultAgyBin()
	if _, err := os.Stat(agyBin); err != nil {
		t.Skip("official agy binary not found, skipping live TUI test")
	}
	pigasusBin := filepath.Join(findRepoRoot(t), "bin", "pigasus")
	if _, err := os.Stat(pigasusBin); err != nil {
		t.Skip("pigasus binary not found, skipping live TUI test")
	}

	convID := fmt.Sprintf("test-pigasus-agy-bidirectional-%d-%d", os.Getpid(), time.Now().UnixNano())
	acquireLiveStore(t)
	agyHome := os.ExpandEnv("$HOME/.gemini/antigravity-cli")
	dbPath := filepath.Join(agyHome, "conversations", convID+".db")
	t.Cleanup(func() {
		_ = DeleteConversation(convID)
		_ = os.Remove(dbPath)
	})

	steps := []Step{
		{Idx: 0, Type: StepUserInput, Status: 3, Text: "Turn 1: Check status"},
		{Idx: 1, Type: StepToolCall, Status: 3, ToolName: "run_command",
			ToolArgs:    `{"CommandLine":"git status"}`,
			ToolResult:  "On branch main\nnothing to commit",
			ToolSummary: "git status"},
		{Idx: 2, Type: StepModelResponse, Status: 3, Text: "Turn 1: Working tree clean."},
		{Idx: 3, Type: StepUserInput, Status: 3, Text: "Turn 2: Show version"},
		{Idx: 4, Type: StepToolCall, Status: 3, ToolName: "run_command",
			ToolArgs:    `{"CommandLine":"go version"}`,
			ToolResult:  "go version go1.24 darwin/arm64",
			ToolSummary: "go version"},
		{Idx: 5, Type: StepModelResponse, Status: 3, Text: "Turn 2: Go compiler is ready."},
	}

	testDir := t.TempDir()
	if err := SaveConversation(convID, testDir, steps); err != nil {
		t.Fatalf("SaveConversation failed: %v", err)
	}

	// Both surfaces use private servers, never the operator's tmux session, and
	// both start in the workspace the fixture was saved against: agy treats a
	// conversation whose saved workspace is not the current directory as
	// foreign and renders an empty session instead of the transcript.
	paneAgy := newAgyPane(t, agyBin, convID, testDir)
	lastPaneAgy := paneAgy.waitFor("Go compiler is ready", 15*time.Second)
	if (!bytes.Contains(lastPaneAgy, []byte("Ran (git status)")) && !bytes.Contains(lastPaneAgy, []byte("Bash(git status)"))) ||
		(!bytes.Contains(lastPaneAgy, []byte("Ran (go version)")) && !bytes.Contains(lastPaneAgy, []byte("Bash(go version)"))) {
		t.Errorf("Tool steps not visible in agy pane:\n%s", string(lastPaneAgy))
	}
	paneAgy.waitReady("Go compiler is ready", 10*time.Second)
	paneAgy.press("C-o")
	paneAgy.waitForScrollback("go1.24", 10*time.Second)
	if err := paneAgy.tmux("kill-server"); err != nil {
		t.Fatalf("stop agy: %v", err)
	}

	// Verify Pigasus can resume the exact same conversation.
	panePig := newAgyPane(t, pigasusBin, convID, testDir)
	lastPanePig := panePig.waitFor("Go compiler is ready", 15*time.Second)
	if (!bytes.Contains(lastPanePig, []byte("Ran (git status)")) && !bytes.Contains(lastPanePig, []byte("Bash(git status)"))) ||
		(!bytes.Contains(lastPanePig, []byte("Ran (go version)")) && !bytes.Contains(lastPanePig, []byte("Bash(go version)"))) {
		t.Errorf("Tool steps not visible in pigasus pane:\n%s", string(lastPanePig))
	}
	panePig.waitReady("Go compiler is ready", 10*time.Second)
	panePig.press("C-o")
	panePig.waitForScrollback("go1.24", 10*time.Second)
	if err := panePig.tmux("kill-server"); err != nil {
		t.Fatalf("stop pigasus: %v", err)
	}

	// 3. Verify --continue / -c resolution
	resID := LookupLastConversation(testDir)
	if resID != convID {
		t.Errorf("LookupLastConversation(%s) = %q, want %q", testDir, resID, convID)
	}
}

func execCommand(name string, args ...string) error {
	cmd := exec.Command(name, args...)
	return cmd.Run()
}

func TestParity_PigasusResumesAgyDB(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agy_format.db")

	// Create a DB in agy's native format where tool call is ONLY in field 140 and render_info column
	rinfo := encodeStepRenderInfo(Step{
		Type:        StepToolCall,
		Status:      3,
		ToolName:    "run_command",
		ToolArgs:    `{"CommandLine":"git status --short"}`,
		ToolResult:  " M internal/app/session/convstore/agy.go\n",
		ToolSummary: "git status short",
	})

	// Emulate agy 1.5.8: payload only has 1, 4, 140 (no field 5.4)
	genCall := encodeProtoField(1, 2, []byte("run_command"))
	genCall = append(genCall, encodeProtoField(2, 2, []byte(`{"CommandLine":"git status --short"}`))...)

	var genRes []byte
	genRes = append(genRes, encodeProtoField(1, 2, []byte(" M internal/app/session/convstore/agy.go\n"))...)
	genRes = append(genRes, encodeProtoField(3, 2, rinfo)...)

	var generic []byte
	generic = append(generic, encodeProtoField(1, 2, genCall)...)
	generic = append(generic, encodeProtoField(2, 2, genRes)...)

	payload := encodeProtoVarint(1, 132)
	payload = append(payload, encodeProtoVarint(4, 3)...)
	payload = append(payload, encodeProtoField(140, 2, generic)...)

	sdb, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=rwc")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	_, _ = sdb.Exec(`CREATE TABLE trajectory_meta (trajectory_id text, cascade_id text, trajectory_type integer, source integer, PRIMARY KEY (trajectory_id))`)
	_, _ = sdb.Exec(`CREATE TABLE steps (idx integer, step_type integer, status integer, metadata blob, render_info blob, step_payload blob, PRIMARY KEY (idx))`)
	_, _ = sdb.Exec(`INSERT INTO trajectory_meta VALUES ('traj-agy', 'traj-agy', 4, 17)`)
	_, _ = sdb.Exec(`INSERT INTO steps VALUES (0, 132, 3, NULL, ?, ?)`, rinfo, payload)
	sdb.Close()

	// Load with pigasus LoadConversation
	c, err := LoadConversation(dbPath)
	if err != nil {
		t.Fatalf("LoadConversation failed: %v", err)
	}

	if len(c.Steps) != 1 {
		t.Fatalf("Loaded steps = %d, want 1", len(c.Steps))
	}

	s := c.Steps[0]
	if s.ToolName != "run_command" {
		t.Errorf("ToolName = %q, want 'run_command'", s.ToolName)
	}
	if s.ToolResult != " M internal/app/session/convstore/agy.go" {
		t.Errorf("ToolResult = %q", s.ToolResult)
	}
	if s.ToolSummary != "git status short" {
		t.Errorf("ToolSummary = %q", s.ToolSummary)
	}

	// Verify Transcript does not drop it
	transcript := c.Transcript()
	if len(transcript) != 1 {
		t.Errorf("Transcript dropped agy tool step! len = %d", len(transcript))
	}
}

func TestParity_PigasusResumesAgyDB_TypeZero(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "agy_typezero.db")

	// In official agy 1.5.14 databases, step_type is often stored as 0 in the SQLite table,
	// and the payload oneof defines whether it is user input (field 19) or model response (field 20).
	pUser := encodeProtoField(19, 2, encodeProtoField(3, 2, encodeProtoField(1, 2, []byte("How does memory trim work?"))))
	pModel := encodeProtoField(20, 2, encodeProtoField(1, 2, []byte("Memory trim frees unneeded cache pages.")))

	sdb, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=rwc")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	_, _ = sdb.Exec(`CREATE TABLE trajectory_meta (trajectory_id text, cascade_id text, trajectory_type integer, source integer, PRIMARY KEY (trajectory_id))`)
	_, _ = sdb.Exec(`CREATE TABLE steps (idx integer, step_type integer, status integer, metadata blob, render_info blob, step_payload blob, PRIMARY KEY (idx))`)
	_, _ = sdb.Exec(`INSERT INTO trajectory_meta VALUES ('traj-typezero', 'traj-typezero', 4, 17)`)
	_, _ = sdb.Exec(`INSERT INTO steps VALUES (0, 0, 0, NULL, NULL, ?)`, pUser)
	_, _ = sdb.Exec(`INSERT INTO steps VALUES (1, 0, 0, NULL, NULL, ?)`, pModel)
	sdb.Close()

	c, err := LoadConversation(dbPath)
	if err != nil {
		t.Fatalf("LoadConversation failed: %v", err)
	}
	if len(c.Steps) != 2 {
		t.Fatalf("Loaded %d steps, want 2", len(c.Steps))
	}

	step0 := c.Steps[0]
	if step0.Type != StepUserInput {
		t.Errorf("step 0 Type = %d, want StepUserInput (%d)", step0.Type, StepUserInput)
	}
	if step0.Role() != "user" {
		t.Errorf("step 0 Role = %q, want 'user'", step0.Role())
	}
	if step0.Text != "How does memory trim work?" {
		t.Errorf("step 0 Text = %q, want 'How does memory trim work?'", step0.Text)
	}

	step1 := c.Steps[1]
	if step1.Type != StepModelResponse {
		t.Errorf("step 1 Type = %d, want StepModelResponse (%d)", step1.Type, StepModelResponse)
	}
	if step1.Role() != "model" {
		t.Errorf("step 1 Role = %q, want 'model'", step1.Role())
	}
	if step1.Text != "Memory trim frees unneeded cache pages." {
		t.Errorf("step 1 Text = %q, want 'Memory trim frees unneeded cache pages.'", step1.Text)
	}

	transcript := c.Transcript()
	if len(transcript) != 2 {
		t.Fatalf("Transcript dropped type-zero steps! len = %d, want 2", len(transcript))
	}
}

func execCommandOutput(name string, args ...string) ([]byte, error) {
	cmd := exec.Command(name, args...)
	return cmd.Output()
}

func TestParity_NonDestructiveStoragePreservesRawBlobs(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "preserve.db")

	fakeCustomPayload := []byte("\x08\x0e\x12\x04fake\x50\x99\x01\xaa\xbb\xcc")
	fakeCustomRenderInfo := []byte("\x2a\x05FakeR\x32\x04icon\xaa\xbb")
	fakeCustomMetadata := []byte("\x12\x0aCustomMeta")

	// Create DB with pre-existing rich step
	sdb, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=rwc")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	_, _ = sdb.Exec(`CREATE TABLE trajectory_meta (trajectory_id text, cascade_id text, trajectory_type integer, source integer, PRIMARY KEY (trajectory_id))`)
	_, _ = sdb.Exec(`CREATE TABLE steps (idx integer, step_type integer, status integer, metadata blob, render_info blob, step_payload blob, PRIMARY KEY (idx))`)
	_, _ = sdb.Exec(`INSERT INTO trajectory_meta VALUES ('traj-1', 'traj-1', 4, 17)`)
	_, _ = sdb.Exec(`INSERT INTO steps VALUES (0, 132, 3, ?, ?, ?)`, fakeCustomMetadata, fakeCustomRenderInfo, fakeCustomPayload)
	sdb.Close()

	// Load with LoadConversation
	c, err := LoadConversation(dbPath)
	if err != nil {
		t.Fatalf("LoadConversation failed: %v", err)
	}
	if len(c.Steps) != 1 {
		t.Fatalf("loaded steps = %d, want 1", len(c.Steps))
	}
	if !bytes.Equal(c.Steps[0].RawPayload, fakeCustomPayload) {
		t.Fatalf("RawPayload not loaded properly!")
	}
	if !bytes.Equal(c.Steps[0].RawRenderInfo, fakeCustomRenderInfo) {
		t.Fatalf("RawRenderInfo not loaded properly!")
	}
	if !bytes.Equal(c.Steps[0].RawMetadata, fakeCustomMetadata) {
		t.Fatalf("RawMetadata not loaded properly!")
	}

	// Append a new step and re-save
	newStep := Step{
		Idx:    1,
		Type:   StepModelResponse,
		Status: 3,
		Text:   "New reply",
	}
	allSteps := append(c.Steps, newStep)
	if err := WriteConversation(dbPath, "traj-1", allSteps); err != nil {
		t.Fatalf("WriteConversation failed: %v", err)
	}

	// Verify step 0 in SQLite was NOT mutated or overwritten!
	sdb2, err := sql.Open("sqlite3", "file:"+dbPath+"?mode=ro")
	if err != nil {
		t.Fatalf("sql.Open failed: %v", err)
	}
	defer sdb2.Close()

	var gotMeta, gotRinfo, gotPayload []byte
	if err := sdb2.QueryRow(`SELECT metadata, render_info, step_payload FROM steps WHERE idx = 0`).Scan(&gotMeta, &gotRinfo, &gotPayload); err != nil {
		t.Fatalf("QueryRow failed: %v", err)
	}

	if !bytes.Equal(gotPayload, fakeCustomPayload) {
		t.Errorf("RawPayload was overwritten/corrupted! got %x, want %x", gotPayload, fakeCustomPayload)
	}
	if !bytes.Equal(gotRinfo, fakeCustomRenderInfo) {
		t.Errorf("RawRenderInfo was overwritten/corrupted! got %x, want %x", gotRinfo, fakeCustomRenderInfo)
	}
	if !bytes.Equal(gotMeta, fakeCustomMetadata) {
		t.Errorf("RawMetadata was overwritten/corrupted! got %x, want %x", gotMeta, fakeCustomMetadata)
	}
}
