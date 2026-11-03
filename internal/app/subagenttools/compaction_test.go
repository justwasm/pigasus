// compaction_test.go — the subagent's own compaction summary (agy 1.4.10).
package subagenttools

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding/extension"
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

	prep := extension.CompactionPreparation{
		FirstKeptEntryID: "entry-7",
		TokensBefore:     4242,
		MessagesToSummarize: []piagent.AgentMessage{
			{Assistant: &piagent.AssistantMessage{
				Role:    piagent.RoleAssistant,
				Content: []pi.AssistantContentBlock{pi.TextContent{Text: "I found the changelog."}},
			}},
			{Assistant: &piagent.AssistantMessage{
				Role: piagent.RoleAssistant,
				Content: []pi.AssistantContentBlock{pi.ToolCall{
					ID:   "call-1",
					Name: "read_file",
				}},
			}},
			{ToolResult: &piagent.ToolResultMessage{
				Role:     piagent.RoleToolResult,
				ToolName: "read_file",
				Content:  []pi.ToolResultMessageContent{pi.TextContent{Text: "the file's text"}},
			}},
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
	var msgs []piagent.AgentMessage
	for i := 0; i < MaxDigestLines+10; i++ {
		msgs = append(msgs, piagent.AgentMessage{
			Assistant: &piagent.AssistantMessage{
				Role:    piagent.RoleAssistant,
				Content: []pi.AssistantContentBlock{pi.TextContent{Text: "step"}},
			},
		})
	}
	lines := digestMessages(msgs)
	if len(lines) != MaxDigestLines {
		t.Fatalf("digest has %d lines, want %d", len(lines), MaxDigestLines)
	}
}

// TestLoadParentContextExtensionAnswersTheHook pins that the extension the
// clone loads into a subagent's session answers PiG's compaction hook with
// a summary that carries the parent's half.
func TestLoadParentContextExtensionAnswersTheHook(t *testing.T) {
	pctx := &ParentContext{ID: "parent-id", Task: "Do the thing", TaskAt: time.Unix(1, 0)}

	exts, extRuntime := minimalBuiltinExtensions()
	exts = loadParentContextExtension(exts, extRuntime, pctx)
	if len(exts) == 0 {
		t.Fatal("no extension loaded")
	}

	var last *extension.Extension
	for i := range exts {
		if exts[i].Path == ParentContextExtensionName {
			last = &exts[i]
		}
	}
	if last == nil {
		t.Fatalf("the parent-context extension is not among the loaded extensions")
	}
	handlers := last.EventHandlers("session_before_compact")
	if len(handlers) == 0 {
		t.Fatal("the extension registers no session_before_compact handler")
	}

	evt := extension.SessionBeforeCompactEvent{
		Type: "session_before_compact",
		Preparation: extension.CompactionPreparation{
			FirstKeptEntryID: "entry-3",
			TokensBefore:     99,
		},
	}
	raw, err := handlers[0](&evt, context.Background())
	if err != nil {
		t.Fatalf("handler: %v", err)
	}
	result, ok := raw.(*extension.SessionBeforeCompactResult)
	if !ok {
		t.Fatalf("handler returned %T, want *extension.SessionBeforeCompactResult", raw)
	}
	if result.Compaction == nil {
		t.Fatal("the handler carried no compaction")
	}
	if result.Compaction.FirstKeptEntryID != "entry-3" {
		t.Fatalf("first kept = %q, want %q", result.Compaction.FirstKeptEntryID, "entry-3")
	}
	if result.Compaction.TokensBefore != 99 {
		t.Fatalf("tokens before = %d, want 99", result.Compaction.TokensBefore)
	}
	if !strings.Contains(result.Compaction.Summary, "Do the thing") {
		t.Fatalf("the summary does not carry the task:\n%s", result.Compaction.Summary)
	}
	if !strings.Contains(result.Compaction.Summary, "parent-id") {
		t.Fatalf("the summary does not carry the parent's id:\n%s", result.Compaction.Summary)
	}
}

// TestTheSummarySurvivesTheWire pins that the summary reaches PiG through
// the JSON envelope the hook's result travels in.
func TestTheSummarySurvivesTheWire(t *testing.T) {
	pctx := &ParentContext{ID: "parent-id", Task: "Do the thing", TaskAt: time.Unix(1, 0)}
	summary := SubagentSummary(pctx, extension.CompactionPreparation{FirstKeptEntryID: "e"})
	raw, err := json.Marshal(extension.SessionBeforeCompactResult{
		Compaction: &extension.CompactionResult{Summary: summary, FirstKeptEntryID: "e"},
	})
	if err != nil {
		t.Fatal(err)
	}
	var envelope struct {
		Compaction struct {
			Summary          string `json:"summary"`
			FirstKeptEntryID string `json:"firstKeptEntryId"`
		} `json:"compaction"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.Compaction.Summary != summary {
		t.Fatalf("summary = %q, want %q", envelope.Compaction.Summary, summary)
	}
	if envelope.Compaction.FirstKeptEntryID != "e" {
		t.Fatalf("first kept = %q, want %q", envelope.Compaction.FirstKeptEntryID, "e")
	}
}
