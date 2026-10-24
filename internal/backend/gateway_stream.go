// Package backend — gateway_stream.go: HTTP streaming and SSE chunk
// consumption for the gateway transport.
package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

// gatewaySSEChunk is the subset of the gateway's chunk shape that we consume.
type gatewaySSEChunk struct {
	Choices []struct {
		Delta        gatewayDelta `json:"delta"`
		FinishReason string       `json:"finish_reason"`
	} `json:"choices"`
	Usage *struct {
		PromptTokens            int `json:"prompt_tokens"`
		CompletionTokens        int `json:"completion_tokens"`
		TotalTokens             int `json:"total_tokens"`
		CompletionTokensDetails struct {
			ReasoningTokens int `json:"reasoning_tokens"`
		} `json:"completion_tokens_details"`
	} `json:"usage"`
}

type gatewayDelta struct {
	Content   string `json:"content"`
	ToolCalls []struct {
		Index    int    `json:"index"`
		ID       string `json:"id"`
		Type     string `json:"type"`
		Function struct {
			Name      string `json:"name"`
			Arguments string `json:"arguments"`
		} `json:"function"`
	} `json:"tool_calls"`
}

type gatewayToolAccumulator struct {
	order []int
	byIdx map[int]*gatewayToolCall
}

func newGatewayToolAccumulator() *gatewayToolAccumulator {
	return &gatewayToolAccumulator{byIdx: map[int]*gatewayToolCall{}}
}

func (a *gatewayToolAccumulator) add(index int, id, name, argsFragment string) {
	tc, ok := a.byIdx[index]
	if !ok {
		tc = &gatewayToolCall{}
		a.byIdx[index] = tc
		a.order = append(a.order, index)
	}
	if id != "" {
		tc.ID = id
	}
	if name != "" {
		tc.Function.Name = name
	}
	tc.Function.Arguments += argsFragment
}

func (a *gatewayToolAccumulator) empty() bool { return len(a.order) == 0 }

func (a *gatewayToolAccumulator) list() []ToolCall {
	out := make([]ToolCall, 0, len(a.order))
	for _, i := range a.order {
		tc := a.byIdx[i]
		if tc.Function.Name == "" {
			continue
		}
		args := tc.Function.Arguments
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		out = append(out, ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: json.RawMessage(args),
		})
	}
	return out
}

func (a *gatewayToolAccumulator) emit(ctx context.Context, out chan<- StreamChunk) bool {
	if a.empty() {
		return true
	}
	select {
	case <-ctx.Done():
		out <- StreamChunk{Error: ctx.Err(), Done: true}
		return false
	case out <- StreamChunk{ToolCalls: a.list()}:
		return true
	}
}

// openGatewayWithRetry performs one synchronous HTTP open and retries on
// connection drops only.
func (c *GatewayClient) openGatewayWithRetry(ctx context.Context, raw []byte) (*http.Response, error) {
	var lastErr error
	for attempt := 0; attempt <= MaxConnectionRetries; attempt++ {
		if attempt > 0 {
			delay := NextConnectionRetryDelay(attempt - 1)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(delay):
			}
		}
		resp, err := c.openGatewayOnce(ctx, raw)
		if err == nil {
			return resp, nil
		}
		if !IsConnectionDropped(err) {
			return nil, err
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = &ConnectionDroppedError{URL: c.baseURL, Err: errors.New("max retries exceeded")}
	}
	return nil, lastErr
}

// openGatewayOnce performs one HTTP attempt.
func (c *GatewayClient) openGatewayOnce(ctx context.Context, raw []byte) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")
	c.applyProtocolHeaders(httpReq)
	httpReq.Header.Set("User-Agent", GatewayUserAgent)
	for name, value := range c.cfg.Headers {
		httpReq.Header.Set(name, value)
	}

	resp, err := c.httpDo(httpReq)
	if err != nil {
		return nil, ClassifyTransportError(err, c.baseURL)
	}
	if resp.StatusCode >= 400 {
		rawBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &HTTPError{StatusCode: resp.StatusCode, Body: rawBody, URL: c.baseURL}
	}
	return resp, nil
}

