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
// The mapping is not recursive — a substituted value that contains another
// `$VAR` stays literal — and only values are scanned, never keys.
//
// Stdlib only.
package mcp

import "strings"

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
		out[k] = substituteOne(v, env)
	}
	return out
}

// substituteOne scans a single value string. Exported only via
// SubstituteHeaderVars; visible to tests in the same package.
func substituteOne(s string, env map[string]string) string {
	if s == "" {
		return s
	}
	// Fast path: no dollar at all, return the original backing
	// string unchanged. Saves an allocation in the common case
	// (e.g. an Authorization: Bearer <opaque-no-dollar> header).
	if !strings.ContainsRune(s, '$') {
		return s
	}
	var b strings.Builder
	b.Grow(len(s))
	i := 0
	for i < len(s) {
		c := s[i]
		if c != '$' {
			b.WriteByte(c)
			i++
			continue
		}
		// c == '$'
		if i+1 >= len(s) {
			// trailing '$' — emit literal.
			b.WriteByte('$')
			i++
			continue
		}
		next := s[i+1]
		switch {
		case next == '$':
			// "$$" → literal "$".
			b.WriteByte('$')
			i += 2
		case next == '{':
			// ${...}: read the closing brace.
			end := strings.IndexByte(s[i+2:], '}')
			if end < 0 {
				// No closing brace: emit "$" literally and
				// let the rest of the run flow through the
				// outer loop, matching what a shell would
				// do for an unterminated brace.
				b.WriteByte('$')
				i++
				continue
			}
			name := s[i+2 : i+2+end]
			nameOnly, defaultVal, hasDefault := strings.Cut(name, ":-")
			consumed := end + 3 // "${" + name + "}"
			if nameOnly == "" {
				// "${}" / "${:-D}" — empty identifier, leave
				// literal: an empty name is not a valid
				// variable reference.
				b.WriteString(s[i : i+consumed])
				i += consumed
				continue
			}
			if hasDefault {
				if v, ok := env[nameOnly]; ok && v != "" {
					b.WriteString(v)
				} else {
					b.WriteString(defaultVal)
				}
			} else {
				// "${NAME}" without ":-" — present value or "".
				b.WriteString(env[nameOnly])
			}
			i += consumed
		case isIdentStart(next):
			// "$NAME" — read [A-Za-z_][A-Za-z0-9_]*
			j := i + 2
			for j < len(s) && isIdentPart(s[j]) {
				j++
			}
			name := s[i+1 : j]
			b.WriteString(env[name])
			i = j
		default:
			// '$' followed by something that is not '$', '{', or an
			// identifier start — emit the '$' literally and continue.
			b.WriteByte('$')
			i++
		}
	}
	return b.String()
}

// isIdentStart reports whether c can start a bare-dollar identifier.
// Follows Go's identifier rules: letter or underscore.
func isIdentStart(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || c == '_'
}

// isIdentPart reports whether c can continue an identifier. Follows Go's
// identifier rules: letter, digit, or underscore.
func isIdentPart(c byte) bool {
	return isIdentStart(c) || (c >= '0' && c <= '9')
}
