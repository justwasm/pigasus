package agentserver

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/endpoints"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/features/models"
	"github.com/justwasm/pigasus/internal/features/models/catalog"
	"github.com/justwasm/pigasus/internal/infra/backend/cloudcode"
	backendselect "github.com/justwasm/pigasus/internal/infra/backend/select"
	"github.com/justwasm/pigasus/internal/infra/mockremoteagent"
	"github.com/justwasm/pigasus/internal/rchost"
	"github.com/justwasm/pigasus/internal/remotecontrol"
	"github.com/justwasm/pigasus/internal/remotecontrol/cascades"
)

// resolveWorkspace extracts and cleans the target workspace directory.
// Priority:
// 1. First valid directory in session.WorkspaceURIs (handling file:// and URL-encoding)
// 2. defaultWorkspace flag
// 3. Current process working directory
func resolveWorkspace(session *mockremoteagent.CascadeSession, defaultWS string) string {
	if session != nil && len(session.WorkspaceURIs) > 0 {
		rawURI := session.WorkspaceURIs[0]
		parsed, err := url.Parse(rawURI)
		var wsPath string
		if err == nil && parsed.Scheme == "file" {
			wsPath, _ = url.PathUnescape(parsed.Path)
		} else {
			wsPath = strings.TrimPrefix(rawURI, "file://")
			wsPath, _ = url.PathUnescape(wsPath)
		}
		if info, err := os.Stat(wsPath); err == nil && info.IsDir() {
			if abs, err := filepath.Abs(wsPath); err == nil {
				return abs
			}
			return wsPath
		}
	}
	if defaultWS != "" {
		if abs, err := filepath.Abs(defaultWS); err == nil {
			return abs
		}
		return defaultWS
	}
	wd, err := os.Getwd()
	if err == nil {
		return wd
	}
	return "."
}

// extractHistory reconstructs prior conversation turns from session steps.
func extractHistory(session *mockremoteagent.CascadeSession) []cascades.HistoryTurn {
	if session == nil {
		return nil
	}
	steps := session.StepsSnapshot()
	if len(steps) <= 1 {
		return nil
	}
	var history []cascades.HistoryTurn
	// Exclude the last step, which is the current turn's USER_INPUT.
	for i := 0; i < len(steps)-1; i++ {
		s := steps[i]
		switch s.Type {
		case "USER_INPUT", "CORTEX_STEP_TYPE_USER_INPUT":
			if s.Content != "" {
				history = append(history, cascades.HistoryTurn{
					Role: "user",
					Text: s.Content,
				})
			}
		case "PLANNER_RESPONSE", "CORTEX_STEP_TYPE_PLANNER_RESPONSE":
			if s.Content != "" {
				history = append(history, cascades.HistoryTurn{
					Role: "assistant",
					Text: s.Content,
				})
			}
		}
	}
	return history
}

// AgentHandlerConfig is the Pigasus-side configuration of the bridge.
// The model is chosen here, not by the remote client: agy's `--remote-agent`
// model selection only affects its TUI, while the local mock decides which
// provider credential actually serves the turn.
type AgentHandlerConfig struct {
	// Model is a reference model id/label (gemini-3.8-flash-high) or a
	// configured model name from providers.json. Empty reads
	// OPENAGY_MODEL, then falls back to gemini-3.8-flash-high.
	Model string
	// Workspace overrides the workspace URI the remote client sends.
	Workspace string
	// CloudCodeEndpoint overrides the reference endpoint. Empty uses
	// endpoints.DefaultCloudCode (daily-cloudcode-pa.googleapis.com).
	CloudCodeEndpoint string
}

// agentBackendOverride is the test seam for the backend factory. Production
// never sets it.
var agentBackendOverride func(model string) backend.Client

// resolveAgentModel maps a Pigasus-side model setting to the wire model.
// reference reports whether it is served by CloudCode (so the bridge builds
// the official transport) rather than a configured gateway.
func resolveAgentModel(requested string) (model string, reference bool, ok bool) {
	requested = strings.TrimSpace(requested)
	if requested == "" {
		requested = strings.TrimSpace(os.Getenv("OPENAGY_MODEL"))
	}
	if requested == "" {
		requested = "gemini-3.8-flash-high"
	}
	for _, e := range catalog.ReferenceModels() {
		if e.ID == requested || e.Label == requested {
			return e.ID, true, true
		}
	}
	if m, found := models.CachedRegistry().GetByName(requested); found && m != nil {
		return requested, false, true
	}
	return requested, false, false
}

