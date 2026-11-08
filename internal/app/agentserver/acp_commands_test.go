package agentserver

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/ironpark/acp-go/acp1"
	"github.com/ironpark/acp-go/acp2"
)

// writeACPTestFile writes content under root, creating parents.
func writeACPTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func hasV1Command(cmds []string, want string) bool {
	for _, name := range cmds {
		if name == want {
			return true
		}
	}
	return false
}

func v1NamesOf(cmds []acp1.AvailableCommand) []string {
	out := make([]string, len(cmds))
	for i, cmd := range cmds {
		out[i] = cmd.Name
	}
	return out
}

func v2NamesOf(cmds []acp2.AvailableCommand) []string {
	out := make([]string, len(cmds))
	for i, cmd := range cmds {
		out[i] = cmd.Name
	}
	return out
}

// TestACPCommands_DefaultCatalogue pins the built-in list: a session
// without a custom agent advertises exactly the old catalogue, with no
// agent or skill rows.
func TestACPCommands_DefaultCatalogue(t *testing.T) {
	cfg := Config{}
	v1 := v1CommandsFor(cfg)
	if len(v1) != len(defaultV1Commands()) {
		t.Fatalf("v1 commands = %d, want %d", len(v1), len(defaultV1Commands()))
	}
	for i := range v1 {
		if v1[i].Name != defaultV1Commands()[i].Name {
			t.Errorf("v1[%d] = %q, want %q", i, v1[i].Name, defaultV1Commands()[i].Name)
		}
	}

	v2 := v2CommandsFor(cfg)
	if len(v2) != len(defaultV2Commands()) {
		t.Fatalf("v2 commands = %d, want %d", len(v2), len(defaultV2Commands()))
	}
	for i := range v2 {
		if v2[i].Name != defaultV2Commands()[i].Name {
			t.Errorf("v2[%d] = %q, want %q", i, v2[i].Name, defaultV2Commands()[i].Name)
		}
	}
	for _, cmd := range v1 {
		if cmd.Name == "/agent" {
			t.Errorf("default catalogue unexpectedly advertises /agent")
		}
	}
}

// TestACPCommands_IncludeAgentAndSkills builds a custom agent on disk
// whose `skills:` lists a directory (with its own SKILL.md), a loose
// Markdown file, and a bare name reference, then asserts both ACP
// versions advertise the agent and every skill.
func TestACPCommands_IncludeAgentAndSkills(t *testing.T) {
	root := t.TempDir()

	writeACPTestFile(t, filepath.Join(root, "skills", "reviewer", "SKILL.md"),
		"---\nname: code-reviewer\ndescription: Reviews code changes\n---\n")
	writeACPTestFile(t, filepath.Join(root, "skills", "loose.md"),
		"---\nname: loose-skill\ndescription: A loose Markdown skill\n---\n")
	agentPath := filepath.Join(root, "helper.md")
	writeACPTestFile(t, agentPath,
		"---\nname: helper\ndescription: Helps with work\nskills:\n  - skills/reviewer\n  - skills/loose.md\n  - plain-name\n---\nHelper body\n")

	cfg := Config{AgentName: "helper", AgentPath: agentPath}

	v1 := v1CommandsFor(cfg)
	for _, want := range []string{"/agent", "/code-reviewer", "/loose-skill", "/plain-name"} {
		if !hasV1Command(v1NamesOf(v1), want) {
			t.Errorf("v1 commands missing %q: %v", want, v1NamesOf(v1))
		}
	}
	if len(v1) != len(defaultV1Commands())+4 {
		t.Errorf("v1 commands = %d, want %d", len(v1), len(defaultV1Commands())+4)
	}

	v2 := v2CommandsFor(cfg)
	for _, want := range []string{"/agent", "/code-reviewer", "/loose-skill", "/plain-name"} {
		if !hasV1Command(v2NamesOf(v2), want) {
			t.Errorf("v2 commands missing %q: %v", want, v2NamesOf(v2))
		}
	}
	if len(v2) != len(defaultV2Commands())+4 {
		t.Errorf("v2 commands = %d, want %d", len(v2), len(defaultV2Commands())+4)
	}

	// The skill descriptions travel with the command.
	for _, cmd := range v1 {
		if cmd.Name == "/code-reviewer" && cmd.Description != "Reviews code changes" {
			t.Errorf("/code-reviewer description = %q", cmd.Description)
		}
	}
}

// TestACPCommands_MissingAgentFileKeepsCatalogue guards the warning path:
// an unusable agent path must not remove the built-in commands.
func TestACPCommands_MissingAgentFileKeepsCatalogue(t *testing.T) {
	cfg := Config{AgentName: "helper", AgentPath: filepath.Join(t.TempDir(), "absent.md")}
	v1 := v1CommandsFor(cfg)
	if !hasV1Command(v1NamesOf(v1), "/model") {
		t.Errorf("built-in commands missing: %v", v1NamesOf(v1))
	}
	if !hasV1Command(v1NamesOf(v1), "/agent") {
		t.Errorf("active agent name missing from commands: %v", v1NamesOf(v1))
	}
}
