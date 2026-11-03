package main

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
	"github.com/ironpark/acp-go/acp2"
	"github.com/ironpark/acp-go/router"
	"github.com/justwasm/pigasus/internal/core/version"
)

func TestPigcap_Version(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"--version"}, nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	want := "pigcap " + version.Reference + " (agy-acp-server clone)\n"
	if out.String() != want {
		t.Errorf("got %q, want %q", out.String(), want)
	}
}

func TestPigcap_Help(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"--help"}, nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(errOut.String(), "Standalone Agent Client Protocol (ACP) server") {
		t.Errorf("unexpected help text: %s", errOut.String())
	}
}

func TestPigcap_StdioHandshake_V1(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agentInR, clientInW := io.Pipe()
	clientOutR, agentOutW := io.Pipe()
	defer agentInR.Close()
	defer clientInW.Close()
	defer clientOutR.Close()
	defer agentOutW.Close()

	var errBuf bytes.Buffer
	go func() {
		_ = run([]string{"--workspace", t.TempDir(), "--model", "test-model"}, agentInR, agentOutW, &errBuf)
	}()

	clientConn := acp1.NewClientSideConnection(func(c *acp1.ClientSideConnection) acp1.Client {
		return &dummyV1Client{}
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
		t.Fatalf("v1 Initialize failed: %v", err)
	}

	if initResp.ProtocolVersion != 1 {
		t.Errorf("ProtocolVersion = %d, want 1", initResp.ProtocolVersion)
	}
	if initResp.AgentInfo == nil || (initResp.AgentInfo.Name != "Google Antigravity" && initResp.AgentInfo.Name != "antigravity-acp") {
		t.Errorf("unexpected agent info: %+v", initResp.AgentInfo)
	}
}

func TestPigcap_StdioHandshake_V2(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agentInR, clientInW := io.Pipe()
	clientOutR, agentOutW := io.Pipe()
	defer agentInR.Close()
	defer clientInW.Close()
	defer clientOutR.Close()
	defer agentOutW.Close()

	var errBuf bytes.Buffer
	go func() {
		_ = run([]string{"--workspace", t.TempDir(), "--model", "test-model"}, agentInR, agentOutW, &errBuf)
	}()

	clientConn := acp2.NewClientSideConnection(func(c *acp2.ClientSideConnection) acp2.Client {
		return &dummyV2Client{}
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
		t.Fatalf("v2 Initialize failed: %v", err)
	}

	if initResp.ProtocolVersion != 2 {
		t.Errorf("ProtocolVersion = %d, want 2", initResp.ProtocolVersion)
	}
	if initResp.Info.Name != "Google Antigravity" && initResp.Info.Name != "antigravity-acp" {
		t.Errorf("unexpected agent info: %+v", initResp.Info)
	}
}

func TestPigcap_Notices(t *testing.T) {
	var out, errOut bytes.Buffer
	code := run([]string{"--notices"}, nil, &out, &errOut)
	if code != 0 {
		t.Fatalf("expected exit code 0, got %d", code)
	}
	if !strings.Contains(out.String(), "Google Antigravity Agent Client Protocol") {
		t.Errorf("unexpected notices text: %s", out.String())
	}
}

func TestPigcap_DualAutoNegotiation(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	agentInR, clientInW := io.Pipe()
	clientOutR, agentOutW := io.Pipe()
	defer agentInR.Close()
	defer clientInW.Close()
	defer clientOutR.Close()
	defer agentOutW.Close()

	var errBuf bytes.Buffer
	go func() {
		_ = run([]string{"--workspace", t.TempDir(), "--model", "test-model", "--acp-version", "auto"}, agentInR, agentOutW, &errBuf)
	}()

	client := &dummyV2Client{}
	agent, err := router.NewClient().
		WithV2(func(c *acp2.ClientSideConnection) acp2.Client {
			return client
		}, &acp2.InitializeRequest{
			Info: acp2.Implementation{Name: "negotiator", Version: "1.0.0"},
		}).
		Connect(ctx, func(context.Context) (acp.Transport, error) {
			return acp.NewStdioTransport(clientOutR, clientInW), nil
		})
	if err != nil {
		t.Fatalf("negotiation failed: %v", err)
	}
	defer agent.Close()

	if agent.V2 == nil {
		t.Fatalf("expected V2 negotiated agent")
	}
}

type dummyV1Client struct {
	acp1.UnimplementedClient
}

type dummyV2Client struct {
	acp2.UnimplementedClient
}
