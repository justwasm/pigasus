// Package codeium_common_pb — secondary message-type file.
//
// Contains all recovered enum types (104 total) from the v1.3.5 binary
// (pclntab 0x660ea00-0x6624000) and additional smaller message types
// not present in codeium_common.go.
//
// Each enum is declared as int32 with a recovery note naming the
// proto enum it corresponds to. Field numbers (for the message-type
// protobuf tags in codeium_common.go) are recovered from pclntab
// getter addresses.
package codeium_common_pb

import "time"

// ---------------------------------------------------------------------------
// Enums (104 total, recovered 1:1 from v1.3.5 binary Enum getters).
// ---------------------------------------------------------------------------

// APIProvider — recovered enum (pclntab codeium_common_go_proto.APIProvider).
type APIProvider int32

const (
	APIProviderUnspecified     APIProvider = 0
	APIProviderCodeiumCloud    APIProvider = 1
	APIProviderOpenAI          APIProvider = 2
	APIProviderAnthropic       APIProvider = 3
	APIProviderGoogle          APIProvider = 4
	APIProviderAzure           APIProvider = 5
	APIProviderBedrock         APIProvider = 6
	APIProviderVertex          APIProvider = 7
	APIProviderMistral         APIProvider = 8
	APIProviderCohere          APIProvider = 9
	APIProviderCustom          APIProvider = 10
)

// AgentBrowserTools — recovered enum (pclntab codeium_common_go_proto.AgentBrowserTools).
type AgentBrowserTools int32

const (
	AgentBrowserToolsUnspecified AgentBrowserTools = 0
	AgentBrowserToolsDisabled    AgentBrowserTools = 1
	AgentBrowserToolsEnabled     AgentBrowserTools = 2
)

// AgentPermissionPreset — the reference's own enum.
//
// 【一手】 2026-10-27: the FileDescriptorProto of
// third_party/jetski/codeium_common_pb/codeium_common.proto in the agy
// 1.4.8 binary (file offset 0x3cd5130). The names and the numbers are
// the descriptor's.
//
// The 1.3.5 recovery guessed four values from the binary's *strings*;
// the descriptor shows seven, and none of the four old names is one of
// them. The wire spells an enum by its name, so the names matter more
// than the numbers.
type AgentPermissionPreset int32

const (
	AgentPermissionPresetUnspecified   AgentPermissionPreset = 0
	AgentPermissionPresetDefault       AgentPermissionPreset = 1
	AgentPermissionPresetRequestReview AgentPermissionPreset = 2
	AgentPermissionPresetTurbo         AgentPermissionPreset = 3
	AgentPermissionPresetVetted        AgentPermissionPreset = 4
	AgentPermissionPresetNone          AgentPermissionPreset = 5
	AgentPermissionPresetAuto          AgentPermissionPreset = 6
)

// AgentPermissionPresetName returns the reference's name for a
// preset — the spelling proto-JSON puts on the wire.
func AgentPermissionPresetName(p AgentPermissionPreset) string {
	switch p {
	case AgentPermissionPresetDefault:
		return "AGENT_PERMISSION_PRESET_DEFAULT"
	case AgentPermissionPresetRequestReview:
		return "AGENT_PERMISSION_PRESET_REQUEST_REVIEW"
	case AgentPermissionPresetTurbo:
		return "AGENT_PERMISSION_PRESET_TURBO"
	case AgentPermissionPresetVetted:
		return "AGENT_PERMISSION_PRESET_VETTED"
	case AgentPermissionPresetNone:
		return "AGENT_PERMISSION_PRESET_NONE"
	case AgentPermissionPresetAuto:
		return "AGENT_PERMISSION_PRESET_AUTO"
	}
	return "AGENT_PERMISSION_PRESET_UNSPECIFIED"
}

// AgentSettingPolicy — recovered enum.
type AgentSettingPolicy int32

const (
	AgentSettingPolicyUnspecified AgentSettingPolicy = 0
	AgentSettingPolicyAllow       AgentSettingPolicy = 1
	AgentSettingPolicyAsk         AgentSettingPolicy = 2
	AgentSettingPolicyDeny        AgentSettingPolicy = 3
)

// AnnotationsConfig — recovered enum.
type AnnotationsConfig int32

const (
	AnnotationsConfigUnspecified AnnotationsConfig = 0
	AnnotationsConfigDisabled    AnnotationsConfig = 1
	AnnotationsConfigInline      AnnotationsConfig = 2
	AnnotationsConfigMargin      AnnotationsConfig = 3
)

// ArtifactApprovalStatus — recovered enum.
type ArtifactApprovalStatus int32

const (
	ArtifactApprovalStatusUnspecified ArtifactApprovalStatus = 0
	ArtifactApprovalStatusPending     ArtifactApprovalStatus = 1
	ArtifactApprovalStatusApproved    ArtifactApprovalStatus = 2
	ArtifactApprovalStatusRejected    ArtifactApprovalStatus = 3
)

// ArtifactReviewMode — the reference's own enum.
//
// 【一手】 2026-10-27: the same 1.4.8 descriptor as
// AgentPermissionPreset. The 1.3.5 recovery guessed three values
// ("inline", "sidebar") that the descriptor does not have.
type ArtifactReviewMode int32

const (
	ArtifactReviewModeUnspecified ArtifactReviewMode = 0
	ArtifactReviewModeAlways      ArtifactReviewMode = 1
	ArtifactReviewModeTurbo       ArtifactReviewMode = 2
	ArtifactReviewModeAuto        ArtifactReviewMode = 3
)

// ArtifactReviewModeName returns the reference's name for a review
// mode — the spelling proto-JSON puts on the wire.
func ArtifactReviewModeName(m ArtifactReviewMode) string {
	switch m {
	case ArtifactReviewModeAlways:
		return "ARTIFACT_REVIEW_MODE_ALWAYS"
	case ArtifactReviewModeTurbo:
		return "ARTIFACT_REVIEW_MODE_TURBO"
	case ArtifactReviewModeAuto:
		return "ARTIFACT_REVIEW_MODE_AUTO"
	}
	return "ARTIFACT_REVIEW_MODE_UNSPECIFIED"
}

// ArtifactType — recovered enum.
type ArtifactType int32

const (
	ArtifactTypeUnspecified ArtifactType = 0
	ArtifactTypeFile        ArtifactType = 1
	ArtifactTypeCode        ArtifactType = 2
	ArtifactTypeImage       ArtifactType = 3
	ArtifactTypeDocument    ArtifactType = 4
)

// AttributionStatus — recovered enum.
type AttributionStatus int32

const (
	AttributionStatusUnspecified AttributionStatus = 0
	AttributionStatusUnknown     AttributionStatus = 1
	AttributionStatusNotFound    AttributionStatus = 2
	AttributionStatusFound       AttributionStatus = 3
)

// AutoContinueOnMaxGeneratorInvocations — recovered enum.
type AutoContinueOnMaxGeneratorInvocations int32

const (
	AutoContinueOnMaxGeneratorInvocationsUnspecified AutoContinueOnMaxGeneratorInvocations = 0
	AutoContinueOnMaxGeneratorInvocationsOff          AutoContinueOnMaxGeneratorInvocations = 1
	AutoContinueOnMaxGeneratorInvocationsOn           AutoContinueOnMaxGeneratorInvocations = 2
)

// AutocompleteSpeed — recovered enum.
type AutocompleteSpeed int32

const (
	AutocompleteSpeedUnspecified AutocompleteSpeed = 0
	AutocompleteSpeedSlow        AutocompleteSpeed = 1
	AutocompleteSpeedNormal      AutocompleteSpeed = 2
	AutocompleteSpeedFast        AutocompleteSpeed = 3
	AutocompleteSpeedTurbo       AutocompleteSpeed = 4
)

// BetterDirection — recovered enum.
type BetterDirection int32

const (
	BetterDirectionUnspecified BetterDirection = 0
	BetterDirectionLeft        BetterDirection = 1
	BetterDirectionRight       BetterDirection = 2
	BetterDirectionUp          BetterDirection = 3
	BetterDirectionDown        BetterDirection = 4
)

// BrowserInstallationStatus — recovered enum.
type BrowserInstallationStatus int32

const (
	BrowserInstallationStatusUnspecified BrowserInstallationStatus = 0
	BrowserInstallationStatusNotInstalled BrowserInstallationStatus = 1
	BrowserInstallationStatusInstalled   BrowserInstallationStatus = 2
	BrowserInstallationStatusOutdated    BrowserInstallationStatus = 3
)

// BrowserJsAutoRunPolicy — recovered enum.
type BrowserJsAutoRunPolicy int32

const (
	BrowserJsAutoRunPolicyUnspecified BrowserJsAutoRunPolicy = 0
	BrowserJsAutoRunPolicyDeny        BrowserJsAutoRunPolicy = 1
	BrowserJsAutoRunPolicyAsk         BrowserJsAutoRunPolicy = 2
	BrowserJsAutoRunPolicyAllow       BrowserJsAutoRunPolicy = 3
)

// BrowserJsExecutionPolicy — recovered enum.
type BrowserJsExecutionPolicy int32

const (
	BrowserJsExecutionPolicyUnspecified BrowserJsExecutionPolicy = 0
	BrowserJsExecutionPolicyDeny        BrowserJsExecutionPolicy = 1
	BrowserJsExecutionPolicyAsk         BrowserJsExecutionPolicy = 2
	BrowserJsExecutionPolicyAllow       BrowserJsExecutionPolicy = 3
)

// CascadeCommandsAutoExecution — recovered enum.
type CascadeCommandsAutoExecution int32

const (
	CascadeCommandsAutoExecutionUnspecified CascadeCommandsAutoExecution = 0
	CascadeCommandsAutoExecutionOff        CascadeCommandsAutoExecution = 1
	CascadeCommandsAutoExecutionOn         CascadeCommandsAutoExecution = 2
	CascadeCommandsAutoExecutionAsk        CascadeCommandsAutoExecution = 3
)

// CascadeInputAutocomplete — recovered enum.
type CascadeInputAutocomplete int32

const (
	CascadeInputAutocompleteUnspecified CascadeInputAutocomplete = 0
	CascadeInputAutocompleteOff         CascadeInputAutocomplete = 1
	CascadeInputAutocompleteOn          CascadeInputAutocomplete = 2
)

// CascadeNUXEvent — recovered enum.
type CascadeNUXEvent int32

const (
	CascadeNUXEventUnspecified CascadeNUXEvent = 0
	CascadeNUXEventShown      CascadeNUXEvent = 1
	CascadeNUXEventDismissed  CascadeNUXEvent = 2
	CascadeNUXEventAccepted   CascadeNUXEvent = 3
	CascadeNUXEventExpired    CascadeNUXEvent = 4
)

// CascadeNUXIcon — recovered enum.
type CascadeNUXIcon int32

const (
	CascadeNUXIconUnspecified CascadeNUXIcon = 0
	CascadeNUXIconInfo        CascadeNUXIcon = 1
	CascadeNUXIconWarning     CascadeNUXIcon = 2
	CascadeNUXIconSuccess     CascadeNUXIcon = 3
	CascadeNUXIconError       CascadeNUXIcon = 4
)

// CascadeNUXInteractionType — recovered enum.
type CascadeNUXInteractionType int32

const (
	CascadeNUXInteractionTypeUnspecified CascadeNUXInteractionType = 0
	CascadeNUXInteractionTypeClick      CascadeNUXInteractionType = 1
	CascadeNUXInteractionTypeDismiss    CascadeNUXInteractionType = 2
	CascadeNUXInteractionTypeLink       CascadeNUXInteractionType = 3
)

// CascadeNUXLocation — recovered enum.
type CascadeNUXLocation int32

const (
	CascadeNUXLocationUnspecified CascadeNUXLocation = 0
	CascadeNUXLocationChat        CascadeNUXLocation = 1
	CascadeNUXLocationEditor      CascadeNUXLocation = 2
	CascadeNUXLocationSidebar     CascadeNUXLocation = 3
	CascadeNUXLocationPanel       CascadeNUXLocation = 4
)

// CascadeNUXSession — recovered enum.
type CascadeNUXSession int32

const (
	CascadeNUXSessionUnspecified CascadeNUXSession = 0
	CascadeNUXSessionAnonymous   CascadeNUXSession = 1
	CascadeNUXSessionLoggedIn    CascadeNUXSession = 2
	CascadeNUXSessionReturning   CascadeNUXSession = 3
)

// CascadeNUXTrigger — recovered enum.
type CascadeNUXTrigger int32

const (
	CascadeNUXTriggerUnspecified CascadeNUXTrigger = 0
	CascadeNUXTriggerFirstRun    CascadeNUXTrigger = 1
	CascadeNUXTriggerUpdate      CascadeNUXTrigger = 2
	CascadeNUXTriggerManual      CascadeNUXTrigger = 3
)

// CascadeRunExtensionCode — recovered enum.
type CascadeRunExtensionCode int32

const (
	CascadeRunExtensionCodeUnspecified CascadeRunExtensionCode = 0
	CascadeRunExtensionCodeOff         CascadeRunExtensionCode = 1
	CascadeRunExtensionCodeOn          CascadeRunExtensionCode = 2
)

// CascadeRunExtensionCodeAutoRun — recovered enum.
type CascadeRunExtensionCodeAutoRun int32

const (
	CascadeRunExtensionCodeAutoRunUnspecified CascadeRunExtensionCodeAutoRun = 0
	CascadeRunExtensionCodeAutoRunOff         CascadeRunExtensionCodeAutoRun = 1
	CascadeRunExtensionCodeAutoRunOn          CascadeRunExtensionCodeAutoRun = 2
)

// CascadeWebSearchTool — recovered enum.
type CascadeWebSearchTool int32

const (
	CascadeWebSearchToolUnspecified CascadeWebSearchTool = 0
	CascadeWebSearchToolCodeium     CascadeWebSearchTool = 1
	CascadeWebSearchToolBrave       CascadeWebSearchTool = 2
	CascadeWebSearchToolGoogle      CascadeWebSearchTool = 3
	CascadeWebSearchToolExa         CascadeWebSearchTool = 4
)

// ChatMessageSource — recovered enum.
type ChatMessageSource int32

const (
	ChatMessageSourceUnspecified ChatMessageSource = 0
	ChatMessageSourceUser        ChatMessageSource = 1
	ChatMessageSourceModel       ChatMessageSource = 2
	ChatMessageSourceSystem      ChatMessageSource = 3
)

// CodeContextSource — recovered enum.
type CodeContextSource int32

const (
	CodeContextSourceUnspecified CodeContextSource = 0
	CodeContextSourceCurrentFile CodeContextSource = 1
	CodeContextSourceWorkspace   CodeContextSource = 2
	CodeContextSourceIndex       CodeContextSource = 3
	CodeContextSourceUser        CodeContextSource = 4
)

// CodeContextType — recovered enum.
type CodeContextType int32

const (
	CodeContextTypeUnspecified CodeContextType = 0
	CodeContextTypeSnippet     CodeContextType = 1
	CodeContextTypeFile        CodeContextType = 2
	CodeContextTypeFunction    CodeContextType = 3
	CodeContextTypeClass       CodeContextType = 4
)

// CodeSource — recovered enum.
type CodeSource int32

const (
	CodeSourceUnspecified CodeSource = 0
	CodeSourceUser        CodeSource = 1
	CodeSourceModel       CodeSource = 2
	CodeSourceTool        CodeSource = 3
)

// CommandRequestSource — recovered enum.
type CommandRequestSource int32

const (
	CommandRequestSourceUnspecified CommandRequestSource = 0
	CommandRequestSourceChat        CommandRequestSource = 1
	CommandRequestSourceTerminal    CommandRequestSource = 2
	CommandRequestSourceSidebar     CommandRequestSource = 3
)

// CommitIntentType — recovered enum.
type CommitIntentType int32

const (
	CommitIntentTypeUnspecified CommitIntentType = 0
	CommitIntentTypeFeat        CommitIntentType = 1
	CommitIntentTypeFix         CommitIntentType = 2
	CommitIntentTypeDocs        CommitIntentType = 3
	CommitIntentTypeStyle       CommitIntentType = 4
	CommitIntentTypeRefactor    CommitIntentType = 5
	CommitIntentTypeTest        CommitIntentType = 6
	CommitIntentTypeChore       CommitIntentType = 7
)

// CompletionSource — recovered enum.
type CompletionSource int32

const (
	CompletionSourceUnspecified CompletionSource = 0
	CompletionSourceLocal       CompletionSource = 1
	CompletionSourceCloud       CompletionSource = 2
	CompletionSourceCache       CompletionSource = 3
)

// CompletionType — recovered enum.
type CompletionType int32

const (
	CompletionTypeUnspecified CompletionType = 0
	CompletionTypeLine        CompletionType = 1
	CompletionTypeBlock       CompletionType = 2
	CompletionTypeSnippet     CompletionType = 3
)

// ContextInclusionType — recovered enum.
type ContextInclusionType int32

const (
	ContextInclusionTypeUnspecified ContextInclusionType = 0
	ContextInclusionTypeExplicit   ContextInclusionType = 1
	ContextInclusionTypeImplicit   ContextInclusionType = 2
	ContextInclusionTypePinned     ContextInclusionType = 3
)

// ContextScopeType — recovered enum.
type ContextScopeType int32

const (
	ContextScopeTypeUnspecified ContextScopeType = 0
	ContextScopeTypeWorkspace   ContextScopeType = 1
	ContextScopeTypeFile        ContextScopeType = 2
	ContextScopeTypeFunction    ContextScopeType = 3
)

// ContextSnippetType — recovered enum.
type ContextSnippetType int32

const (
	ContextSnippetTypeUnspecified ContextSnippetType = 0
	ContextSnippetTypeCode        ContextSnippetType = 1
	ContextSnippetTypeComment     ContextSnippetType = 2
	ContextSnippetTypeString      ContextSnippetType = 3
)

// ConversationalPlannerMode — recovered enum.
type ConversationalPlannerMode int32

const (
	ConversationalPlannerModeUnspecified ConversationalPlannerMode = 0
	ConversationalPlannerModeOff         ConversationalPlannerMode = 1
	ConversationalPlannerModeOn          ConversationalPlannerMode = 2
)

// CortexErrorCategory — recovered enum.
type CortexErrorCategory int32

const (
	CortexErrorCategoryUnspecified CortexErrorCategory = 0
	CortexErrorCategoryNetwork     CortexErrorCategory = 1
	CortexErrorCategoryAuth        CortexErrorCategory = 2
	CortexErrorCategoryTimeout     CortexErrorCategory = 3
	CortexErrorCategoryInternal    CortexErrorCategory = 4
	CortexErrorCategoryValidation  CortexErrorCategory = 5
)

// DeploymentBuildStatus — recovered enum.
type DeploymentBuildStatus int32

const (
	DeploymentBuildStatusUnspecified DeploymentBuildStatus = 0
	DeploymentBuildStatusPending     DeploymentBuildStatus = 1
	DeploymentBuildStatusBuilding    DeploymentBuildStatus = 2
	DeploymentBuildStatusDeployed    DeploymentBuildStatus = 3
	DeploymentBuildStatusFailed      DeploymentBuildStatus = 4
)

// DeploymentProvider — recovered enum.
type DeploymentProvider int32

const (
	DeploymentProviderUnspecified DeploymentProvider = 0
	DeploymentProviderVercel      DeploymentProvider = 1
	DeploymentProviderNetlify     DeploymentProvider = 2
	DeploymentProviderRender      DeploymentProvider = 3
	DeploymentProviderCloudflare  DeploymentProvider = 4
	DeploymentProviderCustom      DeploymentProvider = 5
)

// DetectAndUseProxy — recovered enum.
type DetectAndUseProxy int32

const (
	DetectAndUseProxyUnspecified DetectAndUseProxy = 0
	DetectAndUseProxyOff         DetectAndUseProxy = 1
	DetectAndUseProxyOn          DetectAndUseProxy = 2
)

// DocumentType — recovered enum.
type DocumentType int32

const (
	DocumentTypeUnspecified DocumentType = 0
	DocumentTypePlain       DocumentType = 1
	DocumentTypeMarkdown    DocumentType = 2
	DocumentTypeHtml        DocumentType = 3
	DocumentTypeCode        DocumentType = 4
)

// EmbedType — recovered enum.
type EmbedType int32

const (
	EmbedTypeUnspecified EmbedType = 0
	EmbedTypeDense       EmbedType = 1
	EmbedTypeSparse      EmbedType = 2
	EmbedTypeColbert     EmbedType = 3
)

// EmbeddingPrefix — recovered enum.
type EmbeddingPrefix int32

const (
	EmbeddingPrefixUnspecified EmbeddingPrefix = 0
	EmbeddingPrefixDocument   EmbeddingPrefix = 1
	EmbeddingPrefixQuery      EmbeddingPrefix = 2
)

// EmbeddingPriority — recovered enum.
type EmbeddingPriority int32

const (
	EmbeddingPriorityUnspecified EmbeddingPriority = 0
	EmbeddingPriorityLow         EmbeddingPriority = 1
	EmbeddingPriorityNormal      EmbeddingPriority = 2
	EmbeddingPriorityHigh        EmbeddingPriority = 3
)

// EmbeddingSource — recovered enum.
type EmbeddingSource int32

const (
	EmbeddingSourceUnspecified EmbeddingSource = 0
	EmbeddingSourceLocal       EmbeddingSource = 1
	EmbeddingSourceCloud       EmbeddingSource = 2
)

// EventType — recovered enum.
type EventType int32

const (
	EventTypeUnspecified EventType = 0
	EventTypeAction      EventType = 1
	EventTypeState       EventType = 2
	EventTypeMetric      EventType = 3
	EventTypeError       EventType = 4
)

// ExperimentKey — recovered enum.
type ExperimentKey int32

const (
	ExperimentKeyUnspecified ExperimentKey = 0
	ExperimentKeyABTest      ExperimentKey = 1
	ExperimentKeyFeatureFlag ExperimentKey = 2
	ExperimentKeyConfig      ExperimentKey = 3
)

// ExperimentSource — recovered enum.
type ExperimentSource int32

const (
	ExperimentSourceUnspecified ExperimentSource = 0
	ExperimentSourceRemote      ExperimentSource = 1
	ExperimentSourceLocal       ExperimentSource = 2
)

// ExtensionPanelTab — recovered enum.
type ExtensionPanelTab int32

const (
	ExtensionPanelTabUnspecified ExtensionPanelTab = 0
	ExtensionPanelTabChat        ExtensionPanelTab = 1
	ExtensionPanelTabHistory     ExtensionPanelTab = 2
	ExtensionPanelTabSettings    ExtensionPanelTab = 3
)

// FeatureUsageType — recovered enum.
type FeatureUsageType int32

const (
	FeatureUsageTypeUnspecified FeatureUsageType = 0
	FeatureUsageTypeCount       FeatureUsageType = 1
	FeatureUsageTypeLatency     FeatureUsageType = 2
	FeatureUsageTypeBoolean     FeatureUsageType = 3
)

// FilterReason — recovered enum.
type FilterReason int32

const (
	FilterReasonUnspecified FilterReason = 0
	FilterReasonLength      FilterReason = 1
	FilterReasonLanguage    FilterReason = 2
	FilterReasonUserBlocked FilterReason = 3
	FilterReasonPii         FilterReason = 4
)

// GpuType — recovered enum.
type GpuType int32

const (
	GpuTypeUnspecified GpuType = 0
	GpuTypeNvidia      GpuType = 1
	GpuTypeAmd         GpuType = 2
	GpuTypeAppleSilicon GpuType = 3
	GpuTypeTpu         GpuType = 4
)

// IndexChoice — recovered enum.
type IndexChoice int32

const (
	IndexChoiceUnspecified IndexChoice = 0
	IndexChoiceLocal       IndexChoice = 1
	IndexChoiceCloud       IndexChoice = 2
	IndexChoiceBoth        IndexChoice = 3
)

// Language — recovered enum (subset of common language codes).
type Language int32

const (
	LanguageUnspecified Language = 0
	LanguageGo          Language = 1
	LanguagePython      Language = 2
	LanguageJava        Language = 3
	LanguageJavaScript  Language = 4
	LanguageTypeScript  Language = 5
	LanguageRust        Language = 6
	LanguageCpp         Language = 7
	LanguageC           Language = 8
	LanguageRuby        Language = 9
	LanguagePHP         Language = 10
	LanguageShell       Language = 11
	LanguageSQL         Language = 12
)

// LastUpdateType — recovered enum.
type LastUpdateType int32

const (
	LastUpdateTypeUnspecified LastUpdateType = 0
	LastUpdateTypeRefresh     LastUpdateType = 1
	LastUpdateTypeInvalidate  LastUpdateType = 2
	LastUpdateTypeAppend      LastUpdateType = 3
)

// MarkdownNodeType — recovered enum.
type MarkdownNodeType int32

