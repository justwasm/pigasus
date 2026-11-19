package projects

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	tea "charm.land/bubbletea/v2"

	coreprojects "github.com/justwasm/pigasus/internal/core/cliflags/projects"
)

func TestProjectsPanel_LifecycleAndWizard(t *testing.T) {
	tempHome := t.TempDir()
	tempWS := filepath.Join(tempHome, "workspace")
	if err := os.MkdirAll(tempWS, 0755); err != nil {
		t.Fatal(err)
	}
	subDir := filepath.Join(tempWS, "subpkg")
	if err := os.MkdirAll(subDir, 0755); err != nil {
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

	// 1. Initial render in list mode
	lines := p.Lines(80)
	joined := strings.Join(lines, "\n")
	if !strings.Contains(joined, "Projects") || !strings.Contains(joined, "Type to search projects...") {
		t.Fatalf("unexpected initial render output:\n%s", joined)
	}
	if !strings.Contains(joined, "+ New project...") {
		t.Fatalf("expected '+ New project...', got:\n%s", joined)
	}
	if !strings.Contains(joined, "Keyboard:") || !strings.Contains(joined, "New Project") {
		t.Fatalf("expected New Project keyboard hint when on row 0, got:\n%s", joined)
	}

	// 2. Navigate Down to first project row
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	if p.Cursor != 1 {
		t.Fatalf("Cursor = %d, want 1", p.Cursor)
	}
	lines = p.Lines(80)
	joined = strings.Join(lines, "\n")
	if !strings.Contains(joined, "Keyboard:") || !strings.Contains(joined, "Actions") {
		t.Fatalf("expected Actions in keyboard hint when on project row, got:\n%s", joined)
	}

	// 3. Search filtering
	for _, ch := range "CLI" {
		p.HandleKey(tea.Key{Code: rune(ch), Text: string(ch)})
	}
	if p.SearchQuery != "CLI" {
		t.Fatalf("SearchQuery = %q, want 'CLI'", p.SearchQuery)
	}
	lines = p.Lines(80)
	joined = strings.Join(lines, "\n")
	if !strings.Contains(joined, "Search:") || !strings.Contains(joined, "CLI") || !strings.Contains(joined, "Clear Search") {
		t.Fatalf("expected active search line and Clear Search hint, got:\n%s", joined)
	}

	// Esc clears search
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if p.SearchQuery != "" {
		t.Fatalf("expected empty SearchQuery after Esc, got %q", p.SearchQuery)
	}

	// 4. Create a new project via wizard
	p.Cursor = 0
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.Mode != ModeEditor || !p.IsNew {
		t.Fatalf("expected ModeEditor with IsNew=true, got Mode=%v IsNew=%v", p.Mode, p.IsNew)
	}
	if p.Tab != 0 {
		t.Fatalf("expected Tab=0, got %d", p.Tab)
	}

	// Backspace default workspace name
	for len(p.NameInput) > 0 {
		p.HandleKey(tea.Key{Code: tea.KeyBackspace})
	}
	// Try to advance with empty name -> should show error
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.NameError != "Enter a project name." || p.Tab != 0 {
		t.Fatalf("expected 'Enter a project name.' error, got %q (Tab %d)", p.NameError, p.Tab)
	}

	// Type unique project name
	for _, ch := range "AlphaProject" {
		p.HandleKey(tea.Key{Code: rune(ch), Text: string(ch)})
	}
	if p.NameInput != "AlphaProject" {
		t.Fatalf("NameInput = %q, want 'AlphaProject'", p.NameInput)
	}

	// Advance to Tab 1 (Folders) with Enter
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.Tab != 1 {
		t.Fatalf("expected Tab=1 (Folders), got %d", p.Tab)
	}
	if len(p.Folders) != 1 || p.Folders[0].Path != tempWS || !p.Folders[0].Checked {
		t.Fatalf("expected initial workspace folder to be checked, got %+v", p.Folders)
	}

	// Cursor is on row 0 (workspace folder). Navigate Down to "+ Add a folder..."
	p.HandleKey(tea.Key{Code: tea.KeyDown})
	if p.FolderCursor != 1 {
		t.Fatalf("FolderCursor = %d, want 1", p.FolderCursor)
	}

	// Press Enter to start adding a folder
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !p.AddingFolder {
		t.Fatal("expected AddingFolder=true")
	}

	// Type subfolder name and submit
	for _, ch := range "subpkg" {
		p.HandleKey(tea.Key{Code: rune(ch), Text: string(ch)})
	}
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.AddingFolder {
		t.Fatalf("expected AddingFolder to be completed, but still adding: error = %q", p.AddFolderError)
	}
	if len(p.Folders) != 2 || p.Folders[1].Path != subDir || !p.Folders[1].Checked {
		t.Fatalf("expected subDir to be added and checked, got %+v", p.Folders)
	}

	// Save with ctrl+s
	p.HandleKey(tea.Key{Code: 's', Mod: tea.ModCtrl})
	if p.Mode != ModeList {
		t.Fatalf("expected ModeList after save, got %v", p.Mode)
	}
	if !strings.Contains(p.StatusMessage, "Created AlphaProject") {
		t.Fatalf("expected '(Created AlphaProject)', got %q", p.StatusMessage)
	}

	// 5. Expand actions on the newly created project and switch
	sel := p.Selected()
	if sel == nil || sel.Name != "AlphaProject" {
		t.Fatalf("expected selected project to be 'AlphaProject', got %+v", sel)
	}

	// Enter opens actions menu
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.ExpandedID != sel.ID || p.ActionCursor != 0 {
		t.Fatalf("expected ExpandedID=%q and ActionCursor=0, got %q and %d", sel.ID, p.ExpandedID, p.ActionCursor)
	}

	// Enter on ActionCursor 0 ("Switch to project") enters switch confirm
	p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if p.SwitchConfirmID != sel.ID {
		t.Fatalf("expected SwitchConfirmID=%q, got %q", sel.ID, p.SwitchConfirmID)
	}
	lines = p.Lines(80)
	joined = strings.Join(lines, "\n")
	if !strings.Contains(joined, "[Switch? Starts a new conversation (y/n)]") {
		t.Fatalf("expected switch prompt in lines, got:\n%s", joined)
	}

	// Press 'y' to confirm switch
	p.HandleKey(tea.Key{Code: 'y'})
	if switchedProject == nil || switchedProject.Name != "AlphaProject" {
		t.Fatalf("expected switchedProject to be AlphaProject, got %+v", switchedProject)
	}
	if p.CurrentID != sel.ID {
		t.Fatalf("CurrentID = %q, want %q", p.CurrentID, sel.ID)
	}

	// 6. Test Esc closes panel
	p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !closed {
		t.Fatal("expected Esc to close panel")
	}
}
