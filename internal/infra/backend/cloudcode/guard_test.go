package cloudcode

import (
	"os"
	"testing"

	"github.com/justwasm/pigasus/internal/infra/backend/retrytest"
	"github.com/justwasm/pigasus/internal/testguard"
)

// TestMain keeps this package off the machine's own configuration and off the
// production retry schedule.
//
// The guard is load-bearing here rather than defensive: `isSandboxFailoverEligible`
// accepts a custom primary endpoint only when it equals the resolved
// `endpoints.Current().CloudCode`, and every client in these tests is built with
// that value. When the shell carries the launcher's own
// `CLOUD_CODE_URL=http://127.0.0.1:<port>`, "the primary" became the developer's
// proxy: TestCloudCodeImageSandboxFailover sent its primary request to the proxy
// and its mirror request to Google, then reported `calls primary/mirror = 0/1`.
//
// The schedule is shortened for the same reason as in the gateway package: a
// retryable status in a fake server's reply now costs the production schedule.
func TestMain(m *testing.M) {
	restore := retrytest.Short()
	code := testguard.Run(m)
	restore()
	os.Exit(code)
}
