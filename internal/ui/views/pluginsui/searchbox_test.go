package pluginsui

import (
	"strings"
	"testing"
)

func TestSearchBoxLine(t *testing.T) {
	// Not searching
	line := SearchBoxLine(true, false, false, false, false, false, "")
	if !strings.Contains(line, "Type to search plugins...") {
		t.Errorf("expected placeholder, got %q", line)
	}

	// Discover searching with blink on
	line = SearchBoxLine(false, true, true, false, false, true, "foo")
	if !strings.Contains(line, "Search: ") || !strings.Contains(line, "foo") || !strings.Contains(line, PluginSearchCursor) {
		t.Errorf("expected discover search line with cursor, got %q", line)
	}

	// Installed searching committed and asleep
	line = SearchBoxLine(true, false, true, true, true, false, "bar")
	if !strings.Contains(line, "Search: ") || !strings.Contains(line, "bar") || !strings.Contains(line, PluginInstalledSearchHintCommitted) {
		t.Errorf("expected committed search hint, got %q", line)
	}
}
