// slash_statusline_external.go — cycle status-line-script: the external
// status-line script path that AGY 1.1.27 introduced.
//
// The reference's statusLine config now carries a `command` field whose
// value is the path to a user-supplied script (a tmux-style renderer is
// the canonical example). Every status-line refresh the CLI spawns the
// script, writes the recovered JSON payload to stdin, and reads the
// rendered line(s) from stdout. The clone wires the same surface so a
// pre-existing tmux-style script (or the recovered /btw /cost workflow
// tests) lights up with zero additional glue.
//
// Cycle status-line-script covers two AGY changelog lines:
//
//	1.1.21 line 393 — added a `cost` field to the status line data
//	                  model, exposing the unrounded estimated cost of
//	                  the current session so a custom status line can
//	                  display running token spend.
//
//	1.1.27 line 318 — added a `conversation_title` field to the JSON
//	                  payload passed to custom status line and window
//	                  title scripts, so they can show the active
//	                  conversation's name and pick up renames
//	                  immediately.
//
// The contract:
//
//   - settings.json's `statusLine.command` is the script path. The
//     command string is split on whitespace the way the cycle-103
//     `splitCommand` helper does, so a value like
//     `/usr/local/bin/tmux-status --foo` lands as the slice
//     `["/usr/local/bin/tmux-status", "--foo"]`. `statusLine.enabled`
//     must also be true; either being false falls back to the built-in
//     status row.
//
//   - The payload is the JSON object
//     {"model": ..., "cost": <float>, "conversation_title": ...,
//     "current_dir": ...}  written to the script's stdin, and the
//     rendered text is read back from stdout. The script is given a
//     1-second budget; an over-budget or non-zero-exit spawn falls
//     back to the built-in row with a one-line stderr note.
//
//   - `cost` is the unrounded running total. We compute it from a
//     per-model per-1M-token pricing table (see pricingForModel below)
//     applied to the session's accumulated prompt + completion token
//     counts. The clone does not have access to the reference's
//     private pricing so the numbers are best-effort approximations;
//     the field is *named* correctly and unrounded, which is what the
//     changelog contract demands.
//
//   - `conversation_title` is `m.conversationTitle()` — the same value
//     /title already shows. Renames land through /rename and are
//     visible immediately because the TUI re-reads this on every
//     tick.
//
// The two new fields are the only additions to the JSON shape that
// the older cycles did not already pin. The rest of the payload —
// `model`, `current_dir` — is what every cycle-193 / cycle-205 probe
// has been writing. We do not name the full schema
// `{"model","cost","conversation_title","current_dir"}` because the
// reference can grow new keys between releases and we want the schema
// to stay forward-compatible.
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
)

// statusLineExternalConfig is the recovered AGY 1.1.27 shape of the
// `statusLine` block in settings.json. It carries `type`, `command`
// and `enabled` — the print-mode /config panel re-encodes these from
// the same struct (see print_slash.go's `jsonStatusLineSetting`), so
// /statusline and /config stay in lock-step.
//
// The clone only reads `command` and `enabled`; `type` is left alone
// because the reference renders it for the /config panel but never
// looks at it for the actual rendering path.
type statusLineExternalConfig struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Enabled bool   `json:"enabled"`
}

// loadStatusLineConfig reads the nested `statusLine` block from
// settings.json. A missing block, a missing file, or an unparseable
// block all return ("", false) so the caller can fall back to
// the built-in row without surfacing an error to the user.
//
// The function reads via auth.SettingsField so dot-notation paths
// (`statusLine.command`) hit the same reader /config does. Returning
// ok=false when the block is missing keeps the cycle-105 fallback
// (built-in row) live — no configuration, no external mode.
//
// IMPORTANT: when settings.json's `statusLine` key is a flat bool
// (the cycle-184 schema), this returns ("", false) so the
// /statusline handler takes the cycle-105 two-state toggle path.
// The two schemas are mutually exclusive — the bool form does not
// conflate with the block form, and once the block form is present
// `SetSettingsBool("statusLine", true)` would clobber it. Use
// SetSettingsField("statusLine.enabled", bool) instead.
func loadStatusLineConfig() (command string, ok bool) {
	raw, err := auth.SettingsField("statusLine")
	if err != nil || raw == nil {
		return "", false
	}
	switch v := raw.(type) {
	case map[string]any:
		cmd, _ := v["command"].(string)
		en, _ := v["enabled"].(bool)
		// `enabled=false` keeps the script declared but does not
		// arm external rendering — loadStatusLineConfig() returns
		// ok=false so the /statusline handler takes cycle-105.
		if !en {
			return "", false
		}
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			return "", false
		}
		return cmd, true
	}
	// A flat-bool `statusLine` (cycle-184 schema) falls through:
	// the script path is empty, so the cycle-105 toggle wins.
	return "", false
}

