// slash_viewer_changelog.go — the /changelog panel document, excerpts and styling.
package cliapp

import (
	"github.com/justwasm/pigasus/internal/changelog"
	"github.com/justwasm/pigasus/internal/tuiviewer"
)

// changelogBody is the opening set of bullet points under `## 1.3.6`.
var changelogBody = tuiviewer.ChangelogBody

// panelChangelogLines renders the /changelog viewer.
func (m *runMode) panelChangelogLines() []string {
	spec, _ := m.viewerSpecFor()
	rows, start, end := m.viewerWindow(spec)
	lines := append([]string{}, rows...)
	lines = append(lines,
		viewerCounter(start, end, spec.total),
		"",
		panelDotHint("  ", "↑/↓", "Navigate", "esc", "Cancel"))
	return lines
}

// changelogExcerptRows is the visible first screen of the /changelog viewer.
func changelogExcerptRows() []string {
	return tuiviewer.ChangelogExcerptRows()
}

// changelogWindowRows is how many document rows the viewer shows at once.
func changelogWindowRows() int {
	return tuiviewer.ChangelogWindowRows()
}

const changelogTotalLines = tuiviewer.ChangelogTotalLines

// changelogViewerLines is the /changelog document.
func (m *runMode) changelogViewerLines() []string {
	if m.width > 0 && m.height > 0 {
		if src := changelogSourceLines(); src != nil {
			return changelogSourceWrapped(src, m.width)
		}
		return changelog.PanelDoc(m.width)
	}
	if src := changelogSourceLines(); src != nil {
		return changelog.Styled(src)
	}
	out := append([]string{}, changelogExcerptRows()...)
	return append(out, changelogRestRows...)
}

// changelogRestRows is the remainder of the release notes (1.3.5 down through 1.0.0).
var changelogRestRows = changelog.BuildRestRows()

const (
	chgTextOpen   = changelog.TextOpen
	chgTextClose  = changelog.TextClose
	chgCodeOpen   = changelog.CodeOpen
	chgCodeClose  = changelog.CodeClose
	chgTitleRow   = changelog.TitleRow
	chgHeaderOpen = changelog.HeaderOpen
	chgHeaderEnd  = changelog.HeaderEnd
)

const changelogPadLimit = changelog.PadLimit

// changelogSourceLines returns the loaded release-notes source.
func changelogSourceLines() []string {
	return changelog.Lines()
}

// changelogSourceWrapped renders the loaded source wrapped to width.
func changelogSourceWrapped(src []string, width int) []string {
	return changelog.Wrapped(src, width)
}
