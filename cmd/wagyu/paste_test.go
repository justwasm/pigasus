// paste_test.go — cycle 278: bracketed paste and ctrl+v.
package main

import (
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"
)

// withClipboard swaps the clipboard-read seam for the duration of a test.
func withClipboard(t *testing.T, fn func() string) {
	t.Helper()
	old := clipboardReadText
	clipboardReadText = fn
	t.Cleanup(func() { clipboardReadText = old })
}

func TestPasteMsgInsertsAtCursor(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.input.SetValue("ab")
	m.input.CursorStart()

	if _, cmd := m.Update(tea.PasteMsg{Content: "XY"}); cmd != nil {
		t.Fatalf("paste returned a cmd; it must not submit or leave the loop")
	}
	if got, want := m.input.Value(), "XYab"; got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}
}

func TestPasteMsgPreservesNewlines(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.input.SetValue("")

	m.Update(tea.PasteMsg{Content: "a\nb\nc"})
	if got, want := m.input.Value(), "a\nb\nc"; got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}
	if n := m.input.LineCount(); n != 3 {
		t.Fatalf("LineCount = %d, want 3", n)
	}
}

// A Windows-style clipboard brings CRLF; the buffer must not keep "\r".
func TestPasteMsgNormalizesCRLF(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.input.SetValue("")

	m.Update(tea.PasteMsg{Content: "a\r\nb\r"})
	if got, want := m.input.Value(), "a\nb"; got != want {
		t.Fatalf("input = %q, want %q", got, want)
	}
}

func TestPasteMsgDoesNotSubmit(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.input.SetValue("")
	before := len(m.response)

	m.Update(tea.PasteMsg{Content: "hello"})
	if len(m.response) != before {
		t.Fatalf("paste appended %d transcript rows; it must never submit",
			len(m.response)-before)
	}
	if m.streaming {
		t.Fatalf("paste started a turn")
	}
}

func TestPasteMsgEmptyIsNoOp(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.input.SetValue("keep")

	if _, cmd := m.Update(tea.PasteMsg{Content: ""}); cmd != nil {
		t.Fatalf("empty paste returned a cmd")
	}
	if got := m.input.Value(); got != "keep" {
		t.Fatalf("empty paste changed the buffer: %q", got)
	}
}

func TestPasteMsgIgnoredWhileHelpOpen(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.input.SetValue("keep")
	m.helpMode = true

	m.Update(tea.PasteMsg{Content: "junk"})
	if got := m.input.Value(); got != "keep" {
		t.Fatalf("paste leaked into the prompt behind /help: %q", got)
	}
}

func TestPasteMsgRoutesToResumeSearch(t *testing.T) {
	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.resumeMode = true
	m.resumeQuery = ""
	m.input.SetValue("")

	m.Update(tea.PasteMsg{Content: "abc"})
	if m.resumeQuery != "abc" {
		t.Fatalf("resumeQuery = %q, want \"abc\"", m.resumeQuery)
	}
	if got := m.input.Value(); got != "" {
		t.Fatalf("paste leaked into the prompt while /resume was open: %q", got)
	}
}

func TestCtrlVPastesClipboardText(t *testing.T) {
	withClipboard(t, func() string { return "from-clipboard" })

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.input.SetValue("")

	m.Update(ctrlKey('v'))
	if got, want := m.input.Value(), "from-clipboard"; got != want {
		t.Fatalf("ctrl+v input = %q, want %q", got, want)
	}
}

func TestCtrlVEmptyClipboardIsSilent(t *testing.T) {
	withClipboard(t, func() string { return "" })

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.input.SetValue("keep")
	before := len(m.response)

	m.Update(ctrlKey('v'))
	if got := m.input.Value(); got != "keep" {
		t.Fatalf("empty clipboard changed the buffer: %q", got)
	}
	if len(m.response) != before {
		t.Fatalf("empty clipboard produced %d transcript rows", len(m.response)-before)
	}
}

// The paste must be the *clipboard* content, not a literal "v": the
// textarea's own ctrl+v path would have read the clipboard too, but its
// result was discarded, so the visible symptom was "nothing happens".
func TestCtrlVDoesNotInsertLiteralV(t *testing.T) {
	withClipboard(t, func() string { return "" })

	m := NewRunMode(&fakeClient{}, "u@t", "m", "t", "/tmp", true)
	m.input.SetValue("")

	m.Update(ctrlKey('v'))
	if strings.Contains(m.input.Value(), "v") {
		t.Fatalf("ctrl+v inserted a literal v: %q", m.input.Value())
	}
}
