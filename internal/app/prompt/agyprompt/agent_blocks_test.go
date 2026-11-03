package agyprompt

import (
	"strings"
	"testing"
)

// The reference wraps a custom agent's Markdown body in one tag per part,
// split at level-1 headings. These are the five shapes a live agy 1.4.6
// `--agent` request body carried (【一手】, docs/cycles/reference-drift-146/).
func TestBuildAgent_SplitsBodyAtLevel1Headings(t *testing.T) {
	cases := []struct {
		name string
		body string
		want string
	}{
		{
			name: "no heading keeps the literal tag",
			body: "Answer with one word only.\n",
			want: "<System Prompt>\nAnswer with one word only.\n</System Prompt>\n",
		},
		{
			name: "leading heading becomes the tag",
			body: "# Probe agent\n\nAnswer in one short sentence.\n",
			want: "<Probe agent>\nAnswer in one short sentence.\n</Probe agent>\n",
		},
		{
			name: "heading text is verbatim",
			body: "# Totally Different Heading\n\nAnswer with one word.\n",
			want: "<Totally Different Heading>\nAnswer with one word.\n</Totally Different Heading>\n",
		},
		{
			name: "preamble keeps its own block",
			body: "Intro line first.\n\n# Later Heading\n\nAnswer with one word.\n",
			want: "<System Prompt>\nIntro line first.\n</System Prompt>\n" +
				"<Later Heading>\nAnswer with one word.\n</Later Heading>\n",
		},
		{
			name: "only level-1 headings split",
			body: "Preamble text.\n\n# One\nBody one.\n\n## Two\nBody two.\n\n### Three\nBody three.\n\n## Two Again\nBody four.\n",
			want: "<System Prompt>\nPreamble text.\n</System Prompt>\n" +
				"<One>\nBody one.\n\n## Two\nBody two.\n\n### Three\nBody three.\n\n## Two Again\nBody four.\n</One>\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := BuildAgent(tc.body, DefaultOptions("/tmp/ws", "/tmp/agent", "id"))
			if !strings.HasPrefix(got, tc.want) {
				t.Fatalf("prefix = %q, want %q", got[:min(len(got), len(tc.want)+40)], tc.want)
			}
		})
	}
}

// The section list that follows the body is unchanged by the split.
func TestBuildAgent_SectionsFollowTheBody(t *testing.T) {
	got := BuildAgent("# Title\n\nBody\n", DefaultOptions("/tmp/ws", "/tmp/agent", "id"))
	want := "<Title>\nBody\n</Title>\n<user_information>"
	if !strings.HasPrefix(got, want) {
		t.Fatalf("prefix = %q", got[:min(len(got), 120)])
	}
}
