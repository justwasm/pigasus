// Package harnessprompt — the system instruction the Antigravity harness
// sends to the model.
//
// # What this is
//
// The `localharness` binary owns the system instruction. It renders a
// sequence of `<title>\n{body}\n</title>` sections and sends the result
// as the Gemini API request's `systemInstruction`.
//
// The section set the reference ships is small and stable:
//
//	identity                 who the agent is
//	non_interactive_mode     what to do without a user in the loop
//	user_information         the machine, the workspaces, the directories
//	messaging                how background messages arrive
//	conversation_transcript  where the transcript lives and its format
//	communication_style      how to answer
//	artifacts                the artifact feature (interactive mode only)
//
// The section bodies are captured verbatim from the reference binary —
// see `out/localharness/` and the cycle report. They are embedded as
// files so the text stays readable and diffable.
//
// # Two identities
//
// The reference has two identity texts, and the difference is not
// cosmetic:
//
//   - The **default** prompt's identity is the agent's public description.
//   - The **template** identity (the one a `builtin_section: "identity"`
//     part renders) is the "Advanced Agentic Coding" variant.
//
// A client that replaces the whole system instruction with a
// `builtin_section` gets the template one; a client that appends to the
// default gets the default one. Both are reproduced here.
//
// # Behaviour
//
// `AgentBehavior` selects the prompt's shape:
//
//	unspecified / autonomous  the non-interactive prompt
//	interactive               the interactive prompt (no mode section,
//	                          the artifacts section, the interactive
//	                          communication-style line)
//	minimal                   the smallest prompt the reference has
//
// The minimal behaviour is not reproduced: the reference filters the
// prompt down to "core identity, guidelines, and communication style"
// and the clone keeps the autonomous prompt for it.
package harnessprompt

import (
	"embed"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"text/template"
)

//go:embed sections/*.txt
var sectionFS embed.FS

// Section names the reference accepts in a `builtin_section` part.
const (
	SectionIdentity               = "identity"
	SectionNonInteractiveMode     = "non_interactive_mode"
	SectionUserInformation        = "user_information"
	SectionSubagents              = "subagents"
	SectionMessaging              = "messaging"
	SectionConversationTranscript = "conversation_transcript"
	SectionCommunicationStyle     = "communication_style"
	SectionArtifacts              = "artifacts"
)

// BuiltinSections lists the section names a client may reference.
var BuiltinSections = []string{
	SectionIdentity,
	SectionNonInteractiveMode,
	SectionUserInformation,
	SectionSubagents,
	SectionMessaging,
	SectionConversationTranscript,
	SectionCommunicationStyle,
	SectionArtifacts,
}

// Behavior is the prompt's behavioural mode.
type Behavior string

// The behaviours the protocol's `AgentBehavior` enum carries.
const (
	BehaviorUnspecified Behavior = ""
	BehaviorAutonomous  Behavior = "autonomous"
	BehaviorInteractive Behavior = "interactive"
	BehaviorMinimal     Behavior = "minimal"
)

// Interactive reports whether the behaviour puts a user in the loop.
func (b Behavior) Interactive() bool { return b == BehaviorInteractive }

// Options describes the session a prompt is rendered for.
type Options struct {
	// Behavior selects the prompt shape.
	Behavior Behavior
	// Workspaces are the session's roots. The first one is the working
	// directory.
	Workspaces []string
	// AppDataDir is the client's application-data directory. It is the
	// parent of the `brain/<conversation-id>` artifact directory and of
	// the `scratch` directory the prompt points at.
	AppDataDir string
	// ConversationID identifies the conversation. It names the artifact
	// directory and the transcript.
	ConversationID string
	// OSName is the operating system the prompt names. The reference
	// writes "mac" where the SDK reports "darwin".
	OSName string
	// Identity overrides the identity section's body. The client sends
	// it as `SystemInstructions.appended.custom_identity`.
	Identity string
	// SubagentTool reports whether the session may invoke subagents.
	//
	// It is the reference's
	// `CascadeConfig.PlannerConfig.ToolConfig.InvokeSubagent.Enabled`,
	// and it decides two things: whether the prompt carries the
	// `<subagents>` section, and whether the messaging section counts
	// agents among the message sources.
	SubagentTool bool
	// MessageTool reports whether the session may deliver user-queued
	// messages. It is the reference's
	// `CascadeConfig.MessageConfig.Enabled`.
	MessageTool bool
	// Subagents are the subagents the prompt lists under "Available
	// subagents:".
	Subagents []Subagent
}

