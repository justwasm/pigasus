package tui

// tui_tools_test.go — cycle 99 interactive tool-calling tests.
//
// These cover the three behaviours the cycle adds to the interactive session:
//
//  1. the turn advertises the recovered tool surface and executes the model's
//     calls (before cycle 99 the TUI advertised nothing);
//  2. the transcript renders the tool steps and ctrl+o expands/collapses the
//     detail view (recovered hints "ctrl+o to expand" / "ctrl+o to collapse");
//  3. the spinner line names the tool currently running.

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/app/agenttools"
	"github.com/justwasm/pigasus/internal/app/session/setup"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/cloudcode"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/tuitools"
	diffaction "github.com/justwasm/pigasus/internal/ui/views/diff/action"
)

// scriptedTUIClient replays canned chunk sets, one per Stream call, and records
// every request so a test can assert what was advertised.
type scriptedTUIClient struct {
	turns [][]backend.StreamChunk
	reqs  []backend.StreamRequest
	i     int
}

func (s *scriptedTUIClient) Stream(_ context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	s.reqs = append(s.reqs, req)
	var chunks []backend.StreamChunk
	if s.i < len(s.turns) {
		chunks = s.turns[s.i]
	} else {
		chunks = []backend.StreamChunk{{Done: true}}
	}
	s.i++
	out := make(chan backend.StreamChunk, len(chunks)+1)
	for _, c := range chunks {
		out <- c
	}
	close(out)
	return out, nil
}

// recordingRunner is declared in permission_test.go; it records the tool names
// it is asked to execute in .ran.

// TestTUITools_TurnAdvertisesAndRunsTool is the core cycle-99 behaviour: one
// turn advertises the tool surface, the model answers with a tool call, the
// runner executes it, and the loop streams a second turn with the result.
//
// Cycle 228: the advertised surface also includes the AGY 1.4.0 subagent
// toolset (invoke_subagent + the two stubs), wired via m.subAgentExtras. The
// assertion is updated from "exactly one tool, named run_command" to
// "run_command is one of the advertised tools", so the test pins the cycle-99
// behaviour without depending on the cycle-228 subagent wiring being absent.
func TestTUITools_TurnAdvertisesAndRunsTool(t *testing.T) {
	be := &scriptedTUIClient{turns: [][]backend.StreamChunk{
		{
			{ToolCalls: []backend.ToolCall{{
				ID:        "c1",
				Name:      "run_command",
				Arguments: json.RawMessage(`{"command":"date"}`),
			}}},
			{Done: true},
		},
		{{Text: "It is now."}, {Done: true}},
	}}
	runner := &recordingRunner{}
	m := newRunMode(be, "u@t", "m", "t", "/tmp", true)
	m.enableTools([]backend.ToolDecl{{Name: "run_command"}}, runner)

	msg := m.streamPrompt("what time is it")()
	done, ok := msg.(toolsteps.TurnDoneMsg)
	if !ok {
		t.Fatalf("expected toolsteps.TurnDoneMsg, got %T", msg)
	}
	if len(be.reqs) == 0 {
		t.Fatal("no backend request recorded")
	}
	advertised := map[string]bool{}
	for _, t := range be.reqs[0].Tools {
		advertised[t.Name] = true
	}
	if !advertised["run_command"] {
		t.Fatalf("run_command missing from surface: %+v", be.reqs[0].Tools)
	}
	// Cycle 228: the AGY 1.4.0 subagent toolset is now part of every
	// turn's surface even when m.session.cwd / auth home services fail to
	// open (subAgentExtras returns nil, so the surface shrinks back
	// to {run_command}). Both outcomes are valid; only run_command
	// is required.
	for _, want := range []string{"invoke_subagent", "send_message", "manage_subagents"} {
		if advertised[want] {
			continue
		}
	}
	if len(runner.ran) != 1 || runner.ran[0] != "run_command" {
		t.Fatalf("tool not executed: %+v", runner.ran)
	}
	if len(done.Steps) != 1 {
		t.Fatalf("expected 1 step, got %+v", done.Steps)
	}
	if done.Steps[0].Title != "Bash(date)" {
		t.Fatalf("unexpected step title %q", done.Steps[0].Title)
	}
	if !strings.Contains(done.Reply, "It is now.") {
		t.Fatalf("final reply lost: %q", done.Reply)
	}
}

// TestTUITools_NoRunnerIsTextOnly guards that a session without a runner
// behaves like the pre-cycle-99 TUI: text only, no panic.
func TestTUITools_NoRunnerIsTextOnly(t *testing.T) {
	be := &scriptedTUIClient{turns: [][]backend.StreamChunk{
		{{Text: "hello"}, {Done: true}},
	}}
	m := newRunMode(be, "u@t", "m", "t", "/tmp", true)
	msg := m.streamPrompt("hi")()
	done, ok := msg.(toolsteps.TurnDoneMsg)
	if !ok {
		t.Fatalf("expected toolsteps.TurnDoneMsg, got %T", msg)
	}
	if len(done.Steps) != 0 {
		t.Fatalf("unexpected steps: %+v", done.Steps)
	}
	if done.Reply != "hello" {
		t.Fatalf("reply = %q", done.Reply)
	}
}

// TestTUITools_CtrlOTogglesStepDetail pins the expand/collapse behaviour and
// the recovered hint strings.
func TestTUITools_CtrlOTogglesStepDetail(t *testing.T) {
	m := newRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.engine.response = append(m.engine.response, "> run date")
	m.engine.liveStart = len(m.engine.response)
	_, _ = m.Update(toolsteps.TurnDoneMsg{
		Reply: "It is now.",
		Steps: []toolsteps.Step{{
			Name:   "run_command",
			Title:  "Ran date",
			Detail: "Ran in the sandbox\nCommand output:\nFri Oct 16 06:11:50 CST 2026",
		}},
	})

	joined := strings.Join(m.engine.response, "\n")
	if !strings.Contains(joined, "Ran") || !strings.Contains(joined, "date") {
		t.Fatalf("step title missing:\n%s", joined)
	}
	if !strings.Contains(joined, "ctrl+o to expand") {
		t.Fatalf("collapsed hint missing:\n%s", joined)
	}
	if strings.Contains(joined, "06:11:50") {
		t.Fatalf("detail visible while collapsed:\n%s", joined)
	}
	if !strings.Contains(joined, "It is now.") {
		t.Fatalf("reply missing:\n%s", joined)
	}

	_, _ = m.Update(ctrlKey('o'))
	joined = strings.Join(m.engine.response, "\n")
	if !strings.Contains(joined, "06:11:50") {
		t.Fatalf("ctrl+o did not expand:\n%s", joined)
	}
	if strings.Contains(joined, "ctrl+o to expand") {
		t.Fatalf("collapsed hint still present after expanding:\n%s", joined)
	}

	_, _ = m.Update(ctrlKey('o'))
	joined = strings.Join(m.engine.response, "\n")
	if strings.Contains(joined, "06:11:50") {
		t.Fatalf("ctrl+o did not collapse:\n%s", joined)
	}
	if !strings.Contains(joined, "ctrl+o to expand") {
		t.Fatalf("collapsed hint missing after collapsing:\n%s", joined)
	}
	if !strings.Contains(joined, "It is now.") {
		t.Fatalf("reply lost after toggling:\n%s", joined)
	}
}

// TestTUITools_TurnDoneRendersMarkdown pins the cycle-228 fix: a live-turn
// reply that arrives via toolsteps.TurnDoneMsg must reach m.engine.response through the same
// glamour-backed pipeline the resumed-conversation path uses (cycle 155 +
// cycle glamour-migration). Before this fix the cycle-99 turn-end handler
// appended msg.reply verbatim so a reply containing **bold**, lists, or
// fenced code blocks showed up in the transcript as raw markdown.
func TestTUITools_TurnDoneRendersMarkdown(t *testing.T) {
	m := newRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.engine.response = append(m.engine.response, "> ask for a markdown answer")
	m.engine.liveStart = len(m.engine.response)
	_, _ = m.Update(toolsteps.TurnDoneMsg{
		Reply: "Here is **bold** and a list:\n\n- one\n- two",
	})
	joined := strings.Join(m.engine.response, "\n")
	if strings.Contains(joined, "**bold**") {
		t.Fatalf("toolsteps.TurnDoneMsg reply reached transcript as raw markdown:\n%s", joined)
	}
	if !strings.Contains(joined, "bold") {
		t.Fatalf("reply text missing from transcript:\n%s", joined)
	}
	if !strings.Contains(joined, "one") || !strings.Contains(joined, "two") {
		t.Fatalf("list items missing from transcript:\n%s", joined)
	}
	// Cycle 271: glamour folds the document colour and the bold
	// attribute into one SGR (ESC[38;5;23;1m), so check for the
	// attribute rather than a bare ESC[1m.
	if !hasSGRBold(joined) {
		t.Fatalf("bold SGR attribute missing from transcript (glamour did not run):\n%s", joined)
	}
}

