// citation_strip.go implements AGY 1.2.10 cycle 230: gate citation
// extraction off Bash and Read tool results. The 1.2.10 changelog reads:
//
//	"Fixed tool result blocks for Bash and Read tools including
//	 citation-like [N] footnotes that the model never asked for,
//	 polluting the model context with garbage noise."
//
// Root cause: the upstream pipeline ran a search-result citation extractor
// on every tool result before the 1.2.10 fix. Search/web results genuinely
// need that annotation, but Bash and Read tools produce shell / filesystem
// text that has nothing to cite. The extracted `[N] https://...` lines
// leaked into the model's transcript as garbage the model never asked for.
//
// Fix: detect the trailing citation block (a `Sources:` / `Citations:` /
// `References:` / `Footnotes:` header followed by numbered citation lines,
// or a bare block of ≥ 2 consecutive numbered citation lines) and strip
// it from the result text before it is fed back to the model.
//
// The detection is intentionally conservative. A citation block must be at
// the *trailing* non-whitespace content of the result text. Real `[N]`
// occurrences inside prose — `printf "[%d]\n", 1`, grep matches that print
// numbered lines, an array index `arr[3]` — are left alone because they
// do not form a contiguous trailing block. The header keyword is matched
// against a tight `^\s*Sources:\s*$` pattern so a prose sentence that
// happens to mention "Sources:" is not mistaken for a citation header.
package tools

import (
	"regexp"
	"strings"
)

// citationStripTools is the set of tool names whose result text is run
// through StripCitationFootnotes before reaching the model. Only Bash and
// Read tools are gated — search_web / read_url_content / generate_image
// / call_mcp_tool legitimately surface citations and must keep them.
//
// The set covers every name under which Bash / Read results are emitted
// in this clone:
//
//   - run_command        (primary Bash tool, command.go)
//   - run_shell_command  (alias / shell mode, command.go)
//   - exec_command       (execvp mode, command.go)
//   - read_file          (ReadFile struct, filesystem.go)
//   - view_file          (reference alias of read_file, catalog.go)
var citationStripTools = map[string]bool{
	"run_command":       true,
	"run_shell_command": true,
	"exec_command":      true,
	"read_file":         true,
	"view_file":         true,
}

// citationHeaderRe matches the standalone header line that announces a
// citation block. The line must contain only the header keyword (plus
// optional whitespace) so a prose sentence that happens to contain the
// word "Sources:" is not mistaken for a header.
var citationHeaderRe = regexp.MustCompile(`^\s*(Sources|Citations|References|Footnotes)\s*:\s*$`)

// citationLineRe matches a single numbered citation line. Recognised
// shapes:
//
//	[1] https://example.com/page
//	[12] Title of paper
//	[1, 2, 3] Combined references
//	[1-5] A range of references
//
// The content after the bracket must be non-empty and contain at least
// one non-whitespace character, so an empty `[1]` line does not match.
// The line may have any amount of leading whitespace (the extractor
// sometimes indents the block).
var citationLineRe = regexp.MustCompile(`^\s*\[\d+(?:\s*[-,]\s*\d+)*\]\s+\S.*$`)

// ShouldStripCitations reports whether results from the named tool
// should be run through StripCitationFootnotes before being returned to
// the model. Currently true only for the Bash and Read tools; search /
// web tools legitimately carry citation annotations and are left alone.
//
// Exported so Dispatch callers (and tests) can apply the same gate without
// hard-coding the tool-name set twice.
func ShouldStripCitations(name string) bool {
	return citationStripTools[name]
}

// StripCitationFootnotes removes a citation footnote block from the end
// of text. A citation block is either:
//
//  1. a "Sources:" / "Citations:" / "References:" / "Footnotes:" header
//     line followed by one or more numbered citation lines, or
//  2. two or more consecutive numbered citation lines with no header.
//
// The block must be the trailing non-whitespace content of the input.
// Returns the input unchanged when no block is found.
//
// Real `[N]` references inside prose — `printf "[%d]\n", 1`, grep matches
// that print numbered lines, an array index `arr[3]` — are left alone
// because they do not form a contiguous trailing block. Year-style
// brackets like `[2024]` are also left alone because they contain four
// digits, more than a citation index.
func findCitationStart(lines []string, end int) (int, bool) {
	i := end
	for i > 0 && citationLineRe.MatchString(lines[i-1]) {
		i--
	}
	citeCount := end - i
	if citeCount == 0 {
		return 0, false
	}
	if i >= 1 && citationHeaderRe.MatchString(strings.TrimSpace(lines[i-1])) {
		i--
	} else if citeCount < 2 {
		return 0, false
	}
	for i > 0 && strings.TrimSpace(lines[i-1]) == "" {
		i--
	}
	return i, true
}

func StripCitationFootnotes(text string) string {
	if text == "" {
		return text
	}
	lines := strings.Split(text, "\n")
	end := len(lines)
	for end > 0 && strings.TrimSpace(lines[end-1]) == "" {
		end--
	}
	if end == 0 {
		return text
	}
	i, ok := findCitationStart(lines, end)
	if !ok {
		return text
	}
	var b strings.Builder
	for j := 0; j < i; j++ {
		if j > 0 {
			b.WriteByte('\n')
		}
		b.WriteString(lines[j])
	}
	for j := end; j < len(lines); j++ {
		b.WriteByte('\n')
	}
	return b.String()
}
