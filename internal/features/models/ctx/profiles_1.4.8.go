// profiles_1.4.8.go — the reference's own cascade catalogue.
//
// 【一手】 agy 1.4.8 embeds a JSON document — model_profiles.json, 57 984
// bytes at file offset 0x9ab160a of the Mach-O — that carries the
// reference's per-model cascade configuration:
//
//   - model_profiles: one profile per model family, each with a
//     CASCADE_USE_EXPERIMENT_CHECKPOINTER payload;
//   - models: the alias map that folds every model id onto one profile;
//   - base: the global settings, including the "checkpoint-variant" the
//     CLI inherits.
//
// The document is committed verbatim at
// testdata/model_profiles_1.4.8.json, and profiles_1.4.8_test.go checks
// every number below against it. The 1.4.7 document stays on disk as the
// previous release's evidence; profiles_1.4.8_test.go pins what moved
// between the two.
//
// # Why this replaces the 1.4.4 server capture
//
// The clone used to read the numbers from a live probe of
// fetchAvailableModels (1.4.4, committed at
// docs/cycles/model-catalogue-argon-gate/fetchAvailableModels_1.4.4.json).
// That capture is a *server* answer; this document is what the binary
// itself ships, and it carries the fields the capture never had: the
// strategy, the three snapshot switches, the session-summary override,
// the retry policy and the variant payloads.
//
// The two agree on every number the clone uses — the 1.4.4 probe already
// carried the same checkpoint payloads for the models it advertised — so
// this cycle changes no window. What it adds is the mechanism the
// capture could not show: the variant (a per-profile payload that raises
// MODEL_PLACEHOLDER_M400's window to 840 000 tokens), the strategy, and
// the fields the validator below reads.
//
// # The window
//
// 【反编译】 cortex/chatconverters.(*TrajectoryChatConverter).TrajectoryToChatMessages
// computes the conversion limit as
//
//	limit = model.input_token_limit
//	if checkpointConfig.enabled && checkpointConfig.max_token_limit != 0:
//	    limit = min(limit, checkpointConfig.max_token_limit)
//
// so the effective window of a model is the smaller of its own input
// limit and the profile's max_token_limit. The reserve the clone holds
// back is the profile's max_output_tokens.
//
// # What is *not* here
//
// The reference's cascade has more than this: the strategy switch
// (SAME_MODEL summarises with the session's model, SINGLE_PROMPT with
// checkpoint_model), the retry policy, the summary cache, the
// `shouldCheckpoint` predicate and the fourth resume section ("#
// Conversation Logs"). The clone's compaction is PiG's, not a cascade, so
// those stay out of scope for now — see
// docs/cycles/cycle-303-cascade-model-profiles/deliverable.md.
package ctx

// CheckpointVariant is the reference's base checkpoint variant — the
// "checkpoint-variant" setting of the embedded document's base block.
//
// 【反编译】 config.selectCheckpointExperimentKey builds the lookup key
// "<experiment>/<variant>" from the variant name and the experiment key,
// so the variant is a *per-profile* payload: the reference asks the
// profile for "<experiment>/claude-1m_840k" and falls back to the plain
// payload when the profile has none. One profile ships one —
// MODEL_PLACEHOLDER_M400, whose variant payload raises the window to
// 840 000 tokens and turns on use_model_reported_token_count.
//
// The IDE and SDK sub-clients set "checkpoint-variant": null, so the CLI
// inherits the base block's value.
const CheckpointVariant = "claude-1m_840k"

// The checkpoint strategies, as the payloads spell them.
//
// 【一手】 exa.cortex_pb.CheckpointStrategy = UNSPECIFIED 0,
// SAME_MODEL 1, SINGLE_PROMPT 2.
const (
	StrategyUnspecified  = "CHECKPOINT_STRATEGY_UNSPECIFIED"
	StrategySameModel    = "CHECKPOINT_STRATEGY_SAME_MODEL"
	StrategySinglePrompt = "CHECKPOINT_STRATEGY_SINGLE_PROMPT"
)

