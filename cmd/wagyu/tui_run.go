// tui_run.go — cycle 71 v1.3.5 binary 1:1 TUI (24-bit RGB logo +
// charm.land/bubbles/v2 textarea input component).
//
// user live run 反馈 (cycle 47): "tui 细节不一致, 自己在 tmux 里面对比两者
// tui 交互, 个别体验不一样, close the gap".
//
// user 反馈 (cycle 71): "这个输入组件的结构还是和 agy 不太一样, 它是放在
// 两条横线中间的, 而且是可以扩充大小的, 而当前实现不支持 ctrl+j 多行输入
// 而且光标的位置还是没有跟随输入的文字"
// → 之前是手写 []rune 数组: 单行, ctrl+j 不响应, 光标 X 用 1+len(buffer)
//
//	估算 (multi-byte rune / multi-line 都错位). 本 commit 改:
//	m.input 是 charm.land/bubbles/v2/textarea.Model, MaxHeight=8 +
//	DynamicHeight=true, 自带 ctrl+j / alt+←→ / ctrl+w / ctrl+a/e 等全套
//	编辑键, m.input.Cursor() 精确给出 v.Cursor.X/Y. prompt 行包在两条
//	──── 之间, 跟 /tmp/agy_real_idle.txt /tmp/agy_multiline.txt 1:1.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	sp "charm.land/bubbles/v2/spinner"
	tas "charm.land/bubbles/v2/textarea"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"golang.org/x/term"
	"unicode/utf8"

	piagent "github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/coding"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/agenttools"
	"github.com/justwasm/pigasus/internal/announce"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/branding"
	"github.com/justwasm/pigasus/internal/convstore"
	"github.com/justwasm/pigasus/internal/hooks"
	"github.com/justwasm/pigasus/internal/keybind"
	"github.com/justwasm/pigasus/internal/mdrender"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/pluginwatch"
	"github.com/justwasm/pigasus/internal/render"
	"github.com/justwasm/pigasus/internal/sandbox"
	"github.com/justwasm/pigasus/internal/sidequestion"
	"github.com/justwasm/pigasus/internal/subagent"
	"github.com/justwasm/pigasus/internal/subagentquestions"
	"github.com/justwasm/pigasus/internal/subagentrouter"
	"github.com/justwasm/pigasus/internal/subagenttools"
	"github.com/justwasm/pigasus/internal/taskregistry"
	"github.com/justwasm/pigasus/internal/tools"
	"github.com/justwasm/pigasus/internal/tuilib"
	"github.com/justwasm/pigasus/internal/vim"
	"github.com/justwasm/pigasus/internal/voice"
)

// newConversationID returns an RFC-4122-style 8-4-4-4-12 hex UUID
// for the current session. Generated once at NewRunMode time via
// crypto/rand; on the (extremely rare) failure path we fall back to a
// timestamp-based UUID so the /help general panel still renders.
//
// Cycle 86: ref binary shows "Conversation: <uuid>" in the /help
// general panel — this matches its byte-for-byte visible output (the
// actual UUID bytes differ per session by design).
func newConversationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		// Fallback: derive 16 bytes from nanosecond timestamp.
		ts := uint64(time.Now().UnixNano())
		for i := 0; i < 16; i++ {
			b[i] = byte(ts >> (uint(i) * 4))
		}
	}
	// Format as 8-4-4-4-12 hex digits per RFC 4122 v4 layout.
	hex := hex.EncodeToString(b[:])
	return hex[0:8] + "-" + hex[8:12] + "-" + hex[12:16] + "-" + hex[16:20] + "-" + hex[20:32]
}

// logoLines are the four *text* rows of the v1.3.5 binary 1:1 banner —
// the colour matrix that forms the left side of each row now lives in
// logo_palette.go (the logoArt matrix plus the renderLogoLines
// renderer).
//
// Cycle 126: row 1 ("Antigravity CLI <ver>") uses 24-bit RGB
// (38;2;66;133;244) plus bold, matching the ref binary's literal
// output (`script -q /tmp/agycmp/ref-color.txt agy`). Rows 2-4
// (account, model, cwd) use 24-bit RGB (38;2;95;99;104) instead of
// the 256-colour 241 the clone previously emitted. Cycle 88 (and
// later cycles) was correctly paired with the ref binary's escape
// sequence *of that build* (some ref builds sampled through tmux
// emit 256-colour and some emit 24-bit; both decode to the same
// visual shade in tmux's 256-colour cube, but a real terminal
// distinguishes them). The 1.3.6 binary on this machine emits
// 24-bit RGB to a TTY, so the clone now matches.
//
// Cycle 290: the per-cell ANSI sequences used to live inline here as
// `logoBlock` plus a hardcoded `logoLines[4]`. Both were moved to
// logo_palette.go as a 5-row palette-indexed logoArt matrix plus a
// renderer, so this file now only formats the text half of each
// banner row. The `%s` placeholders receive the rendered art row
// from `renderLogoRow(logoArt[i])` (16 cols wide), and 2 spaces pad
// the gutter so banner text begins at column 18.
var logoLines = []string{
	"%s  " + cBlueBoldOpen + "Pigasus CLI " + cliVersion + "\x1b[0m",
	"%s  " + cDimGrayOpen + "%s" + cDimGrayClose,
	"%s  " + cDimGrayOpen + "%s" + cDimGrayClose,
	"%s  " + cDimGrayOpen + "%s" + cDimGrayClose,
}

// separatorWidth is the v1.3.5 binary 1:1 horizontal separator
// width. Cycle 80: ref capture showed 220 chars (cycle 78 used 200
// which was wrong). The v1.3.5 binary renders two full-width
// horizontal rules around the input box — matching the terminal width
// of 220 cols when captured in a 220-col tmux pane.
const separatorWidth = 220

var separator = cRule(separatorWidth)

// separator returns the horizontal rule drawn between the banner and the
// prompt. Cycle 119: when the terminal width is known the rule is drawn at
// that width rather than the fixed 220 columns, so it never wraps (a wrapped
// separator breaks the renderer's row arithmetic).
func (m *runMode) separator() string {
	if m.width > 0 {
		return cRule(m.width)
	}
	return separator
}

const inputPrompt = "> "

// inputWidth is the textarea render width. The v1.3.5 binary's
// prompt region fills the full terminal width minus the "> " prompt
// glyph (2 chars). Cycle 80: bump 199 → 219 to match the new 220-col
// separator.
const inputWidth = 219

// statusWidth is the canonical screen width used by the status
// row pad math. Cycle 80 fix: it must agree with separatorWidth
// so the right-anchored model label lands exactly on column 220.
const statusWidth = separatorWidth

// statusRight is the literal right-side label of the status row.
// Cycle 81 fix: use ASCII '·' (middle-dot ASCII variant already
// available as U+00B7 but UTF-8 encoded is 2 bytes; we use the
// rune count = 1 via a Go string literal). The visual char is
// the same in both tmux and the v1.3.5 binary; only the byte
// length differs. We compute pad from rune count below to keep
// the math consistent across both ref and our output.
const statusRightPrefix = " \u00b7 high" // " · high" (7 runes; 8 bytes UTF-8)

// staticStatusRow is the static variant of statusRow used by
// the non-tty render path (no picker state available).
func staticStatusRow(model string) string {
	// Static status row used by the non-tty render path. The visible
	// left label is always "? for shortcuts" — picker state is not
	// available in that path.
	leftPlain := "? for shortcuts"
	left := "\033[38;2;154;160;166m" + leftPlain + "\033[39m"
	right := "\033[38;2;154;160;166m" + model + statusRightPrefix + "\033[39m"
	// Cycle 81 fix: pad math uses RUNE counts (not byte length) so
	// the right-anchored label lands exactly on column 220 even when
	// statusRightPrefix contains the UTF-8 middle-dot U+00B7 (2
	// bytes; 1 visible char).
	pad := strings.Repeat(" ", statusWidth-utf8.RuneCountInString(leftPlain)-utf8.RuneCountInString(model)-utf8.RuneCountInString(statusRightPrefix))
	return left + pad + right
}

// staticStatusRowWithTasks is the cycle-tasks-panel variant of
// staticStatusRow that renders an extra " · N tasks" segment to
// the right of the effort suffix. The non-tty render path is the
// headless / --print route; it does not have a runMode, so the
// caller passes n in directly. The pad math accounts for the
// segment so the right-anchored model label still lands on
// column 220.
func staticStatusRowWithTasks(model string, n int) string {
	taskSuffix := ""
	if n == 1 {
		taskSuffix = " \u00b7 1 task"
	} else if n > 1 {
		taskSuffix = fmt.Sprintf(" \u00b7 %d tasks", n)
	}
	leftPlain := "? for shortcuts"
	left := "\033[38;2;154;160;166m" + leftPlain + "\033[39m"
	right := "\033[38;2;154;160;166m" + model + statusRightPrefix + taskSuffix + "\033[39m"
	pad := strings.Repeat(" ", statusWidth-utf8.RuneCountInString(leftPlain)-utf8.RuneCountInString(model)-utf8.RuneCountInString(statusRightPrefix)-utf8.RuneCountInString(taskSuffix))
	return left + pad + right
}

func (m *runMode) statusRow() string {
	// Cycle status-line-script: when a statusLine.command is configured
	// *and* the user has not hidden the row, prefer the external
	// script's rendered output over the built-in template. The check
	// has to short-circuit *before* the label composition below,
	// otherwise the script's output would be thrown away. externalRenderOK
	// is nil-safe so this branch is a no-op when external mode is
	// disabled (which is the default at startup and during /statusline).
	if out, _ := m.externalRenderedForRow(); out != "" {
		return out
	}
	// Cycle 87: status row uses 256-color dim gray (38;2;101;123;131) instead

	// of 24-bit RGB (38;2;154;160;166). The ref v1.3.5 binary emits
	// 38;2;101;123;131 directly (verified via /tmp/ref_idle_status_raw.bin
	// line 19: "\x1b[38;2;101;123;131m? for shortcuts..."). Our 24-bit RGB
	// renders as 38;5;109 in tmux which is a noticeably bluer shade.
	// Both ref's left ("? for shortcuts" / "esc to cancel") and right
	// (model label + " · high") labels share the same dim gray.
	leftPlain := "? for shortcuts"
	leftOpen := cDimGrayOpen
	leftClose := cDimGrayClose
	left := leftOpen + "? for shortcuts" + leftClose
	bufferEmpty := strings.TrimSpace(m.input.Value()) == ""
	if m.picker.Visible() {
		// Picker overlay is open — ref binary swaps the left label
		// from "? for shortcuts" to "esc to cancel" so the user
		// sees the close binding inline with the status bar.
		// Pad is computed from the visible label so the right column
		// stays anchored at column 220 (cycle 75 fix; cycle 80
		// bumped width 200→220).
		// Cycle 87: "esc to cancel" uses 38;5;238 (darker gray) per
		// ref capture; the bare "esc to cancel" otherwise shares
		// 38;2;101;123;131 with "? for shortcuts".
		// Cycle 91: "esc to cancel" uses 38;2;101;123;131 (dim gray) to
		// match ref picker-state byte-for-byte. Cycle 87 used
		// 38;5;238 (darker gray) which matched the bare-idle form
		// but was off by one shade when picker was visible.
		leftPlain = "esc to cancel"
		left = leftOpen + "esc to cancel" + leftClose
	}
	// Cycle 283: while a turn is in flight the reference's left label
	// is "esc to cancel" (the gesture that interrupts the stream), not
	// "? for shortcuts". Measured from a truecolour pty capture of agy
	// 1.4.3 during a `sleep 8` turn.
	if m.streaming && !m.picker.Visible() && leftPlain != "" {
		leftPlain = "esc to cancel"
		left = leftOpen + "esc to cancel" + leftClose
	}
	// Cycle 283: a queued prompt owns the left label while it exists —
	// the reference replaces "esc to cancel" with the up-arrow hint
	// (two leading spaces included) so the user knows the message can
	// be pulled back into the input.
	if len(m.streamQueue) > 0 && !m.picker.Visible() {
		leftPlain = "  Press up to edit queued messages"
		left = leftOpen + leftPlain + leftClose
	}
	if (!bufferEmpty || m.resumeMode) && !m.picker.Visible() {
		// Cycle 82: hide the left label entirely when the user is
		// typing into the input (and picker is closed). The right
		// model label then right-anchors flush against the spacer —
		// matching the v1.3.5 binary's status-row byte-for-byte.
		// Cycle 184: the /resume picker blanks it too — REF renders
		// the row with the right-aligned model label alone.
		leftPlain = ""
		left = ""
		leftOpen = ""
		leftClose = ""
	}
	// Cycle 216: the vim mode badge. The reference's status line stores
	// the styled badge as the *start* of the left accumulator and then
	// appends the usual hint, so it reads "-- INSERT -- ? for shortcuts"
	// (the hint is blanked while typing, leaving the badge alone). Mirror
	// that by prepending the badge to whatever label survived the rules
	// above. Normal mode has no badge, so the pre-existing hint stands.
	// Only reachable when "Editor Mode › Vim" is set, so every
	// byte-verified non-vim capture is untouched.
	if badge := m.vimBadge(); badge != "" {
		badgeStyled := cDimGrayOpen + badge + cDimGrayClose
		if leftPlain != "" {
			leftPlain = badge + " " + leftPlain
			left = badgeStyled + " " + left
		} else {
			leftPlain = badge
			left = badgeStyled
		}
	}
	if m.helpMode || (m.panel.kind != panelNone && !m.panel.kind.keepStatusHint()) {
		// Cycle 84: /help overlay owns the screen — ref binary shows
		// only the right model label, no left "?" hint or "esc to
		// cancel" prompt. The keyboard hint for closing /help is
		// already inside the help panel.
		// Cycle 185: the same holds for the modal slash-command
		// panels (/mcp, /tasks, …) — /tmp/agycmp/panels/ppref.mcp.txt
		// line 19 is blank except for the right-aligned model label.
		// /codesearch is the exception: REF keeps "? for shortcuts"
		// there (ppref.codesearch.txt line 5), so it is excluded.
		leftPlain = ""
		left = ""
		leftOpen = ""
		leftClose = ""
	}
	if m.exitConfirm {
		// Cycle 144: the v1.3.6 reference requires two Ctrl+C
		// presses to exit. After the first press the left side of
		// the status row swaps to "press ctrl+c again to exit",
		// rendered in the same dim gray as "? for shortcuts" /
		// "esc to cancel". A second Ctrl+C quits; the hint
		// auto-clears after a few seconds (the auto-clear isn't
		// strictly required for parity — the reference doesn't
		// auto-clear either, the user has to either press Ctrl+C
		// again or type any other key).
		leftPlain = "press ctrl+c again to exit"
		left = leftOpen + leftPlain + leftClose
	}
	// Cycle 227: auto-compact overrides the left label. While
	// compactStart has been observed and compactEnd hasn't (active
	// state), the row shows "Compacting context… 184 K → 42 K (3 s)";
	// when the end event fires the labels revert (one render later —
	// we don't blank the row immediately because the spinner tick
	// already cleared the active flag, but the loop might still be
	// streaming the final text deltas). End events that arrived with
	// no start (an upstream /compact slash command fired before the
	// loop attached its hook) leave the row alone, since both
	// timestamps would be zero.
	if !m.compactStart.IsZero() {
		frame := usageSpinnerFrame(m.compactSpinnerFrame)
		var label string
		switch {
		case m.compactEnd.IsZero():
			// Still running. Use "Compacting context…" with the
			// upstream tokens-after if the end event fired *before*
			// the row was rebuilt (rare — the observer normalises
			// start before end), else just the spinner + word.
			if m.compactTokensAfter > 0 {
				label = frame + "  Compacting context…  " +
					tokenKMid(m.compactTokensBefore) + " → " +
					tokenKMid(m.compactTokensAfter) + " (" +
					compactDuration(m.compactStart, time.Now()) + ")"
			} else {
				label = frame + "  Compacting context… (" +
					compactDuration(m.compactStart, time.Now()) + ")"
			}
		default:
			label = "Compacted " +
				tokenKMid(m.compactTokensBefore) + " → " +
				tokenKMid(m.compactTokensAfter) + " (" +
				compactDuration(m.compactStart, m.compactEnd) + ")"
		}
		leftPlain = label
		left = leftOpen + label + leftClose
	}
	// Cycle 80: the status row's right label uses the *base* model
	// name (no parenthetical tier suffix), e.g. "Gemini 3.8 Flash"
	// instead of the banner's "Gemini 3.8 Flash (High)". This matches
	// the v1.3.5 binary's status row byte-for-byte; only the banner
	// row carries the tier in parentheses.
	statusModel := m.statusModel
	if statusModel == "" {
		statusModel = m.model
	}
	// Cycle 283: the reference prefixes the model label with the active
	// execution mode in a per-mode colour — auto #CB4B16, plan #268BD2,
	// accept-edits #859900 — and prints the bare model in the default
	// mode. Verified from a truecolour pty capture of agy 1.4.3:
	//
	//	\x1b[38;2;203;75;22mauto\x1b[38;2;101;123;131m · MiniMax M3
	//
	// modePlain keeps the padLen math below honest (the escapes are
	// zero-width), modeStyled carries the same text with the colour.
	modePlain, modeStyled := "", ""
	if m.mode != "" {
		modePlain = m.mode + " · "
		modeStyled = statusModeColor(m.mode) + m.mode + cDimGrayOpen + " · "
	}
	// Cycle 182: the right label always carries the dim-gray wrap, even
	// when the left label is blanked (typing / picker / help overlay).
	// Before this it reused leftOpen/leftClose, which the blanking
	// branches set to "" — so the /help overlay's model label lost its
	// colour while REF keeps it (`\x1b[38;2;101;123;131mGemini 3.8 Flash ·
	// high`). The idle row is unchanged because leftOpen was already
	// cDimGrayOpen there.
	// Cycle 208: the " · <effort>" suffix tracks the live effort (REF
	// shows "· medium" after Enter on /effort), not the constant.
	suffix := m.statusEffortSuffix()
	// Cycle tasks-panel: the "· N tasks" segment lives to the
	// right of the effort suffix. tasksStatusSuffix() returns ""
	// when N == 0 so the segment is hidden — matching the
	// reference's behaviour before cycle 230 ("no segment when
	// no tasks") and the changelog's "improved the status line
	// to continue to show background tasks that had already
	// finished" (which adds the *just-finished* counter in the
	// reference; this cycle ships the running counter only —
	// see deliverable-tasks.md §存疑 for the trade-off).
	taskSuffix := m.tasksStatusSuffix()
	right := modeStyled + cDimGrayOpen + statusModel + suffix + taskSuffix + cDimGrayClose
	// A markdown agent active in this session wraps the model label:
	//   auto · zeta (Gemini 3.8 Flash · high)  /agents
	// The trailing /agents is the reference's shortcut back to the agent
	// picker. Verified live against agy 1.4.3 at 120x40 after Esc from a
	// prepared /agents selection.
	agentPartPlain := ""
	if m.agentName != "" {
		agentPartPlain = m.agentName + " (" + statusModel + suffix + taskSuffix + ")  /agents"
		right = modeStyled + cDimGrayOpen + agentPartPlain + cDimGrayClose
	}
	// Cycle 244: when queueMode is on (alt+m flipped the toggle), an
	// additional " · Queue mode" suffix sits on the right side so
	// the user can see at a glance that Enter inserts a newline and
	// Cmd+Enter sends. Suffix form mirrors the cycle-227 compact
	// notes' style (same dim-gray wrap, same separator). The
	// padLen below uses leftPlain + statusModel + suffix to keep
	// the right label flush against the terminal's last column;
	// the queue-mode suffix rides on the same right segment so
	// the pad math stays correct.
	if m.queueMode {
		if m.agentName != "" {
			agentPartPlain += " · Queue mode"
			right = modeStyled + cDimGrayOpen + agentPartPlain + cDimGrayClose
		} else {
			right = modeStyled + cDimGrayOpen + statusModel + suffix + taskSuffix +
				" · Queue mode" + cDimGrayClose
		}
	}
	// Cycle 119: the row is padded to the terminal width when it is known
	// (previously always 220 columns), so the right-anchored model label
	// lands on the terminal's last column instead of wrapping. The pad is
	// floored at 1 so a narrow terminal cannot produce a negative repeat.
	width := statusWidth
	if m.width > 0 {
		width = m.width
	}
	rightPlainLen := utf8.RuneCountInString(statusModel) + utf8.RuneCountInString(suffix) + utf8.RuneCountInString(taskSuffix)
	if m.agentName != "" {
		rightPlainLen = utf8.RuneCountInString(agentPartPlain)
	}
	padLen := width - utf8.RuneCountInString(leftPlain) - utf8.RuneCountInString(modePlain) - rightPlainLen
	if padLen < 1 {
		padLen = 1
	}
	// Known byte-level deviation: with a Markdown agent active the
	// reference pads the right segment with "\t " (tab + space) where
	// the clone emits four spaces. Bubble Tea expands tabs before the
	// frame reaches the terminal, so the visible column is identical;
	// see docs/cycles/cycle-288-at-picker-context/deliverable.md §存疑.
	pad := strings.Repeat(" ", padLen)
	return left + pad + right
}

func renderBanner(w io.Writer, email, model, tier, cwd string) {
	// Cycle 290: rows 0-3 of `logoArt` form the left side of the first
	// four banner rows; `logoLines[i]` is the text-half format string
	// paired with art row i. Row 4 is the decorative underline
	// (logoArt[4]); its presence depends on AGY_CLI_HIDE_LOGO.
	artRows := renderLogoLines()
	fmt.Fprintf(w, "%s  "+cBlueBoldOpen+"%s "+cliVersion+"\x1b[0m\n", artRows[0], branding.Current().CLIName)
	// Cycle 146: tier annotation on the email row is conditional,
	// NOT removed. The v1.3.6 reference default is
	// tier = "Antigravity Starter Quota"; the email row renders as
	//   "navigaid@gmail.com (Antigravity Starter Quota)"
	// when the user is on the free tier. Earlier cycles (139, 145)
	// stripped the default tier and forced the email row to bare;
	// that was based on a stale capture where the tier happened to
	// be unset. With the default restored, the email row reads
	// "navigaid@gmail.com (Antigravity Starter Quota)" and the
	// model row reads "Gemini 3.8 Flash (High)" (the tier there is
	// the parenthetical "High", which the model string already
	// carries — the cycle-145 conditional handles both cases).
	emailLine := email
	if tier != "" {
		emailLine += " (" + tier + ")"
	}
	fmt.Fprintf(w, logoLines[1]+"\n", artRows[1], emailLine)
	// Model row (cycle 145/146): the v1.3.6 reference puts the
	// active model name on banner row 3. The model string already
	// carries the effort tier in its parenthetical (e.g.
	// "Gemini 3.8 Flash (High)") — that is *not* the same field
	// as the email-row plan tier ("Antigravity Starter Quota").
	// We render the model string verbatim; the plan tier lives
	// on the email row above. The status row still shows the base
	// model without the parenthetical, so this row is the only
	// place the full "<model> (<effort>)" string appears.
	fmt.Fprintf(w, logoLines[2]+"\n", artRows[2], model)
	fmt.Fprintf(w, logoLines[3]+"\n", artRows[3], shortenCwd(cwd))
	// Cycle 255: the AGY 1.1.19 changelog added AGY_CLI_HIDE_LOGO so
	// the user can suppress the decorative 24-bit-RGB ASCII-art row
	// on narrow terminals, screen readers and recordings. The flag
	// is consulted at every render so a runtime mutation (e.g.
	// t.Setenv during testing) is observable without re-importing
	// the package; the default behaviour (no env var) keeps the
	// art row so the test surface that pins "Antigravity CLI"
	// inside View() output (cmd/wagyu/tui_cycle124_test.go:100,116)
	// still passes byte-for-byte. The trailing "\n" is preserved
	// either way so scrollHeaderRows' split on "\n" still yields
	// the empty "blank between banner and top sep" element it
	// expects — the difference is one fewer rendered row.
	if !envHideLogoOn() {
		fmt.Fprintln(w, artRows[4])
	} else {
		fmt.Fprintln(w)
	}
}

// shortenCwd replaces the leading $HOME with "~" so the banner
// displays "~/GitHub/openagy" instead of "/Users/gear/GitHub/openagy",
// matching the v1.3.5 binary byte-for-byte (cycle 79). Cycles when
// cwd is outside $HOME leave it untouched.
func shortenCwd(cwd string) string {
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return cwd
	}
	if cwd == home {
		return "~"
	}
	if strings.HasPrefix(cwd, home+"/") {
		return "~" + cwd[len(home):]
	}
	return cwd
}

// stripTier removes a parenthesised tier suffix from a model
// display name. e.g. "Gemini 3.8 Flash (High)" → "Gemini 3.8 Flash".
// Cycle 80: the v1.3.5 binary's status row renders the *base* name
// without the tier in parentheses; only the banner row carries
// "Gemini 3.8 Flash (High)". This helper splits the two so the
// right-anchored status label matches v1.3.5 byte-for-byte. Models
// without a parenthetical suffix pass through unchanged.
func stripTier(model string) string {
	if i := strings.LastIndex(model, " ("); i >= 0 {
		if model[len(model)-1] == ')' {
			return model[:i]
		}
	}
	return model
}

// renderAgentWarning is the reference's ⚠ Agent Not Found card. Its shape is
// renderWarning's (orange header, blue ⎿ bullet, dim body) with a different
// fixed title and caller-supplied body; captured from agy 1.4.3 under a pty.
func renderAgentWarning(body string) string {
	var b strings.Builder
	b.WriteString(cWarningOpen)
	b.WriteString("⚠ Agent Not Found")
	b.WriteString(cWarningClose)
	b.WriteString("\n")
	b.WriteString(cWarningBlueOpen)
	b.WriteString("  ⎿  ")
	b.WriteString(cWarningBlueClose)
	b.WriteString(cWarningBodyOpen)
	b.WriteString(body)
	b.WriteString(cWarningBodyClose)
	b.WriteString("\n")
	return b.String()
}

// renderWarning emits the cycle-81 fallback warning block that
// the v1.3.6 binary renders when a configured model is unavailable:
//
//	⚠ Warning (24-bit orange RGB)
//	  ⎿  "<from>" is no longer available. Using "<to>".
//
// Cycle 126: the v1.3.6 binary on this machine emits 24-bit RGB
// escapes (38;2;...), not the 256-colour fallback (38;5;...) the
// cycle-81 capture picked up. See colors.go for the recovered values:
//
//   - header:   38;2;203;75;22   (#CB4B16, orange)
//   - bullet:   38;2;38;139;210 + ESC[1m (bold bright blue) for "  ⎿  "
//   - body:     38;2;88;110;117 + ESC[22m (#586E75, dim gray with bold off)
//
// The bullet's bold does NOT carry onto the body because the body
// open includes ESC[22m — that is the literal byte sequence in the
// 1.4.3 capture (`38;2;88;110;117;22m` between the "⎿" and the
// first quote), so we reproduce it.
//
// The 2 spaces of indent match the ref capture's
// "  ⎿  \"MiniMax-M3\" is no longer available. Using \"Gemini 3.8 Flash (High)\"."
// byte-for-byte.
func renderWarning(from, to string) string {
	var b strings.Builder
	// Row 1: header. 24-bit RGB orange.
	b.WriteString(cWarningOpen)
	b.WriteString("⚠ Warning")
	b.WriteString(cWarningClose)
	b.WriteString("\n")
	// Row 2: body. Bold blue bullet + 2-space indent, dim gray text.
	b.WriteString(cWarningBlueOpen)
	b.WriteString("  ⎿  ")
	b.WriteString(cWarningBlueClose)
	b.WriteString(cWarningBodyOpen)
	b.WriteString("\"")
	b.WriteString(from)
	b.WriteString("\" is no longer available. Using \"")
	b.WriteString(to)
	b.WriteString("\".")
	b.WriteString(cWarningBodyClose)
	b.WriteString("\n")
	// Cycle 182: no third row. The pre-182 version appended an extra
	// "\n" here, which — combined with the row split in transcriptRows
	// — produced TWO blank rows between the warning body and the top
	// separator. REF v1.4.0 has exactly one:
	//   ⚠ Warning
	//     ⎿  "MiniMax-M3" is no longer available. ...
	//   <blank>
	//   ────────────────
	// The single trailing "\n" above yields one empty split element,
	// which is the one blank row.
	return b.String()
}

// Cycle 85 / 126: tab bar ANSI escape sequences observed in ref binary
// (capture /tmp/ref_tab_bar_*_raw.bin). All three layouts share the
// same prefix/suffix, only the "active tab" word is wrapped in
// bold+underline (\x1b[1;4m) + true-colour blue (38;2;66;133;244).
// Inactive tabs and the trailing hint use the recovered separator
// gray (38;2;218;220;224) — same shade as the row separators.
// "Antigravity CLI" label itself is plain true-colour blue. Two
// spaces between "CLI" and the first tab word.
const (
	tabBarBrandOpen     = cBlueOpen
	tabBarBrandClose    = cBlueClose
	tabBarActiveOpen    = "\x1b[1;4m" + cBlueOpen
	tabBarActiveClose   = "\x1b[0m"
	tabBarInactiveOpen  = cSeparatorOpen
	tabBarInactiveClose = cSeparatorClose
	tabBarHintOpen      = cSeparatorOpen
	tabBarHintClose     = cSeparatorClose
	tabBarHintSuffix    = "  (\u2190/\u2192 or tab to cycle)"
)

// renderTabBar emits the cycle-85 styled tab bar. The active tab
// word is wrapped in bold+underline+blue, inactive tabs in dim gray,
// the brand label in plain blue, and the trailing hint in dim gray.
// Matches the v1.3.5 binary byte-for-byte (verified against
// /tmp/ref_tab_bar_*_raw.bin captures for all three states).
//
// The three default tabs are fixed; renderTabBarTabs generalises the
// layout for the vim build, which appends a fourth "vim" tab only when
// "Editor Mode › Vim" is on (cycle 211).
func renderTabBar(active int) string {
	return renderTabBarTabs(active, []string{"general", "commands", "shortcuts"})
}

// renderTabBarTabs emits the tab bar for an arbitrary tab list. Two
// spaces of padding separate each tab word and the trailing hint.
func renderTabBarTabs(active int, tabs []string) string {
	var b strings.Builder
	b.WriteString(tabBarBrandOpen)
	b.WriteString(branding.Current().CLIName)
	b.WriteString(tabBarBrandClose)
	b.WriteString("   ")
	for i, name := range tabs {
		if i > 0 {
			b.WriteString("    ") // 4 spaces between tabs
		}
		if i == active {
			b.WriteString(tabBarActiveOpen)
			b.WriteString(name)
			b.WriteString(tabBarActiveClose)
		} else {
			b.WriteString(tabBarInactiveOpen)
			b.WriteString(name)
			b.WriteString(tabBarInactiveClose)
		}
	}
	b.WriteString(" ")
	b.WriteString(tabBarHintOpen)
	b.WriteString(tabBarHintSuffix)
	b.WriteString(tabBarHintClose)
	return b.String()
}

// renderHelp emits the cycle-83 /help TUI menu panel content.
//
// Layout (when rendered inside View() between the response area and
// the bottom separator):
//
//	tab 0 (general):
//	  Antigravity CLI   general    commands    shortcuts   (←/→ or tab to cycle)
//	  Antigravity CLI understands your codebase, makes edits with your permission,
//	  and executes commands — right from your terminal.
//
//	  Version 1.3.5
//	  navigaid@gmail.com
//	  Workspace:      ~/GitHub/openagy
//	  Project Config: ~/.gemini/config/projects/default-cli-project.json
//	  Log File:       ~/.gemini/antigravity-cli/log/cli-<TS>.log
//
//	  Quick Reference
//	    /  Type / to see available commands
//
//	  Keyboard: ↑/↓ Navigate  ←/→ Switch View  esc Close
//
//	tab 1 (commands): full catalog list, each row "<cmd> (<aliases>)  <description>"
//	tab 2 (shortcuts): keyboard binding list
//
// Returns a string with embedded '\n' so View() can write it
// directly to its bytes.Buffer.
func renderHelp(tab int, model, tier, cwd, email, conversationID string, m *runMode) string {
	// Cycle 83: shortenCwd so the Workspace row matches the v1.3.5
	// binary's "~/GitHub/openagy" output (cycle 79 parity). The banner
	// already calls shortenCwd before rendering — the help panel
	// needs the same treatment or it shows the full $HOME-prefixed
	// path which differs from the ref by ~12 bytes.
	// Cycle 216: the tab list grows a fourth "vim" entry only when vim
	// mode is enabled, so the default three-tab bar is byte-identical to
	// the captured reference.
	tabs := helpTabs(m)
	if tab < 0 || tab >= len(tabs) {
		tab = 0
	}
	var b strings.Builder
	// Cycle 85: tab bar with ANSI styling (active bold+underline+blue).
	b.WriteString(renderTabBarTabs(tab, tabs))
	b.WriteString("\n")
	switch tab {
	case 0:
		renderHelpGeneral(&b, model, tier, shortenCwd(cwd), email, conversationID)
	case 1:
		renderHelpCommands(&b, m)
	case 2:
		renderHelpShortcuts(&b, m)
	case 3:
		renderHelpVim(&b, m)
	}
	return b.String()
}

