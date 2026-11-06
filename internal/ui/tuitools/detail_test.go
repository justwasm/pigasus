package tuitools

import (
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/palette"
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
		{
			// The clone's own result — the shape agy 1.4.4 and 1.4.7 send.
			// Before cycle 298 this one did not parse, so the timestamp
			// stamp reached the screen and a failed command was never
			// marked as failed.
			name:   "current shape",
			tool:   "run_command",
			detail: "Created At: x\nCompleted At: y\nCommand exited with code 0.\n\nCommand output:\nhello\n",
			want:   "Command exited with code 0.\n\nCommand output:\nhello",
		},
		{
			name:   "current shape, failed command",
			tool:   "run_command",
			detail: "Created At: x\nCompleted At: y\nCommand exited with code 2.\n\nCommand output:\nboom\n",
			want:   "Command exited with code 2.\n\nCommand output:\nboom",
		},
		{
			name:   "current shape, empty output",
			tool:   "run_command",
			detail: "Created At: x\nCompleted At: y\nCommand exited with code 0.\n\nNo output from the command.",
			want:   "Command exited with code 0.\n\nNo output from the command.",
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
	if !strings.HasPrefix(out[1], DetailIndent) {
		t.Errorf("second row %q is not indented with DetailIndent", out[1])
	}
}

func TestDetailWrapWidth(t *testing.T) {
	cases := []struct {
		width int
		want  int
	}{
		{80, 73},
		{100, 93},
		{27, 20},
		{25, 20}, // clamped to DetailMinWrapWidth (20)
		{10, 20}, // clamped to DetailMinWrapWidth (20)
	}
	for _, c := range cases {
		if got := DetailWrapWidth(c.width); got != c.want {
			t.Errorf("DetailWrapWidth(%d) = %d, want %d", c.width, got, c.want)
		}
	}
}

func TestFormatDetailLines_ExpandsTabs(t *testing.T) {
	out := FormatDetailLines([]string{"\tfunc main() {", "\t\tr.mu.Lock()"}, false)
	if len(out) != 2 {
		t.Fatalf("got %d rows, want 2", len(out))
	}
	if strings.Contains(out[0], "\t") || strings.Contains(out[1], "\t") {
		t.Errorf("FormatDetailLines did not expand tabs: %q, %q", out[0], out[1])
	}
	if !strings.Contains(out[0], "    func main() {") {
		t.Errorf("expected 4 spaces for first tab in %q", out[0])
	}
	if !strings.Contains(out[1], "        r.mu.Lock()") {
		t.Errorf("expected 8 spaces for two tabs in %q", out[1])
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
	want := DetailIndent + "<output +3 lines>"
	if out[len(out)-1] != want {
		t.Errorf("overflow row = %q, want %q", out[len(out)-1], want)
	}
}

func TestWrapDiffLine_AddAndRemove(t *testing.T) {
	longAdd := "+func VeryLongFunctionNameThatExceedsTheGivenWrapWidthBySubstantialMargin(a string) error {"
	partsAdd := WrapDiffLine(longAdd, 35)
	if len(partsAdd) < 3 {
		t.Fatalf("expected at least 3 wrapped parts, got %d: %v", len(partsAdd), partsAdd)
	}
	for i, p := range partsAdd {
		if !strings.Contains(p, palette.ToolAddOpen) || !strings.Contains(p, palette.ToolAddClose) {
			t.Errorf("part %d missing ToolAdd styling: %q", i, p)
		}
	}

	longRemove := "-func OldFunctionNameThatExceedsTheGivenWrapWidthBySubstantialMargin(a string) error {"
	partsRemove := WrapDiffLine(longRemove, 35)
	if len(partsRemove) < 3 {
		t.Fatalf("expected at least 3 wrapped parts, got %d: %v", len(partsRemove), partsRemove)
	}
	for i, p := range partsRemove {
		if !strings.Contains(p, palette.ToolRemoveOpen) || !strings.Contains(p, palette.ToolRemoveClose) {
			t.Errorf("part %d missing ToolRemove styling: %q", i, p)
		}
	}

	contextLine := "    func ContextLineNotModified(a string, b int, c bool) (string, error) {"
	partsCtx := WrapDiffLine(contextLine, 35)
	if len(partsCtx) < 2 {
		t.Fatalf("expected at least 2 wrapped parts for context, got %d: %v", len(partsCtx), partsCtx)
	}
	for i, p := range partsCtx {
		if strings.Contains(p, palette.ToolAddOpen) || strings.Contains(p, palette.ToolRemoveOpen) {
			t.Errorf("context part %d should not have diff styling: %q", i, p)
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
