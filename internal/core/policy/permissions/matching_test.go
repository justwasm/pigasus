package permissions

import "testing"

// TestMatchCommand pins the command action's matching rule.
//
// The documentation's own words: "Matches by exact word or token prefix
// literally by default. To match by regular expression, start the target
// with the `regex:` prefix (each whitespace-separated token is evaluated
// as an anchored regular expression `^(?:pattern)$`)".
//
// The rule the clone implements: the granted target is split into
// whitespace-separated tokens, the command is split the same way, and the
// granted tokens must match the command's *leading* tokens one for one,
// each as a full word.
func TestMatchCommand(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		target  string
		want    bool
	}{
		{"exact word", "git", "git", true},
		{"word prefix", "git", "git status", true},
		{"compound chain", "git", "git status && git log", true},
		{"not a whole word", "git", "gitk status", false},
		{"word inside another word", "git", "legit", false},
		{"two tokens", "git status", "git status --short", true},
		{"two tokens, other subcommand", "git status", "git push", false},
		{"two tokens, longer word", "git status", "git statuses", false},
		{"destructive pair", "rm -rf", "rm -rf /", true},
		{"destructive pair, shorter flag", "rm -rf", "rm -r /", false},
		{"the grant leads the command", "git", "sudo git status", false},
		{"empty command", "git", "", false},
		{"newline separated", "git status", "git\nstatus", true},
		// The documentation's own example.
		{"regex target", "regex:npm run (build.*)", "npm run build", true},
		{"regex target, other script", "regex:npm run (build.*)", "npm run lint", false},
		{"regex target, other command", "regex:npm run (build.*)", "npm test", false},
		{"regex target, anchored", "regex:npm run (build.*)", "npm run build:prod", true},
		// A per-token regex prefix switches that token alone.
		{"per-token regex", "npm regex:run.*", "npm run build", true},
		{"per-token regex, no match", "npm regex:run.*", "npm test", false},
		{"literal token next to a regex token", "npm regex:run.+", "npm run", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchCommand(tc.pattern, tc.target); got != tc.want {
				t.Errorf("matchCommand(%q, %q) = %v, want %v", tc.pattern, tc.target, got, tc.want)
			}
		})
	}
}

// TestMatchPath pins the file actions' matching rule: a path prefix, with
// the Windows normalisation the documentation describes ("stripping drive
// letters (for example, `C:`) and converting all backslashes (`\`) to
// forward slashes (`/`)").
func TestMatchPath(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		target  string
		roots   []string
		want    bool
	}{
		{"under the directory", "/var/log", "/var/log/app.log", nil, true},
		{"the directory itself", "/var/log", "/var/log", nil, true},
		{"trailing separator in the grant", "/var/log/", "/var/log/app.log", nil, true},
		{"a sibling with a longer name", "/var/log", "/var/logical/app.log", nil, false},
		{"outside the directory", "/var/log", "/var/lib/app.log", nil, false},
		{"windows grant, windows target", `C:\Users\me`, `C:\Users\me\file.txt`, nil, true},
		{"windows grant, unix target", `C:\Users\me`, "/Users/me/file.txt", nil, true},
		{"unix grant, windows target", "/Users/me", `C:\Users\me\file.txt`, nil, true},
		{"windows grant, forward-slash target", `C:\Users\me`, "C:/Users/me/file.txt", nil, true},
		// A relative grant, with no workspace root known.
		{"relative grant", "src", "/ws/src/main.go", nil, true},
		{"relative grant, other component", "src", "/ws/srcx/main.go", nil, false},
		{"relative grant, directory", "src/", "/ws/src/main.go", nil, true},
		// A relative grant, resolved against a workspace root.
		{"workspace relative grant", "src", "/ws/src/main.go", []string{"/ws"}, true},
		{"workspace relative grant, outside", "src", "/other/src/main.go", []string{"/ws"}, false},
		{"workspace relative grant, sibling", "src", "/ws/srcx/main.go", []string{"/ws"}, false},
		{"workspace relative grant, root itself", "src", "/ws/src", []string{"/ws"}, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchPath(tc.pattern, tc.target, tc.roots); got != tc.want {
				t.Errorf("matchPath(%q, %q, %v) = %v, want %v",
					tc.pattern, tc.target, tc.roots, got, tc.want)
			}
		})
	}
}

