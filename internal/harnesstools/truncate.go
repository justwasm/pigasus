// truncate.go — the client's tool-output truncation.
//
// # What the client asks for
//
// `HarnessConfig.tool_output_truncation.truncate.max_tokens` is a budget
// for a single tool response. The SDK's `ToolOutputTruncationConfig`
// carries nothing but that number, and the SDK never sets it by default:
// a session that does not ask for truncation gets the whole output.
//
// # The reference's rule
//
// Captured from the 1.4.7 binary through the SDK's own protocol
// (【一手】; the captures are listed in
// docs/cycles/cycle-308-harness-tool-surface/deliverable.md):
//
//   - the budget is `max_tokens × 3` bytes. The captures pin it at
//     fifteen, thirty, sixty and three hundred bytes for budgets of
//     five, ten, twenty and a hundred;
//
//   - the *tail* is kept. A run of one hundred 'A' followed by one
//     hundred 'B' keeps the 'B's, and a `seq 1 1000` keeps the lines
//     402 to 1000;
//
//   - the cut lands on whole lines. The budget of thirty bytes keeps
//     the last twenty-nine bytes of a `seq 1 100` — the seven lines
//     "94" to "100" — and not the thirty-first byte of the line "93";
//
//   - a tail that holds a line loses its first line, whatever that line
//     was: a budget of twenty-four bytes over twenty "ab" lines keeps
//     five of them and says one line went;
//
//   - the notice is the reference's own text, and it comes *before* the
//     output that survived:
//
//     <truncated 1 lines>
//     <truncated 22 bytes>
//
// # The byte count is not the byte count
//
// The notice in bytes does not say how many bytes were dropped. It says
// how many *decimal digits* the dropped count has, plus nineteen:
//
//	dropped   10   30   70   99  100  170  200  470  1000  4970
//	notice    21   21   21   21   22   22   22   22    23    23
//
// Ten captures pin that rule; what the reference means by it is not
// known (存疑). The clone reproduces the bytes it captured rather than
// the rule behind them.
package harnesstools

import (
	"fmt"
	"strconv"
	"strings"
)

// bytesPerToken is the reference's estimate of a token's size.
//
// The budget is `max_tokens × bytesPerToken` bytes, and the captures
// agree on three: fifteen bytes for five tokens, thirty for ten,
// sixty for twenty, three hundred for a hundred.
const bytesPerToken = 3

// noticeDigitBase is what the reference's byte notice counts from.
//
// The notice in bytes counts the dropped bytes' decimal digits and adds
// this to them; the captures agree on it from the first probe to the
// tenth.
const noticeDigitBase = 19

// Truncate cuts a tool's output down to the client's budget.
//
// The output that survives is the tail of the text, cut on a line
// boundary when the text has lines to cut on. The notice in front of it
// is the reference's own.
//
// A budget of zero leaves the text alone: the SDK's configuration says
// that zero disables truncation.
func Truncate(text string, maxTokens int) string {
	if maxTokens <= 0 || len(text) == 0 {
		return text
	}
	budget := maxTokens * bytesPerToken
	if len(text) <= budget {
		return text
	}
	dropped := len(text) - budget
	start := len(text) - budget
	tail := text[start:]
	if idx := strings.IndexByte(tail, '\n'); idx >= 0 {
		// The tail holds a line: its first line goes, and the notice
		// counts that one line. The reference drops it whether the cut
		// left a fragment of it or cut it whole.
		return fmt.Sprintf("<truncated %d lines>\n%s", 1, tail[idx+1:])
	}
	return fmt.Sprintf("<truncated %d bytes>\n%s", noticeBytes(dropped), tail)
}

// noticeBytes renders the reference's byte notice.
//
// The number is the dropped count's decimal digits plus nineteen, not
// the dropped count itself.
func noticeBytes(dropped int) int {
	if dropped <= 0 {
		return noticeDigitBase + 1
	}
	return noticeDigitBase + len(strconv.Itoa(dropped))
}
