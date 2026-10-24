package tuitools

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/palette"
)

func TestStepDetailText_Commands(t *testing.T) {
	cases := []struct {
		name   string
		tool   string
		detail string
		want   string
	}{
		{
			name:   "command success",
			tool:   "run_command",
			detail: "Created At: x\nCompleted At: y\n\nThe command exited with code 0.\nOutput:\nhello\n",
			want:   "Command exited with code 0.\n\nCommand output:\nhello",
		},
		{
			name:   "command empty output",
			tool:   "run_command",
			detail: "The command exited with code 2.\nOutput:\n\n",
			want:   "Command exited with code 2.\n\nNo output from the command.",
		},
		{
			name:   "command without exit code keeps raw",
			tool:   "run_command",
			detail: "tool blew up",
			want:   "tool blew up",
		},
	}
	for _, c := range cases {
		if got := StepDetailText(c.tool, c.detail, false); got != c.want {
			t.Errorf("StepDetailText(%q) = %q, want %q", c.tool, got, c.want)
		}
	}
}

func TestStepDetailText_Other(t *testing.T) {
	cases := []struct {
		name      string
		tool      string
		detail    string
		isThought bool
		want      string
	}{
		{
			name:   "read file",
			tool:   "view_file",
			detail: "Showing lines 1-3 of 3\n",
			want:   "Read 3 lines",
		},
		{
			name:   "unknown tool keeps raw",
			tool:   "search_web",
			detail: "results",
			want:   "results",
		},
		{
			name:      "thought is verbatim",
			tool:      "run_command",
			detail:    "reasoning body",
			isThought: true,
			want:      "reasoning body",
		},
	}
	for _, c := range cases {
		if got := StepDetailText(c.tool, c.detail, c.isThought); got != c.want {
			t.Errorf("StepDetailText(%q) = %q, want %q", c.tool, got, c.want)
		}
	}
}

func TestParseReadDetailLines(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"Showing lines 1-3 of 10", 3},
		{"Showing lines 4 to 6 of 10", 3},
		{"Partial read (lines 2-5 of 9)", 4},
		{"File is empty", 0},
		{"a\nb\nc", 3},
		{"", 0},
	}
	for _, c := range cases {
		if got := ParseReadDetailLines(c.in); got != c.want {
			t.Errorf("ParseReadDetailLines(%q) = %d, want %d", c.in, got, c.want)
		}
	}
}

func TestFormatDetailLines(t *testing.T) {
	out := FormatDetailLines([]string{"one", "two"}, false)
	if len(out) != 2 {
		t.Fatalf("FormatDetailLines returned %d rows, want 2", len(out))
	}
	if !strings.HasPrefix(out[0], palette.DetailBulletOpen) {
		t.Errorf("first row %q does not carry the detail bullet", out[0])
	}
	if !strings.HasPrefix(out[1], "            ") {
		t.Errorf("second row %q is not indented", out[1])
	}
}

func TestFormatDetailLinesOverflow(t *testing.T) {
	lines := make([]string, MaxDetailLines+3)
	for i := range lines {
		lines[i] = "row"
	}
	out := FormatDetailLines(lines, false)
	if len(out) != MaxDetailLines+1 {
		t.Fatalf("FormatDetailLines returned %d rows, want %d", len(out), MaxDetailLines+1)
	}
	want := "            <output +3 lines>"
	if out[len(out)-1] != want {
		t.Errorf("overflow row = %q, want %q", out[len(out)-1], want)
	}
}

