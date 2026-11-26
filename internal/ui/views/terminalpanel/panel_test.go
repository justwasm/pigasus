package terminalpanel_test

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
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

func TestTerminalPanel_PassThroughEscAndStandalone(t *testing.T) {
	var written []byte
	var detached, quit bool

	p := terminalpanel.New(20, 100, terminalpanel.Seams{
		PassThroughEsc: true,
		Standalone:     true,
		Width:          func() int { return 100 },
		Height:         func() int { return 24 },
		WriteInput: func(b []byte) error {
			written = append(written, b...)
			return nil
		},
		Detach: func() tea.Cmd {
			detached = true
			return nil
		},
		Quit: func() tea.Cmd {
			quit = true
			return nil
		},
	})

	// Standalone fills full 24 rows (1 top + 22 body + 1 bottom)
	rendered := p.Render()
	lines := strings.Split(rendered, "\n")
	if len(lines) != 24 {
		t.Fatalf("standalone rendered lines = %d, want 24", len(lines))
	}
	if strings.Contains(rendered, "Esc: Detach") {
		t.Fatalf("standalone header should not show Esc: Detach:\n%s", lines[0])
	}
	if !strings.Contains(rendered, "Alt+Q: Quit") {
		t.Fatalf("standalone header should show Alt+Q: Quit:\n%s", lines[0])
	}

	// Esc in live mode passes 0x1B to PTY and does NOT detach
	handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatal("expected Esc to be handled")
	}
	if detached || quit {
		t.Fatalf("Esc must not detach (%v) or quit (%v) when PassThroughEsc=true", detached, quit)
	}
	if len(written) != 1 || written[0] != 0x1B {
		t.Fatalf("Esc written = %v, want [0x1B]", written)
	}
	written = nil

	// Shift+Tab encodes as CSI Z
	p.HandleKey(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
	if string(written) != "\x1b[Z" {
		t.Fatalf("Shift+Tab written = %q, want \\x1b[Z", written)
	}
}

func TestTerminalPanel_OmniBarBorderlessAndMenu(t *testing.T) {
	var switchedTo string
	var newTabCalled bool
	tabs := []terminalpanel.TabInfo{
		{ID: "t1", Title: "zsh", Active: true},
		{ID: "t2", Title: "pigasus", Active: false},
		{ID: "t3", Title: "server", Active: false},
	}

	p := terminalpanel.New(23, 80, terminalpanel.Seams{
		PassThroughEsc: true,
		Standalone:     true,
		OmniBar:        true,
		Width:          func() int { return 80 },
		Height:         func() int { return 24 },
		Cwd:            func() string { return "/Users/gear/GitHub/openagy" },
		Tabs:           func() []terminalpanel.TabInfo { return tabs },
		NewTab: func() tea.Cmd {
			newTabCalled = true
			return nil
		},
		SwitchTab: func(id string) tea.Cmd {
			switchedTo = id
			return nil
		},
		Quit: func() tea.Cmd { return nil },
	})
	p.WriteOutput([]byte("FULL_BLEED_LINE_1\r\nFULL_BLEED_LINE_2"))

	rendered := p.Render()
	lines := strings.Split(rendered, "\n")
	if len(lines) != 24 {
		t.Fatalf("OmniBar rendered lines = %d, want 24 (1 omni bar + 23 body)", len(lines))
	}
	if strings.Contains(rendered, "╭─") || strings.Contains(rendered, "╰─") || strings.Contains(rendered, "│ ") {
		t.Fatalf("OmniBar should not render box borders:\n%s", rendered)
	}
	topPlain := ansi.Strip(lines[0])
	if !strings.Contains(topPlain, "1:zsh") || !strings.Contains(topPlain, "2:pigasus") || !strings.Contains(topPlain, "[≡ Menu]") {
		t.Fatalf("OmniBar top row missing tabs or menu button: %q", topPlain)
	}
	if !strings.HasPrefix(ansi.Strip(lines[1]), "FULL_BLEED_LINE_1") {
		t.Fatalf("expected full-bleed body starting at col 0 on row 1, got %q", ansi.Strip(lines[1]))
	}

	// Selection extraction in OmniBar mode preserves col 0 and last row
	sel := terminalpanel.ExtractSelectionTextWithOptions(lines, 0, 0, 2, 16, true, true)
	if sel != "FULL_BLEED_LINE_1\nFULL_BLEED_LINE_2" {
		t.Fatalf("ExtractSelectionTextWithOptions = %q", sel)
	}

	// Click on tab 2 in OmniBar row 0
	tab2Byte := strings.Index(topPlain, "2:pigasus")
	if tab2Byte < 0 {
		t.Fatalf("could not locate 2:pigasus in %q", topPlain)
	}
	tab2X := ansi.StringWidth(topPlain[:tab2Byte])
	handled, _ := p.HandleMouse(tea.Mouse{X: tab2X + 1, Y: 0, Button: tea.MouseLeft}, false, false)
	if !handled || switchedTo != "t2" {
		t.Fatalf("click on tab 2: handled=%v switchedTo=%q", handled, switchedTo)
	}

	// Click on [+] button in OmniBar row 0
	plusByte := strings.Index(topPlain, "[+]")
	if plusByte < 0 {
		t.Fatalf("could not locate [+] in %q", topPlain)
	}
	plusX := ansi.StringWidth(topPlain[:plusByte])
	handled, _ = p.HandleMouse(tea.Mouse{X: plusX + 1, Y: 0, Button: tea.MouseLeft}, false, false)
	if !handled || !newTabCalled {
		t.Fatalf("click on [+]: handled=%v newTabCalled=%v", handled, newTabCalled)
	}

	// Open menu and verify session metadata moved into menu header
	p.SetMenuOpen(true)
	menuFrame := ansi.Strip(p.Render())
	if !strings.Contains(menuFrame, "Terminal Menu") || !strings.Contains(menuFrame, "80x23") || !strings.Contains(menuFrame, "cwd: /Users/gear/GitHub/openagy") {
		t.Fatalf("expected session metadata inside menu:\n%s", menuFrame)
	}
}

