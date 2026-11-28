// trajectory_verb_test.go — the verb a collapsed tool line draws.
//
// # The two vocabularies
//
// The store and the transcript do not use the same words for the same call,
// and they are not supposed to. Sampled from the 800 reference conversations
// on this machine (4,515 tool steps), render_info's field 5 carries the
// *storage* verb:
//
//	read_file / view_file   "Analyzed"   549 steps
//	write_file              "Created"     60
//	edit_file               "Edited"     260
//	run_command             "Ran"      2,134
//
// while the transcript the reference actually draws says "Explored 1 file
// (agy-cont.sh)" for a file read — cycle 285's PTY capture, byte-identical to
// the reference binary.
//
// # The bug these pin
//
// The Trajectory view joined field 5 to field 9, so a file read drew
// "Analyzed deliverable.md": both halves individually correct, the verb taken
// from the wrong layer. The transcript view drew the right line for the same
// step, so the two views disagreed about one call.
package tui

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
)

// TestTranscriptVerbsDifferFromStoredVerbs pins the two vocabularies against
// each other, so a future refactor cannot quietly collapse them into one.
//
// The sample behind this is recorded above and in toolStepSummary's comment:
// the store's prefix for a read is "Analyzed", the screen's word is
// "Explored". If either side is changed to match the other, this fails and
// the change has to argue for itself.
func TestTranscriptVerbsDifferFromStoredVerbs(t *testing.T) {
	storedRead := convstore.RenderPrefix("read_file")
	if storedRead != "Analyzed" {
		t.Errorf("RenderPrefix(read_file) = %q, want %q (the reference's field 5)", storedRead, "Analyzed")
	}
	screenRead := transcriptSummary("read_file", `{"Path":"/tmp/deliverable.md"}`)
	if !strings.HasPrefix(screenRead, "Explored") {
		t.Errorf("transcriptSummary(read_file) = %q, want it to start with %q (the transcript's word)", screenRead, "Explored")
	}
	if strings.HasPrefix(screenRead, storedRead) {
		t.Errorf("the transcript line %q starts with the store's verb %q; the two layers have collapsed into one", screenRead, storedRead)
	}
}

// TestStoredVerbNeverLeaksIntoTheTranscriptLine is the regression itself.
//
// A file read must not draw "Analyzed …": that is the store's word, and the
// reference's transcript never prints it.
func TestStoredVerbNeverLeaksIntoTheTranscriptLine(t *testing.T) {
	cases := []struct {
		name     string
		step     convstore.Step
		wantLine string
	}{
		{
			name: "read, the shape the write path now stores",
			step: convstore.Step{
				ToolName: "read_file", ToolPrefix: "Analyzed",
				ToolSummary: "deliverable.md",
				ToolArgs:    `{"Path":"/tmp/deliverable.md"}`,
			},
			wantLine: "Explored 1 file (deliverable.md)",
		},
		{
			name: "read, arguments gone so the line is rebuilt from the noun phrase",
			step: convstore.Step{
				ToolName: "read_file", ToolPrefix: "Analyzed",
				ToolSummary: "deliverable.md",
			},
			wantLine: "Explored 1 file (deliverable.md)",
		},
		{
			name: "write, the shape the write path now stores",
			step: convstore.Step{
				ToolName: "write_file", ToolPrefix: "Created",
				ToolSummary: "x.go",
				ToolArgs:    `{"Path":"/tmp/x.go"}`,
			},
			wantLine: "Edited 1 file (x.go)",
		},
		{
			name: "write, arguments gone",
			step: convstore.Step{
				ToolName: "write_file", ToolPrefix: "Created",
				ToolSummary: "x.go",
			},
			wantLine: "Edited 1 file (x.go)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toolStepSummary(tc.step)
			if got != tc.wantLine {
				t.Errorf("toolStepSummary() = %q, want %q", got, tc.wantLine)
			}
			if verb := firstWord(tc.step.ToolPrefix); verb != "" && strings.HasPrefix(got, verb+" ") {
				t.Errorf("line %q opens with the store's verb %q; the transcript uses its own", got, verb)
			}
		})
	}
}

// TestLegacyDisplayLineIsNotDoubleVerbed — field 9 used to hold the whole
// display line, including a verb, and joining that printed the verb twice.
//
// Both old spellings are covered because the write path changed which verb it
// stored: the store's ("Ran (…)") and, briefly, the transcript's ("Explored 1
// file (…)").
func TestLegacyDisplayLineIsNotDoubleVerbed(t *testing.T) {
	cases := []struct {
		name string
		step convstore.Step
		verb string
	}{
		{
			name: "store spelling",
			step: convstore.Step{
				ToolName: "run_command", ToolPrefix: "Ran",
				ToolSummary: "Ran (go build ./...)",
				ToolArgs:    `{"CommandLine":"go build ./..."}`,
			},
			verb: "Ran",
		},
		{
			name: "transcript spelling",
			step: convstore.Step{
				ToolName: "read_file", ToolPrefix: "Analyzed",
				ToolSummary: "Explored 1 file (deliverable.md)",
				ToolArgs:    `{"Path":"/tmp/deliverable.md"}`,
			},
			verb: "Explored",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := toolStepSummary(tc.step)
			if n := strings.Count(got, tc.verb); n != 1 {
				t.Errorf("line %q carries %q %d times, want exactly 1", got, tc.verb, n)
			}
		})
	}
}

// TestUnknownToolKeepsTheStoredDescription — a tool outside the transcript's
// vocabulary has no line of its own, so the store's description is what is
// left. The fallback must not invent one.
func TestUnknownToolKeepsTheStoredDescription(t *testing.T) {
	s := convstore.Step{
		ToolName: "some_mcp_tool", ToolPrefix: "Ran",
		ToolSummary: "did a thing",
		ToolArgs:    `{"foo":"bar"}`,
	}
	if got, want := toolStepSummary(s), "Ran did a thing"; got != want {
		t.Errorf("toolStepSummary() = %q, want %q", got, want)
	}
}

// TestTranscriptLineForFillsTheCategorysShape pins the rebuild path's shapes.
func TestTranscriptLineForFillsTheCategorysShape(t *testing.T) {
	cases := []struct{ tool, noun, want string }{
		{"read_file", "main.go", "Explored 1 file (main.go)"},
		{"view_file", "main.go", "Explored 1 file (main.go)"},
		{"search_web", "golang", "Explored 1 search (golang)"},
		{"write_file", "x.go", "Edited 1 file (x.go)"},
		{"edit_file", "x.go", "Edited 1 file (x.go)"},
		{"run_command", "go build ./...", "Ran (go build ./...)"},
		{"some_mcp_tool", "thing", ""},
	}
	for _, tc := range cases {
		if got := transcriptLineFor(tc.tool, tc.noun); got != tc.want {
			t.Errorf("transcriptLineFor(%q, %q) = %q, want %q", tc.tool, tc.noun, got, tc.want)
		}
	}
}
