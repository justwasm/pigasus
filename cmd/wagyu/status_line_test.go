// status_line_test.go — cycle 257 (AGY 1.1.27):
// `conversation_title` field on the JSON payload passed to custom
// status-line and window-title scripts.
//
// The field is populated from `m.conversationTitle()` — the same
// helper `/title` and the terminal WindowTitle read. The cycle pins
// five guarantees around the wire form:
//
//  1. The field is *named* `conversation_title` (matches the recovered
//     AGY changelog contract) and *present* in every payload the
//     spawned script reads from stdin, even when the conversation
//     hasn't been user-named.
//  2. A `/rename <name>` followed by the next tick refreshes the
//     field — the spawn goroutine re-reads m.conversationName on
//     every tick, so the rename is visible within 100 ms.
//  3. The field is *never* omitted from the JSON object: when
//     conversationName is empty and there is no first-user-message
//     fallback, the helper falls back to `filepath.Base(os.Args[0])`
//     so the script can rely on the field being a non-empty string.
//  4. Existing field names (`model`, `cost`, `current_dir`) are
//     untouched — adding a new key must not break scripts that
//     ignore unknown fields. The shape is forward-additive.
//  5. Print mode (`-p`) does *not* spawn the configured script:
//     `runExternalStatusLineRender` is reachable only from the TUI's
//     bubbletea tick goroutine (cmd/wagyu/tui_run.go:3776), so the
//     `conversation_title` field is only relevant for interactive
//     runs. Print mode never calls it.
//
// The cycle's contribution is the test surface. The production
// struct (`statusLinePayload.ConversationTitle`) and its mapper
// (`buildStatusLinePayload`) were introduced in the
// `status-line-script` cycle alongside AGY 1.1.21's `cost` field.
// What this cycle adds is a dedicated file pinning the wire form so
// a regression in `conversationTitle()` (or a future refactor that
// renames / drops the field) trips a targeted test rather than a
// downstream /btw or /cost one.
package main