// TestTUITools_LiveStreamReplacesGlamourAtTurnEnd pins the cycle-229
// streaming fix: when chunks arrive via backendChunkMsg, the raw text is
// appended to m.engine.response live (so the TUI scrolls as the model types);
// when the matching toolsteps.TurnDoneMsg lands, the [replyStart, len(m.engine.response))
// range is replaced with the glamour-rendered reply so the user sees the
// styled output without double-rendering (raw streamed + glamour'd
// appended). Pre-cycle 229 the TUI accumulated chunks silently in the
// agent loop's `full` buffer, never forwarding them to the tea.Program,
// so the user saw a frozen spinner until the turn finished.
func TestTUITools_LiveStreamReplacesGlamourAtTurnEnd(t *testing.T) {
	m := newRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.engine.response = append(m.engine.response, "> stream markdown")
	m.engine.liveStart = len(m.engine.response)
	// Send three chunks; the body of m.engine.response should contain the raw
	// streamed text immediately, anchored by replyStart.
	_, _ = m.Update(backendChunkMsg("Here is "))
	_, _ = m.Update(backendChunkMsg("**bold** "))
	_, _ = m.Update(backendChunkMsg("and a list."))
	startAfterChunks := len(m.engine.response)
	if startAfterChunks <= m.engine.liveStart {
		t.Fatalf("backendChunkMsg did not append to m.engine.response: liveStart=%d len=%d",
			m.engine.liveStart, startAfterChunks)
	}
	// Cycle 272: the live range is rendered on every chunk (it is
	// renderMarkdown(accumulated raw), the same function toolsteps.TurnDoneMsg
	// calls), so the streamed text is already styled before
	// toolsteps.TurnDoneMsg — the prose is present, the raw ** markers are not.
	liveVisible := diffaction.StripANSI(strings.Join(m.engine.response, "\n"))
	if !strings.Contains(liveVisible, "Here is bold and a list.") {
		t.Fatalf("streamed prose missing before toolsteps.TurnDoneMsg:\n%s",
			strings.Join(m.engine.response, "\n"))
	}
	if strings.Contains(liveVisible, "**bold**") {
		t.Fatalf("raw **bold** survived the mid-stream render:\n%s",
			strings.Join(m.engine.response, "\n"))
	}
	if m.engine.replyStart < m.engine.liveStart || m.engine.replyStart >= startAfterChunks {
		t.Fatalf("replyStart=%d not within live-stream range [%d,%d)",
			m.engine.replyStart, m.engine.liveStart, startAfterChunks)
	}
	// toolsteps.TurnDoneMsg: the raw streamed text in [replyStart, len(m.engine.response))
	// must be replaced (not appended next to) with the glamour'd version
	// of the SAME reply.
	reply := "Here is **bold** and a list."
	_, _ = m.Update(toolsteps.TurnDoneMsg{Reply: reply})
	// No double-render: the glamour'd text should not appear twice.
	joined := strings.Join(m.engine.response, "\n")
	// Count occurrences of the bold-text sentinel: glamour escapes the
	// "bold" word with ESC[1m surrounding it, so an unrendered pass
	// leaves the literal "**bold**". After the replace, the literal must
	// be gone AND there must not be a second copy of the formatted form.
	if strings.Count(joined, "**bold**") != 0 {
		t.Fatalf("raw **bold** still present after toolsteps.TurnDoneMsg (no replace):\n%s", joined)
	}
	// The transcript must contain the glamour ANSI bold escape exactly
	// where the streamed text used to live — i.e. within the replaced
	// range, not appended after it. We don't pin byte offsets, only that
	// it appears at least once.
	if !hasSGRBold(joined) {
		t.Fatalf("glamour bold attribute missing after replace:\n%s", joined)
	}
	if m.engine.replyStart != -1 {
		t.Fatalf("replyStart not reset to -1 after toolsteps.TurnDoneMsg: got %d", m.engine.replyStart)
	}
	if m.engine.liveReplyIdx != -1 {
		t.Fatalf("liveReplyIdx not reset to -1 after toolsteps.TurnDoneMsg: got %d", m.engine.liveReplyIdx)
	}
}

// TestTUITools_LiveStreamCoalescesSessionMessages pins cycle-229's
// "one assistant message per turn" semantics: multiple backendChunkMsgs
// for the same turn must accumulate into a single m.engine.session entry (not
// N entries as cycle-99 did). The liveReplyIdx field tracks the
// in-progress assistant message; backendChunkMsg appends on the first
// chunk and updates in place on subsequent chunks.
func TestTUITools_LiveStreamCoalescesSessionMessages(t *testing.T) {
	m := newRunMode(nil, "u@t", "m", "t", "/tmp", true)
	// Seed a user message by hand so we don't need ANTIGRAVITY_HOME +
	// submitPrompt to put the DB on disk. The point of the test is the
	// chunk→session coalescing, not submitPrompt's plumbing.
	m.engine.session = append(m.engine.session, backend.Message{Role: "user", Content: "ask"})
	if len(m.engine.session) != 1 || m.engine.session[0].Role != "user" {
		t.Fatalf("pre-chunk session = %+v, want 1 user msg", m.engine.session)
	}
	_, _ = m.Update(backendChunkMsg("Hello"))
	_, _ = m.Update(backendChunkMsg(", "))
	_, _ = m.Update(backendChunkMsg("world!"))
	if len(m.engine.session) != 2 {
		t.Fatalf("session grew to %d entries after 3 chunks, want 2: %+v",
			len(m.engine.session), m.engine.session)
	}
	if got := m.engine.session[1]; got.Role != "assistant" || got.Content != "Hello, world!" {
		t.Fatalf("assistant message = %+v, want {assistant, \"Hello, world!\"}", got)
	}
	// toolsteps.TurnDoneMsg overwrites the liveReply entry with the authoritative
	// msg.reply (rather than appending a duplicate).
	_, _ = m.Update(toolsteps.TurnDoneMsg{Reply: "Hello, world!"})
	if len(m.engine.session) != 2 {
		t.Fatalf("toolsteps.TurnDoneMsg appended a duplicate assistant entry, got %d messages: %+v",
			len(m.engine.session), m.engine.session)
	}
	if got := m.engine.session[1].Content; got != "Hello, world!" {
		t.Fatalf("toolsteps.TurnDoneMsg message = %q, want \"Hello, world!\"", got)
	}
}

// TestTUITools_LiveStreamCoalescesChunksIntoOneRow pins cycle-230's
// line discipline + cycle-232's mid-stream glamour: multiple
// backendChunkMsg updates with no '\n' boundary must end up in ONE
// row of m.engine.response (not N as cycle-229 did), so a model that
// streams token-sized deltas does not produce one transcript row
// per delta. A chunk containing '\n' must extend the current row up
// to the newline, then start a fresh row for the trailing partial.
//
// Cycle 232 update: completing a line via '\n' now glamour-renders
// the row in place (so the user sees the styled output grow row-by-
// row as the model types, matching AGY 1.4.2). Pre-cycle-232 the
// test pinned the row's RAW text at this point ("Here's a test.
// line2") — that contract changes to "the row contains the prose
// and now carries ANSI styling for any markdown constructs present".
// The reply in this test uses **bold** markers so glamour can be
// expected to emit ANSI escapes.
//
// Cycle 272 update: the live range is rendered with the same
// renderMarkdown function toolsteps.TurnDoneMsg uses, on every chunk, from the
// raw accumulated reply — so a partially-typed row is rendered too
// (an unclosed **bold** shows literally, exactly as goldmark parses
// it). toolsteps.TurnDoneMsg still swaps the live range for the authoritative
// single-copy render (no double-rendering).
func TestTUITools_LiveStreamCoalescesChunksIntoOneRow(t *testing.T) {
	m := newRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.engine.response = append(m.engine.response, "> stream markdown")
	m.engine.liveStart = len(m.engine.response)
	// Three chunks, no '\n' anywhere. They must still coalesce into
	// one transcript row (cycle 230 discipline) — and cycle 272 means
	// that one row is already the rendered form of the accumulated
	// text, not the raw chunk concatenation.
	_, _ = m.Update(backendChunkMsg("Here's "))
	_, _ = m.Update(backendChunkMsg("**bold "))
	_, _ = m.Update(backendChunkMsg("text**."))
	if m.engine.replyStart < m.engine.liveStart {
		t.Fatalf("replyStart=%d not anchored (liveStart=%d)", m.engine.replyStart, m.engine.liveStart)
	}
	startedAt := m.engine.replyStart
	if got, want := len(m.engine.response), startedAt+1; got != want {
		t.Fatalf("3 same-line chunks produced %d rows, want %d: %+v",
			got-startedAt, want-startedAt, m.engine.response[startedAt:])
	}
	// Cycle 272: the live row is renderMarkdown("Here's **bold
	// text**.") — the ** markers are already gone (styled to ANSI)
	// and the prose is present.
	if got := diffaction.StripANSI(m.engine.response[startedAt]); !strings.Contains(got, "Here's bold text.") {
		t.Fatalf("live row = %q, want visible text %q", got,
			"Here's bold text.")
	}
	if strings.Contains(diffaction.StripANSI(m.engine.response[startedAt]), "**") {
		t.Fatalf("live row still carries raw ** markers: %q", m.engine.response[startedAt])
	}
	// A chunk ending in '\n' extends the same paragraph; glamour
	// renders the accumulated buffer, so the segment before the
	// newline lands on the row and the whole paragraph stays one
	// rendered row (no raw trailing slot to preserve).
	_, _ = m.Update(backendChunkMsg(" line2\n"))
	live := diffaction.StripANSI(strings.Join(m.engine.response[startedAt:], "\n"))
	if !strings.Contains(live, "Here's bold text. line2") {
		t.Fatalf("accumulated prose missing after \\n chunk:\n%s", live)
	}
	if strings.Contains(live, "**") {
		t.Fatalf("raw ** markers survived the mid-stream render:\n%s", live)
	}
	// And it carries ANSI styling — glamour styled the bold span.
	if !strings.Contains(strings.Join(m.engine.response[startedAt:], "\n"), "\x1b[") {
		t.Fatalf("live rows carry no ANSI styling mid-stream:\n%s", live)
	}
	// toolsteps.TurnDoneMsg glamour-replace: the reply "Here's **bold text**.
	// line2" must replace (not duplicate) the live range. The
	// substring must appear exactly once.
	_, _ = m.Update(toolsteps.TurnDoneMsg{Reply: "Here's **bold text**. line2"})
	joined := strings.Join(m.engine.response, "\n")
	if n := strings.Count(diffaction.StripANSI(joined), "Here's"); n != 1 {
		t.Fatalf("toolsteps.TurnDoneMsg duplicated text, found %d copies of "+
			"%q:\n%s", n, "Here's", joined)
	}
	if n := strings.Count(diffaction.StripANSI(joined), "line2"); n != 1 {
		t.Fatalf("toolsteps.TurnDoneMsg duplicated text, found %d copies of %q:\n%s",
			n, "line2", joined)
	}
	if m.engine.replyStart != -1 {
		t.Fatalf("replyStart not reset to -1 after toolsteps.TurnDoneMsg: got %d", m.engine.replyStart)
	}
	if m.engine.liveReplyIdx != -1 {
		t.Fatalf("liveReplyIdx not reset to -1 after toolsteps.TurnDoneMsg: got %d", m.engine.liveReplyIdx)
	}
}

