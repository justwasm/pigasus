package permissions

import (
	"strings"
	"testing"
)

func TestModes_MatchReference(t *testing.T) {
	if got := ModeList(); got != "accept-edits, auto, plan" {
		t.Errorf("ModeList() = %q", got)
	}
	for _, m := range []string{"accept-edits", "auto", "plan"} {
		if !ValidMode(m) {
			t.Errorf("ValidMode(%q) = false", m)
		}
	}
	for _, m := range []string{"", "bogus", "AUTO", "auto ", "jetski"} {
		if ValidMode(m) {
			t.Errorf("ValidMode(%q) = true", m)
		}
	}
}

func TestVerdict_WireNames(t *testing.T) {
	cases := map[Verdict]string{
		Allow: "allow", Deny: "deny", Ask: "ask",
		ForceAsk: "force_ask", DenyUnlessPriorGrant: "deny_unless_prior_grant",
	}
	for v, want := range cases {
		if got := v.String(); got != want {
			t.Errorf("Verdict(%d).String() = %q, want %q", v, got, want)
		}
	}
}

func TestClassify(t *testing.T) {
	cases := map[string]Class{
		"write_to_file":              ClassEdit,
		"replace_file_content":       ClassEdit,
		"multi_replace_file_content": ClassEdit,
		"sed_file":                   ClassEdit,
		"notebook_edit":              ClassEdit,
		"run_command":                ClassCommand,
		"view_file":                  ClassRead,
		"grep_search":                ClassRead,
		"finish":                     ClassOther,
		// Cycle 293: the 1.4.4 wire names must land in the same classes as
		// their 1.3.6 counterparts, or plan / accept-edits modes treat a
		// read as an unknown tool and an edit as "other".
		"read_file":  ClassRead,
		"write_file": ClassEdit,
		"edit_file":  ClassEdit,
	}
	for tool, want := range cases {
		if got := Classify(tool); got != want {
			t.Errorf("Classify(%q) = %v, want %v", tool, got, want)
		}
	}
}

// TestDecide_SkipPermissions covers --dangerously-skip-permissions, which wins
// over every rule and every mode.
func TestDecide_SkipPermissions(t *testing.T) {
	p := Policy{
		Mode: ModePlan, SkipPermissions: true, Strict: true,
		Deny: []Rule{{Tool: "run_command"}},
	}
	if got := p.Decide(Request{Tool: "run_command", Target: "rm -rf /"}); got != Allow {
		t.Errorf("skip-permissions verdict = %v, want allow", got)
	}
}

// TestDecide_DenyBeatsAllow pins the rule order: an explicit deny wins.
func TestDecide_DenyBeatsAllow(t *testing.T) {
	p := Policy{
		Deny:  []Rule{{Tool: "run_command", Pattern: "rm"}},
		Allow: []Rule{{Tool: "run_command"}},
	}
	if got := p.Decide(Request{Tool: "run_command", Target: "rm -rf /"}); got != Deny {
		t.Errorf("verdict = %v, want deny", got)
	}
	if got := p.Decide(Request{Tool: "run_command", Target: "ls"}); got != Allow {
		t.Errorf("verdict = %v, want allow", got)
	}
}

// TestDecide_AllowRuleMatchesTargetPrefix covers the rule description
// "Matches the file or everything under the directory".
func TestDecide_AllowRuleMatchesTargetPrefix(t *testing.T) {
	p := Policy{Strict: true, Allow: []Rule{{Tool: "view_file", Pattern: "src/"}}}
	if got := p.Decide(Request{Tool: "view_file", Target: "src/main.go"}); got != Allow {
		t.Errorf("in-prefix verdict = %v, want allow", got)
	}
	if got := p.Decide(Request{Tool: "view_file", Target: "docs/x.md"}); got != Ask {
		t.Errorf("out-of-prefix verdict = %v, want ask", got)
	}
}

