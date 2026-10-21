package backend

import (
	"fmt"
	"strings"
	"time"
)

// The reference wraps every user turn before it reaches CloudCode. The
// envelope is not cosmetic: it is the text the model actually reads, and it is
// what agy's /context panel counts. Captured verbatim from a 1.4.3 request
// body (see docs/cycles/cycle-292-context-tokens/):
//
//	<USER_REQUEST>
//	read file
//	</USER_REQUEST>
//	<ADDITIONAL_METADATA>
//	The current local time is: 2026-10-21T07:13:26+08:00.
//	</ADDITIONAL_METADATA>
//	<USER_SETTINGS_CHANGE>
//	The user changed setting `Model Selection` from None to Gemini 3.8 Flash (Medium). …
//	</USER_SETTINGS_CHANGE>
//
// The settings block rides only on the first turn of a conversation and on the
// turn where the model selection actually changed.
const (
	userRequestOpen   = "<USER_REQUEST>\n"
	userRequestClose  = "\n</USER_REQUEST>"
	userMetadataOpen  = "\n<ADDITIONAL_METADATA>\nThe current local time is: "
	userMetadataClose = ".\n</ADDITIONAL_METADATA>"
	userSettingsOpen  = "\n<USER_SETTINGS_CHANGE>\n"
	userSettingsClose = "\n</USER_SETTINGS_CHANGE>"

	userSettingsBody = "The user changed setting `Model Selection` from None to %s. " +
		"No need to comment on this change if the user doesn't ask about it. " +
		"If reporting what model you are, please use a human readable name instead of the exact string."
)

// WrapUserContent renders the reference's <USER_REQUEST> envelope around raw.
// now supplies the local-time line; modelLabel and settingsChanged add the
// <USER_SETTINGS_CHANGE> block the reference sends when the model selection is
// new or has just changed.
func WrapUserContent(raw, modelLabel string, settingsChanged bool, now time.Time) string {
	var b strings.Builder
	b.WriteString(userRequestOpen)
	b.WriteString(raw)
	b.WriteString(userRequestClose)
	b.WriteString(userMetadataOpen)
	b.WriteString(now.Format(time.RFC3339))
	b.WriteString(userMetadataClose)
	if settingsChanged && modelLabel != "" {
		b.WriteString(userSettingsOpen)
		fmt.Fprintf(&b, userSettingsBody, modelLabel)
		b.WriteString(userSettingsClose)
	}
	return b.String()
}

// UserContentTokenCount is the reference's /context accounting for one user
// message: len(wire)/3 + 6. The divisor is EstimateTokenCounter's 3.0; the +6
// is the fixed per-message overhead the panel adds. Verified against 1.4.3:
// wire 437 -> 151, 433 -> 150, 131 -> 49, 232 -> 83.
func UserContentTokenCount(wire string) int {
	if wire == "" {
		return 0
	}
	return len(wire)/3 + 6
}

// ToolResultTokenCount is the reference's /context accounting for one tool
// call, measured from the raw tool result (the wire's
// functionResponse.response.output) rather than the rendered step:
// (len(result)+1)/3 + 6. Verified against 1.4.3: 109 -> 42, 158 -> 59,
// 211 -> 76.
func ToolResultTokenCount(result string) int {
	if result == "" {
		return 0
	}
	return (len(result)+1)/3 + 6
}
