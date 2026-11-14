package toolsteps

import (
	"fmt"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/ui/tuitools"
	"github.com/justwasm/pigasus/palette"
)

// Bullets used by the tool-step transcript. ⏺ is the "record" glyph the
// reference uses to lead a step; ⎿ is the same detail glyph the reference
// warning block uses ("  ⎿  \"<model>\" is no longer available."), which
// renderWarning already emits.
//
// Cycle 127: the step bullet is wrapped in the brand-blue
// `cBlueOpen`/`cBlueClose` so it visually anchors the tool step
// against the dim gray transcript body. The detail bullet was
// already wrapped in `cWarningBlueOpen` (used by the warning block)
// — cycle 127 keeps that.
//
// Cycle 156: the tool-bullet glyph. v1.3.6 emits U+25CF BLACK CIRCLE
// ("●") in the brand teal (256-colour 29); the cycle-127 clone
// used U+23FA ⏺ "record" in brand blue, picked from Aider/Cursor
// convention rather than the v1.3.6 binary itself. The recovered
// capture-pane diff in docs/cycles/cycle-156/ confirms the byte.
//
// Cycle 157: failed-tool-bullet glyph. v1.3.6 swaps the bullet
// palette to red (256-colour 166) when the tool exited non-zero,
// but keeps the U+25CF glyph and the same close escape.
//
// Cycle 156: thought-bullet glyph. v1.3.6 uses U+25B8 BLACK
// RIGHT-POINTING SMALL TRIANGLE ("▸") in dim gray (256-colour
// 238); the cycle-127 clone used U+23BF ⎿ "down tack and overbar"
// in warning-blue, again from a non-v1.3.6 source.
//
// The palette's colours follow the selected colour scheme, so the
// bullets are refreshed whenever the scheme changes.
var (
	stepBulletOpen        = palette.ToolBulletOpen + "●" + palette.ToolBulletClose
	stepBulletErrOpen     = palette.ToolBulletErrOpen + "●" + palette.ToolBulletClose
	thoughtBulletOpen     = palette.ThoughtBulletOpen + "▸" + palette.ThoughtBulletClose
	stepBulletRunningOpen = palette.ToolBulletRunningOpen + "●" + palette.ToolBulletClose
)

func init() {
	palette.OnSchemeChange(refreshBullets)
}

// refreshBullets re-reads the palette's colours after the selected
// colour scheme changed.
func refreshBullets() {
	stepBulletOpen = palette.ToolBulletOpen + "●" + palette.ToolBulletClose
	stepBulletErrOpen = palette.ToolBulletErrOpen + "●" + palette.ToolBulletClose
	thoughtBulletOpen = palette.ThoughtBulletOpen + "▸" + palette.ThoughtBulletClose
	stepBulletRunningOpen = palette.ToolBulletRunningOpen + "●" + palette.ToolBulletClose
}

// styledStepBullet is the bullet the transcript paints for a call in the
// given state. 1.4.3 only reddens the bullet in the *collapsed* summary
// ("● Ran (… ) failed"); once expanded the head is "● Bash(…)" with the
// success green bullet even for a non-zero exit (measured raw:
// \x1b[38;2;133;153;0m● with the red left to the detail text).
func styledStepBullet(s Step, expanded bool) string {
	if s.Kind == KindThought {
		return thoughtBulletOpen
	}
	if s.Running {
		return stepBulletRunningOpen
	}
	if s.Err && !expanded {
		return stepBulletErrOpen
	}
	return stepBulletOpen
}

// RenderOptions configures tool-step block rendering across verbosity levels.
type RenderOptions struct {
	Expanded     bool
	Width        int
	Verbosity    Verbosity
	TokenMetrics TokenMetrics
}

// BlockLines renders a turn's tool-step block with medium verbosity.
//
// Thought steps are not part of a tool group: the reference prints
// them as their own line above the calls. A single call renders as
// one step; a batch renders as one header per category.
func BlockLines(steps []Step, expanded bool, width int) []string {
	return RenderBlock(steps, RenderOptions{
		Expanded:  expanded,
		Width:     width,
		Verbosity: VerbosityMedium,
	})
}

