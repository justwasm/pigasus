package modular_test

import (
	"bytes"
	"context"
	"flag"
	"io"
	"testing"

	"github.com/justwasm/pigasus/internal/modular"
)

type echoEngine struct{}

func (e *echoEngine) Name() string { return "echo" }
func (e *echoEngine) Send(ctx context.Context, in modular.TurnInput) (*modular.TurnResult, error) {
	msg := "echo: " + in.Prompt
	if in.Out != nil {
		_, _ = io.WriteString(in.Out, msg)
	}
	return &modular.TurnResult{Response: msg, Model: in.Model}, nil
}

func TestAppPrintMode(t *testing.T) {
	modular.RegisterEngine("echo", &echoEngine{})

	app := modular.NewApp(modular.AppOptions{
		Name:          "testapp",
		Version:       "1.0.0-test",
		DefaultEngine: "echo",
	})

	var out bytes.Buffer
	// Create an app instance and test parser execution directly
	code := app.Run([]string{"testapp", "--print", "hello world"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	_ = out
}

func TestAppInteractiveModeWarning(t *testing.T) {
	app := modular.NewApp(modular.AppOptions{
		Name:          "testapp",
		Version:       "1.0.0-test",
		DefaultEngine: "echo",
	})

	code := app.Run([]string{"testapp", "-i", "start interactive session"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
}

func TestAppHelpSubcommand(t *testing.T) {
	app := modular.NewApp(modular.AppOptions{
		Name:          "testapp",
		Version:       "1.0.0-test",
		DefaultEngine: "echo",
	})

	code := app.Run([]string{"testapp", "--version"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
}

func TestAppCustomEngineSelection(t *testing.T) {
	app := modular.NewApp(modular.AppOptions{
		Name:          "testapp",
		Version:       "1.0.0-test",
		DefaultEngine: "echo",
	})

	code := app.Run([]string{"testapp", "-p", "what is this?"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
}

func TestAppNoArgsHelp(t *testing.T) {
	app := modular.NewApp(modular.AppOptions{
		Name:          "testapp",
		Version:       "1.0.0-test",
		DefaultEngine: "echo",
	})

	// Must succeed without failing on process os.Args flags
	code := app.Run([]string{"testapp"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
}

func TestAppNoArgsInteractiveRunner(t *testing.T) {
	invoked := false
	app := modular.NewApp(modular.AppOptions{
		Name:          "testapp",
		Version:       "1.0.0-test",
		DefaultEngine: "echo",
		InteractiveRunner: func(w io.Writer, fs *flag.FlagSet) error {
			invoked = true
			return nil
		},
	})

	code := app.Run([]string{"testapp"})
	if code != 0 {
		t.Fatalf("expected code 0, got %d", code)
	}
	if !invoked {
		t.Fatalf("expected InteractiveRunner to be invoked when no args passed")
	}
}

