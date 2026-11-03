package refbackend

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
)

// fakeCSRFReference is a reference that demands the secret: it serves
// the index document that carries it and refuses every call that does
// not present it.
func fakeCSRFReference(t *testing.T, token *atomic.Value) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/" {
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_, _ = w.Write([]byte(`<script>window.__APP_CONFIG__ = {"csrfToken":"` + token.Load().(string) + `"};</script>`))
			return
		}
		if r.Header.Get("x-codeium-csrf-token") != token.Load().(string) {
			w.WriteHeader(http.StatusUnauthorized)
			_, _ = w.Write([]byte(`{"code":"unauthenticated","message":"unauthenticated: missing CSRF token"}`))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

// TestDispatchPresentsTheReferencesSecret checks that the proxy learns
// the reference's secret from the reference's own page and presents it:
// the reference rejects every call that does not.
func TestDispatchPresentsTheReferencesSecret(t *testing.T) {
	var token atomic.Value
	token.Store("ref-csrf-token")
	srv := fakeCSRFReference(t, &token)

	disp, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	res, err := disp.DispatchDirect(context.Background(), "GetUserStatus", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect: %v", err)
	}
	if !strings.Contains(res, `"ok":true`) {
		t.Fatalf("unexpected answer: %s", res)
	}
}

// TestDispatchRelearnsTheSecret checks that a reference that was
// restarted with another secret is picked up: the proxy learns it again
// and tries once more.
func TestDispatchRelearnsTheSecret(t *testing.T) {
	var token atomic.Value
	token.Store("first-token")
	srv := fakeCSRFReference(t, &token)

	disp, err := New(srv.URL)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := disp.DispatchDirect(context.Background(), "GetUserStatus", "{}"); err != nil {
		t.Fatalf("DispatchDirect: %v", err)
	}
	token.Store("second-token")
	res, err := disp.DispatchDirect(context.Background(), "GetUserStatus", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect after the restart: %v", err)
	}
	if !strings.Contains(res, `"ok":true`) {
		t.Fatalf("unexpected answer: %s", res)
	}
}
