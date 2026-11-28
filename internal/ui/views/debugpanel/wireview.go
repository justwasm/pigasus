// wireview.go — the panel's Wire view: what the next turn puts on the wire.
//
// # What it answers
//
// "Why did this turn cost 20 000 more tokens than the last one." The
// three things a request carries are the system instruction, the tool
// declarations and the conversation; the view sizes each of them, and
// sizes each member of each, so the answer is a row rather than a guess.
//
// # What it does not do
//
// It does not build a request. The wire body is the backend's
// (`geminicode.BuildRequest` adds the labels, the thinking config and
// the execution model), and the host only holds the inputs the backend
// is given. So the view reports *host-side* sizes and says so on its
// heading: a byte-exact body wants a backend seam, and pretending
// otherwise would be the same class of dishonesty as naming a model the
// endpoint is not serving.
//
// # Where the numbers come from
//
// The system instruction's sizes are `agyprompt.SectionTokenCount` over
// the sections `agyprompt.Build` emits; the tool sizes are
// `cloudcode.WireToolsJSON` over the advertised declarations; the
// message sizes are `backend.UserContentTokenCount` and
// `backend.ToolResultTokenCount`, the reference's own accounting. The
// host passes them in; this file only lays them out.
package debugpanel

import (
	"fmt"
	"sort"
	"strings"

	"github.com/charmbracelet/x/ansi"

	"github.com/justwasm/pigasus/internal/core/tokenfmt"
	"github.com/justwasm/pigasus/palette"
)

// WireSection is one section of the system instruction, sized.
type WireSection struct {
	// Name is the XML tag's name, without the angle brackets.
	Name string
	// Bytes is the section's own byte length, tags included.
	Bytes int
	// Tokens is the section's share of the prompt, as the reference's
	// /context accounting measures it.
	Tokens int
}

// WireTool is one advertised tool declaration, sized as the wire
// serialises it.
type WireTool struct {
	Name   string
	Bytes  int
	Tokens int
}

// WireMessage is one message of the conversation, sized.
type WireMessage struct {
	// Index is the message's position in the request, 1-based.
	Index int
	// Role is the wire role: user, model, tool or system.
	Role string
	// Kind names what the message carries — "envelope" for a wrapped
	// user turn, "tool_call", "function response", "reply", "thought".
	Kind string
	// Bytes is the message's content length.
	Bytes int
	// Tokens is the message's share of the conversation.
	Tokens int
	// Preview is the first line of the content, already truncated to
	// the view's width by the host. It is the row's folded body.
	Preview string
}

// WireState is everything the Wire view prints.
type WireState struct {
	// Model and Engine name what will serve the request.
	Model  string
	Engine string

	Sections []WireSection
	Tools    []WireTool
	Messages []WireMessage

	// Images counts the attached pictures, which ride outside the text
	// sizes and are worth naming when they are there.
	Images int

	// Note is an optional caveat printed under the heading — the host
	// puts the "host-side sizes" caveat there.
	Note string

	// SortByBytes orders each group by size, largest first, instead of
	// by request order. The view's default, because "what is big" is
	// the question.
	SortByBytes bool

	// Width is the terminal width, for truncating the previews.
	Width int
}

// wireTotals sums the three groups.
func (st WireState) wireTotals() (bytes, tokens int) {
	for _, s := range st.Sections {
		bytes += s.Bytes
		tokens += s.Tokens
	}
	for _, t := range st.Tools {
		bytes += t.Bytes
		tokens += t.Tokens
	}
	for _, m := range st.Messages {
		bytes += m.Bytes
		tokens += m.Tokens
	}
	return bytes, tokens
}

