package tui

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestRootModel_ComponentFields pins the composite-tree shape: the root
// holds named component models, not loose state for the composer, the
// overlays or the panels.
func TestRootModel_ComponentFields(t *testing.T) {
	typ := reflect.TypeOf(RootModel{})
	fields := map[string]reflect.Type{}
	for i := 0; i < typ.NumField(); i++ {
		f := typ.Field(i)
		fields[f.Name] = f.Type
	}
	want := map[string]reflect.Type{
		"prompt":       reflect.TypeOf(PromptModel{}),
		"overlays":     reflect.TypeOf(OverlayModel{}),
		"statusLine":   reflect.TypeOf(StatusLineModel{}),
		"conversation": reflect.TypeOf(ConversationModel{}),
		"diff":         reflect.TypeOf(DiffModel{}),
		"auth":         reflect.TypeOf(AuthModel{}),
		"rewind":       reflect.TypeOf(RewindModel{}),
		"activeItems":  reflect.TypeOf(ActiveItemsModel{}),
		"help":         reflect.TypeOf(HelpModel{}),
		"audio":        reflect.TypeOf(AudioModel{}),
		"askQuestion":  reflect.TypeOf(AskQuestionModel{}),
		"tasks":        reflect.TypeOf(TasksModel{}),
		"agents":       reflect.TypeOf(AgentsModel{}),
		"tools":        reflect.TypeOf(ToolsModel{}),
		"remote":       reflect.TypeOf(RemoteModel{}),
		"compaction":   reflect.TypeOf(CompactionModel{}),
		"turn":         reflect.TypeOf(TurnService{}),
		"display":      reflect.TypeOf(DisplayModel{}),
		"commands":     reflect.TypeOf(CommandsModel{}),
	}
	for name, wt := range want {
		got, ok := fields[name]
		if !ok {
			t.Errorf("RootModel has no %q field", name)
			continue
		}
		if got != wt {
			t.Errorf("RootModel.%s is %v, want %v", name, got, wt)
		}
	}
}

// TestComponentModelsDoNotReachThroughTheRoot is the guard for the next
// extraction: a component file may not read the root's session, engine or
// viewport buckets. It may only use its own state and the seams the root
// passes in.
func TestComponentModelsDoNotReachThroughTheRoot(t *testing.T) {
	for _, name := range []string{"prompt_model.go", "overlay_model.go", "status_line_model.go", "conversation_model.go", "diff_model.go", "auth_model.go", "rewind_model.go", "active_items_model.go", "help_model.go", "audio_model.go", "ask_question_model.go", "tasks_model.go", "agents_model.go", "tools_model.go", "remote_model.go", "compaction_model.go", "turn_service.go", "display_model.go", "commands_model.go"} {
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		src := string(data)
		for _, forbidden := range []string{"m.session.", "m.engine.", "m.viewport."} {
			if strings.Contains(src, forbidden) {
				t.Errorf("%s reaches through the root with %q; pass a seam instead", name, forbidden)
			}
		}
	}
}
