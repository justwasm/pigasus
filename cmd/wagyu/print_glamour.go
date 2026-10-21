// print_glamour.go — cycle 231 glamour-on-Done routing for print mode.
//
// Cycle 230 brought live streaming + line discipline to the interactive
// TUI's chat transcript; the TUI glamour call lives in
// cmd/wagyu/conversation.go:491 renderMarkdown, which the TUI's
// turnDoneMsg handler wraps every reply in. Print mode (`-p`) skipped
// that wrapping entirely: cmd/wagyu/main.go:944 wrote sanitised chunks
// straight to out with `fmt.Fprint(out, sanitized)`, so a user who ran
//
//	$ wagyu -p='reply with **bold** and a list:
//	- alpha
//	- beta'
//
// saw the literal `**bold**` and `- alpha` markers on stdout, not the
// styled output the TUI produces. AGY's --print path is reported to
// apply glamour too (cycle 226 investigation: the binary's chat
// transcript goes through the embedded `charm.land/glamour/v2`
// renderer for both surfaces), so the cycle closes that gap.
//
// The routing rule — captured in the table at the bottom of this file
// — is:
//
//	text + TTY:        buffer everything, glamour at Done.
//	text + non-TTY:    stream raw (ANSI escapes are noise on piped).
//	json / stream-json: stream raw (ANSI would corrupt the envelope).
//
// Width 0 handed to mdrender.ANSI means glamour auto-fits the terminal
// width via the COLUMNS env var / the actual terminal size when
// connected to one. Cycle 228's fallback contract — glamour error
// returns the raw text — is preserved: renderPrintText returns the
// raw buffer when the glamour call errors or yields empty styled
// output, so the user always sees something rendered instead of an
// empty transcript.
//
// What glamour-on-Done gives up (documented in deliverable.md):
//
//   - Live streaming for text + TTY. The chunks accumulate in the
//     router until Done, then the styled buffer lands on stdout in one
//     write. The cycle-229 / 230 raw-then-styled transition the TUI
//     shows does not happen here — print mode has no live transcript
//     to re-style in place. A user who wants live streaming can pipe
//     stdout (`| cat`), which disables glamour and reverts to the
//     historical live-stream behaviour.
//   - --print-partial-response has no visible effect on text + TTY.
//     The flag was already a no-op for text format (partialResponseState
//     never writes to W for text — the chunk handler streamed raw text
//     directly), and glamour-on-Done replaces that live-stream path
//     with buffering. Documented as a trade-off in deliverable.md.
package main

import (
	"io"
	"strings"

	"github.com/justwasm/pigasus/internal/mdrender"
)

// printTextRouter is the cycle-231 print-mode text-format chunk router.
//
// useGlamour=true (text format + TTY): Push accumulates sanitised
// chunks in an internal strings.Builder; Flush glamour-renders the
// accumulated buffer and writes the styled output to out. The user
// therefore sees nothing until Flush, when the entire styled reply
// lands in one go — the same raw-then-styled transition cycle 230
// documents for the TUI, except without a transcript to swap in
// place.
//
// useGlamour=false (text format + non-TTY / piped, or any JSON format
// — though the JSON formats never construct this router): Push writes
// the chunk to out live; Flush writes the held-back flushed text to
// out. This is the cycle-50 chunkHandler behaviour preserved
// byte-for-byte, so non-TTY consumers and the JSON envelope paths are
// unaffected by cycle 231.
type printTextRouter struct {
	useGlamour bool
	buf        strings.Builder
}

// newPrintTextRouter constructs a router with the glamour decision
// already resolved. runPrintMode decides once per turn and shares the
// router across every chunk that turn produces.
func newPrintTextRouter(useGlamour bool) *printTextRouter {
	return &printTextRouter{useGlamour: useGlamour}
}

// UseGlamour reports whether the router is buffering for glamour-at-
// Flush. The chunk handler checks this to decide whether the
// --remote-control daemon's session mirror should be appended per-chunk
// (live-stream path) or once at Done with the rendered text
// (glamour-buffer path). See runPrintMode for the two call sites.
func (r *printTextRouter) UseGlamour() bool {
	return r.useGlamour
}

// Push routes one sanitised chunk to either the glamour buffer
// (useGlamour=true, silent) or to out (useGlamour=false, live-stream
// shape the cycle-50 chunkHandler established). The asymmetry is
// intentional: live-stream has been the byte-level contract for
// non-TTY text since cycle 50, so Push leaves that path untouched and
// only adds the glamour-accumulator path on top.
func (r *printTextRouter) Push(sanitized string, out io.Writer) error {
	if r.useGlamour {
		r.buf.WriteString(sanitized)
		return nil
	}
	_, err := io.WriteString(out, sanitized)
	return err
}

// Flush routes the sanitizer-held-back final text (flushed) the same
// way Push routes each chunk — to the buffer when useGlamour, or
// straight to out otherwise — and returns the text the user actually
// saw. Glamour errors fall back to the raw buffer (cycle 228
// contract), so an empty transcript never reaches the user even if
// glamour's parser rejects the input.
//
// The trailing newline is the caller's responsibility: runPrintMode
// keeps the existing `if format == printFormatText { fmt.Fprintln(out) }`
// shape so cycle 230's terminator contract on text format is
// preserved.
func (r *printTextRouter) Flush(flushed string, out io.Writer) (string, error) {
	if !r.useGlamour {
		if _, err := io.WriteString(out, flushed); err != nil {
			return "", err
		}
		return flushed, nil
	}
	r.buf.WriteString(flushed)
	rendered := renderPrintText(r.buf.String(), glamourRenderWidth(out))
	if _, err := io.WriteString(out, rendered); err != nil {
		return "", err
	}
	return rendered, nil
}

// renderPrintText applies the glamour ANSI render to a fully-
// accumulated reply. Width 0 hands the sizing decision to glamour
// (COLUMNS env / terminal size). On a glamour error or empty styled
// output the function returns the raw input — the cycle 228 fallback
// contract for the chat-transcript glamour path, kept identical so
// the print-mode renderer cannot regress to "empty stdout on a glamour
// error".
//
// Cycle 268: the helper takes an explicit width so the caller can
// hand it the resolved terminal width; width <= 0 falls through to
// mdrender.CurrentWidth() (which reads $COLUMNS / x/term). Before
// this cycle every render went through with width=0, capped at
// glamour's hard default of 80 cols regardless of how wide the
// terminal was.
func renderPrintText(raw string, width int) string {
	if raw == "" {
		return raw
	}
	if width <= 0 {
		width = mdrender.CurrentWidth()
	}
	styled, err := mdrender.ANSI(raw, width)
	if err != nil || styled == "" {
		return raw
	}
	return styled
}

// Print-mode glamour routing table (mirrors the table in deliverable.md):
//
//	format      stdout    useGlamour   behaviour
//	-----       ------    ----------   ---------------------------------
//	text        TTY       true         buffer; glamour at Done
//	text        non-TTY   false        live-stream raw (ANSI = noise)
//	json        any       false        accumulate in responseBuf (envelope)
//	stream-json any       false        accumulate + emit per-chunk events
//
// (UseGlamour is exported only so the runPrintMode chunk handler can
// decide whether to mirror per-chunk or once-at-Done to the
// --remote-control daemon. Tests in print_glamour_test.go exercise
// the four cases directly.)