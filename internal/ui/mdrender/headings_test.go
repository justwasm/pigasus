package mdrender

import (
	"strings"
	"testing"
)

// TestHeadings1512_NoHashMarkers verifies changelog 1.5.12 item 4:
// Markdown headings in agent responses and artifacts now show only their text,
// set apart by bold, italic, or underline, instead of starting with ## to ###### markers.
func TestHeadings1512_NoHashMarkers(t *testing.T) {
	cases := []struct {
		md       string
		wantText string
	}{
		{"## Section Title", "Section Title"},
		{"### Subsection Title", "Subsection Title"},
		{"#### Heading Four", "Heading Four"},
		{"##### Heading Five", "Heading Five"},
		{"###### Heading Six", "Heading Six"},
	}

	for _, tc := range cases {
		out, err := ANSI(tc.md, 80)
		if err != nil {
			t.Fatalf("ANSI(%q) error: %v", tc.md, err)
		}
		if strings.Contains(out, "##") || strings.Contains(out, "###") || strings.Contains(out, "####") || strings.Contains(out, "#####") || strings.Contains(out, "######") {
			t.Errorf("ANSI(%q) unexpectedly contains hash markers: %q", tc.md, out)
		}
		if !strings.Contains(out, tc.wantText) {
			t.Errorf("ANSI(%q) missing text %q: %q", tc.md, tc.wantText, out)
		}
	}
}
