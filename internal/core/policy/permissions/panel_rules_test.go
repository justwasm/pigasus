package permissions

import (
	"path/filepath"
	"strings"
	"testing"

	projectflags "github.com/justwasm/pigasus/internal/core/cliflags/projects"
)

// TestProjectConfigPath pins the file the panel's project scope reads and
// writes: the default project's own file, in the reference's project
// directory.
//
// The id and the path shape belong to the package that owns them, so the
// expectation asks that package — the literal itself is pinned there.
//
// The home directory is redirected: the test must not reach the user's
// own home.
func TestProjectConfigPath(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	want := projectflags.ConfigPath(home, projectflags.DefaultProjectID)
	if got := ProjectConfigPath(); got != want {
		t.Errorf("the project config path is %q, want %q", got, want)
	}
	if got := filepath.Base(ProjectConfigPath()); got != projectflags.DefaultProjectID+".json" {
		t.Errorf("the project config file is %q, want %q", got, projectflags.DefaultProjectID+".json")
	}
}

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
		// Cycle 381: the cases the /permissions panel's own test
		// pinned moved here with the rule they exercise.
		{"command(grep)", ""},
		{"command(a|b)", ""},
		{"command()", ""},
		{`command(grep)ZZ`, `invalid grant string: "command(grep)ZZ"`},
		{`foo(bar)`, `unknown action "foo" in grant string: "foo(bar)"`},
		{`read(*)`, `unknown action "read" in grant string: "read(*)"`},
		{``, `invalid grant string: ""`},
		{`command(`, `invalid grant string: "command("`},
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
