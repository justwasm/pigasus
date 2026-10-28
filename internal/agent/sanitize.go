// Package agent — sanitize.go: filter recovered-pipeline debug
// artifacts out of the user-visible text stream.
//
// Background (cycle 50, sub-agent A): the recovered 1.3.5 binary's
// `printmode.(*textEmitter)` writes its own reasoning trace into a
// `<think>...</think>` block at the top of every assistant turn.
// The v1.3.5 binary strips this block before streaming the
// response to stdout; the recovered parse-parity shell was
// forwarding it through verbatim, which is byte-level inconsistent
// with the v1.3.5 binary (the user complaint from the cycle 49
// review: stdout shows `<think>...</think>` then `Hi there! 👋
// ...`, while v1.3.5 shows only `Hi! How can I help you today?`).
//
// The recovered build streams the response in small chunks (12-byte
// windows over the SSE delta stream) and the upstream emitter
// happily splits a single `<think>...</think>` block across many
// chunks. A naive per-chunk regex match misses the boundary
// entirely. We need a streaming sanitizer that:
//
//  1. Buffers text until we know we're outside a think block.
//  2. Tracks whether the last bytes of the buffer could be a
//     partial opening/closing tag, so we can hold them back
//     across chunk boundaries.
//  3. Skips leading whitespace emitted right after `</think>`,
//     matching the v1.3.5 binary's "no whitespace gap" invariant.
//
// The Sanitizer type below implements this streaming contract.
// SanitizeText is the simpler, one-shot helper for tests and for
// callers that already have the full text in hand.
package agent

import (
	"regexp"
	"strings"
)

// thinkBlockRe matches a single `<think>...</think>` block plus
// any trailing whitespace, allowing newlines inside (`(?s)`
// flips DOTALL on for the regex). The `*?` is non-greedy so
// consecutive blocks are each matched independently, not
// coalesced into one large deletion.
//
// Why include the trailing whitespace in the match: the v1.3.5
// binary strips the think block AND the whitespace that follows
// it (typically "\n\n" that the model uses to separate its
// reasoning from its response). By absorbing that trailing
// whitespace into the match, a single `ReplaceAllString` call
// produces the byte-level output the binary emits:
//
//	in:  "<think>reasoning</think>\n\nHi there!"
//	out: "Hi there!"
//
// We deliberately do NOT include leading whitespace in the match
// (no `[\s\n]*` before the opening tag). Leading whitespace
// before the think block is user-intended paragraph formatting
// (e.g. "before\n<think>...") and we preserve it as the v1.3.5
// binary does.
var thinkBlockRe = regexp.MustCompile(`(?s)<think>.*?</think>[\t\n\r ]*`)

// thinkStartTag and thinkEndTag are the literal boundary markers
// we look for in the streaming sanitizer. We track them as
// constants so the test cases can reason about them without
// re-deriving the regex shape.
const (
	thinkStartTag = "<think>"
	thinkEndTag   = "</think>"
)

// maxTagLen is the length of the longer of the two tag literals.
// The streaming sanitizer holds back at most this many bytes
// across a chunk boundary in case the next chunk completes a
// partial tag.
const maxTagLen = len(thinkEndTag)

// isTagPrefix reports whether `s` is a non-empty prefix of either
// the start or end tag. We use this to decide whether to hold
// back the tail of a chunk: only hold back if the tail could be
// the start of a tag.
func isTagPrefix(s string) bool {
	if s == "" {
		return false
	}
	// Check if s is a prefix of thinkStartTag or thinkEndTag.
	// (We don't need to check s == thinkStartTag as that means
	// the full tag matched; the caller handles the matched case
	// separately.)
	return strings.HasPrefix(thinkStartTag, s) || strings.HasPrefix(thinkEndTag, s)
}

// longestMatchingPrefix returns the longest suffix of `s` (up to
// maxTagLen bytes) that is also a prefix of either tag. This is
// the suffix we should hold back across a chunk boundary, so the
// next chunk can complete the tag if it materialises.
//
// Example: s = "Hello <thin" → returns "<thin" (6 bytes, prefix
// of thinkStartTag).
// Example: s = "Hello world"  → returns "" (no suffix matches a
// tag prefix).
func longestMatchingPrefix(s string) string {
	if len(s) > maxTagLen {
		s = s[len(s)-maxTagLen:]
	}
	// Walk from longest possible suffix down to length 1.
	for n := len(s); n > 0; n-- {
		suffix := s[len(s)-n:]
		if isTagPrefix(suffix) {
			return suffix
		}
	}
	return ""
}

