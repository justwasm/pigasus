package toolsteps

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/tuitools"
)

// TestStepDetailLines_CommandOutputKeepsItsIndentation is the reported
// inconsistency (2026-11-14): two consecutive `run_command` steps, the first
// shown with its source indentation and backtick-quoted struct tags intact,
// the second dedented with the tags stripped.
//
// The second block's output contained `meta **StreamMeta`; the `**` alone
// used to mark the whole body as markdown, so goldmark re-flowed it. Command
// output is drawn preformatted.
func TestStepDetailLines_CommandOutputKeepsItsIndentation(t *testing.T) {
	body := "Command exited with code 0.\n\nCommand output:\n" +
		"func updateMessageDelta(payload string, meta **StreamMeta) {\n" +
		"\tvar d struct {\n" +
		"\t\tUsage struct {\n" +
		"\t\t\tInputTokens  int `json:\"input_tokens\"`\n" +
		"\t\t}\n" +
		"\t}\n" +
		"}\n" +
		"exit code 0"
	s := Step{Name: "run_command", Title: "Bash(sed -n '90,220p' anthropic.go)", Detail: body}

	lines := stepDetailLines(s, 0)
	joined := strings.Join(lines, "\n")
	for _, want := range []string{
		"meta **StreamMeta",
		"`json:\"input_tokens\"`", // the tag's backticks must survive
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("verbatim fragment %q lost:\n%s", want, joined)
		}
	}
	// The body rows are indented one level (DetailIndent) plus whatever the
	// source line carried; the inner struct field keeps its two tabs' worth
	// of nesting, expanded to spaces.
	if !strings.Contains(joined, tuitools.DetailIndent+"    InputTokens") {
		t.Errorf("source nesting lost:\n%s", joined)
	}
}

// A Go double pointer on its own must not mark a body as markdown, whichever
// tool produced it.
func TestLooksLikeMarkdown_GoDoublePointerIsNotMarkdown(t *testing.T) {
	for _, body := range []string{
		"func updateMessageDelta(payload string, meta **StreamMeta) {",
		"char **argv = envp; char **envp2 = argv;",
		"glob **/*.go",
		"x := 2**10",
	} {
		if tuitools.LooksLikeMarkdown(body) {
			t.Errorf("LooksLikeMarkdown(%q) = true, want false", body)
		}
	}
	// A real strong span still fires.
	if !tuitools.LooksLikeMarkdown("this is **important** text") {
		t.Error("a real strong span was not recognised")
	}
}

// A page tool's body keeps the markdown path: read_url_content returns a
// document, and cycle 176 pinned that its markers are rendered.
func TestStepDetailLines_PageToolStillRendersMarkdown(t *testing.T) {
	s := Step{
		Name:   "read_url_content",
		Title:  "Read README.md",
		Detail: "# Project\n\nThis is **important** text.\n",
	}
	joined := strings.Join(stepDetailLines(s, 0), "\n")
	if strings.Contains(joined, "**important**") {
		t.Errorf("raw ** survived for a page tool:\n%s", joined)
	}
	if !strings.Contains(joined, "important") {
		t.Errorf("text lost:\n%s", joined)
	}
}
