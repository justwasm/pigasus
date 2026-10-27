package remotecontrol

import (
	"context"
	"encoding/json"
	"testing"
	"time"
)

// TestTheSubscriptionStreamsStayOpen pins the rule the Web UI's
// subscriptions live by: a subscription stream must not end.
//
// The frontend reads the first frame of a subscription as "this is the
// current list" and every later frame as "the list changed". When the
// instance ends the stream instead, the frontend re-subscribes — and
// the re-subscription drags the whole bootstrap with it, so the page
// never settles.
//
// 【一手】 2026-10-26, live: against the clone, the two subscriptions
// that ended immediately were re-asked 176 times in ten minutes, each
// time with the skills, the workflows, the slash commands and the MCP
// prompts — 176 full bootstraps. The three streams the clone kept
// open were asked once.
func TestTheSubscriptionStreamsStayOpen(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	cases := []struct {
		method string
		field  string
	}{
		{method: "SubscribeToMcpServerStates", field: "states"},
		{method: "SubscribeToPlugins", field: "plugins"},
	}

	for _, tc := range cases {
		t.Run(tc.method, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()

			frames := make(chan json.RawMessage, 4)
			done := make(chan error, 1)
			go func() {
				handled, err := d.DispatchStream(ctx, tc.method, "{}", func(raw json.RawMessage) error {
					frames <- raw
					return nil
				})
				if !handled {
					done <- errNotAStream
					return
				}
				done <- err
			}()

			select {
			case raw := <-frames:
				var frame map[string]any
				if err := json.Unmarshal(raw, &frame); err != nil {
					t.Fatalf("Unmarshal frame: %v", err)
				}
				if _, ok := frame[tc.field]; !ok {
					t.Errorf("frame has no %q field: %s", tc.field, raw)
				}
			case <-time.After(3 * time.Second):
				t.Fatalf("%s: no frame", tc.method)
			}

			select {
			case err := <-done:
				t.Fatalf("%s: the stream ended instead of staying open: %v", tc.method, err)
			case <-time.After(300 * time.Millisecond):
			}

			cancel()
			select {
			case err := <-done:
				if err != nil {
					t.Errorf("%s: the stream ended with %v", tc.method, err)
				}
			case <-time.After(3 * time.Second):
				t.Fatalf("%s: the stream did not end when the client went away", tc.method)
			}
		})
	}
}

// errNotAStream is the marker the test uses for "the dispatcher did
// not claim the method".
var errNotAStream = errNotAStreamType{}

type errNotAStreamType struct{}

func (errNotAStreamType) Error() string { return "the dispatcher did not claim the method" }
