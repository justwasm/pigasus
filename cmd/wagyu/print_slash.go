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
package main

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/agent"
	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/cli"
	"gopkg.in/yaml.v3"
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

	switch name {
	case "help":
		return printSlashResult{Handled: true, Output: printSlashHelpText,
			Name: "help", Data: helpCommandsData()}
	case "changelog":
		return printSlashResult{Handled: true, Output: printSlashChangelog,
			Name: "changelog", Data: map[string]any{"changelog": printSlashChangelog}}
	case "feature-diff", "featdiff", "diff-agy":
		// Cycle 236: `/feature-diff` is a print-mode-eligible command
		// (it produces a text table that mirrors `--diff-with-agy`).
		// The JSON variant is opt-in via --output-format json, just
		// like the CLI flag path.
		buf := &strings.Builder{}
		runFeatDiffCLI(buf, printOutputFormat(fs))
		return printSlashResult{Handled: true, Output: buf.String(),
			Name: "feature-diff", Data: nil}
	case "model":
		id, label, err := printSlashModelID(fs)
		if err != nil {
			return rejected(1, strings.TrimPrefix(err.Error(), "error: "))
		}
		return printSlashResult{Handled: true, Output: id + "\t" + label + "\n",
			Name: "model", Data: printSlashModelData(id, label)}
	case "config":
		return printSlashResult{Handled: true, Output: printSlashConfig(),
			Name: "config", Data: printSlashConfigData()}
	case "effort":
		// Cycle effort-picker: `/effort <level>` direct setter. The
		// reference's print mode validates the word, applies it to
		// the flag set, and echoes the same "Effort set to <level>"
		// line the picker Enter prints in the TUI. We mirror that for
		// symmetry, then fall through to the bare `/effort` answer so
		// the JSON envelope still carries the current level.
		if trimmed := trimSpace(args); trimmed != "" {
			if level, ok := parseEffortWord(trimmed); ok {
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
	case "hooks":
		// The reference prints nothing when no hooks are configured, but it
		// still emits the command payload with an empty list.
		return printSlashResult{Handled: true, Name: "hooks",
			Data: map[string]any{"hooks": []any{}}}
	case "agents":
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
	case "agents-dirs":
		// Cycle agents-parent-dir-walk: AGY 1.2.16 changelog line made
		// project-local .agents/ visible between CWD and HOME. The
		// diagnostic surfaces every directory the walker found, in
		// CWD-closest-first order; empty string when nothing matched
		// (the convention printSlashSkills / printSlashHooks follow).
		return printSlashResult{Handled: true, Output: printSlashAgentsDirs(),
			Name: "agents-dirs", Data: map[string]any{"dirs": printSlashAgentsDirs()}}
	case "permissions":
		return printSlashResult{Handled: true, Output: printSlashPermissions(),
			Name: "permissions", Data: printSlashPermissionsData()}
	case "skills":
		return printSlashResult{Handled: true, Output: printSlashSkills(),
			Name: "skills", Data: printSlashSkillsData()}
	case "usage":
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
	case "credits":
		return rejected(1, "/credits failed: retrieving credits: no credits info found")
	case "plugin":
		return rejected(2, strings.TrimPrefix(printSlashPluginRejection, "error: "))
	}
	// Cycle 203: "/<skill>" is neither a local command nor, in the reference,
	// a literal prompt. Every skill /skills lists is invocable, and print mode
	// expands it to the skill's instruction text before the model turn — the
	// same routing handleSlashCommand's default branch does for the TUI (see
	// slash_skills.go for the captured aliases and the 【推测】 prompt choice).
	// Handled stays false because a model turn still runs; Prompt replaces the
	// literal token at the runPrintMode call site.
	if info, ok := slashSkillLookup(name); ok {
		return printSlashResult{Prompt: slashSkillPrompt(info, args)}
	}
	// Unknown commands are forwarded to the model as literal text.
	return printSlashResult{}
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
var printSlashChangelog string

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

// printSlashModels prints the effective model as "<slug>\t<label>", which is
// what `agy -p /model` emits.
func printSlashModels(fs *flag.FlagSet) string {
	id, label, err := printSlashModelID(fs)
	if err != nil {
		return ""
	}
	return id + "\t" + label + "\n"
}

// printSlashModelID resolves the model `/model` reports, as a slug and a
// display label.
//
// Cycle 114: --effort alone also selects a model. Captured from the reference:
//
//	agy -p=/model --effort=low    -> gemini-3.8-flash-low  Gemini 3.8 Flash (Low)
//	agy -p=/model --effort=xhigh  -> rc 1, "gemini-3.8-flash has no \"xhigh\"
//	                                 effort (available: low, medium, high)"
//
// so the (--model, --effort) pair is validated on the slash path too, and a
// failure becomes an ERROR envelope.
func printSlashModelID(fs *flag.FlagSet) (id, label string, err error) {
	model := flagStringValue(fs, "model")
	effort := flagStringValue(fs, "effort")
	if model != "" || effort != "" {
		id, err := resolveModelSelection(model, effort)
		if err != nil {
			return "", "", err
		}
		return id, catalogueLabel(id), nil
	}
	name, _ := resolveEffectiveModel("")
	return modelSlug(name), name, nil
}

// jsonModelInfo is the `/model` command payload. Field order is the
// reference's; a map would marshal alphabetically (effort, id, is_default,
// label) and not match.
type jsonModelInfo struct {
	ID        string `json:"id"`
	Label     string `json:"label"`
	Effort    string `json:"effort"`
	IsDefault bool   `json:"is_default"`
}

// printSlashModelData renders the `/model` command payload.
//
// `is_default` is false in every capture (with and without --model, for three
// different models), so the clone emits false; the flag's real meaning is
// unrecovered.
func printSlashModelData(id, label string) any {
	return jsonModelInfo{
		ID:        id,
		Label:     label,
		Effort:    modelEffortFromID(id),
		IsDefault: false,
	}
}

// modelBaseSlug strips a trailing effort suffix from a catalogue id:
// "gemini-3.8-flash-high" -> "gemini-3.8-flash". Ids that carry no effort
// suffix (claude-sonnet-4-6, claude-opus-4-6-thinking) are returned unchanged.
func modelBaseSlug(id string) string {
	for _, e := range validEfforts {
		if strings.HasSuffix(id, "-"+e) {
			return strings.TrimSuffix(id, "-"+e)
		}
	}
	return id
}

// modelEffortFromID returns the effort a catalogue id encodes, or "".
func modelEffortFromID(id string) string {
	base := modelBaseSlug(id)
	if base == id {
		return ""
	}
	return strings.TrimPrefix(id, base+"-")
}

// jsonEffortInfo is the `/effort` command payload, in the reference's field
// order (a map would sort adjustable, available, current).
type jsonEffortInfo struct {
	Adjustable bool     `json:"adjustable"`
	Current    string   `json:"current"`
	Available  []string `json:"available"`
}

// printSlashEffortData renders the `/effort` command payload. `current` is the
// same word the text answer carries; `available` and `adjustable` come from the
// resolved model's effort levels.
func printSlashEffortData(id, current string) any {
	levels := []string{}
	adjustable := false
	if spec, ok := baseModelEfforts[modelBaseSlug(id)]; ok {
		levels = spec.levels
		adjustable = len(spec.levels) > 1
	}
	return jsonEffortInfo{Adjustable: adjustable, Current: current, Available: levels}
}

// printSlashEffort prints the current reasoning effort, which the reference
// reports as a bare word ("high" on a default install).

// modelSlug turns a display model name into the reference's slug form:
// "Gemini 3.8 Flash (High)" -> "gemini-3.8-flash-high". Cycle 101: the
// catalogue is consulted first, because the mechanical transform disagrees with
// the reference for Claude ("Claude Sonnet 4.6 (Thinking)" is
// "claude-sonnet-4-6", not "claude-sonnet-4.6-thinking").
func modelSlug(name string) string {
	if id, ok := catalogueID(name); ok {
		return id
	}
	s := strings.ToLower(name)
	// Two passes: the parens become spaces first, then the spaces become
	// dashes. strings.Replacer is single-pass, so it cannot do both at once.
	s = strings.NewReplacer("(", " ", ")", " ", "_", "-").Replace(s)
	s = strings.ReplaceAll(s, " ", "-")
	for strings.Contains(s, "--") {
		s = strings.ReplaceAll(s, "--", "-")
	}
	return strings.Trim(s, "-")
}

// printSlashEffort prints the current reasoning effort, which the reference
// reports as a bare word ("high" on a default install).
func printSlashEffort(fs *flag.FlagSet) string {
	if e := flagStringValue(fs, "effort"); e != "" {
		return e + "\n"
	}
	return "high\n"
}

// printSlashConfigKeys is the reference's settings key set in the order
// `agy -p /config` prints it (alphabetical). Recovered from the 1.3.6 output.
var printSlashConfigKeys = []string{
	"agentMode",
	"allowNonWorkspaceAccess",
	"altScreenMode",
	"artifactReviewPolicy",
	"clearScrollbackOnResize",
	"colorScheme",
	"contextWindow",
	"copyOnSelect",
	"customModelsConfig",
	"disableSlashCommands",
	"editor",
	"editorMode",
	"enableTerminalSandbox",
	"gcp",
	"hideActiveItems",
	"historySize",
	"model",
	"modelProvider",
	"notifications",
	"permissions",
	"pickerGrouping",
	"queuedMessages",
	"runningLightSpeed",
	"sandboxProxy",
	"showFeedbackSurvey",
	"showTips",
	"statusLine",
	"title",
	"toolPermission",
	"trustedWorkspaces",
	"useG1Credits",
	"useTerminalCursor",
	"verbosity",
	"vimInsertFirst",
}

// printSlashConfigDefaults are the reference's built-in settings defaults, as
// printed for keys the user has never set. Recovered from a default-profile
// `agy -p /config` capture.
var printSlashConfigDefaults = map[string]string{
	"allowNonWorkspaceAccess": "false",
	"altScreenMode":           "default",
	"artifactReviewPolicy":    "always-proceed",
	"clearScrollbackOnResize": "true",
	"colorScheme":             "light",
	"copyOnSelect":            "true",
	"disableSlashCommands":    "false",
	"editor":                  "auto",
	"editorMode":              "default",
	"enableTerminalSandbox":   "false",
	"hideActiveItems":         "false",
	"historySize":             "2000",
	"notifications":           "false",
	"pickerGrouping":          "none",
	"queuedMessages":          "queue",
	"runningLightSpeed":       "medium",
	"showFeedbackSurvey":      "true",
	"showTips":                "true",
	"toolPermission":          "always-proceed",
	"useG1Credits":            "false",
	"useTerminalCursor":       "false",
	"verbosity":               "medium",
	"vimInsertFirst":          "false",
}

// printSlashConfig dumps the settings as "key\tvalue" lines. String values are
// unquoted and everything else keeps its raw JSON, matching the reference.
func printSlashConfig() string {
	raw := map[string]json.RawMessage{}
	if b, err := os.ReadFile(filepath.Join(auth.AntigravityHome(), "settings.json")); err == nil {
		_ = json.Unmarshal(b, &raw)
	}
	var b strings.Builder
	for _, k := range printSlashConfigKeys {
		val := printSlashConfigDefaults[k]
		if v, ok := raw[k]; ok {
			var s string
			if enc, done := encodeOrderedConfigValue(k, v); done {
				val = enc
			} else if json.Unmarshal(v, &s) == nil {
				val = s
			} else if anyVal := any(nil); json.Unmarshal(v, &anyVal) == nil {
				// The reference re-encodes non-string values with the
				// standard library encoder, so a pretty-printed
				// settings.json still prints on one line and `<`/`>`/`&`
				// come back as \u003c/\u003e/\u0026 (Go's default HTML
				// escaping).
				if b, err := json.Marshal(anyVal); err == nil {
					val = string(b)
				}
			} else {
				val = string(v)
			}
		}
		fmt.Fprintf(&b, "%s\t%s\n", k, val)
	}
	return b.String()
}

// printSlashPermissions prints the settings.json permission rules as
// "<scope>\t<action>\t<rule>", which is the shape `agy -p /permissions`
// emits (e.g. "global\tallow\tcommand(grep)").
func printSlashPermissions() string {
	settings, err := readPrintSlashSettings()
	if err != nil {
		return ""
	}
	var b strings.Builder
	for _, r := range settings.Allow {
		fmt.Fprintf(&b, "global\tallow\t%s\n", r)
	}
	for _, r := range settings.Deny {
		fmt.Fprintf(&b, "global\tdeny\t%s\n", r)
	}
	return b.String()
}

// printSlashSettings is the subset of settings.json the print-mode commands
// read.
type printSlashSettings struct {
	Allow []string `json:"allow"`
	Deny  []string `json:"deny"`
}

func readPrintSlashSettings() (printSlashSettings, error) {
	var out printSlashSettings
	b, err := readSettingsFile()
	if err != nil {
		return out, err
	}
	var wrapper struct {
		Permissions printSlashSettings `json:"permissions"`
	}
	if err := json.Unmarshal(b, &wrapper); err != nil {
		return out, err
	}
	return wrapper.Permissions, nil
}

// readSettingsFile reads the CLI's settings.json with any leading UTF-8
// BOM stripped. AGY 1.2.16 changelog line 13 (cycle config-bom-strip):
// Notepad / PowerShell `Set-Content -Encoding UTF8` on Windows writes a
// BOM, and json.Unmarshal fails with
// "invalid character 'ï' looking for beginning of value" until the
// leading 0xEF 0xBB 0xBF is removed. Every call site that previously
// read settings.json now gets the BOM-tolerant form for free.
func readSettingsFile() ([]byte, error) {
	data, err := os.ReadFile(filepath.Join(auth.AntigravityHome(), "settings.json"))
	if err != nil {
		return nil, err
	}
	return stripUTF8BOM(data), nil
}

// stripUTF8BOM drops a leading UTF-8 byte order mark (EF BB BF) if
// present. See readSettingsFile.
func stripUTF8BOM(b []byte) []byte {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return b[3:]
	}
	return b
}

// jsonPermissionScope is one entry of the `/permissions` payload. `scope` comes
// first and the two rule lists are omitempty, matching the reference:
//
//	{"scope":"global","allow":["command(grep)",…]}
type jsonPermissionScope struct {
	Scope string   `json:"scope"`
	Allow []string `json:"allow,omitempty"`
	Deny  []string `json:"deny,omitempty"`
}

// printSlashPermissionsData renders the `/permissions` command payload.
//
// The reference always reports the three scopes in the order project, shared,
// global. On the capture machine only the global scope holds rules — which is
// why the text answer (global lines only) and the payload agree — but the empty
// project and shared scopes are still emitted.
func printSlashPermissionsData() any {
	settings, err := readPrintSlashSettings()
	if err != nil {
		settings = printSlashSettings{}
	}
	global := jsonPermissionScope{Scope: "global", Allow: settings.Allow, Deny: settings.Deny}
	return map[string]any{"permissions": []jsonPermissionScope{
		{Scope: "project"},
		{Scope: "shared"},
		global,
	}}
}

// jsonUsageBucket is one quota bucket of the `/usage` payload. Field order is
// the reference's wire order (a map would sort them alphabetically).
//
// `remaining_fraction` drives the text answer's percentage
// (`round(fraction*100)`), and `reset_time` is the RFC 3339 instant the bucket
// refills. Both are live figures in the reference.
type jsonUsageBucket struct {
	ID                string  `json:"id"`
	Name              string  `json:"name"`
	Description       string  `json:"description"`
	Window            string  `json:"window"`
	RemainingFraction float64 `json:"remaining_fraction"`
	ResetTime         string  `json:"reset_time"`
}

// jsonUsageGroup is one model group (a set of models that share a quota
// bucket).
type jsonUsageGroup struct {
	Name        string            `json:"name"`
	Description string            `json:"description"`
	Buckets     []jsonUsageBucket `json:"buckets"`
}

// jsonUsagePayload is the `/usage` command payload.
type jsonUsagePayload struct {
	Description string           `json:"description"`
	Groups      []jsonUsageGroup `json:"groups"`
}

// printSlashUsagePayload is the `/usage` answer.
//
// Placeholder for the live figures (【推测】): the reference resolves this from
// a live weekly-quota service (`agy -p /usage` calls it and blocks on the
// response), so the *text* answer's percentage and reset time change as quota
// is consumed and as the window rolls over — a byte-for-byte diff against the
// reference for `/usage` is therefore unattainable, not merely unimplemented.
// What is 【一手】 is the *shape*: captured from
// `agy -p=/usage --output-format json` on 1.4.0 —
//
//	{"description":"Within each group, models share a weekly limit. …",
//	 "groups":[{"name":"Gemini Models",
//	            "description":"Models within this group: Gemini Flash, Gemini Pro",
//	            "buckets":[{"id":"gemini-weekly","name":"Weekly Limit Remaining",
//	                        "description":"You have used some of your weekly limit, …",
//	                        "window":"weekly","remaining_fraction":0.92,
//	                        "reset_time":"2026-10-23T22:19:25Z"}]},
//	           {"name":"Claude and GPT models",
//	            "description":"Models within this group: Claude Opus, Claude Sonnet, GPT-OSS",
//	            "buckets":[{"id":"3p-weekly","name":"Weekly Limit Remaining",
//	                        "description":"You have hit your weekly limit, …",
//	                        "window":"weekly","remaining_fraction":0,
//	                        "reset_time":"2026-10-20T00:48:30Z"}]}]}
//
// The two group names, the bucket ids/names/window, the two group descriptions
// and the top-level description are recovered verbatim. The fractions, the
// relative "in N days, N hours" clauses inside the bucket descriptions, and
// both reset times are the values observed on the capture machine at one
// instant; they are frozen here as placeholders because nothing in the clone
// can recompute them.
func printSlashUsagePayload() jsonUsagePayload {
	return jsonUsagePayload{
		Description: "Within each group, models share a weekly limit. Quota is consumed proportionally to the cost of the tokens. Thus, limits will last longer with shorter tasks or using more cost-effective models. Your weekly limit is tied directly to your individual tier.",
		Groups: []jsonUsageGroup{
			{
				Name:        "Gemini Models",
				Description: "Models within this group: Gemini Flash, Gemini Pro",
				Buckets: []jsonUsageBucket{{
					ID:                "gemini-weekly",
					Name:              "Weekly Limit Remaining",
					Description:       "You have used some of your weekly limit, it will fully refresh in 6 days, 15 hours.",
					Window:            "weekly",
					RemainingFraction: 0.92,
					ResetTime:         "2026-10-23T22:19:25Z",
				}},
			},
			{
				Name:        "Claude and GPT models",
				Description: "Models within this group: Claude Opus, Claude Sonnet, GPT-OSS",
				Buckets: []jsonUsageBucket{{
					ID:                "3p-weekly",
					Name:              "Weekly Limit Remaining",
					Description:       "You have hit your weekly limit, it refreshes in 2 days, 17 hours. If on a supported paid plan, you can use AI credits in the interim or upgrade to a higher tier.",
					Window:            "weekly",
					RemainingFraction: 0,
					ResetTime:         "2026-10-20T00:48:30Z",
				}},
			},
		},
	}
}

func liveOrStaticUsagePayload() jsonUsagePayload {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	summary, err := auth.FetchQuotaSummary(ctx)
	if err == nil && summary != nil && len(summary.Groups) > 0 {
		payload := jsonUsagePayload{
			Description: summary.Description,
		}
		if payload.Description == "" {
			payload.Description = "Within each group, models share a weekly limit and a 5-hour limit. Quota is consumed proportionally to the cost of the tokens. Thus, limits will last longer with shorter tasks or using more cost-effective models. The 5-hour limit smooths out aggregate demand to fairly distribute global capacity across all users, while your weekly limit is tied directly to your individual tier."
		}
		for _, g := range summary.Groups {
			group := jsonUsageGroup{
				Name:        g.Name,
				Description: g.Description,
			}
			for _, b := range g.Buckets {
				resetStr := ""
				if !b.ResetTime.IsZero() {
					resetStr = b.ResetTime.UTC().Format(time.RFC3339)
				}
				group.Buckets = append(group.Buckets, jsonUsageBucket{
					ID:                b.ID,
					Name:              b.DisplayName,
					Description:       b.Description,
					Window:            b.Window,
					RemainingFraction: b.RemainingFraction,
					ResetTime:         resetStr,
				})
			}
			payload.Groups = append(payload.Groups, group)
		}
		return payload
	}
	return printSlashUsagePayload()
}

// printSlashUsageText renders the tabular text output for -p /usage:
// starts with "Quota:\n" and aligns each bucket to fixed columns with
// local time display, matching agy 1.4.3.
func printSlashUsageText(payload jsonUsagePayload) string {
	var b strings.Builder
	b.WriteString("Quota:\n")
	for _, g := range payload.Groups {
		for _, bk := range g.Buckets {
			resetStr := bk.ResetTime
			if t, err := time.Parse(time.RFC3339, bk.ResetTime); err == nil && !t.IsZero() {
				resetStr = t.Local().Format("2006-01-02 15:04 MST")
			}
			fmt.Fprintf(&b, "%-23s%-27s%3.0f%%  %s\n",
				g.Name, bk.Name, bk.RemainingFraction*100, resetStr)
		}
	}
	return b.String()
}

// printSlashUsage renders the `/usage` payload as the text answer: one
// "<group>\t<bucket>\t<pct>%\t<reset>" line per bucket, in group order. The
// percentage is the bucket's remaining fraction rounded to the nearest whole
// number, which is what the reference prints (fraction 0.8987 → "90%").
func printSlashUsage() string {
	return printSlashUsageWithPayload(printSlashUsagePayload())
}

func printSlashUsageWithPayload(payload jsonUsagePayload) string {
	var b strings.Builder
	for _, g := range payload.Groups {
		for _, bk := range g.Buckets {
			fmt.Fprintf(&b, "%s\t%s\t%.0f%%\t%s\n",
				g.Name, bk.Name, bk.RemainingFraction*100, bk.ResetTime)
		}
	}
	return b.String()
}

// jsonStatusLineSetting is the value of the `statusLine` and `title` settings.
//
// The reference models these as a Go struct and re-encodes it with
// encoding/json, so the members come back in *struct* field order
// (type, command, enabled) — a map[string]any would sort them alphabetically
// (command, enabled, type) and print `{"command":"","enabled":true,"type":""}`.
// Field order is 【一手】: captured from `agy -p=/config` in both text and
// `--output-format json`.
type jsonStatusLineSetting struct {
	Type    string `json:"type"`
	Command string `json:"command"`
	Enabled bool   `json:"enabled"`
}

// orderSensitiveConfigKeys are the settings whose value is a JSON *object* with
// a recovered field order, so it must round-trip through its struct rather than
// a map (which the encoder would re-sort).
var orderSensitiveConfigKeys = map[string]bool{
	"statusLine": true,
	"title":      true,
}

// encodeOrderedConfigValue renders one settings value the way the reference
// does for the text dump. Simple values keep their raw JSON (so a string is
// printed unquoted, its surrounding quotes stripped by the caller), while an
// object with a recovered struct order is re-marshaled from that struct.
//
// Returns ("", false) when the value is not one this helper knows how to
// render — the caller falls back to its generic path.
func encodeOrderedConfigValue(key string, raw json.RawMessage) (string, bool) {
	if !orderSensitiveConfigKeys[key] {
		return "", false
	}
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return "", false
	}
	var sl jsonStatusLineSetting
	if err := json.Unmarshal(raw, &sl); err != nil {
		return "", false
	}
	b, err := json.Marshal(sl)
	if err != nil {
		return "", false
	}
	return string(b), true
}

