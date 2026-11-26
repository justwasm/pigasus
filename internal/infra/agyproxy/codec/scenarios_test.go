package codec_test

import (
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/justwasm/pigasus/geminicode"
	"github.com/justwasm/pigasus/internal/infra/agyproxy/codec"
)

func TestRequestDecode_MalformedPayloadScenarios(t *testing.T) {
	for _, scenario := range []struct {
		name, body string
		request    any
		syntax     bool
	}{
		{"OpenAI truncated JSON", `{"messages":[`, &codec.OpenAIChatRequest{}, true},
		{"Anthropic truncated JSON", `{"messages":[`, &codec.AnthropicRequest{}, true},
		{"OpenAI messages must be an array", `{"messages":"wrong"}`, &codec.OpenAIChatRequest{}, false},
		{"Anthropic max_tokens must be an integer", `{"max_tokens":{}}`, &codec.AnthropicRequest{}, false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			err := json.Unmarshal([]byte(scenario.body), scenario.request)
			var syntax *json.SyntaxError
			var mismatch *json.UnmarshalTypeError
			if scenario.syntax && !errors.As(err, &syntax) || !scenario.syntax && !errors.As(err, &mismatch) {
				t.Fatalf("decode returned %T %v; syntax=%v", err, err, scenario.syntax)
			}
		})
	}
}

func TestRequestConversion_DefaultsAndUnknownContent(t *testing.T) {
	for _, effort := range []string{"", "unexpected", " HIGH ", "NONE", "low"} {
		t.Run("OpenAI reasoning "+effort, func(t *testing.T) {
			req := codec.OpenAIChatRequest{Messages: []codec.OpenAIMessage{{Role: " USER ", Content: false}}, MaxTokens: -1, ReasoningEffort: effort}
			turn := codec.OpenAIToTurn(req, codec.Media{}, "s", "r")
			wantEffort := "high"
			if effort == "NONE" || effort == "low" {
				wantEffort = strings.ToLower(effort)
			}
			if turn.MaxOutputTokens != 8192 || turn.Temperature != 0 || turn.ThinkingLevel != wantEffort || len(turn.Messages) != 1 || turn.Messages[0].Role != "user" || turn.Messages[0].Content != "" {
				t.Fatalf("default conversion = %+v", turn)
			}
		})
	}
	t.Run("Anthropic unsupported blocks remain ignored", func(t *testing.T) {
		turn := codec.AnthropicToTurn(codec.AnthropicRequest{System: 10, Messages: []codec.AnthropicMessage{{Role: "user", Content: []any{nil, false, map[string]any{"type": "unknown"}}}}}, codec.Media{}, "s", "r")
		if turn.MaxOutputTokens != 8192 || turn.SystemPrompt != "" || len(turn.Messages) != 0 || turn.ThinkingLevel != "" {
			t.Fatalf("default conversion = %+v", turn)
		}
	})
}

// TestPureMapping_SystemPromptVerbatim ensures codec performs no cloaking or
// mutation of the system prompt: the raw joined text is passed through as-is.
// Cloaking is the proxy host's responsibility, not the codec's.
func TestPureMapping_SystemPromptVerbatim(t *testing.T) {
	t.Run("OpenAI system prompt passes through verbatim", func(t *testing.T) {
		const system = "Built on Anthropic's Claude Agent SDK. You are Codex, an agent based on GPT-5."
		req := codec.OpenAIChatRequest{Messages: []codec.OpenAIMessage{
			{Role: "system", Content: system},
			{Role: "user", Content: "hi"},
		}}
		turn := codec.OpenAIToTurn(req, codec.Media{}, "s", "r")
		if turn.SystemPrompt != system {
			t.Fatalf("codec mutated system prompt: got %q want %q", turn.SystemPrompt, system)
		}
		if strings.Contains(turn.SystemPrompt, "\u200b") {
			t.Fatalf("codec must not insert zero-width separators: %q", turn.SystemPrompt)
		}
	})

	t.Run("Anthropic system prompt passes through verbatim", func(t *testing.T) {
		const system = "Built on Anthropic's Claude Agent SDK."
		req := codec.AnthropicRequest{
			System:   []any{map[string]any{"type": "text", "text": system}},
			Messages: []codec.AnthropicMessage{{Role: "user", Content: "hi"}},
		}
		turn := codec.AnthropicToTurn(req, codec.Media{}, "s", "r")
		if turn.SystemPrompt != system {
			t.Fatalf("codec mutated system prompt: got %q want %q", turn.SystemPrompt, system)
		}
		if strings.Contains(turn.SystemPrompt, "\u200b") {
			t.Fatalf("codec must not insert zero-width separators: %q", turn.SystemPrompt)
		}
	})
}

