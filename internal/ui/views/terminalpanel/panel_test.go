package terminalpanel_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/views/terminalpanel"
)

func TestTerminalPanel_BasicLifecycleAndRender(t *testing.T) {
	var receivedInput []byte
	var resizedRows, resizedCols int
	var detached, closed bool

	seams := terminalpanel.Seams{
		WriteInput: func(b []byte) error {
			receivedInput = append(receivedInput, b...)
			return nil
		},
		Resize: func(rows, cols int) error {
			resizedRows = rows
			resizedCols = cols
			return nil
		},
		Detach: func() tea.Cmd {
			detached = true
			return nil
		},
		Close: func() tea.Cmd {
			closed = true
			return nil
		},
		Title: func() string {
			return "term-main"
		},
		Width: func() int {
			return 80
		},
		Height: func() int {
			return 24
		},
	}

	p := terminalpanel.New(20, 80, seams)
	if p.RowCount() != 20 {
		t.Fatalf("expected rows=20, got %d", p.RowCount())
	}
	if p.Status() != "Running" {
		t.Fatalf("expected status 'Running', got %q", p.Status())
	}

	// Feed initial terminal output
	p.WriteOutput([]byte("Hello, terminal world!\r\n$ "))
	if p.ResumeOffset() != int64(len("Hello, terminal world!\r\n$ ")) {
		t.Fatalf("expected resumeOffset=%d, got %d", len("Hello, terminal world!\r\n$ "), p.ResumeOffset())
	}

	rendered := p.Render()
	if !strings.Contains(rendered, "term-main") {
		t.Errorf("rendered output missing title 'term-main':\n%s", rendered)
	}
	if !strings.Contains(rendered, "Hello, terminal world!") {
		t.Errorf("rendered output missing text 'Hello, terminal world!':\n%s", rendered)
	}
	if !strings.Contains(rendered, "Running") {
		t.Errorf("rendered output missing status 'Running':\n%s", rendered)
	}

	// Test Resize
	p.Resize(25, 90)
	if resizedRows != 25 || resizedCols != 90 {
		t.Errorf("expected resize callback with (25, 90), got (%d, %d)", resizedRows, resizedCols)
	}

	// Test Exit Status
	p.SetExitStatus(0, nil)
	exited, code, err := p.ExitStatus()
	if !exited || code != 0 || err != nil {
		t.Errorf("expected exited=true, code=0, err=nil, got exited=%v, code=%d, err=%v", exited, code, err)
	}
	if !strings.Contains(p.Status(), "Exited") {
		t.Errorf("expected status containing 'Exited', got %q", p.Status())
	}

	_ = detached
	_ = closed
	_ = receivedInput
}

func TestTerminalPanel_KeyEncodingAndModalIsolation(t *testing.T) {
	var written []byte
	var detached bool
	var closed bool

	p := terminalpanel.New(20, 80, terminalpanel.Seams{
		WriteInput: func(b []byte) error {
			written = append(written, b...)
			return nil
		},
		Detach: func() tea.Cmd {
			detached = true
			return nil
		},
		Close: func() tea.Cmd {
			closed = true
			return nil
		},
	})

	// 1. Regular character input
	handled, _ := p.HandleKey(tea.Key{Code: 'l', Text: "l"})
	if !handled || string(written) != "l" {
		t.Errorf("typing 'l': handled=%v, written=%q, want 'l'", handled, written)
	}
	written = nil

	// 2. Enter key
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if string(written) != "\r" {
		t.Errorf("Enter key: written=%q, want '\\r'", written)
	}
	written = nil

	// 3. Arrow keys
	p.HandleKey(tea.Key{Code: tea.KeyUp})
	if string(written) != "\x1b[A" {
		t.Errorf("Up arrow: written=%q, want '\\x1b[A'", written)
	}
	written = nil

	p.HandleKey(tea.Key{Code: tea.KeyDown})
	if string(written) != "\x1b[B" {
		t.Errorf("Down arrow: written=%q, want '\\x1b[B'", written)
	}
	written = nil

	// 4. Ctrl+C: MUST pass through to PTY as 0x03 and MUST NOT trigger Detach or Close (AGENTS.md §10)
	handled, _ = p.HandleKey(tea.Key{Code: 'c', Mod: tea.ModCtrl})
	if !handled {
		t.Errorf("Ctrl+C was not handled by terminal modal")
	}
	if len(written) != 1 || written[0] != 0x03 {
		t.Errorf("Ctrl+C did not encode to 0x03; got %v", written)
	}
	if detached || closed {
		t.Fatalf("VIOLATION of AGENTS.md §10: Ctrl+C leaked to Detach (%v) or Close (%v)", detached, closed)
	}
	written = nil

	// 5. Ctrl+D: passes through to PTY as 0x04 (EOF)
	p.HandleKey(tea.Key{Code: 'd', Mod: tea.ModCtrl})
	if len(written) != 1 || written[0] != 0x04 {
		t.Errorf("Ctrl+D did not encode to 0x04; got %v", written)
	}
	written = nil

	// 6. Alt+Key: ESC + character
	p.HandleKey(tea.Key{Code: 'f', Mod: tea.ModAlt, Text: "f"})
	if string(written) != "\x1bf" {
		t.Errorf("Alt+f: written=%q, want '\\x1bf'", written)
	}
	written = nil

	// 7. Esc key: Detaches modal without closing process
	handled, _ = p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled || !detached {
		t.Errorf("Esc key: handled=%v, detached=%v, want true/true", handled, detached)
	}
}

