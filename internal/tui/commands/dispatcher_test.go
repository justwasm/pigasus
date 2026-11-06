package commands_test

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/tui/commands"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

type mockHost struct {
	responded   string
	promptToken string
	promptText  string
	backend     string
}

func (m *mockHost) SlashScopeContext() slashcommands.ScopeContext {
	return slashcommands.ScopeContext{EngineName: m.backend}
}

func (m *mockHost) SlashRespond(cmd, msg string, extra ...string) {
	m.responded = cmd + ": " + msg
}

func (m *mockHost) SendAsPrompt(token, prompt string) tea.Cmd {
	m.promptToken = token
	m.promptText = prompt
	return nil
}

func (m *mockHost) EngineSlashCommands() []slashcommands.Command {
	return []slashcommands.Command{
		{Name: "/agent-cmd", Description: "Test agent command"},
	}
}

func (m *mockHost) TurnEngine() any {
	return nil
}

func (m *mockHost) LookupSkillPrompt(name, args string) (string, bool) {
	if name == "my-skill" {
		return "Skill instruction prompt: " + args, true
	}
	return "", false
}

func TestDispatcher_CustomHandler(t *testing.T) {
	host := &mockHost{backend: "pigasus"}
	disp := commands.NewDispatcher(host)

	called := false
	disp.Register("hello", func(args string) (bool, tea.Cmd) {
		called = true
		if args != "world" {
			t.Errorf("expected args 'world', got %q", args)
		}
		return true, nil
	})

	handled, _ := disp.Dispatch("/hello world")
	if !handled || !called {
		t.Fatalf("expected /hello command to be handled and executed")
	}
}

func TestDispatcher_Skill(t *testing.T) {
	host := &mockHost{backend: "pigasus"}
	disp := commands.NewDispatcher(host)

	handled, _ := disp.Dispatch("/my-skill do-task")
	if !handled {
		t.Fatalf("expected /my-skill command to be handled")
	}
	if host.promptToken != "/my-skill do-task" {
		t.Fatalf("expected promptToken '/my-skill do-task', got %q", host.promptToken)
	}
}

func TestDispatcher_Chained(t *testing.T) {
	host := &mockHost{backend: "pigasus"}
	disp := commands.NewDispatcher(host)

	var gotCommands []string
	var gotTask string
	disp.RegisterChained(func(commands []string, task string) (bool, tea.Cmd) {
		gotCommands = commands
		gotTask = task
		return true, nil
	})

	handled, _ := disp.Dispatch("/boost /plan /goal execute refactor")
	if !handled {
		t.Fatalf("expected chained dispatch to be handled")
	}
	if len(gotCommands) != 3 || gotCommands[0] != "boost" || gotCommands[1] != "plan" || gotCommands[2] != "goal" {
		t.Errorf("gotCommands = %v, want [boost plan goal]", gotCommands)
	}
	if gotTask != "execute refactor" {
		t.Errorf("gotTask = %q, want 'execute refactor'", gotTask)
	}
}
