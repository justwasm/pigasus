// gateway_anthropic.go — the Anthropic Messages dialect of the gateway
// transport.
//
// The OpenAI-compatible dialect in gateway.go and this one share the HTTP
// plumbing (the connection-drop retry budget, the *HTTPError
// classification, the idle-timeout reader) and differ in two places: the
// request body, and the way the response stream is read.
package backend

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
)

// anthropicRequestBody is the Messages request shape.
type anthropicRequestBody struct {
	Model     string             `json:"model"`
	MaxTokens int                `json:"max_tokens,omitempty"`
	System    string             `json:"system,omitempty"`
	Messages  []anthropicMessage `json:"messages"`
	Stream    bool               `json:"stream"`
	Tools     []anthropicTool    `json:"tools,omitempty"`
}

type anthropicMessage struct {
	Role    string             `json:"role"`
	Content []anthropicContent `json:"content"`
}

type anthropicContent struct {
	Type      string          `json:"type"`
	Text      string          `json:"text,omitempty"`
	ID        string          `json:"id,omitempty"`
	Name      string          `json:"name,omitempty"`
	Input     json.RawMessage `json:"input,omitempty"`
	ToolUseID string          `json:"tool_use_id,omitempty"`
	Content   string          `json:"content,omitempty"`
}

type anthropicTool struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	InputSchema json.RawMessage `json:"input_schema,omitempty"`
}

