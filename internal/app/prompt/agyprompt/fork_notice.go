package agyprompt

import (
	"bytes"
	"fmt"
	"text/template"
)

// ForkNoticeData holds fields required to render the <fork_notice> block.
type ForkNoticeData struct {
	SourceCascadeID  string
	OldArtifactDir   string
	NewArtifactDir   string
	ArtifactNote     string
	Notes            []string
	DroppedSubagents bool
	CanceledTasks    bool
}

// ForkNoticeTemplate is the prompt template used when a conversation is branched/forked.
const ForkNoticeTemplate = `<fork_notice>
This conversation was branched from a previous conversation (ID: {{.SourceCascadeID}}).

Your active artifact directory has changed:
- Old artifact directory (referenced in past steps): {{.OldArtifactDir}}
- New artifact directory (use for all new work): {{.NewArtifactDir}}

{{.ArtifactNote}}
{{range .Notes}}
{{.}}
{{end}}{{if .DroppedSubagents}}
The subagents invoked in the history above are NOT yours: they belong to the source conversation and were not branched with it. They are not listed among your subagents, and you cannot message, inspect or kill them. Invoke your own if you need them.
{{- /* The end and the if that follows it stay on one line: a newline between them is literal text and would render as a second blank line between these notes. */}}
{{end}}{{if .CanceledTasks}}
The background tasks in the history above are NOT yours: they belong to the source conversation and were not branched with it. They are marked cancelled here and will send you no output. Start your own if you need them.
{{end}}
IMPORTANT: For all future file edits, artifact creation, and run_command invocations, you MUST use the new artifact directory. Do not reference the old one.
</fork_notice>`

// RenderForkNotice parses and executes ForkNoticeTemplate with the provided data.
func RenderForkNotice(data ForkNoticeData) (string, error) {
	tmpl, err := template.New("fork_notice").Parse(ForkNoticeTemplate)
	if err != nil {
		return "", fmt.Errorf("parse fork notice template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", fmt.Errorf("execute fork notice template: %w", err)
	}
	return buf.String(), nil
}
