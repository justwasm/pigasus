package customize

// Budget is the context window the trimmer fits customization
// payloads into.
//
// Window is the model's full context window (200 000 for the
// Gemini-class Antigravity CLI ships; see
// internal/pigrt/runtime.go's compactContextWindow).
//
// Reserve is the headroom the trimmer leaves for the live
// conversation and the model's output. The reference's
// auto-compact threshold (see internal/pigrt.CompactThreshold) maps
// onto this number; the trimmer uses Reserve as a fixed floor so a
// caller can decide "I have 50 K tokens of room for everything
// that isn't the agent's stream" and the trimmer respects it.
//
// Used is the non-customization tokens already in the context —
// the system prompt, the tool catalog, the resume history, etc.
// Capacity() subtracts it from the available headroom.
type Budget struct {
	Window  int
	Reserve int
	Used    int
}

// Capacity is the budget customization payloads can spend. It is
// Window minus Reserve minus Used, clamped at zero; a Budget whose
// Used already exceeds Window - Reserve reports zero so an
// over-full conversation cannot trigger the trimmer into keeping
// customization tokens it cannot afford.
func (b Budget) Capacity() int {
	cap := b.Window - b.Reserve - b.Used
	if cap < 0 {
		return 0
	}
	return cap
}

// Fits reports whether the given token count is within the
// customization budget. It is the boolean the Trimmer uses per
// candidate; exposed so a caller can short-circuit ("I know this
// /skills payload is bigger than my headroom; tell me how much I
// have to trim elsewhere") without re-doing the arithmetic.
func (b Budget) Fits(tokens int) bool {
	return tokens <= b.Capacity()
}

// Remaining reports how many tokens of capacity a hypothetical
// payload of the given size would leave. Negative results are
// clamped to zero so a caller rendering a "we had to drop X tokens
// of customization to fit" message does not need its own bounds
// check.
func (b Budget) Remaining(tokens int) int {
	r := b.Capacity() - tokens
	if r < 0 {
		return 0
	}
	return r
}

// DefaultBudget is the budget a context-assembly step uses when no
// caller has supplied one. It mirrors the canonical 200 K Gemini
// window and a 50 K reserve — enough for a long-running resume + a
// large model output, while leaving 150 K for customization and the
// actual user prompt. Override per-call via NewBudget when a
// smaller-window model is in play.
func DefaultBudget() Budget {
	return Budget{
		Window:  200_000,
		Reserve: 50_000,
		Used:    0,
	}
}

// NewBudget constructs a Budget with the given window/reserve/used
// triple. It is the constructor callers use when DefaultBudget is
// the wrong shape (small-window model, pre-loaded system prompt
// counted in Used, etc.).
func NewBudget(window, reserve, used int) Budget {
	return Budget{Window: window, Reserve: reserve, Used: used}
}