const (
	MarkdownNodeTypeUnspecified MarkdownNodeType = 0
	MarkdownNodeTypeDocument    MarkdownNodeType = 1
	MarkdownNodeTypeHeading     MarkdownNodeType = 2
	MarkdownNodeTypeParagraph   MarkdownNodeType = 3
	MarkdownNodeTypeList        MarkdownNodeType = 4
	MarkdownNodeTypeCodeBlock   MarkdownNodeType = 5
)

// MediaResolution — recovered enum.
type MediaResolution int32

const (
	MediaResolutionUnspecified MediaResolution = 0
	MediaResolutionLow         MediaResolution = 1
	MediaResolutionMedium      MediaResolution = 2
	MediaResolutionHigh        MediaResolution = 3
)

// MetricsScope — recovered enum.
type MetricsScope int32

const (
	MetricsScopeUnspecified MetricsScope = 0
	MetricsScopePerRequest  MetricsScope = 1
	MetricsScopePerSession  MetricsScope = 2
	MetricsScopePerUser     MetricsScope = 3
	MetricsScopeGlobal      MetricsScope = 4
)

// Modality — recovered enum.
type Modality int32

const (
	ModalityUnspecified Modality = 0
	ModalityText        Modality = 1
	ModalityImage       Modality = 2
	ModalityAudio       Modality = 3
	ModalityVideo       Modality = 4
)

// Model — recovered enum (alias for the model identifier enum).
type Model int32

const (
	ModelUnspecified Model = 0
	ModelGPT4        Model = 1
	ModelGPT4Turbo   Model = 2
	ModelGPT35Turbo  Model = 3
	ModelClaudeOpus  Model = 4
	ModelClaudeSonnet Model = 5
	ModelClaudeHaiku Model = 6
	ModelGemini15Pro Model = 7
	ModelGemini15Flash Model = 8
	ModelLlama3_70B   Model = 9
	ModelLlama3_8B    Model = 10
)

// ModelAlias — recovered enum (model alias identifier).
type ModelAlias int32

const (
	ModelAliasUnspecified ModelAlias = 0
	ModelAliasDefault     ModelAlias = 1
	ModelAliasFast        ModelAlias = 2
	ModelAliasFree        ModelAlias = 3
	ModelAliasPro         ModelAlias = 4
)

// ModelPricingType — recovered enum.
type ModelPricingType int32

const (
	ModelPricingTypeUnspecified ModelPricingType = 0
	ModelPricingTypeFree        ModelPricingType = 1
	ModelPricingTypePayPerUse  ModelPricingType = 2
	ModelPricingTypeIncluded   ModelPricingType = 3
)

// ModelProvider — recovered enum.
type ModelProvider int32

const (
	ModelProviderUnspecified ModelProvider = 0
	ModelProviderOpenAI      ModelProvider = 1
	ModelProviderAnthropic   ModelProvider = 2
	ModelProviderGoogle      ModelProvider = 3
	ModelProviderMistral     ModelProvider = 4
	ModelProviderMeta        ModelProvider = 5
	ModelProviderCodeium     ModelProvider = 6
)

// ModelStatus — recovered enum.
type ModelStatus int32

const (
	ModelStatusUnspecified  ModelStatus = 0
	ModelStatusAvailable    ModelStatus = 1
	ModelStatusDegraded     ModelStatus = 2
	ModelStatusDeprecated   ModelStatus = 3
	ModelStatusUnavailable  ModelStatus = 4
)

// ModelType — recovered enum.
type ModelType int32

const (
	ModelTypeUnspecified ModelType = 0
	ModelTypeChat        ModelType = 1
	ModelTypeCompletion  ModelType = 2
	ModelTypeEmbedding   ModelType = 3
	ModelTypeEdit        ModelType = 4
	ModelTypeMultimodal  ModelType = 5
)

// OnboardingActionType — recovered enum.
type OnboardingActionType int32

const (
	OnboardingActionTypeUnspecified OnboardingActionType = 0
	OnboardingActionTypeConnect     OnboardingActionType = 1
	OnboardingActionTypeInstall     OnboardingActionType = 2
	OnboardingActionTypeConfigure   OnboardingActionType = 3
	OnboardingActionTypeInvite      OnboardingActionType = 4
)

// Permission — recovered enum.
type Permission int32

const (
	PermissionUnspecified Permission = 0
	PermissionAllow       Permission = 1
	PermissionDeny        Permission = 2
	PermissionAsk         Permission = 3
)

// PlanMode — recovered enum.
type PlanMode int32

const (
	PlanModeUnspecified PlanMode = 0
	PlanModeFree        PlanMode = 1
	PlanModePro         PlanMode = 2
	PlanModeEnterprise  PlanMode = 3
	PlanModeTeam        PlanMode = 4
)

// PlanningMode — recovered enum.
type PlanningMode int32

const (
	PlanningModeUnspecified PlanningMode = 0
	PlanningModeOff         PlanningMode = 1
	PlanningModeOn          PlanningMode = 2
	PlanningModeAuto        PlanningMode = 3
)

// ProductEventType — recovered enum.
type ProductEventType int32

const (
	ProductEventTypeUnspecified ProductEventType = 0
	ProductEventTypeClick       ProductEventType = 1
	ProductEventTypeView        ProductEventType = 2
	ProductEventTypeSubmit      ProductEventType = 3
	ProductEventTypeError       ProductEventType = 4
)

// PromptAnnotationKind — recovered enum.
type PromptAnnotationKind int32

const (
	PromptAnnotationKindUnspecified PromptAnnotationKind = 0
	PromptAnnotationKindCode        PromptAnnotationKind = 1
	PromptAnnotationKindComment     PromptAnnotationKind = 2
	PromptAnnotationKindLink        PromptAnnotationKind = 3
)

// PromptElementExclusionReason — recovered enum.
type PromptElementExclusionReason int32

const (
	PromptElementExclusionReasonUnspecified PromptElementExclusionReason = 0
	PromptElementExclusionReasonTooLong    PromptElementExclusionReason = 1
	PromptElementExclusionReasonNotRelevant PromptElementExclusionReason = 2
	PromptElementExclusionReasonDuplicate  PromptElementExclusionReason = 3
)

// PromptElementKind — recovered enum.
type PromptElementKind int32

const (
	PromptElementKindUnspecified PromptElementKind = 0
	PromptElementKindText        PromptElementKind = 1
	PromptElementKindCode        PromptElementKind = 2
	PromptElementKindImage       PromptElementKind = 3
)

// PromptTemplaterType — recovered enum.
type PromptTemplaterType int32

const (
	PromptTemplaterTypeUnspecified PromptTemplaterType = 0
	PromptTemplaterTypeFString     PromptTemplaterType = 1
	PromptTemplaterTypeJinja       PromptTemplaterType = 2
	PromptTemplaterTypeHandlebars  PromptTemplaterType = 3
)

// ProviderSource — recovered enum.
type ProviderSource int32

const (
	ProviderSourceUnspecified ProviderSource = 0
	ProviderSourceConfig      ProviderSource = 1
	ProviderSourceEnv         ProviderSource = 2
	ProviderSourceDefault     ProviderSource = 3
)

// RefreshCustomizationType — recovered enum.
type RefreshCustomizationType int32

const (
	RefreshCustomizationTypeUnspecified RefreshCustomizationType = 0
	RefreshCustomizationTypeManual      RefreshCustomizationType = 1
	RefreshCustomizationTypeAutomatic   RefreshCustomizationType = 2
)

// RememberLastModelSelection — recovered enum.
type RememberLastModelSelection int32

const (
	RememberLastModelSelectionUnspecified RememberLastModelSelection = 0
	RememberLastModelSelectionOff         RememberLastModelSelection = 1
	RememberLastModelSelectionOn          RememberLastModelSelection = 2
)

// ScmProvider — recovered enum.
type ScmProvider int32

const (
	ScmProviderUnspecified ScmProvider = 0
	ScmProviderGitHub      ScmProvider = 1
	ScmProviderGitLab      ScmProvider = 2
	ScmProviderBitbucket   ScmProvider = 3
	ScmProviderPerforce    ScmProvider = 4
	ScmProviderSVN         ScmProvider = 5
)

// ScmType — recovered enum.
type ScmType int32

const (
	ScmTypeUnspecified ScmType = 0
	ScmTypeGit         ScmType = 1
	ScmTypePerforce    ScmType = 2
	ScmTypeSVN         ScmType = 3
)

// SearchResultType — recovered enum.
type SearchResultType int32

const (
	SearchResultTypeUnspecified SearchResultType = 0
	SearchResultTypeFile        SearchResultType = 1
	SearchResultTypeSymbol      SearchResultType = 2
	SearchResultTypeText        SearchResultType = 3
	SearchResultTypeCode        SearchResultType = 4
)

// SlashCommandType — recovered enum.
type SlashCommandType int32

const (
	SlashCommandTypeUnspecified SlashCommandType = 0
	SlashCommandTypeBuiltin    SlashCommandType = 1
	SlashCommandTypeCustom     SlashCommandType = 2
	SlashCommandTypePlugin     SlashCommandType = 3
)

// StatusLevel — recovered enum.
type StatusLevel int32

const (
	StatusLevelUnspecified StatusLevel = 0
	StatusLevelInfo        StatusLevel = 1
	StatusLevelWarning     StatusLevel = 2
	StatusLevelError       StatusLevel = 3
	StatusLevelCritical    StatusLevel = 4
)

// StopReason — recovered enum.
type StopReason int32

const (
	StopReasonUnspecified StopReason = 0
	StopReasonMaxTokens   StopReason = 1
	StopReasonEndTurn     StopReason = 2
	StopReasonToolUse     StopReason = 3
	StopReasonContentFilter StopReason = 4
	StopReasonUserCancel  StopReason = 5
	StopReasonError       StopReason = 6
)

// SupercompleteTriggerCondition — recovered enum.
type SupercompleteTriggerCondition int32

const (
	SupercompleteTriggerConditionUnspecified SupercompleteTriggerCondition = 0
	SupercompleteTriggerConditionAlways     SupercompleteTriggerCondition = 1
	SupercompleteTriggerConditionOnDemand   SupercompleteTriggerCondition = 2
)

// TabEnabled — recovered enum.
type TabEnabled int32

const (
	TabEnabledUnspecified TabEnabled = 0
	TabEnabledOff         TabEnabled = 1
	TabEnabledOn          TabEnabled = 2
)

// TabToJump — recovered enum.
type TabToJump int32

const (
	TabToJumpUnspecified TabToJump = 0
	TabToJumpNext        TabToJump = 1
	TabToJumpPrevious    TabToJump = 2
	TabToJumpFirst       TabToJump = 3
	TabToJumpLast        TabToJump = 4
)

// TeamsFeatures — recovered enum.
type TeamsFeatures int32

const (
	TeamsFeaturesUnspecified         TeamsFeatures = 0
	TeamsFeaturesAuditLog            TeamsFeatures = 1
	TeamsFeaturesSsoEnforcement      TeamsFeatures = 2
	TeamsFeaturesCustomBranding      TeamsFeatures = 3
	TeamsFeaturesPrioritySupport     TeamsFeatures = 4
	TeamsFeaturesUsageAnalytics      TeamsFeatures = 5
	TeamsFeaturesAdminDashboard      TeamsFeatures = 6
)

// TeamsTier — recovered enum.
type TeamsTier int32

const (
	TeamsTierUnspecified TeamsTier = 0
	TeamsTierFree        TeamsTier = 1
	TeamsTierPro         TeamsTier = 2
	TeamsTierBusiness    TeamsTier = 3
	TeamsTierEnterprise  TeamsTier = 4
	TeamsTierIndividual  TeamsTier = 5
)

// TerminalShellCommandSource — recovered enum.
type TerminalShellCommandSource int32

const (
	TerminalShellCommandSourceUnspecified TerminalShellCommandSource = 0
	TerminalShellCommandSourceUser        TerminalShellCommandSource = 1
	TerminalShellCommandSourceAI          TerminalShellCommandSource = 2
)

// TerminalShellCommandStatus — recovered enum.
type TerminalShellCommandStatus int32

const (
	TerminalShellCommandStatusUnspecified TerminalShellCommandStatus = 0
	TerminalShellCommandStatusRunning    TerminalShellCommandStatus = 1
	TerminalShellCommandStatusCompleted  TerminalShellCommandStatus = 2
	TerminalShellCommandStatusFailed     TerminalShellCommandStatus = 3
	TerminalShellCommandStatusKilled     TerminalShellCommandStatus = 4
)

// ThemePreference — recovered enum.
type ThemePreference int32

const (
	ThemePreferenceUnspecified ThemePreference = 0
	ThemePreferenceLight       ThemePreference = 1
	ThemePreferenceDark        ThemePreference = 2
	ThemePreferenceSystem      ThemePreference = 3
)

// ThirdPartyWebSearchModel — recovered enum.
type ThirdPartyWebSearchModel int32

const (
	ThirdPartyWebSearchModelUnspecified ThirdPartyWebSearchModel = 0
	ThirdPartyWebSearchModelDefault     ThirdPartyWebSearchModel = 1
	ThirdPartyWebSearchModelPro         ThirdPartyWebSearchModel = 2
)

// ThirdPartyWebSearchProvider — recovered enum.
type ThirdPartyWebSearchProvider int32

const (
	ThirdPartyWebSearchProviderUnspecified ThirdPartyWebSearchProvider = 0
	ThirdPartyWebSearchProviderBrave      ThirdPartyWebSearchProvider = 1
	ThirdPartyWebSearchProviderGoogle     ThirdPartyWebSearchProvider = 2
	ThirdPartyWebSearchProviderExa        ThirdPartyWebSearchProvider = 3
)

// ToolFormatterType — recovered enum.
type ToolFormatterType int32

const (
	ToolFormatterTypeUnspecified ToolFormatterType = 0
	ToolFormatterTypeJSON        ToolFormatterType = 1
	ToolFormatterTypeXML         ToolFormatterType = 2
	ToolFormatterTypeMarkdown    ToolFormatterType = 3
)

// TrajectoryType — recovered enum.
type TrajectoryType int32

const (
	TrajectoryTypeUnspecified TrajectoryType = 0
	TrajectoryTypeChat        TrajectoryType = 1
	TrajectoryTypeEdit        TrajectoryType = 2
	TrajectoryTypeCommand     TrajectoryType = 3
)

// TransactionStatus — recovered enum.
type TransactionStatus int32

const (
	TransactionStatusUnspecified TransactionStatus = 0
	TransactionStatusPending    TransactionStatus = 1
	TransactionStatusCompleted  TransactionStatus = 2
	TransactionStatusFailed     TransactionStatus = 3
	TransactionStatusRefunded   TransactionStatus = 4
)

// UserFeatures — recovered enum.
type UserFeatures int32

const (
	UserFeaturesUnspecified      UserFeatures = 0
	UserFeaturesAutocomplete     UserFeatures = 1
	UserFeaturesChat            UserFeatures = 2
	UserFeaturesCascade         UserFeatures = 3
	UserFeaturesIndexing        UserFeatures = 4
	UserFeaturesBrowserTools    UserFeatures = 5
	UserFeaturesMcp             UserFeatures = 6
	UserFeaturesCommandRun      UserFeatures = 7
	UserFeaturesWebSearch       UserFeatures = 8
	UserFeaturesCustomModels    UserFeatures = 9
	UserFeaturesTelemetry       UserFeatures = 10
	UserFeaturesCrashReporting  UserFeatures = 11
	UserFeaturesErrorReporting  UserFeatures = 12
)

// UserNUXEvent — recovered enum.
type UserNUXEvent int32

const (
	UserNUXEventUnspecified UserNUXEvent = 0
	UserNUXEventShown       UserNUXEvent = 1
	UserNUXEventDismissed   UserNUXEvent = 2
	UserNUXEventAccepted    UserNUXEvent = 3
)

// UserTeamStatus — recovered enum.
type UserTeamStatus int32

const (
	UserTeamStatusUnspecified UserTeamStatus = 0
	UserTeamStatusSolo        UserTeamStatus = 1
	UserTeamStatusTeamMember  UserTeamStatus = 2
	UserTeamStatusTeamAdmin   UserTeamStatus = 3
	UserTeamStatusTeamOwner   UserTeamStatus = 4
)

// ValidationStatus — recovered enum.
type ValidationStatus int32

const (
	ValidationStatusUnspecified ValidationStatus = 0
	ValidationStatusPending     ValidationStatus = 1
	ValidationStatusValid       ValidationStatus = 2
	ValidationStatusInvalid     ValidationStatus = 3
	ValidationStatusError       ValidationStatus = 4
)

// VideoContentLayer — recovered enum.
type VideoContentLayer int32

const (
	VideoContentLayerUnspecified VideoContentLayer = 0
	VideoContentLayerAudio       VideoContentLayer = 1
	VideoContentLayerVideo       VideoContentLayer = 2
	VideoContentLayerSubtitles   VideoContentLayer = 3
)

// WorkingDirectoryStatus — recovered enum.
type WorkingDirectoryStatus int32

const (
	WorkingDirectoryStatusUnspecified WorkingDirectoryStatus = 0
	WorkingDirectoryStatusValid       WorkingDirectoryStatus = 1
	WorkingDirectoryStatusInvalid     WorkingDirectoryStatus = 2
	WorkingDirectoryStatusReadOnly    WorkingDirectoryStatus = 3
)

// ---------------------------------------------------------------------------
// Additional message types recovered 1:1 from v1.3.5 binary.
// ---------------------------------------------------------------------------

// DefaultPinnedContextConfig is the default config for pinned context.
type DefaultPinnedContextConfig struct {
	MaxPinnedItems     int32    `protobuf:"varint,1,opt,name=max_pinned_items,json=maxPinnedItems,proto3" json:"max_pinned_items,omitempty"`
	IncludeByDefault   []string `protobuf:"bytes,2,rep,name=include_by_default,json=includeByDefault,proto3" json:"include_by_default,omitempty"`
	ExcludeByDefault   []string `protobuf:"bytes,3,rep,name=exclude_by_default,json=excludeByDefault,proto3" json:"exclude_by_default,omitempty"`
}

// Metadata is a generic (key, value) metadata block.
type Metadata struct {
	Entries map[string]string `protobuf:"bytes,1,rep,name=entries,proto3" json:"entries,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
}

// Completion is a code completion entry.
type Completion struct {
	Text             string  `protobuf:"bytes,1,opt,name=text,proto3" json:"text,omitempty"`
	Range            *Range  `protobuf:"bytes,2,opt,name=range,proto3" json:"range,omitempty"`
	Score            float32 `protobuf:"fixed32,3,opt,name=score,proto3" json:"score,omitempty"`
	Source           CompletionSource `protobuf:"varint,4,opt,name=source,proto3,enum=codeium_common_pb.CompletionSource" json:"source,omitempty"`
	Type             CompletionType   `protobuf:"varint,5,opt,name=type,proto3,enum=codeium_common_pb.CompletionType" json:"type,omitempty"`
	Language         Language         `protobuf:"varint,6,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	Model            string  `protobuf:"bytes,7,opt,name=model,proto3" json:"model,omitempty"`
	GenerationId     string  `protobuf:"bytes,8,opt,name=generation_id,json=generationId,proto3" json:"generation_id,omitempty"`
	CompletionId     string  `protobuf:"bytes,9,opt,name=completion_id,json=completionId,proto3" json:"completion_id,omitempty"`
	LineContent      string  `protobuf:"bytes,10,opt,name=line_content,json=lineContent,proto3" json:"line_content,omitempty"`
	LineNumber       int32   `protobuf:"varint,11,opt,name=line_number,json=lineNumber,proto3" json:"line_number,omitempty"`
	Column           int32   `protobuf:"varint,12,opt,name=column,proto3" json:"column,omitempty"`
	WasAccepted      bool    `protobuf:"varint,13,opt,name=was_accepted,json=wasAccepted,proto3" json:"was_accepted,omitempty"`
	WasPartiallyAccepted bool `protobuf:"varint,14,opt,name=was_partially_accepted,json=wasPartiallyAccepted,proto3" json:"was_partially_accepted,omitempty"`
	UserEdits        string  `protobuf:"bytes,15,opt,name=user_edits,json=userEdits,proto3" json:"user_edits,omitempty"`
}

// Range is a (start, end) line/column range.
type Range struct {
	StartLine int32 `protobuf:"varint,1,opt,name=start_line,json=startLine,proto3" json:"start_line,omitempty"`
	StartCol  int32 `protobuf:"varint,2,opt,name=start_col,json=startCol,proto3" json:"start_col,omitempty"`
	EndLine   int32 `protobuf:"varint,3,opt,name=end_line,json=endLine,proto3" json:"end_line,omitempty"`
	EndCol    int32 `protobuf:"varint,4,opt,name=end_col,json=endCol,proto3" json:"end_col,omitempty"`
}

// Document is a generic document reference.
type Document struct {
	Uri        string   `protobuf:"bytes,1,opt,name=uri,proto3" json:"uri,omitempty"`
	Language   Language `protobuf:"varint,2,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	Text       string   `protobuf:"bytes,3,opt,name=text,proto3" json:"text,omitempty"`
	Hash       string   `protobuf:"bytes,4,opt,name=hash,proto3" json:"hash,omitempty"`
	Source     string   `protobuf:"bytes,5,opt,name=source,proto3" json:"source,omitempty"`
	Kind       DocumentType `protobuf:"varint,6,opt,name=kind,proto3,enum=codeium_common_pb.DocumentType" json:"kind,omitempty"`
	Version    int32    `protobuf:"varint,7,opt,name=version,proto3" json:"version,omitempty"`
	Tags       []string `protobuf:"bytes,8,rep,name=tags,proto3" json:"tags,omitempty"`
	MimeType   string   `protobuf:"bytes,9,opt,name=mime_type,json=mimeType,proto3" json:"mime_type,omitempty"`
	LineCount  int32    `protobuf:"varint,10,opt,name=line_count,json=lineCount,proto3" json:"line_count,omitempty"`
	CharCount  int32    `protobuf:"varint,11,opt,name=char_count,json=charCount,proto3" json:"char_count,omitempty"`
	Metadata   *Metadata `protobuf:"bytes,12,opt,name=metadata,proto3" json:"metadata,omitempty"`
	Outline    *DocumentOutline `protobuf:"bytes,13,opt,name=outline,proto3" json:"outline,omitempty"`
	Position   *DocumentPosition `protobuf:"bytes,14,opt,name=position,proto3" json:"position,omitempty"`
	Repo       *Repository `protobuf:"bytes,15,opt,name=repo,proto3" json:"repo,omitempty"`
	EditorOptions *EditorOptions `protobuf:"bytes,16,opt,name=editor_options,json=editorOptions,proto3" json:"editor_options,omitempty"`
	WasEdited  bool     `protobuf:"varint,17,opt,name=was_edited,json=wasEdited,proto3" json:"was_edited,omitempty"`
	ViewedAt   time.Time `protobuf:"bytes,18,opt,name=viewed_at,json=viewedAt,proto3,stdtime" json:"viewed_at,omitempty"`
}

// DocumentPosition is the cursor position in a document.
type DocumentPosition struct {
	Line   int32 `protobuf:"varint,1,opt,name=line,proto3" json:"line,omitempty"`
	Column int32 `protobuf:"varint,2,opt,name=column,proto3" json:"column,omitempty"`
}

// DocumentOutline is the outline tree.
type DocumentOutline struct {
	Elements []*DocumentOutlineElement `protobuf:"bytes,1,rep,name=elements,proto3" json:"elements,omitempty"`
	Version  int32                     `protobuf:"varint,2,opt,name=version,proto3" json:"version,omitempty"`
}

// DocumentOutlineElement is a single outline node.
type DocumentOutlineElement struct {
	Name      string  `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Kind      string  `protobuf:"bytes,2,opt,name=kind,proto3" json:"kind,omitempty"`
	Range     *Range  `protobuf:"bytes,3,opt,name=range,proto3" json:"range,omitempty"`
	Selection *Range  `protobuf:"bytes,4,opt,name=selection,proto3" json:"selection,omitempty"`
}

// EditorOptions is the editor view options.
type EditorOptions struct {
	TabSize     int32  `protobuf:"varint,1,opt,name=tab_size,json=tabSize,proto3" json:"tab_size,omitempty"`
	IndentSize  int32  `protobuf:"varint,2,opt,name=indent_size,json=indentSize,proto3" json:"indent_size,omitempty"`
	UseTabs     bool   `protobuf:"varint,3,opt,name=use_tabs,json=useTabs,proto3" json:"use_tabs,omitempty"`
}

// Repository is a git/svn repository reference.
type Repository struct {
	Uri        string   `protobuf:"bytes,1,opt,name=uri,proto3" json:"uri,omitempty"`
	Branch     string   `protobuf:"bytes,2,opt,name=branch,proto3" json:"branch,omitempty"`
	Commit     string   `protobuf:"bytes,3,opt,name=commit,proto3" json:"commit,omitempty"`
	Remote     string   `protobuf:"bytes,4,opt,name=remote,proto3" json:"remote,omitempty"`
	ScmType    ScmType  `protobuf:"varint,5,opt,name=scm_type,json=scmType,proto3,enum=codeium_common_pb.ScmType" json:"scm_type,omitempty"`
	ScmProvider ScmProvider `protobuf:"varint,6,opt,name=scm_provider,json=scmProvider,proto3,enum=codeium_common_pb.ScmProvider" json:"scm_provider,omitempty"`
	WorkspaceInfo *ScmWorkspaceInfo `protobuf:"bytes,7,opt,name=workspace_info,json=workspaceInfo,proto3" json:"workspace_info,omitempty"`
}

