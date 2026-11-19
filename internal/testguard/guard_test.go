package testguard

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMain(m *testing.M) { os.Exit(Run(m)) }

// The guard has to hold for every path the product writes: the four files and
// the two homes. A variable it forgets is a variable a test can follow to the
// machine's own configuration.
func TestInstallPinsEveryPath(t *testing.T) {
	home := HomeForTest(t)
	if !strings.HasPrefix(home, os.TempDir()) {
		t.Errorf("home = %q, want a directory under %q", home, os.TempDir())
	}
	cases := map[string]string{
		"HOME":                     home,
		"OPENAGY_CONFIG_HOME":      home,
		"OPENAGY_CONFIG":           filepath.Join(home, "config.json"),
		"OPENAGY_ACCOUNTS_CONFIG":  filepath.Join(home, "accounts.json"),
		"OPENAGY_PROVIDERS_CONFIG": filepath.Join(home, "providers.json"),
	}
	for name, want := range cases {
		if got := os.Getenv(name); got != want {
			t.Errorf("%s = %q, want %q", name, got, want)
		}
	}
	if os.Getenv("ANTIGRAVITY_HOME") == "" {
		t.Error("ANTIGRAVITY_HOME is not pinned: the vendor CLI's home would be the real one")
	}
	// The credentials are not read either.
	for _, name := range []string{"JETSKI_OAUTH_TOKEN", "ANTIGRAVITY_REFRESH_TOKEN"} {
		if got := os.Getenv(name); got != "" {
			t.Errorf("%s = %q, want it cleared", name, got)
		}
	}
	if os.Getenv("OPENAGY_DISABLE_KEYCHAIN") != "1" {
		t.Error("the keychain should be disabled for a test binary")
	}
}

// The pinned paths must not exist yet: the guard creates the directory, not the
// files, so a test that expects "a first run with nothing on disk" still gets
// one.
func TestInstallLeavesTheFilesAbsent(t *testing.T) {
	home := HomeForTest(t)
	for _, name := range []string{"config.json", "accounts.json", "providers.json"} {
		if _, err := os.Stat(filepath.Join(home, name)); err == nil {
			t.Errorf("%s exists; the guard must not write configuration", name)
		}
	}
}
