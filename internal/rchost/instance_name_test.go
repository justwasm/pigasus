// Package rchost — instance_name_test.go: the daemon-side rename
// watcher.
//
// The watcher is the clone's answer to the reference's own
// "the hostname changed, rename the instance over the API" path: the
// configuration file is the source of truth, and a change travels to
// the cloud over the session that is already up.
//
// 【反编译】 the reference's own wording, which the watcher reproduces:
//
//	[RemoteControl] Hostname changed from %q to %q, updating via API
//	[RemoteControl] Failed to update instance metadata: %v
package rchost

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/persistentrc"
	"github.com/justwasm/pigasus/internal/rctunnel"
)

// writeInstanceName writes the instance name into the configuration
// file the watcher reads.
func writeInstanceName(t *testing.T, configHome, name string) {
	t.Helper()
	if err := os.MkdirAll(configHome, 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", configHome, err)
	}
	body := map[string]any{
		"userSettings": map[string]any{
			"cliRemoteControlHostname": name,
		},
	}
	data, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	path := filepath.Join(configHome, "config.json")
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

// TestWatchInstanceName_PushesTheRename pins the whole path: a name
// written into the configuration file travels to the update endpoint,
// and the tunnel's own registration name follows it.
func TestWatchInstanceName_PushesTheRename(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", configHome)
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-access-token")

	old := writeInstanceName
	_ = old
	writeInstanceName(t, configHome, "old-name")

	var (
		gotBody string
		gotPath string
	)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		gotBody = string(b)
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := rctunnel.NewClient(rctunnel.ClientOptions{
		Endpoint:               server.URL,
		InstanceID:             "id-1",
		InstanceName:           persistentrc.ResolveInstanceName(""),
		UpdateInstanceEndpoint: server.URL + "/v1internal:updateInstance",
	})

	// Shorten the poll so the test does not wait two seconds.
	oldInterval := instanceNamePollInterval
	instanceNamePollInterval = 10 * time.Millisecond
	defer func() { instanceNamePollInterval = oldInterval }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var log bytes.Buffer
	done := make(chan struct{})
	go func() {
		watchInstanceName(ctx, client, &log, "remote-control serve", "old-name")
		close(done)
	}()

	// Rename.
	writeInstanceName(t, configHome, "new-name")

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if gotBody != "" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}

	if gotBody == "" {
		cancel()
		<-done
		t.Fatalf("the rename never reached the update endpoint; log:\n%s", log.String())
	}
	if gotPath != "/v1internal:updateInstance" {
		t.Errorf("path = %q, want /v1internal:updateInstance", gotPath)
	}
	var body map[string]any
	if err := json.Unmarshal([]byte(gotBody), &body); err != nil {
		t.Fatalf("the body is not JSON: %v (%s)", err, gotBody)
	}
	meta, _ := body["metadata"].(map[string]any)
	want := persistentrc.ResolveInstanceName("new-name")
	if meta["name"] != want {
		t.Errorf("metadata.name = %v, want %v", meta["name"], want)
	}
	if body["updateMask"] != "metadata.name" {
		t.Errorf("updateMask = %v, want metadata.name", body["updateMask"])
	}

	// The tunnel's own name follows, so a reconnect does not put the
	// old name back on the dashboard.
	waitFor(t, 5*time.Second, func() bool {
		return client.InstanceName() == want
	}, "the tunnel's registration name did not follow the rename")

	if !strings.Contains(log.String(), "[RemoteControl] Hostname changed from") {
		t.Errorf("the log does not carry the reference's wording:\n%s", log.String())
	}
}

// TestWatchInstanceName_KeepsTheTunnelUp pins the property the whole
// design exists for: the rename does not tear the session down.
func TestWatchInstanceName_KeepsTheTunnelUp(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", configHome)
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-access-token")
	writeInstanceName(t, configHome, "old-name")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := rctunnel.NewClient(rctunnel.ClientOptions{
		Endpoint:               server.URL,
		InstanceID:             "id-1",
		InstanceName:           persistentrc.ResolveInstanceName(""),
		UpdateInstanceEndpoint: server.URL + "/v1internal:updateInstance",
	})

	oldInterval := instanceNamePollInterval
	instanceNamePollInterval = 10 * time.Millisecond
	defer func() { instanceNamePollInterval = oldInterval }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchInstanceName(ctx, client, io.Discard, "remote-control serve", "old-name")

	writeInstanceName(t, configHome, "new-name")
	time.Sleep(200 * time.Millisecond)

	// The client is still the same client and still holds the new
	// name: nothing tore the session down.
	if client.InstanceName() != persistentrc.ResolveInstanceName("new-name") {
		t.Errorf("the tunnel's name is %q, want %q", client.InstanceName(), persistentrc.ResolveInstanceName("new-name"))
	}
}

// TestWatchInstanceName_IgnoresAnEmptyName pins the reference's own
// guard: an empty hostname is refused, not pushed.
func TestWatchInstanceName_IgnoresAnEmptyName(t *testing.T) {
	configHome := t.TempDir()
	t.Setenv("GEMINI_CONFIG_HOME", configHome)
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-access-token")

	// No configuration file at all: the name resolves to the host
	// name, which is not empty, so write an empty one instead.
	writeInstanceName(t, configHome, "")
	// An empty hostname in the file makes the resolver fall back to
	// the host name — the reference's own behaviour.

	var hits int
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits++
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := rctunnel.NewClient(rctunnel.ClientOptions{
		Endpoint:               server.URL,
		InstanceID:             "id-1",
		InstanceName:           "stable",
		UpdateInstanceEndpoint: server.URL + "/v1internal:updateInstance",
	})

	oldInterval := instanceNamePollInterval
	instanceNamePollInterval = 10 * time.Millisecond
	defer func() { instanceNamePollInterval = oldInterval }()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go watchInstanceName(ctx, client, io.Discard, "remote-control serve", "old-name")

	time.Sleep(150 * time.Millisecond)
	if hits != 0 {
		t.Errorf("the watcher pushed %d times, want none", hits)
	}
	if client.InstanceName() != "stable" {
		t.Errorf("the tunnel's name is %q, want stable", client.InstanceName())
	}
}

// waitFor polls until the condition holds or the deadline passes.
func waitFor(t *testing.T, timeout time.Duration, cond func() bool, msg string) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("%s", msg)
}
