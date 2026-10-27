package rctunnel

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"
)

// TestReopenBackChannelRetriesTransientFailures pins the rule that a
// failed reopen does not hand the session back.
//
// The cloud's browser-side channel is bound to the session's SID, so a
// new session leaves the browser's in-flight requests unanswered — the
// web UI waits for them forever and the remote-control page never
// leaves "Connecting to instance…". The reference retries the GET
// rather than handing the session back ("Going to retry GET" is in its
// string table).
//
// 【一手】 2026-10-26, live: the clone gave the session up on the
// first failed reopen. 861 responses were lost that way in one day,
// every one of them with "context canceled".
func TestReopenBackChannelRetriesTransientFailures(t *testing.T) {
	var attempts atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if attempts.Add(1) <= 2 {
			w.WriteHeader(http.StatusBadRequest)
			_, _ = w.Write([]byte("<!DOCTYPE html>"))
			return
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()

	c := NewClient(ClientOptions{Endpoint: srv.URL})
	c.httpClient = srv.Client()
	c.sess = connectHandshake{ver: 8, sver: 1, bcTimeoutMS: 50000}

	resp, err := c.retryReopenBackChannel(context.Background(), "token", "sid", "gsession",
		errors.New("reopen backchannel HTTP 400"), time.Minute)
	if err != nil {
		t.Fatalf("retryReopenBackChannel: %v", err)
	}
	defer resp.Body.Close()

	if got := attempts.Load(); got != 3 {
		t.Errorf("attempts = %d, want 3 (two failures, then a success)", got)
	}
	if resp.StatusCode != http.StatusOK {
		t.Errorf("status = %d, want 200", resp.StatusCode)
	}
}

// TestReopenBackChannelGivesUpAfterTheWindow pins the other half: a
// session that cannot be reopened is handed back, so the tunnel can
// register a fresh one.
func TestReopenBackChannelGivesUpAfterTheWindow(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte("<!DOCTYPE html>"))
	}))
	defer srv.Close()

	c := NewClient(ClientOptions{Endpoint: srv.URL})
	c.httpClient = srv.Client()
	c.sess = connectHandshake{ver: 8, sver: 1, bcTimeoutMS: 50000}

	// The window bounds the retry; the test shortens it so the case
	// stays quick.
	start := time.Now()
	_, err := c.retryReopenBackChannel(context.Background(), "token", "sid", "gsession",
		errors.New("reopen backchannel HTTP 400"), 800*time.Millisecond)
	if err == nil {
		t.Fatal("retryReopenBackChannel: want an error, got none")
	}
	if elapsed := time.Since(start); elapsed > 10*time.Second {
		t.Errorf("the retry took %s, want it bounded by the window", elapsed)
	}
}
