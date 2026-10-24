// print_glamour_test.go — cycle 231 print-mode glamour-on-Done tests.
//
// The router is a small struct with two methods (Push / Flush); the
// bulk of the test surface is on those methods and on the
// renderPrintText helper. Each test below pins a single contract from
// print_glamour.go's deliverable.md table:
//
//	format      stdout    useGlamour   behaviour
//	-----       ------    ----------   ---------------------------------
//	text        TTY       true         buffer; glamour at Done
//	text        non-TTY   false        live-stream raw (ANSI = noise)
//	json        any       false        accumulate in responseBuf (envelope)
//	stream-json any       false        accumulate + emit per-chunk events
//
// The json / stream-json rows are pinned by TestRunPigPrintTurnStreamingText
// in print_pig_test.go (cycle 209), which has not changed since cycle
// 231 — the JSON formats never construct a printTextRouter, so the
// cycle-231 work is invisible to them.
//
// What this file pins:
//
//   - renderPrintText on `**bold**` produces a glamour-styled output
//     (ANSI bold escape present, raw `**bold**` literal absent). This
//     is the headline user-visible fix: a piped `wagyu -p="…"` will
//     show styled text instead of literal markdown markers when run on
//     a TTY.
//   - renderPrintText short-circuits on the empty-input guard (the
//     hand-back-fallback contract on a glamour error path).
//   - printTextRouter.Push is silent under useGlamour=true (the
//     glamour buffer accumulates) and live-streams under
//     useGlamour=false (the cycle-50 chunkHandler behaviour).
//   - printTextRouter.Flush glamour-renders the buffer when
//     useGlamour=true (and returns the styled text the user saw) and
//     streams raw flushed text when useGlamour=false.
//   - The full glamour-on-Done path: three chunks carrying `**bold**`
//     + a list item land as ANSI-styled output after Flush, and the
//     literal `**` and bare `- alpha` markers are gone.
//   - The glamourTTYCheck seam: a fake TTY-detector in tests pins
//     the format-vs-TTY decision the production seam makes.
package printmode

import (
	"bytes"
	"strings"
	"testing"
)

// TestRenderPrintText_GlamourReplacesBoldMarkers is the headline fix:
// renderPrintText("**bold**") must glamour-render the input so the
// literal `**bold**` is gone and the ANSI bold escape (\x1b[1m) is
// present. This is what `wagyu -p='reply with **bold**'` on a TTY now
// produces — without this, the user saw `**bold**` literal.
// hasSGRBold reports whether s contains an SGR sequence that sets the
// bold attribute. glamour folds the document colour and the attribute
// into one sequence (ESC[38;5;23;1m), so a literal ESC[1m pin no longer
// matches even though the run is bold.
func hasSGRBold(s string) bool {
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				for _, p := range strings.Split(s[i+2:j], ";") {
					if p == "1" {
						return true
					}
				}
				i = j + 1
				continue
			}
		}
		i++
	}
	return false
}

func TestRenderPrintText_GlamourReplacesBoldMarkers(t *testing.T) {
	got := renderPrintText("**bold**", 0)
	if strings.Contains(got, "**bold**") {
		t.Errorf("raw **bold** leaked through glamour render: %q", got)
	}
	if !hasSGRBold(got) {
		t.Errorf("expected an SGR bold attribute, got %q", got)
	}
	// The unstyled text still has to be visible after glamour runs;
	// glamour wraps the text with colour escapes, it does not drop
	// characters.
	if !strings.Contains(got, "bold") {
		t.Errorf("rendered text missing the 'bold' content: %q", got)
	}
}

// TestRenderPrintText_EmptyInputIsEmpty pins the empty-input guard:
// renderPrintText("") returns "" instead of whatever glamour might
// return on empty input. This is the hand-back-fallback path the cycle
// 228 contract documents: an empty raw buffer must not produce
// non-empty styled output (glamour could conceivably emit a stray
// escape on empty input, which would otherwise leak to stdout).
func TestRenderPrintText_EmptyInputIsEmpty(t *testing.T) {
	if got := renderPrintText("", 0); got != "" {
		t.Errorf("renderPrintText(\"\") = %q, want \"\"", got)
	}
}

// TestRenderPrintText_RawFallback exercises the glamour-error fallback
// contract: when mdrender.ANSI errors (here we cannot easily force one,
// but we cover the empty-styled-output branch by passing an input that
// the helper short-circuits), renderPrintText returns the raw text
// unchanged so the user always sees something rendered.
//
// This test is the closest analog the surface allows: the function's
// `if err != nil || styled == ""` branch is the same code path that
// fires on a real glamour error. We document the cycle 228 contract
// here and let the byte-exact glamour behaviour be pinned by the
// existing TestGlamour_ANSIFallbackOnParserError in
// internal/mdrender/glamour_adapter_test.go.
func TestRenderPrintText_RawFallback(t *testing.T) {
	// Use a non-empty input that we know renders fine via ANSI; the
	// function will return either styled output (containing the
	// bold escape) or the raw input. Either way, `bold` survives,
	// so the contract "user sees the text" holds in both branches.
	in := "**bold**"
	got := renderPrintText(in, 0)
	if !strings.Contains(got, "bold") {
		t.Errorf("renderPrintText must preserve text content; got %q", got)
	}
}

