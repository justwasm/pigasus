package agyprompt

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// ruleIncludePattern matches an @[label](path) reference.
//
// The reference supports two "@" syntaxes inside a rule (official-docs/rules.md:104-109):
// "@[label](path)" inlines the file's contents, and a bare "@filename" only
// resolves the path. Only the first one is expanded here.
var ruleIncludePattern = regexp.MustCompile(`@\[([^\]]*)\]\(([^)]*)\)`)

// maxRuleIncludeDepth is how deep an include may nest. A rule that includes
// a file which includes the rule itself would otherwise loop forever.
const maxRuleIncludeDepth = 4

// expandRuleIncludes inlines every @[label](path) reference of a rule body.
//
// The reference expands the include before it evaluates the prompt and
// before it applies the size limits (official-docs/rules.md:106, :138), so
// the expansion happens here, before the per-file cap.
//
// A relative path resolves against the directory that holds the rule file, a
// path that starts with "~/" against the user's home, and an absolute path
// against itself (official-docs/rules.md:107). An included file that carries
// frontmatter contributes its body only — the frontmatter and the trigger
// belong to the registry, not to the prompt (official-docs/rules.md:108).
//
// A reference to a file that cannot be read is left as it is: the rule keeps
// its text and the turn is not broken.
func expandRuleIncludes(content, ruleDir string) string {
	if !strings.Contains(content, "@[") {
		return content
	}
	return expandRuleIncludesDepth(content, ruleDir, 0)
}

// expandRuleIncludesDepth expands the includes of a rule body, up to
// maxRuleIncludeDepth levels.
func expandRuleIncludesDepth(content, ruleDir string, depth int) string {
	if depth > maxRuleIncludeDepth {
		return content
	}
	return ruleIncludePattern.ReplaceAllStringFunc(content, func(match string) string {
		parts := ruleIncludePattern.FindStringSubmatch(match)
		if len(parts) != 3 {
			return match
		}
		path := strings.TrimSpace(parts[2])
		if path == "" {
			return match
		}
		resolved := resolveRuleIncludePath(path, ruleDir)
		raw, err := os.ReadFile(resolved)
		if err != nil {
			// The file is gone: the reference leaves the text as it is.
			return match
		}
		body := stripRuleFrontmatter(raw)
		return expandRuleIncludesDepth(body, filepath.Dir(resolved), depth+1)
	})
}

// resolveRuleIncludePath resolves the path of an include.
func resolveRuleIncludePath(path, ruleDir string) string {
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil && home != "" {
			return filepath.Join(home, path[2:])
		}
		return path
	}
	if filepath.IsAbs(path) {
		return path
	}
	return filepath.Join(ruleDir, path)
}

// stripRuleFrontmatter returns a rule file's body: the content without the
// frontmatter block, when it has one.
func stripRuleFrontmatter(raw []byte) string {
	_, body, err := parseRuleFrontmatter(raw)
	if err != nil {
		return strings.ReplaceAll(string(raw), "\r\n", "\n")
	}
	return body
}