type anthropicEvent struct {
	Type    string `json:"type"`
	Message struct {
		ID    string `json:"id"`
		Usage struct {
			InputTokens  int `json:"input_tokens"`
			OutputTokens int `json:"output_tokens"`
		} `json:"usage"`
	} `json:"message"`
	Index        int `json:"index"`
	ContentBlock struct {
		Type  string          `json:"type"`
		ID    string          `json:"id"`
		Name  string          `json:"name"`
		Text  string          `json:"text"`
		Input json.RawMessage `json:"input"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		PartialJSON string `json:"partial_json"`
	} `json:"delta"`
	Usage struct {
		InputTokens  int `json:"input_tokens"`
		OutputTokens int `json:"output_tokens"`
	} `json:"usage"`
}

type anthropicToolBlock struct {
	id    string
	name  string
	index int
	args  strings.Builder
}

type anthropicToolTracker struct {
	blocks map[int]*anthropicToolBlock
	order  []int
}

func newAnthropicToolTracker() *anthropicToolTracker {
	return &anthropicToolTracker{blocks: map[int]*anthropicToolBlock{}}
}

func (t *anthropicToolTracker) emit(ctx context.Context, out chan<- StreamChunk) bool {
	if len(t.order) == 0 {
		return true
	}
	calls := make([]ToolCall, 0, len(t.order))
	for _, idx := range t.order {
		b := t.blocks[idx]
		if b == nil || strings.TrimSpace(b.name) == "" {
			continue
		}
		args := b.args.String()
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		calls = append(calls, ToolCall{
			ID:        b.id,
			Name:      b.name,
			Arguments: json.RawMessage(args),
		})
	}
	if len(calls) == 0 {
		return true
	}
	select {
	case <-ctx.Done():
		out <- StreamChunk{Error: ctx.Err(), Done: true}
		return false
	case out <- StreamChunk{ToolCalls: calls}:
		return true
	}
}

// buildAnthropicRequest renders the Messages body.
func (c *GatewayClient) buildAnthropicRequest(req StreamRequest, externalModel string) ([]byte, error) {
	maxTokens := req.MaxTokens
	if maxTokens <= 0 {
		maxTokens = c.defaultMaxTokens()
	}
	sys := req.SystemPrompt
	body := anthropicRequestBody{
		Model:     externalModel,
		MaxTokens: maxTokens,
		Stream:    true,
		System:    sys,
		Messages:  formatAnthropicMessages(req.Messages, &sys),
		Tools:     formatAnthropicTools(req.Tools),
	}
	body.System = sys
	raw, err := json.Marshal(body)
	if err != nil {
		return nil, fmt.Errorf("gateway: marshal (anthropic): %w", err)
	}
	return raw, nil
}

func formatAnthropicMessages(messages []Message, system *string) []anthropicMessage {
	var out []anthropicMessage
	for _, m := range messages {
		if m.Role == "system" {
			if *system == "" {
				*system = m.Content
			}
			continue
		}
		if msg, ok := formatAnthropicMessage(m); ok {
			out = append(out, msg)
		}
	}
	return out
}

func formatAnthropicMessage(m Message) (anthropicMessage, bool) {
	switch m.Role {
	case "assistant":
		blocks := formatAnthropicAssistantBlocks(m)
		if len(blocks) == 0 {
			return anthropicMessage{}, false
		}
		return anthropicMessage{Role: "assistant", Content: blocks}, true
	case "tool":
		return anthropicMessage{
			Role: "user",
			Content: []anthropicContent{{
				Type:      "tool_result",
				ToolUseID: m.ToolCallID,
				Content:   m.Content,
			}},
		}, true
	default:
		return anthropicMessage{
			Role:    "user",
			Content: []anthropicContent{{Type: "text", Text: m.WireText()}},
		}, true
	}
}

func formatAnthropicAssistantBlocks(m Message) []anthropicContent {
	var blocks []anthropicContent
	if text := strings.TrimSpace(m.Content); text != "" {
		blocks = append(blocks, anthropicContent{Type: "text", Text: m.Content})
	}
	for _, tc := range m.ToolCalls {
		args := tc.Arguments
		if len(strings.TrimSpace(string(args))) == 0 {
			args = json.RawMessage("{}")
		}
		blocks = append(blocks, anthropicContent{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Name,
			Input: args,
		})
	}
	return blocks
}

func formatAnthropicTools(tools []ToolDecl) []anthropicTool {
	var out []anthropicTool
	for _, t := range tools {
		if strings.TrimSpace(t.Name) == "" {
			continue
		}
		schema := t.Parameters
		if len(strings.TrimSpace(string(schema))) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		}
		out = append(out, anthropicTool{
			Name:        t.Name,
			Description: t.Description,
			InputSchema: schema,
		})
	}
	return out
}

const defaultMaxTokens = 4096

func (c *GatewayClient) defaultMaxTokens() int {
	if c.defaultMax > 0 {
		return c.defaultMax
	}
	return defaultMaxTokens
}

// readAnthropicBody reads one Messages response body to completion.
func (c *GatewayClient) readAnthropicBody(ctx context.Context, resp *http.Response, externalModel string, out chan<- StreamChunk) error {
	idleR := NewIdleTimeoutReader(resp.Body, StreamIdleTimeout)
	scanner := bufio.NewScanner(idleR)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)

	var meta *ChunkMeta
	tracker := newAnthropicToolTracker()

	for scanner.Scan() {
		line := scanner.Text()
		done, err := c.handleAnthropicSSELine(ctx, line, tracker, &meta, externalModel, out)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		classified := ClassifyTransportError(fmt.Errorf("gateway: anthropic sse read: %w", err), c.baseURL)
		if !IsConnectionDropped(classified) {
			out <- StreamChunk{Error: classified, Done: true}
		}
		return classified
	}
	if !tracker.emit(ctx, out) {
		return nil
	}
	out <- StreamChunk{Done: true, Meta: meta}
	return nil
}

func (c *GatewayClient) handleAnthropicSSELine(ctx context.Context, line string, tracker *anthropicToolTracker, meta **ChunkMeta, externalModel string, out chan<- StreamChunk) (bool, error) {
	if !strings.HasPrefix(line, "data:") {
		return false, nil
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" {
		return false, nil
	}
	var ev anthropicEvent
	if err := json.Unmarshal([]byte(payload), &ev); err != nil {
		select {
		case <-ctx.Done():
			out <- StreamChunk{Error: ctx.Err(), Done: true}
		default:
			out <- StreamChunk{Error: fmt.Errorf("gateway: anthropic sse decode: %w", err), Done: true}
		}
		return true, fmt.Errorf("gateway: anthropic sse decode: %w", err)
	}
	return c.dispatchAnthropicEvent(ctx, ev, payload, tracker, meta, externalModel, out)
}

func (c *GatewayClient) dispatchAnthropicEvent(ctx context.Context, ev anthropicEvent, payload string, tracker *anthropicToolTracker, meta **ChunkMeta, externalModel string, out chan<- StreamChunk) (bool, error) {
	switch ev.Type {
	case "message_start":
		*meta = &ChunkMeta{
			PromptTokens:      ev.Message.Usage.InputTokens,
			CompletionTokens:  ev.Message.Usage.OutputTokens,
			SystemFingerprint: GatewaySystemFingerprint(c.baseURL, externalModel),
		}
	case "content_block_start":
		if ev.ContentBlock.Type == "tool_use" {
			b := &anthropicToolBlock{id: ev.ContentBlock.ID, name: ev.ContentBlock.Name, index: ev.Index}
			if _, seen := tracker.blocks[ev.Index]; !seen {
				tracker.order = append(tracker.order, ev.Index)
			}
			tracker.blocks[ev.Index] = b
		}
	case "content_block_delta":
		return false, handleAnthropicDelta(ctx, ev, tracker.blocks, out)
	case "message_delta":
		updateAnthropicMeta(meta, ev, c.baseURL, externalModel)
	case "message_stop":
		if !tracker.emit(ctx, out) {
			return true, nil
		}
		out <- StreamChunk{Done: true, Meta: *meta}
		return true, nil
	case "error":
		err := fmt.Errorf("gateway: anthropic stream error: %s", payload)
		out <- StreamChunk{Error: err, Done: true}
		return true, err
	}
	return false, nil
}

func handleAnthropicDelta(ctx context.Context, ev anthropicEvent, blocks map[int]*anthropicToolBlock, out chan<- StreamChunk) error {
	b, isTool := blocks[ev.Index]
	switch ev.Delta.Type {
	case "text_delta":
		if ev.Delta.Text == "" {
			return nil
		}
		select {
		case <-ctx.Done():
			out <- StreamChunk{Error: ctx.Err(), Done: true}
			return ctx.Err()
		case out <- StreamChunk{Text: ev.Delta.Text}:
			return nil
		}
	case "thinking_delta":
		return nil
	case "input_json_delta":
		if isTool {
			b.args.WriteString(ev.Delta.PartialJSON)
		}
	}
	return nil
}

func updateAnthropicMeta(meta **ChunkMeta, ev anthropicEvent, baseURL, externalModel string) {
	if *meta == nil {
		*meta = &ChunkMeta{SystemFingerprint: GatewaySystemFingerprint(baseURL, externalModel)}
	}
	if ev.Usage.InputTokens > 0 {
		(*meta).PromptTokens = ev.Usage.InputTokens
	}
	if ev.Usage.OutputTokens > 0 {
		(*meta).CompletionTokens = ev.Usage.OutputTokens
	}
	(*meta).TotalTokens = (*meta).PromptTokens + (*meta).CompletionTokens
}