// persistStatusLineVisibility writes the cycle's visible/hidden
// state into settings.json without clobbering the statusLine block
// the script-config lives under. When a statusLine.command is
// configured, the `enabled` field doubles as the visibility flag —
// flipping it true shows the row, flipping it false hides it. When
// no command is configured the caller falls through to
// auth.SetSettingsBool (cycle-184 persistence) and this helper is
// a no-op.
//
// The helper mutates the existing block in place: it never
// overwrites `type` or `command`, and a settings.json whose
// `statusLine` key is a flat bool (cycle-184 form) is left
// untouched so legacy persistence keeps working.
func persistStatusLineVisibility(visible bool) {
	if raw, err := auth.SettingsField("statusLine"); err == nil {
		if _, ok := raw.(map[string]any); ok {
			_ = auth.SetSettingsField("statusLine.enabled", visible)
			return
		}
	}
	// No block present — leave the bool form alone, the cycle-105
	// path handles persistence.
}

// statusLinePayload is the JSON object the CLI writes to the script's
// stdin. Field order matches the reference's recovered wire form:
// `model`, `cost`, `conversation_title`, `current_dir` (the field
// names the changelog lines call out explicitly are `cost` and
// `conversation_title`; `model` and `current_dir` are the obvious
// companion keys).
//
// The struct lives in this file rather than in `internal/...` because
// it is the TUI's outward contract to a user script, not an
// internal-cross-package type. Keeping it here keeps the test
// surface local.
type statusLinePayload struct {
	Model             string  `json:"model"`
	Cost              float64 `json:"cost"`
	ConversationTitle string  `json:"conversation_title"`
	CurrentDir        string  `json:"current_dir"`
}

// buildStatusLinePayload snapshots the runMode state the script
// needs. It is a pure function (no I/O) so a test can drive it
// without spinning up a subprocess.
func (m *runMode) buildStatusLinePayload() statusLinePayload {
	model := m.statusModel
	if model == "" {
		model = m.model
	}
	prompt, completion := m.sessionTokenTotals()
	cost := pricingForModel(model, prompt, completion)
	return statusLinePayload{
		Model:             model,
		Cost:              cost,
		ConversationTitle: m.conversationTitle(),
		CurrentDir:        m.cwd,
	}
}

// pricingRow matches a model name fragment (case-insensitive substring)
// to a per-million-token input/output rate. The clone's price list is
// a best-effort approximation of the public published rate card as of
// 2026-Q4; the reference's own `jetski_cortex.ModelPricingInfo` is a
// protobuf that we cannot reach from the clone. The match is on
// substrings so a slug like `gemini-3.8-flash-low` resolves to the
// same row as the catalogue label.
type pricingRow struct {
	match          string
	inputPer1M     float64
	outputPer1M    float64
	cacheReadPer1M float64
}

// pricingTable is the ordered list of (substring, rate) pairs. Order
// matters: the first match wins, so a more-specific match needs to
// precede a less-specific one. As of this cycle the only such pair is
// `claude-opus` vs `claude-sonnet`; the `claude-opus` row has to
// precede the `claude-sonnet` row because the latter is a substring
// of the former.
//
// `minimax` resolves to zero — MiniMax M3 is a configured-only model
// the reference renders with no cost figure (the catalogue row is
// `MiniMax M3` with no `(tier)` suffix and no pricing). Treating it as
// $0 keeps the cycle-225 quota UI consistent: MiniMax M3 does not
// drain the weekly budget.
var pricingTable = []pricingRow{
	{"claude-opus", 15.0, 75.0, 15.0},
	{"claude-sonnet", 3.0, 15.0, 3.0},
	{"gpt-oss", 0.5, 1.5, 0.0},
	{"gemini-3.8-flash", 0.075, 0.30, 0.0},
	{"minimax", 0, 0, 0},
}

