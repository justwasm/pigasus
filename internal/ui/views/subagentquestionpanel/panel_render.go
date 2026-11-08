// panel_render.go — the picker's two row sets.
//
// The alert rows are the status line's badge: one row per scope that
// has a pending question, so the user knows a subagent is waiting even
// when the picker is closed. The picker rows are the overlay itself:
// the scope's questions, the options of the highlighted one, and the
// keyboard hints.
//
// The rows are read fresh on every draw, so a question that was
// answered while the picker was open disappears from the next frame.

package subagentquestionpanel

import (
	"sort"

	subagentquestions "github.com/justwasm/pigasus/internal/app/subagent/questions"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/palette"
)

// AlertRows is the slice of status-row fragments the host appends when
// the picker is closed.
//
// Each pending question on each scope renders one row, sorted by scope
// (the parent first, then the subagents in alphabetical order) so the
// badge is stable across renders.
func (p *Panel) AlertRows() []string {
	if p == nil || p.seams.Manager == nil {
		return nil
	}
	all := p.seams.Manager.AllPending()
	if len(all) == 0 {
		return nil
	}
	scopes := make([]string, 0, len(all))
	for s := range all {
		scopes = append(scopes, s)
	}
	// Sort so the parent ("") renders first, then the subagents in
	// alphabetical order. The empty string sorts first in the natural
	// string ordering, so a plain sort gets the right shape.
	sort.Strings(scopes)
	var rows []string
	for _, scope := range scopes {
		pending := all[scope]
		rows = append(rows,
			palette.WarningBlueOpen+"  ⎿  "+palette.WarningBlueClose+
				palette.DimGrayOpen+scopeLabel(scope)+
				" has a question · "+
				tuilib.FirstLine(pending[0].Prompt)+palette.DimGrayClose)
	}
	return rows
}

// PickerRows renders the picker overlay.
//
// The rows are appended after the prompt and before the status row, so
// the picker sits directly above the status indicator (matching the
// resume picker's shape).
func (p *Panel) PickerRows() []string {
	if p == nil || !p.open || p.seams.Manager == nil {
		return nil
	}
	pending := p.pending()
	scope := scopeLabel(p.scope)
	var rows []string
	rows = append(rows,
		palette.WarningBlueOpen+"Pending questions for "+scope+palette.WarningBlueClose,
		"",
	)
	if len(pending) == 0 {
		rows = append(rows,
			palette.DimGrayOpen+"  No pending questions."+palette.DimGrayClose)
	} else {
		rows = append(rows, p.renderItems(pending)...)
	}
	rows = append(rows,
		"",
		palette.DimGrayOpen+"Keyboard: "+palette.DimGrayClose+
			palette.BlueOpen+"↑/↓"+palette.WarningBlueClose+
			palette.DimGrayOpen+" Questions  "+palette.DimGrayClose+
			palette.BlueOpen+"←/→"+palette.WarningBlueClose+
			palette.DimGrayOpen+" Options  "+palette.DimGrayClose+
			palette.BlueOpen+"enter"+palette.WarningBlueClose+
			palette.DimGrayOpen+" Select  "+palette.DimGrayClose+
			palette.BlueOpen+"esc"+palette.WarningBlueClose+
			palette.DimGrayOpen+" Skip"+palette.DimGrayClose,
	)
	return rows
}

// renderItems renders the picker's question rows.
//
// cycle feature/ask-question-left-right-nav: when the highlighted
// question has a non-empty Options list, the options are rendered as an
// indented sub-list directly under the row. Each option keeps a leading
// marker that follows the question's highlight, and the option cursor
// moves with Left/Right (with bounds clamping). The surrounding picker's
// shape (one row per pending question, hint row at the bottom) is
// unchanged.
func (p *Panel) renderItems(pending []*subagentquestions.Question) []string {
	var rows []string
	for i, q := range pending {
		marker := "  "
		if i == p.cursor {
			marker = palette.BlueOpen + "> " + palette.WarningBlueClose
		}
		prompt := tuilib.FirstLine(q.Prompt)
		if len(prompt) > 60 {
			prompt = prompt[:57] + "..."
		}
		rows = append(rows, marker+palette.DimGrayOpen+prompt+palette.DimGrayClose)
		if i != p.cursor {
			continue
		}
		for oi, opt := range q.Options {
			optRune := "  "
			if oi == p.optionCursor {
				optRune = palette.BlueOpen + "› " + palette.DimGrayClose
			}
			row := "    " + optRune + tuilib.FirstLine(opt)
			rows = append(rows, row+palette.DimGrayClose)
		}
		// The write-in row is the last row of the option list: it lets
		// the user answer in their own words instead of choosing one of
		// the offered options.
		writeInRune := "  "
		if p.optionCursor >= len(q.Options) {
			writeInRune = palette.BlueOpen + "› " + palette.DimGrayClose
		}
		label := "Write-in answer…"
		if len(q.Options) == 0 {
			label = "Type your answer…"
		}
		rows = append(rows, "    "+writeInRune+palette.DimGrayOpen+label+palette.DimGrayClose)
	}
	return rows
}
