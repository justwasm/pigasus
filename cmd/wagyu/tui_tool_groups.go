package main

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

func renderStepTitle(s toolStep, expanded bool) []string {
	if s.kind == stepKindThought {
		return []string{thoughtBulletOpen + " " + thoughtTitle(s.dur, s.tokens)}
	}
	return []string{styledStepBullet(s, expanded) + " " + styledCallTitle(s)}
}

// parenArg returns the inside of the final parenthesised group in a title,
// e.g. "Bash(echo one)" -> "echo one", "Ran (no parens here)" -> "".
func parenArg(title string) string {
	i := strings.LastIndexByte(title, '(')
	if i < 0 || !strings.HasSuffix(title, ")") {
		return ""
	}
	return title[i+1 : len(title)-1]
}

// stripBashParens extracts the inner command from a "Bash (cmd)" or
// "Bash(cmd)" title; returns cmd and ok=false when the prefix doesn't.
func stripBashParens(title string) (cmd string, ok bool) {
	if !strings.HasPrefix(title, "Bash ") {
		return "", false
	}
	rest := strings.TrimPrefix(title, "Bash ")
	rest = strings.TrimSpace(rest)
	if strings.HasPrefix(rest, "(") && strings.HasSuffix(rest, ")") {
		return rest[1 : len(rest)-1], true
	}
	return "", false
}

