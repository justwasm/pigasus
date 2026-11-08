package commands

import (
	"context"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/agentturn"
)

type mockSteerHost struct {
	engine   any
	response string
}

func (m *mockSteerHost) TurnEngine() any {
	return m.engine
}

func (m *mockSteerHost) SlashRespond(cmd, msg string, extra ...string) {
	m.response = msg
}

type mockSteerEngine struct {
	steered string
}

func (m *mockSteerEngine) Steer(instruction string) error {
	m.steered = instruction
	return nil
}

type mockActionEngine struct {
	action string
	args   string
}

func (m *mockActionEngine) ExecuteAction(ctx context.Context, action string, args string) (agentturn.ActionResult, error) {
	m.action = action
	m.args = args
	return agentturn.ActionResult{
		Success: true,
		Message: "Custom steer handled",
	}, nil
}

func TestRegisterEngineCommands_SteerDirect(t *testing.T) {
	eng := &mockSteerEngine{}
	host := &mockSteerHost{engine: eng}
	d := NewDispatcher(nil)
	RegisterEngineCommands(d, host)

	handled, _ := d.Dispatch("/steer keep it brief")
	if !handled {
		t.Fatal("expected /steer to be handled")
	}
	if eng.steered != "keep it brief" {
		t.Errorf("steered = %q, want 'keep it brief'", eng.steered)
	}
	if !strings.Contains(host.response, "Steering guidance queued") {
		t.Errorf("unexpected response: %q", host.response)
	}
}

func TestRegisterEngineCommands_SteerActionHandler(t *testing.T) {
	eng := &mockActionEngine{}
	host := &mockSteerHost{engine: eng}
	d := NewDispatcher(nil)
	RegisterEngineCommands(d, host)

	handled, _ := d.Dispatch("/steer use tests")
	if !handled {
		t.Fatal("expected /steer to be handled")
	}
	if eng.action != "steer" || eng.args != "use tests" {
		t.Errorf("action=%q args=%q, want steer / use tests", eng.action, eng.args)
	}
	if host.response != "Custom steer handled" {
		t.Errorf("host response = %q, want 'Custom steer handled'", host.response)
	}
}

func TestRegisterEngineCommands_SteerEmpty(t *testing.T) {
	host := &mockSteerHost{engine: &mockSteerEngine{}}
	d := NewDispatcher(nil)
	RegisterEngineCommands(d, host)

	handled, _ := d.Dispatch("/steer")
	if !handled {
		t.Fatal("expected /steer to be handled")
	}
	if !strings.Contains(host.response, "Usage:") {
		t.Errorf("expected usage message, got %q", host.response)
	}
}
