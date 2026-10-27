package rctunnel

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// closeFrame is what the cloud sends when it ends the session: the
// string "close" as an item of the backchannel chunk.
func closeFrame() string {
	payload := `[[1,["close"]]]`
	return fmt.Sprintf("%d\n%s", len(payload), payload)
}

// TestReadBackChannelStopsOnTheCloseSignal pins the rule that the
// cloud's "close" signal ends the session instead of being logged and
// ignored.
//
// 【一手】 2026-10-26, live: the cloud sent the string "close" on the
// backchannel at 12:36:23.404 — the same second the instance's row in
// the dashboard's registry flipped to INSTANCE_STATUS_DISCONNECTED and
// its statusLastUpdatedAt froze there for the rest of the day. The
// clone logged the signal, kept reading the same session, and kept
// answering the cloud's GetStatus pings; the dashboard still refused
// to open the app iframe, because the cloud had already disowned that
// session.
func TestReadBackChannelStopsOnTheCloseSignal(t *testing.T) {
	c := NewClient(ClientOptions{})

	err := c.readBackChannel(context.Background(), "token", strings.NewReader(closeFrame()))
	if !errors.Is(err, errServerClosed) {
		t.Fatalf("readBackChannel: err = %v, want errServerClosed", err)
	}
}

// TestPumpBackChannelDoesNotReopenAClosedSession pins the other half:
// a closed session is not re-opened on the same SID. Only a fresh
// handshake — a new SID and a new registration — puts the instance back
// into the cloud's registry.
func TestPumpBackChannelDoesNotReopenAClosedSession(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(closeFrame()))
	}))
	defer srv.Close()

	c := NewClient(ClientOptions{Endpoint: srv.URL})
	c.httpClient = srv.Client()
	c.sess = connectHandshake{ver: 8, sver: 1, bcTimeoutMS: 50000}

	resp, err := c.httpClient.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()

	err = c.pumpBackChannel(context.Background(), "token", "sid", "gsession", resp)
	if !errors.Is(err, errServerClosed) {
		t.Fatalf("pumpBackChannel: err = %v, want errServerClosed", err)
	}
	// The initial GET is the one the test made itself; the pump must
	// not have re-opened the poll on the dead SID.
	if got := requests.Load(); got != 1 {
		t.Errorf("requests = %d, want 1 (the poll must not be re-opened)", got)
	}
}

// TestPumpBackChannelReopensAfterAPollEnds is the other side of the
// same rule: a poll that ends on its own is re-opened on the same
// session. The cloud closes the poll every ~47 s and the session
// survives it.
func TestPumpBackChannelReopensAfterAPollEnds(t *testing.T) {
	var requests atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		w.WriteHeader(http.StatusOK)
		// The first poll ends with EOF, the second one stays open.
		if requests.Load() == 1 {
			return
		}
		<-r.Context().Done()
	}))
	defer srv.Close()

	c := NewClient(ClientOptions{Endpoint: srv.URL})
	c.httpClient = srv.Client()
	c.sess = connectHandshake{ver: 8, sver: 1, bcTimeoutMS: 50000}

	resp, err := c.httpClient.Get(srv.URL)
	if err != nil {
		t.Fatalf("get: %v", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	go func() {
		time.Sleep(2 * time.Second)
		cancel()
	}()
	_ = c.pumpBackChannel(ctx, "token", "sid", "gsession", resp)

	if got := requests.Load(); got < 2 {
		t.Errorf("requests = %d, want at least 2 (the poll must be re-opened)", got)
	}
}
