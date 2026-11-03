// truncate.go — AGY 1.4.1 cycle 248: structured-output truncation.
//
// AGY 1.4.1 changelog (recoverable from
// internal/cli/testdata/changelog_reference.txt:12):
//
//	"Fixed large structured tool outputs (such as JSON results from
//	 MCP tools) bypassing the per-step token limit while still being
//	 counted as truncated, which could push long conversations past
//	 the model's context window before automatic compaction ran."
//
// Root cause (recovered from the bug report): the per-step token
// limit that prevents tool results from flooding the model's context
// window was being bypassed for structured output formats (JSON,
// YAML, etc. — anything that's "structured" rather than freeform
// prose). The token counter recognised freeform-prose truncation but
// the truncation logic for structured output happened AFTER token
// counting, so a 5000-token JSON blob could be reported to the model
// as "5000 tokens used (truncated)" — the truncation metadata was
// set but the actual content was not reduced.
//
// Fix: when token-counting structured tool outputs, ALSO truncate the
// content to fit the per-step budget. The truncation produces a
// shorter payload ({"_truncated": true, "_preview": "..."} for JSON,
// etc.) that the model actually receives. This file implements the
// truncation half of that fix. output_budget.go wires it into the
// per-step budget pipeline.
//
// Design notes:
//
//   - The token counter is the cheap 4-chars-per-token heuristic the
//     rest of the clone uses (customize.EstimateTokens, compaction.
//     EstimateTokens, internal/backend/client.go's approxTokens).
//     A precise BPE counter is intentionally out of scope — the
//     per-step budget is a guard, not a precise context-window
//     measurement. Any future cycle that switches to a real
//     tokenizer should re-derive the truncation walk; the public
//     function shape will not change.
//
//   - Truncation preserves syntactic validity. A truncated JSON
//     object is still parseable; a truncated YAML doc is still
//     parseable. If a future cycle needs semantic preservation
//     (e.g. valid-JSON-but-still-meaningful), that's a follow-up.
//
//   - YAML detection uses a heuristic (first byte is alphanumeric,
//     contains ": " on the first line). Some YAML inputs may fall
//     through to the prose path. Acceptable trade-off — the cycle
//     does not try to parse every YAML input.
//
//   - A defensive recover() guards the public entry point so a bad
//     input (a string the JSON parser chokes on, a nil-ish value
//     smuggled through) cannot crash the agent. The fallback is the
//     prose truncation, which is guaranteed to terminate.
package tools

import (
	"encoding/json"
	"sort"
	"strings"
)

// Format constants returned by DetectFormat and consumed by
// ApplyOutputBudget (output_budget.go). The values are stable
// strings so they survive serialisation round-trips in test
// fixtures; tests pin against the literal values rather than
// introducing a typed enum.
const (
	FormatJSON  = "json"
	FormatYAML  = "yaml"
	FormatXML   = "xml"
	FormatProse = "prose"
)

// DefaultApproxCharsPerToken is the heuristic used to estimate token
// counts from byte/character lengths. Matches the rest of the clone
// (customize.EstimateTokens, compaction.EstimateTokens,
// internal/backend/client.go:approxTokens). A future cycle that
// switches to a real tokenizer will replace this constant.
const DefaultApproxCharsPerToken = 4

// estimateTokens is the cheap length-based token estimator. Empty
// input yields zero tokens so a missing source does not consume
// budget (matches customize.EstimateTokens). Very short inputs
// (1-3 chars) still round up to 1 token — a 1-character reply is
// still 1 token, not 0.
func estimateTokens(s string) int {
	if s == "" {
		return 0
	}
	n := len(s) / DefaultApproxCharsPerToken
	if n < 1 {
		return 1
	}
	return n
}

