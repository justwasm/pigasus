// panel_seams_context_views.go — the host's facts for the /logs panel's
// Wire and Trajectory views.
//
// # What lives here
//
// The panel (internal/ui/views/debugpanel) lays out the views and measures
// nothing. This file is the other half of that split: it reads the
// session's real facts — the prompt's sections, the tool declarations'
// wire bytes, the transcript's messages, the conversation's steps — and
// hands them over as plain values.
//
// # Why the numbers come from the existing single sources
//
// Every count here is a call into the code that already owns it:
//
//	agyprompt.Sections       the sections Build emits, byte-exact
//	cloudcode.WireToolsJSON  the tool surface as the request serialises it
//	backend.UserContentTokenCount  the reference's own /context accounting
//	convstore.BuildSteps     the one assembly of the conversation's steps
//
// A view that re-derived any of them would be a second answer to a
// question the host already answers, which is the class of bug that made
// a configured model's "(1017)" suffix disappear from the status row.
package tui

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/prompt/agyprompt"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
	"github.com/justwasm/pigasus/internal/infra/backend/cloudcode"
	"github.com/justwasm/pigasus/internal/ui/tuitools"
	"github.com/justwasm/pigasus/internal/ui/views/debugpanel"
)

// wirePreviewLen caps a message's folded preview. The folded line is an
// identifier, not the message: a 40 KB tool result belongs one fold
// away, and loading it to draw a one-line summary would make the panel
// slow exactly when the conversation is long.
const wirePreviewLen = 160

// contextWireState gathers the Wire view's facts: what the next turn puts
// on the wire, sized by the code that would put it there.
//
// The sizes are host-side: the system instruction is the prompt template
// rendered, the tools are the declarations as CloudCode serialises them,
// the messages are the transcript. The backend adds its own envelope
// (thinkingConfig, executionModel, labels) that this view cannot see, and
// the Note says so rather than implying byte-exactness.
func (m *RootModel) contextWireState() debugpanel.WireState {
	if m == nil {
		return debugpanel.WireState{}
	}
	engine := m.engineName()
	st := debugpanel.WireState{
		Model:  m.contextModel(),
		Engine: engine,
		Note:   "host-side sizes · the backend adds its own request envelope",
	}
	st.Sections = m.contextWireSections()
	st.Tools = m.contextWireTools()

	msgs := m.SessionMessages()
	for i, msg := range msgs {
		st.Messages = append(st.Messages, m.contextWireMessage(i+1, msg))
	}
	for _, msg := range msgs {
		st.Images += len(msg.Images)
	}
	return st
}

// contextWireSections sizes each section of the system instruction the
// way the request carries it.
//
// A custom agent's prompt is a different document (agyprompt.BuildAgent),
// with a different section set, so it is measured as one blob rather than
// mis-attributed to sections it does not have.
func (m *RootModel) contextWireSections() []debugpanel.WireSection {
	if m.session.agentBody != "" {
		full := agyprompt.BuildAgent(m.session.agentBody, m.promptOptions())
		return []debugpanel.WireSection{{
			Name:   "agent prompt",
			Bytes:  len(full),
			Tokens: len(full) / 3,
		}}
	}
	sections := agyprompt.Sections(m.promptOptions())
	out := make([]debugpanel.WireSection, 0, len(sections))
	for _, s := range sections {
		out = append(out, debugpanel.WireSection{
			Name:   s.Name,
			Bytes:  len(s.Text),
			Tokens: agyprompt.SectionTokenCount(s.Name, s.Body),
		})
	}
	return out
}

// contextWireTools sizes each tool declaration as the wire serialises it.
//
// The declaration is measured one at a time so the rows are attributable;
// the sum is not the group's wire size (the array's brackets and commas
// are the group's), which is why the panel labels the group with the sum
// of its members rather than claiming to be byte-exact either.
func (m *RootModel) contextWireTools() []debugpanel.WireTool {
	decls := m.wireToolDecls()
	out := make([]debugpanel.WireTool, 0, len(decls))
	for _, d := range decls {
		n := len(cloudcode.WireToolsJSON([]backend.ToolDecl{d}))
		out = append(out, debugpanel.WireTool{
			Name:   d.Name,
			Bytes:  n,
			Tokens: n / 3,
		})
	}
	return out
}

