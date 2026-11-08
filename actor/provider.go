package actor

import (
	"context"
	"io"
	"sync"
)

// Role specifies the author role of a message in a conversation.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
	RoleTool      Role = "tool"
)

// ConversationMessage is an individual message sent to the model backend.
type ConversationMessage struct {
	Role       Role       `json:"role"`
	Content    string     `json:"content"`
	Thought    string     `json:"thought,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	ToolName   string     `json:"tool_name,omitempty"`
}

// GenerateRequest bundles parameters for a model generation turn.
type GenerateRequest struct {
	Model        string                `json:"model"`
	SystemPrompt string                `json:"system_prompt,omitempty"`
	Messages     []ConversationMessage `json:"messages"`
	Tools        []ToolDefinition      `json:"tools,omitempty"`
	Temperature  *float32              `json:"temperature,omitempty"`
	MaxTokens    int                   `json:"max_tokens,omitempty"`
}

// StreamChunk is one incremental delta emitted by a ResponseStream.
type StreamChunk struct {
	Delta        string      `json:"delta,omitempty"`
	Thought      string      `json:"thought,omitempty"`
	ToolCalls    []ToolCall  `json:"tool_calls,omitempty"`
	Usage        *TokenUsage `json:"usage,omitempty"`
	FinishReason string      `json:"finish_reason,omitempty"`
}

// ResponseStream consumes incremental streaming responses from a Provider.
type ResponseStream interface {
	// Recv yields the next StreamChunk, or io.EOF when generation finishes.
	Recv() (StreamChunk, error)
	// Close releases any resources held by the stream.
	Close() error
}

// Provider represents an LLM inference service (Gemini, Claude, GPT, local, etc.).
type Provider interface {
	// Generate opens a streaming inference session with the given request.
	Generate(ctx context.Context, req GenerateRequest) (ResponseStream, error)
}

// MockProvider is an in-memory implementation of Provider for testing.
type MockProvider struct {
	mu             sync.Mutex
	Handlers       []func(ctx context.Context, req GenerateRequest) ([]StreamChunk, error)
	DefaultHandler func(ctx context.Context, req GenerateRequest) ([]StreamChunk, error)
	CallIndex      int
	Requests       []GenerateRequest
}

// NewMockProvider creates a new mock provider.
func NewMockProvider() *MockProvider {
	return &MockProvider{
		Handlers: make([]func(ctx context.Context, req GenerateRequest) ([]StreamChunk, error), 0),
		Requests: make([]GenerateRequest, 0),
	}
}

// SetDefaultHandler sets a fallback handler when the queue is exhausted.
func (m *MockProvider) SetDefaultHandler(h func(ctx context.Context, req GenerateRequest) ([]StreamChunk, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.DefaultHandler = h
}

// EnqueueResponse queues a simple text response stream chunk.
func (m *MockProvider) EnqueueResponse(text string) {
	m.EnqueueChunks([]StreamChunk{{Delta: text, FinishReason: "stop"}})
}

// EnqueueChunks queues a sequence of stream chunks to return on the next call.
func (m *MockProvider) EnqueueChunks(chunks []StreamChunk) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Handlers = append(m.Handlers, func(ctx context.Context, req GenerateRequest) ([]StreamChunk, error) {
		return chunks, nil
	})
}

// EnqueueHandler registers a dynamic handler for inspecting the request and returning chunks.
func (m *MockProvider) EnqueueHandler(h func(req GenerateRequest) ([]StreamChunk, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Handlers = append(m.Handlers, func(ctx context.Context, req GenerateRequest) ([]StreamChunk, error) {
		return h(req)
	})
}

// EnqueueContextHandler registers a handler that receives context for cancellation awareness.
func (m *MockProvider) EnqueueContextHandler(h func(ctx context.Context, req GenerateRequest) ([]StreamChunk, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.Handlers = append(m.Handlers, h)
}

// Generate implements Provider.
func (m *MockProvider) Generate(ctx context.Context, req GenerateRequest) (ResponseStream, error) {
	m.mu.Lock()
	m.Requests = append(m.Requests, req)

	var handler func(ctx context.Context, req GenerateRequest) ([]StreamChunk, error)
	if m.CallIndex < len(m.Handlers) {
		handler = m.Handlers[m.CallIndex]
		m.CallIndex++
	} else if m.DefaultHandler != nil {
		handler = m.DefaultHandler
	} else {
		handler = func(ctx context.Context, req GenerateRequest) ([]StreamChunk, error) {
			return []StreamChunk{{Delta: "OK", FinishReason: "stop"}}, nil
		}
	}
	m.mu.Unlock()

	chunks, err := handler(ctx, req)
	if err != nil {
		return nil, err
	}

	return &sliceStream{chunks: chunks, ctx: ctx}, nil
}

type sliceStream struct {
	chunks []StreamChunk
	idx    int
	ctx    context.Context
}

func (s *sliceStream) Recv() (StreamChunk, error) {
	select {
	case <-s.ctx.Done():
		return StreamChunk{}, s.ctx.Err()
	default:
	}

	if s.idx >= len(s.chunks) {
		return StreamChunk{}, io.EOF
	}
	c := s.chunks[s.idx]
	s.idx++
	return c, nil
}

func (s *sliceStream) Close() error {
	return nil
}
