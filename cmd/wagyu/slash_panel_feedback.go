package main

import (
	tea "charm.land/bubbletea/v2"
)

// feedbackCategories are the six categories the reference lists in stage 1
// of the /feedback panel. Verbatim from
// /tmp/agycmp/panels/ppref.feedback.raw.txt rows 5–10. Selecting a category
// and pressing Enter transitions REF to a "Review Submission Data" stage
// (free-form textarea + ctrl+s Submit); the openagy clone replicates the
// form byte-for-byte (cycle 208) and additionally binds Enter to submit
// in stage 2 (cycle 221) — see handleFeedbackTextKey.
var feedbackCategories = [6]string{
	"1. Code doesn't work (e.g., syntax errors, bad logic)",
	"2. Repeated itself (e.g., stuck running the same tool over again)",
	"3. Took too many steps (e.g., progress was much too slow)",
	"4. Ignored instructions (e.g., missed specific constraints)",
	"5. Got stuck (e.g., unable to recover from an error)",
	"6. Other",
}

// panelFeedbackLines renders the /feedback stage-1 category picker. The
// selected row is prefixed with `> ` in the panel's blue; everything else
// is plain. Layout recovered from ppref.feedback.raw.txt:
//   - bold "Submit Feedback" title
//   - blank
//   - "What went wrong? Select a category to assist your report:" (plain)
//   - blank
//   - six category rows
//   - blank
//   - "  esc Cancel · 1-6 Select & Continue · enter Continue" dot hint
//
// Cycle 201 added stage 2 (the free-text form). Cycle 221 closes the
// TODO §4 / 2.1 follow-up: stage 2 is now reachable from stage 1, the
// buffer fills on typing, backspace deletes, Enter (and ctrl+s) submit
// and echo the cycle-192 local-only line, and Esc returns to stage 1
// with the same category still selected.
func (m *runMode) panelFeedbackLines() []string {
	if m.panel.stage > 0 {
		return m.panelFeedbackStage2()
	}
	lines := []string{
		panelTitleOpen + "Submit Feedback" + panelTitleClose,
		"",
		"What went wrong? Select a category to assist your report:",
		"",
	}
	for i, c := range feedbackCategories {
		if i == m.panel.cursor {
			lines = append(lines, cBlueOpen+"> "+c+cBlueClose)
		} else {
			lines = append(lines, "  "+c)
		}
	}
	lines = append(lines,
		"",
		panelDotHint("  ", "esc", "Cancel", "1-6", "Select & Continue", "enter", "Continue"),
	)
	return lines
}

// panelFeedbackStage2 renders the /feedback free-text stage. Cycle 208
// captured REF's real form (fin_fb_stage2.raw.txt); the lines now come
// from feedbackStage2Lines. The old body was marked 【推测】.
func (m *runMode) panelFeedbackStage2() []string {
	return m.feedbackStage2Lines()
}


func (m *runMode) handleFeedbackTextKey(key tea.Key) (bool, tea.Cmd) {
	switch {
	case key.Code == tea.KeyEsc:
		m.panel.stage = 0
		m.panel.text = ""
		return true, nil
	case key.Code == tea.KeyEnter:
		// Cycle 221: Enter submits (openagy has no upload endpoint;
		// the cycle-192-equivalent local-only line is the only outcome
		// the user can observe, and forcing a separate ctrl+s key
		// would be a UX dead-end). m.panel.cursor holds the
		// stage-1 category index and is preserved by submitFeedback's
		// blank reset, so a subsequent /feedback reopens on the same
		// row the user picked.
		return true, m.submitFeedback()
	case key.Code == 's' && key.Mod.Contains(tea.ModCtrl):
		// ctrl+s is kept as a parallel submit key — REF advertises
		// it in the legend and the existing cycle-208 path was a no-op
		// to remove.
		return true, m.submitFeedback()
	case key.Code == tea.KeyBackspace:
		if r := []rune(m.panel.text); len(r) > 0 {
			m.panel.text = string(r[:len(r)-1])
		}
		return true, nil
	}
	if isTypedText(key) {
		m.panel.text += key.Text
		return true, nil
	}
	if key.Mod == 0 {
		return true, nil
	}
	return false, nil
}

// submitFeedback closes the /feedback panel after stage 2 and records
// the same local-only message the non-panel handler in slash_commands.go
// emits (the clone has no upload endpoint). Bound to both Enter and
// ctrl+s — see handleFeedbackTextKey for the rationale.
func (m *runMode) submitFeedback() tea.Cmd {
	m.panel = slashPanel{}
	m.slashRespond("/feedback", "Feedback recorded (openagy has no upload endpoint; saved locally only)")
	return nil
}

