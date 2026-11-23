package gateway

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
	"github.com/justwasm/pigasus/internal/infra/backend/streampipe"
)

// ---- Responses API Wire Types ----------------------------------------------

type responsesRequestBody struct {
	Model           string               `json:"model"`
	Instructions    string               `json:"instructions,omitempty"`
	Input           []responsesInputItem `json:"input"`
	Stream          bool                 `json:"stream"`
	Tools           []responsesToolDef   `json:"tools,omitempty"`
	MaxOutputTokens int                  `json:"max_output_tokens,omitempty"`
	Temperature     *float64             `json:"temperature,omitempty"`
	Reasoning       *responsesReasoning  `json:"reasoning,omitempty"`
}

type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

type responsesInputItem struct {
	Type      string `json:"type"` // "message", "function_call", "function_call_output"
	Role      string `json:"role,omitempty"`
	Content   any    `json:"content,omitempty"`
	ID        string `json:"id,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

type responsesToolDef struct {
	Type        string          `json:"type"` // "function"
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

type responsesContentPart struct {
	Type     string `json:"type"` // "input_text" or "input_image"
	Text     string `json:"text,omitempty"`
	ImageURL string `json:"image_url,omitempty"`
}

// buildResponsesRequest renders the OpenAI Responses API body.
func (c *GatewayClient) buildResponsesRequest(req backend.StreamRequest, externalModel string) ([]byte, error) {
	maxTokens := req.MaxTokens
	if maxTokens == 0 {
		maxTokens = c.cfg.DefaultMaxTokens
	}

	var temp *float64
	if req.Temperature > 0 {
		t := req.Temperature
		temp = &t
	}

	supportsToolCalls := c.cfg.SupportsToolCalls
	if req.SupportsToolCalls != nil {
		supportsToolCalls = req.SupportsToolCalls
	}
	var tools []responsesToolDef
	if supportsToolCalls == nil || *supportsToolCalls {
		tools = formatResponsesTools(req)
	}

	body := responsesRequestBody{
		Model:           externalModel,
		Instructions:    strings.TrimSpace(req.SystemPrompt),
		Stream:          true,
		MaxOutputTokens: maxTokens,
		Temperature:     temp,
		Tools:           tools,
	}

	effort := strings.TrimSpace(c.cfg.ReasoningEffort)
	if strings.TrimSpace(req.ReasoningEffort) != "" {
		effort = strings.TrimSpace(req.ReasoningEffort)
	}
	supportsThinking := c.cfg.SupportsThinking
	if req.SupportsThinking != nil {
		supportsThinking = req.SupportsThinking
	}
	if supportsThinking != nil && !*supportsThinking {
		effort = ""
	}
	if effort != "" {
		body.Reasoning = &responsesReasoning{Effort: effort}
	}

	for _, m := range req.Messages {
		switch m.Role {
		case "system":
			if body.Instructions == "" {
				body.Instructions = m.Content
			} else {
				body.Input = append(body.Input, responsesInputItem{
					Type:    "message",
					Role:    "system",
					Content: m.Content,
				})
			}
		case "user":
			if len(m.Images) == 0 {
				body.Input = append(body.Input, responsesInputItem{
					Type:    "message",
					Role:    "user",
					Content: m.Content,
				})
			} else {
				var parts []responsesContentPart
				if m.Content != "" {
					parts = append(parts, responsesContentPart{Type: "input_text", Text: m.Content})
				}
				for _, img := range m.Images {
					if len(img.Data) == 0 {
						continue
					}
					uri := "data:" + img.MIMEType + ";base64," + base64.StdEncoding.EncodeToString(img.Data)
					parts = append(parts, responsesContentPart{Type: "input_image", ImageURL: uri})
				}
				body.Input = append(body.Input, responsesInputItem{
					Type:    "message",
					Role:    "user",
					Content: parts,
				})
			}
		case "assistant":
			if m.Content != "" {
				body.Input = append(body.Input, responsesInputItem{
					Type:    "message",
					Role:    "assistant",
					Content: m.Content,
				})
			}
			for _, tc := range m.ToolCalls {
				body.Input = append(body.Input, responsesInputItem{
					Type:      "function_call",
					CallID:    tc.ID,
					Name:      tc.Name,
					Arguments: string(geminicode.CanonicalArgs(tc.Arguments)),
				})
			}
		case "tool":
			body.Input = append(body.Input, responsesInputItem{
				Type:   "function_call_output",
				CallID: m.ToolCallID,
				Output: m.Content,
			})
		}
	}

	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("gateway: responses marshal: %w", err)
	}
	return raw, nil
}

func formatResponsesTools(req backend.StreamRequest) []responsesToolDef {
	if len(req.Tools) == 0 {
		return nil
	}
	out := make([]responsesToolDef, 0, len(req.Tools))
	for _, t := range req.Tools {
		if strings.TrimSpace(t.Name) == "" {
			continue
		}
		params := t.Parameters
		if len(strings.TrimSpace(string(params))) == 0 {
			params = json.RawMessage(`{"type":"object","properties":{}}`)
		} else if norm := geminicode.NormalizeToolSchema(params, true); norm != nil {
			if b, err := json.Marshal(norm); err == nil {
				params = b
			}
		}
		out = append(out, responsesToolDef{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  params,
		})
	}
	return out
}

// ---- Responses Stream Parser -----------------------------------------------

type responsesToolTracker struct {
	calls map[string]*responsesToolCall
	order []string
}

type responsesToolCall struct {
	id   string
	name string
	args strings.Builder
}

func newResponsesToolTracker() *responsesToolTracker {
	return &responsesToolTracker{
		calls: make(map[string]*responsesToolCall),
	}
}

func (t *responsesToolTracker) addCall(id, name string) {
	if id == "" {
		return
	}
	if _, exists := t.calls[id]; !exists {
		t.calls[id] = &responsesToolCall{id: id, name: name}
		t.order = append(t.order, id)
	} else if name != "" {
		t.calls[id].name = name
	}
}

func (t *responsesToolTracker) appendDelta(id, delta string) {
	if id == "" {
		if len(t.order) > 0 {
			id = t.order[len(t.order)-1]
		} else {
			return
		}
	}
	if call, ok := t.calls[id]; ok {
		call.args.WriteString(delta)
	}
}

func (t *responsesToolTracker) setArgs(id, args string) {
	if id == "" {
		if len(t.order) > 0 {
			id = t.order[len(t.order)-1]
		} else {
			return
		}
	}
	if call, ok := t.calls[id]; ok {
		call.args.Reset()
		call.args.WriteString(args)
	}
}

func (t *responsesToolTracker) emit(ctx context.Context, out chan<- backend.StreamChunk) bool {
	if len(t.order) == 0 {
		return true
	}
	toolCalls := make([]backend.ToolCall, 0, len(t.order))
	for _, id := range t.order {
		call := t.calls[id]
		rawArgs := geminicode.CanonicalArgs([]byte(call.args.String()))
		toolCalls = append(toolCalls, backend.ToolCall{
			ID:        call.id,
			Name:      call.name,
			Arguments: rawArgs,
		})
	}
	t.order = nil
	t.calls = make(map[string]*responsesToolCall)

	select {
	case <-ctx.Done():
		out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
		return false
	case out <- backend.StreamChunk{ToolCalls: toolCalls}:
		return true
	}
}

type responsesSSEEvent struct {
	Type        string                `json:"type"`
	Delta       string                `json:"delta,omitempty"`
	Text        string                `json:"text,omitempty"`
	ItemID      string                `json:"item_id,omitempty"`
	OutputIndex int                   `json:"output_index,omitempty"`
	Item        *responsesSSEItem     `json:"item,omitempty"`
	Response    *responsesSSEResponse `json:"response,omitempty"`
	Error       *sseErrorPayload      `json:"error,omitempty"`
}

type responsesSSEItem struct {
	ID        string `json:"id,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Type      string `json:"type,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type responsesSSEResponse struct {
	ID    string           `json:"id,omitempty"`
	Error *sseErrorPayload `json:"error,omitempty"`
	Usage *struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
		TotalTokens  int `json:"total_tokens"`
	} `json:"usage,omitempty"`
}

