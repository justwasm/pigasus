// ls_agentbrowser_test.go: the three LanguageServerService RPCs of the
// agent↔browser bridge.
//
// 【一手】 2026-10-27, the reference's own descriptor set
// (out/ide-language-server/protos/language_server.protoset) pins the
// three messages; the reference's frontend reads exactly the member
// names the clone writes.
package remotecontrol

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/justwasm/pigasus/agentbrowser"
)

// browserDispatcher returns a dispatcher with one broker.
func browserDispatcher(t *testing.T) (*Dispatcher, *agentbrowser.Broker) {
	t.Helper()
	broker := agentbrowser.NewBroker()
	disp, err := NewDispatcher(Config{
		Session: NewSessionState("1.4.7"),
		Browser: broker,
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	return disp, broker
}

// waitForSubscriber waits until the window registered itself.
func waitForSubscriber(t *testing.T, broker *agentbrowser.Broker) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for broker.SubscriberCount() == 0 && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if broker.SubscriberCount() != 1 {
		t.Fatal("the window did not subscribe")
	}
}

// browserFrame is the frame the subscribe stream answers with.
type browserFrame struct {
	Request struct {
		RequestID  string `json:"requestId"`
		Action     string `json:"action"`
		ParamsJSON string `json:"paramsJson"`
	} `json:"request"`
}

func TestAgentBrowserStreamCarriesTheRequest(t *testing.T) {
	disp, broker := browserDispatcher(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	frames := make(chan json.RawMessage, 4)
	done := make(chan error, 1)
	go func() {
		_, err := disp.DispatchStream(ctx, "SubscribeToAgentBrowserRequests",
			`{"windowId":"win-1","surface":"hub","openConversationIds":["conv-1"],"displayedRootConversationId":"conv-1"}`,
			func(frame json.RawMessage) error {
				frames <- frame
				return nil
			})
		done <- err
	}()

	waitForSubscriber(t, broker)

	go func() {
		_, _ = broker.Request(context.Background(), &agentbrowser.Request{
			Action:             "click",
			RootConversationID: "conv-1",
			ParamsJSON:         `{"uid":"e-1"}`,
		})
	}()

	select {
	case frame := <-frames:
		var decoded browserFrame
		if err := json.Unmarshal(frame, &decoded); err != nil {
			t.Fatalf("the frame: %v", err)
		}
		if decoded.Request.Action != "click" {
			t.Fatalf("the action is %q", decoded.Request.Action)
		}
		if decoded.Request.ParamsJSON != `{"uid":"e-1"}` {
			t.Fatalf("the params are %q", decoded.Request.ParamsJSON)
		}
		if decoded.Request.RequestID == "" {
			t.Fatal("the request has no id")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("no frame arrived")
	}
	cancel()
	<-done
}

func TestAgentBrowserUpdateSubscription(t *testing.T) {
	disp, broker := browserDispatcher(t)
	sub := broker.Subscribe(context.Background(), agentbrowser.SubscribeRequest{WindowID: "win-1"})
	defer sub.Close()

	result, rpcErr, ok := disp.routeAgentBrowserMethods("UpdateAgentBrowserSubscription",
		json.RawMessage(`{"windowId":"win-1","displayedRootConversationId":"conv-7","supportsIsolatedWorld":true}`))
	if !ok {
		t.Fatal("the method was not routed")
	}
	if rpcErr != nil {
		t.Fatalf("the error is %+v", rpcErr)
	}
	if result == nil {
		t.Fatal("the answer is nil")
	}
	if !broker.SupportsIsolatedWorld() {
		t.Fatal("the isolated world was not recorded")
	}
}

func TestAgentBrowserUpdateSubscriptionNeedsAWindow(t *testing.T) {
	disp, _ := browserDispatcher(t)
	_, rpcErr, ok := disp.routeAgentBrowserMethods("UpdateAgentBrowserSubscription", json.RawMessage(`{}`))
	if !ok {
		t.Fatal("the method was not routed")
	}
	if rpcErr == nil || rpcErr.Code != -32602 {
		t.Fatalf("the error is %+v", rpcErr)
	}
	if rpcErr.Message != "UpdateAgentBrowserSubscription requires window_id" {
		t.Fatalf("the message is %q", rpcErr.Message)
	}
}

func TestAgentBrowserFulfillNeedsAResponse(t *testing.T) {
	disp, _ := browserDispatcher(t)
	_, rpcErr, ok := disp.routeAgentBrowserMethods("FulfillAgentBrowserRequest", json.RawMessage(`{}`))
	if !ok {
		t.Fatal("the method was not routed")
	}
	if rpcErr == nil || rpcErr.Code != -32602 {
		t.Fatalf("the error is %+v", rpcErr)
	}
	if rpcErr.Message != "FulfillAgentBrowserRequest requires a non-nil response payload" {
		t.Fatalf("the message is %q", rpcErr.Message)
	}
}

func TestAgentBrowserFulfillNeedsARequestID(t *testing.T) {
	disp, _ := browserDispatcher(t)
	_, rpcErr, ok := disp.routeAgentBrowserMethods("FulfillAgentBrowserRequest", json.RawMessage(`{"response":{}}`))
	if !ok {
		t.Fatal("the method was not routed")
	}
	if rpcErr == nil || rpcErr.Code != -32602 {
		t.Fatalf("the error is %+v", rpcErr)
	}
	if rpcErr.Message != "FulfillAgentBrowserRequest requires response.request_id" {
		t.Fatalf("the message is %q", rpcErr.Message)
	}
}

func TestAgentBrowserFulfillAnswersAnUnknownRequest(t *testing.T) {
	disp, _ := browserDispatcher(t)
	_, rpcErr, ok := disp.routeAgentBrowserMethods("FulfillAgentBrowserRequest",
		json.RawMessage(`{"response":{"requestId":"nope","ok":true}}`))
	if !ok {
		t.Fatal("the method was not routed")
	}
	if rpcErr == nil || rpcErr.Code != -32018 {
		t.Fatalf("the error is %+v", rpcErr)
	}
}

func TestAgentBrowserFulfillHandsTheAnswerOver(t *testing.T) {
	disp, broker := browserDispatcher(t)
	sub := broker.Subscribe(context.Background(), agentbrowser.SubscribeRequest{WindowID: "win-1"})
	defer sub.Close()

	answers := make(chan *agentbrowser.Response, 1)
	go func() {
		req := <-sub.Requests
		answers <- &agentbrowser.Response{RequestID: req.RequestID, OK: true}
	}()

	go func() {
		_, _ = broker.Request(context.Background(), &agentbrowser.Request{
			Action:             "click",
			RootConversationID: "conv-1",
		})
	}()

	deadline := time.Now().Add(2 * time.Second)
	var requestID string
	for requestID == "" && time.Now().Before(deadline) {
		select {
		case resp := <-answers:
			requestID = resp.RequestID
		case <-time.After(10 * time.Millisecond):
		}
	}
	if requestID == "" {
		t.Fatal("the request never arrived")
	}

	result, rpcErr, ok := disp.routeAgentBrowserMethods("FulfillAgentBrowserRequest",
		json.RawMessage(`{"response":{"requestId":"`+requestID+`","ok":true}}`))
	if !ok {
		t.Fatal("the method was not routed")
	}
	if rpcErr != nil {
		t.Fatalf("the error is %+v", rpcErr)
	}
	if result == nil {
		t.Fatal("the answer is nil")
	}
}
