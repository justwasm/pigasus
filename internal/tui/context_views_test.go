// context_views_test.go — the host's side of the /logs panel's Wire and
// Trajectory views.
//
// The views themselves are internal/ui/views/debugpanel's; these tests pin
// the wiring — that the host hands the panel facts it measured with the
// code that owns them, and that `/logs <view>` lands on the view the word
// names.

package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/views/debugpanel"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
)

// TestContextWireStateSectionsAreThePromptsOwn — the Wire view's system
// instruction rows are the sections Build emits, measured by the same
// function /context's System prompt row uses.
func TestContextWireStateSectionsAreThePromptsOwn(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120

	ws := m.contextWireState()
	if len(ws.Sections) == 0 {
		t.Fatal("the Wire view has no systemInstruction sections")
	}
	var bytes int
	for _, s := range ws.Sections {
		if s.Bytes <= 0 {
			t.Errorf("section %q has %d bytes; the prompt carries it", s.Name, s.Bytes)
		}
		if s.Tokens <= 0 {
			t.Errorf("section %q has %d tokens; the prompt carries it", s.Name, s.Tokens)
		}
		bytes += s.Bytes
	}
	// The sections are the whole prompt: their sum plus the single
	// newline Build writes between them is its byte length. A section the
	// view dropped or invented would break this by more than a newline.
	full := len(buildPrompt(m))
	if want := bytes + len(ws.Sections) - 1; full != want {
		t.Errorf("the prompt is %d bytes; the sections sum to %d and the %d separators make %d",
			full, bytes, len(ws.Sections)-1, want)
	}
	if ws.Note == "" {
		t.Error("the Wire view carries no caveat; its sizes are host-side")
	}
}

// buildPrompt is the prompt the turn would send, built by the host's own
// entry point so the test measures the same document the request does.
func buildPrompt(m *RootModel) string { return m.systemPrompt() }

// TestContextWireStateToolsAreTheWireDeclarations — each tool row is
// sized by serialising that declaration the way the request does.
func TestContextWireStateToolsAreTheWireDeclarations(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"

	ws := m.contextWireState()
	decls := m.wireToolDecls()
	if len(ws.Tools) != len(decls) {
		t.Fatalf("the Wire view lists %d tools, want the %d advertised", len(ws.Tools), len(decls))
	}
	if len(ws.Tools) == 0 {
		t.Skip("this build advertises no tools")
	}
	for i, tool := range ws.Tools {
		if tool.Name != decls[i].Name {
			t.Errorf("tool %d = %q, want %q", i, tool.Name, decls[i].Name)
		}
		if tool.Bytes <= 0 {
			t.Errorf("tool %q has %d wire bytes", tool.Name, tool.Bytes)
		}
	}
	// The rows are the /context System tools row's own measurement.
	var tokens int
	for _, tool := range ws.Tools {
		tokens += tool.Tokens
	}
	if st := m.contextState(); st.SystemTools > 0 && tokens == 0 {
		t.Errorf("the System tools row reports %d tokens and the Wire view reports 0", st.SystemTools)
	}
}

// TestContextWireStateMessagesAreTheTranscript — one row per message,
// with the request's role and the message's own size.
func TestContextWireStateMessagesAreTheTranscript(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.setSessionMessages([]backend.Message{
		{Role: "user", Content: "fix the build"},
		{Role: "assistant", Content: "looking"},
		{Role: "user", Content: "<USER_REQUEST>go on</USER_REQUEST>"},
	})

	ws := m.contextWireState()
	if len(ws.Messages) != 3 {
		t.Fatalf("the Wire view lists %d messages, want the transcript's 3", len(ws.Messages))
	}
	roles := []string{"user", "model", "user"}
	for i, want := range roles {
		if ws.Messages[i].Role != want {
			t.Errorf("message %d role = %q, want %q", i+1, ws.Messages[i].Role, want)
		}
		if ws.Messages[i].Index != i+1 {
			t.Errorf("message %d index = %d, want %d", i+1, ws.Messages[i].Index, i+1)
		}
		if ws.Messages[i].Bytes <= 0 {
			t.Errorf("message %d has %d bytes", i+1, ws.Messages[i].Bytes)
		}
	}
	if ws.Messages[2].Kind != "envelope" {
		t.Errorf("the wrapped request's kind = %q, want envelope", ws.Messages[2].Kind)
	}
	if ws.Messages[1].Kind != "reply" {
		t.Errorf("the assistant message's kind = %q, want reply", ws.Messages[1].Kind)
	}
}