// ScmWorkspaceInfo is the workspace info from a SCM provider.
type ScmWorkspaceInfo struct {
	RootPath   string   `protobuf:"bytes,1,opt,name=root_path,json=rootPath,proto3" json:"root_path,omitempty"`
	Workdir    string   `protobuf:"bytes,2,opt,name=workdir,proto3" json:"workdir,omitempty"`
	Branch     string   `protobuf:"bytes,3,opt,name=branch,proto3" json:"branch,omitempty"`
}

// LogRequest is the upstream log service request.
type LogRequest struct {
	ServiceName string             `protobuf:"bytes,1,opt,name=service_name,json=serviceName,proto3" json:"service_name,omitempty"`
	LogName     string             `protobuf:"bytes,2,opt,name=log_name,json=logName,proto3" json:"log_name,omitempty"`
	Severity    StatusLevel         `protobuf:"varint,3,opt,name=severity,proto3,enum=codeium_common_pb.StatusLevel" json:"severity,omitempty"`
	Payload     *LogEvent           `protobuf:"bytes,4,opt,name=payload,proto3" json:"payload,omitempty"`
}

// LogResponse is the upstream log service response.
type LogResponse struct {
	Accepted bool   `protobuf:"varint,1,opt,name=accepted,proto3" json:"accepted,omitempty"`
	Error    string `protobuf:"bytes,2,opt,name=error,proto3" json:"error,omitempty"`
}

// LogEvent is a single log event.
type LogEvent struct {
	Message   string   `protobuf:"bytes,1,opt,name=message,proto3" json:"message,omitempty"`
	Severity  StatusLevel `protobuf:"varint,2,opt,name=severity,proto3,enum=codeium_common_pb.StatusLevel" json:"severity,omitempty"`
	Timestamp time.Time `protobuf:"bytes,3,opt,name=timestamp,proto3,stdtime" json:"timestamp,omitempty"`
}

// Event is a generic event message.
type Event struct {
	Name       string    `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Properties *Metadata `protobuf:"bytes,2,opt,name=properties,proto3" json:"properties,omitempty"`
	Timestamp  time.Time `protobuf:"bytes,3,opt,name=timestamp,proto3,stdtime" json:"timestamp,omitempty"`
}

// ProductEvent is a product analytics event.
type ProductEvent struct {
	Name       string    `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Type       ProductEventType `protobuf:"varint,2,opt,name=type,proto3,enum=codeium_common_pb.ProductEventType" json:"type,omitempty"`
	UserId     string    `protobuf:"bytes,3,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	SessionId  string    `protobuf:"bytes,4,opt,name=session_id,json=sessionId,proto3" json:"session_id,omitempty"`
	Properties *Metadata `protobuf:"bytes,5,opt,name=properties,proto3" json:"properties,omitempty"`
	Timestamp  time.Time `protobuf:"bytes,6,opt,name=timestamp,proto3,stdtime" json:"timestamp,omitempty"`
	Source     string    `protobuf:"bytes,7,opt,name=source,proto3" json:"source,omitempty"`
	Page       string    `protobuf:"bytes,8,opt,name=page,proto3" json:"page,omitempty"`
	Component  string    `protobuf:"bytes,9,opt,name=component,proto3" json:"component,omitempty"`
}

// Embedding is an embedding vector.
type Embedding struct {
	Vector   []float32 `protobuf:"fixed32,1,rep,packed,name=vector,proto3" json:"vector,omitempty"`
	Model    string    `protobuf:"bytes,2,opt,name=model,proto3" json:"model,omitempty"`
	Source   EmbeddingSource `protobuf:"varint,3,opt,name=source,proto3,enum=codeium_common_pb.EmbeddingSource" json:"source,omitempty"`
	Prefix   EmbeddingPrefix  `protobuf:"varint,4,opt,name=prefix,proto3,enum=codeium_common_pb.EmbeddingPrefix" json:"prefix,omitempty"`
}

// EmbeddingResponse is the embedding RPC response.
type EmbeddingResponse struct {
	Embedding *Embedding `protobuf:"bytes,1,opt,name=embedding,proto3" json:"embedding,omitempty"`
	Metadata  *EmbeddingMetadata `protobuf:"bytes,2,opt,name=metadata,proto3" json:"metadata,omitempty"`
}

// EmbeddingMetadata is the metadata for an embedding response.
type EmbeddingMetadata struct {
	Model        string  `protobuf:"bytes,1,opt,name=model,proto3" json:"model,omitempty"`
	Dimensions   int32   `protobuf:"varint,2,opt,name=dimensions,proto3" json:"dimensions,omitempty"`
	TimeMs       int32   `protobuf:"varint,3,opt,name=time_ms,json=timeMs,proto3" json:"time_ms,omitempty"`
	CostUsd      float32 `protobuf:"fixed32,4,opt,name=cost_usd,json=costUsd,proto3" json:"cost_usd,omitempty"`
}

// EmbeddingsRequest is the embedding RPC request.
type EmbeddingsRequest struct {
	Input    []string `protobuf:"bytes,1,rep,name=input,proto3" json:"input,omitempty"`
	Model    string   `protobuf:"bytes,2,opt,name=model,proto3" json:"model,omitempty"`
	Prefix   EmbeddingPrefix `protobuf:"varint,3,opt,name=prefix,proto3,enum=codeium_common_pb.EmbeddingPrefix" json:"prefix,omitempty"`
	Priority EmbeddingPriority `protobuf:"varint,4,opt,name=priority,proto3,enum=codeium_common_pb.EmbeddingPriority" json:"priority,omitempty"`
}

// SearchResultRecord is a search result.
type SearchResultRecord struct {
	Id        string  `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	Type      SearchResultType `protobuf:"varint,2,opt,name=type,proto3,enum=codeium_common_pb.SearchResultType" json:"type,omitempty"`
	Title     string  `protobuf:"bytes,3,opt,name=title,proto3" json:"title,omitempty"`
	Snippet   string  `protobuf:"bytes,4,opt,name=snippet,proto3" json:"snippet,omitempty"`
	Score     float32 `protobuf:"fixed32,5,opt,name=score,proto3" json:"score,omitempty"`
	Uri       string  `protobuf:"bytes,6,opt,name=uri,proto3" json:"uri,omitempty"`
	Range     *Range  `protobuf:"bytes,7,opt,name=range,proto3" json:"range,omitempty"`
	Language  Language `protobuf:"varint,8,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	Metadata  *Metadata `protobuf:"bytes,9,opt,name=metadata,proto3" json:"metadata,omitempty"`
	EmbeddingPriority EmbeddingPriority `protobuf:"varint,10,opt,name=embedding_priority,json=embeddingPriority,proto3,enum=codeium_common_pb.EmbeddingPriority" json:"embedding_priority,omitempty"`
	Source    string  `protobuf:"bytes,11,opt,name=source,proto3" json:"source,omitempty"`
}

// TerminalShellCommand is a single shell command issued by Cascade.
type TerminalShellCommand struct {
	CommandLine     string `protobuf:"bytes,1,opt,name=command_line,json=commandLine,proto3" json:"command_line,omitempty"`
	Cwd             string `protobuf:"bytes,2,opt,name=cwd,proto3" json:"cwd,omitempty"`
	ExitCode        int32  `protobuf:"varint,3,opt,name=exit_code,json=exitCode,proto3" json:"exit_code,omitempty"`
	StartedAt       time.Time `protobuf:"bytes,4,opt,name=started_at,json=startedAt,proto3,stdtime" json:"started_at,omitempty"`
	CompletedAt     time.Time `protobuf:"bytes,5,opt,name=completed_at,json=completedAt,proto3,stdtime" json:"completed_at,omitempty"`
	Source          TerminalShellCommandSource `protobuf:"varint,6,opt,name=source,proto3,enum=codeium_common_pb.TerminalShellCommandSource" json:"source,omitempty"`
	Status          TerminalShellCommandStatus `protobuf:"varint,7,opt,name=status,proto3,enum=codeium_common_pb.TerminalShellCommandStatus" json:"status,omitempty"`
	ConversationId  string `protobuf:"bytes,8,opt,name=conversation_id,json=conversationId,proto3" json:"conversation_id,omitempty"`
	TerminalId      string `protobuf:"bytes,9,opt,name=terminal_id,json=terminalId,proto3" json:"terminal_id,omitempty"`
	Header          *TerminalShellCommandHeader `protobuf:"bytes,10,opt,name=header,proto3" json:"header,omitempty"`
	Trailer         *TerminalShellCommandTrailer `protobuf:"bytes,11,opt,name=trailer,proto3" json:"trailer,omitempty"`
}

// TerminalShellCommandHeader is the leading metadata for a stream.
type TerminalShellCommandHeader struct {
	CommandLine    string `protobuf:"bytes,1,opt,name=command_line,json=commandLine,proto3" json:"command_line,omitempty"`
	Cwd            string `protobuf:"bytes,2,opt,name=cwd,proto3" json:"cwd,omitempty"`
	StartedAt      time.Time `protobuf:"bytes,3,opt,name=started_at,json=startedAt,proto3,stdtime" json:"started_at,omitempty"`
	ConversationId string `protobuf:"bytes,4,opt,name=conversation_id,json=conversationId,proto3" json:"conversation_id,omitempty"`
	TerminalId     string `protobuf:"bytes,5,opt,name=terminal_id,json=terminalId,proto3" json:"terminal_id,omitempty"`
	Source         TerminalShellCommandSource `protobuf:"varint,6,opt,name=source,proto3,enum=codeium_common_pb.TerminalShellCommandSource" json:"source,omitempty"`
	HeaderId       string `protobuf:"bytes,7,opt,name=header_id,json=headerId,proto3" json:"header_id,omitempty"`
}

// TerminalShellCommandTrailer is the trailing metadata.
type TerminalShellCommandTrailer struct {
	ExitCode    int32  `protobuf:"varint,1,opt,name=exit_code,json=exitCode,proto3" json:"exit_code,omitempty"`
	CompletedAt time.Time `protobuf:"bytes,2,opt,name=completed_at,json=completedAt,proto3,stdtime" json:"completed_at,omitempty"`
}

// TerminalShellCommandStreamChunk is one chunk in the stream of a
// terminal shell command (extension_server_pb.ExecuteCommand stream).
type TerminalShellCommandStreamChunk struct {
	ChunkType string `protobuf:"bytes,1,opt,name=chunk_type,json=chunkType,proto3" json:"chunk_type,omitempty"`
	Data      []byte `protobuf:"bytes,2,opt,name=data,proto3" json:"data,omitempty"`
	Line      string `protobuf:"bytes,3,opt,name=line,proto3" json:"line,omitempty"`
	Sequence  int64  `protobuf:"varint,4,opt,name=sequence,proto3" json:"sequence,omitempty"`
}

// Media is a media attachment (image / audio / video).
type Media struct {
	MimeType    string  `protobuf:"bytes,1,opt,name=mime_type,json=mimeType,proto3" json:"mime_type,omitempty"`
	Data        []byte  `protobuf:"bytes,2,opt,name=data,proto3" json:"data,omitempty"`
	Url         string  `protobuf:"bytes,3,opt,name=url,proto3" json:"url,omitempty"`
	Width       int32   `protobuf:"varint,4,opt,name=width,proto3" json:"width,omitempty"`
	Height      int32   `protobuf:"varint,5,opt,name=height,proto3" json:"height,omitempty"`
	Duration    float32 `protobuf:"fixed32,6,opt,name=duration,proto3" json:"duration,omitempty"`
	Resolution  MediaResolution `protobuf:"varint,7,opt,name=resolution,proto3,enum=codeium_common_pb.MediaResolution" json:"resolution,omitempty"`
	Modality    Modality `protobuf:"varint,8,opt,name=modality,proto3,enum=codeium_common_pb.Modality" json:"modality,omitempty"`
	Hash        string  `protobuf:"bytes,9,opt,name=hash,proto3" json:"hash,omitempty"`
	Layer       VideoContentLayer `protobuf:"varint,10,opt,name=layer,proto3,enum=codeium_common_pb.VideoContentLayer" json:"layer,omitempty"`
	Encoding    string  `protobuf:"bytes,11,opt,name=encoding,proto3" json:"encoding,omitempty"`
	Bitrate     int32   `protobuf:"varint,12,opt,name=bitrate,proto3" json:"bitrate,omitempty"`
	SampleRate  int32   `protobuf:"varint,13,opt,name=sample_rate,json=sampleRate,proto3" json:"sample_rate,omitempty"`
	Channels    int32   `protobuf:"varint,14,opt,name=channels,proto3" json:"channels,omitempty"`
	FrameRate   float32 `protobuf:"fixed32,15,opt,name=frame_rate,json=frameRate,proto3" json:"frame_rate,omitempty"`
	Language    Language `protobuf:"varint,16,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	Source      string  `protobuf:"bytes,17,opt,name=source,proto3" json:"source,omitempty"`
	Caption     string  `protobuf:"bytes,18,opt,name=caption,proto3" json:"caption,omitempty"`
}

// Blobref is a reference to a blob.
type Blobref struct {
	Hash    string `protobuf:"bytes,1,opt,name=hash,proto3" json:"hash,omitempty"`
	Storage string `protobuf:"bytes,2,opt,name=storage,proto3" json:"storage,omitempty"`
	Uri     string `protobuf:"bytes,3,opt,name=uri,proto3" json:"uri,omitempty"`
}

// ImageData is an image attachment.
type ImageData struct {
	Uri      string `protobuf:"bytes,1,opt,name=uri,proto3" json:"uri,omitempty"`
	MimeType string `protobuf:"bytes,2,opt,name=mime_type,json=mimeType,proto3" json:"mime_type,omitempty"`
	Data     []byte `protobuf:"bytes,3,opt,name=data,proto3" json:"data,omitempty"`
	Width    int32  `protobuf:"varint,4,opt,name=width,proto3" json:"width,omitempty"`
	Height   int32  `protobuf:"varint,5,opt,name=height,proto3" json:"height,omitempty"`
}

// TextData is a text attachment.
type TextData struct {
	Text  string `protobuf:"bytes,1,opt,name=text,proto3" json:"text,omitempty"`
	Lang  Language `protobuf:"varint,2,opt,name=lang,proto3,enum=codeium_common_pb.Language" json:"lang,omitempty"`
	Hash  string `protobuf:"bytes,3,opt,name=hash,proto3" json:"hash,omitempty"`
	Size  int32  `protobuf:"varint,4,opt,name=size,proto3" json:"size,omitempty"`
}

// FunctionInfo is a function/method metadata.
type FunctionInfo struct {
	Name       string   `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	QualifiedName string `protobuf:"bytes,2,opt,name=qualified_name,json=qualifiedName,proto3" json:"qualified_name,omitempty"`
	Signature  string   `protobuf:"bytes,3,opt,name=signature,proto3" json:"signature,omitempty"`
	Docstring  string   `protobuf:"bytes,4,opt,name=docstring,proto3" json:"docstring,omitempty"`
	Range      *Range   `protobuf:"bytes,5,opt,name=range,proto3" json:"range,omitempty"`
	Language   Language `protobuf:"varint,6,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	ClassName  string   `protobuf:"bytes,7,opt,name=class_name,json=className,proto3" json:"class_name,omitempty"`
	IsAsync    bool     `protobuf:"varint,8,opt,name=is_async,json=isAsync,proto3" json:"is_async,omitempty"`
	IsStatic   bool     `protobuf:"varint,9,opt,name=is_static,json=isStatic,proto3" json:"is_static,omitempty"`
	IsExported bool     `protobuf:"varint,10,opt,name=is_exported,json=isExported,proto3" json:"is_exported,omitempty"`
	IsPublic   bool     `protobuf:"varint,11,opt,name=is_public,json=isPublic,proto3" json:"is_public,omitempty"`
	IsPrivate  bool     `protobuf:"varint,12,opt,name=is_private,json=isPrivate,proto3" json:"is_private,omitempty"`
	Parameters []string `protobuf:"bytes,13,rep,name=parameters,proto3" json:"parameters,omitempty"`
	ReturnType string   `protobuf:"bytes,14,opt,name=return_type,json=returnType,proto3" json:"return_type,omitempty"`
}

// ClassInfo is a class/type metadata.
type ClassInfo struct {
	Name       string        `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	QualifiedName string     `protobuf:"bytes,2,opt,name=qualified_name,json=qualifiedName,proto3" json:"qualified_name,omitempty"`
	Range      *Range        `protobuf:"bytes,3,opt,name=range,proto3" json:"range,omitempty"`
	Language   Language      `protobuf:"varint,4,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	Methods    []*FunctionInfo `protobuf:"bytes,5,rep,name=methods,proto3" json:"methods,omitempty"`
	Fields     []*FunctionInfo `protobuf:"bytes,6,rep,name=fields,proto3" json:"fields,omitempty"`
	BaseClasses []string     `protobuf:"bytes,7,rep,name=base_classes,json=baseClasses,proto3" json:"base_classes,omitempty"`
	Interfaces []string      `protobuf:"bytes,8,rep,name=interfaces,proto3" json:"interfaces,omitempty"`
	Docstring  string        `protobuf:"bytes,9,opt,name=docstring,proto3" json:"docstring,omitempty"`
	IsAbstract bool          `protobuf:"varint,10,opt,name=is_abstract,json=isAbstract,proto3" json:"is_abstract,omitempty"`
	IsExported bool          `protobuf:"varint,11,opt,name=is_exported,json=isExported,proto3" json:"is_exported,omitempty"`
	IsPublic   bool          `protobuf:"varint,12,opt,name=is_public,json=isPublic,proto3" json:"is_public,omitempty"`
	IsPrivate  bool          `protobuf:"varint,13,opt,name=is_private,json=isPrivate,proto3" json:"is_private,omitempty"`
}

// IndexerStats is the indexer runtime stats.
type IndexerStats struct {
	FilesIndexed  int64 `protobuf:"varint,1,opt,name=files_indexed,json=filesIndexed,proto3" json:"files_indexed,omitempty"`
	BytesIndexed  int64 `protobuf:"varint,2,opt,name=bytes_indexed,json=bytesIndexed,proto3" json:"bytes_indexed,omitempty"`
	EmbeddingsComputed int64 `protobuf:"varint,3,opt,name=embeddings_computed,json=embeddingsComputed,proto3" json:"embeddings_computed,omitempty"`
}

// IndexerDbStats is the indexer DB stats.
type IndexerDbStats struct {
	DocCount   int64 `protobuf:"varint,1,opt,name=doc_count,json=docCount,proto3" json:"doc_count,omitempty"`
	TotalSize  int64 `protobuf:"varint,2,opt,name=total_size,json=totalSize,proto3" json:"total_size,omitempty"`
	LastVacuum time.Time `protobuf:"bytes,3,opt,name=last_vacuum,json=lastVacuum,proto3,stdtime" json:"last_vacuum,omitempty"`
}

// LocalSqliteFaissDbStats is the local SQLite+FAISS db stats.
type LocalSqliteFaissDbStats struct {
	DbStats      *IndexerDbStats `protobuf:"bytes,1,opt,name=db_stats,json=dbStats,proto3" json:"db_stats,omitempty"`
	FaissStats   *FaissStateStats `protobuf:"bytes,2,opt,name=faiss_stats,json=faissStats,proto3" json:"faiss_stats,omitempty"`
	LastSyncedAt time.Time       `protobuf:"bytes,3,opt,name=last_synced_at,json=lastSyncedAt,proto3,stdtime" json:"last_synced_at,omitempty"`
}

// FaissStateStats is the FAISS index stats.
type FaissStateStats struct {
	TotalVectors int64 `protobuf:"varint,1,opt,name=total_vectors,json=totalVectors,proto3" json:"total_vectors,omitempty"`
	Dimension    int32 `protobuf:"varint,2,opt,name=dimension,proto3" json:"dimension,omitempty"`
	IndexType    string `protobuf:"bytes,3,opt,name=index_type,json=indexType,proto3" json:"index_type,omitempty"`
}

// WorkspaceStats is the workspace stats.
type WorkspaceStats struct {
	Files     int64  `protobuf:"varint,1,opt,name=files,proto3" json:"files,omitempty"`
	Lines     int64  `protobuf:"varint,2,opt,name=lines,proto3" json:"lines,omitempty"`
	Languages []string `protobuf:"bytes,3,rep,name=languages,proto3" json:"languages,omitempty"`
	SizeBytes int64  `protobuf:"varint,4,opt,name=size_bytes,json=sizeBytes,proto3" json:"size_bytes,omitempty"`
}

// UserTableStats is the per-user usage stats.
type UserTableStats struct {
	CompletionCount int64 `protobuf:"varint,1,opt,name=completion_count,json=completionCount,proto3" json:"completion_count,omitempty"`
	ChatCount       int64 `protobuf:"varint,2,opt,name=chat_count,json=chatCount,proto3" json:"chat_count,omitempty"`
	CommandCount    int64 `protobuf:"varint,3,opt,name=command_count,json=commandCount,proto3" json:"command_count,omitempty"`
	AcceptanceRate  float32 `protobuf:"fixed32,4,opt,name=acceptance_rate,json=acceptanceRate,proto3" json:"acceptance_rate,omitempty"`
	LastActiveAt    time.Time `protobuf:"bytes,5,opt,name=last_active_at,json=lastActiveAt,proto3,stdtime" json:"last_active_at,omitempty"`
	StreakDays      int32  `protobuf:"varint,6,opt,name=streak_days,json=streakDays,proto3" json:"streak_days,omitempty"`
	IndexedFiles    int64  `protobuf:"varint,7,opt,name=indexed_files,json=indexedFiles,proto3" json:"indexed_files,omitempty"`
	IndexedBytes    int64  `protobuf:"varint,8,opt,name=indexed_bytes,json=indexedBytes,proto3" json:"indexed_bytes,omitempty"`
	EmbeddingsComputed int64 `protobuf:"varint,9,opt,name=embeddings_computed,json=embeddingsComputed,proto3" json:"embeddings_computed,omitempty"`
	CostUsd         float32 `protobuf:"fixed32,10,opt,name=cost_usd,json=costUsd,proto3" json:"cost_usd,omitempty"`
	TokensUsed      int64  `protobuf:"varint,11,opt,name=tokens_used,json=tokensUsed,proto3" json:"tokens_used,omitempty"`
	ErrorsReported  int32  `protobuf:"varint,12,opt,name=errors_reported,json=errorsReported,proto3" json:"errors_reported,omitempty"`
}

// ModelUsageStats is the per-model usage stats.
type ModelUsageStats struct {
	Model         string  `protobuf:"bytes,1,opt,name=model,proto3" json:"model,omitempty"`
	CompletionCount int64 `protobuf:"varint,2,opt,name=completion_count,json=completionCount,proto3" json:"completion_count,omitempty"`
	ChatCount     int64   `protobuf:"varint,3,opt,name=chat_count,json=chatCount,proto3" json:"chat_count,omitempty"`
	CommandCount  int64   `protobuf:"varint,4,opt,name=command_count,json=commandCount,proto3" json:"command_count,omitempty"`
	AcceptanceRate float32 `protobuf:"fixed32,5,opt,name=acceptance_rate,json=acceptanceRate,proto3" json:"acceptance_rate,omitempty"`
	AvgLatencyMs  int32   `protobuf:"varint,6,opt,name=avg_latency_ms,json=avgLatencyMs,proto3" json:"avg_latency_ms,omitempty"`
	CostUsd       float32 `protobuf:"fixed32,7,opt,name=cost_usd,json=costUsd,proto3" json:"cost_usd,omitempty"`
	TokensUsed    int64   `protobuf:"varint,8,opt,name=tokens_used,json=tokensUsed,proto3" json:"tokens_used,omitempty"`
	ErrorsReported int32  `protobuf:"varint,9,opt,name=errors_reported,json=errorsReported,proto3" json:"errors_reported,omitempty"`
}

