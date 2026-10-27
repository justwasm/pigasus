// conversation.go — one client connection's agent session.
//
// # The shape of a conversation
//
// A connection carries one conversation. The client opens it with the
// standalone `InitializeConversationEvent` (which carries the
// `HarnessConfig`), and from then on the two sides exchange `InputEvent`s
// and `OutputEvent`s.
//
// The conversation owns:
//
//   - the agent runtime (`internal/pigrt`), one per conversation, because
//     PiG's session owns a single turn queue;
//   - the budget manager (`budget`), seeded from the config's
//     `budget_config`;
//   - the hook dispatcher (`lifecyclehooks`'s model, on the
//     harness side);
//   - the client-side tools the config declared.
//
// # Concurrency
//
// The read loop is single-threaded: it decodes a frame and dispatches it.
// A turn, however, blocks — the model streams, tools run, and a tool may
// wait for the client's answer. So a turn runs on its own goroutine, and
// the loop stays free to deliver the answers that turn is waiting for.
//
// One turn at a time: a second `user_input` while a turn is running is
// dropped, because PiG's session is not safe for concurrent sends.
//
// # Limitations
//
//   - The turn's `InputEvent`s are not queued. A `user_input` that arrives
//     during a turn is dropped rather than buffered.
//   - The harness does not implement the reference's *planner* (its
//     separate planning model and its `TaskBoundary` steps). The clone's
//     single-loop turn stands in for it.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	piagent "github.com/MichaelKinsy/PiG/agent"
	pi "github.com/MichaelKinsy/PiG/ai"

	"github.com/justwasm/pigasus/budget"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/backendselect"
	"github.com/justwasm/pigasus/internal/harnesstools"
	"github.com/justwasm/pigasus/internal/modelcatalog"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/piagentbridge"
	"github.com/justwasm/pigasus/internal/pigrt"
	"github.com/justwasm/pigasus/internal/printmode"
	"github.com/justwasm/pigasus/internal/toolrunner"
	"github.com/justwasm/pigasus/internal/tools"
	"github.com/justwasm/pigasus/lifecyclehooks"
	"github.com/justwasm/pigasus/localharness"
	pb "github.com/justwasm/pigasus/localharness/pb/localharness"
)

// maxContinuations bounds how many times a STOP hook may force the loop to
// keep going. Without a bound a CONTINUE verdict is an infinite loop.
const maxContinuations = 16

// minCascadeIDLength is the shortest cascade id the reference accepts.
//
// The reference refuses a shorter one and closes the connection: the
// cascade id names the conversation's directories, and a short one would
// collide with another conversation's.
const minCascadeIDLength = 32

// conversation is one client connection's agent session.
// tokenCount is the conversation's running token count, one field per
// counter the protocol's UsageUpdate carries.
type tokenCount [usageCount]uint64

// conversation is one accepted connection.
type conversation struct {
	conn   *localharness.ServerConn
	opts   options
	logger *slog.Logger
	client *pb.ClientInfo
	input  *pb.InputConfig

	cfg          *pb.HarnessConfig
	settings     sessionSettings
	configPolicy *configPolicy
	rt           *pigrt.Runtime
	budget       *budget.Manager
	hooks        *hookDispatcher
	policy       permissions.Policy

	cascadeID    string
	trajectoryID string

	// seq numbers the OutputEvents. The protocol carries a monotonic
	// counter; the client uses it to order the frames it receives.
	seqMu sync.Mutex
	seq   int64

	// stepIndex numbers the steps of the current turn. It is -1 until
	// the first step is opened, so the first one is 0 — the reference
	// numbers the user's message 0.
	//
	// The protocol's `step_index` names a logical step, not a frame:
	// the reference sends the model's text as three frames under one
	// index and the client folds them together.
	stepMu    sync.Mutex
	stepIndex int32
	// modelStep is the index of the model's open message step, and
	// modelOpen says whether one is open.
	modelStep uint32
	modelOpen bool
	// textBuf accumulates the model's text for the open message step.
	textBuf strings.Builder
	// toolSteps remembers the step index of the tool calls in flight,
	// so the end frame keeps the index the start frame used.
	toolSteps map[string]uint32

	// pending correlates a request this harness sent with the client's
	// answer: hook requests, client-side tool calls and policy decisions.
	pendingMu sync.Mutex
	pending   map[string]chan *pb.InputEvent

	// toolArgs remembers the arguments of the tool calls in flight, so the
	// end event can carry the same call the start event did.
	toolArgsMu sync.Mutex
	toolArgs   map[string]json.RawMessage

	// outcomes remembers what each tool produced, so the frame that closes
	// the step can carry it — a command's exit code, an edit's diff, a
	// listing's entries.
	outcomes *outcomeStore

	// pendingUsage holds the token usage of a model response whose
	// message called a tool. The reference reports that count after the
	// call has been dispatched.
	pendingUsageMu sync.Mutex
	pendingUsage   *pi.Usage

	// verdicts remembers the pre-tool verdict of each call, keyed by the
	// call's id. The reference asks the client before it announces the
	// call, so the answer is known before the call runs.
	verdictsMu sync.Mutex
	verdicts   map[string]callVerdict

	// evalMu guards the evaluations in flight.
	evalMu sync.Mutex
	// evals holds the calls whose guards are being evaluated. The
	// reference asks the client about a call once, and PiG can reach the
	// hook before the announcement has finished — the two run in
	// different goroutines — so the evaluation happens once per call id
	// and the second caller gets the first one's verdict.
	evals map[string]*callEvaluation

	// invalidMu guards the counter of the calls the reference refused.
	invalidMu sync.Mutex
	// invalidCalls counts the refused calls in a row. The reference tells
	// the model how many retries are left, and a call that works starts
	// the count over.
	invalidCalls int
	// invalidFailure is the reason the run failed: the reference gives
	// up when the model uses up its retries, and the trajectory carries
	// the reason.
	invalidFailure string

	// sessionMu guards the session's end.
	sessionMu sync.Mutex
	// sessionFailed says the run failed and the connection has to end.
	sessionFailed bool

	// usageMu guards the running token total.
	usageMu sync.Mutex
	// usageTotal is the conversation's running token count.
	//
	// The protocol's UsageUpdate carries the total so far, not the last
	// response's count: the captured frames go 18, 36, 54 — each one the
	// sum of the responses so far.
	usageTotal tokenCount
	// turnMu guards the turn bookkeeping below.
	turnMu   sync.Mutex
	turnBusy bool
	turnCtx  context.Context
	cancel   context.CancelFunc
}

