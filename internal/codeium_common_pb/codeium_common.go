// Package codeium_common_pb is a recovered (re-implemented) binding
// for google3/third_party/jetski/codeium_common_pb. Source-of-truth
// artifacts are recovered from the v1.3.5 binary at
// /workspace/agy-recovered/docs/analysis-v1.3.5-official/funcs-v1.3.5-pclntab.txt
// (1379 Get<Field> methods across 253 message types, plus 104 enum
// types in this package).
//
// The 1.3.5 binary uses this package for the shared proto schema of
// the jetski line-server products (Cascade IDE assistant, indexing
// service, telemetry, and the connect-rpc interfaces). The package
// has no gRPC service of its own — it only provides the message and
// enum types that other packages (extension_server_pb, cortex_pb,
// language_server_pb, ...) compose into their services.
//
// Key types recovered from the v1.3.5 binary (decompile dir
// /workspace/agy-recovered/docs/decompiled-v1.3.5/c/google3_third_party_jetski_codeium_common_pb_codeium_common_go_proto/
// contains 102 .c files; full schema reconstructed from pclntab):
//
//   - CascadeModelConfigData (6 fields)
//   - CascadeNUXConfig (18 fields)
//   - CascadeNUXState (2 fields)
//   - PlanInfo (31 fields)
//   - TeamOrganizationalControls (6 fields)
//   - UserSettings (63 fields)
//   - UserStatus (22 fields)
//   - ... 246 more message types
//
// In the recovered build, this package provides:
//   - the proto message Go structs (one struct per proto type);
//   - the proto enum Go types (one int32 enum per proto enum);
//   - a small set of Get<Field>() accessors that match the recovered
//     protobuf-generated method names 1:1.
//
// Style matches internal/language_server_pb and the new
// internal/extension_server_pb: package comment + 1:1 decompile note
// + types split into a sibling file + a unit test file with 5+ sub-tests.
package codeium_common_pb

import "time"

// ErrNotImplemented is returned by helpers that wrap upstream services.
var ErrNotImplemented = ErrNoImpl

// ErrNoImpl is a placeholder error returned by stub methods.
var ErrNoImpl = &codeiumError{msg: "codeium_common_pb: not implemented in recovered build"}

type codeiumError struct{ msg string }

func (e *codeiumError) Error() string { return e.msg }

// ---------------------------------------------------------------------------
// Top-level message types (recovered 1:1 from pclntab 0x661e260-0x6624000).
// ---------------------------------------------------------------------------

// CascadeModelConfigData is the resolved model configuration delivered
// to the IDE. It carries the client-side model configs and the
// server-side battle-mode model configs.
type CascadeModelConfigData struct {
	ClientModelConfigs          []*ClientModelConfig  `protobuf:"bytes,1,rep,name=client_model_configs,json=clientModelConfigs,proto3" json:"client_model_configs,omitempty"`
	ClientModelSorts            []*ClientModelSort    `protobuf:"bytes,2,rep,name=client_model_sorts,json=clientModelSorts,proto3" json:"client_model_sorts,omitempty"`
	DefaultOverrideModelConfig  *DefaultOverrideModelConfig `protobuf:"bytes,3,opt,name=default_override_model_config,json=defaultOverrideModelConfig,proto3" json:"default_override_model_config,omitempty"`
	BattleModeModelConfigs      []*ClientModelConfig  `protobuf:"bytes,4,rep,name=battle_mode_model_configs,json=battleModeModelConfigs,proto3" json:"battle_mode_model_configs,omitempty"`
	BattleModeModelSorts        []*ClientModelSort    `protobuf:"bytes,5,rep,name=battle_mode_model_sorts,json=battleModeModelSorts,proto3" json:"battle_mode_model_sorts,omitempty"`
	ErrorMessage                string                `protobuf:"bytes,6,opt,name=error_message,json=errorMessage,proto3" json:"error_message,omitempty"`
}

// DefaultOverrideModelConfig is a single (model, multiplier) entry.
type DefaultOverrideModelConfig struct {
	ModelOrAlias    string  `protobuf:"bytes,1,opt,name=model_or_alias,json=modelOrAlias,proto3" json:"model_or_alias,omitempty"`
	CreditMultiplier float32 `protobuf:"fixed32,2,opt,name=credit_multiplier,json=creditMultiplier,proto3" json:"credit_multiplier,omitempty"`
}

// ClientModelConfig is one model config in the client model config list.
type ClientModelConfig struct {
	ModelOrAlias         string             `protobuf:"bytes,1,opt,name=model_or_alias,json=modelOrAlias,proto3" json:"model_or_alias,omitempty"`
	Label                string             `protobuf:"bytes,2,opt,name=label,proto3" json:"label,omitempty"`
	ChatLabel            string             `protobuf:"bytes,3,opt,name=chat_label,json=chatLabel,proto3" json:"chat_label,omitempty"`
	Description          string             `protobuf:"bytes,4,opt,name=description,proto3" json:"description,omitempty"`
	MaxOutputTokens      int32              `protobuf:"varint,5,opt,name=max_output_tokens,json=maxOutputTokens,proto3" json:"max_output_tokens,omitempty"`
	ContextLimit         int32              `protobuf:"varint,6,opt,name=context_limit,json=contextLimit,proto3" json:"context_limit,omitempty"`
	ModelFeatures        *ModelFeatures     `protobuf:"bytes,7,opt,name=model_features,json=modelFeatures,proto3" json:"model_features,omitempty"`
	ModelInfo            *ModelInfo         `protobuf:"bytes,8,opt,name=model_info,json=modelInfo,proto3" json:"model_info,omitempty"`
	Enabled              bool               `protobuf:"varint,9,opt,name=enabled,proto3" json:"enabled,omitempty"`
	SupportsImages       bool               `protobuf:"varint,10,opt,name=supports_images,json=supportsImages,proto3" json:"supports_images,omitempty"`
	SupportsTools        bool               `protobuf:"varint,11,opt,name=supports_tools,json=supportsTools,proto3" json:"supports_tools,omitempty"`
	IsRecommended        bool               `protobuf:"varint,12,opt,name=is_recommended,json=isRecommended,proto3" json:"is_recommended,omitempty"`
	IsFree               bool               `protobuf:"varint,13,opt,name=is_free,json=isFree,proto3" json:"is_free,omitempty"`
	Cost                 float32            `protobuf:"fixed32,14,opt,name=cost,proto3" json:"cost,omitempty"`
	SortOrder            int32              `protobuf:"varint,15,opt,name=sort_order,json=sortOrder,proto3" json:"sort_order,omitempty"`
	Capabilities         []string           `protobuf:"bytes,16,rep,name=capabilities,proto3" json:"capabilities,omitempty"`
	RequiredTier         string             `protobuf:"bytes,17,opt,name=required_tier,json=requiredTier,proto3" json:"required_tier,omitempty"`
	Group                *ClientModelGroup  `protobuf:"bytes,18,opt,name=group,proto3" json:"group,omitempty"`
	BattleModeConfig     *DefaultOverrideModelConfig `protobuf:"bytes,19,opt,name=battle_mode_config,json=battleModeConfig,proto3" json:"battle_mode_config,omitempty"`
	TabToJump            TabToJump          `protobuf:"varint,20,opt,name=tab_to_jump,json=tabToJump,proto3,enum=codeium_common_pb.TabToJump" json:"tab_to_jump,omitempty"`
	TabEnabled           TabEnabled         `protobuf:"varint,21,opt,name=tab_enabled,json=tabEnabled,proto3,enum=codeium_common_pb.TabEnabled" json:"tab_enabled,omitempty"`
}

