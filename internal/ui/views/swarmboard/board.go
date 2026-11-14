package swarmboard

import (
	"fmt"
	"strings"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/palette"
)

// Pane identifiers
const (
	PaneRoster  = 0
	PanePosts   = 1
	PaneMessage = 2
)

// Board is the 3-pane Swarm Board overlay view.
type Board struct {
	active bool
	title  string

	roster []Member
	posts  []Post
	isDone bool

	focusPane    int  // PaneRoster, PanePosts, PaneMessage
	zoomed       bool // zoomed to full screen
	showMessage  bool // whether message pane is shown
	follow       bool // whether following new posts
	filterMember string

	rosterCursor  int
	postsCursor   int
	messageScroll int

	width  int
	height int

	onOpenSubagent func(convID string)
}

// NewBoard creates a new Swarm Board overlay view.
func NewBoard(onOpenSubagent func(convID string)) *Board {
	return &Board{
		showMessage:    true,
		follow:         true,
		onOpenSubagent: onOpenSubagent,
	}
}

// Active reports whether the overlay is visible.
func (b *Board) Active() bool {
	return b != nil && b.active
}

// Open opens the swarm board overlay.
func (b *Board) Open() {
	if b == nil {
		return
	}
	b.active = true
	b.normalizeCursors()
}

// Close closes the swarm board overlay.
func (b *Board) Close() {
	if b == nil {
		return
	}
	b.active = false
}

// Toggle toggles the overlay visibility.
func (b *Board) Toggle() {
	if b == nil {
		return
	}
	if b.active {
		b.Close()
	} else {
		b.Open()
	}
}

// SetSize updates the viewport dimensions.
func (b *Board) SetSize(width, height int) {
	if b == nil {
		return
	}
	b.width = width
	b.height = height
}

// ApplySnapshot updates the swarm state from a snapshot.
func (b *Board) ApplySnapshot(snap *SwarmSnapshot) {
	if b == nil || snap == nil {
		return
	}
	b.title = snap.Title
	b.roster = snap.Members
	b.isDone = snap.IsDone

	prevPostCount := len(b.posts)
	b.posts = snap.Posts

	if b.follow && len(b.posts) > prevPostCount {
		b.postsCursor = len(b.posts) - 1
	}
	b.normalizeCursors()
}

// Title returns the swarm title.
func (b *Board) Title() string {
	if b == nil {
		return ""
	}
	return b.title
}

// IsDone returns whether the swarm is done.
func (b *Board) IsDone() bool {
	return b != nil && b.isDone
}

// PostCount returns the total number of posts.
func (b *Board) PostCount() int {
	if b == nil {
		return 0
	}
	return len(b.posts)
}

// LatestPost returns the latest post if any.
func (b *Board) LatestPost() (Post, bool) {
	if b == nil || len(b.posts) == 0 {
		return Post{}, false
	}
	return b.posts[len(b.posts)-1], true
}

// FocusPane returns the currently focused pane index.
func (b *Board) FocusPane() int {
	if b == nil {
		return PaneRoster
	}
	return b.focusPane
}

func (b *Board) normalizeCursors() {
	if b.rosterCursor < 0 {
		b.rosterCursor = 0
	}
	if len(b.roster) > 0 && b.rosterCursor >= len(b.roster) {
		b.rosterCursor = len(b.roster) - 1
	}

	filtered := b.filteredPosts()
	if b.postsCursor < 0 {
		b.postsCursor = 0
	}
	if len(filtered) > 0 && b.postsCursor >= len(filtered) {
		b.postsCursor = len(filtered) - 1
	}
}

func (b *Board) filteredPosts() []Post {
	if b == nil || b.filterMember == "" {
		return b.posts
	}
	target := strings.ToLower(b.filterMember)
	var out []Post
	for _, p := range b.posts {
		if strings.ToLower(p.Author) == target {
			out = append(out, p)
			continue
		}
		for _, m := range p.Mentions {
			if strings.ToLower(m) == target {
				out = append(out, p)
				break
			}
		}
	}
	return out
}

