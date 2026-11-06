package printmode

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/session/setup"
	taskregistry "github.com/justwasm/pigasus/internal/app/task"
	"github.com/justwasm/pigasus/internal/app/toolrunner"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/app/tools/surface"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/cliflags"
	"github.com/justwasm/pigasus/internal/core/policy/permissions"
	settings "github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/features/agents"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/rchost"
	"github.com/justwasm/pigasus/internal/remotecontrol"
)

type printRunConfig struct {
	modelName       string
	format          string
	timeout         time.Duration
	partialResponse bool
	sel             ConversationSelection
	prompt          string
	conversationID  string
	started         time.Time
	rcSess          *remotecontrol.SessionState
	rcDaemon        *remotecontrol.Daemon
}

// runPrintMode streams a one-shot reply for prompt through the agent loop.
func Run(w io.Writer, prompt string, fs *flag.FlagSet) error {
	cfg, err := initPrintRunConfig(fs, prompt)
	if err != nil {
		return err
	}
	defer rchost.ShutdownDaemon(cfg.rcDaemon, os.Stderr)

	handled, newPrompt, slashErr := handlePrintSlashEarly(w, prompt, fs, cfg.format)
	if handled {
		return slashErr
	}
	cfg.prompt = newPrompt

	return executePrintModelTurn(w, fs, cfg)
}

func initPrintRunConfig(fs *flag.FlagSet, prompt string) (*printRunConfig, error) {
	modelName := flagStringValue(fs, "model")
	if modelName == "" {
		modelName = settings.ResolveModel("Gemini 3.8 Flash (High)")
	}
	rcSess := remotecontrol.NewSessionState(Version)
	rcSess.Model = modelName
	rcDaemon := rchost.MaybeStartDaemon(context.Background(), fs, rcSess)
	rchost.BindSession(rcDaemon, rcSess)

	sel := ConversationResolver(fs, os.Stderr)
	convID := newEnvelopeConversationID()
	if sel.ID != "" {
		convID = sel.ID
	}
	rcSess.SetConversationID(convID)

	return &printRunConfig{
		modelName:       modelName,
		format:          printOutputFormat(fs),
		timeout:         printTimeout(fs),
		partialResponse: flagBoolValue(fs, "print-partial-response"),
		sel:             sel,
		prompt:          prompt,
		conversationID:  convID,
		started:         time.Now(),
		rcSess:          rcSess,
		rcDaemon:        rcDaemon,
	}, nil
}

func handlePrintSlashEarly(w io.Writer, prompt string, fs *flag.FlagSet, format string) (bool, string, error) {
	res := dispatchPrintSlash(prompt, fs)
	if res.Prompt != "" {
		prompt = res.Prompt
	}
	if !res.Handled {
		return false, prompt, nil
	}
	if format == printFormatText {
		if res.Output != "" {
			fmt.Fprint(w, res.Output)
		}
		return true, prompt, res.Err
	}
	return true, prompt, emitPrintSlashEnvelope(w, res, format)
}

func emitPrintSlashEnvelope(w io.Writer, res printSlashResult, format string) error {
	env := jsonEnvelope{Status: printStatusSuccess, Response: res.Output}
	if res.Err != nil {
		env.Status = printStatusError
		env.Response = ""
		env.Error = res.ErrorText
	} else if res.Name != "" {
		env.Command = &jsonCommandPayload{Name: res.Name, Data: res.Data}
		if format == printFormatStreamJSON {
			if err := writeJSONLine(w, jsonStreamEvent{Event: streamEventCommandResult, Command: env.Command}); err != nil {
				return err
			}
		}
	}
	if format == printFormatStreamJSON {
		if err := writeJSONLine(w, jsonStreamEvent{Event: streamEventResult, Result: &env}); err != nil {
			return err
		}
		return res.Err
	}
	if err := writeJSONLine(w, env); err != nil {
		return err
	}
	return res.Err
}

type printTurnState struct {
	responseBuf   strings.Builder
	turnUsage     jsonUsage
	turnStarted   bool
	turnBegan     bool
	turnStartedAt time.Time
	deadline      time.Time
	textRouter    *printTextRouter
	sanitizer     *agentturn.Sanitizer
}

