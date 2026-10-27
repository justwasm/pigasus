// print_format.go — cycle 114: the `--output-format` print-mode envelope.
//
// The reference's `-p` accepts `--output-format text|json|stream-json`. `text`
// is the default and is what every earlier cycle implemented. `json` replaces
// the plain stdout text with a single-line JSON envelope; `stream-json` emits
// one JSON event per line.
//
// Everything below is 【一手】 — captured from the 1.3.6 binary. The account
// quota is exhausted, so a model turn cannot succeed, but the *local* print-mode
// slash commands answer without a model call and expose the full success shape:
//
//	$ agy -p=/help --output-format json
//	{"conversation_id":"","status":"SUCCESS","response":"/agents\tList available…",
//	 "duration_seconds":0,"num_turns":0,
//	 "usage":{"input_tokens":0,"output_tokens":0,"thinking_tokens":0,"cache_read_tokens":0,"total_tokens":0},
//	 "command":{"name":"help","data":{"commands":[{"name":"agents","description":"List available custom agents"},…]}}}
//
// and the failure shapes:
//
//	$ agy -p=/clear --output-format json            # rc 2
//	{"conversation_id":"","status":"ERROR","response":"","error":"/clear is not available in print mode (…)",…}
//
//	$ agy -p=/credits --output-format json          # rc 1
//	{"conversation_id":"","status":"ERROR","response":"","error":"/credits failed: retrieving credits: no credits info found",…}
//
//	$ agy -p='say hi' --output-format json          # rc 3, quota exhausted
//	{"conversation_id":"cfebbff3-…","status":"ERROR","response":"","error":"Individual quota reached. …","num_turns":1,…}
//
// Note what the four captures pin down:
//
//   - the field order is conversation_id, status, response, error,
//     duration_seconds, num_turns, usage, command;
//   - `error` and `command` are omitempty (absent on the other path);
//   - `conversation_id` is "" for anything answered without a session and a
//     UUID once a model turn starts;
//   - `num_turns` is 0 for a local command and 1 for a model turn, even when
//     that turn fails;
//   - the envelope is written to stdout on every path, including the rc 2
//     rejections, whose stderr line is unchanged.
package printmode

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/apierror"
)

// Output-format names, and the two `status` values the envelope carries.
const (
	PrintFormatText       = "text"
	PrintFormatJSON       = "json"
	PrintFormatStreamJSON = "stream-json"

	printFormatText       = PrintFormatText
	printFormatJSON       = PrintFormatJSON
	printFormatStreamJSON = PrintFormatStreamJSON

	printStatusSuccess = "SUCCESS"
	printStatusError   = "ERROR"
)

// PrintOutputFormat resolves --output-format.
func PrintOutputFormat(fs *flag.FlagSet) string {
	return printOutputFormat(fs)
}

// printOutputFormat resolves --output-format. Anything unrecognised (including
// the empty default) means `text`.
func printOutputFormat(fs *flag.FlagSet) string {
	switch flagStringValue(fs, "output-format") {
	case printFormatJSON:
		return printFormatJSON
	case printFormatStreamJSON:
		return printFormatStreamJSON
	default:
		return printFormatText
	}
}

// jsonUsage is the envelope's `usage` object. The reference reports it as all
// zeros for a local command; for a model turn the numbers are the turn's token
// counts.
type jsonUsage struct {
	InputTokens     int `json:"input_tokens"`
	OutputTokens    int `json:"output_tokens"`
	ThinkingTokens  int `json:"thinking_tokens"`
	CacheReadTokens int `json:"cache_read_tokens"`
	TotalTokens     int `json:"total_tokens"`
}

// jsonCommandPayload is the `command` member: the structured form of a
// print-mode slash command's answer, alongside the same answer rendered as
// text in `response`.
type jsonCommandPayload struct {
	Name string `json:"name"`
	Data any    `json:"data"`
}

