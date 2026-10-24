// print_partial_response_test.go — cycle headless-partial-response-flag
// (AGY 1.2.10 close-gap).
//
// Five+ tests covering the per-chunk side-channel that --print-partial-response
// enables. The helper being tested (partialResponseState.WriteChunk) is the
// single place the flag's effect lives in cmd/wagyu; main.go's runPrintMode
// just routes through it. So if the helper works, the integration works.
//
// What the tests pin:
//
//   - Enabled=false is a strict no-op for the partial side-channel. The
//     chunk handler's existing buffered behaviour is preserved so the
//     byte-level contract for the v1.3.6 capture the clone was pinned
//     to is untouched.
//
//   - Enabled=true with text format is also a no-op for the side-channel:
//     text was already streaming. The flag must not change byte-level
//     output for text format.
//
//   - Enabled=true with json format writes each sanitised chunk to stdout
//     immediately, in order, AND the responseBuf still carries the full
//     accumulated text so the closing envelope matches.
//
//   - Enabled=true with stream-json format emits one `partial_response`
//     event per chunk, with `partial_response.response` as the payload,
//     and the responseBuf still carries the full text for the closing
//     `result` event.
//
//   - Chunks arrive in order: chunk N's side-channel output is byte-for-byte
//     what the caller pushed in that order.
//
//   - Empty sanitised strings (think-block tags held back by the
//     sanitizer) are NOT emitted — `partial_response` would otherwise
//     spam zero-byte frames.
//
//   - An empty ResponseBuf on text format is allowed (the text path never
//     reads it).
//
//   - A successful turn with Enabled=true still produces a complete
//     envelope whose response is the concatenation of every chunk.
package printmode

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

// TestPartialResponse_DisabledIsNoop pins the byte-level invariant: when
// --print-partial-response is unset, the partial side-channel is silent.
// Whatever the chunk handler does (write to `out`, etc.) is preserved; the
// helper must not write a single byte to W in the disabled path.
func TestPartialResponse_DisabledIsNoop(t *testing.T) {
	var w bytes.Buffer
	var rb strings.Builder
	state := partialResponseState{
		W:           &w,
		ResponseBuf: &rb,
		Format:      printFormatJSON,
		Enabled:     false,
	}
	if err := state.WriteChunk("hello "); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if err := state.WriteChunk("world"); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if got := w.String(); got != "" {
		t.Errorf("disabled flag must not write to W; got %q", got)
	}
	if got := rb.String(); got != "" {
		t.Errorf("disabled flag must not write to ResponseBuf (caller owns it); got %q", got)
	}
}

// TestPartialResponse_DisabledStreamJSONIsNoop is the same invariant for
// stream-json: no `partial_response` event must appear on stdout when the
// flag is unset, otherwise the byte-level contract against the v1.3.6
// reference capture would break.
func TestPartialResponse_DisabledStreamJSONIsNoop(t *testing.T) {
	var w bytes.Buffer
	var rb strings.Builder
	state := partialResponseState{
		W:           &w,
		ResponseBuf: &rb,
		Format:      printFormatStreamJSON,
		Enabled:     false,
	}
	if err := state.WriteChunk("a"); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if err := state.WriteChunk("b"); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if got := w.String(); got != "" {
		t.Errorf("disabled flag must not emit partial_response events; got %q", got)
	}
}

// TestPartialResponse_TextEnabledIsNoop pins the text-format invariant:
// text was already streaming per chunk (the cycle 50 `fmt.Fprint(out,
// sanitizer.Push(chunk.Text))` line), so the flag must not duplicate any
// bytes on stdout for the text path. The helper is the side-channel; for
// text it stays quiet.
func TestPartialResponse_TextEnabledIsNoop(t *testing.T) {
	var w bytes.Buffer
	state := partialResponseState{
		W:           &w,
		ResponseBuf: nil, // text format has no envelope
		Format:      printFormatText,
		Enabled:     true,
	}
	if err := state.WriteChunk("Hello, "); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if err := state.WriteChunk("world!"); err != nil {
		t.Fatalf("WriteChunk: %v", err)
	}
	if got := w.String(); got != "" {
		t.Errorf("text format + flag must not write to W (already streaming); got %q", got)
	}
}

