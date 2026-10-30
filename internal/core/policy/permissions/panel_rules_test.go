package permissions

import (
	"strings"
	"testing"
)

func TestValidateGrant(t *testing.T) {
	cases := []struct {
		rule string
		want string
	}{
		{"command(ls)", ""},
		{"command(git diff)", ""},
		{"read(foo)", `unknown action "read" in grant string: "read(foo)"`},
		{"bad-rule", `invalid grant string: "bad-rule"`},
		{"foo()", `unknown action "foo" in grant string: "foo()"`},
	}
	for _, tc := range cases {
		got := ValidateGrant(tc.rule)
		if got != tc.want {
			t.Errorf("ValidateGrant(%q) = %q, want %q", tc.rule, got, tc.want)
		}
	}
}

func TestPermTabRow(t *testing.T) {
	row := PermTabRow(0, [3]int{1, 2, 3})
	if !strings.Contains(row, "allowlist") || !strings.Contains(row, "(1)") {
		t.Errorf("PermTabRow(0, ...) missing allowlist or count: %q", row)
	}
}

func TestAnyStringSlice(t *testing.T) {
	in := []any{"a", "b", 123, "c"}
	out := AnyStringSlice(in)
	if len(out) != 3 || out[0] != "a" || out[1] != "b" || out[2] != "c" {
		t.Errorf("AnyStringSlice(%v) = %v, want [a b c]", in, out)
	}
	if AnyStringSlice("not-a-slice") != nil {
		t.Errorf("AnyStringSlice(non-slice) should return nil")
	}
}
