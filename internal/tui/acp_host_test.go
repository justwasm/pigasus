package tui

import (
	"bytes"
	"context"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	"github.com/ironpark/acp-go/acp2"
	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/cli"
)

func TestACPHost_StdioEndToEnd(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// Connect client and server via pipes simulating stdin/stdout
	agentStdinReader, clientStdinWriter := io.Pipe()
	clientStdoutReader, agentStdoutWriter := io.Pipe()
	defer agentStdinReader.Close()
	defer clientStdinWriter.Close()
	defer clientStdoutReader.Close()
	defer agentStdoutWriter.Close()

	errBuf := &bytes.Buffer{}

	go func() {
		_ = runAcpHandler(nil, cli.ACPArgs{
			In:        agentStdinReader,
			Out:       agentStdoutWriter,
			Err:       errBuf,
			Ctx:       ctx,
			Workspace: t.TempDir(),
			Model:     "test-model",
		})
	}()

	// Connect as ACP client
	remote, err := agentturn.NewACPRemote(ctx, clientStdinWriter, clientStdoutReader, nil, t.TempDir(), "auto")
	if err != nil {
		t.Fatalf("NewACPRemote failed: %v", err)
	}
	defer remote.Close()

	frames, err := remote.Turn(ctx, agentturn.Turn{
		History: []agentlink.Message{
			{Role: "user", Content: "Hello from ACP Stdio client"},
		},
	})
	if err != nil {
		t.Fatalf("remote.Turn failed: %v", err)
	}

	var sawResult bool
	var responseText string
	for f := range frames {
		switch f.Kind {
		case agentlink.KindText:
			responseText += f.Text
		case agentlink.KindResult:
			sawResult = true
		}
	}

	if !sawResult {
		t.Errorf("expected result frame from ACP stdio turn")
	}
	t.Logf("got response text: %q", responseText)
}

func TestACPHost_TCPListener(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	errBuf := &bytes.Buffer{}

	// Start ACP listening on 127.0.0.1:0
	ready := make(chan string, 1)
	go func() {
		// Listen on an ephemeral port directly to get the address
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			return
		}
		addr := ln.Addr().String()
		ln.Close()
		ready <- addr

		_ = runAcpHandler(nil, cli.ACPArgs{
			Addr:      addr,
			Err:       errBuf,
			Ctx:       ctx,
			Workspace: t.TempDir(),
			Model:     "test-model",
		})
	}()

	select {
	case addr := <-ready:
		// Give listener a moment to bind
		time.Sleep(50 * time.Millisecond)

		conn, err := net.Dial("tcp", addr)
		if err != nil {
			t.Fatalf("net.Dial(%s) failed: %v", addr, err)
		}
		defer conn.Close()

		remote, err := agentturn.NewACPRemote(ctx, conn, conn, conn, t.TempDir(), "1")
		if err != nil {
			t.Fatalf("NewACPRemote over TCP failed: %v", err)
		}
		defer remote.Close()

		frames, err := remote.Turn(ctx, agentturn.Turn{
			History: []agentlink.Message{
				{Role: "user", Content: "Hello over TCP"},
			},
		})
		if err != nil {
			t.Fatalf("remote.Turn failed: %v", err)
		}

		var sawResult bool
		for f := range frames {
			if f.Kind == agentlink.KindResult {
				sawResult = true
			}
		}
		if !sawResult {
			t.Errorf("expected result frame from ACP TCP turn")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting for TCP listener")
	}
}

