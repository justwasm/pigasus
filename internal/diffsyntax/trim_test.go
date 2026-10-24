package diffsyntax

import (
	"testing"
)

func TestTrimTrailing_Tab(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"+hello\t", "+hello"},
		{"+hello\t\t", "+hello"},
		{"plain\t", "plain"},
		{"@@ -1,2 +1,2 @@\t", "@@ -1,2 +1,2 @@"},
	}
	for _, c := range cases {
		if got := TrimTrailing(c.in); got != c.want {
			t.Errorf("TrimTrailing(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTrimTrailing_Space(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"+hello ", "+hello"},
		{"-world   ", "-world"},
		{" trailing space  ", " trailing space"},
		{"single", "single"},
	}
	for _, c := range cases {
		if got := TrimTrailing(c.in); got != c.want {
			t.Errorf("TrimTrailing(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTrimTrailing_PreservesInternalWhitespace(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"+a   b", "+a   b"},
		{"+a\tb", "+a\tb"},
		{"+a   b   ", "+a   b"},
		{" context line", " context line"},
	}
	for _, c := range cases {
		if got := TrimTrailing(c.in); got != c.want {
			t.Errorf("TrimTrailing(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTrimTrailing_BlankLinePreserved(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", ""},
		{" ", ""},
		{"\t", ""},
		{" \t \t", ""},
		{"  keep me  ", "  keep me"},
	}
	for _, c := range cases {
		if got := TrimTrailing(c.in); got != c.want {
			t.Errorf("TrimTrailing(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTrimTrailing_PlusMarkerNotTrimmed(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"+", "+"},
		{"+ ", "+"},
		{"+\t\t", "+"},
		{"+ \t \t", "+"},
		{"+hi\t", "+hi"},
		{"+ real content", "+ real content"},
	}
	for _, c := range cases {
		if got := TrimTrailing(c.in); got != c.want {
			t.Errorf("TrimTrailing(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTrimTrailing_MinusMarkerNotTrimmed(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"-", "-"},
		{"- ", "-"},
		{"-\t\t", "-"},
		{"-row\t", "-row"},
	}
	for _, c := range cases {
		if got := TrimTrailing(c.in); got != c.want {
			t.Errorf("TrimTrailing(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestTrimTrailing_HunkHeaderPreserved(t *testing.T) {
	hdr := "@@ -1,2 +1,2 @@ context"
	if got := TrimTrailing(hdr); got != hdr {
		t.Errorf("TrimTrailing(%q) = %q, want unchanged", hdr, got)
	}
}

func TestTrimTrailing_FileHeaderPreserved(t *testing.T) {
	cases := []string{
		"diff --git a/foo b/foo",
		"index 1234..5678 100644",
		"--- a/foo",
		"+++ b/foo",
		"new file mode 100644",
	}
	for _, in := range cases {
		if got := TrimTrailing(in); got != in {
			t.Errorf("TrimTrailing(%q) = %q, want unchanged", in, got)
		}
	}
}