// Cycle 86 / 126: /help general panel ANSI styling constants. The
// v1.3.6 ref binary on this machine emits 24-bit RGB escapes to a
// TTY (see colors.go):
//   - description/metadata rows: dim gray (38;2;95;99;104)
//   - Quick Reference header: bold + dim gray (1m + 38;2;60;64;67).
//     The bold is set inside helpHeaderOpen itself; there is no
//     preceding bold escape, so the help header does NOT need the
//     ESC[22m (bold-off) the warning body escape carries.
//   - Quick Reference "/" symbol: blue (38;2;66;133;244)
//   - Keyboard hint: bold "Keyboard:" prefix, blue keys, dim gray labels
//
// Cycle 86 also adds a "Conversation: <uuid>" row that ref shows
// after the Log File row. The UUID is generated per-session at
// startup so the user can identify their conversation in
// logs/support tickets.
const (
	helpDimGrayOpen  = cDimGrayOpen
	helpDimGrayClose = cDimGrayClose
	helpHeaderOpen   = "\x1b[1m\x1b[38;2;88;110;117m"
	helpHeaderClose  = "\x1b[0m"
	helpBlueOpen     = cBlueOpen
	helpBlueClose    = cBlueClose
	// cHelpDesc* is the darker description gray used on the COMMANDS
	// tab (every row) and on the SHORTCUTS cursor row. rgb(60,64,67) —
	// the section-header shade minus the bold. REF v1.4.0 PTY capture:
	//   '\x1b[38;2;60;64;67mAdd a directory to the workspace\x1b[m'
	// The SHORTCUTS *non-cursor* rows use the lighter helpDimGray
	// (rgb 95,99,104) instead — verified at
	//   '  \x1b[38;2;66;133;244m\\ + enter\x1b[m...\x1b[38;2;95;99;104mInsert newline fallback\x1b[m'.
	cHelpDescOpen  = "\x1b[38;2;88;110;117m"
	cHelpDescClose = "\x1b[39m"
)

// helpKeyboardHint is the "Keyboard: ↑/↓ Navigate  ←/→ Switch View
// esc Close" footer shared by the COMMANDS and SHORTCUTS tabs.
// Recovered from REF v1.4.0: bold "Keyboard:" label, blue key glyphs,
// rgb(95,99,104) labels.
var helpKeyboardHint = "\x1b[1m" + "Keyboard: " + "\x1b[0m" +
	helpBlueOpen + "\u2191/\u2193" + helpBlueClose + " " +
	helpDimGrayOpen + "Navigate" + helpDimGrayClose + "  " +
	helpBlueOpen + "\u2190/\u2192" + helpBlueClose + " " +
	helpDimGrayOpen + "Switch View" + helpDimGrayClose + "  " +
	helpBlueOpen + "esc" + helpBlueClose + " " +
	helpDimGrayOpen + "Close" + helpDimGrayClose

// renderHelpGeneral — the default /help overview panel matching
// ref binary's line 13-30 layout byte-for-byte.
func renderHelpGeneral(b *strings.Builder, model, tier, cwd, email, conversationID string) {
	// Cycle 85: tab bar is now emitted by renderHelp above this
	// switch with proper ANSI styling — only the panel body is
	// rendered here. Cycle 86 wraps description + metadata blocks
	// in dim gray (38;2;101;123;131), Quick Reference header in bold+darker
	// gray (1m + 38;5;238), and Quick Reference / in blue.
	// Description block (3 lines: 2 visible + 1 blank).
	b.WriteString(helpDimGrayOpen)
	b.WriteString(branding.Current().CLIName + " understands your codebase, makes edits with your permission,")
	b.WriteString(helpDimGrayClose)
	b.WriteString("\n")
	b.WriteString(helpDimGrayOpen)
	b.WriteString("and executes commands \u2014 right from your terminal.")
	b.WriteString(helpDimGrayClose)
	b.WriteString("\n")
	b.WriteString("\n")
	// Metadata block.
	b.WriteString(helpDimGrayOpen)
	b.WriteString("Version " + cliVersion)
	b.WriteString(helpDimGrayClose)
	b.WriteString("\n")
	// Cycle 145: the /help GENERAL email row is bare —
	// same as the banner email row. The tier annotation lives on
	// the banner model row, not on either email row. Captured
	// cold_start / help_tabs ref output is
	//   Version 1.3.6
	//   navigaid@gmail.com
	//   Workspace:      ~/GitHub/openagy
	//
	// Cycle 182: that was a misreading of a stale capture. REF v1.4.0
	// renders the plan tier on the GENERAL email row exactly like the
	// banner does — 'navigaid@gmail.com (Antigravity Starter Quota)'
	// (raw capture: ref_help_general_raw.txt line 18, and the same
	// string on banner row 2). Both rows now share the renderBanner
	// rule: append " (<tier>)" only when the tier is non-empty.
	b.WriteString(helpDimGrayOpen)
	b.WriteString(email)
	if tier != "" {
		b.WriteString(" (" + tier + ")")
	}
	b.WriteString(helpDimGrayClose)
	b.WriteString("\n")
	b.WriteString(helpDimGrayOpen)
	b.WriteString("Workspace:      ")
	b.WriteString(cwd)
	b.WriteString(helpDimGrayClose)
	b.WriteString("\n")
	b.WriteString(helpDimGrayOpen)
	b.WriteString("Project Config: ~/.gemini/config/projects/default-cli-project.json")
	b.WriteString(helpDimGrayClose)
	b.WriteString("\n")
	b.WriteString(helpDimGrayOpen)
	b.WriteString("Log File:       ~/.gemini/antigravity-cli/log/cli-" + formatLogTS() + ".log")
	b.WriteString(helpDimGrayClose)
	b.WriteString("\n")
	// agy 1.4.3 prints the conversation UUID between Log File and the
	// blank row. Cycle 138 removed it from a 1.3.6 capture; the live
	// 1.4.3 panel restored it (captured 2026-10-20 at 120x40).
	if conversationID != "" {
		b.WriteString(helpDimGrayOpen)
		b.WriteString("Conversation:   ")
		b.WriteString(conversationID)
		b.WriteString(helpDimGrayClose)
		b.WriteString("\n")
	}
	b.WriteString("\n")
	// Quick Reference section. Header is bold + darker gray.
	b.WriteString(helpHeaderOpen)
	b.WriteString("Quick Reference")
	b.WriteString(helpHeaderClose)
	b.WriteString("\n")
	// Quick Reference item: '/' in blue, text in dim gray.
	b.WriteString("  ")
	b.WriteString(helpBlueOpen)
	b.WriteString("/")
	b.WriteString(helpBlueClose)
	b.WriteString("  ")
	b.WriteString(helpDimGrayOpen)
	b.WriteString("Type / to see available commands")
	b.WriteString(helpDimGrayClose)
	b.WriteString("\n")
	b.WriteString("\n")
	// Keyboard hint row at bottom of panel. Bold "Keyboard:" prefix,
	// blue key glyphs (↑/↓, ←/→, esc), dim gray labels.
	b.WriteString("\x1b[1m")
	b.WriteString("Keyboard: ")
	b.WriteString("\x1b[0m")
	b.WriteString(helpBlueOpen)
	b.WriteString("\u2191/\u2193")
	b.WriteString(helpBlueClose)
	b.WriteString(" ")
	b.WriteString(helpDimGrayOpen)
	b.WriteString("Navigate")
	b.WriteString(helpDimGrayClose)
	b.WriteString("  ")
	b.WriteString(helpBlueOpen)
	b.WriteString("\u2190/\u2192")
	b.WriteString(helpBlueClose)
	b.WriteString(" ")
	b.WriteString(helpDimGrayOpen)
	b.WriteString("Switch View")
	b.WriteString(helpDimGrayClose)
	b.WriteString("  ")
	b.WriteString(helpBlueOpen)
	b.WriteString("esc")
	b.WriteString(helpBlueClose)
	b.WriteString(" ")
	b.WriteString(helpDimGrayOpen)
	b.WriteString("Close")
	b.WriteString(helpDimGrayClose)
	b.WriteString("\n")
	// Cycle 182: one blank row between the keyboard hint and whatever
	// follows (the status row in the /help overlay). REF v1.4.0 puts
	// the right-aligned model label at row 28 of a 30-row terminal,
	// directly under this blank:
	//   26  Keyboard: ↑/↓ Navigate  ←/→ Switch View  esc Close
	//   27  <blank>
	//   28                              Gemini 3.8 Flash · high
	b.WriteString("\n")
}

// renderHelpCommands — the full slash command catalog. Each row
// is left-aligned: '  <name><aliases-padded>  <description>'. Ref
// binary renders an 18-row window; cycle 182 scrolls it with the
// cursor (see helpListDown in history.go).
//
// Cursor/window live on m (the runMode); the renderer reads them to
// slice the catalog and emit the "[N-N of N items]" footer.
func renderHelpCommands(b *strings.Builder, m *runMode) {
	// Cycle 85: tab bar now emitted by renderHelp above. Section
	// title + bottom keyboard hint still rendered here.
	// Cycle 182: header + page indicator now carry the reference's
	// ANSI styling (bold rgb(60,64,67) header, rgb(95,99,104) footer).
	b.WriteString(helpHeaderOpen)
	b.WriteString("Available Commands")
	b.WriteString(helpHeaderClose)
	b.WriteString("\n\n")
	// Cycle 147 (reverted): the picker overlay filters out 5
	// chrome-devtools-plugin:* plugin entries (cycle 135) but the
	// /help COMMANDS tab keeps all 99 catalog entries. Captured
	// /tmp/agycmp/hotpath/help_tabs/ref.001-second.txt shows
	// "[1-18 of 99 items]" — same as the catalog. Filtering
	// /help down to 94 (cycle 147 first attempt) was wrong.
	//
	// Cycle 184: the COMMANDS tab is a flat alphabetical sort of the
	// catalog; the "/" picker is not. See tuilib.SortedSlashCommands
	// for the capture pair that proves the two orders differ.
	cmds := tuilib.SortedSlashCommands()
	total := len(cmds)
	m.helpListClampWindow(total)
	start := m.helpWindow
	visible := helpListVisible(total, start, m.helpListWindowSize())
	for i := 0; i < visible; i++ {
		idx := start + i
		row := buildCommandRow(cmds[idx], idx == m.helpCursor, m.width)
		b.WriteString(row)
		b.WriteString("\n")
	}
	// Page indicator: "[N-N of N items]" matching the ref capture
	// at /tmp/agycmp/hotpath/help_tabs/ref.001-second.txt.
	from := start + 1
	to := start + visible
	fmt.Fprintf(b, "%s  [%d-%d of %d items]%s\n", helpDimGrayOpen, from, to, total, helpDimGrayClose)
	b.WriteString("\n")
	b.WriteString(helpKeyboardHint)
	b.WriteString("\n")
	// Cycle 182: blank row between the keyboard hint and the status
	// row (see renderHelpGeneral for the row accounting).
	b.WriteString("\n")
}

// renderHelpShortcuts — keyboard binding catalog. Hardcoded list
// matching ref binary's shortcuts tab capture. Cycle 182: uses the
// same 18-row scrolling window as the COMMANDS tab (see helpListDown
// in history.go). Page size 18, `[N-N of N items]` indicator.
//
// Row styling (verified against REF v1.4.0 at 220×30):
//
//	normal   "  " + blue(key) + pad + 241(desc)
//	cursor   blue("> " + key) + pad + 238(desc)
//
// i.e. the `> ` marker shares the key's blue wrap on the cursor row,
// and the description switches from 241 (normal) to 238 (cursor).
// Description column is 34 (2-char marker + 32-char key field).
//
// Source-of-truth: /tmp/agycmp/ref_help_shortcuts_raw.txt
// shows "Keyboard Shortcuts\n  /keybindings to customize\n\n" + 18
// rows + "  [1-18 of 34 items]" + "\n\nKeyboard: ...". The clone
// pre-cycle-137 dumped all 34 rows in one shot, which scrolled the
// page indicator and the trailing keyboard hint off-screen.
func renderHelpShortcuts(b *strings.Builder, m *runMode) {
	// Cycle 85: tab bar now emitted by renderHelp above. Section
	// title + bottom keyboard hint still rendered here.
	b.WriteString(helpHeaderOpen)
	b.WriteString("Keyboard Shortcuts")
	b.WriteString(helpHeaderClose)
	b.WriteString("\n")
	// "  /keybindings to customize" — the /keybindings token is blue,
	// the rest dim gray. REF v1.4.0 capture:
	//   '  \x1b[38;5;69m/keybindings\x1b[39m \x1b[38;2;101;123;131mto customize\x1b[39m'
	b.WriteString("  ")
	b.WriteString(helpBlueOpen)
	b.WriteString("/keybindings")
	b.WriteString(helpBlueClose)
	b.WriteString(" ")
	b.WriteString(helpDimGrayOpen)
	b.WriteString("to customize")
	b.WriteString(helpDimGrayClose)
	b.WriteString("\n\n")
	total := len(helpShortcuts)
	m.helpListClampWindow(total)
	start := m.helpWindow
	visible := helpListVisible(total, start, m.helpListWindowSize())
	for i := 0; i < visible; i++ {
		idx := start + i
		b.WriteString(buildShortcutRow(helpShortcuts[idx], idx == m.helpCursor))
		b.WriteString("\n")
	}
	// Page indicator — same format + 241 gray as the COMMANDS tab.
	from := start + 1
	to := start + visible
	fmt.Fprintf(b, "%s  [%d-%d of %d items]%s\n", helpDimGrayOpen, from, to, total, helpDimGrayClose)
	b.WriteString("\n")
	b.WriteString(helpKeyboardHint)
	b.WriteString("\n")
	// Cycle 182: blank row between the keyboard hint and the status
	// row (see renderHelpGeneral for the row accounting).
	b.WriteString("\n")
}

// buildShortcutRow formats one SHORTCUTS-tab row with the v1.4.0
// reference's exact ANSI styling. Rows in helpShortcuts are stored as
// `marker(2) + keyField(32) + description` plain text; this function
// splits them and re-emits the marker/key/description with the right
// colour wrap for the cursor state:
//
//	normal: "  " + blue(key) + pad + 241(desc)
//	cursor: blue("> " + key) + pad + 238(desc)
func buildShortcutRow(row string, highlighted bool) string {
	if len(row) < 2 {
		return row
	}
	// Rows are laid out on a fixed grid: marker (2) + key field (32) +
	// description. Split by *column position*, not by the first space
	// run — several keys contain spaces (`\ + enter`) or commas
	// (`alt+enter, ctrl+j, shift+enter`), so a space-based split
	// truncates them. The reference's own PTY output confirms the
	// description always starts at column 35 (1-based):
	//   '  \x1b[38;2;66;133;244malt+enter, ctrl+j, shift+enter\x1b[m  \x1b[38;2;95;99;104mInsert newline\x1b[m'
	// (2 marker + 30 key + 2 separator = 34 cells, description next).
	const keyFieldWidth = 32
	runes := []rune(row)
	keyEnd := 2 + keyFieldWidth
	if keyEnd > len(runes) {
		keyEnd = len(runes)
	}
	key := strings.TrimRight(string(runes[2:keyEnd]), " ")
	desc := ""
	if keyEnd < len(runes) {
		desc = strings.TrimLeft(string(runes[keyEnd:]), " ")
	}
	// keyField width is 32 (the reference pads the key column to 32
	// visible chars; marker adds the 2 that make the description
	// column land at 34).
	pad := keyFieldWidth - utf8.RuneCountInString(key)
	if pad < 0 {
		pad = 0
	}
	var b strings.Builder
	if highlighted {
		b.WriteString(helpBlueOpen)
		b.WriteString("> ")
		b.WriteString(key)
		b.WriteString(helpBlueClose)
	} else {
		b.WriteString("  ")
		b.WriteString(helpBlueOpen)
		b.WriteString(key)
		b.WriteString(helpBlueClose)
	}
	b.WriteString(strings.Repeat(" ", pad))
	if desc != "" {
		if highlighted {
			b.WriteString(cHelpDescOpen)
		} else {
			b.WriteString(helpDimGrayOpen)
		}
		b.WriteString(desc)
		if highlighted {
			b.WriteString(cHelpDescClose)
		} else {
			b.WriteString(helpDimGrayClose)
		}
	}
	return b.String()
}

// helpShortcuts is the keyboard binding catalog displayed in the
// /help SHORTCUTS tab. Order matches ref binary's right-2 capture.
var helpShortcuts = []string{
	"> /                               Open slash commands",
	"  \\ + enter                       Insert newline fallback",
	"  alt+enter, ctrl+j, shift+enter  Insert newline",
	"  alt+j                           Manage subagent",
	"  alt+m                           Send with opposite Queued Messages setting",
	"  alt+v, ctrl+v                   Paste image/video",
	"  ctrl+_, ctrl+shift+-            Undo",
	"  ctrl+c, esc                     Go back / dismiss",
	"  ctrl+d                          Exit",
	"  ctrl+end                        Go to bottom",
	"  ctrl+g                          Open prompt in $EDITOR",
	"  ctrl+home                       Go to top",
	"  ctrl+k                          Approve subagent fast",
	"  ctrl+l                          Clear CLI screen",
	"  ctrl+o                          Toggle trajectory view",
	"  ctrl+r                          Review artifact",
	"  ctrl+shift+z                    Redo",
	"  ctrl+y                          Yank (paste from kill ring)",
	"  ctrl+z                          Suspend CLI",
	"  down                            Move down",
	"  enter                           Send message or confirm",
	"  f2                              Rename",
	"  f4                              Delete",
	"  f5                              Start voice dictation",
	"  left                            Move left / Prev tab",
	"  pgdown                          Page down",
	"  pgup                            Page up",
	"  right                           Move right / Next tab",
	"  shift+down                      Half page down",
	"  shift+tab                       Cycle mode",
	"  shift+up                        Half page up",
	"  shift/alt+click                 Select and copy text",
	"  tab                             Tab",
	"  up                              Move up",
}

// buildCommandRow formats one catalog row for the COMMANDS tab with the
// v1.4.0 reference's exact ANSI styling:
//
//	cursor: blue("> " + name + aliases) + pad + cHelpDesc(description)
//	normal: "  " + name + aliases          + pad + cHelpDesc(description)
//
// The name column is padded to 51 visible chars — the reference's
// description column, measured with ref_desc_col.py at widths
// 60/80/100/120/220 (the description always begins at 0-based column
// 51, i.e. 52 1-based).
//
// Cycle 182: added the ANSI styling. Before this the clone emitted the
// row as plain text, so the whole COMMANDS tab was uncoloured while REF
// wraps the cursor name in brand blue and every description in
// rgb(60,64,67). Verified against a PTY capture (COLORTERM=truecolor):
//
//	'\x1b[38;2;66;133;244m> /add-dir\x1b[39m ... \x1b[38;2;60;64;67mAdd a directory to the workspace\x1b[39m'
//	'  /agents          \x1b[38;2;60;64;67mList available custom agents\x1b[39m'
//
// Cycle 132: takes a highlighted flag — true when this row's index
// matches m.helpCursor. Replaces the cycle-83g hardcoded "/add-dir"
// check.
//
// Cycle 136 hard-coded a 48-char description cap. Cycle 182 replaced
// that with the reference's real rule, which is width-relative: a
// description is truncated only when the row would reach the right
// edge, and the cap is `width - 52` cells (REF leaves the last cell
// unused). Measured against REF v1.4.0:
//
//	width  80  -> 'Ask a side question witho...'   (25 + "...")
//	width 100  -> 'Ask a side question without interrupting the ...' (45 + "...")
//	width 220  -> untruncated (57-char description)
//
// A non-positive width (terminal size not yet reported) disables
// truncation, which is what the pre-182 snapshot fixtures expect.
const helpDescColumn = 51

// buildCommandRow renders one COMMANDS-tab row. width is the terminal
// width; pass 0 when unknown to disable description truncation.
func buildCommandRow(c tuilib.SlashCommand, highlighted bool, width int) string {
	var nameCol strings.Builder
	// Cycle 132: highlight the cursor row with "> " prefix.
	if highlighted {
		nameCol.WriteString("> ")
	} else {
		nameCol.WriteString("  ")
	}
	nameCol.WriteString(c.Name)
	if len(c.Aliases) > 0 {
		nameCol.WriteString(" (")
		for i, a := range c.Aliases {
			if i > 0 {
				nameCol.WriteString(", ")
			}
			nameCol.WriteString(a)
		}
		nameCol.WriteString(")")
	}
	// Pad name column to helpDescColumn visible chars (ref binary's
	// COMMANDS tab uses a 51-char left column with descriptions
	// flowing right; measured at /tmp/agycmp/ref_desc_col.py).
	leftRunes := utf8.RuneCountInString(nameCol.String())
	pad := ""
	if leftRunes < helpDescColumn {
		pad = strings.Repeat(" ", helpDescColumn-leftRunes)
	}
	// Cycle 182: width-relative truncation. The reference keeps
	// `width - 52` description cells; anything longer becomes
	// `<cap-3>...`.
	desc := c.Description
	if width > 0 {
		cap := width - helpDescColumn - 1
		if cap < 0 {
			cap = 0
		}
		if utf8.RuneCountInString(desc) > cap {
			if cap <= 3 {
				desc = desc[:0]
			} else {
				desc = string([]rune(desc)[:cap-3]) + "..."
			}
		}
	}
	// Cycle 182: wrap the name in brand blue only on the cursor row
	// (REF leaves the normal-row name plain); always wrap the
	// description in the darker description gray.
	var b strings.Builder
	if highlighted {
		b.WriteString(helpBlueOpen)
		b.WriteString(nameCol.String())
		b.WriteString(helpBlueClose)
	} else {
		b.WriteString(nameCol.String())
	}
	b.WriteString(pad)
	b.WriteString(cHelpDescOpen)
	b.WriteString(desc)
	b.WriteString(cHelpDescClose)
	return b.String()
}

// formatLogTS returns the current timestamp suffix used in
// Log File path. Format: 20060102_150405. Matches ref binary.
func formatLogTS() string {
	return time.Now().Format("20060102_150405")
}

// helpCommandsPageSize is the historical fixed /help window size. The
// live value is computed per terminal height by helpListWindowFor in
// history.go (18 rows at the 30-row geometry). Kept only so the
// cycle-132 constant has a named home; do not add new uses.
const helpCommandsPageSize = 18

// dispatchSlashLine runs the slash half of submitPrompt: full-screen
// panels first (cycle 185/213), then the per-command handler (cycle 77).
// The buffer must already have been recorded in history by the caller.
//
// Cycle 289: turnDoneMsg's queued-prompt flush calls this too, so a
// slash command submitted while a turn was streaming is dispatched
// once the turn finishes instead of being replayed to the model as
// literal "/plan" text.
func (m *runMode) dispatchSlashLine(buf string) (bool, tea.Cmd) {
	if !strings.HasPrefix(buf, "/") {
		return false, nil
	}
	if cmdName, cmdArgs := splitSlash(buf); m.openSlashPanelArgs(cmdName, cmdArgs) {
		m.input.Reset()
		m.clearInputSelection()
		m.historyIdx = 0
		m.historyEdit = ""
		m.picker.Reset()
		m.atFileDropdown.Reset()
		// Cycle 225: arm the /usage quota spinner when the just-opened
		// panel is the usage one. The first usageQuotaTickMsg arrives
		// ~100 ms later, advances the spinner frame, and after
		// usageQuotaLoadingTicks it flips m.panel.quota.state to
		// quotaLoaded via quotaStubRefills. For every other panel the
		// tick never arms — the dispatch in Update looks at
		// m.panel.kind first. Reset quotaFrame so a re-open of /usage
		// doesn't continue from the previous frame count.
		if m.panel.kind == panelUsage && m.height > 0 {
			m.quotaFrame = 0
			return true, tea.Batch(usageQuotaTick(), fetchQuotaSummaryCmd())
		}
		// Cycle 228: /codesearch kicks off the streaming search in
		// openSlashPanelArgs (right above); here we just arm the
		// first tick so the spinner advances and any pre-arrival
		// matches flow in. The handler keeps arming ticks until the
		// runner's channel closes.
		if m.panel.kind == panelCodeSearch && m.height > 0 {
			return true, csTick(m)
		}
		return true, nil
	}
	// Cycle 77: route any "/…" buffer to the per-command handler.
	// The handler performs the local action (or shows a stub
	// feedback line) and returns (handled, cmd). cmd is non-nil
	// for commands like /exit that need to schedule tea.Quit;
	// it is nil for everything else. Previously only /clear was
	// handled; every other slash command was forwarded to the
	// model which is not what the v1.3.5 binary does.
	if handled, cmd := m.handleSlashCommand(buf); handled {
		return true, cmd
	}
	return false, nil
}

// submitPrompt runs the prompt-submit path: picker substitution, slash
// panels, slash commands, then an agent turn. It is shared by the plain
// Enter binding and by the vim engine's submit results (ctrl+s from
// Normal/Insert mode, ctrl+enter, and a bare Enter under Insert First).
// Returns the tea.Cmd to run, or nil when there was nothing to submit.
func (m *runMode) submitPrompt() tea.Cmd {
	// Cycle 292: any submit consumes the transient input-row notice
	// ("Nothing to compact yet."), the way typing over a placeholder does.
	m.inputNotice = ""
	if m.streaming {
		// Cycle 283: queue instead of dropping. The reference accepts
		// the prompt, clears the input, and only echoes the user row
		// into the transcript when the current turn has finished and
		// the queued turn actually starts (verified with a pyte
		// replay of a truecolour pty capture: the "> SECOND ..." row
		// sits *after* the first turn's DONE, not above the input).
		// Echoing it immediately would interleave it with the live
		// streamed reply, so the echo belongs to the flush.
		//
		// Cycle 289: slash commands are not prompts. agy 1.4.3 runs
		// local/panel commands (/context, /usage, /model, ...) while a
		// turn is still streaming; only commands that start another
		// model turn (and transcript-mutating commands) stay queued.
		// Verified against a live 1.4.3 session driven by a slow mock
		// CloudCode stream: "/context" and "/usage" opened immediately
		// while the spinner kept running.
		prompt := m.input.Value()
		if isBlank(prompt) {
			m.input.Reset()
			m.clearInputSelection()
			m.historyIdx = 0
			m.historyEdit = ""
			m.picker.Reset()
			return nil
		}
		if !strings.HasPrefix(prompt, "/") || m.slashQueuesWhileStreaming(prompt) {
			m.pushHistory(prompt)
			m.streamQueue = append(m.streamQueue, prompt)
			m.input.Reset()
			m.clearInputSelection()
			m.historyIdx = 0
			m.historyEdit = ""
			m.picker.Reset()
			m.atFileDropdown.Reset()
			m.modeHintDismissed = true
			return nil
		}
		// Fall through to the ordinary slash dispatch below: the
		// command runs against the live session without disturbing
		// the in-flight turn.
	}
	// Cycle subagent-question-picker: when the picker staged a
	// question target, the next submit reads the input as that
	// question's answer rather than as a model prompt — even
	// when the input is empty (an empty answer is the
	// "(skipped)" shortcut). submitSubAgentQuestionAnswer
	// consumes the input and resets the target; returning nil
	// here keeps the regular model-turn path from firing on the
	// same keystroke. The intercept must run BEFORE the empty
	// prompt guard so the skip path is reachable.
	if m.submitSubAgentQuestionAnswer() {
		return nil
	}
	prompt := m.input.Value()
	// Cycle trim-empty-history-messages: a whitespace-only prompt
	// (e.g. a single space or a newline) is rejected alongside the
	// existing empty-prompt guard. Previously " " slipped past and
	// appended a blank `> ` row to m.response plus an empty user
	// message to m.session — wasting a screen row, confusing the
	// history list, and shifting scroll offsets by exactly one row
	// per accidental submit.
	if isBlank(prompt) {
		m.input.Reset()
		m.clearInputSelection()
		m.historyIdx = 0
		m.historyEdit = ""
		m.picker.Reset()
		return nil
	}
	// feature/announcement-cards: sending any prompt hides the
	// active announcement card for the rest of this session. The
	// card is NOT persisted (Esc is the "stop showing me this"
	// gesture); the next session will see the same card again.
	// This runs before the slash / picker intercepts because the
	// gesture is "I acknowledge, move on" — the user might have
	// answered the announcement by typing the very thing it asked
	// for (e.g. "/model claude-opus-5-5" after a model-launch card).
	m.hideCurrentAnnouncementForSession()
	// Cycle 151: when the picker overlay is visible, prefer the
	// highlighted command over the literal buffer text. The
	// reference binary's picker substitutes the highlighted row
	// (e.g. typing "/ch" with the cursor on "/changelog" and
	// pressing Enter dispatches `/ch`, not the literal
	// `/ch`). Previously we forwarded the raw buffer text, which
	// got "Unknown command: /ch" instead of running /changelog.
	// Cycle 213: only substitute when the buffer has not been
	// extended with positional arguments — a buffer that carries
	// a non-space argument tail (e.g. "/codesearch needle") must
	// be passed through verbatim so openSlashPanelArgs below
	// receives the query. This is the same rule the picker
	// itself uses (its Update() splits on the first whitespace).
	if m.picker.Visible() {
		if sel := m.picker.Selected(); sel != nil && strings.IndexByte(prompt, ' ') < 0 && strings.IndexByte(prompt, '\t') < 0 {
			prompt = sel.Name
		}
	}
	// Cycle 286: push history before slash command / slash panel /
	// @subagent intercepts so slash commands and subagent dispatches
	// are captured into Up/Down history recall and history.jsonl.
	m.pushHistory(prompt)
	// Cycle 231: `@<subagent> <message>` prompt routing. The hook
	// runs before the slash panel / slash command / chat paths so
	// a `@`-prefixed buffer that names a registered subagent never
	// reaches the model. handlePromptAtPrefix returns true even
	// when the routed dispatch failed (so an error row is appended
	// and the input reset) and returns false only on parse
	// mismatch (so the chat path takes over).
	if handled, cmd := m.handlePromptAtPrefix(prompt); handled {
		return cmd
	}
	// Cycle 185: the reference opens a full-screen panel for a
	// handful of commands (and never prints their text blob into
	// the transcript). Intercept those before the text router.
	// Cycle 289: shared with the queued-slash flush in turnDoneMsg so
	// a command queued mid-turn executes when the turn ends instead of
	// being sent to the model as literal text.
	if handled, cmd := m.dispatchSlashLine(prompt); handled {
		return cmd
	}
	// Unauthenticated guard: if user logged out and sends a model prompt, require login
	if flag.Lookup("test.v") == nil && !hasOAuthToken() && os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("AGY_ADC_AUTH") == "" {
		m.slashRespond("/login", "Authentication required. Please visit the URL to log in:")
		lm := newLoginModel()
		lm.width = m.width
		lm.height = m.height
		m.login = lm
		m.input.Reset()
		m.clearInputSelection()
		return lm.Init()
	}
	m.input.Reset()
	m.clearInputSelection()
	// feature/at-file-dropdown-newline: close the @ file-mention
	// overlay when the prompt is submitted so it doesn't linger
	// into the next turn.
	m.atFileDropdown.Reset()
	// Cycle 262: a new prompt implicitly acknowledges the prior
	// eligibility-error inline notice. The notice is cleared here
	// (after input.Reset, before the turn starts) so a submit
	// mid-error does not leave the previous notice on screen
	// during the new turn. The clear is idempotent so an empty
	// notice is a free no-op.
	m.clearEligibilityNotice()
	m.streaming = true
	m.historyIdx = 0
	m.historyEdit = ""
	// Cycle osc-7501: clear the last-turn outcome so the OSC 7501
	// status does not report "Done" or "Error" for a frame between
	// the user submitting and the streaming flag flipping. The
	// streaming-flag side of programStatus() will own the report
	// from this point until turnDoneMsg lands.
	m.clearLastTurnOutcome()
	// Cycle resume-continuation: auto-generate a conversation title
	// from the first user message so the /resume picker row shows
	// something meaningful rather than the placeholder the very first
	// message produces. The heuristic is deliberately lightweight
	// (4-6 content words, CJK-aware truncation, no LLM call) to keep
	// startup latency and quota usage at zero. Stored on the runMode
	// and applied by saveSession when the conversation is first
	// persisted.
	if m.autoTitle == "" {
		m.autoTitle = generateConversationTitle(prompt)
	}
	// Cycle 283: the reference echoes the submitted prompt in bold
	// Solarized blue (38;2;38;139;210;1), not as plain text. Verified
	// from a truecolour pty capture of 1.4.3: the row is exactly
	// "\x1b[38;2;38;139;210;1m> <prompt>\x1b[0m".
	m.response = append(m.response, userPromptRow(prompt))
	// Cycle 99: the turn's tool-step block begins right after the
	// prompt echo, so ctrl+o can re-render exactly that range.
	m.liveStart = len(m.response)
	// Cycle 229: clear the reply-stream anchor from any prior turn so
	// the first chunk of this turn starts a fresh glamour-replace range.
	m.replyStart = -1
	m.liveReplyIdx = -1
	m.liveSteps = nil
	m.liveStepRuns = nil
	m.activeTool = ""
	m.thinking = false
	m.liveTextLines = 0
	m.liveReplyBuf = ""
	m.streamedText = false
	m.liveBatch = 0
	m.batchBreak = false
	m.anyToolRun = false
	m.spinFrame = 0
	m.picker.Reset()
	// Cycle 227: clear any stale compact state from a prior turn so
	// the status row's "Compacted ..." note does not bleed into the
	// new turn. The compact fields are zero by default — explicit
	// zeroing here documents that the reset is intentional.
	m.compactStart = time.Time{}
	m.compactEnd = time.Time{}
	m.compactSpinnerFrame = 0
	m.compactTokensBefore = 0
	m.compactTokensAfter = 0
	m.compactReason = ""
	m.appendUserTurn(prompt)
	m.saveSession()
	// Cycle 76: start spinner ticking alongside the model stream.
	// sp.Tick() returns a tea.Msg — wrap it in a tea.Cmd and
	// batch with streamPrompt so the first frame appears.
	tickCmd := func() tea.Msg { return m.spinner.Tick() }
	return tea.Batch(m.streamPrompt(prompt), tickCmd)
}