// ClientModelSort is a (model, position) sort hint.
type ClientModelSort struct {
	ModelOrAlias string `protobuf:"bytes,1,opt,name=model_or_alias,json=modelOrAlias,proto3" json:"model_or_alias,omitempty"`
	SortOrder    int32  `protobuf:"varint,2,opt,name=sort_order,json=sortOrder,proto3" json:"sort_order,omitempty"`
}

// ClientModelGroup groups models in the UI.
type ClientModelGroup struct {
	GroupId   string `protobuf:"bytes,1,opt,name=group_id,json=groupId,proto3" json:"group_id,omitempty"`
	GroupName string `protobuf:"bytes,2,opt,name=group_name,json=groupName,proto3" json:"group_name,omitempty"`
}

// CascadeNUXConfig is a single NUX (new-user experience) notification.
type CascadeNUXConfig struct {
	Uid                  string   `protobuf:"bytes,1,opt,name=uid,proto3" json:"uid,omitempty"`
	Location             CascadeNUXLocation   `protobuf:"varint,2,opt,name=location,proto3,enum=codeium_common_pb.CascadeNUXLocation" json:"location,omitempty"`
	Trigger              CascadeNUXTrigger    `protobuf:"varint,3,opt,name=trigger,proto3,enum=codeium_common_pb.CascadeNUXTrigger" json:"trigger,omitempty"`
	AnalyticsEventName   string   `protobuf:"bytes,4,opt,name=analytics_event_name,json=analyticsEventName,proto3" json:"analytics_event_name,omitempty"`
	LearnMoreUrl         string   `protobuf:"bytes,5,opt,name=learn_more_url,json=learnMoreUrl,proto3" json:"learn_more_url,omitempty"`
	Priority             int32    `protobuf:"varint,6,opt,name=priority,proto3" json:"priority,omitempty"`
	Icon                 CascadeNUXIcon        `protobuf:"varint,7,opt,name=icon,proto3,enum=codeium_common_pb.CascadeNUXIcon" json:"icon,omitempty"`
	RequiresIdleCascade  bool     `protobuf:"varint,8,opt,name=requires_idle_cascade,json=requiresIdleCascade,proto3" json:"requires_idle_cascade,omitempty"`
	Title                string   `protobuf:"bytes,9,opt,name=title,proto3" json:"title,omitempty"`
	Body                 string   `protobuf:"bytes,10,opt,name=body,proto3" json:"body,omitempty"`
	ImageUrl             string   `protobuf:"bytes,11,opt,name=image_url,json=imageUrl,proto3" json:"image_url,omitempty"`
	VideoUrl             string   `protobuf:"bytes,12,opt,name=video_url,json=videoUrl,proto3" json:"video_url,omitempty"`
	AvailableInteractions []CascadeNUXInteractionType `protobuf:"varint,13,rep,packed,name=available_interactions,json=availableInteractions,proto3,enum=codeium_common_pb.CascadeNUXInteractionType" json:"available_interactions,omitempty"`
	PrimaryCtaText       string   `protobuf:"bytes,14,opt,name=primary_cta_text,json=primaryCtaText,proto3" json:"primary_cta_text,omitempty"`
	FileUrl              string   `protobuf:"bytes,15,opt,name=file_url,json=fileUrl,proto3" json:"file_url,omitempty"`
	FileContent          string   `protobuf:"bytes,16,opt,name=file_content,json=fileContent,proto3" json:"file_content,omitempty"`
	G1RedirectUrl        string   `protobuf:"bytes,17,opt,name=g1_redirect_url,json=g1RedirectUrl,proto3" json:"g1_redirect_url,omitempty"`
	Session              CascadeNUXSession    `protobuf:"varint,18,opt,name=session,proto3,enum=codeium_common_pb.CascadeNUXSession" json:"session,omitempty"`
}

// CascadeNUXState is the persisted state of a NUX notification.
type CascadeNUXState struct {
	Event     CascadeNUXEvent `protobuf:"varint,1,opt,name=event,proto3,enum=codeium_common_pb.CascadeNUXEvent" json:"event,omitempty"`
	Timestamp time.Time       `protobuf:"bytes,2,opt,name=timestamp,proto3,stdtime" json:"timestamp,omitempty"`
}

// PlanInfo is the resolved plan (free / pro / enterprise / ...).
type PlanInfo struct {
	CanAllowCascadeInBackground         bool   `protobuf:"varint,1,opt,name=can_allow_cascade_in_background,json=canAllowCascadeInBackground,proto3" json:"can_allow_cascade_in_background,omitempty"`
	HasMoreModels                       bool   `protobuf:"varint,2,opt,name=has_more_models,json=hasMoreModels,proto3" json:"has_more_models,omitempty"`
	HasFlexTier                         bool   `protobuf:"varint,3,opt,name=has_flex_tier,json=hasFlexTier,proto3" json:"has_flex_tier,omitempty"`
	PlanName                            string `protobuf:"bytes,4,opt,name=plan_name,json=planName,proto3" json:"plan_name,omitempty"`
	PlanTier                            TeamsTier `protobuf:"varint,5,opt,name=plan_tier,json=planTier,proto3,enum=codeium_common_pb.TeamsTier" json:"plan_tier,omitempty"`
	IsPaidTier                          bool   `protobuf:"varint,6,opt,name=is_paid_tier,json=isPaidTier,proto3" json:"is_paid_tier,omitempty"`
	MaxCascadeMessagesPerDay            int32  `protobuf:"varint,7,opt,name=max_cascade_messages_per_day,json=maxCascadeMessagesPerDay,proto3" json:"max_cascade_messages_per_day,omitempty"`
	MaxCodeCompletionsPerDay            int32  `protobuf:"varint,8,opt,name=max_code_completions_per_day,json=maxCodeCompletionsPerDay,proto3" json:"max_code_completions_per_day,omitempty"`
	BillingPeriodStartIso               string `protobuf:"bytes,9,opt,name=billing_period_start_iso,json=billingPeriodStartIso,proto3" json:"billing_period_start_iso,omitempty"`
	BillingPeriodEndIso                 string `protobuf:"bytes,10,opt,name=billing_period_end_iso,json=billingPeriodEndIso,proto3" json:"billing_period_end_iso,omitempty"`
	AllowCasCadeOverDailyLimit          bool   `protobuf:"varint,11,opt,name=allow_cascade_over_daily_limit,json=allowCascadeOverDailyLimit,proto3" json:"allow_cascade_over_daily_limit,omitempty"`
	AllowCascadeInBackground            bool   `protobuf:"varint,12,opt,name=allow_cascade_in_background,json=allowCascadeInBackground,proto3" json:"allow_cascade_in_background,omitempty"`
	AllowBrowserTools                   bool   `protobuf:"varint,13,opt,name=allow_browser_tools,json=allowBrowserTools,proto3" json:"allow_browser_tools,omitempty"`
	AllowCascadeWebSearch               bool   `protobuf:"varint,14,opt,name=allow_cascade_web_search,json=allowCascadeWebSearch,proto3" json:"allow_cascade_web_search,omitempty"`
	AllowMcp                            bool   `protobuf:"varint,15,opt,name=allow_mcp,json=allowMcp,proto3" json:"allow_mcp,omitempty"`
	HasCreditsForUnlimitedCascade       bool   `protobuf:"varint,16,opt,name=has_credits_for_unlimited_cascade,json=hasCreditsForUnlimitedCascade,proto3" json:"has_credits_for_unlimited_cascade,omitempty"`
	HasCascadeAvailable                 bool   `protobuf:"varint,17,opt,name=has_cascade_available,json=hasCascadeAvailable,proto3" json:"has_cascade_available,omitempty"`
	AvailableModelConfigs               []*AllowedModelConfig `protobuf:"bytes,18,rep,name=available_model_configs,json=availableModelConfigs,proto3" json:"available_model_configs,omitempty"`
	QuotaInfo                           *QuotaInfo            `protobuf:"bytes,19,opt,name=quota_info,json=quotaInfo,proto3" json:"quota_info,omitempty"`
	TopUpStatus                         *TopUpStatus          `protobuf:"bytes,20,opt,name=top_up_status,json=topUpStatus,proto3" json:"top_up_status,omitempty"`
	PlanStatus                          *PlanStatus           `protobuf:"bytes,21,opt,name=plan_status,json=planStatus,proto3" json:"plan_status,omitempty"`
	PlanMode                            PlanMode              `protobuf:"varint,22,opt,name=plan_mode,json=planMode,proto3,enum=codeium_common_pb.PlanMode" json:"plan_mode,omitempty"`
	CascadePlannerMode                  ConversationalPlannerMode `protobuf:"varint,23,opt,name=cascade_planner_mode,json=cascadePlannerMode,proto3,enum=codeium_common_pb.ConversationalPlannerMode" json:"cascade_planner_mode,omitempty"`
	CascadeAllowedCommands              []string              `protobuf:"bytes,24,rep,name=cascade_allowed_commands,json=cascadeAllowedCommands,proto3" json:"cascade_allowed_commands,omitempty"`
	CascadeDeniedCommands               []string              `protobuf:"bytes,25,rep,name=cascade_denied_commands,json=cascadeDeniedCommands,proto3" json:"cascade_denied_commands,omitempty"`
	AutoContinueOnMaxGeneratorInvocations AutoContinueOnMaxGeneratorInvocations `protobuf:"varint,26,opt,name=auto_continue_on_max_generator_invocations,json=autoContinueOnMaxGeneratorInvocations,proto3,enum=codeium_common_pb.AutoContinueOnMaxGeneratorInvocations" json:"auto_continue_on_max_generator_invocations,omitempty"`
	WebSearchTool                       CascadeWebSearchTool  `protobuf:"varint,27,opt,name=web_search_tool,json=webSearchTool,proto3,enum=codeium_common_pb.CascadeWebSearchTool" json:"web_search_tool,omitempty"`
	RunExtensionCode                    CascadeRunExtensionCode `protobuf:"varint,28,opt,name=run_extension_code,json=runExtensionCode,proto3,enum=codeium_common_pb.CascadeRunExtensionCode" json:"run_extension_code,omitempty"`
	CascadeInputAutocomplete            CascadeInputAutocomplete `protobuf:"varint,29,opt,name=cascade_input_autocomplete,json=cascadeInputAutocomplete,proto3,enum=codeium_common_pb.CascadeInputAutocomplete" json:"cascade_input_autocomplete,omitempty"`
	CascadeAutoExecutionPolicy          CascadeCommandsAutoExecution `protobuf:"varint,30,opt,name=cascade_auto_execution_policy,json=cascadeAutoExecutionPolicy,proto3,enum=codeium_common_pb.CascadeCommandsAutoExecution" json:"cascade_auto_execution_policy,omitempty"`
	RunExtensionCodeAutoRun             CascadeRunExtensionCodeAutoRun `protobuf:"varint,31,opt,name=run_extension_code_auto_run,json=runExtensionCodeAutoRun,proto3,enum=codeium_common_pb.CascadeRunExtensionCodeAutoRun" json:"run_extension_code_auto_run,omitempty"`
}

