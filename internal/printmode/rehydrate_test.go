package printmode

import (
	"path/filepath"
	"testing"

	"github.com/justwasm/pigasus/internal/convstore"
)

// TestRehydratePrintConversation pins that a continued conversation's
// subagent definitions come back from its trajectory.
//
// The reference scopes a definition to the conversation, and derives the
// live state from the trajectory when the conversation is loaded
// (【反编译】 the reference's `cortex/rehydration` package; see
// out/ide-language-server/README.md §10.9). The print mode replays a
// continued conversation's earlier turns ahead of the prompt; the state
// the tools hold does not come back with them.
func TestRehydratePrintConversation(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "conv.db")
	steps := []convstore.Step{
		{Idx: 0, Type: convstore.StepUserInput, Status: 3, Text: "define a viewer subagent"},
		{
			Idx:      1,
			Type:     convstore.StepToolCall,
			Status:   3,
			ToolName: "define_subagent",
			ToolArgs: `{"Name":"viewer","SystemPrompt":"You are a Go developer."}`,
		},
		{Idx: 2, Type: convstore.StepModelResponse, Status: 3, Text: "Defined."},
	}
	if err := convstore.WriteConversation(path, "traj-print", steps); err != nil {
		t.Fatalf("write: %v", err)
	}
	c, err := convstore.LoadConversation(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	defs := rehydratePrintConversation(ConversationSelection{Store: c}, "conv-1")
	if defs == nil {
		t.Fatalf("the definition store is nil")
	}
	def, ok := defs.Get("viewer")
	if !ok {
		t.Fatalf("the definition was not rehydrated: %v", defs.Names())
	}
	if def.SystemPrompt != "You are a Go developer." {
		t.Fatalf("rehydrated definition = %+v", def)
	}
}

// TestRehydratePrintConversationWithoutStore pins the empty case: a
// fresh conversation gets an empty store, not a nil one.
func TestRehydratePrintConversationWithoutStore(t *testing.T) {
	defs := rehydratePrintConversation(ConversationSelection{}, "conv-1")
	if defs == nil {
		t.Fatalf("the definition store is nil")
	}
	if names := defs.Names(); len(names) != 0 {
		t.Fatalf("definitions = %v, want none", names)
	}
}
