package projects

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	coreprojects "github.com/justwasm/pigasus/internal/core/cliflags/projects"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/palette"
)

const (
	ansiBold  = "\x1b[1m"
	ansiReset = "\x1b[0m"
)

// Phase defines whether the projects panel is in the list view or the editor wizard.
type Phase int

const (
	PhaseList Phase = iota
	PhaseEditor
)

// Mode is an alias for Phase for backward compatibility.
type Mode = Phase

const (
	ModeList   = PhaseList
	ModeEditor = PhaseEditor

	// Backward-compatible mode constants
	ModeCreateName    = PhaseEditor
	ModeCreateFolders = PhaseEditor
	ModeRename        = PhaseEditor
	ModeEditFolders   = PhaseEditor
)

// FolderItem represents an associated folder checkbox entry in the editor.
type FolderItem struct {
	Path    string
	Checked bool
}

// Seams are the host interactions needed by the projects panel.
type Seams struct {
	Close         func() tea.Cmd
	SwitchProject func(proj coreprojects.LocalProject) tea.Cmd
}

// Panel handles the /projects modal UI.
type Panel struct {
	Home      string
	Workspace string
	Projects  []coreprojects.LocalProject
	CurrentID string

	// List phase state
	Cursor          int
	SearchQuery     string
	ExpandedID      string
	ActionCursor    int
	SwitchConfirmID string
	StatusMessage   string

	// Editor phase state
	Mode           Mode
	IsNew          bool
	OriginalProj   coreprojects.LocalProject
	Tab            int // 0 = Name, 1 = Folders
	NameInput      string
	NameError      string
	Folders        []FolderItem
	FolderCursor   int
	AddingFolder   bool
	AddFolderInput string
	AddFolderError string
	ExitConfirm    bool

	seams Seams
}

// New constructs a new projects panel with seams.
func New(home, workspace string, seams Seams) *Panel {
	p := &Panel{
		Home:      home,
		Workspace: workspace,
		Mode:      ModeList,
		seams:     seams,
	}
	p.Reload()
	return p
}

// NewPanel constructs a new projects panel without seams (for backward compatibility).
func NewPanel(home, workspace string) *Panel {
	return New(home, workspace, Seams{})
}

// Reload refreshes projects from ~/.gemini/config/projects/.
func (p *Panel) Reload() {
	list, err := coreprojects.ListLocalProjects(p.Home)
	if err == nil {
		p.Projects = list
	}
	sort.Slice(p.Projects, func(i, j int) bool {
		return strings.ToLower(p.Projects[i].Name) < strings.ToLower(p.Projects[j].Name)
	})
	p.CurrentID = coreprojects.CurrentLocalProjectID(p.Home, p.Workspace)
	p.clampListCursor()
}

func (p *Panel) filteredProjects() []coreprojects.LocalProject {
	q := strings.TrimSpace(strings.ToLower(p.SearchQuery))
	if q == "" {
		return p.Projects
	}
	var out []coreprojects.LocalProject
	for _, pr := range p.Projects {
		if strings.Contains(strings.ToLower(pr.Name), q) {
			out = append(out, pr)
			continue
		}
		for _, f := range pr.Folders {
			if strings.Contains(strings.ToLower(f), q) {
				out = append(out, pr)
				break
			}
		}
	}
	return out
}

func (p *Panel) clampListCursor() {
	filtered := p.filteredProjects()
	maxCursor := len(filtered)
	if p.Cursor > maxCursor {
		p.Cursor = maxCursor
	}
	if p.Cursor < 0 {
		p.Cursor = 0
	}
}

// Selected returns the currently selected project in list mode, or nil.
func (p *Panel) Selected() *coreprojects.LocalProject {
	filtered := p.filteredProjects()
	if p.Cursor <= 0 || p.Cursor > len(filtered) {
		return nil
	}
	return &filtered[p.Cursor-1]
}