// SingleLines renders a single step in isolation (used for the live,
// in-flight transcript before the turn finalises into a block).
func SingleLines(s Step, expanded bool, width int) []string {
	return BlockLines([]Step{s}, expanded, width)
}

// RenderBlock renders a turn's tool-step block under the specified
// verbosity and expansion options.
//
// agy 1.4.11 / 1.4.12:
//   - VerbosityLow (collapsed): CollapsedLowVerbosityGroup — token metrics table
//     ("%s in | %s out | %s thinking") plus compact tool/thought rows.
//   - VerbosityHigh: shows calls and thoughts in full detail (unless collapsed).
//   - VerbosityMedium: groups tool calls and thoughts into concise summaries.
func RenderBlock(steps []Step, opts RenderOptions) []string {
	if len(steps) == 0 {
		return nil
	}

	var thoughts, tools []Step
	for _, s := range steps {
		if s.Kind == KindThought {
			thoughts = append(thoughts, s)
		} else {
			tools = append(tools, s)
		}
	}

	// Pure thought block: no tools were executed.
	// In the reference (1.4.5+ / 1.5.8), pure thought steps never emit a token
	// metrics table summary, and never attach "(ctrl+o to expand/collapse)"
	// hint text to the thought header or reasoning text.
	if len(tools) == 0 {
		var out []string
		for _, s := range thoughts {
			out = append(out, thoughtStepLines(s, opts.Expanded, opts.Width)...)
		}
		return out
	}

	if opts.Verbosity == VerbosityLow {
		if !opts.Expanded {
			return CollapsedLowVerbosityGroup(steps, opts.TokenMetrics, opts.Width)
		}
		// In low-verbosity mode when tools are present, thoughts are represented
		// only in the token metrics and are omitted from tool expansion.
		thoughts = nil
	}

	var out []string
	for _, s := range thoughts {
		out = append(out, thoughtStepLines(s, opts.Expanded, opts.Width)...)
	}
	switch len(tools) {
	case 1:
		out = append(out, stepLines(tools[0], opts.Expanded, opts.Width)...)
	default:
		out = append(out, groupLines(tools, opts.Expanded, opts.Width)...)
	}
	if len(out) == 0 {
		return nil
	}
	// The reference keeps the hint on the collapsed summary line, and in
	// expanded mode on the very last detail row of the tool group.
	hint := "ctrl+o to expand"
	if opts.Expanded {
		hint = "ctrl+o to collapse"
	}
	out[len(out)-1] = out[len(out)-1] + " " + styledHint(hint)
	return out
}

// CollapsedLowVerbosityGroup renders the reference's CollapsedLowVerbosityGroup:
// a token metrics summary row ("X in | Y out | Z thinking") plus compact status rows
// for thoughts and tools, ending with "(ctrl+o to expand)".
func CollapsedLowVerbosityGroup(steps []Step, metrics TokenMetrics, width int) []string {
	var out []string
	if metrics.HasTokens() {
		out = append(out, palette.DimGrayOpen+metrics.Summary()+palette.DimGrayClose)
	}

	var thoughts, tools []Step
	for _, s := range steps {
		if s.Kind == KindThought {
			thoughts = append(thoughts, s)
		} else {
			tools = append(tools, s)
		}
	}
	for _, s := range thoughts {
		title := tuitools.ThoughtTitle(s.Dur, s.Tokens)
		if s.Running {
			title = "Thinking..."
		}
		out = append(out, thoughtBulletOpen+" "+palette.DimGrayOpen+title+palette.DimGrayClose)
	}
	// The reference's collapsed multi-call block is one row: the last call.
	// A per-category aggregate ("Ran 3 commands (a · b · c)") is not part of
	// the 1.5.8 screen — see groupLines.
	if len(tools) > 0 {
		last := tools[len(tools)-1]
		out = append(out, styledStepBullet(last, false)+" "+styledCallTitle(last))
	}
	if len(out) == 0 {
		return nil
	}
	if len(tools) > 0 {
		out[len(out)-1] = out[len(out)-1] + " " + styledHint("ctrl+o to expand")
	}
	return out
}

