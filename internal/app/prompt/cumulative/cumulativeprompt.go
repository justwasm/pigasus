// Package cumulativeprompt turns the reference's implicit trajectory into the
// chat messages the next prompt carries.
//
// 【反编译】 agy 1.4.7. The reference records a `!` command in its own
// per-workspace store — the implicit trajectory — and never in the
// conversation. The cumulative-prompt handler
// (`google3/third_party/jetski/prompt/cumulative_prompt_handler/cumulative_prompt_handler`,
// init 0x10225a6f0) is what makes that trajectory model-visible: it converts
// the trajectory into chat messages when the next prompt is built, and the
// reference's own converter
// (`google3/third_party/jetski/cortex/chatconverters/chatconverters`,
// `(*TrajectoryChatConverter).constructMessagesForStep`, 0x101519d10) decides
// what those messages say.
//
// This package is that converter, for the one step type the clone's store
// holds.
//
// The reference's shape, in the clone's own words:
//
//	Step Id: 0
//	Created At: 2026-10-26T12:34:56+08:00
//	Completed At: 2026-10-26T12:34:57+08:00
//	The USER performed the following action:
//	Command: ls -la
//
//	Step Id: 0
//	Created At: 2026-10-26T12:34:56+08:00
//	Completed At: 2026-10-26T12:34:57+08:00
//	The command exited with code 0.
//	Output:
//	total 12
//
// Two messages per step. The first says what the user did, the second
// carries the step's output verbatim. A step the *user* did not perform gets
// the same pair with two other lead-ins — the reference asks the step's
// string converter for the input and for the output and puts the two into
// the two messages.
package cumulative

import (
	"fmt"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/agystep"
	"github.com/justwasm/pigasus/internal/compaction"
	"github.com/justwasm/pigasus/internal/cortex_pb"
	"github.com/justwasm/pigasus/internal/implicit"
)

// RoleUser is the role the reference carries the trajectory's messages in.
//
// 【反编译】 the message the converter builds is a
// `chat_go_proto.ChatMessagePrompt` (408 B) and its `source` field — the
// int32 at +0x18 — is `ChatMessageSource_USER` (1). The type descriptor's
// own address is not quoted here: Go's namedata layout cannot be resolved
// statically out of the binary, so the address would be a guess.
// It becomes `ChatMessageSource_CONTEXT` (6) only when the converter's
// `enable_context_role` flag is on *and* the step's payload is neither a
// user input (payload case 19) nor a plan input (8). That flag is not a
// field of the prompt config either — it belongs to the converter's own
// config, `exa.cortex_pb.TrajectoryConversionConfig` (field 12). The
// current turn's own user message is built with the same source
// (`prompt.ConstructChatMessagesForChatCompletion`, 0x101b2b7b0), so the
// trajectory's messages travel in the user-content position, like the
// user's own message.
const RoleUser = "user"

// The reference's own phrases.
//
// 【反编译】 each is a `fmt.Sprintf` format string in agy 1.4.8:
//
//	"The USER performed the following action:\n%s"  0x1035fed83, 43 B
//	"The following action was requested:\n%s"       0x1035e05fb, 38 B
//	"The action produced the following result:\n%s"  0x1036045eb, 44 B
//	"Step Id: %d\n"                                  0x10347022b, 12 B
//	"Step Id: %d\n%s"                               0x10349ff2e, 14 B
//	"Created At: %s\n"                              0x1034b540d, 15 B
//	"Completed At: %s\n"                            0x1034ddd83, 17 B
//	"Command: %s"                                   0x10346ca3c, 11 B
//	"CWD: %s"                                       0x103406ac8,  7 B
const (
	// userActionPrefix introduces the message that says what the user did.
	userActionPrefix = "The USER performed the following action:\n"

	// actionRequestedPrefix introduces the message that says what was asked
	// for, for a step the user did not perform.
	actionRequestedPrefix = "The following action was requested:\n"

	// actionResultPrefix introduces the message that says what an action
	// produced, for a step the user did not perform.
	actionResultPrefix = "The action produced the following result:\n"

	// stepIDFormat is the header the handler writes on every message it
	// converts.
	stepIDFormat = "Step Id: %d\n"

	// createdAtFormat and completedAtFormat are the timestamp lines the
	// converter puts in front of every message it builds.
	createdAtFormat   = "Created At: %s\n"
	completedAtFormat = "Completed At: %s\n"

	// commandFormat is the reference's own label for the command a step
	// ran, and cwdFormat its label for the directory it ran in.
	commandFormat = "Command: %s"
	cwdFormat     = "CWD: %s"
)