func TestTUITools_LiveStream_MultiParagraphIncremental(t *testing.T) {
	m := newRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.engine.response = append(m.engine.response, "> stream paragraphs")
	m.engine.liveStart = len(m.engine.response)

	_, _ = m.Update(backendChunkMsg("First paragraph content.\n\n"))
	if m.engine.lastStreamSave.IsZero() {
		t.Fatalf("expected lastStreamSave to be recorded on first chunk")
	}

	_, _ = m.Update(backendChunkMsg("Second paragraph begins..."))
	visible := diffaction.StripANSI(strings.Join(m.engine.response, "\n"))
	if !strings.Contains(visible, "First paragraph content.") {
		t.Fatalf("missing first paragraph in live view:\n%s", visible)
	}
	if !strings.Contains(visible, "Second paragraph begins...") {
		t.Fatalf("missing second paragraph in live view:\n%s", visible)
	}
}

// TestTUITools_StreamedLinesRenderGlamour pins the cycle-232 mid-
// stream glamour surface: as each '\n'-bounded line completes (via
// a chunk-ending '\n' boundary), the cycle-232 backendChunkMsg
// handler runs that completed line through mdrender.LineANSI and
// replaces the raw row in m.engine.response with the styled row. The user
// sees the styled output grow row-by-row as the model streams,
// matching the AGY 1.4.2 chat-transcript shape (crush's
// streamingMarkdown.Render in
// ../crush/internal/ui/chat/streaming_markdown.go:91 also does
// per-line glamour; wagyu's simpler one-pass-per-line path is
// cycle-232's design).
//
// Setup: three chunks. Chunk 1 carries bold markdown and a
// trailing '\n' — it finalises row 0 ("**First bold**.") and
// creates an empty trailing partial slot. Chunks 2 + 3 extend the
// partial slot (no '\n' in either), so row 0 stays glamour'd and
// the partial row stays raw mid-stream.
//
// The first row's ANSI styling comes from chunk 1 alone. We assert
// the row carries an escape AFTER chunk 1 (the cycle-232 visible
// change), not just after toolsteps.TurnDoneMsg as cycle-228 did. The test
// uses bold markdown markers because glamour only emits ANSI
// escapes for styled constructs — plain prose renders with
// padding-only (no escapes).
func TestTUITools_StreamedLinesRenderGlamour(t *testing.T) {
	m := newRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.engine.response = append(m.engine.response, "> ask three chunks")
	m.engine.liveStart = len(m.engine.response)
	// Chunk 1 carries a trailing '\n' → it holds "**First bold**."
	// and cycle 272 renders it immediately.
	_, _ = m.Update(backendChunkMsg("**First bold**.\n"))
	// Cycle 285: the reference separates the prompt from the first
	// assistant message with one blank row, so the run starts at
	// liveStart+1.
	if m.engine.replyStart != m.engine.liveStart+1 || m.engine.response[m.engine.liveStart] != "" {
		t.Fatalf("replyStart=%d not anchored after the blank row at %d",
			m.engine.replyStart, m.engine.liveStart)
	}
	completedRow := m.engine.response[m.engine.replyStart]
	// Cycle 272: the row at replyStart carries ANSI glamour styling.
	if !strings.Contains(completedRow, "\x1b[") {
		t.Fatalf("completed row did not receive mid-stream "+
			"glamour styling:\n  got: %q", completedRow)
	}
	if !strings.Contains(completedRow, "First bold") {
		t.Fatalf("completed row lost prose content: %q", completedRow)
	}
	// The raw ** markers must have been stripped — glamour
	// converts them to ANSI bold opens/closes on a colour-capable
	// terminal.
	if strings.Contains(diffaction.StripANSI(completedRow), "**First bold**") {
		t.Fatalf("completed row still carries raw ** markers: %q",
			completedRow)
	}
	// Chunk 2 extends the reply with a second paragraph containing a
	// complete bold span. Cycle 272 renders the accumulated buffer:
	// both the first paragraph and the new bold span are styled.
	_, _ = m.Update(backendChunkMsg("**Second** "))
	if got := m.engine.response[m.engine.replyStart]; !strings.Contains(got, "\x1b[") {
		t.Fatalf("row 0 lost glamour after chunk 2: %q", got)
	}
	if got := strings.TrimSpace(diffaction.StripANSI(m.engine.response[len(m.engine.response)-1])); got != "Second" {
		t.Fatalf("trailing row after chunk 2 = %q, want %q",
			got, "Second")
	}
	// Chunk 3 extends the row; the now-unclosed `**` renders
	// literally ("partial third" stays as typed).
	_, _ = m.Update(backendChunkMsg("partial third"))
	if got := m.engine.response[m.engine.replyStart]; !strings.Contains(got, "\x1b[") {
		t.Fatalf("row 0 lost glamour after chunk 3: %q", got)
	}
	if got := strings.TrimSpace(diffaction.StripANSI(m.engine.response[len(m.engine.response)-1])); got != "Second partial third" {
		t.Fatalf("trailing row after chunk 3 = %q, want %q", got,
			"Second partial third")
	}
}

// TestTUITools_PartialLastLineRendersLiterally pins the cycle-272
// mid-stream contract for a still-arriving line: the accumulated
// buffer is rendered on every chunk, so a half-typed construct is
// interpreted by goldmark as it currently stands. An unclosed
// `**bold` is a literal `**bold` (goldmark does not open emphasis
// without a closer), which is exactly what the user has typed so
// far; when the closer arrives the span snaps to bold. There is no
// "raw partial slot" any more — the live range is always a render
// of the accumulated markdown, which is what removes the pre-272
// row-explosion and the half-rendered cross-line artefacts.
func TestTUITools_PartialLastLineRendersLiterally(t *testing.T) {
	m := newRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.engine.response = append(m.engine.response, "> ask mid-stream")
	m.engine.liveStart = len(m.engine.response)
	_, _ = m.Update(backendChunkMsg("**bold partial"))
	// Cycle 285: the reference separates the prompt from the first
	// assistant message with one blank row, so the run starts at
	// liveStart+1.
	if m.engine.replyStart != m.engine.liveStart+1 || m.engine.response[m.engine.liveStart] != "" {
		t.Fatalf("replyStart=%d not anchored after the blank row at %d",
			m.engine.replyStart, m.engine.liveStart)
	}
	row := m.engine.response[m.engine.replyStart]
	// The unclosed ** renders literally: the visible glyphs are the
	// source's own, so nothing is silently dropped or mis-bolded.
	if got := strings.TrimSpace(diffaction.StripANSI(row)); got != "**bold partial" {
		t.Fatalf("partial row visible text = %q, want %q", got,
			"**bold partial")
	}
	// Closing the span promotes it to real bold with the markers
	// gone — proving the partial is rendered through the same
	// pipeline and re-rendered when the construct completes.
	_, _ = m.Update(backendChunkMsg(" done**"))
	if strings.Contains(strings.Join(m.engine.response[m.engine.replyStart:], "\n"), "**") {
		t.Fatalf("closing chunk left raw ** markers:\n%s",
			strings.Join(m.engine.response[m.engine.replyStart:], "\n"))
	}
	if !hasSGRBold(strings.Join(m.engine.response[m.engine.replyStart:], "\n")) {
		t.Fatalf("closing chunk did not style the completed span:\n%s",
			strings.Join(m.engine.response[m.engine.replyStart:], "\n"))
	}
}