// newConversation builds a conversation for an accepted connection.
func newConversation(conn *localharness.ServerConn, opts options, logger *slog.Logger, input *pb.InputConfig) *conversation {
	return &conversation{
		conn:     conn,
		opts:     opts,
		logger:   logger,
		client:   input.GetClientInfo(),
		input:    input,
		pending:  map[string]chan *pb.InputEvent{},
		toolArgs: map[string]json.RawMessage{},
		outcomes: newOutcomeStore(),
		// The first step the reference writes is 0, so the counter
		// starts one below it.
		stepIndex: -1,
	}
}

// rememberToolArgs records the arguments of a tool call in flight.
func (c *conversation) rememberToolArgs(name string, args json.RawMessage) {
	c.toolArgsMu.Lock()
	defer c.toolArgsMu.Unlock()
	if c.toolArgs == nil {
		c.toolArgs = map[string]json.RawMessage{}
	}
	c.toolArgs[name] = args
}

// toolArgsOf returns the arguments of the last call of a tool.
func (c *conversation) toolArgsOf(name string) json.RawMessage {
	c.toolArgsMu.Lock()
	defer c.toolArgsMu.Unlock()
	return c.toolArgs[name]
}

// serve is the connection's read loop.
func (c *conversation) serve(ctx context.Context) error {
	defer c.close()
	for {
		frame, err := c.conn.Read(ctx)
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			if c.sessionEnded() {
				// The run failed: the connection was closed with the
				// normal close, and the read ends with the close error.
				return nil
			}
			return err
		}
		switch {
		case frame.Initialize != nil:
			if err := c.initialize(ctx, frame.Initialize); err != nil {
				return err
			}
		case frame.Event != nil:
			if err := c.handleEvent(ctx, frame.Event); err != nil {
				return err
			}
		}
	}
}

// failSession ends the connection after a failed run.
//
// The reference closes the connection when the run fails — the client sees
// a normal close (1000) — and the process stays up. The clone does the
// same.
//
// The read loop cannot be woken without dropping the connection: the
// websocket's read context closes the connection when it is cancelled, and
// the client would see a broken pipe instead of a close. So the close
// frame is written from here, and the read loop ends when the peer
// answers it.
func (c *conversation) failSession() {
	c.sessionMu.Lock()
	c.sessionFailed = true
	c.sessionMu.Unlock()
	go func() {
		if err := c.conn.CloseGracefully(); err != nil {
			c.logger.Debug("harness: close after a failed run", "error", err)
		}
	}()
}

// sessionEnded reports whether the session has to end.
func (c *conversation) sessionEnded() bool {
	c.sessionMu.Lock()
	defer c.sessionMu.Unlock()
	return c.sessionFailed
}

