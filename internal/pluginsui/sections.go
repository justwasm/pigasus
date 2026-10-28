package pluginsui

import (
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
