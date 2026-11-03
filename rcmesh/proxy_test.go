package rcmesh

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"testing"
)

func TestHTTPBridgeWireAndBinaryPayload(t *testing.T) {
	for _, payload := range [][]byte{[]byte("hello"), {0xff, 0, 1}, bytes.Repeat([]byte("large"), 10000)} {
		bridge := HTTPBridge{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Header.Get("X-Jetski-Remote-Control-Transport") != "datachannel" || r.Header.Get("X-Jetski-Via-Remote-Control") != "true" {
				t.Error("missing transport identity")
			}
			w.Header().Set("Content-Type", "application/proto")
			w.WriteHeader(201)
			_, _ = w.Write(payload)
		})}
		request, _ := json.Marshal(RequestEnvelope{ProxyRequest: &ProxyRequest{RequestID: "9007199254740993", RPCPath: "/exa.language_server_pb.LanguageServerService/GetStatus"}})
		var frames []ProxyResponse
		err := bridge.Handle(context.Background(), request, func(data []byte) error {
			var e ResponseEnvelope
			if err := json.Unmarshal(data, &e); err != nil {
				return err
			}
			frames = append(frames, *e.ProxyResponse)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var got []byte
		for i, f := range frames {
			if f.RequestID != "9007199254740993" || f.StatusCode != 201 {
				t.Fatalf("bad response %+v", f)
			}
			if f.IsFinal != (i == len(frames)-1) {
				t.Fatal("incorrect final frame")
			}
			got = append(got, f.Payload...)
			got = append(got, f.PayloadText...)
		}
		if !bytes.Equal(got, payload) {
			t.Fatal("response payload changed")
		}
	}
}
func TestHTTPBridgeRejectsUnsafeRoutes(t *testing.T) {
	called := false
	bridge := HTTPBridge{http.HandlerFunc(func(http.ResponseWriter, *http.Request) { called = true })}
	for _, path := range []string{"https://example.com/", "/../GetStatus", "/exa.language_server_pb.LanguageServerService/../GetStatus", "/exa.language_server_pb.OtherService/GetStatus", "/exa.language_server_pb.LanguageServerService/GetStatus?x=y"} {
		request, _ := json.Marshal(RequestEnvelope{ProxyRequest: &ProxyRequest{RequestID: "1", RPCPath: path}})
		if err := bridge.Handle(context.Background(), request, func([]byte) error { return nil }); err == nil {
			t.Fatalf("accepted %q", path)
		}
	}
	if called {
		t.Fatal("unsafe route reached HTTP handler")
	}
}
func TestHTTPBridgePropagatesSenderFailure(t *testing.T) {
	failure := errors.New("closed data channel")
	bridge := HTTPBridge{http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("a")); _, _ = w.Write([]byte("b")) })}
	request := []byte(`{"proxyRequest":{"requestId":"1","rpcPath":"/exa.language_server_pb.LanguageServerService/GetStatus"}}`)
	if err := bridge.Handle(context.Background(), request, func([]byte) error { return failure }); !errors.Is(err, failure) {
		t.Fatalf("lost send failure: %v", err)
	}
}

func TestDecodeRequestRejectsIncompleteAndTrailingFrames(t *testing.T) {
	valid := `{"proxyRequest":{"requestId":"1","rpcPath":"/exa.language_server_pb.LanguageServerService/GetStatus"}}`
	for _, frame := range []string{valid + `{}`, valid + ` garbage`, `{"proxyRequest":{"requestId":"1","isFinal":false}}`, `{"proxyRequest":{"requestId":1}}`, `{"proxyRequest":{"requestId":"1","payload":"","payloadText":""}}`} {
		if _, err := DecodeRequest([]byte(frame)); err == nil {
			t.Fatalf("accepted %s", frame)
		}
	}
	if _, err := DecodeRequest([]byte(valid)); err != nil {
		t.Fatal(err)
	}
	cancel, err := DecodeRequest([]byte(`{"proxyRequest":{"requestId":"1","headers":{"X-Cancel-Request":"true"}}}`))
	if err != nil || !IsCancelRequest(cancel) {
		t.Fatalf("bad cancellation: %+v %v", cancel, err)
	}
}

func TestResponseBodyOneofPresence(t *testing.T) {
	for _, test := range []struct {
		response ProxyResponse
		want     string
	}{
		{ProxyResponse{RequestID: "1", StatusCode: 200, IsFinal: true}, `{"requestId":"1","statusCode":200,"isFinal":true,"payloadText":""}`},
		{ProxyResponse{RequestID: "1", StatusCode: 200, Payload: []byte{255}}, `{"requestId":"1","statusCode":200,"payload":"/w=="}`},
	} {
		got, err := json.Marshal(test.response)
		if err != nil || string(got) != test.want {
			t.Fatalf("body presence changed: %s, %v", got, err)
		}
	}
}

// 【反编译】 errorResponseV2 (0x1025ac8f0) builds a ConnectInstanceRequestV2
// whose ProxyResponseV2 carries the request id, the status code, is_final true
// and the message as payload_text; sendErrorResponse (0x1025b22a0) marshals it
// with protojson.
func TestErrorResponseWireShape(t *testing.T) {
	frame, err := ErrorResponse("42", 400, "orphaned request chunk without active reassembly buffer")
	if err != nil {
		t.Fatal(err)
	}
	want := `{"proxyResponse":{"requestId":"42","statusCode":400,"isFinal":true,` +
		`"payloadText":"orphaned request chunk without active reassembly buffer"}}`
	if string(frame) != want {
		t.Fatalf("error frame = %s, want %s", frame, want)
	}
	var envelope ResponseEnvelope
	if err := json.Unmarshal(frame, &envelope); err != nil {
		t.Fatal(err)
	}
	if envelope.ProxyResponse == nil || envelope.ProxyResponse.StatusCode != 400 {
		t.Fatalf("bad envelope: %+v", envelope)
	}
}
