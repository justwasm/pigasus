package terminal

import (
	"os"
	"testing"

	"github.com/justwasm/pigasus/internal/testguard"
)

// Exercise the production login shell without sourcing the operator's profile
// or inheriting their configuration and credentials.
func TestMain(m *testing.M) { os.Exit(testguard.Run(m)) }

func TestTerminalScenariosUseIsolatedHome(t *testing.T) {
	want := testguard.HomeForTest(t)
	if got := os.Getenv("HOME"); got != want {
		t.Fatalf("PTY fixture HOME = %q, want test home %q", got, want)
	}
}
