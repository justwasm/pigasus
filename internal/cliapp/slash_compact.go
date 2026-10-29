// slash_compact.go — cycle 229 seam between the manual /compact
// slash command and PiG's coding.Session.Compact(), extended by
// cycle 237 with a generic summarizer path.
//
// Cycle 229 closes the PiG loop: the manual /compact slash command
// fires PiG's Compact(ctx, customInstructions), listens for the
// matching CompactionEndEvent, and surfaces the upstream token counts
// in the transcript. AGY 1.4.0 builds on the same API — strings like
// `summaryCache` and `cascade_config.checkpoint_config.max_token_limit`
// (recovered in docs/research/agy-auto-compact-vs-pig.md) are the
// upstream knobs PiG already exposes through Session.Compact.
//
// Cycle 237 closes the AGY 1.4.2 gap: the reference binary shipped
// `/compact <message>` (summarize, then answer the user's appended
// question) and a "Compacting conversation..." progress label with
// Esc-cancel. The cycle-229 PiG seam covers the production runtime;
// cycle 237 adds a parallel path that does not require a pigrt
// runtime, so offline tests / print mode / direct handleSlashCommand
// callers see the cycle-237 "Compacted N messages (-Y%)" echo
// instead of the cycle-167 half-trim stub.
//
// The seam is `manualCompactor` — a three-method interface so the
// production wiring (a thin adapter over *pigrt.Runtime) and the
// test wiring (a fakeCompactor that records calls and replays
// synthetic events) compose in the same handleSlashCommand path.
// runMode holds one compactor; cycle 227 populates it when the
// pigrt.Runtime TUI integration lands, and cycle 229 leaves it nil
// for callers that have not wired PiG yet (print mode, offline
// tests). Cycle 237 falls back to internal/compaction.ComputeSummary
// when the compactor is nil — the cycle-167 half-trim helper is
// retired.
//
// The pigrtCompactor adapter lives next to the interface so adding
// later cycles (e.g. per-cycle TUI plumbing) does not have to read
// this file twice; the adapter is small and the test for it is in
// slash_cycle229_test.go.
//
// Cycle 237 deliberately does NOT touch internal/pigrt/runtime.go,
// internal/agent/pig.go, or cmd/wagyu/tui_run.go (cycle 232's
// territory). The Esc-cancel hook uses a package-level channel
// (compactCancel) so the TUI's existing Esc handler can fire it
// from a future cycle without this file changing again — see the
// 硬約束 section of the cycle-237 deliverable for the trade-off.

package cliapp

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	piagent "github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/coding"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/compaction"
	"github.com/justwasm/pigasus/internal/pigrt"
	"github.com/justwasm/pigasus/internal/tools"
)

// manualCompactor is aliased to compaction.ManualCompactor.
type manualCompactor = compaction.ManualCompactor

// pigrtCompactor is the production implementation: it forwards the
// three calls straight to the underlying PiG session. cycle 229 ships
// this adapter so the print / TUI plumbing in a later cycle (the
// cycle-227 worktree already populates m.compactor for the print path)
// can wire it without revisiting slash_compact.go.
type pigrtCompactor struct {
	rt *pigrt.Runtime
}

// newPigrtCompactor wraps a *pigrt.Runtime into a manualCompactor. The
// runtime must already have a live session; newPigrtCompactor panics
// if rt is nil because every call would dereference nil — the seam
// only exists so callers who have a runtime can wire one up.
func newPigrtCompactor(rt *pigrt.Runtime) *pigrtCompactor {
	if rt == nil {
		panic("newPigrtCompactor: nil runtime")
	}
	return &pigrtCompactor{rt: rt}
}

func (p *pigrtCompactor) Compact(ctx context.Context, customInstructions string) error {
	// PiG 0.5.1 returns (*coding.CompactionResult, error); the manual
	// compactor's contract only surfaces the error.
	_, err := p.rt.Session().Compact(ctx, customInstructions)
	return err
}