func TestTerminalPanel_ScrollbackBufferAndNavigation(t *testing.T) {
	p := terminalpanel.New(5, 40, terminalpanel.Seams{})

	// Emit 30 lines to trigger OnScrollback
	for i := 1; i <= 30; i++ {
		p.WriteOutput([]byte(strings.Repeat("X", 30) + "\r\n"))
	}

	lines := p.ScrollbackLines()
	if len(lines) == 0 {
		t.Fatalf("expected scrollback lines to accumulate, got 0")
	}

	// Trigger scrollback with PgUp
	handled, _ := p.HandleKey(tea.Key{Code: tea.KeyPgUp})
	if !handled || p.ScrollOffset() <= 0 {
		t.Errorf("PgUp should activate scrollback; offset=%d", p.ScrollOffset())
	}

	// Render view should now show scrollback badge
	rendered := p.Render()
	if !strings.Contains(rendered, "Scrollback") {
		t.Errorf("expected rendered scrollback view to show Scrollback hint, got:\n%s", rendered)
	}

	// Esc in scrollback mode drops back to live output without detaching
	var detached bool
	p = terminalpanel.New(5, 40, terminalpanel.Seams{
		Detach: func() tea.Cmd {
			detached = true
			return nil
		},
	})
	for i := 1; i <= 20; i++ {
		p.WriteOutput([]byte("line\r\n"))
	}
	p.HandleKey(tea.Key{Code: tea.KeyPgUp})
	if p.ScrollOffset() == 0 {
		t.Fatal("expected non-zero scrollOffset after PgUp")
	}

	// First Esc returns to live mode
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.ScrollOffset() != 0 {
		t.Errorf("Esc should reset scrollOffset to 0, got %d", p.ScrollOffset())
	}
	if detached {
		t.Errorf("First Esc should exit scrollback mode, not detach")
	}

	// Second Esc detaches
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !detached {
		t.Errorf("Second Esc should trigger Detach")
	}
}

func TestTerminalPanel_PasteEncoding(t *testing.T) {
	text := "git status\n"
	encoded := terminalpanel.EncodePaste(text)
	want := "\x1b[200~git status\n\x1b[201~"
	if string(encoded) != want {
		t.Errorf("EncodePaste got %q, want %q", string(encoded), want)
	}
}

func TestTerminalPanel_VisibleCursorRendered(t *testing.T) {
	p := terminalpanel.New(10, 40, terminalpanel.Seams{
		Width:  func() int { return 44 },
		Height: func() int { return 14 },
	})
	p.WriteOutput([]byte("$ hi"))
	rendered := p.Render()
	if !strings.Contains(rendered, "\x1b[7m") || !strings.Contains(rendered, "\x1b[0m") {
		t.Fatalf("expected reverse-video cursor cell (\\x1b[7m...\\x1b[0m) in rendered output:\n%q", rendered)
	}

	// CSI ? 25 l hides cursor
	p.WriteOutput([]byte("\x1b[?25l"))
	hidden := p.Render()
	if strings.Contains(hidden, "\x1b[7m") {
		t.Fatalf("expected cursor reverse-video to be hidden after CSI ? 25 l, got:\n%q", hidden)
	}

	// CSI ? 25 h shows cursor again
	p.WriteOutput([]byte("\x1b[?25h"))
	shown := p.Render()
	if !strings.Contains(shown, "\x1b[7m") {
		t.Fatalf("expected cursor reverse-video to reappear after CSI ? 25 h, got:\n%q", shown)
	}
}

