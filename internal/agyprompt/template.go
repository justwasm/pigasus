// template.go — the reference's own prompt templates, rendered.
//
// The reference does NOT carry the system prompt as a frozen string. It
// carries Go text/template sources under
// google3/third_party/jetski/prompt/template_provider/templates/ and renders
// the prompt from them at request time; the "static" sections the clone used
// to hardcode are one instantiation of those templates with a particular set
// of flags.
//
// The files under templates/ are the reference's, recovered verbatim from the
// //go:embed file system of the Antigravity CLI binary (agy 1.4.7) — see
// internal/agytemplates/templates/PROVENANCE.md for the extraction recipe and
// the per-file digests. They are used here as-is: this file only supplies the
// data model the templates read and the section framing around them. The
// bundle itself lives in internal/agytemplates, which embeds it once and
// hands it to this package and to internal/agystep.
//
// The data model below is the reverse of the field references the recovered
// templates make. It is not the reference's own Go type — that one lives in
// the closed-source monorepo — but it has to answer the same field names,
// because the templates are the contract.
//
//	.IsAutonomous
//	.CascadeConfig.GetPlannerConfig.GetSupportsLatexRendering
//	.CascadeConfig.GetPlannerConfig.GetPlanModel.String
//	.ExperimentEnabled (called with a name)
//	.ArtifactDirectoryPath  .SystemGeneratedLogsPath
//	.KnowledgeDirectoryPath .MetadataFileName
//	.Data (per template: a list, or a struct with Skills/PreloadedSkills/
//	       TruncatedPrompt)
package agyprompt

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/justwasm/pigasus/internal/agytemplates"
)

// newTemplateSet parses the recovered templates with the function map the
// reference's own engine provides.
//
// Four of the templates read a bare identifier — "{{ArtifactDirectoryPath}}",
// "{{ SystemGeneratedLogsPath }}", "{{KnowledgeDirectoryPath}}",
// "{{MetadataFileName}}" — with no leading dot, which in a Go template is a
// *function call*, not a field. The reference's engine therefore registers
// them as functions; so does this one. They are bound to the data of the
// render, which is why the set is built per prompt rather than once.
//
// The set holds every template at once. They have to share one set:
// helpers/errors.tmpl defines "templateError" and
// step_strings/helpers/default_status_output_string.tmpl defines
// "defaultStatusOutputString", and other templates call both by name.
//
// The bundle itself lives in internal/agytemplates, which embeds it once
// and hands it to the two consumers — this package for the prompt sections
// and internal/agystep for the step strings.
func newTemplateSet(data templateData) *template.Template {
	return agytemplates.NewSet(template.FuncMap{
		"ArtifactDirectoryPath":   func() string { return data.ArtifactDirectoryPath },
		"SystemGeneratedLogsPath": func() string { return data.SystemGeneratedLogsPath },
		"KnowledgeDirectoryPath":  func() string { return data.KnowledgeDirectoryPath },
		"MetadataFileName":        func() string { return data.MetadataFileName },
	})
}

// renderTemplate renders one recovered template. A failure means the
// embedded bundle and this file disagree about the data model, which is a
// programming error and not a runtime condition — the same treatment
// mustSection gives a missing section file.
func renderTemplate(set *template.Template, name string, data templateData) string {
	var buf bytes.Buffer
	if err := set.ExecuteTemplate(&buf, name, data); err != nil {
		panic(fmt.Sprintf("agyprompt: render %s: %v", name, err))
	}
	return buf.String()
}

// templateData is the data every recovered template is rendered against.
//
// The fields the templates read but the shipped prompt does not use
// (KnowledgeDirectoryPath, MetadataFileName) belong to the reference's
// test_funcmap.tmpl, a self-check template that renders the four directory
// names. They are carried so that template renders too.
type templateData struct {
	IsAutonomous            bool
	CascadeConfig           cascadeConfig
	ArtifactDirectoryPath   string
	SystemGeneratedLogsPath string
	KnowledgeDirectoryPath  string
	MetadataFileName        string
	// experiments backs ExperimentEnabled. It has to be a method and not a
	// field: a Go template can only call a *field* of function type without
	// arguments, and the templates call it with one.
	experiments map[string]bool
	// Data is the per-template payload. Its shape depends on the template:
	// slash_commands.tmpl ranges over a list of commands, skills.tmpl reads
	// a struct of lists, plugins.tmpl ranges over a list of plugins.
	Data any
}

// ExperimentEnabled reports whether a server-side experiment is on. The
// reference's templates ask about "enable-url-artifacts"; the clone has no
// server, so the answer comes from Options.Experiments.
func (d templateData) ExperimentEnabled(name string) bool {
	return d.experiments[name]
}

// cascadeConfig mirrors the reference's cascade configuration, which the
// templates reach through two chained getters.
type cascadeConfig struct {
	planner plannerConfig
}

