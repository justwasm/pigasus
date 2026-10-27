// title_test.go — Subagent.SetTitle's nil-receiver safety.
//
// Cycle 346: this test lived in internal/tools/stubs_bg_name_test.go,
// next to the create_task tests that drove the deleted v4 stub. The
// behaviour it pins belongs to internal/subagent, so the test moved
// here with it.
package subagent

import "testing"

// TestSubagentSetTitleNilReceiver — SetTitle must be safe on a nil
// Subagent so callers that branch on a non-nil Subagent can chain
// the call without a guard.
func TestSubagentSetTitleNilReceiver(t *testing.T) {
	var s *Subagent
	// Must not panic.
	s.SetTitle("safe")
}