// StartCreate transitions to the editor wizard to create a new project.
func (p *Panel) StartCreate() {
	p.Mode = ModeEditor
	p.IsNew = true
	p.OriginalProj = coreprojects.LocalProject{}
	p.Tab = 0
	defaultName := ""
	if p.Workspace != "" {
		defaultName = filepath.Base(p.Workspace)
	}
	p.NameInput = defaultName
	p.NameError = ""
	p.Folders = nil
	if p.Workspace != "" {
		p.Folders = append(p.Folders, FolderItem{Path: p.Workspace, Checked: true})
	}
	p.FolderCursor = 0
	p.AddingFolder = false
	p.AddFolderInput = ""
	p.AddFolderError = ""
	p.ExitConfirm = false
	p.StatusMessage = ""
}

// StartEdit transitions to the editor wizard to edit an existing project.
func (p *Panel) StartEdit(proj coreprojects.LocalProject) {
	p.Mode = ModeEditor
	p.IsNew = false
	p.OriginalProj = proj
	p.Tab = 0
	p.NameInput = proj.Name
	p.NameError = ""
	p.Folders = nil
	for _, f := range proj.Folders {
		p.Folders = append(p.Folders, FolderItem{Path: f, Checked: true})
	}
	p.FolderCursor = 0
	p.AddingFolder = false
	p.AddFolderInput = ""
	p.AddFolderError = ""
	p.ExitConfirm = false
	p.StatusMessage = ""
}

func (p *Panel) isDirty() bool {
	if p.IsNew {
		defaultName := ""
		if p.Workspace != "" {
			defaultName = filepath.Base(p.Workspace)
		}
		if p.NameInput != defaultName {
			return true
		}
		if p.Workspace != "" {
			if len(p.Folders) != 1 || p.Folders[0].Path != p.Workspace || !p.Folders[0].Checked {
				return true
			}
		} else {
			if len(p.Folders) > 0 {
				return true
			}
		}
		return false
	}
	if p.NameInput != p.OriginalProj.Name {
		return true
	}
	var sel []string
	for _, f := range p.Folders {
		if f.Checked {
			sel = append(sel, f.Path)
		}
	}
	if len(sel) != len(p.OriginalProj.Folders) {
		return true
	}
	for i := range sel {
		if sel[i] != p.OriginalProj.Folders[i] {
			return true
		}
	}
	return false
}

func (p *Panel) nameExists(name string) bool {
	for _, pr := range p.Projects {
		if !p.IsNew && pr.ID == p.OriginalProj.ID {
			continue
		}
		if strings.EqualFold(pr.Name, name) {
			return true
		}
	}
	return false
}

func (p *Panel) validateName() bool {
	name := strings.TrimSpace(p.NameInput)
	if name == "" {
		p.NameError = "Enter a project name."
		p.Tab = 0
		return false
	}
	if p.nameExists(name) {
		p.NameError = "A project with this name already exists."
		p.Tab = 0
		return false
	}
	p.NameError = ""
	return true
}

func (p *Panel) submit() (bool, tea.Cmd) {
	if !p.validateName() {
		return true, nil
	}
	name := strings.TrimSpace(p.NameInput)
	var checkedFolders []string
	for _, f := range p.Folders {
		if f.Checked {
			checkedFolders = append(checkedFolders, f.Path)
		}
	}

	if p.IsNew {
		projID := fmt.Sprintf("proj-%d", time.Now().UnixNano())
		proj := coreprojects.LocalProject{
			ID:      projID,
			Name:    name,
			Folders: checkedFolders,
		}
		if err := coreprojects.SaveLocalProject(p.Home, proj); err != nil {
			p.NameError = "Failed to save: " + err.Error()
			p.Tab = 0
			return true, nil
		}
		p.StatusMessage = fmt.Sprintf("(Created %s)", name)
		p.Mode = ModeList
		p.SearchQuery = ""
		p.Reload()
		for i, pr := range p.filteredProjects() {
			if pr.ID == projID {
				p.Cursor = i + 1
				break
			}
		}
		return true, nil
	}

	// Editing existing project
	proj := p.OriginalProj
	proj.Name = name
	proj.Folders = checkedFolders
	if err := coreprojects.SaveLocalProject(p.Home, proj); err != nil {
		p.NameError = "Failed to save: " + err.Error()
		p.Tab = 0
		return true, nil
	}
	p.StatusMessage = fmt.Sprintf("(Saved %s)", name)
	p.Mode = ModeList
	p.SearchQuery = ""
	p.Reload()
	for i, pr := range p.filteredProjects() {
		if pr.ID == proj.ID {
			p.Cursor = i + 1
			break
		}
	}
	return true, nil
}

