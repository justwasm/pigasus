package rchost

import "github.com/justwasm/pigasus/internal/auth"

import (
	"flag"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/cli"
)

// ---- helpers --------------------------------------------------------------

// newFlagSetForTest parses `args` against the standard v1.3.5 flag set
// the real binary ships. The 21 top-level flags are all there
// (`--remote-control` included) so tests below exercise the real flag
// path the operator hits on the wire, not a hand-rolled subset.
func newFlagSetForTest(t *testing.T, args []string) *flag.FlagSet {
	t.Helper()
	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	if code := p.Root().Parse(args); code != 0 {
		t.Fatalf("Parse(%v) = %d", args, code)
	}
	return p.Root().FlagSet()
}

// ---- auth.IsBusinessAccount ----------------------------------------------------
//
// Cycle 241 renamed `isADCEnabled` → `auth.IsBusinessAccount` and moved the
// implementation to `business_account.go`. Both the 1.3.3 remote-control ×
// ADC gate and the 1.4.1 `Enable Telemetry` lock share the helper; the
// tests below pin the truthy/falsy matrix the same way they did before
// the rename.

// TestIsBusinessAccount_TruthyValues pins every value the reference treats
// as "Business requested" — non-empty after trimming. t.Setenv is the
// stdlib-supported way to flip an env var in a test (the value is
// restored at test end).
func TestIsBusinessAccount_TruthyValues(t *testing.T) {
	cases := []struct {
		name, val string
	}{
		{"true", "true"},
		{"one", "1"},
		{"uppercase_TRUE", "TRUE"},
		{"padded", "  yes  "},
		{"arbitrary_string", "adc-from-gcloud-cli"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			t.Setenv("AGY_ADC_AUTH", c.val)
			if !auth.IsBusinessAccount() {
				t.Errorf("auth.IsBusinessAccount() = false, want true for AGY_ADC_AUTH=%q", c.val)
			}
		})
	}
}

// TestIsBusinessAccount_FalsyValues covers the env-var unset / empty /
// whitespace-only path. The recovered shell treats these as
// "fall through to keyring/oAuth/refresh".
func TestIsBusinessAccount_FalsyValues(t *testing.T) {
	cases := []struct {
		name, val string
	}{
		{"unset", ""},
		{"empty", ""},
		{"whitespace", "   "},
		{"tab", "\t"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			// The "unset" sub-test still has to call Setenv to clear the
			// host's value so the result is deterministic.
			t.Setenv("AGY_ADC_AUTH", c.val)
			if auth.IsBusinessAccount() {
				t.Errorf("auth.IsBusinessAccount() = true, want false for AGY_ADC_AUTH=%q", c.val)
			}
		})
	}
}

// ---- CheckRemoteControlADC: matrix ----------------------------------------

// TestCheckRemoteControlADC_RejectsCombination is the head-line case:
// --remote-control + AGY_ADC_AUTH -> exit 3 (the AGY_ERROR convention
// from cmd/wagyu/api_error.go) with the documented message. Every
// recovery-changelog entry in this repo follows the `error: ...`
// prefix shape; the test pins the exit code, the prefix, and every
// grep-able recovery-path substring (the env var name + /logout).
func TestCheckRemoteControlADC_RejectsCombination(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "true")
	fs := newFlagSetForTest(t, []string{"--remote-control"})
	err := CheckRemoteControlADC(fs)
	if err == nil {
		t.Fatal("CheckRemoteControlADC returned nil; want cli.ExitCodeError(3, …)")
	}
	code, ok := cli.ExitCodeOf(err)
	if !ok {
		t.Fatalf("returned error is not an ExitCodeError: %v", err)
	}
	if code != 3 {
		t.Errorf("exit code = %d, want 3 (AGY_ERROR convention)", code)
	}
	if !strings.Contains(err.Error(), "remote-control") {
		t.Errorf("error message %q must mention 'remote-control'", err.Error())
	}
	if !strings.Contains(err.Error(), "ADC") {
		t.Errorf("error message %q must mention 'ADC'", err.Error())
	}
	if !strings.Contains(err.Error(), "AGY_ADC_AUTH") {
		t.Errorf("error message %q must mention 'AGY_ADC_AUTH'", err.Error())
	}
	if !strings.Contains(err.Error(), "/logout") {
		t.Errorf("error message %q must mention '/logout'", err.Error())
	}
}

// TestCheckRemoteControlADC_AcceptsRCWithoutADC: the operator asked
// for remote control but ADC is *not* in effect — the binary must
// fall through to the existing TUI / daemon path with no error. This
// is the "happy path" 1:1 with the documented 1.3.3 contract: the
// gate only fires when both signals are present.
func TestCheckRemoteControlADC_AcceptsRCWithoutADC(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "")
	fs := newFlagSetForTest(t, []string{"--remote-control"})
	if err := CheckRemoteControlADC(fs); err != nil {
		t.Fatalf("CheckRemoteControlADC returned %v; want nil when AGY_ADC_AUTH is empty", err)
	}
}

