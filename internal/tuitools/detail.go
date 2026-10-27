package tuitools

import (
	"bytes"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/agystep"
	"github.com/justwasm/pigasus/internal/mdrender"
	"github.com/justwasm/pigasus/palette"
)

// MaxDetailLines caps how many output lines the expanded view shows
// before switching to the recovered "<output +%d lines>" overflow marker.
const MaxDetailLines = 20

// StepDetailText maps a tool result onto the text the reference's
// expanded step actually shows. The tool *result* and the transcript
// *display* are different strings:
//
//	run_command result:  Created At: …\nCompleted At: …\nCommand exited
//	                     with code 0.\n\nCommand output:\n<out>\n
//
//	run_command display: Command exited with code 0.\n\nCommand output:\n<out>
//	                     (empty output: "No output from the command.")
//	view_file result:    <file bytes>
//	view_file display:   Read 3 lines
//
// The display form was verified against agy 1.4.3 with `echo one`, `true`
// and `false` (docs/cycles/cycle-285-tool-display/deliverable.md). Anything
// we have not captured (write/replace/search) keeps the raw result.
//
// isThought marks a reasoning step, whose detail is shown verbatim.
func StepDetailText(name, detail string, isThought bool) string {
	if isThought {
		return detail
	}
	switch name {
	case "run_command":
		if text, ok := CommandDetailText(detail); ok {
			return text
		}
	case "view_file", "read_file":
		return ReadDetailText(detail)
	}
	return detail
}

// CommandDetailText reformats a run_command result body into the transcript
// display form. ok is false when the body carries no exit-code line (a
// tool-level failure or a timeout note), in which case the caller keeps the
// raw text.
//
// The reader is internal/agystep's: it knows the reference's two shapes —
// the CLI's own formatter ("Command exited with code N." / "Command
// output:") and the step string ("The command exited with code N." /
// "Output:"). Before cycle 298 this function only knew the second one, so
// it stopped recognising the clone's own results: a failed command was not
// marked as failed and the "Created At / Completed At" stamp reached the
// screen, which cycle 285 says it never does.
func CommandDetailText(result string) (string, bool) {
	res, ok := agystep.ParseRunCommandResult(result)
	if !ok {
		return "", false
	}
	out := strings.TrimRight(res.Output, "\n")
	var b strings.Builder
	fmt.Fprintf(&b, "Command exited with code %d.", res.ExitCode)
	if strings.TrimSpace(out) == "" {
		b.WriteString("\n\nNo output from the command.")
	} else {
		b.WriteString("\n\nCommand output:\n")
		b.WriteString(out)
	}
	return b.String(), true
}

// ParseReadDetailLines counts the lines a read tool reports.
func ParseReadDetailLines(result string) int {
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

// ReadDetailText renders the "Read N lines" summary a read tool shows.
func ReadDetailText(result string) string {
	n := ParseReadDetailLines(result)
	if n < 2 {
		return ""
	}
	return "Read " + CountLabel(n, [2]string{"line", "lines"})
}

// RenderMarkdownDetail renders a markdown payload the way the reference
// does, at the given width.
func RenderMarkdownDetail(detail string, width int) string {
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

// FormatDetailLines renders the body rows of an expanded step, capping
// the count and adding the overflow marker.
func FormatDetailLines(lines []string, diffMode bool) []string {
	extra := 0
	if len(lines) > MaxDetailLines {
		extra = len(lines) - MaxDetailLines
		lines = lines[:MaxDetailLines]
	}
	out := make([]string, 0, len(lines)+1)
	for i, l := range lines {
		styled := l
		if diffMode {
			styled = StyleDiffLine(l)
		}
		if i == 0 {
			out = append(out, palette.DetailBulletOpen+"  ⎿  "+"\x1b[m"+styled)
		} else {
			out = append(out, "            "+styled)
		}
	}
	if extra > 0 {
		out = append(out, fmt.Sprintf("            <output +%d lines>", extra))
	}
	return out
}

// NormalizeDetailNewlines folds CRLF and bare CR into LF.
//
// The reference renders a carriage return as a line break, so a tool
// result that carries CRLF line endings collapses into a single line in
// the transcript — and the expanded output renders blank. That is why
// the detail only looked broken after ctrl+o: while collapsed the body
// is not drawn.
//
// This mirrors internal/convstore.cleanToolResult (cycle 178), which was
// the same fix on the resumed-conversation path. Display-only: never
// write the normalised string back into the step's detail.
func NormalizeDetailNewlines(s string) string {
	if !strings.ContainsRune(s, '\r') {
		return s
	}
	s = strings.ReplaceAll(s, "\r\n", "\n")
	return strings.ReplaceAll(s, "\r", "")
}

// LooksLikeDiff returns true if s contains markers that identify
// the body as a unified-diff payload. The check is purely structural: any of
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
func LooksLikeDiff(s string) bool {
	if strings.HasPrefix(s, "@@") || strings.Contains(s, "\n@@") {
		return true
	}
	hasMinus := strings.Contains(s, "--- ") || strings.HasPrefix(s, "--- ")
	hasPlus := strings.Contains(s, "+++ ") || strings.HasPrefix(s, "+++ ")
	return hasMinus && hasPlus
}

// LooksLikeMarkdown returns true if s contains Markdown markers that
// goldmark would recognise on parse.
//
// The heuristic also fires on "- " and "* " at line start (the standard
// Markdown bullet markers). The diff side is tightened (LooksLikeDiff
// requires "@@" or both "--- " / "+++ ") so the Markdown side can safely
// widen. A bullet-only Markdown payload like `cat .gitignore` (the
// original cycle-176 regression case) still does NOT trip the heuristic —
// it has only "- " lines, no other markers — so its "- pattern" rows are
// now rendered verbatim (no bullet-conversion, no spurious red colouring),
// which is the correct output for a `.gitignore` payload.
//
// The six markers checked are:
//
//   - "**"               paired-asterisk strong (most Markdown
//     prose has at least one bold run)
//   - "```"              fenced code block delimiter (readme /
//     changelog files almost always have one)
//   - "# " at line start ATX heading
//   - "]("               inline / reference link
//   - "- " at line start unordered list item
//   - "* " at line start unordered list item
//
// The check is purely lexical — it does not parse the content with
// goldmark first. A pathological input that contains "**" as
// literal text (e.g. someone running `printf 'a**b' | cat`) would
// be mis-rendered; in practice such content never appears in tool
// output.
func LooksLikeMarkdown(s string) bool {
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
	if strings.HasPrefix(s, "- ") || strings.Contains(s, "\n- ") {
		return true
	}
	if strings.HasPrefix(s, "* ") || strings.Contains(s, "\n* ") {
		return true
	}
	return false
}

// StyleDiffLine returns line with its leading `+`/`-` column wrapped
// in the recovered diff add/remove colour, when applicable. Lines
// without a leading diff marker (and lines that exceed the maximum
// styling width to keep the byte budget bounded) pass through
// unchanged. The styled column is limited to the first 200 bytes
// of the line to avoid runaway byte growth on long diffs.
//
// The recovered reference rendering treats "---"/"+++"
// (more than one marker at column 0) as section separators, not as
// diff content, and only styles single-marker lines.
func StyleDiffLine(line string) string {
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
		return trimmed[:j+1] + palette.ToolAddOpen + trimmed[j+1:] + palette.ToolAddClose
	case '-':
		if len(trimmed) > j+1+200 {
			return line
		}
		return trimmed[:j+1] + palette.ToolRemoveOpen + trimmed[j+1:] + palette.ToolRemoveClose
	}
	return line
}