func (p *Panel) close() tea.Cmd {
	if p.seams.Close != nil {
		return p.seams.Close()
	}
	return nil
}

func (p *Panel) switchProject(proj coreprojects.LocalProject) tea.Cmd {
	if p.seams.SwitchProject != nil {
		return p.seams.SwitchProject(proj)
	}
	return nil
}

// HandleKey owns keyboard events for the projects panel.
func (p *Panel) HandleKey(key tea.Key) (bool, tea.Cmd) {
	if p == nil {
		return false, nil
	}

	switch p.Mode {
	case ModeList:
		return p.handleListKey(key)
	case ModeEditor:
		return p.handleEditorKey(key)
	}
	return false, nil
}

func (p *Panel) handleListKey(key tea.Key) (bool, tea.Cmd) {
	filtered := p.filteredProjects()

	// 1. Switch confirmation prompt
	if p.SwitchConfirmID != "" {
		switch {
		case key.Code == 'y' || key.Code == 'Y':
			var target *coreprojects.LocalProject
			for _, pr := range p.Projects {
				if pr.ID == p.SwitchConfirmID {
					target = &pr
					break
				}
			}
			p.SwitchConfirmID = ""
			if target != nil {
				coreprojects.SetCurrentLocalProject(p.Home, p.Workspace, target.ID)
				p.CurrentID = target.ID
				return true, p.switchProject(*target)
			}
			return true, nil
		case key.Code == 'n' || key.Code == 'N' || key.Code == tea.KeyEsc:
			p.SwitchConfirmID = ""
			return true, nil
		}
		return true, nil
	}

	// 2. Inline actions expanded
	if p.ExpandedID != "" {
		isCurrent := p.ExpandedID == p.CurrentID
		maxAction := 1
		if isCurrent {
			maxAction = 0 // only Edit
		}

		switch {
		case key.Code == tea.KeyEsc:
			p.ExpandedID = ""
			return true, nil
		case key.Code == tea.KeyUp || (key.Code == 'k' && key.Mod == 0):
			if p.ActionCursor > 0 {
				p.ActionCursor--
			}
			return true, nil
		case key.Code == tea.KeyDown || (key.Code == 'j' && key.Mod == 0):
			if p.ActionCursor < maxAction {
				p.ActionCursor++
			}
			return true, nil
		case key.Code == tea.KeyEnter:
			var target *coreprojects.LocalProject
			for _, pr := range p.Projects {
				if pr.ID == p.ExpandedID {
					target = &pr
					break
				}
			}
			if target == nil {
				p.ExpandedID = ""
				return true, nil
			}
			if isCurrent {
				p.ExpandedID = ""
				p.StartEdit(*target)
				return true, nil
			}
			if p.ActionCursor == 0 {
				p.SwitchConfirmID = target.ID
				p.ExpandedID = ""
				return true, nil
			} else {
				p.ExpandedID = ""
				p.StartEdit(*target)
				return true, nil
			}
		}
		return true, nil
	}

	// 3. Normal list navigation & search
	switch {
	case key.Code == tea.KeyEsc:
		if p.SearchQuery != "" {
			p.SearchQuery = ""
			p.clampListCursor()
			return true, nil
		}
		return true, p.close()

	case key.Code == tea.KeyUp || (key.Code == 'k' && key.Mod == 0):
		if p.Cursor > 0 {
			p.Cursor--
		}
		return true, nil

	case key.Code == tea.KeyDown || (key.Code == 'j' && key.Mod == 0):
		if p.Cursor < len(filtered) {
			p.Cursor++
		}
		return true, nil

	case key.Code == tea.KeyEnter:
		if p.Cursor == 0 {
			p.StartCreate()
			return true, nil
		}
		if p.Cursor <= len(filtered) {
			target := filtered[p.Cursor-1]
			p.ExpandedID = target.ID
			p.ActionCursor = 0
			return true, nil
		}
		return true, nil

	case key.Code == tea.KeyBackspace:
		if p.SearchQuery != "" {
			runes := []rune(p.SearchQuery)
			p.SearchQuery = string(runes[:len(runes)-1])
			p.clampListCursor()
			return true, nil
		}
		return true, nil

	default:
		if keybind.IsTypedText(key) {
			p.SearchQuery += key.Text
			p.clampListCursor()
			return true, nil
		}
	}

	return false, nil
}

