package main

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/mdrender"
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

// renderStepTitle returns the styled expanded call title as rows. A
// run_command whose CommandLine spans shell separators (&&, ||, ;, or a
// newline) wraps onto continuation rows the way the reference does; every
// other tool is a single row.
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
func stepDetailText(s toolStep) string {
	if s.kind == stepKindThought {
		return s.detail
	}
	switch s.name {
	case "run_command":
		if text, ok := commandDetailText(s.detail); ok {
			return text
		}
	case "view_file", "read_file":
		return readDetailText(s.detail)
	}
	return s.detail
}

// commandDetailText reformats a run_command result body into the transcript
// display form. ok is false when the body carries no exit-code line (a
// tool-level failure or a timeout note), in which case the caller keeps the
// raw text.
func commandDetailText(result string) (string, bool) {
	code, ok := commandExitCode(result)
	if !ok {
		return "", false
	}
	out := ""
	if i := strings.Index(result, "Output:"); i >= 0 {
		out = strings.TrimPrefix(result[i+len("Output:"):], "\n")
	}
	out = strings.TrimRight(out, "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "Command exited with code %d.", code)
	if strings.TrimSpace(out) == "" {
		b.WriteString("\n\nNo output from the command.")
	} else {
		b.WriteString("\n\nCommand output:\n")
		b.WriteString(out)
	}
	return b.String(), true
}

func parseReadDetailLines(result string) int {
	if i := strings.Index(result, "Showing lines "); i >= 0 {
		var first, last, total int
		if _, err := fmt.Sscanf(result[i:], "Showing lines %d-%d of %d", &first, &last, &total); err == nil {
			return last - first + 1
		}
		if j := strings.Index(result[i:], " to "); j >= 0 {
			if _, err := fmt.Sscanf(result[i:], "Showing lines %d", &first); err == nil {
				if _, err := fmt.Sscanf(result[i+j:], " to %d", &last); err == nil {
					return last - first + 1
				}
			}
		}
	}
	if i := strings.Index(result, "Partial read (lines "); i >= 0 {
		var first, last, total int
		if _, err := fmt.Sscanf(result[i:], "Partial read (lines %d-%d of %d)", &first, &last, &total); err == nil {
			return last - first + 1
		}
	}
	if strings.Contains(result, "File is empty") {
		return 0
	}
	trimmed := strings.TrimRight(result, "\n")
	if strings.TrimSpace(trimmed) != "" {
		return strings.Count(trimmed, "\n") + 1
	}
	return 0
}

func readDetailText(result string) string {
	n := parseReadDetailLines(result)
	if n < 2 {
		return ""
	}
	return "Read " + countLabel(n, [2]string{"line", "lines"})
}

func renderMarkdownDetail(detail string, width int) string {
	pre := mdrender.PreprocessFileLinks(detail)
	stripped, placeholders := mdrender.StripMermaidAndMath(pre)
	toolWidth := width
	if toolWidth <= 0 {
		toolWidth = mdrender.CurrentWidth()
	}
	rendered, err := mdrender.ANSI(stripped, toolWidth)
	if err != nil || rendered == "" {
		var buf bytes.Buffer
		if e := mdrender.RenderANSI(detail, &buf); e == nil {
			rendered = buf.String()
		}
	} else {
		rendered = mdrender.RestoreMermaidAndMath(rendered, placeholders)
	}
	if rendered != "" {
		return strings.TrimRight(rendered, "\n")
	}
	return detail
}

func formatDetailLines(lines []string, diffMode bool) []string {
	extra := 0
	if len(lines) > maxStepDetailLines {
		extra = len(lines) - maxStepDetailLines
		lines = lines[:maxStepDetailLines]
	}
	out := make([]string, 0, len(lines)+1)
	for i, l := range lines {
		styled := l
		if diffMode {
			styled = styleDiffLine(l)
		}
		if i == 0 {
			out = append(out, detailBulletOpen+styled)
		} else {
			out = append(out, "            "+styled)
		}
	}
	if extra > 0 {
		out = append(out, fmt.Sprintf("            <output +%d lines>", extra))
	}
	return out
}

func stepDetailLines(s toolStep, width int) []string {
	if s.diff != nil {
		return s.diff.lines(width)
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


// 0 and the expanded output renders blank. That is why the detail only
// looked broken after ctrl+o — while collapsed the body is not drawn.
//
// This mirrors internal/convstore.cleanToolResult (cycle 178), which was
// the same fix on the resumed-conversation path. Display-only: never
// write the normalised string back into s.detail / m.response / m.session.
func normalizeDetailNewlines(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "")
}

// looksLikeDiff returns true if s contains markers that identify
// the body as a unified-diff payload. Cycle 178 uses this in
// stepDetailLines to gate the per-line diff colouring pass —
// before cycle 178 a `- pattern` line in a non-diff payload
// (cat .gitignore, a YAML list, shell command output) was
// spuriously coloured red.
//
// The check is purely structural: any of:
//
//   - "@@" hunk marker on its own line (the canonical
//     unified-diff body delimiter), at line start or after a
//     newline
//   - both "--- " and "+++ " file headers (the canonical
//     unified-diff file delimiter pair) anywhere in the body
//
// fires it. Each alone is not enough — a markdown file might
// contain a "---" thematic break and a shell transcript might
// contain a "+++" arithmetic fragment, but the combination of
// both is unambiguous in practice. "@@" alone is unambiguous.
func looksLikeDiff(s string) bool {
	if strings.HasPrefix(s, "@@") || strings.Contains(s, "\n@@") {
		return true
	}
	hasMinus := strings.Contains(s, "--- ") || strings.HasPrefix(s, "--- ")
	hasPlus := strings.Contains(s, "+++ ") || strings.HasPrefix(s, "+++ ")
	return hasMinus && hasPlus
}

// looksLikeMarkdown returns true if s contains Markdown markers that
// goldmark would recognise on parse. Cycle 178 introduced this in
// stepDetailLines; cycle 178 widens it.
//
// Cycle 178: the heuristic now also fires on `- ` and `* ` at line
// start (the standard Markdown bullet markers). Cycle 178 had to
// exclude those because they collided with the cycle-127 diff-style
// alphabet. Cycle 178 tightens the diff side (looksLikeDiff now
// requires hunk markers or both file headers) so the Markdown side
// can safely widen. A bullet-only Markdown payload like `cat
// .gitignore` (the original cycle-176 regression case) still does
// NOT trip the heuristic — it has only `- ` lines, no other markers
// — so its `- pattern` rows are now rendered verbatim (no
// bullet-conversion, no spurious red colouring), which is the
// correct output for a `.gitignore` payload.
//
// The six markers checked are:
//
//   - "**"               paired-asterisk strong (most Markdown
//     prose has at least one bold run)
//   - "```"              fenced code block delimiter (readme /
//     changelog files almost always have one)
//   - "# " at line start ATX heading
//   - "]("               inline / reference link
//   - "- " at line start unordered list item (cycle 178 addition)
//   - "* " at line start unordered list item (cycle 178 addition)
//
// The check is purely lexical — it does not parse the content with
// goldmark first. A pathological input that contains `**` as
// literal text (e.g. someone running `printf 'a**b' | cat`) would
// be mis-rendered; in practice such content never appears in tool
// output.
func looksLikeMarkdown(s string) bool {
	if strings.Contains(s, "**") {
		return true
	}
	if strings.Contains(s, "```") {
		return true
	}
	if strings.HasPrefix(s, "# ") || strings.Contains(s, "\n# ") {
		return true
	}
	if strings.Contains(s, "](") {
		return true
	}
	// Cycle 178: `- ` and `* ` at line start are now Markdown
	// bullet markers (cycle 178 had to exclude them because the
	// diff side was over-broad). The diff side is now tightened
	// (looksLikeDiff requires `@@` or both `--- ` / `+++ `), so a
	// payload that genuinely is bulleted Markdown (or .gitignore,
	// which has `- pattern` rows) renders correctly: bullet rows
	// become `␡ ` round dots, never get the spurious red colour.
	if strings.HasPrefix(s, "- ") || strings.Contains(s, "\n- ") {
		return true
	}
	if strings.HasPrefix(s, "* ") || strings.Contains(s, "\n* ") {
		return true
	}
	return false
}

// styleDiffLine returns line with its leading `+`/`-` column wrapped
// in the recovered diff add/remove colour, when applicable. Lines
// without a leading diff marker (and lines that exceed the maximum
// styling width to keep the byte budget bounded) pass through
// unchanged. Cycle 127 limits the styled column to the first 200 bytes
// of the line to avoid runaway byte growth on long diffs.
//
// Cycle 178: the cycle-127 heuristic was over-broad — a run_command
// stdout that starts with "---deleted---" or "---git status---"
// would also be styled as a diff and lose the leading dash to the
// red colour. The recovered reference rendering treats "---"/"+++"
// (more than one marker at column 0) as section separators, not as
// diff content, and only styles single-marker lines.
func styleDiffLine(line string) string {
	// Strip a single leading horizontal-tab or space block to find the
	// diff marker — replace_file_content usually emits the marker at
	// column 0, but multi_replace_file_content sometimes pads with a
	// tab. Anything past that is content; only the marker is coloured.
	trimmed := line
	j := 0
	for j < len(trimmed) && (trimmed[j] == ' ' || trimmed[j] == '\t') {
		j++
	}
	if j >= len(trimmed) {
		return line
	}
	marker := trimmed[j]
	// Skip "---" / "+++" / "++++" / "----" — these are diff file
	// headers or section separators, not content rows. The reference
	// leaves them in the default palette.
	if j+1 < len(trimmed) && trimmed[j+1] == marker {
		return line
	}
	switch marker {
	case '+':
		if len(trimmed) > j+1+200 {
			return line
		}
		return trimmed[:j+1] + cToolAddOpen + trimmed[j+1:] + cToolAddClose
	case '-':
		if len(trimmed) > j+1+200 {
			return line
		}
		return trimmed[:j+1] + cToolRemoveOpen + trimmed[j+1:] + cToolRemoveClose
	}
	return line
}

// toolStepLines renders a single step in isolation (used for the live,
// in-flight transcript before the turn finalises into a block).
func toolStepLines(s toolStep, expanded bool, width int) []string {
	return toolBlockLines([]toolStep{s}, expanded, width)
}

