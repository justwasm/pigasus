package modelctx

import (
	"math"
	"math/big"
	"os"
	"strconv"
	"strings"
)

// EnvAutoCompactOverride is the environment variable that overrides the
// auto-compaction token threshold across all eligible models.
//
// Accepted formats (matching upstream agy 1.5.15):
//   - Plain positive integer tokens: "700000"
//   - Decimal with K/k or M/m suffix: "800K", "0.5M"
//   - Percentage of the model's context window: "80%" (0 < pct <= 100)
const EnvAutoCompactOverride = "AGY_CLI_AUTO_COMPACT_OVERRIDE"

// AutoCompactOverrideSpec returns the trimmed raw value of
// AGY_CLI_AUTO_COMPACT_OVERRIDE.
func AutoCompactOverrideSpec() string {
	return strings.TrimSpace(os.Getenv(EnvAutoCompactOverride))
}

// AutoCompactOverride parses AGY_CLI_AUTO_COMPACT_OVERRIDE against maxTokens
// (the model's context window). It returns the resolved token threshold, the
// trimmed raw spec, and whether a valid override is active.
//
// Invalid values, non-positive values, percentages when maxTokens <= 0, or
// thresholds exceeding maxTokens (when maxTokens > 0) or math.MaxUint32 are
// ignored and return (0, "", false).
func AutoCompactOverride(maxTokens int) (tokens int, spec string, ok bool) {
	spec = AutoCompactOverrideSpec()
	if spec == "" {
		return 0, "", false
	}
	if strings.HasSuffix(spec, "%") {
		if maxTokens <= 0 {
			return 0, "", false
		}
		pctStr := strings.TrimSpace(strings.TrimSuffix(spec, "%"))
		rat, valid := parseUnsignedDecimalRat(pctStr)
		if !valid || rat.Sign() <= 0 {
			return 0, "", false
		}
		hundred := big.NewRat(100, 1)
		if rat.Cmp(hundred) > 0 {
			return 0, "", false
		}
		// floor(maxTokens * rat / 100)
		scaled := new(big.Rat).Mul(big.NewRat(int64(maxTokens), 1), rat)
		scaled.Quo(scaled, hundred)
		n, valid := ratFloorUint64(scaled)
		if !valid || n == 0 || n > uint64(maxTokens) || n > math.MaxUint32 || n > math.MaxInt {
			return 0, "", false
		}
		return int(n), spec, true
	}

	n, valid := parseOverrideTokens(spec)
	if !valid || n == 0 || n > math.MaxUint32 || n > math.MaxInt {
		return 0, "", false
	}
	if maxTokens > 0 && n > uint64(maxTokens) {
		return 0, "", false
	}
	return int(n), spec, true
}

// AutoCompactWindow returns the effective auto-compaction threshold in tokens
// for modelID and the AGY_CLI_AUTO_COMPACT_OVERRIDE spec if overridden.
// When neither an override nor a per-model configured threshold is active, it
// returns (0, "") to indicate server default.
func AutoCompactWindow(modelID string) (tokens int, overrideSpec string) {
	maxTokens := PublishedContextWindow(modelID)
	if ov, spec, ok := AutoCompactOverride(maxTokens); ok {
		return ov, spec
	}
	if cp, ok := ConfiguredCompactThreshold(modelID); ok && cp > 0 {
		return cp, ""
	}
	return 0, ""
}

func parseOverrideTokens(s string) (uint64, bool) {
	if s == "" {
		return 0, false
	}
	last := s[len(s)-1]
	var mult int64
	switch last {
	case 'K', 'k':
		mult = 1_000
	case 'M', 'm':
		mult = 1_000_000
	default:
		n, err := strconv.ParseUint(s, 10, 32)
		if err != nil || n == 0 {
			return 0, false
		}
		return n, true
	}
	prefix := strings.TrimSpace(s[:len(s)-1])
	rat, ok := parseUnsignedDecimalRat(prefix)
	if !ok || rat.Sign() <= 0 {
		return 0, false
	}
	scaled := new(big.Rat).Mul(rat, big.NewRat(mult, 1))
	return ratFloorUint64(scaled)
}

// parseUnsignedDecimalRat parses an unsigned decimal string (e.g. "800", "0.5",
// "80.25") without signs, exponents, leading/trailing dots, or multiple dots.
func parseUnsignedDecimalRat(s string) (*big.Rat, bool) {
	if s == "" {
		return nil, false
	}
	dotSeen := false
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '.' {
			if dotSeen || i == 0 || i == len(s)-1 {
				return nil, false
			}
			dotSeen = true
			continue
		}
		if c < '0' || c > '9' {
			return nil, false
		}
	}
	r, ok := new(big.Rat).SetString(s)
	if !ok {
		return nil, false
	}
	return r, true
}

func ratFloorUint64(r *big.Rat) (uint64, bool) {
	if r == nil || r.Sign() < 0 {
		return 0, false
	}
	q := new(big.Int).Quo(r.Num(), r.Denom())
	if !q.IsUint64() {
		return 0, false
	}
	return q.Uint64(), true
}