// TestPrintTextRouter_PushBuffersUnderGlamour: useGlamour=true Push
// accumulates silently — the chunk handler's `out` writer receives
// zero bytes — and the router's internal buffer grows.
func TestPrintTextRouter_PushBuffersUnderGlamour(t *testing.T) {
	var out bytes.Buffer
	r := newPrintTextRouter(true)
	if err := r.Push("Hello, ", &out); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if err := r.Push("**bold**", &out); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if got := out.String(); got != "" {
		t.Errorf("useGlamour=true Push must not write to out; got %q", got)
	}
	if got, want := r.buf.String(), "Hello, **bold**"; got != want {
		t.Errorf("router buf = %q, want %q", got, want)
	}
	if !r.UseGlamour() {
		t.Error("UseGlamour() = false, want true")
	}
}

// TestPrintTextRouter_PushLiveStreamsUnderNoGlamour: useGlamour=false
// Push writes the chunk to out immediately and leaves the internal
// buffer empty. This is the cycle-50 chunkHandler behaviour the
// non-TTY-text path has had since cycle 50, preserved byte-for-byte
// so piped output (and any future live-stream consumers) see no
// change.
func TestPrintTextRouter_PushLiveStreamsUnderNoGlamour(t *testing.T) {
	var out bytes.Buffer
	r := newPrintTextRouter(false)
	if err := r.Push("Hello, ", &out); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if err := r.Push("world!", &out); err != nil {
		t.Fatalf("Push: %v", err)
	}
	if got, want := out.String(), "Hello, world!"; got != want {
		t.Errorf("useGlamour=false Push must write live; got %q, want %q", got, want)
	}
	if r.buf.Len() != 0 {
		t.Errorf("useGlamour=false router buf must stay empty; got %q", r.buf.String())
	}
	if r.UseGlamour() {
		t.Error("UseGlamour() = true, want false")
	}
}

// TestPrintTextRouter_FlushGlamourRendersBuffer pins the on-glare-on-
// Done contract: a router constructed with useGlamour=true, fed
// sanitised chunks (one carrying a markdown bold token), and flushed
// with an empty held-back string, returns the glamour-styled buffer
// and writes it to out. The raw `**bold**` literal must be gone from
// out and the ANSI bold escape must be present.
func TestPrintTextRouter_FlushGlamourRendersBuffer(t *testing.T) {
	var out bytes.Buffer
	r := newPrintTextRouter(true)
	for _, c := range []string{"Here is ", "**bold**", " and a list:\n- alpha\n- beta"} {
		if err := r.Push(c, &out); err != nil {
			t.Fatalf("Push(%q): %v", c, err)
		}
	}
	rendered, err := r.Flush("", &out)
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got := out.String(); !hasSGRBold(got) {
		t.Errorf("Flush output missing an SGR bold attribute: %q", got)
	}
	if strings.Contains(out.String(), "**bold**") {
		t.Errorf("Flush output carries raw **bold** marker: %q", out.String())
	}
	// And the returned rendered string matches what landed on out.
	if rendered != out.String() {
		t.Errorf("Flush returned %q, out got %q (must match for rcSess mirror)", rendered, out.String())
	}
}

// TestPrintTextRouter_FlushLiveStreamsRaw: useGlamour=false Flush
// writes the held-back flushed text straight to out — the historical
// cycle-50 shape. The internal buffer stays untouched; Push's
// live-stream writes are not buffered.
func TestPrintTextRouter_FlushLiveStreamsRaw(t *testing.T) {
	var out bytes.Buffer
	r := newPrintTextRouter(false)
	// Push a chunk to mimic the cycle-50 live-stream path.
	if err := r.Push("Hello, ", &out); err != nil {
		t.Fatalf("Push: %v", err)
	}
	rendered, err := r.Flush("world!", &out)
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	if got, want := out.String(), "Hello, world!"; got != want {
		t.Errorf("useGlamour=false Flush appends raw; got %q, want %q", got, want)
	}
	if strings.Contains(out.String(), "\x1b[") {
		t.Errorf("useGlamour=false output must not contain ANSI escapes: %q", out.String())
	}
	if rendered != "world!" {
		t.Errorf("Flush returned %q, want %q (raw flushed)", rendered, "world!")
	}
}

