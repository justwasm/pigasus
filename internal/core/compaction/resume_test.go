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

	"github.com/justwasm/pigasus/internal/core/backend"
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

// TestResumeBlock_ConversationLogsMatches153 pins the fixed tail the 1.5.3
// CHECKPOINT step carries. The paths are placeholders in the reference's
// own output — it does not substitute the app-data dir or conversation id.
func TestResumeBlock_ConversationLogsMatches153(t *testing.T) {
	block := ResumeBlock(ResumeOptions{
		UserRequests:           []string{"do the thing"},
		Summary:                "<summary>body</summary>",
		IncludeConversationLog: true,
	})
	const wantLogs = "# Conversation Logs\n" +
		"Start with the compact transcript; if a step has `truncated_fields`, read only that specific line in `transcript_full.jsonl`:\n" +
		"- `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript.jsonl`\n" +
		"- `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript_full.jsonl`"
	want := "# Resuming from a compaction\n" +
		"\n" +
		"You are continuing work on the task described above, but you have lost access to the full conversation history, and need to resume work efficiently using the progress summary below:\n" +
		"\n" +
		"# User Requests\n" +
		"The following were user requests from the truncated conversation in chronological order:\n" +
		"1. do the thing\n" +
		"\n" +
		"<summary>body</summary>\n" +
		"\n" + wantLogs
	if block != want {
		t.Errorf("ResumeBlock =\n%q\nwant\n%q", block, want)
	}
	if strings.HasSuffix(block, "\n") {
		t.Error("the captured block ends at transcript_full.jsonl with no trailing newline")
	}
}

// TestResumeBlock_ArtifactsMatch153 pins the artifact section: the header
// counts the entries, each entry is three lines, and one blank line
// separates the entries and the following section.
func TestResumeBlock_ArtifactsMatch153(t *testing.T) {
	block := ResumeBlock(ResumeOptions{
		UserRequests: []string{"do the thing"},
		Summary:      "<summary>body</summary>",
		Artifacts: []ArtifactRef{
			{
				Name:       "full_agent_bridge_plan",
				Path:       "file:///home/brain/conv/full_agent_bridge_plan.md",
				LastEdited: "2026-11-07T16:01:33Z",
			},
			{
				Name:       "mock_remote_agent_plan",
				Path:       "file:///home/brain/conv/mock_remote_agent_plan.md",
				LastEdited: "2026-11-07T13:55:31Z",
			},
		},
		IncludeConversationLog: true,
	})
	wantTail := "<summary>body</summary>\n\n" +
		"You have the 2 following artifacts written to the artifacts directory:\n\n" +
		"[ARTIFACT: full_agent_bridge_plan]\n" +
		"Path: file:///home/brain/conv/full_agent_bridge_plan.md\n" +
		"Last Edited: 2026-11-07T16:01:33Z\n\n" +
		"[ARTIFACT: mock_remote_agent_plan]\n" +
		"Path: file:///home/brain/conv/mock_remote_agent_plan.md\n" +
		"Last Edited: 2026-11-07T13:55:31Z\n\n" +
		ConversationLogs
	if !strings.HasSuffix(block, wantTail) {
		t.Errorf("ResumeBlock tail =\n%q\nwant suffix\n%q", block, wantTail)
	}
}

// TestResumeBlock_RunningTasksMatch153 pins the recovered running-task
// section: header, two-line preamble, then newline-joined protojson task
// snapshots. 【反编译】 1.5.3 serializers.DefaultSerializer.checkpointToMessage.
func TestResumeBlock_RunningTasksMatch153(t *testing.T) {
	block := ResumeBlock(ResumeOptions{
		UserRequests: []string{"do the thing"},
		Summary:      "<summary>body</summary>",
		RunningTasks: []TaskRef{
			{TaskID: "task-4", ToolName: "run_command", Description: "build", StepIndex: 12, LogURI: "file:///tmp/task-4.log"},
			{TaskID: "task-5", ToolName: "run_command", IsDaemon: true},
		},
	})
	wantTail := "<summary>body</summary>\n\n" +
		"# Running Background Tasks\n" +
		"The following background tasks were running at the time this checkpoint was created.\n" +
		"Use the manage_task tool to interact with them (e.g. to kill them or check their status).\n\n" +
		"{\n" +
		"  \"taskId\":  \"task-4\",\n" +
		"  \"toolName\":  \"run_command\",\n" +
		"  \"description\":  \"build\",\n" +
		"  \"stepIndex\":  12,\n" +
		"  \"logUri\":  \"file:///tmp/task-4.log\"\n" +
		"}\n" +
		"{\n" +
		"  \"taskId\":  \"task-5\",\n" +
		"  \"toolName\":  \"run_command\",\n" +
		"  \"isDaemon\":  true\n" +
		"}"
	if !strings.HasSuffix(block, wantTail) {
		t.Errorf("ResumeBlock tail =\n%q\nwant suffix\n%q", block, wantTail)
	}
}

