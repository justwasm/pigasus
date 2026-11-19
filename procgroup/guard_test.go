package procgroup_test

import (
	"os"
	"path/filepath"
	"testing"
)

// TestMain keeps this package's configuration inside a temporary directory.
//
// The package is public, so it cannot import internal/testguard; the four
// variables below are the same ones that helper sets, and they are the ones
// every configuration reader prefers over $HOME. The package's tests start
// child processes, which inherit them — without the pin a child resolves the
// machine's own home and writes the user's accounts over.
func TestMain(m *testing.M) {
	if os.Getenv("GOPATH") == "" {
		if h, err := os.UserHomeDir(); err == nil && h != "" {
			os.Setenv("GOPATH", filepath.Join(h, "go"))
		}
	}
	if os.Getenv("GOMODCACHE") == "" {
		if gp := os.Getenv("GOPATH"); gp != "" {
			os.Setenv("GOMODCACHE", filepath.Join(gp, "pkg", "mod"))
		}
	}
	dir, err := os.MkdirTemp("", "pigasus-test-home-")
	if err == nil {
		os.Setenv("HOME", dir)
		os.Setenv("OPENAGY_CONFIG_HOME", dir)
		os.Setenv("OPENAGY_CONFIG", filepath.Join(dir, "config.json"))
		os.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(dir, "accounts.json"))
		os.Setenv("OPENAGY_PROVIDERS_CONFIG", filepath.Join(dir, "providers.json"))
		os.Setenv("OPENAGY_DISABLE_KEYCHAIN", "1")
	}
	code := m.Run()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	os.Exit(code)
}
