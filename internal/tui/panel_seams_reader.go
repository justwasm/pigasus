// panel_seams_reader.go — the host's half of the /logs reader.
//
// # What this file answers
//
// The panel's list carries excerpts: a message's first line, a step's
// first twelve lines, a section's byte count. The reader exists to show
// the text behind an excerpt, and the excerpt is all the panel has — so
// the panel asks here, by the row's stable ID, and this file finds the
// body the ID names.
//
// # Why the IDs are parsed rather than looked up in a table
//
// The IDs are the panel's own vocabulary (see wireview.go and
// trajectoryview.go): "wire/system/<tag>", "wire/tools/<name>",
// "wire/msg/<n>", "traj/<idx>", "traj/turn/<n>". They are stable strings
// precisely so a fold survives a rebuild, and re-deriving the body from
// one is cheaper than keeping a second index that could disagree with the
// rows on screen.
//
// # Why a command
//
// A cold step's body is a database read. The panel is drawn every frame,
// so the read runs off the update goroutine and comes back as a
// debugpanel.ReaderMsg.
package tui

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/cloudcode"
	"github.com/justwasm/pigasus/internal/ui/views/debugpanel"
)

// logsReadBody answers one row's full body for the reader.
//
// An ID this host does not recognise is answered with an error the reader
// prints, rather than an empty body: a reader that opened onto nothing
// would be indistinguishable from a row that genuinely has no body.
func (m *RootModel) logsReadBody(id string) (debugpanel.Body, error) {
	if m == nil {
		return debugpanel.Body{}, fmt.Errorf("no session")
	}
	switch {
	case strings.HasPrefix(id, "wire/system/"):
		return m.readWireSection(strings.TrimPrefix(id, "wire/system/")), nil
	case strings.HasPrefix(id, "wire/tools/"):
		return m.readWireTool(strings.TrimPrefix(id, "wire/tools/")), nil
	case strings.HasPrefix(id, "wire/msg/"):
		return m.readWireMessage(strings.TrimPrefix(id, "wire/msg/")), nil
	case strings.HasPrefix(id, "traj/turn/"):
		return m.readTrajTurn(strings.TrimPrefix(id, "traj/turn/")), nil
	case strings.HasPrefix(id, "traj/"):
		return m.readTrajStep(strings.TrimPrefix(id, "traj/"))
	}
	// The three group headings are rows too. A user who opens a group's
	// body expects the group's contents, not an error.
	if name, ok := wireGroupNames[id]; ok {
		return m.readWireGroup(name), nil
	}
	return debugpanel.Body{}, fmt.Errorf("no body for %q", id)
}

// wireGroupNames maps the Wire view's three group rows to the name their
// heading prints, so the reader's body and the row cannot disagree about
// which group it belongs to.
var wireGroupNames = map[string]string{
	"wire/system":   "systemInstruction",
	"wire/tools":    "tools",
	"wire/contents": "contents",
}

// readWireGroup returns one group's whole contents as a table.
//
// The group's body is its members, one per line, in the view's own
// columns: opening the group is how a user reads twelve section names at
// once instead of twelve rows.
func (m *RootModel) readWireGroup(name string) debugpanel.Body {
	st := m.contextWireState()
	st.SortByBytes = false
	body := debugpanel.Body{
		Title: "wire · " + name,
		Note:  "the whole group, in request order",
	}
	for _, group := range debugpanel.WireRows(st) {
		if group.ID != "wire/"+groupIDKey(name) {
			continue
		}
		if len(group.Children) == 0 {
			body.Note = "this group is empty"
			return body
		}
		var b strings.Builder
		for _, child := range group.Children {
			b.WriteString(child.PlainText())
			b.WriteString("\n")
		}
		body.Parts = []debugpanel.ReaderPart{{Text: strings.TrimRight(b.String(), "\n")}}
		return body
	}
	body.Note = "this group is no longer in the request"
	return body
}

// groupIDKey maps a group's display name to its row ID's suffix.
func groupIDKey(name string) string {
	switch name {
	case "systemInstruction":
		return "system"
	case "tools":
		return "tools"
	}
	return "contents"
}

// readWireSection returns one section of the system instruction, verbatim.
//
// The bytes the Wire view sized are the bytes the request carries, so the
// reader shows the section's own text rather than the tags around it: a
// user reading <guidelines> wants what is inside it.
func (m *RootModel) readWireSection(name string) debugpanel.Body {
	title := "wire/system/" + name
	note := "the system instruction's section, as the prompt builds it"
	if m.session.agentBody != "" {
		full := agyprompt.BuildAgent(m.session.agentBody, m.promptOptions())
		if name != "agent prompt" {
			return debugpanel.Body{
				Title: title,
				Note:  "a custom agent's prompt is one document; this row sizes the whole of it",
				Parts: []debugpanel.ReaderPart{{Text: full}},
			}
		}
		return debugpanel.Body{Title: title, Note: note, Parts: []debugpanel.ReaderPart{{Text: full}}}
	}
	for _, s := range agyprompt.Sections(m.promptOptions()) {
		if s.Name != name {
			continue
		}
		return debugpanel.Body{
			Title: title,
			Note:  note,
			Parts: []debugpanel.ReaderPart{{Text: s.Body}},
		}
	}
	return debugpanel.Body{
		Title: title,
		Note:  "the prompt does not build a section with this name",
	}
}