func TestTerminalPanel_ModifiedKeyPassthrough(t *testing.T) {
	var written []byte
	p := terminalpanel.New(24, 80, terminalpanel.Seams{
		PassThroughEsc: true,
		WriteInput: func(data []byte) error {
			written = append(written, data...)
			return nil
		},
	})

	cases := []struct {
		name string
		key  tea.Key
		want []byte
	}{
		{
			name: "Alt+Backspace (macOS Option+Backspace)",
			key:  tea.Key{Code: tea.KeyBackspace, Mod: tea.ModAlt},
			want: []byte{0x1B, 0x7F},
		},
		{
			name: "Ctrl+Backspace",
			key:  tea.Key{Code: tea.KeyBackspace, Mod: tea.ModCtrl},
			want: []byte{0x08},
		},
		{
			name: "Ctrl+Alt+Backspace",
			key:  tea.Key{Code: tea.KeyBackspace, Mod: tea.ModCtrl | tea.ModAlt},
			want: []byte{0x1B, 0x08},
		},
		{
			name: "Alt+Enter",
			key:  tea.Key{Code: tea.KeyEnter, Mod: tea.ModAlt},
			want: []byte{0x1B, '\r'},
		},
		{
			name: "Alt+Tab",
			key:  tea.Key{Code: tea.KeyTab, Mod: tea.ModAlt},
			want: []byte{0x1B, '\t'},
		},
		{
			name: "Alt+Shift+Tab",
			key:  tea.Key{Code: tea.KeyTab, Mod: tea.ModAlt | tea.ModShift},
			want: []byte("\x1b[1;4Z"),
		},
		{
			name: "Alt+Esc",
			key:  tea.Key{Code: tea.KeyEsc, Mod: tea.ModAlt},
			want: []byte{0x1B, 0x1B},
		},
		{
			name: "Ctrl+Alt+A",
			key:  tea.Key{Code: 'a', Mod: tea.ModCtrl | tea.ModAlt},
			want: []byte{0x1B, 0x01},
		},
		{
			name: "Ctrl+Alt+Space",
			key:  tea.Key{Code: tea.KeySpace, Mod: tea.ModCtrl | tea.ModAlt},
			want: []byte{0x1B, 0x00},
		},
		{
			name: "Ctrl+Slash",
			key:  tea.Key{Code: '/', Mod: tea.ModCtrl},
			want: []byte{0x1F},
		},
		{
			name: "Alt+Shift+A",
			key:  tea.Key{Code: 'a', ShiftedCode: 'A', Mod: tea.ModAlt | tea.ModShift},
			want: []byte("\x1bA"),
		},
		{
			name: "Alt+Delete",
			key:  tea.Key{Code: tea.KeyDelete, Mod: tea.ModAlt},
			want: []byte("\x1b[3;3~"),
		},
		{
			name: "Ctrl+Left",
			key:  tea.Key{Code: tea.KeyLeft, Mod: tea.ModCtrl},
			want: []byte("\x1b[1;5D"),
		},
		{
			name: "Shift+Up",
			key:  tea.Key{Code: tea.KeyUp, Mod: tea.ModShift},
			want: []byte("\x1b[1;2A"),
		},
		{
			name: "Alt+F4",
			key:  tea.Key{Code: tea.KeyF4, Mod: tea.ModAlt},
			want: []byte("\x1b[1;3S"),
		},
		{
			name: "Ctrl+F5",
			key:  tea.Key{Code: tea.KeyF5, Mod: tea.ModCtrl},
			want: []byte("\x1b[15;5~"),
		},
	}

	for _, tc := range cases {
		written = nil
		handled, _ := p.HandleKey(tc.key)
		if !handled {
			t.Fatalf("%s: expected handled=true", tc.name)
		}
		if string(written) != string(tc.want) {
			t.Fatalf("%s: written = %q (%v), want %q (%v)", tc.name, written, written, tc.want, tc.want)
		}
	}

	// When child app enables xterm modifyOtherKeys=2 (e.g. Bubbletea v2 / pigasus),
	// Shift+Enter, Ctrl+Enter, Ctrl+Shift+Z, and Ctrl+Backspace preserve full modifiers,
	// while Alt+Backspace continues sending universal ESC+DEL.
	p.WriteOutput([]byte("\x1b[>4;2m"))
	mokCases := []struct {
		name string
		key  tea.Key
		want []byte
	}{
		{
			name: "modifyOtherKeys Alt+Backspace",
			key:  tea.Key{Code: tea.KeyBackspace, Mod: tea.ModAlt},
			want: []byte{0x1B, 0x7F},
		},
		{
			name: "modifyOtherKeys Shift+Enter",
			key:  tea.Key{Code: tea.KeyEnter, Mod: tea.ModShift},
			want: []byte("\x1b[27;2;13~"),
		},
		{
			name: "modifyOtherKeys Ctrl+Enter",
			key:  tea.Key{Code: tea.KeyEnter, Mod: tea.ModCtrl},
			want: []byte("\x1b[27;5;13~"),
		},
		{
			name: "modifyOtherKeys Ctrl+Shift+Z",
			key:  tea.Key{Code: 'z', Mod: tea.ModCtrl | tea.ModShift},
			want: []byte("\x1b[27;6;122~"),
		},
		{
			name: "modifyOtherKeys Ctrl+Backspace",
			key:  tea.Key{Code: tea.KeyBackspace, Mod: tea.ModCtrl},
			want: []byte("\x1b[27;5;127~"),
		},
	}
	for _, tc := range mokCases {
		written = nil
		p.HandleKey(tc.key)
		if string(written) != string(tc.want) {
			t.Fatalf("%s: written = %q, want %q", tc.name, written, tc.want)
		}
	}

	// Reset modifyOtherKeys back to 0
	p.WriteOutput([]byte("\x1b[>4m"))
	written = nil
	p.HandleKey(tea.Key{Code: tea.KeyEnter, Mod: tea.ModShift})
	if string(written) != "\r" {
		t.Fatalf("after reset modifyOtherKeys, Shift+Enter = %q, want \\r", written)
	}
}