// splitShellSegments splits a shell command string into its pipeline
// segments, breaking at `&&`, `||`, `;`, or `\n` boundaries. The
// boundaries themselves are dropped from the returned segments.
//
// Cycle 178: the reference wraps `run_command` titles at these
// boundaries (verified by capturing the live `4638a7b0-…` transcript),
// so this is the only splitting rule the renderer needs.
func splitShellSegments(s string) []string {
	var out []string
	var cur strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		// Drop any leading whitespace on a new segment.
		if cur.Len() == 0 && (c == ' ' || c == '\t') {
			continue
		}
		if c == '\n' {
			if cur.Len() > 0 {
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
			continue
		}
		// Two-char operators: &&, ||.
		if i+1 < len(s) && s[i+1] == c && (c == '&' || c == '|') {
			if cur.Len() > 0 {
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
			i++ // skip the second char
			continue
		}
		if c == ';' {
			if cur.Len() > 0 {
				out = append(out, strings.TrimSpace(cur.String()))
				cur.Reset()
			}
			continue
		}
		cur.WriteByte(c)
	}
	if cur.Len() > 0 {
		out = append(out, strings.TrimSpace(cur.String()))
	}
	return out
}

// categoryWords is the reference's recovered singular/plural pair for each
// count category. Every pair below appears verbatim in the 1.3.6 string table
// ("1 file" / "%d files", "1 search" / "%d searches", …); the changelog records
// the fix — "Fixed counts in tool steps and the `/codesearch` panel, which now
// read \"Found 1 result\", \"1 match\", \"1 file\", and \"Wrote 1 line\" instead of
// \"1 results\"".
var categoryWords = map[string][2]string{
	"file":     {"file", "files"},
	"edit":     {"file", "files"},
	"search":   {"search", "searches"},
	"command":  {"command", "commands"},
	"tool":     {"tool", "tools"},
	"task":     {"task", "tasks"},
	"cron":     {"cron", "crons"},
	"page":     {"page", "pages"},
	"resource": {"resource", "resources"},
	"question": {"question", "questions"},
	"timer":    {"timer", "timers"},
	"image":    {"image", "images"},
	"step":     {"step", "steps"},
	"result":   {"result", "results"},
	// Cycle 228: /codesearch streams matches into a counter row
	// ("Found 3 matches in 1 file"). The pair rides alongside the
	// tool-step categories because the reference shares the same
	// singular/plural rules across both surfaces.
	"match": {"match", "matches"},
}

// toolCategories maps a recovered tool name onto its count category. The
// tool-name → category mapping is 【推测】 (the reference derives the category
// from its own step model); the vocabulary itself is not.
var toolCategories = map[string]string{
	"run_command":                "command",
	"command_status":             "command",
	"send_command_input":         "command",
	"view_file":                  "file",
	"read_file":                  "file",
	"write_to_file":              "edit",
	"write_file":                 "edit",
	"create_file":                "edit",
	"replace_file_content":       "edit",
	"edit_file":                  "edit",
	"multi_replace_file_content": "edit",
	"sed_file":                   "edit",
	"list_dir":                   "file",
	"notebook_edit":              "edit",
	"grep_search":                "search",
	"find_by_name":               "search",
	"codebase_search":            "search",
	"search_web":                 "search",
	"read_url_content":           "page",
	"view_content_chunk":         "page",
	"manage_task":                "task",
	"send_message":               "task",
	"wait":                       "task",
	"schedule":                   "cron",
	"list_resources":             "resource",
	"read_resource":              "resource",
	"ask_question":               "question",
	"generate_image":             "image",
}

// categoryOrder is the stable order categories appear in a mixed group header.
var categoryOrder = []string{
	"file", "edit", "search", "command", "page", "resource", "question",
	"timer", "image", "task", "cron", "tool", "step",
}

// toolCategory maps a tool name onto its count category.
func toolCategory(name string) string {
	if c, ok := toolCategories[name]; ok {
		return c
	}
	return "tool"
}

// countLabel renders "<n> <word>" using the reference's singular form when n is
// one ("1 file", never "1 files").
func countLabel(n int, word [2]string) string {
	if n == 1 {
		return "1 " + word[0]
	}
	return fmt.Sprintf("%d %s", n, word[1])
}

// groupSummary returns the three styled pieces of a batch header:
// "Ran" + "2 commands" + "(echo one · echo two)". Verified against 1.4.3.
func mixedGroupSummary(steps []toolStep) (verb, count, detail string) {
	counts := map[string]int{}
	for _, s := range steps {
		counts[toolCategory(s.name)]++
	}
	parts := make([]string, 0, len(counts))
	for _, c := range categoryOrder {
		if counts[c] > 0 {
			parts = append(parts, countLabel(counts[c], categoryWords[c]))
		}
	}
	return "Ran", strings.Join(parts, ", "), ""
}

func homogeneousGroupSummary(cat string, steps []toolStep) (verb, count, detail string) {
	switch cat {
	case "command":
		verb = "Ran"
		count = countLabel(len(steps), categoryWords["command"])
		detail = "(" + strings.Join(stepArgs(steps, parenArg), " \u00b7 ") + ")"
	case "file":
		verb = "Explored"
		count = countLabel(len(steps), categoryWords["file"])
		detail = "(" + strings.Join(stepArgs(steps, fileNameArg), " \u00b7 ") + ")"
	case "edit":
		verb = "Edited"
		count = countLabel(len(steps), categoryWords["edit"])
		detail = "(" + strings.Join(stepArgs(steps, fileNameArg), " \u00b7 ") + ")"
	case "search":
		verb = "Explored"
		count = countLabel(len(steps), categoryWords["search"])
		detail = "(" + strings.Join(stepArgs(steps, searchArg), " \u00b7 ") + ")"
	default:
		verb = "Ran"
		count = countLabel(len(steps), categoryWords[cat])
	}
	return verb, count, detail
}

func groupSummary(steps []toolStep) (verb, count, detail string) {
	cat := toolCategory(steps[0].name)
	for _, s := range steps[1:] {
		if toolCategory(s.name) != cat {
			return mixedGroupSummary(steps)
		}
	}
	return homogeneousGroupSummary(cat, steps)
}

// stepArgs extracts one display argument per step using pick.
func stepArgs(steps []toolStep, pick func(string) string) []string {
	out := make([]string, 0, len(steps))
	for _, s := range steps {
		if a := pick(s.title); a != "" {
			out = append(out, a)
			continue
		}
		out = append(out, s.name)
	}
	return out
}

// searchArg is the search-batch display argument: the bare query.
func searchArg(title string) string {
	if p := parenArg(title); p != "" {
		return p
	}
	return ""
}

// fileNameArg is the file-batch display argument: the basename, matching the
// reference's "Explored 1 file (agy-cont.sh)".
func fileNameArg(title string) string {
	if p := parenArg(title); p != "" {
		return filepath.Base(p)
	}
	return ""
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
	ds := humanDuration(d)
	if tokens > 0 {
		return fmt.Sprintf("Thought for %s, %d tokens", ds, tokens)
	}
	return "Thought for " + ds
}

// humanDuration renders a duration the way the reference does: whole seconds
// stay integral ("3s"), sub-second parts keep one decimal ("1.5s"), and
// anything under a second is shown in milliseconds.
func humanDuration(d time.Duration) string {
	if d < time.Second {
		return fmt.Sprintf("%dms", d.Milliseconds())
	}
	return d.Round(100 * time.Millisecond).String()
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