func (p *Panel) handleEditorKey(key tea.Key) (bool, tea.Cmd) {
	// 1. Exit confirm prompt
	if p.ExitConfirm {
		switch {
		case key.Code == 'y' || key.Code == 'Y':
			p.ExitConfirm = false
			p.Mode = ModeList
			p.SearchQuery = ""
			p.clampListCursor()
			return true, nil
		case key.Code == 'n' || key.Code == 'N' || key.Code == tea.KeyEsc:
			p.ExitConfirm = false
			return true, nil
		}
		return true, nil
	}

	// 2. Global editor shortcut: ctrl+s to submit
	if (key.Code == 's' || key.Code == 'S') && (key.Mod&tea.ModCtrl != 0) {
		return p.submit()
	}

	// 3. Tab 0: Name
	if p.Tab == 0 {
		switch {
		case key.Code == tea.KeyEsc:
			if p.isDirty() {
				p.ExitConfirm = true
				return true, nil
			}
			p.Mode = ModeList
			p.SearchQuery = ""
			p.clampListCursor()
			return true, nil

		case key.Code == tea.KeyRight || key.Code == tea.KeyTab || key.Code == tea.KeyEnter:
			if p.validateName() {
				p.Tab = 1
				p.FolderCursor = 0
			}
			return true, nil

		case key.Code == tea.KeyBackspace:
			if r := []rune(p.NameInput); len(r) > 0 {
				p.NameInput = string(r[:len(r)-1])
			}
			p.NameError = ""
			return true, nil

		default:
			if keybind.IsTypedText(key) {
				p.NameInput += key.Text
				p.NameError = ""
				return true, nil
			}
		}
		return true, nil
	}

	// 4. Tab 1: Folders
	if p.Tab == 1 {
		if p.AddingFolder {
			switch {
			case key.Code == tea.KeyEsc:
				p.AddingFolder = false
				p.AddFolderInput = ""
				p.AddFolderError = ""
				return true, nil

			case key.Code == tea.KeyEnter:
				raw := strings.TrimSpace(p.AddFolderInput)
				if raw == "" {
					p.AddFolderError = "Folder path cannot be empty."
					return true, nil
				}
				resolved, err := coreprojects.ResolveFolderPath(p.Workspace, raw)
				if err != nil {
					p.AddFolderError = err.Error()
					return true, nil
				}
				info, err := os.Stat(resolved)
				if err != nil {
					p.AddFolderError = err.Error()
					return true, nil
				}
				if !info.IsDir() {
					p.AddFolderError = fmt.Sprintf("%s is not a directory", resolved)
					return true, nil
				}
				for _, f := range p.Folders {
					if f.Path == resolved {
						p.AddFolderError = "Folder already added."
						return true, nil
					}
				}
				p.Folders = append(p.Folders, FolderItem{Path: resolved, Checked: true})
				p.FolderCursor = len(p.Folders) - 1
				p.AddingFolder = false
				p.AddFolderInput = ""
				p.AddFolderError = ""
				return true, nil

			case key.Code == tea.KeyBackspace:
				if r := []rune(p.AddFolderInput); len(r) > 0 {
					p.AddFolderInput = string(r[:len(r)-1])
				}
				p.AddFolderError = ""
				return true, nil

			default:
				if keybind.IsTypedText(key) {
					p.AddFolderInput += key.Text
					p.AddFolderError = ""
					return true, nil
				}
			}
			return true, nil
		}

		// Normal Folders navigation
		switch {
		case key.Code == tea.KeyEsc:
			if p.isDirty() {
				p.ExitConfirm = true
				return true, nil
			}
			p.Mode = ModeList
			p.SearchQuery = ""
			p.clampListCursor()
			return true, nil

		case key.Code == tea.KeyLeft || (key.Code == tea.KeyTab && (key.Mod&tea.ModShift != 0)):
			p.Tab = 0
			return true, nil

		case key.Code == tea.KeyUp || (key.Code == 'k' && key.Mod == 0):
			if p.FolderCursor > 0 {
				p.FolderCursor--
			}
			return true, nil

		case key.Code == tea.KeyDown || (key.Code == 'j' && key.Mod == 0):
			if p.FolderCursor < len(p.Folders) {
				p.FolderCursor++
			}
			return true, nil

		case key.Code == tea.KeySpace || key.Code == tea.KeyEnter:
			if p.FolderCursor < len(p.Folders) {
				p.Folders[p.FolderCursor].Checked = !p.Folders[p.FolderCursor].Checked
				return true, nil
			}
			// Cursor is on "+ Add a folder..."
			p.AddingFolder = true
			p.AddFolderInput = ""
			p.AddFolderError = ""
			return true, nil
		}
		return true, nil
	}

	return false, nil
}

