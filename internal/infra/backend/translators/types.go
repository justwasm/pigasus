package translators

import "encoding/json"

// TranslateOptions configures schema translation.
type TranslateOptions struct {
	// Strict indicates whether to enforce OpenAI Strict JSON Schema mode
	// (additionalProperties: false on all objects, all properties required with nullable types).
	Strict bool

	// Sandboxed indicates whether the execution environment is sandboxed.
	// When false, BypassSandbox parameter is stripped from run_command.
	Sandboxed bool
}

// Option modifies TranslateOptions.
type Option func(*TranslateOptions)

// WithStrict sets the strict schema mode.
func WithStrict(strict bool) Option {
	return func(o *TranslateOptions) {
		o.Strict = strict
	}
}

// WithSandbox sets the sandboxed environment flag.
func WithSandbox(sandboxed bool) Option {
	return func(o *TranslateOptions) {
		o.Sandboxed = sandboxed
	}
}

func defaultOptions() TranslateOptions {
	return TranslateOptions{
		Strict:    true,
		Sandboxed: true,
	}
}

func applyOptions(opts []Option) TranslateOptions {
	o := defaultOptions()
	for _, opt := range opts {
		if opt != nil {
			opt(&o)
		}
	}
	return o
}

// OpenAITool represents an OpenAI Chat Completions function tool.
type OpenAITool struct {
	Type     string         `json:"type"` // "function"
	Function OpenAIFunction `json:"function"`
}

// OpenAIFunction represents the function definition within an OpenAITool.
type OpenAIFunction struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Parameters  map[string]any `json:"parameters"`
	Strict      *bool          `json:"strict,omitempty"`
}

// OpenAIToolCall represents a tool call emitted by an OpenAI-compatible model.
type OpenAIToolCall struct {
	ID       string `json:"id"`
	Type     string `json:"type"` // "function"
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

// AnthropicTool represents a tool in Anthropic Messages API format.
type AnthropicTool struct {
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	InputSchema map[string]any `json:"input_schema"`
}

// AnthropicToolUse represents a tool use content block emitted by Anthropic.
type AnthropicToolUse struct {
	ID    string         `json:"id"`
	Type  string         `json:"type"` // "tool_use"
	Name  string         `json:"name"`
	Input map[string]any `json:"input"`
}

// AnthropicToolResult represents a tool result content block sent back to Anthropic.
type AnthropicToolResult struct {
	Type      string          `json:"type"` // "tool_result"
	ToolUseID string          `json:"tool_use_id"`
	Content   string          `json:"content"`
	IsError   bool            `json:"is_error,omitempty"`
	RawJSON   json.RawMessage `json:"-"`
}
