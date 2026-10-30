// Package modelctx holds the reference's per-model context limit and the
// token reserve it holds back for the next step.
//
// # Where the numbers come from
//
// 【一手】 Two sources, in the order the clone consults them:
//
//  1. The binary's own cascade catalogue — the model_profiles.json agy
//     1.4.7 embeds at file offset 0x9a0f20a. It carries one
//     CASCADE_USE_EXPERIMENT_CHECKPOINTER payload per model family, the
//     alias map that folds the model ids onto those families, and the
//     per-profile variant payloads. See profiles_1.4.7.go; the document
//     itself is committed at testdata/model_profiles_1.4.7.json.
//
//  2. The server's own model catalogue, captured at agy 1.4.4 by a live
//     probe of fetchAvailableModels
//     (docs/cycles/model-catalogue-argon-gate/fetchAvailableModels_1.4.4.json).
//     Every entry carries the model's own maxTokens (its input context
//     limit), its maxOutputTokens, and — for the models that carry one —
//     the checkpoint experiment payload naming max_token_limit and
//     max_output_tokens.
//
// The clone never calls fetchAvailableModels at runtime — `agy models`
// prints a static catalogue — so the capture is a mirror, and the
// catalogue table below is its reduced form.
//
// # The reference's effective limit
//
// 【反编译】 cortex/chatconverters.(*TrajectoryChatConverter).TrajectoryToChatMessages
// computes the conversion limit as
//
//	tokenLimit = model.input_token_limit            (the model's own limit)
//	if checkpointConfig.enabled && checkpointConfig.max_token_limit != 0:
//	    tokenLimit = min(tokenLimit, checkpointConfig.max_token_limit)
//
// and cortex/utils.GetCheckpointConfig clamps the experiment-provided limit to
// at most 256000 (and the output cap to at most 16384). So the effective
// context limit of a model is
//
//	min(model input limit, checkpoint max_token_limit)
//
// which is what ContextWindow below holds.
//
// # The reference's reserve
//
// The same conversion errors out when the base context plus the most recent
// step would exceed the limit. The binary's own error text spells the
// arithmetic out:
//
//	the most recent step (%s) could not fit in the available context limit:
//	  Total attempted tokens: ~%d (exceeding cascade_config.checkpoint_config.max_token_limit (%d))
//	    - Base context: ~%d tokens (instructions: ~%d, tool definitions: ~%d)
//	    - Most recent step: ~%d tokens (text: %d, image: %d)
//
// The reference holds the checkpoint's `max_output_tokens` back so the next
// step has room to answer. That number is 16384 for every model with a
// checkpoint experiment except gpt-oss-120b-medium (8192) — and 16384 is
// PiG's own default reserve, so the clone's rule
//
//	contextTokens > contextWindow - reserveTokens
//
// is the same comparison the reference makes, once the window is the
// reference's effective limit and the reserve is its max_output_tokens.
//
// # The fallback
//
// A model the clone cannot name (an unknown --model, a registry entry the
// server never advertised) has no captured limit. DefaultContextWindow keeps
// the historical 200 K stand-in for that case; the reference's own fallback
// is the model's input limit, which the clone cannot see.
//
// # The configured models
//
// A model the user configured is not in the captured catalogue at all — the
// server never advertised it. Its two numbers come from the user's provider
// configuration instead: context_window is the model's input limit and
// default_max_tokens its output cap, which is the same pair the reference's
// catalogue carries and the same pair its checkpoint experiment reduces to.
package ctx

import (
	"strings"

	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/models"
)

const (
	// DefaultContextWindow is the window used for a model the clone has no
	// captured limit for. It is the historical stand-in, not a reference
	// measurement — the reference's fallback is the model's own input limit,
	// which the clone cannot observe because it never calls
	// fetchAvailableModels at runtime.
	DefaultContextWindow = 200_000

	// DefaultReserveTokens is the reserve used for a model with no captured
	// checkpoint output cap. 16384 is the reference's canonical checkpoint
	// max_output_tokens and PiG's own default reserve.
	DefaultReserveTokens = 16_384
)

// Limit is one model's context budget: the window the context is measured
// against, and the reserve held back for the next step.
type Limit struct {
	// ContextWindow is the effective context limit, in tokens.
	ContextWindow int
	// ReserveTokens is the number of tokens held back for the next step's
	// output.
	ReserveTokens int
}

