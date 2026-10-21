// banner_test.go — cycle 255: pin the AGY_CLI_HIDE_LOGO env-var gate.
//
// The AGY 1.1.19 changelog (recovered at
// `internal/cli/testdata/changelog_reference.txt:443`) adds an
// AGY_CLI_HIDE_LOGO env var so the user can suppress the
// decorative 24-bit-RGB ASCII-art row of the startup banner on
// narrow terminals, screen readers and recordings. The cycle ships
// three production surfaces — the env-var constant, the
// `parseBoolish` parser, and the `envHideLogoOn` reader — all in
// `cmd/wagyu/banner_env.go`. This test file pins every behaviour
// the changelog advertises, plus the default fallback, plus the
// "garbage value degrades to default" rule.
//
// The tests drive `renderBanner` directly (no runMode / bubbletea
// dependency) because the function is the only place the gate
// fires. ANSI escapes are stripped via the existing ansi.Strip
// helper (`github.com/charmbracelet/x/ansi` — already in the test
// dependency graph; `cmd/wagyu/picker_view_test.go:8,21` uses it
// the same way) so the byte-for-byte check on the art row uses
// just the visible glyph stream rather than the entire 24-bit-RGB
// escape sequence.
//
// Six groups:
//
//   - parseBoolish table (15 spellings × 2 outcomes): the parser.
//   - TestBanner_LogoShownByDefault:        unset → art present.
//   - TestBanner_LogoHiddenOnHideLogo1:     "1"     → art absent.
//   - TestBanner_LogoHiddenOnHideLogoTrue:  "true"  → art absent.
//   - TestBanner_LogoHiddenOnHideLogoYes:   "yes"   → art absent.
//   - TestBanner_LogoHiddenOnHideLogoUppercase:
//     "TRUE"  → art absent (case-insensitive).
//   - TestBanner_VersionAndAccountStillShown: "1"     → "Pigasus CLI"
//     text AND email line are still rendered.
//   - TestBanner_LogoShownOnGarbageValue:   "garbage" → art present.
//
// All env-var subtests use t.Setenv so the surrounding test process
// is left untouched — Go restores the original value on test exit.
package main

import (
	"bytes"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/branding"
	"github.com/charmbracelet/x/ansi"
)

// renderBannerForTest is a thin wrapper around renderBanner that
// returns the captured bytes as a string. The two callers in
// cmd/wagyu (main.go:1638 for `--tui`, tui_viewport.go:305 for the
// scrolling header) both pass an io.Writer; this helper pins the
// byte shape of the banner in the test environment.
func renderBannerForTest(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	renderBanner(&b, "u@example.com", "Gemini 3.8 Flash (High)", "Antigravity Starter Quota", "/Users/gear/Documents/GitHub/openagy")
	return b.String()
}

// bannerHasArtRow reports whether the banner output contains the
// 24-bit-RGB unicode-block-character art row. We strip ANSI escapes
// first because the art row's distinguishing feature is the wide
// block characters (▄ / ▀) and their 24-bit-RGB colour frame —
// without stripping we would need to pin the entire escape sequence
// byte-for-byte, which would make the test brittle to a future
// re-tint of the colour palette.
//
// The art row is the only banner line whose visible content is
// pure decorative block characters; the textual rows
// (Pigasus CLI ver, email, model, cwd) all carry readable
// ASCII. We use the unicode block character "▀" as the marker —
// it appears only in the art row, never in the textual rows.
func bannerHasArtRow(banner string) bool {
	plain := ansi.Strip(banner)
	for _, line := range strings.Split(plain, "\n") {
		trimmed := strings.TrimSpace(line)
		// The art row has the form "▄▀▀     ▀▀▄" — entirely block
		// characters and whitespace. The textual rows always contain
		// at least one readable ASCII glyph from
		// "Pigasus CLI" / "<email>" / "<model>" / "<cwd>".
		// We assert on the absence of any readable glyph AND the
		// presence of at least one block character, which together
		// uniquely identifies the art row.
		if trimmed == "" {
			continue
		}
		hasBlock := strings.ContainsAny(trimmed, "▀▄")
		if !hasBlock {
			continue
		}
		readable := false
		for _, r := range trimmed {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				readable = true
				break
			}
		}
		if !readable {
			return true
		}
	}
	return false
}

