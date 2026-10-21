package main

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
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/mdrender"
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
	m := NewRunMode(be, "u@t", "m", "t", "/tmp", true)
	m.EnableTools([]backend.ToolDecl{{Name: "run_command"}}, runner)

	msg := m.streamPrompt("what time is it")()
	done, ok := msg.(turnDoneMsg)
	if !ok {
		t.Fatalf("expected turnDoneMsg, got %T", msg)
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
	// turn's surface even when m.cwd / auth home services fail to
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
	if len(done.steps) != 1 {
		t.Fatalf("expected 1 step, got %+v", done.steps)
	}
	if done.steps[0].title != "Bash(date)" {
		t.Fatalf("unexpected step title %q", done.steps[0].title)
	}
	if !strings.Contains(done.reply, "It is now.") {
		t.Fatalf("final reply lost: %q", done.reply)
	}
}

// TestTUITools_NoRunnerIsTextOnly guards that a session without a runner
// behaves like the pre-cycle-99 TUI: text only, no panic.
func TestTUITools_NoRunnerIsTextOnly(t *testing.T) {
	be := &scriptedTUIClient{turns: [][]backend.StreamChunk{
		{{Text: "hello"}, {Done: true}},
	}}
	m := NewRunMode(be, "u@t", "m", "t", "/tmp", true)
	msg := m.streamPrompt("hi")()
	done, ok := msg.(turnDoneMsg)
	if !ok {
		t.Fatalf("expected turnDoneMsg, got %T", msg)
	}
	if len(done.steps) != 0 {
		t.Fatalf("unexpected steps: %+v", done.steps)
	}
	if done.reply != "hello" {
		t.Fatalf("reply = %q", done.reply)
	}
}

