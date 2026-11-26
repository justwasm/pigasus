package gateway

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
)

// Exercise the public stream, not just parser state: partial text must survive,
// non-success termination must reach the caller, and buffered tool calls must
// never escape a truncated/filtered/refused response (even with valid JSON).
func TestGatewayNonSuccessTermination(t *testing.T) {
	const openAIPrefix = `data: {"choices":[{"delta":{"content":"partial text","tool_calls":[{"index":0,"id":"call_1","function":{"name":"test_tool","arguments":"{\"arg\":\"value\"}"}}]}}]}` + "\n\n"
	const responsesPrefix = `data: {"type":"response.output_text.delta","delta":"partial text"}` + "\n\n" +
		`data: {"type":"response.output_item.added","item":{"type":"function_call","id":"call_1","call_id":"call_1","name":"test_tool"}}` + "\n\n" +
		`data: {"type":"response.function_call_arguments.delta","item_id":"call_1","delta":"{\"arg\":\"value\"}"}` + "\n\n"
	const anthropicPrefix = `data: {"type":"message_start","message":{"usage":{"input_tokens":11,"output_tokens":1}}}` + "\n\n" +
		`data: {"type":"content_block_delta","index":0,"delta":{"type":"text_delta","text":"partial text"}}` + "\n\n" +
		`data: {"type":"content_block_start","index":1,"content_block":{"type":"tool_use","id":"call_1","name":"test_tool"}}` + "\n\n" +
		`data: {"type":"content_block_delta","index":1,"delta":{"type":"input_json_delta","partial_json":"{\"arg\":\"value\"}"}}` + "\n\n"

	type scenario struct {
		name, protocol, stream, reason string
		wantUsage                      bool
	}
	var scenarios []scenario
	for _, reason := range []string{"length", "content_filter"} {
		for _, ending := range []string{"done", "eof"} {
			stream := openAIPrefix + fmt.Sprintf(`data: {"choices":[{"delta":{},"finish_reason":%q}],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}`+"\n\n", reason)
			if ending == "done" {
				stream += "data: [DONE]\n\n"
			}
			scenarios = append(scenarios, scenario{"chat_" + reason + "_" + ending, GatewayProtocolOpenAI, stream, reason, true})
		}
	}
	for _, reason := range []string{"max_output_tokens", "content_filter"} {
		for _, event := range []string{"response.incomplete", "response.completed", "response.done"} {
			stream := responsesPrefix + fmt.Sprintf(`data: {"type":%q,"response":{"status":"incomplete","incomplete_details":{"reason":%q},"usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}}`+"\n\ndata: [DONE]\n\n", event, reason)
			scenarios = append(scenarios, scenario{event + "_" + reason, GatewayProtocolResponses, stream, reason, true})
		}
	}
	scenarios = append(scenarios, scenario{"responses_incomplete_without_details", GatewayProtocolResponses,
		responsesPrefix + "data: {\"type\":\"response.incomplete\"}\n\n", "incomplete", false})
	for _, reason := range []string{"max_tokens", "refusal"} {
		for _, ending := range []string{"message_stop", "eof"} {
			stream := anthropicPrefix + fmt.Sprintf(`data: {"type":"message_delta","delta":{"stop_reason":%q},"usage":{"output_tokens":7}}`+"\n\n", reason)
			if ending == "message_stop" {
				stream += "data: {\"type\":\"message_stop\"}\n\n"
			}
			scenarios = append(scenarios, scenario{"anthropic_" + reason + "_" + ending, GatewayProtocolAnthropic, stream, reason, true})
		}
	}

	// The safety rule must not depend on whether the partial JSON happens
	// to parse: neither complete-looking nor incomplete calls may execute.
	for _, tc := range append([]scenario(nil), scenarios...) {
		tc.name += "_partial_json"
		tc.stream = strings.ReplaceAll(tc.stream, `value\"}`, `value`)
		scenarios = append(scenarios, tc)
	}

	for _, tc := range scenarios {
		t.Run(tc.name, func(t *testing.T) {
			chunks := collectGatewayTerminationFixture(t, tc.protocol, tc.stream)
			var text strings.Builder
			var terminal *backend.StreamChunk
			errors, done := 0, 0
			for i := range chunks {
				chunk := &chunks[i]
				text.WriteString(chunk.Text)
				if len(chunk.ToolCalls) != 0 {
					t.Errorf("non-success stream leaked tool calls: %+v", chunk.ToolCalls)
				}
				if chunk.Done {
					done++
					terminal = chunk
				}
				if chunk.Error != nil {
					errors++
					if !chunk.Done || !strings.Contains(chunk.Error.Error(), tc.reason) {
						t.Errorf("terminal error = %+v, want Done and reason %q", chunk, tc.reason)
					}
				}
			}
			if text.String() != "partial text" {
				t.Errorf("text = %q, want partial text preserved", text.String())
			}
			if errors != 1 || done != 1 {
				t.Fatalf("errors=%d done=%d, want exactly one failed terminal chunk; chunks=%+v", errors, done, chunks)
			}
			if tc.wantUsage && (terminal.Meta == nil || terminal.Meta.PromptTokens != 11 || terminal.Meta.CompletionTokens != 7 || terminal.Meta.TotalTokens != 18) {
				t.Errorf("terminal usage = %+v, want 11 input / 7 output / 18 total", terminal.Meta)
			}
		})
	}
}

