// Package contextpanel provides breakdown calculations and visual progress
// grid lines for the /context usage panel.
package contextpanel

import (
	"time"

	"github.com/justwasm/pigasus/internal/backend"
)

// Header is the ANSI-styled title header of the /context panel.
const Header = "\x1b[1m\x1b[38;2;88;110;117m└ Context Usage\x1b[0m"

// Breakdown holds token counts across all context categories.
type Breakdown struct {
	Model        string
	ContextMax   int
	User         int
	Agent        int
	Tool         int
	SystemPrompt int
	SystemTools  int
	Skills       int
	Subagents    int
}

// TotalUsed returns the unclamped sum of all token categories.
func (b Breakdown) TotalUsed() int {
	return b.User + b.Agent + b.Tool + b.SystemPrompt + b.SystemTools + b.Skills + b.Subagents
}

// StreamingTotalUsed returns the sum of in-flight token categories.
func (b Breakdown) StreamingTotalUsed() int {
	return b.User + b.Agent + b.Tool
}

// ClampedUsage calculates clamped used tokens and free space for completed turns.
func (b Breakdown) ClampedUsage() (used, free int) {
	return ClampedUsage(b.ContextMax, b.TotalUsed())
}

// StreamingClampedUsage calculates clamped used and free tokens for in-flight turns.
func (b Breakdown) StreamingClampedUsage() (used, free int) {
	return ClampedUsage(b.ContextMax, b.StreamingTotalUsed())
}

// FreePercent returns the percentage of context capacity remaining.
func (b Breakdown) FreePercent() float64 {
	used, _ := b.ClampedUsage()
	return FreeCapacityPercentage(used, b.ContextMax)
}

// UsedPercent returns the percentage of context capacity used.
func (b Breakdown) UsedPercent() float64 {
	used, _ := b.ClampedUsage()
	return UsedCapacityPercentage(used, b.ContextMax)
}

// FreePct returns the formatted percentage of free context capacity "(X.X%)".
func (b Breakdown) FreePct() string {
	_, free := b.ClampedUsage()
	return Pct(free, b.ContextMax)
}

// UsedPct returns the formatted percentage of used context capacity "(X.X%)".
func (b Breakdown) UsedPct() string {
	used, _ := b.ClampedUsage()
	return Pct(used, b.ContextMax)
}

// LiveGridLines renders the complete visual grid bar lines for this breakdown.
func (b Breakdown) LiveGridLines(width int) []string {
	return LiveGridLines(b.Model, width, b.ContextMax, b.User, b.Agent, b.Tool,
		b.SystemPrompt, b.SystemTools, b.Skills, b.Subagents)
}

// StreamingGridLines renders the in-flight visual grid bar lines for this breakdown.
func (b Breakdown) StreamingGridLines(width int) []string {
	return StreamingGridLines(b.Model, width, b.ContextMax, b.User, b.Agent, b.Tool)
}

// ClampedUsage bounds used tokens to [0, contextMax] and computes remaining free space.
func ClampedUsage(contextMax, rawUsed int) (used, free int) {
	used = rawUsed
	if used > contextMax {
		used = contextMax
	}
	if used < 0 {
		used = 0
	}
	free = contextMax - used
	if free < 0 {
		free = 0
	}
	return used, free
}

// FreeCapacityPercentages returns remaining free tokens, formatted used %, and free %.
func FreeCapacityPercentages(contextMax, rawUsed int) (free int, usedPct, freePct string) {
	used, free := ClampedUsage(contextMax, rawUsed)
	return free, Pct(used, contextMax), Pct(free, contextMax)
}

// FreeCapacityPercentage returns the float percentage of free space in [0.0, 100.0].
func FreeCapacityPercentage(used, contextMax int) float64 {
	if contextMax <= 0 {
		return 0.0
	}
	free := contextMax - used
	if free <= 0 {
		return 0.0
	}
	if free >= contextMax {
		return 100.0
	}
	return float64(free) / float64(contextMax) * 100
}

// UsedCapacityPercentage returns the float percentage of used space in [0.0, 100.0].
func UsedCapacityPercentage(used, contextMax int) float64 {
	if contextMax <= 0 || used <= 0 {
		return 0.0
	}
	if used >= contextMax {
		return 100.0
	}
	return float64(used) / float64(contextMax) * 100
}

// Percentage computes the float percentage of part relative to total.
func Percentage(part, total int) float64 {
	if total <= 0 || part <= 0 {
		return 0.0
	}
	return float64(part) / float64(total) * 100
}

// FormatPercentage formats part/total as "(X.X%)", delegating to Pct.
func FormatPercentage(part, total int) string {
	return Pct(part, total)
}

// UserMessageTokens counts tokens across all user messages in a session.
func UserMessageTokens(messages []backend.Message) int {
	var user int
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		wire := msg.WireContent
		if wire == "" {
			wire = backend.WrapUserContent(msg.Content, "", false, time.Now())
		}
		user += backend.UserContentTokenCount(wire)
	}
	return user
}

