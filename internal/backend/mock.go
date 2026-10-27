// Package backend — mock.go: a sandbox-safe stand-in for the real
// cloudaicompanion gRPC backend. The recovered 1.3.3 build's real
// backend (CodeAssistClient.StreamGenerateContentFromAICode) is wired
// through grpc/api_server.go and reaches
// https://cloudcode-pa.googleapis.com via Google's internal mTLS
// trust, which is not available in the dev sandbox.
//
// This file provides:
//
//   - MockCodeAssistClient: implements the CodeAssistClient interface
//     but emits synthetic StreamChunk values from a deterministic
//     prompt -> response mapping (no network calls).
//   - MockManager: a tiny state machine that wraps MockCodeAssistClient
//     and exposes the same Start / HandleUserInput / PollStatus surface
//     the recovered orchestrator (cmd/wagyu/tui.go) drives.
//
// The mock is deliberately stateless from the orchestrator's
// perspective — every call returns immediately and produces a finite
// stream of chunks that mirror what the real LLM would emit
// ("hello\n" -> echo + tool list). It exists so the TUI can be
// exercised end-to-end without a real backend in the loop.
package backend

import (
	"context"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/justwasm/pigasus/grpc"
)

// MockCodeAssistClient is a sandbox-safe implementation of the
// grpc.CodeAssistClient interface. Every StreamGenerateContent call
// returns a finite channel of StreamChunk values derived from the
// prompt — never the real cloudaicompanion gRPC endpoint.
type MockCodeAssistClient struct {
	mu       sync.Mutex
	endpoint string
	tier     grpc.Tier
	closed   bool
}

// NewMockCodeAssistClient constructs a mock client with sane defaults.
func NewMockCodeAssistClient() *MockCodeAssistClient {
	return &MockCodeAssistClient{
		endpoint: "mock://localhost",
		tier:     grpc.TierFree,
	}
}

// SetTier overrides the tier returned by GetUserTier (test hook).
func (m *MockCodeAssistClient) SetTier(t grpc.Tier) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.tier = t
}

// EndpointURL implements grpc.CodeAssistClient.
func (m *MockCodeAssistClient) EndpointURL() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.endpoint
}

// SetEndpointURL implements grpc.CodeAssistClient.
func (m *MockCodeAssistClient) SetEndpointURL(s string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.endpoint = s
}

// FetchLicensesFromAICode returns a fixed list. No network call.
func (m *MockCodeAssistClient) FetchLicensesFromAICode(_ context.Context) ([]string, error) {
	return []string{"free-tier", "standard-tier", "enterprise-tier"}, nil
}

// SelfAssignLicense is a no-op in the mock.
func (m *MockCodeAssistClient) SelfAssignLicense(_ context.Context, _ string) error {
	return nil
}

// StreamGenerateContentFromAICode emits a deterministic synthetic
// stream of chunks derived from prompt. We never reach the real
// backend. Each chunk is a short piece of text; the final chunk has
// Done=true.
//
// The stream layout (per prompt):
//   1. Acknowledge: "Echo: <prompt>\n"
//   2. List a tiny tool plan: "Tool: read_file\n" / "Tool: list_dir\n"
//   3. Final response: "Mock response to <prompt>: ...\n"
//
// Tests / callers can drain the channel and assert on the produced
// strings without any timing dependency.
func (m *MockCodeAssistClient) StreamGenerateContentFromAICode(ctx context.Context, model, prompt string) (<-chan grpc.StreamChunk, error) {
	if m.closed {
		return nil, errors.New("mock: client closed")
	}
	if model == "" {
		return nil, errors.New("mock: model is empty")
	}
	if prompt == "" {
		return nil, errors.New("mock: prompt is empty")
	}
	out := make(chan grpc.StreamChunk, 8)
	go streamMockContent(ctx, model, prompt, out)
	return out, nil
}

func streamMockContent(ctx context.Context, model, prompt string, out chan<- grpc.StreamChunk) {
	defer close(out)
	select {
	case <-ctx.Done():
		return
	case out <- grpc.StreamChunk{Text: "Echo: " + prompt + "\n", ModelVersion: model}:
	}
	for _, toolName := range []string{"read_file", "list_dir"} {
		select {
		case <-ctx.Done():
			return
		case out <- grpc.StreamChunk{Text: "Tool: " + toolName + "\n", ModelVersion: model}:
		}
		time.Sleep(5 * time.Millisecond)
	}
	response := buildMockResponse(prompt)
	for _, chunk := range splitForStream(response, 8) {
		select {
		case <-ctx.Done():
			return
		case out <- grpc.StreamChunk{Text: chunk, ModelVersion: model}:
		}
		time.Sleep(5 * time.Millisecond)
	}
	out <- grpc.StreamChunk{Done: true, ModelVersion: model}
}

// FetchConfigFromAICode returns a fixed per-user config.
func (m *MockCodeAssistClient) FetchConfigFromAICode(_ context.Context) (*grpc.UserConfig, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	return &grpc.UserConfig{
		CAICProject: "mock-project",
		GCPLocation: "mock-location",
		EndpointURL: m.endpoint,
		UserAgent:   "openagy/1.3.3-mock",
		HTTPHeaders: map[string]string{
			"X-Mock-Backend": "true",
		},
	}, nil
}