// DefaultAppDataDir is the directory the SDK defaults to when the client
// does not name one: `~/.gemini/antigravity`.
func DefaultAppDataDir() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ".gemini/antigravity"
	}
	return filepath.Join(home, ".gemini", "antigravity")
}

// Build renders the default system instruction.
//
// The sections are separated by a single newline and the last one carries
// no trailing newline — the reference's exact shape.
func Build(opts Options) string {
	sections := []string{
		wrapSection(SectionIdentity, identityBody(opts)),
	}
	if !opts.Behavior.Interactive() {
		sections = append(sections, wrapSection(SectionNonInteractiveMode, staticSection(SectionNonInteractiveMode)))
	}
	sections = append(sections, wrapSection(SectionUserInformation, userInformation(opts)))
	if opts.SubagentTool {
		sections = append(sections, wrapSection(SectionSubagents, subagentsBody(opts)))
	}
	sections = append(sections,
		wrapSection(SectionMessaging, messagingBody(opts)),
		wrapSection(SectionConversationTranscript, staticSection(SectionConversationTranscript)),
	)
	if opts.Behavior.Interactive() {
		sections = append(sections, wrapSection(SectionArtifacts, staticSection(SectionArtifacts)))
	}
	sections = append(sections, wrapSection(SectionCommunicationStyle, communicationStyle(opts)))
	return strings.Join(sections, "\n")
}

// subagentsBody renders the `<subagents>` section.
//
// The section has three parts: the reference's own text (which names the
// three tools), the list of the subagents the session may invoke, and the
// closing reminder.
//
// The text names the tools by the names the *model* sees, which are the
// harness's own: `invoke_subagent`, `define_subagent` and `send_message`.
func subagentsBody(opts Options) string {
	header := fmt.Sprintf(staticSection(SectionSubagents),
		"invoke_subagent", "define_subagent", "send_message")
	entries := make([]string, 0, len(opts.Subagents))
	for _, sub := range opts.Subagents {
		entries = append(entries, "- "+sub.Name+": "+sub.Description)
	}
	var b strings.Builder
	b.WriteString(header)
	b.WriteString("\n\nAvailable subagents:\n")
	b.WriteString(strings.Join(entries, "\n"))
	b.WriteString("\n\n")
	b.WriteString(staticSection("subagents_footer"))
	// The reference's body ends with a newline: the closing reminder
	// sits in its own paragraph, and the section's closing tag follows
	// a blank line.
	b.WriteString("\n")
	return b.String()
}

// messagingBody renders the `<messaging>` section.
//
// The reference renders it from a template with three holes: where
// messages come from, what wakes the agent up, and the closing remark.
// The first two depend on the session's two features — the subagent tool
// and the user-queued message — and the third is fixed.
func messagingBody(opts Options) string {
	text, err := renderMessaging(messagingData{
		Sources: messageSources(opts),
		Wakeup:  wakeupConditions(opts),
		Closing: messagingClosing,
	})
	if err != nil {
		return staticSection(SectionMessaging)
	}
	return text
}

// messagingClosing is the reference's closing remark.
//
// The binary carries two spellings of this sentence — one for the
// template-driven path ("After launching a task that runs in the
// background") and this one, which every captured prompt carries.
const messagingClosing = "After launching anything that performs work asynchronously, " +
	"you may continue other work or simply stop by calling no more tools."