// TestTUITools_TurnDoneReplaceStillIdempotent is the cycle-232
// toolsteps.TurnDoneMsg contract: after a streaming turn that mid-stream-
// styled its completed lines, toolsteps.TurnDoneMsg produces a final m.engine.response
// in which every line carries glamour styling (the partial that
// toolsteps.TurnDoneMsg finally completes now also gets glamour). Crucially,
// no double-styling — the cycle-228 renderMarkdown(whole-reply)
// call that drives the toolsteps.TurnDoneMsg replace runs exactly once on
// the full reply, so a styled row doesn't carry nested escapes.
//
// The cycle-228 toolsteps.BlockLines wipe earlier in the toolsteps.TurnDoneMsg
// handler (transcript.ReplaceRange(liveStart, ..., block)) clears the mid-
// stream-styled rows, and the cycle-228 transcript.ReplaceRange(replyStart,
// ..., indented) re-inserts the cycle-228 renderMarkdown output via
// transcript.ReplaceRange's insert semantics. The mid-stream glamour pass is
// the *transient* visual the user sees during streaming; this
// replace is the *authoritative* final state. Cross-line constructs
// that mid-stream glamour rendered slightly off (an unclosed
// `**bold**` like the chunk sequence below) are re-styled cleanly.
func TestTUITools_TurnDoneReplaceStillIdempotent(t *testing.T) {
	m := newRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.engine.response = append(m.engine.response, "> idempotent replace")
	m.engine.liveStart = len(m.engine.response)
	// Three chunks forming one soft-wrapped paragraph ("**bold
	// first**" + "*italic second*" + "trailing partial"). Cycle 272
	// renders the accumulated buffer on every chunk.
	_, _ = m.Update(backendChunkMsg("**bold first**\n"))
	_, _ = m.Update(backendChunkMsg("*italic second*\n"))
	_, _ = m.Update(backendChunkMsg("trailing partial"))
	// After these chunks the live range is already the rendered
	// form: styled (ANSI present), markers stripped, all text present.
	live := strings.Join(m.engine.response[m.engine.liveStart:], "\n")
	if !strings.Contains(live, "\x1b[") {
		t.Fatalf("live range carries no ANSI styling mid-stream:\n%q", live)
	}
	for _, expected := range []string{"bold first", "italic second", "trailing partial"} {
		if !strings.Contains(diffaction.StripANSI(live), expected) {
			t.Fatalf("live range lost text %q mid-stream:\n%s", expected, diffaction.StripANSI(live))
		}
	}
	if strings.Contains(diffaction.StripANSI(live), "**") || strings.Contains(diffaction.StripANSI(live), "*italic") {
		t.Fatalf("live range still carries raw markers:\n%s", diffaction.StripANSI(live))
	}
	// Final reply covers all three lines (the trailing partial is
	// part of line 3). toolsteps.TurnDoneMsg renders the whole reply via
	// renderMarkdown and replaces the live range.
	fullReply := "**bold first**\n*italic second*\ntrailing partial"
	_, _ = m.Update(toolsteps.TurnDoneMsg{Reply: fullReply})
	joined := strings.Join(m.engine.response, "\n")
	// Every line in the joined response must contain its text
	// content (after the cycle-228 renderMarkdown + indentModelReply
	// pass that handles the replace). Glamour's whole-reply render
	// may flatten short multi-line replies into one paragraph view;
	// the assertions check the substrings survive, not the row
	// structure.
	for _, expected := range []string{
		"bold first",
		"italic second",
		"trailing partial",
	} {
		if !strings.Contains(joined, expected) {
			t.Fatalf("toolsteps.TurnDoneMsg lost text %q in joined output: "+
				"\n%s", expected, joined)
		}
	}
	// Cross-line construct check: the **bold first** and
	// *italic second* must each appear exactly once — no double-
	// styling. The cycle-228 renderMarkdown of the full reply
	// runs once, so each construct gets one styling. Counting
	// occurrences guards against gratuitous duplication.
	if n := strings.Count(joined, "bold first"); n != 1 {
		t.Fatalf("toolsteps.TurnDoneMsg duplicated 'bold first' %d times: "+
			"double-styling detected? \n%s", n, joined)
	}
	// The literal `**bold first**` and `*italic second*` markers
	// must NOT survive the cycle-228 renderMarkdown pass — they
	// are styled to ANSI escapes during glamour.
	if strings.Contains(joined, "**bold first**") {
		t.Fatalf("toolsteps.TurnDoneMsg left raw **bold** markers: %q", joined)
	}
	if strings.Contains(joined, "*italic second*") {
		t.Fatalf("toolsteps.TurnDoneMsg left raw *italic* markers: %q", joined)
	}
	// And the joined output must carry ANSI escape sequences
	// (glamour's rendering output) — proves the cycle-228
	// renderMarkdown path did run, no harm done.
	if !strings.Contains(joined, "\x1b[") {
		t.Fatalf("toolsteps.TurnDoneMsg did not produce any ANSI escapes: "+
			"renderMarkdown apparently skipped — cycle-228 "+
			"contract broken?\n%s", joined)
	}
	if m.engine.replyStart != -1 {
		t.Fatalf("replyStart not reset to -1 after toolsteps.TurnDoneMsg: got %d",
			m.engine.replyStart)
	}
}

// TestTUITools_CrossLineBoldMidStream pins the cross-line-construct
// property mid-stream: a bold span split across a soft line break
// ("**bold opens\nmore text**") renders correctly because the live
// pass renders the whole accumulated buffer in one glamour pass, so
// goldmark sees both halves together. (Cycle 242 achieved this only
// when a blank-line boundary happened to exist; cycle 272 removes
// that precondition.)
func TestTUITools_CrossLineBoldMidStream(t *testing.T) {
	InvalidateCacheMidStream(t)
	m := newRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.engine.response = append(m.engine.response, "> cycle 242 cross-line bold")
	m.engine.liveStart = len(m.engine.response)

	// Chunks 1 + 2 emit a bold span split across a soft line break
	// ("**bold opens\nmore text**"). Cycle 272 renders the whole
	// accumulated buffer, so goldmark sees both halves in one pass and
	// styles them together — no blank-line boundary is required (this
	// is strictly stronger than the cycle-242 boundary path it
	// replaces).
	_, _ = m.Update(backendChunkMsg("**bold opens\n"))
	_, _ = m.Update(backendChunkMsg("more text**\n"))
	live := strings.Join(m.engine.response[m.engine.liveStart:], "\n")
	if !strings.Contains(live, "\x1b[") {
		t.Fatalf("cross-line bold produced no ANSI escapes:\n%s", live)
	}
	if !strings.Contains(diffaction.StripANSI(live), "bold opens") || !strings.Contains(diffaction.StripANSI(live), "more text") {
		t.Fatalf("cross-line bold content missing mid-stream:\n%s", diffaction.StripANSI(live))
	}
	if strings.Contains(diffaction.StripANSI(live), "**") {
		t.Fatalf("raw ** markers survived the mid-stream render:\n%s", diffaction.StripANSI(live))
	}

	// A following paragraph renders as its own block once the blank
	// line arrives, and stays rendered as more text streams in.
	_, _ = m.Update(backendChunkMsg("\n"))
	_, _ = m.Update(backendChunkMsg("New paragraph."))
	live = diffaction.StripANSI(strings.Join(m.engine.response[m.engine.liveStart:], "\n"))
	if !strings.Contains(live, "New paragraph.") {
		t.Fatalf("next paragraph missing mid-stream:\n%s", live)
	}
}

// InvalidateCacheMidStream clears the glamour TermRenderer cache
// before the cycle-242 mid-stream test runs so cached state from
// a prior test does not leak colour / wrap behaviour into the
// new boundary-aware path.
func InvalidateCacheMidStream(t *testing.T) {
	t.Helper()
	mdrender.InvalidateCache()
}

// TestTUITools_GroupHeaderForMultipleSteps pins the recovered category counts in
// the group summary (one file + one command reads "1 file, 1 command") and the
// expanded "ctrl+o to collapse" hint.
func TestTUITools_GroupHeaderForMultipleSteps(t *testing.T) {
	m := newRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.engine.response = append(m.engine.response, "> do two things")
	m.engine.liveStart = len(m.engine.response)
	_, _ = m.Update(toolsteps.TurnDoneMsg{
		Reply: "done",
		Steps: []toolsteps.Step{
			{Name: "run_command", Title: "Bash(date)", Detail: "out-1"},
			{Name: "view_file", Title: "Read(secret.txt)", Detail: "a\nb\nc\n"},
		},
	})
	joined := strings.Join(m.engine.response, "\n")
	if !strings.Contains(joined, "file") || !strings.Contains(joined, "command") {
		t.Fatalf("group summary missing:\n%s", joined)
	}
	// Cycle 178: collapsed groups hide the per-step titles — only the
	// group header is shown. This test uses a MIXED category group
	// ("1 file, 1 command") so the "(+N · <last>)" suffix is NOT
	// emitted (suffix is single-category only); the assertion below
	// only checks the count summary, not a per-step title.
	if strings.Contains(joined, "out-1") || strings.Contains(joined, "out-2") {
		t.Fatalf("details visible while collapsed:\n%s", joined)
	}

	_, _ = m.Update(ctrlKey('o'))
	joined = strings.Join(m.engine.response, "\n")
	if !strings.Contains(joined, "file") || !strings.Contains(joined, "command") {
		t.Fatalf("expanded group header wrong:\n%s", joined)
	}
	if !strings.Contains(joined, "Bash") || !strings.Contains(joined, "date") || !strings.Contains(joined, "Read") || !strings.Contains(joined, "secret.txt") {
		t.Fatalf("steps missing when expanded:\n%s", joined)
	}
	if !strings.Contains(joined, "out-1") {
		t.Fatalf("details missing when expanded:\n%s", joined)
	}
	// view_file's expanded body is the reference's line count, not the
	// file bytes ("Read 3 lines" — verified against agy 1.4.3).
	if !strings.Contains(joined, "Read 3 lines") {
		t.Fatalf("view_file detail missing its line count:\n%s", joined)
	}
}

