// Package extension_server_pb — secondary message-type file.
//
// Most message structs live in extension_server.go (alongside the
// service interface, matching the internal/language_server_pb style).
// This file collects the remaining recovered types that don't fit into
// the request/response shape — nested helpers, terminal-stream chunks,
// and the recovered generated getter signatures.
//
// All protobuf tags are assigned 1:1 from the v1.3.5 binary field
// numbers in pclntab 0x7313000-0x732a000.
package extension_server_pb

import "time"

// ---------------------------------------------------------------------------
// Recovered nested message types not in extension_server.go.
// ---------------------------------------------------------------------------

// GetDefinitionRequestItem is a single (file_uri, position, line_content) item.
// (Also declared in extension_server.go; redeclared here for the
// 1:1 "every recovered type in its own file" mapping.)
//
// In the v1.3.5 binary the message is referenced as
// extension_server_go_proto.(*GetDefinitionRequestItem) with three
// getters: GetFileUri / GetPosition / GetLineContent.
type _GetDefinitionRequestItem = GetDefinitionRequestItem

// GetDefinitionResponseItem is the resolved definition list for one
// request item.
type _GetDefinitionResponseItem = GetDefinitionResponseItem

// GetLintErrorsRequest_AwaitNewLintsConfig is the nested config for
// awaiting new lint errors.
type _GetLintErrorsRequest_AwaitNewLintsConfig = GetLintErrorsRequest_AwaitNewLintsConfig

// EditNotebookRequest_Cell is a single cell edit.
type _EditNotebookRequest_Cell = EditNotebookRequest_Cell

// EditNotebookResponse_ModifiedCell is the cell diff returned.
type _EditNotebookResponse_ModifiedCell = EditNotebookResponse_ModifiedCell

// ExecuteNotebookResponse_CellOutput is the per-cell output entry.
type _ExecuteNotebookResponse_CellOutput = ExecuteNotebookResponse_CellOutput

// UnifiedStateSyncUpdate is a server-streamed message.
type _UnifiedStateSyncUpdate = UnifiedStateSyncUpdate

// ---------------------------------------------------------------------------
// Recovered 1:1 Get<Field> methods (one per field, per v1.3.5 binary).
//
// These thin accessors match the protobuf-generated Get<Field>() method
// pattern. They are no-ops that return the field value so that callers
// can use the recovered 1:1 API surface.
// ---------------------------------------------------------------------------

// IsAgentManagerEnabledResponse.Enabled getter.
func (x *IsAgentManagerEnabledResponse) GetEnabled() bool {
	if x == nil {
		return false
	}
	return x.Enabled
}

// HeartbeatResponse.ServerTime getter.
func (x *HeartbeatResponse) GetServerTime() time.Time {
	if x == nil {
		return time.Time{}
	}
	return x.ServerTime
}

// LanguageServerStartedRequest.HttpPort getter.
func (x *LanguageServerStartedRequest) GetHttpPort() int32 {
	if x == nil {
		return 0
	}
	return x.HttpPort
}

// LanguageServerStartedRequest.HttpsPort getter.
func (x *LanguageServerStartedRequest) GetHttpsPort() int32 {
	if x == nil {
		return 0
	}
	return x.HttpsPort
}

// LanguageServerStartedRequest.LspPort getter.
func (x *LanguageServerStartedRequest) GetLspPort() int32 {
	if x == nil {
		return 0
	}
	return x.LspPort
}

// LanguageServerStartedRequest.CsrfToken getter.
func (x *LanguageServerStartedRequest) GetCsrfToken() string {
	if x == nil {
		return ""
	}
	return x.CsrfToken
}

// LaunchBrowserRequest.ExtensionPath getter.
func (x *LaunchBrowserRequest) GetExtensionPath() string {
	if x == nil {
		return ""
	}
	return x.ExtensionPath
}

// LaunchBrowserRequest.LaunchInForeground getter.
func (x *LaunchBrowserRequest) GetLaunchInForeground() bool {
	if x == nil {
		return false
	}
	return x.LaunchInForeground
}

// LaunchBrowserResponse.CdpAddress getter.
func (x *LaunchBrowserResponse) GetCdpAddress() string {
	if x == nil {
		return ""
	}
	return x.CdpAddress
}

// GetBrowserOnboardingPortResponse.Port getter.
func (x *GetBrowserOnboardingPortResponse) GetPort() int32 {
	if x == nil {
		return 0
	}
	return x.Port
}

