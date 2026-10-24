// resume_test.go — the reference's post-compaction context block and
// its token accounting.
//
// The block shape is byte-exact evidence: it was read out of the
// reference's own CHECKPOINT step in the trajectory log agy writes to
// ~/.gemini/antigravity-cli/brain/<conversation-id>/.system_generated/logs/transcript.jsonl.

package compaction

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

// TestUserRequests_ChronologicalAndCapped pins the reference's
// captureUserRequests behaviour: only the user turns, in the order they
// were made, capped at the reference's ten-request limit.
func TestUserRequests_ChronologicalAndCapped(t *testing.T) {
	sess := []backend.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "reply"},
		{Role: "user", Content: "second"},
		{Role: "user", Content: "   "}, // blank: dropped
		{Role: "tool", Content: "tool output"},
		{Role: "user", Content: "third"},
	}
	got := UserRequests(sess, MaxUserRequests)
	want := []string{"first", "second", "third"}
	if len(got) != len(want) {
		t.Fatalf("UserRequests = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("UserRequests[%d] = %q, want %q", i, got[i], want[i])
		}
	}

	// The cap keeps the *most recent* requests.
	many := make([]backend.Message, 0, 15)
	for i := 0; i < 15; i++ {
		many = append(many, backend.Message{Role: "user", Content: "r" + string(rune('a'+i))})
	}
	capped := UserRequests(many, MaxUserRequests)
	if len(capped) != MaxUserRequests {
		t.Fatalf("UserRequests cap = %d, want %d", len(capped), MaxUserRequests)
	}
	if capped[0] != "rf" {
		t.Errorf("UserRequests kept %q first, want the oldest of the last ten (%q)", capped[0], "rf")
	}
	if capped[len(capped)-1] != "ro" {
		t.Errorf("UserRequests kept %q last, want %q", capped[len(capped)-1], "ro")
	}
}

// TestResumingBlock_MatchesReferenceShape pins the block the reference
// installs in place of the compacted history.
func TestResumingBlock_MatchesReferenceShape(t *testing.T) {
	block := ResumingBlock([]string{"do the thing", "and the other"}, "<summary>\nbody\n</summary>")

	want := "# Resuming from a compaction\n" +
		"\n" +
		"You are continuing work on the task described above, but you have lost access to the full conversation history, and need to resume work efficiently using the progress summary below:\n" +
		"\n" +
		"# User Requests\n" +
		"The following were user requests from the truncated conversation in chronological order:\n" +
		"1. do the thing\n" +
		"2. and the other\n" +
		"\n" +
		"<summary>\nbody\n</summary>\n"
	if block != want {
		t.Errorf("ResumingBlock =\n%q\nwant\n%q", block, want)
	}
}

// TestResumingBlock_NoUserRequests pins the degenerate case: the
// reference's block still carries the preamble and the summary.
func TestResumingBlock_NoUserRequests(t *testing.T) {
	block := ResumingBlock(nil, "<summary>s</summary>")
	if !strings.HasPrefix(block, "# Resuming from a compaction\n\n") {
		t.Errorf("ResumingBlock should start with the reference's header; got %q", block)
	}
	if !strings.Contains(block, "# User Requests\n") {
		t.Errorf("ResumingBlock should keep the '# User Requests' header; got %q", block)
	}
	if !strings.HasSuffix(block, "<summary>s</summary>\n") {
		t.Errorf("ResumingBlock should end with the summary; got %q", block)
	}
}

// TestContextOverheadTokens_ThreeBytesPerToken pins the reference's
// estimator: three bytes per token, over the system prompt plus the
// tool declarations.
func TestContextOverheadTokens_ThreeBytesPerToken(t *testing.T) {
	if BytesPerToken != 3 {
		t.Fatalf("BytesPerToken = %d, want 3", BytesPerToken)
	}
	got := ContextOverheadTokens(strings.Repeat("a", 3000), []byte(strings.Repeat("b", 6000)))
	if want := 9000 / 3; got != want {
		t.Errorf("ContextOverheadTokens = %d, want %d", got, want)
	}
	if got := ContextOverheadTokens("", nil); got != 0 {
		t.Errorf("ContextOverheadTokens(empty) = %d, want 0", got)
	}
}
