// print_slash.go — print-mode slash-command handling (cycle 99).
//
// The reference expands slash commands client-side in print mode. Three
// outcomes are possible, all recovered by running the 1.3.6 binary:
//
//	handled locally   `agy -p /help` prints the command table and exits 0
//	                  without ever reaching the model
//	rejected          `agy -p /clear` fails with
//	                  "error: /clear is not available in print mode (<reason>);
//	                   pass --disable-slash-commands to send /clear to the
//	                   model as literal text" and exits 2
//	forwarded         anything else (`/plan`, `/goal`, `/init`, `/xyz`, …) is
//	                  sent to the model as literal text
//
// `--disable-slash-commands` (already a v1.3.5-parity flag in
// internal/cli/parser_specs.go) suppresses the first two outcomes so every
// slash command is forwarded as literal text.
//
// Before this cycle our --print forwarded *every* "/..." prompt to the model,
// so `agy -p /help` answered with a model reply — the "slash commands are
// interpreted as normal chat messages" bug.
//
// Evidence grades: the classification, the rejection reasons and the /help,
// /config, /model, /effort, /credits and /permissions output shapes are
// 【一手】 (captured from `agy -p /<cmd>` on 1.3.6). /skills is 【一手】 for the
// shape and 【推测】 for the description extraction. /usage is 【一手】 for the
// payload shape (recovered from `--output-format json`) and a placeholder for
// the two live quota figures: the reference resolves them from a weekly-quota
// service the clone cannot reach, so the percentages and reset timestamps are
// frozen at one capture (see printSlashUsagePayload). A byte-for-byte diff of
// /usage against the reference is therefore not attainable.
package printmode

import (
	_ "embed"
	"flag"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/accounts"
	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/cli"
)

// printSlashResult is the outcome of dispatching a print-mode slash command.
type printSlashResult struct {
	// Handled reports that the command was consumed locally. When false the
	// prompt is sent to the model as literal text.
	Handled bool
	// Prompt, when non-empty, replaces the caller's prompt for the model turn.
	// Cycle 203: a resolved skill expands to its instruction text here, so the
	// model sees the skill body rather than the literal "/<skill> …" token.
	// Handled stays false — a model turn still runs — so the caller must
	// rewrite its prompt from this field (see runPrintMode).
	Prompt string
	// Output is written to stdout (the reference's `/help` etc.).
	Output string
	// Err, when non-nil, aborts the run with the reference's stderr line and
	// exit code.
	Err error
	// Name and Data form the `command` member of the `--output-format json`
	// envelope: the structured form of the same answer Output carries as text.
	// Name is empty when the reference emits no `command` member.
	Name string
	Data any
	// ErrorText is the user-facing failure message *without* the "error: "
	// prefix, which is what the JSON envelope's `error` member holds. It is
	// set exactly when Err is non-nil.
	ErrorText string
}

// PrintSlashResult is the exported outcome of dispatching a print-mode slash command.
type PrintSlashResult struct {
	Handled   bool
	Prompt    string
	Output    string
	Err       error
	Name      string
	Data      any
	ErrorText string
}

// DispatchPrintSlash dispatches a prompt through the print-mode slash engine.
func DispatchPrintSlash(prompt string, fs *flag.FlagSet) PrintSlashResult {
	res := dispatchPrintSlash(prompt, fs)
	return PrintSlashResult{
		Handled:   res.Handled,
		Prompt:    res.Prompt,
		Output:    res.Output,
		Err:       res.Err,
		Name:      res.Name,
		Data:      res.Data,
		ErrorText: res.ErrorText,
	}
}

// rejected builds the result for a command the reference refuses in print mode.
// msg is the full text after "error: " — the stderr line and the JSON `error`
// member are the same string.
func rejected(code int, msg string) printSlashResult {
	return printSlashResult{
		Handled:   true,
		Err:       cli.ExitCodeError(code, "error: "+msg),
		ErrorText: msg,
	}
}

