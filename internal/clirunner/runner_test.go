package clirunner_test

import (
	"bytes"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/clirunner"
)

func TestSetupCLIParser_SubcommandsRegistered(t *testing.T) {
	p := clirunner.SetupCLIParser("pigasus")
	specs := p.SubcommandSpecs()

	foundServer := false
	foundTUI := false
	for _, s := range specs {
		if s.Name == "server" {
			foundServer = true
		}
		if s.Name == "tui" {
			foundTUI = true
		}
	}
	if !foundServer {
		t.Errorf("server subcommand not found in SetupCLIParser specs")
	}
	if !foundTUI {
		t.Errorf("tui subcommand not found in SetupCLIParser specs")
	}
}

func TestSetupCLIParser_ServerHelp(t *testing.T) {
	p := clirunner.SetupCLIParser("pigasus")
	var out, errOut bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errOut)

	err := p.ExecuteArgs([]string{"server", "--help"})
	if err != nil {
		t.Fatalf("execute server --help error: %v", err)
	}
	if p.ExitCode() != 0 {
		t.Errorf("exit code = %d, want 0", p.ExitCode())
	}
	stderr := errOut.String()
	for _, flag := range []string{"--addr", "--port", "--host", "--workspace", "--model", "--token"} {
		if !strings.Contains(stderr, flag) {
			t.Errorf("server --help missing %s in output:\n%s", flag, stderr)
		}
	}
}

func TestSetupCLIParser_ServerDispatch(t *testing.T) {
	p := clirunner.SetupCLIParser("pigasus")
	var captured cli.ServerArgs
	cli.RegisterServerHandler(func(j *cli.JetskiCLI, args cli.ServerArgs) error {
		captured = args
		return nil
	})
	t.Cleanup(func() { cli.RegisterServerHandler(nil) })
	var out, errOut bytes.Buffer
	p.SetOut(&out)
	p.SetErr(&errOut)

	args := []string{
		"server",
		"--addr", "127.0.0.1:9099",
		"--workspace", "/tmp/testws",
		"--model", "claude-3-5-sonnet",
		"--token", "secret123",
	}
	if err := p.ExecuteArgs(args); err != nil {
		t.Fatalf("execute server dispatch failed: %v", err)
	}
	if captured.Addr != "127.0.0.1:9099" {
		t.Errorf("captured addr = %q, want 127.0.0.1:9099", captured.Addr)
	}
	if captured.Workspace != "/tmp/testws" {
		t.Errorf("captured workspace = %q, want /tmp/testws", captured.Workspace)
	}
	if captured.Model != "claude-3-5-sonnet" {
		t.Errorf("captured model = %q, want claude-3-5-sonnet", captured.Model)
	}
	if captured.Token != "secret123" {
		t.Errorf("captured token = %q, want secret123", captured.Token)
	}
}

func TestRun_MultiCallBinaryACP(t *testing.T) {
	for _, alias := range []string{"agy-acp-server", "pigcap"} {
		t.Run(alias, func(t *testing.T) {
			var captured cli.ACPArgs
			cli.RegisterACPHandler(func(j *cli.JetskiCLI, args cli.ACPArgs) error {
				captured = args
				return nil
			})
			t.Cleanup(func() { cli.RegisterACPHandler(nil) })

			code := clirunner.Run([]string{alias, "--stdio", "--model", "custom-model"})
			if code != 0 {
				t.Errorf("expected exit code 0, got %d", code)
			}
			foundStdio := false
			foundModel := false
			for i, arg := range captured.RawArgs {
				if arg == "--stdio" {
					foundStdio = true
				}
				if arg == "--model" && i+1 < len(captured.RawArgs) && captured.RawArgs[i+1] == "custom-model" {
					foundModel = true
				}
			}
			if !foundStdio {
				t.Errorf("expected --stdio in captured args: %v", captured.RawArgs)
			}
			if !foundModel {
				t.Errorf("expected --model custom-model in captured args: %v", captured.RawArgs)
			}
		})
	}
}
