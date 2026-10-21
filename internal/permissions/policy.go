// Package permissions implements the recovered agy tool-permission model:
// the `--mode` values, the five-way tool-call verdict, and the headless
// (print-mode) confirmation policy with its verbatim user-facing strings.
//
// Provenance (agy 1.3.6, SHA-256
// 827ba22b4aa33557614a57aac2cc13f386e17db6b802febd3932226906ac05ab):
//
//   - The mode set comes from the flag's own format string,
//     `Set the agent execution mode for this session (%s)` and
//     `warning: unrecognized --mode value %q (valid: %s)`, both substituted
//     with "accept-edits, auto, plan".
//   - The verdict enum comes from a JSON-schema tag on the policy call:
//     jsonschema:"required,enum=allow,enum=deny,enum=ask,enum=force_ask,
//     enum=deny_unless_prior_grant" with the description "Decision for the
//     tool call: 'allow' to proceed, 'deny' to block, 'ask' to request user
//     confirmation."
//   - The headless strings (notice, denial text, mode banner) are verbatim
//     string-table hits.
//
// Deliberate divergence, chosen by the operator: this clone defaults to
// Allow when nothing else decides. The reference asks the user at that point,
// and in headless mode turns the ask into a soft-deny. Setting
// OPENAGY_PERMISSION_POLICY=strict restores the reference behaviour, which is
// what the soft-deny tests exercise.
package permissions

import (
	"strings"
)

// Modes accepted by the global --mode flag. Recovered in reference order.
const (
	ModeAcceptEdits = "accept-edits"
	ModeAuto        = "auto"
	ModePlan        = "plan"
)

// Modes is the accepted --mode set in the order the reference prints it.
var Modes = []string{ModeAcceptEdits, ModeAuto, ModePlan}

// ModeList renders Modes the way the reference substitutes it into both the
// flag usage text and the unrecognized-value warning.
func ModeList() string { return strings.Join(Modes, ", ") }

// ValidMode reports whether s is an accepted --mode value. The empty string
// (flag unset) is handled by the caller and is not "valid" here.
func ValidMode(s string) bool {
	for _, m := range Modes {
		if m == s {
			return true
		}
	}
	return false
}

// Verdict is the decision for one tool call, mirroring the recovered enum.
type Verdict int

const (
	// Allow lets the call proceed.
	Allow Verdict = iota
	// Deny blocks the call outright.
	Deny
	// Ask requests user confirmation.
	Ask
	// ForceAsk always prompts, ignoring cached permissions.
	ForceAsk
	// DenyUnlessPriorGrant blocks unless an earlier grant covers the call.
	DenyUnlessPriorGrant
)

// String returns the wire name, matching the recovered enum labels.
func (v Verdict) String() string {
	switch v {
	case Allow:
		return "allow"
	case Deny:
		return "deny"
	case Ask:
		return "ask"
	case ForceAsk:
		return "force_ask"
	case DenyUnlessPriorGrant:
		return "deny_unless_prior_grant"
	}
	return "unknown"
}

// Class categorises a tool call for the mode auto-approval rules. The mode
// banners name only two classes — "file edits" (accept-edits) and
// "edits & safe commands" (auto) — plus the always-prompt admin escalation
// case, so the classification below is the finest split the evidence supports.
type Class int

const (
	// ClassRead inspects state without changing it.
	ClassRead Class = iota
	// ClassEdit creates or modifies files.
	ClassEdit
	// ClassCommand runs a shell command.
	ClassCommand
	// ClassOther is anything not covered above (still subject to rules).
	ClassOther
)

// String returns the class name used in the mode banners.
func (c Class) String() string {
	switch c {
	case ClassRead:
		return "read"
	case ClassEdit:
		return "file edit"
	case ClassCommand:
		return "command"
	}
	return "other"
}

// editTools are the advertised tools that write files. Grade 【推测】: the
// cortex tools package is not in the decompiled set, so this is derived from
// each tool's name and documented behaviour (write_to_file, the replace/sed
// family, and the two notebook mutators). It matches the reference's
// "file edits auto-approved" wording, which is the only classification the
// recovered strings name.
var editTools = map[string]bool{
	"write_to_file":              true,
	"replace_file_content":       true,
	"multi_replace_file_content": true,
	"sed_file":                   true,
	"notebook_edit":              true,
	"notebook_execution":         true,
	// Cycle 293: the 1.4.4 wire names. A reference-shaped edit must be
	// auto-approved by the same modes that approve write_to_file, or
	// accept-edits mode silently denies every real file edit.
	"write_file": true,
	"edit_file":  true,
}