// TestTUITools_CtrlOTogglesStepDetail pins the expand/collapse behaviour and
// the recovered hint strings.
func TestTUITools_CtrlOTogglesStepDetail(t *testing.T) {
	m := NewRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.response = append(m.response, "> run date")
	m.liveStart = len(m.response)
	_, _ = m.Update(turnDoneMsg{
		reply: "It is now.",
		steps: []toolStep{{
			name:   "run_command",
			title:  "Ran date",
			detail: "Ran in the sandbox\nCommand output:\nFri Oct 16 06:11:50 CST 2026",
		}},
	})

	joined := strings.Join(m.response, "\n")
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
	joined = strings.Join(m.response, "\n")
	if !strings.Contains(joined, "06:11:50") {
		t.Fatalf("ctrl+o did not expand:\n%s", joined)
	}
	if strings.Contains(joined, "ctrl+o to expand") {
		t.Fatalf("collapsed hint still present after expanding:\n%s", joined)
	}

	_, _ = m.Update(ctrlKey('o'))
	joined = strings.Join(m.response, "\n")
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
// reply that arrives via turnDoneMsg must reach m.response through the same
// glamour-backed pipeline the resumed-conversation path uses (cycle 155 +
// cycle glamour-migration). Before this fix the cycle-99 turn-end handler
// appended msg.reply verbatim so a reply containing **bold**, lists, or
// fenced code blocks showed up in the transcript as raw markdown.
func TestTUITools_TurnDoneRendersMarkdown(t *testing.T) {
	m := NewRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.response = append(m.response, "> ask for a markdown answer")
	m.liveStart = len(m.response)
	_, _ = m.Update(turnDoneMsg{
		reply: "Here is **bold** and a list:\n\n- one\n- two",
	})
	joined := strings.Join(m.response, "\n")
	if strings.Contains(joined, "**bold**") {
		t.Fatalf("turnDoneMsg reply reached transcript as raw markdown:\n%s", joined)
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
// appended to m.response live (so the TUI scrolls as the model types);
// when the matching turnDoneMsg lands, the [replyStart, len(m.response))
// range is replaced with the glamour-rendered reply so the user sees the
// styled output without double-rendering (raw streamed + glamour'd
// appended). Pre-cycle 229 the TUI accumulated chunks silently in the
// agent loop's `full` buffer, never forwarding them to the tea.Program,
// so the user saw a frozen spinner until the turn finished.
func TestTUITools_LiveStreamReplacesGlamourAtTurnEnd(t *testing.T) {
	m := NewRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.response = append(m.response, "> stream markdown")
	m.liveStart = len(m.response)
	// Send three chunks; the body of m.response should contain the raw
	// streamed text immediately, anchored by replyStart.
	_, _ = m.Update(backendChunkMsg("Here is "))
	_, _ = m.Update(backendChunkMsg("**bold** "))
	_, _ = m.Update(backendChunkMsg("and a list."))
	startAfterChunks := len(m.response)
	if startAfterChunks <= m.liveStart {
		t.Fatalf("backendChunkMsg did not append to m.response: liveStart=%d len=%d",
			m.liveStart, startAfterChunks)
	}
	// Cycle 272: the live range is rendered on every chunk (it is
	// renderMarkdown(accumulated raw), the same function turnDoneMsg
	// calls), so the streamed text is already styled before
	// turnDoneMsg — the prose is present, the raw ** markers are not.
	liveVisible := stripANSI(strings.Join(m.response, "\n"))
	if !strings.Contains(liveVisible, "Here is bold and a list.") {
		t.Fatalf("streamed prose missing before turnDoneMsg:\n%s",
			strings.Join(m.response, "\n"))
	}
	if strings.Contains(liveVisible, "**bold**") {
		t.Fatalf("raw **bold** survived the mid-stream render:\n%s",
			strings.Join(m.response, "\n"))
	}
	if m.replyStart < m.liveStart || m.replyStart >= startAfterChunks {
		t.Fatalf("replyStart=%d not within live-stream range [%d,%d)",
			m.replyStart, m.liveStart, startAfterChunks)
	}
	// turnDoneMsg: the raw streamed text in [replyStart, len(m.response))
	// must be replaced (not appended next to) with the glamour'd version
	// of the SAME reply.
	reply := "Here is **bold** and a list."
	_, _ = m.Update(turnDoneMsg{reply: reply})
	// No double-render: the glamour'd text should not appear twice.
	joined := strings.Join(m.response, "\n")
	// Count occurrences of the bold-text sentinel: glamour escapes the
	// "bold" word with ESC[1m surrounding it, so an unrendered pass
	// leaves the literal "**bold**". After the replace, the literal must
	// be gone AND there must not be a second copy of the formatted form.
	if strings.Count(joined, "**bold**") != 0 {
		t.Fatalf("raw **bold** still present after turnDoneMsg (no replace):\n%s", joined)
	}
	// The transcript must contain the glamour ANSI bold escape exactly
	// where the streamed text used to live — i.e. within the replaced
	// range, not appended after it. We don't pin byte offsets, only that
	// it appears at least once.
	if !hasSGRBold(joined) {
		t.Fatalf("glamour bold attribute missing after replace:\n%s", joined)
	}
	if m.replyStart != -1 {
		t.Fatalf("replyStart not reset to -1 after turnDoneMsg: got %d", m.replyStart)
	}
	if m.liveReplyIdx != -1 {
		t.Fatalf("liveReplyIdx not reset to -1 after turnDoneMsg: got %d", m.liveReplyIdx)
	}
}

// TestTUITools_LiveStreamCoalescesSessionMessages pins cycle-229's
// "one assistant message per turn" semantics: multiple backendChunkMsgs
// for the same turn must accumulate into a single m.session entry (not
// N entries as cycle-99 did). The liveReplyIdx field tracks the
// in-progress assistant message; backendChunkMsg appends on the first
// chunk and updates in place on subsequent chunks.
func TestTUITools_LiveStreamCoalescesSessionMessages(t *testing.T) {
	m := NewRunMode(nil, "u@t", "m", "t", "/tmp", true)
	// Seed a user message by hand so we don't need ANTIGRAVITY_HOME +
	// submitPrompt to put the DB on disk. The point of the test is the
	// chunk→session coalescing, not submitPrompt's plumbing.
	m.session = append(m.session, backend.Message{Role: "user", Content: "ask"})
	if len(m.session) != 1 || m.session[0].Role != "user" {
		t.Fatalf("pre-chunk session = %+v, want 1 user msg", m.session)
	}
	_, _ = m.Update(backendChunkMsg("Hello"))
	_, _ = m.Update(backendChunkMsg(", "))
	_, _ = m.Update(backendChunkMsg("world!"))
	if len(m.session) != 2 {
		t.Fatalf("session grew to %d entries after 3 chunks, want 2: %+v",
			len(m.session), m.session)
	}
	if got := m.session[1]; got.Role != "assistant" || got.Content != "Hello, world!" {
		t.Fatalf("assistant message = %+v, want {assistant, \"Hello, world!\"}", got)
	}
	// turnDoneMsg overwrites the liveReply entry with the authoritative
	// msg.reply (rather than appending a duplicate).
	_, _ = m.Update(turnDoneMsg{reply: "Hello, world!"})
	if len(m.session) != 2 {
		t.Fatalf("turnDoneMsg appended a duplicate assistant entry, got %d messages: %+v",
			len(m.session), m.session)
	}
	if got := m.session[1].Content; got != "Hello, world!" {
		t.Fatalf("turnDoneMsg message = %q, want \"Hello, world!\"", got)
	}
}

// TestTUITools_LiveStreamCoalescesChunksIntoOneRow pins cycle-230's
// line discipline + cycle-232's mid-stream glamour: multiple
// backendChunkMsg updates with no '\n' boundary must end up in ONE
// row of m.response (not N as cycle-229 did), so a model that
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
// renderMarkdown function turnDoneMsg uses, on every chunk, from the
// raw accumulated reply — so a partially-typed row is rendered too
// (an unclosed **bold** shows literally, exactly as goldmark parses
// it). turnDoneMsg still swaps the live range for the authoritative
// single-copy render (no double-rendering).
func TestTUITools_LiveStreamCoalescesChunksIntoOneRow(t *testing.T) {
	m := NewRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.response = append(m.response, "> stream markdown")
	m.liveStart = len(m.response)
	// Three chunks, no '\n' anywhere. They must still coalesce into
	// one transcript row (cycle 230 discipline) — and cycle 272 means
	// that one row is already the rendered form of the accumulated
	// text, not the raw chunk concatenation.
	_, _ = m.Update(backendChunkMsg("Here's "))
	_, _ = m.Update(backendChunkMsg("**bold "))
	_, _ = m.Update(backendChunkMsg("text**."))
	if m.replyStart < m.liveStart {
		t.Fatalf("replyStart=%d not anchored (liveStart=%d)", m.replyStart, m.liveStart)
	}
	startedAt := m.replyStart
	if got, want := len(m.response), startedAt+1; got != want {
		t.Fatalf("3 same-line chunks produced %d rows, want %d: %+v",
			got-startedAt, want-startedAt, m.response[startedAt:])
	}
	// Cycle 272: the live row is renderMarkdown("Here's **bold
	// text**.") — the ** markers are already gone (styled to ANSI)
	// and the prose is present.
	if got := stripANSI(m.response[startedAt]); !strings.Contains(got, "Here's bold text.") {
		t.Fatalf("live row = %q, want visible text %q", got,
			"Here's bold text.")
	}
	if strings.Contains(stripANSI(m.response[startedAt]), "**") {
		t.Fatalf("live row still carries raw ** markers: %q", m.response[startedAt])
	}
	// A chunk ending in '\n' extends the same paragraph; glamour
	// renders the accumulated buffer, so the segment before the
	// newline lands on the row and the whole paragraph stays one
	// rendered row (no raw trailing slot to preserve).
	_, _ = m.Update(backendChunkMsg(" line2\n"))
	live := stripANSI(strings.Join(m.response[startedAt:], "\n"))
	if !strings.Contains(live, "Here's bold text. line2") {
		t.Fatalf("accumulated prose missing after \\n chunk:\n%s", live)
	}
	if strings.Contains(live, "**") {
		t.Fatalf("raw ** markers survived the mid-stream render:\n%s", live)
	}
	// And it carries ANSI styling — glamour styled the bold span.
	if !strings.Contains(strings.Join(m.response[startedAt:], "\n"), "\x1b[") {
		t.Fatalf("live rows carry no ANSI styling mid-stream:\n%s", live)
	}
	// turnDoneMsg glamour-replace: the reply "Here's **bold text**.
	// line2" must replace (not duplicate) the live range. The
	// substring must appear exactly once.
	_, _ = m.Update(turnDoneMsg{reply: "Here's **bold text**. line2"})
	joined := strings.Join(m.response, "\n")
	if n := strings.Count(stripANSI(joined), "Here's"); n != 1 {
		t.Fatalf("turnDoneMsg duplicated text, found %d copies of "+
			"%q:\n%s", n, "Here's", joined)
	}
	if n := strings.Count(stripANSI(joined), "line2"); n != 1 {
		t.Fatalf("turnDoneMsg duplicated text, found %d copies of %q:\n%s",
			n, "line2", joined)
	}
	if m.replyStart != -1 {
		t.Fatalf("replyStart not reset to -1 after turnDoneMsg: got %d", m.replyStart)
	}
	if m.liveReplyIdx != -1 {
		t.Fatalf("liveReplyIdx not reset to -1 after turnDoneMsg: got %d", m.liveReplyIdx)
	}
}

// TestTUITools_StreamedLinesRenderGlamour pins the cycle-232 mid-
// stream glamour surface: as each '\n'-bounded line completes (via
// a chunk-ending '\n' boundary), the cycle-232 backendChunkMsg
// handler runs that completed line through mdrender.LineANSI and
// replaces the raw row in m.response with the styled row. The user
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
// change), not just after turnDoneMsg as cycle-228 did. The test
// uses bold markdown markers because glamour only emits ANSI
// escapes for styled constructs — plain prose renders with
// padding-only (no escapes).
func TestTUITools_StreamedLinesRenderGlamour(t *testing.T) {
	m := NewRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.response = append(m.response, "> ask three chunks")
	m.liveStart = len(m.response)
	// Chunk 1 carries a trailing '\n' → it holds "**First bold**."
	// and cycle 272 renders it immediately.
	_, _ = m.Update(backendChunkMsg("**First bold**.\n"))
	// Cycle 285: the reference separates the prompt from the first
	// assistant message with one blank row, so the run starts at
	// liveStart+1.
	if m.replyStart != m.liveStart+1 || m.response[m.liveStart] != "" {
		t.Fatalf("replyStart=%d not anchored after the blank row at %d",
			m.replyStart, m.liveStart)
	}
	completedRow := m.response[m.replyStart]
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
	if strings.Contains(stripANSI(completedRow), "**First bold**") {
		t.Fatalf("completed row still carries raw ** markers: %q",
			completedRow)
	}
	// Chunk 2 extends the reply with a second paragraph containing a
	// complete bold span. Cycle 272 renders the accumulated buffer:
	// both the first paragraph and the new bold span are styled.
	_, _ = m.Update(backendChunkMsg("**Second** "))
	if got := m.response[m.replyStart]; !strings.Contains(got, "\x1b[") {
		t.Fatalf("row 0 lost glamour after chunk 2: %q", got)
	}
	if got := strings.TrimSpace(stripANSI(m.response[len(m.response)-1])); got != "Second" {
		t.Fatalf("trailing row after chunk 2 = %q, want %q",
			got, "Second")
	}
	// Chunk 3 extends the row; the now-unclosed `**` renders
	// literally ("partial third" stays as typed).
	_, _ = m.Update(backendChunkMsg("partial third"))
	if got := m.response[m.replyStart]; !strings.Contains(got, "\x1b[") {
		t.Fatalf("row 0 lost glamour after chunk 3: %q", got)
	}
	if got := strings.TrimSpace(stripANSI(m.response[len(m.response)-1])); got != "Second partial third" {
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
	m := NewRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.response = append(m.response, "> ask mid-stream")
	m.liveStart = len(m.response)
	_, _ = m.Update(backendChunkMsg("**bold partial"))
	// Cycle 285: the reference separates the prompt from the first
	// assistant message with one blank row, so the run starts at
	// liveStart+1.
	if m.replyStart != m.liveStart+1 || m.response[m.liveStart] != "" {
		t.Fatalf("replyStart=%d not anchored after the blank row at %d",
			m.replyStart, m.liveStart)
	}
	row := m.response[m.replyStart]
	// The unclosed ** renders literally: the visible glyphs are the
	// source's own, so nothing is silently dropped or mis-bolded.
	if got := strings.TrimSpace(stripANSI(row)); got != "**bold partial" {
		t.Fatalf("partial row visible text = %q, want %q", got,
			"**bold partial")
	}
	// Closing the span promotes it to real bold with the markers
	// gone — proving the partial is rendered through the same
	// pipeline and re-rendered when the construct completes.
	_, _ = m.Update(backendChunkMsg(" done**"))
	if strings.Contains(strings.Join(m.response[m.replyStart:], "\n"), "**") {
		t.Fatalf("closing chunk left raw ** markers:\n%s",
			strings.Join(m.response[m.replyStart:], "\n"))
	}
	if !hasSGRBold(strings.Join(m.response[m.replyStart:], "\n")) {
		t.Fatalf("closing chunk did not style the completed span:\n%s",
			strings.Join(m.response[m.replyStart:], "\n"))
	}
}

// TestTUITools_TurnDoneReplaceStillIdempotent is the cycle-232
// turnDoneMsg contract: after a streaming turn that mid-stream-
// styled its completed lines, turnDoneMsg produces a final m.response
// in which every line carries glamour styling (the partial that
// turnDoneMsg finally completes now also gets glamour). Crucially,
// no double-styling — the cycle-228 renderMarkdown(whole-reply)
// call that drives the turnDoneMsg replace runs exactly once on
// the full reply, so a styled row doesn't carry nested escapes.
//
// The cycle-228 toolBlockLines wipe earlier in the turnDoneMsg
// handler (replaceRange(liveStart, ..., block)) clears the mid-
// stream-styled rows, and the cycle-228 replaceRange(replyStart,
// ..., indented) re-inserts the cycle-228 renderMarkdown output via
// replaceRange's insert semantics. The mid-stream glamour pass is
// the *transient* visual the user sees during streaming; this
// replace is the *authoritative* final state. Cross-line constructs
// that mid-stream glamour rendered slightly off (an unclosed
// `**bold**` like the chunk sequence below) are re-styled cleanly.
func TestTUITools_TurnDoneReplaceStillIdempotent(t *testing.T) {
	m := NewRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.response = append(m.response, "> idempotent replace")
	m.liveStart = len(m.response)
	// Three chunks forming one soft-wrapped paragraph ("**bold
	// first**" + "*italic second*" + "trailing partial"). Cycle 272
	// renders the accumulated buffer on every chunk.
	_, _ = m.Update(backendChunkMsg("**bold first**\n"))
	_, _ = m.Update(backendChunkMsg("*italic second*\n"))
	_, _ = m.Update(backendChunkMsg("trailing partial"))
	// After these chunks the live range is already the rendered
	// form: styled (ANSI present), markers stripped, all text present.
	live := strings.Join(m.response[m.liveStart:], "\n")
	if !strings.Contains(live, "\x1b[") {
		t.Fatalf("live range carries no ANSI styling mid-stream:\n%q", live)
	}
	for _, expected := range []string{"bold first", "italic second", "trailing partial"} {
		if !strings.Contains(stripANSI(live), expected) {
			t.Fatalf("live range lost text %q mid-stream:\n%s", expected, stripANSI(live))
		}
	}
	if strings.Contains(stripANSI(live), "**") || strings.Contains(stripANSI(live), "*italic") {
		t.Fatalf("live range still carries raw markers:\n%s", stripANSI(live))
	}
	// Final reply covers all three lines (the trailing partial is
	// part of line 3). turnDoneMsg renders the whole reply via
	// renderMarkdown and replaces the live range.
	fullReply := "**bold first**\n*italic second*\ntrailing partial"
	_, _ = m.Update(turnDoneMsg{reply: fullReply})
	joined := strings.Join(m.response, "\n")
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
			t.Fatalf("turnDoneMsg lost text %q in joined output: "+
				"\n%s", expected, joined)
		}
	}
	// Cross-line construct check: the **bold first** and
	// *italic second* must each appear exactly once — no double-
	// styling. The cycle-228 renderMarkdown of the full reply
	// runs once, so each construct gets one styling. Counting
	// occurrences guards against gratuitous duplication.
	if n := strings.Count(joined, "bold first"); n != 1 {
		t.Fatalf("turnDoneMsg duplicated 'bold first' %d times: "+
			"double-styling detected? \n%s", n, joined)
	}
	// The literal `**bold first**` and `*italic second*` markers
	// must NOT survive the cycle-228 renderMarkdown pass — they
	// are styled to ANSI escapes during glamour.
	if strings.Contains(joined, "**bold first**") {
		t.Fatalf("turnDoneMsg left raw **bold** markers: %q", joined)
	}
	if strings.Contains(joined, "*italic second*") {
		t.Fatalf("turnDoneMsg left raw *italic* markers: %q", joined)
	}
	// And the joined output must carry ANSI escape sequences
	// (glamour's rendering output) — proves the cycle-228
	// renderMarkdown path did run, no harm done.
	if !strings.Contains(joined, "\x1b[") {
		t.Fatalf("turnDoneMsg did not produce any ANSI escapes: "+
			"renderMarkdown apparently skipped — cycle-228 "+
			"contract broken?\n%s", joined)
	}
	if m.replyStart != -1 {
		t.Fatalf("replyStart not reset to -1 after turnDoneMsg: got %d",
			m.replyStart)
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
	m := NewRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.response = append(m.response, "> cycle 242 cross-line bold")
	m.liveStart = len(m.response)

	// Chunks 1 + 2 emit a bold span split across a soft line break
	// ("**bold opens\nmore text**"). Cycle 272 renders the whole
	// accumulated buffer, so goldmark sees both halves in one pass and
	// styles them together — no blank-line boundary is required (this
	// is strictly stronger than the cycle-242 boundary path it
	// replaces).
	_, _ = m.Update(backendChunkMsg("**bold opens\n"))
	_, _ = m.Update(backendChunkMsg("more text**\n"))
	live := strings.Join(m.response[m.liveStart:], "\n")
	if !strings.Contains(live, "\x1b[") {
		t.Fatalf("cross-line bold produced no ANSI escapes:\n%s", live)
	}
	if !strings.Contains(stripANSI(live), "bold opens") || !strings.Contains(stripANSI(live), "more text") {
		t.Fatalf("cross-line bold content missing mid-stream:\n%s", stripANSI(live))
	}
	if strings.Contains(stripANSI(live), "**") {
		t.Fatalf("raw ** markers survived the mid-stream render:\n%s", stripANSI(live))
	}

	// A following paragraph renders as its own block once the blank
	// line arrives, and stays rendered as more text streams in.
	_, _ = m.Update(backendChunkMsg("\n"))
	_, _ = m.Update(backendChunkMsg("New paragraph."))
	live = stripANSI(strings.Join(m.response[m.liveStart:], "\n"))
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
	m := NewRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.response = append(m.response, "> do two things")
	m.liveStart = len(m.response)
	_, _ = m.Update(turnDoneMsg{
		reply: "done",
		steps: []toolStep{
			{name: "run_command", title: "Bash(date)", detail: "out-1"},
			{name: "view_file", title: "Read(secret.txt)", detail: "a\nb\nc\n"},
		},
	})
	joined := strings.Join(m.response, "\n")
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
	joined = strings.Join(m.response, "\n")
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

// TestGroupLabel pins the recovered category counts. Every word used here is a
// literal in the 1.3.6 string table ("1 file" / "%d files", "1 search" /
// "%d searches", "1 command" / "%d commands", …); the changelog records the fix
// ("now read \"Found 1 result\", \"1 match\", \"1 file\", and \"Wrote 1 line\"
// instead of \"1 results\").
//
// Cycle 178: a single-category group with ≥2 steps now also gets the
// recovered "(+N · <last-title>)" suffix that the reference appends to
// its group header (the last step's title appears verbatim between the
// "·" and the closing paren). The two-step run_command case below
// matches the recovered reference header exactly.
func TestGroupLabel(t *testing.T) {
	step := func(name, title string) toolStep {
		return toolStep{name: name, title: title}
	}
	label := func(steps []toolStep) string {
		verb, count, detail := groupSummary(steps)
		out := verb + " " + count
		if detail != "" {
			out += " " + detail
		}
		return out
	}
	for _, tc := range []struct {
		name  string
		steps []toolStep
		want  string
	}{
		{"one category, plural", []toolStep{
			step("view_file", "Read(a.go)"),
			step("view_file", "Read(b.go)"),
		}, "Explored 2 files (a.go · b.go)"},
		{"one edit", []toolStep{step("write_to_file", "Create(b.go)")}, "Edited 1 file (b.go)"},
		{"two edits", []toolStep{
			step("edit_file", "Edit(a.go)"),
			step("write_file", "Create(b.go)"),
		}, "Edited 2 files (a.go · b.go)"},
		{"one category, singular", []toolStep{step("view_file", "Read(a.go)")}, "Explored 1 file (a.go)"},
		{"one search", []toolStep{step("search_web", "WebSearch(TODO)")}, "Explored 1 search (TODO)"},
		{"two commands", []toolStep{
			step("run_command", "Bash(ls)"),
			step("run_command", "Bash(date)"),
		}, "Ran 2 commands (ls · date)"},
		{"two crons", []toolStep{
			step("schedule", "Scheduled foo"),
			step("schedule", "Scheduled bar"),
		}, "Ran 2 crons"},
		{"unknown tool", []toolStep{step("call_mcp", "Called mcp")}, "Ran 1 tool"},
		{"mixed order", []toolStep{
			step("run_command", "Bash(ls)"),
			step("view_file", "Read(a.go)"),
			step("search_web", "WebSearch(TODO)"),
		}, "Ran 1 file, 1 search, 1 command"},
		{"mixed repeated", []toolStep{
			step("view_file", "Read(a.go)"),
			step("view_file", "Read(b.go)"),
			step("search_web", "WebSearch(TODO)"),
		}, "Ran 2 files, 1 search"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := label(tc.steps); got != tc.want {
				t.Errorf("groupLabel(%s) = %q, want %q", tc.name, got, tc.want)
			}
		})
	}
}

// TestCountLabel pins the singular/plural rule directly.
func TestCountLabel(t *testing.T) {
	for _, tc := range []struct {
		n    int
		word [2]string
		want string
	}{
		{1, categoryWords["file"], "1 file"},
		{0, categoryWords["file"], "0 files"},
		{2, categoryWords["search"], "2 searches"},
		{1, categoryWords["command"], "1 command"},
		{5, categoryWords["step"], "5 steps"},
	} {
		if got := countLabel(tc.n, tc.word); got != tc.want {
			t.Errorf("countLabel(%d) = %q, want %q", tc.n, got, tc.want)
		}
	}
}

// TestToolCategory checks that every catalogued tool name maps onto a category
// that has a recovered singular/plural pair.
func TestToolCategory(t *testing.T) {
	for name, cat := range toolCategories {
		if _, ok := categoryWords[cat]; !ok {
			t.Errorf("tool %q maps to unknown category %q", name, cat)
		}
	}
	if got := toolCategory("no_such_tool"); got != "tool" {
		t.Errorf("toolCategory(no_such_tool) = %q, want tool", got)
	}
}

// TestTUITools_TruncatesLongDetail pins the recovered "<output +%d lines>"
// overflow marker.
func TestTUITools_TruncatesLongDetail(t *testing.T) {
	var lines []string
	for i := 0; i < maxStepDetailLines+7; i++ {
		lines = append(lines, "line")
	}
	got := strings.Join(toolStepLines(toolStep{
		name:   "run_command",
		title:  "Ran seq",
		detail: strings.Join(lines, "\n"),
	}, true, 0), "\n")
	if !strings.Contains(got, "<output +7 lines>") {
		t.Fatalf("overflow marker missing:\n%s", got)
	}
}

// TestTUITools_SpinnerNamesRunningTool pins the 1.4.3 running surface: the
// spinner line names the activity ("Running command...") and the transcript
// grows a provisional "● Running (…)" row that the finished step replaces.
func TestTUITools_SpinnerNamesRunningTool(t *testing.T) {
	m := NewRunMode(nil, "u@t", "m", "t", "/tmp", true)
	_, _ = m.Update(toolRunningMsg{step: newRunningStep("run_command", json.RawMessage(`{"CommandLine":"date"}`))})
	if m.activeTool != "Running command" || !m.streaming {
		t.Fatalf("spinner hint not set: %q streaming=%v", m.activeTool, m.streaming)
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
	_, _ = m.Update(toolStepMsg{step: newToolStep("run_command",
		json.RawMessage(`{"CommandLine":"date"}`), runCommandResult("0", "Mon Oct 20"), false)})
	joined := strings.Join(m.response, "\n")
	if strings.Contains(joined, "Running (date)") {
		t.Fatalf("provisional row survived completion:\n%s", joined)
	}
	if !strings.Contains(joined, "Ran") || !strings.Contains(joined, "(date)") {
		t.Fatalf("finished row missing:\n%s", joined)
	}
}

// TestTUITools_StepTitleVocabulary pins the recovered step-title verbs.
func TestTUITools_StepTitleVocabulary(t *testing.T) {
	cases := []struct {
		tool string
		args string
		want string
	}{
		{"run_command", `{"command":"date"}`, "Bash(date)"},
		{"view_file", `{"path":"a/b.txt"}`, "Read(a/b.txt)"},
		{"write_to_file", `{"path":"out.txt"}`, "Create(out.txt)"},
		{"replace_file_content", `{"path":"x.go"}`, "Edit(x.go)"},
		{"list_dir", `{"path":"src"}`, "Listed src"},
		{"grep_search", `{"query":"TODO"}`, "Searched TODO"},
		{"find_by_name", `{"path":"."}`, "Explored ."},
		{"search_web", `{"query":"golang"}`, "WebSearch(golang)"},
		{"ask_permission", `{}`, "Needs approval"},
		{"send_message", `{"message":"hi"}`, "Messaged hi"},
		{"call_mcp", `{"server":"fs"}`, "Called fs"},
		{"finish", `{}`, "Finished"},
		{"unknown_tool", `{}`, "unknown_tool"},
	}
	for _, tc := range cases {
		got := toolStepTitle(tc.tool, json.RawMessage(tc.args))
		if got != tc.want {
			t.Errorf("toolStepTitle(%q) = %q, want %q", tc.tool, got, tc.want)
		}
	}
}

// TestTUITools_FailedStepTitle pins the recovered "Failed" title for a
// non-zero-exit command.
//
// Cycle 157: the suffix is wrapped in cToolFailedSuffixOpen / Close
// (bold + red) and the bullet swaps to stepBulletErrOpen (red).
// The verb "Ran" stays in the cycle-157 verb palette (bold + orange).
func TestTUITools_FailedStepTitle(t *testing.T) {
	step := toolStep{
		name:    "run_command",
		title:   "Bash(false)",
		summary: "Ran (false)",
		detail:  "exit status 1",
		err:     true,
	}
	collapsed := strings.Join(toolStepLines(step, false, 0), "\n")
	if !strings.Contains(collapsed, "Ran") || !strings.Contains(collapsed, "false") || !strings.Contains(collapsed, "failed") {
		t.Fatalf("failed collapsed title missing:\n%s", collapsed)
	}
	if !strings.HasPrefix(collapsed, stepBulletErrOpen) {
		t.Fatalf("failed bullet should be red, got:\n%s", collapsed)
	}
	// The expanded call title stays clean, as in 1.4.3.
	expanded := strings.Join(toolStepLines(step, true, 0), "\n")
	if strings.Contains(expanded, "Failed") || strings.Contains(expanded, "failed") {
		t.Fatalf("expanded title should not carry a failure word:\n%s", expanded)
	}
}

// TestTUITools_ThoughtStepFromStream pins the cycle-100 behaviour: the model's
// reasoning is folded into a leading "Thought for Ns, N tokens" step instead of
// being silently discarded, the reasoning tokens come from the gateway usage
// block, and the visible reply is still the stripped answer (never raw
//
//	thinking text).
func TestTUITools_ThoughtStepFromStream(t *testing.T) {
	be := &scriptedTUIClient{turns: [][]backend.StreamChunk{
		{
			{Text: "\x3cthink\x3e17*23 is 391, let me verify.\x3c/think\x3e\n\n"},
			{Text: "The answer is 391."},
			{Done: true, Meta: &backend.ChunkMeta{ReasoningTokens: 24, CompletionTokens: 30}},
		},
	}}
	m := NewRunMode(be, "u@t", "m", "t", "/tmp", true)
	msg := m.streamPrompt("17*23?")()
	done, ok := msg.(turnDoneMsg)
	if !ok {
		t.Fatalf("expected turnDoneMsg, got %T", msg)
	}
	if strings.Contains(done.reply, "think") {
		t.Fatalf("raw think text leaked into reply: %q", done.reply)
	}
	if done.reply != "The answer is 391." {
		t.Fatalf("reply = %q", done.reply)
	}
	if len(done.steps) != 1 {
		t.Fatalf("expected 1 thought step, got %+v", done.steps)
	}
	st := done.steps[0]
	if st.kind != stepKindThought {
		t.Fatalf("step kind = %v; want thought", st.kind)
	}
	if st.tokens != 24 {
		t.Errorf("tokens = %d; want 24", st.tokens)
	}
	if !strings.HasPrefix(thoughtTitle(st.dur, st.tokens), "Thought for ") ||
		!strings.HasSuffix(thoughtTitle(st.dur, st.tokens), ", 24 tokens") {
		t.Errorf("thought title = %q", thoughtTitle(st.dur, st.tokens))
	}
	if !strings.Contains(st.detail, "391") {
		t.Errorf("thought detail lost: %q", st.detail)
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
	m := NewRunMode(be, "u@t", "m", "t", "/tmp", true)
	m.EnableTools([]backend.ToolDecl{{Name: "view_file"}}, runner)

	msg := m.streamPrompt("read a.txt")()
	done, ok := msg.(turnDoneMsg)
	if !ok {
		t.Fatalf("expected turnDoneMsg, got %T", msg)
	}
	if len(done.steps) != 2 {
		t.Fatalf("expected thought + tool, got %+v", done.steps)
	}
	if done.steps[0].kind != stepKindThought {
		t.Errorf("first step kind = %v; want thought", done.steps[0].kind)
	}
	if done.steps[1].kind != stepKindTool {
		t.Errorf("second step kind = %v; want tool", done.steps[1].kind)
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
	if got := thinkingLiveLine(31 * time.Second); got != wantBullet+" Thinking... (31s)" {
		t.Errorf("live frame = %q; want %q", got, wantBullet+" Thinking... (31s)")
	}
	if got := thinkingLiveLine(1500 * time.Millisecond); got != wantBullet+" Thinking... (1.5s)" {
		t.Errorf("live frame = %q", got)
	}

	// A completed thought renders as a collapsed step with the recovered
	// expand hint; ctrl+o reveals the reasoning.
	steps := []toolStep{{kind: stepKindThought, detail: "line one\nline two", dur: 2 * time.Second, tokens: 12}}
	collapsed := toolBlockLines(steps, false, 0)
	if len(collapsed) != 1 || !strings.Contains(collapsed[0], "Thought for 2s, 12 tokens") {
		t.Fatalf("collapsed = %+v", collapsed)
	}
	if !strings.Contains(collapsed[0], "ctrl+o to expand") {
		t.Fatalf("missing expand hint: %q", collapsed[0])
	}
	expanded := toolBlockLines(steps, true, 0)
	if len(expanded) != 3 || !strings.Contains(expanded[1], "line one") {
		t.Fatalf("expanded = %+v", expanded)
	}

	// thoughtStartMsg flips the live frame on.
	m := NewRunMode(nil, "u@t", "m", "t", "/tmp", true)
	if m.thinking {
		t.Fatal("thinking should start false")
	}
	m.Update(thoughtStartMsg{})
	if !m.thinking || m.thinkingSince.IsZero() {
		t.Fatal("thoughtStartMsg did not start the live frame")
	}
	m.Update(turnDoneMsg{})
	if m.thinking {
		t.Fatal("turnDoneMsg did not clear the live frame")
	}
}

// Cycle 127: diff-line colourisation. Leading `+` becomes green
// (cToolAddOpen), leading `-` becomes red (cToolRemoveOpen), and
// context lines pass through unchanged. The 200-byte cap is there to
// keep the styled segment bounded on long diff hunks.
func TestStyleDiffLine_AddAndRemove(t *testing.T) {
	added := styleDiffLine("+func main() {}")
	if !strings.Contains(added, cToolAddOpen+"func main() {}\x1b[39m") {
		t.Errorf("+ diff line missing add colour: %q", added)
	}
	removed := styleDiffLine("-func old() {}")
	if !strings.Contains(removed, cToolRemoveOpen+"func old() {}\x1b[39m") {
		t.Errorf("- diff line missing remove colour: %q", removed)
	}
	plain := styleDiffLine(" context")
	if strings.Contains(plain, cToolAddOpen) || strings.Contains(plain, cToolRemoveOpen) {
		t.Errorf("plain diff line should pass through: %q", plain)
	}
	long := "+" + strings.Repeat("a", 250)
	if got := styleDiffLine(long); got != long {
		t.Errorf("long line should be passed through unchanged, got %q...", got[:60])
	}
}

// Cycle 127: the step bullet now ships in brand blue.
//
// Cycle 156: the step bullet is U+25CF "●" in 256-colour 29 (deep
// teal/green), not U+23FA ⏺ in brand blue. The recovery reads the
// recovered byte sequence "\x1b[38;2;133;153;0m\u25cf\x1b[39m" from the
// v1.3.6 capture diff.
func TestToolBlockLines_StepBulletIsStyled(t *testing.T) {
	s := toolStep{name: "view_file", title: "Read secret.txt"}
	lines := toolBlockLines([]toolStep{s}, false, 0)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line, got %d", len(lines))
	}
	if !strings.HasPrefix(lines[0], cToolBulletOpen+"\u25cf"+cToolBulletClose+" ") {
		t.Errorf("collapsed step should lead with teal ● bullet, got %q", lines[0])
	}
}

// Cycle 127: expanded detail with a diff body uses coloured +/- prefixes.
//
// Cycle 178: the fixture must contain `@@` hunk markers (or both
// `--- ` / `+++ ` file headers) for looksLikeDiff to fire. The
// pre-cycle-178 fixture of "+added\n-removed\n context" without
// the markers no longer triggers the diff path — that's the whole
// point of cycle 178's tightened heuristic.
func TestStepDetailLines_ColoredDiff(t *testing.T) {
	s := toolStep{
		name:   "replace_file_content",
		title:  "Edit a.go",
		detail: "@@ -1,3 +1,3 @@\n+func NewFunc() {}\n-oldFunc()\n context",
	}
	lines := stepDetailLines(s, 0)
	if len(lines) != 4 {
		t.Fatalf("expected 4 lines (header + 3 body), got %d (%v)", len(lines), lines)
	}
	if !strings.Contains(lines[0], cDetailBulletOpen+"  \u23bf  ") {
		t.Errorf("first detail line should lead with the dim \u23bf prefix, got %q", lines[0])
	}
	if !strings.Contains(lines[1], cToolAddOpen+"func NewFunc() {}"+cToolAddClose) {
		t.Errorf("+ diff body should be green, got %q", lines[1])
	}
	if !strings.Contains(lines[2], cToolRemoveOpen+"oldFunc()"+cToolRemoveClose) {
		t.Errorf("- diff body should be red, got %q", lines[2])
	}
}

// Cycle 176: looksLikeMarkdown covers the unambiguous Markdown
// markers (paired-asterisk strong, fenced code, ATX heading,
// inline-link `[...](...)`). Cases that must NOT fire are also
// pinned: empty input, plain text, diff-only content (no Markdown
// markers).
//
// Cycle 178: `- ` and `* ` at line start now DO fire (cycle-178
// widening — the diff side has been tightened in lockstep, so
// the collision that kept cycle-176 from firing these is gone).
// The non-fire cases below are now: plain text, diff-only
// (without diff markers — looksLikeDiff handles those), and
// shell-output fragments that happen to start with `-`.
func TestLooksLikeMarkdown(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		// Markdown markers that DO fire.
		{"bold", "**important** thing", true},
		{"fence", "before\n```go\ncode\n```\nafter", true},
		{"heading at start", "# Heading\n\nbody", true},
		{"heading in body", "para\n\n# Heading\n\nmore", true},
		{"inline link", "see [docs](https://example.invalid/page)", true},
		{"fenced with lang", "```bash\n$ ls\n```", true},
		// Cycle 178 additions.
		{"dash bullet", "- foo\n- bar\n- baz", true},
		{"dash bullet at start", "- foo", true},
		{"star bullet", "* foo\n* bar", true},
		// Non-Markdown that must NOT fire.
		{"empty", "", false},
		{"plain text", "the API endpoint is down", false},
		{"diff only no markers", "+added\n-removed\n context", false},
		{"hash but not heading", "127.0.0.1 localhost", false},
		{"single asterisk italic", "*foo* bar", false},
		{"link bracket alone", "see [docs]", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := looksLikeMarkdown(c.in); got != c.want {
				t.Errorf("looksLikeMarkdown(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// Cycle 178: looksLikeDiff covers the unified-diff markers that
// gate the cycle-127 per-line diff colouring pass. Cases that
// fire: real diff hunk markers (`@@` at line start), both
// `--- ` and `+++ ` file headers. Cases that must NOT fire:
// content with `- ` bullet rows (Markdown), single `-` / `+`
// lines without diff context (tool output), thematic break
// `---` alone (Markdown), and shell arithmetic `+++` alone.
func TestLooksLikeDiff(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want bool
	}{
		// Fire cases.
		{"hunk at start", "@@ -1,3 +1,3 @@\n+added\n-removed", true},
		{"hunk mid body", "before\n@@ -1,3 +1,3 @@\n+added\n-removed", true},
		{"both file headers", "--- a/foo.txt\n+++ b/foo.txt\n+added\n-removed", true},
		{"minimal diff", "--- a\n+++ b\n@@ -1 +1 @@\n-old\n+new", true},
		// Non-fire cases.
		{"empty", "", false},
		{"plain text", "the api endpoint is down", false},
		{"bullet only", "- foo\n- bar", false},
		{"dash only", "--- a section heading", false},
		{"plus only", "+++ counter", false},
		{"thematic break", "before\n\n---\n\nafter", false},
		{"single plus line", "+added\n context", false},
		{"single minus line", "-removed\n context", false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := looksLikeDiff(c.in); got != c.want {
				t.Errorf("looksLikeDiff(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// Cycle 178: a `cat .gitignore`-style payload (just `- pattern`
// rows, no other markers) does NOT trip the diff path. The
// spurious cycle-127 red colouring that cycle 178 documented as
// "a pre-existing quirk" is gone. The output is byte-identical
// to plain-text tool output: `- pattern` rows render verbatim,
// with no ANSI colouring.
func TestStepDetailLines_NoSpuriousDiffColoringOnGitignore(t *testing.T) {
	s := toolStep{
		name:   "read_url_content",
		title:  "Read .gitignore",
		detail: "node_modules/\n*.log\n.env\n",
	}
	lines := stepDetailLines(s, 0)
	joined := strings.Join(lines, "\n")
	if strings.Contains(joined, cToolRemoveOpen) || strings.Contains(joined, cToolAddOpen) {
		t.Errorf("non-diff content should not have +/- colour escapes:\n%s", joined)
	}
	if strings.Contains(joined, cToolRemoveClose) || strings.Contains(joined, cToolAddClose) {
		t.Errorf("non-diff content should not have +/- close escapes:\n%s", joined)
	}
	// Sanity: the content is preserved verbatim.
	if !strings.Contains(joined, "node_modules/") {
		t.Errorf("node_modules/ missing:\n%s", joined)
	}
	if !strings.Contains(joined, "*.log") {
		t.Errorf("*.log missing:\n%s", joined)
	}
}

// Cycle 176: tool-result content that contains Markdown markers
// gets rendered through mdrender. The transcript rows show the
// bold / italic / heading styling instead of the raw markers.
//
// Cycle glamour-migration: bold spans now ship with glamour's
// closing escape (ESC[m = full style reset, vs the cycle-175
// walker which closed with ESC[22m to keep the bold attribute
// off only). The structural property — bold-on around the
// emphasised span, no raw markers surviving — is preserved.
func TestStepDetailLines_RendersMarkdown(t *testing.T) {
	s := toolStep{
		name:   "read_url_content",
		title:  "Read README.md",
		detail: "# Project\n\nThis is **important** text.\n\n- step one\n- step two\n",
	}
	lines := stepDetailLines(s, 0)
	if len(lines) == 0 {
		t.Fatal("stepDetailLines returned no rows for markdown input")
	}
	joined := strings.Join(lines, "\n")
	// Bold span: glamour emits the document colour and the bold
	// attribute in one SGR (cycle 271) and closes with ESC[m.
	if !hasSGRBold(joined) || !strings.Contains(joined, "important") {
		t.Errorf("bold span opening missing:\n%s", joined)
	}
	// Bullets are U+2022 round dots (AGY 1.4.2's Item.BlockPrefix,
	// cycle 271; the cycle-175 walker used U+2421).
	if !strings.Contains(joined, "\u2022 step one") {
		t.Errorf("bullet marker lost:\n%s", joined)
	}
	if !strings.Contains(joined, "\u2022 step two") {
		t.Errorf("second bullet marker lost:\n%s", joined)
	}
	// The raw markdown markers must NOT survive in the output.
	if strings.Contains(joined, "**important**") {
		t.Errorf("raw ** survived markdown rendering:\n%s", joined)
	}
	if strings.Contains(joined, "- step one") {
		t.Errorf("raw - survived bullet conversion:\n%s", joined)
	}
	// The detail bullet prefix is still emitted on the first row.
	if !strings.Contains(lines[0], detailBulletOpen) {
		t.Errorf("first row missing detail bullet:\n%s", lines[0])
	}
}

// Cycle 176: fenced code blocks from `cat <file>.md` get the
// 4-space indent and the ``` marker lines are stripped (cycle-127
// diff path doesn't apply because no `+` / `-` is at line start
// after mdrender runs).
//
// Cycle 233: a language-tagged fence ("```go") is now chroma-
// styled by the cycle-233 chroma Restore pass (see
// internal/mdrender/chroma_inline.go). The 4-space indent on each
// line of the rendered block survives chroma — the chroma output
// is wrapped through indentChromaBlock before insertion — so the
// shape of the result is the same as cycle 176/232, just with
// ANSI SGR escapes between tokens. The test strips ANSI before
// checking for "    func main()" to keep the structural pin.
func TestStepDetailLines_FencedCodeInMarkdown(t *testing.T) {
	s := toolStep{
		name:   "read_url_content",
		title:  "Read docs.md",
		detail: "before\n```go\nfunc main() {}\n```\nafter",
	}
	lines := stepDetailLines(s, 0)
	joined := strings.Join(lines, "\n")
	// Cycle 233: chroma wraps each token in ANSI escapes, so a
	// direct substring check for "    func main()" fails (the
	// ANSI codes break the literal sequence). Strip ANSI before
	// checking.
	plain := stripAnsiForTest(joined)
	if !strings.Contains(plain, "    func main()") {
		t.Errorf("code block not indented to 4 spaces:\n%s", joined)
	}
	if strings.Contains(joined, "```") {
		t.Errorf("fence marker survived:\n%s", joined)
	}
	if !strings.Contains(joined, "before") || !strings.Contains(joined, "after") {
		t.Errorf("surrounding prose lost:\n%s", joined)
	}
}

// stripAnsiForTest removes ANSI SGR sequences from s. Cycle 233
// added this helper because the cycle-176 step-detail code-fence
// test now has chroma in the path -- the chroma terminal16m
// formatter wraps each token in 24-bit RGB ANSI escapes, breaking
// literal substring matching for "    func main()".
func stripAnsiForTest(s string) string {
	var b strings.Builder
	i := 0
	for i < len(s) {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			// skip until 'm' (SGR terminator)
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				i = j + 1
				continue
			}
		}
		b.WriteByte(s[i])
		i++
	}
	return b.String()
}

// Cycle 176: plain-text tool output (no Markdown markers) is
// unchanged from cycle-127 — the heuristic returns false and the
// diff-style pass still applies to `+` / `-` lines. Cycle 178 also
// widened the first-line spacing from one space after the bullet to
// two ("  ⎿  content" vs the cycle-127 "  ⎿ content") so the
// recovered reference layout matches.
func TestStepDetailLines_PlainTextUnchanged(t *testing.T) {
	s := toolStep{
		name:   "run_command",
		title:  "Ran cat /etc/hostname",
		detail: "myhost.local\n",
	}
	lines := stepDetailLines(s, 0)
	if len(lines) != 1 {
		t.Fatalf("expected 1 line for single-line plain text, got %d", len(lines))
	}
	want := detailBulletOpen + "myhost.local"
	if lines[0] != want {
		t.Errorf("plain-text detail byte mismatch:\n  got: %q\n want: %q", lines[0], want)
	}
}

// Cycle 176: an empty / whitespace-only detail returns nil, same
// as cycle-127 (no Markdown rendering happens because the
// stepDetailLines early-return fires before the heuristic).
func TestStepDetailLines_EmptyPassthrough(t *testing.T) {
	for _, body := range []string{"", "   ", "\n\n", " \t\n"} {
		s := toolStep{name: "x", title: "y", detail: body}
		if got := stepDetailLines(s, 0); got != nil {
			t.Errorf("stepDetailLines(detail=%q) = %v, want nil", body, got)
		}
	}
}

// Cycle 178: bullet-only Markdown (e.g. `cat .gitignore` payload,
// or `cat TODO`) now trips the cycle-178-widened looksLikeMarkdown
// heuristic (because `- ` at line start is now a Markdown
// marker). The body is rendered through mdrender so `- foo`
// becomes `␡ foo`, and the cycle-127 diff colouring does NOT run
// (looksLikeDiff is false — no `@@` or `---`/`+++`).
//
// This is the deliberate cycle-178 trade: the spurious red colouring
// is gone (correct), and the bullet rows render as round-dot
// Markdown bullets (also correct for any payload that genuinely is
// a bulleted list).
func TestStepDetailLines_BulletOnlyRendersAsMarkdown(t *testing.T) {
	s := toolStep{
		name:   "read_url_content",
		title:  "Read .gitignore",
		detail: "- foo\n- bar\n",
	}
	lines := stepDetailLines(s, 0)
	// Cycle glamour-migration: glamour wraps the source with a
	// leading blank line ("\n\n" before the first bullet) that
	// the legacy walker did not. The two real bullet rows are
	// at indices 1..2; the leading blank or whitespace row may
	// also be present. Pin >= 2 lines and verify the bullets
	// are present somewhere in the rendered set, rather than
	// asserting a strict count.
	if len(lines) < 2 {
		t.Fatalf("expected at least 2 lines, got %d (%v)", len(lines), lines)
	}
	joined := strings.Join(lines, "\n")
	// The `- foo` line must have been converted to `• foo` by
	// the glamour Item.BlockPrefix config (cycle 271 marker).
	if !strings.Contains(joined, "\u2022 foo") {
		t.Errorf("bullet-only line should now get Markdown bullet conversion:\n%s", joined)
	}
	// The cycle-127 red diff colour must NOT survive anywhere
	// in the rendered set — that was the cycle-127 collision
	// bug cycle 178 fixed.
	if strings.Contains(joined, cToolRemoveOpen) {
		t.Errorf("bullet-only line should NOT get cycle-127 red diff colour (cycle-178 trade):\n%s", joined)
	}
}

// Cycle 176: real `replace_file_content` diff output (with
// hunk / file headers) does NOT trip the Markdown heuristic —
// `@@` is not a Markdown marker, `---` / `+++` files are not
// Markdown markers, and the diff body has no `**` / ``` /
// `# ` / `[...](...)` content. The cycle-127 path renders the
// +/- colour and the user's snapshot expectations continue to
// hold for real diffs.
func TestStepDetailLines_RealDiffUnchanged(t *testing.T) {
	s := toolStep{
		name:   "replace_file_content",
		title:  "Edit foo.go",
		detail: "--- a/foo.go\n+++ b/foo.go\n@@ -1,3 +1,3 @@\n-foo\n+bar\n context\n",
	}
	lines := stepDetailLines(s, 0)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, cToolAddOpen+"bar"+cToolAddClose) {
		t.Errorf("real diff +line lost green colour:\n%s", joined)
	}
	if !strings.Contains(joined, cToolRemoveOpen+"foo"+cToolRemoveClose) {
		t.Errorf("real diff -line lost red colour:\n%s", joined)
	}
	// The raw diff syntax must NOT be mdrender'd — `--- ` /
	// `+++ ` / `@@` are not Markdown markers.
	if strings.Contains(joined, "\u2421 a/foo.go") {
		t.Errorf("diff header got bullet conversion (regression):\n%s", joined)
	}
}

// TestSpinnerText_MatchesReferenceFrames pins the reference's animated
// activity label. Each frame below is a (base, highlight) split measured
// from a truecolour pty capture of agy 1.4.3 reading a file:
//
//	R|eadin  Re|ading  Rea|ding    …  Reading| file  Reading |file.
//	Reading f|ile..  Reading fi|le...  Reading file  Reading file.  …
//
// ("|" marks the #268BD2 highlight boundary and is not emitted.)
func TestSpinnerText_MatchesReferenceFrames(t *testing.T) {
	split := func(base, hi string) string {
		return cSpinnerBaseOpen + base + cSpinnerHiOpen + hi + "\x1b[m"
	}
	for _, tc := range []struct {
		frame int
		want  string
	}{
		{0, split("R", "eadin")},
		{1, split("Re", "ading")},
		{6, split("Reading", " file")},
		{7, split("Reading ", "file.")},
		{9, split("Reading fi", "le...")},
		{10, cSpinnerBaseOpen + "Reading file" + "\x1b[m"},
		{11, cSpinnerBaseOpen + "Reading file." + "\x1b[m"},
		{13, cSpinnerBaseOpen + "Reading file..." + "\x1b[m"},
	} {
		if got := spinnerText("Reading file", tc.frame); got != tc.want {
			t.Errorf("spinnerText(Reading file, %d) = %q, want %q", tc.frame, got, tc.want)
		}
	}
}

// TestLiveRows_PostToolPhrase pins the two settled phrases: "Generating"
// before the turn has run a call and "Working" afterwards (both measured;
// see ref_view.bin / ref_queue.bin captures).
func TestLiveRows_PostToolPhrase(t *testing.T) {
	m := NewRunMode(nil, "u@t", "m", "t", "/tmp", true)
	m.streaming = true
	// Frames past the "typing" phase show the settled phrase.
	m.spinFrame = 9
	if got := strings.Join(m.liveRows(120), "\n"); !strings.Contains(got, "Generating") {
		t.Fatalf("pre-tool spinner label = %q; want Generating", got)
	}
	m.anyToolRun = true
	m.spinFrame = 6
	if got := strings.Join(m.liveRows(120), "\n"); !strings.Contains(got, "Working") {
		t.Fatalf("post-tool spinner label = %q; want Working", got)
	}
	m.activeTool = "Running command"
	m.spinFrame = 15
	if got := strings.Join(m.liveRows(120), "\n"); !strings.Contains(got, "Running command") {
		t.Fatalf("in-call spinner label = %q; want Running command", got)
	}
}
