package accountrouting

import (
	"testing"
)

func TestFacadeAccountRouting(t *testing.T) {
	if DebugEnvVar != "OPENAGY_ACCOUNT_POOL_DEBUG" {
		t.Fatalf("unexpected DebugEnvVar: %s", DebugEnvVar)
	}
	r := Shared()
	if r == nil {
		t.Fatal("expected Shared() to return non-nil Router")
	}
	_ = Enabled()
	_ = Policy()
}
