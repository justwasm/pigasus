package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
)

// TestWalkRemoteWorkspace pins the @ picker's remote listing: it recurses
// through readDir, keeps directories as "dir/" entries, and skips the same
// noise directories the local scan skips.
func TestWalkRemoteWorkspace(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(r.URL.Path, ":readDir") {
			http.NotFound(w, r)
			return
		}
		var body struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		var entries []remoteagentclient.DirEntry
		switch body.Path {
		case "/ws":
			entries = []remoteagentclient.DirEntry{
				{Name: "go.mod"},
				{Name: "cmd", IsDir: true},
				{Name: ".git", IsDir: true},
				{Name: ".DS_Store"},
			}
		case "/ws/cmd":
			entries = []remoteagentclient.DirEntry{{Name: "main.go"}}
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": entries})
	}))
	defer srv.Close()

	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: srv.URL})
	entries, err := walkRemoteWorkspace(context.Background(), client, "/ws")
	if err != nil {
		t.Fatalf("walkRemoteWorkspace: %v", err)
	}

	got := make([]string, 0, len(entries))
	for _, e := range entries {
		rel := e.Rel
		if e.IsDir {
			rel += "(dir)"
		}
		got = append(got, rel)
	}
	sort.Strings(got)
	want := []string{"cmd/(dir)", "cmd/main.go", "go.mod"}
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Errorf("entries = %v, want %v", got, want)
	}
}

// TestMentionListUsesRemoteCache pins the host seam: a remote-agent RootModel
// resolves the picker's lister to the readDir-backed cache, and the cache
// fills asynchronously without blocking the caller.
func TestMentionListUsesRemoteCache(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Path string `json:"path"`
		}
		_ = json.NewDecoder(r.Body).Decode(&body)
		entries := []remoteagentclient.DirEntry{{Name: "go.mod"}}
		_ = json.NewEncoder(w).Encode(map[string]any{"entries": entries})
	}))
	defer srv.Close()

	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: srv.URL})
	m := &RootModel{
		session: sessionState{remoteAgent: true, cwd: "/ws"},
		remote:  RemoteModel{files: newRemoteFileCache(client)},
	}
	list := m.MentionList()
	if got, err := list("/ws"); err != nil || got != nil {
		t.Fatalf("first List = (%v, %v), want an async miss", got, err)
	}

	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		got, err := list("/ws")
		if err != nil {
			t.Fatalf("List: %v", err)
		}
		if len(got) > 0 {
			if got[0].Rel != "go.mod" {
				t.Fatalf("entries = %+v", got)
			}
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("remote listing never became available")
}
