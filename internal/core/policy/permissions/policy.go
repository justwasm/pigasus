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
//
// The same choice governs the `toolPermission` setting: the setting is
// honoured when it is *present* (its four documented values steer the
// otherwise-undecided calls), and the *absent* case keeps the operator's
// allow-by-default. The reference's own default is "request-review", which
// asks; the clone's default is the one above, and OPENAGY_PERMISSION_POLICY=strict
// is the switch that restores the reference's ask.
package permissions

import (
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	corehooks "github.com/justwasm/pigasus/internal/core/policy/hooks"
)

// Modes accepted by the global --mode flag. Recovered in reference order.
const (
	ModeAcceptEdits = "accept-edits"
	ModeAuto        = "auto"
	ModePlan        = "plan"
)

// AutoModeReviewerModel is the lightweight model with lower thinking budget used
// for safety reviews in Auto mode when commands attempt to leave the sandbox (AGY 1.5.4).
const AutoModeReviewerModel = "gemini-3.8-flash-low"

// Modes is the accepted --mode set in the order the reference prints it.
var Modes = []string{ModeAcceptEdits, ModeAuto, ModePlan}

// ModeList renders Modes the way the reference substitutes it into both the
// flag usage text and the unrecognized-value warning.
func ModeList() string { return strings.Join(Modes, ", ") }

// The grant actions — the reference's permission vocabulary.
//
// 【一手】 agy 1.4.11's own string table carries the grant format regex
//
//	^(command|read_file|write_file|read_url|mcp|execute_url|unsandboxed)\s*\(.*\)$
//
// and the panels' format hint "Format: action(target)". The official
// documentation names the same six live actions, plus `unsandboxed` as the
// retired one.
const (
	// ActionCommand runs a shell command.
	ActionCommand = "command"
	// ActionReadFile reads a file.
	ActionReadFile = "read_file"
	// ActionWriteFile writes a file.
	ActionWriteFile = "write_file"
	// ActionReadURL fetches a URL.
	ActionReadURL = "read_url"
	// ActionExecuteURL actuates on a web page.
	ActionExecuteURL = "execute_url"
	// ActionMCP calls an MCP tool.
	ActionMCP = "mcp"
)

// ActionUnsandboxed is the retired action. The reference still names it in
// the format regex but rejects every rule that uses it.
//
// 【一手】 agy 1.4.11: "unsandboxed is no longer a valid rule, use
// command() instead".
const ActionUnsandboxed = "unsandboxed"

// UnsandboxedRejectedMessage is the reference's own rejection message for an
// `unsandboxed` rule. Recovered verbatim.
const UnsandboxedRejectedMessage = "unsandboxed is no longer a valid rule, use command() instead"

// The four values the `toolPermission` setting takes.
//
// 【一手】 the setting's own vocabulary: the official documentation names
// request-review (the default), proceed-in-sandbox, strict and
// always-proceed, and the binary logs "CLI settings initialized:
// permissions=%+v, toolPermission=%s".
const (
	// ToolPermissionRequestReview asks before a write, a command, a web
	// request or an MCP call.
	ToolPermissionRequestReview = "request-review"
	// ToolPermissionProceedInSandbox runs a command without asking while
	// the terminal sandbox is on.
	ToolPermissionProceedInSandbox = "proceed-in-sandbox"
	// ToolPermissionStrict asks before every tool call.
	ToolPermissionStrict = "strict"
	// ToolPermissionAlwaysProceed never asks.
	ToolPermissionAlwaysProceed = "always-proceed"
)

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

// readTools are the advertised tools that read files without changing them.
//
// The set is the read half of the grant-action mapping below: the
// reference's `read_file` action covers exactly these names.
// `list_permissions` reads no file — it introspects the grants themselves —
// so it is a read for the mode rules but not a file read for the grammar.
var readTools = map[string]bool{
	"read_file":          true,
	"view_file":          true,
	"list_dir":           true,
	"grep_search":        true,
	"find_by_name":       true,
	"codebase_search":    true,
	"view_content_chunk": true,
	"list_resources":     true,
	"read_resource":      true,
}

