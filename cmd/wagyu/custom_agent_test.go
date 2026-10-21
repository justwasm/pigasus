package main

import (
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
)

// The default agent's <subagents> section lists markdown agents discovered in
// the workspace/user scopes (agy 1.4.3: "- foo: A test agent" appeared after
// image-generator in a live prompt). customSubagents is the cmd/wagyu bridge
// between internal/agent discovery and agyprompt.
func TestCustomSubagents_AppendsDiscoveredAgents(t *testing.T) {
	ws, home := t.TempDir(), t.TempDir()
	dir := filepath.Join(ws, ".agents", "agents")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: foo\ndescription: A test agent\n---\nYou are foo.\n"
	if err := os.WriteFile(filepath.Join(dir, "foo.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(ws)
	t.Setenv("HOME", home)

	got := customSubagents()
	if len(got) != 1 || got[0].Name != "foo" || got[0].Description != "A test agent" {
		t.Fatalf("customSubagents() = %+v", got)
	}
}

// A --agent session advertises exactly the seven captured declarations; in
// particular the ask_question re-registration for subagents must not add an
// eighth tool to the filtered catalogue.
func TestAgentToolRegistry_CustomAgentHasSevenTools(t *testing.T) {
	m := &runMode{agentBody: "You are foo."}
	catalog := agentToolRegistry(t.TempDir(), m.agentBody)
	m.registerSubagentQuestionTool(catalog)

	names := catalog.List()
	sort.Strings(names)
	want := "generate_image,manage_task,read_url_content,schedule,search_web,send_message,view_file"
	if got := strings.Join(names, ","); got != want {
		t.Fatalf("custom agent tools = %s, want %s", got, want)
	}
	if _, ok := catalog.Get("ask_question"); ok {
		t.Error("ask_question leaked into the custom agent surface")
	}
}

func TestAgentToolRegistry_DefaultIncludesAskQuestion(t *testing.T) {
	m := &runMode{}
	catalog := agentToolRegistry(t.TempDir(), "")
	m.registerSubagentQuestionTool(catalog)
	if _, ok := catalog.Get("ask_question"); !ok {
		t.Error("default agent lost ask_question")
	}
}
