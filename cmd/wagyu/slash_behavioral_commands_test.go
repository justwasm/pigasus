// slash_cycle194_test.go — cycle 194: the seven Class-D behavioral slash
// commands from TODO.md §2.4. These tests pin the cycle-194 contract:
//
//   - /goal, /plan, /learn, /grill-me echo the slash token, set
//     m.streaming=true and append a synthesized prompt to m.session
//     so the model receives a real "generate" request rather than the
//     cycle-167 stub message.
//
//   - /schedule, /browser return the canonical "Error ID: <uuid>-1"
//     row (the reference's behaviour for commands that have no
//     working backend). m.streaming stays false — there is no model
//     call to gate.
//
//   - /keybindings creates ~/.gemini/antigravity-cli/keybindings.json
//     if it does not exist and returns a non-nil editor command.
//     The editor itself is not invoked in tests; we only verify the
//     contract (file existence + cmd).
//
// Tests deliberately do not run a *tea.Program, so the agent-loop
// goroutine inside streamPrompt is never executed; the returned
// tea.Cmd is held but never fired. m.session, m.streaming and
// m.response are observed synchronously after handleSlashCommand.
package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
)

// TestRunGoal_SendsPrompt exercises the cycle-194 /goal handler.
// Confirms the slash token is echoed, m.streaming is flipped, and a
// user-role message is appended to m.session — the three signals a
// normal Enter-key submit also produces (see tui_run.go).
func TestRunGoal_SendsPrompt(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/goal ship the v2 feature")
	if cmd == nil {
		t.Fatal("/goal should return a non-nil tea.Cmd (the model stream)")
	}
	if !m.streaming {
		t.Error("/goal should set m.streaming=true while the model runs")
	}
	if len(m.response) == 0 || m.response[0] != "> /goal" {
		t.Errorf("/goal first echo = %q, want %q", firstNLines(m.response, 1), "> /goal")
	}
	if got := lastUserMessage(m); !strings.Contains(got, "goal") {
		t.Errorf("/goal session message lost the goal intent: %q", got)
	}
}

// TestRunGoal_NoArgsAlsoSendsPrompt confirms bare "/goal" still sends
// a prompt. The cycle-167 handler printed "  Goal cleared." for the
// empty-arg path; cycle 194 matches REF (which always generates).
func TestRunGoal_NoArgsAlsoSendsPrompt(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/goal")
	if cmd == nil {
		t.Fatal("bare /goal should return a non-nil tea.Cmd")
	}
	if !m.streaming {
		t.Error("bare /goal should set m.streaming=true")
	}
	if m.response[0] != "> /goal" {
		t.Errorf("bare /goal echo = %q, want %q", m.response[0], "> /goal")
	}
}

// TestRunPlan_SendsPrompt exercises the cycle-194 /plan handler. The
// cycle-167 implementation toggled the agent mode flag (now bound to
// Shift+Tab); cycle 194 sends a planning prompt to the model.
func TestRunPlan_SendsPrompt(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/plan")
	if cmd == nil {
		t.Fatal("/plan should return a non-nil tea.Cmd")
	}
	if !m.streaming {
		t.Error("/plan should set m.streaming=true")
	}
	if m.response[0] != "> /plan" {
		t.Errorf("/plan echo = %q, want %q", m.response[0], "> /plan")
	}
	if got := lastUserMessage(m); !strings.Contains(got, "plan") {
		t.Errorf("/plan session message lost the plan intent: %q", got)
	}
}

// TestRunLearn_SendsPrompt exercises the cycle-194 /learn handler.
// The cycle-167 stub appended to m.learnings and printed a fixed
// echo; cycle 194 sends a prompt.
func TestRunLearn_SendsPrompt(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/learn **API**: endpoint is *down*")
	if cmd == nil {
		t.Fatal("/learn should return a non-nil tea.Cmd")
	}
	if !m.streaming {
		t.Error("/learn should set m.streaming=true")
	}
	if m.response[0] != "> /learn" {
		t.Errorf("/learn echo = %q, want %q", m.response[0], "> /learn")
	}
	if got := lastUserMessage(m); !strings.Contains(got, "learn") {
		t.Errorf("/learn session message lost the learn intent: %q", got)
	}
}

// TestRunGrillMe_SendsPrompt exercises the cycle-194 /grill-me
// handler. Cycle 167 printed a stub "Interview mode enabled" line;
// cycle 194 sends a Socratic-style prompt.
func TestRunGrillMe_SendsPrompt(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/grill-me")
	if cmd == nil {
		t.Fatal("/grill-me should return a non-nil tea.Cmd")
	}
	if !m.streaming {
		t.Error("/grill-me should set m.streaming=true")
	}
	if m.response[0] != "> /grill-me" {
		t.Errorf("/grill-me echo = %q, want %q", m.response[0], "> /grill-me")
	}
	if got := lastUserMessage(m); !strings.Contains(got, "question") &&
		!strings.Contains(got, "reasoning") {
		t.Errorf("/grill-me session message lost the Socratic intent: %q", got)
	}
}

