// Tests for the recovered `--mode` surface (cycle 98).
//
// Reference behaviour, verified by probing agy 1.3.6
// (SHA-256 827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab)
// over a scratch HOME:
//
//	agy --mode bogus models    → stderr "warning: unrecognized --mode value
//	                             \"bogus\" (valid: accept-edits, auto, plan)"
//	                             then "Fetching available models..." + the
//	                             sign-in error, exit 1
//	agy --mode bogus models x  → only the unexpected-arguments error (the
//	                             warning sits after arg validation)
//	agy --mode bogus agents    → warning only, exit 0
//	agy --mode bogus agents x  → unexpected-arguments error, no warning
//	agy --mode bogus changelog → no warning (not an agent-session path)
//
// The auto-unavailable warning is `--print`-only and lives in
// cmd/wagyu (see warnAgentMode); it is covered there.
package cli

import (
	"strings"
	"testing"
)

func TestModeList(t *testing.T) {
	if got := ModeList(); got != "accept-edits, auto, plan" {
		t.Errorf("ModeList() = %q, want %q", got, "accept-edits, auto, plan")
	}
}

func TestModeWarning_ExactStrings(t *testing.T) {
	cases := []struct {
		mode string
		want string
	}{
		{"", ""},
		{"accept-edits", ""},
		{"auto", ""},
		{"plan", ""},
		{"bogus", `warning: unrecognized --mode value "bogus" (valid: accept-edits, auto, plan)`},
		{"ACCEPT-EDITS", `warning: unrecognized --mode value "ACCEPT-EDITS" (valid: accept-edits, auto, plan)`},
		{"auto ", `warning: unrecognized --mode value "auto " (valid: accept-edits, auto, plan)`},
	}
	for _, c := range cases {
		if got := ModeWarning(c.mode); got != c.want {
			t.Errorf("ModeWarning(%q) =\n  %q\nwant\n  %q", c.mode, got, c.want)
		}
	}
}

func TestAutoUnavailableWarning_ExactString(t *testing.T) {
	want := "warning: --mode auto is not available for this account; continuing in the default mode."
	if got := AutoUnavailableWarning(ModeAuto); got != want {
		t.Errorf("AutoUnavailableWarning(auto) =\n  %q\nwant\n  %q", got, want)
	}
}

// TestModeUsage_DerivedFromModeValues pins the flag usage to the same
// single source of truth as the warning. The reference builds it from a
// format string ("Set the agent execution mode for this session (%s)"),
// so a divergence here would mean the list was duplicated.
func TestModeUsage_DerivedFromModeValues(t *testing.T) {
	var usage string
	for _, f := range StandardFlags() {
		if f.Name == "mode" {
			usage = f.Usage
		}
	}
	if usage == "" {
		t.Fatal("no --mode flag in StandardFlags()")
	}
	want := "Set the agent execution mode for this session (" + ModeList() + ")"
	if usage != want {
		t.Errorf("--mode usage =\n  %q\nwant\n  %q", usage, want)
	}
}

// TestModeWarning_EmissionSurface checks which subcommands emit the
// warning and at which point in the sequence, which is the part most
// easily got wrong (the warning sits *after* argument validation).
func TestModeWarning_EmissionSurface(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantWarn bool
	}{
		{"models", []string{"--mode", "bogus", "models"}, true},
		{"models+extra-arg", []string{"--mode", "bogus", "models", "extra"}, false},
		{"agents", []string{"--mode", "bogus", "agents"}, true},
		{"agents+arg", []string{"--mode", "bogus", "agents", "x"}, false},
		{"agent", []string{"--mode", "bogus", "agent"}, true},
		{"changelog", []string{"--mode", "bogus", "changelog"}, false},
		{"plugin", []string{"--mode", "bogus", "plugin", "list"}, false},
		{"mcp", []string{"--mode", "bogus", "mcp", "list"}, false},
		{"models+valid-mode", []string{"--mode", "plan", "models"}, false},
		{"models+no-mode", []string{"models"}, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, errb, _ := runJetski(t, c.args...)
			warned := strings.Contains(errb, "unrecognized --mode")
			if warned != c.wantWarn {
				t.Errorf("args %v: warned=%v want %v\nstderr:\n%s", c.args, warned, c.wantWarn, errb)
			}
		})
	}
}

// TestModeWarning_IsFirstStderrLine pins the ordering: on the models path
// the warning precedes "Fetching available models...". The scratch HOME
// makes the sign-in check fail, which is what gives exit 1.
func TestModeWarning_IsFirstStderrLine(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("JETSKI_OAUTH_TOKEN", "")
	_, errb, code := runJetski(t, "--mode", "bogus", "models")
	lines := strings.Split(strings.TrimSuffix(errb, "\n"), "\n")
	if len(lines) < 1 || !strings.HasPrefix(lines[0], "warning: unrecognized --mode value \"bogus\"") {
		t.Fatalf("first stderr line = %q, want the --mode warning", lines[0])
	}
	if len(lines) < 2 || lines[1] != "Fetching available models..." {
		t.Fatalf("second stderr line = %q, want \"Fetching available models...\"", lines[1])
	}
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
}