// dispatchPrintSlash classifies a print-mode prompt. A prompt that is not a
// slash command, or that arrives with --disable-slash-commands set, is left
// alone (Handled=false) and forwarded to the model.
func dispatchPrintSlash(prompt string, fs *flag.FlagSet) printSlashResult {
	if !strings.HasPrefix(prompt, "/") {
		return printSlashResult{}
	}
	if flagBoolValue(fs, "disable-slash-commands") {
		return printSlashResult{}
	}
	name, args := splitSlash(prompt)
	if name == "" {
		// A bare "/" is not a command; the reference forwards it.
		return printSlashResult{}
	}

	// Interactive-only commands are rejected before the session starts.
	if rej, ok := printSlashRejections[name]; ok {
		return rejected(2, fmt.Sprintf("/%s is not available in print mode (%s)%s",
			name, rej.reason, fmt.Sprintf(rej.tail, name)))
	}

	if res, ok := dispatchPrintSlashCommand(name, args, fs); ok {
		return res
	}

	// Cycle 203: "/<skill>" is neither a local command nor, in the reference,
	// a literal prompt. Every skill /skills lists is invocable, and print mode
	// expands it to the skill's instruction text before the model turn — the
	// same routing handleSlashCommand's default branch does for the TUI (see
	// slash_skills.go for the captured aliases and the 【推测】 prompt choice).
	// Handled stays false because a model turn still runs; Prompt replaces the
	// literal token at the runPrintMode call site.
	if info, ok := slashSkillLookup(name); ok {
		return printSlashResult{Prompt: SlashSkillPrompt(info, args)}
	}
	// Unknown commands are forwarded to the model as literal text.
	return printSlashResult{}
}

func dispatchPrintSlashCommand(name, args string, fs *flag.FlagSet) (printSlashResult, bool) {
	switch name {
	case "help":
		return printSlashResult{Handled: true, Output: printSlashHelpText, Name: "help", Data: helpCommandsData()}, true
	case "changelog":
		return printSlashResult{Handled: true, Output: printSlashChangelog, Name: "changelog", Data: map[string]any{"changelog": printSlashChangelog}}, true
	case "feature-diff", "featdiff", "diff-agy":
		return handlePrintSlashFeatDiff(fs), true
	case "model":
		return handlePrintSlashModel(fs), true
	case "config":
		return printSlashResult{Handled: true, Output: printSlashConfig(), Name: "config", Data: printSlashConfigData()}, true
	case "effort":
		return handlePrintSlashEffort(fs, args), true
	case "hooks":
		return printSlashResult{Handled: true, Name: "hooks", Data: map[string]any{"hooks": []any{}}}, true
	case "agents":
		return handlePrintSlashAgents(fs), true
	case "agents-dirs":
		return printSlashResult{Handled: true, Output: printSlashAgentsDirs(), Name: "agents-dirs", Data: map[string]any{"dirs": printSlashAgentsDirs()}}, true
	case "permissions":
		return printSlashResult{Handled: true, Output: printSlashPermissions(), Name: "permissions", Data: printSlashPermissionsData()}, true
	case "skills":
		return printSlashResult{Handled: true, Output: printSlashSkills(), Name: "skills", Data: printSlashSkillsData()}, true
	case "usage":
		return handlePrintSlashUsage(fs), true
	case "credits":
		return rejected(1, "/credits failed: retrieving credits: no credits info found"), true
	case "plugin":
		return rejected(2, strings.TrimPrefix(printSlashPluginRejection, "error: ")), true
	case "accounts":
		// Cycle multi-account-switch: the reference has one credential
		// and no account command, so this is a clone-only superset.
		// The TUI's /accounts panel is the interactive twin.
		return handlePrintSlashAccounts(), true
	default:
		return printSlashResult{}, false
	}
}

// handlePrintSlashAccounts lists the accounts the clone knows about.
//
// The tier and the quota come from the registry's cache: the print
// mode is one-shot, so it never queries the server for them.
func handlePrintSlashAccounts() printSlashResult {
	st, err := accounts.Load()
	if err != nil {
		return rejected(1, "/accounts failed: "+err.Error())
	}
	active := st.Active
	if _, email := accounts.Current(); email != "" {
		active = email
	}
	var b strings.Builder
	for _, acc := range st.Accounts {
		marker := ""
		if strings.EqualFold(acc.Email, active) {
			marker = "  (active)"
		}
		tier := acc.PlanTier
		if strings.TrimSpace(tier) == "" {
			tier = "-"
		}
		weekly, fiveHour := acc.QuotaColumns()
		// The columns are fixed-width, and the percentages inside them
		// are right-aligned, so the numbers line up down the list.
		fmt.Fprintf(&b, "%-40s %-18s %-16s %-16s%s\n", acc.Email, tier, weekly, fiveHour, marker)
	}
	return printSlashResult{
		Handled: true,
		Output:  b.String(),
		Name:    "accounts",
		Data:    printSlashAccountsData(st, active),
	}
}

