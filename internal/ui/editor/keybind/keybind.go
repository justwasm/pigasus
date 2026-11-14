// Package keybind parses the user's keybindings.json and translates
// raw terminal key events into the alt+letter combinations the
// rest of the TUI dispatcher expects.
//
// Cycle alt-keybinding-option-meta: the AGY 1.3.3 changelog says
// "Fixed custom `alt` keybindings such as `alt+s` not working with
// the macOS Option key in terminals that don't treat Option as
// Meta, such as Terminal.app and VS Code-based terminals."
//
// Terminal.app and VS Code's integrated terminal do not forward
// the Option key as Meta unless the user enables "Use Option as
// Meta key" in Preferences ▸ Profiles ▸ Keyboard. With the option
// off, Option+s sends the special Unicode character 'ß' rather
// than ESC + 's'. Modern terminals (iTerm2, kitty, WezTerm,
// Alacritty, ghostty) already forward Option as Meta, so the
// bubbletea event arrives as a single KeyPressMsg with Mod = ModAlt
// and Code = 's'.
//
// This package's only job is to detect the broken terminals and
// hand the TUI the pieces it needs to bridge them. It does not
// dispatch the bindings itself — the host TUI keeps that seam.
package keybind

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	tea "charm.land/bubbletea/v2"
)

// EscReleaseWindow is the maximum delay between a bare Esc and
// the letter that the buffer treats as alt+<letter>. 50ms matches
// ultraviolet's own EscTimeout, so the worst-case behaviour is the
// same as if we had not buffered anything.
const EscReleaseWindow = 50 * time.Millisecond

// TermNeedsOptionMeta reports whether the running terminal does
// NOT forward the macOS Option key as Meta natively. Terminal.app
// and VS Code's integrated terminal fall in this group when the
// user has not enabled "Use Option as Meta key" — they send the
// special Unicode character (e.g. 'ß' for Option+s) on press.
//
// iTerm2, kitty, WezTerm, Alacritty and the rest of the modern
// terminal-emulator pack already forward Option as Meta and let
// ultraviolet decode ESC+<letter> into ModAlt. They are not on
// this list.
//
// Detection sniffs $TERM_PROGRAM — the conventional Apple-defined
// marker — rather than $TERM (which only carries a capability
// class, not the application). $LC_TERMINAL is the second best
// signal but is far less standardised; we keep the gate narrow so
// we don't second-guess terminals that already do the right thing.
func TermNeedsOptionMeta() bool {
	switch os.Getenv("TERM_PROGRAM") {
	case "Apple_Terminal", "vscode":
		return true
	}
	return false
}

// DefaultPath returns the canonical keybindings.json location the
// reference binary uses: ~/.gemini/config/keybindings.json.
//
// 【反编译】 agy 1.4.8: `(*ServerBackend).KeybindingsFilePath`
// (0x1026c9d80) hands back the path the backend holds at +0x160/+0x170,
// and the two literals it is assembled from are "~/.gemini/config"
// (0x1034d870f) and "keybindings.json" (0x1034c8c4f) — so the path is
// unchanged.
//
// The path was first recovered from the v1.3.4 decompilation
// (re-docs/decompile-google3_third_party_jetski_cli_backend_backend..md,
// KeybindingsFilePath @ 0x1023c8ee0). That address is a v1.3.4 one —
// every address moves on every release, and at 1.4.8 that address holds
// pion/dtls/v3/dtls.flight5Generate.
//
// Tests override via Load.
func DefaultPath() string {
	home, _ := os.UserHomeDir()
	cfg := filepath.Join(home, ".gemini", "config", "keybindings.json")
	if _, err := os.Stat(cfg); err == nil {
		return cfg
	}
	return filepath.Join(home, ".gemini", "antigravity-cli", "keybindings.json")
}

// Keybindings is a parsed keybindings.json. The file is a JSON
// object: keys are binding names ("alt+s", "ctrl+x"), values are
// action names the host dispatches ("submit", "cancel"), OR keys are action names
// mapping to a list of key combinations (official-docs/cli/using.md:68 schema).
//
// Bindings with empty values or unknown key names are skipped at
// load time so the lookup path can be a straight map read. The
// raw map survives unmodified for the host so it can iterate.
type Keybindings struct {
	path       string
	raw        map[string]string
	actionKeys map[string][]string
}

// ActionKeys returns the mapping of action names to their bound key sequences.
func (k *Keybindings) ActionKeys() map[string][]string {
	if k == nil {
		return nil
	}
	return k.actionKeys
}

// KeysForAction returns the key strings bound to the given action.
func (k *Keybindings) KeysForAction(action string) []string {
	if k == nil {
		return nil
	}
	return k.actionKeys[action]
}

