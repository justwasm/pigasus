package tools

import (
	"encoding/json"
	"fmt"
	"strings"
)

// truncateYAML walks the first N lines that fit within maxTokens.
// The output is a YAML doc with _truncated and _preview keys:
//
//	_truncated: true
//	_preview: |
//	  <first N lines of original>
//
// The block-scalar (`|`) preserves the original lines verbatim
// without escaping YAML metacharacters. The wrapped output is
// still parseable YAML.
func truncateYAML(content string, maxTokens int) string {
	lines := strings.Split(content, "\n")
	var preview strings.Builder
	used := estimateTokens("_truncated: true\n_preview: |\n")
	first := true
	for _, line := range lines {
		entry := "  " + line + "\n"
		needed := estimateTokens(entry)
		if used+needed > maxTokens {
			break
		}
		preview.WriteString(entry)
		used += needed
		first = false
	}
	if first {
		preview.WriteString("\n")
	}
	return "_truncated: true\n_preview: |\n" + preview.String()
}

// truncateXML wraps the first N tokens of content in a <truncated> tag.
func truncateXML(content string, maxTokens int) string {
	maxBytes := maxTokens * DefaultApproxCharsPerToken
	if maxBytes >= len(content) {
		return content
	}
	cut := maxBytes
	for cut > 0 && !isRuneStart(content[cut:]) {
		cut--
	}
	for cut > 0 {
		c := content[cut-1]
		if c == ' ' || c == '\n' || c == '\t' || c == '<' {
			break
		}
		cut--
	}
	return "<truncated>" + content[:cut] + "</truncated>"
}

// truncateProse is the unknown / unparseable fallback.
func truncateProse(content string, maxTokens int) string {
	maxBytes := maxTokens * DefaultApproxCharsPerToken
	if maxBytes >= len(content) {
		return content
	}
	cut := maxBytes
	for cut > 0 && !isRuneStart(content[cut:]) {
		cut--
	}
	for cut > 0 {
		c := content[cut-1]
		if c == ' ' || c == '\n' || c == '\t' {
			break
		}
		cut--
	}
	return content[:cut] + "\n…[truncated]"
}

// encodeTruncatedEnvelope wraps a partial JSON-object preview
// string in the {"_truncated": true, "_preview": "..."} envelope.
func encodeTruncatedEnvelope(preview string) string {
	out := struct {
		Truncated bool   `json:"_truncated"`
		Preview   string `json:"_preview"`
	}{Truncated: true, Preview: preview}
	encoded, err := json.Marshal(out)
	if err != nil {
		return fmt.Sprintf(`{"_truncated":true,"_preview":%q}`, preview)
	}
	return string(encoded)
}

// encodeTruncatedArrayEnvelope wraps a slice of JSON element fragments
// in the {"_truncated": true, "_preview": [...]} envelope.
func encodeTruncatedArrayEnvelope(elements []json.RawMessage) string {
	out := struct {
		Truncated bool              `json:"_truncated"`
		Preview   []json.RawMessage `json:"_preview"`
	}{Truncated: true, Preview: elements}
	encoded, err := json.Marshal(out)
	if err != nil {
		return fmt.Sprintf(`{"_truncated":true,"_preview":[]}`)
	}
	return string(encoded)
}

// isRuneStart reports whether the byte at position i is the start of a UTF-8 rune.
func isRuneStart(s string) bool {
	if len(s) == 0 {
		return true
	}
	return s[0]&0xC0 != 0x80
}
