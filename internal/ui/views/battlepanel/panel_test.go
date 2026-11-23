package battlepanel

import (
	"fmt"
	"regexp"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/app/battlemode"
	"github.com/justwasm/pigasus/palette"
)

var sgrPattern = regexp.MustCompile(`\x1b\[[0-9;]*m`)

func stripSGR(s string) string {
	return sgrPattern.ReplaceAllString(s, "")
}

func sampleSnapshot() battlemode.SessionSnapshot {
	var longDiff strings.Builder
	longDiff.WriteString("diff --git a/main.go b/main.go\n")
	longDiff.WriteString("--- a/main.go\n")
	longDiff.WriteString("+++ b/main.go\n")
	longDiff.WriteString("@@ -1,3 +1,25 @@\n")
	longDiff.WriteString("-func old() {}\n")
	longDiff.WriteString("+func newImpl() {}\n")
	for i := 0; i < 30; i++ {
		longDiff.WriteString(fmt.Sprintf("+// added line %02d\n", i))
	}

	return battlemode.SessionSnapshot{
		Task:        "Refactor greeting handler",
		WinnerIndex: -1,
		Running:     true,
		Arms: []battlemode.ArmState{
			{
				Spec:         battlemode.ArmSpec{Index: 0, Model: "modelA"},
				Status:       battlemode.StatusDone,
				Duration:     1200 * time.Millisecond,
				Usage:        battlemode.TokenUsage{InputTokens: 100, OutputTokens: 50, TotalTokens: 150},
				Response:     "Refactored main.go cleanly.",
				Diff:         longDiff.String(),
				ChangedFiles: []string{"main.go"},
			},
			{
				Spec:     battlemode.ArmSpec{Index: 1, Model: "modelB"},
				Status:   battlemode.StatusRunning,
				Response: "",
			},
			{
				Spec:         battlemode.ArmSpec{Index: 2, Model: "modelC"},
				Status:       battlemode.StatusDone,
				Duration:     800 * time.Millisecond,
				Response:     "Alternative implementation in helper.go.",
				Diff:         "diff --git a/helper.go b/helper.go\n--- /dev/null\n+++ b/helper.go\n@@ -0,0 +1 @@\n+package main\n",
				ChangedFiles: []string{"helper.go"},
			},
		},
	}
}

func TestPanel_RenderArmTabsAndDiffColors(t *testing.T) {
	p := NewPanel(Options{})
	p.SetSize(100, 24)
	p.SetSnapshot(sampleSnapshot())
	p.Open()

	rendered := p.View()
	if rendered == "" {
		t.Fatal("expected non-empty rendered view when active")
	}
	plain := stripSGR(rendered)

	// Header & task
	if !strings.Contains(plain, "Battle Mode") {
		t.Errorf("rendered view missing Battle Mode header:\n%s", plain)
	}
	if !strings.Contains(plain, "Refactor greeting handler") {
		t.Errorf("rendered view missing task:\n%s", plain)
	}

	// Arm tabs: [1] modelA ✓ 1.2s and [2] modelB ⣾ ...
	if !strings.Contains(plain, "[1] modelA ✓ 1.2s") {
		t.Errorf("rendered view missing arm 1 tab '[1] modelA ✓ 1.2s':\n%s", plain)
	}
	if !strings.Contains(plain, "[2] modelB ⣾ ...") {
		t.Errorf("rendered view missing arm 2 tab '[2] modelB ⣾ ...':\n%s", plain)
	}

	// Response & Diff ANSI coloring assertions (AGENTS.md §13)
	if !strings.Contains(plain, "Refactored main.go cleanly.") {
		t.Errorf("rendered view missing response text:\n%s", plain)
	}
	if !strings.Contains(rendered, palette.ToolAddOpen+"+func newImpl() {}") {
		t.Errorf("expected added diff line to carry palette.ToolAddOpen ANSI sequence:\n%q", rendered)
	}
	if !strings.Contains(rendered, palette.ToolRemoveOpen+"-func old() {}") {
		t.Errorf("expected removed diff line to carry palette.ToolRemoveOpen ANSI sequence:\n%q", rendered)
	}
	if !strings.Contains(rendered, palette.BlueOpen+"@@ -1,3 +1,25 @@") {
		t.Errorf("expected hunk header to carry palette.BlueOpen ANSI sequence:\n%q", rendered)
	}
}

