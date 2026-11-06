package remotecontrol

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestLanguageServer_GetLocalUserInfo(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	resText, err := d.DispatchDirect(context.Background(), "GetLocalUserInfo", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect GetLocalUserInfo: %v", err)
	}

	var res map[string]any
	if err := json.Unmarshal([]byte(resText), &res); err != nil {
		t.Fatalf("Unmarshal result: %v", err)
	}

	if res["username"] == nil || res["username"] == "" {
		t.Errorf("expected username, got %v", res["username"])
	}
	if res["homeDirUri"] == nil || !filepath.IsAbs(parseURIPath(res["homeDirUri"].(string))) {
		t.Errorf("expected absolute homeDirUri, got %v", res["homeDirUri"])
	}
	if res["machineType"] != "local" {
		t.Errorf("expected machineType=local, got %v", res["machineType"])
	}
}

func TestLanguageServer_Heartbeat(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	resText, err := d.DispatchDirect(context.Background(), "Heartbeat", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect Heartbeat: %v", err)
	}

	// The reference's own answer is
	// {"lastExtensionHeartbeat":"2026-10-25T19:31:11.993337Z"}: a
	// google.protobuf.Timestamp is a proto-JSON *string*, and the
	// message is not the GetStatus one.
	var res map[string]any
	if err := json.Unmarshal([]byte(resText), &res); err != nil {
		t.Fatalf("Unmarshal result: %v", err)
	}
	if _, ok := res["status"]; ok {
		t.Fatalf("Heartbeat answered the GetStatus shape: %s", resText)
	}
	stamp, ok := res["lastExtensionHeartbeat"].(string)
	if !ok || stamp == "" {
		t.Fatalf("expected lastExtensionHeartbeat, got %s", resText)
	}
	if _, err := time.Parse("2006-01-02T15:04:05.000000Z", stamp); err != nil {
		t.Fatalf("lastExtensionHeartbeat is not an RFC3339 UTC stamp: %v", err)
	}
}

func TestLanguageServer_GetServerConfiguration(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	resText, err := d.DispatchDirect(context.Background(), "GetServerConfiguration", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect GetServerConfiguration: %v", err)
	}

	var res struct {
		Config struct {
			AppDataDir    string `json:"appDataDir"`
			SubclientType string `json:"subclientType"`
		} `json:"config"`
	}
	if err := json.Unmarshal([]byte(resText), &res); err != nil {
		t.Fatalf("Unmarshal result: %v", err)
	}

	if res.Config.AppDataDir == "" {
		t.Errorf("expected non-empty appDataDir")
	}
	if res.Config.SubclientType != "cli" {
		t.Errorf("expected subclientType=cli, got %v", res.Config.SubclientType)
	}
}

