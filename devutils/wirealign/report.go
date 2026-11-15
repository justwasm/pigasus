package main

import (
	"fmt"
	"html"
	"os"
	"strings"
	"time"
)

// ANSI terminal color codes
const (
	ansiReset  = "\x1b[0m"
	ansiBold   = "\x1b[1m"
	ansiGreen  = "\x1b[32m"
	ansiRed    = "\x1b[31m"
	ansiYellow = "\x1b[33m"
	ansiCyan   = "\x1b[36m"
	ansiDim    = "\x1b[2m"
)

// RenderTerminal produces a colored ANSI summary for CLI output.
func RenderTerminal(res *WireDiffResult) string {
	var sb strings.Builder

	sb.WriteString("\n")
	sb.WriteString(ansiBold + ansiCyan + "================================================================================" + ansiReset + "\n")
	sb.WriteString(ansiBold + "  WIREALIGN PARITY REPORT: Reference (AGY 1.5.9) vs Target (Pigasus)" + ansiReset + "\n")
	sb.WriteString(ansiBold + ansiCyan + "================================================================================" + ansiReset + "\n")

	// Overall status badge
	if res.Passed {
		sb.WriteString(fmt.Sprintf("  Status: %s%s✔ ALL WIRE CHECKS PASSED%s (%d/%d checks passed, %d warnings)\n\n",
			ansiBold, ansiGreen, ansiReset, res.PassedChecks, res.TotalChecks, res.WarningsCount))
	} else {
		sb.WriteString(fmt.Sprintf("  Status: %s%s✗ PARITY DRIFT DETECTED%s (%d passed, %d failed, %d warnings)\n\n",
			ansiBold, ansiRed, ansiReset, res.PassedChecks, res.FailedChecks, res.WarningsCount))
	}

	// 1. Prompt Structure Summary
	sb.WriteString(ansiBold + "1. Prompt Structure & Sections:" + ansiReset + "\n")
	if res.PromptDiff != nil {
		if res.PromptDiff.OrderMatched {
			sb.WriteString(fmt.Sprintf("   %s✔%s Section Order: [%s]\n", ansiGreen, ansiReset, strings.Join(res.PromptDiff.RefSections, ", ")))
		} else {
			sb.WriteString(fmt.Sprintf("   %s✗%s Section Order Mismatch!\n", ansiRed, ansiReset))
			sb.WriteString(fmt.Sprintf("     Ref: [%s]\n", strings.Join(res.PromptDiff.RefSections, ", ")))
			sb.WriteString(fmt.Sprintf("     Tgt: [%s]\n", strings.Join(res.PromptDiff.TgtSections, ", ")))
		}

		for _, sd := range res.PromptDiff.SectionDiffs {
			mark := ansiGreen + "✔" + ansiReset
			if !sd.Matched {
				mark = ansiRed + "✗" + ansiReset
				sb.WriteString(fmt.Sprintf("   %s <%s> Diff (ref: %d B, tgt: %d B)\n", mark, sd.Tag, sd.RefBytes, sd.TgtBytes))
				if sd.RefSnippet != "" {
					sb.WriteString(fmt.Sprintf("       Ref: %s\n", sd.RefSnippet))
					sb.WriteString(fmt.Sprintf("       Tgt: %s\n", sd.TgtSnippet))
				}
			} else {
				sb.WriteString(fmt.Sprintf("   %s <%s> (%d B)\n", mark, sd.Tag, sd.RefBytes))
			}
		}
	}

	// 2. Tools Declarations Summary
	sb.WriteString("\n" + ansiBold + "2. Tools Surface (Function Declarations):" + ansiReset + "\n")
	if res.ToolsDiff != nil {
		if res.ToolsDiff.Matched {
			sb.WriteString(fmt.Sprintf("   %s✔%s All %d tools matched in name, parameters and descriptions\n",
				ansiGreen, ansiReset, res.ToolsDiff.RefCount))
		} else {
			sb.WriteString(fmt.Sprintf("   %s✗%s Tool discrepancies found (ref: %d, tgt: %d)\n",
				ansiRed, ansiReset, res.ToolsDiff.RefCount, res.ToolsDiff.TgtCount))
			if len(res.ToolsDiff.MissingTools) > 0 {
				sb.WriteString(fmt.Sprintf("     Missing tools: %v\n", res.ToolsDiff.MissingTools))
			}
			if len(res.ToolsDiff.ExtraTools) > 0 {
				sb.WriteString(fmt.Sprintf("     Extra tools: %v\n", res.ToolsDiff.ExtraTools))
			}
			for _, td := range res.ToolsDiff.ToolDiffs {
				if !td.Matched {
					sb.WriteString(fmt.Sprintf("     %s %s: desc_matched=%v, param_matched=%v\n",
						ansiRed+"✗"+ansiReset, td.Name, td.DescMatched, td.ParamMatched))
					for _, pd := range td.ParamDiffs {
						sb.WriteString(fmt.Sprintf("       - %s\n", pd))
					}
				}
			}
		}
	}

	// 3. Diagnostics
	if len(res.Diagnostics) > 0 {
		sb.WriteString("\n" + ansiBold + "3. Semantic Diagnostics & Observations:" + ansiReset + "\n")
		for _, d := range res.Diagnostics {
			badge := ansiRed + "[ERROR]" + ansiReset
			if d.Severity == SeverityWarning {
				badge = ansiYellow + "[WARN ]" + ansiReset
			} else if d.Severity == SeverityInfo {
				badge = ansiCyan + "[INFO ]" + ansiReset
			}
			sb.WriteString(fmt.Sprintf("   %s %s: %s\n", badge, ansiBold+d.Code+ansiReset, d.Message))
			if d.Detail != "" {
				sb.WriteString(fmt.Sprintf("       %s%s%s\n", ansiDim, strings.ReplaceAll(d.Detail, "\n", "\n       "), ansiReset))
			}
		}
	}

	sb.WriteString(ansiBold + ansiCyan + "================================================================================" + ansiReset + "\n")
	return sb.String()
}

