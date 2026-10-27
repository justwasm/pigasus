// failure.go — the reference's own marker for a tool call that failed.
//
// 【一手】 agy 1.4.8, captured live through devutils/refcapture on
// 2026-10-27 (the reference binary's sha256
// 09fcdd8554e03bed9b2ec936e454c7f115d5610610de09ed7d79fa2feae76940):
// a call to a tool that does not exist and a call to a tool that failed
// both come back to the model as
//
//	"functionResponse": {"id": "call_1", "name": "read_file",
//	                     "response": {"error": "Created At: …\nCompleted At: …\nEncountered error in tool execution: file does not exist"}}
//
// Two things are fixed there. The envelope is `error`, not `output` — the
// 1.4.7 changelog's own words: "failed tool calls … were being passed
// back to the agent as ordinary results; the agent now sees them as
// errors". And the body's own wording is the reference's:
//
//	Encountered error in tool execution: <reason>   — the tool ran and failed
//	Encountered error in tool validation: <reason> — the call never ran
//
// The clone's tools already write that wording (and the two stamp lines
// that precede it) into the *text* of an ordinary result, because that is
// what the reference showed the model before 1.4.8. The wording is
// therefore the clone's only signal that a call failed — this file is
// where that signal is read out of the text.
package tools

import "strings"

// FailureMarker is the reference's prefix for a tool call that did not
// succeed.
//
// 【一手】 1.4.8: "Encountered error in tool execution: file does not
// exist" and "Encountered error in tool validation: unknown tool:
// \"view_file\" — check spelling".
const FailureMarker = "Encountered error in tool "

// stampPrefixes are the two lines every result carries before its body.
// The clone writes them itself (see resultStamp in filesystem_edit.go and
// its copies), and the reference writes them too — the captured bodies
// start with "Created At: …\nCompleted At: …\n".
var stampPrefixes = []string{"Created At: ", "Completed At: "}

// IsFailureText reports whether a tool's result text is the reference's
// own failure text.
//
// The check is deliberately narrow: the marker has to be the *first
// thing in the body*, after the two stamp lines. A result whose body
// merely mentions the marker — a file that happens to contain the
// sentence, a grep hit — is an ordinary result.
//
// The stamp lines are optional: a tool that fails before it stamps
// anything still writes the marker first.
func IsFailureText(text string) bool {
	body := stripStamps(text)
	return strings.HasPrefix(body, FailureMarker)
}

// stripStamps removes the two leading stamp lines, if they are there.
func stripStamps(text string) string {
	for _, prefix := range stampPrefixes {
		if !strings.HasPrefix(text, prefix) {
			return text
		}
		// The stamp line ends at the newline; without one the text is
		// not a stamp at all.
		nl := strings.IndexByte(text, '\n')
		if nl < 0 {
			return text
		}
		text = text[nl+1:]
	}
	return text
}