// HandleKey processes a key event when the board overlay is active.
func (b *Board) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if b == nil || !b.active {
		return false, nil
	}

	// 1. Esc or 'q' - back out or dismiss
	if key.Code == tea.KeyEsc || (key.Code == 'q' && key.Mod == 0) {
		if b.zoomed {
			b.zoomed = false
			return true, nil
		}
		if b.filterMember != "" {
			b.filterMember = ""
			b.normalizeCursors()
			return true, nil
		}
		b.Close()
		return true, nil
	}

	// 2. Tab / Shift+Tab - cycle panes
	if key.Code == tea.KeyTab {
		if key.Mod.Contains(tea.ModShift) {
			b.cycleFocus(-1)
		} else {
			b.cycleFocus(1)
		}
		return true, nil
	}

	// 3. 'z' - zoom focused pane
	if (key.Code == 'z' || key.Text == "z") && key.Mod == 0 {
		b.zoomed = !b.zoomed
		return true, nil
	}

	// 4. 'f' - toggle follow
	if (key.Code == 'f' || key.Text == "f") && key.Mod == 0 {
		b.follow = !b.follow
		if b.follow && len(b.posts) > 0 {
			b.postsCursor = len(b.filteredPosts()) - 1
		}
		return true, nil
	}

	// 5. '[' and ']' - step through posts
	if (key.Code == '[' || key.Text == "[") && key.Mod == 0 {
		b.stepPost(-1)
		return true, nil
	}
	if (key.Code == ']' || key.Text == "]") && key.Mod == 0 {
		b.stepPost(1)
		return true, nil
	}

	// 6. 'p' - filter feed to selected member
	if (key.Code == 'p' || key.Text == "p") && key.Mod == 0 {
		b.toggleFilter()
		return true, nil
	}

	// 7. 'o' - open member's subagent details
	if (key.Code == 'o' || key.Text == "o") && key.Mod == 0 {
		b.openSubagentDetails()
		return true, nil
	}

	// 8. Enter - toggle message pane
	if key.Code == tea.KeyEnter && key.Mod == 0 {
		b.showMessage = !b.showMessage
		if !b.showMessage && b.focusPane == PaneMessage {
			b.focusPane = PanePosts
		}
		return true, nil
	}

	// 9. Navigation within focused pane
	switch key.Code {
	case tea.KeyUp:
		b.navigate(-1)
		return true, nil
	case tea.KeyDown:
		b.navigate(1)
		return true, nil
	case tea.KeyPgUp:
		b.navigate(-5)
		return true, nil
	case tea.KeyPgDown:
		b.navigate(5)
		return true, nil
	case tea.KeyHome:
		b.navigateStart()
		return true, nil
	case tea.KeyEnd:
		b.navigateEnd()
		return true, nil
	}

	// In vim mode or letter navigation
	if key.Mod == 0 {
		if key.Code == 'k' || key.Text == "k" {
			b.navigate(-1)
			return true, nil
		}
		if key.Code == 'j' || key.Text == "j" {
			b.navigate(1)
			return true, nil
		}
	}

	return true, nil
}

func (b *Board) cycleFocus(delta int) {
	maxPane := PanePosts
	if b.showMessage {
		maxPane = PaneMessage
	}
	b.focusPane += delta
	if b.focusPane < 0 {
		b.focusPane = maxPane
	}
	if b.focusPane > maxPane {
		b.focusPane = 0
	}
}

func (b *Board) stepPost(delta int) {
	filtered := b.filteredPosts()
	if len(filtered) == 0 {
		return
	}
	b.postsCursor += delta
	if b.postsCursor < 0 {
		b.postsCursor = 0
	}
	if b.postsCursor >= len(filtered) {
		b.postsCursor = len(filtered) - 1
	}
	b.messageScroll = 0
}

func (b *Board) toggleFilter() {
	var target string
	if b.focusPane == PaneRoster {
		if b.rosterCursor >= 0 && b.rosterCursor < len(b.roster) {
			target = b.roster[b.rosterCursor].Name
		}
	} else {
		filtered := b.filteredPosts()
		if b.postsCursor >= 0 && b.postsCursor < len(filtered) {
			target = filtered[b.postsCursor].Author
		}
	}

	if target == "" {
		return
	}
	if strings.EqualFold(b.filterMember, target) {
		b.filterMember = "" // clear filter
	} else {
		b.filterMember = target
	}
	b.postsCursor = 0
	b.messageScroll = 0
	b.normalizeCursors()
}