func (c cascadeConfig) GetPlannerConfig() plannerConfig { return c.planner }

// plannerConfig holds the two planner flags the templates branch on.
type plannerConfig struct {
	latex     bool
	planModel string
}

func (p plannerConfig) GetSupportsLatexRendering() bool { return p.latex }

func (p plannerConfig) GetPlanModel() planModel { return planModel{String: p.planModel} }

// planModel is the reference's plan-model enum. The templates compare its
// String against a list of MODEL_PLACEHOLDER_* names; a plain string field
// keeps that comparison textual.
type planModel struct {
	String string
}

// The reference names the artifact directory and the transcript logs
// directory with placeholders, not with the real paths — the real paths are
// in <user_information>. Both literals are in the reference binary next to
// the templates (【一手】).
const (
	defaultArtifactDirectoryPath = "<appDataDir>/brain/<conversation-id>"
	defaultTranscriptLogsPath    = "<appDataDir>/brain/<conversation-id>/.system_generated/logs"
)

// templateOptions turns the prompt options into the template data. An empty
// path means "the reference's literal"; a nil slash-command list means the
// reference's own nine.
func (o Options) templateOptions() templateData {
	artifactPath := o.ArtifactDirectoryPath
	if artifactPath == "" {
		artifactPath = defaultArtifactDirectoryPath
	}
	logsPath := o.TranscriptLogsPath
	if logsPath == "" {
		logsPath = defaultTranscriptLogsPath
	}
	commands := o.SlashCommands
	if commands == nil {
		commands = DefaultSlashCommands()
	}
	return templateData{
		IsAutonomous:            o.Autonomous,
		CascadeConfig:           cascadeConfig{planner: plannerConfig{latex: o.SupportsLatex, planModel: o.PlanModel}},
		experiments:             o.Experiments,
		ArtifactDirectoryPath:   artifactPath,
		SystemGeneratedLogsPath: logsPath,
		KnowledgeDirectoryPath:  o.KnowledgeDirectoryPath,
		MetadataFileName:        o.MetadataFileName,
	}
}

// SlashCommand is one entry of the prompt's "Available slash commands you
// can recommend to the user" list. The reference renders the list from a
// runtime registry; the template only formats the entries.
type SlashCommand struct {
	Name           string
	Recommendation string
}

// DefaultSlashCommands is the list the reference sends for a default
// conversation. The text is frozen from the 1.4.6 capture
// (testdata/capture_1.4.6.txt); the reference itself keeps the list in a
// runtime registry, which the recovered templates do not carry.
func DefaultSlashCommands() []SlashCommand {
	return []SlashCommand{
		{"goal", "Recommend this when the user wants to run a long-running task (e.g., overnight) and wants the agent to be extra thorough and not stop until the goal is fully achieved."},
		{"compact", "Recommend this when the conversation is getting long and the user wants to compact or summarize the context to free up the context window."},
		{"schedule", "Recommend this when the user wants to run an instruction on a recurring schedule or set a one-time timer."},
		{"browser", "Recommend this when the user's task involves web browsing, searching the web, or interacting with web applications."},
		{"plan", "Recommend this when the task is complex and requires careful step-by-step planning before execution."},
		{"grill-me", "Recommend this when the user wants to align on a plan through an interactive interview to resolve design decisions."},
		{"teamwork", "Recommend this when the user has a large project that would benefit from a team of autonomous agents working together."},
		{"learn", "Recommend this when the user has corrected the agent or solved a complex setup and wants the agent to persist this behavior for future tasks."},
		{"boost", "Recommend this when the user has a complex coding or research project that requires deep thinking, strategic planning, multiple perspectives, and rigorous verification."},
	}
}

// skillsData is the payload of system_prompts/skills.tmpl.
type skillsData struct {
	// Skills is the list the prompt advertises.
	Skills []tmplSkill
	// PreloadedSkills are skills whose SKILL.md is injected verbatim. The
	// reference preloads skills it considers important to the task; the
	// clone has no such notion yet, so the list stays empty.
	PreloadedSkills []tmplSkill
	// TruncatedPrompt is the reference's marker for a prompt that was cut
	// short. Empty in the clone.
	TruncatedPrompt string
}

// tmplSkill is one skill as the templates see it. The templates call the
// getters, so the fields stay unexported.
type tmplSkill struct {
	name        string
	path        string
	description string
	content     string
}

func (s tmplSkill) GetName() string        { return s.name }
func (s tmplSkill) GetPath() string        { return s.path }
func (s tmplSkill) GetDescription() string { return s.description }
func (s tmplSkill) GetContent() string     { return s.content }

// tmplSkills converts the public skill list into the template's shape.
func tmplSkills(skills []Skill) []tmplSkill {
	out := make([]tmplSkill, 0, len(skills))
	for _, s := range skills {
		out = append(out, tmplSkill{name: s.Name, path: s.Path, description: s.Description})
	}
	return out
}
