package remotecontrol_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/lsproto"
	"github.com/justwasm/pigasus/remotetransport"
)

type grpcStatusCoder interface {
	GRPCStatus() int
}

func TestLanguageServer23StreamingMethodsParity(t *testing.T) {
	codec, err := lsproto.Shared()
	if err != nil {
		t.Fatalf("lsproto.Shared: %v", err)
	}
	allMethods := codec.ServerStreamingMethods()
	if len(allMethods) != 23 {
		t.Fatalf("expected 23 server-streaming methods in descriptor, got %d: %v", len(allMethods), allMethods)
	}

	d, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session: remotecontrol.NewSessionState("test"),
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	expectedFailures := map[string]struct {
		grpcStatus int
		msgPrefix  string
	}{
		"HandleStreamingCommand":                {2, "unexpected request source"},
		"StreamCascadeReactiveUpdates":          {2, "reactive state is deprecated"},
		"StreamCascadeSummariesReactiveUpdates": {2, "Summaries reactive state is deprecated"},
		"JetboxSubscribeToOAuthState":           {2, "JetboxSubscribeToOAuthState not supported in external builds"},
		"StreamUserTrajectoryReactiveUpdates":   {12, "unimplemented: user trajectory reactive updates are deprecated"},
		"StreamCascadePanelReactiveUpdates":     {12, "unimplemented: unimplemented"},
		"StartNarration":                        {12, "unimplemented: narration is not available in external builds"},
		"SetupJetskiChat":                       {12, "unimplemented: Unimplemented"},
		"StreamTerminalOutput":                  {5, "terminal  not found"},
		"StreamAudioTranscription":              {13, "internal: failed to receive setup response"},
	}

	for _, method := range allMethods {
		t.Run(method, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			type streamResult struct {
				handled bool
				err     error
			}
			resCh := make(chan streamResult, 1)

			var mu sync.Mutex
			var frames []json.RawMessage

			go func() {
				handled, err := d.DispatchStream(ctx, method, "{}", func(raw json.RawMessage) error {
					mu.Lock()
					frames = append(frames, raw)
					mu.Unlock()
					return nil
				})
				resCh <- streamResult{handled: handled, err: err}
			}()

			if exp, isFail := expectedFailures[method]; isFail {
				select {
				case res := <-resCh:
					if !res.handled {
						t.Fatalf("%s: expected handled=true, got false", method)
					}
					if res.err == nil {
						t.Fatalf("%s: expected failure, got nil error", method)
					}
					var gsc grpcStatusCoder
					if !errors.As(res.err, &gsc) {
						t.Fatalf("%s: error %v does not implement grpcStatusCoder", method, res.err)
					}
					if gsc.GRPCStatus() != exp.grpcStatus {
						t.Errorf("%s: got gRPC status %d, want %d", method, gsc.GRPCStatus(), exp.grpcStatus)
					}
					if !strings.HasPrefix(res.err.Error(), exp.msgPrefix) {
						t.Errorf("%s: error message %q does not start with %q", method, res.err.Error(), exp.msgPrefix)
					}
				case <-time.After(500 * time.Millisecond):
					t.Fatalf("%s: failure stream did not end immediately", method)
				}
				return
			}

			if method == "StreamSearchCode" {
				select {
				case res := <-resCh:
					if !res.handled {
						t.Fatalf("StreamSearchCode: handled=false")
					}
					if res.err != nil {
						t.Errorf("StreamSearchCode: unexpected error %v", res.err)
					}
					mu.Lock()
					count := len(frames)
					mu.Unlock()
					if count != 0 {
						t.Errorf("StreamSearchCode: expected 0 frames, got %d", count)
					}
				case <-time.After(500 * time.Millisecond):
					t.Fatalf("StreamSearchCode did not complete immediately")
				}
				return
			}

			if method == "WatchDirectory" || method == "SubscribeToAgentBrowserRequests" || method == "StreamAgentStateUpdates" {
				select {
				case res := <-resCh:
					t.Fatalf("%s: stream ended immediately instead of staying open: %+v", method, res)
				case <-time.After(100 * time.Millisecond):
				}
				mu.Lock()
				count := len(frames)
				mu.Unlock()
				if count != 0 {
					t.Errorf("%s: expected held-open stream with 0 frames (empty request), got %d", method, count)
				}
			} else {
				// Expect at least 1 frame before cancel
				deadline := time.Now().Add(1 * time.Second)
				for time.Now().Before(deadline) {
					mu.Lock()
					count := len(frames)
					mu.Unlock()
					if count > 0 {
						break
					}
					select {
					case res := <-resCh:
						t.Fatalf("%s: stream ended prematurely: %+v", method, res)
					case <-time.After(10 * time.Millisecond):
					}
				}
				mu.Lock()
				count := len(frames)
				mu.Unlock()
				if count == 0 {
					t.Errorf("%s: expected message-then-open stream with at least 1 frame, got 0", method)
				}
			}

			// Clean up by cancelling context and checking stream exits
			cancel()
			select {
			case res := <-resCh:
				if !res.handled {
					t.Errorf("%s: handled=false", method)
				}
				if res.err != nil {
					t.Errorf("%s: exit error after cancel: %v", method, res.err)
				}
			case <-time.After(500 * time.Millisecond):
				t.Errorf("%s: stream did not exit after context cancel", method)
			}
		})
	}
}