func TestACPHost_ProtocolVersionFlags(t *testing.T) {
	for _, ver := range []string{"1", "2"} {
		t.Run("Version_"+ver, func(t *testing.T) {
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()

			agentInR, clientInW := io.Pipe()
			clientOutR, agentOutW := io.Pipe()
			defer agentInR.Close()
			defer clientInW.Close()
			defer clientOutR.Close()
			defer agentOutW.Close()

			go func() {
				_ = runAcpHandler(nil, cli.ACPArgs{
					In:        agentInR,
					Out:       agentOutW,
					Err:       &bytes.Buffer{},
					Ctx:       ctx,
					Workspace: t.TempDir(),
					RawArgs:   []string{"--acp-version", ver},
				})
			}()

			if ver == "1" {
				clientConn := acp1.NewClientSideConnection(func(c *acp1.ClientSideConnection) acp1.Client {
					return &testACPClientForHost{}
				}, acp.NewStdioTransport(clientOutR, clientInW))
				go func() { _ = clientConn.Start(ctx) }()

				initResp, err := clientConn.Initialize(ctx, &acp1.InitializeRequest{
					ProtocolVersion: 1,
					ClientInfo: &acp1.Implementation{
						Name:    "zed",
						Version: "0.150.0",
					},
				})
				if err != nil {
					t.Fatalf("Initialize v1 failed: %v", err)
				}
				if initResp.ProtocolVersion != 1 {
					t.Errorf("ProtocolVersion = %d, want 1", initResp.ProtocolVersion)
				}
			} else {
				clientConn := acp2.NewClientSideConnection(func(c *acp2.ClientSideConnection) acp2.Client {
					return &testACPV2ClientForHost{}
				}, acp.NewStdioTransport(clientOutR, clientInW))
				go func() { _ = clientConn.Start(ctx) }()

				initResp, err := clientConn.Initialize(ctx, &acp2.InitializeRequest{
					ProtocolVersion: 2,
					Info: acp2.Implementation{
						Name:    "v2-client",
						Version: "2.0.0",
					},
				})
				if err != nil {
					t.Fatalf("Initialize v2 failed: %v", err)
				}
				if initResp.ProtocolVersion != 2 {
					t.Errorf("ProtocolVersion = %d, want 2", initResp.ProtocolVersion)
				}
			}
		})
	}
}

type testACPClientForHost struct {
	acp1.UnimplementedClient
}

type testACPV2ClientForHost struct {
	acp2.UnimplementedClient
}

func TestParseACPFlags_Agent(t *testing.T) {
	opts := parseACPFlags([]string{"--agent", "helper", "--model", "m"})
	if opts.agent != "helper" {
		t.Errorf("agent = %q, want helper", opts.agent)
	}
	if opts.model != "m" {
		t.Errorf("model = %q, want m", opts.model)
	}
	if opts := parseACPFlags(nil); opts.agent != "" {
		t.Errorf("default agent = %q, want empty", opts.agent)
	}
}

// TestACPHost_AgentFlagAdvertisesSkills runs the ACP host under
// `--agent helper` and asserts the v2 session advertises the custom
// agent's skill in its command list.
func TestACPHost_AgentFlagAdvertisesSkills(t *testing.T) {
	workspace := t.TempDir()
	writeHostTestFile(t, filepath.Join(workspace, ".agents", "agents", "helper.md"),
		"---\nname: helper\ndescription: Helps\nskills:\n  - ../../skills/reviewer\n---\nHelper body\n")
	writeHostTestFile(t, filepath.Join(workspace, "skills", "reviewer", "SKILL.md"),
		"---\nname: code-reviewer\ndescription: Reviews code changes\n---\n")
	t.Chdir(workspace)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agentInR, clientInW := io.Pipe()
	clientOutR, agentOutW := io.Pipe()
	defer agentInR.Close()
	defer clientInW.Close()
	defer clientOutR.Close()
	defer agentOutW.Close()

	go func() {
		_ = runAcpHandler(nil, cli.ACPArgs{
			In:        agentInR,
			Out:       agentOutW,
			Err:       &bytes.Buffer{},
			Ctx:       ctx,
			Workspace: workspace,
			Model:     "test-model",
			RawArgs:   []string{"--agent", "helper", "--acp-version", "2"},
		})
	}()

	clientConn := acp2.NewClientSideConnection(func(c *acp2.ClientSideConnection) acp2.Client {
		return &testACPV2ClientForHost{}
	}, acp.NewStdioTransport(clientOutR, clientInW))
	go func() { _ = clientConn.Start(ctx) }()

	if _, err := clientConn.Initialize(ctx, &acp2.InitializeRequest{
		ProtocolVersion: 2,
		Info:            acp2.Implementation{Name: "v2-client", Version: "2.0.0"},
	}); err != nil {
		t.Fatalf("Initialize v2 failed: %v", err)
	}
	sessResp, err := clientConn.NewSession(ctx, &acp2.NewSessionRequest{
		Cwd: acp2.AbsolutePath(workspace),
	})
	if err != nil {
		t.Fatalf("NewSession failed: %v", err)
	}

	var names []string
	for _, cmd := range sessResp.AvailableCommands {
		names = append(names, cmd.Name)
	}
	for _, want := range []string{"/agent", "/code-reviewer"} {
		found := false
		for _, name := range names {
			if name == want {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("AvailableCommands missing %q: %v", want, names)
		}
	}
}

func writeHostTestFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", filepath.Dir(path), err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}
