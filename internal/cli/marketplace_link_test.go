// marketplace_link_test.go — cycle 251: AGY 1.4.x skill-marketplace-link
// helper.
//
// Each table row pins one of the gating branches the panel relies on:
//
//   - "skill folder present, no plugin"          → URL returned
//   - "skill folder present, plugin supplies"   → suppressed (no URL)
//   - "no skill folder"                          → suppressed
//   - "URL escape"                               → special chars encoded
//   - "OSC 8 supported"                          → frame emitted
//   - "OSC 8 not supported"                      → plain-text fallback
//   - "label customisable"                       → custom label wins
//
// The URL-emitter tests are independent of the filesystem: the OS-8
// switch is the only knob EmitMarketplaceLink consults at runtime, so a
// table test of {url,label,supportsOSC8} rows fully pins the byte
// shape of the OSC 8 frame the cycle ships.
package cli

import (
	"github.com/justwasm/pigasus/endpoints"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// marketplaceLinkForSkillInRoute exposes the testable inner so each test
// gets a self-contained set of (skillRoot, pluginRoots) directories
// driven from t.TempDir() instead of the host's real $ANTIGRAVITY_HOME.
// The exported MarketplaceLinkForSkill wrapper passes the production
// roots in; the tests use the inner directly so they don't have to fight
// $ANTIGRAVITY_HOME every time.
func marketplaceLinkForSkillInRoute(name, skillRoot string, pluginRoots []string) string {
	return marketplaceLinkForSkillIn(name, skillRoot, pluginRoots)
}

// fakePluginRoot prepares <parent>/plugins/<plugin>/skills/ and returns
// the <plugin>/skills subdirectory path the helper expects. Pass an
// empty pluginName for an unpopulated root.
func fakePluginRoot(t *testing.T, parent string, pluginName string) string {
	t.Helper()
	root := filepath.Join(parent, "plugins", pluginName, skillFolderName)
	if err := os.MkdirAll(root, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", root, err)
	}
	return root
}

// TestMarketplaceLinkForSkill_SkillPresentNoPlugin: the happy path the
// feature is here for — the user has a skill folder, no plugin supplies
// it, so the helper returns the marketplace URL.
func TestMarketplaceLinkForSkill_SkillPresentNoPlugin(t *testing.T) {
	home := t.TempDir()
	skillRoot := filepath.Join(home, skillFolderName)
	if err := os.MkdirAll(filepath.Join(skillRoot, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir alpha skill: %v", err)
	}
	got := marketplaceLinkForSkillInRoute("alpha", skillRoot, nil)
	want := endpoints.Current().Marketplace + "alpha"
	if got != want {
		t.Errorf("MarketplaceLinkForSkill(alpha) = %q, want %q", got, want)
	}
}

// TestMarketplaceLinkForSkill_SkillPresentPluginReferencesIt: the
// suppression branch — even with the skill folder present, an installed
// plugin that ships the same name wins and the link is hidden.
func TestMarketplaceLinkForSkill_SkillPresentPluginReferencesIt(t *testing.T) {
	home := t.TempDir()
	skillRoot := filepath.Join(home, skillFolderName)
	if err := os.MkdirAll(filepath.Join(skillRoot, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir alpha skill: %v", err)
	}
	pluginHome := t.TempDir()
	pluginRoot := fakePluginRoot(t, pluginHome, "chrome-devtools-plugin")
	if err := os.MkdirAll(filepath.Join(pluginRoot, "alpha"), 0o755); err != nil {
		t.Fatalf("mkdir alpha in plugin: %v", err)
	}
	got := marketplaceLinkForSkillInRoute("alpha", skillRoot, []string{pluginRoot})
	if got != "" {
		t.Errorf("MarketplaceLinkForSkill(alpha) = %q, want \"\" (plugin supplies alpha)", got)
	}
}

// TestMarketplaceLinkForSkill_NoSkillFolder: the suppression branch on
// the directory side — without a local skill folder there is nothing
// to link to.
func TestMarketplaceLinkForSkill_NoSkillFolder(t *testing.T) {
	home := t.TempDir()
	skillRoot := filepath.Join(home, skillFolderName)
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatalf("mkdir skillRoot: %v", err)
	}
	pluginHome := t.TempDir()
	pluginRoot := fakePluginRoot(t, pluginHome, "chrome-devtools-plugin")
	got := marketplaceLinkForSkillInRoute("alpha", skillRoot, []string{pluginRoot})
	if got != "" {
		t.Errorf("MarketplaceLinkForSkill(no skill folder) = %q, want \"\"", got)
	}
}

// TestMarketplaceLinkForSkill_URLEncoded: a skill name with spaces must
// URL-encode the space as "%20" (not "+") so the marketplace URL
// matches the grammar its search server expects. The encoding itself
// goes through net/url.QueryEscape (the hard-pin for this cycle); the
// "+" → "%20" normalising step happens in buildMarketplaceURL so the
// URL the marketplace server sees is RFC-3986 path-segment style
// rather than application/x-www-form-urlencoded form style. Both
// round-trip through the server, but %20 is the canonical shape and
// what this test pins.
func TestMarketplaceLinkForSkill_URLEncoded(t *testing.T) {
	home := t.TempDir()
	skillRoot := filepath.Join(home, skillFolderName)
	if err := os.MkdirAll(filepath.Join(skillRoot, "My Skill"), 0o755); err != nil {
		t.Fatalf("mkdir \"My Skill\": %v", err)
	}
	got := marketplaceLinkForSkillInRoute("My Skill", skillRoot, nil)
	want := endpoints.Current().Marketplace + "My%20Skill"
	if got != want {
		t.Errorf("MarketplaceLinkForSkill(\"My Skill\") = %q, want %q", got, want)
	}
}

// TestMarketplaceLinkForSkill_SpecialCharsEscaped: the third escape
// branch — a skill name containing &, ?, and = (chars that would
// otherwise collide with the marketplace URL grammar) is encoded
// by url.QueryEscape into the %XX form so the marketplace server
// doesn't split the query at the first delimiter.
//
// "?q=a%26b%3Fc%3Dd" appears verbatim on the marketplace server when
// the user searches for "a&b?c=d". The pinned shape matches what
// the v1.4.x release-note example uses.
func TestMarketplaceLinkForSkill_SpecialCharsEscaped(t *testing.T) {
	home := t.TempDir()
	skillRoot := filepath.Join(home, skillFolderName)
	if err := os.MkdirAll(filepath.Join(skillRoot, "a&b?c=d"), 0o755); err != nil {
		t.Fatalf("mkdir weird-name: %v", err)
	}
	got := marketplaceLinkForSkillInRoute("a&b?c=d", skillRoot, nil)
	want := endpoints.Current().Marketplace + "a%26b%3Fc%3Dd"
	if got != want {
		t.Errorf("MarketplaceLinkForSkill(\"a&b?c=d\") = %q, want %q", got, want)
	}
}

// TestMarketplaceLinkForSkill_FileNotDirectory: a file with the same
// name as a skill is *not* a skill folder — the helper requires a
// real directory.
func TestMarketplaceLinkForSkill_FileNotDirectory(t *testing.T) {
	home := t.TempDir()
	skillRoot := filepath.Join(home, skillFolderName)
	if err := os.MkdirAll(skillRoot, 0o755); err != nil {
		t.Fatalf("mkdir skillRoot: %v", err)
	}
	if err := os.WriteFile(filepath.Join(skillRoot, "alpha"), []byte("not a folder"), 0o644); err != nil {
		t.Fatalf("write alpha file: %v", err)
	}
	got := marketplaceLinkForSkillInRoute("alpha", skillRoot, nil)
	if got != "" {
		t.Errorf("MarketplaceLinkForSkill(alpha) = %q, want \"\" (not a directory)", got)
	}
}

// TestEmitMarketplaceLink_OSC8Supported: with termSupportsOSC8=true the
// output is wrapped in the ESC ] 8 ; ; URL ST framing, the label
// appears between the open and close sequences, and the URL is
// embedded verbatim in the opener.
func TestEmitMarketplaceLink_OSC8Supported(t *testing.T) {
	out := EmitMarketplaceLink("https://marketplace.antigravity.com/skills?q=alpha", "Marketplace", true)
	const oscOpen = "\x1b]8;;"
	const st = "\x1b\\"
	if !strings.HasPrefix(out, oscOpen) {
		t.Fatalf("output does not start with OSC 8 opener: %q", out)
	}
	if !strings.HasSuffix(out, oscOpen+st) {
		t.Fatalf("output does not end with OSC 8 closer: %q", out)
	}
	if !strings.Contains(out, "https://marketplace.antigravity.com/skills?q=alpha") {
		t.Errorf("output does not contain the URL verbatim: %q", out)
	}
	if !strings.Contains(out, "Marketplace") {
		t.Errorf("output does not contain the label verbatim: %q", out)
	}
	// The byte-level shape is also pinned — the rendered string is
	// the opener, the URL, the ST, the label, the opener, and the ST,
	// in that exact order. A regression that reorders the segments
	// would still be "OSC 8" but the terminals wouldn't know which
	// half to treat as the link URL.
	want := oscOpen + "https://marketplace.antigravity.com/skills?q=alpha" + st +
		"Marketplace" + oscOpen + st
	if out != want {
		t.Errorf("shape mismatch:\n got = %q\nwant = %q", out, want)
	}
}

// TestEmitMarketplaceLink_OSC8NotSupported: with termSupportsOSC8=false
// the helper falls back to "<label> (<url>)" — no escape sequences.
func TestEmitMarketplaceLink_OSC8NotSupported(t *testing.T) {
	out := EmitMarketplaceLink("https://marketplace.antigravity.com/skills?q=alpha", "Marketplace", false)
	want := "Marketplace (https://marketplace.antigravity.com/skills?q=alpha)"
	if out != want {
		t.Errorf("plain-text fallback shape:\n got = %q\nwant = %q", out, want)
	}
	if strings.Contains(out, "\x1b") {
		t.Errorf("plain-text fallback must contain no ESC characters: %q", out)
	}
}

// TestEmitMarketplaceLink_LabelCustomizable: passing a non-empty
// custom label replaces the default "Marketplace" label everywhere
// it appears.
func TestEmitMarketplaceLink_LabelCustomizable(t *testing.T) {
	const customLabel = "Find on the marketplace"
	out := EmitMarketplaceLink("https://marketplace.antigravity.com/skills?q=alpha", customLabel, false)
	if !strings.Contains(out, customLabel) {
		t.Errorf("output does not contain the custom label: %q", out)
	}
	if strings.Contains(out, "Marketplace (") {
		// The substring "Marketplace (" is the no-label fallback;
		// ensure the custom label took precedence.
		t.Errorf("output still contains the default 'Marketplace' fallback shape: %q", out)
	}
	want := customLabel + " (https://marketplace.antigravity.com/skills?q=alpha)"
	if out != want {
		t.Errorf("custom-label shape:\n got = %q\nwant = %q", out, want)
	}
}

// TestEmitMarketplaceLink_EmptyURL: url == "" passes through empty —
// emitting an OSC 8 frame around nothing produces two ESC ]8;;ST
// sequences with nothing between them, which the terminal would
// either silently discard or render as a mysterious escape block. The
// panel always checks the URL is non-empty before calling the helper
// (the helper itself returns "" as a defensive measure).
func TestEmitMarketplaceLink_EmptyURL(t *testing.T) {
	if got := EmitMarketplaceLink("", "Marketplace", true); got != "" {
		t.Errorf("empty URL must produce empty output, got %q", got)
	}
	if got := EmitMarketplaceLink("", "Marketplace", false); got != "" {
		t.Errorf("empty URL must produce empty output, got %q", got)
	}
}