// grantActionTools maps every live grant action onto the clone's own tool
// names — the one table that answers "which tools does this action cover?".
//
// The reference's actions are the *permission* vocabulary and the clone's
// tool names are the *execution* vocabulary, and the two do not line up: the
// reference's `command` is the clone's `run_command`, its `read_file` is
// the clone's `view_file` and `read_file`, and so on. Three of the six
// actions are the clone's own sets already — the command tools, the
// file-writing tools and the file-reading tools — so those are taken from
// there instead of being written a second time.
//
// 【推测】 the pairing itself: the reference's binary publishes no table
// (its `shared.ParseGrantString` is not recovered), so each action is
// matched to the tools that do the work the action names.
var grantActionTools = map[string][]string{
	ActionCommand:   toolNames(commandTools),
	ActionReadFile:  toolNames(readTools),
	ActionWriteFile: toolNames(editTools),
	ActionReadURL:   {"read_url_content", "search_web"},
	ActionExecuteURL: {
		"open_browser_url",
		"browser_click_element",
		"browser_input",
		"browser_press_key",
		"browser_select_option",
		"click_browser_pixel",
		"execute_browser_javascript",
	},
	ActionMCP: {"call_mcp_tool"},
}

// toolNames returns the names of a tool set in a stable order, so the
// mapping above does not depend on the map's iteration order.
func toolNames(set map[string]bool) []string {
	out := make([]string, 0, len(set))
	for name := range set {
		out = append(out, name)
	}
	sort.Strings(out)
	return out
}

// actionCoversTool reports whether a grant action covers a tool name.
//
// It is the one reader of the mapping above, so the rule matcher and the
// permissions panel cannot drift apart: both go through it.
func actionCoversTool(action, tool string) bool {
	for _, name := range grantActionTools[action] {
		if name == tool {
			return true
		}
	}
	return false
}

// ToolAction maps an execution tool name or alias to its grant action verb.
func ToolAction(tool string) string {
	if ValidGrantAction(tool) {
		return tool
	}
	for action, tools := range grantActionTools {
		for _, name := range tools {
			if name == tool {
				return action
			}
		}
	}
	return ""
}

// reviewActions are the actions whose calls the reference's
// request-review policy asks about: "Prompts for your approval before
// running write, bash, web, or MCP tools" (the official documentation).
var reviewActions = []string{
	ActionWriteFile,
	ActionCommand,
	ActionReadURL,
	ActionExecuteURL,
	ActionMCP,
}

// requiresReview reports whether a call is one the request-review policy
// asks about.
func requiresReview(names []string) bool {
	for _, n := range names {
		for _, action := range reviewActions {
			if actionCoversTool(action, n) {
				return true
			}
		}
	}
	return false
}

// isCommandCall reports whether a call runs a shell command.
func isCommandCall(names []string) bool {
	for _, n := range names {
		if actionCoversTool(ActionCommand, n) {
			return true
		}
	}
	return false
}

// Classify maps an advertised tool name to its class.
func Classify(tool string) Class {
	switch {
	case editTools[tool]:
		return ClassEdit
	case commandTools[tool]:
		return ClassCommand
	case readTools[tool], tool == "list_permissions":
		return ClassRead
	}
	return ClassOther
}

// Rule is one entry of settings.json permissions.allow, permissions.ask or
// permissions.deny.
//
// A rule is one of two shapes:
//
//   - a *grant* — the reference's own `action(target)` form. Action names
//     one of the six live grant actions and Pattern is the target the
//     action covers.
//
//   - a *legacy* rule — the `tool:pattern` form (and the
//     `{"tool": …, "pattern": …}` object form) this clone shipped before
//     the documentation was read. Tool names one of the clone's own tools
//     and is mapped to the corresponding grant action via ToolAction.
//
// Pattern is the rule's target. A *grant*'s pattern is matched by the
// action's own rule (see matching.go).
type Rule struct {
	// Tool is the clone's tool name — the legacy "tool:pattern" form.
	Tool string
	// Action is the reference's grant action — the "action(target)" form.
	Action string
	// Pattern is the rule's target.
	Pattern string
}

// Request is a tool call under evaluation.
type Request struct {
	Tool   string
	Target string
}

// matches evaluates the rule against a call.
//
// A *grant* — the reference's own `action(target)` form — is matched by
// the action's own rule (see matching.go): the action names the tools it
// covers, and the target is matched under the action's rule — a token-wise
// match for a command, a path prefix for a file, a hostname for a URL,
// a server and a tool for MCP.
//
// A legacy rule naming a tool is mapped to the corresponding grant action.
//
// The caller's own decision path is Policy.covers, which also knows the
// workspace roots and the two implicit rules.
func (r Rule) matches(req Request) bool {
	if r.Action != "" {
		// A grant covers every tool the action names — "command(git)"
		// reaches run_command, command_status and send_command_input.
		return grantCovers(r.Action, r.Pattern, req.Tool, req.Target, nil)
	}
	if r.Tool != "" && r.Tool != req.Tool {
		return false
	}
	action := ToolAction(r.Tool)
	if action != "" {
		return grantCovers(action, r.Pattern, req.Tool, req.Target, nil)
	}
	return false
}

