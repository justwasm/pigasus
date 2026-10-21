// Package extension_server_pb is a recovered (re-implemented) binding
// for google3/third_party/jetski/extension_server_pb. Source-of-truth
// artifacts are recovered from the v1.3.5 binary at
// /workspace/agy-recovered/docs/analysis-v1.3.5-official/funcs-v1.3.5-pclntab.txt
// (941 funcs in the package).
//
// The 1.3.5 binary defines an ExtensionServerService Connect-RPC service
// (transitively a gRPC service) with 53 RPCs. The service is used by the
// IDE host (VS Code / JetBrains / ...) to invoke actions on the local
// antigravity language server (terminal, file, notebook, browser, audio,
// secrets, unified state sync, etc.).
//
// The full 53-method RPC list is enumerated in ExtensionServerServiceServer
// below. The two confirmed server-streaming RPCs (GenericServerStream
// instantiations in the binary) are SubscribeToUnifiedStateSyncTopic and
// ExecuteCommand; the remaining 51 RPCs are unary.
//
// In the recovered build, this package provides:
//   - the proto message Go structs (one Request/Response pair per RPC,
//     plus nested types and enums);
//   - the service interface (ExtensionServerServiceServer);
//   - a placeholder client (ExtensionServerServiceClient) that satisfies
//     the interface but does not actually invoke the RPCs (calls return
//     ErrNotImplemented or a structured placeholder).
//
// The recovered style matches internal/language_server_pb: package
// comment + 1:1 decompile note + types split into a sibling file + a
// unit test file with 5+ sub-tests.
package extension_server_pb

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"time"
)

// Service name (Connect-RPC / gRPC service descriptor).
const ServiceName = "jetski.extension_server.ExtensionServerService"

// ErrNotImplemented is returned by placeholder RPCs.
var ErrNotImplemented = errors.New("extension_server_pb: RPC not implemented in recovered build")

// ActiveEditorType is the recovered enum for InsertCodeAtCursorRequest.EditorType.
type ActiveEditorType int32

const (
	ActiveEditorTypeUnspecified  ActiveEditorType = 0
	ActiveEditorTypeCodeEditor   ActiveEditorType = 1
	ActiveEditorTypeChatComposer ActiveEditorType = 2
)

// SoundType is the recovered enum for PlaySoundRequest.SoundType.
type SoundType int32

const (
	SoundTypeUnspecified SoundType = 0
	SoundTypeChime       SoundType = 1
	SoundTypeError       SoundType = 2
)

// EditNotebookOperation is the recovered enum for EditNotebookRequest.Operation.
type EditNotebookOperation int32

const (
	EditNotebookOperationUnspecified EditNotebookOperation = 0
	EditNotebookOperationInsert      EditNotebookOperation = 1
	EditNotebookOperationDelete      EditNotebookOperation = 2
	EditNotebookOperationReplace     EditNotebookOperation = 3
)

// ExecuteNotebookResponse_CellStatus is the recovered enum for
// ExecuteNotebookResponse_CellOutput.Status.
type ExecuteNotebookResponse_CellStatus int32

const (
	ExecuteNotebookResponse_CellStatusUnspecified ExecuteNotebookResponse_CellStatus = 0
	ExecuteNotebookResponse_CellStatusOK          ExecuteNotebookResponse_CellStatus = 1
	ExecuteNotebookResponse_CellStatusError       ExecuteNotebookResponse_CellStatus = 2
)

// ExecuteNotebookResponse_ExecutionStatus is the recovered enum for
// ExecuteNotebookResponse.Status.
type ExecuteNotebookResponse_ExecutionStatus int32

const (
	ExecuteNotebookResponse_ExecutionStatusUnspecified ExecuteNotebookResponse_ExecutionStatus = 0
	ExecuteNotebookResponse_ExecutionStatusOK          ExecuteNotebookResponse_ExecutionStatus = 1
	ExecuteNotebookResponse_ExecutionStatusError       ExecuteNotebookResponse_ExecutionStatus = 2
)

// EventMetadataRecord is a (key, value) telemetry metadata record
// (recovered from funcs-v1.3.5-pclntab.txt: *EventMetadataRecord).
type EventMetadataRecord struct {
	Key   string `protobuf:"bytes,1,opt,name=key,proto3" json:"key,omitempty"`
	Value string `protobuf:"bytes,2,opt,name=value,proto3" json:"value,omitempty"`
}

// FocusEditorWindow is a streaming-style message used by the
// focus editor flow (recovered as a top-level proto message).
type FocusEditorWindow struct {
	CascadeId string `protobuf:"bytes,1,opt,name=cascade_id,json=cascadeId,proto3" json:"cascade_id,omitempty"`
}

// FocusManagerWindow is a streaming-style message used by the
// focus manager flow (recovered as a top-level proto message).
type FocusManagerWindow struct {
	CascadeId string `protobuf:"bytes,1,opt,name=cascade_id,json=cascadeId,proto3" json:"cascade_id,omitempty"`
}

// UnifiedStateSyncUpdate is a server-streamed message from
// SubscribeToUnifiedStateSyncTopic (GenericServerStream specialized in
// the binary at pclntab 0x7335bc0). One of initial_state or update is
// populated per message.
type UnifiedStateSyncUpdate struct {
	UpdateType     UnifiedStateSyncUpdate_UpdateType `protobuf:"varint,1,opt,name=update_type,json=updateType,proto3,enum=extension_server_pb.UnifiedStateSyncUpdate_UpdateType" json:"update_type,omitempty"`
	InitialState   []byte `protobuf:"bytes,2,opt,name=initial_state,json=initialState,proto3" json:"initial_state,omitempty"`
	AppliedUpdate  []byte `protobuf:"bytes,3,opt,name=applied_update,json=appliedUpdate,proto3" json:"applied_update,omitempty"`
}

// UnifiedStateSyncUpdate.UpdateType is the recovered enum for the
// oneof discriminator.
type UnifiedStateSyncUpdate_UpdateType int32

const (
	UnifiedStateSyncUpdate_UpdateTypeUnspecified    UnifiedStateSyncUpdate_UpdateType = 0
	UnifiedStateSyncUpdate_UpdateTypeInitial        UnifiedStateSyncUpdate_UpdateType = 1
	UnifiedStateSyncUpdate_UpdateTypeAppend         UnifiedStateSyncUpdate_UpdateType = 2
	UnifiedStateSyncUpdate_UpdateTypeAck            UnifiedStateSyncUpdate_UpdateType = 3
)

// ---------------------------------------------------------------------------
// Request/Response message structs — 53 RPC pairs.
// Each is recovered 1:1 from pclntab method-getter signatures.
// ---------------------------------------------------------------------------

type IsAgentManagerEnabledRequest struct{}

type IsAgentManagerEnabledResponse struct {
	Enabled bool `protobuf:"varint,1,opt,name=enabled,proto3" json:"enabled,omitempty"`
}

type HeartbeatRequest struct{}

type HeartbeatResponse struct {
	ServerTime time.Time `protobuf:"bytes,1,opt,name=server_time,json=serverTime,proto3,stdtime" json:"server_time,omitempty"`
}

type LanguageServerStartedRequest struct {
	HttpPort   int32  `protobuf:"varint,1,opt,name=http_port,json=httpPort,proto3" json:"http_port,omitempty"`
	HttpsPort  int32  `protobuf:"varint,2,opt,name=https_port,json=httpsPort,proto3" json:"https_port,omitempty"`
	LspPort    int32  `protobuf:"varint,3,opt,name=lsp_port,json=lspPort,proto3" json:"lsp_port,omitempty"`
	CsrfToken  string `protobuf:"bytes,4,opt,name=csrf_token,json=csrfToken,proto3" json:"csrf_token,omitempty"`
}

type LanguageServerStartedResponse struct{}

type LaunchBrowserRequest struct {
	ExtensionPath      string `protobuf:"bytes,1,opt,name=extension_path,json=extensionPath,proto3" json:"extension_path,omitempty"`
	LaunchInForeground bool   `protobuf:"varint,2,opt,name=launch_in_foreground,json=launchInForeground,proto3" json:"launch_in_foreground,omitempty"`
}

type LaunchBrowserResponse struct {
	CdpAddress string `protobuf:"bytes,1,opt,name=cdp_address,json=cdpAddress,proto3" json:"cdp_address,omitempty"`
}

