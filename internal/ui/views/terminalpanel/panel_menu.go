package terminalpanel

import (
	"fmt"
	"strings"
	"unicode"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
)

type menuItemKind int

const (
	menuItemNew menuItemKind = iota
	menuItemRename
	menuItemClose
	menuItemCopyMode
	menuItemDetach
	menuItemQuit
	menuItemSwitchTab
)

type menuItem struct {
	kind     menuItemKind
	label    string
	shortcut string
	tabID    string
	active   bool
	exited   bool
}

func isMenuToggleKey(key tea.Key) bool {
	if (key.Code == 'm' || key.Code == 'M') && key.Mod.Contains(tea.ModAlt) && !key.Mod.Contains(tea.ModCtrl) {
		return true
	}
	if key.Code == tea.KeyF10 && key.Mod == 0 {
		return true
	}
	return false
}

// MenuOpen reports whether the interactive terminal actions menu is currently open.
func (p *Panel) MenuOpen() bool {
	if p == nil {
		return false
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.menuOpen
}

// SetMenuOpen opens or closes the interactive terminal actions menu.
func (p *Panel) SetMenuOpen(open bool) {
	if p == nil {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.menuOpen = open
	if !open {
		p.menuRenaming = false
		p.menuRenameBuf = ""
	} else {
		p.menuCursor = 0
	}
}

func (p *Panel) activeTabIDLocked() string {
	for _, tab := range p.tabsLocked() {
		if tab.Active {
			return tab.ID
		}
	}
	return ""
}

func (p *Panel) menuItemsLocked() []menuItem {
	items := []menuItem{
		{kind: menuItemNew, label: "New Terminal", shortcut: "Alt+N"},
		{kind: menuItemRename, label: "Rename Session", shortcut: ""},
		{kind: menuItemClose, label: "Close Session", shortcut: "Alt+W"},
		{kind: menuItemCopyMode, label: "Copy Mode", shortcut: "Alt+C"},
	}
	if !p.seams.PassThroughEsc && (p.seams.Detach != nil || p.seams.Close != nil) {
		items = append(items, menuItem{kind: menuItemDetach, label: "Detach Panel", shortcut: "Esc"})
	}
	if p.seams.Quit != nil {
		items = append(items, menuItem{kind: menuItemQuit, label: "Quit Terminal", shortcut: "Alt+Q"})
	}
	tabs := p.tabsLocked()
	for i, tab := range tabs {
		label := tab.Title
		if label == "" {
			label = tab.ID
		}
		if label == "" {
			label = fmt.Sprintf("term-%d", i+1)
		}
		items = append(items, menuItem{
			kind:   menuItemSwitchTab,
			label:  fmt.Sprintf("Switch: %d:%s", i+1, label),
			tabID:  tab.ID,
			active: tab.Active,
			exited: tab.Exited,
		})
	}
	return items
}

func (p *Panel) handleMenuKeyLocked(key tea.Key) (handled bool, cmd tea.Cmd, unlockAndRun func() tea.Cmd) {
	if p.menuRenaming {
		switch key.Code {
		case tea.KeyEsc:
			p.menuRenaming = false
			p.menuRenameBuf = ""
			return true, nil, nil
		case tea.KeyEnter:
			newTitle := strings.TrimSpace(p.menuRenameBuf)
			activeID := p.activeTabIDLocked()
			renameFn := p.seams.Rename
			if newTitle != "" {
				p.title = newTitle
			}
			p.menuRenaming = false
			p.menuRenameBuf = ""
			p.menuOpen = false
			if newTitle != "" && renameFn != nil {
				return true, nil, func() tea.Cmd {
					renameFn(activeID, newTitle)
					return nil
				}
			}
			return true, nil, nil
		case tea.KeyBackspace:
			runes := []rune(p.menuRenameBuf)
			if len(runes) > 0 {
				p.menuRenameBuf = string(runes[:len(runes)-1])
			}
			return true, nil, nil
		default:
			if key.Text != "" && !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) {
				p.menuRenameBuf += key.Text
			} else if key.Code > 0 && unicode.IsPrint(key.Code) && !key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) {
				p.menuRenameBuf += string(key.Code)
			}
			return true, nil, nil
		}
	}

	items := p.menuItemsLocked()
	if len(items) == 0 {
		p.menuOpen = false
		return true, nil, nil
	}
	if p.menuCursor < 0 {
		p.menuCursor = 0
	}
	if p.menuCursor >= len(items) {
		p.menuCursor = len(items) - 1
	}

	switch {
	case key.Code == tea.KeyEsc || isMenuToggleKey(key):
		p.menuOpen = false
		return true, nil, nil
	case key.Code == tea.KeyUp || (!key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) && (key.Code == 'k' || key.Text == "k")):
		if p.menuCursor > 0 {
			p.menuCursor--
		} else {
			p.menuCursor = len(items) - 1
		}
		return true, nil, nil
	case key.Code == tea.KeyDown || (!key.Mod.Contains(tea.ModCtrl) && !key.Mod.Contains(tea.ModAlt) && (key.Code == 'j' || key.Text == "j")):
		p.menuCursor = (p.menuCursor + 1) % len(items)
		return true, nil, nil
	case key.Code == tea.KeyEnter || key.Code == tea.KeySpace || key.Text == " ":
		return true, nil, p.executeMenuItemLocked(items[p.menuCursor])
	}
	return true, nil, nil
}

