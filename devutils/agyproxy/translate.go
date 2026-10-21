package agyproxy

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/justwasm/pigasus/devutils/geminiwire"
	"github.com/justwasm/pigasus/internal/models"
)

// ToChatRequest converts agy's Gemini request into the provider-neutral shape
// internal/models speaks.
//
// Mapping notes (agy → provider):
//
//	systemInstruction.parts[].text        → ChatRequest.SystemPrompt
//	contents[].role "model"               → "assistant"
//	contents[].parts[].text               → message content
//	contents[].parts[].functionCall       → assistant .ToolCalls
//	contents[].parts[].functionResponse   → role "tool" message with .ToolCallID
//	tools[].functionDeclarations[]        → ChatRequest.Tools
//	generationConfig.maxOutputTokens      → MaxTokens
//	generationConfig.temperature          → Temperature
//
// Thought parts are dropped: they are Gemini's chain-of-thought echo and no
// provider in the registry has a field for them yet (see the deliverable's
// follow-ups).
func ToChatRequest(m models.Model, inner *geminiwire.GenerateContentRequest) models.ChatRequest {
	req := models.ChatRequest{
		ExternalModel:   m.ExternalModelName,
		SystemPrompt:    inner.SystemText(),
		ReasoningEffort: m.ReasoningEffort,
	}
	if gc := inner.GenerationConfig; gc != nil {
		if gc.MaxOutputTokens > 0 {
			req.MaxTokens = gc.MaxOutputTokens
		}
		if gc.Temperature != nil {
			req.Temperature = *gc.Temperature
		}
	}

	for _, c := range inner.Contents {
		role := strings.ToLower(strings.TrimSpace(c.Role))
		if role == "" {
			role = "user"
		}
		hasCall, hasResponse := false, false
		for _, p := range c.Parts {
			if p.FunctionCall != nil {
				hasCall = true
			}
			if p.FunctionResponse != nil {
				hasResponse = true
			}
		}

		switch {
		case hasCall:
			msg := models.ChatMessage{Role: "assistant"}
			for _, p := range c.Parts {
				if p.FunctionCall != nil {
					msg.ToolCalls = append(msg.ToolCalls, models.ToolCall{
						ID:        callID(p.FunctionCall),
						Name:      p.FunctionCall.Name,
						Arguments: jsonArgs(p.FunctionCall.Args),
					})
					continue
				}
				if p.Text != "" && !p.Thought {
					msg.Content += p.Text
				}
			}
			req.Messages = append(req.Messages, msg)
		case hasResponse:
			// One tool message per functionResponse: that is the OpenAI
			// convention and it preserves the call/response pairing.
			for _, p := range c.Parts {
				if p.FunctionResponse == nil {
					continue
				}
				req.Messages = append(req.Messages, models.ChatMessage{
					Role:       "tool",
					Content:    functionResponseText(p.FunctionResponse),
					ToolCallID: responseCallID(p.FunctionResponse),
				})
			}
		default:
			outRole := "user"
			if role == "model" || role == "assistant" {
				outRole = "assistant"
			}
			var parts []string
			for _, p := range c.Parts {
				if p.Text == "" || p.Thought {
					continue
				}
				parts = append(parts, p.Text)
			}
			if len(parts) == 0 {
				continue
			}
			sep := ""
			if outRole == "user" {
				sep = "\n"
			}
			req.Messages = append(req.Messages, models.ChatMessage{
				Role:    outRole,
				Content: strings.Join(parts, sep),
			})
		}
	}

	for _, t := range inner.Tools {
		for _, fd := range t.FunctionDeclarations {
			spec := models.ToolSpec{Name: fd.Name, Description: fd.Description}
			if fd.Parameters != nil {
				if b, err := json.Marshal(lowerSchemaTypes(fd.Parameters)); err == nil {
					spec.Parameters = b
				}
			}
			req.Tools = append(req.Tools, spec)
		}
	}
	return req
}

func callID(fc *geminiwire.FunctionCall) string {
	if fc.ID != "" {
		return fc.ID
	}
	return "call_" + fc.Name
}

func responseCallID(fr *geminiwire.FunctionResponse) string {
	if fr.ID != "" {
		return fr.ID
	}
	return "call_" + fr.Name
}

func jsonArgs(args map[string]any) string {
	if len(args) == 0 {
		return "{}"
	}
	b, err := json.Marshal(args)
	if err != nil {
		return "{}"
	}
	return string(b)
}

// functionResponseText is what the tool result looks like on the wire. Gemini
// lets a response be any JSON value; OpenAI wants a string, so a string stays
// verbatim and everything else is serialised.
func functionResponseText(fr *geminiwire.FunctionResponse) string {
	switch v := fr.Response.(type) {
	case nil:
		return "{}"
	case string:
		return v
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprintf("%v", v)
		}
		return string(b)
	}
}

// lowerSchemaTypes rewrites Gemini's upper-case JSON-Schema types ("STRING",
// "OBJECT") to the lower-case spelling OpenAI requires. Unknown keys are copied
// untouched.
func lowerSchemaTypes(v any) any {
	switch t := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(t))
		for k, val := range t {
			if k == "type" {
				if s, ok := val.(string); ok {
					out[k] = strings.ToLower(s)
					continue
				}
			}
			out[k] = lowerSchemaTypes(val)
		}
		return out
	case []any:
		out := make([]any, len(t))
		for i, e := range t {
			out[i] = lowerSchemaTypes(e)
		}
		return out
	default:
		return v
	}
}

// ---- provider → Gemini --------------------------------------------------

