// matching.go — the per-action matching rules of the permission grant
// grammar.
//
// The grant grammar itself — `action(target)`, the six live actions, the
// retired `unsandboxed` — is the reference's own and lives in settings.go.
// What this file owns is the second half: how the *target* of a grant is
// matched against a concrete operation.
//
// The official documentation is the source
// (official-docs/permissions.md, "Supported actions and matching rules"):
//
//	read_file / write_file  A path prefix. The path may be absolute or
//	                        relative to a project workspace root, and a
//	                        Windows path is normalised first: the drive
//	                        letter is stripped and the backslashes
//	                        become forward slashes.
//
//	read_url / execute_url  A hostname. A grant covers the host itself
//	                        and its subdomains ("google.com covers
//	                        mail.google.com"), and the URL's path is
//	                        ignored.
//
//	command                 Token-wise. Every whitespace-separated token
//	                        of the granted target is matched as a full
//	                        word — the reference's own prose says the
//	                        token is evaluated as the anchored regular
//	                        expression `^(?:pattern)$` — and a token may
//	                        carry the `regex:` prefix to be read as a
//	                        regular expression.
//
//	mcp                     `server/tool` (one exact tool), `server/*`
//	                        (every tool on that server) or `*` (every
//	                        tool of every server).
//
// The two implicit rules of the same document live here as well:
//
//	"Write implies read": allowing write_file on a path grants
//	read_file on that path.
//
//	"Deny read implies deny write": denying read_file on a path blocks
//	write_file on that path.
//
// 存疑 (deferred): the document's "When commands require an exact match"
// section turns the *prefix* matching off for a command line that carries
// a shell construct it cannot parse cleanly — command or process
// substitution, arithmetic contexts, brace expansion, redirections, a
// non-literal command name — and then accepts the line only on a
// character-for-character match of the whole line. Deciding that needs a
// shell parser, and this package is tier 1 (stdlib only), so the rule is
// not implemented here: the token matching below is the ordinary prefix
// matching the document describes for every other command line.
//
// 存疑 (reading): two sentences of the document admit more than one
// reading, and the clone takes the narrower one.
//
//   - "Matches by exact word or token prefix" — the clone reads it as
//     *the granted tokens are the command's leading tokens*, so
//     "command(git)" covers "git status && git log" but not
//     "sudo git status". The other reading — the grant's word may appear
//     anywhere in the command — would let "command(git)" cover
//     "sudo git status" as well.
//
//   - "start the target with the `regex:` prefix" — the clone reads a
//     leading `regex:` as putting the whole target into
//     regular-expression mode (the form the document's own example
//     uses, `command(regex:npm run (build.*))`) and a `regex:` on any
//     other token as switching that token alone. Both are supported.
package permissions

import (
	"net/url"
	"regexp"
	"strings"
)

// regexPrefix is the reference's own opt-in marker for a regular
// expression target.
const regexPrefix = "regex:"

// matchGrantTarget matches a grant's target against a concrete
// operation, under the action's own matching rule.
//
// An empty target and the global wildcard "*" match every target: the
// wildcard is the reference's own ("Across all supported action types …
// passing the global wildcard `*` matches all targets within that entire
// action namespace"), and the empty target keeps the clone's long-standing
// behaviour for a grant that names no target at all.
func matchGrantTarget(action, pattern, target string, roots []string) bool {
	if pattern == "" || pattern == "*" {
		return true
	}
	switch action {
	case ActionCommand:
		return matchCommand(pattern, target)
	case ActionReadFile, ActionWriteFile:
		return matchPath(pattern, target, roots)
	case ActionReadURL, ActionExecuteURL:
		return matchHost(pattern, target)
	case ActionMCP:
		return matchMCPTarget(pattern, target)
	}
	return false
}

// grantCovers reports whether a grant covers a call: the action must
// cover the call's tool, and the grant's target must match the call's
// target.
//
// A grant covers every tool its action names — "command(git)" reaches
// run_command, command_status and send_command_input.
func grantCovers(action, pattern, tool, target string, roots []string) bool {
	if !actionCoversTool(action, tool) {
		return false
	}
	return matchGrantTarget(action, pattern, target, roots)
}

// commandToken is one whitespace-separated token of a granted command
// target.
type commandToken struct {
	// pattern is the token's text: the literal word, or the regular
	// expression when regex is set.
	pattern string
	// regex reports whether the token is a regular expression.
	regex bool
}