// catalogue is the 1.4.4 capture, reduced to the fields the clone needs.
//
// The key is the catalogue id the server advertises; the comment gives the
// internal model name — the enum the binary's own alias map folds onto a
// cascade profile.
//
//	inputLimit — the catalogue's maxTokens, the model's own context limit
//	outputCap  — the catalogue's maxOutputTokens
//	ckptLimit  — the checkpoint experiment's max_token_limit (0 = no experiment)
//	ckptOutput — the checkpoint experiment's max_output_tokens
//	reserve    — overrides the derived reserve when it is not zero
type catalogueEntry struct {
	enum       string
	inputLimit int
	outputCap  int
	ckptLimit  int
	ckptOutput int
	reserve    int
}

// limit returns the row's 1.4.4 budget: the window is the smaller of the
// model's own input limit and the checkpoint experiment's limit, and the
// reserve is the checkpoint's output cap — the catalogue's own output cap
// for a model that carries no experiment.
func (e catalogueEntry) limit() Limit {
	window := e.inputLimit
	if e.ckptLimit > 0 && e.ckptLimit < window {
		window = e.ckptLimit
	}
	reserve := e.ckptOutput
	if e.ckptLimit == 0 {
		reserve = e.outputCap
	}
	if e.reserve > 0 {
		reserve = e.reserve
	}
	return Limit{ContextWindow: window, ReserveTokens: reserve}
}

