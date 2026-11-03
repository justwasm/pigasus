package helppanel

import (
	"fmt"
	"regexp"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// panel_bytes_test.go — the overlay's rendered bytes, pinned.
//
// The /help overlay is a pixel-for-pixel clone of the host's own
// overlay, so the escape sequences are part of the contract: a colour
// that moves changes what the user sees, and a segment that is
// dropped leaves a row in the wrong colour.
//
// The expectations below are the bytes the host's own renderer wrote
// before the panel took the row over. They were taken from the
// pre-tranche tree (88a0c70f) and compared byte for byte against the
// panel's output.
//
// The colour scheme is pinned, because the bytes carry it.
//
// Two tokens are masked: the reference's version string and the log
// file's timestamp. Both change on their own — the version on a
// release, the timestamp every second — and neither is part of the
// overlay's shape.

// GENERAL_BYTES is the GENERAL tab, byte for byte.
const GENERAL_BYTES = "\x1b[38;2;38;139;210mPigasus CLI\x1b[39m   \x1b[1;4m\x1b[38;2;38;139;210mgeneral\x1b[0m    \x1b[38;2;238;232;213mcommands\x1b[0m    \x1b[38;2;238;232;213mshortcuts\x1b[0m \x1b[38;2;238;232;213m  (←/→ or tab to cycle)\x1b[0m\n" +
	"\x1b[38;2;101;123;131mPigasus CLI understands your codebase, makes edits with your permission,\x1b[39m\n" +
	"\x1b[38;2;101;123;131mand executes commands — right from your terminal.\x1b[39m\n" +
	"\n" +
	"\x1b[38;2;101;123;131mVersion X.Y.Z\x1b[39m\n" +
	"\x1b[38;2;101;123;131mu@t\x1b[39m\n" +
	"\x1b[38;2;101;123;131mWorkspace:      /tmp\x1b[39m\n" +
	"\x1b[38;2;101;123;131mProject Config: ~/.gemini/config/projects/default-cli-project.json\x1b[39m\n" +
	"\x1b[38;2;101;123;131mLog File:       ~/.gemini/antigravity-cli/log/cli-TIMESTAMP.log\x1b[39m\n" +
	"\x1b[38;2;101;123;131mConversation:   id\x1b[39m\n" +
	"\n" +
	"\x1b[1m\x1b[38;2;88;110;117mQuick Reference\x1b[0m\n" +
	"  \x1b[38;2;38;139;210m/\x1b[39m  \x1b[38;2;101;123;131mType / to see available commands\x1b[39m\n" +
	"\n" +
	"\x1b[1mKeyboard: \x1b[0m\x1b[38;2;38;139;210m↑/↓\x1b[39m \x1b[38;2;101;123;131mNavigate\x1b[39m  \x1b[38;2;38;139;210m←/→\x1b[39m \x1b[38;2;101;123;131mSwitch View\x1b[39m  \x1b[38;2;38;139;210mesc\x1b[39m \x1b[38;2;101;123;131mClose\x1b[39m\n" +
	"\n" +
	""

// SHORTCUTS_BYTES is the SHORTCUTS tab, byte for byte.
const SHORTCUTS_BYTES = "\x1b[38;2;38;139;210mPigasus CLI\x1b[39m   \x1b[38;2;238;232;213mgeneral\x1b[0m    \x1b[38;2;238;232;213mcommands\x1b[0m    \x1b[1;4m\x1b[38;2;38;139;210mshortcuts\x1b[0m \x1b[38;2;238;232;213m  (←/→ or tab to cycle)\x1b[0m\n" +
	"\x1b[1m\x1b[38;2;88;110;117mKeyboard Shortcuts\x1b[0m\n" +
	"  \x1b[38;2;38;139;210m/keybindings\x1b[39m \x1b[38;2;101;123;131mto customize\x1b[39m\n" +
	"\n" +
	"\x1b[38;2;38;139;210m> /\x1b[39m                               \x1b[38;2;88;110;117mOpen slash commands\x1b[39m\n" +
	"  \x1b[38;2;38;139;210m\\ + enter\x1b[39m                       \x1b[38;2;101;123;131mInsert newline fallback\x1b[39m\n" +
	"  \x1b[38;2;38;139;210malt+enter, ctrl+j, shift+enter\x1b[39m  \x1b[38;2;101;123;131mInsert newline\x1b[39m\n" +
	"  \x1b[38;2;38;139;210malt+j\x1b[39m                           \x1b[38;2;101;123;131mToggle active items\x1b[39m\n" +
	"  \x1b[38;2;38;139;210malt+m\x1b[39m                           \x1b[38;2;101;123;131mSend with opposite Queued Messages setting\x1b[39m\n" +
	"  \x1b[38;2;38;139;210malt+v, ctrl+v\x1b[39m                   \x1b[38;2;101;123;131mPaste image/video\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mctrl+c, esc\x1b[39m                     \x1b[38;2;101;123;131mGo back / dismiss\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mctrl+d\x1b[39m                          \x1b[38;2;101;123;131mExit\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mctrl+end\x1b[39m                        \x1b[38;2;101;123;131mGo to bottom\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mctrl+g\x1b[39m                          \x1b[38;2;101;123;131mOpen prompt in $EDITOR\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mctrl+home\x1b[39m                       \x1b[38;2;101;123;131mGo to top\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mctrl+k\x1b[39m                          \x1b[38;2;101;123;131mSubagent questions\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mctrl+l\x1b[39m                          \x1b[38;2;101;123;131mClear CLI screen\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mctrl+o\x1b[39m                          \x1b[38;2;101;123;131mToggle trajectory view\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mctrl+r\x1b[39m                          \x1b[38;2;101;123;131mReview artifact\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mctrl+y\x1b[39m                          \x1b[38;2;101;123;131mYank (paste from kill ring)\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mctrl+z\x1b[39m                          \x1b[38;2;101;123;131mSuspend CLI\x1b[39m\n" +
	"  \x1b[38;2;38;139;210menter\x1b[39m                           \x1b[38;2;101;123;131mSend message or confirm\x1b[39m\n" +
	"\x1b[38;2;101;123;131m  [1-18 of 25 items]\x1b[39m\n" +
	"\n" +
	"\x1b[1mKeyboard: \x1b[0m\x1b[38;2;38;139;210m↑/↓\x1b[39m \x1b[38;2;101;123;131mNavigate\x1b[39m  \x1b[38;2;38;139;210m←/→\x1b[39m \x1b[38;2;101;123;131mSwitch View\x1b[39m  \x1b[38;2;38;139;210mesc\x1b[39m \x1b[38;2;101;123;131mClose\x1b[39m\n" +
	"\n" +
	""

// VIM_BYTES is the VIM tab, byte for byte.
const VIM_BYTES = "\x1b[38;2;38;139;210mPigasus CLI\x1b[39m   \x1b[38;2;238;232;213mgeneral\x1b[0m    \x1b[38;2;238;232;213mcommands\x1b[0m    \x1b[38;2;238;232;213mshortcuts\x1b[0m    \x1b[1;4m\x1b[38;2;38;139;210mvim\x1b[0m \x1b[38;2;238;232;213m  (←/→ or tab to cycle)\x1b[0m\n" +
	"\x1b[1m\x1b[38;2;88;110;117mVim Modal Shortcuts\x1b[0m\n" +
	"  \x1b[38;2;101;123;131mStandard Vim key bindings active in Vim Mode\x1b[39m\n" +
	"\n" +
	"\x1b[38;2;38;139;210m> i, a, I, A, o, O\x1b[39m                \x1b[38;2;88;110;117mEnter Insert mode\x1b[39m\n" +
	"  \x1b[38;2;38;139;210m/ !                            I\x1b[39m\x1b[38;2;101;123;131mnsert the character, enter Insert mode\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mesc                            I\x1b[39m\x1b[38;2;101;123;131mnsert/Visual → Normal\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mh j k l                        L\x1b[39m\x1b[38;2;101;123;131meft / down / up / right\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mw b e, W B E                   W\x1b[39m\x1b[38;2;101;123;131mord / WORD motions\x1b[39m\n" +
	"  \x1b[38;2;38;139;210m0 ^ $, gg G                    L\x1b[39m\x1b[38;2;101;123;131mine and buffer bounds\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mf F t T, ; ,                   F\x1b[39m\x1b[38;2;101;123;131mind character, repeat\x1b[39m\n" +
	"  \x1b[38;2;38;139;210md c y                          D\x1b[39m\x1b[38;2;101;123;131melete / change / yank (motion or object)\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mx X D C                        D\x1b[39m\x1b[38;2;101;123;131melete char / back / to end, change to end\x1b[39m\n" +
	"  \x1b[38;2;38;139;210miw aw iW aW ip ap              T\x1b[39m\x1b[38;2;101;123;131mext objects\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mi( a( i\" a\" ...                B\x1b[39m\x1b[38;2;101;123;131mracket and quote pairs\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mv V                            V\x1b[39m\x1b[38;2;101;123;131misual / Visual line\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mp P                            P\x1b[39m\x1b[38;2;101;123;131maste after / before\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mu, U                           U\x1b[39m\x1b[38;2;101;123;131mndo / redo\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mr{char}                        R\x1b[39m\x1b[38;2;101;123;131meplace one character or the selection\x1b[39m\n" +
	"  \x1b[38;2;38;139;210m~                              T\x1b[39m\x1b[38;2;101;123;131moggle case\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mno count prefix                3\x1b[39m\x1b[38;2;101;123;131mdd deletes one line\x1b[39m\n" +
	"  \x1b[38;2;38;139;210mctrl+s, ctrl+enter             S\x1b[39m\x1b[38;2;101;123;131mubmit (Normal and Insert)\x1b[39m\n" +
	"\x1b[38;2;101;123;131m  [1-18 of 21 items]\x1b[39m\n" +
	"\n" +
	"\x1b[1mKeyboard: \x1b[0m\x1b[38;2;38;139;210m↑/↓\x1b[39m \x1b[38;2;101;123;131mNavigate\x1b[39m  \x1b[38;2;38;139;210m←/→\x1b[39m \x1b[38;2;101;123;131mSwitch View\x1b[39m  \x1b[38;2;38;139;210mesc\x1b[39m \x1b[38;2;101;123;131mClose\x1b[39m\n" +
	"\n" +
	""

// TAB_BAR_COMMANDS is the tab bar the COMMANDS tab draws.
const TAB_BAR_COMMANDS = "\x1b[38;2;38;139;210mPigasus CLI\x1b[39m   \x1b[38;2;238;232;213mgeneral\x1b[0m    \x1b[1;4m\x1b[38;2;38;139;210mcommands\x1b[0m    \x1b[38;2;238;232;213mshortcuts\x1b[0m \x1b[38;2;238;232;213m  (←/→ or tab to cycle)\x1b[0m"

// COMMANDS_HEADER is the COMMANDS tab's section header.
const COMMANDS_HEADER = "\x1b[1m\x1b[38;2;88;110;117mAvailable Commands\x1b[0m"

// versionRe matches the reference's version string, which changes on
// every release.
var versionRe = regexp.MustCompile(`Version \d+\.\d+\.\d+`)

// logTSRe matches the log file's timestamp, which changes every
// second.
var logTSRe = regexp.MustCompile(`cli-\d{8}_\d{6}\.log`)

// normaliseGeneral masks the two tokens of the GENERAL tab that
// change on their own.
func normaliseGeneral(s string) string {
	s = versionRe.ReplaceAllString(s, "Version X.Y.Z")
	return logTSRe.ReplaceAllString(s, "cli-TIMESTAMP.log")
}

// TestRenderedBytes pins the overlay's rendered bytes.
//
// The GENERAL, SHORTCUTS and VIM tabs are pinned in full. The COMMANDS
// tab renders the clone's whole slash-command catalogue, which grows
// with every new command, so it is pinned by shape instead — see
// TestCommandsTabShape.
func TestRenderedBytes(t *testing.T) {
	restore := palette.PushScheme("solarized light")
	defer restore()

	f := newFixture(false)

	f.panel.Open(0)
	if got := normaliseGeneral(f.panel.Rows()); got != GENERAL_BYTES {
		t.Errorf("the GENERAL tab changed:\ngot\n%q\nwant\n%q", got, GENERAL_BYTES)
	}

	f.panel.Open(2)
	if got := f.panel.Rows(); got != SHORTCUTS_BYTES {
		t.Errorf("the SHORTCUTS tab changed:\ngot\n%q\nwant\n%q", got, SHORTCUTS_BYTES)
	}

	f.vimOn = true
	f.panel.Open(3)
	if got := f.panel.Rows(); got != VIM_BYTES {
		t.Errorf("the VIM tab changed:\ngot\n%q\nwant\n%q", got, VIM_BYTES)
	}
}

// TestCommandsTabShape pins the COMMANDS tab's shape.
//
// What must not change is the tab's frame: the tab bar, the section
// header, the number of visible rows, the counter's format and the
// footer. The rows themselves come from the catalogue, so they are
// checked structurally rather than byte for byte.
func TestCommandsTabShape(t *testing.T) {
	restore := palette.PushScheme("solarized light")
	defer restore()

	f := newFixture(false)
	f.panel.Open(1)
	lines := strings.Split(f.panel.Rows(), "\n")

	if len(lines) != 26 {
		t.Fatalf("the COMMANDS tab has %d lines, want 26", len(lines))
	}
	if lines[0] != TAB_BAR_COMMANDS {
		t.Errorf("tab bar = %q, want %q", lines[0], TAB_BAR_COMMANDS)
	}
	if lines[1] != COMMANDS_HEADER {
		t.Errorf("header = %q, want %q", lines[1], COMMANDS_HEADER)
	}
	wantCounter := tuilib.HelpDimGrayOpen +
		fmt.Sprintf("  [1-18 of %d items]", len(slashcommands.Default)) +
		tuilib.HelpDimGrayClose
	if lines[21] != wantCounter {
		t.Errorf("counter = %q, want %q", lines[21], wantCounter)
	}
	if lines[23] != tuilib.HelpKeyboardHint {
		t.Errorf("footer = %q, want %q", lines[23], tuilib.HelpKeyboardHint)
	}
	// The cursor's row opens with the blue marker; the row below it
	// does not.
	if !strings.HasPrefix(lines[3], tuilib.HelpBlueOpen+"> ") {
		t.Errorf("the cursor row = %q", lines[3])
	}
	if strings.HasPrefix(lines[4], tuilib.HelpBlueOpen+"> ") {
		t.Errorf("the row below the cursor carries its marker: %q", lines[4])
	}
}
