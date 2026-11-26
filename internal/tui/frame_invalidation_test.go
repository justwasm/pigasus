package tui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
)

func TestComposerEditsPreserveTranscriptWrap(t *testing.T) {
	m := warmModel(40, 240)
	m.prompt.input.Focus()
	for _, key := range []tea.Key{
		{Code: 'x', Text: "x"}, {Code: '中', Text: "中"}, {Code: tea.KeyLeft}, {Code: tea.KeyRight},
		{Code: tea.KeyBackspace}, {Code: tea.KeyHome}, {Code: tea.KeyEnd},
	} {
		_ = m.View()
		m.viewport.rowsCacheDirty = false
		_, _ = m.Update(tea.KeyPressMsg(key))
		if m.viewport.rowsCacheDirty {
			t.Fatalf("composer key %+v dirtied transcript", key)
		}
		warm := m.View()
		m.viewport.rowsCacheDirty = true
		m.invalidateViewCache()
		cold := m.View()
		if warm.Content != cold.Content {
			t.Fatalf("key %+v changed warm vs cold ANSI", key)
		}
		if (warm.Cursor == nil) != (cold.Cursor == nil) || (warm.Cursor != nil && *warm.Cursor != *cold.Cursor) {
			t.Fatalf("key %+v changed cursor", key)
		}
	}
	if !strings.Contains(ansi.Strip(m.View().Content), "x") {
		t.Fatal("typed composer text absent from rendered frame")
	}
}

func TestComposerCacheDoesNotTreatCustomCommandsAsEdits(t *testing.T) {
	m := warmModel(10, 100)
	m.prompt.input.Focus()
	path := filepath.Join(t.TempDir(), "keybindings.json")
	if err := os.WriteFile(path, []byte(`{"view.toggle_trajectory":["x"]}`), 0600); err != nil {
		t.Fatal(err)
	}
	kb, err := keybind.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	m.prompt.keybindings = kb
	// Even when navigation or a printable key is bound to a conversation action,
	// do not assume that key only edits the composer.
	for _, key := range []tea.Key{{Code: tea.KeyEnter}, {Code: tea.KeyEsc}, {Code: 'o', Mod: tea.ModCtrl}, {Code: 'x', Text: "x"}} {
		if m.composerOnlyMsg(tea.KeyPressMsg(key)) {
			t.Fatalf("command key %+v treated as edit", key)
		}
	}
}

func BenchmarkCPUComposerFrame(b *testing.B) {
	for _, steps := range []int{100, 1000, 5000} {
		b.Run(fmt.Sprint(steps), func(b *testing.B) {
			m := warmModel(steps, 200)
			m.prompt.input.Focus()
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				_, _ = m.Update(tea.KeyPressMsg{Code: tea.KeyLeft})
				_ = m.View()
			}
		})
	}
}