// appendUserTurn records one user turn in m.session. Content keeps the text
// the transcript and the conversation store show; WireContent carries the
// reference's <USER_REQUEST> envelope that actually goes to the model. The
// settings-change block rides on the first turn of a conversation and on the
// first turn after a /model change (cycle 292).
func (m *runMode) appendUserTurn(text string) {
	m.session = append(m.session, backend.Message{
		Role:        "user",
		Content:     text,
		WireContent: backend.WrapUserContent(text, m.model, m.wireSettingsPending, time.Now()),
	})
	m.wireSettingsPending = false
}

// slashQueuesWhileStreaming reports whether a slash command has to wait
// for the current turn to finish instead of running immediately.
//
// agy 1.4.3/1.4.4 dispatches every local command while a turn streams
// (verified live: /context, /usage, /model, /rewind, /resume, /compact,
// /fork and /clear all answered with the spinner still running). Only
// commands that start *another model turn* wait: those would interleave a
// second stream into the live one, so they queue with ordinary prompts and
// run from turnDoneMsg. /share and /quote are not commands at all in this
// release — the picker fuzzy-matches a skill instead — so they fall through
// to the normal dispatch path.
func (m *runMode) slashQueuesWhileStreaming(buf string) bool {
	cmd, _ := splitSlash(buf)
	switch cmd {
	case "continue", "plan", "goal", "learn", "teamwork", "boost", "grill-me":
		return true
	}
	if _, ok := slashSkillLookup(cmd); ok {
		return true
	}
	return false
}

// abortTurnForLocalCommand cancels an in-flight turn on behalf of a local
// slash command and marks the turn discarded, so its late turnDoneMsg does
// not finalise anything into the transcript or the session. Text already
// painted stays where it is; /fork relies on that, /clear wipes it.
func (m *runMode) abortTurnForLocalCommand() {
	if !m.streaming {
		return
	}
	m.discardTurn = true
	if m.turnCancel != nil {
		m.turnCancel()
		m.turnCancel = nil
	}
	m.streaming = false
	m.thinking = false
}

// resetSessionTokenCounters zeroes the per-session usage totals /context and
// the external status line read. /clear and /new start a fresh conversation,
// so the agent-response row must not carry the old one's tokens.
func (m *runMode) resetSessionTokenCounters() {
	atomic.StoreInt64(&m.sessionPromptTokens, 0)
	atomic.StoreInt64(&m.sessionCompletionTokens, 0)
	atomic.StoreInt64(&m.lastTurnCompletionTokens, 0)
}

// toggleQueueMode flips m.queueMode and emits the AGY-style feedback
// row that the v1.4.0 changelog documents. The feedback echoes the
// user's "Queued Messages" setting — Queue / Send Immediately —
// with the (new) active mode highlighted so the user can see their
// keypress was honoured and what behaviour they are now on. The
// feedback row reuses the cycle-71 alt-key dispatcher's "⎿  …" row
// shape (see alt_keytranslate.go) so the visual look matches.
//
// Per the cycle-244 hard-constraints section the toggle is per-session
// state and is NOT written back to settings.json's queuedMessages key.
// Persisting would silently override the user's last /config choice
// across restarts; per-session semantics keep the toggle predictable
// to "you press alt+m, the rest of the session changes".
//
// The dispatcher always reaches this method regardless of whether a
// picker / help / panel / vim / subagent-question picker is open
// because alt+m is a built-in global (not a configured binding and
// not a per-overlay key); only the buffer-not-set gate is checked
// here. In tests, drivers go through Update() so the same path is
// exercised.
func (m *runMode) toggleQueueMode() tea.Cmd {
	prev := m.queueMode
	m.queueMode = !prev
	label := "Send Immediately"
	if m.queueMode {
		label = "Queue"
	}
	// Feedback row: mirror the cycle-71 alt-key dispatcher's row
	// shape ("⎿  …" in dim gray) so the visual matches what the
	// user sees when an alt+<letter> binding fires. A future cycle
	// could refactor this into a shared alt-feedback builder.
	m.response = append(m.response,
		cWarningBlueOpen+"  \u23bf  "+cWarningBlueClose+
			cDimGrayOpen+"alt+m \u2192 Queued Messages: "+label+cDimGrayClose)
	return nil
}

// submitOrQueuePrompt is the Cmd+Enter handler. The dispatch lives
// here (rather than inline in Update) so the test surface mirrors
// submitPrompt's.
//
// Behaviour matrix (queueMode × streaming):
//
//	queueMode==false → calls submitPrompt (acts as a synonym for
//	  plain Enter on a single-line buffer; preserves the existing
//	  UX for users who migrate from chat clients that bind
//	  Cmd+Enter as send — Cmd+Enter has always been a newline in
//	  the v1.3.5 ref binary, so the clone keeps that legacy).
//	queueMode==true  → saves the textarea value into m.queuedPrompt
//	  and clears the input; a later turnDoneMsg flushes the queued
//	  prompt as the natural next user message. Streaming is
//	  ignored: queue-mode is explicitly designed to let the user
//	  line up the next turn while a turn is already in flight.
//
// The "trim blank" guard mirrors submitPrompt so a Cmd+Enter on a
// blank buffer doesn't capture "" into m.queuedPrompt (a queued
// empty prompt would conflate with no-queue; the guard keeps the
// state predictable).
func (m *runMode) submitOrQueuePrompt() tea.Cmd {
	prompt := m.input.Value()
	if isBlank(prompt) {
		m.input.Reset()
		m.clearInputSelection()
		m.historyIdx = 0
		m.historyEdit = ""
		m.picker.Reset()
		return nil
	}
	if !m.queueMode {
		// In send mode Cmd+Enter is the existing newline path;
		// submit the prompt normally so the binding stays
		// recognisable as "send" for chat-client migrants.
		return m.submitPrompt()
	}
	m.pushHistory(prompt)
	// Queue mode: capture the prompt, clear the textarea, leave the
	// status row's "queue mode" badge in place. The user can keep
	// typing the next message into the cleared input; the queued
	// one will dispatch after the current turn finishes.
	m.queuedPrompt = prompt
	m.input.Reset()
	m.clearInputSelection()
	m.historyIdx = 0
	m.historyEdit = ""
	m.picker.Reset()
	m.atFileDropdown.Reset()
	// Echo the queued prompt into the transcript so the user can
	// see what was queued (and edit later by toggling alt+m back).
	m.response = append(m.response,
		cDimGrayOpen+"  \u23bf  "+"\u00bb queued: "+prompt+cDimGrayClose)
	return nil
}

// saveSession persists the in-memory session to the agy-compatible
// conversation store so the user can -c / --continue /resume from
// either binary. It is a no-op for empty sessions or missing conv IDs.
//
// Cycle resume-continuation: when an auto-generated title has been
// computed from the first user message, apply it to both the index
// row and the metadata cache. The title override runs after Save so
// the row exists (Save creates it on first run); both writers are
// no-ops on a missing row, so subsequent saves that don't change the
// title stay cheap.
func (m *runMode) saveSession() {
	if m.conversationID == "" || len(m.session) == 0 {
		return
	}
	steps := sessionToSteps(m.session)
	if err := convstore.SaveConversation(m.conversationID, m.cwd, steps); err != nil {
		return
	}
	if m.autoTitle != "" {
		_ = convstore.WriteSummariesTitle(m.conversationID, m.autoTitle)
		_ = convstore.WriteCacheTitle(m.conversationID, m.autoTitle)
	}
}

// runMode is the v1.3.5 binary 1:1 single-prompt TUI.
type runMode struct {
	email string
	model string
	// Cycle 80: status row model label is the base name only (no
	// parenthetical tier suffix). e.g. when m.model is
	// "Gemini 3.8 Flash (High)", m.statusModel is "Gemini 3.8 Flash"
	// so the status row matches the v1.3.5 binary byte-for-byte.
	statusModel string
	tier        string
	cwd         string
	backend     backend.Client
	// backendFor rebuilds the transport after a model change, so the
	// selected model family decides the endpoint: Gemini/Claude on
	// CloudCode, MiniMax on agw. nil in tests that construct runMode
	// directly; the backend then stays whatever NewRunMode received.
	backendFor func(model string) backend.Client

	// Cycle 86: per-session conversation UUID shown in the /help
	// general panel's "Conversation: <uuid>" row. Generated once at
	// NewRunMode time so the user can identify their session
	// in logs / support tickets. Matches ref binary's behavior.
	conversationID string

	// Cycle tasks-panel: the registry that backs /tasks and the
	// status-line "· N tasks" segment. Set by NewRunMode at
	// construction time so panelLines() can read it without
	// threading through every method. Nil = no registry wired,
	// which the panel renders as the empty state.
	taskReg *taskregistry.Registry
	// tasksSubscribed is true after startTasksSubscription()
	// has armed the subscriber goroutine. Re-armed by every
	// reopen of the panel so a long-idle session does not miss
	// events that fired while no subscriber was parked.
	tasksSubscribed bool
	// tasksSubCancel is the unsubscribe function returned by
	// the registry's Subscribe(); called from a future close()
	// path or when the runMode is re-initialised.
	tasksSubCancel func()
	// tasksDirty is set by the subscriber goroutine on every
	// event. The TUI's tick handler reads it; if true, it
	// returns a tick that forces a View() re-render.
	// int32 because the goroutine writes it without holding
	// any lock — atomic.StoreInt32 / atomic.LoadInt32 keep the
	// race detector happy.
	tasksDirty int32
	// taskSubagentManager is the subagent manager the panel
	// uses to translate a "kill this row" action into a
	// Subagent.Kill. nil = use the package-level
	// defaultTaskSubagentManager (see
	// SetDefaultTaskSubagentManager).
	taskSubagentManager *subagent.Manager
	// Cycle resume-continuation: auto-generated conversation title
	// produced from the first user message. Cached on the runMode
	// once we have a first prompt; saveSession passes it through so
	// the /resume picker shows the meaningful title rather than a
	// placeholder. Empty before the first message; once set, do not
	// regenerate — the v1.4.0 changelog notes this is fixed at
	// "conversation created" time.
	autoTitle string

	// Cycle 81: when the configured model (settings.json's
	// "model" key) is unavailable and the binary falls back to
	// another model, the v1.3.5 binary emits a 4-row warning
	// block between the banner and the top separator:
	//   ⚠ Warning
	//     ⎿  "<from>" is no longer available. Using "<to>".
	//
	// fallbackFrom: the user-configured model that's unavailable
	//               (e.g. "MiniMax-M3"). Empty string = no warning.
	// fallbackTo:   the model actually used (e.g. "Gemini 3.8 Flash (High)").
	//               Both fields must be non-empty for View() to emit
	//               the block.
	fallbackFrom string
	fallbackTo   string

	altScreen bool

	// Cycle 174: in-app text selection state. MouseModeCellMotion
	// is always emitted in alt-screen mode (no longer togglable);
	// when the user mouse-downs on the transcript, selDragging goes
	// true and selStart/selEnd track the live range; on release we
	// emit OSC52 with the joined selection text and clear the
	// highlight. Coordinates are visible-screen cell coords: row
	// index into the transcript produced by transcriptWindow(),
	// col index into the (already-padded) row.
	selDragging bool
	selStartRow int
	selStartCol int
	selEndRow   int
	selEndCol   int

	// In-app input field text selection state.
	inputSelDragging bool
	inputSelStart    int // byte offset in m.input.Value()
	inputSelEnd      int // byte offset in m.input.Value()

	session []backend.Message

	// Cycle 229: the manual /compact slash command hooks into PiG's
	// coding.Session.Compact() through this seam. When nil the
	// cycle-167 stub (compactSession helper in slash_helpers.go)
	// takes over — print mode, offline tests, and any caller that
	// has not wired a pigrt.Runtime. Cycle 227 will populate
	// m.compactor when the auto-compact runtime integration merges;
	// cycle 229 only defines the seam so the manual /compact path
	// can be exercised in tests today and lights up in production
	// the moment cycle 227 lands.
	//
	// The interface is exported via slash_compact.go's
	// manualCompactor; the production wiring is a thin
	// pigrtCompactor over *pigrt.Runtime. m.compactor stays nil
	// until a caller (cycle 227's TUI plumbing, or a future /open
	// path) sets it — so the cycle-167 behaviour is preserved for
	// every existing test that drives handleSlashCommand directly.
	compactor manualCompactor

	// Cycle 71: input component. charm.land/bubbles/v2 textarea
	// because the v1.3.5 binary's input is multi-line (Ctrl+J / Shift+Enter
	// newline), grows up to ~8 rows, and supports ctrl+a / ctrl+e / ctrl+w /
	// alt+left / alt+right / alt+backspace / alt+enter / ctrl+u / ctrl+k
	// natively — bubbletea v2's textarea already implements all of them via
	// the default KeyMap.
	input tas.Model

	// Cycle 216: the Vim editing engine, created on first use when the
	// "Editor Mode" setting selects vim. It owns the buffer while vim
	// mode is on; the textarea is kept in sync as a display surface. See
	// vim_mode.go.
	vimEng *vim.State
	// vimOn / vimFirstOn mirror the "Editor Mode" and "Editor Mode ›
	// Insert First" settings. They are read at start-up and refreshed
	// when a slash panel closes (reloadVimSettings), never per keystroke.
	vimOn      bool
	vimFirstOn bool

	// Cycle alt-keybinding-option-meta: the user's
	// ~/.gemini/config/keybindings.json dispatcher. Loaded by
	// NewRunMode at start-up. nil = no dispatcher (e.g. tests that
	// drive Update without a config home). See cmd/wagyu/alt_keytranslate.go.
	keybindings *keybind.Keybindings
	// altEscBuf holds one buffered bare Esc key while the TUI waits
	// for a possible alt+letter follow-up. Only used on Terminal.app
	// and VS Code-based terminals — see keybind.TermNeedsOptionMeta.
	altEscBuf keybind.EscBuffer
	// altKeyFires records every alt+letter dispatch in chronological
	// order. Tests assert on this slice; production code reads it
	// through altDispatchAction which also writes a feedback row.
	altKeyFires []altKeyFire

	response  []string
	streaming bool
	// Cycle 235: cancel function for the in-flight turn's stream
	// context. streamPrompt installs this when the model stream is
	// armed so the Esc handler can cancel the stream out of band
	// (AGY 1.2.10: "Esc while the suggestions dropdown is open…
	// a second Esc interrupts"). Set to nil when the turn ends
	// (turnDoneMsg / backendDoneMsg / backendChunkMsg handlers)
	// so the Esc handler can tell "a turn is in flight, ready to
	// cancel" from "streaming flag is stale".
	turnCancel context.CancelFunc
	quitting   bool
	// Cycle 144: two-step Ctrl+C exit confirmation. exitConfirm=true
	// means the user pressed Ctrl+C once and the status row is now
	// showing "press ctrl+c again to exit"; exitConfirmAt records
	// when that press happened so the confirmation auto-expires
	// after a few seconds (matching the v1.3.6 reference behaviour
	// captured at /tmp/refscroll.txt).
	exitConfirm   bool
	exitConfirmAt time.Time

	// Cycle 76: streaming spinner. The v1.3.5 binary renders a Braille
	// animation ("⣾ ⣽ ⣻ ⢿ ⡿ ⣟ ⣯ ⣷") prefixed to "  Generating..." in
	// the response region while a model stream is in flight. We use
	// the bubbles/v2 spinner.Dot (10 FPS) which is byte-for-byte the
	// same frame set.
	spinner sp.Model

	history     []string
	historyIdx  int
	historyEdit string

	picker tuilib.PickerState

	// Cycle 129: picker scroll offset. The picker renders a fixed
	// 5-row window (matching the ref binary's visible-row count
	// recovered in /tmp/agy_slash.txt), and the highlighted row's
	// index can grow past that window as the user holds Down.
	// Before cycle 129 the highlight was lost because the renderer
	// loop only iterated i < visible — when the highlight's idx
	// was >= 5, *no* row was rendered as highlighted. The offset
	// is the index of the first row drawn: the renderer walks
	// [offset, offset+visible), with the highlight on
	// picker.Idx() == high_idx falling in that range.
	pickerOffset int

	// feature/at-file-dropdown-newline: the @ file-mention
	// dropdown state. Hidden until the buffer carries an active
	// @<chars> token (atfiles.Parse) — the AGY 1.3.6 line-12
	// fix lets the dropdown appear after a newline in a multi-
	// line prompt, not just at end-of-line. Reset on Esc / submit
	// and re-derived by Update from the input value.
	atFileDropdown atFileDropdown
	// Cycle 232: picker mouse-click support. pickerLayoutLocal is
	// populated as a side effect of pickerView() and carries the
	// internal layout (row count, list range, scroll-tail flags).
	// pickerBodyStart is the line index in the pre-trim body where
	// the picker's first row sits. View() converts those into
	// pickerGeom (the on-screen rectangle in terminal coords) by
	// subtracting the top-trimmed-row count. pickerGeom has the
	// screen-Y range the mouse handlers dispatch against.
	pickerLayoutLocal pickerLayout
	pickerBodyStart   int
	pickerGeom        pickerGeom
	// promptBodyStart is the line index in the pre-trim body
	// where the prompt (input field) row sits. View() subtracts the
	// top-trim count to produce promptScreenY, the screen Y of the
	// prompt row the cycle-232 mouse handler uses to detect "click
	// on the input field reopens the picker".
	promptBodyStart int
	promptScreenY   int

	// Cycle 99: interactive tool calling. tools is the recovered advertised
	// tool surface and toolRunner executes the model's calls; both are
	// installed by runLiveTUI through EnableTools. Before cycle 99 the
	// interactive session advertised nothing and the model answered "I don't
	// have a file reading tool available" (live 1.3.6 capture).
	tools      []backend.ToolDecl
	toolRunner agent.ToolRunner
	// agentBody is the --agent custom agent's Markdown body. Non-empty swaps
	// the default system prompt for the reference's custom-agent prompt and
	// restricts the advertised tools to the captured seven.
	agentBody string
	// agentName is the active markdown agent's name, or "" for the built-in
	// default agent. The /agents panel can switch it at runtime; the status
	// row then reads "auto · <name> (<model> · <effort>)  /agents".
	agentName string
	// toolSandbox is the session sandbox installed on the tool gate. A
	// runtime /agents switch rebuilds the catalogue and must keep the same
	// sandbox/policy pair, so the sandbox is retained here.
	toolSandbox sandbox.Sandbox
	// agentWarning is the ⚠ Agent Not Found body when --agent named nothing
	// and the session fell back to default (agy 1.4.3 pty capture).
	agentWarning string

	// prog is the running *tea.Program. The streaming goroutine uses it to
	// push tool-step updates into the UI; it is nil in tests, which drive
	// Update directly with the same messages.
	prog *tea.Program

	// liveSteps accumulates the steps of the turn currently in flight;
	// liveStart is the index in m.response where that turn's step block
	// begins; activeTool is the spinner hint while a tool executes
	// (recovered: the spinner line "names the tool call the agent is
	// currently running").
	liveSteps  []toolStep
	liveStart  int
	activeTool string
	// liveStepRuns mirrors liveSteps with transcript coordinates, so
	// ctrl+o can expand/collapse steps that are still streaming (the
	// turnDoneMsg path only re-renders m.blocks, which covers finished
	// turns). One entry per toolStepMsg, in paint order.
	liveStepRuns []liveStepRun
	// Cycle 229: replyStart marks the index in m.response where the model
	// reply's raw streamed text begins. Set by the first backendChunkMsg
	// of a turn; cleared at turn-end (turnDoneMsg / backendDoneMsg). The
	// turnDoneMsg handler uses [replyStart, len(m.response)) as the range
	// to replace with the glamour-rendered msg.reply, so the user sees
	// the raw text scroll in live and then have it re-styled in place at
	// turn-end — without double-rendering. -1 when no streaming is in
	// flight (e.g. headless tests that drive turnDoneMsg directly).
	replyStart int
	// Cycle 285: liveTextLines is the row count of the current streaming
	// text run (anchored at replyStart), and liveReplyBuf is that run's
	// raw text. Reset when a tool step lands so the next assistant
	// message is rendered as its own run — that is what keeps prose that
	// precedes a call above the block and prose that follows it below.
	liveTextLines int
	liveReplyBuf  string
	// streamedText records whether this turn rendered any text live. When
	// it did, turnDoneMsg leaves the streamed rows alone (they are
	// already glamour-rendered) instead of wiping the range.
	streamedText bool
	// liveBatch is the batch id of the calls emitted by the current
	// assistant message; batchBreak is set whenever text streams, so the
	// next call starts a new block. The reference renders each batch as
	// one header ("● Ran 2 commands (a · b)") and keeps the prose between
	// batches in place.
	liveBatch  int
	batchBreak bool
	// spinFrame drives the animated spinner label (spinnerText). It
	// advances once per spinner tick so the shimmer tracks the glyph.
	spinFrame int
	// anyToolRun flips once a call has run in this turn; the reference's
	// post-tool working phrase is "Working", not "Generating".
	anyToolRun bool
	// Cycle 229: liveReplyIdx is the index in m.session where the
	// in-progress assistant message sits. The first chunk of a turn
	// appends a fresh assistant entry and stores the index here;
	// subsequent chunks update that entry in place so a 500-delta
	// stream still produces ONE assistant session message per turn
	// (cycle-99's per-chunk append produced N entries and broke
	// downstream conversation replay). -1 when no streaming is in
	// flight. turnDoneMsg uses this to decide whether to overwrite the
	// existing entry with msg.reply or append a new one.
	liveReplyIdx int
	// Cycle 272 removed glamourThroughIndex (cycle 232) and fnvCache
	// (cycle 243): the mid-stream pass no longer renders per-completed-
	// line or memoises a boundary-rendered prefix. It renders the raw
	// accumulated reply with renderMarkdown on every chunk — the same
	// function turnDoneMsg calls — and swaps the whole live range in,
	// so there is no partial-render bookkeeping left to carry.
	// mdrender.LineANSIBounded / LineANSI keep their own unit tests as
	// library helpers; they are simply no longer on the TUI hot path.

	// thinking is true while the model is inside a think block and the
	// conversation view is showing the live "⏺ Thinking... (Ns)" frame;
	// thinkingSince is when that frame started. Recovered from the
	// changelog: "in-progress thinking frames ... (for example showing
	// `Thinking... (31s)` forever)". On turn completion the live frame is
	// replaced by the "Thought for Ns, N tokens" step.
	thinking      bool
	thinkingSince time.Time

	// expanded is the ctrl+o toggle for tool-step groups (recovered hints
	// "ctrl+o to expand" / "ctrl+o to collapse", keybinding name
	// KeyToggleExpandGroup).
	expanded bool

	// blocks records where each turn's step block lives in m.response so
	// ctrl+o can re-render it in place without disturbing interleaved
	// slash-command feedback.
	blocks []turnBlock

	// Cycle 83: 3-tab /help TUI menu overlay. When helpMode is true,
	// the View() renders a tabbed help panel between the top separator
	// and the bottom separator / status row instead of the normal
	// response transcript.
	//
	// Tab 0 = "general"   (default — Antigravity CLI intro/version/etc)
	// Tab 1 = "commands"  (full 99-entry slash command catalog)
	// Tab 2 = "shortcuts" (keyboard bindings)
	//
	// Cycle 83 keys:
	//   ←/→ or Tab: switch tab (cycle 0→1→2→0)
	//   esc:       close help overlay (returns to normal TUI)
	helpMode bool
	helpTab  int // 0/1/2

	// Voice dictation state (matching AGY AudioModel and KeyToggleDictation).
	voiceMode      bool
	voicePhase     int
	voiceTrigger   string
	voiceRecorder  *voice.Recorder
	voiceCancel    bool
	voiceConnectFn func() error

	// Cycle 185: modal slash-command panel. The reference renders
	// /mcp, /tasks, /effort, /hooks, /permissions, /agents, /credits,
	// /codesearch and /artifact as a full-screen picker in the slot
	// below the prompt box instead of printing a line into the
	// transcript; panel is that picker's state (kind == panelNone when
	// closed). See cmd/wagyu/slash_panels.go.
	panel slashPanel

	// Cycle 182: /help list navigation state. The reference renders an
	// 18-row window into the full catalog; helpCursor is the 0-based
	// index of the highlighted row in the *whole* catalog, helpWindow
	// is the 0-based index of the first visible row. Up/Down move the
	// cursor and scroll the window one row at a time; Right/Left/Tab
	// cycle tabs and reset both to 0. See helpListDown in history.go
	// for the verified REF model. Cycle 137's helpPage field (fixed
	// 18-row pages + Right/Left page turns) was replaced by this.
	helpWindow int
	helpCursor int

	// Cycle 167: /resume picker state. When resumeMode is true,
	// the input field is hidden and the transcript is replaced
	// with a list of conversations from StoreDir()/conversations/.
	// Up/Down navigates resumeCursor, Enter loads the highlighted
	// conversation, Esc cancels. resumeList is populated when
	// /resume runs; empty entries mean "list failed, show error".
	resumeMode   bool
	resumeList   []convstore.ConversationMeta
	resumeCursor int
	// Cycle 184: the v1.4.0 picker's tab / search / window state.
	// resumeTab is 0 (CLI) or 1 (Other); resumeQuery is the live
	// search text; resumeWindow is the index of the first visible row.
	resumeTab    int
	resumeQuery  string
	resumeWindow int
	// resumeCursorOn is the blink phase of the search cursor. REF blinks
	// it; resumeBlinkMsg toggles this.
	resumeCursorOn bool

	// flags is the parsed top-level flag set. Cycle 102: the TUI reuses the
	// same recovered content the print-mode slash handlers emit (`/model`,
	// `/effort`), which read their values from the flag set.
	flags *flag.FlagSet

	// mode is the agent execution mode for this session ("", "accept-edits",
	// "auto" or "plan"). Cycle 104: Shift+Tab cycles it, and policy points at
	// the same value the tool gate consults, so the change takes effect on the
	// next tool call.
	mode   string
	policy *permissions.Policy

	// Cycle 105: /statusline and /title toggles. statusLine hides the bottom
	// status row; title turns the custom terminal window title on, which sets
	// View().WindowTitle. Both are seeded from settings.json by runLiveTUI.
	statusLine string
	title      string

	// Cycle status-line-script: AGY 1.1.27 wires statusLine.command into
	// settings.json; a configured + enabled command takes the row over from
	// the built-in renderer. statusLineExternal tracks whether the external
	// branch is currently active; statusLineCommand is the resolved command
	// string (the runLiveTUI seed reads it once at startup). The two
	// together replace the old two-state "" / "off" cycle with a three-state
	// "" / external / "off" cycle driven by /statusline (see
	// cmd/wagyu/slash_commands.go).
	statusLineExternal bool
	statusLineCommand  string

	// Cycle status-line-script: per-session token totals accumulated from
	// every chunk-meta hook the chunk handler installs. Atomic so the
	// recordTurnUsage call (chunk drain goroutine) and the
	// sessionTokenTotals read (status-line tick goroutine) stay race-free.
	// The sessionPromptTokens figure is the *unrounded* prompt-token
	// count the upstream gateway emits, which JSON `cost` is computed off.
	sessionPromptTokens     int64
	sessionCompletionTokens int64
	// Cycle 292: the completion count the *last* streamed step reported.
	// /context's "Agent responses" row is sessionCompletionTokens minus
	// this value, because agy counts finished response steps and the step
	// currently landing has not been counted yet (measured: a text-only
	// first turn shows 0, after a tool turn 12, after a second text turn
	// 12 again).
	lastTurnCompletionTokens int64

	// Cycle 292: true on the first user turn of a conversation and again
	// after a /model change, so the wire envelope carries the reference's
	// <USER_SETTINGS_CHANGE> block exactly once per selection.
	wireSettingsPending bool

	// Cycle 292: set when a mid-stream local command (/clear, /new, /fork,
	// /branch) cancelled the in-flight turn. turnDoneMsg then skips
	// finalising that turn into the transcript and the session.
	discardTurn bool

	// Cycle 292: transient text in the empty input's placeholder slot. The
	// reference answers /compact mid-stream with "Nothing to compact yet."
	// *in the input row* (measured on 1.4.4: gray body after a blue ">"),
	// not as a transcript row. Cleared by the next submit or turn end.
	inputNotice string

	// statusLineExternalState is the cached rendered output / tick counter
	// the external status-line path owns. Lazily allocated by
	// ensureExternalState the first time the external path fires; left
	// nil otherwise so a runMode that has never armed the external mode
	// pays zero memory cost.
	statusLineExternalState *statusLineExternalState

	// Cycle 106: /rename name (used by conversationTitle) and the
	// /remote-control session flag.
	conversationName string
	remoteControl    bool

	// Cycle 250: /narration session state. narrationOff is true after
	// the user typed "/narration off" once in this session — every
	// subsequent /narration invocation no-ops until the TUI restarts.
	// narrationStart is the timestamp /narration feeds into the
	// catalogue FNV hash so a new session yields a new paragraph; it
	// is captured in NewRunMode and stays constant for the rest of the
	// session, matching the spec's "deterministic from session start
	// time + task type" wording.
	narrationOff   bool
	narrationStart time.Time

	// Cycle 167: /goal sets the session goal text; /learn appends to
	// the in-memory learnings slice. Both are session-only — not
	// persisted across restarts.
	goal      string
	learnings []string

	// Cycle 230: /btw side-question cards. btwManager is constructed
	// lazily on the first /btw invocation; it owns the cards the
	// TUI renders as collapsed rows in the transcript and the cards
	// /copy btw copies. btwCards is the same slice the Manager
	// tracks; the TUI keeps its own copy only so it can iterate
	// without taking the Manager's lock on every render. activeBtwID
	// is the ID /copy btw addresses — the most recent card the TUI
	// rendered — so a follow-up /copy keeps working even after the
	// session grows more cards. btwExpanded tracks the per-card
	// expansion state (false = collapsed, true = expanded); the
	// flag's default matches the reference's behaviour where every
	// newly-settled card sits collapsed and Enter/Esc toggle it.
	btwManager      *sidequestion.Manager
	btwCards        []*sidequestion.Card
	activeBtwID     string
	activeBtwIndex  int
	activeBtwCancel context.CancelFunc
	btwScroll       int
	btwExpanded     map[string]bool

	// Cycle btw-runs-hooks: lifecycle-hook bus for the TUI session.
	// Constructed in NewRunMode so /btw can dispatch
	// PreSideQuestion / PostSideQuestion. Production code does not
	// register handlers today; subagent / extension hosts are
	// expected to Register at startup. Tests build a Manager
	// directly and pass it in by overwriting this field after
	// NewRunMode returns.
	hooks *hooks.Manager

	// Cycle 231: atRouter is the named-subagent registry the
	// `@<subagent> <message>` prompt syntax dispatches against.
	// NewRunMode seeds this with at_subagent.go's DefaultRouter so
	// the TUI session and the print path share the same entries;
	// tests can overwrite it with a private Manager so they don't
	// pollute the global registry.
	atRouter *subagentrouter.Manager

	// Cycle 119: terminal geometry and the alt-screen transcript viewport.
	// See tui_viewport.go for the recovered evidence. width/height stay 0
	// until bubbletea delivers the first tea.WindowSizeMsg, and while they
	// are 0 the view falls back to the pre-119 unclipped layout (which the
	// byte-exact snapshots and the unit tests rely on).
	width, height int

	// scroll is the index of the first transcript row shown (the viewport's
	// YOffset). autoScroll pins it to the bottom while new rows arrive;
	// scrolling up clears it and ctrl+end / reaching the bottom re-arms it
	// (store.Manager.AutoScroll / .SetAutoScroll).
	scroll     int
	autoScroll bool

	// Cycle 179: row indices in the scrollable transcript whose first
	// visible cell is the start of a user prompt. Repopulated on every
	// render in transcriptWindow(). The sticky bar logic (stickyBarRow
	// below) uses these to pick the row to render above the scrollable
	// content — scrolling itself stays smooth, see scrollBy comment.
	userPromptRows []int

	// Cycle 179: the sticky user-prompt bar text. Cached on every render
	// in transcriptWindow() so viewBody can prepend it without re-walking
	// the response rows. Empty when the viewport is pinned to the bottom
	// (input chrome already owns the prompt slot) or when no user prompt
	// is visible above the current scroll position.
	stickyBarRow string

	// Cycle 253: AGY 1.3.3 pinned-prompt header. In altscreen mode the
	// viewport pins the prompt that produced the assistant response the
	// user is reading while scrolled into a turn — a dim/borderless
	// header above the scrollable content with the prompt text and a
	// "↳ jump" hint. Clicking or pressing Enter on it scrolls the viewport
	// back to that prompt's row. The feature is purely visual and does
	// NOT mutate m.session or the model state. Print mode (`-p`) is
	// unaffected because it never instantiates runMode.
	//
	// pinnedPromptRowIdx is the absolute index in the rendered `rows`
	// (header + resp + live) where the producing user-prompt row lives,
	// or -1 when no pinned prompt is shown this frame. pinnedPromptText
	// is the cached display string (the prompt row with the dim border
	// and jump hint appended). Both are populated by transcriptWindow on
	// every render and reset to (-1, "") when altscreen is off or the
	// conditions of stickyUserPromptRow aren't met.
	pinnedPromptRowIdx int
	pinnedPromptText   string

	// vpRows is the transcript viewport's height in rows, recorded on every
	// render so the page-scroll bindings know how far a page is.
	vpRows int

	// Cycle 122: per-width caches of the wrapped m.response rows. The
	// transcript can hold thousands of rows (a resumed conversation) and
	// wrapping the whole thing per render — especially with a 70 KB model
	// reply — was the dominant cost of every scroll keypress. Two slots:
	// the full-width wrap (rowsCache) and a narrower wrap (rowsCacheN1),
	// which pre-cycle-134 layouts consulted for the scrollbar column.
	// Both are filled on first use and reused while valid.
	//
	// Cycle 281: the slots became incremental (`wrapSlot.rebuild` only
	// re-wraps the entries whose source string changed), so invalidating on
	// an input keystroke no longer re-wraps the whole transcript. rowsGen
	// is bumped by every rebuild and keys the composed-row cache below.
	rowsCache      wrapSlot
	rowsCacheN1    wrapSlot
	rowsCacheDirty bool
	rowsGen        int

	// Cycle 281: the per-frame layout cache. Composing the scrollable rows
	// (header + response + live), their screen-row counts, the prefix sum of
	// those counts and the user-prompt indices used to be O(total rows) on
	// every render — the clone's analogue of the reference's per-frame
	// "prefix-sum cost" that the 1.1.2 changelog says was cut. A valid
	// composition makes a scroll frame O(viewport). Invalidate by bumping
	// rowsGen (a response mutation) or by changing the header/live rows.
	rowsView composedRows

	// Cycle 225: /usage quota spinner frame. usageSpinnerFrame picks
	// the nth frame from the same sp.Dot set the streaming spinner
	// uses; the usageQuotaTickMsg handler advances it once every
	// 100 ms while the /usage panel is open. The counter wraps mod 8
	// (the sp.Dot frame count) and is reset to 0 every time
	// openSlashPanel arms a new quota fetch.
	quotaFrame int

	// Cycle 227: auto-compact spinner frame. compactSpinnerFrame
	// advances on the same 100 ms cadence as the streaming spinner
	// (both use the sp.Dot frame set), and compactStart / compactEnd
	// stamp the wall-clock times the agent loop reported. The status
	// row reads compactActive between renders so a long task — the
	// TODO §4 row — no longer hangs the chat thread silently while
	// PiG summarises.
	compactSpinnerFrame int
	compactStart        time.Time
	compactEnd          time.Time
	// compactStartTokens / compactEndTokens are the upstream-reported
	// token counts. A start of 0 with a non-zero end means the upstream
	// emitted a CompactionEndEvent but never paired it with a
	// CompactionStartEvent, which the cycle-227 runner silently
	// drops — see internal/agent/loop.go: recordCompact. The end
	// values still drive the "(184 K → 42 K)" rendering in the row.
	compactTokensBefore int
	compactTokensAfter  int
	// compactReason is the upstream "manual" | "threshold" | "overflow"
	// string. Hover / future expansion may render this; for cycle 227
	// only "threshold" and "overflow" are surfaced (manual is the
	// `/compact` slash command, which has its own echo).
	compactReason string

	// Cycle 228 (codesearch-panel): csCancel is the cancel function
	// for the in-flight /codesearch search. The runner goroutine
	// writes it during Run() and clears it on return; the panel's Esc
	// handler calls it to kill the rg child or stop the walk. nil
	// when no search is running.
	csCancel context.CancelFunc

	// streamingSearch gates the cycle-228 streaming path on or off.
	// Defaults to true (production TUI wants live streaming); tests
	// that pin the synchronous cycle-213 layout flip it to false so
	// the synchronous csScan fills m.panel.cs.hits before codesearchStart
	// returns.
	streamingSearch bool

	// feature/announcement-cards: in-TUI announcement card queue.
	// announceVisible is the head-rendered slice — front is what
	// announcementRow() emits, back is what the user has not seen
	// yet. announceSessionHiddenID is the ID the user acknowledged
	// by sending a message (so /clear + send again does not bring
	// the same card back). announceSaveError carries the last
	// SaveDismissed failure so the next render can surface a
	// warning — the in-memory dismiss still takes effect even when
	// persistence fails.
	announceVisible         []announce.Card
	announceSessionHiddenID string
	announceSaveError       error

	// Cycle 262: eligibility-error inline notice. The value is set
	// by the chunk handler in streamPrompt when a turn terminates on
	// an *EligibilityError, and cleared by the tea.KeyMsg handler
	// when the user starts typing. View() reads the value through
	// eligibilityNoticeRow() and renders the row above the prompt
	// input (the same slot announcementRow occupies); the row is
	// NEVER appended to m.response or m.session — cycle 262's hard
	// constraint is "inline notice must NOT be appended to the
	// data structures that hold user-visible history". The struct
	// is a value (not a pointer) so the zero state is "no notice",
	// matching the announcement-card style.
	eligibilityNotice eligibilityNotice

	// Cycle subagent-question-picker: per-subagent pending question
	// store. The parent's ask_question tool routes through it;
	// subagents call it with their own SubAgentID so the picker
	// knows which agent's question to answer. Initialised in
	// NewRunMode and reused for the lifetime of the TUI. The
	// manager is headless=false (interactive) so the picker has
	// to settle the question.
	subAgentQuestions *subagentquestions.Manager

	// Cycle 244: AGY 1.4.0 "Queued Messages" semantics. The user's
	// choice in /config ↔ "Queued Messages" ↔ settings key
	// `queuedMessages` (values "queue" / "send-immediately") maps
	// to a session-level flag on the runMode. alt+m toggles it for
	// the rest of the session (the cycle ships per-session state;
	// the toggle does NOT write back to settings.json — see
	// deliverable cycle-244-alt-m §硬约束 for the rationale).
	//
	// queueMode == true  → Enter inserts a newline, Cmd+Enter sends.
	// queueMode == false (the AGY 1.4.0 default — "send right
	// away" maps to Enter-sends) → Enter submits through the
	// ordinary submitPrompt path. The flag drives the statusRow
	// badge and the Enter-branch in Update; the persisted
	// settings key is consulted only at the moment of toggle so
	// re-toggling restores the user's configured default rather
	// than the prior ad-hoc state.
	queueMode bool
	// Cycle 244: the prompt the user submitted while in queue
	// mode. Captured into a temporary slot rather than dispatched
	// immediately; a turnDoneMsg handler flushes it as the next
	// user message so the model sees the queued prompt as the
	// natural follow-up after the current turn's assistant reply.
	// Cleared on flush and on a second Cmd+Enter submission
	// (later queued submissions replace earlier ones — matching
	// the ref binary's "last-write-wins" semantics captured at
	// /tmp/refqueue).
	queuedPrompt string
	// streamQueue holds prompts submitted with Enter while a turn is
	// in flight. Cycle 283: the reference does not drop them. It echoes
	// the user row into the transcript at once and dispatches the
	// prompt as the next turn when the current one ends (verified live
	// against agy 1.4.3: a `sleep 8` turn plus a second prompt typed
	// mid-stream — the second row appears immediately and is answered
	// after the first turn's DONE). Unlike queuedPrompt (cycle 244's
	// Cmd+Enter slot) this is a FIFO: each Enter appends.
	streamQueue []string
	// modeHintDismissed records that the user has edited the input since
	// the mode cue was last shown. The reference's placeholder is not
	// permanent: it appears at startup (and again after a Shift+Tab) and
	// disappears on the first keystroke that reaches the textarea — it
	// does not come back when the buffer is emptied again, nor after the
	// prompt is submitted. Verified at 200x40 against agy 1.4.3: type
	// "a", backspace → bare ">"; Shift+Tab → the new banner; type, delete
	// → bare ">" again.
	modeHintDismissed bool
	// subAgentQuestionsActive is the ID the picker is currently
	// scoped to. ParentKey for the parent's own questions; a
	// subagent's ID when the user is viewing that subagent's
	// transcript. The picker reads List / Pending off this scope.
	subAgentQuestionsActive string
	// subAgentQuestionPickerOpen is true while the question
	// picker overlay is on screen. Renders the picker rows above
	// the prompt; closes on Esc / Enter on a question / selection
	// outside the pending list.
	subAgentQuestionPickerOpen bool
	// subAgentQuestionPickerCursor is the highlighted row index
	// in the picker. Up/Down move it; Enter triggers the input
	// prompt for that question.
	subAgentQuestionPickerCursor int
	// subAgentQuestionPickerOptionCursor is the highlighted option
	// index within the currently-highlighted question's Options
	// slice. Left/Right (cycle feature/ask-question-left-right-nav)
	// move it without committing; Up/Down reset it to 0 so a fresh
	// question always begins on its first option. The cursor is a
	// preview hint — Enter's commit semantic (staging the question
	// for the freeform answer flow) is unchanged.
	subAgentQuestionPickerOptionCursor int
	// subAgentQuestionAlert is the most recent alert message
	// surfaced as "<agent> has a question" rows. Kept here so the
	// status row can render it without recomputing from the
	// manager on every frame.
	subAgentQuestionAlert string
	// subAgentQuestionAnswerBuffer is the in-flight freeform
	// answer the user is typing for the selected question. The
	// picker swaps in this textarea when the user hits Enter on a
	// row; Esc cancels and restores the normal prompt.
	subAgentQuestionAnswerBuffer string
	// subAgentQuestionAnswerTarget is the question ID the
	// answer textarea is addressing. Empty while the picker is
	// closed; set on Enter so the next submit knows which
	// question to Answer.
	subAgentQuestionAnswerTarget string

	// Cycle osc-7501: OSC 7501 (Program Status Protocol) state the TUI
	// reports back to the terminal. The three fields together drive
	// (*runMode).programStatus() in program_status.go.
	//
	// lastTurnOutcome mirrors crush's UI.turnOutcome — empty until the
	// turn-end handlers (turnDoneMsg in tui_run.go, plus the future
	// chunk-Error path) call recordTurnOutcome() with ProgramStateDone /
	// ProgramStateError. Cleared when the user sends a new message so
	// the OSC 7501 status does not report "Done" alongside the new
	// turn's "Working" frame.
	lastTurnOutcome tea.ProgramState
	// pendingPermission is a test-only seam for the Blocked / Permission
	// state. The wagyu 1:1 permissionRunner today soft-denies in print
	// mode and has no interactive permission dialog, so no production
	// message flips this. The seam is in place so a future cycle that
	// introduces a permission prompt can hook into programStatus()
	// without reordering this struct. False is the only value the
	// production TUI sees.
	pendingPermission bool
	// pendingAuth is the test-only seam for Blocked / Auth — same shape
	// as pendingPermission. wagyu today routes sign-in through /logout
	// + keyring replace, not a TUI modal, so production code never sets
	// this. False at every steady-state.
	pendingAuth bool

	// In-session login model (when /login is triggered or auth is required).
	login *loginModel

	// Cycle osc-hyperlinks (cycle 2 of the OSC pair): terminal
	// capability detection for OSC 8 hyperlinks. Built once at
	// startup in runLiveTUI (after the tea.WithColorProfile call
	// so the log line can pair with the colour-profile decision)
	// and read-only thereafter. Consumers — the glamour adapter,
	// future markdown-linkers, the /changelog row renderer — all
	// read this single field instead of re-running the env
	// detection; see internal/render.DetectHyperlinkCaps for the
	// rule table the cycle ships.
	hyperlinkCaps render.HyperlinkCaps

	// Cycle plugin-fsnotify: pluginWatcher is the optional
	// internal/pluginwatch.Watcher that flips plugin/skill/marketplace
	// caches on filesystem changes. nil when (a) the user is in
	// print mode (no UI loop to drain the watcher) or (b) the home
	// tree is empty / fsnotify could not be constructed. Every loader
	// that reads ~/.gemini on the TUI path must guard on this field
	// rather than nil-receive; IsStale / MarkFresh are safe on a nil
	// receiver so a loader can call them unconditionally.
	pluginWatcher *pluginwatch.Watcher
}

