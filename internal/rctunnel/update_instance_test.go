package rctunnel

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// withTestToken points the token lookup at a fixed access token for the
// duration of the test.
func withTestToken(t *testing.T, token string) {
	t.Helper()
	t.Setenv("JETSKI_OAUTH_TOKEN", token)
}

// TestUpdateInstanceName_Body pins the wire body of the rename call.
//
// 【反编译】 the reference marshals an UpdateInstanceRequest with
// protojson: instance_id (1), metadata (2), update_mask (3), in
// field-number order. A google.protobuf.FieldMask is written by
// protojson as a comma-separated string, so the mask travels as
// "metadata.name".
func TestUpdateInstanceName_Body(t *testing.T) {
	withTestToken(t, "test-access-token")

	var (
		gotPath   string
		gotMethod string
		gotCT     string
		gotAuth   string
		gotBody   string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotMethod = r.Method
		gotCT = r.Header.Get("Content-Type")
		gotAuth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		gotBody = string(body)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(ClientOptions{
		Endpoint:               server.URL,
		InstanceID:             "0d159523-6e02-4362-abab-93e0c69edea4-v2",
		UpdateInstanceEndpoint: server.URL + "/v1internal:updateInstance",
	})

	if err := client.UpdateInstanceName(context.Background(), "new-name"); err != nil {
		t.Fatalf("UpdateInstanceName: %v", err)
	}

	if gotMethod != http.MethodPost {
		t.Errorf("method = %q, want POST", gotMethod)
	}
	if gotPath != "/v1internal:updateInstance" {
		t.Errorf("path = %q, want /v1internal:updateInstance", gotPath)
	}
	if gotCT != "application/json" {
		t.Errorf("content-type = %q, want application/json", gotCT)
	}
	if gotAuth != "Bearer test-access-token" {
		t.Errorf("authorization = %q, want Bearer test-access-token", gotAuth)
	}

	want := `{"instanceId":"0d159523-6e02-4362-abab-93e0c69edea4-v2","metadata":{"name":"new-name"},"updateMask":"metadata.name"}`
	if gotBody != want {
		t.Errorf("body =\n  %s\nwant\n  %s", gotBody, want)
	}
}

// TestUpdateInstanceName_Status pins the reference's failure envelope:
// anything but HTTP 200 is "request failed with status %d: %s".
func TestUpdateInstanceName_Status(t *testing.T) {
	withTestToken(t, "test-access-token")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte("nope"))
	}))
	defer server.Close()

	client := NewClient(ClientOptions{
		Endpoint:               server.URL,
		InstanceID:             "id-1",
		UpdateInstanceEndpoint: server.URL + "/v1internal:updateInstance",
	})

	err := client.UpdateInstanceName(context.Background(), "new-name")
	if err == nil {
		t.Fatal("expected an error for HTTP 403")
	}
	if !strings.Contains(err.Error(), "request failed with status 403") {
		t.Errorf("error = %q, want it to name the status", err.Error())
	}
	if !strings.Contains(err.Error(), "nope") {
		t.Errorf("error = %q, want it to carry the body", err.Error())
	}
}

// TestUpdateInstanceName_EmptyName pins the reference's own guard.
func TestUpdateInstanceName_EmptyName(t *testing.T) {
	withTestToken(t, "test-access-token")

	client := NewClient(ClientOptions{Endpoint: "http://127.0.0.1:1", InstanceID: "id-1"})
	err := client.UpdateInstanceName(context.Background(), "   ")
	if err == nil {
		t.Fatal("expected an error for an empty name")
	}
	if err.Error() != "newName is empty" {
		t.Errorf("error = %q, want %q", err.Error(), "newName is empty")
	}
}

// TestUpdateInstanceName_DerivesEndpointFromTunnel pins the derivation:
// the rename goes to the tunnel's own host, with the update RPC's
// path. That is what keeps a local mock answering it.
func TestUpdateInstanceName_DerivesEndpointFromTunnel(t *testing.T) {
	withTestToken(t, "test-access-token")

	var gotPath string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(ClientOptions{
		Endpoint:   server.URL + "/v1internal:connectInstanceV2",
		InstanceID: "id-1",
	})

	if err := client.UpdateInstanceName(context.Background(), "new-name"); err != nil {
		t.Fatalf("UpdateInstanceName: %v", err)
	}
	if gotPath != "/v1internal:updateInstance" {
		t.Errorf("path = %q, want /v1internal:updateInstance", gotPath)
	}
}

// TestUpdateInstanceName_BodyIsProtoJSON guards the shape of the
// request against a silent drift: the body has to be a JSON object
// with exactly the three members the reference's proto declares.
func TestUpdateInstanceName_BodyIsProtoJSON(t *testing.T) {
	withTestToken(t, "test-access-token")

	var raw map[string]any
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &raw); err != nil {
			t.Errorf("body is not JSON: %v", err)
		}
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := NewClient(ClientOptions{
		Endpoint:               server.URL,
		InstanceID:             "id-1",
		UpdateInstanceEndpoint: server.URL + "/v1internal:updateInstance",
	})
	if err := client.UpdateInstanceName(context.Background(), "new-name"); err != nil {
		t.Fatalf("UpdateInstanceName: %v", err)
	}
	for _, key := range []string{"instanceId", "metadata", "updateMask"} {
		if _, ok := raw[key]; !ok {
			t.Errorf("body is missing %q: %v", key, raw)
		}
	}
	meta, _ := raw["metadata"].(map[string]any)
	if meta["name"] != "new-name" {
		t.Errorf("metadata.name = %v, want new-name", meta["name"])
	}
	if raw["updateMask"] != "metadata.name" {
		t.Errorf("updateMask = %v, want metadata.name", raw["updateMask"])
	}
}
