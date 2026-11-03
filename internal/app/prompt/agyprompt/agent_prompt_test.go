package agyprompt

import (
	"strings"
	"testing"
)

func TestBuildAgent_SectionOrderAndPrefix(t *testing.T) {
	o := Options{
		OSVersion:      "mac",
		Workspaces:     []string{"/w"},
		CWD:            "/w",
		AppDataDir:     "/home/u/.gemini/antigravity-cli",
		ConversationID: "00000000-0000-0000-0000-000000000000",
	}
	got := BuildAgent("You are foo.\n", o)

	if !strings.HasPrefix(got, "<System Prompt>\nYou are foo.\n</System Prompt>\n<user_information>") {
		t.Fatalf("prefix = %q", got[:min(120, len(got))])
	}
	order := []string{"user_information", "skills", "messaging", "artifacts", "guidelines", "communication_style"}
	pos := -1
	for _, name := range order {
		at := strings.Index(got, "<"+name+">")
		if at < 0 {
			t.Fatalf("missing section %q", name)
		}
		if at <= pos {
			t.Errorf("section %q out of order", name)
		}
		pos = at
	}
	for _, absent := range []string{"<identity>", "<subagents>", "<slash_commands>", "<conversation_transcript>"} {
		if strings.Contains(got, absent) {
			t.Errorf("custom agent prompt must not contain %s", absent)
		}
	}
	if !strings.HasSuffix(got, "</communication_style>") || strings.HasSuffix(got, "\n") {
		t.Errorf("prompt must end without a trailing newline")
	}
}

func TestBuildAgent_MessagingOmitsPeerAgents(t *testing.T) {
	got := BuildAgent("body", DefaultOptions("/w", "/a", "id"))
	if strings.Contains(got, "subagent or peer agent") || strings.Contains(got, "agents, background tasks") {
		t.Errorf("custom-agent messaging still names subagents:\n%s", got)
	}
	if !strings.Contains(got, "background tasks, user-queued messages") {
		t.Errorf("custom-agent messaging preamble missing")
	}
}

func TestBuildAgent_FiltersBuiltinSkillsOnly(t *testing.T) {
	o := DefaultOptions("/w", "/a", "id")
	o.Skills = []Skill{
		{Name: "builtin-one", Path: "/a/builtin/skills/b1/SKILL.md", Description: "b"},
		{Name: "plugin", Path: "/home/u/.gemini/config/plugins/p/skills/x/SKILL.md", Description: "p"},
		{Name: "workspace", Path: "/w/.agents/skills/y/SKILL.md", Description: "w"},
	}
	got := BuildAgent("body", o)
	if strings.Contains(got, "builtin-one") {
		t.Error("built-in skill should be filtered out of a custom agent prompt")
	}
	for _, want := range []string{"plugin", "workspace"} {
		if !strings.Contains(got, want) {
			t.Errorf("missing %q in skills section", want)
		}
	}
}
