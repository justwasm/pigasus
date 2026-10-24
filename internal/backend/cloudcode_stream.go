package backend

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
)

type ccFrame struct {
	Response json.RawMessage `json:"response"`
}

type ccCandidate struct {
	Content struct {
		Role  string   `json:"role"`
		Parts []ccPart `json:"parts"`
	} `json:"content"`
	FinishReason string `json:"finishReason"`
}

type ccResponse struct {
	Candidates []ccCandidate `json:"candidates"`
	UsageMetadata struct {
		PromptTokenCount     int `json:"promptTokenCount"`
		CandidatesTokenCount int `json:"candidatesTokenCount"`
		TotalTokenCount      int `json:"totalTokenCount"`
		ThoughtsTokenCount   int `json:"thoughtsTokenCount"`
	} `json:"usageMetadata"`
	ModelVersion string `json:"modelVersion"`
}

func (c *CloudCodeClient) openWithRetry(ctx context.Context, body []byte, token string) (*http.Response, error) {
	for attempt := 0; attempt <= MaxConnectionRetries; attempt++ {
		resp, err := c.open(ctx, body, token)
		if err == nil {
			return resp, nil
		}
		if !IsConnectionDropped(err) || attempt >= MaxConnectionRetries {
			return nil, err
		}
		delay := NextConnectionRetryDelay(attempt)
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	return nil, &ConnectionDroppedError{Err: ErrConnectionDropped, URL: c.cfg.Endpoint}
}

func (c *CloudCodeClient) runCloudCodeStreamBodyWithRetry(ctx context.Context, resp *http.Response, body []byte, token string, out chan<- StreamChunk) {
	defer func() {
		if resp != nil {
			_ = resp.Body.Close()
		}
	}()
	for attempt := 0; ; attempt++ {
		err := c.readBodyOnce(ctx, resp.Body, out)
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
		freshToken := token
		if t, terr := c.token(ctx); terr == nil && t != "" {
			freshToken = t
		}
		newResp, openErr := c.open(ctx, body, freshToken)
		if openErr != nil {
			if !IsConnectionDropped(openErr) {
				out <- StreamChunk{Error: openErr, Done: true}
				return
			}
			resp = nil
			continue
		}
		resp = newResp
	}
}

func (c *CloudCodeClient) open(ctx context.Context, body []byte, token string) (*http.Response, error) {
	url := strings.TrimRight(c.cfg.Endpoint, "/") + "/v1internal:streamGenerateContent?alt=sse"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("cloudcode: build request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", CloudCodeUserAgent())
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, ClassifyTransportError(err, url)
	}
	if resp.StatusCode == http.StatusUnauthorized {
		// One reactive retry: the token may have expired between the cache
		// read and the request. The unified cache refreshes inside Get.
		_ = resp.Body.Close()
		if fresh, terr := c.token(ctx); terr == nil && fresh != token {
			req2, rerr := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(body))
			if rerr != nil {
				return nil, fmt.Errorf("cloudcode: build request: %w", rerr)
			}
			req2.Header = req.Header.Clone()
			req2.Header.Set("Authorization", "Bearer "+fresh)
			resp, err = c.http.Do(req2)
			if err != nil {
				return nil, ClassifyTransportError(err, url)
			}
		}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		defer resp.Body.Close()
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		return nil, NewHTTPError(resp.StatusCode, raw, url)
	}
	return resp, nil
}

type ccStreamState struct {
	client    *CloudCodeClient
	ctx       context.Context
	out       chan<- StreamChunk
	calls     []ToolCall
	seenCalls map[string]bool
	meta      ChunkMeta
	finished  bool
	sawFinish bool
	inThought bool
}

const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"
)

func (s *ccStreamState) closeThought() {
	if !s.inThought {
		return
	}
	s.inThought = false
	select {
	case <-s.ctx.Done():
	case s.out <- StreamChunk{Text: thinkClose}:
	}
}

func (s *ccStreamState) emit(chunk StreamChunk) bool {
	select {
	case <-s.ctx.Done():
		s.out <- StreamChunk{Error: s.ctx.Err(), Done: true}
		return false
	case s.out <- chunk:
		return true
	}
}

func (s *ccStreamState) final() {
	if s.finished {
		return
	}
	s.closeThought()
	s.finished = true
	chunk := StreamChunk{Done: true, ToolCalls: s.calls}
	if s.meta != (ChunkMeta{}) {
		chunk.Meta = &s.meta
	}
	select {
	case s.out <- chunk:
	case <-s.ctx.Done():
		s.out <- StreamChunk{Error: s.ctx.Err(), Done: true}
	}
}