// close tears the conversation down.
func (c *conversation) close() {
	c.turnMu.Lock()
	cancel := c.cancel
	c.cancel = nil
	rt := c.rt
	c.rt = nil
	c.turnMu.Unlock()
	if cancel != nil {
		cancel()
	}
	if rt != nil {
		if err := rt.Close(); err != nil {
			c.logger.Debug("harness: runtime close", "error", err)
		}
	}
}

// initialize handles the conversation's first frame.
//
// The reference refuses a cascade id shorter than 32 characters and closes
// the connection without answering:
//
//	Failed to create agent: cascade_id must be at least 32 characters long, got 8
//
// The clone does the same: the error goes to stderr and the client sees
// the connection go away.
func (c *conversation) initialize(ctx context.Context, cfg *pb.HarnessConfig) error {
	c.cfg = cfg
	c.cascadeID = strings.TrimSpace(cfg.GetCascadeId())
	if len(c.cascadeID) < minCascadeIDLength {
		err := fmt.Errorf("cascade_id must be at least %d characters long, got %d",
			minCascadeIDLength, len(c.cascadeID))
		c.logger.Error("harness: failed to create agent", "error", err)
		return err
	}
	// The reference's root trajectory is the cascade itself: every frame
	// it writes carries the cascade id in both fields.
	c.trajectoryID = c.cascadeID
	c.settings = resolveSettings(cfg, c.input, c.opts)
	if c.settings.ConversationID == "" {
		c.settings.ConversationID = c.cascadeID
	}
	c.hooks = newHookDispatcher(c, cfg.GetEnabledHooks())
	c.configPolicy = newConfigPolicy(cfg.GetPolicyConfig())
	c.budget = budget.NewManager(budgetConfigFromProto(cfg.GetBudgetConfig()))
	c.budget.SetRootTrajectoryID(c.trajectoryID)
	c.policy = harnessPolicy(c.opts.agentDir)

	if err := c.startRuntime(ctx); err != nil {
		// The conversation still opens: the client learns the cascade id
		// and can report the failure itself. The turn reports it again.
		c.logger.Error("harness: agent runtime did not start", "error", err)
	}

	resp := &pb.InitializeConversationResponse{}
	resp.SetCascadeId(c.cascadeID)
	status := &pb.SandboxStatus{}
	status.SetAvailable(true)
	// The reference writes the reason even when the sandbox is
	// available: the field has explicit presence and it is set to the
	// empty string.
	status.SetUnavailableReason("")
	resp.SetSandboxStatus(status)

	ev := &pb.OutputEvent{}
	ev.SetInitializeConversationResponse(resp)
	if err := c.send(ctx, ev); err != nil {
		return err
	}
	c.hooks.sessionStart(ctx)
	return nil
}

// startRuntime builds the agent runtime for this conversation.
//
// The transport comes from the client's own configuration when it names a
// model endpoint: the SDK hands the harness the endpoint and the key in
// `HarnessConfig.models`, and that configuration is authoritative for a
// harness session. Without it the clone falls back to its own dispatch —
// a catalogue model goes to CloudCode and a configured model to its own
// endpoint, the same rule the CLI uses.
func (c *conversation) startRuntime(ctx context.Context) error {
	be, wire, err := c.transport()
	if err != nil {
		return err
	}
	model := printmode.PigPrintModel(be, wire)
	if model == nil {
		return fmt.Errorf("backend %T has no provider", be)
	}
	slot := &pigrt.ErrorSlot{}
	model.Provider = c.withFunctionCallingMode(model.Provider, be)
	model.Provider = pigrt.RecordProvider(model.Provider, slot)

	workspace := c.settings.CWD
	catalog := tools.RuntimeCatalog(workspace)
	enabled := c.sideToolSubset()
	extra := piagentbridge.LenientSurfaceToolsFor(catalog, nil, harnessDeclarations(enabled, c.settings.FinishSchema))
	extra = harnessTools(extra, c.outcomes, c.settings.ToolOutputTokens)
	extra = append(extra, c.clientTools()...)

	agentHome := agentHomeFor(c.opts, c.settings)
	rt, err := pigrt.New(ctx, pigrt.Options{
		CWD:              workspace,
		AgentDir:         agentHome,
		Model:            model,
		SystemPrompt:     c.systemPrompt(),
		SkillPaths:       c.settings.SkillsPaths,
		ExtraTools:       extra,
		SkipBuiltinTools: true,
		BeforeToolCall:   []piagent.BeforeToolCallHook{c.beforeToolCall},
		ErrorSlot:        slot,
		NoSession:        true,
		MCPConfigPath:    mcpConfigPath(agentHome),
		CompactThreshold: compactFraction(c.settings.CompactionThreshold, model.Capabilities.ContextWindow),
	})
	if err != nil {
		return err
	}
	c.turnMu.Lock()
	c.rt = rt
	c.turnMu.Unlock()
	c.logger.Debug("harness: runtime ready",
		"model", wire, "workspace", workspace, "agent_home", agentHome,
		"tools", enabledToolNames(enabled))
	return nil
}

