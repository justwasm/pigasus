// Package testguard keeps a test binary's configuration inside a temporary
// directory.
//
// # Why it exists
//
// `accounts.json` holds the account registry *and* the pool's routing policy;
// `config.json` holds the endpoint overrides and the proxy's own state. A test
// that rewrites either one does not dirty a fixture — it takes the machine's
// own configuration with it, and the user finds their pool gone.
//
// Per-test isolation is not enough on its own. A package's tests share one
// process, and `t.Setenv` restores the environment when a test ends: a write
// that runs after the restore — a deferred save, a background goroutine that
// resolves the path a moment later, a later test that forgets to isolate —
// lands on the real file. The failure is silent, and it is the *next* test
// that looks broken.
//
// So each test package calls Install from its own `TestMain`, before any test
// runs. The environment points at a temporary directory for the whole process,
// and a caller that sets its own variables still overrides that.
//
// # Using it
//
//	func TestMain(m *testing.M) {
//		os.Exit(testguard.Run(m))
//	}
package testguard

import (
	"go/build"
	"os"
	"path/filepath"
	"testing"
)

// preserveGoEnv ensures the Go toolchain caches and paths are pinned to the real
// developer / system locations before HOME is redirected to a temporary directory.
//
// Without this, any test that runs `go run` or `go test` in a subprocess calculates
// GOPATH and GOMODCACHE under the temporary $HOME, finds an empty module cache, and
// attempts to download dependencies over the network during test execution.
func preserveGoEnv() {
	if os.Getenv("GOPATH") == "" && build.Default.GOPATH != "" {
		os.Setenv("GOPATH", build.Default.GOPATH)
	}
	if os.Getenv("GOROOT") == "" && build.Default.GOROOT != "" {
		os.Setenv("GOROOT", build.Default.GOROOT)
	}
	if os.Getenv("GOMODCACHE") == "" {
		if gp := os.Getenv("GOPATH"); gp != "" {
			os.Setenv("GOMODCACHE", filepath.Join(gp, "pkg", "mod"))
		}
	}
	if os.Getenv("GOCACHE") == "" {
		if cacheDir, err := os.UserCacheDir(); err == nil && cacheDir != "" {
			os.Setenv("GOCACHE", filepath.Join(cacheDir, "go-build"))
		}
	}
}

// Home returns the directory Install points the configuration at. It is empty
// until Install or Run has been called.
var home string

// Install points every configuration path at a fresh temporary directory and
// returns it.
//
// The variables it sets are the ones the product reads:
//
//	HOME                        the fallback home
//	OPENAGY_CONFIG_HOME         the clone's configuration home
//	OPENAGY_CONFIG              ~/.openagy/config.json
//	OPENAGY_ACCOUNTS_CONFIG     ~/.openagy/accounts.json
//	OPENAGY_PROVIDERS_CONFIG    ~/.openagy/providers.json
//	ANTIGRAVITY_HOME            the vendor CLI's own home
//
// It also turns the keychain off and clears the token variables, so no test
// reads or writes the machine's credentials either.
//
// A directory that cannot be created leaves the environment alone: a test
// binary that cannot isolate is still better off running than failing to start,
// and the per-test helpers that follow still set their own paths.
func Install() string {
	preserveGoEnv()
	dir := InstallConfigOnly()
	if dir == "" {
		return ""
	}
	os.Setenv("HOME", dir)
	os.Setenv("ANTIGRAVITY_HOME", filepath.Join(dir, ".gemini", "antigravity-cli"))
	return dir
}

// InstallConfigOnly pins the four configuration files and leaves $HOME and the
// vendor CLI's home alone, for a package that has its own plan for them.
//
// The configuration still resolves under the temporary directory, because every
// reader prefers the explicit variable — `OPENAGY_CONFIG_HOME`,
// `OPENAGY_ACCOUNTS_CONFIG`, `OPENAGY_PROVIDERS_CONFIG` — over $HOME.
func InstallConfigOnly() string {
	preserveGoEnv()
	dir, err := os.MkdirTemp("", "pigasus-test-home-")
	if err != nil {
		return ""
	}
	home = dir
	os.Setenv("OPENAGY_CONFIG_HOME", dir)
	os.Setenv("OPENAGY_CONFIG", filepath.Join(dir, "config.json"))
	os.Setenv("OPENAGY_ACCOUNTS_CONFIG", filepath.Join(dir, "accounts.json"))
	os.Setenv("OPENAGY_PROVIDERS_CONFIG", filepath.Join(dir, "providers.json"))
	os.Setenv("OPENAGY_DISABLE_KEYCHAIN", "1")
	os.Setenv("JETSKI_OAUTH_TOKEN", "")
	os.Setenv("ANTIGRAVITY_REFRESH_TOKEN", "")
	return dir
}

// Run is Install plus `m.Run` plus the cleanup, for a package's TestMain:
//
//	func TestMain(m *testing.M) { os.Exit(testguard.Run(m)) }
func Run(m *testing.M) int {
	dir := Install()
	code := m.Run()
	if dir != "" {
		_ = os.RemoveAll(dir)
	}
	return code
}

// HomeForTest returns the isolated home, and fails the test when the package
// forgot to install one. It exists so a package's tests can assert that the
// guard is in place rather than trusting it.
func HomeForTest(t *testing.T) string {
	t.Helper()
	if home == "" {
		t.Fatal("this package's TestMain did not call testguard.Install: a test could write the real configuration")
	}
	return home
}
