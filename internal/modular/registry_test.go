package modular_test

import (
	"context"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/modular"
)


type dummyEngine struct {
	name string
}

func (d *dummyEngine) Name() string { return d.name }
func (d *dummyEngine) Send(ctx context.Context, in modular.TurnInput) (*modular.TurnResult, error) {
	return &modular.TurnResult{Response: "dummy reply", Model: in.Model}, nil
}

type dummyToolProvider struct {
	name string
}

func (p *dummyToolProvider) Name() string { return p.name }
func (p *dummyToolProvider) Declarations() []backend.ToolDecl {
	return []backend.ToolDecl{
		{Name: "dummy_calc", Description: "a dummy calculator"},
	}
}
func (p *dummyToolProvider) Run(ctx context.Context, call backend.ToolCall) (string, error) {
	if call.Name == "dummy_calc" {
		return "42", nil
	}
	return "", modular.ErrUnknownTool(call.Name)
}

func TestEngineRegistry(t *testing.T) {
	eng := &dummyEngine{name: "test-engine"}
	modular.RegisterEngine("test-engine", eng)

	got, ok := modular.GetEngine("test-engine")
	if !ok || got == nil {
		t.Fatalf("expected engine to be found")
	}
	if got.Name() != "test-engine" {
		t.Fatalf("expected test-engine, got %s", got.Name())
	}

	res, err := got.Send(context.Background(), modular.TurnInput{Prompt: "hi", Model: "test-m"})
	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if res.Response != "dummy reply" {
		t.Fatalf("unexpected response: %s", res.Response)
	}
}

func TestToolProviderRegistry(t *testing.T) {
	prov := &dummyToolProvider{name: "test-provider"}
	modular.RegisterToolProvider(prov)

	decls := modular.AllToolDeclarations()
	found := false
	for _, d := range decls {
		if d.Name == "dummy_calc" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("expected dummy_calc in AllToolDeclarations")
	}

	res, err := modular.DispatchToolCall(context.Background(), backend.ToolCall{
		Name:      "dummy_calc",
		Arguments: []byte("{}"),
	})
	if err != nil {
		t.Fatalf("dispatch error: %v", err)
	}
	if res != "42" {
		t.Fatalf("expected 42, got %s", res)
	}
}

type dummySubsystem struct {
	name    string
	enabled bool
}

func (s *dummySubsystem) Name() string                     { return s.name }
func (s *dummySubsystem) Enabled() bool                    { return s.enabled }
func (s *dummySubsystem) Init(ctx context.Context) error   { return nil }
func (s *dummySubsystem) Shutdown(ctx context.Context) error { return nil }

func TestSubsystemRegistry(t *testing.T) {
	modular.RegisterSubsystem(&dummySubsystem{name: "sub-on", enabled: true})
	modular.RegisterSubsystem(&dummySubsystem{name: "sub-off", enabled: false})

	if !modular.HasSubsystem("sub-on") {
		t.Errorf("expected sub-on to be true")
	}
	if modular.HasSubsystem("sub-off") {
		t.Errorf("expected sub-off to be false")
	}
	if modular.HasSubsystem("non-existent") {
		t.Errorf("expected non-existent to be false")
	}

	sub, ok := modular.GetSubsystem("sub-on")
	if !ok || sub == nil || sub.Name() != "sub-on" {
		t.Errorf("expected GetSubsystem to return sub-on")
	}
}

func TestSubcommandRegistry(t *testing.T) {
	modular.RegisterSubcommand(cli.SubcommandSpec{
		Name:  "custom-cmd",
		Short: "Custom modular subcommand",
	})

	subs := modular.GetSubcommands()
	found := false
	for _, s := range subs {
		if s.Name == "custom-cmd" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected custom-cmd in registered subcommands")
	}
}

