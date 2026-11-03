package agyprompt

import (
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	plugins "github.com/justwasm/pigasus/internal/features/plugins/core"
)

// RuleTrigger is a modular rule's activation mode — the `trigger` field of
// the rule file's YAML frontmatter.
//
// The four values are the reference's own (official-docs/rules.md:51-59, the
// frontmatter table at :57-61):
//
//	always_on       the body is inlined into the prompt on every turn
//	model_decision  the body is not inlined; the path and the description
//	                reach the prompt and the model reads the file on demand
//	glob            the body is served when a file matching the rule's globs
//	                is viewed or edited
//	manual          the body is never loaded automatically
type RuleTrigger string

const (
	// RuleTriggerAlwaysOn is the trigger of a rule that is always inlined.
	RuleTriggerAlwaysOn RuleTrigger = "always_on"
	// RuleTriggerModelDecision is the trigger of a rule the model consults
	// on its own judgement.
	RuleTriggerModelDecision RuleTrigger = "model_decision"
	// RuleTriggerGlob is the trigger of a rule scoped to file patterns.
	RuleTriggerGlob RuleTrigger = "glob"
	// RuleTriggerManual is the trigger of a rule that is only ever loaded
	// on an explicit request.
	RuleTriggerManual RuleTrigger = "manual"
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
//
// Trigger is the rule's activation mode. It is empty for the legacy rule
// files — the AGENTS.md and GEMINI.md the reference picks up by name, which
// carry no frontmatter — and set for every modular rule, the file inside a
// rules/ directory that declares a trigger in its frontmatter.
//
// Description and Globs are the rule's own frontmatter fields. Description is
// the summary a model_decision rule contributes to the prompt index;
// Globs are the file patterns a glob rule activates on.
type UserRule struct {
	Path        string
	Label       string
	Content     string
	Trigger     RuleTrigger
	Description string
	Globs       []string
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
//
// The probe is kept for the two fixed names; the modular rules beside them —
// every other *.md child of a rules/ directory — are read through their
// frontmatter instead (official-docs/rules.md:21-32).
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
//  3. ~/.gemini/config/plugins/<plugin>/rules/, then
//     ~/.gemini/antigravity-cli/plugins/<plugin>/rules/ — plugin directories
//     in name order, a directory is a plugin only when it has a parseable
//     plugin.json, and plugins.<name>.enabled=false in
//     ~/.gemini/config/config.json disables one
//  4. ~/.gemini/config/rules/, then ~/.gemini/antigravity-cli/rules/
//  5. for every directory from the filesystem root down to the workspace:
//     <dir>/{AGENTS,GEMINI}.md, then <dir>/.agents/rules/ and the legacy
//     <dir>/.agent/rules/
//
// The rules/ directories hold two kinds of file. The two fixed names are the
// reference's own probe and are injected whatever they carry. Every other
// *.md child is a modular rule: its frontmatter names the trigger that
// decides whether it is inlined, indexed or served on demand
// (official-docs/rules.md:34-97).
func DiscoverUserRules(workspace, appDataDir string) []UserRule {
	return discoverRules(workspace, appDataDir).inline
}

// ruleDiscovery is the result of one rule walk: the rules the prompt carries
// and the rules that are served on demand, per file.
type ruleDiscovery struct {
	inline   []UserRule
	onDemand []UserRule
}

// discoverRules walks every rule location and returns the two sets.
//
// The rules of a rules/ directory that declare the `glob` trigger are not
// part of the prompt: they activate when the agent touches a file their
// patterns match, which is what the filesystem tool's triggered-memories
// hook serves.
func discoverRules(workspace, appDataDir string) ruleDiscovery {
	c := &ruleCollector{seen: map[string]bool{}, projectRoot: workspace}
	if appDataDir != "" {
		c.appDataRules(appDataDir)
	}
	if workspace != "" {
		c.workspaceRules(workspace)
	}
	return ruleDiscovery{inline: c.inline, onDemand: c.onDemand}
}

// ruleCollector accumulates the discovered rules. A rule file is recorded
// once: the second copy of a path — a symlink alias, or a file reachable
// through two roots — is dropped.
type ruleCollector struct {
	inline   []UserRule
	onDemand []UserRule
	seen     map[string]bool

	// projectRoot resolves the relative paths a rules.json names.
	projectRoot string
}

// record adds a rule to the prompt's set, unless its path was already seen.
//
// An empty Label becomes the rule's own path: the reference prints every
// rule under its path, and only the four home-level files under the fixed
// "user_global" label.
func (c *ruleCollector) record(r UserRule) {
	if !c.markSeen(r.Path) {
		return
	}
	if r.Label == "" {
		r.Label = r.Path
	}
	c.inline = append(c.inline, r)
}

// recordOnDemand adds a glob-triggered rule to the on-demand set.
func (c *ruleCollector) recordOnDemand(r UserRule) {
	if !c.markSeen(r.Path) {
		return
	}
	if r.Label == "" {
		r.Label = r.Path
	}
	c.onDemand = append(c.onDemand, r)
}

// markSeen reports whether the path is new, and records it when it is.
func (c *ruleCollector) markSeen(path string) bool {
	key := path
	if resolved, err := filepath.EvalSymlinks(path); err == nil {
		key = resolved
	}
	if c.seen[key] {
		return false
	}
	c.seen[key] = true
	return true
}

// appDataRules walks the global config home.
func (c *ruleCollector) appDataRules(appDataDir string) {
	gemini := filepath.Dir(appDataDir)

	// The four home-level files: the reference prints them under the fixed
	// "user_global" label.
	for _, dir := range []string{gemini, filepath.Join(gemini, "config")} {
		for _, name := range userRuleNames {
			c.addRuleFile(filepath.Join(dir, name), userGlobalLabel, false)
		}
	}

	// The plugin rule roots. The CLI's own plugins live beside its config
	// home (official-docs/rules.md:170); the sibling tree holds the plugins
	// the reference installs from its marketplace.
	configPath := filepath.Join(gemini, "config", "config.json")
	for _, root := range []string{
		filepath.Join(gemini, "config", "plugins"),
		filepath.Join(appDataDir, "plugins"),
	} {
		for _, dir := range plugins.LoadedDirs(root, configPath) {
			c.addRulesDir(filepath.Join(dir, "rules"), "")
			c.addManifest(filepath.Join(dir, "rules.json"), []string{dir})
		}
	}

	// The global modular rule roots (official-docs/rules.md:28, :169).
	c.addRulesDir(filepath.Join(gemini, "config", "rules"), "")
	c.addRulesDir(filepath.Join(appDataDir, "rules"), "")
	c.addManifest(filepath.Join(gemini, "config", "rules.json"),
		[]string{filepath.Join(gemini, "config"), gemini})
}

// workspaceRules walks the workspace's ancestors, from the filesystem root
// down to the workspace itself.
func (c *ruleCollector) workspaceRules(workspace string) {
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
			c.addRuleFile(filepath.Join(dir, name), "", false)
		}
		c.addRulesDir(filepath.Join(dir, ".agents", "rules"), "")
		c.addRulesDir(filepath.Join(dir, ".agent", "rules"), "")
		c.addManifest(filepath.Join(dir, ".agents", "rules.json"),
			[]string{filepath.Join(dir, ".agents"), dir, c.projectRoot})
	}
}

