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
	"encoding/json"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/core/backend"
)

// ResumeHeader is the first line of the post-compaction block.
const ResumeHeader = "# Resuming from a compaction"

// IsResumeBlock reports whether a message is the post-compaction block.
//
// The block is installed as a user-role message, but it is not something the
// user typed: a later checkpoint must not list it as a request (the reference
// keeps the original request list in the checkpoint instead).
func IsResumeBlock(text string) bool {
	return strings.HasPrefix(strings.TrimSpace(text), ResumeHeader)
}

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
		if text == "" || IsResumeBlock(text) {
			continue
		}
		out = append(out, text)
	}
	if len(out) > limit {
		out = out[len(out)-limit:]
	}
	return out
}

// CarriedBlock builds the part of the post-compaction context that
// carries the conversation forward **besides** the summary: the
// reference's own header and the "# User Requests" list.
//
// 【一手】 agy 1.4.10, the vendor's own release notes: "Improved the
// after-compaction size shown in the compaction marker and in `/context`
// to also count what the summary carries into the next prompt, such as
// your earlier requests and artifact, subagent, and running-task
// snapshots, so it is closer to the real size; the `(N summary)` label
// still shows the summary alone."
//
// The clone's resume block carries the earlier requests; it has no
// artifact, subagent or running-task snapshots — those are the
// reference's own checkpoint sections — so what is counted here is the
// header and the request list.
func CarriedBlock(userRequests []string) string {
	var b strings.Builder
	b.WriteString(resumePreamble)
	b.WriteString("# User Requests\n")
	b.WriteString("The following were user requests from the truncated conversation in chronological order:\n")
	for i, r := range userRequests {
		fmt.Fprintf(&b, "%d. %s\n", i+1, r)
	}
	b.WriteString("\n")
	return b.String()
}

// resumePreamble is the fixed lead-in every carried block shares.
const resumePreamble = "# Resuming from a compaction\n\n" +
	"You are continuing work on the task described above, but you have lost access to the full conversation history, and need to resume work efficiently using the progress summary below:\n\n"

// ParentBlock builds the carried half of a subagent's checkpoint: the task
// the parent gave the child and the parent's later messages, instead of the
// child's own "# User Requests".
//
// 【一手】 agy 1.5.3, conversation a9756e99-…, checkpoint at step 350:
//
//	# Parent Agent Messages
//	The following were messages from your parent agent in the truncated conversation in chronological order:
//	1. [Message] timestamp=… sender=… priority=MESSAGE_PRIORITY_HIGH content=…
//
// The capped variant's preamble is "The following were the most recent
// messages from your parent agent in chronological order:". The entries are
// already rendered (agentmessage.RenderLine).
func ParentBlock(messages []string, mostRecent bool) string {
	var b strings.Builder
	b.WriteString(resumePreamble)
	b.WriteString("# Parent Agent Messages\n")
	if mostRecent {
		b.WriteString("The following were the most recent messages from your parent agent in chronological order:\n")
	} else {
		b.WriteString("The following were messages from your parent agent in the truncated conversation in chronological order:\n")
	}
	for i, m := range messages {
		fmt.Fprintf(&b, "%d. %s\n", i+1, m)
	}
	b.WriteString("\n")
	return b.String()
}

// ConversationLogs is the reference's "# Conversation Logs" section.
//
// 【一手】 agy 1.5.3, every CHECKPOINT step in
// ~/.gemini/antigravity-cli/brain/<conversation-id>/.system_generated/logs/transcript.jsonl
// carries this tail byte for byte. The paths are the reference's own
// placeholders — it does not substitute the app-data dir or the conversation
// id here; the model is expected to expand them from <user_information>.
const ConversationLogs = "# Conversation Logs\n" +
	"Start with the compact transcript; if a step has `truncated_fields`, read only that specific line in `transcript_full.jsonl`:\n" +
	"- `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript.jsonl`\n" +
	"- `<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript_full.jsonl`"

// ArtifactRef is one entry of the reference's artifact snapshot section.
//
// 【一手】 agy 1.5.3, the same CHECKPOINT step:
//
//	You have the 2 following artifacts written to the artifacts directory:
//
//	[ARTIFACT: full_agent_bridge_plan]
//	Path: file:///…/brain/<conversation-id>/full_agent_bridge_plan.md
//	Last Edited: 2026-11-07T16:01:33Z
//
// Name is the artifact's file stem. Path is the file:// URI. LastEdited is
// the reference's timestamp spelling (UTC, second precision), kept as a
// string so the block reproduces the captured bytes exactly.
type ArtifactRef struct {
	Name       string
	Path       string
	LastEdited string
}

// TaskRef is one running background task in the reference's checkpoint
// section.
//
// 【反编译】 the section is written by serializers.DefaultSerializer's
// checkpoint path: the header is the recovered template
// "# Running Background Tasks\nThe following background tasks were running
// at the time this checkpoint was created.\nUse the manage_task tool to
// interact with them (e.g. to kill them or check their status).\n\n%s",
// and %s is the newline-joined protojson form of the checkpoint's
// running_task_snapshots (TaskSnapshot). The JSON field names below are the
// proto field names in protojson spelling; zero values are omitted.
type TaskRef struct {
	TaskID      string
	ToolName    string
	ToolSummary string
	Description string
	StartTime   string
	StepIndex   uint32
	LogURI      string
	IsDaemon    bool
}