// SuggestAllowAlwaysPattern derives an "Always allow" permission pattern from a command string.
//   - Normalizes line endings (\r\n -> \n) and trims surrounding whitespace.
//   - For multi-line commands and inline script runners (python -c, node -e, ruby -e, bash -c, sh -c, perl -e),
//     preserves the exact script or command so approving one script does not grant arbitrary execution.
//   - For compound commands (e.g. chained with && or ;), preserves the exact compound chain.
//   - For script runners (npm run, yarn, pnpm, cargo run), pins the specific script/target name.
//   - For standard CLI commands, suggests the command or subcommand prefix.
func SuggestAllowAlwaysPattern(cmd string) string {
	cmd = strings.ReplaceAll(cmd, "\r\n", "\n")
	cmd = strings.TrimSpace(cmd)
	if cmd == "" {
		return ""
	}

	// If the command is multi-line, preserve the entire multi-line command
	if strings.Contains(cmd, "\n") {
		return cmd
	}

	// Check for script runners with inline code: python -c, node -e, bash -c, sh -c, etc.
	fields := strings.Fields(cmd)
	if len(fields) >= 2 {
		base := filepath.Base(fields[0])
		switch base {
		case "python", "python3", "node", "ruby", "perl", "bash", "sh", "zsh":
			for _, arg := range fields[1:] {
				if arg == "-c" || arg == "-e" {
					// Inline script execution: pin exact command
					return cmd
				}
			}
		case "npm", "yarn", "pnpm":
			// Pin the specific script name (e.g. npm run dev, yarn build)
			if len(fields) >= 3 && fields[1] == "run" {
				return fields[0] + " run " + fields[2]
			}
			if len(fields) >= 2 && base != "npm" { // yarn dev, pnpm build
				return fields[0] + " " + fields[1]
			}
		case "cargo":
			if len(fields) >= 3 && fields[1] == "run" {
				return strings.Join(fields[:3], " ")
			}
		}
	}

	// If it contains compound operators (&&, ;, ||, |)
	if strings.Contains(cmd, "&&") || strings.Contains(cmd, ";") || strings.Contains(cmd, "||") || strings.Contains(cmd, "|") {
		return cmd
	}

	return cmd
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
	// Allow, Ask and Deny are the settings.json rule lists.
	//
	// The precedence is the reference's: Deny > Ask > Allow
	// ("Conflicting rules are strictly evaluated in priority order:
	// Deny > Ask > Allow").
	Allow []Rule
	// Ask sits between Deny and Allow: a matching ask rule prompts even
	// when an allow rule matches too.
	Ask []Rule
	// Deny precedes Ask and Allow — a deny rule wins.
	Deny []Rule
	// ToolPermission is the settings.json "toolPermission" value — one of
	// the four documented values, or "" when the key is absent.
	//
	// The *absent* case is the clone's own allow-by-default divergence and
	// is not this setting's business: the setting steers the calls no rule
	// and no mode decided, and an absent key steers nothing.
	ToolPermission string
	// SandboxEnabled reports whether the terminal sandbox is on. The
	// proceed-in-sandbox policy needs it: a command is auto-approved only
	// while the sandbox runs it.
	SandboxEnabled bool
	// WorkspaceRoots are the absolute paths of the project workspace
	// roots. A *relative* path grant — "write_file(src/)" — is resolved
	// against them, so the grant covers the directory inside the
	// workspace and everything under it.
	//
	// The field is optional: a host that does not know the workspace
	// leaves it empty, and a relative grant is then matched against the
	// components of the concrete path (see matchPath).
	WorkspaceRoots []string
}

// ruleList names the three rule lists, so that the implicit rules are
// applied to the list they belong to.
type ruleList int

const (
	// listAllow is the allow list.
	listAllow ruleList = iota
	// listAsk is the ask list.
	listAsk
	// listDeny is the deny list.
	listDeny
)

// implicitActions returns the actions a rule covers in addition to its
// own, per the reference's two implicit rules.
//
//	"Write implies read": allowing write_file on a path grants
//	read_file on that path.
//
//	"Deny read implies deny write": denying read_file on a path blocks
//	write_file on that path.
//
// The rules are one-directional, because the documentation states them in
// that direction only: an allow on read_file does not allow a write, and
// a deny on write_file does not deny a read.
func implicitActions(list ruleList, action string) []string {
	switch {
	case list == listAllow && action == ActionWriteFile:
		return []string{ActionReadFile}
	case list == listDeny && action == ActionReadFile:
		return []string{ActionWriteFile}
	}
	return nil
}