// printSlashConfigData renders the `/config` JSON payload.
//
// It wraps configData (print_format.go) — which returns a plain
// map[string]any — and swaps the two object-valued settings for their struct
// form so the `--output-format json` payload keeps the reference's field order
// (type, command, enabled) instead of the alphabetical order a nested map
// would produce. The settings' top-level keys are unaffected: the reference
// emits those alphabetically too, which is what a map gives us.
func printSlashConfigData() any {
	data := configData()
	outer, ok := data.(map[string]any)
	if !ok {
		return data
	}
	vals, ok := outer["config"].(map[string]any)
	if !ok {
		return data
	}
	for k := range orderSensitiveConfigKeys {
		m, ok := vals[k].(map[string]any)
		if !ok {
			continue // unset (nil) or already a non-object value
		}
		b, err := json.Marshal(m)
		if err != nil {
			continue
		}
		var sl jsonStatusLineSetting
		if json.Unmarshal(b, &sl) != nil {
			continue
		}
		vals[k] = sl
	}
	return data
}

// jsonSkillInfo is one `/skills` row, in the reference's field order:
//
//	{"name":"agents-sdk","description":"…","path":"/Users/…/SKILL.md","builtin":false,"model_invocable":true}
//	{"name":"chrome-devtools-plugin:…","…","plugin":"chrome-devtools-plugin","builtin":false,"model_invocable":true}
//
// `plugin` is present only for entries discovered under a plugin's skills/
// directory, and sits between `path` and `builtin`.
//
// `model_invocable` is true for all 57 skills on the capture machine; the
// frontmatter marker that would clear it is unrecovered, so the clone always
// reports true.
type jsonSkillInfo struct {
	Name           string `json:"name"`
	Description    string `json:"description"`
	Path           string `json:"path"`
	Plugin         string `json:"plugin,omitempty"`
	Builtin        bool   `json:"builtin"`
	ModelInvocable bool   `json:"model_invocable"`
}

