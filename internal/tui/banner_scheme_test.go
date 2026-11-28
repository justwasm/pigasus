package tui

import (
	"regexp"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/palette"
)

// The banner is redrawn whenever the header cache key changes, and that
// key carries the colour scheme. Its four rows must therefore be
// formatted at draw time: a row built once at package init keeps the
// startup scheme's colour forever.
//
// This is exactly what happened to logoLines — the account, model and
// cwd rows stayed at the startup scheme's DimFg while the title row
// followed /config, so the banner was two schemes at once.
func TestBannerRowsFollowTheColourScheme(t *testing.T) {
	render := func() string {
		var b strings.Builder
		renderBannerWithTheme(&b, nil, "user@example.com", "Gemini 3.8 Flash (High)", "Pro", "/tmp/ws")
		return b.String()
	}

	palette.SetScheme("solarized light")
	defer palette.SetScheme("solarized light")
	light := render()

	palette.SetScheme("dark")
	dark := render()

	if light == dark {
		t.Fatal("banner is byte-identical under two schemes, so nothing follows the scheme")
	}

	// The title wears the scheme's accent plus bold; the body rows wear
	// its DimFg. Both must move.
	beforeAccent := palette.SeqWith(mustSlot("solarized light", "Accent"), "1")
	afterAccent := palette.SeqWith(mustSlot("dark", "Accent"), "1")
	if beforeAccent == afterAccent {
		t.Fatal("the accent is the same under both schemes; test cannot discriminate")
	}
	if !strings.Contains(light, beforeAccent) {
		t.Errorf("solarized banner title does not carry %q", beforeAccent)
	}
	if !strings.Contains(dark, afterAccent) {
		t.Errorf("dark banner title does not carry %q", afterAccent)
	}
	for _, slot := range []string{"DimFg"} {
		before := slotSeq("solarized light", slot)
		after := slotSeq("dark", slot)
		if before == after {
			t.Fatalf("scheme slot %s is the same under both schemes; test cannot discriminate", slot)
		}
		if !strings.Contains(light, before) {
			t.Errorf("solarized banner does not carry %s (%q)", slot, before)
		}
		if !strings.Contains(dark, after) {
			t.Errorf("dark banner does not carry %s (%q)", slot, after)
		}
	}
}

// A row drawn from a package-level pre-formatted string would keep the
// startup scheme's DimFg no matter what /config does. Pin the concrete
// escape so a regression names the exact value it froze at.
func TestBannerBodyDoesNotFreezeTheStartupScheme(t *testing.T) {
	palette.SetScheme("dark")
	defer palette.SetScheme("solarized light")

	var b strings.Builder
	renderBannerWithTheme(&b, nil, "user@example.com", "Gemini 3.8 Flash (High)", "Pro", "/tmp/ws")
	out := b.String()

	frozen := slotSeq("solarized light", "DimFg")
	live := slotSeq("dark", "DimFg")

	if !strings.Contains(out, live) {
		t.Errorf("banner under dark does not carry dark's DimFg %q:\n%s", live, out)
	}
	if strings.Contains(out, frozen) {
		t.Errorf("banner under dark still carries solarized light's DimFg %q (frozen at init):\n%s", frozen, out)
	}
}

// Nothing in the banner may be a compile-time string: an escape baked
// into a package-level literal cannot follow the scheme.
func TestBannerHasNoInitFrozenColour(t *testing.T) {
	// The three body rows and the title row each open with an escape.
	ansi := regexp.MustCompile("\x1b\\[[0-9;]*m")
	palette.SetScheme("tokyo night")
	defer palette.SetScheme("solarized light")

	var b strings.Builder
	renderBannerWithTheme(&b, nil, "u@e.com", "m", "", "/tmp")
	lines := strings.Split(strings.TrimRight(b.String(), "\n"), "\n")
	if len(lines) < 4 {
		t.Fatalf("banner has %d lines, want at least 4", len(lines))
	}

	tokyoAccent := palette.SeqWith(mustSlot("tokyo night", "Accent"), "1")
	tokyoDim := slotSeq("tokyo night", "DimFg")

	if !strings.Contains(lines[0], tokyoAccent) {
		t.Errorf("title row does not carry tokyo's accent %q: %q", tokyoAccent, ansi.ReplaceAllString(lines[0], ""))
	}
	for i := 1; i <= 3; i++ {
		if !strings.Contains(lines[i], tokyoDim) {
			t.Errorf("body row %d does not carry tokyo's DimFg %q", i, tokyoDim)
		}
	}
}

// mustSlot resolves a scheme slot, failing loudly when the name is wrong.
func mustSlot(scheme, slot string) palette.Color {
	c, ok := palette.Schemes[scheme].Slot(slot)
	if !ok {
		panic("no such scheme slot: " + scheme + "/" + slot)
	}
	return c
}

// slotSeq renders the escape a named scheme's slot opens.
func slotSeq(scheme, slot string) string {
	c, ok := palette.Schemes[scheme].Slot(slot)
	if !ok {
		panic("no such scheme slot: " + scheme + "/" + slot)
	}
	return palette.SeqWith(c, "")
}
