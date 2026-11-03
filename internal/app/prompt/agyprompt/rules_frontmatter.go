package agyprompt

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

// ruleFrontmatter is the frontmatter of a modular rule file.
//
// The reference's rules carry three fields (official-docs/rules.md:45-61):
// the trigger, the description and the globs. The singular `glob:` is
// accepted beside `globs:` (official-docs/rules.md:53).
type ruleFrontmatter struct {
	Trigger     RuleTrigger
	Description string
	Globs       []string
}

// errRuleNoFrontmatter is the error parseRuleFrontmatter returns for a file
// that carries no frontmatter block at all.
var errRuleNoFrontmatter = errors.New("agyprompt: the rule has no frontmatter")

// parseRuleFrontmatter splits a rule file into its frontmatter and its body.
//
// The block is the reference's own shape — the same one the plugin validator
// reads (internal/cli/plugin_validate_components.go's parseFrontmatter): the
// file must start with a "---" line, the block ends at the next "---", and
// that "---" must be on a line of its own. A file that does not start with
// "---" carries no frontmatter, and so does a file whose "---" is never
// closed.
//
// The frontmatter itself is read as the flat "key: value" list the
// reference's rules use, without a YAML dependency: four trigger values and
// two string fields do not need a parser for a general language. A line that
// carries no key at all is malformed — the reference's YAML parser rejects
// it and its loader skips the rule.
//
// The body keeps the newline that follows the closing "---", exactly as the
// reference's own parseFrontmatter leaves it (internal/cli's
// plugin_validate_components.go): the block ends at the "---" line and the
// line break that terminated it is dropped, the blank line the author wrote
// after the frontmatter is not.
func parseRuleFrontmatter(raw []byte) (ruleFrontmatter, string, error) {
	text := strings.ReplaceAll(string(raw), "\r\n", "\n")
	if !strings.HasPrefix(text, "---\n") {
		return ruleFrontmatter{}, text, errRuleNoFrontmatter
	}
	rest := text[4:]
	end := strings.Index(rest, "\n---")
	if end < 0 {
		return ruleFrontmatter{}, text, errRuleNoFrontmatter
	}
	after := rest[end+4:]
	if after != "" && !strings.HasPrefix(after, "\n") {
		return ruleFrontmatter{}, text, errRuleNoFrontmatter
	}
	block := rest[:end]
	if strings.HasPrefix(after, "\n") {
		after = after[1:]
	}
	fm, err := parseRuleFrontmatterFields(block)
	if err != nil {
		return ruleFrontmatter{}, after, err
	}
	return fm, after, nil
}

// parseRuleFrontmatterFields reads the frontmatter's key/value lines.
func parseRuleFrontmatterFields(block string) (ruleFrontmatter, error) {
	var fm ruleFrontmatter
	lines := strings.Split(block, "\n")
	for i := 0; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" || strings.HasPrefix(trimmed, "#") {
			continue
		}
		key, value, ok := strings.Cut(trimmed, ":")
		if !ok {
			return fm, fmt.Errorf("the frontmatter line %q has no key", trimmed)
		}
		key = strings.TrimSpace(key)
		switch key {
		case "trigger", "description", "globs", "glob":
		default:
			// A field the rules do not use. The reference's parser reads
			// the frontmatter into a struct and ignores what it does not
			// know, so an unknown key is not an error.
			continue
		}
		value = strings.TrimSpace(value)
		if isBlockScalar(value) {
			value = readBlockScalar(lines, &i, value)
		}
		value = cleanScalar(value)
		switch key {
		case "trigger":
			fm.Trigger = RuleTrigger(value)
		case "description":
			fm.Description = value
		case "globs", "glob":
			fm.Globs = splitRuleGlobs(value)
		}
	}
	return fm, nil
}

// isBlockScalar reports whether the value opens a folded or literal block.
func isBlockScalar(value string) bool {
	switch value {
	case ">", ">-", ">+", "|", "|-", "|+":
		return true
	}
	return false
}

// readBlockScalar reads the indented lines that follow a block scalar.
//
// A folded block (">") joins its lines with a space, a literal block ("|")
// keeps its line breaks. The lines are returned without their indentation.
func readBlockScalar(lines []string, i *int, style string) string {
	indent := -1
	var parts []string
	last := *i
	for j := *i + 1; j < len(lines); j++ {
		line := lines[j]
		if strings.TrimSpace(line) == "" {
			parts = append(parts, "")
			last = j
			continue
		}
		lead := len(line) - len(strings.TrimLeft(line, " \t"))
		if indent < 0 {
			indent = lead
		}
		if lead < indent {
			break
		}
		parts = append(parts, strings.TrimSpace(line))
		last = j
	}
	*i = last
	joined := strings.Join(parts, " ")
	if strings.HasPrefix(style, "|") {
		joined = strings.Join(parts, "\n")
	}
	return strings.TrimSpace(joined)
}

// cleanScalar strips the quotes and the trailing comment of a value.
func cleanScalar(value string) string {
	if len(value) >= 2 {
		first, last := value[0], value[len(value)-1]
		if (first == '"' && last == '"') || (first == '\'' && last == '\'') {
			return value[1 : len(value)-1]
		}
	}
	if i := strings.Index(value, " #"); i >= 0 {
		value = value[:i]
	}
	return strings.TrimSpace(value)
}

// splitRuleGlobs splits a comma-separated list of glob patterns.
func splitRuleGlobs(value string) []string {
	var out []string
	for _, part := range strings.Split(value, ",") {
		part = strings.TrimSpace(part)
		if part != "" {
			out = append(out, part)
		}
	}
	return out
}

// matchRuleGlobs reports whether one of the rule's patterns matches the
// file.
//
// A pattern is matched against the file's base name and against its path
// relative to the workspace, so "*.proto" reaches a .proto file in any
// directory and "src/**/*.go" reaches a file below src.
//
// The patterns are the rule's own `globs` field
// (official-docs/rules.md:61, :83-95).
func matchRuleGlobs(globs []string, target, workspace string) bool {
	if len(globs) == 0 {
		return false
	}
	base := filepath.Base(target)
	rel := ""
	if workspace != "" {
		if r, err := filepath.Rel(workspace, target); err == nil {
			rel = filepath.ToSlash(r)
		}
	}
	for _, pattern := range globs {
		pattern = strings.TrimSpace(pattern)
		if pattern == "" {
			continue
		}
		if matchGlobPath(pattern, base) {
			return true
		}
		if rel != "" && matchGlobPath(pattern, rel) {
			return true
		}
	}
	return false
}

// matchGlobPath matches a slash-separated path against a glob pattern.
//
// A "*" matches inside one path segment, a "**" matches across segments, and
// a "?" matches one character.
func matchGlobPath(pattern, name string) bool {
	return matchGlobSegments(strings.Split(pattern, "/"), strings.Split(name, "/"))
}

// matchGlobSegments matches the segments of a pattern against the segments of
// a path.
func matchGlobSegments(pattern, name []string) bool {
	if len(pattern) == 0 {
		return len(name) == 0
	}
	if pattern[0] == "**" {
		for i := 0; i <= len(name); i++ {
			if matchGlobSegments(pattern[1:], name[i:]) {
				return true
			}
		}
		return false
	}
	if len(name) == 0 {
		return false
	}
	if !matchGlobSegment(pattern[0], name[0]) {
		return false
	}
	return matchGlobSegments(pattern[1:], name[1:])
}

// matchGlobSegment matches one path segment against one pattern segment.
func matchGlobSegment(pattern, name string) bool {
	ok, err := filepath.Match(pattern, name)
	return err == nil && ok
}
