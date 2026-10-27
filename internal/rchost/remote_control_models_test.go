// Tests for the host's model catalogue (remote_control_backends.go).
//
// The catalogue is the Web UI's picker, and one field of it decides
// whether a picture can be attached at all: the model's own word about
// pictures (`supports_images` in the provider configuration). The word
// travels from the file to the catalogue row, and the catalogue row is
// what the dispatcher's model configs are built from.
package rchost

import (
	"testing"

	"github.com/justwasm/pigasus/internal/configtest"
)

// TestHostListModelsCarriesTheSupportsImagesMarker pins the marker on
// the catalogue row: a model that states it keeps the statement, and a
// model that says nothing stays silent — the family table decides for
// it (remotecontrol.modelTakesImages).
func TestHostListModelsCarriesTheSupportsImagesMarker(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"agw": {
	  "type": "openai",
	  "api_url": "https://gw.example/v1/chat/completions",
	  "models": [
	    {"id": "minimax-m3", "name": "MiniMax M3", "model": "MiniMax-M3", "supports_images": true},
	    {"id": "plain", "name": "Plain", "model": "Plain"}
	  ]}}}`)
	rows, err := hostListModels("", "", "")
	if err != nil {
		t.Fatalf("hostListModels: %v", err)
	}
	byID := map[string]int{}
	for i, r := range rows {
		byID[r.ID] = i
	}
	idx, ok := byID["minimax-m3"]
	if !ok {
		t.Fatalf("the catalogue does not carry the model: %+v", rows)
	}
	if rows[idx].SupportsImages == nil || !*rows[idx].SupportsImages {
		t.Errorf("the marker did not reach the catalogue row: %+v", rows[idx])
	}
	idx, ok = byID["plain"]
	if !ok {
		t.Fatalf("the catalogue does not carry the model: %+v", rows)
	}
	if rows[idx].SupportsImages != nil {
		t.Errorf("a model that says nothing must carry no marker: %+v", rows[idx])
	}
}

// TestHostListModelsCarriesTheModelAttributes pins the rest of the
// per-model attributes on the catalogue row.
//
// The row is what the dispatcher's model configs are built from, so an
// attribute that does not travel this far never reaches the Web UI's
// picker.
func TestHostListModelsCarriesTheModelAttributes(t *testing.T) {
	configtest.WriteProviders(t, `{"providers": {"agw": {
	  "type": "openai",
	  "api_url": "https://gw.example/v1/chat/completions",
	  "models": [
	    {"id": "full", "name": "Full", "model": "Full-v1",
	     "supported_mime_types": ["image/png", "application/pdf"],
	     "disabled": true,
	     "quota_info": {"remaining_fraction": 0.25, "reset_time": "2026-10-27T00:00:00Z"},
	     "supports_thought_circulation": true,
	     "model_url": "https://example.com/models/full",
	     "tag_title": "NEW",
	     "tag_description": "The newest model",
	     "effort_supported": false},
	    {"id": "silent", "name": "Silent", "model": "Silent-v1"}
	  ]}}}`)
	rows, err := hostListModels("", "", "")
	if err != nil {
		t.Fatalf("hostListModels: %v", err)
	}
	byID := map[string]int{}
	for i, r := range rows {
		byID[r.ID] = i
	}
	idx, ok := byID["full"]
	if !ok {
		t.Fatalf("the catalogue does not carry the model: %+v", rows)
	}
	full := rows[idx]
	if len(full.SupportedMimeTypes) != 2 || full.SupportedMimeTypes[0] != "image/png" {
		t.Errorf("the media types did not reach the catalogue row: %+v", full)
	}
	if !full.Disabled {
		t.Errorf("the disabled flag did not reach the catalogue row: %+v", full)
	}
	if full.QuotaInfo == nil || full.QuotaInfo.ResetTime != "2026-10-27T00:00:00Z" {
		t.Errorf("the quota did not reach the catalogue row: %+v", full.QuotaInfo)
	}
	if full.QuotaInfo != nil && full.QuotaInfo.RemainingFraction == nil {
		t.Errorf("the quota's share did not reach the catalogue row: %+v", full.QuotaInfo)
	}
	if full.SupportsThoughtCirculation == nil || !*full.SupportsThoughtCirculation {
		t.Errorf("the thought marker did not reach the catalogue row: %+v", full)
	}
	if full.ModelURL != "https://example.com/models/full" {
		t.Errorf("the model's own link did not reach the catalogue row: %+v", full)
	}
	if full.ModelID != "Full-v1" {
		t.Errorf("the upstream id did not reach the catalogue row: %+v", full)
	}
	if full.TagTitle != "NEW" || full.TagDescription != "The newest model" {
		t.Errorf("the tag line did not reach the catalogue row: %+v", full)
	}
	// The reasoning level: the configuration's word wins, and a model
	// that says nothing keeps the clone's long-standing default.
	if full.EffortSupported {
		t.Errorf("the effort flag did not reach the catalogue row: %+v", full)
	}
	idx, ok = byID["silent"]
	if !ok {
		t.Fatalf("the catalogue does not carry the model: %+v", rows)
	}
	silent := rows[idx]
	if len(silent.SupportedMimeTypes) != 0 || silent.Disabled || silent.QuotaInfo != nil {
		t.Errorf("a model that says nothing must carry no attributes: %+v", silent)
	}
	if silent.SupportsThoughtCirculation != nil || silent.ModelURL != "" {
		t.Errorf("a model that says nothing must carry no attributes: %+v", silent)
	}
	// The upstream id is not a statement — it is the model's own name
	// at the endpoint, and every model of the user's own has one.
	if silent.ModelID != "Silent-v1" {
		t.Errorf("the upstream id did not reach the catalogue row: %+v", silent)
	}
	if !silent.EffortSupported {
		t.Errorf("a model that says nothing keeps the default: %+v", silent)
	}
}