// TestDecide_ModeRules covers the two mode banners' claims: accept-edits
// auto-approves file edits, auto also covers commands.
func TestDecide_ModeRules(t *testing.T) {
	edit := Request{Tool: "write_to_file", Target: "a.txt"}
	cmd := Request{Tool: "run_command", Target: "date"}

	if got := (Policy{Mode: ModeAcceptEdits, Strict: true}).Decide(edit); got != Allow {
		t.Errorf("accept-edits: file edit = %v, want allow", got)
	}
	if got := (Policy{Mode: ModeAcceptEdits, Strict: true}).Decide(cmd); got != Ask {
		t.Errorf("accept-edits: command = %v, want ask", got)
	}
	if got := (Policy{Mode: ModeAuto, Strict: true}).Decide(edit); got != Allow {
		t.Errorf("auto: file edit = %v, want allow", got)
	}
	if got := (Policy{Mode: ModeAuto, Strict: true}).Decide(cmd); got != Allow {
		t.Errorf("auto: command = %v, want allow", got)
	}
	if got := (Policy{Mode: ModePlan, Strict: true}).Decide(cmd); got != Ask {
		t.Errorf("plan: command = %v, want ask", got)
	}
	if got := (Policy{Mode: ModePlan, Strict: true}).Decide(Request{Tool: "view_file"}); got != Allow {
		t.Errorf("plan: read = %v, want allow", got)
	}
}

// TestDecide_DefaultIsAllow is the clone's documented divergence: with no rule
// and no strict flag, everything is allowed so `--print` keeps working out of
// the box. Strict restores the reference default.
func TestDecide_DefaultIsAllow(t *testing.T) {
	if got := (Policy{}).Decide(Request{Tool: "run_command", Target: "date"}); got != Allow {
		t.Errorf("default verdict = %v, want allow (clone divergence)", got)
	}
	if got := (Policy{Strict: true}).Decide(Request{Tool: "run_command", Target: "date"}); got != Ask {
		t.Errorf("strict verdict = %v, want ask (reference default)", got)
	}
}

func TestStrictFromEnv(t *testing.T) {
	for _, s := range []string{"strict", "STRICT", " strict "} {
		if !StrictFromEnv(s) {
			t.Errorf("StrictFromEnv(%q) = false", s)
		}
	}
	for _, s := range []string{"", "allow", "0"} {
		if StrictFromEnv(s) {
			t.Errorf("StrictFromEnv(%q) = true", s)
		}
	}
}

// TestRecoveredStrings pins the verbatim reference text. Every literal here
// was read out of the agy 1.3.6 string table.
func TestRecoveredStrings(t *testing.T) {
	if got, want := HeadlessNotice(2),
		"the 2 tool(s) required approval that headless mode cannot prompt for, "+
			"so they were auto-denied. Settings allow-rules do not apply; re-run with "+
			"--dangerously-skip-permissions to auto-approve all tools."; got != want {
		t.Errorf("HeadlessNotice:\n got %q\nwant %q", got, want)
	}
	if got, want := DeniedToolResult("run_command", "date"),
		"Permission prompt for action 'run_command' on target 'date' was denied. "+
			"Do not attempt to circumvent this denial by rephrasing the command, using alternative "+
			"tools/scripts (e.g. python, sh, curl), or accessing the same target resource. "+
			"Proceed without performing this action."; got != want {
		t.Errorf("DeniedToolResult:\n got %q\nwant %q", got, want)
	}
	if !strings.Contains(HeadlessDenialNotice("view_file", "read", "src/a.go", "it", `{"tool":"view_file"}`),
		"required the read src/a.go that headless mode cannot prompt for") {
		t.Error("HeadlessDenialNotice lost its recovered shape")
	}
	if SkipPermissionsNotice != "Print mode: --dangerously-skip-permissions set, auto-approving all tool permissions" {
		t.Error("SkipPermissionsNotice changed")
	}
	if AcceptEditsBanner != "Accept-edits mode: file edits auto-approved (shift+tab to cycle)" {
		t.Error("AcceptEditsBanner changed")
	}
	if AutoBanner != "Auto mode: edits & safe commands auto-approved (shift+tab to cycle)" {
		t.Error("AutoBanner changed")
	}
	if !strings.HasSuffix(NoToolCallNudgeHeadless, "Remember that the user cannot answer any questions.") {
		t.Error("NoToolCallNudgeHeadless lost its headless sentence")
	}
}

func TestModeBanner(t *testing.T) {
	if ModeBanner(ModeAcceptEdits) != AcceptEditsBanner {
		t.Error("accept-edits banner")
	}
	if ModeBanner(ModeAuto) != AutoBanner {
		t.Error("auto banner")
	}
	// Cycle 104: `Plan mode: research & plan only (shift+tab to cycle)` is
	// recovered from the 1.3.6 string table, so plan does have a banner; only
	// the default mode has none.
	if ModeBanner(ModePlan) != PlanBanner {
		t.Error("plan banner")
	}
	if ModeBanner("") != "" {
		t.Error("default should have no banner")
	}
}

