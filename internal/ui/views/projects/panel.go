package projects

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	coreprojects "github.com/justwasm/pigasus/internal/core/cliflags/projects"
	"github.com/justwasm/pigasus/internal/ui/editor/keybind"
	"github.com/justwasm/pigasus/palette"
)

type Mode int

const (
	ModeList Mode = iota
	ModeCreateName
	ModeCreateFolders
	ModeRename
	ModeEditFolders
)

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
	Cursor    int
	Mode      Mode

	InputBuffer  string
	CreatingName string
	TargetID     string

	StatusMessage string
	seams         Seams
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
	p.CurrentID = coreprojects.CurrentLocalProjectID(p.Home, p.Workspace)
	if p.Cursor >= len(p.Projects) {
		p.Cursor = len(p.Projects) - 1
	}
	if p.Cursor < 0 {
		p.Cursor = 0
	}
}

// Selected returns the currently selected project, or nil.
func (p *Panel) Selected() *coreprojects.LocalProject {
	if len(p.Projects) == 0 || p.Cursor < 0 || p.Cursor >= len(p.Projects) {
		return nil
	}
	return &p.Projects[p.Cursor]
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
		switch {
		case key.Code == tea.KeyEsc:
			return true, p.close()
		case key.Code == tea.KeyUp || (key.Code == 'k' && key.Mod == 0):
			if p.Cursor > 0 {
				p.Cursor--
			}
			return true, nil
		case key.Code == tea.KeyDown || (key.Code == 'j' && key.Mod == 0):
			if p.Cursor < len(p.Projects)-1 {
				p.Cursor++
			}
			return true, nil
		case key.Code == tea.KeyEnter:
			if sel := p.Selected(); sel != nil {
				coreprojects.SetCurrentLocalProject(p.Home, p.Workspace, sel.ID)
				p.CurrentID = sel.ID
				return true, p.switchProject(*sel)
			}
			return true, nil
		case (key.Code == 'c' || key.Code == 'n') && key.Mod == 0:
			p.Mode = ModeCreateName
			p.InputBuffer = ""
			p.StatusMessage = ""
			return true, nil
		case key.Code == 'r' && key.Mod == 0:
			if sel := p.Selected(); sel != nil {
				p.Mode = ModeRename
				p.TargetID = sel.ID
				p.InputBuffer = sel.Name
				p.StatusMessage = ""
			}
			return true, nil
		case key.Code == 'e' && key.Mod == 0:
			if sel := p.Selected(); sel != nil {
				p.Mode = ModeEditFolders
				p.TargetID = sel.ID
				p.InputBuffer = strings.Join(sel.Folders, ", ")
				p.StatusMessage = ""
			}
			return true, nil
		case key.Code == 'd' && key.Mod == 0:
			if sel := p.Selected(); sel != nil && sel.ID != coreprojects.DefaultProjectID {
				_ = coreprojects.DeleteLocalProject(p.Home, sel.ID)
				p.Reload()
			}
			return true, nil
		}

	case ModeCreateName:
		switch {
		case key.Code == tea.KeyEsc:
			p.Mode = ModeList
			p.InputBuffer = ""
			return true, nil
		case key.Code == tea.KeyEnter:
			name := strings.TrimSpace(p.InputBuffer)
			if name == "" {
				p.StatusMessage = "Project name cannot be empty."
				return true, nil
			}
			p.CreatingName = name
			p.Mode = ModeCreateFolders
			p.InputBuffer = ""
			p.StatusMessage = ""
			return true, nil
		case key.Code == tea.KeyBackspace:
			if r := []rune(p.InputBuffer); len(r) > 0 {
				p.InputBuffer = string(r[:len(r)-1])
			}
			return true, nil
		default:
			if keybind.IsTypedText(key) {
				p.InputBuffer += key.Text
				return true, nil
			}
		}

	case ModeCreateFolders:
		switch {
		case key.Code == tea.KeyEsc:
			p.Mode = ModeList
			p.InputBuffer = ""
			return true, nil
		case key.Code == tea.KeyEnter:
			folders := p.parseFolders(p.InputBuffer)
			projID := fmt.Sprintf("proj-%d", time.Now().UnixNano())
			proj := coreprojects.LocalProject{
				ID:      projID,
				Name:    p.CreatingName,
				Folders: folders,
			}
			if err := coreprojects.SaveLocalProject(p.Home, proj); err != nil {
				p.StatusMessage = "Failed to save: " + err.Error()
				return true, nil
			}
			p.Reload()
			for i, pr := range p.Projects {
				if pr.ID == projID {
					p.Cursor = i
					break
				}
			}
			p.Mode = ModeList
			p.InputBuffer = ""
			return true, nil
		case key.Code == tea.KeyBackspace:
			if r := []rune(p.InputBuffer); len(r) > 0 {
				p.InputBuffer = string(r[:len(r)-1])
			}
			return true, nil
		default:
			if keybind.IsTypedText(key) {
				p.InputBuffer += key.Text
				return true, nil
			}
		}

	case ModeRename:
		switch {
		case key.Code == tea.KeyEsc:
			p.Mode = ModeList
			p.InputBuffer = ""
			return true, nil
		case key.Code == tea.KeyEnter:
			name := strings.TrimSpace(p.InputBuffer)
			if name == "" {
				p.StatusMessage = "Project name cannot be empty."
				return true, nil
			}
			if sel := p.Selected(); sel != nil {
				sel.Name = name
				_ = coreprojects.SaveLocalProject(p.Home, *sel)
				p.Reload()
			}
			p.Mode = ModeList
			p.InputBuffer = ""
			return true, nil
		case key.Code == tea.KeyBackspace:
			if r := []rune(p.InputBuffer); len(r) > 0 {
				p.InputBuffer = string(r[:len(r)-1])
			}
			return true, nil
		default:
			if keybind.IsTypedText(key) {
				p.InputBuffer += key.Text
				return true, nil
			}
		}

	case ModeEditFolders:
		switch {
		case key.Code == tea.KeyEsc:
			p.Mode = ModeList
			p.InputBuffer = ""
			return true, nil
		case key.Code == tea.KeyEnter:
			folders := p.parseFolders(p.InputBuffer)
			if sel := p.Selected(); sel != nil {
				sel.Folders = folders
				_ = coreprojects.SaveLocalProject(p.Home, *sel)
				p.Reload()
			}
			p.Mode = ModeList
			p.InputBuffer = ""
			return true, nil
		case key.Code == tea.KeyBackspace:
			if r := []rune(p.InputBuffer); len(r) > 0 {
				p.InputBuffer = string(r[:len(r)-1])
			}
			return true, nil
		default:
			if keybind.IsTypedText(key) {
				p.InputBuffer += key.Text
				return true, nil
			}
		}
	}

	return false, nil
}