// KeyString formats a tea.Key into a canonical binding key string like "ctrl+l", "ctrl+g", "alt+m", "shift+up", "enter".
func KeyString(key tea.Key) string {
	var parts []string
	if key.Mod.Contains(tea.ModCtrl) {
		parts = append(parts, "ctrl")
	}
	if key.Mod.Contains(tea.ModAlt) {
		parts = append(parts, "alt")
	}
	if key.Mod.Contains(tea.ModShift) {
		parts = append(parts, "shift")
	}

	var codeStr string
	switch key.Code {
	case tea.KeyEnter:
		codeStr = "enter"
	case tea.KeyEsc:
		codeStr = "esc"
	case tea.KeyBackspace:
		codeStr = "backspace"
	case tea.KeyTab:
		codeStr = "tab"
	case tea.KeyUp:
		codeStr = "up"
	case tea.KeyDown:
		codeStr = "down"
	case tea.KeyLeft:
		codeStr = "left"
	case tea.KeyRight:
		codeStr = "right"
	case tea.KeyPgUp:
		codeStr = "pgup"
	case tea.KeyPgDown:
		codeStr = "pgdown"
	case tea.KeyHome:
		codeStr = "home"
	case tea.KeyEnd:
		codeStr = "end"
	case tea.KeyDelete:
		codeStr = "delete"
	case tea.KeySpace:
		codeStr = "space"
	case tea.KeyF1:
		codeStr = "f1"
	case tea.KeyF2:
		codeStr = "f2"
	case tea.KeyF3:
		codeStr = "f3"
	case tea.KeyF4:
		codeStr = "f4"
	case tea.KeyF5:
		codeStr = "f5"
	case tea.KeyF6:
		codeStr = "f6"
	case tea.KeyF7:
		codeStr = "f7"
	case tea.KeyF8:
		codeStr = "f8"
	case tea.KeyF9:
		codeStr = "f9"
	case tea.KeyF10:
		codeStr = "f10"
	case tea.KeyF11:
		codeStr = "f11"
	case tea.KeyF12:
		codeStr = "f12"
	default:
		if key.Code >= 'a' && key.Code <= 'z' {
			codeStr = string(key.Code)
		} else if key.Code >= 'A' && key.Code <= 'Z' {
			codeStr = strings.ToLower(string(key.Code))
		} else if key.Code == ' ' {
			codeStr = "space"
		} else if key.Code > 0 && unicode.IsPrint(key.Code) {
			codeStr = strings.ToLower(string(key.Code))
		} else if key.Text != "" {
			codeStr = strings.ToLower(key.Text)
		}
	}

	if codeStr == "" {
		return ""
	}
	parts = append(parts, codeStr)
	return strings.Join(parts, "+")
}

// ActionForKey resolves the action mapped to the tea.Key event.
func (k *Keybindings) ActionForKey(key tea.Key) string {
	if k == nil {
		return ""
	}
	ks := KeyString(key)
	if ks == "" {
		return ""
	}
	return k.ActionForExactKey(ks)
}

func normalizeKeyString(k string) string {
	k = strings.ToLower(strings.TrimSpace(k))
	parts := strings.Split(k, "+")
	for i, p := range parts {
		switch p {
		case "pageup", "page_up":
			parts[i] = "pgup"
		case "pagedown", "page_down":
			parts[i] = "pgdown"
		case "escape":
			parts[i] = "esc"
		}
	}
	return strings.Join(parts, "+")
}

// ActionForExactKey returns the action name mapped to the given key combination (e.g. "ctrl+d").
// Non-vim actions take precedence over vim-scoped actions so bindings like "enter" in
// vim.insert.insert_newline do not shadow cli.enter in global/composer modes.
func (k *Keybindings) ActionForExactKey(key string) string {
	if k == nil {
		return ""
	}
	normKey := normalizeKeyString(key)
	// 1. Prioritize non-vim actions
	for act, keys := range k.actionKeys {
		if strings.HasPrefix(act, "vim.") {
			continue
		}
		for _, candidate := range keys {
			if normalizeKeyString(candidate) == normKey {
				return act
			}
		}
	}
	// 2. Check vim actions
	for act, keys := range k.actionKeys {
		if !strings.HasPrefix(act, "vim.") {
			continue
		}
		for _, candidate := range keys {
			if normalizeKeyString(candidate) == normKey {
				return act
			}
		}
	}
	if act, ok := k.raw[normKey]; ok {
		return act
	}
	return k.raw[key]
}

