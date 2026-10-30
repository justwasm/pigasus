// render.go — the reference's step strings, rendered from its own templates.
//
// The reference ships two step-string templates:
//
//	step_strings/run_command.tmpl          (RunCommandStringConverter)
//	step_strings/send_command_input.tmpl   (SendCommandInputStringConverter)
//
// Both take a StepStringData with a Step field and print the step as text.
// This file renders them; step.go supplies the data model they read.
//
// What the reference does with the rendered string depends on which path
// produced the step:
//
//   - The plain run_command result that the CLI sends to the model is NOT
//     this template's output. The CLI carries its own format strings
//     ("Command exited with code %d.", "No output from the command.") and
//     the 1.4.7 conversation store holds that shape (一手 — see
//     docs/cycles/cycle-298-step-strings/deliverable.md).
//   - Every other command-step wording in the binary — "Background command
//     ID:", "Output snapshot:", "Input sent successfully.", "The command is
//     still running.", "Terminal ID:" and the sandbox guidance — exists in
//     the binary ONLY inside these templates (一手: each string appears
//     exactly once in `strings -a ~/.local/bin/agy`, inside the template
//     file). So for those paths the template is the reference's wording.
//
// The clone therefore renders the background and send_command_input results
// through these templates, and keeps its plain run_command result on the
// CLI's own formatter, which is what the reference sends today.
package step

import (
	"bytes"
	"fmt"
	"text/template"

	"github.com/justwasm/pigasus/internal/agytemplates"
)

// stepStringData is the payload the reference's step strings take: a struct
// with a Step field.
type stepStringData struct {
	Step *Step
}

// newSet builds the shared template set. The step strings read no bare
// identifier, so they need no engine functions.
func newSet() *template.Template {
	return agytemplates.NewSet(nil)
}

// RunCommandString renders step_strings/run_command.tmpl.
//
// The reference's converter is named in the template's own header
// ("Template for RunCommandStringConverter.ToOutputString").
func RunCommandString(step *Step) string {
	return render("step_strings/run_command.tmpl", stepStringData{Step: step})
}

// SendCommandInputString renders step_strings/send_command_input.tmpl
// ("Template for SendCommandInputStringConverter.ToOutputString").
func SendCommandInputString(step *Step) string {
	return render("step_strings/send_command_input.tmpl", stepStringData{Step: step})
}

// render executes one recovered template.
//
// A failure means the embedded bundle and the data model disagree, which is
// a programming error and not a runtime condition — the same treatment
// agyprompt gives a missing section file.
func render(name string, data stepStringData) string {
	var buf bytes.Buffer
	if err := newSet().ExecuteTemplate(&buf, name, data); err != nil {
		panic(fmt.Sprintf("agystep: render %s: %v", name, err))
	}
	return buf.String()
}
