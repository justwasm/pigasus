package tasks

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/justwasm/pigasus/internal/app/tools"
	"github.com/justwasm/pigasus/internal/core/backend"
)

const (
	// UpdateStepsToolName is the model-facing name of the live step checklist tool.
	UpdateStepsToolName = "update_steps"

	// MaxUpdateSteps is the maximum number of steps allowed in a single update_steps call.
	MaxUpdateSteps = 12

	// UpdateStepsDescription is the reference description for update_steps. Verified
	// byte-identical in the 1.5.16 and 1.5.17 binaries.
	UpdateStepsDescription = `Shows the user a live checklist of the steps you are carrying out.

When to use:
- Call it once you have committed to an ordered list of 3 to 12 distinct steps for the current request, whatever their source: an approved plan, a list the user gave you, a skill's procedure, or steps you worked out yourself.
- Skip it for requests that take 1-2 steps, pure questions, and open-ended exploration where you cannot name the steps yet (use update_focus for that). Never invent steps just to fill the list.

How to use:
- Always send the full list. Each call replaces the previous one, so to re-plan, send the new list. Keep steps in the order you do them: when you split or add a step, put it where it happens, not at the end.
- Mark the step you are working on "in_progress"; the rest are "pending", "done", "skipped" or "failed". When you work on several steps at once (for example, parallel subagents), mark each of them "in_progress".
- Call it in parallel with the first tool call of a step, so it adds no extra turn: once at the start (first step in_progress), then each time you move on (mark the finished step done and the next one in_progress in the same call).
- When the last step is finished, call it once more with every step done before writing your final reply.
- Each time you mark a step done, tell the user in one or two sentences what you did and what comes next, in your message alongside that call.

Titles:
- Short and imperative (2-6 words), in plain words the user would use, e.g. "Merge branch into main".
- No numbers, IDs or labels from your plan ("0.1", "Step 2:", "P0"), and no code identifiers unless the user named them.
- Aim for 3 to 8 steps; group small tasks into one step rather than listing each.
- Keep using update_focus for the finer-grained activity inside a step.`
)

var updateStepsWireParams = json.RawMessage(`{
	"type": "OBJECT",
	"properties": {
		"steps": {
			"type": "ARRAY",
			"description": "The full ordered list of steps (at most 12). Replaces any previous list.",
			"maxItems": 12,
			"items": {
				"type": "OBJECT",
				"properties": {
					"title": {
						"type": "STRING",
						"description": "Short imperative title of the step (2-6 words), e.g. 'Merge branch into main'."
					},
					"status": {
						"type": "STRING",
						"description": "Current status of the step.",
						"enum": ["pending", "in_progress", "done", "skipped", "failed"]
					}
				},
				"required": ["title", "status"]
			}
		}
	},
	"required": ["steps"]
}`)

var updateStepsSchema = json.RawMessage(`{
	"type": "object",
	"properties": {
		"steps": {
			"type": "array",
			"description": "The full ordered list of steps (at most 12). Replaces any previous list.",
			"maxItems": 12,
			"items": {
				"type": "object",
				"properties": {
					"title": {
						"type": "string",
						"description": "Short imperative title of the step (2-6 words), e.g. 'Merge branch into main'."
					},
					"status": {
						"type": "string",
						"description": "Current status of the step.",
						"enum": ["pending", "in_progress", "done", "skipped", "failed"]
					}
				},
				"required": ["title", "status"]
			}
		}
	},
	"required": ["steps"]
}`)

// StepStatus constants for update_steps items.
const (
	StepStatusPending    = "pending"
	StepStatusInProgress = "in_progress"
	StepStatusDone       = "done"
	StepStatusSkipped    = "skipped"
	StepStatusFailed     = "failed"
)

// StepItem represents one step in an update_steps checklist.
type StepItem struct {
	Title  string `json:"title"`
	Status string `json:"status"`
}

// UpdateStepsDecl returns the backend.ToolDecl advertised to models for update_steps.
func UpdateStepsDecl() backend.ToolDecl {
	return backend.ToolDecl{
		Name:        UpdateStepsToolName,
		Description: UpdateStepsDescription,
		Parameters:  updateStepsWireParams,
	}
}

// UpdateSteps implements the update_steps tool.
type UpdateSteps struct{}

func (u *UpdateSteps) Name() string            { return UpdateStepsToolName }
func (u *UpdateSteps) Description() string     { return UpdateStepsDescription }
func (u *UpdateSteps) Schema() json.RawMessage { return updateStepsSchema }

func (u *UpdateSteps) Run(_ context.Context, args json.RawMessage) (any, error) {
	var p struct {
		Steps []StepItem `json:"steps"`
	}
	if err := tools.ArgsOf(args, &p); err != nil {
		return nil, err
	}
	if err := ValidateUpdateSteps(p.Steps); err != nil {
		return nil, err
	}
	return FormatUpdateStepsMarkdown(p.Steps), nil
}

// ValidateUpdateSteps validates the step list against the reference rules
// (unchanged between 1.5.16 and 1.5.17).
func ValidateUpdateSteps(steps []StepItem) error {
	if len(steps) > MaxUpdateSteps {
		return fmt.Errorf("too many steps (%d); at most %d allowed", len(steps), MaxUpdateSteps)
	}
	for i, s := range steps {
		if strings.TrimSpace(s.Title) == "" {
			return fmt.Errorf("step %d has an empty title", i+1)
		}
		switch s.Status {
		case StepStatusPending, StepStatusInProgress, StepStatusDone, StepStatusSkipped, StepStatusFailed:
		default:
			return fmt.Errorf("step %d has invalid status %q; use pending, in_progress, done, skipped or failed", i+1, s.Status)
		}
	}
	return nil
}

// FormatUpdateStepsMarkdown formats steps as a markdown checklist matching the
// reference (unchanged between 1.5.16 and 1.5.17).
func FormatUpdateStepsMarkdown(steps []StepItem) string {
	if len(steps) == 0 {
		return ""
	}
	var b strings.Builder
	for i, s := range steps {
		if i > 0 {
			b.WriteByte('\n')
		}
		marker := " "
		switch s.Status {
		case StepStatusInProgress:
			marker = ">"
		case StepStatusDone:
			marker = "x"
		case StepStatusSkipped:
			marker = "-"
		case StepStatusFailed:
			marker = "!"
		}
		b.WriteString("- [")
		b.WriteString(marker)
		b.WriteString("] ")
		b.WriteString(strings.TrimSpace(s.Title))
	}
	return b.String()
}

var checklistLineRe = regexp.MustCompile(`^\s*[-*]\s+\[([ >xX\-!])\]\s+(.+?)\s*$`)

// ParseChecklistMarkdown parses a markdown checklist produced by update_steps.
func ParseChecklistMarkdown(md string) []StepItem {
	if strings.TrimSpace(md) == "" {
		return nil
	}
	var out []StepItem
	for _, line := range strings.Split(md, "\n") {
		m := checklistLineRe.FindStringSubmatch(line)
		if len(m) != 3 {
			continue
		}
		status := StepStatusPending
		switch m[1] {
		case ">":
			status = StepStatusInProgress
		case "x", "X":
			status = StepStatusDone
		case "-":
			status = StepStatusSkipped
		case "!":
			status = StepStatusFailed
		}
		out = append(out, StepItem{
			Title:  strings.TrimSpace(m[2]),
			Status: status,
		})
	}
	return out
}
