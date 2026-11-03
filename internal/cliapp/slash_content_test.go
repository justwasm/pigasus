package cliapp

// slash_content_test.go — cycle 102: the TUI slash commands whose content is
// recoverable now render the same content the print-mode handler emits, instead
// of the cycle-77 "(cycle 77: stub)" placeholders.

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/MichaelKinsy/PiG/ai"
	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/backend"
	confhome "github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/features/plugins/hooks"
	"github.com/justwasm/pigasus/internal/features/tools/isolation"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/ui/mdrender"
	"github.com/justwasm/pigasus/internal/ui/panelregistry"
	termcolor "github.com/justwasm/pigasus/internal/ui/render/color"
	"github.com/justwasm/pigasus/internal/ui/toolsteps"
	"github.com/justwasm/pigasus/internal/ui/transcript"
	"github.com/justwasm/pigasus/internal/ui/views/sidequestion"
	"github.com/justwasm/pigasus/internal/ui/views/slashquote"
	"github.com/justwasm/pigasus/internal/ui/views/voice"
	"github.com/justwasm/pigasus/palette"
)

func newSlashModel(t *testing.T) *runMode {
	t.Helper()
	origScheme, _ := palette.CurrentScheme()
	t.Cleanup(func() {
		palette.SetScheme(origScheme)
		mdrender.SetColorScheme(origScheme)
	})
	// Test runs use HOME=/Users/gear by default, where settings.json pins
	// "model": "MiniMax-M3" — make /model's effective-model lookup start
	// from a clean slate by pointing HOME at an empty tempdir. Only
	// override ANTIGRAVITY_HOME when the caller hasn't set it themselves;
	// some tests (e.g. TestSlashToggles_Persist) point ANTIGRAVITY_HOME at
	// a specific dir so they can write settings.json into it.
	if os.Getenv("ANTIGRAVITY_HOME") == "" {
		t.Setenv("ANTIGRAVITY_HOME", t.TempDir())
	}
	t.Setenv("HOME", t.TempDir())
	m := newRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
	m.flags = newStandardFlagSetForTest(t, nil)
	return m
}

// lastLines returns the transcript entries produced by the slash handler.
// Cycle 195: REF v1.4.0 echoes every slash command that produces a
// transcript message:
//
//	\x1b[38;2;38;139;210;1m> /<cmd>\x1b[0m
//	\x1b[38;2;38;139;210;1m  ⎿  \x1b[0m\x1b[38;2;88;110;117m<msg>\x1b[39m
//	<blank>
//	<additional handler rows for multi-line responses>
//	<blank>
//
// afterEcho strips the echo wrapper (`> /cmd` line + SGR/`⎿  ` prefix
// from the message line + trailing blank) and returns the handler rows
// the assertion cares about — either the single `<msg>` line for a
// 1-line response, or `<msg>` + extra rows for a multi-line response.
//
// The picker-mode /mcp call goes through slashContent (no echo wrapper)
// and is unaffected. /compact's empty message goes through
// slashRespondPlain (one styled row, no `⎿` wrapper) — handled by the
// fallback branch below.
//
// Cycle 149 previously asserted the v1.3.6 reference did not echo into
// the transcript. v1.4.0 reverses that. The picker overlay still shows
// the typed token; the echo lives in the transcript below it.
func afterEcho(m *runMode) []string {
	if len(m.response) == 0 {
		return m.response
	}
	// Detect the `> /<cmd>` echo header: cWarningBlueOpen + "> ".
	// If absent (e.g. slashContent or slashRespondPlain), return the
	// SGR-stripped rows as-is.
	if !strings.HasPrefix(m.response[0], cWarningBlueOpen+"> ") {
		out := make([]string, len(m.response))
		for i, r := range m.response {
			out[i] = termcolor.StripSGR(r)
		}
		return out
	}
	if len(m.response) < 2 {
		return nil
	}
	// Strip SGR + "  ⎿  " prefix from the message line.
	msgLine := termcolor.StripSGR(m.response[1])
	msgLine = strings.TrimPrefix(msgLine, "  \u23bf  ")
	out := []string{msgLine}
	// Append any extra rows between the `⎿` line and the trailing blank.
	// The blank line is always last; everything between is extra content
	// (e.g. /voice's 5-space-indented hint line).
	for _, r := range m.response[2:] {
		if r == "" {
			break
		}
		out = append(out, termcolor.StripSGR(r))
	}
	return out
}

func TestSlashContent_ModelAndEffort(t *testing.T) {
	m := newSlashModel(t)
	m.handleSlashCommand("/model")
	got := strings.Join(afterEcho(m), "\n")
	want := "gemini-3.8-flash-high  Gemini 3.8 Flash (High)"
	if got != want {
		t.Errorf("/model rendered %q, want %q", got, want)
	}

	m = newSlashModel(t)
	m.flags = newStandardFlagSetForTest(t, []string{"--model", "gemini-3.8-flash", "--effort", "low"})
	m.handleSlashCommand("/model")
	if got := strings.Join(afterEcho(m), "\n"); got != "gemini-3.8-flash-low  Gemini 3.8 Flash (Low)" {
		t.Errorf("/model with --effort rendered %q", got)
	}

	m = newSlashModel(t)
	m.handleSlashCommand("/effort")
	if got := strings.Join(afterEcho(m), "\n"); got != "high" {
		t.Errorf("/effort rendered %q, want \"high\"", got)
	}
}

func TestSlashContent_ConfigKeys(t *testing.T) {
	m := newSlashModel(t)
	m.handleSlashCommand("/config")
	got := strings.Join(afterEcho(m), "\n")
	lines := afterEcho(m)
	for _, key := range printmode.PrintSlashConfigKeys {
		found := false
		for _, ln := range lines {
			if ln == key || strings.HasPrefix(ln, key+" ") {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("/config output missing key %q\n%s", key, got)
		}
	}
	if strings.Contains(got, "stub") {
		t.Errorf("/config still renders a stub: %s", got)
	}
}

func TestSlashContent_SkillsPermissionsHooksAgents(t *testing.T) {
	m := newSlashModel(t)
	// Must not panic and must not render the old stub text.
	for _, cmd := range []string{"/skills", "/permissions"} {
		m = newSlashModel(t)
		m.handleSlashCommand(cmd)
		if got := strings.Join(afterEcho(m), "\n"); strings.Contains(got, "stub") {
			t.Errorf("%s still renders a stub: %s", cmd, got)
		}
	}
	// Nothing configured: the reference prints nothing.
	for _, cmd := range []string{"/hooks", "/agents"} {
		m = newSlashModel(t)
		m.handleSlashCommand(cmd)
		if got := afterEcho(m); len(got) != 0 {
			t.Errorf("%s should render nothing, got %q", cmd, got)
		}
	}
}

func TestSlashContent_ChangelogAndQuota(t *testing.T) {
	m := newSlashModel(t)
	m.handleSlashCommand("/changelog")
	got := strings.Join(afterEcho(m), "\n")
	if !strings.Contains(got, "1.3.") {
		head := got
		if len(head) > 80 {
			head = head[:80]
		}
		t.Errorf("/changelog output does not look like release notes: %q", head)
	}

	for _, tc := range []struct{ cmd, want string }{
		{"/usage", "error: /usage failed: retrieving quota summary: failed to retrieve user quota summary: no quota summary is available for this account"},
	} {
		m = newSlashModel(t)
		m.handleSlashCommand(tc.cmd)
		if got := strings.Join(afterEcho(m), "\n"); got != tc.want {
			t.Errorf("%s rendered %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

// TestSlashCreditsOpensPanel pins that `/credits` opens the credits panel
// instead of answering with an error line.
//
// 【一手】 agy 1.4.8, live: the command opens the panel on the picker path
// and on the direct path alike (the composer holding "/credits" with the
// picker dismissed).
func TestSlashCreditsOpensPanel(t *testing.T) {
	m := newSlashModel(t)
	handled, _ := m.handleSlashCommand("/credits")
	if !handled {
		t.Fatal("/credits was not handled")
	}
	if m.panel.kind != panelregistry.Credits {
		t.Fatalf("/credits opened panel %v, want panelregistry.Credits", m.panel.kind)
	}
}

// TestSlashNewStartsFreshConversation pins that /new is routed (cycle 77 never
// handled it, so it fell through to "Unknown command: /new").
func TestSlashNewStartsFreshConversation(t *testing.T) {
	m := newSlashModel(t)
	m.session = append(m.session, backend.Message{Role: "user", Content: "hi"})
	m.blocks = []transcript.Block{{Start: 0, Lines: 1}}
	m.liveSteps = []toolsteps.Step{{Kind: toolsteps.KindTool}}
	m.activeTool = "read_file"

	handled, cmd := m.handleSlashCommand("/new")
	if !handled {
		t.Fatal("/new should be handled")
	}
	if cmd != nil {
		t.Error("/new should not schedule a tea.Cmd")
	}
	if len(m.session) != 0 || len(m.blocks) != 0 || len(m.liveSteps) != 0 || m.activeTool != "" {
		t.Errorf("/new did not reset state: session=%d blocks=%d liveSteps=%d activeTool=%q",
			len(m.session), len(m.blocks), len(m.liveSteps), m.activeTool)
	}
	for _, line := range m.response {
		if strings.Contains(line, "Unknown command") {
			t.Errorf("/new reported as unknown: %v", m.response)
		}
	}
}

func TestForkConversationEmptySession(t *testing.T) {
	id, msg := convstore.ForkConversation(nil, "")
	if id != "" {
		t.Errorf("forkConversation empty session: id=%q, want \"\"", id)
	}
	if msg != "Error: no active conversation to fork" {
		t.Errorf("forkConversation empty session: msg=%q, want REF text", msg)
	}
	id, msg = convstore.ForkConversation([]backend.Message{}, "")
	if id != "" {
		t.Errorf("forkConversation empty slice: id=%q, want \"\"", id)
	}
	if msg != "Error: no active conversation to fork" {
		t.Errorf("forkConversation empty slice: msg=%q, want REF text", msg)
	}
}

func TestForkConversationActiveSession(t *testing.T) {
	sess := []backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}
	parentID := "11111111-2222-3333-4444-555555555555"
	id, msg := convstore.ForkConversation(sess, parentID)
	if id == "" {
		t.Fatalf("forkConversation active session: expected non-empty ID")
	}
	want := fmt.Sprintf("Forked conversation.\nTo go back, use: /resume %s", parentID)
	if msg != want {
		t.Errorf("forkConversation active session with parent: got %q, want %q", msg, want)
	}

	id2, msg2 := convstore.ForkConversation(sess, "")
	if id2 == "" {
		t.Fatalf("forkConversation active session (no parent): expected non-empty ID")
	}
	want2 := "Forked conversation."
	if msg2 != want2 {
		t.Errorf("forkConversation active session without parent: got %q, want %q", msg2, want2)
	}
}

func TestForkSlashCommandResponse(t *testing.T) {
	m := newRunMode(&fakeClient{}, "u@t", "m", "t", t.TempDir(), true)
	m.conversationID = "parent-conv-1234"
	m.session = []backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}
	_, _ = m.handleSlashCommand("/fork")
	if m.conversationID == "parent-conv-1234" || m.conversationID == "" {
		t.Fatalf("expected new conversation ID, got %q", m.conversationID)
	}
	joined := strings.Join(m.response, "\n")
	if !strings.Contains(joined, "> /fork") {
		t.Errorf("expected > /fork in response: %v", m.response)
	}
	if !strings.Contains(joined, "Forked conversation.") {
		t.Errorf("expected Forked conversation. in response: %v", m.response)
	}
	if !strings.Contains(joined, "To go back, use: /resume parent-conv-1234") {
		t.Errorf("expected To go back, use: /resume parent-conv-1234 in response: %v", m.response)
	}
}

func hasSGRBold(s string) bool {
	for i := 0; i < len(s); {
		if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '[' {
			j := i + 2
			for j < len(s) && s[j] != 'm' {
				j++
			}
			if j < len(s) {
				for _, p := range strings.Split(s[i+2:j], ";") {
					if p == "1" {
						return true
					}
				}
				i = j + 1
				continue
			}
		}
		i++
	}
	return false
}

// ---- cycle 235: /keybindings editor resolution tests -----------------------

// TestCycle235_NoEditorConfiguredError pins the headline regression:
// empty $EDITOR + empty $VISUAL produces the canonical "No editor
// configured" error and does NOT launch an editor process.
func TestCycle235_NoEditorConfiguredError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/keybindings edit")
	if cmd != nil {
		t.Errorf("/keybindings with no editor: cmd = %v, want nil", cmd)
	}

	got := strings.Join(m.response, "\n")
	if !strings.Contains(got, "No editor configured") {
		t.Errorf("/keybindings no-editor response missing error header: %q", got)
	}
	if !strings.Contains(got, "$EDITOR") || !strings.Contains(got, "$VISUAL") {
		t.Errorf("/keybindings no-editor response missing env-var names: %q", got)
	}

	if _, err := os.Stat(filepath.Join(dir, "keybindings.json")); err != nil {
		t.Errorf("/keybindings did not pre-create keybindings.json: %v", err)
	}
}

// TestCycle235_EditorSetUsesEditor pins the happy path: $EDITOR set
// → the returned cmd spawns the $EDITOR binary.
func TestCycle235_EditorSetUsesEditor(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	editorName := editorTestPath()
	t.Setenv("EDITOR", editorName+" -w")

	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/keybindings edit")
	_ = cmd

	got := strings.Join(m.response, "\n")
	if strings.Contains(got, "No editor configured") {
		t.Errorf("/keybindings with $EDITOR set still surfaced error: %q", got)
	}
}

// TestCycle235_VisualSetUsesVisual pins the second-precedence path:
// $VISUAL set, $EDITOR unset → resolveKeybindingsEditor picks $VISUAL.
func TestCycle235_VisualSetUsesVisual(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "/usr/bin/vi -e")

	m := newSlashModel(t)
	_, _ = m.handleSlashCommand("/keybindings edit")

	got := strings.Join(m.response, "\n")
	if strings.Contains(got, "No editor configured") {
		t.Errorf("/keybindings with $VISUAL set should not report error: %q", got)
	}
}

// TestCycle235_BothSetEditorWins pins the rule that $EDITOR overrides
// $VISUAL when both are set.
func TestCycle235_BothSetEditorWins(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	t.Setenv("EDITOR", editorTestPath())
	t.Setenv("VISUAL", "/usr/bin/vi -e")

	m := newSlashModel(t)
	_, _ = m.handleSlashCommand("/keybindings edit")

	got := strings.Join(m.response, "\n")
	if strings.Contains(got, "No editor configured") {
		t.Errorf("/keybindings with both env vars set should not report error: %q", got)
	}
}

// TestCycle235_ErrorMessageMentionsBothEnvVars pins the wording.
func TestCycle235_ErrorMessageMentionsBothEnvVars(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	m := newSlashModel(t)
	_, _ = m.handleSlashCommand("/keybindings edit")

	got := termcolor.StripSGR(strings.Join(m.response, "\n"))
	want := "No editor configured: set $EDITOR or $VISUAL"
	if !strings.Contains(got, want) {
		t.Errorf("/keybindings error = %q, want substring %q", got, want)
	}
}

// TestCycle235_BindingsTableRendersOnError pins the user-visible
// fallback: the editor verb's error path answers with the error alone.
//
// Cycle 384 (A-26): the error path used to render the shortcut table
// next to the error. The table was the clone's own block
// (tuilib.KeybindingsText) and it is gone with the report: the plain
// command reports the configured bindings, and the editor verb opens the
// file.
func TestCycle235_BindingsTableRendersOnError(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	m := newSlashModel(t)
	_, _ = m.handleSlashCommand("/keybindings edit")

	got := termcolor.StripSGR(strings.Join(m.response, "\n"))
	if !strings.Contains(got, "No editor configured") {
		t.Errorf("/keybindings edit error path did not render the error: %q", got)
	}
	if strings.Contains(got, "Keyboard shortcuts") {
		t.Errorf("/keybindings edit error path still renders the shortcut table: %q", got)
	}
}

// TestCycle235_HelperEnvVarPrecedence covers resolveKeybindingsEditor directly.
func TestCycle235_HelperEnvVarPrecedence(t *testing.T) {
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	if name, _, _, ok := resolveKeybindingsEditor(""); ok || name != "" {
		t.Errorf("all-empty: got (%q, true, %v), want (false)", name, ok)
	}
	if name, _, _, ok := resolveKeybindingsEditor("auto"); ok || name != "" {
		t.Errorf("auto with no env: got (%q, true, %v), want (false)", name, ok)
	}

	t.Setenv("EDITOR", "/usr/bin/vi -e")
	t.Setenv("VISUAL", "/usr/bin/nano")
	if name, args, source, ok := resolveKeybindingsEditor("code --wait"); !ok ||
		name != "code" || len(args) != 1 || args[0] != "--wait" ||
		source != "settings.editor" {
		t.Errorf("settings.editor wins: got (%q, %v, %q, %v)", name, args, source, ok)
	}

	if name, _, source, ok := resolveKeybindingsEditor(""); !ok ||
		name != "/usr/bin/vi" || source != "$EDITOR" {
		t.Errorf("$EDITOR wins: got (%q, _, %q, %v)", name, source, ok)
	}

	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "/usr/bin/nano -w")
	if name, args, source, ok := resolveKeybindingsEditor(""); !ok ||
		name != "/usr/bin/nano" || len(args) != 1 || args[0] != "-w" ||
		source != "$VISUAL" {
		t.Errorf("$VISUAL fallback: got (%q, %v, %q, %v)", name, args, source, ok)
	}

	t.Setenv("EDITOR", "/usr/bin/vi")
	t.Setenv("VISUAL", "")
	if name, _, source, ok := resolveKeybindingsEditor("auto"); !ok ||
		name != "/usr/bin/vi" || source != "$EDITOR" {
		t.Errorf("auto falls through to $EDITOR: got (%q, _, %q, %v)", name, source, ok)
	}

	t.Setenv("EDITOR", "   ")
	t.Setenv("VISUAL", "")
	if name, _, _, ok := resolveKeybindingsEditor(""); ok || name != "" {
		t.Errorf("whitespace-only $EDITOR: got (%q, _, _, %v)", name, ok)
	}
}

