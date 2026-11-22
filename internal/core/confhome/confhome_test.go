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

func TestAntigravity_Precedence(t *testing.T) {
	t.Setenv("OPENAGY_DATA_DIR", "")
	t.Setenv("ANTIGRAVITY_DATA_DIR", "")
	t.Setenv("ANTIGRAVITY_HOME", "")
	t.Setenv("HOME", "/custom/home")

	wantDefault := filepath.Join("/custom/home", ".gemini", "antigravity-cli")
	if got := confhome.Antigravity(); got != wantDefault {
		t.Errorf("Antigravity() default = %q, want %q", got, wantDefault)
	}

	t.Setenv("ANTIGRAVITY_HOME", "/custom/agy_home")
	if got := confhome.Antigravity(); got != "/custom/agy_home" {
		t.Errorf("Antigravity() with ANTIGRAVITY_HOME = %q, want /custom/agy_home", got)
	}

	t.Setenv("ANTIGRAVITY_DATA_DIR", "/custom/agy_data")
	if got := confhome.Antigravity(); got != "/custom/agy_data" {
		t.Errorf("Antigravity() with ANTIGRAVITY_DATA_DIR = %q, want /custom/agy_data", got)
	}

	t.Setenv("OPENAGY_DATA_DIR", "/custom/openagy_data")
	if got := confhome.Antigravity(); got != "/custom/openagy_data" {
		t.Errorf("Antigravity() with OPENAGY_DATA_DIR = %q, want /custom/openagy_data", got)
	}
}

func TestConfhome_DerivedDirs(t *testing.T) {
	t.Setenv("OPENAGY_DATA_DIR", "/custom/data/antigravity-cli")
	if got := confhome.SkillsDir(); got != "/custom/data/antigravity-cli/skills" {
		t.Errorf("SkillsDir() = %q, want /custom/data/antigravity-cli/skills", got)
	}
	if got := confhome.SharedSkillsDir(); got != "/custom/data/skills" {
		t.Errorf("SharedSkillsDir() = %q, want /custom/data/skills", got)
	}
	if got := confhome.BrainDir("c1"); got != "/custom/data/antigravity-cli/brain/c1" {
		t.Errorf("BrainDir() = %q, want /custom/data/antigravity-cli/brain/c1", got)
	}
	if got := confhome.ConversationsDir(); got != "/custom/data/antigravity-cli/conversations" {
		t.Errorf("ConversationsDir() = %q, want /custom/data/antigravity-cli/conversations", got)
	}
}