// compactFraction maps the protocol's compaction threshold onto the
// clone's fraction of the context window.
//
// The protocol carries an absolute token count — "compact once the
// transcript reaches N tokens". The clone carries the fraction of the
// model's context window at which the compaction runs, so the two are the
// same number once the window is known.
func compactFraction(tokenThreshold uint32, window int) float64 {
	if tokenThreshold == 0 || window <= 0 {
		return 0
	}
	fraction := float64(tokenThreshold) / float64(window)
	if fraction <= 0 || fraction >= 1 {
		return 0
	}
	return fraction
}

// transport picks the transport the session runs on and the model id it
// puts on the request.
//
// The client's model endpoint wins: the SDK owns the credentials, and the
// clone's own dispatch would silently serve a different model. The
// fallback — the clone's dispatch — runs only when the config names no
// endpoint at all.
func (c *conversation) transport() (backend.Client, string, error) {
	configured, err := configModelFor(c.cfg, c.client)
	if err != nil {
		return nil, "", err
	}
	if configured != nil {
		c.logger.Info("harness: using the client's model endpoint",
			"model", configured.Label, "wire", configured.Wire,
			"models", describeConfigModels(c.cfg))
		return configured.Client, configured.Wire, nil
	}

	name := c.opts.model
	if name == "" {
		name = modelcatalog.DefaultModel
	}
	label, unavailable := modelcatalog.ResolveEffectiveModel(name)
	if unavailable != "" {
		c.logger.Warn("harness: model is not available, falling back", "requested", unavailable, "using", label)
	}

	be := backendselect.PickWithLog(c.backendFlags(), label, os.Stderr, c.opts.timeout, catalogueID)
	return be, label, nil
}

