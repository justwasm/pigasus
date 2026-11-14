// gateway_anthropic.go — the Anthropic Messages dialect of the gateway
// transport.
//
// The OpenAI-compatible dialect in gateway.go and this one share the HTTP
// plumbing (the connection-drop retry budget, the *HTTPError
// classification, the idle-timeout reader) and differ in two places: the
// request body, and the way the response stream is read.
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
	// Thinking and Signature are the extended-thinking block's content
	// and cryptographic token. Both must be preserved verbatim and
	// replayed on the next assistant turn; dropping either causes the
	// Anthropic API to reject the request with "Invalid signature in
	// thinking block".
	Thinking  string `json:"thinking,omitempty"`
	Signature string `json:"signature,omitempty"`
	// Source carries an image block's payload: the base64 image and its
	// media type.
	Source *anthropicImageSource `json:"source,omitempty"`
}

// anthropicImageSource is the source of an image block.
//
// 【一手】 the shape is the Anthropic Messages one, and it is the shape
// PiG's own Anthropic dialect writes (ai/anthropic.go): a base64
// source with the media type beside it.
type anthropicImageSource struct {
	Type      string `json:"type"`
	MediaType string `json:"media_type"`
	Data      string `json:"data"`
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
		Type      string          `json:"type"`
		ID        string          `json:"id"`
		Name      string          `json:"name"`
		Text      string          `json:"text"`
		Input     json.RawMessage `json:"input"`
		Signature string          `json:"signature"`
	} `json:"content_block"`
	Delta struct {
		Type        string `json:"type"`
		Text        string `json:"text"`
		Thinking    string `json:"thinking"`
		Signature   string `json:"signature"`
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

// anthropicThinkingBlock tracks one streaming thinking block: its accumulated
// reasoning text and cryptographic signature. The Anthropic SSE stream delivers
// the thinking text via "thinking_delta" events and the signature via a
// "signature_delta" event, both under the same index.
type anthropicThinkingBlock struct {
	thinking  strings.Builder
	signature string
	index     int
}

type anthropicThinkingTracker struct {
	blocks map[int]*anthropicThinkingBlock
	order  []int
}

func newAnthropicThinkingTracker() *anthropicThinkingTracker {
	return &anthropicThinkingTracker{blocks: map[int]*anthropicThinkingBlock{}}
}

// assembled returns the fully-formed thinking blocks in index order, discarding
// any block that has no signature (a block without a signature cannot be
// replayed and would cause a 400 on the next turn).
func (t *anthropicThinkingTracker) assembled() []backend.ThinkingBlock {
	if len(t.order) == 0 {
		return nil
	}
	var out []backend.ThinkingBlock
	for _, idx := range t.order {
		b := t.blocks[idx]
		if b == nil || b.signature == "" {
			continue
		}
		out = append(out, backend.ThinkingBlock{
			Thinking:  b.thinking.String(),
			Signature: b.signature,
		})
	}
	return out
}

func (t *anthropicToolTracker) emit(ctx context.Context, out chan<- backend.StreamChunk) bool {
	if len(t.order) == 0 {
		return true
	}
	calls := make([]backend.ToolCall, 0, len(t.order))
	for _, idx := range t.order {
		b := t.blocks[idx]
		if b == nil || strings.TrimSpace(b.name) == "" {
			continue
		}
		args := b.args.String()
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		calls = append(calls, backend.ToolCall{
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
		out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
		return false
	case out <- backend.StreamChunk{ToolCalls: calls}:
		return true
	}
}

// buildAnthropicRequest renders the Messages body.
func (c *GatewayClient) buildAnthropicRequest(req backend.StreamRequest, externalModel string) ([]byte, error) {
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

func formatAnthropicMessages(messages []backend.Message, system *string) []anthropicMessage {
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

func formatAnthropicMessage(m backend.Message) (anthropicMessage, bool) {
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
		blocks := []anthropicContent{{Type: "text", Text: m.WireText()}}
		for _, img := range m.Images {
			if len(img.Data) == 0 {
				continue
			}
			blocks = append(blocks, anthropicContent{
				Type: "image",
				Source: &anthropicImageSource{
					Type:      "base64",
					MediaType: img.MIMEType,
					Data:      base64.StdEncoding.EncodeToString(img.Data),
				},
			})
		}
		return anthropicMessage{
			Role:    "user",
			Content: blocks,
		}, true
	}
}

func formatAnthropicAssistantBlocks(m backend.Message) []anthropicContent {
	var blocks []anthropicContent
	// Thinking blocks must appear before text and tool_use blocks; the
	// Anthropic API rejects a request that omits them or changes their order.
	for _, tb := range m.ThinkingBlocks {
		if tb.Signature == "" {
			continue
		}
		blocks = append(blocks, anthropicContent{
			Type:      "thinking",
			Thinking:  tb.Thinking,
			Signature: tb.Signature,
		})
	}
	if text := strings.TrimSpace(m.Content); text != "" {
		blocks = append(blocks, anthropicContent{Type: "text", Text: m.Content})
	}
	for _, tc := range m.ToolCalls {
		// Canonical arguments, for the same reason the OpenAI dialect uses
		// them: a duplicated key from a streamed call is rejected upstream.
		args := geminicode.CanonicalArgs(tc.Arguments)
		blocks = append(blocks, anthropicContent{
			Type:  "tool_use",
			ID:    tc.ID,
			Name:  tc.Name,
			Input: args,
		})
	}
	return blocks
}

func formatAnthropicTools(tools []backend.ToolDecl) []anthropicTool {
	var out []anthropicTool
	for _, t := range tools {
		if strings.TrimSpace(t.Name) == "" {
			continue
		}
		schema := t.Parameters
		if len(strings.TrimSpace(string(schema))) == 0 {
			schema = json.RawMessage(`{"type":"object","properties":{}}`)
		} else if norm := geminicode.NormalizeToolSchema(schema, true); norm != nil {
			if b, err := json.Marshal(norm); err == nil {
				schema = b
			}
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
func (c *GatewayClient) readAnthropicBody(ctx context.Context, resp *http.Response, externalModel string, out chan<- backend.StreamChunk) error {
	var meta *backend.ChunkMeta
	tracker := newAnthropicToolTracker()
	thinkTracker := newAnthropicThinkingTracker()

	done, scanErr := streampipe.ScanSSELines(ctx, resp.Body, 1<<20, c.baseURL, func(line string) (bool, error) {
		return c.handleAnthropicSSELine(ctx, line, tracker, thinkTracker, &meta, externalModel, out)
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
	out <- backend.StreamChunk{Done: true, Meta: meta, ThinkingBlocks: thinkTracker.assembled()}
	return nil
}

func (c *GatewayClient) handleAnthropicSSELine(ctx context.Context, line string, tracker *anthropicToolTracker, thinkTracker *anthropicThinkingTracker, meta **backend.ChunkMeta, externalModel string, out chan<- backend.StreamChunk) (bool, error) {
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
			out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
		default:
			out <- backend.StreamChunk{Error: fmt.Errorf("gateway: anthropic sse decode: %w", err), Done: true}
		}
		return true, fmt.Errorf("gateway: anthropic sse decode: %w", err)
	}
	return c.dispatchAnthropicEvent(ctx, ev, payload, tracker, thinkTracker, meta, externalModel, out)
}

func (c *GatewayClient) dispatchAnthropicEvent(ctx context.Context, ev anthropicEvent, payload string, tracker *anthropicToolTracker, thinkTracker *anthropicThinkingTracker, meta **backend.ChunkMeta, externalModel string, out chan<- backend.StreamChunk) (bool, error) {
	switch ev.Type {
	case "message_start":
		*meta = &backend.ChunkMeta{
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
		} else if ev.ContentBlock.Type == "thinking" {
			if _, seen := thinkTracker.blocks[ev.Index]; !seen {
				thinkTracker.order = append(thinkTracker.order, ev.Index)
				thinkTracker.blocks[ev.Index] = &anthropicThinkingBlock{index: ev.Index}
			}
		}
	case "content_block_delta":
		return false, handleAnthropicDelta(ctx, ev, tracker.blocks, thinkTracker.blocks, out)
	case "message_delta":
		updateAnthropicMeta(meta, ev, c.baseURL, externalModel)
	case "message_stop":
		if !tracker.emit(ctx, out) {
			return true, nil
		}
		out <- backend.StreamChunk{Done: true, Meta: *meta, ThinkingBlocks: thinkTracker.assembled()}
		return true, nil
	case "error":
		err := fmt.Errorf("gateway: anthropic stream error: %s", payload)
		out <- backend.StreamChunk{Error: err, Done: true}
		return true, err
	}
	return false, nil
}

func handleAnthropicDelta(ctx context.Context, ev anthropicEvent, blocks map[int]*anthropicToolBlock, thinkBlocks map[int]*anthropicThinkingBlock, out chan<- backend.StreamChunk) error {
	b, isTool := blocks[ev.Index]
	switch ev.Delta.Type {
	case "text_delta":
		if ev.Delta.Text == "" {
			return nil
		}
		select {
		case <-ctx.Done():
			out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
			return ctx.Err()
		case out <- backend.StreamChunk{Text: ev.Delta.Text}:
			return nil
		}
	case "thinking_delta":
		// Accumulate the thinking text for multi-turn replay; do not
		// surface it as visible text content.
		if tb := thinkBlocks[ev.Index]; tb != nil {
			tb.thinking.WriteString(ev.Delta.Thinking)
		}
		return nil
	case "signature_delta":
		// Capture the cryptographic signature emitted after the thinking
		// text. It must be replayed verbatim on the next assistant turn.
		if tb := thinkBlocks[ev.Index]; tb != nil {
			tb.signature = ev.Delta.Signature
		}
		return nil
	case "input_json_delta":
		if isTool {
			b.args.WriteString(ev.Delta.PartialJSON)
		}
	}
	return nil
}

func updateAnthropicMeta(meta **backend.ChunkMeta, ev anthropicEvent, baseURL, externalModel string) {
	if *meta == nil {
		*meta = &backend.ChunkMeta{SystemFingerprint: GatewaySystemFingerprint(baseURL, externalModel)}
	}
	if ev.Usage.InputTokens > 0 {
		(*meta).PromptTokens = ev.Usage.InputTokens
	}
	if ev.Usage.OutputTokens > 0 {
		(*meta).CompletionTokens = ev.Usage.OutputTokens
	}
	(*meta).TotalTokens = (*meta).PromptTokens + (*meta).CompletionTokens
}
