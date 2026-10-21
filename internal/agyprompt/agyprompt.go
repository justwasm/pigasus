// Package agyprompt reproduces the system prompt the Antigravity CLI sends.
//
// The text was frozen from a real `agy` 1.4.2 request captured by the cycle-274
// wrapper (see testdata/capture_1.4.4.txt). The CLI composes ten XML-tagged
// sections in a fixed order; seven are byte-for-byte constant, two carry a
// generated list (subagents, skills) and one describes the host and the
// conversation (user_information).
//
//	<identity><artifacts><conversation_transcript><guidelines>
//	<communication_style><slash_commands><messaging><subagents>
//	<skills><user_information>
//
// agy sends tool declarations separately (13 of them, as
// functionDeclarations); they are NOT part of this text.
//
// Build is the only entry point. Options carries the dynamic inputs; pass the
// zero value's siblings (DefaultOptions) to get what the reference emits on a
// host with no custom subagents.
package agyprompt

import (
	"embed"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
)

//go:embed sections/*.txt
var sectionFS embed.FS

// Skill is one entry of the prompt's "Available skills" list.
type Skill struct {
	Name        string
	Path        string // the skill's SKILL.md
	Description string
}

// Subagent is one entry of the prompt's "Available subagents" list.
type Subagent struct {
	Name        string
	Description string
}

// Options is the dynamic input of the prompt. The zero value is not valid:
// use DefaultOptions and override what differs.
type Options struct {
	// OSVersion is what agy prints in "The USER's OS version is %s." — it uses
	// short names ("mac", "linux"), not uname strings.
	OSVersion string
	// Workspaces are the absolute workspace roots. agy prints each as a
	// bullet; the capture shows exactly one.
	Workspaces []string
	// CWD is the command working directory.
	CWD string
	// AppDataDir is the agy config home (~/.gemini/antigravity-cli).
	AppDataDir string
	// ConversationID identifies the conversation that owns ArtifactDir.
	ConversationID string
	// ArtifactDir defaults to AppDataDir/brain/ConversationID when empty.
	ArtifactDir string
	// Skills populate the "Available skills" list, in agy's order.
	Skills []Skill
	// Subagents populate the "Available subagents" list. The reference always
	// lists at least self, research and image-generator.
	Subagents []Subagent
	// UserRules populate the <user_rules> section: AGENTS.md / GEMINI.md
	// files discovered from the workspace and the global config home.
	UserRules []UserRule
}

// sectionOrder is the order agy emits its tags in.
var sectionOrder = []string{
	"identity",
	"artifacts",
	"conversation_transcript",
	"guidelines",
	"communication_style",
	"slash_commands",
	"messaging",
	"subagents",
	"user_rules",
	"skills",
	"user_information",
}

// Build renders the full prompt. Sections are joined with a single newline and
// the result has no trailing newline, matching the captured request byte for
// byte (see TestBuildMatchesCapture).
func Build(o Options) string {
	bodies := sectionBodies(o)

	var b strings.Builder
	first := true
	for _, name := range sectionOrder {
		if name == "user_rules" && len(o.UserRules) == 0 {
			continue
		}
		if !first {
			b.WriteString("\n")
		}
		first = false
		b.WriteString("<")
		b.WriteString(name)
		b.WriteString(">")
		b.WriteString(bodies[name])
		b.WriteString("</")
		b.WriteString(name)
		b.WriteString(">")
	}
	return b.String()
}

// sectionBodies renders every section body once. Build and SectionBody share
// it so the /context token breakdown measures exactly the bytes Build emits.
func sectionBodies(o Options) map[string]string {
	return map[string]string{
		"identity":                mustSection("identity.txt"),
		"artifacts":               mustSection("artifacts.txt"),
		"conversation_transcript": mustSection("conversation_transcript.txt"),
		"guidelines":              mustSection("guidelines.txt"),
		"communication_style":     mustSection("communication_style.txt"),
		"slash_commands":          mustSection("slash_commands.txt"),
		"messaging":               mustSection("messaging.txt"),
		"subagents":               renderSubagents(o.Subagents),
		"user_rules":              renderUserRules(o.UserRules),
		"skills":                  renderSkills(o.Skills),
		"user_information":        renderUserInformation(o),
	}
}

// SectionBody returns the body of one XML section exactly as Build renders it.
// The body excludes the opening and closing tags. Callers that need the full
// section size should add len("<name>")+len("</name>").
func SectionBody(o Options, name string) string {
	return sectionBodies(o)[name]
}

// agentSectionOrder is the order the reference emits the sections of a custom
// markdown agent's prompt (captured from a 1.4.3 `--agent foo -p` request).
// It is a different prompt, not the default prompt with the identity replaced:
// agent/artifacts/transcript/slash_commands/subagents are gone, and the
// remaining sections are reordered.
var agentSectionOrder = []string{
	"user_information",
	"skills",
	"messaging",
	"artifacts",
	"guidelines",
	"communication_style",
}

