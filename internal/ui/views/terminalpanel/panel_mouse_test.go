package terminalpanel_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/ui/views/terminalpanel"
)

func TestTerminalPanel_MousePassthroughSGRAndX10(t *testing.T) {
	var written []byte
	p := terminalpanel.New(10, 40, terminalpanel.Seams{
		Width:  func() int { return 44 },
		Height: func() int { return 14 },
		WriteInput: func(b []byte) error {
			written = append(written, b...)
			return nil
		},
	})
	p.Render()

	// Enable DEC 1000 (clicks), 1002 (cell motion), 1006 (SGR)
	p.WriteOutput([]byte("\x1b[?1000h\x1b[?1002h\x1b[?1006h"))
	if !p.MouseTrackingEnabled() {
		t.Fatal("expected MouseTrackingEnabled() == true after CSI ?1000h")
	}

	// Body starts at x=2, y=1 (no tab bar). Click at panel (x=5, y=3) -> body (col=3, row=2) -> 1-based (4, 3)
	handled, _ := p.HandleMouse(tea.Mouse{X: 5, Y: 3, Button: tea.MouseLeft}, false, false)
	if !handled {
		t.Fatal("expected HandleMouse press to be handled")
	}
	if string(written) != "\x1b[<0;4;3M" {
		t.Fatalf("SGR press = %q, want %q", string(written), "\x1b[<0;4;3M")
	}
	written = nil

	// Drag motion to (x=7, y=4) -> body (col=5, row=3) -> 1-based (6, 4)
	handled, _ = p.HandleMouse(tea.Mouse{X: 7, Y: 4, Button: tea.MouseNone}, false, true)
	if !handled {
		t.Fatal("expected HandleMouse drag motion to be handled")
	}
	if string(written) != "\x1b[<32;6;4M" {
		t.Fatalf("SGR drag = %q, want %q", string(written), "\x1b[<32;6;4M")
	}
	written = nil

	// Release at (x=7, y=4)
	handled, _ = p.HandleMouse(tea.Mouse{X: 7, Y: 4, Button: tea.MouseNone}, true, false)
	if !handled {
		t.Fatal("expected HandleMouse release to be handled")
	}
	if string(written) != "\x1b[<0;6;4m" {
		t.Fatalf("SGR release = %q, want %q", string(written), "\x1b[<0;6;4m")
	}
	written = nil

	// Wheel up & down in SGR mode
	if !p.HandleWheel(tea.Mouse{X: 5, Y: 3, Button: tea.MouseWheelUp}) {
		t.Fatal("expected HandleWheel up to be handled")
	}
	if string(written) != "\x1b[<64;4;3M" {
		t.Fatalf("SGR wheel up = %q, want %q", string(written), "\x1b[<64;4;3M")
	}
	written = nil

	if !p.HandleWheel(tea.Mouse{X: 5, Y: 3, Button: tea.MouseWheelDown}) {
		t.Fatal("expected HandleWheel down to be handled")
	}
	if string(written) != "\x1b[<65;4;3M" {
		t.Fatalf("SGR wheel down = %q, want %q", string(written), "\x1b[<65;4;3M")
	}
	written = nil

	// Disable SGR (1006l) -> falls back to X10 encoding
	p.WriteOutput([]byte("\x1b[?1006l"))
	p.HandleMouse(tea.Mouse{X: 2, Y: 1, Button: tea.MouseRight}, false, false)
	wantX10 := []byte{0x1B, '[', 'M', 32 + 2, 32 + 1, 32 + 1}
	if string(written) != string(wantX10) {
		t.Fatalf("X10 right click = %v, want %v", written, wantX10)
	}
	written = nil

	// Disable mouse tracking (1000l, 1002l) -> body click returns false so host can select text
	p.WriteOutput([]byte("\x1b[?1000l\x1b[?1002l"))
	if p.MouseTrackingEnabled() {
		t.Fatal("expected MouseTrackingEnabled() == false after CSI ?1000l")
	}
	handled, _ = p.HandleMouse(tea.Mouse{X: 5, Y: 3, Button: tea.MouseLeft}, false, false)
	if handled {
		t.Fatal("expected body click to return false when mouse tracking is disabled")
	}
}

func TestTerminalPanel_WheelScrollbackAndAltScroll(t *testing.T) {
	var written []byte
	p := terminalpanel.New(5, 40, terminalpanel.Seams{
		Width:  func() int { return 44 },
		Height: func() int { return 9 },
		WriteInput: func(b []byte) error {
			written = append(written, b...)
			return nil
		},
	})
	for i := 0; i < 20; i++ {
		p.WriteOutput([]byte("scroll-line\r\n"))
	}

	// Wheel up enters scrollback
	if !p.HandleWheel(tea.Mouse{X: 5, Y: 2, Button: tea.MouseWheelUp}) {
		t.Fatal("expected wheel up to be handled")
	}
	if p.ScrollOffset() <= 0 {
		t.Fatalf("expected positive scrollOffset after wheel up, got %d", p.ScrollOffset())
	}

	// Wheel down returns to live view
	if !p.HandleWheel(tea.Mouse{X: 5, Y: 2, Button: tea.MouseWheelDown}) {
		t.Fatal("expected wheel down to be handled")
	}
	if p.ScrollOffset() != 0 {
		t.Fatalf("expected scrollOffset == 0 after wheel down, got %d", p.ScrollOffset())
	}

	// Alt screen without mouse reporting -> wheel sends Up/Down arrow keys
	p.WriteOutput([]byte("\x1b[?1049h"))
	p.HandleWheel(tea.Mouse{X: 5, Y: 2, Button: tea.MouseWheelUp})
	if string(written) != "\x1b[A\x1b[A\x1b[A" {
		t.Fatalf("alt-screen wheel up = %q, want 3x Up arrow", string(written))
	}
}