func (p *pigrtCompactor) IsCompacting() bool {
	return p.rt.Session().IsCompacting()
}

func (p *pigrtCompactor) SubscribeManual(listener func(piagent.AgentEvent)) func() {
	return p.rt.Session().Subscribe(listener)
}

// sessionForTest exposes the underlying coding.Session for tests that
// need to drive a real PiG session through the pigrt runtime instead
// of the fakeCompactor. The pigrt_compactor_test.go in cycle 227 uses
// this pattern (rt.Session().Subscribe + rt.InjectForTest); cycle 229
// adopts the same surface so the two cycles share test scaffolding.
//
// `sessionForTest` is unexported to keep the production seam narrow —
// nothing outside cmd/wagyu should reach into the session directly.
// Tests in this package (which is `package main`, so they have full
// access) call it via the unexported helper.
func (p *pigrtCompactor) sessionForTest() *coding.Session {
	return p.rt.Session()
}

// startManualCompact is the /compact slash-command body. It is on
// runMode (not a free function) because the transcript rows live on
// m.response, and the cycle-229 spec calls for two consecutive rows
// ("Compacting context…" + "Compacted: …") styled as a single
// bracketed event.
//
// Cycle 237 split the old "Branch 1" (cycle-167 stub) into a real
// summarizer that calls internal/compaction.ComputeSummary
// directly. The cycle-167 half-trim of m.session is gone; the new
// path replaces m.session with one user-role message carrying the
// model's reply (matching AGY 1.4.2's "the transcript becomes the
// summary" contract).
//
// The method has four branches:
//
//   - m.compactor == nil: cycle-237 generic summarizer. Fires
//     compaction.ComputeSummary in a goroutine with a cancellable
//     context (cycle-237's local channel). The transcript gets
//     "Compacting conversation…" (matching the AGY 1.4.2 spec)
//     and a follow-up "Compacted N messages (-Y%)" / "[compact
//     cancelled]" row when the goroutine resolves. A trailing
//     user-supplied customInstructions (the "/compact <message>"
//     form) is dispatched as a fresh prompt after the summary
//     lands.
//
//   - m.compactor != nil && m.compactor.IsCompacting() == true:
//     short-circuit with "⎿  Already compacting." — the cycle-229
//     spec calls out the race where a user types /compact while
//     PiG is mid-summarisation (auto-compact from cycle 227 OR a
//     second /compact press before the first finishes). PiG
//     itself would abort the in-flight compact and start a new
//     one; the gate keeps the transcript honest.
//
//   - happy path (cycle-229): emit "⎿  Compacting context…" +
//     custom-instructions echo, subscribe a one-shot listener on
//     the compactor, and fire Compact(ctx, customInstructions) in
//     a goroutine. The goroutine posts a compactDoneMsg back to
//     the bubbletea runtime so the transcript gets the success /
//     failure row.
//
//   - happy path (cycle-237 fallback): emit "⎿  Compacting
//     conversation…" + custom-instructions echo, run the generic
//     summarizer with a 60 s timeout, replace m.session with the
//     summary, and echo the stats. The same cancel channel the
//     Esc handler will reach from a future cycle (cycle 237 left
//     it as a package-level singleton — see compactCancel below)
//     aborts the summarization without touching m.session.
//
// The Compact() / ComputeSummary calls use a 60 s timeout — the
// same window the TUI prompt stream uses (streamPrompt in
// tui_run.go). Long compactions that exceed it are aborted at
// the goroutine level; PiG's own compactCancel path
// (coding/session.go:compactCancel) takes over from there.
func (m *runMode) startManualCompact(customInstructions string) {
	// Branch 1 (cycle-237): no PiG compactor wired. Use the generic
	// summarizer. The cycle-167 half-trim is retired — the new path
	// replaces m.session with one user-role message carrying the
	// model's reply, matching AGY 1.4.2's "transcript becomes the
	// summary" contract.
	if m.compactor == nil {
		m.startGenericCompact(customInstructions)
		return
	}

	// Branch 2: an auto-compact (cycle 227) or a manual press from
	// a moment ago is still running. PiG's coding.Session would
	// queue / abort-and-restart, but the cycle-229 spec is explicit:
	// echo "Already compacting." and skip the call so the user
	// sees a clear "I heard you, but I am already busy" line.
	if m.compactor.IsCompacting() {
		m.response = append(m.response,
			cWarningBlueOpen+"\u23bf  "+cWarningBlueClose+panelGray238+"Already compacting."+panelGray238C,
			"",
		)
		return
	}

	// Branch 3: cycle-229 happy path. Echo the running row first so
	// the transcript shows the command was accepted, then arm the
	// one-shot listener and kick off Compact() in the background.
	//
	// `m.prog` is the running *tea.Program; nil in tests, in which
	// case the result is delivered straight into m.response (the
	// goroutine skips the prog.Send call). The split is the same
	// shape cycle 227's compactStartMsg / compactEndMsg handlers
	// use — cycle 229's manual path just collapses the start/end
	// pair into a single two-row transcript block.
	m.response = append(m.response,
		cWarningBlueOpen+"\u23bf  "+cWarningBlueClose+panelGray238+"Compacting context\u2026"+panelGray238C,
		"",
	)
	compactor := m.compactor
	prog := m.prog
	go runManualCompactionTask(compactor, customInstructions, prog)
}