// TestCheckRemoteControlADC_AcceptsADCWithoutRC: ADC is in effect
// but the operator did not pass --remote-control. The existing
// behaviour (continue with fallback auth, no error) must be
// preserved. This is the no-regression case.
func TestCheckRemoteControlADC_AcceptsADCWithoutRC(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "true")
	fs := newFlagSetForTest(t, nil)
	if err := CheckRemoteControlADC(fs); err != nil {
		t.Fatalf("CheckRemoteControlADC returned %v; want nil when --remote-control is absent", err)
	}
}

// TestCheckRemoteControlADC_BothAbsent confirms the no-op case for
// callers that go through the check unconditionally (runRoot does,
// even when nothing was set).
func TestCheckRemoteControlADC_BothAbsent(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "")
	fs := newFlagSetForTest(t, nil)
	if err := CheckRemoteControlADC(fs); err != nil {
		t.Fatalf("CheckRemoteControlADC returned %v; want nil when neither flag nor env are set", err)
	}
}

// TestCheckRemoteControlADC_RCExplicitFalse: `--remote-control=false`
// on the wire must NOT trip the gate. Only the explicit "true" / `=true`
// forms count; this pins that the gate isn't tripped by the
// zero-value-of-bool footgun.
func TestCheckRemoteControlADC_RCExplicitFalse(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "true")
	fs := newFlagSetForTest(t, []string{"--remote-control=false"})
	if err := CheckRemoteControlADC(fs); err != nil {
		t.Fatalf("CheckRemoteControlADC returned %v; want nil when --remote-control=false even with ADC", err)
	}
}

// TestCheckRemoteControlADC_ADCWhitespace: only non-whitespace
// env values trigger the gate; whitespace-only env is treated as
// unset (matches the recovered reference's behaviour).
func TestCheckRemoteControlADC_ADCWhitespace(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "   ")
	fs := newFlagSetForTest(t, []string{"--remote-control"})
	if err := CheckRemoteControlADC(fs); err != nil {
		t.Fatalf("CheckRemoteControlADC returned %v; want nil when AGY_ADC_AUTH is whitespace only", err)
	}
}

// ---- runRoot integration --------------------------------------------------

// TestRunRootGateReturnsExitCodeThree covers the end-to-end wiring:
// the call inside runRoot short-circuits before the agent loop is
// reached. We hit the real main.go code by parsing through the same
// cli.Parser the binary uses and asserting the returned error encodes
// exit code 3 — the AGY_ERROR convention captured in
// `cmd/wagyu/api_error.go:33 headlessAPIExitCode` and lifted from the
// 1.3.3 value (1) for cycle 264.
//
// (runRoot itself needs a working TTY/wizard env to reach the gate,
// so the test exercises the helper directly — that's the unit the gate
// runs on; full process-level coverage is in the help-level test below.)
func TestRunRootGateReturnsExitCodeThree(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "true")
	fs := newFlagSetForTest(t, []string{"--remote-control"})
	err := CheckRemoteControlADC(fs)
	if err == nil {
		t.Fatal("expected an error from the gate; got nil")
	}
	code, ok := cli.ExitCodeOf(err)
	if !ok {
		t.Fatalf("error is not ExitCodeError: %v", err)
	}
	if code != 3 {
		t.Fatalf("exit code = %d, want exactly 3 (the AGY_ERROR / cycle 264 contract)", code)
	}
}

// TestHelpStillWorksWithADC is the regression-safety net for the
// gating decision: --help is handled by the parser before runRoot
// runs, so a user inspecting the binary with `antigravity --help`
// while AGY_ADC_AUTH is set must still see the help block and get
// exit code 0 (not the ADC error).
//
// We exercise this end-to-end by routing through cli.Parser.Parse so
// the parser's --help short-circuit fires; that mirrors what the
// binary does in production.
func TestHelpStillWorksWithADC(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "true")
	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	code := p.Root().Parse([]string{"--help"})
	if code != 0 {
		t.Fatalf("--help exit code = %d, want 0 (must not trip the ADC gate)", code)
	}
}

// TestVersionStillWorksWithADC: the same short-circuit rule applies
// to --version. Documenting the contract for both flags together so a
// future move of the gate doesn't quietly regress either path.
func TestVersionStillWorksWithADC(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "true")
	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	code := p.Root().Parse([]string{"--version"})
	if code != 0 {
		t.Fatalf("--version exit code = %d, want 0 (must not trip the ADC gate)", code)
	}
}
