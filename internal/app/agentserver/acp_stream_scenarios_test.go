package agentserver

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	acp "github.com/ironpark/acp-go"
	"github.com/ironpark/acp-go/acp1"
)

func TestACPStreamScenarios(t *testing.T) {
	for _, scenario := range []struct {
		name       string
		initialize bool
	}{
		{name: "cancel_before_initialize_interrupts_owned_stream"},
		{name: "initialize_then_cancel_joins_protocol_reader", initialize: true},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			local, peer := net.Pipe()
			defer local.Close()
			defer peer.Close()
			// Initialization has no model, tools, credentials, or session effects.
			srv := &Server{cfg: Config{ACPVersion: "1"}}
			t.Cleanup(func() {
				acpStoreMu.Lock()
				delete(serverV1Stores, srv)
				acpStoreMu.Unlock()
			})
			done := make(chan error, 1)
			go func() { done <- srv.ServeACPStream(ctx, local) }()

			if scenario.initialize {
				clientCtx, stopClient := context.WithTimeout(context.Background(), 5*time.Second)
				defer stopClient()
				client := acp1.ConnectAgent(clientCtx, acp.NewStdioTransport(peer, peer), func(*acp1.ClientSideConnection) acp1.Client {
					return &acp1.UnimplementedClient{}
				})
				defer client.Close()
				resp, err := client.Initialize(clientCtx, &acp1.InitializeRequest{ProtocolVersion: 1})
				if err != nil {
					t.Fatalf("initialize: %v", err)
				}
				if resp.ProtocolVersion != 1 || resp.AgentInfo == nil || resp.AgentInfo.Name != "antigravity-acp" {
					t.Fatalf("initialize response = %+v", resp)
				}
			}

			cancel()
			select {
			case err := <-done:
				if !errors.Is(err, context.Canceled) {
					t.Fatalf("serve error = %v, want typed context cancellation", err)
				}
			case <-time.After(5 * time.Second):
				t.Fatal("owned stream did not unblock and join server reader")
			}
			if _, err := peer.Write([]byte("{}\n")); err == nil {
				t.Fatal("server returned without closing its owned stream")
			}
		})
	}
}

func TestACPStreamRejectsNil(t *testing.T) {
	srv := &Server{}
	if err := srv.ServeACPStream(context.Background(), nil); err == nil {
		t.Fatal("nil stream accepted")
	}
}
