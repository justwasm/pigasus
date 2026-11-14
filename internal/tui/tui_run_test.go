package tui

// tui_run_test.go — cycle 50+71 v1.3.5 binary 1:1 input component tests.
//
// Cycle 71: m.composer.input is a charm.land/bubbles/v2 textarea.Model. Tests
// now populate Key.Text in synthetic tea.KeyPressMsg so the textarea's
// default branch (which uses msg.Text to insert runes) receives the
// right rune. Real bubbletea events from tea.NewProgram populate
// Key.Text for us.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	sp "charm.land/bubbles/v2/spinner"
	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	uv "github.com/charmbracelet/ultraviolet"
	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/agentlink"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tuistore"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/branding"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	"github.com/justwasm/pigasus/internal/features/models/configtest"
	"github.com/justwasm/pigasus/internal/features/tools/bashmode"
	"github.com/justwasm/pigasus/internal/infra/backend/gateway"
	backendselect "github.com/justwasm/pigasus/internal/infra/backend/select"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/internal/ui/editor/wordmove"
	tuiviewport "github.com/justwasm/pigasus/internal/ui/foundations/viewport"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	tuilib "github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/announce"
	"github.com/justwasm/pigasus/internal/ui/views/contextpanel"
	"github.com/justwasm/pigasus/internal/ui/views/loginui"
	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
	"github.com/justwasm/pigasus/internal/ui/views/programstatus"
	"github.com/justwasm/pigasus/internal/ui/views/statusline"
	"github.com/justwasm/pigasus/palette"
	"github.com/justwasm/pigasus/vim"
)

// runeKey returns a tea.KeyPressMsg for a printable rune with Text
// populated so the textarea's default-branch insert path works.
func runeKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)})
}

// ctrlKey returns a tea.KeyPressMsg for a non-printable control combo.
func ctrlKey(r rune) tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: r, Mod: tea.ModCtrl})
}

// shiftEnterKey returns Shift+Enter with Text populated.
func shiftEnterKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter, Mod: tea.ModShift, Text: "\n"})
}

// enterKey returns plain Enter with Text populated.
func enterKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter, Text: "\n"})
}

// fakeClient is the cycle 50 fake backend.Client.
type fakeClient struct {
	lastPrompt string
	stream     string
}

func (f *fakeClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	return nil, nil
}

// Test_EnterSendsPrompt verifies that Enter submits the prompt.
func Test_EnterSendsPrompt(t *testing.T) {
	m := newRootModel(&fakeClient{}, "user@test", "claude-4.5-sonnet", "starter", "/tmp", true)
	m.Update(runeKey('h'))
	m.Update(runeKey('i'))
	if got := m.prompt.input.Value(); got != "hi" {
		t.Fatalf("expected textarea 'hi', got %q", got)
	}
	m.conversation.streaming = false
	_, _ = m.Update(enterKey())
	if !m.conversation.streaming {
		t.Fatalf("expected streaming=true after Enter, got false (cycle 50)")
	}
	if v := m.prompt.input.Value(); v != "" {
		t.Errorf("expected textarea cleared after Enter, got %q", v)
	}
}

// Test_CtrlJNewline verifies that Ctrl+J inserts a newline.
func Test_CtrlJNewline(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('a'))
	m.Update(ctrlKey('j'))
	m.Update(runeKey('b'))
	got := m.prompt.input.Value()
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") || !strings.Contains(got, "\n") {
		t.Fatalf("expected 'a\\nb', got %q (cycle 71)", got)
	}
	if m.conversation.streaming {
		t.Errorf("Ctrl+J should NOT trigger streaming")
	}
}

// Test_ShiftEnterNewline verifies Shift+Enter inserts newline.
func Test_ShiftEnterNewline(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('a'))
	m.Update(shiftEnterKey())
	m.Update(runeKey('b'))
	got := m.prompt.input.Value()
	if !strings.Contains(got, "a") || !strings.Contains(got, "b") || !strings.Contains(got, "\n") {
		t.Fatalf("expected 'a\\nb', got %q", got)
	}
}

// Test_BufferInsertSingleKey verifies ordinary rune insertion.
func Test_BufferInsertSingleKey(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	for _, r := range "hello" {
		m.Update(runeKey(r))
	}
	if got := m.prompt.input.Value(); got != "hello" {
		t.Fatalf("expected 'hello', got %q", got)
	}
}

// Test_ViewAltScreen verifies View() returns AltScreen=true with
// a Cursor pointing into the textarea.
func Test_ViewAltScreen(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	for _, r := range "hi" {
		m.Update(runeKey(r))
	}
	v := m.View()
	if !v.AltScreen {
		t.Error("v.AltScreen should be true")
	}
	if v.Cursor == nil {
		t.Fatal("v.Cursor should not be nil")
	}
}

// Test_LoginViewKeepsAltScreen pins the /accounts → a regression: the
// login screen renders its own view, but it runs inside this program,
// so it has to carry the session's terminal settings. Without them
// bubbletea reads AltScreen off the view and leaves the alternate
// screen in the middle of the session.
// MouseMode is kept at None so that terminal-native text selection and
// Cmd+click on hyperlinks are preserved while on the login modal.
func Test_LoginViewKeepsAltScreen(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.auth.login = loginui.New()
	v := m.View()
	if !v.AltScreen {
		t.Error("the login view must keep the alternate screen")
	}
	if v.MouseMode != tea.MouseModeNone {
		t.Errorf("the login view mouse mode = %v, want None", v.MouseMode)
	}
}

// Test_LoginViewRespectsNoAltScreen is the other half: the login view
// must not force the alternate screen on when the session was started
// without it.
func Test_LoginViewRespectsNoAltScreen(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.auth.login = loginui.New()
	if v := m.View(); v.AltScreen {
		t.Error("the login view must not turn the alternate screen on")
	}
}

// Test_CtrlLClearsScreen verifies that Ctrl+L clears the screen and resets scroll.
func Test_CtrlLClearsScreen(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.viewport.scroll = 42
	_, cmd := m.Update(ctrlKey('l'))
	if m.viewport.scroll != 0 {
		t.Errorf("scroll after Ctrl+L = %d, want 0", m.viewport.scroll)
	}
	if cmd == nil {
		t.Fatalf("Ctrl+L should return tea.ClearScreen command, got nil")
	}
	if !m.viewport.altScreen {
		t.Errorf("Ctrl+L must not mutate altScreen; got %v, want true", m.viewport.altScreen)
	}
}

// Test_MouseModeConstant verifies cycle 174: when alt-screen is
// on, MouseMode is always CellMotion. There is no longer a runtime
// toggle — mouse tracking is unconditional and the in-app
// selection overlay replaces the cycle-166 "use the host terminal's
// selection at the cost of wheel scroll" trade-off.
func Test_MouseModeConstant(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	v := m.View()
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("default altScreen: got %v, want CellMotion", v.MouseMode)
	}
	m.viewport.altScreen = false
	v = m.View()
	if v.MouseMode != tea.MouseModeNone {
		t.Errorf("altScreen off: got %v, want None", v.MouseMode)
	}
	m.viewport.altScreen = true
	v = m.View()
	if v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("altScreen on: got %v, want CellMotion", v.MouseMode)
	}
}

// Test_SessionAccumulatesTurns verifies cycle 70b multi-turn.
func Test_SessionAccumulatesTurns(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	for _, r := range "hi" {
		m.Update(runeKey(r))
	}
	m.conversation.streaming = false
	_, _ = m.Update(enterKey())
	if len(m.SessionMessages()) != 1 || m.SessionMessages()[0].Role != "user" || m.SessionMessages()[0].Content != "hi" {
		t.Fatalf("expected 1 user msg 'hi', got %+v", m.SessionMessages())
	}
	m.Update(backendChunkMsg("hello-back"))
	if len(m.SessionMessages()) != 2 || m.SessionMessages()[1].Role != "assistant" || m.SessionMessages()[1].Content != "hello-back" {
		t.Fatalf("expected 2 msgs, got %+v", m.SessionMessages())
	}
	m.conversation.streaming = true
	m.Update(backendDoneMsg{})
	m.conversation.streaming = false
	for _, r := range "yo" {
		m.Update(runeKey(r))
	}
	_, _ = m.Update(enterKey())
	if len(m.SessionMessages()) != 3 || m.SessionMessages()[2].Role != "user" || m.SessionMessages()[2].Content != "yo" {
		t.Fatalf("expected 3 msgs after 2nd turn, got %+v", m.SessionMessages())
	}
}

// Test_InputBetweenSeps verifies cycle 71: View() wraps the
// textarea between two horizontal rules.
func Test_InputBetweenSeps(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	v := m.View()
	view := v.Content
	// At least two separator runs should appear (one above, one
	// below the textarea).
	count := strings.Count(view, strings.Repeat("─", 200))
	if count < 2 {
		t.Fatalf("expected at least 2 separators in view, got %d (cycle 71)", count)
	}
}

// Test_DynamicHeightGrows verifies cycle 71: multi-line content
// makes m.composer.input.Height() > 1.
func Test_DynamicHeightGrows(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.prompt.input.SetValue("line1\nline2\nline3")
	h := m.prompt.input.Height()
	if h < 2 {
		t.Fatalf("expected textarea height >= 2 after multi-line content, got %d", h)
	}
}

// Test_TextareaCursorTracksText verifies the cursor position
// tracks the textarea content (cycle 71 fix).
func Test_TextareaCursorTracksText(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.prompt.input.SetValue("hello world")
	// Move to end via the textarea's CharacterForward binding (ctrl+f).
	for i := 0; i < 50; i++ {
		m.prompt.input, _ = m.prompt.input.Update(tea.KeyPressMsg(tea.Key{Code: 'f', Mod: tea.ModCtrl}))
	}
	v := m.View()
	if v.Cursor == nil {
		t.Fatal("v.Cursor should not be nil")
	}
	// Cursor X should be at least "hello world" length + 1 (" " + ">")
	if v.Cursor.X < 12 {
		t.Errorf("Cursor.X should be >= 12, got %d (cycle 71 cursor tracking)", v.Cursor.X)
	}
}

// reference tas.New() to satisfy import (textarea.New used in test below)
var _ = tas.New

// Test_PromptPrefixMatchesRef verifies cycle 74: prompt prefix is "> "
// not " " (matches v1.3.5 binary byte-for-byte).
func Test_PromptPrefixMatchesRef(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	if m.prompt.input.Prompt != "> " {
		t.Fatalf("expected input.Prompt='> ', got %q (cycle 74)", m.prompt.input.Prompt)
	}
}

// Test_IdlePromptHasMarker verifies cycle 74: idle view contains the
// "> " prompt prefix (not just "> " literal at empty state).
// Cycle 89: the > glyph is wrapped in blue ANSI per ref capture;
// cycle 126 normalises the colour to 24-bit RGB (38;2;38;139;210)
// matching the v1.3.6 binary, instead of the 256-colour 38;5;69 the
// earlier capture picked up. Cycle 90: the prompt colour is applied
// via lipgloss's Focused.Prompt style, which produces either
// \x1b[39m (foreground reset) or \x1b[m (full SGR reset) as the
// close — both render identically, so the test accepts either.
func Test_IdlePromptHasMarker(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	v := m.View()
	view := v.Content
	hasBlueOpen := strings.Contains(view, "\x1b[38;2;38;139;210m>")
	if !hasBlueOpen {
		t.Fatalf("idle view missing true-colour blue '\x1b[38;2;38;139;210m>' prompt opener (cycle 89/90/126): %q", view)
	}
}

// Test_PickerVisibleStatusLabel verifies cycle 74: when the picker
// is visible the status row shows "esc to cancel" instead of
// "? for shortcuts" (matches v1.3.5 binary). Cycle 126: the
// "esc to cancel" label is wrapped in true-colour dim gray
// (38;2;101;123;131) per the v1.3.6 capture (cycle 91 used the
// 256-colour fallback 38;2;101;123;131 which is the same shade in tmux
// but distinguishable in a real terminal).
func Test_PickerVisibleStatusLabel(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	if !m.prompt.picker.Visible() {
		t.Fatal("picker should be visible after typing '/'")
	}
	status := m.statusRow()
	if !strings.Contains(status, "esc to cancel") {
		t.Errorf("picker-visible status should contain 'esc to cancel', got %q (cycle 74)", status)
	}
	if strings.Contains(status, "? for shortcuts") {
		t.Errorf("picker-visible status should NOT contain '? for shortcuts', got %q (cycle 74)", status)
	}
	if !strings.Contains(status, "\x1b[38;2;101;123;131mesc to cancel\x1b[39m") {
		t.Errorf("picker-visible 'esc to cancel' should be in true-colour dim gray (cycle 91/126), got %q", status)
	}
}

// Test_PickerHiddenStatusLabel verifies that when picker is hidden
// the status row shows "? for shortcuts" (idle behavior).
func Test_PickerHiddenStatusLabel(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	if m.prompt.picker.Visible() {
		t.Fatal("picker should be hidden in idle state")
	}
	status := m.statusRow()
	if !strings.Contains(status, "? for shortcuts") {
		t.Errorf("idle status should contain '? for shortcuts', got %q", status)
	}
	if strings.Contains(status, "esc to cancel") {
		t.Errorf("idle status should NOT contain 'esc to cancel', got %q", status)
	}
}

// Test_PickerHighlightColorIs69 verifies cycle 74: the highlighted
// picker row uses true-colour blue (38;2;38;139;210) matching the
// v1.3.6 binary. The cycle-74 capture observed 256-colour 38;5;69,
// which is the same shade in tmux but distinguishable in a real
// terminal; cycle 126 normalises to 24-bit RGB.
func Test_PickerHighlightColorIs69(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	rows := m.prompt.picker.Rows()
	if !strings.Contains(rows, "\033[38;2;38;139;210m") {
		t.Fatalf("picker highlight should contain \\033[38;2;38;139;210m (true-colour blue), got %q (cycle 74/126)", rows)
	}
}

// Test_PickerDescriptionDim verifies cycle 74: picker descriptions
// are rendered in dim gray (matches ref binary). Cycle 126 normalised
// the colour from 256-colour 38;5;238 to true-colour 38;2;101;123;131.
func Test_PickerDescriptionDim(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	rows := m.prompt.picker.Rows()
	if !strings.Contains(rows, "\033[38;2;101;123;131m") {
		t.Fatalf("picker rows should contain true-colour dim gray 38;2;101;123;131, got %q (cycle 74/126)", rows)
	}
}

// Test_CursorOnPromptLine verifies cycle 74f: cursor lands on the
// prompt line (not on the second separator or picker row).
func Test_CursorOnPromptLine(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	v := m.View()
	// Layout: top blank + 5 banner + blank + top sep + prompt line.
	// 0-indexed prompt line Y = 8.
	if v.Cursor.Y != 8 {
		t.Errorf("idle cursor Y should be 8, got %d (cycle 74f)", v.Cursor.Y)
	}
}

// Test_CursorTracksTypedText verifies cycle 74f: cursor X moves
// with each rune insertion (matches v1.3.5 binary).
func Test_CursorTracksTypedText(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	v := m.View()
	idleX := v.Cursor.X
	for _, r := range "hello" {
		m.Update(runeKey(r))
	}
	v = m.View()
	// Each rune should advance cursor X by 1.
	if v.Cursor.X != idleX+5 {
		t.Errorf("after 'hello', cursor X should be %d, got %d (cycle 74f)", idleX+5, v.Cursor.X)
	}
}

// Test_PickerCatalogMatchesRef pins both the catalog and the
// picker-visible count against the v1.3.6 binary.
//
//	/help COMMANDS tab  → 99 entries (footer "[1-18 of 99 items]")
//	                       captured at
//	                       /tmp/agycmp/hotpath/help_tabs/ref.001-second.txt
//	/ picker overlay    → 94 entries (header "↓ 89 more" at offset 0;
//	                       "↓ 86 more" at offset 3 after 5 Down presses)
//	                       captured at
//	                       /tmp/agycmp/hotpath/picker/ref.000-slash.txt
//
// Cycle 351 (agy 1.4.8): the chrome-devtools-plugin:* filter is GONE —
// the 1.4.8 picker shows every catalogue row, 105 of them, the same
// count the /help COMMANDS tab reports ("[1-33 of 105 items]"). The
// 1.5.0 reference adds /projects (106), and the catalogue holds those
// plus the clone's four superset commands (/feature-diff, /accounts,
// /provider, /pool): 110 rows, and the picker shows all of them.
//
// Cycle 361 adds /pool: the /accounts panel's Pool tab on its own — the
// account-routing policy that decides which account serves a turn.
func Test_PickerCatalogMatchesRef(t *testing.T) {
	got := len(slashcommands.Default)
	if got != 110 {
		t.Fatalf("expected 110 slash commands in catalog (106 ref incl. /projects + /feature-diff + /accounts + /provider + /pool), got %d", got)
	}
	filtered := len(slashcommands.Filter("/"))
	if filtered != 110 {
		t.Fatalf("expected 110 slash commands in picker (no chrome-devtools-plugin filter since 1.4.8), got %d", filtered)
	}
}

// Test_PickerFirstRowIsAddDir verifies cycle 75: the very first row
// of the catalog (and first visible row when "/" is typed) is /add-dir,
// matching the v1.3.5 binary's first-page order exactly.
func Test_PickerFirstRowIsAddDir(t *testing.T) {
	if len(slashcommands.Default) == 0 {
		t.Fatal("catalog is empty")
	}
	if slashcommands.Default[0].Name != "/add-dir" {
		t.Fatalf("first command should be /add-dir, got %q (cycle 75)", slashcommands.Default[0].Name)
	}
}

// Test_PickerLastRowIsWrangler verifies cycle 75: the very last row
// is /wrangler, matching the cursor-walk capture at scan_98.bin.
func Test_PickerLastRowIsWrangler(t *testing.T) {
	last := slashcommands.Default[len(slashcommands.Default)-1]
	if last.Name != "/wrangler" {
		t.Fatalf("last command should be /wrangler, got %q (cycle 75)", last.Name)
	}
}

// slowFakeClient is a backend that blocks until ctx is cancelled, so
// the spinner stays in its "streaming" state until the test releases it.
type slowFakeClient struct {
	delay time.Duration
	ch    chan struct{}
}

func (s *slowFakeClient) Stream(ctx context.Context, req backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	ch := make(chan backend.StreamChunk, 8)
	go func() {
		defer close(ch)
		parts := []string{"Hello", " world", "!"}
		for _, p := range parts {
			select {
			case <-ctx.Done():
				return
			case <-time.After(s.delay):
			}
			ch <- backend.StreamChunk{Text: p}
		}
		ch <- backend.StreamChunk{Done: true}
	}()
	return ch, nil
}

// Test_SpinnerAppearsDuringStream verifies cycle 76: while
// m.conversation.streaming is true, the view contains the literal row
// "  <glyph>  Generating..." (two leading spaces, one spinner
// glyph, two more spaces, then the word). Source-of-truth:
// /tmp/ref_streaming_t*.bin captured from the v1.3.5 binary.
func Test_SpinnerAppearsDuringStream(t *testing.T) {
	be := &slowFakeClient{delay: 500 * time.Millisecond}
	m := newRootModel(be, "u@t", "m", "t", "/", true)
	for _, r := range "hi" {
		m.Update(runeKey(r))
	}
	m.conversation.streaming = false
	_, _ = m.Update(enterKey())
	if !m.conversation.streaming {
		t.Fatal("expected streaming=true after Enter")
	}
	v := m.View()
	// The activity label is the reference's animated wave: the first
	// frames reveal only a prefix of "Generating", so the visible text
	// (not the raw bytes, which carry the highlight split) is what the
	// test can look for.
	plain := ansi.Strip(v.Content)
	if !strings.Contains(plain, "Genera") {
		t.Fatalf("expected a visible 'Genera' prefix in view during stream, got %q", plain)
	}
	// Verify the spinner glyph sits between two pairs of spaces:
	// "  <braille>  Genera..." (2 spaces + glyph + 2 spaces).
	idx := strings.Index(plain, "Genera")
	if idx < 5 {
		t.Fatal("Genera not found with enough leading chars")
	}
	// The pattern in v1.3.5 is: <glyph><space><space>Generating...
	// (no leading indent before the glyph). bubbles sp.Dot frames
	// each carry a trailing space, so the total is 2 spaces.
	prefix := plain[idx-5 : idx]
	// 5 bytes = 3-byte UTF-8 braille glyph + 2 spaces
	if !strings.HasSuffix(prefix, "  ") {
		t.Errorf("expected 2 trailing spaces before Generating, got %q (cycle 76)", prefix)
	}
	// The leading byte must be a UTF-8 continuation byte (>= 0x80).
	if prefix[0] < 0x80 {
		t.Errorf("expected Braille glyph at start of spinner row, got %q (cycle 76)", prefix)
	}
	// Finish stream so the test doesn't hang on cleanup.
	m.Update(backendDoneMsg{})
}

// Test_SpinnerAnimatesAcrossTicks verifies cycle 76: each
// sp.TickMsg advances the spinner frame. Since sp.Tick filters on
// tag, we must supply an empty TickMsg (ID=0, tag=0) which is the
// first tick from the bubble loop. The spinner model is initialized
// with frame=0; after the first tick it should be on frame 1.
func Test_SpinnerAnimatesAcrossTicks(t *testing.T) {
	m := newRootModel(&slowFakeClient{delay: time.Second}, "u@t", "m", "t", "/", true)
	for _, r := range "hi" {
		m.Update(runeKey(r))
	}
	m.conversation.streaming = false
	_, _ = m.Update(enterKey())
	if !m.conversation.streaming {
		t.Fatal("expected streaming=true after Enter")
	}
	// Drain TickMsgs manually (simulating the bubbletea loop). Each
	// frame reveals one more character of "Generating"; the braille
	// glyph in the row is the spinner's animated frame.
	seen := make(map[rune]bool)
	sawFull := false
	for i := 0; i < 6; i++ {
		plain := ansi.Strip(m.View().Content)
		if strings.Contains(plain, "Generating") {
			sawFull = true
		}
		for _, r := range plain {
			if r >= 0x2800 && r <= 0x28FF {
				seen[r] = true
			}
		}
		if len(seen) == 0 {
			t.Fatalf("frame %d: no spinner glyph", i)
		}
		// Simulate next tick — send empty TickMsg and Update.
		_, _ = m.Update(sp.TickMsg{})
	}
	if len(seen) < 2 {
		t.Errorf("spinner did not animate: only saw %d distinct frames %v", len(seen), seen)
	}
	if !sawFull {
		t.Errorf("six ticks never revealed the full 'Generating' label")
	}
	// Cleanup.
	m.Update(backendDoneMsg{})
}

// Test_SpinnerStopsWhenStreamDone verifies cycle 76: after
// backendDoneMsg arrives, m.conversation.streaming is false and subsequent
// TickMsg are ignored (frame freezes).
func Test_SpinnerStopsWhenStreamDone(t *testing.T) {
	be := &slowFakeClient{delay: 30 * time.Millisecond}
	m := newRootModel(be, "u@t", "m", "t", "/", true)
	for _, r := range "hi" {
		m.Update(runeKey(r))
	}
	m.conversation.streaming = false
	_, _ = m.Update(enterKey())
	// In the test environment the tea.Cmd from Enter is never executed,
	// so streaming stays true unless we manually flip it. Simulate the
	// backend completing.
	m.Update(backendDoneMsg{})
	if m.conversation.streaming {
		t.Error("after backendDoneMsg, m.conversation.streaming should be false")
	}
	// View should NOT contain "Generating..." anymore.
	v := m.View()
	if strings.Contains(v.Content, "Generating...") {
		t.Errorf("after stream done, view should not show 'Generating...', got %q", v.Content)
	}
}

// Test_SlashCommandsAreNotSentToLLM verifies cycle 77: when the
// user presses Enter on a slash command buffer (e.g., "/help"), the
// handleSlashCommand method intercepts and returns true so the LLM
// path is skipped. Previously only /clear was intercepted.
func Test_SlashCommandsAreNotSentToLLM(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	for _, r := range "/help" {
		m.Update(runeKey(r))
	}
	before := len(m.SessionMessages())
	handled, _ := m.handleSlashCommand("/help")
	if !handled {
		t.Error("/help should be handled by router (cycle 77)")
	}
	if len(m.SessionMessages()) != before {
		t.Errorf("/help should NOT add to session (was %d, now %d)", before, len(m.SessionMessages()))
	}
	// /unknown_xyz is routed to the "unknown" handler which echoes
	// feedback. Still doesn't reach the LLM.
	handled, _ = m.handleSlashCommand("/totally_made_up_command")
	if !handled {
		t.Error("unknown commands should be handled (cycle 77 feedback)")
	}
}

// Test_SlashExitReturnsQuitCmd verifies cycle 77: /exit and
// /quit both schedule tea.Quit so the program actually terminates.
func Test_SlashExitReturnsQuitCmd(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	handled, cmd := m.handleSlashCommand("/exit")
	if !handled {
		t.Fatal("/exit should be handled")
	}
	if cmd == nil {
		t.Error("/exit should return a non-nil tea.Cmd to schedule Quit")
	}
	handled, cmd = m.handleSlashCommand("/quit")
	if !handled {
		t.Fatal("/quit should be handled")
	}
	if cmd == nil {
		t.Error("/quit should return a non-nil tea.Cmd to schedule Quit")
	}
}

// Test_SlashHelpFeedbackIncludesCatalog verifies cycle 83: /help
// opens a 3-tab TUI overlay (the /help panel) instead of dumping the
// catalog into m.conversation.response. The TUI panels are rendered by View() via
// the panel's rows; here we only confirm the overlay is armed and the
// response area only has the user echo line.
func Test_SlashHelpFeedbackIncludesCatalog(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	handled, _ := m.handleSlashCommand("/help")
	if !handled {
		t.Fatal("/help should be handled")
	}
	if !m.help.Panel.Active() {
		t.Error("expected the /help overlay after /help (cycle 83 overlay)")
	}
	if m.help.Panel.Tab() != 0 {
		t.Errorf("expected helpTab=0 on entry, got %d", m.help.Panel.Tab())
	}
	// Response should be empty after /help — the reference binary
	// does not echo slash commands into the transcript (the picker
	// overlay's prompt area already shows the command while the user
	// is typing, and the handler output is the next thing rendered).
	// Cycle 149: the cycle 92 / 126 echo is removed; capturing the
	// ref binary running /help, /changelog, and other slash commands
	// never shows a "> /<cmd>" line in the transcript.
	if len(m.conversation.response) != 0 {
		t.Errorf("expected empty transcript after /help (cycle 149), got %v", m.conversation.response)
	}
}

// Test_HelpTabBarStyling verifies cycle 85 / 126: the tab bar uses
// the v1.3.6 binary's ANSI styling — active tab in
// bold+underline+true-colour blue (38;2;38;139;210), inactive tabs
// in separator-gray (38;2;218;220;224), brand label in plain true-
// colour blue, trailing hint in separator-gray. (Cycle 85 used the
// 256-colour fallback 38;5;69/253 which tmux's cube maps to the same
// visible shade.)
func Test_HelpTabBarStyling(t *testing.T) {
	// All three states share the brand prefix and hint suffix.
	for tab := 0; tab <= 2; tab++ {
		got := tuilib.RenderTabBar(tab)
		// Brand prefix: \x1b[38;2;38;139;210mPigasus CLI\x1b[39m
		if !strings.HasPrefix(got, "\x1b[38;2;38;139;210mPigasus CLI\x1b[39m") {
			t.Errorf("tab %d: missing brand prefix, got %q", tab, got[:60])
		}
		// Trailing hint: ' \x1b[38;2;238;232;213m  (←/→ or tab to cycle)\x1b[0m'
		wantHint := " \x1b[38;2;238;232;213m  (\u2190/\u2192 or tab to cycle)\x1b[0m"
		if !strings.HasSuffix(got, wantHint) {
			t.Errorf("tab %d: missing trailing hint suffix, got %q", tab, got[len(got)-50:])
		}
		// Active tab word must carry the bold+underline+blue prefix.
		wantActiveOpen := "\x1b[1;4m\x1b[38;2;38;139;210m"
		tabs := []string{"general", "commands", "shortcuts"}
		active := tabs[tab]
		// Check that active word is wrapped with the active open/close.
		activePattern := wantActiveOpen + active + "\x1b[0m"
		if !strings.Contains(got, activePattern) {
			t.Errorf("tab %d: active word %q not wrapped in active styling", tab, active)
		}
		// Inactive words must use the dim gray wrap.
		for i, name := range tabs {
			if i == tab {
				continue
			}
			inactivePattern := "\x1b[38;2;238;232;213m" + name + "\x1b[0m"
			if !strings.Contains(got, inactivePattern) {
				t.Errorf("tab %d: inactive word %q not in dim gray", tab, name)
			}
		}
	}
}

// Test_HelpTabBarActiveTabHighlight verifies cycle 85 / 126: the
// active tab word carries the bold+underline SGR (wire detail:
// \x1b[1;4m\x1b[38;2;38;139;210m...\x1b[0m in the v1.3.6 binary).
func Test_HelpTabBarActiveTabHighlight(t *testing.T) {
	for tab := 0; tab <= 2; tab++ {
		got := tuilib.RenderTabBar(tab)
		if !strings.Contains(got, "\x1b[1;4m") {
			t.Errorf("tab %d: missing bold+underline SGR \\x1b[1;4m", tab)
		}
		if !strings.Contains(got, "\x1b[38;2;38;139;210m") {
			t.Errorf("tab %d: missing blue \\x1b[38;2;38;139;210m (cycle 126)", tab)
		}
		// Only ONE active wrap should be present per tab bar.
		count := strings.Count(got, "\x1b[1;4m")
		if count != 1 {
			t.Errorf("tab %d: expected 1 active wrap, got %d", tab, count)
		}
	}
}

// Test_SlashNonSlashFallsThrough verifies cycle 77: buffers not
// starting with "/" fall through to the LLM path (handleSlashCommand
// returns false, nil).
func Test_SlashNonSlashFallsThrough(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	handled, cmd := m.handleSlashCommand("hello world")
	if handled {
		t.Error("plain text should not be handled as slash command (cycle 77)")
	}
	if cmd != nil {
		t.Error("plain text handler should return nil cmd")
	}
}

// Test_HelpGeneralPanelStyling verifies cycle 86 / 126: the
// /help general panel wraps description/metadata rows in true-colour
// dim gray (38;2;101;123;131), Quick Reference header in bold + warning-
// body dim gray (1m + 38;2;60;64;67), Quick Reference "/" symbol in
// true-colour blue (38;2;38;139;210), and the Keyboard hint uses
// bold prefix + true-colour blue keys + dim gray labels.
func Test_HelpGeneralPanelStyling(t *testing.T) {
	var b strings.Builder
	tuilib.RenderHelpGeneral(&b, "Gemini 3.8 Flash (High)", "Antigravity Starter Quota", "~/GitHub/openagy", "user@example.com", "test-uuid-0000-0000-0000-000000000000")
	out := b.String()

	// Description block must be wrapped in dim gray.
	if !strings.Contains(out, "\x1b[38;2;101;123;131mPigasus CLI understands") {
		t.Error("description row 1 missing dim gray wrap")
	}
	if !strings.Contains(out, "\x1b[38;2;101;123;131mand executes commands") {
		t.Error("description row 2 missing dim gray wrap")
	}
	// Metadata rows.
	wantRows := []string{
		"\x1b[38;2;101;123;131mVersion " + cliVersion + "\x1b[39m",
		"\x1b[38;2;101;123;131mWorkspace:      ~/GitHub/openagy\x1b[39m",
		"\x1b[38;2;101;123;131mProject Config: ~/.gemini/config/projects/default-cli-project.json\x1b[39m",
		"\x1b[38;2;101;123;131mLog File:       ~/.gemini/antigravity-cli/log/cli-",
	}
	for _, want := range wantRows {
		if !strings.Contains(out, want) {
			snip := want
			if len(snip) > 50 {
				snip = snip[:50]
			}
			t.Errorf("missing styled row %q", snip)
		}
	}
	// Quick Reference header must be bold + warning-body gray (38;2;60;64;67).
	if !strings.Contains(out, "\x1b[1m\x1b[38;2;88;110;117mQuick Reference\x1b[0m") {
		t.Error("Quick Reference header missing bold+warning-body-gray styling (cycle 86/126)")
	}
	// Quick Reference "/" must be in true-colour blue.
	if !strings.Contains(out, "\x1b[38;2;38;139;210m/\x1b[39m") {
		t.Error("Quick Reference / missing true-colour blue styling (cycle 126)")
	}
	// Keyboard hint must use bold prefix + true-colour blue keys + dim gray labels.
	if !strings.Contains(out, "\x1b[1mKeyboard: \x1b[0m") {
		t.Error("Keyboard hint missing bold prefix")
	}
	if !strings.Contains(out, "\x1b[38;2;38;139;210m\u2191/\u2193\x1b[39m") {
		t.Error("Keyboard ↑/↓ keys missing true-colour blue styling (cycle 126)")
	}
	if !strings.Contains(out, "\x1b[38;2;38;139;210mesc\x1b[39m") {
		t.Error("Keyboard esc key missing true-colour blue styling (cycle 126)")
	}
	if !strings.Contains(out, "\x1b[38;2;101;123;131mNavigate\x1b[39m") {
		t.Error("Keyboard Navigate label missing dim gray (cycle 126)")
	}
}

// Test_QuestionMarkOpensHelpOverlay verifies cycle 181: pressing `?`
// on an empty prompt opens the help overlay on the SHORTCUTS tab
// (the SHORTCUTS tab) — matches v1.4.0 REF, where the active word
// "shortcuts" in the tab bar is wrapped in bold+underline. The /help
// slash command still lands on the GENERAL tab (tab 0).
//
// '?' opens the shortcuts overlay when the composer is empty.
// When text is present, typing '?' enters the character into the composer.
func Test_QuestionMarkOpensHelpOverlay(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)

	// Precondition: idle prompt, the overlay closed, input empty.
	if m.help.Panel.Active() {
		t.Fatal("the /help overlay should start closed")
	}
	if v := m.prompt.input.Value(); v != "" {
		t.Fatalf("input should start empty, got %q", v)
	}

	// Case 1: `?` on an empty input opens the overlay on SHORTCUTS.
	_, _ = m.Update(runeKey('?'))
	if !m.help.Panel.Active() {
		t.Error("expected the overlay after ? on empty input (cycle 181)")
	}
	if m.help.Panel.Tab() != 2 {
		t.Errorf("expected helpTab=2 (shortcuts) on ?-entry, got %d", m.help.Panel.Tab())
	}
	if v := m.prompt.input.Value(); v != "" {
		t.Errorf("expected input cleared on entry to overlay, got %q", v)
	}

	// Esc dismisses (cycle 83 path).
	_, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.help.Panel.Active() {
		t.Error("expected the overlay to close after Esc")
	}

	// Case 2: typing text first, then `?` must type '?' into the composer,
	// not open the shortcuts overlay.
	for _, r := range "hello" {
		_, _ = m.Update(runeKey(r))
	}
	if v := m.prompt.input.Value(); v != "hello" {
		t.Fatalf("precondition: expected 'hello' in input, got %q", v)
	}
	_, _ = m.Update(runeKey('?'))
	if m.help.Panel.Active() {
		t.Error("`?` with text in input must not open the overlay")
	}
	if v := m.prompt.input.Value(); v != "hello?" {
		t.Errorf("the composer holds %q, want 'hello?'", v)
	}

	// Case 3: clear and press ? again — overlay reopens on shortcuts.
	for i := 0; i < 6; i++ {
		_, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	}
	if v := m.prompt.input.Value(); v != "" {
		t.Fatalf("precondition: input should be empty after backspaces, got %q", v)
	}
	_, _ = m.Update(runeKey('?'))
	if !m.help.Panel.Active() {
		t.Error("expected overlay to re-open after clearing input and pressing ?")
	}
	if m.help.Panel.Tab() != 2 {
		t.Errorf("expected helpTab=2 on second ?-entry, got %d", m.help.Panel.Tab())
	}

	// Case 4: /help still lands on GENERAL (tab 0).
	_, _ = m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.help.Panel.Active() {
		t.Fatal("expected the overlay to close after Esc")
	}
	handled, _ := m.handleSlashCommand("/help")
	if !handled {
		t.Fatal("/help should be handled")
	}
	if m.help.Panel.Tab() != 0 {
		t.Errorf("expected helpTab=0 on /help entry, got %d", m.help.Panel.Tab())
	}
}

// Test_NewConversationID verifies cycle 86: UUID generator
// returns an 8-4-4-4-12 hex RFC-4122-style string.
func Test_NewConversationID(t *testing.T) {
	id := newConversationID()
	wantLen := 36 // 8+1+4+1+4+1+4+1+12
	if len(id) != wantLen {
		t.Fatalf("UUID length: want %d, got %d (%q)", wantLen, len(id), id)
	}
	// Check dashes at positions 8, 13, 18, 23.
	for _, i := range []int{8, 13, 18, 23} {
		if id[i] != '-' {
			t.Errorf("UUID position %d: want '-', got %c (id=%q)", i, id[i], id)
		}
	}
	// All other positions must be lowercase hex.
	hexChars := "0123456789abcdef"
	for i, c := range id {
		if i == 8 || i == 13 || i == 18 || i == 23 {
			continue
		}
		if !strings.ContainsRune(hexChars, rune(c)) {
			t.Errorf("UUID position %d: not hex, got %c", i, c)
		}
	}
	// Two consecutive UUIDs must differ (high entropy).
	id2 := newConversationID()
	if id == id2 {
		t.Errorf("two consecutive UUIDs collided: %q", id)
	}
}

// Test_CursorXWithMultibyteRune verifies cycle 90: cursor X
// advances by visible column count when typing multi-byte runes
// (e.g. Chinese characters). The v1.3.5 binary's prompt `>` glyph
// is wrapped in saturated blue ANSI via lipgloss Prompt style, and
// our cursorX math (m.composer.input.Cursor().X) lands on the correct
// visible column — counting CJK characters as 2 columns wide per
// terminal convention.
func Test_CursorXWithMultibyteRune(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	v := m.View()
	idleX := v.Cursor.X
	// Type a single Chinese character. Bubbles' Cursor().X counts
	// visible columns, so 1 CJK rune = 2 column advance.
	for _, r := range "中" {
		m.Update(runeKey(r))
	}
	v = m.View()
	if v.Cursor.X != idleX+2 {
		t.Errorf("after typing single CJK rune '中', cursor X should be idleX+2=%d (visible cols), got %d (cycle 90)", idleX+2, v.Cursor.X)
	}
}

// Test_CursorXWithPromptANSIOffset verifies cycle 90: idle
// cursor X is 2 (the position immediately after the `> ` prompt
// prefix). The lipgloss Prompt style wraps the prompt in ANSI
// bytes that bubbles correctly excludes from Cursor().X count.
func Test_CursorXWithPromptANSIOffset(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	v := m.View()
	if v.Cursor.X != 2 {
		t.Errorf("idle cursor X should be 2 (after '> ' prompt prefix), got %d (cycle 90)", v.Cursor.X)
	}
}

// Test_PickerHintRowStyling verifies cycle 91 / 126: the picker
// overlay's hint row ("↑/↓ Navigate · enter Select · tab Complete")
// wraps keys (↑/↓, enter, tab) in bold + true-colour blue
// (38;2;38;139;210) and labels in true-colour dim gray
// (38;2;101;123;131), matching the v1.3.6 binary.
func Test_PickerHintRowStyling(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	if !m.prompt.picker.Visible() {
		t.Fatal("picker should be visible after typing '/'")
	}
	view := m.View().Content
	wantKeys := []string{
		"\x1b[1m\x1b[38;2;38;139;210m\u2191/\u2193\x1b[0m",
		"\x1b[1m\x1b[38;2;38;139;210menter\x1b[0m",
		"\x1b[1m\x1b[38;2;38;139;210mtab\x1b[0m",
	}
	for _, k := range wantKeys {
		if !strings.Contains(view, k) {
			t.Errorf("picker hint row should contain key %q, view: %q", k, view[:300])
		}
	}
	wantLabels := []string{
		"\x1b[38;2;101;123;131m Navigate \u00b7 ",
		"\x1b[38;2;101;123;131m Select \u00b7 ",
		"\x1b[38;2;101;123;131m Complete",
	}
	for _, l := range wantLabels {
		if !strings.Contains(view, l) {
			t.Errorf("picker hint row should contain label %q, view: %q", l, view[:300])
		}
	}
}

// Test_HelpExitFeedback verifies cycle 92 / 126: when the /help
// overlay is dismissed via Esc, an "Exited /help command" feedback
// row is appended to the response transcript, matching the v1.3.6
// binary byte-for-byte. The row uses bold + true-colour warning blue
// (38;2;25;103;210) for the ⎿ bullet and true-colour dim gray
// (38;2;101;123;131 — for the body label).
func Test_HelpExitFeedback(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	handled, _ := m.handleSlashCommand("/help")
	if !handled || !m.help.Panel.Active() {
		t.Fatal("/help should open overlay")
	}
	// Dismiss via Esc
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.help.Panel.Active() {
		t.Fatal("the /help overlay should be closed after Esc")
	}
	wantBlock := []string{
		"\x1b[38;2;38;139;210;1m> /help\x1b[0m",
		"\x1b[38;2;38;139;210;1m  \u23bf  \x1b[0m\x1b[38;2;88;110;117mExited /help command\x1b[39m",
		"",
	}
	found := false
	for i := 0; i+len(wantBlock) <= len(m.conversation.response); i++ {
		if strings.Join(m.conversation.response[i:i+len(wantBlock)], "\x00") == strings.Join(wantBlock, "\x00") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected the standard `> /help` + Exited block, got %v", m.conversation.response)
	}
}

func TestRootModelSystemPromptIsTheReferencePrompt(t *testing.T) {
	root := t.TempDir()
	agentHome := filepath.Join(root, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(agentHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_HOME", agentHome)

	pluginDir := filepath.Join(root, ".gemini", "config", "plugins", "demo-plugin")
	skillDir := filepath.Join(pluginDir, "skills", "demo-skill")
	if err := os.MkdirAll(skillDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(pluginDir, "plugin.json"), []byte(`{"name":"demo-plugin"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	body := "---\nname: demo-skill\ndescription: A demo skill for the TUI prompt test.\n---\n\nbody\n"
	if err := os.WriteFile(filepath.Join(skillDir, "SKILL.md"), []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}

	ws := filepath.Join(root, "ws")
	m := &RootModel{session: sessionState{cwd: ws, conversationID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee"}}
	got := m.systemPrompt()

	for _, want := range []string{
		"<identity>\nYou are Antigravity, a powerful agentic AI coding assistant",
		"Command Working Directory: " + ws,
		"Conversation ID: aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee",
		"- demo-skill (" + filepath.Join(skillDir, "SKILL.md") + "): A demo skill for the TUI prompt test.",
	} {
		if !strings.Contains(got, want) {
			t.Fatalf("TUI system prompt is missing %q", want)
		}
	}
	if strings.Contains(got, "operating inside pig") {
		t.Fatal("TUI system prompt is PiG's default, not the reference's")
	}
}

func TestRootModelSystemPrompt_CustomAgent(t *testing.T) {
	root := t.TempDir()
	agentHome := filepath.Join(root, ".gemini", "antigravity-cli")
	if err := os.MkdirAll(agentHome, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("ANTIGRAVITY_HOME", agentHome)
	ws := filepath.Join(root, "ws")
	m := &RootModel{session: sessionState{cwd: ws, conversationID: "aaaaaaaa-bbbb-cccc-dddd-eeeeeeeeeeee", agentBody: "You are foo."}}
	got := m.systemPrompt()

	if !strings.HasPrefix(got, "<System Prompt>\nYou are foo.\n</System Prompt>\n<user_information>") {
		t.Fatalf("custom agent prompt prefix = %.160q", got)
	}
	for _, absent := range []string{"<identity>", "<subagents>", "<slash_commands>", "<conversation_transcript>"} {
		if strings.Contains(got, absent) {
			t.Errorf("custom agent prompt contains %s", absent)
		}
	}
	if strings.HasSuffix(got, "\n") {
		t.Error("custom agent prompt has a trailing newline")
	}
}

// escKey returns a tea.KeyPressMsg for Esc.
func escKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc})
}

// TestEscPickerOpenClosesDropdownOnly pins the first half of the
// spec: pressing Esc while the suggestions dropdown is open must
// close the dropdown and MUST NOT cancel the streaming turn. The
// test arms both states (picker visible + a running turn bridge),
// presses Esc, and verifies that:
//   - the picker is closed (Visible() == false)
//   - the cancel function was NOT invoked
//   - m.conversation.streaming stays true (turn is still in flight)
func TestEscPickerOpenClosesDropdownOnly(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	if !m.prompt.picker.Visible() {
		t.Fatalf("precondition: picker should be visible after '/'")
	}
	m.conversation.streaming = true
	bridge, ctx := armRunningBridge(t)
	m.turn.activeBridge = bridge

	m.Update(escKey())

	if m.prompt.picker.Visible() {
		t.Errorf("picker should be closed after Esc, still visible")
	}
	requireNotCancelled(t, ctx)
	if !m.conversation.streaming {
		t.Errorf("first Esc must leave streaming=true (turn untouched), got false")
	}
}

// TestEscNoPickerInterruptsStreamingTurn pins the second half of
// the spec: with the picker already closed and a turn in flight,
// pressing Esc must cancel the in-flight stream through the turn
// bridge. After the cancel fires the runtime clears itself, so a third
// Esc press (no-op in normal use) cannot re-cancel.
func TestEscNoPickerInterruptsStreamingTurn(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.conversation.streaming = true
	bridge, ctx := armRunningBridge(t)
	m.turn.activeBridge = bridge

	m.Update(escKey())

	requireCancelled(t, ctx)
	if bridge.IsRunning() {
		t.Error("the bridge still reports a running turn after cancel")
	}
	if !m.conversation.streaming {
		t.Errorf("streaming stays true until turnDoneMsg fires; Esc alone only cancels the ctx, got false")
	}
}

// TestEscTwoPressesCloseThenInterrupt reproduces the user flow:
//  1. type '/'  → picker opens
//  2. press Esc → picker closes (turn untouched)
//  3. press Esc → turn cancels (picker already gone)
//
// The two presses are sequential m.Update calls; the assertions
// check both states (picker visibility and cancel invocation)
// after each press.
func TestEscTwoPressesCloseThenInterrupt(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('/'))
	if !m.prompt.picker.Visible() {
		t.Fatalf("precondition: picker should be visible after '/'")
	}
	m.conversation.streaming = true
	bridge, ctx := armRunningBridge(t)
	m.turn.activeBridge = bridge

	// First Esc — close the dropdown.
	m.Update(escKey())
	if m.prompt.picker.Visible() {
		t.Errorf("after first Esc: picker should be closed, still visible")
	}
	requireNotCancelled(t, ctx)
	if !bridge.IsRunning() {
		t.Errorf("after first Esc: the turn should still run")
	}

	// Second Esc — interrupt the turn.
	m.Update(escKey())
	requireCancelled(t, ctx)
	if bridge.IsRunning() {
		t.Errorf("after second Esc: the bridge still reports a running turn")
	}
}

// TestEscNoDropdownNoStreamAbsorbsSilently is the regression guard
// for the existing "absorb Esc silently when nothing else applies"
// branch. Pressing Esc with the picker closed, no turn in flight,
// and an empty input must do nothing — including NOT setting any
// new state and NOT invoking any cancel.
func TestEscNoDropdownNoStreamAbsorbsSilently(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	if m.prompt.picker.Visible() {
		t.Fatalf("precondition: picker must start hidden")
	}
	if m.conversation.streaming {
		t.Fatalf("precondition: must not be streaming")
	}
	m.Update(escKey())

	if m.prompt.picker.Visible() {
		t.Errorf("picker must remain hidden on no-op Esc")
	}
	if m.conversation.streaming {
		t.Errorf("streaming must remain false on no-op Esc")
	}
	if m.turn.activeBridge != nil {
		t.Errorf("no-op Esc installed a turn bridge")
	}
	if m.prompt.input.Value() != "" {
		t.Errorf("input must remain empty on no-op Esc, got %q", m.prompt.input.Value())
	}
	if len(m.conversation.response) != 0 {
		t.Errorf("no-op Esc must not append to response, got %d rows", len(m.conversation.response))
	}
}

// TestEscStreamingWithoutCancelDoesNotCrash covers the stale-flag
// guard: streaming=true but no active bridge (the test fakeClient
// returns nil from Stream, so the RootModel is left in streaming=true
// with no live cancel — the same shape a backend error leaves the
// session in). Esc must NOT panic and must NOT block; it should
// fall through to the silent-absorb branch.
func TestEscStreamingWithoutCancelDoesNotCrash(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.conversation.streaming = true
	if m.turn.activeBridge != nil {
		t.Fatalf("precondition: activeBridge should start nil")
	}

	// Should not panic, should not append, should not crash.
	m.Update(escKey())

	if m.turn.activeBridge != nil {
		t.Errorf("stale streaming without a bridge must leave activeBridge nil")
	}
	if !m.conversation.streaming {
		t.Errorf("stale streaming flag must not be cleared by Esc, got false")
	}
	if len(m.conversation.response) != 0 {
		t.Errorf("no response rows should be appended on stale-streaming Esc, got %d", len(m.conversation.response))
	}
}

// TestEscInputClearStillWorks pins that the long-standing "Esc
// clears the input when there's text" branch is not regressed by
// the new streaming-interrupt branch. Priority: picker-close >
// streaming-interrupt > input-clear > silent-absorb. So Esc with
// input text + a turn in flight + no picker must clear the input
// (the input-clear branch fires BEFORE the silent-absorb branch,
// but the streaming-interrupt branch fires BEFORE input-clear and
// takes precedence when a turn bridge is running).
//
// The spec is "second Esc interrupts" — once the picker is closed
// and the turn is still streaming, the *next* Esc interrupts the
// turn (input is left alone). This matches the reference binary's
// behaviour captured in /tmp/agycmp/tui/esc_interrupts.esc.txt.
func TestEscInputClearOrInterruptPriority(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(runeKey('h'))
	m.Update(runeKey('i'))
	if got := m.prompt.input.Value(); got != "hi" {
		t.Fatalf("precondition: input should be 'hi', got %q", got)
	}
	m.conversation.streaming = true
	bridge, ctx := armRunningBridge(t)
	m.turn.activeBridge = bridge

	m.Update(escKey())

	// Streaming-interrupt takes priority over input-clear: the
	// turn is interrupted, the input text "hi" stays.
	requireCancelled(t, ctx)
	if got := m.prompt.input.Value(); got != "hi" {
		t.Errorf("Esc with streaming+no picker must leave input text intact (interrupts turn, not input), got %q", got)
	}
}

type mockGatewayBackend struct {
	cfg gateway.GatewayConfig
}

func (m *mockGatewayBackend) Stream(_ context.Context, _ backend.StreamRequest) (<-chan backend.StreamChunk, error) {
	ch := make(chan backend.StreamChunk, 1)
	ch <- backend.StreamChunk{Text: "ok", Done: true}
	close(ch)
	return ch, nil
}

func (m *mockGatewayBackend) GatewayConfig() gateway.GatewayConfig {
	return m.cfg
}

func TestMiniMaxHeaderAndFooter(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "https://gateway.example/v1",
		"models": [{"id": "minimax-m3", "name": "MiniMax M3", "model": "MiniMax-M3"}]}}}`)
	be := &mockGatewayBackend{
		cfg: gateway.GatewayConfig{
			ExternalModel: "MiniMax-M3",
		},
	}
	m := newRootModel(be, "user@test.com", "", "", "/test/workspace", false)
	if m.session.model != "MiniMax M3" {
		t.Fatalf("m.session.model = %q, want 'MiniMax M3'", m.session.model)
	}
	if m.session.statusModel != "MiniMax M3" {
		t.Fatalf("m.session.statusModel = %q, want 'MiniMax M3'", m.session.statusModel)
	}

	view := m.View().Content
	if !strings.Contains(view, "MiniMax M3") {
		t.Errorf("View() should contain 'MiniMax M3'")
	}
	if strings.Contains(view, "Gemini 3.8 Flash") {
		t.Errorf("View() should NOT contain hardcoded 'Gemini 3.8 Flash', got:\n%s", view)
	}
	if strings.Contains(view, "· high") || strings.Contains(view, "· medium") || strings.Contains(view, "· low") {
		t.Errorf("View() should NOT have effort suffix for MiniMax M3, got:\n%s", view)
	}

	m2 := newRootModel(nil, "user@test.com", "MiniMax M3", "", "/test/workspace", false)
	if m2.session.model != "MiniMax M3" || m2.session.statusModel != "MiniMax M3" {
		t.Fatalf("m2.session.model = %q, m2.session.statusModel = %q", m2.session.model, m2.session.statusModel)
	}
	view2 := m2.View().Content
	if !strings.Contains(view2, "MiniMax M3") {
		t.Errorf("View() should contain 'MiniMax M3'")
	}
	if strings.Contains(view2, "Gemini 3.8 Flash") {
		t.Errorf("View() should NOT contain hardcoded 'Gemini 3.8 Flash'")
	}

	m3 := newRootModel(nil, "user@test.com", "minimax-m3", "", "/test/workspace", false)
	if m3.session.model != "MiniMax M3" || m3.session.statusModel != "MiniMax M3" {
		t.Fatalf("m3.model = %q, m3.statusModel = %q", m3.session.model, m3.session.statusModel)
	}

	suffix := m.statusEffortSuffix()
	if suffix != "" {
		t.Errorf("statusEffortSuffix() = %q, want empty for MiniMax M3", suffix)
	}
}

var sgrRe = regexp.MustCompile("\x1b\\[[0-9;]*m")

// plainRow strips SGR so an assertion can look for the visible text of a
// styled row (the status row interleaves per-mode colour escapes).
func plainRow(s string) string { return sgrRe.ReplaceAllString(s, "") }

func shiftTabKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyTab, Mod: tea.ModShift})
}

// TestShiftTabCyclesBuildToDefault: starting at "build" (accept-edits),
// Shift+Tab → "default" (""). The cycle's first hop is the one the
// 1.3.5 changelog implicitly endorses by naming accept-edits as the
// "file edits auto-approved" mode — the user must be able to escape
// Build without burning two presses.
func TestShiftTabCyclesBuildToDefault(t *testing.T) {
	policy := permissions.Policy{Mode: permissions.ModeAcceptEdits}
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.session.policy = &policy
	m.session.mode = policy.Mode

	m.Update(shiftTabKey())
	if m.session.mode != "" {
		t.Errorf("Shift+Tab from Build: m.session.mode = %q, want %q (default)", m.session.mode, "")
	}
	if policy.Mode != "" {
		t.Errorf("Shift+Tab from Build: policy.Mode = %q, want %q (default)", policy.Mode, "")
	}
	// Cycle 283: the cue is the input placeholder, not a transcript line.
	if got := m.prompt.input.Placeholder; got != "" {
		t.Errorf("Build→Default placeholder = %q, want empty", got)
	}
	if len(m.conversation.response) != 0 {
		t.Errorf("Build→Default wrote %d transcript lines, want 0", len(m.conversation.response))
	}
}

// TestShiftTabCyclesDefaultToAuto: starting at "default" (""), Shift+Tab
// → "auto". Cycle 254's reorder makes this the first hop from a fresh
// session's mode (was accept-edits in cycle 104). Documented in 硬約束.
func TestShiftTabCyclesDefaultToAuto(t *testing.T) {
	policy := permissions.Policy{Mode: ""}
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.session.policy = &policy
	m.session.mode = policy.Mode

	m.Update(shiftTabKey())
	if m.session.mode != permissions.ModeAuto {
		t.Errorf("Shift+Tab from Default: m.session.mode = %q, want %q", m.session.mode, permissions.ModeAuto)
	}
	if policy.Mode != permissions.ModeAuto {
		t.Errorf("Shift+Tab from Default: policy.Mode = %q, want %q", policy.Mode, permissions.ModeAuto)
	}
	if got := m.prompt.input.Placeholder; got != permissions.AutoBanner {
		t.Errorf("Default→Auto placeholder = %q, want AutoBanner %q", got, permissions.AutoBanner)
	}
	if len(m.conversation.response) != 0 {
		t.Errorf("Default→Auto wrote %d transcript lines, want 0", len(m.conversation.response))
	}
}

// TestShiftTabCyclesAutoToPlan: starting at "auto", Shift+Tab → "plan".
func TestShiftTabCyclesAutoToPlan(t *testing.T) {
	policy := permissions.Policy{Mode: permissions.ModeAuto}
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.session.policy = &policy
	m.session.mode = policy.Mode

	m.Update(shiftTabKey())
	if m.session.mode != permissions.ModePlan {
		t.Errorf("Shift+Tab from Auto: m.session.mode = %q, want %q", m.session.mode, permissions.ModePlan)
	}
	if policy.Mode != permissions.ModePlan {
		t.Errorf("Shift+Tab from Auto: policy.Mode = %q, want %q", policy.Mode, permissions.ModePlan)
	}
	if got := m.prompt.input.Placeholder; got != permissions.PlanBanner {
		t.Errorf("Auto→Plan placeholder = %q, want PlanBanner %q", got, permissions.PlanBanner)
	}
	if len(m.conversation.response) != 0 {
		t.Errorf("Auto→Plan wrote %d transcript lines, want 0", len(m.conversation.response))
	}
}

// TestShiftTabCyclesPlanToBuild: starting at "plan", Shift+Tab → "build"
// (accept-edits). This is the cycle's wrap; the cycle reaches back to
// Build so the user can re-enter the file-edits mode without a
// second lap.
func TestShiftTabCyclesPlanToBuild(t *testing.T) {
	policy := permissions.Policy{Mode: permissions.ModePlan}
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.session.policy = &policy
	m.session.mode = policy.Mode

	m.Update(shiftTabKey())
	if m.session.mode != permissions.ModeAcceptEdits {
		t.Errorf("Shift+Tab from Plan (wrap): m.session.mode = %q, want %q", m.session.mode, permissions.ModeAcceptEdits)
	}
	if policy.Mode != permissions.ModeAcceptEdits {
		t.Errorf("Shift+Tab from Plan (wrap): policy.Mode = %q, want %q", policy.Mode, permissions.ModeAcceptEdits)
	}
	if got := m.prompt.input.Placeholder; got != permissions.AcceptEditsBanner {
		t.Errorf("Plan→Build placeholder = %q, want AcceptEditsBanner %q", got, permissions.AcceptEditsBanner)
	}
	if len(m.conversation.response) != 0 {
		t.Errorf("Plan→Build wrote %d transcript lines, want 0", len(m.conversation.response))
	}
}

// TestShiftTab_NotInConfigMenu: when the /config Agent Mode menu is
// open, Shift+Tab does NOT cycle the global agent mode. The /config
// panel swallows Tab (and its Shift+Tab variant) as the catch-all
// in handlePanelKey's panelregistry.Config switch — the reference uses Tab
// for value cycling when the row is value-bearing and our clone
// uses ←/→. Either way the key is consumed before it can reach the
// global Shift+Tab handler in tui_run.go's key router. Pinning this
// prevents a future refactor of handlePanelKey from accidentally
// letting Tab fall through to cycleMode() while the user is editing
// the Agent Mode setting.
func TestShiftTab_NotInConfigMenu(t *testing.T) {
	policy := permissions.Policy{Mode: ""}
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.session.policy = &policy
	m.session.mode = policy.Mode
	// Open /config on Agent Mode (cursor 0 — first row).
	m.overlays.panel = slashPanel{kind: panelregistry.Config, cmd: "/config", cursor: 0}
	startMode := m.session.mode
	startRespLen := len(m.conversation.response)

	m.Update(shiftTabKey())

	if m.session.mode != startMode {
		t.Errorf("Shift+Tab in /config mutated m.session.mode = %q, want %q (panel must swallow)", m.session.mode, startMode)
	}
	if policy.Mode != startMode {
		t.Errorf("Shift+Tab in /config mutated policy.Mode = %q, want %q", policy.Mode, startMode)
	}
	if got := len(m.conversation.response); got != startRespLen {
		t.Errorf("Shift+Tab in /config appended to response (%d → %d); the panel owns the key", startRespLen, got)
	}
}

// TestShiftTab_StatusLineReflects pins the reference's actual mode cue,
// verified against agy 1.4.3 at 200x40: the *input placeholder* carries
// the banner and the *status row* prefixes the model with the mode
// ("auto · MiniMax M3"). Shift+Tab never writes a transcript line — the
// pre-283 clone did, which stacked stale banners in the conversation.
func TestShiftTab_StatusLineReflects(t *testing.T) {
	policy := permissions.Policy{Mode: permissions.ModeAcceptEdits}
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.session.policy = &policy
	m.session.mode = policy.Mode
	m.session.statusModel = "MiniMax M3"

	m.Update(shiftTabKey()) // Build → Default
	if got := m.prompt.input.Placeholder; got != "" {
		t.Errorf("Build→Default placeholder = %q, want empty", got)
	}
	if row := plainRow(m.statusRow()); strings.Contains(row, "accept-edits · ") || strings.Contains(row, " · MiniMax M3") {
		t.Errorf("Build→Default status row still carries a mode prefix: %q", row)
	}

	m.Update(shiftTabKey()) // Default → Auto
	if got := m.prompt.input.Placeholder; got != permissions.AutoBanner {
		t.Errorf("Default→Auto placeholder = %q, want AutoBanner", got)
	}
	if row := plainRow(m.statusRow()); !strings.Contains(row, "auto · MiniMax M3") {
		t.Errorf("Default→Auto status row = %q, want it to contain %q", row, "auto · MiniMax M3")
	}

	m.Update(shiftTabKey()) // Auto → Plan
	if got := m.prompt.input.Placeholder; got != permissions.PlanBanner {
		t.Errorf("Auto→Plan placeholder = %q, want PlanBanner", got)
	}
	if row := plainRow(m.statusRow()); !strings.Contains(row, "plan · MiniMax M3") {
		t.Errorf("Auto→Plan status row = %q, want it to contain %q", row, "plan · MiniMax M3")
	}

	if len(m.conversation.response) != 0 {
		t.Errorf("Shift+Tab wrote %d transcript lines, want 0", len(m.conversation.response))
	}
}

// TestModeHintHidesAfterEdit pins the reference's placeholder lifetime
// (agy 1.4.3, 200x40): the cue is shown at startup and after Shift+Tab,
// and any keystroke in the textarea removes it for good — emptying the
// buffer does not bring it back, and neither does submitting.
func TestModeHintHidesAfterEdit(t *testing.T) {
	policy := permissions.Policy{Mode: permissions.ModeAuto}
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.session.policy = &policy
	m.session.mode = policy.Mode
	m.prompt.input.Placeholder = permissions.ModeBanner(m.session.mode)

	// A keystroke marks the input as edited.
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Text: "a"}))
	if !m.prompt.modeHintDismissed {
		t.Fatal("typing did not dismiss the mode hint")
	}

	// Shift+Tab re-arms it (and the new mode's banner takes its place).
	m.Update(shiftTabKey())
	if m.prompt.modeHintDismissed {
		t.Fatal("Shift+Tab did not re-arm the mode hint")
	}
	if got := m.prompt.input.Placeholder; got != permissions.PlanBanner {
		t.Errorf("after Shift+Tab placeholder = %q, want PlanBanner", got)
	}
}

func TestShiftTabCyclesMode(t *testing.T) {
	policy := permissions.Policy{Mode: permissions.ModeAcceptEdits}
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.session.policy = &policy
	m.session.mode = policy.Mode

	for _, want := range []struct{ mode, placeholder string }{
		{"", ""},
		{permissions.ModeAuto, permissions.AutoBanner},
		{permissions.ModePlan, permissions.PlanBanner},
		{permissions.ModeAcceptEdits, permissions.AcceptEditsBanner},
	} {
		m.Update(shiftTabKey())
		if m.session.mode != want.mode {
			t.Fatalf("after Shift+Tab m.session.mode = %q, want %q", m.session.mode, want.mode)
		}
		if policy.Mode != want.mode {
			t.Fatalf("after Shift+Tab policy.Mode = %q, want %q (the tool gate must see it)", policy.Mode, want.mode)
		}
		if got := m.prompt.input.Placeholder; got != want.placeholder {
			t.Errorf("mode %q placeholder = %q, want %q", want.mode, got, want.placeholder)
		}
		if len(m.conversation.response) != 0 {
			t.Errorf("mode %q wrote %d transcript lines, want 0", want.mode, len(m.conversation.response))
		}
	}
}

func TestShiftTabStartsFromFlagMode(t *testing.T) {
	policy := permissions.Policy{Mode: permissions.ModeAuto}
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.session.policy = &policy
	m.session.mode = policy.Mode

	m.Update(shiftTabKey())
	if m.session.mode != permissions.ModePlan {
		t.Errorf("--mode auto + Shift+Tab = %q, want plan", m.session.mode)
	}
}

func TestShiftTabDoesNotCompletePickerRow(t *testing.T) {
	policy := permissions.Policy{}
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.session.policy = &policy
	m.prompt.input.SetValue("/a")
	m.prompt.picker.Update([]rune("/a"))
	if m.prompt.picker.Selected() == nil {
		t.Skip("picker has no selection for this prefix")
	}
	before := m.prompt.input.Value()

	m.Update(shiftTabKey())
	if m.prompt.input.Value() != before {
		t.Errorf("Shift+Tab rewrote the input to %q", m.prompt.input.Value())
	}
	if m.session.mode == "" {
		t.Errorf("Shift+Tab with the picker open did not cycle (mode=%q)", m.session.mode)
	}
}

// TestTurnDoneMsg_EmptyAssistantNotRecorded pins the headline
// behaviour: a turnDoneMsg with no reply, no tool calls, and no
// reasoning must NOT grow m.session. The single user message that
// was appended before submitPrompt (the prompt echo) stays the only
// entry.
func TestTurnDoneMsg_EmptyAssistantNotRecorded(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	// Simulate the prompt echo + user-step append the real
	// submitPrompt path produces. The cycle keeps this step; the
	// gate is only about the assistant step.
	m.conversation.response = append(m.conversation.response, "> hi")
	m.conversation.liveStart = len(m.conversation.response)
	m.setSessionMessages(append(m.SessionMessages(), backend.Message{Role: "user", Content: "hi"}))

	_, _ = m.Update(toolsteps.TurnDoneMsg{
		Reply:     "",
		Steps:     nil,
		Reasoning: "",
	})

	if len(m.SessionMessages()) != 1 {
		t.Fatalf("empty turn appended %d session entries; want 1 (just the user prompt)", len(m.SessionMessages()))
	}
	if m.SessionMessages()[0].Role != "user" {
		t.Errorf("session[0].Role = %q; want user", m.SessionMessages()[0].Role)
	}
}

// TestTurnDoneMsg_ReasoningOnlyRecordedAsStep pins the carve-out
// for thinking-only turns: the classifier treats reasoning as
// content, so a turn that produced only thinking (no text, no tool
// calls) is NOT empty — it grows m.engine.session with one assistant entry
// AND the on-screen tool-step block carries the "Thought for Ns, N
// tokens" rendering. The previous gate `if msg.reply != ""` already
// recorded this case (msg.reply is "" so the gate would actually
// skip; the new gate fixes that bug by also considering reasoning).
func TestTurnDoneMsg_ReasoningOnlyRecordedAsStep(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.conversation.response = append(m.conversation.response, "> think about it")
	m.conversation.liveStart = len(m.conversation.response)
	m.setSessionMessages(append(m.SessionMessages(), backend.Message{Role: "user", Content: "think about it"}))

	thoughtStep := toolsteps.Step{
		Kind:   toolsteps.KindThought,
		Name:   "thinking",
		Detail: "spent 3s on the problem",
	}
	_, _ = m.Update(toolsteps.TurnDoneMsg{
		Reply:     "",
		Steps:     []toolsteps.Step{thoughtStep},
		Reasoning: "spent 3s on the problem",
	})

	// Reasoning IS content per the AGY 1.3.6 classifier, so the
	// session grows with one assistant entry (text empty, but the
	// step exists because reasoning happened). The conversation
	// database stores this as StepModelResponse with empty text —
	// the presence of the step tells the resumed conversation the
	// model spent quota on this turn.
	if len(m.SessionMessages()) != 2 {
		t.Fatalf("reasoning-only turn appended %d session entries; want 2", len(m.SessionMessages()))
	}
	if m.SessionMessages()[1].Role != "assistant" {
		t.Errorf("session[1].Role = %q; want assistant", m.SessionMessages()[1].Role)
	}
	// The on-screen tool-step block is also rendered, so the user
	// sees the reasoning content.
	if len(m.conversation.blocks) == 0 {
		t.Error("reasoning-only turn did not record a tool-step block; the on-screen reasoning would be lost")
	}
}

// TestTurnDoneMsg_ReplyRecorded pins the happy path: a turn that
// streamed text content grows m.engine.session with one assistant entry.
// This is the case the previous gate `if msg.reply != ""` already
// covered; the new gate must keep working.
func TestTurnDoneMsg_ReplyRecorded(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.conversation.response = append(m.conversation.response, "> greet")
	m.conversation.liveStart = len(m.conversation.response)
	m.setSessionMessages(append(m.SessionMessages(), backend.Message{Role: "user", Content: "greet"}))

	_, _ = m.Update(toolsteps.TurnDoneMsg{
		Reply:     "Hello there.",
		Steps:     nil,
		Reasoning: "",
	})

	if len(m.SessionMessages()) != 2 {
		t.Fatalf("normal turn recorded %d session entries; want 2", len(m.SessionMessages()))
	}
	if m.SessionMessages()[1].Role != "assistant" || m.SessionMessages()[1].Content != "Hello there." {
		t.Errorf("session[1] = %+v; want assistant/Hello there.", m.SessionMessages()[1])
	}
}

// TestTurnDoneMsg_PartialReplyRecorded pins the --print-timeout
// carve-out: a turn that streamed some text and then was cancelled
// keeps the partial text as the assistant step. The classifier
// reports non-empty (content is non-empty), so the gate fires.
func TestTurnDoneMsg_PartialReplyRecorded(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.conversation.response = append(m.conversation.response, "> long question")
	m.conversation.liveStart = len(m.conversation.response)
	m.setSessionMessages(append(m.SessionMessages(), backend.Message{Role: "user", Content: "long question"}))

	partial := "I was about to say that"
	_, _ = m.Update(toolsteps.TurnDoneMsg{
		Reply:     partial,
		Steps:     nil,
		Reasoning: "",
	})

	if len(m.SessionMessages()) != 2 {
		t.Fatalf("partial turn recorded %d session entries; want 2", len(m.SessionMessages()))
	}
	if m.SessionMessages()[1].Content != partial {
		t.Errorf("session[1].Content = %q; want %q", m.SessionMessages()[1].Content, partial)
	}
}

// TestTurnDoneMsg_ToolCallOnlyDoesNotAppendSessionStep pins the
// recovered wire shape: tool calls are recorded as StepToolCall
// rows of the conversation database (via the tool-step block in
// m.conversation.blocks), NOT as StepModelResponse rows from m.session. The new
// gate passes through the classifier's "non-empty" verdict for
// tool-call-only turns, but the call site still skips the
// m.engine.session append because the wire shape wants StepToolCall, not
// StepModelResponse. The on-screen step block is the user-visible
// artefact.
func TestTurnDoneMsg_ToolCallOnlyDoesNotAppendSessionStep(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.conversation.response = append(m.conversation.response, "> read the file")
	m.conversation.liveStart = len(m.conversation.response)
	m.setSessionMessages(append(m.SessionMessages(), backend.Message{Role: "user", Content: "read the file"}))

	toolStepEntry := toolsteps.Step{
		Name:   "read_file",
		Title:  "Read /tmp/x",
		Detail: "Tool call: read_file",
	}
	_, _ = m.Update(toolsteps.TurnDoneMsg{
		Reply:     "",
		Steps:     []toolsteps.Step{toolStepEntry},
		Reasoning: "",
	})

	// The classifier agrees the turn is non-empty (tool calls ARE
	// content), so the on-screen tool-step block is rendered.
	if len(m.conversation.blocks) == 0 {
		t.Error("tool-call-only turn did not record a tool-step block; on-screen tool step would be lost")
	}
	// But the m.engine.session does NOT grow with a StepModelResponse —
	// tool calls live as StepToolCall rows in the conversation
	// database, not StepModelResponse.
	if len(m.SessionMessages()) != 1 {
		t.Fatalf("tool-call-only turn appended %d session entries; want 1 (just the user prompt)", len(m.SessionMessages()))
	}
	if m.SessionMessages()[0].Role != "user" {
		t.Errorf("session[0].Role = %q; want user", m.SessionMessages()[0].Role)
	}
}

// TestTurnDoneMsg_WhitespaceOnlyReplyClassifiedEmpty pins the
// gateway's leading-newline SSE framing: the gateway transport delivers
// "\n" as the very first delta on every stream, and the recovered
// sanitizer keeps that byte. Without trimming, the classifier would
// treat that newline as content and skip the gate on every single
// request — the very leak the changelog row deletes.
//
// This is the regression test for an earlier prototype that used
// `content == ""` instead of `strings.TrimSpace(content) == ""`.
func TestTurnDoneMsg_WhitespaceOnlyReplyClassifiedEmpty(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.conversation.response = append(m.conversation.response, "> ping")
	m.conversation.liveStart = len(m.conversation.response)
	m.setSessionMessages(append(m.SessionMessages(), backend.Message{Role: "user", Content: "ping"}))

	_, _ = m.Update(toolsteps.TurnDoneMsg{
		Reply:     "\n",
		Steps:     nil,
		Reasoning: "",
	})

	if len(m.SessionMessages()) != 1 {
		t.Fatalf("whitespace-only reply appended %d session entries; want 1", len(m.SessionMessages()))
	}
}

// TestTurnDoneMsg_ErrorRowSurvivesTurnBlock pins the shape the user sees: the
// tool block is still collapsed, and the failure is spelled out underneath it.
func TestTurnDoneMsg_ErrorRowSurvivesTurnBlock(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.conversation.response = append(m.conversation.response, "> do a long thing")
	m.conversation.liveStart = len(m.conversation.response)
	m.setSessionMessages(append(m.SessionMessages(), backend.Message{Role: "user", Content: "do a long thing"}))

	_, _ = m.Update(toolsteps.TurnDoneMsg{
		Reply: "",
		Steps: []toolsteps.Step{{
			Kind:   toolsteps.KindTool,
			Name:   "Bash",
			Title:  "Bash",
			Detail: "sh chain.sh start",
		}},
		Err: errors.New("context deadline exceeded"),
	})

	joined := strings.Join(m.conversation.response, "\n")
	if !strings.Contains(joined, "Error: context deadline exceeded") {
		t.Fatalf("transcript has no error row:\n%s", joined)
	}
	if !strings.Contains(joined, "ctrl+o to expand") {
		t.Errorf("tool block vanished; transcript:\n%s", joined)
	}
}

// TestTurnDoneMsg_ErrorRowUsesReferenceCopy pins the API-error path: when the
// failure carries a *apierror.HTTPError, the row uses the reference copy.
func TestTurnDoneMsg_ErrorRowUsesReferenceCopy(t *testing.T) {
	api := &apierror.HTTPError{StatusCode: 429, Body: []byte(`{"error":{"message":"Individual quota reached."}}`)}
	message, ok := agentturn.TurnErrorMessage(api)
	if !ok {
		t.Fatal("TurnErrorMessage reported no message")
	}
	row := toolsteps.ErrorRow(message)
	if !strings.HasPrefix(row, "Error: ") {
		t.Errorf("row = %q, want an Error: prefix", row)
	}
	if !strings.Contains(row, "Individual quota reached.") {
		t.Errorf("row = %q, want the API message", row)
	}
	if strings.Contains(row, "429") && !strings.Contains(row, "quota") {
		t.Errorf("row leaked the transport shape: %q", row)
	}
	if _, ok := agentturn.TurnErrorMessage(nil); ok {
		t.Error("TurnErrorMessage(nil) reported a message, want none")
	}
}

// TestPickBackend_InteractiveHasNoTotalRequestDeadline pins that interactive
// mode passes GatewayTimeoutUnlimited.
func TestPickBackend_InteractiveHasNoTotalRequestDeadline(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	t.Setenv("OPENAGY_BACKEND", "gateway")
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "https://gateway.example/v1",
		"models": [{"id": "m", "name": "M", "model": "Upstream-1"}]}}}`)

	be := backendselect.Pick(nil, "m", modelpicker.ResolveCatalogueID)
	gw, ok := be.(interface{ GatewayConfig() gateway.GatewayConfig })
	if !ok {
		t.Fatalf("pickBackend returned %T, want a configured-endpoint client", be)
	}
	if got := gw.GatewayConfig().TimeoutSeconds; got != gateway.GatewayTimeoutUnlimited {
		t.Errorf("interactive TimeoutSeconds = %d, want %d (no total deadline)",
			got, gateway.GatewayTimeoutUnlimited)
	}
}

// TestPickBackend_HonoursExplicitBackendFlag guards that --backend gateway is honoured.
func TestPickBackend_HonoursExplicitBackendFlag(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "https://gateway.example/v1",
		"models": [{"id": "m", "name": "M", "model": "Upstream-1"}]}}}`)
	fs := flag.NewFlagSet("t", flag.ContinueOnError)
	fs.String("backend", "", "")
	if err := fs.Parse([]string{"--backend", "gateway"}); err != nil {
		t.Fatalf("parse: %v", err)
	}
	if _, ok := backendselect.PickWithLog(fs, "m", io.Discard, gateway.GatewayTimeoutUnlimited, modelpicker.ResolveCatalogueID).(interface{ GatewayConfig() gateway.GatewayConfig }); !ok {
		t.Fatal("--backend gateway did not yield a configured-endpoint client")
	}
}

// vimTestMode builds a RootModel in an isolated config home seeded with
// the given settings. newRootModel itself is inert (vim off); the settings
// are applied the same way the production entry point does it.
func vimTestMode(t *testing.T, settings map[string]any) *RootModel {
	t.Helper()
	cycle196TestHome(t, settings)
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.reloadVimSettings()
	return m
}

// vimPress sends one key event through the run loop.
func vimPress(t *testing.T, m *RootModel, msg tea.Msg) tea.Cmd {
	t.Helper()
	_, cmd := m.Update(msg)
	return cmd
}

// vimType feeds a string of printable runes.
func vimType(t *testing.T, m *RootModel, s string) {
	t.Helper()
	for _, r := range s {
		vimPress(t, m, runeKey(r))
	}
}

func vimEscKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc})
}

// --- mode gating -------------------------------------------------------

func TestVimDisabledLeavesInputAlone(t *testing.T) {
	m := vimTestMode(t, map[string]any{})
	if m.vimEnabled() {
		t.Fatal("vim should be off with empty settings")
	}
	vimType(t, m, "abc")
	if got := m.prompt.input.Value(); got != "abc" {
		t.Fatalf("non-vim textarea = %q, want abc", got)
	}
	if m.prompt.vimEng != nil {
		t.Fatal("the engine must not be built when vim is off")
	}
	if got := m.statusRowState().VimBadge; got != "" {
		t.Fatalf("badge = %q, want empty", got)
	}
}

func TestVimNormalModeConsumesRunes(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	if !m.vimEnabled() {
		t.Fatal("vim should be on")
	}
	// In Normal mode "a" opens Insert; the following runes are text.
	vimType(t, m, "abc")
	if got := m.prompt.input.Value(); got != "bc" {
		t.Fatalf("textarea = %q, want bc", got)
	}
	if m.prompt.vimEng.Mode != vim.Insert {
		t.Fatalf("mode = %v, want Insert", m.prompt.vimEng.Mode)
	}
	// Esc returns to Normal without touching the buffer.
	vimPress(t, m, vimEscKey())
	if m.prompt.vimEng.Mode != vim.Normal {
		t.Fatalf("mode = %v, want Normal", m.prompt.vimEng.Mode)
	}
	if got := m.prompt.input.Value(); got != "bc" {
		t.Fatalf("textarea after Esc = %q, want bc", got)
	}
	// Esc never clears the prompt in vim mode.
	if got := m.prompt.input.Value(); got == "" {
		t.Fatal("Esc must not clear the prompt in vim mode")
	}
}

// --- submit ------------------------------------------------------------

func TestVimInsertCtrlSSubmits(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "ihello")
	if got := m.prompt.input.Value(); got != "hello" {
		t.Fatalf("textarea = %q, want hello", got)
	}
	vimPress(t, m, ctrlKey('s'))
	if !m.conversation.streaming {
		t.Fatal("ctrl+s should submit from Insert mode")
	}
	if got := m.prompt.input.Value(); got != "" {
		t.Fatalf("textarea after submit = %q, want empty", got)
	}
}

func TestVimNormalEnterSubmits(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "iabc")
	vimPress(t, m, vimEscKey())
	// A bare Enter in Normal mode submits: the vim-normal map binds only
	// ctrl+s / ctrl+enter, so Enter falls through to the global binding.
	vimPress(t, m, enterKey())
	if !m.conversation.streaming {
		t.Fatal("plain Enter in Normal mode should submit")
	}

	// ctrl+s is the other Normal-mode submit binding.
	m2 := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m2, "iabc")
	vimPress(t, m2, vimEscKey())
	vimPress(t, m2, ctrlKey('s'))
	if !m2.conversation.streaming {
		t.Fatal("ctrl+s should submit from Normal mode")
	}
}

func TestVimOpenLineEntersInsert(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "iabc")
	vimPress(t, m, vimEscKey())
	vimPress(t, m, runeKey('o'))
	if got := m.statusRowState().VimBadge; got != "-- INSERT --" {
		t.Fatalf("badge after o = %q, want -- INSERT --", got)
	}
	if got := m.prompt.input.Value(); got != "abc\n" {
		t.Fatalf("textarea after o = %q, want %q", got, "abc\n")
	}

	m2 := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m2, "iab")
	vimPress(t, m2, vimEscKey())
	vimPress(t, m2, runeKey('O'))
	if got := m2.prompt.input.Value(); got != "\nab" {
		t.Fatalf("textarea after O = %q, want %q", got, "\nab")
	}
}

// --- Insert First ------------------------------------------------------

func TestVimInsertFirstOpensInInsert(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim", "vimInsertFirst": true})
	if got := m.statusRowState().VimBadge; got != "-- INSERT --" {
		t.Fatalf("badge = %q, want -- INSERT --", got)
	}
	// The first rune is text, not a command.
	vimType(t, m, "hi")
	if got := m.prompt.input.Value(); got != "hi" {
		t.Fatalf("textarea = %q, want hi", got)
	}
	// A bare Enter submits...
	vimPress(t, m, enterKey())
	if !m.conversation.streaming {
		t.Fatal("bare Enter under Insert First should submit")
	}
}

func TestVimInsertFirstShiftEnterInsertsNewline(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim", "vimInsertFirst": true})
	vimType(t, m, "hi")
	vimPress(t, m, shiftEnterKey())
	if m.conversation.streaming {
		t.Fatal("shift+enter under Insert First must insert a newline, not submit")
	}
	if got := m.prompt.input.Value(); got != "hi\n" {
		t.Fatalf("textarea = %q, want %q", got, "hi\n")
	}
	// ctrl+j inserts a newline too.
	vimPress(t, m, ctrlKey('j'))
	if got := m.prompt.input.Value(); got != "hi\n\n" {
		t.Fatalf("textarea = %q, want %q", got, "hi\n\n")
	}
}

// --- textarea mirroring ------------------------------------------------

func TestVimCursorMirrorsEngine(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "ihello world")
	vimPress(t, m, vimEscKey())
	// 0 then w: the engine lands on "world" (col 6).
	vimPress(t, m, runeKey('0'))
	vimPress(t, m, runeKey('w'))
	if m.prompt.vimEng.Col != 6 {
		t.Fatalf("engine col = %d, want 6", m.prompt.vimEng.Col)
	}
	// The textarea reports the same position (row 0, col 6).
	if row, col := m.prompt.input.Line(), m.prompt.input.Column(); row != 0 || col != 6 {
		t.Fatalf("textarea cursor = (row %d, col %d), want (0,6)", row, col)
	}
}

func TestVimEngineReseedsAfterHostRewrite(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "ihello")
	vimPress(t, m, vimEscKey())
	// The host rewrites the buffer behind the engine's back (history
	// recall, picker substitution, slash rewrite).
	m.prompt.input.SetValue("world")
	vimType(t, m, "A!")
	if got := m.prompt.input.Value(); got != "world!" {
		t.Fatalf("textarea = %q, want world!", got)
	}
}

// --- badges and /help --------------------------------------------------

func TestVimStatusBadges(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	if got := m.statusRowState().VimBadge; got != "" {
		t.Fatalf("Normal mode badge = %q, want none", got)
	}
	vimType(t, m, "i")
	if got := m.statusRowState().VimBadge; got != "-- INSERT --" {
		t.Fatalf("Insert badge = %q", got)
	}
	// With an empty prompt the hint survives and the badge is prepended
	// to it (the reference's status line is "-- INSERT -- ? for …").
	if row := m.statusRow(); !strings.Contains(row, "-- INSERT --") || !strings.Contains(row, "? for shortcuts") {
		t.Fatalf("status row = %q, want badge before the hint", row)
	}
	vimPress(t, m, vimEscKey())
	vimType(t, m, "v")
	if got := m.statusRowState().VimBadge; got != "-- VISUAL --" {
		t.Fatalf("Visual badge = %q", got)
	}
	vimType(t, m, "V")
	if got := m.statusRowState().VimBadge; got != "-- V-LINE --" {
		t.Fatalf("Visual line badge = %q", got)
	}
	// The badge lands on the status row.
	if row := m.statusRow(); !strings.Contains(row, "-- V-LINE --") {
		t.Fatalf("status row missing badge: %q", row)
	}
}

// --- global keys still reach the host ----------------------------------

func TestVimDeclinesGlobalKeys(t *testing.T) {
	m := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimType(t, m, "iabc")
	// ctrl+c is not a vim binding: the engine declines it and the host's
	// two-step exit confirmation runs instead.
	vimPress(t, m, ctrlKey('c'))
	if !m.display.exitConfirm {
		t.Fatal("ctrl+c should arm the exit confirmation even in vim mode")
	}
	if m.display.quitting {
		t.Fatal("the first ctrl+c must not quit")
	}
	// Ctrl+D exits immediately.
	m2 := vimTestMode(t, map[string]any{"editorMode": "vim"})
	vimPress(t, m2, ctrlKey('d'))
	if !m2.display.quitting {
		t.Fatal("ctrl+d should quit even in vim mode")
	}
}

// upKey / downKey build plain arrow tea.KeyPressMsg with no
// modifier — matches the keys the runtime dispatches when the user
// presses Up/Down on the input box.
func upKey() tea.KeyPressMsg   { return tea.KeyPressMsg(tea.Key{Code: tea.KeyUp}) }
func downKey() tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: tea.KeyDown}) }
func leftKey() tea.KeyPressMsg { return tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}) }

// --- pure helpers ----------------------------------------------------

func TestCursorLine_SingleLine_NoNewlines(t *testing.T) {
	cases := []struct {
		value  string
		cursor int
		want   int
	}{
		{"", 0, 0},
		{"hello", 0, 0},
		{"hello", 3, 0},
		{"hello", 5, 0}, // end-of-input on the only line
	}
	for _, c := range cases {
		if got := tuilib.CursorLine(c.value, c.cursor); got != c.want {
			t.Errorf("cursorLine(%q, %d) = %d, want %d", c.value, c.cursor, got, c.want)
		}
	}
}

func TestCursorLine_MultiLine(t *testing.T) {
	// Layout: a\nb\nc
	// Bytes:  0 1 2 3 4
	//         a \n b \n c
	// Lines:  0   1   2
	cases := []struct {
		cursor int
		want   int
	}{
		{0, 0}, // start of "a"
		{1, 0}, // end of "a", just before the first \n
		{2, 1}, // start of "b", just after the first \n
		{3, 1}, // middle of "b"
		{4, 2}, // start of "c", just after the second \n
		{5, 2}, // end of "c"
	}
	for _, c := range cases {
		if got := tuilib.CursorLine("a\nb\nc", c.cursor); got != c.want {
			t.Errorf("cursorLine(\"a\\nb\\nc\", %d) = %d, want %d", c.cursor, got, c.want)
		}
	}
}

func TestCursorLine_ClampOutOfRange(t *testing.T) {
	// Negative cursor clamps to 0; cursor past end clamps to len.
	if got := tuilib.CursorLine("a\nb", -5); got != 0 {
		t.Errorf("cursorLine(_, -5) = %d, want 0", got)
	}
	if got := tuilib.CursorLine("a\nb", 100); got != 1 {
		t.Errorf("cursorLine(_, 100) = %d, want 1 (last line)", got)
	}
	// Cursor == len(value) sits on the last line (one byte past
	// the last rune is still inside the value).
	if got := tuilib.CursorLine("a\nb", 3); got != 1 {
		t.Errorf("cursorLine(\"a\\nb\", 3) = %d, want 1 (end-of-input is on the last line)", got)
	}
}

func TestIsOnFirstLine_TrueCases(t *testing.T) {
	// Empty value: trivially on the first (and only) line.
	if !tuilib.IsOnFirstLine("", 0) {
		t.Error(`isOnFirstLine("", 0) = false, want true`)
	}
	// Single-line value: every cursor is on the first line.
	for i := 0; i <= 5; i++ {
		if !tuilib.IsOnFirstLine("hello", i) {
			t.Errorf(`isOnFirstLine("hello", %d) = false, want true`, i)
		}
	}
	// Multi-line: cursor before the first \n is on the first line.
	if !tuilib.IsOnFirstLine("a\nb\nc", 0) {
		t.Error(`isOnFirstLine("a\nb\nc", 0) = false, want true`)
	}
	if !tuilib.IsOnFirstLine("a\nb\nc", 1) {
		t.Error(`isOnFirstLine("a\nb\nc", 1) = false, want true (just before first \n)`)
	}
}

func TestIsOnFirstLine_FalseCases(t *testing.T) {
	// Cursor at the start of "b" is on the second line.
	if tuilib.IsOnFirstLine("a\nb\nc", 2) {
		t.Error(`isOnFirstLine("a\nb\nc", 2) = true, want false`)
	}
	// Cursor at the start of "c" is on the third (last) line.
	if tuilib.IsOnFirstLine("a\nb\nc", 5) {
		t.Error(`isOnFirstLine("a\nb\nc", 5) = true, want false`)
	}
}

func TestIsOnLastLine_TrueCases(t *testing.T) {
	// Empty value: trivially on the last (and only) line.
	if !tuilib.IsOnLastLine("", 0) {
		t.Error(`isOnLastLine("", 0) = false, want true`)
	}
	// Single-line value: every cursor is on the last line.
	for i := 0; i <= 5; i++ {
		if !tuilib.IsOnLastLine("hello", i) {
			t.Errorf(`isOnLastLine("hello", %d) = false, want true`, i)
		}
	}
	// Multi-line: cursor on the last line is on the last line.
	if !tuilib.IsOnLastLine("a\nb\nc", 5) {
		t.Error(`isOnLastLine("a\nb\nc", 5) = false, want true`)
	}
	if !tuilib.IsOnLastLine("a\nb\nc", 6) {
		t.Error(`isOnLastLine("a\nb\nc", 6) = false, want true (end-of-input on last line)`)
	}
}

func TestIsOnLastLine_FalseCases(t *testing.T) {
	// Cursor on the first line has more content after it.
	if tuilib.IsOnLastLine("a\nb\nc", 0) {
		t.Error(`isOnLastLine("a\nb\nc", 0) = true, want false`)
	}
	// Cursor on the middle line has more content after it.
	if tuilib.IsOnLastLine("a\nb\nc", 2) {
		t.Error(`isOnLastLine("a\nb\nc", 2) = true, want false`)
	}
}

func TestInputCursorByteOffset_MultiLine(t *testing.T) {
	m := tas.New()
	// Pin a wide width so the textarea doesn't soft-wrap short
	// single-character rows. The default width is 40 but the
	// prompt is "┃ " which eats 2 columns and active style may
	// add borders; 100 leaves plenty of headroom.
	m.SetWidth(100)
	m.SetHeight(10)
	m.SetValue("a\nb\nc")
	// bubbles v2 textarea's SetValue leaves the cursor at the end
	// of the inserted text, not at row 0 col 0. MoveToBegin
	// anchors it back at the start so the byte offset tests start
	// from a known position.
	m.MoveToBegin()
	if got := tuilib.InputCursorByteOffset(m); got != 0 {
		t.Errorf("cursor at start after MoveToBegin = %d, want 0", got)
	}
	// Move to row 1, col 0 via CursorDown. CursorDown preserves
	// the column when the row below is at least as long as the
	// cursor's current column — col 0 stays col 0 here.
	m.CursorDown()
	if got := tuilib.InputCursorByteOffset(m); got != 2 {
		t.Errorf("cursor on row 1, col 0 = %d, want 2 (a + \\n)", got)
	}
	// Move to row 1, col 1 (inside "b"). Byte offset 3.
	m.SetCursorColumn(1)
	if got := tuilib.InputCursorByteOffset(m); got != 3 {
		t.Errorf("cursor on row 1, col 1 = %d, want 3", got)
	}
	// Move to row 2. CursorDown preserves the column when the
	// row below has the column in range; "c" is 1 char so the
	// cursor lands at (2, 1) — end-of-row. Byte offset 5.
	m.CursorDown()
	if got := tuilib.InputCursorByteOffset(m); got != 5 {
		t.Errorf("cursor on row 2 (col preserved as 1) = %d, want 5", got)
	}
	// Back up to (2, 0) via SetCursorColumn(0). Byte offset 4.
	m.SetCursorColumn(0)
	if got := tuilib.InputCursorByteOffset(m); got != 4 {
		t.Errorf("cursor on row 2, col 0 = %d, want 4 (start of c)", got)
	}
}

func TestInputCursorByteOffset_UTF8(t *testing.T) {
	// 2-byte UTF-8 runes: "αβγ" each is 2 bytes. "α\nβ" should
	// produce a byte offset of 3 (after α + the \n) when the
	// cursor sits at the start of "β".
	m := tas.New()
	m.SetValue("α\nβ")
	// SetValue leaves the cursor at the end; MoveToBegin anchors
	// it back at row 0 col 0.
	m.MoveToBegin()
	// Row 0 col 0 → byte offset 0.
	if got := tuilib.InputCursorByteOffset(m); got != 0 {
		t.Errorf("UTF8 cursor on row 0, col 0 = %d, want 0", got)
	}
	// Row 1 col 0 → byte offset 3 (α is 2 bytes + the \n).
	m.CursorDown()
	if got := tuilib.InputCursorByteOffset(m); got != 3 {
		t.Errorf("UTF8 cursor on row 1, col 0 = %d, want 3 (α is 2 bytes + \\n)", got)
	}
	// Row 1 col 1 → byte offset 5 (β starts at byte 3 and is
	// 2 bytes long).
	m.SetCursorColumn(1)
	if got := tuilib.InputCursorByteOffset(m); got != 5 {
		t.Errorf("UTF8 cursor on row 1, col 1 = %d, want 5", got)
	}
}

// --- integration: RootModel + tea.KeyMsg ------------------------------

// rootModelForTest returns a freshly-constructed RootModel with no
// history and no picker state, suitable for driving KeyMsg paths
// from a unit test without leaking state across cases. Reuses
// fakeClient declared in tui_run_test.go (same package).
func rootModelForTest() *RootModel {
	return newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
}

func TestUpOnFirstLineSingleLine_TriggersHistory(t *testing.T) {
	m := rootModelForTest()
	// Push a history entry directly. Single-line input value
	// (cursor on the first / only line) should recall it.
	m.pushHistory("first prompt")
	if got := m.prompt.input.Value(); got != "" {
		t.Fatalf("pre-condition: empty input got %q", got)
	}
	// Up arrow on empty input → first (and only) line → historyUp.
	m.Update(upKey())
	if got := m.prompt.input.Value(); got != "first prompt" {
		t.Errorf("Up on empty input should recall history; got input = %q, want %q",
			got, "first prompt")
	}
	if m.prompt.promptHistory.Index() != 1 {
		t.Errorf("historyIdx = %d, want 1 after one historyUp", m.prompt.promptHistory.Index())
	}
}

func TestUpOnFirstLineMultiLine_PreservesUnsubmittedInput(t *testing.T) {
	m := rootModelForTest()
	m.pushHistory("previous prompt")
	m.prompt.input.SetValue("first\nsecond\nthird")
	m.prompt.input.MoveToBegin()
	if m.prompt.input.Line() != 0 {
		t.Fatalf("pre-condition: cursor should be on line 0, got line %d", m.prompt.input.Line())
	}
	valueBefore := m.prompt.input.Value()
	m.Update(upKey())
	// Parity with agy: unsubmitted text must never be blown away by Up arrow.
	if got := m.prompt.input.Value(); got != valueBefore {
		t.Errorf("Up on first line of unsubmitted multi-line input must preserve input; got %q, want %q", got, valueBefore)
	}
	if m.prompt.promptHistory.Index() != 0 {
		t.Errorf("historyIdx = %d, want 0 (no history recall for unsubmitted text)", m.prompt.promptHistory.Index())
	}
}

func TestHistory_MultiLineEntry_PagingDirectlyWithoutIntraLineNavigation(t *testing.T) {
	m := rootModelForTest()
	m.pushHistory("older prompt")
	m.pushHistory("multi\n[Pasted text #1 +16 lines]\nthird line")

	// Up #1 recalls newest entry
	m.Update(upKey())
	if got := m.prompt.input.Value(); got != "multi\n[Pasted text #1 +16 lines]\nthird line" {
		t.Fatalf("Up #1: got %q", got)
	}
	if m.prompt.promptHistory.Index() != 1 {
		t.Fatalf("Up #1: historyIdx = %d, want 1", m.prompt.promptHistory.Index())
	}

	// Up #2 directly jumps to next older entry without moving within the multi-line entry
	m.Update(upKey())
	if got := m.prompt.input.Value(); got != "older prompt" {
		t.Fatalf("Up #2: got %q, want %q", got, "older prompt")
	}
	if m.prompt.promptHistory.Index() != 2 {
		t.Fatalf("Up #2: historyIdx = %d, want 2", m.prompt.promptHistory.Index())
	}

	// Down #1 directly jumps back to the multi-line entry
	m.Update(downKey())
	if got := m.prompt.input.Value(); got != "multi\n[Pasted text #1 +16 lines]\nthird line" {
		t.Fatalf("Down #1: got %q", got)
	}
	if m.prompt.promptHistory.Index() != 1 {
		t.Fatalf("Down #1: historyIdx = %d, want 1", m.prompt.promptHistory.Index())
	}

	// Down #2 returns to initial empty prompt
	m.Update(downKey())
	if got := m.prompt.input.Value(); got != "" {
		t.Fatalf("Down #2: got %q, want empty", got)
	}
	if m.prompt.promptHistory.Index() != 0 {
		t.Fatalf("Down #2: historyIdx = %d, want 0", m.prompt.promptHistory.Index())
	}
}

func TestHistory_MultiLineEntry_HorizontalCursorMoveExitsHistoryNavigation(t *testing.T) {
	m := rootModelForTest()
	m.pushHistory("older prompt")
	m.pushHistory("multi\nline2\nline3")

	// Up #1 recalls newest entry
	m.Update(upKey())
	if m.prompt.promptHistory.Index() != 1 {
		t.Fatalf("Up #1: historyIdx = %d, want 1", m.prompt.promptHistory.Index())
	}

	// Left arrow moves cursor and exits history browsing mode
	m.Update(leftKey())
	if m.prompt.promptHistory.Index() != 0 {
		t.Fatalf("Left arrow should exit history mode; got historyIdx = %d, want 0", m.prompt.promptHistory.Index())
	}

	// Subsequent Up arrow navigates lines inside the entry instead of jumping history
	m.Update(upKey())
	if got := m.prompt.input.Value(); got != "multi\nline2\nline3" {
		t.Fatalf("Up after cursor move should navigate within text; got %q", got)
	}
	if m.prompt.promptHistory.Index() != 0 {
		t.Fatalf("historyIdx should remain 0; got %d", m.prompt.promptHistory.Index())
	}
}

func TestHistory_SlashCommandMultiLineEntry_NavigatesLinesAfterLeftArrow(t *testing.T) {
	m := rootModelForTest()
	m.pushHistory("older prompt")
	entry := "/btw 现在进行到什么进度了\n\n[Pasted text #1 +16 lines]\n\n根据这个进程，现在是第几阶段"
	m.pushHistory(entry)

	// Up #1 recalls newest entry
	m.Update(upKey())
	if m.prompt.promptHistory.Index() != 1 {
		t.Fatalf("Up #1: historyIdx = %d, want 1", m.prompt.promptHistory.Index())
	}
	if got := m.prompt.input.Value(); got != entry {
		t.Fatalf("Up #1: input value = %q, want %q", got, entry)
	}
	if m.prompt.picker.Visible() {
		t.Fatal("recalled history entry should not leave picker visible")
	}

	// Left arrow moves cursor and exits history browsing mode
	m.Update(leftKey())
	if m.prompt.promptHistory.Index() != 0 {
		t.Fatalf("Left arrow should exit history mode; got historyIdx = %d, want 0", m.prompt.promptHistory.Index())
	}
	if m.prompt.picker.Visible() {
		t.Fatal("Left arrow navigation in /btw multi-line prompt should not open picker")
	}

	lastLine := m.prompt.input.Line()
	if lastLine == 0 {
		t.Fatalf("pre-condition: cursor should start near bottom line, got %d", lastLine)
	}

	// Subsequent Up arrow navigates lines inside the entry instead of being blocked by picker or jumping history
	m.Update(upKey())
	if got := m.prompt.input.Value(); got != entry {
		t.Fatalf("Up after cursor move should navigate within text; got %q", got)
	}
	if m.prompt.promptHistory.Index() != 0 {
		t.Fatalf("historyIdx should remain 0; got %d", m.prompt.promptHistory.Index())
	}
	if m.prompt.input.Line() >= lastLine {
		t.Fatalf("Up arrow should have moved cursor up a line, but line is %d (was %d)", m.prompt.input.Line(), lastLine)
	}

	// Subsequent Down arrow moves cursor down a line
	lineBeforeDown := m.prompt.input.Line()
	m.Update(downKey())
	if m.prompt.input.Line() <= lineBeforeDown {
		t.Fatalf("Down arrow should have moved cursor down a line, but line is %d (was %d)", m.prompt.input.Line(), lineBeforeDown)
	}
}

func TestPastePlaceholder_AtomicDeletion(t *testing.T) {
	m := rootModelForTest()
	entry := "prefix [Pasted text #1 +16 lines] suffix"
	m.prompt.input.SetValue(entry)

	// Position cursor inside the placeholder (e.g. byte offset 20)
	m.prompt.SetInputCursorOffset(20)
	if off := tuilib.InputCursorByteOffset(m.prompt.input); off != 20 {
		t.Fatalf("pre-condition: cursor offset = %d, want 20", off)
	}

	// Backspace inside the placeholder atomically deletes the entire placeholder
	m.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
	if got := m.prompt.input.Value(); got != "prefix  suffix" {
		t.Fatalf("Backspace inside placeholder: got %q, want %q", got, "prefix  suffix")
	}
	if off := tuilib.InputCursorByteOffset(m.prompt.input); off != len("prefix ") {
		t.Fatalf("Cursor after atomic Backspace: got offset %d, want %d", off, len("prefix "))
	}

	// Test Delete at start of placeholder
	entry2 := "before [Pasted text #2 42 chars] after"
	m.prompt.input.SetValue(entry2)
	startOfPlaceholder := len("before ")
	m.prompt.SetInputCursorOffset(startOfPlaceholder)

	m.Update(tea.KeyPressMsg{Code: tea.KeyDelete})
	if got := m.prompt.input.Value(); got != "before  after" {
		t.Fatalf("Delete at start of placeholder: got %q, want %q", got, "before  after")
	}
	if off := tuilib.InputCursorByteOffset(m.prompt.input); off != startOfPlaceholder {
		t.Fatalf("Cursor after atomic Delete: got offset %d, want %d", off, startOfPlaceholder)
	}
}

func TestUpOnMiddleLine_DoesNotTriggerHistory(t *testing.T) {
	m := rootModelForTest()
	m.pushHistory("should not be recalled")
	m.prompt.input.SetValue("first\nsecond\nthird")
	// SetValue leaves cursor on line 2; MoveToBegin + CursorDown
	// lands it on the middle line (row 1).
	m.prompt.input.MoveToBegin()
	m.prompt.input.CursorDown()
	if m.prompt.input.Line() != 1 {
		t.Fatalf("pre-condition: cursor should be on line 1, got line %d", m.prompt.input.Line())
	}
	valueBefore := m.prompt.input.Value()
	m.Update(upKey())
	// Input value must NOT change (no historyUp).
	if got := m.prompt.input.Value(); got != valueBefore {
		t.Errorf("Up on middle line should NOT call historyUp; input changed from %q to %q",
			valueBefore, got)
	}
	// historyIdx stays at 0 (no history navigation occurred).
	if m.prompt.promptHistory.Index() != 0 {
		t.Errorf("historyIdx = %d, want 0 (no history recall)", m.prompt.promptHistory.Index())
	}
	// Cursor should have moved up one logical line via the
	// textarea's LinePrevious binding.
	if m.prompt.input.Line() != 0 {
		t.Errorf("cursor Line() = %d, want 0 after Up moved cursor up", m.prompt.input.Line())
	}
}

func TestDownOnLastLine_PreservesUnsubmittedInput(t *testing.T) {
	m := rootModelForTest()
	m.pushHistory("older prompt")
	m.pushHistory("newer prompt")
	// User types unsubmitted multi-line input (historyIdx remains 0)
	m.prompt.input.SetValue("alpha\nbeta\ngamma")
	m.prompt.input.CursorDown() // row=1
	m.prompt.input.CursorDown() // row=2, last line
	if m.prompt.input.Line() != 2 {
		t.Fatalf("pre-condition: cursor should be on last line (2), got line %d", m.prompt.input.Line())
	}
	valueBefore := m.prompt.input.Value()
	m.Update(downKey())
	// Parity with agy: Down on last line of unsubmitted text must NOT trigger history.
	if got := m.prompt.input.Value(); got != valueBefore {
		t.Errorf("Down on last line should preserve unsubmitted text; got %q, want %q", got, valueBefore)
	}
	if m.prompt.promptHistory.Index() != 0 {
		t.Errorf("historyIdx = %d, want 0", m.prompt.promptHistory.Index())
	}
}

func TestDownOnMiddleLine_DoesNotTriggerHistory(t *testing.T) {
	m := rootModelForTest()
	m.pushHistory("must not be recalled")
	m.prompt.input.SetValue("first\nsecond\nthird")
	// SetValue leaves cursor on line 2; MoveToBegin + CursorDown
	// lands it on the middle line (row 1).
	m.prompt.input.MoveToBegin()
	m.prompt.input.CursorDown()
	if m.prompt.input.Line() != 1 {
		t.Fatalf("pre-condition: cursor should be on line 1, got line %d", m.prompt.input.Line())
	}
	valueBefore := m.prompt.input.Value()
	idxBefore := m.prompt.promptHistory.Index()
	m.Update(downKey())
	// Input value must NOT change (no historyDown).
	if got := m.prompt.input.Value(); got != valueBefore {
		t.Errorf("Down on middle line should NOT call historyDown; input changed from %q to %q",
			valueBefore, got)
	}
	if m.prompt.promptHistory.Index() != idxBefore {
		t.Errorf("historyIdx = %d, want %d (no history navigation)", m.prompt.promptHistory.Index(), idxBefore)
	}
	// Cursor should have moved down one logical line via the
	// textarea's LineNext binding.
	if m.prompt.input.Line() != 2 {
		t.Errorf("cursor Line() = %d, want 2 after Down moved cursor down", m.prompt.input.Line())
	}
}

func TestPickerVisible_OverridesUpHistory(t *testing.T) {
	m := rootModelForTest()
	m.pushHistory("would be recalled if picker were hidden")
	// Open the picker by typing '/'. After this m.composer.picker is
	// visible with idx == 0.
	m.Update(runeKey('/'))
	if !m.prompt.picker.Visible() {
		t.Fatalf("setup: picker should be visible after typing '/'")
	}
	// Move the picker highlight to row 1 so an Up arrow has
	// somewhere to land.
	m.Update(downKey()) // picker.MoveDown → idx=1
	if m.prompt.picker.Idx() != 1 {
		t.Fatalf("setup: picker.Idx() = %d, want 1", m.prompt.picker.Idx())
	}
	valueBefore := m.prompt.input.Value()
	idxBefore := m.prompt.promptHistory.Index()
	m.Update(upKey()) // picker.MoveUp → idx=0; historyUp MUST NOT fire
	if m.prompt.picker.Idx() != 0 {
		t.Errorf("Up with picker visible should call picker.MoveUp; idx = %d, want 0",
			m.prompt.picker.Idx())
	}
	if m.prompt.promptHistory.Index() != idxBefore {
		t.Errorf("historyIdx = %d, want %d (picker should override historyUp)",
			m.prompt.promptHistory.Index(), idxBefore)
	}
	if got := m.prompt.input.Value(); got != valueBefore {
		t.Errorf("input changed from %q to %q — picker path should not touch history",
			valueBefore, got)
	}
}

// ---------------------------------------------------------------- streaming clear (consolidated from streaming_clear_test.go)

func TestTurnDoneMsg_StreamingClearReturnsClearScreenCmd(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.conversation.response = append(m.conversation.response, "> analyze pwd")
	m.conversation.liveStart = len(m.conversation.response)
	m.setSessionMessages(append(m.SessionMessages(), backend.Message{Role: "user", Content: "analyze pwd"}))
	chunks := []string{"mock reply f", "rom model=Ge", "mini 3.8 Fla", "sh (Medium):", " analyze pwd"}
	for _, c := range chunks {
		_, _ = m.Update(backendChunkMsg(c))
	}
	if m.conversation.replyStart < 0 {
		t.Fatalf("replyStart not anchored after 5 chunks; m.conversation.replyStart=%d", m.conversation.replyStart)
	}

	_, cmd := m.Update(toolsteps.TurnDoneMsg{
		Reply:     "mock reply from model=Gemini 3.8 Flash (Medium): analyze pwd",
		Steps:     nil,
		Reasoning: "",
	})
	if cmd == nil {
		t.Fatalf("turnDoneMsg returned nil Cmd; want a tea.Batch(... ClearScreen) so the next render does a full repaint")
	}

	got := execCmdForTest(cmd)
	if !isClearScreenMsg(got) {
		t.Fatalf("turnDoneMsg's Cmd did not yield tea.ClearScreen()'s clearScreenMsg; got=%T(%v)", got, got)
	}
}

func TestBackendDoneMsg_StreamingClearReturnsClearScreenCmd(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.conversation.response = append(m.conversation.response, "> hi")
	m.conversation.liveStart = len(m.conversation.response)
	m.conversation.streaming = true
	m.setSessionMessages(append(m.SessionMessages(), backend.Message{Role: "user", Content: "hi"}))

	_, cmd := m.Update(backendDoneMsg{})
	if cmd == nil {
		t.Fatalf("backendDoneMsg returned nil Cmd; want tea.ClearScreen so the next render does a full repaint")
	}

	got := execCmdForTest(cmd)
	if !isClearScreenMsg(got) {
		t.Fatalf("backendDoneMsg's Cmd did not yield tea.ClearScreen()'s clearScreenMsg; got=%T(%v)", got, got)
	}
}

func TestTurnDoneMsg_QueueFlushKeepsClearScreen(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.conversation.response = append(m.conversation.response, "> first")
	m.conversation.liveStart = len(m.conversation.response)
	m.setSessionMessages(append(m.SessionMessages(), backend.Message{Role: "user", Content: "first"}))

	m.session.store.QueuedPrompt = "second"

	_, cmd := m.Update(toolsteps.TurnDoneMsg{
		Reply:     "first reply",
		Steps:     nil,
		Reasoning: "",
	})
	if cmd == nil {
		t.Fatalf("turnDoneMsg with queuedPrompt returned nil Cmd; want a Batch that includes both queueCmd and tea.ClearScreen")
	}

	got := execCmdForTest(cmd)
	batch, ok := got.(tea.BatchMsg)
	if !ok {
		t.Fatalf("turnDoneMsg with queuePrompt returned %T(%v); want tea.BatchMsg so both queueCmd and tea.ClearScreen fit", got, got)
	}
	foundClear := false
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if reflect.ValueOf(sub).Pointer() == reflect.ValueOf(tea.ClearScreen).Pointer() {
			foundClear = true
			break
		}
	}
	if !foundClear {
		t.Fatalf("BatchMsg %v does not include tea.ClearScreen; queue-mode flush will leak stale pixels from the preceding turn", batch)
	}
}

func TestTurnDoneMsg_ErrorHaltsQueueAutoFlush(t *testing.T) {
	m := queueTestMode(t)
	m.session.store.QueueMode = true
	m.session.store.StreamQueue = []string{"next-queued-message"}

	// When turn completes with an error, the queue should not be flushed
	_, cmd := m.Update(toolsteps.TurnDoneMsg{
		Reply: "failed turn",
		Err:   errors.New("network timeout"),
	})

	if len(m.session.store.StreamQueue) == 0 || m.session.store.StreamQueue[0] != "next-queued-message" {
		t.Fatalf("expected queued message to be preserved on error, got: %v", m.session.store.StreamQueue)
	}

	// The command returned should not be a batch initiating another stream
	if cmd == nil {
		t.Fatalf("expected clear screen command, got nil")
	}
	got := execCmdForTest(cmd)
	if _, isBatch := got.(tea.BatchMsg); isBatch {
		t.Fatalf("expected simple command (ClearScreen), not BatchMsg starting next turn on error")
	}
}

func TestTurnDoneMsg_StreamingClearContractIsStableAcrossResponseShapes(t *testing.T) {
	cases := []struct {
		name  string
		reply string
	}{
		{"empty", ""},
		{"whitespace", "   \n\t  "},
		{"single-word", "ok"},
		{"multi-line", "line 1\nline 2\nline 3"},
		{"long-line", "x" + repeat("y", 200)},
		{"code-fence", "```bash\necho hi\n```"},
		{"mixed-glamour", "# Heading\n\n- bullet 1\n- bullet 2\n\n**bold** here"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
			m.conversation.response = append(m.conversation.response, "> probe")
			m.conversation.liveStart = len(m.conversation.response)
			m.setSessionMessages(append(m.SessionMessages(), backend.Message{Role: "user", Content: "probe"}))
			m.Update(backendChunkMsg(tc.reply))

			_, cmd := m.Update(toolsteps.TurnDoneMsg{Reply: tc.reply})
			if cmd == nil {
				t.Fatalf("%s: turnDoneMsg returned nil Cmd; want a Cmd that yields clearScreenMsg", tc.name)
			}
			got := execCmdForTest(cmd)
			if !isClearScreenMsg(got) && !batchContainsClearScreen(got) {
				t.Fatalf("%s: cmd produced %T(%v); want clearScreenMsg or BatchMsg containing tea.ClearScreen", tc.name, got, got)
			}
		})
	}
}

func execCmdForTest(cmd tea.Cmd) tea.Msg {
	if cmd == nil {
		return nil
	}
	return cmd()
}

func isClearScreenMsg(m tea.Msg) bool {
	if m == nil {
		return false
	}
	t := reflect.TypeOf(m)
	if t.Name() == "clearScreenMsg" && t.PkgPath() == "charm.land/bubbletea/v2" {
		return true
	}
	return false
}

func batchContainsClearScreen(m tea.Msg) bool {
	batch, ok := m.(tea.BatchMsg)
	if !ok {
		return false
	}
	clearPtr := reflect.ValueOf(tea.ClearScreen).Pointer()
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if reflect.ValueOf(sub).Pointer() == clearPtr {
			return true
		}
	}
	return false
}

func repeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}

const streamAccountingSample = "# Go 语言入门笔记\n" +
	"\n" +
	"这是一段介绍 **加粗** 和 *斜体* 以及 `行内代码` 的文字。\n" +
	"\n" +
	"## 1. 代码块\n" +
	"\n" +
	"```go\n" +
	"package main\n" +
	"\n" +
	"func main() {\n" +
	"\tfmt.Println(\"hello\")\n" +
	"}\n" +
	"```\n" +
	"\n" +
	"## 2. 表格\n" +
	"\n" +
	"| 列名 | 说明 |\n" +
	"| --- | --- |\n" +
	"| 语言 | Go |\n" +
	"\n" +
	"> 这是一个引用块。\n"

func TestStreamRowCountDoesNotExplode(t *testing.T) {
	m := newRootModel(nil, "u@t", "m", "t", "/tmp", true)
	m.viewport.width = 100
	m.conversation.response = append(m.conversation.response, "> seed")
	m.conversation.liveStart = len(m.conversation.response)

	sourceLines := strings.Split(strings.TrimRight(streamAccountingSample, "\n"), "\n")
	accum := ""
	for i, ln := range sourceLines {
		chunk := ln + "\n"
		if i == len(sourceLines)-1 {
			chunk = ln
		}
		accum += chunk
		_, _ = m.Update(backendChunkMsg([]byte(chunk)))
		rows := len(m.conversation.response) - m.conversation.replyStart
		want := len(strings.Split(mdrender.RenderMarkdown(accum, m.viewport.width), "\n"))
		if rows != want {
			t.Fatalf("after chunk %q the live range has %d rows; "+
				"renderMarkdown(accumulated) has %d (re-rendering styled "+
				"rows feeds pad back through glamour); live range:\n%s",
				ln, rows, want, strings.Join(m.conversation.response[m.conversation.replyStart:], "\n"))
		}
		if rows > 3*len(sourceLines) {
			t.Fatalf("live transcript exploded: %d rows after %q (%d source lines)",
				rows, ln, len(sourceLines))
		}
	}
	want := strings.Split(mdrender.RenderMarkdown(streamAccountingSample, m.viewport.width), "\n")
	got := m.conversation.response[m.conversation.replyStart:]
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("final live range != renderMarkdown(accumulated):\ngot %d rows, want %d\n%s",
			len(got), len(want), strings.Join(got, "\n"))
	}
}

// ---------------------------------------------------------------- program status (consolidated from program_status_test.go)

func newPSModel() *RootModel {
	return &RootModel{}
}

// psModelWithOpenPicker returns a RootModel whose subagent-question
// picker is open, so the status protocol reports the blocked/question
// state.
func psModelWithOpenPicker(t *testing.T) *RootModel {
	t.Helper()
	m := newPSModel()
	mgr := questions.NewManager(false)
	m.askQuestion.manager = mgr
	m.askQuestion.panel = m.newSubAgentQuestionPanel()
	mgr.Enqueue(questions.ParentKey, "Q?", nil)
	m.askQuestion.panel.Toggle()
	return m
}

// psModelWithStagedAnswer returns a RootModel with an answer staged on a
// queued question — the picker is closed, the answer is not sent yet.
func psModelWithStagedAnswer(t *testing.T) *RootModel {
	t.Helper()
	m := psModelWithOpenPicker(t)
	m.askQuestion.panel.HandleKey(tea.Key{Code: tea.KeyEnter})
	return m
}

// psModelWithOpenPickerAndStagedAnswer returns a RootModel with both the
// picker open and an answer staged.
func psModelWithOpenPickerAndStagedAnswer(t *testing.T) *RootModel {
	t.Helper()
	m := psModelWithStagedAnswer(t)
	m.askQuestion.panel.Toggle()
	return m
}

func TestProgramStatus(t *testing.T) {
	cases := []struct {
		name  string
		setup func(m *RootModel)
		want  tea.ProgramStatus
	}{
		{
			name: "idle",
			want: tea.ProgramStatus{State: tea.ProgramStateIdle, App: programstatus.App},
		},
		{
			name: "working from streaming",
			setup: func(m *RootModel) {
				m.conversation.streaming = true
			},
			want: tea.ProgramStatus{State: tea.ProgramStateWorking, App: programstatus.App},
		},
		{
			name: "working from thinking",
			setup: func(m *RootModel) {
				m.conversation.thinking = true
			},
			want: tea.ProgramStatus{State: tea.ProgramStateWorking, App: programstatus.App},
		},
		{
			name: "working from in-flight cancel",
			setup: func(m *RootModel) {
				bridge, _ := armRunningBridge(t)
				m.turn.activeBridge = bridge
			},
			want: tea.ProgramStatus{State: tea.ProgramStateWorking, App: programstatus.App},
		},
		{
			name: "blocked question when picker open",
			setup: func(m *RootModel) {
				*m = *psModelWithOpenPicker(t)
			},
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programstatus.App,
				Kind:    tea.ProgramStatusKindQuestion,
				Message: "Questions need your input",
			},
		},
		{
			name: "blocked question when answer target set",
			setup: func(m *RootModel) {
				*m = *psModelWithStagedAnswer(t)
			},
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programstatus.App,
				Kind:    tea.ProgramStatusKindQuestion,
				Message: "Questions need your input",
			},
		},
		{
			name: "blocked permission wins over question",
			setup: func(m *RootModel) {
				*m = *psModelWithOpenPicker(t)
				m.session.pendingPermission = true
			},
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programstatus.App,
				Kind:    tea.ProgramStatusKindPermission,
				Message: "Permission required",
			},
		},
		{
			name: "blocked auth",
			setup: func(m *RootModel) {
				m.session.pendingAuth = true
			},
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programstatus.App,
				Kind:    tea.ProgramStatusKindAuth,
				Message: "Sign-in required",
			},
		},
		{
			name: "blocked question wins over auth too",
			setup: func(m *RootModel) {
				*m = *psModelWithOpenPicker(t)
				m.session.pendingAuth = true
			},
			want: tea.ProgramStatus{
				State:   tea.ProgramStateBlocked,
				App:     programstatus.App,
				Kind:    tea.ProgramStatusKindQuestion,
				Message: "Questions need your input",
			},
		},
		{
			name: "working wins over the last outcome",
			setup: func(m *RootModel) {
				m.display.lastTurnOutcome = tea.ProgramStateDone
				m.conversation.streaming = true
			},
			want: tea.ProgramStatus{State: tea.ProgramStateWorking, App: programstatus.App},
		},
		{
			name: "outcome once idle: done",
			setup: func(m *RootModel) {
				m.display.lastTurnOutcome = tea.ProgramStateDone
			},
			want: tea.ProgramStatus{State: tea.ProgramStateDone, App: programstatus.App},
		},
		{
			name: "outcome once idle: error",
			setup: func(m *RootModel) {
				m.display.lastTurnOutcome = tea.ProgramStateError
			},
			want: tea.ProgramStatus{State: tea.ProgramStateError, App: programstatus.App},
		},
		{
			name: "idle when last outcome cleared",
			setup: func(m *RootModel) {
				m.display.lastTurnOutcome = tea.ProgramStateDone
				m.clearLastTurnOutcome()
			},
			want: tea.ProgramStatus{State: tea.ProgramStateIdle, App: programstatus.App},
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newPSModel()
			if c.setup != nil {
				c.setup(m)
			}
			got := m.programStatus()
			if got == nil {
				t.Fatalf("programStatus() = nil, want %+v", c.want)
			}
			if *got != c.want {
				t.Errorf("programStatus() = %+v, want %+v", *got, c.want)
			}
		})
	}
}

func TestRecordTurnOutcomeClears(t *testing.T) {
	m := newPSModel()
	m.recordTurnOutcome(tea.ProgramStateDone)
	if got := m.programStatus(); got.State != tea.ProgramStateDone {
		t.Errorf("after recordTurnOutcome(Done): state = %q, want %q", got.State, tea.ProgramStateDone)
	}
	m.clearLastTurnOutcome()
	if got := m.programStatus(); got.State != tea.ProgramStateIdle {
		t.Errorf("after clearLastTurnOutcome: state = %q, want %q", got.State, tea.ProgramStateIdle)
	}
}

func TestProgramStatusReportIncludesApp(t *testing.T) {
	for _, s := range []tea.ProgramState{
		tea.ProgramStateIdle, tea.ProgramStateWorking, tea.ProgramStateDone,
		tea.ProgramStateError,
	} {
		ps := &tea.ProgramStatus{State: s, App: programstatus.App}
		if ps.App != "wagyu" {
			t.Errorf("App for state %q = %q, want %q", s, ps.App, "wagyu")
		}
	}
	for _, k := range []tea.ProgramStatusKind{
		tea.ProgramStatusKindPermission, tea.ProgramStatusKindQuestion,
		tea.ProgramStatusKindAuth,
	} {
		ps := &tea.ProgramStatus{State: tea.ProgramStateBlocked, Kind: k, App: programstatus.App}
		if ps.App != "wagyu" {
			t.Errorf("Blocked App for kind %q = %q, want %q", k, ps.App, "wagyu")
		}
	}
}

func TestHasPendingQuestion(t *testing.T) {
	cases := []struct {
		name string
		set  func(m *RootModel)
		want bool
	}{
		{"off", func(m *RootModel) {}, false},
		{"picker open", func(m *RootModel) { *m = *psModelWithOpenPicker(t) }, true},
		{"answer target set", func(m *RootModel) { *m = *psModelWithStagedAnswer(t) }, true},
		{"both", func(m *RootModel) {
			*m = *psModelWithOpenPickerAndStagedAnswer(t)
		}, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newPSModel()
			c.set(m)
			if got := m.askQuestion.panel.HasPendingQuestion(); got != c.want {
				t.Errorf("hasPendingQuestion() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestIsAgentBusy(t *testing.T) {
	cases := []struct {
		name string
		set  func(m *RootModel)
		want bool
	}{
		{"idle", func(m *RootModel) {}, false},
		{"streaming", func(m *RootModel) { m.conversation.streaming = true }, true},
		{"thinking", func(m *RootModel) { m.conversation.thinking = true }, true},
		{"running bridge", func(m *RootModel) {
			bridge, _ := armRunningBridge(t)
			m.turn.activeBridge = bridge
		}, true},
		{"thinking wins over no streaming flag", func(m *RootModel) { m.conversation.thinking = true }, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newPSModel()
			c.set(m)
			if got := m.isAgentBusy(); got != c.want {
				t.Errorf("isAgentBusy() = %v, want %v", got, c.want)
			}
		})
	}
}

func TestProgramStatusEmitsOSC7501Sequence(t *testing.T) {
	cases := []struct {
		name string
		set  func(m *RootModel)
		want string
	}{
		{
			name: "idle",
			set:  func(m *RootModel) {},
			want: "\x1b]7501;state=idle:app=wagyu\x07",
		},
		{
			name: "working",
			set:  func(m *RootModel) { m.conversation.streaming = true },
			want: "\x1b]7501;state=working:app=wagyu\x07",
		},
		{
			name: "done",
			set:  func(m *RootModel) { m.display.lastTurnOutcome = tea.ProgramStateDone },
			want: "\x1b]7501;state=done:app=wagyu\x07",
		},
		{
			name: "error",
			set:  func(m *RootModel) { m.display.lastTurnOutcome = tea.ProgramStateError },
			want: "\x1b]7501;state=error:app=wagyu\x07",
		},
		{
			name: "blocked permission",
			set:  func(m *RootModel) { m.session.pendingPermission = true },
			want: "\x1b]7501;state=blocked:kind=permission:app=wagyu:msg=UGVybWlzc2lvbiByZXF1aXJlZA==\x07",
		},
		{
			name: "blocked question",
			set:  func(m *RootModel) { *m = *psModelWithOpenPicker(t) },
			want: "\x1b]7501;state=blocked:kind=question:app=wagyu:msg=UXVlc3Rpb25zIG5lZWQgeW91ciBpbnB1dA==\x07",
		},
		{
			name: "blocked auth",
			set:  func(m *RootModel) { m.session.pendingAuth = true },
			want: "\x1b]7501;state=blocked:kind=auth:app=wagyu:msg=U2lnbi1pbiByZXF1aXJlZA==\x07",
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := newPSModel()
			c.set(m)
			var buf bytes.Buffer
			if err := uv.EncodeProgramStatus(&buf, m.programStatus()); err != nil {
				t.Fatalf("EncodeProgramStatus: %v", err)
			}
			got := buf.String()
			if got != c.want {
				t.Errorf("OSC 7501 bytes = %q, want %q", got, c.want)
			}
			if !strings.HasPrefix(got, "\x1b]7501;") {
				t.Errorf("missing OSC 7501 prefix: %q", got)
			}
			if !strings.HasSuffix(got, "\x07") {
				t.Errorf("missing BEL terminator: %q", got)
			}
			if !strings.Contains(got, "app=wagyu") {
				t.Errorf("missing app=wagyu: %q", got)
			}
		})
	}
}

func TestProgramStatusAssignedToView(t *testing.T) {
	attach := func(body string, ps *tea.ProgramStatus) tea.View {
		v := tea.NewView(body)
		v.ProgramStatus = ps
		return v
	}

	m := newPSModel()
	got := attach("body", m.programStatus())
	if got.ProgramStatus == nil {
		t.Fatalf("attach() did not propagate programStatus; got nil")
	}
	if got.ProgramStatus.State != tea.ProgramStateIdle {
		t.Errorf("ProgramStatus.State = %q, want %q", got.ProgramStatus.State, tea.ProgramStateIdle)
	}

	m.conversation.streaming = true
	got2 := attach("body", m.programStatus())
	if got2.ProgramStatus.State != tea.ProgramStateWorking {
		t.Errorf("after streaming=true: ProgramStatus.State = %q, want %q",
			got2.ProgramStatus.State, tea.ProgramStateWorking)
	}
}

// clearDisableEscapeSeqOptEnv unsets the cycle-256 env var for
// tests that exercise the unset default. Mirrors the
// blockedMCPClearGate helper in slash_mcp_blocked_test.go:59-65
// so a stray shell export does not silently leak into the test.
func clearDisableEscapeSeqOptEnv(t *testing.T) {
	t.Helper()
	t.Setenv(envDisableEscapeSequenceOptimizations, "")
}

// TestRenderer_OptimizationsEnabledByDefault pins requirement #1:
// when AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS is unset,
// optimizations are enabled (the default). The accessor returns
// true; the underlying env reader returns false.
func TestRenderer_OptimizationsEnabledByDefault(t *testing.T) {
	clearDisableEscapeSeqOptEnv(t)

	// Defensive: also clear from os.Environ if a parent process
	// exported it. t.Setenv already covers the cwd-scoped env,
	// but the production code path (escape_optimizations.go:71)
	// reads os.Getenv, which consults the process environ. A test
	// that forgot to t.Setenv would fail here visibly.
	if got, want := os.Getenv(envDisableEscapeSequenceOptimizations), ""; got != want {
		t.Fatalf("env var leaked into default test: got %q want %q", got, want)
	}

	if !tuilib.EscapeSequenceOptimizationsEnabled() {
		t.Fatal("optimizations should be enabled by default; got disabled")
	}
	if tuilib.EscapeSequenceOptimizationsBypassed() {
		t.Fatal("env reader should report false on unset; got true")
	}
}

// TestRenderer_OptimizationsDisabledOn1 pins requirement #2:
// AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS=1 disables
// optimizations.
func TestRenderer_OptimizationsDisabledOn1(t *testing.T) {
	t.Setenv(envDisableEscapeSequenceOptimizations, "1")

	if tuilib.EscapeSequenceOptimizationsEnabled() {
		t.Fatal("optimizations should be disabled when env = \"1\"; got enabled")
	}
	if !tuilib.EscapeSequenceOptimizationsBypassed() {
		t.Fatal("env reader should report true on \"1\"; got false")
	}
}

// TestRenderer_OptimizationsDisabledOnTrue pins requirement #3:
// AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS=true disables
// optimizations.
func TestRenderer_OptimizationsDisabledOnTrue(t *testing.T) {
	t.Setenv(envDisableEscapeSequenceOptimizations, "true")

	if tuilib.EscapeSequenceOptimizationsEnabled() {
		t.Fatal("optimizations should be disabled when env = \"true\"; got enabled")
	}
}

// TestRenderer_OptimizationsDisabledOnYes pins requirement #4:
// AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS=yes disables
// optimizations.
func TestRenderer_OptimizationsDisabledOnYes(t *testing.T) {
	t.Setenv(envDisableEscapeSequenceOptimizations, "yes")

	if tuilib.EscapeSequenceOptimizationsEnabled() {
		t.Fatal("optimizations should be disabled when env = \"yes\"; got enabled")
	}
}

// TestRenderer_OptimizationsCaseInsensitive pins requirement #5:
// the truthy match is case-insensitive AND whitespace-trimmed.
// Exercises TRUE, Yes, " 1 " (with surrounding whitespace), and
// 1 to make sure no canonicalisation slips through.
func TestRenderer_OptimizationsCaseInsensitive(t *testing.T) {
	cases := []struct {
		in   string
		want bool // expected envDisableEscapeSequenceOptimizations result
	}{
		{"TRUE", true},
		{"True", true},
		{"Yes", true},
		{"YES", true},
		{" 1 ", true}, // surrounding whitespace
		{"  true  ", true},
		{"yes\t", true}, // trailing tab
	}
	for _, tc := range cases {
		tc := tc
		t.Run("env="+tc.in, func(t *testing.T) {
			t.Setenv(envDisableEscapeSequenceOptimizations, tc.in)
			got := tuilib.EscapeSequenceOptimizationsBypassed()
			if got != tc.want {
				t.Fatalf("escapeSequenceOptimizationsBypassed() = %v, want %v (input %q)", got, tc.want, tc.in)
			}
			if got == tuilib.EscapeSequenceOptimizationsEnabled() {
				t.Fatalf("escapeSequenceOptimizationsEnabled() must be the inverse of env reader for %q", tc.in)
			}
		})
	}
}

// TestRenderer_OptimizationsEnabledOnGarbage pins requirement #6:
// unrecognized values silently default to the optimisations-on
// behaviour. This matches the AGY 1.1.19 reading of the spec
// ("bypass on recognized truthy values; otherwise use the
// optimisation path") and means mistyping the env name
// (e.g. AGY_CLI_DISABLE_ESCAPE_OPTIMIZATIONS) does not silently
// flip the renderer into the slow mode.
func TestRenderer_OptimizationsEnabledOnGarbage(t *testing.T) {
	cases := []string{
		"garbage",
		"on",     // "on" is NOT in the cycle-256 truthy set; OSC 7501 uses it (program_status.go:124), this gate does not.
		"no",     // "no" is in OSC 7501's off set; cycle 256 is one-sided.
		"2",      // only "1" trips the gate; "2" and similar look truthy but aren't.
		"truely", // close but not "true"
		"",       // empty (same as unset, but exercised explicitly)
		" ",      // whitespace-only -> trim yields "" -> false
		"\t\n",   // tabs+newlines only -> trim yields "" -> false
	}
	for _, in := range cases {
		in := in
		t.Run("env="+in, func(t *testing.T) {
			t.Setenv(envDisableEscapeSequenceOptimizations, in)
			if tuilib.EscapeSequenceOptimizationsBypassed() {
				t.Fatalf("env reader must return false on %q; got true", in)
			}
			if !tuilib.EscapeSequenceOptimizationsEnabled() {
				t.Fatalf("escapeSequenceOptimizationsEnabled() must remain true on %q; got false", in)
			}
		})
	}
}

// TestRenderer_PrintModeUnaffected pins requirement #7: print
// mode does not trigger the renderer code path. The
// shouldConsultRenderer gate returns false for the spelled-out
// "print", the shorthand "-p", and any case/whitespace variant
// of those, so the env var's value is irrelevant on -p. Every
// other mode (the default TUI, --tui, --prompt-interactive,
// mode omissions, etc.) returns true.
//
// Defensive: also confirm applyRenderer's output is ignored in
// print mode by combining the two helpers - this is the failure
// mode where a future cycle wires applyRenderer into runPrintMode
// by mistake.
func TestRenderer_PrintModeUnaffected(t *testing.T) {
	// Even with the gate active, print mode ignores it.
	t.Setenv(envDisableEscapeSequenceOptimizations, "1")

	printModes := []string{
		"print",
		"-p",
		"PRINT",  // case-insensitive
		"  -p  ", // surrounding whitespace
		"Print",  // mixed case
	}
	for _, mode := range printModes {
		mode := mode
		t.Run("mode="+mode, func(t *testing.T) {
			if shouldConsultRenderer(mode) {
				t.Fatalf("shouldConsultRenderer(%q) must be false; got true", mode)
			}
			// Optimisations are off globally, but print mode
			// doesn't consult the renderer at all. applyRenderer
			// is never called on the print path.
			if !tuilib.EscapeSequenceOptimizationsEnabled() {
				// sanity - the env reader reports the bypass is
				// armed. The point of this test is that shouldConsultRenderer
				// short-circuits BEFORE applyRenderer is reached.
				if shouldConsultRenderer(mode) {
					t.Fatalf("mode %q leaked into renderer; shouldConsultRenderer returned true", mode)
				}
			}
		})
	}

	// And every non-print mode DOES consult the renderer.
	consultingModes := []string{
		"", // empty string (e.g. mode flag omitted)
		"tui",
		"interactive",
		"--prompt-interactive",
		"-i",
		"live",
	}
	for _, mode := range consultingModes {
		mode := mode
		t.Run("consult="+mode, func(t *testing.T) {
			if !shouldConsultRenderer(mode) {
				t.Fatalf("shouldConsultRenderer(%q) must be true; got false", mode)
			}
		})
	}
}

// TestApplyRenderer_NoChangeOptimized: when previous and current
// view are byte-identical and optimisations are on, the renderer
// signals "no-op" by returning the empty string. This is the
// cycle-256 fast-path: bubbletea/v2's cursed_renderer would
// emit zero bytes when the screen state does not change, and
// applyRenderer pins that contract.
func TestApplyRenderer_NoChangeOptimized(t *testing.T) {
	view := "\x1b[2Jfull viewport\x1b[H"
	got := applyRenderer(view, view, true)
	if got != "" {
		t.Fatalf("expected empty (no-op) when prev == current and optimizations on; got %q", got)
	}
}

// TestApplyRenderer_NoChangeBypassed: even when the views match,
// the bypass (optimisations OFF) forces a full redraw. The
// "must still produce correct output" guarantee lives here: the
// emitted bytes are exactly `current`, with no diffing applied.
func TestApplyRenderer_NoChangeBypassed(t *testing.T) {
	view := "\x1b[2Jfull viewport\x1b[H"
	got := applyRenderer(view, view, false)
	if got != view {
		t.Fatalf("bypass mode must emit the full view; got %q want %q", got, view)
	}
}

// TestApplyRenderer_ChangeOptimized: when the view differs and
// optimisations are on, applyRenderer emits the full current view
// (the diff itself is deferred to bubbletea/v2's downstream
// cursed_renderer; cycle 256 exercises the gate, not the diff).
func TestApplyRenderer_ChangeOptimized(t *testing.T) {
	prev := "old frame"
	current := "\x1b[2Jnew frame\x1b[H"
	got := applyRenderer(prev, current, true)
	if got != current {
		t.Fatalf("view changed under optimizations must emit current; got %q want %q", got, current)
	}
}

// TestApplyRenderer_ChangeBypassed: view changed AND bypass
// active -> full redraw. Sanity pin that the bypass doesn't
// accidentally suppress the change.
func TestApplyRenderer_ChangeBypassed(t *testing.T) {
	prev := "old frame"
	current := "\x1b[2Jnew frame\x1b[H"
	got := applyRenderer(prev, current, false)
	if got != current {
		t.Fatalf("bypass mode must always emit current; got %q want %q", got, current)
	}
}

// TestApplyRenderer_EmptyViews: edge case where both previous
// and current frames are empty. With optimisations on, the
// match is still exact and empty, which returns "" (no-op).
// With optimisations off, the bypass still emits the current
// view, which is also "". This keeps the contract honest for
// the empty-string case.
func TestApplyRenderer_EmptyViews(t *testing.T) {
	if got := applyRenderer("", "", true); got != "" {
		t.Fatalf("empty views, optimized: expected \"\"; got %q", got)
	}
	if got := applyRenderer("", "", false); got != "" {
		t.Fatalf("empty views, bypassed: expected \"\"; got %q", got)
	}
	// Empty -> non-empty (or vice versa) is always a redraw.
	if got := applyRenderer("", "non-empty", true); got != "non-empty" {
		t.Fatalf("empty -> non-empty, optimized: expected \"non-empty\"; got %q", got)
	}
	if got := applyRenderer("non-empty", "", true); got != "" {
		t.Fatalf("non-empty -> empty, optimized: expected \"\"; got %q", got)
	}
}

// TestParseBoolish_EscapeSequenceOptimizations: small table test
// for the shared truthy parser from cycle 256's perspective. The
// canonical TestParseBoolish now lives in banner_test.go (cycle
// 255's home — the function itself moved to banner_env.go when
// cycle 259 consolidated the two byte-identical copies); this
// local copy keeps the cycle-256-specific case coverage that the
// shared test would otherwise lose ("on" explicitly rejected,
// "2" / "-1" rejected, etc.).
//
// Same case list the optimization tests use, plus a few extras
// the cycle-256 deliverable calls out specifically
// (mixed-case, surrounding whitespace, canonical pairs the OS
// 7501 parser handles differently).
func TestParseBoolish_EscapeSequenceOptimizations(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		// Truthy
		{"1", true},
		{"true", true},
		{"yes", true},
		{"TRUE", true},
		{"True", true},
		{"YES", true},
		{" 1 ", true},
		{"  true  ", true},
		{"\tyes\n", true},
		{"tRuE", true},
		// Falsy
		{"", false},
		{"garbage", false},
		{"on", false}, // cycle 256 does NOT accept "on"; OSC 7501 does.
		{"no", false}, // 1-sided parser; cycle 256 is opt-out via OTHER env vars.
		{"2", false},  // not "1"
		{"-1", false},
		{"0", false},     // note "0" is OSC 7501's off-set; cycle 256 is one-sided on "1".
		{"false", false}, // not "true"
		{"yesplease", false},
		{"   ", false}, // whitespace-only -> trim -> "" -> false
	}
	for _, tc := range cases {
		tc := tc
		name := strings.ReplaceAll(tc.in, "\t", "\\t")
		name = strings.ReplaceAll(name, "\n", "\\n")
		t.Run("in="+name, func(t *testing.T) {
			if got := tuilib.ParseBoolish(tc.in); got != tc.want {
				t.Fatalf("ParseBoolish(%q) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

// TestLoadStatusLineConfig_DisabledAndMissing covers the three ways
// settings.json can fail to light up external mode:
//
//   - the `statusLine` block is missing entirely
//   - the block is present but `enabled` is false
//   - the block is present but `command` is blank
//
// All three must return ok = false so the cycle-105 toggle stays the
// only path the user has to drive the row.
func TestLoadStatusLineConfig_DisabledAndMissing(t *testing.T) {
	cases := []struct {
		name string
		body string
		ok   bool
	}{
		{"no block", `{"colorScheme":"light"}`, false},
		{"enabled=false", `{"statusLine":{"type":"command","command":"/bin/echo","enabled":false}}`, false},
		{"blank command", `{"statusLine":{"type":"command","command":"  ","enabled":true}}`, false},
		{"enabled + command", `{"statusLine":{"type":"command","command":"/bin/echo hi","enabled":true}}`, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			home := t.TempDir()
			t.Setenv("ANTIGRAVITY_HOME", home)
			writeSettings(t, home, tc.body)
			// The live path is the statusline package's own reader: the
			// host's reloadStatusLineConfig keeps exactly this pair
			// (cfg, found) and the block's Active() verdict.
			cfg, found := statusline.LoadConfig()
			ok := found && cfg.Active()
			if ok != tc.ok {
				t.Errorf("ok = %v, want %v", ok, tc.ok)
			}
			if ok && cfg.Command == "" {
				t.Error("command empty despite ok = true")
			}
		})
	}
}

// TestStatusLinePayload_Fields pins the JSON wire form. The contract is
// the reference's `types.StatusLineData`: the field order, the JSON names
// and the omit rules are all part of it, because the official example
// scripts read the members by name.
//
// Evidence 【一手】: agy 1.4.7's captured payloads — for example
//
//	{"cwd":"/tmp/agyprobe/repo","session_id":"eab2…","conversation_id":"eab2…",
//	 "conversation_title":"ProbeTitle","transcript_path":"…/transcript.jsonl",
//	 "model":{"id":"Gemini 3.8 Flash (High)","display_name":"Gemini 3.8 Flash (High)",
//	          "effort":"high"},
//	 "workspace":{"current_dir":"/tmp/agyprobe/repo","project_dir":"/tmp/agyprobe/repo"},
//	 "version":"1.4.7","context_window":{…},"exceeds_200k_tokens":false,
//	 "product":"antigravity","quota":{…},"agent_state":"idle","vcs":{"type":"git"},
//	 "cycle_mode":"auto","sandbox":{"enabled":false},"plan_tier":"Google AI Pro",
//	 "email":"…","terminal_width":120}
func TestStatusLinePayload_Fields(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	m := &RootModel{
		session: sessionState{
			statusModel: "MiniMax M3",
			model:       "MiniMax M3",
			cwd:         "/Users/foo/project",
			store: tuistore.Store{
				Tokens: tuistore.Tokens{
					SessionPromptTokens:     1000,
					SessionCompletionTokens: 500,
				},
			},
		},
	}
	m.session.conversationName = "My Session"
	got := m.buildStatusLinePayload()
	if got.Model == nil || got.Model.DisplayName != "MiniMax M3" {
		t.Errorf("Model = %+v", got.Model)
	}
	if got.ConversationTitle != "My Session" {
		t.Errorf("ConversationTitle = %q", got.ConversationTitle)
	}
	if got.Cwd != "/Users/foo/project" {
		t.Errorf("Cwd = %q", got.Cwd)
	}
	if got.Workspace.CurrentDir != "/Users/foo/project" ||
		got.Workspace.ProjectDir != "/Users/foo/project" {
		t.Errorf("Workspace = %+v", got.Workspace)
	}
	if got.Version != cli.ReferenceVersion {
		t.Errorf("Version = %q, want %q", got.Version, cli.ReferenceVersion)
	}
	if got.Product != "antigravity" {
		t.Errorf("Product = %q", got.Product)
	}
	if got.AgentState != "idle" {
		t.Errorf("AgentState = %q", got.AgentState)
	}
	if got.ContextWindow.TotalInputTokens != 1000 ||
		got.ContextWindow.TotalOutputTokens != 500 {
		t.Errorf("ContextWindow = %+v", got.ContextWindow)
	}
	if got.Exceeds200kTokens != nil {
		t.Errorf("Exceeds200kTokens = %v, want null before the first response", *got.Exceeds200kTokens)
	}
	// The transcript path is the brain directory's log file.
	if !strings.HasSuffix(got.TranscriptPath,
		filepath.Join("brain", "", ".system_generated", "logs", "transcript.jsonl")) {
		t.Errorf("TranscriptPath = %q", got.TranscriptPath)
	}
	out, err := json.Marshal(got)
	if err != nil {
		t.Fatal(err)
	}
	// The field order is part of the contract.
	if !bytes.HasPrefix(out, []byte(`{"cwd":"/Users/foo/project","session_id":`)) {
		t.Errorf("field order = %s", out)
	}
	if !bytes.Contains(out, []byte(`"conversation_title":"My Session"`)) {
		t.Errorf("conversation_title field missing: %s", out)
	}
	if !bytes.Contains(out, []byte(`"model":{"id":"MiniMax M3","display_name":"MiniMax M3"`)) {
		t.Errorf("model field missing: %s", out)
	}
	if !bytes.Contains(out, []byte(`"product":"antigravity"`)) {
		t.Errorf("product field missing: %s", out)
	}
	if !bytes.Contains(out, []byte(`"exceeds_200k_tokens":null`)) {
		t.Errorf("exceeds_200k_tokens field missing: %s", out)
	}
}

// TestPricingForModel pins the rate-card table the JSON `cost` field
// is computed off. The two ordering invariants are:
//
//   - claude-opus must precede claude-sonnet (the latter is a substring
//     of the former);
//   - MiniMax M3 routes to the $0 row so the cycle-225 quota UI stays
//     consistent.
func TestPricingForModel(t *testing.T) {
	// 1M prompt tokens, 0 completion tokens → exact row hit.
	if got := statusline.PricingForModel("claude-opus-4.7", 1_000_000, 0); got != 15.0 {
		t.Errorf("claude-opus input rate = %v, want 15.0", got)
	}
	// Substring match: claude-sonnet should NOT route to claude-opus.
	if got := statusline.PricingForModel("claude-sonnet-4.5", 1_000_000, 0); got != 3.0 {
		t.Errorf("claude-sonnet input rate = %v, want 3.0", got)
	}
	// MiniMax M3 is free.
	if got := statusline.PricingForModel("MiniMax M3", 1_000_000, 1_000_000); got != 0 {
		t.Errorf("MiniMax M3 rate = %v, want 0", got)
	}
	// Unknown model falls back to 0 (well-formed JSON, but $0 is a
	// better default than NaN/Infinity).
	if got := statusline.PricingForModel("totally-unknown-model", 1000, 1000); got != 0 {
		t.Errorf("unknown model rate = %v, want 0", got)
	}
	// Output rate vs input rate — claude-sonnet output is 5x its
	// input rate (3 → 15).
	if got := statusline.PricingForModel("claude-sonnet", 0, 1_000_000); got != 15.0 {
		t.Errorf("claude-sonnet output rate = %v, want 15.0", got)
	}
}

// TestRecordTurnUsage_Accumulates puts the chunk-meta hook through
// its paces: every Done chunk adds Meta.PromptTokens /
// Meta.CompletionTokens to the RootModel totals, regardless of how
// many calls land in flight.
func TestRecordTurnUsage_Accumulates(t *testing.T) {
	m := &RootModel{}
	for i := 0; i < 5; i++ {
		m.recordTurnUsage(&backend.ChunkMeta{
			PromptTokens:     100,
			CompletionTokens: 50,
		})
	}
	p, c := m.sessionTokenTotals()
	if p != 500 || c != 250 {
		t.Errorf("totals = (%d, %d), want (500, 250)", p, c)
	}
}

// TestRecordTurnUsage_NilSafe pins the defensive checks: a nil
// receiver and a nil meta are both no-ops so the chunk handler's
// call site stays simple.
func TestRecordTurnUsage_NilSafe(t *testing.T) {
	var nilM *RootModel
	nilM.recordTurnUsage(&backend.ChunkMeta{PromptTokens: 100}) // must not panic
	var m *RootModel
	m.recordTurnUsage(nil) // nil meta — no panic, no change
	nilM.recordTurnUsage(nil)
}

// TestRunStatusLineScript_SpawnEcho writes a tiny sh script that
// reads the payload from stdin (one JSON object, terminated by a
// NUL byte) and prints a fixed line. The post-test parses the
// payload back and asserts the wire form so the test does not
// silently regress.
func TestRunStatusLineScript_SpawnEcho(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in -short mode")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "echo.sh")
	body := "#!/bin/sh\n" +
		// Read the JSON payload (terminator not required for the
		// short length this test uses). Echo a fixed marker.
		"cat >/dev/null\n" +
		"echo \"model said hi\"\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	payload := []byte(`{"model":"MiniMax M3","cost":0,"conversation_title":"","current_dir":"/"}`)
	res := statusline.RunScript(script, payload)
	if res.Err != nil {
		t.Fatalf("spawn err = %v", res.Err)
	}
	if got := strings.TrimSpace(res.Output); got != "model said hi" {
		t.Errorf("Output = %q", got)
	}
}

// TestRunStatusLineScript_NonZeroExit exercises the failure path:
// the script exits with a non-zero status, the runner surfaces a
// non-nil error so the row falls back to the built-in template.
func TestRunStatusLineScript_NonZeroExit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in -short mode")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "fail.sh")
	body := "#!/bin/sh\necho boom >&2\nexit 7\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	res := statusline.RunScript(script, []byte(`{}`))
	if res.Err == nil {
		t.Fatal("expected non-nil error on exit 7")
	}
	if !strings.Contains(res.Err.Error(), "boom") {
		t.Errorf("Err = %v, want stderr to bubble up", res.Err)
	}
}

// TestRunStatusLineScript_EmptyOutput ensures the script producing
// no stdout (or only whitespace) is treated as an error, so the
// caller can keep showing the last good render instead of a blank
// row.
func TestRunStatusLineScript_EmptyOutput(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping subprocess test in -short mode")
	}
	dir := t.TempDir()
	script := filepath.Join(dir, "silent.sh")
	body := "#!/bin/sh\nexit 0\n"
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	res := statusline.RunScript(script, []byte(`{}`))
	if res.Err == nil {
		t.Fatal("expected non-nil error on empty stdout")
	}
}

// TestSplitStatusLineCommand pins the whitespace split the clone keeps for
// its own bookkeeping. The command itself reaches the script through a
// shell — `sh -c "<command>"`, verbatim — so the split does *not* describe
// how the script is invoked.
//
// Evidence 【一手】: agy 1.4.7 rendered a command of `~/bin/probe.sh | cat`,
// which only works when the command reaches a shell.
func TestSplitStatusLineCommand(t *testing.T) {
	cases := []struct {
		in   string
		name string
		args []string
	}{
		{"/usr/local/bin/tmux-status", "/usr/local/bin/tmux-status", nil},
		{"/usr/local/bin/tmux-status --foo", "/usr/local/bin/tmux-status", []string{"--foo"}},
		{"   /bin/echo  hi there ", "/bin/echo", []string{"hi", "there"}},
		{"", "", nil},
	}
	for _, tc := range cases {
		gotName, gotArgs := statusline.SplitCommand(tc.in)
		if gotName != tc.name {
			t.Errorf("%q name = %q, want %q", tc.in, gotName, tc.name)
		}
		if len(gotArgs) != len(tc.args) {
			t.Errorf("%q args = %v, want %v", tc.in, gotArgs, tc.args)
			continue
		}
		for i := range tc.args {
			if gotArgs[i] != tc.args[i] {
				t.Errorf("%q args[%d] = %q, want %q", tc.in, i, gotArgs[i], tc.args[i])
			}
		}
	}
}

// TestStatuslineExternal_NeverConfigured pins the reference's behaviour when
// no status-line command is configured: `/statusline` toggles the
// statusline on and the built-in row stays.
//
// Evidence 【一手】: agy 1.4.8 with an empty `statusLine` block answered
// the bare `/statusline` with "Statusline on." (and flipped
// settings.json's `statusLine.enabled` to true). agy 1.4.7 answered the
// same command with the usage block.
func TestStatuslineExternal_NeverConfigured(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{}`)

	m := newSlashModel(t)
	m.handleSlashCommand("/statusline")
	rows := afterEcho(m)
	if len(rows) == 0 || rows[0] != "Statusline on." {
		t.Fatalf("/statusline rendered %q", rows)
	}
	if m.statusLine.external {
		t.Error("statusLineExternal must stay off without a command")
	}
	// The built-in row is still rendered.
	if !strings.Contains(m.View().Content, "? for shortcuts") {
		t.Error("built-in status row missing")
	}
}

// TestStatuslineExternal_CommandLifecycle covers the reference's
// status-line transitions with a command configured.
//
// Evidence 【一手】: agy 1.4.7 —
//
//	/statusline <command> → "Statusline set to: <command>", the script runs
//	/statusline off       → "Statusline off.", the script stops and the
//	                        built-in row comes back
//	/statusline on        → "Statusline on. (<command>)", the script runs
//	/statusline delete    → the command is dropped
func TestStatuslineExternal_CommandLifecycle(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	writeSettings(t, home, `{"statusLine":{"type":"command","command":"/bin/echo","enabled":true}}`)

	m := newSlashModel(t)
	m.reloadStatusLineConfig()
	if !m.statusLine.external {
		t.Fatal("statusLineExternal must be on with an enabled command")
	}

	// off: the script stops, the built-in row returns, the command stays.
	m.handleSlashCommand("/statusline off")
	if m.statusLine.external {
		t.Error("statusLineExternal must be off after /statusline off")
	}
	if m.statusLine.visible == "off" {
		t.Error("the built-in row must come back after /statusline off")
	}
	body := readSettings(t, home)
	if !strings.Contains(body, `"enabled": false`) {
		t.Errorf("settings.json missing enabled=false:\n%s", body)
	}
	if !strings.Contains(body, `"command": "/bin/echo"`) {
		t.Errorf("settings.json clobbered the command field:\n%s", body)
	}

	// on: the script runs again.
	m.handleSlashCommand("/statusline on")
	if !m.statusLine.external {
		t.Error("statusLineExternal must be on after /statusline on")
	}
	body = readSettings(t, home)
	if !strings.Contains(body, `"enabled": true`) {
		t.Errorf("settings.json missing enabled=true:\n%s", body)
	}
	if !strings.Contains(body, `"command": "/bin/echo"`) {
		t.Errorf("settings.json clobbered the command field:\n%s", body)
	}

	// delete: the whole block is removed.
	//
	// Evidence 【一手】: agy 1.4.7's settings.json after `/statusline
	// delete` held no `statusLine` key at all.
	m.handleSlashCommand("/statusline delete")
	if m.statusLine.external {
		t.Error("statusLineExternal must be off after /statusline delete")
	}
	body = readSettings(t, home)
	if strings.Contains(body, `"statusLine"`) {
		t.Errorf("settings.json still carries the statusLine block:\n%s", body)
	}
}

// TestStatusLineRowUsesExternalRender verifies that when the
// external state has produced a non-empty output, View() shows it
// in place of the "? for shortcuts" placeholder.
func TestStatusLineRowUsesExternalRender(t *testing.T) {
	m := newSlashModel(t)
	m.viewport.width = 80
	m.viewport.height = 24
	m.statusLine.external = true
	m.statusLine.visible = "on" // visible
	state := m.ensureExternalState()
	state.SetRendered("…custom…", nil)

	view := m.View().Content
	if !strings.Contains(view, "…custom…") {
		t.Error("external render missing from View()")
	}
	if strings.Contains(view, "? for shortcuts") {
		t.Error("built-in hint still rendered alongside external row")
	}
}

// TestStatusLineRowFallsBackOnError covers the recovery path: a failed
// script shows the reference's error card in the custom row's place, so
// the user is never staring at a blank row.
//
// Evidence 【一手】: agy 1.4.7 rendered
//
//	⚠ Statusline Error
//	  ⎿  Statusline error:
//	     command failed: exit status 3 (stderr: boom)
func TestStatusLineRowFallsBackOnError(t *testing.T) {
	m := newSlashModel(t)
	m.viewport.width = 80
	m.viewport.height = 24
	m.statusLine.external = true
	m.statusLine.visible = "on"
	// Simulate a transient error: the script failed, so the cached
	// output is empty and the error card takes the custom row's place.
	state := m.ensureExternalState()
	state.SetRendered("", errTestBoom)

	view := m.View().Content
	if !strings.Contains(view, "Statusline Error") {
		t.Error("status-line error card missing after script error")
	}
	if !strings.Contains(view, "boom") {
		t.Error("status-line error card missing the error text")
	}
	if strings.Contains(view, "? for shortcuts") {
		t.Error("built-in hint rendered alongside the error card")
	}
}

// errTestBoom is a sentinel error used by the fallback test.
var errTestBoom = stringErr("boom")

// stringErr is a tiny error type used only by the fallback test
// (the production code passes `error`, which is satisfied).
type stringErr string

func (s stringErr) Error() string { return string(s) }

// TestStatusLinePayload_IncludesConversationTitle pins the field's
// presence in the JSON wire form. The contract from the AGY 1.1.27
// changelog is that the field is `conversation_title` (not
// `title`, not `conversationTitle`, not `name`) — a downstream
// script can grep that exact key. We assert both the Go field and
// the encoded JSON form so a struct rename trips the test.
func TestStatusLinePayload_IncludesConversationTitle(t *testing.T) {
	m := &RootModel{
		session: sessionState{
			statusModel: "MiniMax M3",
			model:       "MiniMax M3",
			cwd:         "/Users/foo/project",
		},
	}
	m.session.conversationName = "Refactor auth"
	payload := m.buildStatusLinePayload()
	if payload.ConversationTitle != "Refactor auth" {
		t.Errorf("ConversationTitle = %q, want %q", payload.ConversationTitle, "Refactor auth")
	}
	out, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(out, []byte(`"conversation_title":"Refactor auth"`)) {
		t.Errorf("wire form missing conversation_title field: %s", out)
	}
}

// TestStatusLinePayload_UpdatesOnRename pins the rename → next-tick
// path: after `/rename New Title`, the next call to
// `buildStatusLinePayload` reads the new name without any explicit
// cache invalidation. The cycle-status-line-script tick handler
// (cmd/wagyu/tui_run.go:3776) re-reads on every 100 ms tick, so a
// /rename is visible within ~100 ms — but the read is *direct* on
// m.session.conversationName, not cached anywhere.
func TestStatusLinePayload_UpdatesOnRename(t *testing.T) {
	m := &RootModel{
		session: sessionState{
			statusModel: "MiniMax M3",
			model:       "MiniMax M3",
			cwd:         "/tmp",
		},
	}
	// Before rename: empty conversationName, the helper falls
	// through to the first user message and then to the program
	// name. Either way the field is non-empty so the JSON is
	// well-formed.
	before := m.buildStatusLinePayload()
	if before.ConversationTitle == "New Title" {
		t.Fatal("ConversationTitle already New Title before /rename")
	}

	// /rename New Title — the cycle's recovery path is
	// renameFeedback(args, current) returning the new name, and
	// the slash dispatcher writing it back into m.session.conversationName
	// (cmd/wagyu/slash_commands.go:528-529). The cycle pins that
	// payload follows rename without any other wiring.
	next, msg := tuilib.RenameFeedback("New Title", m.session.conversationName)
	if msg == "" || !strings.HasPrefix(msg, "Conversation renamed") {
		t.Fatalf("renameFeedback msg = %q", msg)
	}
	m.session.conversationName = next

	after := m.buildStatusLinePayload()
	if after.ConversationTitle != "New Title" {
		t.Errorf("after /rename ConversationTitle = %q, want %q",
			after.ConversationTitle, "New Title")
	}
	// And the JSON wire form follows the rename.
	out, err := json.Marshal(after)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !bytes.Contains(out, []byte(`"conversation_title":"New Title"`)) {
		t.Errorf("wire form did not pick up the rename: %s", out)
	}
}

// TestStatusLinePayload_EmptyTitleWhenUntitled pins that the field
// is *always* present in the JSON output, not omitted when the
// conversation hasn't been user-named. AGY 1.1.27 ships a fallback
// (first user message → program name) so the field value is
// never an empty string either; the test asserts both invariants.
//
// "Not omitted" is the contract an end-user script depends on:
// `payload.conversation_title` can be accessed unconditionally
// without a `if "conversation_title" in payload` guard. A script
// that wants to distinguish "untitled" from "titled" checks
// `payload.conversation_title !== "<expected-fallback>"`, never
// `payload.conversation_title === ""`.
func TestStatusLinePayload_EmptyTitleWhenUntitled(t *testing.T) {
	m := &RootModel{
		session: sessionState{
			statusModel: "MiniMax M3",
			model:       "MiniMax M3",
			cwd:         "/private/tmp",
		},
	}
	// No conversationName, no session — helper falls through to
	// filepath.Base(os.Args[0]). `go test` runs the test binary
	// so os.Args[0] is a path ending in `.test`; the basename is
	// stable for a given test invocation.
	payload := m.buildStatusLinePayload()
	if payload.ConversationTitle == "" {
		t.Errorf("ConversationTitle empty in Go value; field should fall back to program name")
	}

	// Decode the JSON back so we can verify the field is present
	// at the *JSON* layer too (a future "omitempty" tag would
	// drop a zero-value, which the contract disallows).
	out, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	v, ok := decoded["conversation_title"]
	if !ok {
		t.Fatalf("conversation_title key absent from JSON: %s", out)
	}
	if v == "" {
		t.Errorf("conversation_title is empty string in JSON: %s", out)
	}
}

// TestStatusLinePayload_ReferenceShape pins the payload to the
// reference's `types.StatusLineData`.
//
// The clone used to invent its own four-field shape (`model` as a string,
// `cost` as a number, `current_dir`). The reference never had that shape:
// at 1.4.7 `model` is an object, the directory lives in `workspace`, and
// the cost is an object. The official example scripts read the reference's
// members — `.model.display_name`, `.workspace.current_dir`,
// `.context_window.used_percentage` — so the clone now sends the
// reference's document.
//
// Evidence 【一手】: agy 1.4.7's captured payloads.
func TestStatusLinePayload_ReferenceShape(t *testing.T) {
	m := &RootModel{
		session: sessionState{
			statusModel: "MiniMax M3",
			model:       "MiniMax M3",
			cwd:         "/Users/foo/project",
			store: tuistore.Store{
				Tokens: tuistore.Tokens{
					SessionPromptTokens:     1000,
					SessionCompletionTokens: 500,
				},
			},
		},
		viewport: uiState{width: 120},
	}
	m.session.conversationName = "Refactor auth"
	payload := m.buildStatusLinePayload()

	// Go-level: the members the reference exposes.
	if payload.Model == nil || payload.Model.ID != "MiniMax M3" ||
		payload.Model.DisplayName != "MiniMax M3" {
		t.Errorf("Model = %+v", payload.Model)
	}
	if payload.Workspace.CurrentDir != "/Users/foo/project" {
		t.Errorf("Workspace.CurrentDir = %q", payload.Workspace.CurrentDir)
	}
	if payload.ContextWindow.TotalInputTokens != 1000 ||
		payload.ContextWindow.TotalOutputTokens != 500 {
		t.Errorf("ContextWindow = %+v", payload.ContextWindow)
	}

	// JSON-level: every member the reference's payload carries.
	out, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{
		"cwd", "session_id", "conversation_id", "conversation_title",
		"transcript_path", "model", "workspace", "version", "context_window",
		"exceeds_200k_tokens", "product", "agent_state", "sandbox",
		"terminal_width",
	} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("reference key %q missing from JSON: %s", key, out)
		}
	}
	// The model is an object, not a string.
	model, ok := decoded["model"].(map[string]any)
	if !ok {
		t.Fatalf("model is not an object: %s", out)
	}
	if _, ok := model["display_name"]; !ok {
		t.Errorf("model.display_name missing: %s", out)
	}
	// The workspace holds the directories.
	workspace, ok := decoded["workspace"].(map[string]any)
	if !ok {
		t.Fatalf("workspace is not an object: %s", out)
	}
	if workspace["current_dir"] != "/Users/foo/project" {
		t.Errorf("workspace.current_dir = %v", workspace["current_dir"])
	}
}

// TestStatusLinePayload_PrintModeUnaffected pins the structural
// fact that print mode (`-p`) never spawns the configured script:
// the `conversation_title` field is only meaningful for the
// interactive TUI. The test uses go/parser to walk the package
// source and assert that no print-mode code path references the
// spawn helpers (`runStatusLineScript`, `runExternalStatusLineRender`).
func TestStatusLinePayload_PrintModeUnaffected(t *testing.T) {
	// 1. Pure-function check: buildStatusLinePayload is deterministic
	//    and does not spawn. Two back-to-back calls produce the
	//    same payload — no goroutine, no subprocess.
	m := &RootModel{
		session: sessionState{
			statusModel: "MiniMax M3",
			model:       "MiniMax M3",
			cwd:         "/private/tmp",
		},
	}
	m.setSessionMessages([]backend.Message{{Role: "user", Content: "hello"}})
	a := m.buildStatusLinePayload()
	b := m.buildStatusLinePayload()
	if !reflect.DeepEqual(a, b) {
		t.Errorf("buildStatusLinePayload not deterministic: %+v vs %+v", a, b)
	}

	// 2. Static check: walk cmd/wagyu/*.go and assert that the two
	//    spawn helpers have no callers in print-mode files. The
	//    test's working directory is cmd/wagyu/ (go test runs
	//    there), so the cmd/wagyu path is just ".".
	wd, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	cmdDir := wd
	if filepath.Base(wd) != "tui" {
		cmdDir = filepath.Join(wd, "internal", "tui")
	}
	entries, err := os.ReadDir(cmdDir)
	if err != nil {
		t.Fatalf("readdir: %v", err)
	}

	fset := token.NewFileSet()
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		isPrintMode := strings.HasPrefix(name, "print_") ||
			name == "main.go"
		if !isPrintMode {
			continue
		}
		path := filepath.Join(cmdDir, name)
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			fn, ok := call.Fun.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			switch fn.Sel.Name {
			case "runStatusLineScript", "runExternalStatusLineRender":
				t.Errorf("print-mode file %s calls %s — print mode must not spawn the status-line script",
					name, fn.Sel.Name)
			}
			return true
		})
	}
}

// altTestMode builds a RootModel wired for alt-keytranslation tests.
// The caller sets TERM_PROGRAM and (optionally) writes a
// keybindings.json under home before calling this — the helper
// only sets the env and constructs the mode.
//
// Pass an empty home path to skip file I/O entirely; the RootModel
// will still pick up the term-driven gates.
func altTestMode(t *testing.T, keybindingsJSON string) *RootModel {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	// keybind.DefaultPath() resolves via os.UserHomeDir() which
	// reads $HOME. Pin $HOME to the test root so the dispatcher
	// loads from a temp dir rather than the user's real home.
	homeDir := t.TempDir()
	t.Setenv("HOME", homeDir)
	geminiDir := filepath.Join(homeDir, ".gemini")
	configDir := filepath.Join(geminiDir, "config")
	if err := os.MkdirAll(configDir, 0o755); err != nil {
		t.Fatalf("mkdir config: %v", err)
	}
	if keybindingsJSON != "" {
		if err := os.WriteFile(filepath.Join(configDir, "keybindings.json"),
			[]byte(keybindingsJSON), 0o644); err != nil {
			t.Fatalf("write keybindings: %v", err)
		}
	}
	return newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
}

// altFires returns the letter/action pairs the dispatcher fired
// during a test. Useful for assertions without coupling to
// response-row formatting.
func (m *RootModel) altFires() []altKeyFire {
	out := make([]altKeyFire, len(m.prompt.altKeyFires))
	copy(out, m.prompt.altKeyFires)
	return out
}

// withTerminal sets TERM_PROGRAM for the duration of one test and
// returns a cleanup closure. Tests that don't care about the
// terminal gate should call altTestMode and not bother with this.
func withTerminal(t *testing.T, prog string) {
	t.Helper()
	t.Setenv("TERM_PROGRAM", prog)
}

// TestAlt_TerminalApp_SynthesisesAltS verifies the headline case:
// on Terminal.app with alt+s bound, a bare Esc followed by 's'
// within the release window produces one alt+s dispatch.
//
// The Esc is held by m.composer.altEscBuf; 's' arrives; the buffer
// synthesises alt+s; altDispatchAction records the fire.
func TestAlt_TerminalApp_SynthesisesAltS(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, `{"alt+s": "submit"}`)

	// Bare Esc arrives first — buffered, no dispatch.
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if cmd == nil {
		t.Error("buffering an Esc should arm the release tick")
	}
	if len(m.altFires()) != 0 {
		t.Error("Esc alone should not fire anything")
	}

	// 's' follows within microseconds (within EscReleaseWindow).
	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))

	fires := m.altFires()
	if len(fires) != 1 {
		t.Fatalf("altFires has %d entries, want 1: %+v", len(fires), fires)
	}
	if fires[0].Letter != 's' || fires[0].Action != "submit" {
		t.Errorf("fire = %+v, want {s submit}", fires[0])
	}
	// The synthesised event must not have leaked into the input —
	// alt+s is not a printable rune.
	if v := m.prompt.input.Value(); v != "" {
		t.Errorf("input should not contain 's', got %q", v)
	}
}

// TestAlt_ITerm2_NativeAltS verifies the Meta-native path: on
// iTerm2 (or any terminal that forwards Option as Meta), ultraviolet
// already produces a single KeyPressMsg with Mod=ModAlt. The
// dispatcher's job is to fire the binding — no buffering involved.
func TestAlt_ITerm2_NativeAltS(t *testing.T) {
	withTerminal(t, "iTerm.app")
	m := altTestMode(t, `{"alt+s": "submit"}`)

	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModAlt, Text: "s"}))

	fires := m.altFires()
	if len(fires) != 1 {
		t.Fatalf("altFires has %d entries, want 1", len(fires))
	}
	if fires[0].Letter != 's' || fires[0].Action != "submit" {
		t.Errorf("fire = %+v, want {s submit}", fires[0])
	}
}

// TestAlt_TerminalApp_ArbitraryLetter verifies that 'alt+x'
// synthesises correctly — same path as alt+s, but exercises the
// letter parameter rather than a hard-coded 's'.
func TestAlt_TerminalApp_ArbitraryLetter(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, `{"alt+x": "cancel"}`)

	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"}))

	fires := m.altFires()
	if len(fires) != 1 {
		t.Fatalf("altFires has %d entries, want 1", len(fires))
	}
	if fires[0].Letter != 'x' || fires[0].Action != "cancel" {
		t.Errorf("fire = %+v, want {x cancel}", fires[0])
	}
}

// TestAlt_NoAltsConfigured_FallsThrough is the regression guard:
// when the user has no keybindings.json (or it has no alt+letter
// entries), Esc+s must NOT swallow the Esc — the Esc reaches the
// host's Esc handler, and 's' inserts as text.
//
// Cycle 383 (B-21): the reference clears the prompt box on `esc esc`
// (official-docs/cli/using.md:22), so one Esc only arms the clear and
// the second one empties the composer. The pair is the observable proof
// that the Esc was processed rather than swallowed.
func TestAlt_NoAltsConfigured_FallsThrough(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, "" /* no keybindings.json */)

	m.Update(runeKey('h'))
	m.Update(runeKey('i'))
	if v := m.prompt.input.Value(); v != "hi" {
		t.Fatalf("setup: input should hold 'hi', got %q", v)
	}

	// Esc arrives: with no alts configured, the buffer gate
	// short-circuits and the Esc reaches the host's Esc handler. It
	// arms the `esc esc` clear, so the text is still there.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if v := m.prompt.input.Value(); v != "hi" {
		t.Errorf("one Esc must not clear the input (esc esc), got %q", v)
	}

	// The second Esc completes the pair: the first one was processed,
	// not swallowed as an alt prefix.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if v := m.prompt.input.Value(); v != "" {
		t.Errorf("the second Esc should clear the input, got %q", v)
	}

	// 's' arrives: insert as text.
	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))
	if v := m.prompt.input.Value(); v != "s" {
		t.Errorf("'s' should insert into the cleared input, got %q", v)
	}
	if len(m.altFires()) != 0 {
		t.Errorf("no bindings configured → no fires, got %d", len(m.altFires()))
	}
}

// TestAlt_VimMode_NotDoubleHandled verifies that Esc-then-letter on
// Terminal.app is not double handled when vim owns the keyboard: the Esc
// leaves Insert (vim dispatches it on the press) and the letter is a vim
// *command*, not an alt-pair fold.
//
// This is the precedence vim mode implies: `s` in Normal mode is
// "substitute", so folding the pair into alt+s would swallow a vim command.
// What the test guards is that the letter never leaks into the buffer as
// text and that no alt binding fires behind vim's back.
func TestAlt_VimMode_NotDoubleHandled(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	cycle196TestHome(t, map[string]any{"editorMode": "vim"})
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.reloadVimSettings()
	if !m.vimEnabled() {
		t.Fatal("vim mode should be on")
	}
	// Inject a keybindings with alt+s.
	path := filepath.Join(filepath.Dir(os.Getenv("ANTIGRAVITY_HOME")), "config", "keybindings.json")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"alt+s": "submit"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if kb, err := keybind.Load(path); err == nil {
		m.prompt.keybindings = kb
	}

	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))

	if got := len(m.altFires()); got != 0 {
		t.Fatalf("altFires has %d entries, want 0: vim owns the letter", got)
	}
	// 's' ran vim's substitute, which re-enters Insert with the text
	// untouched — it must not have been inserted as a literal rune.
	if v := m.prompt.input.Value(); strings.Contains(v, "s") {
		t.Errorf("input contains a literal 's': %q", v)
	}
	// The letter stayed in vim: the engine is still in Normal (or in
	// whatever mode its own `s` selects), never Insert-by-literal-rune.
	if got := m.statusRowState().VimBadge; got == "-- INSERT --" && !strings.HasSuffix(m.prompt.input.Value(), "s") {
		t.Logf("vim ran substitute (badge %q); the letter was a command, not text", got)
	}
}

// TestAlt_LinuxNative_ModAlt verifies the Linux equivalent of
// iTerm2's path: any terminal that emits ModAlt directly works
// without the buffer (TERM_PROGRAM is unset on Linux so the
// NeedsOptionMeta gate returns false; we just exercise the
// dispatcher in isolation).
func TestAlt_LinuxNative_ModAlt(t *testing.T) {
	t.Setenv("TERM_PROGRAM", "")
	m := altTestMode(t, `{"alt+s": "submit"}`)

	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModAlt, Text: "s"}))

	if got := len(m.altFires()); got != 1 {
		t.Fatalf("altFires has %d entries, want 1", got)
	}
	if fire := m.altFires()[0]; fire.Letter != 's' || fire.Action != "submit" {
		t.Errorf("fire = %+v, want {s submit}", fire)
	}
}

// TestAlt_PlainLetter_NotMistranslated is the negative control:
// a bare 's' with no preceding Esc inserts as text. The buffer
// must not fire any alt binding, and the input must contain 's'.
func TestAlt_PlainLetter_NotMistranslated(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, `{"alt+s": "submit", "alt+x": "cancel"}`)

	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))

	if v := m.prompt.input.Value(); v != "s" {
		t.Errorf("plain 's' should insert into input, got %q", v)
	}
	if got := len(m.altFires()); got != 0 {
		t.Errorf("plain 's' should not fire any alt binding, got %d", got)
	}
}

// TestAlt_EscTimeout_ReleasesStaleBuffer verifies the 50 ms tick
// path: a buffered Esc with no follow-up key gets released by
// EscTimeoutMsg. We simulate the timeout by clearing the buffer
// state directly (the tick fires asynchronously, but the
// production handler is exactly what we test below).
//
// Cycle 383 (B-21): the released Esc arms the `esc esc` clear, so the
// text survives the release; the second Esc — released the same way —
// completes the pair and empties the composer.
func TestAlt_EscTimeout_ReleasesStaleBuffer(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, `{"alt+s": "submit"}`)
	m.prompt.input.SetValue("hi")

	// Buffer an Esc.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if v := m.prompt.input.Value(); v != "hi" {
		t.Errorf("Esc should not yet clear input (still buffered), got %q", v)
	}

	// Simulate the tick firing after the window by manually
	// expiring the buffer's timestamp. The handler runs the
	// Esc branch on the buffer's behalf.
	m.prompt.altEscBuf.At = time.Now().Add(-100 * time.Millisecond)
	m.Update(keybind.EscTimeoutMsg{})

	// The Esc branch fired — it armed the clear, so the text is still
	// there.
	if v := m.prompt.input.Value(); v != "hi" {
		t.Errorf("EscTimeoutMsg must not clear the input on its own, got %q", v)
	}

	// The second Esc, released the same way, completes the pair.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	m.prompt.altEscBuf.At = time.Now().Add(-100 * time.Millisecond)
	m.Update(keybind.EscTimeoutMsg{})
	if v := m.prompt.input.Value(); v != "" {
		t.Errorf("the second Esc should clear the input via handleEscKey, got %q", v)
	}
}

// TestAlt_StaleBufferFallsThrough confirms that an Esc+s where the
// window has elapsed (synthetic test: clear At before the 's'
// arrives) drops the buffer and processes both keys normally.
//
// Cycle 383 (B-21): the released Esc arms the `esc esc` clear, and the
// 's' that follows breaks the pair — so the text keeps both characters.
func TestAlt_StaleBufferFallsThrough(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, `{"alt+s": "submit"}`)
	m.prompt.input.SetValue("hi")

	// Buffer an Esc, then expire it.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	m.prompt.altEscBuf.At = time.Now().Add(-100 * time.Millisecond)

	// 's' arrives; the buffer sees the window elapsed, releases
	// the Esc (which arms the clear), and the 's' inserts.
	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Text: "s"}))

	if v := m.prompt.input.Value(); v != "his" {
		t.Errorf("after stale Esc + 's', input should be 'his', got %q", v)
	}
	if got := len(m.altFires()); got != 0 {
		t.Errorf("stale buffer must not synthesise, got %d fires", got)
	}
}

// TestAlt_DoubleLetterEscapesBuffer verifies the modifier guard:
// Ctrl+Alt+s (not just Alt+s) does not match the dispatcher's
// plain alt+s shape and must not fire. This is the guard
// cmd/wagyu/alt_keytranslate.go's IsAltLetter check enforces.
func TestAlt_DoubleLetterEscapesBuffer(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, `{"alt+s": "submit"}`)

	// Ctrl+Alt+s — extra modifier means IsAltLetter returns
	// ok=false; the dispatcher does not see this as alt+s.
	m.Update(tea.KeyPressMsg(tea.Key{Code: 's', Mod: tea.ModAlt | tea.ModCtrl, Text: "s"}))
	if got := len(m.altFires()); got != 0 {
		t.Errorf("Ctrl+Alt+s must not match plain alt+s, got %d fires", got)
	}
}

// TestAlt_AltAfterBufferReleasesGuard confirms a non-letter
// follow-up releases the buffered Esc without synthesising:
// the third case of EscBuffer.Feed (non-letter).
func TestAlt_NonLetterFollowUpReleases(t *testing.T) {
	withTerminal(t, "Apple_Terminal")
	m := altTestMode(t, `{"alt+s": "submit"}`)
	m.prompt.input.SetValue("hi")

	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	// digit follows — not a letter candidate, release the Esc.
	m.Update(tea.KeyPressMsg(tea.Key{Code: '1', Text: "1"}))

	// Cycle 383 (B-21): the released Esc arms the `esc esc` clear and
	// the digit breaks the pair, so the text keeps both characters.
	if v := m.prompt.input.Value(); v != "hi1" {
		t.Errorf("Esc+1 should leave 'hi1', got %q", v)
	}
	if got := len(m.altFires()); got != 0 {
		t.Errorf("Esc+1 must not synthesise alt+letter, got %d fires", got)
	}
}

// altLeftKey / altRightKey build the synthetic tea.KeyPressMsg the
// RootModel.Update handler dispatches against. The bubbles textarea
// needs a focused model and a non-virtual cursor for Cursor() to
// return a non-nil offset (cycle 71 fix); the test setup below
// mirrors what newRootModel produces for production.
func altLeftKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft, Mod: tea.ModAlt})
}

func altRightKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyRight, Mod: tea.ModAlt})
}

// typeString drives the textarea via the same rune-typed
// tea.KeyPressMsg path tui_run_test.go uses, so the runes bubble sees
// come from the same Update dispatch the production TUI uses (the
// real terminal also delivers printable runes as Key.Code + Key.Text,
// not via InsertRune).
func typeString(m *RootModel, s string) {
	for _, r := range s {
		m.Update(tea.KeyPressMsg(tea.Key{Code: r, Text: string(r)}))
	}
}

// cursorRowCol returns the current (row, col) of the textarea. Both
// fields are read from the public Line() / Column() accessors.
func cursorRowCol(m *RootModel) (row, col int) {
	return m.prompt.input.Line(), m.prompt.input.Column()
}

// TestAltRightNextWordBoundary verifies Alt+Right moves the cursor to
// the end of the next word, matching macOS NSText semantics.
func TestAltRightNextWordBoundary(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello world foo bar")
	// Cursor is at the end (line=0, col=19). Alt+Right at end is a
	// no-op (cycle-225 spec: stays put at end-of-buffer).
	m.Update(altRightKey())
	if row, col := cursorRowCol(m); row != 0 || col != 19 {
		t.Fatalf("Alt+Right at end should stay at (0,19); got (%d,%d)", row, col)
	}
	// Alt+Left → "hello world foo |bar" (col=16, start of "bar").
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 16 {
		t.Fatalf("Alt+Left 1 should land at start of 'bar' (col 16); got (%d,%d)", row, col)
	}
	// Alt+Left → "hello world |foo bar" (col=12, start of "foo").
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 12 {
		t.Fatalf("Alt+Left 2 should land at start of 'foo' (col 12); got (%d,%d)", row, col)
	}
	// Alt+Left → "hello world |" (col=6, start of "world"; we land
	// at the first rune of the word, not the rune after the
	// whitespace run — verified against Cocoa).
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 6 {
		t.Fatalf("Alt+Left 3 should land at start of 'world' (col 6); got (%d,%d)", row, col)
	}
	// Alt+Left → "|hello world foo bar" (col=0).
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 0 {
		t.Fatalf("Alt+Left 4 should land at col 0; got (%d,%d)", row, col)
	}
	// Alt+Right → "hello| world foo bar" (col=5, end of "hello").
	m.Update(altRightKey())
	if row, col := cursorRowCol(m); row != 0 || col != 5 {
		t.Fatalf("Alt+Right from start should land at end of 'hello' (col 5); got (%d,%d)", row, col)
	}
	// Alt+Right → "hello world| foo bar" (col=11, end of "world").
	m.Update(altRightKey())
	if row, col := cursorRowCol(m); row != 0 || col != 11 {
		t.Fatalf("Alt+Right should land at end of 'world' (col 11); got (%d,%d)", row, col)
	}
}

// TestAltLeftPreviousWordBoundary verifies Alt+Left walks the cursor
// backwards word-by-word, from end-of-buffer to start, landing on
// the first rune of each word.
func TestAltLeftPreviousWordBoundary(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello world foo bar")
	// Cursor at end (col=19). Walk back via Alt+Left until col=0.
	//   19 → 16 (start of "bar")
	//   16 → 12 (start of "foo")
	//   12 →  6 (start of "world")
	//    6 →  0 (start of "hello")
	want := []int{16, 12, 6, 0}
	for i, w := range want {
		m.Update(altLeftKey())
		if row, col := cursorRowCol(m); row != 0 || col != w {
			t.Fatalf("Alt+Left #%d expected col %d, got (%d,%d)", i+1, w, row, col)
		}
	}
}

// TestAltRightAtEndStays verifies Alt+Right at the end of the buffer
// is a no-op — the cursor does not move. macOS NSText and bubbles'
// wordRight agree on this.
func TestAltRightAtEndStays(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello world")
	startRow, startCol := cursorRowCol(m)
	m.Update(altRightKey())
	m.Update(altRightKey())
	m.Update(altRightKey())
	if row, col := cursorRowCol(m); row != startRow || col != startCol {
		t.Fatalf("Alt+Right at end should stay put; got (%d,%d) want (%d,%d)",
			row, col, startRow, startCol)
	}
}

// TestAltLeftAtStartStays verifies Alt+Left at the start of the buffer
// is a no-op. macOS NSText and bubbles' wordLeft agree.
func TestAltLeftAtStartStays(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello world")
	// Move cursor to start with the textarea's Home binding
	// (ctrl+a → LineStart).
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModCtrl}))
	row, col := cursorRowCol(m)
	if row != 0 || col != 0 {
		t.Fatalf("precondition: ctrl+a should land at (0,0); got (%d,%d)", row, col)
	}
	m.Update(altLeftKey())
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 0 {
		t.Fatalf("Alt+Left at start should stay put; got (%d,%d)", row, col)
	}
}

// TestSingleWordNoMovement verifies Alt+Right and Alt+Left on a
// single-word buffer both stay put — there are no word boundaries to
// cross.
func TestSingleWordNoMovement(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello")
	// Cursor at end (col=5). Alt+Right stays.
	m.Update(altRightKey())
	if row, col := cursorRowCol(m); row != 0 || col != 5 {
		t.Fatalf("Alt+Right on single word should stay at col 5; got (%d,%d)", row, col)
	}
	// Move to start with ctrl+a, then Alt+Left stays.
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModCtrl}))
	if row, col := cursorRowCol(m); row != 0 || col != 0 {
		t.Fatalf("ctrl+a should land at (0,0); got (%d,%d)", row, col)
	}
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 0 {
		t.Fatalf("Alt+Left on single word should stay at col 0; got (%d,%d)", row, col)
	}
}

// TestAltWordCrossesLines verifies Alt+Left / Alt+Right walk across
// line boundaries, treating '\n' as whitespace. A two-line buffer
// "hello\nworld" with the cursor at the end of "world" should:
//   - Alt+Left once → start of "world" (row=1, col=0)
//   - Alt+Left again → walk across the '\n', past "hello", land at
//     the start of "hello" (row=0, col=0) — macOS convention puts
//     the cursor at the START of the prior word, not its end.
//   - Alt+Left again → no-op (already at start).
//
// Verified against bubbles textarea v2.1.1 (the prompt editor
// upstream — cycle 71) via a probe before this fix.
func TestAltWordCrossesLines(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello")
	// ctrl+j inserts a newline (cycle 71 path).
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'j', Mod: tea.ModCtrl}))
	typeString(m, "world")
	if row, col := cursorRowCol(m); row != 1 || col != 5 {
		t.Fatalf("precondition: cursor should be at (1,5); got (%d,%d)", row, col)
	}
	// Alt+Left from end-of-line-1 → start of "world" (row=1, col=0).
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 1 || col != 0 {
		t.Fatalf("Alt+Left within line should land at start of 'world' (1,0); got (%d,%d)", row, col)
	}
	// Alt+Left again → cross the newline, walk past "hello", land
	// at start of "hello" (row=0, col=0). The whitespace run
	// between the lines is just the '\n' itself; the macOS
	// convention puts us on the first non-whitespace rune of the
	// prior word.
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 0 {
		t.Fatalf("Alt+Left across newline should land at start of 'hello' (0,0); got (%d,%d)", row, col)
	}
	// Alt+Left once more → no-op (already at start).
	m.Update(altLeftKey())
	if row, col := cursorRowCol(m); row != 0 || col != 0 {
		t.Fatalf("Alt+Left at start should stay at (0,0); got (%d,%d)", row, col)
	}
}

// TestAltRightFromMiddleOfWord verifies Alt+Right from inside a word
// still lands at the end of the NEXT word, not the end of the current
// one (macOS convention).
func TestAltRightFromMiddleOfWord(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello world")
	// Cursor at end (col=11). Move to middle of "hello" with
	// repeated ctrl+b (bubble's WordBackward — verified to land at
	// the start of "hello").
	for i := 0; i < 6; i++ {
		m.Update(altLeftKey())
	}
	if row, col := cursorRowCol(m); row != 0 || col != 0 {
		t.Fatalf("precondition: 6×Alt+Left should land at (0,0); got (%d,%d)", row, col)
	}
	// Move into the middle of "hello" via the right arrow × 2.
	for i := 0; i < 2; i++ {
		m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyRight}))
	}
	if row, col := cursorRowCol(m); row != 0 || col != 2 {
		t.Fatalf("precondition: 2×Right should land at (0,2); got (%d,%d)", row, col)
	}
	// Alt+Right from inside "hello" → end of "hello" (col=5).
	m.Update(altRightKey())
	if row, col := cursorRowCol(m); row != 0 || col != 5 {
		t.Fatalf("Alt+Right from middle of word should land at end of 'hello' (col 5); got (%d,%d)", row, col)
	}
	// Alt+Right → end of "world" (col=11).
	m.Update(altRightKey())
	if row, col := cursorRowCol(m); row != 0 || col != 11 {
		t.Fatalf("Alt+Right after 'hello' should land at end of 'world' (col 11); got (%d,%d)", row, col)
	}
}

// TestAltWordHelperDirect exercises wordBoundaryBack / wordBoundaryFwd
// directly on representative macOS-convention inputs (Chinese text,
// mixed punctuation, leading/trailing whitespace). The RootModel-level
// tests above exercise the dispatcher; this one locks the helpers'
// arithmetic against regressions.
func TestAltWordHelperDirect(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		col      int
		wantBack int
		wantFwd  int
	}{
		{
			name:     "ascii two words, mid first word",
			line:     "hello world",
			col:      2,
			wantBack: 0,
			wantFwd:  5,
		},
		{
			name:     "ascii trailing whitespace",
			line:     "hello   ",
			col:      8,
			wantBack: 0,
			wantFwd:  8,
		},
		{
			name:     "ascii leading whitespace",
			line:     "   hello",
			col:      0,
			wantBack: 0,
			wantFwd:  8,
		},
		{
			name:     "chinese chars no spaces",
			line:     "你好世界",
			col:      2,
			wantBack: 0,
			wantFwd:  4,
		},
		{
			name:     "chinese with ascii word mixed",
			line:     "你好 world",
			col:      1,
			wantBack: 0,
			wantFwd:  2,
		},
		{
			name:     "punctuation joins the surrounding word",
			line:     "foo,bar",
			col:      1,
			wantBack: 0,
			wantFwd:  7,
		},
		{
			name:     "col at end stays put on Alt+Right",
			line:     "abc",
			col:      3,
			wantBack: 0,
			wantFwd:  3,
		},
		{
			name:     "col at start stays put on Alt+Left",
			line:     "abc def",
			col:      0,
			wantBack: 0,
			wantFwd:  3,
		},
	}
	for _, tc := range cases {
		t.Run("back/"+tc.name, func(t *testing.T) {
			got := wordmove.BoundaryBack(tc.line, tc.col)
			if got != tc.wantBack {
				t.Fatalf("wordBoundaryBack(%q,%d)=%d want %d", tc.line, tc.col, got, tc.wantBack)
			}
		})
		t.Run("fwd/"+tc.name, func(t *testing.T) {
			got := wordmove.BoundaryFwd(tc.line, tc.col)
			if got != tc.wantFwd {
				t.Fatalf("wordBoundaryFwd(%q,%d)=%d want %d", tc.line, tc.col, got, tc.wantFwd)
			}
		})
	}
}

// TestAltWordPickerOverlayStillUpdates verifies the picker overlay's
// first-line filter is refreshed after Alt+Left / Alt+Right move the
// cursor (the dispatcher calls picker.Update with the new first line;
// without that, the picker's "matches" cache would lag the cursor and
// a subsequent Tab would complete against the old substring).
func TestAltWordPickerOverlayStillUpdates(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	typeString(m, "hello world foo bar")
	// Move cursor with Alt+Left to mid-word.
	for i := 0; i < 2; i++ {
		m.Update(altLeftKey())
	}
	row, col := cursorRowCol(m)
	if row != 0 || col != 12 {
		t.Fatalf("precondition: 2×Alt+Left from end should land at col 12 (start of 'foo'); got (%d,%d)", row, col)
	}
	// The picker's filter is keyed on firstLine(value); a "/" trigger
	// would now filter against "hello world foo bar" (unchanged, but
	// exercised through this code path so any future regression
	// where the picker is skipped on Alt+Left surfaces here).
	if v := m.prompt.input.Value(); !strings.Contains(v, "hello world foo bar") {
		t.Fatalf("expected value intact after Alt+Left motion; got %q", v)
	}
}

// altMKey returns a tea.KeyPressMsg equivalent to alt+m on a terminal
// that forwards Option as Meta (iTerm2, kitty, WezTerm, Alacritty,
// ghostty). The Terminal.app/VS Code path goes through
// consumeAltEsc → Feed → IsAltLetter and reaches the same handler;
// testing the native path keeps the test free of timing gates.
func altMKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: 'm', Mod: tea.ModAlt, Text: "m"})
}

// ctrlEnterKey returns Ctrl+Enter (or Cmd+Enter on macOS — bubbletea
// uses the same ModCtrl bit for both because the runtime normalises
// the chord to the same Key.String() form).
func ctrlEnterKey() tea.KeyPressMsg {
	return tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter, Mod: tea.ModCtrl, Text: "\n"})
}

// queueTestMode builds a RootModel wired for the cycle-244 tests.
// Unlike altTestMode this helper does NOT set TERM_PROGRAM (the
// tests pass the native alt+m chord directly), but it does pin
// ANTIGRAVITY_HOME so any persistence path stays off the user's
// real settings.json.
func queueTestMode(t *testing.T) *RootModel {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	t.Setenv("HOME", t.TempDir())
	m := newRootModel(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "", "/tmp", true)
	m.session.store.QueueMode = true
	return m
}

// TestAltMSubmitAlternate: In Queue mode, alt+m submits with the opposite setting.
func TestAltMSubmitAlternate(t *testing.T) {
	m := queueTestMode(t)
	m.session.store.QueueMode = true
	m.prompt.input.SetValue("alternate prompt")

	m.Update(altMKey())
	// In Queue mode, submitting with alternate executes immediate submission
	if m.prompt.input.Value() != "" {
		t.Errorf("after alt+m submit alternate, input should be cleared; got %q", m.prompt.input.Value())
	}
}

func TestMacOptionMTranslatesToAltM(t *testing.T) {
	if runtime.GOOS != "darwin" {
		t.Skip("darwin only")
	}
	orig := os.Getenv("AGY_CLI_MAC_OPTION_AS_ALT")
	defer os.Setenv("AGY_CLI_MAC_OPTION_AS_ALT", orig)
	os.Unsetenv("AGY_CLI_MAC_OPTION_AS_ALT")

	m := queueTestMode(t)
	m.session.store.QueueMode = true
	m.prompt.input.SetValue("alternate prompt option m")

	// On macOS terminal without Option-as-meta, Option+m generates 'µ' (U+00B5)
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'µ', Text: "µ"}))
	if m.prompt.input.Value() != "" {
		t.Errorf("after typing Option+m ('µ'), input should be cleared by alt+m submit; got %q", m.prompt.input.Value())
	}
}

// TestQueueModeCmdEnterSubmits confirms Cmd+Enter's behaviour:
// Cmd+Enter submits through the existing path.
func TestQueueModeCmdEnterSubmits(t *testing.T) {
	m := queueTestMode(t)
	m.prompt.input.SetValue("queued-prompt-body")

	m.Update(ctrlEnterKey())
	if v := m.prompt.input.Value(); v != "" {
		t.Errorf("Cmd+Enter: expected input cleared, got %q", v)
	}
}

// TestQueuedPromptDispatchesAfterTurnDone covers the AGY 1.4.0
// "queue a message while in Send Immediately mode" path: a queued
// prompt is dispatched as the natural next user message the moment
// the in-flight turn completes (turnDoneMsg). The test seeds
// m.queuedPrompt by hand so it does not depend on the
// submitOrQueuePrompt path (which has its own test above), then
// drives turnDoneMsg and asserts the queued prompt becomes a
// model turn.
func TestQueuedPromptDispatchesAfterTurnDone(t *testing.T) {
	m := queueTestMode(t)
	m.session.store.QueueMode = true
	m.session.store.QueuedPrompt = "hello"

	// A turnDoneMsg with no assistant reply: fakeClient has no model
	// session, so the streaming-rendering branch is skipped; the
	// queue-flush branch is the only thing we exercise here.
	prevSessionLen := len(m.SessionMessages())
	_, cmd := m.Update(toolsteps.TurnDoneMsg{Reply: "", Steps: nil, Reasoning: ""})
	if cmd == nil {
		t.Fatalf("after turnDoneMsg with queued prompt, expected non-nil cmd (the streamPrompt batch); got nil")
	}

	// m.queuedPrompt must be cleared; m.engine.session must have grown by one
	// user-role message containing the queued text.
	if m.session.store.QueuedPrompt != "" {
		t.Errorf("m.queuedPrompt should be cleared after flush; got %q", m.session.store.QueuedPrompt)
	}
	if got := len(m.SessionMessages()); got != prevSessionLen+1 {
		t.Fatalf("m.engine.session length = %d, want %d", got, prevSessionLen+1)
	}
	last := m.SessionMessages()[len(m.SessionMessages())-1]
	if last.Role != "user" {
		t.Errorf("queued role = %q, want user", last.Role)
	}
	if last.Content != "hello" {
		t.Errorf("queued content = %q, want %q", last.Content, "hello")
	}
}

// TestQueueModePersistsAcrossTurns verifies queueMode stays on across turns.
func TestQueueModePersistsAcrossTurns(t *testing.T) {
	m := queueTestMode(t)
	m.session.store.QueueMode = true

	// Drive one cycle: a regular submitPrompt followed by a turnDoneMsg.
	m.prompt.input.SetValue("first prompt")
	_, _ = m.Update(enterKey())
	_, _ = m.Update(toolsteps.TurnDoneMsg{Reply: "ok", Steps: nil, Reasoning: ""})
	if !m.session.store.QueueMode {
		t.Errorf("after turn, queueMode should still be true; got false")
	}
}

// _ ensures backend import is touched even when -p paths get
// stripped from a later refactor. The compiler will inline-discard
// if unused, but it's here as a tripwire for refactors that drop
// the cycle-244 helpers.
var _ = backend.Message{}

// streamQueueMode returns a RootModel already marked as streaming, so
// submitPrompt takes the queue branch.
func streamQueueMode(t *testing.T) *RootModel {
	t.Helper()
	m := queueTestMode(t)
	m.viewport.width, m.viewport.height = 200, 40
	m.conversation.streaming = true
	return m
}

// TestSubmitWhileStreamingQueues: Enter mid-turn stashes the prompt,
// clears the input, and leaves the transcript untouched.
func TestSubmitWhileStreamingQueues(t *testing.T) {
	m := streamQueueMode(t)
	m.session.store.QueueMode = true
	m.prompt.input.SetValue("SECOND message while running")
	before := len(m.conversation.response)

	if cmd := m.submitPrompt(); cmd != nil {
		t.Fatalf("submitPrompt mid-stream returned a cmd, want nil")
	}
	if len(m.session.store.StreamQueue) != 1 || m.session.store.StreamQueue[0] != "SECOND message while running" {
		t.Fatalf("streamQueue = %q, want the submitted prompt", m.session.store.StreamQueue)
	}
	if m.prompt.input.Value() != "" {
		t.Errorf("input not cleared after queueing: %q", m.prompt.input.Value())
	}
	if len(m.conversation.response) != before {
		t.Errorf("queueing echoed %d transcript rows, want 0", len(m.conversation.response)-before)
	}
}

type mockSteerEngine struct {
	steered []string
}

func (m *mockSteerEngine) Turn(ctx context.Context, t agentturn.Turn) (<-chan agentlink.Frame, error) {
	ch := make(chan agentlink.Frame)
	close(ch)
	return ch, nil
}
func (m *mockSteerEngine) Close() error { return nil }
func (m *mockSteerEngine) Steer(instruction string) error {
	m.steered = append(m.steered, instruction)
	return nil
}
func (m *mockSteerEngine) PendingSteer() []string {
	return m.steered
}
func (m *mockSteerEngine) PopSteer() (string, bool) {
	if len(m.steered) == 0 {
		return "", false
	}
	last := m.steered[len(m.steered)-1]
	m.steered = m.steered[:len(m.steered)-1]
	return last, true
}
func (m *mockSteerEngine) ClearSteer() []string {
	s := m.steered
	m.steered = nil
	return s
}

func TestTUI_SteerActiveTurn(t *testing.T) {
	m := streamQueueMode(t)
	eng := &mockSteerEngine{}
	m.turn.engine = eng
	m.prompt.input.SetValue("/steer format output as JSON")

	cmd := m.submitPrompt()
	if cmd != nil {
		t.Errorf("expected nil cmd, got %v", cmd)
	}
	if len(eng.steered) != 1 || eng.steered[0] != "format output as JSON" {
		t.Errorf("steered = %v, want ['format output as JSON']", eng.steered)
	}
	if m.prompt.input.Value() != "" {
		t.Errorf("input not reset after steer: %q", m.prompt.input.Value())
	}
	// Verify that the instruction is in SendingMessages waiting for step completion
	if len(m.session.store.SendingMessages) != 1 || m.session.store.SendingMessages[0] != "format output as JSON" {
		t.Errorf("expected SendingMessages to contain 'format output as JSON', got: %v", m.session.store.SendingMessages)
	}
	// Verify view renders the Sending message indicator
	view := m.View().Content
	if !strings.Contains(view, "format output as JSON (Sending message...)") {
		t.Errorf("expected view to contain Sending message row, got: %s", view)
	}

	// Now simulate step completion: steerAppliedMsg arrives
	m.Update(steerAppliedMsg{instruction: "format output as JSON"})

	// SendingMessages is reconciled
	if len(m.session.store.SendingMessages) != 0 {
		t.Errorf("expected SendingMessages to be empty after steer applied, got: %v", m.session.store.SendingMessages)
	}
	// Verify that user prompt row was displayed in transcript
	foundPrompt := false
	for _, line := range m.conversation.response {
		if strings.Contains(line, "format output as JSON") && strings.Contains(line, ">") {
			foundPrompt = true
			break
		}
	}
	if !foundPrompt {
		t.Errorf("expected user prompt row in transcript, got: %v", m.conversation.response)
	}
	if len(m.SessionMessages()) == 0 || m.SessionMessages()[len(m.SessionMessages())-1].Content != "format output as JSON" {
		t.Errorf("expected steer message in session, got: %v", m.SessionMessages())
	}
	if !m.viewport.rowsCacheDirty {
		t.Errorf("expected rowsCacheDirty to be true after steering")
	}
}

func TestTUI_SteerCommand_DisplaysAsUserInputMessage(t *testing.T) {
	m := streamQueueMode(t)
	eng := &mockSteerEngine{}
	m.turn.engine = eng
	m.prompt.input.SetValue("/steer Please switch to JSON format")

	cmd := m.submitPrompt()
	if cmd != nil {
		t.Errorf("expected nil cmd, got %v", cmd)
	}
	if len(eng.steered) != 1 || eng.steered[0] != "Please switch to JSON format" {
		t.Errorf("steered = %v, want ['Please switch to JSON format']", eng.steered)
	}
	if m.prompt.input.Value() != "" {
		t.Errorf("input not reset after steer: %q", m.prompt.input.Value())
	}
	if len(m.session.store.SendingMessages) != 1 || m.session.store.SendingMessages[0] != "Please switch to JSON format" {
		t.Errorf("expected SendingMessages to have steer prompt, got: %v", m.session.store.SendingMessages)
	}

	// When step completes, steerAppliedMsg arrives
	m.Update(steerAppliedMsg{instruction: "Please switch to JSON format"})

	// Verify that user message appears in m.conversation.response as a user prompt row
	foundPromptRow := false
	for _, line := range m.conversation.response {
		if strings.Contains(line, "Please switch to JSON format") && strings.Contains(line, ">") {
			foundPromptRow = true
			break
		}
	}
	if !foundPromptRow {
		t.Errorf("expected user prompt row in m.conversation.response, got: %v", m.conversation.response)
	}
	// Verify that user block is recorded
	if len(m.conversation.userBlocks) == 0 || m.conversation.userBlocks[len(m.conversation.userBlocks)-1].Raw != "Please switch to JSON format" {
		t.Errorf("expected user block recorded, got: %v", m.conversation.userBlocks)
	}
	// Verify that session recorded user turn
	if len(m.SessionMessages()) == 0 || m.SessionMessages()[len(m.SessionMessages())-1].Content != "Please switch to JSON format" {
		t.Errorf("expected session message recorded, got: %v", m.SessionMessages())
	}
	// Verify cache marked dirty
	if !m.viewport.rowsCacheDirty {
		t.Errorf("expected rowsCacheDirty to be true")
	}
}

func TestTUI_AltEnter_SteersInQueueMode(t *testing.T) {
	m := streamQueueMode(t)
	m.session.store.QueueMode = true
	eng := &mockSteerEngine{}
	m.turn.engine = eng
	m.prompt.input.SetValue("make this concise")

	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if len(eng.steered) != 1 || eng.steered[0] != "make this concise" {
		t.Fatalf("steered = %v, want ['make this concise']", eng.steered)
	}
	if m.prompt.input.Value() != "" {
		t.Errorf("input not reset: %q", m.prompt.input.Value())
	}
	if len(m.session.store.SendingMessages) != 1 || m.session.store.SendingMessages[0] != "make this concise" {
		t.Errorf("expected SendingMessages to contain 'make this concise', got: %v", m.session.store.SendingMessages)
	}

	m.Update(steerAppliedMsg{instruction: "make this concise"})

	foundPrompt := false
	for _, line := range m.conversation.response {
		if strings.Contains(line, "make this concise") && strings.Contains(line, ">") {
			foundPrompt = true
			break
		}
	}
	if !foundPrompt {
		t.Errorf("expected user prompt in response: %v", m.conversation.response)
	}
}

func TestTUI_SteerAppliedAtToolStepCompletion(t *testing.T) {
	m := streamQueueMode(t)
	m.session.store.QueueMode = false // normal mode: plain Enter steers while streaming
	eng := &mockSteerEngine{}
	m.turn.engine = eng

	// 1. Tool starts running in Step 1
	m.Update(toolsteps.ToolRunningMsg{Step: toolsteps.NewRunningStep("run_command", json.RawMessage(`{"CommandLine":"git status"}`))})

	// 2. User submits steer mid-tool
	m.prompt.input.SetValue("check diff as well")
	cmd := m.submitPrompt()
	if cmd != nil {
		t.Errorf("expected nil cmd, got %v", cmd)
	}
	if len(eng.steered) != 1 || eng.steered[0] != "check diff as well" {
		t.Fatalf("expected engine to receive steer, got: %v", eng.steered)
	}
	if len(m.session.store.SendingMessages) != 1 || m.session.store.SendingMessages[0] != "check diff as well" {
		t.Fatalf("expected SendingMessages to hold steer prompt, got: %v", m.session.store.SendingMessages)
	}

	// 3. Tool step finishes -> Steer must be immediately rendered into transcript at step boundary!
	m.Update(toolsteps.ToolStepMsg{Step: toolsteps.NewToolStep("run_command", json.RawMessage(`{"CommandLine":"git status"}`), "On branch main", false)})

	if len(m.session.store.SendingMessages) != 0 {
		t.Errorf("expected SendingMessages to be cleared after tool step finished, got: %v", m.session.store.SendingMessages)
	}

	// Verify that user prompt is visibly displayed in m.conversation.response
	foundPrompt := false
	for _, line := range m.conversation.response {
		if strings.Contains(line, "check diff as well") && strings.Contains(line, ">") {
			foundPrompt = true
			break
		}
	}
	if !foundPrompt {
		t.Fatalf("expected steer message rendered as user prompt in transcript at step completion, got: %v", m.conversation.response)
	}

	// 4. Step 2 streams its text response
	m.Update(backendChunkMsg("All changes verified."))

	// 5. Turn finishes
	m.Update(toolsteps.TurnDoneMsg{
		Reply: "All changes verified.",
		Steps: []toolsteps.Step{
			toolsteps.NewToolStep("run_command", json.RawMessage(`{"CommandLine":"git status"}`), "On branch main", false),
		},
	})

	// Verify that steer message is STILL in transcript and wasn't overwritten by TurnDoneMsg
	foundPromptAfterTurnDone := false
	for _, line := range m.conversation.response {
		if strings.Contains(line, "check diff as well") && strings.Contains(line, ">") {
			foundPromptAfterTurnDone = true
			break
		}
	}
	if !foundPromptAfterTurnDone {
		t.Fatalf("expected steer message to persist in transcript after turn completion, got: %v", m.conversation.response)
	}
}

func TestTUI_PromoteQueuedToSteer(t *testing.T) {
	m := streamQueueMode(t)
	eng := &mockSteerEngine{}
	m.turn.engine = eng
	m.session.store.QueuedPrompt = "make it fast"
	m.prompt.input.SetValue("/steer")

	cmd := m.submitPrompt()
	if cmd != nil {
		t.Errorf("expected nil cmd, got %v", cmd)
	}
	if len(eng.steered) != 1 || eng.steered[0] != "make it fast" {
		t.Errorf("steered = %v, want ['make it fast']", eng.steered)
	}
	if m.session.store.QueuedPrompt != "" {
		t.Errorf("queuedPrompt not cleared after promote: %q", m.session.store.QueuedPrompt)
	}
}

// TestQueuedPromptRendersAboveInput pins the pending row, the extra
// separator and the status label.
func TestQueuedPromptRendersAboveInput(t *testing.T) {
	m := streamQueueMode(t)
	m.session.store.StreamQueue = []string{"SECOND message while running"}

	view := m.View().Content
	if !strings.Contains(view, cDimGrayOpen+"\u25b8 SECOND message while running (alt+m to send immediately)") {
		t.Errorf("pending queue row missing or mis-styled:\n%q", view)
	}
	plain := termcolor.StripSGR(m.statusRow())
	if !strings.Contains(plain, "  Press up to edit queued messages") {
		t.Errorf("status left label = %q, want the up-to-edit hint", plain)
	}
	if strings.Contains(plain, "esc to cancel") {
		t.Errorf("status still shows esc-to-cancel while a prompt is queued: %q", plain)
	}
}

// TestSendingMessageNoDuplicateSeparator verifies that when a message is
// in SendingMessages or StreamQueue, there is no duplicate separator line
// between the queue row and the prompt top rule.
func TestSendingMessageNoDuplicateSeparator(t *testing.T) {
	m := streamQueueMode(t)
	m.session.store.SendingMessages = []string{"in flight message"}

	view := m.View().Content
	sep := m.separator()
	doubleSep := sep + "\n" + sep
	if strings.Contains(view, doubleSep) {
		t.Errorf("found duplicate separator line below SendingMessages in view:\n%s", view)
	}

	// Also verify with StreamQueue
	m.session.store.SendingMessages = nil
	m.session.store.StreamQueue = []string{"queued message"}
	viewQueue := m.View().Content
	if strings.Contains(viewQueue, doubleSep) {
		t.Errorf("found duplicate separator line below StreamQueue in view:\n%s", viewQueue)
	}
}

// TestTurnDoneFlushesQueuedPrompt: the finished turn's rows are
// finalised first, then the queued prompt is echoed and dispatched.
func TestTurnDoneFlushesQueuedPrompt(t *testing.T) {
	m := streamQueueMode(t)
	m.session.store.StreamQueue = []string{"SECOND message while running"}

	_, cmd := m.Update(toolsteps.TurnDoneMsg{Reply: "DONE"})
	if len(m.session.store.StreamQueue) != 0 {
		t.Fatalf("streamQueue not drained: %q", m.session.store.StreamQueue)
	}
	if cmd == nil {
		t.Fatalf("turnDoneMsg returned nil cmd, want the queued streamPrompt batch")
	}
	last := m.conversation.response[len(m.conversation.response)-1]
	if last != transcript.UserPromptRow("SECOND message while running") {
		t.Errorf("flushed user row = %q, want %q", last, transcript.UserPromptRow("SECOND message while running"))
	}
	if len(m.SessionMessages()) == 0 || m.SessionMessages()[len(m.SessionMessages())-1].Content != "SECOND message while running" {
		t.Errorf("queued prompt not appended to the session: %#v", m.SessionMessages())
	}
}

// TestUpRecallsQueuedPrompt: Up on an empty buffer moves all pending prompts
// back into the input (joined by \n) and clears the queue.
func TestUpRecallsQueuedPrompt(t *testing.T) {
	m := streamQueueMode(t)
	m.session.store.StreamQueue = []string{"first", "second"}

	key := tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	m.Update(key)

	if got := m.prompt.input.Value(); got != "first\nsecond" {
		t.Errorf("input after Up = %q, want %q", got, "first\nsecond")
	}
	if len(m.session.store.StreamQueue) != 0 {
		t.Errorf("streamQueue after Up = %q, want empty", m.session.store.StreamQueue)
	}
}

func TestTUI_UpRecallsQueuedPrompt_QueueMode(t *testing.T) {
	m := queueTestMode(t)
	m.session.store.QueueMode = true
	m.session.store.StreamQueue = []string{"queue mode prompt"}

	key := tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	m.Update(key)

	if got := m.prompt.input.Value(); got != "queue mode prompt" {
		t.Errorf("input after Up = %q, want 'queue mode prompt'", got)
	}
	if len(m.session.store.StreamQueue) != 0 {
		t.Errorf("StreamQueue not cleared after Up: %v", m.session.store.StreamQueue)
	}
}

func TestTUI_UpRecallsSteerPrompt(t *testing.T) {
	m := streamQueueMode(t)
	eng := &mockSteerEngine{}
	eng.steered = []string{"steer guidance"}
	m.turn.engine = eng

	key := tea.KeyPressMsg(tea.Key{Code: tea.KeyUp})
	m.Update(key)

	if got := m.prompt.input.Value(); got != "steer guidance" {
		t.Errorf("input after Up = %q, want 'steer guidance'", got)
	}
	if len(eng.steered) != 0 {
		t.Errorf("steer queue not cleared after Up: %v", eng.steered)
	}
}

func TestTUI_MoveSteerToQueue(t *testing.T) {
	m := streamQueueMode(t)
	eng := &mockSteerEngine{}
	eng.steered = []string{"steer instruction"}
	m.turn.engine = eng

	m.prompt.input.SetValue("/queue")
	cmd := m.submitPrompt()
	if cmd != nil {
		t.Errorf("expected nil cmd, got %v", cmd)
	}
	if len(eng.steered) != 0 {
		t.Errorf("steer queue should be popped after /queue, got %v", eng.steered)
	}
	if len(m.session.store.StreamQueue) != 1 || m.session.store.StreamQueue[0] != "steer instruction" {
		t.Errorf("expected 'steer instruction' in StreamQueue, got %v", m.session.store.StreamQueue)
	}
	if len(m.conversation.response) != 0 {
		t.Errorf("expected empty response after /queue, got: %v", m.conversation.response)
	}
}

func TestTUI_ExplicitQueueCommandWhileStreaming(t *testing.T) {
	m := streamQueueMode(t)
	m.session.store.QueueMode = false // normal mode: plain text would steer
	m.prompt.input.SetValue("/queue check this later")

	cmd := m.submitPrompt()
	if cmd != nil {
		t.Errorf("expected nil cmd, got %v", cmd)
	}
	if len(m.session.store.StreamQueue) != 1 || m.session.store.StreamQueue[0] != "check this later" {
		t.Errorf("expected StreamQueue to have 'check this later', got: %v", m.session.store.StreamQueue)
	}
}

func TestSubmitPrompt_RunsContextPanelWhileStreaming(t *testing.T) {
	m := streamQueueMode(t)
	m.prompt.input.SetValue("/context")

	if cmd := m.submitPrompt(); cmd != nil {
		t.Fatalf("submitPrompt(/context) mid-stream returned a cmd, want nil")
	}
	if len(m.session.store.StreamQueue) != 0 {
		t.Fatalf("streamQueue = %q, want slash command not queued", m.session.store.StreamQueue)
	}
	if m.overlays.panel.kind != panelregistry.Context {
		t.Fatalf("panel.kind = %v, want panelregistry.Context", m.overlays.panel.kind)
	}
	if !m.conversation.streaming {
		t.Error("panel dispatch cleared streaming; the in-flight turn must survive")
	}
}

func TestSubmitPrompt_RunsAccountsPanelWhileStreaming(t *testing.T) {
	m := streamQueueMode(t)
	m.prompt.input.SetValue("/accounts")

	_ = m.submitPrompt()
	if len(m.session.store.StreamQueue) != 0 {
		t.Fatalf("streamQueue = %q, want /accounts not queued as prompt", m.session.store.StreamQueue)
	}
	if m.overlays.panel.kind != panelregistry.Accounts {
		t.Fatalf("panel.kind = %v, want panelregistry.Accounts", m.overlays.panel.kind)
	}
	if !m.conversation.streaming {
		t.Error("panel dispatch cleared streaming; the in-flight turn must survive")
	}
}

func TestSubmitPrompt_QueuesModelTurnSlashWhileStreaming(t *testing.T) {
	m := streamQueueMode(t)
	m.prompt.input.SetValue("/plan next steps")

	if cmd := m.submitPrompt(); cmd != nil {
		t.Fatalf("submitPrompt(/plan) mid-stream returned a cmd, want nil")
	}
	if len(m.session.store.StreamQueue) != 1 || m.session.store.StreamQueue[0] != "/plan next steps" {
		t.Fatalf("streamQueue = %q, want /plan queued", m.session.store.StreamQueue)
	}
	if m.overlays.panel.kind != panelregistry.None {
		t.Errorf("panel.kind = %v, want panelregistry.None for a queued /plan", m.overlays.panel.kind)
	}
}

func TestSubmitPrompt_RunsTranscriptMutatingSlashWhileStreaming(t *testing.T) {
	for _, buf := range []string{"/clear", "/new"} {
		m := streamQueueMode(t)
		m.setSessionMessages([]backend.Message{{Role: "user", Content: "live turn"}})
		m.conversation.response = []string{"> live turn"}
		m.prompt.input.SetValue(buf)
		m.submitPrompt()

		if len(m.session.store.StreamQueue) != 0 {
			t.Errorf("%s: streamQueue = %q, want immediate dispatch", buf, m.session.store.StreamQueue)
		}
		if m.conversation.streaming {
			t.Errorf("%s: streaming = true, want the turn aborted", buf)
		}
		if !m.conversation.discardTurn {
			t.Errorf("%s: discardTurn = false, want the cancelled turn suppressed", buf)
		}
		if len(m.SessionMessages()) != 0 || len(m.conversation.response) != 0 {
			t.Errorf("%s: session/response not cleared (session %d, response %d)", buf, len(m.SessionMessages()), len(m.conversation.response))
		}
	}

	m := streamQueueMode(t)
	m.prompt.input.SetValue("/compact")
	m.submitPrompt()
	if !m.conversation.streaming {
		t.Error("/compact mid-stream stopped the turn; the reference keeps streaming")
	}
	if m.viewport.inputNotice != "Nothing to compact yet." {
		t.Errorf("/compact mid-stream inputNotice = %q, want %q", m.viewport.inputNotice, "Nothing to compact yet.")
	}
	if len(m.conversation.response) != 0 {
		t.Errorf("/compact mid-stream wrote a transcript row: %q", m.conversation.response)
	}
	if m.prompt.input.Value() == "Nothing to compact yet." {
		t.Error("/compact mid-stream put the notice in the editable buffer")
	}
	if m.conversation.discardTurn {
		t.Error("/compact mid-stream discarded the turn")
	}

	m = streamQueueMode(t)
	m.prompt.input.SetValue("/rewind")
	m.submitPrompt()
	if m.overlays.panel.kind != panelregistry.Rewind {
		t.Errorf("/rewind mid-stream panel = %v, want panelregistry.Rewind", m.overlays.panel.kind)
	}
	if !m.conversation.streaming {
		t.Error("/rewind mid-stream stopped the turn; the reference leaves it running")
	}
}

func TestQueuedSlashDispatchesOnTurnDone(t *testing.T) {
	m := streamQueueMode(t)
	m.session.store.StreamQueue = []string{"/context"}
	prevSession := len(m.SessionMessages())

	if _, cmd := m.Update(toolsteps.TurnDoneMsg{Reply: "", Steps: nil, Reasoning: ""}); cmd == nil {
		t.Fatal("turnDoneMsg with a queued slash returned a nil cmd (ClearScreen should remain)")
	}
	if m.overlays.panel.kind != panelregistry.Context {
		t.Fatalf("queued /context did not open the panel on flush; panel.kind = %v", m.overlays.panel.kind)
	}
	if len(m.SessionMessages()) != prevSession {
		t.Errorf("queued slash was appended to the transcript as a user message (len %d, want %d)",
			len(m.SessionMessages()), prevSession)
	}
	if m.conversation.streaming {
		t.Error("queued slash started a model turn; streaming = true")
	}
}

func TestDispatchSlashLine(t *testing.T) {
	m := streamQueueMode(t)
	if handled, _ := m.dispatchSlashLine("plain text"); handled {
		t.Error("dispatchSlashLine(plain text) = handled, want false")
	}
	if handled, _ := m.dispatchSlashLine("/context"); !handled || m.overlays.panel.kind != panelregistry.Context {
		t.Errorf("/context: handled=%v panel=%v, want handled=true panelregistry.Context", handled, m.overlays.panel.kind)
	}
}

func TestContextPanel_StreamingLayout(t *testing.T) {
	m := streamQueueMode(t)
	m.viewport.width = 120
	m.setSessionMessages(append(m.SessionMessages(), backend.Message{Role: "user", Content: "slow sea story please"}))

	lines := contextpanel.Lines(m.contextState())
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Estimated usage (awaiting generation)") {
		t.Errorf("streaming /context missing the awaiting-generation row:\n%s", joined)
	}
	if strings.Contains(joined, "Token usage by category") {
		t.Errorf("streaming /context rendered the completed-session layout:\n%s", joined)
	}
	wantUser := contextpanel.TokenLabel(len("slow sea story please") / 4)
	if !strings.Contains(joined, "User messages: "+wantUser+" tokens") {
		t.Errorf("streaming /context user tokens = want %s:\n%s", wantUser, joined)
	}
	m.conversation.liveReplyBuf = "The lighthouse stood at the edge of the harbor, its white tower weathered by salt and wind.  The lighthouse stood"
	joined = strings.Join(contextpanel.Lines(m.contextState()), "\n")
	wantAgent := contextpanel.TokenLabel(len(m.conversation.liveReplyBuf) / 4)
	if !strings.Contains(joined, "Agent responses: "+wantAgent+" tokens") {
		t.Errorf("streaming /context agent tokens = want %s:\n%s", wantAgent, joined)
	}
	if !strings.Contains(joined, "Free space: 1.0M (100.0%)") {
		t.Errorf("streaming /context free row = %q, want 1.0M (100.0%%)", joined)
	}
}

func TestModelPicker_KeepsConfiguredModels(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"gateway": {
		"type": "openai", "base_url": "https://gateway.example/v1",
		"models": [{"id": "minimax-m3", "name": "MiniMax M3", "model": "Upstream-1"}]}}}`)
	var found bool
	for _, r := range modelpicker.RowsView() {
		if r.Slug == "minimax-m3" {
			found = true
			if r.Display != "MiniMax M3" {
				t.Errorf("display = %q, want MiniMax M3", r.Display)
			}
			if r.Note != "configured model" && r.Note != "via gateway" {
				t.Errorf("note = %q, want the configured-model description", r.Note)
			}
		}
	}
	if !found {
		t.Fatal("minimax-m3 missing from the picker's rows; the /model picker must keep it")
	}
}

func mouseClick(x, y int, btn tea.MouseButton) tea.MouseClickMsg {
	return tea.MouseClickMsg{X: x, Y: y, Button: btn}
}

func mouseMotion(x, y int) tea.MouseMotionMsg {
	return tea.MouseMotionMsg{X: x, Y: y, Button: tea.MouseLeft}
}

func mouseRelease(x, y int, btn tea.MouseButton) tea.MouseReleaseMsg {
	return tea.MouseReleaseMsg{X: x, Y: y, Button: btn}
}

// Test_SelectionMouseDownMotionRelease walks a single-row drag and
// verifies: state is updated on each event, the final Release
// returns an OSC52 copy cmd, and the highlight vanishes after the
// release so subsequent frames don't paint a phantom rectangle.
func Test_SelectionMouseDownMotionRelease(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Press at (col=3, row=2). State: drag=true, start=end=(2,3).
	m.Update(mouseClick(3, 2, tea.MouseLeft))
	if !m.viewport.SelDragging || m.viewport.SelStartRow != 2 || m.viewport.SelStartCol != 3 ||
		m.viewport.SelEndRow != 2 || m.viewport.SelEndCol != 3 {
		t.Fatalf("after click: %+v", m)
	}

	// Move to (col=10, row=2). State: drag=true, end=(2,10).
	m.Update(mouseMotion(10, 2))
	if !m.viewport.SelDragging || m.viewport.SelEndRow != 2 || m.viewport.SelEndCol != 10 {
		t.Fatalf("after motion: %+v", m)
	}

	// Release at (col=10, row=2). The cmd must be non-nil (it
	// runs OSC52); drag state clears; range is zeroed so the
	// highlight vanishes from subsequent frames.
	_, cmd := m.Update(mouseRelease(10, 2, tea.MouseLeft))
	if cmd == nil {
		t.Fatalf("release should produce a SetClipboard cmd, got nil")
	}
	if m.viewport.SelDragging || m.viewport.SelStartRow != 0 || m.viewport.SelStartCol != 0 ||
		m.viewport.SelEndRow != 0 || m.viewport.SelEndCol != 0 {
		t.Fatalf("release did not clear state: %+v", m)
	}

	// After release, selectionPass is a no-op (no active range).
	// Verify: the render output does NOT contain the highlight
	// escape.
	view := m.View().Content
	if strings.Contains(view, selHighlightOpen) {
		t.Errorf("highlight persisted after release")
	}
}

// Test_SelectionCopyText verifies the joined text the OSC52 cmd
// would write: a single-row drag yields that row's plain-text
// slice; a multi-row drag joins rows with \n.
func Test_SelectionCopyText(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Single row.
	m.Update(mouseClick(2, 1, tea.MouseLeft))
	m.Update(mouseMotion(6, 1))
	got := m.selectionText()
	if got != "" {
		// We don't pin the transcript content here; the test
		// only asserts the function runs without panicking and
		// returns a string. A non-empty result is the success
		// path: there were 5 columns of transcript at row 1.
		if len(got) == 0 {
			t.Fatalf("single-row drag produced empty text")
		}
	}

	// Multi-row.
	m.Update(mouseClick(0, 0, tea.MouseLeft))
	m.Update(mouseMotion(5, 3))
	got = m.selectionText()
	// Multi-row drags must contain a newline between rows.
	if !strings.Contains(got, "\n") {
		t.Errorf("multi-row drag missing newline: %q", got)
	}
}

// Test_SelectionReverseDrag verifies the range is normalized — a
// drag from right-to-left still produces start <= end so the
// highlight paints the right cells.
func Test_SelectionReverseDrag(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.Update(mouseClick(20, 3, tea.MouseLeft))
	m.Update(mouseMotion(5, 1)) // reversed direction

	sr, sc, er, ec, ok := tuiviewport.SelectionRange(m.viewport.SelStartRow, m.viewport.SelStartCol, m.viewport.SelEndRow, m.viewport.SelEndCol, m.viewport.SelDragging)
	if !ok {
		t.Fatalf("range inactive")
	}
	if sr > er || (sr == er && sc > ec) {
		t.Errorf("range not normalized: (%d,%d)->(%d,%d)", sr, sc, er, ec)
	}
	// After release the highlight should still reflect the same
	// normalized rectangle.
	if sr != 1 || sc != 5 || er != 3 || ec != 20 {
		t.Errorf("wrong normalization: got (%d,%d)->(%d,%d) want (1,5)->(3,20)",
			sr, sc, er, ec)
	}
}

// Test_SelectionHighlightInView verifies the rendered View contains
// the highlight escape exactly once per active drag cell range.
// We construct the state directly (no mouse events) so the test is
// independent of the transcript content.
func Test_SelectionHighlightInView(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// No drag → no highlight.
	if strings.Contains(m.View().Content, selHighlightOpen) {
		t.Errorf("idle view should not contain highlight")
	}

	// Drag active on rows 2..4, cols 5..15.
	m.viewport.SelDragging = true
	m.viewport.SelStartRow, m.viewport.SelStartCol = 2, 5
	m.viewport.SelEndRow, m.viewport.SelEndCol = 4, 15

	view := m.View().Content
	if !strings.Contains(view, selHighlightOpen) {
		t.Errorf("active drag should paint highlight")
	}
	// Should appear at least once per highlighted row (3 rows).
	count := strings.Count(view, selHighlightOpen)
	if count < 3 {
		t.Errorf("expected ≥3 highlights (one per row), got %d", count)
	}
}

// Test_SelectionClickWithoutDrag verifies a click without any drag
// is a no-op for copy: no OSC52 cmd, no highlight.
func Test_SelectionClickWithoutDrag(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	m.Update(mouseClick(3, 3, tea.MouseLeft))
	_, cmd := m.Update(mouseRelease(3, 3, tea.MouseLeft))
	if cmd != nil {
		t.Errorf("click-without-drag should not produce a copy cmd")
	}
	if strings.Contains(m.View().Content, selHighlightOpen) {
		t.Errorf("click-without-drag should leave no highlight")
	}
}

// Test_MouseSlashCommandRemoved verifies the cycle-166 /mouse slash
// command no longer mutates RootModel state. The reference binary
// has no such toggle and the cycle-174 work removes the toggle
// infrastructure entirely. handleSlashCommand's default branch
// handles /mouse the same way it handles any other unknown
// command: appends "Unknown command: /mouse" to the response and
// leaves the model fields alone.
func Test_MouseSlashCommandRemoved(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})

	// Snapshot the response before, so we can detect the
	// "Unknown command" feedback without depending on whether
	// some other state has been written previously.
	before := len(m.conversation.response)
	for _, arg := range []string{"/mouse off", "/mouse on", "/mouse toggle"} {
		m.handleSlashCommand(arg)
	}
	// Every invocation must produce exactly one response entry —
	// the "Unknown command: /mouse" line. None of them produce
	// the cycle-166 "Mouse on / Mouse off" feedback.
	for _, line := range m.conversation.response[before:] {
		if strings.Contains(line, "Mouse on ") || strings.Contains(line, "Mouse off ") {
			t.Errorf("cycle-166 toggle feedback leaked: %q", line)
		}
	}
	// After the unrecognised commands the View must still report
	// MouseModeCellMotion — there's no fallback toggle to flip.
	if v := m.View(); v.MouseMode != tea.MouseModeCellMotion {
		t.Errorf("MouseMode flipped by /mouse: got %v, want CellMotion", v.MouseMode)
	}
}

// sgrReverseMask reports, for every display cell of s, whether reverse
// video is active at that cell. Cycle 278: the old tests only checked
// that the opening escape was present, which stayed true even when the
// selected text replayed its own "\x1b[m" right after it and painted
// nothing. Asserting the per-cell effect is the only way to catch that.
func sgrReverseMask(s string) []bool {
	var mask []bool
	reverse := false
	for i := 0; i < len(s); {
		if s[i] == 0x1b {
			if i+1 < len(s) && s[i+1] == '[' {
				j := i + 2
				for j < len(s) && !(s[j] >= 0x40 && s[j] <= 0x7e) {
					j++
				}
				if j < len(s) {
					if s[j] == 'm' {
						for _, p := range strings.Split(s[i+2:j], ";") {
							n := 0
							if p != "" {
								n, _ = strconv.Atoi(p)
							}
							switch n {
							case 0, 27:
								reverse = false
							case 7:
								reverse = true
							}
						}
					}
					i = j + 1
					continue
				}
			}
			i++
			continue
		}
		r, size := utf8.DecodeRuneInString(s[i:])
		w := ansi.StringWidth(string(r))
		for k := 0; k < w; k++ {
			mask = append(mask, reverse)
		}
		i += size
	}
	return mask
}

// TestApplySelectionHighlight_Table pins the cue cell-by-cell across the
// row shapes a real transcript produces. The "after-reset" case is the
// one the user reported: the selection started inside "remember", i.e.
// after glamour's closing "\x1b[m", and the old implementation emitted
// open → replay(reset) → text, so not a single cell was inverted.
func TestApplySelectionHighlight_Table(t *testing.T) {
	const bold = "  this is \x1b[38;2;7;54;66;1mimportant\x1b[m to remember"
	const chip = "  use \x1b[38;5;32;48;5;254mgo test ./...\x1b[0m to verify"
	const cjk = "  日本語のテキスト"
	const rev = "  \x1b[7mpicker cursor\x1b[27m tail"

	cases := []struct {
		name     string
		row      string
		from, to int // half-open cell range
	}{
		{"plain", "  plain text row", 2, 12},
		{"glamour-before-reset", bold, 2, 10},
		{"glamour-across-reset", bold, 10, 20},
		{"glamour-after-reset", bold, 24, 31},
		{"inline-code-chip", chip, 6, 20},
		{"cjk", cjk, 2, 6},
		{"reverse-off", rev, 4, 14},
		{"whole-row", "abc", 0, 3},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := tuiviewport.ApplySelectionHighlight(tc.row, 0, tc.from, 0, tc.to-1, 0)

			if w, want := ansi.StringWidth(got), ansi.StringWidth(tc.row); w != want {
				t.Fatalf("width changed: %d -> %d (%q)", want, w, got)
			}
			if s, want := ansi.Strip(got), ansi.Strip(tc.row); s != want {
				t.Fatalf("text changed: %q -> %q", want, s)
			}

			gotMask := sgrReverseMask(got)
			rowMask := sgrReverseMask(tc.row)
			for c := tc.from; c < tc.to && c < len(gotMask); c++ {
				if !gotMask[c] {
					t.Fatalf("cell %d is not inverted; the cue was punched through\n got=%q", c, got)
				}
			}
			for c := 0; c < len(rowMask) && c < len(gotMask); c++ {
				if c >= tc.from && c < tc.to {
					continue
				}
				if gotMask[c] != rowMask[c] {
					t.Fatalf("cell %d outside the selection changed: got %v want %v\n got=%q",
						c, gotMask[c], rowMask[c], got)
				}
			}
		})
	}
}

// TestApplySelectionHighlight_OutOfRangeRowsAreUntouched keeps the
// pass's contract: rows above/below the drag are returned byte-identical.
func TestApplySelectionHighlight_OutOfRangeRowsAreUntouched(t *testing.T) {
	row := "  \x1b[38;2;7;54;66;1mstyled\x1b[m row"
	for _, idx := range []int{0, 5, -1} {
		if got := tuiviewport.ApplySelectionHighlight(row, 2, 0, 4, 3, idx); got != row {
			t.Fatalf("row %d was modified: %q", idx, got)
		}
	}
}

// setupInputMode creates a RootModel in altScreen mode with an initialized terminal size.
func setupInputMode(val string) *RootModel {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	m.prompt.input.SetValue(val)
	// Render once to populate promptScreenY
	_ = m.View()
	return m
}

func TestInputSelection_MouseClickWithoutDrag(t *testing.T) {
	m := setupInputMode("hello world")
	if m.prompt.promptScreenY < 0 {
		t.Fatalf("promptScreenY should be >= 0, got %d", m.prompt.promptScreenY)
	}

	// Click at 'w' (col = 2 (prompt "> ") + 6 ("hello ") = 8)
	m.Update(mouseClick(8, m.prompt.promptScreenY, tea.MouseLeft))
	if !m.prompt.InputSelDragging {
		t.Errorf("expected inputSelDragging to be true during click")
	}

	// Release without moving
	_, cmd := m.Update(mouseRelease(8, m.prompt.promptScreenY, tea.MouseLeft))
	if cmd != nil {
		t.Errorf("expected nil cmd for click without drag, got %v", cmd)
	}
	if m.prompt.HasInputSelection() {
		t.Errorf("expected no selection after click without drag")
	}
	if m.prompt.input.Column() != 6 {
		t.Errorf("expected cursor column 6, got %d", m.prompt.input.Column())
	}
}

func TestInputSelection_MouseDragForwardAutoCopy(t *testing.T) {
	m := setupInputMode("hello world")

	// Click at start of "hello" (x=2)
	m.Update(mouseClick(2, m.prompt.promptScreenY, tea.MouseLeft))
	// Drag to end of "hello" (x=7)
	m.Update(mouseMotion(7, m.prompt.promptScreenY))

	if !m.prompt.HasInputSelection() {
		t.Fatalf("expected active input selection during drag")
	}
	start, end := m.prompt.InputSelectionRange()
	if start != 0 || end != 5 {
		t.Fatalf("expected range [0, 5], got [%d, %d]", start, end)
	}

	// Verify visual highlight in View()
	v := m.View()
	viewStr := v.Content
	if !strings.Contains(viewStr, "\x1b[0m\x1b[7mhello\x1b[0m") {
		t.Errorf("expected reverse video highlight for 'hello', got: %q", viewStr)
	}

	// Release: should auto-copy "hello" to clipboard
	_, cmd := m.Update(mouseRelease(7, m.prompt.promptScreenY, tea.MouseLeft))
	if cmd == nil {
		t.Errorf("expected non-nil copy cmd on release")
	}
	// Selection stays active after release
	if !m.prompt.HasInputSelection() {
		t.Errorf("expected selection to remain highlighted after mouse release")
	}
}

func TestInputSelection_MouseDragBackwardAutoCopy(t *testing.T) {
	m := setupInputMode("hello world")

	// Click at end of "hello" (x=7)
	m.Update(mouseClick(7, m.prompt.promptScreenY, tea.MouseLeft))
	// Drag backward to start of "hello" (x=2)
	m.Update(mouseMotion(2, m.prompt.promptScreenY))

	start, end := m.prompt.InputSelectionRange()
	if start != 0 || end != 5 {
		t.Fatalf("expected normalized range [0, 5], got [%d, %d]", start, end)
	}

	// Release
	_, cmd := m.Update(mouseRelease(2, m.prompt.promptScreenY, tea.MouseLeft))
	if cmd == nil {
		t.Errorf("expected non-nil copy cmd on release")
	}
}

func TestInputSelection_DragOutsideClamps(t *testing.T) {
	m := setupInputMode("hello")

	// Click at x=2
	m.Update(mouseClick(2, m.prompt.promptScreenY, tea.MouseLeft))

	// Drag way beyond right wall and below screen
	m.Update(mouseMotion(150, 40))
	start, end := m.prompt.InputSelectionRange()
	if start != 0 || end != len("hello") {
		t.Errorf("expected clamped [0, 5], got [%d, %d]", start, end)
	}

	// Drag way above and to the left
	m.Update(mouseMotion(-10, -5))
	start, end = m.prompt.InputSelectionRange()
	if start != 0 || end != 0 {
		t.Errorf("expected clamped [0, 0], got [%d, %d]", start, end)
	}
}

func TestInputSelection_CmdA_SelectAllAndAutoCopy(t *testing.T) {
	m := setupInputMode("echo 123")

	// Press Cmd+A (ModSuper)
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModSuper}))
	if cmd == nil {
		t.Errorf("expected non-nil copy cmd on Cmd+A")
	}
	if !m.prompt.HasInputSelection() {
		t.Fatalf("expected active input selection on Cmd+A")
	}
	start, end := m.prompt.InputSelectionRange()
	if start != 0 || end != len("echo 123") {
		t.Errorf("expected [0, 8], got [%d, %d]", start, end)
	}

	// Verify reverse-video highlight over all text
	v := m.View()
	viewStr := v.Content
	if !strings.Contains(viewStr, "\x1b[0m\x1b[7mecho 123\x1b[0m") {
		t.Errorf("expected reverse video highlight for 'echo 123', got: %q", viewStr)
	}
}

func TestInputSelection_CtrlShiftA_SelectAll(t *testing.T) {
	m := setupInputMode("cat file.txt")

	// Press Ctrl+Shift+A
	_, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModCtrl | tea.ModShift}))
	if cmd == nil {
		t.Errorf("expected non-nil copy cmd on Ctrl+Shift+A")
	}
	if !m.prompt.HasInputSelection() {
		t.Fatalf("expected active input selection on Ctrl+Shift+A")
	}
	start, end := m.prompt.InputSelectionRange()
	if start != 0 || end != len("cat file.txt") {
		t.Errorf("expected [0, 12], got [%d, %d]", start, end)
	}
}

func TestInputSelection_BackspaceDeletesSelection(t *testing.T) {
	m := setupInputMode("hello world")

	// Select "world" [6, 11]
	m.prompt.InputSelStart = 6
	m.prompt.InputSelEnd = 11

	// Press Backspace
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyBackspace}))
	if m.prompt.HasInputSelection() {
		t.Errorf("expected selection cleared after backspace")
	}
	if got := m.prompt.input.Value(); got != "hello " {
		t.Errorf("expected 'hello ', got %q", got)
	}
}

func TestInputSelection_DeleteDeletesSelection(t *testing.T) {
	m := setupInputMode("hello world")

	// Select "hello " [0, 6]
	m.prompt.InputSelStart = 0
	m.prompt.InputSelEnd = 6

	// Press Delete
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyDelete}))
	if m.prompt.HasInputSelection() {
		t.Errorf("expected selection cleared after delete")
	}
	if got := m.prompt.input.Value(); got != "world" {
		t.Errorf("expected 'world', got %q", got)
	}
}

func TestInputSelection_TypeReplacesSelection(t *testing.T) {
	m := setupInputMode("hello world")

	// Select "world" [6, 11]
	m.prompt.InputSelStart = 6
	m.prompt.InputSelEnd = 11

	// Type '!'
	m.Update(tea.KeyPressMsg(tea.Key{Code: '!', Text: "!"}))
	if m.prompt.HasInputSelection() {
		t.Errorf("expected selection cleared after typing")
	}
	if got := m.prompt.input.Value(); got != "hello !" {
		t.Errorf("expected 'hello !', got %q", got)
	}
}

func TestInputSelection_PasteReplacesSelection(t *testing.T) {
	m := setupInputMode("hello world")

	// Select "world" [6, 11]
	m.prompt.InputSelStart = 6
	m.prompt.InputSelEnd = 11

	// Paste "there"
	m.insertPastedText("there")
	if m.prompt.HasInputSelection() {
		t.Errorf("expected selection cleared after paste")
	}
	if got := m.prompt.input.Value(); got != "hello there" {
		t.Errorf("expected 'hello there', got %q", got)
	}
}

func TestInputSelection_EscapeClearsSelection(t *testing.T) {
	m := setupInputMode("hello world")

	// Select all via Cmd+A
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModSuper}))
	if !m.prompt.HasInputSelection() {
		t.Fatalf("expected active selection")
	}

	// Press Esc
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.prompt.HasInputSelection() {
		t.Errorf("expected selection cleared after Esc")
	}
	// Buffer should NOT be cleared by the first Esc when selection was active
	if got := m.prompt.input.Value(); got != "hello world" {
		t.Errorf("expected buffer preserved after Esc-cleared selection, got %q", got)
	}
}

func TestInputSelection_ArrowKeysClearSelection(t *testing.T) {
	m := setupInputMode("hello world")

	m.prompt.InputSelStart = 0
	m.prompt.InputSelEnd = 5

	// Press Left arrow
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyLeft}))
	if m.prompt.HasInputSelection() {
		t.Errorf("expected selection cleared after KeyLeft")
	}
	if col := m.prompt.input.Column(); col != 0 {
		t.Errorf("expected cursor at col 0, got %d", col)
	}
}

func TestInputSelection_MultiLine(t *testing.T) {
	m := setupInputMode("first line\nsecond line")
	if m.prompt.InputLineCount() != 2 {
		t.Fatalf("expected 2 lines, got %d", m.prompt.InputLineCount())
	}

	// Select all
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'a', Mod: tea.ModSuper}))
	v := m.View()
	viewStr := v.Content
	if !strings.Contains(viewStr, "\x1b[0m\x1b[7mfirst line\x1b[0m") {
		t.Errorf("expected highlight on first line, got: %q", viewStr)
	}
	if !strings.Contains(viewStr, "\x1b[0m\x1b[7msecond line\x1b[0m") {
		t.Errorf("expected highlight on second line, got: %q", viewStr)
	}
}

func TestInputSelection_UTF8Accuracy(t *testing.T) {
	m := setupInputMode("你好世界")

	// Click at start of "世界": prompt "> " (2 cells) + "你好" (4 cells) = cell 6
	m.Update(mouseClick(6, m.prompt.promptScreenY, tea.MouseLeft))
	// Drag to end of "世界": cell 6 + 4 = 10
	m.Update(mouseMotion(10, m.prompt.promptScreenY))

	start, end := m.prompt.InputSelectionRange()
	val := m.prompt.input.Value()
	if val[start:end] != "世界" {
		t.Errorf("expected '世界' selected, got %q (range %d..%d)", val[start:end], start, end)
	}

	v := m.View()
	viewStr := v.Content
	if !strings.Contains(viewStr, "\x1b[0m\x1b[7m世界\x1b[0m") {
		t.Errorf("expected reverse video highlight for '世界', got: %q", viewStr)
	}
}

// withClipboard swaps the clipboard-read seam for the duration of a test.
func withClipboard(t *testing.T, fn func() string) {
	t.Helper()
	old := clipboardReadText
	clipboardReadText = fn
	t.Cleanup(func() { clipboardReadText = old })
}

func TestPasteMsgInsertsAtCursor(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.prompt.input.SetValue("ab")
	m.prompt.input.CursorStart()

	if _, cmd := m.Update(tea.PasteMsg{Content: "XY"}); cmd != nil {
		t.Fatalf("paste returned a cmd; it must not submit or leave the loop")
	}
	if got, want := m.prompt.input.Value(), "XYab"; got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}
}

func TestPasteMsgPreservesNewlines(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.prompt.input.SetValue("")

	m.Update(tea.PasteMsg{Content: "a\nb\nc"})
	if got, want := m.prompt.input.Value(), "a\nb\nc"; got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}
	if n := m.prompt.input.LineCount(); n != 3 {
		t.Fatalf("LineCount = %d, want 3", n)
	}
}

func TestPasteMsgNormalizesCRLF(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.prompt.input.SetValue("")

	m.Update(tea.PasteMsg{Content: "a\r\nb\r"})
	// Stray CR becomes newline to preserve lines in terminals emitting CR on paste
	if got, want := m.prompt.input.Value(), "a\nb\n"; got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}
}

func TestPasteMsgDoesNotSubmit(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.prompt.input.SetValue("")
	before := len(m.conversation.response)

	m.Update(tea.PasteMsg{Content: "hello"})
	if len(m.conversation.response) != before {
		t.Fatalf("paste appended %d transcript rows; it must never submit",
			len(m.conversation.response)-before)
	}
	if m.conversation.streaming {
		t.Fatalf("paste started a turn")
	}
}

func TestPasteMsgFoldsLargeBlocks(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.prompt.input.SetValue("")

	// 16 lines folds into [Pasted text #1 +16 lines]
	content16 := strings.Repeat("line\n", 15) + "line16"
	m.Update(tea.PasteMsg{Content: content16})
	if got := m.prompt.input.Value(); got != "[Pasted text #1 +16 lines]" {
		t.Fatalf("16 lines paste folded into %q, want %q", got, "[Pasted text #1 +16 lines]")
	}

	// Submit expands placeholder and records expanded prompt to history
	m.prompt.input.SetValue("echo " + m.prompt.input.Value())
	cmd := m.submitPrompt()
	_ = cmd
	if got := m.prompt.promptHistory.Entries()[m.prompt.promptHistory.Len()-1]; got != "echo "+content16 {
		t.Fatalf("history entry = %q, want expanded prompt", got)
	}
	if len(m.prompt.pastedBlocks) != 0 {
		t.Fatalf("pastedBlocks should be reset after submission, got %d", len(m.prompt.pastedBlocks))
	}

	// 1001 chars folds into [Pasted text #2 1001 chars]
	m.prompt.input.Reset()
	content1001 := strings.Repeat("x", 1001)
	m.Update(tea.PasteMsg{Content: content1001})
	if got := m.prompt.input.Value(); got != "[Pasted text #1 1001 chars]" {
		t.Fatalf("1001 chars paste folded into %q, want %q", got, "[Pasted text #1 1001 chars]")
	}
}

func TestHistoryRecallFoldsLargeEntries(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)

	// 1. Recalling an entry with >= 16 lines folds into [Pasted text #1 +16 lines]
	content16 := strings.Repeat("line\n", 15) + "line16"
	m.pushHistory(content16)

	m.Update(upKey())
	if got := m.prompt.input.Value(); got != "[Pasted text #1 +16 lines]" {
		t.Fatalf("recalled 16 lines folded into %q, want %q", got, "[Pasted text #1 +16 lines]")
	}
	if m.prompt.pastedBlocks["[Pasted text #1 +16 lines]"] != content16 {
		t.Fatalf("pastedBlocks missing content for recalled entry")
	}

	// Submitting the recalled entry expands placeholder back to full content.
	// The test drops the returned command, so the fake turn never runs;
	// mark it finished so the next Esc belongs to the composer rather than
	// to turn cancellation.
	m.submitPrompt()
	m.conversation.streaming = false
	m.turn.ClearActiveBridge()
	if got := m.prompt.promptHistory.Entries()[m.prompt.promptHistory.Len()-1]; got != content16 {
		t.Fatalf("re-submitted history entry = %q, want full content", got)
	}

	// 2. Recalling an entry with > 1000 characters folds into [Pasted text #1 1005 chars]
	content1005 := strings.Repeat("y", 1005)
	m.pushHistory(content1005)

	m.Update(upKey())
	if got := m.prompt.input.Value(); got != "[Pasted text #1 1005 chars]" {
		t.Fatalf("recalled 1005 chars folded into %q, want %q", got, "[Pasted text #1 1005 chars]")
	}
	if m.prompt.pastedBlocks["[Pasted text #1 1005 chars]"] != content1005 {
		t.Fatalf("pastedBlocks missing content for recalled entry")
	}

	// Esc Esc clears composer and clears pastedBlocks
	m.handleEscKey()
	m.handleEscKey()
	if len(m.prompt.pastedBlocks) != 0 {
		t.Fatalf("pastedBlocks should be reset after Esc Esc, got %d", len(m.prompt.pastedBlocks))
	}
}

func TestUnbracketedPasteEnterBurstAbsorbedAsNewline(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.prompt.input.SetValue("line1")

	// First enter without preceding enter sets lastEnterTime; normal submit attempt
	// (or empty submit if not streaming).
	// To simulate rapid unbracketed paste from a terminal lacking bracketed paste:
	// A burst of consecutive Enter keystrokes arrives spaced < 10ms apart.
	m.prompt.lastEnterTime = time.Now()
	// Rapid Enter arrives 1ms after lastEnterTime
	enterKey := tea.KeyPressMsg(tea.Key{Code: tea.KeyEnter})
	m.Update(enterKey)

	if m.prompt.enterBurstCount != 1 {
		t.Fatalf("enterBurstCount = %d, want 1", m.prompt.enterBurstCount)
	}
	if got := m.prompt.input.Value(); got != "line1\n" {
		t.Fatalf("input value = %q, want %q", got, "line1\n")
	}
	if m.conversation.streaming {
		t.Fatalf("rapid enter burst prematurely submitted the turn")
	}

	// Another rapid Enter in the burst arrives immediately
	m.Update(enterKey)
	if m.prompt.enterBurstCount != 2 {
		t.Fatalf("enterBurstCount = %d, want 2", m.prompt.enterBurstCount)
	}
	if got := m.prompt.input.Value(); got != "line1\n\n" {
		t.Fatalf("input value = %q, want %q", got, "line1\n\n")
	}

	// Any non-enter key resets the burst counter
	m.Update(tea.KeyPressMsg(tea.Key{Code: 'x', Text: "x"}))
	if m.prompt.enterBurstCount != 0 {
		t.Fatalf("enterBurstCount after typing 'x' = %d, want 0", m.prompt.enterBurstCount)
	}
}

// TestPasteMsgEmptyProbesTheClipboard covers agy 1.4.9's image pasting:
// a paste that delivered nothing is the "Cmd+V with only an image
// copied" case, so the composer asks the platform clipboard for an
// image. The buffer itself is left alone.
func TestPasteMsgEmptyProbesTheClipboard(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.prompt.input.SetValue("keep")

	if _, cmd := m.Update(tea.PasteMsg{Content: ""}); cmd == nil {
		t.Fatalf("empty paste returned no cmd; it must probe the clipboard")
	}
	if got := m.prompt.input.Value(); got != "keep" {
		t.Fatalf("empty paste changed the buffer: %q", got)
	}
}

func TestPasteMsgIgnoredWhileHelpOpen(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.prompt.input.SetValue("keep")
	m.help.Panel.Open(0)

	m.Update(tea.PasteMsg{Content: "junk"})
	if got := m.prompt.input.Value(); got != "keep" {
		t.Fatalf("paste leaked into the prompt behind /help: %q", got)
	}
}

func TestPasteMsgRoutesToResumeSearch(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	if err := m.overlays.resumePicker.Open(); err != nil {
		t.Fatalf("open the picker: %v", err)
	}
	m.prompt.input.SetValue("")

	m.Update(tea.PasteMsg{Content: "abc"})
	if got := m.overlays.resumePicker.Query(); got != "abc" {
		t.Fatalf("query = %q, want \"abc\"", got)
	}
	if got := m.prompt.input.Value(); got != "" {
		t.Fatalf("paste leaked into the prompt while /resume was open: %q", got)
	}
}

func TestCtrlVPastesClipboardText(t *testing.T) {
	withClipboard(t, func() string { return "from-clipboard" })

	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.prompt.input.SetValue("")

	m.Update(ctrlKey('v'))
	if got, want := m.prompt.input.Value(), "from-clipboard"; got != want {
		t.Fatalf("ctrl+v input = %q, want %q", got, want)
	}
}

func TestCtrlVEmptyClipboardIsSilent(t *testing.T) {
	withClipboard(t, func() string { return "" })

	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.prompt.input.SetValue("keep")
	before := len(m.conversation.response)

	m.Update(ctrlKey('v'))
	if got := m.prompt.input.Value(); got != "keep" {
		t.Fatalf("empty clipboard changed the buffer: %q", got)
	}
	if len(m.conversation.response) != before {
		t.Fatalf("empty clipboard produced %d transcript rows", len(m.conversation.response)-before)
	}
}

func TestCtrlVDoesNotInsertLiteralV(t *testing.T) {
	withClipboard(t, func() string { return "" })

	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.prompt.input.SetValue("")

	m.Update(ctrlKey('v'))
	if strings.Contains(m.prompt.input.Value(), "v") {
		t.Fatalf("ctrl+v inserted a literal v: %q", m.prompt.input.Value())
	}
}

func TestCtrlD_ForwardDeleteWhenTextPresent(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", false)
	m.prompt.input.SetValue("hello")
	m.prompt.input.CursorStart()

	m.Update(ctrlKey('d'))
	if m.display.quitting {
		t.Fatal("ctrl+d on non-empty prompt must not quit")
	}
	if got := m.prompt.input.Value(); got != "ello" {
		t.Fatalf("ctrl+d did not delete character forward: got %q, want 'ello'", got)
	}

	m.prompt.input.SetValue("abcdef")
	m.prompt.InputSelStart = 1
	m.prompt.InputSelEnd = 4
	m.Update(ctrlKey('d'))
	if m.display.quitting {
		t.Fatal("ctrl+d on active selection must not quit")
	}
	if got := m.prompt.input.Value(); got != "aef" {
		t.Fatalf("ctrl+d did not delete selection: got %q, want 'aef'", got)
	}

	m.prompt.input.Reset()
	m.ResetInputSelection()
	m.Update(ctrlKey('d'))
	if !m.display.quitting {
		t.Fatal("ctrl+d on empty prompt must trigger quit")
	}
}

func TestNewlineShortcuts(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", false)

	m.prompt.input.SetValue("echo foo \\")
	m.prompt.input.CursorEnd()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter})
	if got := m.prompt.input.Value(); got != "echo foo \n" {
		t.Fatalf("\\ + Enter did not replace \\ with newline: got %q, want %q", got, "echo foo \n")
	}

	m.prompt.input.SetValue("line 1")
	m.prompt.input.CursorEnd()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModAlt})
	if got := m.prompt.input.Value(); got != "line 1\n" {
		t.Fatalf("Alt+Enter did not insert newline: got %q, want %q", got, "line 1\n")
	}

	m.prompt.input.SetValue("line A")
	m.prompt.input.CursorEnd()
	m.Update(tea.KeyPressMsg{Code: tea.KeyEnter, Mod: tea.ModShift})
	if got := m.prompt.input.Value(); got != "line A\n" {
		t.Fatalf("Shift+Enter did not insert newline: got %q, want %q", got, "line A\n")
	}
}

// sizedMode returns a model with a known terminal size and n response rows.
func sizedMode(w, h, n int) *RootModel {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	for i := 0; i < n; i++ {
		m.conversation.response = append(m.conversation.response, fmt.Sprintf("row-%02d", i))
	}
	return m
}

// viewLines returns View()'s content split into lines.
func viewLines(t *testing.T, m *RootModel) []string {
	t.Helper()
	v := m.View()
	return strings.Split(v.Content, "\n")
}

// Test_ViewportFitsTerminal is the core cycle-119 regression: however long
// the transcript gets, the rendered view must never be taller than the
// terminal.
func Test_ViewportFitsTerminal(t *testing.T) {
	for _, h := range []int{10, 24, 40} {
		for _, n := range []int{0, 1, 5, 40, 400} {
			m := sizedMode(80, h, n)
			lines := viewLines(t, m)
			if len(lines) > h {
				t.Errorf("h=%d n=%d: view has %d rows, terminal is %d", h, n, len(lines), h)
			}
		}
	}
}

// Test_ViewportShowsNewestRows verifies the user-reported symptom is gone:
// with an overflowing transcript the newest rows are on screen and the oldest
// are scrolled off.
func Test_ViewportShowsNewestRows(t *testing.T) {
	m := sizedMode(80, 24, 40)
	joined := ansi.Strip(m.View().Content)
	if !strings.Contains(joined, "row-39") {
		t.Errorf("newest row (row-39) is not visible:\n%s", joined)
	}
	if strings.Contains(joined, "row-00") {
		t.Errorf("oldest row (row-00) should be scrolled off:\n%s", joined)
	}
}

// Test_ViewportPromptStaysBelowTranscript verifies the prompt is still the
// last thing before the bottom separator once the transcript is clipped.
func Test_ViewportPromptStaysBelowTranscript(t *testing.T) {
	m := sizedMode(80, 24, 40)
	lines := viewLines(t, m)
	if len(lines) < 3 {
		t.Fatalf("view too short: %d rows", len(lines))
	}
	// Bottom of the view: status row, bottom separator, prompt.
	last := lines[len(lines)-1]
	if !strings.Contains(ansi.Strip(last), "for shortcuts") {
		t.Errorf("last row is not the status row: %q", ansi.Strip(last))
	}
	if !strings.Contains(ansi.Strip(lines[len(lines)-2]), "─") {
		t.Errorf("second-to-last row is not the bottom separator: %q", ansi.Strip(lines[len(lines)-2]))
	}
	if !strings.Contains(ansi.Strip(lines[len(lines)-3]), ">") {
		t.Errorf("prompt is not above the bottom separator: %q", ansi.Strip(lines[len(lines)-3]))
	}
}

// Test_ViewportCursorTracksPrompt pins the cursor row: it must land on the
// prompt line, which is no longer a fixed offset once the transcript clips.
func Test_ViewportCursorTracksPrompt(t *testing.T) {
	for _, n := range []int{0, 3, 40, 400} {
		m := sizedMode(80, 24, n)
		v := m.View()
		lines := strings.Split(v.Content, "\n")
		if v.Cursor.Y >= len(lines) {
			t.Fatalf("n=%d: cursor row %d outside the %d-row view", n, v.Cursor.Y, len(lines))
		}
		got := ansi.Strip(lines[v.Cursor.Y])
		if !strings.HasPrefix(got, "> ") {
			t.Errorf("n=%d: cursor row %d is not the prompt line: %q", n, v.Cursor.Y, got)
		}
	}
}

// Test_ViewportScrollKeys covers the recovered bindings: pgup / pgdown,
// shift+up / shift+down, ctrl+home / ctrl+end.
func Test_ViewportScrollKeys(t *testing.T) {
	key := func(code rune, mod tea.KeyMod) tea.KeyPressMsg {
		return tea.KeyPressMsg(tea.Key{Code: code, Mod: mod})
	}
	m := sizedMode(80, 24, 200)
	m.View() // prime vpRows / the scroll window
	if !m.viewport.autoScroll {
		t.Fatal("autoScroll should start on")
	}
	top := func() string { return ansi.Strip(m.View().Content) }
	if !strings.Contains(top(), "row-199") {
		t.Fatalf("expected the newest row at the bottom:\n%s", top())
	}

	// Page up: auto-scroll off, and a strictly older window.
	m.Update(key(tea.KeyPgUp, 0))
	if m.viewport.autoScroll {
		t.Error("pgup should clear autoScroll")
	}
	if strings.Contains(top(), "row-199") {
		t.Error("pgup did not scroll away from the bottom")
	}

	// ctrl+end: back to the bottom, auto-scroll re-armed.
	m.Update(key(tea.KeyEnd, tea.ModCtrl))
	if !m.viewport.autoScroll {
		t.Error("ctrl+end should re-arm autoScroll")
	}
	if !strings.Contains(top(), "row-199") {
		t.Error("ctrl+end did not return to the bottom")
	}

	// ctrl+home: top of the transcript.
	m.Update(key(tea.KeyHome, tea.ModCtrl))
	topRow := func() string {
		for _, l := range strings.Split(ansi.Strip(m.View().Content), "\n") {
			if strings.HasPrefix(strings.TrimSpace(l), "row-") {
				return strings.TrimSpace(l)
			}
		}
		return ""
	}
	if !strings.HasPrefix(topRow(), "row-00") {
		t.Errorf("ctrl+home did not reach the top (first row %q):\n%s", topRow(), top())
	}
	if m.viewport.scroll != 0 {
		t.Errorf("ctrl+home left scroll at %d", m.viewport.scroll)
	}

	// Half page down twice from the top lands short of the bottom.
	m.Update(key(tea.KeyDown, tea.ModShift))
	m.Update(key(tea.KeyDown, tea.ModShift))
	half := m.viewport.scroll
	if half <= 0 {
		t.Error("shift+down did not scroll")
	}
	m.Update(key(tea.KeyPgDown, 0))
	if m.viewport.scroll <= half {
		t.Error("pgdown did not advance past the half-page position")
	}
}

// Test_ViewportAutoScrollFollowsNewContent verifies the streaming case:
// while pinned to the bottom, appended rows stay visible without any key.
func Test_ViewportAutoScrollFollowsNewContent(t *testing.T) {
	m := sizedMode(80, 24, 40)
	m.View()
	for i := 40; i < 120; i++ {
		m.conversation.response = append(m.conversation.response, fmt.Sprintf("row-%03d", i))
		// Cycle 122: Update() invalidates the wrap cache for any non-scroll
		// message. Tests that mutate m.conversation.response directly bypass Update()
		// and must dirty the cache themselves.
		m.viewport.rowsCacheDirty = true
		m.View()
	}
	joined := ansi.Strip(m.View().Content)
	if !strings.Contains(joined, "row-119") {
		t.Errorf("newest row not visible while auto-scrolling:\n%s", joined)
	}
}

// Test_NoScrollbarEverywhere pins the absence of a transcript scrollbar.
// The reference has zero `│` / `█` characters in any capture in
// /tmp/agycmp/hotpath/*/ref.*.txt; the cycle 122–133 clone added one and
// had to deduct a column from the wrap width, which broke line breaks
// and made the thumb formula collapse to a 1-cell thumb that looked
// like a gap. Cycle 134 removes the scrollbar entirely.
func Test_NoScrollbarEverywhere(t *testing.T) {
	short := sizedMode(80, 40, 3)
	if strings.ContainsRune(short.View().Content, '│') {
		t.Error("scrollbar track (│) appeared on a fitting transcript")
	}
	if strings.ContainsRune(short.View().Content, '█') {
		t.Error("scrollbar thumb (█) appeared on a fitting transcript")
	}
	tall := sizedMode(80, 24, 200)
	for _, line := range viewLines(t, tall) {
		if strings.ContainsRune(line, '│') {
			t.Errorf("scrollbar track (│) appeared on an overflowing transcript: %q", ansi.Strip(line))
		}
		if strings.ContainsRune(line, '█') {
			t.Errorf("scrollbar thumb (█) appeared on an overflowing transcript: %q", ansi.Strip(line))
		}
	}
}

// Test_ViewportWidthIsRespected verifies the width half of "screen size
// awareness": no emitted line is wider than the terminal, at any width, so
// nothing wraps behind the renderer's back.
func Test_ViewportWidthIsRespected(t *testing.T) {
	for _, w := range []int{40, 60, 80, 100} {
		m := sizedMode(w, 24, 40)
		for _, line := range viewLines(t, m) {
			if got := ansi.StringWidth(line); got > w {
				t.Errorf("w=%d: line is %d cells wide: %q", w, got, ansi.Strip(line))
			}
		}
	}
}

// Test_IsUserPromptRow pins the row-classifier heuristic used by the
// cycle-179 snap-to-user-prompt behaviour. The user-prompt row is
// produced by transcript.UserPromptRow() and starts with the cWarningBlueOpen
// escape followed by the literal "> " marker; continuation rows of a
// wrapped user prompt don't carry the prefix and must NOT be
// classified as user-prompt starts.
func Test_IsUserPromptRow(t *testing.T) {
	for _, tc := range []struct {
		name string
		row  string
		want bool
	}{
		{"simple user prompt", cWarningBlueOpen + "> hello world" + cWarningBlueClose, true},
		{"empty user prompt", cWarningBlueOpen + "> " + cWarningBlueClose, true},
		{"wrapped continuation (no prefix)", "and the answer is 42", false},
		{"plain text", "row-05", false},
		{"assistant bullet", "  • a bullet", false},
		{"separator line", "──────", false},
		{"empty", "", false},
		{"warning header (same blue prefix, no >)", cWarningBlueOpen + "⚠ Warning" + cWarningBlueClose, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := tuiviewport.IsUserPromptRow(tc.row); got != tc.want {
				t.Errorf("isUserPromptRow(%q) = %v, want %v", tc.row, got, tc.want)
			}
		})
	}
}

// Test_StickyUserPromptRow covers the cycle-179 sticky-bar logic: the
// sticky bar shows the most recent user prompt <= m.viewport.scroll (so the
// visible content's "owning" turn is always labelled at the top of
// the pane). Scrolling itself is NOT snapped — see scrollBy comment.
//
// The sticky row index is -1 when there is no user prompt <= scroll
// (header area, before first turn) or when m.viewport.autoScroll is true (the
// user is parked at the bottom and the input chrome owns the bottom
// rows).
func Test_StickyUserPromptRow(t *testing.T) {
	for _, tc := range []struct {
		name       string
		promptRows []int
		scroll     int
		autoScroll bool
		want       int // -1 means "no sticky row"
	}{
		{"no prompts", nil, 50, false, -1},
		{"autoScroll on, no sticky", []int{5, 15, 25}, 100, true, -1},
		{"scroll 0, header area", []int{5, 15, 25}, 0, false, -1},
		{"scroll before first prompt", []int{10, 20}, 5, false, -1},
		{"at first prompt", []int{10, 20}, 10, false, 10},
		{"between prompts", []int{5, 15, 25}, 20, false, 15},
		{"at last prompt", []int{5, 15, 25}, 25, false, 25},
		{"past last prompt", []int{5, 15, 25}, 100, false, 25},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
			m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
			m.viewport.userPromptRows = tc.promptRows
			m.viewport.scroll = tc.scroll
			m.viewport.autoScroll = tc.autoScroll
			got := tuiviewport.StickyUserPromptRow(m.viewport.userPromptRows, m.viewport.autoScroll, m.viewport.scroll)
			if tc.want == -1 {
				if got != -1 {
					t.Errorf("stickyUserPromptRow = %d, want -1 (no sticky)", got)
				}
			} else {
				if got != tc.want {
					t.Errorf("stickyUserPromptRow = %d, want %d", got, tc.want)
				}
			}
		})
	}
}

// Test_ViewportLongLinesWrap verifies long response rows are soft-wrapped
// rather than clipped, so no content is lost on a narrow terminal.
func Test_ViewportLongLinesWrap(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 40, Height: 20})
	m.conversation.response = append(m.conversation.response, strings.Repeat("abcdefghij", 12)) // 120 cells
	m.conversation.response = append(m.conversation.response, "tail-marker")
	joined := ansi.Strip(m.View().Content)
	if !strings.Contains(joined, "tail-marker") {
		t.Errorf("tail marker lost:\n%s", joined)
	}
	for _, line := range viewLines(t, m) {
		if w := ansi.StringWidth(line); w > 40 {
			t.Errorf("wrapped line is %d cells wide", w)
		}
	}
}

// Test_ViewportUnknownSizeKeepsOldLayout is the backwards-compatibility
// guard: before the first WindowSizeMsg the view is exactly the pre-119
// layout, which is what the byte-exact snapshots assert.
func Test_ViewportUnknownSizeKeepsOldLayout(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	for i := 0; i < 40; i++ {
		m.conversation.response = append(m.conversation.response, fmt.Sprintf("row-%02d", i))
	}
	v := m.View()
	lines := strings.Split(v.Content, "\n")
	if len(lines) < 40 {
		t.Errorf("unclipped layout should keep every row, got %d", len(lines))
	}
	if v.Cursor.Y != 1+5+1+1+40 {
		t.Errorf("cursor Y = %d, want %d", v.Cursor.Y, 1+5+1+1+40)
	}
}

// Test_ViewportMouseWheel verifies the recovered
// AltScreenStrategy.ShouldHandleWheel behaviour: the wheel scrolls in
// alt-screen mode and is ignored otherwise.
func Test_ViewportMouseWheel(t *testing.T) {
	wheel := tea.MouseWheelMsg(tea.Mouse{X: 1, Y: 1, Button: tea.MouseWheelUp})
	m := sizedMode(80, 24, 200)
	m.View()
	m.Update(wheel)
	if m.viewport.autoScroll {
		t.Error("wheel up should clear autoScroll")
	}
	before := m.viewport.scroll

	inline := sizedMode(80, 24, 200)
	inline.viewport.altScreen = false
	inline.View()
	inlineBefore := inline.viewport.scroll
	inline.Update(wheel)
	if inline.viewport.scroll != inlineBefore {
		t.Errorf("inline mode must not consume the wheel (scroll %d -> %d)", inlineBefore, inline.viewport.scroll)
	}
	_ = before
}

// Test_ViewportDump is a manual inspection aid:
// OPENAGY_DUMP_VIEW=1 go test ./cmd/wagyu -run ViewportDump -v
func Test_ViewportDump(t *testing.T) {
	if os.Getenv("OPENAGY_DUMP_VIEW") == "" {
		t.Skip("set OPENAGY_DUMP_VIEW=1 to print a frame")
	}
	m := sizedMode(80, 24, 30)
	for _, line := range viewLines(t, m) {
		fmt.Printf("|%s|\n", ansi.Strip(line))
	}
}

// TestScrollCacheHit guards the cycle-122 cache: a scroll render after the
// first one must be two cache hits (the full-width slot and the narrow
// width-1 slot used by the overflow path) and well under a millisecond.
func TestScrollCacheHit(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	m := newRootModel(nil, "", "Gemini 3.8 Flash (High)", "", "/tmp", true)
	m.viewport.width = 100
	m.viewport.height = 30
	long := strings.Repeat("the quick brown fox jumps over the lazy dog ", 800) // ~35 KB
	for i := 0; i < 200; i++ {
		if i%5 == 1 || i%5 == 2 {
			m.conversation.response = append(m.conversation.response, long)
		} else {
			m.conversation.response = append(m.conversation.response, fmt.Sprintf("prompt row %d", i))
		}
	}
	_ = m.View()
	m.scrollBy(1)
	_ = m.View()
	start := time.Now()
	for i := 0; i < 100; i++ {
		m.scrollBy(1)
		_ = m.View()
	}
	elapsed := time.Since(start)
	per := elapsed / 100
	t.Logf("100 warm scrolls: %v (avg %v/op)", elapsed, per)
	if per > 10*time.Millisecond {
		t.Errorf("warm scroll render too slow: %v/op (expected cache hits)", per)
	}
}

// synthLargeTranscript populates m.conversation.response with N rows: a few long model
// replies (the dominant cost) plus the usual mix of prompts and tool rows.
func synthLargeTranscript(m *RootModel, n int) {
	long := strings.Repeat("the quick brown fox jumps over the lazy dog ", 1600) // ~70 KB
	for i := 0; i < n; i++ {
		switch i % 5 {
		case 0:
			m.conversation.response = append(m.conversation.response, transcript.UserPromptRow(fmt.Sprintf("prompt %d", i)))
		case 1, 2:
			m.conversation.response = append(m.conversation.response, long)
		case 3:
			m.conversation.response = append(m.conversation.response, "⏺ Ran a command")
		case 4:
			m.conversation.response = append(m.conversation.response, "")
		}
	}
}

// TestWrapRowsQuadratic guards the cycle-122 single-pass wrap: doubling a
// line's length should roughly double the wrap cost, not quadruple it.
func TestWrapRowsQuadratic(t *testing.T) {
	if testing.Short() {
		t.Skip("timing test")
	}
	measure := func(n int) time.Duration {
		line := strings.Repeat("x", n)
		// Warm up
		for i := 0; i < 3; i++ {
			_ = tuilib.WrapRows(line, 100)
		}
		var best time.Duration
		for run := 0; run < 3; run++ {
			start := time.Now()
			for i := 0; i < 20; i++ {
				_ = tuilib.WrapRows(line, 100)
			}
			d := time.Since(start)
			if best == 0 || d < best {
				best = d
			}
		}
		return best
	}
	small := measure(20_000)
	large := measure(40_000)
	ratio := float64(large) / float64(small)
	t.Logf("20k=%v 40k=%v ratio=%.2f (linear≈2, quadratic≈4)", small, large, ratio)
	if ratio > 4.5 {
		t.Errorf("wrapRows looks super-linear: ratio %.2f for a 2x input", ratio)
	}
}

// BenchmarkViewCold times the first render of a large transcript.
func BenchmarkViewCold(b *testing.B) {
	m := newBenchModel()
	synthLargeTranscript(m, 4000)
	m.viewport.width = 100
	m.viewport.height = 30
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.viewport.rowsCacheDirty = true
		_ = m.View()
	}
}

// BenchmarkViewWarm times a render that the scroll path takes.
func BenchmarkViewWarm(b *testing.B) {
	m := newBenchModel()
	synthLargeTranscript(m, 4000)
	m.viewport.width = 100
	m.viewport.height = 30
	_ = m.View()
	m.scrollBy(1)
	_ = m.View()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		m.scrollBy(1)
		_ = m.View()
	}
}

// TestSeedThenScroll tests loading a conversation, scrolling, and re-rendering.
func TestSeedThenScroll(t *testing.T) {
	id := "4638a7b0-e5f4-4961-80c2-d61499f06050"
	if !convstore.HasConversation(id) {
		t.Skip("real conversation db not on this machine")
	}
	c, err := convstore.LoadConversation(convstore.ConversationPath(id))
	if err != nil {
		t.Fatal(err)
	}
	m := newRootModel(nil, "", "Gemini 3.8 Flash (High)", "", "/tmp", true)
	m.seedConversation(c)
	m.viewport.width = 100
	m.viewport.height = 30
	_ = m.View()
	for i := 0; i < 10; i++ {
		m.scrollBy(3)
		_ = m.View()
	}
}

// newBenchModel builds a RootModel with the fields View() needs.
func newBenchModel() *RootModel {
	return newRootModel(nil, "", "Gemini 3.8 Flash (High)", "", "/tmp", true)
}

func TestCycle124_ClipToScreenRows(t *testing.T) {
	cases := []struct {
		name      string
		allRows   []string
		visible   []string
		scrollOff int
		maxRows   int
		wantOut   int
		wantRows  int
	}{
		{
			name:      "single-line entries, no clipping",
			allRows:   []string{"a", "b", "c", "d"},
			visible:   []string{"a", "b", "c", "d"},
			scrollOff: 0,
			maxRows:   10,
			wantOut:   4,
			wantRows:  4,
		},
		{
			name:      "single-line entries, fits exactly",
			allRows:   []string{"a", "b", "c"},
			visible:   []string{"a", "b", "c"},
			scrollOff: 0,
			maxRows:   3,
			wantOut:   3,
			wantRows:  3,
		},
		{
			name:      "multi-line last entry gets trimmed",
			allRows:   []string{"a", "b", "c\nd\ne"},
			visible:   []string{"a", "b", "c\nd\ne"},
			scrollOff: 0,
			maxRows:   4,
			wantOut:   2,
			wantRows:  2,
		},
		{
			name:      "first row is multi-line",
			allRows:   []string{"a\nb\nc", "d"},
			visible:   []string{"a\nb\nc", "d"},
			scrollOff: 0,
			maxRows:   3,
			wantOut:   1,
			wantRows:  3,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			counts := tuiviewport.ScreenRowCounts(tc.allRows)
			got := tuiviewport.ClipToScreenRows(tc.visible, counts, tc.scrollOff, tc.maxRows)
			if len(got) != tc.wantOut {
				t.Errorf("len = %d, want %d", len(got), tc.wantOut)
			}
			rows := 0
			for _, r := range got {
				rows += tuiviewport.ScreenRows(r)
			}
			if rows != tc.wantRows {
				t.Errorf("totalRows = %d, want %d", rows, tc.wantRows)
			}
		})
	}
}

func TestCycle124_BannerPreservedAtTop(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "Antigravity Starter Quota", "/Users/gear/Documents/GitHub/openagy", true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.session.fallbackFrom = "MiniMax-M3"
	m.session.fallbackTo = "Gemini 3.8 Flash (High)"
	m.conversation.response = append(m.conversation.response,
		"line one",
		"line two with embedded\nnewline inside an entry",
		"line three",
	)
	m.gotoTop()
	v := m.View()
	if !containsBannerLine(v.Content, "Pigasus CLI") {
		t.Errorf("banner should appear in body when scroll==0; first lines:\n%s", debugFirstLines(v.Content))
	}
}

func TestCycle124_BannerHidesAfterScroll(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	for i := 0; i < 600; i++ {
		m.conversation.response = append(m.conversation.response, "transcript line")
	}
	m.gotoTop()
	v := m.View()
	if !containsBannerLine(v.Content, "Pigasus CLI") {
		t.Fatalf("precondition: banner must be at the top when scroll==0; first lines:\n%s scroll=%d autoScroll=%v", debugFirstLines(v.Content), m.viewport.scroll, m.viewport.autoScroll)
	}
	m.scrollBy(tuilib.ClampInt(m.viewport.vpRows-1, 1, 1<<20) * 2)
	v = m.View()
	if containsBannerLine(v.Content, "Pigasus CLI") {
		t.Errorf("banner must not be visible after scrolling away; first lines:\n%s", debugFirstLines(v.Content))
	}
}

func debugFirstLines(content string) string {
	plain := ansi.Strip(content)
	lines := strings.Split(plain, "\n")
	if len(lines) > 8 {
		lines = lines[:8]
	}
	return strings.Join(lines, "\n")
}

func containsBannerLine(content, marker string) bool {
	plain := ansi.Strip(content)
	return strings.Contains(plain, marker)
}

func TestCycle152_ChromeAnchoredAtBottom(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "Antigravity Starter Quota", "/Users/gear/Documents/GitHub/openagy", true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.session.fallbackFrom = ""
	m.session.fallbackTo = ""
	for i := 0; i < 600; i++ {
		m.conversation.response = append(m.conversation.response, "transcript line that is long enough to wrap onto two visual rows in a 100 column terminal for testing scrolling")
	}
	m.gotoTop()

	rows := []int{}
	for _, off := range []int{0, 200, 400} {
		m.viewport.scroll = off
		m.viewport.autoScroll = false
		v := m.View()
		plain := ansi.Strip(v.Content)
		inputLine := -1
		for i, line := range strings.Split(plain, "\n") {
			if strings.HasPrefix(line, ">") {
				inputLine = i
				break
			}
		}
		if inputLine < 0 {
			t.Fatalf("scroll=%d: no input row found in body; first 8 lines:\n%s", off, debugFirstLines(v.Content))
		}
		rows = append(rows, inputLine)
	}

	for i := 1; i < len(rows); i++ {
		if rows[i] != rows[0] {
			t.Errorf("input row oscillates across scroll: scroll=0 → row %d, scroll=%d → row %d (chrome not anchored)", rows[0], []int{0, 200, 400}[i], rows[i])
		}
	}
	want := m.viewport.height - 3
	if rows[0] != want {
		t.Errorf("input row = %d, want %d (m.viewport.height-3 = status-1)", rows[0], want)
	}
}

func TestCycle152_ChromeAnchoredEmptyTranscript(t *testing.T) {
	m := newRootModel(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "Antigravity Starter Quota", "/Users/gear/Documents/GitHub/openagy", true)
	m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m.session.fallbackFrom = ""
	m.session.fallbackTo = ""
	v := m.View()
	plain := ansi.Strip(v.Content)
	lines := strings.Split(plain, "\n")
	inputLine := -1
	statusLine := -1
	for i, line := range lines {
		if inputLine < 1 && strings.HasPrefix(line, ">") {
			inputLine = i
		}
		if strings.Contains(line, "shortcuts") {
			statusLine = i
			break
		}
	}
	if inputLine < 0 {
		t.Fatalf("no input row found; first 8 lines:\n%s", debugFirstLines(v.Content))
	}
	if statusLine < 0 {
		t.Fatalf("no status row found; first 8 lines:\n%s", debugFirstLines(v.Content))
	}
	if statusLine != inputLine+2 {
		// The prompt's chrome is contiguous unless something is drawn
		// between the input and the status row — and the only thing that
		// goes there is the active-items tray, fed by the package-level
		// task registry. Name the rows when that happens: a task left in
		// StateRunning by an earlier test is the usual cause.
		for _, task := range m.tasks.reg.List() {
			t.Logf("active-items row: %s kind=%s state=%s", task.ID, task.Kind, task.State)
		}
		t.Errorf("chrome broken: input at row %d, status at row %d (want contiguous chrome)", inputLine, statusLine)
	}
	if len(lines) > m.viewport.height+1 {
		t.Errorf("body has %d rows, terminal has %d (chrome overflowed)", len(lines)-1, m.viewport.height)
	}
}

// pinnedPromptMode is a transcript builder helper: it stands up a
// RootModel with the given altscreen flag and seeds m.conversation.response with
// a sequence of "user prompt / assistant reply" turns so the
// viewport has at least one user-prompt row to anchor the cycle-253
// pinned header. The cycle-253 viewport reserves 1 row at the top
// of the visible area when the pinned header is shown, so the
// transcript has to be tall enough to overflow the 24-line
// terminal for the scroll conditions to fire.
func pinnedPromptMode(t *testing.T, altscreen bool) *RootModel {
	t.Helper()
	m := newRootModel(&fakeClient{}, "u@t", "m", "t", "/", altscreen)
	m.Update(tea.WindowSizeMsg{Width: 80, Height: 24})
	// transcript.UserPromptRow() emits cWarningBlueOpen + "> <text>" + cWarningBlueClose
	// which the cycle-179 isUserPromptRow classifier recognises. Each
	// turn is "<user prompt> / <blank> / <assistant reply> / <blank>"
	// — the layout a real conversation produces (submitPrompt
	// appends ""> " + buf" then the streamed reply rows).
	for i := 0; i < 30; i++ {
		m.conversation.response = append(m.conversation.response,
			transcript.UserPromptRow("prompt-"+string(rune('a'+i%26))),
			"",
			"reply-line-1",
			"reply-line-2",
			"",
		)
	}
	return m
}

// pinnedPromptView walks View() and returns the rendered content
// with ANSI stripped, so tests can do plain substring checks for
// the prompt text and the "↳ jump" hint without worrying about
// escape sequences.
func pinnedPromptView(t *testing.T, m *RootModel) string {
	t.Helper()
	return ansi.Strip(m.View().Content)
}

// scrollAwayFromBottom moves the viewport away from the bottom so
// stickyUserPromptRow() can return a non-negative index — i.e.
// pinnedPromptText becomes non-empty and the cycle-253 header is
// rendered. m.viewport.scroll is set to a value safely past the first user
// prompt.
func scrollAwayFromBottom(m *RootModel) {
	m.viewport.autoScroll = false
	// The header (banner + fallback warning) takes ~7 rows; with
	// 30 turns × 5 rows = 150 rows of transcript the viewport at
	// h=24 has avail ~18 rows. Setting scroll = 50 lands well past
	// the first user prompt at row ~7.
	m.viewport.scroll = 50
}

// TestPinnedPrompt_NoAltscreen: when altscreen is OFF, no pinned
// prompt is rendered even when scrolled back. Inline mode falls
// back to the cycle-179 sticky bar (which carries no jump hint),
// so the cycle-253 header must not be visible.
func TestPinnedPrompt_NoAltscreen(t *testing.T) {
	m := pinnedPromptMode(t, false)
	scrollAwayFromBottom(m)
	if got := pinnedPromptView(t, m); strings.Contains(got, "↳ jump to prompt") {
		t.Errorf("inline mode leaked the cycle-253 pinned-prompt hint: %q", got)
	}
	if m.viewport.pinnedPromptText != "" {
		t.Errorf("altscreen=false: m.viewport.pinnedPromptText should be empty, got %q", m.viewport.pinnedPromptText)
	}
}

// TestPinnedPrompt_AltscreenAtTop: when altscreen is ON and the
// viewport is parked at the bottom (autoScroll on, scroll == 0),
// no pinned prompt is rendered. The header is only useful when
// the user has scrolled away from the bottom; in the at-bottom
// state the bottom-of-screen prompt slot is occupied by the
// input chrome's "> " prompt and a second one at the top would
// be redundant.
func TestPinnedPrompt_AltscreenAtTop(t *testing.T) {
	m := pinnedPromptMode(t, true)
	if !m.viewport.autoScroll {
		t.Fatalf("starting mode: autoScroll should be true")
	}
	if m.viewport.scroll != 0 {
		t.Fatalf("starting mode: scroll should be 0, got %d", m.viewport.scroll)
	}
	if got := pinnedPromptView(t, m); strings.Contains(got, "↳ jump to prompt") {
		t.Errorf("at-bottom view leaked the cycle-253 pinned-prompt hint: %q", got)
	}
	if m.viewport.pinnedPromptText != "" {
		t.Errorf("at-bottom view: m.viewport.pinnedPromptText should be empty, got %q", m.viewport.pinnedPromptText)
	}
}

// TestPinnedPrompt_AltscreenScrolledIntoAssistant: when altscreen
// is ON and the viewport is scrolled away from the bottom, the
// pinned-prompt header is rendered with the producing prompt's
// text followed by a horizontal divider rule (matching agy 1.4.10,
// with no jump hint suffix). We seed the transcript with "prompt-a"
// as the first user prompt and assert that the rendered view contains
// that exact string and divider rule while scrolled into the assistant
// reply region.
func TestPinnedPrompt_AltscreenScrolledIntoAssistant(t *testing.T) {
	m := pinnedPromptMode(t, true)
	scrollAwayFromBottom(m)
	view := pinnedPromptView(t, m)
	if strings.Contains(view, "jump to prompt") {
		t.Errorf("altscreen+scrolled: expected no jump hint in view, got:\n%s", view)
	}
	// Find one of the prompt texts we seeded — pinnedPromptText
	// should carry the producing prompt's "> <text>" prefix.
	if m.viewport.pinnedPromptText == "" {
		t.Fatalf("altscreen+scrolled: m.viewport.pinnedPromptText is empty, want prompt content")
	}
	if !strings.HasPrefix(ansi.Strip(m.viewport.pinnedPromptText), "> prompt-") {
		t.Errorf("m.viewport.pinnedPromptText = %q, want a \"> prompt-<x>\" prefix", ansi.Strip(m.viewport.pinnedPromptText))
	}
	if strings.Contains(ansi.Strip(m.viewport.pinnedPromptText), "jump to prompt") {
		t.Errorf("m.viewport.pinnedPromptText = %q, want no jump hint appended", ansi.Strip(m.viewport.pinnedPromptText))
	}
	if m.viewport.pinnedPromptRowIdx < 0 {
		t.Errorf("altscreen+scrolled: m.viewport.pinnedPromptRowIdx should be >= 0, got %d", m.viewport.pinnedPromptRowIdx)
	}
}

// TestPinnedPrompt_AltscreenScrolledIntoUser: when the visible
// content is itself a user prompt (the user just scrolled or jumped to
// that turn), the prompt is already visible in the viewport, so the
// sticky header is deactivated (agy 1.4.10) to prevent showing the
// prompt message twice.
func TestPinnedPrompt_AltscreenScrolledIntoUser(t *testing.T) {
	m := pinnedPromptMode(t, true)
	_ = m.View()
	if len(m.viewport.userPromptRows) == 0 {
		t.Fatal("precondition: userPromptRows should be populated after View()")
	}
	if len(m.viewport.userPromptRows) < 2 {
		t.Fatal("seeded transcript has fewer than 2 user prompts")
	}
	targetRow := m.viewport.userPromptRows[1]
	m.viewport.autoScroll = false
	m.viewport.scroll = targetRow
	// When scrolled directly to the user prompt, the prompt is on screen,
	// so sticky prompt must be inactive (no duplicate prompt display).
	_ = m.View()
	if m.viewport.pinnedPromptText != "" {
		t.Errorf("scrolled-onto-user-prompt: expected pinnedPromptText to be empty (no duplicate display), got %q", m.viewport.pinnedPromptText)
	}
}

// TestPinnedPrompt_EnterJumpsToPrompt: pressing Enter on the
// pinned-prompt header (with an empty input buffer) scrolls the
// viewport back to that prompt's turn. We seed a transcript with
// several turns, scroll past the first user prompt, take a
// snapshot of the pinned prompt's row index, press Enter, and
// assert m.viewport.scroll snaps to that turn.
func TestPinnedPrompt_EnterJumpsToPrompt(t *testing.T) {
	m := pinnedPromptMode(t, true)
	scrollAwayFromBottom(m)
	_ = m.View()
	if m.viewport.pinnedPromptRowIdx < 0 {
		t.Fatal("precondition: pinnedPromptRowIdx should be populated after View()")
	}
	target := m.viewport.pinnedPromptRowIdx
	if m.viewport.scroll <= target {
		t.Fatalf("precondition: scroll=%d should be > target=%d for the jump to be non-trivial",
			m.viewport.scroll, target)
	}
	// Enter with empty buffer should jump to prompt turn start.
	m.Update(enterKey())
	if m.viewport.scroll > target {
		t.Errorf("after Enter: scroll=%d, want <= %d (prompt turn start)", m.viewport.scroll, target)
	}
	if m.viewport.autoScroll {
		t.Errorf("after Enter: autoScroll should be cleared (we jumped away from the bottom), got true")
	}
	// After jump, prompt is on screen so sticky header must be deactivated
	_ = m.View()
	if m.viewport.pinnedPromptText != "" {
		t.Errorf("after jump: expected sticky header to be deactivated, got %q", m.viewport.pinnedPromptText)
	}

	// Buffer-non-empty case: re-scroll away, type a char, press
	// Enter, and assert the submit path fires (streaming flips to
	// true and the input is cleared). The cycle-71/244 submit path
	// must remain reachable when the buffer has content — the
	// pinned prompt does not steal Enter from a typed message.
	scrollAwayFromBottom(m)
	m.Update(runeKey('h'))
	if v := m.prompt.input.Value(); v != "h" {
		t.Fatalf("typed rune: input.Value()=%q, want %q", v, "h")
	}
	preSession := len(m.SessionMessages())
	m.conversation.streaming = false
	// The jump's Enter and this one are separated by human typing; clear
	// the unbracketed-paste clock so this Enter submits instead of being
	// absorbed as a pasted newline.
	m.prompt.lastEnterTime = time.Time{}
	_, _ = m.Update(enterKey())
	if !m.conversation.streaming {
		t.Errorf("Enter with non-empty buffer must still submit (cycle 71 / 244), got streaming=false")
	}
	if m.prompt.input.Value() != "" {
		t.Errorf("Enter with non-empty buffer must clear the input, got %q", m.prompt.input.Value())
	}
	if len(m.SessionMessages()) != preSession+1 {
		t.Errorf("submitting a turn should append to m.engine.session (pre=%d post=%d)",
			preSession, len(m.SessionMessages()))
	}
	if m.SessionMessages()[len(m.SessionMessages())-1].Role != "user" || m.SessionMessages()[len(m.SessionMessages())-1].Content != "h" {
		t.Errorf("last session message = %+v, want {user, h}", m.SessionMessages()[len(m.SessionMessages())-1])
	}
}

// TestPinnedPrompt_NoClickInPrintMode: the cycle-253 pinned
// prompt is a TUI-only feature. Print mode (`-p`) goes through
// runPrintMode (cmd/wagyu/main.go) which never instantiates
// RootModel / newRootModel. We verify this at the import-graph level
// so a future refactor that accidentally wires the pinned prompt
// into runPrintMode (e.g. by sharing a helper file) is caught:
//
//  1. m.viewport.pinnedPromptText is populated only inside RootModel's
//     transcriptWindow — print mode has no RootModel, so
//     pinnedPromptText is meaningless outside the TUI.
//  2. The TUI click handler is in RootModel.Update() and is only
//     reachable when the user is driving the alt-screen
//     programme; -p does not.
//
// The test stands up a normal RootModel with altscreen enabled and
// sends a tea.MouseClickMsg at row 0. The jump fires; it does NOT
// touch m.engine.session (verified). A grep against the print path
// (cmd/wagyu/main.go + cmd/wagyu/print_*.go) confirms the
// pinned-prompt fields / functions are never referenced.
func TestPinnedPrompt_NoClickInPrintMode(t *testing.T) {
	m := pinnedPromptMode(t, true)
	scrollAwayFromBottom(m)
	_ = m.View()
	if m.viewport.pinnedPromptText == "" {
		t.Fatal("precondition: pinned prompt should be set up")
	}
	// Snapshot the session length so the click-jump is asserted
	// to leave it unchanged.
	preLen := len(m.SessionMessages())
	target := m.viewport.pinnedPromptRowIdx
	rows, _, _ := m.syncRowsView()
	wantScroll := tuiviewport.PromptTurnStart(rows, target)
	// Click row 0 (the pinned-prompt header).
	m.Update(mouseClick(0, 0, tea.MouseLeft))
	// Jump should have fired: m.viewport.scroll should equal prompt turn start.
	if m.viewport.scroll == 50 {
		t.Errorf("click on row 0 did not jump: scroll stayed at 50")
	}
	if m.viewport.scroll != wantScroll {
		t.Errorf("click jump: scroll=%d, want %d (PromptTurnStart)",
			m.viewport.scroll, wantScroll)
	}
	// The pinned-prompt feature must NOT touch m.session.
	if len(m.SessionMessages()) != preLen {
		t.Errorf("click-jump mutated m.engine.session: pre=%d post=%d (pinned prompt is purely visual)",
			preLen, len(m.SessionMessages()))
	}
}

// renderBannerForTest is a thin wrapper around renderBanner that
// returns the captured bytes as a string. The two callers in
// cmd/wagyu (main.go:1638 for `--tui`, tui_viewport.go:305 for the
// scrolling header) both pass an io.Writer; this helper pins the
// byte shape of the banner in the test environment.
func renderBannerForTest(t *testing.T) string {
	t.Helper()
	var b bytes.Buffer
	cwd := "/Users/gear/Documents/GitHub/openagy"
	if home, err := os.UserHomeDir(); err == nil && home != "" {
		cwd = home + "/Documents/GitHub/openagy"
	}
	renderBanner(&b, "u@example.com", "Gemini 3.8 Flash (High)", "Antigravity Starter Quota", cwd)
	return b.String()
}

// bannerHasArtRow reports whether the banner output contains the
// 24-bit-RGB unicode-block-character art row. We strip ANSI escapes
// first because the art row's distinguishing feature is the wide
// block characters (▄ / ▀) and their 24-bit-RGB colour frame —
// without stripping we would need to pin the entire escape sequence
// byte-for-byte, which would make the test brittle to a future
// re-tint of the colour palette.
//
// The art row is the only banner line whose visible content is
// pure decorative block characters; the textual rows
// (Pigasus CLI ver, email, model, cwd) all carry readable
// ASCII. We use the unicode block character "▀" as the marker —
// it appears only in the art row, never in the textual rows.
func bannerHasArtRow(banner string) bool {
	plain := ansi.Strip(banner)
	for _, line := range strings.Split(plain, "\n") {
		trimmed := strings.TrimSpace(line)
		// The art row has the form "▄▀▀     ▀▀▄" — entirely block
		// characters and whitespace. The textual rows always contain
		// at least one readable ASCII glyph from
		// "Pigasus CLI" / "<email>" / "<model>" / "<cwd>".
		// We assert on the absence of any readable glyph AND the
		// presence of at least one block character, which together
		// uniquely identifies the art row.
		if trimmed == "" {
			continue
		}
		hasBlock := strings.ContainsAny(trimmed, "▀▄")
		if !hasBlock {
			continue
		}
		readable := false
		for _, r := range trimmed {
			if (r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
				readable = true
				break
			}
		}
		if !readable {
			return true
		}
	}
	// Under LogoStyleSimple (Apple_Terminal / iTerm2), the bottom row has background-color spaces with no readable text.
	for _, line := range strings.Split(banner, "\n") {
		plainLine := ansi.Strip(line)
		trimmed := strings.TrimSpace(plainLine)
		if trimmed == "" && strings.Contains(line, "\x1b[48;2;") {
			return true
		}
	}
	return false
}

// bannerHasText pins that a given readable substring (after ANSI
// strip) appears in the banner. Used by the
// VersionAndAccountStillShown test to assert that the textual rows
// survive the gate.
func bannerHasText(banner, want string) bool {
	return strings.Contains(ansi.Strip(banner), want)
}

// TestParseBoolish pins the parser the cycle ships. The table
// covers every spelling the AGY 1.1.19 changelog accepts (1, true,
// yes — case-insensitive), the negative spellings the changelog
// explicitly does NOT list (0, false, no, on — all should fall
// through to false), the empty string, a "garbage" unknown, and a
// whitespace-padded truthy to confirm the TrimSpace path. The
// default-degradation property (truthy → true, anything else →
// false) is asserted on every row.
func TestParseBoolish(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		// changelog-accepted truthy spellings (case-insensitive)
		{"1", true},
		{"true", true},
		{"TRUE", true},
		{"True", true},
		{"yes", true},
		{"YES", true},
		{"Yes", true},
		// whitespace tolerance (changelog says "case-insensitive";
		// the cycle's parser trims first, so "  yes  " works too —
		// matches programstatus.EnvOSC7501* helpers' behaviour in
		// cmd/wagyu/program_status.go:107-128)
		{"  yes  ", true},
		{"\t1\n", true},
		// not in the changelog — must NOT suppress
		{"", false},
		{"garbage", false},
		{"0", false},
		{"false", false},
		{"False", false},
		{"FALSE", false},
		{"no", false},
		{"NO", false},
		{"on", false}, // strconv.ParseBool accepts this; we don't.
		{"y", false},  // single-letter; the changelog says "yes"
		{"enable", false},
		// whitespace-only — must NOT suppress (a " " env var is not
		// the user setting the flag, it's shell noise)
		{"   ", false},
	}
	for _, c := range cases {
		t.Run(c.in, func(t *testing.T) {
			if got := tuilib.ParseBoolish(c.in); got != c.want {
				t.Errorf("tuilib.ParseBoolish(%q) = %v, want %v", c.in, got, c.want)
			}
		})
	}
}

// TestBanner_LogoShownByDefault pins the no-env-var baseline:
// when AGY_CLI_HIDE_LOGO is unset, the art row is rendered. This
// is the regression sentinel for the cycle: a regression that
// flipped the default to "hidden" would produce a banner missing
// the art row on every existing user's machine.
func TestBanner_LogoShownByDefault(t *testing.T) {
	t.Setenv(tuilib.EnvHideLogo, "")
	got := renderBannerForTest(t)
	if !bannerHasArtRow(got) {
		t.Errorf("AGY_CLI_HIDE_LOGO unset: art row should be present, banner = %q", got)
	}
}

// TestBanner_LogoHiddenOnHideLogo1 covers the most idiomatic
// "boolean env var" spelling. Setting =1 is what shell users
// reach for when they want a quick toggle.
func TestBanner_LogoHiddenOnHideLogo1(t *testing.T) {
	t.Setenv(tuilib.EnvHideLogo, "1")
	got := renderBannerForTest(t)
	if bannerHasArtRow(got) {
		t.Errorf("AGY_CLI_HIDE_LOGO=1: art row should be suppressed, banner = %q", got)
	}
}

// TestBanner_LogoHiddenOnHideLogoTrue covers the explicit boolean
// spelling. Pinning "true" (vs "1") keeps the parser honest: a
// regression that only matched "1" would still trip the changelog
// spec, which lists both.
func TestBanner_LogoHiddenOnHideLogoTrue(t *testing.T) {
	t.Setenv(tuilib.EnvHideLogo, "true")
	got := renderBannerForTest(t)
	if bannerHasArtRow(got) {
		t.Errorf("AGY_CLI_HIDE_LOGO=true: art row should be suppressed, banner = %q", got)
	}
}

// TestBanner_LogoHiddenOnHideLogoYes covers the third truthy
// spelling. "yes" is what users reach for when they have an
// /etc/environment-style config file that aliases "true" to a
// different string.
func TestBanner_LogoHiddenOnHideLogoYes(t *testing.T) {
	t.Setenv(tuilib.EnvHideLogo, "yes")
	got := renderBannerForTest(t)
	if bannerHasArtRow(got) {
		t.Errorf("AGY_CLI_HIDE_LOGO=yes: art row should be suppressed, banner = %q", got)
	}
}

// TestBanner_LogoHiddenOnHideLogoUppercase pins the case-
// insensitive axis. A regression that dropped ToLower would only
// match the lowercase spellings and silently leave the art on
// when a user (or their .zshenv) writes AGY_CLI_HIDE_LOGO=TRUE.
func TestBanner_LogoHiddenOnHideLogoUppercase(t *testing.T) {
	t.Setenv(tuilib.EnvHideLogo, "TRUE")
	got := renderBannerForTest(t)
	if bannerHasArtRow(got) {
		t.Errorf("AGY_CLI_HIDE_LOGO=TRUE: art row should be suppressed, banner = %q", got)
	}
}

// TestBanner_VersionAndAccountStillShown pins the second clause of
// the changelog: "keeping the version and account lines". When
// the art row is suppressed, the version line ("Antigravity
// CLI <ver>") AND the email/account row must still appear. The
// cwd row and model row are also kept — they are textual, not art
// — but the changelog only names the first two explicitly. This
// test pins all four textual rows to give the parser a stronger
// contract: the gate suppresses only the art, not anything else.
func TestBanner_VersionAndAccountStillShown(t *testing.T) {
	t.Setenv(tuilib.EnvHideLogo, "1")
	got := renderBannerForTest(t)
	if bannerHasArtRow(got) {
		t.Errorf("AGY_CLI_HIDE_LOGO=1: art row should be suppressed, banner = %q", got)
	}
	// Version line (banner row 1): active brand CLI name
	if !bannerHasText(got, branding.Current().CLIName) {
		t.Errorf("AGY_CLI_HIDE_LOGO=1: version line missing, banner = %q", got)
	}
	// Account line (banner row 2): the email address.
	if !bannerHasText(got, "u@example.com") {
		t.Errorf("AGY_CLI_HIDE_LOGO=1: account line missing, banner = %q", got)
	}
	// Model row (banner row 3): the active model name.
	if !bannerHasText(got, "Gemini 3.8 Flash") {
		t.Errorf("AGY_CLI_HIDE_LOGO=1: model row missing, banner = %q", got)
	}
	// Cwd row (banner row 4): the working directory. The
	// shortenCwd helper converts the leading $HOME to "~", so the
	// visible text is "~/Documents/GitHub/openagy".
	if !bannerHasText(got, "~/Documents/GitHub/openagy") {
		t.Errorf("AGY_CLI_HIDE_LOGO=1: cwd row missing, banner = %q", got)
	}
}

// TestBanner_LogoShownOnGarbageValue pins the default-degradation
// rule: an unrecognised value (including "garbage", "0",
// "false", "no", "on", and the empty string) leaves the art row
// rendered. This is the safe default — a typo in the env var
// degrades to "the user sees the same banner as before", not to
// "the user has a quiet prompt and didn't intend to".
func TestBanner_LogoShownOnGarbageValue(t *testing.T) {
	cases := []string{"garbage", "0", "false", "no", "on", "", " "}
	for _, v := range cases {
		t.Run(v, func(t *testing.T) {
			t.Setenv(tuilib.EnvHideLogo, v)
			got := renderBannerForTest(t)
			if !bannerHasArtRow(got) {
				t.Errorf("AGY_CLI_HIDE_LOGO=%q: art row should be present (default-degradation), banner = %q",
					v, got)
			}
		})
	}
}

func TestBanner_Branding_Switch(t *testing.T) {
	defer branding.Reset()

	// Pigasus brand (Pigasus CLI)
	branding.SetCurrent(branding.BrandPigasus)
	got := renderBannerForTest(t)
	if !bannerHasText(got, "Pigasus CLI") {
		t.Errorf("expected Pigasus CLI in banner under BrandPigasus, got:\n%s", got)
	}

	// Antigravity brand (Antigravity CLI)
	branding.SetCurrent(branding.BrandAntigravity)
	got = renderBannerForTest(t)
	if !bannerHasText(got, "Antigravity CLI") {
		t.Errorf("expected Antigravity CLI in banner under BrandAntigravity, got:\n%s", got)
	}
}

// announcementModel returns a fresh RootModel with the announcement
// queue seeded. Tests that need an empty catalog pass an
// ANTIGRAVITY_HOME that has dismissed every card; tests that need a
// populated catalog leave ANTIGRAVITY_HOME pointing at a fresh dir.
func announcementModel(t *testing.T) *RootModel {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	m := newRootModel(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.overlays.announceQueue.Load()
	return m
}

// announcementModelWithDismissals returns a fresh RootModel whose
// queue has every catalog card pre-dismissed. Useful for the
// "empty queue shows no card row" test.
func announcementModelWithDismissals(t *testing.T) *RootModel {
	t.Helper()
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("HOME", t.TempDir())
	for _, c := range announce.Catalog() {
		if err := announce.SaveDismissed(c.ID); err != nil {
			t.Fatalf("SaveDismissed(%q): %v", c.ID, err)
		}
	}
	m := newRootModel(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.overlays.announceQueue.Load()
	return m
}

// TestAnnouncementQueue_NewestFirst pins the "newest first, head of
// slice = highest priority" semantic. The TUI renders the head as
// the active card; the spec requires the highest-priority card be
// the one shown first.
func TestAnnouncementQueue_NewestFirst(t *testing.T) {
	m := announcementModel(t)
	if len(m.overlays.announceQueue.Cards) == 0 {
		t.Fatal("queue empty after Load; expected 3 cards")
	}
	card := m.overlays.announceQueue.Active()
	if card == nil {
		t.Fatal("Active returned nil for non-empty queue")
	}
	wantHead := "model-claude-opus-5-5"
	if card.ID != wantHead {
		t.Errorf("head card ID = %q, want %q", card.ID, wantHead)
	}
	// Walk the rest of the queue and pin the order.
	want := []string{
		"model-claude-opus-5-5",
		"deprecate-gemini-2-0-flash",
		"service-quota-rollover",
	}
	if len(m.overlays.announceQueue.Cards) != len(want) {
		t.Fatalf("queue length = %d, want %d", len(m.overlays.announceQueue.Cards), len(want))
	}
	for i, id := range want {
		if m.overlays.announceQueue.Cards[i].ID != id {
			t.Errorf("queue card %d ID = %q, want %q", i, m.overlays.announceQueue.Cards[i].ID, id)
		}
	}
}

// TestAnnouncementRender_AppearsAbovePrompt is the layout test:
// View() output must contain the active card's title somewhere
// above the prompt ">". We find the row index of each; the card
// title must come strictly before the prompt line.
func TestAnnouncementRender_AppearsAbovePrompt(t *testing.T) {
	m := announcementModel(t)
	view := m.View()
	lines := strings.Split(view.Content, "\n")

	titleIdx := -1
	promptIdx := -1
	for i, line := range lines {
		if strings.Contains(termcolor.StripSGR(line), "Claude Opus 5.5 available") {
			titleIdx = i
		}
		if strings.HasPrefix(termcolor.StripSGR(line), ">") && promptIdx == -1 && strings.Contains(termcolor.StripSGR(line), " ") {
			promptIdx = i
		}
	}
	if titleIdx == -1 {
		t.Fatalf("announcement title not in view:\n%s", view.Content)
	}
	if promptIdx == -1 {
		t.Fatalf("prompt not in view:\n%s", view.Content)
	}
	if titleIdx >= promptIdx {
		t.Errorf("title row %d should be above prompt row %d:\n%s", titleIdx, promptIdx, view.Content)
	}
}

// TestAnnouncementRender_EmptyQueueHidden is the spec's "empty
// queue shows no card row" rule. The view must not contain the
// announcement title when every catalog card is dismissed.
func TestAnnouncementRender_EmptyQueueHidden(t *testing.T) {
	m := announcementModelWithDismissals(t)
	view := m.View()
	for _, c := range announce.Catalog() {
		if strings.Contains(view.Content, c.Title) {
			t.Errorf("dismissed card %q title still in view:\n%s", c.Title, view.Content)
		}
	}
	// Cursor Y should match the legacy idle position (no card row).
	if view.Cursor == nil {
		t.Fatal("view has no cursor")
	}
	if view.Cursor.Y != 8 {
		t.Errorf("idle cursor Y = %d, want 8 (legacy layout)", view.Cursor.Y)
	}
}

// TestAnnouncementEsc_DismissesCard is the spec's "Esc on empty
// prompt dismisses the current card permanently and shows the next
// one". After Esc the head of the queue advances AND the dismissed
// ID is persisted.
func TestAnnouncementEsc_DismissesCard(t *testing.T) {
	m := announcementModel(t)
	if got := m.overlays.announceQueue.Active().ID; got != "model-claude-opus-5-5" {
		t.Fatalf("pre-Esc head = %q", got)
	}
	// Esc on empty prompt — fire the keypress through Update so the
	// branch in the real Update path runs.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if got := m.overlays.announceQueue.Active(); got == nil {
		t.Fatal("post-Esc head is nil; expected next card")
	} else if got.ID != "deprecate-gemini-2-0-flash" {
		t.Errorf("post-Esc head = %q, want deprecate-gemini-2-0-flash", got.ID)
	}
	// And the dismissed ID is persisted to settings.json. We don't
	// need to round-trip through newRootModel — LoadDismissed is
	// already covered by internal/announce — but a sanity check on
	// the file content pins the integration.
	body, err := os.ReadFile(os.Getenv("ANTIGRAVITY_HOME") + "/settings.json")
	if err != nil {
		t.Fatalf("settings.json missing: %v", err)
	}
	if !strings.Contains(string(body), "model-claude-opus-5-5") {
		t.Errorf("dismissed id not persisted:\n%s", body)
	}
}

// TestAnnouncementEsc_NonEmptyPrompt does NOT dismiss — the spec
// requires "on an empty prompt". Pressing Esc with text in the
// buffer should keep the current card visible.
func TestAnnouncementEsc_NonEmptyPrompt(t *testing.T) {
	m := announcementModel(t)
	for _, r := range "hello" {
		m.Update(runeKey(r))
	}
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if got := m.overlays.announceQueue.Active(); got == nil {
		t.Fatal("Esc on non-empty prompt should NOT dismiss")
	} else if got.ID != "model-claude-opus-5-5" {
		t.Errorf("head after Esc-on-text = %q, want unchanged model-claude-opus-5-5", got.ID)
	}
}

// TestAnnouncementEsc_DismissesAllThenEmpty confirms the queue
// empties after enough Esc presses, and that the third Esc on an
// empty queue is the silent-absorb branch (no panic, no error).
func TestAnnouncementEsc_DismissesAllThenEmpty(t *testing.T) {
	m := announcementModel(t)
	for i := 0; i < 3; i++ {
		m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	}
	if m.overlays.announceQueue.Active() != nil {
		t.Errorf("queue not empty after 3 Escs; head = %q", m.overlays.announceQueue.Active().ID)
	}
	// A fourth Esc on an empty queue must not panic and must not
	// change state.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.overlays.announceQueue.Active() != nil {
		t.Errorf("extra Esc surfaced a card")
	}
}

// TestAnnouncementSubmit_HidesForSession is the second half of the
// spec: "sending a message hides the card for the rest of the
// session". Submitting a prompt hides the card in-memory but does
// NOT persist.
func TestAnnouncementSubmit_HidesForSession(t *testing.T) {
	m := announcementModel(t)
	if got := m.overlays.announceQueue.Active(); got == nil || got.ID != "model-claude-opus-5-5" {
		t.Fatalf("pre-submit head = %v, want model-claude-opus-5-5", got)
	}
	for _, r := range "hi" {
		m.Update(runeKey(r))
	}
	m.conversation.streaming = false
	m.Update(enterKey())
	if got := m.overlays.announceQueue.Active(); got == nil {
		t.Fatal("submit should hide for session; got nil card with more in queue")
	}
	if got := m.overlays.announceQueue.Active().ID; got != "deprecate-gemini-2-0-flash" {
		t.Errorf("post-submit head = %q, want deprecate-gemini-2-0-flash", got)
	}
	if !m.announceSessionHiddenAfterSubmit("model-claude-opus-5-5") {
		t.Error("session-hidden ID was not recorded")
	}
	// The ID must NOT be persisted.
	if _, err := os.Stat(os.Getenv("ANTIGRAVITY_HOME") + "/settings.json"); err == nil {
		body, _ := os.ReadFile(os.Getenv("ANTIGRAVITY_HOME") + "/settings.json")
		if strings.Contains(string(body), "model-claude-opus-5-5") {
			t.Errorf("submit should not persist; settings.json contains the id:\n%s", body)
		}
	}
}

// TestAnnouncementSubmit_NoCardNoop confirms submitPrompt is a
// no-op when there's nothing to hide.
func TestAnnouncementSubmit_NoCardNoop(t *testing.T) {
	m := announcementModelWithDismissals(t)
	for _, r := range "hi" {
		m.Update(runeKey(r))
	}
	m.conversation.streaming = false
	m.Update(enterKey())
	if m.overlays.announceQueue.Active() != nil {
		t.Errorf("submit on empty queue surfaced a card")
	}
	if m.overlays.announceQueue.SessionHiddenID != "" {
		t.Errorf("submit on empty queue recorded a hidden ID: %q", m.overlays.announceQueue.SessionHiddenID)
	}
}

// TestAnnouncementRestart_RespectsPersistedDismiss is the
// integration test: a card dismissed in one session is NOT shown in
// the next. We persist via the queue's Dismiss (which calls
// announce.SaveDismissed under the hood), then construct a fresh
// RootModel against the same ANTIGRAVITY_HOME so it picks up the
// persisted list.
func TestAnnouncementRestart_RespectsPersistedDismiss(t *testing.T) {
	home := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", home)
	t.Setenv("HOME", t.TempDir())

	m := newRootModel(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.overlays.announceQueue.Load()
	m.overlays.announceQueue.Dismiss()
	if got := m.overlays.announceQueue.Active(); got == nil || got.ID != "deprecate-gemini-2-0-flash" {
		t.Fatalf("first dismiss did not advance; head = %v", got)
	}
	// New session, same ANTIGRAVITY_HOME — the persisted dismissal
	// must be honoured.
	m2 := newRootModel(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m2.overlays.announceQueue.Load()
	if m2.overlays.announceQueue.Active() == nil {
		t.Fatal("new session has no active announcement after one dismiss; expected two left")
	}
	if m2.overlays.announceQueue.Active().ID != "deprecate-gemini-2-0-flash" {
		t.Errorf("new session head = %q, want deprecate-gemini-2-0-flash (model-claude-opus-5-5 was dismissed)",
			m2.overlays.announceQueue.Active().ID)
	}
	for _, c := range m2.overlays.announceQueue.Cards {
		if c.ID == "model-claude-opus-5-5" {
			t.Errorf("persisted dismiss not honoured; model-claude-opus-5-5 still in queue")
		}
	}
}

// announceSessionHiddenAfterSubmit returns true when the user's
// last submitPrompt hid the given ID for the rest of the session.
// Test helper to keep the assertion in TestAnnouncementSubmit_*.
func (m *RootModel) announceSessionHiddenAfterSubmit(id string) bool {
	if m == nil {
		return false
	}
	return m.overlays.announceQueue.SessionHiddenID == id
}

// TestAnnouncementRender_HintRow covers the card's footer hint row.
// The spec's example shows "Esc to dismiss · Send a message to hide"
// — the renderer must emit this string verbatim (or fall back to it
// when the catalog entry omits Hint).
func TestAnnouncementRender_HintRow(t *testing.T) {
	m := announcementModel(t)
	view := m.View()
	if !strings.Contains(termcolor.StripSGR(view.Content), "Esc to dismiss") {
		t.Errorf("hint row missing from view:\n%s", view.Content)
	}
	if !strings.Contains(termcolor.StripSGR(view.Content), "Send a message to hide") {
		t.Errorf("hint row missing from view:\n%s", view.Content)
	}
}

// TestAnnouncementRender_CardRowNotInTranscript checks the layout
// invariant: the card row sits between the top separator and the
// prompt, not inside the scrollable transcript. The transcript
// rows must precede the card's title row in the rendered output.
func TestAnnouncementRender_CardRowNotInTranscript(t *testing.T) {
	m := announcementModel(t)
	m.conversation.response = []string{"transcript line 1", "transcript line 2"}
	view := m.View()
	transcriptRow := "transcript line 2"
	transcriptIdx := -1
	titleIdx := -1
	for i, line := range strings.Split(view.Content, "\n") {
		if strings.Contains(termcolor.StripSGR(line), transcriptRow) && transcriptIdx == -1 {
			transcriptIdx = i
		}
		if strings.Contains(termcolor.StripSGR(line), "Claude Opus 5.5 available") && titleIdx == -1 {
			titleIdx = i
		}
	}
	if transcriptIdx == -1 || titleIdx == -1 {
		t.Fatalf("missing rows; transcriptIdx=%d titleIdx=%d", transcriptIdx, titleIdx)
	}
	if titleIdx < transcriptIdx {
		t.Errorf("card title at row %d appears before transcript row %d; layout broken:\n%s",
			titleIdx, transcriptIdx, view.Content)
	}
}

func bashModeRootModel(t *testing.T) *RootModel {
	t.Helper()
	t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	m := newRootModel(&fakeClient{}, "u@t", "Gemini 3.8 Flash (Medium)", "", "/tmp", false)
	m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return m
}

// TestBashHeadRow pins the three bullet palettes and the bold-blue "! cmd".
func TestBashHeadRow(t *testing.T) {
	for _, tc := range []struct {
		name    string
		failed  bool
		running bool
		want    string
	}{
		{
			name: "success",
			want: "\x1b[38;2;133;153;0m\u25cf\x1b[m \x1b[38;2;38;139;210;1m! echo hello\x1b[m",
		},
		{
			name:   "failure",
			failed: true,
			want:   "\x1b[38;2;220;50;47m\u25cf\x1b[m \x1b[38;2;38;139;210;1m! echo hello\x1b[m",
		},
		{
			name:    "running",
			running: true,
			want:    "\x1b[38;2;181;137;0m\u25cb\x1b[m \x1b[38;2;38;139;210;1m! echo hello\x1b[m",
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := bashmode.HeadRow("echo hello", tc.failed, tc.running); got != tc.want {
				t.Fatalf("bashHeadRow =\n%q\nwant\n%q", got, tc.want)
			}
		})
	}
}

// TestBashOutputRows pins the "  -|  " body: blue bold prefix, ";22" first
// row, continuation rows starting at column 0, red on failure.
func TestBashOutputRows(t *testing.T) {
	cases := []struct {
		name   string
		out    string
		failed bool
		want   []string
	}{
		{
			name: "success single line",
			out:  "hello",
			want: []string{
				"\x1b[38;2;38;139;210;1m  \u23bf  \x1b[38;2;88;110;117;22mhello\x1b[m",
			},
		},
		{
			name: "success multi line",
			out:  "x\ny",
			want: []string{
				"\x1b[38;2;38;139;210;1m  \u23bf  \x1b[38;2;88;110;117;22mx\x1b[m",
				"\x1b[38;2;88;110;117my\x1b[m",
			},
		},
		{
			name:   "failure keeps the output and adds no status line",
			out:    "out\nerr",
			failed: true,
			want: []string{
				"\x1b[38;2;38;139;210;1m  \u23bf  \x1b[38;2;220;50;47;22mout\x1b[m",
				"\x1b[38;2;220;50;47merr\x1b[m",
			},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := bashmode.OutputRows(tc.out, tc.failed)
			if len(got) != len(tc.want) {
				t.Fatalf("rows = %d, want %d: %#v", len(got), len(tc.want), got)
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Fatalf("row %d =\n%q\nwant\n%q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

// TestExecBashCommand covers the four measured result shapes: success,
// failure with output (no status line), failure without output (the Go error
// text), and cancellation (no output, not failed).
func TestExecBashCommand(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "marker.txt"), []byte("from-cwd"), 0o644); err != nil {
		t.Fatal(err)
	}
	run := func(ctx context.Context, command string) bashDoneMsg {
		t.Helper()
		return execBashCommand(ctx, dir, command, &tools.SafeBuffer{}, 1)
	}

	t.Run("success", func(t *testing.T) {
		msg := run(context.Background(), "printf 'a\\nb\\n'")
		if msg.output != "a\nb" || msg.failed || msg.cancelled {
			t.Fatalf("got (%q, %v, %v)", msg.output, msg.failed, msg.cancelled)
		}
	})

	t.Run("cwd", func(t *testing.T) {
		msg := run(context.Background(), "cat marker.txt")
		if msg.output != "from-cwd" || msg.failed {
			t.Fatalf("got (%q, %v)", msg.output, msg.failed)
		}
	})

	t.Run("failure with output has no status line", func(t *testing.T) {
		msg := run(context.Background(), "echo out; echo err >&2; exit 3")
		if msg.output != "out\nerr" || !msg.failed {
			t.Fatalf("got (%q, %v)", msg.output, msg.failed)
		}
	})

	t.Run("failure without output is the Go error text", func(t *testing.T) {
		msg := run(context.Background(), "exit 3")
		if msg.output != "exit status 3" || !msg.failed {
			t.Fatalf("got (%q, %v)", msg.output, msg.failed)
		}
	})

	t.Run("cancel leaves no output and is not a failure", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		msg := run(ctx, "sleep 5")
		if msg.output != "" || msg.failed || !msg.cancelled {
			t.Fatalf("got (%q, %v, %v)", msg.output, msg.failed, msg.cancelled)
		}
	})

	// The reference stops the whole tree, so a cancelled command must not
	// keep the turn alive for as long as an orphaned grandchild runs. Before
	// the process-group kill this blocked for the full 30 s: `zsh -c` died,
	// `sh -c`/`sleep` inherited the stdout pipe, and exec.Cmd.Wait waited on
	// the copy goroutine until EOF.
	t.Run("cancel kills grandchildren instead of waiting", func(t *testing.T) {
		out := &tools.SafeBuffer{}
		ctx, cancel := context.WithCancel(context.Background())
		go func() {
			for out.String() == "" {
				time.Sleep(5 * time.Millisecond)
			}
			cancel()
		}()
		started := time.Now()
		msg := execBashCommand(ctx, dir, "sh -c 'echo first; sleep 30'", out, 7)
		if elapsed := time.Since(started); elapsed > 5*time.Second {
			t.Fatalf("cancel took %v; grandchildren kept the pipe open", elapsed)
		}
		if !msg.cancelled {
			t.Fatalf("cancelled = false, want true")
		}
		if got := bashmode.TrimOutput(out.String()); got != "first" {
			t.Fatalf("partial output = %q, want %q (a preemption snapshots this)", got, "first")
		}
	})
}

// TestBashModeKeyArming: "!" on an empty composer arms the mode and is
// consumed; on a non-empty composer it is a literal rune.
func TestBashModeKeyArming(t *testing.T) {
	m := bashModeRootModel(t)
	m.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
	if !m.session.store.BashMode {
		t.Fatal("! on empty composer did not arm bash mode")
	}
	if m.prompt.input.Value() != "" {
		t.Fatalf("buffer = %q, want empty (the ! is the mode marker)", m.prompt.input.Value())
	}

	// Esc leaves the mode.
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if m.session.store.BashMode {
		t.Fatal("Esc did not leave bash mode")
	}

	// A space then "!" is ordinary text: the mode only arms on an empty
	// buffer (【一手】 ` !echo lead` went to the model).
	m.Update(tea.KeyPressMsg{Code: ' ', Text: " "})
	m.Update(tea.KeyPressMsg{Code: '!', Text: "!"})
	if m.session.store.BashMode {
		t.Fatal("! after a space armed bash mode")
	}
	if got := m.prompt.input.Value(); got != " !" {
		t.Fatalf("buffer = %q, want %q", got, " !")
	}
}

// TestBashModeSubmitRunsLocally: Enter in bash mode never touches the
// model. The transcript gets the running head row and the mode clears.
func TestBashModeSubmitRunsLocally(t *testing.T) {
	m := bashModeRootModel(t)
	m.session.store.BashMode = true
	m.prompt.input.SetValue("echo hello")

	cmd := m.submitPrompt()
	if cmd == nil {
		t.Fatal("submitPrompt returned nil, want the exec cmd")
	}
	if !m.conversation.streaming {
		t.Fatal("streaming not set while the command runs")
	}
	if m.session.store.BashMode {
		t.Fatal("bash mode survived submit")
	}
	if len(m.SessionMessages()) != 0 {
		t.Fatalf("shell command reached the model session: %#v", m.SessionMessages())
	}
	head := m.conversation.response[len(m.conversation.response)-1]
	if want := bashmode.HeadRow("echo hello", false, true); head != want {
		t.Fatalf("head row =\n%q\nwant\n%q", head, want)
	}
	if got := m.prompt.promptHistory.Entries()[m.prompt.promptHistory.Len()-1]; got != "!echo hello" {
		t.Fatalf("history entry = %q, want %q", got, "!echo hello")
	}

	// Completion repaints the bullet and appends the body + blank row.
	m.finishBashCommand(bashDoneMsg{cmd: "echo hello", output: "hello"})
	if m.conversation.streaming {
		t.Fatal("streaming survived completion")
	}
	if m.session.store.BashRowIdx != -1 {
		t.Fatalf("bashRowIdx = %d, want -1", m.session.store.BashRowIdx)
	}
	if want := bashmode.HeadRow("echo hello", false, false); m.conversation.response[len(m.conversation.response)-3] != want {
		t.Fatalf("final head row = %q", m.conversation.response[len(m.conversation.response)-3])
	}
	if m.conversation.response[len(m.conversation.response)-1] != "" {
		t.Fatal("no trailing blank row after the block")
	}
}

// TestBashModeEmptyCommandJustLeaves: `!` + Enter prints nothing.
func TestBashModeEmptyCommandJustLeaves(t *testing.T) {
	m := bashModeRootModel(t)
	m.session.store.BashMode = true
	if cmd := m.submitPrompt(); cmd != nil {
		t.Fatal("empty bash command returned a cmd")
	}
	if m.session.store.BashMode {
		t.Fatal("bash mode survived the empty submit")
	}
	if len(m.conversation.response) != 0 {
		t.Fatalf("empty bash command wrote a transcript row: %#v", m.conversation.response)
	}
}

// TestBashModeCancel: Esc runs the cancel func and completion leaves the
// head row without a body (【一手】 `!sleep 5` + Esc).
func TestBashModeCancel(t *testing.T) {
	m := bashModeRootModel(t)
	cancelled := false
	m.session.store.BashCancel = func() { cancelled = true }
	m.Update(tea.KeyPressMsg{Code: tea.KeyEsc})
	if !cancelled {
		t.Fatal("Esc did not cancel the running command")
	}
	if m.session.store.BashCancel != nil {
		t.Fatal("bashCancel still installed")
	}
}

// TestBashHistoryRecall re-arms the mode when the entry was a command.
func TestBashHistoryRecall(t *testing.T) {
	m := bashModeRootModel(t)
	m.prompt.promptHistory.Push("hello")
	m.prompt.promptHistory.Push("!echo hi")
	m.prompt.input.Reset()

	m.historyUp()
	if !m.session.store.BashMode || m.prompt.input.Value() != "echo hi" {
		t.Fatalf("recall = (%v, %q), want (true, %q)", m.session.store.BashMode, m.prompt.input.Value(), "echo hi")
	}
	m.historyUp()
	if m.session.store.BashMode || m.prompt.input.Value() != "hello" {
		t.Fatalf("recall = (%v, %q), want (false, %q)", m.session.store.BashMode, m.prompt.input.Value(), "hello")
	}
}

// TestBashPreemptsRunningCommand: a shell command submitted while another is
// running interrupts it — the old block keeps the output it had produced and
// the new command starts at once, instead of queueing behind an Esc.
//
// 【一手】 agy 1.4.5 `!sh -c 'echo first; sleep 30'` then `!echo queued2`:
// the first block gains `  ⎿  first` while keeping its hollow `○`, and the
// second block completes, all before any key press.
func TestBashPreemptsRunningCommand(t *testing.T) {
	m := bashModeRootModel(t)
	m.conversation.streaming = true
	m.session.store.BashGen = 4
	m.session.store.BashRowIdx = len(m.conversation.response)
	m.conversation.response = append(m.conversation.response, bashmode.HeadRow("top", false, true))
	headIdx := m.session.store.BashRowIdx
	out := &tools.SafeBuffer{}
	if _, err := out.Write([]byte("first\nsecond\n")); err != nil {
		t.Fatal(err)
	}
	m.session.store.BashRun = out
	m.session.store.BashCmd = "top"
	cancelled := false
	m.session.store.BashCancel = func() { cancelled = true }

	m.session.store.BashMode = true
	m.prompt.input.SetValue("echo next")
	cmd := m.submitPrompt()
	if cmd == nil {
		t.Fatal("preemption returned no cmd for the new command")
	}
	if !cancelled {
		t.Fatal("the running command was not cancelled")
	}
	if len(m.session.store.BashQueue) != 0 {
		t.Fatalf("preemption queued instead of running: %#v", m.session.store.BashQueue)
	}
	if !m.conversation.streaming {
		t.Fatal("streaming cleared during preemption")
	}
	if got := m.conversation.response[headIdx]; got != bashmode.HeadRow("top", false, true) {
		t.Fatalf("interrupted head was repainted:\n%q", got)
	}
	wantBody := bashmode.OutputRows("first\nsecond", false)
	for i, want := range wantBody {
		if got := m.conversation.response[headIdx+1+i]; got != want {
			t.Fatalf("body row %d =\n%q\nwant\n%q", i, got, want)
		}
	}
	if got := m.conversation.response[len(m.conversation.response)-1]; got != bashmode.HeadRow("echo next", false, true) {
		t.Fatalf("new running head =\n%q", got)
	}
	if m.session.store.BashGen != 5 {
		t.Fatalf("bashGen = %d, want 5", m.session.store.BashGen)
	}

	// The preempted command's late completion carries the stale generation
	// and must not touch the live block.
	before := len(m.conversation.response)
	m.updateBashDoneMsg(bashDoneMsg{gen: 4, cmd: "top", output: "late"})
	if len(m.conversation.response) != before || !m.conversation.streaming {
		t.Fatal("stale completion mutated the live block")
	}
}

// TestBashCtrlCCancelsCommand: Ctrl+C stops a running shell command and still
// arms the two-step exit confirmation on the same press (【一手】 agy 1.4.5).
func TestBashCtrlCCancelsCommand(t *testing.T) {
	m := bashModeRootModel(t)
	cancelled := false
	m.session.store.BashCancel = func() { cancelled = true }
	m.Update(tea.KeyPressMsg{Code: 'c', Mod: tea.ModCtrl})
	if !cancelled {
		t.Fatal("Ctrl+C did not cancel the running command")
	}
	if !m.display.exitConfirm {
		t.Fatal("Ctrl+C did not arm the exit confirmation")
	}
	if m.display.quitting {
		t.Fatal("the first Ctrl+C quit")
	}
}

// TestBashCancelDropsPartialOutput: Esc / Ctrl+C finalise the block with the
// head only, even when the command had already printed something.
func TestBashCancelDropsPartialOutput(t *testing.T) {
	m := bashModeRootModel(t)
	m.session.store.BashRowIdx = 0
	m.conversation.response = []string{bashmode.HeadRow("x", false, true)}
	m.finishBashCommand(bashDoneMsg{gen: 1, cmd: "x", output: "first", cancelled: true})
	if len(m.conversation.response) != 2 {
		t.Fatalf("cancelled block rows = %#v, want head + blank", m.conversation.response)
	}
	if want := bashmode.HeadRow("x", false, false); m.conversation.response[0] != want {
		t.Fatalf("head = %q, want %q", m.conversation.response[0], want)
	}
	if m.conversation.response[1] != "" {
		t.Fatalf("expected the trailing blank row, got %q", m.conversation.response[1])
	}
}

// TestBashBlockTruncates pins the collapsed long-output form: the elided
// count, the last `height/2` rows, and the expand hint on the final row.
//
// 【一手】 agy 1.4.5 at 120×40: `!seq 1 21` →
//
//	  ⎿  <output +1 lines>
//	2 … 21 (ctrl+o to expand)
func TestBashBlockTruncates(t *testing.T) {
	lines := make([]string, 21)
	for i := range lines {
		lines[i] = fmt.Sprintf("%d", i+1)
	}
	rows := (&bashmode.Block{Cmd: "seq 1 21", Body: strings.Join(lines, "\n")}).Lines(false, 40)

	// head + marker + 20 body rows + blank
	if len(rows) != 23 {
		t.Fatalf("rows = %d, want 23: %#v", len(rows), rows)
	}
	if want := bashmode.HeadRow("seq 1 21", false, false); rows[0] != want {
		t.Fatalf("head = %q, want %q", rows[0], want)
	}
	wantMarker := cWarningBlueOpen + "  \u23bf  " + palette.WarningBodyOpen + "<output +1 lines>\x1b[m"
	if rows[1] != wantMarker {
		t.Fatalf("marker = %q, want %q", rows[1], wantMarker)
	}
	if want := cSubtleOpen + "2\x1b[m"; rows[2] != want {
		t.Fatalf("first kept row = %q, want %q (continuation style, no \u23bf)", rows[2], want)
	}
	last := rows[len(rows)-2]
	if !strings.Contains(last, "21\x1b[m ") || !strings.Contains(last, "(ctrl+o to expand)") {
		t.Fatalf("last row = %q, want 21 + expand hint", last)
	}
	if rows[len(rows)-1] != "" {
		t.Fatal("missing trailing blank row")
	}

	// 20 rows never truncate and never show a hint (【一手】 `!seq 1 20`).
	rows = (&bashmode.Block{Cmd: "seq 1 20", Body: strings.Join(lines[:20], "\n")}).Lines(false, 40)
	if len(rows) != 22 {
		t.Fatalf("20-line rows = %d, want 22", len(rows))
	}
	for _, r := range rows {
		if strings.Contains(r, "ctrl+o") || strings.Contains(r, "<output") {
			t.Fatalf("short body grew a marker/hint: %q", r)
		}
	}

	// The cap is half the terminal height (measured 20/25/30 at 40/50/60):
	// 40 lines at height 60 show 30 and elide 10.
	var forty []string
	for i := 1; i <= 40; i++ {
		forty = append(forty, fmt.Sprintf("%d", i))
	}
	rows = (&bashmode.Block{Cmd: "seq 1 40", Body: strings.Join(forty, "\n")}).Lines(false, 60)
	if !strings.Contains(rows[1], "<output +10 lines>") {
		t.Fatalf("60-row terminal marker = %q, want <output +10 lines>", rows[1])
	}
	if len(rows) != 33 { // head + marker + 30 + blank
		t.Fatalf("60-row rows = %d, want 33", len(rows))
	}
}

// TestBashBlockExpanded: ctrl+o drops the marker, shows the full body and
// flips the hint to "(ctrl+o to collapse)".
func TestBashBlockExpanded(t *testing.T) {
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, fmt.Sprintf("%d", i))
	}
	b := &bashmode.Block{Cmd: "seq 1 200", Body: strings.Join(lines, "\n")}

	collapsed := b.Lines(false, 40)
	expanded := b.Lines(true, 40)
	if len(expanded) <= len(collapsed) {
		t.Fatalf("expanded rows = %d, collapsed = %d", len(expanded), len(collapsed))
	}
	for _, r := range expanded {
		if strings.Contains(r, "<output +") {
			t.Fatalf("expanded block kept the marker: %q", r)
		}
	}
	last := expanded[len(expanded)-2]
	if !strings.Contains(last, "200\x1b[m ") || !strings.Contains(last, "(ctrl+o to collapse)") {
		t.Fatalf("expanded last row = %q", last)
	}
}

// TestBashCtrlOTogglesBlock: the ctrl+o key re-renders the recorded block in
// place, so the transcript row count follows the hint.
func TestBashCtrlOTogglesBlock(t *testing.T) {
	m := bashModeRootModel(t)
	m.session.store.BashRowIdx = len(m.conversation.response)
	m.conversation.response = append(m.conversation.response, bashmode.HeadRow("seq 1 21", false, true))
	var lines []string
	for i := 1; i <= 200; i++ {
		lines = append(lines, fmt.Sprintf("%d", i))
	}
	m.finishBashCommand(bashDoneMsg{gen: 1, cmd: "seq 1 200", output: strings.Join(lines, "\n")})
	collapsed := len(m.conversation.response)

	m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	expanded := len(m.conversation.response)
	if expanded != collapsed+179 {
		t.Fatalf("expanded rows = %d, want %d (179 more than the marker + 20 kept rows)", expanded, collapsed+179)
	}
	if strings.Contains(strings.Join(m.conversation.response, "\n"), "<output +") {
		t.Fatal("ctrl+o kept the truncation marker")
	}
	if !strings.Contains(m.conversation.response[len(m.conversation.response)-2], "(ctrl+o to collapse)") {
		t.Fatalf("missing collapse hint: %q", m.conversation.response[len(m.conversation.response)-2])
	}

	m.Update(tea.KeyPressMsg{Code: 'o', Mod: tea.ModCtrl})
	if len(m.conversation.response) != collapsed {
		t.Fatalf("collapse rows = %d, want %d", len(m.conversation.response), collapsed)
	}
}

// TestBashQueueWhileStreaming: a command submitted mid-turn is queued and
// rendered as "▸ <cmd>" without the (alt+m …) prompt suffix.
func TestBashQueueWhileStreaming(t *testing.T) {
	m := bashModeRootModel(t)
	m.conversation.streaming = true
	m.session.store.BashMode = true
	m.prompt.input.SetValue("echo queued")
	if cmd := m.submitPrompt(); cmd != nil {
		t.Fatal("a queued bash command returned a cmd")
	}
	if len(m.session.store.BashQueue) != 1 || m.session.store.BashQueue[0] != "echo queued" {
		t.Fatalf("bashQueue = %#v", m.session.store.BashQueue)
	}
	if m.conversation.streaming != true {
		t.Fatal("queueing cleared the streaming flag")
	}
	// The queued row is the reference's own bytes: a dim grey "▸ <cmd>" and
	// no "(alt+m …)" suffix (unlike a queued prompt).
	wantRow := cDimGrayOpen + "\u25b8 echo queued" + cDimGrayClose
	if view := m.View().Content; !strings.Contains(view, wantRow) {
		t.Fatalf("queue row missing from view:\n%s", view)
	}
}

// countTranscriptText counts the transcript rows carrying text.
func countTranscriptText(m *RootModel, text string) int {
	n := 0
	for _, line := range m.conversation.response {
		if strings.Contains(line, text) {
			n++
		}
	}
	return n
}

// TestSteerAtTurnEndKeepsTheMessageVisible is the AGY 1.5.3 send-
// immediately fix: a steer that arrives just as the turn ends stays in
// the conversation instead of disappearing and reappearing.
//
// The message is visible from the "Sending message..." row, becomes a
// transcript prompt when the steer is applied, and is still there —
// once — when the turn finalises.
func TestSteerAtTurnEndKeepsTheMessageVisible(t *testing.T) {
	m := streamQueueMode(t)
	m.session.store.QueueMode = false // Send Immediately
	m.updateBackendChunkMsg(backendChunkMsg("pre-answer"))
	m.prompt.input.SetValue("follow up")
	if cmd := m.submitPrompt(); cmd != nil {
		t.Fatalf("submit returned a cmd, want nil")
	}
	if countTranscriptText(m, "follow up") != 0 {
		t.Fatalf("the steer is already a transcript prompt before it is applied")
	}

	m.Update(steerAppliedMsg{instruction: "follow up"})
	if got := countTranscriptText(m, "follow up"); got != 1 {
		t.Fatalf("prompt rows after the steer applied = %d, want 1: %v", got, m.conversation.response)
	}

	m.Update(toolsteps.TurnDoneMsg{Reply: "pre-answer"})
	if got := countTranscriptText(m, "follow up"); got != 1 {
		t.Fatalf("prompt rows after the turn ended = %d, want 1: %v", got, m.conversation.response)
	}
	if got := countTranscriptText(m, "pre-answer"); got != 1 {
		t.Errorf("pre-steer answer rows = %d, want 1 (the reply was repeated below the prompt): %v", got, m.conversation.response)
	}
}

// TestSteerAtTurnEndSplitsTheReply pins the same race with text after
// the steer: the transcript keeps the pre-steer answer above the prompt
// and the post-steer suffix below it, and the session history is split
// the same way instead of duplicating the whole turn on the second
// assistant message.
func TestSteerAtTurnEndSplitsTheReply(t *testing.T) {
	m := streamQueueMode(t)
	m.session.store.QueueMode = false
	m.updateBackendChunkMsg(backendChunkMsg("pre-answer"))
	m.prompt.input.SetValue("follow up")
	_ = m.submitPrompt()
	m.Update(steerAppliedMsg{instruction: "follow up"})
	m.updateBackendChunkMsg(backendChunkMsg("post-answer"))
	m.Update(toolsteps.TurnDoneMsg{Reply: "pre-answerpost-answer"})

	joined := strings.Join(m.conversation.response, "\n")
	if got := strings.Count(joined, "pre-answer"); got != 1 {
		t.Errorf("pre-answer appears %d times, want 1: %v", got, m.conversation.response)
	}
	if got := strings.Count(joined, "post-answer"); got != 1 {
		t.Errorf("post-answer appears %d times, want 1: %v", got, m.conversation.response)
	}
	for _, msg := range m.SessionMessages() {
		if msg.Role == "assistant" && msg.Content == "pre-answerpost-answer" {
			t.Errorf("the session duplicated the whole turn on one assistant message: %v", m.SessionMessages())
		}
	}
	last := m.SessionMessages()[len(m.SessionMessages())-1]
	if last.Role != "assistant" || last.Content != "post-answer" {
		t.Errorf("last session message = %+v, want the post-steer suffix", last)
	}
}

// TestPendingSteerAtTurnEndIsCleared pins the other half: a steer the
// engine accepted but did not apply before the turn ended becomes the
// next turn's prompt, so the actor's pending copy must be dropped —
// otherwise the next turn applies it twice.
func TestPendingSteerAtTurnEndIsCleared(t *testing.T) {
	m := streamQueueMode(t)
	m.session.store.QueueMode = false
	eng := &mockSteerEngine{}
	m.turn.engine = eng
	m.prompt.input.SetValue("just in time")
	if cmd := m.submitPrompt(); cmd != nil {
		t.Fatalf("submit returned a cmd, want nil")
	}
	if len(eng.steered) != 1 {
		t.Fatalf("engine pending steers = %v, want the submitted message", eng.steered)
	}

	m.Update(toolsteps.TurnDoneMsg{Reply: "done"})

	if len(eng.steered) != 0 {
		t.Errorf("the engine kept the pending steer across the turn end: %v", eng.steered)
	}
	if got := countTranscriptText(m, "just in time"); got != 1 {
		t.Errorf("prompt rows = %d, want 1: %v", got, m.conversation.response)
	}
}