// AdjustStreamingUserTokens adjusts user token count for the in-flight user turn.
func AdjustStreamingUserTokens(messages []backend.Message, user int) int {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg.Role != "user" {
			continue
		}
		wire := msg.WireContent
		if wire == "" {
			wire = backend.WrapUserContent(msg.Content, "", false, time.Now())
		}
		user -= backend.UserContentTokenCount(wire)
		if n := len(msg.Content) / 4; n > 1 {
			user += n
		} else {
			user += 1
		}
		break
	}
	if user < 0 {
		user = 0
	}
	return user
}

// AgentTokens computes completed agent tokens, floored at 0.
func AgentTokens(sessionCompletionTokens, lastTurnCompletionTokens int64) int {
	completed := int(sessionCompletionTokens - lastTurnCompletionTokens)
	if completed < 0 {
		return 0
	}
	return completed
}

// StreamingAgentTokens computes agent tokens including the in-flight reply buffer.
func StreamingAgentTokens(sessionTokens, lastTurnTokens int64, liveReplyLen int) int {
	return AgentTokens(sessionTokens, lastTurnTokens) + liveReplyLen/4
}

// ToolTokenEstimate estimates token count from backend tool declarations.
func ToolTokenEstimate(tools []backend.ToolDecl) int {
	b := backend.WireToolsJSON(tools)
	return ToolTokenEstimateFromBytes(b)
}

// ToolTokenEstimateFromBytes estimates token count from wire tool JSON bytes.
func ToolTokenEstimateFromBytes(rawJSON []byte) int {
	return len(rawJSON) / 3
}

// ToolResultTokens sums token counts across tool result strings.
func ToolResultTokens(results ...string) int {
	var total int
	for _, r := range results {
		total += backend.ToolResultTokenCount(r)
	}
	return total
}

// PromptSectionTokens measures token count of an XML section <name>body</name>.
func PromptSectionTokens(name, body string) int {
	if body == "" {
		return 0
	}
	return (len(body) + 2*len(name) + 2 + 1) / 3
}

// SystemPromptTokens calculates base system prompt tokens by subtracting skills and subagents.
func SystemPromptTokens(fullPromptLen, skillsTokens, subagentsTokens int) int {
	base := (fullPromptLen - skillsTokens*3 - subagentsTokens*3) / 3
	if base < 0 {
		return 0
	}
	return base
}

// EmptyRows returns the 7 descriptor rows of the empty context state.
func EmptyRows(model string) []Row {
	return []Row{
		{model + " · 0/1.0M tokens", 5},
		{"(0.0%)", 6},
		{"\x1b[1m\x1b[38;2;88;110;117mEstimated usage (awaiting generation)\x1b[0m", 5},
		{"\x1b[1m\x1b[38;5;26m◉\x1b[0m User messages: 0 tokens (0.0%)", 5},
		{"\x1b[38;2;133;153;0m◉\x1b[39m Agent responses: 0 tokens (0.0%)", 5},
		{"\x1b[1m\x1b[38;5;172m◉\x1b[0m Tool calls: 0 tokens (0.0%)", 5},
		{"\x1b[38;2;101;123;131m□\x1b[39m Free space: 1.0M (100.0%)", 5},
	}
}

// RenderGridLines renders descriptor rows into visual grid bar lines.
func RenderGridLines(rows []Row, fill []string, cols int) []string {
	out := make([]string, len(rows))
	for i, r := range rows {
		out[i] = GridRowFilled(r.Text, r.Pad, cols, fill, i*cols)
	}
	return out
}

// EmptyGridLines renders the visual grid bar lines for the empty context state.
func EmptyGridLines(model string, width int) []string {
	return RenderGridLines(EmptyRows(model), nil, ColsFor(width))
}

// StreamingGridLines renders the visual grid bar lines for an in-flight turn.
func StreamingGridLines(model string, width, contextMax, user, agent, tool int) []string {
	used, free := ClampedUsage(contextMax, user+agent+tool)
	fill := StreamingFill(user, agent, tool)
	rows := StreamingRows(model, used, free, contextMax, user, agent, tool)
	return RenderGridLines(rows, fill, ColsFor(width))
}

// LiveGridLines renders the visual grid bar lines for a completed turn.
func LiveGridLines(model string, width, contextMax, user, agent, tool, sysPrompt, sysTools, skills, subagents int) []string {
	total := user + agent + tool + sysPrompt + sysTools + skills + subagents
	used, free := ClampedUsage(contextMax, total)
	fill := LiveFill(sysPrompt, user, agent, tool, sysTools, skills, subagents)
	rows := LiveRows(model, used, free, contextMax, user, agent, tool, sysPrompt, sysTools, skills, subagents)
	return RenderGridLines(rows, fill, ColsFor(width))
}
