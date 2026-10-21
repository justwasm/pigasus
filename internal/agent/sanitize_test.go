// sanitize_test.go — cycle 50 sub-agent A: SanitizeText +
// Sanitizer unit tests. Both are pure (no I/O, no logging), so
// we test them directly without any fixture plumbing.
package agent

import (
	"strings"
	"testing"
)

// ---- SanitizeText (one-shot) -------------------------------------------

// TestSanitizeText_StripsSingleBlock is the headline byte-level
// check: a string of the form
//
//	"<think>reasoning</think>\n\nHi there!"
//
// must reduce to "Hi there!" — the exact text the v1.3.5 binary
// prints in `antigravity --print "say hi" 2>&1`.
func TestSanitizeText_StripsSingleBlock(t *testing.T) {
	in := "<think>The user is asking me to say hi. This is a simple greeting request...</think>\n\nHi there! 👋 I'm MiniMax-M3, an AI model developed by MiniMax. How can I help you today?"
	got := SanitizeText(in)
	if got != "Hi there! 👋 I'm MiniMax-M3, an AI model developed by MiniMax. How can I help you today?" {
		t.Errorf("SanitizeText = %q; want stripped answer only", got)
	}
}

// TestSanitizeText_PreservesPlainText is a no-op regression: a
// string with no think block must round-trip unchanged (modulo
// the trim we explicitly apply).
func TestSanitizeText_PreservesPlainText(t *testing.T) {
	in := "Hello, world!"
	if got := SanitizeText(in); got != "Hello, world!" {
		t.Errorf("SanitizeText plain = %q; want unchanged", got)
	}
}

// TestSanitizeText_StripsMultipleBlocks covers the case where the
// upstream emitter writes more than one think block (it does this
// when a subagent injects its own trace). The non-greedy `*?`
// in the regex must catch each block independently, not coalesce
// them.
//
// Note: the regex also strips the trailing whitespace after
// each `</think>` (see thinkBlockRe docs in sanitize.go), so
// the two spaces between "middle" and "end" (one before each
// think block) are both consumed. The result is "middle end"
// with a single space.
func TestSanitizeText_StripsMultipleBlocks(t *testing.T) {
	in := "<think>first</think> middle <think>second</think> end"
	got := SanitizeText(in)
	if got != "middle end" {
		t.Errorf("SanitizeText multi = %q; want %q", got, "middle end")
	}
}

// TestSanitizeText_HandlesNewlineSpanningBlocks covers the
// DOTALL case: the think block may contain newlines, and the
// regex must still strip the entire block.
func TestSanitizeText_HandlesNewlineSpanningBlocks(t *testing.T) {
	in := "before\n<think>step 1\nstep 2\nstep 3</think>\nafter"
	got := SanitizeText(in)
	if got != "before\nafter" {
		t.Errorf("SanitizeText multiline = %q; want %q", got, "before\nafter")
	}
}

// TestSanitizeText_TrimsLeadingTrailingWhitespace is the
// v1.3.5-binary invariant: no leading/trailing whitespace on
// the visible response. The binary trims; we trim.
func TestSanitizeText_TrimsLeadingTrailingWhitespace(t *testing.T) {
	in := "\n\n<think>reasoning</think>\n\n   Hi!   \n\n"
	if got := SanitizeText(in); got != "Hi!" {
		t.Errorf("SanitizeText trim = %q; want %q", got, "Hi!")
	}
}

// TestSanitizeText_EmptyInput is the edge case: empty string
// returns empty string (no panic, no allocation).
func TestSanitizeText_EmptyInput(t *testing.T) {
	if got := SanitizeText(""); got != "" {
		t.Errorf("SanitizeText empty = %q; want empty", got)
	}
}

// TestSanitizeText_CollapsesBlankLines covers the
// double-newline-gap cleanup: if the think block was followed by
// more than one blank line, the visible text shouldn't have a
// "double paragraph break" the model itself didn't write.
func TestSanitizeText_CollapsesBlankLines(t *testing.T) {
	in := "para 1\n\n\n\npara 2"
	got := SanitizeText(in)
	if strings.Contains(got, "\n\n\n") {
		t.Errorf("SanitizeText must collapse 3+ newlines, got %q", got)
	}
	if got != "para 1\n\npara 2" {
		t.Errorf("SanitizeText collapse = %q; want %q", got, "para 1\n\npara 2")
	}
}

