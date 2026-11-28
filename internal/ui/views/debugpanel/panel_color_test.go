package debugpanel

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/palette"
)

// TestLogBodyFollowsTheScheme pins the fix for a body colour that was
// frozen into the source.
//
// styleLogSegment painted every ordinary line \x1b[38;2;232;234;237m —
// the *dark* scheme's foreground. Nothing in this package asserted the
// colour, so the literal survived every scheme: under `light` the /logs
// body drew #e8eaed text on a light terminal, and no test noticed.
//
// The assertion is deliberately about the *relationship*, not a number:
// the body must be exactly the scheme's foreground slot, which is what
// the transcript's document body reads. A future scheme added to the
// table then cannot be forgotten here.
func TestLogBodyFollowsTheScheme(t *testing.T) {
	const line = "[2026-11-28 01:00:00.000] [tui] interactive session starting"

	for _, name := range []string{
		"dark", "light", "solarized light", "solarized dark",
		"colorblind-friendly light", "colorblind-friendly dark", "tokyo night",
	} {
		restore := palette.PushScheme(name)
		sc := palette.Schemes[name]
		want := sc.Fg.Open()

		got := styleLogSegment(line, line)
		restore()

		if want == "" {
			t.Fatalf("%s: the scheme has no foreground; the test cannot tell", name)
		}
		if !strings.HasPrefix(got, want) {
			t.Errorf("%s: the log body is not the scheme's foreground\n got %q\nwant prefix %q",
				name, got, want)
		}
		if !strings.HasSuffix(got, logColourClose) {
			t.Errorf("%s: the body does not close its colour: %q", name, got)
		}
		if body := strings.TrimSuffix(strings.TrimPrefix(got, want), logColourClose); body != line {
			t.Errorf("%s: the body text changed: %q", name, body)
		}
	}
}

// TestLogBodyIsReadableUnderALightScheme is the defect stated as the
// user saw it: a light scheme must not paint the body near-white.
//
// The old literal was #e8eaed — luminance 233/255 — which is invisible
// on a light terminal. The light scheme's own foreground is dark, so a
// body that follows the scheme is readable by construction; this test
// pins the property rather than trusting the slot.
func TestLogBodyIsReadableUnderALightScheme(t *testing.T) {
	const line = "[2026-11-28 01:00:00.000] [tui] fetch ok"

	for _, name := range []string{"light", "solarized light", "colorblind-friendly light"} {
		restore := palette.PushScheme(name)
		got := styleLogSegment(line, line)
		restore()

		r, g, b, ok := firstTrueColour(got)
		if !ok {
			t.Fatalf("%s: the body carries no true-colour foreground: %q", name, got)
		}
		// Perceived luminance, ITU-R BT.601. Anything above the midpoint
		// is light-on-light under a light scheme.
		lum := 0.299*float64(r) + 0.587*float64(g) + 0.114*float64(b)
		if lum > 127.5 {
			t.Errorf("%s: the log body is near-white (r=%d g=%d b=%d, luminance %.0f) — "+
				"it was #e8eaed and unreadable on a light terminal", name, r, g, b, lum)
		}
	}
}

// TestLogSeverityColoursDoNotFollowTheScheme pins the other half of the
// decision: failure, retry and success are semantic, and they stay put
// when the scheme changes.
//
// Without this, "make the body follow the scheme" could plausibly be
// extended to the three loud cases, and a failed request would lose the
// colour that makes it loud.
func TestLogSeverityColoursDoNotFollowTheScheme(t *testing.T) {
	cases := []struct{ line, want string }{
		{"[x] request failed err=503", logErrorOpen},
		{"[x] retry_in=2s attempt 3/10", logRetryOpen},
		{"[x] fetch succeeded", logSuccessOpen},
	}
	for _, c := range cases {
		seen := map[string]bool{}
		for _, name := range []string{"dark", "light", "terminal"} {
			restore := palette.PushScheme(name)
			got := styleLogSegment(c.line, c.line)
			restore()
			if !strings.HasPrefix(got, c.want) {
				t.Errorf("%s: %q is not painted with its severity colour\n got %q\nwant prefix %q",
					name, c.line, got, c.want)
			}
			seen[got] = true
		}
		if len(seen) != 1 {
			t.Errorf("%q rendered differently across schemes: %v", c.line, seen)
		}
	}
}

// TestTerminalSchemeBodyInheritsTheTerminal is the alignment the user
// asked for: under the terminal scheme the body must carry no foreground
// at all, so it takes the terminal's own colour — which is exactly what
// the wire, trajectory and reader bodies do.
func TestTerminalSchemeBodyInheritsTheTerminal(t *testing.T) {
	const line = "[2026-11-28 01:00:00.000] [tui] interactive session starting"

	restore := palette.PushScheme("terminal")
	got := styleLogSegment(line, line)
	restore()

	if strings.Contains(got, "38;") || strings.Contains(got, "38;") {
		t.Errorf("the terminal scheme's body sets a foreground: %q", got)
	}
	if !strings.HasPrefix(got, line) {
		t.Errorf("the terminal scheme's body is not its plain text: %q", got)
	}
}

// firstTrueColour pulls the r, g, b out of leading "\x1b[38;2;r;g;bm".
func firstTrueColour(s string) (int, int, int, bool) {
	const open = "\x1b[38;2;"
	if !strings.HasPrefix(s, open) {
		return 0, 0, 0, false
	}
	rest := s[len(open):]
	end := strings.IndexByte(rest, 'm')
	if end < 0 {
		return 0, 0, 0, false
	}
	parts := strings.Split(rest[:end], ";")
	if len(parts) != 3 {
		return 0, 0, 0, false
	}
	var v [3]int
	for i, p := range parts {
		n := 0
		for _, c := range p {
			if c < '0' || c > '9' {
				return 0, 0, 0, false
			}
			n = n*10 + int(c-'0')
		}
		v[i] = n
	}
	return v[0], v[1], v[2], true
}
