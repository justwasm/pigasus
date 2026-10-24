// print_partial_response.go — cycle headless-partial-response-flag
// (AGY 1.2.10 close-gap).
//
// AGY 1.2.10 added `--print-partial-response` for headless mode: when set,
// the print path emits each chunk of the model's output as soon as it
// arrives instead of buffering the whole response until the turn
// completes. The reference's text format was already streaming, so the
// visible change is concentrated in `--output-format json` and
// `--output-format stream-json`, where the response would otherwise only
// surface inside the final envelope.
//
// The flag is a boolean, default false (preserves the byte-level contract
// for the v1.3.6 capture the clone was pinned to). When true:
//
//   - printFormatText: no change. The text path already does
//     `fmt.Fprint(out, sanitizer.Push(chunk.Text))` per chunk, so chunks
//     land on stdout as they arrive. The flag is therefore a no-op for
//     text mode — but it stays a valid combination so a script can set
//     it unconditionally.
//
//   - printFormatJSON: in addition to the existing buffer-into-envelope
//     behaviour, each chunk's sanitised text is written to stdout
//     immediately. The final envelope's `response` field still carries
//     the full accumulated text, so JSON consumers that wait for the
//     envelope still see the same value they did before.
//
//   - printFormatStreamJSON: a `partial_response` event is emitted per
//     chunk, with the sanitised text as its `response` payload. The
//     closing `result` event still carries the full envelope, so
//     consumers that only handle `result` keep working.
//
// The wire-level decisions pinned here:
//
//   - the partial side-channel writes the *sanitised* text (after the
//     think-block stripper in agent.Sanitizer.Push runs), so the user
//     never sees a chunk of `<think>…` and the next `partial_response`
//     carries the visible continuation.
//   - `partial_response` events do NOT carry conversation_id. The
//     jsonStreamEvent comment pins that field to `init`; every
//     `partial_response` is preceded by an `init` so consumers resolve
//     the id from the session header.
//   - the final `result` envelope is unchanged regardless of the flag —
//     the same `response`, `usage`, `num_turns`, `duration_seconds`, and
//     `error` fields the buffered path emitted. The flag's whole point
//     is to give earlier feedback, not to change the terminator.
//
// The helper below is the single point where the flag's effect lives, so
// it can be unit-tested without standing up the full PiG-runtime turn.
package printmode

import (
	"fmt"
	"io"
	"strings"
)

// partialResponseState is the per-turn accumulator runPrintMode hands the
// chunk handler. It carries the destination writer (so chunks land on
// the same stdout the envelope eventually does), the buffered response
// builder for json/stream-json envelopes, and the format the user
// picked.
type partialResponseState struct {
	// W is the byte-level sink both the streamed chunks and the final
	// envelope land on. Usually os.Stdout in production; tests use a
	// bytes.Buffer.
	W io.Writer
	// ResponseBuf accumulates the full text the model produced across
	// every chunk so the final jsonEnvelope / stream-json `result`
	// carries the entire response. Nil is allowed for the text format
	// path, where the chunks already go straight to W and no envelope
	// is written.
	ResponseBuf *strings.Builder
	// Format is the resolved `--output-format` value
	// (printFormatText / printFormatJSON / printFormatStreamJSON).
	// When the user picks stream-json, the partial side-channel emits
	// `partial_response` events; for json it writes plain text; for
	// text it does nothing (already streaming).
	Format string
	// Enabled is the value of `--print-partial-response`. False leaves
	// the chunk handler on the buffered path the previous cycles
	// produced; true engages the per-chunk side-channel.
	Enabled bool
}

// WriteChunk routes one sanitised chunk through the partial-response
// machinery. With Enabled=false it falls through to the buffered
// behaviour (the chunk lands only in ResponseBuf, and the envelope at
// end carries it). With Enabled=true the chunk is also written
// immediately:
//
//   - text format: nothing extra to do — W already got the chunk via
//     `fmt.Fprint(W, sanitized)` in the caller. This helper therefore
//     only writes to ResponseBuf, which is unused for text.
//   - json format: the sanitised chunk goes to W directly (raw text on
//     stdout) AND into ResponseBuf (so the envelope is still complete).
//   - stream-json format: a `partial_response` event is emitted to W
//     AND the text lands in ResponseBuf.
//
// An empty sanitised string is a no-op for the partial side-channel —
// the agent.Sanitizer may hold text back at chunk boundaries (think-block
// tags), and emitting an empty `partial_response` would pollute the
// stream with zero-byte frames.
func (s partialResponseState) WriteChunk(sanitized string) error {
	if sanitized == "" {
		return nil
	}
	if !s.Enabled {
		// Buffered path: the caller has already written `sanitized`
		// to whatever `out` it chose; nothing extra to do here.
		// Kept symmetric so the chunk handler calls this
		// unconditionally regardless of the flag's value.
		return nil
	}
	switch s.Format {
	case printFormatJSON:
		// Write the chunk to stdout immediately so the user sees it
		// before the turn completes. The final envelope still
		// carries the full accumulated text via ResponseBuf, which
		// the caller already updated.
		if _, err := fmt.Fprint(s.W, sanitized); err != nil {
			return err
		}
	case printFormatStreamJSON:
		// Emit one stream-json `partial_response` event. The payload
		// is `{"event":"partial_response","partial_response":{"response":"…"}}`.
		if err := emitPartialResponse(s.W, sanitized); err != nil {
			return err
		}
	case printFormatText:
		// Text format was already streaming per chunk; the caller
		// wrote sanitized to W. Nothing to do.
	default:
		// Unknown format: treat as buffered to keep the previous
		// cycle's behaviour. The flag must never crash an existing
		// pipeline.
	}
	return nil
}