// NewRunMode constructs the v1.3.5 binary 1:1 run mode.
func NewRunMode(b backend.Client, email, model, tier, cwd string, altScreen bool) *runMode {
	// Cycle 264: AGY 1.4.1 changelog line 79 — under
	// AGY_ADC_AUTH (Application Default Credentials) the
	// /remote-control slash command is hidden from the picker
	// because Remote Control does not yet support ADC. We
	// compute the hidden set up front (before `m` is built)
	// and apply it after the struct literal below.
	pickerHidden := pickerHiddenCommands()
	if email == "" {
		email = auth.EffectiveUserEmail()
	}
	if email == "" {
		email = "navigaid@gmail.com"
	}
	// Inspect agw backend for its configured model before applying the
	// empty-model default, so an agw client configured with MiniMax-M3
	// (the default ExternalModel) wins over the Gemini fallback.
	if b != nil && model == "" {
		if ab, ok := b.(agwBackend); ok {
			cfg := ab.AgwConfig()
			if isMiniMax(cfg.ExternalModel) {
				model = "MiniMax M3"
			}
		}
	}
	// Normalise any MiniMax slug to the display label before the empty-model
	// default fires, so --model minimax-m3 or --model MiniMax-M3 all land here.
	if isMiniMax(model) {
		model = "MiniMax M3"
	}
	if model == "" {
		model = "Gemini 3.8 Flash (High)"
	}
	// Cycle 146: the v1.3.6 reference defaults tier to "Antigravity
	// Starter Quota" on a fresh session, which the banner email
	// row renders as
	//   "navigaid@gmail.com (Antigravity Starter Quota)"
	// Cycle 139 stripped this default based on a stale capture
	// where the harness happened to run with an unset tier; the
	// steady-state ref behaviour is to render the parenthetical.
	// The tier string is still independent — backend calls, /usage,
	// and the picker can override it later.
	if tier == "" {
		tier = "Antigravity Starter Quota"
	}
	if cwd == "" {
		if c, err := os.Getwd(); err == nil {
			cwd = c
		} else {
			cwd = "/"
		}
	}
	m := &runMode{
		email: email,
		model: model,
		// Cycle 80: statusModel is the model name with any
		// parenthetical tier suffix removed. e.g. "Gemini 3.8 Flash
		// (High)" → "Gemini 3.8 Flash". This is what the status row
		// in the v1.3.5 binary renders.
		statusModel: stripTier(model),
		tier:        tier,
		cwd:         cwd,
		backend:     b,
		altScreen:   altScreen,
		session:     nil,
		// Cycle tasks-panel: the registry is shared with
		// internal/tools so the bash wrappers' Spawn / Wait
		// updates flow into /tasks and the status row. Set
		// before any tool can run (which happens in
		// EnableTools below).
		taskReg:             tools.SharedTaskRegistry(),
		taskSubagentManager: tools.SharedSubagentManager(),
		// Cycle subagent-question-picker: the per-subagent
		// pending question store. headless=false so the
		// interactive picker settles every question; the -p
		// print path uses its own NewManager(true) and skips
		// the picker entirely.
		subAgentQuestions: subagentquestions.NewManager(false),
		// subAgentQuestionsActive defaults to the parent key
		// — the picker always opens on the parent's own
		// questions. The user can change scope via the
		// upcoming /agents-style listing once cycle 229
		// surfaces real subagent transcripts.
		subAgentQuestionsActive: subagentquestions.ParentKey,
		// Cycle 86: per-session UUID shown in /help general panel.
		// Format: 8-4-4-4-12 hex digits (RFC 4122 v4) but we use
		// crypto/rand to fill 16 bytes; if rand fails (extremely
		// rare) we fall back to a deterministic timestamp-based UUID
		// so the help overlay still renders something useful.
		conversationID: newConversationID(),
		// Cycle 119: the transcript viewport starts pinned to the bottom
		// (store.Manager.AutoScroll defaults on in the reference).
		autoScroll: true,
		// Cycle 122: the wrap cache is dirty until the first render runs.
		rowsCacheDirty: true,
		// Cycle 253: no pinned prompt at construction — the viewport
		// is pinned to the bottom and the user hasn't scrolled into a
		// turn. transcriptWindow repopulates this on every render.
		pinnedPromptRowIdx: -1,
		// Cycle 228: production TUI streams /codesearch results
		// progressively. Tests opt out (see streamingContent
		// construction sites in the cycle-213 test file).
		streamingSearch: true,
		// Cycle 229: replyStart anchors the model-reply live-stream
		// range. -1 means "no stream in flight", which the turnDoneMsg
		// handler reads as "append glamour'd reply at end" instead of
		// "replace streamed raw". -1 is the right default for tests
		// that drive turnDoneMsg without first sending any
		// backendChunkMsg.
		replyStart:     -1,
		liveReplyIdx:   -1,
		activeBtwIndex: -1,
		// Cycle 231: atRouter shares the cycle-231 default registry so
		// any subagent registered before the TUI came up is reachable
		// from the prompt input. Tests can overwrite this field after
		// NewRunMode returns to inject a private Manager.
		atRouter: DefaultRouter,
		// Cycle btw-runs-hooks: lifecycle-hook bus. Production TUI
		// sessions start with an empty Manager; subagent / extension
		// hosts can call Register before the first /btw invocation
		// (or any other event the future cycle wires up). Tests
		// overwrite m.hooks after NewRunMode returns to inject a
		// Manager with custom handlers.
		hooks: hooks.NewManager(),
		// Cycle 250: capture the session start time once so every
		// /narration call in this TUI session hashes against the
		// same RFC3339Nano-encoded bytes and lands on the same
		// catalogue entry. The time is captured at NewRunMode
		// construction — re-seeding it on every call would defeat
		// the "two consecutive calls return the same paragraph"
		// guarantee.
		narrationStart: time.Now(),
		// Cycle 292: a conversation's first turn tells the model which
		// model it is ("from None to …"). /clear and /new restore this.
		wireSettingsPending: true,
	}
	// Cycle alt-keybinding-option-meta: load
	// ~/.gemini/config/keybindings.json (or whatever ANTIGRAVITY_HOME
	// resolves to) so the alt+letter dispatcher is wired before the
	// first key arrives. Missing file is fine — Load returns an empty
	// dispatcher and the alt-keytranslation gate short-circuits.
	if kb, err := keybind.Load(keybind.DefaultPath()); err == nil {
		m.keybindings = kb
	}
	// Cycle tasks-panel: arm the registry subscriber so /tasks
	// and the status row re-render on every state change. The
	// goroutine inside startTasksSubscription just bumps
	// m.tasksDirty; the TUI's existing 100 ms tick handler is
	// what actually triggers a redraw.
	m.startTasksSubscription()
	// feature/announcement-cards: the production TUI calls
	// m.loadAnnouncements() immediately after NewRunMode so the
	// in-memory visible queue is seeded with the catalog minus
	// whatever the user has dismissed. Tests that drive NewRunMode
	// directly without going through runLiveTUI get an empty
	// queue — the announcement row is not part of the legacy
	// byte-exact layouts those tests assert against. The
	// announcement_cards_test.go file calls loadAnnouncements()
	// explicitly to set up.
	m.input = tas.New()
	m.input.SetVirtualCursor(false)
	m.input.Prompt = inputPrompt
	// Cycle 74e: dynamic prompt so multi-line input renders '> ' only
	// on the first visual line and 2 spaces of indent on continuation
	// lines (matches the v1.3.5 binary byte-for-byte; the bubbles
	// textarea's static Prompt field would otherwise paint '> ' on
	// every wrapped line).
	// Cycle 89: ref binary renders the prompt `>` glyph in saturated
	// blue (\x1b[38;5;69m). Cycle 90 fix: emit the blue color via
	// lipgloss's Focused.Prompt style rather than embedding raw ANSI
	// in SetPromptFunc's returned string. Bubbles' textarea counts
	// Cursor().X by visible runes; if we put ANSI bytes in the
	// returned prompt string, Cursor().X gets confused and cursor X
	// ends up offset by the ANSI byte count (cycle 90 fix — multi-
	// byte rune cursor accuracy).
	m.input.SetPromptFunc(2, func(info tas.PromptInfo) string {
		if info.LineNumber == 0 {
			return "> "
		}
		return "  "
	})
	m.input.Placeholder = ""
	m.input.ShowLineNumbers = false
	m.input.SetWidth(inputWidth)
	m.input.SetHeight(1)
	// Cycle 82: bumped MaxHeight 8 → 24 so the input box can grow
	// beyond 8 lines without scrolling. The v1.3.5 binary's input is
	// effectively unbounded (it grew to 30 rows in our 30-line input
	// test, with no internal scroll). The two separators stay anchored
	// around the input box and slide vertically as the input height
	// changes — top sep at fixed row 10, bottom sep = top sep + 1 +
	// input row count. Status row slides together with the bottom sep.
	// We pick 24 as the upper cap to stay safely within the 50-row
	// terminal we capture into (banner+warning=10 rows + 24 input +
	// sep + status = 36 rows, leaving room for the spinner + picker).
	m.input.MaxHeight = 24
	m.input.CharLimit = 0
	m.input.EndOfBufferCharacter = ' '
	m.input.DynamicHeight = true
	m.input.MinHeight = 1
	// Cycle 90 / 126: render the prompt `>` glyph in the reference's
	// true-colour blue (24-bit RGB 66,133,244) rather than the
	// 256-colour fallback. The lipgloss.Color("#4285F4") API takes
	// the same `#RRGGBB` form the original code used, so the byte
	// sequence we emit becomes `38;2;66;133;244m` (true-colour)
	// instead of `38;5;69m` (256-colour). Using a lipgloss style
	// (rather than raw ANSI in SetPromptFunc) keeps bubbles'
	// Cursor().X counting visible runes only, so our +1 offset in
	// View() lands correctly after the prompt glyph regardless of
	// multi-byte runes typed.
	// Cycle 283: Solarized blue #268BD2 (the 1.4.x prompt colour), not
	// the 1.3.x brand blue #4285F4.
	promptBlueStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#268BD2"))
	// The mode hint is the input's placeholder; the reference renders it
	// in Solarized base00 #657B83 (same as the status row).
	placeholderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("#657B83"))
	st := m.input.Styles()
	st.Focused.Base = lipgloss.NewStyle()
	st.Focused.CursorLine = lipgloss.NewStyle()
	st.Focused.Text = lipgloss.NewStyle()
	st.Focused.Prompt = promptBlueStyle
	st.Focused.Placeholder = placeholderStyle
	st.Focused.EndOfBuffer = lipgloss.NewStyle()
	st.Blurred.Base = lipgloss.NewStyle()
	st.Blurred.Text = lipgloss.NewStyle()
	st.Blurred.Prompt = promptBlueStyle
	st.Blurred.Placeholder = placeholderStyle
	st.Blurred.EndOfBuffer = lipgloss.NewStyle()
	m.input.SetStyles(st)
	m.input.Focus()
	if initScheme := auth.SettingsString("colorScheme"); initScheme != "" {
		m.applyThemeLive(initScheme)
	}

	// Cycle 76: initialize streaming spinner with the Dot frameset.
	// Ref's frame set is "⣾ ⣽ ⣻ ⢿ ⡿ ⣟ ⣯ ⣷" (8 frames) at 100ms each,
	// matching sp.Dot exactly.
	m.spinner = sp.New(sp.WithSpinner(sp.Dot))

	// Cycle 264: AGY 1.4.1 changelog line 79 — apply the
	// session-level picker hidden set now that m exists. The TUI
	// already calls m.picker.Update() on every keystroke; the
	// hidden set persists across the picker filter pass so
	// /remote-control disappears from the menu when ADC is in
	// effect.
	if len(pickerHidden) > 0 {
		m.picker.SetHiddenCommands(pickerHidden...)
	}

	return m
}

// pickerHiddenCommands returns the slash-command names the picker
// should suppress at session start. Cycle 264: only "/remote-control"
// is hidden, and only when AGY_ADC_AUTH is set (see IsBusinessAccount
// in cmd/wagyu/business_account.go). Picked out as a helper so future
// feature flags can add their own hidden entries without touching
// NewRunMode's struct-literal block.
//
// Kept in tui_run.go (not remote_control_adc.go) because it is a TUI
// presentation concern — the picker is a TUI surface, and so is the
// slash-command handler in cmd/wagyu/slash_commands.go. The actual
// ADC-or-not predicate still lives in business_account.go (one
// place, two callers — the runRoot --remote-control gate and these
// TUI surfaces).
func pickerHiddenCommands() []string {
	if !IsBusinessAccount() {
		return nil
	}
	return []string{"/remote-control"}
}

// tokenKMid formats a token count for the compact status row, the
// midstream "184 K" / "1.2 M" form cycle 227's compact note carries. The
// "Mid" suffix distinguishes this from tokenK in cmd/wagyu/print_pig.go
// (the print-path helper — same formatter, kept separate so the two
// files can evolve without an import dance).
func tokenKMid(tokens int) string {
	if tokens <= 0 {
		return "-"
	}
	if tokens < 1000 {
		return strconv.Itoa(tokens)
	}
	if tokens < 1_000_000 {
		kilo := float64(tokens) / 1000.0
		return strings.TrimSuffix(strconv.FormatFloat(kilo, 'f', 1, 64), ".0") + " K"
	}
	mega := float64(tokens) / 1_000_000.0
	return strings.TrimSuffix(strconv.FormatFloat(mega, 'f', 1, 64), ".0") + " M"
}

// compactDuration formats a (start, end) pair as "3s" / "1m 12s" — the
// bracket the cycle-227 spec asks the status row to render between
// the "184 K → 42 K" token figures. Negative durations (end before
// start, which a clock skew on a test host can produce) clamp to 0
// so the row never prints "(−2s)".
func compactDuration(start, end time.Time) string {
	if start.IsZero() || end.IsZero() {
		return "0s"
	}
	d := end.Sub(start)
	if d < 0 {
		d = 0
	}
	return d.Truncate(time.Second).String()
}

// Init implements tea.Model.
func (m *runMode) Init() tea.Cmd {
	// Cycle tasks-panel: arm the live-update tick alongside the
	// input-cursor blink. The tick is re-armed on every event
	// (see Update → tasksTickMsg) so the 100 ms cadence is
	// self-sustaining as long as a task is dirty.
	cmds := []tea.Cmd{tas.Blink, armTasksTick(), fetchPlanTierCmd()}
	// Cycle status-line-script: when a statusLine command is
	// configured the external rendering path needs its own 100 ms
	// tick. Re-armed in Update → statusLineExternalTickMsg, so the
	// chain is self-sustaining as long as external mode is active.
	// (When external mode is off, the tick handler is a no-op so
	// the chain dies the moment it stops being re-armed — we
	// deliberately do not arm it from /statusline toggles because
	// doing so would race with the bubbletea runtime.)
	if m.statusLineExternal {
		cmds = append(cmds, armExternalStatusLineTick())
	}
	return tea.Batch(cmds...)
}

// cycleMode advances the agent execution mode the way Shift+Tab does in the
// reference. Recovered 1.3.5 changelog: "Choose it with `--mode auto`, from
// `Agent Mode` in `/config`, or by pressing `Shift+Tab` to cycle modes."
//
// The mode is applied to the live permission policy, so the tool gate uses the
// new mode on the next call.
//
// Cycle 283: the mode cue is the empty input's *placeholder* plus a prefix on
// the status row's model label ("auto · MiniMax M3"). The pre-283 code appended
// a line to the transcript instead, which the reference never does — verified
// against agy 1.4.3 at 200x40: three Shift+Tab presses leave the transcript
// empty while the input row cycles
//
//	> Auto mode: edits & safe commands auto-approved (shift+tab to cycle)
//	> Plan mode: research & plan only (shift+tab to cycle)
//	>                                   (default: no placeholder)
//	> Accept-edits mode: file edits auto-approved (shift+tab to cycle)
//
// and the status row reads auto/plan/accept-edits · <model>.
func (m *runMode) cycleMode() {
	next := permissions.NextMode(m.mode)
	m.mode = next
	if m.policy != nil {
		m.policy.Mode = next
	}
	m.input.Placeholder = permissions.ModeBanner(next)
	m.modeHintDismissed = false
}

// EnableTools installs the advertised tool surface and the runner that
// executes the model's calls for the interactive session. Cycle 99: without
// this the TUI is text-only and the model cannot act (see tui_tools.go).
func (m *runMode) EnableTools(decls []backend.ToolDecl, runner agent.ToolRunner) {
	m.tools = decls
	m.toolRunner = runner
}

// moveWordLeft runs Alt+Left word-back motion on the prompt textarea
// and is a no-op when the cursor is already at the start of the buffer.
// See wordmove.go for the macOS-convention semantics. The method is
// exposed as a receiver so wordmove_test.go can drive the helper
// without standing up a full bubbletea program.
func (m *runMode) moveWordLeft() bool {
	return m.moveWord(true)
}

// moveWordRight runs Alt+Right word-forward motion. The boolean
// reports whether the cursor actually moved (false at the boundaries
// — start for moveWordLeft, end for moveWordRight).
func (m *runMode) moveWordRight() bool {
	return m.moveWord(false)
}

// moveWord is the shared runner for moveWordLeft / moveWordRight. The
// `back` parameter selects the direction. The current cursor position
// is read from m.input (via the public Line / Column methods), the
// new (row, col) is computed by wordMove (in wordmove.go) and applied
// to the textarea via SetCursorColumn after walking rows with
// CursorUp / CursorDown if the motion crossed a line boundary.
func (m *runMode) moveWord(back bool) bool {
	row := m.input.Line()
	col := m.input.Column()
	val := m.input.Value()
	var newRow, newCol int
	var moved bool
	if back {
		newRow, newCol, moved = wordMoveLeft(val, row, col)
	} else {
		newRow, newCol, moved = wordMoveRight(val, row, col)
	}
	if !moved {
		return false
	}
	// Walk rows if the motion crossed a '\n'. CursorUp / CursorDown
	// are the only public surface to move the row index.
	for row > newRow {
		m.input.CursorUp()
		row--
	}
	for row < newRow {
		m.input.CursorDown()
		row++
	}
	m.input.SetCursorColumn(newCol)
	return true
}

// subAgentExtras builds the AGY 1.4.0 subagent toolset (cycle 228) for
// the TUI's per-turn wiring. It opens a parallel coding.Services on the
// same AgentDir the TUI uses, derives the pi.Model from the backend
// (agw via the native provider, anything else via piagentbridge), and
// returns the three tools plus a close func that releases the services.
//
// The cost of reopening services per turn is one auth-storage open
// (~1ms) and one settings reload; cycle 229 can cache this on runMode
// once the TUI gets a long-lived PiG runtime of its own.
//
// The PermissionGate wiring is intentionally nil — the TUI doesn't
// thread its policy through to the subagent today. Cycle 229 will
// reuse m.policy via a thin internal/permissions adapter so the
// subagent's tool calls honour the same Shift+Tab state the parent
// does.
// agentToolRegistry is the TUI's advertised catalogue: the full recovered
// surface for the default agent, or the captured seven declarations for a
// --agent markdown agent.
func agentToolRegistry(workspace, agentBody string) *tools.Registry {
	// Cycle 293: RuntimeCatalog adds read_file / write_file / edit_file, the
	// names the 1.4.4 default agent actually calls. Catalog stays the 60-name
	// print-mode init list.
	catalog := tools.RuntimeCatalog(workspace)
	if agentBody != "" {
		return agenttools.Registry(catalog)
	}
	return catalog
}

// registerSubagentQuestionTool re-registers ask_question with the parent's
// per-subagent question manager. A custom agent's captured surface has no
// ask_question, so filtering must not be undone by this registration.
func (m *runMode) registerSubagentQuestionTool(catalog *tools.Registry) {
	if m.agentBody != "" {
		return
	}
	catalog.Register(&tools.AskQuestion{
		SubAgentQuestions: m.subAgentQuestions,
		SubAgentID:        subagentquestions.ParentKey,
	})
}

// applyAgentSwitch activates a custom markdown agent (or the built-in
// default when name is empty) after /agents prepares a selection and the
// user presses Esc. The reference forks the conversation at that point:
// the old transcript is saved and a fresh one starts under the new agent.
// Rebuild the advertised tool catalogue so a custom agent gets exactly the
// captured seven declarations instead of the default surface.
func (m *runMode) applyAgentSwitch(name, body string) {
	m.saveSession()
	m.agentName = name
	m.agentBody = body
	workspace := m.cwd
	if workspace == "" {
		workspace, _ = os.Getwd()
	}
	if m.policy != nil {
		catalog := agentToolRegistry(workspace, body)
		m.registerSubagentQuestionTool(catalog)
		m.EnableTools(advertisedToolDecls(body, catalog), &permissionRunner{
			inner:   &registryRunner{reg: catalog},
			policy:  m.policy,
			sandbox: m.toolSandbox,
		})
	}
	// Fork: clear the visible conversation and mint a new conversation id
	// so the previous one remains resumable.
	m.response = nil
	m.session = nil
	m.blocks = nil
	m.liveSteps = nil
	m.activeTool = ""
	m.autoTitle = ""
	m.conversationID = newConversationID()
}