// splitCommandTokens splits a granted command target into its tokens.
//
// A `regex:` prefix on the *first* token puts the whole target into
// regular-expression mode: every token is then a regular expression.
// That is the form the documentation's own example uses —
// `command(regex:npm run (build.*))` — so the prefix on the first token
// cannot be a per-token switch alone.
//
// A `regex:` prefix on any other token switches that token alone, which
// is the form a mixed target takes (`command(npm regex:run.*)`).
func splitCommandTokens(target string) []commandToken {
	fields := strings.Fields(target)
	if len(fields) == 0 {
		return nil
	}
	whole := strings.HasPrefix(fields[0], regexPrefix)
	out := make([]commandToken, 0, len(fields))
	for _, f := range fields {
		if rest, ok := strings.CutPrefix(f, regexPrefix); ok {
			out = append(out, commandToken{pattern: rest, regex: true})
			continue
		}
		out = append(out, commandToken{pattern: f, regex: whole})
	}
	return out
}

// matchCommand matches a granted command target against a command line.
//
// The granted target is split into whitespace-separated tokens and the
// command is split the same way. The granted tokens must match the
// command's *leading* tokens, one for one, and each of them as a full
// word: a literal token is compared as text, and a `regex:` token is
// compiled as the anchored regular expression `^(?:pattern)$` the
// reference's own prose describes.
//
// The full-word rule is what keeps a grant from reaching past its own
// words: `command(git)` covers "git status" and "git status && git log"
// but not "gitk" and not "legit".
func matchCommand(pattern, target string) bool {
	tokens := splitCommandTokens(pattern)
	if len(tokens) == 0 {
		return true
	}
	target = strings.ReplaceAll(target, "\r\n", "\n")
	words := strings.Fields(target)
	if len(words) < len(tokens) {
		return false
	}
	for i, tok := range tokens {
		if !matchCommandToken(tok, words[i]) {
			return false
		}
	}
	return true
}

// matchCommandToken matches one granted token against one word of the
// command.
//
// A regular-expression token is compiled as the anchored expression
// `^(?:pattern)$`, so it covers the word and nothing else. A literal
// token is compared as text, which is the same thing for a word that
// carries no metacharacter.
func matchCommandToken(tok commandToken, word string) bool {
	if !tok.regex {
		if tok.pattern == word {
			return true
		}
		if strings.HasSuffix(tok.pattern, "/") && strings.HasPrefix(word, tok.pattern) {
			return true
		}
		return false
	}
	re, err := regexp.Compile("^(?:" + tok.pattern + ")$")
	if err != nil {
		return false
	}
	return re.MatchString(word)
}

// matchPath matches a granted file path against a concrete path.
//
// An absolute grant covers the path itself and everything under it. A
// relative grant covers the same, resolved against each workspace root;
// when no workspace root is known the granted path must appear in the
// concrete path as a run of whole components, which is the clone's
// approximation of "relative to the project workspace".
//
// Both sides are normalised first: the backslashes become forward
// slashes and the Windows drive letter is stripped, so `C:\Users\me` and
// `/Users/me` are the same path — the normalisation the documentation
// describes for Windows.
func matchPath(pattern, target string, roots []string) bool {
	grant := normalisePath(pattern)
	concrete := normalisePath(target)
	if grant == "" || concrete == "" {
		return false
	}
	if strings.HasPrefix(grant, "/") {
		return pathContains(grant, concrete)
	}
	if len(roots) > 0 {
		for _, root := range roots {
			base := normalisePath(root)
			if base == "" {
				continue
			}
			if !strings.HasPrefix(base, "/") {
				base = "/" + base
			}
			if pathContains(joinPath(base, grant), concrete) {
				return true
			}
		}
		return false
	}
	return pathHasComponents(concrete, grant)
}

// normalisePath puts a path into the shape the rules compare: forward
// slashes, no drive letter, no repeated or trailing separator.
func normalisePath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	p = strings.ReplaceAll(p, "\\", "/")
	// A Windows drive letter ("C:") carries no meaning for a rule that
	// matches a path, and the documentation strips it.
	if len(p) >= 2 && p[1] == ':' && isDriveLetter(p[0]) {
		p = p[2:]
	}
	for strings.Contains(p, "//") {
		p = strings.ReplaceAll(p, "//", "/")
	}
	for len(p) > 1 && strings.HasSuffix(p, "/") {
		p = p[:len(p)-1]
	}
	return p
}