// editorTestPath returns a binary present on every supported Go target.
func editorTestPath() string {
	if runtime.GOOS == "windows" {
		return "cmd"
	}
	return "/usr/bin/env"
}

// seedSession writes the supplied messages into m.session verbatim.
// Test fixtures use this to put the runMode into a known prior-turn
// state without driving the full Enter-key path.
func seedSession(m *runMode, msgs []backend.Message) {
	m.session = append(m.session, msgs...)
}

// TestQuoteInsertsVerbatim: the prior turn's content goes into m.input
// exactly as it was typed — no leading or trailing whitespace trim,
// no escape sequences, no quote markers.
func TestQuoteInsertsVerbatim(t *testing.T) {
	m := newSlashModel(t)
	seedSession(m, []backend.Message{
		{Role: "user", Content: "line1"},
		{Role: "assistant", Content: "ok"},
	})

	if handled, _ := m.handleSlashCommand("/quote"); !handled {
		t.Fatal("/quote was not handled by the router")
	}
	if got := m.input.Value(); got != "line1" {
		t.Errorf("input after /quote = %q, want %q", got, "line1")
	}
}

// TestQuoteNoTrailingNewline is the direct pin on the AGY 1.2.13
// changelog entry. A single-line prior turn must NOT produce a
// trailing '\n' in the input — that was the bug, and the fix is the
// strings.TrimRight(content, "\n") in quoteInsertText.
func TestQuoteNoTrailingNewline(t *testing.T) {
	m := newSlashModel(t)
	seedSession(m, []backend.Message{
		{Role: "user", Content: "single line turn"},
		{Role: "assistant", Content: "ack"},
	})

	if handled, _ := m.handleSlashCommand("/quote"); !handled {
		t.Fatal("/quote was not handled")
	}
	got := m.input.Value()
	if strings.HasSuffix(got, "\n") {
		t.Errorf("input after /quote ends in '\\n' (the AGY 1.2.13 bug): %q", got)
	}
	if strings.HasSuffix(got, "\n\n") {
		t.Errorf("input after /quote ends in '\\n\\n' (would render as a blank line): %q", got)
	}
	// Belt-and-suspenders: count the trailing newlines explicitly.
	trail := len(got) - len(strings.TrimRight(got, "\n"))
	if trail != 0 {
		t.Errorf("input after /quote carries %d trailing '\\n' bytes: %q", trail, got)
	}
}

// TestQuotePreservesInternalNewlines: a multi-line prior turn lands
// in m.input with its internal line breaks intact. The TrimRight fix
// only strips trailing newlines, never internal ones.
func TestQuotePreservesInternalNewlines(t *testing.T) {
	m := newSlashModel(t)
	seedSession(m, []backend.Message{
		{Role: "user", Content: "first\nsecond\nthird"},
		{Role: "assistant", Content: "ok"},
	})

	if handled, _ := m.handleSlashCommand("/quote"); !handled {
		t.Fatal("/quote was not handled")
	}
	want := "first\nsecond\nthird"
	if got := m.input.Value(); got != want {
		t.Errorf("input after /quote = %q, want %q", got, want)
	}
	// Sanity: confirm no leading newline snuck in either.
	if strings.HasPrefix(m.input.Value(), "\n") {
		t.Errorf("input after /quote starts with '\\n': %q", m.input.Value())
	}
}

// TestQuoteEmptyIsNoOp: when there are no prior user turns in the
// session, /quote must NOT silently insert an empty string into the
// input — that would visually clear whatever the user had typed and
// would look identical to "the command did nothing". The reference's
// recoverable path is a slashRespond line; we mirror that and keep
// m.input untouched.
func TestQuoteEmptyIsNoOp(t *testing.T) {
	m := newSlashModel(t)
	// Intentionally seed nothing — empty session.
	pre := m.input.Value()
	if handled, _ := m.handleSlashCommand("/quote"); !handled {
		t.Fatal("/quote was not handled")
	}
	if got := m.input.Value(); got != pre {
		t.Errorf("input mutated by /quote on empty session: was %q, now %q", pre, got)
	}
	// The recoverable feedback lives in m.response after the echo
	// wrapper (`> /quote` + `⎿  <msg>`); confirm the body is the
	// recovered "No previous turn to quote." line.
	var found bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), "No previous turn to quote.") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("empty-session /quote did not echo 'No previous turn to quote.': %v", m.response)
	}
}

// TestQuoteMultiLineInsertedCorrectly: a three-line prior turn with
// no trailing newline in storage goes into m.input byte-exact. This
// is the more demanding version of TestQuotePreservesInternalNewlines
// (different fixtures, different length, pinned across two content
// rounds).
func TestQuoteMultiLineInsertedCorrectly(t *testing.T) {
	cases := []string{
		"alpha\nbeta\ngamma",
		"x\n\ny",       // blank line in the middle
		"one\ntwo",     // two lines, no trailing newline
		"trailing\n\n", // ends in \n\n — TrimRight strips both
		"trailing\n",   // single trailing \n — TrimRight strips it
	}
	for _, prior := range cases {
		m := newSlashModel(t)
		seedSession(m, []backend.Message{
			{Role: "user", Content: prior},
			{Role: "assistant", Content: "ack"},
		})
		if handled, _ := m.handleSlashCommand("/quote"); !handled {
			t.Fatalf("/quote was not handled (prior=%q)", prior)
		}
		want := strings.TrimRight(prior, "\n")
		if got := m.input.Value(); got != want {
			t.Errorf("input after /quote (prior=%q) = %q, want %q", prior, got, want)
		}
	}
}

// TestQuoteIndexedArg: /quote 2 picks the n-th previous user turn
// (1-based; 1 = most recent). The most-recent rule is what the
// reference binary implements — the user expects "the last turn I
// sent" by default.
func TestQuoteIndexedArg(t *testing.T) {
	m := newSlashModel(t)
	seedSession(m, []backend.Message{
		{Role: "user", Content: "first ever"},
		{Role: "assistant", Content: "ack"},
		{Role: "user", Content: "second"},
		{Role: "assistant", Content: "ack"},
		{Role: "user", Content: "third and latest"},
		{Role: "assistant", Content: "ack"},
	})

	// /quote (no args) → most recent
	if handled, _ := m.handleSlashCommand("/quote"); !handled {
		t.Fatal("/quote was not handled")
	}
	if got := m.input.Value(); got != "third and latest" {
		t.Errorf("/quote default = %q, want %q", got, "third and latest")
	}

	// /quote 2 → second most recent
	if handled, _ := m.handleSlashCommand("/quote 2"); !handled {
		t.Fatal("/quote 2 was not handled")
	}
	if got := m.input.Value(); got != "second" {
		t.Errorf("/quote 2 = %q, want %q", got, "second")
	}

	// /quote 3 → third most recent (oldest in this fixture)
	if handled, _ := m.handleSlashCommand("/quote 3"); !handled {
		t.Fatal("/quote 3 was not handled")
	}
	if got := m.input.Value(); got != "first ever" {
		t.Errorf("/quote 3 = %q, want %q", got, "first ever")
	}
}

