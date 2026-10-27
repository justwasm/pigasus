// picker_view.go — render the tuilib picker overlay above the prompt
// block. Cycle 74 / 126.
//
// /tmp/agy_slash.txt captured state when the user typed "/" into the
// prompt (v1.3.5 binary 1:1):
//
//	> /
//	───────────────────────────────────────────────────────────────────────
//	> /add-dir          Add a directory to the workspace
//	  /agents           List available custom agents
//	  /artifact         View and review artifacts
//	  /btw              Ask a side question without interrupting the current task
//	  /changelog        Show release notes and changes
//	  /compact          Compact the conversation now (cycle 237, AGY 1.4.2)
//	   ↓ 89 more
//
//	  ↑/↓ Navigate · enter Select · tab Complete
//	esc to cancel                                          Gemini 3.8 Flash · high
//
// Cycle 126 alignment with the v1.3.6 ref capture:
//   - Highlighted row: "> " is part of the command name in true-colour
//     blue (38;2;66;133;244) — same as the brand blue
//   - Non-highlighted: "  " prefix + name (no color), dim gray desc
//   - Name column padded to 51 chars ("> " + 49 or "  " + 49)
//   - "↓ N more" tail row in dim gray (same as descriptions)
//   - Blank row between "↓ N more" and the hint row
//   - "esc to cancel" stays in statusRow when picker is visible
//
// The original cycle-74 capture used 256-colour 38;5;69/238/241;
// cycle 126 normalises them to the recovered 24-bit RGB palette
// (see colors.go). The visible byte-for-byte shape is unchanged in
// tmux's colour cube (same shade), but a real Terminal distinguishes
// the two.
package cliapp

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/tuilib"
)

// Cycle 91 / 126: picker hint row ANSI escape sequences. The
// v1.3.6 ref binary emits 24-bit RGB blue (38;2;66;133;244) for
// the keys and 24-bit RGB dim gray (38;2;95;99;104) for the
// labels — matching the prompt and the tab-bar brand palette.
// See colors.go.
var (
	pickerNameHi = cBlueOpen
	// Cycle 283: the reference renders picker descriptions and the
	// "↓ N more" counter in Solarized base01 #586E75 (verified from a
	// truecolour capture of the `/` picker: 38;2;88;110;117), one step
	// dimmer than the labels.
	pickerDescDim  = cSubtleOpen  // #586E75 for descriptions
	pickerLabelDim = cDimGrayOpen // dim gray for keyboard labels (same gray)
	pickerKeyOpen  = "\x1b[1m" + cBlueOpen
)

const (
	pickerKeyClose = "\x1b[0m"
	pickerReset    = "\x1b[39m"
)

// Cycle 131: picker description column width. The reference's
// description column is exactly 48 characters wide; descriptions
// that exceed it are truncated to 45 chars + "..." (recovered from
// /tmp/agycmp/hotpath/picker/ref.001-down5.txt where descriptions
// like `Ask a side question without interrupting the current task`
// (57 chars) truncates to `Ask a side question without interrupting the ...`
// (45 + `...`), and `Search code in the workspace (usage: /codesearch <query>)`
// (57 chars) truncates to `Search code in the workspace (usage: /codesea...`
// (45 + `...`).
const pickerDescWidth = 48

// truncatePickerDesc returns desc with at most pickerDescWidth
// characters, appending "..." when truncation occurred. Descriptions
// that already fit are returned unchanged. Cycle 131's width cap
// keeps the picker rows from overflowing into the hint row below.
func truncatePickerDesc(desc string) string {
	if len(desc) <= pickerDescWidth {
		return desc
	}
	return desc[:pickerDescWidth-3] + "..."
}

// pickerView renders the picker overlay body, returning the rows that
// go above the prompt block. Returns "" when the picker is hidden.
//
// Cycle 129: when the highlight (picker.Idx()) advances past the
// visible window (5 rows), the renderer needs to scroll the window
// so the highlight stays visible. pickerOffset is the index of the
// first row drawn; the loop walks [offset, offset+visible) and
// matches highlight against picker.Idx() == i. The "↓ N more" tail
// reflects how many rows lie past the visible window.
//
// Cycle 130: the reference centres the highlight in the visible
// window when the highlight passes the bottom row, instead of
// pinning it to the bottom (the previous pager-style behaviour).
// After pressing Down 5× with the picker on "/", the highlight
// lands on /clear (idx 5) and the reference's window shows rows
// [3..7] — `/btw`, `/changelog`, `> /clear (new)` (highlighted),
// `/codesearch`, `/config` — with "↑ 3 more" above the window
// and "↓ 91 more" below. Net: window_offset = idx - (visible/2).
// We replicate that here.
//
// Cycle 232: as a side effect, populates m.pickerLayoutLocal with the
// internal layout (row count, list-row range, hint row, scroll-tail
// flags). viewBody reads this back to derive the screen rectangle of
// the picker overlay so the mouse handlers can map a (x, y) screen
// coordinate onto a picker list row.
func computePickerOffset(hi, visible, total int) int {
	if hi < 0 {
		hi = 0
	}
	wantOffset := 0
	if hi >= visible-1 {
		wantOffset = hi - visible/2
	}
	if wantOffset > hi {
		wantOffset = hi
	}
	if wantOffset > total-visible {
		wantOffset = total - visible
	}
	if wantOffset < 0 {
		wantOffset = 0
	}
	return wantOffset
}

