// normalize.go — the start-up normalisation of settings.json.
//
// 【一手】 official-docs/settings.md:123 — "**The file is normalized at
// startup.** When the CLI starts with a valid file, it rewrites the file
// in its own format, which also removes keys that are set to their
// default values." The same page: "**Only non-default values are
// saved.** … the CLI saves only the keys whose values differ from the
// defaults" and "**Unknown keys are kept.** The CLI ignores keys it
// doesn't recognize, such as `$schema` or a setting from a newer version,
// and keeps them when it saves the file."
//
// The file is written in the clone's own format — the same
// `formatSettingsJSON` every other write of this package uses — and with
// the same 0600 mode.
package settings

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"reflect"

	"github.com/tidwall/sjson"

	"github.com/justwasm/pigasus/internal/core/confhome"
)

// keptAtDefault names the keys the normalisation must not drop.
//
// A key may only be dropped when the clone's *absent* case and the
// compiled default mean the same thing. Dropping a key replaces the
// stored value with the absent case, so the two have to agree — for
// every other key of the 36 they do, and the audit behind that claim is
// the defaults table plus the reader of each key (the absent case of
// `altScreenMode` is the alternate screen, of `editorMode` is "not vim",
// of `verbosity` is medium, of `editor` is the same branch as "auto", of
// `statusLine` and `title` is "no block", of `permissions` and
// `trustedWorkspaces` is "no rule", of `showTips` is "on", of
// `enableTerminalSandbox` and `hideActiveItems` and `vimInsertFirst` is
// "off", of `agentMode` and `editor` and `verbosity` is the empty
// string).
//
// Two keys disagree in the clone, and both are kept:
//
//   - `toolPermission` — the reference's default is "request-review",
//     but the clone's absent case is allow-by-default. That divergence
//     is deliberate (the clone's own absent case, recorded in AGENTS.md
//     as the `toolPermission` invariant), and dropping the key would
//     turn a review request into a silent approval.
//   - `colorScheme` — the reference's default is "terminal", but the
//     clone's absent case resolves to the palette's own default scheme
//     ("solarized light"), so dropping the key would repaint the whole
//     session.
//
// 【存疑】 the reference normalises both of them away: its absent case
// is its default for every key, so a file holding the default value is
// rewritten without it. The clone keeps the two keys until their absent
// cases agree with their defaults — that is a behaviour decision, not an
// alignment, and it is the only place the clone's normalisation differs
// from the reference's.
var keptAtDefault = map[string]bool{
	"toolPermission": true,
	"colorScheme":    true,
}

// NormalizeOnStartup rewrites settings.json in the clone's own format and
// drops the keys whose value is the reference's default.
//
// The rules, and why:
//
//   - A missing file is left alone. The CLI has not written one yet, and
//     the start-up path must not create one.
//   - A file that is not a JSON object is left alone, byte for byte. The
//     reference opens a "Settings Error" screen for it; the clone has no
//     such screen, so the file is reported and kept — a settings file that
//     cannot be parsed must never be truncated.
//   - A key the reference does not know is kept, whatever it holds.
//   - A key whose value equals the compiled default is dropped: the
//     reference persists only the values that differ from the defaults,
//     and "setting an option back to its default removes its key".
//   - A key whose *absent* case means something else than its default is
//     kept (`keptAtDefault` above).
//   - A file whose normalised bytes equal the bytes on disk is not
//     written at all. The start-up path must not churn a file that needs
//     nothing.
//
// The error is advisory. The caller is a start-up hook, and a settings
// file that cannot be normalised must not keep the CLI from starting.
func NormalizeOnStartup() error {
	path := Path()
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	current := confhome.StripBOM(raw)
	var obj map[string]json.RawMessage
	if err := json.Unmarshal(current, &obj); err != nil {
		return fmt.Errorf("settings file %s is not a JSON object: %w", path, err)
	}

	data := current
	for key, value := range obj {
		def, known := defaults[key]
		if !known {
			continue
		}
		if keptAtDefault[key] {
			continue
		}
		if !valueIsDefault(value, def) {
			continue
		}
		dropped, err := sjson.DeleteBytes(data, key)
		if err != nil {
			return fmt.Errorf("settings file %s: dropping %q: %w", path, key, err)
		}
		data = dropped
	}

	out := formatSettingsJSON(data)
	if bytes.Equal(out, current) {
		return nil
	}
	if err := os.WriteFile(path, out, 0600); err != nil {
		return err
	}
	// WriteFile applies the mode only when it creates the file; a file
	// that was already there keeps its old mode, and the reference makes
	// the file readable by its owner only.
	return os.Chmod(path, 0600)
}

// valueIsDefault reports whether a settings value is the compiled default
// of its key.
//
// Both sides go through the JSON encoder, so the comparison is on the
// *value* and not on its spelling: a `historySize` of 2000 and a
// `historySize` of 2e3 are the same number, and a `null` is the same
// null the seven nullable keys default to.
func valueIsDefault(raw json.RawMessage, def any) bool {
	var got any
	if err := json.Unmarshal(raw, &got); err != nil {
		return false
	}
	want, err := json.Marshal(def)
	if err != nil {
		return false
	}
	var expected any
	if err := json.Unmarshal(want, &expected); err != nil {
		return false
	}
	return reflect.DeepEqual(got, expected)
}
