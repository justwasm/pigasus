package agyproxy

import (
	"os"
	"testing"
)

// TestMain points the clone's configuration home at a scratch directory for
// the whole package.
//
// The proxy's state lives in the configuration document now, and the
// document lives in the configuration home. A test that reads or writes it
// without an override would otherwise reach the user's own ~/.openagy — and
// would write the user's own configuration.
func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "agyproxy-home")
	if err != nil {
		panic(err)
	}
	os.Setenv("OPENAGY_CONFIG_HOME", dir)
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}