func TestTerminalPanel_MenuAndChromeClicks(t *testing.T) {
	var newClicked bool
	var switchedTo string
	var closedID string
	var renamedID, renamedTitle string

	tabs := []terminalpanel.TabInfo{
		{ID: "t1", Title: "bash", Active: true},
		{ID: "t2", Title: "zsh", Active: false},
	}
	p := terminalpanel.New(12, 80, terminalpanel.Seams{
		Width:     func() int { return 84 },
		Height:    func() int { return 16 },
		Tabs:      func() []terminalpanel.TabInfo { return tabs },
		NewTab:    func() tea.Cmd { newClicked = true; return nil },
		SwitchTab: func(id string) tea.Cmd { switchedTo = id; return nil },
		CloseTab:  func(id string) tea.Cmd { closedID = id; return nil },
		Rename:    func(id, title string) { renamedID = id; renamedTitle = title },
	})

	rendered := ansi.Strip(p.Render())
	lines := strings.Split(rendered, "\n")
	if len(lines) < 3 {
		t.Fatalf("unexpected rendered lines: %d", len(lines))
	}

	// 1. Click [+ New] on row 0
	newIdx := strings.Index(lines[0], "[+ New]")
	if newIdx < 0 {
		t.Fatalf("header missing [+ New]: %q", lines[0])
	}
	newCol := ansi.StringWidth(lines[0][:newIdx])
	handled, _ := p.HandleMouse(tea.Mouse{X: newCol + 1, Y: 0, Button: tea.MouseLeft}, false, false)
	if !handled || !newClicked {
		t.Fatalf("clicking [+ New] failed: handled=%v newClicked=%v", handled, newClicked)
	}

	// 2. Click tab 2 on row 1 to switch
	tab2Idx := strings.Index(lines[1], "2:zsh")
	if tab2Idx < 0 {
		t.Fatalf("tab bar missing 2:zsh: %q", lines[1])
	}
	tab2Col := ansi.StringWidth(lines[1][:tab2Idx])
	handled, _ = p.HandleMouse(tea.Mouse{X: tab2Col + 1, Y: 1, Button: tea.MouseLeft}, false, false)
	if !handled || switchedTo != "t2" {
		t.Fatalf("clicking tab 2 failed: handled=%v switchedTo=%q", handled, switchedTo)
	}

	// 3. Click close '×' on tab 2
	closeIdx := strings.Index(lines[1][tab2Idx:], "×")
	if closeIdx < 0 {
		t.Fatalf("tab 2 missing close button: %q", lines[1])
	}
	closeCol := ansi.StringWidth(lines[1][:tab2Idx+closeIdx])
	handled, _ = p.HandleMouse(tea.Mouse{X: closeCol, Y: 1, Button: tea.MouseLeft}, false, false)
	if !handled || closedID != "t2" {
		t.Fatalf("clicking tab 2 close failed: handled=%v closedID=%q", handled, closedID)
	}

	// 4. Click [≡ Menu] on row 0 to open menu, then rename via keyboard
	menuIdx := strings.Index(lines[0], "[≡ Menu]")
	if menuIdx < 0 {
		t.Fatalf("header missing [≡ Menu]: %q", lines[0])
	}
	menuCol := ansi.StringWidth(lines[0][:menuIdx])
	handled, _ = p.HandleMouse(tea.Mouse{X: menuCol + 1, Y: 0, Button: tea.MouseLeft}, false, false)
	if !handled || !p.MenuOpen() {
		t.Fatalf("clicking [≡ Menu] did not open menu")
	}
	menuView := ansi.Strip(p.Render())
	if !strings.Contains(menuView, "Terminal Menu") || !strings.Contains(menuView, "Rename Session") {
		t.Fatalf("menu overlay missing expected items:\n%s", menuView)
	}

	// Move down to "Rename Session" (index 1), press Enter, type "build", press Enter
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	// Clear existing "bash" title with backspaces
	for i := 0; i < 10; i++ {
		p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	}
	for _, r := range "build" {
		p.HandleKey(tea.Key{Code: r, Text: string(r)})
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.MenuOpen() {
		t.Fatal("expected menu to close after rename Enter")
	}
	if renamedID != "t1" || renamedTitle != "build" {
		t.Fatalf("rename callback got (%q, %q), want (%q, %q)", renamedID, renamedTitle, "t1", "build")
	}
}
