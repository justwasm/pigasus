package slashpicker

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
	"github.com/justwasm/pigasus/palette"
)

func TestRenderRow_AgentCommandColoring(t *testing.T) {
	withScheme(t)

	cmd := &slashcommands.Command{
		Name:        "/review",
		Description: "Review code changes",
		Source:      slashcommands.SourceAgent,
		Badge:       "agent",
	}

	// 1. Unselected row: should contain palette.AgentFgOpen and "[agent]" badge
	var bUnselected strings.Builder
	renderRow(&bUnselected, cmd, false)
	gotUnselected := bUnselected.String()

	if !strings.Contains(gotUnselected, palette.AgentFgOpen) {
		t.Errorf("unselected agent row should contain AgentFgOpen color, got: %q", gotUnselected)
	}
	if !strings.Contains(gotUnselected, "[agent]") {
		t.Errorf("unselected agent row should contain [agent] badge, got: %q", gotUnselected)
	}

	// 2. Selected (highlighted) row: should contain bold + AgentFgOpen
	var bSelected strings.Builder
	renderRow(&bSelected, cmd, true)
	gotSelected := bSelected.String()

	if !strings.Contains(gotSelected, "\x1b[1m"+palette.AgentFgOpen) {
		t.Errorf("selected agent row should contain bold AgentFgOpen color, got: %q", gotSelected)
	}
	if !strings.Contains(gotSelected, "> /review") {
		t.Errorf("selected agent row should contain '> /review', got: %q", gotSelected)
	}
	if !strings.Contains(gotSelected, "[agent]") {
		t.Errorf("selected agent row should contain [agent] badge, got: %q", gotSelected)
	}
}

func TestPanel_DynamicAgentCommandsFiltered(t *testing.T) {
	buf := "/rev"
	p := New(Seams{
		Buffer: func() string { return buf },
		CustomCommands: func() []slashcommands.Command {
			return []slashcommands.Command{
				{Name: "/review", Description: "Review code changes", Source: slashcommands.SourceAgent, Badge: "agent"},
			}
		},
	})

	p.Update([]rune(buf))
	if len(p.all) == 0 {
		t.Fatalf("expected /review to be filtered and present in popup rows")
	}
	found := false
	for _, c := range p.all {
		if c.Name == "/review" && c.Source == slashcommands.SourceAgent {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("expected dynamic command /review to be found in p.all: %+v", p.all)
	}
}
