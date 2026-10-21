package convstore

import (
	"testing"
)

// TestProjectFromURIsBasename pins the basename extraction that the
// /resume picker relies on for its right-hand project column. The
// cwd-blank rule is exercised by the parity harness (zzz and x@26 on
// this machine), not here, because it requires the real working
// directory and a conversation started from it.
func TestProjectFromURIsBasename(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		// Plain JSON array of one URI.
		{`["file:///tmp/agycmp"]`, "agycmp"},
		// Bare URI, no array wrapper (the index column shape).
		{`file:///Users/gear/Documents/GitHub/openagy`, "openagy"},
		// Trailing slash stripped before basename.
		{`["file:///tmp/agycmp/"]`, "agycmp"},
		// No `file://` marker → empty.
		{`["/tmp/agycmp"]`, ""},
		// Empty / malformed → empty.
		{``, ""},
		{`[]`, ""},
	}
	for _, tc := range cases {
		got := projectFromURIs(tc.raw)
		if got != tc.want {
			t.Errorf("projectFromURIs(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
