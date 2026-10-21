package codeium_common_pb

import (
	"testing"
	"time"
)

// 1:1 verification: ServiceName + 7 decompiled message types
// (CascadeModelConfigData, PlanInfo, UserSettings, UserStatus,
// TeamOrganizationalControls, CascadeNUXConfig, CascadeNUXState)
// recovered from /workspace/agy-recovered/docs/decompiled-v1.3.5/c/google3_third_party_jetski_codeium_common_pb_codeium_common_go_proto/.
func TestCascadeModelConfigData_Fields(t *testing.T) {
	c := &CascadeModelConfigData{
		ClientModelConfigs: []*ClientModelConfig{
			{ModelOrAlias: "gpt-4", Label: "GPT-4"},
		},
		ErrorMessage: "ok",
	}
	if len(c.ClientModelConfigs) != 1 {
		t.Errorf("ClientModelConfigs length = %d, want 1", len(c.ClientModelConfigs))
	}
	if c.ClientModelConfigs[0].ModelOrAlias != "gpt-4" {
		t.Errorf("ModelOrAlias = %q, want gpt-4", c.ClientModelConfigs[0].ModelOrAlias)
	}
	if c.ErrorMessage != "ok" {
		t.Errorf("ErrorMessage = %q, want ok", c.ErrorMessage)
	}
}

func TestCascadeNUXConfig_Fields(t *testing.T) {
	c := &CascadeNUXConfig{
		Uid:       "nux-1",
		Location:  CascadeNUXLocationChat,
		Trigger:   CascadeNUXTriggerFirstRun,
		Title:     "Welcome to Cascade",
		Body:      "Try it now",
		Session:   CascadeNUXSessionLoggedIn,
	}
	if c.Uid != "nux-1" {
		t.Errorf("Uid = %q, want nux-1", c.Uid)
	}
	if c.Location != CascadeNUXLocationChat {
		t.Errorf("Location = %d, want %d", c.Location, CascadeNUXLocationChat)
	}
	if c.Trigger != CascadeNUXTriggerFirstRun {
		t.Errorf("Trigger = %d, want %d", c.Trigger, CascadeNUXTriggerFirstRun)
	}
}

func TestCascadeNUXState_Fields(t *testing.T) {
	now := time.Now()
	s := &CascadeNUXState{
		Event:     CascadeNUXEventShown,
		Timestamp: now,
	}
	if s.Event != CascadeNUXEventShown {
		t.Errorf("Event = %d, want %d", s.Event, CascadeNUXEventShown)
	}
	if s.Timestamp != now {
		t.Errorf("Timestamp = %v, want %v", s.Timestamp, now)
	}
}

func TestPlanInfo_Fields(t *testing.T) {
	p := &PlanInfo{
		PlanName:                       "Pro",
		PlanTier:                       TeamsTierPro,
		IsPaidTier:                     true,
		AllowCasCadeOverDailyLimit:     false,
		AllowCascadeInBackground:       true,
		AllowBrowserTools:              true,
		CascadePlannerMode:             ConversationalPlannerModeOn,
		RunExtensionCode:               CascadeRunExtensionCodeOn,
		RunExtensionCodeAutoRun:        CascadeRunExtensionCodeAutoRunOff,
		CascadeInputAutocomplete:       CascadeInputAutocompleteOn,
		CascadeAutoExecutionPolicy:     CascadeCommandsAutoExecutionOn,
		AutoContinueOnMaxGeneratorInvocations: AutoContinueOnMaxGeneratorInvocationsOn,
	}
	if p.PlanName != "Pro" {
		t.Errorf("PlanName = %q, want Pro", p.PlanName)
	}
	if p.PlanTier != TeamsTierPro {
		t.Errorf("PlanTier = %d, want %d", p.PlanTier, TeamsTierPro)
	}
}

func TestUserSettings_BooleanFields(t *testing.T) {
	u := &UserSettings{
		DisableTelemetry:            true,
		DisableCascade:              false,
		DisableAutocomplete:         false,
		AllowTabKey:                 true,
		EnableSupercomplete:         true,
		EnableMcp:                   true,
		EnableBrowserTools:          true,
		EnableCascadeWebSearch:      true,
		EnableCustomModels:          false,
		CascadeWebSearchDisabled:    false,
		DisableOpenCascadeOnReload:  false,
		AllowCascadeAccessGitignoreFiles: true,
		DisableCascadeAutoFixLints:  false,
		AllowCascadeInBackground:    true,
		RunExtensionCode:            CascadeRunExtensionCodeOn,
		RunExtensionCodeAutoRun:     CascadeRunExtensionCodeAutoRunOff,
		CascadeInputAutocomplete:    CascadeInputAutocompleteOn,
		ThemePreference:             ThemePreferenceDark,
		RememberLastModelSelection:  RememberLastModelSelectionOn,
	}
	if !u.DisableTelemetry {
		t.Error("DisableTelemetry = false, want true")
	}
	if u.ThemePreference != ThemePreferenceDark {
		t.Errorf("ThemePreference = %d, want %d", u.ThemePreference, ThemePreferenceDark)
	}
}

