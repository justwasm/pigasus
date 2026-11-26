package terminalmgr

import (
	"os"
	"testing"

	"github.com/justwasm/pigasus/internal/testguard"
)

// Real-PTY integration scenarios still use the production shell launcher. Give
// it an isolated HOME/config so login shells cannot source the operator's
// profile or inherit credentials. This changes the fixture, not shell behavior.
func TestMain(m *testing.M) { os.Exit(testguard.Run(m)) }

func TestTerminalScenariosUseIsolatedHome(t *testing.T) {
	want := testguard.HomeForTest(t)
	if got := os.Getenv("HOME"); got != want {
		t.Fatalf("PTY fixture HOME = %q, want test home %q", got, want)
	}
}