// contextWireMessage sizes one transcript message and names what it
// carries.
func (m *RootModel) contextWireMessage(index int, msg backend.Message) debugpanel.WireMessage {
	text := msg.WireText()
	kind := wireMessageKind(msg)
	return debugpanel.WireMessage{
		Index:   index,
		Role:    wireMessageRole(msg),
		Kind:    kind,
		Bytes:   len(text),
		Tokens:  backend.UserContentTokenCount(text),
		Preview: contextFirstLine(text, wirePreviewLen),
	}
}

// wireMessageRole is the role the request carries: a tool result travels
// in the user position, as a function response.
func wireMessageRole(msg backend.Message) string {
	switch msg.Role {
	case "assistant":
		return "model"
	case "tool":
		return "tool"
	case "system":
		return "system"
	default:
		return "user"
	}
}

// wireMessageKind names what a message carries, so a row says more than
// its size.
func wireMessageKind(msg backend.Message) string {
	switch msg.Role {
	case "tool":
		if msg.IsError {
			return "error"
		}
		return "response"
	case "assistant":
		if len(msg.ToolCalls) > 0 {
			return "tool_call"
		}
		return "reply"
	case "system":
		return "notice"
	}
	if strings.HasPrefix(strings.TrimSpace(msg.WireText()), "<USER_REQUEST>") {
		return "envelope"
	}
	return "text"
}

// contextTrajectoryState gathers the Trajectory view's facts from the
// trajectory's own assembly.
//
// convstore.BuildSteps is the single source: the compaction path, the
// rewind picker and the workflow tool all read it, and this view reads
// the same list. Nothing here re-derives a summary — the collapsed line
// is the store's own render_info text.
func (m *RootModel) contextTrajectoryState() debugpanel.TrajectoryState {
	if m == nil {
		return debugpanel.TrajectoryState{}
	}
	steps := convstore.BuildSteps(m.trajectoryFacts())
	spans := convstore.GroupTurns(steps)
	st := debugpanel.TrajectoryState{
		Turns: len(spans),
		Note:  "the store's own steps · the summarized ones are still here",
	}
	foldedThrough := int64(0)
	for _, s := range steps {
		if s.Type == convstore.StepCheckpoint && int64(s.IncludedEnd) > foldedThrough {
			foldedThrough = int64(s.IncludedEnd)
		}
	}
	for _, span := range spans {
		turn := debugpanel.TrajTurn{
			Index: span.Index,
		}
		allFolded := span.End > span.Start
		for _, s := range steps[span.Start:span.End] {
			if s.Type == convstore.StepModelResponse && s.Thinking != "" {
				thought := debugpanel.TrajStep{
					Index:   s.Idx,
					Kind:    debugpanel.TrajThought,
					Summary: formatThinkingSummary(s.Thinking, s.ThinkingDuration),
					Detail:  bodyLines(s.Thinking, 12),
				}
				if foldedThrough > 0 && s.Idx < foldedThrough {
					thought.Folded = true
				}
				turn.Steps = append(turn.Steps, thought)
				st.Steps = append(st.Steps, thought)
			}

			ts := m.contextTrajStep(s, foldedThrough)
			turn.Steps = append(turn.Steps, ts)
			st.Steps = append(st.Steps, ts)

			if ts.Kind == debugpanel.TrajUser && turn.UserPrompt == "" {
				turn.UserPrompt = ts.Summary
			}
			if ts.Kind == debugpanel.TrajTool {
				turn.Tools++
			}
			turn.Tokens += ts.Tokens
			if !ts.Folded {
				allFolded = false
			}

			switch ts.Kind {
			case debugpanel.TrajCheckpoint:
				st.Checkpoints++
			}
			if ts.Cold {
				st.ColdChunks++
			}
		}
		if turn.UserPrompt == "" && len(turn.Steps) > 0 {
			turn.UserPrompt = turn.Steps[0].Summary
		}
		turn.Folded = allFolded
		st.TurnsList = append(st.TurnsList, turn)
	}
	return st
}

func formatThinkingSummary(thinking string, dur time.Duration) string {
	if dur > 0 {
		return fmt.Sprintf("Thought for %s", dur.Round(100*time.Millisecond))
	}
	first := contextFirstLine(thinking, wirePreviewLen)
	if first != "" {
		return "Thought: " + first
	}
	return "Thought"
}

