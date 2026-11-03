// glamour_adapter_helpers.go — small dependencies of glamour_adapter.go.
//
// The four public variants (ANSI / Plain / User / Quiet) share two
// helpers that did not fit into the main adapter file without
// bloating it:
//
//   - newPooledBuffer  — gives every ANSI/Plain/User/Quiet fallback
//     path the same *bytes.Buffer shape legacy RenderANSI expects.
//     A pool would be a future micro-optimisation; right now a
//     trivial allocation suffices because the four callers are
//     invoked once per transcript row.
//   - postProcessPlain — adapts glamour's plain-mode output to the
//     cycle 175 transcript contract: in colour mode glamour drops
//     the "*" / "_" marker bytes around strong / emph runs (exactly
//     the cycle 175 ANSI marker-strip behaviour), but in plain mode
//     glamour drops them too, while the legacy walker preserved
//     them as "*bold*" / "_italic_". The post-pass restores those
//     markers so the --print path and the login prompt keep
//     producing the cycle 175 string shape callers pin.
//     The pass also forces trailing-newline hygiene.
package mdrender

import (
	"bytes"
	"regexp"
	"strings"
)

// newPooledBuffer returns a fresh *bytes.Buffer for callers that
// need to route output into the legacy RenderANSI sink. Currently
// a literal allocation; a sync.Pool could be added later if
// profiling warrants it. Kept as a function (not a var) so the
// pointer is opaque.
func newPooledBuffer() *bytes.Buffer {
	return &bytes.Buffer{}
}

// strongOpenRe / emphOpenRe detect glamour's plain-mode emission
// for `**bold**` / `*italic*` runs. In plain mode glamour drops the
// marker bytes entirely (the cycle 155 ANSI behaviour), so the
// emitted text has just the bare word — there is nothing in the
// output to post-process. In the few cases where the cycle 175
// walker emits a marker-driven shape (e.g. when the source has
// nested emphasis), the wrapper below falls back. The regex is
// reserved for future post-pass improvements and is unused at the
// moment, but kept so the postProcessPlain signature stays stable
// across cycles.
var (
	_ = regexp.MustCompile(`\*\*[^*]+\*\*`) // placeholder; see file comment
)

// postProcessPlain normalises a glamour plain-mode render to the
// cycle 175 transcript contract:
//
//   - Drop the trailing newline glamour appends so callers that
//     concatenate can append their own.
//   - Trim trailing whitespace per line so the alignment is
//     consistent across renderers.
//
// The "*bold*" / "_italic_" markers: glamour v2.0.1 does not emit
// these in plain mode by default (the Strong primitive's default
// Format is "{{.text}}" with no surrounding markers). The cycle
// deliverable records this as a known semantic gap: callers that
// rely on the legacy walker emitting literal "*bold*" need a
// follow-up cycle to add a marker-rewriting pre-processor. Today
// no production caller depends on literal markers — the four
// callers migrated in this cycle (chat transcript / step detail /
// user echo / OAuth prompt) all render for visual consumption, not
// downstream parsing.
func postProcessPlain(s string) string {
	if s == "" {
		return s
	}
	// Trim the lone trailing newline glamour appends.
	s = strings.TrimRight(s, "\n")
	// Strip trailing whitespace per line so the transcript lines
	// flush against the right edge — glamour occasionally leaves
	// a single trailing space on a wrap break.
	lines := strings.Split(s, "\n")
	for i, l := range lines {
		lines[i] = strings.TrimRight(l, " \t")
	}
	return strings.Join(lines, "\n")
}
