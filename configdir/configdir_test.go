package configdir

import (
	"path/filepath"
	"testing"
)

// TestDirPrefersTheEnvOverride pins the resolution order: the explicit
// configuration home wins over the convention, and the convention is
// $HOME/.openagy.
func TestDirPrefersTheEnvOverride(t *testing.T) {
	t.Setenv(EnvHome, "/tmp/explicit")
	t.Setenv("HOME", "/tmp/home")
	if got := Dir(); got != "/tmp/explicit" {
		t.Errorf("Dir() = %q, want %q", got, "/tmp/explicit")
	}
	t.Setenv(EnvHome, "")
	if got := Dir(); got != filepath.Join("/tmp/home", ".openagy") {
		t.Errorf("Dir() = %q, want %q", got, filepath.Join("/tmp/home", ".openagy"))
	}
	t.Setenv("HOME", "")
	if got := Dir(); got != "" {
		t.Errorf("Dir() without a home = %q, want the empty string", got)
	}
}

// TestPathIsEmptyWithoutAHome pins that a process with no home has no
// path, rather than a path under "/.openagy".
func TestPathIsEmptyWithoutAHome(t *testing.T) {
	t.Setenv(EnvHome, "")
	t.Setenv("HOME", "")
	if got := Path("config.json"); got != "" {
		t.Errorf("Path() = %q, want the empty string", got)
	}
}
