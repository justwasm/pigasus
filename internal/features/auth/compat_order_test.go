package auth

import (
	"strings"
	"testing"
)

// TestRenderJetskiStatePbtxt_MigrationsAreOrdered pins the migration
// blocks' order.
//
// The blocks come out of a Go map, and a map iterates in a random
// order — so without an explicit sort the file's bytes differ from
// run to run, and the reference's own file does not. The order is the
// key order.
func TestRenderJetskiStatePbtxt_MigrationsAreOrdered(t *testing.T) {
	migrations := map[uint32]string{
		3: "MIGRATION_STATUS_COMPLETED",
		1: "MIGRATION_STATUS_PENDING",
		7: "MIGRATION_STATUS_SKIPPED",
	}
	want := []string{
		"  key:  1\n  value:  MIGRATION_STATUS_PENDING",
		"  key:  3\n  value:  MIGRATION_STATUS_COMPLETED",
		"  key:  7\n  value:  MIGRATION_STATUS_SKIPPED",
	}
	// Run it a few times: a map's iteration order is randomised per
	// range statement, so one pass could agree by luck.
	for i := 0; i < 20; i++ {
		out := string(renderJetskiStatePbtxt("uuid", nil, migrations))
		pos := -1
		for _, block := range want {
			at := strings.Index(out, block)
			if at < 0 {
				t.Fatalf("missing block %q in:\n%s", block, out)
			}
			if at <= pos {
				t.Fatalf("block %q is out of order in:\n%s", block, out)
			}
			pos = at
		}
	}
}