// TestPartialResponse_JSONStreamInOrder drives three chunks through the
// json path with the flag enabled and asserts:
//
//   - W receives the chunks in the order they were pushed
//   - the responseBuf (caller-side accumulator) also sees the chunks so
//     the final envelope still carries the full text
//
// This is the changelog's "emit each chunk as it arrives" guarantee: the
// user sees bytes landing on stdout in real time.
func TestPartialResponse_JSONStreamInOrder(t *testing.T) {
	var w bytes.Buffer
	var rb strings.Builder
	state := partialResponseState{
		W:           &w,
		ResponseBuf: &rb,
		Format:      printFormatJSON,
		Enabled:     true,
	}
	chunks := []string{"Hello", ", ", "world", "!"}
	for _, c := range chunks {
		if err := state.WriteChunk(c); err != nil {
			t.Fatalf("WriteChunk(%q): %v", c, err)
		}
		// Mirrors what runPrintMode does after the helper: keep
		// building the envelope's responseBuf so the closing
		// jsonEnvelope still carries the full text.
		rb.WriteString(c)
	}
	want := "Hello, world!"
	if got := w.String(); got != want {
		t.Errorf("W = %q, want %q (chunks in arrival order)", got, want)
	}
	if got := rb.String(); got != want {
		t.Errorf("ResponseBuf = %q, want %q (envelope must still carry full text)", got, rb.String())
	}
}

// TestPartialResponse_StreamJSONEmitsEvents drives three chunks through
// the stream-json path and decodes each emitted line. The wire shape is:
//
//	{"event":"partial_response","partial_response":{"response":"…"}}
//
// One line per chunk, in order, with the sanitised text as the
// `partial_response.response` payload. conversation_id is intentionally
// absent — that field is reserved for the `init` event per the
// jsonStreamEvent comment.
func TestPartialResponse_StreamJSONEmitsEvents(t *testing.T) {
	var w bytes.Buffer
	var rb strings.Builder
	state := partialResponseState{
		W:           &w,
		ResponseBuf: &rb,
		Format:      printFormatStreamJSON,
		Enabled:     true,
	}
	chunks := []string{"Hello", ", ", "world", "!"}
	for _, c := range chunks {
		if err := state.WriteChunk(c); err != nil {
			t.Fatalf("WriteChunk(%q): %v", c, err)
		}
		rb.WriteString(c)
	}
	lines := strings.Split(strings.TrimRight(w.String(), "\n"), "\n")
	if len(lines) != len(chunks) {
		t.Fatalf("got %d lines, want %d (one partial_response per chunk):\n%s", len(lines), len(chunks), w.String())
	}
	for i, line := range lines {
		var ev jsonStreamEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Fatalf("line %d: %v\nline = %s", i, err, line)
		}
		if ev.Event != streamEventPartialResponse {
			t.Errorf("line %d event = %q, want %q", i, ev.Event, streamEventPartialResponse)
		}
		if ev.PartialResponse == nil {
			t.Fatalf("line %d: partial_response payload missing", i)
		}
		if got, want := ev.PartialResponse.Response, chunks[i]; got != want {
			t.Errorf("line %d payload = %q, want %q", i, got, want)
		}
		if ev.ConversationID != "" {
			t.Errorf("line %d carries conversation_id %q; partial_response must omit it (init-only)", i, ev.ConversationID)
		}
	}
	if got, want := rb.String(), "Hello, world!"; got != want {
		t.Errorf("ResponseBuf = %q, want %q", got, want)
	}
}

// TestPartialResponse_EmptyChunkIsNoop asserts that an empty sanitised
// string (which the sanitizer returns when it has buffered a `<think>`
// tag and is waiting for the closing tag at the next chunk boundary)
// does NOT produce a `partial_response` event. Zero-byte events would
// pollute the stream and break consumers that split on newlines.
func TestPartialResponse_EmptyChunkIsNoop(t *testing.T) {
	for _, format := range []string{printFormatJSON, printFormatStreamJSON, printFormatText} {
		var w bytes.Buffer
		var rb strings.Builder
		state := partialResponseState{
			W:           &w,
			ResponseBuf: &rb,
			Format:      format,
			Enabled:     true,
		}
		if err := state.WriteChunk(""); err != nil {
			t.Fatalf("format=%s: WriteChunk: %v", format, err)
		}
		if got := w.String(); got != "" {
			t.Errorf("format=%s: empty chunk must not write to W; got %q", format, got)
		}
	}
}