func TestTerminalPanel_ScrollbackPreservesANSISGR(t *testing.T) {
	p := terminalpanel.New(5, 30, terminalpanel.Seams{
		Width:  func() int { return 34 },
		Height: func() int { return 9 },
	})
	for i := 1; i <= 12; i++ {
		p.WriteOutput([]byte("\x1b[31mred-line\x1b[0m " + strings.Repeat("W", 40) + "\r\n"))
	}
	sb := p.ScrollbackLines()
	if len(sb) == 0 {
		t.Fatal("expected scrollback lines")
	}
	if !strings.Contains(sb[0], "\x1b[31m") {
		t.Fatalf("expected scrollback line to preserve ANSI SGR \\x1b[31m, got %q", sb[0])
	}

	p.HandleKey(tea.Key{Code: tea.KeyPgUp})
	rendered := p.Render()
	if !strings.Contains(rendered, "\x1b[31m") {
		t.Fatalf("expected rendered scrollback view to preserve ANSI SGR \\x1b[31m, got %q", rendered)
	}
	for _, row := range strings.Split(rendered, "\n") {
		if !strings.HasSuffix(row, "╮") && !strings.HasSuffix(row, "│\x1b[0m") && !strings.HasSuffix(row, "╯") {
			t.Fatalf("row overflowed right border: %q", row)
		}
	}
}

func TestTerminalPanel_CopyModeNavigationSelectAndYank(t *testing.T) {
	var yanked string
	p := terminalpanel.New(6, 40, terminalpanel.Seams{
		Width:    func() int { return 44 },
		Height:   func() int { return 10 },
		CopyText: func(s string) error { yanked = s; return nil },
	})
	p.WriteOutput([]byte("alpha beta\r\ngamma delta\r\nepsilon zeta"))

	// Enter copy mode with Alt+C
	p.HandleKey(tea.Key{Code: 'c', Mod: tea.ModAlt, Text: "c"})
	if !p.InCopyMode() {
		t.Fatal("expected Alt+C to enter copy mode")
	}
	rendered := p.Render()
	if !strings.Contains(rendered, "COPY MODE") {
		t.Fatalf("expected COPY MODE badge in render:\n%s", rendered)
	}

	// Move up to "gamma delta", go to col 0, move right 6 ("delta"), visual select 5 chars, yank
	p.HandleKey(tea.Key{Code: 'k', Text: "k"})
	p.HandleKey(tea.Key{Code: '0', Text: "0"})
	for i := 0; i < 6; i++ {
		p.HandleKey(tea.Key{Code: 'l', Text: "l"})
	}
	p.HandleKey(tea.Key{Code: 'v', Text: "v"})
	for i := 0; i < 4; i++ {
		p.HandleKey(tea.Key{Code: 'l', Text: "l"})
	}
	selRendered := p.Render()
	if !strings.Contains(selRendered, "\x1b[7mdelta\x1b[0m") {
		t.Fatalf("expected reverse-video selection highlight around 'delta', got:\n%q", selRendered)
	}

	p.HandleKey(tea.Key{Code: 'y', Text: "y"})
	if p.InCopyMode() {
		t.Fatal("expected y to exit copy mode")
	}
	if yanked != "delta" {
		t.Fatalf("yanked = %q, want %q", yanked, "delta")
	}

	// Line visual mode (V) + Enter
	yanked = ""
	p.HandleKey(tea.Key{Code: 'c', Mod: tea.ModAlt, Text: "c"})
	p.HandleKey(tea.Key{Code: 'g', Text: "g"})
	p.HandleKey(tea.Key{Code: 'V', Text: "V"})
	p.HandleKey(tea.Key{Code: 'j', Text: "j"})
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if yanked != "alpha beta\ngamma delta" {
		t.Fatalf("line-mode yanked = %q, want %q", yanked, "alpha beta\ngamma delta")
	}
}

func TestTerminalPanel_MultiSessionTabBar(t *testing.T) {
	p := terminalpanel.New(10, 60, terminalpanel.Seams{
		Width:  func() int { return 64 },
		Height: func() int { return 14 },
		Tabs: func() []terminalpanel.TabInfo {
			return []terminalpanel.TabInfo{
				{ID: "t1", Title: "bash", Active: true},
				{ID: "t2", Title: "zsh", Active: false},
			}
		},
	})
	rendered := p.Render()
	if !strings.Contains(rendered, "1:bash") || !strings.Contains(rendered, "2:zsh") {
		t.Fatalf("expected tab bar with 1:bash and 2:zsh, got:\n%s", rendered)
	}
	lines := strings.Split(rendered, "\n")
	if len(lines) > 13 {
		t.Fatalf("expected rendered lines (%d) to fit inside viewport height 14 minus status line", len(lines))
	}
}
