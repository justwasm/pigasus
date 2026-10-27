// trajectory_id_test.go — the trajectory id is a UUIDv5 over the
// workspace URI.
//
// The id is opaque to the reference (it mints a random one), but the
// clone derives it so the file name is stable across sessions. That
// makes it a *value* the clone owns, so it is pinned here: a swap of
// the UUID implementation that changes these bytes would rename every
// trajectory file the clone has already written.
package implicit

import "testing"

// TestTrajectoryID_Pinned pins the UUIDv5 values for a fixed
// namespace and workspace URI. The expected strings were computed
// independently (RFC 4122 v5 = SHA-1 over namespace || name, with the
// version and variant nibbles set).
func TestTrajectoryID_Pinned(t *testing.T) {
	cases := []struct {
		workspace string
		want      string
	}{
		{"file:///tmp/example", "412b9fd1-4f58-57f9-b854-2cfc12cccd4e"},
		{"file:///workspace/one", "cb084940-01fd-598c-a85a-ab5ee322dbd2"},
	}
	for _, c := range cases {
		got := trajectoryID(c.workspace)
		if got != c.want {
			t.Errorf("trajectoryID(%q) = %q, want %q", c.workspace, got, c.want)
		}
	}
}

// TestNewUUIDv4_Shape pins the v4 shape: the version nibble is 4 and
// the variant nibble is one of 8/9/a/b.
func TestNewUUIDv4_Shape(t *testing.T) {
	for i := 0; i < 16; i++ {
		id := newUUIDv4()
		if len(id) != 36 {
			t.Fatalf("newUUIDv4() = %q (len %d, want 36)", id, len(id))
		}
		if id[14] != '4' {
			t.Fatalf("newUUIDv4() = %q, want version nibble 4", id)
		}
		switch id[19] {
		case '8', '9', 'a', 'b':
		default:
			t.Fatalf("newUUIDv4() = %q, want a RFC 4122 variant nibble", id)
		}
	}
}
