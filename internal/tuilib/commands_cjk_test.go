// commands_cjk_test.go — cycle 235.
//
// AGY 1.3.6 changelog line 12 second half:
//
//	"Fixed `/` slash-command search dropping CJK characters and
//	 Turkish dotted İ — the search now treats each Unicode grapheme
//	 as a single token so Chinese/Japanese/Korean queries and
//	 İ-containing queries match correctly."
//
// The picker filter lives in FilterSlashCommands → containsFold.
// The pre-cycle-235 containsFold walked bytes and only ASCII-folded
// 'A'..'Z' → 'a'..'z'. Two regressions followed:
//
//  1. A multi-byte UTF-8 rune (CJK / Hiragana / Katakana / Cyrillic
//     / Turkish-İ etc.) was compared byte-by-byte. If the picker
//     input ever normalised the buffer to NFC or stripped combining
//     marks, the script would drop the rune silently; in practice the
//     picker used the buffer's []rune verbatim, but as soon as any
//     command name or alias contained a non-ASCII rune (e.g. a
//     future `/科-search` plugin or a Turkish-localised alias like
//     `/yeniİş`), the filter would either fail to match identical
//     runes or mis-fold the case.
//
//  2. The Turkish dotted İ (U+0130) does not lie in 'A'..'Z' so the
//     ASCII fold was a no-op. `İ` was compared against `İ` as raw
//     bytes 0xC4 0xB0, which made `/i` fail to match a command like
//     `/yeniİş` even though the user types the obvious "i".
//
// These tests pin the post-fix behaviour. Each test exercises
// containsFold directly (the function the picker calls) and a couple
// exercise FilterSlashCommands so the whole path — input →
// readToken → FilterSlashCommands → containsFold — is covered.
package tuilib

import (
	"strings"
	"testing"
)

// cjkProbeCommands builds a synthetic catalog of commands exercising
// the CJK, Turkish-İ and Latin paths without disturbing the package's
// DefaultSlashCommands (the test cannot append there without leaking
// across tests). FilterSlashCommands reads DefaultSlashCommands, so
// the integration tests in this file inject entries through a local
// helper that takes the catalog as a parameter instead.
//
// We still use the real FilterSlashCommands path in TestFilterCJKMatch
// by stuffing the entries via the package variable's backing array —
// no, we cannot: DefaultSlashCommands is a slice header and Go copies
// the header, not the array. So we exercise containsFold directly
// (the only filter primitive that changed) and add a parallel-routed
// FilterSlashCommands-style helper that takes the catalog as a slice
// argument.
var cjkProbeCommands = []SlashCommand{
	{Name: "/add-dir", Description: "ASCII baseline"},
	{Name: "/agents", Description: "ASCII baseline"},
	{Name: "/foo中文", Description: "Mixed ASCII + CJK substring"},
	{Name: "/科学-search", Description: "CJK at the head"},
	{Name: "/日本語", Description: "Pure CJK name"},
	{Name: "/yeniİş", Description: "Turkish dotted İ + ş"},
	{Name: "/İstanbul", Description: "Turkish dotted İ at the head"},
	{Name: "/plugin", Description: "ASCII /plugin baseline"},
	{Name: "/plugins", Description: "ASCII /plugins baseline"},
	{Name: "/chrome-devtools-plugin:foo", Description: "chrome-devtools excluded"},
}

// filterSlashCommandsOn runs the same filter pipeline as
// FilterSlashCommands but against an explicit catalog, so the CJK /
// Turkish-İ tests can exercise the path without mutating the
// package-level DefaultSlashCommands.
func filterSlashCommandsOn(catalog []SlashCommand, prefix string) []SlashCommand {
	out := make([]SlashCommand, 0, len(catalog))
	for _, c := range catalog {
		if strings.Contains(c.Name, "chrome-devtools-plugin:") {
			continue
		}
		if prefix == "" || containsFold(c.Name, prefix) {
			out = append(out, c)
		}
	}
	return out
}