func (m *runMode) subAgentExtras(_ context.Context, be backend.Client, modelLabel string) ([]piagent.AgentTool, func()) {
	if be == nil || m.agentBody != "" {
		return nil, func() {}
	}
	piModel := pigPrintModel(be, modelLabel)
	if piModel == nil {
		return nil, func() {}
	}
	workspace := m.cwd
	if workspace == "" {
		workspace = defaultWorkspaceForRunMode()
	}
	agentDir := auth.AntigravityConfigHome()
	services, err := coding.CreateAgentSessionServices(coding.CreateAgentSessionServicesOptions{
		CWD:      workspace,
		AgentDir: agentDir,
	})
	if err != nil {
		// Subagent tools are an extra layer of convenience; if the
		// services can't open (broken auth dir), the TUI's normal
		// toolset still works. Log-and-skip mirrors print-mode's
		// reaction to a missing mcp.json.
		printModeSilenceLogger.Printf("[tui] subagent services: %v", err)
		return nil, func() {}
	}
	tools := subagenttools.Tools(subagenttools.Deps{
		Services: services,
		Model:    piModel,
		CWD:      workspace,
		AgentDir: agentDir,
	})
	closeFn := func() { services.Close() }
	return tools, closeFn
}

// defaultWorkspaceForRunMode returns m.cwd when set, else the
// process cwd. It is a thin indirection so the helper above does
// not have to special-case an empty m.cwd against os.Getwd.
func defaultWorkspaceForRunMode() string {
	if wd, err := os.Getwd(); err == nil {
		return wd
	}
	return ""
}

