package gemini

import (
	"os"
	"testing"

	"github.com/justwasm/pigasus/internal/infra/backend/retrytest"
)

// TestMain shortens the production retry schedule for the whole binary.
//
// GeminiClient.run retries a mid-stream 429/5xx exactly as the gateway and
// CloudCode transports do (retry.IsConnectionDropped || apierror.IsRetryableStatusErr),
// and the production schedule is 1s/2s/4s… capped at four minutes — 8m15s over
// the nine-retry budget. A test whose fake server answers the same failing
// frame on every attempt therefore pays that whole budget unless it pins the
// budget itself. The shortcut means such a test costs milliseconds; a test that
// *asserts* the classification still pins the budget with retrytest.NoRetries,
// which is the stronger statement.
func TestMain(m *testing.M) {
	restore := retrytest.Short()
	code := m.Run()
	restore()
	os.Exit(code)
}