// GetChromeDevtoolsMcpUrlResponse.Url getter.
func (x *GetChromeDevtoolsMcpUrlResponse) GetUrl() string {
	if x == nil {
		return ""
	}
	return x.Url
}

// OpenExternalUrlRequest.Url getter.
func (x *OpenExternalUrlRequest) GetUrl() string {
	if x == nil {
		return ""
	}
	return x.Url
}

// OpenExternalUrlRequest.UseIdeBrowser getter.
func (x *OpenExternalUrlRequest) GetUseIdeBrowser() bool {
	if x == nil {
		return false
	}
	return x.UseIdeBrowser
}

// CheckTerminalShellSupportResponse.HasShellIntegration getter.
func (x *CheckTerminalShellSupportResponse) GetHasShellIntegration() bool {
	if x == nil {
		return false
	}
	return x.HasShellIntegration
}

// CheckTerminalShellSupportResponse.ShellName getter.
func (x *CheckTerminalShellSupportResponse) GetShellName() string {
	if x == nil {
		return ""
	}
	return x.ShellName
}

// CheckTerminalShellSupportResponse.ShellPath getter.
func (x *CheckTerminalShellSupportResponse) GetShellPath() string {
	if x == nil {
		return ""
	}
	return x.ShellPath
}

// OpenTerminalRequest.ProcessId getter.
func (x *OpenTerminalRequest) GetProcessId() int64 {
	if x == nil {
		return 0
	}
	return x.ProcessId
}

// ShowTerminalRequest.TerminalId getter.
func (x *ShowTerminalRequest) GetTerminalId() string {
	if x == nil {
		return ""
	}
	return x.TerminalId
}

// ReadTerminalRequest.ConversationId getter.
func (x *ReadTerminalRequest) GetConversationId() string {
	if x == nil {
		return ""
	}
	return x.ConversationId
}

// ReadTerminalRequest.ProcessId getter.
func (x *ReadTerminalRequest) GetProcessId() int64 {
	if x == nil {
		return 0
	}
	return x.ProcessId
}

// ReadTerminalResponse.Contents getter.
func (x *ReadTerminalResponse) GetContents() string {
	if x == nil {
		return ""
	}
	return x.Contents
}

// SendTerminalInputRequest.ConversationId getter.
func (x *SendTerminalInputRequest) GetConversationId() string {
	if x == nil {
		return ""
	}
	return x.ConversationId
}

// SendTerminalInputRequest.TerminalId getter.
func (x *SendTerminalInputRequest) GetTerminalId() string {
	if x == nil {
		return ""
	}
	return x.TerminalId
}

// SendTerminalInputRequest.Input getter.
func (x *SendTerminalInputRequest) GetInput() string {
	if x == nil {
		return ""
	}
	return x.Input
}

// SendTerminalInputResponse.Success getter.
func (x *SendTerminalInputResponse) GetSuccess() bool {
	if x == nil {
		return false
	}
	return x.Success
}

// SendTerminalInputResponse.Error getter.
func (x *SendTerminalInputResponse) GetError() string {
	if x == nil {
		return ""
	}
	return x.Error
}

// TerminateCommandRequest.ConversationId getter.
func (x *TerminateCommandRequest) GetConversationId() string {
	if x == nil {
		return ""
	}
	return x.ConversationId
}

// TerminateCommandRequest.TerminalId getter.
func (x *TerminateCommandRequest) GetTerminalId() string {
	if x == nil {
		return ""
	}
	return x.TerminalId
}

// ExecuteCommandRequest.CommandLine getter.
func (x *ExecuteCommandRequest) GetCommandLine() string {
	if x == nil {
		return ""
	}
	return x.CommandLine
}

// ExecuteCommandRequest.Cwd getter.
func (x *ExecuteCommandRequest) GetCwd() string {
	if x == nil {
		return ""
	}
	return x.Cwd
}

// ExecuteCommandRequest.Env getter.
func (x *ExecuteCommandRequest) GetEnv() map[string]string {
	if x == nil {
		return nil
	}
	return x.Env
}

// ExecuteCommandRequest.SandboxOverride getter.
func (x *ExecuteCommandRequest) GetSandboxOverride() string {
	if x == nil {
		return ""
	}
	return x.SandboxOverride
}

// ExecuteCommandRequest.TerminalId getter.
func (x *ExecuteCommandRequest) GetTerminalId() string {
	if x == nil {
		return ""
	}
	return x.TerminalId
}