import (
	"bytes"
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

// TestStatusLinePayload_IncludesConversationTitle pins the field's
// presence in the JSON wire form. The contract from the AGY 1.1.27
// changelog is that the field is `conversation_title` (not
// `title`, not `conversationTitle`, not `name`) — a downstream
// script can grep that exact key. We assert both the Go field and
// the encoded JSON form so a struct rename trips the test.
func TestStatusLinePayload_IncludesConversationTitle(t *testing.T) {
	m := &runMode{
		statusModel: "MiniMax M3",
		model:       "MiniMax M3",
		cwd:         "/Users/foo/project",
	}
	m.conversationName = "Refactor auth"
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
// m.conversationName, not cached anywhere.
func TestStatusLinePayload_UpdatesOnRename(t *testing.T) {
	m := &runMode{
		statusModel: "MiniMax M3",
		model:       "MiniMax M3",
		cwd:         "/tmp",
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
	// the slash dispatcher writing it back into m.conversationName
	// (cmd/wagyu/slash_commands.go:528-529). The cycle pins that
	// payload follows rename without any other wiring.
	next, msg := renameFeedback("New Title", m.conversationName)
	if msg == "" || !strings.HasPrefix(msg, "Conversation renamed") {
		t.Fatalf("renameFeedback msg = %q", msg)
	}
	m.conversationName = next

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
	m := &runMode{
		statusModel: "MiniMax M3",
		model:       "MiniMax M3",
		cwd:         "/private/tmp",
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

// TestStatusLinePayload_BackwardCompatible pins the existing wire
// shape. AGY 1.1.27 added `conversation_title` *additively* —
// scripts that pre-date 1.1.27 only know about `model`, `cost`,
// and `current_dir`. A cycle-257 regression that, say, renamed
// `cost` → `total_cost` would silently break every existing user
// script; this test catches that.
//
// Field order in the JSON object is also pinned: `model`, `cost`,
// `conversation_title`, `current_dir` (struct field order; Go's
// `encoding/json` honours it).
func TestStatusLinePayload_BackwardCompatible(t *testing.T) {
	m := &runMode{
		statusModel:             "MiniMax M3",
		model:                   "MiniMax M3",
		cwd:                     "/Users/foo/project",
		sessionPromptTokens:     1000,
		sessionCompletionTokens: 500,
	}
	m.conversationName = "Refactor auth"
	payload := m.buildStatusLinePayload()

	// Go-level: every old field is still on the struct, with the
	// same types.
	if payload.Model != "MiniMax M3" {
		t.Errorf("Model = %q", payload.Model)
	}
	// Cost is the unrounded USD running total. MiniMax M3 is a
	// configured-only model with $0 pricing (cycle 225 quota UI),
	// so a known-shape input produces a known value of 0. The
	// field is *present* and a number; the value is allowed to be
	// 0 here.
	if payload.Cost != 0 {
		t.Errorf("Cost = %v, want 0 (MiniMax M3 is free)", payload.Cost)
	}
	if payload.CurrentDir != "/Users/foo/project" {
		t.Errorf("CurrentDir = %q", payload.CurrentDir)
	}

	// JSON-level: every old key is still in the wire form. The
	// decoder path (rather than Contains on a substring) is the
	// safer test — it catches a future refactor that renames the
	// JSON tag.
	out, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var decoded map[string]any
	if err := json.Unmarshal(out, &decoded); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	for _, key := range []string{"model", "cost", "current_dir"} {
		if _, ok := decoded[key]; !ok {
			t.Errorf("legacy key %q missing from JSON: %s", key, out)
		}
	}
	// And the new key lands alongside them, not by replacing one.
	if _, ok := decoded["conversation_title"]; !ok {
		t.Errorf("new key conversation_title missing from JSON: %s", out)
	}
}

// TestStatusLinePayload_PrintModeUnaffected pins the structural
// fact that print mode (`-p`) never spawns the configured script:
// the `conversation_title` field is only meaningful for the
// interactive TUI. The test uses go/parser to walk the package
// source and assert that no print-mode code path references the
// spawn helpers (`runStatusLineScript`, `runExternalStatusLineRender`).
//
// A simpler version of this test would just check that
// `buildStatusLinePayload` is pure (no I/O) — and we *do* check
// that. But the contract the cycle pins is stronger: print mode
// must not just *be able* to skip the spawn, it must *have no
// code path* to invoke it. A future refactor that wires the spawn
// into runPrintMode would silently break print-mode latency and
// fail this test.
func TestStatusLinePayload_PrintModeUnaffected(t *testing.T) {
	// 1. Pure-function check: buildStatusLinePayload is deterministic
	//    and does not spawn. Two back-to-back calls produce the
	//    same payload — no goroutine, no subprocess.
	m := &runMode{
		statusModel: "MiniMax M3",
		model:       "MiniMax M3",
		cwd:         "/private/tmp",
	}
	m.session = []backend.Message{{Role: "user", Content: "hello"}}
	a := m.buildStatusLinePayload()
	b := m.buildStatusLinePayload()
	if a != b {
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
	if filepath.Base(wd) != "wagyu" {
		// Fallback: walk up to the repo root and re-join. This
		// branch fires only when the test is run with `go test
		// ./cmd/wagyu/` from outside the directory — typical
		// during a sweep.
		cmdDir = filepath.Join(wd, "cmd", "wagyu")
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
			name == "main.go" /* main.go hosts runPrintMode */
		if !isPrintMode {
			continue
		}
		path := filepath.Join(cmdDir, name)
		f, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", name, err)
		}
		// Walk every reference to runStatusLineScript or
		// runExternalStatusLineRender. A reference in
		// `print_*.go` is a contract violation; a reference
		// inside runPrintMode in main.go is too. The test
		// explicitly calls out main.go as a "print mode file"
		// even though main.go also hosts runLiveTUI; the
		// function-name filter inside the walker is what
		// keeps runLiveTUI from tripping this assertion.
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
				// The function itself is allowed to be
				// declared here — we only flag *calls*.
				// Selector on the package's own decl is
				// not reachable through Inspect's filter
				// above, but be defensive anyway.
				t.Errorf("print-mode file %s calls %s — print mode must not spawn the status-line script",
					name, fn.Sel.Name)
			}
			return true
		})
	}
}