// DurationSeconds returns how long the turn ran, in seconds — the value the
// `agent_response` step reports as its `duration_seconds`.
//
// The seconds are rounded to two decimals: the doc's three captured
// durations (6.28, 0.53 and 6.88) all carry exactly two, which a raw
// float64 would not (`official-docs/cli/headless.md:144-146`). A turn that
// never began has no duration.
func (st *printTurnState) DurationSeconds() float64 {
	if st.turnStartedAt.IsZero() {
		return 0
	}
	return math.Round(time.Since(st.turnStartedAt).Seconds()*100) / 100
}

func executePrintModelTurn(w io.Writer, fs *flag.FlagSet, cfg *printRunConfig) error {
	state := &printTurnState{
		textRouter: newPrintTextRouter(cfg.format == printFormatText && glamourTTYCheck()),
		sanitizer:  agentturn.NewSanitizer(),
	}
	if cfg.timeout > 0 {
		state.deadline = cfg.started.Add(cfg.timeout)
	}

	workspace, catalog, err := initPrintCatalogAndStream(w, fs, cfg)
	if err != nil {
		return err
	}

	be, gate, policy := setupPrintBackendAndGate(fs, cfg, workspace, catalog)
	chunkHandler := buildPrintChunkHandler(w, cfg, state)

	ctx, cancel := printContext(cfg.timeout)
	defer cancel()
	// agy 1.4.11: a Ctrl+C ends the turn and the envelope says so
	// (`finished_reason: interrupted`). Catching the signal is what
	// makes the envelope possible at all.
	guard := newPrintInterruptGuard()
	defer guard.Close()
	guard.cancel(cancel)

	workspaces := []string{workspace}
	for _, dir := range flagStringValues(fs, "add-dir") {
		if abs, err := filepath.Abs(dir); err == nil {
			workspaces = append(workspaces, abs)
		}
	}
	if len(workspaces) > 1 {
		tools.SetExtraWorkspaces(workspaces[1:])
	}

	denials, runErr := runActorPrintTurn(ctx, actorPrintConfig{
		Backend:        be,
		Model:          cfg.modelName,
		Prompt:         cfg.prompt,
		History:        printHistory(cfg.sel),
		Catalog:        catalog,
		Policy:         policy,
		Handler:        chunkHandler,
		Workspace:      workspace,
		Workspaces:     workspaces,
		AgentDir:       auth.AntigravityHome(),
		ConversationID: cfg.conversationID,
		Effort:         flagStringValue(fs, "effort"),
		JSONSchema:     printInitJSONSchema(fs),
		AgentBody:      agents.AgentBody(agents.CustomAgentFromFlags(fs)),
		Definitions:    rehydratePrintConversation(cfg.sel, cfg.conversationID),
		Supervisor:     cfg.sel.Supervisor,
		Selection:      cfg.sel,
	})
	gate.Denials = append(gate.Denials, denials...)

	if runErr == nil && !guard.Interrupted() {
		if err := waitForBackgroundTasks(ctx); err != nil {
			runErr = err
		}
	}

	timedOut, timeoutErr := handlePrintTurnTimeout(ctx, cfg.timeout, runErr)
	interrupted := guard.Interrupted()
	reportHeadlessDenials(gate.Denials)
	exit := classifyHeadlessExit(runErr)
	if timedOut {
		exit = timeoutErr
	} else if interrupted {
		// A Ctrl+C is not an API failure: it keeps the generic exit
		// path but with the conventional 128+SIGINT status.
		exit = cli.ExitCodeError(printInterruptedExitCode, printInterruptedMessage(runErr))
	}

	if cfg.format != printFormatText {
		if err := emitFinalPrintEnvelope(w, fs, cfg, state, runErr, timedOut, interrupted); err != nil {
			return err
		}
	}
	return exit
}

func initPrintCatalogAndStream(w io.Writer, fs *flag.FlagSet, cfg *printRunConfig) (string, *tools.Registry, error) {
	workspace, _ := os.Getwd()
	// The three MCP tools (call_mcp_tool, list_resources, read_resource)
	// need a live MCP connection; the pool is lazy, so this costs nothing
	// when no server is configured.
	setup.InstallMCPProvider(auth.AntigravityHome(), workspace)
	catalog := surface.Catalog(workspace)
	initTools := toolrunner.ToolDeclsFromRegistry(catalog)
	surface.AddReferenceTools(catalog, workspace)
	if err := maybeEmitStreamInit(w, fs, cfg, workspace, initTools); err != nil {
		return "", nil, err
	}
	return workspace, catalog, nil
}