// tokenSource prefers the bearer token the remote client sent (it belongs to
// the signed-in account) and otherwise reads the unified credential from this
// machine. It never returns an empty token: an unauthenticated turn must
// surface the real error instead of silently falling back to the mock client.
func tokenSource(session *mockremoteagent.CascadeSession) func(context.Context) (string, error) {
	return func(ctx context.Context) (string, error) {
		if session != nil {
			if t := strings.TrimSpace(session.AuthToken); t != "" {
				return t, nil
			}
		}
		tok, err := auth.LoadTokenUnified()
		if err != nil {
			return "", err
		}
		if tok == nil || tok.Token.AccessToken == "" {
			return "", auth.ErrNoToken
		}
		updated, _, rerr := tok.RefreshIfNeeded(ctx)
		if rerr != nil {
			if updated == nil {
				return "", rerr
			}
			tok = updated
		} else if updated != nil {
			tok = updated
		}
		return tok.Token.AccessToken, nil
	}
}

// agentBackend builds the real transport for the Pigasus-side model.
//
// A reference model always goes through CloudCode with an explicit endpoint
// and token source; a configured model goes through the gateway picker. The
// function deliberately never calls backend.NewMock(): a failure must reach
// the TUI as an error, not as a canned "mock reply from model=…" answer.
func agentBackend(session *mockremoteagent.CascadeSession, model string, reference bool, endpoint string) backend.Client {
	if agentBackendOverride != nil {
		return agentBackendOverride(model)
	}
	if reference {
		return cloudcode.NewCloudCodeWithConfig(cloudcode.CloudCodeConfig{
			Endpoint:  endpoint,
			Model:     model,
			TokenFunc: tokenSource(session),
		})
	}
	return backendselect.Pick(nil, model, nil)
}

