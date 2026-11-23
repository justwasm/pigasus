package tui

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/infra/remoteagentclient"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
)

func TestTerminalPanel_InteractiveSessionAndModalIsolation(t *testing.T) {
	var mu sync.Mutex
	var receivedInput []byte
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, ":listTerminals"):
			_ = json.NewEncoder(w).Encode(map[string]any{"terminals": []any{}})
		case strings.HasSuffix(r.URL.Path, ":createTerminal"):
			_ = json.NewEncoder(w).Encode(map[string]any{"terminal": map[string]any{
				"terminalId": "term-test-42", "title": "Interactive Shell", "cwd": "/workspace", "pid": 1234,
			}})
		case strings.HasSuffix(r.URL.Path, ":sendTerminalInput"):
			var body struct {
				Input []byte `json:"input"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			receivedInput = append(receivedInput, body.Input...)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		case strings.HasSuffix(r.URL.Path, ":closeTerminal"):
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: srv.URL})
	engine := agentturn.NewRemoteAgentEngine(agentturn.RemoteAgentConfig{Client: client, Model: "m"})
	m := &RootModel{
		session: sessionState{remoteAgent: true},
		turn:    TurnService{engine: engine},
	}
	m.viewport.width = 100
	m.viewport.height = 30

	// 1. Launch /terminal -> creates terminal and opens modal overlay
	handled, _ := m.slashTerminal("")
	if !handled {
		t.Fatal("/terminal was not handled")
	}
	if m.overlays.panel.kind != panelregistry.Terminal {
		t.Fatalf("expected panel kind Terminal, got %v", m.overlays.panel.kind)
	}
	if m.remote.terminalID != "term-test-42" {
		t.Fatalf("expected terminalID term-test-42, got %q", m.remote.terminalID)
	}

	term := m.terminalPanel()
	if term == nil {
		t.Fatal("terminal panel is nil")
	}

	// 2. Stream chunk into terminal panel
	_, _, handled = m.updateStreamingMsg(terminalChunkMsg{Text: "Welcome to Pigasus Terminal\r\n$ "})
	if !handled {
		t.Fatalf("terminalChunkMsg was not handled")
	}
	screen := strings.Join(term.ScreenLines(), "\n")
	if !strings.Contains(screen, "Welcome to Pigasus Terminal") {
		t.Errorf("screen did not reflect streamed chunk:\n%s", screen)
	}

	// 3. User types "ls -la" + Enter
	for _, ch := range "ls -la" {
		term.HandleKey(tea.Key{Code: rune(ch), Text: string(ch)})
	}
	term.HandleKey(tea.Key{Code: tea.KeyEnter})

	mu.Lock()
	gotInput := string(receivedInput)
	receivedInput = nil
	mu.Unlock()
	if gotInput != "ls -la\r" {
		t.Errorf("expected received input 'ls -la\\r', got %q", gotInput)
	}

	// 4. Ctrl+C modal isolation (AGENTS.md §10):
	// Ctrl+C MUST be sent to PTY as 0x03 and MUST NOT dismiss modal
	term.HandleKey(tea.Key{Code: 'c', Mod: tea.ModCtrl})
	mu.Lock()
	gotCtrlC := receivedInput
	receivedInput = nil
	mu.Unlock()
	if len(gotCtrlC) != 1 || gotCtrlC[0] != 0x03 {
		t.Errorf("expected Ctrl+C to send 0x03, got %v", gotCtrlC)
	}
	if m.overlays.panel.kind != panelregistry.Terminal {
		t.Fatalf("Ctrl+C improperly closed the panel modal")
	}

	// 5. Esc modal isolation (AGENTS.md §10):
	// Esc detaches the panel modal, returning to None, leaving terminal ID active
	handled, cmd := term.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Errorf("Esc was not handled")
	}
	if cmd != nil {
		_ = cmd() // execute detach cmd (calls closePanel)
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("Esc did not detach panel, kind=%v", m.overlays.panel.kind)
	}
	if m.remote.terminalID != "term-test-42" {
		t.Errorf("terminal session was improperly terminated on detach")
	}
}

func TestRootModel_TerminalModalWiringAndIsolation(t *testing.T) {
	var mu sync.Mutex
	var receivedInput []byte
	var resizedCols, resizedRows int32
	var closedCount int

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, ":listTerminals"):
			_ = json.NewEncoder(w).Encode(map[string]any{"terminals": []any{}})
		case strings.HasSuffix(r.URL.Path, ":createTerminal"):
			_ = json.NewEncoder(w).Encode(map[string]any{"terminal": map[string]any{
				"terminalId": "term-wire-1", "title": "bash", "cwd": "/ws", "pid": 4242,
			}})
		case strings.HasSuffix(r.URL.Path, ":sendTerminalInput"):
			var body struct {
				Input  []byte `json:"input"`
				Resize *struct {
					Cols int32 `json:"cols"`
					Rows int32 `json:"rows"`
				} `json:"resize"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			if len(body.Input) > 0 {
				receivedInput = append(receivedInput, body.Input...)
			}
			if body.Resize != nil {
				resizedCols = body.Resize.Cols
				resizedRows = body.Resize.Rows
			}
			_ = json.NewEncoder(w).Encode(map[string]any{"ackedOffset": len(body.Input)})
		case strings.HasSuffix(r.URL.Path, ":closeTerminal"):
			closedCount++
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: srv.URL})
	engine := agentturn.NewRemoteAgentEngine(agentturn.RemoteAgentConfig{Client: client, Model: "m"})
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", false)
	m.session.remoteAgent = true
	m.turn.engine = engine
	m.viewport.width = 100
	m.viewport.height = 30

	// Verify openSlashPanelArgs does NOT swallow /terminal
	if m.openSlashPanelArgs("terminal", "") {
		t.Fatal("openSlashPanelArgs(\"terminal\", \"\") must return false so slashTerminal runs")
	}

	// 1. Dispatch /terminal via handleSlashCommand
	handled, _ := m.handleSlashCommand("/terminal")
	if !handled {
		t.Fatal("/terminal was not handled")
	}
	if m.overlays.panel.kind != panelregistry.Terminal {
		t.Fatalf("expected panel kind Terminal, got %v", m.overlays.panel.kind)
	}
	if !m.isFullViewportPanel() {
		t.Fatal("expected Terminal to be a full-viewport panel")
	}

	// 2. RootModel.View() renders the terminal box in AltScreen with inline cursor
	v := m.View()
	if !v.AltScreen {
		t.Errorf("expected AltScreen=true when terminal panel is open")
	}
	if v.Cursor != nil {
		t.Errorf("expected v.Cursor == nil when terminal panel is open, got %+v", v.Cursor)
	}
	if !strings.Contains(v.Content, "╭─") || !strings.Contains(v.Content, "bash") {
		t.Fatalf("RootModel.View() did not render terminal panel box:\n%s", v.Content)
	}

	// 3. Stream output into session -> appears in View().Content, NOT in conversation.response
	m.Update(terminalChunkMsg{TerminalID: "term-wire-1", Text: "hello-pty\r\n$ "})
	v = m.View()
	if !strings.Contains(ansi.Strip(v.Content), "hello-pty") {
		t.Fatalf("RootModel.View() missing streamed terminal output:\n%s", v.Content)
	}
	if strings.Contains(strings.Join(m.conversation.response, "\n"), "hello-pty") {
		t.Fatalf("interactive terminal output leaked into conversation.response")
	}

	// 4. Ctrl+C, Ctrl+D, Tab, Ctrl+L, Ctrl+Z reach PTY via RootModel.Update without quitting/closing
	for _, tc := range []struct {
		name string
		key  tea.Key
		want byte
	}{
		{"Ctrl+C", tea.Key{Code: 'c', Mod: tea.ModCtrl}, 0x03},
		{"Ctrl+D", tea.Key{Code: 'd', Mod: tea.ModCtrl}, 0x04},
		{"Tab", tea.Key{Code: tea.KeyTab}, '\t'},
		{"Ctrl+L", tea.Key{Code: 'l', Mod: tea.ModCtrl}, 0x0c},
		{"Ctrl+Z", tea.Key{Code: 'z', Mod: tea.ModCtrl}, 0x1a},
	} {
		mu.Lock()
		receivedInput = nil
		mu.Unlock()
		_, cmd := m.Update(tea.KeyPressMsg(tc.key))
		if m.display.quitting {
			t.Fatalf("%s triggered TUI quit", tc.name)
		}
		if m.overlays.panel.kind != panelregistry.Terminal {
			t.Fatalf("%s closed terminal panel", tc.name)
		}
		if cmd != nil {
			if msg := cmd(); msg != nil {
				if _, isQuit := msg.(tea.QuitMsg); isQuit {
					t.Fatalf("%s returned tea.Quit", tc.name)
				}
			}
		}
		mu.Lock()
		got := append([]byte(nil), receivedInput...)
		mu.Unlock()
		if len(got) != 1 || got[0] != tc.want {
			t.Fatalf("%s sent %v to PTY, want [%#x]", tc.name, got, tc.want)
		}
	}

	// 5. tea.PasteMsg routes to PTY with bracketed paste, NOT composer
	mu.Lock()
	receivedInput = nil
	mu.Unlock()
	m.Update(tea.PasteMsg{Content: "echo pasted"})
	mu.Lock()
	pasted := string(receivedInput)
	mu.Unlock()
	if pasted != "\x1b[200~echo pasted\x1b[201~" {
		t.Fatalf("PasteMsg sent %q to PTY, want bracketed paste", pasted)
	}
	if m.prompt.input.Value() != "" {
		t.Fatalf("PasteMsg leaked into composer: %q", m.prompt.input.Value())
	}

	// 6. tea.WindowSizeMsg resizes terminal panel and notifies backend
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	mu.Lock()
	gotCols, gotRows := resizedCols, resizedRows
	mu.Unlock()
	if gotCols != 116 || gotRows != 36 {
		t.Fatalf("WindowSizeMsg resized PTY to (%d, %d), want (116, 36)", gotRows, gotCols)
	}

	// 7. Esc detaches without killing PTY, and re-opening /terminal preserves screen buffer
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if cmd != nil {
		_ = cmd()
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Fatalf("Esc did not detach terminal panel, kind=%v", m.overlays.panel.kind)
	}
	mu.Lock()
	cc := closedCount
	mu.Unlock()
	if cc != 0 {
		t.Fatalf("Esc closed PTY (%d times), want 0", cc)
	}

	// Re-open /terminal -> screen buffer still has "hello-pty"
	m.handleSlashCommand("/terminal")
	v = m.View()
	if !strings.Contains(ansi.Strip(v.Content), "hello-pty") {
		t.Fatalf("re-opening /terminal lost screen buffer:\n%s", v.Content)
	}
}

