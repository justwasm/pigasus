package agentserver

import (
	"os"
	"testing"

	"github.com/justwasm/pigasus/internal/testguard"
)

// TestMain keeps this package's configuration inside a temporary directory.
//
// The package's tests start real processes — the product's own binary, a shell,
// a proxy — and an inherited environment is the only thing that tells those
// processes where the configuration lives. Without it they resolve the
// machine's own home and write the user's accounts (and the pool's policy)
// over.
func TestMain(m *testing.M) {
	os.Exit(testguard.Run(m))
}