// NewPigasusAgentTurnHandler creates a CustomTurnHandler that delegates turns
// to Pigasus's full agent runtime via rchost.NewRemoteCascadeSession.
func NewPigasusAgentTurnHandler(cfg AgentHandlerConfig) mockremoteagent.CustomTurnHandler {
	return func(ctx context.Context, session *mockremoteagent.CascadeSession, userPrompt string) {
		go func() {
			turnCtx, cancel := session.BeginTurn()
			defer cancel()

			ws := resolveWorkspace(session, cfg.Workspace)
			resolvedModel, reference, ok := resolveAgentModel(cfg.Model)
			if !ok {
				msg := fmt.Sprintf("Model %q is not available on this machine. "+
					"Set --model to a reference model (for example gemini-3.8-flash-high) "+
					"or configure the model in providers.json.", cfg.Model)
				log.Printf("[AgentHandler] %s", msg)
				session.AddStep("PLANNER_RESPONSE", msg, "", nil)
				session.SetStatus("IDLE")
				return
			}
			endpoint := strings.TrimSpace(cfg.CloudCodeEndpoint)
			if endpoint == "" {
				endpoint = endpoints.DefaultCloudCode
			}
			log.Printf("[AgentHandler] turn: model=%s reference=%v workspace=%s endpoint=%s client_token=%v",
				resolvedModel, reference, ws, endpoint, strings.TrimSpace(session.AuthToken) != "")

			backendPicker := func(string) backend.Client {
				return agentBackend(session, resolvedModel, reference, endpoint)
			}

			// Instantiate Pigasus Remote Cascade Session
			sessFactory := rchost.NewRemoteCascadeSession(rchost.RemoteCascadeOptions{
				Backend: backendPicker(resolvedModel),
				BackendFactory: func(m string) backend.Client {
					return backendPicker(m)
				},
				Workspace:       ws,
				AgentDir:        auth.AntigravityHome(),
				Browser:         nil,
				InteractionHost: &sessionInteractionHost{session: session},
			})

			history := extractHistory(session)
			prompt := cascades.Prompt{
				ConversationID: session.ID,
				Text:           userPrompt,
				Model:          resolvedModel,
				History:        history,
			}

			rcSession, err := sessFactory(turnCtx, prompt, cascades.SendOptions{})
			if err != nil {
				log.Printf("[AgentHandler] Failed to initialize remote cascade session: %v", err)
				session.AddStep("PLANNER_RESPONSE", fmt.Sprintf("Error initializing agent session: %v", err), "", nil)
				session.SetStatus("IDLE")
				return
			}
			defer rcSession.Close()

			// Prepare Step 1: Initial generating response
			currentStep := session.AddStepWithStatus(
				"PLANNER_RESPONSE",
				"CORTEX_STEP_STATUS_GENERATING",
				"",
				"",
				nil,
			)
			stepIndex := currentStep.StepIndex

			events := make(chan remotecontrol.SessionEvent, 128)
			sendDone := make(chan struct{})

			go func() {
				defer close(sendDone)
				_, _ = rcSession.Send(turnCtx, userPrompt, events)
			}()

			var (
				mu           sync.Mutex
				responseBuf  strings.Builder
				dirty        bool
				turnFinished bool
			)

			// SSE Debounce ticker (60ms) to prevent overwhelming the client
			ticker := time.NewTicker(60 * time.Millisecond)
			defer ticker.Stop()

			flush := func(status string) {
				mu.Lock()
				defer mu.Unlock()
				if !dirty && status == "CORTEX_STEP_STATUS_GENERATING" {
					return
				}
				session.UpdateStep(
					stepIndex,
					status,
					responseBuf.String(),
					"",
					nil,
				)
				dirty = false
			}

			for !turnFinished {
				select {
				case <-turnCtx.Done():
					flush("CORTEX_STEP_STATUS_DONE")
					session.SetStatus("IDLE")
					return

				case <-ticker.C:
					flush("CORTEX_STEP_STATUS_GENERATING")

				case ev, ok := <-events:
					if !ok {
						turnFinished = true
						break
					}
					switch ev.Type {
					case remotecontrol.SessionEventTextDelta:
						if ev.Delta != "" {
							mu.Lock()
							responseBuf.WriteString(ev.Delta)
							dirty = true
							mu.Unlock()
						}

					case remotecontrol.SessionEventToolCall:
						// 1. Finalize the preceding thought/text step
						flush("CORTEX_STEP_STATUS_DONE")

						// 2. Map and add the tool step
						tc := ev.ToolCall
						if fileChangeStep(session, ws, tc.Name, tc.Args) {
							// File edits publish a FILE_CHANGE step with the
							// DiffBlock /diff opens.
						} else if tc.Name == "run_command" {
							var cmdArgs struct {
								CommandLine string `json:"CommandLine"`
								Command     string `json:"command"`
								Cmd         string `json:"cmd"`
							}
							_ = json.Unmarshal(tc.Args, &cmdArgs)
							cmdLine := cmdArgs.CommandLine
							if cmdLine == "" {
								cmdLine = cmdArgs.Command
							}
							if cmdLine == "" {
								cmdLine = cmdArgs.Cmd
							}
							if cmdLine == "" {
								cmdLine = string(tc.Args)
							}

							toolPayload := map[string]any{
								"commandLine": cmdLine,
								"stdout":      tc.Result,
								"stderr":      "",
								"exitCode":    0,
								"combined":    tc.Result,
							}
							session.AddStep("RUN_COMMAND", cmdLine, "", []any{toolPayload})
						} else {
							toolPayload := map[string]any{
								"toolName": tc.Name,
								"args":     string(tc.Args),
								"result":   tc.Result,
							}
							session.AddStep("GENERIC", tc.Name, "", []any{toolPayload})
						}

						// 3. Allocate a fresh PLANNER_RESPONSE step for continued generation
						nextStep := session.AddStepWithStatus(
							"PLANNER_RESPONSE",
							"CORTEX_STEP_STATUS_GENERATING",
							"",
							"",
							nil,
						)
						mu.Lock()
						stepIndex = nextStep.StepIndex
						responseBuf.Reset()
						dirty = false
						mu.Unlock()

					case remotecontrol.SessionEventDone:
						turnFinished = true

					case remotecontrol.SessionEventError:
						if ev.Error != nil {
							mu.Lock()
							if responseBuf.Len() > 0 {
								responseBuf.WriteString("\n\n")
							}
							responseBuf.WriteString(fmt.Sprintf("Error: %v", ev.Error))
							dirty = true
							mu.Unlock()
						}
						turnFinished = true
					}
				}
			}

			// Wait for rcSession.Send to finish
			<-sendDone

			// Final flush to guarantee status is DONE
			flush("CORTEX_STEP_STATUS_DONE")

			select {
			case <-turnCtx.Done():
				return
			case <-time.After(50 * time.Millisecond):
			}

			session.SetStatus("IDLE")
		}()
	}
}
