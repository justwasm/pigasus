// cycle267_streaming_clear_test.go — cycle 267 unit + integration coverage
// for the streaming-chunks-clear fix.
//
// Bug, in one sentence: bubbletea v2 + ultraviolet's diff-based terminal
// renderer skips repainting any row whose new content has no cells touched,
// so when a turn transitions from the live-stream layout (chunks written
// via cursor-positioned writes at specific rows like banner-area row 9,
// announcement-Card-1 row 12, the "  Generating…" spinner row 14) back to
// the recovered glamour-rendered layout, those rows keep their old pixels:
//
//   - the spinner never clears because the new View() has no cells at
//     what used to be the spinner row → UV's transformLine doesn't even
//     visit that row → the "  Generating…" stays visible.
//   - the announcement Card 1 → Card 2 transition leaves Card 1's title
//     "Claude Opus 5.5" visible behind Card 2's response because the
//     row had old content but the new View() at the *same* row index
//     has different (response) content, and the diff only updates the
//     changed columns — old pixels past the new content's width stay.
//
// The fix (cmd/wagyu/tui_run.go — case turnDoneMsg and case backendDoneMsg):
// batch `tea.ClearScreen` into the returned Cmd so the next render does
// `s.scr.Erase()` → `clearUpdate(newbuf)` → a full-screen redraw driven
// by the freshly returned View(). Same shape as the cycle 99 / cycle 244
// turn-end Cmd shape; the only addition is `tea.ClearScreen`.
//
// These tests pin three rows of the matrix the user-visible bug
// observation documents:
//
//  1. streamPrompt's "happy path" turnDoneMsg returns a Cmd that, when
//     invoked, yields tea.ClearScreen()'s clearScreenMsg.
//  2. streamPrompt's error/empty-reply backendDoneMsg also returns
//     tea.ClearScreen — same diff-leaves-stale-pixels problem without
//     the glamour-replace (the spinner still lived somewhere).
//  3. queueMode Cmd+Enter flushes the queued turn via the same path,
//     so the clear is preserved across the queue flush (the cycle 244
//     queueCmd is batched with tea.ClearScreen so they ship together).
//
// The "is the actual screen clean?" assertion lives in the cycle-267
// PTY harness (docs/cycles/cycle-267/repro/wg01_pty_check.go), not here:
// these tests are headless — `tea.ClearScreen` is a bubbletea Cmd, not a
// concrete screen mutation, so all we can verify from a bare-metal
// in-process Update call is "yes, the returned Cmd, when executed, hands
// back a clearScreenMsg to the bubbletea message loop". The PTY harness
// is what proves the user-visible terminal state matches the recovered
// layout.
package main

import (
	"reflect"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/backend"
)

// TestTurnDoneMsg_StreamingClearReturnsClearScreenCmd pins headless row
// (1): streamPrompt returns turnDoneMsg with non-empty reply after
// streaming 5 backendChunkMsg deltas. m.Update(turnDoneMsg{...}) must
// return a Cmd whose invocation produces tea.ClearScreen()'s
// clearScreenMsg, so the bubbletea renderer does a full repaint on the
// next flush and clears the stale-pixels bug at the glamour-replace
// boundary.
//
// The test deliberately mirrors the cycle 266 test pattern
// (turn_empty_step_test.go:46) and reuses fakeClient so the assertion
// stays fast — no *tea.Program is involved.
func TestTurnDoneMsg_StreamingClearReturnsClearScreenCmd(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	// Mirror the streaming sequence the real streamPrompt goroutine
	// produces: prompt echo, then 5 raw-style chunks at 12-char
	// boundaries. replyStart is set on the first chunk so the
	// glamour-replace path engages.
	m.response = append(m.response, "> analyze pwd")
	m.liveStart = len(m.response)
	m.session = append(m.session, backend.Message{Role: "user", Content: "analyze pwd"})
	chunks := []string{"mock reply f", "rom model=Ge", "mini 3.8 Fla", "sh (Medium):", " analyze pwd"}
	for _, c := range chunks {
		_, _ = m.Update(backendChunkMsg(c))
	}
	if m.replyStart < 0 {
		t.Fatalf("replyStart not anchored after 5 chunks; m.replyStart=%d", m.replyStart)
	}

	_, cmd := m.Update(turnDoneMsg{
		reply:     "mock reply from model=Gemini 3.8 Flash (Medium): analyze pwd",
		steps:     nil,
		reasoning: "",
	})
	if cmd == nil {
		t.Fatalf("turnDoneMsg returned nil Cmd; want a tea.Batch(... ClearScreen) so the next render does a full repaint")
	}

	// tea.ClearScreen is itself a Cmd, so a single-element batch
	// collapses to it directly via compactCmds. We accept either a
	// direct tea.ClearScreen (batch with one cmd) or a BatchMsg
	// whose elements include tea.ClearScreen.
	got := execCmdForTest(cmd)
	if !isClearScreenMsg(got) {
		t.Fatalf("turnDoneMsg's Cmd did not yield tea.ClearScreen()'s clearScreenMsg; got=%T(%v)", got, got)
	}
}