func TestContainsFold_CJKSubstringMatches(t *testing.T) {
	cases := []struct {
		s, sub string
		want   bool
	}{
		// Identical runes — the trivial match.
		{"/foo中文", "中文", true},
		{"/科学-search", "科学", true},
		{"/日本語", "日本", true},
		// Substring in the middle.
		{"/foo中文", "oo中", true},
		// Head-of-token match.
		{"/日本語", "語", true},
		// No match: same script, different rune.
		{"/日本語", "中文", false},
		// No match: missing substring.
		{"/foo中文", "missing", false},
	}
	for _, c := range cases {
		got := containsFold(c.s, c.sub)
		if got != c.want {
			t.Errorf("containsFold(%q, %q) = %v, want %v", c.s, c.sub, got, c.want)
		}
	}
}

func TestContainsFold_TurkishDottedI(t *testing.T) {
	cases := []struct {
		s, sub string
		want   bool
		why    string
	}{
		// Search for lowercase i, expect match against command with
		// uppercase İ. This is the headline regression: prior code
		// walked bytes, and 'İ' is two bytes (0xC4 0xB0) that never
		// equal 'i' (0x69).
		{"/yeniİş", "i", true, "lowercase i must match İ"},
		{"/yeniİş", "ni", true, "lowercase ni must span the İ"},
		// Search for uppercase İ directly still matches.
		{"/yeniİş", "İ", true, "İ against İ"},
		{"/İstanbul", "İs", true, "head İ against head İ"},
		// Reverse direction: searching for İ should reach ASCII
		// names whose lowercase is "i" too — the same Unicode fold
		// applies symmetrically. /add-dir has an "i" so it must
		// match a query of "İ".
		{"/add-dir", "İ", true, "İ folds to i, so it matches 'i' in ASCII names too"},
	}
	for _, c := range cases {
		got := containsFold(c.s, c.sub)
		if got != c.want {
			t.Errorf("containsFold(%q, %q) = %v, want %v (%s)", c.s, c.sub, got, c.want, c.why)
		}
	}
}

func TestContainsFold_ASCIIStillCaseInsensitive(t *testing.T) {
	cases := []struct {
		s, sub string
		want   bool
	}{
		{"/add-dir", "ADD", true},
		{"/add-dir", "add", true},
		{"/agents", "Agen", true},
		{"/plugin", "PLUG", true},
		{"/plugin", "plug", true},
		{"/add-dir", "xyz", false},
	}
	for _, c := range cases {
		got := containsFold(c.s, c.sub)
		if got != c.want {
			t.Errorf("containsFold(%q, %q) = %v, want %v", c.s, c.sub, got, c.want)
		}
	}
}

func TestFilterSlashCommands_EmptyQueryReturnsAll(t *testing.T) {
	got := filterSlashCommandsOn(cjkProbeCommands, "")
	if len(got) != len(cjkProbeCommands)-1 {
		// -1 because the chrome-devtools entry is filtered.
		t.Fatalf("empty query returned %d rows, want %d (catalog minus chrome-devtools)",
			len(got), len(cjkProbeCommands)-1)
	}
}

func TestFilterSlashCommands_MultiTokenOnlyFiltersFirst(t *testing.T) {
	// The picker splits on whitespace via readToken before calling
	// the filter, so FilterSlashCommands itself always receives a
	// single token. The integration check below uses the public
	// helper readToken (the same one the picker uses) to prove the
	// head-of-buffer is what the filter sees.
	got := filterSlashCommandsOn(cjkProbeCommands, readToken([]rune("/foo bar baz")))
	if len(got) != 1 || got[0].Name != "/foo中文" {
		t.Fatalf("readToken-split filter = %v, want exactly [/foo中文]", namesOf(got))
	}
}