// TestContextTrajectoryStateReadsTheStoresSteps — the Trajectory view's
// rows are the steps convstore assembles, one row each, in order.
func TestContextTrajectoryStateReadsTheStoresSteps(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"

	// A user turn, a tool call and a reply.
	m.conversation.blocks = nil
	m.setSessionMessages([]backend.Message{
		{Role: "user", Content: "run the build"},
		{Role: "assistant", Content: "the build passed"},
	})
	m.recordTrajectoryCalls([]toolsteps.Step{
		toolsteps.NewToolStep("run_command", []byte(`{"CommandLine":"go build ./..."}`), "ok", false),
	})

	ts := m.contextTrajectoryState()
	want := convstore.BuildSteps(m.trajectoryFacts())
	if len(ts.Steps) != len(want) {
		t.Fatalf("the view lists %d steps, want the store's %d", len(ts.Steps), len(want))
	}
	for i, s := range want {
		if ts.Steps[i].Index != s.Idx {
			t.Errorf("step %d index = %d, want the store's %d", i, ts.Steps[i].Index, s.Idx)
		}
	}
	// The user's prompt is the first step and it has a body.
	if ts.Steps[0].Kind != debugpanel.TrajUser {
		t.Errorf("step 1 kind = %q, want user", ts.Steps[0].Kind)
	}
	if len(ts.Steps[0].Detail) == 0 {
		t.Error("the user step has no body to open")
	}
}

// TestContextTrajectoryStepSummariesAreTheStoresOwn — a tool step's
// collapsed line is render_info's prefix and summary, not a string the
// view rebuilt.
//
// The expected line carries the parentheses the reference draws
// ("Ran (go build ./...)"). The store's own fields are still where the
// line comes from — this test seeds render_info's shape rather than a
// display string — but the join is the renderer's, and the renderer
// wraps the noun phrase in parentheses.
func TestContextTrajectoryStepSummariesAreTheStoresOwn(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.setSessionMessages([]backend.Message{
		{Role: "user", Content: "build"},
		{Role: "assistant", Content: "done"},
	})
	// A tool step carries the store's own render_info fields; the host
	// seeds the trajectory with them and the view reads them back.
	m.session.sup().RecordTrajectoryCalls([]convstore.Step{{
		Type:        convstore.StepToolCall,
		Status:      3,
		ToolName:    "run_command",
		ToolPrefix:  "Ran",
		ToolSummary: "go build ./...",
		ToolResult:  "ok",
		ToolArgs:    `{"CommandLine":"go build ./..."}`,
	}})

	ts := m.contextTrajectoryState()
	var tool *debugpanel.TrajStep
	for i := range ts.Steps {
		if ts.Steps[i].Kind == debugpanel.TrajTool {
			tool = &ts.Steps[i]
			break
		}
	}
	if tool == nil {
		t.Fatal("the trajectory has no tool step")
	}
	if want := "Ran (go build ./...)"; tool.Summary != want {
		t.Errorf("summary = %q, want the store's own %q", tool.Summary, want)
	}
	if len(tool.Detail) == 0 {
		t.Error("the tool step has no body")
	}
}

