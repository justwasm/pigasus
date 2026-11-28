package rcfrontend

// Guards for the Remote Control browser-view fixes in agy 1.5.18.
//
// 【一手】 agy 1.5.18 changelog, the four Remote Control bullets:
//
//	Fixed copying from code blocks and from raw Markdown or code file views in
//	the Remote Control browser view dropping blank lines, so pasted text keeps
//	its original spacing.
//	Fixed LaTeX equations in subagent replies showing as raw TeX in the Remote
//	Control browser view; they now render as formatted math like the rest of
//	the conversation.
//	Fixed shell variables in chat text in the Remote Control browser view, such
//	as `cp $src_dir/$name .` or `http://$host:$port/api`, being rendered as
//	LaTeX math; they now show as plain text and links keep their correct
//	addresses.
//	Fixed the model quota section of the Remote Control browser view's settings
//	spinning forever when quota fails to load; it now shows an empty state or
//	the last known quota, and `Refresh` works again.
//
// # Why these are byte assertions on the served bundle
//
// All four are front-end-only fixes: they live in the minified React bundle the
// reference serves, and the clone serves that bundle verbatim. There is no Go
// code to unit-test, so the honest assertion is over the bytes the browser
// receives — the shape internal/rcfrontend/latex_label_test.go already uses for
// the 1.5.17 `\label` fix.
//
// Two of the four have a *provable* delta between the 1.5.17 and 1.5.18
// bundles, and those are asserted one by one below (the shell-variable guard
// and the quota panel's initialised/empty/refresh states). The other two
// (blank lines on copy, LaTeX in subagent replies) were reworked inside
// functions whose minified names changed wholesale, so the bundle digest is
// what pins them: it says these bytes are the 1.5.18 build and not a
// hand-patched 1.5.17, which is the claim that actually matters. The delta
// evidence for all four is written down in
// docs/guide/reference-drift.md ("The 1.5.17 → 1.5.18 delta").

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// bundleDigest is the SHA-256 of internal/rcfrontend/assets/main.js.
//
// It is the reference's own 1.5.18 build, byte for byte:
// out/extracted-1.5.18/web-assets/main.js has the same digest. A hand patch
// into the old bundle — the tempting move for a one-line fix — would change
// this and be caught, which is the point: a file that is neither release is
// worse than either.
const bundleDigest = "fb6f082db1a4cb612b7f6f50c971a6d8fde2f3257a82fefa25e966d4c043d6af"

// servedMainJS fetches the bundle the browser actually receives, so a patch in
// internal/rctunnel/patches that rewrote the file on the way out is covered
// too.
func servedMainJS(t *testing.T) string {
	t.Helper()
	handler := Handler(Options{EnableTouchPatches: true})
	rr := httptest.NewRecorder()
	handler.ServeHTTP(rr, httptest.NewRequest(http.MethodGet, "/main.js", nil))
	if rr.Code != http.StatusOK {
		t.Fatalf("GET /main.js returned status %d, want 200", rr.Code)
	}
	return rr.Body.String()
}

// TestBundleIsThe1518Build pins the provenance of the tracked bundle: the
// markers below are only meaningful if the bytes are the 1.5.18 build.
func TestBundleIsThe1518Build(t *testing.T) {
	raw, err := fs.ReadFile(FS(), "main.js")
	if err != nil {
		t.Fatalf("read embedded main.js: %v", err)
	}
	sum := sha256.Sum256(raw)
	if got := hex.EncodeToString(sum[:]); got != bundleDigest {
		t.Fatalf("internal/rcfrontend/assets/main.js is not the agy 1.5.18 build:\n  got  sha256 %s\n  want sha256 %s\ncopy it verbatim from out/extracted-1.5.18/web-assets/main.js", got, bundleDigest)
	}
}

// TestBundleKeepsShellVariablesOutOfMath pins the `$var` fix.
//
// The bundle decides whether a `$` opens inline math by looking at the next
// character. 1.5.17's list of "this `$` closes something, it does not open
// math" cases was `)`, `/` and `*`; 1.5.18 adds `'`. Without that branch,
// `cp $src_dir/$name .` and `http://$host:$port/api` were parsed as math and
// rendered as TeX.
func TestBundleKeepsShellVariablesOutOfMath(t *testing.T) {
	body := servedMainJS(t)

	const withFix = `c===")"||c==="/"||c==="*"||c==="'"?`
	const withoutFix = `c===")"||c==="/"||c==="*"?`

	if !strings.Contains(body, withFix) {
		t.Error("the served main.js no longer treats `$'` as a shell variable: the 1.5.18 shell-variable fix is missing")
	}
	if strings.Contains(body, withoutFix) {
		t.Error("the served main.js still carries the 1.5.17 `$` guard (no `'` branch): shell variables would render as LaTeX again")
	}
}