func handleCompactorResult(err error, customInstructions string, doneCh chan compactDoneMsg) {
	compaction.HandleManualResult(err, customInstructions, doneCh)
}

func runManualCompactionTask(compactor manualCompactor, customInstructions string, prog *tea.Program) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	compaction.RunManualTask(ctx, compactor, customInstructions, func(msg compactDoneMsg) {
		if prog != nil {
			prog.Send(msg)
		}
	})
}

// installCompactCancel delegates to compaction.InstallCancel.
func installCompactCancel(cancel context.CancelFunc) func() {
	return compaction.InstallCancel(cancel)
}

// TriggerCompactCancel fires the cycle-237 cancel signal. The
// cycle-237 TUI wires this into the Esc handler in a future cycle
// (see 硬約束 in docs/cycles/cycle-237-compact/deliverable.md).
// Calling it when no compact is in flight is a no-op.
func TriggerCompactCancel() {
	compaction.TriggerCancel()
}

// startGenericCompact is the cycle-237 path: no PiG compactor, so
// the slash command drives internal/compaction.ComputeSummary
// directly. The transcript gets:
//
//	⎿  Compacting conversation…                       (running row, cycle-237)
//	⎿  Compacted 12 messages (-87%). [follow-up row]  (success)
//	or
//	⎿  [compact cancelled]                            (Esc / timeout)
//
// The session is replaced with one user-role message carrying the
// summary, so the next model turn sees only the summary in its
// history. The original session is preserved verbatim until the
// goroutine resolves, so a cancellation does not clobber the
// user's history (TestSlashCompact_CancelRestoresSession).
type genericCompactDoneMsg = compaction.GenericDoneMsg

func runGenericCompactionTask(backendClient backend.Client, model, systemPrompt string, overhead int, sessSnap []backend.Message, customInstructions string, started time.Time, prog *tea.Program, onDone func(genericCompactDoneMsg)) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	cleanup := compaction.InstallCancel(cancel)
	defer cleanup()

	compaction.RunGenericTask(ctx, backendClient, model, systemPrompt, overhead, sessSnap, customInstructions, started, func(msg genericCompactDoneMsg) {
		if prog != nil {
			prog.Send(msg)
		} else if onDone != nil {
			onDone(msg)
		}
	})
}

