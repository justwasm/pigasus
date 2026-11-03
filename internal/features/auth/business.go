package auth

import (
	"os"
	"strings"
)

// TelemetryProductName is the product name used in telemetry descriptions.
const TelemetryProductName = "Pigasus CLI"

// TelemetryDisabledValue is the literal shown in the /config panel for Business accounts.
const TelemetryDisabledValue = "off (disabled)"

// TelemetryDisabledResponse is the response when an operator tries to toggle telemetry on a Business account.
const TelemetryDisabledResponse = "Telemetry is disabled for your account type."

// IsBusinessAccount reports whether the current shell looks like a Business
// sign-in to the recovered CLI. The recovered signal is the env var the
// reference itself honours: `AGY_ADC_AUTH`. When that env var is set the
// reference walks the adcAuth branch on first auth and (per 1.4.1) the
// `Enable Telemetry` row in `/config` becomes non-editable.
func IsBusinessAccount() bool {
	return strings.TrimSpace(os.Getenv("AGY_ADC_AUTH")) != ""
}

// TelemetryBusinessDescription is the description text shown under the row
// when the user is signed in via a Business account.
func TelemetryBusinessDescription() string {
	return "Disabled for Business accounts. " + TelemetryProductName +
		" does not collect any of your prompts, content, or model responses," +
		" but will still collect general usage patterns."
}
