// convstore/rendertitle.go — the title half of a tool step's render_info.
//
// # The two halves
//
// The reference stores a tool call's collapsed line as two fields, not
// one:
//
//	field 5  ToolPrefix   the verb, selected by the tool's category
//	                     ("Ran", "Analyzed", "Created", "Edited", …)
//	field 9  TitlePart   the *noun phrase* the verb applies to
//	                     ("go build ./...", "main.go", "2 files")
//
// A reader joins them ("Ran" + " " + "go build ./..."). So field 9 must
// not carry the verb: the joined line would print it twice.
//
// # Why this is its own file
//
// Two callers need the same derivation and neither may own it alone:
//
//	write.go's encodeStepRenderInfo  computes the title it stores
//	the panel's Trajectory reader     computes the title to display
//
// A second copy of this rule is how "Ran Ran (go build ./...)" appeared:
// the host wrote the already-collapsed display line into a field the
// reference defines as the noun phrase, and every reader re-attached the
// verb on top of it.
package convstore

import (
	"encoding/json"
	"path/filepath"
	"strings"
)

// renderIcon returns the render_info icon and aggregation key a tool
// name maps to.
//
// The mapping is the reference's own: it decides which of the
// render_info's content blocks the step carries (a terminal block, a
// file block, a markdown block), so it is not a display detail.
func renderIcon(toolName string) (icon, aggKey string) {
	toolLower := strings.ToLower(toolName)
	switch {
	case strings.Contains(toolLower, "command") || strings.Contains(toolLower, "bash") || toolLower == "sh":
		return "terminal", "terminal"
	case strings.Contains(toolLower, "read") ||
		(strings.Contains(toolLower, "file") && !strings.Contains(toolLower, "edit") && !strings.Contains(toolLower, "write")):
		return "description", "files"
	case strings.Contains(toolLower, "write"):
		return "edit", "edits"
	case strings.Contains(toolLower, "edit"):
		return "edit", "edits"
	case strings.Contains(toolLower, "task"):
		return "task_alt", "tasks"
	default:
		return "terminal", "terminal"
	}
}

// RenderPrefix returns the verb the reference stores in field 5 for a
// tool name. DefaultPrefix is what the category uses when the step
// carries no prefix of its own.
func RenderPrefix(toolName string) string {
	toolLower := strings.ToLower(toolName)
	switch {
	case strings.Contains(toolLower, "command") || strings.Contains(toolLower, "bash") || toolLower == "sh":
		return "Ran"
	case strings.Contains(toolLower, "read") ||
		(strings.Contains(toolLower, "file") && !strings.Contains(toolLower, "edit") && !strings.Contains(toolLower, "write")):
		return "Analyzed"
	case strings.Contains(toolLower, "write"):
		return "Created"
	case strings.Contains(toolLower, "edit"):
		return "Edited"
	case strings.Contains(toolLower, "task"):
		return "Checked task"
	default:
		return "Ran"
	}
}

// ToolArgsFields is the part of a tool's arguments the render_info
// describes the call with.
type ToolArgsFields struct {
	// CommandLine is a shell call's command, when the arguments carry
	// one.
	CommandLine string
	// Cwd is the working directory the call ran in.
	Cwd string
	// Path is the file the call targets.
	Path string
}

// ParseToolArgsFields reads the describe-the-call fields out of a tool's
// raw arguments. Arguments that are not a JSON object describe nothing,
// and the zero value says so rather than guessing.
func ParseToolArgsFields(args string) ToolArgsFields {
	if !strings.HasPrefix(strings.TrimSpace(args), "{") {
		return ToolArgsFields{}
	}
	var m map[string]any
	if err := json.Unmarshal([]byte(args), &m); err != nil {
		return ToolArgsFields{}
	}
	var f ToolArgsFields
	if c, ok := m["CommandLine"].(string); ok {
		f.CommandLine = c
	}
	if w, ok := m["Cwd"].(string); ok {
		f.Cwd = w
	}
	if p, ok := m["Path"].(string); ok {
		f.Path = p
	}
	return f
}

// RenderTitle is the noun phrase the reference stores in field 9: what
// the call did, without the verb.
//
// The order is the reference's fallback chain: the caller's own summary,
// then the command, then the file's basename, then the raw arguments,
// then the tool's name. Every branch is a narrower description of the
// same call, so a step that knows less still gets a title rather than an
// empty field.
func RenderTitle(summary, toolName, args string) string {
	if title := strings.TrimSpace(summary); title != "" {
		return title
	}
	f := ParseToolArgsFields(args)
	switch {
	case f.CommandLine != "":
		return f.CommandLine
	case f.Path != "":
		return filepath.Base(f.Path)
	case strings.TrimSpace(args) != "":
		return args
	default:
		return toolName
	}
}

// TitleFromSummary is RenderTitle for a caller holding a display summary
// that may already carry a verb.
//
// toolsteps.Step.Summary is the collapsed line the transcript draws —
// "Ran (go build ./...)", "Explored 1 file (main.go)" — while the
// store's ToolSummary is the noun phrase alone. Storing the display line
// is what made every reader print the verb twice.
//
// The unwrap is by shape rather than by a verb list, because the display
// vocabulary and the store's are not the same: tuitools renders a read as
// "Explored 1 file (main.go)" while render_info's field 5 carries
// "Analyzed". A list of verbs would miss the second spelling; "the text
// in the trailing parentheses is the noun phrase" catches both.
func TitleFromSummary(summary, toolName, args string) string {
	s := strings.TrimSpace(summary)
	if s == "" {
		return RenderTitle("", toolName, args)
	}
	// "Ran (go build ./...)" -> "go build ./..."
	// "Explored 1 file (main.go)" -> "main.go"
	if title, ok := unwrapTrailingParens(s); ok {
		if !placeholderTitle(title) {
			return title
		}
		// "Ran (a command)" names the shape of the call, not the call:
		// the arguments describe it better than the placeholder does.
		if derived := RenderTitle("", toolName, args); derived != "" && derived != toolName {
			return derived
		}
		return ""
	}
	// "Ran go build ./..." — the bare form, no parentheses. Stripping the
	// store's own prefix is safe here because that is the verb the reader
	// would re-attach; a title that legitimately begins with it is
	// indistinguishable and no worse off, since the joined line stays
	// readable either way.
	if prefix := RenderPrefix(toolName); prefix != "" {
		if rest, ok := strings.CutPrefix(s, prefix+" "); ok {
			if rest = strings.TrimSpace(rest); rest != "" {
				return rest
			}
		}
	}
	return RenderTitle(s, toolName, args)
}

// unwrapTrailingParens returns the text inside a trailing "(…)" group
// when the group has something before it to describe.
//
// A summary that *is* a parenthesised group ("(no output)") carries no
// verb, so there is nothing to unwrap and the caller keeps it whole.
func unwrapTrailingParens(s string) (string, bool) {
	if !strings.HasSuffix(s, ")") {
		return "", false
	}
	open := strings.LastIndex(s, " (")
	if open <= 0 {
		return "", false
	}
	inner := strings.TrimSpace(s[open+2 : len(s)-1])
	if inner == "" {
		return "", false
	}
	return inner, true
}

// placeholderTitle reports whether an unwrapped title names the kind of
// call rather than the call itself.
//
// The renderers emit these when they have nothing more specific: a
// command with no command line still renders "Ran (a command)", and the
// row is more useful carrying the arguments.
func placeholderTitle(title string) bool {
	switch strings.ToLower(title) {
	case "a command", "a file", "1 file", "some files", "contents":
		return true
	}
	return false
}