// TestSanitizeText_NoThinkBlockKeepsBlankLines is a no-op
// regression: a model-emitted paragraph break (single blank
// line) must NOT be collapsed. We only collapse what the
// removed think block left behind.
func TestSanitizeText_NoThinkBlockKeepsBlankLines(t *testing.T) {
	in := "para 1\n\npara 2"
	if got := SanitizeText(in); got != "para 1\n\npara 2" {
		t.Errorf("SanitizeText no-think = %q; want unchanged", got)
	}
}

// ---- Sanitizer (streaming) ---------------------------------------------

// TestSanitizer_StripsSingleBlockChunked is the headline
// streaming test: feed the sanitizer one chunk at a time and
// confirm the visible text emerges cleanly. The chunking here
// matches the 12-byte SSE windows the recovered agw_k0s backend
// emits (see internal/backend/agw_k0s.go:98).
func TestSanitizer_StripsSingleBlockChunked(t *testing.T) {
	s := NewSanitizer()
	full := "<think>The user just wants a simple greeting.</think>\n\nHi there! 👋 How can I help you today?"
	var got strings.Builder
	// Feed in 12-byte chunks to match the production
	// upstream emitter's window size.
	for i := 0; i < len(full); i += 12 {
		end := i + 12
		if end > len(full) {
			end = len(full)
		}
		got.WriteString(s.Push(full[i:end]))
	}
	got.WriteString(s.Finish())
	want := "Hi there! 👋 How can I help you today?"
	if got.String() != want {
		t.Errorf("Sanitizer chunked = %q; want %q", got.String(), want)
	}
}

// TestSanitizer_StripsSingleBlockOneShot is the trivial
// case: the entire text arrives in one Push.
func TestSanitizer_StripsSingleBlockOneShot(t *testing.T) {
	s := NewSanitizer()
	got := s.Push("<think>reasoning</think>\n\nHello!")
	got += s.Finish()
	if got != "Hello!" {
		t.Errorf("Sanitizer oneshot = %q; want %q", got, "Hello!")
	}
}

// TestSanitizer_PassesPlainText is the no-think no-op case.
// Every byte of the input is emitted, possibly after a small
// buffer delay for partial tag detection. The total output
// (Push + Finish) must equal the input.
func TestSanitizer_PassesPlainText(t *testing.T) {
	s := NewSanitizer()
	in := "Hello, world! No thinking trace here."
	got := s.Push(in) + s.Finish()
	if got != in {
		t.Errorf("Sanitizer plain = %q; want %q", got, in)
	}
}

// TestSanitizer_HandlesBoundaryCaseAtOpenTag is the
// worst-case chunk boundary test: a chunk ends exactly in
// the middle of a `<think>` opening tag (`<thin`), and the
// next chunk begins with `k>...`. The sanitizer must hold
// the partial tag in its buffer and only consume it once
// the full tag is available, so the think block is correctly
// stripped.
func TestSanitizer_HandlesBoundaryCaseAtOpenTag(t *testing.T) {
	s := NewSanitizer()
	if got := s.Push("Hello <thin"); got != "Hello " {
		t.Errorf("Sanitizer partial-open chunk1 = %q; want %q", got, "Hello ")
	}
	// The "<thin" is in the buffer. The next chunk
	// completes the opening tag and continues with the
	// think block content.
	if got := s.Push("k>reasoning</think>\n\nworld!"); got != "world!" {
		t.Errorf("Sanitizer partial-open chunk2 = %q; want %q", got, "world!")
	}
	if got := s.Finish(); got != "" {
		t.Errorf("Sanitizer finish = %q; want empty", got)
	}
}

// TestSanitizer_HandlesBoundaryCaseAtCloseTag is the
// counterpart: a chunk ends inside `</think>`. The sanitizer
// must stay in the inside state and only transition to
// outside when the full closing tag is in hand.
func TestSanitizer_HandlesBoundaryCaseAtCloseTag(t *testing.T) {
	s := NewSanitizer()
	// Chunk 1: opens the think block, fills it, ends in
	// the middle of the close tag.
	if got := s.Push("<think>thinking </thin"); got != "" {
		t.Errorf("Sanitizer partial-close chunk1 = %q; want empty", got)
	}
	// Chunk 2: completes the close tag, then emits the
	// visible response.
	if got := s.Push("k>Hi!"); got != "Hi!" {
		t.Errorf("Sanitizer partial-close chunk2 = %q; want %q", got, "Hi!")
	}
	if got := s.Finish(); got != "" {
		t.Errorf("Sanitizer finish = %q; want empty", got)
	}
}