// TestBackendDoneMsg_StreamingClearReturnsClearScreenCmd pins headless
// row (2): streamPrompt's empty-reply / tool-call-only / err branches
// all return backendDoneMsg (rather than turnDoneMsg). The case
// backendDoneMsg handler must also batch in tea.ClearScreen because
// the same diff-leaves-stale-pixels pathology applies to those paths
// too — the "  Generating…" spinner row, which is appended while
// streaming is true, sits at a row the post-View() never re-writes
// (View() drops the spinner row when m.streaming flips to false), so
// UV's transformLine ignores it and the spinner stays on screen.
func TestBackendDoneMsg_StreamingClearReturnsClearScreenCmd(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.response = append(m.response, "> hi")
	m.liveStart = len(m.response)
	m.streaming = true
	m.session = append(m.session, backend.Message{Role: "user", Content: "hi"})

	_, cmd := m.Update(backendDoneMsg{})
	if cmd == nil {
		t.Fatalf("backendDoneMsg returned nil Cmd; want tea.ClearScreen so the next render does a full repaint")
	}

	got := execCmdForTest(cmd)
	if !isClearScreenMsg(got) {
		t.Fatalf("backendDoneMsg's Cmd did not yield tea.ClearScreen()'s clearScreenMsg; got=%T(%v)", got, got)
	}
}

// TestTurnDoneMsg_QueueFlushKeepsClearScreen pins headless row (3):
// the cycle 244 queue-mode Cmd+Enter path flushes the queued user
// message into a fresh streamPrompt alongside the turnDoneMsg handler.
// The clear-screen Cmd must ship with the queue flush so the just-
// queued turn does not inherit a stale-pixel problem from the
// preceding turn's glamour-replace boundary. The batch collapses
// (queueCmd, tea.ClearScreen) via compactCmds when queueCmd is nil,
// and the (BatchMsg{queueCmd, tea.ClearScreen}) case keeps both
// intact when queueCmd is non-nil.
func TestTurnDoneMsg_QueueFlushKeepsClearScreen(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.response = append(m.response, "> first")
	m.liveStart = len(m.response)
	m.session = append(m.session, backend.Message{Role: "user", Content: "first"})

	// Queue the second prompt so the queue-mode flush engages.
	m.queuedPrompt = "second"

	_, cmd := m.Update(turnDoneMsg{
		reply:     "first reply",
		steps:     nil,
		reasoning: "",
	})
	if cmd == nil {
		t.Fatalf("turnDoneMsg with queuedPrompt returned nil Cmd; want a Batch that includes both queueCmd and tea.ClearScreen")
	}

	got := execCmdForTest(cmd)
	// The cmd's return type is tea.BatchMsg (because compactCmds sees
	// two non-nil cmds and wraps them). Iterate and assert one of the
	// batched sub-Cmds is tea.ClearScreen — proves the clear is not
	// lost when the queue flush is also wired.
	batch, ok := got.(tea.BatchMsg)
	if !ok {
		// Single non-nil cmd collapses to itself — but we expect two
		// cmds, so the path here would be a regression. Fail loud.
		t.Fatalf("turnDoneMsg with queuePrompt returned %T(%v); want tea.BatchMsg so both queueCmd and tea.ClearScreen fit", got, got)
	}
	foundClear := false
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if reflect.ValueOf(sub).Pointer() == reflect.ValueOf(tea.ClearScreen).Pointer() {
			foundClear = true
			break
		}
	}
	if !foundClear {
		t.Fatalf("BatchMsg %v does not include tea.ClearScreen; queue-mode flush will leak stale pixels from the preceding turn", batch)
	}
}

