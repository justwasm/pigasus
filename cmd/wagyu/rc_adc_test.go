// rc_adc_test.go — cycle 264: AGY 1.4.1 changelog line 79.
//
//	"Fixed `--remote-control` failing when you sign in with Application
//	 Default Credentials (`AGY_ADC_AUTH`); Remote Control doesn't
//	 support ADC yet, so the flag now exits with a clear error and the
//	 `/remote-control` command is hidden."
//
// The 1.3.3-line-53 surface was raised from exit 1 to exit 3 (the
// AGY_ERROR convention from cmd/wagyu/api_error.go) and expanded to
// cover:
//   - the `--remote-control` flag → cli.ExitCodeError(3, msg)
//     naming the env var and the /logout recovery path;
//   - the in-session `/remote-control` slash command → the same
//     message rendered in the slash Respond wrapper;
//   - the slash menu picker → the entry disappears from the menu
//     (and from the `/remote` substring filter) when ADC is in
//     effect.
//
// Existing 1.3.3 tests for the flag gate live in
// `cmd/wagyu/remote_control_adc_test.go` and were updated in this
// cycle to pin the AGY_ERROR exit code (3) and the new message
// contract. The tests here exercise the cross-surface wiring — what
// the user actually sees — and the picker filter, which has no
// pre-existing tests because the cycle-241 picker API was
// fixed-shape (no hidden set).
package main

import (
	"flag"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/tuilib"
)

// ---- 1. flag gate: --remote-control under ADC --------------------------------

// TestRemoteControl_FailsUnderADC is the headline end-to-end test for
// the new gate's AGY_ERROR exit code (3). It matches the wording of
// the AGY 1.4.1 changelog entry: ADC active + --remote-control → exit
// with a clear error.
//
// The previous cycle's flag-gate tests in
// `cmd/wagyu/remote_control_adc_test.go` already pin the same contract
// at the helper level (checkRemoteControlADC). This test additionally
// pulls `cli.ExitCodeError` so a regression where the helper returns
// `errors.New(...)` instead of the typed error is caught — the public
// path through runRoot depends on `cli.ExitCodeOf` to translate the
// error into the process exit code.
func TestRemoteControl_FailsUnderADC(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "1")
	fs := buildRemoteControlFlagSet(t, []string{"--remote-control"})

	err := checkRemoteControlADC(fs)
	if err == nil {
		t.Fatal("checkRemoteControlADC returned nil; want non-nil under ADC + --remote-control")
	}

	// Exit code 3 is the AGY_ERROR convention (cmd/wagyu/api_error.go
	// `headlessAPIExitCode`). Pinned exactly because the process exits
	// there, not 1 (cycle 241) and not 2 (the --print/--prompt-
	// interactive conflict code).
	code, ok := cli.ExitCodeOf(err)
	if !ok {
		t.Fatalf("error is not a cli.ExitCodeError: %T %v", err, err)
	}
	if code != 3 {
		t.Errorf("exit code = %d, want exactly 3 (AGY_ERROR convention)", code)
	}

	// Error message contract — the hard-constraint substring set:
	msg := err.Error()
	wantSubstrs := []string{
		"remote-control",      // grep-able from logs
		"Application Default", // full human-readable name
		"Credentials",         // ditto
		"AGY_ADC_AUTH",        // the env var the user can unset
		"/logout",             // the recovery command
	}
	for _, want := range wantSubstrs {
		if !strings.Contains(msg, want) {
			t.Errorf("error message %q must contain substring %q", msg, want)
		}
	}
}

// ---- 2. TUI slash command: /remote-control under ADC -------------------------

// slashRemoteControlRunMode builds a runMode suitable for driving
// handleSlashCommand with the /remote-control case directly. The TUI
// input editor must be fully initialised because handleSlashCommand
// resets m.input as part of the dispatch prelude (see
// cmd/wagyu/slash_commands.go:117), which delegates to the bubbles
// textarea's Reset, which in turn touches the viewport. A bare
// `&runMode{}` panics — we go through NewRunMode with a no-op backend
// (the test never sends a turn) and a fresh tmpdir for ANTIGRAVITY_HOME
// so the constructor's keyring/auth probing doesn't wander off into
// the host's real config tree.
func slashRemoteControlRunMode(t *testing.T) *runMode {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", t.TempDir(), true)
	return m
}

