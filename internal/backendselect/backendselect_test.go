// backendselect_test.go — the transport dispatch's own contract.
//
// Cycle 361: the account router is installed whether the policy is on
// or not. The policy is read per turn, so a policy the user turns on
// from the TUI's own panel takes effect on the next turn — a transport
// built without the router would have to be rebuilt first, and the
// user would have to restart the clone.
package backendselect

import (
	"io"
	"testing"

	"github.com/justwasm/pigasus/internal/accountrouting"
	"github.com/justwasm/pigasus/internal/backend"
)

// TestTheAccountRouterIsInstalledEvenWithoutAPolicy pins the live
// toggle: the transport the reference's models run on is routed through
// the account pool whether the configuration file carries a policy or
// not.
func TestTheAccountRouterIsInstalledEvenWithoutAPolicy(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	t.Setenv("OPENAGY_CONFIG", t.TempDir()+"/config.json")

	client := newOfficialBackend("Gemini 3.8 Flash", io.Discard, func(string) string {
		return "gemini-3.8-flash"
	})
	if _, ok := client.(*backend.CloudCodeClient); ok {
		t.Fatal("the transport is not routed through the account pool")
	}
	// The pool wraps the reference's own transport, and the wrapper
	// hands the inner client back for the callers that need it.
	inner, ok := accountrouting.Unwrap(client).(*backend.CloudCodeClient)
	if !ok {
		t.Fatalf("the routed transport wraps %T, want the CloudCode client", accountrouting.Unwrap(client))
	}
	if inner == nil {
		t.Fatal("the routed transport has no inner client")
	}
}