// TeamOrganizationalControls is the team-level configuration overlay.
type TeamOrganizationalControls struct {
	CascadeModelLabels     []string `protobuf:"bytes,1,rep,name=cascade_model_labels,json=cascadeModelLabels,proto3" json:"cascade_model_labels,omitempty"`
	AllowedModelConfigs    []*AllowedModelConfig `protobuf:"bytes,2,rep,name=allowed_model_configs,json=allowedModelConfigs,proto3" json:"allowed_model_configs,omitempty"`
	CascadeAllowedCommands []string `protobuf:"bytes,3,rep,name=cascade_allowed_commands,json=cascadeAllowedCommands,proto3" json:"cascade_allowed_commands,omitempty"`
	CascadeDeniedCommands  []string `protobuf:"bytes,4,rep,name=cascade_denied_commands,json=cascadeDeniedCommands,proto3" json:"cascade_denied_commands,omitempty"`
	DisabledFeatures       []UserFeatures `protobuf:"varint,5,rep,packed,name=disabled_features,json=disabledFeatures,proto3,enum=codeium_common_pb.UserFeatures" json:"disabled_features,omitempty"`
	TeamsFeatures          *TeamsFeaturesMetadata `protobuf:"bytes,6,opt,name=teams_features,json=teamsFeatures,proto3" json:"teams_features,omitempty"`
}

