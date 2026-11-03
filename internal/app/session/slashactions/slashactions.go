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

// PlanPrompt returns the prompt /plan sends to the model.
func PlanPrompt() string {
	return "Help me create a plan for the work I just described. Outline the next concrete steps, call out anything that needs clarification before we start, and end with the single question I should answer first."
}

// GoalPrompt returns the prompt /goal sends to the model.
func GoalPrompt() string {
	return "Help me set a clear goal for this session. Restate the goal in one sentence, list the success criteria, and flag anything that could pull us off-track."
}

// LearnPrompt returns the prompt /learn sends to the model.
func LearnPrompt() string {
	return "Capture a learning from what I just discussed. Write it as a short note I can refer back to, with the takeaway up front and any open questions below."
}

// TeamworkPrompt returns the prompt /teamwork sends to the model.
func TeamworkPrompt() string {
	return "Coordinate a team of autonomous agents to tackle this project. Decompose the goal into clear milestones, assign specialized roles, track progress systematically, and verify outcomes before synthesizing the final result."
}

// GrillMePrompt returns the prompt /grill-me sends to the model.
func GrillMePrompt() string {
	return "Interrogate my reasoning on the topic I just raised. Ask probing questions that expose weak assumptions, missing evidence, or unstated tradeoffs. Keep going until I've either tightened the argument or admitted it's not yet defensible."
}

// SchedulePrompt is the prompt /schedule sends to the model.
func SchedulePrompt() string {
	return "Register this instruction to run later, as a one-time timer or on a recurring schedule. Tell me when it is registered, when it will first run, and what it will do."
}

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