// TestResumeBlock_SubagentsMatch153 pins the subagent section from the live
// 1.5.3 checkpoint at step 7414 (conversation
// 43966145-9669-4114-ac5c-91e08c7cd05b).
func TestResumeBlock_SubagentsMatch153(t *testing.T) {
	block := ResumeBlock(ResumeOptions{
		UserRequests: []string{"do the thing"},
		Summary:      "<summary>body</summary>",
		Subagents: &SubagentSection{
			Total: 1,
			Max:   20,
			Entries: []SubagentRef{{
				TypeName:       "DeepCoder",
				Role:           "Subagent & Task TUI Alignment Engineer",
				ConversationID: "b1923c8b-d44e-4b8a-a368-29b54434bee4",
				LogAbsoluteURI: "file:///home/brain/b1923c8b-d44e-4b8a-a368-29b54434bee4/.system_generated/logs/transcript.jsonl",
				WorkspaceURIs:  []string{"file:///Users/gear/GitHub/openagy"},
				SpawnStepIndex: 7363,
			}},
		},
		IncludeConversationLog: true,
	})
	wantSection := "# Subagents\n" +
		"The following subagents were spawned during this conversation (up to 20).\n\n" +
		"{\n" +
		"  \"spec\":  {\n" +
		"    \"typeName\":  \"DeepCoder\",\n" +
		"    \"role\":  \"Subagent & Task TUI Alignment Engineer\"\n" +
		"  },\n" +
		"  \"result\":  {\n" +
		"    \"conversationId\":  \"b1923c8b-d44e-4b8a-a368-29b54434bee4\",\n" +
		"    \"logAbsoluteUri\":  \"file:///home/brain/b1923c8b-d44e-4b8a-a368-29b54434bee4/.system_generated/logs/transcript.jsonl\",\n" +
		"    \"workspaceUris\":  [\n" +
		"      \"file:///Users/gear/GitHub/openagy\"\n" +
		"    ]\n" +
		"  },\n" +
		"  \"spawnStepIndex\":  7363\n" +
		"}\n\n" +
		ConversationLogs
	if !strings.HasSuffix(block, wantSection) {
		t.Errorf("ResumeBlock tail =\n%q\nwant suffix\n%q", block, wantSection)
	}
}

// TestResumeBlock_SubagentsTruncatedHeader pins the header and preamble the
// reference uses when the snapshot cap hides older subagents.
func TestResumeBlock_SubagentsTruncatedHeader(t *testing.T) {
	block := ResumeBlock(ResumeOptions{
		UserRequests: []string{"do the thing"},
		Summary:      "<summary>body</summary>",
		Subagents: &SubagentSection{
			Total: 25,
			Max:   20,
			Entries: []SubagentRef{
				{TypeName: "DeepCoder"},
			},
		},
	})
	if !strings.Contains(block, "# Subagents (25 total, showing 1 most recent)\n") {
		t.Errorf("missing the capped header: %q", block)
	}
	if !strings.Contains(block, "The following are the most recent subagents spawned during this conversation.\n\n") {
		t.Errorf("missing the recent preamble: %q", block)
	}
}

// TestUserRequests_SkipsTheResumeBlock pins that a later checkpoint does not
// treat the post-compaction block as something the user typed. The reference
// carries the original request list in the checkpoint instead.
func TestUserRequests_SkipsTheResumeBlock(t *testing.T) {
	resume := ResumingBlock([]string{"first"}, "<summary>body</summary>")
	got := UserRequests([]backend.Message{
		{Role: "user", Content: resume},
		{Role: "assistant", Content: "reply"},
		{Role: "user", Content: "second"},
	}, MaxUserRequests)
	if len(got) != 1 || got[0] != "second" {
		t.Fatalf("UserRequests = %v, want [second]", got)
	}
	if !IsResumeBlock(resume) {
		t.Fatal("IsResumeBlock did not recognize the block")
	}
}

// TestResumeBlock_ParentMessagesMatch153 pins the subagent checkpoint's
// carried half: "# Parent Agent Messages" and the parent's one-line messages
// replace "# User Requests".
func TestResumeBlock_ParentMessagesMatch153(t *testing.T) {
	line := "[Message] timestamp=2026-11-03T21:30:22Z sender=53dc2d4d-166f-4082-9dce-7ca95818e00d priority=MESSAGE_PRIORITY_HIGH content=<original_task>\nTask: hello"
	block := ResumeBlock(ResumeOptions{
		UserRequests:           []string{"this must not render"},
		Summary:                "<summary>body</summary>",
		ParentMessages:         []string{line},
		IncludeConversationLog: true,
	})
	wantPrefix := "# Resuming from a compaction\n\n" +
		"You are continuing work on the task described above, but you have lost access to the full conversation history, and need to resume work efficiently using the progress summary below:\n\n" +
		"# Parent Agent Messages\n" +
		"The following were messages from your parent agent in the truncated conversation in chronological order:\n" +
		"1. " + line + "\n\n" +
		"<summary>body</summary>"
	if !strings.HasPrefix(block, wantPrefix) {
		t.Errorf("ResumeBlock = %q, want prefix %q", block, wantPrefix)
	}
	if strings.Contains(block, "# User Requests") {
		t.Errorf("subagent block must not carry # User Requests: %q", block)
	}
	if !strings.HasSuffix(block, ConversationLogs) {
		t.Errorf("subagent block lost the conversation-log tail: %q", block)
	}
}

// TestResumeBlock_ParentMessagesMostRecent pins the capped preamble.
func TestResumeBlock_ParentMessagesMostRecent(t *testing.T) {
	block := ResumeBlock(ResumeOptions{
		Summary:                  "<summary>body</summary>",
		ParentMessages:           []string{"line one"},
		ParentMessagesMostRecent: true,
	})
	if !strings.Contains(block, "The following were the most recent messages from your parent agent in chronological order:\n") {
		t.Errorf("missing the most-recent preamble: %q", block)
	}
}