type GetBrowserOnboardingPortRequest struct{}

type GetBrowserOnboardingPortResponse struct {
	Port int32 `protobuf:"varint,1,opt,name=port,proto3" json:"port,omitempty"`
}

type GetChromeDevtoolsMcpUrlRequest struct{}

type GetChromeDevtoolsMcpUrlResponse struct {
	Url string `protobuf:"bytes,1,opt,name=url,proto3" json:"url,omitempty"`
}

type OpenExternalUrlRequest struct {
	Url           string `protobuf:"bytes,1,opt,name=url,proto3" json:"url,omitempty"`
	UseIdeBrowser bool   `protobuf:"varint,2,opt,name=use_ide_browser,json=useIdeBrowser,proto3" json:"use_ide_browser,omitempty"`
}

type OpenExternalUrlResponse struct{}

type CheckTerminalShellSupportRequest struct{}

type CheckTerminalShellSupportResponse struct {
	HasShellIntegration bool   `protobuf:"varint,1,opt,name=has_shell_integration,json=hasShellIntegration,proto3" json:"has_shell_integration,omitempty"`
	ShellName           string `protobuf:"bytes,2,opt,name=shell_name,json=shellName,proto3" json:"shell_name,omitempty"`
	ShellPath           string `protobuf:"bytes,3,opt,name=shell_path,json=shellPath,proto3" json:"shell_path,omitempty"`
}

type OpenTerminalRequest struct {
	ProcessId int64 `protobuf:"varint,1,opt,name=process_id,json=processId,proto3" json:"process_id,omitempty"`
}

type OpenTerminalResponse struct{}

type ShowTerminalRequest struct {
	TerminalId string `protobuf:"bytes,1,opt,name=terminal_id,json=terminalId,proto3" json:"terminal_id,omitempty"`
}

type ShowTerminalResponse struct{}

type ReadTerminalRequest struct {
	ConversationId string `protobuf:"bytes,1,opt,name=conversation_id,json=conversationId,proto3" json:"conversation_id,omitempty"`
	ProcessId      int64  `protobuf:"varint,2,opt,name=process_id,json=processId,proto3" json:"process_id,omitempty"`
}

type ReadTerminalResponse struct {
	Contents string `protobuf:"bytes,1,opt,name=contents,proto3" json:"contents,omitempty"`
}

type SendTerminalInputRequest struct {
	ConversationId string `protobuf:"bytes,1,opt,name=conversation_id,json=conversationId,proto3" json:"conversation_id,omitempty"`
	TerminalId     string `protobuf:"bytes,2,opt,name=terminal_id,json=terminalId,proto3" json:"terminal_id,omitempty"`
	Input          string `protobuf:"bytes,3,opt,name=input,proto3" json:"input,omitempty"`
}

type SendTerminalInputResponse struct {
	Success bool   `protobuf:"varint,1,opt,name=success,proto3" json:"success,omitempty"`
	Error   string `protobuf:"bytes,2,opt,name=error,proto3" json:"error,omitempty"`
}

type TerminateCommandRequest struct {
	ConversationId string `protobuf:"bytes,1,opt,name=conversation_id,json=conversationId,proto3" json:"conversation_id,omitempty"`
	TerminalId     string `protobuf:"bytes,2,opt,name=terminal_id,json=terminalId,proto3" json:"terminal_id,omitempty"`
}

type TerminateCommandResponse struct{}

