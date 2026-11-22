// Package wirehud implements the interactive TUI --debug-wire diagnostics HUD overlay.
//
// It renders an observable top-right overlay displaying real-time streaming metrics:
// RTT, time to first token (TTFT), packet counts, byte throughput, and token generation rate.
package wirehud

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/justwasm/pigasus/palette"
)

// Metrics holds the active wire telemetry values.
type Metrics struct {
	Protocol     string
	Status       string
	RTT          time.Duration
	TTFT         time.Duration
	SentPkts     int64
	SentBytes    int64
	RecvPkts     int64
	RecvBytes    int64
	TokensPerSec float64
}

// HUD maintains the visibility state and real-time metrics for the wire overlay.
type HUD struct {
	mu      sync.RWMutex
	visible bool
	metrics Metrics

	turnStartTime time.Time
	tokenCount    int64
}

// New constructs an initialized HUD overlay.
func New() *HUD {
	return &HUD{
		metrics: Metrics{
			Protocol: "CloudCode/gRPC",
			Status:   "IDLE",
		},
	}
}

// Toggle flips visibility and returns the new state.
func (h *HUD) Toggle() bool {
	if h == nil {
		return false
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.visible = !h.visible
	return h.visible
}

// SetVisible sets the explicit visibility.
func (h *HUD) SetVisible(v bool) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.visible = v
}

// IsVisible reports whether the HUD is currently enabled and showing.
func (h *HUD) IsVisible() bool {
	if h == nil {
		return false
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.visible
}

// UpdateMetrics updates the telemetry metrics displayed by the HUD.
func (h *HUD) UpdateMetrics(m Metrics) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.metrics = m
}

// GetMetrics returns a copy of the current metrics.
func (h *HUD) GetMetrics() Metrics {
	if h == nil {
		return Metrics{}
	}
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.metrics
}

// RecordStreamStart initializes tracking for a new streaming turn.
func (h *HUD) RecordStreamStart(proto string) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if proto != "" {
		h.metrics.Protocol = proto
	}
	h.metrics.Status = "STREAMING"
	h.turnStartTime = time.Now()
	h.metrics.TTFT = 0
	h.tokenCount = 0
	h.metrics.SentPkts++
}

// RecordFirstToken notes the arrival of the first token chunk.
func (h *HUD) RecordFirstToken() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	if !h.turnStartTime.IsZero() && h.metrics.TTFT == 0 {
		h.metrics.TTFT = time.Since(h.turnStartTime)
	}
}

// RecordChunk records an incoming chunk and updates rate calculations.
func (h *HUD) RecordChunk(bytesLen int) {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.metrics.RecvPkts++
	h.metrics.RecvBytes += int64(bytesLen)
	h.tokenCount++
	elapsed := time.Since(h.turnStartTime).Seconds()
	if elapsed > 0.05 {
		h.metrics.TokensPerSec = float64(h.tokenCount) / elapsed
	}
}

// RecordStreamDone marks the stream finished.
func (h *HUD) RecordStreamDone() {
	if h == nil {
		return
	}
	h.mu.Lock()
	defer h.mu.Unlock()
	h.metrics.Status = "IDLE"
	if !h.turnStartTime.IsZero() {
		h.metrics.RTT = time.Since(h.turnStartTime)
	}
}

func formatBytes(b int64) string {
	if b < 1024 {
		return fmt.Sprintf("%d B", b)
	} else if b < 1024*1024 {
		return fmt.Sprintf("%.1f KB", float64(b)/1024)
	}
	return fmt.Sprintf("%.1f MB", float64(b)/(1024*1024))
}

// RenderBox renders the HUD lines as a framed rectangular box.
func (h *HUD) RenderBox(width int) []string {
	if h == nil {
		return nil
	}
	h.mu.RLock()
	m := h.metrics
	h.mu.RUnlock()

	if width < 36 {
		width = 36
	}

	innerW := width - 4
	lineFmt := func(label, val string) string {
		content := fmt.Sprintf("%-10s %s", label, val)
		cLen := ansi.StringWidth(content)
		pad := innerW - cLen
		if pad < 0 {
			pad = 0
		}
		return "│ " + content + strings.Repeat(" ", pad) + " │"
	}

	rttStr := "-"
	if m.RTT > 0 {
		rttStr = fmt.Sprintf("%dms", m.RTT.Milliseconds())
	}
	ttftStr := "-"
	if m.TTFT > 0 {
		ttftStr = fmt.Sprintf("%dms", m.TTFT.Milliseconds())
	}
	rateStr := "-"
	if m.TokensPerSec > 0 {
		rateStr = fmt.Sprintf("%.1f tok/s", m.TokensPerSec)
	}

	title := " WIRE DIAGNOSTICS [/wire] "
	titleLen := len(title)
	borderRight := width - 2 - 2 - titleLen
	if borderRight < 2 {
		borderRight = 2
	}

	topBorder := "┌─" + title + strings.Repeat("─", borderRight) + "┐"
	botBorder := "└" + strings.Repeat("─", width-2) + "┘"

	rawLines := []string{
		topBorder,
		lineFmt("Protocol:", fmt.Sprintf("%s [%s]", m.Protocol, m.Status)),
		lineFmt("Latency:", fmt.Sprintf("RTT: %s  TTFT: %s", rttStr, ttftStr)),
		lineFmt("Tx Wire:", fmt.Sprintf("%d pkts (%s)", m.SentPkts, formatBytes(m.SentBytes))),
		lineFmt("Rx Wire:", fmt.Sprintf("%d pkts (%s)", m.RecvPkts, formatBytes(m.RecvBytes))),
		lineFmt("Rate:", rateStr),
		botBorder,
	}

	// Apply distinct HUD styling with DimGray and Blue accents
	styled := make([]string, len(rawLines))
	for i, l := range rawLines {
		styled[i] = palette.DimGrayOpen + l + "\x1b[0m"
	}
	return styled
}

// OverlayOnRows blends the HUD box into the top-right corner of screen rows.
func (h *HUD) OverlayOnRows(screenRows []string, screenWidth int) []string {
	if h == nil || !h.IsVisible() || screenWidth < 50 || len(screenRows) == 0 {
		return screenRows
	}

	boxW := 38
	boxLines := h.RenderBox(boxW)
	if len(boxLines) == 0 {
		return screenRows
	}

	out := make([]string, len(screenRows))
	copy(out, screenRows)

	startCol := screenWidth - boxW - 2
	if startCol < 0 {
		startCol = 0
	}

	startRow := 1 // Top margin
	for i, bl := range boxLines {
		rowIdx := startRow + i
		if rowIdx >= len(out) {
			break
		}
		orig := out[rowIdx]
		origWidth := ansi.StringWidth(orig)
		if origWidth < startCol {
			orig = orig + strings.Repeat(" ", startCol-origWidth)
		}

		// Cut base row before startCol and splice the HUD line
		left := ansi.Truncate(orig, startCol, "")
		leftWidth := ansi.StringWidth(left)
		if leftWidth < startCol {
			left += strings.Repeat(" ", startCol-leftWidth)
		}
		out[rowIdx] = left + bl
	}

	return out
}