func TestPanel_ArmSwitchingAndScrolling(t *testing.T) {
	p := NewPanel(Options{})
	p.SetSize(100, 18)
	p.SetSnapshot(sampleSnapshot())
	p.Open()

	// Scroll down on arm 0
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyDown}); !handled {
		t.Fatal("Down key not handled")
	}
	if p.ScrollOffset() != 1 {
		t.Errorf("ScrollOffset = %d, want 1", p.ScrollOffset())
	}
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyPgDown}); !handled {
		t.Fatal("PgDown key not handled")
	}
	if p.ScrollOffset() <= 1 {
		t.Errorf("expected PgDown to advance scroll offset, got %d", p.ScrollOffset())
	}
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyPgUp}); !handled {
		t.Fatal("PgUp key not handled")
	}
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyUp}); !handled {
		t.Fatal("Up key not handled")
	}

	// Tab switches to arm 1 (index 1) and resets scroll
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyTab}); !handled {
		t.Fatal("Tab key not handled")
	}
	if p.ActiveArm() != 1 {
		t.Errorf("ActiveArm = %d, want 1", p.ActiveArm())
	}
	if p.ScrollOffset() != 0 {
		t.Errorf("expected scroll reset on arm switch, got %d", p.ScrollOffset())
	}
	if plain := stripSGR(p.View()); !strings.Contains(plain, "(running...)") {
		t.Errorf("expected running placeholder for arm 1:\n%s", plain)
	}

	// Right switches to arm 2
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyRight}); !handled {
		t.Fatal("Right key not handled")
	}
	if p.ActiveArm() != 2 {
		t.Errorf("ActiveArm = %d, want 2", p.ActiveArm())
	}
	if plain := stripSGR(p.View()); !strings.Contains(plain, "Alternative implementation in helper.go.") {
		t.Errorf("expected arm 2 response in view:\n%s", plain)
	}

	// Left switches back to arm 1
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyLeft}); !handled {
		t.Fatal("Left key not handled")
	}
	if p.ActiveArm() != 1 {
		t.Errorf("ActiveArm = %d, want 1", p.ActiveArm())
	}

	// Number key '3' jumps to arm 2
	if handled, _ := p.HandleKey(tea.Key{Code: '3', Text: "3"}); !handled {
		t.Fatal("key '3' not handled")
	}
	if p.ActiveArm() != 2 {
		t.Errorf("ActiveArm = %d, want 2", p.ActiveArm())
	}
}

func TestPanel_PickWinnerEliminateAndEscDismissal(t *testing.T) {
	picked := -1
	eliminated := -1
	closed := false

	p := NewPanel(Options{
		OnPickWinner: func(idx int) { picked = idx },
		OnEliminate:  func(idx int) { eliminated = idx },
		OnClose: func() tea.Cmd {
			closed = true
			return nil
		},
	})
	p.SetSize(100, 24)
	snap := sampleSnapshot()
	p.SetSnapshot(snap)
	p.Open()

	// 'x' eliminates active arm (0)
	if handled, _ := p.HandleKey(tea.Key{Code: 'x', Text: "x"}); !handled {
		t.Fatal("key 'x' not handled")
	}
	if eliminated != 0 {
		t.Errorf("eliminated = %d, want 0", eliminated)
	}
	if plain := stripSGR(p.View()); !strings.Contains(plain, "Eliminated: [1] modelA") {
		t.Errorf("expected elimination notice in rendered view:\n%s", plain)
	}

	// Switch to arm 2 ('3') and pick winner with 'w'
	p.HandleKey(tea.Key{Code: '3', Text: "3"})
	if handled, _ := p.HandleKey(tea.Key{Code: 'w', Text: "w"}); !handled {
		t.Fatal("key 'w' not handled")
	}
	if picked != 2 {
		t.Errorf("picked = %d, want 2", picked)
	}

	// Update snapshot with winner marked and verify rendered winner badge
	snap.WinnerIndex = 2
	snap.Running = false
	snap.Arms[2].Winner = true
	p.SetSnapshot(snap)
	plain := stripSGR(p.View())
	if !strings.Contains(plain, "★ WINNER: modelC") {
		t.Errorf("expected winner badge in rendered header:\n%s", plain)
	}
	if !strings.Contains(plain, "[3] modelC ★") {
		t.Errorf("expected winner star on arm 3 tab:\n%s", plain)
	}

	// Enter also triggers OnPickWinner on arm 0
	picked = -1
	p.HandleKey(tea.Key{Code: '1', Text: "1"})
	if handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEnter}); !handled {
		t.Fatal("Enter key not handled")
	}
	if picked != 0 {
		t.Errorf("picked via Enter = %d, want 0", picked)
	}

	// Ctrl+C falls through (not swallowed)
	if handled, _ := p.HandleKey(tea.Key{Code: 'c', Mod: tea.ModCtrl}); handled {
		t.Error("Ctrl+C should fall through to host")
	}

	// Esc closes overlay without leaking (handled=true)
	handled, _ := p.HandleKey(tea.Key{Code: tea.KeyEsc})
	if !handled {
		t.Fatal("Esc must be handled by overlay so it does not leak")
	}
	if !closed {
		t.Fatal("expected OnClose callback to be invoked")
	}
	if p.Active() {
		t.Fatal("expected panel to be inactive after Esc")
	}
	if p.View() != "" {
		t.Errorf("expected empty View() after Esc dismissal, got %q", p.View())
	}
}
