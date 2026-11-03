// markdown.go — the reference's own /changelog document.
//
// The blob is the release-notes markdown the reference carries inside its
// binary and prints verbatim for `agy -p /changelog`. It is 180,070 bytes,
// captured byte for byte from the 1.4.9 binary (cycle 200 refreshed it from
// the 1.3.6 capture, which was 150,390 bytes and stopped at the 1.3.5
// section; cycle 351 refreshed it again from the 1.4.0 capture, which was
// 153,612 bytes and stopped at the 1.3.6 section).
//
// 【一手】 agy 1.4.9, three runs on 2026-10-28 returned the same sha256
// 0b911c6fb5187c9286ac2a318a432c8ae1bfdd3f9820edc720a4f13c5ed38d56,
// which is also the sha256 of the 180,070 bytes read out of the binary at
// file offset 63,893,369.
//
// The blob's newest section is 1.4.8 — the binary ships its own release
// notes one release behind, so the 1.4.9 notes live in the public
// repository only (see internal/cli/testdata/changelog_upstream_1.4.9.md).
//
// The document lives here, and not in the print path, because three
// surfaces show it: the print path's `/changelog` slash command, the
// TUI's `/changelog` panel and the panel's own wrapper. The panel *is*
// this package, so the data belongs to it.
package changelog

import _ "embed"

// Markdown is the reference's embedded /changelog document.
//
//go:embed testdata/print_changelog.md
var Markdown string