// Close releases the mock (no-op).
func (m *MockCodeAssistClient) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

// buildMockResponse returns a deterministic string for the given
// prompt. Recognised prompts:
//   - "hello" / "hi"            -> greeting
//   - anything starting with /  -> mock slash command response
//   - anything else              -> echo + plan summary
func buildMockResponse(prompt string) string {
	p := strings.TrimSpace(prompt)
	switch strings.ToLower(p) {
	case "hello", "hi":
		return "Hi from the mock backend! I have read your message.\n"
	}
	if strings.HasPrefix(p, "/") {
		return "Mock slash command " + strings.SplitN(p, " ", 2)[0] + " executed.\n"
	}
	return "Mock response to: " + prompt + "\n"
}

// splitForStream splits s into <= n pieces for streaming display.
func splitForStream(s string, n int) []string {
	if n <= 1 || len(s) <= n {
		return []string{s}
	}
	size := (len(s) + n - 1) / n
	if size < 4 {
		size = 4
	}
	var out []string
	for i := 0; i < len(s); i += size {
		end := i + size
		if end > len(s) {
			end = len(s)
		}
		out = append(out, s[i:end])
	}
	return out
}

// ---------------------------------------------------------------------------
// MockManager
// ---------------------------------------------------------------------------

// MockManager is a sandbox-safe orchestrator-facing facade. It wraps a
// MockCodeAssistClient and exposes the small surface the TUI
// orchestrator (cmd/wagyu/tui.go) needs:
//
//   - Start(): a synchronous ready() that returns nil immediately.
//   - HandleUserInput(input, onChunk, onToolStart, onToolDone):
//     kicks off a goroutine that drains the mock stream and calls
//     onChunk / onToolStart / onToolDone as each chunk arrives.
//   - PollStatus(): returns a stable snapshot suitable for the
//     StatusPanel periodic update.
//
// All callbacks are invoked on a single goroutine per
// HandleUserInput call; callers that need to push messages back into
// the Bubbletea runtime must do so via tea.Program.Send from inside
// the callback (the orchestrator wires this in cmd/wagyu/tui.go).
type MockManager struct {
	mu     sync.Mutex
	client *MockCodeAssistClient
	closed bool
}

// NewMockManager returns a fresh MockManager.
func NewMockManager() *MockManager {
	return &MockManager{client: NewMockCodeAssistClient()}
}

// Client returns the underlying mock client (test hook).
func (m *MockManager) Client() grpc.CodeAssistClient { return m.client }

// Start is a no-op; the mock is always ready.
func (m *MockManager) Start(_ context.Context) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.closed {
		return errors.New("mock: manager closed")
	}
	return nil
}

// Close releases the underlying mock client.
func (m *MockManager) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return m.client.Close()
}

// Status is a snapshot of the mock's externally-visible state.
type Status struct {
	AuthMode      string
	UserTier      string
	Entitlement   string
	Endpoint      string
	Eligible      bool
	ConnectedMTLS bool
}

// PollStatus returns a stable status snapshot. The values mirror the
// "happy path" the orchestrator should display:
//
//   - AuthMode       "mock"
//   - UserTier       "free"
//   - Entitlement    "free_user"
//   - Endpoint       "mock://localhost"
//   - Eligible       true
//   - ConnectedMTLS  true
func (m *MockManager) PollStatus() Status {
	return Status{
		AuthMode:      "mock",
		UserTier:      "free",
		Entitlement:   "free_user",
		Endpoint:      "mock://localhost",
		Eligible:      true,
		ConnectedMTLS: true,
	}
}

// HandleUserInput starts a goroutine that drains the mock stream and
// invokes the supplied callbacks for each chunk / tool event.
//
// Callbacks:
//
//   - onChunk(text string): called for every non-empty text chunk.
//   - onToolStart(name string): called when a tool is "invoked" by the
//     mock (currently read_file + list_dir).
//   - onToolDone(name, result string, tokens int): called once the
//     tool "finishes"; tokens is a synthetic count derived from the
//     prompt length.
//
// The function returns immediately; all callbacks fire on the
// goroutine it spawns. Tests should drain the returned wait channel
// before asserting on final state.
func (m *MockManager) HandleUserInput(
	ctx context.Context,
	model, input string,
	onChunk func(string),
	onToolStart func(string),
	onToolDone func(name, result string, tokens int),
) (wait func()) {
	done := make(chan struct{})
	go func() {
		defer close(done)
		stream, err := m.client.StreamGenerateContentFromAICode(ctx, model, input)
		if err != nil {
			return
		}
		toolTokens := map[string]int{}
		for chunk := range stream {
			if chunk.Error != nil {
				continue
			}
			text := chunk.Text
			if text == "" && !chunk.Done {
				continue
			}
			if strings.HasPrefix(text, "Tool: ") {
				name := strings.TrimSpace(strings.TrimPrefix(text, "Tool: "))
				if onToolStart != nil {
					onToolStart(name)
				}
				toolTokens[name] = len(name) * 7 // synthetic token count
				if onToolDone != nil {
					// Tools "complete" in the mock immediately.
					onToolDone(name, "mock-result-"+name, toolTokens[name])
				}
				continue
			}
			if text != "" && onChunk != nil {
				onChunk(text)
			}
		}
	}()
	return func() { <-done }
}