// backendFlags builds the flag set the shared transport dispatch reads.
//
// The dispatch looks for a "backend" flag; the harness's own flag has the
// same name, so the value is handed over rather than re-parsed.
func (c *conversation) backendFlags() *flag.FlagSet {
	fs := flag.NewFlagSet("harness-backend", flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.String("backend", c.opts.backend, "")
	return fs
}

// catalogueID resolves a model label to the catalogue id the transports key
// on. It is the same resolver the CLI's dispatch uses.
func catalogueID(value string) string {
	id, _ := modelcatalog.CatalogueID(value)
	return id
}

// handleEvent dispatches one input event.
func (c *conversation) handleEvent(ctx context.Context, ev *pb.InputEvent) error {
	switch {
	case ev.HasUserInput():
		c.startTurn(ctx, ev.GetUserInput())
	case ev.HasAutomatedTrigger():
		c.startTurn(ctx, nil)
	case ev.HasToolResponse():
		c.deliver(ev.GetToolResponse().GetId(), ev)
	case ev.HasCallHookResponse():
		c.deliver(ev.GetCallHookResponse().GetRequestId(), ev)
	case ev.HasPolicyDecisionResponse():
		c.deliver(ev.GetPolicyDecisionResponse().GetRequestId(), ev)
	case ev.HasToolConfirmation():
		c.logger.Debug("harness: tool confirmation", "accepted", ev.GetToolConfirmation().GetAccepted())
	case ev.HasQuestionResponse():
		c.logger.Debug("harness: question response")
	case ev.HasHaltRequest():
		c.halt()
	case ev.HasSessionEndRequest():
		c.endSession(ctx)
	default:
		c.logger.Debug("harness: ignoring input event")
	}
	return nil
}

// startTurn runs a turn on its own goroutine.
//
// The read loop must stay free while the turn runs: the turn may be
// waiting for a hook answer or a client-side tool's result, and both come
// in as input events.
func (c *conversation) startTurn(ctx context.Context, in *pb.UserInput) {
	c.turnMu.Lock()
	if c.turnBusy {
		c.turnMu.Unlock()
		c.logger.Warn("harness: a turn is already running; dropping the input")
		return
	}
	c.turnBusy = true
	turnCtx, cancel := context.WithCancel(ctx)
	c.turnCtx = turnCtx
	c.cancel = cancel
	c.turnMu.Unlock()

	go func() {
		defer func() {
			cancel()
			c.turnMu.Lock()
			c.turnBusy = false
			c.turnCtx = nil
			c.cancel = nil
			c.turnMu.Unlock()
		}()
		if err := c.runTurn(turnCtx, in); err != nil {
			c.logger.Debug("harness: turn ended", "error", err)
		}
	}()
}

// halt cancels the running turn.
func (c *conversation) halt() {
	c.turnMu.Lock()
	cancel := c.cancel
	c.turnMu.Unlock()
	if cancel == nil {
		return
	}
	c.logger.Debug("harness: halt requested")
	cancel()
}

// endSession fires the session-end hook and stops the conversation.
//
// The client's `disconnect()` sends `session_end_request` and then waits
// for the answer, so the acknowledgement must go out before the
// conversation is torn down.
func (c *conversation) endSession(ctx context.Context) {
	c.logger.Debug("harness: session end requested")
	c.hooks.sessionEnd(ctx)
	c.emitSessionEnd(ctx, true)
	c.halt()
}

// runTurn runs one turn: the pre-turn hook, the model loop, the post-turn
// hook and the stop hook.
func (c *conversation) runTurn(ctx context.Context, in *pb.UserInput) error {
	// The runtime is taken once: the teardown may close it under us, and
	// a local copy keeps the turn's own reference stable.
	c.turnMu.Lock()
	rt := c.rt
	c.turnMu.Unlock()
	if rt == nil {
		err := errors.New("harness: the agent runtime is not running")
		c.emitStepError(err.Error())
		return err
	}
	prompt := ""
	if in != nil {
		prompt = lifecyclehooks.RenderUserInput(in)
	}

	verdict, err := c.hooks.preTurn(ctx, prompt)
	if err != nil {
		c.emitStepError("pre-turn hook failed: " + err.Error())
		return nil
	}
	if verdict.Denied {
		c.emitStepError(verdict.Reason)
		c.emitIdle(pb.TrajectoryStateUpdate_STOP_REASON_UNSPECIFIED)
		return nil
	}
	if v, ok := c.budget.LimitExhausted(); ok {
		c.logger.Info("harness: budget exhausted", "kind", v.Kind.String(), "limit", v.Limit)
		c.emitStepError(v.Error())
		c.emitIdle(stopReasonFor(v.StopReason()))
		return nil
	}

	continuations := 0
	for {
		// The reference writes the running state first and the user's
		// message second, then the model's answer.
		c.emitRunning()
		if continuations == 0 {
			c.emitUserInput(prompt)
		}
		// The model reads the user's turn inside the reference's
		// envelope, not as the bare text. The captured request body
		// shows it verbatim:
		//
		//	<USER_REQUEST>
		//	hello
		//	</USER_REQUEST>
		//	<ADDITIONAL_METADATA>
		//	The current local time is: 2026-10-26T01:12:58+08:00.
		//	</ADDITIONAL_METADATA>
		//
		// The echo above keeps the bare text: the client's transcript
		// shows what the user typed.
		wire := backend.WrapUserContent(prompt, "", false, time.Now())
		msgs, runErr := rt.RunWith(ctx, wire, c.onText, c.onEvent)
		responseText := lastAssistantText(msgs)
		c.hooks.postTurn(ctx, responseText)
		if reason := c.invalidFailureReason(); reason != "" {
			// The reference gives up when the model uses up its retries:
			// the run fails with the reason, the trajectory says so, and
			// the connection is closed.
			c.emitFailed(reason)
			c.failSession()
			return nil
		}
		if runErr != nil {
			c.emitStepError(runErr.Error())
			c.emitIdle(pb.TrajectoryStateUpdate_STOP_REASON_UNSPECIFIED)
			return runErr
		}

		stop, stopErr := c.hooks.stop(ctx, responseText, continuations)
		if stopErr != nil {
			c.logger.Warn("harness: stop hook failed", "error", stopErr)
		}
		if !stop.Continue || continuations >= maxContinuations {
			if stop.Continue {
				c.logger.Warn("harness: continuation limit reached", "limit", maxContinuations)
			}
			c.emitIdle(pb.TrajectoryStateUpdate_STOP_REASON_UNSPECIFIED)
			return nil
		}
		continuations++
		prompt = ""
	}
}

// beforeToolCall is the PiG hook that runs before every tool call.
//
// It is also where the three guards live: the client's tool-call policy,
// the clone's own permission policy, and the client's PRE_TOOL lifecycle
// hook (which may deny the call or rewrite its arguments).
//
// The client's policy comes first: it is the session's own rule set, and
// it is the only one that knows about MCP servers and dynamic rules.
//
// The guards run when the call is *announced*, not when it runs: the
// reference asks the client before it announces the call, so a call the
// client refuses is never announced. The verdict is remembered, and this
// hook — which PiG calls when the call is about to run — hands it back
// instead of asking twice.
func (c *conversation) beforeToolCall(ctx context.Context, toolCallID, toolName string, args json.RawMessage) piagent.ToolCallHookResult {
	verdict := c.evaluateToolCallOnce(ctx, toolCallID, toolName, args)
	return piagent.ToolCallHookResult{
		Block:  verdict.Blocked,
		Reason: verdict.Result,
		Args:   verdict.Args,
	}
}

// callEvaluation is one call's guards, evaluated once.
type callEvaluation struct {
	once    sync.Once
	verdict callVerdict
}

// evaluateToolCallOnce runs the guards for a call, once per call id.
//
// The reference asks the client about a call once. PiG's hook can run
// before the announcement has finished — the two run in different
// goroutines — so the evaluation is done once and the second caller waits
// for it and gets the same verdict. Without this the guards would run
// twice for one call: the client would be asked twice, and the refusal
// counter would fall twice for one refusal.
func (c *conversation) evaluateToolCallOnce(ctx context.Context, toolCallID, toolName string, args json.RawMessage) callVerdict {
	c.evalMu.Lock()
	if c.evals == nil {
		c.evals = map[string]*callEvaluation{}
	}
	evaluation, ok := c.evals[toolCallID]
	if !ok {
		evaluation = &callEvaluation{}
		c.evals[toolCallID] = evaluation
	}
	c.evalMu.Unlock()

	evaluation.once.Do(func() {
		evaluation.verdict = c.evaluateToolCall(ctx, toolCallID, toolName, args)
		c.rememberVerdict(toolCallID, evaluation.verdict)
	})
	return evaluation.verdict
}

// callVerdict is what the guards decided about a call.
type callVerdict struct {
	// Blocked says the call must not run.
	Blocked bool
	// Cancelled says the call is a question the harness could not ask:
	// it runs and fails, and the reference writes one frame for it.
	Cancelled bool
	// Invalid says the model produced a call the reference refuses
	// before it runs: the frame says so, and the model reads the
	// reference's own paragraph.
	Invalid bool
	// InvalidMessage is the sentence the frame of an invalid call
	// carries. It is empty when the reference has no sentence of its
	// own for the refusal.
	InvalidMessage string
	// Reason is why it must not run, in the client's own words.
	Reason string
	// Result is what the model sees: the failed step's rendering.
	Result string
	// Args are the rewritten arguments, when the hook changed them.
	Args json.RawMessage
}

// evaluateToolCall runs the three guards for a call.
//
// The client sees the reference's own wording: a call its policy refuses
// is a pre-tool hook that denied it, and the model sees the failed step's
// rendering.
func (c *conversation) evaluateToolCall(ctx context.Context, toolCallID, toolName string, args json.RawMessage) callVerdict {
	if verdict, invalid := c.checkCall(toolName, args); invalid {
		return verdict
	}
	if reason, blocked, err := c.applyClientPolicy(ctx, toolName, args); blocked {
		if err != nil {
			c.logger.Warn("harness: policy decision failed", "tool", toolName, "error", err)
		}
		return deniedVerdict(reason, true)
	}
	if c.policy.Decide(permissions.Request{Tool: toolName, Target: toolTarget(toolName, args)}) != permissions.Allow {
		return deniedVerdict(permissions.DeniedToolResult(toolName, toolTarget(toolName, args)), false)
	}
	verdict, err := c.hooks.preTool(ctx, toolName, args, toolCallID)
	if err != nil {
		return deniedVerdict("pre-tool hook failed: "+err.Error(), true)
	}
	if verdict.Denied {
		return deniedVerdict(verdict.Reason, true)
	}
	if len(verdict.ModifiedArgs) > 0 {
		return callVerdict{Args: verdict.ModifiedArgs}
	}
	return callVerdict{}
}

// deniedVerdict builds the verdict for a call that must not run.
//
// The frame quotes the failure the way the reference does, and the model
// sees the failed step:
//
//	the reason ("denied by pre-tool hook: the reason")
//
// The step's own failure carries the reference's prefix when the denial
// came from a pre-tool hook — the client's policy rules and the client's
// lifecycle hook are both hooks in the reference.
func deniedVerdict(reason string, hook bool) callVerdict {
	step := reason
	if hook {
		step = "denied by pre-tool hook: " + reason
	}
	return callVerdict{
		Blocked: true,
		Reason:  reason,
		Result:  harnesstools.Denied(step, time.Now()),
	}
}

// verdictFor returns the pre-tool verdict of a call, when it is known.
func (c *conversation) verdictFor(callID string) (callVerdict, bool) {
	c.verdictsMu.Lock()
	defer c.verdictsMu.Unlock()
	verdict, ok := c.verdicts[callID]
	return verdict, ok
}

// rememberVerdict records a call's pre-tool verdict.
func (c *conversation) rememberVerdict(callID string, verdict callVerdict) {
	c.verdictsMu.Lock()
	defer c.verdictsMu.Unlock()
	if c.verdicts == nil {
		c.verdicts = map[string]callVerdict{}
	}
	c.verdicts[callID] = verdict
}

// forgetVerdict drops a call's verdict once the call is over.
func (c *conversation) forgetVerdict(callID string) {
	c.verdictsMu.Lock()
	delete(c.verdicts, callID)
	c.verdictsMu.Unlock()
	c.evalMu.Lock()
	delete(c.evals, callID)
	c.evalMu.Unlock()
}

// toolTarget extracts the target string the permission policy matches on.
// It reuses the shared runner's rule so the harness and the CLI agree.
func toolTarget(toolName string, args json.RawMessage) string {
	return toolrunner.ToolTarget(backend.ToolCall{Name: toolName, Arguments: args})
}

// send writes one OutputEvent, stamping it with the sequence number and
// the timestamp.
func (c *conversation) send(ctx context.Context, ev *pb.OutputEvent) error {
	c.seqMu.Lock()
	c.seq++
	ev.SetSeqNum(c.seq)
	ev.SetTimestampMicros(time.Now().UnixMicro())
	c.seqMu.Unlock()
	return c.conn.Send(ctx, ev)
}

// await blocks for the client's answer to a request this harness sent.
//
// The correlation id is the request's id: a hook request's `request_id`, a
// client-side tool call's tool-call id, a policy decision's `request_id`.
//
// The slot is registered *before* `send` runs, so an answer that arrives
// while the request is still being written is not dropped. That race is
// real: the client may answer on the very next frame.
func (c *conversation) await(ctx context.Context, id string, send func() error) (*pb.InputEvent, error) {
	ch := make(chan *pb.InputEvent, 1)
	c.pendingMu.Lock()
	if c.pending == nil {
		c.pending = map[string]chan *pb.InputEvent{}
	}
	c.pending[id] = ch
	c.pendingMu.Unlock()
	defer func() {
		c.pendingMu.Lock()
		delete(c.pending, id)
		c.pendingMu.Unlock()
	}()
	if send != nil {
		if err := send(); err != nil {
			return nil, err
		}
	}
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	case ev := <-ch:
		return ev, nil
	}
}