// ActionForExactKeyInScope returns the action name mapped to the key within a specific scope prefix (e.g. "vim.insert.").
func (k *Keybindings) ActionForExactKeyInScope(key string, scopePrefix string) string {
	if k == nil {
		return ""
	}
	normKey := normalizeKeyString(key)
	for act, keys := range k.actionKeys {
		if scopePrefix != "" && !strings.HasPrefix(act, scopePrefix) {
			continue
		}
		for _, candidate := range keys {
			if normalizeKeyString(candidate) == normKey {
				return act
			}
		}
	}
	return ""
}

// ActionForKeyInScope resolves the action mapped to tea.Key within a specific scope prefix.
func (k *Keybindings) ActionForKeyInScope(key tea.Key, scopePrefix string) string {
	if k == nil {
		return ""
	}
	ks := KeyString(key)
	if ks == "" {
		return ""
	}
	return k.ActionForExactKeyInScope(ks, scopePrefix)
}

// Path returns the file these bindings were loaded from.
func (k *Keybindings) Path() string { return k.path }

// Has reports whether at least one binding is configured. Used by
// the TUI translation gate: if the dispatcher has nothing to fire,
// we skip the ESC buffering entirely.
func (k *Keybindings) Has(letter rune) bool {
	if k == nil {
		return false
	}
	_, ok := k.LookupAltLetter(letter)
	return ok
}

// RawBindings returns the parsed bindings map. The TUI's
// translation gate uses this so an empty dispatcher short-
// circuits the ESC buffer entirely (see
// cmd/wagyu/alt_keytranslate.go consumeAltEsc).
func (k *Keybindings) RawBindings() map[string]string {
	if k == nil {
		return nil
	}
	return k.raw
}

// Load reads path; a missing file returns an empty Keybindings
// without error so the call site can stay simple. An empty object
// ("{}") is also empty bindings — the user is expected to populate
// it via /keybindings.
//
// Invalid bindings (wrong shape, empty action, unknown key name)
// are skipped, not fatal. /keybindings validation runs separately
// in the reference binary; we keep this loader permissive so a
// half-edited file does not brick the TUI.
func Load(path string) (*Keybindings, error) {
	out := &Keybindings{
		path:       path,
		raw:        map[string]string{},
		actionKeys: map[string][]string{},
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return out, nil
		}
		return out, fmt.Errorf("read keybindings: %w", err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return out, nil
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return out, fmt.Errorf("parse keybindings: %w", err)
	}
	for k, v := range raw {
		// Schema 1: "alt+s": "action_name"
		if s, ok := v.(string); ok {
			if strings.TrimSpace(s) == "" {
				continue
			}
			out.raw[k] = s
			out.actionKeys[s] = append(out.actionKeys[s], k)
			continue
		}
		// Schema 2: "action_name": ["alt+s", "ctrl+x"]
		if list, ok := v.([]any); ok {
			var keys []string
			for _, item := range list {
				if str, ok := item.(string); ok && strings.TrimSpace(str) != "" {
					keys = append(keys, str)
					out.raw[str] = k
				}
			}
			out.actionKeys[k] = keys
		}
	}
	return out, nil
}

// ParseBinding splits "alt+s" → (ModAlt, 's', true). It accepts
// alt/ctrl/shift modifiers and a single lowercase ASCII letter;
// any other shape (multi-rune, function-key, digit, punctuation,
// unknown modifier) returns ok=false so the loader can skip it.
//
// The cycle's scope is alt+<letter>; ctrl/shift bindings are
// accepted by Parse so the loader does not reject them, but the
// translation in cmd/wagyu/alt_keytranslate.go only acts on
// alt+letter today.
func ParseBinding(name string) (tea.KeyMod, rune, bool) {
	parts := strings.Split(name, "+")
	if len(parts) < 2 {
		return 0, 0, false
	}
	var mod tea.KeyMod
	for _, p := range parts[:len(parts)-1] {
		switch strings.ToLower(p) {
		case "alt":
			mod |= tea.ModAlt
		case "ctrl":
			mod |= tea.ModCtrl
		case "shift":
			mod |= tea.ModShift
		default:
			return 0, 0, false
		}
	}
	tail := parts[len(parts)-1]
	runes := []rune(tail)
	if len(runes) != 1 {
		return 0, 0, false
	}
	r := runes[0]
	if r < 'a' || r > 'z' {
		return 0, 0, false
	}
	return mod, r, true
}

// LookupAltLetter returns the action name bound to alt+<letter>.
// ok is false when no binding exists, in which case the caller is
// expected to fall through to its default handling.
func (k *Keybindings) LookupAltLetter(letter rune) (string, bool) {
	if k == nil {
		return "", false
	}
	name := fmt.Sprintf("alt+%c", letter)
	action, ok := k.raw[name]
	return action, ok
}