func TestLanguageServer_StatUriAndReadFile(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	tmpFile, err := os.CreateTemp("", "pigasus-ls-test-*.txt")
	if err != nil {
		t.Fatalf("CreateTemp: %v", err)
	}
	defer os.Remove(tmpFile.Name())
	_, _ = tmpFile.WriteString("hello connect rpc")
	_ = tmpFile.Close()

	uri := "file://" + filepath.ToSlash(tmpFile.Name())

	statPayload, _ := json.Marshal(map[string]any{"uri": uri})
	statResText, err := d.DispatchDirect(context.Background(), "StatUri", string(statPayload))
	if err != nil {
		t.Fatalf("DispatchDirect StatUri: %v", err)
	}
	// The reference's own answer is
	// {"fileType":"FILE_TYPE_FILE","modTime":"2026-10-26T13:47:00.907136440Z",
	//  "normalizedUri":"file:///…","size":"17"}: the enum is spelled
	// by its name and the int64 by a string.
	var statRes struct {
		FileType      string `json:"fileType"`
		Size          string `json:"size"`
		ModTime       string `json:"modTime"`
		NormalizedURI string `json:"normalizedUri"`
	}
	_ = json.Unmarshal([]byte(statResText), &statRes)
	if statRes.FileType != "FILE_TYPE_FILE" {
		t.Errorf("expected fileType FILE_TYPE_FILE, got %q", statRes.FileType)
	}
	if statRes.Size != "17" {
		t.Errorf("expected size 17, got %q", statRes.Size)
	}
	if statRes.NormalizedURI != uri {
		t.Errorf("expected normalizedUri %q, got %q", uri, statRes.NormalizedURI)
	}
	if _, err := time.Parse(time.RFC3339Nano, statRes.ModTime); err != nil {
		t.Errorf("modTime is not an RFC3339 stamp: %v", err)
	}

	readPayload, _ := json.Marshal(map[string]any{"uri": uri})
	readResText, err := d.DispatchDirect(context.Background(), "ReadFile", string(readPayload))
	if err != nil {
		t.Fatalf("DispatchDirect ReadFile: %v", err)
	}
	// `content` is a `bytes` field: proto-JSON spells it as base64, and
	// the Web UI's decoder refuses raw text.
	var readRes struct {
		Content  string `json:"content"`
		Language string `json:"language"`
	}
	_ = json.Unmarshal([]byte(readResText), &readRes)
	decoded, err := base64.StdEncoding.DecodeString(readRes.Content)
	if err != nil {
		t.Fatalf("ReadFile content is not base64: %v", err)
	}
	if string(decoded) != "hello connect rpc" {
		t.Errorf("expected 'hello connect rpc', got %q", string(decoded))
	}
	if readRes.Language != "LANGUAGE_PLAINTEXT" {
		t.Errorf("expected language LANGUAGE_PLAINTEXT, got %q", readRes.Language)
	}
}

func TestLanguageServer_AuthAndJetboxState(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	// HasAuthToken
	hasTokenText, err := d.DispatchDirect(context.Background(), "HasAuthToken", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect HasAuthToken: %v", err)
	}
	var hasTokenRes struct {
		HasToken bool `json:"hasToken"`
	}
	_ = json.Unmarshal([]byte(hasTokenText), &hasTokenRes)
	if !hasTokenRes.HasToken {
		t.Errorf("expected hasToken=true, got %v", hasTokenRes.HasToken)
	}

	// GetAuthStatus
	authStatusText, err := d.DispatchDirect(context.Background(), "GetAuthStatus", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect GetAuthStatus: %v", err)
	}
	var authStatusRes struct {
		AuthResult struct {
			HasValidAuth bool `json:"hasValidAuth"`
		} `json:"authResult"`
	}
	_ = json.Unmarshal([]byte(authStatusText), &authStatusRes)
	if !authStatusRes.AuthResult.HasValidAuth {
		t.Errorf("expected authResult.hasValidAuth=true, got %v", authStatusRes.AuthResult.HasValidAuth)
	}

	// JetboxSubscribeToState
	stateText, err := d.DispatchDirect(context.Background(), "JetboxSubscribeToState", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect JetboxSubscribeToState: %v", err)
	}
	var stateRes struct {
		AppState struct {
			// The enum is spelled by its NAME: proto-JSON's rule, and
			// the reference's own app state says
			// "AGENT_ONBOARDING_STATE_COMPLETED".
			AgentOnboardingCompleted string `json:"agentOnboardingCompleted"`
		} `json:"appState"`
	}
	_ = json.Unmarshal([]byte(stateText), &stateRes)
	if stateRes.AppState.AgentOnboardingCompleted != "AGENT_ONBOARDING_STATE_COMPLETED" {
		t.Errorf("expected agentOnboardingCompleted=AGENT_ONBOARDING_STATE_COMPLETED, got %q",
			stateRes.AppState.AgentOnboardingCompleted)
	}

	// GetUserStatus
	userStatusText, err := d.DispatchDirect(context.Background(), "GetUserStatus", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect GetUserStatus: %v", err)
	}
	var userStatusRes struct {
		UserStatus struct {
			CascadeModelConfigData struct {
				ClientModelConfigs []struct {
					Label string `json:"label"`
				} `json:"clientModelConfigs"`
			} `json:"cascadeModelConfigData"`
		} `json:"userStatus"`
	}
	_ = json.Unmarshal([]byte(userStatusText), &userStatusRes)
	configs := userStatusRes.UserStatus.CascadeModelConfigData.ClientModelConfigs
	if len(configs) == 0 {
		t.Errorf("expected clientModelConfigs to be populated, got 0")
	}
}

