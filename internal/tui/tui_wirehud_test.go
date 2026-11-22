package tui

import (
	"flag"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/internal/ui/views/wirehud"
)

func TestRootModel_WireHUD_SlashCommandAndKeyToggle(t *testing.T) {
	ws := t.TempDir()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	m := newRootModel(nil, "user@test", "gemini-2.5-pro", "tier1", ws, false)
	setupTUIToolsAndPolicy(m, fs)
	m.viewport.width = 100
	m.viewport.height = 30

	if m.overlays.wireHUD == nil {
		t.Fatal("expected wireHUD to be initialized")
	}
	if m.overlays.wireHUD.IsVisible() {
		t.Fatal("expected wireHUD to be initially hidden")
	}

	// 1. Toggle via /wire
	handled, _ := m.handleSlashCommand("/wire")
	if !handled {
		t.Fatal("expected /wire to be handled")
	}
	if !m.overlays.wireHUD.IsVisible() {
		t.Fatal("expected wireHUD to become visible after /wire")
	}

	// Toggle off via /wire off
	handled, _ = m.handleSlashCommand("/wire off")
	if !handled || m.overlays.wireHUD.IsVisible() {
		t.Fatal("expected wireHUD to become hidden after /wire off")
	}

	// 2. Toggle via Alt+W key shortcut
	keyAltW := tea.Key{Code: 'w', Mod: tea.ModAlt}
	m.updateKeyMsg(tea.KeyPressMsg(keyAltW))
	if !m.overlays.wireHUD.IsVisible() {
		t.Fatal("expected wireHUD to become visible after Alt+W")
	}

	// 3. Verify View() renders HUD on screen
	m.overlays.wireHUD.UpdateMetrics(wirehud.Metrics{
		Protocol:     "CloudCode/Live",
		Status:       "STREAMING",
		RTT:          25 * time.Millisecond,
		TTFT:         95 * time.Millisecond,
		SentPkts:     8,
		SentBytes:    2048,
		RecvPkts:     34,
		RecvBytes:    8192,
		TokensPerSec: 35.0,
	})

	view := m.View()
	rawContent := view.Content
	clean := ansi.Strip(rawContent)

	if !strings.Contains(clean, "WIRE DIAGNOSTICS") {
		t.Fatalf("expected rendered viewport content to include 'WIRE DIAGNOSTICS', got:\n%s", clean)
	}
	if !strings.Contains(clean, "CloudCode/Live") {
		t.Fatalf("expected rendered viewport content to include protocol 'CloudCode/Live', got:\n%s", clean)
	}
	if !strings.Contains(clean, "TTFT: 95ms") {
		t.Fatalf("expected rendered viewport content to include 'TTFT: 95ms', got:\n%s", clean)
	}
}