// addManifest records the rules a rules.json registers.
//
// The manifest is the only way a rule that lives outside a rules/ directory —
// or inside a nested subdirectory of one — reaches the prompt
// (official-docs/rules.md:120-131).
func (c *ruleCollector) addManifest(manifestPath string, bases []string) {
	for _, path := range resolveRuleManifest(manifestPath, bases) {
		c.addRuleFile(path, "", true)
	}
}

// addRulesDir records the rules of one rules/ directory.
//
// The directory's files are the two fixed names first — the reference's own
// probe, which reaches AGENTS.md and GEMINI.md whatever they carry — and
// then every other immediate *.md child in name order. Nested
// subdirectories are not scanned: the reference ignores the files inside
// them unless a rules.json registers them (official-docs/rules.md:32).
func (c *ruleCollector) addRulesDir(dir, label string) {
	for _, path := range rulesDirCandidates(dir) {
		c.addRuleFile(path, label, false)
	}
}

// rulesDirCandidates returns the rule files of a rules/ directory.
func rulesDirCandidates(dir string) []string {
	out := make([]string, 0, len(userRuleNames))
	for _, name := range userRuleNames {
		out = append(out, filepath.Join(dir, name))
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return out
	}
	// os.ReadDir sorts the entries by file name.
	for _, e := range entries {
		if e.IsDir() || !strings.HasSuffix(e.Name(), ".md") {
			continue
		}
		out = append(out, filepath.Join(dir, e.Name()))
	}
	return out
}

