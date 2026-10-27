package agentbrowser

import (
	"context"
	"strings"
	"testing"
	"time"
)

// newTestBroker returns a broker with a short timeout, so a test that
// waits for a timeout does not wait for the reference's 30 seconds.
func newTestBroker(t *testing.T) *Broker {
	t.Helper()
	b := NewBroker()
	b.timeout = 50 * time.Millisecond
	return b
}

// subscribeWindow registers one window and returns its subscription.
func subscribeWindow(t *testing.T, b *Broker, windowID string) *Subscription {
	t.Helper()
	sub := b.Subscribe(context.Background(), SubscribeRequest{
		WindowID:                    windowID,
		Surface:                     "hub",
		DisplayedRootConversationID: "conv-1",
	})
	t.Cleanup(sub.Close)
	return sub
}

func TestBrokerRequestReachesTheWindowAndTheAnswerComesBack(t *testing.T) {
	b := newTestBroker(t)
	sub := subscribeWindow(t, b, "win-1")

	go func() {
		req := <-sub.Requests
		_ = b.Fulfill(&Response{
			RequestID:  req.RequestID,
			OK:         true,
			ResultJSON: `{"ok":true,"url":"https://example.com"}`,
			WindowID:   "win-1",
		})
	}()

	resp, err := b.Request(context.Background(), &Request{
		Action:             "click",
		RootConversationID: "conv-1",
		ParamsJSON:         `{"uid":"u1"}`,
	})
	if err != nil {
		t.Fatalf("Request: %v", err)
	}
	if !resp.OK {
		t.Fatalf("the answer is not ok: %+v", resp)
	}
	if !strings.Contains(resp.ResultJSON, "example.com") {
		t.Fatalf("the result is %q", resp.ResultJSON)
	}
}

func TestBrokerRequestWithoutAWindowFails(t *testing.T) {
	b := newTestBroker(t)
	_, err := b.Request(context.Background(), &Request{Action: "click"})
	if err == nil {
		t.Fatal("the request succeeded without a window")
	}
	if err.Error() != "no active AGY Hub webview browser windows connected to Language Server" {
		t.Fatalf("the error is %q", err.Error())
	}
}

func TestBrokerRequestNeedsAnAction(t *testing.T) {
	b := newTestBroker(t)
	_, err := b.Request(context.Background(), &Request{})
	if err == nil || err.Error() != "webview_browser requires a non-empty 'action'" {
		t.Fatalf("the error is %v", err)
	}
}

func TestBrokerRequestTimesOut(t *testing.T) {
	b := newTestBroker(t)
	subscribeWindow(t, b, "win-1")
	_, err := b.Request(context.Background(), &Request{Action: "click"})
	if err == nil {
		t.Fatal("the request did not time out")
	}
	want := "timed out after 50ms waiting for AUIT webview to fulfill browser action \"click\""
	if !strings.HasPrefix(err.Error(), want) {
		t.Fatalf("the error is %q, want the prefix %q", err.Error(), want)
	}
}

func TestBrokerFulfillRejectsTheFourFailures(t *testing.T) {
	b := newTestBroker(t)
	if err := b.Fulfill(nil); err == nil || err.Error() != "agentbrowser.Response cannot be nil" {
		t.Fatalf("a nil response: %v", err)
	}
	if err := b.Fulfill(&Response{}); err == nil || err.Error() != "agentbrowser.Response.RequestID is required" {
		t.Fatalf("a response without an id: %v", err)
	}
	err := b.Fulfill(&Response{RequestID: "nope"})
	if err == nil || !strings.HasPrefix(err.Error(), "no pending agent browser request found for request_id=\"nope\"") {
		t.Fatalf("an unknown request id: %v", err)
	}
}

func TestBrokerFulfillRejectsAWindowMismatch(t *testing.T) {
	b := newTestBroker(t)
	sub := subscribeWindow(t, b, "win-1")
	go func() {
		req := <-sub.Requests
		err := b.Fulfill(&Response{RequestID: req.RequestID, WindowID: "win-2"})
		if err == nil {
			t.Error("the mismatch was accepted")
			return
		}
		want := "window_id mismatch for request_id=\"" + req.RequestID + "\": got \"win-2\", want \"win-1\""
		if err.Error() != want {
			t.Errorf("the error is %q, want %q", err.Error(), want)
		}
	}()
	if _, err := b.Request(context.Background(), &Request{Action: "click"}); err == nil {
		t.Fatal("the request did not fail")
	}
}

