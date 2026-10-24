package cliapp

import (
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/tuitools"
)

func toolBlockLines(steps []toolStep, expanded bool, width int) []string {
	if len(steps) == 0 {
		return nil
	}
	// Thought steps are not part of a tool group: the reference prints
	// them as their own line above the calls.
	var thoughts, tools []toolStep
	for _, s := range steps {
		if s.kind == stepKindThought {
			thoughts = append(thoughts, s)
		} else {
			tools = append(tools, s)
		}
	}
	var out []string
	for _, s := range thoughts {
		out = append(out, thoughtStepLines(s, expanded, width)...)
	}
	switch len(tools) {
	case 0:
	case 1:
		out = append(out, stepLines(tools[0], expanded, width)...)
	default:
		out = append(out, groupLines(tools, expanded, width)...)
	}
	if len(out) == 0 {
		return nil
	}
	// The reference keeps the hint on the collapsed summary line, and in
	// expanded mode on the very last detail row (verified against 1.4.3:
	// "● Bash(echo one)" has no hint, the trailing "one (ctrl+o to
	// collapse)" does). A step with nothing to show keeps the hint on its
	// head so it is never a bare hint.
	hint := "ctrl+o to expand"
	if expanded {
		hint = "ctrl+o to collapse"
	}
	out[len(out)-1] = out[len(out)-1] + " " + styledHint(hint)
	return out
}

// stepLines renders one call. Collapsed is the reference's one-line summary
// ("Ran (echo one)"), expanded is the call form plus its detail rows.
func stepLines(s toolStep, expanded bool, width int) []string {
	if s.running || !expanded {
		line := styledStepBullet(s, false) + " " + styledSummary(s)
		if s.err && !s.running && toolCategory(s.name) == "command" {
			// Only a non-zero *command* carries the word (measured: a
			// failed search_web reddens the bullet but prints no
			// " failed").
			line += " " + cToolArgsOpen + "failed" + cToolArgsClose
		}
		return []string{line}
	}
	out := renderStepTitle(s, true)
	return append(out, stepDetailLines(s, width)...)
}

// categoryGroups splits a batch into per-category runs, in first-appearance
// order. 1.4.3 prints one summary row per category rather than one mixed
// header (measured: a read_file + edit_file batch rendered
// "● Explored 1 file (w285b.txt)" above "● Edited 1 file (w285b.txt)").
func categoryGroups(tools []toolStep) [][]toolStep {
	var groups [][]toolStep
	idx := map[string]int{}
	for _, s := range tools {
		c := toolCategory(s.name)
		if i, ok := idx[c]; ok {
			groups[i] = append(groups[i], s)
			continue
		}
		idx[c] = len(groups)
		groups = append(groups, []toolStep{s})
	}
	return groups
}

// groupLines renders a batch of calls emitted together (parallel tool
// calls). Collapsed shows one summary row per category; expanded keeps those
// rows and lists each call underneath them. Verified against 1.4.3:
//
//	● Ran 2 commands (echo one · echo two) (ctrl+o to expand)
//	● Ran 2 commands (echo one · echo two)
//	● Bash(echo one)
//	  ⎿  Command exited with code 0. …
//	● Bash(echo two)
//	  ⎿  … two (ctrl+o to collapse)
//
// A mixed batch keeps the same shape, one row per category:
//
//	● Explored 1 file (w285b.txt)
//	● Edited 1 file (w285b.txt)
//	● Read(/tmp/w285b.txt)   ← no detail: one-line file
//	● Edit(/tmp/w285b.txt)
//	  ⎿  +1 / -1 lines …
func groupLines(tools []toolStep, expanded bool, width int) []string {
	var out []string
	for _, g := range categoryGroups(tools) {
		out = append(out, styledStepBullet(g[0], expanded)+" "+styledGroupSummary(g))
	}
	if !expanded {
		return out
	}
	for _, s := range tools {
		out = append(out, renderStepTitle(s, true)...)
		out = append(out, stepDetailLines(s, width)...)
	}
	return out
}

// thoughtStepLines renders a thought step: "▸ Thought for 3s" plus, when
// expanded, the reasoning body.
func thoughtStepLines(s toolStep, expanded bool, width int) []string {
	head := thoughtBulletOpen + " " + thoughtTitle(s.dur, s.tokens)
	if !expanded {
		return []string{head}
	}
	out := []string{head}
	return append(out, stepDetailLines(s, width)...)
}

// hasDetail reports whether the step carries a body worth expanding.
func hasDetail(s toolStep) bool {
	return strings.TrimSpace(stepDetailText(s)) != ""
}

