// panel_reader_test.go — the host's half of the /logs reader.
//
// The panel's tests pin what it does with a body; these pin that the host
// can produce one at all. The failure this guards against is a reader
// that opens onto "no body for wire/msg/2": the panel would look correct
// and every row would be unreadable.
package tui

import (
	"strconv"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/views/debugpanel"
)

// readerModel is a session with a conversation and a wire.
func readerModel(t *testing.T) *RootModel {
	t.Helper()
	m := mkPanel(panelregistry.Debug, "/debug", 0)
	m.session.model = "Gemini 3.8 Flash (High)"
	m.viewport.width = 120
	m.viewport.height = 40
	m.setSessionMessages([]backend.Message{
		{Role: "user", Content: "read the file please"},
		{Role: "assistant", Content: "I will", ToolCalls: []backend.ToolCall{
			{Name: "read_file", Arguments: []byte(`{"path":"main.go"}`)},
		}},
		{Role: "tool", Content: "package main\n\nfunc main() {}\n"},
		{Role: "assistant", Content: "done"},
	})
	m.session.sup().RecordTrajectoryCalls([]convstore.Step{
		{Idx: 1, Type: convstore.StepUserInput, Text: "read the file please"},
		{Idx: 2, Type: convstore.StepToolCall, Status: 3, ToolName: "read_file",
			ToolPrefix: "Read", ToolSummary: "main.go",
			ToolArgs: `{"path":"main.go"}`, ToolResult: "package main\n\nfunc main() {}\n"},
		{Idx: 3, Type: convstore.StepModelResponse, Text: "done"},
	})
	return m
}

// TestReaderResolvesEveryWireRow — every row the Wire view draws has a
// body the host can answer for.
//
// The rows are walked from the panel's own row set, so a row added
// without a reader case fails here rather than opening onto an error.
func TestReaderResolvesEveryWireRow(t *testing.T) {
	m := readerModel(t)
	for _, row := range debugpanel.WireRows(m.contextWireState()) {
		ids := []string{row.ID}
		for _, c := range row.Children {
			ids = append(ids, c.ID)
		}
		for _, id := range ids {
			body, err := m.logsReadBody(id)
			if err != nil {
				t.Errorf("the host cannot answer for %q: %v", id, err)
				continue
			}
			if len(body.Parts) == 0 {
				t.Errorf("%q resolved to an empty body", id)
			}
			if body.Title == "" {
				t.Errorf("%q resolved to a body with no title", id)
			}
		}
	}
}

// TestReaderResolvesEveryTrajectoryRow — the same for the steps.
func TestReaderResolvesEveryTrajectoryRow(t *testing.T) {
	m := readerModel(t)
	rows := debugpanel.TrajectoryRows(m.contextTrajectoryState())
	if len(rows) == 0 {
		t.Fatal("the trajectory is empty")
	}
	for _, row := range rows {
		ids := []string{row.ID}
		for _, c := range row.Children {
			ids = append(ids, c.ID)
		}
		for _, id := range ids {
			body, err := m.logsReadBody(id)
			if err != nil {
				t.Errorf("the host cannot answer for %q: %v", id, err)
				continue
			}
			if len(body.Parts) == 0 {
				t.Errorf("%q resolved to an empty body", id)
			}
		}
	}
}

// TestReaderShowsTheWholeMessageNotThePreview — the body is the message,
// not the clipped first line the row carries.
//
// This is the difference the reader exists for: the list's preview is 160
// cells of one line, and a body that came back the same length would mean
// the reader was reading the row rather than the conversation.
func TestReaderShowsTheWholeMessageNotThePreview(t *testing.T) {
	m := readerModel(t)
	long := strings.Repeat("a long line of tool output\n", 200)
	m.setSessionMessages([]backend.Message{
		{Role: "user", Content: "go"},
		{Role: "tool", Content: long},
	})

	st := m.contextWireState()
	var id string
	var preview int
	for _, msg := range st.Messages {
		if msg.Role == "tool" {
			id = "wire/msg/" + strconv.Itoa(msg.Index)
			preview = len(msg.Preview)
			break
		}
	}
	if id == "" {
		t.Fatal("no tool message in the wire")
	}

	body, err := m.logsReadBody(id)
	if err != nil {
		t.Fatalf("%s: %v", id, err)
	}
	if len(body.Parts) == 0 {
		t.Fatal("the message resolved to no parts")
	}
	got := body.Parts[0].Text
	if len(got) <= preview {
		t.Errorf("the body is %d bytes, no longer than the %d-byte preview: the reader is reading the row, not the message",
			len(got), preview)
	}
	if !strings.Contains(got, "a long line of tool output") {
		t.Errorf("the body lost the message's text: %q", clipForTest(got, 120))
	}
}

