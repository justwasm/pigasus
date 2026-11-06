// Package hostactions defines and implements frontend-only actions (ScopeHost)
// that run directly in the TUI presentation layer without depending on a specific
// agentturn engine backend.
//
// Following the architecture tier rules, this package is located in Tier 5 (internal/ui).
package hostactions

import (
	"strings"

	"github.com/justwasm/pigasus/internal/core/slashparse"
)

// Seams abstracts the host operations needed by host actions.
type Seams struct {
	// AbortTurn cancels any ongoing turn execution.
	AbortTurn func()
	// ClearState resets the visual transcript and buffers.
	ClearState func()
	// Quit triggers terminal exit.
	Quit func()
	// OpenHelp opens the help overlay.
	OpenHelp func()
	// OpenPanel opens a modal panel by canonical command name.
	OpenPanel func(cmd string) bool
	// GetPlannerResponses retrieves the assistant response history.
	GetPlannerResponses func() []string
	// GetBtwCard retrieves the last BTW card content.
	GetBtwCard func() (string, string)
	// WriteClipboard writes content to the system clipboard.
	WriteClipboard func(text string) error
	// Respond echoes feedback or error rows to the user transcript.
	Respond func(cmd string, msg string, extra ...string)
	// ResetInputs clears composer and picker buffers.
	ResetInputs func()
}

// Action represents an executable host action.
type Action func(seams Seams, args string) bool

// Registry maps command names to host actions.
var Registry = map[string]Action{
	"clear":    HandleClear,
	"exit":     HandleExit,
	"quit":     HandleExit,
	"help":     HandleHelp,
	"copy":     HandleCopy,
	"feedback": HandleFeedback,
}

// HandleClear clears the visual transcript and state.
func HandleClear(seams Seams, _ string) bool {
	if seams.AbortTurn != nil {
		seams.AbortTurn()
	}
	if seams.ClearState != nil {
		seams.ClearState()
	}
	return true
}

// HandleExit terminates the interactive TUI session.
func HandleExit(seams Seams, _ string) bool {
	if seams.Quit != nil {
		seams.Quit()
	}
	return true
}

// HandleHelp displays the interactive help overlay.
func HandleHelp(seams Seams, _ string) bool {
	if seams.OpenHelp != nil {
		seams.OpenHelp()
	}
	if seams.ResetInputs != nil {
		seams.ResetInputs()
	}
	return true
}

// HandleFeedback notes that feedback was recorded.
func HandleFeedback(seams Seams, _ string) bool {
	if seams.Respond != nil {
		seams.Respond("/feedback", "Feedback recorded (openagy has no upload endpoint; saved locally only)")
	}
	return true
}

// HandleCopy copies the selected response to the clipboard.
func HandleCopy(seams Seams, args string) bool {
	trimmed := slashparse.Trim(args)
	if strings.EqualFold(trimmed, "btw") {
		if seams.GetBtwCard == nil {
			if seams.Respond != nil {
				seams.Respond("/copy btw", "No BTW card to copy.")
			}
			return true
		}
		payload, msg := seams.GetBtwCard()
		if payload != "" && seams.WriteClipboard != nil {
			if err := seams.WriteClipboard(payload); err != nil {
				if seams.Respond != nil {
					seams.Respond("/copy btw", "Failed to copy to clipboard: "+err.Error())
				}
				return true
			}
		}
		if seams.Respond != nil {
			seams.Respond("/copy btw", msg)
		}
		return true
	}

	var responses []string
	if seams.GetPlannerResponses != nil {
		responses = seams.GetPlannerResponses()
	}

	payload, msg := ResolveCopy(args, responses)
	if payload != "" && seams.WriteClipboard != nil {
		if err := seams.WriteClipboard(payload); err != nil {
			if seams.Respond != nil {
				seams.Respond("/copy", "Failed to copy to clipboard: "+err.Error())
			}
			return true
		}
	}
	if seams.Respond != nil {
		seams.Respond("/copy", msg)
	}
	return true
}
