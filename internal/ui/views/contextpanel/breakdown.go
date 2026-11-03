// Package contextpanel provides breakdown calculations and visual progress
// grid lines for the /context usage panel.
package contextpanel

import (
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/core/backend"
	"github.com/justwasm/pigasus/internal/core/compaction"
	"github.com/justwasm/pigasus/internal/infra/backend/cloudcode"
	"github.com/justwasm/pigasus/palette"
)

// Header is the ANSI-styled title header of the /context panel. Its
// colour follows the selected colour scheme.
var Header = "\x1b[1m" + palette.SubtleOpen + "└ Context Usage\x1b[0m"

// Breakdown holds token counts across all context categories.
//
// The categories are the reference's own (official-docs/cli/commands/context.md:32-42);
// see categories.go for the row set, the order and the markers.
type Breakdown struct {
	Model      string
	ContextMax int

	// The conversation rows: always shown.
	User  int
	Agent int
	Tool  int

	// The system rows.
	SystemPrompt   int
	SystemTools    int
	SystemOverhead int

	// The customization rows.
	Rules     int
	Skills    int
	Subagents int
	Workflows int
	Plugins   int
	MCPTools  int

	// SystemMessages counts the system-generated messages in the
	// conversation.
	SystemMessages int

	// CheckpointBuffer is the token count the session's compaction
	// checkpoint holds.
	//
	// It is not a category: the reference tracks it outside the grid and
	// outside the usage total, so it is in neither TotalUsed nor the
	// grid's fill.
	CheckpointBuffer int
}

// TotalUsed returns the unclamped sum of all token categories.
//
// The checkpoint buffer is not one of them: the reference counts it
// separately, so that the summarized history does not inflate the
// usage percentage.
func (b Breakdown) TotalUsed() int {
	total := 0
	for _, c := range b.allCategories() {
		total += c.Tokens
	}
	return total
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
	return LiveGridLines(b.Model, width, b.ContextMax, b.Categories())
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

// SystemMessageTokens counts the tokens of the session's system-generated
// messages.
//
// The reference reports them on a row of their own ("System messages:
// tokens from system-generated messages in the conversation"), so they
// are not part of the user row. A system message travels to the model
// in the user-content position, so it is counted the way a user message
// is counted.
func SystemMessageTokens(messages []backend.Message) int {
	var total int
	for _, msg := range messages {
		if msg.Role != "system" {
			continue
		}
		wire := msg.WireContent
		if wire == "" {
			wire = backend.WrapUserContent(msg.Content, "", false, time.Now())
		}
		total += backend.UserContentTokenCount(wire)
	}
	return total
}

// CheckpointBufferTokens counts the tokens the session's compaction
// checkpoint holds.
//
// A compaction replaces the compacted turns with a resume block — the
// clone's own block is the compaction package's CarriedBlock followed by
// the summary. The reference tracks that block's tokens outside the
// grid, so the caller takes them out of the user row: they stand in for
// turns that have already been compacted and must not inflate the usage
// percentage.
//
// A session that was never compacted has no checkpoint, and the count is
// zero.
func CheckpointBufferTokens(messages []backend.Message) int {
	header := resumeHeader()
	for _, msg := range messages {
		if msg.Role != "user" {
			continue
		}
		if !strings.HasPrefix(msg.Content, header) {
			return 0
		}
		return compaction.EstimateTokens(msg.Content)
	}
	return 0
}

// resumeHeader is the first line of the clone's post-compaction resume
// block.
//
// The header is taken out of the compaction package's own block rather
// than spelled out here, so the two cannot drift apart.
func resumeHeader() string {
	return strings.SplitN(compaction.CarriedBlock(nil), "\n", 2)[0]
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
	b := cloudcode.WireToolsJSON(tools)
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
		{"\x1b[1m" + palette.SubtleOpen + "Estimated usage (awaiting generation)\x1b[0m", 5},
		{UserRowMarker + " User messages: 0 tokens (0.0%)", 5},
		{AgentGlyph + " Agent responses: 0 tokens (0.0%)", 5},
		{EmptyToolRowMarker + " Tool calls: 0 tokens (0.0%)", 5},
		{palette.DimGrayOpen + "□\x1b[39m Free space: 1.0M (100.0%)", 5},
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
//
// The rows are the breakdown's visible categories and the free-space
// row; the cells are painted in the reference's own order.
func LiveGridLines(model string, width, contextMax int, cats []Category) []string {
	total := 0
	for _, c := range cats {
		total += c.Tokens
	}
	used, free := ClampedUsage(contextMax, total)
	fill := LiveFill(cats)
	rows := LiveRows(model, used, free, contextMax, cats)
	return RenderGridLines(rows, fill, ColsFor(width))
}
