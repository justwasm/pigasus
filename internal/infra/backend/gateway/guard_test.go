package gateway

import (
	"os"
	"testing"

	"github.com/justwasm/pigasus/internal/infra/backend/retrytest"
	"github.com/justwasm/pigasus/internal/testguard"
)

// TestMain keeps this package off the machine's own configuration and off the
// production retry schedule.
//
// The guard: the transport reads the resolved endpoint table, and the launcher
// exports `CLOUD_CODE_URL` — plus the sandbox and voice endpoints — for the
// reference binary. `go test` inherits those, and the environment outranks even
// the configuration file the guard pins.
//
// The schedule: a mid-stream 429/5xx is retried now (AGY 1.5.0's rule), so a
// test whose fake server answers the same failing frame on every attempt burns
// the whole production budget — nine retries, capped at four minutes each,
// 8m15s in total — unless it pins the budget. A package-wide shortcut means a
// test that forgets the pin costs milliseconds instead of a coffee break;
// retrytest.NoRetries is still what a classification test asserts with.
func TestMain(m *testing.M) {
	restore := retrytest.Short()
	code := testguard.Run(m)
	restore()
	os.Exit(code)
}
