package geminicode

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Chunk is one element of a decoded stream.
//
// Text carries the model's output. A thought is wrapped in the reference's
// own `<think>…</think>` markers — the reference's CLI renders thoughts
// that way and strips the block again before it shows the reply, so the
// markers are part of the dialect's observable behaviour and not an
// invention of this package.
//
// ToolCalls ride on the terminating chunk: a chunk with calls is a request
// to act, not finished text.
type Chunk struct {
	Text      string
	ToolCalls []TurnToolCall
	Meta      *Meta
	Error     error
	Done      bool
}

// Meta is the usage the terminating chunk carries.
type Meta struct {
	PromptTokens        int
	CompletionTokens    int
	TotalTokens         int
	ReasoningTokens     int
	CachedContentTokens int
	FinishReason        string
}

const (
	thinkOpen  = "<think>"
	thinkClose = "</think>"
)

// Decoder turns a CloudCode SSE body into chunks.
//
// It is a pure state machine: the caller owns the reader (and therefore
// the transport policy — an idle timeout, a retry), and hands the decoder
// one line at a time.
//
//	decode := geminicode.NewDecoder(client)
//	for scanner.Scan() {
//	    for _, chunk := range decode.Feed(scanner.Text()) {
//	        emit(chunk)
//	    }
//	}
//	chunks, err := decode.Finish(scanner.Err())
//
// The decoder keeps the thought signatures the model returns with its tool
// calls, so the next turn can replay them; see Client.Signatures.
type Decoder struct {
	client    *Client
	calls     []TurnToolCall
	seenCalls map[string]bool
	meta      Meta
	finished  bool
	sawFinish bool
	inThought bool
	streamErr error
}

// NewDecoder returns a decoder for one stream. The client may be nil when
// the caller does not want the thought signatures collected.
func NewDecoder(c *Client) *Decoder {
	return &Decoder{client: c, seenCalls: make(map[string]bool)}
}

// Feed consumes one SSE line and returns the chunks it produced.
//
// A line that carries no frame — a keep-alive, an error envelope, the
// `)]}'` anti-hijacking prefix Google's endpoints put in front of a
// response — produces no chunks.
func (d *Decoder) Feed(line string) []Chunk {
	line = strings.TrimSpace(line)
	if line == "" || strings.HasPrefix(line, ")]}'") {
		return nil
	}
	payload := line
	if strings.HasPrefix(payload, "data:") {
		payload = strings.TrimSpace(strings.TrimPrefix(payload, "data:"))
	}
	if payload == "" {
		return nil
	}
	if payload == "[DONE]" {
		return d.finalChunks()
	}
	if sErr := ParseFrameError(payload); sErr != nil {
		d.streamErr = sErr
		d.finished = true
		out := d.closeThought()
		return append(out, Chunk{Error: sErr, Done: true})
	}
	resp, ok := ParseFrame(payload)
	if !ok {
		return nil
	}
	return d.processResponse(resp)
}

// Finish ends the stream. It returns the closing chunks — the thought
// terminator, and the terminating chunk when the stream carried one — and
// the error, if any.
//
// The error is raw: the caller classifies it, because whether a dropped
// connection is worth retrying is a policy decision and not a dialect
// question. A stream that ends without a finishReason and without a tool
// call is reported as io.ErrUnexpectedEOF — the upstream closed mid-turn.
func (d *Decoder) Finish(scanErr error) ([]Chunk, error) {
	if scanErr != nil {
		return d.closeThought(), scanErr
	}
	if d.streamErr != nil {
		return d.closeThought(), d.streamErr
	}
	if !d.sawFinish && len(d.calls) == 0 {
		return d.closeThought(), io.ErrUnexpectedEOF
	}
	return d.finalChunks(), nil
}

// Calls returns the tool calls the stream collected so far.
func (d *Decoder) Calls() []TurnToolCall { return d.calls }

