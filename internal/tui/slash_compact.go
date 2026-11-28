package tui

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/session/convstore"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/infra/memtrim"
	"github.com/justwasm/pigasus/internal/ui/rows"
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"
)

// ---- slash_compact.go ----

// slash_compact.go — the /compact slash command.
//
// There is one path. `/compact` snapshots the session, runs the generic
// summarizer over it (internal/compaction.RunGenericTaskWithTools), and
// replaces the session with one user-role message carrying the resume
// block: the reference's "the transcript becomes the summary" contract.
//
// Cycle 229 shipped a second path behind a `manualCompactor` seam, meant to
// drive PiG's coding.Session.Compact(). It never had a production
// implementation. Cycle 381 removed the unreachable `pigrtCompactor`
// adapter and left the seam behind as an open question: either the TUI
// grows a pigrt.Runtime and the seam is wired, or the seam goes too. The
// pigrt package is gone from the tree and from go.mod, so the first option
// has no carrier left. The seam is deleted here.
//
// What that removes, and why none of it was load-bearing:
//
//   - The "Already compacting." short-circuit. It was gated on
//     Compactor().IsCompacting(), which is always nil in production, so the
//     branch could never fire.
//   - The manual result path (compactDoneMsg -> updateCompactDoneMsg ->
//     appendCompactResult). It drew a divider and a stats row, which the
//     generic path draws too — with the summary size and the elapsed time
//     the manual path passed as "absent", plus the cancelled branch it did
//     not have, plus the session replacement it never did.
//
// `/compact <message>` still summarizes and then answers the message: the
// generic path appends it and streams it, as before.

// startManualCompact is the /compact slash-command body.
//
// It is a delegate, and deliberately nothing more: there is one compaction
// path, so a second function with branches would only be a place for the
// two to drift apart. The comment above records what the other path was.
func (m *RootModel) startManualCompact(customInstructions string) tea.Cmd {
	return m.startGenericCompact(customInstructions)
}

// startGenericCompact is the cycle-237 path: no PiG compactor, so
// the slash command drives internal/compaction.ComputeSummary
// directly. The transcript gets:
//
//	⎿  Compacting conversation…                       (running row, cycle-237)
//	⎿  Compacted 12 messages (-87%). [follow-up row]  (success)
//	or
//	⎿  Compaction cancelled. Conversation was not compacted. (Esc / timeout)
//
// The session is replaced with one user-role message carrying the
// summary, so the next model turn sees only the summary in its
// history. The original session is preserved verbatim until the
// goroutine resolves, so a cancellation does not clobber the
// user's history (TestSlashCompact_CancelRestoresSession).
type genericCompactDoneMsg = compaction.GenericDoneMsg

func runGenericCompactionTask(ctx context.Context, backendClient backend.Client, model, systemPrompt string, overhead int, sessSnap []backend.Message, tools []backend.ToolDecl, customInstructions string, started time.Time, prog *tea.Program, onDone func(genericCompactDoneMsg)) {
	compaction.RunGenericTaskWithTools(ctx, backendClient, model, systemPrompt, overhead, sessSnap, tools, customInstructions, started, func(msg genericCompactDoneMsg) {
		if prog != nil {
			prog.Send(msg)
		} else if onDone != nil {
			onDone(msg)
		}
	})
}

// beginGenericCompaction creates the context the worker runs under and installs
// its cancel func, returning the context and a cleanup that clears the slot.
//
// It runs on the *caller's* goroutine, before the worker is started, and that
// ordering is the point. InstallCancel used to happen inside the worker, so a
// cancel fired before the worker got scheduled landed on an empty slot and was
// silently dropped: Esc right after "/compact" did nothing, and the test that
// pinned that promise passed only when the scheduler happened to run the
// worker first. Installing first makes "esc to cancel" true from the moment
// the row is drawn.
func beginGenericCompaction() (context.Context, func()) {
	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	cleanup := compaction.InstallCancel(cancel)
	return ctx, func() {
		cleanup()
		cancel()
	}
}

