package historypanel

import (
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/ui/tuilib"
)

const (
	panelTitleOpen  = "\x1b[1m\x1b[39m"
	panelTitleClose = "\x1b[0m"
	blueOpen        = "\x1b[38;2;38;139;210m"
	colorClose      = "\x1b[39m"
	dimGrayOpen     = "\x1b[38;2;101;123;131m"
	boldOpen        = "\x1b[1m"
)

// historyListHeight sizes the visible list rows based on terminal height.
func historyListHeight(h int) int {
	if h <= 0 {
		return 8
	}
	const chrome = 9
	v := h - chrome - 1
	if v < 3 {
		v = 3
	}
	if v > 12 {
		v = 12
	}
	return v
}

func firstLineSnippet(s string, max int) string {
	for _, line := range strings.SplitN(s, "\n", 2) {
		line = strings.TrimSpace(line)
		if line != "" {
			if r := []rune(line); len(r) > max {
				return string(r[:max-1]) + "…"
			}
			return line
		}
	}
	return ""
}

func formatRelativeTime(ms int64) string {
	if ms <= 0 {
		return ""
	}
	d := time.Since(time.UnixMilli(ms))
	if d < 0 {
		return ""
	}
	if d < time.Minute {
		return "just now"
	}
	if d < time.Hour {
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	}
	if d < 24*time.Hour {
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	}
	return fmt.Sprintf("%dd ago", int(d.Hours()/24))
}

// Lines renders the full /history panel view.
func (p *Panel) Lines() []string {
	if p == nil {
		return nil
	}

	var lines []string

	// Header: Title
	lines = append(lines, panelTitleOpen+"Prompt History"+panelTitleClose)

	// Scope indicator row
	if p.scope == ScopeWorkspace {
		lines = append(lines, "  "+dimGrayOpen+"Scope: "+colorClose+blueOpen+"[Current Workspace]"+colorClose+dimGrayOpen+"  (tab: All Workspaces)"+colorClose)
	} else {
		lines = append(lines, "  "+dimGrayOpen+"Scope: "+colorClose+blueOpen+"[All Workspaces]"+colorClose+dimGrayOpen+"  (tab: Current Workspace)"+colorClose)
	}

	// Search filter row
	if p.query == "" {
		lines = append(lines, "  "+dimGrayOpen+"Search: "+colorClose+dimGrayOpen+"(type to filter)"+colorClose)
	} else {
		lines = append(lines, "  "+dimGrayOpen+"Search: "+colorClose+boldOpen+p.query+panelTitleClose+blueOpen+"█"+colorClose)
	}

	lines = append(lines, "")

	entries := p.filteredEntries()
	total := len(entries)

	if total == 0 {
		if p.query != "" {
			lines = append(lines, "  "+dimGrayOpen+fmt.Sprintf("No prompts match %q", p.query)+colorClose)
		} else {
			lines = append(lines, "  "+dimGrayOpen+"No prompt history found."+colorClose)
		}
		lines = append(lines, "")
		lines = append(lines, tuilib.PanelKeyboardHint("tab", "Scope", "esc", "Dismiss"))
		return lines
	}

	p.clamp()
	visible := p.visible(total)
	window := p.window

	// Render prompt list items
	for i := 0; i < visible; i++ {
		idx := window + i
		if idx >= total {
			break
		}
		e := entries[idx]
		snippet := firstLineSnippet(e.Text, 60)

		var extraTag string
		lineCount := strings.Count(e.Text, "\n")
		if lineCount > 0 {
			extraTag += fmt.Sprintf(" [+%d lines]", lineCount)
		}
		if tStr := formatRelativeTime(e.Timestamp); tStr != "" {
			extraTag += " " + tStr
		}

		if idx == p.cursor {
			lines = append(lines, blueOpen+fmt.Sprintf("> %d. %s", idx+1, snippet)+extraTag+colorClose)
		} else {
			lines = append(lines, fmt.Sprintf("  %d. %s", idx+1, snippet)+dimGrayOpen+extraTag+colorClose)
		}
	}

	// Preview section for currently selected prompt if multi-line or long
	if cur, ok := p.currentEntry(); ok {
		split := strings.Split(strings.TrimSpace(cur.Text), "\n")
		lines = append(lines, "")
		lines = append(lines, dimGrayOpen+"  Preview:"+colorClose)
		previewCount := 3
		if len(split) < previewCount {
			previewCount = len(split)
		}
		for j := 0; j < previewCount; j++ {
			trimmedLine := strings.TrimSpace(split[j])
			if r := []rune(trimmedLine); len(r) > 70 {
				trimmedLine = string(r[:69]) + "…"
			}
			lines = append(lines, "    "+trimmedLine)
		}
		if len(split) > previewCount {
			lines = append(lines, dimGrayOpen+fmt.Sprintf("    ... (+%d more lines)", len(split)-previewCount)+colorClose)
		}
	}

	lines = append(lines, "")

	if total > visible {
		lines = append(lines, dimGrayOpen+fmt.Sprintf("  (%d–%d of %d prompts)", window+1, window+visible, total)+colorClose)
	}

	lines = append(lines, tuilib.PanelKeyboardHint("↑/↓", "Navigate", "enter", "Select", "tab", "Scope", "esc", "Dismiss"))

	return lines
}
