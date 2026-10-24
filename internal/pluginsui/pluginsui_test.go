package pluginsui

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/plugins"
)

func TestValidatePluginID(t *testing.T) {
	cases := []struct {
		id      string
		wantErr bool
	}{
		{"valid-plugin", false},
		{"valid_123.abc", false},
		{"", true},
		{"   ", true},
		{"../escape", true},
		{"sub/dir", true},
		{"sub\\dir", true},
		{".hidden", true},
		{"has null\x00byte", true},
		{"has spaces", true},
		{"special$char", true},
	}
	for _, tc := range cases {
		err := ValidatePluginID(tc.id)
		if (err != nil) != tc.wantErr {
			t.Errorf("ValidatePluginID(%q) error = %v, wantErr %v", tc.id, err, tc.wantErr)
		}
	}
}

func TestFilterInstalled(t *testing.T) {
	rows := []plugins.Row{
		{Name: "chrome-devtools", Desc: "Browser automation"},
		{Name: "science", Desc: "Lab skills"},
	}
	if got := FilterInstalled(rows, ""); len(got) != 2 {
		t.Fatalf("expected 2 rows for empty query, got %d", len(got))
	}
	if got := FilterInstalled(rows, "auto"); len(got) != 1 || got[0].Name != "chrome-devtools" {
		t.Fatalf("unexpected filter result: %v", got)
	}
	if got := FilterInstalled(rows, "SCIENCE"); len(got) != 1 || got[0].Name != "science" {
		t.Fatalf("unexpected filter result: %v", got)
	}
	if got := FilterInstalled(rows, "none"); len(got) != 0 {
		t.Fatalf("expected 0 matches, got %d", len(got))
	}
}

func TestFilterDiscover(t *testing.T) {
	mkts := []plugins.Marketplace{
		{
			Name: "official",
			Items: []plugins.Row{
				{Name: "plugin-a", Desc: "alpha item"},
				{Name: "plugin-b", Desc: "beta item"},
			},
		},
	}
	if got := FilterDiscover(mkts, ""); len(got) != 2 {
		t.Fatalf("expected 2 items, got %d", len(got))
	}
	if got := FilterDiscover(mkts, "official"); len(got) != 2 {
		t.Fatalf("marketplace name should match items, got %d", len(got))
	}
	if got := FilterDiscover(mkts, "beta"); len(got) != 1 || got[0].Name != "plugin-b" {
		t.Fatalf("expected plugin-b, got %v", got)
	}
}

func TestWrapWords(t *testing.T) {
	s := "the quick brown fox jumps over the lazy dog"
	chunks := WrapWords(s, 15)
	if len(chunks) < 2 {
		t.Fatalf("expected wrapping, got %v", chunks)
	}
	rejoined := strings.Join(chunks, " ")
	if rejoined != s {
		t.Fatalf("rejoined mismatch: got %q, want %q", rejoined, s)
	}
}

func TestPadRightAndTruncate(t *testing.T) {
	if got := PadRight("abc", 5); got != "abc  " {
		t.Fatalf("PadRight mismatch: got %q", got)
	}
	if got := TruncateTo("abcdef", 4); got != "a..." {
		t.Fatalf("TruncateTo mismatch: got %q, want %q", got, "a...")
	}
	if got := TruncateTo("abcdef", 10); got != "abcdef" {
		t.Fatalf("TruncateTo mismatch: got %q", got)
	}
}

func TestBadgesAndLines(t *testing.T) {
	bOn := Badge(true)
	if !strings.Contains(bOn, "[enabled]") {
		t.Fatalf("expected enabled badge, got %q", bOn)
	}
	bOff := Badge(false)
	if !strings.Contains(bOff, "[disabled]") {
		t.Fatalf("expected disabled badge, got %q", bOff)
	}
	line := InstalledLine(true, true, "test-plugin", "a test", 80)
	if !strings.Contains(line, "test-plugin") || !strings.Contains(line, "> ") {
		t.Fatalf("expected cursor line with test-plugin, got %q", line)
	}
}