// Lines renders the panel lines for display.
func (p *Panel) Lines(width int) []string {
	if p == nil {
		return nil
	}
	return strings.Split(p.Render(width), "\n")
}

// Render produces the view string for the projects panel.
func (p *Panel) Render(width int) string {
	if p.Mode == ModeEditor {
		return p.renderEditor(width)
	}
	return p.renderList(width)
}

func (p *Panel) renderList(width int) string {
	var lines []string

	// Header line 1
	header := ansiBold + "  Projects" + ansiReset
	if p.StatusMessage != "" {
		header += " " + palette.DimGrayOpen + p.StatusMessage + palette.DimGrayClose
	}
	lines = append(lines, header)

	// Search line 2
	if p.SearchQuery == "" {
		lines = append(lines, palette.DimGrayOpen+"  Type to search projects..."+palette.DimGrayClose)
	} else {
		lines = append(lines, palette.DimGrayOpen+"  Search: "+palette.DimGrayClose+p.SearchQuery+"\x1b[7m\x1b[37m \x1b[0m")
	}

	filtered := p.filteredProjects()

	// Row 0: + New project...
	if p.Cursor == 0 {
		lines = append(lines, palette.BlueOpen+"> "+ansiReset+ansiBold+"+ New project..."+ansiReset)
	} else {
		lines = append(lines, "  "+ansiBold+"+ New project..."+ansiReset)
	}

	// Projects rows
	if len(filtered) == 0 && p.SearchQuery != "" {
		lines = append(lines, palette.DimGrayOpen+"  No projects match your search."+palette.DimGrayClose)
	} else {
		for i, pr := range filtered {
			rowIdx := i + 1
			isCursor := rowIdx == p.Cursor
			isCurrent := pr.ID == p.CurrentID

			bullet := "\x1b[38;5;238m●\x1b[39m"
			if isCurrent {
				bullet = palette.AgentFgOpen + "●" + palette.AgentFgClose
			}

			currentBadge := ""
			if isCurrent {
				currentBadge = "  " + palette.AgentFgOpen + "current" + palette.AgentFgClose
			}

			if isCursor {
				if p.SwitchConfirmID == pr.ID {
					// In switch confirmation
					nameStr := ansiBold + palette.BlueOpen + pr.Name + ansiReset
					lines = append(lines, palette.BlueOpen+"> "+ansiReset+bullet+" "+nameStr+" \x1b[38;5;214m[Switch? Starts a new conversation (y/n)]\x1b[39m")
				} else if p.ExpandedID == pr.ID {
					// Expanded actions
					nameStr := ansiBold + pr.Name + ansiReset
					lines = append(lines, "  "+bullet+" "+nameStr+currentBadge)
					if isCurrent {
						// Only "Edit"
						lines = append(lines, "    "+palette.BlueOpen+"> "+ansiReset+ansiBold+"Edit"+ansiReset)
					} else {
						if p.ActionCursor == 0 {
							lines = append(lines, "    "+palette.BlueOpen+"> "+ansiReset+ansiBold+"Switch to project"+ansiReset)
							lines = append(lines, "      Edit")
						} else {
							lines = append(lines, "      Switch to project")
							lines = append(lines, "    "+palette.BlueOpen+"> "+ansiReset+ansiBold+"Edit"+ansiReset)
						}
					}
				} else {
					// Normal selected row
					nameStr := ansiBold + palette.BlueOpen + pr.Name + ansiReset
					lines = append(lines, palette.BlueOpen+"> "+ansiReset+bullet+" "+nameStr+currentBadge)
				}
			} else {
				// Unselected row
				nameStr := ansiBold + pr.Name + ansiReset
				lines = append(lines, "  "+bullet+" "+nameStr+currentBadge)
			}
		}
	}

	lines = append(lines, "")

	// Keyboard footer
	var kbd []string
	if p.SwitchConfirmID != "" {
		kbd = append(kbd, p.kbdHint("y", "Switch"), p.kbdHint("n", "Cancel"))
	} else if p.ExpandedID != "" {
		kbd = append(kbd, p.kbdHint("↑/↓", "Navigate"), p.kbdHint("enter", "Select"), p.kbdHint("esc", "Cancel"))
	} else if p.Cursor == 0 {
		escDesc := "Close"
		if p.SearchQuery != "" {
			escDesc = "Clear Search"
		}
		kbd = append(kbd, p.kbdHint("↑/↓", "Navigate"), p.kbdHint("enter", "New Project"), p.kbdHint("esc", escDesc))
	} else {
		escDesc := "Close"
		if p.SearchQuery != "" {
			escDesc = "Clear Search"
		}
		kbd = append(kbd, p.kbdHint("↑/↓", "Navigate"), p.kbdHint("enter", "Actions"), p.kbdHint("esc", escDesc))
	}
	lines = append(lines, ansiBold+"Keyboard: "+ansiReset+strings.Join(kbd, "  "))

	return strings.Join(lines, "\n")
}

