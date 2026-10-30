package customize

import "strings"

// Token is one customization payload contributed by a slash command
// (/customize, /config, /agents, /hooks, /mcp, /plugin, /skills,
// /commands). The Trimmer compares Priority values to decide what to
// drop when the context window is tight; the lower the value, the
// earlier the token is trimmed.
//
// Name is the slash command that produced the payload ("customize",
// "config", "agents", …); it survives trimming so callers can
// report what the trimmer dropped.
//
// Content is the rendered body. The estimator divides len(Content)
// by 4 to match internal/backend/client.go's approxTokens helper —
// the upstream's wire format is too far from a real tokenizer for
// the clone to do better, and any systematic bias affects every
// payload equally so the relative ordering the trimmer depends on
// stays correct.
type Token struct {
	Name     string
	Content  string
	Priority int
	// tokens is len(Content)/4, computed by EstimateTokens. Negative
	// or zero content yields zero tokens so a missing source does
	// not consume budget.
	tokens int
}

// EstimateTokens returns the ~4-chars-per-token estimate the package
// uses for every payload. Centralising the formula here keeps the
// tests honest: a unit test that hand-builds a Token with a known
// string length can read .Tokens() and assert the trimmer matches.
//
// Whitespace-only strings collapse to zero tokens — an empty
// customization payload is free.
func EstimateTokens(s string) int {
	if strings.TrimSpace(s) == "" {
		return 0
	}
	n := len(s) / 4
	if n < 1 {
		return 1
	}
	return n
}

// Tokens reports the cached estimate. The cache is filled lazily by
// the Trimmer's first pass so a Token built with NewToken and never
// handed to a Trimmer still reports its estimate.
func (t *Token) Tokens() int {
	if t.tokens > 0 {
		return t.tokens
	}
	t.tokens = EstimateTokens(t.Content)
	return t.tokens
}

// NewToken constructs a Token with its tokens pre-computed so callers
// that hand it to a Trimmer immediately pay no allocation overhead
// in the sort comparator.
func NewToken(name, content string, priority int) *Token {
	t := &Token{Name: name, Content: content, Priority: priority}
	t.tokens = EstimateTokens(content)
	return t
}

// Source is the optional interface a slash command can implement to
// describe its hand to the trimmer without pre-rendering the body.
// The default Trimmer path does not use Source — handlers call
// NewToken directly — but defining it here keeps the per-command
// surface uniform so a future caller can enumerate sources without
// touching the handlers.
type Source interface {
	// Name is the slash command (without the leading slash).
	Name() string
	// Priority is the canonical ordering (see priority.go).
	Priority() int
	// Render produces the current payload. Trimmer callers are
	// expected to wrap the result in a Token via NewToken; Render
	// alone is not enough because the trimmer needs to count
	// tokens on the rendered body.
	Render() string
}