// ExecuteCommandRequest.TerminalHost getter.
func (x *ExecuteCommandRequest) GetTerminalHost() string {
	if x == nil {
		return ""
	}
	return x.TerminalHost
}

// ExecuteCommandRequest.ConversationId getter.
func (x *ExecuteCommandRequest) GetConversationId() string {
	if x == nil {
		return ""
	}
	return x.ConversationId
}

// TerminalResearchResultRequest.LastStep getter.
func (x *TerminalResearchResultRequest) GetLastStep() int32 {
	if x == nil {
		return 0
	}
	return x.LastStep
}

// RunExtensionCodeRequest.Code getter.
func (x *RunExtensionCodeRequest) GetCode() string {
	if x == nil {
		return ""
	}
	return x.Code
}

// RunExtensionCodeResponse.Output getter.
func (x *RunExtensionCodeResponse) GetOutput() string {
	if x == nil {
		return ""
	}
	return x.Output
}

// OpenFilePointerRequest.FileUri getter.
func (x *OpenFilePointerRequest) GetFileUri() string {
	if x == nil {
		return ""
	}
	return x.FileUri
}

// OpenFilePointerRequest.StartLine getter.
func (x *OpenFilePointerRequest) GetStartLine() int32 {
	if x == nil {
		return 0
	}
	return x.StartLine
}

// OpenFilePointerRequest.StartCol getter.
func (x *OpenFilePointerRequest) GetStartCol() int32 {
	if x == nil {
		return 0
	}
	return x.StartCol
}

// OpenFilePointerRequest.EndLine getter.
func (x *OpenFilePointerRequest) GetEndLine() int32 {
	if x == nil {
		return 0
	}
	return x.EndLine
}

// OpenFilePointerRequest.EndCol getter.
func (x *OpenFilePointerRequest) GetEndCol() int32 {
	if x == nil {
		return 0
	}
	return x.EndCol
}

// OpenFilePointerRequest.IsCascadeDiff getter.
func (x *OpenFilePointerRequest) GetIsCascadeDiff() bool {
	if x == nil {
		return false
	}
	return x.IsCascadeDiff
}

// OpenVirtualFileRequest.Text getter.
func (x *OpenVirtualFileRequest) GetText() string {
	if x == nil {
		return ""
	}
	return x.Text
}

// OpenVirtualFileRequest.ShowMarkdownPreview getter.
func (x *OpenVirtualFileRequest) GetShowMarkdownPreview() bool {
	if x == nil {
		return false
	}
	return x.ShowMarkdownPreview
}

// SaveDocumentRequest.Uri getter.
func (x *SaveDocumentRequest) GetUri() string {
	if x == nil {
		return ""
	}
	return x.Uri
}

// SaveDocumentRequest.ConversationId getter.
func (x *SaveDocumentRequest) GetConversationId() string {
	if x == nil {
		return ""
	}
	return x.ConversationId
}

// InsertCodeAtCursorRequest.Text getter.
func (x *InsertCodeAtCursorRequest) GetText() string {
	if x == nil {
		return ""
	}
	return x.Text
}

// InsertCodeAtCursorRequest.EditorType getter.
func (x *InsertCodeAtCursorRequest) GetEditorType() ActiveEditorType {
	if x == nil {
		return ActiveEditorTypeUnspecified
	}
	return x.EditorType
}

// OpenDiffZonesRequest.CascadeId getter.
func (x *OpenDiffZonesRequest) GetCascadeId() string {
	if x == nil {
		return ""
	}
	return x.CascadeId
}

// OpenDiffZonesRequest.StepIndices getter.
func (x *OpenDiffZonesRequest) GetStepIndices() []int32 {
	if x == nil {
		return nil
	}
	return x.StepIndices
}

// OpenDiffZonesRequest.TraceId getter.
func (x *OpenDiffZonesRequest) GetTraceId() string {
	if x == nil {
		return ""
	}
	return x.TraceId
}

// OpenDiffZonesRequest.ValidationStates getter.
func (x *OpenDiffZonesRequest) GetValidationStates() []string {
	if x == nil {
		return nil
	}
	return x.ValidationStates
}

// OpenDiffZonesRequest.WriteNewState getter.
func (x *OpenDiffZonesRequest) GetWriteNewState() bool {
	if x == nil {
		return false
	}
	return x.WriteNewState
}

// OpenSettingRequest.SettingId getter.
func (x *OpenSettingRequest) GetSettingId() string {
	if x == nil {
		return ""
	}
	return x.SettingId
}

