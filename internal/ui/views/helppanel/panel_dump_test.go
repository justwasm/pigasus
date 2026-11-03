package helppanel

import (
	"os"
	"strconv"
	"testing"

	"github.com/justwasm/pigasus/palette"
)

// TestDumpRows writes the overlay's rendered bytes to the file named
// by HELP_DUMP.
//
// It is the harness the byte proof runs on. Cycle 381 moved the
// /help overlay out of the host, so the proof compared two dumps: the
// host's own renderer before the move, and this one after it. The two
// were byte for byte identical.
//
//	HELP_DUMP=/tmp/help.txt go test ./internal/ui/views/helppanel/
//
// The dump is the input for the pinned bytes in panel_bytes_test.go.
func TestDumpRows(t *testing.T) {
	path := os.Getenv("HELP_DUMP")
	if path == "" {
		t.Skip("HELP_DUMP is unset")
	}
	restore := palette.PushScheme("solarized light")
	defer restore()

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create dump: %v", err)
	}
	defer f.Close()

	fixture := newFixture(false)
	for tab := 0; tab <= 2; tab++ {
		fixture.panel.Open(tab)
		f.WriteString("=== tab " + strconv.Itoa(tab) + " ===\n")
		f.WriteString(fixture.panel.Rows())
	}

	fixture.vimOn = true
	fixture.panel.Open(3)
	f.WriteString("=== tab 3 (vim) ===\n")
	f.WriteString(fixture.panel.Rows())
}