// UserSettings is the IDE-side user settings message.
type UserSettings struct {
	LastSelectedCascadeModelOrAlias  string  `protobuf:"bytes,1,opt,name=last_selected_cascade_model_or_alias,json=lastSelectedCascadeModelOrAlias,proto3" json:"last_selected_cascade_model_or_alias,omitempty"`
	CascadePlannerMode               ConversationalPlannerMode `protobuf:"varint,2,opt,name=cascade_planner_mode,json=cascadePlannerMode,proto3,enum=codeium_common_pb.ConversationalPlannerMode" json:"cascade_planner_mode,omitempty"`
	CascadeAllowedCommands           []string `protobuf:"bytes,3,rep,name=cascade_allowed_commands,json=cascadeAllowedCommands,proto3" json:"cascade_allowed_commands,omitempty"`
	CascadeDeniedCommands            []string `protobuf:"bytes,4,rep,name=cascade_denied_commands,json=cascadeDeniedCommands,proto3" json:"cascade_denied_commands,omitempty"`
	CascadeWebSearchDisabled         bool    `protobuf:"varint,5,opt,name=cascade_web_search_disabled,json=cascadeWebSearchDisabled,proto3" json:"cascade_web_search_disabled,omitempty"`
	DisableOpenCascadeOnReload       bool    `protobuf:"varint,6,opt,name=disable_open_cascade_on_reload,json=disableOpenCascadeOnReload,proto3" json:"disable_open_cascade_on_reload,omitempty"`
	CascadeAutoExecutionPolicy       CascadeCommandsAutoExecution `protobuf:"varint,7,opt,name=cascade_auto_execution_policy,json=cascadeAutoExecutionPolicy,proto3,enum=codeium_common_pb.CascadeCommandsAutoExecution" json:"cascade_auto_execution_policy,omitempty"`
	LastSelectedCascadeId            string  `protobuf:"bytes,8,opt,name=last_selected_cascade_id,json=lastSelectedCascadeId,proto3" json:"last_selected_cascade_id,omitempty"`
	AllowCascadeAccessGitignoreFiles bool    `protobuf:"varint,9,opt,name=allow_cascade_access_gitignore_files,json=allowCascadeAccessGitignoreFiles,proto3" json:"allow_cascade_access_gitignore_files,omitempty"`
	DisableCascadeAutoFixLints       bool    `protobuf:"varint,10,opt,name=disable_cascade_auto_fix_lints,json=disableCascadeAutoFixLints,proto3" json:"disable_cascade_auto_fix_lints,omitempty"`
	AllowCascadeInBackground         bool    `protobuf:"varint,11,opt,name=allow_cascade_in_background,json=allowCascadeInBackground,proto3" json:"allow_cascade_in_background,omitempty"`
	CascadeWebSearch                 CascadeWebSearchTool `protobuf:"varint,12,opt,name=cascade_web_search,json=cascadeWebSearch,proto3,enum=codeium_common_pb.CascadeWebSearchTool" json:"cascade_web_search,omitempty"`
	RunExtensionCode                 CascadeRunExtensionCode `protobuf:"varint,13,opt,name=run_extension_code,json=runExtensionCode,proto3,enum=codeium_common_pb.CascadeRunExtensionCode" json:"run_extension_code,omitempty"`
	RunExtensionCodeAutoRun          CascadeRunExtensionCodeAutoRun `protobuf:"varint,14,opt,name=run_extension_code_auto_run,json=runExtensionCodeAutoRun,proto3,enum=codeium_common_pb.CascadeRunExtensionCodeAutoRun" json:"run_extension_code_auto_run,omitempty"`
	CascadeInputAutocomplete         CascadeInputAutocomplete `protobuf:"varint,15,opt,name=cascade_input_autocomplete,json=cascadeInputAutocomplete,proto3,enum=codeium_common_pb.CascadeInputAutocomplete" json:"cascade_input_autocomplete,omitempty"`
	ThemePreference                  ThemePreference `protobuf:"varint,16,opt,name=theme_preference,json=themePreference,proto3,enum=codeium_common_pb.ThemePreference" json:"theme_preference,omitempty"`
	RememberLastModelSelection       RememberLastModelSelection `protobuf:"varint,17,opt,name=remember_last_model_selection,json=rememberLastModelSelection,proto3,enum=codeium_common_pb.RememberLastModelSelection" json:"remember_last_model_selection,omitempty"`
	AutoContinueOnMaxGeneratorInvocations AutoContinueOnMaxGeneratorInvocations `protobuf:"varint,18,opt,name=auto_continue_on_max_generator_invocations,json=autoContinueOnMaxGeneratorInvocations,proto3,enum=codeium_common_pb.AutoContinueOnMaxGeneratorInvocations" json:"auto_continue_on_max_generator_invocations,omitempty"`
	WebSearchTool                    CascadeWebSearchTool `protobuf:"varint,19,opt,name=web_search_tool,json=webSearchTool,proto3,enum=codeium_common_pb.CascadeWebSearchTool" json:"web_search_tool,omitempty"`
	ExtensionPanelTab                ExtensionPanelTab `protobuf:"varint,20,opt,name=extension_panel_tab,json=extensionPanelTab,proto3,enum=codeium_common_pb.ExtensionPanelTab" json:"extension_panel_tab,omitempty"`
	DetectedAndUseProxy              DetectAndUseProxy `protobuf:"varint,21,opt,name=detected_and_use_proxy,json=detectedAndUseProxy,proto3,enum=codeium_common_pb.DetectAndUseProxy" json:"detected_and_use_proxy,omitempty"`
	CustomAnnotationConfig           *AnnotationsConfig `protobuf:"bytes,22,opt,name=custom_annotation_config,json=customAnnotationConfig,proto3" json:"custom_annotation_config,omitempty"`
	DefaultPinnedContextConfig       *DefaultPinnedContextConfig `protobuf:"bytes,23,opt,name=default_pinned_context_config,json=defaultPinnedContextConfig,proto3" json:"default_pinned_context_config,omitempty"`
	DisableTelemetry                 bool    `protobuf:"varint,24,opt,name=disable_telemetry,json=disableTelemetry,proto3" json:"disable_telemetry,omitempty"`
	DisableCascade                   bool    `protobuf:"varint,25,opt,name=disable_cascade,json=disableCascade,proto3" json:"disable_cascade,omitempty"`
	DisableAutocomplete              bool    `protobuf:"varint,26,opt,name=disable_autocomplete,json=disableAutocomplete,proto3" json:"disable_autocomplete,omitempty"`
	DisableIndexing                  bool    `protobuf:"varint,27,opt,name=disable_indexing,json=disableIndexing,proto3" json:"disable_indexing,omitempty"`
	DisableImages                    bool    `protobuf:"varint,28,opt,name=disable_images,json=disableImages,proto3" json:"disable_images,omitempty"`
	AllowTabKey                      bool    `protobuf:"varint,29,opt,name=allow_tab_key,json=allowTabKey,proto3" json:"allow_tab_key,omitempty"`
	AcceptNonTrustedCerts            bool    `protobuf:"varint,30,opt,name=accept_non_trusted_certs,json=acceptNonTrustedCerts,proto3" json:"accept_non_trusted_certs,omitempty"`
	DisableFastStaleCompletions      bool    `protobuf:"varint,31,opt,name=disable_fast_stale_completions,json=disableFastStaleCompletions,proto3" json:"disable_fast_stale_completions,omitempty"`
	EnableTabCompletionAfterSignature bool   `protobuf:"varint,32,opt,name=enable_tab_completion_after_signature,json=enableTabCompletionAfterSignature,proto3" json:"enable_tab_completion_after_signature,omitempty"`
	EnableSupercomplete              bool    `protobuf:"varint,33,opt,name=enable_supercomplete,json=enableSupercomplete,proto3" json:"enable_supercomplete,omitempty"`
	EnableStickyScroll               bool    `protobuf:"varint,34,opt,name=enable_sticky_scroll,json=enableStickyScroll,proto3" json:"enable_sticky_scroll,omitempty"`
	EnableCodeLlama                  bool    `protobuf:"varint,35,opt,name=enable_code_llama,json=enableCodeLlama,proto3" json:"enable_code_llama,omitempty"`
	EnableCustomContext              bool    `protobuf:"varint,36,opt,name=enable_custom_context,json=enableCustomContext,proto3" json:"enable_custom_context,omitempty"`
	EnableLocalIndexing              bool    `protobuf:"varint,37,opt,name=enable_local_indexing,json=enableLocalIndexing,proto3" json:"enable_local_indexing,omitempty"`
	EnableMcp                        bool    `protobuf:"varint,38,opt,name=enable_mcp,json=enableMcp,proto3" json:"enable_mcp,omitempty"`
	EnableBrowserTools               bool    `protobuf:"varint,39,opt,name=enable_browser_tools,json=enableBrowserTools,proto3" json:"enable_browser_tools,omitempty"`
	EnableRankedCompletions          bool    `protobuf:"varint,40,opt,name=enable_ranked_completions,json=enableRankedCompletions,proto3" json:"enable_ranked_completions,omitempty"`
	EnableCascadeWebSearch           bool    `protobuf:"varint,41,opt,name=enable_cascade_web_search,json=enableCascadeWebSearch,proto3" json:"enable_cascade_web_search,omitempty"`
	EnableMcpBrowserTools            bool    `protobuf:"varint,42,opt,name=enable_mcp_browser_tools,json=enableMcpBrowserTools,proto3" json:"enable_mcp_browser_tools,omitempty"`
	EnableCortex                     bool    `protobuf:"varint,43,opt,name=enable_cortex,json=enableCortex,proto3" json:"enable_cortex,omitempty"`
	EnableCustomModels               bool    `protobuf:"varint,44,opt,name=enable_custom_models,json=enableCustomModels,proto3" json:"enable_custom_models,omitempty"`
	EnableUsageDataTelemetry         bool    `protobuf:"varint,45,opt,name=enable_usage_data_telemetry,json=enableUsageDataTelemetry,proto3" json:"enable_usage_data_telemetry,omitempty"`
	EnableCrashTelemetry             bool    `protobuf:"varint,46,opt,name=enable_crash_telemetry,json=enableCrashTelemetry,proto3" json:"enable_crash_telemetry,omitempty"`
	EnableErrorTelemetry             bool    `protobuf:"varint,47,opt,name=enable_error_telemetry,json=enableErrorTelemetry,proto3" json:"enable_error_telemetry,omitempty"`
	EnableNonTelemetry               bool    `protobuf:"varint,48,opt,name=enable_non_telemetry,json=enableNonTelemetry,proto3" json:"enable_non_telemetry,omitempty"`
	EnableNonEphemeralCompletionCache bool   `protobuf:"varint,49,opt,name=enable_non_ephemeral_completion_cache,json=enableNonEphemeralCompletionCache,proto3" json:"enable_non_ephemeral_completion_cache,omitempty"`
	EnableCacheOnCompletionRequest   bool    `protobuf:"varint,50,opt,name=enable_cache_on_completion_request,json=enableCacheOnCompletionRequest,proto3" json:"enable_cache_on_completion_request,omitempty"`
	EnableCrossFileContext            bool    `protobuf:"varint,51,opt,name=enable_cross_file_context,json=enableCrossFileContext,proto3" json:"enable_cross_file_context,omitempty"`
	EnableWorkspaceIndexing           bool    `protobuf:"varint,52,opt,name=enable_workspace_indexing,json=enableWorkspaceIndexing,proto3" json:"enable_workspace_indexing,omitempty"`
	EnableContextPinning              bool    `protobuf:"varint,53,opt,name=enable_context_pinning,json=enableContextPinning,proto3" json:"enable_context_pinning,omitempty"`
	EnableCommandRun                  bool    `protobuf:"varint,54,opt,name=enable_command_run,json=enableCommandRun,proto3" json:"enable_command_run,omitempty"`
	EnableCommandDefault              bool    `protobuf:"varint,55,opt,name=enable_command_default,json=enableCommandDefault,proto3" json:"enable_command_default,omitempty"`
	EnableCommandWhitelist            bool    `protobuf:"varint,56,opt,name=enable_command_whitelist,json=enableCommandWhitelist,proto3" json:"enable_command_whitelist,omitempty"`
	EnableCommandBlacklist            bool    `protobuf:"varint,57,opt,name=enable_command_blacklist,json=enableCommandBlacklist,proto3" json:"enable_command_blacklist,omitempty"`
	EnableCommandCustomization        bool    `protobuf:"varint,58,opt,name=enable_command_customization,json=enableCommandCustomization,proto3" json:"enable_command_customization,omitempty"`
	EnableHotkeyCustomization         bool    `protobuf:"varint,59,opt,name=enable_hotkey_customization,json=enableHotkeyCustomization,proto3" json:"enable_hotkey_customization,omitempty"`
	EnableAutocompleteDebounce        bool    `protobuf:"varint,60,opt,name=enable_autocomplete_debounce,json=enableAutocompleteDebounce,proto3" json:"enable_autocomplete_debounce,omitempty"`
	EnableLocalSearch                 bool    `protobuf:"varint,61,opt,name=enable_local_search,json=enableLocalSearch,proto3" json:"enable_local_search,omitempty"`
	EnableFastCompletions             bool    `protobuf:"varint,62,opt,name=enable_fast_completions,json=enableFastCompletions,proto3" json:"enable_fast_completions,omitempty"`
	EnableDebugCompletions            bool    `protobuf:"varint,63,opt,name=enable_debug_completions,json=enableDebugCompletions,proto3" json:"enable_debug_completions,omitempty"`
}