// Update implements tea.Model.
func (m *runMode) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.login != nil {
		if !isScrollMsg(msg) {
			m.rowsCacheDirty = true
		}
		switch msg := msg.(type) {
		case tea.WindowSizeMsg:
			m.width, m.height = msg.Width, msg.Height
			m.login.width, m.login.height = msg.Width, msg.Height
			return m, nil
		case tea.KeyPressMsg:
			if msg.String() == "ctrl+c" {
				m.login = nil
				return m, tea.Quit
			}
			if msg.String() == "esc" && m.login.state == loginStateSelectMethod {
				m.login = nil
				m.slashRespond("/login", "Sign in cancelled.")
				return m, nil
			}
		case exchangeSuccessMsg:
			m.login = nil
			m.email = auth.EffectiveUserEmail()
			if m.backendFor != nil {
				m.backend = m.backendFor(m.model)
			}
			m.slashRespond("/login", "Authentication successful!")
			return m, nil
		}
		newLM, cmd := m.login.Update(msg)
		if lm, ok := newLM.(*loginModel); ok {
			m.login = lm
			if lm.authenticated {
				m.login = nil
				m.email = auth.EffectiveUserEmail()
				if m.backendFor != nil {
					m.backend = m.backendFor(m.model)
				}
				m.slashRespond("/login", "Authentication successful!")
				return m, nil
			}
		}
		return m, cmd
	}

	// Cycle 122: invalidate the wrap cache for any message that could
	// mutate m.response. Scroll messages (the 6 keys and the 2 wheel
	// events) only touch m.scroll / m.autoScroll, so they keep the cache
	// and a scroll is a cache hit. Everything else — WindowSize, prompt
	// input, slash commands, streamed chunks, ctrl+o expand, the lifecycle
	// surface — is "may have mutated", and over-invalidating is safe.
	if !isScrollMsg(msg) {
		m.rowsCacheDirty = true
	}
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		// Cycle 119: the terminal geometry drives the transcript viewport
		// and the width-aware chrome. Before this the clone ignored the
		// terminal size entirely, so a view taller than the screen broke
		// bubbletea's alt-screen rendering.
		m.width, m.height = msg.Width, msg.Height
		// Cycle 268: also pin the mdrender package's view of the
		// terminal width so the cycle-232 / cycle-243 mid-stream
		// glamour pass (which resolves its width via
		// currentWidth()) renders at the actual terminal width
		// instead of falling through to the $COLUMNS env /
		// x/term default. The chat-transcript and step-detail
		// paths already take m.width directly via cycle 268's
		// renderMarkdown(text, width); this SetCurrentWidth call
		// closes the parallel path for LineANSIBounded /
		// LineANSI / LineANSIBoundedWithCache which the
		// cycle-243 cache keys against currentWidth().
		mdrender.SetCurrentWidth(msg.Width)
		// Size the prompt to the terminal so it never wraps (pre-119 the
		// textarea was fixed at the 219-column capture width).
		if w := msg.Width - len(inputPrompt); w > 10 {
			m.input.SetWidth(w)
		}
		return m, nil

	case tea.MouseClickMsg:
		// Cycle 174: in-app text selection starts on a left-button
		// press inside the transcript area. track the press cell as
		// both endpoints of an empty selection; the next Motion
		// event will grow it. Wheel-clicks (Button 2) and right
		// button (Button 3) are ignored.
		ev := msg.Mouse()
		if !m.altScreen || ev.Button != tea.MouseLeft {
			return m, nil
		}
		// Cycle 232: the picker overlay (and its accompanying input
		// row) consume clicks first. If the click lands in the
		// picker region the existing selection path is skipped so
		// the two interactions do not race for the same click.
		if m.handlePickerClick(ev) {
			return m, nil
		}
		// AGY 1.2.13: clicks outside the picker dismiss the open
		// picker. Reset the picker state before falling through to
		// the existing selection logic so a transcript click both
		// closes the dropdown AND starts the in-app selection.
		if m.picker.Visible() && !m.isInputY(ev.Y) {
			m.picker.Reset()
		}
		// Cycle 253: a click on the pinned-prompt header (the
		// topmost row of the transcript, screen row 0) jumps the
		// viewport back to that prompt instead of starting a text
		// selection. The pinned prompt is only shown in altscreen
		// mode (gated above) and only when m.pinnedPromptText is
		// non-empty (so a click on row 0 when the prompt is below
		// the fold — banner area at scroll 0 — falls through to
		// the existing selection handler).
		if m.pinnedPromptText != "" && ev.Y == 0 {
			if m.jumpToPinnedPrompt() {
				return m, nil
			}
		}
		// In-app input field mouse selection: click on input row.
		if m.isInputY(ev.Y) {
			m.selStartRow, m.selStartCol, m.selEndRow, m.selEndCol = 0, 0, 0, 0
			m.selDragging = false

			offset := m.inputOffsetAt(ev.X, ev.Y)
			m.setInputCursorOffset(offset)
			m.inputSelDragging = true
			m.inputSelStart = offset
			m.inputSelEnd = offset
			return m, nil
		}
		m.clearInputSelection()
		// Clamp into the visible transcript area; if the click is
		// outside we ignore so the picker/overlay/help windows keep
		// owning their own clicks. The transcript rows live at the
		// top of the screen — row 0 is the banner, and rows
		// beyond the viewport height are below the chrome.
		row, col, ok := m.transcriptCellAt(ev.X, ev.Y)
		if !ok {
			return m, nil
		}
		m.selDragging = true
		m.selStartRow, m.selStartCol = row, col
		m.selEndRow, m.selEndCol = row, col
		return m, nil

	case tea.MouseMotionMsg:
		// Cycle 174: extend the live selection as the user drags.
		// We trigger a re-render only while a drag is in progress so
		// idle motion events don't churn the frame.
		// Cycle 232: hover over the picker rows changes the
		// highlight. Run before the drag check so hover also fires
		// while a selection drag is in progress (the user can mouse
		// back into the picker while still dragging in the
		// transcript — though since the picker dismisses on
		// outside-click, this is mostly for the same-frame re-render
		// when the picker is open and the user moves the mouse).
		if m.altScreen {
			m.handlePickerHover(msg.Mouse())
		}
		if m.inputSelDragging && m.altScreen {
			ev := msg.Mouse()
			offset := m.inputOffsetAt(ev.X, ev.Y)
			if offset != m.inputSelEnd {
				m.inputSelEnd = offset
				m.setInputCursorOffset(offset)
			}
			return m, nil
		}
		if !m.selDragging || !m.altScreen {
			return m, nil
		}
		row, col, ok := m.transcriptCellAt(msg.Mouse().X, msg.Mouse().Y)
		if !ok {
			return m, nil
		}
		if row != m.selEndRow || col != m.selEndCol {
			m.selEndRow, m.selEndCol = row, col
		}
		return m, nil

	case tea.MouseReleaseMsg:
		// Input field selection release: copy selection to clipboard on release.
		if m.inputSelDragging {
			m.inputSelDragging = false
			ev := msg.Mouse()
			if ev.Button == tea.MouseLeft {
				m.inputSelEnd = m.inputOffsetAt(ev.X, ev.Y)
			}
			start, end := m.inputSelectionRange()
			if start < end {
				val := m.input.Value()
				if start < len(val) && end <= len(val) {
					text := val[start:end]
					_ = copyToClipboard(text)
					return m, m.copySelectionCmd(text)
				}
			} else {
				m.clearInputSelection()
			}
			return m, nil
		}
		// Cycle 174: copy the selection to the clipboard on release,
		// then clear the drag state. A click without a drag
		// (start == end) is a no-op for copy — the highlight
		// vanishes, no OSC52 is emitted.
		if !m.selDragging {
			return m, nil
		}
		m.selDragging = false
		ev := msg.Mouse()
		if ev.Button != tea.MouseLeft {
			m.selStartRow, m.selStartCol, m.selEndRow, m.selEndCol = 0, 0, 0, 0
			return m, nil
		}
		row, col, ok := m.transcriptCellAt(ev.X, ev.Y)
		if ok {
			m.selEndRow, m.selEndCol = row, col
		}
		text := m.selectionText()
		// Reset state before emitting the copy cmd so the re-render
		// produced by View() (after Update returns) sees an empty
		// highlight.
		m.selStartRow, m.selStartCol, m.selEndRow, m.selEndCol = 0, 0, 0, 0
		if text != "" {
			_ = copyToClipboard(text)
			return m, m.copySelectionCmd(text)
		}
		return m, nil

	case tea.MouseMsg:
		// Cycle 119: the alt-screen strategy owns wheel events
		// (viewport.AltScreenStrategy.ShouldHandleWheel).
		if m.handleMouseWheel(msg) {
			return m, nil
		}
		return m, nil

	case editorFinishedMsg:
		// Cycle 108/110: an external editor process returned. /open reports
		// a failure with the recovered "Editor exited with error: %v";
		// ctrl+g additionally reads the scratch file back into the prompt.
		if msg.tempPath != "" {
			defer func() { _ = os.Remove(msg.tempPath) }()
			if msg.err == nil {
				if data, err := os.ReadFile(msg.tempPath); err == nil {
					m.input.SetValue(strings.TrimRight(string(data), "\n"))
				}
			}
		}
		if msg.err != nil {
			m.response = append(m.response, "Editor exited with error: "+msg.err.Error())
		}
		return m, nil

	case resumeBlinkMsg:
		// Cycle 184: the /resume search cursor blinks. The loop re-arms
		// itself only while the picker is open with a query to sit after;
		// closing the picker (or clearing the query) drops the pending
		// tick and the loop ends.
		if m.resumeMode && m.resumeQuery != "" {
			m.resumeCursorOn = !m.resumeCursorOn
			return m, resumeBlink()
		}
		m.resumeCursorOn = false
		return m, nil

	case keybind.EscTimeoutMsg:
		// Cycle alt-keybinding-option-meta: the buffered Esc
		// release tick fired. If a follow-up key never arrived
		// inside keybind.EscReleaseWindow, run the standard Esc
		// branch now so the user sees their keypress take effect.
		if m.altEscBuf.Pending {
			m.altEscBuf.Pending = false
			_, _ = m.handleEscKey()
		}
		return m, nil

	case voiceConnectDoneMsg:
		return m.handleVoiceConnectDone(msg)

	case voiceTickMsg:
		return m.handleVoiceTick()

	case tea.PasteMsg:
		// Cycle 278: bracketed paste (Cmd+V / the terminal's own paste,
		// sent as an escape-framed payload when bracketed paste is on).
		// Before this the message had no case and was dropped, so pasting
		// a block of text did nothing at all.
		return m, m.insertPastedText(msg.Content)

	case tea.KeyMsg:
		// Cycle alt-keybinding-option-meta: the macOS Option key on
		// Terminal.app / VS Code-based terminals arrives as a bare
		// Esc followed by a letter in two separate events. The
		// consumeAltEsc helper bridges that gap by either
		// synthesising alt+<letter> for the dispatcher or releasing
		// the buffered Esc through handleEscKey. On terminals that
		// already forward Option as Meta (iTerm2, kitty, WezTerm,
		// Alacritty, ghostty) this is a no-op.
		newMsg, consumed, escCmd := m.consumeAltEsc(msg)
		if consumed {
			return m, escCmd
		}
		msg = newMsg
		key := msg.Key()
		// If we just synthesised an alt+letter, fire the
		// corresponding binding before the rest of the dispatcher
		// sees the event. This keeps the seam testable: a
		// configured alt+s is consumed exactly once, no double-
		// handling in vim mode either (alt+s reaches vim as
		// alt+s, never as Esc-then-s).
		if r, ok := keybind.IsAltLetter(key); ok {
			if m.keybindings != nil {
				if action, ok := m.keybindings.LookupAltLetter(r); ok {
					m.altDispatchAction(r, action)
					return m, nil
				}
			}
			// Cycle 244: alt+m is a built-in global shortcut — AGY 1.4.0
			// "send with opposite Queued Messages setting" — independent
			// of the user's keybindings.json. The check lives here (after
			// the configured-binding branch) so a user-supplied
			// `{"alt+m": ...}` in keybindings.json wins over the built-in
			// toggle, matching AGY's "user overrides built-in" precedence
			// model. With the keymap empty (the default) the built-in
			// toggle fires below.
			if r == 'm' {
				return m, m.toggleQueueMode()
			}
		}
		// If voice mode is active, voice owns every key until dismissed or confirmed.
		if m.voiceMode {
			if handled, cmd := m.handleVoiceKey(key); handled {
				return m, cmd
			}
		}
		// Cycle 206: the four list panels (/tasks /artifact /codesearch
		// /context) own every key until Esc, so they must be consulted
		// *before* the global ctrl+l / ctrl+o / ctrl+g bindings below.
		// The cycle-202 panel branch further down is too late for these:
		// the reference leaves the panel untouched when ctrl+g or ctrl+l
		// is pressed with one open, while the clone opened $EDITOR over
		// it and toggled alt-screen underneath it (agy206/probe_one.py).
		if listPanelKind(m.panel.kind) {
			if handled, cmd := m.handlePanelKey(key); handled {
				return m, cmd
			}
		}
		if key.Code == 'l' && key.Mod.Contains(tea.ModCtrl) && !m.resumeMode {
			m.scroll = 0
			return m, tea.ClearScreen
		}
		// Cycle 99: ctrl+o toggles the tool-step groups between the
		// collapsed hint and the expanded detail view. Recovered: the
		// KeyToggleExpandGroup keybinding name, and the hints
		// "ctrl+o to expand" / "ctrl+o to collapse".
		//
		// Cycle 284: the in-flight turn's steps are not in m.blocks
		// yet (turnDoneMsg builds that block), so rerenderBlocks alone
		// left every already-visible step collapsed until the turn
		// ended. rerenderLiveSteps restyles those rows in place.
		if key.Code == 'o' && key.Mod.Contains(tea.ModCtrl) && !m.resumeMode {
			m.expanded = !m.expanded
			m.rerenderBlocks()
			m.rerenderLiveSteps()
			return m, nil
		}
		// Cycle 110: ctrl+g edits the prompt in the configured editor.
		// Recovered from the changelog: "Fixed pressing `Ctrl+G` to edit the
		// prompt in a full-screen terminal editor such as `vim` hanging…
		// external editors now run against a real terminal stdout."
		if key.Code == 'g' && key.Mod.Contains(tea.ModCtrl) && !m.resumeMode {
			return m, m.editPromptCmd()
		}
		// Cycle subagent-question-picker: ctrl+k (or plain `a`
		// when the input is empty) opens the question picker
		// when there is at least one pending question for the
		// currently-viewed agent. The picker lists every
		// pending question on the active scope (parent or
		// subagent); selecting one routes the answer back to
		// that agent's session. Help advertises "ctrl+k
		// Approve subagent fast" but no real handler has
		// claimed the binding — this cycle takes it.
		if key.Code == 'k' && key.Mod.Contains(tea.ModCtrl) && !m.resumeMode {
			return m, m.openSubAgentQuestionPicker()
		}
		// Cycle 181: `?` opens the help overlay when the input buffer
		// is empty — mirrors the v1.4.0 reference binary. The status
		// row's "? for shortcuts" hint points at this binding; without
		// it the hint is a lie. With text already in the prompt `?`
		// falls through to the textarea and is typed as a literal
		// rune (verified against REF: typing "hello world" then `?`
		// produces "> hello world?" rather than opening the overlay).
		//
		// The keybinding lands on the SHORTCUTS tab (helpTab=2) — the
		// v1.4.0 REF binary verifies this with the active word
		// "shortcuts" wrapped in 1;4m (bold+underline). /help opens
		// the GENERAL tab (helpTab=0); these two entry points have
		// different defaults in REF.
		if key.Code == '?' && key.Mod == 0 && m.input.Value() == "" &&
			!m.helpMode && !m.picker.Visible() && !m.resumeMode {
			m.helpMode = true
			m.helpTab = 2 // shortcuts
			m.helpWindow = 0
			m.helpCursor = 0
			m.input.Reset()
			m.clearInputSelection()
			m.historyIdx = 0
			m.historyEdit = ""
			return m, nil
		}
		// F2 opens the rename conversation panel.
		// Matches AGY 1.4.0 shortcuts: "f2 Rename".
		if key.Code == tea.KeyF2 && !m.resumeMode && !m.helpMode && m.panel.kind == panelNone && !m.picker.Visible() {
			m.openRenamePanel()
			return m, nil
		}
		// F5 starts voice dictation.
		// Matches AGY shortcuts: "f5 Start voice dictation" and keybinding KeyToggleDictation.
		if key.Code == tea.KeyF5 && !m.resumeMode && !m.helpMode && m.panel.kind == panelNone && !m.picker.Visible() && !m.voiceMode {
			return m, m.startVoiceDictation("f5")
		}
		// Cycle 202: an open slash-command panel owns every key until it
		// is dismissed (Esc), matching the reference's full-screen pickers.
		// This block must precede the transcript scroll bindings below: the
		// document viewers (/changelog, /usage) drive their window with
		// pgup/pgdown/ctrl+home/ctrl+end, and those keys would otherwise be
		// consumed by handleScrollKey even while the panel is on screen.
		if m.panel.kind != panelNone {
			if handled, cmd := m.handlePanelKey(key); handled {
				return m, cmd
			}
		}
		// Cycle 119: the recovered transcript scroll bindings (pgup /
		// pgdown / shift+up / shift+down / ctrl+home / ctrl+end). The
		// picker and the /help overlay replace the transcript, so they
		// keep the keys for themselves.
		if !m.helpMode && !m.picker.Visible() && !m.resumeMode && m.handleScrollKey(key) {
			return m, nil
		}
		// Cycle 167 / 184: the /resume picker owns every key while it is
		// open. Up/Down move the cursor one row (scrolling at the edges),
		// Right/Left page by one window, Tab cycles the CLI/Other tabs,
		// Enter resumes the highlighted conversation, Esc clears the
		// search first and only closes the picker when it is empty, and
		// any other printable text edits the search query.
		if m.resumeMode {
			rows := m.resumeFiltered()
			total := len(rows)
			switch key.Code {
			case tea.KeyEsc:
				if m.resumeQuery != "" {
					m.resumeQuery = ""
					m.resumeWindow, m.resumeCursor = 0, 0
					return m, nil
				}
				m.resumeMode = false
				m.resumeList = nil
				m.resumeCursor = 0
				m.resumeWindow = 0
				m.resumeQuery = ""
				m.resumeTab = 0
				return m, nil
			case tea.KeyUp:
				m.resumeListUp(total)
				return m, nil
			case tea.KeyDown:
				m.resumeListDown(total)
				return m, nil
			case tea.KeyRight:
				m.resumeListPage(total, 1)
				return m, nil
			case tea.KeyLeft:
				m.resumeListPage(total, -1)
				return m, nil
			case tea.KeyTab:
				m.resumeTab = (m.resumeTab + 1) % 2
				m.resumeWindow, m.resumeCursor = 0, 0
				return m, nil
			case tea.KeyEnter:
				// Cycle 278: on the empty screen "Press Enter to return."
				// means exactly that — close the overlay, echo nothing.
				if total == 0 {
					m.resumeMode = false
					m.resumeList = nil
					m.resumeCursor = 0
					m.resumeWindow = 0
					m.resumeQuery = ""
					m.resumeTab = 0
					return m, nil
				}
				if m.resumeTab != 0 || m.resumeCursor < 0 || m.resumeCursor >= total {
					return m, nil
				}
				picked := rows[m.resumeCursor]
				m.resumeMode = false
				m.resumeList = nil
				m.resumeCursor = 0
				m.resumeWindow = 0
				m.resumeQuery = ""
				m.resumeTab = 0
				return m, m.resumeConversation(picked)
			case tea.KeyBackspace:
				if m.resumeQuery != "" {
					r := []rune(m.resumeQuery)
					m.resumeQuery = string(r[:len(r)-1])
					m.resumeWindow, m.resumeCursor = 0, 0
					m.resumeCursorOn = true
					return m, resumeBlink()
				}
				return m, nil
			case tea.KeyF4:
				// F4 deletes the highlighted conversation from store and refreshes list.
				// Matches AGY 1.4.0 shortcuts: "f4 Delete" and /resume keyboard hint.
				if m.resumeTab == 0 && total > 0 && m.resumeCursor >= 0 && m.resumeCursor < total {
					picked := rows[m.resumeCursor]
					if picked.ID != "" {
						_ = convstore.DeleteConversation(picked.ID)
						m.reloadResumeList()
						rows = m.resumeFiltered()
						total = len(rows)
						if m.resumeCursor >= total && total > 0 {
							m.resumeCursor = total - 1
						}
						if m.resumeWindow > m.resumeCursor {
							m.resumeWindow = m.resumeCursor
						}
					}
					return m, nil
				}
			case tea.KeyF2:
				// F2 renames the highlighted conversation.
				// Matches AGY 1.4.0 shortcuts: "f2 Rename" and /resume keyboard hint.
				if m.resumeTab == 0 && total > 0 && m.resumeCursor >= 0 && m.resumeCursor < total {
					picked := rows[m.resumeCursor]
					m.resumeMode = false
					m.resumeList = nil
					m.conversationName = picked.Title
					m.openRenamePanel()
					return m, nil
				}
			}
			// Ctrl+U clears the search (the hint's "Clear search").
			if key.Code == 'u' && key.Mod.Contains(tea.ModCtrl) {
				m.resumeQuery = ""
				m.resumeWindow, m.resumeCursor = 0, 0
				return m, nil
			}
			if isTypedText(key) {
				m.resumeQuery += key.Text
				m.resumeWindow, m.resumeCursor = 0, 0
				// Editing the query shows the cursor immediately and
				// restarts its blink, the same way bubbles' textarea
				// resets its own cursor on a keystroke.
				m.resumeCursorOn = true
				return m, resumeBlink()
			}
			return m, nil
		}
		// Cycle subagent-question-picker: the question picker
		// owns every key while it is open. Up/Down move the
		// cursor; Enter opens the answer input for the
		// highlighted question; Esc closes the picker without
		// answering. ctrl+k / `a` toggle (close) the picker when
		// it's already open.
		if m.subAgentQuestionPickerOpen {
			return m, m.handleSubAgentQuestionPickerKey(key)
		}
		// Cycle 216: vim mode. When "Editor Mode › Vim" is on and no
		// overlay owns the screen, the engine gets first refusal on the
		// key: it owns Esc (→ Normal), Enter, the motions and every text
		// rune. Keys it declines (ctrl+c, ctrl+d, ctrl+o, plain Tab, …)
		// fall through to the host bindings below, so the global exit
		// and overlay keys keep working.
		if m.vimEnabled() && !m.helpMode && !m.picker.Visible() && !m.resumeMode && m.panel.kind == panelNone {
			if cmd, handled := m.handleVimKey(key); handled {
				// A keypress clears a pending Ctrl+C exit confirmation,
				// the same way the generic binding below does.
				m.exitConfirm = false
				return m, cmd
			}
		}
		if key.Code == tea.KeyEsc {
			// Cycle 83: helpMode takes priority over picker/input dismiss.
			if m.helpMode {
				m.helpMode = false
				// The reference writes the same two-line echo every
				// slash panel uses: the `> /help` header followed by
				// `⎿  Exited /help command` and a blank row. /help is
				// a full-screen overlay, not a panel, so it has its
				// own dismissal branch and used to append only the
				// feedback row (missing the header and the blank).
				// Verified live against agy 1.4.3 at 120x40.
				m.response = append(m.response,
					slashEchoLines("/help", "Exited /help command")...)
				return m, nil
			}
			if m.picker.Visible() {
				// Cycle 150: closing the picker via Esc leaves the
				// input buffer intact. The reference binary keeps
				// the typed text in the prompt area — e.g. Esc
				// from "/changelog" leaves the prompt showing
				// "/changelog", not "" — so the user can edit
				// and submit. Previously we reset the input
				// alongside the picker, which wiped the text the
				// user just typed.
				//
				// Cycle 235: AGY 1.2.10 — "Fixed pressing Esc while
				// the suggestions dropdown is open interrupting
				// the agent's turn; it now closes the dropdown,
				// and a second Esc interrupts." The picker-close
				// path must NOT cancel the turn: only the next
				// Esc press (which falls through to the
				// streaming-interrupt branch below) interrupts.
				// Closing the dropdown therefore takes priority
				// over the interrupt.
				m.picker.Reset()
				return m, nil
			}
			// feature/at-file-dropdown-newline: closing the
			// @ file-mention dropdown also leaves the buffer
			// intact (the user has already typed "@src" and
			// we'd rather not have to retype it). Pick up
			// here so the candidate list is cleared before
			// the next branch (which resets the input).
			if m.atFileDropdown.Visible() {
				m.atFileDropdown.Reset()
				return m, nil
			}
			// Cycle 235: second Esc press (no dropdown, turn in
			// flight) cancels the model's stream. Picker-close
			// above has priority — the user's first Esc is always
			// interpreted as "close the dropdown", so this branch
			// only runs when the picker is already closed. The
			// cancel function is installed by streamPrompt and
			// cleared by the turn-end handlers (turnDoneMsg /
			// backendDoneMsg / backendChunkMsg), so a stale
			// `streaming == true` left over from a Turn that
			// returned nil from Stream() (e.g. the test fakeClient
			// or a backend error) cannot trigger a phantom
			// cancel. We swallow the cancel return — the in-flight
			// Stream will deliver its final {Error: ctx.Err(),
			// Done: true} chunk and the streamPrompt goroutine
			// turns it into the usual turnDoneMsg with whatever
			// partial text was already buffered.
			if m.streaming && m.turnCancel != nil {
				m.turnCancel()
				m.turnCancel = nil
				return m, nil
			}
			TriggerCompactCancel()
			if m.hasInputSelection() {
				m.clearInputSelection()
				return m, nil
			}
			if m.input.Value() != "" {
				m.input.Reset()
				m.clearInputSelection()
				return m, nil
			}
			// feature/announcement-cards: Esc on an empty prompt
			// dismisses the active announcement card permanently
			// (writes to settings.json) and pops the next one. This
			// branch sits between the input-reset and the silent
			// absorb because the user's gesture is specifically
			// "dismiss this card" — when there's no card to dismiss
			// we fall through to the silent absorb.
			if m.activeAnnouncement() != nil {
				m.dismissCurrentAnnouncement()
				if msg := m.announcementSaveErrorMessage(); msg != "" {
					m.response = append(m.response, msg)
				}
				return m, nil
			}
			if m.isBtwCardVisible() {
				m.DismissBtwResponse()
				return m, nil
			}
			// Absorb Esc silently — ref binary does not quit on Esc,
			// only on Ctrl+C and Ctrl+D.
			return m, nil
		}
		if key.Code == 'c' && key.Mod.Contains(tea.ModCtrl) {
			// Cycle 144: the v1.3.6 reference requires two Ctrl+C presses
			// to exit. The first press sets m.exitConfirm = true and the
			// status row swaps from "? for shortcuts" to "press ctrl+c
			// again to exit". A second Ctrl+C within the timeout
			// actually quits; any other key cancels the confirmation.
			// This matches the recovered byte-exact status row text
			// ("press ctrl+c again to exit") captured at
			// /tmp/refscroll.txt line 17.
			if m.exitConfirm {
				m.quitting = true
				return m, tea.Quit
			}
			m.exitConfirm = true
			m.exitConfirmAt = time.Now()
			return m, nil
		}
		// Cycle 288: ctrl+d acts as forward-delete when the prompt contains
		// text (or deletes active selection), only triggering exit when the
		// prompt is empty. Matches AGY 1.4.0 changelog: "Fixed ctrl+d behavior
		// to act as a forward-delete when the input prompt contains text, only
		// triggering the exit flow when the prompt is empty."
		if key.Code == 'd' && key.Mod.Contains(tea.ModCtrl) {
			if m.hasInputSelection() {
				m.deleteInputSelection()
				m.modeHintDismissed = true
				m.picker.Update([]rune(firstLine(m.input.Value())))
				m.clearEligibilityNotice()
				m.atFileDropdown.Update(m.input.Value(), m.cwd)
				return m, nil
			}
			if m.input.Value() == "" {
				m.quitting = true
				return m, tea.Quit
			}
			// When prompt contains text, ctrl+d falls through to m.input.Update
			// which executes DeleteCharacterForward.
		}
		// Cycle subagent-question-picker: plain `a` opens the
		// question picker when there is at least one pending
		// question and the input is empty. The changelog line
		// says "Ctrl+K or a in the subagent's view opens a
		// question picker" — both bindings map to the same
		// openSubAgentQuestionPicker seam. We only claim the
		// key when a real question is waiting; otherwise `a`
		// falls through to the textarea so the user can type
		// "a" normally (and openSubAgentQuestionPicker's "no
		// pending questions" path also stays out of the way
		// when the picker is invoked from Ctrl+K with no
		// pending questions — it just echoes a line).
		if key.Code == 'a' && key.Mod == 0 && m.input.Value() == "" &&
			!m.helpMode && !m.picker.Visible() && !m.resumeMode &&
			m.panel.kind == panelNone &&
			m.subAgentQuestions != nil &&
			m.subAgentQuestions.PendingCount(m.subAgentQuestionsActive) > 0 {
			return m, m.openSubAgentQuestionPicker()
		}
		// Any other keypress cancels the pending Ctrl+C confirmation.
		if m.exitConfirm {
			m.exitConfirm = false
		}
		// Cycle 104: plain Tab completes the highlighted picker row; Shift+Tab
		// is the mode cycle and must not be swallowed here.
		if key.Code == tea.KeyTab && !key.Mod.Contains(tea.ModShift) && m.picker.Selected() != nil {
			sel := m.picker.Selected()
			m.input.SetValue(sel.Name)
			m.picker.Update([]rune(sel.Name))
			m.historyIdx = 0
			m.historyEdit = ""
			return m, nil
		}
		// feature/at-file-dropdown-newline: Tab on the @ file-mention
		// dropdown splices the highlighted candidate into the input
		// buffer in place of "@<prefix>" — same gesture as the
		// slash picker above, scoped to a different overlay.
		if key.Code == tea.KeyTab && !key.Mod.Contains(tea.ModShift) && m.atFileDropdown.Visible() && m.atFileDropdown.Selected() != "" {
			if m.tabSpliceAtFile(true) {
				m.historyIdx = 0
				m.historyEdit = ""
				return m, nil
			}
		}
		// Cycle 83 / 182: when helpMode is on, Tab / ← / → cycle the
		// 3-tab /help TUI menu. Other keys (other than Esc handled
		// earlier) are swallowed.
		//
		// Cycle 182: the reference does NOT paginate the COMMANDS /
		// SHORTCUTS tabs with ← / → — those keys always cycle tabs
		// (general → commands → shortcuts → general). The lists use an
		// 18-row scrolling window driven by ↑ / ↓ (and Ctrl+Home /
		// Ctrl+End for the ends); switching tabs resets the window and
		// cursor to 0. Verified against REF v1.4.0 at 220×30; see
		// helpListDown in history.go for the full model.
		if m.helpMode {
			switch key.Code {
			case tea.KeyTab, tea.KeyRight:
				m.helpTab = (m.helpTab + 1) % len(helpTabs(m))
				m.helpWindow = 0
				m.helpCursor = 0
				return m, nil
			case tea.KeyLeft:
				n := len(helpTabs(m))
				m.helpTab = (m.helpTab + n - 1) % n // -1 mod n
				m.helpWindow = 0
				m.helpCursor = 0
				return m, nil
			case tea.KeyUp:
				m.helpListUp(helpListLen(m.helpTab))
				return m, nil
			case tea.KeyDown:
				m.helpListDown(helpListLen(m.helpTab))
				return m, nil
			case tea.KeyHome:
				if key.Mod.Contains(tea.ModCtrl) {
					m.helpListHome()
				}
				return m, nil
			case tea.KeyEnd:
				if key.Mod.Contains(tea.ModCtrl) {
					m.helpListEnd(helpListLen(m.helpTab))
				}
				return m, nil
			}
			return m, nil
		}
		// Cycle 104: Shift+Tab cycles the agent execution mode. Recovered
		// 1.3.5 changelog: "Choose it with `--mode auto`, from `Agent Mode`
		// in `/config`, or by pressing `Shift+Tab` to cycle modes."
		//
		// Placed after the helpMode block so the overlay keeps owning Tab.
		if key.Code == tea.KeyTab && key.Mod.Contains(tea.ModShift) {
			m.cycleMode()
			return m, nil
		}
		if key.Code == tea.KeyTab && !key.Mod.Contains(tea.ModShift) && m.isBtwCardVisible() && !m.picker.Visible() && !m.atFileDropdown.Visible() {
			m.ToggleBtwExpanded()
			return m, nil
		}
		switch key.Code {
		case tea.KeyUp:
			if m.picker.Visible() {
				m.picker.MoveUp()
				return m, nil
			}
			// feature/at-file-dropdown-newline: the @ file-mention
			// dropdown takes precedence over historyUp when it is
			// visible, the same way the slash picker does.
			if m.atFileDropdown.Visible() {
				m.atFileDropdown.MoveUp()
				return m, nil
			}
			if m.isBtwCardVisible() && m.input.Value() == "" {
				if m.BtwScrollBy(-1) {
					return m, nil
				}
			}
			// Cycle 283: "Press up to edit queued messages" — Up on an
			// empty buffer pulls the newest queued prompt back into the
			// input and drops it from the pending queue. The reference
			// advertises this in the status row while the queue is
			// non-empty.
			if len(m.streamQueue) > 0 && m.input.Value() == "" {
				last := len(m.streamQueue) - 1
				m.input.SetValue(m.streamQueue[last])
				m.streamQueue = m.streamQueue[:last]
				m.modeHintDismissed = true
				return m, nil
			}
			// Cycle multiline-cursor-history-boundary: history
			// recall on Up is a boundary action — only fires when
			// the cursor is on the first visual line of the input
			// box. On any other line of a multi-line prompt, fall
			// through to m.input.Update below so the textarea's
			// built-in LinePrevious binding moves the cursor up
			// one logical line. The boundary predicate is
			// isOnFirstLine; its unit-tested, value/cursor-based
			// form lives in tui_cursor_boundary.go.
			if isOnFirstLine(m.input.Value(), inputCursorByteOffset(m.input)) {
				m.historyUp()
				return m, nil
			}
		case tea.KeyDown:
			if m.picker.Visible() {
				m.picker.MoveDown()
				return m, nil
			}
			if m.atFileDropdown.Visible() {
				m.atFileDropdown.MoveDown()
				return m, nil
			}
			if m.isBtwCardVisible() && m.input.Value() == "" {
				if m.BtwScrollBy(1) {
					return m, nil
				}
			}
			// Cycle multiline-cursor-history-boundary: history
			// recall on Down only fires when the cursor is on the
			// last visual line. The boundary predicate is
			// isOnLastLine; its unit-tested, value/cursor-based
			// form lives in tui_cursor_boundary.go.
			if isOnLastLine(m.input.Value(), inputCursorByteOffset(m.input)) {
				m.historyDown()
				return m, nil
			}
		}
		// Cycle 244: Enter with queueMode == true inserts a newline rather
		// than submitting — the user's prompt is then "queued" and is
		// submitted on the next Cmd+Enter, or auto-flushed when the
		// in-flight turn completes (see turnDoneMsg below). When
		// queueMode is off we keep the cycle-71 path verbatim: Enter
		// submits through submitPrompt. The choice between newline
		// and submit lives ONLY in this branch so all the picker /
		// @ file / resume / help overlays below behave unchanged
		// (they already take precedence over Enter when their
		// overlays are open).
		//
		// Cycle 253: when the pinned-prompt header is visible (the
		// user scrolled into a turn) AND the input buffer is empty,
		// Enter jumps the viewport back to that prompt instead of
		// submitting. With text in the buffer the user is composing
		// a new prompt and Enter falls through to the existing
		// submit path — the pinned prompt does not steal the
		// submit key from a typed message. (The cycle-179 sticky
		// bar has no interactive behaviour; the cycle-253 pinned
		// header adds click + Enter without breaking submit.)
		if key.Code == tea.KeyEnter && key.Mod == 0 && m.altScreen &&
			m.pinnedPromptText != "" && m.input.Value() == "" {
			if m.jumpToPinnedPrompt() {
				return m, nil
			}
		}
		if key.Code == tea.KeyEnter && key.Mod == 0 && m.isBtwCardVisible() && m.input.Value() == "" {
			m.DismissBtwResponse()
			return m, nil
		}
		if key.Code == tea.KeyEnter && key.Mod == 0 {
			// Cycle 288: \ + enter is the newline fallback. When cursor is preceded
			// by a backslash '\', delete the '\' and insert a newline instead of submitting.
			// Matches AGY 1.4.0 shortcuts: "\\ + enter Insert newline fallback".
			offset := inputCursorByteOffset(m.input)
			val := m.input.Value()
			if offset > 0 && offset <= len(val) && val[offset-1] == '\\' {
				m.input, _ = m.input.Update(tea.KeyPressMsg{Code: tea.KeyBackspace})
				m.input.InsertRune('\n')
				m.modeHintDismissed = true
				m.picker.Update([]rune(firstLine(m.input.Value())))
				m.atFileDropdown.Update(m.input.Value(), m.cwd)
				return m, nil
			}
			// 1.4.3: Enter on the @ file-mention picker selects the
			// highlighted row (inserting "@path" and closing the
			// overlay) instead of submitting the prompt. This
			// applies in queue mode too — the picker owns Enter.
			if m.atFileDropdown.Visible() {
				if m.tabSpliceAtFile(false) {
					m.historyIdx = 0
					m.historyEdit = ""
				}
				return m, nil
			}
			if m.queueMode {
				// Cycle 244: in queue mode, Enter is a newline
				// inside the textarea. The bubbles textarea
				// already binds enter → InsertNewline so the
				// simplest path is to delegate to it via
				// InsertRune('\n'); the equivalent to typing
				// the newline rune at the cursor position.
				m.input.InsertRune('\n')
				m.picker.Update([]rune(firstLine(m.input.Value())))
				m.atFileDropdown.Update(m.input.Value(), m.cwd)
				return m, nil
			}
			return m, m.submitPrompt()
		}
		// Cycle 288: Alt+Enter and Shift+Enter insert a newline in the textarea.
		// Matches AGY 1.4.0 shortcuts: "alt+enter, ctrl+j, shift+enter Insert newline".
		if key.Code == tea.KeyEnter && (key.Mod.Contains(tea.ModAlt) || key.Mod.Contains(tea.ModShift)) {
			m.input.InsertRune('\n')
			m.modeHintDismissed = true
			m.picker.Update([]rune(firstLine(m.input.Value())))
			m.atFileDropdown.Update(m.input.Value(), m.cwd)
			return m, nil
		}
		// Cycle 244: Cmd+Enter (and Ctrl+Enter) flush the queued
		// prompt OR submit the textarea content even when queueMode
		// is off (behaving as a second Enter on a multi-line buffer
		// when queueMode==false so the clone stays predictable).
		// The submit path saves the textarea content into
		// m.queuedPrompt (when queueMode is on) OR clears the input
		// and dispatches the turn (when queueMode is off).
		if key.Code == tea.KeyEnter && key.Mod.Contains(tea.ModCtrl) {
			return m, m.submitOrQueuePrompt()
		}
		if key.Code == 'j' && key.Mod.Contains(tea.ModCtrl) {
			m.input.InsertRune('\n')
			m.picker.Update([]rune(firstLine(m.input.Value())))
			// feature/at-file-dropdown-newline: re-derive the @ file
			// dropdown from the post-newline buffer so a fresh
			// "@<char>" at the start of a new line shows the
			// dropdown just like at the end of a line.
			m.atFileDropdown.Update(m.input.Value(), m.cwd)
			return m, nil
		}
		// Cycle 225: AGY 1.2.16 changelog — "Added word-wise cursor
		// movement in the prompt editor: Alt/Option+Left and
		// Alt/Option+Right move the cursor to the previous/next
		// word boundary, matching macOS text-field conventions."
		// Wired here (between Ctrl+J and the textarea default Update)
		// so the input is guaranteed regardless of how the terminal
		// encodes the chord (bubbles' WordForward/WordBackward
		// keymap is string-equality on Key.String() and silently
		// no-ops for the readline-style ESC b/f encoding). The
		// helpers in wordmove.go implement the macOS NSText
		// semantics (skipWhitespace then skipNonWhitespace, in the
		// requested direction). Placed after the vim-mode block so
		// vim users keep their single-character semantics on the
		// unmodified Alt modifier (vim Insert users get word
		// movement via b / w).
		switch key.Code {
		case tea.KeyLeft:
			if key.Mod.Contains(tea.ModAlt) && key.Mod&(tea.ModCtrl|tea.ModShift) == 0 {
				m.moveWordLeft()
				m.picker.Update([]rune(firstLine(m.input.Value())))
				return m, nil
			}
			if m.isBtwCardVisible() && m.input.Value() == "" && len(m.btwCards) > 1 {
				m.NavigateBtw(-1)
				return m, nil
			}
		case tea.KeyRight:
			if key.Mod.Contains(tea.ModAlt) && key.Mod&(tea.ModCtrl|tea.ModShift) == 0 {
				m.moveWordRight()
				m.picker.Update([]rune(firstLine(m.input.Value())))
				return m, nil
			}
			if m.isBtwCardVisible() && m.input.Value() == "" && len(m.btwCards) > 1 {
				m.NavigateBtw(1)
				return m, nil
			}
		}
		// Cycle 278: ctrl+v reads the OS clipboard and inserts it. This
		// must run before the textarea sees the key: bubbles' default
		// keymap binds ctrl+v to its own Paste command, which returns an
		// unexported textarea.pasteMsg that the catch-all below discards —
		// i.e. it read the clipboard and threw the result away. Placed
		// after the vim block so vim's own ctrl+v still wins when vim
		// mode owns the key.
		if key.Code == 'v' && key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) {
			return m, m.insertPastedText(clipboardReadText())
		}

		// Cmd+A (or Ctrl+Shift+A): select all input and auto-copy to clipboard.
		if (key.Code == 'a' || key.Code == 'A') && (key.Mod.Contains(tea.ModSuper) || key.Mod.Contains(tea.ModMeta) || (key.Mod.Contains(tea.ModCtrl) && key.Mod.Contains(tea.ModShift))) {
			if cmd, ok := m.selectAllInput(); ok {
				return m, cmd
			}
			return m, nil
		}

		// Cmd+C: copy input selection if any.
		if (key.Code == 'c' || key.Code == 'C') && key.Mod.Contains(tea.ModSuper) {
			if m.hasInputSelection() {
				start, end := m.inputSelectionRange()
				val := m.input.Value()
				if start < len(val) && end <= len(val) {
					text := val[start:end]
					_ = copyToClipboard(text)
					return m, m.copySelectionCmd(text)
				}
			}
			return m, nil
		}

		// Backspace or Delete with active input selection deletes the selection.
		if (key.Code == tea.KeyBackspace || key.Code == tea.KeyDelete) && m.hasInputSelection() {
			m.deleteInputSelection()
			m.modeHintDismissed = true
			m.picker.Update([]rune(firstLine(m.input.Value())))
			m.clearEligibilityNotice()
			m.atFileDropdown.Update(m.input.Value(), m.cwd)
			return m, nil
		}

		// Left / Right arrow with active input selection clears selection and positions cursor.
		if (key.Code == tea.KeyLeft || key.Code == tea.KeyRight) && m.hasInputSelection() && !key.Mod.Contains(tea.ModShift) {
			start, end := m.inputSelectionRange()
			m.clearInputSelection()
			if key.Code == tea.KeyLeft {
				m.setInputCursorOffset(start)
			} else {
				m.setInputCursorOffset(end)
			}
			return m, nil
		}

		// Typing printable text with active input selection replaces the selection.
		if m.hasInputSelection() && key.Text != "" && !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) && !key.Mod.Contains(tea.ModSuper) {
			m.replaceInputSelection(key.Text)
			m.modeHintDismissed = true
			m.picker.Update([]rune(firstLine(m.input.Value())))
			m.clearEligibilityNotice()
			m.atFileDropdown.Update(m.input.Value(), m.cwd)
			return m, nil
		}
		var cmd tea.Cmd
		m.input, cmd = m.input.Update(msg)
		// Cycle 283: any keystroke that reaches the textarea is the
		// reference's "user is editing" signal — the mode placeholder
		// goes away and stays away until the next Shift+Tab.
		m.modeHintDismissed = true
		m.picker.Update([]rune(firstLine(m.input.Value())))
		// Cycle 262: the keystroke reached the input — the user
		// has started typing. Clear the eligibility-error inline
		// notice so it does not stay on screen as the user
		// composes their next prompt. The clear is idempotent
		// (an empty notice is a free no-op) and only fires on
		// keystrokes that reach the textarea branch — modifier-
		// only events (bare Shift, etc.) are filtered upstream
		// by bubbletea and never reach Update() with a non-empty
		// KeyMsg, so this branch is exactly the "user starts
		// typing" signal cycle 262's spec calls out. Subagent /
		// overlay key handlers (list-panel, ctrl+k, etc.) early-
		// return before this branch and so do NOT clear the
		// notice — a deliberate design choice, so a subagent
		// picker keystroke does not hide the eligibility row
		// the user might still want to act on.
		m.clearEligibilityNotice()
		// feature/at-file-dropdown-newline: ditto on every
		// keystroke that mutates the textarea; the parser walks
		// the trailing run anywhere in the (possibly multi-line)
		// buffer.
		m.atFileDropdown.Update(m.input.Value(), m.cwd)
		return m, cmd
	case turnDoneMsg:
		// Cycle 292: a mid-stream local command (/clear, /new, /fork,
		// /branch) already cancelled this turn and owns the transcript
		// now. Finalising it here would append the reply the user threw
		// away — or, after /clear, resurrect a conversation that was
		// just emptied. Reset the live anchors and stop.
		m.inputNotice = ""
		if m.discardTurn {
			m.discardTurn = false
			m.liveSteps = nil
			m.liveStepRuns = nil
			m.activeTool = ""
			m.streaming = false
			m.thinking = false
			m.replyStart = -1
			m.liveReplyIdx = -1
			m.liveTextLines = 0
			m.liveReplyBuf = ""
			m.streamedText = false
			m.liveBatch = 0
			m.batchBreak = false
			return m, tea.ClearScreen
		}
		// Cycle 244: when the user queued a follow-up prompt via
		// Cmd+Enter (m.queuedPrompt), flush it as the next user
		// message as soon as the in-flight turn completes. The
		// dispatch returns a tea.Cmd the case batches alongside the
		// turn-finalisation output (no extra return statement — Go
		// switch cases share the case block). The captured cmd
		// becomes the queue flush's streamPrompt + spinner tick
		// batch; the rest of the turnDoneMsg handler continues to
		// finalise the just-finished turn as usual.
		// Cycle 283: pick the next queued prompt up front but do not
		// touch the transcript yet — the current turn's block and reply
		// are finalised below by replaceRange(liveStart, len), which
		// would eat a row appended here. The flush at the end of the
		// case re-anchors liveStart against the finalised transcript.
		nextQueued := ""
		if m.queuedPrompt != "" {
			nextQueued = m.queuedPrompt
			m.queuedPrompt = ""
		} else if len(m.streamQueue) > 0 {
			nextQueued = m.streamQueue[0]
			m.streamQueue = m.streamQueue[1:]
		}
		var queueCmd tea.Cmd
		// Cycle 99: finalize the turn's tool-step block, then append the
		// reply. Cycle 285 rewrote this: the live turns now finalise
		// each batch *in place*, leaving the streamed prose rows where
		// they were painted. The reference interleaves prose → block →
		// prose; the pre-285 single-range replace moved every step above
		// the whole reply and glued the messages together. The
		// block+reply replace below is kept as the headless fallback
		// (no live step runs, e.g. a test driving turnDoneMsg directly).
		if finalised := m.finaliseLiveTurn(msg); !finalised {
			var replyLines []string
			if !agent.IsEmptyAssistantTurn(msg.reply, nil, msg.reasoning) {
				styled := renderMarkdown(msg.reply, m.width)
				replyLines = strings.Split(styled, "\n")
			}
			block := toolBlockLines(msg.steps, m.expanded, m.width)
			final := make([]string, 0, len(block)+len(replyLines)+1)
			pad := m.liveStart > 0 && strings.TrimSpace(stripANSI(m.response[m.liveStart-1])) != ""
			if pad {
				final = append(final, "")
			}
			final = append(final, block...)
			final = append(final, replyLines...)
			m.replaceRange(m.liveStart, len(m.response), final)
			if len(msg.steps) > 0 {
				start := m.liveStart
				if pad {
					start++
				}
				m.blocks = append(m.blocks, turnBlock{
					start: start,
					lines: len(block),
					steps: msg.steps,
				})
			}
		} else if !m.streamedText && !agent.IsEmptyAssistantTurn(msg.reply, nil, msg.reasoning) {
			// The steps painted live but no delta ever reached the view
			// (a scripted/mock stream). The reply has to be appended by
			// hand, below the finalised blocks.
			styled := renderMarkdown(msg.reply, m.width)
			m.response = append(m.response, strings.Split(styled, "\n")...)
		}
		m.liveSteps = nil
		m.liveStepRuns = nil
		m.activeTool = ""
		m.streaming = false
		m.thinking = false
		m.replyStart = -1
		m.liveTextLines = 0
		m.liveReplyBuf = ""
		m.streamedText = false
		m.liveBatch = 0
		m.batchBreak = false
		// Cycle osc-7501: a turnDoneMsg represents the model finishing
		// the user-facing work. Record the success outcome so the OSC
		// 7501 status reads "Done" until the next prompt is submitted;
		// clearLastTurnOutcome is called from the prompt-submit path.
		// A future cycle will route chunk.Error / ErrAutoContMaxDepth
		// through a turnErrorMsg and call recordTurnOutcome(Error)
		// there; today the TUI's chunk handler swallows that path,
		// so the Error surface stays at "zero matches the
		// not-yet-tested scenario", and Done is the only steady-state
		// outcome the cycle ships.
		m.recordTurnOutcome(tea.ProgramStateDone)
		// Cycle stall-empty-step-cleanup: a cancelled turn whose reply
		// was empty, produced no tool calls, and carried no reasoning
		// must not be recorded as an assistant step. The previous gate
		// was `if msg.reply != ""`, which leaked an empty step whenever
		// a turn aborted before its first byte AND failed to catch the
		// rarer case where the model produced only thinking with no
		// follow-up text. agent.IsEmptyAssistantTurn is the unit-tested
		// classifier; the AGY 1.3.6 changelog row "a stalled request
		// no longer leaves an empty response step behind" is the spec
		// the gate implements.
		//
		// Tool calls are tracked separately on m.blocks (see the
		// turnBlock append above) — a tool-call-only turn does NOT add
		// to m.session because the recovered wire shape carries the
		// tool calls in a StepToolCall row of the conversation
		// database, not a StepModelResponse. The classifier still
		// confirms non-emptiness for the side effect of skipping the
		// response-line append when the reply is "".
		if !agent.IsEmptyAssistantTurn(msg.reply, nil, msg.reasoning) {
			// Cycle 228: the reply is rendered through the same
			// glamour-backed pipeline the conversation-loading path uses
			// (renderMarkdown); cycle 284 hoists it above the block wipe so
			// the whole live range is finalised in one replace — see the
			// comment there for why the old two-step wipe dropped steps.
			//
			// Cycle 229: if backendChunkMsg already extended an in-place
			// assistant session message this turn (liveReplyIdx >= 0),
			// overwrite it with the authoritative msg.reply now that
			// the turn is done. Otherwise (headless tests driving
			// turnDoneMsg directly, or a subagent reply path that
			// bypassed backendChunkMsg) append a fresh entry.
			if m.liveReplyIdx >= 0 && m.liveReplyIdx < len(m.session) {
				m.session[m.liveReplyIdx].Content = msg.reply
			} else {
				m.session = append(m.session, backend.Message{Role: "assistant", Content: msg.reply})
			}
		}
		// Cycle 279: a failed turn always leaves a visible row.
		//
		// Before this, every failure path through streamPrompt ended here
		// with the error dropped, and the replaceRange above had already
		// wiped whatever the chunk handler streamed into m.response. The
		// transcript therefore showed the tool block and nothing else:
		// "it suddenly can't continue and prints nothing". The row is
		// appended AFTER the reply range is finalised so nothing replaces
		// it, and it is the same `Error: …` shape the rest of the TUI
		// uses for user-facing failures.
		if msg.err != nil {
			m.response = append(m.response, turnErrorRow(msg.err))
		}
		m.replyStart = -1
		m.liveReplyIdx = -1
		m.saveSession()
		if len(m.response) > 0 {
			m.response = append(m.response, "")
		}
		// Cycle 267: streaming-chunks-clear — return tea.ClearScreen
		// alongside queueCmd so the next render does a full repaint of
		// the screen. Without this, charm.land/bubbletea/v2 + ultraviolet's
		// diff-based renderer leaves stale pixels at rows that had live
		// streaming content (chunks written at specific cursor positions)
		// but no longer have content in the new View() output — UV's
		// transformLine skips rows where newbuf.Touched[i] is nil, so an
		// announcement-card transition (Card 1 → Card 2) after a streamed
		// turn leaves the old Card 1 row 1 / 2 visible behind the new
		// response and the "  Generating…" spinner never clears (it lived
		// at a row the new View() no longer writes to). tea.ClearScreen
		// sets renderer.s.clear = true, which routes the next flush
		// through clearUpdate / s.scr.Erase() — a full-screen redraw
		// driven by the freshly returned View() so the rendered grid
		// matches the recovered layout exactly. The 1-frame (~16 ms at
		// 60 fps) latency is imperceptible; aggressive per-chunk clears
		// would cause flicker.
		//
		// This is the upstream-documented purpose of ClearScreen per
		// charm.land/bubbletea/v2@v2.0.10/screen.go:12-20 ("use a Cmd to
		// signal the screen-should-be-cleared-after-next-update"). It is
		// not "regular redraws", but a known recovery path for layout
		// transitions that out-of-band writes (chunk-by-cursor writes) put
		// outside the diff model.
		//
		// Cycle 244: when m.queuedPrompt was captured by the
		// queue-mode Cmd+Enter path, queueCmd is the
		// streamPrompt-batch for the next user turn. Returning it
		// here bubbles the queued turn out through bubbletea so it
		// can stream concurrently with the just-finalised turn's
		// glamour-replace. When queueMode wasn't on, queueCmd is
		// nil and tea.Batch(nil, ...) collapses to tea.ClearScreen
		// alone via compactCmds.
		// Cycle 283: flush the queued prompt now that the finished
		// turn's rows are final. Both queue flavours echo their user
		// row here; the reference places it after the reply, not
		// mid-stream.
		if nextQueued != "" {
			// Cycle 289: a slash command queued while the previous
			// turn was streaming executes now. Sending it through
			// streamPrompt would replay the literal "/plan" (or
			// "/clear") text to the model, which is what the old
			// queue path did before local commands were dispatched
			// mid-stream.
			if handled, cmd := m.dispatchSlashLine(nextQueued); handled {
				queueCmd = cmd
			} else {
				m.response = append(m.response, userPromptRow(nextQueued))
				m.liveStart = len(m.response)
				m.replyStart = -1
				m.liveReplyIdx = -1
				m.liveSteps = nil
				m.liveStepRuns = nil
				m.activeTool = ""
				m.thinking = false
				m.liveTextLines = 0
				m.liveReplyBuf = ""
				m.streamedText = false
				m.liveBatch = 0
				m.batchBreak = false
				m.anyToolRun = false
				m.spinFrame = 0
				m.appendUserTurn(nextQueued)
				m.saveSession()
				m.streaming = true
				m.clearLastTurnOutcome()
				tickCmd := func() tea.Msg { return m.spinner.Tick() }
				queueCmd = tea.Batch(m.streamPrompt(nextQueued), tickCmd)
			}
		}
		return m, tea.Batch(queueCmd, tea.ClearScreen)
	case toolRunningMsg:
		// The reference paints the in-flight call in the transcript
		// ("● Running (…) (ctrl+o to expand)") and names the activity on
		// the spinner line ("Running command..."). Both were verified
		// against agy 1.4.3; the previous clone named the tool on the
		// spinner and showed no row at all until the call returned.
		m.activeTool = runningSpinnerText(msg.step.name)
		m.anyToolRun = true
		m.streaming = true
		// A call after streamed prose starts a new batch/block.
		if m.batchBreak {
			m.liveBatch++
			m.batchBreak = false
		}
		// The reference separates a block from the prose above it with
		// one blank row (verified against 1.4.3).
		m.blankBefore()
		step := msg.step
		step.batch = m.liveBatch
		lines := toolStepLines(step, m.expanded, m.width)
		m.liveStepRuns = append(m.liveStepRuns, liveStepRun{
			start: len(m.response),
			lines: len(lines),
			step:  step,
		})
		m.response = append(m.response, lines...)
	case thoughtStartMsg:
		// The model entered its thinking block. Show the recovered live
		// frame ("Thinking... (Ns)") until the turn finalises. Guarded so
		// a re-entry (e.g. a second think block in the same turn) keeps
		// the original start time and does not double-insert the frame.
		if !m.thinking {
			m.thinking = true
			m.thinkingSince = time.Now()
		}
		m.streaming = true
	case autoContBannerMsg:
		// Cycle auto-continue-interrupt: a stream-interrupt or iteration-cap
		// is being replayed. The Options.AutoContBanner callback (see
		// streamPrompt / agent.Options) emits this synchronously before each
		// retry; we render it as a transient "↻ auto-continuing…" row above
		// the rest of the recovered turn. Pushed into m.response like a
		// slash-command echo so the user can see it inline with the tool
		// steps; the next backendChunk / backendDone that finalises the
		// turn leaves the row right above the recovered reply.
		banner := "↻  auto-continuing… (retry " + fmt.Sprintf("%d", msg.depth) + " of " + fmt.Sprintf("%d", msg.maxDepth) + ")"
		m.response = append(m.response, banner)
		if m.streaming {
			m.streaming = false
		}
	case eligibilityNoticeMsg:
		// Cycle 262: an eligibility error reached the TUI. The chunk
		// handler in streamPrompt forwarded chunk.Error via prog.Send
		// because the error carried an *EligibilityError (AsEligibilityError
		// walked the wrapped chain). We store the notice on the runMode
		// so viewBody's eligibilityNoticeRow renders it above the
		// prompt input on the next render — NEVER append to m.response
		// or m.session (cycle 262's hard constraint). The diagnostic
		// line is emitted by setEligibilityNotice itself (stays out of
		// the conversation transcript; surfaces only on stderr where an
		// operator can tail it).
		//
		// When the same chunk also signals Done (chunk.Done && !done),
		// the chunk handler returns backendDoneMsg right after this
		// message — Update() will see the Done case in a subsequent
		// invocation and finalise the turn as usual. The order matters:
		// the eligibility message arrives first so the user sees the
		// inline notice before the streaming flag flips off.
		m.setEligibilityNotice(msg.Reason, msg.VerificationURL)
	case toolStepMsg:
		// Render the finished step immediately so the transcript shows
		// progress while the turn is still running; turnDoneMsg re-renders
		// the whole block (adding the group header when there are >=2).
		// The run is recorded so ctrl+o can restyle these rows before the
		// turn finalises (rerenderLiveSteps).
		//
		// Cycle 285: when a provisional "Running (…)" row is still on
		// screen, this call finished it — rewrite that row in place
		// instead of appending a second one, exactly as 1.4.3 does.
		m.liveSteps = append(m.liveSteps, msg.step)
		m.activeTool = ""
		// Close the current assistant message's text run: any prose that
		// streams next belongs below this block.
		m.replyStart = -1
		m.liveTextLines = 0
		m.liveReplyBuf = ""
		step := msg.step
		lines := toolStepLines(step, m.expanded, m.width)
		replaced := false
		if n := len(m.liveStepRuns); n > 0 && m.liveStepRuns[n-1].step.running {
			r := &m.liveStepRuns[n-1]
			oldEnd := r.start + r.lines
			if r.start >= 0 && oldEnd <= len(m.response) {
				step.batch = r.step.batch
				lines = toolStepLines(step, m.expanded, m.width)
				delta := len(lines) - r.lines
				m.replaceRange(r.start, oldEnd, lines)
				r.lines = len(lines)
				r.step = step
				if delta != 0 && m.replyStart >= oldEnd {
					m.replyStart += delta
				}
				replaced = true
			}
		}
		if !replaced {
			if m.batchBreak {
				m.liveBatch++
				m.batchBreak = false
			}
			step.batch = m.liveBatch
			lines = toolStepLines(step, m.expanded, m.width)
			m.liveStepRuns = append(m.liveStepRuns, liveStepRun{
				start: len(m.response),
				lines: len(lines),
				step:  step,
			})
			m.response = append(m.response, lines...)
		}
		m.liveSteps[len(m.liveSteps)-1] = step
	case backendChunkMsg:
		// Cycle 229: live model-reply streaming. Each backendChunkMsg
		// carries one sanitised delta from the agent loop's chunk
		// handler. The first chunk of a turn anchors replyStart so the
		// turnDoneMsg handler can replace [replyStart, len(m.response))
		// with the glamour-rendered msg.reply without double-rendering.
		//
		// Pre-cycle 229 this handler was a stub that appended each
		// chunk as a fresh line, called saveSession per chunk (very
		// expensive), and added a trailing blank line per chunk — all
		// artefacts of the cycle-99 single-shot design that never
		// anticipated multi-chunk live streaming.
		//
		// Cycle 230: line discipline. The cycle-229 implementation
		// appended each chunk as a fresh line in m.response, so a
		// model that streams token-sized deltas (no '\n' between them)
		// produced one transcript row per delta — the user saw
		// "H", "He", "Hel", "Hell", "Hello" while the model typed. The
		// fix is the same discipline crush's
		// `internal/ui/chat/streaming_markdown.go` uses in spirit
		// (crush caches a glamour-rendered stable prefix per stream and
		// only re-renders the trailing partial — too invasive for the
		// wagyu one-shot glamour pass, so we adopt the lightweight
		// half here): split chunk on '\n', then
		//
		//   * the first segment extends the current last row in the
		//     live-stream range (no new line added for a mid-stream
		//     token boundary);
		//   * each subsequent segment becomes a fresh row.
		//
		// This keeps words appearing on a single growing line at the
		// bottom of the transcript while honouring '\n' as a paragraph
		// break. The turnDoneMsg glamour-replace keeps working
		// unchanged because the live range is still
		// [replyStart, len(m.response)) — it just spans N rows now
		// instead of strictly 1, and the replace nukes them all.
		chunk := string(msg)
		if chunk == "" {
			break
		}
		// Cycle 292: a local command cancelled this turn (/clear, /new,
		// /fork). Whatever the provider still delivers belongs to a turn
		// the user threw away, so it must not repaint the transcript.
		if m.discardTurn {
			break
		}
		// Cycle 285: each assistant message is its own text run. A tool
		// step closes the run (replyStart = -1), so prose after a call
		// starts a fresh range below the block instead of replacing the
		// rows above it — the pre-285 single range meant the next chunk
		// wiped the steps that had been painted in between.
		if m.replyStart < 0 {
			m.blankBefore()
			m.replyStart = len(m.response)
			m.liveTextLines = 0
			m.liveReplyBuf = ""
		}
		m.liveReplyBuf += chunk
		m.streamedText = true
		m.batchBreak = true
		// Cycle 229: extend or append the in-progress assistant
		// message in m.session. The first chunk of a turn appends a
		// fresh entry (TestTUISessionPersistence / Test_SessionAccu-
		// mulatesTurns pin this: after a chunk, m.session is the
		// user message + the chunk's assistant message). Subsequent
		// chunks update that entry in place so 500 deltas still
		// produce one assistant session message per turn — cycle-99
		// appended N entries and broke replay semantics.
		if m.liveReplyIdx < 0 {
			m.session = append(m.session, backend.Message{Role: "assistant", Content: chunk})
			m.liveReplyIdx = len(m.session) - 1
		} else {
			m.session[m.liveReplyIdx].Content += chunk
		}
		// Cycle 272: render the live range from the RAW accumulated
		// reply, never from the already-styled rows in m.response.
		//
		// Root cause of the pre-272 mid-stream artefact: the cycle-232 /
		// cycle-242 pass built its "running buffer" by joining
		// m.response[replyStart:], but those rows are the *output* of
		// the previous pass — glamour-padded and SGR-wrapped. At every
		// blank-line boundary the pass fed that styled text back through
		// glamour, which counted the trailing pad as content,
		// word-wrapped it into extra rows and re-padded; the live
		// transcript roughly doubled in row count at each boundary
		// (observed: a 22-line reply inflated to 216 rows) before the
		// turn-end replace collapsed it back. AGY renders the
		// accumulated markdown once and shows the result, so the fix is
		// to do the same: keep the raw reply text (the in-progress
		// session message, appended above), render it with the
		// turn-end renderer, and swap the whole live range in.
		//
		// renderMarkdown is the exact function turnDoneMsg calls, so the
		// live frame is a prefix of the final frame: a heading chip, list
		// bullet, blockquote glyph, table rule and fenced chroma all
		// appear mid-stream in the same shape they keep at turn-end, and
		// cross-line constructs are interpreted by goldmark in one pass
		// over the whole buffer (an unclosed **bold** shows literally
		// until it closes, then snaps to bold — the "render the
		// accumulated buffer" behaviour the reference renderer has).
		styled := renderMarkdown(m.liveReplyBuf, m.width)
		lines := strings.Split(styled, "\n")
		m.replaceRange(m.replyStart, m.replyStart+m.liveTextLines, lines)
		m.liveTextLines = len(lines)
		m.streaming = true
		// Save once per chunk so tests that drive only backendChunkMsg
		// (no backendDoneMsg / turnDoneMsg follows) still see the
		// assistant step land on disk. In production this is the
		// measurable cost of cycle 229; a 500-delta turn does 500
		// disk writes. Accepted: the disk hit is small relative to
		// the model stream latency, and a follow-up cycle can debounce
		// (e.g. save every N chunks + at backendDoneMsg) without
		// changing the on-disk schema.
		m.saveSession()
	case planTierMsg:
		if msg.tier != "" {
			m.tier = msg.tier
		}
		return m, nil
	case backendDoneMsg:
		m.replyStart = -1
		m.liveReplyIdx = -1
		m.streaming = false
		m.thinking = false
		m.saveSession()
		if len(m.response) > 0 {
			m.response = append(m.response, "")
		}
		// Cycle 267: mirror the streaming-chunks-clear trigger the
		// turnDoneMsg handler emits. backendDoneMsg is the cmd
		// result for the empty-reply / tool-call-only / err-path
		// outcomes — the diff renderer still leaves the
		// "  Generating…" row visible on those because the spinner
		// row had streaming-only content the new View() never
		// re-writes, and UV's transformLine skips untouched rows.
		// Same one-frame, full-redraw recovery the turn-end path
		// uses (see the long comment at the case turnDoneMsg: site).
		return m, tea.ClearScreen
	case compactDoneMsg:
		// Cycle 229: the manual /compact slash command resolves
		// into one of three transcript rows: the running echo
		// (already emitted by startManualCompact), the
		// already-running echo, the failure echo, or the success
		// echo with tokens-before/after. We append a fresh row
		// each time so a /compact press mid-stream does not
		// collide with the cycle-227 auto-compact status row,
		// and so ctrl+o re-render keeps the compact feedback in
		// its original place.
		//
		// The success row is the same colour/style as the
		// running echo (dim gray `⎿  Compacted: …`) so the two
		// rows look like one bracketed event. Tokens are
		// raw-token counts (the cycle-227 status row uses
		// 184 K → 42 K), because the manual /compact transcript
		// rows are emitted once and never re-rendered against a
		// different width.
		//
		// When msg.TokensBefore == 0 AND msg.TokensAfter == 0
		// (an aborted compact or one PiG fired without token
		// counters), we still emit a row — the "(no token
		// counts)" suffix is the explicit "0 → 0" tail the
		// cycle-229 spec calls for, so the user always sees
		// that a compact finished.
		m.appendCompactResult(msg)
		if msg.CustomInstructions != "" && msg.Err == nil && !msg.Aborted {
			return m, m.streamPrompt(msg.CustomInstructions)
		}
		return m, nil
	case genericCompactDoneMsg:
		cmd := m.applyGenericCompactResult(msg, msg.CustomInstructions)
		return m, cmd
	case sp.TickMsg:
		// Cycle 76: route spinner ticks when streaming. Stop
		// ticking once streaming flips off so the spinner
		// freezes on its last frame.
		if m.streaming {
			var cmd tea.Cmd
			m.spinner, cmd = m.spinner.Update(msg)
			m.spinFrame++
			return m, cmd
		}
	case tasksTickMsg:
		// Cycle tasks-panel: the subscriber goroutine that
		// watches the registry sets m.tasksDirty = true on every
		// event; we re-emit a Tick so the View() redraws while
		// the panel is open OR while the status row's "· N
		// tasks" segment needs to refresh. The 100 ms cadence
		// is the cost of the live update — it would be cheaper
		// to use program.Send(), but the bubbletea v2 API does
		// not expose that from inside an Update handler.
		if m.consumeTasksDirty() {
			return m, armTasksTick()
		}
		return m, nil
	case statusLineExternalTickMsg:
		// Cycle status-line-script: the 100 ms tick the external
		// status-line path owns. The handler bumps the tick
		// counter (so a stuck ticker still gets drained) and
		// arms the next tick — the actual subprocess spawn is
		// done in a goroutine spawned by the tick handler so the
		// bubbletea runtime cannot deadlock on a slow script.
		// When external mode is not active (no command configured,
		// built-in row still in use, or /statusline turned it off)
		// the goroutine is a no-op so the tick is free.
		m.ensureExternalState().markTick()
		go m.runExternalStatusLineTick()
		return m, armExternalStatusLineTick()
	case quotaSummaryMsg:
		if m.panel.kind != panelUsage {
			return m, nil
		}
		if msg.summary != nil {
			m.panel.quota.summary = msg.summary
			m.panel.quota.loadedAt = time.Now()
			m.panel.quota.state = quotaLoaded
		} else if msg.err != nil {
			m.panel.quota.fetchErr = msg.err.Error()
		}
		return m, nil
	case usageQuotaTickMsg:
		// Cycle 225: only route ticks when the open panel is the
		// /usage one. m.quotaFrame keeps the spinner in step, and
		// after usageQuotaLoadingTicks the state flips to
		// quotaLoaded via quotaStubRefills so the live doc
		// replaces the loading doc on the next render. Return the
		// next tick so the chain continues; the panel-closed case
		// returns nil so the tick dies.
		if m.panel.kind != panelUsage {
			return m, nil
		}
		m.quotaFrame++
		if m.panel.quota.state == quotaLoading && m.quotaFrame >= usageQuotaLoadingTicks {
			quotaStubRefills(m)
		}
		return m, usageQuotaTick()
	case compactStartMsg:
		// Cycle 227: auto-compact is starting. Stamp the wall
		// clock, reset the spinner frame, and arm the per-100 ms
		// tick. The status row reads these between renders and
		// shows "Compacting context… 184 K → 42 K" while the run
		// is in flight; the matching compactEndMsg clears the
		// state and stops the tick.
		m.compactStart = msg.StartedAt
		m.compactEnd = time.Time{}
		m.compactSpinnerFrame = 0
		m.compactReason = msg.Reason
		m.compactTokensBefore = 0
		m.compactTokensAfter = 0
		return m, compactSpinnerTick()
	case compactEndMsg:
		// Cycle 227: stamp end time + tokens, clear the spinner
		// arm by NOT returning a tick. The next render reads
		// compactStart.IsZero() — false — and compactEnd — now
		// non-zero — to decide which row to emit (active vs
		// ended). Status-row clear happens after one further
		// render, when compactSpinnerTickMsg doesn't re-arm.
		m.compactEnd = msg.EndedAt
		m.compactReason = msg.Reason
		m.compactTokensBefore = msg.TokensBefore
		m.compactTokensAfter = msg.EstimatedTokensAfter
		return m, nil
	case compactSpinnerTickMsg:
		// Cycle 227: advance the spinner frame while compact
		// is active, then re-arm. When the end event arrived
		// compactStart has been cleared conceptually (end
		// non-zero), so the tick stops on its own. The frame
		// set is the same sp.Dot cycle cycle 225 reuses for
		// /usage (8 frames).
		if m.compactStart.IsZero() || !m.compactEnd.IsZero() {
			return m, nil
		}
		m.compactSpinnerFrame++
		if m.compactSpinnerFrame >= 8 {
			m.compactSpinnerFrame = 0
		}
		return m, compactSpinnerTick()
	case pluginBlinkMsg:
		// Cycle 212: /plugin cursor blink. The search box and the
		// local-install prompt both draw their trailing block cursor
		// only on the "on" phase, and only the /plugin panel owns the
		// tick while it is the active panel.
		if m.panel.kind == panelPlugin &&
			(m.panel.pluginSearch || m.pluginLocalInstallActive()) {
			m.panel.pluginBlinkOn = !m.panel.pluginBlinkOn
			return m, pluginBlink()
		}
		m.panel.pluginBlinkOn = false
		return m, nil
	case csTickMsg:
		// Cycle 228: /codesearch drain tick. Drains every buffered
		// match off the runner channel into m.panel.cs.hits, then
		// re-arms the next tick unless the search has settled. The
		// channel is closed by the runner when State has flipped
		// off StateRunning, so the `closed` return from the drain
		// is the signal to stop arming.
		if m.panel.kind != panelCodeSearch {
			return m, nil
		}
		if m.panel.cs.search == nil {
			return m, nil
		}
		drained, closed := m.drainCodesearchMatches()
		if closed {
			// One final drain after close so any matches that
			// arrived between the previous tick and the close are
			// visible in the final render.
			return m, nil
		}
		if drained {
			// The view will re-read cs.hits on the next paint; no
			// explicit invalidate is needed — bubbletea repaints
			// after every Update.
			return m, csTick(m)
		}
		// Nothing arrived — keep ticking so the spinner advances
		// and we catch the next batch.
		return m, csTick(m)
	}
	return m, nil
}