func (c *GatewayClient) readResponsesBody(ctx context.Context, resp *http.Response, externalModel string, out chan<- backend.StreamChunk) error {
	var meta *backend.ChunkMeta
	tracker := newResponsesToolTracker()

	done, scanErr := streampipe.ScanSSELines(ctx, resp.Body, 1<<20, c.baseURL, func(line string) (bool, error) {
		return c.handleResponsesSSELine(ctx, line, tracker, &meta, externalModel, out)
	})
	if scanErr != nil {
		if !retry.IsConnectionDropped(scanErr) {
			out <- backend.StreamChunk{Error: scanErr, Done: true}
		}
		return scanErr
	}
	if done {
		return nil
	}
	if !tracker.emit(ctx, out) {
		return nil
	}
	out <- backend.StreamChunk{Done: true, Meta: meta}
	return nil
}

func (c *GatewayClient) handleResponsesSSELine(ctx context.Context, line string, tracker *responsesToolTracker, meta **backend.ChunkMeta, externalModel string, out chan<- backend.StreamChunk) (bool, error) {
	if !strings.HasPrefix(line, "data:") {
		return false, nil
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" {
		return false, nil
	}
	if payload == "[DONE]" {
		if !tracker.emit(ctx, out) {
			return true, nil
		}
		out <- backend.StreamChunk{Done: true, Meta: *meta}
		return true, nil
	}

	var ev responsesSSEEvent
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		return false, nil
	}

	var errPayload *sseErrorPayload
	if ev.Error != nil {
		errPayload = ev.Error
	} else if ev.Response != nil && ev.Response.Error != nil {
		errPayload = ev.Response.Error
	}
	if errPayload != nil || ev.Type == "response.failed" || ev.Type == "error" {
		var errType, errCode, errMsg string
		if errPayload != nil {
			errType = errPayload.Type
			errCode = formatSSEErrorCode(errPayload.Code)
			errMsg = errPayload.Message
		}
		if errMsg == "" && errCode != "" {
			errMsg = errCode
		}
		if errMsg == "" {
			errMsg = "responses api call failed"
		}
		return true, sseErrorToHTTPError(errType, errCode, errMsg, []byte(payload), c.baseURL)
	}

	switch ev.Type {
	case "response.output_text.delta", "response.text.delta":
		if ev.Delta != "" {
			select {
			case <-ctx.Done():
				out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
				return true, ctx.Err()
			case out <- backend.StreamChunk{Text: ev.Delta}:
			}
		}
	case "response.reasoning_text.delta", "response.reasoning_summary_text.delta":
		// Reasoning deltas from Responses API
		if ev.Delta != "" {
			select {
			case <-ctx.Done():
				out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
				return true, ctx.Err()
			case out <- backend.StreamChunk{Text: ev.Delta}:
			}
		}
	case "response.output_item.added":
		if ev.Item != nil && ev.Item.Type == "function_call" {
			id := ev.Item.CallID
			if id == "" {
				id = ev.Item.ID
			}
			tracker.addCall(id, ev.Item.Name)
		}
	case "response.function_call_arguments.delta":
		tracker.appendDelta(ev.ItemID, ev.Delta)
	case "response.function_call_arguments.done":
		if ev.Text != "" {
			tracker.setArgs(ev.ItemID, ev.Text)
		}
	case "response.output_item.done":
		if ev.Item != nil && ev.Item.Type == "function_call" {
			id := ev.Item.CallID
			if id == "" {
				id = ev.Item.ID
			}
			tracker.addCall(id, ev.Item.Name)
			if ev.Item.Arguments != "" {
				tracker.setArgs(id, ev.Item.Arguments)
			}
		}
	case "response.completed", "response.done":
		if ev.Response != nil && ev.Response.Usage != nil {
			*meta = &backend.ChunkMeta{
				PromptTokens:     ev.Response.Usage.InputTokens,
				CompletionTokens: ev.Response.Usage.OutputTokens,
				TotalTokens:      ev.Response.Usage.TotalTokens,
			}
		}
		if !tracker.emit(ctx, out) {
			return true, nil
		}
		out <- backend.StreamChunk{Done: true, Meta: *meta}
		return true, nil
	}

	return false, nil
}