func (p *Panel) executeMenuItemLocked(item menuItem) func() tea.Cmd {
	switch item.kind {
	case menuItemNew:
		p.menuOpen = false
		newFn := p.seams.NewTab
		if newFn != nil {
			return newFn
		}
	case menuItemRename:
		curTitle := p.title
		if curTitle == "" && p.seams.Title != nil {
			curTitle = p.seams.Title()
		}
		p.menuRenaming = true
		p.menuRenameBuf = curTitle
		return nil
	case menuItemClose:
		p.menuOpen = false
		activeID := p.activeTabIDLocked()
		closeTabFn := p.seams.CloseTab
		closeFn := p.seams.Close
		if activeID != "" && closeTabFn != nil {
			return func() tea.Cmd { return closeTabFn(activeID) }
		}
		if closeFn != nil {
			return closeFn
		}
	case menuItemCopyMode:
		p.menuOpen = false
		p.enterCopyModeLocked(copyVisualNone)
		return nil
	case menuItemDetach:
		p.menuOpen = false
		detachFn := p.seams.Detach
		closeFn := p.seams.Close
		if detachFn != nil {
			return detachFn
		}
		if closeFn != nil {
			return closeFn
		}
	case menuItemQuit:
		p.menuOpen = false
		quitFn := p.seams.Quit
		if quitFn != nil {
			return quitFn
		}
	case menuItemSwitchTab:
		p.menuOpen = false
		switchFn := p.seams.SwitchTab
		tabID := item.tabID
		if switchFn != nil && tabID != "" {
			return func() tea.Cmd { return switchFn(tabID) }
		}
	}
	return nil
}