// TestMatchHost pins the URL actions' matching rule: a hostname, with
// the subdomains it covers ("google.com covers mail.google.com"), and
// without the URL's path.
func TestMatchHost(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		target  string
		want    bool
	}{
		{"the host itself", "google.com", "https://google.com", true},
		{"a subdomain", "google.com", "https://mail.google.com/search?q=1", true},
		{"a deeper subdomain", "google.com", "https://a.b.google.com", true},
		{"the path is ignored", "google.com", "https://google.com/a/b/c", true},
		{"a port is ignored", "google.com", "https://google.com:8443/x", true},
		{"the host is case-insensitive", "google.com", "https://GOOGLE.COM/x", true},
		{"a bare host", "google.com", "google.com/path", true},
		{"another host", "google.com", "https://example.com", false},
		{"a host that only ends with the grant", "google.com", "https://notgoogle.com", false},
		{"a host that carries the grant as a suffix", "google.com", "https://google.com.evil.test", false},
		{"the grant is a subdomain", "mail.google.com", "https://mail.google.com/x", true},
		{"the grant is a subdomain, other host", "mail.google.com", "https://www.google.com", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchHost(tc.pattern, tc.target); got != tc.want {
				t.Errorf("matchHost(%q, %q) = %v, want %v", tc.pattern, tc.target, got, tc.want)
			}
		})
	}
}

// TestMatchMCPTarget pins the MCP action's matching rule: an exact
// tool, every tool of a server, or every tool of every server.
func TestMatchMCPTarget(t *testing.T) {
	cases := []struct {
		name    string
		pattern string
		target  string
		want    bool
	}{
		{"the exact tool", "linter/check", "linter/check", true},
		{"another tool of the same server", "linter/check", "linter/fix", false},
		{"another server", "linter/check", "other/check", false},
		{"every tool of a server", "linter/*", "linter/check", true},
		{"every tool of a server, other server", "linter/*", "other/check", false},
		{"every tool of every server", "*", "any/tool", true},
		{"a server alone covers its tools", "linter", "linter/check", true},
		{"a server alone, other server", "linter", "other/check", false},
		{"any server, one tool", "*/check", "linter/check", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := matchMCPTarget(tc.pattern, tc.target); got != tc.want {
				t.Errorf("matchMCPTarget(%q, %q) = %v, want %v",
					tc.pattern, tc.target, got, tc.want)
			}
		})
	}
}

// TestMatchGrantTargetWildcard pins the global wildcard: "Across all
// supported action types (such as `read_file(*)`, `command(*)`, and
// `mcp(*)`), passing the global wildcard `*` matches all targets within
// that entire action namespace."
func TestMatchGrantTargetWildcard(t *testing.T) {
	for _, action := range []string{
		ActionCommand, ActionReadFile, ActionWriteFile,
		ActionReadURL, ActionExecuteURL, ActionMCP,
	} {
		if !matchGrantTarget(action, "*", "anything at all", nil) {
			t.Errorf("action %q: the wildcard did not match", action)
		}
	}
}

// TestGrantActionsUseTheirOwnRule pins that each action is matched by
// its own rule and not by one shared prefix rule: a command grant does
// not reach a path, and a path grant does not reach a command.
func TestGrantActionsUseTheirOwnRule(t *testing.T) {
	cmd := Policy{Strict: true, Allow: []Rule{{Action: ActionCommand, Pattern: "git"}}}
	if got := cmd.Decide(Request{Tool: "run_command", Target: "git status"}); got != Allow {
		t.Errorf("command grant = %v, want allow", got)
	}
	if got := cmd.Decide(Request{Tool: "run_command", Target: "gitk status"}); got != Ask {
		t.Errorf("command grant on a longer word = %v, want ask", got)
	}

	path := Policy{Strict: true, Allow: []Rule{{Action: ActionReadFile, Pattern: "/ws"}}}
	if got := path.Decide(Request{Tool: "view_file", Target: "/ws/a.go"}); got != Allow {
		t.Errorf("read grant = %v, want allow", got)
	}
	if got := path.Decide(Request{Tool: "view_file", Target: "/wsx/a.go"}); got != Ask {
		t.Errorf("read grant on a sibling = %v, want ask", got)
	}

	url := Policy{Strict: true, Allow: []Rule{{Action: ActionReadURL, Pattern: "google.com"}}}
	if got := url.Decide(Request{Tool: "read_url_content", Target: "https://mail.google.com/x"}); got != Allow {
		t.Errorf("url grant = %v, want allow", got)
	}
	if got := url.Decide(Request{Tool: "read_url_content", Target: "https://example.com/x"}); got != Ask {
		t.Errorf("url grant on another host = %v, want ask", got)
	}

	mcp := Policy{Strict: true, Allow: []Rule{{Action: ActionMCP, Pattern: "linter/*"}}}
	if got := mcp.Decide(Request{Tool: "call_mcp_tool", Target: "linter/check"}); got != Allow {
		t.Errorf("mcp grant = %v, want allow", got)
	}
	if got := mcp.Decide(Request{Tool: "call_mcp_tool", Target: "other/check"}); got != Ask {
		t.Errorf("mcp grant on another server = %v, want ask", got)
	}
}