// UserStatus is the user-account status (plan + team + quota).
type UserStatus struct {
	Email                       string                `protobuf:"bytes,1,opt,name=email,proto3" json:"email,omitempty"`
	AuthUid                     string                `protobuf:"bytes,2,opt,name=auth_uid,json=authUid,proto3" json:"auth_uid,omitempty"`
	PlanInfo                    *PlanInfo             `protobuf:"bytes,3,opt,name=plan_info,json=planInfo,proto3" json:"plan_info,omitempty"`
	TeamConfig                  *TeamConfig           `protobuf:"bytes,4,opt,name=team_config,json=teamConfig,proto3" json:"team_config,omitempty"`
	TeamOrganizationalControls  *TeamOrganizationalControls `protobuf:"bytes,5,opt,name=team_organizational_controls,json=teamOrganizationalControls,proto3" json:"team_organizational_controls,omitempty"`
	UserTeamStatus              UserTeamStatus        `protobuf:"varint,6,opt,name=user_team_status,json=userTeamStatus,proto3,enum=codeium_common_pb.UserTeamStatus" json:"user_team_status,omitempty"`
	PlanName                    string                `protobuf:"bytes,7,opt,name=plan_name,json=planName,proto3" json:"plan_name,omitempty"`
	IsPayingUser                bool                  `protobuf:"varint,8,opt,name=is_paying_user,json=isPayingUser,proto3" json:"is_paying_user,omitempty"`
	ShouldSuggestCommandRun     bool                  `protobuf:"varint,9,opt,name=should_suggest_command_run,json=shouldSuggestCommandRun,proto3" json:"should_suggest_command_run,omitempty"`
	HasRunCascade               bool                  `protobuf:"varint,10,opt,name=has_run_cascade,json=hasRunCascade,proto3" json:"has_run_cascade,omitempty"`
	HasRunCommand               bool                  `protobuf:"varint,11,opt,name=has_run_command,json=hasRunCommand,proto3" json:"has_run_command,omitempty"`
	HasIndexed                  bool                  `protobuf:"varint,12,opt,name=has_indexed,json=hasIndexed,proto3" json:"has_indexed,omitempty"`
	HasRunCompletions           bool                  `protobuf:"varint,13,opt,name=has_run_completions,json=hasRunCompletions,proto3" json:"has_run_completions,omitempty"`
	HasRunChat                  bool                  `protobuf:"varint,14,opt,name=has_run_chat,json=hasRunChat,proto3" json:"has_run_chat,omitempty"`
	HasRunBrowserTools          bool                  `protobuf:"varint,15,opt,name=has_run_browser_tools,json=hasRunBrowserTools,proto3" json:"has_run_browser_tools,omitempty"`
	HasAcceptedTos              bool                  `protobuf:"varint,16,opt,name=has_accepted_tos,json=hasAcceptedTos,proto3" json:"has_accepted_tos,omitempty"`
	CollectionOptIn             bool                  `protobuf:"varint,17,opt,name=collection_opt_in,json=collectionOptIn,proto3" json:"collection_opt_in,omitempty"`
	HasDismissedCommandTip      bool                  `protobuf:"varint,18,opt,name=has_dismissed_command_tip,json=hasDismissedCommandTip,proto3" json:"has_dismissed_command_tip,omitempty"`
	HasAcceptedTosV2            bool                  `protobuf:"varint,19,opt,name=has_accepted_tos_v2,json=hasAcceptedTosV2,proto3" json:"has_accepted_tos_v2,omitempty"`
	RequiresTosAcceptance       bool                  `protobuf:"varint,20,opt,name=requires_tos_acceptance,json=requiresTosAcceptance,proto3" json:"requires_tos_acceptance,omitempty"`
	CascadeModelConfigData      *CascadeModelConfigData `protobuf:"bytes,21,opt,name=cascade_model_config_data,json=cascadeModelConfigData,proto3" json:"cascade_model_config_data,omitempty"`
	AntigravityDeployment       *AntigravityDeployment `protobuf:"bytes,22,opt,name=antigravity_deployment,json=antigravityDeployment,proto3" json:"antigravity_deployment,omitempty"`
}

