package viewerpanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/ui/foundations/viewer"
)

func Test_ViewerPanelPureMessagePassing(t *testing.T) {
	p := New(Seams{
		Spec: func() (viewer.Spec, bool) {
			return viewer.Spec{
				Lines:   []string{"line1", "line2", "line3"},
				Visible: 2,
				Total:   3,
			}, true
		},
	})
	p.Open()

	// Esc emits MsgClose
	claimed, cmd, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc}, 0)
	if !claimed || cmd == nil {
		t.Fatalf("expected Esc to be claimed with command")
	}
	msg := cmd()
	if _, ok := msg.(MsgClose); !ok {
		t.Fatalf("expected MsgClose, got %T", msg)
	}

	// 'q' also emits MsgClose
	claimed, cmd, _ = p.HandleKey(tea.Key{Code: 'q', Text: "q"}, 0)
	if !claimed || cmd == nil {
		t.Fatalf("expected 'q' to be claimed with command")
	}
	msg = cmd()
	if _, ok := msg.(MsgClose); !ok {
		t.Fatalf("expected MsgClose, got %T", msg)
	}
}
