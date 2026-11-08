package tui

import (
	"testing"

	"github.com/justwasm/pigasus/internal/ui/views/statusline"
)

// TestStatusLineModel_ScriptActive pins the two ways an external status
// script counts as active: the cached settings block says so, or the
// command was turned on for this session.
func TestStatusLineModel_ScriptActive(t *testing.T) {
	var s StatusLineModel
	if s.ScriptActive() {
		t.Fatal("zero model reports an active script")
	}
	s.cfg = statusline.Config{Type: "command", Command: "date", Enabled: true}
	if !s.ScriptActive() {
		t.Fatal("configured command not reported active")
	}
	s = StatusLineModel{external: true}
	if !s.ScriptActive() {
		t.Fatal("session-enabled script not reported active")
	}
}

// TestStatusLineModel_ExternalRenderOK keeps the row predicate with the
// state that decides it: the row must be on, a script must be active and
// the last run must have produced text.
func TestStatusLineModel_ExternalRenderOK(t *testing.T) {
	s := StatusLineModel{visible: "off", external: true}
	s.EnsureExternalState().SetRendered("12:00", nil)
	if s.ExternalRenderOK() {
		t.Fatal("a hidden row reported a renderable script")
	}

	s.visible = "on"
	s.external = false
	if s.ExternalRenderOK() {
		t.Fatal("unconfigured script reported renderable")
	}

	s.cfg = statusline.Config{Type: "command", Command: "date", Enabled: true}
	if !s.ExternalRenderOK() {
		t.Fatal("rendered script not reported renderable")
	}

	s.EnsureExternalState().SetRendered("   ", nil)
	if s.ExternalRenderOK() {
		t.Fatal("blank output reported renderable")
	}
}

// TestStatusLineModel_TitleStoreIsLazy checks that the store is created on
// first use and then reused, so a title written by the script is visible to
// the row.
func TestStatusLineModel_TitleStoreIsLazy(t *testing.T) {
	var s StatusLineModel
	first := s.TitleStore()
	if first == nil {
		t.Fatal("TitleStore returned nil")
	}
	first.Set("hello")
	if got := s.TitleStore().Get(); got != "hello" {
		t.Fatalf("TitleStore did not reuse the store: %q", got)
	}
}

// TestStatusLineModel_AreaState keeps the assembled facts consistent with
// the model's own predicates.
func TestStatusLineModel_AreaState(t *testing.T) {
	s := StatusLineModel{visible: "on", cfg: statusline.Config{Type: "command", Command: "date", Enabled: true}}
	s.EnsureExternalState().SetRendered("built", nil)
	area := s.AreaState("builtin")
	if area.Builtin != "builtin" || !area.Active || area.Config.Command != "date" {
		t.Fatalf("AreaState = %+v", area)
	}
	if area.State != s.ExternalState() {
		t.Fatal("AreaState did not carry the model's state")
	}
}