func TestTUITools_SpinnerNamesRunningTool(t *testing.T) {
	m := newRunMode(nil, "u@t", "m", "t", "/tmp", true)
	_, _ = m.Update(toolsteps.ToolRunningMsg{Step: toolsteps.NewRunningStep("run_command", json.RawMessage(`{"CommandLine":"date"}`))})
	if m.engine.activeTool != "Running command" || !m.engine.streaming {
		t.Fatalf("spinner hint not set: %q streaming=%v", m.engine.activeTool, m.engine.streaming)
	}
	view := m.View().Content
	if !strings.Contains(view, "Running command") {
		t.Fatalf("spinner line does not name the activity:\n%s", view)
	}
	if !strings.Contains(view, "Running") || !strings.Contains(view, "(date)") {
		t.Fatalf("transcript is missing the provisional running row:\n%s", view)
	}
	if strings.Contains(view, "Generating...") {
		t.Fatalf("generic label still shown while a tool runs:\n%s", view)
	}
	// The finished step rewrites that row instead of appending a second.
	_, _ = m.Update(toolsteps.ToolStepMsg{Step: toolsteps.NewToolStep("run_command",
		json.RawMessage(`{"CommandLine":"date"}`), runCommandResult("0", "Mon Oct 20"), false)})
	joined := strings.Join(m.engine.response, "\n")
	if strings.Contains(joined, "Running (date)") {
		t.Fatalf("provisional row survived completion:\n%s", joined)
	}
	if !strings.Contains(joined, "Ran") || !strings.Contains(joined, "(date)") {
		t.Fatalf("finished row missing:\n%s", joined)
	}
}

func TestTUITools_SpinnerAndTipStickyAboveInputWhenScrolledUp(t *testing.T) {
	m := newRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 80
	m.viewport.height = 25
	m.viewport.altScreen = true

	// Populate transcript with enough rows to enable scrolling
	for i := 0; i < 60; i++ {
		m.engine.response = append(m.engine.response, fmt.Sprintf("Response line %02d", i))
	}

	// Active running command with spinner and tip
	m.engine.streaming = true
	m.engine.activeTool = "Running command"
	m.viewport.activeTip = "esc to interrupt"

	// 1. When at bottom, spinner and tip are visible above input
	viewBottom := m.View().Content
	if !strings.Contains(viewBottom, "Running command") {
		t.Fatalf("at bottom: view must contain spinner 'Running command'")
	}
	if !strings.Contains(viewBottom, "esc to interrupt") {
		t.Fatalf("at bottom: view must contain tip 'esc to interrupt'")
	}

	// 2. Scroll up into history: spinner and tip MUST remain visible (sticky above input)
	m.viewport.autoScroll = false
	m.viewport.scroll = 5
	viewScrolled := m.View().Content
	if !strings.Contains(viewScrolled, "Running command") {
		t.Fatalf("scrolled up: spinner 'Running command' must remain visible above input field:\n%s", viewScrolled)
	}
	if !strings.Contains(viewScrolled, "esc to interrupt") {
		t.Fatalf("scrolled up: tip 'esc to interrupt' must remain visible above input field:\n%s", viewScrolled)
	}

	// Verify that the spinner appears above the top prompt rule
	promptRule := m.promptRule(true)
	ruleIdx := strings.Index(viewScrolled, promptRule)
	spinnerIdx := strings.Index(viewScrolled, "Running command")
	tipIdx := strings.Index(viewScrolled, "esc to interrupt")
	if ruleIdx == -1 || spinnerIdx == -1 || tipIdx == -1 {
		t.Fatalf("missing elements in view: ruleIdx=%d, spinnerIdx=%d, tipIdx=%d", ruleIdx, spinnerIdx, tipIdx)
	}
	if spinnerIdx >= ruleIdx || tipIdx >= ruleIdx {
		t.Fatalf("spinner and tip must be above prompt rule: spinnerIdx=%d, tipIdx=%d, ruleIdx=%d", spinnerIdx, tipIdx, ruleIdx)
	}

	// 3. When streaming stops, the sticky spinner and tip disappear
	m.engine.streaming = false
	m.engine.activeTool = ""
	m.viewport.activeTip = ""
	viewIdle := m.View().Content
	if strings.Contains(viewIdle, "Running command") {
		t.Fatalf("idle: spinner must not be visible when streaming=false")
	}
	if strings.Contains(viewIdle, "esc to interrupt") {
		t.Fatalf("idle: tip must not be visible when streaming=false")
	}
}

func TestTUITools_ThoughtStepFromStream(t *testing.T) {
	be := &scriptedTUIClient{turns: [][]backend.StreamChunk{
		{
			{Text: "\x3cthink\x3e17*23 is 391, let me verify.\x3c/think\x3e\n\n"},
			{Text: "The answer is 391."},
			{Done: true, Meta: &backend.ChunkMeta{ReasoningTokens: 24, CompletionTokens: 30}},
		},
	}}
	m := newRunMode(be, "u@t", "m", "t", "/tmp", true)
	msg := m.streamPrompt("17*23?")()
	done, ok := msg.(toolsteps.TurnDoneMsg)
	if !ok {
		t.Fatalf("expected toolsteps.TurnDoneMsg, got %T", msg)
	}
	if strings.Contains(done.Reply, "think") {
		t.Fatalf("raw think text leaked into reply: %q", done.Reply)
	}
	if done.Reply != "The answer is 391." {
		t.Fatalf("reply = %q", done.Reply)
	}
	if len(done.Steps) != 1 {
		t.Fatalf("expected 1 thought step, got %+v", done.Steps)
	}
	st := done.Steps[0]
	if st.Kind != toolsteps.KindThought {
		t.Fatalf("step kind = %v; want thought", st.Kind)
	}
	if st.Tokens != 24 {
		t.Errorf("tokens = %d; want 24", st.Tokens)
	}
	if !strings.HasPrefix(tuitools.ThoughtTitle(st.Dur, st.Tokens), "Thought for ") ||
		!strings.HasSuffix(tuitools.ThoughtTitle(st.Dur, st.Tokens), ", 24 tokens") {
		t.Errorf("thought title = %q", tuitools.ThoughtTitle(st.Dur, st.Tokens))
	}
	if !strings.Contains(st.Detail, "391") {
		t.Errorf("thought detail lost: %q", st.Detail)
	}
}

// TestTUITools_ThoughtPrecedesToolSteps checks the reference's merge rule
// (cli/store.canUpgradeTrailingThoughtToToolGroup): a thought that precedes tool
// calls heads the same step block.
func TestTUITools_ThoughtPrecedesToolSteps(t *testing.T) {
	be := &scriptedTUIClient{turns: [][]backend.StreamChunk{
		{
			{Text: "\x3cthink\x3eI should read the file.\x3c/think\x3e"},
			{ToolCalls: []backend.ToolCall{{
				ID: "c1", Name: "view_file", Arguments: json.RawMessage(`{"path":"a.txt"}`),
			}}},
			{Done: true, Meta: &backend.ChunkMeta{ReasoningTokens: 8}},
		},
		{{Text: "done"}, {Done: true}},
	}}
	runner := &recordingRunner{}
	m := newRunMode(be, "u@t", "m", "t", "/tmp", true)
	m.enableTools([]backend.ToolDecl{{Name: "view_file"}}, runner)

	msg := m.streamPrompt("read a.txt")()
	done, ok := msg.(toolsteps.TurnDoneMsg)
	if !ok {
		t.Fatalf("expected toolsteps.TurnDoneMsg, got %T", msg)
	}
	if len(done.Steps) != 2 {
		t.Fatalf("expected thought + tool, got %+v", done.Steps)
	}
	if done.Steps[0].Kind != toolsteps.KindThought {
		t.Errorf("first step kind = %v; want thought", done.Steps[0].Kind)
	}
	if done.Steps[1].Kind != toolsteps.KindTool {
		t.Errorf("second step kind = %v; want tool", done.Steps[1].Kind)
	}
}

// TestTUITools_ThoughtLiveFrame pins the recovered live frame text and the
// transcript rendering of a completed thought step.
//
// Cycle 127: the leading bullet \u23fa is now wrapped in the brand blue
// (24-bit RGB 66,133,244) instead of bare. The test pins the styled
// form, matching the v1.3.6 binary's chrome palette (see colors.go).
//
// Cycle 156: the leading bullet is the U+25B8 "▸" thought glyph in
// dim gray (256-colour 238), not the U+23FA ⏺ tool glyph in brand
// blue. v1.3.6 distinguishes thought and tool steps with both a
// different glyph and a different palette.
func TestTUITools_ThoughtLiveFrame(t *testing.T) {
	wantBullet := "\x1b[38;2;88;110;117m\u25b8\x1b[39m"
	if got := toolsteps.ThinkingLiveLine(31 * time.Second); got != wantBullet+" Thinking... (31s)" {
		t.Errorf("live frame = %q; want %q", got, wantBullet+" Thinking... (31s)")
	}
	if got := toolsteps.ThinkingLiveLine(1500 * time.Millisecond); got != wantBullet+" Thinking... (1.5s)" {
		t.Errorf("live frame = %q", got)
	}

	// A completed thought renders as a collapsed step with the recovered
	// expand hint; ctrl+o reveals the reasoning.
	steps := []toolsteps.Step{{Kind: toolsteps.KindThought, Detail: "line one\nline two", Dur: 2 * time.Second, Tokens: 12}}
	collapsed := toolsteps.BlockLines(steps, false, 0)
	if len(collapsed) != 1 || !strings.Contains(collapsed[0], "Thought for 2s, 12 tokens") {
		t.Fatalf("collapsed = %+v", collapsed)
	}
	if !strings.Contains(collapsed[0], "ctrl+o to expand") {
		t.Fatalf("missing expand hint: %q", collapsed[0])
	}
	expanded := toolsteps.BlockLines(steps, true, 0)
	if len(expanded) != 3 || !strings.Contains(expanded[1], "line one") {
		t.Fatalf("expanded = %+v", expanded)
	}

	// toolsteps.ThoughtStartMsg flips the live frame on.
	m := newRunMode(nil, "u@t", "m", "t", "/tmp", true)
	if m.engine.thinking {
		t.Fatal("thinking should start false")
	}
	m.Update(toolsteps.ThoughtStartMsg{})
	if !m.engine.thinking || m.engine.thinkingSince.IsZero() {
		t.Fatal("toolsteps.ThoughtStartMsg did not start the live frame")
	}
	m.Update(toolsteps.TurnDoneMsg{})
	if m.engine.thinking {
		t.Fatal("toolsteps.TurnDoneMsg did not clear the live frame")
	}
}