// jsonEnvelope is the object `--output-format json` prints, and the `result`
// member of the `--output-format stream-json` stream. Field order matches the
// reference's wire order.
type jsonEnvelope struct {
	ConversationID  string              `json:"conversation_id"`
	Status          string              `json:"status"`
	Response        string              `json:"response"`
	Error           string              `json:"error,omitempty"`
	DurationSeconds int                 `json:"duration_seconds"`
	NumTurns        int                 `json:"num_turns"`
	Usage           jsonUsage           `json:"usage"`
	Command         *jsonCommandPayload `json:"command,omitempty"`
}

// writeJSONLine marshals v and writes it followed by a newline.
func writeJSONLine(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if _, err := w.Write(append(b, '\n')); err != nil {
		return err
	}
	return nil
}

// The five `event` names `--output-format stream-json` emits. The first
// four are captured from 1.3.6:
//
//	agy -p=/help --output-format stream-json
//	{"event":"command_result","command":{…}}
//	{"event":"result","result":{…}}
//
//	agy -p='say hi' --output-format stream-json
//	{"event":"init","conversation_id":"…","init":{…}}
//	{"event":"step_update","step_update":{…}}
//	{"event":"result","result":{…}}
//
// Cycle headless-partial-response-flag (AGY 1.2.10 close-gap) adds a fifth
// event `partial_response`, which `--print-partial-response` emits once per
// text chunk so a long-running turn gives earlier feedback than waiting for
// the final `result` envelope.
//
// The sixth — assistant_message — is emitted only when
// `--output-format stream-json --verbose` is in effect (AGY 1.2.7
// close-gap). It carries the assistant text body the model produced
// for the turn, so consumers see the message *before* the closing
// `result` envelope — useful for tooling that wants to display the
// assistant text without parsing the buffered `response` field of the
// envelope. The cycle
// headless-jsonl-mode-trailing-newline verifies each assistant_message
// event ends with a `\n` so the shell prompt that runs after the
// CLI does not glue to the last byte of stdout.
const (
	streamEventInit            = "init"
	streamEventStepUpdate      = "step_update"
	streamEventCommandResult   = "command_result"
	streamEventPartialResponse = "partial_response"
	streamEventResult          = "result"
	streamEventAssistantMessage = "assistant_message"
)

// jsonInitPayload is the `init` member: the session's starting state, emitted
// before the model is called.
type jsonInitPayload struct {
	CWD            string   `json:"cwd"`
	Tools          []string `json:"tools"`
	PermissionMode string   `json:"permission_mode"`
}

// jsonStepUpdate is the `step_update` member. `state` is DONE for every step the
// clone emits (the reference also has RUNNING/PENDING states, which are
// unrecovered because a successful turn cannot be captured while the account is
// exhausted).
type jsonStepUpdate struct {
	ConversationID string `json:"conversation_id"`
	StepIndex      int    `json:"step_index"`
	State          string `json:"state"`
	StepType       string `json:"step_type"`
}

// jsonAssistantMessage is the `assistant_message` member of the
// `--output-format stream-json --verbose` event stream. It carries
// the assistant text body the model produced for the current turn,
// with the wire-level shape:
//
//	{"event":"assistant_message","assistant_message":{"message":"…","model":"…"}}
//
// `message` is the full accumulated text — the same value the closing
// `result.response` carries — and `model` is the model id the turn
// ran against (omitted when the empty default applies, matching the
// AGY wire shape). The cycle headless-jsonl-mode-trailing-newline
// requires every assistant_message event to terminate with a
// newline; emitAssistantMessage is the single point that handles
// emission and goes through writeJSONLine.
type jsonAssistantMessage struct {
	Message string `json:"message"`
	Model   string `json:"model,omitempty"`
}

