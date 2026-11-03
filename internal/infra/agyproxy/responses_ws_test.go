package agyproxy

import (
	"context"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/tidwall/gjson"
)

func TestResponsesWS_Warmup(t *testing.T) {
	backend := fakeCloudCodeServer(t, "unused")
	srv, err := New(Config{Upstream: backend.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/responses"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer c.CloseNow()

	// Codex warmup request: generate = false
	warmup := `{"type":"response.create","generate":false,"model":"gpt-5","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"warmup"}]}]}`
	if err := c.Write(ctx, websocket.MessageText, []byte(warmup)); err != nil {
		t.Fatalf("write warmup: %v", err)
	}

	// First frame: response.created
	_, b1, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read frame 1: %v", err)
	}
	r1 := gjson.ParseBytes(b1)
	if r1.Get("type").String() != "response.created" {
		t.Fatalf("got frame 1 type %q, want response.created (body: %s)", r1.Get("type").String(), string(b1))
	}
	respID := r1.Get("response.id").String()
	if respID == "" {
		t.Fatalf("missing response.id in warmup response.created")
	}

	// Second frame: response.completed
	_, b2, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read frame 2: %v", err)
	}
	r2 := gjson.ParseBytes(b2)
	if r2.Get("type").String() != "response.completed" {
		t.Fatalf("got frame 2 type %q, want response.completed (body: %s)", r2.Get("type").String(), string(b2))
	}
	if r2.Get("response.status").String() != "completed" {
		t.Errorf("got warmup status %q, want completed", r2.Get("response.status").String())
	}
}

func TestResponsesWS_TurnAndContinue(t *testing.T) {
	backend := fakeCloudCodeServer(t, "Hello from WebSocket Codex!")
	srv, err := New(Config{Upstream: backend.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/responses"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer c.CloseNow()

	// Turn 1
	turn1 := `{"type":"response.create","model":"gpt-5","stream":true,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`
	if err := c.Write(ctx, websocket.MessageText, []byte(turn1)); err != nil {
		t.Fatalf("write turn 1: %v", err)
	}

	var turn1ID string
	var sawCreated, sawDelta, sawCompleted bool

	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read turn 1 frames: %v", err)
		}
		r := gjson.ParseBytes(b)
		typ := r.Get("type").String()
		switch typ {
		case "response.created":
			sawCreated = true
			turn1ID = r.Get("response.id").String()
		case "response.output_text.delta":
			sawDelta = true
		case "response.completed":
			sawCompleted = true
			goto Turn1Done
		}
	}

Turn1Done:
	if !sawCreated || !sawDelta || !sawCompleted || turn1ID == "" {
		t.Fatalf("turn 1 incomplete: created=%v, delta=%v, completed=%v, id=%s", sawCreated, sawDelta, sawCompleted, turn1ID)
	}

	// Turn 2 with previous_response_id
	turn2 := fmt.Sprintf(`{"type":"response.create","model":"gpt-5","stream":true,"previous_response_id":%q,"input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"and more"}]}]}`, turn1ID)
	if err := c.Write(ctx, websocket.MessageText, []byte(turn2)); err != nil {
		t.Fatalf("write turn 2: %v", err)
	}

	sawTurn2Completed := false
	for {
		_, b, err := c.Read(ctx)
		if err != nil {
			t.Fatalf("read turn 2 frames: %v", err)
		}
		r := gjson.ParseBytes(b)
		if r.Get("type").String() == "response.completed" {
			sawTurn2Completed = true
			break
		}
	}

	if !sawTurn2Completed {
		t.Errorf("turn 2 did not complete successfully")
	}
}

func TestResponsesWS_Interrupt(t *testing.T) {
	backend := fakeCloudCodeServer(t, "Some long text")
	srv, err := New(Config{Upstream: backend.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/responses"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer c.CloseNow()

	// Sending interrupt when nothing in flight returns interrupt.failed
	interrupt := `{"type":"response.interrupt","response_id":"resp_none"}`
	if err := c.Write(ctx, websocket.MessageText, []byte(interrupt)); err != nil {
		t.Fatalf("write interrupt: %v", err)
	}

	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read interrupt reply: %v", err)
	}
	r := gjson.ParseBytes(b)
	if r.Get("type").String() != "response.interrupt.failed" {
		t.Errorf("got %q, want response.interrupt.failed", r.Get("type").String())
	}
}

func TestResponsesWS_UnknownPreviousResponseID(t *testing.T) {
	backend := fakeCloudCodeServer(t, "unused")
	srv, err := New(Config{Upstream: backend.URL})
	if err != nil {
		t.Fatal(err)
	}
	defer srv.Close()

	ts := httptest.NewServer(srv)
	defer ts.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	wsURL := "ws" + strings.TrimPrefix(ts.URL, "http") + "/v1/responses"
	c, _, err := websocket.Dial(ctx, wsURL, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer c.CloseNow()

	turn := `{"type":"response.create","model":"gpt-5","stream":true,"previous_response_id":"resp_unknown_999","input":[{"type":"message","role":"user","content":[{"type":"input_text","text":"hello"}]}]}`
	if err := c.Write(ctx, websocket.MessageText, []byte(turn)); err != nil {
		t.Fatalf("write turn: %v", err)
	}

	_, b, err := c.Read(ctx)
	if err != nil {
		t.Fatalf("read error frame: %v", err)
	}
	r := gjson.ParseBytes(b)
	if r.Get("type").String() != "error" {
		t.Fatalf("expected error frame, got: %s", string(b))
	}
	if r.Get("error.code").String() != "previous_response_not_found" {
		t.Errorf("got error code %q, want previous_response_not_found", r.Get("error.code").String())
	}
}