// covers reports whether a rule of the given list covers the call.
//
// A grant covers the call under its own action, and — where the list's
// implicit rule says so — under the action the implicit rule gives it.
// A legacy rule naming a tool is mapped to the corresponding grant action.
func (p Policy) covers(r Rule, list ruleList, req Request) bool {
	if r.Action != "" {
		if grantCovers(r.Action, r.Pattern, req.Tool, req.Target, p.WorkspaceRoots) {
			return true
		}
		for _, a := range implicitActions(list, r.Action) {
			if grantCovers(a, r.Pattern, req.Tool, req.Target, p.WorkspaceRoots) {
				return true
			}
		}
		return false
	}
	if r.Tool != "" && r.Tool != req.Tool {
		return false
	}
	action := ToolAction(r.Tool)
	if action == "" {
		return false
	}
	if grantCovers(action, r.Pattern, req.Tool, req.Target, p.WorkspaceRoots) {
		return true
	}
	for _, a := range implicitActions(list, action) {
		if grantCovers(a, r.Pattern, req.Tool, req.Target, p.WorkspaceRoots) {
			return true
		}
	}
	return false
}

// StrictFromEnv reports whether OPENAGY_PERMISSION_POLICY requests the
// reference default (strict) rather than the clone's allow-by-default.
func StrictFromEnv(v string) bool {
	return strings.EqualFold(strings.TrimSpace(v), "strict")
}

// Decide returns the verdict for one tool call. Order, matching the reference:
// skip-permissions wins outright; an explicit deny rule blocks; an explicit
// ask rule prompts; an explicit allow rule permits; then the toolPermission
// policy and the mode's auto-approval rules; then the default.
func (p Policy) Decide(req Request) Verdict {
	return p.DecideNames(req.Target, req.Tool)
}

// DecideNames returns the verdict for one tool call under any of the names
// the same tool is known by.
//
// The reference publishes two spellings of every tool: the twelve
// model-facing declarations (read_file, write_file, edit_file, …) and the
// catalogue the advertised surface names (view_file, write_to_file,
// replace_file_content, …). A subagent calls the catalogue's spelling
// (its tool set is drawn from the catalogue, the way a custom agent's is —
// 【一手】 the 1.4.7 custom-agent capture), while the parent calls the
// declaration's. A rule the user wrote for the parent must not stop
// applying to the child, so the call is judged under both spellings.
//
// The order is the reference's, and it is applied across the spellings
// together: a deny rule that matches under either spelling blocks, an ask
// rule that matches under either spelling prompts, an allow rule that
// matches under either spelling permits, and only then do the
// toolPermission policy, the mode's auto-approval rules and the default
// decide.
//
// The full order is:
//
//  1. --dangerously-skip-permissions wins outright.
//  2. the deny list.
//  3. the ask list — a matching ask rule prompts even when an allow rule
//     matches too ("Deny > Ask > Allow").
//  4. the allow list.
//  5. the two toolPermission values the reference gives precedence over
//     the execution mode ("strict and always-proceed take precedence over
//     auto mode"). The rule lists still come first: an explicit allow
//     rule is the user's own decision.
//  6. the mode's auto-approval rules.
//  7. the two remaining toolPermission values (request-review and
//     proceed-in-sandbox). They are the *default* policies, so they come
//     after the mode: accept-edits still approves a file edit under
//     request-review.
//  8. OPENAGY_PERMISSION_POLICY=strict.
//  9. the default — Allow, the clone's documented divergence.
//
// A name that classifies as nothing (the MCP tools, the browser tools) does
// not narrow the other spelling's class: the mode's auto-approval looks at
// the strictest class any spelling carries.
func (p Policy) DecideNames(target string, names ...string) Verdict {
	if p.SkipPermissions {
		return Allow
	}
	if len(names) == 0 {
		names = []string{""}
	}
	for _, n := range names {
		for _, r := range p.Deny {
			if p.covers(r, listDeny, Request{Tool: n, Target: target}) {
				return Deny
			}
		}
	}
	for _, n := range names {
		for _, r := range p.Ask {
			if p.covers(r, listAsk, Request{Tool: n, Target: target}) {
				return Ask
			}
		}
	}
	for _, n := range names {
		for _, r := range p.Allow {
			if p.covers(r, listAllow, Request{Tool: n, Target: target}) {
				return Allow
			}
		}
	}
	switch p.ToolPermission {
	case ToolPermissionAlwaysProceed:
		return Allow
	case ToolPermissionStrict:
		return Ask
	}
	cls := ClassOther
	for _, n := range names {
		if c := Classify(n); c != ClassOther {
			if cls == ClassOther || classRank(c) > classRank(cls) {
				cls = c
			}
		}
	}
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
	switch p.ToolPermission {
	case ToolPermissionProceedInSandbox:
		// A command runs without prompting while the sandbox runs it; the
		// sandbox's own gate is a separate decision and still applies.
		if p.SandboxEnabled && isCommandCall(names) {
			return Allow
		}
		if requiresReview(names) {
			return Ask
		}
		return Allow
	case ToolPermissionRequestReview:
		if requiresReview(names) {
			return Ask
		}
		return Allow
	}
	if p.Strict {
		return Ask
	}
	return Allow
}

