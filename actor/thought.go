package actor

import (
	"strings"
)

// ThoughtSource classifies the originating provider format for model reasoning.
type ThoughtSource string

const (
	// ThoughtSourceGemini represents Gemini native thought parts (thought: true).
	ThoughtSourceGemini ThoughtSource = "gemini"
	// ThoughtSourceAnthropic represents Claude 3.7 extended thinking blocks.
	ThoughtSourceAnthropic ThoughtSource = "anthropic"
	// ThoughtSourceDeepSeek represents DeepSeek <think>...</think> tagged reasoning.
	ThoughtSourceDeepSeek ThoughtSource = "deepseek"
	// ThoughtSourceOpenAI represents OpenAI / OpenRouter reasoning_content delta.
	ThoughtSourceOpenAI ThoughtSource = "openai"
	// ThoughtSourceGeneric represents generic or unclassified reasoning traces.
	ThoughtSourceGeneric ThoughtSource = "generic"
)

// ThoughtEvent represents a normalized reasoning/thinking unit emitted during turn execution.
type ThoughtEvent struct {
	Source     ThoughtSource `json:"source,omitempty"`
	Content    string        `json:"content,omitempty"`
	Signature  string        `json:"signature,omitempty"`
	IsComplete bool          `json:"is_complete,omitempty"`
}

// knownOpenTags are the opening tags recognized for reasoning streams.
var knownOpenTags = []string{"<think>", "<thought>", "<thinking>"}

// knownCloseTags are the closing tags corresponding to knownOpenTags.
var knownCloseTags = []string{"</think>", "</thought>", "</thinking>"}

// isPrefixOfAny reports whether s (lowercased) is a prefix of any tag in tags.
func isPrefixOfAny(s string, tags []string) bool {
	lower := strings.ToLower(s)
	for _, tag := range tags {
		if strings.HasPrefix(tag, lower) {
			return true
		}
	}
	return false
}

// matchAnyTag checks if s (lowercased) starts with any tag in tags.
// If matched, returns the matched tag length, otherwise 0.
func matchAnyTag(s string, tags []string) int {
	lower := strings.ToLower(s)
	for _, tag := range tags {
		if strings.HasPrefix(lower, tag) {
			return len(tag)
		}
	}
	return 0
}

// ThoughtNormalizer normalizes heterogeneous streaming thought fragments across
// Gemini, Anthropic, DeepSeek, and other providers into unified StreamChunk units.
type ThoughtNormalizer struct {
	inThought bool
	tagBuf    strings.Builder
	source    ThoughtSource
}

// NewThoughtNormalizer creates an initialized ThoughtNormalizer.
func NewThoughtNormalizer() *ThoughtNormalizer {
	return &ThoughtNormalizer{
		source: ThoughtSourceDeepSeek,
	}
}

// Feed processes an incoming StreamChunk and emits zero, one, or more normalized StreamChunks.
func (n *ThoughtNormalizer) Feed(chunk StreamChunk) []StreamChunk {
	var result []StreamChunk

	// 1. Direct thought from Gemini, Anthropic, or OpenAI reasoning_content
	if chunk.Thought != "" {
		te := chunk.ThoughtEvent
		if te == nil {
			te = &ThoughtEvent{
				Source:  ThoughtSourceGemini,
				Content: chunk.Thought,
			}
		} else if te.Source == "" {
			te.Source = ThoughtSourceGemini
		}
		result = append(result, StreamChunk{
			Thought:      chunk.Thought,
			ThoughtEvent: te,
			Usage:        chunk.Usage,
			FinishReason: chunk.FinishReason,
		})
		// If there is no delta, we are done with this chunk
		if chunk.Delta == "" && len(chunk.ToolCalls) == 0 {
			return result
		}
	}

	// 2. Pass tool calls through if present
	if len(chunk.ToolCalls) > 0 && chunk.Delta == "" {
		result = append(result, StreamChunk{
			ToolCalls:    chunk.ToolCalls,
			Usage:        chunk.Usage,
			FinishReason: chunk.FinishReason,
		})
		return result
	}

	// 3. Process delta for <think> tags (DeepSeek style)
	if chunk.Delta != "" {
		deltaChunks := n.feedDeltaText(chunk.Delta)
		for i := range deltaChunks {
			// Attach usage, finish reason, tool calls to the last chunk if applicable
			if i == len(deltaChunks)-1 {
				if chunk.Usage != nil && deltaChunks[i].Usage == nil {
					deltaChunks[i].Usage = chunk.Usage
				}
				if chunk.FinishReason != "" && deltaChunks[i].FinishReason == "" {
					deltaChunks[i].FinishReason = chunk.FinishReason
				}
				if len(chunk.ToolCalls) > 0 {
					deltaChunks[i].ToolCalls = append(deltaChunks[i].ToolCalls, chunk.ToolCalls...)
				}
			}
			result = append(result, deltaChunks[i])
		}
		return result
	}

	// Fallback for empty delta with finish reason or usage
	if len(result) == 0 && (chunk.FinishReason != "" || chunk.Usage != nil) {
		result = append(result, chunk)
	}

	return result
}

