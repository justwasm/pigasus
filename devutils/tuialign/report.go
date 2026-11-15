package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"
)

// SnapshotItem models one visual parity snapshot ready for master report aggregation.
type SnapshotItem struct {
	ID           string
	Label        string
	ScenarioName string
	ParityScore  float64
	MatchLines   int
	TotalLines   int
	DiffLines    int
	ColorDiffs   int
	Identical    bool
	TableBody    string
	TextReport   string
}

var (
	htmlTitleRe = regexp.MustCompile(`<title>tuialign:\s*(.*?)</title>`)
	htmlTbodyRe = regexp.MustCompile(`(?s)<tbody>(.*?)</tbody>`)
)

// AggregateHTMLFiles scans and parses multiple standalone HTML snapshots into SnapshotItems.
func AggregateHTMLFiles(filePaths []string) ([]SnapshotItem, error) {
	var items []SnapshotItem
	for _, p := range filePaths {
		content, err := os.ReadFile(p)
		if err != nil {
			continue
		}

		label := strings.TrimSuffix(strings.TrimPrefix(filepath.Base(p), "tuialign_"), ".html")
		if tMatches := htmlTitleRe.FindSubmatch(content); len(tMatches) > 1 {
			label = strings.TrimSpace(string(tMatches[1]))
		}

		bMatches := htmlTbodyRe.FindSubmatch(content)
		if len(bMatches) < 2 {
			continue
		}
		tbody := string(bMatches[1])

		matches := strings.Count(tbody, `<tr class="match">`)
		diffs := strings.Count(tbody, `<tr class="text-diff">`)
		colors := strings.Count(tbody, `<tr class="color-diff">`)
		total := matches + diffs + colors

		parity := 0.0
		if total > 0 {
			parity = float64(matches) / float64(total) * 100.0
		}

		// Try to read corresponding .txt file if present
		txtPath := strings.TrimSuffix(p, ".html") + ".txt"
		textReport := ""
		if tBytes, err := os.ReadFile(txtPath); err == nil {
			textReport = string(tBytes)
		}

		cleanID := sanitizeID(label)
		items = append(items, SnapshotItem{
			ID:           cleanID,
			Label:        label,
			ScenarioName: inferScenarioName(label),
			ParityScore:  parity,
			MatchLines:   matches,
			TotalLines:   total,
			DiffLines:    diffs,
			ColorDiffs:   colors,
			Identical:    diffs == 0 && colors == 0,
			TableBody:    tbody,
			TextReport:   textReport,
		})
	}

	sort.SliceStable(items, func(i, j int) bool {
		return items[i].Label < items[j].Label
	})

	return items, nil
}

func sanitizeID(s string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(s) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			b.WriteRune(r)
		} else {
			b.WriteRune('_')
		}
	}
	res := b.String()
	if res == "" {
		return "snap"
	}
	return res
}

func inferScenarioName(label string) string {
	parts := strings.SplitN(label, "_", 2)
	if len(parts) == 2 {
		return strings.ReplaceAll(parts[1], "_", " ")
	}
	return strings.ReplaceAll(label, "_", " ")
}

