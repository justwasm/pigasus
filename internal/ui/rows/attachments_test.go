// attachments_test.go — the attachment indicator's own bytes.
//
// The indicator is pinned by its exact byte sequence: the difference
// between the collapsed and the expanded shape is a glyph and a hint, and
// the difference between the collapsed one and the collapsed one with a
// single attachment is the attachment's own name — none of which a
// rendered comparison shows.
//
// The golden constants come from rows_test.go, which pins the palette's
// default scheme to them.

package rows

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/features/tools/clipboard"
)

// attachmentMedia is one attachment with a known name, format and size.
func attachmentMedia(name string, size int64) clipboard.Media {
	return clipboard.Media{Filename: name, Format: 1, Size: size, Data: []byte{1, 2, 3}}
}

// TestRows_AttachmentRowsCollapsed pins the collapsed indicator: the
// paperclip glyph, the count, and the hint that expands it.
func TestRows_AttachmentRowsCollapsed(t *testing.T) {
	one := []clipboard.Media{attachmentMedia("shot.png", 24000)}
	want := []string{
		goldenDimGrayOpen + "  ▸ 📎 1 media attached (shot.png, PNG, 23 KB)  (ctrl+o to expand)" + goldenClose39,
	}
	got := AttachmentRows(one, false)
	if len(got) != len(want) {
		t.Fatalf("AttachmentRows = %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}

	two := append(one, attachmentMedia("second.png", 1024))
	want = []string{
		goldenDimGrayOpen + "  ▸ 📎 2 media attached  (ctrl+o to expand)" + goldenClose39,
	}
	got = AttachmentRows(two, false)
	if len(got) != len(want) {
		t.Fatalf("AttachmentRows = %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRows_AttachmentRowsExpanded pins the expanded indicator: the
// chevron header, and one row per attachment.
func TestRows_AttachmentRowsExpanded(t *testing.T) {
	media := []clipboard.Media{
		attachmentMedia("shot.png", 24000),
		attachmentMedia("second.png", 1024),
	}
	want := []string{
		goldenDimGrayOpen + "  ▾ 📎 2 Media(s) attached  (ctrl+o to collapse)" + goldenClose39,
		goldenDimGrayOpen + "    1. shot.png · PNG · 23 KB" + goldenClose39,
		goldenDimGrayOpen + "    2. second.png · PNG · 1 KB" + goldenClose39,
	}
	got := AttachmentRows(media, true)
	if len(got) != len(want) {
		t.Fatalf("AttachmentRows = %d rows, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("row %d = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestRows_AttachmentRowsNameTheAttachment pins that the collapsed row
// with a single attachment names it.
func TestRows_AttachmentRowsNameTheAttachment(t *testing.T) {
	if got := AttachmentRows(nil, false); len(got) != 0 {
		t.Fatalf("AttachmentRows(nil) = %v, want none", got)
	}
	got := AttachmentRows([]clipboard.Media{attachmentMedia("pasted-image-1.png", 4096)}, false)
	if len(got) != 1 {
		t.Fatalf("AttachmentRows = %d rows, want 1", len(got))
	}
	if !strings.Contains(got[0], "pasted-image-1.png") {
		t.Errorf("row %q does not name the attachment", got[0])
	}
}