func (m *runMode) startGenericCompact(customInstructions string) {
	if len(m.session) == 0 {
		m.slashRespondPlain("Nothing to compact yet.")
		return
	}

	sessSnap := make([]backend.Message, len(m.session))
	copy(sessSnap, m.session)

	m.response = append(m.response,
		cWarningBlueOpen+"\u23bf  "+cWarningBlueClose+panelGray238+"Compacting conversation\u2026"+panelGray238C,
		"",
	)

	backendClient := m.backend
	model := m.model
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	prog := m.prog
	started := time.Now()

	systemPrompt := m.systemPrompt()
	overhead := compaction.ContextOverheadTokens(systemPrompt, modelDeclarationsJSON())

	if prog != nil {
		// Production: the result travels as a tea.Msg and is applied by
		// the Update loop, on the UI goroutine.
		go runGenericCompactionTask(backendClient, model, systemPrompt, overhead, sessSnap, customInstructions, started, prog, nil)
		return
	}
	// No program to post to (the test path). Hand the result over a
	// channel instead: the worker never touches m.response or m.session,
	// which belong to the goroutine that owns the runMode.
	ch := make(chan genericCompactDoneMsg, 1)
	m.compactDone = ch
	go runGenericCompactionTask(backendClient, model, systemPrompt, overhead, sessSnap, customInstructions, started, nil, func(msg genericCompactDoneMsg) {
		ch <- msg
	})
}

func modelDeclarationsJSON() []byte {
	decls := tools.ModelDeclarations()
	b, err := json.Marshal(decls)
	if err != nil {
		return nil
	}
	return b
}

func computeCompactTokens(msg genericCompactDoneMsg, carriedTokens int) (int, int, int) {
	return compaction.ComputeCompactTokens(msg.StatsBefore.Tokens, msg.InputTokens, msg.StatsAfter.Tokens, msg.OutputTokens, msg.Overhead, msg.Summary, carriedTokens)
}

func (m *runMode) applyGenericCompactSuccess(msg genericCompactDoneMsg, customInstructions string) tea.Cmd {
	// What the summary carries into the next prompt is counted beside
	// the summary in the "after" figure — the earlier requests and the
	// snapshots the reference's checkpoint carries. The clone's resume
	// block carries the requests; it has no artifact, subagent or
	// running-task snapshots.
	carriedTokens := 0
	if msg.Summary != "" {
		requests := compaction.UserRequests(m.session, compaction.MaxUserRequests)
		carriedTokens = compaction.EstimateTokens(compaction.CarriedBlock(requests))
		m.session = []backend.Message{{
			Role:    "user",
			Content: compaction.ResumingBlock(requests, msg.Summary),
		}}
	}
	beforeTokens, afterTokens, summaryTokens := computeCompactTokens(msg, carriedTokens)
	m.response = append(m.response,
		compaction.DividerRow(m.width),
		compaction.StatsRow(m.width,
			msg.StatsBefore.Messages,
			beforeTokens,
			afterTokens,
			summaryTokens,
			msg.Took),
		"",
	)
	if customInstructions = strings.TrimSpace(customInstructions); customInstructions != "" {
		m.session = append(m.session, backend.Message{Role: "user", Content: customInstructions})
		return m.streamPrompt(customInstructions)
	}
	return nil
}

func (m *runMode) applyGenericCompactResult(msg genericCompactDoneMsg, customInstructions string) tea.Cmd {
	switch {
	case msg.Cancelled:
		m.response = append(m.response,
			cWarningBlueOpen+"\u23bf  "+cWarningBlueClose+panelGray238+"[compact cancelled]"+panelGray238C,
			"",
		)
		return nil
	case msg.Err != nil:
		m.response = append(m.response,
			cWarningBlueOpen+"\u23bf  "+cWarningBlueClose+panelGray238+"Compact failed: "+msg.Err.Error()+panelGray238C,
			"",
		)
		return nil
	default:
		return m.applyGenericCompactSuccess(msg, customInstructions)
	}
}