func TestLooksLikeDiff(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"@@ -1,3 +1,3 @@\n-a\n+b", true},
		{"--- a/file\n+++ b/file\n", true},
		{"--- deleted ---", false},
		{"- pattern\n- other", false},
		{"plain text", false},
	}
	for _, c := range cases {
		if got := LooksLikeDiff(c.in); got != c.want {
			t.Errorf("LooksLikeDiff(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestLooksLikeMarkdown(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"**bold**", true},
		{"```go\ncode\n```", true},
		{"# heading", true},
		{"[link](http://x)", true},
		{"- bullet", true},
		{"* bullet", true},
		{"plain text", false},
	}
	for _, c := range cases {
		if got := LooksLikeMarkdown(c.in); got != c.want {
			t.Errorf("LooksLikeMarkdown(%q) = %v, want %v", c.in, got, c.want)
		}
	}
}

func TestStyleDiffLine(t *testing.T) {
	added := StyleDiffLine("+func main() {}")
	if !strings.Contains(added, "func main() {}") {
		t.Errorf("StyleDiffLine dropped the content: %q", added)
	}
	if !strings.Contains(added, palette.ToolAddOpen) {
		t.Errorf("StyleDiffLine(+…) = %q, want the add colour", added)
	}
	if !strings.HasPrefix(added, "+") {
		t.Errorf("StyleDiffLine(+…) = %q, want the marker first", added)
	}
	removed := StyleDiffLine("-func old() {}")
	if !strings.Contains(removed, palette.ToolRemoveOpen) {
		t.Errorf("StyleDiffLine(-…) = %q, want the remove colour", removed)
	}
	if plain := StyleDiffLine(" context"); plain != " context" {
		t.Errorf("StyleDiffLine(context) = %q, want it unchanged", plain)
	}
	if plain := StyleDiffLine("--- header"); plain != "--- header" {
		t.Errorf("StyleDiffLine(--- header) = %q, want it unchanged", plain)
	}
	long := "+" + strings.Repeat("x", 300)
	if got := StyleDiffLine(long); got != long {
		t.Errorf("StyleDiffLine on an over-long line changed it")
	}
}

func TestNormalizeDetailNewlines(t *testing.T) {
	if got := NormalizeDetailNewlines("a\r\nb\rc"); got != "a\nbc" {
		t.Errorf("NormalizeDetailNewlines = %q, want %q", got, "a\nbc")
	}
	if got := NormalizeDetailNewlines("plain"); got != "plain" {
		t.Errorf("NormalizeDetailNewlines changed a clean string: %q", got)
	}
}

func TestGroupSummary(t *testing.T) {
	homogeneous := []Step{
		{Name: "run_command", Title: "Bash(echo one)", Summary: "Ran (echo one)"},
		{Name: "run_command", Title: "Bash(echo two)", Summary: "Ran (echo two)"},
	}
	verb, count, detail := GroupSummary(homogeneous)
	if verb != "Ran" {
		t.Errorf("verb = %q, want %q", verb, "Ran")
	}
	if count != "2 commands" {
		t.Errorf("count = %q, want %q", count, "2 commands")
	}
	if detail != "(echo one · echo two)" {
		t.Errorf("detail = %q, want %q", detail, "(echo one · echo two)")
	}

	mixed := []Step{
		{Name: "run_command", Title: "Bash(echo one)"},
		{Name: "view_file", Title: "Read(/tmp/x)"},
	}
	verb, count, detail = GroupSummary(mixed)
	if verb != "Ran" {
		t.Errorf("mixed verb = %q, want %q", verb, "Ran")
	}
	if count != "1 file, 1 command" {
		t.Errorf("mixed count = %q, want %q", count, "1 file, 1 command")
	}
	if detail != "" {
		t.Errorf("mixed detail = %q, want it empty", detail)
	}
}

func TestSplitVerbArgs(t *testing.T) {
	cases := []struct {
		in       string
		wantVerb string
		wantArgs string
		wantSep  string
	}{
		{"Bash(echo one)", "Bash", "(echo one)", ""},
		{"Bash echo one", "Bash", "echo one", " "},
		{"Bash", "Bash", "", ""},
	}
	for _, c := range cases {
		verb, args, sep := SplitVerbArgs(c.in)
		if verb != c.wantVerb || args != c.wantArgs || sep != c.wantSep {
			t.Errorf("SplitVerbArgs(%q) = (%q, %q, %q), want (%q, %q, %q)",
				c.in, verb, args, sep, c.wantVerb, c.wantArgs, c.wantSep)
		}
	}
}

func TestHumanDuration(t *testing.T) {
	cases := []struct {
		in   time.Duration
		want string
	}{
		{500 * time.Millisecond, "500ms"},
		{1500 * time.Millisecond, "1.5s"},
		{3 * time.Second, "3s"},
	}
	for _, c := range cases {
		if got := HumanDuration(c.in); got != c.want {
			t.Errorf("HumanDuration(%v) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestThoughtTitle(t *testing.T) {
	if got := ThoughtTitle(3*time.Second, 0); got != "Thought for 3s" {
		t.Errorf("ThoughtTitle = %q, want %q", got, "Thought for 3s")
	}
	if got := ThoughtTitle(3*time.Second, 12); got != "Thought for 3s, 12 tokens" {
		t.Errorf("ThoughtTitle = %q, want %q", got, "Thought for 3s, 12 tokens")
	}
}
