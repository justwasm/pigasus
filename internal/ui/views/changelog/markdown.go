// markdown.go — the reference's own /changelog document.
//
// The blob is the release-notes markdown the reference carries inside its
// binary and prints verbatim for `agy -p /changelog`. It is 251,052 bytes,
// captured byte for byte from the 1.5.14 binary at file offset 0x4692429
// (sha256: 2774eabe676dfed43160388c019ba786e26bcc59e996ffc610e28bf38268ebc0).
//
// The blob's newest section is 1.5.13 — the binary ships its own release
// notes one release behind.
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
