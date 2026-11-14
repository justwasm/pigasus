package mockremoteagent

// Environment represents an execution environment in the remote agent proxy.
type Environment struct {
	Capsule         map[string]any `json:"capsule,omitempty"`
	Ace             map[string]any `json:"ace,omitempty"`
	IsDefault       bool           `json:"isDefault,omitempty"`
	ConnectionState string         `json:"connectionState,omitempty"` // e.g. "CONNECTION_STATE_CONNECTED"
}

// EnsureEnvironmentRequest represents the request to EnsureEnvironment.
type EnsureEnvironmentRequest struct {
	Capsule           map[string]any `json:"capsule,omitempty"`
	Ace               map[string]any `json:"ace,omitempty"`
	ExecutionIdentity string         `json:"executionIdentity,omitempty"`
}

// EnsureEnvironmentResponse represents the response to EnsureEnvironment.
type EnsureEnvironmentResponse struct {
	Environment Environment `json:"environment"`
}

// ListEnvironmentsResponse represents the response to ListEnvironments.
type ListEnvironmentsResponse struct {
	Environments []Environment `json:"environments"`
}

// ModelInfo represents an available AI model.
type ModelInfo struct {
	Model       string `json:"model"`
	DisplayName string `json:"displayName"`
	Description string `json:"description"`
}

// ListAvailableModelsResponse represents the response to ListAvailableModels.
type ListAvailableModelsResponse struct {
	Models []ModelInfo `json:"models"`
}

// SlashCommandInfo represents an available slash command.
type SlashCommandInfo struct {
	Name        string `json:"name"`
	Description string `json:"description"`
}

// ListSlashCommandsResponse represents the response to ListSlashCommands.
type ListSlashCommandsResponse struct {
	SlashCommands []SlashCommandInfo `json:"slashCommands"`
}

// TextOrScopeItem represents a text item in a cascade message or prompt.
type TextOrScopeItem struct {
	Text string `json:"text,omitempty"`
}

// ProjectIdentifier represents a project identifier object.
type ProjectIdentifier struct {
	ProjectID string `json:"projectId,omitempty"`
}

// StartCascadeRequest represents the request to StartCascade.
type StartCascadeRequest struct {
	InitialPrompt          string             `json:"initialPrompt,omitempty"`
	UserPrompt             string             `json:"userPrompt,omitempty"`
	Items                  []TextOrScopeItem  `json:"items,omitempty"`
	ProjectID              string             `json:"projectId,omitempty"`
	ProjectIdentifier      *ProjectIdentifier `json:"projectIdentifier,omitempty"`
	WorkspaceURIs          []string           `json:"workspaceUris,omitempty"`
	Environment            *Environment       `json:"environment,omitempty"`
	ForceThirdPartyRouting bool               `json:"forceThirdPartyRouting,omitempty"`
	ConfigOverrides        map[string]any     `json:"configOverrides,omitempty"`
	AceVMID                string             `json:"aceVmId,omitempty"`
	Media                  []map[string]any   `json:"media,omitempty"`
	Title                  string             `json:"title,omitempty"`
	Tags                   []string           `json:"tags,omitempty"`
	ClientMessageID        string             `json:"clientMessageId,omitempty"`
	AgentPath              string             `json:"agentPath,omitempty"`
	ClientInfo             map[string]any     `json:"clientInfo,omitempty"`
	Model                  string             `json:"model,omitempty"`
}

// ExecutionStatus represents execution status in Cascade lifecycle.
// ExecutionStatus is the proto message .exa.jetski_cortex_pb.ExecutionStatus.
// The 1.5.3 client decodes it as an object ({"status":N}); a bare enum number
// is a proto-JSON syntax error.
type ExecutionStatus struct {
	Status int `json:"status"` // 1: IDLE, 2: RUNNING, 3: FAILED, 4: COMPLETED
}

// StartCascadeResponse represents the response to StartCascade.
type StartCascadeResponse struct {
	CascadeID       string          `json:"cascadeId"`
	ExecutionStatus ExecutionStatus `json:"executionStatus"`
}