func (s *ccStreamState) handleFunctionCall(part ccPart) {
	s.closeThought()
	call := ToolCall{Name: part.FunctionCall.Name, Arguments: part.FunctionCall.Args}
	call.ID = part.FunctionCall.ID
	if call.ID == "" {
		call.ID = fmt.Sprintf("call_%d", len(s.calls)+1)
	}
	if len(call.Arguments) == 0 || !json.Valid(call.Arguments) {
		call.Arguments = json.RawMessage(`{}`)
	}
	if s.seenCalls[call.ID] {
		return
	}
	s.seenCalls[call.ID] = true
	s.calls = append(s.calls, call)
	if part.ThoughtSignature != "" {
		s.client.mu.Lock()
		s.client.signatures[call.ID] = part.ThoughtSignature
		s.client.mu.Unlock()
	}
}

func (s *ccStreamState) handleThought(part ccPart) bool {
	if part.Text == "" {
		return true
	}
	if !s.inThought {
		s.inThought = true
		if !s.emit(StreamChunk{Text: thinkOpen}) {
			return false
		}
	}
	return s.emit(StreamChunk{Text: part.Text})
}

func (s *ccStreamState) handlePart(part ccPart) bool {
	if part.FunctionCall != nil {
		s.handleFunctionCall(part)
		return true
	}
	if part.Thought {
		return s.handleThought(part)
	}
	if part.Text == "" {
		return true
	}
	s.closeThought()
	return s.emit(StreamChunk{Text: part.Text})
}

func (s *ccStreamState) processCandidates(candidates []ccCandidate) bool {
	for _, cand := range candidates {
		for _, part := range cand.Content.Parts {
			if !s.handlePart(part) {
				return false
			}
		}
		if cand.FinishReason != "" {
			s.sawFinish = true
		}
	}
	return true
}

func (s *ccStreamState) processResponse(resp ccResponse) (done bool, cont bool) {
	if !s.processCandidates(resp.Candidates) {
		return false, false
	}
	if u := resp.UsageMetadata; u.TotalTokenCount > 0 {
		s.meta = ChunkMeta{
			PromptTokens:     u.PromptTokenCount,
			CompletionTokens: u.CandidatesTokenCount,
			TotalTokens:      u.TotalTokenCount,
			ReasoningTokens:  u.ThoughtsTokenCount,
		}
	}
	if s.sawFinish {
		s.final()
		return true, false
	}
	return false, true
}

func (c *CloudCodeClient) readBodyOnce(ctx context.Context, body io.ReadCloser, out chan<- StreamChunk) error {
	idleR := NewIdleTimeoutReader(body, StreamIdleTimeout)
	scanner := bufio.NewScanner(idleR)
	scanner.Buffer(make([]byte, 0, 64*1024), 4<<20)

	s := &ccStreamState{
		client:    c,
		ctx:       ctx,
		out:       out,
		seenCalls: make(map[string]bool),
	}

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ")]}'") {
			continue
		}
		payload := line
		if strings.HasPrefix(payload, "data:") {
			payload = strings.TrimSpace(strings.TrimPrefix(payload, "data:"))
		}
		if payload == "" {
			continue
		}
		if payload == "[DONE]" {
			s.final()
			return nil
		}
		resp, ok := parseCloudCodePayload(payload)
		if !ok {
			continue
		}
		done, cont := s.processResponse(resp)
		if done {
			return nil
		}
		if !cont {
			return nil
		}
	}
	return s.finishScan(scanner.Err())
}

func (s *ccStreamState) finishScan(scanErr error) error {
	if scanErr != nil {
		s.closeThought()
		classified := ClassifyTransportError(scanErr, s.client.cfg.Endpoint)
		if IsConnectionDropped(classified) {
			return classified
		}
		select {
		case s.out <- StreamChunk{Error: classified, Done: true}:
		case <-s.ctx.Done():
			s.out <- StreamChunk{Error: s.ctx.Err(), Done: true}
		}
		return classified
	}
	if !s.sawFinish && len(s.calls) == 0 {
		s.closeThought()
		return &ConnectionDroppedError{Err: io.ErrUnexpectedEOF, URL: s.client.cfg.Endpoint}
	}
	s.final()
	return nil
}

// parseCloudCodePayload accepts both the wrapped frame the CloudCode endpoint
// sends and a bare inner response (some intermediaries strip the wrapper).
func parseCloudCodePayload(payload string) (ccResponse, bool) {
	var frame ccFrame
	if err := json.Unmarshal([]byte(payload), &frame); err == nil && len(frame.Response) > 0 {
		var out ccResponse
		if err := json.Unmarshal(frame.Response, &out); err == nil {
			return out, true
		}
	}
	var out ccResponse
	if err := json.Unmarshal([]byte(payload), &out); err == nil && (len(out.Candidates) > 0 || out.UsageMetadata.TotalTokenCount > 0) {
		return out, true
	}
	return ccResponse{}, false
}