// TestContextTrajectoryMarksFoldedSteps — a step an earlier checkpoint
// summarized is marked, which is the reason the view exists.
func TestContextTrajectoryMarksFoldedSteps(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.setSessionMessages([]backend.Message{
		{Role: "user", Content: "old prompt"},
		{Role: "assistant", Content: "old reply"},
		{Role: "user", Content: "new prompt"},
		{Role: "assistant", Content: "new reply"},
	})
	m.session.sup().RecordTrajectoryCalls([]convstore.Step{{
		Type:          convstore.StepCheckpoint,
		IncludedStart: 1,
		IncludedEnd:   3,
		Summary:       "the old turn",
		Strategy:      "SAME_MODEL",
		TokensBefore:  9000,
		TokensAfter:   1200,
	}})

	ts := m.contextTrajectoryState()
	var folded, unfolded int
	for _, s := range ts.Steps {
		if s.Kind == debugpanel.TrajCheckpoint {
			continue
		}
		if s.Folded {
			folded++
		} else {
			unfolded++
		}
	}
	if folded == 0 {
		t.Error("no step is marked as folded; the checkpoint folded steps 1–2")
	}
	if unfolded == 0 {
		t.Error("every step is marked as folded; the checkpoint only covers the first two")
	}
	if ts.Checkpoints != 1 {
		t.Errorf("the heading counts %d checkpoints, want 1", ts.Checkpoints)
	}
}

// TestContextTrajectoryRowRendersThroughThePanel — the panel's Trajectory
// view renders the host's facts: the bar, the heading, the rows, the
// counter and the legend.
func TestContextTrajectoryRowRendersThroughThePanel(t *testing.T) {
	m := mkPanel(panelregistry.Debug, "/debug", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120
	m.viewport.height = 40
	m.setSessionMessages([]backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	})
	m.openSlashPanelArgs("debug", "traj")

	lines := plainLines(m)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(lines[0], "Trajectory") {
		t.Errorf("row 0 = %q, want the bar", lines[0])
	}
	if !strings.Contains(joined, "Trajectory") || !strings.Contains(joined, "2 steps") {
		t.Errorf("the view's heading is missing:\n%s", joined)
	}
	if !strings.Contains(joined, "hello") {
		t.Errorf("the user step's line is missing:\n%s", joined)
	}
	if !strings.Contains(joined, "enter open/read") {
		t.Errorf("the legend is missing:\n%s", joined)
	}
	// The reader's own key is named too: a key the legend does not mention
	// is a key nobody finds.
	if !strings.Contains(joined, "v read full") {
		t.Errorf("the legend does not name the reader key:\n%s", joined)
	}
}

// TestLogsViewArgumentRoutes — `/logs wire` and its aliases land on the
// view the word names; a word that names no view opens the log view.
func TestLogsViewArgumentRoutes(t *testing.T) {
	// The bare command lands on the panel's own default, whatever that is:
	// a literal here would silently re-pin the landing view from a test
	// about argument routing.
	landing := debugpanel.New(debugpanel.Seams{}).View()
	cases := []struct {
		args string
		want debugpanel.View
	}{
		{"", landing},
		{"wire", debugpanel.ViewWire},
		{"payload", debugpanel.ViewWire},
		{"traj", debugpanel.ViewTrajectory},
		{"trajectory", debugpanel.ViewTrajectory},
		{"steps", debugpanel.ViewTrajectory},
		{"logs", debugpanel.ViewLogs},
		{"log", debugpanel.ViewLogs},
		// The Requests view answers to every spelling of "request", so
		// `/logs request` and `/logs requests` are the same view rather
		// than two views one letter apart.
		{"requests", debugpanel.ViewRequests},
		{"request", debugpanel.ViewRequests},
		{"http", debugpanel.ViewRequests},
		// A word no view answers to leaves the default up rather than
		// rejecting the command.
		{"nonsense", landing},
	}
	for _, c := range cases {
		m := mkPanel(panelregistry.None, "", 0)
		m.session.model = "Gemini 3.8 Flash (High)"
		if !m.openSlashPanelArgs("debug", c.args) {
			t.Errorf("/logs %q was refused", c.args)
			continue
		}
		if m.overlays.panel.kind != panelregistry.Debug {
			t.Errorf("/logs %q opened %v, want the Logs panel", c.args, m.overlays.panel.kind)
			continue
		}
		if got := m.debugPanel().View(); got != c.want {
			t.Errorf("/logs %q = %v, want %v", c.args, got, c.want)
		}
	}
}