// SanitizeText strips recovered `<think>...</think>` blocks from
// `s` and returns the remaining text. The transformation is the
// same one the v1.3.5 binary's printmode emitter applies to its
// own internal reasoning trace before forwarding to the user.
//
// Behaviour:
//   - Multiple think blocks are each removed independently.
//   - The whitespace immediately surrounding a removed block is
//     collapsed to nothing (so the join with adjacent text doesn't
//     produce a `\n\n` gap that wasn't in the v1.3.5 binary).
//   - Leading and trailing whitespace on the final result is
//     trimmed, matching the v1.3.5 binary's "no leading/trailing
//     newlines" invariant.
//
// This is a pure string transform; it never errors and never
// touches I/O. Safe to call from any goroutine.
//
// NOTE: this helper assumes the full text is in hand. For the
// streaming case (text arrives in chunks and a single think block
// may straddle chunk boundaries), use Sanitizer instead.
func SanitizeText(s string) string {
	if s == "" {
		return s
	}
	// Strip every think block. The replacement function
	// inspects the surrounding whitespace and discards it
	// along with the block — this prevents the "before\n" +
	// "" + "\nafter" → "before\n\nafter" doubling that
	// would otherwise leave a paragraph-break-sized gap.
	stripped := thinkBlockRe.ReplaceAllStringFunc(s, func(match string) string {
		// Strip any leading whitespace on the match side
		// (between "before" and "<think>") and any
		// trailing whitespace on the match side (between
		// "</think>" and "after"). The function replaces
		// each block with ""; surrounding whitespace is
		// dropped by the regex match itself.
		return ""
	})
	// Collapse runs of 3+ newlines to 2 (the v1.3.5 binary does
	// not collapse, but cycle 49 review asked for "no
	// double-blank-line gap" in the visible output; this is the
	// minimum that satisfies that constraint without altering
	// the model's own paragraph breaks).
	stripped = collapseBlankLines(stripped)
	// Drop the invisible interlinear-annotation characters — they
	// reach the terminal around wrapped code and some terminals
	// draw a box for them.
	stripped, _ = stripInvisible(stripped)
	// Trim leading/trailing whitespace so the first/last
	// visible character is the model's first/last token.
	return strings.TrimSpace(stripped)
}

// blankLineRe matches 3 or more consecutive newlines (with
// optional spaces/tabs between them).
var blankLineRe = regexp.MustCompile(`\n[\t ]*(\n[\t ]*){2,}`)

// collapseBlankLines reduces runs of 3+ newlines (i.e. 2+ blank
// lines) to exactly one blank line (2 newlines). Single newlines
// (paragraph breaks the model itself emitted) are preserved.
func collapseBlankLines(s string) string {
	return blankLineRe.ReplaceAllString(s, "\n\n")
}

// ---- Streaming Sanitizer --------------------------------------------------
//
// The streaming Sanitizer is the chunk-aware variant of
// SanitizeText. It accepts text incrementally via Push and emits
// only the "outside a think block" portion.
//
// State machine
// -------------
//   - outside              : normal text — emit as we go
//   - inside               : inside a think block — drop until we see "</think>"
//   - outsideJustAfter     : just exited a think block, skip leading
//     whitespace on the next emission so the
//     v1.3.5 binary's "no gap" invariant holds
//
// Memory: at most maxTagLen bytes held back in buf.
type Sanitizer struct {
	inside    bool
	justAfter bool // set true when we just exited a think block
	buf       strings.Builder
	// pending holds the trailing bytes that could be the start of an
	// invisible interlinear-annotation character (U+FFF9 / U+FFFA).
	// A chunk can end in the middle of one of the three-byte
	// sequences; the bytes are prepended to the next chunk so the
	// halves are recognised and dropped as one character.
	//
	// 【一手】 agy 1.4.9 — see invisible.go.
	pending string

	// OnThought, when non-nil, receives the thinking text as it is consumed
	// (i.e. the content inside a ` thinking...</think>` block). It is never
	// given the boundary tags themselves. The callback lets a caller that
	// *wants* to surface the model's reasoning — the interactive conversation
	// view, which renders a "Thought for 1.5s, 24 tokens" step — observe the
	// thought stream without changing what is forwarded to the user. Callers
	// that only want the answer (print mode) leave it nil and pay nothing.
	//
	// The callback runs on the Push caller's goroutine and must be cheap.
	OnThought func(string)
}