// bannerHasText pins that a given readable substring (after ANSI
// strip) appears in the banner. Used by the
// VersionAndAccountStillShown test to assert that the textual rows
// survive the gate.
func bannerHasText(banner, want string) bool {
	return strings.Contains(ansi.Strip(banner), want)
}

// TestParseBoolish pins the parser the cycle ships. The table
// covers every spelling the AGY 1.1.19 changelog accepts (1, true,
// yes — case-insensitive), the negative spellings the changelog
// explicitly does NOT list (0, false, no, on — all should fall
// through to false), the empty string, a "garbage" unknown, and a
// whitespace-padded truthy to confirm the TrimSpace path. The
// default-degradation property (truthy → true, anything else →
// false) is asserted on every row.
func TestParseBoolish(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		// changelog-accepted truthy spellings (case-insensitive)
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"yes", true},
		{"YES", true},
		{"Yes", true},
		// whitespace tolerance (changelog says "case-insensitive";
		// the cycle's parser trims first, so "  yes  " works too —
		// matches envOSC7501* helpers' behaviour in
		// cmd/wagyu/program_status.go:107-128)
		{"  yes  ", true},
		{"\t1\n", true},
		// not in the changelog — must NOT suppress
		{"", false},
		{"garbage", false},
		{"0", false},
		{"false", false},
		{"False", false},
		{"FALSE", false},
		{"no", false},
		{"NO", false},
		{"on", false}, // strconv.ParseBool accepts this; we don't.
		{"y", false},  // single-letter; the changelog says "yes"
		{"enable", false},
		// whitespace-only — must NOT suppress (a " " env var is not
		// the user setting the flag, it's shell noise)
		{"   ", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := parseBoolish(c.in); got != c.want {
				t.Errorf("parseBoolish(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestBanner_LogoShownByDefault pins the no-env-var baseline:
// when AGY_CLI_HIDE_LOGO is unset, the art row is rendered. This
// is the regression sentinel for the cycle: a regression that
// flipped the default to "hidden" would produce a banner missing
// the art row on every existing user's machine.
func TestBanner_LogoShownByDefault(t *testing.T) {
	t.Setenv(envHideLogo, "")
	got := renderBannerForTest(t)
	if !bannerHasArtRow(got) {
		t.Errorf("AGY_CLI_HIDE_LOGO unset: art row should be present, banner = %q", got)
	}
}

// TestBanner_LogoHiddenOnHideLogo1 covers the most idiomatic
// "boolean env var" spelling. Setting =1 is what shell users
// reach for when they want a quick toggle.
func TestBanner_LogoHiddenOnHideLogo1(t *testing.T) {
	t.Setenv(envHideLogo, "1")
	got := renderBannerForTest(t)
	if bannerHasArtRow(got) {
		t.Errorf("AGY_CLI_HIDE_LOGO=1: art row should be suppressed, banner = %q", got)
	}
}

// TestBanner_LogoHiddenOnHideLogoTrue covers the explicit boolean
// spelling. Pinning "true" (vs "1") keeps the parser honest: a
// regression that only matched "1" would still trip the changelog
// spec, which lists both.
func TestBanner_LogoHiddenOnHideLogoTrue(t *testing.T) {
	t.Setenv(envHideLogo, "true")
	got := renderBannerForTest(t)
	if bannerHasArtRow(got) {
		t.Errorf("AGY_CLI_HIDE_LOGO=true: art row should be suppressed, banner = %q", got)
	}
}

// TestBanner_LogoHiddenOnHideLogoYes covers the third truthy
// spelling. "yes" is what users reach for when they have an
// /etc/environment-style config file that aliases "true" to a
// different string.
func TestBanner_LogoHiddenOnHideLogoYes(t *testing.T) {
	t.Setenv(envHideLogo, "yes")
	got := renderBannerForTest(t)
	if bannerHasArtRow(got) {
		t.Errorf("AGY_CLI_HIDE_LOGO=yes: art row should be suppressed, banner = %q", got)
	}
}

// TestBanner_LogoHiddenOnHideLogoUppercase pins the case-
// insensitive axis. A regression that dropped ToLower would only
// match the lowercase spellings and silently leave the art on
// when a user (or their .zshenv) writes AGY_CLI_HIDE_LOGO=TRUE.
func TestBanner_LogoHiddenOnHideLogoUppercase(t *testing.T) {
	t.Setenv(envHideLogo, "TRUE")
	got := renderBannerForTest(t)
	if bannerHasArtRow(got) {
		t.Errorf("AGY_CLI_HIDE_LOGO=TRUE: art row should be suppressed, banner = %q", got)
	}
}

// TestBanner_VersionAndAccountStillShown pins the second clause of
// the changelog: "keeping the version and account lines". When
// the art row is suppressed, the version line ("Antigravity
// CLI <ver>") AND the email/account row must still appear. The
// cwd row and model row are also kept — they are textual, not art
// — but the changelog only names the first two explicitly. This
// test pins all four textual rows to give the parser a stronger
// contract: the gate suppresses only the art, not anything else.
func TestBanner_VersionAndAccountStillShown(t *testing.T) {
	t.Setenv(envHideLogo, "1")
	got := renderBannerForTest(t)
	if bannerHasArtRow(got) {
		t.Errorf("AGY_CLI_HIDE_LOGO=1: art row should be suppressed, banner = %q", got)
	}
	// Version line (banner row 1): active brand CLI name
	if !bannerHasText(got, branding.Current().CLIName) {
		t.Errorf("AGY_CLI_HIDE_LOGO=1: version line missing, banner = %q", got)
	}
	// Account line (banner row 2): the email address.
	if !bannerHasText(got, "u@example.com") {
		t.Errorf("AGY_CLI_HIDE_LOGO=1: account line missing, banner = %q", got)
	}
	// Model row (banner row 3): the active model name.
	if !bannerHasText(got, "Gemini 3.8 Flash") {
		t.Errorf("AGY_CLI_HIDE_LOGO=1: model row missing, banner = %q", got)
	}
	// Cwd row (banner row 4): the working directory. The
	// shortenCwd helper converts the leading $HOME to "~", so the
	// visible text is "~/Documents/GitHub/openagy".
	if !bannerHasText(got, "~/Documents/GitHub/openagy") {
		t.Errorf("AGY_CLI_HIDE_LOGO=1: cwd row missing, banner = %q", got)
	}
}

// TestBanner_LogoShownOnGarbageValue pins the default-degradation
// rule: an unrecognised value (including "garbage", "0",
// "false", "no", "on", and the empty string) leaves the art row
// rendered. This is the safe default — a typo in the env var
// degrades to "the user sees the same banner as before", not to
// "the user has a quiet prompt and didn't intend to".
func TestBanner_LogoShownOnGarbageValue(t *testing.T) {
	cases := []string{"garbage", "0", "false", "no", "on", "", " "}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			t.Setenv(envHideLogo, v)
			got := renderBannerForTest(t)
			if !bannerHasArtRow(got) {
				t.Errorf("AGY_CLI_HIDE_LOGO=%q: art row should be present (default-degradation), banner = %q",
					v, got)
			}
		})
	}
}

func TestBanner_Branding_Switch(t *testing.T) {
	defer branding.Reset()

	// Pigasus brand (Pigasus CLI)
	branding.SetCurrent(branding.BrandPigasus)
	got := renderBannerForTest(t)
	if !bannerHasText(got, "Pigasus CLI") {
		t.Errorf("expected Pigasus CLI in banner under BrandPigasus, got:\n%s", got)
	}

	// Antigravity brand (Antigravity CLI)
	branding.SetCurrent(branding.BrandAntigravity)
	got = renderBannerForTest(t)
	if !bannerHasText(got, "Antigravity CLI") {
		t.Errorf("expected Antigravity CLI in banner under BrandAntigravity, got:\n%s", got)
	}
}
