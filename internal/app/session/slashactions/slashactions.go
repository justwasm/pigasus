// Package slashactions implements the pure resolution and feedback logic
// for slash commands, decoupled from Bubble Tea and UI state.
//
// Following the architecture tier rules, this package belongs to tier 4
// (internal/app/session) and depends only on lower tiers (core, features, infra).
package slashactions

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	"github.com/justwasm/pigasus/internal/core/titlefmt"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
)

// ClipboardWrite is clipboard.Write, indirected so tests can exercise the
// copy handler without touching the real system clipboard.
var ClipboardWrite = clipboard.Write

// PlannerResponses extracts the assistant ("planner") response contents in order
// from the given session messages.
func PlannerResponses(session []backend.Message) []string {
	var out []string
	for _, msg := range session {
		if msg.Role == "assistant" && strings.TrimSpace(msg.Content) != "" {
			out = append(out, msg.Content)
		}
	}
	return out
}

// ResolveCopy implements /copy and /copy <n>. It returns the text to
// put on the clipboard ("" when there is nothing) and the message to show.
//
// Indexing is 1-based from the most recent response.
func ResolveCopy(args string, responses []string) (string, string) {
	args = slashparse.Trim(args)
	if args == "" {
		if len(responses) == 0 {
			return "", "No response to copy."
		}
		return responses[len(responses)-1], "Copied last response to clipboard."
	}
	n, err := strconv.Atoi(args)
	if err != nil || n < 1 || n > len(responses) {
		if err != nil || n < 1 {
			// Not an index at all: behave like the bare form.
			if len(responses) == 0 {
				return "", "No response to copy."
			}
			return responses[len(responses)-1], "Copied last response to clipboard."
		}
		return "", "No response to copy."
	}
	return responses[len(responses)-n], fmt.Sprintf("Copied response %d to clipboard.", n)
}

// ConversationTitle determines the title for /title.
// A configured name takes precedence, followed by the first user message,
// and finally fallbackName (typically the binary name).
func ConversationTitle(configuredName string, session []backend.Message, fallbackName string) string {
	if s := titlefmt.Normalize(configuredName); s != "" {
		return s
	}
	for _, msg := range session {
		if msg.Role == "user" {
			if s := titlefmt.Normalize(msg.Content); s != "" {
				return s
			}
		}
	}
	if fallbackName != "" {
		return filepath.Base(fallbackName)
	}
	if len(os.Args) > 0 {
		return filepath.Base(os.Args[0])
	}
	return "pigasus"
}

// MCPServersFeedback renders the textual status table for /mcp.
func MCPServersFeedback(cfg *auth.MCPConfig) string {
	if cfg == nil || len(cfg.Servers) == 0 {
		return "No MCP servers configured."
	}
	names := make([]string, 0, len(cfg.Servers))
	for name := range cfg.Servers {
		names = append(names, name)
	}
	sort.Strings(names)
	mcpsBlocked := auth.MCPsBlocked()
	var b strings.Builder
	b.WriteString("MCP servers")
	for _, name := range names {
		s := cfg.Servers[name]
		state := "disabled"
		if s.Enabled {
			state = "enabled"
		}
		if mcpsBlocked {
			state = "blocked"
		}
		target := s.URL
		if target == "" {
			target = strings.TrimSpace(s.Command + " " + strings.Join(s.Args, " "))
		}
		fmt.Fprintf(&b, "\n  %s\t%s\t%s\t%s", name, s.Type, state, target)
	}
	return b.String()
}

// PlanPrompt returns the reference's /plan instruction text.
//
// It is NOT what /plan puts on the wire. /plan is an agent mode, not a
// prompt-sending command: the reference sets the mode and sends the turn
// verbatim, and 20 live 1.5.18 captures (TUI, `--mode plan`, `-p`) contain
// zero occurrences of this text. The clone used to prepend it, sending the
// model a block the reference never sends; the TUI path now sends the
// verbatim turn (see internal/tui/slash_plan_test.go).
//
// The text is still recovered and pinned (devutils/wirealign marks it
// NotSent) because it is the reference's own explanation of what plan mode
// asks for — but nothing forwards it to the model. See planPromptReference
// for the offset.
func PlanPrompt() string { return planPromptReference }