// readWireTool returns one tool declaration as the wire serialises it.
//
// The declaration is shown as JSON rather than as a prose description,
// because the question the Wire view provokes is a byte question: what
// exactly does this tool cost, and a pretty-printed declaration answers
// it in a way a summary cannot.
func (m *RootModel) readWireTool(name string) debugpanel.Body {
	title := "wire/tools/" + name
	for _, d := range m.wireToolDecls() {
		if d.Name != name {
			continue
		}
		raw := cloudcode.WireToolsJSON([]backend.ToolDecl{d})
		return debugpanel.Body{
			Title: title,
			Note:  "the declaration as CloudCode serialises it",
			Parts: []debugpanel.ReaderPart{
				{Label: "description", Text: d.Description},
				{Label: "schema", Text: string(d.Parameters)},
				{Label: "wire", Text: string(raw)},
			},
		}
	}
	return debugpanel.Body{Title: title, Note: "this declaration is no longer advertised"}
}

// readWireMessage returns one transcript message's full wire text.
//
// Tool calls are a second part: they ride next to the content on the
// wire, and a message that carries one is not fully described by its
// text alone.
func (m *RootModel) readWireMessage(indexText string) debugpanel.Body {
	var index int
	if _, err := fmt.Sscanf(indexText, "%d", &index); err != nil {
		return debugpanel.Body{Title: "wire/msg/" + indexText, Note: "unreadable message index"}
	}
	msgs := m.SessionMessages()
	if index < 1 || index > len(msgs) {
		return debugpanel.Body{
			Title: fmt.Sprintf("wire/msg/%d", index),
			Note:  fmt.Sprintf("the conversation has %d messages", len(msgs)),
		}
	}
	msg := msgs[index-1]
	body := debugpanel.Body{
		Title: fmt.Sprintf("wire/msg/%d · %s · %s", index, wireMessageRole(msg), wireMessageKind(msg)),
		Note:  "the message's wire text, as the next request carries it",
		Parts: []debugpanel.ReaderPart{{Text: msg.WireText()}},
	}
	if len(msg.ToolCalls) > 0 {
		for i, call := range msg.ToolCalls {
			body.Parts = append(body.Parts, debugpanel.ReaderPart{
				Label: fmt.Sprintf("tool call %d · %s", i+1, call.Name),
				Text:  string(call.Arguments),
			})
		}
	}
	if len(msg.Images) > 0 {
		body.Note += fmt.Sprintf("  ·  %d image(s) ride outside the text", len(msg.Images))
	}
	return body
}

// readTrajTurn returns one turn's body: every step in it, in order.
//
// A turn is not one document, it is a run of them, so the reader lists
// the steps rather than trying to reformat the set as a single value.
// This is what makes "expand the turn" answerable without opening seven
// steps one at a time.
func (m *RootModel) readTrajTurn(indexText string) debugpanel.Body {
	var index int
	if _, err := fmt.Sscanf(indexText, "%d", &index); err != nil {
		return debugpanel.Body{Title: "traj/turn/" + indexText, Note: "unreadable turn index"}
	}
	title := fmt.Sprintf("traj/turn/%d", index)
	steps := convstore.BuildSteps(m.trajectoryFacts())
	for _, span := range convstore.GroupTurns(steps) {
		if span.Index != index {
			continue
		}
		body := debugpanel.Body{
			Title: title,
			Note:  debugpanel.CountLabel(span.End-span.Start, "step") + " in this turn",
		}
		for _, s := range steps[span.Start:span.End] {
			for _, part := range trajStepBody(s, "").Parts {
				label := part.Label
				if label == "" {
					label = trajStepKindName(s.Type)
				}
				body.Parts = append(body.Parts, debugpanel.ReaderPart{
					Label: fmt.Sprintf("#%d %s", s.Idx, label),
					Text:  part.Text,
				})
			}
		}
		return body
	}
	return debugpanel.Body{Title: title, Note: "this turn is no longer in the conversation"}
}

