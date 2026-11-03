// Package permissionprompt implements a dedicated TUI interactive permission
// confirmation prompt component for tool approvals (e.g. ACP session/request_permission).
package permissionprompt

// Decision represents the user's decision on a permission request.
type Decision string

const (
	// DecisionNone represents an undecided/pending prompt.
	DecisionNone Decision = ""
	// DecisionApproveOnce grants permission for this invocation only ('y').
	DecisionApproveOnce Decision = "approve_once"
	// DecisionAlwaysAllow grants permission always for this tool in the session ('a').
	DecisionAlwaysAllow Decision = "always_allow"
	// DecisionDeny rejects the tool invocation ('n' or Esc).
	DecisionDeny Decision = "deny"
	// DecisionFeedback rejects the tool invocation with user feedback ('d').
	DecisionFeedback Decision = "feedback"
)

// Result holds the outcome of a permission prompt.
type Result struct {
	Decision Decision `json:"decision"`
	Feedback string   `json:"feedback,omitempty"`
	OptionID string   `json:"optionId,omitempty"`
}

// IsApproved reports whether the decision is an approval (once or always).
func (r Result) IsApproved() bool {
	return r.Decision == DecisionApproveOnce || r.Decision == DecisionAlwaysAllow
}

// IsDenied reports whether the decision is a denial (plain or with feedback).
func (r Result) IsDenied() bool {
	return r.Decision == DecisionDeny || r.Decision == DecisionFeedback
}

// Option represents the focused button in the confirmation prompt.
type Option int

const (
	// OptionApproveOnce represents the [y] Approve once button.
	OptionApproveOnce Option = iota
	// OptionAlwaysAllow represents the [a] Always allow button.
	OptionAlwaysAllow
	// OptionDeny represents the [n] Deny button.
	OptionDeny
	// OptionFeedback represents the [d] Feedback button.
	OptionFeedback
)

// PermissionOption describes a protocol-level option presented to the user.
type PermissionOption struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	Kind  string `json:"kind,omitempty"`
}

// Kind distinguishes standard tool permission prompts from file-edit/file-creation prompts.
type Kind string

const (
	// KindDefault represents standard tool approvals (run_command, mcp, etc.).
	KindDefault Kind = ""
	// KindFileEdit represents the "Accept this file edit?" confirmation prompt.
	KindFileEdit Kind = "file_edit"
	// KindFileCreate represents the "Allow creation of this file?" confirmation prompt.
	KindFileCreate Kind = "file_create"
)

// Request encapsulates the tool call details requiring user permission.
type Request struct {
	ID          string             `json:"id,omitempty"`
	Kind        Kind               `json:"kind,omitempty"`
	Tool        string             `json:"tool"`
	Command     string             `json:"command,omitempty"`
	Path        string             `json:"path,omitempty"`
	Diff        string             `json:"diff,omitempty"`
	OutsideWS   bool               `json:"outside_ws,omitempty"`
	Description string             `json:"description,omitempty"`
	Title       string             `json:"title,omitempty"`
	Args        map[string]any     `json:"args,omitempty"`
	RawInput    string             `json:"raw_input,omitempty"`
	Options     []PermissionOption `json:"options,omitempty"`
}

// CommandStr returns the shell command, looking first at Command then Args.
func (r Request) CommandStr() string {
	if r.Command != "" {
		return r.Command
	}
	if r.Args != nil {
		if cmd, ok := r.Args["CommandLine"].(string); ok && cmd != "" {
			return cmd
		}
		if cmd, ok := r.Args["command"].(string); ok && cmd != "" {
			return cmd
		}
		if cmd, ok := r.Args["cmd"].(string); ok && cmd != "" {
			return cmd
		}
	}
	return ""
}

// PathStr returns the target path, looking first at Path then Args.
func (r Request) PathStr() string {
	if r.Path != "" {
		return r.Path
	}
	if r.Args != nil {
		if p, ok := r.Args["Path"].(string); ok && p != "" {
			return p
		}
		if p, ok := r.Args["path"].(string); ok && p != "" {
			return p
		}
		if p, ok := r.Args["target"].(string); ok && p != "" {
			return p
		}
	}
	return ""
}

// TitleStr returns the prompt title, defaulting to "Permission Required".
func (r Request) TitleStr() string {
	if r.Title != "" {
		return r.Title
	}
	switch r.Kind {
	case KindFileEdit:
		return "Accept this file edit?"
	case KindFileCreate:
		return "Create file"
	default:
		return "Permission Required"
	}
}

// PromptQuestionStr returns the question displayed above the choices.
func (r Request) PromptQuestionStr() string {
	switch r.Kind {
	case KindFileEdit:
		return "Accept this file edit?"
	case KindFileCreate:
		return "Allow creation of this file?"
	default:
		return ""
	}
}

// MatchOptionID resolves the option ID corresponding to the given decision.
func (r Request) MatchOptionID(d Decision) string {
	if len(r.Options) == 0 {
		return string(d)
	}
	for _, opt := range r.Options {
		switch d {
		case DecisionApproveOnce:
			if opt.Kind == "allow_once" || opt.Kind == "approve_once" || opt.ID == "allow_once" || opt.ID == "approve_once" || opt.ID == "once" {
				return opt.ID
			}
		case DecisionAlwaysAllow:
			if opt.Kind == "allow_always" || opt.Kind == "always_allow" || opt.ID == "allow_always" || opt.ID == "always" {
				return opt.ID
			}
		case DecisionDeny:
			if opt.Kind == "reject" || opt.Kind == "deny" || opt.ID == "reject" || opt.ID == "deny" {
				return opt.ID
			}
		case DecisionFeedback:
			if opt.Kind == "reject_with_feedback" || opt.Kind == "feedback" || opt.ID == "feedback" {
				return opt.ID
			}
		}
	}
	return string(d)
}