// Profile is one cascade profile: the CASCADE_USE_EXPERIMENT_CHECKPOINTER
// payload the reference ships for a model family.
//
// The field names are the payload's own keys.
type Profile struct {
	// Strategy is the checkpoint strategy: SAME_MODEL summarises with the
	// session's model, SINGLE_PROMPT with CheckpointModel.
	Strategy string
	// MaxTokenLimit is the checkpoint's own context limit. The effective
	// window is the smaller of this and the model's input limit.
	MaxTokenLimit int
	// MaxOutputTokens is the output cap the checkpoint holds back.
	MaxOutputTokens int
	// TokenThreshold is the checkpoint's own threshold.
	TokenThreshold int
	// MaxOverheadRatio is the overhead budget the validator checks.
	MaxOverheadRatio float64
	// Enabled is the checkpoint switch.
	Enabled bool
	// CheckpointModel is the model the SINGLE_PROMPT strategy summarises
	// with.
	CheckpointModel string
	// UseLastPlannerModel summarises with the planner's model.
	UseLastPlannerModel bool
	// IsSync makes the checkpoint synchronous.
	IsSync bool
	// MaxUserRequests caps how many user requests the summary carries.
	MaxUserRequests int
	// IncludeLastUserMessage keeps the last user message in the summary.
	IncludeLastUserMessage bool
	// IncludeConversationLog adds the "# Conversation Logs" section to
	// the resume block.
	IncludeConversationLog bool
	// IncludeRunningTaskSnaps, IncludeSubagentSnapshots and
	// IncludeArtifactSnapshots add the corresponding snapshot sections.
	IncludeRunningTaskSnaps  bool
	IncludeSubagentSnapshots bool
	IncludeArtifactSnapshots bool
	// RetryMaxRetries is the checkpoint's own retry policy.
	RetryMaxRetries int
	// UseModelReportedTokenCount makes the checkpoint trust the model's
	// reported token count instead of estimating it.
	UseModelReportedTokenCount bool
	// SessionSummaryPrompt records whether the profile replaces the
	// built-in session-summary instruction. The text itself is the
	// compaction instruction, not a cascade knob, so it lives in
	// internal/compaction — at 1.4.7 the two profiles that set it carry
	// the same bytes as the built-in default.
	SessionSummaryPrompt bool
}