// stepDetailText maps a tool result onto the text the reference's expanded
// step actually shows. The tool *result* and the transcript *display* are
// different strings in 1.4.3:
//
//	run_command result:  Created At: …\nCompleted At: …\n\nThe command
//	                     exited with code 0.\nOutput:\n<out>\n
//	run_command display: Command exited with code 0.\n\nCommand output:\n<out>
//	                     (empty output: "No output from the command.")
//	view_file result:    <file bytes>
//	view_file display:   Read 3 lines
//
// Verified against agy 1.4.3 with `echo one`, `true` and `false`
// (docs/cycles/cycle-285-tool-display/deliverable.md). Anything we have not
// captured (write/replace/search) keeps the raw result.
//
// The rule itself now lives in internal/tuitools.
func stepDetailText(s toolStep) string {
	return tuitools.StepDetailText(s.name, s.detail, s.kind == stepKindThought)
}

// readDetailText renders the "Read N lines" summary a read tool shows.
func readDetailText(result string) string {
	return tuitools.ReadDetailText(result)
}

// renderMarkdownDetail renders a markdown payload the way the reference
// does, at the given width.
func renderMarkdownDetail(detail string, width int) string {
	return tuitools.RenderMarkdownDetail(detail, width)
}

// formatDetailLines renders the body rows of an expanded step, capping
// the count and adding the overflow marker.
func formatDetailLines(lines []string, diffMode bool) []string {
	return tuitools.FormatDetailLines(lines, diffMode)
}

func stepDetailLines(s toolStep, width int) []string {
	if s.diff != nil {
		return s.diff.Lines(width)
	}
	detail := strings.TrimRight(normalizeDetailNewlines(stepDetailText(s)), "\n")
	if strings.TrimSpace(detail) == "" {
		return nil
	}
	body := detail
	diffMode := looksLikeDiff(detail)
	if !diffMode && looksLikeMarkdown(detail) {
		body = renderMarkdownDetail(detail, width)
	}
	return formatDetailLines(strings.Split(body, "\n"), diffMode)
}

// normalizeDetailNewlines folds CRLF and bare CR into LF.
//
// The reference renders a carriage return as a line break, so a tool
// result that carries CRLF line endings collapses into a single line in
// the transcript — and the expanded output renders blank. That is why
// the detail only looked broken after ctrl+o — while collapsed the body
// is not drawn.
//
// This mirrors internal/convstore.cleanToolResult (cycle 178), which was
// the same fix on the resumed-conversation path. Display-only: never
// write the normalised string back into s.detail / m.response / m.session.
func normalizeDetailNewlines(s string) string {
	return tuitools.NormalizeDetailNewlines(s)
}

// looksLikeDiff returns true if s contains markers that identify
// the body as a unified-diff payload. Cycle 178 uses this in
// stepDetailLines to gate the per-line diff colouring pass —
// before cycle 178 a `- pattern` line in a non-diff payload
// (cat .gitignore, a YAML list, shell command output) was
// spuriously coloured red.
//
// The rule itself now lives in internal/tuitools.
func looksLikeDiff(s string) bool {
	return tuitools.LooksLikeDiff(s)
}

// looksLikeMarkdown returns true if s contains Markdown markers that
// goldmark would recognise on parse.
//
// The rule itself now lives in internal/tuitools.
func looksLikeMarkdown(s string) bool {
	return tuitools.LooksLikeMarkdown(s)
}

// styleDiffLine returns line with its leading `+`/`-` column wrapped
// in the recovered diff add/remove colour, when applicable.
//
// The rule itself now lives in internal/tuitools.
func styleDiffLine(line string) string {
	return tuitools.StyleDiffLine(line)
}

// toolStepLines renders a single step in isolation (used for the live,
// in-flight transcript before the turn finalises into a block).
func toolStepLines(s toolStep, expanded bool, width int) []string {
	return toolBlockLines([]toolStep{s}, expanded, width)
}

func renderStepTitle(s toolStep, expanded bool) []string {
	if s.kind == stepKindThought {
		return []string{thoughtBulletOpen + " " + thoughtTitle(s.dur, s.tokens)}
	}
	return []string{styledStepBullet(s, expanded) + " " + styledCallTitle(s)}
}

var categoryWords = tuitools.CategoryWords
var categoryOrder = tuitools.CategoryOrder
var toolCategories = tuitools.ToolCategories

// toolCategory maps a tool name onto its count category.
func toolCategory(name string) string {
	return tuitools.ToolCategory(name)
}

// countLabel renders "<n> <word>" using the reference's singular form when n is one.
func countLabel(n int, word [2]string) string {
	return tuitools.CountLabel(n, word)
}

// groupSummary returns the three styled pieces of a batch header:
// "Ran" + "2 commands" + "(echo one \u00b7 echo two)". Verified against 1.4.3.
func groupSummary(steps []toolStep) (verb, count, detail string) {
	return tuitools.GroupSummary(toolStepsFor(steps))
}

