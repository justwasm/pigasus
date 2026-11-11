// Package keybindings renders the /keybindings report.
//
// The report is a read-only view of the user's keybindings.json: it
// names the configured actions and the keys bound to them, and it never
// writes the file — so it can never re-bind the exit action.
//
// # The report's vocabulary
//
// Every string the report prints is owned by somebody else:
//
//   - The header is the slash-command catalogue's description of the
//     /keybindings command ("Set custom keybindings"), so the string has
//     exactly one owner — internal/ui/slashcommands.
//   - The exit action's label is the catalogue's description of the
//     /exit command ("Exit the CLI"), for the same reason.
//   - Every other action's label is the reference's own /help SHORTCUTS
//     label — the right-hand column of the "Keyboard Shortcuts" tab,
//     captured in out/changelog-147/ref/help-shortcuts.plain.txt
//     【一手】. The action each label belongs to comes from the
//     reference's "Default keybindings" tables
//     (official-docs/cli/reference.md) 【一手】.
//
// # The file's format
//
// The reference's keybindings.json is a flat JSON object mapping an
// action name to the list of keys bound to it
// (official-docs/settings.md, "Keybindings configuration") 【一手】:
//
//	{"cli.clear_screen": ["ctrl+l"], "prompt.insert_newline": ["shift+enter", "ctrl+j"]}
//
// An action mapped to an empty list is disabled and is not reported. An
// entry the file cannot express is skipped, so a malformed entry falls
// back to the default for that one action — the file itself is never
// the reason the report fails.
//
// # 存疑 — the report's shape
//
// The two strings the reference prints for the command are recoverable
// ("No keybindings found." and "Set custom keybindings" 【strings】), but
// no capture of the reference's rendered report is: the surviving pruned
// index of the 1.4.11 dump (out/local-macos-1.4.11/strings-index.txt)
// carries the strings, not the rows around them. The rows below are
// therefore the clone's own shape, modelled on the reference's /help
// SHORTCUTS tab — a key column and a label column 【推测】.
package keybindings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/ui/slashcommands"
)

// noKeybindingsFound is the reference's own answer for a file that holds
// no bindings.
//
// 【strings】 the 1.4.11 binary carries the string verbatim; the
// surviving copy of the dump is out/local-macos-1.4.11/strings-index.txt.
const noKeybindingsFound = "No keybindings found."

// actionLabels maps an action to the label the report prints for it.
//
// The labels are the reference's own /help SHORTCUTS labels 【一手】;
// the action each one belongs to is the reference's own "Default
// keybindings" table (official-docs/cli/reference.md) 【一手】.
//
// An action the table does not hold is reported under its own name.
var actionLabels = map[string]string{
	"cli.clear_screen":          "Clear CLI screen",
	"cli.cycle_mode":            "Cycle mode",
	"cli.enter":                 "Send message or confirm",
	"cli.escape":                "Go back / dismiss",
	"cli.suspend":               "Suspend CLI",
	"cli.test":                  "Test CLI action",
	"confirm.no":                "Reject",
	"confirm.yes":               "Confirm",
	"edit.open_editor":          "Open prompt in $EDITOR",
	"edit.paste":                "Paste image/video",
	"edit.redo":                 "Redo",
	"edit.undo":                 "Undo",
	"edit.yank":                 "Yank (paste from kill ring)",
	"item.delete":               "Delete",
	"item.rename":               "Rename",
	"live.mute":                 "Mute Live Mode",
	"live.stop":                 "Stop Live Mode",
	"live.toggle":               "Start or Mute Live Mode",
	"navigation.down":           "Move down",
	"navigation.go_to_bottom":   "Go to bottom",
	"navigation.go_to_top":      "Go to top",
	"navigation.half_page_down": "Half page down",
	"navigation.half_page_up":   "Half page up",
	"navigation.left":           "Move left",
	"navigation.page_down":      "Page down",
	"navigation.page_up":        "Page up",
	"navigation.right":          "Move right",
	"navigation.tab":            "Tab",
	"navigation.up":             "Move up",
	"prompt.insert_newline":     "Insert newline",
	"prompt.submit_alternate":   "Send with opposite Queued Messages setting",
	"subagent.approve_fast":     "Approve subagent fast",
	"subagent.jump_to_waiting":  "Manage subagent",
	"swarm.board":               "Swarm board",
	"swarm.dismiss":             "Dismiss swarm",
	"view.review_artifact":      "Review artifact",
	"view.toggle_trajectory":    "Toggle trajectory view",
	"vim.insert.insert_newline": "Insert newline (Vim insert)",
	"vim.insert.submit":         "Submit prompt (Vim insert)",
	"vim.normal.submit":         "Submit prompt (Vim normal)",
	"voice.start_dictation":     "Start voice dictation",
}

