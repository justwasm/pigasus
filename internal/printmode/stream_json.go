package printmode

import (
	"bufio"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/justwasm/pigasus/actor"
	"github.com/justwasm/pigasus/internal/app/agentturn"
	"github.com/justwasm/pigasus/internal/app/subagenttools"
	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/core/slashparse"
	"github.com/justwasm/pigasus/internal/features/agents"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/rchost"
)

type streamInputEvent struct {
	Event   string `json:"event"`
	Message struct {
		Content any `json:"content"`
	} `json:"message"`
}

func parseStreamInputContent(raw any) (string, error) {
	switch v := raw.(type) {
	case string:
		if strings.TrimSpace(v) == "" {
			return "", errors.New("empty prompt content")
		}
		return v, nil
	case []any:
		var sb strings.Builder
		for _, item := range v {
			m, ok := item.(map[string]any)
			if !ok {
				return "", errors.New("invalid content block: expected object")
			}
			blockType, _ := m["type"].(string)
			if blockType != "text" {
				return "", fmt.Errorf("unsupported block type %q: text is the only supported block type", blockType)
			}
			txt, _ := m["text"].(string)
			sb.WriteString(txt)
		}
		res := sb.String()
		if strings.TrimSpace(res) == "" {
			return "", errors.New("empty prompt content")
		}
		return res, nil
	default:
		return "", errors.New("invalid content format: expected string or array of text blocks")
	}
}