// catalogue is the captured catalogue, one row per model the reference
// advertises.
//
//	window = min(inputLimit, ckptLimit)
//	reserve = ckptOutput, else outputCap
var catalogue = map[string]catalogueEntry{
	// --- the models the reference lets the CLI select ---

	// MODEL_PLACEHOLDER_M318 — maxTokens 1048576, max_output 65536,
	// checkpoint limit 256000 / output 16384.
	"gemini-3.8-flash-high": {enum: "MODEL_PLACEHOLDER_M318", inputLimit: 1_048_576, outputCap: 65_536, ckptLimit: 256_000, ckptOutput: 16_384},
	// MODEL_PLACEHOLDER_M319 — maxTokens 1048576, max_output 65536,
	// checkpoint limit 256000 / output 16384.
	"gemini-3.8-flash-medium": {enum: "MODEL_PLACEHOLDER_M319", inputLimit: 1_048_576, outputCap: 65_536, ckptLimit: 256_000, ckptOutput: 16_384},
	// MODEL_PLACEHOLDER_M320 — maxTokens 1048576, max_output 65536,
	// checkpoint limit 256000 / output 16384.
	"gemini-3.8-flash-low": {enum: "MODEL_PLACEHOLDER_M320", inputLimit: 1_048_576, outputCap: 65_536, ckptLimit: 256_000, ckptOutput: 16_384},
	// MODEL_PLACEHOLDER_M35 — maxTokens 250000, max_output 64000,
	// checkpoint limit 160000 / output 16384.
	"claude-sonnet-4-6": {enum: "MODEL_PLACEHOLDER_M35", inputLimit: 250_000, outputCap: 64_000, ckptLimit: 160_000, ckptOutput: 16_384},
	// MODEL_PLACEHOLDER_M26 — maxTokens 250000, max_output 64000,
	// checkpoint limit 160000 / output 16384.
	"claude-opus-4-6-thinking": {enum: "MODEL_PLACEHOLDER_M26", inputLimit: 250_000, outputCap: 64_000, ckptLimit: 160_000, ckptOutput: 16_384},
	// MODEL_OPENAI_GPT_OSS_120B_MEDIUM — maxTokens 131072, max_output
	// 32768, checkpoint limit 80000 / output 8192.
	"gpt-oss-120b-medium": {enum: "MODEL_OPENAI_GPT_OSS_120B_MEDIUM", inputLimit: 131_072, outputCap: 32_768, ckptLimit: 80_000, ckptOutput: 8_192},

	// --- advertised but not selectable (the reference's own validator
	// refuses them; kept so the table stays the captured catalogue) ---

	// MODEL_GOOGLE_GEMINI_2_5_FLASH — checkpoint limit 128000.
	"gemini-2.5-flash": {enum: "MODEL_GOOGLE_GEMINI_2_5_FLASH", inputLimit: 1_048_576, outputCap: 65_535, ckptLimit: 128_000, ckptOutput: 16_384},
	// MODEL_GOOGLE_GEMINI_2_5_FLASH_LITE — checkpoint limit 128000.
	"gemini-2.5-flash-lite": {enum: "MODEL_GOOGLE_GEMINI_2_5_FLASH_LITE", inputLimit: 1_048_576, outputCap: 65_535, ckptLimit: 128_000, ckptOutput: 16_384},
	// MODEL_GOOGLE_GEMINI_2_5_FLASH_THINKING — checkpoint limit 128000.
	"gemini-2.5-flash-thinking": {enum: "MODEL_GOOGLE_GEMINI_2_5_FLASH_THINKING", inputLimit: 1_048_576, outputCap: 65_535, ckptLimit: 128_000, ckptOutput: 16_384},
	// MODEL_GOOGLE_GEMINI_2_5_PRO — checkpoint limit 128000.
	"gemini-2.5-pro": {enum: "MODEL_GOOGLE_GEMINI_2_5_PRO", inputLimit: 1_048_576, outputCap: 65_535, ckptLimit: 128_000, ckptOutput: 16_384},
	// MODEL_PLACEHOLDER_M18 — checkpoint limit 128000.
	"gemini-3-flash": {enum: "MODEL_PLACEHOLDER_M18", inputLimit: 1_048_576, outputCap: 65_536, ckptLimit: 128_000, ckptOutput: 16_384},
	// MODEL_PLACEHOLDER_M84 — checkpoint limit 256000.
	"gemini-3-flash-agent": {enum: "MODEL_PLACEHOLDER_M84", inputLimit: 1_048_576, outputCap: 65_536, ckptLimit: 256_000, ckptOutput: 16_384},
	// MODEL_PLACEHOLDER_M50 — checkpoint limit 128000.
	"gemini-3.1-flash-lite": {enum: "MODEL_PLACEHOLDER_M50", inputLimit: 1_048_576, outputCap: 65_535, ckptLimit: 128_000, ckptOutput: 16_384},
	// MODEL_PLACEHOLDER_M187 — checkpoint limit 256000.
	"gemini-3.5-flash-extra-low": {enum: "MODEL_PLACEHOLDER_M187", inputLimit: 1_048_576, outputCap: 65_536, ckptLimit: 256_000, ckptOutput: 16_384},
	// MODEL_PLACEHOLDER_M198 — checkpoint limit 128000.
	"gemini-3.5-flash-lite": {enum: "MODEL_PLACEHOLDER_M198", inputLimit: 1_048_576, outputCap: 65_535, ckptLimit: 128_000, ckptOutput: 16_384},
	// MODEL_PLACEHOLDER_M20 — checkpoint limit 256000.
	"gemini-3.5-flash-low": {enum: "MODEL_PLACEHOLDER_M20", inputLimit: 1_048_576, outputCap: 65_536, ckptLimit: 256_000, ckptOutput: 16_384},
	// MODEL_PLACEHOLDER_M322 — checkpoint limit 256000.
	"gemini-3.8-flash-tiered": {enum: "MODEL_PLACEHOLDER_M322", inputLimit: 1_048_576, outputCap: 65_536, ckptLimit: 256_000, ckptOutput: 16_384},
	// MODEL_PLACEHOLDER_M421 — checkpoint limit 256000.
	"gemini-4-argon-disabled": {enum: "MODEL_PLACEHOLDER_M421", inputLimit: 1_048_576, outputCap: 65_536, ckptLimit: 256_000, ckptOutput: 16_384},

	// --- models with no checkpoint experiment: the window is the model's
	// own input limit and the reserve its own output cap ---

	// MODEL_PLACEHOLDER_M21 — maxTokens 65536, max_output 65536. The
	// catalogue's own output cap would leave no room in a 65536-token
	// window, so the clone holds back half of it. 【推测】 The reference
	// has no checkpoint experiment for this model and therefore holds
	// nothing back; the clone needs a reserve for its own trigger.
	"gemini-3.1-flash-image": {enum: "MODEL_PLACEHOLDER_M21", inputLimit: 65_536, outputCap: 65_536, reserve: 32_768},
	// MODEL_PLACEHOLDER_M19 — maxTokens 16384, max_output 4096.
	"tab_flash_lite_preview": {enum: "MODEL_PLACEHOLDER_M19", inputLimit: 16_384, outputCap: 4_096},
	// MODEL_PLACEHOLDER_M28 — maxTokens 16384, max_output 4096.
	"tab_jump_flash_lite_preview": {enum: "MODEL_PLACEHOLDER_M28", inputLimit: 16_384, outputCap: 4_096},
	// MODEL_CHAT_20706 — maxTokens 16384, no output cap.
	"chat_20706": {enum: "MODEL_CHAT_20706", inputLimit: 16_384},
	// MODEL_CHAT_23310 — maxTokens 32768, no output cap.
	"chat_23310": {enum: "MODEL_CHAT_23310", inputLimit: 32_768},
}