// TestQuoteOutOfRange: when n exceeds the number of user turns, the
// helper returns a recoverable "out of range" message and m.input is
// not mutated.
func TestQuoteOutOfRange(t *testing.T) {
	m := newSlashModel(t)
	seedSession(m, []backend.Message{
		{Role: "user", Content: "only one"},
		{Role: "assistant", Content: "ack"},
	})
	pre := m.input.Value()
	if handled, _ := m.handleSlashCommand("/quote 5"); !handled {
		t.Fatal("/quote 5 was not handled")
	}
	if got := m.input.Value(); got != pre {
		t.Errorf("/quote 5 mutated input: was %q, now %q", pre, got)
	}
	var found bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), "Quote index out of range") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("/quote 5 did not echo 'Quote index out of range': %v", m.response)
	}
}

// TestQuoteBadArg: a non-numeric /quote argument is a recoverable
// usage error, not a panic and not a silent empty insertion.
func TestQuoteBadArg(t *testing.T) {
	m := newSlashModel(t)
	seedSession(m, []backend.Message{
		{Role: "user", Content: "stuff"},
		{Role: "assistant", Content: "ack"},
	})
	pre := m.input.Value()
	if handled, _ := m.handleSlashCommand("/quote abc"); !handled {
		t.Fatal("/quote abc was not handled")
	}
	if got := m.input.Value(); got != pre {
		t.Errorf("/quote abc mutated input: was %q, now %q", pre, got)
	}
	var found bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), "Usage: /quote [n]") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("/quote abc did not echo 'Usage: /quote [n]': %v", m.response)
	}
}

// TestResolveQuotePure: the helper's projection over a session slice
// is a pure function. These tests do not exercise the slash router —
// they pin resolveQuote's contract directly so a future refactor that
// re-routes /quote through openSlashPanelArgs (the cycle-185 panel
// pattern) still has a stable input/output to test against.
func TestResolveQuotePure(t *testing.T) {
	sess := []backend.Message{
		{Role: "user", Content: "first"},
		{Role: "assistant", Content: "a1"},
		{Role: "user", Content: "second\n\n"}, // trailing newlines get trimmed
		{Role: "assistant", Content: "a2"},
		{Role: "user", Content: "third"},
	}
	for _, tc := range []struct {
		name string
		args string
		want string
	}{
		{"default", "", "third"},
		{"explicit-1", "1", "third"},
		{"explicit-2", "2", "second"}, // "second\n\n" → "second"
		{"explicit-3", "3", "first"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got, msg := slashquote.ResolveQuote(tc.args, sess)
			if msg != "" {
				t.Errorf("resolveQuote(%q) returned error %q on a happy path", tc.args, msg)
			}
			if got != tc.want {
				t.Errorf("resolveQuote(%q) = %q, want %q", tc.args, got, tc.want)
			}
		})
	}
}

// TestQuoteInsertTextTrimsTrailingNewlines is the unit-level pin on
// the AGY 1.2.13 fix: quoteInsertText must strip trailing '\n' bytes
// while leaving internal '\n' bytes alone. This is the smallest possible
// reproduction of the bug — a regression here is a regression in the
// changelog claim.
func TestQuoteInsertTextTrimsTrailingNewlines(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"line1", "line1"},       // no trailing newline → unchanged
		{"line1\n", "line1"},     // one trailing newline → stripped
		{"line1\n\n", "line1"},   // two trailing newlines → both stripped
		{"a\nb", "a\nb"},         // internal newline preserved
		{"a\nb\n", "a\nb"},       // internal + trailing → trailing stripped
		{"\n", ""},               // only-newline input → empty
		{"", ""},                 // empty input → empty
		{"a\n\nb\n\n", "a\n\nb"}, // trailing pair stripped, internal pair preserved
	}
	for _, tc := range cases {
		got := slashquote.InsertText(tc.in)
		if got != tc.want {
			t.Errorf("quoteInsertText(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestRunGoal_SendsPrompt exercises the cycle-194 /goal handler.
// Confirms the slash token is echoed, m.streaming is flipped, and a
// user-role message is appended to m.session — the three signals a
// normal Enter-key submit also produces (see tui_run.go).
func TestRunGoal_SendsPrompt(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/goal ship the v2 feature")
	if cmd == nil {
		t.Fatal("/goal should return a non-nil tea.Cmd (the model stream)")
	}
	if !m.streaming {
		t.Error("/goal should set m.streaming=true while the model runs")
	}
	if len(m.response) == 0 || m.response[0] != "> /goal" {
		t.Errorf("/goal first echo = %q, want %q", firstNLines(m.response, 1), "> /goal")
	}
	if got := lastUserMessage(m); !strings.Contains(got, "goal") {
		t.Errorf("/goal session message lost the goal intent: %q", got)
	}
}

// TestRunGoal_NoArgsAlsoSendsPrompt confirms bare "/goal" still sends
// a prompt. The cycle-167 handler printed "  Goal cleared." for the
// empty-arg path; cycle 194 matches REF (which always generates).
func TestRunGoal_NoArgsAlsoSendsPrompt(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/goal")
	if cmd == nil {
		t.Fatal("bare /goal should return a non-nil tea.Cmd")
	}
	if !m.streaming {
		t.Error("bare /goal should set m.streaming=true")
	}
	if m.response[0] != "> /goal" {
		t.Errorf("bare /goal echo = %q, want %q", m.response[0], "> /goal")
	}
}

// TestRunPlan_SendsPrompt exercises the cycle-194 /plan handler. The
// cycle-167 implementation toggled the agent mode flag (now bound to
// Shift+Tab); cycle 194 sends a planning prompt to the model.
func TestRunPlan_SendsPrompt(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/plan")
	if cmd == nil {
		t.Fatal("/plan should return a non-nil tea.Cmd")
	}
	if !m.streaming {
		t.Error("/plan should set m.streaming=true")
	}
	if m.response[0] != "> /plan" {
		t.Errorf("/plan echo = %q, want %q", m.response[0], "> /plan")
	}
	if got := lastUserMessage(m); !strings.Contains(got, "plan") {
		t.Errorf("/plan session message lost the plan intent: %q", got)
	}
}

// TestRunLearn_SendsPrompt exercises the cycle-194 /learn handler.
// The cycle-167 stub appended to m.learnings and printed a fixed
// echo; cycle 194 sends a prompt.
func TestRunLearn_SendsPrompt(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/learn **API**: endpoint is *down*")
	if cmd == nil {
		t.Fatal("/learn should return a non-nil tea.Cmd")
	}
	if !m.streaming {
		t.Error("/learn should set m.streaming=true")
	}
	if m.response[0] != "> /learn" {
		t.Errorf("/learn echo = %q, want %q", m.response[0], "> /learn")
	}
	if got := lastUserMessage(m); !strings.Contains(got, "learn") {
		t.Errorf("/learn session message lost the learn intent: %q", got)
	}
}

// TestRunGrillMe_SendsPrompt exercises the cycle-194 /grill-me
// handler. Cycle 167 printed a stub "Interview mode enabled" line;
// cycle 194 sends a Socratic-style prompt.
func TestRunGrillMe_SendsPrompt(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/grill-me")
	if cmd == nil {
		t.Fatal("/grill-me should return a non-nil tea.Cmd")
	}
	if !m.streaming {
		t.Error("/grill-me should set m.streaming=true")
	}
	if m.response[0] != "> /grill-me" {
		t.Errorf("/grill-me echo = %q, want %q", m.response[0], "> /grill-me")
	}
	if got := lastUserMessage(m); !strings.Contains(got, "question") &&
		!strings.Contains(got, "reasoning") {
		t.Errorf("/grill-me session message lost the Socratic intent: %q", got)
	}
}

// TestRunTeamwork_SendsPrompt exercises the /teamwork-preview handler
// through its /teamwork alias. Confirms the alias reaches the command,
// that the echo carries the *canonical* name, that streaming starts and
// that a teamwork orchestration prompt is appended to the session.
//
// Cycle 383 (A-8): the reference's own command is "/teamwork-preview"
// (its changelog names the command) and "/teamwork" is the alias the
// documentation advertises for it.
func TestRunTeamwork_SendsPrompt(t *testing.T) {
	for _, typed := range []string{"/teamwork", "/teamwork-preview"} {
		m := newSlashModel(t)
		_, cmd := m.handleSlashCommand(typed + " build multi-agent system")
		if cmd == nil {
			t.Fatalf("%s should return a non-nil tea.Cmd", typed)
		}
		if !m.streaming {
			t.Errorf("%s should set m.streaming=true", typed)
		}
		if m.response[0] != "> /teamwork-preview" {
			t.Errorf("%s echo = %q, want %q", typed, m.response[0], "> /teamwork-preview")
		}
		if got := lastUserMessage(m); !strings.Contains(got, "team") &&
			!strings.Contains(got, "autonomous agents") {
			t.Errorf("%s session message lost the teamwork intent: %q", typed, got)
		}
	}
}

// TestRunBoost_SendsPrompt exercises the /boost handler.
// Confirms /boost echoes the token, sets streaming, and appends
// a boost multi-agent orchestrator prompt to the session.
func TestRunBoost_SendsPrompt(t *testing.T) {
	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/boost analyze architecture")
	if cmd == nil {
		t.Fatal("/boost should return a non-nil tea.Cmd")
	}
	if !m.streaming {
		t.Error("/boost should set m.streaming=true")
	}
	if m.response[0] != "> /boost" {
		t.Errorf("/boost echo = %q, want %q", m.response[0], "> /boost")
	}
	if got := lastUserMessage(m); !strings.Contains(got, "deep thinking") &&
		!strings.Contains(got, "verification") {
		t.Errorf("/boost session message lost the boost intent: %q", got)
	}
}

// TestRunKeybindings_CreatesFile verifies /keybindings edit creates
// ~/.gemini/antigravity-cli/keybindings.json with a JSON skeleton if
// the file does not exist. We redirect $ANTIGRAVITY_HOME so the test
// does not touch the real config dir.
//
// Cycle 384 (A-26): the editor path is the explicit "/keybindings edit"
// verb; the plain command reports the configured bindings and writes
// nothing.
func TestRunKeybindings_CreatesFile(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	// Cycle 235: explicitly clear $EDITOR / $VISUAL so the test is
	// deterministic across machines — the cycle-194 version inherited
	// the parent env and so depended on whatever the developer had
	// set. The cycle-235 fix surfaces a clear error in the empty case,
	// which is the assertion below.
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")

	m := newSlashModel(t)
	_, cmd := m.handleSlashCommand("/keybindings edit")

	path := filepath.Join(dir, "keybindings.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("/keybindings did not create %s: %v", path, err)
	}
	// Body is a JSON object skeleton. "{}" is what the cycle-194
	// implementation writes on a first run; any non-empty valid
	// JSON object is acceptable since the editor overwrites it.
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read keybindings.json: %v", err)
	}
	if !strings.Contains(string(body), "{") {
		t.Errorf("keybindings.json is not a JSON object: %q", body)
	}

	// Cycle 235: with no $EDITOR/$VISUAL the helper returns the
	// "No editor configured" error; we still don't launch an editor
	// process. The bindings table renders alongside the error so the
	// user has something useful to read; the regression-specific
	// tests in slash_cycle235_test.go own that contract.
	if cmd != nil {
		t.Errorf("/keybindings returned non-nil editor cmd with no $EDITOR/$VISUAL: %v", cmd)
	}
}

// TestRunKeybindings_NoCreateIfExists confirms /keybindings edit does not
// overwrite an existing keybindings.json. The test creates the file
// with a sentinel payload, runs /keybindings edit, and asserts the bytes
// are unchanged.
func TestRunKeybindings_NoCreateIfExists(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", dir)
	// Cycle 235: see TestRunKeybindings_CreatesFile — make the env
	// deterministic.
	t.Setenv("EDITOR", "")
	t.Setenv("VISUAL", "")
	path := filepath.Join(dir, "keybindings.json")
	sentinel := []byte(`{"sentinel": true}` + "\n")
	if err := os.WriteFile(path, sentinel, 0o644); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}

	m := newSlashModel(t)
	m.handleSlashCommand("/keybindings edit")

	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read keybindings.json: %v", err)
	}
	if string(got) != string(sentinel) {
		t.Errorf("/keybindings overwrote existing file: got %q, want %q", got, sentinel)
	}
}