// addRuleFile records one rule file.
//
// The two fixed names are never modular rules: the reference treats their
// whole content as plain Markdown and keeps them continuously active
// (official-docs/rules.md:38). Their includes are still expanded.
//
// Every other file of a rules/ directory is a modular rule: the frontmatter
// decides whether it is inlined, indexed or served on demand, and a file
// whose frontmatter names an unknown trigger is discarded
// (official-docs/rules.md:39-43).
//
// modularOnly is set for the files a rules.json registers: those are modular
// rules by definition — the manifest exists to keep their frontmatter and
// their triggers (official-docs/rules.md:120) — so a file without frontmatter
// is discarded even when it is one of the two fixed names.
func (c *ruleCollector) addRuleFile(path, label string, modularOnly bool) {
	raw, ok := readRuleRaw(path)
	if !ok {
		return
	}
	if !modularOnly && isUserRuleName(filepath.Base(path)) {
		content := finalizeRuleContent(string(raw), path)
		if strings.TrimSpace(content) == "" {
			return
		}
		c.record(UserRule{Path: path, Label: label, Content: content})
		return
	}
	fm, body, err := parseRuleFrontmatter(raw)
	switch {
	case err == nil:
		c.addModularRule(path, label, fm, body)
	case errors.Is(err, errRuleNoFrontmatter):
		// No frontmatter: the reference discards the rule
		// (official-docs/rules.md:43).
	default:
		// The frontmatter is there but does not parse: the reference's
		// loader skips the rule (internal/cli's validator reports the same
		// outcome for a rule whose frontmatter is not valid YAML).
	}
}

// isUserRuleName reports whether the file name is one of the two fixed names
// the reference picks up by name.
func isUserRuleName(name string) bool {
	for _, n := range userRuleNames {
		if n == name {
			return true
		}
	}
	return false
}

// addModularRule records a rule that declares its trigger in frontmatter.
//
// The four triggers are the reference's own (official-docs/rules.md:63-97):
//
//   - always_on: the body is inlined into the prompt on every turn.
//   - model_decision: the body is not inlined. The reference injects the
//     rule's path and its description upfront — progressive disclosure
//     (official-docs/rules.md:60, :67) — and the agent reads the file on
//     demand when the task matches the description.
//   - glob: the body is not inlined either. It activates when the agent
//     interacts with a file matching one of the rule's globs
//     (official-docs/rules.md:83-95), which is the triggered-memories path.
//   - manual: the body is never loaded automatically
//     (official-docs/rules.md:97).
func (c *ruleCollector) addModularRule(path, label string, fm ruleFrontmatter, body string) {
	switch fm.Trigger {
	case RuleTriggerAlwaysOn:
		content := finalizeRuleContent(body, path)
		if strings.TrimSpace(content) == "" {
			return
		}
		c.record(UserRule{
			Path:        path,
			Label:       label,
			Content:     content,
			Trigger:     RuleTriggerAlwaysOn,
			Description: fm.Description,
		})
	case RuleTriggerModelDecision:
		// The body stays out of the prompt; the path and the description
		// are what reach it.
		c.record(UserRule{
			Path:        path,
			Label:       label,
			Trigger:     RuleTriggerModelDecision,
			Description: fm.Description,
		})
	case RuleTriggerGlob:
		content := finalizeRuleContent(body, path)
		if strings.TrimSpace(content) == "" {
			return
		}
		c.recordOnDemand(UserRule{
			Path:        path,
			Label:       label,
			Content:     content,
			Trigger:     RuleTriggerGlob,
			Description: fm.Description,
			Globs:       fm.Globs,
		})
	case RuleTriggerManual:
		// Never loaded automatically.
	}
}