func (m *runMode) renderPickerEmpty(b *strings.Builder, rowWidth int) string {
	b.WriteString(pickerDescDim)
	b.WriteString("   No matches")
	b.WriteString(strings.Repeat(" ", rowWidth-13))
	b.WriteString(pickerReset)
	b.WriteByte('\n')
	b.WriteByte('\n')
	b.WriteString("  ")
	b.WriteString(pickerKeyOpen + "\u2191/\u2193" + pickerKeyClose)
	b.WriteString(pickerLabelDim + " Navigate \u00b7 ")
	b.WriteString(pickerKeyOpen + "enter" + pickerKeyClose)
	b.WriteString(pickerLabelDim + " Select \u00b7 ")
	b.WriteString(pickerKeyOpen + "tab" + pickerKeyClose)
	b.WriteString(pickerLabelDim + " Complete" + pickerReset + "\n")
	m.pickerLayoutLocal.Rows = 3
	m.pickerLayoutLocal.HintRow = 2
	return b.String()
}

func renderPickerRow(b *strings.Builder, c *tuilib.SlashCommand, isHi bool) {
	aliasSuffix := ""
	if len(c.Aliases) > 0 {
		aliasSuffix = " (" + c.Aliases[0] + ")"
	}
	desc := truncatePickerDesc(c.Description)
	const nameWidth = 51
	if isHi {
		name := "> " + c.Name + aliasSuffix
		for len(name) < nameWidth {
			name += " "
		}
		b.WriteString(pickerNameHi)
		b.WriteString(name)
		b.WriteString(pickerReset)
		b.WriteString(pickerDescDim)
		b.WriteString(desc)
		b.WriteString(pickerReset)
		b.WriteString("\n")
		return
	}
	name := "  " + c.Name + aliasSuffix
	for len(name) < nameWidth {
		name += " "
	}
	b.WriteString(name)
	b.WriteString(pickerDescDim)
	b.WriteString(desc)
	b.WriteString(pickerReset)
	b.WriteString("\n")
}

// modelPickerNameWidth is the width of the label column in the
// "/model <query>" popup.
//
// 【一手】 agy 1.4.6, live tmux capture: the reference pads
// "marker + label" to 30 columns and puts the catalogue id two columns
// further right, in the same dim gray the command descriptions use:
//
//	> Gemini 3.8 Flash (High)     gemini-3.8-flash-high
//	  Claude Sonnet 4.6 (Thinking)  claude-sonnet-4-6
//
// The highlighted row colours the 30-column field (and only that field);
// the two-column gap stays uncoloured.
const modelPickerNameWidth = 30

// renderModelCompletionRow renders one row of the "/model <query>" popup: the
// display label in the 30-column field, the catalogue id in dim gray.
func renderModelCompletionRow(b *strings.Builder, c *tuilib.SlashCommand, isHi bool) {
	name := "  " + c.Name
	if isHi {
		name = "> " + c.Name
	}
	if len(name) < modelPickerNameWidth {
		name += strings.Repeat(" ", modelPickerNameWidth-len(name))
	}
	if isHi {
		b.WriteString(pickerNameHi)
		b.WriteString(name)
		b.WriteString(pickerReset)
	} else {
		b.WriteString(name)
	}
	b.WriteString("  ")
	b.WriteString(pickerDescDim)
	b.WriteString(c.Description)
	b.WriteString(pickerReset)
	b.WriteString("\n")
}

