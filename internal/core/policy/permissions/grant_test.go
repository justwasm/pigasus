package permissions

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// TestParseRuleString_GrantGrammar pins the reference's grant grammar —
// `action(target)` — and the two legacy shapes the clone still reads.
//
// The grammar is:
//
//	grant  := action "(" target ")"
//	action := command | read_file | write_file | read_url | execute_url | mcp
//	target := the bytes between the first "(" and the last ")", byte for byte
//
// The action vocabulary comes from the binary's own format regex
// `^(command|read_file|write_file|read_url|mcp|execute_url|unsandboxed)\s*\(.*\)$`
// 【一手】 agy 1.4.11, and `unsandboxed` is the retired action the reference
// rejects.
func TestParseRuleString_GrantGrammar(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want Rule
		ok   bool
	}{
		{"command", "command(git)", Rule{Action: "command", Pattern: "git"}, true},
		{"read_file", "read_file(/var/log/app)", Rule{Action: "read_file", Pattern: "/var/log/app"}, true},
		{"write_file", "write_file(src/)", Rule{Action: "write_file", Pattern: "src/"}, true},
		{"read_url", "read_url(google.com)", Rule{Action: "read_url", Pattern: "google.com"}, true},
		{"execute_url", "execute_url(aws.amazon.com)", Rule{Action: "execute_url", Pattern: "aws.amazon.com"}, true},
		{"mcp", "mcp(linter/*)", Rule{Action: "mcp", Pattern: "linter/*"}, true},
		{"empty target", "command()", Rule{Action: "command", Pattern: ""}, true},
		{"wildcard target", "command(*)", Rule{Action: "command", Pattern: "*"}, true},
		{"target with spaces", "command(git status)", Rule{Action: "command", Pattern: "git status"}, true},
		{"regex target", `command(regex:npm run (build|lint))`, Rule{Action: "command", Pattern: "regex:npm run (build|lint)"}, true},
		{"missing closing paren", "command(git", Rule{}, false},
		{"no opening paren", "command git)", Rule{Pattern: "command git)"}, true},
		{"space before the paren", "command (git)", Rule{Action: "command", Pattern: "git"}, true},
		{"unsandboxed is retired", "unsandboxed(git push)", Rule{}, false},
		{"unknown action", "foo(bar)", Rule{}, false},
		{"read is not an action", "read(foo)", Rule{}, false},
		{"legacy tool:pattern", "run_command:go test", Rule{Tool: "run_command", Pattern: "go test"}, true},
		{"legacy tool:pattern with parens", "run_command:echo (hi)", Rule{Tool: "run_command", Pattern: "echo (hi)"}, true},
		{"legacy bare pattern", "view_file", Rule{Pattern: "view_file"}, true},
		{"empty string", "", Rule{}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := parseRuleString(tc.in)
			if ok != tc.ok {
				t.Fatalf("parseRuleString(%q) ok = %v, want %v (rule %+v)", tc.in, ok, tc.ok, got)
			}
			if got != tc.want {
				t.Errorf("parseRuleString(%q) = %+v, want %+v", tc.in, got, tc.want)
			}
		})
	}
}

