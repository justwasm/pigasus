// compaction_test.go — the subagent's own compaction summary.
package subagenttools

import (
	"strings"
	"testing"
	"time"
)

// TestSubagentSummaryCarriesTheParentsHalf pins that the summary the
// subagent's compaction installs carries the task, the parent's id and the
// parent's messages beside the digest of the subagent's own work.
func TestSubagentSummaryCarriesTheParentsHalf(t *testing.T) {
	pctx := &ParentContext{
		ID:     "parent-id",
		Task:   "Collect the changelog",
		TaskAt: time.Unix(100, 0),
	}
	pctx.Record("also look at the release notes", time.Unix(200, 0))

	prep := CompactionPreparation{
		FirstKeptEntryID: "entry-7",
		TokensBefore:     4242,
		MessagesToSummarize: []SubagentMessage{
			{
				Role:    "assistant",
				Content: "I found the changelog.",
			},
			{
				Role:     "assistant",
				ToolName: "read_file",
				Content:  `{"path": "CHANGELOG.md"}`,
			},
			{
				Role:     "toolResult",
				ToolName: "read_file",
				Content:  "the file's text",
			},
		},
	}

	summary := SubagentSummary(pctx, prep)
	for _, want := range []string{
		"Collect the changelog",
		"parent-id",
		"also look at the release notes",
		"I found the changelog.",
		"read_file",
		"the file's text",
	} {
		if !strings.Contains(summary, want) {
			t.Fatalf("summary is missing %q:\n%s", want, summary)
		}
	}
}

// TestDigestKeepsTheMostRecentSteps pins the bound on the digest.
func TestDigestKeepsTheMostRecentSteps(t *testing.T) {
	var msgs []SubagentMessage
	for i := 0; i < MaxDigestLines+10; i++ {
		msgs = append(msgs, SubagentMessage{
			Role:    "assistant",
			Content: "step",
		})
	}
	lines := digestMessages(msgs)
	if len(lines) != MaxDigestLines {
		t.Fatalf("digest has %d lines, want %d", len(lines), MaxDigestLines)
	}
}