// SendUserCascadeMessageRequest represents the request to SendUserCascadeMessage.
type SendUserCascadeMessageRequest struct {
	CascadeID        string            `json:"cascadeId"`
	Text             string            `json:"text,omitempty"`
	MessageText      string            `json:"messageText,omitempty"`
	Items            []TextOrScopeItem `json:"items,omitempty"`
	DeliveryStrategy int               `json:"deliveryStrategy,omitempty"`
	ContinueExecution bool              `json:"continueExecution,omitempty"`
	ConfigOverrides  map[string]any    `json:"configOverrides,omitempty"`
	Media            []map[string]any  `json:"media,omitempty"`
	ArtifactComments []map[string]any  `json:"artifactComments,omitempty"`
	FileDiffComments []map[string]any  `json:"fileDiffComments,omitempty"`
	FileComments     []map[string]any  `json:"fileComments,omitempty"`
	Tags             []string          `json:"tags,omitempty"`
	ClientMessageID  string            `json:"clientMessageId,omitempty"`
}

// SendUserCascadeMessageResponse represents the response to SendUserCascadeMessage.
type SendUserCascadeMessageResponse struct {
	CascadeID       string          `json:"cascadeId"`
	ExecutionStatus ExecutionStatus `json:"executionStatus"`
}

// GenerateConversationTitleRequest represents the request to GenerateConversationTitle.
type GenerateConversationTitleRequest struct {
	Prompt string            `json:"prompt"`
	Items  []TextOrScopeItem `json:"items,omitempty"`
}

// GenerateConversationTitleResponse represents the response to GenerateConversationTitle.
type GenerateConversationTitleResponse struct {
	Title string `json:"title"`
}

// CancelCascadeRequest represents the request to CancelCascade.
type CancelCascadeRequest struct {
	CascadeID      string `json:"cascadeId"`
	ConversationID string `json:"conversationId,omitempty"`
}

// CancelCascadeResponse represents the response to CancelCascade.
type CancelCascadeResponse struct {
	Success bool `json:"success"`
}

// StreamAgentStateUpdatesRequest represents the request for streaming updates.
type StreamAgentStateUpdatesRequest struct {
	ConversationID string `json:"conversationId"`
}

// StreamAgentStateUpdatesResponse represents a streaming frame.
type StreamAgentStateUpdatesResponse struct {
	Update                     map[string]any `json:"update"`
	ResumeToken                string         `json:"resumeToken,omitempty"`
	PendingAgentMessagesSeqNum int64          `json:"pendingAgentMessagesSequenceNumber,omitempty"`
	MainTrajectoryTruncation   any            `json:"mainTrajectoryTruncation,omitempty"`
}

// ReadFileRequest represents the request to ReadFile.
type ReadFileRequest struct {
	Path string `json:"path"`
}

// ReadFileResponse represents the response to ReadFile.
type ReadFileResponse struct {
	Content string `json:"content"`
	Exists  bool   `json:"exists"`
}

// ReadDirRequest represents the request to ReadDir.
type ReadDirRequest struct {
	Path string `json:"path"`
}

// DirEntry represents an entry in ReadDirResponse.
type DirEntry struct {
	Name  string `json:"name"`
	IsDir bool   `json:"isDir"`
	Size  int64  `json:"size"`
}

// ReadDirResponse represents the response to ReadDir.
type ReadDirResponse struct {
	Entries []DirEntry `json:"entries"`
}

// ConversationSummary represents a conversation summary item matching exa.jetski_cortex_pb.CascadeTrajectorySummary.
type ConversationSummary struct {
	Summary          string `json:"summary"`
	TrajectoryID     string `json:"trajectoryId"`
	StepCount        int    `json:"stepCount"`
	LastModifiedTime string `json:"lastModifiedTime,omitempty"`
	CreatedTime      string `json:"createdTime,omitempty"`
	Status           string `json:"status,omitempty"`
}

// ListConversationsResponse represents the response to ListConversations.
type ListConversationsResponse struct {
	Conversations []ConversationSummary `json:"conversations"`
}

// GetConversationRequest represents the request to GetConversation.
type GetConversationRequest struct {
	ConversationID string `json:"conversationId"`
}