// action is one configured action and the keys bound to it.
type action struct {
	name string
	keys []string
}

// Report renders the /keybindings report for the file at path.
//
// The data is the file's content; a nil or an empty data is an absent
// or an empty file, and the report is then the reference's own "No
// keybindings found.".
//
// The report never writes: it is a read-only view of the file.
//
// The rows are tab-separated cells — the header, then one row per
// configured action (the keys, then the action's label). The ui tier's
// own row layout (internal/ui/rows.TabAlignedRows) aligns the columns.
func Report(path string, data []byte) (string, error) {
	actions, err := parse(data)
	if err != nil {
		return "", fmt.Errorf("error loading keybindings from %s: %w (delete %s and run /keybindings to reset)", path, err, path)
	}
	if len(actions) == 0 {
		return noKeybindingsFound, nil
	}
	rows := make([]string, 0, len(actions)+1)
	rows = append(rows, reportHeader())
	for _, a := range actions {
		rows = append(rows, "  "+strings.Join(a.keys, ", ")+"\t"+displayName(a.name))
	}
	return strings.Join(rows, "\n"), nil
}

// reportHeader is the command's own description — the catalogue's row
// for /keybindings.
//
// The fallback is the command's name: the catalogue always holds the
// row, so the fallback is unreachable, and a second copy of the
// description would be a second owner of the string.
func reportHeader() string {
	if d, ok := slashcommands.DescriptionOf("/keybindings"); ok {
		return d
	}
	return "/keybindings"
}

// displayName is the label the report prints for an action.
//
// The exit action's label is the catalogue's description of the /exit
// command, so the string has one owner; every other action's label is
// the reference's own /help SHORTCUTS label.
func displayName(name string) string {
	if name == "cli.exit" {
		if d, ok := slashcommands.DescriptionOf("/exit"); ok {
			return d
		}
	}
	if d, ok := actionLabels[name]; ok {
		return d
	}
	return name
}

// parse decodes the file's actions, in the order the file lists them.
//
// An empty file holds no actions; a malformed file is an error.
func parse(data []byte) ([]action, error) {
	trimmed := bytes.TrimSpace(data)
	if len(trimmed) == 0 {
		return nil, nil
	}
	dec := json.NewDecoder(bytes.NewReader(trimmed))
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	if delim, ok := tok.(json.Delim); !ok || delim != '{' {
		return nil, fmt.Errorf("expected a JSON object")
	}
	var out []action
	for dec.More() {
		tok, err := dec.Token()
		if err != nil {
			return nil, err
		}
		name, ok := tok.(string)
		if !ok {
			return nil, fmt.Errorf("expected a binding name")
		}
		var raw json.RawMessage
		if err := dec.Decode(&raw); err != nil {
			return nil, err
		}
		keys, err := decodeKeys(raw)
		if err != nil {
			// A malformed entry falls back to the default for that
			// one action: the entry is skipped, the rest of the file
			// is still reported.
			continue
		}
		if len(keys) == 0 {
			// An empty list disables the action — there is no binding
			// to report.
			continue
		}
		out = append(out, action{name: name, keys: keys})
	}
	return out, nil
}

// decodeKeys reads one action's keys.
//
// The reference's format is a list of key names; a bare string is read
// as a one-key list.
func decodeKeys(raw json.RawMessage) ([]string, error) {
	trimmed := bytes.TrimSpace(raw)
	if len(trimmed) == 0 {
		return nil, fmt.Errorf("empty value")
	}
	var list []string
	if trimmed[0] == '[' {
		if err := json.Unmarshal(trimmed, &list); err != nil {
			return nil, err
		}
	} else {
		var one string
		if err := json.Unmarshal(trimmed, &one); err != nil {
			return nil, err
		}
		list = []string{one}
	}
	keys := make([]string, 0, len(list))
	for _, k := range list {
		k = canonicalKey(k)
		if k == "" {
			continue
		}
		keys = append(keys, k)
	}
	return keys, nil
}

// canonicalKey normalises a key name: trimmed, lower-cased and without
// the spaces around the "+" separators.
//
// 【推测】 the reference canonicalises the key names it reads
// (cli/keybindings.CanonicalizeKeybinding), but its rules are not
// recoverable; this is the normalisation the clone's own file needs.
func canonicalKey(key string) string {
	parts := strings.Split(strings.ToLower(strings.TrimSpace(key)), "+")
	for i, p := range parts {
		parts[i] = strings.TrimSpace(p)
	}
	return strings.Join(parts, "+")
}
