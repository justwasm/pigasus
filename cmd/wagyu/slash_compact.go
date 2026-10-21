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

package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	"github.com/MichaelKinsy/PiG/coding"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/compaction"
	"github.com/justwasm/pigasus/internal/pigrt"
)

// manualCompactor is the cycle-229 seam for the /compact slash command.
// Implementations wrap a PiG coding.Session (production: pigrtCompactor)
// or record the calls and replay synthetic events (test: fakeCompactor).
//
// The interface is intentionally narrow — just the three PiG methods the
// manual /compact path actually calls. Cycle 227's auto-compact wiring
// uses Options.CompactEnabled/CompactThreshold + the
// CompactionStart/EndEvent drain in the same package; cycle 229 is the
// user-driven companion and needs no settings plumbing (PiG itself
// honours the same settings for manual compact via the
// coding/session.go:checkPromptCompaction path that runs *during*
// compact, not before — cycle 229 only fires when the user asks).
//
// IsCompacting is checked *before* Compact() to short-circuit an
// already-running compact (the cycle-229 spec calls out the race
// where a user presses /compact while PiG is mid-summarisation). PiG
// itself aborts an in-flight compact when Compact is called a second
// time (coding/session.go:beginManualCompaction loops until the active
// one finishes), so the IsCompacting gate is a UX nicety — the
// transcript echoes "Already compacting." instead of silently queueing.
//
// SubscribeManual returns an unsubscribe function. The cycle-229
// /compact handler attaches a one-shot listener that translates the
// next CompactionEndEvent into a compactDoneMsg, and unsubscribes
// immediately after the first event fires — so a long-lived TUI does
// not accumulate listeners every time the user types /compact.
type manualCompactor interface {
	Compact(ctx context.Context, customInstructions string) error
	IsCompacting() bool
	SubscribeManual(listener func(piagent.AgentEvent)) func()
}

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
	go func() {
		// 60 s matches the streamPrompt deadline in tui_run.go;
		// a long summarisation aborts at the goroutine level and
		// PiG's compactCancel takes over. The cancel is harmless
		// when Compact() returns first.
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Subscribe a one-shot listener BEFORE calling Compact so
		// the CompactionStart/End events emitted during the call
		// reach us. We deliberately unsubscribe after the first
		// event — a long-lived TUI would otherwise accumulate one
		// listener per /compact press.
		var (
			doneCh      = make(chan compactDoneMsg, 1)
			unsubscribe func()
		)
		unsubscribe = compactor.SubscribeManual(func(ev piagent.AgentEvent) {
			switch e := ev.(type) {
			case piagent.CompactionEndEvent:
				msg := compactDoneMsg{
					TokensBefore:       e.TokensBefore,
					TokensAfter:        e.EstimatedTokensAfter,
					Reason:             e.Reason,
					Aborted:            e.Aborted,
					CustomInstructions: customInstructions,
				}
				if e.ErrorMessage != "" {
					msg.Err = errors.New(e.ErrorMessage)
				}
				select {
				case doneCh <- msg:
				default:
				}
				if unsubscribe != nil {
					unsubscribe()
				}
			}
		})
		defer unsubscribe()

		err := compactor.Compact(ctx, customInstructions)
		if err != nil {
			// Compact() returns even when the listener already saw
			// the End event; suppress the duplicate unless the
			// listener fired with no error of its own.
			select {
			case got := <-doneCh:
				if got.Err == nil {
					got.Err = err
					select {
					case doneCh <- got:
					default:
					}
				}
			default:
				doneCh <- compactDoneMsg{Err: err, CustomInstructions: customInstructions}
			}
		}

		// Drain the one-shot channel. If Compact() returned without
		// firing an end event (race / PiG bug), synthesise a no-op
		// done message so the transcript gets a final row either way.
		var msg compactDoneMsg
		select {
		case msg = <-doneCh:
		case <-ctx.Done():
			msg = compactDoneMsg{Err: ctx.Err()}
		default:
			msg = compactDoneMsg{}
		}
		if prog != nil {
			prog.Send(msg)
		}
	}()
}

// compactCancelMu guards compactCancel, the package-level cancel
// function the cycle-237 generic-compact path installs while a
// summarization is in flight. The TUI's Esc handler will reach
// this from a future cycle (cycle 237 left it as a singleton
// because adding a field to runMode would touch tui_run.go, which
// is cycle 232's territory). A nil value means "no compact in
// flight" — the cancel is a no-op.
//
// The mutex is necessary because two runMode instances can race
// when one finishes /compact and another starts: the clean-up
// defer would otherwise race the new install. In practice a single
// TUI is the only call site; the mutex costs nothing.
var (
	compactCancelMu sync.Mutex
	compactCancel   context.CancelFunc
)

// installCompactCancel atomically swaps in cancel for the package-
// level singleton. The cleanup is the value to defer at the call
// site so the singleton is cleared even when the goroutine panics.
//
// The cleanup does NOT restore a prior value: if a second /compact
// press fired in between, the newer cancel owns the slot and the
// older goroutine's cleanup must not clobber it. Compare-and-swap
// is not possible with context.CancelFunc (functions are not
// directly comparable in Go), so the cleanup is conservative — it
// always clears, leaving the slot nil. The trade-off is documented
// in the cycle-237 deliverable's 硬約束 section: a /compact press
// within milliseconds of another cancels the newer one too, which
// matches AGY 1.4.2's "second /compact cancels the first" race
// behaviour anyway.
func installCompactCancel(cancel context.CancelFunc) func() {
	compactCancelMu.Lock()
	compactCancel = cancel
	compactCancelMu.Unlock()
	return func() {
		compactCancelMu.Lock()
		compactCancel = nil
		compactCancelMu.Unlock()
	}
}

