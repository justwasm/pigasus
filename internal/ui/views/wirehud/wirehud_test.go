package wirehud

import (
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/x/ansi"
)

func TestHUD_LifecycleAndMetrics(t *testing.T) {
	hud := New()
	if hud.IsVisible() {
		t.Fatal("expected HUD to be initially hidden")
	}

	hud.Toggle()
	if !hud.IsVisible() {
		t.Fatal("expected HUD to be visible after toggle")
	}

	hud.RecordStreamStart("CloudCode/gRPC")
	time.Sleep(10 * time.Millisecond)
	hud.RecordFirstToken()
	hud.RecordChunk(100)
	hud.RecordChunk(200)
	hud.RecordStreamDone()

	m := hud.GetMetrics()
	if m.Protocol != "CloudCode/gRPC" {
		t.Errorf("got protocol %q, want CloudCode/gRPC", m.Protocol)
	}
	if m.SentPkts != 1 {
		t.Errorf("got sent packets %d, want 1", m.SentPkts)
	}
	if m.RecvPkts != 2 {
		t.Errorf("got recv packets %d, want 2", m.RecvPkts)
	}
	if m.RecvBytes != 300 {
		t.Errorf("got recv bytes %d, want 300", m.RecvBytes)
	}
	if m.TTFT <= 0 {
		t.Errorf("expected positive TTFT, got %v", m.TTFT)
	}
	if m.RTT <= 0 {
		t.Errorf("expected positive RTT, got %v", m.RTT)
	}
}

func TestHUD_RenderBox(t *testing.T) {
	hud := New()
	hud.UpdateMetrics(Metrics{
		Protocol:     "CloudCode/Live",
		Status:       "STREAMING",
		RTT:          45 * time.Millisecond,
		TTFT:         120 * time.Millisecond,
		SentPkts:     15,
		SentBytes:    4096,
		RecvPkts:     62,
		RecvBytes:    16384,
		TokensPerSec: 42.5,
	})

	box := hud.RenderBox(40)
	if len(box) != 7 {
		t.Fatalf("expected 7 lines in box, got %d", len(box))
	}

	full := strings.Join(box, "\n")
	stripped := ansi.Strip(full)

	if !strings.Contains(stripped, "WIRE DIAGNOSTICS") {
		t.Errorf("rendered box missing title: %s", stripped)
	}
	if !strings.Contains(stripped, "CloudCode/Live") {
		t.Errorf("rendered box missing protocol: %s", stripped)
	}
	if !strings.Contains(stripped, "TTFT: 120ms") {
		t.Errorf("rendered box missing TTFT: %s", stripped)
	}
	if !strings.Contains(stripped, "42.5 tok/s") {
		t.Errorf("rendered box missing token rate: %s", stripped)
	}
}

func TestHUD_OverlayOnRows(t *testing.T) {
	hud := New()
	hud.SetVisible(true)
	hud.UpdateMetrics(Metrics{
		Protocol: "TestWire",
		Status:   "IDLE",
	})

	baseRows := []string{
		"Row 0: Top bar header line .....................................................",
		"Row 1: Transcript message line 1 ...............................................",
		"Row 2: Transcript message line 2 ...............................................",
		"Row 3: Transcript message line 3 ...............................................",
		"Row 4: Transcript message line 4 ...............................................",
		"Row 5: Transcript message line 5 ...............................................",
		"Row 6: Transcript message line 6 ...............................................",
		"Row 7: Transcript message line 7 ...............................................",
		"Row 8: Transcript message line 8 ...............................................",
		"Row 9: Bottom statusline .......................................................",
	}

	screenWidth := 80
	overlaid := hud.OverlayOnRows(baseRows, screenWidth)

	if len(overlaid) != len(baseRows) {
		t.Fatalf("expected %d rows, got %d", len(baseRows), len(overlaid))
	}

	// Verify HUD box appeared in row 1-7
	row1Clean := ansi.Strip(overlaid[1])
	if !strings.Contains(row1Clean, "WIRE DIAGNOSTICS") {
		t.Errorf("row 1 expected to contain HUD title, got: %s", row1Clean)
	}

	// Row 0 should remain untouched
	if overlaid[0] != baseRows[0] {
		t.Errorf("row 0 modified: got %q, want %q", overlaid[0], baseRows[0])
	}
}

func TestHUD_RenderBox_VoiceTelemetry(t *testing.T) {
	hud := New()
	hud.SetVisible(true)
	hud.UpdateMetrics(Metrics{
		Protocol: "CloudCode/Live",
		Status:   "STREAMING",
	})
	hud.UpdateVoiceMetrics(
		true,
		"Connected",
		3200,
		4,
		4800,
		6,
		3,
		2,
		1,
		85*time.Millisecond,
	)

	box := hud.RenderBox(44)
	if len(box) != 11 {
		t.Fatalf("expected 11 lines in voice-enabled box, got %d", len(box))
	}

	stripped := ansi.Strip(strings.Join(box, "\n"))
	for _, want := range []string{
		"Voice:",
		"Connected (t:2 int:1)",
		"Voice Tx:",
		"4 ch (3.1 KB)",
		"Voice Rx:",
		"6 ch (4.7 KB) txt:3",
		"Voice Lat:",
		"85ms",
	} {
		if !strings.Contains(stripped, want) {
			t.Errorf("rendered voice HUD missing %q:\n%s", want, stripped)
		}
	}

	baseRows := make([]string, 14)
	for i := range baseRows {
		baseRows[i] = strings.Repeat(".", 90)
	}
	overlaid := hud.OverlayOnRows(baseRows, 90)
	overlaidClean := ansi.Strip(strings.Join(overlaid, "\n"))
	if !strings.Contains(overlaidClean, "Voice Tx:") || !strings.Contains(overlaidClean, "Voice Lat:") {
		t.Fatalf("expected OverlayOnRows to render voice telemetry rows, got:\n%s", overlaidClean)
	}
}
