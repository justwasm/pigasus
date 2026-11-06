// markdown_test.go — the reference's embedded /changelog document, pinned.
//
// The blob is a fixture that a stale embed would silently break: the
// command still exits 0, it just prints the wrong release notes. The
// pins are the reference's own bytes.
//
// 【一手】 agy 1.4.9, three runs on 2026-10-28 all returned sha256
// 0b911c6fb5187c9286ac2a318a432c8ae1bfdd3f9820edc720a4f13c5ed38d56
// for the blob the binary prints, and the blob is 180,070 bytes read
// out of the binary at file offset 63,893,369 — trailing newline
// included.
package changelog

import (
	"strings"
	"testing"
)

// TestMarkdownIsTheReferenceCapture pins the document to the REF v1.5.0
// capture: 188,570 bytes (the 1.4.9 capture was 180,070, the 1.4.8 capture was 176,788,
// the 1.4.0 capture 153,612 and the 1.3.6 capture 150,390) and prefixed by the
// 1.4.11 section — the binary ships its own release notes one release
// behind.
func TestMarkdownIsTheReferenceCapture(t *testing.T) {
	if len(Markdown) != 188570 {
		t.Errorf("embedded changelog is %d bytes, want 188570 (REF v1.5.0)", len(Markdown))
	}
	if !strings.Contains(Markdown, "\n## 1.4.11\n") {
		t.Error("embedded changelog has no 1.4.11 section; it is stale")
	}
	if !strings.HasSuffix(Markdown, "- Initial release of the Antigravity CLI.\n") {
		t.Error("embedded changelog does not end with the 1.0.0 section's last line")
	}
}
