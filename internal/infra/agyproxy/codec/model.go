package codec

import "strings"

// NormalizeModelName maps user-friendly names or aliases to CloudCode wire IDs.
func NormalizeModelName(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "gemini-3.8-flash-high"
	}
	low := strings.ToLower(raw)
	for _, prefix := range []string{"antigravity/", "google/", "anthropic/", "openai/", "models/"} {
		if strings.HasPrefix(low, prefix) {
			low = strings.TrimPrefix(low, prefix)
		}
	}
	switch {
	case strings.Contains(low, "3.1-pro") || strings.Contains(low, "3-1-pro") || strings.Contains(low, "gemini-pro"):
		if strings.Contains(low, "low") {
			return "gemini-3.1-pro-low"
		}
		return "gemini-3.1-pro-high"
	case strings.Contains(low, "2.5-pro") || strings.Contains(low, "2-5-pro"):
		return "gemini-2.5-pro"
	case strings.Contains(low, "2.5-flash") || strings.Contains(low, "2-5-flash"):
		return "gemini-2.5-flash"
	case strings.Contains(low, "flash-medium"):
		return "gemini-3.8-flash-medium"
	case strings.Contains(low, "flash-low"):
		return "gemini-3.8-flash-low"
	case strings.Contains(low, "flash") || strings.Contains(low, "gemini"):
		return "gemini-3.8-flash-high"
	case strings.Contains(low, "opus"):
		return "claude-opus-4-6-thinking"
	case strings.Contains(low, "sonnet"):
		return "claude-sonnet-4-6"
	case strings.Contains(low, "gpt-oss") || strings.Contains(low, "gpt_oss"):
		return "gpt-oss-120b-medium"
	case strings.Contains(low, "gpt") || strings.Contains(low, "sol") || strings.Contains(low, "luna") || strings.Contains(low, "terra") || strings.Contains(low, "astra") || strings.Contains(low, "codex") || strings.HasPrefix(low, "o1") || strings.HasPrefix(low, "o3") || strings.HasPrefix(low, "o4") || strings.HasPrefix(low, "chatgpt"):
		return "gemini-3.8-flash-high"
	default:
		return raw
	}
}