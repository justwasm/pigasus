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

	// The @-mention expansion block. The reference sends a slash command's
	// body *here*, not as the user turn: the turn keeps the command text
	// verbatim and the body rides in <ADDITIONAL_METADATA> as an expansion.
	// Captured from agy 1.5.18 for /learn, /goal and /grill-me; the three
	// blocks were then rebuilt from the clone's own bodies and compared
	// byte-for-byte (2,442 B, 782 B, 742 B — all identical).
	//
	//	/user_request: "/learn always run make arch"
	//	<ADDITIONAL_METADATA>
	//	The current local time is: …
	//
	//	The user has mentioned some items in the form @[ITEM]. Here is extra
	//	information about the items that were mentioned by the user, in the
	//	order that they appear:
	//
	//	/learn is a [Slash Command]:
	//	<LEARN>
	//	…body…
	//	</LEARN>
	//	</ADDITIONAL_METADATA>
	mentionPreamble = "The user has mentioned some items in the form @[ITEM]. " +
		"Here is extra information about the items that were mentioned by the user, " +
		"in the order that they appear:"
)

// Mention is one @-mention the reference expands into <ADDITIONAL_METADATA>.
// Kind is the bracketed classification the reference prints — "Slash Command"
// for a slash command, "Text Block" for a file/@-mention (the two the
// reference's own parser recognises, per the regex in its binary:
// `(?:/(\S+) is a \[Slash Command\]|@\[([^\n]+)\] is a \[Text Block\]):`).
type Mention struct {
	// Name is what the reference prints before "is a": the command with its
	// leading slash, or the @-mention's path.
	Name string
	// Kind is the bracketed classification.
	Kind string
	// Body is the expansion text that follows the header line.
	Body string
}

// MentionKindSlashCommand is the classification the reference prints for a
// slash command's expansion.
const MentionKindSlashCommand = "Slash Command"

// WrapUserContent renders the reference's <USER_REQUEST> envelope around raw.
// now supplies the local-time line; modelLabel and settingsChanged add the
// <USER_SETTINGS_CHANGE> block the reference sends when the model selection is
// new or has just changed.
// System messages (<SYSTEM_MESSAGE>) are not user requests and are returned verbatim.
func WrapUserContent(raw, modelLabel string, settingsChanged bool, now time.Time) string {
	trimmed := strings.TrimSpace(raw)
	if strings.HasPrefix(trimmed, "<USER_REQUEST>") ||
		strings.HasPrefix(trimmed, "<CONTEXT_SUMMARY>") ||
		strings.HasPrefix(trimmed, "# Resuming from a compaction") ||
		strings.HasPrefix(trimmed, "The following is a <SYSTEM_MESSAGE>") ||
		strings.HasPrefix(trimmed, "<SYSTEM_MESSAGE>") ||
		strings.Contains(trimmed, "<SYSTEM_MESSAGE>") ||
		strings.HasPrefix(raw, "Step Id:") {
		return raw
	}
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

// WrapUserContentWithMentions renders the same envelope as WrapUserContent,
// with the @-mention expansion block appended inside <ADDITIONAL_METADATA>.
//
// This is the shape the reference actually sends for a slash command: the
// user turn is the command text verbatim, and the command's instruction is
// an expansion. The reference's parser reads it back with
//
//	(?:/(\S+) is a \[Slash Command\]|@\[([^\n]+)\] is a \[Text Block\]):\n
//
// so the header line and the newline after it are part of the format, not
// decoration. Each item is "header:\n" + body + "\n" — verified byte-for-byte
// against three live 1.5.18 captures.
//
// The block sits after the local-time line and before the settings block,
// separated from the time line by a blank line.
func WrapUserContentWithMentions(raw string, mentions []Mention, modelLabel string, settingsChanged bool, now time.Time) string {
	base := WrapUserContent(raw, modelLabel, settingsChanged, now)
	if len(mentions) == 0 {
		return base
	}
	// The mention block belongs inside <ADDITIONAL_METADATA>, so it has to be
	// spliced in before that element's closing tag rather than appended to
	// the whole envelope. A raw turn that WrapUserContent passed through
	// untouched (a <SYSTEM_MESSAGE>, a compaction summary) has no metadata
	// element; leave it alone — the reference does not expand a command
	// there either.
	// userMetadataClose is ".\n</ADDITIONAL_METADATA>", so the index lands on
	// the period that terminates the time line. Split there, re-add that
	// period and its newline by hand, and put the block in the gap: the
	// result is "…+08:00.\n" + "\n" + preamble, which is the blank line the
	// captures show between the time and the expansion.
	close := strings.LastIndex(base, userMetadataClose)
	if close < 0 {
		return base
	}
	var b strings.Builder
	b.WriteString(base[:close])
	b.WriteString(".\n\n")
	b.WriteString(mentionPreamble)
	b.WriteString("\n\n")
	for _, m := range mentions {
		b.WriteString(m.Name)
		b.WriteString(" is a [")
		b.WriteString(m.Kind)
		b.WriteString("]:\n")
		b.WriteString(m.Body)
		// A body that already ends with a newline needs no terminator; the
		// reference writes the body's own trailing newline directly against
		// the next header (or the close tag). Adding one unconditionally put
		// a blank line before </ADDITIONAL_METADATA> that the wire does not
		// have — caught by the capture fixtures, off by exactly one byte.
		if !strings.HasSuffix(m.Body, "\n") {
			b.WriteString("\n")
		}
	}
	b.WriteString(base[close+2:]) // past ".\n": just the close tag
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
