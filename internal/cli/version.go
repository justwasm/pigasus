// Package cli — version.go: the reference version the CLI reports.
//
// The constant itself lives in tier 1 (`internal/core/version`): the
// help panel, the Web UI's session state and the remote-control
// handshake all print it, and none of them may import a host to read
// it. This file keeps the name the CLI's own code already uses.
package cli

import "github.com/justwasm/pigasus/internal/core/version"

// ReferenceVersion is the version of the reference `agy` binary the clone's
// observable behaviour is measured against. See `internal/core/version`.
const ReferenceVersion = version.Reference