// commandTools are the advertised tools that execute a command.
var commandTools = map[string]bool{
	"run_command":        true,
	"command_status":     true,
	"send_command_input": true,
}

// Classify maps an advertised tool name to its class.
func Classify(tool string) Class {
	switch {
	case editTools[tool]:
		return ClassEdit
	case commandTools[tool]:
		return ClassCommand
	case tool == "view_file", tool == "list_dir", tool == "grep_search",
		tool == "find_by_name", tool == "codebase_search",
		tool == "view_content_chunk", tool == "list_resources",
		tool == "read_resource", tool == "list_permissions",
		// Cycle 293: read_file is the 1.4.4 wire name for the same read.
		tool == "read_file":
		return ClassRead
	}
	return ClassOther
}

// Rule is one entry of settings.json permissions.allow or permissions.deny.
// Pattern is matched as a case-sensitive prefix of the call's target (the
// reference describes the rules as literal absolute paths or command
// prefixes, e.g. "Matches the file or everything under the directory").
type Rule struct {
	Tool    string
	Pattern string
}

// Request is a tool call under evaluation.
type Request struct {
	Tool   string
	Target string
}

func (r Rule) matches(req Request) bool {
	if r.Tool != "" && r.Tool != req.Tool {
		return false
	}
	if r.Pattern == "" {
		return true
	}
	return strings.HasPrefix(req.Target, r.Pattern)
}

// Policy is the local permission engine.
type Policy struct {
	// Mode is the global --mode value ("" is the default mode).
	Mode string
	// SkipPermissions is --dangerously-skip-permissions.
	SkipPermissions bool
	// Strict restores the reference default: an otherwise-undecided call
	// becomes Ask (which headless mode turns into a soft-deny) instead of
	// Allow. Set from OPENAGY_PERMISSION_POLICY=strict.
	Strict bool
	// Allow and Deny are the settings.json rule lists.
	Allow []Rule
	// Deny precedes Allow, as in the reference (a deny rule wins).
	Deny []Rule
}

// StrictFromEnv reports whether OPENAGY_PERMISSION_POLICY requests the
// reference default (strict) rather than the clone's allow-by-default.
func StrictFromEnv(v string) bool {
	return strings.EqualFold(strings.TrimSpace(v), "strict")
}

// Decide returns the verdict for one tool call. Order, matching the reference:
// skip-permissions wins outright; an explicit deny rule blocks; an explicit
// allow rule permits; then the mode's auto-approval rules; then the default.
func (p Policy) Decide(req Request) Verdict {
	if p.SkipPermissions {
		return Allow
	}
	for _, r := range p.Deny {
		if r.matches(req) {
			return Deny
		}
	}
	for _, r := range p.Allow {
		if r.matches(req) {
			return Allow
		}
	}
	cls := Classify(req.Tool)
	switch p.Mode {
	case ModeAcceptEdits:
		if cls == ClassEdit {
			return Allow
		}
	case ModeAuto:
		if cls == ClassEdit || cls == ClassCommand {
			return Allow
		}
	case ModePlan:
		if cls == ClassRead {
			return Allow
		}
	}
	if p.Strict {
		return Ask
	}
	return Allow
}

// HeadlessNotice is the reference's end-of-run notice about tool calls it
// auto-denied because headless mode cannot prompt for them. Recovered
// verbatim; `n` is the count of denied calls.
func HeadlessNotice(n int) string {
	return "the " + itoa(n) + " tool(s) required approval that headless mode cannot prompt for, " +
		"so they were auto-denied. Settings allow-rules do not apply; re-run with " +
		"--dangerously-skip-permissions to auto-approve all tools."
}

