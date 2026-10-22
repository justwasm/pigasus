package main

import "testing"

// Cycle 293: read_file / view_file results now carry the reference's
// timestamp header and a "Showing lines A-B of N" line. The transcript's
// detail row must keep showing the number of lines read, not count the
// header (which would print "Read 6 lines" for a 3-line file).
func TestReadDetailTextStampedResult(t *testing.T) {
	stamp := "Created At: 2026-10-21T23:18:40+08:00\nCompleted At: 2026-10-21T23:18:40+08:00\n"
	cases := []struct {
		name   string
		result string
		want   string
	}{
		{
			name:   "three lines",
			result: stamp + "Showing lines 1-3 of 3\n1: one\n2: two\n3: three",
			want:   "Read 3 lines",
		},
		{
			name:   "range of one prints no row",
			result: stamp + "Showing lines 2-2 of 3\n2: two",
			want:   "",
		},
		{
			name:   "single line prints no row",
			result: stamp + "Showing lines 1-1 of 1\n1: one",
			want:   "",
		},
		{
			name:   "byte-capped read reports its line range",
			result: stamp + "Partial read (lines 1-100 of 100). Showing bytes 0-46080 of 100491 in this line range, truncated at the 46080-byte limit. To read the rest of this line range, call this tool again with the same StartLine and EndLine and ByteOffset=46080.\n1: xxx",
			want:   "Read 100 lines",
		},
		{
			name:   "view_file range uses 'to' and reports its width",
			result: stamp + "File Path: `file:///tmp/a.txt`\nTotal Lines: 5\nTotal Bytes: 12\nShowing lines 1 to 5\n1: l1",
			want:   "Read 5 lines",
		},
		{
			name:   "empty file prints no row",
			result: stamp + "File is empty",
			want:   "",
		},
		{
			name:   "legacy raw result still counts lines",
			result: "one\ntwo\nthree\n",
			want:   "Read 3 lines",
		},
	}
	for _, tc := range cases {
		if got := readDetailText(tc.result); got != tc.want {
			t.Errorf("%s: readDetailText = %q, want %q", tc.name, got, tc.want)
		}
	}
}