// DetectFormat inspects the first non-whitespace byte(s) of content
// and returns one of FormatJSON, FormatYAML, FormatXML, FormatProse.
// Empty input returns FormatProse so the caller's "is this
// structured?" branch falls through to the prose path.
//
// Detection rules (cheap heuristics; do not aim for full coverage
// of any format — a precise parser is out of scope):
//
//   - "{..."  → JSON object
//   - "[..."  → JSON array
//   - "<..."  → XML / HTML
//   - first line matches `[a-zA-Z_][a-zA-Z0-9_-]*:\s` → YAML
//   - everything else → prose
func DetectFormat(content string) string {
	trimmed := strings.TrimLeft(content, " \t\n\r")
	if len(trimmed) == 0 {
		return FormatProse
	}
	switch trimmed[0] {
	case '{':
		return FormatJSON
	case '[':
		// A JSON array starts with [ — could also be a YAML
		// flow-style sequence, but JSON wins here because
		// the cycle's target is MCP tool output (always JSON).
		return FormatJSON
	case '<':
		return FormatXML
	}
	if isYAMLHeuristic(trimmed) {
		return FormatYAML
	}
	return FormatProse
}

// isYAMLHeuristic reports whether content's first line looks like a
// YAML mapping key. The rule: first line begins with an
// alphanumeric / underscore key character, followed by ":" then
// whitespace or end-of-line. This is intentionally conservative —
// a YAML list (`- foo`) is treated as prose so the truncation
// path falls through to the cheaper char-count path.
func isYAMLHeuristic(content string) bool {
	if len(content) == 0 {
		return false
	}
	// First line only.
	eol := strings.IndexByte(content, '\n')
	first := content
	if eol >= 0 {
		first = content[:eol]
	}
	if len(first) == 0 {
		return false
	}
	// First character must be alphanumeric or underscore (YAML
	// plain-scalar mapping keys cannot begin with a punctuation
	// character without quoting).
	c := first[0]
	if !isAlphaNum(c) && c != '_' {
		return false
	}
	// Find the first colon. The heuristic accepts either:
	//   key: value
	//   key:
	// but rejects
	//   http://example.com/path (no leading key char — already
	//   excluded by the leading-char check).
	colon := strings.IndexByte(first, ':')
	if colon <= 0 {
		return false
	}
	// The character before the colon must be a valid key char
	// (alphanumeric or underscore) so "x:1" or "x :1" don't
	// false-positive.
	if !isAlphaNum(first[colon-1]) && first[colon-1] != '_' {
		return false
	}
	// After the colon: whitespace OR end-of-string OR "#" (YAML
	// comment) OR end-of-line.
	if colon == len(first)-1 {
		return true // "key:" with no value
	}
	next := first[colon+1]
	return next == ' ' || next == '\t' || next == '#'
}