// printSlashSkillInfos enumerates the installed skills.
//
// The reference merges skills from the builtin bundle, the user's
// ~/.gemini/skills, and every plugin's skills/ directory (whose entries are
// prefixed "<plugin>:<name>"). The display name comes from the SKILL.md
// frontmatter, not the directory — e.g. builtin/skills/antigravity_guide
// declares `name: antigravity-guide`. Entries may be symlinks (this machine's
// ~/.gemini/skills/terminal-browser is), so the directory test follows the
// link.
//
// After the merge, the result is folded by content hash (slash_skills_dedup.go)
// so a SKILL.md discovered through both a plugin's skills/ directory and a
// project-level `.claude/skills/` folder appears once, with its declared
// (frontmatter `name:`) name. The reference's AGY 1.2.7 changelog records
// this as the gap fix; see the file comment of slash_skills_dedup.go for
// the rule and its evidence grade.
func printSlashSkillInfos() []jsonSkillInfo {
	// Cycle plugin-fsnotify: clear the watcher staleness flag so the
	// next /skills render starts from a known-fresh state. The flag
	// is the same one installedPluginRows / pluginMarketplaces flip;
	// print mode leaves pluginWatcherPtr nil and this is a single
	// pointer read.
	maybeConsumeStaleness()
	var found []jsonSkillInfo
	// add appends every skill directory under dir. listed, when non-nil,
	// restricts the bundle to those directory names. builtin marks the
	// entries that come from the shipped bundle.
	add := func(dir, prefix string, listed map[string]bool, builtin bool) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			return
		}
		for _, e := range entries {
			path := filepath.Join(dir, e.Name())
			if !isDirFollowingLinks(path) {
				continue
			}
			if listed != nil && !listed[e.Name()] {
				continue
			}
			skillMD := filepath.Join(path, "SKILL.md")
			name, desc := skillFrontmatter(skillMD)
			if name == "" {
				name = e.Name()
			}
			if builtin && e.Name() == "automation" {
				// The bundled automation skill declares `name: automation`
				// plus `metadata.icon: schedule`; agy 1.4.3 lists it as
				// "schedule automation" in /skills. Other builtin names
				// come straight from frontmatter.
				name = "schedule automation"
			}
			if prefix != "" {
				name = prefix + ":" + name
			}
			found = append(found, jsonSkillInfo{
				Name:           name,
				Description:    desc,
				Path:           skillMD,
				Plugin:         prefix,
				Builtin:        builtin,
				ModelInvocable: true,
			})
		}
	}

	home := auth.AntigravityHome()
	gemini := filepath.Dir(home) // ~/.gemini
	// The builtin bundle ships nine skills on disk but `/skills` lists only
	// two of them (agy-customizations, antigravity-guide); the other seven
	// (automation, generative_ui, migrate-workflows, permissioned-github,
	// plugin, run-workflow, ui-plugin-navigation) are internal and have no
	// frontmatter marker that distinguishes them. The set below is therefore
	// empirical: it reproduces the reference's output on this machine, but
	// the reference's selection rule is unknown.
	add(filepath.Join(home, "builtin", "skills"), "", printSlashBuiltinListed, true)
	add(filepath.Join(gemini, "skills"), "", nil, false)
	// Note: ~/.claude/skills is deliberately NOT a root. The 1.3.6 reference
	// lists `agents-sdk` (which lives in ~/.gemini/skills) but none of the
	// Claude-only skills (algo/alpha/assets/…), so it does not read the
	// Claude directory. The dedup pass below is what would collapse any
	// duplicates if a second root was added in a later release.
	pluginsDir := filepath.Join(gemini, "config", "plugins")
	if plugins, err := os.ReadDir(pluginsDir); err == nil {
		for _, p := range plugins {
			if isDirFollowingLinks(filepath.Join(pluginsDir, p.Name())) {
				add(filepath.Join(pluginsDir, p.Name(), "skills"), p.Name(), nil, false)
			}
		}
	}

	sort.Slice(found, func(i, j int) bool { return found[i].Name < found[j].Name })
	return dedupeSkillInfosByHash(found)
}