// TriggerCompactCancel fires the cycle-237 cancel signal. The
// cycle-237 TUI wires this into the Esc handler in a future cycle
// (see 硬約束 in docs/cycles/cycle-237-compact/deliverable.md).
// Calling it when no compact is in flight is a no-op.
func TriggerCompactCancel() {
	compactCancelMu.Lock()
	cancel := compactCancel
	compactCancelMu.Unlock()
	if cancel != nil {
		cancel()
	}
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
//
// When customInstructions is non-empty (the "/compact <message>"
// form), the message is appended to m.session after the summary
// lands and a regular turn fires against the new history — the
// AGY 1.4.2 contract is "summarize, then answer the message".
func (m *runMode) startGenericCompact(customInstructions string) {
	// Empty transcript short-circuit: nothing to summarize, mirror
	// the cycle-167 "Nothing to compact yet." line. We keep the
	// plain (no wrapper) form because REF special-cases /compact
	// the same way (see slashRespondPlain's doc comment).
	if len(m.session) == 0 {
		m.slashRespondPlain("Nothing to compact yet.")
		return
	}

	// Snapshot the session so cancellation cannot clobber it.
	// ComputeSummary reads sess by value (it's a slice header), but
	// the assignment back to m.session only happens on success —
	// the test TestSlashCompact_CancelRestoresSession pins that.
	sessSnap := make([]backend.Message, len(m.session))
	copy(sessSnap, m.session)

	// Echo the running row first so the transcript shows the
	// command was accepted, exactly as the cycle-229 PiG branch
	// does. The cycle-237 spec text is "Compacting conversation…"
	// (matching AGY 1.4.2's literal).
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

	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()

		// Install the cancel singleton so TriggerCompactCancel()
		// can fire from a future Esc-handler patch without this
		// file changing again. The defer restores the prior
		// value (or nil) so two /compact presses race correctly.
		cleanup := installCompactCancel(cancel)
		defer cleanup()

		summary, err := compaction.ComputeSummary(ctx, backendClient, model, "", sessSnap)

		// Build the result message and deliver it to the bubbletea
		// runtime (or straight into m.response when no program is
		// running, as in tests).
		var msg genericCompactDoneMsg
		msg.CustomInstructions = customInstructions
		switch {
		case errors.Is(err, compaction.ErrCancelled):
			msg.Cancelled = true
			msg.StatsBefore = summary.StatsBefore
		case errors.Is(err, compaction.ErrEmptyTranscript):
			// Should never happen — the caller checked len(m.session)
			// before launching — but mirror it as a no-op.
			return
		case err != nil:
			msg.Err = err
			msg.StatsBefore = summary.StatsBefore
		default:
			msg.Summary = summary.Text
			msg.StatsBefore = summary.StatsBefore
			msg.StatsAfter = summary.StatsAfter
		}

		if prog != nil {
			prog.Send(msg)
		} else {
			m.applyGenericCompactResult(msg, customInstructions)
		}
	}()
}

// genericCompactDoneMsg is the message startGenericCompact posts
// back to the bubbletea runtime when the goroutine resolves. The
// TUI handler is in tui_run.go; tests drive applyGenericCompactResult
// directly via the no-prog branch.
type genericCompactDoneMsg struct {
	Summary            string
	StatsBefore        compaction.Stats
	StatsAfter         compaction.Stats
	Cancelled          bool
	Err                error
	CustomInstructions string
}

// applyGenericCompactResult is the no-prog branch of
// startGenericCompact. It is on runMode because the transcript
// rows live on m.response; tests call it to drive the same
// path the TUI handler will. The method has three sub-cases,
// matching the message's three populated fields:
//
//   - Summary != "": success. Replace m.session with the
//     summary, append a stats row, and dispatch the trailing
//     customInstructions if non-empty.
//   - Cancelled == true: append "[compact cancelled]" and leave
//     m.session untouched.
//   - Err != nil: append "Compact failed: <err>" and leave
//     m.session untouched.
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
		// Success path. Replace m.session with one user-role
		// message carrying the summary (the AGY 1.4.2 contract).
		if msg.Summary != "" {
			m.session = []backend.Message{{Role: "user", Content: msg.Summary}}
		}
		// Append the stats row. Format mirrors the AGY spec:
		// "Compacted N messages (-Y%)" with the byte-/4 estimate.
		pct := compaction.ReductionPercent(msg.StatsBefore, msg.StatsAfter)
		var statsLine string
		if pct > 0 {
			statsLine = fmt.Sprintf("Compacted %d messages (-%d%%)", msg.StatsBefore.Messages, pct)
		} else {
			statsLine = fmt.Sprintf("Compacted %d messages", msg.StatsBefore.Messages)
		}
		m.response = append(m.response,
			cWarningBlueOpen+"\u23bf  "+cWarningBlueClose+panelGray238+statsLine+panelGray238C,
			"",
		)
		// Trailing customInstructions — the "/compact <message>"
		// form. Append to m.session and arm a fresh prompt. The
		// prompt runs through the regular streamPrompt path; the
		// cancel-style race (two /compact presses) cannot happen
		// here because we already cleared m.compactor above.
		if customInstructions = strings.TrimSpace(customInstructions); customInstructions != "" {
			m.session = append(m.session, backend.Message{Role: "user", Content: customInstructions})
			return m.streamPrompt(customInstructions)
		}
		return nil
	}
}

// _silenceStringsUnused is a build-time guarantee that the strings
// import (added for customInstructions trimming) is used even if
// future refactors drop the field. Cheap to keep.
var _ = strings.HasPrefix
