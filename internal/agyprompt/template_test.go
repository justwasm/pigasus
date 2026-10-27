package agyprompt

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/agytemplates"
)

// The recovered templates' digests are pinned in internal/agytemplates,
// which owns the bundle. This package only consumes it.
func TestEveryTemplateRenders(t *testing.T) {
	data := Options{
		ArtifactDirectoryPath: "/artifacts",
		TranscriptLogsPath:    "/logs",
	}.templateOptions()
	set := newTemplateSet(data)

	for _, rel := range agytemplates.Paths() {
		body, err := agytemplates.Read(rel)
		if err != nil {
			t.Fatalf("%s: %v", rel, err)
		}
		if !strings.Contains(string(body), "{{") {
			continue // a template with no actions needs no data
		}
		if err := set.ExecuteTemplate(io_Discard{}, rel, data); err != nil {
			// The step-string templates need a step proto the clone does
			// not model here; internal/agystep renders those. Everything
			// else has to render with the prompt's data alone.
			if strings.HasPrefix(rel, "step_strings/") {
				continue
			}
			t.Errorf("%s does not render: %v", rel, err)
		}
	}
}

// io_Discard is a tiny io.Writer so the smoke test does not have to import
// io for one use.
type io_Discard struct{}

func (io_Discard) Write(p []byte) (int, error) { return len(p), nil }

// TestFlagsReachTheTemplates checks that the branches the recovered
// templates take are reachable at all: a template that ignores its flag is
// indistinguishable from a template that lost it.
func TestFlagsReachTheTemplates(t *testing.T) {
	base := DefaultOptions("/w", "/agent", "conv")

	autonomous := base
	autonomous.Autonomous = true
	if got := SectionBody(autonomous, "identity"); !strings.Contains(got, "The USER is not available") {
		t.Error("the autonomous branch of identity.tmpl is not reachable")
	}
	if got := SectionBody(autonomous, "communication_style"); !strings.Contains(got, "make your best judgement") {
		t.Error("the autonomous branch of communication_style.tmpl is not reachable")
	}
	if got := SectionBody(base, "identity"); strings.Contains(got, "The USER is not available") {
		t.Error("the non-autonomous prompt mentions the autonomous branch")
	}

	latex := base
	latex.SupportsLatex = true
	if got := SectionBody(latex, "communication_style"); !strings.Contains(got, "KaTeX") {
		t.Error("the LaTeX branch of communication_style.tmpl is not reachable")
	}
	if got := SectionBody(latex, "artifacts"); !strings.Contains(got, "## LaTeX / Math") {
		t.Error("the LaTeX branch of artifacts.tmpl is not reachable")
	}
	if got := SectionBody(base, "communication_style"); strings.Contains(got, "KaTeX") {
		t.Error("the default prompt has the LaTeX branch")
	}

	plan := base
	plan.PlanModel = "MODEL_PLACEHOLDER_M54"
	if got := SectionBody(plan, "communication_style"); !strings.Contains(got, "CRITICAL INSTRUCTION 1") {
		t.Error("the plan-model branch of communication_style.tmpl is not reachable")
	}

	exp := base
	exp.Experiments = map[string]bool{"enable-url-artifacts": true}
	if got := SectionBody(exp, "artifacts"); !strings.Contains(got, "Bookmarking URLs with URL Artifacts") {
		t.Error("the enable-url-artifacts branch of artifacts.tmpl is not reachable")
	}
}

// TestPlaceholderPathsAreTheReferences checks the two paths the templates
// print. The reference sends placeholders, not the real directories — the
// real ones are in <user_information>.
func TestPlaceholderPathsAreTheReferences(t *testing.T) {
	body := SectionBody(DefaultOptions("/w", "/agent", "conv"), "artifacts")
	if !strings.Contains(body, "`<appDataDir>/brain/<conversation-id>`") {
		t.Error("the artifacts section does not name the reference's artifact directory placeholder")
	}
	if strings.Contains(body, "/agent/brain/conv") {
		t.Error("the artifacts section leaks the real artifact directory")
	}
	body = SectionBody(DefaultOptions("/w", "/agent", "conv"), "conversation_transcript")
	if !strings.Contains(body, "`<appDataDir>/brain/<conversation-id>/.system_generated/logs/transcript.jsonl`") {
		t.Error("the conversation_transcript section does not name the reference's logs placeholder")
	}
}

// TestSlashCommandsAreTheReferences pins the nine commands the reference
// recommends, in its order.
func TestSlashCommandsAreTheReferences(t *testing.T) {
	want := []string{"goal", "compact", "schedule", "browser", "plan", "grill-me", "teamwork", "learn", "boost"}
	got := DefaultSlashCommands()
	if len(got) != len(want) {
		t.Fatalf("%d slash commands, want %d", len(got), len(want))
	}
	for i, c := range got {
		if c.Name != want[i] {
			t.Errorf("slash command %d is %q, want %q", i, c.Name, want[i])
		}
		if c.Recommendation == "" {
			t.Errorf("slash command %q has no recommendation", c.Name)
		}
	}
	body := SectionBody(DefaultOptions("/w", "/agent", "conv"), "slash_commands")
	for _, name := range want {
		if !strings.Contains(body, "- /"+name+": ") {
			t.Errorf("the prompt does not recommend /%s", name)
		}
	}
}

// TestSkillsSectionRendersTheList checks the skills template's list format —
// the reference prints "- name (path): description".
func TestSkillsSectionRendersTheList(t *testing.T) {
	o := DefaultOptions("/w", "/agent", "conv")
	o.Skills = []Skill{{Name: "alpha", Path: "/s/alpha/SKILL.md", Description: "First one."}}
	body := SectionBody(o, "skills")
	if !strings.Contains(body, "- alpha (/s/alpha/SKILL.md): First one.") {
		t.Errorf("the skills section does not use the reference's list format:\n%s", body)
	}
}

// TestTemplateFilesAreNotEditable guards the sections that have no template:
// they are literal prompt text in the reference binary, and there is nothing
// to regenerate them from.
func TestLiteralSectionsExist(t *testing.T) {
	for _, name := range []string{"terminal_sandbox.txt", "messaging.txt", "agent_messaging.txt", "subagents_head.txt", "subagents_tail.txt"} {
		if _, err := os.Stat(filepath.Join("sections", name)); err != nil {
			t.Errorf("%s is missing: %v", name, err)
		}
	}
}