// WireHeaderLines renders the view's heading: what is being described,
// its totals, and the caveat.
func WireHeaderLines(st WireState) []string {
	bytes, tokens := st.wireTotals()
	engine := st.Engine
	if engine == "" {
		engine = "pigasus"
	}
	head := "\x1b[1m" + palette.SubtleOpen + "└ Wire request" + "\x1b[0m"
	head += palette.DimGrayOpen + "  ·  " + engine
	if st.Model != "" {
		head += "  ·  " + st.Model
	}
	head += fmt.Sprintf("  ·  %d msgs", len(st.Messages))
	if st.Images > 0 {
		head += fmt.Sprintf("  ·  %d image(s)", st.Images)
	}
	head += palette.DimGrayClose
	lines := []string{head}

	total := palette.DimGrayOpen + "   total  " +
		padLeft(HumanBytes(bytes), 9) + "  " +
		padLeft(tokenfmt.K(tokens)+" tok", 10) + palette.DimGrayClose
	lines = append(lines, total)
	if st.Note != "" {
		lines = append(lines, "   "+palette.DimGrayOpen+st.Note+palette.DimGrayClose)
	}
	return lines
}

// WireRows renders the three groups as foldable rows.
//
// Each group is a row whose children are its members. A member with a
// preview carries it as its body, so opening the member shows the
// message's opening line — enough to identify it without dumping a tool
// result into the panel.
//
// The three groups are laid out on one geometry rather than three: the
// name column is as wide as the widest name in the group *and* the
// group's own name, so a heading's numbers sit in the same columns as
// its members'. A group computing its own width is what put "tools  3
// 3.8 KB" three columns left of "run_command  2.1 KB".
func WireRows(st WireState) []FoldRow {
	sections := append([]WireSection(nil), st.Sections...)
	tools := append([]WireTool(nil), st.Tools...)
	messages := append([]WireMessage(nil), st.Messages...)
	if st.SortByBytes {
		sort.SliceStable(sections, func(i, j int) bool { return sections[i].Bytes > sections[j].Bytes })
		sort.SliceStable(tools, func(i, j int) bool { return tools[i].Bytes > tools[j].Bytes })
		sort.SliceStable(messages, func(i, j int) bool { return messages[i].Bytes > messages[j].Bytes })
	}

	w := wireNameWidth(st)
	rows := make([]FoldRow, 0, 3)
	rows = append(rows, sectionRow("wire/system", "systemInstruction", sectionChildren(sections, w), w))
	rows = append(rows, sectionRow("wire/tools", "tools", toolChildren(tools, w), w))
	rows = append(rows, sectionRow("wire/contents", "contents", messageChildren(messages, st.Width, w), w))
	return rows
}

// sectionRow renders one group heading with its members under it, on the
// view's shared name column.
func sectionRow(id, name string, children []FoldRow, w int) FoldRow {
	bytes, tokens := 0, 0
	for _, c := range children {
		bytes += c.metaBytes
		tokens += c.metaTokens
	}
	return FoldRow{
		ID:       id,
		Text:     groupLabel(name, w, len(children), bytes, tokens),
		Children: children,
	}
}

// The numeric columns, shared by a group heading and its members so the
// numbers form one column per measure down the whole view.
const (
	countWidth = 3
	bytesWidth = 9
	tokWidth   = 10
)

// groupLabel is a group heading: the name, how many members, and the two
// sizes, in the same columns as the members below it.
//
// A group with no members says so instead of printing the shape of a
// measurement: "0 B  - tok" is what an unmeasured value looks like, and
// there is nothing there to have measured.
func groupLabel(name string, width, n, bytes, tokens int) string {
	label := "\x1b[1m" + padRight(name, width) + "\x1b[0m"
	if n == 0 {
		return label + strings.Repeat(" ", countWidth) +
			palette.DimGrayOpen + "(empty)" + palette.DimGrayClose
	}
	return label + fmt.Sprintf("%*d", countWidth, n) +
		palette.DimGrayOpen +
		padLeft(HumanBytes(bytes), bytesWidth) + "  " +
		padLeft(tokenfmt.K(tokens)+" tok", tokWidth) +
		palette.DimGrayClose
}

