package stats

import "strings"

// Pricing structure per 1M tokens in USD.
type modelPricing struct {
	promptPerMillion     float64
	completionPerMillion float64
	cacheReadPerMillion  float64
}

// EstimateCost calculates the estimated USD cost for a model invocation.
func EstimateCost(model string, promptTokens, completionTokens, cacheReadTokens int64) float64 {
	pricing := lookupPricing(model)
	cost := (float64(promptTokens) * pricing.promptPerMillion / 1_000_000.0) +
		(float64(completionTokens) * pricing.completionPerMillion / 1_000_000.0)
	if cacheReadTokens > 0 && pricing.cacheReadPerMillion > 0 {
		cost += float64(cacheReadTokens) * pricing.cacheReadPerMillion / 1_000_000.0
	}
	return cost
}

// ResolveProvider infers the AI provider name from the model identifier.
func ResolveProvider(model string) string {
	lower := strings.ToLower(model)
	switch {
	case strings.Contains(lower, "gemini") || strings.Contains(lower, "google"):
		return "Google"
	case strings.Contains(lower, "claude") || strings.Contains(lower, "anthropic"):
		return "Anthropic"
	case strings.Contains(lower, "gpt") || strings.HasPrefix(lower, "o1") || strings.HasPrefix(lower, "o3") || strings.Contains(lower, "openai"):
		return "OpenAI"
	case strings.Contains(lower, "deepseek"):
		return "DeepSeek"
	case strings.Contains(lower, "qwen"):
		return "Qwen"
	case strings.Contains(lower, "mistral") || strings.Contains(lower, "codestral"):
		return "Mistral"
	case strings.Contains(lower, "llama") || strings.Contains(lower, "meta"):
		return "Meta"
	default:
		if i := strings.Index(model, "/"); i > 0 {
			return strings.ToUpper(model[:1]) + model[1:i]
		}
		return "CloudCode"
	}
}

func lookupPricing(model string) modelPricing {
	lower := strings.ToLower(model)
	switch {
	// Anthropic Claude
	case strings.Contains(lower, "opus"):
		return modelPricing{promptPerMillion: 15.00, completionPerMillion: 75.00, cacheReadPerMillion: 1.50}
	case strings.Contains(lower, "sonnet"):
		return modelPricing{promptPerMillion: 3.00, completionPerMillion: 15.00, cacheReadPerMillion: 0.30}
	case strings.Contains(lower, "haiku"):
		return modelPricing{promptPerMillion: 0.80, completionPerMillion: 4.00, cacheReadPerMillion: 0.08}

	// Google Gemini
	case strings.Contains(lower, "gemini") && (strings.Contains(lower, "flash") || strings.Contains(lower, "flash-lite")):
		return modelPricing{promptPerMillion: 0.075, completionPerMillion: 0.30, cacheReadPerMillion: 0.01875}
	case strings.Contains(lower, "gemini") && strings.Contains(lower, "pro"):
		return modelPricing{promptPerMillion: 1.25, completionPerMillion: 5.00, cacheReadPerMillion: 0.3125}
	case strings.Contains(lower, "gemini"):
		return modelPricing{promptPerMillion: 0.35, completionPerMillion: 1.05, cacheReadPerMillion: 0.0875}

	// OpenAI
	case strings.Contains(lower, "o1-preview") || strings.HasPrefix(lower, "o1"):
		return modelPricing{promptPerMillion: 15.00, completionPerMillion: 60.00, cacheReadPerMillion: 7.50}
	case strings.Contains(lower, "o3-mini"):
		return modelPricing{promptPerMillion: 1.10, completionPerMillion: 4.40, cacheReadPerMillion: 0.55}
	case strings.Contains(lower, "gpt-4o-mini"):
		return modelPricing{promptPerMillion: 0.15, completionPerMillion: 0.60, cacheReadPerMillion: 0.075}
	case strings.Contains(lower, "gpt-4o"):
		return modelPricing{promptPerMillion: 2.50, completionPerMillion: 10.00, cacheReadPerMillion: 1.25}

	// DeepSeek
	case strings.Contains(lower, "deepseek-reasoner") || strings.Contains(lower, "r1"):
		return modelPricing{promptPerMillion: 0.55, completionPerMillion: 2.19, cacheReadPerMillion: 0.14}
	case strings.Contains(lower, "deepseek"):
		return modelPricing{promptPerMillion: 0.14, completionPerMillion: 0.28, cacheReadPerMillion: 0.014}

	// Default fallback
	default:
		return modelPricing{promptPerMillion: 1.00, completionPerMillion: 3.00, cacheReadPerMillion: 0.25}
	}
}
