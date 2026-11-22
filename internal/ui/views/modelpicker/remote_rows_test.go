package modelpicker_test

import (
	"strings"
	"testing"

	"github.com/justwasm/pigasus/internal/ui/views/modelpicker"
)

func TestModelPickerRemoteAgentFiltering(t *testing.T) {
	// Standard local picker with custom rows
	pLocal := modelpicker.New(modelpicker.Seams{
		CustomRows: func() []modelpicker.Row {
			return []modelpicker.Row{
				{Display: "Local Custom Model", Slug: "local-custom"},
			}
		},
		IsRemoteAgent: func() bool { return false },
	})
	rowsLocal := pLocal.Rows()
	if len(rowsLocal) != 1 || rowsLocal[0].Display != "Local Custom Model" {
		t.Fatalf("expected custom row in local mode, got %+v", rowsLocal)
	}

	// Remote agent picker: custom rows MUST be omitted, unsupported catalog models marked
	pRemote := modelpicker.New(modelpicker.Seams{
		CustomRows: func() []modelpicker.Row {
			return []modelpicker.Row{
				{Display: "Local Custom Model", Slug: "local-custom"},
			}
		},
		IsRemoteAgent: func() bool { return true },
	})
	rowsRemote := pRemote.Rows()
	for _, r := range rowsRemote {
		if r.Display == "Local Custom Model" {
			t.Errorf("remote agent picker must omit local custom models, found: %s", r.Display)
		}
		if r.Slug == "gemini-3.8-flash" || strings.Contains(r.Slug, "claude-sonnet-4-6") || strings.Contains(r.Slug, "claude-opus-4-6") {
			if r.Note == "not available for remote agents" {
				t.Errorf("model %q should be supported remotely but was marked unavailable", r.Display)
			}
		}
		if r.Slug == "gpt-oss-120b-medium" || strings.Contains(r.Slug, "gemini-4-") {
			if r.Note != "not available for remote agents" {
				t.Errorf("model %q should be marked 'not available for remote agents', got Note=%q", r.Display, r.Note)
			}
			if !r.Dim {
				t.Errorf("model %q should be dim in remote agent picker", r.Display)
			}
			if !strings.Contains(r.UnavailableReason, "isn't available for remote conversations yet") {
				t.Errorf("model %q should have 1.5.14 unavailable reason, got %q", r.Display, r.UnavailableReason)
			}
		}
	}
}