// GoalPrompt returns the prompt /goal sends to the model.
//
// 【一手】 the reference's own text: see goalPromptReference for the offset,
// the live-capture confirmation, and why the markers in it matter.
func GoalPrompt() string { return goalPromptReference }

// LearnPrompt returns the prompt /learn sends to the model.
//
// 【一手】 recovered verbatim from the agy 1.5.18 binary: the reference's
// whole `/learn` instruction sits in one literal at file offset 0x3ee0d64,
// 1,965 B, wrapped in `<LEARN>` … `</LEARN>`. The body is reproduced here
// without the wrapper — the clone's other prompt-sending commands (`/goal`,
// `/plan`, `/grill-me`) send bare prose too, and the wrapper is the
// reference's own private framing rather than something the model reads.
//
// One step in the reference is a `%s` the binary fills from a second literal
// (`Check the `%s` skill in the skills section …`, with `%s` = the
// customization skill's name, `agy-customizations` on this machine). It is
// spelled out below.
//
// The 1.5.8 changelog is what this text exists for: "Fixed `/learn`
// repeatedly appending to rule files such as `GEMINI.md` or `AGENTS.md` until
// they grew past the rule size limit, after which their endings were silently
// cut off on every turn. Before updating a rule, `/learn` now checks the
// documented size limits, and when an update would get close to or exceed
// them, it proposes condensing existing entries, splitting them into a scoped
// rule, or moving step-by-step guidance into a skill."
//
// The clone used to send a one-sentence summary of that fix ("Capture a
// learning … verifying that rule files … remain within their 24 KB … limit").
// A summary is not the same instruction: it drops the classification step
// (rule vs skill), the update-before-create preference, the four-step
// proposal workflow and the artifact contract, so the model had no reason to
// produce a `learning_proposal.md` or to wait for approval.
func LearnPrompt() string {
	return `The user invoked /learn to persist reusable behaviors from recent interactions, corrections, or successes. Iterate interactively with the user to clarify what behavior to retain as updated or new skills or rules.

## Identify What to Learn
1. **Analyze User Messages**: Prioritize analyzing recent user messages for explicit corrections, constraints, overrides, or pointers (e.g., "no", "instead", "that failed").
2. **Identify the Fix**: Compare failed attempts with the successful resolution to isolate the pivotal change.
3. **Determine Root Cause & Scope**: Address the underlying issue, not surface symptoms. Determine if it's universal or domain-specific.
4. **Verify if learning is needed**: If the interaction did not reveal any new reusable behaviors or constraints, explain this to the user and exit without proposing changes.

## Classify Rules vs. Skills
1. **Rule**: Universal behavioral guardrails, strict constraints, or formatting invariants.
2. **Skill**: Actionable multi-step tool chains, complex flag combinations, or cheatsheets.

## Create vs. Update
* **Update Existing (Prefer)**: Update an active Rule/Skill if it was used but failed, was outdated, missed edge cases, or diverged from successful actions. If updating a target file would approach or exceed its documented size/budget limits (or it is already truncated in context), propose condensing it or splitting/extracting into a scoped rule or skill instead of appending.
* **Create New**: When the behavior covers an entirely new domain or guardrail not covered by existing rules or skills, or when splitting an oversized file.

## Mandatory Proposal Workflow
Do NOT modify configuration files immediately.
1. Check the ` + "`agy-customizations`" + ` skill in the skills section (including its relevant guide) and inspect existing workspace rules/skills to follow their exact schema, frontmatter, directory conventions, and size/budget limits.
2. Create/update a learning_proposal.md artifact outlining your classification, rationale, and precise text additions/diffs.
3. Set request_feedback = true in ArtifactMetadata for user review.
4. Only execute file/tool modifications after explicit user approval.
`
}

// TeamworkPrompt returns the prompt /teamwork sends to the model.
//
// 【一手】 the reference's own text, with its two indexed verbs filled:
// `invoke_subagent` (the tool) and `sentinel` (the team's root type). See
// teamworkPromptReference for the offset and the evidence for each name.
func TeamworkPrompt() string { return teamworkPromptReference }

