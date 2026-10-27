package rctunnel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// TestSendForwardMarksTheSessionDeadOnA4xx pins the rule that a
// session the cloud has disowned is ended instead of being used.
//
// 【一手】 2026-10-26, live: the clone kept posting into a session the
// cloud had disowned. Every answer came back
//
//	400 Bad Request — "Your client has issued a malformed or illegal
//	request. Unknown SID That's all we know."
//
// the browser's request was cancelled ten seconds later ("proxyRequest
// reqID=13 was canceled by server"), and the dashboard's liveness
// probe never completed — the page said the instance was disconnected
// while the instance was sitting right there, registered and healthy.
//
// The reference ends the connection when its session dies and starts a
// new one:
//
//	Connection ended. Retrying immediately...
//	Starting V2 remote control connection to: https://jetski-webchannel.googleapis.com:443
//	Connection status: Connected
func TestSendForwardMarksTheSessionDeadOnA4xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("<p>Unknown SID <ins>That's all we know.</ins>"))
	}))
	defer srv.Close()

	c := NewClient(ClientOptions{Endpoint: srv.URL})
	c.httpClient = srv.Client()

	if _, err := c.sendForward(context.Background(), "token", "{}"); err == nil {
		t.Fatal("sendForward: err = nil, want an error")
	}
	if !c.deadSession.Load() {
		t.Error("the session is still alive; a 4xx from the forward channel must end it")
	}
}

// TestSendForwardKeepsTheSessionAliveOnA2xx is the other side of the
// same rule: an answer the cloud accepted leaves the session alone.
func TestSendForwardKeepsTheSessionAliveOnA2xx(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("9\n[1,1,7]\n"))
	}))
	defer srv.Close()

	c := NewClient(ClientOptions{Endpoint: srv.URL})
	c.httpClient = srv.Client()

	if _, err := c.sendForward(context.Background(), "token", "{}"); err != nil {
		t.Fatalf("sendForward: %v", err)
	}
	if c.deadSession.Load() {
		t.Error("the session was marked dead; an accepted answer must leave it alone")
	}
}

// TestPumpBackChannelEndsOnADeadSession pins that the poll does not
// carry on after the cloud has disowned the session: the poll is
// cancelled, the pump returns, and the run loop hands the instance a
// new session.
func TestPumpBackChannelEndsOnADeadSession(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewClient(ClientOptions{Endpoint: srv.URL})
	c.httpClient = srv.Client()
	c.sess = connectHandshake{ver: 8, sver: 1, bcTimeoutMS: 50000}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.sessionCancel = cancel

	req, err := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := c.httpClient.Do(req)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	// The cloud disowns the session: the poll has to end with it.
	c.markSessionDead("forward POST HTTP 400")

	err = c.pumpBackChannel(ctx, "token", "sid", "gsession", resp)
	if err == nil {
		t.Fatal("pumpBackChannel: err = nil, want the dead-session error")
	}
	if !strings.Contains(err.Error(), "disowned") {
		t.Errorf("pumpBackChannel: err = %v, want it to name the disowned session", err)
	}
	// The poll must not have been re-opened on the dead session.
	if got := requests.Load(); got != 1 {
		t.Errorf("requests = %d, want 1 (the poll must not be re-opened)", got)
	}
}

// TestMarkSessionDeadCancelsThePoll pins that the poll waiting on a
// disowned session is cancelled at once: the poll is a long poll, so
// without the cancel the client would not notice the dead session
// until the poll ended on its own.
func TestMarkSessionDeadCancelsThePoll(t *testing.T) {
	c := NewClient(ClientOptions{})

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	c.sessionCancel = cancel

	c.markSessionDead("forward POST HTTP 400")

	select {
	case <-ctx.Done():
	case <-time.After(time.Second):
		t.Fatal("the poll was not cancelled")
	}
	if !c.deadSession.Load() {
		t.Error("the session was not marked dead")
	}
}
