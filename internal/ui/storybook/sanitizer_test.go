package storybook

import (
	"testing"
)

func TestSanitizer_Paths(t *testing.T) {
	s := newDefaultSanitizer()
	s.repoRoot = "/mock/user/workspace/openagy"
	s.userHome = "/mock/user"

	tests := []struct {
		input string
		want  string
	}{
		{
			input: "Read(/mock/user/workspace/openagy/internal/ui/storybook/types.go)",
			want:  "Read(/workspace/openagy/internal/ui/storybook/types.go)",
		},
		{
			input: "Cwd: /Users/openagy/workspace",
			want:  "Cwd: /workspace/workspace",
		},
		{
			input: "Path: /home/runner/work/openagy/test.go",
			want:  "Path: /workspace/work/openagy/test.go",
		},
		{
			input: "Workspace: ~/workspace/openagy/internal/ui/storybook/.agents/skills/{skill_name}/SKILL.md",
			want:  "Workspace: ~/workspace/openagy/internal/ui/storybook/.agents/skills/{skill_name}/SKILL.md",
		},
	}

	for _, tc := range tests {
		got := s.Sanitize(tc.input)
		if got != tc.want {
			t.Errorf("Sanitize(%q) =\n  got:  %q\n  want: %q", tc.input, got, tc.want)
		}
	}
}

func TestSanitizer_Durations(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "--- PASS: TestCursor (0.01s)",
			want:  "--- PASS: TestCursor (XX.Xs)",
		},
		{
			input: "--- FAIL: TestCursor (0.00s)",
			want:  "--- FAIL: TestCursor (XX.Xs)",
		},
		{
			input: "Finished in (14.2s)",
			want:  "Finished in (XX.Xs)",
		},
		{
			input: "ok  github.com/justwasm/pigasus 1.450s",
			want:  "ok  github.com/justwasm/pigasus XX.Xs",
		},
		{
			input: "Thought for 4.2s, 280 tokens",
			want:  "Thought for XX.Xs, 280 tokens",
		},
		{
			input: "worker task: worked 14s (exit 0)",
			want:  "worker task: worked XXs (exit 0)",
		},
		{
			input: "Saved 15m ago",
			want:  "Saved XXm ago",
		},
		{
			input: "Refreshes in 4h 12m",
			want:  "Refreshes in XXh XXm",
		},
	}

	s := DefaultSanitizer()
	for _, tc := range tests {
		got := s.Sanitize(tc.input)
		if got != tc.want {
			t.Errorf("Sanitize(%q) =\n  got:  %q\n  want: %q", tc.input, got, tc.want)
		}
	}
}

func TestSanitizer_Timestamps(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{
			input: "Log entry at 14:20:00 received",
			want:  "Log entry at HH:MM:SS received",
		},
		{
			input: "Time: 2026-11-22T16:03:43Z - Done",
			want:  "Time: YYYY-MM-DDTHH:MM:SSZ - Done",
		},
		{
			input: "Reset at 00:00 UTC",
			want:  "Reset at HH:MM UTC",
		},
	}

	s := DefaultSanitizer()
	for _, tc := range tests {
		got := s.Sanitize(tc.input)
		if got != tc.want {
			t.Errorf("Sanitize(%q) =\n  got:  %q\n  want: %q", tc.input, got, tc.want)
		}
	}
}
