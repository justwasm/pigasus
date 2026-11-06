package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	coreprojects "github.com/justwasm/pigasus/internal/core/cliflags/projects"
)

func TestProjectsPanel_Lifecycle(t *testing.T) {
	tempHome := t.TempDir()
	tempWS := filepath.Join(tempHome, "workspace")
	if err := os.MkdirAll(tempWS, 0755); err != nil {
		t.Fatal(err)
	}

	var switchedProject *coreprojects.LocalProject
	var closed bool

	seams := Seams{
		Close: func() tea.Cmd {
			closed = true
			return nil
		},
		SwitchProject: func(proj coreprojects.LocalProject) tea.Cmd {
			switchedProject = &proj
			return nil
		},
	}

	p := New(tempHome, tempWS, seams)
	if len(p.Projects) == 0 {
		t.Fatal("expected at least default project")
	}

	// 1. Initial render contains "Projects" and "(current)"
	lines := p.Lines(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Projects") || !strings.Contains(joined, "CLI Project") {
		t.Fatalf("unexpected render output:\n%s", joined)
	}

	// 2. Create a new project: 'c'
	handled, _ := p.HandleKey(tea.Key{Code: 'c'})
	if !handled || p.Mode != ModeCreateName {
		t.Fatalf("expected ModeCreateName after 'c', got %v", p.Mode)
	}

	for _, ch := range "My App" {
		p.HandleKey(tea.Key{Code: rune(ch), Text: string(ch)})
	}
	if p.InputBuffer != "My App" {
		t.Fatalf("InputBuffer = %q, want My App", p.InputBuffer)
	}

	// Enter to advance to folders
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.Mode != ModeCreateFolders || p.CreatingName != "My App" {
		t.Fatalf("expected ModeCreateFolders with CreatingName='My App', got %v", p.Mode)
	}

	// Enter folders: "src, docs"
	for _, ch := range "src, docs" {
		p.HandleKey(tea.Key{Code: rune(ch), Text: string(ch)})
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})

	if p.Mode != ModeList {
		t.Fatalf("expected ModeList after creation, got %v", p.Mode)
	}

	// Verify project was saved and is listed
	var foundNew bool
	for _, pr := range p.Projects {
		if pr.Name == "My App" {
			foundNew = true
			if len(pr.Folders) != 2 {
				t.Fatalf("expected 2 folders, got %v", pr.Folders)
			}
			break
		}
	}
	if !foundNew {
		t.Fatal("created project 'My App' not found in projects list")
	}

	// 3. Switch to selected project with Enter
	// Move cursor to "My App"
	for i, pr := range p.Projects {
		if pr.Name == "My App" {
			p.Cursor = i
			break
		}
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if switchedProject == nil || switchedProject.Name != "My App" {
		t.Fatalf("expected switchProject to be called for 'My App', got %v", switchedProject)
	}
	if p.CurrentID != switchedProject.ID {
		t.Fatalf("CurrentID = %q, want %q", p.CurrentID, switchedProject.ID)
	}

	// 4. Rename project
	p.HandleKey(tea.Key{Code: 'r'})
	if p.Mode != ModeRename {
		t.Fatalf("expected ModeRename, got %v", p.Mode)
	}
	p.InputBuffer = "Renamed App"
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.Selected().Name != "Renamed App" {
		t.Fatalf("project name after rename = %q, want 'Renamed App'", p.Selected().Name)
	}

	// 5. Esc closes panel
	closed = false
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !closed {
		t.Fatal("expected Esc to close panel")
	}
}