// GetConversationResponse represents the response to GetConversation matching google.gca.aicode.v1main.GetConversationResponse.
type GetConversationResponse struct {
	ConversationID   string         `json:"conversationId"`
	Update           map[string]any `json:"update,omitempty"`
	ResumeToken      string         `json:"resumeToken,omitempty"`
	ModelDisplayName string         `json:"modelDisplayName,omitempty"`
}

// Steps extracts the list of gemini_coder.Step maps from Update.
func (r *GetConversationResponse) Steps() []map[string]any {
	if r == nil || r.Update == nil {
		return nil
	}
	mtu, ok := r.Update["mainTrajectoryUpdate"].(map[string]any)
	if !ok {
		return nil
	}
	su, ok := mtu["stepsUpdate"].(map[string]any)
	if !ok {
		return nil
	}
	if typed, ok := su["steps"].([]map[string]any); ok {
		return typed
	}
	rawSteps, ok := su["steps"].([]any)
	if !ok {
		return nil
	}
	steps := make([]map[string]any, 0, len(rawSteps))
	for _, s := range rawSteps {
		if m, ok := s.(map[string]any); ok {
			steps = append(steps, m)
		}
	}
	return steps
}

// Steps extracts the slice of trajectory step maps from the update frame.
func (r *StreamAgentStateUpdatesResponse) Steps() []map[string]any {
	if r == nil || r.Update == nil {
		return nil
	}
	mtu, ok := r.Update["mainTrajectoryUpdate"].(map[string]any)
	if !ok {
		return nil
	}
	su, ok := mtu["stepsUpdate"].(map[string]any)
	if !ok {
		return nil
	}
	if typed, ok := su["steps"].([]map[string]any); ok {
		return typed
	}
	rawSteps, ok := su["steps"].([]any)
	if !ok {
		return nil
	}
	steps := make([]map[string]any, 0, len(rawSteps))
	for _, s := range rawSteps {
		if m, ok := s.(map[string]any); ok {
			steps = append(steps, m)
		}
	}
	return steps
}

// ExperimentFlag represents a feature flag from Mendel.
type ExperimentFlag struct {
	Name        string  `json:"name"`
	BoolValue   *bool   `json:"boolValue,omitempty"`
	StringValue *string `json:"stringValue,omitempty"`
	IntValue    *int64  `json:"intValue,omitempty"`
}

// ExperimentConfig represents experiment and feature flag configurations.
type ExperimentConfig struct {
	EnabledExperimentIds []int64          `json:"enabledExperimentIds"`
	ExperimentFlags      []ExperimentFlag `json:"experimentFlags"`
}

// Experience represents an available experience.
type Experience struct {
	Name  string      `json:"name"`
	Model ModelConfig `json:"model"`
}

// ModelConfig represents an available model in FetchConfig.
type ModelConfig struct {
	ModelID           string   `json:"modelId"`
	Name              string   `json:"name,omitempty"`
	DisplayName       string   `json:"displayName"`
	SupportedFeatures []string `json:"supportedFeatures,omitempty"`
}

// SurfaceConfigExperienceGroup represents an experience group in SurfaceConfig.
type SurfaceConfigExperienceGroup struct {
	ExperienceNames   []string `json:"experienceNames"`
	DefaultExperience string   `json:"defaultExperience"`
}

// SurfaceConfig represents the surface configuration in FetchConfig.
type SurfaceConfig struct {
	AgentExperiences SurfaceConfigExperienceGroup `json:"agentExperiences"`
}

// FetchConfigResponse represents the response to GET /v1alpha:fetchConfig.
type FetchConfigResponse struct {
	ExperimentConfig     ExperimentConfig `json:"experimentConfig"`
	AvailableExperiences []Experience     `json:"availableExperiences"`
	AvailableModels      []ModelConfig    `json:"availableModels"`
	AgySurfaceConfig     *SurfaceConfig   `json:"agySurfaceConfig,omitempty"`
}

// ProjectResource represents a resource attached to a cloud project.
type ProjectResource struct {
	Type        string `json:"type,omitempty"`
	ResourceID  string `json:"resourceId,omitempty"`
	DisplayName string `json:"displayName,omitempty"`
	URL         string `json:"url,omitempty"`
	IconURL     string `json:"iconUrl,omitempty"`
	ProjectID   string `json:"projectId,omitempty"`
	MIMEType    string `json:"mimeType,omitempty"`
	SizeBytes   int64  `json:"sizeBytes,omitempty"`
	MountPath   string `json:"mountPath,omitempty"`
}