// contextTrajStep converts one stored step to the view's row.
func (m *RootModel) contextTrajStep(s convstore.Step, foldedThrough int64) debugpanel.TrajStep {
	ts := debugpanel.TrajStep{Index: s.Idx}
	switch s.Type {
	case convstore.StepUserInput:
		ts.Kind = debugpanel.TrajUser
		ts.Summary = contextFirstLine(s.Text, wirePreviewLen)
		ts.Detail = bodyLines(s.Text, 12)
	case convstore.StepModelResponse:
		ts.Kind = debugpanel.TrajReply
		ts.Summary = contextFirstLine(s.Text, wirePreviewLen)
		ts.Detail = bodyLines(s.Text, 12)
	case convstore.StepToolCall:
		ts.Kind = debugpanel.TrajTool
		ts.Summary = toolStepSummary(s)
		ts.Tokens = backend.ToolResultTokenCount(s.ToolResult)
		switch {
		case s.ToolResult != "" || len(s.RawPayload) > 0:
			ts.Detail = toolStepDetail(s)
		case m.overlays.panel.ctxHydrated[s.Idx].ToolResult != "":
			// The panel asked the store for this step and the answer
			// landed: the row carries the body the store kept.
			loaded := m.overlays.panel.ctxHydrated[s.Idx]
			ts.Tokens = backend.ToolResultTokenCount(loaded.ToolResult)
			ts.Detail = toolStepDetail(loaded)
		default:
			// The step's body is on disk and not in memory: the panel
			// offers the read with Enter rather than pretending the call
			// produced nothing.
			ts.Cold = true
			ts.ColdNote = "body not in memory"
		}
	case convstore.StepCheckpoint:
		ts.Kind = debugpanel.TrajCheckpoint
		ts.Summary = checkpointSummary(s)
		ts.Detail = checkpointDetail(s)
	case convstore.StepError:
		ts.Kind = debugpanel.TrajError
		ts.Summary = contextFirstLine(s.Text, wirePreviewLen)
		ts.Detail = bodyLines(s.Text, 12)
	default:
		if strings.TrimSpace(s.Text) == "" {
			ts.Kind = debugpanel.TrajNotice
			ts.Summary = fmt.Sprintf("step %d", s.Idx)
			return ts
		}
		ts.Kind = debugpanel.TrajNotice
		ts.Summary = contextFirstLine(s.Text, wirePreviewLen)
		ts.Detail = bodyLines(s.Text, 12)
	}
	// A step a checkpoint already folded into its summary is no longer in
	// the context window. The view marks it so the outline shows what the
	// summary stands in for, which is the whole reason the view exists.
	if foldedThrough > 0 && s.Type != convstore.StepCheckpoint && s.Idx < foldedThrough {
		ts.Folded = true
	}
	return ts
}

// toolStepSummary is the collapsed line: the verb and the noun phrase the
// reference's own transcript rendered.
//
// # Two vocabularies, two layers
//
// The store and the screen do not use the same words, and they are not
// supposed to. Sampled from the 800 reference conversations on this machine
// (4,515 tool steps), render_info's field 5 carries the *storage* verb:
//
//	read_file / view_file   "Analyzed"   (549 steps)
//	write_file              "Created"    (60)
//	edit_file               "Edited"     (260)
//	run_command             "Ran"        (2,134)
//
// while the transcript the reference draws says "Explored 1 file (agy-cont.sh)"
// for the same step (cycle 285's PTY capture, byte-identical to the
// reference). So field 5 is not what the screen shows: the screen takes the
// verb from its own renderer, and the store's prefix is only a fallback for
// a row whose tool the renderer does not know.
//
// # The bug this ordering fixes
//
// Joining the stored prefix to the noun phrase printed "Analyzed
// deliverable.md" for a file read — a line the reference never draws. Both
// halves of the line were individually correct; the *verb* was taken from
// the wrong layer.
//
// # The old rows
//
// Sessions written before the write path learned to keep the two halves
// apart stored the already-collapsed display line ("Ran (go build ./...)")
// in field 9, and joining *that* printed the verb twice. The guard keeps
// those rows readable.
func toolStepSummary(s convstore.Step) string {
	summary := strings.TrimSpace(s.ToolSummary)
	prefix := strings.TrimSpace(s.ToolPrefix)

	// What the transcript itself would draw for this call. Preferred over
	// the store's prefix because the two vocabularies differ: a file read
	// draws "Explored 1 file (x)" while field 5 records "Analyzed".
	line := transcriptSummary(s.ToolName, s.ToolArgs)

	// A stored line that already starts with a verb is the pre-split shape —
	// field 9 holds the whole display line — so it is handed back whole
	// rather than having a second verb stacked on it.
	if summary != "" && alreadyCarriesAVerb(summary, prefix, line) {
		return summary
	}
	if line != "" {
		return line
	}

	// The arguments cannot describe the call (they are absent, truncated, or
	// from a tool outside the transcript's vocabulary). The noun phrase the
	// store kept is still good, so rebuild the line around it with the
	// category's own words rather than falling back to field 5's storage verb
	// — "Analyzed deliverable.md" is a line the reference never draws.
	if summary != "" {
		if rebuilt := transcriptLineFor(s.ToolName, summary); rebuilt != "" {
			return rebuilt
		}
		if prefix != "" {
			return prefix + " " + summary
		}
		return summary
	}
	if prefix != "" {
		return prefix + " " + s.ToolName
	}
	return s.ToolName
}