// deliver hands a client answer to whoever is waiting for it.
//
// An answer nobody waits for is dropped: the reference logs it and moves
// on, and a stale answer must not block the next request.
func (c *conversation) deliver(id string, ev *pb.InputEvent) {
	if id == "" {
		return
	}
	c.pendingMu.Lock()
	ch, ok := c.pending[id]
	c.pendingMu.Unlock()
	if !ok {
		c.logger.Debug("harness: answer for an unknown request", "id", id)
		return
	}
	select {
	case ch <- ev:
	default:
	}
}

// nextRequestID mints a correlation id.
func (c *conversation) nextRequestID() string {
	return "req-" + randomID()
}

// currentStepIndex returns the index of the step in flight.
//
// A hook request carries it, so the client can match the answer to the
// step that asked.
func (c *conversation) currentStepIndex() uint32 {
	c.stepMu.Lock()
	defer c.stepMu.Unlock()
	if c.modelOpen {
		return c.modelStep
	}
	if c.stepIndex < 0 {
		return 0
	}
	return uint32(c.stepIndex)
}

// beginStep opens a new logical step and returns its index.
//
// A step is one action: the user's message, the model's text, a tool
// call. The frames that belong to it share the index.
func (c *conversation) beginStep() uint32 {
	c.stepMu.Lock()
	defer c.stepMu.Unlock()
	c.stepIndex++
	c.modelOpen = false
	c.textBuf.Reset()
	return uint32(c.stepIndex)
}

