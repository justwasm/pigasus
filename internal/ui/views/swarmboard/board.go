package swarmboard

import (
	"fmt"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/graphic"
	"github.com/justwasm/pigasus/palette"
)

// Pane identifiers
const (
	PaneRoster      = 0 // classic left pane (alias for PaneCoordinator)
	PanePosts       = 1 // posts list
	PaneMessage     = 2 // message detail
	PaneWorkers     = 3 // workers partition (htop/lazygit mode)
	PaneMonitor     = 4 // monitor partition (htop/lazygit mode)
	PaneCoordinator = 0 // coordinator partition
)

// Board is the interactive Swarm Board overlay view supporting both classic 3-pane
// and lazygit/htop-like 4-block partitioned layout.
type Board struct {
	active bool
	title  string

	roster  []Member
	posts   []Post
	isDone  bool
	monitor *MonitorStatus

	focusPane    int  // PaneCoordinator (0), PanePosts (1), PaneMessage (2), PaneWorkers (3), PaneMonitor (4)
	zoomed       bool // zoomed to full screen
	showMessage  bool // whether message pane is shown
	follow       bool // whether following new posts
	filterMember string
	partitioned  bool // whether lazygit/htop partitioned mode is enabled

	rosterCursor  int
	workersCursor int
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

// Partitioned reports whether the lazygit/htop partitioned layout is active.
func (b *Board) Partitioned() bool {
	return b != nil && b.partitioned
}

// SetPartitioned toggles partitioned view mode.
func (b *Board) SetPartitioned(partitioned bool) {
	if b == nil {
		return
	}
	b.partitioned = partitioned
	if !b.partitioned && (b.focusPane == PaneWorkers || b.focusPane == PaneMonitor) {
		b.focusPane = PaneRoster
	}
	b.normalizeCursors()
}

// TogglePartitioned toggles partitioned view mode.
func (b *Board) TogglePartitioned() {
	if b == nil {
		return
	}
	b.SetPartitioned(!b.partitioned)
}

// SetMonitor updates the health/stall monitor status.
func (b *Board) SetMonitor(m *MonitorStatus) {
	if b == nil {
		return
	}
	b.monitor = m
}

// Monitor returns the current monitor status, synthesizing one if none was set.
func (b *Board) Monitor() *MonitorStatus {
	if b == nil {
		return nil
	}
	if b.monitor != nil {
		return b.monitor
	}
	// Synthesize monitor state from roster
	var stalled []string
	for _, m := range b.roster {
		if strings.EqualFold(m.State, "waiting") || strings.EqualFold(m.State, "blocked") {
			stalled = append(stalled, m.Name)
		}
	}
	state := "healthy"
	msg := "All workers healthy and responsive"
	if len(stalled) > 0 {
		state = "monitoring"
		msg = fmt.Sprintf("Monitoring %d waiting worker(s)", len(stalled))
	}
	return &MonitorStatus{
		Enabled:        true,
		State:          state,
		LastHeartbeat:  time.Now(),
		StalledWorkers: stalled,
		Message:        msg,
	}
}

// Coordinator returns the coordinator member if one is designated.
func (b *Board) Coordinator() (Member, bool) {
	if b == nil || len(b.roster) == 0 {
		return Member{}, false
	}
	for _, m := range b.roster {
		if m.IsCoordinator || strings.EqualFold(m.Name, "coordinator") {
			return m, true
		}
	}
	return b.roster[0], true
}

// Workers returns all non-coordinator members.
func (b *Board) Workers() []Member {
	if b == nil {
		return nil
	}
	var out []Member
	for _, m := range b.roster {
		if !m.IsCoordinator && !strings.EqualFold(m.Name, "coordinator") {
			out = append(out, m)
		}
	}
	return out
}

// ApplySnapshot updates the swarm state from a snapshot.
func (b *Board) ApplySnapshot(snap *SwarmSnapshot) {
	if b == nil || snap == nil {
		return
	}
	b.title = snap.Title
	b.roster = snap.Members
	b.isDone = snap.IsDone
	if snap.Monitor != nil {
		b.monitor = snap.Monitor
	}

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

// MemberCount returns the total number of members.
func (b *Board) MemberCount() int {
	if b == nil {
		return 0
	}
	return len(b.roster)
}

// Members returns a copy of the swarm members.
func (b *Board) Members() []Member {
	if b == nil {
		return nil
	}
	out := make([]Member, len(b.roster))
	copy(out, b.roster)
	return out
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

	workers := b.Workers()
	if b.workersCursor < 0 {
		b.workersCursor = 0
	}
	if len(workers) > 0 && b.workersCursor >= len(workers) {
		b.workersCursor = len(workers) - 1
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

	// 2. Direct Number Key Partition Navigation (lazygit style: '1'..'5')
	if key.Mod == 0 {
		switch key.Code {
		case '1':
			b.focusPane = PaneCoordinator
			return true, nil
		case '2':
			if b.partitioned {
				b.focusPane = PaneWorkers
			} else {
				b.focusPane = PaneRoster
			}
			return true, nil
		case '3':
			if b.partitioned {
				b.focusPane = PaneMonitor
			} else {
				b.focusPane = PanePosts
			}
			return true, nil
		case '4':
			b.focusPane = PanePosts
			return true, nil
		case '5':
			b.focusPane = PaneMessage
			return true, nil
		case 'm', 'M':
			b.TogglePartitioned()
			return true, nil
		}
	}

	// 3. Tab / Shift+Tab - cycle panes
	if key.Code == tea.KeyTab {
		if key.Mod.Contains(tea.ModShift) {
			b.cycleFocus(-1)
		} else {
			b.cycleFocus(1)
		}
		return true, nil
	}

	// 4. 'z' - zoom focused pane
	if (key.Code == 'z' || key.Text == "z") && key.Mod == 0 {
		b.zoomed = !b.zoomed
		return true, nil
	}

	// 5. 'f' - toggle follow
	if (key.Code == 'f' || key.Text == "f") && key.Mod == 0 {
		b.follow = !b.follow
		if b.follow && len(b.posts) > 0 {
			b.postsCursor = len(b.filteredPosts()) - 1
		}
		return true, nil
	}

	// 6. '[' and ']' - step through posts
	if (key.Code == '[' || key.Text == "[") && key.Mod == 0 {
		b.stepPost(-1)
		return true, nil
	}
	if (key.Code == ']' || key.Text == "]") && key.Mod == 0 {
		b.stepPost(1)
		return true, nil
	}

	// 7. 'p' - filter feed to selected member
	if (key.Code == 'p' || key.Text == "p") && key.Mod == 0 {
		b.toggleFilter()
		return true, nil
	}

	// 8. 'o' - open member's subagent details
	if (key.Code == 'o' || key.Text == "o") && key.Mod == 0 {
		b.openSubagentDetails()
		return true, nil
	}

	// 9. Enter - toggle message pane
	if key.Code == tea.KeyEnter && key.Mod == 0 {
		b.showMessage = !b.showMessage
		if !b.showMessage && b.focusPane == PaneMessage {
			b.focusPane = PanePosts
		}
		return true, nil
	}

	// 10. Navigation within focused pane
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
	if b.partitioned {
		// Lazygit partitioned order: Coordinator (0) -> Workers (3) -> Monitor (4) -> Posts (1) -> Message (2)
		cycle := []int{PaneCoordinator, PaneWorkers, PaneMonitor, PanePosts}
		if b.showMessage {
			cycle = append(cycle, PaneMessage)
		}
		currIdx := 0
		for i, p := range cycle {
			if p == b.focusPane {
				currIdx = i
				break
			}
		}
		nextIdx := (currIdx + delta) % len(cycle)
		if nextIdx < 0 {
			nextIdx += len(cycle)
		}
		b.focusPane = cycle[nextIdx]
		return
	}

	// Classic 3-pane order
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
	if b.focusPane == PaneRoster || b.focusPane == PaneCoordinator {
		if b.rosterCursor >= 0 && b.rosterCursor < len(b.roster) {
			target = b.roster[b.rosterCursor].Name
		}
	} else if b.focusPane == PaneWorkers {
		workers := b.Workers()
		if b.workersCursor >= 0 && b.workersCursor < len(workers) {
			target = workers[b.workersCursor].Name
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
	if b.focusPane == PaneRoster || b.focusPane == PaneCoordinator {
		if b.rosterCursor >= 0 && b.rosterCursor < len(b.roster) {
			convID = b.roster[b.rosterCursor].ConversationID
		}
	} else if b.focusPane == PaneWorkers {
		workers := b.Workers()
		if b.workersCursor >= 0 && b.workersCursor < len(workers) {
			convID = workers[b.workersCursor].ConversationID
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
	case PaneCoordinator:
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
	case PaneWorkers:
		workers := b.Workers()
		if len(workers) == 0 {
			return
		}
		b.workersCursor += delta
		if b.workersCursor < 0 {
			b.workersCursor = 0
		}
		if b.workersCursor >= len(workers) {
			b.workersCursor = len(workers) - 1
		}
	case PaneMonitor:
		// Monitor has no internal item cursor, scroll if needed
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
	case PaneCoordinator:
		b.rosterCursor = 0
	case PaneWorkers:
		b.workersCursor = 0
	case PanePosts:
		b.postsCursor = 0
		b.messageScroll = 0
	case PaneMessage:
		b.messageScroll = 0
	}
}

func (b *Board) navigateEnd() {
	switch b.focusPane {
	case PaneCoordinator:
		if len(b.roster) > 0 {
			b.rosterCursor = len(b.roster) - 1
		}
	case PaneWorkers:
		workers := b.Workers()
		if len(workers) > 0 {
			b.workersCursor = len(workers) - 1
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
	var modeBadge string
	if b.partitioned {
		modeBadge = " " + palette.AgentActiveOpen + "[HTOP/LAZYGIT]" + palette.BlueClose
	}
	var zoomBadge string
	if b.zoomed {
		zoomBadge = " " + palette.AgentActiveOpen + "[ZOOMED]" + palette.BlueClose
	}
	headerLine := fmt.Sprintf("%s%s %s%s%s%s", palette.BoldOpen, titleText, statusBadge, modeBadge, zoomBadge, palette.BoldClose)
	sb.WriteString(headerLine + "\n\n")

	contentHeight := h - 4
	if contentHeight < 6 {
		contentHeight = 6
	}

	if b.partitioned {
		sb.WriteString(b.renderPartitionedView(w, contentHeight))
	} else {
		sb.WriteString(b.renderClassicView(w, contentHeight))
	}

	// Footer Hints
	sb.WriteString("\n")
	sb.WriteString(b.renderFooterHints(w))

	return sb.String()
}

func (b *Board) renderClassicView(w, contentHeight int) string {
	var sb strings.Builder
	if b.height >= 28 && !b.zoomed {
		ov := b.renderOverview(w)
		sb.WriteString(ov)
		ovLines := strings.Count(ov, "\n")
		contentHeight -= ovLines
		if contentHeight < 6 {
			contentHeight = 6
		}
	}

	if b.zoomed {
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
	return sb.String()
}

// renderPartitionedView renders lazygit/htop-like 4-block layout:
// Left: [1] Coordinator, [2] Workers, [3] Monitor
// Right: [4] Posts, [5] Message Detail
func (b *Board) renderPartitionedView(w, contentHeight int) string {
	if b.zoomed {
		switch b.focusPane {
		case PaneCoordinator:
			return b.renderCoordinatorBlock(w, contentHeight)
		case PaneWorkers:
			return b.renderWorkersBlock(w, contentHeight)
		case PaneMonitor:
			return b.renderMonitorBlock(w, contentHeight)
		case PanePosts:
			return b.renderPostsPane(w, contentHeight)
		case PaneMessage:
			return b.renderMessagePane(w, contentHeight)
		default:
			return b.renderCoordinatorBlock(w, contentHeight)
		}
	}

	leftWidth := w * 42 / 100
	if leftWidth < 30 {
		leftWidth = 30
	}
	if leftWidth > 45 {
		leftWidth = 45
	}
	rightWidth := w - leftWidth - 3
	if rightWidth < 35 {
		rightWidth = 35
	}

	// Height split for Left:
	// Coordinator: fixed 6 lines
	// Workers: remaining * 60%
	// Monitor: remaining * 40%
	coordH := 6
	remLeft := contentHeight - coordH - 2
	if remLeft < 6 {
		remLeft = 6
	}
	workersH := remLeft * 60 / 100
	if workersH < 4 {
		workersH = 4
	}
	monitorH := remLeft - workersH
	if monitorH < 3 {
		monitorH = 3
	}

	coordLines := strings.Split(b.renderCoordinatorBlock(leftWidth, coordH), "\n")
	workerLines := strings.Split(b.renderWorkersBlock(leftWidth, workersH), "\n")
	monitorLines := strings.Split(b.renderMonitorBlock(leftWidth, monitorH), "\n")

	var leftLines []string
	leftLines = append(leftLines, coordLines...)
	leftLines = append(leftLines, workerLines...)
	leftLines = append(leftLines, monitorLines...)

	// Height split for Right:
	postsH := contentHeight * 55 / 100
	msgH := contentHeight - postsH - 1
	if postsH < 4 {
		postsH = 4
	}
	if msgH < 3 {
		msgH = 3
	}
	pLines := strings.Split(b.renderPostsPane(rightWidth, postsH), "\n")
	mLines := strings.Split(b.renderMessagePane(rightWidth, msgH), "\n")

	var rightLines []string
	rightLines = append(rightLines, pLines...)
	rightLines = append(rightLines, palette.DimGrayOpen+strings.Repeat("─", rightWidth)+palette.DimGrayClose)
	rightLines = append(rightLines, mLines...)

	maxRows := len(leftLines)
	if len(rightLines) > maxRows {
		maxRows = len(rightLines)
	}
	if maxRows > contentHeight {
		maxRows = contentHeight
	}

	var sb strings.Builder
	for i := 0; i < maxRows; i++ {
		lLine := ""
		if i < len(leftLines) {
			lLine = leftLines[i]
		}
		rLine := ""
		if i < len(rightLines) {
			rLine = rightLines[i]
		}

		lPlain := stripANSI(lLine)
		pad := leftWidth - len(lPlain)
		if pad < 0 {
			pad = 0
		}
		sb.WriteString(lLine + strings.Repeat(" ", pad))
		sb.WriteString(palette.DimGrayOpen + " │ " + palette.DimGrayClose)
		sb.WriteString(rLine)
		sb.WriteString("\n")
	}

	return sb.String()
}

func (b *Board) renderCoordinatorBlock(width, height int) string {
	var lines []string
	header := "[1] COORDINATOR"
	if b.focusPane == PaneCoordinator {
		header = palette.BoldOpen + palette.BlueOpen + "▶ [1] COORDINATOR" + palette.BlueClose + palette.BoldClose
	} else {
		header = palette.DimGrayOpen + header + palette.DimGrayClose
	}
	lines = append(lines, header)

	coord, hasCoord := b.Coordinator()
	if !hasCoord {
		lines = append(lines, "  (no coordinator)")
		return strings.Join(lines, "\n")
	}

	nameStr := fmt.Sprintf("  Leader: %s%s%s", palette.BoldOpen, coord.Name, palette.BoldClose)
	if coord.Model != "" {
		nameStr += fmt.Sprintf(" (%s)", coord.Model)
	}
	lines = append(lines, nameStr)

	// Progress Meter (Htop style)
	done, total, pct := b.OverallProgress()
	meter := renderProgressBar(pct, width-4)
	lines = append(lines, fmt.Sprintf("  Tasks:  %s (%d/%d done)", meter, done, total))

	if b.title != "" && len(lines) < height {
		goalSnippet := b.formatSnippet(b.title, width-10)
		lines = append(lines, fmt.Sprintf("  Goal:   %s%s%s", palette.DimGrayOpen, goalSnippet, palette.DimGrayClose))
	}

	lines = append(lines, palette.DimGrayOpen+strings.Repeat("─", width)+palette.DimGrayClose)
	return strings.Join(lines, "\n")
}

func (b *Board) renderWorkersBlock(width, height int) string {
	var lines []string
	workers := b.Workers()
	header := fmt.Sprintf("[2] WORKERS (%d)", len(workers))
	if b.focusPane == PaneWorkers {
		header = palette.BoldOpen + palette.BlueOpen + "▶ " + header + palette.BlueClose + palette.BoldClose
	} else {
		header = palette.DimGrayOpen + header + palette.DimGrayClose
	}
	lines = append(lines, header)

	if len(workers) == 0 {
		lines = append(lines, palette.DimGrayOpen+"  (no workers registered)"+palette.DimGrayClose)
		lines = append(lines, palette.DimGrayOpen+strings.Repeat("─", width)+palette.DimGrayClose)
		return strings.Join(lines, "\n")
	}

	for i, w := range workers {
		if len(lines) >= height-1 {
			break
		}
		cursor := "  "
		if i == b.workersCursor && b.focusPane == PaneWorkers {
			cursor = palette.BlueOpen + "> " + palette.BlueClose
		}
		bullet := b.memberBullet(w.State)
		modelStr := ""
		if w.Model != "" {
			modelStr = fmt.Sprintf(" (%s)", w.Model)
		}
		line := fmt.Sprintf("%s%s %s%s", cursor, bullet, w.Name, modelStr)
		lines = append(lines, line)

		if len(lines) < height-1 {
			// Sub-line with progress and state
			prog := w.Progress
			if prog == 0 && (strings.EqualFold(w.State, "done") || strings.EqualFold(w.State, "completed")) {
				prog = 100
			}
			meter := renderProgressBar(prog, 18)
			detail := fmt.Sprintf("    %s %s%s%s", meter, palette.DimGrayOpen, w.State, palette.DimGrayClose)
			if w.ActiveTurns > 0 {
				detail += fmt.Sprintf(" · %dT", w.ActiveTurns)
			}
			lines = append(lines, detail)
		}
	}

	lines = append(lines, palette.DimGrayOpen+strings.Repeat("─", width)+palette.DimGrayClose)
	return strings.Join(lines, "\n")
}

func (b *Board) renderMonitorBlock(width, height int) string {
	var lines []string
	header := "[3] MONITOR"
	if b.focusPane == PaneMonitor {
		header = palette.BoldOpen + palette.BlueOpen + "▶ [3] MONITOR" + palette.BlueClose + palette.BoldClose
	} else {
		header = palette.DimGrayOpen + header + palette.DimGrayClose
	}
	lines = append(lines, header)

	mon := b.Monitor()
	var stateBadge string
	switch strings.ToLower(mon.State) {
	case "healthy":
		stateBadge = palette.AgentDoneOpen + "● HEALTHY" + palette.BlueClose
	case "stalled":
		stateBadge = palette.AgentErrorOpen + "▲ STALLED" + palette.BlueClose
	case "monitoring":
		stateBadge = palette.AgentActiveOpen + "○ MONITORING" + palette.BlueClose
	default:
		stateBadge = palette.DimGrayOpen + "○ " + mon.State + palette.DimGrayClose
	}

	lines = append(lines, fmt.Sprintf("  Pulse: %s", stateBadge))
	if !mon.LastHeartbeat.IsZero() {
		lines = append(lines, fmt.Sprintf("  Beat:  %s%s%s", palette.DimGrayOpen, mon.LastHeartbeat.Format("15:04:05"), palette.DimGrayClose))
	}
	if mon.Message != "" && len(lines) < height {
		msg := b.formatSnippet(mon.Message, width-8)
		lines = append(lines, fmt.Sprintf("  Msg:   %s%s%s", palette.DimGrayOpen, msg, palette.DimGrayClose))
	}

	return strings.Join(lines, "\n")
}

// OverallProgress computes task completion statistics across workers.
func (b *Board) OverallProgress() (done int, total int, pct int) {
	workers := b.Workers()
	total = len(workers)
	if total == 0 {
		return 0, 0, 0
	}
	var totalProg int
	for _, w := range workers {
		if strings.EqualFold(w.State, "done") || strings.EqualFold(w.State, "completed") || w.Progress >= 100 {
			done++
			totalProg += 100
		} else {
			totalProg += w.Progress
		}
	}
	pct = totalProg / total
	return done, total, pct
}

// renderProgressBar formats an htop-style ASCII progress bar.
func renderProgressBar(pct int, width int) string {
	if pct < 0 {
		pct = 0
	}
	if pct > 100 {
		pct = 100
	}
	barWidth := width - 7
	if barWidth < 4 {
		barWidth = 4
	}
	filled := (pct * barWidth) / 100
	empty := barWidth - filled
	if empty < 0 {
		empty = 0
	}
	bar := strings.Repeat("|", filled) + strings.Repeat(".", empty)
	return fmt.Sprintf("[%s] %3d%%", bar, pct)
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
	if b.partitioned {
		header = fmt.Sprintf("[4] POSTS (%d)", len(filtered))
	}
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
	if b.partitioned {
		header = "[5] MESSAGE DETAIL"
	}
	if hasSelected {
		header = fmt.Sprintf("%s (#%d from @%s)", header, selected.Seq, selected.Author)
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
		palette.BlueOpen + "Tab" + palette.BlueClose + ": next",
		palette.BlueOpen + "1-5" + palette.BlueClose + ": jump",
		palette.BlueOpen + "m" + palette.BlueClose + ": htop layout",
		palette.BlueOpen + "z" + palette.BlueClose + ": zoom",
		palette.BlueOpen + "Enter" + palette.BlueClose + ": msg",
		palette.BlueOpen + "[ ]" + palette.BlueClose + ": post",
		palette.BlueOpen + "p" + palette.BlueClose + ": filter",
		palette.BlueOpen + "o" + palette.BlueClose + ": subagent",
		palette.BlueOpen + "f" + palette.BlueClose + ": follow",
		palette.BlueOpen + "Esc" + palette.BlueClose + ": back",
	}
	return palette.DimGrayOpen + strings.Join(hints, " · ") + palette.DimGrayClose
}

func (b *Board) renderOverview(w int) string {
	var total, done, blocked int
	for _, m := range b.roster {
		if m.IsCoordinator {
			continue
		}
		total++
		switch strings.ToLower(m.State) {
		case "done", "completed":
			done++
		case "waiting", "blocked", "waiting on a peer":
			blocked++
		}
	}
	if total == 0 && len(b.roster) > 0 {
		total = len(b.roster)
	}

	header := fmt.Sprintf("Overview · %d of %d tasks done", done, total)
	if blocked > 0 {
		header += fmt.Sprintf(" · %d blocked", blocked)
	}

	var sb strings.Builder
	sb.WriteString(palette.BoldOpen + header + palette.BoldClose + "\n")
	if b.title != "" {
		sb.WriteString("Goal: " + b.title + "\n")
	}

	// Up to 6 workstream threads
	count := 0
	var threadItems []string
	for _, m := range b.roster {
		if m.IsCoordinator && len(b.roster) > 1 {
			continue
		}
		if count >= 6 {
			break
		}
		count++
		status := m.State
		var glyph string
		switch strings.ToLower(status) {
		case "working":
			glyph = palette.AgentActiveOpen + "● working" + palette.BlueClose
		case "review", "in review":
			glyph = palette.BlueOpen + "◎ in review" + palette.BlueClose
		case "waiting", "waiting on a peer":
			glyph = palette.WarningOpen + "◷ waiting on a peer" + palette.BlueClose
		case "failed", "killed", "error":
			glyph = palette.AgentErrorOpen + "✗ failed" + palette.BlueClose
		case "done", "completed":
			glyph = palette.AgentDoneOpen + "✓ done" + palette.BlueClose
		default:
			glyph = palette.DimGrayOpen + "○ " + status + palette.DimGrayClose
		}
		threadItems = append(threadItems, fmt.Sprintf("%s: %s", m.Name, glyph))
	}
	if len(threadItems) > 0 {
		sb.WriteString("Threads: " + strings.Join(threadItems, "  ") + "\n")
	}

	if w >= 60 {
		caps := graphic.DetectEnv()
		if caps.Protocol == graphic.ProtocolNone {
			beeLines := strings.Split(graphic.RenderQueenBee(12, caps), "\n")
			textLines := strings.Split(strings.TrimRight(sb.String(), "\n"), "\n")
			rows := len(beeLines)
			if len(textLines) > rows {
				rows = len(textLines)
			}
			var combined strings.Builder
			for i := 0; i < rows; i++ {
				left := strings.Repeat(" ", 12)
				if i < len(beeLines) {
					left = beeLines[i]
				}
				right := ""
				if i < len(textLines) {
					right = textLines[i]
				}
				combined.WriteString(left)
				if right != "" {
					combined.WriteString("  ")
					combined.WriteString(right)
				}
				combined.WriteByte('\n')
			}
			combined.WriteByte('\n')
			return combined.String()
		}
	}

	sb.WriteString("\n")
	return sb.String()
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
