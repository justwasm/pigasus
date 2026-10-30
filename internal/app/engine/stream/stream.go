package stream

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/justwasm/pigasus/internal/agyprompt"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/backend"
	"github.com/justwasm/pigasus/internal/backendselect"
	"github.com/justwasm/pigasus/internal/cascade"
	"github.com/justwasm/pigasus/internal/modelcatalog"
	"github.com/justwasm/pigasus/internal/modular"
	"github.com/justwasm/pigasus/internal/tools"
)


func init() {
	modular.RegisterEngine("stream", NewEngine())
}

// Engine implements modular.Engine with a pure-Go streaming turn loop.
type Engine struct {
	maxTurns int
}

// NewEngine returns a configured StreamEngine.
func NewEngine() *Engine {
	return &Engine{
		maxTurns: 10,
	}
}

func (e *Engine) Name() string {
	return "stream"
}

// Send executes one conversational turn, streaming output and running tool calls if requested.
func (e *Engine) Send(ctx context.Context, input modular.TurnInput) (*modular.TurnResult, error) {
	out := input.Out
	if out == nil {
		out = os.Stdout
	}
	errOut := input.ErrOut
	if errOut == nil {
		errOut = os.Stderr
	}

	transition := func(from, to cascade.CascaderState, note string) {
		if input.OnStateTransition != nil {
			input.OnStateTransition(from, to, note)
		}
	}

	transition(cascade.CascaderInit, cascade.CascaderReady, "loading context and prompt")

	// 1. Resolve Model
	modelName := input.Model
	if modelName == "" {
		modelName = auth.ResolveSettingsModel("Gemini 3.8 Flash (High)")
	}
	resolvedModel, err := modelcatalog.ResolveModelSelection(modelName, input.Effort)
	if err == nil && resolvedModel != "" {
		modelName = resolvedModel
	}

	transition(cascade.CascaderReady, cascade.CascaderExecute, fmt.Sprintf("model %s selected", modelName))

	// 2. Resolve Backend Client
	var client backend.Client
	if input.Backend != nil {
		client = input.Backend
	} else {
		client = backendselect.Pick(input.FlagSet, modelName, nil)
	}
	if client == nil {
		return nil, fmt.Errorf("stream: failed to initialize backend client for %q", modelName)
	}


	// 3. Resolve System Prompt
	sysPrompt := input.SystemPrompt
	if sysPrompt == "" {
		cwd, _ := os.Getwd()
		appData := auth.AntigravityConfigHome()
		convID := agyprompt.NewConversationID()
		opts := agyprompt.DefaultOptions(cwd, appData, convID)
		sysPrompt = agyprompt.Build(opts)
	}

	// 4. Resolve Tools
	toolDecls := input.Tools
	if len(toolDecls) == 0 {
		toolDecls = modular.AllToolDeclarations()
	}

	toolRunner := input.ToolRunner
	if toolRunner == nil {
		toolRunner = modular.DispatchToolCall
	}

	// 5. Setup Messages
	messages := make([]backend.Message, len(input.Messages))
	copy(messages, input.Messages)
	if len(messages) == 0 && input.Prompt != "" {
		messages = append(messages, backend.Message{
			Role:    "user",
			Content: input.Prompt,
		})
	}

	res := &modular.TurnResult{
		Model: modelName,
	}

	// 6. Multi-turn execution loop
	var fullResponse strings.Builder
	for turn := 0; turn < e.maxTurns; turn++ {
		res.TurnCount++
		transition(cascade.CascaderReady, cascade.CascaderExecute, fmt.Sprintf("turn %d execution", turn+1))

		req := backend.StreamRequest{
			Model:        modelName,
			SystemPrompt: sysPrompt,
			Messages:     messages,
			Tools:        toolDecls,
		}

		streamCh, err := client.Stream(ctx, req)
		if err != nil {
			transition(cascade.CascaderExecute, cascade.CascaderError, err.Error())
			return nil, fmt.Errorf("stream backend: %w", err)
		}

		transition(cascade.CascaderExecute, cascade.CascaderStream, "reading chunks")

		var assistantText strings.Builder
		var accumulatedToolCalls []backend.ToolCall

		for chunk := range streamCh {
			if chunk.Error != nil {
				transition(cascade.CascaderStream, cascade.CascaderError, chunk.Error.Error())
				return nil, chunk.Error
			}
			if chunk.Text != "" {
				assistantText.WriteString(chunk.Text)
				fullResponse.WriteString(chunk.Text)
				if input.OnChunk != nil {
					_ = input.OnChunk(chunk)
				} else {
					_, _ = fmt.Fprint(out, chunk.Text)
				}
			}
			if len(chunk.ToolCalls) > 0 {
				accumulatedToolCalls = append(accumulatedToolCalls, chunk.ToolCalls...)
			}
			if chunk.Meta != nil {
				res.InputTokens += chunk.Meta.PromptTokens
				res.OutputTokens += chunk.Meta.CompletionTokens
			}
		}

		// If no tool calls were requested, we are done
		if len(accumulatedToolCalls) == 0 {
			break
		}

		// Append the assistant's request-to-act turn
		messages = append(messages, backend.Message{
			Role:      "assistant",
			Content:   assistantText.String(),
			ToolCalls: accumulatedToolCalls,
		})

		// Execute all requested tool calls
		for _, tc := range accumulatedToolCalls {
			toolResult, toolErr := toolRunner(ctx, tc)
			isErr := false
			if toolErr != nil {
				toolResult = fmt.Sprintf("Tool %s execution failed: %v", tc.Name, toolErr)
				isErr = true
			} else if tools.IsFailureText(toolResult) {
				isErr = true
			}
			messages = append(messages, backend.Message{
				Role:       "tool",
				ToolCallID: tc.ID,
				Name:       tc.Name,
				Content:    toolResult,
				IsError:    isErr,
			})
		}


		transition(cascade.CascaderStream, cascade.CascaderExecute, "tool execution complete; planning follow-up turn")
	}

	transition(cascade.CascaderStream, cascade.CascaderSave, "saving transcript")
	transition(cascade.CascaderSave, cascade.CascaderDone, "turn complete")

	res.Response = fullResponse.String()
	return res, nil
}
