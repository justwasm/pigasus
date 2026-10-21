package main

// AGY 1.4.1 changelog line 8:
//
//	"Fixed `Enable Telemetry` in `/config` remaining editable for Business
//	 accounts (such as Google Cloud project and Application Default
//	 Credentials sign-in via `AGY_ADC_AUTH`), where prompts, content, and
//	 model responses are never collected: the row now shows
//	 `off (disabled)` and explains why, and first-run setup with
//	 `AGY_ADC_AUTH` now shows the Google Cloud Platform Terms of Service
//	 without the consumer telemetry opt-in checkbox."
//
// Both the `--remote-control` × ADC gate (1.3.3) and the `/config` "Enable
// Telemetry" lock (1.4.1) answer the same question — "is the user signed in
// via ADC / Business?" — by inspecting the same env-var the reference itself
// honours. Centralising the helper here keeps the two call sites in sync so
// the new panel behaviour cannot drift from the long-standing remote-control
// gate.
//
// Cycle 241 lifted the function out of `remote_control_adc.go` (where it
// lived as the package-private `isADCEnabled`) and renamed it to
// `IsBusinessAccount`. The rename is intentional: `isADCEnabled` described
// *how the signal is read*; `IsBusinessAccount` describes *what the signal
// means* — the same env var (`AGY_ADC_AUTH`, set non-empty) is what AGY
// uses to switch into the "Business account" branch of both the auth flow
// and the telemetry policy.

import (
	"os"
	"strings"
)

// telemetryProductName is the binary's product name as it appears in the
// reference's /config panel description for the "Enable Telemetry" row:
// `When toggled on, Antigravity CLI collects usage data to help Google
// enhance performance and features` (recovered verbatim from `strings
// ~/.local/bin/agy | grep`). The Business-account variant of the same row
// (1.4.1) uses the same product name in the position the binary formats
// with `%s`:
//
//	Disabled for Business accounts. Antigravity CLI does not collect any of
//	your prompts, content, or model responses, but will still collect
//	general usage patterns.
//
// Both are pinned as strings here so the cycle-241 regression test can
// cross-check them without a string-table round-trip.
const telemetryProductName = "Pigasus CLI"

// IsBusinessAccount reports whether the current shell looks like a Business
// sign-in to the recovered CLI. The recovered signal is the env var the
// reference itself honours: `AGY_ADC_AUTH`. When that env var is set the
// reference walks the adcAuth branch on first auth and (per 1.4.1) the
// `Enable Telemetry` row in `/config` becomes non-editable. When it is
// unset the reference falls through to keyring/oAuth/refresh and the row
// behaves as for a personal account.
//
// Any non-empty trimmed value counts: `AGY_ADC_AUTH=true`, `=1`, and
// `=some-value` all select the Business branch, mirroring how
// `remote_control_adc.go` interprets the same variable.
func IsBusinessAccount() bool {
	return strings.TrimSpace(os.Getenv("AGY_ADC_AUTH")) != ""
}

// telemetryDisabledValue is the exact literal the 1.4.1 reference paints in
// the value column for the `Enable Telemetry` row when the user is signed
// in via a Business account. Recovered from the 1.4.1 release-notes line:
// "the row now shows `off (disabled)` and explains why". The literal is
// not present verbatim elsewhere in the binary (it is only present inside
// the changelog string), so it is pinned here as the contract for the
// clone's panel.
const telemetryDisabledValue = "off (disabled)"

// telemetryDisabledResponse is the message the clone prints in the
// transcript when the operator tries to ←/→ the locked `Enable Telemetry`
// row. The reference binary does not appear to print a dedicated line for
// the failed toggle (the panel's row value does not change; the user can
// infer the lock from the dim grey "off (disabled)" value and the
// explanation in the description slot); this clone-side message is the
// clearest, most visible way to surface the lock and matches the
// `/command` "Exited /cmd command" / `⎿` shape other panels emit when
// rejecting user input. A binary-recovery pass before a future release
// should tighten this to whatever AGY actually prints.
const telemetryDisabledResponse = "Telemetry is disabled for your account type."

// telemetryBusinessDescription is the description text the reference
// paints under the row when the user is signed in via a Business account,
// recovered verbatim from `strings ~/.local/bin/agy | grep`:
//
//	Disabled for Business accounts. Antigravity CLI does not collect any of
//	your prompts, content, or model responses, but will still collect
//	general usage patterns.
//
// The clone pins the same text (with `Antigravity CLI` substituted for the
// binary's `%s` placeholder) so the /config row reads the same way the
// recovered binary does.
func telemetryBusinessDescription() string {
	return "Disabled for Business accounts. " + telemetryProductName +
		" does not collect any of your prompts, content, or model responses," +
		" but will still collect general usage patterns."
}

// telemetryRowName is the literal name of the /config row this cycle
// locks. Pinned as a constant so the helpers below do not typo the
// catalogue entry.
const telemetryRowName = "Enable Telemetry"

// isTelemetryRow reports whether a given configRow is the `Enable
// Telemetry` row. Centralised so the display and toggle paths agree.
func isTelemetryRow(row configRow) bool {
	return row.name == telemetryRowName
}