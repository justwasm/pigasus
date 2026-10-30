package customize

// recorder.go — in-process accumulation of customization bodies for prompt trimming.
//
// AGY 1.2.16 fix: the /customize, /config, /agents, /hooks, /mcp,
// /plugin, /skills, /commands slash commands all render bodies of
// text that the LLM sees on every turn. When the context window is
// tight, the lower-priority payloads should be dropped so the model
// has more headroom for actual work.
//
// The recorder is a single in-process append-only log. It is
// process-global because slash commands can run on any goroutine
// (the TUI's bubbletea Update loop, the print path's prompt
// goroutine, the test harness) and the LLM turn is the only
// snapshot point that matters — every Add lands in the same
// append-only slice regardless of caller.
//
// Each TrimTokens call builds a fresh Trimmer bound
// to the caller's budget, replays every recorded token, and trims.
// The recorder itself is unchanged by a trim, so subsequent Adds
// land alongside the prior entries; a future Trim with a tighter
// budget sees everything the user has contributed in the live
// session.

import (
	"sync"
)

// recorder holds the in-process accumulation of customization tokens.
// The lock guards the tokens slice; the slice is append-only so a
// reader can safely copy it under the lock and walk the copy without
// re-locking.
var recorder = struct {
	sync.Mutex
	tokens []*Token
}{}

// RecordToken contributes a body to the in-process recorder. It is
// safe to call from any goroutine; the underlying Token is built
// fresh on every call so the slice never shares mutable state
// between callers.
//
// Callers should pass the canonical slash-command name
// ("customize", "config", "agents", "hooks", "mcp", "plugin",
// "skills", "commands"). Unknown names inherit PriorityCommands'
// conservative default — see PriorityFor — so the trimmer treats
// them as "expensive, drop last".
//
// Empty content is a no-op: the trimmer would treat it as a
// zero-cost payload that always wins, but skipping the Add avoids
// the bookkeeping overhead and the slice growth.
func RecordToken(name, content string) {
	if content == "" {
		return
	}
	recorder.Lock()
	defer recorder.Unlock()
	recorder.tokens = append(recorder.tokens,
		NewToken(name, content, int(PriorityFor(name))))
}

// SnapshotTokens returns a copy of the recorded tokens in registration
// order. Useful for tests and for diagnostics that want to render the
// live list without invoking the trimmer.
func SnapshotTokens() []*Token {
	recorder.Lock()
	defer recorder.Unlock()
	out := make([]*Token, len(recorder.tokens))
	copy(out, recorder.tokens)
	return out
}

// ResetRecorder discards every recorded token. Test helpers call this
// between cases; production code never resets because the per-conversation
// lifecycle already starts with a fresh process and a fresh recorder.
func ResetRecorder() {
	recorder.Lock()
	defer recorder.Unlock()
	recorder.tokens = nil
}

// TrimTokens fits the recorded tokens into the budget the LLM
// context-assembly step provides. It is the public bridge the AGY
// 1.2.16 fix wires into the context-build path.
//
// Returns:
//
//   - kept: the tokens that survived the trim, in registration order
//     (so the caller's downstream rendering sees a stable layout);
//   - dropped: the tokens the trimmer cut, in priority order (so a
//     "we trimmed X first" diagnostic matches the canonical priority
//     ordering).
//
// nil for both slices when nothing has been contributed yet — the
// caller can short-circuit on empty input.
//
// The function is safe to call from any goroutine; the lock is taken
// only for the duration of the replay-and-trim.
func TrimTokens(budget Budget) (kept, dropped []*Token) {
	recorder.Lock()
	defer recorder.Unlock()
	if len(recorder.tokens) == 0 {
		return nil, nil
	}
	fresh := NewTrimmer(budget)
	for _, tok := range recorder.tokens {
		fresh.AddToken(tok)
	}
	kept = fresh.Trim()
	dropped = fresh.Dropped()
	return kept, dropped
}