// runGatewayStreamBodyWithRetry owns the async body read and retry loop.
func (c *GatewayClient) runGatewayStreamBodyWithRetry(ctx context.Context, resp *http.Response, raw []byte, externalModel string, out chan<- StreamChunk) {
	defer func() {
		if resp != nil {
			_ = resp.Body.Close()
		}
	}()
	for attempt := 0; ; attempt++ {
		err := c.readGatewayBodyOnce(ctx, resp, externalModel, out)
		if err == nil || !IsConnectionDropped(err) {
			return
		}
		if attempt >= MaxConnectionRetries {
			out <- StreamChunk{Error: err, Done: true}
			return
		}
		_ = resp.Body.Close()
		delay := NextConnectionRetryDelay(attempt)
		select {
		case <-ctx.Done():
			out <- StreamChunk{Error: ctx.Err(), Done: true}
			return
		case <-time.After(delay):
		}
		newResp, openErr := c.openGatewayOnce(ctx, raw)
		if openErr != nil {
			out <- StreamChunk{Error: openErr, Done: true}
			return
		}
		resp = newResp
	}
}

// readGatewayBodyOnce reads one response body to completion.
func (c *GatewayClient) readGatewayBodyOnce(ctx context.Context, resp *http.Response, externalModel string, out chan<- StreamChunk) error {
	if c.cfg.protocol() == GatewayProtocolAnthropic {
		return c.readAnthropicBody(ctx, resp, externalModel, out)
	}
	return c.readOpenAIBody(ctx, resp, externalModel, out)
}

// readOpenAIBody reads one Chat Completions response body to completion.
func (c *GatewayClient) readOpenAIBody(ctx context.Context, resp *http.Response, externalModel string, out chan<- StreamChunk) error {
	idleR := NewIdleTimeoutReader(resp.Body, StreamIdleTimeout)
	scanner := bufio.NewScanner(idleR)
	scanner.Buffer(make([]byte, 0, 64*1024), 1<<20)
	var meta *ChunkMeta
	acc := newGatewayToolAccumulator()

	for scanner.Scan() {
		line := scanner.Text()
		done, err := c.handleOpenAISSELine(ctx, line, acc, &meta, externalModel, out)
		if err != nil {
			return err
		}
		if done {
			return nil
		}
	}
	if err := scanner.Err(); err != nil {
		classified := ClassifyTransportError(fmt.Errorf("gateway: sse read: %w", err), c.baseURL)
		if !IsConnectionDropped(classified) {
			out <- StreamChunk{Error: classified, Done: true}
		}
		return classified
	}
	if !acc.emit(ctx, out) {
		return nil
	}
	out <- StreamChunk{Done: true, Meta: meta}
	return nil
}

func (c *GatewayClient) handleOpenAISSELine(ctx context.Context, line string, acc *gatewayToolAccumulator, meta **ChunkMeta, externalModel string, out chan<- StreamChunk) (bool, error) {
	if !strings.HasPrefix(line, "data:") {
		return false, nil
	}
	payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
	if payload == "" {
		return false, nil
	}
	if payload == "[DONE]" {
		if !acc.emit(ctx, out) {
			return true, nil
		}
		out <- StreamChunk{Done: true, Meta: *meta}
		return true, nil
	}
	var chunk gatewaySSEChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		select {
		case <-ctx.Done():
			out <- StreamChunk{Error: ctx.Err(), Done: true}
		default:
			out <- StreamChunk{Error: fmt.Errorf("gateway: sse decode: %w", err), Done: true}
		}
		return true, fmt.Errorf("gateway: sse decode: %w", err)
	}
	if chunk.Usage != nil {
		*meta = &ChunkMeta{
			PromptTokens:      chunk.Usage.PromptTokens,
			CompletionTokens:  chunk.Usage.CompletionTokens,
			TotalTokens:       chunk.Usage.TotalTokens,
			ReasoningTokens:   chunk.Usage.CompletionTokensDetails.ReasoningTokens,
			SystemFingerprint: GatewaySystemFingerprint(c.baseURL, externalModel),
		}
	}
	return false, processOpenAIChoices(ctx, chunk, acc, out)
}

func processOpenAIChoices(ctx context.Context, chunk gatewaySSEChunk, acc *gatewayToolAccumulator, out chan<- StreamChunk) error {
	for _, choice := range chunk.Choices {
		for _, tc := range choice.Delta.ToolCalls {
			acc.add(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments)
		}
		if choice.Delta.Content == "" {
			continue
		}
		select {
		case <-ctx.Done():
			out <- StreamChunk{Error: ctx.Err(), Done: true}
			return ctx.Err()
		case out <- StreamChunk{Text: choice.Delta.Content}:
		}
	}
	return nil
}
