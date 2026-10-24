// Package cli — version.go: the single source of truth for the reference
// binary version this clone tracks.
//
// The parity target moved 1.3.5 → 1.3.6 in cycle 97 (see
// docs/cycles/cycle-97/deliverable-A-tool-calling.md, "Reference drift"),
// 1.3.6 → 1.4.0 in cycle 181, then 1.4.0 → 1.4.2 in cycle rc-polish
// (fresh-eyes re-investigation gap G12: `~/.local/bin/agy --version`
// prints `1.4.2`, not `1.4.0`). Every user-visible version string in
// the clone reads this constant, so the next bump is a one-line change.
package cli

// ReferenceVersion is the version of the reference `agy` binary the clone's
// observable behaviour is measured against.
//
// Source of truth: `$ ~/.local/bin/agy --version` prints the literal
// string we mirror here. Cycle 283 moved to 1.4.3; 1.4.5 was the
// reference-drift-145 bump; 1.4.6 is the reference-drift-146 bump
// (the installed binary prints 1.4.6).
const ReferenceVersion = "1.4.6"