// stepLines renders one call. Collapsed is the reference's one-line summary
// ("Ran (echo one)"), expanded is the call form plus its detail rows.
func stepLines(s Step, expanded bool, width int) []string {
	if s.Running || !expanded {
		// Collapsed, the reference names the call itself — `● Bash(echo
		// one)`, `● ReadFile()` — not the past-tense summary form.
		line := styledStepBullet(s, false) + " " + styledCallTitle(s)
		if s.Err && !s.Running && tuitools.ToolCategory(s.Name) == "command" {
			// Only a non-zero *command* carries the word (measured: a
			// failed search_web reddens the bullet but prints no
			// " failed").
			line += " " + palette.ToolArgsOpen + "failed" + palette.ToolArgsClose
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
func categoryGroups(tools []Step) [][]Step {
	var groups [][]Step
	idx := map[string]int{}
	for _, s := range tools {
		c := tuitools.ToolCategory(s.Name)
		if i, ok := idx[c]; ok {
			groups[i] = append(groups[i], s)
			continue
		}
		idx[c] = len(groups)
		groups = append(groups, []Step{s})
	}
	return groups
}

// groupLines renders a batch of calls emitted together (parallel tool
// calls) or a turn's run of sequential steps.
//
// Measured on 1.5.8 (tuialign scenarios 27 and 28): the reference never
// aggregates a multi-call block into one summary. Collapsed, the block is a
// single row naming the last call —
//
//	● Bash(echo gamma) (ctrl+o to expand)
//
// — and expanded, every call appears with its own detail rows and no summary
// header above them. The clone used to print one "Ran N commands (a · b · c)"
// row that grew with every call, which wrapped into a paragraph.
func groupLines(tools []Step, expanded bool, width int) []string {
	if len(tools) == 0 {
		return nil
	}
	if !expanded {
		last := tools[len(tools)-1]
		return []string{styledStepBullet(last, false) + " " + styledCallTitle(last)}
	}
	var out []string
	for _, s := range tools {
		out = append(out, renderStepTitle(s, true)...)
		out = append(out, stepDetailLines(s, width)...)
	}
	return out
}

// thoughtStepLines renders a thought step: "▸ Thought for 3s" plus, when
// expanded, the reasoning body.
func thoughtStepLines(s Step, expanded bool, width int) []string {
	bullet := thoughtBulletOpen
	if expanded {
		bullet = palette.ThoughtBulletOpen + "▾" + palette.ThoughtBulletClose
	}
	title := tuitools.ThoughtTitle(s.Dur, s.Tokens)
	if s.Running {
		title = "Thinking..."
	}
	head := bullet + " " + title
	if !expanded {
		return []string{head}
	}
	out := []string{head}
	return append(out, thoughtDetailLines(s, width)...)
}

// thoughtDetailLines renders the reasoning text of an expanded thought step.
// Thoughts following a tool result are rendered with clean 2-space indentation
// rather than being formatted as tool detail bullets (  ⎿  ) (AGY 1.5.4).
func thoughtDetailLines(s Step, width int) []string {
	raw := s.Result
	if raw == "" {
		raw = s.Detail
	}
	detail := strings.TrimRight(tuitools.NormalizeDetailNewlines(raw), "\n")
	if strings.TrimSpace(detail) == "" {
		return nil
	}
	wrapWidth := tuitools.DetailWrapWidth(width)
	rawLines := strings.Split(detail, "\n")
	var wrappedLines []string
	for _, l := range rawLines {
		l = tuitools.ExpandTabs(l, 4)
		wrapped := ansi.Wrap(l, wrapWidth, "")
		wrappedLines = append(wrappedLines, strings.Split(wrapped, "\n")...)
	}
	out := make([]string, 0, len(wrappedLines))
	const indent = "  "
	for _, wl := range wrappedLines {
		out = append(out, indent+wl)
	}
	return out
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
// The rule itself lives in internal/ui/tuitools.
func stepDetailText(s Step) string {
	return tuitools.StepDetailText(s.Name, s.Detail, s.Kind == KindThought)
}

func stepDetailLines(s Step, width int) []string {
	if s.Diff != nil {
		return s.Diff.Lines(width)
	}
	detail := strings.TrimRight(tuitools.NormalizeDetailNewlines(stepDetailText(s)), "\n")
	if strings.TrimSpace(detail) == "" {
		return nil
	}
	wrapWidth := tuitools.DetailWrapWidth(width)
	diffMode := tuitools.LooksLikeDiff(detail)
	// Machine output keeps its own bytes: command stdout, grep hits and
	// directory listings are drawn preformatted, so a Go pointer
	// (`meta **StreamMeta`) must not turn a step into markdown. See
	// tuitools.PreformattedDetail.
	if !diffMode && !tuitools.PreformattedDetail(s.Name) && tuitools.LooksLikeMarkdown(detail) {
		body := tuitools.RenderMarkdownDetail(detail, wrapWidth)
		return tuitools.FormatDetailLines(strings.Split(body, "\n"), false)
	}
	rawLines := strings.Split(detail, "\n")
	var wrappedLines []string
	for _, l := range rawLines {
		if diffMode {
			wrappedLines = append(wrappedLines, tuitools.WrapDiffLine(l, wrapWidth)...)
		} else {
			l = tuitools.ExpandTabs(l, 4)
			wrapped := ansi.Wrap(l, wrapWidth, "")
			wrappedLines = append(wrappedLines, strings.Split(wrapped, "\n")...)
		}
	}
	return tuitools.FormatDetailLines(wrappedLines, diffMode)
}

// renderStepTitle renders the step's head row.
func renderStepTitle(s Step, expanded bool) []string {
	if s.Kind == KindThought {
		bullet := thoughtBulletOpen
		if expanded {
			bullet = palette.ThoughtBulletOpen + "▾" + palette.ThoughtBulletClose
		}
		title := tuitools.ThoughtTitle(s.Dur, s.Tokens)
		if s.Running {
			title = "Thinking..."
		}
		return []string{bullet + " " + title}
	}
	return []string{styledStepBullet(s, expanded) + " " + styledCallTitle(s)}
}

// styledGroupSummary styles a batch header. "Ran" is the bold-orange verb
// (palette.ToolVerbOpen), the count is body-coloured, and the parenthesised
// list plus the hint are dim gray — the exact split measured on 1.4.3:
//
//	\x1b[38;2;181;137;0;1mRan\x1b[m \x1b[38;2;7;54;66m2 commands\x1b[m \x1b[38;2;88;110;117m(echo one · echo two)\x1b[m
func styledGroupSummary(steps []Step) string {
	verb, count, detail := tuitools.GroupSummary(stepsFor(steps))
	var b strings.Builder
	b.WriteString(palette.ToolVerbOpen)
	b.WriteString(verb)
	b.WriteString(palette.ToolVerbClose)
	if count != "" {
		b.WriteString(" ")
		b.WriteString(palette.BodyOpen)
		b.WriteString(count)
		b.WriteString(palette.BodyClose)
	}
	if detail != "" {
		b.WriteString(" ")
		b.WriteString(palette.ToolArgsOpen)
		b.WriteString(detail)
		b.WriteString(palette.ToolArgsClose)
	}
	return b.String()
}

// stepsFor projects the transcript's tool steps onto the
// tuitools.Step shape the grouping rules work on.
func stepsFor(steps []Step) []tuitools.Step {
	out := make([]tuitools.Step, len(steps))
	for i, s := range steps {
		out[i] = tuitools.Step{Name: s.Name, Title: s.Title, Summary: s.Summary}
	}
	return out
}

// styledSummary styles a single step's collapsed form ("Ran (echo one)").
func styledSummary(s Step) string {
	text := s.Summary
	if text == "" {
		text = s.Title
	}
	if text == "" {
		text = s.Name
	}
	return styleVerbArgs(text)
}

// styledCallTitle styles a single step's expanded form ("Bash(echo one)").
func styledCallTitle(s Step) string {
	text := s.Title
	if text == "" {
		text = s.Summary
	}
	if text == "" {
		text = s.Name
	}
	return styleVerbArgs(text)
}

// styleVerbArgs colours the leading verb in bold orange and the rest in the
// dim arg palette. The space between them is kept only when the title has
// one: "Ran (cmd)" keeps it, "Bash(cmd)" does not (both measured on 1.4.3).
func styleVerbArgs(text string) string {
	verb, args, sep := tuitools.SplitVerbArgs(text)
	var b strings.Builder
	b.WriteString(palette.ToolVerbOpen)
	b.WriteString(verb)
	b.WriteString(palette.ToolVerbClose)
	if args != "" {
		b.WriteString(sep)
		b.WriteString(palette.ToolArgsOpen)
		b.WriteString(args)
		b.WriteString(palette.ToolArgsClose)
	}
	return b.String()
}

// styledHint wraps the "(ctrl+o to expand)" hint the same way ref
// does — dim gray (256-colour 238) for the parens and the verb.
// The leading "(" and trailing ")" are also wrapped, so the whole
// hint reads as one highlighted parenthetical.
//
// Cycle 159: the hint's palette is a separate one from the args' —
// the hint is slightly lighter than the args (238 vs 235) so the eye
// reads it as a UI cue, not as content detail.
func styledHint(hint string) string {
	return palette.ToolHintOpen + "(" + hint + ")" + palette.ToolHintClose
}

// ThinkingLiveLine is the in-progress frame the reference shows in the
// conversation history while the model reasons. Recovered from the changelog:
// "Fixed in-progress thinking frames occasionally freezing permanently in the
// conversation history (for example showing `Thinking... (31s)` forever)".
func ThinkingLiveLine(elapsed time.Duration) string {
	// Cycle 156: thought steps use the U+25B8 ▸ glyph in dim gray,
	// not the U+25CF ● tool glyph in teal. v1.3.6's live frame for
	// "still thinking" is the thought style — the cycle-127 clone
	// picked the tool bullet because it didn't distinguish.
	return fmt.Sprintf("%s Thinking... (%s)", thoughtBulletOpen, tuitools.HumanDuration(elapsed))
}

// stripMarkdownFormatting removes ***, **, and * markers from a string.
// Recovered byte-for-byte from render.stripMarkdownFormatting (0x1029d2c50).
func stripMarkdownFormatting(s string) string {
	s = strings.ReplaceAll(s, "***", "")
	s = strings.ReplaceAll(s, "**", "")
	s = strings.ReplaceAll(s, "*", "")
	return s
}

// collapseWhitespace collapses whitespace sequences into a single space.
// Recovered byte-for-byte from render.collapseWhitespace (0x1029b75f0).
func collapseWhitespace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}

// ThinkingLiveSnippet extracts the active intent snippet from streaming
// thinking content to display behind the spinner.
// Recovered from render.thinkingLiveSnippet (0x1029b7fb0):
// 1. strings.TrimSpace(s)
// 2. strings.Split(s, "\n")
// 3. Scan backwards for the last non-empty line
// 4. Strip markdown formatting
// 5. Collapse whitespace
func ThinkingLiveSnippet(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	lines := strings.Split(s, "\n")
	for i := len(lines) - 1; i >= 0; i-- {
		line := strings.TrimSpace(lines[i])
		if len(line) == 0 {
			continue
		}
		line = stripMarkdownFormatting(line)
		line = collapseWhitespace(line)
		if len(line) > 0 {
			return line
		}
	}
	return ""
}