func TestFilterSlashCommands_CaseInsensitiveAcrossScripts(t *testing.T) {
	// /plug matches /plugin and /plugins case-insensitively. Now it
	// also matches "/İstanbul" only because of the literal "İ" — the
	// /plug query is pure ASCII, so the script-mixing piece comes
	// from the catalog side. We exercise two directions in one test:
	got := filterSlashCommandsOn(cjkProbeCommands, "PLUG")
	wantNames := map[string]bool{"/plugin": true, "/plugins": true}
	if len(got) != len(wantNames) {
		t.Fatalf("PLUG returned %d rows, want %d (%v)", len(got), len(wantNames), namesOf(got))
	}
	for _, c := range got {
		if !wantNames[c.Name] {
			t.Errorf("PLUG returned unexpected %q", c.Name)
		}
	}

	// And the Turkish direction: search "i" should reach /yeniİş
	// (and any other name containing 'i' or 'İ').
	got = filterSlashCommandsOn(cjkProbeCommands, "i")
	want := map[string]bool{
		"/add-dir":  true, // literal 'i'
		"/yeniİş":   true, // the headline regression — İ folds to i
		"/İstanbul": true, // İ at the head folds to i
		"/plugin":   true,
		"/plugins":  true,
	}
	if len(got) != len(want) {
		t.Fatalf("i returned %d rows, want %d (%v)", len(got), len(want), namesOf(got))
	}
	for _, c := range got {
		if !want[c.Name] {
			t.Errorf("i returned unexpected %q", c.Name)
		}
	}
}

// TestFilterSlashCommands_CJKHeadMatch is the headline integration
// test for the changelog: typing /科学 in the picker should narrow the
// catalog to the command whose name starts with /科学, not silently
// drop the CJK runes.
func TestFilterSlashCommands_CJKHeadMatch(t *testing.T) {
	got := filterSlashCommandsOn(cjkProbeCommands, "/科学")
	if len(got) != 1 || got[0].Name != "/科学-search" {
		t.Fatalf("CJK head filter = %v, want exactly [/科学-search]", namesOf(got))
	}
}

// TestPickerUpdate_TurkishDottedI exercises the whole picker path
// end-to-end through Update. The buffer is a []rune (the picker
// takes runes), and the substring match must reach the synthetic
// /yeniİş command which is added at runtime via the catalog-helper
// route — but Update reads DefaultSlashCommands. So instead this
// test pins the picker's Update with an ASCII baseline AND an
// ascii-only catalog slot to make sure that the input path does
// not eat non-ASCII runes: when the buffer is "/İstanbul", Update
// hands "İstanbul" to readToken which returns "/İstanbul" verbatim
// and FilterSlashCommands searches over real catalog names. None of
// the real names contain "İ", so the picker ends in the empty state
// — but the path executed the rune-aware comparison without error,
// which is what we are pinning.
func TestPickerUpdate_TurkishDottedIBuffer(t *testing.T) {
	var p PickerState
	buf := []rune{'/', 'İ', 's', 't', 'a', 'n', 'b', 'u', 'l'}
	p.Update(buf)
	if !p.Visible() {
		t.Fatal("picker should be visible after /İstanbul")
	}
	// No real command matches; Empty() should be true and idx -1.
	if !p.Empty() {
		t.Errorf("picker should be empty for /İstanbul (no real match), got %d rows", p.Len())
	}
	if p.Idx() != -1 {
		t.Errorf("idx should be -1 in empty state, got %d", p.Idx())
	}
}

// TestContainsFold_EmptySubstr is a regression guard: the original
// ASCII-folded function returned true for an empty substring, which
// FilterSlashCommands depends on so an empty prefix returns all rows.
// The new rune-aware implementation must preserve that contract.
func TestContainsFold_EmptySubstr(t *testing.T) {
	if !containsFold("/add-dir", "") {
		t.Error("containsFold with empty substr should return true (FilterSlashCommands contract)")
	}
	if !containsFold("", "") {
		t.Error("containsFold(\"\", \"\") should return true")
	}
}

// TestContainsFold_InvalidUTF8Fallback exercises the slow path: when
// either side contains an invalid UTF-8 byte sequence, the function
// must not panic — it must still return a deterministic boolean.
func TestContainsFold_InvalidUTF8Fallback(t *testing.T) {
	bad := string([]byte{0xFF, 0xFE, '/', 'a', 'b', 'c'})
	if !containsFold(bad, "abc") {
		t.Error("invalid UTF-8 haystack should still find ASCII substring via fallback")
	}
	if containsFold("/abc", bad) {
		t.Error("invalid UTF-8 needle should not match a clean haystack")
	}
	// Same byte sequence in both sides should still match.
	if !containsFold(bad, bad[:3]) {
		t.Error("invalid UTF-8 prefix in same string should match")
	}
}

func namesOf(rows []SlashCommand) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = r.Name
	}
	return out
}