// toolStepsFor projects the transcript's tool steps onto the
// tuitools.Step shape the grouping rules work on.
func toolStepsFor(steps []toolStep) []tuitools.Step {
	out := make([]tuitools.Step, len(steps))
	for i, s := range steps {
		out[i] = tuitools.Step{Name: s.name, Title: s.title, Summary: s.summary}
	}
	return out
}

// styledGroupSummary styles a batch header. "Ran" is the bold-orange verb
// (cToolVerbOpen), the count is body-coloured, and the parenthesised list
// plus the hint are dim gray — the exact split measured on 1.4.3:
//
//	\x1b[38;2;181;137;0;1mRan\x1b[m \x1b[38;2;7;54;66m2 commands\x1b[m \x1b[38;2;88;110;117m(echo one · echo two)\x1b[m
func styledGroupSummary(steps []toolStep) string {
	verb, count, detail := groupSummary(steps)
	var b strings.Builder
	b.WriteString(cToolVerbOpen)
	b.WriteString(verb)
	b.WriteString(cToolVerbClose)
	if count != "" {
		b.WriteString(" ")
		b.WriteString(cBodyOpen)
		b.WriteString(count)
		b.WriteString(cBodyClose)
	}
	if detail != "" {
		b.WriteString(" ")
		b.WriteString(cToolArgsOpen)
		b.WriteString(detail)
		b.WriteString(cToolArgsClose)
	}
	return b.String()
}

// styledSummary styles a single step's collapsed form ("Ran (echo one)").
func styledSummary(s toolStep) string {
	text := s.summary
	if text == "" {
		text = s.title
	}
	if text == "" {
		text = s.name
	}
	return styleVerbArgs(text)
}

// styledCallTitle styles a single step's expanded form ("Bash(echo one)").
func styledCallTitle(s toolStep) string {
	text := s.title
	if text == "" {
		text = s.summary
	}
	if text == "" {
		text = s.name
	}
	return styleVerbArgs(text)
}

// styleVerbArgs colours the leading verb in bold orange and the rest in the
// dim arg palette. The space between them is kept only when the title has
// one: "Ran (cmd)" keeps it, "Bash(cmd)" does not (both measured on 1.4.3).
func styleVerbArgs(text string) string {
	verb, args, sep := splitVerbArgs(text)
	var b strings.Builder
	b.WriteString(cToolVerbOpen)
	b.WriteString(verb)
	b.WriteString(cToolVerbClose)
	if args != "" {
		b.WriteString(sep)
		b.WriteString(cToolArgsOpen)
		b.WriteString(args)
		b.WriteString(cToolArgsClose)
	}
	return b.String()
}

// splitVerbArgs splits a title into verb, args and the separator that sat
// between them. A "(" before the first space means the args are glued to the
// verb ("Bash(cmd)"), otherwise the separator is the single space.
func splitVerbArgs(title string) (verb, args, sep string) {
	return tuitools.SplitVerbArgs(title)
}

// styledHint wraps the "(ctrl+o to expand)" hint the same way ref
// does — dim gray (256-colour 238) for the parens and the verb.
// The leading "(" and trailing ")" are also wrapped, so the whole
// hint reads as one highlighted parenthetical.
//
// Cycle 159: cToolHintOpen / Close are a separate palette from
// cToolArgsOpen / Close — the hint is slightly lighter than the
// args (238 vs 235) so the eye reads it as a UI cue, not as
// content detail.
func styledHint(hint string) string {
	return cToolHintOpen + "(" + hint + ")" + cToolHintClose
}

// thoughtTitle renders the completed-thought summary. The duration uses Go's
// Duration.String() shape ("3s", "1.5s"), which matches the reference's
// "Thought for %s" / "Thought for %.1fs" pair for the common cases.
func thoughtTitle(d time.Duration, tokens int) string {
	return tuitools.ThoughtTitle(d, tokens)
}

// humanDuration renders a duration the way the reference does: whole seconds
// stay integral ("3s"), sub-second parts keep one decimal ("1.5s"), and
// anything under a second is shown in milliseconds.
func humanDuration(d time.Duration) string {
	return tuitools.HumanDuration(d)
}

// thinkingLiveLine is the in-progress frame the reference shows in the
// conversation history while the model reasons. Recovered from the changelog:
// "Fixed in-progress thinking frames occasionally freezing permanently in the
// conversation history (for example showing `Thinking... (31s)` forever)".
func thinkingLiveLine(elapsed time.Duration) string {
	// Cycle 156: thought steps use the U+25B8 ▸ glyph in dim gray,
	// not the U+25CF ● tool glyph in teal. v1.3.6's live frame for
	// "still thinking" is the thought style — the cycle-127 clone
	// picked the tool bullet because it didn't distinguish.
	return fmt.Sprintf("%s Thinking... (%s)", thoughtBulletOpen, humanDuration(elapsed))
}
