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
