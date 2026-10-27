// header_substitute_test.go — cycle 247 MCP server ${VAR} header
// substitution.
//
// AGY 1.4.0 changelog (internal/cli/testdata/changelog_reference.txt:21):
//
//	"Fixed plugin MCP servers that use a variable in a request header,
//	 such as `"Authorization": "Bearer ${TOKEN}"`, sending the literal
//	 placeholder and failing to authenticate; headers now get the value
//	 you configured, like the server's command, arguments, and
//	 environment already did."
//
// These tests pin the four supported syntaxes plus three guard rails
// (keys untouched, input unmutated, empty input safe).
package mcp

import (
	"reflect"
	"testing"
)

// TestSubstituteHeaderVars_Basic covers the canonical case from the
// changelog: a "Bearer ${TOKEN}" header expands to the configured
// value when TOKEN is present in the merged env.
func TestSubstituteHeaderVars_Basic(t *testing.T) {
	headers := map[string]string{
		"Authorization": "Bearer ${TOKEN}",
	}
	env := map[string]string{"TOKEN": "abc123"}
	got := SubstituteHeaderVars(headers, env)
	want := map[string]string{"Authorization": "Bearer abc123"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got["Authorization"], want["Authorization"])
	}
}

// TestSubstituteHeaderVars_BareDollar covers the shell-style bare
// `$VAR` form (no braces) — used when the variable is followed by a
// non-identifier character.
func TestSubstituteHeaderVars_BareDollar(t *testing.T) {
	headers := map[string]string{"X-Trace": "$TOKEN/prod"}
	env := map[string]string{"TOKEN": "abc123"}
	got := SubstituteHeaderVars(headers, env)
	want := map[string]string{"X-Trace": "abc123/prod"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got["X-Trace"], want["X-Trace"])
	}
}

// TestSubstituteHeaderVars_Default covers the `${VAR:-default}` form
// with an unset variable — the default value is emitted.
func TestSubstituteHeaderVars_Default(t *testing.T) {
	headers := map[string]string{"X-Tenant": "${MISSING:-fallback}"}
	env := map[string]string{} // MISSING absent
	got := SubstituteHeaderVars(headers, env)
	want := map[string]string{"X-Tenant": "fallback"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got["X-Tenant"], want["X-Tenant"])
	}
}

// TestSubstituteHeaderVars_UnsetEmpty covers `${MISSING}` (no default)
// with the variable absent — the empty string is emitted and the
// function does not panic.
func TestSubstituteHeaderVars_UnsetEmpty(t *testing.T) {
	headers := map[string]string{"Authorization": "Bearer ${MISSING}"}
	env := map[string]string{} // MISSING absent
	got := SubstituteHeaderVars(headers, env)
	want := map[string]string{"Authorization": "Bearer "}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got["Authorization"], want["Authorization"])
	}
}

// TestSubstituteHeaderVars_EscapeDollar covers the `$$` escape — useful
// when a literal `$` followed by digits must reach the wire (e.g. an
// opaquely-prefixed price header).
func TestSubstituteHeaderVars_EscapeDollar(t *testing.T) {
	headers := map[string]string{"X-Price": "$$100"}
	env := map[string]string{"100": "should not be used"}
	got := SubstituteHeaderVars(headers, env)
	want := map[string]string{"X-Price": "$100"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got["X-Price"], want["X-Price"])
	}
}

// TestSubstituteHeaderVars_NoSubstitutionInKey pins that header names
// (keys) are passed through unchanged even when a syntactically valid
// reference appears in the key. The function scans values, not keys.
func TestSubstituteHeaderVars_NoSubstitutionInKey(t *testing.T) {
	headers := map[string]string{"$TOKEN": "${TOKEN}"} // key looks like a variable
	env := map[string]string{"TOKEN": "abc123", "$TOKEN": "metadata-name"}
	got := SubstituteHeaderVars(headers, env)
	want := map[string]string{"$TOKEN": "abc123"} // key untouched, value substituted
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %+v, want %+v", got, want)
	}
	// Belt-and-braces: the key in the result must equal the input key byte-for-byte.
	if _, ok := got["$TOKEN"]; !ok {
		t.Errorf("result map lost the literal-key entry %q", "$TOKEN")
	}
}

// TestSubstituteHeaderVars_DoesNotMutateInput pins the contract that
// the input map is not modified in place. The cloned headers map is
// checked key-by-key against a deep copy of the original.
func TestSubstituteHeaderVars_DoesNotMutateInput(t *testing.T) {
	headers := map[string]string{
		"Authorization": "Bearer ${TOKEN}",
		"X-Price":       "$$100",
		"X-Tenant":      "${MISSING:-fallback}",
		"X-Static":      "no-vars-here",
	}
	original := make(map[string]string, len(headers))
	for k, v := range headers {
		original[k] = v
	}
	env := map[string]string{"TOKEN": "abc123"}
	_ = SubstituteHeaderVars(headers, env)
	if !reflect.DeepEqual(headers, original) {
		t.Errorf("input mutated: got %+v, want %+v", headers, original)
	}
}

// TestSubstituteHeaderVars_NilAndEmpty covers the safety rails: a nil
// input map yields a non-nil empty result (so callers can range over
// it without a nil check), and an empty value string is returned as-is.
func TestSubstituteHeaderVars_NilAndEmpty(t *testing.T) {
	t.Run("nil input yields empty map", func(t *testing.T) {
		got := SubstituteHeaderVars(nil, map[string]string{"X": "y"})
		if got == nil {
			t.Fatalf("got nil; want non-nil empty map")
		}
		if len(got) != 0 {
			t.Errorf("got %+v; want empty", got)
		}
	})
	t.Run("empty value string passes through", func(t *testing.T) {
		got := SubstituteHeaderVars(map[string]string{"X": ""}, nil)
		want := map[string]string{"X": ""}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
	t.Run("value with no dollar passes through", func(t *testing.T) {
		got := SubstituteHeaderVars(map[string]string{"X": "plain"}, map[string]string{"X": "should not be touched"})
		want := map[string]string{"X": "plain"}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("got %+v, want %+v", got, want)
		}
	})
}

// TestSubstituteHeaderVars_DefaultSkippedWhenSet covers the rule that
// the default clause of `${VAR:-default}` is only used when the
// variable is unset or empty. A set, non-empty value wins.
func TestSubstituteHeaderVars_DefaultSkippedWhenSet(t *testing.T) {
	headers := map[string]string{"X-Tenant": "${TENANT:-fallback}"}
	env := map[string]string{"TENANT": "acme"}
	got := SubstituteHeaderVars(headers, env)
	want := map[string]string{"X-Tenant": "acme"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got["X-Tenant"], want["X-Tenant"])
	}
}

// TestSubstituteHeaderVars_NoRecursiveExpansion pins that a
// substituted value containing another `$VAR` is not re-scanned — the
// header reaches the wire as the literal substituted value, not a
// second expansion.
func TestSubstituteHeaderVars_NoRecursiveExpansion(t *testing.T) {
	headers := map[string]string{"X-Meta": "${OUTER}"}
	// OUTER's value contains `${INNER}` — should not be re-expanded.
	env := map[string]string{
		"OUTER": "got-${INNER}",
		"INNER": "should-not-appear",
	}
	got := SubstituteHeaderVars(headers, env)
	want := map[string]string{"X-Meta": "got-${INNER}"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q, want %q", got["X-Meta"], want["X-Meta"])
	}
}