// OpenAntigravityRulesFileRequest.IsWorkspaceRules getter.
func (x *OpenAntigravityRulesFileRequest) GetIsWorkspaceRules() bool {
	if x == nil {
		return false
	}
	return x.IsWorkspaceRules
}

// OpenPluginPageRequest.PluginId getter.
func (x *OpenPluginPageRequest) GetPluginId() string {
	if x == nil {
		return ""
	}
	return x.PluginId
}

// OpenPluginConfigModalRequest.PluginId getter.
func (x *OpenPluginConfigModalRequest) GetPluginId() string {
	if x == nil {
		return ""
	}
	return x.PluginId
}

// OpenConversationWorkspaceQuickPickRequest.CascadeId getter.
func (x *OpenConversationWorkspaceQuickPickRequest) GetCascadeId() string {
	if x == nil {
		return ""
	}
	return x.CascadeId
}

// OpenConversationWorkspaceQuickPickRequest.WorkspaceUris getter.
func (x *OpenConversationWorkspaceQuickPickRequest) GetWorkspaceUris() []string {
	if x == nil {
		return nil
	}
	return x.WorkspaceUris
}

// OpenConversationWorkspaceQuickPickResponse.OpenInCurrentWindow getter.
func (x *OpenConversationWorkspaceQuickPickResponse) GetOpenInCurrentWindow() bool {
	if x == nil {
		return false
	}
	return x.OpenInCurrentWindow
}

// GetSecretValueRequest.Key getter.
func (x *GetSecretValueRequest) GetKey() string {
	if x == nil {
		return ""
	}
	return x.Key
}

// GetSecretValueResponse.Value getter.
func (x *GetSecretValueResponse) GetValue() string {
	if x == nil {
		return ""
	}
	return x.Value
}

// StoreSecretValueRequest.Key getter.
func (x *StoreSecretValueRequest) GetKey() string {
	if x == nil {
		return ""
	}
	return x.Key
}

// StoreSecretValueRequest.Value getter.
func (x *StoreSecretValueRequest) GetValue() string {
	if x == nil {
		return ""
	}
	return x.Value
}

// LogEventRequest.Event getter.
func (x *LogEventRequest) GetEvent() string {
	if x == nil {
		return ""
	}
	return x.Event
}

// LogEventRequest.EventString getter.
func (x *LogEventRequest) GetEventString() string {
	if x == nil {
		return ""
	}
	return x.EventString
}

// LogEventRequest.Metadata getter.
func (x *LogEventRequest) GetMetadata() []*EventMetadataRecord {
	if x == nil {
		return nil
	}
	return x.Metadata
}

// EventMetadataRecord.Key getter.
func (x *EventMetadataRecord) GetKey() string {
	if x == nil {
		return ""
	}
	return x.Key
}

// EventMetadataRecord.Value getter.
func (x *EventMetadataRecord) GetValue() string {
	if x == nil {
		return ""
	}
	return x.Value
}

// RecordErrorRequest.ErrorTrace getter.
func (x *RecordErrorRequest) GetErrorTrace() string {
	if x == nil {
		return ""
	}
	return x.ErrorTrace
}

// HandleAsyncPostMessageRequest.MessageType getter.
func (x *HandleAsyncPostMessageRequest) GetMessageType() string {
	if x == nil {
		return ""
	}
	return x.MessageType
}

// HandleAsyncPostMessageRequest.MessageContent getter.
func (x *HandleAsyncPostMessageRequest) GetMessageContent() string {
	if x == nil {
		return ""
	}
	return x.MessageContent
}

// HandleAsyncPostMessageResponse.ResponseContent getter.
func (x *HandleAsyncPostMessageResponse) GetResponseContent() string {
	if x == nil {
		return ""
	}
	return x.ResponseContent
}

// HandleProposeCodeExtensionVerificationRequest.Step getter.
func (x *HandleProposeCodeExtensionVerificationRequest) GetStep() string {
	if x == nil {
		return ""
	}
	return x.Step
}

// FocusIDEWindowRequest.WindowType getter.
func (x *FocusIDEWindowRequest) GetWindowType() string {
	if x == nil {
		return ""
	}
	return x.WindowType
}

// FocusIDEWindowRequest.Editor getter.
func (x *FocusIDEWindowRequest) GetEditor() *FocusEditorWindow {
	if x == nil {
		return nil
	}
	return x.Editor
}

// FocusIDEWindowRequest.Manager getter.
func (x *FocusIDEWindowRequest) GetManager() *FocusManagerWindow {
	if x == nil {
		return nil
	}
	return x.Manager
}