// pricingForModel returns the cost (USD) of accumulating prompt +
// completion tokens under the model's per-1M pricing. `cacheRead` is
// also priced because the gateway emits cache-read tokens in some
// backends, but the clone's main loop has not wired that yet — pass
// 0.
//
// The result is the *unrounded* USD total, matching the changelog
// contract. Rounding happens at the script layer (the user is in
// charge of how many digits land in their tmux status bar).
func pricingForModel(model string, prompt, completion int) float64 {
	name := strings.ToLower(strings.TrimSpace(model))
	for _, row := range pricingTable {
		if strings.Contains(name, row.match) {
			return float64(prompt)*row.inputPer1M/1_000_000 +
				float64(completion)*row.outputPer1M/1_000_000
		}
	}
	return 0
}

// sessionTokenTotals returns the prompt and completion token totals
// accumulated across every turn the runMode has handled since
// NewRunMode. The counter is bumped once per chunk with a non-nil
// `Meta` (the cycle-76 stream-meta hook in the TUI's chunk handler),
// which mirrors the way `chunk.Meta` flows in: a single Done chunk per
// turn carries the per-turn usage; we add those into the running
// totals so a session's cost is the sum of every turn's reported
// cost.
//
// The two ints are *not* the rounded quota-progress-bar value — they
// are the raw prompt/completion counters the upstream gateway
// returned. The JSON payload's `cost` is computed off them.
//
// All reads/writes go through atomic ops so the chunk handler
// goroutine and the status-line tick goroutine stay race-free. A
// stale read during a turn is harmless: the worst case is one extra
// refresh where the cost is one turn's worth behind reality.
func (m *runMode) sessionTokenTotals() (prompt, completion int) {
	if m == nil {
		return 0, 0
	}
	return int(atomic.LoadInt64(&m.sessionPromptTokens)),
		int(atomic.LoadInt64(&m.sessionCompletionTokens))
}

// recordTurnUsage adds one turn's usage to the runMode session
// counters. Called from the interactive TUI's chunk handler
// (cmd/wagyu/tui_run.go) the moment a Done chunk carries Meta. The
// caller is responsible for ignoring nil meta.
//
// The function is also a hook: callers in tests can drive it
// directly to seed the cost field without going through a real
// gateway. Print-mode does not need this — print mode has no status
// line to render.
func (m *runMode) recordTurnUsage(meta *backend.ChunkMeta) {
	if m == nil || meta == nil {
		return
	}
	atomic.AddInt64(&m.sessionPromptTokens, int64(meta.PromptTokens))
	atomic.AddInt64(&m.sessionCompletionTokens, int64(meta.CompletionTokens))
	// Cycle 292: /context reports the response tokens of every *finished*
	// step, so the step currently landing (this one) is subtracted again at
	// render time. Keeping the value here rather than in the panel means a
	// panel opened mid-turn subtracts the right step.
	atomic.StoreInt64(&m.lastTurnCompletionTokens, int64(meta.CompletionTokens))
}

// splitStatusLineCommand is the same shell-style split the cycle-103
// editor helper applies. Whitespace separates tokens; a quoted token
// is not yet needed — the recovered `command` is always a path with
// optional flags, never a path containing a space.
func splitStatusLineCommand(command string) (string, []string) {
	fields := strings.Fields(command)
	if len(fields) == 0 {
		return "", nil
	}
	return fields[0], fields[1:]
}

// runStatusLineScriptResult is what runStatusLineScript returns.
// `Output` is the rendered text; on success it ends in a single
// trailing newline (the way shell conventions expect), and on a
// non-zero exit or timeout it is the cached *previous* value so the
// caller can keep showing the last good render while a misbehaving
// script recovers.
type runStatusLineScriptResult struct {
	Output string
	Err    error
}