// BuildAgent renders the system prompt for a markdown custom agent. The body
// is the agent file's Markdown body; the reference wraps it in a literal
// `<System Prompt>` tag (with a space) and sends six sections in
// agentSectionOrder.
//
// Two differences from Build beyond the section set, both captured:
//   - `<messaging>` omits the subagent/peer-agent clauses, because a custom
//     agent cannot receive those messages.
//   - `<skills>` omits the CLI's built-in skills (everything under
//     <AppDataDir>/builtin/skills); plugin and user skills stay.
func BuildAgent(body string, o Options) string {
	skills := o.Skills
	if o.AppDataDir != "" {
		builtin := filepath.Join(o.AppDataDir, "builtin", "skills")
		filtered := make([]Skill, 0, len(skills))
		for _, s := range skills {
			if strings.HasPrefix(filepath.Clean(s.Path), builtin+string(filepath.Separator)) {
				continue
			}
			filtered = append(filtered, s)
		}
		skills = filtered
	}

	bodies := map[string]string{
		"user_information":    renderUserInformation(o),
		"skills":              renderSkills(skills),
		"messaging":           mustSection("agent_messaging.txt"),
		"artifacts":           mustSection("artifacts.txt"),
		"guidelines":          mustSection("guidelines.txt"),
		"communication_style": mustSection("communication_style.txt"),
	}

	var b strings.Builder
	b.WriteString("<System Prompt>\n")
	b.WriteString(strings.TrimSpace(body))
	b.WriteString("\n</System Prompt>\n")
	for i, name := range agentSectionOrder {
		b.WriteString("<")
		b.WriteString(name)
		b.WriteString(">")
		b.WriteString(bodies[name])
		b.WriteString("</")
		b.WriteString(name)
		b.WriteString(">")
		if i < len(agentSectionOrder)-1 {
			b.WriteString("\n")
		}
	}
	return b.String()
}

// renderSubagents mirrors agy's list rendering: a fixed head, one "- name:
// description" line per subagent, then the fixed tail.
func renderSubagents(subagents []Subagent) string {
	head := mustSection("subagents_head.txt")
	tail := mustSection("subagents_tail.txt")
	lines := make([]string, 0, len(subagents))
	for _, s := range subagents {
		lines = append(lines, "- "+s.Name+": "+s.Description)
	}
	return head + strings.Join(lines, "\n") + tail
}

// renderSkills mirrors agy's list rendering: a fixed preamble, one
// "- name (path): description" line per skill, then the fixed tail. A
// description may itself contain newlines; agy prints it verbatim and so do we.
func renderSkills(skills []Skill) string {
	head := mustSection("skills_head.txt")
	tail := mustSection("skills_tail.txt")
	lines := make([]string, 0, len(skills))
	for _, s := range skills {
		lines = append(lines, "- "+s.Name+" ("+s.Path+"): "+s.Description)
	}
	return head + strings.Join(lines, "\n") + tail
}

// renderUserInformation is the one section agy builds entirely from the host.
func renderUserInformation(o Options) string {
	artifactDir := o.ArtifactDir
	if artifactDir == "" && o.AppDataDir != "" {
		artifactDir = filepath.Join(o.AppDataDir, "brain", o.ConversationID)
	}

	var b strings.Builder
	b.WriteString("\nThe USER's OS version is ")
	b.WriteString(o.OSVersion)
	b.WriteString(".\nActive Workspaces:\n")
	for _, w := range o.Workspaces {
		b.WriteString("- ")
		b.WriteString(w)
		b.WriteString("\n")
	}
	b.WriteString("Command Working Directory: ")
	b.WriteString(o.CWD)
	b.WriteString("\nCode relating to the user's requests should be written in the locations listed above. Avoid writing project code files to tmp, in the .gemini dir, or directly to the Desktop and similar folders unless explicitly asked.\nApp Data Directory: ")
	b.WriteString(o.AppDataDir)
	b.WriteString("\nConversation ID: ")
	b.WriteString(o.ConversationID)
	b.WriteString("\nArtifact Directory Path: ")
	b.WriteString(artifactDir)
	b.WriteString("\n")
	return b.String()
}

func mustSection(name string) string {
	b, err := sectionFS.ReadFile("sections/" + name)
	if err != nil {
		panic(fmt.Sprintf("agyprompt: embedded section %s: %v", name, err))
	}
	return string(b)
}

// OSName maps runtime.GOOS to the short name agy prints. agy prints "mac" for
// Darwin; the captured macOS request is the only one verified.
func OSName() string {
	switch runtime.GOOS {
	case "darwin":
		return "mac"
	case "windows":
		return "windows"
	default:
		return runtime.GOOS
	}
}

// BuiltinSubagents returns the three subagents the reference always advertises.
// The descriptions are verbatim from the 1.4.2 capture.
func BuiltinSubagents() []Subagent {
	return []Subagent{
		{
			Name: "self",
			Description: "Subagent that inherits the parent agent's full configuration including tools, system prompt, and model. " +
				"Use this when you need to run a task in a separate conversation context but with the same capabilities as the current agent.",
		},
		{
			Name: "research",
			Description: "Research subagent with read-only tools for exploring the codebase, searching the web, and reading files. " +
				"Delegate to this agent when you need to run a task in a separate conversation context but with the same capabilities as the current agent, " +
				"when a research task requires many search and file-reading steps that would clutter your context, or when you need a broad survey of the codebase or documentation. " +
				"Prefer doing research yourself for quick, targeted lookups.",
		},
		{
			Name:        "image-generator",
			Description: "Generates or edits images from a brief and replies with the saved file paths.",
		},
	}
}

// DefaultOptions is what the reference emits for a fresh single-workspace
// conversation: the builtin subagents, no custom skills, and the host facts.
func DefaultOptions(cwd, appDataDir, conversationID string) Options {
	return Options{
		OSVersion:      OSName(),
		Workspaces:     []string{cwd},
		CWD:            cwd,
		AppDataDir:     appDataDir,
		ConversationID: conversationID,
		Subagents:      BuiltinSubagents(),
	}
}
