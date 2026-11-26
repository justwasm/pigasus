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
	"github.com/justwasm/pigasus/internal/core/clilog"
	"github.com/justwasm/pigasus/internal/infra/backend/retry"
	"github.com/justwasm/pigasus/internal/infra/backend/streampipe"
)

// gatewaySSEChunk is the subset of the gateway's chunk shape that we consume.
type gatewaySSEChunk struct {
	Error   *sseErrorPayload `json:"error,omitempty"`
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
// connection drops and retryable 429/5xx errors, honoring Retry-After, and
// rotating across healthy keys in KeyPool on 401/403/429/quota errors.
func (c *GatewayClient) openGatewayWithRetry(ctx context.Context, raw []byte) (*http.Response, string, error) {
	if c.keyPool == nil {
		resp, err := streampipe.ExecuteWithRetry(ctx, c.baseURL, func(ctx context.Context) (*http.Response, error) {
			return c.openGatewayOnce(ctx, raw)
		})
		return resp, c.apiKey, err
	}

	maxRotations := c.keyPool.Len()
	if maxRotations < 1 {
		maxRotations = 1
	}
	var lastErr error
	for rot := 0; rot < maxRotations; rot++ {
		_, key := c.keyPool.CurrentKey()
		if key == "" {
			key = c.apiKey
		}
		resp, err := streampipe.ExecuteWithRetry(ctx, c.baseURL, func(ctx context.Context) (*http.Response, error) {
			return c.openGatewayOnceWithKey(ctx, raw, key)
		})
		if err == nil {
			c.keyPool.MarkSuccess(key)
			return resp, key, nil
		}
		lastErr = err
		if c.keyPool.MarkFailure(key, err) {
			clilog.Logf("gateway", "endpoint=%s key rotation=%d/%d err=%v", c.baseURL, rot+1, maxRotations, err)
			continue
		}
		clilog.Logf("gateway", "endpoint=%s open failed err=%v", c.baseURL, err)
		return nil, key, err
	}
	return nil, "", lastErr
}

// openGatewayOnce performs one HTTP attempt using the active API key.
func (c *GatewayClient) openGatewayOnce(ctx context.Context, raw []byte) (*http.Response, error) {
	return c.openGatewayOnceWithKey(ctx, raw, c.activeAPIKey())
}

func (c *GatewayClient) openGatewayOnceWithKey(ctx context.Context, raw []byte, key string) (*http.Response, error) {
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL, bytes.NewReader(raw))
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("content-type", "application/json")
	c.applyProtocolHeadersWithKey(httpReq, key)
	ua := GatewayUserAgent
	if c.cfg.protocol() == GatewayProtocolResponses {
		ua = "opencode/2.3.2"
	}
	httpReq.Header.Set("User-Agent", ua)
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
func (c *GatewayClient) runGatewayStreamBodyWithRetry(ctx context.Context, resp *http.Response, raw []byte, externalModel, usedKey string, out chan<- backend.StreamChunk) {
	activeKey := usedKey
	p := streampipe.Pipeline[[]byte]{
		EndpointURL: c.baseURL,
		Open: func(ctx context.Context, body []byte) (*http.Response, error) {
			if c.keyPool != nil {
				if _, k := c.keyPool.CurrentKey(); k != "" {
					activeKey = k
				}
			}
			return c.openGatewayOnceWithKey(ctx, body, activeKey)
		},
		ReadBody: func(ctx context.Context, r *http.Response, ch chan<- backend.StreamChunk) error {
			err := c.readGatewayBodyOnce(ctx, r, externalModel, ch)
			if c.keyPool != nil {
				if err == nil {
					c.keyPool.MarkSuccess(activeKey)
				} else {
					c.keyPool.MarkFailure(activeKey, err)
				}
			}
			return err
		},
	}
	p.Run(ctx, resp, raw, out)
}

// readGatewayBodyOnce reads one response body to completion.
//
// A nil resp means the mid-stream reopen produced no response. That is the
// condition the caller's retry budget exists to absorb, so it is reported as
// a connection drop rather than dereferenced. CloudCode's reader carries the
// same guard; without it every protocol branch below would fault on Body.
func (c *GatewayClient) readGatewayBodyOnce(ctx context.Context, resp *http.Response, externalModel string, out chan<- backend.StreamChunk) error {
	if resp == nil {
		return &retry.ConnectionDroppedError{
			URL: c.baseURL,
			Err: fmt.Errorf("gateway: reopen produced no response"),
		}
	}
	switch c.cfg.protocol() {
	case GatewayProtocolAnthropic:
		return c.readAnthropicBody(ctx, resp, externalModel, out)
	case GatewayProtocolResponses:
		return c.readResponsesBody(ctx, resp, externalModel, out)
	default:
		return c.readOpenAIBody(ctx, resp, externalModel, out)
	}
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
			out <- backend.StreamChunk{Error: scanErr, Done: true, Meta: meta}
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
		return true, fmt.Errorf("gateway: sse decode: %w", err)
	}
	if chunk.Error != nil {
		httpErr := sseErrorToHTTPError(
			chunk.Error.Type,
			formatSSEErrorCode(chunk.Error.Code),
			chunk.Error.Message,
			[]byte(payload),
			c.baseURL,
		)
		return true, httpErr
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
		if choice.Delta.Content != "" {
			select {
			case <-ctx.Done():
				out <- backend.StreamChunk{Error: ctx.Err(), Done: true}
				return ctx.Err()
			case out <- backend.StreamChunk{Text: choice.Delta.Content}:
			}
		}
		// Emit the last text delta, but never the accumulated calls, when
		// this same frame declares a truncated or filtered response.
		switch choice.FinishReason {
		case "length", "content_filter":
			return gatewayTerminationError(GatewayProtocolOpenAI, choice.FinishReason)
		}
	}
	return nil
}
