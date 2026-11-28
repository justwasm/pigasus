// summaryview.go — the /debug surface's Summary view.
//
// # What it answers
//
// The other five views drill into specific diagnostic subsystems (logs, wire,
// trajectory, requests, context). This view brings together the session's
// active model, context occupancy, network exchange health, errors, and
// compactions into a single cohesive overview.
package debugpanel

import (
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/core/wirelog"
	"github.com/justwasm/pigasus/palette"
)

// SummaryHeaderLines renders the view's pinned header.
func SummaryHeaderLines(ctxSt ContextState, reqSt RequestState) []string {
	failed := 0
	for _, r := range reqSt.Records {
		if r.Err != "" || r.Status >= 400 {
			failed++
		}
	}
	head := "\x1b[1m" + palette.SubtleOpen + "└ Summary" + "\x1b[0m"
	if failed > 0 {
		head += palette.DimGrayOpen + "  ·  " + logErrorOpen + fmt.Sprintf("Health: %d failed exchange(s)", failed) + logColourClose
	} else {
		head += palette.DimGrayOpen + "  ·  " + logSuccessOpen + "Health: OK" + logColourClose
	}
	if ctxSt.Model != "" {
		head += palette.DimGrayOpen + fmt.Sprintf("  ·  %s", ctxSt.Model)
	}
	head += palette.DimGrayClose

	sub := "   " + palette.DimGrayOpen +
		"Session overview  ·  press enter on any section to jump directly to its view" +
		palette.DimGrayClose
	return []string{head, sub}
}

// SummaryLegend is the view's key hint.
func SummaryLegend() string {
	return "enter jump to view  ·  1-6 switch view  ·  esc close  ·  tab next view"
}