// firstLine returns the substring of s up to the first newline.
func firstLine(s string) string {
	if i := strings.IndexByte(s, '\n'); i >= 0 {
		return s[:i]
	}
	return s
}

// streamPrompt drives one agent turn through the shared agent loop and feeds
// the result back as turnDoneMsg.
//
// Cycle 99: the loop is what makes the interactive session able to act — it
// advertises m.tools and executes the model's tool calls through m.toolRunner.
// Before this the method issued a single backend.Stream with no `tools` and
// dropped every StreamChunk.ToolCalls, so the model could only talk. The reply
// is passed through the recovered think-block sanitizer (the reference hides
//
//	thinking content; print mode already does this).
func (m *runMode) streamPrompt(prompt string) tea.Cmd {
	be := m.backend
	model := m.model
	if model == "" {
		model = "claude-4.5-sonnet"
	}
	decls := m.tools
	runner := m.toolRunner
	prog := m.prog
	// Cycle 292: the reference sends each user turn inside a
	// <USER_REQUEST>/<ADDITIONAL_METADATA> envelope. The transcript and the
	// conversation store keep the raw text; only the request body carries
	// the envelope, so it is substituted into this per-turn copy.
	msgs := make([]backend.Message, len(m.session))
	copy(msgs, m.session)
	for i := range msgs {
		if msgs[i].Role != "user" {
			continue
		}
		msgs[i].Content = msgs[i].WireText()
		msgs[i].WireContent = ""
	}
	return func() tea.Msg {
		if be == nil {
			return backendDoneMsg{}
		}
		// Cycle 279: the turn is cancellable, NOT time-boxed.
		//
		// Cycle 155 introduced a 60-second context.WithTimeout here, which
		// capped the *whole* agentic turn — every model request plus every
		// tool call, across all tool iterations. A long-horizon task
		// ("read each of these 30 files and append a line for each")
		// routinely needs 90-120 s, so the deadline fired mid-task. The
		// failure was silent: sendPrompt's caller discarded loop.Send's
		// error and turnDoneMsg replaced the streamed range with the tool
		// block, so the user lost the reply AND the reason. Reproduced
		// against MiniMax M3: "run sleep 70" died at 1m0.017s with
		// text=0.
		//
		// What bounds a turn instead:
		//   - the grpc/SSE idle watcher (piagentbridge.StallTimeout = 5 min
		//     with no frame), which is the reference's own rule ("Added a
		//     5-minute stall detection for stuck API requests", AGY 1.3.6);
		//   - the user's Esc (m.turnCancel below);
		//   - the tool-level timeout a tool's own schema carries.
		// The reference has no per-turn wall-clock cap: with the same
		// sleep-70 prompt it waited and answered.
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		// Cycle 235: install the cancel function on the runMode so
		// the Esc handler can cancel this turn out of band. The
		// timeout above still owns the deadline; this extra cancel
		// just lets the user short-circuit a slow stream. Cleared
		// in a second defer so the Esc handler can tell a fresh,
		// cancellable turn apart from a stale streaming flag left
		// behind by a backend that returned nil from Stream.
		m.turnCancel = cancel
		defer func() { m.turnCancel = nil }()

		var steps []toolStep
		var wrapped agent.ToolRunner
		if runner != nil {
			wrapped = &tuiToolRunner{
				inner: runner,
				onStart: func(call backend.ToolCall) {
					if prog != nil {
						prog.Send(toolRunningMsg{step: newRunningStep(call.Name, call.Arguments)})
					}
				},
				onEnd: func(call backend.ToolCall, res string, err error) {
					st := newToolStep(call.Name, call.Arguments, res, err != nil)
					steps = append(steps, st)
					if prog != nil {
						prog.Send(toolStepMsg{step: st})
					}
				},
			}
		}

		sanitizer := agent.NewSanitizer()
		var full strings.Builder
		// Cycle 100: the sanitizer hands us the model's reasoning so the
		// conversation view can render the recovered "Thought for Ns, N
		// tokens" step instead of discarding it silently. Print mode
		// leaves OnThought nil and keeps the byte-exact strip-only
		// behaviour.
		var thought strings.Builder
		var thoughtStart time.Time
		thoughtSent := false
		var meta *backend.ChunkMeta
		sanitizer.OnThought = func(t string) {
			if t == "" {
				return
			}
			if thoughtStart.IsZero() {
				thoughtStart = time.Now()
			}
			thought.WriteString(t)
			if !thoughtSent {
				thoughtSent = true
				if prog != nil {
					prog.Send(thoughtStartMsg{})
				}
			}
		}
		done := false
		// Cycle 228: wire the AGY 1.4.0 subagent toolset into the TUI's
		// agent loop. The subagent tools (invoke_subagent + the two stubs)
		// drive their own LLM turn via piagentbridge, so they don't go
		// through m.toolRunner — they ride on Options.ExtraAgentTools,
		// which Loop.pigTools appends after wrapping Tools. The wiring
		// opens a parallel coding.Services so the subagent's transcript
		// and skill paths land in the same AgentDir the TUI uses.
		extras, closeSub := m.subAgentExtras(ctx, be, model)
		defer closeSub()

		loop, err := agent.New(agent.Options{
			Backend: be,
			// Cycle agy-system-prompt: the reference's system prompt. Empty
			// here meant PiG sent no instructions at all.
			SystemPrompt: m.systemPrompt(),
			Handler: func(chunk backend.StreamChunk) error {
				if chunk.Error != nil {
					// Cycle 262 runs FIRST for the more specific eligibility case;
					// cycle 263 general-error rendering catches everything else.
					if ee := AsEligibilityError(chunk.Error); ee != nil {
						if prog != nil {
							prog.Send(eligibilityNoticeMsg{
								Reason:          ee.Reason,
								VerificationURL: ee.VerificationURL,
							})
						}
						return nil
					}
					// Cycle 263: previously the TUI handler
					// returned nil and silently swallowed the
					// upstream error. The user only saw the
					// failure via the print path (AGY_ERROR
					// envelope). For the TUI surface, render the
					// error text into m.response so the user
					// gets the same hint the AGY_ERROR envelope
					// surfaces — the OAuth scope-mismatch case
					// (status 403 + body keyword) appends the
					// canonical Run `/logout` then `/login` hint
					// via backend.HTTPError.IsScopeMismatch +
					// backend.OAuthScopeHintMessage. The hint
					// is only added for the precise scope case,
					// not for any 403.
					errmsg := chunk.Error.Error()
					var he *backend.HTTPError
					if errors.As(chunk.Error, &he) && he.IsScopeMismatch() {
						errmsg = errmsg + " " + backend.OAuthScopeHintMessage
					}
					if prog != nil {
						prog.Send(backendChunkMsg(errmsg))
					}
					return nil
				}
				if chunk.Meta != nil {
					meta = chunk.Meta
					// Cycle status-line-script: the Done chunk carries the
					// per-turn prompt/completion tokens. Accumulate them
					// into the runMode session totals so the JSON `cost`
					// field the external script reads is the running sum
					// of every turn this session has produced. nil-safe
					// (recordTurnUsage drops a nil meta) and race-free
					// (recordTurnUsage is atomic on the runMode fields).
					m.recordTurnUsage(chunk.Meta)
				}
				if chunk.Text != "" {
					sanitized := sanitizer.Push(chunk.Text)
					full.WriteString(sanitized)
					// Cycle 229: forward every sanitised delta to the
					// bubble-tea program so the model reply streams
					// live into m.response (raw, glamour-styled at
					// turn-end via the turnDoneMsg handler). Pre-cycle
					// 229 the handler accumulated into `full` and only
					// emitted one turnDoneMsg at the end — the user
					// saw a frozen spinner until the turn finished.
					// prog is nil in headless tests so this is a
					// no-op there.
					if prog != nil {
						prog.Send(backendChunkMsg(sanitized))
					}
				}
				if chunk.Done && !done {
					done = true
					full.WriteString(sanitizer.Finish())
				}
				return nil
			},
			DefaultModel: model,
			MaxTurns:     1,
			Tools:        decls,
			ToolRunner:   wrapped,
			// Cycle 227: TODO §4 row. The agent loop fires these
			// callbacks from its drain goroutine on every PiG
			// CompactionStart/End; we translate to tea.Msg via
			// prog.Send so the status row re-renders. A nil
			// prog (test mode) drops them silently — the loop's
			// internal record still updates for callers that
			// read LastCompact.
			OnCompactionStart: func(reason string, startedAt time.Time) {
				if prog != nil {
					prog.Send(compactStartMsg{Reason: reason, StartedAt: startedAt})
				}
			},
			OnCompactionEnd: func(reason string, endedAt time.Time, summary string, tokensBefore, tokensAfter int, aborted bool) {
				if prog != nil {
					prog.Send(compactEndMsg{
						Reason:               reason,
						EndedAt:              endedAt,
						Summary:              summary,
						TokensBefore:         tokensBefore,
						EstimatedTokensAfter: tokensAfter,
						Aborted:              aborted,
					})
				}
			},
			// Cycle 228: extra tools that already implement
			// piagent.AgentTool (subagent delegation) bypass the
			// ToolRunner wrap — see agent.Options.ExtraAgentTools.
			ExtraAgentTools: extras,
			// Cycle effort-picker: the TUI loop's reasoning level comes
			// off m.flags (--effort, or whatever /effort <level> /
			// the picker last wrote). An empty string keeps the model's
			// own default.
			Effort: m.currentEffortName(),
			// Cycle auto-continue-interrupt: a stream-interrupt or
			// iteration-cap is recovered transparently — the TUI
			// replays the user's prompt with an AGY verbatim recovery
			// copy appended ("The stream was interrupted. Please
			// continue the task you were working on.") so the
			// interrupted tool-call chain picks up where it left off
			// instead of leaving the terminal stuck. Recovery depth
			// is bounded by AutoContMaxDepth (=3) inside the loop;
			// autoContBanner publishes a transient "↻
			// auto-continuing…" row each retry so the user sees the
			// recovery live.
			AutoCont: true,
			AutoContBanner: func(depth, maxDepth int) {
				if prog != nil {
					prog.Send(autoContBannerMsg{
						depth:    depth,
						maxDepth: maxDepth,
					})
				}
			},
		})
		if err != nil {
			return backendDoneMsg{}
		}
		defer loop.Close()
		sendErr := loop.Send(ctx, agent.TurnInput{Messages: msgs})
		if ctx.Err() != nil {
			// Esc (or the caller's own cancel) is not a failure to
			// report: the reference treats a user-cancelled turn as done.
			// agent.Loop.Send returns ctx.Err() for it by contract.
			sendErr = nil
		}
		// Cycle 100: fold the turn's reasoning into a leading "Thought for
		// Ns, N tokens" step. The reference merges consecutive thoughts
		// and upgrades a trailing thought into the following tool group
		// (cli/store.canMergeConsecutiveThoughts,
		// canUpgradeTrailingThoughtToToolGroup), so a thought that precedes
		// tool calls belongs to the same block — exactly what prepending
		// here produces.
		if thought.Len() > 0 {
			tokens := 0
			if meta != nil {
				tokens = meta.ReasoningTokens
			}
			steps = append([]toolStep{{
				kind:   stepKindThought,
				name:   "thinking",
				detail: strings.TrimSpace(thought.String()),
				dur:    time.Since(thoughtStart),
				tokens: tokens,
			}}, steps...)
		}

		out := full.String()
		if out == "" && len(steps) == 0 && sendErr == nil {
			return backendDoneMsg{}
		}
		// Cycle stall-empty-step-cleanup: thread the accumulated reasoning
		// through to the receiver so agent.IsEmptyAssistantTurn can
		// classify a turn that produced only thinking (no text, no tool
		// calls) as empty and skip the m.session append. The string is
		// already whitespace-clean: sanitizer.Finish eats the trailing
		// space the chunk-boundary stripping leaves behind.
		//
		// Cycle 279: sendErr rides along. The turn used to end here with
		// the error dropped on the floor (`_ = loop.Send(...)` at the call
		// site), which is what made a mid-task failure indistinguishable
		// from a finished turn.
		return turnDoneMsg{reply: out, steps: steps, reasoning: thought.String(), err: sendErr}
	}
}

type backendChunkMsg []byte

// planTierMsg carries the account tier fetched from loadCodeAssist after
// the first frame. The reference banner starts without the parenthetical
// and adds it when the call lands; keeping the fetch off the constructor
// keeps headless unit tests network-free.
type planTierMsg struct{ tier string }

func fetchPlanTierCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
		defer cancel()
		return planTierMsg{tier: auth.FetchPlanTier(ctx)}
	}
}

// quotaSummaryMsg carries the live retrieveUserQuotaSummary response. A
// failure leaves the spinner's stub fallback in place and is recorded for
// diagnostics; the reference shows "no quota summary" only for accounts
// that genuinely have none, not for transport errors.
type quotaSummaryMsg struct {
	summary *auth.QuotaSummary
	err     error
}

func fetchQuotaSummaryCmd() tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		summary, err := auth.FetchQuotaSummary(ctx)
		return quotaSummaryMsg{summary: summary, err: err}
	}
}

type backendDoneMsg struct{}

// autoContBannerMsg is the transient "↻ auto-continuing…" row that the
// agent.Loop.Options.AutoContBanner callback posts into the tea.Program
// while cycle auto-continue-interrupt is replaying a turn. Depth and
// maxDepth are the AGY verbatim facts the spec asks the user to see on
// the banner so they can tell at a glance whether the next retry is the
// last allowed one; see internal/agent/auto_continue.go.
type autoContBannerMsg struct {
	depth    int
	maxDepth int
}

// compactDoneMsg is the message the manual /compact slash command posts
// back to the bubbletea runtime when PiG's coding.Session emits a
// CompactionEndEvent (cycle 229). The handler in Update translates it
// into a transcript row so the user sees the result.
//
// TokensBefore / TokensAfter are the upstream-reported figures from
// agent.CompactionEndEvent — PiG always populates them when the
// compaction produced a summary, and they stay zero on a "Nothing to
// compact" or aborted compact. The transcript formatter treats zero
// values as "(no token counts)" so the row never says "0 → 0".
//
// Reason is the upstream string ("manual" for /compact, "threshold"
// for the cycle-227 auto-compact path). Cycle 229 only ever fires
// "manual", but the field is forwarded for symmetry.
//
// Err is the upstream Compact() error, populated only when Compact()
// itself failed (the cancel/abort path). A nil Err with non-zero
// TokensBefore indicates a successful compaction that produced a
// summary.
//
// Aborted is set when the upstream end event had
// CompactionEndEvent.Aborted=true — for cycle 229 this can only happen
// if a cycle-227 auto-compact races the manual press (PiG itself
// aborts an in-flight compact when Compact() is called twice; the
// IsCompacting gate short-circuits that case before the manual
// Compact() fires, so Aborted stays false on the manual path).
type compactDoneMsg struct {
	TokensBefore       int
	TokensAfter        int
	Reason             string
	Aborted            bool
	Err                error
	CustomInstructions string
}

// compactStartMsg is pushed via tea.Program.Send when the agent loop
// observes a PiG agent.CompactionStartEvent (cycle 227). The handler
// stores the start time on m and arms a compactSpinnerTickMsg tick so
// the status-row spinner advances once per 100 ms.
type compactStartMsg struct {
	Reason    string
	StartedAt time.Time
}

// compactEndMsg is the matching finish message; the handler captures
// the upstream TokensBefore / EstimatedTokensAfter on the runMode so
// the status row renders "Compacting context… 184 K → 42 K (3 s)" once
// the loop's `tokenK` formatter receives non-zero values. End with
// aborted=true keeps the row visible (no "0 K → 0 K" rewrite) and lets
// the user see the abort happened.
type compactEndMsg struct {
	Reason               string
	EndedAt              time.Time
	Summary              string
	TokensBefore         int
	EstimatedTokensAfter int
	Aborted              bool
}

// compactSpinnerTickMsg advances the compact spinner by one frame on
// a 100 ms cadence (same as the streaming spinner / cycle 225 quota
// spinner). Only re-armed while compact is active; once the end event
// fires the tick stops on its own.
type compactSpinnerTickMsg struct{}

// replaceRange replaces m.response[start:end] with lines. Indices are clamped
// so a stale block offset can never panic.
func (m *runMode) replaceRange(start, end int, lines []string) {
	if start < 0 || start > len(m.response) {
		return
	}
	if end < start || end > len(m.response) {
		end = len(m.response)
	}
	tail := append([]string(nil), m.response[end:]...)
	m.response = append(m.response[:start], lines...)
	m.response = append(m.response, tail...)
}

// rerenderBlocks re-renders every recorded tool-step block at the current
// expand/collapse state. This is what ctrl+o drives: collapsed shows the
// step title plus "(ctrl+o to expand)", expanded shows the step's output
// (recovered hints "ctrl+o to expand" / "ctrl+o to collapse", and the
// "<output +%d lines>" overflow marker).
func (m *runMode) rerenderBlocks() {
	// Cycle 278: the response rows just changed underneath the view, so
	// any cached row slice is stale. Set it here rather than relying on
	// the caller, so ctrl+o mid-stream cannot paint a pre-expansion
	// frame.
	m.rowsCacheDirty = true
	for i := range m.blocks {
		b := &m.blocks[i]
		lines := toolBlockLines(b.steps, m.expanded, m.width)
		delta := len(lines) - b.lines
		oldEnd := b.start + b.lines
		if delta == 0 {
			m.replaceRange(b.start, oldEnd, lines)
			continue
		}
		m.replaceRange(b.start, oldEnd, lines)
		b.lines = len(lines)
		for j := range m.blocks {
			if m.blocks[j].start > b.start {
				m.blocks[j].start += delta
			}
		}
		// Cycle 278: expand/collapse changes the row count *before* the
		// live turn's anchors, so shift them too. Without this, a
		// turnDoneMsg during a streaming turn replaces
		// [liveStart, len(response)) from a stale index — eating rows
		// out of the expanded block, or out of the streamed reply —
		// which is the second half of "output not displayed after
		// ctrl+o". Anchors sitting inside the replaced range are left
		// alone; they are rewritten by the turn itself.
		if m.liveStart >= oldEnd {
			m.liveStart += delta
		}
		if m.replyStart >= oldEnd {
			m.replyStart += delta
		}
	}
}

// blankBefore inserts one empty transcript row when the last row is not
// already blank. The reference puts a blank line between an assistant
// message and the tool block that follows it, and between a block and the
// message that follows. Verified against agy 1.4.3.
func (m *runMode) blankBefore() {
	if len(m.response) == 0 {
		return
	}
	if strings.TrimSpace(stripANSI(m.response[len(m.response)-1])) != "" {
		m.response = append(m.response, "")
	}
}

// finaliseLiveTurn rewrites each in-flight batch where it was painted and
// leaves the streamed prose rows alone. It returns false when the turn has no
// live step runs (the headless/turnDoneMsg-only path, which keeps the original
// block + reply replace).
//
// The reference's finished transcript interleaves: prose, then the block for
// the calls of that assistant message, then the next message's prose. The
// pre-285 clone replaced the whole live range with "all steps, then the whole
// reply", which both reordered the steps above earlier prose and concatenated
// every message (msg.reply has no separators).
func (m *runMode) finaliseLiveTurn(msg turnDoneMsg) bool {
	if len(m.liveStepRuns) == 0 {
		return false
	}
	m.rowsCacheDirty = true
	// A leading thought step is synthesised at turn end and never had a
	// live run. Paint it above the first block.
	if extra := len(msg.steps) - len(m.liveStepRuns); extra > 0 && msg.steps[0].kind == stepKindThought {
		thought := msg.steps[:extra]
		lines := toolBlockLines(thought, m.expanded, m.width)
		at := m.liveStart
		m.replaceRange(at, at, lines)
		for j := range m.liveStepRuns {
			m.liveStepRuns[j].start += len(lines)
		}
		if m.replyStart >= at {
			m.replyStart += len(lines)
		}
		m.blocks = append(m.blocks, turnBlock{start: at, lines: len(lines), steps: thought})
	}
	type batch struct {
		last  int
		start int
		steps []toolStep
	}
	var batches []batch
	for i, r := range m.liveStepRuns {
		if n := len(batches); n > 0 && batches[n-1].steps[0].batch == r.step.batch {
			batches[n-1].last = i
			batches[n-1].steps = append(batches[n-1].steps, r.step)
			continue
		}
		batches = append(batches, batch{last: i, start: r.start, steps: []toolStep{r.step}})
	}
	// Newest batch first: replacing a later range cannot move an earlier
	// batch's anchor.
	for bi := len(batches) - 1; bi >= 0; bi-- {
		b := batches[bi]
		end := m.liveStepRuns[b.last].start + m.liveStepRuns[b.last].lines
		if b.start < 0 || end > len(m.response) {
			continue
		}
		lines := toolBlockLines(b.steps, m.expanded, m.width)
		delta := len(lines) - (end - b.start)
		m.replaceRange(b.start, end, lines)
		m.blocks = append(m.blocks, turnBlock{start: b.start, lines: len(lines), steps: b.steps})
		if delta != 0 {
			for j := range m.liveStepRuns {
				if m.liveStepRuns[j].start >= end {
					m.liveStepRuns[j].start += delta
				}
			}
			if m.replyStart >= end {
				m.replyStart += delta
			}
		}
	}
	sort.SliceStable(m.blocks, func(i, j int) bool { return m.blocks[i].start < m.blocks[j].start })
	return true
}

// rerenderLiveSteps re-renders the in-flight turn's already-painted tool
// steps at the current expand/collapse state. Until turnDoneMsg replaces
// [liveStart, len(response)) with the final block, those steps are ordinary
// transcript rows with no turnBlock pointing at them, so rerenderBlocks
// cannot reach them — the reported "ctrl+o does nothing while generating".
//
// The runs are walked newest first so replacing a later run cannot move an
// earlier run's start; only anchors that sit *after* the replaced range
// (replyStart in the usual step-then-reply order) need the delta.
func (m *runMode) rerenderLiveSteps() {
	if len(m.liveStepRuns) == 0 {
		return
	}
	// Same reason as rerenderBlocks: the cached row slice is stale.
	m.rowsCacheDirty = true
	for i := len(m.liveStepRuns) - 1; i >= 0; i-- {
		r := &m.liveStepRuns[i]
		oldEnd := r.start + r.lines
		if r.start < 0 || oldEnd > len(m.response) {
			// The range no longer exists (e.g. a slash-command row
			// spliced over it). Drop the run rather than editing the
			// wrong rows.
			m.liveStepRuns = append(m.liveStepRuns[:i], m.liveStepRuns[i+1:]...)
			continue
		}
		lines := toolStepLines(r.step, m.expanded, m.width)
		delta := len(lines) - r.lines
		m.replaceRange(r.start, oldEnd, lines)
		if delta == 0 {
			continue
		}
		r.lines = len(lines)
		// Rows after this run moved: every newer run (already restyled
		// above, but its recorded start is stale) and a reply that
		// streamed after the block.
		for j := i + 1; j < len(m.liveStepRuns); j++ {
			m.liveStepRuns[j].start += delta
		}
		if m.replyStart >= oldEnd {
			m.replyStart += delta
		}
	}
}

// appendCompactResult formats a compactDoneMsg into one styled
// transcript row and appends it to m.response. Cycle 229: the row is
// always added (no collapse / no merge), so a /compact press during a
// streaming turn produces a separate, easy-to-find pair
// ("Compacting context…" + "Compacted: …") below the running turn.
//
// Three messages cover all cases the cycle-229 spec calls out:
//
//   - success:           "⎿  Compacted: 184000 → 42000 tokens (76.7% reduction)"
//   - aborted/empty:     "⎿  Compacted: 0 → 0 tokens"  (no reduction suffix)
//   - error:             "⎿  Compact failed: <err>"
//
// The styling is the same dim-gray + ⎿ prefix slashRespond uses, so
// the row reads as a continuation of the running echo rather than a
// fresh slash command. Reduction percentage is computed on
// TokensBefore; a TokensBefore of zero (PiG aborted before recording)
// falls back to the raw token string so the row never displays
// "Infinity %" or a divide-by-zero.
//
// The helper is unexported because only the compactDoneMsg handler in
// Update() should call it; cycle-227's auto-compact status row uses
// different text (cycle-227 ships its own renderer, see the openagy-w227
// branch — cycle 229 does not touch that path).
func (m *runMode) appendCompactResult(msg compactDoneMsg) {
	var body string
	switch {
	case msg.Err != nil:
		body = "Compact failed: " + msg.Err.Error()
	default:
		var tokensLine string
		if msg.TokensBefore > 0 || msg.TokensAfter > 0 {
			tokensLine = fmt.Sprintf("%d \u2192 %d tokens", msg.TokensBefore, msg.TokensAfter)
			if msg.TokensBefore > 0 {
				pct := (1 - float64(msg.TokensAfter)/float64(msg.TokensBefore)) * 100
				if pct < 0 {
					pct = 0
				}
				tokensLine += fmt.Sprintf(" (%.1f%% reduction)", pct)
			}
		} else {
			tokensLine = "0 \u2192 0 tokens"
		}
		body = "Compacted: " + tokensLine
		if msg.Aborted {
			body += " (aborted)"
		}
	}
	m.response = append(m.response,
		cWarningBlueOpen+"\u23bf  "+cWarningBlueClose+panelGray238+body+panelGray238C,
		"",
	)
}