// SubagentRef is one subagent in the reference's "# Subagents" section.
//
// 【一手】 agy 1.5.3, checkpoint at step 7414 of conversation
// 43966145-…: the section is a plain "# Subagents" header, the sentence
// "The following subagents were spawned during this conversation (up to 20).",
// then one pretty-printed protojson SubagentSnapshot per spawned subagent.
// Only spec.typeName/spec.role and result's conversationId/logAbsoluteUri/
// workspaceUris appeared; empty fields are omitted.
type SubagentRef struct {
	TypeName       string
	Role           string
	InitialPrompt  string
	Inherit        bool
	Branch         string
	WorkspaceURI   string
	Model          string
	ModelTier      string
	RunAsTask      bool
	Share          string
	Fork           bool
	ConversationID string
	LogAbsoluteURI string
	WorkspaceURIs  []string
	Killed         bool
	SpawnStepIndex int32
}

// SubagentSection is the reference's subagent snapshot section.
//
// Total is the number of subagents spawned; Entries is the capped list the
// reference keeps (max_subagent_snapshots, 20 by default). When Total is
// larger than len(Entries), the header carries the counts and the preamble
// says "most recent"; otherwise the header is plain and the preamble names
// the cap.
type SubagentSection struct {
	Total   int
	Max     int
	Entries []SubagentRef
}

// ResumeOptions is the data the post-compaction block renders.
type ResumeOptions struct {
	// UserRequests is the capped request list (see UserRequests).
	UserRequests []string
	// Summary is the model's reply, including its <summary> wrapper.
	Summary string
	// Artifacts are the conversation's current artifacts. Empty omits the
	// section.
	Artifacts []ArtifactRef
	// IncludeConversationLog adds the reference's fixed "# Conversation Logs"
	// tail. The profile's include_conversation_log decides it.
	IncludeConversationLog bool
	// ParentMessages is a subagent's parent half. Non-empty replaces the
	// "# User Requests" section with "# Parent Agent Messages".
	ParentMessages []string
	// ParentMessagesMostRecent selects the capped preamble ("the most
	// recent messages") instead of the truncated-conversation one.
	ParentMessagesMostRecent bool
	// RunningTasks renders the "# Running Background Tasks" section. Empty
	// omits it.
	RunningTasks []TaskRef
	// Subagents renders the "# Subagents" section. Nil omits it.
	Subagents *SubagentSection
}

// ResumeBlock builds the context the reference installs in place of the
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
//	You have the N following artifacts written to the artifacts directory:
//
//	[ARTIFACT: name]
//	Path: file:///…
//	Last Edited: 2026-11-07T16:01:33Z
//
//	# Conversation Logs
//	…
//
// The block is CarriedBlock's bytes followed by the summary — the split
// is what lets the stats row count the carried half separately from the
// summary (agy 1.4.10). The artifact and conversation-log sections are
// byte-exact against the 1.5.3 checkpoint step; the artifact header only
// appears when the conversation has artifacts, and the log tail only when
// the profile sets include_conversation_log.
func ResumeBlock(o ResumeOptions) string {
	var b strings.Builder
	if len(o.ParentMessages) > 0 {
		b.WriteString(ParentBlock(o.ParentMessages, o.ParentMessagesMostRecent))
	} else {
		b.WriteString(CarriedBlock(o.UserRequests))
	}
	b.WriteString(strings.TrimSpace(o.Summary))
	b.WriteString("\n")
	if len(o.Artifacts) > 0 {
		fmt.Fprintf(&b, "\nYou have the %d following artifacts written to the artifacts directory:\n\n", len(o.Artifacts))
		for i, a := range o.Artifacts {
			if i > 0 {
				b.WriteString("\n")
			}
			fmt.Fprintf(&b, "[ARTIFACT: %s]\nPath: %s\nLast Edited: %s\n", a.Name, a.Path, a.LastEdited)
		}
	}
	if len(o.RunningTasks) > 0 {
		b.WriteString(sectionGap(&b))
		b.WriteString("# Running Background Tasks\n")
		b.WriteString("The following background tasks were running at the time this checkpoint was created.\n")
		b.WriteString("Use the manage_task tool to interact with them (e.g. to kill them or check their status).\n\n")
		for i, task := range o.RunningTasks {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(taskJSON(task))
		}
	}
	if o.Subagents != nil && len(o.Subagents.Entries) > 0 {
		b.WriteString(sectionGap(&b))
		b.WriteString(subagentHeader(o.Subagents))
		for i, sub := range o.Subagents.Entries {
			if i > 0 {
				b.WriteString("\n")
			}
			b.WriteString(subagentJSON(sub))
		}
	}
	if o.IncludeConversationLog {
		b.WriteString(sectionGap(&b))
		b.WriteString(ConversationLogs)
	}
	return b.String()
}