// TestRemoteControl_FailsUnderADCSlashCommand drives the TUI's
// handleSlashCommand with "/remote-control" under ADC. The handler
// must short-circuit before remoteControlFeedback runs (so
// m.remoteControl stays false) and emit the slashRespond wrapper
// containing the recovery-path message instead of the typical
// "Remote control on for this session." line.
func TestRemoteControl_FailsUnderADCSlashCommand(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "1")

	m := slashRemoteControlRunMode(t)
	handled, cmd := m.handleSlashCommand("/remote-control")
	if !handled {
		t.Fatal(`/remote-control was not handled under ADC; want handled=true`)
	}
	// The returned cmd should be nil — handleSlashCommand returns a
	// tea.Cmd only for commands like /exit that schedule a Quit, and
	// the ADC rejection takes neither path.
	if cmd != nil {
		t.Errorf("handleSlashCommand returned a tea.Cmd (%T %v); want nil", cmd, cmd)
	}

	// The transcript (`m.response`) must contain the error message.
	// m.slashRespond appends three rows: `> /remote-control`, then
	// `  ⎿  <msg>`, then a blank. We look for the canonical keywords
	// across the joined transcript so the test does not fragment when
	// slashRespond's colour codes change.
	var joined strings.Builder
	for _, line := range m.response {
		joined.WriteString(line)
		joined.WriteByte('\n')
	}
	transcript := joined.String()

	wantSubstrs := []string{
		"/remote-control",
		"Application Default",
		"Credentials",
		"AGY_ADC_AUTH",
		"/logout",
	}
	for _, want := range wantSubstrs {
		if !strings.Contains(transcript, want) {
			t.Errorf("transcript must contain %q; full transcript:\n%s", want, transcript)
		}
	}

	// Critical side-effect: the session flag must NOT have flipped.
	// The cycle 106 contract is that m.remoteControl becomes true on
	// a successful "/remote-control" (bare or "/remote-control on").
	// Under ADC we must reject without flipping the flag, otherwise a
	// subsequent NewRunMode boot could read a stale true and report a
	// ghost session.
	if m.remoteControl {
		t.Errorf("m.remoteControl became true under ADC rejection; want false (no state change)")
	}
}

// TestRemoteControl_AllowedUnderOAuth confirms the OAuth happy path
// still works. With AGY_ADC_AUTH unset the helper returns nil (no
// gate), so the runRoot flow falls through to the normal RC startup
// path. Pinned so a future "fail-closed when ADC state is unknown"
// regression surfaces here.
func TestRemoteControl_AllowedUnderOAuth(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "") // explicit OAuth path — env unset

	// 1. Flag gate: --remote-control with no ADC env → no error.
	fs := buildRemoteControlFlagSet(t, []string{"--remote-control"})
	if err := checkRemoteControlADC(fs); err != nil {
		t.Fatalf("checkRemoteControlADC returned %v under OAuth; want nil", err)
	}

	// 2. Slash command handler under OAuth: the cycle-106 contract
	//    ("/remote-control" bare = "Remote control on for this
	//    session.") must still fire. We don't assert the exact
	//    transcript — that's what TestRemoteControlFeedback in
	//    `cmd/wagyu/slash_actions_test.go` already pins at the
	//    feedback-helper level. Here we only check that
	//    m.remoteControl flips true (the state change), so a future
	//    refactor that mistakenly always rejects has no chance to slip
	//    through unnoticed.
	m := slashRemoteControlRunMode(t)
	handled, _ := m.handleSlashCommand("/remote-control")
	if !handled {
		t.Fatal(`/remote-control was not handled under OAuth; want handled=true`)
	}
	if !m.remoteControl {
		t.Errorf("m.remoteControl = false after /remote-control under OAuth; want true")
	}
}

// ---- 3. picker filter --------------------------------------------------------

// TestRemoteControl_HiddenUnderADCInSlashMenu drives the
// tuilib.PickerState with the same SetHiddenCommands call
// cmd/wagyu.tui_run.go's NewRunMode uses when ADC is in effect. The
// picker starts on a bare "/" buffer; no row whose Name equals
// "/remote-control" may appear in the result. The substring filter
// still runs, so typing "/remote" under ADC must produce the picker-
// empty overlay (no rows, "No matches" hint) — pinned via a second
// sub-test.
func TestRemoteControl_HiddenUnderADCInSlashMenu(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "1")

	// Hide RC just like NewRunMode does.
	hidden := pickerHiddenCommands()
	if len(hidden) == 0 {
		t.Fatal("pickerHiddenCommands returned nil under ADC; want at least /remote-control")
	}

	t.Run("bare_slash_excludes_remote_control", func(t *testing.T) {
		p := tuilib.PickerState{}
		p.SetHiddenCommands(hidden...)
		p.Update([]rune{'/'})

		if !p.Visible() {
			t.Fatal("picker should be visible on bare '/'")
		}
		if p.Empty() {
			// Possible if all 95 default + custom entries were also
			// hidden, which we don't expect. Surface a useful message
			// rather than a generic diff.
			t.Fatalf("picker is empty on bare '/' — that means the cycle 264 hidden set suppressed every entry, which is a regression")
		}
		for i := 0; i < p.Len(); i++ {
			row := p.SelectedAt(i)
			if row.Name == "/remote-control" {
				t.Errorf("row %d is /remote-control; picker must hide it under ADC", i)
			}
		}
	})

	t.Run("slash_remote_substring_filter_has_no_match", func(t *testing.T) {
		p := tuilib.PickerState{}
		p.SetHiddenCommands(hidden...)
		p.Update([]rune{'/', 'r', 'e', 'm', 'o', 't', 'e'})

		if !p.Visible() {
			t.Fatal("picker should be visible on '/remote'")
		}
		if !p.Empty() {
			t.Errorf("picker should be empty on '/remote' under ADC; got %d rows (first: %q)",
				p.Len(), p.Selected().Name)
		}
	})
}

