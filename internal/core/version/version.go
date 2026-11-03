// Package version carries the version of the reference binary the clone's
// observable behaviour is measured against.
//
// It is a fact about the *reference*, not about the CLI: the help panel
// prints it, the Web UI's session state carries it, the update check
// compares against it and the remote-control handshake stamps it. Four
// surfaces that have nothing to do with each other all need the same
// string, so it cannot live in the one host that happened to define it
// first — the view that prints it would have to import a host to ask.
//
// The constant therefore lives in tier 1, next to the other facts the
// whole tree shares. `internal/cli` re-exports it as `cli.ReferenceVersion`
// for the hosts that already read it from there.
package version

// Reference is the version of the reference `agy` binary the clone's
// observable behaviour is measured against.
//
// Source of truth: `$ ~/.local/bin/agy --version` prints the literal
// string we mirror here. Cycle 283 moved to 1.4.3; 1.4.5 was the
// reference-drift-145 bump; 1.4.6 is the reference-drift-146 bump
// (the installed binary prints 1.4.6); 1.4.7 is the reference-drift-147
// bump (the installed binary prints 1.4.7); 1.4.8 is the cycle-350 bump
// (the installed binary prints 1.4.8 — see
// docs/cycles/cycle-350-agy-1.4.8-alignment/deliverable.md for the
// 1.4.7 → 1.4.8 delta); 1.4.9 is the cycle-362 bump (the installed
// binary prints 1.4.9 — see
// docs/cycles/cycle-362-agy-1.4.9-alignment/deliverable.md for the
// 1.4.8 → 1.4.9 delta); 1.4.10 is the cycle-363 bump (the installed
// binary prints 1.4.10 — see
// docs/cycles/cycle-363-agy-1.4.10-alignment/deliverable.md for the
// 1.4.9 → 1.4.10 delta); 1.4.11 is the cycle-365 bump (the installed
// binary prints 1.4.11 — see
// docs/cycles/cycle-365-agy-1.4.11-version-alignment/deliverable.md for
// the 1.4.10 → 1.4.11 delta); 1.4.12 is the cycle-377 bump (the installed
// binary prints 1.4.12 — see
// docs/cycles/cycle-377-agy-1.4.12-alignment/deliverable.md for the
// 1.4.11 → 1.4.12 delta).
const Reference = "1.4.12"