func maybeEmitStreamInit(w io.Writer, fs *flag.FlagSet, cfg *printRunConfig, workspace string, initTools []backend.ToolDecl) error {
	if cfg.format != printFormatStreamJSON {
		return nil
	}
	names := make([]string, 0, len(initTools))
	for _, d := range initTools {
		names = append(names, d.Name)
	}
	return writeJSONLine(w, jsonStreamEvent{
		Event:          streamEventInit,
		ConversationID: cfg.conversationID,
		Init: &jsonInitPayload{
			CWD:            workspace,
			Tools:          names,
			PermissionMode: printPermissionMode(fs),
			Model:          printInitModel(fs, cfg),
			Agent:          printInitAgent(fs),
			JSONSchema:     printInitJSONSchema(fs),
		},
	})
}

// printInitModel returns the model the init payload reports, or "".
//
// The doc: "`model` and `agent` appear only when set with `--model` or
// `--agent`" (`official-docs/cli/headless.md:159`). The member is present
// only when the flag was given — the resolved default (the model's own
// settings answer) is not an override and stays out of the wire.
func printInitModel(fs *flag.FlagSet, cfg *printRunConfig) string {
	if !cliflags.WasSet(fs, "model") {
		return ""
	}
	return cfg.modelName
}

// printInitAgent returns the agent the init payload reports, or "".
//
// Only a custom agent is an override: the empty value and "default" name
// the built-in agent, and `agents.CustomAgentFromFlags` owns that rule — so
// the question is asked of its owner rather than restated here.
func printInitAgent(fs *flag.FlagSet) string {
	if !cliflags.WasSet(fs, "agent") {
		return ""
	}
	if a := agents.CustomAgentFromFlags(fs); a != nil {
		return a.Name
	}
	return ""
}

// printInitJSONSchema returns the enforced schema the init payload reports,
// or nil when no schema was requested.
//
// The doc: "`json_schema` — Enforced schema, when set with `--json-schema`"
// (`official-docs/cli/headless.md:168`). The flag's value is a path or inline
// JSON; the payload carries the schema itself, not the flag's spelling of it.
func printInitJSONSchema(fs *flag.FlagSet) json.RawMessage {
	value := flagStringValue(fs, "json-schema")
	if strings.TrimSpace(value) == "" {
		return nil
	}
	raw, err := ResolveJSONSchema(value)
	if err != nil {
		return nil
	}
	return json.RawMessage(raw)
}

func setupPrintBackendAndGate(fs *flag.FlagSet, cfg *printRunConfig, workspace string, catalog *tools.Registry) (backend.Client, *toolrunner.PermissionRunner, *permissions.Policy) {
	be := BackendPicker(fs, printBackendModel(fs, cfg.modelName), printModeSilenceLogger.Writer())
	if resolvedBackend, _ := BackendResolver(fs, io.Discard); resolvedBackend != "" && cfg.rcDaemon != nil {
		cfg.rcSess.SetBackend(resolvedBackend)
	}
	printPolicy := printModePolicy(fs)
	printSandbox := printModeSandbox(fs, workspace, cfg.conversationID)
	gate := &toolrunner.PermissionRunner{
		Inner:   &toolrunner.RegistryRunner{Reg: catalog},
		Policy:  &printPolicy,
		Sandbox: printSandbox,
	}
	return be, gate, &printPolicy
}

// printBackendModel is the model the turn asks the transport for.
//
// The reference spells the reasoning tier into the model's own id
// ("gemini-3.8-flash-low"), so the (--model, --effort) pair is resolved
// into the catalogue id that carries it. A model the user configured
// declares its tiers the same way — "minimax-m3.1-flash-xhigh" is that
// model at the xhigh tier — and the tier then rides the request as the
// upstream's reasoning-effort hint.
//
// A pair that does not resolve (an unknown model, a tier the model does
// not offer) keeps the name the user wrote: the resolution's own error
// belongs to the /model command, not to a turn.
func printBackendModel(fs *flag.FlagSet, modelName string) string {
	resolved, err := ModelResolver(modelName, flagStringValue(fs, "effort"))
	if err != nil || strings.TrimSpace(resolved) == "" {
		return modelName
	}
	return resolved
}

