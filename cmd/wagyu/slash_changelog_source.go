// slash_changelog_source.go — cycle changelog-pagination: load the
// /changelog viewer's source document at runtime from
// internal/cli/testdata/changelog_reference.txt (915 lines / ~150 KB of
// release notes, in 1.3.6 → 1.0.0 order) and render it as a scrollable
// panel. Falls back to the cycle-193 frozen block (the `## 1.3.6`
// transcription in `changelogBody` plus the rest parsed from
// cmd/wagyu/testdata/print_changelog.md) when the file cannot be
// located — the binary is intended to be installed in a user's PATH
// where the project tree is not.
package main

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// changelogSource holds the loaded reference file. Loaded once on
// first use; the cached slice is read-only thereafter.
var (
	changelogSourceOnce sync.Once
	changelogSourceData []string // nil when the file was not found
	changelogSourcePath string   // absolute path, or "" on miss
)

// changelogSourceLoaded reports whether the reference file was found
// and parsed.
func changelogSourceLoaded() bool {
	changelogSourceOnce.Do(loadChangelogSource)
	return changelogSourceData != nil
}

// changelogSourceLines returns the raw lines from the reference file
// (each `1.X.Y:` header and each `· …` bullet on its own line), or nil
// when the file could not be located or read.
func changelogSourceLines() []string {
	changelogSourceOnce.Do(loadChangelogSource)
	return changelogSourceData
}

// changelogSourceFilePath returns the absolute path the source was
// loaded from, or "" if the file was not found. Used by the test to
// confirm the runtime loader saw the file.
func changelogSourceFilePath() string {
	changelogSourceOnce.Do(loadChangelogSource)
	return changelogSourcePath
}

// loadChangelogSource reads internal/cli/testdata/changelog_reference.txt
// from the project root (the nearest ancestor of cwd that contains
// go.mod) and stores the lines. The result is cached; a missing or
// unreadable file leaves changelogSourceData nil so callers fall back
// to the embedded cycle-193 transcription.
func loadChangelogSource() {
	path := findChangelogReferencePath()
	if path == "" {
		return
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return
	}
	// Normalise line endings so a CRLF checkout does not break the
	// header / bullet detection.
	text := strings.ReplaceAll(string(data), "\r\n", "\n")
	changelogSourceData = strings.Split(text, "\n")
	changelogSourcePath = path
}

// findChangelogReferencePath returns the absolute path to
// internal/cli/testdata/changelog_reference.txt relative to the project
// root (the nearest ancestor directory containing go.mod). Returns ""
// when no project root is found.
//
// The cwd-based walk lets `go test ./cmd/wagyu/` (run from anywhere)
// find the file, while a production binary that lives in a user's PATH
// simply fails to find it and falls back to the cycle-193 frozen
// block — both cases exercise the same code path.
func findChangelogReferencePath() string {
	cwd, err := os.Getwd()
	if err != nil {
		return ""
	}
	root := findProjectRoot(cwd)
	if root == "" {
		return ""
	}
	candidate := filepath.Join(root, "internal", "cli", "testdata", "changelog_reference.txt")
	if _, err := os.Stat(candidate); err == nil {
		return candidate
	}
	return ""
}

// findProjectRoot walks up from dir looking for go.mod. Returns "" if
// the walk reaches the filesystem root without finding one.
func findProjectRoot(dir string) string {
	cur, err := filepath.Abs(dir)
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(cur, "go.mod")); err == nil {
			return cur
		}
		parent := filepath.Dir(cur)
		if parent == cur {
			return ""
		}
		cur = parent
	}
}

// isChangelogSourceHeader reports whether a line in the bullet-list
// source is a release header (matches `^\d+\.\d+\.\d+:$`). The two
// dotted version parts and the trailing colon are required; the file
// never carries release headers without all three.
func isChangelogSourceHeader(line string) bool {
	if len(line) < 5 || line[len(line)-1] != ':' {
		return false
	}
	body := line[:len(line)-1]
	parts := strings.Split(body, ".")
	if len(parts) != 3 {
		return false
	}
	for _, p := range parts {
		if p == "" {
			return false
		}
		for i := 0; i < len(p); i++ {
			c := p[i]
			if c < '0' || c > '9' {
				return false
			}
		}
	}
	return true
}

// changelogSourceHeaderLabel turns `1.3.6:` into `## 1.3.6`. The panel
// reuses the same bold-blue header styling the cycle-201 markdown
// parser produced.
func changelogSourceHeaderLabel(line string) string {
	return "## " + strings.TrimSuffix(line, ":")
}

// changelogSourceStyled renders the raw bullet-list source lines into
// the panel's visual layout. Headers become bold-blue `## X.Y.Z`
// rows, bullets become dim `• body` rows, blank lines stay blank.
// The first three rows are the title row + blank + tagline so the
// first-screen shape (and therefore the test pins in
// TestPanelChangelogViewerScrolls) match the cycle-193 layout.
func changelogSourceStyled(src []string) []string {
	out := []string{chgTitleRow, "", changelogSourceTagline}
	for _, raw := range src {
		line := strings.TrimRight(raw, "\r")
		switch {
		case isChangelogSourceHeader(line):
			// The reference puts a blank row before and after every
			// release header; the source file only carries the one after
			// the preceding bullet.
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			out = append(out, chgHeaderOpen+changelogSourceHeaderLabel(line)+chgHeaderEnd, "")
		case strings.HasPrefix(line, "· "):
			out = append(out, "  "+chgTextOpen+"\u2022 "+panelPlainInline(line[2:])+chgTextClose)
		case line == "":
			out = append(out, "")
		}
	}
	return out
}

// changelogSourceWrapped is the live-terminal variant of
// changelogSourceStyled: each bullet is word-wrapped to the panel
// width via changelogWrapParagraph (cycle-214). Headers pass through
// unchanged. Inline `code` backticks survive in the source only in
// rare cases, so the styling cost of running them through the same
// wrapper is paid in exchange for consistent wrap behaviour.
func changelogSourceWrapped(src []string, width int) []string {
	budget := changelogWrapBudget(width)
	out := []string{chgTitleRow, "", changelogSourceTagline}
	for _, raw := range src {
		line := strings.TrimRight(raw, "\r")
		switch {
		case isChangelogSourceHeader(line):
			// The reference puts a blank row before and after every
			// release header; the source file only carries the one after
			// the preceding bullet.
			if len(out) > 0 && out[len(out)-1] != "" {
				out = append(out, "")
			}
			out = append(out, chgHeaderOpen+changelogSourceHeaderLabel(line)+chgHeaderEnd, "")
		case strings.HasPrefix(line, "· "):
			out = append(out, changelogWrapParagraph(panelPlainInline(line[2:]), true, budget)...)
		case line == "":
			out = append(out, "")
		}
	}
	return out
}

// changelogSourceTagline is the dim-gray subtitle that sits between
// the title row and the first release section. The reference binary
// emits this on every captured /changelog screen; both the cycle-201
// hardcoded transcription and the cycle-214 height-aware renderer
// carry the same text, so the new bullet-list source emits it too.
const changelogSourceTagline = "  \x1b[38;5;235mThe terminal-first surface to interact with Pigasus agents. Stay in your flow without context switching.\x1b[39m"
