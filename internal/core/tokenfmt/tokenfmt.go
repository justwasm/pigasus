// Package tokenfmt formats a count of tokens the way the reference
// does.
//
// The format is a fact about tokens, not about a surface: the print
// path writes it on its post-compact line and the status row writes it
// in the compaction label, so it lives in the core tier and both
// import it.
//
// 【一手】 agy 1.4.7's own post-compact stdout line reads
// "Compacted context 184 K → 1.2 M tokens", and cycle 225's quota
// countdowns use the same figures.
package tokenfmt

import (
	"strconv"
	"strings"
)

// K renders a count of tokens as "184" / "184 K" / "1.2 M".
//
// 0 yields "-" so the print path never prints "0 K → 0 K tokens" on an
// event that lacked the upstream figure.
func K(tokens int) string {
	if tokens <= 0 {
		return "-"
	}
	if tokens < 1000 {
		return strconv.Itoa(tokens)
	}
	if tokens < 1_000_000 {
		// Round to 1 decimal, drop trailing ".0" for whole kilo figures.
		kilo := float64(tokens) / 1000.0
		return strings.TrimSuffix(strconv.FormatFloat(kilo, 'f', 1, 64), ".0") + " K"
	}
	mega := float64(tokens) / 1_000_000.0
	return strings.TrimSuffix(strconv.FormatFloat(mega, 'f', 1, 64), ".0") + " M"
}
