package geminicode

import (
	"testing"
)

func TestParseFrame_Wrapped(t *testing.T) {
	payload := `{"response":{"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"totalTokenCount":10}},"traceId":"123"}`
	resp, ok := ParseFrame(payload)
	if !ok {
		t.Fatalf("expected ok=true, got false")
	}
	if len(resp.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(resp.Candidates))
	}
	if resp.Candidates[0].Content.Parts[0].Text != "hello" {
		t.Fatalf("expected text 'hello', got %q", resp.Candidates[0].Content.Parts[0].Text)
	}
}

func TestParseFrame_Bare(t *testing.T) {
	payload := `{"candidates":[{"content":{"parts":[{"text":"world"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"totalTokenCount":5}}`
	resp, ok := ParseFrame(payload)
	if !ok {
		t.Fatalf("expected ok=true, got false")
	}
	if len(resp.Candidates) != 1 {
		t.Fatalf("expected 1 candidate, got %d", len(resp.Candidates))
	}
	if resp.Candidates[0].Content.Parts[0].Text != "world" {
		t.Fatalf("expected text 'world', got %q", resp.Candidates[0].Content.Parts[0].Text)
	}
}

func TestParseFrameError_NormalChunk(t *testing.T) {
	payload := `{"response":{"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"}}]}}`
	if err := ParseFrameError(payload); err != nil {
		t.Fatalf("expected nil error on normal chunk, got %v", err)
	}
}

func TestParseFrameError_ErrorFrame(t *testing.T) {
	payload := `{"error":{"code":429,"message":"Resource exhausted","status":"RESOURCE_EXHAUSTED"}}`
	err := ParseFrameError(payload)
	if err == nil {
		t.Fatalf("expected error, got nil")
	}
	if err.Code != 429 {
		t.Fatalf("expected code 429, got %d", err.Code)
	}
	if err.Status != "RESOURCE_EXHAUSTED" {
		t.Fatalf("expected status RESOURCE_EXHAUSTED, got %q", err.Status)
	}
}

func BenchmarkParseFrame_Wrapped(b *testing.B) {
	payload := `{"response":{"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"},"finishReason":"STOP","index":0}],"usageMetadata":{"totalTokenCount":10}},"traceId":"123"}`
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_, _ = ParseFrame(payload)
	}
}

func BenchmarkParseFrameError_NormalChunk(b *testing.B) {
	payload := `{"response":{"candidates":[{"content":{"parts":[{"text":"hello"}],"role":"model"}}]}}`
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ParseFrameError(payload)
	}
}