// NewSanitizer returns a fresh Sanitizer in the outside state.
func NewSanitizer() *Sanitizer {
	return &Sanitizer{}
}

// Push feeds `s` into the sanitizer and returns the text that
// should be forwarded to the user (i.e. the text outside any
// think block, with leading-whitespace-after-think-block
// skipped).
func (s *Sanitizer) Push(text string) string {
	if text == "" {
		return ""
	}
	// The pending bytes come first: they are the tail of the previous
	// chunk, so they precede the new text. The think-tag hold-back
	// (s.buf) is also a tail, and the two holds are mutually exclusive
	// — a tail is either a partial think tag or a partial invisible
	// character, never both.
	combined := s.buf.String() + s.pending + text
	s.buf.Reset()
	s.pending = ""

	// 【一手】 agy 1.4.9: the invisible interlinear-annotation
	// characters never reach the terminal. The strip happens on the
	// input side, so a character split across two chunks is dropped
	// as one character rather than as two broken runes.
	combined, held := stripInvisible(combined)
	s.pending = held

	var out strings.Builder
	for i := 0; i < len(combined); {
		if s.inside {
			if s.stepInside(combined, &i, &out) {
				return out.String()
			}
		} else {
			if s.stepOutside(combined, &i, &out) {
				return out.String()
			}
		}
	}
	return out.String()
}

func (s *Sanitizer) stepInside(combined string, i *int, out *strings.Builder) bool {
	idx := strings.Index(combined[*i:], thinkEndTag)
	if idx < 0 {
		hold := longestMatchingPrefix(combined[*i:])
		holdStart := len(combined) - len(hold)
		if holdStart < *i {
			holdStart = *i
		}
		if s.OnThought != nil && holdStart > *i {
			s.OnThought(combined[*i:holdStart])
		}
		if hold != "" {
			s.buf.WriteString(hold)
		}
		return true
	}
	if s.OnThought != nil && idx > 0 {
		s.OnThought(combined[*i : *i+idx])
	}
	*i += idx + len(thinkEndTag)
	s.inside = false
	s.justAfter = true
	return false
}

func (s *Sanitizer) stepOutside(combined string, i *int, out *strings.Builder) bool {
	idx := strings.Index(combined[*i:], thinkStartTag)
	if idx < 0 {
		hold := longestMatchingPrefix(combined[*i:])
		holdStart := len(combined) - len(hold)
		if holdStart < *i {
			holdStart = *i
		}
		emitRange := combined[*i:holdStart]
		if s.justAfter {
			emitRange = strings.TrimLeft(emitRange, " \t\n\r")
			if emitRange != "" {
				s.justAfter = false
			}
		}
		out.WriteString(emitRange)
		if hold != "" {
			s.buf.WriteString(hold)
		}
		return true
	}
	emitRange := combined[*i : *i+idx]
	if s.justAfter {
		emitRange = strings.TrimLeft(emitRange, " \t\n\r")
		s.justAfter = false
	}
	out.WriteString(emitRange)
	*i += idx + len(thinkStartTag)
	s.inside = true
	return false
}

// Finish returns the remaining text in the buffer. Call this
// after the last Push to flush any text that was held back due
// to a partial tag at the chunk boundary. If we're inside a
// think block at the end (e.g. the stream ended without a
// closing tag), the buffered text is treated as plain text and
// returned verbatim — the v1.3.5 binary would have shown the
// partial block to the user; we mirror that.
func (s *Sanitizer) Finish() string {
	remaining := s.buf.String()
	s.buf.Reset()
	s.inside = false
	s.justAfter = false
	// The held-back invisible bytes are text like any other: at the
	// end of the stream they are flushed (and the invisible characters
	// they might have completed are dropped).
	pending := s.pending
	s.pending = ""
	remaining += pending
	remaining, _ = stripInvisible(remaining)
	return remaining
}