// Some compatible gateways attach the final text delta to the same frame as
// finish_reason. Reporting truncation must not hide those already received bytes.
func TestGatewayChatCompletionFinalTextOnFailure(t *testing.T) {
	for _, reason := range []string{"length", "content_filter"} {
		t.Run(reason, func(t *testing.T) {
			stream := fmt.Sprintf(`data: {"choices":[{"delta":{"content":"last text"},"finish_reason":%q}]}`+"\n\ndata: [DONE]\n\n", reason)
			chunks := collectGatewayTerminationFixture(t, GatewayProtocolOpenAI, stream)
			if len(chunks) != 2 || chunks[0].Text != "last text" || !chunks[1].Done || chunks[1].Error == nil || !strings.Contains(chunks[1].Error.Error(), reason) {
				t.Fatalf("chunks = %+v, want final text followed by the terminal error", chunks)
			}
		})
	}
}

func TestGatewaySuccessfulTerminationPreservesToolsAndUsage(t *testing.T) {
	scenarios := []struct{ name, protocol, stream string }{
		{"chat_tool_calls_usage_trailer", GatewayProtocolOpenAI,
			`data: {"choices":[{"delta":{"tool_calls":[{"index":0,"id":"call_1","function":{"name":"test_tool","arguments":"{\"arg\":\"value\"}"}}]},"finish_reason":"tool_calls"}]}` + "\n\n" +
				`data: {"choices":[],"usage":{"prompt_tokens":11,"completion_tokens":7,"total_tokens":18}}` + "\n\ndata: [DONE]\n\n"},
		{"responses_completed", GatewayProtocolResponses,
			`data: {"type":"response.output_item.done","item":{"type":"function_call","id":"call_1","call_id":"call_1","name":"test_tool","arguments":"{\"arg\":\"value\"}"}}` + "\n\n" +
				`data: {"type":"response.completed","response":{"status":"completed","usage":{"input_tokens":11,"output_tokens":7,"total_tokens":18}}}` + "\n\n"},
		{"anthropic_tool_use", GatewayProtocolAnthropic,
			`data: {"type":"message_start","message":{"usage":{"input_tokens":11,"output_tokens":1}}}` + "\n\n" +
				`data: {"type":"content_block_start","index":0,"content_block":{"type":"tool_use","id":"call_1","name":"test_tool"}}` + "\n\n" +
				`data: {"type":"content_block_delta","index":0,"delta":{"type":"input_json_delta","partial_json":"{\"arg\":\"value\"}"}}` + "\n\n" +
				`data: {"type":"message_delta","delta":{"stop_reason":"tool_use"},"usage":{"output_tokens":7}}` + "\n\ndata: {\"type\":\"message_stop\"}\n\n"},
	}
	for _, tc := range scenarios {
		t.Run(tc.name, func(t *testing.T) {
			chunks := collectGatewayTerminationFixture(t, tc.protocol, tc.stream)
			var calls []backend.ToolCall
			var terminal *backend.StreamChunk
			done := 0
			for i := range chunks {
				chunk := &chunks[i]
				if chunk.Error != nil {
					t.Fatalf("unexpected chunk error: %v", chunk.Error)
				}
				calls = append(calls, chunk.ToolCalls...)
				if chunk.Done {
					done++
					terminal = chunk
				}
			}
			if done != 1 || terminal.Meta == nil || terminal.Meta.PromptTokens != 11 || terminal.Meta.CompletionTokens != 7 || terminal.Meta.TotalTokens != 18 {
				t.Fatalf("done=%d terminal=%+v, want clean completion with usage 11/7/18", done, terminal)
			}
			if len(calls) != 1 || calls[0].ID != "call_1" || calls[0].Name != "test_tool" || string(calls[0].Arguments) != `{"arg":"value"}` {
				t.Fatalf("calls = %+v, want one intact tool call", calls)
			}
		})
	}
}

func collectGatewayTerminationFixture(t *testing.T, protocol, stream string) []backend.StreamChunk {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = fmt.Fprint(w, stream)
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	client := NewGatewayWithConfig(GatewayConfig{URL: srv.URL, APIKey: "fixture-not-a-credential", ExternalModel: "fixture", Type: protocol})
	out, err := client.Stream(ctx, backend.StreamRequest{Messages: []backend.Message{{Role: "user", Content: "fixture"}}})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	var chunks []backend.StreamChunk
	for chunk := range out {
		chunks = append(chunks, chunk)
	}
	return chunks
}