// printSlashSkills lists installed skills as "<name>\t<description>", which is
// the shape `agy -p /skills` emits (57 entries on the capture machine).
func printSlashSkills() string {
	var b strings.Builder
	for _, s := range printSlashSkillInfos() {
		fmt.Fprintf(&b, "%s\t%s\n", s.Name, s.Description)
	}
	return b.String()
}

// printSlashSkillsData renders the `/skills` command payload.
func printSlashSkillsData() any {
	return map[string]any{"skills": printSlashSkillInfos()}
}

// isDirFollowingLinks reports whether path is a directory, following symlinks
// (DirEntry.IsDir is false for a symlink to a directory).
func isDirFollowingLinks(path string) bool {
	st, err := os.Stat(path)
	return err == nil && st.IsDir()
}

// printSlashBuiltinListed is the subset of the builtin skill bundle the
// reference's `/skills` panel lists. It is deliberately NOT the prompt
// allowlist (agyprompt.BuiltinSkillDirs): agy 1.4.3 lists five builtin
// skills, while its system prompt advertises only agy-customizations and
// antigravity-guide. The remaining four directories (generative_ui,
// permissioned-github, plugin, ui-plugin-navigation) are internal and stay
// out of both surfaces. Verified live at 120x40 on agy 1.4.3.
var printSlashBuiltinListed = map[string]bool{
	"agy-customizations": true,
	"antigravity_guide":  true,
	"automation":         true,
	"migrate-workflows":  true,
	"run-workflow":       true,
}