// ProjectConfig represents a cloud project.
type ProjectConfig struct {
	ProjectID          string            `json:"projectId"`
	Title              string            `json:"title"`
	UpdateTimeMicros   int64             `json:"updateTimeMicros"`
	Archived           bool              `json:"archived"`
	Resources          []ProjectResource `json:"resources"`
	PermissionSettings map[string]any    `json:"permissionSettings,omitempty"`
}

// CreateProjectRequest represents the request to CreateProject.
type CreateProjectRequest struct {
	Project   ProjectConfig `json:"project"`
	ProjectID string        `json:"projectId,omitempty"`
}

// CreateProjectResponse represents the response to CreateProject.
type CreateProjectResponse struct {
	Project ProjectConfig `json:"project"`
}

// ListProjectsResponse represents the response to ListProjects.
type ListProjectsResponse struct {
	Projects []ProjectConfig `json:"projects"`
}

// ConversationConfig represents a conversation configuration in UpsertConversationConfig.
type ConversationConfig struct {
	InteractionID string `json:"interactionId"`
	Title         string `json:"title,omitempty"`
	Archived      bool   `json:"archived,omitempty"`
	Pinned        bool   `json:"pinned,omitempty"`
}

// UpsertConversationConfigRequest represents the request to UpsertConversationConfig.
type UpsertConversationConfigRequest struct {
	Config ConversationConfig `json:"config"`
}

// UpsertConversationConfigResponse represents the response to UpsertConversationConfig.
type UpsertConversationConfigResponse struct {
	Config ConversationConfig `json:"config"`
}

// ListPluginsResponse represents the response to ListPlugins.
type ListPluginsResponse struct {
	Plugins []map[string]any `json:"plugins"`
}

// ListResourceProvidersResponse represents the response to ListResourceProviders.
type ListResourceProvidersResponse struct {
	ResourceProviders []map[string]any `json:"resourceProviders"`
}

// ListProjectResourcesResponse represents the response to ListProjectResources.
type ListProjectResourcesResponse struct {
	Resources []ProjectResource `json:"resources"`
}

// CloudCodeFlag represents a flag entry in CloudCode ListExperimentsResponse.
type CloudCodeFlag struct {
	Name        string  `json:"name"`
	BoolValue   *bool   `json:"boolValue,omitempty"`
	StringValue string  `json:"stringValue,omitempty"`
	IntValue    string  `json:"intValue,omitempty"`
	FloatValue  float64 `json:"floatValue,omitempty"`
}

// ListExperimentsResponse represents the response to CloudCode ListExperiments.
type ListExperimentsResponse struct {
	ExperimentIDs []int64         `json:"experimentIds"`
	Flags         []CloudCodeFlag `json:"flags"`
}

// GetTurnDiffRequest represents the request to GetTurnDiff.
type GetTurnDiffRequest struct {
	ConversationID string `json:"conversationId"`
	StepIndex      uint32 `json:"stepIndex"`
}

// FileDiffData represents diff data for one file, matching
// exa.language_server_pb.FileDiffData. The map key is the path.
type FileDiffData struct {
	Additions        int32  `json:"additions,omitempty"`
	Deletions        int32  `json:"deletions,omitempty"`
	IsArtifactFile   bool   `json:"isArtifactFile,omitempty"`
	OriginalContents string `json:"originalContents,omitempty"`
	ModifiedContents string `json:"modifiedContents,omitempty"`
}

// GetTurnDiffResponse represents the response to GetTurnDiff.
type GetTurnDiffResponse struct {
	FileDiffs      map[string]FileDiffData `json:"fileDiffs"`
	TotalAdditions int32                   `json:"totalAdditions"`
	TotalDeletions int32                   `json:"totalDeletions"`
	TurnStartIndex int32                   `json:"turnStartIndex"`
}

// GetCodeActionDiffRequest represents the request to GetCodeActionDiff.
type GetCodeActionDiffRequest struct {
	ConversationID string `json:"conversationId"`
	StepIndex      int32  `json:"stepIndex"`
}