// ChatStats is the chat usage stats.
type ChatStats struct {
	MessageCount int64 `protobuf:"varint,1,opt,name=message_count,json=messageCount,proto3" json:"message_count,omitempty"`
	UserMessages int64 `protobuf:"varint,2,opt,name=user_messages,json=userMessages,proto3" json:"user_messages,omitempty"`
	AiMessages   int64 `protobuf:"varint,3,opt,name=ai_messages,json=aiMessages,proto3" json:"ai_messages,omitempty"`
	ToolCalls    int64 `protobuf:"varint,4,opt,name=tool_calls,json=toolCalls,proto3" json:"tool_calls,omitempty"`
	AvgLatencyMs int32 `protobuf:"varint,5,opt,name=avg_latency_ms,json=avgLatencyMs,proto3" json:"avg_latency_ms,omitempty"`
	TotalCostUsd float32 `protobuf:"fixed32,6,opt,name=total_cost_usd,json=totalCostUsd,proto3" json:"total_cost_usd,omitempty"`
	TotalTokens  int64 `protobuf:"varint,7,opt,name=total_tokens,json=totalTokens,proto3" json:"total_tokens,omitempty"`
	CascadeCount int64 `protobuf:"varint,8,opt,name=cascade_count,json=cascadeCount,proto3" json:"cascade_count,omitempty"`
	AbortCount   int64 `protobuf:"varint,9,opt,name=abort_count,json=abortCount,proto3" json:"abort_count,omitempty"`
	AcceptCount  int64 `protobuf:"varint,10,opt,name=accept_count,json=acceptCount,proto3" json:"accept_count,omitempty"`
	RejectCount  int64 `protobuf:"varint,11,opt,name=reject_count,json=rejectCount,proto3" json:"reject_count,omitempty"`
	RetryCount   int64 `protobuf:"varint,12,opt,name=retry_count,json=retryCount,proto3" json:"retry_count,omitempty"`
	AvgMsgsPerCascade float32 `protobuf:"fixed32,13,opt,name=avg_msgs_per_cascade,json=avgMsgsPerCascade,proto3" json:"avg_msgs_per_cascade,omitempty"`
	ErrorCount   int64 `protobuf:"varint,14,opt,name=error_count,json=errorCount,proto3" json:"error_count,omitempty"`
	LastUsedAt   time.Time `protobuf:"bytes,15,opt,name=last_used_at,json=lastUsedAt,proto3,stdtime" json:"last_used_at,omitempty"`
}

// CommandStats is the command-usage stats.
type CommandStats struct {
	CommandName  string  `protobuf:"bytes,1,opt,name=command_name,json=commandName,proto3" json:"command_name,omitempty"`
	RunCount     int64   `protobuf:"varint,2,opt,name=run_count,json=runCount,proto3" json:"run_count,omitempty"`
	AcceptCount  int64   `protobuf:"varint,3,opt,name=accept_count,json=acceptCount,proto3" json:"accept_count,omitempty"`
	RejectCount  int64   `protobuf:"varint,4,opt,name=reject_count,json=rejectCount,proto3" json:"reject_count,omitempty"`
	AvgRunTimeMs int32   `protobuf:"varint,5,opt,name=avg_run_time_ms,json=avgRunTimeMs,proto3" json:"avg_run_time_ms,omitempty"`
	LastRunAt    time.Time `protobuf:"bytes,6,opt,name=last_run_at,json=lastRunAt,proto3,stdtime" json:"last_run_at,omitempty"`
	TotalCostUsd float32 `protobuf:"fixed32,7,opt,name=total_cost_usd,json=totalCostUsd,proto3" json:"total_cost_usd,omitempty"`
	TotalTokens  int64   `protobuf:"varint,8,opt,name=total_tokens,json=tokens,proto3" json:"total_tokens,omitempty"`
	Source       CommandRequestSource `protobuf:"varint,9,opt,name=source,proto3,enum=codeium_common_pb.CommandRequestSource" json:"source,omitempty"`
	WasAutoRun   bool    `protobuf:"varint,10,opt,name=was_auto_run,json=wasAutoRun,proto3" json:"was_auto_run,omitempty"`
	FailureCount int64   `protobuf:"varint,11,opt,name=failure_count,json=failureCount,proto3" json:"failure_count,omitempty"`
	TimeoutCount int64   `protobuf:"varint,12,opt,name=timeout_count,json=timeoutCount,proto3" json:"timeout_count,omitempty"`
}

// ChatToolCall is a tool call from the chat assistant.
type ChatToolCall struct {
	Name      string  `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Arguments string  `protobuf:"bytes,2,opt,name=arguments,proto3" json:"arguments,omitempty"`
	Result    string  `protobuf:"bytes,3,opt,name=result,proto3" json:"result,omitempty"`
	Error     string  `protobuf:"bytes,4,opt,name=error,proto3" json:"error,omitempty"`
	StartedAt time.Time `protobuf:"bytes,5,opt,name=started_at,json=startedAt,proto3,stdtime" json:"started_at,omitempty"`
	CompletedAt time.Time `protobuf:"bytes,6,opt,name=completed_at,json=completedAt,proto3,stdtime" json:"completed_at,omitempty"`
	ToolUseId  string `protobuf:"bytes,7,opt,name=tool_use_id,json=toolUseId,proto3" json:"tool_use_id,omitempty"`
	WasAccepted bool `protobuf:"varint,8,opt,name=was_accepted,json=wasAccepted,proto3" json:"was_accepted,omitempty"`
	LatencyMs  int32  `protobuf:"varint,9,opt,name=latency_ms,json=latencyMs,proto3" json:"latency_ms,omitempty"`
	CostUsd    float32 `protobuf:"fixed32,10,opt,name=cost_usd,json=costUsd,proto3" json:"cost_usd,omitempty"`
}

// WorkspaceIndexData is the workspace index snapshot.
type WorkspaceIndexData struct {
	WorkspaceId   string  `protobuf:"bytes,1,opt,name=workspace_id,json=workspaceId,proto3" json:"workspace_id,omitempty"`
	RootPath      string  `protobuf:"bytes,2,opt,name=root_path,json=rootPath,proto3" json:"root_path,omitempty"`
	IndexedAt     time.Time `protobuf:"bytes,3,opt,name=indexed_at,json=indexedAt,proto3,stdtime" json:"indexed_at,omitempty"`
	FilesIndexed  int64   `protobuf:"varint,4,opt,name=files_indexed,json=filesIndexed,proto3" json:"files_indexed,omitempty"`
	TotalSizeBytes int64  `protobuf:"varint,5,opt,name=total_size_bytes,json=totalSizeBytes,proto3" json:"total_size_bytes,omitempty"`
	EmbeddingsCount int64 `protobuf:"varint,6,opt,name=embeddings_count,json=embeddingsCount,proto3" json:"embeddings_count,omitempty"`
	Stats         *WorkspaceStats `protobuf:"bytes,7,opt,name=stats,proto3" json:"stats,omitempty"`
	Documents     []*Document `protobuf:"bytes,8,rep,name=documents,proto3" json:"documents,omitempty"`
	Languages     []Language `protobuf:"varint,9,rep,packed,name=languages,proto3,enum=codeium_common_pb.Language" json:"languages,omitempty"`
	DbStats       *IndexerDbStats `protobuf:"bytes,10,opt,name=db_stats,json=dbStats,proto3" json:"db_stats,omitempty"`
	PartialMetadata *PartialIndexMetadata `protobuf:"bytes,11,opt,name=partial_metadata,json=partialMetadata,proto3" json:"partial_metadata,omitempty"`
}

// PartialIndexMetadata is the partial-index state metadata.
type PartialIndexMetadata struct {
	IsPartial  bool   `protobuf:"varint,1,opt,name=is_partial,json=isPartial,proto3" json:"is_partial,omitempty"`
	Reason     string `protobuf:"bytes,2,opt,name=reason,proto3" json:"reason,omitempty"`
	RetryAfter time.Time `protobuf:"bytes,3,opt,name=retry_after,json=retryAfter,proto3,stdtime" json:"retry_after,omitempty"`
}

// ErrorTrace is an error trace for telemetry.
type ErrorTrace struct {
	Message    string    `protobuf:"bytes,1,opt,name=message,proto3" json:"message,omitempty"`
	Stack      string    `protobuf:"bytes,2,opt,name=stack,proto3" json:"stack,omitempty"`
	Code       string    `protobuf:"bytes,3,opt,name=code,proto3" json:"code,omitempty"`
	Category   CortexErrorCategory `protobuf:"varint,4,opt,name=category,proto3,enum=codeium_common_pb.CortexErrorCategory" json:"category,omitempty"`
	Severity   StatusLevel `protobuf:"varint,5,opt,name=severity,proto3,enum=codeium_common_pb.StatusLevel" json:"severity,omitempty"`
	Timestamp  time.Time `protobuf:"bytes,6,opt,name=timestamp,proto3,stdtime" json:"timestamp,omitempty"`
	Service    string    `protobuf:"bytes,7,opt,name=service,proto3" json:"service,omitempty"`
	UserId     string    `protobuf:"bytes,8,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	Metadata   *Metadata `protobuf:"bytes,9,opt,name=metadata,proto3" json:"metadata,omitempty"`
}

// ContextScopeItem is one scope item in the context.
type ContextScopeItem struct {
	Uri    string   `protobuf:"bytes,1,opt,name=uri,proto3" json:"uri,omitempty"`
	Type   ContextScopeType `protobuf:"varint,2,opt,name=type,proto3,enum=codeium_common_pb.ContextScopeType" json:"type,omitempty"`
	Range  *Range   `protobuf:"bytes,3,opt,name=range,proto3" json:"range,omitempty"`
	Reason ContextInclusionType `protobuf:"varint,4,opt,name=reason,proto3,enum=codeium_common_pb.ContextInclusionType" json:"reason,omitempty"`
	Weight float32  `protobuf:"fixed32,5,opt,name=weight,proto3" json:"weight,omitempty"`
	Source ContextSnippetType `protobuf:"varint,6,opt,name=source,proto3,enum=codeium_common_pb.ContextSnippetType" json:"source,omitempty"`
	Text   string   `protobuf:"bytes,7,opt,name=text,proto3" json:"text,omitempty"`
	Language Language `protobuf:"varint,8,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	Tokens int32    `protobuf:"varint,9,opt,name=tokens,proto3" json:"tokens,omitempty"`
	Hash   string   `protobuf:"bytes,10,opt,name=hash,proto3" json:"hash,omitempty"`
	Metadata *Metadata `protobuf:"bytes,11,opt,name=metadata,proto3" json:"metadata,omitempty"`
	InlineRefs []*ContextSubrange `protobuf:"bytes,12,rep,name=inline_refs,json=inlineRefs,proto3" json:"inline_refs,omitempty"`
	ExcludedReason PromptElementExclusionReason `protobuf:"varint,13,opt,name=excluded_reason,json=excludedReason,proto3,enum=codeium_common_pb.PromptElementExclusionReason" json:"excluded_reason,omitempty"`
}

// ContextSubrange is a subrange of context.
type ContextSubrange struct {
	Range      *Range `protobuf:"bytes,1,opt,name=range,proto3" json:"range,omitempty"`
	Reason     string `protobuf:"bytes,2,opt,name=reason,proto3" json:"reason,omitempty"`
	Weight     float32 `protobuf:"fixed32,3,opt,name=weight,proto3" json:"weight,omitempty"`
}

// ContextScope is the parent context.
type ContextScope struct {
	Items []*ContextScopeItem `protobuf:"bytes,1,rep,name=items,proto3" json:"items,omitempty"`
}

// Citations is a single citation in a model response.
type Citation struct {
	Title    string  `protobuf:"bytes,1,opt,name=title,proto3" json:"title,omitempty"`
	Url      string  `protobuf:"bytes,2,opt,name=url,proto3" json:"url,omitempty"`
	Snippet  string  `protobuf:"bytes,3,opt,name=snippet,proto3" json:"snippet,omitempty"`
	License  string  `protobuf:"bytes,4,opt,name=license,proto3" json:"license,omitempty"`
	Attribution AttributionStatus `protobuf:"varint,5,opt,name=attribution,proto3,enum=codeium_common_pb.AttributionStatus" json:"attribution,omitempty"`
}

// CitationMetadata is the citation set for a response.
type CitationMetadata struct {
	Citations []*Citation `protobuf:"bytes,1,rep,name=citations,proto3" json:"citations,omitempty"`
}

// GroundingMetadata is the grounding source for a response.
type GroundingMetadata struct {
	Sources []*SearchResultRecord `protobuf:"bytes,1,rep,name=sources,proto3" json:"sources,omitempty"`
}

// Recitation is a recitation source.
type Recitation struct {
	Title    string `protobuf:"bytes,1,opt,name=title,proto3" json:"title,omitempty"`
	Url      string `protobuf:"bytes,2,opt,name=url,proto3" json:"url,omitempty"`
	Snippet  string `protobuf:"bytes,3,opt,name=snippet,proto3" json:"snippet,omitempty"`
}

// RecitationMetadata is the recitation set.
type RecitationMetadata struct {
	Recitations []*Recitation `protobuf:"bytes,1,rep,name=recitations,proto3" json:"recitations,omitempty"`
}

// ModalityTokenCount is the per-modality token count.
type ModalityTokenCount struct {
	Modality Modality `protobuf:"varint,1,opt,name=modality,proto3,enum=codeium_common_pb.Modality" json:"modality,omitempty"`
	TokenCount int32  `protobuf:"varint,2,opt,name=token_count,json=tokenCount,proto3" json:"token_count,omitempty"`
}

// ChatCompletionInfo is the chat completion metadata.
type ChatCompletionInfo struct {
	Model    string `protobuf:"bytes,1,opt,name=model,proto3" json:"model,omitempty"`
	Tokens   int64  `protobuf:"varint,2,opt,name=tokens,proto3" json:"tokens,omitempty"`
	CostUsd  float32 `protobuf:"fixed32,3,opt,name=cost_usd,json=costUsd,proto3" json:"cost_usd,omitempty"`
}

// ChatNodeConfig is the chat node config.
type ChatNodeConfig struct {
	NodeId     string  `protobuf:"bytes,1,opt,name=node_id,json=nodeId,proto3" json:"node_id,omitempty"`
	Model      string  `protobuf:"bytes,2,opt,name=model,proto3" json:"model,omitempty"`
	MaxTokens  int32   `protobuf:"varint,3,opt,name=max_tokens,json=maxTokens,proto3" json:"max_tokens,omitempty"`
	Temperature float32 `protobuf:"fixed32,4,opt,name=temperature,proto3" json:"temperature,omitempty"`
	TopP       float32 `protobuf:"fixed32,5,opt,name=top_p,json=topP,proto3" json:"top_p,omitempty"`
}

// CompletionsRequest is the upstream completions RPC request.
type CompletionsRequest struct {
	Model             string  `protobuf:"bytes,1,opt,name=model,proto3" json:"model,omitempty"`
	Prompt            string  `protobuf:"bytes,2,opt,name=prompt,proto3" json:"prompt,omitempty"`
	MaxTokens         int32   `protobuf:"varint,3,opt,name=max_tokens,json=maxTokens,proto3" json:"max_tokens,omitempty"`
	Temperature       float32 `protobuf:"fixed32,4,opt,name=temperature,proto3" json:"temperature,omitempty"`
	TopP              float32 `protobuf:"fixed32,5,opt,name=top_p,json=topP,proto3" json:"top_p,omitempty"`
	StopSequences     []string `protobuf:"bytes,6,rep,name=stop_sequences,json=stopSequences,proto3" json:"stop_sequences,omitempty"`
	Stream            bool    `protobuf:"varint,7,opt,name=stream,proto3" json:"stream,omitempty"`
	N                 int32   `protobuf:"varint,8,opt,name=n,proto3" json:"n,omitempty"`
	User              string  `protobuf:"bytes,9,opt,name=user,proto3" json:"user,omitempty"`
	Language          Language `protobuf:"varint,10,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	Source            CompletionSource `protobuf:"varint,11,opt,name=source,proto3,enum=codeium_common_pb.CompletionSource" json:"source,omitempty"`
	Metadata          *Metadata `protobuf:"bytes,12,opt,name=metadata,proto3" json:"metadata,omitempty"`
	ContextItems      []*ContextScopeItem `protobuf:"bytes,13,rep,name=context_items,json=contextItems,proto3" json:"context_items,omitempty"`
	Documents         []*Document `protobuf:"bytes,14,rep,name=documents,proto3" json:"documents,omitempty"`
	Functions         []*FunctionInfo `protobuf:"bytes,15,rep,name=functions,proto3" json:"functions,omitempty"`
	ToolChoice        string  `protobuf:"bytes,16,opt,name=tool_choice,json=toolChoice,proto3" json:"tool_choice,omitempty"`
	ResponseFormat    string  `protobuf:"bytes,17,opt,name=response_format,json=responseFormat,proto3" json:"response_format,omitempty"`
	SystemPrompt      string  `protobuf:"bytes,18,opt,name=system_prompt,json=systemPrompt,proto3" json:"system_prompt,omitempty"`
	IncludeCitations  bool    `protobuf:"varint,19,opt,name=include_citations,json=includeCitations,proto3" json:"include_citations,omitempty"`
	IncludeGrounding  bool    `protobuf:"varint,20,opt,name=include_grounding,json=includeGrounding,proto3" json:"include_grounding,omitempty"`
	IncludeRecitation bool    `protobuf:"varint,21,opt,name=include_recitation,json=includeRecitation,proto3" json:"include_recitation,omitempty"`
	Image             *ImageData `protobuf:"bytes,22,opt,name=image,proto3" json:"image,omitempty"`
	Media             []*Media `protobuf:"bytes,23,rep,name=media,proto3" json:"media,omitempty"`
	EnableThinking    bool    `protobuf:"varint,24,opt,name=enable_thinking,json=enableThinking,proto3" json:"enable_thinking,omitempty"`
	ThinkingBudget    int32   `protobuf:"varint,25,opt,name=thinking_budget,json=thinkingBudget,proto3" json:"thinking_budget,omitempty"`
	ConversationId    string  `protobuf:"bytes,26,opt,name=conversation_id,json=conversationId,proto3" json:"conversation_id,omitempty"`
}

// CompletionResponse is the upstream completions RPC response.
type CompletionResponse struct {
	Id       string             `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	Model    string             `protobuf:"bytes,2,opt,name=model,proto3" json:"model,omitempty"`
	Choices  []*Completion      `protobuf:"bytes,3,rep,name=choices,proto3" json:"choices,omitempty"`
	Usage    *UsageMetadata     `protobuf:"bytes,4,opt,name=usage,proto3" json:"usage,omitempty"`
	CreatedAt time.Time         `protobuf:"bytes,5,opt,name=created_at,json=createdAt,proto3,stdtime" json:"created_at,omitempty"`
	Citations *CitationMetadata `protobuf:"bytes,6,opt,name=citations,proto3" json:"citations,omitempty"`
	Grounding *GroundingMetadata `protobuf:"bytes,7,opt,name=grounding,proto3" json:"grounding,omitempty"`
	Recitation *RecitationMetadata `protobuf:"bytes,8,opt,name=recitation,proto3" json:"recitation,omitempty"`
	ConversationId string `protobuf:"bytes,9,opt,name=conversation_id,json=conversationId,proto3" json:"conversation_id,omitempty"`
	FinishReason string `protobuf:"bytes,10,opt,name=finish_reason,json=finishReason,proto3" json:"finish_reason,omitempty"`
}

// UsageMetadata tracks token usage (subset of model response).
type UsageMetadata struct {
	PromptTokens     int32 `protobuf:"varint,1,opt,name=prompt_tokens,json=promptTokens,proto3" json:"prompt_tokens,omitempty"`
	CompletionTokens int32 `protobuf:"varint,2,opt,name=completion_tokens,json=completionTokens,proto3" json:"completion_tokens,omitempty"`
	TotalTokens      int32 `protobuf:"varint,3,opt,name=total_tokens,json=totalTokens,proto3" json:"total_tokens,omitempty"`
	ThoughtTokens    int32 `protobuf:"varint,4,opt,name=thought_tokens,json=thoughtTokens,proto3" json:"thought_tokens,omitempty"`
	CachedTokens     int32 `protobuf:"varint,5,opt,name=cached_tokens,json=cachedTokens,proto3" json:"cached_tokens,omitempty"`
	ModalityCounts   []*ModalityTokenCount `protobuf:"bytes,6,rep,name=modality_counts,json=modalityCounts,proto3" json:"modality_counts,omitempty"`
}

// StreamingCompletion is a single streaming chunk.
type StreamingCompletion struct {
	Id        string  `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	Model     string  `protobuf:"bytes,2,opt,name=model,proto3" json:"model,omitempty"`
	DeltaText string  `protobuf:"bytes,3,opt,name=delta_text,json=deltaText,proto3" json:"delta_text,omitempty"`
	Delta     *CompletionDelta `protobuf:"bytes,4,opt,name=delta,proto3" json:"delta,omitempty"`
	FinishReason string `protobuf:"bytes,5,opt,name=finish_reason,json=finishReason,proto3" json:"finish_reason,omitempty"`
	Sequence  int64   `protobuf:"varint,6,opt,name=sequence,proto3" json:"sequence,omitempty"`
	Usage     *UsageMetadata `protobuf:"bytes,7,opt,name=usage,proto3" json:"usage,omitempty"`
	Citations *CitationMetadata `protobuf:"bytes,8,opt,name=citations,proto3" json:"citations,omitempty"`
	Grounding *GroundingMetadata `protobuf:"bytes,9,opt,name=grounding,proto3" json:"grounding,omitempty"`
}

// CompletionDelta is the delta in a streaming response.
type CompletionDelta struct {
	Role       string `protobuf:"bytes,1,opt,name=role,proto3" json:"role,omitempty"`
	Content    string `protobuf:"bytes,2,opt,name=content,proto3" json:"content,omitempty"`
	FunctionName string `protobuf:"bytes,3,opt,name=function_name,json=functionName,proto3" json:"function_name,omitempty"`
	FunctionArgs string `protobuf:"bytes,4,opt,name=function_args,json=functionArgs,proto3" json:"function_args,omitempty"`
}

// StreamingCompletionResponse is the streamed response wrapper.
type StreamingCompletionResponse struct {
	Chunks []*StreamingCompletion `protobuf:"bytes,1,rep,name=chunks,proto3" json:"chunks,omitempty"`
	Usage  *UsageMetadata         `protobuf:"bytes,2,opt,name=usage,proto3" json:"usage,omitempty"`
	Done   bool                   `protobuf:"varint,3,opt,name=done,proto3" json:"done,omitempty"`
	Sequence int64                `protobuf:"varint,4,opt,name=sequence,proto3" json:"sequence,omitempty"`
	FinishReason string           `protobuf:"bytes,5,opt,name=finish_reason,json=finishReason,proto3" json:"finish_reason,omitempty"`
}

// WorkingDirectoryInfo is the working directory info.
type WorkingDirectoryInfo struct {
	Path        string `protobuf:"bytes,1,opt,name=path,proto3" json:"path,omitempty"`
	Status      WorkingDirectoryStatus `protobuf:"varint,2,opt,name=status,proto3,enum=codeium_common_pb.WorkingDirectoryStatus" json:"status,omitempty"`
	GitBranch   string `protobuf:"bytes,3,opt,name=git_branch,json=gitBranch,proto3" json:"git_branch,omitempty"`
	LastTouched time.Time `protobuf:"bytes,4,opt,name=last_touched,json=lastTouched,proto3,stdtime" json:"last_touched,omitempty"`
}

// WorkspacePath is a workspace path.
type WorkspacePath struct {
	Path     string  `protobuf:"bytes,1,opt,name=path,proto3" json:"path,omitempty"`
	Root     bool    `protobuf:"varint,2,opt,name=root,proto3" json:"root,omitempty"`
	Subdir   bool    `protobuf:"varint,3,opt,name=subdir,proto3" json:"subdir,omitempty"`
}

// RepositoryPath is a path within a repository.
type RepositoryPath struct {
	Path    string `protobuf:"bytes,1,opt,name=path,proto3" json:"path,omitempty"`
	Branch  string `protobuf:"bytes,2,opt,name=branch,proto3" json:"branch,omitempty"`
	Commit  string `protobuf:"bytes,3,opt,name=commit,proto3" json:"commit,omitempty"`
}

// GitRepoInfo is the git repo info.
type GitRepoInfo struct {
	Branch       string `protobuf:"bytes,1,opt,name=branch,proto3" json:"branch,omitempty"`
	RemoteUrl    string `protobuf:"bytes,2,opt,name=remote_url,json=remoteUrl,proto3" json:"remote_url,omitempty"`
	Commit       string `protobuf:"bytes,3,opt,name=commit,proto3" json:"commit,omitempty"`
	CommitAuthor string `protobuf:"bytes,4,opt,name=commit_author,json=commitAuthor,proto3" json:"commit_author,omitempty"`
	CommitMessage string `protobuf:"bytes,5,opt,name=commit_message,json=commitMessage,proto3" json:"commit_message,omitempty"`
	IsDirty      bool   `protobuf:"varint,6,opt,name=is_dirty,json=isDirty,proto3" json:"is_dirty,omitempty"`
	AheadCount   int32  `protobuf:"varint,7,opt,name=ahead_count,json=aheadCount,proto3" json:"ahead_count,omitempty"`
	BehindCount  int32  `protobuf:"varint,8,opt,name=behind_count,json=behindCount,proto3" json:"behind_count,omitempty"`
}