// RunStreamJSON runs the continuous --input-format stream-json pipeline.
// It opens with an init event, reads prompts one by one on r (stdin),
// executes a turn per prompt against a single continuous session,
// and emits step_update and result events to w (stdout).
func RunStreamJSON(w io.Writer, r io.Reader, fs *flag.FlagSet) error {
	cfg, err := initPrintRunConfig(fs, "")
	if err != nil {
		return err
	}
	defer rchost.ShutdownDaemon(cfg.rcDaemon, os.Stderr)

	cfg.format = printFormatStreamJSON

	workspace, catalog, err := initPrintCatalogAndStream(w, fs, cfg)
	if err != nil {
		return err
	}

	be, gate, policy := setupPrintBackendAndGate(fs, cfg, workspace, catalog)
	workspaces := []string{workspace}
	for _, dir := range flagStringValues(fs, "add-dir") {
		if abs, err := filepath.Abs(dir); err == nil {
			workspaces = append(workspaces, abs)
		}
	}
	if len(workspaces) > 1 {
		tools.SetExtraWorkspaces(workspaces[1:])
	}

	pcfg := actorPrintConfig{
		Backend:        be,
		Model:          cfg.modelName,
		History:        printHistory(cfg.sel),
		Catalog:        catalog,
		Policy:         policy,
		Workspace:      workspace,
		Workspaces:     workspaces,
		AgentDir:       auth.AntigravityHome(),
		ConversationID: cfg.conversationID,
		Effort:         flagStringValue(fs, "effort"),
		AgentBody:      agents.AgentBody(agents.CustomAgentFromFlags(fs)),
		Definitions:    rehydratePrintConversation(cfg.sel, cfg.conversationID),
		Supervisor:     cfg.sel.Supervisor,
	}

	provider, err := preparePrintProvider(pcfg)
	if err != nil {
		return err
	}
	var denials []string
	defer func() {
		gate.Denials = append(gate.Denials, denials...)
		reportHeadlessDenials(gate.Denials)
	}()

	registry := subagenttools.NewRegistry(pcfg.ConversationID)
	deliveries := make(chan subagentDelivery, 8)
	registry.SetNotify(func(sender, content string) {
		select {
		case deliveries <- subagentDelivery{sender: sender, content: content}:
		default:
		}
	})
	pcfg.Registry = registry
	defer registry.Close()

	sysPrompt := buildPrintSystemPrompt(pcfg)
	toolsList, cleanup, err := buildPrintTools(pcfg, provider, func() string { return sysPrompt }, &denials)
	if err != nil {
		return err
	}
	if cleanup != nil {
		defer cleanup()
	}
	setupPrintAskQuestion(pcfg.Catalog)

	ctx, cancel := printContext(cfg.timeout)
	defer cancel()
	guard := newPrintInterruptGuard()
	defer guard.Close()
	guard.cancel(cancel)

	historyTurns := actorSeedHistory(pcfg)
	initialHistory := make([]actor.ConversationMessage, 0, len(historyTurns))
	for _, ht := range historyTurns {
		initialHistory = append(initialHistory, actor.ConversationMessage{
			Role:    actor.Role(strings.ToLower(ht.Role)),
			Content: ht.Text,
		})
	}

	act, err := actor.Spawn(actor.Config{
		ID:             pcfg.ConversationID,
		Name:           "print_agent",
		SystemPrompt:   sysPrompt,
		Model:          pcfg.Model,
		Provider:       provider,
		Tools:          toolsList,
		InitialHistory: initialHistory,
	})
	if err != nil {
		return err
	}
	defer func() {
		_ = act.Close()
	}()

	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)

	turnCount := cfg.sel.PriorUserTurns
	var totalUsage jsonUsage
	sessionStart := cfg.started
	stepIndex := 0

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var input streamInputEvent
		if err := json.Unmarshal([]byte(line), &input); err != nil {
			return fmt.Errorf("invalid json on stdin: %w", err)
		}
		if input.Event != "user" {
			fmt.Fprintf(os.Stderr, "warning: ignoring unsupported stream input message event %q\n", input.Event)
			continue
		}
		promptText, err := parseStreamInputContent(input.Message.Content)
		if err != nil {
			return err
		}
		if strings.HasPrefix(strings.TrimSpace(promptText), "/") {
			cmdName, _ := slashparse.SplitFields(strings.TrimSpace(promptText))
			if _, ok := dispatchPrintSlashCommand(cmdName, "", fs); ok {
				errMsg := fmt.Sprintf("/%s is answered by the CLI itself and is unavailable with --input-format stream-json; run it as its own --print /%s invocation", cmdName, cmdName)
				resEnv := jsonEnvelope{
					ConversationID: cfg.conversationID,
					Status:         printStatusError,
					Response:       "",
					Error:          errMsg,
					Usage:          totalUsage,
				}
				_ = writeJSONLine(w, jsonStreamEvent{
					Event:  streamEventResult,
					Result: &resEnv,
				})
				return errors.New(errMsg)
			}
		}
		turnCount++

		turnState := &printTurnState{
			textRouter:    newPrintTextRouter(false),
			sanitizer:     agentturn.NewSanitizer(),
			turnStarted:   true,
			turnBegan:     true,
			turnStartedAt: time.Now(),
		}
		if cfg.timeout > 0 {
			turnState.deadline = time.Now().Add(cfg.timeout)
		}
		turnCfg := *cfg
		turnCfg.prompt = promptText

		chunkHandler := buildPrintChunkHandler(w, &turnCfg, turnState)

		// 1. Emit user_input step_update
		if err := emitStep(w, newStepUpdate(cfg.conversationID, stepIndex, "user_input")); err != nil {
			return err
		}
		stepIndex++

		// 2. Run model turn
		turnPCfg := pcfg
		turnPCfg.Prompt = promptText
		turnPCfg.Handler = chunkHandler

		runErr := executeActorPrintRun(ctx, act, turnPCfg, deliveries)

		// 3. Emit agent_response step_update
		stepType := "agent_response"
		if runErr != nil {
			stepType = "error_message"
		}
		// The step's own facts: the text it produced, how long it took and
		// the tokens it spent (the doc's example carries the three on the
		// `agent_response` step — `official-docs/cli/headless.md:144`).
		update := newStepUpdate(cfg.conversationID, stepIndex, stepType)
		update.TextDelta = turnState.responseBuf.String()
		update.DurationSeconds = turnState.DurationSeconds()
		if turnState.turnUsage.known() {
			update.Usage = &turnState.turnUsage
		}
		if err := emitStep(w, update); err != nil {
			return err
		}
		stepIndex++

		// 4. Accumulate usage
		totalUsage.InputTokens += turnState.turnUsage.InputTokens
		totalUsage.OutputTokens += turnState.turnUsage.OutputTokens
		totalUsage.ThinkingTokens += turnState.turnUsage.ThinkingTokens
		totalUsage.CacheReadTokens += turnState.turnUsage.CacheReadTokens
		totalUsage.TotalTokens += turnState.turnUsage.TotalTokens

		// 5. Emit cumulative result event
		resultEnv := jsonEnvelope{
			ConversationID:  cfg.conversationID,
			Status:          printStatusSuccess,
			Response:        turnState.responseBuf.String(),
			DurationSeconds: int(time.Since(sessionStart).Seconds()),
			NumTurns:        turnCount,
			Usage:           totalUsage,
		}
		populateEnvelopeStructuredOutput(&resultEnv, fs, resultEnv.Response)
		if runErr != nil {
			resultEnv.Status = printStatusError
			resultEnv.Response = ""
			resultEnv.Error = printErrorMessage(runErr)
		}
		if err := writeJSONLine(w, jsonStreamEvent{
			Event:  streamEventResult,
			Result: &resultEnv,
		}); err != nil {
			return err
		}
		if runErr != nil {
			return runErr
		}
	}
	return scanner.Err()
}
