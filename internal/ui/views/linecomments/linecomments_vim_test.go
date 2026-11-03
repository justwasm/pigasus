package linecomments

import (
	"testing"

	"github.com/justwasm/pigasus/vim"
)

func TestLineComments_VimEditor(t *testing.T) {
	ed := NewEditor(true)
	if !ed.VimEnabled {
		t.Fatalf("expected VimEnabled to be true")
	}

	ed.Open(10, "initial comment")
	if !ed.Active {
		t.Fatalf("expected editor to be active")
	}
	if ed.Line != 10 {
		t.Errorf("ed.Line = %d, want 10", ed.Line)
	}
	if ed.VimState.Mode != vim.Insert {
		t.Errorf("ed.VimState.Mode = %v, want vim.Insert", ed.VimState.Mode)
	}

	ed.Close()
	if ed.Active {
		t.Errorf("expected editor to be inactive after Close")
	}
}
