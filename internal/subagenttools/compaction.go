// compaction.go — the subagent's own compaction summary.
//
// 【一手】 agy 1.4.10, the vendor's own release notes:
//
//	Fixed subagents losing the task their parent agent gave them, and the
//	parent's ID, after their context was compacted, including with
//	`/compact`; a subagent's summary now keeps the parent agent's 10 most
//	recent messages.
//
// The clone's subagent runs on PiG's `coding.Session`, and PiG compacts
// a session by asking the model to summarize the part of the history
// that no longer fits. The summary it writes knows about the subagent's
// own turns — the model's own work — and nothing about the parent: the
// task the parent gave and the id the parent is answered on are the
// *oldest* messages of the conversation, so they are the first ones the
// summary drops.
//
// The extension below answers PiG's `session_before_compact` hook with a
// summary of its own, one that carries the parent's half (see
// parentcontext.go) beside a digest of the subagent's own work.
//
// 【推测】 The reference's own summary is written by its model, and its
// exact shape is not recoverable from the release notes. The clone's
// summary is therefore its own document: the three things the note names
// (the task, the parent's id, the parent's ten most recent messages),
// and a digest of what the subagent did.
package subagenttools

import (
	"context"
	"encoding/json"
	"strings"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"
	"github.com/MichaelKinsy/PiG/coding/extension"
	"github.com/MichaelKinsy/PiG/coding/extension/factoryload"
)

const (
	// MaxDigestLines is how many lines of the subagent's own work the
	// summary carries. The digest is a fallback for the model's own
	// summary, so it is bounded: a subagent that worked for a long time
	// keeps its most recent steps, not all of them.
	MaxDigestLines = 40

	// maxTextSnippet is how much of one assistant message the digest
	// keeps.
	maxTextSnippet = 400

	// maxArgsSnippet is how much of one tool call's arguments the
	// digest keeps.
	maxArgsSnippet = 200

	// maxResultSnippet is how much of one tool result the digest keeps.
	maxResultSnippet = 200
)

// ParentContextExtensionName is the name the clone's own extension is
// loaded under. It shows up in PiG's diagnostics and in the startup
// extension list.
const ParentContextExtensionName = "subagent-parent-context"

// parentContextExtension builds the extension that answers PiG's
// compaction hook for one subagent.
//
// The extension is loaded into the subagent's own session, so its
// summary is the subagent's summary: the parent's half travels with the
// session and not with the process.
func parentContextExtension(pctx *ParentContext) extension.ExtensionFactory {
	return func(api extension.API) error {
		api.OnSessionBeforeCompact(func(_ context.Context, evt extension.SessionBeforeCompactEvent) (extension.SessionBeforeCompactResult, error) {
			// A preparation without a kept entry is not a compaction
			// PiG can carry out; leaving the result empty lets PiG
			// answer with its own error.
			if evt.Preparation.FirstKeptEntryID == "" {
				return extension.SessionBeforeCompactResult{}, nil
			}
			return extension.SessionBeforeCompactResult{
				Compaction: &extension.CompactionResult{
					Summary:          SubagentSummary(pctx, evt.Preparation),
					FirstKeptEntryID: evt.Preparation.FirstKeptEntryID,
					TokensBefore:     evt.Preparation.TokensBefore,
				},
			}, nil
		})
		return nil
	}
}

// loadParentContextExtension loads the parent-context extension into an
// extension set.
//
// The runtime is the one the built-in extensions were loaded into: the
// extension host that holds it must be the host the session's runner
// uses, or the handler never runs.
func loadParentContextExtension(exts []extension.Extension, extRuntime *extension.ExtensionRuntime, pctx *ParentContext) []extension.Extension {
	if pctx == nil {
		return exts
	}
	ext, err := factoryload.LoadExtensionFromFactory(
		parentContextExtension(pctx), "", nil, extRuntime, ParentContextExtensionName,
	)
	if err != nil {
		// A subagent without the extension still runs: it just loses
		// the parent's half when its context is compacted, which is
		// what the reference used to do.
		return exts
	}
	return append(exts, ext)
}

// SubagentSummary renders the summary a subagent's compaction installs.
//
// It is the parent's half (the task, the parent's id, the parent's ten
// most recent messages) followed by a digest of the subagent's own work.
func SubagentSummary(pctx *ParentContext, prep extension.CompactionPreparation) string {
	var b strings.Builder
	if parent := pctx.Summary(); parent != "" {
		b.WriteString(parent)
		b.WriteString("\n\n")
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
//
// The lines are the steps the subagent took — what it said, what it
// called, what the call answered — because those are what a summary
// that replaces the history has to keep.
func digestMessages(msgs []piagent.AgentMessage) []string {
	var out []string
	for _, msg := range msgs {
		switch {
		case msg.Assistant != nil:
			for _, block := range msg.Assistant.Content {
				switch content := block.(type) {
				case pi.TextContent:
					if text := strings.TrimSpace(content.Text); text != "" {
						out = append(out, "said: "+snippet(text, maxTextSnippet))
					}
				case pi.ToolCall:
					out = append(out, "called "+content.Name+"("+snippet(callArguments(content), maxArgsSnippet)+")")
				}
			}
		case msg.ToolResult != nil:
			out = append(out, "tool "+msg.ToolResult.ToolName+" answered: "+snippet(toolResultText(msg.ToolResult), maxResultSnippet))
		}
	}
	if len(out) > MaxDigestLines {
		out = out[len(out)-MaxDigestLines:]
	}
	return out
}

// callArguments renders a tool call's arguments as JSON.
func callArguments(call pi.ToolCall) string {
	if raw, err := call.ArgumentsJSON(); err == nil && len(raw) > 0 {
		return string(raw)
	}
	if len(call.Arguments) == 0 {
		return ""
	}
	raw, err := json.Marshal(call.Arguments)
	if err != nil {
		return ""
	}
	return string(raw)
}

// toolResultText renders a tool result's text.
func toolResultText(result *piagent.ToolResultMessage) string {
	var b strings.Builder
	for _, block := range result.Content {
		switch content := block.(type) {
		case pi.TextContent:
			b.WriteString(content.Text)
			b.WriteString("\n")
		default:
			if raw, err := json.Marshal(block); err == nil {
				b.Write(raw)
				b.WriteString("\n")
			}
		}
	}
	return strings.TrimSpace(b.String())
}

// snippet shortens a text to at most n characters, marking the cut.
func snippet(text string, n int) string {
	text = strings.TrimSpace(text)
	if n <= 0 || len(text) <= n {
		return text
	}
	return text[:n] + "…"
}
