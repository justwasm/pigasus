// Package backend — transcript_test.go: unit tests for the
// TrimTrailingBlankLines helper and the Lifecycle.Save /
// Transcript integration.
package backend

import (
	"context"
	"strings"
	"testing"
	"time"
)

// TestTrimTrailingBlankLines_TrimsSingleNewline is the simplest
// case from the changelog line: an assistant reply that ends in a
// single "\n" should lose that newline before being persisted, so
// the on-disk steps row does not carry trailing whitespace into
// every subsequent diff.
func TestTrimTrailingBlankLines_TrimsSingleNewline(t *testing.T) {
	got := TrimTrailingBlankLines("Hello there.\n")
	if got != "Hello there." {
		t.Fatalf("TrimTrailingBlankLines(Hello there.\\n) = %q, want %q", got, "Hello there.")
	}
}

// TestTrimTrailingBlankLines_TrimsMultipleBlankLines covers the
// "trailing blank lines from streamed assistant responses" wording
// in the changelog line. A model reply that ends in a paragraph
// break (`\n\n`) before the next thinking/turn boundary should drop
// both newlines so the conversation DB does not grow an empty row
// per turn.
func TestTrimTrailingBlankLines_TrimsMultipleBlankLines(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"two newlines", "Hello world.\n\n", "Hello world."},
		{"three newlines", "Hello world.\n\n\n", "Hello world."},
		{"four newlines", "Hello world.\n\n\n\n", "Hello world."},
		{"mixed trailing whitespace", "Hello world. \t\n", "Hello world."},
		{"blank lines with internal whitespace", "Hello world.\n \n\t\n", "Hello world."},
		{"carriage returns too", "Hello world.\r\n\r\n", "Hello world."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := TrimTrailingBlankLines(tc.in)
			if got != tc.want {
				t.Fatalf("TrimTrailingBlankLines(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// TestTrimTrailingBlankLines_PreservesInternalBlanks is the safety
// property that lets us use this helper confidently: paragraph
// breaks that the model itself emitted (mid-string blank lines,
// e.g. between two paragraphs of a code-fenced reply) must NOT be
// collapsed by the trimmer. If this test fails, the helper has
// regressed into a "trim any whitespace" routine and will
// mutilate the response.
func TestTrimTrailingBlankLines_PreservesInternalBlanks(t *testing.T) {
	in := "First paragraph.\n\nSecond paragraph.\n\nThird.\n\n"
	want := "First paragraph.\n\nSecond paragraph.\n\nThird."
	got := TrimTrailingBlankLines(in)
	if got != want {
		t.Fatalf("TrimTrailingBlankLines(%q) = %q, want %q", in, got, want)
	}
}

// TestTrimTrailingBlankLines_NoTrailingWhitespaceAdded is the
// round-trip safety check: input that ALREADY has no trailing
// whitespace must come out identical, byte-for-byte. The lib
// cannot introduce a whitespace run where none existed.
func TestTrimTrailingBlankLines_NoTrailingWhitespaceAdded(t *testing.T) {
	cases := []string{
		"",
		"abc",
		"abc\ndef",
		"abc\n\ndef",
		"abc\n\n def\n\n  ghi\n",
		"a",
		"a\n",
	}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			got := TrimTrailingBlankLines(in)
			if hasTrailingWhitespace(got) {
				t.Fatalf("TrimTrailingBlankLines(%q) = %q (has trailing whitespace)", in, got)
			}
			// And for inputs that already had no trailing whitespace, the
			// output must equal the input verbatim.
			if !hasTrailingWhitespace(in) && got != in {
				t.Fatalf("TrimTrailingBlankLines(%q) = %q, want unchanged", in, got)
			}
		})
	}
}

// TestTrimTrailingBlankLines_StableForDiffs exercises the "Diff
// between transcripts clean" half of the changelog scope: two
// transcripts that differ only in trailing whitespace (the typical
// race when a streaming response finalises a few microseconds
// before the Save call vs a few microseconds after) must compare
// equal once trimmed. Without the helper, diffs between successive
// runs of the same chat would light up with phantom "\n+"/"-\n"
// lines.
func TestTrimTrailingBlankLines_StableForDiffs(t *testing.T) {
	// Same reply text produced by two consecutive streamings. The
	// first finished with the model-quiet signal received before
	// the trailing "\n\n" chunk arrived; the second finished after
	// it did. Whitespace-equivalent under TrimTrailingBlankLines.
	a := "The quick brown fox.\n\n"
	b := "The quick brown fox.\n\n\n\n"
	c := "The quick brown fox."

	gotA := TrimTrailingBlankLines(a)
	gotB := TrimTrailingBlankLines(b)
	gotC := TrimTrailingBlankLines(c)
	if gotA != gotC {
		t.Fatalf("trim differs between %q and %q: got %q vs %q", a, c, gotA, gotC)
	}
	if gotB != gotC {
		t.Fatalf("trim differs between %q and %q: got %q vs %q", b, c, gotB, gotC)
	}
}

