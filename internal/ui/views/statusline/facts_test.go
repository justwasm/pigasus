// facts_test.go — pins the adapters that turn the session's own facts
// into the payload's vocabulary.
//
// The wire vocabulary is a contract with the scripts that read the
// document, so it lives with the document: the vim mode's names, the
// subagent entries and the context percentage all have one owner here.
package statusline_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
	"github.com/justwasm/pigasus/vim"
)

// TestVimModeName pins the four mode names the payload's `vim.mode`
// member may carry (official-docs/cli/statusline.md:79).
func TestVimModeName(t *testing.T) {
	cases := []struct {
		mode vim.Mode
		want string
	}{
		{vim.Normal, "NORMAL"},
		{vim.Insert, "INSERT"},
		{vim.Visual, "VISUAL"},
		{vim.VisualLine, "VISUAL LINE"},
	}
	for _, c := range cases {
		if got := statusline.VimModeName(c.mode); got != c.want {
			t.Errorf("VimModeName(%d) = %q, want %q", c.mode, got, c.want)
		}
	}
}

// TestSubagents pins the mapping from the registry's list entries to the
// payload's entries.
//
// The name is the type the child was launched as, the role is the role
// the call asked for (falling back to the type's name), and the status
// is the payload's own two-value vocabulary.
func TestSubagents(t *testing.T) {
	states := []subagenttools.StateEntry{
		{Type: "research", Role: "deep dive", State: subagenttools.StateRunning, Model: "Gemini 3.8 Flash (High)"},
		{Type: "self", State: subagenttools.StateIdle},
	}
	got := statusline.Subagents(states)
	if len(got) != 2 {
		t.Fatalf("subagents count = %d, want 2", len(got))
	}
	if got[0].Name != "research" || got[0].Role != "deep dive" || got[0].Status != "running" {
		t.Errorf("subagents[0] = %+v, want research/deep dive/running", got[0])
	}
	if got[0].Model != "Gemini 3.8 Flash (High)" {
		t.Errorf("subagents[0].Model = %q", got[0].Model)
	}
	if got[1].Name != "self" || got[1].Role != "self" || got[1].Status != "completed" {
		t.Errorf("subagents[1] = %+v, want self/self/completed", got[1])
	}
}

// TestSubagents_Empty pins the omit rule: an empty registry produces no
// entries, so the payload omits the member.
func TestSubagents_Empty(t *testing.T) {
	if got := statusline.Subagents(nil); got != nil {
		t.Errorf("Subagents(nil) = %+v, want nil", got)
	}
}

// TestContextUsedPercent pins the arithmetic the payload's
// `used_percentage` member and the status row's context cell share.
func TestContextUsedPercent(t *testing.T) {
	if got := statusline.ContextUsedPercent(19136, 131072); got != 14.599609375 {
		t.Errorf("ContextUsedPercent = %v, want 14.599609375", got)
	}
	if got := statusline.ContextUsedPercent(100, 0); got != 0 {
		t.Errorf("ContextUsedPercent with no window = %v, want 0", got)
	}
}