func (p *Panel) overlayMenuLocked(lines []string, bodyStartRow, width int) []string {
	if !p.menuOpen || len(lines) == 0 || width < 20 {
		return lines
	}
	items := p.menuItemsLocked()
	if len(items) == 0 {
		return lines
	}
	if p.menuCursor < 0 {
		p.menuCursor = 0
	}
	if p.menuCursor >= len(items) {
		p.menuCursor = len(items) - 1
	}

	boxW := 42
	if width-2 < boxW {
		boxW = width - 2
	}
	if boxW < 20 {
		return lines
	}
	innerW := boxW - 2

	var boxRows []string
	title := " Terminal Menu "
	topFill := innerW - ansi.StringWidth(title)
	if topFill < 0 {
		topFill = 0
	}
	boxRows = append(boxRows, ansiCyan+"┌"+ansiBold+title+ansiReset+ansiCyan+strings.Repeat("─", topFill)+"┐"+ansiReset)

	// Session info header inside menu (holds metadata moved out of borders in OmniBar mode)
	if !p.menuRenaming && len(lines) >= 8 {
		sessTitle := p.title
		if sessTitle == "" && p.seams.Title != nil {
			sessTitle = p.seams.Title()
		}
		if sessTitle == "" {
			sessTitle = "Terminal"
		}
		curX, curY := 0, 0
		if p.term != nil {
			curX, curY = p.term.Cursor.X, p.term.Cursor.Y
		}
		infoLine1 := fmt.Sprintf(" %s · %s · %dx%d (%d,%d)", sessTitle, p.status, p.cols, p.rows, curX, curY)
		infoLine1 = ansi.Truncate(infoLine1, innerW, "…")
		if pad := innerW - ansi.StringWidth(infoLine1); pad > 0 {
			infoLine1 += strings.Repeat(" ", pad)
		}
		boxRows = append(boxRows, ansiCyan+"│"+ansiReset+ansiDim+infoLine1+ansiReset+ansiCyan+"│"+ansiReset)

		cwd := p.cwd
		if cwd == "" && p.seams.Cwd != nil {
			cwd = p.seams.Cwd()
		}
		if cwd != "" {
			cwdLine := " cwd: " + cwd
			cwdLine = ansi.Truncate(cwdLine, innerW, "…")
			if pad := innerW - ansi.StringWidth(cwdLine); pad > 0 {
				cwdLine += strings.Repeat(" ", pad)
			}
			boxRows = append(boxRows, ansiCyan+"│"+ansiReset+ansiDim+cwdLine+ansiReset+ansiCyan+"│"+ansiReset)
		}
		boxRows = append(boxRows, ansiCyan+"├"+strings.Repeat("─", innerW)+"┤"+ansiReset)
	}

	itemRowIndices := make([]int, len(items))
	startItem := 0
	endItem := len(items)
	if p.menuRenaming {
		prompt := " Rename: " + p.menuRenameBuf + "█"
		prompt = ansi.Truncate(prompt, innerW, "…")
		pad := innerW - ansi.StringWidth(prompt)
		if pad < 0 {
			pad = 0
		}
		boxRows = append(boxRows, ansiCyan+"│"+ansiReset+prompt+strings.Repeat(" ", pad)+ansiCyan+"│"+ansiReset)
		hint := ansiDim + " Enter: Save · Esc: Cancel" + ansiReset
		hint = ansi.Truncate(hint, innerW, "")
		hPad := innerW - ansi.StringWidth(hint)
		if hPad < 0 {
			hPad = 0
		}
		boxRows = append(boxRows, ansiCyan+"│"+ansiReset+hint+strings.Repeat(" ", hPad)+ansiCyan+"│"+ansiReset)
	} else {
		maxItems := len(lines) - len(boxRows) - 1
		if maxItems < 1 {
			maxItems = 1
		}
		if len(items) > maxItems {
			if p.menuCursor >= maxItems {
				startItem = p.menuCursor - maxItems + 1
			}
			if startItem+maxItems > len(items) {
				startItem = len(items) - maxItems
			}
			if startItem < 0 {
				startItem = 0
			}
			endItem = startItem + maxItems
			if endItem > len(items) {
				endItem = len(items)
			}
		}
		for i := startItem; i < endItem; i++ {
			item := items[i]
			itemRowIndices[i] = len(boxRows)
			prefix := "  "
			if item.kind == menuItemSwitchTab {
				if item.active {
					prefix = "● "
				} else if item.exited {
					prefix = "○ "
				}
			}
			left := prefix + item.label
			right := item.shortcut
			availLeft := innerW - ansi.StringWidth(right) - 1
			if availLeft < 4 {
				availLeft = innerW
				right = ""
			}
			left = ansi.Truncate(left, availLeft, "…")
			gap := innerW - ansi.StringWidth(left) - ansi.StringWidth(right)
			if gap < 0 {
				gap = 0
			}
			content := left + strings.Repeat(" ", gap) + ansiDim + right + ansiReset
			if i == p.menuCursor {
				plain := left + strings.Repeat(" ", gap) + right
				plain = ansi.Truncate(plain, innerW, "")
				if p2 := innerW - ansi.StringWidth(plain); p2 > 0 {
					plain += strings.Repeat(" ", p2)
				}
				content = "\x1b[7m" + plain + ansiReset
			}
			boxRows = append(boxRows, ansiCyan+"│"+ansiReset+content+ansiCyan+"│"+ansiReset)
		}
	}
	boxRows = append(boxRows, ansiCyan+"└"+strings.Repeat("─", innerW)+"┘"+ansiReset)

	boxH := len(boxRows)
	if boxH > len(lines) {
		boxH = len(lines)
		boxRows = boxRows[:boxH]
	}
	boxTop := 0
	boxLeft := width - boxW
	if boxLeft < 0 {
		boxLeft = 0
	}
	leftOffset := 2
	if p.seams.OmniBar {
		leftOffset = 0
	}

	p.menuBox = menuBoxRect{
		valid:  true,
		top:    bodyStartRow + boxTop,
		bottom: bodyStartRow + boxTop + boxH,
		left:   leftOffset + boxLeft,
		right:  leftOffset + boxLeft + boxW,
	}

	if !p.menuRenaming {
		for i := startItem; i < endItem; i++ {
			r := itemRowIndices[i]
			if r > 0 && r < boxH-1 {
				p.hits = append(p.hits, hitRect{
					row:    bodyStartRow + boxTop + r,
					startX: leftOffset + boxLeft + 1,
					endX:   leftOffset + boxLeft + boxW - 1,
					kind:   hitActionMenuItem,
					index:  i,
				})
			}
		}
	}

	out := make([]string, len(lines))
	copy(out, lines)
	for r := 0; r < boxH; r++ {
		line := out[boxTop+r]
		lw := ansi.StringWidth(line)
		if lw < width {
			line += strings.Repeat(" ", width-lw)
		}
		leftPart := ansi.Cut(line, 0, boxLeft)
		rightPart := ansi.Cut(line, boxLeft+boxW, width)
		out[boxTop+r] = leftPart + ansiReset + boxRows[r] + ansiReset + rightPart
	}
	return out
}
