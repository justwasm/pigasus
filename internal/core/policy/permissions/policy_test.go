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

func TestReviewerModelForBase_HydrogenUsesMedium(t *testing.T) {
	if got := ReviewerModelForBase("gemini-4-hydrogen-high"); got != "gemini-4-hydrogen-medium" {
		t.Errorf("ReviewerModelForBase(hydrogen-high) = %q, want gemini-4-hydrogen-medium", got)
	}
	if got := ReviewerModelForBase("gemini-3.8-flash-high"); got != "gemini-3.8-flash-low" {
		t.Errorf("ReviewerModelForBase(flash-high) = %q, want gemini-3.8-flash-low", got)
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
	// Auto names itself in the composer. AutoBanner is agy 1.5.18's own
	// string, present verbatim in the binary; a bare ">" in a capture means
	// the binary never entered auto (the terminal sandbox was unavailable),
	// not that auto has no banner.
	if got := ModeBanner(ModeAuto); got != AutoBanner {
		t.Errorf("auto banner = %q, want %q", got, AutoBanner)
	}
	if ModeBanner(ModePlan) != PlanBanner {
		t.Error("plan banner")
	}
	if ModeBanner("") != "" {
		t.Error("default should have no banner")
	}
}

// TestModeBanner_AutoIsNotTheDefault pins that auto and the stored-empty
// default are two modes, not one.
//
// They are separate cycle stops with different behaviour (default pauses for
// diff review, auto approves edits and commands) and different storage
// ("" against "auto"). Only auto names itself in the composer, because the
// empty value is the absence of a choice rather than a word.
func TestModeBanner_AutoIsNotTheDefault(t *testing.T) {
	if ModeBanner(ModeAuto) == ModeBanner("") {
		t.Errorf("auto banner = %q equals the default's; auto is a mode, the empty value is no choice",
			ModeBanner(ModeAuto))
	}
	if ModeBanner(ModeAuto) != AutoBanner {
		t.Errorf("auto banner = %q, want %q", ModeBanner(ModeAuto), AutoBanner)
	}
}

// TestModeSpecs_EveryModeIsComplete — a mode is one row of facts, and every
// place that names a mode must agree with that row.
//
// This is the invariant the type exists for. Before it, a mode's banner, its
// cycle position, its colour and its auto-approval rules were four separate
// tables keyed by the same word with nothing tying them together: a mode could
// be added to one and silently missing from another. Here every mode is
// walked through each consumer and each consumer must know it.
func TestModeSpecs_EveryModeIsComplete(t *testing.T) {
	for _, mode := range CycleModes {
		t.Run(string(mode), func(t *testing.T) {
			if !mode.Valid() {
				t.Fatalf("%q is in CycleModes but has no modeSpecs row", mode)
			}
			// The flag's accepted set and the cycle's stops are different
			// questions — the stored-empty default is a stop but not a
			// flag value — but every non-default stop must be an accepted
			// flag value.
			if mode != ModeDefault && !ValidMode(string(mode)) {
				t.Errorf("%q is a cycle stop but --mode rejects it", mode)
			}
			// Every mode after the default names itself in the composer,
			// and the default names nothing. A mode that draws no banner
			// is a stop the user lands on with no indication.
			if hasBanner := mode.Banner() != ""; hasBanner != (mode != ModeDefault) {
				t.Errorf("%q banner = %q: only the default may be anonymous", mode, mode.Banner())
			}
		})
	}
}

// TestModeSpecs_ApprovalsMatchTheDecidePath — the table's approval list and
// the policy's actual verdict agree, exactly.
//
// modeSpecs states which classes a mode approves; decideByMode implements it.
// The two are separate pieces of code and could drift, so every (mode, class)
// pair is walked through the real Decide path and compared.
//
// Strict mode is what makes this a real assertion. The clone's default is
// Allow, so under the normal policy an unapproved call and an approved one
// both come back Allow and the comparison says nothing. Strict turns an
// otherwise-undecided call into Ask, leaving the mode's own contribution as
// the only difference between the two outcomes.
func TestModeSpecs_ApprovalsMatchTheDecidePath(t *testing.T) {
	// One call per class, chosen so that no rule list, no toolPermission
	// setting and no protected path interferes: the mode is the only thing
	// left to decide.
	calls := map[Class]struct {
		tool   string
		target string
	}{
		ClassRead:    {"read_file", "/workspace/a.go"},
		ClassEdit:    {"write_file", "/workspace/a.go"},
		ClassCommand: {"run_command", "echo hi"},
	}
	for _, mode := range CycleModes {
		for cls, call := range calls {
			t.Run(string(mode)+"/"+cls.String(), func(t *testing.T) {
				p := Policy{
					Mode:           mode,
					Strict:         true,
					WorkspaceRoots: []string{"/workspace"},
				}
				got := p.DecideNames(call.target, call.tool)
				if mode.Approves(cls) {
					if got != Allow {
						t.Errorf("%q approves %v per modeSpecs, but DecideNames = %v", mode, cls, got)
					}
					return
				}
				// Not approved by the mode. Auto's ClassEdit is the one
				// conditional approval, handled outside the table, so it
				// has its own test below.
				if mode == ModeAuto && cls == ClassEdit {
					return
				}
				if got == Allow {
					t.Errorf("%q does not approve %v per modeSpecs, but DecideNames = %v", mode, cls, got)
				}
			})
		}
	}
}

// TestDecideByMode_AutoEditsAreConditional — auto's edit approval is not a
// row in modeSpecs, because unlike every other entry there it is conditional.
//
// The decision depends on the workspace and on whether the call is one the
// reference approves outside it (ShouldAutoApproveOutsideWorkspaceFileEdit).
// A category in the table means "approved, full stop", so auto's ClassEdit
// would be a claim the code does not make. The two halves are checked through
// the real Decide path instead: an edit inside the workspace is approved, and
// a call the outside-workspace rule does not cover asks.
func TestDecideByMode_AutoEditsAreConditional(t *testing.T) {
	p := Policy{Mode: ModeAuto, Strict: true, WorkspaceRoots: []string{"/workspace"}}

	if got := p.DecideNames("/workspace/a.go", "write_file"); got != Allow {
		t.Errorf("auto edit inside the workspace = %v, want Allow", got)
	}
	// A call the outside-workspace rule does not auto-approve must ask,
	// which is what makes the rule conditional rather than blanket.
	const outsideTarget = "/elsewhere/a.go"
	if ShouldAutoApproveOutsideWorkspaceFileEdit(outsideTarget, "write_file", nil) {
		t.Skip("this fixture is covered by the outside-workspace rule; the condition cannot be shown")
	}
	if got := p.DecideNames(outsideTarget, "write_file"); got != Ask {
		t.Errorf("auto edit outside the workspace = %v, want Ask", got)
	}

	// The table must not claim it, or the two would disagree.
	if ModeAuto.Approves(ClassEdit) {
		t.Error("modeSpecs lists ClassEdit as an unconditional auto approval, but the rule is conditional")
	}
}

// TestNextMode pins the Shift+Tab order against the reference.
//
// official-docs/cli/modes.md states the sequence outright:
//
//	`default` > `accept-edits` > `auto` > `plan` > `default`
//
// Four stops, with default and auto as separate modes. The earlier
// three-entry order folded "" onto auto's slot; see CycleModes for why that
// reading was wrong.
func TestNextMode(t *testing.T) {
	want := map[AgentMode]AgentMode{
		ModeDefault:     ModeAcceptEdits,
		ModeAcceptEdits: ModeAuto,
		ModeAuto:        ModePlan,
		ModePlan:        ModeDefault,
		// An unrecognised mode restarts the cycle rather than landing on
		// "" — "" is a real state now, not the absence of one.
		"something-else": ModeDefault,
	}
	for cur, next := range want {
		if got := NextMode(cur); got != next {
			t.Errorf("NextMode(%q) = %q, want %q", cur, got, next)
		}
	}
	// A full lap returns to the starting mode.
	for _, start := range CycleModes {
		cur := start
		for i := 0; i < len(CycleModes); i++ {
			cur = NextMode(cur)
		}
		if cur != start {
			t.Errorf("after %d steps from %q the cycle ended at %q", len(CycleModes), start, cur)
		}
	}
}

func TestNextRemoteAgentMode(t *testing.T) {
	if got := NextRemoteAgentMode(""); got != ModeAcceptEdits {
		t.Errorf("NextRemoteAgentMode(\"\") = %q, want %q", got, ModeAcceptEdits)
	}
	if got := NextRemoteAgentMode(ModeAcceptEdits); got != ModePlan {
		t.Errorf("NextRemoteAgentMode(ModeAcceptEdits) = %q, want %q", got, ModePlan)
	}
	if got := NextRemoteAgentMode(ModePlan); got != "" {
		t.Errorf("NextRemoteAgentMode(ModePlan) = %q, want \"\"", got)
	}
	if got := NextRemoteAgentMode(ModeAuto); got != ModeAcceptEdits {
		t.Errorf("NextRemoteAgentMode(ModeAuto) = %q, want %q", got, ModeAcceptEdits)
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

// TestAutoModeNonWorkspaceAccess pins AGY 1.5.14: Auto mode allows reading files outside
// the workspace without asking for approval. Writing or editing outside the workspace still asks.
func TestAutoModeNonWorkspaceAccess(t *testing.T) {
	ws := "/Users/example/project"

	pol := Policy{
		Mode:           ModeAuto,
		WorkspaceRoots: []string{ws},
	}
	// Reads outside workspace are allowed without prompting (AGY 1.5.14)
	if got := pol.Decide(Request{Tool: "read_file", Target: "/etc/hosts"}); got != Allow {
		t.Errorf("pol.Decide(read /etc/hosts) = %v, want Allow", got)
	}
	// Reads inside workspace allowed
	if got := pol.Decide(Request{Tool: "read_file", Target: "/Users/example/project/main.go"}); got != Allow {
		t.Errorf("pol.Decide(read main.go) = %v, want Allow", got)
	}

	// Writes/edits outside workspace still ask for approval
	if got := pol.Decide(Request{Tool: "write_file", Target: "/etc/hosts"}); got != Ask {
		t.Errorf("pol.Decide(write /etc/hosts) = %v, want Ask", got)
	}
	// Writes/edits inside workspace allowed
	if got := pol.Decide(Request{Tool: "write_file", Target: "/Users/example/project/main.go"}); got != Allow {
		t.Errorf("pol.Decide(write main.go) = %v, want Allow", got)
	}
}

func TestForbiddenWindowsPaths(t *testing.T) {
	pol := Policy{
		Mode:           ModeAuto,
		WorkspaceRoots: []string{"/Users/example/project"},
	}
	for _, target := range []string{
		`\\server\share\file.txt`,
		`//server/share/file.txt`,
		`\??\C:\Windows`,
		`/??/C:/Windows`,
		`\\.\COM1`,
	} {
		if got := pol.Decide(Request{Tool: "read_file", Target: target}); got != Deny {
			t.Errorf("pol.Decide(%q) = %v, want Deny", target, got)
		}
	}
}