// startGenericCompact is the production /compact path. It returns the
// command that drives the live row's spinner: without it the row is a
// still of frame 0 and the compaction reads as frozen.
func (m *RootModel) startGenericCompact(customInstructions string) tea.Cmd {
	sessSnap := m.SessionMessages()
	if len(sessSnap) == 0 {
		m.slashRespondPlain("Nothing to compact yet.")
		return nil
	}

	started := time.Now()
	echoCmd := "/compact"
	if trimmed := strings.TrimSpace(customInstructions); trimmed != "" {
		echoCmd = "/compact " + trimmed
	}
	m.conversation.response = append(m.conversation.response, "> "+echoCmd)
	// The running compaction occupies one live row, where the reference puts
	// "Generating…" and "Working…". It is armed here and repainted in place
	// by updateCompactSpinnerTickMsg; nothing is added to the status row.
	m.session.store.CompactStart = started
	m.session.store.CompactEnd = time.Time{}
	m.session.store.CompactSpinnerFrame = 0
	// The live row is in the conversation, so the status row stays empty.
	m.session.store.CompactLiveInTranscript = true
	m.conversation.compactRowIdx = len(m.conversation.response)
	m.conversation.compactRowLive = true
	m.conversation.response = append(m.conversation.response,
		m.compactingLine(),
		"",
	)

	backendClient := m.turn.backend
	model := m.session.model
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	prog := m.turn.prog

	systemPrompt := m.systemPrompt()
	overhead := compaction.ContextOverheadTokens(systemPrompt, modelDeclarationsJSON())
	tools := m.tools.decls

	// The cancel func is installed here, before the worker starts, so the
	// "esc to cancel" hint is true from the first frame — see
	// beginGenericCompaction.
	ctx, finish := beginGenericCompaction()

	if prog != nil {
		// Production: the result travels as a tea.Msg and is applied by
		// the Update loop, on the UI goroutine.
		go func() {
			defer finish()
			runGenericCompactionTask(ctx, backendClient, model, systemPrompt, overhead, sessSnap, tools, customInstructions, started, prog, nil)
		}()
		return compactSpinnerTick()
	}
	// No program to post to (the test path). Hand the result over a
	// channel instead: the worker never touches m.conversation.response or m.engine.session,
	// which belong to the goroutine that owns the RootModel.
	ch := make(chan genericCompactDoneMsg, 1)
	m.compaction.SetDone(ch)
	go func() {
		defer finish()
		runGenericCompactionTask(ctx, backendClient, model, systemPrompt, overhead, sessSnap, tools, customInstructions, started, nil, func(msg genericCompactDoneMsg) {
			ch <- msg
		})
	}()
	// The live row is armed on this path too, so it is ticked on this
	// path too: which branch delivers the result is not the row's
	// business.
	return compactSpinnerTick()
}

func modelDeclarationsJSON() []byte {
	decls := surface.ModelDeclarations()
	b, err := json.Marshal(decls)
	if err != nil {
		return nil
	}
	return b
}

// manualCompactRequests builds the request list a manual /compact carries.
//
// 【一手】 agy 1.5.3, a live /compact on Gemini 3.8 Flash High: the checkpoint
// lists the original requests, every earlier /compact, the prompt in between
// and the current /compact, in order. The session itself no longer holds those
// requests after the previous compaction replaced it with a resume block, so
// the persisted checkpoint's list is the source and the current command is
// appended here.
func (m *RootModel) manualCompactRequests() []string {
	limit := compaction.MaxUserRequests
	requests := append([]string(nil), m.lastCheckpointRequests()...)
	if len(requests) > limit-1 {
		requests = requests[len(requests)-(limit-1):]
	}
	// Everything after the previous resume block is a turn the user made
	// since the checkpoint.
	sess := m.SessionMessages()
	if len(sess) > 0 && sess[0].Role == "user" && compaction.IsResumeBlock(sess[0].Content) {
		sess = sess[1:]
	}
	requests = append(requests, compaction.UserRequests(sess, limit-len(requests))...)
	if len(requests) > limit-1 {
		requests = requests[len(requests)-(limit-1):]
	}
	return append(requests, "/compact")
}