// taskJSON renders one TaskSnapshot the way protojson does with
// Multiline:true and Indent:"  ": two spaces after the colon, two-space
// indentation, zero values omitted, fields in proto field order.
func taskJSON(t TaskRef) string {
	var fields []string
	addString := func(name, value string) {
		if value != "" {
			fields = append(fields, fmt.Sprintf("  %q:  %s", name, quoteJSON(value)))
		}
	}
	addString("taskId", t.TaskID)
	addString("toolName", t.ToolName)
	addString("toolSummary", t.ToolSummary)
	addString("description", t.Description)
	addString("startTime", t.StartTime)
	if t.StepIndex != 0 {
		fields = append(fields, fmt.Sprintf("  %q:  %d", "stepIndex", t.StepIndex))
	}
	addString("logUri", t.LogURI)
	if t.IsDaemon {
		fields = append(fields, fmt.Sprintf("  %q:  true", "isDaemon"))
	}
	return "{\n" + strings.Join(fields, ",\n") + "\n}"
}

// subagentHeader renders the "# Subagents" header and preamble.
func subagentHeader(s *SubagentSection) string {
	total := s.Total
	if total < len(s.Entries) {
		total = len(s.Entries)
	}
	shown := len(s.Entries)
	if shown < total {
		return fmt.Sprintf("# Subagents (%d total, showing %d most recent)\n%s\n\n",
			total, shown, "The following are the most recent subagents spawned during this conversation.")
	}
	if s.Max > 0 {
		return fmt.Sprintf("# Subagents\nThe following subagents were spawned during this conversation (up to %d).\n\n", s.Max)
	}
	return "# Subagents\nThe following subagents were spawned during this conversation.\n\n"
}

// subagentJSON renders one SubagentSnapshot the same way taskJSON renders a
// TaskSnapshot.
func subagentJSON(s SubagentRef) string {
	var top []string
	var spec []string
	addSpecString := func(name, value string) {
		if value != "" {
			spec = append(spec, fmt.Sprintf("    %q:  %s", name, quoteJSON(value)))
		}
	}
	addSpecString("typeName", s.TypeName)
	addSpecString("role", s.Role)
	addSpecString("initialPrompt", s.InitialPrompt)
	if s.Inherit {
		spec = append(spec, fmt.Sprintf("    %q:  true", "inherit"))
	}
	addSpecString("branch", s.Branch)
	addSpecString("workspaceUri", s.WorkspaceURI)
	addSpecString("model", s.Model)
	addSpecString("modelTier", s.ModelTier)
	if s.RunAsTask {
		spec = append(spec, fmt.Sprintf("    %q:  true", "runAsTask"))
	}
	addSpecString("share", s.Share)
	if s.Fork {
		spec = append(spec, fmt.Sprintf("    %q:  true", "fork"))
	}
	if len(spec) > 0 {
		top = append(top, "  \"spec\":  {\n"+strings.Join(spec, ",\n")+"\n  }")
	}
	var result []string
	addResultString := func(name, value string) {
		if value != "" {
			result = append(result, fmt.Sprintf("    %q:  %s", name, quoteJSON(value)))
		}
	}
	addResultString("conversationId", s.ConversationID)
	addResultString("logAbsoluteUri", s.LogAbsoluteURI)
	if len(s.WorkspaceURIs) > 0 {
		items := make([]string, 0, len(s.WorkspaceURIs))
		for _, uri := range s.WorkspaceURIs {
			items = append(items, "      "+quoteJSON(uri))
		}
		result = append(result, "    \"workspaceUris\":  [\n"+strings.Join(items, ",\n")+"\n    ]")
	}
	if len(result) > 0 {
		top = append(top, "  \"result\":  {\n"+strings.Join(result, ",\n")+"\n  }")
	}
	if s.Killed {
		top = append(top, "  \"killed\":  true")
	}
	if s.SpawnStepIndex != 0 {
		top = append(top, fmt.Sprintf("  \"spawnStepIndex\":  %d", s.SpawnStepIndex))
	}
	return "{\n" + strings.Join(top, ",\n") + "\n}"
}

// quoteJSON returns a JSON string literal with protojson's escaping: HTML
// characters stay literal (the live 1.5.3 block contains a raw "&").
func quoteJSON(v string) string {
	var b strings.Builder
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return `""`
	}
	return strings.TrimSuffix(b.String(), "\n")
}

// sectionGap returns the bytes that separate two sections by one blank
// line, given what the builder already holds.
func sectionGap(b *strings.Builder) string {
	if strings.HasSuffix(b.String(), "\n") {
		return "\n"
	}
	return "\n\n"
}

// ResumingBlock is ResumeBlock without the artifact and conversation-log
// sections. It is the shape the clone used before 1.5.3's tails were
// captured; callers that do not know the profile keep using it.
//
// summary is the model's reply, which already carries the reference's
// <summary></summary> wrapper because the instruction asks for it.
func ResumingBlock(userRequests []string, summary string) string {
	return ResumeBlock(ResumeOptions{UserRequests: userRequests, Summary: summary})
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
