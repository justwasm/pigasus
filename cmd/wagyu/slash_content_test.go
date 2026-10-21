package main

// slash_content_test.go — cycle 102: the TUI slash commands whose content is
// recoverable now render the same content the print-mode handler emits, instead
// of the cycle-77 "(cycle 77: stub)" placeholders.

import (
	"os"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

func newSlashModel(t *testing.T) *runMode {
	t.Helper()
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
	m := NewRunMode(&fakeClient{}, "u@t", "Gemini 3.8 Flash (High)", "t", "/", true)
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
			out[i] = stripSGR(r)
		}
		return out
	}
	if len(m.response) < 2 {
		return nil
	}
	// Strip SGR + "  ⎿  " prefix from the message line.
	msgLine := stripSGR(m.response[1])
	msgLine = strings.TrimPrefix(msgLine, "  \u23bf  ")
	out := []string{msgLine}
	// Append any extra rows between the `⎿` line and the trailing blank.
	// The blank line is always last; everything between is extra content
	// (e.g. /voice's 5-space-indented hint line).
	for _, r := range m.response[2:] {
		if r == "" {
			break
		}
		out = append(out, stripSGR(r))
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
	for _, key := range printSlashConfigKeys {
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
		{"/credits", "error: /credits failed: retrieving credits: no credits info found"},
	} {
		m = newSlashModel(t)
		m.handleSlashCommand(tc.cmd)
		if got := strings.Join(afterEcho(m), "\n"); got != tc.want {
			t.Errorf("%s rendered %q, want %q", tc.cmd, got, tc.want)
		}
	}
}

// TestSlashNewStartsFreshConversation pins that /new is routed (cycle 77 never
// handled it, so it fell through to "Unknown command: /new").
func TestSlashNewStartsFreshConversation(t *testing.T) {
	m := newSlashModel(t)
	m.session = append(m.session, backend.Message{Role: "user", Content: "hi"})
	m.blocks = []turnBlock{{start: 0, lines: 1}}
	m.liveSteps = []toolStep{{kind: stepKindTool}}
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
