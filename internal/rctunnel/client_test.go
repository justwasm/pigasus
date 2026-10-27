package rctunnel

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type mockDispatcher struct {
	dispatchedMethod  string
	dispatchedPayload string
	responsePayload   string
	err               error
}

func (m *mockDispatcher) DispatchDirect(ctx context.Context, method string, payloadText string) (string, error) {
	m.dispatchedMethod = method
	m.dispatchedPayload = payloadText
	return m.responsePayload, m.err
}

func TestClient_MockWebChannelRoundTrip(t *testing.T) {
	var (
		mu           sync.Mutex
		handshakeHit bool
		regReceived  atomic.Bool
		rpcReceived  atomic.Bool
		forwardOfs   []string
		backChanDone = make(chan struct{})
	)

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case "POST":
			body, _ := io.ReadAll(r.Body)
			bodyStr := string(body)

			if strings.Contains(bodyStr, "count=0") {
				// 1. Handshake POST
				mu.Lock()
				handshakeHit = true
				mu.Unlock()

				w.Header().Set("Content-Type", "text/plain; charset=utf-8")
				w.Header().Set("X-Http-Session-Id", "mock-gsession-xyz")
				w.WriteHeader(http.StatusOK)

				payload := `[["c","mock-sid-7890",null,8]]`
				fmt.Fprintf(w, "%d\n%s", len(payload), payload)
				return
			}

			// ForwardChannel POST
			if r.URL.Query().Get("SID") != "mock-sid-7890" {
				t.Errorf("expected SID=mock-sid-7890, got %s", r.URL.Query().Get("SID"))
			}
			if r.URL.Query().Get("gsessionid") != "mock-gsession-xyz" {
				t.Errorf("expected gsessionid=mock-gsession-xyz, got %s", r.URL.Query().Get("gsessionid"))
			}

			for _, part := range strings.Split(bodyStr, "&") {
				if strings.HasPrefix(part, "ofs=") {
					mu.Lock()
					forwardOfs = append(forwardOfs, strings.TrimPrefix(part, "ofs="))
					mu.Unlock()
				}
			}

			if strings.Contains(bodyStr, "supportsChunking") {
				regReceived.Store(true)
			}
			if strings.Contains(bodyStr, "proxyResponse") {
				rpcReceived.Store(true)
			}

			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("1"))

		case "GET":
			// 2. BackChannel GET
			if r.URL.Query().Get("SID") != "mock-sid-7890" {
				t.Errorf("expected SID=mock-sid-7890, got %s", r.URL.Query().Get("SID"))
			}
			if r.URL.Query().Get("gsessionid") != "mock-gsession-xyz" {
				t.Errorf("expected gsessionid=mock-gsession-xyz, got %s", r.URL.Query().Get("gsessionid"))
			}

			flusher, ok := w.(http.Flusher)
			if !ok {
				t.Fatal("expected http.Flusher")
			}

			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.WriteHeader(http.StatusOK)
			flusher.Flush()

			// Send an incoming RPC to client via BackChannel stream
			go func() {
				time.Sleep(50 * time.Millisecond)
				inboundReq := ConnectInstanceResponseV2{
					ProxyRequest: &ProxyRequestV2{
						RequestID:   "req-101",
						RPCPath:     "/exa.language_server_pb.LanguageServerService/TestCustomRPC",
						PayloadText: `{"param":"hello"}`,
						IsFinal:     true,
					},
				}
				itemBytes, _ := json.Marshal(inboundReq)
				streamChunk := fmt.Sprintf(`[[1,[%s]]]`, string(itemBytes))
				fmt.Fprintf(w, "%d\n%s", len(streamChunk), streamChunk)
				flusher.Flush()
			}()

			<-backChanDone
		}
	}))
	defer server.Close()
	defer close(backChanDone)

	disp := &mockDispatcher{
		responsePayload: `{"status":"ok"}`,
	}

	client := NewClient(ClientOptions{
		Endpoint:     server.URL,
		InstanceName: "pigasus-test-inst",
		Dispatcher:   disp,
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	runErrCh := make(chan error, 1)
	go func() {
		runErrCh <- client.Run(ctx)
	}()

	// Wait for registration and RPC response
	deadline := time.Now().Add(1500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if regReceived.Load() && rpcReceived.Load() {
			break
		}
		time.Sleep(20 * time.Millisecond)
	}

	client.Stop()

	mu.Lock()
	if !handshakeHit {
		t.Error("handshake was not received")
	}
	if !regReceived.Load() {
		t.Error("registration forward was not received")
	}
	if !rpcReceived.Load() {
		t.Error("RPC proxyResponse forward was not received")
	}
	if disp.dispatchedMethod != "TestCustomRPC" {
		t.Errorf("expected dispatchedMethod=TestCustomRPC, got %s", disp.dispatchedMethod)
	}
	if disp.dispatchedPayload != `{"param":"hello"}` {
		t.Errorf("expected dispatchedPayload={\"param\":\"hello\"}, got %s", disp.dispatchedPayload)
	}
	// Verify monotonic ofs: ofs=0 for reg, ofs=1 for RPC response
	if len(forwardOfs) < 2 || forwardOfs[0] != "0" || forwardOfs[1] != "1" {
		t.Errorf("expected forward ofs sequence [0, 1], got %v", forwardOfs)
	}
	mu.Unlock()
}
