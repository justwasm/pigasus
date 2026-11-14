package storybook

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// RenderCanvas draws the preview canvas area with breadcrumb header, component viewport, and controls bar.
func RenderCanvas(story *Story, activeState int, currentTheme string, focus FocusArea, viewContent string, width, height int) string {
	if story == nil {
		return renderEmptyCanvas(width, height)
	}

	var lines []string

	// 1. Breadcrumb & upstream header bar with hollow (○) / solid (●) circle indicator
	var focusIcon string
	var focusHint string
	if focus == FocusPlayground {
		focusIcon = green(bold("● "))
		focusHint = green(bold("(INTERACTIVE · Esc to return)"))
	} else {
		focusIcon = dim("○ ")
		focusHint = dim("(Enter to interact)")
	}

	catStr := dim(string(story.Category) + " > ")
	nameStr := bold(blue(story.Name))
	levelBadge := yellow(story.AtomicLevelOrDefault().LevelTag())
	symStr := ""
	if story.UpstreamSym != "" {
		symStr = dim("[" + story.UpstreamSym + "]")
	}

	headerLeft := " " + focusIcon + catStr + nameStr + " " + levelBadge + " " + focusHint
	headerRight := symStr + " "
	headerLeftW := ansi.StringWidth(headerLeft)
	headerRightW := ansi.StringWidth(headerRight)

	gapW := width - headerLeftW - headerRightW
	if gapW < 1 {
		gapW = 1
	}
	headerRow := headerLeft + strings.Repeat(" ", gapW) + headerRight
	lines = append(lines, headerRow)

	sepLine := dim(strings.Repeat("─", width))
	if focus == FocusPlayground {
		sepLine = cyan(strings.Repeat("─", width))
	}
	lines = append(lines, sepLine)

	// 2. Controls & States row (at bottom)
	// Reserve 2 rows at bottom for controls & help
	contentHeight := height - 4
	if contentHeight < 2 {
		contentHeight = 2
	}

	// 3. Component Content Area
	contentLines := strings.Split(viewContent, "\n")
	for r := 0; r < contentHeight; r++ {
		if r < len(contentLines) {
			lines = append(lines, contentLines[r])
		} else {
			lines = append(lines, "")
		}
	}

	// 4. Separator above controls
	lines = append(lines, dim(strings.Repeat("─", width)))

	// 5. Controls & States line
	var stateBadges []string
	for i, st := range story.States {
		num := i + 1
		badge := fmt.Sprintf("[%d] %s", num, st.Name)
		if i == activeState {
			badge = reverse(bold(" " + badge + " "))
		} else {
			badge = dim(badge)
		}
		stateBadges = append(stateBadges, badge)
	}
	statesBar := " " + strings.Join(stateBadges, "  ")

	modeBadge := ""
	if focus == FocusPlayground {
		modeBadge = green(bold(" ● INTERACTIVE PLAYGROUND: Press Esc to return "))
	} else {
		modeBadge = dim(" ○ [Enter: Interact | Tab: State | T: Theme | q: Quit] ")
	}

	stateW := ansi.StringWidth(statesBar)
	modeW := ansi.StringWidth(modeBadge)
	ctrlGap := width - stateW - modeW
	if ctrlGap < 1 {
		ctrlGap = 1
	}
	controlsRow := statesBar + strings.Repeat(" ", ctrlGap) + modeBadge
	lines = append(lines, controlsRow)

	return strings.Join(lines, "\n")
}

func renderEmptyCanvas(width, height int) string {
	var lines []string
	lines = append(lines, dim(" Select a component from the sidebar to inspect"))
	lines = append(lines, dim(strings.Repeat("─", width)))
	for len(lines) < height {
		lines = append(lines, "")
	}
	return strings.Join(lines, "\n")
}
