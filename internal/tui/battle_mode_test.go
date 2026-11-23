package tui

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/features/perfrecorder"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/palette"
)

func TestSlashBattle_UsageAndOverlayLifecycle(t *testing.T) {
	origScheme, _ := palette.CurrentScheme()
	origMdScheme := mdrender.CurrentColorScheme()
	t.Cleanup(func() {
		palette.SetScheme(origScheme)
		mdrender.SetColorScheme(origMdScheme)
	})
	t.Setenv("HOME", t.TempDir())
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "hello.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	m := newRootModel(&fakeClient{}, "u@t", "m", "t", dir, true)
	m.session.cwd = dir
	m.viewport.width = 100
	m.viewport.height = 30

	// 1. Bare /battle without active session prints usage.
	handled, _ := m.handleSlashCommand("/battle")
	if !handled {
		t.Fatal("/battle was not handled")
	}
	if len(m.conversation.response) == 0 || !strings.Contains(strings.Join(m.conversation.response, "\n"), "Usage: /battle") {
		t.Fatalf("expected usage message, got %v", m.conversation.response)
	}

	// 2. /battle <modelA> <modelB> <task> prepares workspaces and opens the overlay.
	handled, cmd := m.handleSlashCommand("/battle flash pro fix hello.txt")
	if !handled {
		t.Fatal("/battle with args was not handled")
	}
	if cmd == nil {
		t.Fatal("expected tick cmd when starting battle")
	}
	if m.agents.battle.Panel == nil || !m.agents.battle.Panel.Active() {
		t.Fatal("expected battle panel to be active")
	}

	view := m.View()
	if !strings.Contains(view.Content, "Battle Mode") {
		t.Fatalf("expected Battle Mode in rendered view, got:\n%s", view.Content)
	}
	if !strings.Contains(view.Content, "fix hello.txt") {
		t.Fatalf("expected task in rendered view, got:\n%s", view.Content)
	}

	// 3. Esc closes the battle overlay without clearing composer or leaking.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.agents.battle.Panel.Active() {
		t.Fatal("expected Esc to close battle panel")
	}

	// 4. /battle cleanup cleans up workspaces.
	handled, _ = m.handleSlashCommand("/battle cleanup")
	if !handled {
		t.Fatal("/battle cleanup was not handled")
	}
}

func TestRootModel_PerfRecorderTracksUpdateAndView(t *testing.T) {
	origScheme, _ := palette.CurrentScheme()
	origMdScheme := mdrender.CurrentColorScheme()
	t.Cleanup(func() {
		palette.SetScheme(origScheme)
		mdrender.SetColorScheme(origMdScheme)
	})
	t.Setenv("HOME", t.TempDir())
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.viewport.width = 80
	m.viewport.height = 24
	m.display.perf = perfrecorder.New(perfrecorder.WithCapacity(64), perfrecorder.WithEnabled(true))

	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	_ = m.View()

	sum := m.display.perf.Summary()
	if sum.Update.Count == 0 {
		t.Fatalf("expected Update count > 0, got %d", sum.Update.Count)
	}
	if sum.View.Count == 0 {
		t.Fatalf("expected View count > 0, got %d", sum.View.Count)
	}
}
