package diffpanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/vim"
)

func TestDiffPanel_CommentEditor_VimIntegration(t *testing.T) {
	p := New(Seams{
		VimEnabled: func() bool { return true },
	})

	if p.commentEditor == nil || !p.commentEditor.VimEnabled {
		t.Fatalf("expected commentEditor to have VimEnabled set to true")
	}

	// Fake an inline diff view for path "test.go"
	p.view = 1
	p.viewPath = "test.go"
	p.inlineRow = 5

	// Press 'c' to open comment editor
	handled, _ := p.HandleKey(tea.Key{Code: 'c'})
	if !handled || !p.commentEditor.Active {
		t.Fatalf("expected 'c' to activate commentEditor on inline diff view")
	}

	if p.commentEditor.Line != 5 {
		t.Errorf("commentEditor.Line = %d, want 5", p.commentEditor.Line)
	}

	if p.commentEditor.VimState.Mode != vim.Insert {
		t.Errorf("expected vim mode to be vim.Insert, got %v", p.commentEditor.VimState.Mode)
	}

	// Type text into buffer
	p.commentEditor.Buffer.SetValue("needs refactor")

	// Press Enter to save comment
	handledEnter, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handledEnter || p.commentEditor.Active {
		t.Fatalf("expected Enter to save comment and close editor")
	}

	s := p.storeFor("test.go")
	c, ok := s.At(5)
	if !ok || c.Body != "needs refactor" {
		t.Errorf("expected comment 'needs refactor' at line 5, got ok=%v, comment=%+v", ok, c)
	}
}