// GetCodeActionDiffResponse represents the response to GetCodeActionDiff.
type GetCodeActionDiffResponse struct {
	Diff map[string]any `json:"diff,omitempty"`
}

// AskQuestionOption is one choice of an ask_question.
type AskQuestionOption struct {
	ID   string `json:"id"`
	Text string `json:"text"`
}

// AskQuestionResponse is one answered ask_question entry.
type AskQuestionResponse struct {
	Question          string              `json:"question"`
	Options           []AskQuestionOption `json:"options,omitempty"`
	SelectedOptionIDs []string            `json:"selectedOptionIds,omitempty"`
	WriteInResponse   string              `json:"writeInResponse,omitempty"`
	Skipped           bool                `json:"skipped,omitempty"`
}

// AskQuestionInteraction is the answer to an ask_question step.
type AskQuestionInteraction struct {
	Responses []AskQuestionResponse `json:"responses,omitempty"`
	Cancelled bool                  `json:"cancelled,omitempty"`
}

// PermissionInteraction is the verdict on a permission step.
type PermissionInteraction struct {
	Allow               bool   `json:"allow"`
	Scope               string `json:"scope,omitempty"`
	UserDenyInstruction string `json:"userDenyInstruction,omitempty"`
}

// CascadeUserInteraction names the step being answered and carries the answer.
type CascadeUserInteraction struct {
	TrajectoryID string                  `json:"trajectoryId,omitempty"`
	StepIndex    int                     `json:"stepIndex"`
	Permission   *PermissionInteraction  `json:"permission,omitempty"`
	AskQuestion  *AskQuestionInteraction `json:"askQuestion,omitempty"`
}

// HandleCascadeUserInteractionRequest represents the request to HandleCascadeUserInteraction.
type HandleCascadeUserInteractionRequest struct {
	ConversationID string                  `json:"conversationId"`
	CascadeID      string                  `json:"cascadeId"`
	Interaction    *CascadeUserInteraction `json:"interaction"`
}

// HandleCascadeUserInteractionResponse represents the response to HandleCascadeUserInteraction.
type HandleCascadeUserInteractionResponse struct{}

// TerminalInfo represents remote terminal details, matching
// exa.language_server_pb.TerminalInfo.
type TerminalInfo struct {
	TerminalID     string `json:"terminalId"`
	Title          string `json:"title"`
	Cwd            string `json:"cwd,omitempty"`
	Pid            int32  `json:"pid,omitempty"`
	ProjectID      string `json:"projectId,omitempty"`
	WorkspaceURI   string `json:"workspaceUri,omitempty"`
	ConversationID string `json:"conversationId,omitempty"`
	SplitGroupID   string `json:"splitGroupId,omitempty"`
	CreatedAt      string `json:"createdAt,omitempty"`
	Status         string `json:"status,omitempty"`
}

// CreateTerminalRequest represents the request to CreateTerminal.
type CreateTerminalRequest struct {
	ConversationID string `json:"conversationId,omitempty"`
	RequestID      string `json:"requestId,omitempty"`
}

// CreateTerminalResponse represents the response to CreateTerminal.
type CreateTerminalResponse struct {
	Terminal TerminalInfo `json:"terminal"`
}

// ListTerminalsResponse represents the response to ListTerminals.
type ListTerminalsResponse struct {
	Terminals []TerminalInfo `json:"terminals"`
}

// SendTerminalInputRequest represents the request to SendTerminalInput.
type SendTerminalInputRequest struct {
	TerminalID    string `json:"terminalId"`
	Input         []byte `json:"input,omitempty"`
	InputStreamID string `json:"inputStreamId,omitempty"`
	InputOffset   int64  `json:"inputOffset,omitempty"`
}

// CloseTerminalRequest represents the request to CloseTerminal.
type CloseTerminalRequest struct {
	TerminalID string `json:"terminalId"`
}

// StreamTerminalOutputRequest represents the request to StreamTerminalOutput.
type StreamTerminalOutputRequest struct {
	TerminalID   string `json:"terminalId"`
	ResumeOffset int64  `json:"resumeOffset,omitempty"`
}