// TestNextMode pins the Shift+Tab order (cycle 254: Build → Default →
// Auto → Plan → Build). accept-edits is the cycle's Build alias;
// the empty string is Default. The hint text names three modes
// (default, accept-edits, plan) and was not updated when Auto shipped
// in 1.3.5; the cycle keeps the 1.3.5 changelog's "Shift+Tab to
// cycle modes" as the authority for the order, with this cycle's
// choice documented in the cycle-254 deliverable's 硬约束 section.
func TestNextMode(t *testing.T) {
	want := map[string]string{
		"":               ModeAuto,
		ModeAcceptEdits:  "",
		ModeAuto:         ModePlan,
		ModePlan:         ModeAcceptEdits,
		"something-else": ModeAcceptEdits,
	}
	for cur, next := range want {
		if got := NextMode(cur); got != next {
			t.Errorf("NextMode(%q) = %q, want %q", cur, got, next)
		}
	}
	// A full lap returns to the starting mode.
	cur := ""
	for i := 0; i < len(CycleModes); i++ {
		cur = NextMode(cur)
	}
	if cur != "" {
		t.Errorf("after %d steps the cycle ended at %q, want default", len(CycleModes), cur)
	}
}

func TestRuleMatches_MultilineAndRegex(t *testing.T) {
	// 1. Multiline script matching with \r\n vs \n
	multilineCmd := "python -c '\r\nimport os\r\nprint(os.getcwd())\r\n'"
	rule := Rule{
		Tool:    "run_command",
		Pattern: "python -c '\nimport os\nprint(os.getcwd())\n'",
	}
	if !rule.matches(Request{Tool: "run_command", Target: multilineCmd}) {
		t.Errorf("rule failed to match multiline script with CRLF normalization")
	}

	// 2. Strict prefix on multiline script
	prefixRule := Rule{
		Tool:    "run_command",
		Pattern: "python -c",
	}
	if !prefixRule.matches(Request{Tool: "run_command", Target: multilineCmd}) {
		t.Errorf("rule failed to match prefix of multiline script")
	}

	// 3. Opt-in regex matching
	regexRule := Rule{
		Tool:    "run_command",
		Pattern: "regex:^git (status|diff|log)",
	}
	if !regexRule.matches(Request{Tool: "run_command", Target: "git status -s"}) {
		t.Errorf("regex rule failed to match valid target")
	}
	if regexRule.matches(Request{Tool: "run_command", Target: "git push origin main"}) {
		t.Errorf("regex rule incorrectly matched non-matching target")
	}

	// 4. Wildcard pattern
	wildcardRule := Rule{
		Tool:    "run_command",
		Pattern: "*",
	}
	if !wildcardRule.matches(Request{Tool: "run_command", Target: "echo anything"}) {
		t.Errorf("wildcard rule should match any target")
	}
}

func TestSuggestAllowAlwaysPattern(t *testing.T) {
	// Script runner: npm run test -> npm run test
	if got := SuggestAllowAlwaysPattern("npm run test:unit --watch"); got != "npm run test:unit" {
		t.Errorf("npm run suggestion = %q, want 'npm run test:unit'", got)
	}
	// yarn dev -> yarn dev
	if got := SuggestAllowAlwaysPattern("yarn dev --port 3000"); got != "yarn dev" {
		t.Errorf("yarn suggestion = %q, want 'yarn dev'", got)
	}
	// Inline python -c
	pyCmd := "python -c 'import sys; print(sys.version)'"
	if got := SuggestAllowAlwaysPattern(pyCmd); got != pyCmd {
		t.Errorf("python -c suggestion = %q, want full command %q", got, pyCmd)
	}
	// Multi-line python script
	pyMulti := "python -c '\nimport os\nprint(os.name)\n'"
	if got := SuggestAllowAlwaysPattern(pyMulti); got != pyMulti {
		t.Errorf("multiline python suggestion = %q, want %q", got, pyMulti)
	}
	// Chained compound command
	chained := "git fetch origin && git rebase origin/main"
	if got := SuggestAllowAlwaysPattern(chained); got != chained {
		t.Errorf("chained command suggestion = %q, want %q", got, chained)
	}
}