// processResponse folds one frame into the decoder's state.
func (d *Decoder) processResponse(resp GenerateContentResponse) []Chunk {
	var out []Chunk
	for _, cand := range resp.Candidates {
		for _, part := range cand.Content.Parts {
			out = append(out, d.handlePart(part)...)
		}
		if cand.FinishReason != "" {
			d.sawFinish = true
			d.meta.FinishReason = cand.FinishReason
		}
	}
	if u := resp.UsageMetadata; u != nil && u.TotalTokenCount > 0 {
		d.meta.PromptTokens = u.PromptTokenCount
		d.meta.CompletionTokens = u.CandidatesTokenCount
		d.meta.TotalTokens = u.TotalTokenCount
		d.meta.ReasoningTokens = u.ThoughtsTokenCount
		d.meta.CachedContentTokens = u.CachedContentTokenCount
	}
	if d.sawFinish {
		return append(out, d.finalChunks()...)
	}
	return out
}

// handlePart routes one part: a function call is collected, a thought is
// wrapped in the reference's markers, anything else is plain text.
func (d *Decoder) handlePart(part Part) []Chunk {
	if part.FunctionCall != nil {
		d.handleFunctionCall(part)
		return nil
	}
	if part.Thought {
		return d.handleThought(part)
	}
	if part.Text == "" {
		return nil
	}
	out := d.closeThought()
	return append(out, Chunk{Text: part.Text})
}

// handleFunctionCall collects a tool call and remembers the signature the
// model returned with it.
func (d *Decoder) handleFunctionCall(part Part) {
	call := TurnToolCall{Name: NormalizeToolName(part.FunctionCall.Name), Arguments: part.FunctionCall.Args}
	call.ID = part.FunctionCall.ID
	if call.ID == "" {
		call.ID = fmt.Sprintf("call_%d", len(d.calls)+1)
	}
	if len(call.Arguments) == 0 || !json.Valid(call.Arguments) {
		call.Arguments = json.RawMessage(`{}`)
	}
	if d.seenCalls[call.ID] {
		return
	}
	d.seenCalls[call.ID] = true
	d.calls = append(d.calls, call)
	if part.ThoughtSignature != "" && d.client != nil {
		d.client.setSignature(call.ID, part.ThoughtSignature)
	}
}

// handleThought emits the thought's text inside the reference's markers.
func (d *Decoder) handleThought(part Part) []Chunk {
	if part.Text == "" {
		return nil
	}
	var out []Chunk
	if !d.inThought {
		d.inThought = true
		out = append(out, Chunk{Text: thinkOpen})
	}
	return append(out, Chunk{Text: part.Text})
}

// closeThought terminates an open thought block.
func (d *Decoder) closeThought() []Chunk {
	if !d.inThought {
		return nil
	}
	d.inThought = false
	return []Chunk{{Text: thinkClose}}
}

// finalChunks emits the terminating chunk: the tool calls the model asked
// for and the usage it reported.
func (d *Decoder) finalChunks() []Chunk {
	if d.finished {
		return nil
	}
	out := d.closeThought()
	d.finished = true
	chunk := Chunk{Done: true, ToolCalls: d.calls}
	if d.meta != (Meta{}) {
		meta := d.meta
		chunk.Meta = &meta
	}
	return append(out, chunk)
}

// NormalizeToolName strips the Gemini namespace ("default_api:" or "default_api.")
// that Google models sometimes prefix onto tool names.
func NormalizeToolName(name string) string {
	for _, prefix := range []string{"default_api:", "default_api."} {
		if strings.HasPrefix(name, prefix) && len(name) > len(prefix) {
			return name[len(prefix):]
		}
	}
	return name
}

// IsSafetyFinishReason reports whether a finishReason from Gemini indicates
// the content was filtered or blocked by Google's safety systems.
func IsSafetyFinishReason(reason string) bool {
	switch strings.ToUpper(strings.TrimSpace(reason)) {
	case "SAFETY", "PROHIBITED_CONTENT", "BLOCKLIST", "SPII", "IMAGE_SAFETY":
		return true
	default:
		return false
	}
}