func (p *Panel) renderEditor(width int) string {
	var lines []string

	subTitle := "New Project"
	if !p.IsNew {
		subTitle = p.OriginalProj.Name
	}
	header := "  " + ansiBold + "Projects" + ansiReset + "\x1b[38;5;238m › \x1b[1m" + subTitle + ansiReset
	if p.ExitConfirm {
		if p.IsNew {
			header += "\x1b[38;5;214m [Cancel project creation? (y/n)]\x1b[39m"
		} else {
			header += "\x1b[38;5;214m [Discard changes? (y/n)]\x1b[39m"
		}
	}
	lines = append(lines, header)

	// Tabs line
	if p.Tab == 0 {
		lines = append(lines, "  \x1b[1;4m"+palette.BlueOpen+"Name"+ansiReset+"    \x1b[38;5;238mFolders\x1b[39m")
	} else {
		lines = append(lines, "  "+palette.AgentFgOpen+"✓ Name"+palette.AgentFgClose+"    \x1b[1;4m"+palette.BlueOpen+"Folders"+ansiReset)
	}

	if p.Tab == 0 {
		lines = append(lines, "")
		lines = append(lines, palette.BlueOpen+"> "+ansiReset+ansiBold+"Project Name"+ansiReset+"\x1b[38;5;210m *\x1b[39m")
		if p.NameInput == "" {
			lines = append(lines, "    \x1b[38;5;238m└─ \x1b[7m\x1b[37m \x1b[0m\x1b[38;5;146mEnter project name...\x1b[39m")
		} else {
			lines = append(lines, "    \x1b[38;5;238m└─ \x1b[39m"+p.NameInput+"\x1b[7m\x1b[37m \x1b[0m")
		}
		if p.NameError != "" {
			lines = append(lines, "       \x1b[38;5;210m"+p.NameError+"\x1b[39m")
		}
		lines = append(lines, "")

		var kbd []string
		if p.ExitConfirm {
			if p.IsNew {
				kbd = append(kbd, p.kbdHint("y", "Cancel Project"), p.kbdHint("n", "Keep Editing"))
			} else {
				kbd = append(kbd, p.kbdHint("y", "Discard"), p.kbdHint("n", "Keep Editing"))
			}
		} else {
			saveText := "Save"
			if p.IsNew {
				saveText = "Create"
			}
			escText := "Close"
			if p.IsNew {
				escText = "Cancel"
			} else if p.isDirty() {
				escText = "Discard"
			}
			kbd = append(kbd, p.kbdHint("→", "Next"), p.kbdHint("ctrl+s", saveText), p.kbdHint("esc", escText))
		}
		lines = append(lines, ansiBold+"Keyboard: "+ansiReset+strings.Join(kbd, "  "))

	} else {
		// Tab 1: Folders
		for i, f := range p.Folders {
			check := "[ ]"
			if f.Checked {
				check = "[x]"
			}
			if i == p.FolderCursor && !p.AddingFolder {
				lines = append(lines, palette.BlueOpen+"> "+ansiReset+"    "+palette.BlueOpen+check+ansiReset+" "+ansiBold+palette.BlueOpen+f.Path+ansiReset)
			} else {
				lines = append(lines, "      "+palette.BlueOpen+check+ansiReset+" "+f.Path)
			}
		}

		if p.AddingFolder {
			inputDisp := p.AddFolderInput + "\x1b[7m\x1b[37m \x1b[0m"
			if p.AddFolderInput == "" {
				inputDisp = "\x1b[7m\x1b[37m \x1b[0m\x1b[38;5;146m~/path/to/folder\x1b[39m"
			}
			lines = append(lines, palette.BlueOpen+"> "+ansiReset+"    "+ansiBold+palette.BlueOpen+"+ "+ansiReset+inputDisp)
			if p.AddFolderError != "" {
				lines = append(lines, "\t\x1b[38;5;210m"+p.AddFolderError+"\x1b[39m")
			}
		} else {
			if p.FolderCursor == len(p.Folders) {
				lines = append(lines, palette.BlueOpen+"> "+ansiReset+"    "+ansiBold+palette.BlueOpen+"+ Add a folder..."+ansiReset)
			} else {
				lines = append(lines, "      "+ansiBold+"+ Add a folder..."+ansiReset)
			}
		}

		lines = append(lines, "")

		var kbd []string
		if p.ExitConfirm {
			if p.IsNew {
				kbd = append(kbd, p.kbdHint("y", "Cancel Project"), p.kbdHint("n", "Keep Editing"))
			} else {
				kbd = append(kbd, p.kbdHint("y", "Discard"), p.kbdHint("n", "Keep Editing"))
			}
		} else if p.AddingFolder {
			kbd = append(kbd, p.kbdHint("enter", "Add"), p.kbdHint("esc", "Cancel"))
		} else {
			toggleText := "Add Folder"
			if p.FolderCursor < len(p.Folders) {
				if p.Folders[p.FolderCursor].Checked {
					toggleText = "Deselect"
				} else {
					toggleText = "Select"
				}
			}
			saveText := "Save"
			if p.IsNew {
				saveText = "Create"
			}
			escText := "Close"
			if p.IsNew {
				escText = "Cancel"
			} else if p.isDirty() {
				escText = "Discard"
			}
			kbd = append(kbd,
				p.kbdHint("↑/↓", "Navigate"),
				p.kbdHint("enter/space", toggleText),
				p.kbdHint("ctrl+s", saveText),
				p.kbdHint("←", "Back"),
				p.kbdHint("esc", escText),
			)
		}
		lines = append(lines, ansiBold+"Keyboard: "+ansiReset+strings.Join(kbd, "  "))
	}

	return strings.Join(lines, "\n")
}

func (p *Panel) kbdHint(key, desc string) string {
	return palette.BlueOpen + key + palette.BlueClose + " " + palette.DimGrayOpen + desc + palette.DimGrayClose
}