func buildPrintChunkHandler(w io.Writer, cfg *printRunConfig, st *printTurnState) func(backend.StreamChunk) error {
	out := w
	if cfg.format != printFormatText {
		out = &st.responseBuf
	}
	return func(chunk backend.StreamChunk) error {
		if !st.turnStarted {
			st.turnStarted = true
			now := time.Now()
			st.turnStartedAt = now
			st.turnBegan = st.deadline.IsZero() || now.Before(st.deadline)
			if st.turnBegan && cfg.format == printFormatStreamJSON {
				if err := emitStep(w, newStepUpdate(cfg.conversationID, 0, "user_input")); err != nil {
					return err
				}
			}
		}
		if chunk.Error != nil {
			return nil
		}
		if chunk.Meta != nil {
			st.turnUsage = jsonUsage{
				InputTokens:    chunk.Meta.PromptTokens,
				OutputTokens:   chunk.Meta.CompletionTokens,
				ThinkingTokens: chunk.Meta.ReasoningTokens,
				TotalTokens:    chunk.Meta.TotalTokens,
			}
		}
		if chunk.Text != "" {
			if err := handlePrintChunkText(w, out, chunk.Text, cfg, st); err != nil {
				return err
			}
		}
		if chunk.Done {
			if err := handlePrintChunkDone(w, out, cfg, st); err != nil {
				return err
			}
		}
		return nil
	}
}

func handlePrintChunkText(w, out io.Writer, text string, cfg *printRunConfig, st *printTurnState) error {
	sanitized := st.sanitizer.Push(text)
	partial := partialResponseState{
		W:           w,
		ResponseBuf: &st.responseBuf,
		Format:      cfg.format,
		Enabled:     cfg.partialResponse,
	}
	if err := partial.WriteChunk(sanitized); err != nil {
		return err
	}
	if err := st.textRouter.Push(sanitized, out); err != nil {
		return err
	}
	if cfg.rcDaemon != nil && !st.textRouter.UseGlamour() {
		cfg.rcSess.AppendTurnOutput(sanitized)
	}
	return nil
}

func handlePrintChunkDone(w, out io.Writer, cfg *printRunConfig, st *printTurnState) error {
	flushed := st.sanitizer.Finish()
	partial := partialResponseState{
		W:           w,
		ResponseBuf: &st.responseBuf,
		Format:      cfg.format,
		Enabled:     cfg.partialResponse,
	}
	if err := partial.WriteChunk(flushed); err != nil {
		return err
	}
	rendered, err := st.textRouter.Flush(flushed, out)
	if err != nil {
		return err
	}
	if cfg.format == printFormatText {
		fmt.Fprintln(out)
	}
	if cfg.rcDaemon != nil && st.textRouter.UseGlamour() {
		cfg.rcSess.AppendTurnOutput(rendered)
	}
	return nil
}

