package mockremoteagent

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
)

// TestTriggerLifecycle pins create → list → update → delete and the option
// catalogue shape.
func TestTriggerLifecycle(t *testing.T) {
	srv, err := NewServer(WithAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()

	create, _ := json.Marshal(map[string]any{"trigger": map[string]any{
		"displayName": "nightly", "prompt": "run the checks", "optionId": "time",
		"workspaceUris": []string{"file:///ws"},
	}})
	rec := httptest.NewRecorder()
	srv.service.handleCreateTrigger(rec, create)
	var created CreateTriggerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &created); err != nil {
		t.Fatalf("decode create: %v", err)
	}
	if created.Trigger.TriggerID == "" || created.Trigger.State != "TRIGGER_STATE_ACTIVE" || created.Trigger.CreateTime == "" {
		t.Fatalf("created = %+v", created.Trigger)
	}
	id := created.Trigger.TriggerID

	rec = httptest.NewRecorder()
	srv.service.handleListTriggers(rec, nil)
	var listed ListTriggersResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Triggers) != 1 || listed.Triggers[0].Prompt != "run the checks" {
		t.Fatalf("listed = %+v", listed.Triggers)
	}

	update, _ := json.Marshal(map[string]any{
		"trigger":    map[string]any{"triggerId": id, "state": "TRIGGER_STATE_PAUSED"},
		"updateMask": []string{"state"},
	})
	rec = httptest.NewRecorder()
	srv.service.handleUpdateTrigger(rec, update)
	var updated UpdateTriggerResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &updated); err != nil {
		t.Fatalf("decode update: %v", err)
	}
	if updated.Trigger.State != "TRIGGER_STATE_PAUSED" || updated.Trigger.CreateTime != created.Trigger.CreateTime {
		t.Fatalf("updated = %+v", updated.Trigger)
	}

	deleteBody, _ := json.Marshal(map[string]any{"triggerId": id, "abortActiveRuns": true})
	rec = httptest.NewRecorder()
	srv.service.handleDeleteTrigger(rec, deleteBody)
	rec = httptest.NewRecorder()
	srv.service.handleListTriggers(rec, nil)
	if err := json.Unmarshal(rec.Body.Bytes(), &listed); err != nil {
		t.Fatalf("decode list: %v", err)
	}
	if len(listed.Triggers) != 0 {
		t.Errorf("triggers after delete = %+v", listed.Triggers)
	}
}

func TestTriggerOptionsShape(t *testing.T) {
	srv, err := NewServer(WithAddr("127.0.0.1:0"))
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	defer srv.Close()
	rec := httptest.NewRecorder()
	srv.service.handleListTriggerOptions(rec)
	var got ListTriggerOptionsResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(got.TriggerOptions) == 0 || got.TriggerOptions[0].ID == "" || got.TriggerOptions[0].Category == "" {
		t.Errorf("options = %+v", got.TriggerOptions)
	}
}
