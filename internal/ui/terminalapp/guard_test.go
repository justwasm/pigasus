package terminalapp

import (
	"os"
	"testing"

	"github.com/justwasm/pigasus/internal/testguard"
)

// Real PTY scenarios launch a login shell; keep its startup files and all
// configuration inside the test home rather than the operator's environment.
func TestMain(m *testing.M) { os.Exit(testguard.Run(m)) }

func TestTerminalScenariosUseIsolatedHome(t *testing.T) {
	want := testguard.HomeForTest(t)
	if got := os.Getenv("HOME"); got != want {
		t.Fatalf("PTY fixture HOME = %q, want test home %q", got, want)
	}
}