// For returns the context budget for a catalogue model id. An unknown id
// gets the fallback window and the canonical reserve.
//
// The lookup is case-insensitive and tolerates the surrounding whitespace a
// user may have typed, but it does not try to be clever about aliases: the
// ids are the ones the server advertises.
//
// One alias it does accept is the display label ("Gemini 3.8 Flash (High)"),
// because the label is what the TUI shows and therefore what PiG records as
// the model id. A lookup that only knew the slug would miss and silently
// fall back to the 200 K window — a fifth of the real budget, and a
// compaction that fires five times too early.
//
// A configured model is looked up by the same two names — its catalogue id
// and its display label — because the TUI records the label as the model
// id. Its numbers come from the configuration, not from the table above.
//
// The reference's own catalogue comes first: a model the binary's 1.4.7
// profile map carries is budgeted from that profile, and the 1.4.4
// capture is the fallback for the models the profile map does not know.
func For(modelID string) Limit {
	id := normalize(modelID)
	for _, cand := range candidates(modelID, id) {
		if l, ok := lookup(cand); ok {
			return l
		}
	}
	if l, ok := configuredLimit(modelID); ok {
		return l
	}
	return Limit{ContextWindow: DefaultContextWindow, ReserveTokens: DefaultReserveTokens}
}

// candidates returns the names a model id may be filed under: the id
// itself, its slug form, and the catalogue id of its display label.
func candidates(modelID, id string) []string {
	out := []string{id}
	if slug := slugify(id); slug != id {
		out = append(out, slug)
	}
	if canon, ok := catalogueID(modelID); ok && canon != id {
		out = append(out, canon)
	}
	return out
}

// lookup returns the budget of one catalogue id: the reference's own 1.4.7
// profile when it carries one, the 1.4.4 capture otherwise.
func lookup(id string) (Limit, bool) {
	if p, ok := ProfileFor(id); ok {
		window := p.MaxTokenLimit
		if m, ok := catalogue[id]; ok && m.inputLimit > 0 && m.inputLimit < window {
			window = m.inputLimit
		}
		return Limit{ContextWindow: window, ReserveTokens: p.MaxOutputTokens}, true
	}
	if m, ok := catalogue[id]; ok {
		return m.limit(), true
	}
	return Limit{}, false
}

// catalogueID maps a display label to its catalogue id.
func catalogueID(name string) (string, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return "", false
	}
	for _, m := range cli.ModelCatalogue() {
		if m.Label == name || m.ID == name {
			return m.ID, true
		}
	}
	return "", false
}

// configuredLimit returns the context budget of a model the user configured.
//
// The window is the configured context_window and the reserve the configured
// default_max_tokens — the model's own output cap, which is the number the
// reference holds back so the next step has room to answer. A model that
// does not state them falls back to the package defaults.
func configuredLimit(modelID string) (Limit, bool) {
	name := strings.TrimSpace(modelID)
	if name == "" {
		return Limit{}, false
	}
	reg := models.CachedRegistry()
	if reg == nil {
		return Limit{}, false
	}
	m, ok := reg.GetByName(name)
	if !ok {
		// The TUI records the display label as the model id, so a lookup by
		// the catalogue id alone would miss.
		for _, entry := range reg.List() {
			if strings.TrimSpace(entry.DisplayName) == name {
				m = &entry
				ok = true
				break
			}
		}
	}
	if !ok {
		return Limit{}, false
	}
	window := m.ContextWindow
	if window <= 0 {
		window = DefaultContextWindow
	}
	reserve := m.DefaultMaxTokens
	if reserve <= 0 {
		reserve = DefaultReserveTokens
	}
	return Limit{ContextWindow: window, ReserveTokens: reserve}, true
}

// ContextWindowFor returns the effective context limit for a catalogue model
// id, or DefaultContextWindow for an id the clone has no captured limit for.
func ContextWindowFor(modelID string) int {
	return For(modelID).ContextWindow
}

// normalize lowercases and trims a model id.
func normalize(modelID string) string {
	return strings.ToLower(strings.TrimSpace(modelID))
}

// slugify turns a display label into the slug form the table is keyed by:
// "MiniMax M3.1 Flash" -> "minimax-m3.1-flash".
func slugify(id string) string {
	s := strings.ReplaceAll(id, " ", "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return s
}