// TestRunTeamwork_SendsPrompt exercises the /teamwork handler.
// Confirms /teamwork echoes the token, sets streaming, and appends
// a teamwork orchestration prompt to the session.
func TestRunTeamwork_SendsPrompt(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/teamwork build multi-agent system")
	if cmd == nil {
		t.Fatal("/teamwork should return a non-nil tea.Cmd")
	}
	if !m.streaming {
		t.Error("/teamwork should set m.streaming=true")
	}
	if m.response[0] != "> /teamwork" {
		t.Errorf("/teamwork echo = %q, want %q", m.response[0], "> /teamwork")
	}
	if got := lastUserMessage(m); !strings.Contains(got, "team") &&
		!strings.Contains(got, "autonomous agents") {
		t.Errorf("/teamwork session message lost the teamwork intent: %q", got)
	}
}

// TestRunBoost_SendsPrompt exercises the /boost handler.
// Confirms /boost echoes the token, sets streaming, and appends
// a boost multi-agent orchestrator prompt to the session.
func TestRunBoost_SendsPrompt(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/boost analyze architecture")
	if cmd == nil {
		t.Fatal("/boost should return a non-nil tea.Cmd")
	}
	if !m.streaming {
		t.Error("/boost should set m.streaming=true")
	}
	if m.response[0] != "> /boost" {
		t.Errorf("/boost echo = %q, want %q", m.response[0], "> /boost")
	}
	if got := lastUserMessage(m); !strings.Contains(got, "deep thinking") &&
		!strings.Contains(got, "verification") {
		t.Errorf("/boost session message lost the boost intent: %q", got)
	}
}

// TestRunSchedule_ErrorID exercises the cycle-194 /schedule handler.
// REF returns "Error ID: <uuid>-1" for commands with no backend;
// cycle 194 surfaces the same row verbatim. Cycle 217 also surfaces
// the cycle-194 quota-warning line that precedes the Error ID
// (`/tmp/agycmp/panels/ppref.schedule.raw.txt`: the warning line is
// fg 166, the Error ID line is fg 241). m.streaming stays false
// because there is no model call.
func TestRunSchedule_ErrorID(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/schedule")
	if cmd != nil {
		t.Error("/schedule should NOT schedule a tea.Cmd (no model call)")
	}
	if m.streaming {
		t.Error("/schedule should leave m.streaming=false")
	}
	if len(m.response) < 4 {
		t.Fatalf("/schedule produced %d transcript rows, want at least 4",
			len(m.response))
	}
	// Cycle 195 layout:
	//   [0] > /<cmd>
	//   [1]   ⎿  <warning line>      (fg 166)
	//   [2] <Error ID line>           (fg 241)
	//   [3] <blank>
	warnRow := stripSGR(m.response[1])
	warnRow = strings.TrimPrefix(warnRow, "  \u23bf  ")
	if !strings.HasPrefix(warnRow, "\u26a0 Individual quota reached. Please upgrade your subscription to increase your limits. Resets in ") {
		t.Errorf("/schedule warning row = %q", warnRow)
	}
	row := stripSGR(m.response[2])
	// Format is "Error ID: <8-4-4-4-12 hex>-1".
	re := regexp.MustCompile(`^Error ID: [0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}-1$`)
	if !re.MatchString(row) {
		t.Errorf("/schedule Error ID row = %q, want Error ID: <uuid>-1", row)
	}
}

// TestRunBrowser_ErrorID mirrors TestRunSchedule_ErrorID for /browser.
// Same "Error ID: <uuid>-1" shape; same no-stream behaviour; same
// cycle-217 quota-warning line preceding it.
func TestRunBrowser_ErrorID(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/browser")
	if cmd != nil {
		t.Error("/browser should NOT schedule a tea.Cmd")
	}
	if m.streaming {
		t.Error("/browser should leave m.streaming=false")
	}
	if len(m.response) < 4 {
		t.Fatalf("/browser produced %d transcript rows, want at least 4",
			len(m.response))
	}
	warnRow := stripSGR(m.response[1])
	warnRow = strings.TrimPrefix(warnRow, "  \u23bf  ")
	if !strings.HasPrefix(warnRow, "\u26a0 Individual quota reached. Please upgrade your subscription to increase your limits. Resets in ") {
		t.Errorf("/browser warning row = %q", warnRow)
	}
	row := stripSGR(m.response[2])
	re := regexp.MustCompile(`^Error ID: [0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}-1$`)
	if !re.MatchString(row) {
		t.Errorf("/browser Error ID row = %q, want Error ID: <uuid>-1", row)
	}
}

