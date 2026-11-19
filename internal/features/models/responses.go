// responses.go — OpenAI Responses API provider implementation.
//
// Speaks the OpenAI Responses API wire dialect (/v1/responses).
package models

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// responsesProvider talks to the OpenAI Responses API (/v1/responses).
type responsesProvider struct {
	model      Model
	httpClient *http.Client
}

// Name implements Provider.
func (p *responsesProvider) Name() string { return ProviderTypeResponses }

type responsesRequestBody struct {
	Model           string               `json:"model"`
	Instructions    string               `json:"instructions,omitempty"`
	Input           []responsesInputItem `json:"input"`
	Stream          bool                 `json:"stream,omitempty"`
	MaxOutputTokens int                  `json:"max_output_tokens,omitempty"`
	Temperature     *float64             `json:"temperature,omitempty"`
	Reasoning       *responsesReasoning  `json:"reasoning,omitempty"`
	Tools           []responsesTool      `json:"tools,omitempty"`
}

type responsesReasoning struct {
	Effort string `json:"effort,omitempty"`
}

type responsesInputItem struct {
	Type      string `json:"type"` // "message", "function_call", "function_call_output"
	Role      string `json:"role,omitempty"`
	Content   any    `json:"content,omitempty"`
	CallID    string `json:"call_id,omitempty"`
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
	Output    string `json:"output,omitempty"`
}

type responsesTool struct {
	Type        string          `json:"type"` // "function"
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

func (p *responsesProvider) buildBody(req ChatRequest, stream bool) responsesRequestBody {
	modelName := strings.TrimSpace(req.ExternalModel)
	if modelName == "" {
		modelName = strings.TrimSpace(p.model.ExternalModelName)
	}
	if modelName == "" {
		modelName = strings.TrimSpace(p.model.Name)
	}

	body := responsesRequestBody{
		Model:           modelName,
		Instructions:    strings.TrimSpace(req.SystemPrompt),
		Stream:          stream,
		MaxOutputTokens: req.MaxTokens,
		Tools:           formatResponsesTools(req.Tools),
	}
	if req.Temperature > 0 {
		t := req.Temperature
		body.Temperature = &t
	}
	effort := strings.TrimSpace(req.ReasoningEffort)
	if effort == "" {
		effort = strings.TrimSpace(p.model.ReasoningEffort)
	}
	if effort != "" {
		body.Reasoning = &responsesReasoning{Effort: effort}
	}

	for _, m := range req.Messages {
		switch m.Role {
		case "user":
			body.Input = append(body.Input, responsesInputItem{
				Type:    "message",
				Role:    "user",
				Content: m.Content,
			})
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
					Arguments: tc.Arguments,
				})
			}
		case "tool":
			body.Input = append(body.Input, responsesInputItem{
				Type:   "function_call_output",
				CallID: m.ToolCallID,
				Output: m.Content,
			})
		default:
			if m.Role == "system" && body.Instructions == "" {
				body.Instructions = m.Content
			} else {
				body.Input = append(body.Input, responsesInputItem{
					Type:    "message",
					Role:    m.Role,
					Content: m.Content,
				})
			}
		}
	}
	return body
}

func formatResponsesTools(tools []ToolSpec) []responsesTool {
	if len(tools) == 0 {
		return nil
	}
	out := make([]responsesTool, 0, len(tools))
	for _, t := range tools {
		out = append(out, responsesTool{
			Type:        "function",
			Name:        t.Name,
			Description: t.Description,
			Parameters:  t.Parameters,
		})
	}
	return out
}