// runStatusLineScript shells out the configured command, writes the
// JSON payload to stdin, and reads stdout. It is a single-shot call
// (no ticker inside); the TUI arms a 100 ms tick that calls it on
// each fire. The 1-second timeout protects the render path from a
// hung script; on timeout the previous good output is returned so
// the row stays populated.
//
// The spawned subprocess inherits the user's PATH (via
// os.Environ()); the payload is fed through a Pipe so we own the
// write side and can close it without deadlocking. The recovered
// wire form is stdin-JSON (the cycle-227 capture confirms this —
// see /tmp/agycmp/statusline_probe.py), so we deliberately do not
// also expose the payload as an environment variable.
func runStatusLineScript(command string, payload []byte) runStatusLineScriptResult {
	name, args := splitStatusLineCommand(command)
	if name == "" {
		return runStatusLineScriptResult{Err: fmt.Errorf("status-line command is empty")}
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...)
	cmd.Env = os.Environ()
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return runStatusLineScriptResult{Err: fmt.Errorf("stdin pipe: %w", err)}
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return runStatusLineScriptResult{Err: fmt.Errorf("spawn %q: %w", name, err)}
	}
	// Feed the payload in its own goroutine so a blocked writer
	// cannot deadlock with the 1 s context cancel.
	writeErr := make(chan error, 1)
	go func() {
		_, werr := stdin.Write(payload)
		writeErr <- werr
		_ = stdin.Close()
	}()
	waitErr := cmd.Wait()
	if werr := <-writeErr; werr != nil && waitErr == nil {
		// A stdin write error after the process has exited is not
		// interesting — the script already saw EOF.
		waitErr = nil
	}
	if waitErr != nil {
		// Non-zero exit or timeout. Surface a one-line stderr note
		// to the on-screen log so the user has a chance to debug
		// a misbehaving script. The output buffer is empty in
		// every path that produces a non-nil error here.
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		return runStatusLineScriptResult{Err: fmt.Errorf("status-line script failed: %s", msg)}
	}
	out := strings.TrimRight(stdout.String(), "\n")
	if out == "" {
		return runStatusLineScriptResult{Err: fmt.Errorf("status-line script produced no output")}
	}
	return runStatusLineScriptResult{Output: out}
}

// statusLineExternalState is the per-runMode state the external
// rendering path reads. Splitting it out keeps the runMode struct's
// surface small — only one extra pointer field is added there. The
// state itself owns:
//
//   - the cached rendered output (so a script error does not blank
//     the row)
//   - the atomic tick counter (so a tick fires the spawn at most once
//     per 100 ms regardless of how many bubbletea frames have elapsed)
//   - the stderr-once buffer (so a misbehaving script only spams the
//     transcript once, the first time it errors)
type statusLineExternalState struct {
	renderMu        sync.Mutex
	rendered        string
	lastErr         error
	lastErrReported bool
	// tickCount is incremented by the timer handler and consumed by
	// the renderer to decide whether to spawn the subprocess. Atomic
	// because the timer goroutine and the view goroutine both touch
	// it.
	tickCount int64
}

// markTick increments the tick counter. Returns the new value.
func (s *statusLineExternalState) markTick() int64 {
	if s == nil {
		return 0
	}
	return atomic.AddInt64(&s.tickCount, 1)
}

// snapshotTick reads the current tick counter.
func (s *statusLineExternalState) snapshotTick() int64 {
	if s == nil {
		return 0
	}
	return atomic.LoadInt64(&s.tickCount)
}

// setRendered updates the cached rendered output. It records the
// first-ever stderr message so a misbehaving script does not spam
// the transcript on every refresh.
func (s *statusLineExternalState) setRendered(out string, err error) {
	if s == nil {
		return
	}
	s.renderMu.Lock()
	defer s.renderMu.Unlock()
	s.rendered = out
	if err != nil && !s.lastErrReported {
		s.lastErr = err
		s.lastErrReported = true
	} else if err == nil {
		s.lastErr = nil
		s.lastErrReported = false
	}
}

// current returns the cached output and the once-recorded error. The
// caller is expected to drain the error through the transcript if it
// is non-nil — the helper itself does not write to the transcript.
func (s *statusLineExternalState) current() (out string, err error) {
	if s == nil {
		return "", nil
	}
	s.renderMu.Lock()
	defer s.renderMu.Unlock()
	return s.rendered, s.lastErr
}

// externalStateFor returns the runMode's statusLineExternalState, or
// nil if it has not been allocated. Tests that poke at the external
// path without going through NewRunMode rely on this returning nil
// rather than panicking.
func (m *runMode) externalStateFor() *statusLineExternalState {
	if m == nil {
		return nil
	}
	return m.statusLineExternalState
}

// ensureExternalState allocates m.statusLineExternalState the first
// time the external mode is activated. The function is idempotent —
// every subsequent call returns the same pointer.
func (m *runMode) ensureExternalState() *statusLineExternalState {
	if m == nil {
		return nil
	}
	if m.statusLineExternalState == nil {
		m.statusLineExternalState = &statusLineExternalState{}
	}
	return m.statusLineExternalState
}

