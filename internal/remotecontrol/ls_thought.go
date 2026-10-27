// Package remotecontrol — ls_thought.go: the model's thinking, split out
// of the answer text and carried on the trajectory step the Web UI
// renders as the collapsible "Thought for Ns" panel.
//
// The Web UI's planner-response renderer reads the thinking off the
// trajectory step:
//
//	rawThinking || thinking   → the thought text
//	thinkingDuration.seconds  → the "Thought for Ns" label
//
// and it renders nothing at all when the text is empty — the frontend's
// `jLb` component returns null for an empty `thinking`. The clone's step
// carried only `response`, so the panel never appeared.
//
// Where the thinking comes from: the CloudCode transport wraps a model's
// reasoning in `<think>…</think>` markers (internal/backend/cloudcode_stream.go:
// thinkOpen / thinkClose). The reference's own CLI strips that block before
// it prints the answer; the Web UI shows it in a collapsible panel. This
// file performs the same split, and measures how long the thinking took.
package remotecontrol

import (
	"strings"
	"time"
)

// The marker literals the CloudCode transport wraps reasoning in.
const (
	lsThinkOpen  = "<think>"
	lsThinkClose = "</think>"
)

// lsThinkScanner splits a streamed text into the visible part and the
// thinking part.
//
// The scanner holds back the tail of a chunk when the tail could be the
// start of a marker: the transport splits a single `<think>…</think>`
// block across many chunks, so a per-chunk match would miss the boundary
// entirely.
type lsThinkScanner struct {
	pending   string // bytes held back: a possible partial marker
	inThink   bool   // inside a `<think>…</think>` block
	justAfter bool   // just left a think block: skip the leading blanks
	thought   strings.Builder
	startAt   time.Time // when the think block opened
	endAt     time.Time // when the think block closed
	firstAt   time.Time // when the first thinking text arrived
	lastAt    time.Time // when the last thinking text arrived
	onThought func(string)
}

// Push feeds one text delta into the scanner and returns the visible part
// of it. The thinking part is kept by the scanner.
func (s *lsThinkScanner) Push(text string) string {
	if text == "" {
		return ""
	}
	buf := s.pending + text
	s.pending = ""

	var out strings.Builder
	for {
		if s.inThink {
			idx := strings.Index(buf, lsThinkClose)
			if idx < 0 {
				hold := lsMarkerPrefix(buf, lsThinkClose)
				s.addThought(buf[:len(buf)-len(hold)])
				s.pending = hold
				return out.String()
			}
			s.addThought(buf[:idx])
			buf = buf[idx+len(lsThinkClose):]
			s.inThink = false
			s.justAfter = true
			s.endAt = time.Now()
			continue
		}
		idx := strings.Index(buf, lsThinkOpen)
		if idx < 0 {
			hold := lsMarkerPrefix(buf, lsThinkOpen)
			out.WriteString(s.visible(buf[:len(buf)-len(hold)]))
			s.pending = hold
			return out.String()
		}
		out.WriteString(s.visible(buf[:idx]))
		buf = buf[idx+len(lsThinkOpen):]
		s.inThink = true
		s.startAt = time.Now()
	}
}

// visible applies the "no blank line after the think block" rule: the
// reference's CLI drops the whitespace that follows a think block, so the
// answer does not start with a paragraph break.
func (s *lsThinkScanner) visible(text string) string {
	if !s.justAfter {
		return text
	}
	trimmed := strings.TrimLeft(text, " \t\n\r")
	if trimmed == "" {
		return ""
	}
	s.justAfter = false
	return trimmed
}

// addThought records thinking text and the time it arrived.
func (s *lsThinkScanner) addThought(text string) {
	if text == "" {
		return
	}
	now := time.Now()
	if s.firstAt.IsZero() {
		s.firstAt = now
	}
	s.lastAt = now
	s.thought.WriteString(text)
	if s.onThought != nil {
		s.onThought(text)
	}
}

// Finish flushes the tail the scanner held back at the end of the stream.
// A tail that was inside a think block is thinking, not text: the
// reference's CLI shows a truncated think block as plain text, but a Web
// UI message must not carry a dangling `<think>` marker, so the bytes stay
// in the thought.
func (s *lsThinkScanner) Finish() string {
	pending := s.pending
	s.pending = ""
	if s.inThink {
		s.addThought(pending)
		return ""
	}
	return pending
}

