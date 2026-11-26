package rcfrontend

// Guard for the Remote Control browser view's KaTeX \label handling.
//
// The 1.5.17 changelog entry says:
//
//	Fixed LaTeX math blocks containing `\label{...}` failing to render with
//	`Failed to render LaTeX` in the Remote Control browser view.
//
// The cause is the renderer's `throwOnError: true` option: KaTeX has no
// built-in \label, so a block containing one throws, and the React catch arm
// turns the thrown message into the "Failed to render LaTeX: …" banner. The
// upstream fix is a macro that redefines \label to \@firstoftwo{}, which
// consumes the label's braced argument and emits nothing, so the render
// succeeds.
//
// assertMainJSCarriesLabelMacro checks the *served* bundle rather than the
// file on disk, so it also covers the runtime patch layer (rctunnel/patches)
// that rewrites main.js on the way out: a patch that dropped the macro would
// fail here.
//
// Note on the strings these tests use: "Failed to render LaTeX" appears
// whether or not the bug is present — it is the banner text, not the failure
// condition. The real evidence is the macro itself, which appears exactly
// once in the bundle and nowhere else.

import (
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// labelMacroNeedle is the exact minified form of the KaTeX macro the upstream
// 1.5.17 bundle carries. The doubled backslashes are literal: the bundle is
// JavaScript source, so the macro names are written with escaped backslashes.
const labelMacroNeedle = `macros:{"\\label":"\\@firstoftwo{}"}`

// katexCallNeedle is the head of the KaTeX render options object the macro has
// to sit in. It pins the surrounding shape the fix depends on.
const katexCallNeedle = `throwOnError:!0,output:"htmlAndMathml"`

// assertMainJSCarriesLabelMacro fetches the served main.js and asserts both
// that the KaTeX call kept its shape and that the \label macro is attached to
// it.
func assertMainJSCarriesLabelMacro(t *testing.T, body string) {
	t.Helper()

	if strings.Count(body, labelMacroNeedle) != 1 {
		t.Fatalf("served main.js carries the KaTeX \\label macro %d times, want exactly 1 (%q)",
			strings.Count(body, labelMacroNeedle), labelMacroNeedle)
	}

	// The macro must be inside the same options object as throwOnError, not
	// merely somewhere in the file. Take the window between the render call's
	// options head and the first macro occurrence and assert they are
	// adjacent — in the minified bundle the macro is the last entry of that
	// object.
	head := strings.Index(body, katexCallNeedle)
	if head < 0 {
		t.Fatalf("served main.js no longer contains the KaTeX call head %q", katexCallNeedle)
	}
	macros := strings.Index(body, labelMacroNeedle)
	if macros < head {
		t.Fatalf("KaTeX \\label macro at offset %d precedes the render call at %d; it is not attached to the renderer", macros, head)
	}
	between := body[head+len(katexCallNeedle) : macros]
	if len(between) > 8 {
		t.Fatalf("KaTeX \\label macro is %d bytes past the render options, want it adjacent: %q", len(between), between)
	}
}

// TestFrontend_1520LaTeXLabelMacroServed checks the production handler's
// output, which is what a browser actually receives.
func TestFrontend_1520LaTeXLabelMacroServed(t *testing.T) {
	handler := Handler(Options{EnableTouchPatches: true})

	req := httptest.NewRequest("GET", "/main.js", nil)
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, req)
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /main.js returned status %d, want 200", rr.Code)
	}
	assertMainJSCarriesLabelMacro(t, rr.Body.String())
}

// TestFrontend_LaTeXLabelMacroInEmbeddedAsset checks the embedded bytes
// directly, independent of the HTTP plumbing, so a handler change cannot hide
// a regression in the asset.
func TestFrontend_LaTeXLabelMacroInEmbeddedAsset(t *testing.T) {
	raw, err := fs.ReadFile(FS(), "main.js")
	if err != nil {
		t.Fatalf("read embedded main.js: %v", err)
	}
	assertMainJSCarriesLabelMacro(t, string(raw))
}
