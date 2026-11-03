// truncate_test.go — the tool-output truncation, pinned against the
// reference.
//
// Every expectation below is the reference binary's own output, captured
// through the SDK's protocol (一手 — the differential harness is described
// in docs/cycles/cycle-308-harness-tool-surface/deliverable.md).
package harnesstools

import (
	"strconv"
	"strings"
	"testing"
)

// TestTruncateKeepsWholeText pins the case where the output fits.
//
// The budget is `max_tokens × 3` bytes, so a hundred and twenty bytes
// survive a budget of forty tokens untouched — no notice, no cut.
func TestTruncateKeepsWholeText(t *testing.T) {
	text := strings.Repeat("x", 120)
	got := Truncate(text, 40)
	if got != text {
		t.Errorf("Truncate = %q, want the text unchanged", got)
	}
}

// TestTruncateZeroBudgetLeavesTheText pins the SDK's own rule.
//
// The SDK's docstring says that a `max_tokens` of zero disables the
// truncation, and the reference honours it: no notice, no cut.
func TestTruncateZeroBudgetLeavesTheText(t *testing.T) {
	text := strings.Repeat("x", 500)
	got := Truncate(text, 0)
	if got != text {
		t.Errorf("Truncate = %q, want the text unchanged", got)
	}
}

// TestTruncateKeepsTheTail pins the reference's first rule.
//
// The SDK's docstring says the *beginning* survives and the *end* is cut
// ("preserves the beginning (prefix) … truncates the remainder (tail)").
// The reference does the opposite: the capture of a hundred 'A' followed
// by a hundred 'B' keeps the 'B's. The docstring is stale; the binary is
// not.
func TestTruncateKeepsTheTail(t *testing.T) {
	text := strings.Repeat("A", 100) + strings.Repeat("B", 100)
	got := Truncate(text, 10)
	want := "<truncated 22 bytes>\n" + strings.Repeat("B", 30)
	if got != want {
		t.Errorf("Truncate =\n%q\nwant\n%q", got, want)
	}
}

// TestTruncateNoticeCountsDroppedDigits pins the notice in bytes.
//
// The notice does not say how many bytes were dropped. It says how many
// decimal digits the dropped count has, plus nineteen — ten captures pin
// that rule, and the digit count is the only thing that moves with the
// size of the dropped head.
func TestTruncateNoticeCountsDroppedDigits(t *testing.T) {
	cases := []struct {
		name      string
		textLen   int
		maxTokens int
		want      string
	}{
		{"two digits", 129, 10, "<truncated 21 bytes>"},
		{"two digits, larger head", 130, 10, "<truncated 22 bytes>"},
		{"three digits", 200, 10, "<truncated 22 bytes>"},
		{"three digits, larger head", 500, 10, "<truncated 22 bytes>"},
		{"four digits", 1030, 10, "<truncated 23 bytes>"},
		{"four digits, larger head", 5000, 10, "<truncated 23 bytes>"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := Truncate(strings.Repeat("x", tc.textLen), tc.maxTokens)
			if !strings.HasPrefix(got, tc.want) {
				t.Errorf("Truncate notice = %q, want it to start with %q", got, tc.want)
			}
		})
	}
}

// TestTruncateDropsThePartialLine pins the notice in lines.
//
// When the output has lines to cut on, the cut lands on a line boundary
// and the notice counts the line that went. The capture of a `seq 1 100`
// under a budget of thirty bytes keeps the seven lines "94" to "100" —
// the thirty-first byte of the line "93" does not survive, and the
// notice says one line.
func TestTruncateDropsThePartialLine(t *testing.T) {
	// The reference's terminal is a pty, so the lines carry CRLF.
	var text strings.Builder
	for i := 1; i <= 100; i++ {
		text.WriteString(strconv.Itoa(i))
		text.WriteString("\r\n")
	}
	got := Truncate(text.String(), 10)
	want := "<truncated 1 lines>\n"
	for i := 94; i <= 100; i++ {
		want += strconv.Itoa(i) + "\r\n"
	}
	if got != want {
		t.Errorf("Truncate =\n%q\nwant\n%q", got, want)
	}
}

// TestTruncateDropsAWholeLine pins the cut on a line boundary.
//
// The capture of twenty "ab" lines under a budget of twenty-four bytes
// kept five of them: the tail held six whole lines and the reference
// still dropped the first one, whatever it was. The notice says one
// line either way.
func TestTruncateDropsAWholeLine(t *testing.T) {
	var text strings.Builder
	for i := 0; i < 20; i++ {
		text.WriteString("ab\r\n")
	}
	got := Truncate(text.String(), 8)
	want := "<truncated 1 lines>\n" + strings.Repeat("ab\r\n", 5)
	if got != want {
		t.Errorf("Truncate =\n%q\nwant\n%q", got, want)
	}
}

// TestTruncateBudgetIsThreeBytesPerToken pins the budget.
//
// Four captures fix the ratio: five tokens kept fifteen bytes, ten kept
// thirty, twenty kept sixty and a hundred kept three hundred.
func TestTruncateBudgetIsThreeBytesPerToken(t *testing.T) {
	cases := []struct {
		maxTokens int
		wantKept  int
	}{
		{5, 15},
		{10, 30},
		{20, 60},
		{100, 300},
	}
	for _, tc := range cases {
		text := strings.Repeat("x", 1000)
		got := Truncate(text, tc.maxTokens)
		kept := len(got) - len(strings.SplitN(got, "\n", 2)[0]) - 1
		if kept != tc.wantKept {
			t.Errorf("Truncate(maxTokens=%d) kept %d bytes, want %d", tc.maxTokens, kept, tc.wantKept)
		}
	}
}
