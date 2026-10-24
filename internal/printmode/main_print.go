package printmode

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/apierror"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/permissions"
	"github.com/justwasm/pigasus/internal/rchost"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/internal/tools"
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
		modelName = auth.ResolveSettingsModel("Gemini 3.8 Flash (High)")
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
	responseBuf strings.Builder
	turnUsage   jsonUsage
	turnStarted bool
	turnBegan   bool
	deadline    time.Time
	textRouter  *printTextRouter
	sanitizer   *agent.Sanitizer
}

func executePrintModelTurn(w io.Writer, fs *flag.FlagSet, cfg *printRunConfig) error {
	state := &printTurnState{
		textRouter: newPrintTextRouter(cfg.format == printFormatText && glamourTTYCheck()),
		sanitizer:  agent.NewSanitizer(),
	}
	if cfg.timeout > 0 {
		state.deadline = cfg.started.Add(cfg.timeout)
	}

	workspace, catalog, err := initPrintCatalogAndStream(w, cfg)
	if err != nil {
		return err
	}

	be, gate, policy := setupPrintBackendAndGate(fs, cfg, workspace, catalog)
	chunkHandler := buildPrintChunkHandler(w, cfg, state)

	ctx, cancel := printContext(cfg.timeout)
	defer cancel()

	denials, runErr := runPigPrintTurn(ctx, pigPrintConfig{
		Backend:        be,
		Model:          cfg.modelName,
		Prompt:         cfg.prompt,
		History:        pigHistory(cfg.sel),
		Catalog:        catalog,
		Policy:         policy,
		Handler:        chunkHandler,
		Workspace:      workspace,
		AgentDir:       auth.AntigravityHome(),
		ConversationID: cfg.conversationID,
		Effort:         flagStringValue(fs, "effort"),
		AgentBody:      AgentBody(CustomAgentFromFlags(fs)),
		Definitions:    rehydratePrintConversation(cfg.sel, cfg.conversationID),
	})
	gate.Denials = append(gate.Denials, denials...)

	timedOut, runErr := handlePrintTurnTimeout(ctx, cfg.timeout, runErr)
	reportHeadlessDenials(gate.Denials)
	exit := classifyHeadlessExit(runErr)

	if cfg.format != printFormatText {
		if err := emitFinalPrintEnvelope(w, fs, cfg, state, runErr, timedOut); err != nil {
			return err
		}
	}
	return exit
}

func initPrintCatalogAndStream(w io.Writer, cfg *printRunConfig) (string, *tools.Registry, error) {
	workspace, _ := os.Getwd()
	catalog := tools.Catalog(workspace)
	initTools := toolDeclsFromRegistry(catalog)
	tools.AddReferenceTools(catalog, workspace)
	if err := maybeEmitStreamInit(w, cfg, workspace, initTools); err != nil {
		return "", nil, err
	}
	return workspace, catalog, nil
}

func maybeEmitStreamInit(w io.Writer, cfg *printRunConfig, workspace string, initTools []backend.ToolDecl) error {
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
			PermissionMode: printPermissionMode(),
		},
	})
}

func setupPrintBackendAndGate(fs *flag.FlagSet, cfg *printRunConfig, workspace string, catalog *tools.Registry) (backend.Client, *permissionRunner, *permissions.Policy) {
	be := BackendPicker(fs, cfg.modelName, printModeSilenceLogger.Writer())
	if resolvedBackend, _ := BackendResolver(fs, io.Discard); resolvedBackend != "" && cfg.rcDaemon != nil {
		cfg.rcSess.SetBackend(resolvedBackend)
	}
	printPolicy := printModePolicy(fs)
	printSandbox := printModeSandbox(fs, workspace, cfg.conversationID)
	gate := &permissionRunner{
		inner:   &registryRunner{reg: catalog},
		policy:  &printPolicy,
		sandbox: printSandbox,
	}
	return be, gate, &printPolicy
}

func buildPrintChunkHandler(w io.Writer, cfg *printRunConfig, st *printTurnState) func(backend.StreamChunk) error {
	out := w
	if cfg.format != printFormatText {
		out = &st.responseBuf
	}
	return func(chunk backend.StreamChunk) error {
		if !st.turnStarted {
			st.turnStarted = true
			st.turnBegan = st.deadline.IsZero() || time.Now().Before(st.deadline)
			if st.turnBegan && cfg.format == printFormatStreamJSON {
				if err := emitStep(w, cfg.conversationID, 0, "user_input"); err != nil {
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

func handlePrintTurnTimeout(ctx context.Context, timeout time.Duration, runErr error) (bool, error) {
	timedOut := timeout > 0 && runErr != nil &&
		(errors.Is(runErr, context.DeadlineExceeded) || ctx.Err() == context.DeadlineExceeded)
	if timedOut {
		fmt.Fprintln(os.Stderr, printTimeoutWarning(timeout))
		runErr = nil
	}
	return timedOut, runErr
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

func emitFinalPrintEnvelope(w io.Writer, fs *flag.FlagSet, cfg *printRunConfig, st *printTurnState, runErr error, timedOut bool) error {
	env := jsonEnvelope{
		ConversationID:  cfg.conversationID,
		Status:          printStatusSuccess,
		Response:        st.responseBuf.String(),
		DurationSeconds: int(time.Since(cfg.started).Seconds()),
		Usage:           st.turnUsage,
	}
	if st.turnBegan {
		env.NumTurns = 1 + cfg.sel.PriorUserTurns
	}
	if runErr != nil {
		env.Status = printStatusError
		env.Response = ""
		env.Error = printErrorMessage(runErr)
	}
	if cfg.format == printFormatStreamJSON {
		return emitFinalStreamJSON(w, fs, cfg, &env, runErr, st.turnBegan, timedOut)
	}
	return writeJSONLine(w, env)
}

func emitFinalStreamJSON(w io.Writer, fs *flag.FlagSet, cfg *printRunConfig, env *jsonEnvelope, runErr error, turnBegan, timedOut bool) error {
	if turnBegan && !timedOut {
		stepType := "agent_response"
		if runErr != nil {
			stepType = "error_message"
		}
		if err := emitStep(w, cfg.conversationID, 1, stepType); err != nil {
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