func (b *Board) openSubagentDetails() {
	var convID string
	if b.focusPane == PaneRoster {
		if b.rosterCursor >= 0 && b.rosterCursor < len(b.roster) {
			convID = b.roster[b.rosterCursor].ConversationID
		}
	} else {
		filtered := b.filteredPosts()
		if b.postsCursor >= 0 && b.postsCursor < len(filtered) {
			author := filtered[b.postsCursor].Author
			for _, m := range b.roster {
				if strings.EqualFold(m.Name, author) {
					convID = m.ConversationID
					break
				}
			}
		}
	}

	if convID != "" && b.onOpenSubagent != nil {
		b.onOpenSubagent(convID)
	}
}

func (b *Board) navigate(delta int) {
	switch b.focusPane {
	case PaneRoster:
		if len(b.roster) == 0 {
			return
		}
		b.rosterCursor += delta
		if b.rosterCursor < 0 {
			b.rosterCursor = 0
		}
		if b.rosterCursor >= len(b.roster) {
			b.rosterCursor = len(b.roster) - 1
		}
	case PanePosts:
		filtered := b.filteredPosts()
		if len(filtered) == 0 {
			return
		}
		b.postsCursor += delta
		if b.postsCursor < 0 {
			b.postsCursor = 0
		}
		if b.postsCursor >= len(filtered) {
			b.postsCursor = len(filtered) - 1
		}
		b.messageScroll = 0
	case PaneMessage:
		b.messageScroll += delta
		if b.messageScroll < 0 {
			b.messageScroll = 0
		}
	}
}

func (b *Board) navigateStart() {
	switch b.focusPane {
	case PaneRoster:
		b.rosterCursor = 0
	case PanePosts:
		b.postsCursor = 0
		b.messageScroll = 0
	case PaneMessage:
		b.messageScroll = 0
	}
}

func (b *Board) navigateEnd() {
	switch b.focusPane {
	case PaneRoster:
		if len(b.roster) > 0 {
			b.rosterCursor = len(b.roster) - 1
		}
	case PanePosts:
		filtered := b.filteredPosts()
		if len(filtered) > 0 {
			b.postsCursor = len(filtered) - 1
			b.messageScroll = 0
		}
	case PaneMessage:
		b.messageScroll += 20
	}
}

// View renders the Swarm Board full-screen view.
func (b *Board) View() string {
	if b == nil || !b.active {
		return ""
	}

	w := b.width
	if w <= 0 {
		w = 80
	}
	h := b.height
	if h <= 0 {
		h = 24
	}

	var sb strings.Builder

	// Title Bar (AGY 1.5.6: Queen bee in header)
	titleText := "👑🐝 Swarm Board"
	if b.title != "" {
		titleText = fmt.Sprintf("👑🐝 Swarm: %s", b.title)
	}
	statusBadge := palette.AgentDoneOpen + "● RUNNING" + palette.BlueClose
	if b.isDone {
		statusBadge = palette.AgentDoneOpen + "✓ DONE" + palette.BlueClose
	}
	var zoomBadge string
	if b.zoomed {
		zoomBadge = " " + palette.AgentActiveOpen + "[ZOOMED]" + palette.BlueClose
	}
	headerLine := fmt.Sprintf("%s%s %s%s%s", palette.BoldOpen, titleText, statusBadge, zoomBadge, palette.BoldClose)
	sb.WriteString(headerLine + "\n\n")

	contentHeight := h - 4
	if contentHeight < 6 {
		contentHeight = 6
	}

	if b.zoomed {
		// Render only the focused pane
		switch b.focusPane {
		case PaneRoster:
			sb.WriteString(b.renderRosterPane(w, contentHeight))
		case PanePosts:
			sb.WriteString(b.renderPostsPane(w, contentHeight))
		case PaneMessage:
			sb.WriteString(b.renderMessagePane(w, contentHeight))
		}
	} else {
		// 3-pane layout: Roster on left (30%), Posts / Message on right (70%)
		rosterWidth := w * 3 / 10
		if rosterWidth < 22 {
			rosterWidth = 22
		}
		if rosterWidth > 32 {
			rosterWidth = 32
		}
		rightWidth := w - rosterWidth - 3
		if rightWidth < 30 {
			rightWidth = 30
		}

		rosterLines := strings.Split(b.renderRosterPane(rosterWidth, contentHeight), "\n")

		var rightLines []string
		if b.showMessage {
			postsH := contentHeight * 5 / 10
			msgH := contentHeight - postsH - 1
			if postsH < 3 {
				postsH = 3
			}
			if msgH < 3 {
				msgH = 3
			}
			pLines := strings.Split(b.renderPostsPane(rightWidth, postsH), "\n")
			mLines := strings.Split(b.renderMessagePane(rightWidth, msgH), "\n")
			rightLines = append(rightLines, pLines...)
			rightLines = append(rightLines, strings.Repeat("─", rightWidth))
			rightLines = append(rightLines, mLines...)
		} else {
			rightLines = strings.Split(b.renderPostsPane(rightWidth, contentHeight), "\n")
		}

		maxRows := len(rosterLines)
		if len(rightLines) > maxRows {
			maxRows = len(rightLines)
		}
		if maxRows > contentHeight {
			maxRows = contentHeight
		}

		for i := 0; i < maxRows; i++ {
			rLine := ""
			if i < len(rosterLines) {
				rLine = rosterLines[i]
			}
			rtLine := ""
			if i < len(rightLines) {
				rtLine = rightLines[i]
			}

			// Pad roster line to fixed width
			rPlain := stripANSI(rLine)
			pad := rosterWidth - len(rPlain)
			if pad < 0 {
				pad = 0
			}
			sb.WriteString(rLine + strings.Repeat(" ", pad))
			sb.WriteString(palette.DimGrayOpen + " │ " + palette.DimGrayClose)
			sb.WriteString(rtLine)
			sb.WriteString("\n")
		}
	}

	// Footer Hints
	sb.WriteString("\n")
	sb.WriteString(b.renderFooterHints(w))

	return sb.String()
}