func TestLiveRows_PostToolPhrase(t *testing.T) {
	m := newRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.engine.streaming = true
	// Frames past the "typing" phase show the settled phrase.
	m.engine.spinFrame = 9
	if got := strings.Join(m.liveRows(120), "\n"); !strings.Contains(got, "Generating") {
		t.Fatalf("pre-tool spinner label = %q; want Generating", got)
	}
	m.engine.anyToolRun = true
	m.engine.spinFrame = 6
	if got := strings.Join(m.liveRows(120), "\n"); !strings.Contains(got, "Working") {
		t.Fatalf("post-tool spinner label = %q; want Working", got)
	}
	m.engine.activeTool = "Running command"
	m.engine.spinFrame = 15
	if got := strings.Join(m.liveRows(120), "\n"); !strings.Contains(got, "Running command") {
		t.Fatalf("in-call spinner label = %q; want Running command", got)
	}
}

func runCommandResult(code string, output string) string {
	return "Created At: 2026-10-20T00:00:00+08:00\nCompleted At: 2026-10-20T00:00:01+08:00\n\n" +
		"The command exited with code " + code + ".\nOutput:\n" + output + "\n"
}

func TestAdvertisedToolDeclsDefaultAgent(t *testing.T) {
	reg := surface.RuntimeCatalog(t.TempDir())
	decls := toolrunner.AdvertisedToolDecls("", reg)
	want := []string{
		"read_file", "run_command", "manage_task", "send_message", "schedule",
		"invoke_subagent", "define_subagent", "manage_subagents",
		"write_file", "edit_file", "search_web", "ask_question",
	}
	if len(decls) != len(want) {
		t.Fatalf("declarations = %d, want %d", len(decls), len(want))
	}
	for i, name := range want {
		if decls[i].Name != name {
			t.Errorf("declaration[%d] = %q, want %q", i, decls[i].Name, name)
		}
		if decls[i].Description == "" {
			t.Errorf("%s: empty description", name)
		}
	}

	// 13,701 bytes at agy 1.4.6: the 1.4.5 capture was 13,530 and the
	// run_command declaration gained the BypassSandbox boolean (【一手】,
	// docs/cycles/reference-drift-146/).
	if got, want := len(cloudcode.WireToolsJSON(decls)), 13701; got != want {
		t.Errorf("tools wire bytes = %d, want %d", got, want)
	}

	required := func(name string) []string {
		t.Helper()
		for _, d := range decls {
			if d.Name != name {
				continue
			}
			var p struct {
				Required []string `json:"required"`
			}
			if err := json.Unmarshal(d.Parameters, &p); err != nil {
				t.Fatalf("%s: %v", name, err)
			}
			return p.Required
		}
		t.Fatalf("%s: not advertised", name)
		return nil
	}
	if got := required("read_file"); len(got) != 1 || got[0] != "Path" {
		t.Errorf("read_file required = %v", got)
	}
	if got := required("edit_file"); len(got) != 3 || got[0] != "Path" || got[1] != "Target" || got[2] != "Replacement" {
		t.Errorf("edit_file required = %v", got)
	}
	if got := required("run_command"); len(got) != 3 || got[0] != "CommandLine" {
		t.Errorf("run_command required = %v", got)
	}
}

func TestAdvertisedToolDeclsCustomAgent(t *testing.T) {
	reg := agenttools.Registry(surface.Catalog(t.TempDir()))
	decls := toolrunner.AdvertisedToolDecls("custom agent body", reg)
	if len(decls) != 7 {
		t.Fatalf("custom agent declarations = %d, want 7", len(decls))
	}
}

func TestCustomSubagents_AppendsDiscoveredAgents(t *testing.T) {
	ws, home := t.TempDir(), t.TempDir()
	dir := filepath.Join(ws, ".agents", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: foo\ndescription: A test agent\n---\nYou are foo.\n"
	if err := os.WriteFile(filepath.Join(dir, "foo.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(ws)
	t.Setenv("HOME", home)

	got := setup.CustomSubagents()
	if len(got) != 1 || got[0].Name != "foo" || got[0].Description != "A test agent" {
		t.Fatalf("customSubagents() = %+v", got)
	}
}

func TestAgentToolRegistry_CustomAgentHasSevenTools(t *testing.T) {
	m := &runMode{session: sessionState{agentBody: "You are foo."}}
	catalog := agentToolRegistry(t.TempDir(), m.session.agentBody)
	m.registerSubagentQuestionTool(catalog)

	names := catalog.List()
	sort.Strings(names)
	want := "generate_image,manage_task,read_url_content,schedule,search_web,send_message,view_file"
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("custom agent tools = %s, want %s", got, want)
	}
	if _, ok := catalog.Get("ask_question"); ok {
		t.Error("ask_question leaked into the custom agent surface")
	}
}

func TestAgentToolRegistry_DefaultIncludesAskQuestion(t *testing.T) {
	m := &runMode{}
	catalog := agentToolRegistry(t.TempDir(), "")
	m.registerSubagentQuestionTool(catalog)
	if _, ok := catalog.Get("ask_question"); !ok {
		t.Error("default agent lost ask_question")
	}
}

func TestCtrlOExpandedRunCommandOutputIsNotBlank(t *testing.T) {
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})

	m.engine.response = append(m.engine.response, "> ls")
	m.engine.liveStart = len(m.engine.response)
	m.engine.session = append(m.engine.session, backend.Message{Role: "user", Content: "ls"})

	_, _ = m.Update(toolsteps.TurnDoneMsg{
		Reply: "done",
		Steps: []toolsteps.Step{{
			Kind:  toolsteps.KindTool,
			Name:  "run_command",
			Title: "Bash (ls)",
			Detail: "Created At: x\r\nCompleted At: y\r\n\r\n" +
				"The command exited with code 0.\r\nOutput:\r\nhello\r\nworld\r\n",
		}},
	})

	if got := m.View().Content; strings.Contains(got, "hello") {
		t.Fatalf("collapsed view should not show the detail:\n%s", got)
	}

	_, _ = m.Update(ctrlKey('o')) // expand

	content := m.View().Content
	if i := strings.IndexByte(content, '\r'); i >= 0 {
		t.Fatalf("View() emits a bare CR at byte %d; the viewport pads after it, "+
			"so ultraviolet repaints the row from column 0:\n%q", i, content)
	}

	buf := uv.NewScreenBuffer(90, 30)
	uv.NewStyledString(content).Draw(&buf, buf.Bounds())
	screen := buf.String()

	if !strings.Contains(screen, "hello") {
		t.Fatalf("expanded output line 'hello' was blanked after ctrl+o:\n%s", screen)
	}
	if !strings.Contains(screen, "world") {
		t.Fatalf("expanded output line 'world' was blanked after ctrl+o:\n%s", screen)
	}
	if !strings.Contains(screen, "Command output:") {
		t.Fatalf("expanded header 'Command output:' missing after ctrl+o:\n%s", screen)
	}
}

func liveStepFixture(t *testing.T) (*runMode, toolsteps.Step) {
	t.Helper()
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m.engine.response = append(m.engine.response, "> run it")
	m.engine.liveStart = len(m.engine.response)
	step := toolsteps.Step{
		Kind:   toolsteps.KindTool,
		Name:   "run_command",
		Title:  "Bash (echo one)",
		Detail: "Created At: x\nCompleted At: y\n\nThe command exited with code 0.\nOutput:\nhello-live\n",
	}
	return m, step
}

func TestCtrlOExpandsLiveStepDuringStreaming(t *testing.T) {
	m, step := liveStepFixture(t)

	m.Update(toolsteps.ToolStepMsg{Step: step})

	if strings.Contains(m.View().Content, "hello-live") {
		t.Fatalf("collapsed live step already shows its detail")
	}
	if !strings.Contains(m.View().Content, "ctrl+o to expand") {
		t.Fatalf("collapsed live step is missing the expand hint:\n%s", m.View().Content)
	}

	m.Update(ctrlKey('o'))

	view := m.View().Content
	if !strings.Contains(view, "hello-live") {
		t.Fatalf("ctrl+o did not expand the in-flight step:\n%s", view)
	}
	if !strings.Contains(view, "ctrl+o to collapse") {
		t.Fatalf("live step hint still says expand after ctrl+o:\n%s", view)
	}

	m.Update(ctrlKey('o'))
	if strings.Contains(m.View().Content, "hello-live") {
		t.Fatalf("second ctrl+o did not collapse the live step:\n%s", m.View().Content)
	}
	if got := len(m.engine.response); got != 2 {
		t.Fatalf("response has %d rows after collapse, want 2 (prompt + step)", got)
	}
}