// messageSources lists where the session's messages come from.
func messageSources(opts Options) string {
	switch {
	case opts.SubagentTool && opts.MessageTool:
		return "agents, background tasks, user-queued messages"
	case opts.SubagentTool:
		return "agents, background tasks"
	case opts.MessageTool:
		return "background tasks, user-queued messages"
	default:
		return "background tasks"
	}
}

// wakeupConditions lists what resumes the agent's execution.
func wakeupConditions(opts Options) string {
	lines := make([]string, 0, 3)
	if opts.SubagentTool {
		lines = append(lines, "- A message arrives from a subagent or peer agent")
	}
	lines = append(lines, "- A **background task** completes or sends you a notification")
	if opts.MessageTool {
		lines = append(lines, "- A **user-queued message** is ready to be dequeued")
	}
	return strings.Join(lines, "\n")
}

// messagingData is the data the messaging template reads.
type messagingData struct {
	// Sources lists where the session's messages come from.
	Sources string
	// Wakeup lists what resumes the agent's execution.
	Wakeup string
	// Closing is the remark after the wake-up list.
	Closing string
}

// Appended renders the default system instruction with the client's
// identity override and appended sections.
//
// `SystemInstructions.appended` is the "append to the defaults" form: the
// identity body is replaced and every section is appended at the end.
func Appended(opts Options, sections []AppendedSection) string {
	parts := []string{Build(opts)}
	for _, section := range sections {
		parts = append(parts, wrapSection(section.Title, section.Content))
	}
	return strings.Join(parts, "\n")
}

// AppendedSection is one section of the `appended` form.
type AppendedSection struct {
	Title   string
	Content string
}

// Custom renders a `custom` system instruction: the client's own parts,
// and nothing else.
//
// A `text` part becomes a `<System>` block; a `builtin_section` part
// becomes that section. The reference renders an unknown section name as
// an error (`unknown builtin prompt section: "<name>"`), so the caller
// gets the same signal.
func Custom(opts Options, parts []CustomPart) (string, error) {
	sections := make([]string, 0, len(parts))
	for _, part := range parts {
		switch {
		case part.Text != "":
			sections = append(sections, wrapSection("System", part.Text))
		case part.BuiltinSection != "":
			body, ok := builtinBody(part.BuiltinSection, opts)
			if !ok {
				return "", fmt.Errorf("unknown builtin prompt section: %q", part.BuiltinSection)
			}
			sections = append(sections, wrapSection(part.BuiltinSection, body))
		case part.Template != "":
			// The reference resolves a named template against its own
			// template set. The clone has no template server, so the
			// part is skipped rather than guessed at.
			continue
		}
	}
	return strings.Join(sections, "\n"), nil
}

// CustomPart is one part of a `custom` system instruction.
type CustomPart struct {
	Text           string
	BuiltinSection string
	Template       string
}

// builtinBody returns the body of a builtin section.
func builtinBody(name string, opts Options) (string, bool) {
	switch name {
	case SectionIdentity:
		return staticSection("template_identity"), true
	case SectionNonInteractiveMode:
		return staticSection(SectionNonInteractiveMode), true
	case SectionUserInformation:
		return userInformation(opts), true
	case SectionSubagents:
		return subagentsBody(opts), true
	case SectionMessaging:
		return messagingBody(opts), true
	case SectionConversationTranscript:
		return staticSection(SectionConversationTranscript), true
	case SectionCommunicationStyle:
		return communicationStyle(opts), true
	case SectionArtifacts:
		return staticSection(SectionArtifacts), true
	}
	return "", false
}

// identityBody returns the identity section's body, honouring the
// client's identity override.
func identityBody(opts Options) string {
	if strings.TrimSpace(opts.Identity) != "" {
		return opts.Identity
	}
	return staticSection(SectionIdentity)
}

// communicationStyle returns the communication-style body for the mode.
func communicationStyle(opts Options) string {
	if opts.Behavior.Interactive() {
		return staticSection("communication_style_interactive")
	}
	return staticSection("communication_style_autonomous")
}