// HeadlessDenialNotice is the single-call variant of the notice, used when a
// settings suggestion is available. Recovered verbatim as
//
//	%s required the %s %s that headless mode cannot prompt for, so %s
//	auto-denied. Add an allow-rule under permissions.allow in settings.json (e.g. %s)
func HeadlessDenialNotice(what, action, target, subject, example string) string {
	return what + " required the " + action + " " + target +
		" that headless mode cannot prompt for, so " + subject +
		" auto-denied. Add an allow-rule under permissions.allow in settings.json (e.g. " +
		example + ")"
}

// DeniedToolResult is the text the reference feeds back to the model after a
// soft-denied call. Recovered verbatim.
func DeniedToolResult(action, target string) string {
	return "Permission prompt for action '" + action + "' on target '" + target + "' was denied. " +
		"Do not attempt to circumvent this denial by rephrasing the command, using alternative " +
		"tools/scripts (e.g. python, sh, curl), or accessing the same target resource. " +
		"Proceed without performing this action."
}

// SkipPermissionsNotice is the reference's log line when
// --dangerously-skip-permissions is set in print mode. Recovered verbatim.
const SkipPermissionsNotice = "Print mode: --dangerously-skip-permissions set, auto-approving all tool permissions"

// Mode banners. Recovered verbatim; the reference shows one of these as the
// current-mode hint in the status line.
const (
	AcceptEditsBanner = "Accept-edits mode: file edits auto-approved (shift+tab to cycle)"
	AutoBanner        = "Auto mode: edits & safe commands auto-approved (shift+tab to cycle)"
	PlanBanner        = "Plan mode: research & plan only (shift+tab to cycle)"
)

// CycleHint is the recovered hint for the *default* mode. It is the only
// shift+tab hint in the 1.3.6 string table, and it names three modes:
//
//	Press shift+tab to cycle modes (default, accept-edits, plan).
//
// Auto mode was added in 1.3.5 and this text appears not to have been updated
// to include it. Cycle 104 cycles through Auto as well, because the 1.3.5
// changelog states it explicitly ("Choose it with `--mode auto`, from
// `Agent Mode` in `/config`, or by pressing `Shift+Tab` to cycle modes") and
// the changelog is the authority for what the shipped build does. See the
// cycle 104 deliverable for the full reasoning.
const CycleHint = "Press shift+tab to cycle modes (default, accept-edits, plan)."

// CycleModes is the Shift+Tab order. Cycle 254 reordered the cycle to
// start from accept-edits (Build) — Build → Default → Auto → Plan →
// Build. The previous cycle (104) had default first, which made a
// fresh session's first Shift+Tab jump straight to accept-edits. The
// 254 reorder keeps accept-edits reachable in two presses from
// default (default → auto → plan → accept-edits wraps) but is the
// choice documented in cmd/wagyu/shift_tab_test.go and pinned by
// TestShiftTabCyclesBuildToDefault et al. — see 硬约束 in
// docs/cycles/cycle-254-shift-tab/deliverable.md.
var CycleModes = []string{ModeAcceptEdits, "", ModeAuto, ModePlan}

// NextMode returns the mode that follows cur in the Shift+Tab cycle.
// An unknown value (including the empty default) advances to
// accept-edits (the first element of CycleModes — Build).
func NextMode(cur string) string {
	for i, m := range CycleModes {
		if m == cur {
			return CycleModes[(i+1)%len(CycleModes)]
		}
	}
	return CycleModes[0]
}

// ModeBanner returns the status-line banner for a mode, or "" for the default
// mode (the reference names no banner for it in the recovered string table).
func ModeBanner(mode string) string {
	switch mode {
	case ModeAcceptEdits:
		return AcceptEditsBanner
	case ModeAuto:
		return AutoBanner
	case ModePlan:
		return PlanBanner
	}
	return ""
}

// NoToolCallNudge is the reference's reminder when a turn produced no tool
// call. The headless variant appends the second sentence. Both recovered
// verbatim.
const (
	NoToolCallNudge         = "You did not call any tools. You should continue calling tools until you've completed your task. If you have completed your task call the finish tool."
	NoToolCallNudgeHeadless = "You did not call any tools. You should continue calling tools until you've completed your task. If you have completed your task call the finish tool. Remember that the user cannot answer any questions."
)

// itoa avoids importing strconv for one call.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var b [20]byte
	i := len(b)
	for n > 0 {
		i--
		b[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		b[i] = '-'
	}
	return string(b[i:])
}