// TestParseRule_JSONShapes pins the three JSON entry shapes: the grant
// string, the legacy object form and the entries that are skipped.
func TestParseRule_JSONShapes(t *testing.T) {
	cases := []struct {
		in   string
		want Rule
		ok   bool
	}{
		{`"command(git status)"`, Rule{Action: "command", Pattern: "git status"}, true},
		{`{"tool": "view_file", "pattern": "src/"}`, Rule{Tool: "view_file", Pattern: "src/"}, true},
		{`"run_command:go test"`, Rule{Tool: "run_command", Pattern: "go test"}, true},
		{`null`, Rule{}, false},
		{`42`, Rule{}, false},
		{`{"unrelated": true}`, Rule{}, false},
		{`"unsandboxed(git)"`, Rule{}, false},
	}
	for _, tc := range cases {
		got, ok := parseRule(json.RawMessage(tc.in))
		if ok != tc.ok {
			t.Errorf("parseRule(%s) ok = %v, want %v (rule %+v)", tc.in, ok, tc.ok, got)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("parseRule(%s) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}

// TestGrantMatchesCommandTool pins the mapping: a rule written the way the
// documentation writes it reaches the clone's own tool names.
//
// The policy is strict, so a call no rule covers asks — the clone's own
// default is to allow.
func TestGrantMatchesCommandTool(t *testing.T) {
	p := Policy{Strict: true, Allow: []Rule{{Action: "command", Pattern: "git status"}}}
	if got := p.Decide(Request{Tool: "run_command", Target: "git status --short"}); got != Allow {
		t.Errorf("git status --short = %v, want allow", got)
	}
	if got := p.Decide(Request{Tool: "run_command", Target: "git push"}); got != Ask {
		t.Errorf("git push = %v, want ask (no rule covers it)", got)
	}
	// The rule reaches every command tool, not just run_command.
	if got := p.Decide(Request{Tool: "command_status", Target: "git status"}); got != Allow {
		t.Errorf("command_status = %v, want allow", got)
	}
	// …and no other tool.
	if got := p.Decide(Request{Tool: "view_file", Target: "git status"}); got != Ask {
		t.Errorf("view_file = %v, want ask (a command rule covers no read)", got)
	}
}

// TestGrantMatchesReadTool pins the read half of the mapping: the
// reference's read_file action reaches the clone's read tools.
func TestGrantMatchesReadTool(t *testing.T) {
	p := Policy{Strict: true, Allow: []Rule{{Action: "read_file", Pattern: "/abs/path"}}}
	if got := p.Decide(Request{Tool: "view_file", Target: "/abs/path/file.go"}); got != Allow {
		t.Errorf("view_file = %v, want allow", got)
	}
	if got := p.Decide(Request{Tool: "read_file", Target: "/abs/path/file.go"}); got != Allow {
		t.Errorf("read_file = %v, want allow", got)
	}
	if got := p.Decide(Request{Tool: "view_file", Target: "/other/file.go"}); got != Ask {
		t.Errorf("a path outside the rule = %v, want ask", got)
	}
	// A write tool is not a read tool.
	if got := p.Decide(Request{Tool: "write_to_file", Target: "/abs/path/file.go"}); got != Ask {
		t.Errorf("a write = %v, want ask (a read rule covers no write)", got)
	}
}

// TestActionToolsCoversEveryAction pins the mapping's shape: every live
// action names at least one tool, and the retired action names none.
func TestActionToolsCoversEveryAction(t *testing.T) {
	for _, action := range []string{
		ActionCommand, ActionReadFile, ActionWriteFile,
		ActionReadURL, ActionExecuteURL, ActionMCP,
	} {
		if len(grantActionTools[action]) == 0 {
			t.Errorf("action %q covers no tool", action)
		}
		if !ValidGrantAction(action) {
			t.Errorf("ValidGrantAction(%q) = false", action)
		}
	}
	if ValidGrantAction(ActionUnsandboxed) {
		t.Error("unsandboxed must not be a valid action")
	}
	if len(grantActionTools[ActionUnsandboxed]) != 0 {
		t.Error("unsandboxed must cover no tool")
	}
}

// TestValidateGrantActions pins the panel's validation: the six live
// actions are accepted, the retired one is rejected with the reference's
// own message.
func TestValidateGrantActions(t *testing.T) {
	for _, rule := range []string{
		"command(git)", "read_file(/a)", "write_file(/a)", "read_url(a.com)",
		"execute_url(a.com)", "mcp(server/tool)",
	} {
		if msg := ValidateGrant(rule); msg != "" {
			t.Errorf("ValidateGrant(%q) = %q, want no message", rule, msg)
		}
	}
	if msg := ValidateGrant("unsandboxed(git push)"); msg != UnsandboxedRejectedMessage {
		t.Errorf("ValidateGrant(unsandboxed) = %q, want %q", msg, UnsandboxedRejectedMessage)
	}
	if UnsandboxedRejectedMessage != "unsandboxed is no longer a valid rule, use command() instead" {
		t.Errorf("the rejection message changed: %q", UnsandboxedRejectedMessage)
	}
}

// TestPrecedenceDenyAskAllow pins the reference's rule precedence: a deny
// rule wins over an ask rule, and an ask rule wins over an allow rule.
func TestPrecedenceDenyAskAllow(t *testing.T) {
	p := Policy{
		Allow: []Rule{{Action: "command", Pattern: "git"}},
		Ask:   []Rule{{Action: "command", Pattern: "git"}},
		Deny:  []Rule{{Action: "command", Pattern: "git"}},
	}
	req := Request{Tool: "run_command", Target: "git status"}
	if got := p.Decide(req); got != Deny {
		t.Errorf("deny + ask + allow = %v, want deny", got)
	}
	// Without the deny rule the ask rule wins over the allow rule.
	p.Deny = nil
	if got := p.Decide(req); got != Ask {
		t.Errorf("ask + allow = %v, want ask", got)
	}
	// Without the ask rule the allow rule permits.
	p.Ask = nil
	if got := p.Decide(req); got != Allow {
		t.Errorf("allow = %v, want allow", got)
	}
}

// TestAskBeatsModeAutoApproval pins that an ask rule is not overridden by
// the mode's auto-approval: the mode's rules come after the lists.
func TestAskBeatsModeAutoApproval(t *testing.T) {
	p := Policy{
		Mode: ModeAuto,
		Ask:  []Rule{{Action: "command", Pattern: "git"}},
	}
	if got := p.Decide(Request{Tool: "run_command", Target: "git status"}); got != Ask {
		t.Errorf("auto mode + ask rule = %v, want ask", got)
	}
}

// TestToolPermissionPolicies pins the four documented values of the
// `toolPermission` setting.
func TestToolPermissionPolicies(t *testing.T) {
	edit := Request{Tool: "write_to_file", Target: "a.txt"}
	cmd := Request{Tool: "run_command", Target: "date"}
	read := Request{Tool: "view_file", Target: "src/a.go"}
	web := Request{Tool: "read_url_content", Target: "https://example.com"}
	mcp := Request{Tool: "call_mcp_tool", Target: "server/tool"}

	// always-proceed: nothing asks.
	p := Policy{ToolPermission: ToolPermissionAlwaysProceed}
	for _, req := range []Request{edit, cmd, read, web, mcp} {
		if got := p.Decide(req); got != Allow {
			t.Errorf("always-proceed %s = %v, want allow", req.Tool, got)
		}
	}

	// strict: everything asks.
	p = Policy{ToolPermission: ToolPermissionStrict}
	for _, req := range []Request{edit, cmd, read, web, mcp} {
		if got := p.Decide(req); got != Ask {
			t.Errorf("strict %s = %v, want ask", req.Tool, got)
		}
	}

	// request-review: the write, the command, the web call and the MCP
	// call ask; a plain read does not.
	p = Policy{ToolPermission: ToolPermissionRequestReview}
	for _, req := range []Request{edit, cmd, web, mcp} {
		if got := p.Decide(req); got != Ask {
			t.Errorf("request-review %s = %v, want ask", req.Tool, got)
		}
	}
	if got := p.Decide(read); got != Allow {
		t.Errorf("request-review read = %v, want allow", got)
	}

	// proceed-in-sandbox: a command runs without prompting while the
	// sandbox is on…
	p = Policy{ToolPermission: ToolPermissionProceedInSandbox, SandboxEnabled: true}
	if got := p.Decide(cmd); got != Allow {
		t.Errorf("proceed-in-sandbox command = %v, want allow", got)
	}
	// …and it behaves like request-review without the sandbox.
	p = Policy{ToolPermission: ToolPermissionProceedInSandbox}
	if got := p.Decide(cmd); got != Ask {
		t.Errorf("proceed-in-sandbox without sandbox = %v, want ask", got)
	}
	if got := p.Decide(read); got != Allow {
		t.Errorf("proceed-in-sandbox read = %v, want allow", got)
	}

	// The absent setting keeps the clone's allow-by-default divergence.
	p = Policy{}
	if got := p.Decide(cmd); got != Allow {
		t.Errorf("absent toolPermission = %v, want allow (clone divergence)", got)
	}
}

// TestToolPermissionDoesNotOverrideTheRuleLists pins that the setting
// steers the calls no rule decided: an explicit allow rule still permits.
func TestToolPermissionDoesNotOverrideTheRuleLists(t *testing.T) {
	p := Policy{
		ToolPermission: ToolPermissionStrict,
		Allow:          []Rule{{Action: "command", Pattern: "date"}},
	}
	if got := p.Decide(Request{Tool: "run_command", Target: "date"}); got != Allow {
		t.Errorf("allow rule under strict = %v, want allow", got)
	}
	// A deny rule still blocks.
	p.Deny = []Rule{{Action: "command", Pattern: "rm"}}
	if got := p.Decide(Request{Tool: "run_command", Target: "rm -rf /"}); got != Deny {
		t.Errorf("deny rule under strict = %v, want deny", got)
	}
}

// TestLoadRulesReadsTheAskList pins the third list: the reference's
// `PermissionGrantsConfig` carries allow, deny *and* ask, and the settings
// file carries all three.
func TestLoadRulesReadsTheAskList(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	body := `{
	  "permissions": {
	    "allow": ["command(git status)", "read_url(github.com)"],
	    "ask":   ["command(git push)"],
	    "deny":  ["write_file(.env)"]
	  }
	}`
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := LoadRules(path)
	if err != nil {
		t.Fatalf("LoadRules: %v", err)
	}
	wantAsk := []Rule{{Action: "command", Pattern: "git push"}}
	if len(s.Ask) != 1 || s.Ask[0] != wantAsk[0] {
		t.Fatalf("ask = %+v, want %+v", s.Ask, wantAsk)
	}
	if len(s.Allow) != 2 || s.Allow[0] != (Rule{Action: "command", Pattern: "git status"}) {
		t.Errorf("allow = %+v", s.Allow)
	}
	if len(s.Deny) != 1 || s.Deny[0] != (Rule{Action: "write_file", Pattern: ".env"}) {
		t.Errorf("deny = %+v", s.Deny)
	}
	// The round trip: the ask list reaches the policy and steers it.
	p := Policy{Ask: s.Ask}
	if got := p.Decide(Request{Tool: "run_command", Target: "git push"}); got != Ask {
		t.Errorf("ask list verdict = %v, want ask", got)
	}
}