// userInformation renders the session's machine description.
//
// The reference's shape:
//
//	The USER's OS version is mac.
//	Active Workspaces:
//	- /path/one
//	- /path/two
//	Command Working Directory: /path/one
//	Code relating to the user's requests should be written in the
//	locations listed above. …
//	App Data Directory: /home/user/.gemini/antigravity
//	Conversation ID: <id>
//	Artifact Directory Path: /home/user/.gemini/antigravity/brain/<id>
//
// The workspace block is replaced by a paragraph that points at the
// scratch directory when the session has no workspace, and the artifact
// line appears only in interactive mode.
func userInformation(opts Options) string {
	var b strings.Builder
	fmt.Fprintf(&b, "The USER's OS version is %s.\n", osLabel(opts.OSName))
	if len(opts.Workspaces) > 0 {
		b.WriteString("Active Workspaces:\n")
		for _, workspace := range opts.Workspaces {
			fmt.Fprintf(&b, "- %s\n", workspace)
		}
		fmt.Fprintf(&b, "Command Working Directory: %s\n", opts.Workspaces[0])
	} else {
		fmt.Fprintf(&b,
			"The user does not have any active workspace. If the user's request involves creating a new project, "+
				"you should create a reasonable subdirectory inside the default project directory at %s. "+
				"If you do this, you should also recommend the user to set that subdirectory as the active workspace.\n",
			filepath.Join(opts.AppDataDir, "scratch"))
	}
	b.WriteString("Code relating to the user's requests should be written in the locations listed above. " +
		"Avoid writing project code files to tmp, in the .gemini dir, or directly to the Desktop and similar folders " +
		"unless explicitly asked.\n")
	fmt.Fprintf(&b, "App Data Directory: %s\n", opts.AppDataDir)
	fmt.Fprintf(&b, "Conversation ID: %s\n", opts.ConversationID)
	if opts.Behavior.Interactive() {
		fmt.Fprintf(&b, "Artifact Directory Path: %s\n", artifactDir(opts))
	}
	return strings.TrimRight(b.String(), "\n")
}

// artifactDir is the conversation's artifact directory.
func artifactDir(opts Options) string {
	return filepath.Join(opts.AppDataDir, "brain", opts.ConversationID)
}

// osLabel maps the SDK's operating-system name onto the one the prompt
// uses. The SDK reports "darwin"; the prompt says "mac".
func osLabel(name string) string {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "darwin", "mac", "macos", "osx":
		return "mac"
	case "":
		return "mac"
	default:
		return strings.ToLower(strings.TrimSpace(name))
	}
}

// wrapSection renders one `<title>\n{body}\n</title>` block.
func wrapSection(title, body string) string {
	return "<" + title + ">\n" + body + "\n</" + title + ">"
}

// staticSection reads one embedded section body.
//
// The section files hold the body exactly as the reference rendered it,
// with the file's own trailing newline removed. A body may itself end in a
// blank line — the reference's `conversation_transcript` does — so only
// one newline is stripped.
func staticSection(name string) string {
	data, err := sectionFS.ReadFile("sections/" + name + ".txt")
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(string(data), "\n")
}

// messagingTemplate is the reference's own messaging template, parsed
// once.
var messagingTemplate = template.Must(template.New("messaging").Parse(
	strings.TrimSuffix(sectionText(SectionMessaging), "\n"),
))

// renderMessaging renders the reference's messaging template.
func renderMessaging(data messagingData) (string, error) {
	var b strings.Builder
	if err := messagingTemplate.Execute(&b, data); err != nil {
		return "", err
	}
	return b.String(), nil
}

// sectionText reads one embedded section file verbatim.
func sectionText(name string) string {
	data, err := sectionFS.ReadFile("sections/" + name + ".txt")
	if err != nil {
		return ""
	}
	return string(data)
}
