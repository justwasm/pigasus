package tui

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/features/tools/skills"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
)

// TestRemoteSkillsReachTheSkillsSurfaces pins the provider path: the
// engine's cached ListCustomizations skills become /skills rows and / menu
// commands, and /skills reload refetches.
func TestRemoteSkillsReachTheSkillsSurfaces(t *testing.T) {
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case strings.HasSuffix(r.URL.Path, ":listCustomizations"):
			n := atomic.AddInt32(&calls, 1)
			sk := map[string]any{"name": "remote-skill", "description": "does remote things"}
			if n > 1 {
				sk = map[string]any{"name": "reloaded-skill", "description": "after reload"}
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"skills": []any{sk}})
		case strings.HasSuffix(r.URL.Path, ":listSlashCommands"):
			_ = json.NewEncoder(w).Encode(map[string]any{"slashCommands": []any{}})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: srv.URL})
	engine := agentturn.NewRemoteAgentEngine(agentturn.RemoteAgentConfig{Client: client, Model: "m"})
	if _, err := engine.Skills(context.Background()); err != nil {
		t.Fatalf("Skills: %v", err)
	}
	skills.SetRemoteSkillsProvider(engine.CachedSkills)
	defer skills.SetRemoteSkillsProvider(nil)

	found := false
	for _, s := range skills.Discover() {
		if s.Name == "remote-skill" {
			found = true
			if !s.Remote || s.Description != "does remote things" {
				t.Errorf("skill = %+v", s)
			}
		}
	}
	if !found {
		t.Fatal("remote skill missing from Discover")
	}

	found = false
	host := remoteHost(engine)
	for _, cmd := range host.engineSlashCommands() {
		if cmd.Name == "/remote-skill" {
			found = true
			if cmd.Badge != "remote" {
				t.Errorf("badge = %q", cmd.Badge)
			}
		}
	}
	if !found {
		t.Error("remote skill missing from the slash menu")
	}

	host.slashSkills("reload")
	reloaded := false
	for _, s := range skills.Discover() {
		if s.Name == "reloaded-skill" {
			reloaded = true
		}
	}
	if !reloaded {
		t.Errorf("/skills reload did not refetch; calls=%d", atomic.LoadInt32(&calls))
	}
}

// remoteHost builds the host around a remote engine.
func remoteHost(engine *agentturn.RemoteAgentEngine) *RootModel {
	return &RootModel{session: sessionState{remoteAgent: true}, turn: TurnService{engine: engine}}
}