// RenderMasterHTMLReport compiles all snapshot items into a single, cohesive HTML file.
func RenderMasterHTMLReport(title string, items []SnapshotItem) string {
	totalSnapshots := len(items)
	perfectMatches := 0
	totalParity := 0.0
	totalDiffs := 0
	totalColorDiffs := 0

	for _, item := range items {
		if item.Identical {
			perfectMatches++
		}
		totalParity += item.ParityScore
		totalDiffs += item.DiffLines
		totalColorDiffs += item.ColorDiffs
	}

	avgParity := 0.0
	if totalSnapshots > 0 {
		avgParity = totalParity / float64(totalSnapshots)
	}

	// Sidebar items & snapshot sections
	var navItems strings.Builder
	var sections strings.Builder

	for idx, item := range items {
		statusClass := "match"
		badge := "PASS"
		pillClass := "pill-green"
		if !item.Identical {
			if item.DiffLines > 0 {
				statusClass = "text-diff"
				badge = "DIFF"
				pillClass = "pill-red"
			} else {
				statusClass = "color-diff"
				badge = "WARN"
				pillClass = "pill-yellow"
			}
		}

		activeClass := ""
		if idx == 0 {
			activeClass = "active"
		}

		navItems.WriteString(fmt.Sprintf(`
        <li class="nav-item %s %s" data-id="%s" onclick="selectSnapshot('%s')">
          <div class="nav-label">%s</div>
          <div class="nav-meta">
            <span class="pill %s">%.1f%%</span>
            <span class="nav-badge %s">%s</span>
          </div>
        </li>`, statusClass, activeClass, item.ID, item.ID, item.Label, pillClass, item.ParityScore, badge, badge))

		// Render text diff details section if available
		rawTextSection := ""
		if item.TextReport != "" {
			rawTextSection = fmt.Sprintf(`
        <details class="raw-details">
          <summary>View Terminal Raw ASCII Diff</summary>
          <pre class="raw-pre">%s</pre>
        </details>`, escapeHTML(item.TextReport))
		}

		cardActiveClass := ""
		if idx == 0 {
			cardActiveClass = "active"
		}

		sections.WriteString(fmt.Sprintf(`
      <article class="snapshot-card %s %s" id="%s" data-label="%s" data-identical="%t">
        <header class="card-header">
          <div class="card-title-group">
            <span class="card-idx">#%d of %d</span>
            <h2 class="card-title">%s</h2>
          </div>
          <div class="card-stats">
            <div class="stat-pill"><span class="lbl">Parity:</span> <strong class="%s">%.1f%%</strong></div>
            <div class="stat-pill"><span class="lbl">Matches:</span> <strong>%d / %d</strong></div>
            <div class="stat-pill"><span class="lbl">Text diffs:</span> <strong class="diff-text-val">%d</strong></div>
            <div class="stat-pill"><span class="lbl">Color diffs:</span> <strong class="diff-color-val">%d</strong></div>
            <span class="card-status-badge %s">%s</span>
          </div>
        </header>

        <div class="table-container">
          <table class="diff-table">
            <thead>
              <tr>
                <th class="ln">#</th>
                <th class="badge">Status</th>
                <th>Reference (agy 1.5.8)</th>
                <th>Target (pigasus)</th>
              </tr>
            </thead>
            <tbody>
              <tr class="no-diff-msg">
                <td colspan="4">✓ 100%% Exact Match &mdash; Every single line and color matches reference perfectly!</td>
              </tr>
              %s
            </tbody>
          </table>
        </div>
        %s
      </article>`, statusClass, cardActiveClass, item.ID, item.Label, item.Identical, idx+1, totalSnapshots, item.Label,
			pillClass, item.ParityScore, item.MatchLines, item.TotalLines, item.DiffLines, item.ColorDiffs,
			badge, badge, item.TableBody, rawTextSection))
	}

	avgColorClass := "pill-green"
	if avgParity < 80.0 {
		avgColorClass = "pill-red"
	} else if avgParity < 95.0 {
		avgColorClass = "pill-yellow"
	}

	return fmt.Sprintf(`<!DOCTYPE html>
<html lang="en">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width, initial-scale=1.0">
<title>%s</title>
<style>
  :root {
    --bg-dark: #141416;
    --bg-panel: #1e1e22;
    --bg-card: #25252a;
    --border-color: #34343a;
    --text-main: #e0e0e4;
    --text-muted: #888890;
    --accent-blue: #61afef;
    --accent-green: #98c379;
    --accent-yellow: #e5c07b;
    --accent-red: #e06c75;
    --font-mono: ui-monospace, SFMono-Regular, Menlo, Monaco, Consolas, "Liberation Mono", "Courier New", monospace;
  }
  * { box-sizing: border-box; margin: 0; padding: 0; }
  body {
    background-color: var(--bg-dark);
    color: var(--text-main);
    font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, Helvetica, Arial, sans-serif;
    display: flex;
    flex-direction: column;
    height: 100vh;
    overflow: hidden;
  }
  header.top-bar {
    background-color: var(--bg-panel);
    border-bottom: 1px solid var(--border-color);
    padding: 10px 20px;
    display: flex;
    justify-content: space-between;
    align-items: center;
    flex-shrink: 0;
  }
  .title-area h1 {
    font-size: 1.2rem;
    color: var(--accent-blue);
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .title-area p {
    font-size: 0.78rem;
    color: var(--text-muted);
    margin-top: 2px;
  }
  .metrics-strip {
    display: flex;
    gap: 10px;
  }
  .metric-card {
    background-color: var(--bg-card);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    padding: 5px 12px;
    text-align: center;
    min-width: 90px;
  }
  .metric-card .val {
    font-size: 1.1rem;
    font-weight: 700;
  }
  .metric-card .lbl {
    font-size: 0.65rem;
    color: var(--text-muted);
    text-transform: uppercase;
    letter-spacing: 0.5px;
    margin-top: 1px;
  }

  /* Main Workspace Layout */
  .workspace {
    display: flex;
    flex: 1;
    overflow: hidden;
  }

  /* Sidebar */
  aside.sidebar {
    width: 320px;
    background-color: var(--bg-panel);
    border-right: 1px solid var(--border-color);
    display: flex;
    flex-direction: column;
    flex-shrink: 0;
  }
  .sidebar-controls {
    padding: 12px;
    border-bottom: 1px solid var(--border-color);
    display: flex;
    flex-direction: column;
    gap: 8px;
  }
  .search-input {
    width: 100%%;
    background: var(--bg-card);
    border: 1px solid var(--border-color);
    color: var(--text-main);
    padding: 6px 10px;
    border-radius: 4px;
    font-size: 0.82rem;
  }
  .search-input:focus {
    outline: 1px solid var(--accent-blue);
    border-color: var(--accent-blue);
  }
  .filter-tabs {
    display: flex;
    gap: 4px;
  }
  .filter-btn {
    flex: 1;
    background: transparent;
    border: 1px solid var(--border-color);
    color: var(--text-muted);
    padding: 4px 6px;
    border-radius: 4px;
    font-size: 0.75rem;
    cursor: pointer;
    text-align: center;
    transition: all 0.15s ease;
  }
  .filter-btn.active, .filter-btn:hover {
    background: var(--bg-card);
    color: var(--text-main);
    border-color: var(--accent-blue);
  }
  .sidebar-nav {
    flex: 1;
    overflow-y: auto;
    list-style: none;
    padding: 8px;
  }
  .nav-item {
    padding: 8px 10px;
    border-radius: 5px;
    cursor: pointer;
    margin-bottom: 4px;
    display: flex;
    flex-direction: column;
    gap: 4px;
    transition: background 0.1s;
    border-left: 3px solid transparent;
  }
  .nav-item:hover {
    background-color: var(--bg-card);
  }
  .nav-item.active {
    background-color: #2b2b32 !important;
    border-left-color: var(--accent-blue) !important;
  }
  .nav-item.text-diff { border-left-color: var(--accent-red); }
  .nav-item.color-diff { border-left-color: var(--accent-yellow); }
  .nav-item.match { border-left-color: var(--accent-green); }
  .nav-label {
    font-size: 0.82rem;
    font-weight: 500;
    white-space: nowrap;
    overflow: hidden;
    text-overflow: ellipsis;
    font-family: var(--font-mono);
  }
  .nav-meta {
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .pill {
    font-size: 0.7rem;
    font-weight: bold;
    padding: 1px 6px;
    border-radius: 10px;
  }
  .pill-green { color: var(--accent-green); background: rgba(152, 195, 121, 0.12); }
  .pill-yellow { color: var(--accent-yellow); background: rgba(229, 192, 123, 0.12); }
  .pill-red { color: var(--accent-red); background: rgba(224, 108, 117, 0.12); }
  .nav-badge {
    font-size: 0.65rem;
    font-weight: 700;
  }
  .nav-badge.PASS { color: var(--accent-green); }
  .nav-badge.WARN { color: var(--accent-yellow); }
  .nav-badge.DIFF { color: var(--accent-red); }

  /* Content View Area */
  main.content-view {
    flex: 1;
    overflow-y: auto;
    padding: 16px 24px;
    display: flex;
    flex-direction: column;
    gap: 16px;
  }
  .toolbar-strip {
    background-color: var(--bg-panel);
    border: 1px solid var(--border-color);
    border-radius: 6px;
    padding: 8px 14px;
    display: flex;
    justify-content: space-between;
    align-items: center;
    position: sticky;
    top: 0;
    z-index: 100;
  }
  .toolbar-left {
    display: flex;
    align-items: center;
    gap: 14px;
    font-size: 0.82rem;
  }
  .toolbar-right {
    display: flex;
    align-items: center;
    gap: 8px;
  }
  .btn-action {
    background: var(--bg-card);
    border: 1px solid var(--border-color);
    color: var(--text-main);
    padding: 4px 10px;
    border-radius: 4px;
    font-size: 0.75rem;
    cursor: pointer;
    font-family: inherit;
    transition: all 0.15s;
  }
  .btn-action:hover {
    background: #303036;
    border-color: var(--accent-blue);
  }
  .btn-action.active {
    background: var(--accent-blue);
    color: #121214;
    font-weight: bold;
  }

  /* Snapshot Card */
  .snapshot-card {
    background-color: var(--bg-panel);
    border: 1px solid var(--border-color);
    border-radius: 8px;
    overflow: hidden;
  }
  body.mode-single .snapshot-card {
    display: none;
  }
  body.mode-single .snapshot-card.active {
    display: block !important;
  }

  .card-header {
    background-color: var(--bg-card);
    border-bottom: 1px solid var(--border-color);
    padding: 10px 16px;
    display: flex;
    justify-content: space-between;
    align-items: center;
  }
  .card-title-group {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .card-idx {
    font-size: 0.78rem;
    color: var(--text-muted);
    font-family: var(--font-mono);
  }
  .card-title {
    font-size: 1.05rem;
    color: var(--text-main);
    font-family: var(--font-mono);
  }
  .card-stats {
    display: flex;
    align-items: center;
    gap: 10px;
  }
  .stat-pill {
    font-size: 0.78rem;
    background: var(--bg-panel);
    padding: 3px 8px;
    border-radius: 4px;
    border: 1px solid var(--border-color);
  }
  .stat-pill .lbl { color: var(--text-muted); }
  .diff-text-val { color: var(--accent-red); font-weight: bold; }
  .diff-color-val { color: var(--accent-yellow); font-weight: bold; }
  .card-status-badge {
    font-size: 0.75rem;
    font-weight: 700;
    padding: 3px 8px;
    border-radius: 4px;
  }
  .card-status-badge.PASS { background: rgba(152, 195, 121, 0.15); color: var(--accent-green); }
  .card-status-badge.WARN { background: rgba(229, 192, 123, 0.15); color: var(--accent-yellow); }
  .card-status-badge.DIFF { background: rgba(224, 108, 117, 0.15); color: var(--accent-red); }

  /* True Terminal Row Side-by-Side Comparison */
  .table-container {
    overflow-x: auto;
    background: #141416;
  }
  table.diff-table {
    border-collapse: collapse;
    width: 100%%;
    table-layout: auto;
    font-family: var(--font-mono);
  }
  table.diff-table th {
    background: #202025;
    color: #9cdcfe;
    text-align: left;
    padding: 8px 12px;
    border-bottom: 2px solid var(--border-color);
    font-size: 0.8rem;
    letter-spacing: 0.5px;
  }
  table.diff-table td {
    padding: 1px 8px;
    vertical-align: middle;
    font-size: 13px;
    line-height: 1.45;
    white-space: pre; /* Essential: terminal lines never wrap! */
    font-family: var(--font-mono);
  }
  td.ln {
    width: 40px;
    color: #555560;
    text-align: right;
    user-select: none;
    border-right: 1px solid var(--border-color);
    padding-right: 8px;
  }
  td.badge {
    width: 65px;
    font-weight: bold;
    font-size: 0.72rem;
    text-align: center;
    border-right: 1px solid var(--border-color);
  }
  tr.match td.badge { color: var(--accent-green); }
  tr.color-diff {
    background: rgba(229, 192, 123, 0.15) !important;
  }
  tr.color-diff td.badge { color: var(--accent-yellow); font-weight: bold; }
  tr.text-diff {
    background: rgba(224, 108, 117, 0.22) !important;
  }
  tr.text-diff td.badge { color: var(--accent-red); font-weight: bold; }
  td.pane {
    min-width: 450px;
    border-right: 1px solid var(--border-color);
  }
  td.pane:last-child {
    border-right: none;
  }

  /* Hide matching lines mode */
  body.hide-matches tr.match {
    display: none !important;
  }
  tr.no-diff-msg {
    display: none;
  }
  body.hide-matches .snapshot-card[data-identical="true"] tr.no-diff-msg {
    display: table-row !important;
  }
  tr.no-diff-msg td {
    text-align: center;
    padding: 24px;
    color: var(--accent-green);
    font-size: 0.9rem;
    font-style: italic;
    background: rgba(152, 195, 121, 0.05);
  }

  /* Raw ASCII Terminal Details */
  details.raw-details {
    padding: 8px 16px;
    background: #19191d;
    border-top: 1px solid var(--border-color);
  }
  details.raw-details summary {
    font-size: 0.78rem;
    color: var(--accent-blue);
    cursor: pointer;
    user-select: none;
  }
  pre.raw-pre {
    margin-top: 8px;
    padding: 10px;
    background: #0d0d0f;
    border: 1px solid var(--border-color);
    border-radius: 4px;
    font-family: var(--font-mono);
    font-size: 0.75rem;
    color: #abb2bf;
    overflow-x: auto;
    white-space: pre-wrap;
  }
</style>
</head>
<body class="mode-single">

<header class="top-bar">
  <div class="title-area">
    <h1>TUIAlign Visual Parity Master Report</h1>
    <p>Google Antigravity (v1.5.8 reference) vs Pigasus &bull; Generated %s</p>
  </div>
  <div class="metrics-strip">
    <div class="metric-card">
      <div class="val" style="color: var(--accent-blue);">%d</div>
      <div class="lbl">Total Snapshots</div>
    </div>
    <div class="metric-card">
      <div class="val" style="color: var(--accent-green);">%d / %d</div>
      <div class="lbl">100%% Parity</div>
    </div>
    <div class="metric-card">
      <div class="val %s">%.1f%%</div>
      <div class="lbl">Average Parity</div>
    </div>
    <div class="metric-card">
      <div class="val" style="color: var(--accent-red);">%d</div>
      <div class="lbl">Text Diffs</div>
    </div>
    <div class="metric-card">
      <div class="val" style="color: var(--accent-yellow);">%d</div>
      <div class="lbl">Color Diffs</div>
    </div>
  </div>
</header>

<div class="workspace">
  <aside class="sidebar">
    <div class="sidebar-controls">
      <input type="text" class="search-input" id="search-box" placeholder="Filter snapshot name..." oninput="onSearchInput(this.value)">
      <div class="filter-tabs">
        <button class="filter-btn active" onclick="setFilter('all', this)">All (%d)</button>
        <button class="filter-btn" onclick="setFilter('diff', this)">Diffs (%d)</button>
        <button class="filter-btn" onclick="setFilter('match', this)">100%% (%d)</button>
      </div>
    </div>
    <ul class="sidebar-nav" id="sidebar-nav">
      %s
    </ul>
  </aside>

  <main class="content-view" id="content-view">
    <div class="toolbar-strip">
      <div class="toolbar-left">
        <label style="cursor: pointer; display: flex; align-items: center; gap: 6px;">
          <input type="checkbox" id="toggle-hide-matches" onchange="toggleHideMatches(this.checked)">
          <span><strong>Filter to Diff rows only</strong> (collapses identical rows)</span>
        </label>
      </div>
      <div class="toolbar-right">
        <button class="btn-action" onclick="selectPrev()">← Prev (k)</button>
        <button class="btn-action" onclick="selectNext()">Next (j) →</button>
        <button class="btn-action" id="btn-view-mode" onclick="toggleViewMode()">View: All Continuous</button>
      </div>
    </div>

    %s
  </main>
</div>

<script>
let currentFilter = 'all';
let viewMode = 'single'; // 'single' or 'continuous'
let activeSnapshotId = '';

function toggleHideMatches(checked) {
  if (checked) {
    document.body.classList.add('hide-matches');
  } else {
    document.body.classList.remove('hide-matches');
  }
}

function toggleViewMode() {
  const btn = document.getElementById('btn-view-mode');
  if (viewMode === 'single') {
    viewMode = 'continuous';
    document.body.classList.remove('mode-single');
    btn.innerText = 'View: Single Focused';
  } else {
    viewMode = 'single';
    document.body.classList.add('mode-single');
    btn.innerText = 'View: All Continuous';
    if (activeSnapshotId) {
      selectSnapshot(activeSnapshotId);
    }
  }
}

function selectSnapshot(id) {
  activeSnapshotId = id;

  // Update sidebar active class
  document.querySelectorAll('.nav-item').forEach(n => {
    if (n.getAttribute('data-id') === id) {
      n.classList.add('active');
      n.scrollIntoView({ behavior: 'smooth', block: 'nearest' });
    } else {
      n.classList.remove('active');
    }
  });

  // Update card active class
  document.querySelectorAll('.snapshot-card').forEach(card => {
    if (card.id === id) {
      card.classList.add('active');
      card.scrollIntoView({ behavior: 'smooth', block: 'start' });
    } else {
      card.classList.remove('active');
    }
  });

  // Update URL hash without jumping
  history.replaceState(null, '', '#' + id);
}

function getVisibleSnapshotIds() {
  const visibleItems = Array.from(document.querySelectorAll('.nav-item'))
    .filter(item => item.style.display !== 'none');
  return visibleItems.map(item => item.getAttribute('data-id'));
}

function selectNext() {
  const ids = getVisibleSnapshotIds();
  if (ids.length === 0) return;
  const idx = ids.indexOf(activeSnapshotId);
  const nextIdx = Math.min(idx + 1, ids.length - 1);
  selectSnapshot(ids[nextIdx]);
}

function selectPrev() {
  const ids = getVisibleSnapshotIds();
  if (ids.length === 0) return;
  const idx = ids.indexOf(activeSnapshotId);
  const prevIdx = Math.max(idx - 1, 0);
  selectSnapshot(ids[prevIdx]);
}

function setFilter(type, btn) {
  currentFilter = type;
  document.querySelectorAll('.filter-btn').forEach(b => b.classList.remove('active'));
  btn.classList.add('active');
  applyFilters();
}

function onSearchInput(query) {
  applyFilters();
}

function applyFilters() {
  const query = (document.getElementById('search-box').value || '').toLowerCase();
  const navItems = document.querySelectorAll('.nav-item');
  const cards = document.querySelectorAll('.snapshot-card');

  let firstVisibleId = '';

  navItems.forEach(item => {
    const id = item.getAttribute('data-id');
    const label = (item.querySelector('.nav-label').innerText || '').toLowerCase();
    const isDiff = item.classList.contains('text-diff') || item.classList.contains('color-diff');
    const isMatch = item.classList.contains('match');

    let matchFilter = true;
    if (currentFilter === 'diff' && !isDiff) matchFilter = false;
    if (currentFilter === 'match' && !isMatch) matchFilter = false;

    let matchQuery = label.includes(query);

    if (matchFilter && matchQuery) {
      item.style.display = 'flex';
      if (!firstVisibleId) firstVisibleId = id;
    } else {
      item.style.display = 'none';
    }
  });

  cards.forEach(card => {
    const label = (card.getAttribute('data-label') || '').toLowerCase();
    const isDiff = card.classList.contains('text-diff') || card.classList.contains('color-diff');
    const isMatch = card.classList.contains('match');

    let matchFilter = true;
    if (currentFilter === 'diff' && !isDiff) matchFilter = false;
    if (currentFilter === 'match' && !isMatch) matchFilter = false;

    let matchQuery = label.includes(query);

    if (matchFilter && matchQuery) {
      card.style.display = '';
    } else {
      card.style.display = 'none';
    }
  });

  if (firstVisibleId) {
    selectSnapshot(firstVisibleId);
  }
}

// Keyboard navigation
document.addEventListener('keydown', (e) => {
  if (e.target.tagName === 'INPUT' || e.target.tagName === 'TEXTAREA') return;
  if (e.key === 'j' || e.key === 'ArrowDown') {
    selectNext();
    e.preventDefault();
  } else if (e.key === 'k' || e.key === 'ArrowUp') {
    selectPrev();
    e.preventDefault();
  }
});

// Initialize on page load
window.addEventListener('DOMContentLoaded', () => {
  const hash = window.location.hash.replace('#', '');
  if (hash && document.getElementById(hash)) {
    selectSnapshot(hash);
  } else {
    const firstItem = document.querySelector('.nav-item');
    if (firstItem) {
      selectSnapshot(firstItem.getAttribute('data-id'));
    }
  }
});
</script>
</body>
</html>
`, title, time.Now().Format("2006-01-02 15:04:05"), totalSnapshots, perfectMatches, totalSnapshots,
		avgColorClass, avgParity, totalDiffs, totalColorDiffs,
		totalSnapshots, totalSnapshots-perfectMatches, perfectMatches,
		navItems.String(), sections.String())
}

