// envref_test.go — the shared variable-reference scanner.
//
// The scanner serves two surfaces: the MCP header substitution (which
// scans a value) and the provider credential expansion (which expands a
// whole value). The tables below pin both shapes, including the cases
// that are deliberately literal.
package envref

import "testing"

// env is the lookup the tables use.
func env(pairs ...string) Lookup {
	m := map[string]string{}
	for i := 0; i+1 < len(pairs); i += 2 {
		m[pairs[i]] = pairs[i+1]
	}
	return func(name string) string { return m[name] }
}

// TestExpand pins the inline scanner — the shape the MCP header
// substitution uses.
func TestExpand(t *testing.T) {
	lookup := env("TOKEN", "secret", "EMPTY", "")
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"plain", "no dollar here", "no dollar here"},
		{"empty", "", ""},
		{"braced", "Bearer ${TOKEN}", "Bearer secret"},
		{"bare", "Bearer $TOKEN", "Bearer secret"},
		{"braced-unset", "Bearer ${MISSING}", "Bearer "},
		{"bare-unset", "Bearer $MISSING", "Bearer "},
		{"default-taken", "${TOKEN:-fallback}", "secret"},
		{"default-taken-empty-var", "${EMPTY:-fallback}", "fallback"},
		{"default-taken-unset", "${MISSING:-fallback}", "fallback"},
		{"escaped", "$$TOKEN", "$TOKEN"},
		{"two-references", "$TOKEN/$TOKEN", "secret/secret"},
		// A reference with a suffix is a reference *and* a suffix —
		// the shell's shape.
		{"suffix", "$TOKEN/suffix", "secret/suffix"},
		// Deliberately literal.
		{"empty-name", "${}", "${}"},
		{"empty-name-with-default", "${:-D}", "${:-D}"},
		{"unterminated", "${TOKEN", "${TOKEN"},
		{"trailing-dollar", "tail$", "tail$"},
		{"dollar-space", "$ TOKEN", "$ TOKEN"},
		{"dollar-digit", "$1", "$1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Expand(tc.in, lookup); got != tc.want {
				t.Errorf("Expand(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestExpandIsNotRecursive pins that a substituted value is not scanned
// again.
func TestExpandIsNotRecursive(t *testing.T) {
	lookup := env("A", "$B", "B", "value")
	if got := Expand("$A", lookup); got != "$B" {
		t.Errorf("Expand(\"$A\") = %q, want %q", got, "$B")
	}
}

// TestExpandWhole pins the whole-value shape — the one the provider
// credential expansion uses.
func TestExpandWhole(t *testing.T) {
	lookup := env("TOKEN", "secret", "EMPTY", "")
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"bare", "$TOKEN", "secret"},
		{"braced", "${TOKEN}", "secret"},
		{"literal", "literal", "literal"},
		{"dollar-alone", "$", "$"},
		{"unset", "$MISSING", ""},
		{"unset-braced", "${MISSING}", ""},
		{"empty-value", "$EMPTY", ""},
		// Not one reference: the value is handed back untouched.
		{"suffix", "$TOKEN/suffix", "$TOKEN/suffix"},
		{"prefix", "prefix-$TOKEN", "prefix-$TOKEN"},
		{"two-references", "$TOKEN$TOKEN", "$TOKEN$TOKEN"},
		{"empty-name", "${}", "${}"},
		// The value is trimmed before it is examined, and a value
		// that is not a reference comes back untrimmed.
		{"trimmed-reference", "  $TOKEN  ", "secret"},
		{"untrimmed-literal", "  literal  ", "  literal  "},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ExpandWhole(tc.in, lookup); got != tc.want {
				t.Errorf("ExpandWhole(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