// FocusEditorWindow.CascadeId getter.
func (x *FocusEditorWindow) GetCascadeId() string {
	if x == nil {
		return ""
	}
	return x.CascadeId
}

// FocusManagerWindow.CascadeId getter.
func (x *FocusManagerWindow) GetCascadeId() string {
	if x == nil {
		return ""
	}
	return x.CascadeId
}

// PlaySoundRequest.SoundType getter.
func (x *PlaySoundRequest) GetSoundType() SoundType {
	if x == nil {
		return SoundTypeUnspecified
	}
	return x.SoundType
}

// FindAllReferencesRequest.FileUri getter.
func (x *FindAllReferencesRequest) GetFileUri() string {
	if x == nil {
		return ""
	}
	return x.FileUri
}

// FindAllReferencesRequest.Line getter.
func (x *FindAllReferencesRequest) GetLine() int32 {
	if x == nil {
		return 0
	}
	return x.Line
}

// FindAllReferencesRequest.OccurrenceIndex getter.
func (x *FindAllReferencesRequest) GetOccurrenceIndex() int32 {
	if x == nil {
		return 0
	}
	return x.OccurrenceIndex
}

// FindAllReferencesRequest.Symbol getter.
func (x *FindAllReferencesRequest) GetSymbol() string {
	if x == nil {
		return ""
	}
	return x.Symbol
}

// FindAllReferencesResponse.References getter.
func (x *FindAllReferencesResponse) GetReferences() []string {
	if x == nil {
		return nil
	}
	return x.References
}

// GetDefinitionRequest.Request getter.
func (x *GetDefinitionRequest) GetRequest() []*GetDefinitionRequestItem {
	if x == nil {
		return nil
	}
	return x.Request
}

// GetDefinitionRequestItem.FileUri getter.
func (x *GetDefinitionRequestItem) GetFileUri() string {
	if x == nil {
		return ""
	}
	return x.FileUri
}

// GetDefinitionRequestItem.Position getter.
func (x *GetDefinitionRequestItem) GetPosition() int32 {
	if x == nil {
		return 0
	}
	return x.Position
}

// GetDefinitionRequestItem.LineContent getter.
func (x *GetDefinitionRequestItem) GetLineContent() string {
	if x == nil {
		return ""
	}
	return x.LineContent
}

// GetDefinitionResponse.Response getter.
func (x *GetDefinitionResponse) GetResponse() []*GetDefinitionResponseItem {
	if x == nil {
		return nil
	}
	return x.Response
}

// GetDefinitionResponseItem.Definitions getter.
func (x *GetDefinitionResponseItem) GetDefinitions() []string {
	if x == nil {
		return nil
	}
	return x.Definitions
}

// GetDefinitionResponseItem.Status getter.
func (x *GetDefinitionResponseItem) GetStatus() string {
	if x == nil {
		return ""
	}
	return x.Status
}

// GetLintErrorsRequest.ConversationId getter.
func (x *GetLintErrorsRequest) GetConversationId() string {
	if x == nil {
		return ""
	}
	return x.ConversationId
}

// GetLintErrorsRequest.AwaitNewLintsConfig getter.
func (x *GetLintErrorsRequest) GetAwaitNewLintsConfig() *GetLintErrorsRequest_AwaitNewLintsConfig {
	if x == nil {
		return nil
	}
	return x.AwaitNewLintsConfig
}

// GetLintErrorsRequest_AwaitNewLintsConfig.AcknowledgerId getter.
func (x *GetLintErrorsRequest_AwaitNewLintsConfig) GetAcknowledgerId() string {
	if x == nil {
		return ""
	}
	return x.AcknowledgerId
}

// GetLintErrorsRequest_AwaitNewLintsConfig.GracePeriodMs getter.
func (x *GetLintErrorsRequest_AwaitNewLintsConfig) GetGracePeriodMs() int32 {
	if x == nil {
		return 0
	}
	return x.GracePeriodMs
}

// GetLintErrorsRequest_AwaitNewLintsConfig.IncludeSaved getter.
func (x *GetLintErrorsRequest_AwaitNewLintsConfig) GetIncludeSaved() bool {
	if x == nil {
		return false
	}
	return x.IncludeSaved
}

// GetLintErrorsRequest_AwaitNewLintsConfig.TimeoutMs getter.
func (x *GetLintErrorsRequest_AwaitNewLintsConfig) GetTimeoutMs() int32 {
	if x == nil {
		return 0
	}
	return x.TimeoutMs
}

