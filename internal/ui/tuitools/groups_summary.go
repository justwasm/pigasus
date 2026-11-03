package tuitools

import (
	"fmt"
	"strings"
	"time"
)

// Step is one tool call (or thought) rendered in the conversation
// transcript. The cmd/wagyu side owns the model; this package owns the
// *grouping* rules that turn a batch of steps into the reference's
// summary rows ("Ran 2 commands (echo one · echo two)").
type Step struct {
	// Name is the recovered tool name ("run_command", "view_file", …).
	Name string
	// Title is the expanded call form without styling, e.g.
	// "Bash(echo one)" or "Read(/tmp/x)".
	Title string
	// Summary is the collapsed form, e.g. "Ran (echo one)". Empty falls
	// back to Title.
	Summary string
}

// MixedGroupSummary returns the three pieces of a batch header for a
// batch that mixes categories: the verb, the comma-joined counts, and
// the (empty) detail.
func MixedGroupSummary(steps []Step) (verb, count, detail string) {
	counts := map[string]int{}
	for _, s := range steps {
		counts[ToolCategory(s.Name)]++
	}
	parts := make([]string, 0, len(counts))
	for _, c := range CategoryOrder {
		if counts[c] > 0 {
			parts = append(parts, CountLabel(counts[c], CategoryWords[c]))
		}
	}
	return "Ran", strings.Join(parts, ", "), ""
}

// HomogeneousGroupSummary returns the summary for a batch whose steps
// all share one category.
func HomogeneousGroupSummary(cat string, steps []Step) (verb, count, detail string) {
	switch cat {
	case "command":
		verb = "Ran"
		count = CountLabel(len(steps), CategoryWords["command"])
		detail = "(" + strings.Join(StepArgs(steps, ParenArg), " · ") + ")"
	case "file":
		verb = "Explored"
		count = CountLabel(len(steps), CategoryWords["file"])
		detail = "(" + strings.Join(StepArgs(steps, FileNameArg), " · ") + ")"
	case "edit":
		verb = "Edited"
		count = CountLabel(len(steps), CategoryWords["edit"])
		detail = "(" + strings.Join(StepArgs(steps, FileNameArg), " · ") + ")"
	case "search":
		verb = "Explored"
		count = CountLabel(len(steps), CategoryWords["search"])
		detail = "(" + strings.Join(StepArgs(steps, SearchArg), " · ") + ")"
	default:
		verb = "Ran"
		count = CountLabel(len(steps), CategoryWords[cat])
	}
	return verb, count, detail
}

// GroupSummary returns the summary for a batch of steps: the mixed
// form when the batch spans more than one category, the homogeneous
// form otherwise.
func GroupSummary(steps []Step) (verb, count, detail string) {
	cat := ToolCategory(steps[0].Name)
	for _, s := range steps[1:] {
		if ToolCategory(s.Name) != cat {
			return MixedGroupSummary(steps)
		}
	}
	return HomogeneousGroupSummary(cat, steps)
}

// StepArgs extracts one display argument per step using pick.
func StepArgs(steps []Step, pick func(string) string) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		if a := pick(s.Title); a != "" {
			out = append(out, a)
			continue
		}
		out = append(out, s.Name)
	}
	return out
}

// SplitVerbArgs splits a title into verb, args and the separator that sat
// between them. A "(" before the first space means the args are glued to the
// verb ("Bash(cmd)"), otherwise the separator is the single space.
func SplitVerbArgs(title string) (verb, args, sep string) {
	space := strings.IndexByte(title, ' ')
	paren := strings.IndexByte(title, '(')
	if paren >= 0 && (space < 0 || paren < space) {
		return title[:paren], title[paren:], ""
	}
	if space >= 0 {
		return title[:space], title[space+1:], " "
	}
	return title, "", ""
}

// HumanDuration renders a duration the way the reference does: whole seconds
// stay integral ("3s"), sub-second parts keep one decimal ("1.5s"), and
// anything under a second is shown in milliseconds.
func HumanDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return d.Round(100 * time.Millisecond).String()
}

// ThoughtTitle renders the completed-thought summary. The duration uses Go's
// Duration.String() shape ("3s", "1.5s"), which matches the reference's
// "Thought for %s" / "Thought for %.1fs" pair for the common cases.
func ThoughtTitle(d time.Duration, tokens int) string {
	ds := HumanDuration(d)
	if tokens > 0 {
		return fmt.Sprintf("Thought for %s, %d tokens", ds, tokens)
	}
	return "Thought for " + ds
}