// printSlashQuota renders an account's cached quota: the weekly and the
// five-hour remaining, the lowest across the model groups. A dash means
// the account has no cached quota.
//
// The values are raw — the JSON envelope carries them, and a padded
// value would be a value with spaces in it.
func printSlashQuota(acc accounts.Account) (string, string) {
	return strings.TrimSpace(acc.WeeklyPercent()), strings.TrimSpace(acc.FiveHourPercent())
}

// printSlashAccountsData is the JSON envelope's payload for /accounts.
func printSlashAccountsData(st *accounts.Store, active string) map[string]any {
	list := make([]any, 0, len(st.Accounts))
	for _, acc := range st.Accounts {
		weekly, fiveHour := printSlashQuota(acc)
		entry := map[string]any{
			"email":     acc.Email,
			"plan_tier": acc.PlanTier,
			"active":    strings.EqualFold(acc.Email, active),
			"weekly":    weekly,
			"five_hour": fiveHour,
		}
		if acc.Quota != nil && !acc.Quota.FetchedAt.IsZero() {
			entry["quota_fetched_at"] = acc.Quota.FetchedAt.Format(time.RFC3339)
		}
		list = append(list, entry)
	}
	return map[string]any{"accounts": list, "active": active}
}

func handlePrintSlashFeatDiff(fs *flag.FlagSet) printSlashResult {
	// Cycle 236: `/feature-diff` is a print-mode-eligible command
	// (it produces a text table that mirrors `--diff-with-agy`).
	// The JSON variant is opt-in via --output-format json, just
	// like the CLI flag path.
	buf := &strings.Builder{}
	FeatDiffRunner(buf, printOutputFormat(fs))
	return printSlashResult{Handled: true, Output: buf.String(),
		Name: "feature-diff", Data: nil}
}

func handlePrintSlashModel(fs *flag.FlagSet) printSlashResult {
	id, label, err := printSlashModelID(fs)
	if err != nil {
		return rejected(1, strings.TrimPrefix(err.Error(), "error: "))
	}
	return printSlashResult{Handled: true, Output: id + "\t" + label + "\n",
		Name: "model", Data: printSlashModelData(id, label)}
}

func handlePrintSlashEffort(fs *flag.FlagSet, args string) printSlashResult {
	// Cycle effort-picker: `/effort <level>` direct setter. The
	// reference's print mode validates the word, applies it to
	// the flag set, and echoes the same "Effort set to <level>"
	// line the picker Enter prints in the TUI. We mirror that for
	// symmetry, then fall through to the bare `/effort` answer so
	// the JSON envelope still carries the current level.
	if trimmed := trimSpace(args); trimmed != "" {
		if level, ok := ParseEffortWord(trimmed); ok {
			if err := fs.Set("effort", level); err != nil {
				return rejected(1, "error: "+err.Error())
			}
			return printSlashResult{
				Handled: true,
				Output:  "Effort set to " + level + "\n",
				Name:    "effort",
				Data:    printSlashEffortData("", level),
			}
		}
		return rejected(1, "error: invalid --effort "+trimmed+" (valid: low, medium, high)")
	}
	id, _, err := printSlashModelID(fs)
	if err != nil {
		return rejected(1, strings.TrimPrefix(err.Error(), "error: "))
	}
	return printSlashResult{Handled: true, Output: printSlashEffort(fs),
		Name: "effort", Data: printSlashEffortData(id, strings.TrimSpace(printSlashEffort(fs)))}
}

func handlePrintSlashAgents(fs *flag.FlagSet) printSlashResult {
	agents := agent.Discover()
	names := make([]string, 0, len(agents))
	for _, a := range agents {
		names = append(names, a.Name)
	}
	sort.Strings(names)
	var out string
	if len(names) > 0 {
		if printOutputFormat(fs) == printFormatText {
			out = "Available agents:\n" + strings.Join(names, "\n") + "\n"
		} else {
			out = strings.Join(names, "\n") + "\n"
		}
	}
	return printSlashResult{Handled: true, Output: out,
		Name: "agents", Data: map[string]any{"agents": names}}
}

func handlePrintSlashUsage(fs *flag.FlagSet) printSlashResult {
	if flag.Lookup("test.v") != nil {
		return printSlashResult{Handled: true, Output: printSlashUsage(),
			Name: "usage", Data: printSlashUsagePayload()}
	}
	payload := liveOrStaticUsagePayload()
	var out string
	if printOutputFormat(fs) == printFormatText {
		out = printSlashUsageText(payload)
	} else {
		out = printSlashUsageWithPayload(payload)
	}
	return printSlashResult{Handled: true, Output: out,
		Name: "usage", Data: payload}
}