func TestRootModel_TerminalMultiSessionAndShortcuts(t *testing.T) {
	var mu sync.Mutex
	var nextID int
	var closedIDs []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch {
		case strings.HasSuffix(r.URL.Path, ":listTerminals"):
			_ = json.NewEncoder(w).Encode(map[string]any{"terminals": []any{}})
		case strings.HasSuffix(r.URL.Path, ":createTerminal"):
			nextID++
			id := fmt.Sprintf("term-multi-%d", nextID)
			_ = json.NewEncoder(w).Encode(map[string]any{"terminal": map[string]any{
				"terminalId": id, "title": fmt.Sprintf("sh-%d", nextID), "cwd": "/ws", "pid": 100 + nextID,
			}})
		case strings.HasSuffix(r.URL.Path, ":sendTerminalInput"):
			_ = json.NewEncoder(w).Encode(map[string]any{"ackedOffset": 0})
		case strings.HasSuffix(r.URL.Path, ":closeTerminal"):
			var body struct {
				TerminalID string `json:"terminalId"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			closedIDs = append(closedIDs, body.TerminalID)
			_ = json.NewEncoder(w).Encode(map[string]any{"success": true})
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	client := remoteagentclient.New(remoteagentclient.Config{BaseURL: srv.URL})
	engine := agentturn.NewRemoteAgentEngine(agentturn.RemoteAgentConfig{Client: client, Model: "m"})
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", false)
	m.session.remoteAgent = true
	m.turn.engine = engine
	m.viewport.width = 100
	m.viewport.height = 30

	// Session 1
	m.handleSlashCommand("/terminal")
	m.Update(terminalChunkMsg{TerminalID: "term-multi-1", Text: "SESSION_ONE_OUT\r\n"})

	// Alt+N creates Session 2
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'n', Mod: tea.ModAlt, Text: "n"}))
	if m.remote.terminalID != "term-multi-2" {
		t.Fatalf("Alt+N did not switch to new session, active=%q", m.remote.terminalID)
	}
	m.Update(terminalChunkMsg{TerminalID: "term-multi-2", Text: "SESSION_TWO_OUT\r\n"})

	v2 := ansi.Strip(m.View().Content)
	if !strings.Contains(v2, "SESSION_TWO_OUT") || strings.Contains(v2, "SESSION_ONE_OUT") {
		t.Fatalf("session 2 view leaked or missed output:\n%s", v2)
	}
	if !strings.Contains(v2, "1:sh-1") || !strings.Contains(v2, "2:sh-2") {
		t.Fatalf("expected multi-session tab bar in view:\n%s", v2)
	}

	// Alt+Left switches back to Session 1
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft, Mod: tea.ModAlt}))
	if m.remote.terminalID != "term-multi-1" {
		t.Fatalf("Alt+Left did not switch back to term-multi-1, active=%q", m.remote.terminalID)
	}
	v1 := ansi.Strip(m.View().Content)
	if !strings.Contains(v1, "SESSION_ONE_OUT") || strings.Contains(v1, "SESSION_TWO_OUT") {
		t.Fatalf("session 1 view leaked or missed output:\n%s", v1)
	}

	// /terminal switch term-multi-2
	m.handleSlashCommand("/terminal switch term-multi-2")
	if m.remote.terminalID != "term-multi-2" {
		t.Fatalf("/terminal switch failed, active=%q", m.remote.terminalID)
	}

	// Alt+W closes active session (term-multi-2) WITHOUT toggling wireHUD
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'w', Mod: tea.ModAlt, Text: "w"}))
	if m.overlays.wireHUD != nil && m.overlays.wireHUD.IsVisible() {
		t.Fatal("Alt+W toggled wireHUD while terminal panel was open")
	}
	if m.remote.terminalID != "term-multi-1" {
		t.Fatalf("expected Alt+W to fall back to term-multi-1, got %q", m.remote.terminalID)
	}

	// CloseAllTerminals closes remaining session
	m.CloseAllTerminals()
	mu.Lock()
	gotClosed := append([]string(nil), closedIDs...)
	mu.Unlock()
	if len(gotClosed) != 2 {
		t.Fatalf("expected 2 closed terminals total, got %v", gotClosed)
	}
}

func TestRootModel_TerminalPanelSelectionStripsBorders(t *testing.T) {
	lines := []string{
		"╭─ bash · Running ─────────╮",
		"│ hello world              │",
		"│ second line              │",
		"╰─ 24x10 ──────────────────╯",
	}
	got := terminalPanelSelectionText(lines, 0, 0, 3, 27, false)
	want := "hello world\nsecond line"
	if got != want {
		t.Fatalf("terminalPanelSelectionText = %q, want %q", got, want)
	}
}