// jsonStreamEvent is one line of `--output-format stream-json`. Field order is
// the reference's: `event` first, then `conversation_id` (set on init only),
// then whichever single payload member this event carries.
type jsonStreamEvent struct {
	Event            string                `json:"event"`
	ConversationID   string                `json:"conversation_id,omitempty"`
	Init             *jsonInitPayload      `json:"init,omitempty"`
	StepUpdate       *jsonStepUpdate       `json:"step_update,omitempty"`
	Command          *jsonCommandPayload   `json:"command,omitempty"`
	PartialResponse  *jsonPartialResponse  `json:"partial_response,omitempty"`
	AssistantMessage *jsonAssistantMessage `json:"assistant_message,omitempty"`
	Result           *jsonEnvelope         `json:"result,omitempty"`
}

// jsonPartialResponse is the `partial_response` member of a stream-json event
// emitted once per text chunk under `--print-partial-response`. The shape is
// the small surface the reference emits — a single `response` field holding
// the sanitised chunk text. The whole-event payload is:
//
//	{"event":"partial_response","partial_response":{"response":"…"}}
//
// We deliberately don't carry the conversation_id on this event: the comment
// on jsonStreamEvent pins `conversation_id` to the `init` event, and every
// `partial_response` arrives after `init` so consumers can resolve it from
// the session header.
type jsonPartialResponse struct {
	Response string `json:"response"`
}

// emitStep writes one `step_update` event. The clone reports every step as
// DONE; the reference's RUNNING/PENDING transitions are unrecovered, because a
// successful turn cannot be captured while the account is exhausted.
func emitStep(w io.Writer, conversationID string, index int, stepType string) error {
	return writeJSONLine(w, jsonStreamEvent{
		Event: streamEventStepUpdate,
		StepUpdate: &jsonStepUpdate{
			ConversationID: conversationID,
			StepIndex:      index,
			State:          "DONE",
			StepType:       stepType,
		},
	})
}

// emitPartialResponse writes one `partial_response` event carrying the
// sanitised text of a single chunk. Called from the chunk handler when
// `--print-partial-response` is set under `--output-format stream-json`, so
// the user sees each chunk land on stdout as soon as the model emits it
// rather than waiting for the final `result` envelope. The chunk text
// remains in the running envelope for the final result event — this writer
// is the streaming side-channel, not a replacement.
func emitPartialResponse(w io.Writer, text string) error {
	if text == "" {
		return nil
	}
	return writeJSONLine(w, jsonStreamEvent{
		Event:           streamEventPartialResponse,
		PartialResponse: &jsonPartialResponse{Response: text},
	})
}

// emitAssistantMessage writes one `assistant_message` event carrying
// the assistant text body for a single model turn. It is the closing
// event emitted before the `result` envelope when the user passes
// `--output-format stream-json --verbose` (AGY 1.2.7
// close-gap, headless-jsonl-mode-trailing-newline cycle).
//
// The route through writeJSONLine is the load-bearing property:
// appending `\n` to the marshalled JSON is what guarantees the
// trailing newline the AGY 1.2.7 changelog bug fix documents. If a
// future refactor reaches for `json.NewEncoder(w).Encode(...)` here
// directly, that property still holds (Encode adds `\n` on its own),
// but a hand-rolled `w.Write(b)` without the appended `\n` would
// re-introduce the bug the cycle closes. Keep this comment so any
// later edit sees the constraint.
func emitAssistantMessage(w io.Writer, message, model string) error {
	return writeJSONLine(w, jsonStreamEvent{
		Event: streamEventAssistantMessage,
		AssistantMessage: &jsonAssistantMessage{
			Message: message,
			Model:   model,
		},
	})
}