func (b *Board) renderRosterPane(width, height int) string {
	var lines []string
	header := "ROSTER"
	if b.focusPane == PaneRoster {
		header = palette.BoldOpen + palette.BlueOpen + "▶ ROSTER" + palette.BlueClose + palette.BoldClose
	}
	lines = append(lines, header)

	if len(b.roster) == 0 {
		lines = append(lines, palette.DimGrayOpen+"  (no members)"+palette.DimGrayClose)
		return strings.Join(lines, "\n")
	}

	for i, m := range b.roster {
		if len(lines) >= height {
			break
		}
		cursor := "  "
		if i == b.rosterCursor && b.focusPane == PaneRoster {
			cursor = palette.BlueOpen + "> " + palette.BlueClose
		}

		bullet := b.memberBullet(m.State)
		name := m.Name
		if m.IsCoordinator {
			name += " (coord)"
		}

		line := fmt.Sprintf("%s%s %s", cursor, bullet, name)
		lines = append(lines, line)

		if len(lines) < height && (m.Model != "" || m.State != "") {
			detail := fmt.Sprintf("    %s%s (%s)%s", palette.DimGrayOpen, m.State, m.Model, palette.DimGrayClose)
			lines = append(lines, detail)
		}
	}

	return strings.Join(lines, "\n")
}

func (b *Board) memberBullet(state string) string {
	switch strings.ToLower(state) {
	case "working":
		return palette.AgentDoneOpen + "🐝 ●" + palette.BlueClose
	case "waiting":
		return palette.AgentActiveOpen + "🐝 ○" + palette.BlueClose
	case "killed":
		return palette.AgentErrorOpen + "×" + palette.BlueClose
	case "stopped":
		return palette.DimGrayOpen + "○" + palette.DimGrayClose
	default:
		return palette.DimGrayOpen + "●" + palette.DimGrayClose
	}
}