// modelStepIndex returns the index of the model's open message step,
// opening one when none is open.
func (c *conversation) modelStepIndex() uint32 {
	c.stepMu.Lock()
	defer c.stepMu.Unlock()
	if !c.modelOpen {
		c.stepIndex++
		c.modelStep = uint32(c.stepIndex)
		c.modelOpen = true
		c.textBuf.Reset()
	}
	return c.modelStep
}

// closeModelStep closes the model's open message step.
func (c *conversation) closeModelStep() {
	c.stepMu.Lock()
	defer c.stepMu.Unlock()
	c.modelOpen = false
	c.textBuf.Reset()
}

// appendText accumulates the model's text and returns the whole message
// so far.
func (c *conversation) appendText(delta string) string {
	c.stepMu.Lock()
	defer c.stepMu.Unlock()
	c.textBuf.WriteString(delta)
	return c.textBuf.String()
}

// rememberToolStep records the step index of a tool call in flight.
func (c *conversation) rememberToolStep(name string, index uint32) {
	c.stepMu.Lock()
	defer c.stepMu.Unlock()
	if c.toolSteps == nil {
		c.toolSteps = map[string]uint32{}
	}
	c.toolSteps[name] = index
}

// toolStep returns the step index of the tool call in flight, opening a
// new step when the call was never announced.
func (c *conversation) toolStep(name string) uint32 {
	c.stepMu.Lock()
	defer c.stepMu.Unlock()
	if index, ok := c.toolSteps[name]; ok {
		return index
	}
	c.stepIndex++
	c.toolSteps[name] = uint32(c.stepIndex)
	return uint32(c.stepIndex)
}

