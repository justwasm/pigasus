package rcmesh

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestSignalingClientWire(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" || r.Header.Get("Content-Type") != "application/json" {
			t.Error("bad request")
		}
		var body map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Error(err)
		}
		if r.URL.Path == "/poll" {
			if string(body["instanceId"]) != `"host"` || string(body["includeAuthorizedKeys"]) != "true" {
				t.Error("wrong poll wire")
			}
			w.Write([]byte(`{"authorizedKeyBundle":{"delivered":true,"webCryptoPubKeys":["key"]},"messages":[{"sessionId":"s","payload":"{}"}]}`))
			return
		}
		if string(body["payload"]) != `"{\"payload_type\":\"offer\",\"payload\":{}}"` {
			t.Errorf("wrong nested payload %s", body["payload"])
		}
		w.Write([]byte(`{}`))
	}))
	defer server.Close()
	client := SignalingClient{PollURL: server.URL + "/poll", SendURL: server.URL + "/send"}
	response, err := client.Poll(context.Background(), PollRequest{InstanceID: "host", IncludeAuthorizedKeys: true})
	if err != nil || response.AuthorizedKeyBundle == nil || !response.AuthorizedKeyBundle.Delivered || len(response.Messages) != 1 {
		t.Fatalf("%+v %v", response, err)
	}
	if err := client.Send(context.Background(), SendRequest{SessionID: "s", Payload: `{"payload_type":"offer","payload":{}}`}); err != nil {
		t.Fatal(err)
	}
	for _, request := range []PollRequest{{}, {SessionID: "s", InstanceID: "i"}, {InstanceID: "i", AfterTimestamp: "bad"}} {
		if _, err := client.Poll(context.Background(), request); err == nil {
			t.Fatal("invalid target accepted")
		}
	}
}
func TestSignalingClientRejectsRedirectAndErrors(t *testing.T) {
	for _, status := range []int{302, 401, 500} {
		server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Location", "/credentials-must-not-follow")
			w.WriteHeader(status)
		}))
		_, err := (SignalingClient{PollURL: server.URL}).Poll(context.Background(), PollRequest{InstanceID: "i"})
		server.Close()
		if err == nil {
			t.Fatalf("accepted status %d", status)
		}
	}
}

func TestSignalingClientBearerToken(t *testing.T) {
	var gotAuth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{}`))
	}))
	defer server.Close()

	client := SignalingClient{
		PollURL: server.URL,
		TokenFunc: func(ctx context.Context) (string, error) {
			return "secret-token-123", nil
		},
	}
	_, err := client.Poll(context.Background(), PollRequest{InstanceID: "my-instance"})
	if err != nil {
		t.Fatal(err)
	}
	if gotAuth != "Bearer secret-token-123" {
		t.Errorf("Authorization = %q, want Bearer secret-token-123", gotAuth)
	}
}
