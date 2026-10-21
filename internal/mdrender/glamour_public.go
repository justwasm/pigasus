// glamour_public.go — public re-exports for the glamour adapter
// helpers. The preprocessor internals live in glamour_preprocess.go
// and glamour_preprocess_blocks.go as unexported symbols because
// they are implementation details; this file gives callers a stable
// surface:
//
//	PreprocessFileLinks              — file-link URL rewriter
//	StripMermaidAndMath / Restore... — fenced-block placeholder swap
//
// These three functions are the glamour-specific entry points a
// transcript-style caller needs in addition to ANSI / Plain /
// User / Quiet. The two-name "Strip ... / Restore" pair is the
// cycle's preservation strategy for blocks glamour cannot render
// itself.
package mdrender

// PreprocessFileLinks is the public form of the markdown link
// destination rewriter. It walks src and replaces every matched
// `[label](url)` URL with render.NormalizeFileLink(url) so a
// `#L<n>` anchor becomes `%23L<n>`. The label is left unchanged,
// non-matched links pass through verbatim, and the function is
// idempotent.
//
// Pin in: TestGlamour_PreprocessFileLinks.
func PreprocessFileLinks(src string) string {
	return preprocessFileLinks(src)
}
