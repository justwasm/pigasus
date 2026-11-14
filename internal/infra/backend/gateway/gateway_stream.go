// Package backend — gateway_stream.go: HTTP streaming and SSE chunk
// consumption for the gateway transport.
package gateway

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"

	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/core/apierror"
	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
	"github.com/justwasm/pigasus/internal/infra/backend/streampipe"
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

func (a *gatewayToolAccumulator) list() []backend.ToolCall {
	out := make([]backend.ToolCall, 0, len(a.order))
	for _, i := range a.order {
		tc := a.byIdx[i]
		if tc.Function.Name == "" {
			continue
		}
		args := tc.Function.Arguments
		if strings.TrimSpace(args) == "" {
			args = "{}"
		}
		// The loop above concatenates argument fragments; a provider that
		// re-sends a fragment leaves the object with a duplicated key, which
		// the next model's request would be rejected for. Canonicalise once,
		// where the call is completed.
		out = append(out, backend.ToolCall{
			ID:        tc.ID,
			Name:      tc.Function.Name,
			Arguments: geminicode.CanonicalArgs(json.RawMessage(args)),
		})
	}
	return out
}

func (a *gatewayToolAccumulator) emit(ctx context.Context, out chan<- backend.StreamChunk) bool {
	if a.empty() {
		return true
	}
	select {
	case <-ctx.Done():
		out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
		return false
	case out <- backend.StreamChunk{ToolCalls: a.list()}:
		return true
	}
}

// openGatewayWithRetry performs one synchronous HTTP open and retries on
// connection drops and retryable 429/5xx errors, honoring Retry-After.
func (c *GatewayClient) openGatewayWithRetry(ctx context.Context, raw []byte) (*http.Response, error) {
	return streampipe.ExecuteWithRetry(ctx, c.baseURL, func(ctx context.Context) (*http.Response, error) {
		return c.openGatewayOnce(ctx, raw)
	})
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
		return nil, retry.ClassifyTransportError(err, c.baseURL)
	}
	if resp.StatusCode >= 400 {
		rawBody, _ := io.ReadAll(resp.Body)
		resp.Body.Close()
		return nil, &apierror.HTTPError{StatusCode: resp.StatusCode, Body: rawBody, URL: c.baseURL, Header: resp.Header}
	}
	return resp, nil
}

// runGatewayStreamBodyWithRetry owns the async body read and retry loop.
func (c *GatewayClient) runGatewayStreamBodyWithRetry(ctx context.Context, resp *http.Response, raw []byte, externalModel string, out chan<- backend.StreamChunk) {
	p := streampipe.Pipeline[[]byte]{
		EndpointURL: c.baseURL,
		Open:        c.openGatewayOnce,
		ReadBody: func(ctx context.Context, r *http.Response, ch chan<- backend.StreamChunk) error {
			return c.readGatewayBodyOnce(ctx, r, externalModel, ch)
		},
	}
	p.Run(ctx, resp, raw, out)
}

// readGatewayBodyOnce reads one response body to completion.
func (c *GatewayClient) readGatewayBodyOnce(ctx context.Context, resp *http.Response, externalModel string, out chan<- backend.StreamChunk) error {
	if c.cfg.protocol() == GatewayProtocolAnthropic {
		return c.readAnthropicBody(ctx, resp, externalModel, out)
	}
	return c.readOpenAIBody(ctx, resp, externalModel, out)
}

// readOpenAIBody reads one Chat Completions response body to completion.
func (c *GatewayClient) readOpenAIBody(ctx context.Context, resp *http.Response, externalModel string, out chan<- backend.StreamChunk) error {
	var meta *backend.ChunkMeta
	acc := newGatewayToolAccumulator()

	done, scanErr := streampipe.ScanSSELines(ctx, resp.Body, 1<<20, c.baseURL, func(line string) (bool, error) {
		return c.handleOpenAISSELine(ctx, line, acc, &meta, externalModel, out)
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
	if !acc.emit(ctx, out) {
		return nil
	}
	out <- backend.StreamChunk{Done: true, Meta: meta}
	return nil
}

func (c *GatewayClient) handleOpenAISSELine(ctx context.Context, line string, acc *gatewayToolAccumulator, meta **backend.ChunkMeta, externalModel string, out chan<- backend.StreamChunk) (bool, error) {
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
		out <- backend.StreamChunk{Done: true, Meta: *meta}
		return true, nil
	}
	var chunk gatewaySSEChunk
	if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
		select {
		case <-ctx.Done():
			out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
		default:
			out <- backend.StreamChunk{Error: fmt.Errorf("gateway: sse decode: %w", err), Done: true}
		}
		return true, fmt.Errorf("gateway: sse decode: %w", err)
	}
	if chunk.Usage != nil {
		*meta = &backend.ChunkMeta{
			PromptTokens:      chunk.Usage.PromptTokens,
			CompletionTokens:  chunk.Usage.CompletionTokens,
			TotalTokens:       chunk.Usage.TotalTokens,
			ReasoningTokens:   chunk.Usage.CompletionTokensDetails.ReasoningTokens,
			SystemFingerprint: GatewaySystemFingerprint(c.baseURL, externalModel),
		}
	}
	return false, processOpenAIChoices(ctx, chunk, acc, out)
}

func processOpenAIChoices(ctx context.Context, chunk gatewaySSEChunk, acc *gatewayToolAccumulator, out chan<- backend.StreamChunk) error {
	for _, choice := range chunk.Choices {
		for _, tc := range choice.Delta.ToolCalls {
			acc.add(tc.Index, tc.ID, tc.Function.Name, tc.Function.Arguments)
		}
		if choice.Delta.Content == "" {
			continue
		}
		select {
		case <-ctx.Done():
			out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
			return ctx.Err()
		case out <- backend.StreamChunk{Text: choice.Delta.Content}:
		}
	}
	return nil
}
