package main

import (
	"flag"

	"github.com/justwasm/pigasus/internal/cli"
)

// AGY changelog coverage for the --remote-control × ADC interaction:
//
//	1.3.3 line 53:  "Fixed --remote-control silently continuing when ADC
//	                 (Application Default Credentials) auth fails; it
//	                 now exits with a clear error so the user doesn't
//	                 think the daemon is running."
//	1.4.1 line 79:  "Fixed --remote-control failing when you sign in with
//	                 Application Default Credentials (AGY_ADC_AUTH);
//	                 Remote Control doesn't support ADC yet, so the flag
//	                 now exits with a clear error and the /remote-control
//	                 command is hidden."
//
// Cycle 241 renamed the `isADCEnabled` helper to `IsBusinessAccount` and
// moved the implementation into `business_account.go`. Cycle 264 raised
// the existing failure exit code from 1 to 3 (the print-mode AGY_ERROR
// convention captured in `cmd/wagyu/api_error.go:33`), expanded the
// error message to mention the recovery path explicitly, and extended
// the same gate to the in-session `/remote-control` slash command and
// the slash menu picker. The 1.3.3 line 53 surface (one flag, one
// stderr line, one process exit) is preserved; the 1.4.1 line 79
// surface ("the command is hidden") is the new picker filter in
// `internal/tuilib/picker.go`.

// remoteControlEnabled reports whether the operator passed
// `--remote-control` on the command line. We treat `=` syntax (`--remote-
// control=true`) and the bare flag the same way; stdlib's `flag.FlagSet`
// normalises both into a single bool entry.
func remoteControlEnabled(fs *flag.FlagSet) bool {
	return flagBoolValue(fs, "remote-control")
}

// remoteControlADCErrorMessage is the single source of truth for the
// error the user sees when --remote-control collides with ADC. The
// message must mention "remote-control", "Application Default
// Credentials", the env var `AGY_ADC_AUTH`, and the recovery command
// `/logout` so the user can find the failure mode in logs / grep and
// so the tests can pin every key substring of the contract.
//
//   - "remote-control"          grep-able from CI logs
//   - "Application Default Credentials"   the human-readable name
//   - "AGY_ADC_AUTH"            the env var the user can /logout, then unset
//   - "/logout"                 recovery command (AGY 1.4.1 cycle 264 contract)
func remoteControlADCErrorMessage() string {
	return "error: --remote-control cannot be used with Application Default Credentials " +
		"(AGY_ADC_AUTH) sign-in; Remote Control does not support ADC yet — " +
		"sign out with /logout and unset AGY_ADC_AUTH (or use a different sign-in method) and retry."
}

// remoteControlSlashADCErrorMessage is the parallel single-line message
// for the `/remote-control` slash command when ADC is in effect. Same
// grep-able substrings ("remote-control", "Application Default
// Credentials", "AGY_ADC_AUTH", "/logout") as the flag-path message,
// shaped to render cleanly inside the existing `> /remote-control` /
// `⎿ <msg>` slashRespond wrapper (`cmd/wagyu/slash_commands.go:73`).
func remoteControlSlashADCErrorMessage() string {
	return "Remote Control does not yet support Application Default Credentials (AGY_ADC_AUTH) sign-in. " +
		"Sign out with /logout and unset AGY_ADC_AUTH (or use a different sign-in method) and retry."
}

// remoteControlADCExitCode is the exit status the reference uses when a
// flag rejects the active auth surface for "the daemon never started"
// reasons. Cycle 264 raised this from the 1.3.3 value (1) to 3 to align
// with the print-mode AGY_ERROR convention established by cycle 112
// (see `cmd/wagyu/api_error.go:33 headlessAPIExitCode`).
const remoteControlADCExitCode = 3

// checkRemoteControlADC enforces the 1.3.3 + 1.4.1 changelog behaviour:
// when the operator passes --remote-control AND signals ADC via
// $AGY_ADC_AUTH, return *cli.ExitCodeError with the canonical message
// and the AGY_ERROR exit code (3) so the parser prints the message to
// stderr and exits with status 3 — not 1.
//
// Returns nil when:
//   - --remote-control was not given (the existing behaviour is
//     preserved — ADC failures continue with fallback auth, which is
//     the 1.3.0..1.3.2 contract).
//   - --remote-control was given but ADC is not in effect.
//
// Returns *cli.ExitCodeError with code 3 (AGY_ERROR) when both are
// set; the parser loop translates that into the process exit code and
// prints the attached message to stderr.
func checkRemoteControlADC(fs *flag.FlagSet) error {
	if !remoteControlEnabled(fs) {
		return nil
	}
	if !IsBusinessAccount() {
		return nil
	}
	return cli.ExitCodeError(remoteControlADCExitCode, remoteControlADCErrorMessage())
}
