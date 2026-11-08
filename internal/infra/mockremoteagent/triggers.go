// triggers.go — the proxy's trigger management RPCs.
//
// The reference models an automation as
// exa.remote_agent_proxy_pb.RemoteTrigger: an option (time/event), the
// prompt to run, the workspace, a state and a field map. This server keeps
// them in memory for the lifetime of the process.
package mockremoteagent

import (
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"time"
)

func (s *Service) handleListTriggers(w http.ResponseWriter, _ []byte) {
	s.triggersMu.Lock()
	triggers := make([]Trigger, 0, len(s.triggers))
	for _, trigger := range s.triggers {
		triggers = append(triggers, trigger)
	}
	s.triggersMu.Unlock()
	sort.Slice(triggers, func(i, j int) bool { return triggers[i].TriggerID < triggers[j].TriggerID })
	writeJSON(w, ListTriggersResponse{Triggers: triggers})
}

func (s *Service) handleListTriggerOptions(w http.ResponseWriter) {
	writeJSON(w, ListTriggerOptionsResponse{TriggerOptions: []TriggerOption{
		{ID: "time", DisplayName: "Scheduled trigger", Description: "Run on a recurring schedule", Category: "TRIGGER_CATEGORY_TIME"},
		{ID: "event", DisplayName: "Event trigger", Description: "Run when an event matches", Category: "TRIGGER_CATEGORY_EVENT"},
	}})
}

func (s *Service) handleCreateTrigger(w http.ResponseWriter, body []byte) {
	var req CreateTriggerRequest
	_ = json.Unmarshal(body, &req)
	trigger := req.Trigger
	if trigger.TriggerID == "" {
		trigger.TriggerID = fmt.Sprintf("trig-%d", time.Now().UnixNano())
	}
	if trigger.State == "" {
		trigger.State = "TRIGGER_STATE_ACTIVE"
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if trigger.CreateTime == "" {
		trigger.CreateTime = now
	}
	trigger.UpdateTime = now
	s.triggersMu.Lock()
	s.triggers[trigger.TriggerID] = trigger
	s.triggersMu.Unlock()
	writeJSON(w, CreateTriggerResponse{Trigger: trigger})
}

func (s *Service) handleUpdateTrigger(w http.ResponseWriter, body []byte) {
	var req UpdateTriggerRequest
	_ = json.Unmarshal(body, &req)
	trigger := req.Trigger
	if trigger.TriggerID == "" {
		http.Error(w, "triggerId is required", http.StatusBadRequest)
		return
	}
	s.triggersMu.Lock()
	existing, ok := s.triggers[trigger.TriggerID]
	if ok {
		if trigger.CreateTime == "" {
			trigger.CreateTime = existing.CreateTime
		}
		if trigger.State == "" {
			trigger.State = existing.State
		}
		if trigger.OptionID == "" {
			trigger.OptionID = existing.OptionID
		}
		if len(trigger.Fields) == 0 {
			trigger.Fields = existing.Fields
		}
	}
	trigger.UpdateTime = time.Now().UTC().Format(time.RFC3339)
	s.triggers[trigger.TriggerID] = trigger
	s.triggersMu.Unlock()
	writeJSON(w, UpdateTriggerResponse{Trigger: trigger})
}

func (s *Service) handleDeleteTrigger(w http.ResponseWriter, body []byte) {
	var req DeleteTriggerRequest
	_ = json.Unmarshal(body, &req)
	s.triggersMu.Lock()
	delete(s.triggers, req.TriggerID)
	s.triggersMu.Unlock()
	writeJSON(w, map[string]any{"success": true})
}