// TestReaderToolArgumentsKeepTheModelOrder — a tool call's arguments are
// shown in the order the model wrote them.
//
// The panel's format keys preserve order (see the panel's own test), and
// this pins that the host hands the raw text over rather than a
// re-marshalled map that would have lost it before the panel saw it.
func TestReaderToolArgumentsKeepTheModelOrder(t *testing.T) {
	m := readerModel(t)
	// The fixture records its tool step at index 2, with arguments whose
	// key order is deliberately not alphabetical.
	const args = `{"path":"main.go"}`
	steps := convstore.BuildSteps(m.trajectoryFacts())
	var toolIdx int64 = -1
	for _, s := range steps {
		if s.Type == convstore.StepToolCall {
			toolIdx = s.Idx
			break
		}
	}
	if toolIdx < 0 {
		t.Fatal("the fixture has no tool step")
	}

	body, err := m.logsReadBody("traj/" + strconv.FormatInt(toolIdx, 10))
	if err != nil {
		t.Fatalf("traj/%d: %v", toolIdx, err)
	}
	var found bool
	for _, part := range body.Parts {
		if part.Label != "args" {
			continue
		}
		found = true
		if part.Text != args {
			t.Errorf("the arguments were rewritten:\n got %q\nwant %q", part.Text, args)
		}
	}
	if !found {
		t.Fatalf("the tool step has no args part: %+v", body.Parts)
	}
}

// TestReaderSectionIsTheSectionsOwnText — a system-instruction row opens
// onto the section's body, not onto its tags or its size.
func TestReaderSectionIsTheSectionsOwnText(t *testing.T) {
	m := readerModel(t)
	st := m.contextWireState()
	if len(st.Sections) == 0 {
		t.Skip("the prompt builds no sections in this environment")
	}
	section := st.Sections[0]
	body, err := m.logsReadBody("wire/system/" + section.Name)
	if err != nil {
		t.Fatalf("wire/system/%s: %v", section.Name, err)
	}
	if len(body.Parts) == 0 {
		t.Fatalf("wire/system/%s resolved to no parts", section.Name)
	}
	if got := len(body.Parts[0].Text); got == 0 {
		t.Errorf("wire/system/%s is empty", section.Name)
	}
	// The reader shows what the section cost, in the same units the row
	// does: a body far shorter than the row's byte count would mean the
	// reader opened onto a different section.
	if got := len(body.Parts[0].Text); got < section.Bytes/2 {
		t.Errorf("wire/system/%s: the body is %d bytes but the row sizes %d",
			section.Name, got, section.Bytes)
	}
}

// TestReaderToolDeclarationIsTheWireJSON — a tool row opens onto the
// declaration as the request serialises it.
func TestReaderToolDeclarationIsTheWireJSON(t *testing.T) {
	m := readerModel(t)
	decls := m.wireToolDecls()
	if len(decls) == 0 {
		t.Skip("no tools are advertised in this environment")
	}
	name := decls[0].Name
	body, err := m.logsReadBody("wire/tools/" + name)
	if err != nil {
		t.Fatalf("wire/tools/%s: %v", name, err)
	}
	var sawWire bool
	for _, part := range body.Parts {
		if part.Label == "wire" {
			sawWire = true
			if !strings.Contains(part.Text, name) {
				t.Errorf("the wire part does not name the tool: %q", clipForTest(part.Text, 120))
			}
		}
	}
	if !sawWire {
		t.Errorf("the tool body has no wire part: %+v", body.Parts)
	}
}

// TestReaderUnknownIDIsAnError — an ID the host does not know says so,
// rather than opening an empty reader that looks like an empty row.
func TestReaderUnknownIDIsAnError(t *testing.T) {
	m := readerModel(t)
	for _, id := range []string{"wire/nonsense", "traj/9999", "not-a-row", ""} {
		body, err := m.logsReadBody(id)
		if err == nil && len(body.Parts) > 0 {
			t.Errorf("%q resolved to a body: %+v", id, body)
		}
	}
}

// TestReaderAnswerReachesThePanel — the host routes a read's answer into
// the open panel, so the reader a user sees is the body the host found.
func TestReaderAnswerReachesThePanel(t *testing.T) {
	m := readerModel(t)
	if !m.openSlashPanelArgs("debug", "traj") {
		t.Fatal("/logs traj was refused")
	}
	m.View() // draw once: the panel's rows and cursor exist

	p := m.debugPanel()
	handled, cmd := p.HandleKey(tea.Key{Code: 'v', Text: "v"})
	if !handled {
		t.Fatal("v was not handled")
	}
	if cmd == nil {
		t.Fatal("v asked the host for nothing")
	}
	if p.ReaderOpen() {
		t.Fatal("the reader opened before the answer arrived")
	}

	msg, ok := cmd().(debugpanel.ReaderMsg)
	if !ok {
		t.Fatalf("the read answered %T", cmd())
	}
	if msg.Err != nil {
		t.Fatalf("the read failed: %v", msg.Err)
	}
	_, _ = m.Update(msg)

	if !p.ReaderOpen() {
		t.Fatal("the host's answer did not reach the panel")
	}
	joined := strings.Join(plainLines(m), "\n")
	if !strings.Contains(joined, msg.Body.Title) {
		t.Errorf("the frame does not show the body's title %q:\n%s", msg.Body.Title, joined)
	}
}

// clipForTest shortens a body for a failure message, so a test that fails
// on a megabyte of output prints a line rather than a screen.
func clipForTest(s string, n int) string {
	r := []rune(strings.ReplaceAll(s, "\n", " "))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}
