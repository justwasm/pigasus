package convstore

import (
	"strings"
	"testing"
)

func TestGenerateConversationTitle_Tokens(t *testing.T) {
	cases := []struct {
		name  string
		input string
		check func(t *testing.T, got string)
	}{
		{
			name:  "plain sentence takes 4-6 words",
			input: "Please help me write a new unit test for the parser",
			check: func(t *testing.T, got string) {
				words := strings.Fields(got)
				if len(words) < 4 || len(words) > 6 {
					t.Errorf("word count = %d, want 4..6 (got: %q)", len(words), got)
				}
				if !strings.HasPrefix(got, "Please help me write") {
					t.Errorf("unexpected title: %q", got)
				}
			},
		},
		{
			name:  "markdown code block stripped",
			input: "```go\nfunc main() {}\n```\nwhat does this do?",
			check: func(t *testing.T, got string) {
				if strings.Contains(got, "```") || strings.Contains(got, "func") {
					t.Errorf("code fence leaked into title: %q", got)
				}
				if !strings.Contains(got, "what does this do") {
					t.Errorf("content words missing: %q", got)
				}
			},
		},
		{
			name:  "leading blockquote and header stripped",
			input: "> # Header\nreal user question goes here",
			check: func(t *testing.T, got string) {
				if strings.HasPrefix(got, ">") || strings.HasPrefix(got, "#") {
					t.Errorf("markdown prefix leaked into title: %q", got)
				}
				if !strings.Contains(got, "Header real user question") {
					t.Errorf("content words missing: %q", got)
				}
			},
		},
		{
			name:  "empty prompt",
			input: "",
			check: func(t *testing.T, got string) {
				if got != "" {
					t.Errorf("got %q, want empty", got)
				}
			},
		},
		{
			name:  "punctuation only",
			input: "...!!!",
			check: func(t *testing.T, got string) {
				if strings.Count(got, " ") >= 4 {
					t.Errorf("punctuation-only prompt should not yield a 5-token title: %q", got)
				}
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tc.check(t, GenerateConversationTitle(tc.input))
		})
	}
}
