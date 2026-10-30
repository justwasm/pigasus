package statusline

import "strings"

// PricingRow matches a model name fragment (case-insensitive substring)
// to a per-million-token input/output rate.
type PricingRow struct {
	Match          string
	InputPer1M     float64
	OutputPer1M    float64
	CacheReadPer1M float64
}

// PricingTable is the ordered list of (substring, rate) pairs.
var PricingTable = []PricingRow{
	{"claude-opus", 15.0, 75.0, 15.0},
	{"claude-sonnet", 3.0, 15.0, 3.0},
	{"gpt-oss", 0.5, 1.5, 0.0},
	{"gemini-3.8-flash", 0.075, 0.30, 0.0},
	{"minimax", 0, 0, 0},
}

// PricingForModel returns the cost (USD) of accumulating prompt +
// completion tokens under the model's per-1M pricing.
func PricingForModel(model string, prompt, completion int) float64 {
	name := strings.ToLower(strings.TrimSpace(model))
	for _, row := range PricingTable {
		if strings.Contains(name, row.Match) {
			return float64(prompt)*row.InputPer1M/1_000_000 +
				float64(completion)*row.OutputPer1M/1_000_000
		}
	}
	return 0
}