// lastUserMessage returns the content of the most recent user-role
// message in m.session, or empty if there are none. The cycle-194
// handlers all append exactly one user message per slash invocation.
func lastUserMessage(m *runMode) string {
	for i := len(m.session) - 1; i >= 0; i-- {
		if m.session[i].Role == "user" {
			return m.session[i].Content
		}
	}
	return ""
}

// firstNLines is a tiny helper used in error messages to keep the
// transcript dump compact when only the leading row matters.
func firstNLines(rows []string, n int) string {
	if len(rows) < n {
		n = len(rows)
	}
	return strings.Join(rows[:n], "\n")
}

// Compile-time check: the cycle-194 handlers reference a few types
// from the agent / backend packages. If those imports become unused
// after a future refactor, `go build` will fail loudly; this guard
// documents the contract instead.
var (
	_ = confhome.Antigravity
	_ = backend.Message{Role: "user"}
)

// ---------------------------------------------------------------- voice & narration tests (from tui_voice_test.go)

func newVoiceTestMode(t *testing.T) *runMode {
	t.Helper()
	m := newSlashModel(t)
	m.width = 100
	m.height = 30
	return m
}

func TestVoice_PrintModeRejection(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	for _, cmd := range []string{"voice", "record"} {
		res := printmode.DispatchPrintSlash("/"+cmd, fs)
		if !res.Handled {
			t.Fatalf("dispatchPrintSlash(/%s) should be handled", cmd)
		}
		if res.Err == nil {
			t.Fatalf("dispatchPrintSlash(/%s) should return an error", cmd)
		}
		code, ok := cli.ExitCodeOf(res.Err)
		if !ok || code != 2 {
			t.Errorf("expected exit code 2, got %d (ok=%v)", code, ok)
		}
		want := "error: /" + cmd + " is not available in print mode (it starts interactive dictation and records from a microphone); pass --disable-slash-commands to send /" + cmd + " to the model as literal text"
		if strings.TrimSpace(res.Err.Error()) != want {
			t.Errorf("dispatchPrintSlash(/%s) = %q, want %q", cmd, res.Err.Error(), want)
		}
	}
}

func TestVoice_StartConnectingBanner(t *testing.T) {
	m := newVoiceTestMode(t)
	cmd := m.voiceSurface.Start("/voice")
	if cmd == nil {
		t.Fatal("Start returned nil cmd")
	}
	if !m.voiceSurface.Active() {
		t.Fatal("expected the voice surface to be active")
	}
	if m.voiceSurface.Phase() != voice.PhaseConnecting {
		t.Fatalf("expected phase == %d (Connecting), got %d", voice.PhaseConnecting, m.voiceSurface.Phase())
	}

	banner := m.voiceSurface.View()
	plain := termcolor.StripSGR(banner)
	if !strings.Contains(plain, "Connecting...") {
		t.Errorf("banner should contain 'Connecting...', got %q", plain)
	}
	if !strings.Contains(plain, "esc to cancel") {
		t.Errorf("banner should contain 'esc to cancel', got %q", plain)
	}
}

func TestVoice_EscCancelsConnecting(t *testing.T) {
	m := newVoiceTestMode(t)
	_ = m.voiceSurface.Start("/voice")

	handled, _ := m.voiceSurface.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatal("expected Esc to be handled by voice mode")
	}
	if m.voiceSurface.Active() {
		t.Fatal("expected the voice surface to be inactive after Esc")
	}
	if !m.voiceSurface.Cancelled() {
		t.Fatal("expected the session to be marked cancelled")
	}

	respLen := len(m.response)
	m.voiceSurface.HandleConnectDone(voice.ConnectDoneMsg{Err: errors.New("network failure")})
	if len(m.response) != respLen {
		t.Errorf("response should not be appended after cancellation; got %v", m.response)
	}
}

func TestVoice_FailedConnectionSurfacesPermissionDenied(t *testing.T) {
	m := newVoiceTestMode(t)
	_ = m.voiceSurface.Start("/voice")

	m.voiceSurface.HandleConnectDone(voice.ConnectDoneMsg{
		Err: errors.New("transcription failed: failed to receive setup response: rpc error: code = PermissionDenied desc = Request had insufficient authentication scopes."),
	})

	if m.voiceSurface.Active() {
		t.Fatal("the voice surface should be inactive after connection failure")
	}
	if len(m.response) < 2 {
		t.Fatalf("expected response rows for /voice failure, got %v", m.response)
	}
	msgText := strings.Join(m.response, "\n")
	if !strings.Contains(msgText, "Could not start transcription") {
		t.Errorf("response missing 'Could not start transcription': %s", msgText)
	}
	if !strings.Contains(msgText, "Voice dictation needs a permission your current sign-in does not carry") {
		t.Errorf("response missing hint text: %s", msgText)
	}
}

func TestVoice_RecordingBannerAndTimer(t *testing.T) {
	m := newVoiceTestMode(t)
	m.voiceConnectFn = func() error { return nil }
	_ = m.voiceSurface.Start("/voice")

	m.voiceSurface.HandleConnectDone(voice.ConnectDoneMsg{Err: nil})

	if !m.voiceSurface.Active() || m.voiceSurface.Phase() != voice.PhaseRecording {
		t.Fatalf("expected phase == %d (Recording), got %d", voice.PhaseRecording, m.voiceSurface.Phase())
	}

	banner := m.voiceSurface.View()
	plain := termcolor.StripSGR(banner)
	if !strings.Contains(plain, "Recording 00:00") {
		t.Errorf("banner missing 'Recording 00:00': %q", plain)
	}
	if !strings.Contains(plain, "enter or f5 to insert") {
		t.Errorf("banner missing 'enter or f5 to insert': %q", plain)
	}
	if !strings.Contains(plain, "esc to discard") {
		t.Errorf("banner missing 'esc to discard': %q", plain)
	}

	for i := 0; i < 5; i++ {
		m.voiceSurface.HandleTick()
	}
	banner5s := termcolor.StripSGR(m.voiceSurface.View())
	if !strings.Contains(banner5s, "Recording 00:05") {
		t.Errorf("banner missing 'Recording 00:05' after 5 ticks: %q", banner5s)
	}
}

func TestVoice_FinishingAfterMaxDuration(t *testing.T) {
	m := newVoiceTestMode(t)
	m.voiceConnectFn = func() error { return nil }
	_ = m.voiceSurface.Start("/voice")
	m.voiceSurface.HandleConnectDone(voice.ConnectDoneMsg{Err: nil})

	m.voiceSurface.Recorder().Tick(voice.MaxRecordingDuration)
	m.voiceSurface.HandleTick()

	if m.voiceSurface.Phase() != voice.PhaseFinishing {
		t.Fatalf("expected phase == %d (Finishing), got %d", voice.PhaseFinishing, m.voiceSurface.Phase())
	}

	banner := termcolor.StripSGR(m.voiceSurface.View())
	if !strings.Contains(banner, "Finishing up...") {
		t.Errorf("banner missing 'Finishing up...': %q", banner)
	}
	if !strings.Contains(banner, "enter or f5 to insert what you have") {
		t.Errorf("banner missing 'enter or f5 to insert what you have': %q", banner)
	}
}

func TestVoice_EnterOrF5InsertsDraft(t *testing.T) {
	m := newVoiceTestMode(t)
	m.voiceConnectFn = func() error { return nil }
	_ = m.voiceSurface.Start("f5")
	m.voiceSurface.HandleConnectDone(voice.ConnectDoneMsg{Err: nil})

	_ = m.voiceSurface.Recorder().AppendDraft("hello from voice dictation")

	handled, _ := m.voiceSurface.HandleKey(tea.Key{Code: tea.KeyF5})
	if !handled {
		t.Fatal("expected F5 to be handled")
	}
	if m.voiceSurface.Active() {
		t.Fatal("expected the voice surface to be inactive after confirmation")
	}

	got := m.input.Value()
	if got != "hello from voice dictation" {
		t.Errorf("input value = %q, want 'hello from voice dictation'", got)
	}
}

func TestVoice_F5KeyDispatchesVoiceDictation(t *testing.T) {
	m := newVoiceTestMode(t)
	if m.voiceSurface.Active() {
		t.Fatal("the voice surface should be inactive initially")
	}

	model, cmd := m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyF5}))
	updated := model.(*runMode)
	if !updated.voiceSurface.Active() {
		t.Fatal("pressing F5 should activate the voice surface")
	}
	if cmd == nil {
		t.Fatal("expected connect cmd returned by F5 press")
	}
}