// ListCustomizationsRequest represents the request to ListCustomizations.
type ListCustomizationsRequest struct {
	WorkspaceURI      string `json:"workspaceUri,omitempty"`
	ExecutionIdentity string `json:"executionIdentity,omitempty"`
}

// UploadProjectFileRequest represents the request to UploadProjectFile.
type UploadProjectFileRequest struct {
	ProjectID     string `json:"projectId,omitempty"`
	FileName      string `json:"fileName,omitempty"`
	Path          string `json:"path,omitempty"`
	Content       []byte `json:"content,omitempty"`
	InlineContent []byte `json:"inlineContent,omitempty"`
	MIMEType      string `json:"mimeType,omitempty"`
	SizeBytes     int64  `json:"sizeBytes,omitempty"`
	Title         string `json:"title,omitempty"`
}

// UploadProjectFileResponse represents the response to UploadProjectFile.
type UploadProjectFileResponse struct {
	Resource ProjectResource `json:"resource"`
}

// SetArtifactReviewModeRequest represents the request to SetArtifactReviewMode.
type SetArtifactReviewModeRequest struct {
	Scope              string `json:"scope,omitempty"`
	ArtifactReviewMode string `json:"artifactReviewMode"`
}

// SetArtifactReviewModeResponse represents the response to SetArtifactReviewMode.
type SetArtifactReviewModeResponse struct {
	Settings map[string]any `json:"settings"`
}

// Trigger represents a registered remote trigger.
type Trigger struct {
	TriggerID                string                 `json:"triggerId,omitempty"`
	State                    string                 `json:"state,omitempty"`
	DisplayName              string                 `json:"displayName,omitempty"`
	Prompt                   string                 `json:"prompt,omitempty"`
	WorkspaceURIs            []string               `json:"workspaceUris,omitempty"`
	Environment              map[string]any         `json:"environment,omitempty"`
	ConfigOverrides          map[string]any         `json:"configOverrides,omitempty"`
	CreateTime               string                 `json:"createTime,omitempty"`
	UpdateTime               string                 `json:"updateTime,omitempty"`
	OptionID                 string                 `json:"optionId,omitempty"`
	Fields                   map[string]FieldValues `json:"fields,omitempty"`
	Summary                  string                 `json:"summary,omitempty"`
	CredentialExpirationTime string                 `json:"credentialExpirationTime,omitempty"`
	ProjectID                string                 `json:"projectId,omitempty"`
	InteractionID            string                 `json:"interactionId,omitempty"`
}

// FieldValues is one RemoteTrigger.fields map value.
type FieldValues struct {
	Values []string `json:"values,omitempty"`
}

// TriggerOption represents an available trigger type.
type TriggerOption struct {
	ID          string `json:"id"`
	DisplayName string `json:"displayName"`
	Description string `json:"description,omitempty"`
	Category    string `json:"category,omitempty"`
}

// ListTriggersResponse represents the response to ListTriggers.
type ListTriggersResponse struct {
	Triggers      []Trigger `json:"triggers"`
	NextPageToken string    `json:"nextPageToken,omitempty"`
}

// ListTriggerOptionsResponse represents the response to ListTriggerOptions.
type ListTriggerOptionsResponse struct {
	TriggerOptions []TriggerOption `json:"triggerOptions"`
}

// CreateTriggerRequest represents the request to CreateTrigger.
type CreateTriggerRequest struct {
	Trigger Trigger `json:"trigger"`
}

// CreateTriggerResponse represents the response to CreateTrigger.
type CreateTriggerResponse struct {
	Trigger Trigger `json:"trigger"`
}

// UpdateTriggerRequest represents the request to UpdateTrigger.
type UpdateTriggerRequest struct {
	Trigger    Trigger  `json:"trigger"`
	UpdateMask []string `json:"updateMask,omitempty"`
}

// UpdateTriggerResponse represents the response to UpdateTrigger.
type UpdateTriggerResponse struct {
	Trigger Trigger `json:"trigger"`
}

// DeleteTriggerRequest represents the request to DeleteTrigger.
type DeleteTriggerRequest struct {
	TriggerID       string `json:"triggerId"`
	AbortActiveRuns bool   `json:"abortActiveRuns,omitempty"`
}