func (b *Board) renderPostsPane(width, height int) string {
	var lines []string
	filtered := b.filteredPosts()

	header := fmt.Sprintf("POSTS (%d)", len(filtered))
	if b.filterMember != "" {
		header += fmt.Sprintf(" %s[filter: @%s]%s", palette.AgentActiveOpen, b.filterMember, palette.BlueClose)
	}
	if b.follow {
		header += fmt.Sprintf(" %s[follow]%s", palette.AgentDoneOpen, palette.BlueClose)
	}
	if b.focusPane == PanePosts {
		header = palette.BoldOpen + palette.BlueOpen + "▶ " + palette.BlueClose + header + palette.BoldClose
	}
	lines = append(lines, header)

	if len(filtered) == 0 {
		lines = append(lines, palette.DimGrayOpen+"  Waiting for posts..."+palette.DimGrayClose)
		return strings.Join(lines, "\n")
	}

	start := b.postsCursor - (height - 2)
	if start < 0 {
		start = 0
	}

	for i := start; i < len(filtered) && len(lines) < height; i++ {
		p := filtered[i]
		cursor := "  "
		if i == b.postsCursor && b.focusPane == PanePosts {
			cursor = palette.BlueOpen + "> " + palette.BlueClose
		}

		author := "@" + p.Author
		timeStr := ""
		if !p.CreatedAt.IsZero() {
			timeStr = p.CreatedAt.Format("15:04:05")
		}
		lead := fmt.Sprintf("%s#%d %s%s%s %s%s%s", cursor, p.Seq, palette.BoldOpen, author, palette.BoldClose, palette.DimGrayOpen, timeStr, palette.DimGrayClose)
		if p.Channel != "" {
			lead += fmt.Sprintf(" %s#%s%s", palette.AgentActiveOpen, p.Channel, palette.BlueClose)
		}
		lines = append(lines, lead)

		bodySnippet := b.formatSnippet(p.Body, width-6)
		lines = append(lines, "    "+bodySnippet)
	}

	return strings.Join(lines, "\n")
}

func (b *Board) renderMessagePane(width, height int) string {
	var lines []string
	filtered := b.filteredPosts()

	var selected Post
	hasSelected := false
	if b.postsCursor >= 0 && b.postsCursor < len(filtered) {
		selected = filtered[b.postsCursor]
		hasSelected = true
	}

	header := "MESSAGE"
	if hasSelected {
		header = fmt.Sprintf("MESSAGE (#%d from @%s)", selected.Seq, selected.Author)
	}
	if b.focusPane == PaneMessage {
		header = palette.BoldOpen + palette.BlueOpen + "▶ " + palette.BlueClose + header + palette.BoldClose
	}
	lines = append(lines, header)

	if !hasSelected || strings.TrimSpace(selected.Body) == "" {
		lines = append(lines, palette.DimGrayOpen+"  (empty message)"+palette.DimGrayClose)
		return strings.Join(lines, "\n")
	}

	bodyLines := strings.Split(selected.Body, "\n")
	start := b.messageScroll
	if start >= len(bodyLines) {
		start = len(bodyLines) - 1
	}
	if start < 0 {
		start = 0
	}

	for i := start; i < len(bodyLines) && len(lines) < height; i++ {
		lines = append(lines, "  "+bodyLines[i])
	}

	return strings.Join(lines, "\n")
}

func (b *Board) formatSnippet(body string, maxLen int) string {
	trimmed := strings.TrimSpace(body)
	firstLine := strings.Split(trimmed, "\n")[0]
	if maxLen > 0 && len(firstLine) > maxLen {
		firstLine = firstLine[:maxLen-1] + "…"
	}
	return firstLine
}

func (b *Board) renderFooterHints(width int) string {
	hints := []string{
		palette.BlueOpen + "Tab" + palette.BlueClose + ": switch pane",
		palette.BlueOpen + "z" + palette.BlueClose + ": zoom",
		palette.BlueOpen + "Enter" + palette.BlueClose + ": toggle message",
		palette.BlueOpen + "[ ]" + palette.BlueClose + ": prev/next post",
		palette.BlueOpen + "p" + palette.BlueClose + ": filter",
		palette.BlueOpen + "o" + palette.BlueClose + ": subagent",
		palette.BlueOpen + "f" + palette.BlueClose + ": follow",
		palette.BlueOpen + "Esc" + palette.BlueClose + ": back",
	}
	return palette.DimGrayOpen + strings.Join(hints, " · ") + palette.DimGrayClose
}

func stripANSI(s string) string {
	var sb strings.Builder
	inEsc := false
	for _, r := range s {
		if r == '\x1b' {
			inEsc = true
			continue
		}
		if inEsc {
			if r == 'm' {
				inEsc = false
			}
			continue
		}
		sb.WriteRune(r)
	}
	return sb.String()
}
