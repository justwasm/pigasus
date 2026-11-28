package mdrender

// scheme_follow_test.go — every marked-up element follows the colour scheme.
//
// # What this pins
//
// Two primitives used to be ANSI-256 literals rather than scheme slots, so
// they were the elements that ignored /config:
//
//   - the link text was "29" (a magenta);
//   - inline code was "222" on "237" (dark) / "32" on "254" (light).
//
// The reference draws both from the scheme: link text is the scheme's LinkFg
// (#8ab4f8 under `dark`, #7aa2f7 under `tokyo night`) and a code span is the
// scheme's Accent on its Border (#8ab4f8 on #3c4043 under `dark`).
//
// The test's contract is the invariant, not a particular pair of colours: no
// ANSI-256 foreground/background may appear under two different schemes. A
// colour that survives a scheme change is a colour that ignored it.
//
// Following the scheme is necessary but not sufficient, and the second
// property is the one worth guarding: a code span has to stay *visible*. The
// span was briefly painted in the body colour, which followed every scheme
// perfectly and rendered `code` identically to the words around it. The
// invariant that catches that is TestInlineCodeIsVisibleUnderEveryScheme — a
// span that matches its surroundings is not a span.

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// schemeProbeDoc exercises every primitive that carries a colour.
const schemeProbeDoc = "# Head\n\n> quote\n\nA `code` span and [link](http://x).\n\n" +
	"```go\nfunc f() {}\n```\n\n- item\n\n---\n"

var sgrPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

// indexedSGR returns the ANSI-256 colour sequences a render emits.
func indexedSGR(s string) map[string]bool {
	out := map[string]bool{}
	for _, m := range sgrPattern.FindAllString(s, -1) {
		if strings.Contains(m, "38;5;") || strings.Contains(m, "48;5;") {
			out[m] = true
		}
	}
	return out
}

// TestNoColourSurvivesASchemeChange is the invariant: an element whose colour
// is the same under two schemes is an element that ignored the scheme.
func TestNoColourSurvivesASchemeChange(t *testing.T) {
	SetColorScheme("dark")
	dark := indexedSGR(RenderMarkdown(schemeProbeDoc, 100))
	SetColorScheme("tokyo night")
	tokyo := indexedSGR(RenderMarkdown(schemeProbeDoc, 100))
	defer SetColorScheme("solarized light")

	var stuck []string
	for code := range dark {
		if tokyo[code] {
			stuck = append(stuck, code)
		}
	}
	sort.Strings(stuck)
	if len(stuck) > 0 {
		// %q, not %v: an SGR sequence starts with ESC, which %v prints as
		// nothing — an empty-looking list would send the reader hunting for a
		// bug that is not there.
		t.Errorf("these ANSI-256 colours are identical under both schemes, so they ignore it: %q", stuck)
	}
}

// TestLinkTextTakesTheSchemesLinkSlot pins the specific slot, so a future
// refactor cannot substitute "some colour that changes" for the right one.
func TestLinkTextTakesTheSchemesLinkSlot(t *testing.T) {
	cases := []struct{ scheme, want string }{
		{"dark", "#8ab4f8"},
		{"tokyo night", "#7aa2f7"},
	}
	for _, tc := range cases {
		SetColorScheme(tc.scheme)
		if got := linkTextColor(); got != tc.want {
			t.Errorf("linkTextColor() under %q = %q, want %q (the scheme's LinkFg)", tc.scheme, got, tc.want)
		}
	}
	SetColorScheme("solarized light")
}

// TestInlineCodeTakesTheSchemesAccentOnBorder pins the code span to the pair
// the capture recorded, slot by slot.
//
// The pair is not decoration: painting the span in the body colour — which is
// what this did before — made `code` byte-for-byte identical to the words
// around it, so the span stopped being an element. The companion test below
// pins that property; this one pins that the colours are the right ones.
func TestInlineCodeTakesTheSchemesAccentOnBorder(t *testing.T) {
	cases := []struct{ scheme, text, surface string }{
		{"light", "#4285f4", "#dadce0"},
		{"dark", "#8ab4f8", "#3c4043"},
		{"tokyo night", "#7aa2f7", "#414868"},
		{"solarized light", "#268bd2", "#eee8d5"},
	}
	for _, tc := range cases {
		SetColorScheme(tc.scheme)
		if got := codeSpanText(); got != tc.text {
			t.Errorf("codeSpanText() under %q = %q, want %q (the scheme's Accent)", tc.scheme, got, tc.text)
		}
		if got := codeSpanSurface(); got != tc.surface {
			t.Errorf("codeSpanSurface() under %q = %q, want %q (the scheme's Border)", tc.scheme, got, tc.surface)
		}
	}
	SetColorScheme("solarized light")
}

// TestInlineCodeIsVisibleUnderEveryScheme is the invariant the body-coloured
// span broke: a code span has to render differently from the same sentence
// without it.
//
// A span that matches its surroundings is not a span. The assertion is a
// difference rather than a colour, so it keeps holding whichever slots the
// scheme spends on the element — and it fails the moment a slot is picked that
// happens to equal the body colour.
func TestInlineCodeIsVisibleUnderEveryScheme(t *testing.T) {
	schemes := []string{
		"light", "solarized light", "colorblind-friendly light",
		"dark", "solarized dark", "colorblind-friendly dark",
		"tokyo night", "terminal",
	}
	defer SetColorScheme("solarized light")
	for _, scheme := range schemes {
		SetColorScheme(scheme)
		withSpan := RenderMarkdown("a `code` span", 0)
		withoutSpan := RenderMarkdown("a code span", 0)
		if withSpan == withoutSpan {
			t.Errorf("under %q a code span renders identically to the same sentence without one, so the span is invisible:\n  with:    %q\n  without: %q",
				scheme, withSpan, withoutSpan)
			continue
		}
		// The difference has to be the span, not its surroundings: the
		// sentence around it must be untouched.
		if !strings.Contains(withSpan, "code") {
			t.Errorf("under %q the span's content was lost: %q", scheme, withSpan)
		}
	}
}