// profiles is the document's model_profiles map.
var profiles = map[string]Profile{
	"MODEL_GOOGLE_GEMINI_2_5_FLASH": {
		Strategy:                   "CHECKPOINT_STRATEGY_SINGLE_PROMPT",
		MaxTokenLimit:              128000,
		MaxOutputTokens:            16384,
		TokenThreshold:             50000,
		MaxOverheadRatio:           0.15,
		Enabled:                    true,
		CheckpointModel:            "MODEL_PLACEHOLDER_M50",
		UseLastPlannerModel:        false,
		IsSync:                     false,
		MaxUserRequests:            10,
		IncludeLastUserMessage:     false,
		IncludeConversationLog:     true,
		IncludeRunningTaskSnaps:    true,
		IncludeSubagentSnapshots:   true,
		IncludeArtifactSnapshots:   true,
		RetryMaxRetries:            0,
		UseModelReportedTokenCount: false,
		SessionSummaryPrompt:       false,
	},
	"MODEL_GOOGLE_GEMINI_2_5_PRO": {
		Strategy:                   "CHECKPOINT_STRATEGY_SINGLE_PROMPT",
		MaxTokenLimit:              128000,
		MaxOutputTokens:            16384,
		TokenThreshold:             50000,
		MaxOverheadRatio:           0.15,
		Enabled:                    true,
		CheckpointModel:            "MODEL_PLACEHOLDER_M50",
		UseLastPlannerModel:        false,
		IsSync:                     false,
		MaxUserRequests:            10,
		IncludeLastUserMessage:     false,
		IncludeConversationLog:     true,
		IncludeRunningTaskSnaps:    true,
		IncludeSubagentSnapshots:   true,
		IncludeArtifactSnapshots:   true,
		RetryMaxRetries:            0,
		UseModelReportedTokenCount: false,
		SessionSummaryPrompt:       false,
	},
	"MODEL_OPENAI_GPT_OSS_120B_MEDIUM": {
		Strategy:                   "CHECKPOINT_STRATEGY_UNSPECIFIED",
		MaxTokenLimit:              80000,
		MaxOutputTokens:            8192,
		TokenThreshold:             50000,
		MaxOverheadRatio:           0.15,
		Enabled:                    true,
		CheckpointModel:            "MODEL_PLACEHOLDER_M50",
		UseLastPlannerModel:        false,
		IsSync:                     false,
		MaxUserRequests:            10,
		IncludeLastUserMessage:     false,
		IncludeConversationLog:     true,
		IncludeRunningTaskSnaps:    true,
		IncludeSubagentSnapshots:   true,
		IncludeArtifactSnapshots:   true,
		RetryMaxRetries:            0,
		UseModelReportedTokenCount: false,
		SessionSummaryPrompt:       false,
	},
	"MODEL_PLACEHOLDER_M124": {
		Strategy:                   "CHECKPOINT_STRATEGY_SAME_MODEL",
		MaxTokenLimit:              256000,
		MaxOutputTokens:            16384,
		TokenThreshold:             50000,
		MaxOverheadRatio:           0.15,
		Enabled:                    true,
		CheckpointModel:            "MODEL_PLACEHOLDER_M50",
		UseLastPlannerModel:        true,
		IsSync:                     true,
		MaxUserRequests:            10,
		IncludeLastUserMessage:     true,
		IncludeConversationLog:     false,
		IncludeRunningTaskSnaps:    true,
		IncludeSubagentSnapshots:   true,
		IncludeArtifactSnapshots:   true,
		RetryMaxRetries:            4,
		UseModelReportedTokenCount: false,
		SessionSummaryPrompt:       true,
	},
	"MODEL_PLACEHOLDER_M26": {
		Strategy:                   "CHECKPOINT_STRATEGY_UNSPECIFIED",
		MaxTokenLimit:              160000,
		MaxOutputTokens:            16384,
		TokenThreshold:             50000,
		MaxOverheadRatio:           0.15,
		Enabled:                    true,
		CheckpointModel:            "MODEL_PLACEHOLDER_M50",
		UseLastPlannerModel:        false,
		IsSync:                     false,
		MaxUserRequests:            10,
		IncludeLastUserMessage:     false,
		IncludeConversationLog:     true,
		IncludeRunningTaskSnaps:    true,
		IncludeSubagentSnapshots:   true,
		IncludeArtifactSnapshots:   true,
		RetryMaxRetries:            0,
		UseModelReportedTokenCount: false,
		SessionSummaryPrompt:       false,
	},
	"MODEL_PLACEHOLDER_M400": {
		Strategy:                   "CHECKPOINT_STRATEGY_SAME_MODEL",
		MaxTokenLimit:              256000,
		MaxOutputTokens:            16384,
		TokenThreshold:             50000,
		MaxOverheadRatio:           0.15,
		Enabled:                    true,
		CheckpointModel:            "MODEL_PLACEHOLDER_M50",
		UseLastPlannerModel:        true,
		IsSync:                     true,
		MaxUserRequests:            10,
		IncludeLastUserMessage:     true,
		IncludeConversationLog:     false,
		IncludeRunningTaskSnaps:    true,
		IncludeSubagentSnapshots:   true,
		IncludeArtifactSnapshots:   true,
		RetryMaxRetries:            4,
		UseModelReportedTokenCount: false,
		SessionSummaryPrompt:       false,
	},
	"MODEL_PLACEHOLDER_M408": {
		Strategy:                   "CHECKPOINT_STRATEGY_SAME_MODEL",
		MaxTokenLimit:              256000,
		MaxOutputTokens:            16384,
		TokenThreshold:             50000,
		MaxOverheadRatio:           0.15,
		Enabled:                    true,
		CheckpointModel:            "MODEL_PLACEHOLDER_M50",
		UseLastPlannerModel:        true,
		IsSync:                     true,
		MaxUserRequests:            10,
		IncludeLastUserMessage:     true,
		IncludeConversationLog:     false,
		IncludeRunningTaskSnaps:    true,
		IncludeSubagentSnapshots:   true,
		IncludeArtifactSnapshots:   true,
		RetryMaxRetries:            4,
		UseModelReportedTokenCount: false,
		SessionSummaryPrompt:       true,
	},
}

// profileVariants is the document's per-profile variant payloads.
var profileVariants = map[string]Profile{
	"MODEL_PLACEHOLDER_M400/claude-1m_840k": {
		Strategy:                   "CHECKPOINT_STRATEGY_SAME_MODEL",
		MaxTokenLimit:              840000,
		MaxOutputTokens:            16384,
		TokenThreshold:             50000,
		MaxOverheadRatio:           0.15,
		Enabled:                    true,
		CheckpointModel:            "MODEL_PLACEHOLDER_M50",
		UseLastPlannerModel:        true,
		IsSync:                     true,
		MaxUserRequests:            10,
		IncludeLastUserMessage:     true,
		IncludeConversationLog:     false,
		IncludeRunningTaskSnaps:    true,
		IncludeSubagentSnapshots:   true,
		IncludeArtifactSnapshots:   true,
		RetryMaxRetries:            4,
		UseModelReportedTokenCount: true,
		SessionSummaryPrompt:       false,
	},
}

