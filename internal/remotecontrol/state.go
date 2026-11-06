// Package remotecontrol — state.go: the Web UI's server-owned state.
//
// Two blocks of state live on the server:
//
//   - the app state — the onboarding flags, the nux bookkeeping and
//     the model selection the picker stores;
//   - the user config — the user settings.
//
// The reference keeps both in its own store: the Web UI reads them
// through JetboxReadAppState / JetboxSubscribeToState /
// JetboxReadUserConfig and hands them back through JetboxWriteState.
//
// The clone answered every write with an empty object and every read
// with a constant, so two things broke in the Web UI:
//
//   - the model picker snapped back to the first model after every
//     click (the selection lives in the app state's modelSelection);
//   - the settings panel applied nothing (the user settings live in
//     the user config).
//
// The store is in-memory: it lives as long as the dispatcher, which
// is the lifetime of the web server or the remote-control daemon.
package remotecontrol

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"sync"

	"github.com/tidwall/gjson"
	"github.com/tidwall/pretty"
	"github.com/tidwall/sjson"

	"github.com/justwasm/pigasus/atomicfile"
	"github.com/justwasm/pigasus/internal/core/confhome"
	"github.com/justwasm/pigasus/internal/features/auth"
	"github.com/justwasm/pigasus/internal/remotecontrol/rpc"
)

// userInfoSettings holds the settings queried and updated by
// FetchUserInfo / SetUserInfo.
type userInfoSettings struct {
	TelemetryEnabled                bool `json:"telemetryEnabled"`
	UserDataCollectionForceDisabled bool `json:"userDataCollectionForceDisabled"`
	MarketingEmailsEnabled          bool `json:"marketingEmailsEnabled"`
}

// lsSetUserInfoRequest is the envelope received by SetUserInfo.
type lsSetUserInfoRequest struct {
	UserSettings      *lsUserSettingsPayload `json:"userSettings"`
	UserSettingsSnake *lsUserSettingsPayload `json:"user_settings"`
	TelemetryEnabled  *bool                  `json:"telemetryEnabled"`
	TelemetryEnabled_ *bool                  `json:"telemetry_enabled"`
	MarketingEmails   *bool                  `json:"marketingEmailsEnabled"`
	MarketingEmails_  *bool                  `json:"marketing_emails_enabled"`
}

// lsUserSettingsPayload carries the fields within userSettings.
type lsUserSettingsPayload struct {
	TelemetryEnabled                     *bool `json:"telemetryEnabled"`
	TelemetryEnabledSnake                *bool `json:"telemetry_enabled"`
	MarketingEmailsEnabled               *bool `json:"marketingEmailsEnabled"`
	MarketingEmailsEnabledSnake          *bool `json:"marketing_emails_enabled"`
	UserDataCollectionForceDisabled      *bool `json:"userDataCollectionForceDisabled"`
	UserDataCollectionForceDisabledSnake *bool `json:"user_data_collection_force_disabled"`
}

func (r *lsSetUserInfoRequest) extractPayload() lsUserSettingsPayload {
	var target lsUserSettingsPayload
	if r.UserSettings != nil {
		target = *r.UserSettings
	} else if r.UserSettingsSnake != nil {
		target = *r.UserSettingsSnake
	}
	if target.TelemetryEnabled == nil && target.TelemetryEnabledSnake != nil {
		target.TelemetryEnabled = target.TelemetryEnabledSnake
	}
	if target.TelemetryEnabled == nil {
		if r.TelemetryEnabled != nil {
			target.TelemetryEnabled = r.TelemetryEnabled
		} else if r.TelemetryEnabled_ != nil {
			target.TelemetryEnabled = r.TelemetryEnabled_
		}
	}
	if target.MarketingEmailsEnabled == nil && target.MarketingEmailsEnabledSnake != nil {
		target.MarketingEmailsEnabled = target.MarketingEmailsEnabledSnake
	}
	if target.MarketingEmailsEnabled == nil {
		if r.MarketingEmails != nil {
			target.MarketingEmailsEnabled = r.MarketingEmails
		} else if r.MarketingEmails_ != nil {
			target.MarketingEmailsEnabled = r.MarketingEmails_
		}
	}
	return target
}