// memberLabel is one member row: its name and its two sizes.
//
// The name is padded to the group's widest name so the numbers form a
// column; the count column stays blank, because a member's count is
// always one and a column of ones is noise.
func memberLabel(name string, width, bytes, tokens int) string {
	return padRight(name, width) + strings.Repeat(" ", countWidth) +
		palette.DimGrayOpen +
		padLeft(HumanBytes(bytes), bytesWidth) + "  " +
		padLeft(tokenfmt.K(tokens)+" tok", tokWidth) +
		palette.DimGrayClose
}

func sectionChildren(sections []WireSection, w int) []FoldRow {
	names := sectionNames(sections)
	out := make([]FoldRow, 0, len(sections))
	for i, s := range sections {
		out = append(out, FoldRow{
			ID:         "wire/system/" + s.Name,
			Text:       memberLabel(names[i], w, s.Bytes, s.Tokens),
			metaBytes:  s.Bytes,
			metaTokens: s.Tokens,
			Flat:       true,
		})
	}
	return out
}

func toolChildren(tools []WireTool, w int) []FoldRow {
	out := make([]FoldRow, 0, len(tools))
	for _, t := range tools {
		out = append(out, FoldRow{
			ID:         "wire/tools/" + t.Name,
			Text:       memberLabel(t.Name, w, t.Bytes, t.Tokens),
			metaBytes:  t.Bytes,
			metaTokens: t.Tokens,
			Flat:       true,
		})
	}
	return out
}

func messageChildren(messages []WireMessage, width int, w int) []FoldRow {
	out := make([]FoldRow, 0, len(messages))
	for _, m := range messages {
		name := messageName(m)
		row := FoldRow{
			ID:         fmt.Sprintf("wire/msg/%d", m.Index),
			Text:       memberLabel(name, w, m.Bytes, m.Tokens),
			metaBytes:  m.Bytes,
			metaTokens: m.Tokens,
			Flat:       true,
		}
		if m.Preview != "" {
			row.Detail = []string{clipPreview(m.Preview, width)}
		}
		out = append(out, row)
	}
	return out
}

// sectionNames is each section's row name, tags included.
func sectionNames(sections []WireSection) []string {
	names := make([]string, len(sections))
	for i, s := range sections {
		names[i] = "<" + s.Name + ">"
	}
	return names
}

// messageName is a message's row name: its position, role and kind.
func messageName(m WireMessage) string {
	return fmt.Sprintf("#%-3d %-6s %s", m.Index, m.Role, m.Kind)
}

// wireNameWidth is the one name column every group shares.
//
// It is measured over the whole view rather than per group: the three
// groups are one table of measurements, and a column that restarts its
// width per group would step in and out between them. The group names
// are included so a heading's own numbers line up with its members'.
func wireNameWidth(st WireState) int {
	w := 0
	for _, name := range []string{"systemInstruction", "tools", "contents"} {
		if n := ansi.StringWidth(name); n > w {
			w = n
		}
	}
	for _, name := range sectionNames(st.Sections) {
		if n := ansi.StringWidth(name); n > w {
			w = n
		}
	}
	for _, t := range st.Tools {
		if n := ansi.StringWidth(t.Name); n > w {
			w = n
		}
	}
	for _, m := range st.Messages {
		if n := ansi.StringWidth(messageName(m)); n > w {
			w = n
		}
	}
	return w + 2
}

// clipPreview cuts a preview to the panel's width, leaving room for the
// body's indent.
func clipPreview(s string, width int) string {
	s = strings.ReplaceAll(strings.TrimSpace(s), "\n", " ")
	if width <= 12 {
		if len(s) > 80 {
			return s[:80] + "…"
		}
		return s
	}
	limit := width - 8
	if limit < 20 {
		limit = 20
	}
	r := []rune(s)
	if len(r) > limit {
		return string(r[:limit]) + "…"
	}
	return s
}
