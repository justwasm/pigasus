package agentserver

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/microsoft/agent-host-protocol/clients/go/ahp"
	"github.com/microsoft/agent-host-protocol/clients/go/ahptypes"
	"github.com/microsoft/agent-host-protocol/clients/go/ahpws"
)

func TestAHPServer_Handshake_InitializeAndPing(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model: "test-model",
	})
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

	// Test Initialize with root subscription
	initRes, err := client.Initialize(ctx, "test-client-1", ahptypes.SupportedProtocolVersions(), []string{"ahp-root://"})
	if err != nil {
		t.Fatalf("client.Initialize failed: %v", err)
	}

	if initRes.ProtocolVersion != "1.0.0" {
		t.Errorf("expected protocolVersion 1.0.0, got %s", initRes.ProtocolVersion)
	}
	if initRes.ServerInfo == nil || initRes.ServerInfo.Name != "pigasus-ahp" {
		t.Errorf("expected serverInfo.name pigasus-ahp, got %+v", initRes.ServerInfo)
	}
	if len(initRes.Snapshots) != 1 {
		t.Fatalf("expected 1 snapshot from initialSubscriptions, got %d", len(initRes.Snapshots))
	}
	if initRes.Snapshots[0].Resource != ahptypes.RootResourceURI {
		t.Errorf("expected snapshot resource %s, got %s", ahptypes.RootResourceURI, initRes.Snapshots[0].Resource)
	}

	// Test Ping
	if err := client.Ping(ctx); err != nil {
		t.Fatalf("client.Ping failed: %v", err)
	}
}

func TestAHPServer_RootSubscription_And_SessionLifecycle(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model: "test-model",
	})
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

	if _, err := client.Initialize(ctx, "test-client-lifecycle", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Subscribe to ahp-root://
	snap, sub, err := client.Subscribe(ctx, "ahp-root://")
	if err != nil {
		t.Fatalf("Subscribe(ahp-root://) failed: %v", err)
	}
	if snap == nil || snap.Snapshot == nil || snap.Snapshot.Resource != ahptypes.RootResourceURI {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}

	// Create session via session.create
	var createRes map[string]any
	err = client.Request(ctx, "session.create", ahptypes.CreateSessionParams{
		Channel: "ahp-root://",
	}, &createRes)
	if err != nil {
		t.Fatalf("session.create request failed: %v", err)
	}

	sessURI, ok := createRes["session"].(string)
	if !ok || !strings.HasPrefix(sessURI, "ahp-session:/") {
		t.Fatalf("expected session URI starting with ahp-session:/, got %v", createRes["session"])
	}

	// Root subscriber should observe sessionAdded notification
	select {
	case ev := <-sub.Events():
		added, ok := ev.(ahp.SubscriptionEventSessionAdded)
		if !ok {
			t.Logf("received other root event: %T", ev)
		} else {
			if added.Params.Summary.Resource != sessURI {
				t.Errorf("expected added session URI %s, got %s", sessURI, added.Params.Summary.Resource)
			}
		}
	case <-time.After(2 * time.Second):
		t.Errorf("timed out waiting for sessionAdded notification")
	}

	// List sessions via session.list
	var listRes ahptypes.ListSessionsResult
	if err := client.Request(ctx, "session.list", ahptypes.ListSessionsParams{Channel: "ahp-root://"}, &listRes); err != nil {
		t.Fatalf("session.list request failed: %v", err)
	}
	if len(listRes.Items) != 1 {
		t.Fatalf("expected 1 session in list, got %d", len(listRes.Items))
	}
	if listRes.Items[0].Resource != sessURI {
		t.Errorf("expected session URI %s, got %s", sessURI, listRes.Items[0].Resource)
	}

	// Dispose session via session.dispose
	if err := client.Request(ctx, "session.dispose", ahptypes.DisposeSessionParams{Channel: sessURI}, nil); err != nil {
		t.Fatalf("session.dispose request failed: %v", err)
	}

	// Verify session was removed from list
	var listResAfter ahptypes.ListSessionsResult
	if err := client.Request(ctx, "listSessions", ahptypes.ListSessionsParams{Channel: "ahp-root://"}, &listResAfter); err != nil {
		t.Fatalf("listSessions request failed: %v", err)
	}
	if len(listResAfter.Items) != 0 {
		t.Errorf("expected 0 sessions after dispose, got %d", len(listResAfter.Items))
	}
}

func TestAHPServer_SessionAndChat_TurnExecution(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model: "test-model",
	})
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

	if _, err := client.Initialize(ctx, "test-client-turn", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Create session
	var createRes map[string]any
	if err := client.Request(ctx, "session.create", ahptypes.CreateSessionParams{Channel: "ahp-root://"}, &createRes); err != nil {
		t.Fatalf("session.create failed: %v", err)
	}

	chatURI, ok := createRes["chat"].(string)
	if !ok || !strings.HasPrefix(chatURI, "ahp-chat:/") {
		t.Fatalf("expected chat URI starting with ahp-chat:/, got %v", createRes["chat"])
	}

	// Subscribe to chat channel
	snap, sub, err := client.Subscribe(ctx, chatURI)
	if err != nil {
		t.Fatalf("Subscribe(chatURI) failed: %v", err)
	}
	if snap == nil || snap.Snapshot == nil || snap.Snapshot.Resource != chatURI {
		t.Fatalf("unexpected chat snapshot: %+v", snap)
	}

	// Dispatch turn
	turnID := "turn-123"
	turnAction := ahptypes.StateAction{
		Value: &ahptypes.ChatTurnStartedAction{
			Type:      ahptypes.ActionTypeChatTurnStarted,
			TurnId:    turnID,
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Message: ahptypes.Message{
				Text: "Hello from AHP client!",
				Origin: ahptypes.MessageOrigin{
					Kind: ahptypes.MessageKindUser,
				},
			},
		},
	}

	if _, err := client.Dispatch(ctx, chatURI, turnAction); err != nil {
		t.Fatalf("client.Dispatch failed: %v", err)
	}

	// Receive streaming actions
	var textReceived string
	var sawResponsePart bool
	var sawTurnComplete bool

	timeout := time.After(5 * time.Second)
	for !sawTurnComplete {
		select {
		case ev := <-sub.Events():
			act, ok := ev.(ahp.SubscriptionEventAction)
			if !ok {
				continue
			}
			switch a := act.Envelope.Action.Value.(type) {
			case *ahptypes.ChatResponsePartAction:
				sawResponsePart = true
			case *ahptypes.ChatDeltaAction:
				textReceived += a.Content
			case *ahptypes.ChatTurnCompleteAction:
				sawTurnComplete = true
			}
		case <-timeout:
			t.Fatalf("timed out waiting for turn completion. textReceived=%q sawResponsePart=%v", textReceived, sawResponsePart)
		}
	}

	if !sawResponsePart {
		t.Errorf("expected ChatResponsePartAction before streaming deltas")
	}
	if textReceived == "" {
		t.Errorf("expected non-empty streaming text from mock backend")
	}
	t.Logf("turn completed successfully. Assistant response: %q", textReceived)
}

