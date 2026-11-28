package wirelog

import (
	"regexp"
	"strconv"
)

var (
	// Gemini patterns
	rePromptGemini = regexp.MustCompile(`"promptTokenCount"\s*:\s*([0-9]+)`)
	reCandGemini   = regexp.MustCompile(`"candidatesTokenCount"\s*:\s*([0-9]+)`)
	reTotalGemini  = regexp.MustCompile(`"totalTokenCount"\s*:\s*([0-9]+)`)

	// OpenAI / Anthropic patterns
	rePromptOAI = regexp.MustCompile(`"(?:prompt_tokens|input_tokens)"\s*:\s*([0-9]+)`)
	reComplOAI  = regexp.MustCompile(`"(?:completion_tokens|output_tokens)"\s*:\s*([0-9]+)`)
	reTotalOAI  = regexp.MustCompile(`"total_tokens"\s*:\s*([0-9]+)`)
)

// ExtractUsage scans response body text for model token usage metadata.
// It supports Gemini (promptTokenCount / candidatesTokenCount),
// OpenAI (prompt_tokens / completion_tokens), and Anthropic (input_tokens / output_tokens).
// When multiple chunks report usage (e.g. streaming SSE), the last occurrence
// is taken since it represents the final total.
func ExtractUsage(body string) (promptTokens, completionTokens, totalTokens int, found bool) {
	if len(body) == 0 {
		return 0, 0, 0, false
	}
	// Limit search window to the trailing 64 KiB where usage metadata resides.
	window := body
	if len(window) > 64<<10 {
		window = window[len(window)-64<<10:]
	}

	p, okP := lastIntMatch(rePromptGemini, window)
	c, okC := lastIntMatch(reCandGemini, window)
	tot, okTot := lastIntMatch(reTotalGemini, window)

	if !okP && !okC {
		p, okP = lastIntMatch(rePromptOAI, window)
		c, okC = lastIntMatch(reComplOAI, window)
		tot, okTot = lastIntMatch(reTotalOAI, window)
	}

	if !okP && !okC && !okTot {
		return 0, 0, 0, false
	}

	if !okTot && (okP || okC) {
		tot = p + c
	}
	return p, c, tot, true
}

func lastIntMatch(re *regexp.Regexp, s string) (int, bool) {
	matches := re.FindAllStringSubmatch(s, -1)
	if len(matches) == 0 {
		return 0, false
	}
	last := matches[len(matches)-1]
	if len(last) < 2 {
		return 0, false
	}
	val, err := strconv.Atoi(last[1])
	if err != nil {
		return 0, false
	}
	return val, true
}
