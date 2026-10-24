// resume.go — the context block the reference installs after a compaction,
// and the token accounting behind its stats row.
//
// Everything here is derived from agy 1.4.5 at 【一手】 (a live
// compaction) or 【反编译】 (the binary's own functions):
//
//   - the post-compaction block is the reference's own CHECKPOINT step
//     content, read verbatim out of the trajectory log the reference
//     writes under
//     ~/.gemini/antigravity-cli/brain/<conversation-id>/.system_generated/logs/transcript.jsonl;
//   - the "# User Requests" cap is the reference's
//     CheckpointConfig.max_user_requests (10) — the captured checkpoint
//     lists exactly ten requests;
//   - the token divisor is three bytes per token, decompiled from
//     jetski/tokenizer.(*EstimateTokenCounter).GetTokenCount
//     (`int(float64(len) / 3.0)`; createDefaultTokenCounter stores
//     0x4008000000000000 = 3.0).
package compaction

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/backend"
)

// MaxUserRequests is the reference's cap on the "# User Requests" list.
// Its per-model checkpoint config carries max_user_requests: 10, and the
// captured checkpoint lists exactly ten requests.
const MaxUserRequests = 10

// BytesPerToken is the reference's token-estimate divisor: three bytes per
// token. It is the divisor of jetski/tokenizer's EstimateTokenCounter, the
// estimator behind the reference's /context panel and its compaction stats.
const BytesPerToken = 3

// UserRequests returns the session's user requests in chronological order,
// capped at limit entries (the reference's captureUserRequests). Empty
// entries are dropped; the text is kept verbatim, including newlines — the
// reference embeds multi-line requests as they were typed.
func UserRequests(sess []backend.Message, limit int) []string {
	if limit <= 0 {
		return nil
	}
	var out []string
	for _, m := range sess {
		if m.Role != "user" {
			continue
		}
		text := strings.TrimSpace(m.Content)
		if text == "" {
			continue
		}
		out = append(out, text)
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// ResumingBlock builds the context the reference installs in place of the
// compacted history. The reference's own CHECKPOINT step carries this
// shape:
//
//	# Resuming from a compaction
//
//	You are continuing work on the task described above, but you have lost
//	access to the full conversation history, and need to resume work
//	efficiently using the progress summary below:
//
//	# User Requests
//	The following were user requests from the truncated conversation in chronological order:
//	1. …
//
//	<summary>
//	…
//	</summary>
//
// The reference appends a fourth section, "# Conversation Logs", pointing at
// the trajectory files it wrote to disk. The clone keeps its sessions in
// memory and writes no such files, so that section is omitted; the three
// sections above it are reproduced byte for byte.
//
// summary is the model's reply, which already carries the reference's
// <summary></summary> wrapper because the instruction asks for it.
func ResumingBlock(userRequests []string, summary string) string {
	var b strings.Builder
	b.WriteString("# Resuming from a compaction\n\n")
	b.WriteString("You are continuing work on the task described above, but you have lost access to the full conversation history, and need to resume work efficiently using the progress summary below:\n\n")
	b.WriteString("# User Requests\n")
	b.WriteString("The following were user requests from the truncated conversation in chronological order:\n")
	for i, r := range userRequests {
		fmt.Fprintf(&b, "%d. %s\n", i+1, r)
	}
	b.WriteString("\n")
	b.WriteString(strings.TrimSpace(summary))
	b.WriteString("\n")
	return b.String()
}

// ContextOverheadTokens estimates the base context the reference's stats
// row counts beside the summary size: the system prompt plus the tool
// declarations, at the reference's three bytes per token.
//
// The reference computes it in render.ContextOverheadTokens by summing the
// per-item token counts of the request's prompt sections and tool
// declarations. The clone does not keep per-section counts for the tool
// declarations, so it measures the serialized declarations directly.
//
// The measured shape matches the reference: an agy 1.4.5 compaction
// reported "context 13.1k → ~17.9k (266 summary)", and the request's
// instructions (38 737 bytes) plus its twelve declarations (13 827 bytes)
// come to 52 564 bytes — 17 521 tokens at three bytes per token, which is
// the "~17.9k" the row shows once the summary's 266 tokens are added.
func ContextOverheadTokens(systemPrompt string, declarationsJSON []byte) int {
	total := len(systemPrompt) + len(declarationsJSON)
	if total <= 0 {
		return 0
	}
	return total / BytesPerToken
}