// GetLintErrorsResponse.LintErrors getter.
func (x *GetLintErrorsResponse) GetLintErrors() []string {
	if x == nil {
		return nil
	}
	return x.LintErrors
}

// GetLintErrorsResponse.PersistentLintErrors getter.
func (x *GetLintErrorsResponse) GetPersistentLintErrors() []string {
	if x == nil {
		return nil
	}
	return x.PersistentLintErrors
}

// ConversationPickerRequest.CurrentCascadeId getter.
func (x *ConversationPickerRequest) GetCurrentCascadeId() string {
	if x == nil {
		return ""
	}
	return x.CurrentCascadeId
}

// ConversationPickerRequest.CurrentWorkspaceUri getter.
func (x *ConversationPickerRequest) GetCurrentWorkspaceUri() string {
	if x == nil {
		return ""
	}
	return x.CurrentWorkspaceUri
}

// ConversationPickerRequest.CurrentWorkspaceNormalizedPath getter.
func (x *ConversationPickerRequest) GetCurrentWorkspaceNormalizedPath() string {
	if x == nil {
		return ""
	}
	return x.CurrentWorkspaceNormalizedPath
}

// ConversationPickerRequest.Summaries getter.
func (x *ConversationPickerRequest) GetSummaries() []string {
	if x == nil {
		return nil
	}
	return x.Summaries
}

// ConversationPickerResponse.SelectedCascadeId getter.
func (x *ConversationPickerResponse) GetSelectedCascadeId() string {
	if x == nil {
		return ""
	}
	return x.SelectedCascadeId
}

// ConversationPickerResponse.DeletedCascadeIds getter.
func (x *ConversationPickerResponse) GetDeletedCascadeIds() []string {
	if x == nil {
		return nil
	}
	return x.DeletedCascadeIds
}

// ConversationPickerResponse.ChatMessage getter.
func (x *ConversationPickerResponse) GetChatMessage() string {
	if x == nil {
		return ""
	}
	return x.ChatMessage
}

// BroadcastConversationDeletionRequest.DeletedCascadeId getter.
func (x *BroadcastConversationDeletionRequest) GetDeletedCascadeId() string {
	if x == nil {
		return ""
	}
	return x.DeletedCascadeId
}

// SmartFocusConversationRequest.CascadeId getter.
func (x *SmartFocusConversationRequest) GetCascadeId() string {
	if x == nil {
		return ""
	}
	return x.CascadeId
}

// SubscribeToUnifiedStateSyncTopicRequest.Topic getter.
func (x *SubscribeToUnifiedStateSyncTopicRequest) GetTopic() string {
	if x == nil {
		return ""
	}
	return x.Topic
}

// PushUnifiedStateSyncUpdateRequest.Update getter.
func (x *PushUnifiedStateSyncUpdateRequest) GetUpdate() *UnifiedStateSyncUpdate {
	if x == nil {
		return nil
	}
	return x.Update
}

// UnifiedStateSyncUpdate.UpdateType getter.
func (x *UnifiedStateSyncUpdate) GetUpdateType() UnifiedStateSyncUpdate_UpdateType {
	if x == nil {
		return UnifiedStateSyncUpdate_UpdateTypeUnspecified
	}
	return x.UpdateType
}

// UnifiedStateSyncUpdate.InitialState getter.
func (x *UnifiedStateSyncUpdate) GetInitialState() []byte {
	if x == nil {
		return nil
	}
	return x.InitialState
}

// UnifiedStateSyncUpdate.AppliedUpdate getter.
func (x *UnifiedStateSyncUpdate) GetAppliedUpdate() []byte {
	if x == nil {
		return nil
	}
	return x.AppliedUpdate
}

// StartAudioRecordingResponse.RecordingId getter.
func (x *StartAudioRecordingResponse) GetRecordingId() string {
	if x == nil {
		return ""
	}
	return x.RecordingId
}

// GetCurrentAudioRecordingResponse.AverageVolume getter.
func (x *GetCurrentAudioRecordingResponse) GetAverageVolume() float32 {
	if x == nil {
		return 0
	}
	return x.AverageVolume
}

// EndAudioRecordingResponse.Transcription getter.
func (x *EndAudioRecordingResponse) GetTranscription() string {
	if x == nil {
		return ""
	}
	return x.Transcription
}

// EditNotebookRequest.AbsolutePathUri getter.
func (x *EditNotebookRequest) GetAbsolutePathUri() string {
	if x == nil {
		return ""
	}
	return x.AbsolutePathUri
}