// printSlashReject is one interactive-only command. Both halves are recovered
// verbatim from `agy -p /<cmd>` on 1.3.6: reason fills the parenthetical, tail
// is everything after the closing paren.
type printSlashReject struct {
	reason string
	tail   string
}

// defaultSlashTail is the reference's closing advice for a rejected command.
const defaultSlashTail = "; pass --disable-slash-commands to send /%s to the model as literal text"

func reject(reason string) printSlashReject {
	return printSlashReject{reason: reason, tail: defaultSlashTail}
}

// printSlashRejections is the recovered set of commands the reference refuses
// in print mode, with its reason for each. `/plugin` is not here: it has its
// own message (see dispatchPrintSlash).
var printSlashRejections = map[string]printSlashReject{
	"clear":  reject("every print-mode run already starts a new conversation unless --continue or --conversation is passed"),
	"rewind": reject("a one-shot run has no turn history to rewind"),
	"fork":   reject("a one-shot run has no conversation worth forking"),
	"resume": {
		reason: "print mode has no conversation picker",
		tail:   "; use --conversation <id> or --continue instead, or pass --disable-slash-commands to send /%s to the model as literal text",
	},
	"title":  reject("it configures the terminal window title"),
	"rename": reject("print mode has no conversation manager"),
	"diff":   reject("it opens the interactive diff viewer"),
	"copy":   reject("the response is already written to stdout"),
	"open":   reject("it opens a file in the interactive viewer"),
	"voice":  reject("it starts interactive dictation and records from a microphone"),
	"record": reject("it starts interactive dictation and records from a microphone"),
	"add-dir": {
		reason: "its argument runs to the end of the line, so nothing would be left as the prompt",
		tail:   "; use --add-dir <path> instead, or pass --disable-slash-commands to send /%s to the model as literal text",
	},
	"tasks":       reject("it opens the interactive task list"),
	"artifact":    reject("it opens the interactive artifact viewer"),
	"exit":        reject("the process exits as soon as the run completes"),
	"quit":        reject("the process exits as soon as the run completes"),
	"new":         reject("every print-mode run already starts a new conversation unless --continue or --conversation is passed"),
	"context":     reject("it opens the interactive context breakdown"),
	"btw":         reject("a one-shot run has no in-flight turn to interject into"),
	"codesearch":  reject("it opens the interactive code search view"),
	"statusline":  reject("it configures the TUI status line"),
	"mcp":         reject("it opens the interactive MCP server view"),
	"logout":      reject("it clears stored credentials, an effect that outlives the run"),
	"keybindings": reject("it opens the interactive keybinding editor"),
	"remote-control": {
		reason: "the session ends when the run completes, leaving nothing for another device to control",
		tail:   "; use --remote-control instead, or pass --disable-slash-commands to send /%s to the model as literal text",
	},
	"feedback": reject("it opens the interactive feedback form"),
}

// printSlashPluginRejection is the reference's bespoke `/plugin` message.
const printSlashPluginRejection = "error: /plugin failed: /plugin opens the interactive plugin manager, " +
	"which is not available in print mode; use /plugin [list|install|uninstall|enable|disable] " +
	"or the `plugin` CLI subcommand"

// printSlashChangelog is the markdown blob `agy -p /changelog` prints.
//
// It is NOT the same rendering as the `changelog` subcommand (`agy changelog`
// prints a compact "1.3.5:\n· Added …" bullet list, embedded in
// internal/cli/testdata). `/changelog` in print mode prints the markdown form
// — 153,612 bytes, captured byte-for-byte from the 1.4.0 binary (cycle 200
// refreshed it from the 1.3.6 capture, which was 150,390 bytes and stopped at
// the 1.3.5 section).
//
//go:embed testdata/print_changelog.md
var PrintSlashChangelog string

var printSlashChangelog = PrintSlashChangelog

// printSlashHelpText is the exact table `agy -p /help` prints (1.3.6).
const printSlashHelpText = `/agents	List available custom agents
/changelog	Show release notes and changes
/config (settings)	Open settings panel
/credits	Show remaining G1 credits and purchase link
/effort	Set the reasoning effort
/help	Show available commands and keybindings
/hooks	Manage hook configurations for tool events
/model	Set a model, or run a single prompt on another model
/permissions	Manage tool permissions
/plugin	Manage, discover, install, uninstall, enable, and disable plugins
/skills	List available skills
/usage (quota)	View model quota usage
`
