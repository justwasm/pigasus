// TestMain — wire up the sandbox's ALL_PROXY env into Go's standard
// HTTP_PROXY / HTTPS_PROXY variables that net/http.ProxyFromEnvironment
// actually consults. Without this, tests running in the sandbox can't
// reach the public internet.
package auth

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	// ALL_PROXY (sandbox / curl convention) → HTTP_PROXY / HTTPS_PROXY (Go convention)
	if v := os.Getenv("ALL_PROXY"); v != "" {
		if os.Getenv("HTTP_PROXY") == "" {
			os.Setenv("HTTP_PROXY", v)
		}
		if os.Getenv("HTTPS_PROXY") == "" {
			os.Setenv("HTTPS_PROXY", v)
		}
	}
	// Default: skip network tests unless explicitly enabled
	if os.Getenv("ENABLE_LIVE_NETWORK") != "1" {
		os.Setenv("GO_TEST_NO_NET", "1")
	}
	os.Exit(m.Run())
}