// HookVerdict maps a hooks.json decision onto the tool-permission verdict.
//
// The six documented decisions (internal/core/policy/hooks) are a hook's
// own answer about one tool call; the verdict is the clone's answer about
// the same call:
//
//   - "allow" lets the call proceed.
//   - "auto_approve" approves it, unless one of the user's own deny rules
//     matches — the rules the user configured explicitly are honoured.
//   - "deny" blocks it.
//   - "ask" prompts the user.
//   - "force_ask" prompts the user whatever the cached permissions say.
//   - "deny_unless_prior_grant" blocks the call unless one of the user's
//     allow rules covers it — that grant is the user's own "always
//     allow".
//
// The "ask" decision is the one that respects the user's own "always
// allow" grants: the verdict is a prompt, and the caller asks
// HasGrant whether a grant covers the call before it prompts.
// "force_ask" is the decision that ignores them.
//
// A decision the documentation does not name is a prompt: an unusable
// answer must not let a tool call through.
//
// The tool is judged under both of its names — the catalogue's and the
// declaration's — the same way DecideNames judges it.
func (p Policy) HookVerdict(dec corehooks.Decision, target string, names ...string) Verdict {
	switch dec {
	case corehooks.DecisionAllow:
		return Allow
	case corehooks.DecisionAutoApprove:
		if p.explicitDeny(target, names...) {
			return Deny
		}
		return Allow
	case corehooks.DecisionDeny:
		return Deny
	case corehooks.DecisionAsk:
		return Ask
	case corehooks.DecisionForceAsk:
		return ForceAsk
	case corehooks.DecisionDenyUnlessPriorGrant:
		if p.explicitGrant(target, names...) {
			return Allow
		}
		return Deny
	}
	return Ask
}

// HasGrant reports whether one of the user's own allow rules — the
// "always allow" grants — covers the call.
//
// It is the half of the hook mapping the host needs and the verdict
// cannot answer: a hook's "ask" decision respects those grants, and the
// policy's own verdict does not tell a grant apart from the default.
func (p Policy) HasGrant(target string, names ...string) bool {
	return p.explicitGrant(target, names...)
}

// explicitDeny reports whether one of the user's own deny rules matches
// the call.
func (p Policy) explicitDeny(target string, names ...string) bool {
	return p.matchesAny(listDeny, p.Deny, target, names)
}

// explicitGrant reports whether one of the user's own allow rules — the
// "always allow" grants — covers the call.
func (p Policy) explicitGrant(target string, names ...string) bool {
	return p.matchesAny(listAllow, p.Allow, target, names)
}

// matchesAny reports whether one of the rules matches the call under any
// of the names the tool is known by.
func (p Policy) matchesAny(list ruleList, rules []Rule, target string, names []string) bool {
	if len(names) == 0 {
		names = []string{""}
	}
	for _, n := range names {
		for _, r := range rules {
			if p.covers(r, list, Request{Tool: n, Target: target}) {
				return true
			}
		}
	}
	return false
}

// classRank orders the classes by how much a mode's auto-approval should
// hesitate: a read is the weakest, a file edit the strongest.
func classRank(c Class) int {
	switch c {
	case ClassRead:
		return 1
	case ClassOther:
		return 2
	case ClassCommand:
		return 3
	case ClassEdit:
		return 4
	}
	return 0
}

// HeadlessNotice is the reference's end-of-run notice about tool calls it
// auto-denied because headless mode cannot prompt for them. Recovered
// verbatim; `n` is the count of denied calls.
func HeadlessNotice(n int) string {
	return "the " + strconv.Itoa(n) + " tool(s) required approval that headless mode cannot prompt for, " +
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