// TestLogsTabCycleStaysInsideThePanel — Tab moves between the views and
// never leaves the panel: the views are one surface.
//
// The lap length is ViewCount(), not a literal: the assertion is "Tab is a
// cycle that returns to where it started", and a fourth view must not have
// to edit a 3 into a 4 to keep saying that.
func TestLogsTabCycleStaysInsideThePanel(t *testing.T) {
	m := mkPanel(panelregistry.Debug, "/debug", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120
	m.viewport.height = 40

	start := m.debugPanel().View()
	for i := 0; i < debugpanel.ViewCount(); i++ {
		handled, _ := m.handlePanelKey(tea.Key{Code: tea.KeyTab})
		if !handled {
			t.Fatalf("Tab %d was not handled", i)
		}
		if m.overlays.panel.kind != panelregistry.Debug {
			t.Fatalf("Tab %d left the panel: kind = %v", i, m.overlays.panel.kind)
		}
	}
	if got := m.debugPanel().View(); got != start {
		t.Errorf("a full lap of Tabs landed on %v, want the view it started on (%v)", got, start)
	}
}

// TestLogsWireArgumentRendersThroughTheViewport — `/logs wire` reaches the
// screen, not only the panel: the frame the terminal draws is the Wire
// view's own frame, with the panel's rows neither clipped nor
// double-drawn.
//
// The panel object's state can be right while the viewport clips it wrong
// — that was the /context scroll bug — so the assertion reads the
// rendered content.
func TestLogsWireArgumentRendersThroughTheViewport(t *testing.T) {
	m := mkPanel(panelregistry.None, "", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120
	m.viewport.height = 30

	if !m.openSlashPanelArgs("debug", "wire") {
		t.Fatal("/logs wire was refused")
	}
	if got := m.debugPanel().View(); got != debugpanel.ViewWire {
		t.Fatalf("/logs wire opened the %v view, want Wire", got)
	}

	view := ansi.Strip(m.View().Content)
	// The view bar is the frame's first row; a frame clipped from the top
	// would lose it and the tab names with it.
	for _, want := range []string{"Logs", "Wire", "Trajectory", "Wire request", "systemInstruction", "tools"} {
		if !strings.Contains(view, want) {
			t.Errorf("rendered /logs wire is missing %q:\n%s", want, view)
		}
	}
	// The log file's own badge belongs to the other view; finding it here
	// would mean the frame drew two views at once.
	if strings.Contains(view, "Pigasus Session Logs") {
		t.Errorf("rendered /logs wire also drew the Logs view:\n%s", view)
	}
}

// TestLogsTrajArgumentRendersThroughTheViewport — the same for
// `/logs traj`: the turns, not the log file.
func TestLogsTrajArgumentRendersThroughTheViewport(t *testing.T) {
	m := mkPanel(panelregistry.None, "", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120
	m.viewport.height = 30

	if !m.openSlashPanelArgs("debug", "traj") {
		t.Fatal("/logs traj was refused")
	}
	if got := m.debugPanel().View(); got != debugpanel.ViewTrajectory {
		t.Fatalf("/logs traj opened the %v view, want Trajectory", got)
	}

	view := ansi.Strip(m.View().Content)
	for _, want := range []string{"Trajectory", "the store's own steps"} {
		if !strings.Contains(view, want) {
			t.Errorf("rendered /logs traj is missing %q:\n%s", want, view)
		}
	}
	if strings.Contains(view, "Pigasus Session Logs") {
		t.Errorf("rendered /logs traj also drew the Logs view:\n%s", view)
	}
}

// TestLogsFrameFillsTheStatusAreaGap — the panel is told the height the
// status area leaves, so the frame plus the status area is exactly the
// terminal. Off by one here and the frame's first row is clipped away.
func TestLogsFrameFillsTheStatusAreaGap(t *testing.T) {
	m := mkPanel(panelregistry.Debug, "/debug", 0)
	m.viewport.width = 100
	m.viewport.height = 24

	got := len(m.debugPanel().Lines())
	if want := m.debugPanelHeight(); got != want {
		t.Errorf("the /logs frame drew %d rows, want the %d the height seam reports", got, want)
	}
	if got > m.viewport.height {
		t.Errorf("the /logs frame drew %d rows into a %d-row terminal", got, m.viewport.height)
	}
	// The status area is drawn under the panel: the two must not overlap,
	// which is what the panel being told the whole height used to do.
	if rows := len(statusline.AreaRows(m.statusAreaState())); got+rows > m.viewport.height {
		t.Errorf("the frame (%d rows) plus the status area (%d rows) is %d, more than the %d-row terminal",
			got, rows, got+rows, m.viewport.height)
	}
}

// TestContextHydrateReadsTheStore — Enter on a cold step's row reads the
// step from the conversation's own database, off the update loop.
func TestContextHydrateReadsTheStore(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120
	m.viewport.height = 40

	// A tool step whose body the trajectory carries but the in-memory
	// view will treat as cold: the store is the only place it lives.
	m.setSessionMessages([]backend.Message{
		{Role: "user", Content: "read it"},
		{Role: "assistant", Content: "done"},
	})
	m.session.sup().RecordTrajectoryCalls([]convstore.Step{{
		Type:        convstore.StepToolCall,
		Status:      3,
		ToolName:    "read_file",
		ToolPrefix:  "Read",
		ToolSummary: "main.go",
		ToolArgs:    `{"path":"main.go"}`,
	}})

	cmd := m.contextHydrateCmd(2)
	if cmd == nil {
		t.Fatal("the hydrate command is nil")
	}
	msg, ok := cmd().(contextHydrateMsg)
	if !ok {
		t.Fatalf("the hydrate answered %T, want contextHydrateMsg", cmd())
	}
	if msg.index != 2 {
		t.Errorf("the answer is for step %d, want 2", msg.index)
	}
	// A conversation with no database answers not-found rather than an
	// invented step.
	if msg.err == nil && msg.step.Idx == 0 && msg.step.Type == 0 {
		t.Log("no store on this machine: the read answered not-found, which the panel shows as its note")
	}
}

// TestContextCopiesWithoutEscapes — the copy key hands the clipboard text
// a terminal pastes, not the sequences that drew the row.
func TestContextCopiesWithoutEscapes(t *testing.T) {
	m := mkPanel(panelregistry.Debug, "/debug", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120
	m.viewport.height = 40
	m.setSessionMessages([]backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "hi"},
	})
	m.openSlashPanelArgs("debug", "traj")
	m.panelLines()

	// Drive the copy through the panel and capture what it handed over.
	var copied string
	prev := clipboard.Writer
	clipboard.Writer = func(text string) error {
		copied = text
		return nil
	}
	defer func() { clipboard.Writer = prev }()

	if handled, cmd := m.handlePanelKey(tea.Key{Code: 'y', Text: "y"}); !handled {
		t.Fatal("y was not handled by the /logs surface")
	} else if cmd != nil {
		cmd()
	}
	if copied == "" {
		t.Fatal("y copied nothing")
	}
	if strings.Contains(copied, "\x1b") {
		t.Errorf("the copied text carries escapes: %q", copied)
	}
	if !strings.Contains(copied, "hello") {
		t.Errorf("the copied text is not the row under the cursor: %q", copied)
	}
}

// TestContextPanelsFactsAreMeasuredNotGuessed — the Wire view's total is
// the sum of its parts, and the parts are the prompt, the tools and the
// transcript. A view that guessed would not close.
func TestContextPanelsFactsAreMeasuredNotGuessed(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "AGENTS.md"), []byte("# rules\nbe brief\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m.session.cwd = dir
	m.setSessionMessages([]backend.Message{{Role: "user", Content: "hi"}})

	ws := m.contextWireState()
	var sum int
	for _, s := range ws.Sections {
		sum += s.Bytes
	}
	for _, tool := range ws.Tools {
		sum += tool.Bytes
	}
	for _, msg := range ws.Messages {
		sum += msg.Bytes
	}
	if sum <= 0 {
		t.Fatal("the Wire view measured nothing")
	}
	// The system instruction is the largest single member: a prompt
	// carrying no sections would mean the measurement missed the prompt.
	if got := len(buildPrompt(m)); got < sum/2 {
		t.Errorf("the prompt is %d bytes and the whole request measures %d; the sections missed it", got, sum)
	}
}

// TestContextTrajectoryTurnsAccurateCountAndGrouping verifies that
// turns are partitioned by user exchanges, reporting exact turn counts
// rather than inflating by the number of assistant replies.
func TestContextTrajectoryTurnsAccurateCountAndGrouping(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.setSessionMessages([]backend.Message{
		{Role: "user", Content: "turn 1 prompt"},
		{Role: "assistant", Content: "reply 1a"},
		{Role: "user", Content: "turn 2 prompt"},
		{Role: "assistant", Content: "reply 2a"},
	})
	m.session.sup().RecordTrajectoryCalls([]convstore.Step{
		{
			Type:        convstore.StepToolCall,
			Status:      3,
			ToolName:    "run_command",
			ToolPrefix:  "Ran",
			ToolSummary: "go build",
		},
	})

	ts := m.contextTrajectoryState()
	// Must be exactly 2 turns, NOT 4.
	if ts.Turns != 2 {
		t.Fatalf("ts.Turns = %d, want 2", ts.Turns)
	}
	if len(ts.TurnsList) != 2 {
		t.Fatalf("len(ts.TurnsList) = %d, want 2", len(ts.TurnsList))
	}

	// Turn 1
	turn1 := ts.TurnsList[0]
	if turn1.Index != 1 {
		t.Errorf("turn 1 index = %d, want 1", turn1.Index)
	}
	if turn1.UserPrompt != "turn 1 prompt" {
		t.Errorf("turn 1 prompt = %q, want 'turn 1 prompt'", turn1.UserPrompt)
	}
	if turn1.Tools != 1 {
		t.Errorf("turn 1 tools = %d, want 1", turn1.Tools)
	}

	// Turn 2
	turn2 := ts.TurnsList[1]
	if turn2.Index != 2 {
		t.Errorf("turn 2 index = %d, want 2", turn2.Index)
	}
	if turn2.UserPrompt != "turn 2 prompt" {
		t.Errorf("turn 2 prompt = %q, want 'turn 2 prompt'", turn2.UserPrompt)
	}
	if turn2.Tools != 0 {
		t.Errorf("turn 2 tools = %d, want 0", turn2.Tools)
	}
}

// TestContextTrajectoryThinkingExtracted verifies that when a model
// response carries thinking, it surfaces as a distinct TrajThought step.
func TestContextTrajectoryThinkingExtracted(t *testing.T) {
	m := mkPanel(panelregistry.Context, "/context", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.session.sup().SetSeededConversation([]convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "solve problem"},
		{
			Idx:              1,
			Type:             convstore.StepModelResponse,
			Status:           3,
			Text:             "solution",
			Thinking:         "Let me break this down step by step.",
			ThinkingDuration: 2500 * time.Millisecond,
		},
	}, 2)

	ts := m.contextTrajectoryState()
	if ts.Turns != 1 {
		t.Fatalf("ts.Turns = %d, want 1", ts.Turns)
	}

	var hasThought, hasReply bool
	for _, s := range ts.Steps {
		if s.Kind == debugpanel.TrajThought {
			hasThought = true
			if !strings.Contains(s.Summary, "2.5s") {
				t.Errorf("thought summary = %q, want duration '2.5s'", s.Summary)
			}
			if len(s.Detail) == 0 || !strings.Contains(s.Detail[0], "break this down") {
				t.Errorf("thought detail = %v, want reasoning text", s.Detail)
			}
		}
		if s.Kind == debugpanel.TrajReply {
			hasReply = true
		}
	}
	if !hasThought {
		t.Error("expected a TrajThought step to be emitted for response with Thinking")
	}
	if !hasReply {
		t.Error("expected a TrajReply step to be emitted")
	}
}
