// compaction.go — the subagent's own compaction summary.
package subagenttools

import (
	"strings"
)

const (
	// MaxDigestLines is how many lines of the subagent's own work the
	// summary carries.
	MaxDigestLines = 40

	// maxTextSnippet is how much of one assistant message the digest keeps.
	maxTextSnippet = 400

	// maxArgsSnippet is how much of one tool call's arguments the digest keeps.
	maxArgsSnippet = 200

	// maxResultSnippet is how much of one tool result the digest keeps.
	maxResultSnippet = 200

	// ParentContextExtensionName is the name the clone's own extension is
	// loaded under.
	ParentContextExtensionName = "subagent-parent-context"
)

// CompactionPreparation contains messages to summarize for context compaction.
type CompactionPreparation struct {
	FirstKeptEntryID    string
	TokensBefore        int
	MessagesToSummarize []SubagentMessage
}

// SubagentSummary renders the summary a subagent's compaction installs.
func SubagentSummary(pctx *ParentContext, prep CompactionPreparation) string {
	var b strings.Builder
	if pctx != nil {
		if parent := pctx.Summary(); parent != "" {
			b.WriteString(parent)
			b.WriteString("\n\n")
		}
	}
	if digest := digestMessages(prep.MessagesToSummarize); len(digest) > 0 {
		b.WriteString("# Your work so far\n\n")
		for _, line := range digest {
			b.WriteString("- ")
			b.WriteString(line)
			b.WriteString("\n")
		}
	}
	return strings.TrimRight(b.String(), "\n")
}

// digestMessages renders the subagent's own turns as one line each.
func digestMessages(msgs []SubagentMessage) []string {
	var out []string
	for _, msg := range msgs {
		switch msg.Role {
		case "assistant":
			if text := strings.TrimSpace(msg.Content); text != "" {
				out = append(out, "said: "+snippet(text, maxTextSnippet))
			}
			if msg.ToolName != "" {
				out = append(out, "called "+msg.ToolName+"("+snippet(msg.Content, maxArgsSnippet)+")")
			}
		case "tool", "toolResult":
			toolName := msg.ToolName
			if toolName == "" {
				toolName = msg.ToolCallID
			}
			out = append(out, "tool "+toolName+" answered: "+snippet(msg.Content, maxResultSnippet))
		}
	}
	if len(out) > MaxDigestLines {
		out = out[len(out)-MaxDigestLines:]
	}
	return out
}

// snippet shortens a text to at most n characters, marking the cut.
func snippet(text string, n int) string {
	text = strings.TrimSpace(text)
	if n <= 0 || len(text) <= n {
		return text
	}
	return text[:n] + "…"
}