// TestLanguageServer_GetUpdateStatus checks that the update status is
// the message the proto declares: exa.host_bridge_pb.UpdateStatus, with
// `current_version` and `latest_version`. The Web UI's update check
// reads e.status.currentVersion, so a string there breaks the decoder.
func TestLanguageServer_GetUpdateStatus(t *testing.T) {
	sess := NewSessionState("1.4.7")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	resText, err := d.DispatchDirect(context.Background(), "GetUpdateStatus", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect GetUpdateStatus: %v", err)
	}

	var res struct {
		Status struct {
			CurrentVersion string `json:"currentVersion"`
			LatestVersion  string `json:"latestVersion"`
		} `json:"status"`
	}
	if err := json.Unmarshal([]byte(resText), &res); err != nil {
		t.Fatalf("Unmarshal result: %v", err)
	}
	if res.Status.CurrentVersion != "1.4.7" {
		t.Errorf("expected currentVersion=1.4.7, got %q", res.Status.CurrentVersion)
	}
	if res.Status.LatestVersion != "1.4.7" {
		t.Errorf("expected latestVersion=1.4.7, got %q", res.Status.LatestVersion)
	}
}

// TestLanguageServer_ProjectUpdatesStream checks the oneof's wire
// form: proto-JSON spells a oneof by its member, so the frame is
// {"projectList":{…}}. The {case, value} envelope is what
// protobuf-es decodes the frame into — the Web UI reads
// frame.update.case — and the decoder drops it on the wire.
func TestLanguageServer_ProjectUpdatesStream(t *testing.T) {
	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	resText, err := d.DispatchDirect(context.Background(), "projectUpdatesStream", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect projectUpdatesStream: %v", err)
	}

	var res map[string]any
	if err := json.Unmarshal([]byte(resText), &res); err != nil {
		t.Fatalf("Unmarshal result: %v", err)
	}
	if _, ok := res["update"]; ok {
		t.Fatalf("the oneof was spelled as a {case, value} envelope: %s", resText)
	}
	if _, ok := res["projectList"]; !ok {
		t.Fatalf("expected projectList, got %s", resText)
	}
}

func TestLanguageServer_FetchUserInfo_Default(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tmp)

	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	resText, err := d.DispatchDirect(context.Background(), "FetchUserInfo", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect FetchUserInfo: %v", err)
	}

	var res struct {
		UserSettings struct {
			TelemetryEnabled                bool `json:"telemetryEnabled"`
			UserDataCollectionForceDisabled bool `json:"userDataCollectionForceDisabled"`
			MarketingEmailsEnabled          bool `json:"marketingEmailsEnabled"`
		} `json:"userSettings"`
	}
	if err := json.Unmarshal([]byte(resText), &res); err != nil {
		t.Fatalf("Unmarshal result: %v", err)
	}

	if !res.UserSettings.TelemetryEnabled {
		t.Errorf("expected telemetryEnabled=true by default, got %v", res.UserSettings.TelemetryEnabled)
	}
	if res.UserSettings.UserDataCollectionForceDisabled {
		t.Errorf("expected userDataCollectionForceDisabled=false, got %v", res.UserSettings.UserDataCollectionForceDisabled)
	}
	if res.UserSettings.MarketingEmailsEnabled {
		t.Errorf("expected marketingEmailsEnabled=false by default, got %v", res.UserSettings.MarketingEmailsEnabled)
	}
}