func TestParseDataURI_FixtureScenarios(t *testing.T) {
	for _, scenario := range []struct {
		name, uri, mime, text string
		ok                    bool
	}{
		{"base64", "data:image/png;base64,AQID", "image/png", "\x01\x02\x03", true},
		{"empty MIME", "data:;base64,aGk=", "application/octet-stream", "hi", true},
		{"literal data stays literal", "data:text/plain,a%20b", "text/plain", "a%20b", true},
		{"empty body", "data:text/plain,", "text/plain", "", true},
		{"invalid base64", "data:image/png;base64,%%%", "", "", false},
		{"missing comma", "data:image/png;base64", "", "", false},
		{"not a data URI", "https://media.invalid/file", "", "", false},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			mime, data, ok := codec.ParseDataURI(scenario.uri)
			if ok != scenario.ok || mime != scenario.mime || string(data) != scenario.text {
				t.Fatalf("ParseDataURI = %q, %q, %v", mime, data, ok)
			}
		})
	}
}

func TestMediaConversion_InMemoryResolutionAndMIMEPrecedence(t *testing.T) {
	t.Run("OpenAI inline forms bypass resolver and preserve explicit MIME", func(t *testing.T) {
		var raw any
		const body = `[
		  {"type":"image","image":"aGk="},
		  {"type":"document","data":"aGk=","media_type":"application/custom"},
		  {"type":"file","file":{"url":"https://fixture.invalid/document","mimeType":"application/pdf"}},
		  {"type":"input_audio","input_audio":{"data":"aGk="}},
		  {"type":"input_audio","input_audio":{"data":"%%%"}},
		  {"type":"input_image","image_url":{"url":"data:image/webp;base64,aGk="}}
		]`
		if err := json.Unmarshal([]byte(body), &raw); err != nil {
			t.Fatal(err)
		}
		var resolved []string
		_, images := codec.ParseOpenAIContent(raw, codec.Media{
			Resolve: func(uri string) (string, []byte, error) {
				resolved = append(resolved, uri)
				return "application/detected", []byte("doc"), nil
			},
			DetectMIME: func(data []byte) string {
				if string(data) != "hi" {
					t.Fatalf("detector data = %q", data)
				}
				return "image/sniffed"
			},
		})
		var mimes []string
		for _, image := range images {
			mimes = append(mimes, image.MIMEType)
		}
		if !reflect.DeepEqual(mimes, []string{"image/sniffed", "application/custom", "application/pdf", "audio/wav", "image/webp"}) || !reflect.DeepEqual(resolved, []string{"https://fixture.invalid/document"}) {
			t.Fatalf("MIME/resolver precedence = %v / %v", mimes, resolved)
		}
	})
	t.Run("Anthropic defaults and resolved MIME", func(t *testing.T) {
		var req codec.AnthropicRequest
		const body = `{"messages":[{"role":"user","content":[
		  {"type":"image","source":{"type":"base64","data":"aGk="}},
		  {"type":"document","source":{"type":"base64","data":"aGk="}},
		  {"type":"image","source":{"type":"base64","data":"%%%"}},
		  {"type":"document","source":{"type":"path","path":"/fixture/document","media_type":"application/ignored"}}
		]}]}`
		if err := json.Unmarshal([]byte(body), &req); err != nil {
			t.Fatal(err)
		}
		turn := codec.AnthropicToTurn(req, codec.Media{Resolve: func(uri string) (string, []byte, error) {
			if uri != "/fixture/document" {
				t.Fatalf("resolved URI = %q", uri)
			}
			return "application/resolved", []byte("doc"), nil
		}}, "s", "r")
		if len(turn.Messages) != 1 || len(turn.Messages[0].Images) != 3 {
			t.Fatalf("Anthropic media = %+v", turn.Messages)
		}
		images := turn.Messages[0].Images
		if images[0].MIMEType != "image/png" || images[1].MIMEType != "application/pdf" || images[2].MIMEType != "application/resolved" {
			t.Fatalf("Anthropic MIME precedence = %+v", images)
		}
	})
}