// TestRunScheduleAndBrowser_ErrorIDsAreUnique verifies two
// consecutive invocations produce different UUIDs. The TODO
// interpretation is that REF regenerates the ID per call so the
// user can correlate a fresh error with logs / support tickets.
// Cycle 217: the layout now has two `Error ID:` rows on the page
// (one per call) — we read those specifically.
func TestRunScheduleAndBrowser_ErrorIDsAreUnique(t *testing.T) {
	m := newSlashModel(t)
	m.handleSlashCommand("/schedule")
	m.handleSlashCommand("/browser")
	if len(m.response) < 8 {
		t.Fatalf("need 8 transcript rows, got %d", len(m.response))
	}
	// Cycle-217 layout per call:
	//   [0] > /<cmd>
	//   [1]   ⎿  <warning>
	//   [2] <Error ID line>
	//   [3] <blank>
	// Schedule fills [0..3], browser fills [4..7]. Strip SGR + prefix
	// from the two Error ID rows and assert they differ.
	stripErr := func(raw string) string {
		s := stripSGR(raw)
		s = strings.TrimPrefix(s, "  \u23bf  ")
		return s
	}
	schedID := stripErr(m.response[2])
	browserID := stripErr(m.response[6])
	if schedID == browserID {
		t.Errorf("two error IDs collided: %q == %q", schedID, browserID)
	}
	if !strings.HasPrefix(schedID, "Error ID: ") ||
		!strings.HasPrefix(browserID, "Error ID: ") {
		t.Errorf("missing Error ID prefix: schedule=%q browser=%q",
			schedID, browserID)
	}
}

// TestRunKeybindings_CreatesFile verifies /keybindings creates
// ~/.gemini/antigravity-cli/keybindings.json with a JSON skeleton if
// the file does not exist. We redirect $ANTIGRAVITY_HOME so the test
// does not touch the real config dir.
func TestRunKeybindings_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	// Cycle 235: explicitly clear $EDITOR / $VISUAL so the test is
	// deterministic across machines — the cycle-194 version inherited
	// the parent env and so depended on whatever the developer had
	// set. The cycle-235 fix surfaces a clear error in the empty case,
	// which is the assertion below.
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/keybindings")

	path := filepath.Join(dir, "keybindings.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("/keybindings did not create %s: %v", path, err)
	}
	// Body is a JSON object skeleton. "{}" is what the cycle-194
	// implementation writes on a first run; any non-empty valid
	// JSON object is acceptable since the editor overwrites it.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read keybindings.json: %v", err)
	}
	if !strings.Contains(string(body), "{") {
		t.Errorf("keybindings.json is not a JSON object: %q", body)
	}

	// Cycle 235: with no $EDITOR/$VISUAL the helper returns the
	// "No editor configured" error; we still don't launch an editor
	// process. The bindings table renders alongside the error so the
	// user has something useful to read; the regression-specific
	// tests in slash_cycle235_test.go own that contract.
	if cmd != nil {
		t.Errorf("/keybindings returned non-nil editor cmd with no $EDITOR/$VISUAL: %v", cmd)
	}
}

// TestRunKeybindings_NoCreateIfExists confirms /keybindings does not
// overwrite an existing keybindings.json. The test creates the file
// with a sentinel payload, runs /keybindings, and asserts the bytes
// are unchanged.
func TestRunKeybindings_NoCreateIfExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	// Cycle 235: see TestRunKeybindings_CreatesFile — make the env
	// deterministic.
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	path := filepath.Join(dir, "keybindings.json")
	sentinel := []byte(`{"sentinel": true}` + "\n")
	if err := os.WriteFile(path, sentinel, 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	m := newSlashModel(t)
	m.handleSlashCommand("/keybindings")

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read keybindings.json: %v", err)
	}
	if string(got) != string(sentinel) {
		t.Errorf("/keybindings overwrote existing file: got %q, want %q", got, sentinel)
	}
}

// lastUserMessage returns the content of the most recent user-role
// message in m.session, or empty if there are none. The cycle-194
// handlers all append exactly one user message per slash invocation.
func lastUserMessage(m *runMode) string {
	for i := len(m.session) - 1; i >= 0; i-- {
		if m.session[i].Role == "user" {
			return m.session[i].Content
		}
	}
	return ""
}

// firstNLines is a tiny helper used in error messages to keep the
// transcript dump compact when only the leading row matters.
func firstNLines(rows []string, n int) string {
	if len(rows) < n {
		n = len(rows)
	}
	return strings.Join(rows[:n], "\n")
}

// Compile-time check: the cycle-194 handlers reference a few types
// from the agent / backend packages. If those imports become unused
// after a future refactor, `go build` will fail loudly; this guard
// documents the contract instead.
var (
	_ = auth.AntigravityConfigHome
	_ = backend.Message{Role: "user"}
)
