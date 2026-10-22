// Server-advertised models the account cannot select.
//
// `fetchAvailableModels` returns a map whose entries are not all selectable:
// `agy models` and the `Available models:` block print only the ids in
// `agentModelSorts`, and a handful of others are *known* but rejected with a
// reason rather than "not recognized".
//
// 【一手】 agy 1.4.4, live probes (docs/cycles/model-catalogue-argon-gate):
//
//	$ agy -p ok --model gemini-4-argon-disabled
//	error: invalid model selection (--model "gemini-4-argon-disabled" --effort ""):
//	  "Gemini 4 Argon" is unavailable: Gemini 4 Argon is currently available to
//	  Ultra and paid Pro plan subscribers
//	Available models:
//	  Gemini 3.8 Flash (High)
//	  …
//
//	$ agy -p ok --model "Gemini 4 Argon"      # the label takes the same path
//	$ agy -p ok --model gemini-4-argon        # without the suffix: "not recognized"
//	$ agy -p ok --model gemini-4-argon-disabled --effort high   # gate wins over --effort
//	$ agy -p ok --model gemini-4-argon-disabled --effort bogus  # but not over the effort vocabulary
//
// Every other advertised id — gemini-3.8-flash-tiered, gemini-3.5-flash-lite,
// gemini-3-flash, gemini-2.5-pro, gemini-3.1-flash-image, gpt-oss-120b,
// gemini-3-flash-agent, gemini-2.5-flash*, tab_*, chat_* — is "not recognized
// as a known model or custom model in settings", so the gate is Argon's alone.
package cli

// GatedModel is an advertised model whose label is known to the CLI but whose
// selection is refused for plan reasons. It is deliberately *not* part of
// ReferenceModels(): it never reaches /model, `agy models` or the
// "Available models:" block.
type GatedModel struct {
	// ID is the wire id the server advertises ("gemini-4-argon-disabled").
	ID string
	// Label is the display name the error quotes ("Gemini 4 Argon").
	Label string
	// Reason is the server's explanation, verbatim.
	Reason string
}

// referenceGatedModels is the gate table. Add an entry only with a captured
// error string; an empty Reason would render as `"X" is unavailable: ` and
// silently lose the "why".
var referenceGatedModels = []GatedModel{
	{
		ID:     "gemini-4-argon-disabled",
		Label:  "Gemini 4 Argon",
		Reason: "Gemini 4 Argon is currently available to Ultra and paid Pro plan subscribers",
	},
}

// ReferenceGatedModels returns the advertised-but-unavailable models.
func ReferenceGatedModels() []GatedModel {
	out := make([]GatedModel, len(referenceGatedModels))
	copy(out, referenceGatedModels)
	return out
}