func TestVoice_AntigravityMicSSHRemote(t *testing.T) {
	t.Setenv("ANTIGRAVITY_MIC", "127.0.0.1:54321")
	m := &runMode{}
	err := m.connectVoice()
	if err == nil || !strings.Contains(err.Error(), "cannot reach the mic server at 127.0.0.1:54321 (tcp)") {
		t.Fatalf("expected cannot reach mic server error, got: %v", err)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()

	t.Setenv("ANTIGRAVITY_MIC", ln.Addr().String())
	if err := m.connectVoice(); err != nil {
		t.Fatalf("expected nil error after reaching mic server, got: %v", err)
	}
}

func TestVoice_LiveRecordingFlow(t *testing.T) {
	m := newVoiceTestMode(t)
	t.Setenv("ANTIGRAVITY_MIC", "")

	cmd := m.voiceSurface.Start("/voice")
	if !m.voiceSurface.Active() || m.voiceSurface.Phase() != voice.PhaseConnecting {
		t.Fatalf("expected voicePhaseConnecting, got phase=%d", m.voiceSurface.Phase())
	}

	msg := cmd().(voice.ConnectDoneMsg)
	if msg.Err != nil {
		t.Fatalf("expected nil connect error, got: %v", msg.Err)
	}
	tickCmd := m.voiceSurface.HandleConnectDone(msg)
	if !m.voiceSurface.Active() || m.voiceSurface.Phase() != voice.PhaseRecording {
		t.Fatalf("expected voicePhaseRecording, got phase=%d", m.voiceSurface.Phase())
	}
	if tickCmd == nil {
		t.Fatal("expected non-nil tickCmd after entering recording")
	}

	banner := m.voiceSurface.View()
	if !strings.Contains(banner, "Recording 00:00") {
		t.Errorf("banner missing initial timer: %q", banner)
	}
	if !strings.Contains(banner, "enter or f5 to insert") {
		t.Errorf("banner missing instructions: %q", banner)
	}

	m.voiceSurface.HandleTick()
	bannerTick := m.voiceSurface.View()
	if !strings.Contains(bannerTick, "Recording 00:01") {
		t.Errorf("banner missing updated timer: %q", bannerTick)
	}

	handled, _ := m.voiceSurface.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled || m.voiceSurface.Active() {
		t.Fatal("expected Enter to confirm and leave the voice surface")
	}
}

// ---- cycle 250: /narration slash command tests -----------------------------

func narrationTestMode(t *testing.T, sessionStart time.Time) *runMode {
	t.Helper()
	m := newSlashModel(t)
	m.narrationStart = sessionStart
	return m
}

func TestSlashNarration_NoArgs(t *testing.T) {
	start := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	m := narrationTestMode(t, start)

	handled, cmd := m.handleSlashCommand("/narration")
	if !handled {
		t.Fatal("handleSlashCommand reported not handled")
	}
	if cmd != nil {
		t.Error("cmd should be nil — narration is purely synchronous")
	}
	if len(m.response) == 0 {
		t.Fatal("m.response is empty; expected a narration paragraph")
	}
	echoLines := afterEcho(m)
	if len(echoLines) == 0 {
		t.Fatal("afterEcho is empty — expected narration text")
	}
	got := strings.Join(echoLines, "\n")
	if len(got) < 30 {
		t.Errorf("narration text too short: %q", got)
	}
}

func TestSlashNarration_WithText(t *testing.T) {
	start := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	m := narrationTestMode(t, start)

	const custom = "Investigating the memory leak in the connection pool..."
	handled, _ := m.handleSlashCommand("/narration " + custom)
	if !handled {
		t.Fatal("handleSlashCommand not handled")
	}
	echoLines := afterEcho(m)
	if len(echoLines) == 0 {
		t.Fatal("afterEcho is empty")
	}
	got := strings.Join(echoLines, "\n")
	if !strings.Contains(got, custom) {
		t.Errorf("narration output = %q, want it to contain %q verbatim", got, custom)
	}
}

func TestSlashNarration_Off(t *testing.T) {
	start := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	m := narrationTestMode(t, start)

	handled, _ := m.handleSlashCommand("/narration off")
	if !handled {
		t.Fatal("handleSlashCommand not handled")
	}
	if !m.narrationOff {
		t.Error("m.narrationOff should be true after '/narration off'")
	}
	if len(m.response) != 0 {
		t.Errorf("m.response should be empty after '/narration off', got %v", m.response)
	}

	handled, _ = m.handleSlashCommand("/narration")
	if !handled {
		t.Fatal("subsequent /narration not handled")
	}
	if len(m.response) != 0 {
		t.Errorf("subsequent /narration should no-op when narrationOff=true, got %v", m.response)
	}
}

func TestSlashNarration_Auto(t *testing.T) {
	start := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	m := narrationTestMode(t, start)

	handled, _ := m.handleSlashCommand("/narration auto bug-fix")
	if !handled {
		t.Fatal("handleSlashCommand not handled")
	}
	echoLines := afterEcho(m)
	if len(echoLines) == 0 {
		t.Fatal("afterEcho is empty")
	}
	got := strings.Join(echoLines, "\n")
	if len(got) < 30 {
		t.Errorf("auto narration text too short: %q", got)
	}
}

func TestSlashNarration_DoesNotAffectSessionHistory(t *testing.T) {
	start := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	m := narrationTestMode(t, start)

	m.session = []backend.Message{
		{Role: "user", Content: "hello"},
		{Role: "assistant", Content: "world"},
	}
	initialLen := len(m.session)

	m.handleSlashCommand("/narration")
	if len(m.session) != initialLen {
		t.Errorf("session grew after bare /narration: had %d, now %d", initialLen, len(m.session))
	}

	m.handleSlashCommand("/narration customized narrative")
	if len(m.session) != initialLen {
		t.Errorf("session grew after /narration <text>: had %d, now %d", initialLen, len(m.session))
	}

	m.handleSlashCommand("/narration auto refactor")
	if len(m.session) != initialLen {
		t.Errorf("session grew after /narration auto: had %d, now %d", initialLen, len(m.session))
	}

	m.handleSlashCommand("/narration off")
	if len(m.session) != initialLen {
		t.Errorf("session grew after /narration off: had %d, now %d", initialLen, len(m.session))
	}
}

func TestSlashNarration_StableAcrossCalls(t *testing.T) {
	start := time.Date(2026, 4, 1, 10, 0, 0, 0, time.UTC)
	m1 := narrationTestMode(t, start)
	m1.handleSlashCommand("/narration")
	first := strings.Join(afterEcho(m1), "\n")

	m2 := narrationTestMode(t, start)
	m2.handleSlashCommand("/narration")
	second := strings.Join(afterEcho(m2), "\n")

	if first != second {
		t.Errorf("determinism violated for same sessionStart:\nfirst:  %q\nsecond: %q", first, second)
	}
}

// TestBranch_DoesNotModifyParentSkills runs /branch through the
// slash router and asserts the parent runMode's skill catalogue is
// untouched. The skill catalogue lives on disk; the sandbox-local
// registration table is separate, so adding a name to it must not
// create a phantom entry in slashSkillCatalog().
func TestBranch_DoesNotModifyParentSkills(t *testing.T) {
	m := newSlashModel(t)
	// Snapshot the parent's skill catalogue size.
	beforeSkills := len(slashSkillCatalog())
	// Run /branch. The helper inside the sandbox registers
	// "branch-isolate:helper" against the sandbox-local table.
	m.handleSlashCommand("/branch feature-x")
	// Parent's skill catalogue size is unchanged — the parent's
	// view of skills comes from disk, not from the sandbox-local
	// map.
	if got := len(slashSkillCatalog()); got != beforeSkills {
		t.Errorf("after /branch, parent skill catalogue size = %d, want %d (must not leak)",
			got, beforeSkills)
	}
	// Verify the sandbox-local registration table is empty after
	// the command returns. We use a fresh workspace to assert
	// this in isolation rather than reach into m's internals —
	// the parent runMode has no fields that map directly to
	// isolatedWorkspace.registeredSkills.
	iw := isolation.New()
	iw.Capture("/parent/workspace", map[string]string{})
	iw.Run(func(sandbox *isolation.Workspace) {
		sandbox.RegisterSkill("branch-isolate:helper")
	})
	if skills := iw.RegisteredSkills(); len(skills) != 0 {
		t.Errorf("post-Exit sandbox skills = %v, want empty (must not leak)", skills)
	}
	// /branch's output is captured into m.response. The captured
	// transcript must include the four feedback lines.
	resp := strings.Join(afterEcho(m), "\n")
	for _, want := range []string{
		"Branched workspace: /",
		"Sandbox branch: feature-x",
		"Isolated copy: would run in a fresh git worktree",
	} {
		if !strings.Contains(resp, want) {
			t.Errorf("/branch transcript missing %q; full: %s", want, resp)
		}
	}
}

// TestShare_DoesNotModifyParentAgents mirrors
// TestBranch_DoesNotModifyParentSkills for /share. The parent
// session's subagent catalogue (defaultRouter in at_subagent.go) is
// unchanged after /share runs.
func TestShare_DoesNotModifyParentAgents(t *testing.T) {
	m := newSlashModel(t)
	// Snapshot parent agent router size.
	beforeAgents := 0
	if m.atRouter != nil {
		beforeAgents = len(m.atRouter.Names())
	}
	m.handleSlashCommand("/share team-collaboration")
	// Parent's agent router size is unchanged.
	if m.atRouter != nil {
		if got := len(m.atRouter.Names()); got != beforeAgents {
			t.Errorf("after /share, parent agent router size = %d, want %d (must not leak)",
				got, beforeAgents)
		}
	}
	// Verify the sandbox-local registration table is empty after
	// the command returns.
	iw := isolation.New()
	iw.Capture("/parent/workspace", map[string]string{})
	iw.Run(func(sandbox *isolation.Workspace) {
		sandbox.RegisterAgent("branch-isolate:scout")
	})
	if agents := iw.RegisteredAgents(); len(agents) != 0 {
		t.Errorf("post-Exit sandbox agents = %v, want empty (must not leak)", agents)
	}
	// /share's output is captured into m.response. The captured
	// transcript must include the four feedback lines, distinct
	// from /branch's wording.
	resp := strings.Join(afterEcho(m), "\n")
	for _, want := range []string{
		"Shared workspace: /",
		"Sandbox share: team-collaboration",
		"Isolated copy: read-only view of parent env",
	} {
		if !strings.Contains(resp, want) {
			t.Errorf("/share transcript missing %q; full: %s", want, resp)
		}
	}
	// /share's first line must differ from /branch's first line —
	// the changelog promise is that /branch and /share are
	// distinguishable in the transcript.
	branchResp := strings.Join(func() []string {
		mm := newSlashModel(t)
		mm.handleSlashCommand("/branch team-collaboration")
		return afterEcho(mm)
	}(), "\n")
	if strings.HasPrefix(resp, branchResp) {
		t.Errorf("/share transcript should differ from /branch; got %q", resp)
	}
}

// TestBranch_OutputCapturedIntoParent asserts that /branch's
// output is captured and displayed in the parent session's
// transcript. The captured lines flow through the slashRespond
// wrapper (the `> /branch` echo + ⎿ wrapper) and append below it.
func TestBranch_OutputCapturedIntoParent(t *testing.T) {
	m := newSlashModel(t)
	if handled, _ := m.handleSlashCommand("/branch captured"); !handled {
		t.Fatal("/branch should be handled by the router")
	}
	// m.response must include the echo header `> /branch`.
	if len(m.response) == 0 || !strings.HasPrefix(m.response[0], cWarningBlueOpen+"> /branch") {
		t.Errorf("/branch echo header missing; m.response[0] = %q", firstSliceLine(m.response))
	}
	// The captured output appears below the echo wrapper.
	lines := afterEcho(m)
	if len(lines) < 4 {
		t.Fatalf("/branch produced %d transcript lines, want >= 4 (the four feedback lines)", len(lines))
	}
	want := []string{
		"Branched workspace: /",
		"Sandbox branch: captured",
		"Isolated copy: would run in a fresh git worktree",
		"State changes (cwd, env, registrations) will not affect parent session.",
	}
	for i, w := range want {
		if i >= len(lines) {
			t.Errorf("missing line %d (%q)", i, w)
			continue
		}
		if lines[i] != w {
			t.Errorf("line %d = %q, want %q", i, lines[i], w)
		}
	}
	// Parent's session / blocks / liveSteps are untouched.
	if len(m.session) != 0 {
		t.Errorf("/branch should not modify parent session; got %d messages", len(m.session))
	}
	if len(m.blocks) != 0 {
		t.Errorf("/branch should not modify parent blocks; got %d entries", len(m.blocks))
	}
}

// TestShare_OutputCapturedIntoParent is the share-side
// counterpart of TestBranch_OutputCapturedIntoParent. The echo
// wrapper is identical (`> /share`), but the captured lines below it
// use /share's wording.
func TestShare_OutputCapturedIntoParent(t *testing.T) {
	m := newSlashModel(t)
	if handled, _ := m.handleSlashCommand("/share captured"); !handled {
		t.Fatal("/share should be handled by the router")
	}
	if len(m.response) == 0 || !strings.HasPrefix(m.response[0], cWarningBlueOpen+"> /share") {
		t.Errorf("/share echo header missing; m.response[0] = %q", firstSliceLine(m.response))
	}
	lines := afterEcho(m)
	if len(lines) < 4 {
		t.Fatalf("/share produced %d transcript lines, want >= 4 (the four feedback lines)", len(lines))
	}
	want := []string{
		"Shared workspace: /",
		"Sandbox share: captured",
		"Isolated copy: read-only view of parent env, no side-effects.",
		"State changes (cwd, env, registrations) will not affect parent session.",
	}
	for i, w := range want {
		if i >= len(lines) {
			t.Errorf("missing line %d (%q)", i, w)
			continue
		}
		if lines[i] != w {
			t.Errorf("line %d = %q, want %q", i, lines[i], w)
		}
	}
	// Bare /share with no args: the default share-isolate name
	// must appear in the captured output.
	m2 := newSlashModel(t)
	m2.handleSlashCommand("/share")
	share2 := strings.Join(afterEcho(m2), "\n")
	if !strings.Contains(share2, "Sandbox share: share-isolate") {
		t.Errorf("/share default name missing; got %q", share2)
	}
}

// firstSliceLine returns the first non-empty entry of s, or "" if
// all entries are empty. Named to avoid colliding with tui_run.go's
// firstLine helper, which has a different signature. Used to render
// readable error messages when m.response[0] is the empty string.
func firstSliceLine(s []string) string {
	for _, l := range s {
		if l != "" {
			return l
		}
	}
	return ""
}

// csTmpTree builds a self-contained workspace under t.TempDir() with the
// files the assertions below need. Returns the root path.
func csTmpTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	must := func(rel, body string) {
		path := filepath.Join(root, rel)
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	must("a.txt", "line1\nline2 needle here\nline4\nline5\nneedle again\n")
	must("d.txt", "NEEDLE upper\n")
	must("dbl.txt", "needle needle here\n")
	must("end.txt", "aaa zwk\n")
	must("sub/c.txt", "alpha zwk beta\nneedle\n")
	// A long line whose match sits past the 216-cell content budget.
	must("long.txt", strings.Repeat("P", 300)+" zwk "+strings.Repeat("Q", 300)+"\n")
	return root
}

// TestCycle213PanelLinesUsage verifies that the panel still renders the
// usage form when openSlashPanelArgs is called with an empty argument
// tail — this is the cycle-206-equivalent contract for bare /codesearch.
func TestCycle213PanelLinesUsage(t *testing.T) {
	m := mkPanel(panelregistry.CodeSearch, "/codesearch", 0)
	plain := plainLines(m)
	// Usage row must contain "/codesearch [-F|--literal] <query>".
	found := false
	for _, l := range plain {
		if strings.Contains(l, "Usage: /codesearch") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("usage row missing; got %q", plain)
	}
}

// TestCycle213PanelLinesResultList: openSlashPanelArgs("codesearch",
// "needle") must populate cs.hits and produce a panel whose plain row
// contains the cycle-228 "Found N matches in K files." header (cycle 213
// only knew the total count; cycle 228 also tracks the file count
// recovered from the AGY 1.4.0 changelog).
func TestCycle213PanelLinesResultList(t *testing.T) {
	root := csTmpTree(t)
	m := newRunMode(nil, "u@t", "Gemini 3.8 Flash (High)", "t", root, false)
	if !m.openSlashPanelArgs("codesearch", "needle") {
		t.Fatal("openSlashPanelArgs(codesearch, needle) = false")
	}
	if m.codesearchPanel().Query() != "needle" {
		t.Errorf("query = %q, want needle", m.codesearchPanel().Query())
	}
	if hits := m.codesearchPanel().Hits(); len(hits) != 5 {
		// csTmpTree: a.txt × 2, d.txt (smart-case), dbl.txt, sub/c.txt
		t.Errorf("hits = %d, want 5; raw=%+v", len(hits), hits)
	}
	plain := plainLines(m)

	wantSubstrings := []string{
		"Code Search: needle",
		"Found 5",     // cycle 228 says "Found 5 matches in 4 files."
		"in 4 files.", // 4 distinct files: a.txt, d.txt, dbl.txt, sub/c.txt
		"a.txt", "d.txt", "dbl.txt", "sub/c.txt",
		"L2: ",
		// Counter may or may not appear depending on whether the list
		// overflowed the budget; just confirm we got the legend.
		"Keyboard: ",
	}
	for _, w := range wantSubstrings {
		found := false
		for _, l := range plain {
			if strings.Contains(l, w) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("panel row missing %q in %q", w, plain)
		}
	}
}

// TestCycle213PanelLinesNoResults and PatternError lock the empty-list
// fallback forms.
func TestCycle213PanelLinesNoResults(t *testing.T) {
	m := newRunMode(nil, "u@t", "m", "t", t.TempDir(), false)
	m.openSlashPanelArgs("codesearch", "nevergonnamatch")
	if hits := m.codesearchPanel().Hits(); len(hits) != 0 {
		t.Fatalf("hits = %d, want 0", len(hits))
	}
	plain := plainLines(m)
	found := false
	for _, l := range plain {
		if strings.Contains(l, "No results") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected `No results` row in %q", plain)
	}

	// Pattern error: unbalanced bracket yields a user-facing message.
	m2 := newRunMode(nil, "u@t", "m", "t", t.TempDir(), false)
	m2.openSlashPanelArgs("codesearch", "(unclosed")
	if m2.codesearchPanel().Err() == "" {
		t.Error("expected the panel to carry a pattern error")
	}
	plain = plainLines(m2)
	found = false
	for _, l := range plain {
		if strings.Contains(l, "Error") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected `Error:` row in %q", plain)
	}
}

// TestCycle213NonCodesearchArgsPanelsKeepsExisting confirms the
// openSlashPanelArgs refactor did not change any non-/codesearch branch:
// every other panel command still renders its old content.
func TestCycle213NonCodesearchArgsPanelsKeepsExisting(t *testing.T) {
	for _, cmd := range []string{"mcp", "tasks", "context"} {
		m := &runMode{}
		if !m.openSlashPanelArgs(cmd, "withargs") {
			t.Errorf("openSlashPanelArgs(%q, ...) = false", cmd)
			continue
		}
		// The arg tail is not echoed by these panels — verify it isn't
		// accidentally attached anywhere they don't expect it.
		plain := plainLines(m)
		for _, l := range plain {
			if strings.Contains(l, "withargs") {
				t.Errorf("%s panel rendered the argument tail %q; should drop it", cmd, l)
			}
		}
	}
}

// TestSlashCodesearchNoArgs: typing `> /codesearch` echoes
// `⎿  /codesearch needs a query.` — the panel does NOT open.
func TestSlashCodesearchNoArgs(t *testing.T) {
	m := newSlashModel(t)
	if handled, cmd := m.handleSlashCommand("/codesearch"); !handled || cmd != nil {
		t.Errorf("handleSlashCommand(/codesearch) = (handled=%v cmd=%v), want handled=true cmd=nil",
			handled, cmd)
	}
	if m.panel.kind == panelregistry.CodeSearch {
		t.Errorf("bare /codesearch should not open the panel; panel.kind = %v", m.panel.kind)
	}
	joined := strings.Join(afterEcho(m), "\n")
	if !strings.Contains(joined, "/codesearch needs a query.") {
		t.Errorf("bare /codesearch missing needs-query echo; got %q", joined)
	}
}

// TestSlashCodesearchAliases: /cs and /search route through the same
// handler as /codesearch (the cycle-206 alias mapping, now owned by
// internal/ui/slashcommands).
func TestSlashCodesearchAliases(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "a.go"),
		[]byte("// TODO\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	for _, alias := range []string{"cs", "search"} {
		t.Run(alias, func(t *testing.T) {
			m := newSlashModel(t)
			m.cwd = root
			// Disable streaming to make the panel settle synchronously.
			m.streamingSearch = false

			if !m.openSlashPanelArgs(alias, "TODO") {
				t.Fatalf("openSlashPanelArgs(%q, TODO) = false", alias)
			}
			if m.panel.kind != panelregistry.CodeSearch {
				t.Errorf("panel.kind = %v, want panelregistry.CodeSearch", m.panel.kind)
			}
			if q := m.codesearchPanel().Query(); q != "TODO" {
				t.Errorf("query = %q, want TODO", q)
			}
		})
	}
}

