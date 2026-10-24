// summarize.go — the cycle-237 /compact summarizer.
//
// ComputeSummary takes a transcript (sess), wraps it in the
// SummarizePrompt, calls the model via backend.Client, and returns
// the model's reply as a Summary. The call is synchronous from the
// caller's point of view; the slash command runs it in a goroutine
// (cmd/wagyu/slash_compact.go) and posts the result back to the
// bubbletea runtime as a tea.Msg.
//
// The package deliberately avoids the PiG / coding.Session path that
// cycle-229's manualCompactor exposes — AGY 1.4.2's /compact
// works against the same backend as the regular turns, and a
// standalone summarizer keeps the slash command testable without
// wiring a *pigrt.Runtime.
//
// The package does not mutate the input session. When the context
// fires cancellation mid-call, ComputeSummary returns
// (Summary{}, ErrCancelled) and the caller (the TUI slash command)
// is responsible for keeping m.session intact — the regression
// test TestSlashCompact_CancelRestoresSession pins this contract.
package compaction

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/backend"
)

// Stats is the pre/post transcript statistics ComputeSummary reports
// alongside the summary text. The TUI's post-compact echo formats
// these as "X messages · context -Y% (M → K tokens)".
//
// Messages is the wire count (one Message per assistant / user /
// tool turn). Tokens is the byte-count / 4 approximation — see
// estimate.go for the rationale.
type Stats struct {
	Messages int
	Tokens   int
}

// Summary is the result of one summarization call. The TUI replaces
// m.session with one backend.Message carrying Text as Content when
// ComputeSummary returns successfully; StatsBefore / StatsAfter
// drive the "-Y%" line.
type Summary struct {
	// Text is the model's summary. Replaces the transcript in
	// m.session as a single user-role message (so the next model
	// turn sees the summary in its history).
	Text string
	// InputTokens is the model-reported prompt-token count of the
	// compaction request — the context size at the moment of the
	// compaction. The reference's stats row prints this as the
	// number before the arrow ("context 13.1k → …"), because its
	// checkpoint config sets use_model_reported_token_count.
	//
	// Zero when the backend does not report usage; callers fall
	// back to the transcript estimate.
	InputTokens int
	// OutputTokens is the model-reported completion-token count of
	// the compaction request: the size of the summary itself, which
	// the reference prints in parentheses ("(266 summary)").
	//
	// Zero when the backend does not report usage.
	OutputTokens int
	// StatsBefore is the transcript stats the user sees on the
	// "Compacted: …" line. Always populated, even on cancellation
	// (the caller's "Nothing to compact" / "Cancelled" paths need
	// the count).
	StatsBefore Stats
	// StatsAfter is the post-compact stats; on cancellation or
	// model failure it is zero (the caller can detect that and
	// omit the "-Y%" reduction).
	StatsAfter Stats
}

// ErrCancelled is the sentinel ComputeSummary returns when ctx is
// cancelled mid-call. Callers should check errors.Is — the typed
// error path is what lets the TUI distinguish a user-pressed Esc
// from a backend failure.
var ErrCancelled = errors.New("compaction: cancelled")

// ErrEmptyTranscript is the sentinel ComputeSummary returns when
// sess is empty. The TUI uses this to echo "Nothing to compact
// yet." without calling the model.
var ErrEmptyTranscript = errors.New("compaction: empty transcript")

// ComputeSummary calls client with the joined transcript and the
// reference's continuation-summary instruction, and returns the model's
// reply as Summary.Text; StatsBefore / StatsAfter drive the reduction
// headline.
//
// systemPrompt is the request's system prompt — the caller passes the
// *normal* Antigravity instructions, because the reference keeps them
// there and appends the summary instruction as the last user turn
// instead of replacing the system prompt.
//
// Cancellation contract:
//
//   - ctx pre-cancelled before the call: ErrEmptyTranscript /
//     ErrCancelled are returned as the error; no model call fires.
//   - ctx cancelled mid-stream: the call drains the in-flight
//     chunks (model.Client.Stream is expected to surface the
//     cancellation as a chunk with Error == ctx.Err() and Done ==
//     true) and returns ErrCancelled.
//
// The original transcript is preserved on the caller side; this
// function never mutates sess.
func ComputeSummary(ctx context.Context, client backend.Client, model, systemPrompt string, sess []backend.Message) (Summary, error) {
	before := Stats{
		Messages: len(sess),
		Tokens:   TranscriptTokens(sess),
	}
	if len(sess) == 0 {
		return Summary{StatsBefore: before, StatsAfter: before}, ErrEmptyTranscript
	}
	if client == nil {
		return Summary{StatsBefore: before}, errors.New("compaction: nil backend client")
	}
	if model == "" {
		model = "claude-sonnet-4-6"
	}
	req := buildSummarizeRequest(model, systemPrompt, sess)

	chunks, err := client.Stream(ctx, req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return Summary{StatsBefore: before}, ErrCancelled
		}
		return Summary{StatsBefore: before}, fmt.Errorf("compaction: stream: %w", err)
	}
	if chunks == nil {
		return Summary{StatsBefore: before}, errors.New("compaction: nil channel")
	}

	text, inputTokens, outputTokens, err := consumeSummaryChunks(ctx, chunks)
	if err != nil {
		statsAfter := Stats{Tokens: EstimateTokens(text)}
		return Summary{StatsBefore: before, StatsAfter: statsAfter}, err
	}
	summary := strings.TrimSpace(text)
	after := Stats{Messages: 1, Tokens: EstimateTokens(summary)}
	return Summary{
		Text:         summary,
		StatsBefore:  before,
		StatsAfter:   after,
		InputTokens:  inputTokens,
		OutputTokens: outputTokens,
	}, nil
}