// timestampLayout is the reference's own time layout.
//
// 【反编译】 the literal "2006-01-02T15:04:05Z07:00" (25 B) is the format
// string the converter hands to time.Time.Format.
const timestampLayout = "2006-01-02T15:04:05Z07:00"

// truncationNote is what the reference appends to a step's text when the
// per-step token limit cuts it.
//
// 【反编译】 the 135-byte literal at 0x10369e4b6, concatenated onto the
// truncated text by `runtime.concatstring2` in
// `(*TrajectoryChatConverter).truncateStringToTokenLimit` (0x101518bf0).
const truncationNote = "\n\nNOTE: The output was truncated because it was too long. " +
	"Use a more targeted query or a smaller range to get the information you need."

// Message is one chat message the cumulative prompt carries.
type Message struct {
	// Role is the message's role — RoleUser.
	Role string
	// Content is the message's text.
	Content string
}

// Options configures a conversion.
type Options struct {
	// From is the number of trajectory steps an earlier prompt already
	// carried. The conversion starts at that index.
	//
	// The reference tracks the same thing: its handler caches the messages
	// it has already converted (`SetPersistentCachedContext`) and converts
	// only the new steps (`getNewPersistentChatMessagePrompts`).
	From int

	// AllowedStepTypes is the reference's `allowed_implicit_step_types` —
	// the map from a Cortex step type's *name* to whether that type may
	// contribute.
	//
	// 【一手】 the reference's config declares it as `map<string, bool>`
	// (exa.prompt_pb.CumulativePromptConfig, field 22), keyed by the step
	// type's *name* — "CORTEX_STEP_TYPE_RUN_COMMAND", not the number. The
	// clone keys the same way.
	//
	// 【反编译】 `prompt.shouldFilterTrajectoryStep` (0x101b2d7c0) looks the
	// step's type up in the map, by name, and filters the step unless the
	// entry is *present and true*. A type that is absent and a type that is
	// present-and-false are the same thing to the reference: both are
	// filtered.
	//
	// nil means the reference's own default gate —
	// defaultAllowedStepTypes. An empty map filters everything.
	AllowedStepTypes map[string]bool

	// StepIndexIncrement is the reference's `trajectory_index_increment`.
	//
	// 【反编译】 the cumulative-prompt config's field 25, a plain int32.
	// The config the reference builds when the server-side flag is absent
	// (`prompt.ReadCumulativePromptConfigFromUnleash`, 0x101b2dfd0) zeroes
	// it explicitly, and the id a step gets is then its own index in the
	// trajectory.
	StepIndexIncrement int

	// TrajectoryStartIndex is the reference's `trajectory_start_index`.
	//
	// 【反编译】 the cumulative-prompt config's field 24, a proto3
	// optional int32 — so it is a pointer, and "unset" is a value of its
	// own. The reference's default config leaves it unset: the builder
	// writes nil to the config's +0x88.
	//
	// When it is set, the id a step gets is
	// `start + index − CachedMessages`, not the step's own index.
	TrajectoryStartIndex *int

	// CachedMessages is the number of messages an earlier prompt already
	// carried — the handler's cached-message count.
	//
	// 【反编译】 the handler writes it into the field the renumbering
	// reads (the handler's +0x60) when it caches the messages it has
	// already converted.
	CachedMessages int

	// MaxTokensPerStep is the reference's `max_tokens_per_step` — the
	// per-step token limit the converter cuts a step's text against.
	//
	// 【反编译】 `(*TrajectoryChatConverter).getMaxTokensForStep`
	// (0x101518b40) reads it from the config, and the user-input and
	// plan-input steps use `max_tokens_per_user_input_step` instead. The
	// limit is not a field of the prompt config — it belongs to the
	// converter's own config, `exa.cortex_pb.TrajectoryConversionConfig`
	// (field 13). The reference's default leaves both at zero, and a limit
	// below one makes `truncateStringToTokenLimit` leave the text alone —
	// so the path is inert until the server sends a limit.
	MaxTokensPerStep int
}

// runCommandStepType is the name of the step type the clone's store holds —
// the key the reference's gate is keyed by.
//
// 【一手】 the reference's `allowed_implicit_step_types` is a
// `map<string, bool>` keyed by the step type's *name*, not by its number.
var runCommandStepType = cortex_pb.CortexStepTypeRunCommand.String()

