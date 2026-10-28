// Package cli — changelog_data.go: the reference binary's `changelog`
// output, embedded verbatim.
//
// `agy changelog` prints the release-notes block bundled in the binary.
// The blob below is the v1.4.9 build's: 79 version sections (1.4.8 down
// through 1.0.0 — the newest section is one release *behind* the
// binary's own version, and 1.3.7/1.3.8/1.3.9 are still skipped
// between 1.4.0 and 1.3.6) totalling 180,524 bytes / 1,038
// lines. The reference output was captured byte-for-byte from
// `agy changelog` and committed as testdata/changelog_reference.txt;
// this file embeds it so the binary ships the exact same bytes.
//
// This supersedes the cycle-49 hand-transcribed 4,640-byte subset, which
// reproduced only the 1.3.5 + 1.3.4 bullets and truncated the remaining
// sections. Older blobs are available in git history (v1.4.0 at
// 153,988 bytes / 915 lines; v1.3.6 at 150,756 bytes / 900 lines;
// v1.3.5 at 148,631 bytes / 892 lines).
package cli

import _ "embed"

// referenceChangelog is the exact byte sequence `agy changelog` prints.
//
//go:embed testdata/changelog_reference.txt
var referenceChangelog string

// ReferenceChangelog returns the changelog blob the reference binary ships.
// cmd/wagyu's print-mode `/changelog` handler uses it so the command
// prints the same bytes as `agy changelog`.
func ReferenceChangelog() string { return referenceChangelog }