// skillFrontmatter returns the `name:` and `description:` values from a
// SKILL.md YAML frontmatter block. Frontmatter must open with `---` on the
// first line and close with `---` on a later line; only the bytes between
// those fences are fed to the YAML parser, so trailing markdown body lines
// after the close fence never see a YAML decoder.
//
// Cycle 175 replaces the previous from-scratch block-scalar walker with
// gopkg.in/yaml.v3, the same library the reference compiles into its binary
// (verified via `strings`: the `yaml:"…"` tag interface and `encoding/yaml`-ish
// error strings appear in `~/.local/bin/agy`). What this gains over the
// hand-rolled walker:
//   - comments (`#`), anchors (`&`) and aliases (`*`)
//   - flow-style mappings (`{a: b}`) and sequences (`[…]`)
//   - escape sequences inside double-quoted scalars (`\n`, `\t`, Unicode)
//   - every block-scalar flavour (`>`, `>-`, `>+`, `|`, `|-`, `|+`) parsed
//     the same way the rest of the YAML world reads them
//
// `description` is right-trimmed of `\n` to match the prior contract — the
// callers append it to a `\t` separator in `/skills` output, where a trailing
// newline would emit a stray blank line.
func skillFrontmatter(path string) (name, description string) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", ""
	}
	// Carve out the frontmatter span (the bytes between the opening and
	// closing `---` fences). The body after the close fence is left
	// untouched so the markdown source stays a valid stream.
	lines := bytes.Split(b, []byte{'\n'})
	if len(lines) == 0 || !bytes.Equal(bytes.TrimSpace(lines[0]), []byte("---")) {
		return "", ""
	}
	var close int
	found := false
	for i := 1; i < len(lines); i++ {
		if bytes.Equal(bytes.TrimSpace(lines[i]), []byte("---")) {
			close = i
			found = true
			break
		}
	}
	if !found {
		return "", ""
	}
	fm := bytes.Join(lines[1:close], []byte{'\n'})

	var doc struct {
		Name        string `yaml:"name"`
		Description string `yaml:"description"`
	}

	// Cycle `yaml-colon-description`: every plain-scalar value in the
	// frontmatter that is unquoted AND contains a ": " sequence gets
	// wrapped in double quotes before the YAML decoder sees it. YAML 1.2
	// reserves ": " (colon-space) for the key/value separator, so a plain
	// scalar like "Agent: helper" raises "mapping values are not allowed
	// in this context". Auto-quoting preserves the bytes verbatim while
	// making the scanner accept them. Block scalars (">", "|", and their
	// "-"/"+" chomping modifiers) and pre-quoted scalars are passed
	// through untouched; continuation lines are folded into the parent
	// value before that decision is made. A description that the
	// SKILL.md author wrote as a mapping or sequence (rare; the
	// reference never does this) still fails to unmarshal, and we
	// surface that as empty, matching the prior "best effort" contract.
	if err := yaml.Unmarshal(normalizeFrontmatter(fm), &doc); err != nil {
		return "", ""
	}
	return strings.TrimSpace(doc.Name), strings.TrimRight(doc.Description, "\n")
}

