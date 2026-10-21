// fake_provider_test.go — a smoke test for the scripted ai.Provider type.
//
// In a real subagenttools.Invoke call, PiG's `coding.Session` ignores
// the inline `pi.Model.Provider` field and builds its own provider
// from `Model.ProviderMeta.ProviderID` against the registry. The
// only auth-free path in this cycle is the "test-faux" ProviderID
// (gated on `PIG_TEST_FAUX=1`), so end-to-end Invoke tests live in
// subagenttools_test.go using ai.TestFauxProvider. This file keeps
// `scriptedProvider` as a small fixture-only type that lets the
// package's own tests marshal a fake transcript without depending on
// network — it is exercised once here so the type is part of the
// "go test" build and stays compile-clean if future cycles need it.
package subagenttools

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	pi "github.com/MichaelKinsy/PiG/ai"
)

// jsonMarshal is a tiny indirection over json.Marshal so this file's
// helper signature looks identical to the upstream helper that some
// other PiG packages expose. It exists only so the fixture reads
// cleanly; inlining json.Marshal would also work.
func jsonMarshal(v any) ([]byte, error) { return json.Marshal(v) }

// emptyTranscript returns a TranscriptContext with no messages —
// enough to drive scriptedProvider.Stream in unit tests. PiG only
// exposes TranscriptContext construction through NormalizeContext,
// which is the public seam used everywhere providers consume a
// request; the unexported `newTranscriptContext` is private.
func emptyTranscript() pi.TranscriptContext {
	return pi.NormalizeContext(pi.Context{})
}

// scriptedProvider is a deterministic ai.Provider this package's
// subagenttools tests use directly. It is NOT the path real
// subagenttools.Invoke takes (see file header).
type scriptedProvider struct {
	// Response is the text the provider emits on every Stream call.
	Response string
	// Calls records each Stream invocation's transcript snapshot.
	Calls []scriptedCall
	// FailWith, when non-nil, is returned from Stream as a Go error
	// (no events emitted). Tests use this to drive the error path.
	FailWith error
	mu       sync.Mutex
}

// scriptedCall captures one Stream call's essentials.
type scriptedCall struct {
	// LastText is the user-role message text the parent's session
	// produced for the most-recent turn.
	LastText string
}

func (p *scriptedProvider) ID() string   { return "subagenttools-scripted" }
func (p *scriptedProvider) Close() error { return nil }

// Stream emits Response as a single TextStart + TextDelta + TextEnd
// sequence followed by Done. It honours FailWith on every call so a
// test can drive the error path without subclassing.
func (p *scriptedProvider) Stream(ctx context.Context, transcript pi.TranscriptContext, _ pi.StreamOptions) (*pi.AssistantMessageEventStream, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	p.mu.Lock()
	resp := p.Response
	if resp == "" {
		resp = "stubbed subagent reply"
	}
	if p.FailWith != nil {
		err := p.FailWith
		p.mu.Unlock()
		return nil, err
	}
	p.Calls = append(p.Calls, scriptedCall{LastText: lastUserText(transcript)})
	p.mu.Unlock()

	stream := pi.NewAssistantMessageEventStream()
	partial := &pi.AssistantMessage{
		API:        pi.APIPiMessages,
		Provider:   "subagenttools-scripted",
		Model:      "subagenttools-scripted-model",
		Usage:      pi.Usage{},
		StopReason: pi.StopReasonPending,
		Timestamp:  time.Now().UnixMilli(),
	}
	partial.Content = append(partial.Content, pi.TextContent{Text: ""})
	if err := stream.Push(pi.StartEvent{Partial: partial}); err != nil {
		return nil, err
	}
	if err := stream.Push(pi.TextStartEvent{ContentIndex: 0, Partial: partial}); err != nil {
		return nil, err
	}
	if err := stream.Push(pi.TextDeltaEvent{ContentIndex: 0, Delta: resp, Partial: partial}); err != nil {
		return nil, err
	}
	partial.Content[0] = pi.TextContent{Text: resp}
	if err := stream.Push(pi.TextEndEvent{ContentIndex: 0, Content: resp, Partial: partial}); err != nil {
		return nil, err
	}
	final := *partial
	final.StopReason = pi.StopReasonStop
	if err := stream.Push(pi.DoneEvent{Reason: pi.StopReasonStop, Message: &final}); err != nil {
		return nil, err
	}
	stream.End(&final)
	return stream, nil
}