// TestSanitizer_StripsMultipleBlocks checks that two
// independent think blocks (e.g. main + subagent) are both
// removed, AND the trailing whitespace immediately after each
// `</think>` is also stripped (matching the v1.3.5 binary's
// byte-level behaviour: the "\n\n" the model inserts between
// its thinking and its response is gone).
//
// The original text has 2 spaces between "middle" and "end":
// one before <think>second and one after </think>second. After
// the sanitizer strips each think block + its trailing
// whitespace, only the inner single space remains, giving
// "middle end".
func TestSanitizer_StripsMultipleBlocks(t *testing.T) {
	s := NewSanitizer()
	in := "<think>first</think> middle <think>second</think> end"
	got := s.Push(in) + s.Finish()
	if got != "middle end" {
		t.Errorf("Sanitizer multi = %q; want %q", got, "middle end")
	}
}

// TestSanitizer_EmptyInputs is the no-op case: empty Push
// returns empty, Finish returns empty.
func TestSanitizer_EmptyInputs(t *testing.T) {
	s := NewSanitizer()
	if got := s.Push(""); got != "" {
		t.Errorf("Sanitizer empty push = %q; want empty", got)
	}
	if got := s.Finish(); got != "" {
		t.Errorf("Sanitizer empty finish = %q; want empty", got)
	}
}

// TestSanitizer_FlushesBufferOnFinish covers the case where
// the stream ends with text that is NOT a think block, but
// was held back in the buffer because the last chunk ended
// with what looked like a partial tag. The user sees the
// buffered text verbatim on Finish.
func TestSanitizer_FlushesBufferOnFinish(t *testing.T) {
	s := NewSanitizer()
	// Stream ends with what looks like a partial "<thin"
	// — but the stream is done, so there's no follow-up
	// chunk to complete the tag. The sanitizer should
	// flush the buffer to the user, not silently drop it.
	if got := s.Push("Hello <thin"); got != "Hello " {
		t.Errorf("Sanitizer flush chunk = %q; want %q", got, "Hello ")
	}
	if got := s.Finish(); got != "<thin" {
		t.Errorf("Sanitizer flush finish = %q; want %q", got, "<thin")
	}
}

// TestSanitizer_OnThoughtReportsReasoning pins the cycle 100 contract: the
// OnThought callback receives the thinking text (never the boundary tags) while
// the forwarded output stays exactly the strip-only answer. The interactive
// conversation view uses this to render the recovered "Thought for Ns, N
// tokens" step; print mode leaves OnThought nil and is unaffected.
func TestSanitizer_OnThoughtReportsReasoning(t *testing.T) {
	s := NewSanitizer()
	var thoughts []string
	s.OnThought = func(t string) { thoughts = append(thoughts, t) }

	got := s.Push(thinkStartTag+"step one ") + s.Push("step two"+thinkEndTag+"\n\nAnswer.")
	got += s.Finish()
	if got != "Answer." {
		t.Errorf("forwarded = %q; want %q", got, "Answer.")
	}
	joined := strings.Join(thoughts, "")
	if joined != "step one step two" {
		t.Errorf("OnThought text = %q; want %q", joined, "step one step two")
	}
}

// TestSanitizer_OnThoughtSpansChunks checks the callback fires per fragment so a
// live view can stream the reasoning as it arrives, and that nothing is reported
// for text outside the block.
func TestSanitizer_OnThoughtSpansChunks(t *testing.T) {
	s := NewSanitizer()
	var got []string
	s.OnThought = func(t string) { got = append(got, t) }

	_ = s.Push("Visible ")
	_ = s.Push(thinkStartTag + "rea")
	_ = s.Push("soning" + thinkEndTag)
	_ = s.Push(" more")
	if len(got) == 0 {
		t.Fatal("OnThought never fired")
	}
	if strings.Join(got, "") != "reasoning" {
		t.Errorf("OnThought = %q; want %q", strings.Join(got, ""), "reasoning")
	}
}