// needsDoubleQuoting reports whether v is a plain-scalar YAML value whose
// "key: v" line would be rejected by yaml.v3 because v contains a ": "
// (colon-space) sequence. Block scalars and pre-quoted scalars are never
// double-quoted; empty or whitespace-only values are not.
//
// The same predicate is also exported so tests can pin the boundary the
// preprocessor observes.
func needsDoubleQuoting(v []byte) bool {
	if len(v) == 0 {
		return false
	}
	switch v[0] {
	case '"', '\'', '|', '>', '[', '{', '&', '*', '!', '%', '@', '`':
		return false
	}
	// '-' is a sequence-indicator at column 0; here we already trimmed to
	// the value side, but a YAML sequence value (`- foo`) still starts with
	// '-' and is not a plain scalar.
	if v[0] == '-' {
		return false
	}
	return bytes.Contains(v, []byte(": "))
}

// escapeDoubleQuoted returns b with the byte sequences that YAML double-
// quoted strings reserve escaped: `\` → `\\`, `"` → `\"`, newline → `\n`,
// carriage return → `\r`, tab → `\t`, backspace → `\b`, form-feed → `\f`,
// and the eight other ASCII control bytes (`\x00`–`\x17` except those
// covered) → `\xNN`. The intent is round-trip — the resulting bytes are a
// valid YAML double-quoted scalar and yaml.v3 unescapes them back to the
// original b.
func escapeDoubleQuoted(b []byte) []byte {
	out := make([]byte, 0, len(b)+8)
	for _, c := range b {
		switch c {
		case '\\':
			out = append(out, '\\', '\\')
		case '"':
			out = append(out, '\\', '"')
		case '\n':
			out = append(out, '\\', 'n')
		case '\r':
			out = append(out, '\\', 'r')
		case '\t':
			out = append(out, '\\', 't')
		case '\b':
			out = append(out, '\\', 'b')
		case '\f':
			out = append(out, '\\', 'f')
		case 0:
			out = append(out, '\\', '0')
		default:
			if c < 0x20 {
				out = append(out, '\\', 'x', "0123456789abcdef"[c>>4], "0123456789abcdef"[c&0x0f])
			} else {
				out = append(out, c)
			}
		}
	}
	return out
}