func TestAHPServer_MultiClient_Broadcast(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model: "test-model",
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"

	// Connect Client 1
	tr1, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect (client 1) failed: %v", err)
	}
	c1, err := ahp.Connect(ctx, tr1, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect (client 1) failed: %v", err)
	}
	defer c1.Shutdown(ctx)
	if _, err := c1.Initialize(ctx, "client-1", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("c1 Initialize failed: %v", err)
	}

	// Connect Client 2
	tr2, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect (client 2) failed: %v", err)
	}
	c2, err := ahp.Connect(ctx, tr2, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect (client 2) failed: %v", err)
	}
	defer c2.Shutdown(ctx)
	if _, err := c2.Initialize(ctx, "client-2", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("c2 Initialize failed: %v", err)
	}

	// Client 1 creates session
	var createRes map[string]any
	if err := c1.Request(ctx, "session.create", ahptypes.CreateSessionParams{Channel: "ahp-root://"}, &createRes); err != nil {
		t.Fatalf("session.create failed: %v", err)
	}
	chatURI := createRes["chat"].(string)

	// Both subscribe to chat
	_, sub1, err := c1.Subscribe(ctx, chatURI)
	if err != nil {
		t.Fatalf("c1 subscribe failed: %v", err)
	}
	_, sub2, err := c2.Subscribe(ctx, chatURI)
	if err != nil {
		t.Fatalf("c2 subscribe failed: %v", err)
	}

	// Client 1 dispatches turn
	turnAction := ahptypes.StateAction{
		Value: &ahptypes.ChatTurnStartedAction{
			Type:      ahptypes.ActionTypeChatTurnStarted,
			TurnId:    "broadcast-turn-1",
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Message: ahptypes.Message{
				Text: "Broadcast test message",
				Origin: ahptypes.MessageOrigin{
					Kind: ahptypes.MessageKindUser,
				},
			},
		},
	}
	if _, err := c1.Dispatch(ctx, chatURI, turnAction); err != nil {
		t.Fatalf("c1 Dispatch failed: %v", err)
	}

	// Both c1 and c2 must receive ChatTurnCompleteAction
	waitComplete := func(sub *ahp.Subscription, name string) {
		timeout := time.After(5 * time.Second)
		for {
			select {
			case ev := <-sub.Events():
				if act, ok := ev.(ahp.SubscriptionEventAction); ok {
					if _, isComplete := act.Envelope.Action.Value.(*ahptypes.ChatTurnCompleteAction); isComplete {
						return
					}
				}
			case <-timeout:
				t.Fatalf("%s timed out waiting for broadcast complete", name)
			}
		}
	}

	waitComplete(sub1, "client 1")
	waitComplete(sub2, "client 2")
}