func waitForBackgroundTasks(ctx context.Context) error {
	reg := tools.SharedTaskRegistry()
	if reg == nil {
		return nil
	}
	ticker := time.NewTicker(50 * time.Millisecond)
	defer ticker.Stop()
	for {
		active := reg.Active()
		running := 0
		for _, t := range active {
			if t.State == taskregistry.StateRunning || t.State == taskregistry.StatePending {
				running++
			}
		}
		if running == 0 {
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

func terminateRunningTasks() []string {
	reg := tools.SharedTaskRegistry()
	if reg == nil {
		return nil
	}
	var terminated []string
	for _, t := range reg.Active() {
		if t.State == taskregistry.StateRunning || t.State == taskregistry.StatePending {
			name := t.Name
			if name == "" {
				name = t.ID
			}
			terminated = append(terminated, name)
			reg.Update(t.ID, func(tk *taskregistry.Task) {
				tk.State = taskregistry.StateKilled
				tk.EndedAt = time.Now()
			})
			if h := reg.GetHandle(t.ID); h != nil {
				if h.Cancel != nil {
					h.Cancel()
				} else if h.Abort != nil {
					h.Abort()
				}
			}
		}
	}
	return terminated
}

func handlePrintTurnTimeout(ctx context.Context, timeout time.Duration, runErr error) (bool, error) {
	timedOut := timeout > 0 &&
		(errors.Is(runErr, context.DeadlineExceeded) || (ctx != nil && ctx.Err() == context.DeadlineExceeded))
	if !timedOut {
		return false, runErr
	}

	terminated := terminateRunningTasks()

	shortErr := fmt.Sprintf("DEADLINE_EXCEEDED: print timeout after %s", timeout)
	if len(terminated) > 0 {
		shortErr = fmt.Sprintf("DEADLINE_EXCEEDED: print timeout after %s (terminated background tasks: %s)", timeout, strings.Join(terminated, ", "))
	}

	payload := map[string]any{
		"short_error": shortErr,
		"status":      "DEADLINE_EXCEEDED",
		"retryable":   false,
		"reason":      "print_timeout",
	}
	if len(terminated) > 0 {
		payload["terminated_tasks"] = terminated
	}
	b, _ := json.Marshal(payload)

	warnLine := printTimeoutWarning(timeout)
	agyLine := "AGY_ERROR: " + string(b)
	stderrMsg := warnLine + "\n" + agyLine

	return true, cli.ExitCodeError(apierror.HeadlessAPIExitCode, stderrMsg)
}

func reportHeadlessDenials(denials []string) {
	if n := len(denials); n > 0 {
		fmt.Fprintln(os.Stderr, permissions.HeadlessNotice(n))
	}
}

func classifyHeadlessExit(runErr error) error {
	if runErr == nil {
		return nil
	}
	if api, ok := apierror.ClassifyAPIError(runErr); ok {
		return cli.ExitCodeError(apierror.HeadlessAPIExitCode, api.Lines())
	}
	return runErr
}

func populateEnvelopeStructuredOutput(env *jsonEnvelope, fs *flag.FlagSet, response string) {
	if fs == nil {
		return
	}
	schemaRaw := printInitJSONSchema(fs)
	if len(schemaRaw) == 0 {
		return
	}
	env.JSONSchema = schemaRaw
	trimmed := strings.TrimSpace(response)
	if trimmed != "" {
		var parsed any
		if err := json.Unmarshal([]byte(trimmed), &parsed); err == nil {
			env.StructuredOutput = parsed
		}
	}
}

func emitFinalPrintEnvelope(w io.Writer, fs *flag.FlagSet, cfg *printRunConfig, st *printTurnState, runErr error, timedOut, interrupted bool) error {
	env := jsonEnvelope{
		ConversationID:  cfg.conversationID,
		Status:          printStatusSuccess,
		Response:        st.responseBuf.String(),
		DurationSeconds: int(time.Since(cfg.started).Seconds()),
		Usage:           st.turnUsage,
	}
	populateEnvelopeStructuredOutput(&env, fs, env.Response)
	if st.turnBegan {
		env.NumTurns = 1 + cfg.sel.PriorUserTurns
	}
	if runErr != nil {
		env.Status = printStatusError
		env.Response = ""
		env.Error = printErrorMessage(runErr)
	}
	// agy 1.4.11's `finished_reason`: why the turn ended early. It is
	// omitted when the turn finished normally.
	switch {
	case timedOut:
		env.FinishedReason = finishedReasonPrintTimeout
	case interrupted:
		env.FinishedReason = finishedReasonInterrupted
	case runErr != nil:
		env.FinishedReason = finishedReasonError
	}
	if cfg.format == printFormatStreamJSON {
		return emitFinalStreamJSON(w, fs, cfg, st, &env, runErr, st.turnBegan, timedOut)
	}
	return writeJSONLine(w, env)
}

func emitFinalStreamJSON(w io.Writer, fs *flag.FlagSet, cfg *printRunConfig, st *printTurnState, env *jsonEnvelope, runErr error, turnBegan, timedOut bool) error {
	if turnBegan && !timedOut {
		stepType := "agent_response"
		if runErr != nil {
			stepType = "error_message"
		}
		// The step's own facts: the text it produced, how long it took and
		// the tokens it spent. The doc's example carries the three on the
		// `agent_response` step (`official-docs/cli/headless.md:144`).
		update := newStepUpdate(cfg.conversationID, 1, stepType)
		update.TextDelta = st.responseBuf.String()
		update.DurationSeconds = st.DurationSeconds()
		if st.turnUsage.known() {
			update.Usage = &st.turnUsage
		}
		if err := emitStep(w, update); err != nil {
			return err
		}
	}
	if printVerbose(fs) && turnBegan && !timedOut && env.Response != "" {
		if err := emitAssistantMessage(w, env.Response, cfg.modelName); err != nil {
			return err
		}
	}
	return writeJSONLine(w, jsonStreamEvent{Event: streamEventResult, Result: env})
}