// TestRemoteControl_VisibleUnderOAuthInSlashMenu is the mirror of the
// previous test under OAuth: the picker has no hidden entries, so
// /remote-control appears in the bare-slash menu and the "/remote"
// substring filter returns exactly the row the v1.3.5 binary shows
// at the same offset. Pinned so a future "set hidden by default"
// regression surfaces here.
func TestRemoteControl_VisibleUnderOAuthInSlashMenu(t *testing.T) {
	t.Setenv("AGY_ADC_AUTH", "")

	hidden := pickerHiddenCommands()
	if len(hidden) != 0 {
		t.Fatalf("pickerHiddenCommands returned %v under OAuth; want nil", hidden)
	}

	p := tuilib.PickerState{}
	// Explicitly clear the hidden set so a future refactor that
	// leaves stale entries around does not change the result.
	p.SetHiddenCommands()
	p.Update([]rune{'/'})

	found := -1
	for i := 0; i < p.Len(); i++ {
		if p.SelectedAt(i).Name == "/remote-control" {
			found = i
			break
		}
	}
	if found < 0 {
		t.Fatalf("/remote-control missing from bare-slash picker under OAuth (rows: %d)", p.Len())
	}

	// /remote substring filter — by the v1.4.0 catalog only
	// "/remote-control" contains the substring (no /remote-backup or
	// /remote-help siblings exist today).
	p2 := tuilib.PickerState{}
	p2.Update([]rune{'/', 'r', 'e', 'm', 'o', 't', 'e'})
	if p2.Empty() {
		t.Fatal("picker empty on '/remote' under OAuth; want exactly one row")
	}
	if p2.Len() != 1 || p2.Selected().Name != "/remote-control" {
		t.Errorf("/remote filter produced %d rows (%q first); want exactly 1 row '/remote-control'",
			p2.Len(), p2.Selected().Name)
	}
}

// ---- 4. env var detection ----------------------------------------------------

// TestRemoteControl_ADCDetectionViaEnv pins the env-var pivot:
// `t.Setenv("AGY_ADC_AUTH", "1")` flips IsBusinessAccount to true,
// unsetting it flips it back to false. The reference binary checks
// the same env var with the same semantics for its own adcAuth branch
// (see cmd/wagyu/business_account.go docstring), and the gate here
// reads it via that helper.
//
// Without this contract the gate could be defeated by a stale
// t.Setenv from a sibling test (the previous test in this file calls
// `t.Setenv("AGY_ADC_AUTH", "")` on the OAuth side), which is the
// reason the other tests in this file are written to set the env at
// the top of every test function.
func TestRemoteControl_ADCDetectionViaEnv(t *testing.T) {
	// OAuth baseline.
	t.Setenv("AGY_ADC_AUTH", "")
	if IsBusinessAccount() {
		t.Errorf("IsBusinessAccount() = true with AGY_ADC_AUTH=\"\"; want false")
	}

	// The reference treats every non-empty trimmed value as truthy.
	// Pin a handful of representative values so the trimmed-truthy
	// contract is documented in the test and not only in the helper
	// docstring.
	for _, val := range []string{"1", "true", "TRUE", "yes", " adc-from-gcloud-cli "} {
		t.Run("set="+val, func(t *testing.T) {
			t.Setenv("AGY_ADC_AUTH", val)
			if !IsBusinessAccount() {
				t.Errorf("IsBusinessAccount() = false with AGY_ADC_AUTH=%q; want true", val)
			}
		})
	}

	// Whitespace-only env is treated as unset (matching the helper).
	t.Run("whitespace_only", func(t *testing.T) {
		t.Setenv("AGY_ADC_AUTH", "   ")
		if IsBusinessAccount() {
			t.Error("IsBusinessAccount() = true with whitespace-only AGY_ADC_AUTH; want false")
		}
	})
}

// ---- helpers -----------------------------------------------------------------

// buildRemoteControlFlagSet parses `args` against the same standard
// flag set the binary ships so the test exercises the real flag
// path the operator hits on the wire, not a hand-rolled subset that
// could mask a `--remote-control` registration regression.
//
// A local copy of the helper is intentional: the cycle-264
// test file should stand on its own — readers reviewing the
// per-cycle commit should not need to grep
// remote_control_adc_test.go just to confirm `remote-control` is
// registered in the standard flag set.
func buildRemoteControlFlagSet(t *testing.T, args []string) *flag.FlagSet {
	t.Helper()
	p := cli.NewParser(cli.StandardFlags(), cli.StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	if code := p.Root().Parse(args); code != 0 {
		t.Fatalf("Parse(%v) = %d; want 0", args, code)
	}
	return p.Root().FlagSet()
}