// TestTrimTrailingBlankLines_TrimsWholeWhitespace is the
// degenerate edge case: if the entire string is whitespace (which
// can happen if a stream errors out after emitting only "\n\n"),
// the trimmer returns the empty string rather than a non-empty
// run of "\n" the consumer has to handle specially.
func TestTrimTrailingBlankLines_TrimsWholeWhitespace(t *testing.T) {
	cases := []string{"\n", "\n\n", " \t\n", "\r\n\r\n", "\n  \n\n\t"}
	for _, in := range cases {
		t.Run(in, func(t *testing.T) {
			got := TrimTrailingBlankLines(in)
			if got != "" {
				t.Fatalf("TrimTrailingBlankLines(%q) = %q, want empty", in, got)
			}
		})
	}
}

// whitespaceClient is a tiny inline Client that emits a fixed
// text string split into small chunks, so the integration test
// can assert on the Lifecycle's transcript handling without
// depending on the mock fixture's reply shape (which is fixed at
// `NewMock()` time).
type whitespaceClient struct {
	text string
}

func (w *whitespaceClient) Stream(ctx context.Context, _ StreamRequest) (<-chan StreamChunk, error) {
	const chunkSize = 4
	out := make(chan StreamChunk, 16)
	go func() {
		defer close(out)
		for i := 0; i < len(w.text); i += chunkSize {
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Millisecond):
			}
			end := i + chunkSize
			if end > len(w.text) {
				end = len(w.text)
			}
			out <- StreamChunk{Text: w.text[i:end]}
		}
		out <- StreamChunk{Done: true}
	}()
	return out, nil
}

// captureSaver is a Saver that stashes the transcript the
// Lifecycle hands to Save() into its `captured` field for the
// test to assert on.
type captureSaver struct {
	captured string
}

func (c *captureSaver) Save(_ string, txt string) error {
	c.captured = txt
	return nil
}

// TestLifecycle_TranscriptStripsTrailingBlanks exercises the
// integration: a streamed run that emits trailing whitespace in
// its final chunk must surface a trimmed transcript through both
// Lifecycle.Transcript() and Lifecycle.Save(). Without the
// helper, raw whitespace leaked through; with it, both accessors
// produce the same clean string.
func TestLifecycle_TranscriptStripsTrailingBlanks(t *testing.T) {
	// Note the trailing "\n\n\n" — exactly the pattern the AGY
	// 1.2.10 changelog line fixes. The chunked stream emits this
	// across the last three StreamChunk.Text values, so by the
	// time the done marker arrives l.transcript ends in "\n\n\n".
	client := &whitespaceClient{text: "The answer.\n\n\n"}
	saver := &captureSaver{}
	lc, err := NewLifecycle(LifecycleConfig{
		Client:       client,
		Sink:         NewTextSink(nil),
		DefaultModel: "m",
		Saver:        saver,
	})
	if err != nil {
		t.Fatalf("NewLifecycle: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := lc.Init(ctx, "m"); err != nil {
		t.Fatalf("Init: %v", err)
	}
	if err := lc.Run(ctx, "q"); err != nil {
		t.Fatalf("Run: %v", err)
	}

	got := lc.Transcript()
	if got != "The answer." {
		t.Fatalf("Transcript() = %q, want %q (no trailing whitespace)", got, "The answer.")
	}

	if err := lc.Save(); err != nil {
		t.Fatalf("Save: %v", err)
	}
	if saver.captured != "The answer." {
		t.Fatalf("saver received %q, want %q (trailing whitespace leak)", saver.captured, "The answer.")
	}
}

// hasTrailingWhitespace reports whether s ends in any ASCII
// whitespace byte (space, tab, newline, carriage-return). Used by
// TestTrimTrailingBlankLines_NoTrailingWhitespaceAdded so the
// test does not depend on the trim's exact behaviour for inputs
// that had no trailing whitespace in the first place.
func hasTrailingWhitespace(s string) bool {
	if s == "" {
		return false
	}
	return strings.IndexByte(" \t\n\r", s[len(s)-1]) >= 0
}
