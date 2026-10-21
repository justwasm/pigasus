// Package cli — claude55.go: visibility gate for the Claude 5.5 catalogue
// entries (AGY 1.3.6 changelog line 1).
//
// AGY 1.3.6 ships Claude Opus 5.5 and Claude Sonnet 5.5 to customers
// using Application Default Credentials (ADC) sign-in. The catalogue
// surfaces those models only when the operator's Google Cloud project
// is on the allowlist. The recovered shell has no live gcloud ADC
// chain — the upstream `adcAuth.TryAuth` path is a stub in the
// decompiled binary (out/decompile/decompile-google3_third_party_jetski_
// cli_backend_auth_auth..md) — so the gate stands in for the real
// project check: both `AGY_ADC_AUTH=true` and a non-empty project id
// must be set for the entries to appear.
//
// The function is intentionally independent of cmd/wagyu/isADCEnabled:
// the cycle-ADC gate is "any non-empty value" (matches the 1.3.3
// contract documented in cmd/wagyu/remote_control_adc.go) but the 1.3.6
// catalogue gate is stricter — `AGY_ADC_AUTH=true` literally — so the
// two predicates diverge and stay separate rather than share a helper.
package cli

import (
	"os"
	"strings"
)

// Claude55Available reports whether Claude Opus 5.5 and Claude Sonnet
// 5.5 should appear in the model catalogue.
//
// Returns true iff:
//   - AGY_ADC_AUTH is set to "true" (case-insensitive, trimmed), AND
//   - AGY_CLAUDE55_PROJECT is set to a non-empty project id
//
// Any other value for AGY_ADC_AUTH (empty, "1", "false", "yes", …) or a
// missing project id is treated as "not on the allowlist" and the
// caller must hide both 5.5 entries. The `=1` shortcut from the 1.3.3
// cycle is not accepted here because the 1.3.6 changelog pin is the
// explicit `AGY_ADC_AUTH=true` form — making the 5.5 gate stricter
// than the remote-control gate matches the upstream's own wiring
// (AGY_ADC_AUTH=true is the only value that triggers the Claude 5.5
// rollout path in the recovered binary).
//
// AGY_CLAUDE55_PROJECT doubles as a stand-in for the gcloud ADC
// project's "can use Claude 5.5" check: with the recovered shell
// having no live ADC chain, the env var is the only testable handle
// for the gate. Production callers set it from the actual ADC result
// (e.g. `gcloud config get-value project` once the project is on the
// allowlist); tests set it directly via t.Setenv.
func Claude55Available() bool {
	if !strings.EqualFold(strings.TrimSpace(os.Getenv("AGY_ADC_AUTH")), "true") {
		return false
	}
	return strings.TrimSpace(os.Getenv("AGY_CLAUDE55_PROJECT")) != ""
}