func TestAHPServer_Authentication(t *testing.T) {
	token := "valid-secret-token"
	srv, addr := startServer(t, Config{
		Model: "test-model",
		Token: token,
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	// 1. Connect without token
	wsURL := "ws://" + addr + "/ahp"
	tr, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}
	client, err := ahp.Connect(ctx, tr, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}
	defer client.Shutdown(ctx)

	if _, err := client.Initialize(ctx, "auth-client", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Unauthenticated request must fail
	var listRes ahptypes.ListSessionsResult
	err = client.Request(ctx, "session.list", ahptypes.ListSessionsParams{Channel: "ahp-root://"}, &listRes)
	if err == nil {
		t.Fatalf("expected error for unauthenticated session.list, got nil")
	}

	// Authenticate with wrong token
	err = client.Request(ctx, "authenticate", ahptypes.AuthenticateParams{
		Channel: "ahp-root://",
		Token:   "wrong-token",
	}, nil)
	if err == nil {
		t.Fatalf("expected error for wrong token authenticate, got nil")
	}

	// Authenticate with correct token
	err = client.Request(ctx, "authenticate", ahptypes.AuthenticateParams{
		Channel: "ahp-root://",
		Token:   token,
	}, nil)
	if err != nil {
		t.Fatalf("expected successful authenticate, got %v", err)
	}

	// Now session.list must succeed
	if err := client.Request(ctx, "session.list", ahptypes.ListSessionsParams{Channel: "ahp-root://"}, &listRes); err != nil {
		t.Fatalf("session.list after authenticate failed: %v", err)
	}

	// 2. Connect with token in URL query parameter
	wsAuthURL := wsURL + "?token=" + token
	trAuth, err := ahpws.Connect(ctx, wsAuthURL)
	if err != nil {
		t.Fatalf("ahpws.Connect with query token failed: %v", err)
	}
	clientAuth, err := ahp.Connect(ctx, trAuth, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect with query token failed: %v", err)
	}
	defer clientAuth.Shutdown(ctx)

	if _, err := clientAuth.Initialize(ctx, "auth-query-client", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// Pre-authenticated request should succeed immediately
	if err := clientAuth.Request(ctx, "session.list", ahptypes.ListSessionsParams{Channel: "ahp-root://"}, &listRes); err != nil {
		t.Fatalf("session.list with pre-authenticated query token failed: %v", err)
	}
}

func TestAHPServer_Reconnection(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model: "test-model",
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"
	tr1, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}
	c1, err := ahp.Connect(ctx, tr1, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}

	if _, err := c1.Initialize(ctx, "recon-client", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	var createRes map[string]any
	if err := c1.Request(ctx, "session.create", ahptypes.CreateSessionParams{Channel: "ahp-root://"}, &createRes); err != nil {
		t.Fatalf("session.create failed: %v", err)
	}
	sessURI := createRes["session"].(string)

	// Close client 1
	c1.Shutdown(ctx)

	// Connect client 2 (reconnection)
	tr2, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect (reconnect) failed: %v", err)
	}
	c2, err := ahp.Connect(ctx, tr2, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect (reconnect) failed: %v", err)
	}
	defer c2.Shutdown(ctx)

	// Issue Reconnect request
	recRes, err := c2.Reconnect(ctx, "recon-client", 0, []string{sessURI})
	if err != nil {
		t.Fatalf("c2.Reconnect failed: %v", err)
	}
	if recRes == nil || recRes.Value == nil {
		t.Fatalf("expected non-nil reconnect result, got %+v", recRes)
	}

	switch v := recRes.Value.(type) {
	case *ahptypes.ReconnectReplayResult:
		t.Logf("reconnected via replay with %d actions", len(v.Actions))
	case *ahptypes.ReconnectSnapshotResult:
		t.Logf("reconnected via snapshot with %d snapshots", len(v.Snapshots))
	default:
		t.Errorf("unexpected ReconnectResult variant: %T", recRes.Value)
	}
}

func TestAHPServer_MultipleTurnsAndHistory(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model: "turn-model",
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"
	tr, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}
	client, err := ahp.Connect(ctx, tr, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}
	defer client.Shutdown(ctx)

	if _, err := client.Initialize(ctx, "multi-turn-client", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	var createRes map[string]any
	if err := client.Request(ctx, "session.create", ahptypes.CreateSessionParams{Channel: "ahp-root://"}, &createRes); err != nil {
		t.Fatalf("session.create failed: %v", err)
	}
	chatURI := createRes["chat"].(string)

	_, sub, err := client.Subscribe(ctx, chatURI)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	runTurnAndWait := func(turnID string, prompt string) string {
		turnAction := ahptypes.StateAction{
			Value: &ahptypes.ChatTurnStartedAction{
				Type:      ahptypes.ActionTypeChatTurnStarted,
				TurnId:    turnID,
				StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
				Message: ahptypes.Message{
					Text: prompt,
					Origin: ahptypes.MessageOrigin{
						Kind: ahptypes.MessageKindUser,
					},
				},
			},
		}
		if _, err := client.Dispatch(ctx, chatURI, turnAction); err != nil {
			t.Fatalf("Dispatch failed: %v", err)
		}

		var text string
		timeout := time.After(5 * time.Second)
		for {
			select {
			case ev := <-sub.Events():
				if act, ok := ev.(ahp.SubscriptionEventAction); ok {
					switch a := act.Envelope.Action.Value.(type) {
					case *ahptypes.ChatDeltaAction:
						text += a.Content
					case *ahptypes.ChatTurnCompleteAction:
						if a.TurnId == turnID {
							return text
						}
					}
				}
			case <-timeout:
				t.Fatalf("timed out waiting for turn %s to complete", turnID)
			}
		}
	}

	ans1 := runTurnAndWait("t1", "First Question")
	if !strings.Contains(ans1, "First Question") {
		t.Errorf("expected ans1 to contain 'First Question', got %q", ans1)
	}

	ans2 := runTurnAndWait("t2", "Second Question")
	if !strings.Contains(ans2, "Second Question") {
		t.Errorf("expected ans2 to contain 'Second Question', got %q", ans2)
	}
}

func TestAHPServer_SessionSubscriber_ReceivesChatActions(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model: "test-model",
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"
	tr, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}
	client, err := ahp.Connect(ctx, tr, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}
	defer client.Shutdown(ctx)

	if _, err := client.Initialize(ctx, "session-sub-client", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	var createRes map[string]any
	if err := client.Request(ctx, "session.create", ahptypes.CreateSessionParams{Channel: "ahp-root://"}, &createRes); err != nil {
		t.Fatalf("session.create failed: %v", err)
	}
	sessURI := createRes["session"].(string)
	chatURI := createRes["chat"].(string)

	// Subscribe to session URI (not chat URI)
	_, sessSub, err := client.Subscribe(ctx, sessURI)
	if err != nil {
		t.Fatalf("Subscribe(sessURI) failed: %v", err)
	}

	// Dispatch turn on chat URI
	turnAction := ahptypes.StateAction{
		Value: &ahptypes.ChatTurnStartedAction{
			Type:      ahptypes.ActionTypeChatTurnStarted,
			TurnId:    "turn-sess-sub",
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Message: ahptypes.Message{
				Text: "Testing session-level subscription",
				Origin: ahptypes.MessageOrigin{
					Kind: ahptypes.MessageKindUser,
				},
			},
		},
	}
	if _, err := client.Dispatch(ctx, chatURI, turnAction); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	// Session subscriber should receive SessionChatUpdatedAction
	var sawChatUpdated bool
	timeout := time.After(5 * time.Second)
	for !sawChatUpdated {
		select {
		case ev := <-sessSub.Events():
			if act, ok := ev.(ahp.SubscriptionEventAction); ok {
				if upd, isUpdated := act.Envelope.Action.Value.(*ahptypes.SessionChatUpdatedAction); isUpdated {
					if upd.Chat == chatURI {
						sawChatUpdated = true
					}
				}
			}
		case <-timeout:
			t.Fatalf("session subscriber did not receive SessionChatUpdatedAction event")
		}
	}
}

func TestAHPServer_CustomWorkspaceAndModel(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model: "default-model",
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"
	tr, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}
	client, err := ahp.Connect(ctx, tr, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}
	defer client.Shutdown(ctx)

	if _, err := client.Initialize(ctx, "custom-cfg-client", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	customDir := t.TempDir()
	var createRes map[string]any
	err = client.Request(ctx, "session.create", ahptypes.CreateSessionParams{
		Channel:            "ahp-root://",
		WorkingDirectories: []ahptypes.URI{"file://" + customDir},
		Config: map[string]ahptypes.AnyValue{
			"model": ahptypes.AnyValue(`"custom-model-42"`),
		},
	}, &createRes)
	if err != nil {
		t.Fatalf("session.create failed: %v", err)
	}

	sessURI := createRes["session"].(string)
	chatURI := createRes["chat"].(string)

	snap, _, err := client.Subscribe(ctx, sessURI)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	if snap == nil || snap.Snapshot == nil || snap.Snapshot.State.Session == nil {
		t.Fatalf("unexpected snapshot: %+v", snap)
	}

	sessState := snap.Snapshot.State.Session
	if len(sessState.WorkingDirectories) == 0 || !strings.Contains(sessState.WorkingDirectories[0], customDir) {
		t.Errorf("expected working directory containing %s, got %v", customDir, sessState.WorkingDirectories)
	}

	// Verify turn runs under custom model
	_, chatSub, err := client.Subscribe(ctx, chatURI)
	if err != nil {
		t.Fatalf("chat subscribe failed: %v", err)
	}

	turnAction := ahptypes.StateAction{
		Value: &ahptypes.ChatTurnStartedAction{
			Type:      ahptypes.ActionTypeChatTurnStarted,
			TurnId:    "turn-custom-model",
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Message: ahptypes.Message{
				Text: "Hello custom model",
				Origin: ahptypes.MessageOrigin{
					Kind: ahptypes.MessageKindUser,
				},
			},
		},
	}
	if _, err := client.Dispatch(ctx, chatURI, turnAction); err != nil {
		t.Fatalf("Dispatch failed: %v", err)
	}

	var respText string
	timeout := time.After(5 * time.Second)
	for {
		select {
		case ev := <-chatSub.Events():
			if act, ok := ev.(ahp.SubscriptionEventAction); ok {
				switch a := act.Envelope.Action.Value.(type) {
				case *ahptypes.ChatDeltaAction:
					respText += a.Content
				case *ahptypes.ChatTurnCompleteAction:
					goto done
				}
			}
		case <-timeout:
			t.Fatalf("timed out waiting for custom model turn")
		}
	}
done:
	if !strings.Contains(respText, "model=custom-model-42") {
		t.Errorf("expected response from model=custom-model-42, got %q", respText)
	}
}

func TestAHPServer_ErrorCases(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model: "test-model",
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"
	tr, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}
	client, err := ahp.Connect(ctx, tr, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}
	defer client.Shutdown(ctx)

	if _, err := client.Initialize(ctx, "error-test-client", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	// 1. Unknown method
	err = client.Request(ctx, "nonexistent.method", map[string]any{}, nil)
	if err == nil {
		t.Errorf("expected error for nonexistent method, got nil")
	}

	// 2. Dispose nonexistent session
	err = client.Request(ctx, "session.dispose", ahptypes.DisposeSessionParams{
		Channel: "ahp-session:/does-not-exist",
	}, nil)
	if err == nil {
		t.Errorf("expected error for disposing nonexistent session, got nil")
	}

	// 3. Duplicate session creation
	var createRes map[string]any
	fixedChannel := "ahp-session:/fixed-test-uuid-12345"
	if err := client.Request(ctx, "session.create", ahptypes.CreateSessionParams{Channel: fixedChannel}, &createRes); err != nil {
		t.Fatalf("initial session.create failed: %v", err)
	}
	// Try creating with identical fixedChannel
	err = client.Request(ctx, "session.create", ahptypes.CreateSessionParams{Channel: fixedChannel}, nil)
	if err == nil {
		t.Errorf("expected error for duplicate session.create, got nil")
	}
}

type testCustomMockBackend struct {
	streamFn func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error)
}

func (m *testCustomMockBackend) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	return m.streamFn(ctx, req)
}