// feedDeltaText processes raw text delta through the streaming tag state machine.
func (n *ThoughtNormalizer) feedDeltaText(delta string) []StreamChunk {
	var chunks []StreamChunk
	var outThought strings.Builder
	var outDelta strings.Builder

	flushThought := func() {
		if outThought.Len() > 0 {
			str := outThought.String()
			chunks = append(chunks, StreamChunk{
				Thought: str,
				ThoughtEvent: &ThoughtEvent{
					Source:  n.source,
					Content: str,
				},
			})
			outThought.Reset()
		}
	}

	flushDelta := func() {
		if outDelta.Len() > 0 {
			chunks = append(chunks, StreamChunk{
				Delta: outDelta.String(),
			})
			outDelta.Reset()
		}
	}

	runes := []rune(delta)
	idx := 0
	total := len(runes)

	for idx < total {
		r := runes[idx]

		if n.tagBuf.Len() > 0 {
			// Currently accumulating a potential tag
			n.tagBuf.WriteRune(r)
			bufStr := n.tagBuf.String()

			if !n.inThought {
				// Looking for opening tag (e.g. <think>)
				if tagLen := matchAnyTag(bufStr, knownOpenTags); tagLen > 0 {
					// Opening tag matched!
					flushDelta()
					n.inThought = true
					n.tagBuf.Reset()
					// If buffer had trailing runes past the tag, handle them
					extra := bufStr[tagLen:]
					if extra != "" {
						outThought.WriteString(extra)
					}
					idx++
					continue
				}

				if isPrefixOfAny(bufStr, knownOpenTags) {
					// Still potentially a tag prefix, continue buffering
					idx++
					continue
				}

				// Not a tag prefix, flush buffer to delta
				outDelta.WriteString(bufStr)
				n.tagBuf.Reset()
				idx++
				continue
			} else {
				// Looking for closing tag (e.g. </think>)
				if tagLen := matchAnyTag(bufStr, knownCloseTags); tagLen > 0 {
					// Closing tag matched!
					flushThought()
					n.inThought = false
					n.tagBuf.Reset()
					extra := bufStr[tagLen:]
					if extra != "" {
						outDelta.WriteString(extra)
					}
					idx++
					continue
				}

				if isPrefixOfAny(bufStr, knownCloseTags) {
					// Still potentially a closing tag prefix, continue buffering
					idx++
					continue
				}

				// Not a closing tag prefix, flush buffer to thought
				outThought.WriteString(bufStr)
				n.tagBuf.Reset()
				idx++
				continue
			}
		}

		// tagBuf is empty
		if r == '<' {
			// Potential tag start
			n.tagBuf.WriteRune('<')
			idx++
			continue
		}

		if n.inThought {
			outThought.WriteRune(r)
		} else {
			outDelta.WriteRune(r)
		}
		idx++
	}

	flushThought()
	flushDelta()

	return chunks
}

// Flush emits any remaining buffered characters when the stream finishes.
func (n *ThoughtNormalizer) Flush() []StreamChunk {
	if n.tagBuf.Len() == 0 {
		return nil
	}

	bufStr := n.tagBuf.String()
	n.tagBuf.Reset()

	if n.inThought {
		return []StreamChunk{
			{
				Thought: bufStr,
				ThoughtEvent: &ThoughtEvent{
					Source:  n.source,
					Content: bufStr,
				},
			},
		}
	}

	return []StreamChunk{
		{
			Delta: bufStr,
		},
	}
}

// ParseThought parses full non-streaming text into normalized thought events and cleaned response text.
func ParseThought(raw string) (thoughts []ThoughtEvent, content string) {
	normalizer := NewThoughtNormalizer()
	chunks := normalizer.Feed(StreamChunk{Delta: raw})
	chunks = append(chunks, normalizer.Flush()...)

	var thoughtBuilder strings.Builder
	var contentBuilder strings.Builder

	for _, c := range chunks {
		if c.Thought != "" {
			thoughtBuilder.WriteString(c.Thought)
		}
		if c.Delta != "" {
			contentBuilder.WriteString(c.Delta)
		}
	}

	th := thoughtBuilder.String()
	if th != "" {
		thoughts = append(thoughts, ThoughtEvent{
			Source:     ThoughtSourceDeepSeek,
			Content:    th,
			IsComplete: true,
		})
	}
	return thoughts, contentBuilder.String()
}