// defaultAllowedStepTypes is the reference's own default gate — the
// `allowed_implicit_step_types` map the default cumulative-prompt config
// carries.
//
// 【反编译】 `prompt.map.init.0` (0x101b2b2f0) builds the map at init time
// and `prompt.ReadCumulativePromptConfigFromUnleash` (0x101b2dfd0) copies
// the built map into the config's field 22. Eight entries, every one of
// them true: the code action, the git commit, the grep search, the view
// file, the run command, the find, the command status and the clipboard.
//
// The clone's own store holds the run command only, so the other seven
// entries never fire here — but the gate is the reference's, not a guess.
var defaultAllowedStepTypes = map[string]bool{
	"CORTEX_STEP_TYPE_CODE_ACTION":    true,
	"CORTEX_STEP_TYPE_GIT_COMMIT":     true,
	"CORTEX_STEP_TYPE_GREP_SEARCH":    true,
	"CORTEX_STEP_TYPE_VIEW_FILE":      true,
	"CORTEX_STEP_TYPE_RUN_COMMAND":    true,
	"CORTEX_STEP_TYPE_FIND":           true,
	"CORTEX_STEP_TYPE_COMMAND_STATUS": true,
	"CORTEX_STEP_TYPE_CLIPBOARD":      true,
}

// Convert turns the trajectory's steps into the chat messages the next
// prompt carries.
//
// A step the user performed becomes two messages: the first says what the
// user did, the second carries the step's output verbatim.
func Convert(steps []implicit.Step, opts Options) []Message {
	if opts.From < 0 {
		opts.From = 0
	}
	if opts.From > len(steps) {
		opts.From = len(steps)
	}
	allowed := opts.AllowedStepTypes
	if allowed == nil {
		allowed = defaultAllowedStepTypes
	}
	var out []Message
	for i := opts.From; i < len(steps); i++ {
		if !allowed[runCommandStepType] {
			continue
		}
		step := steps[i]
		header := stepHeader(stepID(opts, i), step.CreatedAt, step.CompletedAt)
		input := truncateToTokenLimit(inputString(step), opts.MaxTokensPerStep)
		output := truncateToTokenLimit(outputString(step), opts.MaxTokensPerStep)
		out = append(out,
			Message{Role: RoleUser, Content: header + userActionPrefix + input},
			Message{Role: RoleUser, Content: header + output},
		)
	}
	return out
}

// stepID is the id the reference writes in the header.
//
// 【反编译】 the handler's renumbering, at 0x10225b3b8–0x10225b3dc:
//
//	ldr  x6, [x5, #0x110]     ; the message's own step id
//	ldr  x7, [sp, #0x240]     ; the config the handler holds
//	ldr  x8, [x7, #0x68]     ; the start index — an optional int32
//	cbz  x8, 10225b3d8
//	ldr  x2, [x8]            ; *start index
//	add  x2, x2, x6          ; start + stepIndex
//	ldr  x3, [sp, #0x58]     ; the handler's cached-message count
//	sub  x6, x2, x3          ; start + stepIndex - cached
//	ldr  x1, [x7, #0x70]     ; the index increment
//	add  x0, x1, x6          ; increment + (start + stepIndex - cached)
//
// So the id is the step's own index in the trajectory, plus the increment,
// and — when the start index is set — less the number of messages an
// earlier prompt already carried.
//
// 【反编译】 the two knobs are the cumulative-prompt config's
// `trajectory_start_index` (24, a proto3 optional int32) and
// `trajectory_index_increment` (25). The handler does not read the proto
// itself: the proto's own layout puts the two fields at +0x88 and +0x90
// (the generated getters, 0x10090d470 and 0x10090d490), while the derived
// config the handler holds at handler+0x248 puts them at +0x68 and +0x70.
// Same two knobs, a different struct.
//
// 【反编译】 the reference's default config
// (`prompt.ReadCumulativePromptConfigFromUnleash`, 0x101b2dfd0) leaves the
// start index unset and the increment at zero — it writes nil to the proto's
// +0x88 and zero to its +0x90. The two globals the builder reads
// (0x10b5b0048 and 0x10b5b0050) are *not* those two knobs: they are the
// len and the cap of the default `allowed_cascade_step_types` slice, and
// both are zero — the default config allows no cascade step type at all.
func stepID(opts Options, index int) int {
	id := index
	if opts.TrajectoryStartIndex != nil {
		id = *opts.TrajectoryStartIndex + index - opts.CachedMessages
	}
	return id + opts.StepIndexIncrement
}

// stepHeader is the block the reference's converter puts in front of every
// message it builds.
//
// 【反编译】 the converter's closure concatenates, in this order,
// "Step Id: %d\n", "Created At: %s\n" and "Completed At: %s\n" onto the
// message's own text. A step with no timestamp gets no line for it — the
// reference tests the timestamp's presence before formatting it.
func stepHeader(index int, created, completed time.Time) string {
	var b strings.Builder
	fmt.Fprintf(&b, stepIDFormat, index)
	if !created.IsZero() {
		fmt.Fprintf(&b, createdAtFormat, created.Local().Format(timestampLayout))
	}
	if !completed.IsZero() {
		fmt.Fprintf(&b, completedAtFormat, completed.Local().Format(timestampLayout))
	}
	return b.String()
}