// withBtwManager injects a side-question Manager backed by PiG's
// faux provider. Returns a cleanup func that restores the previous
// factory; tests call defer withBtwManager(t)(...).
func withBtwManager(t *testing.T) func() {
	t.Helper()
	prev := newBtwManager
	newBtwManager = func(opts sidequestion.Options) *sidequestion.Manager {
		opts.Model = &ai.Model{
			Type:         ai.ModelTypeChat,
			ID:           "faux-1",
			DisplayName:  "faux-1",
			Provider:     &ai.TestFauxProvider{},
			ProviderMeta: ai.ProviderMetadata{ProviderID: "test-faux", API: "test-faux"},
			Input:        []string{"text"},
		}
		return sidequestion.NewManager(opts)
	}
	return func() { newBtwManager = prev }
}

// stripColor removes ANSI escape sequences from a transcript row so
// assertions can match on plain text. The transcript renderer mixes
// SGR escapes (the slashRespond header uses cWarningBlueOpen +
// panelGray238); stripColor matches the helper slash_content_test.go
// already exports via the parent package's other tests.
func stripColor(s string) string {
	var b strings.Builder
	in := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			in = true
		case in && r == 'm':
			in = false
		case !in:
			b.WriteRune(r)
		}
	}
	return b.String()
}

// TestBtwSlashNoArgs: typing `/btw` with no question and no history must echo the
// authentic "Usage: /btw <question>" line and never open a Session.
func TestBtwSlashNoArgs(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	if handled, _ := m.handleSlashCommand("/btw"); !handled {
		t.Fatal("/btw not handled")
	}
	if len(m.btwCards) != 0 {
		t.Errorf("no-args /btw opened %d cards", len(m.btwCards))
	}
	// The transcript carries the echo + "Usage: /btw <question>" line;
	// strip the ANSI wrapper and assert on the message body.
	var found bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), "Usage: /btw <question>") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("/btw (no args) transcript missing 'Usage: /btw <question>' line: %v", m.response)
	}
}

// TestBtwSlashStreamingCard: /btw <question> spawns a coding.Session,
// sends the question, and the rendered Card has non-empty Answer +
// Tokens > 0. PiG's faux provider yields "42" for "What is 20+22?";
// the test asks a question that triggers the over-window branch so
// Tokens is non-zero (the basic text branch leaves Usage nil).
func TestBtwSlashStreamingCard(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	if handled, _ := m.handleSlashCommand("/btw Trigger: over-window response"); !handled {
		t.Fatal("/btw not handled")
	}
	if len(m.btwCards) != 1 {
		t.Fatalf("expected 1 card, got %d", len(m.btwCards))
	}
	card := m.btwCards[0]
	_, answer, tokens, state, err := card.LockedSnapshot()
	if err != nil {
		t.Fatalf("Card.Err: %v", err)
	}
	if state != sidequestion.StateDone {
		t.Errorf("Card.State = %q, want done", state)
	}
	if answer == "" {
		t.Error("Card.Answer is empty")
	}
	if tokens <= 0 {
		t.Errorf("Card.Tokens = %d, want > 0", tokens)
	}
	// The active id pointer advanced to the new card.
	if m.activeBtwID != card.ID {
		t.Errorf("activeBtwID = %q, want %q", m.activeBtwID, card.ID)
	}
}

// TestBtwParentTranscriptUntouched: after a /btw round-trip the
// parent's m.session records the side question as a user message.
// Cycle btw-runs-hooks inverts cycle-230's "session untouched"
// contract so /btw shows up in the conversation history like any
// other user input — see slash_btw.go's appendBtwToSession helper
// and the cycle btw-runs-hooks deliverable for the rationale.
func TestBtwParentTranscriptUntouched(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	parent := []backend.Message{
		{Role: "user", Content: "parent question"},
		{Role: "assistant", Content: "parent answer"},
	}
	m.session = append([]backend.Message(nil), parent...)

	if handled, _ := m.handleSlashCommand("/btw What is 20+22?"); !handled {
		t.Fatal("/btw not handled")
	}

	// The two parent messages stay intact and a new user message
	// lands at the end with a "> /btw <question>" body so future
	// turns can replay the side question in context.
	if len(m.session) != len(parent)+1 {
		t.Fatalf("parent session grew from %d to %d messages, want %d", len(parent), len(m.session), len(parent)+1)
	}
	for i, msg := range parent {
		if m.session[i].Role != msg.Role || m.session[i].Content != msg.Content {
			t.Errorf("parent session[%d] = %+v, want %+v", i, m.session[i], msg)
		}
	}
	last := m.session[len(m.session)-1]
	if last.Role != "user" {
		t.Errorf("appended role = %q, want user", last.Role)
	}
	if !strings.Contains(last.Content, "What is 20+22?") {
		t.Errorf("appended content = %q, want it to contain the side question", last.Content)
	}
	if !strings.HasPrefix(last.Content, "> /btw ") {
		t.Errorf("appended content = %q, want it to start with \"> /btw \"", last.Content)
	}
}

// TestBtwErrorPath: a session that fails mid-stream leaves the
// transcript with `⎿  btw failed: <err>` and no body rows.
//
// The faux provider returns a clean stop on most requests; to force
// an error path we directly call the manager with a question that
// triggers the "test-faux: unsupported scenario" branch — that
// branch is what every non-"parity-basic" Scenario raises.
func TestBtwErrorPath(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	// Set the Scenario so the next Stream call fails with an
	// unsupported-scenario error.
	t.Setenv("PIG_TEST_FAUX_SCENARIO", "not-a-real-scenario")

	m := newSlashModel(t)
	if handled, _ := m.handleSlashCommand("/btw anything"); !handled {
		t.Fatal("/btw not handled")
	}
	// The transcript must carry `btw failed: <err>` even though no
	// card was registered.
	if len(m.btwCards) != 0 {
		t.Errorf("error path registered %d cards; want 0", len(m.btwCards))
	}
	var found bool
	for _, row := range m.response {
		if strings.HasPrefix(stripColor(row), "btw failed: ") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("btw failed line missing from transcript: %v", m.response)
	}
}

// TestCopyBtwSlash: /copy btw with a visible card copies the active
// card's text; without a card the echo is "No active btw response
// to copy." The clipboard mock is swapped so the test does not
// touch the real system clipboard.
func TestCopyBtwSlash(t *testing.T) {
	orig := clipboardWrite
	defer func() { clipboardWrite = orig }()

	cleanup := withBtwManager(t)
	defer cleanup()

	// No card yet → "No /btw response to copy."
	var copied string
	clipboardWrite = func(s string) error { copied = s; return nil }
	m := newSlashModel(t)
	m.handleSlashCommand("/copy btw")
	if copied != "" {
		t.Errorf("clipboard written with no active card: %q", copied)
	}
	var noActive bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), "No /btw response to copy.") {
			noActive = true
			break
		}
	}
	if !noActive {
		t.Errorf("/copy btw with no card missed the no-active echo: %v", m.response)
	}

	// Add a card via /btw, then /copy btw again.
	clipboardWrite = func(s string) error { copied = s; return nil }
	m = newSlashModel(t)
	m.handleSlashCommand("/btw What is 20+22?")
	card := m.btwCards[0]
	m.handleSlashCommand("/copy btw")
	if copied != "42" {
		t.Errorf("clipboard got %q, want %q", copied, "42")
	}
	var copiedMsg bool
	for _, row := range m.response {
		plain := stripColor(row)
		if strings.Contains(plain, "Copied /btw answer to clipboard.") {
			copiedMsg = true
			break
		}
	}
	if !copiedMsg {
		t.Errorf("/copy btw echo missing 'Copied /btw answer to clipboard.': %v", m.response)
	}
	_ = card
}