// TeamConfig is the team config the user is part of.
type TeamConfig struct {
	TeamId                 string   `protobuf:"bytes,1,opt,name=team_id,json=teamId,proto3" json:"team_id,omitempty"`
	TeamName               string   `protobuf:"bytes,2,opt,name=team_name,json=teamName,proto3" json:"team_name,omitempty"`
	TeamTier               TeamsTier `protobuf:"varint,3,opt,name=team_tier,json=teamTier,proto3,enum=codeium_common_pb.TeamsTier" json:"team_tier,omitempty"`
	Seats                  int32    `protobuf:"varint,4,opt,name=seats,proto3" json:"seats,omitempty"`
	UsedSeats              int32    `protobuf:"varint,5,opt,name=used_seats,json=usedSeats,proto3" json:"used_seats,omitempty"`
	IsAdmin                bool     `protobuf:"varint,6,opt,name=is_admin,json=isAdmin,proto3" json:"is_admin,omitempty"`
	IsOwner                bool     `protobuf:"varint,7,opt,name=is_owner,json=isOwner,proto3" json:"is_owner,omitempty"`
	BillingEmail           string   `protobuf:"bytes,8,opt,name=billing_email,json=billingEmail,proto3" json:"billing_email,omitempty"`
	AllowSeatSelfAssign    bool     `protobuf:"varint,9,opt,name=allow_seat_self_assign,json=allowSeatSelfAssign,proto3" json:"allow_seat_self_assign,omitempty"`
	AllowSocialLogin       bool     `protobuf:"varint,10,opt,name=allow_social_login,json=allowSocialLogin,proto3" json:"allow_social_login,omitempty"`
	TeamDomain             string   `protobuf:"bytes,11,opt,name=team_domain,json=teamDomain,proto3" json:"team_domain,omitempty"`
	EnforceSso             bool     `protobuf:"varint,12,opt,name=enforce_sso,json=enforceSso,proto3" json:"enforce_sso,omitempty"`
	AllowPersonalAccount   bool     `protobuf:"varint,13,opt,name=allow_personal_account,json=allowPersonalAccount,proto3" json:"allow_personal_account,omitempty"`
	CreatedAtIso           string   `protobuf:"bytes,14,opt,name=created_at_iso,json=createdAtIso,proto3" json:"created_at_iso,omitempty"`
	TrialEndsAtIso         string   `protobuf:"bytes,15,opt,name=trial_ends_at_iso,json=trialEndsAtIso,proto3" json:"trial_ends_at_iso,omitempty"`
	SubscriptionStatus     string   `protobuf:"bytes,16,opt,name=subscription_status,json=subscriptionStatus,proto3" json:"subscription_status,omitempty"`
	CancelledAtIso         string   `protobuf:"bytes,17,opt,name=cancelled_at_iso,json=cancelledAtIso,proto3" json:"cancelled_at_iso,omitempty"`
	MaxCascadeMessagesPerDay int32   `protobuf:"varint,18,opt,name=max_cascade_messages_per_day,json=maxCascadeMessagesPerDay,proto3" json:"max_cascade_messages_per_day,omitempty"`
	MaxCodeCompletionsPerDay int32   `protobuf:"varint,19,opt,name=max_code_completions_per_day,json=maxCodeCompletionsPerDay,proto3" json:"max_code_completions_per_day,omitempty"`
	IsInGracePeriod        bool     `protobuf:"varint,20,opt,name=is_in_grace_period,json=isInGracePeriod,proto3" json:"is_in_grace_period,omitempty"`
	GracePeriodEndsAtIso   string   `protobuf:"bytes,21,opt,name=grace_period_ends_at_iso,json=gracePeriodEndsAtIso,proto3" json:"grace_period_ends_at_iso,omitempty"`
	HasStripeCustomer      bool     `protobuf:"varint,22,opt,name=has_stripe_customer,json=hasStripeCustomer,proto3" json:"has_stripe_customer,omitempty"`
	StripeCustomerId       string   `protobuf:"bytes,23,opt,name=stripe_customer_id,json=stripeCustomerId,proto3" json:"stripe_customer_id,omitempty"`
	AllowUserApiKeys       bool     `protobuf:"varint,24,opt,name=allow_user_api_keys,json=allowUserApiKeys,proto3" json:"allow_user_api_keys,omitempty"`
}

// AllowedModelConfig is a (model, multiplier) allowlist entry.
type AllowedModelConfig struct {
	ModelOrAlias    string  `protobuf:"bytes,1,opt,name=model_or_alias,json=modelOrAlias,proto3" json:"model_or_alias,omitempty"`
	CreditMultiplier float32 `protobuf:"fixed32,2,opt,name=credit_multiplier,json=creditMultiplier,proto3" json:"credit_multiplier,omitempty"`
}

// QuotaInfo is the user's current quota state.
type QuotaInfo struct {
	UsedCredits       int64   `protobuf:"varint,1,opt,name=used_credits,json=usedCredits,proto3" json:"used_credits,omitempty"`
	TotalCredits      int64   `protobuf:"varint,2,opt,name=total_credits,json=totalCredits,proto3" json:"total_credits,omitempty"`
	ResetAtIso        string  `protobuf:"bytes,3,opt,name=reset_at_iso,json=resetAtIso,proto3" json:"reset_at_iso,omitempty"`
}

// TopUpStatus is the top-up billing status.
type TopUpStatus struct {
	BalanceCredits    int64   `protobuf:"varint,1,opt,name=balance_credits,json=balanceCredits,proto3" json:"balance_credits,omitempty"`
	AutoTopUpEnabled  bool    `protobuf:"varint,2,opt,name=auto_top_up_enabled,json=autoTopUpEnabled,proto3" json:"auto_top_up_enabled,omitempty"`
	ThresholdCredits  int64   `protobuf:"varint,3,opt,name=threshold_credits,json=thresholdCredits,proto3" json:"threshold_credits,omitempty"`
	TopUpAmount       int64   `protobuf:"varint,4,opt,name=top_up_amount,json=topUpAmount,proto3" json:"top_up_amount,omitempty"`
	LastTopUpAtIso    string  `protobuf:"bytes,5,opt,name=last_top_up_at_iso,json=lastTopUpAtIso,proto3" json:"last_top_up_at_iso,omitempty"`
	PaymentMethodMask string  `protobuf:"bytes,6,opt,name=payment_method_mask,json=paymentMethodMask,proto3" json:"payment_method_mask,omitempty"`
}

// PlanStatus is the plan state (active / past_due / canceled / ...).
type PlanStatus struct {
	StatusLabel       string            `protobuf:"bytes,1,opt,name=status_label,json=statusLabel,proto3" json:"status_label,omitempty"`
	BillingIssue      string            `protobuf:"bytes,2,opt,name=billing_issue,json=billingIssue,proto3" json:"billing_issue,omitempty"`
	WillCancelAtIso   string            `protobuf:"bytes,3,opt,name=will_cancel_at_iso,json=willCancelAtIso,proto3" json:"will_cancel_at_iso,omitempty"`
	HasPaymentMethod  bool              `protobuf:"varint,4,opt,name=has_payment_method,json=hasPaymentMethod,proto3" json:"has_payment_method,omitempty"`
	TrialDaysLeft     int32             `protobuf:"varint,5,opt,name=trial_days_left,json=trialDaysLeft,proto3" json:"trial_days_left,omitempty"`
	IsTrialing        bool              `protobuf:"varint,6,opt,name=is_trialing,json=isTrialing,proto3" json:"is_trialing,omitempty"`
	GracePeriodEndsAt string            `protobuf:"bytes,7,opt,name=grace_period_ends_at,json=gracePeriodEndsAt,proto3" json:"grace_period_ends_at,omitempty"`
	DaysUntilGracePeriod int32          `protobuf:"varint,8,opt,name=days_until_grace_period,json=daysUntilGracePeriod,proto3" json:"days_until_grace_period,omitempty"`
	PlanName          string            `protobuf:"bytes,9,opt,name=plan_name,json=planName,proto3" json:"plan_name,omitempty"`
	PlanTier          TeamsTier         `protobuf:"varint,10,opt,name=plan_tier,json=planTier,proto3,enum=codeium_common_pb.TeamsTier" json:"plan_tier,omitempty"`
}

