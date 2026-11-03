// error_row_test.go — the failed turn's row.
package toolsteps

import "testing"

// TestErrorRow_PinsTheReferenceRow pins the row's own bytes: the
// reference prints "Error: " followed by the failure's message.
func TestErrorRow_PinsTheReferenceRow(t *testing.T) {
	if got := ErrorRow("context deadline exceeded"); got != "Error: context deadline exceeded" {
		t.Errorf("ErrorRow = %q, want 'Error: context deadline exceeded'", got)
	}
}

// TestErrorRow_TrimsTheMessage pins that a message carrying a
// trailing newline still renders one clean row.
func TestErrorRow_TrimsTheMessage(t *testing.T) {
	if got := ErrorRow("  boom  \n"); got != "Error: boom" {
		t.Errorf("ErrorRow = %q, want 'Error: boom'", got)
	}
}
