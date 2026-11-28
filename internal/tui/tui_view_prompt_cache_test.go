package tui

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/features/models/configtest"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

func promptFrameTestModel(t *testing.T) *RootModel {
	t.Helper()
	configtest.Isolate(t)
	m := newRootModel(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "", "/tmp", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 64})
	m.viewport.extraRowsValid = false
	m.viewport.viewCacheValid = false
	return m
}

// renderPromptColdView reproduces the pre-cache View path: measuring the body
// in viewportHeight formats the textarea once, then the final body formats it
// again. It returns the real tea.View so ANSI output and hardware cursor can
// be compared rather than inferred from model state.
func renderPromptColdView(m *RootModel) tea.View {
	restore := palette.PushScheme(m.viewport.colorScheme)
	defer restore()

	m.viewport.viewCacheValid = false
	m.viewport.extraRowsValid = false
	m.viewport.promptFrameActive = false
	m.viewport.preparedInputView = ""
	m.viewport.preparedInputViewValid = false
	var transcript []string
	if m.viewport.height > 0 {
		transcript = m.transcriptWindow()
	} else {
		transcript = m.transcriptRows(m.viewport.width)
	}
	transcript = m.selectionPass(transcript)
	body, cursorY := m.viewBody(transcript)
	if m.viewport.width > 0 && m.overlays.panel.kind != panelregistry.Changelog && m.overlays.panel.kind != panelregistry.Debug {
		body = tuilib.FitLines(body, m.viewport.width)
	}
	body, cursorY = m.clipViewBody(body, cursorY)
	return m.buildTeaView(body, m.activeCursorX(), cursorY)
}

func assertRenderedPromptViewsEqual(t *testing.T, got, want tea.View) {
	t.Helper()
	if got.Content != want.Content {
		t.Fatalf("rendered ANSI content differs:\n--- frame cached ---\n%s\n--- cold ---\n%s", got.Content, want.Content)
	}
	if got.Cursor == nil || want.Cursor == nil {
		if got.Cursor != nil || want.Cursor != nil {
			t.Fatalf("cursor visibility differs: cached=%+v cold=%+v", got.Cursor, want.Cursor)
		}
		return
	}
	if *got.Cursor != *want.Cursor {
		t.Fatalf("cursor differs: cached=%+v cold=%+v", got.Cursor, want.Cursor)
	}
}

func TestViewFormatsComposerAtMostOncePerFrame(t *testing.T) {
	cases := []struct {
		name   string
		setup  func(*RootModel)
		stream bool
	}{
		{
			name: "placeholder",
		},
		{
			name: "multiline CJK emoji and long input",
			setup: func(m *RootModel) {
				value := strings.Repeat("中文🙂é /model ", 8) + "\n" + strings.Repeat("宽字符🧬 ", 24)
				m.prompt.SetComposerValue(value)
				m.prompt.SetInputCursorOffset(len(value))
			},
		},
		{
			name: "selected input",
			setup: func(m *RootModel) {
				m.prompt.SetComposerValue("select 中文🙂 text")
				if _, ok := m.prompt.SelectAll(); !ok {
					t.Fatal("SelectAll did not create an input selection")
				}
			},
		},
		{
			name: "resized terminal",
			setup: func(m *RootModel) {
				m.prompt.SetComposerValue(strings.Repeat("resize 🧭 中文 ", 8))
				m.Update(tea.WindowSizeMsg{Width: 42, Height: 48})
			},
		},
		{
			name: "streaming",
			setup: func(m *RootModel) {
				m.prompt.SetComposerValue("while a response streams")
			},
			stream: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := promptFrameTestModel(t)
			if tc.setup != nil {
				tc.setup(m)
			}
			m.conversation.streaming = tc.stream
			m.viewport.extraRowsValid = false
			m.viewport.viewCacheValid = false

			got := m.View()
			if m.viewport.lastPromptInputViewMisses != 1 {
				t.Fatalf("textarea formatting misses = %d, want 1 for this frame", m.viewport.lastPromptInputViewMisses)
			}
			if m.viewport.promptFrameActive || m.viewport.preparedInputViewValid || m.viewport.preparedInputView != "" {
				t.Fatal("per-frame composer cache survived View")
			}

			// Streaming rows may contain a time-dependent activity label. The
			// composer output and cursor geometry are deterministic; for idle
			// states the complete rendered view is deterministic as well.
			want := renderPromptColdView(m)
			if tc.stream {
				if got.Cursor == nil || want.Cursor == nil || *got.Cursor != *want.Cursor {
					t.Fatalf("streaming cursor differs: cached=%+v cold=%+v", got.Cursor, want.Cursor)
				}
				composerRow := func(content string) string {
					for _, row := range strings.Split(content, "\n") {
						if strings.Contains(ansi.Strip(row), m.prompt.input.Value()) {
							return row
						}
					}
					return ""
				}
				gotRow, wantRow := composerRow(got.Content), composerRow(want.Content)
				if gotRow == "" || gotRow != wantRow {
					t.Fatalf("streaming composer ANSI differs: cached=%q cold=%q", gotRow, wantRow)
				}
			} else {
				assertRenderedPromptViewsEqual(t, got, want)
			}

			// A changed value/selection must be formatted afresh on the next
			// frame; no output is retained across the View boundary.
			m.conversation.streaming = false
			m.prompt.SetComposerValue("next frame: 🧪")
			m.prompt.ResetInputSelection()
			m.viewport.extraRowsValid = false
			m.viewport.viewCacheValid = false
			next := m.View()
			if m.viewport.lastPromptInputViewMisses != 1 {
				t.Fatalf("next-frame formatting misses = %d, want 1", m.viewport.lastPromptInputViewMisses)
			}
			if !strings.Contains(next.Content, "next frame: 🧪") {
				t.Fatalf("next frame rendered stale composer: %q", next.Content)
			}
		})
	}
}

func TestViewDoesNotPrepareComposerForFullViewportPanel(t *testing.T) {
	m := promptFrameTestModel(t)
	m.overlays.panel = slashPanel{kind: panelregistry.Changelog, cmd: "/changelog"}
	m.viewport.extraRowsValid = false
	m.viewport.viewCacheValid = false

	_ = m.View()
	if m.viewport.lastPromptInputViewMisses != 0 {
		t.Fatalf("full-viewport panel formatted composer %d times, want 0", m.viewport.lastPromptInputViewMisses)
	}
}

func BenchmarkPromptFrameFormatting(b *testing.B) {
	for _, mode := range []string{"frame-cached", "cold-double-format"} {
		b.Run(mode, func(b *testing.B) {
			m := newBenchModel()
			m.viewport.width = 100
			m.viewport.height = 50
			m.prompt.SetComposerValue(strings.Repeat("long prompt 中文🙂 ", 20))
			m.prompt.SetInputCursorOffset(len(m.prompt.ComposerValue()))
			m.viewport.rowsCacheDirty = false
			b.ReportAllocs()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				m.viewport.viewCacheValid = false
				m.viewport.extraRowsValid = false
				if mode == "frame-cached" {
					_ = m.View()
				} else {
					_ = renderPromptColdView(m)
				}
			}
		})
	}
}