func TestLanguageServer_SetUserInfo_PersistAndFetch(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tmp)

	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	// 1. Mutate settings: disable telemetry, enable marketing emails
	setReq := `{"userSettings":{"telemetryEnabled":false,"marketingEmailsEnabled":true,"userDataCollectionForceDisabled":false}}`
	setRespText, err := d.DispatchDirect(context.Background(), "SetUserInfo", setReq)
	if err != nil {
		t.Fatalf("DispatchDirect SetUserInfo: %v", err)
	}

	var setResp struct {
		UserSettings struct {
			TelemetryEnabled                bool `json:"telemetryEnabled"`
			UserDataCollectionForceDisabled bool `json:"userDataCollectionForceDisabled"`
			MarketingEmailsEnabled          bool `json:"marketingEmailsEnabled"`
		} `json:"userSettings"`
	}
	if err := json.Unmarshal([]byte(setRespText), &setResp); err != nil {
		t.Fatalf("Unmarshal SetUserInfo result: %v", err)
	}

	if setResp.UserSettings.TelemetryEnabled {
		t.Errorf("expected telemetryEnabled=false after SetUserInfo, got true")
	}
	if !setResp.UserSettings.MarketingEmailsEnabled {
		t.Errorf("expected marketingEmailsEnabled=true after SetUserInfo, got false")
	}

	// 2. FetchUserInfo should return the updated settings
	fetchRespText, err := d.DispatchDirect(context.Background(), "FetchUserInfo", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect FetchUserInfo: %v", err)
	}
	var fetchResp struct {
		UserSettings struct {
			TelemetryEnabled                bool `json:"telemetryEnabled"`
			UserDataCollectionForceDisabled bool `json:"userDataCollectionForceDisabled"`
			MarketingEmailsEnabled          bool `json:"marketingEmailsEnabled"`
		} `json:"userSettings"`
	}
	if err := json.Unmarshal([]byte(fetchRespText), &fetchResp); err != nil {
		t.Fatalf("Unmarshal FetchUserInfo result: %v", err)
	}
	if fetchResp.UserSettings.TelemetryEnabled != false || fetchResp.UserSettings.MarketingEmailsEnabled != true {
		t.Errorf("FetchUserInfo mismatch: %+v", fetchResp.UserSettings)
	}

	// 3. Verify settings.json on disk contains the persisted choices
	settingsFile := filepath.Join(tmp, "settings.json")
	data, err := os.ReadFile(settingsFile)
	if err != nil {
		t.Fatalf("ReadFile settings.json: %v", err)
	}
	var onDisk map[string]any
	if err := json.Unmarshal(data, &onDisk); err != nil {
		t.Fatalf("Unmarshal disk settings.json: %v", err)
	}
	if onDisk["telemetryEnabled"] != false {
		t.Errorf("on disk telemetryEnabled = %v, want false", onDisk["telemetryEnabled"])
	}
	if onDisk["marketingEmailsEnabled"] != true {
		t.Errorf("on disk marketingEmailsEnabled = %v, want true", onDisk["marketingEmailsEnabled"])
	}

	// 4. Fresh dispatcher (simulating restart) loads persisted settings
	d2, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher 2: %v", err)
	}
	fetch2Text, err := d2.DispatchDirect(context.Background(), "fetchUserInfo", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect fetchUserInfo: %v", err)
	}
	var fetch2Resp struct {
		UserSettings struct {
			TelemetryEnabled       bool `json:"telemetryEnabled"`
			MarketingEmailsEnabled bool `json:"marketingEmailsEnabled"`
		} `json:"userSettings"`
	}
	if err := json.Unmarshal([]byte(fetch2Text), &fetch2Resp); err != nil {
		t.Fatalf("Unmarshal fetchUserInfo result: %v", err)
	}
	if fetch2Resp.UserSettings.TelemetryEnabled != false || fetch2Resp.UserSettings.MarketingEmailsEnabled != true {
		t.Errorf("Restarted dispatcher mismatch: %+v", fetch2Resp.UserSettings)
	}
}