// isDriveLetter reports whether c is a Windows drive letter.
func isDriveLetter(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
}

// joinPath appends a relative path to a base path.
func joinPath(base, rel string) string {
	if base == "/" {
		return "/" + rel
	}
	return base + "/" + rel
}

// pathContains reports whether p is dir itself or lives under it.
//
// The comparison is on whole components: "/var/log" contains
// "/var/log/app.log" but not "/var/logical/app.log".
func pathContains(dir, p string) bool {
	if dir == "" {
		return false
	}
	if dir == "/" {
		return strings.HasPrefix(p, "/")
	}
	if p == dir {
		return true
	}
	return strings.HasPrefix(p, dir+"/")
}

// pathComponents splits a normalised path into its components.
func pathComponents(p string) []string {
	p = strings.Trim(p, "/")
	if p == "" {
		return nil
	}
	return strings.Split(p, "/")
}

// pathHasComponents reports whether the concrete path contains the
// granted relative path as a run of whole components.
//
// It is the fallback for a relative grant when the policy does not know
// the workspace root: "src" covers "/ws/src/main.go" and
// "/ws/other/src/main.go", but not "/ws/srcx/main.go".
func pathHasComponents(p, rel string) bool {
	pc := pathComponents(p)
	rc := pathComponents(rel)
	if len(rc) == 0 || len(rc) > len(pc) {
		return false
	}
	for i := 0; i+len(rc) <= len(pc); i++ {
		if equalComponents(pc[i:i+len(rc)], rc) {
			return true
		}
	}
	return false
}

// equalComponents reports whether two component runs are the same.
func equalComponents(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// matchHost matches a granted hostname against a concrete URL.
//
// The grant names a hostname and covers the host itself and its
// subdomains — "google.com covers mail.google.com" — and the URL's path
// is ignored.
func matchHost(pattern, target string) bool {
	grant := hostOf(pattern)
	concrete := hostOf(target)
	if grant == "" || concrete == "" {
		return false
	}
	if grant == "*" {
		return true
	}
	if grant == concrete {
		return true
	}
	return strings.HasSuffix(concrete, "."+grant)
}

// hostOf extracts the hostname of a URL or of a bare host.
//
// A URL's hostname drops the user info and the port; a bare host is what
// stands before the first slash. Both are lowercased, because a hostname
// is case-insensitive.
func hostOf(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if strings.Contains(s, "://") {
		if u, err := url.Parse(s); err == nil {
			if h := u.Hostname(); h != "" {
				return strings.ToLower(h)
			}
		}
		// A malformed URL still carries a host before the first slash.
	}
	if i := strings.IndexAny(s, "/?#"); i >= 0 {
		s = s[:i]
	}
	if i := strings.LastIndex(s, "@"); i >= 0 {
		s = s[i+1:]
	}
	if i := strings.Index(s, ":"); i >= 0 {
		s = s[:i]
	}
	return strings.ToLower(s)
}

// matchMCPTarget matches a granted MCP target against a concrete one.
//
// The grant is `server/tool` (one exact tool), `server/*` (every tool on
// that server) or `*` (every tool of every server). A grant that names
// only a server covers that server's tools.
func matchMCPTarget(pattern, target string) bool {
	grant := strings.TrimSpace(pattern)
	if grant == "" {
		return false
	}
	if grant == "*" {
		return true
	}
	gs, gt, gHasTool := splitMCPTarget(grant)
	ts, tt, tHasTool := splitMCPTarget(strings.TrimSpace(target))
	if gs == "" {
		return false
	}
	if gs != "*" && gs != ts {
		return false
	}
	if !gHasTool {
		// The grant names a server and no tool: it covers the server's
		// tools.
		return true
	}
	if gt == "*" {
		return true
	}
	if !tHasTool {
		return false
	}
	return gt == tt
}

// splitMCPTarget splits an MCP target into its server and its tool.
func splitMCPTarget(s string) (server, tool string, hasTool bool) {
	if i := strings.IndexByte(s, '/'); i >= 0 {
		return s[:i], s[i+1:], true
	}
	return s, "", false
}
