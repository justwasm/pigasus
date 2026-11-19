package rcfrontend

import (
	"os"
	"testing"

	"github.com/justwasm/pigasus/internal/testguard"
)

// TestMain pins this package's configuration at a temporary directory, so no
// test — including one whose own isolation has already been restored — can
// write the machine's accounts, policy or endpoints.
func TestMain(m *testing.M) { os.Exit(testguard.Run(m)) }