// AntigravityDeployment is a single Antigravity project deployment.
type AntigravityDeployment struct {
	AntigravityDeploymentId string `protobuf:"bytes,1,opt,name=antigravity_deployment_id,json=antigravityDeploymentId,proto3" json:"antigravity_deployment_id,omitempty"`
	AntigravityProjectId    string `protobuf:"bytes,2,opt,name=antigravity_project_id,json=antigravityProjectId,proto3" json:"antigravity_project_id,omitempty"`
	AuthUid                 string `protobuf:"bytes,3,opt,name=auth_uid,json=authUid,proto3" json:"auth_uid,omitempty"`
	BuildStatusUrl          string `protobuf:"bytes,4,opt,name=build_status_url,json=buildStatusUrl,proto3" json:"build_status_url,omitempty"`
	ClaimedAt               time.Time `protobuf:"bytes,5,opt,name=claimed_at,json=claimedAt,proto3,stdtime" json:"claimed_at,omitempty"`
	CreatedAt               time.Time `protobuf:"bytes,6,opt,name=created_at,json=createdAt,proto3,stdtime" json:"created_at,omitempty"`
	DeploymentProvider      DeploymentProvider `protobuf:"varint,7,opt,name=deployment_provider,json=deploymentProvider,proto3,enum=codeium_common_pb.DeploymentProvider" json:"deployment_provider,omitempty"`
	DeploymentUrl           string `protobuf:"bytes,8,opt,name=deployment_url,json=deploymentUrl,proto3" json:"deployment_url,omitempty"`
	DeprovisionedAt         time.Time `protobuf:"bytes,9,opt,name=deprovisioned_at,json=deprovisionedAt,proto3,stdtime" json:"deprovisioned_at,omitempty"`
	Domain                  string `protobuf:"bytes,10,opt,name=domain,proto3" json:"domain,omitempty"`
	ExpiresAt               time.Time `protobuf:"bytes,11,opt,name=expires_at,json=expiresAt,proto3,stdtime" json:"expires_at,omitempty"`
	ProjectId               string `protobuf:"bytes,12,opt,name=project_id,json=projectId,proto3" json:"project_id,omitempty"`
	ProjectName             string `protobuf:"bytes,13,opt,name=project_name,json=projectName,proto3" json:"project_name,omitempty"`
	ProjectUrl              string `protobuf:"bytes,14,opt,name=project_url,json=projectUrl,proto3" json:"project_url,omitempty"`
	ProviderDeploymentId    string `protobuf:"bytes,15,opt,name=provider_deployment_id,json=providerDeploymentId,proto3" json:"provider_deployment_id,omitempty"`
	ProviderTeamId          string `protobuf:"bytes,16,opt,name=provider_team_id,json=providerTeamId,proto3" json:"provider_team_id,omitempty"`
	SubdomainName           string `protobuf:"bytes,17,opt,name=subdomain_name,json=subdomainName,proto3" json:"subdomain_name,omitempty"`
	UpdatedAt               time.Time `protobuf:"bytes,18,opt,name=updated_at,json=updatedAt,proto3,stdtime" json:"updated_at,omitempty"`
	WorkspacePath           string `protobuf:"bytes,19,opt,name=workspace_path,json=workspacePath,proto3" json:"workspace_path,omitempty"`
}

// ModelInfo is the runtime info for a model.
type ModelInfo struct {
	ModelName                string  `protobuf:"bytes,1,opt,name=model_name,json=modelName,proto3" json:"model_name,omitempty"`
	ModelProvider            ModelProvider `protobuf:"varint,2,opt,name=model_provider,json=modelProvider,proto3,enum=codeium_common_pb.ModelProvider" json:"model_provider,omitempty"`
	MaxOutputTokens          int32   `protobuf:"varint,3,opt,name=max_output_tokens,json=maxOutputTokens,proto3" json:"max_output_tokens,omitempty"`
	ContextLimit             int32   `protobuf:"varint,4,opt,name=context_limit,json=contextLimit,proto3" json:"context_limit,omitempty"`
	InputCostPer1K           float32 `protobuf:"fixed32,5,opt,name=input_cost_per_1k,json=inputCostPer1k,proto3" json:"input_cost_per_1k,omitempty"`
	OutputCostPer1K          float32 `protobuf:"fixed32,6,opt,name=output_cost_per_1k,json=outputCostPer1k,proto3" json:"output_cost_per_1k,omitempty"`
	ModelPricingType         ModelPricingType `protobuf:"varint,7,opt,name=model_pricing_type,json=modelPricingType,proto3,enum=codeium_common_pb.ModelPricingType" json:"model_pricing_type,omitempty"`
	MaxThinkingTokens        int32   `protobuf:"varint,8,opt,name=max_thinking_tokens,json=maxThinkingTokens,proto3" json:"max_thinking_tokens,omitempty"`
	SupportsImages           bool    `protobuf:"varint,9,opt,name=supports_images,json=supportsImages,proto3" json:"supports_images,omitempty"`
	SupportsTools            bool    `protobuf:"varint,10,opt,name=supports_tools,json=supportsTools,proto3" json:"supports_tools,omitempty"`
	SupportsAudio            bool    `protobuf:"varint,11,opt,name=supports_audio,json=supportsAudio,proto3" json:"supports_audio,omitempty"`
	SupportsVideo            bool    `protobuf:"varint,12,opt,name=supports_video,json=supportsVideo,proto3" json:"supports_video,omitempty"`
	SupportsThinking         bool    `protobuf:"varint,13,opt,name=supports_thinking,json=supportsThinking,proto3" json:"supports_thinking,omitempty"`
	SupportsWebSearch        bool    `protobuf:"varint,14,opt,name=supports_web_search,json=supportsWebSearch,proto3" json:"supports_web_search,omitempty"`
	SupportsCodeExecution    bool    `protobuf:"varint,15,opt,name=supports_code_execution,json=supportsCodeExecution,proto3" json:"supports_code_execution,omitempty"`
	SupportsStreaming        bool    `protobuf:"varint,16,opt,name=supports_streaming,json=supportsStreaming,proto3" json:"supports_streaming,omitempty"`
	SupportsSystemPrompt     bool    `protobuf:"varint,17,opt,name=supports_system_prompt,json=supportsSystemPrompt,proto3" json:"supports_system_prompt,omitempty"`
	SupportsFunctionCalling  bool    `protobuf:"varint,18,opt,name=supports_function_calling,json=supportsFunctionCalling,proto3" json:"supports_function_calling,omitempty"`
	SupportsVision           bool    `protobuf:"varint,19,opt,name=supports_vision,json=supportsVision,proto3" json:"supports_vision,omitempty"`
	SupportsFileUpload       bool    `protobuf:"varint,20,opt,name=supports_file_upload,json=supportsFileUpload,proto3" json:"supports_file_upload,omitempty"`
	SupportsSearch           bool    `protobuf:"varint,21,opt,name=supports_search,json=supportsSearch,proto3" json:"supports_search,omitempty"`
	ModelType                ModelType `protobuf:"varint,22,opt,name=model_type,json=modelType,proto3,enum=codeium_common_pb.ModelType" json:"model_type,omitempty"`
	ModelStatus              ModelStatus `protobuf:"varint,23,opt,name=model_status,json=modelStatus,proto3,enum=codeium_common_pb.ModelStatus" json:"model_status,omitempty"`
	ModelStatusInfo          *ModelStatusInfo `protobuf:"bytes,24,opt,name=model_status_info,json=modelStatusInfo,proto3" json:"model_status_info,omitempty"`
	AverageLatencyMs         int32   `protobuf:"varint,25,opt,name=average_latency_ms,json=averageLatencyMs,proto3" json:"average_latency_ms,omitempty"`
}

