package main

import (
	"flag"
	"testing"

	"github.com/justwasm/pigasus/internal/backend"
)

// modelTransportTestFlags builds the minimal flag set pickBackend reads.
func modelTransportTestFlags(t *testing.T, backendFlag string) *flag.FlagSet {
	t.Helper()
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.String("backend", "", "")
	if backendFlag != "" {
		if err := fs.Set("backend", backendFlag); err != nil {
			t.Fatalf("set backend: %v", err)
		}
	}
	return fs
}

func TestPickBackend_RoutesByModelFamily(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token") // skip the shim downgrade
	t.Setenv("OPENAGY_BACKEND", "")

	cases := []struct {
		name  string
		model string
		want  string
	}{
		{"official gemini label", "Gemini 3.8 Flash (High)", "cloudcode"},
		{"official gemini slug", "gemini-3.8-flash-low", "cloudcode"},
		{"official claude", "Claude Sonnet 4.6 (Thinking)", "cloudcode"},
		{"official gpt-oss", "GPT-OSS 120B (Medium)", "cloudcode"},
		{"custom minimax", "MiniMax M3", "agw"},
		{"unknown falls back to agw", "some-other-model", "agw"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			be := pickBackendWithLog(modelTransportTestFlags(t, ""), tc.model, nil, 0)
			switch tc.want {
			case "cloudcode":
				cc, ok := be.(*backend.CloudCodeClient)
				if !ok {
					t.Fatalf("backend = %T, want *backend.CloudCodeClient", be)
				}
				if cc.Model() != resolveCatalogueID(tc.model) {
					t.Errorf("wire model = %q, want %q", cc.Model(), resolveCatalogueID(tc.model))
				}
			case "agw":
				if _, ok := be.(*backend.AgwK0sClient); !ok {
					t.Fatalf("backend = %T, want *backend.AgwK0sClient", be)
				}
			}
		})
	}
}

func TestPickBackend_ExplicitAgwWinsOverModel(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	t.Setenv("OPENAGY_BACKEND", "")
	be := pickBackendWithLog(modelTransportTestFlags(t, "agw"), "Gemini 3.8 Flash (High)", nil, 0)
	if _, ok := be.(*backend.AgwK0sClient); !ok {
		t.Fatalf("backend = %T, want *backend.AgwK0sClient (explicit --backend agw)", be)
	}
}

func TestPickBackend_ExplicitCloudCode(t *testing.T) {
	t.Setenv("JETSKI_OAUTH_TOKEN", "test-token")
	t.Setenv("OPENAGY_BACKEND", "")
	be := pickBackendWithLog(modelTransportTestFlags(t, "cloudcode"), "MiniMax M3", nil, 0)
	if _, ok := be.(*backend.CloudCodeClient); !ok {
		t.Fatalf("backend = %T, want *backend.CloudCodeClient (explicit --backend cloudcode)", be)
	}
}

// TestModelSelectionSwapsBackend pins the runtime half of the fix: choosing a
// model in /model must replace m.backend, not only m.model, or the next turn
// still goes to MiniMax.
func TestModelSelectionSwapsBackend(t *testing.T) {
	cycle196TestHome(t, map[string]any{"model": "gemini-3.8-flash-medium"})
	m := mkPanel(panelModel, "/model", 1)
	old := backend.NewMock()
	m.backend = old

	byModel := map[string]backend.Client{}
	m.backendFor = func(label string) backend.Client {
		c := backend.NewMock()
		byModel[label] = c
		return c
	}

	// Park the cursor on the Gemini row and press Enter.
	for i, r := range modelRowsView() {
		if r.slug == "gemini-3.8-flash" {
			m.panel.cursor = i
			break
		}
	}
	label := m.selectModelEffort()
	if label == "" {
		t.Fatal("selectModelEffort returned no label")
	}
	got, ok := byModel[label]
	if !ok {
		t.Fatalf("backend factory never saw %q (saw %v)", label, keysOf(byModel))
	}
	if m.backend != got {
		t.Fatal("m.backend was not rebuilt on Enter")
	}

	// ←/→ on the same row must rebuild too. Re-park the cursor: the
	// Enter path closes the panel in the real UI.
	m.backend = old
	for i, r := range modelRowsView() {
		if r.slug == "gemini-3.8-flash" {
			m.panel.cursor = i
			break
		}
	}
	m.cycleModelEffort(+1)
	cycleLabel := m.model
	cycleClient, ok := byModel[cycleLabel]
	if !ok {
		t.Fatalf("backend factory never saw %q on ←/→ (saw %v)", cycleLabel, keysOf(byModel))
	}
	if m.backend != cycleClient {
		t.Fatal("m.backend was not rebuilt on ←/→")
	}
}

func keysOf(m map[string]backend.Client) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
