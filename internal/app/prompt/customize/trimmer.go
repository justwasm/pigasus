package customize

import "sort"

// Trimmer is the policy that fits a set of customization Tokens
// into a Budget. Build one with NewTrimmer, register the tokens
// produced by the slash-command handlers, then call Trim() to
// obtain the kept set in registration order.
//
// The trimmer is not safe for concurrent use; one Trimmer instance
// is owned by one context-assembly step.
type Trimmer struct {
	budget Budget
	tokens []*Token
	// kept is the index-set of tokens that survived Trim, in the
	// order they were registered. Populated by Trim, read by
	// callers that want the dropped list as well as the kept one
	// (the AGY changelog wording "less-important tokens get
	// trimmed first" is what makes this useful).
	kept []*Token
	// dropped mirrors kept: every Token the trimmer cut, with the
	// reason implicit ("would have exceeded the budget").
	dropped []*Token
}

// NewTrimmer constructs a Trimmer that fits tokens into the given
// Budget. A zero Budget is allowed; the trimmer behaves as if every
// token exceeded the budget, which is the right answer for the
// "I have no room at all" case.
func NewTrimmer(budget Budget) *Trimmer {
	return &Trimmer{budget: budget}
}

// Add registers a customization payload for trimming. The order of
// Add calls does not matter for the trim decision (the trimmer
// sorts internally by ascending Priority); Trim reorders the kept
// tokens back to registration order so the caller's downstream
// rendering stays stable.
//
// Adding the same Name twice is permitted; both tokens enter the
// budget. A slash command whose handler renders multiple
// independent blocks (e.g. /config split into "settings" and
// "resolved-defaults") can therefore describe each block
// separately and let the trimmer treat them as two budget items.
func (t *Trimmer) Add(name, content string) *Trimmer {
	priority := PriorityFor(name)
	t.tokens = append(t.tokens, NewToken(name, content, int(priority)))
	return t
}

// AddToken registers a pre-built Token. Useful when a caller has
// already rendered the body and wants to attach metadata
// (production code; the slash-command bridge in cmd/wagyu uses
// this).
func (t *Trimmer) AddToken(tok *Token) *Trimmer {
	if tok == nil {
		return t
	}
	if tok.tokens == 0 {
		tok.tokens = EstimateTokens(tok.Content)
	}
	t.tokens = append(t.tokens, tok)
	return t
}

// Tokens returns a snapshot of the registered tokens. The slice is
// detached from internal state; the caller can read or sort it
// freely.
func (t *Trimmer) Tokens() []*Token {
	out := make([]*Token, len(t.tokens))
	copy(out, t.tokens)
	return out
}

// Budget returns the budget the trimmer was built with. Callers
// that want to render a "we trimmed N tokens" message read
// Tokens() and Capacity() together.
func (t *Trimmer) Budget() Budget { return t.budget }

// Trim returns the tokens that fit the Budget, in the order they
// were registered. Dropped tokens are also recorded on the
// receiver (Dropped()) so the caller can echo a user-visible
// diagnostic.
//
// Algorithm:
//
//  1. Copy the registered tokens into a working slice.
//  2. Stable-sort the working slice by descending Priority (so
//     /commands comes first, /customize last). The sort is stable so
//     ties — two tokens with the same priority — keep their
//     registration order, which matches what tests assert about
//     "/agents trimmed after /customize".
//  3. Walk the sorted slice from the top (highest priority first),
//     accumulating Tokens().Tokens(). Stop keeping the moment the
//     next candidate would push the running total over
//     Budget.Capacity(); every remaining candidate has lower
//     priority than what has already been kept, so dropping the
//     rest is the right call.
//  4. Re-sort the survivors back into registration order so the
//     caller's downstream rendering sees the same ordering it
//     registered with.
//
// The algorithm is O(n log n) where n is the number of registered
// tokens. For the eight-command surface the runtime sees today
// the sort dominates; at any plausible future command count the
// constant factors still win over a per-priority bucket walk.
//
// Why descending rather than ascending? Walking ascending
// accumulates the lowest priorities first and only stops once the
// budget is full — which is the inverse of the changelog's
// contract ("less-important tokens get trimmed first"). The
// descending walk reads the candidates from the most valuable
// down, accepting each until the budget is full, which is the
// knapsack greedy that matches the priority contract.
// filterZeroCost absorbs zero-cost tokens into kept and returns real candidates.
func (t *Trimmer) filterZeroCost() []*Token {
	var real []*Token
	for _, tok := range t.tokens {
		if tok.Tokens() == 0 {
			t.kept = append(t.kept, tok)
			continue
		}
		real = append(real, tok)
	}
	return real
}

func reverseTokens(toks []*Token) {
	for i, j := 0, len(toks)-1; i < j; i, j = i+1, j-1 {
		toks[i], toks[j] = toks[j], toks[i]
	}
}

func (t *Trimmer) Trim() []*Token {
	capacity := t.budget.Capacity()
	t.kept = t.kept[:0]
	t.dropped = t.dropped[:0]
	if len(t.tokens) == 0 {
		return nil
	}
	real := t.filterZeroCost()
	sort.SliceStable(real, func(i, j int) bool {
		return real[i].Priority > real[j].Priority
	})
	used := 0
	for _, tok := range real {
		next := used + tok.Tokens()
		if next > capacity {
			t.dropped = append(t.dropped, tok)
			continue
		}
		used = next
		t.kept = append(t.kept, tok)
	}
	reverseTokens(t.dropped)
	sort.SliceStable(t.kept, func(i, j int) bool {
		return registrationIndex(t.tokens, t.kept[i]) < registrationIndex(t.tokens, t.kept[j])
	})
	out := make([]*Token, len(t.kept))
	copy(out, t.kept)
	return out
}

// Dropped returns the tokens the most recent Trim() cut. The slice
// is detached from internal state; mutating it does not affect the
// trimmer. nil when Trim has not been called.
func (t *Trimmer) Dropped() []*Token {
	if len(t.dropped) == 0 {
		return nil
	}
	out := make([]*Token, len(t.dropped))
	copy(out, t.dropped)
	return out
}

// Kept returns the tokens the most recent Trim() kept. Equivalent
// to Trim() but without re-running the sort; safe to call after
// the caller has already stored the result of Trim() and now needs
// to render a per-token diagnostic.
func (t *Trimmer) Kept() []*Token {
	if len(t.kept) == 0 {
		return nil
	}
	out := make([]*Token, len(t.kept))
	copy(out, t.kept)
	return out
}

// TotalTokens returns the sum of Tokens() across every registered
// token, kept or dropped. Useful for diagnostics.
func (t *Trimmer) TotalTokens() int {
	total := 0
	for _, tok := range t.tokens {
		total += tok.Tokens()
	}
	return total
}

// KeptTokens returns the sum of Tokens() across the kept tokens
// after the most recent Trim().
func (t *Trimmer) KeptTokens() int {
	total := 0
	for _, tok := range t.kept {
		total += tok.Tokens()
	}
	return total
}

// registrationIndex is the O(n) helper that finds a token's
// registration position. The Trim sort uses it as the comparator
// to restore registration order; the linear scan is acceptable
// because n is at most the number of customization commands
// (eight today, and the day it grows past twenty is the day a
// registration-time index is worth caching).
func registrationIndex(registered []*Token, target *Token) int {
	for i, t := range registered {
		if t == target {
			return i
		}
	}
	return len(registered)
}