// TestCopyBtwClipboardError: when the clipboard backend fails, the
// /copy btw echo carries the error and never panics.
func TestCopyBtwClipboardError(t *testing.T) {
	orig := clipboardWrite
	defer func() { clipboardWrite = orig }()

	cleanup := withBtwManager(t)
	defer cleanup()

	clipboardWrite = func(string) error { return os.ErrPermission }

	m := newSlashModel(t)
	m.handleSlashCommand("/btw What is 20+22?")
	m.handleSlashCommand("/copy btw")

	var found bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), "Failed to copy to clipboard: ") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("/copy btw clipboard error echo missing: %v", m.response)
	}
}

// TestBtwIndependentServices: opening a btw card does not mutate
// anything on the runMode outside the btwManager / btwCards /
// session fields. After a /btw round-trip the parent session grows
// by exactly one user message (the side-question record added by
// appendBtwToSession); closing the runMode's main transport
// (m.backend) does not retroactively affect the card. PiG's
// coding.Services per-Ask isolation guarantees this; the test pins
// the contract so a future cycle that decides to share a Services
// cannot regress without noticing.
func TestBtwIndependentServices(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	parent := []backend.Message{{Role: "user", Content: "p"}}
	m.session = append([]backend.Message(nil), parent...)
	if handled, _ := m.handleSlashCommand("/btw What is 20+22?"); !handled {
		t.Fatal("/btw not handled")
	}
	if len(m.session) != len(parent)+1 {
		t.Errorf("parent session length = %d, want %d", len(m.session), len(parent)+1)
	}
	if len(m.btwCards) != 1 {
		t.Errorf("expected 1 btw card, got %d", len(m.btwCards))
	}
}

// ---- cycle btw-runs-hooks: lifecycle-hook dispatch -----------------------
//
// The next six tests are the cycle btw-runs-hooks deliverable for
// AGY 1.3.3 line 49: /btw now fires the registered hooks. The
// production Manager is constructed by newRunMode (see tui_run.go);
// the tests overwrite m.hooks after construction so each case has
// a private registry without leaking handlers between tests.

// TestBtwFiresPreSideQuestionHook: /btw dispatches the
// PreSideQuestion event before opening the side-question Session.
// A handler that returns Allow is enough — the side question still
// runs and the hook saw the typed question text.
func TestBtwFiresPreSideQuestionHook(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	called := 0
	var seenEvent hooks.Event
	m.hooks.Register(hooks.PreSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		called++
		seenEvent = p.Event
		return hooks.DecisionAllow, "", nil
	})

	if handled, _ := m.handleSlashCommand("/btw What is 20+22?"); !handled {
		t.Fatal("/btw not handled")
	}
	if called != 1 {
		t.Errorf("PreSideQuestion fired %d times; want 1", called)
	}
	if seenEvent != hooks.PreSideQuestion {
		t.Errorf("payload event = %q, want %q", seenEvent, hooks.PreSideQuestion)
	}
}

// TestBtwFiresPostSideQuestionHook: /btw dispatches the
// PostSideQuestion event after the side-question Session settles
// (success path). The handler sees the streamed answer and the
// token total in the payload — same fields the TUI renders.
func TestBtwFiresPostSideQuestionHook(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	called := 0
	var seenAnswer string
	var seenTokens int
	m.hooks.Register(hooks.PostSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		called++
		seenAnswer = p.Answer
		seenTokens = p.Tokens
		return hooks.DecisionAllow, "", nil
	})

	if handled, _ := m.handleSlashCommand("/btw Trigger: over-window response"); !handled {
		t.Fatal("/btw not handled")
	}
	if called != 1 {
		t.Errorf("PostSideQuestion fired %d times; want 1", called)
	}
	if seenAnswer == "" {
		t.Errorf("PostSideQuestion payload Answer was empty; want the streamed answer")
	}
	if seenTokens <= 0 {
		t.Errorf("PostSideQuestion payload Tokens = %d; want > 0", seenTokens)
	}
}

// TestBtwHookCanBlock: a PreSideQuestion handler that returns
// DecisionBlock aborts the side question entirely — no card is
// registered, the transcript carries the block reason, and the
// post-event still fires so logging handlers can react.
func TestBtwHookCanBlock(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	preCalls := 0
	postCalls := 0
	var postBlocked bool
	var postReason string
	m.hooks.Register(hooks.PreSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		preCalls++
		return hooks.DecisionBlock, "policy: denied", nil
	})
	m.hooks.Register(hooks.PostSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		postCalls++
		postBlocked = p.Blocked
		postReason = p.BlockReason
		return hooks.DecisionAllow, "", nil
	})

	if handled, _ := m.handleSlashCommand("/btw What is 20+22?"); !handled {
		t.Fatal("/btw not handled")
	}
	if preCalls != 1 {
		t.Errorf("PreSideQuestion fired %d times; want 1", preCalls)
	}
	if postCalls != 1 {
		t.Errorf("PostSideQuestion fired %d times; want 1", postCalls)
	}
	if len(m.btwCards) != 0 {
		t.Errorf("blocked /btw registered %d cards; want 0", len(m.btwCards))
	}
	if !postBlocked {
		t.Errorf("PostSideQuestion payload Blocked = false; want true")
	}
	if postReason != "policy: denied" {
		t.Errorf("PostSideQuestion BlockReason = %q; want %q", postReason, "policy: denied")
	}
	// The block reason echoes into the visible transcript.
	var found bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), "blocked by hook: policy: denied") {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("block reason missing from transcript: %v", m.response)
	}
}

// TestBtwMultipleInvocationsFireHooksPerCall: three back-to-back
// /btw invocations each fire both Pre and Post exactly once. The
// Manager's fire-count counter is a useful invariant for subagent
// hosts that tally events over the session.
//
// The test rebuilds a fresh runMode per /btw so the underlying PiG
// coding.Session lifecycle stays inside one Ask per model — every
// existing cycle-230 test follows the same pattern (TestCopyBtwSlash
// does `m = newSlashModel(t)` between turns; see also
// TestBtwStreamingCard etc.). The three questions share the
// "What is 20+22?" prefix that PiG's faux provider matches, so all
// three resolve to the "42" canned response — varying the suffix
// is enough to give every /btw a distinct Question string for the
// hook-payload assertion.
func TestBtwMultipleInvocationsFireHooksPerCall(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	questions := []string{
		"What is 20+22? (1)",
		"What is 20+22? (2)",
		"What is 20+22? (3)",
	}
	var pre, post int
	var seenQuestions []string

	for i, q := range questions {
		m := newSlashModel(t)
		m.hooks.Register(hooks.PreSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
			seenQuestions = append(seenQuestions, p.Question)
			return hooks.DecisionAllow, "", nil
		})
		buf := "/btw " + q
		if handled, _ := m.handleSlashCommand(buf); !handled {
			t.Fatalf("/btw %d not handled", i+1)
		}
		if len(m.btwCards) != 1 {
			t.Errorf("/btw %d btwCards length = %d, want 1", i+1, len(m.btwCards))
		}
		pre += m.hooks.Count(hooks.PreSideQuestion)
		post += m.hooks.Count(hooks.PostSideQuestion)
	}
	if pre != 3 {
		t.Errorf("cumulative PreSideQuestion count = %d, want 3", pre)
	}
	if post != 3 {
		t.Errorf("cumulative PostSideQuestion count = %d, want 3", post)
	}
	if len(seenQuestions) != 3 {
		t.Fatalf("captured %d hook payloads; want 3", len(seenQuestions))
	}
	for i, want := range questions {
		if seenQuestions[i] != want {
			t.Errorf("PreSideQuestion payload[%d] = %q, want %q", i, seenQuestions[i], want)
		}
	}
}

// TestBtwHookReceivesQuestionText: the hook payload carries the
// user-typed question verbatim. This is the contract a subagent
// host relies on to log or to make a block decision based on the
// question's content.
func TestBtwHookReceivesQuestionText(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	const want = "explain quantum entanglement briefly"
	seen := ""
	m.hooks.Register(hooks.PreSideQuestion, func(ctx context.Context, p hooks.Payload) (hooks.Decision, string, error) {
		seen = p.Question
		return hooks.DecisionAllow, "", nil
	})

	if handled, _ := m.handleSlashCommand("/btw " + want); !handled {
		t.Fatal("/btw not handled")
	}
	if seen != want {
		t.Errorf("hook payload Question = %q, want %q", seen, want)
	}
}

// TestBtwSideQuestionAppearsInTranscript: after /btw the parent
// m.session grows by one user message whose body is the side
// question prefixed with "> /btw ". The transcript records the
// side question like any other user input so /resume and the
// on-disk conversation db see it too.
func TestBtwSideQuestionAppearsInTranscript(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	// Empty session: appendBtwToSession should grow it from 0 to 1.
	if handled, _ := m.handleSlashCommand("/btw What is 6*7?"); !handled {
		t.Fatal("/btw not handled")
	}
	if len(m.session) != 1 {
		t.Fatalf("m.session length = %d, want 1", len(m.session))
	}
	got := m.session[0]
	if got.Role != "user" {
		t.Errorf("appended role = %q, want user", got.Role)
	}
	if !strings.Contains(got.Content, "What is 6*7?") {
		t.Errorf("appended content = %q, want it to contain the side question", got.Content)
	}
	if !strings.HasPrefix(got.Content, "> /btw ") {
		t.Errorf("appended content = %q, want it to start with \"> /btw \"", got.Content)
	}
}

// TestBtwShowHistoryWhenNoArgs verifies that when past BTW cards exist and no question is in progress,
// running `/btw` with no arguments re-opens the most recent card rather than printing usage.
func TestBtwShowHistoryWhenNoArgs(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	m.handleSlashCommand("/btw What is 20+22?")
	if len(m.btwCards) != 1 || m.activeBtwIndex != 0 {
		t.Fatalf("expected 1 card active at index 0, got len=%d idx=%d", len(m.btwCards), m.activeBtwIndex)
	}

	// Dismiss card
	m.DismissBtwResponse()
	if m.isBtwCardVisible() {
		t.Fatal("card should not be visible after dismissal")
	}

	// Run /btw without arguments: should restore history
	respLenBefore := len(m.response)
	m.handleSlashCommand("/btw")
	if !m.isBtwCardVisible() {
		t.Fatal("card should be visible after /btw with history")
	}
	if m.activeBtwIndex != 0 {
		t.Errorf("expected activeBtwIndex=0, got %d", m.activeBtwIndex)
	}
	// No error / usage response should have been appended
	if len(m.response) > respLenBefore {
		for _, row := range m.response[respLenBefore:] {
			if strings.Contains(stripColor(row), "Usage: /btw") {
				t.Errorf("unexpected usage output when showing history: %s", row)
			}
		}
	}
}

// TestBtwConcurrencyGuard verifies that attempting to run `/btw` while another question
// is still generating is rejected with the reference message.
func TestBtwConcurrencyGuard(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	// Seed an in-flight card
	activeCard := &sidequestion.Card{
		ID:       "in-flight-1",
		Question: "Running question?",
		State:    sidequestion.StateStreaming,
	}
	m.btwCards = append(m.btwCards, activeCard)
	m.activeBtwIndex = 0
	m.activeBtwID = activeCard.ID

	if !m.IsBtwActive() {
		t.Fatal("IsBtwActive should report true for streaming card")
	}

	m.handleSlashCommand("/btw Another question")
	var found bool
	wantMsg := "A /btw question is already in progress. Please wait for it to finish."
	for _, row := range m.response {
		if strings.Contains(stripColor(row), wantMsg) {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("transcript missing concurrency guard message %q: %v", wantMsg, m.response)
	}
}

// TestCopyBtwInProgressAndFailed verifies `/copy btw` behavior for generating and failed cards.
func TestCopyBtwInProgressAndFailed(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	streamingCard := &sidequestion.Card{
		ID:       "gen-1",
		Question: "Generating...",
		State:    sidequestion.StateStreaming,
	}
	m.btwCards = append(m.btwCards, streamingCard)
	m.activeBtwIndex = 0
	m.activeBtwID = streamingCard.ID

	m.handleSlashCommand("/copy btw")
	wantGen := "The /btw answer is still being generated."
	var foundGen bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), wantGen) {
			foundGen = true
			break
		}
	}
	if !foundGen {
		t.Errorf("missing in-progress message %q: %v", wantGen, m.response)
	}

	// Now test failed card
	failedCard := &sidequestion.Card{
		ID:       "fail-1",
		Question: "Failing...",
		State:    sidequestion.StateFailed,
		Err:      errors.New("network timeout"),
	}
	m.btwCards = append(m.btwCards, failedCard)
	m.activeBtwIndex = 1
	m.activeBtwID = failedCard.ID

	m.handleSlashCommand("/copy btw")
	wantFail := "The /btw question failed: network timeout"
	var foundFail bool
	for _, row := range m.response {
		if strings.Contains(stripColor(row), wantFail) {
			foundFail = true
			break
		}
	}
	if !foundFail {
		t.Errorf("missing failure message %q: %v", wantFail, m.response)
	}
}

