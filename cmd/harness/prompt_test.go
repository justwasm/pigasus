// prompt_test.go — the system instruction the harness sends.
//
// The prompt is the reference's own: `harnessprompt` rebuilds it
// byte for byte from a capture of the reference binary. These tests pin
// the *wiring* — which flag turns which section on, and what the client
// may override.
package main

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/harnessprompt"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// promptConversation builds a conversation with the given settings.
func promptConversation(settings sessionSettings, cfg *pb.HarnessConfig) *conversation {
	if cfg == nil {
		cfg = &pb.HarnessConfig{}
	}
	return &conversation{cfg: cfg, settings: settings, logger: newLogger(false)}
}

func TestPromptOptionsFollowTheSideTools(t *testing.T) {
	enabled := true
	side := pb.HarnessSideTools_builder{
		Subagents: pb.SubagentsConfig_builder{Enabled: &enabled}.Build(),
	}.Build()
	c := promptConversation(sessionSettings{SideTools: side}, nil)
	opts := c.promptOptions()
	if !opts.SubagentTool {
		t.Error("the subagent tool is off, want it on")
	}
}

func TestPromptSubagentsAreTheBuiltinsAndTheClients(t *testing.T) {
	name := "reviewer"
	description := "reviews a diff"
	custom := pb.CustomAgent_builder{
		Name:        &name,
		Description: &description,
	}.Build()
	cfg := pb.HarnessConfig_builder{CustomSubagents: []*pb.CustomAgent{custom}}.Build()
	c := promptConversation(sessionSettings{}, cfg)
	subagents := c.promptSubagents()
	if len(subagents) != 3 {
		t.Fatalf("subagents = %d, want 3", len(subagents))
	}
	if subagents[0].Name != harnessprompt.SubagentResearch {
		t.Errorf("the first subagent = %q, want %q", subagents[0].Name, harnessprompt.SubagentResearch)
	}
	if subagents[1].Name != harnessprompt.SubagentSelf {
		t.Errorf("the second subagent = %q, want %q", subagents[1].Name, harnessprompt.SubagentSelf)
	}
	if subagents[2].Name != "reviewer" || subagents[2].Description != description {
		t.Errorf("the client's subagent = %+v, want the client's own", subagents[2])
	}
}

func TestPromptTheSubagentSectionFollowsTheTool(t *testing.T) {
	// With the subagent tool off there is no `<subagents>` section; with
	// it on there is one.
	off := promptConversation(sessionSettings{}, nil).systemPrompt()
	if strings.Contains(off, "<subagents>") {
		t.Error("the prompt has a <subagents> section, want none")
	}

	enabled := true
	side := pb.HarnessSideTools_builder{
		Subagents: pb.SubagentsConfig_builder{Enabled: &enabled}.Build(),
	}.Build()
	on := promptConversation(sessionSettings{SideTools: side}, nil).systemPrompt()
	if !strings.Contains(on, "<subagents>") {
		t.Error("the prompt has no <subagents> section, want one")
	}
	if !strings.Contains(on, harnessprompt.SubagentResearch) {
		t.Error("the prompt does not name the research subagent")
	}
}

func TestPromptTheCustomInstructionReplacesTheDefault(t *testing.T) {
	text := "you are the clone"
	part := pb.CustomSystemInstructions_Part_builder{Text: &text}.Build()
	custom := pb.CustomSystemInstructions_builder{Part: []*pb.CustomSystemInstructions_Part{part}}.Build()
	cfg := pb.HarnessConfig_builder{
		SystemInstructions: pb.SystemInstructions_builder{Custom: custom}.Build(),
	}.Build()
	c := promptConversation(sessionSettings{}, cfg)
	got := c.systemPrompt()
	if !strings.Contains(got, text) {
		t.Errorf("the prompt does not carry the custom text: %q", got)
	}
}

func TestPromptTheAppendedInstructionKeepsTheDefault(t *testing.T) {
	identity := "you are a reviewer"
	section := pb.AppendedSystemInstructions_Section_builder{
		Title:   strptr("user_system_instructions"),
		Content: strptr("be careful"),
	}.Build()
	appended := pb.AppendedSystemInstructions_builder{
		CustomIdentity:   &identity,
		AppendedSections: []*pb.AppendedSystemInstructions_Section{section},
	}.Build()
	cfg := pb.HarnessConfig_builder{
		SystemInstructions: pb.SystemInstructions_builder{Appended: appended}.Build(),
	}.Build()
	c := promptConversation(sessionSettings{}, cfg)
	got := c.systemPrompt()
	if !strings.Contains(got, identity) {
		t.Errorf("the prompt does not carry the identity: %q", got)
	}
	if !strings.Contains(got, "be careful") {
		t.Errorf("the prompt does not carry the appended section: %q", got)
	}
}