func TestAHPServer_ToolCallStreaming(t *testing.T) {
	mock := &testCustomMockBackend{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			out := make(chan backend.StreamChunk, 4)
			go func() {
				defer close(out)
				hasToolOutput := false
				for i, m := range req.Messages {
					t.Logf("mock msg[%d]: role=%s, toolCalls=%d, content=%q, toolCallId=%s", i, m.Role, len(m.ToolCalls), m.Content, m.ToolCallID)
					if m.Role == "tool" {
						hasToolOutput = true
						break
					}
				}
				if !hasToolOutput {
					args, _ := json.Marshal(map[string]any{
						"CommandLine":       "echo 'ahp-tool-ok'",
						"toolSummary":       "Echo test",
						"WaitMsBeforeAsync": 1000,
					})
					out <- backend.StreamChunk{
						ToolCalls: []backend.ToolCall{
							{
								ID:        "call_1",
								Name:      "run_command",
								Arguments: args,
							},
						},
						Done: true,
					}
				} else {
					out <- backend.StreamChunk{
						Text: "tool finished successfully",
						Done: true,
					}
				}
			}()
			return out, nil
		},
	}

	srv, addr := startServer(t, Config{
		Model:           "tool-test-model",
		SkipPermissions: true,
		BackendFor: func(string) backend.Client {
			return mock
		},
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"
	tr, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}
	client, err := ahp.Connect(ctx, tr, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}
	defer client.Shutdown(ctx)

	if _, err := client.Initialize(ctx, "tool-client", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	var createRes map[string]any
	if err := client.Request(ctx, "session.create", ahptypes.CreateSessionParams{Channel: "ahp-root://"}, &createRes); err != nil {
		t.Fatalf("session.create failed: %v", err)
	}
	chatURI := createRes["chat"].(string)

	snap, chatSub, err := client.Subscribe(ctx, chatURI)
	if err != nil {
		t.Fatalf("Subscribe(chatURI) failed: %v", err)
	}
	var chatState ahptypes.ChatState
	if snap.Snapshot != nil && snap.Snapshot.State.Chat != nil {
		chatState = *snap.Snapshot.State.Chat
	}

	turnAction := ahptypes.StateAction{
		Value: &ahptypes.ChatTurnStartedAction{
			Type:      ahptypes.ActionTypeChatTurnStarted,
			TurnId:    "turn-tool-test",
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Message: ahptypes.Message{
				Text: "Run command please",
				Origin: ahptypes.MessageOrigin{
					Kind: ahptypes.MessageKindUser,
				},
			},
		},
	}

	if _, err := client.Dispatch(ctx, chatURI, turnAction); err != nil {
		t.Fatalf("Dispatch turn failed: %v", err)
	}

	var (
		startToolID    string
		readyToolID    string
		completeToolID string
		toolSuccess    bool
		turnDone       bool
	)

	timeout := time.After(5 * time.Second)
	for !turnDone {
		select {
		case ev := <-chatSub.Events():
			if act, ok := ev.(ahp.SubscriptionEventAction); ok {
				t.Logf("received action: %T: %+v", act.Envelope.Action.Value, act.Envelope.Action.Value)
				ahp.ApplyActionToChat(&chatState, act.Envelope.Action)

				switch a := act.Envelope.Action.Value.(type) {
				case *ahptypes.ChatToolCallStartAction:
					startToolID = a.ToolCallId
					if a.ToolName != "run_command" {
						t.Errorf("expected toolName run_command, got %s", a.ToolName)
					}
				case *ahptypes.ChatToolCallReadyAction:
					readyToolID = a.ToolCallId
					if a.Confirmed == nil || *a.Confirmed != ahptypes.ToolCallConfirmationReasonNotNeeded {
						t.Errorf("expected Confirmed: not-needed, got %+v", a.Confirmed)
					}
				case *ahptypes.ChatToolCallCompleteAction:
					completeToolID = a.ToolCallId
					toolSuccess = a.Result.Success
				case *ahptypes.ChatTurnCompleteAction:
					turnDone = true
				}
			}
		case <-timeout:
			t.Fatalf("timed out waiting for tool turn completion")
		}
	}

	if startToolID == "" {
		t.Fatalf("expected ChatToolCallStartAction, got empty")
	}
	if readyToolID != startToolID {
		t.Errorf("expected ChatToolCallReadyAction ToolCallId %q to match start %q", readyToolID, startToolID)
	}
	if completeToolID != startToolID {
		t.Errorf("expected ChatToolCallCompleteAction ToolCallId %q to match start %q", completeToolID, startToolID)
	}
	if !toolSuccess {
		t.Errorf("expected tool execution to succeed, got success=false")
	}

	// Verify that Microsoft's ApplyActionToChat reduced the turn correctly
	if len(chatState.Turns) == 0 {
		t.Fatalf("expected completed turn in ChatState.Turns, got 0")
	}
	completedTurn := chatState.Turns[0]
	var foundCompletedToolCall bool
	for _, p := range completedTurn.ResponseParts {
		if tcPart, ok := p.Value.(*ahptypes.ToolCallResponsePart); ok {
			if completedState, isCompleted := tcPart.ToolCall.Value.(*ahptypes.ToolCallCompletedState); isCompleted {
				foundCompletedToolCall = true
				if completedState.ToolCallId != startToolID {
					t.Errorf("expected completed ToolCallId %q, got %q", startToolID, completedState.ToolCallId)
				}
				if !completedState.Success {
					t.Errorf("expected completedState.Success to be true")
				}
			} else if cancelledState, isCancelled := tcPart.ToolCall.Value.(*ahptypes.ToolCallCancelledState); isCancelled {
				t.Fatalf("tool call was wrongly reduced to Cancelled/Skipped state: reason=%v", cancelledState.Reason)
			}
		}
	}
	if !foundCompletedToolCall {
		t.Errorf("did not find ToolCallCompletedState in reduced chat turn response parts")
	}
}

func TestAHPServer_ReasoningParts(t *testing.T) {
	mock := &testCustomMockBackend{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			out := make(chan backend.StreamChunk, 4)
			go func() {
				defer close(out)
				out <- backend.StreamChunk{
					Text: "<think>Let me consider the problem step by step.</think>The answer is 42.",
					Done: true,
				}
			}()
			return out, nil
		},
	}

	srv, addr := startServer(t, Config{
		Model: "thought-test-model",
		BackendFor: func(string) backend.Client {
			return mock
		},
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"
	tr, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}
	client, err := ahp.Connect(ctx, tr, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}
	defer client.Shutdown(ctx)

	if _, err := client.Initialize(ctx, "thought-client", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	var createRes map[string]any
	if err := client.Request(ctx, "session.create", ahptypes.CreateSessionParams{Channel: "ahp-root://"}, &createRes); err != nil {
		t.Fatalf("session.create failed: %v", err)
	}
	chatURI := createRes["chat"].(string)

	snap, chatSub, err := client.Subscribe(ctx, chatURI)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}
	var chatState ahptypes.ChatState
	if snap.Snapshot != nil && snap.Snapshot.State.Chat != nil {
		chatState = *snap.Snapshot.State.Chat
	}

	turnAction := ahptypes.StateAction{
		Value: &ahptypes.ChatTurnStartedAction{
			Type:      ahptypes.ActionTypeChatTurnStarted,
			TurnId:    "turn-reasoning-test",
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Message: ahptypes.Message{
				Text: "Think and answer",
				Origin: ahptypes.MessageOrigin{
					Kind: ahptypes.MessageKindUser,
				},
			},
		},
	}

	if _, err := client.Dispatch(ctx, chatURI, turnAction); err != nil {
		t.Fatalf("Dispatch turn failed: %v", err)
	}

	var (
		sawReasoningAction bool
		reasoningText      string
		deltaText          string
		turnDone           bool
	)

	timeout := time.After(5 * time.Second)
	for !turnDone {
		select {
		case ev := <-chatSub.Events():
			if act, ok := ev.(ahp.SubscriptionEventAction); ok {
				ahp.ApplyActionToChat(&chatState, act.Envelope.Action)

				switch a := act.Envelope.Action.Value.(type) {
				case *ahptypes.ChatReasoningAction:
					sawReasoningAction = true
					reasoningText += a.Content
				case *ahptypes.ChatDeltaAction:
					deltaText += a.Content
				case *ahptypes.ChatTurnCompleteAction:
					turnDone = true
				}
			}
		case <-timeout:
			t.Fatalf("timed out waiting for reasoning turn completion")
		}
	}

	if !sawReasoningAction {
		t.Errorf("expected to receive ChatReasoningAction, but got none")
	}
	if !strings.Contains(reasoningText, "consider the problem") {
		t.Errorf("expected reasoning text to contain 'consider the problem', got %q", reasoningText)
	}
	if !strings.Contains(deltaText, "42") {
		t.Errorf("expected delta text to contain '42', got %q", deltaText)
	}

	if len(chatState.Turns) == 0 {
		t.Fatalf("expected 1 turn in reduced state, got 0")
	}
	turn := chatState.Turns[0]
	var (
		foundReasoningPart bool
		foundMarkdownPart  bool
	)
	for _, p := range turn.ResponseParts {
		if r, ok := p.Value.(*ahptypes.ReasoningResponsePart); ok {
			foundReasoningPart = true
			if !strings.Contains(r.Content, "consider the problem") {
				t.Errorf("reduced ReasoningResponsePart content mismatch: %q", r.Content)
			}
		}
		if m, ok := p.Value.(*ahptypes.MarkdownResponsePart); ok {
			foundMarkdownPart = true
			if !strings.Contains(m.Content, "42") {
				t.Errorf("reduced MarkdownResponsePart content mismatch: %q", m.Content)
			}
		}
	}
	if !foundReasoningPart {
		t.Errorf("reduced ChatState did not contain ReasoningResponsePart")
	}
	if !foundMarkdownPart {
		t.Errorf("reduced ChatState did not contain MarkdownResponsePart")
	}
}

func TestAHPServer_TurnCancellation(t *testing.T) {
	holdTurn := make(chan struct{})
	mock := &testCustomMockBackend{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			out := make(chan backend.StreamChunk, 4)
			go func() {
				defer close(out)
				out <- backend.StreamChunk{Text: "Beginning response..."}
				select {
				case <-holdTurn:
					out <- backend.StreamChunk{Text: " Resumed.", Done: true}
				case <-ctx.Done():
					out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
				}
			}()
			return out, nil
		},
	}
	defer close(holdTurn)

	srv, addr := startServer(t, Config{
		Model: "cancel-test-model",
		BackendFor: func(string) backend.Client {
			return mock
		},
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"
	tr, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}
	client, err := ahp.Connect(ctx, tr, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}
	defer client.Shutdown(ctx)

	if _, err := client.Initialize(ctx, "cancel-client", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	var createRes map[string]any
	if err := client.Request(ctx, "session.create", ahptypes.CreateSessionParams{Channel: "ahp-root://"}, &createRes); err != nil {
		t.Fatalf("session.create failed: %v", err)
	}
	chatURI := createRes["chat"].(string)

	_, chatSub, err := client.Subscribe(ctx, chatURI)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	turnID := "turn-to-cancel-123"
	turnAction := ahptypes.StateAction{
		Value: &ahptypes.ChatTurnStartedAction{
			Type:      ahptypes.ActionTypeChatTurnStarted,
			TurnId:    turnID,
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Message: ahptypes.Message{
				Text: "Start turn to be cancelled",
				Origin: ahptypes.MessageOrigin{
					Kind: ahptypes.MessageKindUser,
				},
			},
		},
	}

	if _, err := client.Dispatch(ctx, chatURI, turnAction); err != nil {
		t.Fatalf("Dispatch turn failed: %v", err)
	}

	// Wait for first delta
	sawDelta := false
	waitTimeout := time.After(3 * time.Second)
	for !sawDelta {
		select {
		case ev := <-chatSub.Events():
			if act, ok := ev.(ahp.SubscriptionEventAction); ok {
				if _, ok := act.Envelope.Action.Value.(*ahptypes.ChatDeltaAction); ok {
					sawDelta = true
				}
			}
		case <-waitTimeout:
			t.Fatalf("timed out waiting for first delta")
		}
	}

	// Dispatch cancellation
	cancelAction := ahptypes.StateAction{
		Value: &ahptypes.ChatTurnCancelledAction{
			Type:   ahptypes.ActionTypeChatTurnCancelled,
			TurnId: turnID,
		},
	}
	if _, err := client.Dispatch(ctx, chatURI, cancelAction); err != nil {
		t.Fatalf("Dispatch cancel failed: %v", err)
	}

	// Verify server does NOT emit ChatTurnCompleteAction
	sawComplete := false
	drainTimeout := time.After(300 * time.Millisecond)
	for {
		select {
		case ev := <-chatSub.Events():
			if act, ok := ev.(ahp.SubscriptionEventAction); ok {
				if _, ok := act.Envelope.Action.Value.(*ahptypes.ChatTurnCompleteAction); ok {
					sawComplete = true
				}
			}
		case <-drainTimeout:
			goto verify
		}
	}

verify:
	if sawComplete {
		t.Errorf("server incorrectly emitted ChatTurnCompleteAction after turn was cancelled")
	}
}

func TestAHPServer_TurnError(t *testing.T) {
	mock := &testCustomMockBackend{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			out := make(chan backend.StreamChunk, 2)
			go func() {
				defer close(out)
				out <- backend.StreamChunk{
					Error: errors.New("upstream model quota exhausted"),
					Done:  true,
				}
			}()
			return out, nil
		},
	}

	srv, addr := startServer(t, Config{
		Model: "error-model",
		BackendFor: func(string) backend.Client {
			return mock
		},
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"
	tr, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}
	client, err := ahp.Connect(ctx, tr, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}
	defer client.Shutdown(ctx)

	if _, err := client.Initialize(ctx, "err-client", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	var createRes map[string]any
	if err := client.Request(ctx, "session.create", ahptypes.CreateSessionParams{Channel: "ahp-root://"}, &createRes); err != nil {
		t.Fatalf("session.create failed: %v", err)
	}
	chatURI := createRes["chat"].(string)

	_, chatSub, err := client.Subscribe(ctx, chatURI)
	if err != nil {
		t.Fatalf("Subscribe failed: %v", err)
	}

	turnAction := ahptypes.StateAction{
		Value: &ahptypes.ChatTurnStartedAction{
			Type:      ahptypes.ActionTypeChatTurnStarted,
			TurnId:    "turn-err-test",
			StartedAt: time.Now().UTC().Format(time.RFC3339Nano),
			Message: ahptypes.Message{
				Text: "Trigger error",
				Origin: ahptypes.MessageOrigin{
					Kind: ahptypes.MessageKindUser,
				},
			},
		},
	}

	if _, err := client.Dispatch(ctx, chatURI, turnAction); err != nil {
		t.Fatalf("Dispatch turn failed: %v", err)
	}

	var (
		sawErrorAction    bool
		sawCompleteAction bool
	)
	timeout := time.After(3 * time.Second)
	for !sawErrorAction {
		select {
		case ev := <-chatSub.Events():
			if act, ok := ev.(ahp.SubscriptionEventAction); ok {
				switch act.Envelope.Action.Value.(type) {
				case *ahptypes.ChatErrorAction:
					sawErrorAction = true
				case *ahptypes.ChatTurnCompleteAction:
					sawCompleteAction = true
				}
			}
		case <-timeout:
			t.Fatalf("timed out waiting for ChatErrorAction")
		}
	}

	if !sawErrorAction {
		t.Errorf("expected ChatErrorAction, but did not receive one")
	}
	if sawCompleteAction {
		t.Errorf("server incorrectly emitted ChatTurnCompleteAction on failed turn")
	}
}

func TestAHPServer_MultipleChats_CreateAndDispose(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model: "multi-chat-model",
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"
	tr, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}
	client, err := ahp.Connect(ctx, tr, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}
	defer client.Shutdown(ctx)

	if _, err := client.Initialize(ctx, "multi-chat-client", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	var createRes map[string]any
	if err := client.Request(ctx, "session.create", ahptypes.CreateSessionParams{Channel: "ahp-root://"}, &createRes); err != nil {
		t.Fatalf("session.create failed: %v", err)
	}
	sessURI := createRes["session"].(string)

	_, sessSub, err := client.Subscribe(ctx, sessURI)
	if err != nil {
		t.Fatalf("Subscribe(sessURI) failed: %v", err)
	}

	// 1. Create a second chat via createChat
	var chat2Res map[string]any
	err = client.Request(ctx, "createChat", ahptypes.CreateChatParams{
		Channel: sessURI,
		Chat:    "ahp-chat:/custom-chat-2",
	}, &chat2Res)
	if err != nil {
		t.Fatalf("createChat request failed: %v", err)
	}
	chat2URI := chat2Res["chat"].(string)
	if chat2URI != "ahp-chat:/custom-chat-2" {
		t.Errorf("expected chat2URI ahp-chat:/custom-chat-2, got %s", chat2URI)
	}

	// Session subscriber should receive SessionChatAddedAction
	var sawChatAdded bool
	waitTimeout := time.After(3 * time.Second)
	for !sawChatAdded {
		select {
		case ev := <-sessSub.Events():
			if act, ok := ev.(ahp.SubscriptionEventAction); ok {
				if added, ok := act.Envelope.Action.Value.(*ahptypes.SessionChatAddedAction); ok {
					if added.Summary.Resource == chat2URI {
						sawChatAdded = true
					}
				}
			}
		case <-waitTimeout:
			t.Fatalf("timed out waiting for SessionChatAddedAction on sessSub")
		}
	}

	// 2. Execute a turn on chat 2
	var promptRes map[string]any
	err = client.Request(ctx, "prompt", map[string]any{
		"channel": chat2URI,
		"prompt":  "Hello on second chat",
	}, &promptRes)
	if err != nil {
		t.Fatalf("prompt on chat2 failed: %v", err)
	}

	// 3. Dispose chat 2
	err = client.Request(ctx, "disposeChat", ahptypes.DisposeChatParams{
		Channel: chat2URI,
	}, nil)
	if err != nil {
		t.Fatalf("disposeChat failed: %v", err)
	}

	// Session subscriber should receive SessionChatRemovedAction
	var sawChatRemoved bool
	removeTimeout := time.After(3 * time.Second)
	for !sawChatRemoved {
		select {
		case ev := <-sessSub.Events():
			if act, ok := ev.(ahp.SubscriptionEventAction); ok {
				if removed, ok := act.Envelope.Action.Value.(*ahptypes.SessionChatRemovedAction); ok {
					if removed.Chat == chat2URI {
						sawChatRemoved = true
					}
				}
			}
		case <-removeTimeout:
			t.Fatalf("timed out waiting for SessionChatRemovedAction on sessSub")
		}
	}
}

func TestAHPServer_Reconnection_ReplayBufferGap(t *testing.T) {
	srv, addr := startServer(t, Config{
		Token: "secret-token-123",
		Model: "recon-test-model",
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp?token=secret-token-123"
	tr1, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}
	c1, err := ahp.Connect(ctx, tr1, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}

	initRes, err := c1.Initialize(ctx, "recon-client-gap", ahptypes.SupportedProtocolVersions(), nil)
	if err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	var createRes map[string]any
	if err := c1.Request(ctx, "session.create", ahptypes.CreateSessionParams{Channel: "ahp-root://"}, &createRes); err != nil {
		t.Fatalf("session.create failed: %v", err)
	}
	sessURI := createRes["session"].(string)

	c1.Shutdown(ctx)

	// Connect unauthenticated to verify auth check on reconnect
	unauthURL := "ws://" + addr + "/ahp"
	trUnauth, err := ahpws.Connect(ctx, unauthURL)
	if err != nil {
		t.Fatalf("unauth connect failed: %v", err)
	}
	cUnauth, err := ahp.Connect(ctx, trUnauth, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("cUnauth Connect failed: %v", err)
	}
	defer cUnauth.Shutdown(ctx)

	_, err = cUnauth.Reconnect(ctx, "recon-client-gap", initRes.ServerSeq, []string{sessURI})
	if err == nil {
		t.Errorf("expected reconnect without auth token to fail, got success")
	}

	// Connect authenticated client
	tr2, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("tr2 connect failed: %v", err)
	}
	c2, err := ahp.Connect(ctx, tr2, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("c2 Connect failed: %v", err)
	}
	defer c2.Shutdown(ctx)

	// Reconnect with lastSeenServerSeq == initRes.ServerSeq (within buffer)
	recRes, err := c2.Reconnect(ctx, "recon-client-gap", initRes.ServerSeq, []string{sessURI})
	if err != nil {
		t.Fatalf("c2.Reconnect failed: %v", err)
	}
	if _, ok := recRes.Value.(*ahptypes.ReconnectReplayResult); !ok {
		t.Errorf("expected ReconnectReplayResult when sequence within buffer, got %T", recRes.Value)
	}
}

func TestAHPServer_UnsupportedProtocolVersion(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model: "version-test-model",
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"
	tr, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}
	client, err := ahp.Connect(ctx, tr, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}
	defer client.Shutdown(ctx)

	// Request completely incompatible version
	_, err = client.Initialize(ctx, "incompatible-client", []string{"99.0.0"}, nil)
	if err == nil {
		t.Fatalf("expected Initialize with incompatible protocol version to fail")
	}
	if !strings.Contains(err.Error(), "unsupported protocol version") && !strings.Contains(err.Error(), "-32005") {
		t.Errorf("expected unsupported protocol version error (-32005), got: %v", err)
	}
}

func TestAHPServer_ConcurrentTurnRejected(t *testing.T) {
	holdTurn := make(chan struct{})
	mock := &testCustomMockBackend{
		streamFn: func(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
			out := make(chan backend.StreamChunk, 2)
			go func() {
				defer close(out)
				<-holdTurn
				out <- backend.StreamChunk{Text: "done", Done: true}
			}()
			return out, nil
		},
	}
	defer close(holdTurn)

	srv, addr := startServer(t, Config{
		Model: "concurrent-model",
		BackendFor: func(string) backend.Client {
			return mock
		},
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"
	tr, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}
	client, err := ahp.Connect(ctx, tr, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}
	defer client.Shutdown(ctx)

	if _, err := client.Initialize(ctx, "concurrent-client", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	var createRes map[string]any
	if err := client.Request(ctx, "session.create", ahptypes.CreateSessionParams{Channel: "ahp-root://"}, &createRes); err != nil {
		t.Fatalf("session.create failed: %v", err)
	}
	chatURI := createRes["chat"].(string)

	// Start first turn
	var pRes1 map[string]any
	err = client.Request(ctx, "prompt", map[string]any{
		"channel": chatURI,
		"prompt":  "First slow turn",
	}, &pRes1)
	if err != nil {
		t.Fatalf("first prompt failed: %v", err)
	}

	// Try starting second turn on same chat immediately
	err = client.Request(ctx, "prompt", map[string]any{
		"channel": chatURI,
		"prompt":  "Second concurrent turn",
	}, nil)
	if err == nil {
		t.Errorf("expected second concurrent turn to be rejected with TurnInProgress error")
	}
	if !strings.Contains(err.Error(), "turn already in progress") && !strings.Contains(err.Error(), "-32004") {
		t.Errorf("expected error -32004 (TurnInProgress), got %v", err)
	}
}

func TestAHPServer_FetchTurns(t *testing.T) {
	srv, addr := startServer(t, Config{
		Model: "fetch-turns-model",
	})
	_ = srv

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws://" + addr + "/ahp"
	tr, err := ahpws.Connect(ctx, wsURL)
	if err != nil {
		t.Fatalf("ahpws.Connect failed: %v", err)
	}
	client, err := ahp.Connect(ctx, tr, ahp.DefaultConfig())
	if err != nil {
		t.Fatalf("ahp.Connect failed: %v", err)
	}
	defer client.Shutdown(ctx)

	if _, err := client.Initialize(ctx, "fetch-client", ahptypes.SupportedProtocolVersions(), nil); err != nil {
		t.Fatalf("Initialize failed: %v", err)
	}

	var createRes map[string]any
	if err := client.Request(ctx, "session.create", ahptypes.CreateSessionParams{Channel: "ahp-root://"}, &createRes); err != nil {
		t.Fatalf("session.create failed: %v", err)
	}
	chatURI := createRes["chat"].(string)

	// Fetch turns on valid chat
	err = client.Request(ctx, "fetchTurns", ahptypes.FetchTurnsParams{
		Channel: chatURI,
	}, nil)
	if err != nil {
		t.Fatalf("fetchTurns on valid chat failed: %v", err)
	}

	// Fetch turns on nonexistent chat
	err = client.Request(ctx, "fetchTurns", ahptypes.FetchTurnsParams{
		Channel: "ahp-chat:/nonexistent-chat",
	}, nil)
	if err == nil {
		t.Errorf("expected fetchTurns on nonexistent chat to fail")
	}
}