// inputString is the step's input string — what the command was.
//
// 【反编译】 `code.(*RunCommandStringConverter).ToInputString` (0x101423100):
// the command is the *proposed* command line, or the command line when the
// proposed one is empty, or the args joined with a space when both are
// empty; the text is "Command: %s", and "CWD: %s" on a second line when the
// command has a working directory. The two parts are joined with a newline.
//
// The clone's store records the command in both fields, so the first two
// cases are the same text here; the args case cannot fire, because the
// clone's store does not record the args.
func inputString(step implicit.Step) string {
	rc := step.RunCommand
	command := rc.ProposedCommandLine
	if command == "" {
		command = rc.CommandLine
	}
	out := fmt.Sprintf(commandFormat, command)
	if rc.CWD != "" {
		out += "\n" + fmt.Sprintf(cwdFormat, rc.CWD)
	}
	return out
}

// outputString is the step's output string — the reference's own run_command
// step string.
//
// 【一手】 the template is the reference's: `step_strings/run_command.tmpl`
// names itself "Template for RunCommandStringConverter.ToOutputString", and
// the clone renders it from internal/agystep.
func outputString(step implicit.Step) string {
	rc := step.RunCommand
	payload := agystep.NewRunCommand(agystep.RunCommandOptions{
		ProposedCommandLine: rc.ProposedCommandLine,
		CommandLine:         rc.CommandLine,
		CommandID:           rc.CommandID,
		CombinedOutput:      rc.CombinedOutput,
		ExitCode:            rc.ExitCode,
	})
	model := agystep.NewStep(agystep.StepOptions{
		Status:     stepStatus(step.Status),
		Source:     agystep.SourceUserExplicit,
		RunCommand: payload,
	})
	return agystep.RunCommandString(model)
}

// truncateToTokenLimit cuts a step's text at the per-step token limit.
//
// 【反编译】 `(*TrajectoryChatConverter).truncateStringToTokenLimit`
// (0x101518bf0): a limit below one leaves the text alone; otherwise the
// text's token count decides, and a text over the limit is cut to
// `limit × 3` bytes and gets the reference's own note appended.
//
// The three bytes per token are the reference's own divisor — the same one
// the clone's compaction.EstimateTokens uses.
func truncateToTokenLimit(s string, limit int) string {
	if limit < 1 || compaction.EstimateTokens(s) <= limit {
		return s
	}
	return truncateTail(s, limit*compaction.BytesPerToken) + truncationNote
}

// truncateTail keeps the tail of a text, cut back to its last line boundary.
//
// 【反编译】 `go_utils.truncateFromEnd` (0x1007f8050): the text is cut to
// maxLen bytes from the front, and when the cut does not land on a line
// boundary the remainder is cut back to its last newline, with a carriage
// return before it dropped. A tail with no newline at all is empty.
//
// 【反编译】 the reference's own call in `truncateStringToTokenLimit` passes
// no truncation-priority option, so the priority is the default — zero —
// and the default cuts from the end.
func truncateTail(s string, maxLen int) string {
	if len(s) <= maxLen {
		return s
	}
	cut := len(s) - maxLen
	tail := s[cut:]
	if s[cut] == '\n' {
		return tail
	}
	idx := strings.LastIndex(tail, "\n")
	if idx < 0 {
		return ""
	}
	end := idx
	if end > 0 && tail[end-1] == '\r' {
		end--
	}
	return tail[:end]
}

// stepStatus maps the store's status onto the step-string model's.
func stepStatus(status int) agystep.Status {
	switch status {
	case implicit.StepStatusCanceled:
		return agystep.StatusCanceled
	case implicit.StepStatusDone:
		return agystep.StatusDone
	default:
		return agystep.StatusUnspecified
	}
}

// ActionRequestedPrefix and ActionResultPrefix are the lead-ins the
// reference uses for a step the user did not perform.
//
// The clone's store holds only steps the user performed — a `!` command —
// so the conversion never emits them. They are exported because the pair is
// the reference's shape and a reader of the report needs to see it.
var (
	// ActionRequestedPrefix introduces the message that says what was asked
	// for.
	ActionRequestedPrefix = actionRequestedPrefix
	// ActionResultPrefix introduces the message that says what an action
	// produced.
	ActionResultPrefix = actionResultPrefix
)
