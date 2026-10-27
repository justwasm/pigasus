// git_test.go — the unified-diff hunk header.
package diffrow

import "testing"

func TestParseHunkHeader(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want HunkHeader
		ok   bool
	}{
		{"@@ -1,3 +1,4 @@", HunkHeader{OldStart: 1, OldCount: 3, NewStart: 1, NewCount: 4}, true},
		{"@@ -12,7 +12,9 @@ func main() {", HunkHeader{OldStart: 12, OldCount: 7, NewStart: 12, NewCount: 9}, true},
		{"@@ -0,0 +1,2 @@", HunkHeader{OldStart: 0, OldCount: 0, NewStart: 1, NewCount: 2}, true},
		// git writes the count only when it is not one.
		{"@@ -5 +5 @@", HunkHeader{OldStart: 5, OldCount: 1, NewStart: 5, NewCount: 1}, true},
		{"@@ -1,3 +1,4 @@", HunkHeader{OldStart: 1, OldCount: 3, NewStart: 1, NewCount: 4}, true},
		{"not a header", HunkHeader{}, false},
		{"@@ bad @@", HunkHeader{}, false},
		{"", HunkHeader{}, false},
	} {
		got, ok := ParseHunkHeader(tc.in)
		if ok != tc.ok {
			t.Errorf("ParseHunkHeader(%q) ok = %v, want %v", tc.in, ok, tc.ok)
			continue
		}
		if ok && got != tc.want {
			t.Errorf("ParseHunkHeader(%q) = %+v, want %+v", tc.in, got, tc.want)
		}
	}
}