// View implements tea.Model.
func (m *runMode) View() tea.View {
	if m.login != nil {
		return m.login.View()
	}

	// Cycle 119: the transcript is rendered through a viewport so the view
	// can never grow past the terminal. While the terminal size is unknown
	// the pre-119 unclipped layout is kept, which is what the byte-exact
	// snapshots and the unit tests assert.
	// Cycle 122: only build the unclipped layout when the sized path is
	// unavailable. The sized path takes a cached wrap.
	var transcript []string
	if m.height > 0 {
		transcript = m.transcriptWindow()
	} else {
		transcript = m.transcriptRows(0)
	}
	// Cycle 174: paint the in-app text-selection highlight on top
	// of the already-padded transcript rows. selectionPass is a
	// no-op when no selection is active, so this is free for the
	// idle frame path.
	transcript = m.selectionPass(transcript)
	body, cursorY := m.viewBody(transcript)
	// Cycle 214: REF's /changelog wrap stops at width-2, leaving the
	// rightmost two cells per line as default-background blanks.
	// fitLines would extend each line to width via an explicit gray
	// padding run, but ultraviolet's paint/transform path collapses
	// that trailing fg-coloured run into a \x1b[K and the 2 cells
	// vanish from the tmux capture. REF's captures match the clone's
	// without that extension, so we skip it while the changelog
	// panel owns the body. The fixed chrome (separators, banner,
	// status row) is unaffected because viewBody emits them after
	// panelView — see lines below.
	if m.width > 0 && m.panel.kind != panelChangelog {
		body = fitLines(body, m.width)
	}
	// Cycle 119: on a terminal shorter than the fixed chrome (banner +
	// separators + prompt + status) the view still must not exceed the
	// screen. Drop whole rows off the top — the prompt and status row are
	// the rows worth keeping — and shift the cursor with them.
	//
	// Cycle 182: REF caps the emitted view at `height + 8` rows and
	// drops the *bottom* beyond that before scrolling. Recovered from
	// the /help overlay, whose panels are the only views tall enough to
	// overflow:
	//
	//	tab        panel rows   status row visible?
	//	general    15           h >= 21
	//	commands   7 + n        h >= 33   (n = 20 at h = 32..33)
	//	shortcuts  8 + n        h >= 38   (n = 24 at h = 38)
	//
	// In every case the rule is `content <= height + 8`, i.e. the app
	// never emits more than 8 rows past the fold, and the status row is
	// simply truncated away when the panel is taller. At 220×30 the
	// COMMANDS panel (39 rows) and the SHORTCUTS panel (40 rows) both
	// clamp to 38, which is why the model label is invisible on those
	// two tabs but present on GENERAL.
	//
	// viewOverflowRows lives next to viewportHeight in tui_viewport.go
	// because the transcript budget has to know about it too.
	if m.height > 0 {
		if maxRows := m.height + viewOverflowRows; viewRows(body) > maxRows {
			lines := strings.Split(body, "\n")
			keep := maxRows
			if keep < 1 {
				keep = 1
			}
			if keep < len(lines) {
				body = strings.Join(lines[:keep], "\n")
			}
		}
		drop := viewRows(body) - m.height
		if drop < 0 {
			drop = 0
		}
		if drop > 0 {
			lines := strings.Split(body, "\n")
			// Cycle 207: viewBody always ends with a newline, so Split
			// yields a phantom trailing empty entry. Without dropping it
			// first the scrolled view emits one row more than the
			// terminal and the real top row scrolls off: the /changelog
			// viewer lost its title at h=24 while REF kept it at row 1.
			if n := len(lines); n > 0 && lines[n-1] == "" {
				lines = lines[:n-1]
			}
			if drop < len(lines) {
				body = strings.Join(lines[drop:], "\n")
				cursorY -= drop
				if cursorY < 0 {
					cursorY = 0
				}
			}
		}
		// Cycle 232: convert the picker's pre-trim body-line indices
		// into screen-Y coordinates now that we know how many rows
		// were trimmed from the top. The picker's geom is the
		// rectangle the mouse handlers dispatch against; the prompt
		// screen Y is the row a click on the input field re-derives
		// from.
		if m.pickerLayoutLocal.Rows > 0 && m.picker.Visible() {
			start := m.pickerBodyStart - drop
			m.pickerGeom = pickerGeom{
				Valid:       true,
				HasList:     m.pickerLayoutLocal.HasList,
				TopY:        start,
				BottomY:     start + m.pickerLayoutLocal.Rows - 1,
				ListTopY:    start + m.pickerLayoutLocal.ListTop,
				ListBottomY: start + m.pickerLayoutLocal.ListBottom,
				HintY:       start + m.pickerLayoutLocal.HintRow,
			}
		}
		if m.promptBodyStart >= drop {
			m.promptScreenY = m.promptBodyStart - drop
		}
	}

	v := tea.NewView(body)
	v.AltScreen = m.altScreen
	// Cycle 174: in alt-screen mode the reference binary always emits
	// MouseModeCellMotion so wheel events scroll the transcript AND
	// the in-app text-selection overlay (mouse-down → drag →
	// release) can copy the highlighted range to the clipboard via
	// OSC52. There is no toggle — removing the cycle-166 --mouse /
	// /mouse infrastructure was the user's explicit ask, because the
	// in-app selection obsoletes the cycle-166 trade-off.
	if m.altScreen {
		v.MouseMode = tea.MouseModeCellMotion
	}
	// Cycle 105: /title on shows a custom terminal window title. The
	// reference's exact text is unrecovered (see titleFeedback); the
	// conversation title is the clone's choice.
	if m.title == "on" {
		v.WindowTitle = m.conversationTitle()
	}
	// Cycle osc-7501: OSC 7501 (Program Status Protocol) report. Set on
	// every View() so terminals that support the protocol (kitty,
	// ghostty, WezTerm ≥ 2024, foot) display a "Working…/Blocked/Done/
	// Error" indicator in the tab or status line. Terminals without
	// support silently ignore the OSC 7501 sequence the runtime emits
	// from the value above.
	v.ProgramStatus = m.programStatus()
	// Cycle 83: when helpMode owns the screen we hide the cursor —
	// the ref binary also has no visible caret while the overlay is
	// up. We still emit a cursor at the prompt line so bubbletea's
	// alt-screen driver doesn't auto-compute one elsewhere.
	cursorX := m.inputCursorX()
	if m.helpMode {
		cursorY = 0
		cursorX = 0
	}
	v.Cursor = &tea.Cursor{
		Position: tea.Position{X: cursorX, Y: cursorY},
		Shape:    tea.CursorBlock,
		Blink:    true,
	}
	return v
}

// viewBody renders the whole screen for the given transcript rows and returns
// it together with the cursor's row. The cursor row is derived from the
// assembled content rather than from a hard-coded layout offset so it stays
// correct once the transcript viewport clips rows (cycle 119): the prompt
// line's position depends on how many transcript rows are actually shown.
//
// Cycle 124: the banner and the fallback warning block are no longer chrome.
// Both moved into the scrollable area as its first rows (scrollHeaderRows),
// so they scroll away with the transcript when the user scrolls down — the
// reference binary hides them past the first screen. The top separator
// also moved: it now sits between the transcript and the prompt, so the
// prompt is the visual unit bracketed by the two horizontal lines, not
// "transcript + prompt together bracketed".
func (m *runMode) viewBody(transcript []string) (string, int) {
	var b strings.Builder

	// Cycle 232: clear the picker's last-frame geometry so a hidden
	// picker from a previous render does not survive into this frame.
	m.pickerGeom = pickerGeom{}
	m.promptScreenY = -1

	// The 1.4.3 /changelog viewer is a full-screen document: title at row 1,
	// window, counter, blank, legend, then the status row at the last row.
	// It replaces the transcript + prompt chrome rather than sitting below it
	// (captured 2026-10-20 at 120x40).
	if (m.panel.kind == panelChangelog || m.panel.kind == panelDiff) && m.height > 0 {
		b.WriteString(m.panelView())
		if m.statusLine != "off" {
			b.WriteString(m.statusRow())
		}
		return b.String(), 0
	}

	// Response transcript (already clipped to the viewport by
	// transcriptWindow whenever the terminal size is known). In the v1.3.5
	// binary the banner + warning + transcript all live in this scrollable
	// region: the banner is the first few rows at the top, and scroll-down
	// makes it disappear.
	for _, line := range transcript {
		b.WriteString(line)
		b.WriteString("\n")
	}

	// Top separator. It now lives BETWEEN the transcript and the prompt,
	// so the prompt is bracketed by the two horizontal lines (this sep +
	// the bottom sep below the input). The reference puts this line in
	// the same place.
	b.WriteString(m.separator())
	b.WriteString("\n")

	// feature/announcement-cards: when the in-memory queue has a
	// head, render the announcement card row between the top
	// separator and the prompt input. Empty queue writes the empty
	// string so the layout stays one-line lower than the previous
	// cycle for the no-announcement case (the spec's "empty queue
	// shows no card row" rule). The card sits in this slot only
	// when the picker / resume / help overlays are NOT visible —
	// those overlays own the prompt chrome while open and the
	// announcement card would visually clash with them.
	if !m.picker.Visible() && !m.resumeMode && !m.helpMode {
		if row := m.announcementRow(); row != "" {
			b.WriteString(row)
		}
		// Cycle 262: eligibility-error inline notice. Renders
		// immediately below the announcement card (or in the
		// same slot when no announcement is active) — both
		// stack between the top separator and the prompt input.
		// Returns "" when no notice is set so the no-notice case
		// is a free no-op. Overlays (picker / resume / help) own
		// the prompt chrome while open and the eligibility row
		// would visually clash with them, so the same
		// !picker.Visible && !resumeMode && !helpMode guard
		// applies. The inline row is written directly into the
		// screen buffer here — it NEVER touches m.response or
		// m.session, so the conversation transcript stays clean
		// (cycle 262's hard constraint).
		if row := m.eligibilityNoticeRow(); row != "" {
			b.WriteString(row)
		}
	}

	// Cycle 283: prompts submitted while a turn was in flight render
	// as a pending queue directly above the input box, one row each,
	// in the dim grey with the reference's hint suffix. Measured from
	// a truecolour pty capture of agy 1.4.3:
	//
	//	▸ SECOND message while running (alt+m to send immediately)
	//	────────────────────────────
	//	>
	//
	// The extra separator above the input is the reference's own box
	// border, so the queue reads as part of the prompt, not the
	// transcript. The rows move into the transcript (as a normal
	// "> ..." user row) when the turn finishes and the flush starts.
	if len(m.streamQueue) > 0 {
		for _, queued := range m.streamQueue {
			b.WriteString(cDimGrayOpen + "\u25b8 " + queued +
				" (alt+m to send immediately)" + cDimGrayClose)
			b.WriteString("\n")
		}
		b.WriteString(m.separator())
		b.WriteString("\n")
	}

	// BTW side-question floating card above the input box (aboveInputCard).
	if row := m.aboveInputCard(); row != "" {
		b.WriteString(row)
		b.WriteString("\n")
	}

	// Rows above the prompt line, needed for the cursor position below.
	rowsAbovePrompt := viewRows(b.String())
	// Cycle 232: the prompt screen Y is derived from this offset
	// after View() trims the leading drop count. We stash the
	// pre-trim body-line index here so View() can subtract drop.
	m.promptBodyStart = rowsAbovePrompt

	// Prompt region between the two separators. The v1.3.5 binary
	// shows the literal "> " prompt with trailing padding to a
	// 10-char prompt row when the buffer is empty (idle), and the
	// full textarea when there's content. We always emit the idle
	// prompt byte-for-byte so the layout matches /tmp/ref_idle.bin.
	// Cycle 283: the mode cue is the empty input's placeholder, but the
	// reference hides it whenever an overlay owns the body — the /resume
	// picker's idle row reads a bare ">" (verified at 200x40 against
	// 1.4.3). Re-derive it each frame so it tracks both the mode and the
	// overlay state without a second field to keep in sync.
	switch {
	case m.picker.Visible() || m.panel.kind != panelNone || m.helpMode || m.resumeMode || m.voiceMode:
		m.input.Placeholder = ""
	case m.inputNotice != "":
		// The transient notice outranks the dismissed mode cue: 1.4.4
		// still prints "Nothing to compact yet." after a prompt has been
		// sent, when the usual placeholder is already suppressed.
		m.input.Placeholder = m.inputNotice
	case m.modeHintDismissed:
		m.input.Placeholder = ""
	default:
		m.input.Placeholder = permissions.ModeBanner(m.mode)
	}
	inputView := m.input.View()
	// Cycle 94: ref binary renders the prompt ">" glyph in
	// saturated blue and the trailing space " " in the default
	// color (the reset is inserted RIGHT AFTER the ">" not after
	// the " "). Our bubbles textarea wraps the entire
	// SetPromptFunc return value "> " with the Foreground blue
	// style, producing `<blue>> <reset>` where the space is also
	// colored.
	//
	// Cycle 182: bubbles v2 emits the 24-bit brand blue
	// (\x1b[38;2;66;133;244m), not the 256-color form the original
	// cycle-94 patch matched, so that patch had silently stopped
	// firing and the prompt space stayed blue. Match either opening
	// sequence and move the reset one cell left.
	inputView = strings.Replace(inputView,
		cBlueOpen+"> \x1b[m",
		cBlueOpen+">\x1b[m ",
		1)
	if !strings.Contains(inputView, cBlueOpen+">\x1b[m ") {
		inputView = strings.Replace(inputView,
			"\x1b[38;5;69m> \x1b[m",
			"\x1b[38;5;69m>\x1b[m ",
			1)
	}
	if !strings.HasSuffix(inputView, "\n") {
		inputView += "\n"
	}
	if m.hasInputSelection() {
		inputView = m.applyInputSelectionHighlight(inputView)
	}
	b.WriteString(inputView)
	// The cursor sits on the prompt's last row. bubbles' Cursor().Y is the
	// row within the textarea, so the absolute row is the number of rows
	// above the prompt plus that offset (cycle 119: previously this was a
	// hard-coded 1+5+1+warning+1+len(m.response), which is wrong as soon as
	// the viewport clips the transcript).
	cursorY := rowsAbovePrompt
	if c := m.input.Cursor(); c != nil {
		cursorY += c.Y
	}

	// Second separator.
	b.WriteString(m.separator())
	b.WriteString("\n")

	// Cycle 232: snapshot the body-line index where the picker rows
	// are about to land. pickerView() will populate pickerLayoutLocal
	// with the picker's internal layout (row count, list range, hint
	// row, scroll-tail flags). View() converts the pre-trim body-line
	// indices into screen-Y coordinates by subtracting the top-trim
	// count it computes after this function returns.
	m.pickerBodyStart = viewRows(b.String())

	// Picker overlay rows (only when visible). In the v1.3.5 binary the
	// picker rows stack between the second separator and the status row.
	if pickerRows := m.pickerView(); pickerRows != "" {
		b.WriteString(pickerRows)
	}
	// feature/at-file-dropdown-newline: the @ file-mention dropdown
	// shares the same view-slot as the slash picker. Mutually
	// exclusive with the slash picker in practice (a buffer cannot
	// start with both "/" and "@"), but writing them in series
	// keeps the layout robust if a future edit ever has one visible
	// while the other isn't.
	if atFileRows := m.atFileView(); atFileRows != "" {
		b.WriteString(atFileRows)
	}
	// Cycle 167: /resume conversation picker overlay (mutually
	// exclusive with the slash-command picker — the latter is for
	// commands in the input, the former for picking conversations).
	if resumeRows := m.resumeView(); resumeRows != "" {
		b.WriteString(resumeRows)
	}

	// Cycle 185: modal slash-command panel (the reference's full-screen
	// picker for /mcp, /tasks, /effort, …). Emitted in the same slot as
	// the /help overlay — below the bottom separator, above the status
	// row.
	if panelRows := m.panelView(); panelRows != "" {
		b.WriteString(panelRows)
	}

	// Voice dictation banner overlay (matching AGY AudioModel.Banner).
	if voiceRows := m.voiceView(); voiceRows != "" {
		b.WriteString(voiceRows)
	}

	// Cycle 83: 3-tab /help TUI overlay. When helpMode is true we emit
	// the active tab's panel content between the bottom separator and
	// the status row. The ref binary always renders the tab bar first
	// (general/commands/shortcuts) followed by the panel body, then
	// the keyboard hint row, then the status row. Pickers and the
	// stream-prompt spinner are suppressed while the overlay owns the
	// screen.
	if m.helpMode {
		b.WriteString(renderHelp(m.helpTab, m.model, m.tier, m.cwd, m.email, m.conversationID, m))
	}

	// Status row at the very bottom. Cycle 105: /statusline off hides it.
	// Cycle 150 wrongly suppressed it while the /help overlay was open;
	// cycle 182 restores it. REF v1.4.0 renders the overlay as
	// "<transcript> <sep> <prompt> <sep> <panel> <status row>" — the
	// status row still follows the panel, and on the GENERAL tab it is
	// plainly visible at row 28 of a 30-row terminal. Only the left
	// hint is dropped (statusRow() already blanks it in helpMode), so
	// the row carries the right-aligned model label alone. On the
	// taller COMMANDS / SHORTCUTS panels the row lands below the
	// fold and the cycle-119 top-drop in View() scrolls it away,
	// exactly as REF does.
	// Cycle 207: REF's /changelog viewer is a full-screen panel — its
	// title is always at row 1 and the screen ends on the panel's own
	// trailing blank (changelog24/26/33/50.ref.raw.txt). The status row
	// it would otherwise push onto row height+1 is not drawn, so a
	// height-aware changelog panel drops it.
	if m.statusLine != "off" && !(m.panel.kind == panelChangelog && m.height > 0) {
		// Cycle subagent-question-picker: render the
		// "<agent> has a question" alert rows above the status
		// row. Each pending question on each scope produces one
		// row, sorted by scope (parent first). The picker
		// overlay sits between the prompt and these alert rows
		// so a user can answer without losing the badge.
		if rows := m.subAgentQuestionAlertRows(); len(rows) > 0 {
			for _, row := range rows {
				b.WriteString(row)
				b.WriteString("\n")
			}
		}
		// The question picker overlay renders between the
		// bottom separator and the status row so it sits
		// directly above the alert badge.
		if rows := m.subAgentQuestionPickerRows(); len(rows) > 0 {
			for _, row := range rows {
				b.WriteString(row)
				b.WriteString("\n")
			}
		}
		b.WriteString(m.statusRow())
	}

	return b.String(), cursorY
}

func (m *runMode) inputCursorX() int {
	if c := m.input.Cursor(); c != nil {
		return c.X
	}
	return 0
}

func runLiveTUI(w io.Writer, fs *flag.FlagSet) error {
	if !isTerminalInteractive(os.Stdin) {
		return renderTUI(w)
	}

	// Cycle 125: the on-startup workspace-trust gate. The reference
	// prompts the user the first time it runs from a cwd that is not
	// already in ~/.gemini/antigravity-cli/settings.json's
	// trustedWorkspaces list. We mirror that: if trustGate says we
	// must ask, run the dialog and either persist cwd to the list
	// (accept) or exit cleanly (deny). Skip hooks:
	//
	//	--trust-cwd     cycle 125 test hook
	//	$OPENAGY_TRUST  =1 / =always / =<path>  (CI / scripted use)
	//
	// isWorkspaceTrusted is set in cmd/wagyu/main.go's initWiring()
	// to point at auth.IsWorkspaceTrusted — keeping the import in
	// main.go avoids an import cycle with cmd/wagyu/trust_prompt.go.
	canonicalCwd, skipTrust, terr := trustGate(fs)
	if terr != nil {
		fmt.Fprintf(w, "warning: workspace trust check failed: %v\n", terr)
	}
	if !skipTrust {
		res, perr := runTrustPrompt(canonicalCwd, os.Stdin, w)
		if perr != nil {
			return perr
		}
		if res == trustDeny {
			// Reference behaviour: clean exit, no TUI ever
			// appears. rc=0 matches the "Esc/n = exit" hint.
			return nil
		}
		if err := addTrustedWorkspace(canonicalCwd); err != nil {
			fmt.Fprintf(w, "warning: could not persist trust: %v\n", err)
		}
	}

	// On-startup auth gate: when no OAuth token, ADC, or GEMINI_API_KEY is found,
	// prompt the user to sign in, matching reference behavior.
	if !hasOAuthToken() && os.Getenv("GEMINI_API_KEY") == "" && os.Getenv("AGY_ADC_AUTH") == "" {
		loginProg := tea.NewProgram(newLoginModel())
		finalM, err := loginProg.Run()
		if err != nil {
			return err
		}
		if lm, ok := finalM.(*loginModel); !ok || !lm.authenticated {
			return errors.New("authentication required")
		}
	}

	modelName := flagStringValue(fs, "model")
	// Cycle 99: the reference resolves flag → settings.json → default and then
	// checks the result against its model catalogue. A configured model that
	// is no longer offered falls back to Gemini 3.8 Flash (High) and renders
	// the "is no longer available" warning. (Cycle 95 made the settings model
	// win unconditionally, which silenced that warning and diverged from the
	// reference — see model_resolve.go.)
	modelName, unavailableFrom := resolveEffectiveModel(modelName)
	if modelName == "" {
		modelName = defaultModel
	}
	altScreen := true
	if flagBoolValue(fs, "no-alt-screen") {
		altScreen = false
	} else if !flagBoolValue(fs, "alt-screen") {
		altScreen = false
	}
	be := pickBackend(fs, modelName)
	// Cycle 219: when the agw backend is the resolved client, prefer its
	// configured ExternalModel over the catalogue's Gemini fallback. The
	// default agw config is MiniMax-M3, so a fresh install (no settings
	// file, no --model flag) should land on MiniMax M3, not Gemini 3.8
	// Flash. We only override when the agw model is a MiniMax slug/label
	// to preserve the catalogue-based warning for the other backends.
	if ab, ok := be.(agwBackend); ok {
		extModel := strings.TrimSpace(ab.AgwConfig().ExternalModel)
		if isMiniMax(extModel) {
			if !isMiniMax(modelName) {
				modelName = "MiniMax M3"
			}
			// Cycle 278: the agw backend genuinely serves MiniMax M3, so
			// the catalogue-availability warning must not fire at all.
			// Before this, a settings.json holding the MiniMax label
			// produced the self-contradictory block
			//
			//	⚠ Warning
			//	  ⎿  "MiniMax M3" is no longer available. Using "MiniMax M3".
			//
			// which also cost three rows of vertical space at the top of
			// every frame (the picker and the transcript both sat three
			// rows lower than the reference's). Verified against agy
			// 1.4.2 at 200x40, which prints no warning in this config.
			unavailableFrom = ""
		}
	}
	userEmail := auth.EffectiveUserEmail()
	m := NewRunMode(be, userEmail, modelName, "", "", altScreen)
	m.backendFor = func(label string) backend.Client { return pickBackend(fs, label) }
	// A markdown custom agent swaps the prompt and the tool surface. An
	// unknown name gets the reference's in-TUI ⚠ Agent Not Found card.
	if name := flagStringValue(fs, "agent"); name != "" {
		if err := agent.ValidateAgent(name); err != nil {
			var nf *agent.AgentNotFoundError
			if errors.As(err, &nf) {
				m.agentWarning = strings.TrimPrefix(nf.Error(), "error: ") + ". Using the default agent instead."
			}
		}
	}
	m.agentBody = agentBody(customAgentFromFlags(fs))
	if m.agentBody != "" {
		m.agentName = strings.TrimSpace(flagStringValue(fs, "agent"))
	}
	// Cycle 216: Editor Mode › Vim / Insert First are read once here (and
	// again whenever a slash panel closes), not per keystroke.
	m.reloadVimSettings()
	// feature/announcement-cards: seed the visible queue from the
	// catalog minus any persisted dismissals. This must run after
	// NewRunMode (which leaves the queue empty) and before the first
	// View() so the user lands on a screen that already has the
	// announcement row visible (or not, if the queue is empty).
	m.loadAnnouncements()
	// Cycle 286: seed the prompt history from history.jsonl so the
	// user can immediately navigate earlier prompts across sessions
	// with Up/Down arrow keys, matching agy's persistent history.
	m.loadHistory()
	// Cycle plugin-fsnotify: arm the ~/.gemini plugin/skill/marketplace
	// watcher. New returns (nil, nil) when the home tree is empty
	// (a fresh install that has never run `plugin install`) — that is
	// the right fallback for a first-launch session; the loader
	// functions already render a useful empty state. The watcher is
	// constructed only on the TUI path; print mode skips it because
	// the print turn exits before any UI tick could drain events, and
	// paying for the fsnotify goroutine on every `agy -p` is wasted
	// work. Tied to a context derived from the TUI program's
	// lifecycle so the goroutine exits on Ctrl-C / Esc / q.
	watchCtx, watchCancel := context.WithCancel(context.Background())
	defer watchCancel()
	if w, werr := pluginwatch.New(auth.AntigravityConfigHome()); werr == nil && w != nil {
		m.pluginWatcher = w
		pluginWatcherPtr.Store(w)
		defer pluginWatcherPtr.Store(nil)
		go w.Run(watchCtx)
		// Close on program exit so the underlying fsnotify watch is
		// released; the deferred watchCancel also stops the loop, but
		// the watcher holds an OS-level fd that Close releases.
		defer w.Close()
	}
	// Cycle 102: the TUI slash handlers reuse the print-mode content
	// generators, which read --model/--effort from the flag set.
	m.flags = fs

	// Cycle 99: give the interactive session the recovered tool surface so
	// the agent can actually act. Print mode has driven the same agent loop
	// since cycle 97; before this the TUI advertised nothing and the model
	// answered "I don't have a file reading tool available" (live capture).
	workspace, _ := os.Getwd()
	catalog := agentToolRegistry(workspace, m.agentBody)
	// Cycle subagent-question-picker: re-register ask_question
	// with the per-subagent question Manager so an ask_question
	// call from the parent enqueues onto the parent's pending
	// list and waits for the picker to settle. The earlier
	// catalog default returned the cycle-214 noUserText and the
	// subagent was left waiting forever — exactly the bug the
	// changelog line is closing. Subagent calls use the same
	// catalog entry but with a per-call SubAgentID set by the
	// subagent's own invoke path (cycle 229 work).
	m.registerSubagentQuestionTool(catalog)
	// Cycle 104: one policy value shared by the tool gate and the TUI, so
	// Shift+Tab cycling takes effect on the next tool call.
	// Cycle 228 (AGY 1.2.10 close-gap): the same print-mode sandbox is
	// attached to the interactive gate. The TUI's per-call bypass is
	// applied through CheckCommandWithBypass in the runner, so a
	// `/bypass-sandbox` invocation lifts the restriction for one call
	// only and the next call goes back to the sandbox-first contract.
	sessionPolicy := printModePolicy(fs)
	sessionSandbox := printModeSandbox(fs, workspace, m.conversationID)
	m.toolSandbox = sessionSandbox
	m.EnableTools(advertisedToolDecls(m.agentBody, catalog), &permissionRunner{
		inner:   &registryRunner{reg: catalog},
		policy:  &sessionPolicy,
		sandbox: sessionSandbox,
	})
	m.mode = sessionPolicy.Mode
	m.policy = &sessionPolicy
	// Cycle 283: the starting mode's cue is the empty input's placeholder,
	// exactly as Shift+Tab leaves it (see cycleMode).
	m.input.Placeholder = permissions.ModeBanner(m.mode)
	// Cycle 105: seed the /statusline and /title toggles from settings.json.
	// Cycle 184: both keys are booleans in the reference's schema — the
	// string form this used to read is accepted too so a settings.json the
	// pre-184 clone wrote still round-trips.
	m.statusLine = "on"
	if v, ok := auth.SettingsBool("statusLine"); ok && !v {
		m.statusLine = "off"
	}
	m.title = "off"
	if v, ok := auth.SettingsBool("title"); ok && v {
		m.title = "on"
	}
	// Cycle status-line-script: AGY 1.1.27 moves the status-line script
	// config into settings.json's `statusLine.command` /
	// `statusLine.enabled` block. When present + enabled, the TUI's
	// built-in status row gives way to the spawned script. Cache the
	// resolved command at startup so the tick handler does not have
	// to re-parse settings.json every 100 ms; the cycle-105 /statusline
	// toggle re-reads it on demand (see /statusline in
	// cmd/wagyu/slash_commands.go).
	if cmd, ok := loadStatusLineConfig(); ok {
		m.statusLineExternal = true
		m.statusLineCommand = cmd
	}

	// Cycle 120: --conversation / -c open the session on the previous turns.
	// The adopted id replaces the freshly minted one so the session is the
	// conversation the caller named, and the loaded transcript is rendered
	// into the viewport.
	//
	// Cycle resume-continuation-r1: the previous cycle added an auto-resume
	// prompt that fired on every launch when stdin was a TTY ("Resume
	// previous conversation? [Y/n] "). That prompt is GONE: it was
	// speculative — `strings ~/.local/bin/agy | grep "Resume previous"`
	// returns nothing, the literal `Resume with -c (or command below):`
	// is the closest match and lives inside the /resume picker, not at
	// startup. The parent/child walk + /continue slash + --continue flag
	// stay; only the on-launch prompt is removed.
	//
	// Cycle resume-continuation-r2: while removing the prompt we noticed
	// REF actually DOES print a hint at startup — "Resume with -c (or
	// command below):" + "<binary> --conversation=<id>" — when a prior
	// session exists for the workspace. The hint is informational, not a
	// prompt: it tells the user how to resume without spelling it out.
	// Suppressed when --continue / -c was given (the picker / --continue
	// path already shows the conversation it picked) or when --conversation=<id>
	// already adopted a session.
	sel := resolveConversationSelection(fs, os.Stderr)
	resumeHintBanner(canonicalCwd, sel.ID != "",
		resolveBoolAlias(fs, "continue", "c"), os.Stderr)
	if sel.ID != "" {
		m.conversationID = sel.ID
	}
	m.seedConversation(sel.Store)

	// Cycle 81/99: the fallback warning is driven by resolveEffectiveModel,
	// which applies the reference's availability check (see
	// model_resolve.go). `unavailableFrom` is the configured model that the
	// catalogue no longer offers.
	if unavailableFrom != "" {
		m.fallbackFrom = unavailableFrom
		m.fallbackTo = modelName
	}

	// v1.3.5 changelog gap: bubbletea detects the colour profile
	// itself in tea.go's program setup (it ignores our
	// lipgloss.Writer.Profile mutation). Pass the LC_TERMINAL-aware
	// profile through WithColorProfile so the bubbletea renderer
	// downsample / conversion path uses the same TrueColor that
	// the rest of the TUI is rendering at. See
	// colorprofile_lc_terminal.go for the precedence.
	prog := tea.NewProgram(m, tea.WithColorProfile(resolveColorProfile(os.Environ())))
	// Cycle osc-hyperlinks (cycle 2 of the OSC pair): the "Terminal
	// detection: …" debug log line the AGY 1.4.0 strings table
	// cites. Computes render.HyperlinkCaps from os.Environ() and
	// the tmux hyperlink round-trip probe, logs the result to
	// stderr, and stores the caps on the runMode so any consumer
	// (glamour adapter, /changelog row renderer, future
	// markdown-linkers) can read it without rebuilding the env.
	// Sequence: probe first (writes "tmux hyperlink probe failed"
	// to stderr when applicable), then the env-merged detection
	// log, then store. The probe's noise and the env log together
	// form the AGY-recovered diagnostic pair; order matches.
	m.hyperlinkCaps = hyperlinkCapsFromEnv(
		os.Stderr,
		os.Environ(),
		tmuxHyperlinkProbe(),
		altScreen,
	)
	// Cycle 99: expose the program to the model so the streaming goroutine
	// can push tool-step updates into the UI as they happen.
	m.prog = prog
	_, err := prog.Run()
	return err
}

// authResolveSettingsModelOrEmpty reads the configured model from
// settings.json (or returns "" when settings.json doesn't have one).
// Cycle 81: wired into runLiveTUI so we can detect when the user's
// configured model differs from what the binary actually uses —
// that's the trigger for the warning block.
func authResolveSettingsModelOrEmpty() string {
	// We deliberately pass "" instead of a default. auth.ResolveSettingsModel
	// returns the fallback iff settings.json is missing/unreadable; we use the
	// empty string so a missing settings file silently yields "" (no warning).
	return strings.TrimSpace(auth.ResolveSettingsModel(""))
}

func isTerminalInteractive(f *os.File) bool {
	if f == nil {
		return false
	}
	// A char-device test alone treats /dev/null as a terminal (it is one),
	// which made an automated `--agent` run try bubbletea, fail on /dev/tty,
	// and lose the reference's fallback warning. IsTerminal is the actual
	// question the TUI launch and the warning split need.
	return term.IsTerminal(int(f.Fd()))
}

// reloadResumeList reloads the conversations from convstore and filters
// out the current active conversation ID (matching /resume behaviour).
func (m *runMode) reloadResumeList() {
	list, err := convstore.ListConversations()
	if err != nil {
		m.resumeList = nil
		return
	}
	if m.conversationID != "" {
		filtered := list[:0]
		for _, c := range list {
			if c.ID != m.conversationID {
				filtered = append(filtered, c)
			}
		}
		list = filtered
	}
	m.resumeList = list
}