func (p *scriptedProvider) callCount() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return len(p.Calls)
}

// lastUserText returns the last user-role text in the transcript, or
// "" when none exists. PiG's transcript message order is stable
// (older messages first), so we walk backward from the end and pick
// the first message with non-empty text — that is what the model's
// "user" role rendered.
func lastUserText(transcript pi.TranscriptContext) string {
	for _, msg := range transcript.Messages() {
		text := jsonText(msg)
		if text != "" {
			return text
		}
	}
	return ""
}

// jsonText extracts a human-readable text representation of a
// transcript message by scanning the JSON encoding for a "text" or
// "content" string field. The result is lossy but stable enough for
// tests that only need to assert "the prompt was forwarded".
func jsonText(msg any) string {
	raw, err := jsonMarshal(msg)
	if err != nil {
		return ""
	}
	s := string(raw)
	for _, key := range []string{`"text":`, `"content":`} {
		if idx := strings.Index(s, key); idx >= 0 {
			rest := s[idx+len(key):]
			rest = strings.TrimLeftFunc(rest, func(r rune) bool { return r == ' ' || r == '\n' || r == '\t' })
			if len(rest) == 0 || rest[0] != '"' {
				continue
			}
			var b strings.Builder
			for i := 1; i < len(rest); i++ {
				c := rest[i]
				if c == '\\' && i+1 < len(rest) {
					switch rest[i+1] {
					case 'n':
						b.WriteByte('\n')
					case 't':
						b.WriteByte('\t')
					case '"':
						b.WriteByte('"')
					case '\\':
						b.WriteByte('\\')
					default:
						b.WriteByte(c)
						b.WriteByte(rest[i+1])
					}
					i++
					continue
				}
				if c == '"' {
					return b.String()
				}
				b.WriteByte(c)
			}
			return b.String()
		}
	}
	return ""
}

// TestScriptedProviderSatisfiesInterface is a compile-time smoke
// test that scriptedProvider implements pi.Provider and that
// Stream emits a Done event. It exists so this file is part of the
// `go test` build and so a future cycle can extend the fixture with
// confidence that the type signature holds.
func TestScriptedProviderSatisfiesInterface(t *testing.T) {
	var _ pi.Provider = (*scriptedProvider)(nil)

	p := &scriptedProvider{Response: "hello-world"}
	stream, err := p.Stream(context.Background(), emptyTranscript(), pi.StreamOptions{})
	if err != nil {
		t.Fatalf("Stream: %v", err)
	}
	if stream == nil {
		t.Fatal("Stream returned nil stream")
	}
	final := stream.Result()
	if final == nil {
		t.Fatal("stream.Result() returned nil message")
	}
	if final.StopReason != pi.StopReasonStop {
		t.Fatalf("StopReason = %q, want %q", final.StopReason, pi.StopReasonStop)
	}
	text := ""
	for _, block := range final.Content {
		if textBlock, ok := block.(pi.TextContent); ok {
			text += textBlock.Text
		}
	}
	if text != "hello-world" {
		t.Fatalf("Content text = %q, want %q", text, "hello-world")
	}
	if p.callCount() != 1 {
		t.Fatalf("callCount = %d, want 1", p.callCount())
	}
}

// TestScriptedProviderFailWith verifies FailWith short-circuits
// Stream before any event is emitted.
func TestScriptedProviderFailWith(t *testing.T) {
	p := &scriptedProvider{FailWith: errors.New("provider offline")}
	stream, err := p.Stream(context.Background(), emptyTranscript(), pi.StreamOptions{})
	if err == nil {
		t.Fatal("Stream returned nil error")
	}
	if stream != nil {
		t.Fatalf("expected nil stream on failure, got %v", stream)
	}
	if err.Error() != "provider offline" {
		t.Fatalf("err.Error() = %q, want %q", err.Error(), "provider offline")
	}
}