// EditNotebookRequest.AnchorCellId getter.
func (x *EditNotebookRequest) GetAnchorCellId() string {
	if x == nil {
		return ""
	}
	return x.AnchorCellId
}

// EditNotebookRequest.AnchorCellIndex getter.
func (x *EditNotebookRequest) GetAnchorCellIndex() int32 {
	if x == nil {
		return 0
	}
	return x.AnchorCellIndex
}

// EditNotebookRequest.DeleteCount getter.
func (x *EditNotebookRequest) GetDeleteCount() int32 {
	if x == nil {
		return 0
	}
	return x.DeleteCount
}

// EditNotebookRequest.Operation getter.
func (x *EditNotebookRequest) GetOperation() EditNotebookOperation {
	if x == nil {
		return EditNotebookOperationUnspecified
	}
	return x.Operation
}

// EditNotebookRequest.Cells getter.
func (x *EditNotebookRequest) GetCells() []*EditNotebookRequest_Cell {
	if x == nil {
		return nil
	}
	return x.Cells
}

// EditNotebookRequest_Cell.Kind getter.
func (x *EditNotebookRequest_Cell) GetKind() int32 {
	if x == nil {
		return 0
	}
	return x.Kind
}

// EditNotebookRequest_Cell.Content getter.
func (x *EditNotebookRequest_Cell) GetContent() string {
	if x == nil {
		return ""
	}
	return x.Content
}

// EditNotebookResponse.TotalCellCount getter.
func (x *EditNotebookResponse) GetTotalCellCount() int32 {
	if x == nil {
		return 0
	}
	return x.TotalCellCount
}

// EditNotebookResponse.ModifiedCells getter.
func (x *EditNotebookResponse) GetModifiedCells() []*EditNotebookResponse_ModifiedCell {
	if x == nil {
		return nil
	}
	return x.ModifiedCells
}

// EditNotebookResponse_ModifiedCell.CellId getter.
func (x *EditNotebookResponse_ModifiedCell) GetCellId() string {
	if x == nil {
		return ""
	}
	return x.CellId
}

// EditNotebookResponse_ModifiedCell.Index getter.
func (x *EditNotebookResponse_ModifiedCell) GetIndex() int32 {
	if x == nil {
		return 0
	}
	return x.Index
}

// EditNotebookResponse_ModifiedCell.Kind getter.
func (x *EditNotebookResponse_ModifiedCell) GetKind() int32 {
	if x == nil {
		return 0
	}
	return x.Kind
}

// EditNotebookResponse_ModifiedCell.Content getter.
func (x *EditNotebookResponse_ModifiedCell) GetContent() string {
	if x == nil {
		return ""
	}
	return x.Content
}

// ReadNotebookRequest.AbsolutePathUri getter.
func (x *ReadNotebookRequest) GetAbsolutePathUri() string {
	if x == nil {
		return ""
	}
	return x.AbsolutePathUri
}

// ReadNotebookRequest.CellIds getter.
func (x *ReadNotebookRequest) GetCellIds() []string {
	if x == nil {
		return nil
	}
	return x.CellIds
}

// ReadNotebookRequest.CellIndices getter.
func (x *ReadNotebookRequest) GetCellIndices() []int32 {
	if x == nil {
		return nil
	}
	return x.CellIndices
}

// ReadNotebookResponse.FlattenedContent getter.
func (x *ReadNotebookResponse) GetFlattenedContent() string {
	if x == nil {
		return ""
	}
	return x.FlattenedContent
}

// ReadNotebookResponse.Blobs getter.
func (x *ReadNotebookResponse) GetBlobs() []byte {
	if x == nil {
		return nil
	}
	return x.Blobs
}

// ReadNotebookResponse.ReadCellCount getter.
func (x *ReadNotebookResponse) GetReadCellCount() int32 {
	if x == nil {
		return 0
	}
	return x.ReadCellCount
}

// ExecuteNotebookRequest.AbsolutePathUri getter.
func (x *ExecuteNotebookRequest) GetAbsolutePathUri() string {
	if x == nil {
		return ""
	}
	return x.AbsolutePathUri
}

// ExecuteNotebookRequest.CellIds getter.
func (x *ExecuteNotebookRequest) GetCellIds() []string {
	if x == nil {
		return nil
	}
	return x.CellIds
}

// ExecuteNotebookRequest.CellIndices getter.
func (x *ExecuteNotebookRequest) GetCellIndices() []int32 {
	if x == nil {
		return nil
	}
	return x.CellIndices
}