// TestBtwNavigation verifies ← / → shifting through historical BTW cards.
func TestBtwNavigation(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	c1 := &sidequestion.Card{ID: "c1", Question: "Q1", Answer: "A1", State: sidequestion.StateDone}
	c2 := &sidequestion.Card{ID: "c2", Question: "Q2", Answer: "A2", State: sidequestion.StateDone}
	c3 := &sidequestion.Card{ID: "c3", Question: "Q3", Answer: "A3", State: sidequestion.StateDone}
	m.btwCards = []*sidequestion.Card{c1, c2, c3}
	m.activeBtwIndex = 2
	m.activeBtwID = c3.ID

	// Left navigates to c2
	m.NavigateBtw(-1)
	if m.activeBtwIndex != 1 || m.activeBtwID != c2.ID {
		t.Errorf("expected idx=1 c2, got idx=%d id=%s", m.activeBtwIndex, m.activeBtwID)
	}

	// Left navigates to c1
	m.NavigateBtw(-1)
	if m.activeBtwIndex != 0 || m.activeBtwID != c1.ID {
		t.Errorf("expected idx=0 c1, got idx=%d id=%s", m.activeBtwIndex, m.activeBtwID)
	}

	// Left clamps at 0
	m.NavigateBtw(-1)
	if m.activeBtwIndex != 0 {
		t.Errorf("expected clamp to idx=0, got %d", m.activeBtwIndex)
	}

	// Right navigates back to c2 then c3
	m.NavigateBtw(1)
	if m.activeBtwIndex != 1 {
		t.Errorf("expected idx=1, got %d", m.activeBtwIndex)
	}
	m.NavigateBtw(1)
	if m.activeBtwIndex != 2 {
		t.Errorf("expected idx=2, got %d", m.activeBtwIndex)
	}
	// Right clamps at len-1
	m.NavigateBtw(1)
	if m.activeBtwIndex != 2 {
		t.Errorf("expected clamp to idx=2, got %d", m.activeBtwIndex)
	}
}

// TestBtwRenderCardFormatting verifies card styling with thick left border (┃),
// question header, answer body, and hint bar.
func TestBtwRenderCardFormatting(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	m.width = 80
	m.height = 30
	c := &sidequestion.Card{
		ID:       "card-render",
		Question: "How does caching work?",
		Answer:   "Caching stores computed results for fast retrieval.",
		State:    sidequestion.StateDone,
	}
	m.btwCards = []*sidequestion.Card{c}
	m.activeBtwIndex = 0
	m.activeBtwID = c.ID

	rendered := m.renderBtwCard()
	if !strings.Contains(rendered, "┃") {
		t.Errorf("rendered card missing thick left border ┃:\n%s", rendered)
	}
	if !strings.Contains(rendered, "How does caching work?") {
		t.Errorf("rendered card missing question:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Caching stores computed results") {
		t.Errorf("rendered card missing answer body:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Tab to expand") {
		t.Errorf("rendered card missing 'Tab to expand' in collapsed mode:\n%s", rendered)
	}
	if !strings.Contains(rendered, "Enter/Esc to dismiss") {
		t.Errorf("rendered card missing 'Enter/Esc to dismiss':\n%s", rendered)
	}

	// Toggle expansion
	m.ToggleBtwExpanded()
	expandedRendered := m.renderBtwCard()
	if !strings.Contains(expandedRendered, "Tab to collapse") {
		t.Errorf("expanded card missing 'Tab to collapse':\n%s", expandedRendered)
	}
}

// TestBtwEscDismissesCardBeforeInterruptingTurn verifies that when a /btw
// card is visible during an active streaming turn:
// 1. First Esc closes the /btw card while the turn keeps streaming.
// 2. Second Esc interrupts the streaming turn.
func TestBtwEscDismissesCardBeforeInterruptingTurn(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	c := &sidequestion.Card{
		ID:       "card-esc",
		Question: "What is 2+2?",
		Answer:   "4",
		State:    sidequestion.StateDone,
	}
	m.btwCards = []*sidequestion.Card{c}
	m.activeBtwIndex = 0
	m.activeBtwID = c.ID

	if !m.isBtwCardVisible() {
		t.Fatal("precondition: /btw card must be visible")
	}

	cancelled := false
	m.streaming = true
	m.turnCancel = func() { cancelled = true }

	// First Esc: dismisses /btw card, turn is NOT cancelled.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.isBtwCardVisible() {
		t.Fatal("first Esc should dismiss the /btw card")
	}
	if cancelled {
		t.Fatal("first Esc should NOT cancel the in-flight streaming turn")
	}
	if m.turnInterrupted {
		t.Fatal("first Esc should NOT mark turnInterrupted")
	}

	// Second Esc: now that /btw card is gone, interrupts the streaming turn.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if !cancelled {
		t.Fatal("second Esc should cancel the in-flight streaming turn")
	}
	if !m.turnInterrupted {
		t.Fatal("second Esc should mark turnInterrupted")
	}
}

// TestMediaExpandedEscCollapsesBeforeInterruptingTurn verifies that an expanded
// media preview is collapsed before interrupting a streaming turn.
func TestMediaExpandedEscCollapsesBeforeInterruptingTurn(t *testing.T) {
	m := newSlashModel(t)
	m.mediaExpanded = true

	cancelled := false
	m.streaming = true
	m.turnCancel = func() { cancelled = true }

	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.mediaExpanded {
		t.Fatal("first Esc should collapse mediaExpanded")
	}
	if cancelled {
		t.Fatal("first Esc should NOT cancel the in-flight streaming turn")
	}

	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if !cancelled {
		t.Fatal("second Esc should cancel the in-flight streaming turn")
	}
}

// TestBtwEscPreservesDraftInputText verifies that Esc dismisses /btw card
// without clearing draft text in the composer.
func TestBtwEscPreservesDraftInputText(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	c := &sidequestion.Card{
		ID:       "card-preserve-input",
		Question: "What is caching?",
		Answer:   "Fast storage.",
		State:    sidequestion.StateDone,
	}
	m.btwCards = []*sidequestion.Card{c}
	m.activeBtwIndex = 0
	m.activeBtwID = c.ID
	m.input.SetValue("my pending question")

	// First Esc: dismisses /btw card, leaves input intact.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if m.isBtwCardVisible() {
		t.Fatal("first Esc should dismiss the /btw card")
	}
	if got := m.input.Value(); got != "my pending question" {
		t.Fatalf("first Esc should preserve input, got %q", got)
	}

	// Second Esc: arms the `esc esc` clear (cycle 383, B-21 — the
	// reference clears the prompt box on two Escs,
	// official-docs/cli/using.md:22), so the text is still there.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if got := m.input.Value(); got != "my pending question" {
		t.Fatalf("second Esc should only arm the clear, got %q", got)
	}

	// Third Esc: completes the pair and clears the input.
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))
	if got := m.input.Value(); got != "" {
		t.Fatalf("third Esc should clear input, got %q", got)
	}
}

// TestBtw_AsyncNonBlocking verifies that in interactive mode (/btw with asyncBtw=true
// or m.prog != nil), /btw immediately creates and attaches the Card in StatePending,
// returns a non-nil tea.Cmd, keeps the TUI non-blocking, and streams the answer to StateDone.
func TestBtw_AsyncNonBlocking(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	m.asyncBtw = true

	handled, cmd := m.handleSlashCommand("/btw What is 20+22?")
	if !handled {
		t.Fatal("/btw not handled")
	}
	if cmd == nil {
		t.Fatal("async /btw must return a non-nil tea.Cmd")
	}

	// The card must be registered immediately in StatePending so the UI can render
	// the thinking card without waiting for the network round-trip.
	if len(m.btwCards) != 1 {
		t.Fatalf("expected 1 card registered immediately, got %d", len(m.btwCards))
	}
	card := m.btwCards[0]
	_, answer, _, state, err := card.LockedSnapshot()
	if err != nil {
		t.Fatalf("unexpected card error: %v", err)
	}
	if state != sidequestion.StatePending {
		t.Errorf("immediate card state = %q, want %q", state, sidequestion.StatePending)
	}
	if answer != "" {
		t.Errorf("immediate card answer = %q, want empty", answer)
	}
	if !m.IsBtwActive() {
		t.Error("m.IsBtwActive() should be true while query is running")
	}
	if !m.isBtwCardVisible() {
		t.Error("card should be visible immediately above the prompt")
	}

	// Executing the returned batch command
	batchMsg := cmd()
	batch, ok := batchMsg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("cmd() returned %T, want tea.BatchMsg", batchMsg)
	}
	if len(batch) != 2 {
		t.Fatalf("expected 2 batch commands (askCmd and btwTick), got %d", len(batch))
	}

	// 1. Verify tick message processing while active
	tickCmd := batch[1]
	tickMsg := tickCmd()
	if _, ok := tickMsg.(btwTickMsg); !ok {
		t.Errorf("tick command produced %T, want btwTickMsg", tickMsg)
	}
	_, rearmCmd := m.Update(tickMsg)
	if rearmCmd == nil {
		t.Error("btwTickMsg should rearm next tick while card is active")
	}

	// 2. Run the askCmd to completion
	askCmd := batch[0]
	doneMsg := askCmd()
	done, ok := doneMsg.(btwDoneMsg)
	if !ok {
		t.Fatalf("askCmd produced %T, want btwDoneMsg", doneMsg)
	}
	if done.err != nil {
		t.Fatalf("askCmd returned error: %v", done.err)
	}

	// 3. Deliver btwDoneMsg to Update
	m.Update(done)

	// Card should now be done with answer "42"
	_, finalAnswer, _, finalState, finalErr := card.LockedSnapshot()
	if finalErr != nil {
		t.Fatalf("card settled with error: %v", finalErr)
	}
	if finalState != sidequestion.StateDone {
		t.Errorf("card settled state = %q, want %q", finalState, sidequestion.StateDone)
	}
	if finalAnswer != "42" {
		t.Errorf("card settled answer = %q, want %q", finalAnswer, "42")
	}
	if m.IsBtwActive() {
		t.Error("m.IsBtwActive() should be false after query settles")
	}
}

// TestBtw_AsyncCancellationOnEsc verifies that pressing Esc while an async
// /btw question is in-flight immediately marks it cancelled and clears active state.
func TestBtw_AsyncCancellationOnEsc(t *testing.T) {
	cleanup := withBtwManager(t)
	defer cleanup()

	m := newSlashModel(t)
	m.asyncBtw = true

	handled, cmd := m.handleSlashCommand("/btw What is 20+22?")
	if !handled || cmd == nil {
		t.Fatal("/btw not handled or nil cmd")
	}
	if !m.IsBtwActive() {
		t.Fatal("precondition: /btw should be active")
	}
	if m.activeBtwCancel == nil {
		t.Fatal("activeBtwCancel should be armed")
	}

	// User presses Esc to cancel in-flight /btw
	m.Update(tea.KeyPressMsg(tea.Key{Code: tea.KeyEsc}))

	if m.isBtwCardVisible() {
		t.Error("card should be dismissed after Esc")
	}
	if m.activeBtwCancel != nil {
		t.Error("activeBtwCancel should be cleared after Esc")
	}
	card := m.btwCards[0]
	if card.State != sidequestion.StateFailed {
		t.Errorf("card state = %q, want failed", card.State)
	}
	if card.Err == nil || !strings.Contains(card.Err.Error(), "cancelled") {
		t.Errorf("card.Err = %v, want cancelled", card.Err)
	}
}