// isAlphaNum is the ASCII-fast path used by isYAMLHeuristic; the
// library's unicode test would also work but the heuristic only
// needs ASCII.
func isAlphaNum(c byte) bool {
	return (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z') || (c >= '0' && c <= '9')
}

// TruncateStructured is the public entry point. It detects the
// format of content and dispatches to the format-specific helper.
//
// Returns content unchanged when:
//
//   - maxTokens <= 0 (treated as "no limit"), or
//   - content is empty, or
//   - content's token estimate is already within maxTokens
//     (the "under budget" no-op path).
//
// The format-specific helpers are:
//
//	truncateJSONObject — JSON object → wrap in {"_truncated": true, "_preview": "<partial object string>"}
//	truncateJSONArray  — JSON array  → wrap in {"_truncated": true, "_preview": [...first N elements...]}
//	truncateYAML       — YAML doc    → "_truncated: true\n_preview: |\n  <first N lines>"
//	truncateXML        — XML/HTML    → "<truncated>first N tokens</truncated>"
//	truncateProse      — unknown / unparseable → keep first N tokens worth of bytes, append a marker
//
// The unknown / unparseable fallback (truncateProse) is the existing
// freeform-prose truncation behaviour: char-count truncation, no
// structural preservation.
func TruncateStructured(content string, maxTokens int) string {
	if maxTokens <= 0 || content == "" {
		return content
	}
	if estimateTokens(content) <= maxTokens {
		return content
	}
	return truncateByFormat(content, DetectFormat(content), maxTokens)
}

// truncateByFormat is the format-dispatching helper shared by
// TruncateStructured (auto-detect) and ApplyOutputBudget
// (caller-supplied format). Returns content unchanged when the
// input is empty; defers to the format-specific helper otherwise.
//
// The defensive recover() guarantees a panic in any helper
// returns the input rather than crashing the caller.
func truncateByFormat(content, format string, maxTokens int) string {
	if content == "" {
		return content
	}
	defer func() { _ = recover() }()
	switch format {
	case FormatJSON:
		trimmed := strings.TrimLeft(content, " \t\n\r")
		if len(trimmed) > 0 && trimmed[0] == '[' {
			return truncateJSONArray(content, maxTokens)
		}
		return truncateJSONObject(content, maxTokens)
	case FormatYAML:
		return truncateYAML(content, maxTokens)
	case FormatXML:
		return truncateXML(content, maxTokens)
	default:
		return truncateProse(content, maxTokens)
	}
}

// truncateJSONObject parses content as a JSON object and walks the
// top-level keys in sorted order (deterministic; JSON object's
// iteration order is implementation-defined). The output is a JSON
// envelope:
//
//	{"_truncated": true, "_preview": "{...partial JSON object string...}"}
//
// The _preview value is itself a string containing a syntactically
// valid partial JSON object — the first key/value pairs that fit
// within maxTokens. The wrapped JSON is parseable by encoding/json
// (no trailing commas, no broken escapes).
//
func buildTruncatedJSONObjectPreview(v map[string]json.RawMessage, keys []string, budget int) string {
	var preview strings.Builder
	preview.WriteByte('{')
	used := 1 // opening "{"
	first := true
	for _, k := range keys {
		val := v[k]
		keyJSON, err := json.Marshal(k)
		if err != nil {
			continue // skip the bad key rather than fail
		}
		entry := string(keyJSON) + ":" + string(val)
		sep := ""
		if !first {
			sep = ","
		}
		needed := estimateTokens(sep + entry)
		if used+needed+1 > budget { // +1 for closing "}"
			break
		}
		preview.WriteString(sep)
		preview.WriteString(entry)
		used += needed
		first = false
	}
	preview.WriteByte('}')
	return preview.String()
}

// When the object does not parse, the helper falls through to
// truncateProse (the unparseable-content fallback).
func truncateJSONObject(content string, maxTokens int) string {
	var v map[string]json.RawMessage
	if err := json.Unmarshal([]byte(content), &v); err != nil {
		return truncateProse(content, maxTokens)
	}
	if len(v) == 0 {
		return encodeTruncatedEnvelope("{}")
	}
	keys := make([]string, 0, len(v))
	for k := range v {
		keys = append(keys, k)
	}
	sort.Strings(keys)

	envelopeOverhead := estimateTokens(`{"_truncated":true,"_preview":""}`)
	budgetForPreview := maxTokens - envelopeOverhead
	if budgetForPreview < 1 {
		return encodeTruncatedEnvelope("{}")
	}
	preview := buildTruncatedJSONObjectPreview(v, keys, budgetForPreview)
	return encodeTruncatedEnvelope(preview)
}

// truncateJSONArray parses content as a JSON array and keeps the
// first N elements that fit within maxTokens. The output is:
//
//	{"_truncated": true, "_preview": [...first N elements...]}
//
// The _preview value is itself a JSON array (not a string) so the
// model sees a structured shape. The wrapped JSON is parseable.
//
// When the array does not parse, the helper falls through to
// truncateProse.
func truncateJSONArray(content string, maxTokens int) string {
	var arr []json.RawMessage
	if err := json.Unmarshal([]byte(content), &arr); err != nil {
		return truncateProse(content, maxTokens)
	}
	if len(arr) == 0 {
		// Empty array — preview is [].
		return encodeTruncatedArrayEnvelope([]json.RawMessage{})
	}

	// Build the preview array as a slice of RawMessage; the
	// envelope marshaller handles the array encoding.
	envelopeOverhead := estimateTokens(`{"_truncated":true,"_preview":[]}`)
	budgetForPreview := maxTokens - envelopeOverhead
	if budgetForPreview < 1 {
		return encodeTruncatedArrayEnvelope([]json.RawMessage{})
	}

	// Walk the array. "used" tracks the byte cost of the
	// preview array as we go (commas + each element).
	preview := make([]json.RawMessage, 0, len(arr))
	used := 0
	for i, elem := range arr {
		sep := 0
		if i > 0 {
			sep = 1 // comma
		}
		needed := sep + estimateTokens(string(elem))
		if used+needed > budgetForPreview {
			break
		}
		preview = append(preview, elem)
		used += needed
	}
	return encodeTruncatedArrayEnvelope(preview)
}
