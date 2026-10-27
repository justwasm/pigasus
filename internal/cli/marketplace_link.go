// Package cli: marketplace_link.go — the AGY 1.4.x skill-marketplace-link
// helper.
//
// AGY 1.4.2 release note ("Skill marketplace link helper", recovered from
// the `GetSkillMarketplaceLink` language-server proto method on disk and
// the changelog's "when you have a skill folder but no marketplace.json
// references it" wording): when the user is browsing installed skills and
// one of them *isn't* backed by an installed plugin, the CLI lights up a
// clickable hyperlink to the Antigravity plugin marketplace search page so
// the user can install the matching plugin in one click.
//
// The cycle ships a hardcoded URL pattern. The literal
// "https://marketplace.antigravity.com/skills?q=" prefix is recoverable from
// the v1.4.x changelog and the marketplace preview strings AGY's binary
// ships. If that URL ever changes upstream, the cycle pins the change in
// MarketplaceLinkPattern below — a one-line constant edit, no other code
// needs to change.
//
// Display rules:
//
//   - The helper is gated on a *directory* test (a folder
//     <AntigravityConfigHome()>/skills/<name>/) and on a *plugin-references*
//     test (no entry under ~/.gemini/config/plugins/*/skills/*/SKILL.md
//     uses the same name). Both gates have to pass before a URL is
//     returned — if either fails the helper returns "".
//   - Special characters in the skill name are URL-encoded with
//     url.QueryEscape (spaces become %20, & becomes %26, ? becomes %3F).
//     The marketplace server interprets the encoded form as a single
//     search token.
//   - The OSC 8 hyperlink emitter only fires when the running terminal
//     advertises OSC 8 capability (cycle 2 of the OSC pair in this
//     repo's history; see render.DetectHyperlinkCaps). The
//     fallback is "label (url)" so plain-text agents still see the
//     URL.
//
// Three exports:
//
//   - MarketplaceLinkPattern         — the URL prefix (string constant).
//   - MarketplaceLinkForSkill(name)  — URL or "" depending on the two gates.
//   - EmitMarketplaceLink(...)       — render with OSC 8 when supported,
//     fall back to "label (url)" otherwise.
//
// There is also an internal helper, marketplaceLinkForSkillIn, that takes the
// skill-folder root and the list of plugin roots explicitly so the tests can
// drive every branch without depending on the host's ~/.gemini tree (the
// production call site uses the same helpers the slash-skills panel uses
// already: auth.AntigravityConfigHome + filepath.Join).
package cli

import (
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/auth"
)

// MarketplaceLinkPattern is the URL prefix the marketplace link is built on.
// Recovered from AGY 1.4.x: the binary exposes a `GetSkillMarketplaceLink`
// language-server proto and an `unknown marketplace` validation path that
// imply the same host as the plugin marketplace. The reverse-engineered
// path is the `/skills` search route, which is the marketplace-side
// counterpart of the installed-side `/plugin` Discover tab.
//
// The trailing ?q= is part of the constant so callers can just do
// `MarketplaceLinkPattern + url.QueryEscape(skillName)` to mint a URL. A
// future AGY release that moves this URL only requires editing this
// constant; the rest of the cycle is pattern-agnostic.
//
// It is the compiled-in default. The live prefix is
// endpoints.Current().Marketplace — the configuration file may point the
// marketplace link at a local service.
const MarketplaceLinkPattern = endpoints.DefaultMarketplace

// skillFolderName is the directory the user-installed skills live in, taken
// relative to auth.AntigravityConfigHome(). The same path appears in
// cmd/wagyu/print_slash.go (with the same name and the same parent layout)
// — cycle 251 does *not* call into the slash-globals so this string can
// stay self-contained here.
const skillFolderName = "skills"

// MarketplaceLinkForSkill returns the marketplace URL for skillName, or ""
// when no link should be shown.
//
// The link is suppressed in two cases:
//
//   - The skill folder <AntigravityHome>/skills/<skillName> doesn't exist
//     (the user doesn't have a local install to attach a plugin to).
//   - A plugin already declares the same skill (the file at any
//     <GeminiConfigHome>/plugins/*/skills/<skillName>/SKILL.md supplies
//     it; once a plugin is installed we don't want to nag the user to
//     install another copy).
//
// Skill name matching is exact and case-sensitive — the same convention
// the slash skills panel uses (cycle 199) and the same convention AGY
// uses for plugin.json.name lookups (recovery strings: "plugin.json
// missing name", "invalid plugin name: %q"). The marketplace URL embeds
// the *encoded* form so a skill with spaces or punctuation in its name
// still results in a well-formed URL.
//
// Returns "" when the link should not be shown.
func MarketplaceLinkForSkill(skillName string) string {
	skillRoot := filepath.Join(auth.AntigravityConfigHome(), skillFolderName)
	pluginRoots := pluginSkillRoots()
	return marketplaceLinkForSkillIn(skillName, skillRoot, pluginRoots)
}