// readTrajStep returns one step's full body.
//
// A step in memory carries its own text; a step whose body was folded out
// of memory is read from the store. The store read is why this returns an
// error: a conversation that is not on disk answers not-found, and the
// reader prints that rather than an empty body that would look like a
// step which produced nothing.
func (m *RootModel) readTrajStep(indexText string) (debugpanel.Body, error) {
	var index int64
	if _, err := fmt.Sscanf(indexText, "%d", &index); err != nil {
		return debugpanel.Body{}, fmt.Errorf("unreadable step index %q", indexText)
	}
	title := fmt.Sprintf("traj/%d", index)
	for _, s := range convstore.BuildSteps(m.trajectoryFacts()) {
		if s.Idx != index {
			continue
		}
		// A tool step with no body in memory has one on disk. Reading it
		// is the same store read a cold row's Enter performs, done here so
		// that `v` on a cold row opens the body rather than a note about
		// it.
		if s.Type == convstore.StepToolCall && s.ToolResult == "" && len(s.RawPayload) > 0 {
			stored, ok, err := loadStoredStep(m.session.conversationID, index)
			if err != nil {
				return debugpanel.Body{}, fmt.Errorf("reading step %d: %w", index, err)
			}
			if ok {
				return trajStepBody(stored, title), nil
			}
		}
		return trajStepBody(s, title), nil
	}
	return debugpanel.Body{}, fmt.Errorf("no step %d in this conversation", index)
}

// trajStepBody is one stored step as the reader prints it.
//
// The parts are the step's own fields, one per document: the reasoning,
// the arguments, the result, the summary. Splitting them is what makes
// the format key useful, because the arguments parse as JSON and the
// result usually does not.
func trajStepBody(s convstore.Step, title string) debugpanel.Body {
	body := debugpanel.Body{Title: title, Note: trajStepNote(s)}
	switch s.Type {
	case convstore.StepUserInput:
		body.Parts = []debugpanel.ReaderPart{{Text: s.Text}}
	case convstore.StepModelResponse:
		if s.Thinking != "" {
			body.Parts = append(body.Parts, debugpanel.ReaderPart{Label: "thinking", Text: s.Thinking})
		}
		body.Parts = append(body.Parts, debugpanel.ReaderPart{Label: "reply", Text: s.Text})
	case convstore.StepToolCall:
		body.Note = fmt.Sprintf("tool %s  ·  %s", s.ToolName, trajStepNote(s))
		if s.ToolArgs != "" {
			body.Parts = append(body.Parts, debugpanel.ReaderPart{Label: "args", Text: s.ToolArgs})
		}
		if s.ToolResult != "" {
			body.Parts = append(body.Parts, debugpanel.ReaderPart{Label: "result", Text: s.ToolResult})
		}
	case convstore.StepCheckpoint:
		if s.Summary != "" {
			body.Parts = append(body.Parts, debugpanel.ReaderPart{Label: "summary", Text: s.Summary})
		}
		if s.UserIntent != "" {
			body.Parts = append(body.Parts, debugpanel.ReaderPart{Label: "intent", Text: s.UserIntent})
		}
		for i, r := range s.UserRequests {
			body.Parts = append(body.Parts, debugpanel.ReaderPart{
				Label: fmt.Sprintf("request %d", i+1),
				Text:  r,
			})
		}
	default:
		body.Parts = []debugpanel.ReaderPart{{Text: s.Text}}
	}
	return body
}

// trajStepNote is the step's one-line provenance.
func trajStepNote(s convstore.Step) string {
	parts := []string{trajStepKindName(s.Type)}
	if s.Status != 0 {
		parts = append(parts, fmt.Sprintf("status %d", s.Status))
	}
	return strings.Join(parts, "  ·  ")
}

// trajStepKindName names a stored step's type for the reader's note.
func trajStepKindName(t int64) string {
	switch t {
	case convstore.StepUserInput:
		return "user input"
	case convstore.StepModelResponse:
		return "model response"
	case convstore.StepToolCall:
		return "tool call"
	case convstore.StepCheckpoint:
		return "checkpoint"
	case convstore.StepError:
		return "error"
	default:
		return fmt.Sprintf("step type %d", t)
	}
}

// debugReaderCmd answers the reader's request off the update loop.
func (m *RootModel) debugReaderCmd(id string) tea.Cmd {
	return func() tea.Msg {
		body, err := m.logsReadBody(id)
		return debugpanel.ReaderMsg{ID: id, Body: body, Err: err}
	}
}

// updateLogsReaderMsg opens the body the reader asked for.
//
// The panel decides whether the answer is still wanted: it recorded which
// row it asked about, and a body arriving after the user moved on is
// dropped rather than replacing what they are now reading.
func (m *RootModel) updateLogsReaderMsg(msg debugpanel.ReaderMsg) (tea.Model, tea.Cmd, bool) {
	if p := m.overlays.panel.debugp; p != nil {
		if p.ApplyReader(msg) {
			m.invalidateViewCache()
		}
	}
	return m, nil, true
}