// RenderHTML generates a standalone interactive HTML parity report.
func RenderHTML(res *WireDiffResult, title string) string {
	if title == "" {
		title = "WireAlign Parity Report"
	}

	statusColor := "#34a853" // Green
	statusText := "ALL WIRE CHECKS PASSED"
	if !res.Passed {
		statusColor = "#ea4335" // Red
		statusText = "PARITY DRIFT DETECTED"
	}

	var sectionsHTML strings.Builder
	if res.PromptDiff != nil {
		for _, sd := range res.PromptDiff.SectionDiffs {
			badge := `<span style="background:#e6f4ea;color:#137333;padding:2px 8px;border-radius:4px;font-size:12px;">MATCH</span>`
			if !sd.Matched {
				badge = `<span style="background:#fce8e6;color:#c5221f;padding:2px 8px;border-radius:4px;font-size:12px;">DIFF</span>`
			}
			sectionsHTML.WriteString(fmt.Sprintf(`
				<tr>
					<td><strong>&lt;%s&gt;</strong></td>
					<td>%s</td>
					<td>%d B</td>
					<td>%d B</td>
				</tr>
			`, html.EscapeString(sd.Tag), badge, sd.RefBytes, sd.TgtBytes))
		}
	}

	var toolsHTML strings.Builder
	if res.ToolsDiff != nil {
		for _, td := range res.ToolsDiff.ToolDiffs {
			badge := `<span style="background:#e6f4ea;color:#137333;padding:2px 8px;border-radius:4px;font-size:12px;">MATCH</span>`
			if !td.Matched {
				badge = `<span style="background:#fce8e6;color:#c5221f;padding:2px 8px;border-radius:4px;font-size:12px;">DIFF</span>`
			}
			toolsHTML.WriteString(fmt.Sprintf(`
				<tr>
					<td><code>%s</code></td>
					<td>%s</td>
					<td>%v</td>
					<td>%v</td>
				</tr>
			`, html.EscapeString(td.Name), badge, td.DescMatched, td.ParamMatched))
		}
	}

	var diagHTML strings.Builder
	for _, d := range res.Diagnostics {
		bgColor := "#fce8e6"
		textColor := "#c5221f"
		if d.Severity == SeverityWarning {
			bgColor = "#fef7e0"
			textColor = "#b06000"
		}
		diagHTML.WriteString(fmt.Sprintf(`
			<div style="margin-bottom:12px;padding:12px;border-radius:6px;background:%s;border-left:4px solid %s;">
				<div style="font-weight:bold;color:%s;">[%s] %s: %s</div>
				<pre style="margin:6px 0 0 0;font-size:12px;white-space:pre-wrap;color:#3c4043;">%s</pre>
			</div>
		`, bgColor, textColor, textColor, d.Severity, html.EscapeString(d.Code), html.EscapeString(d.Message), html.EscapeString(d.Detail)))
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<meta charset="utf-8">
	<title>%s</title>
	<style>
		body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; margin: 0; padding: 24px; background: #f8f9fa; color: #202124; }
		.container { max-width: 1000px; margin: 0 auto; background: #fff; padding: 32px; border-radius: 8px; box-shadow: 0 1px 3px rgba(60,64,67,.3); }
		h1 { margin-top: 0; font-size: 24px; }
		.status-banner { padding: 14px 20px; border-radius: 6px; color: #fff; font-weight: bold; margin-bottom: 24px; background: %s; }
		table { width: 100%%; border-collapse: collapse; margin: 16px 0 24px 0; font-size: 14px; }
		th, td { text-align: left; padding: 10px 12px; border-bottom: 1px solid #dadce0; }
		th { background: #f1f3f4; }
		code { background: #f1f3f4; padding: 2px 6px; border-radius: 4px; font-family: ui-monospace, Menlo, Monaco, monospace; font-size: 13px; }
		.card { background: #fff; border: 1px solid #dadce0; border-radius: 6px; padding: 16px; margin-bottom: 24px; }
	</style>
</head>
<body>
	<div class="container">
		<h1>WireAlign Parity Dashboard</h1>
		<div style="color:#5f6368;font-size:13px;margin-bottom:16px;">Generated: %s | Reference: AGY 1.5.9 vs Target: Pigasus</div>
		
		<div class="status-banner">
			%s — %d/%d checks passed, %d warnings
		</div>

		<div class="card">
			<h3>1. Prompt Section Alignment</h3>
			<table>
				<thead>
					<tr>
						<th>Section Tag</th>
						<th>Status</th>
						<th>Reference Bytes</th>
						<th>Target Bytes</th>
					</tr>
				</thead>
				<tbody>
					%s
				</tbody>
			</table>
		</div>

		<div class="card">
			<h3>2. Tools Surface (12 Function Declarations)</h3>
			<table>
				<thead>
					<tr>
						<th>Tool Name</th>
						<th>Status</th>
						<th>Description Match</th>
						<th>Parameters Match</th>
					</tr>
				</thead>
				<tbody>
					%s
				</tbody>
			</table>
		</div>

		<div class="card">
			<h3>3. Diagnostics & Detailed Observations</h3>
			%s
		</div>
	</div>
</body>
</html>`,
		html.EscapeString(title),
		statusColor,
		time.Now().Format("2006-01-02 15:04:05"),
		statusText, res.PassedChecks, res.TotalChecks, res.WarningsCount,
		sectionsHTML.String(),
		toolsHTML.String(),
		diagHTML.String(),
	)
}

// WriteHTMLReport writes the HTML report to disk.
func WriteHTMLReport(res *WireDiffResult, outPath string) error {
	content := RenderHTML(res, "Wire Parity Report")
	return os.WriteFile(outPath, []byte(content), 0644)
}