// handleCustom serves one generateContent / streamGenerateContent call with a
// custom model: translate the request, stream through internal/models, and
// re-emit Gemini wire format.
func (s *Server) handleCustom(w http.ResponseWriter, r *http.Request, m models.Model, body []byte, stream, wrapped bool) {
	var env geminiwire.GenerateContentRequest
	if err := json.Unmarshal(body, &env); err != nil {
		writeJSONError(w, http.StatusBadRequest, "invalid JSON: "+err.Error())
		return
	}
	inner := env.Inner()
	prov, err := models.NewProvider(m)
	if err != nil {
		writeJSONError(w, http.StatusBadGateway, "provider: "+err.Error())
		return
	}
	chatReq := ToChatRequest(m, inner)
	// The inbound stream flag is authoritative: agy sometimes asks for a
	// non-streaming generateContent even for a streaming-capable model.
	chatReq.Stream = stream
	s.logf("%s → %s (%s, %d msg, %d tool)", m.Name, m.ExternalModelName, streamMode(stream), len(chatReq.Messages), len(chatReq.Tools))

	if !stream {
		resp, err := prov.Chat(r.Context(), chatReq)
		if err != nil {
			writeJSONError(w, http.StatusBadGateway, "upstream: "+err.Error())
			return
		}
		out := geminiwire.NewResponse(responseParts(resp.Content, resp.ToolCalls), "STOP")
		out.UsageMetadata = usage(resp.PromptTokens, resp.OutputTokens, resp.TotalTokens)
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(wrapResponse(out, wrapped))
		return
	}

	events, err := prov.Stream(r.Context(), chatReq)
	if err != nil {
		// Pre-stream failure: still a normal HTTP response so agy shows the
		// provider's message instead of "stream ended unexpectedly".
		writeJSONError(w, http.StatusBadGateway, "upstream: "+err.Error())
		return
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache")
	h.Set("Connection", "keep-alive")
	w.WriteHeader(http.StatusOK)
	flusher, _ := w.(http.Flusher)

	var tools []geminiwire.Part
	var meta *models.StreamMeta
	for ev := range events {
		if ev.Error != nil {
			s.logf("%s: stream error: %v", m.Name, ev.Error)
			writeSSE(w, flusher, map[string]any{"error": map[string]any{
				"code":    http.StatusBadGateway,
				"message": ev.Error.Error(),
				"status":  "UNAVAILABLE",
			}})
			return
		}
		if len(ev.ToolCalls) > 0 {
			tools = append(tools, toolCallParts(ev.ToolCalls)...)
		}
		if ev.Text != "" {
			chunk := geminiwire.NewResponse([]geminiwire.Part{{Text: ev.Text}}, "")
			writeSSE(w, flusher, wrapResponse(chunk, wrapped))
		}
		if ev.Done {
			meta = ev.Meta
			break
		}
	}

	final := geminiwire.NewResponse(tools, "STOP")
	if meta != nil {
		final.UsageMetadata = usage(meta.PromptTokens, meta.CompletionTokens, meta.TotalTokens)
	}
	writeSSE(w, flusher, wrapResponse(final, wrapped))
}

// responseParts renders a completed turn: text first, then any function calls.
func responseParts(text string, calls []models.ToolCall) []geminiwire.Part {
	var parts []geminiwire.Part
	if text != "" {
		parts = append(parts, geminiwire.Part{Text: text})
	}
	return append(parts, toolCallParts(calls)...)
}

// toolCallParts maps provider tool calls to Gemini functionCall parts. The id
// is preserved: agy echoes it back on the functionResponse, which is how the
// next turn pairs result with call.
func toolCallParts(calls []models.ToolCall) []geminiwire.Part {
	out := make([]geminiwire.Part, 0, len(calls))
	for _, c := range calls {
		args := map[string]any{}
		if s := strings.TrimSpace(c.Arguments); s != "" {
			if err := json.Unmarshal([]byte(s), &args); err != nil {
				// Some models emit a bare string or markdown-fenced JSON.
				// Keep the payload rather than losing the call.
				args = map[string]any{"_raw": c.Arguments}
			}
		}
		id := c.ID
		if id == "" {
			id = "call_" + c.Name
		}
		out = append(out, geminiwire.Part{FunctionCall: &geminiwire.FunctionCall{
			Name: c.Name,
			Args: args,
			ID:   id,
		}})
	}
	return out
}

func usage(prompt, output, total int) *geminiwire.UsageMetadata {
	if total == 0 {
		total = prompt + output
	}
	return &geminiwire.UsageMetadata{
		PromptTokenCount:     prompt,
		CandidatesTokenCount: output,
		TotalTokenCount:      total,
	}
}

// wrapResponse reproduces the envelope agy sent. CloudCode calls arrive with a
// `request` wrapper and agy reads the `response` wrapper back; plain Gemini
// calls stay bare.
func wrapResponse(resp geminiwire.GenerateContentResponse, wrapped bool) any {
	if !wrapped {
		return resp
	}
	return geminiwire.CloudCodeResponse{
		Response: resp,
		TraceID:  "",
		Metadata: map[string]any{},
	}
}

// writeSSE emits one `data: …` frame and flushes it.
func writeSSE(w http.ResponseWriter, f http.Flusher, payload any) {
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	_, _ = w.Write([]byte("data: "))
	_, _ = w.Write(b)
	_, _ = w.Write([]byte("\n\n"))
	if f != nil {
		f.Flush()
	}
}

func streamMode(stream bool) string {
	if stream {
		return "stream"
	}
	return "non-stream"
}
