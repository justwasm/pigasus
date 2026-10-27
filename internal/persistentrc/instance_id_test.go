// instance_id_test.go — the instance id helpers.
package persistentrc

import "testing"

// TestMintUUID_Shape pins the v4 shape: the version nibble is 4 and
// the variant nibble is one of 8/9/a/b.
func TestMintUUID_Shape(t *testing.T) {
	for i := 0; i < 16; i++ {
		id := mintUUID()
		if len(id) != 36 {
			t.Fatalf("mintUUID() = %q (len %d, want 36)", id, len(id))
		}
		if id[14] != '4' {
			t.Fatalf("mintUUID() = %q, want version nibble 4", id)
		}
		switch id[19] {
		case '8', '9', 'a', 'b':
		default:
			t.Fatalf("mintUUID() = %q, want a RFC 4122 variant nibble", id)
		}
	}
}

// TestIsUUID pins the accepted shapes. The check is deliberately
// strict — the id lands in a file name and in the remote service's
// registration metadata, so anything that is not the canonical
// 8-4-4-4-12 form is rejected.
func TestIsUUID(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"6ba7b810-9dad-11d1-80b4-00c04fd430c8", true},
		{"6BA7B810-9DAD-11D1-80B4-00C04FD430C8", true},
		{"00000000-0000-4000-8000-000000000000", true},
		{"", false},
		{"not-a-uuid", false},
		{"6ba7b8109dad11d180b400c04fd430c8", false},              // no dashes
		{"6ba7b810-9dad-11d1-80b4-00c04fd430c", false},           // too short
		{"6ba7b810-9dad-11d1-80b4-00c04fd430c88", false},         // too long
		{"6ba7b810_9dad-11d1-80b4-00c04fd430c8", false},          // wrong separator
		{"6ba7b810-9dad-11d1-80b4-00c04fd430cg", false},          // non-hex
		{"urn:uuid:6ba7b810-9dad-11d1-80b4-00c04fd430c8", false}, // prefixed
	}
	for _, c := range cases {
		if got := isUUID(c.in); got != c.want {
			t.Errorf("isUUID(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}
