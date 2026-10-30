package pluginsui

import (
	"strconv"
	"strings"

	"github.com/justwasm/pigasus/internal/plugins"
)

// DefaultDiscoverSections are the four sections the Antigravity plugin marketplace groups by.
var DefaultDiscoverSections = []string{
	"Developer Tools",
	"Productivity",
	"DevOps",
	"Data & Analytics",
}

// DiscoverSection holds a section's title and its matching plugin items.
type DiscoverSection struct {
	Name  string
	Items []plugins.Row
}

// DiscoverSectionHeader formats the bold section title for the Discover tab.
func DiscoverSectionHeader(title string) string {
	return "\x1b[1m  " + title + "\x1b[0m"
}

// PluginBelongsToSection tests if a plugin belongs to a given section.
func PluginBelongsToSection(p plugins.Row, section string) bool {
	cat := strings.ToLower(p.Category)
	text := strings.ToLower(p.Name + " " + p.Desc + " " + strings.Join(p.Keywords, " "))

	switch section {
	case "Data & Analytics":
		if cat == "databases" || cat == "data & analytics" || cat == "data" {
			return true
		}
		for _, kw := range []string{"data", "analytics", "bigquery", "sql", "database", "databases", "science", "spark", "dbt", "biomolecular", "alphafold", "chembl", "genomics", "tabular", "spanner", "alloydb"} {
			if strings.Contains(text, kw) {
				return true
			}
		}
		return false

	case "DevOps":
		if cat == "cloud" || cat == "devops" {
			return true
		}
		for _, kw := range []string{"cloud", "devops", "deploy", "firebase", "docker", "kubernetes", "ci/cd", "infrastructure", "terraform", "aws", "gcp", "azure", "dataflow", "airflow"} {
			if strings.Contains(text, kw) {
				return true
			}
		}
		return false

	case "Productivity":
		if cat == "productivity" || cat == "design" || cat == "office" {
			return true
		}
		for _, kw := range []string{"docs", "calendar", "drive", "notes", "productivity", "sheet", "tasks", "workspace", "work management", "notion", "linear", "trello", "slack", "mail"} {
			if strings.Contains(text, kw) {
				return true
			}
		}
		return false

	case "Developer Tools":
		if cat == "developer tools" {
			return true
		}
		for _, kw := range []string{"tool", "developer", "devtools", "sdk", "git", "debug", "test", "chrome", "code", "api", "extension", "ide", "web", "terminal", "python", "go", "node", "typescript", "react", "flutter", "android"} {
			if strings.Contains(text, kw) {
				return true
			}
		}
		// Fallback: if it doesn't match any of Data & Analytics, DevOps, or Productivity,
		// it belongs to Developer Tools so no plugin is orphaned.
		if !PluginBelongsToSection(p, "Data & Analytics") &&
			!PluginBelongsToSection(p, "DevOps") &&
			!PluginBelongsToSection(p, "Productivity") {
			return true
		}
		return false
	}
	return false
}

// ShelfLimit is how many plugins a shelf shows before its "more" row.
//
// 【一手】 agy 1.4.11: "each shelf now shows its first 6 plugins followed by
// a `⋯ N more — enter to show` row".
const ShelfLimit = 6

// ShelfRow is one row of a shelf: a plugin, or the "more" row that stands
// in for the shelf's tail.
type ShelfRow struct {
	// Plugin is the plugin the row shows. It is the zero value on a
	// "more" row.
	Plugin plugins.Row
	// More marks the "⋯ N more — enter to show" row.
	More bool
	// Hidden is the number of plugins the "more" row stands for.
	Hidden int
}

// ShelfRows returns the rows a shelf renders: its first ShelfLimit plugins
// and — when the shelf holds more and is not expanded — a "more" row for
// the tail.
//
// The reference's shelf is a marketplace shelf (a curated group of
// plugins); the clone's shelf is one of the Discover sections, because
// the marketplace feed the clone reads carries no shelf of its own.
func ShelfRows(items []plugins.Row, expanded bool) []ShelfRow {
	limit := len(items)
	if !expanded && limit > ShelfLimit {
		limit = ShelfLimit
	}
	out := make([]ShelfRow, 0, limit+1)
	for _, it := range items[:limit] {
		out = append(out, ShelfRow{Plugin: it})
	}
	if limit < len(items) {
		out = append(out, ShelfRow{More: true, Hidden: len(items) - limit})
	}
	return out
}

// ShelfMoreText is the text of a shelf's "more" row.
//
// 【一手】 agy 1.4.11's own wording: "a `⋯ N more — enter to show` row".
// 【strings】 the 1.4.11 binary carries the format "%d more — enter to
// show" (em dash) next to the "⋯ " glyph.
func ShelfMoreText(hidden int) string {
	return "⋯ " + strconv.Itoa(hidden) + " more — enter to show"
}

// GroupDiscoverSections partitions items into the DefaultDiscoverSections.
// A plugin appears in every section it belongs to.
func GroupDiscoverSections(items []plugins.Row) []DiscoverSection {
	var sections []DiscoverSection
	for _, sec := range DefaultDiscoverSections {
		var matched []plugins.Row
		for _, it := range items {
			if PluginBelongsToSection(it, sec) {
				matched = append(matched, it)
			}
		}
		if len(matched) > 0 {
			sections = append(sections, DiscoverSection{
				Name:  sec,
				Items: matched,
			})
		}
	}
	return sections
}
