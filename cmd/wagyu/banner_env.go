// banner_env.go — cycle 255: the AGY 1.1.19 banner env-var gate.
//
// AGY 1.1.19 release note (`internal/cli/testdata/changelog_reference.txt:443`):
//
//	"Added the AGY_CLI_HIDE_LOGO environment variable for narrow
//	 terminals, screen readers and recordings where the banner's logo
//	 art gets in the way; setting it suppresses the art while keeping
//	 the version and account lines."
//
// Root cause: the wagyu startup banner has a 5-row layout — a textual
// version line ("Antigravity CLI <ver>"), an email/account row, a
// model row, a cwd row, and a 24-bit RGB ASCII-art "logo" row drawn
// from unicode block characters. The art row is decorative: it does
// not carry information the user cannot already infer (the version is
// also on the version row, the account on the email row, etc.), and
// on narrow terminals / screen readers / recordings the wide block
// characters get in the way. The cycle adds the AGY_CLI_HIDE_LOGO
// env var so the user can suppress only the art row, leaving the
// textual version + account lines untouched.
//
// parseBoolish is the small helper the cycle ships for the env-var
// parser. It is intentionally narrower than strconv.ParseBool: the
// AGY changelog accepts only "1", "true", "yes" (case-insensitive)
// — "on", "enable", "y", and the rest are *not* part of the spec
// and parseBoolish returns false for them, falling through to the
// default (logo rendered). Misconfiguration therefore degrades to
// the user-visible default rather than to "the env var set, but
// nothing changed" — easier to debug than a silent no-op.
//
// The helper lives in cmd/wagyu (not internal/) because the env var
// only gates the TUI surface — print mode does not render the
// banner — and putting the helper next to its sole caller keeps the
// blast radius small.
package main

import (
	"os"
	"strings"
)

// envHideLogo is the AGY 1.1.19 banner-art-suppression env var.
// Kept as a named constant so the test file and the production reader
// cannot drift on the spelling.
const envHideLogo = "AGY_CLI_HIDE_LOGO"

// parseBoolish reports whether s is one of the truthy spellings the
// AGY 1.1.19 changelog accepts for AGY_CLI_HIDE_LOGO: "1", "true",
// "yes" — case-insensitive, whitespace-trimmed. Everything else
// (empty string, "0", "false", "no", "on", "garbage", "y", ...) is
// treated as falsy so misconfiguration degrades to "render the logo"
// (the default) instead of silently turning off a row the user did
// not intend to hide.
//
// parseBoolish is intentionally narrower than strconv.ParseBool:
// strconv.ParseBool also accepts "t", "f", "True", "TRUE", "False",
// and so on, but the AGY changelog is explicit ("setting it
// suppresses the art") and uses "1", "true", "yes" as the example
// values. We honour exactly that set so the helper's behaviour is
// pinned by the spec, not by Go's stdlib quirks.
func parseBoolish(s string) bool {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case "1", "true", "yes":
		return true
	}
	return false
}

// envHideLogoOn reports whether AGY_CLI_HIDE_LOGO is set to a
// truthy value (per parseBoolish). The function reads os.Getenv at
// call time so a runtime mutation via t.Setenv is observable without
// re-importing the package — that is how the test file drives every
// subtest case. An unset env var returns false, matching the
// default-behaviour contract: the banner art renders unless the user
// asks for it to be hidden.
func envHideLogoOn() bool {
	return parseBoolish(os.Getenv(envHideLogo))
}
