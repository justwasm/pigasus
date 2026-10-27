// Package envref expands environment-variable references in a string.
//
// Two surfaces of the clone need it and each used to carry its own
// copy of the scanner:
//
//   - mcp's header substitution (AGY 1.4.0): the reference
//     expands ${VAR}, $VAR, ${VAR:-default} and $$ inside a header
//     value — the fix the 1.4.0 changelog records as "headers now get
//     the value you configured".
//   - internal/models' credential expansion: a provider's API key may
//     be written "$NAME" or "${NAME}" in providers.json.
//
// The two are different shapes — one scans a value, the other expands a
// whole value — but the reference syntax is one, so the scanner lives
// here and both surfaces call it.
//
// The shape mirrors shell parameter expansion:
//
//	${VAR}            the variable's value, or ""
//	$VAR              the variable's value, or ""
//	${VAR:-default}   the variable's value if non-empty, else "default"
//	$$                literal "$"
//
// The mapping is not recursive — a substituted value that contains
// another "$VAR" stays literal — and only values are scanned, never
// keys.
//
// Stdlib only.
package envref

import "strings"

// Lookup resolves a variable name to its value. A variable that is not
// set resolves to the empty string.
//
// The reference does not distinguish "not set" from "set to the empty
// string" in either of the two surfaces that use this package: the
// `${VAR:-default}` form falls back to the default when the value is
// empty, and a plain `${VAR}` yields "" either way.
type Lookup func(name string) string

// Expand scans s and substitutes the variable references it finds.
//
// A reference whose name is empty ("${}", "${:-D}") is left literal: an
// empty name is not a valid identifier, and the caller did not ask for
// a permissive parser. A "$" that starts none of the four forms is
// emitted verbatim and the scan resumes at the next byte.
//
// The mapping is not recursive: a substituted value that contains
// another "$VAR" stays literal.
func Expand(s string, lookup Lookup) string {
	if s == "" || !strings.ContainsRune(s, '$') {
		// No dollar at all: return the original backing string
		// unchanged. Saves an allocation in the common case (e.g. an
		// `Authorization: Bearer <opaque-no-dollar>` header).
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
			name, def, hasDefault, consumed, ok := scanBraced(s[i:])
			if !ok {
				// No closing brace: emit "$" literally and let the
				// rest of the run flow through the outer loop,
				// matching what a shell would do for an
				// unterminated brace.
				b.WriteByte('$')
				i++
				continue
			}
			if name == "" {
				// "${}" / "${:-D}" — empty identifier, leave
				// literal: an empty name is not a valid variable
				// reference.
				b.WriteString(s[i : i+consumed])
				i += consumed
				continue
			}
			if hasDefault {
				if v := lookup(name); v != "" {
					b.WriteString(v)
				} else {
					b.WriteString(def)
				}
			} else {
				// "${NAME}" without ":-" — present value or "".
				b.WriteString(lookup(name))
			}
			i += consumed
		case isIdentStart(next):
			// "$NAME" — read [A-Za-z_][A-Za-z0-9_]*
			name, consumed, _ := scanBare(s[i:])
			b.WriteString(lookup(name))
			i += consumed
		default:
			// '$' followed by something that is not '$', '{', or an
			// identifier start — emit the '$' literally and continue.
			b.WriteByte('$')
			i++
		}
	}
	return b.String()
}

// ExpandWhole expands s when it is exactly one reference.
//
// A value of "$NAME" or "${NAME}" becomes the variable's value. Any
// other value is returned unchanged — including a literal that merely
// contains a dollar sign, and including a reference with a suffix
// ("$NAME/suffix"), which is not one reference but two.
//
// The value is trimmed before it is examined, so a credential written
// with surrounding whitespace still resolves; a value that is not a
// reference is returned *untrimmed*, the way the caller handed it over.
func ExpandWhole(s string, lookup Lookup) string {
	v := strings.TrimSpace(s)
	if v == "" {
		return ""
	}
	switch v[0] {
	case '$':
	default:
		return s
	}
	if len(v) > 1 && v[1] == '{' {
		name, _, _, consumed, ok := scanBraced(v)
		if !ok || name == "" || consumed != len(v) {
			return s
		}
		return lookup(name)
	}
	name, consumed, found := scanBare(v)
	if !found || consumed != len(v) {
		return s
	}
	return lookup(name)
}

// scanBraced scans a "${NAME}" or "${NAME:-default}" reference at the
// start of s.
//
// It returns the variable's name, the default value (when the
// reference carries one), whether a default was present, how many bytes
// the reference occupies and whether a reference was found at all.
func scanBraced(s string) (name string, def string, hasDefault bool, consumed int, ok bool) {
	if len(s) < 2 || s[0] != '$' || s[1] != '{' {
		return "", "", false, 0, false
	}
	end := strings.IndexByte(s[2:], '}')
	if end < 0 {
		return "", "", false, 0, false
	}
	inner := s[2 : 2+end]
	nameOnly, defaultVal, hasDefault := strings.Cut(inner, ":-")
	return nameOnly, defaultVal, hasDefault, end + 3, true
}

// scanBare scans a "$NAME" reference at the start of s. It returns the
// variable's name, how many bytes the reference occupies and whether a
// reference was found at all.
func scanBare(s string) (name string, consumed int, found bool) {
	if len(s) < 2 || s[0] != '$' || !isIdentStart(s[1]) {
		return "", 0, false
	}
	j := 2
	for j < len(s) && isIdentPart(s[j]) {
		j++
	}
	return s[1:j], j, true
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
