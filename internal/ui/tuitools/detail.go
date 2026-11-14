package tuitools

import (
	"bytes"
	"fmt"
	"regexp"
	"strings"

	"github.com/charmbracelet/x/ansi"
	agystep "github.com/justwasm/pigasus/internal/app/prompt/step"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/palette"
)

// MaxDetailLines caps how many output lines the expanded view shows
// before switching to the recovered "<output +%d lines>" overflow marker.
// In AgY 1.5.8, 10 lines of tail output are kept when truncated.
const MaxDetailLines = 10

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

var stampRE = regexp.MustCompile(`^(Created At: [^\r\n]+\r?\nCompleted At: [^\r\n]+\r?\n)`)

func extractStamp(s string) string {
	if m := stampRE.FindString(s); m != "" {
		return m
	}
	return ""
}

// CommandDetailText reformats a run_command result body into the transcript
// display form. ok is false when the body carries no exit-code line (a
// tool-level failure or a timeout note), in which case the caller keeps the
// raw text.
//
// In AgY 1.5.8, timestamp rows (Created At / Completed At) are retained when present.
func CommandDetailText(result string) (string, bool) {
	res, ok := agystep.ParseRunCommandResult(result)
	if !ok {
		return "", false
	}
	out := strings.TrimRight(res.Output, "\n")
	var b strings.Builder
	if stamp := extractStamp(result); stamp != "" {
		b.WriteString(stamp)
	}
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

// DetailIndent is the indent continuation lines carry in expanded tool steps.
// In AgY 1.5.8, continuation lines align with the body of "  ⎿  " (5 spaces).
const DetailIndent = "     "

// DetailIndentWidth is the width of DetailIndent in cells.
const DetailIndentWidth = 5

// DetailMinWrapWidth is the minimum wrap width enforced by the reference.
const DetailMinWrapWidth = 20

// DetailWrapWidth calculates the content wrap width given the terminal width.
// AgY 1.5.8 wrap width: max(20, width - 5).
func DetailWrapWidth(width int) int {
	toolWidth := width
	if toolWidth <= 0 {
		toolWidth = mdrender.CurrentWidth()
	}
	if toolWidth <= 0 {
		toolWidth = 80
	}
	wrapWidth := toolWidth - DetailIndentWidth
	if wrapWidth < DetailMinWrapWidth {
		return DetailMinWrapWidth
	}
	return wrapWidth
}

// ExpandTabs expands tabs to tabWidth spaces using mdrender.ExpandTabs.
func ExpandTabs(s string, tabWidth int) string {
	return mdrender.ExpandTabs(s, tabWidth)
}

// WrapDiffLine wraps a unified diff line to wrapWidth, propagating the diff
// add/remove styling across all continuation rows if the line is an addition or deletion.
func WrapDiffLine(line string, wrapWidth int) []string {
	if wrapWidth <= 0 {
		return []string{StyleDiffLine(line)}
	}
	line = ExpandTabs(line, 4)
	trimmed := line
	j := 0
	for j < len(trimmed) && (trimmed[j] == ' ' || trimmed[j] == '\t') {
		j++
	}
	var marker byte
	if j < len(trimmed) {
		m := trimmed[j]
		if (m == '+' || m == '-') && !(j+1 < len(trimmed) && trimmed[j+1] == m) {
			marker = m
		}
	}
	wrapped := ansi.Wrap(line, wrapWidth, "")
	parts := strings.Split(wrapped, "\n")
	if len(parts) == 0 {
		return nil
	}
	if marker == 0 {
		return parts
	}
	out := make([]string, len(parts))
	for i, p := range parts {
		if p == "" {
			out[i] = ""
			continue
		}
		if i == 0 {
			out[0] = StyleDiffLine(p)
		} else {
			if marker == '+' {
				out[i] = palette.ToolAddOpen + p + palette.ToolAddClose
			} else {
				out[i] = palette.ToolRemoveOpen + p + palette.ToolRemoveClose
			}
		}
	}
	return out
}

// DetailBullet returns the prefix the first expanded detail row carries:
// the muted detail colour, the "⎿" glyph and the reset.
//
// The colour follows the selected scheme, so the prefix is composed on
// every call rather than cached.
func DetailBullet() string {
	return palette.DetailBulletOpen + "  ⎿  " + "\x1b[m"
}

// FormatDetailLines renders the body rows of an expanded step, capping
// the count and adding the overflow marker. In upstream AgY 1.5.5, command
// execution output retains the tail window (most recent rows) when truncated,
// placing the overflow indicator at the top with DetailBullet.
func FormatDetailLines(lines []string, diffMode bool) []string {
	extra := 0
	if len(lines) > MaxDetailLines {
		extra = len(lines) - MaxDetailLines
		lines = lines[extra:]
	}
	out := make([]string, 0, len(lines)+1)
	if extra > 0 {
		out = append(out, fmt.Sprintf("%s<output +%d lines>", DetailBullet(), extra))
		for _, l := range lines {
			l = mdrender.ExpandTabs(l, 4)
			styled := l
			if diffMode && !strings.Contains(l, "\x1b[") {
				styled = StyleDiffLine(l)
			}
			out = append(out, DetailIndent+styled)
		}
	} else {
		for i, l := range lines {
			l = mdrender.ExpandTabs(l, 4)
			styled := l
			if diffMode && !strings.Contains(l, "\x1b[") {
				styled = StyleDiffLine(l)
			}
			if i == 0 {
				out = append(out, DetailBullet()+styled)
			} else {
				out = append(out, DetailIndent+styled)
			}
		}
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

// PreformattedDetail reports whether the tool's detail is machine output —
// a command's stdout, a search result, a directory listing — rather than a
// document a human wrote.
//
// The reference draws those bodies preformatted: every row keeps its own
// leading whitespace, and the text is never re-flows, re-indents or eats a
// backtick. Rendering them through the markdown path was the reported
// inconsistency (2026-11-14): two `run_command` steps, one shown with its
// source indentation and struct tags intact and the next dedented with the
// tags stripped, because the second one's output happened to contain the
// Go type `meta **StreamMeta` — and `**` alone used to mean "this is
// markdown".
//
// A tool that is not listed keeps the markdown path: read_url_content
// returns a page, and a page may genuinely be markdown (cycle 176).
func PreformattedDetail(name string) bool {
	switch name {
	case "run_command", "run_shell_command", "exec_command",
		"bash", "exec", "execute_command", "execute", "terminal", "sh",
		"command_status", "send_command_input",
		"grep_search", "grep", "find_by_name", "list_dir", "list_directory",
		"list_processes":
		return true
	}
	return false
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
//   - "**…**"            paired-asterisk strong — a *span*, not the bare
//     pair of asterisks (see hasStrongSpan)
//   - "```"              fenced code block delimiter (readme /
//     changelog files almost always have one)
//   - "# " at line start ATX heading
//   - "]("               inline / reference link
//   - "- " at line start unordered list item
//   - "* " at line start unordered list item
//
// The check is purely lexical — it does not parse the content with
// goldmark first. A pathological input that contains a literal strong
// span (e.g. `printf 'a **b** c' | cat`) would be mis-rendered; in
// practice such content never appears in tool output.
func LooksLikeMarkdown(s string) bool {
	if hasStrongSpan(s) {
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

// hasStrongSpan reports whether s carries a CommonMark strong span:
// `**` immediately followed by non-space text, and a later `**` immediately
// preceded by non-space text on the same line.
//
// A bare `**` is NOT a marker. Source code is full of them — Go's
// `meta **StreamMeta`, C's `char **argv`, a `**/*.go` glob, `2**10` — and
// treating the pair as "this is markdown" made one tool output render
// preformatted and the next dedented with its backticks eaten
// (2026-11-14). The flanking rules keep both halves of a real emphasis
// span (`**important**`) while refusing every one of those code shapes.
func hasStrongSpan(s string) bool {
	for i := 0; i+1 < len(s); i++ {
		if s[i] != '*' || s[i+1] != '*' {
			continue
		}
		if i+2 >= len(s) {
			return false
		}
		switch s[i+2] {
		case ' ', '\t', '\n', '*':
			continue
		}
		for j := i + 3; j+1 < len(s); j++ {
			if s[j] == '\n' {
				break // emphasis does not cross a line in tool output
			}
			if s[j] != '*' || s[j+1] != '*' {
				continue
			}
			switch s[j-1] {
			case ' ', '\t', '\n':
				continue
			}
			return true
		}
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
