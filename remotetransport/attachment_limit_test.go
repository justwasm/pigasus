package remotetransport

import (
	"bytes"
	"net/http"
	"testing"
)

func TestTCPServer_Accepts20MBAttachmentPayload(t *testing.T) {
	srv, cleanup := newTestRPCServer(t)
	defer cleanup()

	// 20 MB binary payload
	twentyMB := 20 * 1024 * 1024
	body := bytes.Repeat([]byte("a"), twentyMB)

	addr := srv.Addr().String()
	req, err := http.NewRequest(http.MethodPost, "http://"+addr+"/rpc", bytes.NewReader(body))
	if err != nil {
		t.Fatalf("create req: %v", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", "https://antigravity.google.com")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("POST failed: %v", err)
	}
	defer resp.Body.Close()

	// The status must NOT be 413 StatusRequestEntityTooLarge.
	if resp.StatusCode == http.StatusRequestEntityTooLarge {
		t.Fatalf("got 413 StatusRequestEntityTooLarge; want 20MB payload accepted by transport")
	}
}