func (p *Panel) parseFolders(raw string) []string {
	parts := strings.Split(raw, ",")
	var out []string
	for _, part := range parts {
		trimmed := strings.TrimSpace(part)
		if trimmed == "" {
			continue
		}
		resolved, err := coreprojects.ResolveFolderPath(p.Workspace, trimmed)
		if err == nil {
			out = append(out, resolved)
		} else {
			out = append(out, filepath.Clean(trimmed))
		}
	}
	return out
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
	var lines []string

	lines = append(lines, "  \x1b[1;4m"+palette.BlueOpen+"Projects\x1b[0m "+palette.DimGrayOpen+"(tab / esc to exit)"+palette.DimGrayClose, "")

	switch p.Mode {
	case ModeList:
		for i, pr := range p.Projects {
			isCursor := i == p.Cursor
			isCurrent := pr.ID == p.CurrentID

			prefix := "  "
			if isCursor {
				prefix = palette.BlueOpen + "> " + palette.BlueClose
			}

			bullet := palette.DimGrayOpen + "●" + palette.DimGrayClose
			if isCurrent {
				bullet = palette.AgentFgOpen + "●" + palette.AgentFgClose
			}

			nameStyle := pr.Name
			if isCursor {
				nameStyle = "\x1b[1m" + palette.BlueOpen + pr.Name + "\x1b[0m"
			} else {
				nameStyle = "\x1b[1m" + pr.Name + "\x1b[0m"
			}

			currentBadge := ""
			if isCurrent {
				currentBadge = " " + palette.AgentFgOpen + "(current)" + palette.AgentFgClose
			}

			lines = append(lines, prefix+bullet+" "+nameStyle+currentBadge)

			folderText := strings.Join(pr.Folders, ", ")
			if folderText == "" {
				folderText = "(no folders configured)"
			}
			lines = append(lines, "    "+palette.DimGrayOpen+folderText+palette.DimGrayClose)
		}

		lines = append(lines, "", "  "+palette.DimGrayOpen+"Enter Switch · c Create · e Edit folders · r Rename · d Delete · Esc Close"+palette.DimGrayClose)

	case ModeCreateName:
		lines = append(lines, "  Create New Project", "")
		lines = append(lines, "  Name: "+p.InputBuffer+"█")
		if p.StatusMessage != "" {
			lines = append(lines, "", "  \x1b[31m"+p.StatusMessage+"\x1b[0m")
		}
		lines = append(lines, "", "  "+palette.DimGrayOpen+"Enter Next · Esc Cancel"+palette.DimGrayClose)

	case ModeCreateFolders:
		lines = append(lines, "  Create Project: "+p.CreatingName, "")
		lines = append(lines, "  Folders (comma-separated, absolute, ~, or relative):")
		lines = append(lines, "  > "+p.InputBuffer+"█")
		if p.StatusMessage != "" {
			lines = append(lines, "", "  \x1b[31m"+p.StatusMessage+"\x1b[0m")
		}
		lines = append(lines, "", "  "+palette.DimGrayOpen+"Enter Create · Esc Cancel"+palette.DimGrayClose)

	case ModeRename:
		lines = append(lines, "  Rename Project", "")
		lines = append(lines, "  New Name: "+p.InputBuffer+"█")
		if p.StatusMessage != "" {
			lines = append(lines, "", "  \x1b[31m"+p.StatusMessage+"\x1b[0m")
		}
		lines = append(lines, "", "  "+palette.DimGrayOpen+"Enter Save · Esc Cancel"+palette.DimGrayClose)

	case ModeEditFolders:
		lines = append(lines, "  Edit Folders", "")
		lines = append(lines, "  Folders (comma-separated):")
		lines = append(lines, "  > "+p.InputBuffer+"█")
		if p.StatusMessage != "" {
			lines = append(lines, "", "  \x1b[31m"+p.StatusMessage+"\x1b[0m")
		}
		lines = append(lines, "", "  "+palette.DimGrayOpen+"Enter Save · Esc Cancel"+palette.DimGrayClose)
	}

	return strings.Join(lines, "\n")
}