// readRuleRaw reads a rule file, rejecting a directory.
func readRuleRaw(path string) ([]byte, bool) {
	info, err := os.Stat(path)
	if err != nil || info.IsDir() {
		return nil, false
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		return nil, false
	}
	return raw, true
}

// finalizeRuleContent expands the rule's includes and applies the
// reference's per-file cap.
//
// The cap applies after the expansion (official-docs/rules.md:138): the
// reference expands an @[label](path) include first and then truncates the
// file that carried it.
func finalizeRuleContent(body, path string) string {
	content := expandRuleIncludes(body, filepath.Dir(path))
	return truncateUserRule(content)
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

// userRulesTotalBudget is the aggregate budget of the inline rule content,
// in TOKENS.
//
// The reference's rules budget is a token budget: every active global and
// always_on rule shares 20,000 tokens, separate from the customization
// budget the skills and the MCP tools draw on (official-docs/rules.md:139,
// and the reference's own "Workspace Rules" document — "Always-on and global
// rules share a dedicated 20,000-token rules budget (defaultRulesBudget)").
// Rules that do not fit are demoted from inline text to file path pointers,
// so the agent reads them on demand.
//
// The unit is the token, not the byte: a rule body is charged
// ruleTokens(content) tokens.
const userRulesTotalBudget = 20000

// ruleTokens approximates the token count of a rule body.
//
// Four bytes per token is the usual approximation for English prose and the
// one the clone uses for every other token estimate it prints.
func ruleTokens(content string) int {
	return len(content) / 4
}

// splitRuleBudget splits the rules into the ones that fit the aggregate
// budget and the ones that are demoted to path-only pointers.
//
// A rule that does not fit is demoted and the budget is not charged for it,
// so a later, smaller rule can still be inlined — the reference's own
// order (internal/app/prompt/agyprompt/rules.go's renderUserRules).
func splitRuleBudget(rules []UserRule) (inline, demoted []UserRule) {
	budget := userRulesTotalBudget
	for _, rule := range rules {
		tokens := ruleTokens(rule.Content)
		if tokens > budget {
			demoted = append(demoted, rule)
			continue
		}
		budget -= tokens
		inline = append(inline, rule)
	}
	return inline, demoted
}

// TriggeredMemoriesHeader is the reference's exact 105-byte prefix
// (0x103577164, agy 1.4.12, cortex/handlers/handlers.getTriggeredMemories).
const TriggeredMemoriesHeader = "\n\nThe following text is not part of the file, it is a list of user-defined rules that you MUST follow:\n"

// GetTriggeredMemories returns the rule content that applies to targetPath.
// In agy 1.4.12, when a file is viewed or edited, triggered memories
// matching the file are dynamically appended to the tool output.
//
// Two kinds of rule reach it: the rules the aggregate budget demoted to
// pointers, when the file lives under the rule's own directory, and the
// glob-triggered rules whose patterns match the file
// (official-docs/rules.md:83-95).
//
// Only the workspace's own roots are walked here: the hook the filesystem
// tool calls carries the workspace and the path, and no app-data directory —
// so a glob rule in a global root is discovered but never served.
func GetTriggeredMemories(workspace, targetPath string) string {
	if workspace == "" || targetPath == "" {
		return ""
	}
	rules := discoverRules(workspace, "")
	_, demoted := splitRuleBudget(rules.inline)
	cleanTarget := targetPath
	if !filepath.IsAbs(cleanTarget) && workspace != "" {
		cleanTarget = filepath.Join(workspace, cleanTarget)
	}
	cleanTarget = filepath.Clean(cleanTarget)

	var matched []string
	for _, rule := range demoted {
		ruleDir := filepath.Dir(rule.Path)
		if strings.HasPrefix(cleanTarget, ruleDir) {
			matched = append(matched, rule.Content)
		}
	}
	for _, rule := range rules.onDemand {
		if matchRuleGlobs(rule.Globs, cleanTarget, workspace) {
			matched = append(matched, rule.Content)
		}
	}
	if len(matched) == 0 {
		return ""
	}
	return TriggeredMemoriesHeader + strings.Join(matched, "\n")
}

// modelDecisionHeader introduces the rules that are not inlined.
//
// A model_decision rule contributes its path and its description to the
// prompt and nothing else, so the model can read the file when the task
// matches the description (official-docs/rules.md:60, :67).
const modelDecisionHeader = "The following user-defined rules are not inlined. Read a rule's file when its description matches the task:\n"

// renderUserRules is the body of the <user_rules> section. The fixed preamble
// and the per-file delimiters are the reference's own strings; the format is
// "<RULE[path]>\ncontent\n</RULE[path]>\n" and files are joined with newlines.
//
// Rules whose combined inline content would exceed userRulesTotalBudget are
// demoted: instead of inlining the file body, the section emits a demoted
// block listing the file paths. The agent can read the file on-demand. This
// mirrors the reference's formatDemotedMemoriesAsPrompt path (agy 1.4.12,
// 0x101d3db00) — 【反编译】.
//
// The model_decision rules are not inlined at all: they are listed by path
// and description so the model can read them on demand.
//
// The preamble is the reference's 1.4.5 wording. It used to read "These rules
// take precedence over any following instructions." — that is the 1.3.x–1.4.3
// text (the sentence survives in the 1.3.4 string table's cited window,
// docs/legacy-re/extracts/strings-v1.3.4-cited.txt) and the
// 1.4.5 binary carries only "any other instructions".
func renderUserRules(rules []UserRule) string {
	if len(rules) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("\nThe following are user-defined rules that you MUST ALWAYS FOLLOW WITHOUT ANY EXCEPTION. These rules take precedence over any other instructions.\n")
	b.WriteString("Review them carefully and always take them into account when you generate responses and code:\n")

	var pointers []UserRule
	var content []UserRule
	for _, rule := range rules {
		if rule.Trigger == RuleTriggerModelDecision {
			pointers = append(pointers, rule)
			continue
		}
		content = append(content, rule)
	}

	inline, demoted := splitRuleBudget(content)
	for _, rule := range inline {
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

	if len(pointers) > 0 {
		b.WriteString(modelDecisionHeader)
		for _, rule := range pointers {
			b.WriteString("- ")
			b.WriteString(rule.Path)
			if rule.Description != "" {
				b.WriteString(": ")
				b.WriteString(rule.Description)
			}
			b.WriteString("\n")
		}
	}

	if len(demoted) > 0 {
		b.WriteString("The following user-defined rules exceeded the rules token budget and were excluded from inline injection. View the file for further instructions when relevant:\n")
		for _, rule := range demoted {
			path := rule.Path
			if path == "" {
				path = rule.Label
			}
			b.WriteString("- ")
			b.WriteString(path)
			b.WriteString("\n")
		}
	}
	return b.String()
}