// GrillMePrompt returns the prompt /grill-me sends to the model.
//
// 【一手】 recovered verbatim from the agy 1.5.18 binary: the whole
// `/grill-me` instruction is one 491 B literal at file offset 0x3e73ac4,
// wrapped in `<GRILL_ME>` … `</GRILL_ME>`. The body is reproduced here
// without the wrapper, the way the other prompt-sending commands send bare
// prose, and there is no `%s` in it — unlike /learn, nothing is filled in at
// send time.
//
// The sentence the clone used to send ("Interrogate my reasoning on the topic
// I just raised. Ask probing questions that expose weak assumptions…") is not
// in the 1.5.18 binary at all — it was written here, not recovered. It also
// asked for something the reference does not: an adversarial interrogation.
// The reference's instruction is a *structured design interview* — walk the
// design tree, resolve decision dependencies one at a time, recommend an
// answer for each question — and it names three concrete guidelines, the last
// of which (`Use the ask_question tool`) is the one the model needs in order
// to ask through the UI rather than in prose.
func GrillMePrompt() string {
	return `The user has requested that you interview them about every aspect of their task until you've reach a shared understanding. Walk down each branch of the design tree, resolving dependencies between decisions one-by-one. For each question, provide your recommended answer.

Guidelines:
- Ask the questions one at a time.
- If a question can be answered by exploring the codebase, explore the codebase instead.
- Use the ask_question tool for asking questions to the user.
`
}

// SchedulePromptReference is the agy 1.5.18 /schedule expansion body.
//
// 【一手】 the literal at file offset 0x3df6776, 74 B, no wrapper tag — like
// /goal, /schedule ships its body bare. Confirmed on the wire: `/schedule run
// the tests every morning` produces the turn verbatim plus
//
//	/schedule is a [Slash Command]:
//	Please use the schedule tool to create a schedule as specified by the user
//
// The clone used to send an invented three-clause sentence ("Register this
// instruction to run later… Tell me when it is registered…") that the
// reference never sends; the reference's whole instruction is one line telling
// the model which tool to call.
const SchedulePromptReference = "Please use the schedule tool to create a schedule as specified by the user"

// SchedulePrompt is the prompt /schedule sends to the model.
//
// Deprecated shape note: the TUI now sends this as a mention expansion (see
// sendAsMention), not as a prepended turn. Kept as the accessor the wirealign
// pin compares against.
func SchedulePrompt() string { return SchedulePromptReference }

// TasksCount counts running and done tasks based on standard task states.
func TasksCount(states []string) (running, done int) {
	for _, s := range states {
		switch s {
		case "running", "pending":
			running++
		case "done", "failed", "killed":
			done++
		}
	}
	return running, done
}

// TasksFeedback returns the human-readable summary of running and completed tasks.
func TasksFeedback(running, done int) string {
	if running == 0 && done == 0 {
		return "No background tasks."
	}
	return fmt.Sprintf("%d running, %d done", running, done)
}

// TitleVerbKind represents the parsed action for a /title invocation.
type TitleVerbKind int

const (
	TitleVerbInvalid TitleVerbKind = iota
	TitleVerbOn
	TitleVerbOff
	TitleVerbToggle
)

// ParseTitleVerb parses the argument given to /title into an action kind.
func ParseTitleVerb(args string) TitleVerbKind {
	verb := strings.ToLower(strings.TrimSpace(args))
	switch verb {
	case "on", "enable":
		return TitleVerbOn
	case "off", "disable":
		return TitleVerbOff
	case "":
		return TitleVerbToggle
	default:
		return TitleVerbInvalid
	}
}

// ResolveTitleToggle determines the next enabled state based on verb and current state.
func ResolveTitleToggle(verb TitleVerbKind, currentlyEnabled bool) bool {
	switch verb {
	case TitleVerbOn:
		return true
	case TitleVerbOff:
		return false
	case TitleVerbToggle:
		return !currentlyEnabled
	default:
		return currentlyEnabled
	}
}