func buildSummarizeRequest(model, systemPrompt string, sess []backend.Message) backend.StreamRequest {
	return backend.StreamRequest{
		Model:        model,
		SystemPrompt: systemPrompt,
		Messages: []backend.Message{
			{Role: "user", Content: transcriptText(sess)},
			{Role: "user", Content: SummarizePrompt + OnDemandCompactionSuffix},
		},
	}
}

func consumeSummaryChunks(ctx context.Context, chunks <-chan backend.StreamChunk) (string, int, int, error) {
	var b strings.Builder
	var inputTokens, outputTokens int
	for chunk := range chunks {
		if chunk.Error != nil {
			if errors.Is(chunk.Error, context.Canceled) || errors.Is(chunk.Error, context.DeadlineExceeded) {
				return b.String(), inputTokens, outputTokens, ErrCancelled
			}
			return b.String(), inputTokens, outputTokens, fmt.Errorf("compaction: chunk error: %w", chunk.Error)
		}
		b.WriteString(chunk.Text)
		if chunk.Meta != nil {
			if chunk.Meta.PromptTokens > 0 {
				inputTokens = chunk.Meta.PromptTokens
			}
			if chunk.Meta.CompletionTokens > 0 {
				outputTokens = chunk.Meta.CompletionTokens
			}
		}
		if chunk.Done {
			break
		}
		if ctx.Err() != nil {
			return b.String(), inputTokens, outputTokens, ErrCancelled
		}
	}
	return b.String(), inputTokens, outputTokens, nil
}

// ReductionPercent returns the integer percentage reduction from
// before to after, rounded to the nearest whole percent. Returns 0
// when either side is zero (the caller suppresses "-0%" lines).
func ReductionPercent(before, after Stats) int {
	if before.Tokens <= 0 || after.Tokens <= 0 {
		return 0
	}
	drop := before.Tokens - after.Tokens
	if drop <= 0 {
		return 0
	}
	pct := (drop * 100) / before.Tokens
	return pct
}

// transcriptText joins sess into one big "USER: …\nASSISTANT: …"
// block the summarizer prompt can chew on. Tool calls and tool
// results are flattened to "<name>: <args>" / "<name>: <result>"
// so the model sees something human-readable.
//
// The header is a soft preamble: the model's instruction-following
// usually does not need it, but having a marker per turn helps the
// model avoid merging the user's last message into the summary's
// first bullet.
func transcriptText(sess []backend.Message) string {
	if len(sess) == 0 {
		return ""
	}
	var b strings.Builder
	b.WriteString("Transcript to summarize:\n\n")
	for i, m := range sess {
		switch m.Role {
		case "user":
			fmt.Fprintf(&b, "USER: %s\n", strings.TrimSpace(m.Content))
		case "assistant":
			fmt.Fprintf(&b, "ASSISTANT: %s\n", strings.TrimSpace(m.Content))
			for _, tc := range m.ToolCalls {
				fmt.Fprintf(&b, "TOOL_CALL %s: %s\n", tc.Name, strings.TrimSpace(string(tc.Arguments)))
			}
		case "tool":
			fmt.Fprintf(&b, "TOOL_RESULT %s: %s\n", m.Name, strings.TrimSpace(m.Content))
		default:
			fmt.Fprintf(&b, "%s: %s\n", strings.ToUpper(m.Role), strings.TrimSpace(m.Content))
		}
		if i != len(sess)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// TranscriptTokens is the byte-count sum across every message in sess,
// divided by the reference's three-bytes-per-token divisor. Cheaper
// than calling EstimateTokens on the joined transcript (avoids one
// allocation), and keeps the bytes-typed estimate.go helper out of this
// file.
func TranscriptTokens(sess []backend.Message) int {
	if len(sess) == 0 {
		return 0
	}
	total := 0
	for _, m := range sess {
		total += len(m.Content)
		for _, tc := range m.ToolCalls {
			total += len(tc.Arguments)
		}
	}
	return total / BytesPerToken
}