func (p *responsesProvider) doPost(ctx context.Context, body responsesRequestBody) (*http.Response, error) {
	buf, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("marshal: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.model.APIURL, bytes.NewReader(buf))
	if err != nil {
		return nil, fmt.Errorf("new request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	if p.model.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+p.model.APIKey)
	}
	hasUA := false
	for k, v := range p.model.ExtraHeaders {
		if strings.EqualFold(k, "User-Agent") {
			hasUA = true
		}
		req.Header.Set(k, v)
	}
	if !hasUA {
		req.Header.Set("User-Agent", "opencode/2.3.2")
	}
	return p.httpClient.Do(req)
}

// Chat implements Provider.
func (p *responsesProvider) Chat(ctx context.Context, req ChatRequest) (ChatResponse, error) {
	body := p.buildBody(req, true)
	resp, err := p.doPost(ctx, body)
	if err != nil {
		return ChatResponse{}, err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		return ChatResponse{Raw: raw}, &UpstreamError{
			Status: resp.StatusCode,
			Body:   truncate(string(raw), 240),
		}
	}

	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var (
		contentBuilder strings.Builder
		toolCalls      []ToolCall
		finishReason   string
		promptTokens   int
		outputTokens   int
		totalTokens    int
		rawCapture     bytes.Buffer
	)

	for scanner.Scan() {
		line := scanner.Text()
		rawCapture.WriteString(line)
		rawCapture.WriteByte('\n')

		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}

		var baseEv struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(payload), &baseEv); err != nil {
			continue
		}

		switch baseEv.Type {
		case "response.output_text.delta":
			var ev struct {
				Delta string `json:"delta"`
			}
			if err := json.Unmarshal([]byte(payload), &ev); err == nil {
				contentBuilder.WriteString(ev.Delta)
			}
		case "response.output_item.done":
			var ev struct {
				Item struct {
					Type      string `json:"type"`
					ID        string `json:"id"`
					Name      string `json:"name"`
					CallID    string `json:"call_id"`
					Arguments string `json:"arguments"`
				} `json:"item"`
			}
			if err := json.Unmarshal([]byte(payload), &ev); err == nil && ev.Item.Type == "function_call" {
				id := ev.Item.CallID
				if id == "" {
					id = ev.Item.ID
				}
				toolCalls = append(toolCalls, ToolCall{
					ID:        id,
					Name:      ev.Item.Name,
					Arguments: ev.Item.Arguments,
				})
			}
		case "response.completed":
			finishReason = "stop"
			var ev struct {
				Response struct {
					Status string `json:"status"`
					Usage  struct {
						InputTokens  int `json:"input_tokens"`
						OutputTokens int `json:"output_tokens"`
						TotalTokens  int `json:"total_tokens"`
					} `json:"usage"`
				} `json:"response"`
			}
			if err := json.Unmarshal([]byte(payload), &ev); err == nil {
				promptTokens = ev.Response.Usage.InputTokens
				outputTokens = ev.Response.Usage.OutputTokens
				totalTokens = ev.Response.Usage.TotalTokens
			}
		}
	}

	if err := scanner.Err(); err != nil {
		return ChatResponse{}, fmt.Errorf("read: %w", err)
	}

	return ChatResponse{
		Content:      contentBuilder.String(),
		Model:        body.Model,
		FinishReason: finishReason,
		PromptTokens: promptTokens,
		OutputTokens: outputTokens,
		TotalTokens:  totalTokens,
		ToolCalls:    toolCalls,
		Raw:          rawCapture.Bytes(),
	}, nil
}

// Stream implements Provider.
func (p *responsesProvider) Stream(ctx context.Context, req ChatRequest) (<-chan StreamEvent, error) {
	body := p.buildBody(req, true)
	resp, err := p.doPost(ctx, body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		raw, _ := io.ReadAll(resp.Body)
		_ = resp.Body.Close()
		return nil, &UpstreamError{Status: resp.StatusCode, Body: truncate(string(raw), 240)}
	}

	out := make(chan StreamEvent, 16)
	go p.runStreamLoop(ctx, resp.Body, out)
	return out, nil
}

func (p *responsesProvider) runStreamLoop(ctx context.Context, body io.ReadCloser, out chan<- StreamEvent) {
	defer close(out)
	defer body.Close()

	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var (
		meta      *StreamMeta
		toolCalls []ToolCall
	)

	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "" || payload == "[DONE]" {
			continue
		}

		var baseEv struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal([]byte(payload), &baseEv); err != nil {
			continue
		}

		switch baseEv.Type {
		case "response.output_text.delta":
			var ev struct {
				Delta string `json:"delta"`
			}
			if err := json.Unmarshal([]byte(payload), &ev); err == nil && ev.Delta != "" {
				select {
				case <-ctx.Done():
					out <- StreamEvent{Error: ctx.Err(), Done: true}
					return
				case out <- StreamEvent{Text: ev.Delta}:
				}
			}
		case "response.output_item.done":
			var ev struct {
				Item struct {
					Type      string `json:"type"`
					ID        string `json:"id"`
					Name      string `json:"name"`
					CallID    string `json:"call_id"`
					Arguments string `json:"arguments"`
				} `json:"item"`
			}
			if err := json.Unmarshal([]byte(payload), &ev); err == nil && ev.Item.Type == "function_call" {
				id := ev.Item.CallID
				if id == "" {
					id = ev.Item.ID
				}
				toolCalls = append(toolCalls, ToolCall{
					ID:        id,
					Name:      ev.Item.Name,
					Arguments: ev.Item.Arguments,
				})
			}
		case "response.completed":
			var ev struct {
				Response struct {
					Status string `json:"status"`
					Usage  struct {
						InputTokens  int `json:"input_tokens"`
						OutputTokens int `json:"output_tokens"`
						TotalTokens  int `json:"total_tokens"`
					} `json:"usage"`
				} `json:"response"`
			}
			if err := json.Unmarshal([]byte(payload), &ev); err == nil {
				meta = &StreamMeta{
					PromptTokens:     ev.Response.Usage.InputTokens,
					CompletionTokens: ev.Response.Usage.OutputTokens,
					TotalTokens:      ev.Response.Usage.TotalTokens,
				}
			}
		}
	}

	if err := scanner.Err(); err != nil {
		out <- StreamEvent{Error: fmt.Errorf("sse read: %w", err), Done: true}
		return
	}

	if len(toolCalls) > 0 {
		out <- StreamEvent{ToolCalls: toolCalls}
	}
	out <- StreamEvent{Done: true, Meta: meta}
}