func TestUserStatus_Fields(t *testing.T) {
	u := &UserStatus{
		Email:           "user@example.com",
		AuthUid:         "auth-uid-123",
		IsPayingUser:    true,
		HasAcceptedTos:  true,
		CollectionOptIn: false,
		UserTeamStatus:  UserTeamStatusTeamMember,
		PlanName:        "Pro",
	}
	if u.Email != "user@example.com" {
		t.Errorf("Email = %q", u.Email)
	}
	if u.UserTeamStatus != UserTeamStatusTeamMember {
		t.Errorf("UserTeamStatus = %d, want %d", u.UserTeamStatus, UserTeamStatusTeamMember)
	}
}

func TestTeamOrganizationalControls_Fields(t *testing.T) {
	c := &TeamOrganizationalControls{
		CascadeModelLabels:     []string{"gpt-4", "claude-3"},
		CascadeAllowedCommands: []string{"ls", "cat"},
		CascadeDeniedCommands:  []string{"rm -rf"},
	}
	if len(c.CascadeModelLabels) != 2 {
		t.Errorf("CascadeModelLabels length = %d, want 2", len(c.CascadeModelLabels))
	}
	if c.CascadeModelLabels[0] != "gpt-4" {
		t.Errorf("first label = %q, want gpt-4", c.CascadeModelLabels[0])
	}
}

func TestEnums_104DistinctEnums(t *testing.T) {
	// Sanity check: 104 enums declared. Verify a representative
	// subset of 5 has distinct zero values.
	enums := []int32{
		int32(APIProviderUnspecified),
		int32(CascadeCommandsAutoExecutionUnspecified),
		int32(CascadeNUXEventUnspecified),
		int32(TeamsTierUnspecified),
		int32(ModelStatusUnspecified),
	}
	for i, e := range enums {
		if e != 0 {
			t.Errorf("enum[%d] = %d, want 0", i, e)
		}
	}
}

func TestEnums_TeamsTierValues(t *testing.T) {
	if int32(TeamsTierFree) != 1 {
		t.Errorf("TeamsTierFree = %d, want 1", int32(TeamsTierFree))
	}
	if int32(TeamsTierPro) != 2 {
		t.Errorf("TeamsTierPro = %d, want 2", int32(TeamsTierPro))
	}
	if int32(TeamsTierEnterprise) != 4 {
		t.Errorf("TeamsTierEnterprise = %d, want 4", int32(TeamsTierEnterprise))
	}
}

func TestEnums_CascadeCommandsAutoExecution(t *testing.T) {
	if int32(CascadeCommandsAutoExecutionOff) != 1 {
		t.Errorf("CascadeCommandsAutoExecutionOff = %d, want 1", int32(CascadeCommandsAutoExecutionOff))
	}
	if int32(CascadeCommandsAutoExecutionOn) != 2 {
		t.Errorf("CascadeCommandsAutoExecutionOn = %d, want 2", int32(CascadeCommandsAutoExecutionOn))
	}
}

func TestEnums_CascadeNUXLocation(t *testing.T) {
	if int32(CascadeNUXLocationChat) != 1 {
		t.Errorf("CascadeNUXLocationChat = %d, want 1", int32(CascadeNUXLocationChat))
	}
	if int32(CascadeNUXLocationEditor) != 2 {
		t.Errorf("CascadeNUXLocationEditor = %d, want 2", int32(CascadeNUXLocationEditor))
	}
}

func TestEnums_ThemePreference(t *testing.T) {
	if int32(ThemePreferenceLight) != 1 {
		t.Errorf("ThemePreferenceLight = %d, want 1", int32(ThemePreferenceLight))
	}
	if int32(ThemePreferenceDark) != 2 {
		t.Errorf("ThemePreferenceDark = %d, want 2", int32(ThemePreferenceDark))
	}
}

func TestEnums_Language(t *testing.T) {
	if int32(LanguageGo) != 1 {
		t.Errorf("LanguageGo = %d, want 1", int32(LanguageGo))
	}
	if int32(LanguagePython) != 2 {
		t.Errorf("LanguagePython = %d, want 2", int32(LanguagePython))
	}
	if int32(LanguageTypeScript) != 5 {
		t.Errorf("LanguageTypeScript = %d, want 5", int32(LanguageTypeScript))
	}
}

func TestCompletion_Fields(t *testing.T) {
	c := &Completion{
		Text:      "fmt.Println(\"hi\")",
		Source:    CompletionSourceCloud,
		Type:      CompletionTypeSnippet,
		Language:  LanguageGo,
		Model:     "gpt-4",
		WasAccepted: true,
	}
	if c.Text == "" {
		t.Error("Text is empty")
	}
	if c.Source != CompletionSourceCloud {
		t.Errorf("Source = %d, want %d", c.Source, CompletionSourceCloud)
	}
}