// SummaryRows renders the dashboard rows.
func SummaryRows(ctxSt ContextState, reqSt RequestState, width int) []FoldRow {
	if width <= 0 {
		width = 100
	}
	var rows []FoldRow

	addSection := func(title string) {
		rows = append(rows, FoldRow{
			ID:   "section/" + title,
			Text: "\x1b[1m" + palette.SubtleOpen + title + "\x1b[0m",
			Flat: true,
		})
	}

	addRow := func(id, label, value, hint string) {
		colLabel := padRight(label, 16)
		colHint := ""
		if hint != "" {
			colHint = palette.DimGrayOpen + " (" + hint + ")" + palette.DimGrayClose
		}
		text := "  " + palette.DimGrayOpen + "▸ " + palette.DimGrayClose +
			palette.SubtleOpen + colLabel + palette.DimGrayClose + " " +
			value + colHint
		rows = append(rows, FoldRow{
			ID:   id,
			Text: text,
			Flat: true,
		})
	}

	// 1. Model & Endpoint
	addSection("Active Model & Routing")
	modelName := ctxSt.Model
	if modelName == "" {
		modelName = "—"
	}
	addRow("jump/context", "model", modelName, "enter opens Context")

	hosts := requestHosts(reqSt.Records)
	activeHost := "—"
	if len(hosts) > 0 {
		activeHost = strings.Join(hosts, ", ")
	}
	addRow("jump/requests", "endpoint", activeHost, "enter opens Requests")

	if ctxSt.WindowTokens > 0 {
		addRow("jump/context", "context window", contextTokens(ctxSt.WindowTokens), "")
	}

	// 2. Context Window Occupancy
	addSection("Context Occupancy & Tokens")
	if ctxSt.WindowTokens > 0 && ctxSt.LastPromptTokens > 0 {
		pct := float64(ctxSt.LastPromptTokens) / float64(ctxSt.WindowTokens) * 100
		barWidth := 20
		filled := int(pct / 100 * float64(barWidth))
		if filled > barWidth {
			filled = barWidth
		}
		if filled < 0 {
			filled = 0
		}
		bar := strings.Repeat("█", filled) + strings.Repeat("░", barWidth-filled)
		barColor := palette.SubtleOpen
		if pct >= 80 {
			barColor = logErrorOpen
		} else if pct >= 60 {
			barColor = logRetryOpen
		}
		occVal := fmt.Sprintf("%s%s%s  %s / %s (%.1f%%)",
			barColor, bar, logColourClose,
			contextTokens(ctxSt.LastPromptTokens), contextTokens(ctxSt.WindowTokens), pct)
		addRow("jump/context", "occupancy", occVal, "")
	} else if ctxSt.LastPromptTokens > 0 {
		addRow("jump/context", "prompt tokens", contextTokens(ctxSt.LastPromptTokens), "")
	} else {
		addRow("jump/context", "occupancy", "— (no turns recorded yet)", "")
	}

	if ctxSt.LastPromptTokens > 0 || ctxSt.LastCompletionTokens > 0 {
		lastTurn := fmt.Sprintf("%s in · %s out",
			contextTokens(ctxSt.LastPromptTokens), contextTokens(ctxSt.LastCompletionTokens))
		addRow("jump/context", "last turn", lastTurn, "")
	}

	// 3. Network & HTTP Exchanges
	addSection("Network & HTTP Exchanges")
	recs := reqSt.Records
	failedCount, slowCount := 0, 0
	var totalDur time.Duration
	var totalSent, totalRecv int64
	for _, r := range recs {
		if r.Err != "" || r.Status >= 400 {
			failedCount++
		}
		if r.TTFB > slowThreshold {
			slowCount++
		}
		totalDur += r.Duration
		totalSent += r.RequestBytes
		totalRecv += r.ResponseBytes
	}

	// "0 failed" is only good news once something has been tried. On a
	// session that has not sent a request, printing a green zero (or the
	// "all systems nominal" below) reports a healthy network from an empty
	// sample — the one claim this screen must never make, because the
	// reader opens it precisely when they suspect something is wrong.
	exVal := fmt.Sprintf("%d total", len(recs))
	if len(recs) == 0 {
		exVal = "— none sent yet"
	} else if failedCount > 0 {
		exVal += fmt.Sprintf(" · %s%d failed%s", logErrorOpen, failedCount, logColourClose)
	} else {
		exVal += fmt.Sprintf(" · %s0 failed%s", logSuccessOpen, logColourClose)
	}
	if slowCount > 0 {
		exVal += fmt.Sprintf(" · %s%d slow%s", logRetryOpen, slowCount, logColourClose)
	}
	addRow("jump/requests", "exchanges", exVal, "enter opens Requests")

	if len(recs) > 0 {
		avgDur := totalDur / time.Duration(len(recs))
		addRow("jump/requests", "avg latency", formatDuration(avgDur), "")
		bytesStr := fmt.Sprintf("%s sent · %s received", HumanBytes64(totalSent), HumanBytes64(totalRecv))
		addRow("jump/requests", "wire traffic", bytesStr, "summed over every exchange")
	}

	// 4. Compactions
	addSection("Compaction History")
	if len(ctxSt.Compactions) == 0 {
		// "0 compactions" reads as a settled fact about the conversation;
		// what the recorder knows is that it saw none. The Context view
		// says "none recorded", and the two must not disagree.
		compactVal := "none recorded in this session"
		if ctxSt.AutoCompactTokens > 0 {
			compactVal += fmt.Sprintf(" (threshold: %s)", contextTokens(ctxSt.AutoCompactTokens))
		}
		addRow("jump/context", "compactions", compactVal, "")
	} else {
		savedTotal := 0
		for _, c := range ctxSt.Compactions {
			savedTotal += c.SavedTokens()
		}
		compactVal := fmt.Sprintf("%d compaction(s) · %s tokens saved total",
			len(ctxSt.Compactions), contextTokens(savedTotal))
		addRow("jump/context", "compactions", compactVal, "enter opens Context")
	}

	// 5. Recent Issues & Diagnostics
	addSection("Recent Diagnostics")
	var lastErr *wirelog.Record
	for i := len(recs) - 1; i >= 0; i-- {
		if recs[i].Err != "" || recs[i].Status >= 400 {
			lastErr = &recs[i]
			break
		}
	}
	if lastErr != nil {
		errDetail := fmt.Sprintf("%s%d %s%s on %s",
			logErrorOpen, lastErr.Status, httpStatusText(lastErr.Status), logColourClose, lastErr.Path)
		if lastErr.Err != "" {
			errDetail = fmt.Sprintf("%sERR: %s%s", logErrorOpen, lastErr.Err, logColourClose)
		}
		addRow("jump/requests", "last error", errDetail, "enter inspects request")
	} else if len(recs) == 0 {
		// Nothing has been sent, so nothing is known about the network.
		addRow("jump/logs", "status", "no exchanges yet — nothing to judge", "enter opens Logs")
	} else {
		addRow("jump/logs", "status", logSuccessOpen+"no failed exchange in this session"+logColourClose, "enter opens Logs")
	}

	return rows
}