// loadUserInfoSettings seeds the user settings from settings.json or defaults.
func loadUserInfoSettings() userInfoSettings {
	forceDisabled := auth.IsBusinessAccount()
	telemetryEnabled := !forceDisabled
	marketingEmailsEnabled := false

	path := filepath.Join(confhome.Antigravity(), "settings.json")
	if data, err := os.ReadFile(path); err == nil {
		data = confhome.StripBOM(data)
		if gjson.ValidBytes(data) {
			if res := gjson.GetBytes(data, "telemetryEnabled"); res.Exists() {
				if !forceDisabled {
					telemetryEnabled = res.Bool()
				}
			} else if res := gjson.GetBytes(data, "telemetry"); res.Exists() {
				if !forceDisabled {
					telemetryEnabled = res.Bool()
				}
			}
			if res := gjson.GetBytes(data, "marketingEmailsEnabled"); res.Exists() {
				marketingEmailsEnabled = res.Bool()
			}
		}
	}

	return userInfoSettings{
		TelemetryEnabled:                telemetryEnabled,
		UserDataCollectionForceDisabled: forceDisabled,
		MarketingEmailsEnabled:          marketingEmailsEnabled,
	}
}

// persistUserInfoSettings saves telemetryEnabled and marketingEmailsEnabled back to settings.json,
// preserving all existing keys.
func persistUserInfoSettings(telemetryEnabled, marketingEmailsEnabled bool) error {
	dir := confhome.Antigravity()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	path := filepath.Join(dir, "settings.json")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			data = []byte("{\n}")
		} else {
			return err
		}
	} else {
		data = confhome.StripBOM(data)
		if len(bytes.TrimSpace(data)) == 0 || !gjson.ValidBytes(data) {
			data = []byte("{\n}")
		}
	}

	data, err = sjson.SetBytes(data, "telemetryEnabled", telemetryEnabled)
	if err != nil {
		return err
	}
	data, err = sjson.SetBytes(data, "marketingEmailsEnabled", marketingEmailsEnabled)
	if err != nil {
		return err
	}

	prettyData := bytes.TrimSpace(pretty.PrettyOptions(data, &pretty.Options{Indent: "  ", SortKeys: true}))
	return atomicfile.WriteFile(path, prettyData, 0o600)
}

// stateStore holds the app state and the user config.
type stateStore struct {
	mu         sync.Mutex
	appState   map[string]any
	userConfig map[string]any
	userInfo   userInfoSettings
}

// agentOnboardingStateNames spells the jetbox_state_pb
// AgentOnboardingState enum the way proto-JSON spells an enum: by its
// NAME.
//
// The reference's own app state carries
// "agentOnboardingCompleted":"AGENT_ONBOARDING_STATE_COMPLETED"
// (captured first-hand from its JetboxSubscribeToState stream), and
// the Web UI's decoder maps the name back to the number 2 — the
// frontend compares it against 2.
var agentOnboardingStateNames = map[int]string{
	0: "AGENT_ONBOARDING_STATE_UNSPECIFIED",
	1: "AGENT_ONBOARDING_STATE_INCOMPLETE",
	2: "AGENT_ONBOARDING_STATE_COMPLETED",
}

// newStateStore returns a store seeded with the flags the clone has
// always served.
func newStateStore() *stateStore {
	return &stateStore{
		appState: map[string]any{
			"agentOnboardingCompleted": "AGENT_ONBOARDING_STATE_COMPLETED",
			"postOnboarding":           map[string]any{"completedSteps": []any{}},
			"seenNuxs":                 map[string]any{"uids": []any{}},
		},
		userConfig: map[string]any{
			"userSettings": map[string]any{
				"enableNotificationsForSpecialEvents": true,
				"enableSoundsForSpecialEvents":        false,
			},
		},
		userInfo: loadUserInfoSettings(),
	}
}