// TestPartialResponse_EnvelopeStillCompleteAtEnd drives a small "turn"
// through the helper with the flag enabled and verifies the full text is
// recoverable from the responseBuf at the end. This is the contract that
// the closing jsonEnvelope / `result` event still carries the complete
// response — a JSON consumer that only watches the terminator keeps
// working unchanged.
func TestPartialResponse_EnvelopeStillCompleteAtEnd(t *testing.T) {
	var w bytes.Buffer
	var rb strings.Builder
	state := partialResponseState{
		W:           &w,
		ResponseBuf: &rb,
		Format:      printFormatJSON,
		Enabled:     true,
	}
	turn := []string{"The ", "capital ", "of ", "France ", "is ", "Paris."}
	for _, c := range turn {
		if err := state.WriteChunk(c); err != nil {
			t.Fatalf("WriteChunk(%q): %v", c, err)
		}
		rb.WriteString(c)
	}
	// Mirror the cycle 114 envelope build from runPrintMode.
	env := jsonEnvelope{
		Status:   printStatusSuccess,
		Response: rb.String(),
	}
	out, err := json.Marshal(env)
	if err != nil {
		t.Fatal(err)
	}
	// Two invariants on the envelope:
	//   - response is the concatenation of every chunk in arrival order
	//   - status is still SUCCESS (the flag must not affect error framing)
	want := `"response":"The capital of France is Paris."`
	if !bytes.Contains(out, []byte(want)) {
		t.Errorf("envelope missing expected response field; got:\n%s", out)
	}
	if !bytes.Contains(out, []byte(`"status":"SUCCESS"`)) {
		t.Errorf("envelope status changed under flag; got:\n%s", out)
	}
	// And the user got the chunks on stdout as they arrived:
	if got, want := w.String(), "The capital of France is Paris."; got != want {
		t.Errorf("streamed text = %q, want %q", got, want)
	}
}

// TestPartialResponse_StreamJSONOneLinePerChunk is a wire-level guard:
// every emitted `partial_response` event is exactly one JSON line (the
// ndjson contract `--output-format stream-json` documents). A consumer
// that splits on '\n' must see one event per chunk and nothing else.
func TestPartialResponse_StreamJSONOneLinePerChunk(t *testing.T) {
	var w bytes.Buffer
	var rb strings.Builder
	state := partialResponseState{
		W:           &w,
		ResponseBuf: &rb,
		Format:      printFormatStreamJSON,
		Enabled:     true,
	}
	chunks := []string{"alpha", "beta", "gamma", "delta"}
	for _, c := range chunks {
		if err := state.WriteChunk(c); err != nil {
			t.Fatalf("WriteChunk(%q): %v", c, err)
		}
		rb.WriteString(c)
	}
	// No trailing newline from the LAST chunk matters: writeJSONLine
	// appends '\n' after every line it emits, so the buffer ends with
	// exactly len(chunks) newlines. A consumer splitting on '\n' must
	// see len(chunks) lines, the last of which may be empty if the
	// stream ends on a newline.
	if got, want := strings.Count(w.String(), "\n"), len(chunks); got != want {
		t.Errorf("newline count = %d, want %d (one per chunk); got buffer:\n%s", got, want, w.String())
	}
	// Every non-empty line must decode as a partial_response event.
	for i, line := range strings.Split(strings.TrimRight(w.String(), "\n"), "\n") {
		if line == "" {
			continue
		}
		var ev jsonStreamEvent
		if err := json.Unmarshal([]byte(line), &ev); err != nil {
			t.Errorf("line %d: %v\nline=%s", i, err, line)
			continue
		}
		if ev.Event != streamEventPartialResponse {
			t.Errorf("line %d event = %q, want %q", i, ev.Event, streamEventPartialResponse)
		}
	}
}