func TestCtrlOTogglesLiveStepsAroundAStreamedChunk(t *testing.T) {
	m, first := liveStepFixture(t)
	second := toolsteps.Step{
		Kind:   toolsteps.KindTool,
		Name:   "run_command",
		Title:  "Bash (echo two)",
		Detail: "Created At: x\nOutput:\ntwo-live\n",
	}
	m.Update(toolsteps.ToolStepMsg{Step: first})
	m.Update(backendChunkMsg("between the steps"))
	m.Update(toolsteps.ToolStepMsg{Step: second})
	rowsBefore := len(m.engine.response)

	m.Update(ctrlKey('o'))
	view := m.View().Content
	if !strings.Contains(view, "hello-live") || !strings.Contains(view, "two-live") {
		t.Fatalf("ctrl+o did not expand both live steps:\n%s", view)
	}

	m.Update(ctrlKey('o'))
	if got := len(m.engine.response); got != rowsBefore {
		t.Fatalf("collapse left %d rows, want the original %d:\n%s", got, rowsBefore, strings.Join(m.engine.response, "\n"))
	}
	view = m.View().Content
	if strings.Contains(view, "hello-live") || strings.Contains(view, "two-live") {
		t.Fatalf("details survived the collapse:\n%s", view)
	}
}

func TestTurnDoneKeepsStepsThatFollowAStreamedChunk(t *testing.T) {
	m, first := liveStepFixture(t)
	second := toolsteps.Step{
		Kind:   toolsteps.KindTool,
		Name:   "run_command",
		Title:  "Bash (sleep 2; echo two)",
		Detail: "Created At: x\nCompleted At: y\n\nThe command exited with code 0.\nOutput:\ntwo\n",
	}

	m.Update(toolsteps.ToolStepMsg{Step: first})
	m.Update(backendChunkMsg("Running the second one now."))
	if m.engine.replyStart < 0 {
		t.Fatalf("replyStart was not anchored by backendChunkMsg")
	}
	m.Update(toolsteps.ToolStepMsg{Step: second})
	m.Update(backendChunkMsg("DONE"))

	m.viewport.expanded = true
	m.Update(toolsteps.TurnDoneMsg{Reply: "DONE", Steps: []toolsteps.Step{first, second}})

	joined := strings.Join(m.engine.response, "\n")
	if !strings.Contains(joined, "echo one") {
		t.Fatalf("finalised block lost the first step:\n%s", joined)
	}
	if !strings.Contains(joined, "echo two") {
		t.Fatalf("finalised block lost the step that followed the streamed chunk:\n%s", joined)
	}
	if !strings.Contains(joined, "DONE") {
		t.Fatalf("finalised block lost the reply:\n%s", joined)
	}
	if i, j := strings.Index(joined, "sleep 2; echo two"), strings.Index(joined, "DONE"); j < i {
		t.Fatalf("reply rendered before the tool block (step at %d, reply at %d)", i, j)
	}
	if len(m.engine.liveStepRuns) != 0 {
		t.Fatalf("liveStepRuns not cleared at turn end: %+v", m.engine.liveStepRuns)
	}
}

func TestCtrlO_ScrollbackAndClearScreenCmd(t *testing.T) {
	m, step := liveStepFixture(t)
	m.Update(toolsteps.ToolStepMsg{Step: step})

	// Simulate user scrolled up into history.
	m.viewport.scroll = 10
	m.viewport.autoScroll = false

	_, cmd := m.Update(ctrlKey('o'))

	// The reference clears scrollback when toggling trajectory expansion.
	if m.viewport.scroll != 0 {
		t.Fatalf("expected scroll to reset to 0, got %d", m.viewport.scroll)
	}
	if !m.viewport.autoScroll {
		t.Fatalf("expected autoScroll to reset to true")
	}
	if !m.viewport.expandedUserToggled {
		t.Fatalf("expected expandedUserToggled to be true")
	}
	if !m.viewport.expanded {
		t.Fatalf("expected expanded to toggle to true")
	}
	if cmd == nil {
		t.Fatalf("expected tea.ClearScreen command, got nil")
	}
}

func TestCtrlO_ModalPanelIgnored(t *testing.T) {
	m, step := liveStepFixture(t)
	m.Update(toolsteps.ToolStepMsg{Step: step})

	// Open a modal panel (e.g. config).
	m.overlays.panel.kind = panelregistry.Config
	expBefore := m.viewport.expanded

	_, cmd := m.Update(ctrlKey('o'))

	if m.viewport.expanded != expBefore {
		t.Fatalf("ctrl+o should not toggle trajectory while modal panel is open")
	}
	if cmd != nil {
		t.Fatalf("ctrl+o should not return ClearScreen while modal panel is open, got %v", cmd)
	}
}

func TestVerbosityLive_RerendersBlocks(t *testing.T) {
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})
	m.engine.response = append(m.engine.response, "> run")
	m.engine.liveStart = len(m.engine.response)
	m.engine.verbosity = "medium"

	metrics := toolsteps.TokenMetrics{
		Input:     12500,
		Output:    450,
		Reasoning: 120,
	}

	m.Update(toolsteps.TurnDoneMsg{
		Reply: "all done",
		Steps: []toolsteps.Step{{
			Kind:   toolsteps.KindTool,
			Name:   "run_command",
			Title:  "Bash (ls -l)",
			Detail: "Output:\nfile.txt\n",
		}},
		TokenMetrics: metrics,
	})

	// Explicit medium verbosity: should show Bash summary, not token metrics table.
	viewMedium := diffaction.StripANSI(m.View().Content)
	if strings.Contains(viewMedium, "12.5k in") {
		t.Fatalf("medium verbosity unexpectedly shows low-verbosity token metrics:\n%s", viewMedium)
	}
	if !strings.Contains(viewMedium, "Bash (ls -l)") {
		t.Fatalf("medium verbosity should show tool call summary:\n%s", viewMedium)
	}

	// Switch verbosity to low live: should rerender into CollapsedLowVerbosityGroup.
	m.applyVerbosityLive("low")
	viewLow := diffaction.StripANSI(m.View().Content)
	if !strings.Contains(viewLow, "12.5k in | 450 out | 120 thinking") {
		t.Fatalf("low verbosity missing token metrics row:\n%s", viewLow)
	}
	if !strings.Contains(viewLow, "ctrl+o to expand") {
		t.Fatalf("low verbosity missing expand hint:\n%s", viewLow)
	}

	// Expand with ctrl+o while in low verbosity.
	m.Update(ctrlKey('o'))
	viewLowExpanded := diffaction.StripANSI(m.View().Content)
	if !strings.Contains(viewLowExpanded, "file.txt") {
		t.Fatalf("low verbosity expanded should show detail:\n%s", viewLowExpanded)
	}
	if !strings.Contains(viewLowExpanded, "ctrl+o to collapse") {
		t.Fatalf("low verbosity expanded should show collapse hint:\n%s", viewLowExpanded)
	}

	// Collapse back.
	m.Update(ctrlKey('o'))
	viewLowCollapsedAgain := diffaction.StripANSI(m.View().Content)
	if !strings.Contains(viewLowCollapsedAgain, "12.5k in | 450 out | 120 thinking") {
		t.Fatalf("collapsed low verbosity should restore metrics row:\n%s", viewLowCollapsedAgain)
	}

	// Switch back to medium.
	m.applyVerbosityLive("medium")
	viewMediumAgain := diffaction.StripANSI(m.View().Content)
	if strings.Contains(viewMediumAgain, "12.5k in") {
		t.Fatalf("switching back to medium should remove low-verbosity token metrics table:\n%s", viewMediumAgain)
	}
}

func TestTUITools_LiveThinking_SnippetAndExpand(t *testing.T) {
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})

	// Enter thinking
	m.Update(toolsteps.ThoughtStartMsg{})
	if !m.engine.thinking {
		t.Fatalf("expected m.engine.thinking to be true")
	}

	// Stream thinking chunks
	m.Update(toolsteps.ThoughtChunkMsg{Delta: "I am analyzing the problem.\nLet's verify the binary."})
	if m.engine.liveThinkingSnippet != "Let's verify the binary." {
		t.Fatalf("expected liveThinkingSnippet 'Let's verify the binary.', got %q", m.engine.liveThinkingSnippet)
	}

	// Spinner live rows should contain the snippet
	rows := m.liveRows(80)
	if len(rows) == 0 || !strings.Contains(rows[0], "Let's verify the binary.") {
		t.Fatalf("expected liveRows[0] to contain thinking snippet, got %v", rows)
	}

	// Truncation on narrow terminal
	narrowRows := m.liveRows(25)
	if len(narrowRows) == 0 {
		t.Fatalf("expected non-empty narrowRows")
	}
	cleanNarrow := diffaction.StripANSI(narrowRows[0])
	if ansi.StringWidth(cleanNarrow) > 25 || !strings.HasSuffix(cleanNarrow, "...") {
		t.Fatalf("expected narrow spinner line to be truncated to <=25 with '...', got %q (width=%d)", cleanNarrow, ansi.StringWidth(cleanNarrow))
	}

	// Expand live thinking with ctrl+o
	m.Update(ctrlKey('o'))
	if !m.isExpanded() {
		t.Fatalf("expected m.isExpanded() to be true")
	}
	viewExpanded := diffaction.StripANSI(strings.Join(m.engine.response, "\n"))
	if !strings.Contains(viewExpanded, "Thinking...") {
		t.Fatalf("expected response to contain live 'Thinking...' header, got:\n%s", viewExpanded)
	}
	if !strings.Contains(viewExpanded, "Let's verify the binary.") {
		t.Fatalf("expected response to contain reasoning body, got:\n%s", viewExpanded)
	}

	// Stream another chunk while expanded
	m.Update(toolsteps.ThoughtChunkMsg{Delta: "\nFinalizing plan."})
	viewUpdated := diffaction.StripANSI(strings.Join(m.engine.response, "\n"))
	if !strings.Contains(viewUpdated, "Finalizing plan.") {
		t.Fatalf("expected live thinking block to update with new chunk, got:\n%s", viewUpdated)
	}

	// Collapse back
	m.Update(ctrlKey('o'))
	if m.isExpanded() {
		t.Fatalf("expected m.isExpanded() to be false")
	}
	viewCollapsed := diffaction.StripANSI(strings.Join(m.engine.response, "\n"))
	if strings.Contains(viewCollapsed, "Thinking...") {
		t.Fatalf("expected live thinking to be removed from response when collapsed, got:\n%s", viewCollapsed)
	}

	// Transition to tool running: thinking should finalize
	cmdStep := toolsteps.NewRunningStep("run_command", json.RawMessage(`{"CommandLine":"echo 1"}`))
	m.Update(toolsteps.ToolRunningMsg{Step: cmdStep})
	if m.engine.thinking {
		t.Fatalf("expected thinking to be false once tool running begins")
	}
	viewTool := diffaction.StripANSI(strings.Join(m.engine.response, "\n"))
	if !strings.Contains(viewTool, "Thought for") {
		t.Fatalf("expected finalized thought step in response, got:\n%s", viewTool)
	}
}

