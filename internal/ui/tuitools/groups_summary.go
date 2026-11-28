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

// BatchSummary is one batch header, split into the three pieces the
// renderer styles differently: the verb is the bold-orange lead, the count
// is body-coloured, and the parenthesised detail is dim gray.
//
// The three are one value rather than three returns because they are
// produced together, consumed together, and mean nothing apart. As three
// returns every caller had to re-assemble them, and every test had to
// destructure all three even when it asserted one.
//
//	\x1b[38;2;181;137;0;1mRan\x1b[m \x1b[38;2;7;54;66m2 commands\x1b[m \x1b[38;2;88;110;117m(echo one · echo two)\x1b[m
type BatchSummary struct {
	// Verb is the lead word: "Ran", "Explored" or "Edited".
	Verb string
	// Count is the pluralised tally: "2 commands", "1 file".
	Count string
	// Detail is the parenthesised argument list, or "" when the batch
	// has none.
	Detail string
}

// String renders the header as one line, the way the reference's plain
// form reads: "Ran 2 commands (echo one · echo two)".
func (b BatchSummary) String() string {
	out := b.Verb + " " + b.Count
	if b.Detail != "" {
		out += " " + b.Detail
	}
	return out
}

// categoryRule is how one tool category names itself in a batch header.
//
// The table below is the whole rule: a category's verb and the argument
// picker its detail list uses. Categories that are absent from it fall
// back to verb "Ran" and no detail, which is the reference's behaviour for
// the categories it does not name.
//
// The count is not in the table because it is the same everywhere —
// CountLabel with the category's own words — and a field that every entry
// repeats identically is a field that can drift.
type categoryRule struct {
	verb string
	// arg picks one display argument per step; nil means the detail is
	// empty for this category.
	arg func(string) string
}

// categoryRules is the per-category half of the header rules.
var categoryRules = map[string]categoryRule{
	"command": {verb: "Ran", arg: ParenArg},
	"file":    {verb: "Explored", arg: FileNameArg},
	"edit":    {verb: "Edited", arg: FileNameArg},
	"search":  {verb: "Explored", arg: SearchArg},
}

// MixedGroupSummary returns the header for a batch that mixes categories:
// the verb "Ran", the comma-joined counts in CategoryOrder, and no detail.
func MixedGroupSummary(steps []Step) BatchSummary {
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
	return BatchSummary{Verb: "Ran", Count: strings.Join(parts, ", ")}
}

// HomogeneousGroupSummary returns the header for a batch whose steps all
// share one category.
func HomogeneousGroupSummary(cat string, steps []Step) BatchSummary {
	rule, ok := categoryRules[cat]
	if !ok {
		return BatchSummary{Verb: "Ran", Count: CountLabel(len(steps), CategoryWords[cat])}
	}
	sum := BatchSummary{Verb: rule.verb, Count: CountLabel(len(steps), CategoryWords[cat])}
	if rule.arg != nil {
		sum.Detail = "(" + strings.Join(StepArgs(steps, rule.arg), " · ") + ")"
	}
	return sum
}

// GroupSummary returns the header for a batch of steps: the mixed form
// when the batch spans more than one category, the homogeneous form
// otherwise.
func GroupSummary(steps []Step) BatchSummary {
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