// TestTurnDoneMsg_StreamingClearContractIsStableAcrossResponseShapes
// is a regression net for the "different reply shapes produce different
// streaming patterns" axis. Every shape — empty reply, whitespace-only,
// short word, multi-line code, very long line that wraps — must still
// emit the tea.ClearScreen Cmd at turn-end. If a future cycle adds a
// fast-path that skips the glamour-replace (e.g. empty streaming) and
// forgets to clear, this catches it.
func TestTurnDoneMsg_StreamingClearContractIsStableAcrossResponseShapes(t *testing.T) {
	cases := []struct {
		name  string
		reply string
	}{
		{"empty", ""},
		{"whitespace", "   \n\t  "},
		{"single-word", "ok"},
		{"multi-line", "line 1\nline 2\nline 3"},
		{"long-line", "x" + repeat("y", 200)},
		{"code-fence", "```bash\necho hi\n```"},
		{"mixed-glamour", "# Heading\n\n- bullet 1\n- bullet 2\n\n**bold** here"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/", true)
			m.response = append(m.response, "> probe")
			m.liveStart = len(m.response)
			m.session = append(m.session, backend.Message{Role: "user", Content: "probe"})
			m.Update(backendChunkMsg(tc.reply))

			_, cmd := m.Update(turnDoneMsg{reply: tc.reply})
			if cmd == nil {
				t.Fatalf("%s: turnDoneMsg returned nil Cmd; want a Cmd that yields clearScreenMsg", tc.name)
			}
			got := execCmdForTest(cmd)
			if !isClearScreenMsg(got) && !batchContainsClearScreen(got) {
				t.Fatalf("%s: cmd produced %T(%v); want clearScreenMsg or BatchMsg containing tea.ClearScreen", tc.name, got, got)
			}
		})
	}
}

// execCmdForTest runs a Cmd synchronously and returns the Msg it
// produces. We use this to inspect what message bubbletea's main loop
// would receive on the next tick — proves "yes, this Cmd, when
// executed, hands the program back a clearScreenMsg". The Cmd must
// not block or panic when invoked in headless test context (the
// preview tea.ClearScreen does not block — it returns immediately
// with the empty struct).
func execCmdForTest(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

// isClearScreenMsg returns true if the message is the same shape
// tea.ClearScreen() produces. The exported surface area is `Msg`, but
// the concrete type inside bubbletea v2 is unexported. We use a
// reflect-based identity check: type name "clearScreenMsg" with empty
// fields, and same package.
func isClearScreenMsg(m tea.Msg) bool {
	if m == nil {
		return false
	}
	t := reflect.TypeOf(m)
	// bubbletea v2 defines `type clearScreenMsg struct{}` inside
	// package tea. The unexported name normalises via reflect to
	// "clearScreenMsg" (lowercase canonical form Go uses).
	if t.Name() == "clearScreenMsg" && t.PkgPath() == "charm.land/bubbletea/v2" {
		return true
	}
	return false
}

// batchContainsClearScreen handles the two-cmd BatchMsg shape: when
// both queueCmd and tea.ClearScreen are non-nil, the returned Msg is a
// BatchMsg[2] (per compactCmds). Check the slice for one Cmd whose
// function-pointer matches tea.ClearScreen.
func batchContainsClearScreen(m tea.Msg) bool {
	batch, ok := m.(tea.BatchMsg)
	if !ok {
		return false
	}
	clearPtr := reflect.ValueOf(tea.ClearScreen).Pointer()
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if reflect.ValueOf(sub).Pointer() == clearPtr {
			return true
		}
	}
	return false
}

// repeat is a tiny stdlib-only helper for the long-line case — keeps
// the test free of the strings package's Repeat to avoid the import
// churn just for one call.
func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
