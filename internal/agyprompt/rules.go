package agyprompt

import (
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
)

// UserRule is one file injected into the <user_rules> section.
//
// Path is the absolute path printed inside the <RULE[path]> delimiter; Content
// is the file body after the reference's 24 KB line-boundary truncation.
type UserRule struct {
	Path    string
	Content string
}

// userRuleByteCap is the reference's inline-rule budget. Measured against
// agy 1.4.3 with this repository's 54,120-byte AGENTS.md: the prompt kept
// 23,923 bytes (the last complete line under 24,000) and printed
// "<truncated 30197 bytes>". See docs/cycles/cycle-292-context-tokens/.
const userRuleByteCap = 24000

// userRuleNames are the standalone rule files the reference discovers in
// every directory from the workspace up to the filesystem root. The order is
// the one the binary's own picker documents: GEMINI.md then AGENTS.md.
var userRuleNames = []string{"GEMINI.md", "AGENTS.md"}

// DiscoverUserRules walks the global config home and the workspace ancestors,
// returning the rule files the reference would inject. Empty files are
// skipped, symlink aliases are de-duplicated, and oversized files are cut at
// the last complete line under userRuleByteCap.
//
// The search set is the one described by the reference's own customization
// documentation: ~/.gemini/{GEMINI,AGENTS}.md, ~/.gemini/config/*.md,
// ~/.gemini/config/rules/*.md, and, for every directory from the filesystem
// root down to the workspace, {GEMINI,AGENTS}.md plus .agents/rules/*.md.
func DiscoverUserRules(workspace, appDataDir string) []UserRule {
	var candidates []string

	if appDataDir != "" {
		gemini := filepath.Dir(appDataDir)
		for _, name := range userRuleNames {
			candidates = append(candidates, filepath.Join(gemini, name))
		}
		for _, name := range userRuleNames {
			candidates = append(candidates, filepath.Join(gemini, "config", name))
		}
		candidates = append(candidates, markdownFiles(filepath.Join(gemini, "config", "rules"))...)
	}

	if workspace != "" {
		abs, err := filepath.Abs(workspace)
		if err == nil {
			workspace = abs
		}
		// Root-first so a nested workspace rule is appended after its
		// ancestors, matching the reference's "more specific later" order.
		var dirs []string
		for dir := filepath.Clean(workspace); ; dir = filepath.Dir(dir) {
			dirs = append(dirs, dir)
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
		}
		for i := len(dirs) - 1; i >= 0; i-- {
			dir := dirs[i]
			for _, name := range userRuleNames {
				candidates = append(candidates, filepath.Join(dir, name))
			}
			candidates = append(candidates, markdownFiles(filepath.Join(dir, ".agents", "rules"))...)
		}
	}

	seen := make(map[string]bool, len(candidates))
	var out []UserRule
	for _, path := range candidates {
		content, ok := readUserRule(path)
		if !ok {
			continue
		}
		key := path
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			key = resolved
		}
		if seen[key] {
			continue
		}
		seen[key] = true
		out = append(out, UserRule{Path: path, Content: content})
	}
	return out
}

func markdownFiles(dir string) []string {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(strings.ToLower(e.Name()), ".md") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	sort.Strings(out)
	return out
}

func readUserRule(path string) (string, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return "", false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return "", false
	}
	content := truncateUserRule(string(raw))
	if strings.TrimSpace(content) == "" {
		return "", false
	}
	return content, true
}

// truncateUserRule reproduces the reference's cap: keep whole lines under
// userRuleByteCap, then append "<truncated N bytes>" where N counts the bytes
// dropped plus the newline that was replaced by the marker.
func truncateUserRule(content string) string {
	if len(content) <= userRuleByteCap {
		return strings.TrimRight(content, "\r\n")
	}
	cut := content[:userRuleByteCap]
	if i := strings.LastIndexByte(cut, '\n'); i >= 0 {
		cut = cut[:i]
	}
	kept := strings.TrimRight(cut, "\r\n")
	dropped := len(content) - len(kept)
	return kept + "\n<truncated " + strconv.Itoa(dropped) + " bytes>"
}

// renderUserRules is the body of the <user_rules> section. The fixed preamble
// and the per-file delimiters are the reference's own strings; the format is
// "<RULE[path]>\ncontent\n</RULE[path]>\n" and files are joined with newlines.
func renderUserRules(rules []UserRule) string {
	if len(rules) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nThe following are user-defined rules that you MUST ALWAYS FOLLOW WITHOUT ANY EXCEPTION. These rules take precedence over any following instructions.\n")
	b.WriteString("Review them carefully and always take them into account when you generate responses and code:\n")
	for _, rule := range rules {
		b.WriteString("<RULE[")
		b.WriteString(rule.Path)
		b.WriteString("]>\n")
		b.WriteString(strings.TrimRight(rule.Content, "\r\n"))
		b.WriteString("\n</RULE[")
		b.WriteString(rule.Path)
		b.WriteString("]>\n")
	}
	return b.String()
}