// TestImplicitWriteImpliesRead pins the first implicit rule: "Write
// implies read: Allowing `write_file` on a path automatically grants
// `read_file` on that path."
func TestImplicitWriteImpliesRead(t *testing.T) {
	p := Policy{
		Strict: true,
		Allow:  []Rule{{Action: ActionWriteFile, Pattern: "/ws/src"}},
	}
	if got := p.Decide(Request{Tool: "view_file", Target: "/ws/src/a.go"}); got != Allow {
		t.Errorf("read under a write grant = %v, want allow", got)
	}
	if got := p.Decide(Request{Tool: "read_file", Target: "/ws/src/a.go"}); got != Allow {
		t.Errorf("read under a write grant (declaration spelling) = %v, want allow", got)
	}
	// The rule is one-directional: an allow on a read does not allow a
	// write.
	q := Policy{
		Strict: true,
		Allow:  []Rule{{Action: ActionReadFile, Pattern: "/ws/src"}},
	}
	if got := q.Decide(Request{Tool: "write_to_file", Target: "/ws/src/a.go"}); got != Ask {
		t.Errorf("write under a read grant = %v, want ask", got)
	}
}

// TestImplicitDenyReadImpliesDenyWrite pins the second implicit rule:
// "Deny read implies deny write: Denying `read_file` on a path
// immediately blocks `write_file` on that path."
func TestImplicitDenyReadImpliesDenyWrite(t *testing.T) {
	p := Policy{
		Deny: []Rule{{Action: ActionReadFile, Pattern: "/ws/.env"}},
	}
	if got := p.Decide(Request{Tool: "write_to_file", Target: "/ws/.env"}); got != Deny {
		t.Errorf("write under a read deny = %v, want deny", got)
	}
	if got := p.Decide(Request{Tool: "write_file", Target: "/ws/.env"}); got != Deny {
		t.Errorf("write under a read deny (declaration spelling) = %v, want deny", got)
	}
	// A path the deny does not cover is not denied.
	if got := p.Decide(Request{Tool: "write_to_file", Target: "/ws/other"}); got != Allow {
		t.Errorf("write under a read deny, other path = %v, want allow", got)
	}
	// The rule is one-directional: a deny on a write does not deny a read.
	q := Policy{
		Deny: []Rule{{Action: ActionWriteFile, Pattern: "/ws/.env"}},
	}
	if got := q.Decide(Request{Tool: "view_file", Target: "/ws/.env"}); got != Allow {
		t.Errorf("read under a write deny = %v, want allow", got)
	}
}

// TestImplicitRulesDoNotReachTheAskList pins that the implicit rules
// belong to the two lists the documentation names: an ask rule asks for
// what it names and nothing else.
//
// The policy is not strict, so a call no rule covers falls to the clone's
// allow-by-default — a verdict of Ask would then mean the ask rule
// reached the call.
func TestImplicitRulesDoNotReachTheAskList(t *testing.T) {
	p := Policy{
		Ask: []Rule{{Action: ActionWriteFile, Pattern: "/ws/src"}},
	}
	if got := p.Decide(Request{Tool: "view_file", Target: "/ws/src/a.go"}); got != Allow {
		t.Errorf("read under a write ask rule = %v, want allow", got)
	}
	if got := p.Decide(Request{Tool: "write_to_file", Target: "/ws/src/a.go"}); got != Ask {
		t.Errorf("write under a write ask rule = %v, want ask", got)
	}
}