func escapeHTML(s string) string {
	s = strings.ReplaceAll(s, "&", "&amp;")
	s = strings.ReplaceAll(s, "<", "&lt;")
	s = strings.ReplaceAll(s, ">", "&gt;")
	s = strings.ReplaceAll(s, `"`, "&quot;")
	return s
}

// runReportCmd aggregates standalone HTML files into a single master report HTML.
func runReportCmd(args []string) {
	fs := flag.NewFlagSet("report", flag.ExitOnError)
	dir := fs.String("dir", "/tmp", "Directory to search for tuialign_*.html files")
	out := fs.String("out", "/tmp/tuialign_master_report.html", "Output path for aggregated master HTML report")
	openBrowser := fs.Bool("open", false, "Open generated HTML report in default browser")
	fs.Parse(args)

	var files []string
	if len(fs.Args()) > 0 {
		files = fs.Args()
	} else {
		matches, err := filepath.Glob(filepath.Join(*dir, "tuialign_*.html"))
		if err != nil {
			fmt.Fprintf(os.Stderr, "glob error in %s: %v\n", *dir, err)
			os.Exit(1)
		}
		// Filter out any previous master reports
		for _, m := range matches {
			base := filepath.Base(m)
			if base != "tuialign_master_report.html" && base != "tuialign_report.html" && !strings.HasSuffix(base, "_report.html") {
				files = append(files, m)
			}
		}
	}

	if len(files) == 0 {
		fmt.Printf("[tuialign:report] No tuialign_*.html snapshot files found in %s.\n", *dir)
		return
	}

	fmt.Printf("[tuialign:report] Aggregating %d snapshot files from %s...\n", len(files), *dir)
	items, err := AggregateHTMLFiles(files)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error aggregating files: %v\n", err)
		os.Exit(1)
	}

	htmlContent := RenderMasterHTMLReport("TUIAlign Parity Master Report", items)
	if err := os.WriteFile(*out, []byte(htmlContent), 0644); err != nil {
		fmt.Fprintf(os.Stderr, "error writing report to %s: %v\n", *out, err)
		os.Exit(1)
	}

	fmt.Println("\n==================================================================")
	fmt.Printf("✓ MASTER REPORT GENERATED: %d snapshots aggregated!\n", len(items))
	fmt.Printf("  Report Path: %s\n", *out)
	fmt.Printf("  File URL:    file://%s\n", *out)
	fmt.Println("==================================================================")

	if *openBrowser {
		_ = exec.Command("open", *out).Run()
	}
}
