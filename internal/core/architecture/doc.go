// Package architecture guards the tree's layering rules.
//
// The clone is laid out in five tiers under internal/, numbered by
// dependency order:
//
//	core (1) < features (2) < infra (3) < app (4) < ui (5)
//
// A tier-N package may import the tiers below it and nothing above it.
// Above every tier sit the hosts — the seven packages that own the turn
// engines or run a session — and at the module's root sit the public
// packages, the compatibility promise the tree makes to third parties.
//
// The rules are written down in AGENTS.md ("Load-bearing invariants") and
// in docs/guide/architecture-tiers.md. The guard in architecture_test.go
// is the same measurement devutils/tiergraph.py performs, moved into the
// tree so that a violation fails `go test` instead of waiting for
// somebody to remember the script.
//
// The guard asserts:
//
//	R1  no upward edge — a tier-N package imports no tier-M package with
//	    M > N. The rule governs the shipped graph; an upward edge in the
//	    test-only graph is reported, not failed, because a test may reach
//	    across the tiers without shipping the dependency.
//
//	R2  no tier package imports a host, with one documented exemption:
//	    the app tier (4) may reach the turn engines (internal/modular and
//	    internal/cli), because AGENTS.md says only the app tier may use
//	    them. The two entry surfaces (internal/tui and internal/printmode)
//	    sit above every tier, so no tier imports them. The number of
//	    exempted edges is printed on every run, so the exemption stays
//	    visible.
//
//	R3  no ui-tier package imports a host. This is R2's ui half, kept
//	    as its own rule because it is the one the tree cares most about.
//
//	R4  no import cycle in the production graph.
//
//	R5  no import cycle in the test-only graph.
//
//	R6  the public packages import no internal/ package.
//
// Two findings do not hold as written and are excused by name in the
// test file, each with a one-line reason: the test-only cycle between
// remotetransport and internal/remotecontrol, and remotetransport's
// test-only import of internal/remotecontrol. Both live entirely in
// _test.go files, and remotetransport's shipped code imports nothing
// from internal/. The list is printed on every run, and an exception
// that no longer excuses anything is reported as stale.
//
// The guard measures the whole module, because the public-package rule
// is about the module's root packages, and it reports the internal/
// subtree's own numbers next to the module-wide ones — the numbers
// devutils/tiergraph.py prints.
//
// The guard reads the tree with go/parser and go/build and imports
// nothing outside the standard library, so it can sit in the core tier
// itself: it measures the tree rather than being part of it.
package architecture
