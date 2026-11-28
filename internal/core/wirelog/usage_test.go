// usage_test.go — the token counts the recorder lifts off a response.
//
// A usage count is the only number in this package the provider stated
// rather than this process measured, so the questions that matter are
// "which frame's number did it take" and "did it invent one when none was
// sent". Both are answered here.
package wirelog

import (
	"strings"
	"testing"
)

// TestTheLastUsageFrameWins — a stream reports usage more than once, and the
// figure that describes the turn is the last one.
//
// Gemini's stream sends a usageMetadata block per chunk. Taking the first
// would under-report every streamed answer by however many chunks followed.
func TestTheLastUsageFrameWins(t *testing.T) {
	body := strings.Join([]string{
		`data: {"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":1}}`,
		`data: {"usageMetadata":{"promptTokenCount":100,"candidatesTokenCount":48}}`,
		`data: {"usageMetadata":{"promptTokenCount":12345,"candidatesTokenCount":678,"totalTokenCount":13023}}`,
		"",
	}, "\n")

	p, c, tot, ok := ExtractUsage(body)
	if !ok {
		t.Fatal("no usage found in a body that reported it three times")
	}
	if p != 12345 || c != 678 || tot != 13023 {
		t.Errorf("usage = (%d, %d, %d), want the last frame (12345, 678, 13023)", p, c, tot)
	}
}

// TestNoUsageIsNotZeroUsage — a body that says nothing about tokens reports
// "not found", not zero.
//
// The caller distinguishes them: PromptTokens 0 with found=true would claim
// the provider measured an empty prompt, which is a different statement from
// "this endpoint did not report usage at all" (an OpenAI-compatible gateway
// with stream_options unset does exactly that).
func TestNoUsageIsNotZeroUsage(t *testing.T) {
	for _, body := range []string{
		"",
		`data: {"candidates":[{"finishReason":"STOP"}]}`,
		`{"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`, // explicit zeros still count
	} {
		p, c, tot, ok := ExtractUsage(body)
		if !ok && (p != 0 || c != 0 || tot != 0) {
			t.Errorf("body %q: not found but returned (%d, %d, %d)", body, p, c, tot)
		}
	}
	// The explicit-zero case is a real report and must not be dropped: a
	// gateway that measured nothing is telling you something.
	if _, _, _, ok := ExtractUsage(`{"usage":{"prompt_tokens":0,"completion_tokens":0,"total_tokens":0}}`); !ok {
		t.Error("an explicit zero usage report was treated as no report")
	}
}

// TestUsageOutsideTheWindowIsNotInventedFromNoise — the scan is bounded to
// the tail, and a body with no usage in that window reports nothing rather
// than matching a digit sequence that is not a usage block.
func TestUsageOutsideTheWindowIsNotInventedFromNoise(t *testing.T) {
	// Real usage, then more than the 64 KiB window of padding after it.
	body := `{"usageMetadata":{"promptTokenCount":777,"candidatesTokenCount":7}}` +
		strings.Repeat("x", 200<<10)
	if p, _, _, ok := ExtractUsage(body); ok {
		t.Errorf("usage was invented from outside the scan window: prompt=%d", p)
	}
}

// TestTotalIsDerivedWhenOnlyHalvesAreReported — a provider that sends the
// two halves and no total still yields a total, and one that sends a total
// keeps the provider's own arithmetic.
func TestTotalIsDerivedWhenOnlyHalvesAreReported(t *testing.T) {
	p, c, tot, ok := ExtractUsage(`{"usageMetadata":{"promptTokenCount":200,"candidatesTokenCount":50}}`)
	if !ok || p != 200 || c != 50 || tot != 250 {
		t.Errorf("usage = (%d, %d, %d, %v), want (200, 50, 250, true)", p, c, tot, ok)
	}

	// A provider total that disagrees with the sum is the provider's
	// number, not ours to correct.
	_, _, tot, ok = ExtractUsage(`{"usageMetadata":{"promptTokenCount":200,"candidatesTokenCount":50,"totalTokenCount":300}}`)
	if !ok || tot != 300 {
		t.Errorf("provider total was overridden: got %d, want 300", tot)
	}
}

// TestBytesPerPromptTokenIsTheAnomalySignal — the ratio the Requests view
// shows is request bytes over prompt tokens, and it is 0 when either side is
// unknown rather than a division by zero.
func TestBytesPerPromptTokenIsTheAnomalySignal(t *testing.T) {
	r := Record{RequestBytes: 3200, PromptTokens: 1000}
	if got := r.BytesPerPromptToken(); got != 3.2 {
		t.Errorf("ratio = %v, want 3.2", got)
	}
	if got := (Record{RequestBytes: 3200}).BytesPerPromptToken(); got != 0 {
		t.Errorf("ratio with no prompt tokens = %v, want 0", got)
	}
	if got := (Record{PromptTokens: 1000}).BytesPerPromptToken(); got != 0 {
		t.Errorf("ratio with no request bytes = %v, want 0", got)
	}
}

// TestCaptureLiftsUsageFromAFramedResponse — the store's capture path is what
// the view actually reads, so usage has to survive it, including past the
// body cap.
func TestCaptureLiftsUsageFromAFramedResponse(t *testing.T) {
	var b strings.Builder
	for i := 0; i < 3000; i++ {
		b.WriteString(`data: {"candidates":[{"content":{"parts":[{"text":"padding padding padding"}]}}]}`)
		b.WriteString("\n")
	}
	b.WriteString(`data: {"usageMetadata":{"promptTokenCount":54321,"candidatesTokenCount":99},"candidates":[{"finishReason":"STOP"}]}`)
	b.WriteString("\n")
	raw := []byte(b.String())
	if len(raw) <= MaxBodyBytes {
		t.Fatalf("fixture is %d bytes and did not exceed the %d-byte cap", len(raw), MaxBodyBytes)
	}

	st := New(4)
	rec := st.Capture(Record{Seq: 1}, Captured{}, CaptureBody(raw))

	if !rec.ResponseTruncated {
		t.Error("an over-cap body was not marked truncated")
	}
	if rec.PromptTokens != 54321 || rec.CompletionTokens != 99 {
		t.Errorf("usage after the cap = (%d, %d), want (54321, 99) — the tail was cut",
			rec.PromptTokens, rec.CompletionTokens)
	}
	if !strings.Contains(rec.ResponseBody, "finishReason") {
		t.Error("the finish reason in the tail was lost")
	}
	// The sizes are what the exchange carried, not what the record kept.
	if rec.ResponseBytes != int64(len(raw)) {
		t.Errorf("ResponseBytes = %d, want the real %d", rec.ResponseBytes, len(raw))
	}
}