// ExecuteNotebookRequest.StopOnError getter.
func (x *ExecuteNotebookRequest) GetStopOnError() bool {
	if x == nil {
		return false
	}
	return x.StopOnError
}

// ExecuteNotebookRequest.RestartKernel getter.
func (x *ExecuteNotebookRequest) GetRestartKernel() bool {
	if x == nil {
		return false
	}
	return x.RestartKernel
}

// ExecuteNotebookResponse.Status getter.
func (x *ExecuteNotebookResponse) GetStatus() ExecuteNotebookResponse_ExecutionStatus {
	if x == nil {
		return ExecuteNotebookResponse_ExecutionStatusUnspecified
	}
	return x.Status
}

// ExecuteNotebookResponse.CellOutputs getter.
func (x *ExecuteNotebookResponse) GetCellOutputs() []*ExecuteNotebookResponse_CellOutput {
	if x == nil {
		return nil
	}
	return x.CellOutputs
}

// ExecuteNotebookResponse.ErrorTrace getter.
func (x *ExecuteNotebookResponse) GetErrorTrace() string {
	if x == nil {
		return ""
	}
	return x.ErrorTrace
}

// ExecuteNotebookResponse.ExecutedCellsCount getter.
func (x *ExecuteNotebookResponse) GetExecutedCellsCount() int32 {
	if x == nil {
		return 0
	}
	return x.ExecutedCellsCount
}

// ExecuteNotebookResponse_CellOutput.CellId getter.
func (x *ExecuteNotebookResponse_CellOutput) GetCellId() string {
	if x == nil {
		return ""
	}
	return x.CellId
}

// ExecuteNotebookResponse_CellOutput.CellIndex getter.
func (x *ExecuteNotebookResponse_CellOutput) GetCellIndex() int32 {
	if x == nil {
		return 0
	}
	return x.CellIndex
}

// ExecuteNotebookResponse_CellOutput.Status getter.
func (x *ExecuteNotebookResponse_CellOutput) GetStatus() ExecuteNotebookResponse_CellStatus {
	if x == nil {
		return ExecuteNotebookResponse_CellStatusUnspecified
	}
	return x.Status
}

// ExecuteNotebookResponse_CellOutput.OutputText getter.
func (x *ExecuteNotebookResponse_CellOutput) GetOutputText() string {
	if x == nil {
		return ""
	}
	return x.OutputText
}

// ExecuteNotebookResponse_CellOutput.OutputTextTruncated getter.
func (x *ExecuteNotebookResponse_CellOutput) GetOutputTextTruncated() bool {
	if x == nil {
		return false
	}
	return x.OutputTextTruncated
}

// ExecuteNotebookResponse_CellOutput.ErrorTrace getter.
func (x *ExecuteNotebookResponse_CellOutput) GetErrorTrace() string {
	if x == nil {
		return ""
	}
	return x.ErrorTrace
}

// ExecuteNotebookResponse_CellOutput.Blobs getter.
func (x *ExecuteNotebookResponse_CellOutput) GetBlobs() []byte {
	if x == nil {
		return nil
	}
	return x.Blobs
}

// ExecuteNotebookResponse_CellOutput.GeneratedImages getter.
func (x *ExecuteNotebookResponse_CellOutput) GetGeneratedImages() [][]byte {
	if x == nil {
		return nil
	}
	return x.GeneratedImages
}

// UpdateCascadeTrajectorySummariesRequest.Summaries getter.
func (x *UpdateCascadeTrajectorySummariesRequest) GetSummaries() []string {
	if x == nil {
		return nil
	}
	return x.Summaries
}

// WriteCascadeEditRequest.Uri getter.
func (x *WriteCascadeEditRequest) GetUri() string {
	if x == nil {
		return ""
	}
	return x.Uri
}

// WriteCascadeEditRequest.TargetContent getter.
func (x *WriteCascadeEditRequest) GetTargetContent() string {
	if x == nil {
		return ""
	}
	return x.TargetContent
}

// UpdateDetailedViewWithCascadeInputRequest.Request getter.
func (x *UpdateDetailedViewWithCascadeInputRequest) GetRequest() string {
	if x == nil {
		return ""
	}
	return x.Request
}

// UpdateDetailedViewWithCascadeInputRequest.Response getter.
func (x *UpdateDetailedViewWithCascadeInputRequest) GetResponse() string {
	if x == nil {
		return ""
	}
	return x.Response
}
