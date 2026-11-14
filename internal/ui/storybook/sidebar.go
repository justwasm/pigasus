package storybook

import (
	"fmt"
	"strings"
)

// RenderSidebar draws the navigation sidebar with category headers and story rows.
func RenderSidebar(items []NavItem, selectedIdx int, scrollOffset int, width, height int, filtering bool, filterText string, focus FocusArea) string {
	var lines []string

	// Header row: search filter or title
	header := bold(blue(" STORIES"))
	if filtering || filterText != "" {
		header = yellow(" /"+filterText) + dim(" (esc clears)")
	}
	lines = append(lines, header)
	lines = append(lines, dim(strings.Repeat("─", width)))

	// Available content rows for items
	availRows := height - 2
	if availRows < 1 {
		availRows = 1
	}

	// Clamp scroll window around selectedIdx
	if selectedIdx < scrollOffset {
		scrollOffset = selectedIdx
	}
	if selectedIdx >= scrollOffset+availRows {
		scrollOffset = selectedIdx - availRows + 1
	}
	if scrollOffset < 0 {
		scrollOffset = 0
	}

	endIdx := scrollOffset + availRows
	if endIdx > len(items) {
		endIdx = len(items)
	}

	for i := scrollOffset; i < endIdx; i++ {
		item := items[i]
		isSel := i == selectedIdx

		var row string
		if item.IsCategory {
			arrow := "▼"
			catLabel := fmt.Sprintf(" %s %s", arrow, bold(string(item.Category)))
			if isSel {
				row = reverse(catLabel)
			} else {
				row = catLabel
			}
		} else {
			tag := item.Story.AtomicLevelOrDefault().ShortTag()
			prefix := "    • "
			if isSel {
				indicator := "○"
				if focus == FocusPlayground {
					indicator = "●"
				}
				row = reverse("  " + indicator + " " + item.Story.Name + " " + tag)
			} else {
				row = prefix + dim(item.Story.Name) + " " + dim(tag)
			}
		}
		lines = append(lines, row)
	}

	// Fill remaining height with empty rows
	for len(lines) < height {
		lines = append(lines, "")
	}
	if len(lines) > height {
		lines = lines[:height]
	}

	return strings.Join(lines, "\n")
}
