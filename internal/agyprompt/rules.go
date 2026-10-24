package agyprompt

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/plugins"
)

// UserRule is one file injected into the <user_rules> section.
//
// Path is the absolute path of the file; Content is the file body after the
// reference's 24 KB line-boundary truncation.
//
// Label is the text printed inside the <RULE[...]> delimiter. It is the file's
// own path for every scope except the four home-level files, which the
// reference prints as the fixed label "user_global" (【一手】 agy 1.4.5, live
// request body). An empty Label means Path.
type UserRule struct {
	Path    string
	Label   string
	Content string
}

// userRuleByteCap is the reference's inline-rule budget. Measured against
// agy 1.4.3 with this repository's 54,120-byte AGENTS.md: the prompt kept
// 23,923 bytes (the last complete line under 24,000) and printed
// "<truncated 30197 bytes>". See docs/cycles/cycle-292-context-tokens/.
const userRuleByteCap = 24000

// userRuleNames are the two file names the reference picks up in every
// directory it scans. The reference takes AGENTS.md then GEMINI.md, and
// ignores any other file name in the same directory — 【一手】 agy 1.4.5:
// ~/.gemini/config/rules/PROBE.md, .agents/rules/PROBE.md and
// <plugin>/rules/ANY.md were all left out while the AGENTS.md and GEMINI.md
// beside them were injected.
var userRuleNames = []string{"AGENTS.md", "GEMINI.md"}

// userGlobalLabel is the delimiter the reference prints for the four
// home-level rule files (~/.gemini/{AGENTS,GEMINI}.md and
// ~/.gemini/config/{AGENTS,GEMINI}.md) instead of their path.
const userGlobalLabel = "user_global"

// DiscoverUserRules walks the global config home and the workspace ancestors,
// returning the rule files the reference would inject, in the reference's
// order. Empty files are skipped, symlink aliases are de-duplicated, and
// oversized files are cut at the last complete line under userRuleByteCap.
//
// The search set was re-derived against agy 1.4.5 by injecting probe files
// and reading the live request body back (【一手】). The order is:
//
//  1. ~/.gemini/AGENTS.md, ~/.gemini/GEMINI.md          (label "user_global")
//  2. ~/.gemini/config/AGENTS.md, ~/.gemini/config/GEMINI.md (label "user_global")
//  3. ~/.gemini/config/plugins/<plugin>/rules/{AGENTS,GEMINI}.md, plugin
//     directories in name order — a directory is a plugin only when it has a
//     parseable plugin.json, and plugins.<name>.enabled=false in
//     ~/.gemini/config/config.json disables one
//  4. ~/.gemini/config/rules/{AGENTS,GEMINI}.md
//  5. for every directory from the filesystem root down to the workspace:
//     <dir>/{AGENTS,GEMINI}.md then <dir>/.agents/rules/{AGENTS,GEMINI}.md
func DiscoverUserRules(workspace, appDataDir string) []UserRule {
	var out []UserRule
	seen := make(map[string]bool)

	add := func(path, label string) {
		content, ok := readUserRule(path)
		if !ok {
			return
		}
		key := path
		if resolved, err := filepath.EvalSymlinks(path); err == nil {
			key = resolved
		}
		if seen[key] {
			return
		}
		seen[key] = true
		if label == "" {
			label = path
		}
		out = append(out, UserRule{Path: path, Label: label, Content: content})
	}

	if appDataDir != "" {
		discoverAppDataRules(appDataDir, add)
	}
	if workspace != "" {
		discoverWorkspaceRules(workspace, add)
	}
	return out
}

func discoverAppDataRules(appDataDir string, add func(path, label string)) {
	gemini := filepath.Dir(appDataDir)
	for _, dir := range []string{gemini, filepath.Join(gemini, "config")} {
		for _, name := range userRuleNames {
			add(filepath.Join(dir, name), userGlobalLabel)
		}
	}
	pluginDirs := plugins.LoadedDirs(filepath.Join(gemini, "config", "plugins"), filepath.Join(gemini, "config", "config.json"))
	for _, dir := range pluginDirs {
		for _, name := range userRuleNames {
			add(filepath.Join(dir, "rules", name), "")
		}
	}
	for _, name := range userRuleNames {
		add(filepath.Join(gemini, "config", "rules", name), "")
	}
}

func discoverWorkspaceRules(workspace string, add func(path, label string)) {
	abs, err := filepath.Abs(workspace)
	if err == nil {
		workspace = abs
	}
	var dirs []string
	for dir := filepath.Clean(workspace); ; dir = filepath.Dir(dir) {
		dirs = append(dirs, dir)
		if filepath.Dir(dir) == dir {
			break
		}
	}
	for i := len(dirs) - 1; i >= 0; i-- {
		dir := dirs[i]
		for _, name := range userRuleNames {
			add(filepath.Join(dir, name), "")
		}
		for _, name := range userRuleNames {
			add(filepath.Join(dir, ".agents", "rules", name), "")
		}
	}
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
//
// A file under the cap is kept verbatim, trailing newline included: the
// reference writes the rule body as it is and then adds the closing
// delimiter on its own line, so a rule that ends with a newline renders with
// a blank line before </RULE[...]> (【一手】 agy 1.4.5, live request body).
func truncateUserRule(content string) string {
	if len(content) <= userRuleByteCap {
		return content
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
//
// The preamble is the reference's 1.4.5 wording. It used to read "These rules
// take precedence over any following instructions." — that is the 1.3.x–1.4.3
// text (it is still in docs/decompiled-v1.3.4/strings-v1.3.4.txt) and the
// 1.4.5 binary carries only "any other instructions".
func renderUserRules(rules []UserRule) string {
	if len(rules) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nThe following are user-defined rules that you MUST ALWAYS FOLLOW WITHOUT ANY EXCEPTION. These rules take precedence over any other instructions.\n")
	b.WriteString("Review them carefully and always take them into account when you generate responses and code:\n")
	for _, rule := range rules {
		label := rule.Label
		if label == "" {
			label = rule.Path
		}
		b.WriteString("<RULE[")
		b.WriteString(label)
		b.WriteString("]>\n")
		b.WriteString(rule.Content)
		b.WriteString("\n</RULE[")
		b.WriteString(label)
		b.WriteString("]>\n")
	}
	return b.String()
}