// pluginSkillRoots returns the list of plugin-skill directories the
// helper scans when checking for "an installed plugin already supplies
// this skill". Each entry is the path that holds a plugin's `skills/`
// directory; existence of any `<root>/<name>/` directory means the
// plugin supplies it.
//
// The function reads the live plugin directory under
// `~/.gemini/config/plugins/` so a user who installs a fresh plugin
// gets the marketplace link suppressed for that plugin's skills
// without restarting the CLI. A directory that doesn't exist or that
// is unreadable yields an empty list — the caller's "no plugins"
// fallback path then makes the helper always say "show the link".
func pluginSkillRoots() []string {
	root := filepath.Join(auth.GeminiConfigHome(), "plugins")
	entries, err := os.ReadDir(root)
	if err != nil {
		return nil
	}
	var out []string
	for _, e := range entries {
		// The same isDirFollowingLinks test the slash-skills panel
		// uses (cmd/wagyu/print_slash.go) is reimplemented inline
		// here so this file stays self-contained — the panel's
		// helper is in package main and not importable from internal/cli.
		if e.IsDir() {
			out = append(out, filepath.Join(root, e.Name(), skillFolderName))
		}
	}
	return out
}

// marketplaceLinkForSkillIn is the test-friendly inner of MarketplaceLinkForSkill.
// All filesystem checks live here so the exported wrapper has zero
// branches to drive from a test.
func marketplaceLinkForSkillIn(skillName, skillRoot string, pluginRoots []string) string {
	if skillName == "" {
		return ""
	}
	// The skill has to be a *directory* — a stray file with the same
	// name is not a "skill folder" in AGY's sense. IsDir on the entry
	// returns the right answer for a regular file or directory; a
	// symlink to a directory needs `os.Stat`, which the slash-skills
	// panel uses elsewhere.
	st, err := os.Stat(filepath.Join(skillRoot, skillName))
	if err != nil || !st.IsDir() {
		return ""
	}
	// The plugin check: any plugin that ships a <name>/ subdirectory in
	// its skills/ tree wins. We don't bother reading the SKILL.md —
	// the directory test matches the AGY 1.4.x behaviour ("the skill
	// is supplied"), and a plugin that ships an empty skills/<name>/
	// directory would be broken in any case.
	for _, root := range pluginRoots {
		st, err := os.Stat(filepath.Join(root, skillName))
		if err == nil && st.IsDir() {
			return ""
		}
	}
	return buildMarketplaceURL(skillName)
}

// buildMarketplaceURL encodes the skill name with url.QueryEscape so
// punctuation that the marketplace server would otherwise interpret
// (&, =, ?) becomes a literal %XX form. url.QueryEscape is the right
// pick for the meta-characters; it deliberately *also* encodes " "
// to "+" which the marketplace search page interprets as a literal
// plus instead of a space.
//
// We post-process the QueryEscape output to convert "+" back to "%20"
// so the marketplace sees the conventional space encoding — the same
// shape AGY 1.4.2's release-note example URL uses ("?q=foo%20bar"
// rather than "?q=foo+bar"). The conversion is safe because "+" only
// appears in QueryEscape output as the space encoding; every other
// character QueryEscape encodes is already %XX.
func buildMarketplaceURL(skillName string) string {
	// Trim any whitespace the user typed — defends against accidental
	// " my skill " that the panel would have already folded but the
	// call site might not have.
	encoded := url.QueryEscape(strings.TrimSpace(skillName))
	if encoded == "" {
		return ""
	}
	encoded = strings.ReplaceAll(encoded, "+", "%20")
	return endpoints.Current().Marketplace + encoded
}

// EmitMarketplaceLink renders a marketplace link with OSC 8 when the
// running terminal supports hyperlinks, or "label (url)" plain text
// otherwise.
//
// The OSC 8 frame is the same shape cycle 2 of the OSC pair uses
// throughout the panel package: ESC ] 8 ; ; URL ST LABEL ESC ] 8 ; ; ST.
// When termSupportsOSC8 is false (Apple Terminal, GNU screen, tmux
// without hyperlink passthrough, any other terminal that does not
// advertise OSC 8) the helper returns the label followed by a
// space-and-paren URL — the same fallback shape render.Hyperlink emits
// today (cmd/wagyu/hyperlink_probe.go cycle's reported behaviour).
//
// url == "" passes through unchanged. label == "" is treated as a
// request for the default "Marketplace" label so the caller doesn't
// have to remember the literal.
func EmitMarketplaceLink(url string, label string, termSupportsOSC8 bool) string {
	if url == "" {
		return ""
	}
	if label == "" {
		label = "Marketplace"
	}
	if !termSupportsOSC8 {
		// Plain-text fallback: AGY 1.4.0 emits "<label> (<url>)" so
		// the URL is still visible (and copy-pasteable) when the
		// terminal cannot parse OSC 8.
		return label + " (" + url + ")"
	}
	// OSC 8 hyperlink frame:
	//
	//	ESC ] 8 ; ; URL ESC \  LABEL  ESC ] 8 ; ; ESC \
	//
	// The ";" parameter slot is left empty (matches render.Hyperlink
	// in render/render.go) — the param slot is reserved for
	// future styling hints (AGY 1.4.0 ships with the empty-param
	// convention).
	const osc = "\x1b]8;;"
	const st = "\x1b\\"
	var b strings.Builder
	b.Grow(len(osc)*2 + len(url) + len(label) + len(st)*2 + 4)
	b.WriteString(osc)
	b.WriteString(url)
	b.WriteString(st)
	b.WriteString(label)
	b.WriteString(osc)
	b.WriteString(st)
	return b.String()
}
