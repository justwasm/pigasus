// main_test.go — the /changelog package's test isolation.
//
// The panel reads the CLI's own cached copy of the published release notes
// (cache.go), and that copy lives in the user's home. A test that reached
// the user's home would show whatever the user's own `agy` had cached, so
// the whole package's test run points $ANTIGRAVITY_HOME at a scratch tree.
//
// This is the same rule the cliapp package's TestMain follows
// (internal/cliapp/main_test.go): the fixture must not read — or write —
// the user's own files.

package changelog_test

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	root, err := os.MkdirTemp("", "pigasus-changelog-home-")
	if err != nil {
		panic(err)
	}
	agy := filepath.Join(root, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(agy, 0o755); err != nil {
		panic(err)
	}
	if err := os.Setenv("ANTIGRAVITY_HOME", agy); err != nil {
		panic(err)
	}
	code := m.Run()
	_ = os.RemoveAll(root)
	os.Exit(code)
}
