// Package slashcomplete holds the pure string-matching core of the
// composer's slash-command completion.
//
// # Module boundary
//
// The slash-command completion popup (internal/ui/views/slashpicker)
// owns the overlay's *state* — the filtered rows, the highlight, the
// scroll window, the on-screen rectangle — and the host that owns the
// composer (internal/tui) owns the *side effects*: reading the buffer,
// moving the cursor, syncing the vim engine.
//
// This package sits between them and owns neither. It is the pure,
// side-effect-free matching step:
//
//   - Match takes the buffer's leading slash token and the command
//     catalogue and returns the rows that match, with no mutation of
//     any host state.
//   - Complete resolves the single best completion text for a token
//     (the empty input, a unique match, a shared prefix of several
//     matches, or no match at all) and reports what kind of result it
//     is, again without touching the host.
//   - Handle runs Match + Complete and, when there is something to
//     write back, hands the resolved text to a Port. The Port is the
//     one and only side effect, and it is defined here as an interface
//     so the host (internal/tui) can satisfy it with a thin adapter
//     over its composer input.
//
// The package depends only on internal/ui/slashcommands (Tier 5) for
// the Command type and the scoped catalogue filter. It does not import
// internal/tui, RootModel, bubbletea, or any host type, so it is
// trivially testable without a RootModel.
package slashcomplete