func (m *RootModel) applyGenericCompactSuccess(msg genericCompactDoneMsg, customInstructions string) tea.Cmd {
	// What the summary carries into the next prompt is counted beside
	// the summary in the "after" figure — the earlier requests and the
	// snapshots the reference's checkpoint carries. The clone's resume
	// block carries the requests; it has no artifact, subagent or
	// running-task snapshots.
	// markCompacted hands back what it took out of the active context: the
	// tool results the summary is replacing, which the banner's "before"
	// figure adds.
	toolTokens := m.markCompacted()
	carriedTokens := 0
	if msg.Summary != "" {
		requests := m.manualCompactRequests()
		snapshot := m.checkpointSnapshot()
		resume := compaction.ResumeBlock(compaction.ResumeOptions{
			UserRequests:           requests,
			Summary:                msg.Summary,
			Artifacts:              snapshot.Artifacts,
			RunningTasks:           snapshot.RunningTasks,
			Subagents:              snapshot.Subagents,
			IncludeConversationLog: true,
		})
		carriedTokens = compaction.EstimateTokens(compaction.CarriedBlock(requests))
		currentSteps := convstore.BuildSteps(m.trajectoryFacts())
		cpIdx := convstore.NextStepIndex(currentSteps)
		currentSteps = append(currentSteps, convstore.Step{
			Idx:          cpIdx,
			Type:         convstore.StepCheckpoint,
			Status:       3,
			Summary:      msg.Summary,
			UserRequests: requests,
			IncludedEnd:  int(cpIdx) + 1,
		})
		m.session.bumpEpoch()
		m.setSessionMessages([]backend.Message{{
			Role:    "user",
			Content: resume,
		}})
		if sup := m.session.sup(); sup != nil {
			sup.SetSeededConversation(currentSteps, m.session.messageCount())
		}
		m.session.store.CompactJustHappened = true
		m.conversation.compactHappenedThisTurn = true
		m.session.store.CompactTokensBefore = msg.StatsBefore.Tokens
		m.session.store.CompactTokensAfter = msg.StatsAfter.Tokens
		m.session.store.CompactEnd = time.Now()
		memtrim.Release()
	}
	beforeTokens, afterTokens, summaryTokens := compaction.ComputeCompactTokens(msg.StatsBefore.Tokens, toolTokens, msg.InputTokens, msg.StatsAfter.Tokens, msg.OutputTokens, msg.Overhead, msg.Summary, carriedTokens)
	compaction.RecordEvent(compaction.Event{
		Timestamp:    time.Now(),
		Reason:       "manual",
		TokensBefore: beforeTokens,
		TokensAfter:  afterTokens,
		Duration:     msg.Took,
		Summary:      msg.Summary,
	})
	m.conversation.response = append(m.conversation.response,
		compaction.DividerRow(m.viewport.width),
		compaction.StatsRow(m.viewport.width,
			msg.StatsBefore.Messages,
			beforeTokens,
			afterTokens,
			summaryTokens,
			msg.Took),
		"",
	)
	if customInstructions = strings.TrimSpace(customInstructions); customInstructions != "" {
		m.session.appendMessage(backend.Message{Role: "user", Content: customInstructions})
		return tea.Batch(m.streamPrompt(customInstructions), compactExpireCmd())
	}
	return compactExpireCmd()
}

// compactingLine is the running compaction's live row.
//
// It is the one place a running compaction is drawn. The reference shows
// this line where "Generating…" and "Working…" appear — inside the
// conversation — and puts nothing in the status row, so the row is only
// repainted in place and never duplicated.
func (m *RootModel) compactingLine() string {
	frame := usagepanel.SpinnerFrame(m.session.store.CompactSpinnerFrame)
	elapsed := time.Since(m.session.store.CompactStart)
	return rows.CompactingLine(frame, elapsed, true)
}

// repaintCompactingLine rewrites the live row in place, leaving every row
// around it untouched.
func (m *RootModel) repaintCompactingLine() bool {
	if !m.conversation.compactRowLive {
		return false
	}
	if m.session.store.CompactStart.IsZero() || !m.session.store.CompactEnd.IsZero() {
		return false
	}
	i := m.conversation.compactRowIdx
	if i < 0 || i >= len(m.conversation.response) {
		return false
	}
	line := m.compactingLine()
	if m.conversation.response[i] == line {
		return false
	}
	m.conversation.response[i] = line
	return true
}

// clearCompactingLine removes the live row once the compaction settles.
//
// The result row replaces it, so the conversation never shows both a
// "compacting" line and its outcome.
func (m *RootModel) clearCompactingLine() {
	if !m.conversation.compactRowLive {
		return
	}
	i := m.conversation.compactRowIdx
	m.conversation.compactRowLive = false
	if i < 0 || i >= len(m.conversation.response) {
		return
	}
	if !strings.Contains(m.conversation.response[i], "Compacting conversation") {
		return
	}
	m.conversation.response = append(m.conversation.response[:i], m.conversation.response[i+1:]...)
}

func (m *RootModel) cleanCompactingNotice() {
	for len(m.conversation.response) > 0 {
		last := m.conversation.response[len(m.conversation.response)-1]
		if last == "" {
			m.conversation.response = m.conversation.response[:len(m.conversation.response)-1]
			continue
		}
		if strings.Contains(last, "Compacting conversation") {
			m.conversation.response = m.conversation.response[:len(m.conversation.response)-1]
		}
		break
	}
	if len(m.conversation.response) > 0 && m.conversation.response[len(m.conversation.response)-1] != "" {
		m.conversation.response = append(m.conversation.response, "")
	}
}

func (m *RootModel) applyGenericCompactResult(msg genericCompactDoneMsg, customInstructions string) tea.Cmd {
	// The live row is replaced by the outcome: the conversation must never
	// show a "compacting" line beside the row that says it finished.
	m.session.store.CompactEnd = time.Now()
	m.clearCompactingLine()
	m.cleanCompactingNotice()
	switch {
	case msg.Cancelled:
		m.conversation.response = append(m.conversation.response,
			rows.NoticeCompact(compaction.CompactionCancelledLiteral),
			"",
		)
		return nil
	case msg.Err != nil:
		m.conversation.response = append(m.conversation.response,
			rows.NoticeCompact("Compact failed: "+msg.Err.Error()),
			"",
		)
		return nil
	default:
		return m.applyGenericCompactSuccess(msg, customInstructions)
	}
}