func TestUnaryResponse_ExactJSONFixtures(t *testing.T) {
	for _, scenario := range []struct {
		name string
		got  map[string]any
		want string
	}{
		{"OpenAI empty completion", codec.OpenAICompletion("id", "model", 7, "", "", nil, geminicode.Meta{}), `{"choices":[{"finish_reason":"stop","index":0,"message":{"content":"","role":"assistant"}}],"created":7,"id":"id","model":"model","object":"chat.completion","usage":{"completion_tokens":0,"prompt_tokens":0,"total_tokens":0}}`},
		{"Anthropic empty completion retains null content", codec.AnthropicMessageResponse("id", "model", "", "", nil, geminicode.Meta{}), `{"content":null,"id":"id","model":"model","role":"assistant","stop_reason":"end_turn","stop_sequence":null,"type":"message","usage":{"input_tokens":0,"output_tokens":0}}`},
		{"Anthropic JSON null tool argument", codec.AnthropicMessageResponse("id", "model", "", "", []geminicode.TurnToolCall{{ID: "call", Name: "search", Arguments: json.RawMessage(`null`)}}, geminicode.Meta{}), `{"content":[{"id":"call","input":null,"name":"search","type":"tool_use"}],"id":"id","model":"model","role":"assistant","stop_reason":"tool_use","stop_sequence":null,"type":"message","usage":{"input_tokens":0,"output_tokens":0}}`},
	} {
		t.Run(scenario.name, func(t *testing.T) {
			data, err := json.Marshal(scenario.got)
			if err != nil {
				t.Fatal(err)
			}
			if string(data) != scenario.want {
				t.Fatalf("response JSON:\n got %s\nwant %s", data, scenario.want)
			}
		})
	}
}

func TestAnthropicStream_BufferedCallThenErrorFixture(t *testing.T) {
	decoder := geminicode.NewDecoder(nil)
	converter := &codec.AnthropicStream{}
	const fixture = `data: {"response":{"candidates":[{"content":{"role":"model","parts":[{"functionCall":{"id":"call-1","name":"search","args":{"q":"x"}}}]}}]}}

data: {"error":{"code":429,"status":"RESOURCE_EXHAUSTED","message":"quota fixture"}}
`
	var events []codec.Event
	var lastError error
	for _, line := range strings.Split(fixture, "\n") {
		for _, chunk := range decoder.Feed(line) {
			if chunk.Error != nil {
				lastError = chunk.Error
			}
			events = append(events, converter.Feed(chunk)...)
		}
	}
	var typed *geminicode.StreamError
	if !errors.As(lastError, &typed) || typed.Code != 429 || len(decoder.Calls()) != 1 || len(events) != 1 || events[0].Name != "error" {
		t.Fatalf("buffered error fixture = calls %+v, error %v, events %+v", decoder.Calls(), lastError, events)
	}
	final := converter.Finish(len(decoder.Calls()) > 0)
	if final[0].Payload["delta"].(map[string]any)["stop_reason"] != "tool_use" {
		t.Fatalf("buffered calls lost on error: %+v", final)
	}
}