// PerforceDepotInfo is the perforce depot info.
type PerforceDepotInfo struct {
	Depot    string `protobuf:"bytes,1,opt,name=depot,proto3" json:"depot,omitempty"`
	Client   string `protobuf:"bytes,2,opt,name=client,proto3" json:"client,omitempty"`
	Change   string `protobuf:"bytes,3,opt,name=change,proto3" json:"change,omitempty"`
}

// GraphExecutionState is the graph executor state.
type GraphExecutionState struct {
	GraphId   string `protobuf:"bytes,1,opt,name=graph_id,json=graphId,proto3" json:"graph_id,omitempty"`
	NodeId    string `protobuf:"bytes,2,opt,name=node_id,json=nodeId,proto3" json:"node_id,omitempty"`
	Step      int32  `protobuf:"varint,3,opt,name=step,proto3" json:"step,omitempty"`
	MaxSteps  int32  `protobuf:"varint,4,opt,name=max_steps,json=maxSteps,proto3" json:"max_steps,omitempty"`
	Status    string `protobuf:"bytes,5,opt,name=status,proto3" json:"status,omitempty"`
	Error     string `protobuf:"bytes,6,opt,name=error,proto3" json:"error,omitempty"`
	StartedAt time.Time `protobuf:"bytes,7,opt,name=started_at,json=startedAt,proto3,stdtime" json:"started_at,omitempty"`
	CompletedAt time.Time `protobuf:"bytes,8,opt,name=completed_at,json=completedAt,proto3,stdtime" json:"completed_at,omitempty"`
}

// NodeExecutionRecord is a single node execution record.
type NodeExecutionRecord struct {
	NodeId    string  `protobuf:"bytes,1,opt,name=node_id,json=nodeId,proto3" json:"node_id,omitempty"`
	StartedAt time.Time `protobuf:"bytes,2,opt,name=started_at,json=startedAt,proto3,stdtime" json:"started_at,omitempty"`
	CompletedAt time.Time `protobuf:"bytes,3,opt,name=completed_at,json=completedAt,proto3,stdtime" json:"completed_at,omitempty"`
	Status    string  `protobuf:"bytes,4,opt,name=status,proto3" json:"status,omitempty"`
	Output    string  `protobuf:"bytes,5,opt,name=output,proto3" json:"output,omitempty"`
	Error     string  `protobuf:"bytes,6,opt,name=error,proto3" json:"error,omitempty"`
}

// UnleashContext is the Unleash feature-flag context.
type UnleashContext struct {
	UserId    string `protobuf:"bytes,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	SessionId string `protobuf:"bytes,2,opt,name=session_id,json=sessionId,proto3" json:"session_id,omitempty"`
	AppName   string `protobuf:"bytes,3,opt,name=app_name,json=appName,proto3" json:"app_name,omitempty"`
}

// ExperimentConfig is the experiment config.
type ExperimentConfig struct {
	Experiments []*Experiment `protobuf:"bytes,1,rep,name=experiments,proto3" json:"experiments,omitempty"`
	UpdatedAt   time.Time     `protobuf:"bytes,2,opt,name=updated_at,json=updatedAt,proto3,stdtime" json:"updated_at,omitempty"`
	Source      ExperimentSource `protobuf:"varint,3,opt,name=source,proto3,enum=codeium_common_pb.ExperimentSource" json:"source,omitempty"`
}

// Experiment is a single experiment.
type Experiment struct {
	Key   ExperimentKey `protobuf:"varint,1,opt,name=key,proto3,enum=codeium_common_pb.ExperimentKey" json:"key,omitempty"`
	Value string `protobuf:"bytes,2,opt,name=value,proto3" json:"value,omitempty"`
}

// ExperimentWithVariant is an experiment with a variant.
type ExperimentWithVariant struct {
	Key     ExperimentKey `protobuf:"varint,1,opt,name=key,proto3,enum=codeium_common_pb.ExperimentKey" json:"key,omitempty"`
	Variant string `protobuf:"bytes,2,opt,name=variant,proto3" json:"variant,omitempty"`
	Payload []byte `protobuf:"bytes,3,opt,name=payload,proto3" json:"payload,omitempty"`
	Enabled bool `protobuf:"varint,4,opt,name=enabled,proto3" json:"enabled,omitempty"`
	HasVariant bool `protobuf:"varint,5,opt,name=has_variant,json=hasVariant,proto3" json:"has_variant,omitempty"`
	Parameters *Metadata `protobuf:"bytes,6,opt,name=parameters,proto3" json:"parameters,omitempty"`
	Type string `protobuf:"bytes,7,opt,name=type,proto3" json:"type,omitempty"`
	Description string `protobuf:"bytes,8,opt,name=description,proto3" json:"description,omitempty"`
}

// ExternalModel is an external (BYOK) model.
type ExternalModel struct {
	ModelId        string  `protobuf:"bytes,1,opt,name=model_id,json=modelId,proto3" json:"model_id,omitempty"`
	ModelName      string  `protobuf:"bytes,2,opt,name=model_name,json=modelName,proto3" json:"model_name,omitempty"`
	Provider       ModelProvider `protobuf:"varint,3,opt,name=provider,proto3,enum=codeium_common_pb.ModelProvider" json:"provider,omitempty"`
	ApiKey         string  `protobuf:"bytes,4,opt,name=api_key,json=apiKey,proto3" json:"api_key,omitempty"`
	BaseUrl        string  `protobuf:"bytes,5,opt,name=base_url,json=baseUrl,proto3" json:"base_url,omitempty"`
	MaxOutputTokens int32   `protobuf:"varint,6,opt,name=max_output_tokens,json=maxOutputTokens,proto3" json:"max_output_tokens,omitempty"`
	ContextLimit   int32   `protobuf:"varint,7,opt,name=context_limit,json=contextLimit,proto3" json:"context_limit,omitempty"`
	SupportsTools  bool    `protobuf:"varint,8,opt,name=supports_tools,json=supportsTools,proto3" json:"supports_tools,omitempty"`
	SupportsImages bool    `protobuf:"varint,9,opt,name=supports_images,json=supportsImages,proto3" json:"supports_images,omitempty"`
	SupportsStreaming bool `protobuf:"varint,10,opt,name=supports_streaming,json=supportsStreaming,proto3" json:"supports_streaming,omitempty"`
	CostPer1K      float32 `protobuf:"fixed32,11,opt,name=cost_per_1k,json=costPer1k,proto3" json:"cost_per_1k,omitempty"`
	AddedAt        time.Time `protobuf:"bytes,12,opt,name=added_at,json=addedAt,proto3,stdtime" json:"added_at,omitempty"`
}

// KnowledgeBaseItem is a knowledge-base item.
type KnowledgeBaseItem struct {
	ItemId    string `protobuf:"bytes,1,opt,name=item_id,json=itemId,proto3" json:"item_id,omitempty"`
	GroupId   string `protobuf:"bytes,2,opt,name=group_id,json=groupId,proto3" json:"group_id,omitempty"`
	Title     string `protobuf:"bytes,3,opt,name=title,proto3" json:"title,omitempty"`
	Content   string `protobuf:"bytes,4,opt,name=content,proto3" json:"content,omitempty"`
	Url       string `protobuf:"bytes,5,opt,name=url,proto3" json:"url,omitempty"`
	MimeType  string `protobuf:"bytes,6,opt,name=mime_type,json=mimeType,proto3" json:"mime_type,omitempty"`
	Hash      string `protobuf:"bytes,7,opt,name=hash,proto3" json:"hash,omitempty"`
	Tokens    int32  `protobuf:"varint,8,opt,name=tokens,proto3" json:"tokens,omitempty"`
	CreatedAt time.Time `protobuf:"bytes,9,opt,name=created_at,json=createdAt,proto3,stdtime" json:"created_at,omitempty"`
	UpdatedAt time.Time `protobuf:"bytes,10,opt,name=updated_at,json=updatedAt,proto3,stdtime" json:"updated_at,omitempty"`
}

// KnowledgeBaseChunk is a chunk within a KB item.
type KnowledgeBaseChunk struct {
	ItemId     string  `protobuf:"bytes,1,opt,name=item_id,json=itemId,proto3" json:"item_id,omitempty"`
	ChunkId    string  `protobuf:"bytes,2,opt,name=chunk_id,json=chunkId,proto3" json:"chunk_id,omitempty"`
	Content    string  `protobuf:"bytes,3,opt,name=content,proto3" json:"content,omitempty"`
	Tokens     int32   `protobuf:"varint,4,opt,name=tokens,proto3" json:"tokens,omitempty"`
}

// KnowledgeBaseGroup is a KB group.
type KnowledgeBaseGroup struct {
	GroupId   string `protobuf:"bytes,1,opt,name=group_id,json=groupId,proto3" json:"group_id,omitempty"`
	Name      string `protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	ItemCount int32  `protobuf:"varint,3,opt,name=item_count,json=itemCount,proto3" json:"item_count,omitempty"`
}

// KnowledgeBaseItemWithMetadata is a KB item with metadata.
type KnowledgeBaseItemWithMetadata struct {
	Item     *KnowledgeBaseItem `protobuf:"bytes,1,opt,name=item,proto3" json:"item,omitempty"`
	Metadata *Metadata          `protobuf:"bytes,2,opt,name=metadata,proto3" json:"metadata,omitempty"`
	Score    float32            `protobuf:"fixed32,3,opt,name=score,proto3" json:"score,omitempty"`
	Group    *KnowledgeBaseGroup `protobuf:"bytes,4,opt,name=group,proto3" json:"group,omitempty"`
}

// KnowledgeBaseScopeItem is a scope item from a KB query.
type KnowledgeBaseScopeItem struct {
	Item        *KnowledgeBaseItem `protobuf:"bytes,1,opt,name=item,proto3" json:"item,omitempty"`
	Chunk       *KnowledgeBaseChunk `protobuf:"bytes,2,opt,name=chunk,proto3" json:"chunk,omitempty"`
	Score       float32            `protobuf:"fixed32,3,opt,name=score,proto3" json:"score,omitempty"`
	Reason      string             `protobuf:"bytes,4,opt,name=reason,proto3" json:"reason,omitempty"`
	Source      string             `protobuf:"bytes,5,opt,name=source,proto3" json:"source,omitempty"`
	WasUsed     bool               `protobuf:"varint,6,opt,name=was_used,json=wasUsed,proto3" json:"was_used,omitempty"`
	UsedAt      time.Time          `protobuf:"bytes,7,opt,name=used_at,json=usedAt,proto3,stdtime" json:"used_at,omitempty"`
}

// SlashCommandInfo is a slash command metadata.
type SlashCommandInfo struct {
	Name        string  `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Description string  `protobuf:"bytes,2,opt,name=description,proto3" json:"description,omitempty"`
	Type        SlashCommandType `protobuf:"varint,3,opt,name=type,proto3,enum=codeium_common_pb.SlashCommandType" json:"type,omitempty"`
	Source      string  `protobuf:"bytes,4,opt,name=source,proto3" json:"source,omitempty"`
	IsEnabled   bool    `protobuf:"varint,5,opt,name=is_enabled,json=isEnabled,proto3" json:"is_enabled,omitempty"`
	Parameters  []string `protobuf:"bytes,6,rep,name=parameters,proto3" json:"parameters,omitempty"`
	Hotkey      string  `protobuf:"bytes,7,opt,name=hotkey,proto3" json:"hotkey,omitempty"`
	PluginId    string  `protobuf:"bytes,8,opt,name=plugin_id,json=pluginId,proto3" json:"plugin_id,omitempty"`
}

// Rule is a single rule entry.
type Rule struct {
	Id      string `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	Content string `protobuf:"bytes,2,opt,name=content,proto3" json:"content,omitempty"`
}

// Guideline is a guideline entry.
type Guideline struct {
	Id          string  `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	Content     string  `protobuf:"bytes,2,opt,name=content,proto3" json:"content,omitempty"`
}

// GuidelineItem is a guideline item (with metadata).
type GuidelineItem struct {
	Guideline *Guideline `protobuf:"bytes,1,opt,name=guideline,proto3" json:"guideline,omitempty"`
	Source    string     `protobuf:"bytes,2,opt,name=source,proto3" json:"source,omitempty"`
	Enabled   bool       `protobuf:"varint,3,opt,name=enabled,proto3" json:"enabled,omitempty"`
	Priority  int32      `protobuf:"varint,4,opt,name=priority,proto3" json:"priority,omitempty"`
}

// PinnedContext is a pinned context block.
type PinnedContext struct {
	Id      string  `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	Title   string  `protobuf:"bytes,2,opt,name=title,proto3" json:"title,omitempty"`
	Content string  `protobuf:"bytes,3,opt,name=content,proto3" json:"content,omitempty"`
	Tokens  int32   `protobuf:"varint,4,opt,name=tokens,proto3" json:"tokens,omitempty"`
	AddedAt time.Time `protobuf:"bytes,5,opt,name=added_at,json=addedAt,proto3,stdtime" json:"added_at,omitempty"`
	Pinned  bool    `protobuf:"varint,6,opt,name=pinned,proto3" json:"pinned,omitempty"`
	Source  string  `protobuf:"bytes,7,opt,name=source,proto3" json:"source,omitempty"`
}

// PinnedContextConfig is the config for pinned context.
type PinnedContextConfig struct {
	MaxItems int32    `protobuf:"varint,1,opt,name=max_items,json=maxItems,proto3" json:"max_items,omitempty"`
	MaxTokens int32   `protobuf:"varint,2,opt,name=max_tokens,json=maxTokens,proto3" json:"max_tokens,omitempty"`
	AutoPin  []string `protobuf:"bytes,3,rep,name=auto_pin,json=autoPin,proto3" json:"auto_pin,omitempty"`
}

// RuleViolation is a rule violation.
type RuleViolation struct {
	RuleId   string `protobuf:"bytes,1,opt,name=rule_id,json=ruleId,proto3" json:"rule_id,omitempty"`
	Reason   string `protobuf:"bytes,2,opt,name=reason,proto3" json:"reason,omitempty"`
	Severity StatusLevel `protobuf:"varint,3,opt,name=severity,proto3,enum=codeium_common_pb.StatusLevel" json:"severity,omitempty"`
	Snippet  string `protobuf:"bytes,4,opt,name=snippet,proto3" json:"snippet,omitempty"`
}

// PromptSectionJudgeConfig is the section judge config.
type PromptSectionJudgeConfig struct {
	Enabled  bool   `protobuf:"varint,1,opt,name=enabled,proto3" json:"enabled,omitempty"`
	Model    string `protobuf:"bytes,2,opt,name=model,proto3" json:"model,omitempty"`
	MaxTokens int32 `protobuf:"varint,3,opt,name=max_tokens,json=maxTokens,proto3" json:"max_tokens,omitempty"`
}

// PlanningModeJudgeConfig is the planning mode judge config.
type PlanningModeJudgeConfig struct {
	Enabled   bool   `protobuf:"varint,1,opt,name=enabled,proto3" json:"enabled,omitempty"`
	Model     string `protobuf:"bytes,2,opt,name=model,proto3" json:"model,omitempty"`
	MaxTokens int32  `protobuf:"varint,3,opt,name=max_tokens,json=maxTokens,proto3" json:"max_tokens,omitempty"`
}

// UserStepJudgeConfig is the per-user step judge config.
type UserStepJudgeConfig struct {
	Enabled   bool   `protobuf:"varint,1,opt,name=enabled,proto3" json:"enabled,omitempty"`
	Model     string `protobuf:"bytes,2,opt,name=model,proto3" json:"model,omitempty"`
	MaxTokens int32  `protobuf:"varint,3,opt,name=max_tokens,json=maxTokens,proto3" json:"max_tokens,omitempty"`
	SampleRate float32 `protobuf:"fixed32,4,opt,name=sample_rate,json=sampleRate,proto3" json:"sample_rate,omitempty"`
}

// MQueryConfig is the model query config.
type MQueryConfig struct {
	Model            string  `protobuf:"bytes,1,opt,name=model,proto3" json:"model,omitempty"`
	MaxTokens        int32   `protobuf:"varint,2,opt,name=max_tokens,json=maxTokens,proto3" json:"max_tokens,omitempty"`
	Temperature      float32 `protobuf:"fixed32,3,opt,name=temperature,proto3" json:"temperature,omitempty"`
	TopP             float32 `protobuf:"fixed32,4,opt,name=top_p,json=topP,proto3" json:"top_p,omitempty"`
	MaxThinkingTokens int32  `protobuf:"varint,5,opt,name=max_thinking_tokens,json=maxThinkingTokens,proto3" json:"max_thinking_tokens,omitempty"`
	Stream           bool    `protobuf:"varint,6,opt,name=stream,proto3" json:"stream,omitempty"`
	TimeoutMs        int32   `protobuf:"varint,7,opt,name=timeout_ms,json=timeoutMs,proto3" json:"timeout_ms,omitempty"`
	Retries          int32   `protobuf:"varint,8,opt,name=retries,proto3" json:"retries,omitempty"`
	FallbackModel    string  `protobuf:"bytes,9,opt,name=fallback_model,json=fallbackModel,proto3" json:"fallback_model,omitempty"`
	StopSequences    []string `protobuf:"bytes,10,rep,name=stop_sequences,json=stopSequences,proto3" json:"stop_sequences,omitempty"`
}

// TrajectoryDescription is a trajectory description.
type TrajectoryDescription struct {
	Type        TrajectoryType `protobuf:"varint,1,opt,name=type,proto3,enum=codeium_common_pb.TrajectoryType" json:"type,omitempty"`
	Description string         `protobuf:"bytes,2,opt,name=description,proto3" json:"description,omitempty"`
	Tags        []string       `protobuf:"bytes,3,rep,name=tags,proto3" json:"tags,omitempty"`
}

// PromptComponents is the prompt components.
type PromptComponents struct {
	SystemPrompt  string `protobuf:"bytes,1,opt,name=system_prompt,json=systemPrompt,proto3" json:"system_prompt,omitempty"`
	UserPrompt    string `protobuf:"bytes,2,opt,name=user_prompt,json=userPrompt,proto3" json:"user_prompt,omitempty"`
	ContextPrompt string `protobuf:"bytes,3,opt,name=context_prompt,json=contextPrompt,proto3" json:"context_prompt,omitempty"`
}

// PromptElementRange is the prompt element range.
type PromptElementRange struct {
	Start   int32 `protobuf:"varint,1,opt,name=start,proto3" json:"start,omitempty"`
	End     int32 `protobuf:"varint,2,opt,name=end,proto3" json:"end,omitempty"`
	Kind    PromptElementKind `protobuf:"varint,3,opt,name=kind,proto3,enum=codeium_common_pb.PromptElementKind" json:"kind,omitempty"`
	TokenCount int32 `protobuf:"varint,4,opt,name=token_count,json=tokenCount,proto3" json:"token_count,omitempty"`
	Info    *PromptElementKindInfo `protobuf:"bytes,5,opt,name=info,proto3" json:"info,omitempty"`
}

// PromptElementKindInfo is the prompt element kind info.
type PromptElementKindInfo struct {
	Kind    PromptElementKind `protobuf:"varint,1,opt,name=kind,proto3,enum=codeium_common_pb.PromptElementKind" json:"kind,omitempty"`
	Source  string  `protobuf:"bytes,2,opt,name=source,proto3" json:"source,omitempty"`
	ExcludedReason PromptElementExclusionReason `protobuf:"varint,3,opt,name=excluded_reason,json=excludedReason,proto3,enum=codeium_common_pb.PromptElementExclusionReason" json:"excluded_reason,omitempty"`
	Score   float32 `protobuf:"fixed32,4,opt,name=score,proto3" json:"score,omitempty"`
	WasUsed bool    `protobuf:"varint,5,opt,name=was_used,json=wasUsed,proto3" json:"was_used,omitempty"`
}

// PromptElementInclusionMetadata is the inclusion metadata.
type PromptElementInclusionMetadata struct {
	Item   *ContextScopeItem `protobuf:"bytes,1,opt,name=item,proto3" json:"item,omitempty"`
	Reason ContextInclusionType `protobuf:"varint,2,opt,name=reason,proto3,enum=codeium_common_pb.ContextInclusionType" json:"reason,omitempty"`
}

// PromptAnnotationRange is the prompt annotation range.
type PromptAnnotationRange struct {
	Start int32 `protobuf:"varint,1,opt,name=start,proto3" json:"start,omitempty"`
	End   int32 `protobuf:"varint,2,opt,name=end,proto3" json:"end,omitempty"`
	Kind  PromptAnnotationKind `protobuf:"varint,3,opt,name=kind,proto3,enum=codeium_common_pb.PromptAnnotationKind" json:"kind,omitempty"`
	Color string `protobuf:"bytes,4,opt,name=color,proto3" json:"color,omitempty"`
}

// PromptStageLatency is the per-stage latency.
type PromptStageLatency struct {
	Stage       string  `protobuf:"bytes,1,opt,name=stage,proto3" json:"stage,omitempty"`
	LatencyMs   int32   `protobuf:"varint,2,opt,name=latency_ms,json=latencyMs,proto3" json:"latency_ms,omitempty"`
}

// IntellisenseSuggestion is an IntelliSense suggestion.
type IntellisenseSuggestion struct {
	Label       string  `protobuf:"bytes,1,opt,name=label,proto3" json:"label,omitempty"`
	Kind        string  `protobuf:"bytes,2,opt,name=kind,proto3" json:"kind,omitempty"`
	Detail      string  `protobuf:"bytes,3,opt,name=detail,proto3" json:"detail,omitempty"`
	InsertText  string  `protobuf:"bytes,4,opt,name=insert_text,json=insertText,proto3" json:"insert_text,omitempty"`
	Range       *Range  `protobuf:"bytes,5,opt,name=range,proto3" json:"range,omitempty"`
	SortKey     string  `protobuf:"bytes,6,opt,name=sort_key,json=sortKey,proto3" json:"sort_key,omitempty"`
	FilterText  string  `protobuf:"bytes,7,opt,name=filter_text,json=filterText,proto3" json:"filter_text,omitempty"`
	Documentation string `protobuf:"bytes,8,opt,name=documentation,proto3" json:"documentation,omitempty"`
	Source      string  `protobuf:"bytes,9,opt,name=source,proto3" json:"source,omitempty"`
}

// BrowserPageMetadata is the page metadata from a browser session.
type BrowserPageMetadata struct {
	Url      string `protobuf:"bytes,1,opt,name=url,proto3" json:"url,omitempty"`
	Title    string `protobuf:"bytes,2,opt,name=title,proto3" json:"title,omitempty"`
	Favicon  string `protobuf:"bytes,3,opt,name=favicon,proto3" json:"favicon,omitempty"`
	Width    int32  `protobuf:"varint,4,opt,name=width,proto3" json:"width,omitempty"`
	Height   int32  `protobuf:"varint,5,opt,name=height,proto3" json:"height,omitempty"`
	Viewport *Viewport `protobuf:"bytes,6,opt,name=viewport,proto3" json:"viewport,omitempty"`
	Html     string `protobuf:"bytes,7,opt,name=html,proto3" json:"html,omitempty"`
	Text     string `protobuf:"bytes,8,opt,name=text,proto3" json:"text,omitempty"`
	Language Language `protobuf:"varint,9,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
}

// Viewport is a viewport.
type Viewport struct {
	Width  int32 `protobuf:"varint,1,opt,name=width,proto3" json:"width,omitempty"`
	Height int32 `protobuf:"varint,2,opt,name=height,proto3" json:"height,omitempty"`
	DevicePixelRatio float32 `protobuf:"fixed32,3,opt,name=device_pixel_ratio,json=devicePixelRatio,proto3" json:"device_pixel_ratio,omitempty"`
}

// BrowserStateSnapshot is a browser state snapshot.
type BrowserStateSnapshot struct {
	Url       string `protobuf:"bytes,1,opt,name=url,proto3" json:"url,omitempty"`
	Title     string `protobuf:"bytes,2,opt,name=title,proto3" json:"title,omitempty"`
	Screenshot []byte `protobuf:"bytes,3,opt,name=screenshot,proto3" json:"screenshot,omitempty"`
}

// BrowserInteraction is a browser interaction.
type BrowserInteraction struct {
	Type    string  `protobuf:"bytes,1,opt,name=type,proto3" json:"type,omitempty"`
	Selector string `protobuf:"bytes,2,opt,name=selector,proto3" json:"selector,omitempty"`
	X       int32   `protobuf:"varint,3,opt,name=x,proto3" json:"x,omitempty"`
	Y       int32   `protobuf:"varint,4,opt,name=y,proto3" json:"y,omitempty"`
	Value   string  `protobuf:"bytes,5,opt,name=value,proto3" json:"value,omitempty"`
}

// BrowserClickInteraction is a click interaction.
type BrowserClickInteraction struct {
	Selector string `protobuf:"bytes,1,opt,name=selector,proto3" json:"selector,omitempty"`
	Button   string `protobuf:"bytes,2,opt,name=button,proto3" json:"button,omitempty"`
	X        int32  `protobuf:"varint,3,opt,name=x,proto3" json:"x,omitempty"`
	Y        int32  `protobuf:"varint,4,opt,name=y,proto3" json:"y,omitempty"`
	Modifiers []string `protobuf:"bytes,5,rep,name=modifiers,proto3" json:"modifiers,omitempty"`
	HoldMs   int32  `protobuf:"varint,6,opt,name=hold_ms,json=holdMs,proto3" json:"hold_ms,omitempty"`
}

// BrowserScrollInteraction is a scroll interaction.
type BrowserScrollInteraction struct {
	Selector string `protobuf:"bytes,1,opt,name=selector,proto3" json:"selector,omitempty"`
	DeltaX   int32  `protobuf:"varint,2,opt,name=delta_x,json=deltaX,proto3" json:"delta_x,omitempty"`
	DeltaY   int32  `protobuf:"varint,3,opt,name=delta_y,json=deltaY,proto3" json:"delta_y,omitempty"`
}

// CaptureFileRequestData is a file capture request.
type CaptureFileRequestData struct {
	Path        string  `protobuf:"bytes,1,opt,name=path,proto3" json:"path,omitempty"`
	Content     string  `protobuf:"bytes,2,opt,name=content,proto3" json:"content,omitempty"`
	Hash        string  `protobuf:"bytes,3,opt,name=hash,proto3" json:"hash,omitempty"`
	Language    Language `protobuf:"varint,4,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	Encoding    string  `protobuf:"bytes,5,opt,name=encoding,proto3" json:"encoding,omitempty"`
	SizeBytes   int32   `protobuf:"varint,6,opt,name=size_bytes,json=sizeBytes,proto3" json:"size_bytes,omitempty"`
	LineCount   int32   `protobuf:"varint,7,opt,name=line_count,json=lineCount,proto3" json:"line_count,omitempty"`
	CharCount   int32   `protobuf:"varint,8,opt,name=char_count,json=charCount,proto3" json:"char_count,omitempty"`
	IsTruncated bool    `protobuf:"varint,9,opt,name=is_truncated,json=isTruncated,proto3" json:"is_truncated,omitempty"`
}

