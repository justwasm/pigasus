package agentserver

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/microsoft/agent-host-protocol/clients/go/ahp"
	"github.com/microsoft/agent-host-protocol/clients/go/ahptypes"
	"github.com/microsoft/agent-host-protocol/clients/go/ahpws"
)

func TestAHPServer_Terminal_LifecycleAndIO(t *testing.T) {
	srv, addr := startServer(t, Config{Model: "test-model"})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"
	transport, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}

	client, err := ahp.Connect(ctx, transport, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}
	defer client.Shutdown(ctx)

	// 1. Initialize with root subscription
	_, err = client.Initialize(ctx, "terminal-tester", ahptypes.SupportedProtocolVersions(), []string{"ahp-root://"})
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// 2. Create Terminal
	termName := "test-shell"
	cols := int64(80)
	rows := int64(24)
	var createRes map[string]any
	err = client.Request(ctx, "createTerminal", ahptypes.CreateTerminalParams{
		Name: &termName,
		Cols: &cols,
		Rows: &rows,
	}, &createRes)
	if err != nil {
		t.Fatalf("createTerminal failed: %v", err)
	}

	termURI, ok := createRes["channel"].(string)
	if !ok || !strings.HasPrefix(termURI, "ahp-terminal:/") {
		t.Fatalf("unexpected terminal URI: %v", createRes)
	}

	// 3. Subscribe to the terminal channel
	subRes, termSub, err := client.Subscribe(ctx, termURI)
	if err != nil {
		t.Fatalf("Subscribe(termURI) failed: %v", err)
	}
	if subRes == nil || subRes.Snapshot == nil || subRes.Snapshot.State.Terminal == nil {
		t.Fatalf("missing terminal snapshot: %+v", subRes)
	}
	termState := subRes.Snapshot.State.Terminal
	if termState.Title != "test-shell" {
		t.Errorf("expected Title %q, got %q", "test-shell", termState.Title)
	}
	if *termState.Cols != 80 || *termState.Rows != 24 {
		t.Errorf("expected 80x24, got %dx%d", *termState.Cols, *termState.Rows)
	}

	// 4. Send terminal input: echo 'test-ahp-live-echo'
	_, err = client.Dispatch(ctx, termURI, ahptypes.StateAction{
		Value: &ahptypes.TerminalInputAction{
			Type: ahptypes.ActionTypeTerminalInput,
			Data: "echo test-ahp-live-echo\n",
		},
	})
	if err != nil {
		t.Fatalf("Dispatch(terminal/input) failed: %v", err)
	}

	// 5. Watch for terminal/data containing our echo
	sawEcho := false
	timeout := time.After(5 * time.Second)
	for !sawEcho {
		select {
		case evt, ok := <-termSub.Events():
			if !ok {
				t.Fatalf("sub.Events closed unexpectedly")
			}
			if actEvt, ok := evt.(ahp.SubscriptionEventAction); ok {
				if dataAct, ok := actEvt.Envelope.Action.Value.(*ahptypes.TerminalDataAction); ok {
					if strings.Contains(dataAct.Data, "test-ahp-live-echo") {
						sawEcho = true
					}
				}
			}
		case <-timeout:
			t.Fatalf("timed out waiting for echo in terminal/data")
		}
	}

	// 6. Test terminal resize
	newCols := int64(120)
	newRows := int64(40)
	_, err = client.Dispatch(ctx, termURI, ahptypes.StateAction{
		Value: &ahptypes.TerminalResizedAction{
			Type: ahptypes.ActionTypeTerminalResized,
			Cols: newCols,
			Rows: newRows,
		},
	})
	if err != nil {
		t.Fatalf("Dispatch(terminal/resized) failed: %v", err)
	}

	sawResize := false
	timeout = time.After(3 * time.Second)
	for !sawResize {
		select {
		case evt, ok := <-termSub.Events():
			if !ok {
				t.Fatalf("sub.Events closed unexpectedly")
			}
			if actEvt, ok := evt.(ahp.SubscriptionEventAction); ok {
				if resAct, ok := actEvt.Envelope.Action.Value.(*ahptypes.TerminalResizedAction); ok {
					if resAct.Cols == 120 && resAct.Rows == 40 {
						sawResize = true
					}
				}
			}
		case <-timeout:
			t.Fatalf("timed out waiting for terminal/resized broadcast")
		}
	}

	// 7. Verify terminal listing via RPC
	var termList []ahptypes.TerminalInfo
	if err := client.Request(ctx, "terminals", map[string]any{}, &termList); err != nil {
		t.Fatalf("terminals request failed: %v", err)
	}
	if len(termList) == 0 {
		t.Fatalf("expected non-empty terminals list")
	}
	found := false
	for _, ti := range termList {
		if string(ti.Resource) == termURI {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("terminal %s not found in terminals list: %v", termURI, termList)
	}

	// 8. Dispose Terminal
	err = client.Request(ctx, "disposeTerminal", ahptypes.DisposeTerminalParams{
		Channel: ahptypes.URI(termURI),
	}, nil)
	if err != nil {
		t.Fatalf("disposeTerminal failed: %v", err)
	}

	// Verify it was removed from root list
	var termsAfter []ahptypes.TerminalInfo
	if err := client.Request(ctx, "terminals", map[string]any{}, &termsAfter); err != nil {
		t.Fatalf("terminals request after dispose failed: %v", err)
	}
	for _, ti := range termsAfter {
		if string(ti.Resource) == termURI {
			t.Fatalf("terminal %s still present in terminals list after disposal", termURI)
		}
	}
}

func TestAHPServer_Terminal_ExitHandling(t *testing.T) {
	srv, addr := startServer(t, Config{Model: "test-model"})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"
	transport, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}

	client, err := ahp.Connect(ctx, transport, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}
	defer client.Shutdown(ctx)

	_, err = client.Initialize(ctx, "exit-tester", ahptypes.SupportedProtocolVersions(), nil)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Create terminal
	var createRes map[string]any
	err = client.Request(ctx, "createTerminal", ahptypes.CreateTerminalParams{}, &createRes)
	if err != nil {
		t.Fatalf("createTerminal failed: %v", err)
	}
	termURI := createRes["channel"].(string)

	// Subscribe
	_, termSub, err := client.Subscribe(ctx, termURI)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	// Send exit command
	_, err = client.Dispatch(ctx, termURI, ahptypes.StateAction{
		Value: &ahptypes.TerminalInputAction{
			Type: ahptypes.ActionTypeTerminalInput,
			Data: "exit 0\n",
		},
	})
	if err != nil {
		t.Fatalf("Dispatch(exit) failed: %v", err)
	}

	sawExit := false
	timeout := time.After(20 * time.Second)
	for !sawExit {
		select {
		case evt, ok := <-termSub.Events():
			if !ok {
				t.Fatalf("sub.Events closed unexpectedly")
			}
			if actEvt, ok := evt.(ahp.SubscriptionEventAction); ok {
				if exitAct, ok := actEvt.Envelope.Action.Value.(*ahptypes.TerminalExitedAction); ok {
					if exitAct.ExitCode != nil && *exitAct.ExitCode == 0 {
						sawExit = true
					}
				}
			}
		case <-timeout:
			t.Fatalf("timed out waiting for terminal/exited action")
		}
	}
}
