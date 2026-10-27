package rchost

import (
	"bytes"
	"os"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
)

// TestDesktopMendelUserAgent pins the identity the web server's Mendel
// read carries.
//
// 【一手】 2026-10-27: the backend answers a different flag set per
// product — `antigravity/hub/…` gets the desktop app's features (the
// voice input, the jetbox terminal, the aux pane redesign), the CLI's
// and the IDE's sets do not. The desktop mode asks for the hub's set;
// the default is the CLI's own.
func TestDesktopMendelUserAgent(t *testing.T) {
	if got := desktopMendelUserAgent(false); got != "" {
		t.Errorf("the default carries %q, want the CLI's own identity (empty)", got)
	}
	got := desktopMendelUserAgent(true)
	if !strings.HasPrefix(got, "antigravity/hub/") {
		t.Errorf("the desktop mode carries %q, want the hub identity", got)
	}
	if got == auth.MendelUserAgentCLI {
		t.Errorf("the desktop mode carries the CLI's own identity")
	}
}

func TestResolveWebAddr(t *testing.T) {
	tests := []struct {
		name string
		args cli.WebArgs
		want string
	}{
		{
			name: "default",
			args: cli.WebArgs{},
			want: "0.0.0.0:8787",
		},
		{
			name: "port only",
			args: cli.WebArgs{Port: 8080},
			want: "0.0.0.0:8080",
		},
		{
			name: "host and port",
			args: cli.WebArgs{Host: "127.0.0.1", Port: 9000},
			want: "127.0.0.1:9000",
		},
		{
			name: "host only",
			args: cli.WebArgs{Host: "192.168.1.50"},
			want: "192.168.1.50:8787",
		},
		{
			name: "colon port",
			args: cli.WebArgs{Addr: ":8081"},
			want: "0.0.0.0:8081",
		},
		{
			name: "full addr",
			args: cli.WebArgs{Addr: "127.0.0.1:9090"},
			want: "127.0.0.1:9090",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveWebAddr(tt.args)
			if got != tt.want {
				t.Errorf("resolveWebAddr(%+v) = %q, want %q", tt.args, got, tt.want)
			}
		})
	}
}

func TestRunWeb_Lifecycle(t *testing.T) {
	oldSignalProvider := webSignalChProvider
	defer func() { webSignalChProvider = oldSignalProvider }()

	sigCh := make(chan os.Signal, 1)
	webSignalChProvider = func(c chan<- os.Signal) {
		go func() {
			s := <-sigCh
			c <- s
		}()
	}

	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	j := &cli.JetskiCLI{
		Name:   "pigasus",
		Out:    out,
		ErrOut: errOut,
	}

	// Trigger SIGTERM after 100ms
	go func() {
		time.Sleep(100 * time.Millisecond)
		sigCh <- syscall.SIGTERM
	}()

	err := RunWeb(j, cli.WebArgs{
		Addr: "127.0.0.1:0", // random available port
	})
	if err != nil {
		t.Fatalf("RunWeb failed: %v", err)
	}

	outStr := out.String()
	if !strings.Contains(outStr, "pigasus web: server listening on") {
		t.Errorf("expected startup banner in output, got:\n%s", outStr)
	}
	if !strings.Contains(outStr, "Local URL:") {
		t.Errorf("expected Local URL in output, got:\n%s", outStr)
	}

	errStr := errOut.String()
	if !strings.Contains(errStr, "caught signal terminated, shutting down") {
		t.Errorf("expected shutdown message in errOut, got:\n%s", errStr)
	}
}