type ExecuteCommandRequest struct {
	CommandLine    string            `protobuf:"bytes,1,opt,name=command_line,json=commandLine,proto3" json:"command_line,omitempty"`
	Cwd            string            `protobuf:"bytes,2,opt,name=cwd,proto3" json:"cwd,omitempty"`
	Env            map[string]string `protobuf:"bytes,3,rep,name=env,proto3" json:"env,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
	SandboxOverride string           `protobuf:"bytes,4,opt,name=sandbox_override,json=sandboxOverride,proto3" json:"sandbox_override,omitempty"`
	TerminalId     string            `protobuf:"bytes,5,opt,name=terminal_id,json=terminalId,proto3" json:"terminal_id,omitempty"`
	TerminalHost   string            `protobuf:"bytes,6,opt,name=terminal_host,json=terminalHost,proto3" json:"terminal_host,omitempty"`
	ConversationId string            `protobuf:"bytes,7,opt,name=conversation_id,json=conversationId,proto3" json:"conversation_id,omitempty"`
}

// ExecuteCommandResponse is server-streamed as
// TerminalShellCommandStreamChunk (codeium_common_pb) by the
// GenericServerStream specialization at pclntab 0x7335d60.
type ExecuteCommandResponse struct {
	// Placeholder — the wire-level stream chunk is
	// codeium_common_pb.TerminalShellCommandStreamChunk.
	TerminalId string `protobuf:"bytes,1,opt,name=terminal_id,json=terminalId,proto3" json:"terminal_id,omitempty"`
}

type TerminalResearchResultRequest struct {
	LastStep int32 `protobuf:"varint,1,opt,name=last_step,json=lastStep,proto3" json:"last_step,omitempty"`
}

type TerminalResearchResultResponse struct {
	Result string `protobuf:"bytes,1,opt,name=result,proto3" json:"result,omitempty"`
}

type RunExtensionCodeRequest struct {
	Code string `protobuf:"bytes,1,opt,name=code,proto3" json:"code,omitempty"`
}

type RunExtensionCodeResponse struct {
	Output string `protobuf:"bytes,1,opt,name=output,proto3" json:"output,omitempty"`
}

type OpenFilePointerRequest struct {
	FileUri    string `protobuf:"bytes,1,opt,name=file_uri,json=fileUri,proto3" json:"file_uri,omitempty"`
	StartLine  int32  `protobuf:"varint,2,opt,name=start_line,json=startLine,proto3" json:"start_line,omitempty"`
	StartCol   int32  `protobuf:"varint,3,opt,name=start_col,json=startCol,proto3" json:"start_col,omitempty"`
	EndLine    int32  `protobuf:"varint,4,opt,name=end_line,json=endLine,proto3" json:"end_line,omitempty"`
	EndCol     int32  `protobuf:"varint,5,opt,name=end_col,json=endCol,proto3" json:"end_col,omitempty"`
	IsCascadeDiff bool `protobuf:"varint,6,opt,name=is_cascade_diff,json=isCascadeDiff,proto3" json:"is_cascade_diff,omitempty"`
}

type OpenFilePointerResponse struct{}

type OpenVirtualFileRequest struct {
	Text                 string `protobuf:"bytes,1,opt,name=text,proto3" json:"text,omitempty"`
	ShowMarkdownPreview  bool   `protobuf:"varint,2,opt,name=show_markdown_preview,json=showMarkdownPreview,proto3" json:"show_markdown_preview,omitempty"`
}

type OpenVirtualFileResponse struct{}

type SaveDocumentRequest struct {
	Uri            string `protobuf:"bytes,1,opt,name=uri,proto3" json:"uri,omitempty"`
	ConversationId string `protobuf:"bytes,2,opt,name=conversation_id,json=conversationId,proto3" json:"conversation_id,omitempty"`
}

type SaveDocumentResponse struct{}

type InsertCodeAtCursorRequest struct {
	Text       string          `protobuf:"bytes,1,opt,name=text,proto3" json:"text,omitempty"`
	EditorType ActiveEditorType `protobuf:"varint,2,opt,name=editor_type,json=editorType,proto3,enum=extension_server_pb.ActiveEditorType" json:"editor_type,omitempty"`
}

type InsertCodeAtCursorResponse struct{}

type OpenDiffZonesRequest struct {
	CascadeId        string   `protobuf:"bytes,1,opt,name=cascade_id,json=cascadeId,proto3" json:"cascade_id,omitempty"`
	StepIndices      []int32  `protobuf:"varint,2,rep,packed,name=step_indices,json=stepIndices,proto3" json:"step_indices,omitempty"`
	TraceId          string   `protobuf:"bytes,3,opt,name=trace_id,json=traceId,proto3" json:"trace_id,omitempty"`
	ValidationStates []string `protobuf:"bytes,4,rep,name=validation_states,json=validationStates,proto3" json:"validation_states,omitempty"`
	WriteNewState    bool     `protobuf:"varint,5,opt,name=write_new_state,json=writeNewState,proto3" json:"write_new_state,omitempty"`
}

type OpenDiffZonesResponse struct{}

type OpenSettingRequest struct {
	SettingId string `protobuf:"bytes,1,opt,name=setting_id,json=settingId,proto3" json:"setting_id,omitempty"`
}

type OpenSettingResponse struct{}

type OpenAntigravityRulesFileRequest struct {
	IsWorkspaceRules bool `protobuf:"varint,1,opt,name=is_workspace_rules,json=isWorkspaceRules,proto3" json:"is_workspace_rules,omitempty"`
}

type OpenAntigravityRulesFileResponse struct{}

type OpenPluginPageRequest struct {
	PluginId string `protobuf:"bytes,1,opt,name=plugin_id,json=pluginId,proto3" json:"plugin_id,omitempty"`
}

type OpenPluginPageResponse struct{}

type OpenPluginConfigModalRequest struct {
	PluginId string `protobuf:"bytes,1,opt,name=plugin_id,json=pluginId,proto3" json:"plugin_id,omitempty"`
}

type OpenPluginConfigModalResponse struct{}

type OpenConfigurePluginsPageRequest struct{}

type OpenConfigurePluginsPageResponse struct{}

type OpenConversationWorkspaceQuickPickRequest struct {
	CascadeId     string   `protobuf:"bytes,1,opt,name=cascade_id,json=cascadeId,proto3" json:"cascade_id,omitempty"`
	WorkspaceUris []string `protobuf:"bytes,2,rep,name=workspace_uris,json=workspaceUris,proto3" json:"workspace_uris,omitempty"`
}

type OpenConversationWorkspaceQuickPickResponse struct {
	OpenInCurrentWindow bool `protobuf:"varint,1,opt,name=open_in_current_window,json=openInCurrentWindow,proto3" json:"open_in_current_window,omitempty"`
}

type GetSecretValueRequest struct {
	Key string `protobuf:"bytes,1,opt,name=key,proto3" json:"key,omitempty"`
}

type GetSecretValueResponse struct {
	Value string `protobuf:"bytes,1,opt,name=value,proto3" json:"value,omitempty"`
}

type StoreSecretValueRequest struct {
	Key   string `protobuf:"bytes,1,opt,name=key,proto3" json:"key,omitempty"`
	Value string `protobuf:"bytes,2,opt,name=value,proto3" json:"value,omitempty"`
}

type StoreSecretValueResponse struct{}

type LogEventRequest struct {
	Event      string               `protobuf:"bytes,1,opt,name=event,proto3" json:"event,omitempty"`
	EventString string              `protobuf:"bytes,2,opt,name=event_string,json=eventString,proto3" json:"event_string,omitempty"`
	Metadata   []*EventMetadataRecord `protobuf:"bytes,3,rep,name=metadata,proto3" json:"metadata,omitempty"`
}

type LogEventResponse struct{}

type RecordErrorRequest struct {
	ErrorTrace string `protobuf:"bytes,1,opt,name=error_trace,json=errorTrace,proto3" json:"error_trace,omitempty"`
}

type RecordErrorResponse struct{}

type RestartUserStatusUpdaterRequest struct{}

type RestartUserStatusUpdaterResponse struct{}

type HandleAsyncPostMessageRequest struct {
	MessageType    string `protobuf:"bytes,1,opt,name=message_type,json=messageType,proto3" json:"message_type,omitempty"`
	MessageContent string `protobuf:"bytes,2,opt,name=message_content,json=messageContent,proto3" json:"message_content,omitempty"`
}

type HandleAsyncPostMessageResponse struct {
	ResponseContent string `protobuf:"bytes,1,opt,name=response_content,json=responseContent,proto3" json:"response_content,omitempty"`
}

type HandleProposeCodeExtensionVerificationRequest struct {
	Step string `protobuf:"bytes,1,opt,name=step,proto3" json:"step,omitempty"`
}

type HandleProposeCodeExtensionVerificationResponse struct {
	Result string `protobuf:"bytes,1,opt,name=result,proto3" json:"result,omitempty"`
}

type FocusIDEWindowRequest struct {
	WindowType string            `protobuf:"bytes,1,opt,name=window_type,json=windowType,proto3" json:"window_type,omitempty"`
	Editor     *FocusEditorWindow `protobuf:"bytes,2,opt,name=editor,proto3" json:"editor,omitempty"`
	Manager    *FocusManagerWindow `protobuf:"bytes,3,opt,name=manager,proto3" json:"manager,omitempty"`
}

type FocusIDEWindowResponse struct{}

type PlaySoundRequest struct {
	SoundType SoundType `protobuf:"varint,1,opt,name=sound_type,json=soundType,proto3,enum=extension_server_pb.SoundType" json:"sound_type,omitempty"`
}

type PlaySoundResponse struct{}

type FindAllReferencesRequest struct {
	FileUri        string `protobuf:"bytes,1,opt,name=file_uri,json=fileUri,proto3" json:"file_uri,omitempty"`
	Line           int32  `protobuf:"varint,2,opt,name=line,proto3" json:"line,omitempty"`
	OccurrenceIndex int32 `protobuf:"varint,3,opt,name=occurrence_index,json=occurrenceIndex,proto3" json:"occurrence_index,omitempty"`
	Symbol         string `protobuf:"bytes,4,opt,name=symbol,proto3" json:"symbol,omitempty"`
}

type FindAllReferencesResponse struct {
	References []string `protobuf:"bytes,1,rep,name=references,proto3" json:"references,omitempty"`
}

// GetDefinitionRequestItem is a single (file_uri, position, line_content) request item.
type GetDefinitionRequestItem struct {
	FileUri     string `protobuf:"bytes,1,opt,name=file_uri,json=fileUri,proto3" json:"file_uri,omitempty"`
	Position    int32  `protobuf:"varint,2,opt,name=position,proto3" json:"position,omitempty"`
	LineContent string `protobuf:"bytes,3,opt,name=line_content,json=lineContent,proto3" json:"line_content,omitempty"`
}

// GetDefinitionResponseItem is the resolved definition list for one request item.
type GetDefinitionResponseItem struct {
	Definitions []string `protobuf:"bytes,1,rep,name=definitions,proto3" json:"definitions,omitempty"`
	Status      string   `protobuf:"bytes,2,opt,name=status,proto3" json:"status,omitempty"`
}

type GetDefinitionRequest struct {
	Request []*GetDefinitionRequestItem `protobuf:"bytes,1,rep,name=request,proto3" json:"request,omitempty"`
}

type GetDefinitionResponse struct {
	Response []*GetDefinitionResponseItem `protobuf:"bytes,1,rep,name=response,proto3" json:"response,omitempty"`
}

// GetLintErrorsRequest_AwaitNewLintsConfig is the nested config for
// awaiting new lint errors.
type GetLintErrorsRequest_AwaitNewLintsConfig struct {
	AcknowledgerId string `protobuf:"bytes,1,opt,name=acknowledger_id,json=acknowledgerId,proto3" json:"acknowledger_id,omitempty"`
	GracePeriodMs  int32  `protobuf:"varint,2,opt,name=grace_period_ms,json=gracePeriodMs,proto3" json:"grace_period_ms,omitempty"`
	IncludeSaved   bool   `protobuf:"varint,3,opt,name=include_saved,json=includeSaved,proto3" json:"include_saved,omitempty"`
	TimeoutMs      int32  `protobuf:"varint,4,opt,name=timeout_ms,json=timeoutMs,proto3" json:"timeout_ms,omitempty"`
}

type GetLintErrorsRequest struct {
	ConversationId       string                                   `protobuf:"bytes,1,opt,name=conversation_id,json=conversationId,proto3" json:"conversation_id,omitempty"`
	AwaitNewLintsConfig  *GetLintErrorsRequest_AwaitNewLintsConfig `protobuf:"bytes,2,opt,name=await_new_lints_config,json=awaitNewLintsConfig,proto3" json:"await_new_lints_config,omitempty"`
}

type GetLintErrorsResponse struct {
	LintErrors         []string `protobuf:"bytes,1,rep,name=lint_errors,json=lintErrors,proto3" json:"lint_errors,omitempty"`
	PersistentLintErrors []string `protobuf:"bytes,2,rep,name=persistent_lint_errors,json=persistentLintErrors,proto3" json:"persistent_lint_errors,omitempty"`
}

type ConversationPickerRequest struct {
	CurrentCascadeId             string   `protobuf:"bytes,1,opt,name=current_cascade_id,json=currentCascadeId,proto3" json:"current_cascade_id,omitempty"`
	CurrentWorkspaceUri          string   `protobuf:"bytes,2,opt,name=current_workspace_uri,json=currentWorkspaceUri,proto3" json:"current_workspace_uri,omitempty"`
	CurrentWorkspaceNormalizedPath string  `protobuf:"bytes,3,opt,name=current_workspace_normalized_path,json=currentWorkspaceNormalizedPath,proto3" json:"current_workspace_normalized_path,omitempty"`
	Summaries                    []string `protobuf:"bytes,4,rep,name=summaries,proto3" json:"summaries,omitempty"`
}

type ConversationPickerResponse struct {
	SelectedCascadeId string   `protobuf:"bytes,1,opt,name=selected_cascade_id,json=selectedCascadeId,proto3" json:"selected_cascade_id,omitempty"`
	DeletedCascadeIds []string `protobuf:"bytes,2,rep,name=deleted_cascade_ids,json=deletedCascadeIds,proto3" json:"deleted_cascade_ids,omitempty"`
	ChatMessage       string   `protobuf:"bytes,3,opt,name=chat_message,json=chatMessage,proto3" json:"chat_message,omitempty"`
}

type ShowConversationPickerRequest struct {
	Picker *ConversationPickerRequest `protobuf:"bytes,1,opt,name=picker,proto3" json:"picker,omitempty"`
}

type ShowConversationPickerResponse struct {
	Picker *ConversationPickerResponse `protobuf:"bytes,1,opt,name=picker,proto3" json:"picker,omitempty"`
}

type BroadcastConversationDeletionRequest struct {
	DeletedCascadeId string `protobuf:"bytes,1,opt,name=deleted_cascade_id,json=deletedCascadeId,proto3" json:"deleted_cascade_id,omitempty"`
}

type BroadcastConversationDeletionResponse struct{}

type SmartFocusConversationRequest struct {
	CascadeId string `protobuf:"bytes,1,opt,name=cascade_id,json=cascadeId,proto3" json:"cascade_id,omitempty"`
}

type SmartFocusConversationResponse struct{}

type SubscribeToUnifiedStateSyncTopicRequest struct {
	Topic string `protobuf:"bytes,1,opt,name=topic,proto3" json:"topic,omitempty"`
}

// SubscribeToUnifiedStateSyncTopicResponse is a stream of UnifiedStateSyncUpdate.
// See GenericServerStream at pclntab 0x7335bc0 for the wire-level specialization.

type PushUnifiedStateSyncUpdateRequest struct {
	Update *UnifiedStateSyncUpdate `protobuf:"bytes,1,opt,name=update,proto3" json:"update,omitempty"`
}

type PushUnifiedStateSyncUpdateResponse struct{}

type StartAudioRecordingRequest struct{}

type StartAudioRecordingResponse struct {
	RecordingId string `protobuf:"bytes,1,opt,name=recording_id,json=recordingId,proto3" json:"recording_id,omitempty"`
}

type GetCurrentAudioRecordingRequest struct{}

type GetCurrentAudioRecordingResponse struct {
	AverageVolume float32 `protobuf:"fixed32,1,opt,name=average_volume,json=averageVolume,proto3" json:"average_volume,omitempty"`
}

type EndAudioRecordingRequest struct{}

type EndAudioRecordingResponse struct {
	Transcription string `protobuf:"bytes,1,opt,name=transcription,proto3" json:"transcription,omitempty"`
}

// EditNotebookRequest_Cell is a single cell edit (recovered as a nested
// message in pclntab).
type EditNotebookRequest_Cell struct {
	Kind    int32  `protobuf:"varint,1,opt,name=kind,proto3" json:"kind,omitempty"`
	Content string `protobuf:"bytes,2,opt,name=content,proto3" json:"content,omitempty"`
}

// EditNotebookResponse_ModifiedCell is the cell diff returned in the
// response.
type EditNotebookResponse_ModifiedCell struct {
	CellId  string `protobuf:"bytes,1,opt,name=cell_id,json=cellId,proto3" json:"cell_id,omitempty"`
	Index   int32  `protobuf:"varint,2,opt,name=index,proto3" json:"index,omitempty"`
	Kind    int32  `protobuf:"varint,3,opt,name=kind,proto3" json:"kind,omitempty"`
	Content string `protobuf:"bytes,4,opt,name=content,proto3" json:"content,omitempty"`
}

type EditNotebookRequest struct {
	AbsolutePathUri string                    `protobuf:"bytes,1,opt,name=absolute_path_uri,json=absolutePathUri,proto3" json:"absolute_path_uri,omitempty"`
	AnchorCellId    string                    `protobuf:"bytes,2,opt,name=anchor_cell_id,json=anchorCellId,proto3" json:"anchor_cell_id,omitempty"`
	AnchorCellIndex int32                     `protobuf:"varint,3,opt,name=anchor_cell_index,json=anchorCellIndex,proto3" json:"anchor_cell_index,omitempty"`
	DeleteCount     int32                     `protobuf:"varint,4,opt,name=delete_count,json=deleteCount,proto3" json:"delete_count,omitempty"`
	Operation       EditNotebookOperation     `protobuf:"varint,5,opt,name=operation,proto3,enum=extension_server_pb.EditNotebookOperation" json:"operation,omitempty"`
	Cells           []*EditNotebookRequest_Cell `protobuf:"bytes,6,rep,name=cells,proto3" json:"cells,omitempty"`
}

type EditNotebookResponse struct {
	TotalCellCount  int32                              `protobuf:"varint,1,opt,name=total_cell_count,json=totalCellCount,proto3" json:"total_cell_count,omitempty"`
	ModifiedCells   []*EditNotebookResponse_ModifiedCell `protobuf:"bytes,2,rep,name=modified_cells,json=modifiedCells,proto3" json:"modified_cells,omitempty"`
}

type ReadNotebookRequest struct {
	AbsolutePathUri string   `protobuf:"bytes,1,opt,name=absolute_path_uri,json=absolutePathUri,proto3" json:"absolute_path_uri,omitempty"`
	CellIds         []string `protobuf:"bytes,2,rep,name=cell_ids,json=cellIds,proto3" json:"cell_ids,omitempty"`
	CellIndices     []int32  `protobuf:"varint,3,rep,packed,name=cell_indices,json=cellIndices,proto3" json:"cell_indices,omitempty"`
}

type ReadNotebookResponse struct {
	FlattenedContent string `protobuf:"bytes,1,opt,name=flattened_content,json=flattenedContent,proto3" json:"flattened_content,omitempty"`
	Blobs            []byte `protobuf:"bytes,2,opt,name=blobs,proto3" json:"blobs,omitempty"`
	ReadCellCount    int32  `protobuf:"varint,3,opt,name=read_cell_count,json=readCellCount,proto3" json:"read_cell_count,omitempty"`
}

// ExecuteNotebookResponse_CellOutput is the per-cell output entry.
type ExecuteNotebookResponse_CellOutput struct {
	CellId            string                            `protobuf:"bytes,1,opt,name=cell_id,json=cellId,proto3" json:"cell_id,omitempty"`
	CellIndex         int32                             `protobuf:"varint,2,opt,name=cell_index,json=cellIndex,proto3" json:"cell_index,omitempty"`
	Status            ExecuteNotebookResponse_CellStatus `protobuf:"varint,3,opt,name=status,proto3,enum=extension_server_pb.ExecuteNotebookResponse_CellStatus" json:"status,omitempty"`
	OutputText        string                            `protobuf:"bytes,4,opt,name=output_text,json=outputText,proto3" json:"output_text,omitempty"`
	OutputTextTruncated bool                            `protobuf:"varint,5,opt,name=output_text_truncated,json=outputTextTruncated,proto3" json:"output_text_truncated,omitempty"`
	ErrorTrace        string                            `protobuf:"bytes,6,opt,name=error_trace,json=errorTrace,proto3" json:"error_trace,omitempty"`
	Blobs             []byte                            `protobuf:"bytes,7,opt,name=blobs,proto3" json:"blobs,omitempty"`
	GeneratedImages   [][]byte                          `protobuf:"bytes,8,rep,name=generated_images,json=generatedImages,proto3" json:"generated_images,omitempty"`
}

type ExecuteNotebookRequest struct {
	AbsolutePathUri string   `protobuf:"bytes,1,opt,name=absolute_path_uri,json=absolutePathUri,proto3" json:"absolute_path_uri,omitempty"`
	CellIds         []string `protobuf:"bytes,2,rep,name=cell_ids,json=cellIds,proto3" json:"cell_ids,omitempty"`
	CellIndices     []int32  `protobuf:"varint,3,rep,packed,name=cell_indices,json=cellIndices,proto3" json:"cell_indices,omitempty"`
	StopOnError     bool     `protobuf:"varint,4,opt,name=stop_on_error,json=stopOnError,proto3" json:"stop_on_error,omitempty"`
	RestartKernel   bool     `protobuf:"varint,5,opt,name=restart_kernel,json=restartKernel,proto3" json:"restart_kernel,omitempty"`
}

type ExecuteNotebookResponse struct {
	Status             ExecuteNotebookResponse_ExecutionStatus `protobuf:"varint,1,opt,name=status,proto3,enum=extension_server_pb.ExecuteNotebookResponse_ExecutionStatus" json:"status,omitempty"`
	CellOutputs        []*ExecuteNotebookResponse_CellOutput    `protobuf:"bytes,2,rep,name=cell_outputs,json=cellOutputs,proto3" json:"cell_outputs,omitempty"`
	ErrorTrace         string                                   `protobuf:"bytes,3,opt,name=error_trace,json=errorTrace,proto3" json:"error_trace,omitempty"`
	ExecutedCellsCount int32                                    `protobuf:"varint,4,opt,name=executed_cells_count,json=executedCellsCount,proto3" json:"executed_cells_count,omitempty"`
}

type UpdateCascadeTrajectorySummariesRequest struct {
	Summaries []string `protobuf:"bytes,1,rep,name=summaries,proto3" json:"summaries,omitempty"`
}

type UpdateCascadeTrajectorySummariesResponse struct{}

type WriteCascadeEditRequest struct {
	Uri           string `protobuf:"bytes,1,opt,name=uri,proto3" json:"uri,omitempty"`
	TargetContent string `protobuf:"bytes,2,opt,name=target_content,json=targetContent,proto3" json:"target_content,omitempty"`
}

type WriteCascadeEditResponse struct{}

type UpdateDetailedViewWithCascadeInputRequest struct {
	Request  string `protobuf:"bytes,1,opt,name=request,proto3" json:"request,omitempty"`
	Response string `protobuf:"bytes,2,opt,name=response,proto3" json:"response,omitempty"`
}

type UpdateDetailedViewWithCascadeInputResponse struct{}

// ---------------------------------------------------------------------------
// ExtensionServerServiceServer is the recovered gRPC/Connect-RPC server
// interface. The 53 method names correspond 1:1 to the
// UnimplementedExtensionServerServiceServer funcs at pclntab 0x7327000-
// 0x732a000.
// ---------------------------------------------------------------------------

// ExtensionServerServiceServer is the server interface for the
// 53-method ExtensionServerService.
type ExtensionServerServiceServer interface {
	IsAgentManagerEnabled(ctx context.Context, req *IsAgentManagerEnabledRequest) (*IsAgentManagerEnabledResponse, error)
	Heartbeat(ctx context.Context, req *HeartbeatRequest) (*HeartbeatResponse, error)
	LanguageServerStarted(ctx context.Context, req *LanguageServerStartedRequest) (*LanguageServerStartedResponse, error)
	LaunchBrowser(ctx context.Context, req *LaunchBrowserRequest) (*LaunchBrowserResponse, error)
	GetBrowserOnboardingPort(ctx context.Context, req *GetBrowserOnboardingPortRequest) (*GetBrowserOnboardingPortResponse, error)
	GetChromeDevtoolsMcpUrl(ctx context.Context, req *GetChromeDevtoolsMcpUrlRequest) (*GetChromeDevtoolsMcpUrlResponse, error)
	OpenExternalUrl(ctx context.Context, req *OpenExternalUrlRequest) (*OpenExternalUrlResponse, error)
	CheckTerminalShellSupport(ctx context.Context, req *CheckTerminalShellSupportRequest) (*CheckTerminalShellSupportResponse, error)
	OpenTerminal(ctx context.Context, req *OpenTerminalRequest) (*OpenTerminalResponse, error)
	ShowTerminal(ctx context.Context, req *ShowTerminalRequest) (*ShowTerminalResponse, error)
	ReadTerminal(ctx context.Context, req *ReadTerminalRequest) (*ReadTerminalResponse, error)
	SendTerminalInput(ctx context.Context, req *SendTerminalInputRequest) (*SendTerminalInputResponse, error)
	TerminateCommand(ctx context.Context, req *TerminateCommandRequest) (*TerminateCommandResponse, error)
	ExecuteCommand(req *ExecuteCommandRequest, stream ExtensionServerService_ExecuteCommandServer) error
	TerminalResearchResult(ctx context.Context, req *TerminalResearchResultRequest) (*TerminalResearchResultResponse, error)
	RunExtensionCode(ctx context.Context, req *RunExtensionCodeRequest) (*RunExtensionCodeResponse, error)
	OpenFilePointer(ctx context.Context, req *OpenFilePointerRequest) (*OpenFilePointerResponse, error)
	OpenVirtualFile(ctx context.Context, req *OpenVirtualFileRequest) (*OpenVirtualFileResponse, error)
	SaveDocument(ctx context.Context, req *SaveDocumentRequest) (*SaveDocumentResponse, error)
	InsertCodeAtCursor(ctx context.Context, req *InsertCodeAtCursorRequest) (*InsertCodeAtCursorResponse, error)
	OpenDiffZones(ctx context.Context, req *OpenDiffZonesRequest) (*OpenDiffZonesResponse, error)
	OpenSetting(ctx context.Context, req *OpenSettingRequest) (*OpenSettingResponse, error)
	OpenAntigravityRulesFile(ctx context.Context, req *OpenAntigravityRulesFileRequest) (*OpenAntigravityRulesFileResponse, error)
	OpenPluginPage(ctx context.Context, req *OpenPluginPageRequest) (*OpenPluginPageResponse, error)
	OpenPluginConfigModal(ctx context.Context, req *OpenPluginConfigModalRequest) (*OpenPluginConfigModalResponse, error)
	OpenConfigurePluginsPage(ctx context.Context, req *OpenConfigurePluginsPageRequest) (*OpenConfigurePluginsPageResponse, error)
	OpenConversationWorkspaceQuickPick(ctx context.Context, req *OpenConversationWorkspaceQuickPickRequest) (*OpenConversationWorkspaceQuickPickResponse, error)
	GetSecretValue(ctx context.Context, req *GetSecretValueRequest) (*GetSecretValueResponse, error)
	StoreSecretValue(ctx context.Context, req *StoreSecretValueRequest) (*StoreSecretValueResponse, error)
	LogEvent(ctx context.Context, req *LogEventRequest) (*LogEventResponse, error)
	RecordError(ctx context.Context, req *RecordErrorRequest) (*RecordErrorResponse, error)
	RestartUserStatusUpdater(ctx context.Context, req *RestartUserStatusUpdaterRequest) (*RestartUserStatusUpdaterResponse, error)
	HandleAsyncPostMessage(ctx context.Context, req *HandleAsyncPostMessageRequest) (*HandleAsyncPostMessageResponse, error)
	HandleProposeCodeExtensionVerification(ctx context.Context, req *HandleProposeCodeExtensionVerificationRequest) (*HandleProposeCodeExtensionVerificationResponse, error)
	FocusIDEWindow(ctx context.Context, req *FocusIDEWindowRequest) (*FocusIDEWindowResponse, error)
	PlaySound(ctx context.Context, req *PlaySoundRequest) (*PlaySoundResponse, error)
	FindAllReferences(ctx context.Context, req *FindAllReferencesRequest) (*FindAllReferencesResponse, error)
	GetDefinition(ctx context.Context, req *GetDefinitionRequest) (*GetDefinitionResponse, error)
	GetLintErrors(ctx context.Context, req *GetLintErrorsRequest) (*GetLintErrorsResponse, error)
	ShowConversationPicker(ctx context.Context, req *ShowConversationPickerRequest) (*ShowConversationPickerResponse, error)
	BroadcastConversationDeletion(ctx context.Context, req *BroadcastConversationDeletionRequest) (*BroadcastConversationDeletionResponse, error)
	SmartFocusConversation(ctx context.Context, req *SmartFocusConversationRequest) (*SmartFocusConversationResponse, error)
	SubscribeToUnifiedStateSyncTopic(req *SubscribeToUnifiedStateSyncTopicRequest, stream ExtensionServerService_SubscribeToUnifiedStateSyncTopicServer) error
	PushUnifiedStateSyncUpdate(ctx context.Context, req *PushUnifiedStateSyncUpdateRequest) (*PushUnifiedStateSyncUpdateResponse, error)
	StartAudioRecording(ctx context.Context, req *StartAudioRecordingRequest) (*StartAudioRecordingResponse, error)
	GetCurrentAudioRecording(ctx context.Context, req *GetCurrentAudioRecordingRequest) (*GetCurrentAudioRecordingResponse, error)
	EndAudioRecording(ctx context.Context, req *EndAudioRecordingRequest) (*EndAudioRecordingResponse, error)
	EditNotebook(ctx context.Context, req *EditNotebookRequest) (*EditNotebookResponse, error)
	ReadNotebook(ctx context.Context, req *ReadNotebookRequest) (*ReadNotebookResponse, error)
	ExecuteNotebook(ctx context.Context, req *ExecuteNotebookRequest) (*ExecuteNotebookResponse, error)
	UpdateCascadeTrajectorySummaries(ctx context.Context, req *UpdateCascadeTrajectorySummariesRequest) (*UpdateCascadeTrajectorySummariesResponse, error)
	WriteCascadeEdit(ctx context.Context, req *WriteCascadeEditRequest) (*WriteCascadeEditResponse, error)
	UpdateDetailedViewWithCascadeInput(ctx context.Context, req *UpdateDetailedViewWithCascadeInputRequest) (*UpdateDetailedViewWithCascadeInputResponse, error)
}

// ExtensionServerServiceClient is the recovered client interface (mirrors
// the 53 server methods).
type ExtensionServerServiceClient interface {
	IsAgentManagerEnabled(ctx context.Context, req *IsAgentManagerEnabledRequest) (*IsAgentManagerEnabledResponse, error)
	Heartbeat(ctx context.Context, req *HeartbeatRequest) (*HeartbeatResponse, error)
	LanguageServerStarted(ctx context.Context, req *LanguageServerStartedRequest) (*LanguageServerStartedResponse, error)
	LaunchBrowser(ctx context.Context, req *LaunchBrowserRequest) (*LaunchBrowserResponse, error)
	GetBrowserOnboardingPort(ctx context.Context, req *GetBrowserOnboardingPortRequest) (*GetBrowserOnboardingPortResponse, error)
	GetChromeDevtoolsMcpUrl(ctx context.Context, req *GetChromeDevtoolsMcpUrlRequest) (*GetChromeDevtoolsMcpUrlResponse, error)
	OpenExternalUrl(ctx context.Context, req *OpenExternalUrlRequest) (*OpenExternalUrlResponse, error)
	CheckTerminalShellSupport(ctx context.Context, req *CheckTerminalShellSupportRequest) (*CheckTerminalShellSupportResponse, error)
	OpenTerminal(ctx context.Context, req *OpenTerminalRequest) (*OpenTerminalResponse, error)
	ShowTerminal(ctx context.Context, req *ShowTerminalRequest) (*ShowTerminalResponse, error)
	ReadTerminal(ctx context.Context, req *ReadTerminalRequest) (*ReadTerminalResponse, error)
	SendTerminalInput(ctx context.Context, req *SendTerminalInputRequest) (*SendTerminalInputResponse, error)
	TerminateCommand(ctx context.Context, req *TerminateCommandRequest) (*TerminateCommandResponse, error)
	ExecuteCommand(ctx context.Context, req *ExecuteCommandRequest) (ExtensionServerService_ExecuteCommandClient, error)
	TerminalResearchResult(ctx context.Context, req *TerminalResearchResultRequest) (*TerminalResearchResultResponse, error)
	RunExtensionCode(ctx context.Context, req *RunExtensionCodeRequest) (*RunExtensionCodeResponse, error)
	OpenFilePointer(ctx context.Context, req *OpenFilePointerRequest) (*OpenFilePointerResponse, error)
	OpenVirtualFile(ctx context.Context, req *OpenVirtualFileRequest) (*OpenVirtualFileResponse, error)
	SaveDocument(ctx context.Context, req *SaveDocumentRequest) (*SaveDocumentResponse, error)
	InsertCodeAtCursor(ctx context.Context, req *InsertCodeAtCursorRequest) (*InsertCodeAtCursorResponse, error)
	OpenDiffZones(ctx context.Context, req *OpenDiffZonesRequest) (*OpenDiffZonesResponse, error)
	OpenSetting(ctx context.Context, req *OpenSettingRequest) (*OpenSettingResponse, error)
	OpenAntigravityRulesFile(ctx context.Context, req *OpenAntigravityRulesFileRequest) (*OpenAntigravityRulesFileResponse, error)
	OpenPluginPage(ctx context.Context, req *OpenPluginPageRequest) (*OpenPluginPageResponse, error)
	OpenPluginConfigModal(ctx context.Context, req *OpenPluginConfigModalRequest) (*OpenPluginConfigModalResponse, error)
	OpenConfigurePluginsPage(ctx context.Context, req *OpenConfigurePluginsPageRequest) (*OpenConfigurePluginsPageResponse, error)
	OpenConversationWorkspaceQuickPick(ctx context.Context, req *OpenConversationWorkspaceQuickPickRequest) (*OpenConversationWorkspaceQuickPickResponse, error)
	GetSecretValue(ctx context.Context, req *GetSecretValueRequest) (*GetSecretValueResponse, error)
	StoreSecretValue(ctx context.Context, req *StoreSecretValueRequest) (*StoreSecretValueResponse, error)
	LogEvent(ctx context.Context, req *LogEventRequest) (*LogEventResponse, error)
	RecordError(ctx context.Context, req *RecordErrorRequest) (*RecordErrorResponse, error)
	RestartUserStatusUpdater(ctx context.Context, req *RestartUserStatusUpdaterRequest) (*RestartUserStatusUpdaterResponse, error)
	HandleAsyncPostMessage(ctx context.Context, req *HandleAsyncPostMessageRequest) (*HandleAsyncPostMessageResponse, error)
	HandleProposeCodeExtensionVerification(ctx context.Context, req *HandleProposeCodeExtensionVerificationRequest) (*HandleProposeCodeExtensionVerificationResponse, error)
	FocusIDEWindow(ctx context.Context, req *FocusIDEWindowRequest) (*FocusIDEWindowResponse, error)
	PlaySound(ctx context.Context, req *PlaySoundRequest) (*PlaySoundResponse, error)
	FindAllReferences(ctx context.Context, req *FindAllReferencesRequest) (*FindAllReferencesResponse, error)
	GetDefinition(ctx context.Context, req *GetDefinitionRequest) (*GetDefinitionResponse, error)
	GetLintErrors(ctx context.Context, req *GetLintErrorsRequest) (*GetLintErrorsResponse, error)
	ShowConversationPicker(ctx context.Context, req *ShowConversationPickerRequest) (*ShowConversationPickerResponse, error)
	BroadcastConversationDeletion(ctx context.Context, req *BroadcastConversationDeletionRequest) (*BroadcastConversationDeletionResponse, error)
	SmartFocusConversation(ctx context.Context, req *SmartFocusConversationRequest) (*SmartFocusConversationResponse, error)
	SubscribeToUnifiedStateSyncTopic(ctx context.Context, req *SubscribeToUnifiedStateSyncTopicRequest) (ExtensionServerService_SubscribeToUnifiedStateSyncTopicClient, error)
	PushUnifiedStateSyncUpdate(ctx context.Context, req *PushUnifiedStateSyncUpdateRequest) (*PushUnifiedStateSyncUpdateResponse, error)
	StartAudioRecording(ctx context.Context, req *StartAudioRecordingRequest) (*StartAudioRecordingResponse, error)
	GetCurrentAudioRecording(ctx context.Context, req *GetCurrentAudioRecordingRequest) (*GetCurrentAudioRecordingResponse, error)
	EndAudioRecording(ctx context.Context, req *EndAudioRecordingRequest) (*EndAudioRecordingResponse, error)
	EditNotebook(ctx context.Context, req *EditNotebookRequest) (*EditNotebookResponse, error)
	ReadNotebook(ctx context.Context, req *ReadNotebookRequest) (*ReadNotebookResponse, error)
	ExecuteNotebook(ctx context.Context, req *ExecuteNotebookRequest) (*ExecuteNotebookResponse, error)
	UpdateCascadeTrajectorySummaries(ctx context.Context, req *UpdateCascadeTrajectorySummariesRequest) (*UpdateCascadeTrajectorySummariesResponse, error)
	WriteCascadeEdit(ctx context.Context, req *WriteCascadeEditRequest) (*WriteCascadeEditResponse, error)
	UpdateDetailedViewWithCascadeInput(ctx context.Context, req *UpdateDetailedViewWithCascadeInputRequest) (*UpdateDetailedViewWithCascadeInputResponse, error)
	Close() error
}

// ExtensionServerService_ExecuteCommandServer is the server-side
// streaming interface for ExecuteCommand (one of two confirmed
// server-stream RPCs in the binary).
type ExtensionServerService_ExecuteCommandServer interface {
	Send(msg any) error
	Context() context.Context
}

// ExtensionServerService_ExecuteCommandClient is the client-side
// streaming interface for ExecuteCommand.
type ExtensionServerService_ExecuteCommandClient interface {
	Recv() (any, error)
	Context() context.Context
	Close() error
}

// ExtensionServerService_SubscribeToUnifiedStateSyncTopicServer is the
// server-side streaming interface for SubscribeToUnifiedStateSyncTopic.
type ExtensionServerService_SubscribeToUnifiedStateSyncTopicServer interface {
	Send(*UnifiedStateSyncUpdate) error
	Context() context.Context
}

// ExtensionServerService_SubscribeToUnifiedStateSyncTopicClient is the
// client-side streaming interface for SubscribeToUnifiedStateSyncTopic.
type ExtensionServerService_SubscribeToUnifiedStateSyncTopicClient interface {
	Recv() (*UnifiedStateSyncUpdate, error)
	Context() context.Context
	Close() error
}

// ---------------------------------------------------------------------------
// Placeholder client. Same shape as internal/language_server_pb.
// ---------------------------------------------------------------------------

// DialOptions configures the placeholder client.
type DialOptions struct {
	// Endpoint is the local antigravity language server endpoint
	// (default: http://127.0.0.1:8787).
	Endpoint string
	// HTTPClient is the underlying *http.Client. If nil, a default
	// 60s-timeout client is used.
	HTTPClient *http.Client
	// BearerToken is the OAuth access token used for auth (some
	// sub-services authenticate via Connect Bearer auth).
	BearerToken func() (string, error)
}

// DialExtensionServerService creates an ExtensionServerServiceClient that
// can dial the local antigravity language server. The returned client
// preserves the recovered wire format but does not actually call the
// RPCs (calls return ErrNotImplemented or a structured placeholder).
func DialExtensionServerService(opts DialOptions) (ExtensionServerServiceClient, error) {
	if opts.Endpoint == "" {
		opts.Endpoint = "http://127.0.0.1:8787"
	}
	if opts.HTTPClient == nil {
		opts.HTTPClient = &http.Client{Timeout: 60 * time.Second}
	}
	return &esClient{opts: opts}, nil
}

type esClient struct {
	opts DialOptions
	mu   sync.Mutex
}

func (c *esClient) Close() error { return nil }

// placeholderUnary returns ErrNotImplemented for any unary RPC. The 50+
// unary methods all delegate here.
func (c *esClient) placeholderUnary(name string) error {
	return errors.New("extension_server_pb: " + name + " not implemented in recovered build")
}

// IsAgentManagerEnabled — RPC 1.
func (c *esClient) IsAgentManagerEnabled(ctx context.Context, req *IsAgentManagerEnabledRequest) (*IsAgentManagerEnabledResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("IsAgentManagerEnabled")
}

// Heartbeat — RPC 2.
func (c *esClient) Heartbeat(ctx context.Context, req *HeartbeatRequest) (*HeartbeatResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("Heartbeat")
}

// LanguageServerStarted — RPC 3.
func (c *esClient) LanguageServerStarted(ctx context.Context, req *LanguageServerStartedRequest) (*LanguageServerStartedResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("LanguageServerStarted")
}

// LaunchBrowser — RPC 4.
func (c *esClient) LaunchBrowser(ctx context.Context, req *LaunchBrowserRequest) (*LaunchBrowserResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("LaunchBrowser")
}

// GetBrowserOnboardingPort — RPC 5.
func (c *esClient) GetBrowserOnboardingPort(ctx context.Context, req *GetBrowserOnboardingPortRequest) (*GetBrowserOnboardingPortResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("GetBrowserOnboardingPort")
}

// GetChromeDevtoolsMcpUrl — RPC 6.
func (c *esClient) GetChromeDevtoolsMcpUrl(ctx context.Context, req *GetChromeDevtoolsMcpUrlRequest) (*GetChromeDevtoolsMcpUrlResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("GetChromeDevtoolsMcpUrl")
}

// OpenExternalUrl — RPC 7.
func (c *esClient) OpenExternalUrl(ctx context.Context, req *OpenExternalUrlRequest) (*OpenExternalUrlResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("OpenExternalUrl")
}

// CheckTerminalShellSupport — RPC 8.
func (c *esClient) CheckTerminalShellSupport(ctx context.Context, req *CheckTerminalShellSupportRequest) (*CheckTerminalShellSupportResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("CheckTerminalShellSupport")
}

// OpenTerminal — RPC 9.
func (c *esClient) OpenTerminal(ctx context.Context, req *OpenTerminalRequest) (*OpenTerminalResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("OpenTerminal")
}

// ShowTerminal — RPC 10.
func (c *esClient) ShowTerminal(ctx context.Context, req *ShowTerminalRequest) (*ShowTerminalResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("ShowTerminal")
}

// ReadTerminal — RPC 11.
func (c *esClient) ReadTerminal(ctx context.Context, req *ReadTerminalRequest) (*ReadTerminalResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("ReadTerminal")
}

// SendTerminalInput — RPC 12.
func (c *esClient) SendTerminalInput(ctx context.Context, req *SendTerminalInputRequest) (*SendTerminalInputResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("SendTerminalInput")
}

// TerminateCommand — RPC 13.
func (c *esClient) TerminateCommand(ctx context.Context, req *TerminateCommandRequest) (*TerminateCommandResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("TerminateCommand")
}

// ExecuteCommand — RPC 14, server-streaming.
func (c *esClient) ExecuteCommand(ctx context.Context, req *ExecuteCommandRequest) (ExtensionServerService_ExecuteCommandClient, error) {
	return &placeholderStreamClient{ctx: ctx}, nil
}

// TerminalResearchResult — RPC 15.
func (c *esClient) TerminalResearchResult(ctx context.Context, req *TerminalResearchResultRequest) (*TerminalResearchResultResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("TerminalResearchResult")
}

// RunExtensionCode — RPC 16.
func (c *esClient) RunExtensionCode(ctx context.Context, req *RunExtensionCodeRequest) (*RunExtensionCodeResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("RunExtensionCode")
}

// OpenFilePointer — RPC 17.
func (c *esClient) OpenFilePointer(ctx context.Context, req *OpenFilePointerRequest) (*OpenFilePointerResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("OpenFilePointer")
}

// OpenVirtualFile — RPC 18.
func (c *esClient) OpenVirtualFile(ctx context.Context, req *OpenVirtualFileRequest) (*OpenVirtualFileResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("OpenVirtualFile")
}

// SaveDocument — RPC 19.
func (c *esClient) SaveDocument(ctx context.Context, req *SaveDocumentRequest) (*SaveDocumentResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("SaveDocument")
}

// InsertCodeAtCursor — RPC 20.
func (c *esClient) InsertCodeAtCursor(ctx context.Context, req *InsertCodeAtCursorRequest) (*InsertCodeAtCursorResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("InsertCodeAtCursor")
}

// OpenDiffZones — RPC 21.
func (c *esClient) OpenDiffZones(ctx context.Context, req *OpenDiffZonesRequest) (*OpenDiffZonesResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("OpenDiffZones")
}

// OpenSetting — RPC 22.
func (c *esClient) OpenSetting(ctx context.Context, req *OpenSettingRequest) (*OpenSettingResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("OpenSetting")
}

// OpenAntigravityRulesFile — RPC 23.
func (c *esClient) OpenAntigravityRulesFile(ctx context.Context, req *OpenAntigravityRulesFileRequest) (*OpenAntigravityRulesFileResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("OpenAntigravityRulesFile")
}

// OpenPluginPage — RPC 24.
func (c *esClient) OpenPluginPage(ctx context.Context, req *OpenPluginPageRequest) (*OpenPluginPageResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("OpenPluginPage")
}

// OpenPluginConfigModal — RPC 25.
func (c *esClient) OpenPluginConfigModal(ctx context.Context, req *OpenPluginConfigModalRequest) (*OpenPluginConfigModalResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("OpenPluginConfigModal")
}

// OpenConfigurePluginsPage — RPC 26.
func (c *esClient) OpenConfigurePluginsPage(ctx context.Context, req *OpenConfigurePluginsPageRequest) (*OpenConfigurePluginsPageResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("OpenConfigurePluginsPage")
}

// OpenConversationWorkspaceQuickPick — RPC 27.
func (c *esClient) OpenConversationWorkspaceQuickPick(ctx context.Context, req *OpenConversationWorkspaceQuickPickRequest) (*OpenConversationWorkspaceQuickPickResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("OpenConversationWorkspaceQuickPick")
}

// GetSecretValue — RPC 28.
func (c *esClient) GetSecretValue(ctx context.Context, req *GetSecretValueRequest) (*GetSecretValueResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("GetSecretValue")
}

// StoreSecretValue — RPC 29.
func (c *esClient) StoreSecretValue(ctx context.Context, req *StoreSecretValueRequest) (*StoreSecretValueResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("StoreSecretValue")
}

// LogEvent — RPC 30.
func (c *esClient) LogEvent(ctx context.Context, req *LogEventRequest) (*LogEventResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("LogEvent")
}

// RecordError — RPC 31.
func (c *esClient) RecordError(ctx context.Context, req *RecordErrorRequest) (*RecordErrorResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("RecordError")
}

// RestartUserStatusUpdater — RPC 32.
func (c *esClient) RestartUserStatusUpdater(ctx context.Context, req *RestartUserStatusUpdaterRequest) (*RestartUserStatusUpdaterResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("RestartUserStatusUpdater")
}

// HandleAsyncPostMessage — RPC 33.
func (c *esClient) HandleAsyncPostMessage(ctx context.Context, req *HandleAsyncPostMessageRequest) (*HandleAsyncPostMessageResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("HandleAsyncPostMessage")
}

// HandleProposeCodeExtensionVerification — RPC 34.
func (c *esClient) HandleProposeCodeExtensionVerification(ctx context.Context, req *HandleProposeCodeExtensionVerificationRequest) (*HandleProposeCodeExtensionVerificationResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("HandleProposeCodeExtensionVerification")
}

// FocusIDEWindow — RPC 35.
func (c *esClient) FocusIDEWindow(ctx context.Context, req *FocusIDEWindowRequest) (*FocusIDEWindowResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("FocusIDEWindow")
}

// PlaySound — RPC 36.
func (c *esClient) PlaySound(ctx context.Context, req *PlaySoundRequest) (*PlaySoundResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("PlaySound")
}

// FindAllReferences — RPC 37.
func (c *esClient) FindAllReferences(ctx context.Context, req *FindAllReferencesRequest) (*FindAllReferencesResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("FindAllReferences")
}

// GetDefinition — RPC 38.
func (c *esClient) GetDefinition(ctx context.Context, req *GetDefinitionRequest) (*GetDefinitionResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("GetDefinition")
}

// GetLintErrors — RPC 39.
func (c *esClient) GetLintErrors(ctx context.Context, req *GetLintErrorsRequest) (*GetLintErrorsResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("GetLintErrors")
}

// ShowConversationPicker — RPC 40.
func (c *esClient) ShowConversationPicker(ctx context.Context, req *ShowConversationPickerRequest) (*ShowConversationPickerResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("ShowConversationPicker")
}

// BroadcastConversationDeletion — RPC 41.
func (c *esClient) BroadcastConversationDeletion(ctx context.Context, req *BroadcastConversationDeletionRequest) (*BroadcastConversationDeletionResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("BroadcastConversationDeletion")
}

// SmartFocusConversation — RPC 42.
func (c *esClient) SmartFocusConversation(ctx context.Context, req *SmartFocusConversationRequest) (*SmartFocusConversationResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("SmartFocusConversation")
}

// SubscribeToUnifiedStateSyncTopic — RPC 43, server-streaming.
func (c *esClient) SubscribeToUnifiedStateSyncTopic(ctx context.Context, req *SubscribeToUnifiedStateSyncTopicRequest) (ExtensionServerService_SubscribeToUnifiedStateSyncTopicClient, error) {
	return &placeholderUnifiedStateStreamClient{ctx: ctx}, nil
}

// PushUnifiedStateSyncUpdate — RPC 44.
func (c *esClient) PushUnifiedStateSyncUpdate(ctx context.Context, req *PushUnifiedStateSyncUpdateRequest) (*PushUnifiedStateSyncUpdateResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("PushUnifiedStateSyncUpdate")
}

// StartAudioRecording — RPC 45.
func (c *esClient) StartAudioRecording(ctx context.Context, req *StartAudioRecordingRequest) (*StartAudioRecordingResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("StartAudioRecording")
}

// GetCurrentAudioRecording — RPC 46.
func (c *esClient) GetCurrentAudioRecording(ctx context.Context, req *GetCurrentAudioRecordingRequest) (*GetCurrentAudioRecordingResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("GetCurrentAudioRecording")
}

// EndAudioRecording — RPC 47.
func (c *esClient) EndAudioRecording(ctx context.Context, req *EndAudioRecordingRequest) (*EndAudioRecordingResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("EndAudioRecording")
}

// EditNotebook — RPC 48.
func (c *esClient) EditNotebook(ctx context.Context, req *EditNotebookRequest) (*EditNotebookResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("EditNotebook")
}

// ReadNotebook — RPC 49.
func (c *esClient) ReadNotebook(ctx context.Context, req *ReadNotebookRequest) (*ReadNotebookResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("ReadNotebook")
}

// ExecuteNotebook — RPC 50.
func (c *esClient) ExecuteNotebook(ctx context.Context, req *ExecuteNotebookRequest) (*ExecuteNotebookResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("ExecuteNotebook")
}

// UpdateCascadeTrajectorySummaries — RPC 51.
func (c *esClient) UpdateCascadeTrajectorySummaries(ctx context.Context, req *UpdateCascadeTrajectorySummariesRequest) (*UpdateCascadeTrajectorySummariesResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("UpdateCascadeTrajectorySummaries")
}

// WriteCascadeEdit — RPC 52.
func (c *esClient) WriteCascadeEdit(ctx context.Context, req *WriteCascadeEditRequest) (*WriteCascadeEditResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("WriteCascadeEdit")
}

// UpdateDetailedViewWithCascadeInput — RPC 53.
func (c *esClient) UpdateDetailedViewWithCascadeInput(ctx context.Context, req *UpdateDetailedViewWithCascadeInputRequest) (*UpdateDetailedViewWithCascadeInputResponse, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return nil, c.placeholderUnary("UpdateDetailedViewWithCascadeInput")
}

// placeholderStreamClient is the placeholder for the ExecuteCommand
// server-streaming client.
type placeholderStreamClient struct {
	ctx context.Context
}

func (p *placeholderStreamClient) Recv() (any, error)        { return nil, ErrNotImplemented }
func (p *placeholderStreamClient) Context() context.Context   { return p.ctx }
func (p *placeholderStreamClient) Close() error              { return nil }

// placeholderUnifiedStateStreamClient is the placeholder for
// SubscribeToUnifiedStateSyncTopic client.
type placeholderUnifiedStateStreamClient struct {
	ctx context.Context
}

func (p *placeholderUnifiedStateStreamClient) Recv() (*UnifiedStateSyncUpdate, error) {
	return nil, ErrNotImplemented
}
func (p *placeholderUnifiedStateStreamClient) Context() context.Context { return p.ctx }
func (p *placeholderUnifiedStateStreamClient) Close() error            { return nil }
