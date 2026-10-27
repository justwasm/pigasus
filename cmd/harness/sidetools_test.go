// sidetools_test.go — the tool surface a harness session advertises.
//
// The client owns the surface: it tells the harness which tools exist
// through `HarnessConfig.harness_side_tools`, and the harness advertises
// exactly those. The names are the harness's own — it reads a file with
// `view_file`, not with the CLI's `read_file`.
package main

import (
	"encoding/json"
	"testing"

	"github.com/justwasm/pigasus/internal/tools"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// sideToolConfig builds a side-tool configuration from the enabled tools.
func sideToolConfig(enabled ...string) *pb.HarnessSideTools {
	on := map[string]bool{}
	for _, name := range enabled {
		on[name] = true
	}
	return pb.HarnessSideTools_builder{
		Find:          pb.FindToolConfig_builder{Enabled: boolPtr(on["find"])}.Build(),
		RunCommand:    pb.RunCommandToolConfig_builder{Enabled: boolPtr(on["run_command"])}.Build(),
		Subagents:     pb.SubagentsConfig_builder{Enabled: boolPtr(on["subagents"])}.Build(),
		UserQuestions: pb.UserQuestionsConfig_builder{Enabled: boolPtr(on["user_questions"])}.Build(),
		FileEdit:      pb.FileEditToolConfig_builder{Enabled: boolPtr(on["file_edit"])}.Build(),
		ViewFile:      pb.ViewFileToolConfig_builder{Enabled: boolPtr(on["view_file"])}.Build(),
		WriteToFile:   pb.WriteToFileToolConfig_builder{Enabled: boolPtr(on["write_to_file"])}.Build(),
		GrepSearch:    pb.GrepSearchToolConfig_builder{Enabled: boolPtr(on["grep_search"])}.Build(),
		ListDir:       pb.ListDirToolConfig_builder{Enabled: boolPtr(on["list_dir"])}.Build(),
		GenerateImage: pb.GenerateImageToolConfig_builder{Enabled: boolPtr(on["generate_image"])}.Build(),
		SearchWeb:     pb.SearchWebToolConfig_builder{Enabled: boolPtr(on["search_web"])}.Build(),
		ReadUrlContent: pb.ReadUrlContentToolConfig_builder{
			Enabled: boolPtr(on["read_url_content"]),
		}.Build(),
		ManageTask: pb.ManageTaskToolConfig_builder{Enabled: boolPtr(on["manage_task"])}.Build(),
		Schedule:   pb.ScheduleToolConfig_builder{Enabled: boolPtr(on["schedule"])}.Build(),
		RunWorkflow: pb.RunWorkflowToolConfig_builder{
			Enabled: boolPtr(on["run_workflow"]),
		}.Build(),
	}.Build()
}

// boolPtr returns a pointer to a copy of the flag.
func boolPtr(v bool) *bool { return &v }

func TestSideToolNamesMapTheProtocolOntoTheHarnessNames(t *testing.T) {
	names := sideToolNames(sideToolConfig(
		"find", "run_command", "subagents", "user_questions", "file_edit",
		"view_file", "write_to_file", "grep_search", "list_dir",
		"generate_image", "search_web", "read_url_content", "manage_task",
		"schedule", "run_workflow",
	))
	want := []string{
		"find_by_name", "run_command", "invoke_subagent", "define_subagent",
		"manage_subagents", "ask_question", "replace_file_content",
		"view_file", "write_to_file", "grep_search", "list_dir",
		"generate_image", "search_web", "read_url_content", "manage_task",
		"schedule", "run_workflow",
	}
	for _, name := range want {
		if !names[name] {
			t.Errorf("%s is not in the surface", name)
		}
	}
}

func TestSideToolNamesLeaveTheDisabledToolsOut(t *testing.T) {
	names := sideToolNames(sideToolConfig("view_file"))
	if !names["view_file"] {
		t.Error("view_file is not in the surface")
	}
	if names["run_command"] {
		t.Error("run_command is in the surface, want it out")
	}
	if names["invoke_subagent"] {
		t.Error("invoke_subagent is in the surface, want it out")
	}
}

func TestSideToolNamesWithNoConfigurationAdvertiseNothing(t *testing.T) {
	if names := sideToolNames(nil); len(names) != 0 {
		t.Errorf("the surface = %v, want it empty", names)
	}
}

func TestTheHarnessDeclarationsAreTheCapturedEighteen(t *testing.T) {
	decls := tools.HarnessDeclarations()
	if len(decls) != 18 {
		t.Fatalf("declarations = %d, want 18", len(decls))
	}
	names := map[string]bool{}
	for _, decl := range decls {
		if names[decl.Name] {
			t.Errorf("%s is declared twice", decl.Name)
		}
		names[decl.Name] = true
	}
	for _, want := range []string{
		"ask_question", "define_subagent", "find_by_name", "generate_image",
		"grep_search", "invoke_subagent", "list_dir", "manage_subagents",
		"manage_task", "read_url_content", "replace_file_content",
		"run_command", "schedule", "search_web", "send_message", "view_file",
		"write_to_file", "run_workflow",
	} {
		if !names[want] {
			t.Errorf("%s is not declared", want)
		}
	}
	// The CLI names the same operations differently; a harness session
	// must not advertise the CLI's names.
	for _, unwanted := range []string{"read_file", "write_file", "edit_file"} {
		if names[unwanted] {
			t.Errorf("%s is declared, want the harness's own name", unwanted)
		}
	}
}

func TestHarnessDeclarationsAreFilteredToTheEnabledTools(t *testing.T) {
	decls := harnessDeclarations(map[string]bool{
		"view_file":   true,
		"run_command": true,
	}, nil)
	if len(decls) != 2 {
		t.Fatalf("declarations = %d, want 2", len(decls))
	}
	for _, decl := range decls {
		if decl.Name != "view_file" && decl.Name != "run_command" {
			t.Errorf("%s is not enabled", decl.Name)
		}
	}
}

func TestAlwaysOnToolsAreTheMessagingChannelAndTheFinish(t *testing.T) {
	on := alwaysOnTools()
	if !on["send_message"] {
		t.Error("send_message is not always on")
	}
	if !on["finish"] {
		t.Error("finish is not always on")
	}
}

// TestFinishDeclarationIsTheClientsOwnSchema pins the `finish`
// declaration the client owns.
//
// The reference advertises `finish` only when the client named a schema
// for it, and the schema that travels is the client's own bytes with the
// keys sorted — the reference carries it as a `google.protobuf.Struct`,
// so the map's keys come out sorted.
func TestFinishDeclarationIsTheClientsOwnSchema(t *testing.T) {
	cfg := &pb.HarnessConfig{}
	cfg.SetFinishToolSchemaJson(
		`{"type":"object","properties":{"summary":{"type":"string"}},"required":["summary"]}`)
	schema := configFinishSchema(cfg)
	want := `{"properties":{"summary":{"type":"string"}},"required":["summary"],"type":"object"}`
	if string(schema) != want {
		t.Errorf("the finish schema = %s, want %s", schema, want)
	}

	enabled := map[string]bool{"finish": true, "view_file": true}
	decls := harnessDeclarations(enabled, schema)
	var finish *tools.ModelToolDecl
	for i := range decls {
		if decls[i].Name == "finish" {
			finish = &decls[i]
		}
	}
	if finish == nil {
		t.Fatal("finish is not declared")
	}
	if string(finish.Parameters) != want {
		t.Errorf("finish schema = %s, want %s", finish.Parameters, want)
	}
	if finish.Description == "" {
		t.Error("finish has no description; the reference's own text is required")
	}
}

// TestFinishSchemaDropsWhatIsNotJSON pins the reference's own limit.
//
// The reference carries the schema as a `google.protobuf.Struct`, so a
// schema that is not JSON cannot go into one; the session then has no
// `finish` declaration, which is the session the client gets when it
// names no schema at all.
func TestFinishSchemaDropsWhatIsNotJSON(t *testing.T) {
	cfg := &pb.HarnessConfig{}
	cfg.SetFinishToolSchemaJson("not json at all")
	if got := configFinishSchema(cfg); got != nil {
		t.Errorf("the finish schema = %s, want it dropped", got)
	}
}

// TestFinishDeclarationSortsIntoTheList pins the `finish` declaration's
// place.
//
// The reference sends nineteen declarations when the client names a
// schema for `finish`, and the new one sorts into the list by name —
// between `find_by_name` and `generate_image`.
func TestFinishDeclarationSortsIntoTheList(t *testing.T) {
	enabled := map[string]bool{"finish": true}
	for _, decl := range tools.HarnessDeclarations() {
		enabled[decl.Name] = true
	}
	decls := harnessDeclarations(enabled, json.RawMessage(`{"type":"object"}`))
	if len(decls) != len(tools.HarnessDeclarations())+1 {
		t.Fatalf("declarations = %d, want %d", len(decls), len(tools.HarnessDeclarations())+1)
	}
	for i, decl := range decls {
		if decl.Name != "finish" {
			continue
		}
		if i == 0 || i == len(decls)-1 {
			t.Fatalf("finish is at position %d, want it inside the list", i)
		}
		if decls[i-1].Name != "find_by_name" {
			t.Errorf("the declaration before finish is %s, want find_by_name", decls[i-1].Name)
		}
		if decls[i+1].Name != "generate_image" {
			t.Errorf("the declaration after finish is %s, want generate_image", decls[i+1].Name)
		}
		return
	}
	t.Fatal("finish is not declared")
}

// TestFinishDeclarationIsAbsentWithoutASchema pins the other half.
//
// The reference sends eighteen declarations when the client names no
// schema for `finish`, and the eighteen are the reference's own.
func TestFinishDeclarationIsAbsentWithoutASchema(t *testing.T) {
	enabled := map[string]bool{"finish": true}
	for _, decl := range tools.HarnessDeclarations() {
		enabled[decl.Name] = true
	}
	decls := harnessDeclarations(enabled, nil)
	if len(decls) != len(tools.HarnessDeclarations()) {
		t.Fatalf("declarations = %d, want %d", len(decls), len(tools.HarnessDeclarations()))
	}
	for _, decl := range decls {
		if decl.Name == "finish" {
			t.Error("finish is declared; the client named no schema for it")
		}
	}
}