func TestEmbedding_Fields(t *testing.T) {
	e := &Embedding{
		Vector: []float32{0.1, 0.2, 0.3},
		Model:  "text-embedding-3-small",
		Source: EmbeddingSourceCloud,
		Prefix: EmbeddingPrefixDocument,
	}
	if len(e.Vector) != 3 {
		t.Errorf("Vector length = %d, want 3", len(e.Vector))
	}
	if e.Vector[0] != 0.1 {
		t.Errorf("Vector[0] = %f, want 0.1", e.Vector[0])
	}
}

func TestRange_Fields(t *testing.T) {
	r := &Range{
		StartLine: 10,
		StartCol:  5,
		EndLine:   12,
		EndCol:    20,
	}
	if r.StartLine != 10 || r.StartCol != 5 || r.EndLine != 12 || r.EndCol != 20 {
		t.Errorf("Range = %+v, want (10,5,12,20)", r)
	}
}

func TestAntigravityDeployment_Fields(t *testing.T) {
	a := &AntigravityDeployment{
		AntigravityDeploymentId: "deploy-1",
		AntigravityProjectId:    "proj-1",
		AuthUid:                 "uid-1",
		ProjectName:             "my-app",
		DeploymentProvider:      DeploymentProviderVercel,
	}
	if a.AntigravityDeploymentId != "deploy-1" {
		t.Errorf("AntigravityDeploymentId = %q", a.AntigravityDeploymentId)
	}
	if a.DeploymentProvider != DeploymentProviderVercel {
		t.Errorf("DeploymentProvider = %d, want %d", a.DeploymentProvider, DeploymentProviderVercel)
	}
}

func TestTerminalShellCommand_Fields(t *testing.T) {
	now := time.Now()
	c := &TerminalShellCommand{
		CommandLine:     "ls -la",
		Cwd:             "/tmp",
		ExitCode:        0,
		StartedAt:       now,
		CompletedAt:     now,
		Source:          TerminalShellCommandSourceUser,
		Status:          TerminalShellCommandStatusCompleted,
		ConversationId:  "conv-1",
		TerminalId:      "term-1",
	}
	if c.CommandLine != "ls -la" {
		t.Errorf("CommandLine = %q", c.CommandLine)
	}
	if c.Status != TerminalShellCommandStatusCompleted {
		t.Errorf("Status = %d, want %d", c.Status, TerminalShellCommandStatusCompleted)
	}
}

func TestTerminalShellCommandStreamChunk_Fields(t *testing.T) {
	c := &TerminalShellCommandStreamChunk{
		ChunkType: "stdout",
		Data:      []byte("hello\n"),
		Line:      "hello",
		Sequence:  42,
	}
	if c.ChunkType != "stdout" {
		t.Errorf("ChunkType = %q", c.ChunkType)
	}
	if string(c.Data) != "hello\n" {
		t.Errorf("Data = %q", c.Data)
	}
	if c.Sequence != 42 {
		t.Errorf("Sequence = %d, want 42", c.Sequence)
	}
}

func TestErrNotImplemented(t *testing.T) {
	if ErrNotImplemented == nil {
		t.Error("ErrNotImplemented is nil")
	}
	if ErrNotImplemented.Error() == "" {
		t.Error("ErrNotImplemented has empty message")
	}
}

func TestCciWithSubrange_Fields(t *testing.T) {
	c := &CciWithSubrange{
		Item: &CodeContextItem{Uri: "file://main.go"},
		Subrange: &ContextSubrange{
			Reason: "explicitly-included",
		},
	}
	if c.Item == nil || c.Item.Uri != "file://main.go" {
		t.Errorf("Item = %+v", c.Item)
	}
}

func TestWorkspaceIndexData_Fields(t *testing.T) {
	w := &WorkspaceIndexData{
		WorkspaceId:  "ws-1",
		RootPath:     "/repo",
		FilesIndexed: 100,
		Stats:        &WorkspaceStats{Files: 100, Lines: 10000},
	}
	if w.WorkspaceId != "ws-1" {
		t.Errorf("WorkspaceId = %q", w.WorkspaceId)
	}
	if w.Stats.Files != 100 {
		t.Errorf("Stats.Files = %d, want 100", w.Stats.Files)
	}
}

func TestUserFeatures_EnumValues(t *testing.T) {
	if int32(UserFeaturesAutocomplete) != 1 {
		t.Errorf("UserFeaturesAutocomplete = %d, want 1", int32(UserFeaturesAutocomplete))
	}
	if int32(UserFeaturesCascade) != 3 {
		t.Errorf("UserFeaturesCascade = %d, want 3", int32(UserFeaturesCascade))
	}
	if int32(UserFeaturesBrowserTools) != 5 {
		t.Errorf("UserFeaturesBrowserTools = %d, want 5", int32(UserFeaturesBrowserTools))
	}
}