// transcriptLineFor wraps a stored noun phrase in the words the transcript
// uses for that category of tool.
//
// It is the fallback for a step whose arguments can no longer describe it.
// The category decides the shape ("Explored 1 file (x)" against "Ran (x)"),
// and the noun phrase the store kept fills it in — so the line is the one the
// reference would have drawn even though the arguments are gone.
func transcriptLineFor(toolName, nounPhrase string) string {
	switch strings.ToLower(toolName) {
	case "view_file", "read_file", "read", "readfile", "view":
		return "Explored 1 file (" + nounPhrase + ")"
	case "search_web", "websearch", "web_search":
		return "Explored 1 search (" + nounPhrase + ")"
	case "write_to_file", "write_file", "create_file", "write", "writefile",
		"replace_file_content", "edit_file", "multi_replace_file_content",
		"sed_file", "edit", "editfile", "str_replace_editor", "patch":
		return "Edited 1 file (" + nounPhrase + ")"
	case "run_command", "bash", "exec", "execute_command", "execute", "terminal", "sh":
		return "Ran (" + nounPhrase + ")"
	}
	return ""
}

// alreadyCarriesAVerb reports whether a stored summary is the pre-split
// display line rather than the noun phrase alone.
//
// Two shapes have to be recognised, because the write path changed what it
// stored: the store's own verb ("Ran (go build ./...)") and the transcript's
// ("Explored 1 file (deliverable.md)"). A summary that is only a noun phrase
// — "deliverable.md" — carries neither and is safe to rebuild.
func alreadyCarriesAVerb(summary, storePrefix, transcriptLine string) bool {
	for _, verb := range []string{storePrefix, firstWord(transcriptLine)} {
		if verb == "" {
			continue
		}
		if strings.HasPrefix(summary, verb+" ") || summary == verb {
			return true
		}
	}
	return false
}

// firstWord returns a line's leading word, or "" when it has none.
func firstWord(line string) string {
	if i := strings.IndexByte(line, ' '); i > 0 {
		return line[:i]
	}
	return line
}

// transcriptSummary is the collapsed line the transcript draws for a tool,
// built by the package that owns that vocabulary.
//
// It is a call into tuitools rather than a second copy of its switch because
// the words are the reference's, and a copy would be a second answer to "what
// does this step say" — the class of bug that produced "Ran Ran". An empty
// answer means the tool is outside the vocabulary, which the caller treats as
// "fall back to what the store recorded".
func transcriptSummary(toolName, args string) string {
	if toolName == "" || strings.TrimSpace(args) == "" {
		return ""
	}
	return tuitools.ToolStepSummary(toolName, json.RawMessage(args))
}

// toolStepDetail is the expanded body: the arguments the model produced
// and the result the tool answered with.
func toolStepDetail(s convstore.Step) []string {
	var out []string
	if args := prettyJSON(s.ToolArgs); args != "" {
		out = append(out, "args:")
		out = append(out, bodyLines(args, 8)...)
	}
	if strings.TrimSpace(s.ToolResult) != "" {
		out = append(out, "result:")
		out = append(out, bodyLines(s.ToolResult, 16)...)
	}
	return out
}