// runExternalStatusLineTick is the per-tick goroutine entry point.
// It calls drainExternalStatusLine to pick up any ticks that have
// accumulated since the last drain, and runs the spawn only when the
// drain reports work owed (i.e. a tick has fired and a command is
// still configured). The whole call must be safe to invoke even
// when external mode is not active — in that case drain returns
// ("", false) and the function is a no-op.
//
// The function lives outside the bubbletea Update chain on purpose:
// the subprocess can take up to 1 second, and a synchronous spawn
// would block every subsequent Update call behind it.
func (m *runMode) runExternalStatusLineTick() {
	if m == nil {
		return
	}
	command, ok := m.drainExternalStatusLine()
	if !ok {
		return
	}
	m.runExternalStatusLineRender(command)
}

// runExternalStatusLineRender is the per-tick entry point. It
// snapshots the runMode state, marshals the payload, spawns the
// script and stores the rendered output. It is *not* a tea.Cmd —
// the caller wraps it in one and is responsible for re-arming.
//
// The function takes the script command as an explicit argument so
// tests can swap in a tempdir script without going through
// settings.json.
func (m *runMode) runExternalStatusLineRender(command string) {
	if m == nil {
		return
	}
	state := m.ensureExternalState()
	payload, err := json.Marshal(m.buildStatusLinePayload())
	if err != nil {
		state.setRendered("", fmt.Errorf("status-line payload marshal: %w", err))
		return
	}
	res := runStatusLineScript(command, payload)
	if res.Err != nil {
		state.setRendered("", res.Err)
		return
	}
	state.setRendered(res.Output, nil)
}

// statusLineExternalTickMsg fires every 100 ms while the external
// status line is active. The handler in tui_run.go advances the tick
// counter and spawns the script. We deliberately do not call
// runExternalStatusLineRender directly from the timer goroutine:
// doing so would race with the bubbletea runtime's re-entry rules
// (m.runExternalStatusLineRender is safe to call concurrently with
// View(), but the spawn itself can take 100 ms, which would block
// the next tick). The handler instead spawns a goroutine and
// re-arms the next tick.
type statusLineExternalTickMsg struct{}

// armExternalStatusLineTick arms the next statusLineExternalTickMsg.
// It returns the tea.Cmd form so the caller can plant it directly
// into the bubbletea Update chain. The 100 ms cadence mirrors the
// cycle-tasks-panel tick the same way the tasks path does (see
// armTasksTick in slash_tasks_panel.go).
func armExternalStatusLineTick() tea.Cmd {
	return tea.Tick(100*time.Millisecond, func(time.Time) tea.Msg { return statusLineExternalTickMsg{} })
}

// drainExternalStatusLine drains the tick counter and returns true
// when a new spawn is owed. Each call resets the counter, so a tick
// handler that is not yet scheduled still finds 0 here (the next
// arming will pick up the next tick). The signature mirrors
// consumeTasksDirty so the call sites stay in family.
func (m *runMode) drainExternalStatusLine() (command string, ok bool) {
	if m == nil {
		return "", false
	}
	state := m.ensureExternalState()
	if state.snapshotTick() == 0 {
		return "", false
	}
	atomic.StoreInt64(&state.tickCount, 0)
	cmd, ok := loadStatusLineConfig()
	if !ok {
		return "", false
	}
	return cmd, true
}

// externalRenderOK reports whether the external status line is
// currently producing a rendered output. Used by statusRow() to
// decide between the cached external render and the built-in
// fallback.
func (m *runMode) externalRenderOK() bool {
	if m == nil {
		return false
	}
	if !m.statusLineExternal || m.statusLine == "off" {
		return false
	}
	state := m.externalStateFor()
	if state == nil {
		return false
	}
	out, _ := state.current()
	return strings.TrimSpace(out) != ""
}

// externalRenderedForRow returns the rendered output to substitute
// into the status row when external mode is active. It returns "" when
// the built-in row should be used (no command configured, external
// mode disabled, or the script has not yet produced any output).
// statusRow() keys off the empty-string return because that is the
// value that means "fall back to the built-in template".
func (m *runMode) externalRenderedForRow() (string, error) {
	if !m.externalRenderOK() {
		return "", nil
	}
	state := m.externalStateFor()
	if state == nil {
		return "", nil
	}
	out, lastErr := state.current()
	if strings.TrimSpace(out) == "" {
		return "", nil
	}
	return out, lastErr
}