func (m *runMode) renderPickerList(b *strings.Builder, visible, total int) int {
	rowIdx := 0
	if m.pickerOffset > 0 {
		more := m.pickerOffset
		b.WriteString(pickerDescDim + "   \u2191 ")
		if more == 1 {
			b.WriteString("1 more" + pickerReset + "\n")
		} else {
			fmt.Fprintf(b, "%d more%s\n", more, pickerReset)
		}
		m.pickerLayoutLocal.UpMore = true
		rowIdx++
	}
	m.pickerLayoutLocal.ListTop = rowIdx
	for i := 0; i < visible && i+m.pickerOffset < total; i++ {
		row := i + m.pickerOffset
		c := m.picker.SelectedAt(row)
		if c == nil {
			break
		}
		if m.picker.Kind() == tuilib.PickerKindModel {
			renderModelCompletionRow(b, c, row == m.picker.Idx())
		} else {
			renderPickerRow(b, c, row == m.picker.Idx())
		}
		rowIdx++
	}
	m.pickerLayoutLocal.ListBottom = rowIdx - 1
	m.pickerLayoutLocal.HasList = total > 0
	rowsBelowWindow := total - (m.pickerOffset + visible)
	if rowsBelowWindow > 0 {
		b.WriteString(pickerDescDim + "   \u2193 ")
		if rowsBelowWindow == 1 {
			b.WriteString("1 more" + pickerReset + "\n")
		} else {
			fmt.Fprintf(b, "%d more%s\n", rowsBelowWindow, pickerReset)
		}
		m.pickerLayoutLocal.DownMore = true
		rowIdx++
	}
	return rowIdx
}

func (m *runMode) renderPickerFooter(b *strings.Builder, rowIdx int) {
	b.WriteString("\n")
	rowIdx++
	b.WriteString("  ")
	b.WriteString(pickerKeyOpen + "\u2191/\u2193" + pickerKeyClose)
	b.WriteString(pickerLabelDim + " Navigate \u00b7 ")
	b.WriteString(pickerKeyOpen + "enter" + pickerKeyClose)
	b.WriteString(pickerLabelDim + " Select \u00b7 ")
	b.WriteString(pickerKeyOpen + "tab" + pickerKeyClose)
	b.WriteString(pickerLabelDim + " Complete" + pickerReset + "\n")
	m.pickerLayoutLocal.HintRow = rowIdx
	rowIdx++
	m.pickerLayoutLocal.Rows = rowIdx
}

func (m *runMode) pickerView() string {
	var b strings.Builder
	const visible = 5
	const rowWidth = 220
	m.pickerLayoutLocal = pickerLayout{}
	if !m.picker.Visible() {
		return ""
	}
	total := m.picker.Len()
	m.pickerOffset = computePickerOffset(m.picker.Idx(), visible, total)
	if m.picker.Empty() {
		return m.renderPickerEmpty(&b, rowWidth)
	}
	rowIdx := m.renderPickerList(&b, visible, total)
	m.renderPickerFooter(&b, rowIdx)
	return b.String()
}

type pickerLayout = tuilib.PickerLayout
type pickerGeom = tuilib.PickerGeom

func (m *runMode) commitPickerSelectionToBuffer() bool {
	sel := m.picker.Selected()
	if sel == nil {
		return false
	}
	if m.picker.Kind() == tuilib.PickerKindModel {
		// The "/model <query>" popup completes the *argument*: the
		// composer keeps the command and gains the display label.
		// 【一手】 agy 1.4.6, live: "/model oss" + tab → "/model
		// GPT-OSS 120B (Medium)", and the popup closes.
		m.input.SetValue("/model " + sel.Name)
		m.picker.Reset()
		return true
	}
	m.input.SetValue(sel.Name)
	m.picker.Update([]rune(sel.Name))
	return true
}

func (m *runMode) handlePickerClick(ev tea.Mouse) bool {
	if !m.altScreen {
		return false
	}
	x, y := ev.X, ev.Y

	if m.isInputY(y) {
		m.picker.Update([]rune(firstLine(m.input.Value())))
		return false
	}

	if rowOffset, ok := m.pickerGeom.ListRowOffset(x, y, m.width); ok {
		idx := m.pickerLayoutLocal.ListTop + rowOffset
		if !m.picker.SetIdx(idx) {
			return false
		}
		m.commitPickerSelectionToBuffer()
		return true
	}

	return false
}

func (m *runMode) handlePickerHover(ev tea.Mouse) bool {
	if !m.altScreen {
		return false
	}
	if rowOffset, ok := m.pickerGeom.ListRowOffset(ev.X, ev.Y, m.width); ok {
		idx := m.pickerLayoutLocal.ListTop + rowOffset
		return m.picker.SetIdx(idx)
	}
	return false
}

func (m *runMode) handlePickerWheel(ev tea.Mouse) bool {
	if !m.altScreen || !m.pickerGeom.ContainsWheel(ev.Y) {
		return false
	}
	switch ev.Button {
	case tea.MouseWheelUp:
		return m.picker.MoveUp()
	case tea.MouseWheelDown:
		return m.picker.MoveDown()
	}
	return false
}