// ArtifactComment is a comment on a code artifact.
type ArtifactComment struct {
	ArtifactUri    string  `protobuf:"bytes,1,opt,name=artifact_uri,json=artifactUri,proto3" json:"artifact_uri,omitempty"`
	Comment        string  `protobuf:"bytes,2,opt,name=comment,proto3" json:"comment,omitempty"`
	Author         string  `protobuf:"bytes,3,opt,name=author,proto3" json:"author,omitempty"`
	CreatedAt      time.Time `protobuf:"bytes,4,opt,name=created_at,json=createdAt,proto3,stdtime" json:"created_at,omitempty"`
	Range          *Range  `protobuf:"bytes,5,opt,name=range,proto3" json:"range,omitempty"`
	ApprovalStatus ArtifactApprovalStatus `protobuf:"varint,6,opt,name=approval_status,json=approvalStatus,proto3,enum=codeium_common_pb.ArtifactApprovalStatus" json:"approval_status,omitempty"`
	FullFile       bool    `protobuf:"varint,7,opt,name=full_file,json=fullFile,proto3" json:"full_file,omitempty"`
}

// ArtifactMetadata is the artifact metadata.
type ArtifactMetadata struct {
	Uri         string  `protobuf:"bytes,1,opt,name=uri,proto3" json:"uri,omitempty"`
	Title       string  `protobuf:"bytes,2,opt,name=title,proto3" json:"title,omitempty"`
	Kind        ArtifactType `protobuf:"varint,3,opt,name=kind,proto3,enum=codeium_common_pb.ArtifactType" json:"kind,omitempty"`
	CreatedAt   time.Time `protobuf:"bytes,4,opt,name=created_at,json=createdAt,proto3,stdtime" json:"created_at,omitempty"`
	UpdatedAt   time.Time `protobuf:"bytes,5,opt,name=updated_at,json=updatedAt,proto3,stdtime" json:"updated_at,omitempty"`
	Author      string  `protobuf:"bytes,6,opt,name=author,proto3" json:"author,omitempty"`
	Tags        []string `protobuf:"bytes,7,rep,name=tags,proto3" json:"tags,omitempty"`
}

// ArtifactSelection is an artifact selection.
type ArtifactSelection struct {
	Uri    string `protobuf:"bytes,1,opt,name=uri,proto3" json:"uri,omitempty"`
	Target *ArtifactSelectionTarget `protobuf:"bytes,2,opt,name=target,proto3" json:"target,omitempty"`
	ReviewMode ArtifactReviewMode `protobuf:"varint,3,opt,name=review_mode,json=reviewMode,proto3,enum=codeium_common_pb.ArtifactReviewMode" json:"review_mode,omitempty"`
}

// ArtifactSelectionTarget is the selection target.
type ArtifactSelectionTarget struct {
	Range *Range `protobuf:"bytes,1,opt,name=range,proto3" json:"range,omitempty"`
}

// ArtifactImageSelection is an artifact image selection.
type ArtifactImageSelection struct {
	ImageUri string `protobuf:"bytes,1,opt,name=image_uri,json=imageUri,proto3" json:"image_uri,omitempty"`
	X        int32  `protobuf:"varint,2,opt,name=x,proto3" json:"x,omitempty"`
	Y        int32  `protobuf:"varint,3,opt,name=y,proto3" json:"y,omitempty"`
	Width    int32  `protobuf:"varint,4,opt,name=width,proto3" json:"width,omitempty"`
	Height   int32  `protobuf:"varint,5,opt,name=height,proto3" json:"height,omitempty"`
	// Field 6 reserved (was full_file in some proto revisions)
	_ [4]byte `protobuf:"bytes,6,opt,name=full_file,json=fullFile,proto3" json:"-"`
}

// ArtifactFullFileTarget is the full-file artifact target.
type ArtifactFullFileTarget struct {
	Path string `protobuf:"bytes,1,opt,name=path,proto3" json:"path,omitempty"`
	FullFile bool `protobuf:"varint,2,opt,name=full_file,json=fullFile,proto3" json:"full_file,omitempty"`
}

// CodeDiagnostic is a single lint diagnostic.
type CodeDiagnostic struct {
	Range    *Range  `protobuf:"bytes,1,opt,name=range,proto3" json:"range,omitempty"`
	Severity StatusLevel `protobuf:"varint,2,opt,name=severity,proto3,enum=codeium_common_pb.StatusLevel" json:"severity,omitempty"`
	Code     string  `protobuf:"bytes,3,opt,name=code,proto3" json:"code,omitempty"`
	Source   string  `protobuf:"bytes,4,opt,name=source,proto3" json:"source,omitempty"`
	Message  string  `protobuf:"bytes,5,opt,name=message,proto3" json:"message,omitempty"`
	RuleId   string  `protobuf:"bytes,6,opt,name=rule_id,json=ruleId,proto3" json:"rule_id,omitempty"`
	Fix      string  `protobuf:"bytes,7,opt,name=fix,proto3" json:"fix,omitempty"`
	FileUri  string  `protobuf:"bytes,8,opt,name=file_uri,json=fileUri,proto3" json:"file_uri,omitempty"`
}

// CodeContextItem is a code context item.
type CodeContextItem struct {
	Uri      string  `protobuf:"bytes,1,opt,name=uri,proto3" json:"uri,omitempty"`
	Range    *Range  `protobuf:"bytes,2,opt,name=range,proto3" json:"range,omitempty"`
	Source   CodeContextSource `protobuf:"varint,3,opt,name=source,proto3,enum=codeium_common_pb.CodeContextSource" json:"source,omitempty"`
	Type     CodeContextType   `protobuf:"varint,4,opt,name=type,proto3,enum=codeium_common_pb.CodeContextType" json:"type,omitempty"`
	Text     string  `protobuf:"bytes,5,opt,name=text,proto3" json:"text,omitempty"`
	Language Language `protobuf:"varint,6,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	Hash     string  `protobuf:"bytes,7,opt,name=hash,proto3" json:"hash,omitempty"`
	Tokens   int32   `protobuf:"varint,8,opt,name=tokens,proto3" json:"tokens,omitempty"`
	Score    float32 `protobuf:"fixed32,9,opt,name=score,proto3" json:"score,omitempty"`
	Reason   ContextInclusionType `protobuf:"varint,10,opt,name=reason,proto3,enum=codeium_common_pb.ContextInclusionType" json:"reason,omitempty"`
	FunctionName string `protobuf:"bytes,11,opt,name=function_name,json=functionName,proto3" json:"function_name,omitempty"`
	ClassName    string `protobuf:"bytes,12,opt,name=class_name,json=className,proto3" json:"class_name,omitempty"`
	LineCount    int32  `protobuf:"varint,13,opt,name=line_count,json=lineCount,proto3" json:"line_count,omitempty"`
	Metadata     *Metadata `protobuf:"bytes,14,opt,name=metadata,proto3" json:"metadata,omitempty"`
}

// CompletionConfiguration is the per-model completion config.
type CompletionConfiguration struct {
	Model              string  `protobuf:"bytes,1,opt,name=model,proto3" json:"model,omitempty"`
	Temperature        float32 `protobuf:"fixed32,2,opt,name=temperature,proto3" json:"temperature,omitempty"`
	TopP               float32 `protobuf:"fixed32,3,opt,name=top_p,json=topP,proto3" json:"top_p,omitempty"`
	MaxTokens          int32   `protobuf:"varint,4,opt,name=max_tokens,json=maxTokens,proto3" json:"max_tokens,omitempty"`
	StopSequences      []string `protobuf:"bytes,5,rep,name=stop_sequences,json=stopSequences,proto3" json:"stop_sequences,omitempty"`
	PresencePenalty    float32 `protobuf:"fixed32,6,opt,name=presence_penalty,json=presencePenalty,proto3" json:"presence_penalty,omitempty"`
	FrequencyPenalty   float32 `protobuf:"fixed32,7,opt,name=frequency_penalty,json=frequencyPenalty,proto3" json:"frequency_penalty,omitempty"`
	Speed              AutocompleteSpeed `protobuf:"varint,8,opt,name=speed,proto3,enum=codeium_common_pb.AutocompleteSpeed" json:"speed,omitempty"`
	UseCache           bool    `protobuf:"varint,9,opt,name=use_cache,json=useCache,proto3" json:"use_cache,omitempty"`
	UseLocal           bool    `protobuf:"varint,10,opt,name=use_local,json=useLocal,proto3" json:"use_local,omitempty"`
	UseNucleusSampling bool    `protobuf:"varint,11,opt,name=use_nucleus_sampling,json=useNucleusSampling,proto3" json:"use_nucleus_sampling,omitempty"`
	StopAfterN         int32   `protobuf:"varint,12,opt,name=stop_after_n,json=stopAfterN,proto3" json:"stop_after_n,omitempty"`
	UseFim             bool    `protobuf:"varint,13,opt,name=use_fim,json=useFim,proto3" json:"use_fim,omitempty"`
	MaxLines           int32   `protobuf:"varint,14,opt,name=max_lines,json=maxLines,proto3" json:"max_lines,omitempty"`
	IncludeImports     bool    `protobuf:"varint,15,opt,name=include_imports,json=includeImports,proto3" json:"include_imports,omitempty"`
	UseSuffixContext   bool    `protobuf:"varint,16,opt,name=use_suffix_context,json=useSuffixContext,proto3" json:"use_suffix_context,omitempty"`
	UsePrefixContext   bool    `protobuf:"varint,17,opt,name=use_prefix_context,json=usePrefixContext,proto3" json:"use_prefix_context,omitempty"`
	UseCrossFileContext bool   `protobuf:"varint,18,opt,name=use_cross_file_context,json=useCrossFileContext,proto3" json:"use_cross_file_context,omitempty"`
	StreamTimeoutMs    int32   `protobuf:"varint,19,opt,name=stream_timeout_ms,json=streamTimeoutMs,proto3" json:"stream_timeout_ms,omitempty"`
}

// CompletionLatencyInfo is the completion latency info.
type CompletionLatencyInfo struct {
	ModelLatencyMs    int32 `protobuf:"varint,1,opt,name=model_latency_ms,json=modelLatencyMs,proto3" json:"model_latency_ms,omitempty"`
	NetworkLatencyMs  int32 `protobuf:"varint,2,opt,name=network_latency_ms,json=networkLatencyMs,proto3" json:"network_latency_ms,omitempty"`
	PreprocessLatencyMs int32 `protobuf:"varint,3,opt,name=preprocess_latency_ms,json=preprocessLatencyMs,proto3" json:"preprocess_latency_ms,omitempty"`
	PostprocessLatencyMs int32 `protobuf:"varint,4,opt,name=postprocess_latency_ms,json=postprocessLatencyMs,proto3" json:"postprocess_latency_ms,omitempty"`
	TotalLatencyMs    int32 `protobuf:"varint,5,opt,name=total_latency_ms,json=totalLatencyMs,proto3" json:"total_latency_ms,omitempty"`
	TimeToFirstTokenMs int32 `protobuf:"varint,6,opt,name=time_to_first_token_ms,json=timeToFirstTokenMs,proto3" json:"time_to_first_token_ms,omitempty"`
	TtftMs            int32 `protobuf:"varint,7,opt,name=ttft_ms,json=ttftMs,proto3" json:"ttft_ms,omitempty"`
	WasCached         bool  `protobuf:"varint,8,opt,name=was_cached,json=wasCached,proto3" json:"was_cached,omitempty"`
	CacheHitType      string `protobuf:"bytes,9,opt,name=cache_hit_type,json=cacheHitType,proto3" json:"cache_hit_type,omitempty"`
}

// CompletionProfile is the per-completion profile.
type CompletionProfile struct {
	CompletionId   string `protobuf:"bytes,1,opt,name=completion_id,json=completionId,proto3" json:"completion_id,omitempty"`
	Model          string `protobuf:"bytes,2,opt,name=model,proto3" json:"model,omitempty"`
	Language       Language `protobuf:"varint,3,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	Source         CompletionSource `protobuf:"varint,4,opt,name=source,proto3,enum=codeium_common_pb.CompletionSource" json:"source,omitempty"`
	Type           CompletionType   `protobuf:"varint,5,opt,name=type,proto3,enum=codeium_common_pb.CompletionType" json:"type,omitempty"`
	Text           string  `protobuf:"bytes,6,opt,name=text,proto3" json:"text,omitempty"`
}

// CompletionStatistics is the completion stats.
type CompletionStatistics struct {
	TotalCount      int64   `protobuf:"varint,1,opt,name=total_count,json=totalCount,proto3" json:"total_count,omitempty"`
	AcceptCount     int64   `protobuf:"varint,2,opt,name=accept_count,json=acceptCount,proto3" json:"accept_count,omitempty"`
	PartialAcceptCount int64 `protobuf:"varint,3,opt,name=partial_accept_count,json=partialAcceptCount,proto3" json:"partial_accept_count,omitempty"`
	RejectCount     int64   `protobuf:"varint,4,opt,name=reject_count,json=rejectCount,proto3" json:"reject_count,omitempty"`
	AcceptRate      float32 `protobuf:"fixed32,5,opt,name=accept_rate,json=acceptRate,proto3" json:"accept_rate,omitempty"`
	AvgLatencyMs    int32   `protobuf:"varint,6,opt,name=avg_latency_ms,json=avgLatencyMs,proto3" json:"avg_latency_ms,omitempty"`
	TotalCostUsd    float32 `protobuf:"fixed32,7,opt,name=total_cost_usd,json=totalCostUsd,proto3" json:"total_cost_usd,omitempty"`
}

// SingleModelCompletionProfile is a per-model completion profile.
type SingleModelCompletionProfile struct {
	Model         string  `protobuf:"bytes,1,opt,name=model,proto3" json:"model,omitempty"`
	TotalCount    int64   `protobuf:"varint,2,opt,name=total_count,json=totalCount,proto3" json:"total_count,omitempty"`
	AcceptCount   int64   `protobuf:"varint,3,opt,name=accept_count,json=acceptCount,proto3" json:"accept_count,omitempty"`
	RejectCount   int64   `protobuf:"varint,4,opt,name=reject_count,json=rejectCount,proto3" json:"reject_count,omitempty"`
	AcceptRate    float32 `protobuf:"fixed32,5,opt,name=accept_rate,json=acceptRate,proto3" json:"accept_rate,omitempty"`
	AvgLatencyMs  int32   `protobuf:"varint,6,opt,name=avg_latency_ms,json=avgLatencyMs,proto3" json:"avg_latency_ms,omitempty"`
	CostUsd       float32 `protobuf:"fixed32,7,opt,name=cost_usd,json=costUsd,proto3" json:"cost_usd,omitempty"`
	Tokens        int64   `protobuf:"varint,8,opt,name=tokens,proto3" json:"tokens,omitempty"`
	FirstTokenLatencyMs int32 `protobuf:"varint,9,opt,name=first_token_latency_ms,json=firstTokenLatencyMs,proto3" json:"first_token_latency_ms,omitempty"`
	P50LatencyMs  int32   `protobuf:"varint,10,opt,name=p50_latency_ms,json=p50LatencyMs,proto3" json:"p50_latency_ms,omitempty"`
}

// CompletionWithLatencyInfo is a completion + its latency info.
type CompletionWithLatencyInfo struct {
	Completion *Completion          `protobuf:"bytes,1,opt,name=completion,proto3" json:"completion,omitempty"`
	Latency    *CompletionLatencyInfo `protobuf:"bytes,2,opt,name=latency,proto3" json:"latency,omitempty"`
}

// CompletionExample is a completion example (few-shot).
type CompletionExample struct {
	Prefix    string `protobuf:"bytes,1,opt,name=prefix,proto3" json:"prefix,omitempty"`
	Suffix    string `protobuf:"bytes,2,opt,name=suffix,proto3" json:"suffix,omitempty"`
	Completion string `protobuf:"bytes,3,opt,name=completion,proto3" json:"completion,omitempty"`
	Language  Language `protobuf:"varint,4,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	Score     float32 `protobuf:"fixed32,5,opt,name=score,proto3" json:"score,omitempty"`
	Source    CompletionSource `protobuf:"varint,6,opt,name=source,proto3,enum=codeium_common_pb.CompletionSource" json:"source,omitempty"`
}

// CompletionExampleWithMetadata is a completion example with metadata.
type CompletionExampleWithMetadata struct {
	Example  *CompletionExample `protobuf:"bytes,1,opt,name=example,proto3" json:"example,omitempty"`
	Metadata *Metadata          `protobuf:"bytes,2,opt,name=metadata,proto3" json:"metadata,omitempty"`
	WasUsed  bool               `protobuf:"varint,3,opt,name=was_used,json=wasUsed,proto3" json:"was_used,omitempty"`
}

// CompletionDeltaMap is a (key, delta) map of completion deltas.
type CompletionDeltaMap struct {
	Deltas   []*CompletionDelta `protobuf:"bytes,1,rep,name=deltas,proto3" json:"deltas,omitempty"`
	TotalTokens int32           `protobuf:"varint,2,opt,name=total_tokens,json=totalTokens,proto3" json:"total_tokens,omitempty"`
	Source     CompletionSource `protobuf:"varint,3,opt,name=source,proto3,enum=codeium_common_pb.CompletionSource" json:"source,omitempty"`
}

// StreamingCompletionMap is a (key, completion) map.
type StreamingCompletionMap struct {
	Entries map[string]*StreamingCompletion `protobuf:"bytes,1,rep,name=entries,proto3" json:"entries,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
}

// StreamingCompletionInfo is the streaming completion metadata.
type StreamingCompletionInfo struct {
	Id           string  `protobuf:"bytes,1,opt,name=id,proto3" json:"id,omitempty"`
	Model        string  `protobuf:"bytes,2,opt,name=model,proto3" json:"model,omitempty"`
	StartedAt    time.Time `protobuf:"bytes,3,opt,name=started_at,json=startedAt,proto3,stdtime" json:"started_at,omitempty"`
	Sequence     int64   `protobuf:"varint,4,opt,name=sequence,proto3" json:"sequence,omitempty"`
	Tokens       int64   `protobuf:"varint,5,opt,name=tokens,proto3" json:"tokens,omitempty"`
	FinishReason string  `protobuf:"bytes,6,opt,name=finish_reason,json=finishReason,proto3" json:"finish_reason,omitempty"`
	TotalChunks  int32   `protobuf:"varint,7,opt,name=total_chunks,json=totalChunks,proto3" json:"total_chunks,omitempty"`
	WasCancelled bool    `protobuf:"varint,8,opt,name=was_cancelled,json=wasCancelled,proto3" json:"was_cancelled,omitempty"`
	WasCached    bool    `protobuf:"varint,9,opt,name=was_cached,json=wasCached,proto3" json:"was_cached,omitempty"`
	CostUsd      float32 `protobuf:"fixed32,10,opt,name=cost_usd,json=costUsd,proto3" json:"cost_usd,omitempty"`
	FirstTokenLatencyMs int32 `protobuf:"varint,11,opt,name=first_token_latency_ms,json=firstTokenLatencyMs,proto3" json:"first_token_latency_ms,omitempty"`
}

// PackedStreamingCompletionMaps is a packed version of the streaming
// completion map (for telemetry compression).
type PackedStreamingCompletionMaps struct {
	Keys   []string                `protobuf:"bytes,1,rep,name=keys,proto3" json:"keys,omitempty"`
	Values []*StreamingCompletionMap `protobuf:"bytes,2,rep,name=values,proto3" json:"values,omitempty"`
}

// StreamingEvalSuffixInfo is the streaming eval suffix info.
type StreamingEvalSuffixInfo struct {
	HasSuffix bool   `protobuf:"varint,1,opt,name=has_suffix,json=hasSuffix,proto3" json:"has_suffix,omitempty"`
	Suffix    string `protobuf:"bytes,2,opt,name=suffix,proto3" json:"suffix,omitempty"`
}

// PinnedContext is recovered as a proto message (PinnedContext is also
// declared in this file).
type _PinnedContext = PinnedContext

// AntigravityProject is an Antigravity project (vs. deployment).
type AntigravityProject struct {
	AntigravityProjectId string `protobuf:"bytes,1,opt,name=antigravity_project_id,json=antigravityProjectId,proto3" json:"antigravity_project_id,omitempty"`
	AuthUid              string `protobuf:"bytes,2,opt,name=auth_uid,json=authUid,proto3" json:"auth_uid,omitempty"`
	ClaimedAt            time.Time `protobuf:"bytes,3,opt,name=claimed_at,json=claimedAt,proto3,stdtime" json:"claimed_at,omitempty"`
	CreatedAt            time.Time `protobuf:"bytes,4,opt,name=created_at,json=createdAt,proto3,stdtime" json:"created_at,omitempty"`
	DeploymentProvider   DeploymentProvider `protobuf:"varint,5,opt,name=deployment_provider,json=deploymentProvider,proto3,enum=codeium_common_pb.DeploymentProvider" json:"deployment_provider,omitempty"`
	DeprovisionedAt      time.Time `protobuf:"bytes,6,opt,name=deprovisioned_at,json=deprovisionedAt,proto3,stdtime" json:"deprovisioned_at,omitempty"`
	Domain               string `protobuf:"bytes,7,opt,name=domain,proto3" json:"domain,omitempty"`
	ExpiresAt            time.Time `protobuf:"bytes,8,opt,name=expires_at,json=expiresAt,proto3,stdtime" json:"expires_at,omitempty"`
	ProjectName          string `protobuf:"bytes,9,opt,name=project_name,json=projectName,proto3" json:"project_name,omitempty"`
	ProjectUrl           string `protobuf:"bytes,10,opt,name=project_url,json=projectUrl,proto3" json:"project_url,omitempty"`
	ProviderProjectId    string `protobuf:"bytes,11,opt,name=provider_project_id,json=providerProjectId,proto3" json:"provider_project_id,omitempty"`
	ProviderTeamId        string `protobuf:"bytes,12,opt,name=provider_team_id,json=providerTeamId,proto3" json:"provider_team_id,omitempty"`
	SubdomainName         string `protobuf:"bytes,13,opt,name=subdomain_name,json=subdomainName,proto3" json:"subdomain_name,omitempty"`
	UpdatedAt             time.Time `protobuf:"bytes,14,opt,name=updated_at,json=updatedAt,proto3,stdtime" json:"updated_at,omitempty"`
}

// WebAppDeploymentConfig is the web app deployment config.
type WebAppDeploymentConfig struct {
	Provider       DeploymentProvider `protobuf:"varint,1,opt,name=provider,proto3,enum=codeium_common_pb.DeploymentProvider" json:"provider,omitempty"`
	Repository     string  `protobuf:"bytes,2,opt,name=repository,proto3" json:"repository,omitempty"`
	Branch         string  `protobuf:"bytes,3,opt,name=branch,proto3" json:"branch,omitempty"`
	BuildCommand   string  `protobuf:"bytes,4,opt,name=build_command,json=buildCommand,proto3" json:"build_command,omitempty"`
	OutputDir      string  `protobuf:"bytes,5,opt,name=output_dir,json=outputDir,proto3" json:"output_dir,omitempty"`
}