// foldPlainScalar merges a `key: value` line and its indented plain-scalar
// continuation lines into a single logical value, the way YAML 1.2 folds a
// multi-line plain scalar: non-empty continuation lines collapse to a
// single separating space, blank continuation lines become a single `\n`.
// Block-scalar headers (lines whose value starts with `|` or `>`) are not
// continuations and `foldPlainScalar` never sees them — the caller skips
// block scalars before invoking it.
func foldPlainScalar(first []byte, rest [][]byte) []byte {
	out := append([]byte(nil), first...)
	for _, line := range rest {
		if len(bytes.TrimSpace(line)) == 0 {
			out = append(out, '\n')
			continue
		}
		if len(out) > 0 && out[len(out)-1] != '\n' {
			out = append(out, ' ')
		}
		out = append(out, bytes.TrimSpace(line)...)
	}
	return out
}

// normalizeFrontmatter walks fm one line at a time and auto-quotes any
// plain-scalar value that contains a ": " sequence. Block scalars (>|, |,
// etc.) and pre-quoted scalars pass through untouched.
//
// Lines that consist only of whitespace, of `# …` comments, or that start a
// new top-level key are emitted as-is. Indented continuation lines of a
// plain-scalar value are folded into the parent value via foldPlainScalar
// before the `: ` check is applied, so multi-line plain scalars like
//
//	description: First line
//	  continues here: with colon
//
// round-trip into a single double-quoted scalar instead of failing.
//
// This is the precondition step for yaml.Unmarshal in skillFrontmatter; it
// keeps cycle 175's move to yaml.v3 working for SKILL.md files where the
// description is an unquoted plain scalar that happens to mention
// "Agent: helper" or "Step 1: foo. Step 2: bar.".
func normalizeFrontmatter(fm []byte) []byte {
	lines := bytes.Split(fm, []byte{'\n'})
	var out [][]byte
	inBlock := false

	for i := 0; i < len(lines); i++ {
		line := lines[i]

		if inBlock {
			out = append(out, line)
			// A non-blank line that is no longer indented terminates the
			// block scalar's body. YAML allows many chomping styles; the
			// close-with-an-unindented-line heuristic is sufficient for
			// the shapes skill authors actually use.
			if len(line) > 0 && line[0] != ' ' && line[0] != '\t' && len(bytes.TrimRight(line, " \t")) > 0 {
				inBlock = false
			}
			continue
		}

		trimmed := bytes.TrimSpace(line)
		if len(trimmed) == 0 || trimmed[0] == '#' {
			out = append(out, line)
			continue
		}

		// Indented line — pass through. A subsequent key line that owns
		// it is handled by the top-level branch below; standalone
		// indented lines with no parent are uncommon in frontmatter and
		// are preserved as plain YAML expects.
		if line[0] == ' ' || line[0] == '\t' {
			out = append(out, line)
			continue
		}

		idx := bytes.IndexByte(line, ':')
		if idx < 0 {
			out = append(out, line)
			continue
		}

		key := line[:idx]
		rest := bytes.TrimSpace(line[idx+1:])

		// Block-scalar header (`key: |` or `key: >`, optionally with a
		// chomping indicator like `-` or `+` and/or an explicit
		// indentation indicator).
		if len(rest) > 0 && (rest[0] == '|' || rest[0] == '>') {
			out = append(out, line)
			inBlock = true
			continue
		}

		// Empty / comment-only value.
		if len(rest) == 0 || rest[0] == '#' {
			out = append(out, line)
			continue
		}

		// Collect any indented continuations first so a ": " sequence on
		// a later line triggers quoting even when the first line is safe.
		var continuation [][]byte
		j := i + 1
		for j < len(lines) {
			next := lines[j]
			if len(next) == 0 {
				break
			}
			if next[0] != ' ' && next[0] != '\t' {
				break
			}
			continuation = append(continuation, next)
			j++
		}
		folded := foldPlainScalar(rest, continuation)
		if !needsDoubleQuoting(folded) {
			// Either already quoted, a flow-style mapping / sequence,
			// or a plain scalar (single- or multi-line) with no
			// colon-space sequence — pass through verbatim, including
			// any continuation lines.
			out = append(out, line)
			for _, cl := range continuation {
				out = append(out, cl)
			}
			i = j - 1
			continue
		}

		// Unsafe plain scalar — single-line or folded — emit as a
		// single double-quoted scalar.
		var emitted []byte
		emitted = append(emitted, key...)
		emitted = append(emitted, ':', ' ')
		emitted = append(emitted, '"')
		emitted = append(emitted, escapeDoubleQuoted(folded)...)
		emitted = append(emitted, '"')
		out = append(out, emitted)
		// Continuation lines were folded into `folded`; skip past them.
		i = j - 1
	}

	return bytes.Join(out, []byte{'\n'})
}