// printVerbose resolves the `--verbose` bool flag. The flag is hidden
// (absent from --help), so an unset value reads as false — exactly
// what every pre-existing stream-json consumer expected before the
// headless-jsonl-mode-trailing-newline cycle landed.
func printVerbose(fs *flag.FlagSet) bool {
	return flagBoolValue(fs, "verbose")
}
// printTimeout returns the --print-timeout deadline. The changelog changed the
// default headless timeout from 5 minutes to unlimited — "long-running agent
// turns run until the response completes unless --print-timeout is passed
// explicitly" — and the reference treats every non-positive value the same way:
// `--print-timeout=0` and `--print-timeout=-5s` both let the turn run to
// completion. So 0 means "no deadline".
func printTimeout(fs *flag.FlagSet) time.Duration {
	d, err := time.ParseDuration(flagStringValue(fs, "print-timeout"))
	if err != nil || d <= 0 {
		return 0
	}
	return d
}

// printContext bounds a context by the --print-timeout deadline, or leaves it
// unbounded when no timeout was requested.
func printContext(d time.Duration) (context.Context, context.CancelFunc) {
	if d <= 0 {
		return context.WithCancel(context.Background())
	}
	return context.WithTimeout(context.Background(), d)
}

// printTimeoutWarning is the stderr line the reference emits when the deadline
// expires while the turn is still running. Recovered verbatim from the 1.3.6
// string table and confirmed live with `--print-timeout=1ms`:
//
//	[agy] print timeout after 1ms with turn in progress; returning partial output
//
// The reference hardcodes `[agy]` here — it prints the same text when the binary
// is renamed. The clone derives the tag from argv0, which is what it does for
// the program name everywhere else and is identical whenever the binary is
// invoked as `agy`.
func printTimeoutWarning(d time.Duration) string {
	return fmt.Sprintf("[%s] print timeout after %s with turn in progress; returning partial output",
		filepath.Base(os.Args[0]), d)
}

// printPermissionMode returns the `permission_mode` the init event reports.
//
// The reference reads the `toolPermission` setting and falls back to its
// default when the key is absent.
//
// 【一手】 agy 1.4.8: with an empty settings.json the init event reports
// "request-review"; the 1.1.0 changelog entry ("Added `request-review`
// (default) mode as the default execution behavior") agrees. The
// "always-proceed" the cycle-113 capture carries came from a capture machine
// whose settings.json set `toolPermission` explicitly — the file value wins.
func printPermissionMode() string {
	raw := map[string]json.RawMessage{}
	if b, err := readSettingsFile(); err == nil {
		_ = json.Unmarshal(b, &raw)
	}
	var v string
	if len(raw["toolPermission"]) > 0 {
		_ = json.Unmarshal(raw["toolPermission"], &v)
	}
	if v == "" {
		v = printSlashConfigDefaultString("toolPermission")
	}
	return v
}

// configData is in print_format_config.go

// printErrorMessage renders the JSON envelope's `error` member: the same text
// the stderr line carries, without the leading `error: `.
func printErrorMessage(err error) string {
	if err == nil {
		return ""
	}
	// Cycle 262: eligibility errors use the same JSON envelope
	// shape but carry an ELIGIBILITY_DENIED status instead of
	// the usual PERMISSION_DENIED. The user-facing Message here
	// is the upstream Reason — automation that wants the
	// verification URL parses the JSON envelope's
	// `error.message` field instead.
	if api, ok := apierror.ClassifyAPIError(err); ok {
		return api.Message
	}
	return strings.TrimPrefix(err.Error(), "error: ")
}

// newEnvelopeConversationID returns the envelope's `conversation_id`
// for a model turn. The reference leaves it empty for anything answered
// without a session (every local slash command) and emits a UUID once
// a turn starts. The value is random, so only the shape is reproduced.
//
// Print-mode only — the live TUI uses runMode.newConversationID which
// also sets the version/variant bits per RFC 4122 so the /help panel
// matches the reference byte-for-byte.
func newEnvelopeConversationID() string {
	var b [16]byte
	if _, err := rand.Read(b[:]); err != nil {
		return ""
	}
	b[6] = (b[6] & 0x0f) | 0x40 // version 4
	b[8] = (b[8] & 0x3f) | 0x80 // RFC 4122 variant
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:16])
}
