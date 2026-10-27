// header_substitute.go — variable expansion for MCP server header values.
//
// AGY 1.4.0 changelog (internal/cli/testdata/changelog_reference.txt:21):
//
//	"Fixed plugin MCP servers that use a variable in a request header,
//	 such as `"Authorization": "Bearer ${TOKEN}"`, sending the literal
//	 placeholder and failing to authenticate; headers now get the value
//	 you configured, like the server's command, arguments, and
//	 environment already did."
//
// The substitution is performed here, on the agy side, before the headers
// reach PiG's McpServerConfig. The shape mirrors shell parameter expansion:
//
//	${VAR}            env["VAR"] or ""
//	$VAR              env["VAR"] or ""
//	${VAR:-default}   env["VAR"] if non-empty, else "default"
//	$$                literal "$"
//
// The scanner itself lives in envref — the same scanner the
// provider credential expansion uses — so the two surfaces cannot drift
// apart.
package mcp

import "github.com/justwasm/pigasus/envref"

// SubstituteHeaderVars returns a new map with every value scanned for
// variable references and replaced from env. The input map is not
// mutated.
//
// Substitution rules (applied left-to-right):
//
//   - "$$"        → literal "$"
//   - "$NAME"      → env["NAME"] if present, else ""
//   - "${NAME}"    → env["NAME"] if present, else ""
//   - "${NAME:-D}" → env["NAME"] if present and non-empty, else "D"
//
// A reference whose NAME is empty ("${}", "${:-D}") is left literal: an
// empty name is not a valid identifier, and the caller did not ask for a
// permissive parser. A `$` followed by anything that does not match one
// of the four cases is emitted verbatim and the scan resumes at the next
// rune.
//
// The function does not mutate m, does not recurse into substituted
// values, and never panics on missing keys (it returns "" instead).
// Passing a nil map yields an empty (non-nil) result map.
func SubstituteHeaderVars(m map[string]string, env map[string]string) map[string]string {
	out := make(map[string]string, len(m))
	for k, v := range m {
		out[k] = envref.Expand(v, func(name string) string { return env[name] })
	}
	return out
}