func TestTUITools_CtrlO_NoDuplicationAndCleanToggle(t *testing.T) {
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.engine.streaming = true
	m.engine.liveStart = len(m.engine.response)

	// 1. Thinking starts
	m.Update(toolsteps.ThoughtStartMsg{})
	m.Update(toolsteps.ThoughtChunkMsg{Delta: "Thinking line 1\nThinking line 2"})

	// Toggling ctrl+o during thinking: expand -> collapse -> expand -> collapse
	m.Update(ctrlKey('o')) // expand
	if !m.isExpanded() {
		t.Fatalf("expected expanded")
	}
	m.Update(ctrlKey('o')) // collapse
	if m.isExpanded() {
		t.Fatalf("expected collapsed")
	}
	m.Update(ctrlKey('o')) // expand
	m.Update(ctrlKey('o')) // collapse

	// 2. Tool running begins (thinking finishes while collapsed)
	cmdStep := toolsteps.NewRunningStep("search_web", json.RawMessage(`{"query":"weather"}`))
	m.Update(toolsteps.ToolRunningMsg{Step: cmdStep})

	// Tool step finishes
	doneStep := toolsteps.NewToolStep("search_web", json.RawMessage(`{"query":"weather"}`), "sunny tomorrow", false)
	m.Update(toolsteps.ToolStepMsg{Step: doneStep})

	// Toggling ctrl+o after tool finishes: expand -> collapse
	m.Update(ctrlKey('o')) // expand
	m.Update(ctrlKey('o')) // collapse

	// 3. Turn done lands with authoritative Thought step + Tool step
	thoughtStep := toolsteps.Step{
		Kind:   toolsteps.KindThought,
		Dur:    5 * time.Second,
		Detail: "Thinking line 1\nThinking line 2",
		Tokens: 120,
	}
	m.Update(toolsteps.TurnDoneMsg{
		Reply: "Tomorrow is sunny.",
		Steps: []toolsteps.Step{thoughtStep, doneStep},
		TokenMetrics: toolsteps.TokenMetrics{
			Input:     1000,
			Output:    200,
			Reasoning: 120,
		},
	})

	// Check blocks count: should have exactly 1 thought block
	thoughtBlocks := 0
	for _, b := range m.engine.blocks {
		for _, s := range b.Steps {
			if s.Kind == toolsteps.KindThought {
				thoughtBlocks++
			}
		}
	}
	if thoughtBlocks != 1 {
		t.Fatalf("expected exactly 1 thought block in m.engine.blocks, got %d", thoughtBlocks)
	}

	// Repeated toggling ctrl+o after turn completes
	for i := 0; i < 6; i++ {
		m.Update(ctrlKey('o'))
	}

	// Check for any duplicate "Thought for" in m.engine.response
	joined := strings.Join(m.engine.response, "\n")
	thoughtOccurrences := strings.Count(joined, "Thought for")
	if thoughtOccurrences != 1 {
		t.Fatalf("expected 'Thought for' to appear exactly once in response, got %d times in:\n%s", thoughtOccurrences, joined)
	}

	// Now collapse and check line count stability
	if m.isExpanded() {
		m.Update(ctrlKey('o'))
	}
	linesCollapsed := len(m.engine.response)
	m.Update(ctrlKey('o')) // expand
	m.Update(ctrlKey('o')) // collapse
	if len(m.engine.response) != linesCollapsed {
		t.Fatalf("response line count changed after expand+collapse: was %d, now %d", linesCollapsed, len(m.engine.response))
	}
}

func TestTUITools_SpinnerTips(t *testing.T) {
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 30})
	m.engine.streaming = true
	m.viewport.activeTip = "Use /help to see all commands."

	rows := m.liveRows(80)
	if len(rows) < 2 {
		t.Fatalf("expected at least 2 rows (spinner + tip), got %d rows: %v", len(rows), rows)
	}
	tipRow := diffaction.StripANSI(rows[1])
	if !strings.Contains(tipRow, "└ Tip: Use /help to see all commands.") {
		t.Fatalf("expected tip row to contain '└ Tip: ...', got %q", tipRow)
	}

	// Narrow truncation
	narrowRows := m.liveRows(18)
	if len(narrowRows) < 2 {
		t.Fatalf("expected 2 rows on narrow terminal")
	}
	narrowTip := diffaction.StripANSI(narrowRows[1])
	if ansi.StringWidth(narrowTip) > 18 || !strings.HasSuffix(narrowTip, "...") {
		t.Fatalf("expected tip to be truncated to <=18 with '...', got %q (width=%d)", narrowTip, ansi.StringWidth(narrowTip))
	}

	// Turn done clears tip
	m.Update(toolsteps.TurnDoneMsg{})
	if m.viewport.activeTip != "" {
		t.Fatalf("expected activeTip to be cleared after turn completion, got %q", m.viewport.activeTip)
	}
}

func TestTUITools_BatchGrouping_WithIntermediateText(t *testing.T) {
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.Update(tea.WindowSizeMsg{Width: 90, Height: 30})

	// Start two consecutive tool calls in batch 1
	step1Run := toolsteps.NewRunningStep("run_command", json.RawMessage(`{"CommandLine":"echo one"}`))
	m.Update(toolsteps.ToolRunningMsg{Step: step1Run})
	step1Done := toolsteps.NewToolStep("run_command", json.RawMessage(`{"CommandLine":"echo one"}`), "Output:\none\n", false)
	m.Update(toolsteps.ToolStepMsg{Step: step1Done})

	step2Run := toolsteps.NewRunningStep("run_command", json.RawMessage(`{"CommandLine":"echo two"}`))
	m.Update(toolsteps.ToolRunningMsg{Step: step2Run})
	step2Done := toolsteps.NewToolStep("run_command", json.RawMessage(`{"CommandLine":"echo two"}`), "Output:\ntwo\n", false)
	m.Update(toolsteps.ToolStepMsg{Step: step2Done})

	// In collapsed mode, batch 1 must be folded into a single row
	contentCollapsed := diffaction.StripANSI(strings.Join(m.engine.response, "\n"))
	if !strings.Contains(contentCollapsed, "Ran 2 commands (echo one · echo two) (ctrl+o to expand)") {
		t.Fatalf("expected collapsed summary for 2 commands, got:\n%s", contentCollapsed)
	}

	// Expand: should show both commands tightly packed without blank lines between them
	m.Update(ctrlKey('o'))
	contentExpanded := diffaction.StripANSI(strings.Join(m.engine.response, "\n"))
	if !strings.Contains(contentExpanded, "echo one") || !strings.Contains(contentExpanded, "echo two") {
		t.Fatalf("expected expanded view to list both commands:\n%s", contentExpanded)
	}

	// Check contiguous lines: no blank lines between command items
	lines := strings.Split(contentExpanded, "\n")
	hasBlankBetweenTools := false
	inToolBlock := false
	for _, l := range lines {
		if strings.Contains(l, "Ran 2 commands") {
			inToolBlock = true
			continue
		}
		if inToolBlock && strings.TrimSpace(l) == "" {
			hasBlankBetweenTools = true
			break
		}
	}
	if hasBlankBetweenTools {
		t.Fatalf("unexpected blank line within expanded tool block:\n%s", contentExpanded)
	}

	// Collapse back
	m.Update(ctrlKey('o'))

	// Assistant outputs intermediate explanation text
	m.Update(backendChunkMsg("I completed the checks. Now running final step."))

	// Tool 3 runs after text (batch break)
	step3Run := toolsteps.NewRunningStep("run_command", json.RawMessage(`{"CommandLine":"echo three"}`))
	m.Update(toolsteps.ToolRunningMsg{Step: step3Run})
	step3Done := toolsteps.NewToolStep("run_command", json.RawMessage(`{"CommandLine":"echo three"}`), "Output:\nthree\n", false)
	m.Update(toolsteps.ToolStepMsg{Step: step3Done})

	contentAfterBreak := diffaction.StripANSI(strings.Join(m.engine.response, "\n"))
	// Batch 1 summary should remain above the text
	idxBatch1 := strings.Index(contentAfterBreak, "Ran 2 commands")
	idxText := strings.Index(contentAfterBreak, "I completed the checks")
	idxBatch2 := strings.Index(contentAfterBreak, "Ran (echo three)")
	if idxBatch1 < 0 || idxText < 0 || idxBatch2 < 0 {
		t.Fatalf("missing expected elements after break:\n%s", contentAfterBreak)
	}
	if !(idxBatch1 < idxText && idxText < idxBatch2) {
		t.Fatalf("expected order Batch1 < Text < Batch2, got indices %d, %d, %d:\n%s", idxBatch1, idxText, idxBatch2, contentAfterBreak)
	}
}