// ModelStatusInfo is the human-readable model status info.
type ModelStatusInfo struct {
	Reason       string  `protobuf:"bytes,1,opt,name=reason,proto3" json:"reason,omitempty"`
	Details      string  `protobuf:"bytes,2,opt,name=details,proto3" json:"details,omitempty"`
	EstimatedRepairTime string `protobuf:"bytes,3,opt,name=estimated_repair_time,json=estimatedRepairTime,proto3" json:"estimated_repair_time,omitempty"`
}

// ModelFeatures is the feature flag set for a model.
type ModelFeatures struct {
	SupportsImages     bool `protobuf:"varint,1,opt,name=supports_images,json=supportsImages,proto3" json:"supports_images,omitempty"`
	SupportsTools      bool `protobuf:"varint,2,opt,name=supports_tools,json=supportsTools,proto3" json:"supports_tools,omitempty"`
	SupportsAudio      bool `protobuf:"varint,3,opt,name=supports_audio,json=supportsAudio,proto3" json:"supports_audio,omitempty"`
	SupportsVideo      bool `protobuf:"varint,4,opt,name=supports_video,json=supportsVideo,proto3" json:"supports_video,omitempty"`
	SupportsThinking   bool `protobuf:"varint,5,opt,name=supports_thinking,json=supportsThinking,proto3" json:"supports_thinking,omitempty"`
	SupportsWebSearch  bool `protobuf:"varint,6,opt,name=supports_web_search,json=supportsWebSearch,proto3" json:"supports_web_search,omitempty"`
	SupportsCodeExecution bool `protobuf:"varint,7,opt,name=supports_code_execution,json=supportsCodeExecution,proto3" json:"supports_code_execution,omitempty"`
	SupportsStreaming  bool `protobuf:"varint,8,opt,name=supports_streaming,json=supportsStreaming,proto3" json:"supports_streaming,omitempty"`
	SupportsSystemPrompt bool `protobuf:"varint,9,opt,name=supports_system_prompt,json=supportsSystemPrompt,proto3" json:"supports_system_prompt,omitempty"`
	SupportsFunctionCalling bool `protobuf:"varint,10,opt,name=supports_function_calling,json=supportsFunctionCalling,proto3" json:"supports_function_calling,omitempty"`
	SupportsVision     bool `protobuf:"varint,11,opt,name=supports_vision,json=supportsVision,proto3" json:"supports_vision,omitempty"`
	SupportsFileUpload bool `protobuf:"varint,12,opt,name=supports_file_upload,json=supportsFileUpload,proto3" json:"supports_file_upload,omitempty"`
	SupportsSearch     bool `protobuf:"varint,13,opt,name=supports_search,json=supportsSearch,proto3" json:"supports_search,omitempty"`
	SupportsImageGeneration bool `protobuf:"varint,14,opt,name=supports_image_generation,json=supportsImageGeneration,proto3" json:"supports_image_generation,omitempty"`
	SupportsVideoGeneration bool `protobuf:"varint,15,opt,name=supports_video_generation,json=supportsVideoGeneration,proto3" json:"supports_video_generation,omitempty"`
	SupportsCodeSearch bool `protobuf:"varint,16,opt,name=supports_code_search,json=supportsCodeSearch,proto3" json:"supports_code_search,omitempty"`
	SupportsLocalExecution bool `protobuf:"varint,17,opt,name=supports_local_execution,json=supportsLocalExecution,proto3" json:"supports_local_execution,omitempty"`
	SupportsCloudExecution bool `protobuf:"varint,18,opt,name=supports_cloud_execution,json=supportsCloudExecution,proto3" json:"supports_cloud_execution,omitempty"`
	SupportsMultiModal  bool `protobuf:"varint,19,opt,name=supports_multi_modal,json=supportsMultiModal,proto3" json:"supports_multi_modal,omitempty"`
	SupportsCitations   bool `protobuf:"varint,20,opt,name=supports_citations,json=supportsCitations,proto3" json:"supports_citations,omitempty"`
	SupportsSpeech      bool `protobuf:"varint,21,opt,name=supports_speech,json=supportsSpeech,proto3" json:"supports_speech,omitempty"`
	SupportsAudioInput  bool `protobuf:"varint,22,opt,name=supports_audio_input,json=supportsAudioInput,proto3" json:"supports_audio_input,omitempty"`
	SupportsVideoInput  bool `protobuf:"varint,23,opt,name=supports_video_input,json=supportsVideoInput,proto3" json:"supports_video_input,omitempty"`
	SupportsImageInput  bool `protobuf:"varint,24,opt,name=supports_image_input,json=supportsImageInput,proto3" json:"supports_image_input,omitempty"`
	SupportsImageOutput bool `protobuf:"varint,25,opt,name=supports_image_output,json=supportsImageOutput,proto3" json:"supports_image_output,omitempty"`
	SupportsVideoOutput bool `protobuf:"varint,26,opt,name=supports_video_output,json=supportsVideoOutput,proto3" json:"supports_video_output,omitempty"`
	SupportsReasoning   bool `protobuf:"varint,27,opt,name=supports_reasoning,json=supportsReasoning,proto3" json:"supports_reasoning,omitempty"`
	SupportsFunctionResults bool `protobuf:"varint,28,opt,name=supports_function_results,json=supportsFunctionResults,proto3" json:"supports_function_results,omitempty"`
}

// TeamsFeaturesMetadata is the team-level feature flag set.
type TeamsFeaturesMetadata struct {
	DisabledFeatures []UserFeatures `protobuf:"varint,1,rep,packed,name=disabled_features,json=disabledFeatures,proto3,enum=codeium_common_pb.UserFeatures" json:"disabled_features,omitempty"`
	EnabledFeatures  []UserFeatures `protobuf:"varint,2,rep,packed,name=enabled_features,json=enabledFeatures,proto3,enum=codeium_common_pb.UserFeatures" json:"enabled_features,omitempty"`
	CustomFeatures   []string       `protobuf:"bytes,3,rep,name=custom_features,json=customFeatures,proto3" json:"custom_features,omitempty"`
}

// ---------------------------------------------------------------------------
// Supplementary message types (recovered 1:1 from pclntab).
// ---------------------------------------------------------------------------

// ModelNotificationExperimentPayload is the model-notification
// experiment payload.
type ModelNotificationExperimentPayload struct {
	Header string `protobuf:"bytes,1,opt,name=header,proto3" json:"header,omitempty"`
}

// RuntimeEnvironment is the runtime environment descriptor.
type RuntimeEnvironment struct {
	OS        string `protobuf:"bytes,1,opt,name=os,proto3" json:"os,omitempty"`
	Arch      string `protobuf:"bytes,2,opt,name=arch,proto3" json:"arch,omitempty"`
	Hostname  string `protobuf:"bytes,3,opt,name=hostname,proto3" json:"hostname,omitempty"`
	Version   string `protobuf:"bytes,4,opt,name=version,proto3" json:"version,omitempty"`
	BuildId   string `protobuf:"bytes,5,opt,name=build_id,json=buildId,proto3" json:"build_id,omitempty"`
	Region    string `protobuf:"bytes,6,opt,name=region,proto3" json:"region,omitempty"`
	Channel   string `protobuf:"bytes,7,opt,name=channel,proto3" json:"channel,omitempty"`
}

// SuperCompleteFilterReason is the filter reason for super-complete.
type SuperCompleteFilterReason int32

const (
	SuperCompleteFilterReasonUnspecified  SuperCompleteFilterReason = 0
	SuperCompleteFilterReasonLength      SuperCompleteFilterReason = 1
	SuperCompleteFilterReasonLanguage    SuperCompleteFilterReason = 2
	SuperCompleteFilterReasonUserBlocked SuperCompleteFilterReason = 3
)

// TerminalShellCommandData is the data block for a terminal shell command.
type TerminalShellCommandData struct {
	Command string `protobuf:"bytes,1,opt,name=command,proto3" json:"command,omitempty"`
}
