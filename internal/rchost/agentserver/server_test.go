package agentserver

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/app/subcommands"
)

func TestParseConfig(t *testing.T) {
	cfg, err := ParseConfig([]string{
		"--addr", "127.0.0.1:9999",
		"--model", "gemini-3.8-flash-high",
		"--workspace", "/tmp/ws",
		"--cloudcode-endpoint", "http://example.invalid",
		"--real-agent=false",
		"--verbose",
	}, nil)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if cfg.Addr != "127.0.0.1:9999" || cfg.Model != "gemini-3.8-flash-high" ||
		cfg.Workspace != "/tmp/ws" || cfg.CloudCodeEndpoint != "http://example.invalid" ||
		cfg.RealAgent || !cfg.Verbose {
		t.Fatalf("config = %+v", cfg)
	}
}

func TestHelpListsFlags(t *testing.T) {
	var out bytes.Buffer
	_, err := ParseConfig([]string{"--help"}, &out)
	if err == nil {
		t.Fatal("--help = nil error")
	}
	text := out.String()
	for _, flagName := range []string{"addr", "model", "workspace", "cloudcode-endpoint", "real-agent", "verbose"} {
		if !strings.Contains(text, flagName) {
			t.Errorf("help does not mention %s:\n%s", flagName, text)
		}
	}
}

func TestAgentServerSubcommandIsRegistered(t *testing.T) {
	for _, spec := range subcommands.GetSubcommands() {
		if spec.Name == "agent-server" {
			if spec.Run == nil {
				t.Fatal("agent-server has no Run handler")
			}
			if spec.Short == "" {
				t.Error("agent-server has no short description")
			}
			return
		}
	}
	t.Fatal("agent-server is not registered as a subcommand")
}

func TestStartStubServer(t *testing.T) {
	srv, err := Start(Config{Addr: "127.0.0.1:0", Model: "gemini-3.8-flash-high", RealAgent: false})
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	defer srv.Close()
	if srv.URL() == "" {
		t.Fatal("server URL is empty")
	}
}

func TestIsLoopback(t *testing.T) {
	cases := []struct {
		url, addr string
		want      bool
	}{
		{"http://127.0.0.1:8080", "127.0.0.1:8080", true},
		{"http://localhost:8080", "127.0.0.1:9999", true},
		{"https://daily-cloudcode-pa.googleapis.com", "127.0.0.1:8080", false},
		{"", "127.0.0.1:8080", false},
	}
	for _, c := range cases {
		if got := isLoopback(c.url, c.addr); got != c.want {
			t.Errorf("isLoopback(%q, %q) = %v, want %v", c.url, c.addr, got, c.want)
		}
	}
}

// TestServePrintsOneEnvLaunchLine pins the startup banner: the reference
// command is a single `env A=... B=... agy --remote-agent` line, so it does
// not need `export` and cannot leak into the caller's shell.
func TestServePrintsOneEnvLaunchLine(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	var out bytes.Buffer
	if err := Serve(ctx, &out, Config{Addr: "127.0.0.1:0", Model: "gemini-3.8-flash-high", RealAgent: false}); err != nil {
		t.Fatalf("Serve: %v", err)
	}
	text := out.String()
	if strings.Contains(text, "export ") {
		t.Errorf("banner still uses export:\n%s", text)
	}
	var launch []string
	for _, line := range strings.Split(text, "\n") {
		if strings.Contains(line, "env AICODE_ENDPOINT_URL=") {
			launch = append(launch, line)
		}
	}
	if len(launch) != 1 {
		t.Fatalf("want one launch line, got %d:\n%s", len(launch), text)
	}
	line := strings.TrimSpace(launch[0])
	if !strings.HasPrefix(line, "env AICODE_ENDPOINT_URL=") {
		t.Errorf("launch line = %q", line)
	}
	if !strings.Contains(line, " CLOUD_CODE_URL=") {
		t.Errorf("launch line missing CLOUD_CODE_URL: %q", line)
	}
}