func TestLanguageServer_SetUserInfo_BusinessAccount(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tmp)
	t.Setenv("AGY_ADC_AUTH", "1")

	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	// Business account must have telemetry disabled & forced
	fetchRespText, err := d.DispatchDirect(context.Background(), "FetchUserInfo", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect FetchUserInfo: %v", err)
	}
	var fetchResp struct {
		UserSettings struct {
			TelemetryEnabled                bool `json:"telemetryEnabled"`
			UserDataCollectionForceDisabled bool `json:"userDataCollectionForceDisabled"`
		} `json:"userSettings"`
	}
	if err := json.Unmarshal([]byte(fetchRespText), &fetchResp); err != nil {
		t.Fatalf("Unmarshal FetchUserInfo result: %v", err)
	}
	if fetchResp.UserSettings.TelemetryEnabled {
		t.Errorf("business account: expected telemetryEnabled=false, got true")
	}
	if !fetchResp.UserSettings.UserDataCollectionForceDisabled {
		t.Errorf("business account: expected userDataCollectionForceDisabled=true, got false")
	}

	// Attempting to enable telemetry should be ignored for business accounts
	setReq := `{"userSettings":{"telemetryEnabled":true}}`
	setRespText, err := d.DispatchDirect(context.Background(), "SetUserInfo", setReq)
	if err != nil {
		t.Fatalf("DispatchDirect SetUserInfo: %v", err)
	}
	var setResp struct {
		UserSettings struct {
			TelemetryEnabled                bool `json:"telemetryEnabled"`
			UserDataCollectionForceDisabled bool `json:"userDataCollectionForceDisabled"`
		} `json:"userSettings"`
	}
	if err := json.Unmarshal([]byte(setRespText), &setResp); err != nil {
		t.Fatalf("Unmarshal SetUserInfo result: %v", err)
	}
	if setResp.UserSettings.TelemetryEnabled {
		t.Errorf("business account: telemetryEnabled should remain false, got true")
	}
	if !setResp.UserSettings.UserDataCollectionForceDisabled {
		t.Errorf("business account: userDataCollectionForceDisabled should remain true, got false")
	}
}

func TestLanguageServer_GetUserStatus_TelemetryAlignment(t *testing.T) {
	tmp := t.TempDir()
	t.Setenv("ANTIGRAVITY_HOME", tmp)

	sess := NewSessionState("test")
	d, err := NewDispatcher(Config{Session: sess})
	if err != nil {
		t.Fatalf("NewDispatcher: %v", err)
	}

	// Default: telemetry enabled -> disableTelemetry = false
	statusText, err := d.DispatchDirect(context.Background(), "GetUserStatus", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect GetUserStatus: %v", err)
	}
	var status1 struct {
		UserStatus struct {
			DisableTelemetry                bool `json:"disableTelemetry"`
			UserDataCollectionForceDisabled bool `json:"userDataCollectionForceDisabled"`
		} `json:"userStatus"`
	}
	if err := json.Unmarshal([]byte(statusText), &status1); err != nil {
		t.Fatalf("Unmarshal GetUserStatus: %v", err)
	}
	if status1.UserStatus.DisableTelemetry {
		t.Errorf("default: expected disableTelemetry=false, got true")
	}

	// Disable telemetry via SetUserInfo
	_, err = d.DispatchDirect(context.Background(), "SetUserInfo", `{"userSettings":{"telemetryEnabled":false}}`)
	if err != nil {
		t.Fatalf("SetUserInfo: %v", err)
	}

	// Now GetUserStatus should report disableTelemetry = true
	statusText2, err := d.DispatchDirect(context.Background(), "GetUserStatus", "{}")
	if err != nil {
		t.Fatalf("DispatchDirect GetUserStatus 2: %v", err)
	}
	var status2 struct {
		UserStatus struct {
			DisableTelemetry bool `json:"disableTelemetry"`
		} `json:"userStatus"`
	}
	if err := json.Unmarshal([]byte(statusText2), &status2); err != nil {
		t.Fatalf("Unmarshal GetUserStatus 2: %v", err)
	}
	if !status2.UserStatus.DisableTelemetry {
		t.Errorf("after disable: expected disableTelemetry=true, got false")
	}
}