// TestBundleQuotaPanelRecoversFromAFailedLoad pins the quota-spinner fix.
//
// 1.5.17 fetched the quota through `retrieveUserQuotaSummary` into two pieces
// of local state and showed the spinner whenever that state was `undefined`.
// A failed fetch left it `undefined` forever, so the section spun
// indefinitely and Refresh could not recover it. 1.5.18 reads the shared
// query's `isInitialized` flag and renders three distinct states: spinner,
// empty state, groups — so a failure lands on the empty state, and Refresh
// re-initiates the same query.
func TestBundleQuotaPanelRecoversFromAFailedLoad(t *testing.T) {
	body := servedMainJS(t)

	for _, marker := range []struct {
		needle string
		why    string
	}{
		{`useGetModelQuotaQuery(`, "the quota panel must read the shared query, whose isInitialized flag ends the spinner"},
		{`isInitialized?void 0:`, "the spinner must be gated on isInitialized, not on local state a failed fetch leaves undefined"},
		{`"No quota information available."`, "the panel must have an empty state to land on when the load fails"},
		{`"Refresh quota and credits data"`, "the panel must keep a Refresh control"},
		{`"data-testid":"model-quota-panel"`, "the panel must be the 1.5.18 component (1.5.17 carried no such test id)"},
	} {
		if !strings.Contains(body, marker.needle) {
			t.Errorf("the served main.js is missing %q: %s", marker.needle, marker.why)
		}
	}

	// The 1.5.17 shape must be gone: local state initialised to undefined and
	// read as `n===void 0` for the spinner. Its presence would mean the old
	// panel came back.
	if strings.Contains(body, `var [n,p]=F.useState(void 0),[q,r]=F.useState(void 0)`) {
		t.Error("the served main.js still carries the 1.5.17 quota panel (two undefined local states): a failed load would spin forever")
	}
}

// TestBundleCopyKeepsBlankLines pins the "copying drops blank lines" fix.
//
// The DOM-copy helper walks the selection, clones it into an off-screen div,
// strips the chrome (`.select-none`, `.line-number`, buttons, svg…), and reads
// the result back with innerText. 1.5.17 stopped there; 1.5.18 also keeps
// blank lines, by (a) treating an empty rendered line (marked
// `data-empty-line="true"`) as structure to preserve rather than chrome to
// strip, and (b) re-joining the plain `div.code-line` runs that a copy out of a
// raw Markdown or code-file view produces.
//
// Both halves are asserted, because either one alone still loses spacing: the
// first keeps a blank line inside prose, the second keeps one inside a code
// view.
func TestBundleCopyKeepsBlankLines(t *testing.T) {
	body := servedMainJS(t)

	// (a) the empty-line marker is part of the chrome selector, i.e. it is
	// deliberately KEPT rather than removed.
	if !strings.Contains(body, `[data-empty-line="true"]`) {
		t.Error("the served main.js does not preserve empty rendered lines when copying: the 1.5.18 blank-line fix is missing")
	}
	// (b) the code-line re-join pass exists.
	if !strings.Contains(body, `querySelectorAll(".code-line")`) {
		t.Error("the served main.js does not re-join .code-line rows when copying a code view")
	}

	// The 1.5.17 shape must be gone: its chrome selector listed the removals
	// without the empty-line marker.
	const withoutFix = `".select-none, .line-number, button, svg, script, iframe, object, embed, noscript, style"`
	if strings.Contains(body, withoutFix) {
		t.Error("the served main.js still carries the 1.5.17 copy selector: blank lines would be dropped again")
	}
}

// TestBundleRendersMathInSubagentReplies pins the subagent-LaTeX fix.
//
// A step's fallback markdown used to be rendered with math hard-disabled
// (`disableMath:!0`), so a subagent's `$…$` reached the browser as raw TeX.
// 1.5.18 passes the caller's decision through instead (`disableMath:f`), and
// the step renderer's call site computes that decision per step
// (`disableMath:!Scd(a)`) rather than pinning it.
//
// Both halves are asserted: the pass-through alone would still be a constant
// `true` if the call site kept overriding it.
func TestBundleRendersMathInSubagentReplies(t *testing.T) {
	body := servedMainJS(t)

	const withFix = `?F.createElement(h,{disableMath:f},a):null`
	const withoutFix = `?F.createElement(g,{disableMath:!0},a):null`

	if !strings.Contains(body, withFix) {
		t.Error("the served main.js does not pass the caller's disableMath decision into a step's markdown: the 1.5.18 subagent-LaTeX fix is missing")
	}
	if strings.Contains(body, withoutFix) {
		t.Error("the served main.js still hard-disables math on a step's markdown: LaTeX in subagent replies would show as raw TeX")
	}
	if !strings.Contains(body, `disableMath:!Scd(a)`) {
		t.Error("the served main.js does not compute the step's disableMath per step: subagent replies would keep math disabled")
	}
}

// TestIndexHTMLKeepsTheVoiceClientTag pins the one asset that is NOT a verbatim
// copy of the upstream bundle.
//
// `voice_client.js` is this repo's own addition: internal/rcfrontend serves it
// and voice_client_test.go asserts the tag is present. The upstream 1.5.18
// index.html has no such tag, so replacing the file wholesale — the right move
// for every other asset — would silently drop the feature.
func TestIndexHTMLKeepsTheVoiceClientTag(t *testing.T) {
	raw, err := fs.ReadFile(FS(), "index.html")
	if err != nil {
		t.Fatalf("read embedded index.html: %v", err)
	}
	if !strings.Contains(string(raw), `<script src="/voice_client.js"></script>`) {
		t.Fatal("the tracked index.html lost the voice_client.js script tag while being replaced with the 1.5.18 build")
	}
}
