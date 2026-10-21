// estimate.go — cheap byte-counting token estimator for the cycle-237
// /compact command.
//
// The reference Antigravity binary reports the compact reduction as
// "X messages · context -Y%". The clone matches the contract but
// substitutes a /4 byte-count approximation for the upstream tokeniser:
//
//	EstimateTokens(s) ≈ len([]byte(s)) / 4
//
// Reasoning:
//
//   - tiktoken / sentencepiece-grade tokenisers are not vendored and
//     would pull a heavy dependency into the binary for what is a
//     pre-flight stat the user reads once.
//   - The /4 byte heuristic is the same one cycle-235's mockClient
//     uses for prompt / completion token counts (mockClient.Stream
//     reports CompletionTokens = len(reply)/4), so the number the
//     transcript echoes stays in the same units the rest of the
//     binary reports.
//
// Accuracy is +/- 25 % on mixed-content English text; it is *good
// enough* for the "context -Y%" line, which the user reads as a
// relative reduction (and where the +/- 25 % does not change whether
// the headline reads "-30%" or "-50%"). Replace with a tiktoken
// wrapper when the wire cost of the inaccuracy shows up in user
// reports.
package compaction

// EstimateTokens returns a rough token count for s. The count is
// len([]byte(s))/4 — the cheap approximation cycle-237 ships so the
// /compact transcript can echo "-Y%" without pulling a tokeniser
// dependency into the binary.
//
// TODO: replace with tiktoken-grade estimate when used on large
// transcripts.
func EstimateTokens(s string) int {
	if len(s) == 0 {
		return 0
	}
	return len([]byte(s)) / 4
}
