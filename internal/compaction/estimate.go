// estimate.go — cheap byte-counting token estimator for the /compact
// command.
//
// The reference Antigravity binary reports the compaction context as a
// token count ("context 13.1k → ~17.9k"). Its estimator is
// jetski/tokenizer's EstimateTokenCounter, whose divisor is three bytes
// per token — decompiled from agy 1.4.5, createDefaultTokenCounter stores
// the double 0x4008000000000000 (3.0) and GetTokenCount computes
// int(float64(len) / divisor).
//
// The clone matches that divisor:
//
//	EstimateTokens(s) ≈ len([]byte(s)) / 3
//
// Reasoning:
//
//   - tiktoken / sentencepiece-grade tokenisers are not vendored and
//     would pull a heavy dependency into the binary for what is a
//     pre-flight stat the user reads once.
//   - Three bytes per token is the divisor the reference itself uses
//     (the same estimator its /context panel and its compaction stats
//     row are built on), so the numbers the clone prints are on the
//     reference's scale.
//
// Accuracy is +/- 25 % on mixed-content English text; it is *good
// enough* for the "context -Y%" line, which the user reads as a
// relative reduction (and where the +/- 25 % does not change whether
// the headline reads "-30%" or "-50%"). Replace with a tiktoken
// wrapper when the wire cost of the inaccuracy shows up in user
// reports.
package compaction

// EstimateTokens returns a rough token count for s. The count is
// len([]byte(s))/3 — the reference's own divisor (see the file comment).
//
// TODO: replace with tiktoken-grade estimate when used on large
// transcripts.
func EstimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return len([]byte(s)) / BytesPerToken
}