// McpServerConfig is the MCP server config.
type McpServerConfig struct {
	ServerId   string `protobuf:"bytes,1,opt,name=server_id,json=serverId,proto3" json:"server_id,omitempty"`
	Name       string `protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	Enabled    bool   `protobuf:"varint,3,opt,name=enabled,proto3" json:"enabled,omitempty"`
	AutoStart  bool   `protobuf:"varint,4,opt,name=auto_start,json=autoStart,proto3" json:"auto_start,omitempty"`
	// McpLocalServer or McpRemoteServer, depending on transport
	Local  *McpLocalServer  `protobuf:"bytes,5,opt,name=local,proto3" json:"local,omitempty"`
	Remote *McpRemoteServer `protobuf:"bytes,6,opt,name=remote,proto3" json:"remote,omitempty"`
	Headers map[string]string `protobuf:"bytes,7,rep,name=headers,proto3" json:"headers,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
	Env    map[string]string `protobuf:"bytes,8,rep,name=env,proto3" json:"env,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
}

// McpLocalServer is a local MCP server.
type McpLocalServer struct {
	Command string `protobuf:"bytes,1,opt,name=command,proto3" json:"command,omitempty"`
	Args    []string `protobuf:"bytes,2,rep,name=args,proto3" json:"args,omitempty"`
	Cwd     string `protobuf:"bytes,3,opt,name=cwd,proto3" json:"cwd,omitempty"`
}

// McpRemoteServer is a remote MCP server.
type McpRemoteServer struct {
	Url     string `protobuf:"bytes,1,opt,name=url,proto3" json:"url,omitempty"`
	AuthType string `protobuf:"bytes,2,opt,name=auth_type,json=authType,proto3" json:"auth_type,omitempty"`
	Token   string `protobuf:"bytes,3,opt,name=token,proto3" json:"token,omitempty"`
}

// McpServerTemplate is a MCP server template (preset config).
type McpServerTemplate struct {
	TemplateId string `protobuf:"bytes,1,opt,name=template_id,json=templateId,proto3" json:"template_id,omitempty"`
	Name       string `protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	Description string `protobuf:"bytes,3,opt,name=description,proto3" json:"description,omitempty"`
	Command    *McpServerCommand `protobuf:"bytes,4,opt,name=command,proto3" json:"command,omitempty"`
	Config     *McpServerConfig `protobuf:"bytes,5,opt,name=config,proto3" json:"config,omitempty"`
}

// McpServerCommand is a single MCP server command.
type McpServerCommand struct {
	Command string `protobuf:"bytes,1,opt,name=command,proto3" json:"command,omitempty"`
	Args    []string `protobuf:"bytes,2,rep,name=args,proto3" json:"args,omitempty"`
}

// McpCommandTemplate is a parameterized MCP command template.
type McpCommandTemplate struct {
	TemplateId string `protobuf:"bytes,1,opt,name=template_id,json=templateId,proto3" json:"template_id,omitempty"`
	Template   string `protobuf:"bytes,2,opt,name=template,proto3" json:"template,omitempty"`
	Variables  []*McpCommandVariable `protobuf:"bytes,3,rep,name=variables,proto3" json:"variables,omitempty"`
}

// McpCommandVariable is a single variable in a MCP command template.
type McpCommandVariable struct {
	Name        string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Description string `protobuf:"bytes,2,opt,name=description,proto3" json:"description,omitempty"`
	Default     string `protobuf:"bytes,3,opt,name=default,proto3" json:"default,omitempty"`
	Type        string `protobuf:"bytes,4,opt,name=type,proto3" json:"type,omitempty"`
}

// McpResourceItem is a MCP resource item.
type McpResourceItem struct {
	Uri         string `protobuf:"bytes,1,opt,name=uri,proto3" json:"uri,omitempty"`
	Name        string `protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	Description string `protobuf:"bytes,3,opt,name=description,proto3" json:"description,omitempty"`
	MimeType    string `protobuf:"bytes,4,opt,name=mime_type,json=mimeType,proto3" json:"mime_type,omitempty"`
	Content     *McpResourceContent `protobuf:"bytes,5,opt,name=content,proto3" json:"content,omitempty"`
}

// McpResourceContent is the content of a MCP resource.
type McpResourceContent struct {
	Text  string `protobuf:"bytes,1,opt,name=text,proto3" json:"text,omitempty"`
	Data  []byte `protobuf:"bytes,2,opt,name=data,proto3" json:"data,omitempty"`
	MimeType string `protobuf:"bytes,3,opt,name=mime_type,json=mimeType,proto3" json:"mime_type,omitempty"`
	Uri   string `protobuf:"bytes,4,opt,name=uri,proto3" json:"uri,omitempty"`
	Blob  []byte `protobuf:"bytes,5,opt,name=blob,proto3" json:"blob,omitempty"`
}

// McpPromptMessage is a MCP prompt message.
type McpPromptMessage struct {
	Role    string `protobuf:"bytes,1,opt,name=role,proto3" json:"role,omitempty"`
	Content string `protobuf:"bytes,2,opt,name=content,proto3" json:"content,omitempty"`
	Name    string `protobuf:"bytes,3,opt,name=name,proto3" json:"name,omitempty"`
	MimeType string `protobuf:"bytes,4,opt,name=mime_type,json=mimeType,proto3" json:"mime_type,omitempty"`
	Type    string `protobuf:"bytes,5,opt,name=type,proto3" json:"type,omitempty"`
}

// McpPromptArgument is a MCP prompt argument.
type McpPromptArgument struct {
	Name        string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Description string `protobuf:"bytes,2,opt,name=description,proto3" json:"description,omitempty"`
	Required    bool   `protobuf:"varint,3,opt,name=required,proto3" json:"required,omitempty"`
}

// McpPromptScopeItem is a MCP prompt scope item.
type McpPromptScopeItem struct {
	PromptName string `protobuf:"bytes,1,opt,name=prompt_name,json=promptName,proto3" json:"prompt_name,omitempty"`
	ServerId   string `protobuf:"bytes,2,opt,name=server_id,json=serverId,proto3" json:"server_id,omitempty"`
	Arguments  []*McpPromptArgument `protobuf:"bytes,3,rep,name=arguments,proto3" json:"arguments,omitempty"`
	Messages   []*McpPromptMessage `protobuf:"bytes,4,rep,name=messages,proto3" json:"messages,omitempty"`
	WasUsed    bool   `protobuf:"varint,5,opt,name=was_used,json=wasUsed,proto3" json:"was_used,omitempty"`
	Source     string `protobuf:"bytes,6,opt,name=source,proto3" json:"source,omitempty"`
}

// PluginRef is a plugin reference.
type PluginRef struct {
	PluginId string `protobuf:"bytes,1,opt,name=plugin_id,json=pluginId,proto3" json:"plugin_id,omitempty"`
	Version  string `protobuf:"bytes,2,opt,name=version,proto3" json:"version,omitempty"`
	Source   string `protobuf:"bytes,3,opt,name=source,proto3" json:"source,omitempty"`
}

// PluginScopeItem is a plugin scope item.
type PluginScopeItem struct {
	PluginId string `protobuf:"bytes,1,opt,name=plugin_id,json=pluginId,proto3" json:"plugin_id,omitempty"`
	Name     string `protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	Version  string `protobuf:"bytes,3,opt,name=version,proto3" json:"version,omitempty"`
	Enabled  bool   `protobuf:"varint,4,opt,name=enabled,proto3" json:"enabled,omitempty"`
}

// BrowserCodeBlockScopeItem is a code block scope item.
type BrowserCodeBlockScopeItem struct {
	Language  Language `protobuf:"varint,1,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	Text      string  `protobuf:"bytes,2,opt,name=text,proto3" json:"text,omitempty"`
	Source    string  `protobuf:"bytes,3,opt,name=source,proto3" json:"source,omitempty"`
	Range     *Range  `protobuf:"bytes,4,opt,name=range,proto3" json:"range,omitempty"`
	Hash      string  `protobuf:"bytes,5,opt,name=hash,proto3" json:"hash,omitempty"`
	Tokens    int32   `protobuf:"varint,6,opt,name=tokens,proto3" json:"tokens,omitempty"`
}

// BrowserPageScopeItem is a page scope item.
type BrowserPageScopeItem struct {
	Url      string `protobuf:"bytes,1,opt,name=url,proto3" json:"url,omitempty"`
	Title    string `protobuf:"bytes,2,opt,name=title,proto3" json:"title,omitempty"`
	Text     string `protobuf:"bytes,3,opt,name=text,proto3" json:"text,omitempty"`
	Hash     string `protobuf:"bytes,4,opt,name=hash,proto3" json:"hash,omitempty"`
}

// BrowserTextScopeItem is a text scope item.
type BrowserTextScopeItem struct {
	Text  string `protobuf:"bytes,1,opt,name=text,proto3" json:"text,omitempty"`
	Hash  string `protobuf:"bytes,2,opt,name=hash,proto3" json:"hash,omitempty"`
	Range *Range `protobuf:"bytes,3,opt,name=range,proto3" json:"range,omitempty"`
}

// ConversationScopeItem is a conversation scope item.
type ConversationScopeItem struct {
	ConversationId string `protobuf:"bytes,1,opt,name=conversation_id,json=conversationId,proto3" json:"conversation_id,omitempty"`
	Title          string `protobuf:"bytes,2,opt,name=title,proto3" json:"title,omitempty"`
	Summary        string `protobuf:"bytes,3,opt,name=summary,proto3" json:"summary,omitempty"`
	WasUsed        bool   `protobuf:"varint,4,opt,name=was_used,json=wasUsed,proto3" json:"was_used,omitempty"`
}

// LinkScopeItem is a link scope item.
type LinkScopeItem struct {
	Url     string `protobuf:"bytes,1,opt,name=url,proto3" json:"url,omitempty"`
	Title   string `protobuf:"bytes,2,opt,name=title,proto3" json:"title,omitempty"`
	Snippet string `protobuf:"bytes,3,opt,name=snippet,proto3" json:"snippet,omitempty"`
	Source  string `protobuf:"bytes,4,opt,name=source,proto3" json:"source,omitempty"`
	WasUsed bool   `protobuf:"varint,5,opt,name=was_used,json=wasUsed,proto3" json:"was_used,omitempty"`
}

// PathScopeItem is a path scope item.
type PathScopeItem struct {
	Path     string `protobuf:"bytes,1,opt,name=path,proto3" json:"path,omitempty"`
	Range    *Range `protobuf:"bytes,2,opt,name=range,proto3" json:"range,omitempty"`
	Source   string `protobuf:"bytes,3,opt,name=source,proto3" json:"source,omitempty"`
	WasUsed  bool   `protobuf:"varint,4,opt,name=was_used,json=wasUsed,proto3" json:"was_used,omitempty"`
	Reason   string `protobuf:"bytes,5,opt,name=reason,proto3" json:"reason,omitempty"`
	Score    float32 `protobuf:"fixed32,6,opt,name=score,proto3" json:"score,omitempty"`
}

// RepositoryScopeItem is a repository scope item.
type RepositoryScopeItem struct {
	Repository *Repository `protobuf:"bytes,1,opt,name=repository,proto3" json:"repository,omitempty"`
}

// RepositoryPathScopeItem is a path within a repository scope.
type RepositoryPathScopeItem struct {
	Path   *RepositoryPath `protobuf:"bytes,1,opt,name=path,proto3" json:"path,omitempty"`
	Reason string          `protobuf:"bytes,2,opt,name=reason,proto3" json:"reason,omitempty"`
	WasUsed bool            `protobuf:"varint,3,opt,name=was_used,json=wasUsed,proto3" json:"was_used,omitempty"`
}

// RecipeScopeItem is a recipe scope item.
type RecipeScopeItem struct {
	RecipeId   string `protobuf:"bytes,1,opt,name=recipe_id,json=recipeId,proto3" json:"recipe_id,omitempty"`
	Name       string `protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	Content    string `protobuf:"bytes,3,opt,name=content,proto3" json:"content,omitempty"`
	WasUsed    bool   `protobuf:"varint,4,opt,name=was_used,json=wasUsed,proto3" json:"was_used,omitempty"`
	Source     string `protobuf:"bytes,5,opt,name=source,proto3" json:"source,omitempty"`
}

// RuleScopeItem is a rule scope item.
type RuleScopeItem struct {
	RuleId   string `protobuf:"bytes,1,opt,name=rule_id,json=ruleId,proto3" json:"rule_id,omitempty"`
	Content  string `protobuf:"bytes,2,opt,name=content,proto3" json:"content,omitempty"`
	WasUsed  bool   `protobuf:"varint,3,opt,name=was_used,json=wasUsed,proto3" json:"was_used,omitempty"`
}

// TerminalScopeItem is a terminal scope item.
type TerminalScopeItem struct {
	Command      string `protobuf:"bytes,1,opt,name=command,proto3" json:"command,omitempty"`
	Output       string `protobuf:"bytes,2,opt,name=output,proto3" json:"output,omitempty"`
	ExitCode     int32  `protobuf:"varint,3,opt,name=exit_code,json=exitCode,proto3" json:"exit_code,omitempty"`
	WasUsed      bool   `protobuf:"varint,4,opt,name=was_used,json=wasUsed,proto3" json:"was_used,omitempty"`
}

// SlashCommandScopeItem is a slash-command scope item.
type SlashCommandScopeItem struct {
	Command  *SlashCommandInfo `protobuf:"bytes,1,opt,name=command,proto3" json:"command,omitempty"`
	WasUsed  bool   `protobuf:"varint,2,opt,name=was_used,json=wasUsed,proto3" json:"was_used,omitempty"`
}

// UserActivityScopeItem is a user-activity scope item.
type UserActivityScopeItem struct {
	ActivityType string `protobuf:"bytes,1,opt,name=activity_type,json=activityType,proto3" json:"activity_type,omitempty"`
	Timestamp    time.Time `protobuf:"bytes,2,opt,name=timestamp,proto3,stdtime" json:"timestamp,omitempty"`
	WasUsed      bool   `protobuf:"varint,3,opt,name=was_used,json=wasUsed,proto3" json:"was_used,omitempty"`
}

// DOMNode is a DOM tree node.
type DOMNode struct {
	Element *DOMNode_ElementNode `protobuf:"bytes,1,opt,name=element,proto3" json:"element,omitempty"`
	Text    *DOMNode_TextNode    `protobuf:"bytes,2,opt,name=text,proto3" json:"text,omitempty"`
}

// DOMNode_ElementNode is the element node variant.
type DOMNode_ElementNode struct {
	TagName    string      `protobuf:"bytes,1,opt,name=tag_name,json=tagName,proto3" json:"tag_name,omitempty"`
	Attributes []*Metadata  `protobuf:"bytes,2,rep,name=attributes,proto3" json:"attributes,omitempty"`
	Children   []*DOMNode  `protobuf:"bytes,3,rep,name=children,proto3" json:"children,omitempty"`
	InnerText  string      `protobuf:"bytes,4,opt,name=inner_text,json=innerText,proto3" json:"inner_text,omitempty"`
	InnerHtml  string      `protobuf:"bytes,5,opt,name=inner_html,json=innerHtml,proto3" json:"inner_html,omitempty"`
	XPath      string      `protobuf:"bytes,6,opt,name=x_path,json=xPath,proto3" json:"x_path,omitempty"`
	CssPath    string      `protobuf:"bytes,7,opt,name=css_path,json=cssPath,proto3" json:"css_path,omitempty"`
	IsVisible  bool        `protobuf:"varint,8,opt,name=is_visible,json=isVisible,proto3" json:"is_visible,omitempty"`
	IsClickable bool       `protobuf:"varint,9,opt,name=is_clickable,json=isClickable,proto3" json:"is_clickable,omitempty"`
}

// DOMNode_TextNode is the text node variant.
type DOMNode_TextNode struct {
	Text string `protobuf:"bytes,1,opt,name=text,proto3" json:"text,omitempty"`
}

// DOMTree is a DOM tree.
type DOMTree struct {
	Root  *DOMNode `protobuf:"bytes,1,opt,name=root,proto3" json:"root,omitempty"`
	Url   string   `protobuf:"bytes,2,opt,name=url,proto3" json:"url,omitempty"`
}

// DOMElementScopeItem is a DOM element scope item.
type DOMElementScopeItem struct {
	Selector string `protobuf:"bytes,1,opt,name=selector,proto3" json:"selector,omitempty"`
	InnerText string `protobuf:"bytes,2,opt,name=inner_text,json=innerText,proto3" json:"inner_text,omitempty"`
	InnerHtml string `protobuf:"bytes,3,opt,name=inner_html,json=innerHtml,proto3" json:"inner_html,omitempty"`
	Attributes *Metadata `protobuf:"bytes,4,opt,name=attributes,proto3" json:"attributes,omitempty"`
	XPath    string `protobuf:"bytes,5,opt,name=x_path,json=xPath,proto3" json:"x_path,omitempty"`
}

// DynamicContextScopeItem is a dynamic context scope item.
type DynamicContextScopeItem struct {
	Kind        string  `protobuf:"bytes,1,opt,name=kind,proto3" json:"kind,omitempty"`
	Content     string  `protobuf:"bytes,2,opt,name=content,proto3" json:"content,omitempty"`
	Hash        string  `protobuf:"bytes,3,opt,name=hash,proto3" json:"hash,omitempty"`
	Tokens      int32   `protobuf:"varint,4,opt,name=tokens,proto3" json:"tokens,omitempty"`
	Reason      ContextInclusionType `protobuf:"varint,5,opt,name=reason,proto3,enum=codeium_common_pb.ContextInclusionType" json:"reason,omitempty"`
	Source      string  `protobuf:"bytes,6,opt,name=source,proto3" json:"source,omitempty"`
}

// ConsoleLogLine is a single console log line.
type ConsoleLogLine struct {
	Level   StatusLevel `protobuf:"varint,1,opt,name=level,proto3,enum=codeium_common_pb.StatusLevel" json:"level,omitempty"`
	Message string  `protobuf:"bytes,2,opt,name=message,proto3" json:"message,omitempty"`
	Source  string  `protobuf:"bytes,3,opt,name=source,proto3" json:"source,omitempty"`
	Time    time.Time `protobuf:"bytes,4,opt,name=time,proto3,stdtime" json:"time,omitempty"`
}

// ConsoleLogScopeItem is a console-log scope item.
type ConsoleLogScopeItem struct {
	Lines []*ConsoleLogLine `protobuf:"bytes,1,rep,name=lines,proto3" json:"lines,omitempty"`
	Url   string `protobuf:"bytes,2,opt,name=url,proto3" json:"url,omitempty"`
}

// TextOrScopeItem is a union of a text or a scope item.
type TextOrScopeItem struct {
	Text     string             `protobuf:"bytes,1,opt,name=text,proto3" json:"text,omitempty"`
	ScopeItem *ContextScopeItem `protobuf:"bytes,2,opt,name=scope_item,json=scopeItem,proto3" json:"scope_item,omitempty"`
	Kind     ContextScopeType   `protobuf:"varint,3,opt,name=kind,proto3,enum=codeium_common_pb.ContextScopeType" json:"kind,omitempty"`
}

// TextSelection is a text selection.
type TextSelection struct {
	Text     string `protobuf:"bytes,1,opt,name=text,proto3" json:"text,omitempty"`
	Range    *Range `protobuf:"bytes,2,opt,name=range,proto3" json:"range,omitempty"`
	FileUri  string `protobuf:"bytes,3,opt,name=file_uri,json=fileUri,proto3" json:"file_uri,omitempty"`
	Source   string `protobuf:"bytes,4,opt,name=source,proto3" json:"source,omitempty"`
	WasUsed  bool   `protobuf:"varint,5,opt,name=was_used,json=wasUsed,proto3" json:"was_used,omitempty"`
}

// TextBlock is a block of text.
type TextBlock struct {
	Text     string `protobuf:"bytes,1,opt,name=text,proto3" json:"text,omitempty"`
	Language Language `protobuf:"varint,2,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	Range    *Range `protobuf:"bytes,3,opt,name=range,proto3" json:"range,omitempty"`
	Source   string `protobuf:"bytes,4,opt,name=source,proto3" json:"source,omitempty"`
}

// FileComment is a comment on a file.
type FileComment struct {
	FileUri string `protobuf:"bytes,1,opt,name=file_uri,json=fileUri,proto3" json:"file_uri,omitempty"`
	Comment string `protobuf:"bytes,2,opt,name=comment,proto3" json:"comment,omitempty"`
}

// FileCommentInfo is a file comment with metadata.
type FileCommentInfo struct {
	Comment *FileComment `protobuf:"bytes,1,opt,name=comment,proto3" json:"comment,omitempty"`
	Author  string       `protobuf:"bytes,2,opt,name=author,proto3" json:"author,omitempty"`
	CreatedAt time.Time  `protobuf:"bytes,3,opt,name=created_at,json=createdAt,proto3,stdtime" json:"created_at,omitempty"`
}

// FileDiffComment is a file-diff comment.
type FileDiffComment struct {
	FileUri string `protobuf:"bytes,1,opt,name=file_uri,json=fileUri,proto3" json:"file_uri,omitempty"`
	Comment string `protobuf:"bytes,2,opt,name=comment,proto3" json:"comment,omitempty"`
	Range   *Range `protobuf:"bytes,3,opt,name=range,proto3" json:"range,omitempty"`
}

// FileLineRange is a (file, line) range.
type FileLineRange struct {
	FileUri  string `protobuf:"bytes,1,opt,name=file_uri,json=fileUri,proto3" json:"file_uri,omitempty"`
	StartLine int32 `protobuf:"varint,2,opt,name=start_line,json=startLine,proto3" json:"start_line,omitempty"`
	EndLine  int32  `protobuf:"varint,3,opt,name=end_line,json=endLine,proto3" json:"end_line,omitempty"`
}

// DiffCommentInfo is the diff comment info.
type DiffCommentInfo struct {
	Comment string `protobuf:"bytes,1,opt,name=comment,proto3" json:"comment,omitempty"`
	Range   *Range `protobuf:"bytes,2,opt,name=range,proto3" json:"range,omitempty"`
	Author  string `protobuf:"bytes,3,opt,name=author,proto3" json:"author,omitempty"`
}

// WordCount is a word count entry.
type WordCount struct {
	Words    int32 `protobuf:"varint,1,opt,name=words,proto3" json:"words,omitempty"`
	Lines    int32 `protobuf:"varint,2,opt,name=lines,proto3" json:"lines,omitempty"`
	Chars    int32 `protobuf:"varint,3,opt,name=chars,proto3" json:"chars,omitempty"`
	Bytes    int32 `protobuf:"varint,4,opt,name=bytes,proto3" json:"bytes,omitempty"`
}

// SnippetWithWordCount is a snippet with a word count.
type SnippetWithWordCount struct {
	Text      string `protobuf:"bytes,1,opt,name=text,proto3" json:"text,omitempty"`
	WordCount *WordCount `protobuf:"bytes,2,opt,name=word_count,json=wordCount,proto3" json:"word_count,omitempty"`
}

// Source is a generic source reference.
type Source struct {
	Url    string `protobuf:"bytes,1,opt,name=url,proto3" json:"url,omitempty"`
	Title  string `protobuf:"bytes,2,opt,name=title,proto3" json:"title,omitempty"`
	Origin string `protobuf:"bytes,3,opt,name=origin,proto3" json:"origin,omitempty"`
}

// Status is a generic status response.
type Status struct {
	Code    int32  `protobuf:"varint,1,opt,name=code,proto3" json:"code,omitempty"`
	Message string `protobuf:"bytes,2,opt,name=message,proto3" json:"message,omitempty"`
}

// GRPCStatus is a gRPC status.
type GRPCStatus struct {
	Code    int32  `protobuf:"varint,1,opt,name=code,proto3" json:"code,omitempty"`
	Message string `protobuf:"bytes,2,opt,name=message,proto3" json:"message,omitempty"`
}

// ExtraMetadata is extra metadata.
type ExtraMetadata struct {
	Key   string `protobuf:"bytes,1,opt,name=key,proto3" json:"key,omitempty"`
	Value string `protobuf:"bytes,2,opt,name=value,proto3" json:"value,omitempty"`
}

// AnalyticsEventMetadata is the analytics event metadata.
type AnalyticsEventMetadata struct {
	EventType  ProductEventType `protobuf:"varint,1,opt,name=event_type,json=eventType,proto3,enum=codeium_common_pb.ProductEventType" json:"event_type,omitempty"`
	Extra      []*ExtraMetadata `protobuf:"bytes,2,rep,name=extra,proto3" json:"extra,omitempty"`
	Experiments []*ExperimentWithVariant `protobuf:"bytes,3,rep,name=experiments,proto3" json:"experiments,omitempty"`
}

// ErrorSamplingConfig is the error sampling config.
type ErrorSamplingConfig struct {
	Rate     float32 `protobuf:"fixed32,1,opt,name=rate,proto3" json:"rate,omitempty"`
	Strategy string  `protobuf:"bytes,2,opt,name=strategy,proto3" json:"strategy,omitempty"`
}

// MetricsSamplingConfig is the metrics sampling config.
type MetricsSamplingConfig struct {
	Rate     float32 `protobuf:"fixed32,1,opt,name=rate,proto3" json:"rate,omitempty"`
	Strategy string  `protobuf:"bytes,2,opt,name=strategy,proto3" json:"strategy,omitempty"`
	MaxEventsPerSec int32 `protobuf:"varint,3,opt,name=max_events_per_sec,json=maxEventsPerSec,proto3" json:"max_events_per_sec,omitempty"`
	Scope    MetricsScope `protobuf:"varint,4,opt,name=scope,proto3,enum=codeium_common_pb.MetricsScope" json:"scope,omitempty"`
}

// MetricsRecord is a metrics record.
type MetricsRecord struct {
	Name       string  `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Value      float64 `protobuf:"fixed64,2,opt,name=value,proto3" json:"value,omitempty"`
	Tags       *Metadata `protobuf:"bytes,3,opt,name=tags,proto3" json:"tags,omitempty"`
	Timestamp  time.Time `protobuf:"bytes,4,opt,name=timestamp,proto3,stdtime" json:"timestamp,omitempty"`
	Scope      MetricsScope `protobuf:"varint,5,opt,name=scope,proto3,enum=codeium_common_pb.MetricsScope" json:"scope,omitempty"`
	Unit       string  `protobuf:"bytes,6,opt,name=unit,proto3" json:"unit,omitempty"`
	Source     string  `protobuf:"bytes,7,opt,name=source,proto3" json:"source,omitempty"`
	Service    string  `protobuf:"bytes,8,opt,name=service,proto3" json:"service,omitempty"`
	Kind       string  `protobuf:"bytes,9,opt,name=kind,proto3" json:"kind,omitempty"`
}