// IsAltLetter reports whether key is a plain alt+<letter> event —
// the shape the rest of the dispatcher expects after translation. The
// translation gate in the TUI uses this so it doesn't synthesise
// alt+letter from a Ctrl+Alt+s or Shift+Alt+s that already arrived
// with a different modifier set.
func IsAltLetter(key tea.Key) (rune, bool) {
	if !key.Mod.Contains(tea.ModAlt) {
		return 0, false
	}
	// ModAlt alone (no Ctrl, no Shift) — the synthesised shape.
	if key.Mod.Contains(tea.ModCtrl) || key.Mod.Contains(tea.ModShift) {
		return 0, false
	}
	r := key.Code
	if (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') {
		return 0, false
	}
	return unicode.ToLower(r), true
}

// EscTimeoutMsg fires after EscReleaseWindow when a buffered Esc
// has not been promoted to alt+letter. The handler runs the
// original Esc branch so the user sees their keypress take effect
// after the brief hold.
type EscTimeoutMsg struct{}

// EscBuffer holds one bare Esc key while the host waits for a
// possible alt+<letter> follow-up. Only used on terminals where
// the macOS Option key is not forwarded as Meta. The state lives
// on runMode; the Feed method below drives the decision.
type EscBuffer struct {
	Pending bool
	At      time.Time
	// Delivered is true when the buffered Esc was already dispatched to
	// the app — vim mode does that, so Normal mode starts on the key press
	// itself — and only the alt-pair fold is still waiting. Releasing such
	// a buffer must not dispatch a second Esc.
	Delivered bool
}

// Feed walks one KeyMsg through the buffer.
//
//	key            the incoming key
//	hasBinding     does the dispatcher have an alt+<letter> binding?
//	onSynth        callback when an alt+<letter> is synthesised
//	onRelease      callback when the buffered Esc is finally released
//	now            current time (injected so tests can drive it)
//
// Returns true when the input key was fully consumed by the
// buffer — the caller should not dispatch it. Returns false when
// the input key must be dispatched normally (and onRelease has
// already been called so any buffered Esc has been honoured).
func (b *EscBuffer) Feed(
	key tea.Key,
	hasBinding func(letter rune) bool,
	onSynth func(letter rune),
	onRelease func(),
	now time.Time,
) bool {
	if !b.Pending {
		// No buffered Esc — handle this key as-is unless it is itself
		// a bare Esc that should start buffering.
		return false
	}
	// Window elapsed without a follow-up: release the stale Esc.
	if now.Sub(b.At) > EscReleaseWindow {
		b.Pending = false
		if !b.Delivered {
			onRelease()
		}
		b.Delivered = false
		return false
	}
	// The Esc already reached the app (vim dispatches it on the press, so
	// Normal mode starts at once). Folding the next letter would swallow a
	// vim command — `v` would become alt+v instead of Visual mode — so the
	// pair cannot form: the buffer is dropped and the key is dispatched.
	if b.Delivered {
		b.Pending = false
		b.Delivered = false
		return false
	}
	r := key.Code
	// Only a *bare* letter continues the pair: a modified key (ctrl+s on
	// its way to submit, say) is its own command, and folding it into
	// alt+<letter> would swallow the press. Terminal.app's Option+<letter>
	// arrives as Esc then a bare letter, which is the case this exists for.
	if key.Mod == 0 && isAltLetterCandidate(r) {
		letter := unicode.ToLower(r)
		if hasBinding(letter) {
			b.Pending = false
			b.Delivered = false
			onSynth(letter)
			return true
		}
	}
	// Not synthesised — release Esc first (when it was held back), then
	// dispatch the current key.
	b.Pending = false
	if !b.Delivered {
		onRelease()
	}
	b.Delivered = false
	return false
}

// Start records that a bare Esc has arrived and should be buffered
// for the next key. Returns true when the Esc was consumed (the
// caller should not dispatch it).
func (b *EscBuffer) Start(now time.Time) bool {
	if b.Pending {
		return false
	}
	b.Pending = true
	b.At = now
	b.Delivered = false
	return true
}

// Clear drops any pending Esc without firing onRelease. Used by
// tests that want to reset the buffer between cases.
func (b *EscBuffer) Clear() { b.Pending = false }

// isAltLetterCandidate reports whether r is a letter that can be
// the trailing half of an alt+letter sequence on macOS Option
// without Meta forwarding. ASCII a–z / A–Z only; digits and
// punctuation are intentionally excluded so the buffer does not
// swallow legitimate Esc+digit combos.
func isAltLetterCandidate(r rune) bool {
	return (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z')
}
