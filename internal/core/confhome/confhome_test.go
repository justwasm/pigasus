// confhome_test.go — the CLI's own log file's path.
package confhome_test

import (
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/core/confhome"
)

// TestCLILogPath pins the log file's name and the directory it hangs
// under.
//
// The reference writes one log file per launch into the `log` directory
// of its own home, and names it `cli-<timestamp>.log`.
func TestCLILogPath(t *testing.T) {
	t.Setenv("ANTIGRAVITY_HOME", "/tmp/agyclihome")
	got := confhome.CLILogPath("20261025_210717")
	want := filepath.Join("/tmp/agyclihome", "log", "cli-20261025_210717.log")
	if got != want {
		t.Errorf("CLILogPath() = %q, want %q", got, want)
	}
}
