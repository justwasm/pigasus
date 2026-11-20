package agyproxy

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/coder/websocket"
)

func TestIsLiveWebSocket(t *testing.T) {
	tests := []struct {
		name    string
		method  string
		upgrade string
		path    string
		want    bool
	}{
		{
			name:    "Standard Gemini Live WebSocket",
			method:  "GET",
			upgrade: "websocket",
			path:    "/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent",
			want:    true,
		},
		{
			name:    "Constrained Gemini Live WebSocket",
			method:  "GET",
			upgrade: "websocket",
			path:    "/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContentConstrained",
			want:    true,
		},
		{
			name:    "Vertex AI Live WebSocket",
			method:  "GET",
			upgrade: "websocket",
			path:    "/ws/google.cloud.aiplatform.v1beta1.LlmBidiService/BidiGenerateContent",
			want:    true,
		},
		{
			name:    "Live shortcut path",
			method:  "GET",
			upgrade: "websocket",
			path:    "/v1/live",
			want:    true,
		},
		{
			name:    "Responses WebSocket (not Live)",
			method:  "GET",
			upgrade: "websocket",
			path:    "/v1/responses",
			want:    false,
		},
		{
			name:    "Regular HTTP GET to Live path (no WebSocket upgrade)",
			method:  "GET",
			upgrade: "",
			path:    "/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent",
			want:    false,
		},
		{
			name:    "POST to Live path",
			method:  "POST",
			upgrade: "websocket",
			path:    "/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent",
			want:    false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			req, err := http.NewRequest(tc.method, tc.path, nil)
			if err != nil {
				t.Fatalf("NewRequest: %v", err)
			}
			if tc.upgrade != "" {
				req.Header.Set("Upgrade", tc.upgrade)
			}
			got := isLiveWebSocket(req)
			if got != tc.want {
				t.Errorf("isLiveWebSocket(%s %s, Upgrade: %q) = %v; want %v",
					tc.method, tc.path, tc.upgrade, got, tc.want)
			}
		})
	}
}

func TestLiveWebSocket_ProxyRoundtripAndCredentialInjection(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var receivedKey string

	// 1. Mock upstream Gemini Live WebSocket server
	upstreamMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedKey = r.Header.Get("x-goog-api-key")
		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			InsecureSkipVerify: true,
		})
		if err != nil {
			t.Errorf("upstream mock accept failed: %v", err)
			return
		}
		defer conn.CloseNow()

		for {
			typ, b, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			// Echo frame back to client
			if err := conn.Write(r.Context(), typ, b); err != nil {
				return
			}
		}
	}))
	defer upstreamMock.Close()

	// 2. Start agyproxy pointing at the upstream test server
	t.Setenv("GEMINI_API_KEY", "injected-secret-api-key")
	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstreamMock.URL,
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatalf("New agyproxy server: %v", err)
	}
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("srv.Start: %v", err)
	}
	defer srv.Close()

	// 3. Client connects to agyproxy via WebSocket using placeholder key
	clientURL := "ws://" + addr + "/ws/google.ai.generativelanguage.v1alpha.GenerativeService.BidiGenerateContent"
	header := http.Header{}
	header.Set("x-goog-api-key", "alt-backend") // Placeholder key from client

	clientConn, _, err := websocket.Dial(ctx, clientURL, &websocket.DialOptions{
		HTTPHeader: header,
	})
	if err != nil {
		t.Fatalf("Dial proxy live ws: %v", err)
	}
	defer clientConn.CloseNow()

	// Verify upstream received the injected credential
	time.Sleep(50 * time.Millisecond)
	if receivedKey != "injected-secret-api-key" {
		t.Errorf("upstream mock receivedKey = %q; want 'injected-secret-api-key'", receivedKey)
	}

	// 4. Send binary audio frame and verify echo
	audioFrame := []byte{0x01, 0x02, 0x03, 0x04}
	if err := clientConn.Write(ctx, websocket.MessageBinary, audioFrame); err != nil {
		t.Fatalf("client write binary frame: %v", err)
	}

	typ, echoAudio, err := clientConn.Read(ctx)
	if err != nil {
		t.Fatalf("client read echoed binary frame: %v", err)
	}
	if typ != websocket.MessageBinary {
		t.Errorf("echoed frame type = %v; want MessageBinary", typ)
	}
	if string(echoAudio) != string(audioFrame) {
		t.Errorf("echoed frame bytes = %v; want %v", echoAudio, audioFrame)
	}

	// 5. Send text control frame and verify echo
	textFrame := `{"clientContent":{"turns":[{"parts":[{"text":"hello"}]}]}}`
	if err := clientConn.Write(ctx, websocket.MessageText, []byte(textFrame)); err != nil {
		t.Fatalf("client write text frame: %v", err)
	}

	typ, echoText, err := clientConn.Read(ctx)
	if err != nil {
		t.Fatalf("client read echoed text frame: %v", err)
	}
	if typ != websocket.MessageText {
		t.Errorf("echoed frame type = %v; want MessageText", typ)
	}
	if string(echoText) != textFrame {
		t.Errorf("echoed frame text = %s; want %s", string(echoText), textFrame)
	}
}

func TestLiveWebSocket_GracefulTeardownOnServerClose(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	upstreamMock := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		defer conn.CloseNow()
		for {
			typ, b, err := conn.Read(r.Context())
			if err != nil {
				return
			}
			_ = conn.Write(r.Context(), typ, b)
		}
	}))
	defer upstreamMock.Close()

	srv, err := New(Config{
		Addr:     "127.0.0.1:0",
		Upstream: upstreamMock.URL,
		Logf:     t.Logf,
	})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	addr, err := srv.Start()
	if err != nil {
		t.Fatalf("Start: %v", err)
	}

	clientURL := "ws://" + addr + "/v1/live"
	clientConn, _, err := websocket.Dial(ctx, clientURL, nil)
	if err != nil {
		t.Fatalf("Dial: %v", err)
	}
	defer clientConn.CloseNow()

	// Server close should send going away
	closeErrCh := make(chan error, 1)
	go func() {
		closeErrCh <- srv.Close()
	}()

	_, _, err = clientConn.Read(ctx)
	if err == nil {
		t.Error("expected read error after server close, got nil")
	}

	if err := <-closeErrCh; err != nil {
		t.Errorf("srv.Close error: %v", err)
	}
}