// profileAliases is the document's models map.
var profileAliases = map[string]string{
	"MODEL_GOOGLE_GEMINI_2_5_FLASH":          "MODEL_GOOGLE_GEMINI_2_5_FLASH",
	"MODEL_GOOGLE_GEMINI_2_5_FLASH_LITE":     "MODEL_GOOGLE_GEMINI_2_5_FLASH",
	"MODEL_GOOGLE_GEMINI_2_5_FLASH_THINKING": "MODEL_GOOGLE_GEMINI_2_5_FLASH",
	"MODEL_GOOGLE_GEMINI_2_5_PRO":            "MODEL_GOOGLE_GEMINI_2_5_PRO",
	"MODEL_OPENAI_GPT_OSS_120B_MEDIUM":       "MODEL_OPENAI_GPT_OSS_120B_MEDIUM",
	"MODEL_PLACEHOLDER_M124":                 "MODEL_PLACEHOLDER_M124",
	"MODEL_PLACEHOLDER_M148":                 "MODEL_GOOGLE_GEMINI_2_5_PRO",
	"MODEL_PLACEHOLDER_M18":                  "MODEL_GOOGLE_GEMINI_2_5_PRO",
	"MODEL_PLACEHOLDER_M198":                 "MODEL_GOOGLE_GEMINI_2_5_FLASH",
	"MODEL_PLACEHOLDER_M26":                  "MODEL_PLACEHOLDER_M26",
	"MODEL_PLACEHOLDER_M317":                 "MODEL_PLACEHOLDER_M124",
	"MODEL_PLACEHOLDER_M318":                 "MODEL_PLACEHOLDER_M124",
	"MODEL_PLACEHOLDER_M319":                 "MODEL_PLACEHOLDER_M124",
	"MODEL_PLACEHOLDER_M320":                 "MODEL_PLACEHOLDER_M124",
	"MODEL_PLACEHOLDER_M322":                 "MODEL_PLACEHOLDER_M124",
	"MODEL_PLACEHOLDER_M35":                  "MODEL_PLACEHOLDER_M26",
	"MODEL_PLACEHOLDER_M400":                 "MODEL_PLACEHOLDER_M400",
	"MODEL_PLACEHOLDER_M401":                 "MODEL_PLACEHOLDER_M400",
	"MODEL_PLACEHOLDER_M402":                 "MODEL_PLACEHOLDER_M400",
	"MODEL_PLACEHOLDER_M403":                 "MODEL_PLACEHOLDER_M400",
	"MODEL_PLACEHOLDER_M404":                 "MODEL_PLACEHOLDER_M400",
	"MODEL_PLACEHOLDER_M405":                 "MODEL_PLACEHOLDER_M400",
	"MODEL_PLACEHOLDER_M408":                 "MODEL_PLACEHOLDER_M408",
	"MODEL_PLACEHOLDER_M409":                 "MODEL_PLACEHOLDER_M408",
	"MODEL_PLACEHOLDER_M410":                 "MODEL_PLACEHOLDER_M408",
	"MODEL_PLACEHOLDER_M411":                 "MODEL_PLACEHOLDER_M408",
	"MODEL_PLACEHOLDER_M412":                 "MODEL_PLACEHOLDER_M408",
	"MODEL_PLACEHOLDER_M413":                 "MODEL_PLACEHOLDER_M408",
	"MODEL_PLACEHOLDER_M417":                 "MODEL_PLACEHOLDER_M408",
	"MODEL_PLACEHOLDER_M418":                 "MODEL_PLACEHOLDER_M408",
	"MODEL_PLACEHOLDER_M419":                 "MODEL_PLACEHOLDER_M408",
	"MODEL_PLACEHOLDER_M420":                 "MODEL_PLACEHOLDER_M408",
	"MODEL_PLACEHOLDER_M421":                 "MODEL_PLACEHOLDER_M408",
	"MODEL_PLACEHOLDER_M429":                 "MODEL_PLACEHOLDER_M408",
	"MODEL_PLACEHOLDER_M430":                 "MODEL_PLACEHOLDER_M408",
	"MODEL_PLACEHOLDER_M431":                 "MODEL_PLACEHOLDER_M408",
	"MODEL_PLACEHOLDER_M50":                  "MODEL_GOOGLE_GEMINI_2_5_FLASH",
}

// ProfileFor returns the cascade profile of a catalogue model id.
//
// The lookup follows the reference's own chain: the model id names a
// model enum, the enum folds onto a profile through the document's alias
// map, and the profile's variant payload (when it has one) replaces the
// plain payload.
//
// It reports false for a model the 1.4.8 catalogue does not carry — the
// caller falls back to the 1.4.4 capture.
func ProfileFor(modelID string) (Profile, bool) {
	m, ok := catalogue[normalize(modelID)]
	if !ok {
		return Profile{}, false
	}
	name, ok := profileAliases[m.enum]
	if !ok {
		return Profile{}, false
	}
	p, ok := profiles[name]
	if !ok {
		return Profile{}, false
	}
	if v, ok := profileVariants[name+"/"+CheckpointVariant]; ok {
		p = v
	}
	return p, true
}