// currentContext returns the running turn's context, or the background
// context when no turn is running.
func (c *conversation) currentContext() context.Context {
	c.turnMu.Lock()
	defer c.turnMu.Unlock()
	if c.turnCtx != nil {
		return c.turnCtx
	}
	return context.Background()
}

// budgetConfigFromProto maps the wire BudgetConfig onto the budget
// manager's configuration.
//
// A limit of 0 means "not set", which is what the manager expects.
func budgetConfigFromProto(cfg *pb.BudgetConfig) budget.Config {
	if cfg == nil {
		return budget.Config{}
	}
	return budget.Config{
		MaxModelCalls:   uint32(cfg.GetMaxModelCalls()),
		MaxToolCalls:    uint32(cfg.GetMaxToolCalls()),
		MaxInputTokens:  uint64(cfg.GetMaxInputTokens()),
		MaxOutputTokens: uint64(cfg.GetMaxOutputTokens()),
		MaxTotalTokens:  uint64(cfg.GetMaxTotalTokens()),
		Scope:           scopeFromProto(cfg.GetScope()),
	}
}

// scopeFromProto maps the wire budget scope onto the manager's scope.
func scopeFromProto(scope pb.BudgetConfig_BudgetScope) budget.Scope {
	switch scope {
	case pb.BudgetConfig_BUDGET_SCOPE_LIFETIME:
		return budget.ScopeLifetime
	case pb.BudgetConfig_BUDGET_SCOPE_FORWARD_LOOKING:
		return budget.ScopeForwardLooking
	default:
		return budget.ScopeUnspecified
	}
}

// stopReasonFor maps the manager's stop reason onto the wire enum.
func stopReasonFor(reason budget.StopReason) pb.TrajectoryStateUpdate_StopReason {
	switch reason {
	case budget.StopReasonMaxModelCallsExceeded:
		return pb.TrajectoryStateUpdate_STOP_REASON_MAX_MODEL_CALLS_EXCEEDED
	case budget.StopReasonMaxToolCallsExceeded:
		return pb.TrajectoryStateUpdate_STOP_REASON_MAX_TOOL_CALLS_EXCEEDED
	case budget.StopReasonMaxInputTokensExceeded:
		return pb.TrajectoryStateUpdate_STOP_REASON_MAX_INPUT_TOKENS_EXCEEDED
	case budget.StopReasonMaxOutputTokensExceeded:
		return pb.TrajectoryStateUpdate_STOP_REASON_MAX_OUTPUT_TOKENS_EXCEEDED
	case budget.StopReasonMaxTotalTokensExceeded:
		return pb.TrajectoryStateUpdate_STOP_REASON_MAX_TOTAL_TOKENS_EXCEEDED
	case budget.StopReasonQuotaExhausted:
		return pb.TrajectoryStateUpdate_STOP_REASON_QUOTA_EXHAUSTED
	default:
		return pb.TrajectoryStateUpdate_STOP_REASON_UNSPECIFIED
	}
}

// harnessPolicy builds the permission policy the harness enforces.
//
// It is the clone's own policy, loaded from the agy settings.json, so a
// harness session obeys the same rules the CLI does.
func harnessPolicy(agentDir string) permissions.Policy {
	policy := permissions.Policy{
		Strict: permissions.StrictFromEnv(os.Getenv("OPENAGY_PERMISSION_POLICY")),
	}
	if agentDir == "" {
		agentDir = pigrt.DefaultAgentHome()
	}
	if settings, err := permissions.LoadRules(filepath.Join(agentDir, "settings.json")); err == nil {
		policy.Allow = settings.Allow
		policy.Deny = settings.Deny
	}
	return policy
}

// mcpConfigPath is the agy mcp.json inside the config home. An empty
// agent dir disables MCP registration entirely.
func mcpConfigPath(agentDir string) string {
	if agentDir == "" {
		return ""
	}
	return filepath.Join(agentDir, "mcp.json")
}

// randomID returns a short random identifier.
func randomID() string {
	buf := make([]byte, 8)
	if _, err := rand.Read(buf); err != nil {
		return "0000000000000000"
	}
	return hex.EncodeToString(buf)
}
