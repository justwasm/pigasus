package pluginsui

import (
	"testing"

	"github.com/justwasm/pigasus/internal/plugins"
)

func TestPluginBelongsToSection(t *testing.T) {
	cases := []struct {
		plugin  plugins.Row
		section string
		want    bool
	}{
		// Data & Analytics
		{
			plugin:  plugins.Row{Name: "data-agent-kit", Desc: "Transform data with dbt, BigQuery SQL", Keywords: []string{"bigquery", "spark"}},
			section: "Data & Analytics",
			want:    true,
		},
		{
			plugin:  plugins.Row{Name: "science-tools", Desc: "Genomics and biomolecular structure viewer"},
			section: "Data & Analytics",
			want:    true,
		},
		{
			plugin:  plugins.Row{Name: "gcalendar", Desc: "Calendar scheduling", Category: "Productivity"},
			section: "Data & Analytics",
			want:    false,
		},

		// DevOps
		{
			plugin:  plugins.Row{Name: "cloud-deploy", Desc: "Kubernetes and Docker deployment to GCP"},
			section: "DevOps",
			want:    true,
		},
		{
			plugin:  plugins.Row{Name: "firebase-tools", Desc: "Firebase integration and hosting"},
			section: "DevOps",
			want:    true,
		},
		{
			plugin:  plugins.Row{Name: "data-agent-kit", Desc: "Cloud pipelines and airflow dataflow", Keywords: []string{"gcp", "airflow"}},
			section: "DevOps",
			want:    true,
		},

		// Productivity
		{
			plugin:  plugins.Row{Name: "gcalendar", Desc: "Check availability and schedule events", Category: "Productivity"},
			section: "Productivity",
			want:    true,
		},
		{
			plugin:  plugins.Row{Name: "notion-sync", Desc: "Sync notes and tasks with workspace"},
			section: "Productivity",
			want:    true,
		},

		// Developer Tools
		{
			plugin:  plugins.Row{Name: "chrome-devtools-plugin", Desc: "Debugging and automation in Chrome", Category: "Developer Tools"},
			section: "Developer Tools",
			want:    true,
		},
		{
			plugin:  plugins.Row{Name: "custom-utility", Desc: "A generic helper plugin with no tags"},
			section: "Developer Tools",
			want:    true, // fallback
		},
	}

	for _, c := range cases {
		got := PluginBelongsToSection(c.plugin, c.section)
		if got != c.want {
			t.Errorf("PluginBelongsToSection(%s, %s) = %v, want %v", c.plugin.Name, c.section, got, c.want)
		}
	}
}

func TestGroupDiscoverSections(t *testing.T) {
	items := []plugins.Row{
		{Name: "sql-tool", Desc: "BigQuery and SQL database helper"},
		{Name: "deployer", Desc: "Kubernetes docker deployment"},
		{Name: "calendar", Desc: "Calendar schedule and tasks"},
		{Name: "debugger", Desc: "Chrome debug tools"},
	}

	sections := GroupDiscoverSections(items)
	if len(sections) != 4 {
		t.Fatalf("expected 4 sections, got %d", len(sections))
	}

	expectedNames := []string{"Developer Tools", "Productivity", "DevOps", "Data & Analytics"}
	for i, name := range expectedNames {
		if sections[i].Name != name {
			t.Errorf("section %d: got %s, want %s", i, sections[i].Name, name)
		}
		if len(sections[i].Items) == 0 {
			t.Errorf("section %s has no items", name)
		}
	}
}