// Thought returns the accumulated thinking text.
func (s *lsThinkScanner) Thought() string {
	return strings.TrimSpace(s.thought.String())
}

// Duration returns how long the thinking took. It is the span between the
// think block opening and closing; a block that never closed falls back to
// the span between the first and the last thinking chunk.
func (s *lsThinkScanner) Duration() time.Duration {
	if !s.startAt.IsZero() && !s.endAt.IsZero() {
		return s.endAt.Sub(s.startAt)
	}
	if !s.firstAt.IsZero() && !s.lastAt.IsZero() {
		return s.lastAt.Sub(s.firstAt)
	}
	return 0
}

// lsMarkerPrefix returns the longest suffix of s that could be the start
// of marker, or "" when there is none.
func lsMarkerPrefix(s, marker string) string {
	max := len(marker) - 1
	if max > len(s) {
		max = len(s)
	}
	for n := max; n > 0; n-- {
		if strings.HasPrefix(marker, s[len(s)-n:]) {
			return s[len(s)-n:]
		}
	}
	return ""
}

// lsStripThink removes the think blocks from a complete text and trims the
// result. The turn's reply is assembled by the backend from the raw
// deltas, so it still carries the markers the sink split out.
func lsStripThink(text string) string {
	sc := &lsThinkScanner{}
	return strings.TrimSpace(sc.Push(text) + sc.Finish())
}

// lsThoughtSink wraps an EventSink and keeps the model's thinking out of
// the visible text. It forwards the visible part of every text delta to the
// wrapped sink and remembers the thinking for the trajectory step.
//
// The thinking is also handed to the wrapped sink as it arrives, when
// the wrapped sink wants it: the Web UI shows the reasoning in a
// collapsible panel while the model is still thinking, so the text
// cannot wait for the end of the turn.
type lsThoughtSink struct {
	inner   EventSink
	sc      lsThinkScanner
	cascade *Cascade
}

// lsThoughtReceiver is the optional sink capability that receives the
// model's thinking as it arrives.
type lsThoughtReceiver interface {
	OnThought(cascade *Cascade, text string)
}

// newLSThoughtSink wraps inner.
func newLSThoughtSink(inner EventSink) *lsThoughtSink {
	s := &lsThoughtSink{inner: inner}
	s.sc.onThought = s.forwardThought
	return s
}

// forwardThought hands the thinking text to the wrapped sink when it
// wants it.
func (s *lsThoughtSink) forwardThought(text string) {
	receiver, ok := s.inner.(lsThoughtReceiver)
	if !ok {
		return
	}
	receiver.OnThought(s.cascade, text)
}

// OnState forwards the state frame.
func (s *lsThoughtSink) OnState(cascade *Cascade, frame StateFrame) {
	if s.inner != nil {
		s.inner.OnState(cascade, frame)
	}
}

// OnTextDelta splits the delta and forwards only its visible part.
func (s *lsThoughtSink) OnTextDelta(cascade *Cascade, delta string) {
	if s.inner == nil {
		return
	}
	s.cascade = cascade
	if visible := s.sc.Push(delta); visible != "" {
		s.inner.OnTextDelta(cascade, visible)
	}
}

// OnToolCall forwards the tool call.
func (s *lsThoughtSink) OnToolCall(cascade *Cascade, call ToolCall) {
	if s.inner != nil {
		s.inner.OnToolCall(cascade, call)
	}
}

// OnError forwards the error.
func (s *lsThoughtSink) OnError(cascade *Cascade, err error) {
	if s.inner != nil {
		s.inner.OnError(cascade, err)
	}
}

// OnUserInteraction forwards the interaction request.
func (s *lsThoughtSink) OnUserInteraction(cascade *Cascade, intr *PendingInteraction) {
	if s.inner != nil {
		s.inner.OnUserInteraction(cascade, intr)
	}
}

// Flush forwards the text the scanner held back at the end of the stream.
func (s *lsThoughtSink) Flush(cascade *Cascade) {
	if s.inner == nil {
		return
	}
	if tail := s.sc.Finish(); tail != "" {
		s.inner.OnTextDelta(cascade, tail)
	}
}

// Thought returns the thinking text and how long it took.
func (s *lsThoughtSink) Thought() (string, time.Duration) {
	return s.sc.Thought(), s.sc.Duration()
}