func TestBrokerPicksTheWindowThatDisplaysTheConversation(t *testing.T) {
	b := newTestBroker(t)
	subscribeWindow(t, b, "win-a")
	sub := subscribeWindow(t, b, "win-b")
	b.UpdateWindow(UpdateSubscriptionRequest{
		WindowID:                    "win-b",
		DisplayedRootConversationID: "conv-9",
	})
	got, err := b.selectWindow(&Request{RootConversationID: "conv-9"})
	if err != nil {
		t.Fatalf("selectWindow: %v", err)
	}
	if got.sub.WindowID != "win-b" {
		t.Fatalf("the window is %q, want win-b", got.sub.WindowID)
	}
	_ = sub
}

func TestBrokerPicksTheNamedTargetWindow(t *testing.T) {
	b := newTestBroker(t)
	subscribeWindow(t, b, "win-a")
	subscribeWindow(t, b, "win-b")
	got, err := b.selectWindow(&Request{TargetWindowID: "win-a"})
	if err != nil {
		t.Fatalf("selectWindow: %v", err)
	}
	if got.sub.WindowID != "win-a" {
		t.Fatalf("the window is %q, want win-a", got.sub.WindowID)
	}
}

func TestBrokerPrefersTheSenderWindow(t *testing.T) {
	b := newTestBroker(t)
	subscribeWindow(t, b, "win-a")
	subscribeWindow(t, b, "win-b")
	b.RecordUserMessageWindow("conv-1", "win-a")
	got, err := b.selectWindow(&Request{RootConversationID: "conv-1"})
	if err != nil {
		t.Fatalf("selectWindow: %v", err)
	}
	if got.sub.WindowID != "win-a" {
		t.Fatalf("the window is %q, want win-a", got.sub.WindowID)
	}
}

func TestBrokerUpdateWindowNeedsAWindowID(t *testing.T) {
	b := newTestBroker(t)
	err := b.UpdateWindow(UpdateSubscriptionRequest{})
	if err == nil || err.Error() != "UpdateAgentBrowserSubscription requires window_id" {
		t.Fatalf("the error is %v", err)
	}
}

func TestBrokerOwnerFenceRefusesASecondOwner(t *testing.T) {
	b := newTestBroker(t)
	if err := b.ClaimOwner("conv-1", "sub-a", "win-1"); err != nil {
		t.Fatalf("the first claim: %v", err)
	}
	if err := b.ClaimOwner("conv-1", "sub-a", "win-1"); err != nil {
		t.Fatalf("the same owner: %v", err)
	}
	err := b.ClaimOwner("conv-1", "sub-b", "win-1")
	if err == nil {
		t.Fatal("the second owner was accepted")
	}
	if !strings.HasPrefix(err.Error(), "browser is currently in use by another browser subagent in this conversation") {
		t.Fatalf("the error is %q", err.Error())
	}
	b.ReleaseOwner("conv-1", "sub-a")
	if err := b.ClaimOwner("conv-1", "sub-b", "win-1"); err != nil {
		t.Fatalf("the claim after the release: %v", err)
	}
}

func TestBrokerTimeoutPolicy(t *testing.T) {
	b := NewBroker()
	if got := b.requestTimeoutForAction("click", `{"uid":"u"}`); got != 30*time.Second {
		t.Fatalf("the base timeout is %s", got)
	}
	got := b.requestTimeoutForAction("batch", `{"steps":[{"action":"click"},{"action":"click"}]}`)
	want := 50 * time.Second
	if got != want {
		t.Fatalf("the batch timeout is %s, want %s", got, want)
	}
	got = b.requestTimeoutForAction("batch", `{"steps":[`+strings.Repeat(`{"action":"click"},`, 9)+`{"action":"click"}]}`)
	want = 130 * time.Second
	if got != want {
		t.Fatalf("the long batch timeout is %s, want %s", got, want)
	}
}