// checkpointSummary is the checkpoint's collapsed line: the index it
// recorded and the strategy that produced it.
func checkpointSummary(s convstore.Step) string {
	label := "compaction"
	if s.Strategy != "" {
		label = strings.ToLower(s.Strategy)
	}
	return fmt.Sprintf("%s · steps %d–%d", label, s.IncludedStart, s.IncludedEnd-1)
}

// checkpointDetail is what the checkpoint folded away: its summary and
// the requests it stands in for.
func checkpointDetail(s convstore.Step) []string {
	var out []string
	if s.Summary != "" {
		out = append(out, "summary:")
		out = append(out, bodyLines(s.Summary, 10)...)
	}
	if s.UserIntent != "" {
		out = append(out, "intent: "+s.UserIntent)
	}
	for _, r := range s.UserRequests {
		out = append(out, "request: "+contextFirstLine(r, wirePreviewLen))
	}
	if s.TokensBefore > 0 || s.TokensAfter > 0 {
		out = append(out, fmt.Sprintf("tokens: %d → %d", s.TokensBefore, s.TokensAfter))
	}
	return out
}

// contextFirstLine is the message's opening line, clipped.
func contextFirstLine(text string, limit int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	line := strings.SplitN(text, "\n", 2)[0]
	line = strings.TrimSpace(line)
	return clipRunes(line, limit)
}

// bodyLines splits a body into at most limit lines, each clipped, and
// says how many it left out.
func bodyLines(body string, limit int) []string {
	body = strings.TrimRight(body, "\n")
	if strings.TrimSpace(body) == "" {
		return nil
	}
	lines := strings.Split(body, "\n")
	out := make([]string, 0, limit+1)
	for i, l := range lines {
		if i >= limit {
			out = append(out, fmt.Sprintf("… +%d lines", len(lines)-limit))
			break
		}
		out = append(out, clipRunes(l, 240))
	}
	return out
}

// clipRunes cuts a string to limit runes, marking the cut.
func clipRunes(s string, limit int) string {
	r := []rune(s)
	if len(r) <= limit {
		return s
	}
	return string(r[:limit]) + "…"
}

// prettyJSON reformats a raw JSON argument blob so the panel's body is
// readable. A blob that does not parse is passed through as it was: the
// panel shows what the model produced, not a blank.
func prettyJSON(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return raw
	}
	return string(b)
}

// engineName names the engine serving the turn: the bridge's own answer,
// so the Wire view labels the request with the engine that will send it
// rather than a guess.
func (m *RootModel) engineName() string {
	if m == nil {
		return "pigasus"
	}
	name, _ := agentturn.CapabilitiesOf(m.turnEngine())
	return name
}

// loadStoredStep reads one step out of the conversation's own database.
//
// It is the cold path: the step's body lives on disk and the in-memory
// trajectory carries only its collapsed line. A conversation that is not
// on disk, or a store that cannot be read, answers not-found — the
// panel keeps its "body not in memory" note rather than inventing one.
func loadStoredStep(convID string, index int64) (convstore.Step, bool, error) {
	path := convstore.FindConversation(convID)
	if path == "" {
		return convstore.Step{}, false, nil
	}
	c, err := convstore.LoadConversation(path)
	if err != nil {
		return convstore.Step{}, false, err
	}
	for _, s := range c.Steps {
		if s.Idx == index {
			return s, true, nil
		}
	}
	return convstore.Step{}, false, nil
}

// contextHydrateCmd answers the Trajectory view's Enter on a cold step.
//
// The read happens on a goroutine, not on the update loop: a step's body
// is a database read, and the panel is drawn every frame. The message
// carries the step so the host can fold it back into the view without a
// second lookup.
func (m *RootModel) contextHydrateCmd(index int64) tea.Cmd {
	convID := m.session.conversationID
	return func() tea.Msg {
		step, ok, err := loadStoredStep(convID, index)
		if err != nil || !ok {
			return contextHydrateMsg{index: index, err: err}
		}
		return contextHydrateMsg{index: index, step: step}
	}
}

// contextHydrateMsg is a cold step's body coming back from the store.
type contextHydrateMsg struct {
	index int64
	step  convstore.Step
	err   error
}

// clipboardCopyCmd writes text to the clipboard off the update loop.
//
// The copy is a command rather than a plain call for the same reason the
// hydrate is: the platform clipboard tool blocks, and the panel's draw
// must not.
func clipboardCopyCmd(text string) tea.Cmd {
	return func() tea.Msg {
		_ = clipboard.Write(text)
		return nil
	}
}
