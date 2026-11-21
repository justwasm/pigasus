package voice

import (
	"fmt"
	"strings"
	"sync"
)

// DefaultKeepTailTurns is the number of most recent turns preserved in the active sliding tail.
const DefaultKeepTailTurns = 4

// DefaultCompactThreshold is the turn count that triggers automatic history compaction.
const DefaultCompactThreshold = 8

// CompactorOption configures a VoiceHistoryCompactor.
type CompactorOption func(*VoiceHistoryCompactor)

// WithKeepTail sets the number of recent turns kept uncompressed in the active tail.
func WithKeepTail(n int) CompactorOption {
	return func(c *VoiceHistoryCompactor) {
		if n > 0 {
			c.keepTailTurns = n
		}
	}
}

// WithCompactThreshold sets the turn threshold that triggers compaction.
func WithCompactThreshold(n int) CompactorOption {
	return func(c *VoiceHistoryCompactor) {
		if n > 0 {
			c.compactThreshold = n
		}
	}
}

// WithSummarizer configures a custom summarization function for compacting older turns.
func WithSummarizer(fn func(turns []LiveTurnRecord) (string, error)) CompactorOption {
	return func(c *VoiceHistoryCompactor) {
		if fn != nil {
			c.summarizer = fn
		}
	}
}

// VoiceHistoryCompactor manages the accumulation and structured compression of speech
// transcripts in a real-time live session. It enforces a sliding window (keepTail) while
// folding older conversational history into a dense summary, preventing context explosion
// across reconnects, resumes, and session handoffs.
type VoiceHistoryCompactor struct {
	mu               sync.RWMutex
	allTurns         []LiveTurnRecord
	compactedThrough int
	summary          string
	keepTailTurns    int
	compactThreshold int
	summarizer       func(turns []LiveTurnRecord) (string, error)
}

// NewVoiceHistoryCompactor creates an initialized VoiceHistoryCompactor.
func NewVoiceHistoryCompactor(opts ...CompactorOption) *VoiceHistoryCompactor {
	c := &VoiceHistoryCompactor{
		allTurns:         make([]LiveTurnRecord, 0),
		keepTailTurns:    DefaultKeepTailTurns,
		compactThreshold: DefaultCompactThreshold,
		summarizer:       defaultStructuredSummarizer,
	}
	for _, opt := range opts {
		opt(c)
	}
	return c
}

// AddTurn appends a new spoken turn and triggers compaction if the threshold is met.
func (c *VoiceHistoryCompactor) AddTurn(role, content string) {
	content = strings.TrimSpace(content)
	if content == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()

	c.allTurns = append(c.allTurns, LiveTurnRecord{
		Role:    role,
		Content: content,
	})

	c.maybeCompactLocked()
}

// SetTurns replaces the accumulated history turns and re-evaluates compaction.
func (c *VoiceHistoryCompactor) SetTurns(turns []LiveTurnRecord) {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.allTurns = make([]LiveTurnRecord, len(turns))
	copy(c.allTurns, turns)
	c.compactedThrough = 0
	c.summary = ""

	c.maybeCompactLocked()
}

// maybeCompactLocked checks if historical turns exceed keepTail + threshold and folds them.
func (c *VoiceHistoryCompactor) maybeCompactLocked() {
	total := len(c.allTurns)
	if total < c.compactThreshold {
		return
	}

	// Boundary: everything before the tail should be summarized
	cutoff := total - c.keepTailTurns
	if cutoff <= c.compactedThrough {
		return
	}

	toSummarize := c.allTurns[:cutoff]
	if len(toSummarize) == 0 {
		return
	}

	sum, err := c.summarizer(toSummarize)
	if err == nil && sum != "" {
		c.summary = sum
		c.compactedThrough = cutoff
	}
}

// AllTurns returns a thread-safe snapshot of all turns recorded in the session.
func (c *VoiceHistoryCompactor) AllTurns() []LiveTurnRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()
	out := make([]LiveTurnRecord, len(c.allTurns))
	copy(out, c.allTurns)
	return out
}

// ActiveTail returns only the recent uncompacted turns that fit within the keepTail window.
func (c *VoiceHistoryCompactor) ActiveTail() []LiveTurnRecord {
	c.mu.RLock()
	defer c.mu.RUnlock()
	total := len(c.allTurns)
	if total <= c.keepTailTurns {
		out := make([]LiveTurnRecord, total)
		copy(out, c.allTurns)
		return out
	}
	start := total - c.keepTailTurns
	out := make([]LiveTurnRecord, c.keepTailTurns)
	copy(out, c.allTurns[start:])
	return out
}

// Summary returns the current compacted summary of older turns, if any.
func (c *VoiceHistoryCompactor) Summary() string {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.summary
}

// FormatContext formats the conversation context for session resumption or prompt priming.
// It prepends the compacted summary (if present) followed by the active tail turns.
func (c *VoiceHistoryCompactor) FormatContext(header string) string {
	c.mu.RLock()
	defer c.mu.RUnlock()

	var sb strings.Builder
	if header != "" {
		sb.WriteString(header)
		sb.WriteString("\n\n")
	}

	if c.summary != "" {
		sb.WriteString("# Previous Conversation Summary:\n")
		sb.WriteString(c.summary)
		sb.WriteString("\n\n")
	}

	tail := c.allTurns
	if len(tail) > c.keepTailTurns {
		tail = tail[len(tail)-c.keepTailTurns:]
	}

	if len(tail) > 0 {
		if c.summary != "" {
			sb.WriteString("# Recent Utterances:\n")
		}
		for _, t := range tail {
			sb.WriteString(fmt.Sprintf("%s: %s\n", t.Role, t.Content))
		}
	}

	return strings.TrimSpace(sb.String())
}

// FormatReconnectionContext formats an injection block specifically tailored for recovery after
// network interruption, preserving continuity while bounding tokens.
func (c *VoiceHistoryCompactor) FormatReconnectionContext() string {
	header := "[SYSTEM NOTE: Network connection was briefly disrupted and has just reconnected. " +
		"Below is the prior conversation context. Maintain seamless continuity without greeting from scratch.]"
	return c.FormatContext(header)
}

// FormatResumeContext formats an injection block for resuming a previously saved session.
func (c *VoiceHistoryCompactor) FormatResumeContext() string {
	header := "[SYSTEM NOTE: User has resumed a previous LiveChat voice session. " +
		"Below is the compacted context from that session. Maintain full context continuity and acknowledge that you are ready to continue.]"
	return c.FormatContext(header)
}

// defaultStructuredSummarizer produces a clean bulleted summary of spoken interactions.
func defaultStructuredSummarizer(turns []LiveTurnRecord) (string, error) {
	if len(turns) == 0 {
		return "", nil
	}
	var sb strings.Builder
	for _, t := range turns {
		content := strings.TrimSpace(t.Content)
		if content == "" {
			continue
		}
		// Compact long text to first sentence or 120 chars
		if len(content) > 120 {
			if dot := strings.Index(content, "."); dot > 20 && dot <= 120 {
				content = content[:dot+1]
			} else {
				runes := []rune(content)
				if len(runes) > 100 {
					content = string(runes[:97]) + "..."
				}
			}
		}
		sb.WriteString(fmt.Sprintf("- [%s]: %s\n", t.Role, content))
	}
	return strings.TrimSpace(sb.String()), nil
}