// MetricsMetadata is the metrics metadata.
type MetricsMetadata struct {
	Service    string `protobuf:"bytes,1,opt,name=service,proto3" json:"service,omitempty"`
	Hostname   string `protobuf:"bytes,2,opt,name=hostname,proto3" json:"hostname,omitempty"`
	Region     string `protobuf:"bytes,3,opt,name=region,proto3" json:"region,omitempty"`
	AppVersion string `protobuf:"bytes,4,opt,name=app_version,json=appVersion,proto3" json:"app_version,omitempty"`
	BuildId    string `protobuf:"bytes,5,opt,name=build_id,json=buildId,proto3" json:"build_id,omitempty"`
	UserId     string `protobuf:"bytes,6,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
}

// StepLevelMetricsRecord is a per-step metrics record.
type StepLevelMetricsRecord struct {
	Step    int32 `protobuf:"varint,1,opt,name=step,proto3" json:"step,omitempty"`
	Metrics *MetricsRecord `protobuf:"bytes,2,opt,name=metrics,proto3" json:"metrics,omitempty"`
	DurationMs int32 `protobuf:"varint,3,opt,name=duration_ms,json=durationMs,proto3" json:"duration_ms,omitempty"`
	Tokens int64 `protobuf:"varint,4,opt,name=tokens,proto3" json:"tokens,omitempty"`
	CostUsd float32 `protobuf:"fixed32,5,opt,name=cost_usd,json=costUsd,proto3" json:"cost_usd,omitempty"`
}

// OnlineMetricsConfig is the online-metrics config.
type OnlineMetricsConfig struct {
	Enabled   bool   `protobuf:"varint,1,opt,name=enabled,proto3" json:"enabled,omitempty"`
	SampleRate float32 `protobuf:"fixed32,2,opt,name=sample_rate,json=sampleRate,proto3" json:"sample_rate,omitempty"`
	MaxBufferSize int32 `protobuf:"varint,3,opt,name=max_buffer_size,json=maxBufferSize,proto3" json:"max_buffer_size,omitempty"`
}

// ObservabilityData is the observability data block.
type ObservabilityData struct {
	Metrics []*MetricsRecord `protobuf:"bytes,1,rep,name=metrics,proto3" json:"metrics,omitempty"`
	Logs    []*LogEvent      `protobuf:"bytes,2,rep,name=logs,proto3" json:"logs,omitempty"`
	Errors  []*ErrorTrace    `protobuf:"bytes,3,rep,name=errors,proto3" json:"errors,omitempty"`
}

// ErrorTraceMetadata is the error trace metadata.
type ErrorTraceMetadata struct {
	Service string `protobuf:"bytes,1,opt,name=service,proto3" json:"service,omitempty"`
	Version string `protobuf:"bytes,2,opt,name=version,proto3" json:"version,omitempty"`
}

// LastUpdateRecord is the last-update record.
type LastUpdateRecord struct {
	UpdatedAt time.Time `protobuf:"bytes,1,opt,name=updated_at,json=updatedAt,proto3,stdtime" json:"updated_at,omitempty"`
	Type      LastUpdateType `protobuf:"varint,2,opt,name=type,proto3,enum=codeium_common_pb.LastUpdateType" json:"type,omitempty"`
}

// DocumentLinesElement is one document line.
type DocumentLinesElement struct {
	Line    int32  `protobuf:"varint,1,opt,name=line,proto3" json:"line,omitempty"`
	Text    string `protobuf:"bytes,2,opt,name=text,proto3" json:"text,omitempty"`
	Indent  int32  `protobuf:"varint,3,opt,name=indent,proto3" json:"indent,omitempty"`
	HasError bool  `protobuf:"varint,4,opt,name=has_error,json=hasError,proto3" json:"has_error,omitempty"`
}

// DocumentQuery is a document query.
type DocumentQuery struct {
	Path     string `protobuf:"bytes,1,opt,name=path,proto3" json:"path,omitempty"`
	Range    *Range `protobuf:"bytes,2,opt,name=range,proto3" json:"range,omitempty"`
	Language Language `protobuf:"varint,3,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	Query    string `protobuf:"bytes,4,opt,name=query,proto3" json:"query,omitempty"`
	Limit    int32  `protobuf:"varint,5,opt,name=limit,proto3" json:"limit,omitempty"`
}

// FeatureUsageData is the feature-usage data.
type FeatureUsageData struct {
	Feature  UserFeatures `protobuf:"varint,1,opt,name=feature,proto3,enum=codeium_common_pb.UserFeatures" json:"feature,omitempty"`
	Count    int64        `protobuf:"varint,2,opt,name=count,proto3" json:"count,omitempty"`
	LastUsed time.Time    `protobuf:"bytes,3,opt,name=last_used,json=lastUsed,proto3,stdtime" json:"last_used,omitempty"`
}

// DeployTarget is a deploy target.
type DeployTarget struct {
	Name     string  `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Provider DeploymentProvider `protobuf:"varint,2,opt,name=provider,proto3,enum=codeium_common_pb.DeploymentProvider" json:"provider,omitempty"`
	Region   string  `protobuf:"bytes,3,opt,name=region,proto3" json:"region,omitempty"`
	Url      string  `protobuf:"bytes,4,opt,name=url,proto3" json:"url,omitempty"`
	Status   DeploymentBuildStatus `protobuf:"varint,5,opt,name=status,proto3,enum=codeium_common_pb.DeploymentBuildStatus" json:"status,omitempty"`
}

// JetskiTelemetryExtension is a telemetry extension.
type JetskiTelemetryExtension struct {
	ExtensionId   string `protobuf:"bytes,1,opt,name=extension_id,json=extensionId,proto3" json:"extension_id,omitempty"`
	Version       string `protobuf:"bytes,2,opt,name=version,proto3" json:"version,omitempty"`
	Enabled       bool   `protobuf:"varint,3,opt,name=enabled,proto3" json:"enabled,omitempty"`
	SampleRate    float32 `protobuf:"fixed32,4,opt,name=sample_rate,json=sampleRate,proto3" json:"sample_rate,omitempty"`
	MaxBufferSize int32  `protobuf:"varint,5,opt,name=max_buffer_size,json=maxBufferSize,proto3" json:"max_buffer_size,omitempty"`
	FlushIntervalMs int32 `protobuf:"varint,6,opt,name=flush_interval_ms,json=flushIntervalMs,proto3" json:"flush_interval_ms,omitempty"`
	Endpoint      string `protobuf:"bytes,7,opt,name=endpoint,proto3" json:"endpoint,omitempty"`
	Headers       map[string]string `protobuf:"bytes,8,rep,name=headers,proto3" json:"headers,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
	Tags          map[string]string `protobuf:"bytes,9,rep,name=tags,proto3" json:"tags,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
	Compression   string `protobuf:"bytes,10,opt,name=compression,proto3" json:"compression,omitempty"`
	AuthToken     string `protobuf:"bytes,11,opt,name=auth_token,json=authToken,proto3" json:"auth_token,omitempty"`
}

// RemoteControlDetails is the remote-control transport metadata.
type RemoteControlDetails struct {
	Transport *RemoteControlDetails_Transport `protobuf:"bytes,1,opt,name=transport,proto3" json:"transport,omitempty"`
	SessionId string `protobuf:"bytes,2,opt,name=session_id,json=sessionId,proto3" json:"session_id,omitempty"`
}

// RemoteControlDetails_Transport is the transport.
type RemoteControlDetails_Transport struct {
	Kind   string `protobuf:"bytes,1,opt,name=kind,proto3" json:"kind,omitempty"`
	Url    string `protobuf:"bytes,2,opt,name=url,proto3" json:"url,omitempty"`
	ApiKey string `protobuf:"bytes,3,opt,name=api_key,json=apiKey,proto3" json:"api_key,omitempty"`
}

// SamplingStrategyUniform is the uniform sampling strategy.
type SamplingStrategyUniform struct {
	Rate float32 `protobuf:"fixed32,1,opt,name=rate,proto3" json:"rate,omitempty"`
}

// SamplingStrategyGeneratorMetadataAware is the metadata-aware
// sampling strategy.
type SamplingStrategyGeneratorMetadataAware struct {
	BaseRate     float32 `protobuf:"fixed32,1,opt,name=base_rate,json=baseRate,proto3" json:"base_rate,omitempty"`
	MaxRate      float32 `protobuf:"fixed32,2,opt,name=max_rate,json=maxRate,proto3" json:"max_rate,omitempty"`
	MinRate      float32 `protobuf:"fixed32,3,opt,name=min_rate,json=minRate,proto3" json:"min_rate,omitempty"`
	GeneratorKey string  `protobuf:"bytes,4,opt,name=generator_key,json=generatorKey,proto3" json:"generator_key,omitempty"`
}

// UserNUXState is the user NUX state.
type UserNUXState struct {
	Event     UserNUXEvent `protobuf:"varint,1,opt,name=event,proto3,enum=codeium_common_pb.UserNUXEvent" json:"event,omitempty"`
	Timestamp time.Time    `protobuf:"bytes,2,opt,name=timestamp,proto3,stdtime" json:"timestamp,omitempty"`
}

// UserAccountSettings is the user account settings.
type UserAccountSettings struct {
	Email        string `protobuf:"bytes,1,opt,name=email,proto3" json:"email,omitempty"`
	Name         string `protobuf:"bytes,2,opt,name=name,proto3" json:"name,omitempty"`
	AvatarUrl    string `protobuf:"bytes,3,opt,name=avatar_url,json=avatarUrl,proto3" json:"avatar_url,omitempty"`
	Timezone     string `protobuf:"bytes,4,opt,name=timezone,proto3" json:"timezone,omitempty"`
}

// ChatStatsByDateEntry is a per-date chat stats entry.
type ChatStatsByDateEntry struct {
	DateIso string `protobuf:"bytes,1,opt,name=date_iso,json=dateIso,proto3" json:"date_iso,omitempty"`
	Count   int64  `protobuf:"varint,2,opt,name=count,proto3" json:"count,omitempty"`
}

// ChatStatsByModelEntry is a per-model chat stats entry.
type ChatStatsByModelEntry struct {
	Model string `protobuf:"bytes,1,opt,name=model,proto3" json:"model,omitempty"`
	Count int64  `protobuf:"varint,2,opt,name=count,proto3" json:"count,omitempty"`
}

// CompletionByDateEntry is a per-date completion stats entry.
type CompletionByDateEntry struct {
	DateIso string `protobuf:"bytes,1,opt,name=date_iso,json=dateIso,proto3" json:"date_iso,omitempty"`
	Count   int64  `protobuf:"varint,2,opt,name=count,proto3" json:"count,omitempty"`
}

// CompletionByLanguageEntry is a per-language completion stats entry.
type CompletionByLanguageEntry struct {
	Language Language `protobuf:"varint,1,opt,name=language,proto3,enum=codeium_common_pb.Language" json:"language,omitempty"`
	Count    int64    `protobuf:"varint,2,opt,name=count,proto3" json:"count,omitempty"`
}

// CommandStatsByDateEntry is a per-date command stats entry.
type CommandStatsByDateEntry struct {
	DateIso string `protobuf:"bytes,1,opt,name=date_iso,json=dateIso,proto3" json:"date_iso,omitempty"`
	Count   int64  `protobuf:"varint,2,opt,name=count,proto3" json:"count,omitempty"`
}

// ConversationBrainConfig is the conversation brain config.
type ConversationBrainConfig struct {
	Enabled bool `protobuf:"varint,1,opt,name=enabled,proto3" json:"enabled,omitempty"`
}

// ModelOrAlias is a model-or-alias reference.
type ModelOrAlias struct {
	Model string `protobuf:"bytes,1,opt,name=model,proto3" json:"model,omitempty"`
	Alias string `protobuf:"bytes,2,opt,name=alias,proto3" json:"alias,omitempty"`
}

// ActionPointer is a pointer to an action in a cascade.
type ActionPointer struct {
	CodePlanId  string `protobuf:"bytes,1,opt,name=code_plan_id,json=codePlanId,proto3" json:"code_plan_id,omitempty"`
	CortexPlanId string `protobuf:"bytes,2,opt,name=cortex_plan_id,json=cortexPlanId,proto3" json:"cortex_plan_id,omitempty"`
	ActionIndex int32  `protobuf:"varint,3,opt,name=action_index,json=actionIndex,proto3" json:"action_index,omitempty"`
}

// ClientInfo is the client info block.
type ClientInfo struct {
	Name      string `protobuf:"bytes,1,opt,name=name,proto3" json:"name,omitempty"`
	Version   string `protobuf:"bytes,2,opt,name=version,proto3" json:"version,omitempty"`
	BuildId   string `protobuf:"bytes,3,opt,name=build_id,json=buildId,proto3" json:"build_id,omitempty"`
	OS        string `protobuf:"bytes,4,opt,name=os,proto3" json:"os,omitempty"`
	Arch      string `protobuf:"bytes,5,opt,name=arch,proto3" json:"arch,omitempty"`
}

// TerminalCommandData is a terminal command execution data.
type TerminalCommandData struct {
	Command string `protobuf:"bytes,1,opt,name=command,proto3" json:"command,omitempty"`
	Output  string `protobuf:"bytes,2,opt,name=output,proto3" json:"output,omitempty"`
	ExitCode int32 `protobuf:"varint,3,opt,name=exit_code,json=exitCode,proto3" json:"exit_code,omitempty"`
	DurationMs int32 `protobuf:"varint,4,opt,name=duration_ms,json=durationMs,proto3" json:"duration_ms,omitempty"`
}

// ApiProviderConfig is a per-API-provider config.
type ApiProviderConfig struct {
	Provider        APIProvider `protobuf:"varint,1,opt,name=provider,proto3,enum=codeium_common_pb.APIProvider" json:"provider,omitempty"`
	Weight          float32     `protobuf:"fixed32,2,opt,name=weight,proto3" json:"weight,omitempty"`
	CacheTtlMinutes int32       `protobuf:"varint,3,opt,name=cache_ttl_minutes,json=cacheTtlMinutes,proto3" json:"cache_ttl_minutes,omitempty"`
}

// ApiProviderConfigMap is a (provider, config) map.
type ApiProviderConfigMap struct {
	ProviderMap map[string]*ApiProviderConfig `protobuf:"bytes,1,rep,name=provider_map,json=providerMap,proto3" json:"provider_map,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
}

// ApiProviderRoutingConfig is the routing config for API providers.
type ApiProviderRoutingConfig struct {
	ModelMap map[string]*ApiProviderConfigMap `protobuf:"bytes,1,rep,name=model_map,json=modelMap,proto3" json:"model_map,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"bytes,2,opt,name=value,proto3"`
}

// ModelConfig is the per-model config.
type ModelConfig struct {
	Model    string `protobuf:"bytes,1,opt,name=model,proto3" json:"model,omitempty"`
	Provider string `protobuf:"bytes,2,opt,name=provider,proto3" json:"provider,omitempty"`
	Enabled  bool   `protobuf:"varint,3,opt,name=enabled,proto3" json:"enabled,omitempty"`
}

// ModelNotification is a model notification.
type ModelNotification struct {
	Model  string `protobuf:"bytes,1,opt,name=model,proto3" json:"model,omitempty"`
	Status string `protobuf:"bytes,2,opt,name=status,proto3" json:"status,omitempty"`
}

// MockResponseData is a mock response data.
type MockResponseData struct {
	Content string `protobuf:"bytes,1,opt,name=content,proto3" json:"content,omitempty"`
}

// PostgresDbStats is the postgres db stats.
type PostgresDbStats struct {
	DocCount  int64 `protobuf:"varint,1,opt,name=doc_count,json=docCount,proto3" json:"doc_count,omitempty"`
	TotalSize int64 `protobuf:"varint,2,opt,name=total_size,json=totalSize,proto3" json:"total_size,omitempty"`
}

// PermissionGrantsConfig is the permission grants config.
//
// 【一手】 2026-10-27: the 1.4.8 descriptor. The message carries three
// lists of rules — `allow`, `deny` and `ask` — and nothing else; the
// 1.3.5 recovery had a `grants` list, a `mode` string and a `deny`
// list, none of which matches.
type PermissionGrantsConfig struct {
	Allow []string `protobuf:"bytes,1,rep,name=allow,proto3" json:"allow,omitempty"`
	Deny  []string `protobuf:"bytes,2,rep,name=deny,proto3" json:"deny,omitempty"`
	Ask   []string `protobuf:"bytes,3,rep,name=ask,proto3" json:"ask,omitempty"`
}

// CascadeModelHeaderWarningExperimentPayload is the payload for the
// model-header warning experiment.
type CascadeModelHeaderWarningExperimentPayload struct {
	WarningMessage string `protobuf:"bytes,1,opt,name=warning_message,json=warningMessage,proto3" json:"warning_message,omitempty"`
	WarningType    string `protobuf:"bytes,2,opt,name=warning_type,json=warningType,proto3" json:"warning_type,omitempty"`
}

// ExperimentLanguageServerVersionPayload is the language-server
// version experiment payload.
type ExperimentLanguageServerVersionPayload struct {
	MinVersion string `protobuf:"bytes,1,opt,name=min_version,json=minVersion,proto3" json:"min_version,omitempty"`
	MaxVersion string `protobuf:"bytes,2,opt,name=max_version,json=maxVersion,proto3" json:"max_version,omitempty"`
}

// ExperimentMiddleModeTokenPayload is the middle-mode token
// experiment payload.
type ExperimentMiddleModeTokenPayload struct {
	Tokens int32 `protobuf:"varint,1,opt,name=tokens,proto3" json:"tokens,omitempty"`
}

// ExperimentModelConfigPayload is the model-config experiment payload.
type ExperimentModelConfigPayload struct {
	Model  string `protobuf:"bytes,1,opt,name=model,proto3" json:"model,omitempty"`
	Config *ModelConfig `protobuf:"bytes,2,opt,name=config,proto3" json:"config,omitempty"`
}

// ExperimentMultilineModelThresholdPayload is the multiline threshold payload.
type ExperimentMultilineModelThresholdPayload struct {
	Threshold int32 `protobuf:"varint,1,opt,name=threshold,proto3" json:"threshold,omitempty"`
}

// ExperimentProfilingTelemetrySampleRatePayload is the profiling sample rate payload.
type ExperimentProfilingTelemetrySampleRatePayload struct {
	SampleRate float32 `protobuf:"fixed32,1,opt,name=sample_rate,json=sampleRate,proto3" json:"sample_rate,omitempty"`
}

// ExperimentSentryPayload is the sentry experiment payload.
type ExperimentSentryPayload struct {
	ProcedureToSampleRate map[string]float32 `protobuf:"bytes,1,rep,name=procedure_to_sample_rate,json=procedureToSampleRate,proto3" json:"procedure_to_sample_rate,omitempty" protobuf_key:"bytes,1,opt,name=key,proto3" protobuf_val:"fixed32,2,opt,name=value,proto3"`
	Enabled bool `protobuf:"varint,2,opt,name=enabled,proto3" json:"enabled,omitempty"`
	Environment string `protobuf:"bytes,3,opt,name=environment,proto3" json:"environment,omitempty"`
}

// ThirdPartyWebSearchConfig is the 3rd-party web search config.
type ThirdPartyWebSearchConfig struct {
	Provider ThirdPartyWebSearchProvider `protobuf:"varint,1,opt,name=provider,proto3,enum=codeium_common_pb.ThirdPartyWebSearchProvider" json:"provider,omitempty"`
	ApiKey   string `protobuf:"bytes,2,opt,name=api_key,json=apiKey,proto3" json:"api_key,omitempty"`
}

// WebDocsOption is the web docs option.
type WebDocsOption struct {
	Url     string `protobuf:"bytes,1,opt,name=url,proto3" json:"url,omitempty"`
	Title   string `protobuf:"bytes,2,opt,name=title,proto3" json:"title,omitempty"`
	Snippet string `protobuf:"bytes,3,opt,name=snippet,proto3" json:"snippet,omitempty"`
	WasUsed bool   `protobuf:"varint,4,opt,name=was_used,json=wasUsed,proto3" json:"was_used,omitempty"`
	Source  string `protobuf:"bytes,5,opt,name=source,proto3" json:"source,omitempty"`
	Score   float32 `protobuf:"fixed32,6,opt,name=score,proto3" json:"score,omitempty"`
}

// MarkdownChunk is a markdown chunk.
type MarkdownChunk struct {
	Content  string  `protobuf:"bytes,1,opt,name=content,proto3" json:"content,omitempty"`
	Headers  []*MarkdownChunk_MarkdownHeader `protobuf:"bytes,2,rep,name=headers,proto3" json:"headers,omitempty"`
}

// MarkdownChunk_MarkdownHeader is a markdown header.
type MarkdownChunk_MarkdownHeader struct {
	Level int32 `protobuf:"varint,1,opt,name=level,proto3" json:"level,omitempty"`
	Text  string `protobuf:"bytes,2,opt,name=text,proto3" json:"text,omitempty"`
}

// LspReference is an LSP reference.
type LspReference struct {
	Uri  string `protobuf:"bytes,1,opt,name=uri,proto3" json:"uri,omitempty"`
	Range *Range `protobuf:"bytes,2,opt,name=range,proto3" json:"range,omitempty"`
	Text  string `protobuf:"bytes,3,opt,name=text,proto3" json:"text,omitempty"`
}

// LanguageServerDiagnostics is the LSP diagnostics list.
type LanguageServerDiagnostics struct {
	Uri        string             `protobuf:"bytes,1,opt,name=uri,proto3" json:"uri,omitempty"`
	Diagnostics []*CodeDiagnostic `protobuf:"bytes,2,rep,name=diagnostics,proto3" json:"diagnostics,omitempty"`
	Version    int32              `protobuf:"varint,3,opt,name=version,proto3" json:"version,omitempty"`
}

// ClientInfo is declared above; this type alias ensures no double-decl.
type _ClientInfo = ClientInfo

// RewardsRequest is the rewards RPC request.
type RewardsRequest struct {
	UserId    string `protobuf:"bytes,1,opt,name=user_id,json=userId,proto3" json:"user_id,omitempty"`
	RequestId string `protobuf:"bytes,2,opt,name=request_id,json=requestId,proto3" json:"request_id,omitempty"`
	Amount    int64  `protobuf:"varint,3,opt,name=amount,proto3" json:"amount,omitempty"`
}

// RewardsResponse is the rewards RPC response.
type RewardsResponse struct {
	Accepted bool   `protobuf:"varint,1,opt,name=accepted,proto3" json:"accepted,omitempty"`
	Balance  int64  `protobuf:"varint,2,opt,name=balance,proto3" json:"balance,omitempty"`
	Error    string `protobuf:"bytes,3,opt,name=error,proto3" json:"error,omitempty"`
}

// CciWithSubrange is a context-item + subrange pair.
type CciWithSubrange struct {
	Item     *CodeContextItem `protobuf:"bytes,1,opt,name=item,proto3" json:"item,omitempty"`
	Subrange *ContextSubrange `protobuf:"bytes,2,opt,name=subrange,proto3" json:"subrange,omitempty"`
}