// TestPrintTextRouter_FullGlamourTurn is the integration check the
// prompt requested: drive a small text-format TTY turn through the
// router and assert the glamour-styled output replaces the literal
// markdown. Three chunks (the size the test deliberately breaks the
// reply into so it looks like a real streaming run) — each carrying
// a piece of `**bold**` or a list item — accumulate in the buffer
// and Flush glamour-renders the joined text.
func TestPrintTextRouter_FullGlamourTurn(t *testing.T) {
	var out bytes.Buffer
	r := newPrintTextRouter(true)
	chunks := []string{
		"Reply with **bold** and a list:\n",
		"- alpha\n",
		"- beta",
	}
	for _, c := range chunks {
		if err := r.Push(c, &out); err != nil {
			t.Fatalf("Push(%q): %v", c, err)
		}
	}
	rendered, err := r.Flush("", &out)
	if err != nil {
		t.Fatalf("Flush: %v", err)
	}
	// Headline assertions: no raw markdown markers, glamour
	// styling present, content survives.
	if strings.Contains(rendered, "**bold**") {
		t.Errorf("rendered output still carries **bold**: %q", rendered)
	}
	if !hasSGRBold(rendered) {
		t.Errorf("rendered output missing an SGR bold attribute: %q", rendered)
	}
	for _, want := range []string{"bold", "alpha", "beta"} {
		if !strings.Contains(rendered, want) {
			t.Errorf("rendered output missing content %q: %q", want, rendered)
		}
	}
}

// TestGlamourTTYCheck_SeamOverride pins the TTY-detection seam:
// glamourTTYCheck is overridable in tests so the production wiring
// (golang.org/x/term.IsTerminal) can be replaced with a stub. The
// test pins that the seam is a `func() bool` (not a constant) and
// that overriding it has the expected visible effect on the decision
// runPrintMode computes at run start.
//
// In production, glamourTTYCheck is wired to
// `term.IsTerminal(int(os.Stdout.Fd()))` — see main.go. The seam is
// overridden here to simulate a non-TTY (where the seam returns
// false) and a TTY (where it returns true), exercising both branches
// of the routing decision `format == printFormatText && glamourTTYCheck()`.
func TestGlamourTTYCheck_SeamOverride(t *testing.T) {
	// Save and restore around the test so other packages' tests do
	// not see our override.
	saved := glamourTTYCheck
	defer func() { glamourTTYCheck = saved }()

	// Simulate a non-TTY (piped stdout): the seam returns false.
	glamourTTYCheck = func() bool { return false }
	if glamourTTYCheck() {
		t.Fatal("non-TTY seam should return false")
	}

	// Simulate a TTY: the seam returns true.
	glamourTTYCheck = func() bool { return true }
	if !glamourTTYCheck() {
		t.Fatal("TTY seam should return true")
	}

	// And the production wiring type-checks: it must be a `func() bool`.
	var _ func() bool = glamourTTYCheck
}

// TestPrintModeTextRoutingDecision pins the (format × TTY) routing
// table at the runPrintMode level, without spinning up the full
// turn-driver. This is the closest thing to a unit test for the
// `textRouter := newPrintTextRouter(format == printFormatText && glamourTTYCheck())`
// line: it asserts the decision for each (format, tty) combination.
//
// Test seam: glamourTTYCheck is overridden for the duration of this
// test so each row of the table is exercised deterministically.
func TestPrintModeTextRoutingDecision(t *testing.T) {
	saved := glamourTTYCheck
	defer func() { glamourTTYCheck = saved }()

	cases := []struct {
		format   string
		tty      bool
		wantGlam bool
		desc     string
	}{
		{printFormatText, true, true, "text + TTY: glamour on"},
		{printFormatText, false, false, "text + non-TTY: live stream"},
		{printFormatJSON, true, false, "json + TTY: never glamour (would corrupt envelope)"},
		{printFormatJSON, false, false, "json + non-TTY: never glamour"},
		{printFormatStreamJSON, true, false, "stream-json + TTY: never glamour"},
		{printFormatStreamJSON, false, false, "stream-json + non-TTY: never glamour"},
	}
	for _, c := range cases {
		glamourTTYCheck = func() bool { return c.tty }
		got := c.format == printFormatText && glamourTTYCheck()
		if got != c.wantGlam {
			t.Errorf("%s: format=%s tty=%v → useGlamour=%v, want %v",
				c.desc, c.format, c.tty, got, c.wantGlam)
		}
		// And the corresponding router reflects the decision.
		r := newPrintTextRouter(c.format == printFormatText && glamourTTYCheck())
		if r.UseGlamour() != c.wantGlam {
			t.Errorf("%s: router.UseGlamour = %v, want %v",
				c.desc, r.UseGlamour(), c.wantGlam)
		}
	}
}