func TestTCPServer_StreamingErrorWireParity(t *testing.T) {
	d, err := remotecontrol.NewDispatcher(remotecontrol.Config{
		Session: remotecontrol.NewSessionState("test"),
	})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}
	srv, err := remotetransport.NewTCPServer(context.Background(), d, remotetransport.TCPConfig{
		Addr: "127.0.0.1:0",
		WebUIHandler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
		}),
	})
	if err != nil {
		t.Fatalf("NewTCPServer: %v", err)
	}
	defer srv.Shutdown(context.Background())

	// 1. gRPC-Web test: StreamCascadeReactiveUpdates returns trailers-only headers with Grpc-Status: 2
	{
		url := "http://" + srv.Addr().String() + "/exa.language_server_pb.LanguageServerService/StreamCascadeReactiveUpdates"
		req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader("\x00\x00\x00\x00\x02{}"))
		req.Header.Set("Content-Type", "application/grpc-web+json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST gRPC-Web: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		if got := resp.Header.Get("Grpc-Status"); got != "2" {
			t.Errorf("Grpc-Status = %q, want \"2\"", got)
		}
		if msg := resp.Header.Get("Grpc-Message"); !strings.Contains(msg, "reactive state is deprecated") {
			t.Errorf("Grpc-Message = %q, want containing \"reactive state is deprecated\"", msg)
		}
	}

	// 2. Connect test: StreamCascadeReactiveUpdates returns end-of-stream envelope with code "unknown"
	{
		url := "http://" + srv.Addr().String() + "/exa.language_server_pb.LanguageServerService/StreamCascadeReactiveUpdates"
		req, _ := http.NewRequest(http.MethodPost, url, strings.NewReader("\x00\x00\x00\x00\x02{}"))
		req.Header.Set("Content-Type", "application/connect+json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("POST Connect: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Errorf("status = %d, want 200", resp.StatusCode)
		}
		body := make([]byte, 512)
		n, _ := resp.Body.Read(body)
		body = body[:n]
		if len(body) < 5 || body[0] != 0x02 {
			t.Errorf("expected end-of-stream envelope (flag 0x02), got len=%d body=%q", len(body), body)
		}
		if !strings.Contains(string(body), `"unknown"`) || !strings.Contains(string(body), "reactive state is deprecated") {
			t.Errorf("body %s does not contain code unknown or message", string(body))
		}
	}
}