func (s *stateStore) getUserInfo() userInfoSettings {
	if s == nil {
		return loadUserInfoSettings()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	res := s.userInfo
	if auth.IsBusinessAccount() {
		res.TelemetryEnabled = false
		res.UserDataCollectionForceDisabled = true
	}
	return res
}

func (s *stateStore) isTelemetryEnabled() bool {
	if s == nil {
		return !auth.IsBusinessAccount()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if auth.IsBusinessAccount() {
		return false
	}
	return s.userInfo.TelemetryEnabled && !s.userInfo.UserDataCollectionForceDisabled
}

func (s *stateStore) updateUserInfo(params json.RawMessage) (userInfoSettings, error) {
	if s == nil {
		return loadUserInfoSettings(), nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	var req lsSetUserInfoRequest
	if len(params) > 0 {
		if err := json.Unmarshal(params, &req); err != nil {
			return s.userInfo, err
		}
	}

	payload := req.extractPayload()
	forceDisabled := auth.IsBusinessAccount()
	if forceDisabled {
		s.userInfo.TelemetryEnabled = false
		s.userInfo.UserDataCollectionForceDisabled = true
	} else {
		s.userInfo.UserDataCollectionForceDisabled = false
		if payload.TelemetryEnabled != nil {
			s.userInfo.TelemetryEnabled = *payload.TelemetryEnabled
		}
	}

	if payload.MarketingEmailsEnabled != nil {
		s.userInfo.MarketingEmailsEnabled = *payload.MarketingEmailsEnabled
	}

	_ = persistUserInfoSettings(s.userInfo.TelemetryEnabled, s.userInfo.MarketingEmailsEnabled)

	return s.userInfo, nil
}

// lsWriteStateParams is the JetboxWriteState request shape
// (proto-JSON, camelCase): both blocks are optional.
type lsWriteStateParams struct {
	AppState   map[string]any `json:"appState"`
	UserConfig map[string]any `json:"userConfig"`
}

// write merges a JetboxWriteState payload into the store.
func (s *stateStore) write(params json.RawMessage) {
	if s == nil || len(params) == 0 {
		return
	}
	var req lsWriteStateParams
	if err := json.Unmarshal(params, &req); err != nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(req.AppState) > 0 {
		s.appState = mergeState(s.appState, req.AppState)
		normalizeAppStateEnums(s.appState)
	}
	if len(req.UserConfig) > 0 {
		s.userConfig = mergeState(s.userConfig, req.UserConfig)
	}
}

// normalizeAppStateEnums rewrites the enum-valued app-state fields
// into their canonical numeric form.
//
// The Web UI's model picker stores the selection as the
// exa.codeium_common_pb.Model enum, and proto-JSON spells an enum by
// its NAME: the picker decodes `{"model":"MODEL_PLACEHOLDER_M0"}`
// into the number 1000 and re-encodes it as the name again. The
// store therefore has to understand the name — a payload carrying
// the name is the normal case, not an edge case.
//
// The reference's own store keeps the number (its state is a proto
// message) and marshals the name back out on the read path, which is
// what normalizeAppStateEnums and the read path below mirror.
func normalizeAppStateEnums(appState map[string]any) {
	raw, ok := appState["lastSelectedAgentModel"]
	if !ok {
		return
	}
	value, ok := modelValueFromJSON(raw)
	if !ok {
		return
	}
	appState["lastSelectedAgentModel"] = value
}

// snapshot returns the app state and the user config as the Web UI
// expects them: the two blocks in one object, the same shape
// JetboxSubscribeToState answers with.
func (s *stateStore) snapshot() map[string]any {
	if s == nil {
		return map[string]any{}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	return map[string]any{
		"appState":   s.appStateWire(),
		"userConfig": copyState(s.userConfig),
	}
}

// appStateWire renders the app state the way the reference's server
// does: the enums by their NAME, the numbers for the values the
// enum does not know.
func (s *stateStore) appStateWire() map[string]any {
	out := copyState(s.appState)
	if raw, ok := out["lastSelectedAgentModel"]; ok {
		if value, ok := modelValueFromJSON(raw); ok {
			out["lastSelectedAgentModel"] = modelWireJSON(value)
		}
	}
	if raw, ok := out["agentOnboardingCompleted"]; ok {
		out["agentOnboardingCompleted"] = agentOnboardingStateWire(raw)
	}
	return out
}

// agentOnboardingStateWire spells one enum value by its name. A value
// that is already a name, or a number the enum does not know, is left
// alone.
func agentOnboardingStateWire(raw any) any {
	switch value := raw.(type) {
	case float64:
		if name, ok := agentOnboardingStateNames[int(value)]; ok {
			return name
		}
	case int:
		if name, ok := agentOnboardingStateNames[value]; ok {
			return name
		}
	}
	return raw
}

// mergeState merges the src block into dst, recursing into nested
// objects. The Web UI writes one block at a time (the nux bookkeeping
// without the model selection, the user settings without the rest),
// so a shallow replace would drop what the other call wrote.
func mergeState(dst, src map[string]any) map[string]any {
	out := copyState(dst)
	for key, val := range src {
		if sub, ok := val.(map[string]any); ok {
			if cur, ok := out[key].(map[string]any); ok {
				out[key] = mergeState(cur, sub)
				continue
			}
		}
		out[key] = val
	}
	return out
}

// copyState copies a state block so a caller cannot mutate the
// store's own maps.
func copyState(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for key, val := range in {
		if sub, ok := val.(map[string]any); ok {
			out[key] = copyState(sub)
			continue
		}
		out[key] = val
	}
	return out
}

// handleJetboxWriteState stores the app state and the user config
// the Web UI wrote. The reference's JetboxWriteStateResponse has no
// fields, so the answer stays empty.
func (d *Dispatcher) handleJetboxWriteState(params json.RawMessage) (any, *rpc.ErrorBody, bool) {
	d.state.write(params)
	return map[string]any{}, nil, true
}

// handleJetboxReadState answers the three read paths the Web UI uses
// for the app state and the user config.
func (d *Dispatcher) handleJetboxReadState() (any, *rpc.ErrorBody, bool) {
	return d.state.snapshot(), nil, true
}
