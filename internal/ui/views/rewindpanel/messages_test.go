package rewindpanel

import (
	"testing"

	tea "charm.land/bubbletea/v2"
	"github.com/justwasm/pigasus/internal/app/session/rewind"
)

func Test_RewindPanelPureMessagePassing(t *testing.T) {
	p := New(Seams{
		Steps: func() []rewind.Step {
			return []rewind.Step{
				{UserIdx: 0, EndIdx: 1, Content: "Turn 1"},
				{UserIdx: 2, EndIdx: 3, Content: "Turn 2"},
			}
		},
	})

	// Enter -> RewoundMsg
	handled, cmd := p.HandleKey(tea.Key{Code: tea.KeyEnter})
	if !handled || cmd == nil {
		t.Fatalf("expected Enter handled with cmd, got %v, %v", handled, cmd)
	}
	msg := cmd()
	rewound, ok := msg.(RewoundMsg)
	if !ok || rewound.Index != 0 {
		t.Fatalf("expected RewoundMsg(0), got %v", msg)
	}

	// Esc -> DismissedMsg
	handled, cmd = p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled || cmd == nil {
		t.Fatalf("expected Esc handled with cmd, got %v, %v", handled, cmd)
	}
	msg = cmd()
	if _, ok := msg.(DismissedMsg); !ok {
		t.Fatalf("expected DismissedMsg, got %T", msg)
	}
}
