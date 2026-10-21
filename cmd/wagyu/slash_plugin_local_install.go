// slash_plugin_local_install.go — cycle 212: the Discover tab's inline
// path prompt and the leftover search-box edits.
//
// Everything here was measured by driving Antigravity CLI 1.4.0 (the live
// reference) at 220x50 with tmux and diffing the panel cells. The cycle
// closes three Discover gaps left by cycles 198/201/204:
//
//   - Enter / ctrl+s on the "+ Install from local directory..." row opens
//     an inline path prompt that lives in row 3 — the local row's text is
//     reused as the prompt label, with `~/path/to/plugin` as the empty
//     placeholder and a reverse-video block cursor on the trailing space
//     the search box uses;
//   - Tab on the focused Discover search box cycles Installed↔Discover
//     while *keeping* the box focused and the typed query intact (the
//     previous cycle left the box on exit — see cycle 204's
//     TestCycle204_TabFromSearchSwitchesToInstalled);
//   - the cursor in either box blinks at ~530 ms (the bubbles default),
//     so a single blink phase drives both call sites.
//
// The accept path of the prompt — Enter on a directory that contains a
// parseable plugin.json — copies the directory into
// ~/.gemini/config/plugins/<manifest-name>/ and writes the matching
// plugins.<name>.enabled=true entry into config.json. The reference
// performs the same copy+config-write, so the clone matches.
//
// Tab while the prompt has focus does a one-level completion against the
// filesystem: if the typed path is a directory, it is left as-is; if the
// path is a unique prefix of one directory, it is expanded to that
// directory; if no match exists, the typed text is left unchanged and the
// "Directory not found" hint stays. Path completion is the single piece
// not measurable beyond what is shown — the reference cycles a list of
// subdirectories, which the cycle-212 probe did not capture end to end —
// so the implementation is the simplest one that passes the visible
// cases.
//
// See docs/cycles/cycle-212/deliverable-plugin-leftovers.md.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/auth"
)

// pluginLocalInstall is the inline path prompt the Discover tab's
// "+ Install from local directory..." row opens. Only valid while
// m.panel.kind == panelPlugin, m.panel.pluginTab == panelDiscover, and
// m.panel.pluginSearch == false (the search box and the prompt cannot be
// open simultaneously).
type pluginLocalInstall struct {
	active bool   // is the prompt open?
	query  string // the typed path (no leading or trailing whitespace)
}

// pluginBlinkPeriod is the on/off cadence of the search / install cursor
// in the reference. Probed at 220x50 (sampling the box line every ~10 ms
// for 10 s): the block is present for ~530 ms and absent for ~530 ms,
// so the period is one second. The bubbles / textarea default is 530 ms,
// which the clone reuses.
const pluginBlinkPeriod = 530

// pluginBlinkMsg toggles the /plugin cursor's blink phase. It mirrors
// resumeBlinkMsg (cycle 184) but lives here because the cycle-212
// search box edits ship together with the prompt.
type pluginBlinkMsg struct{}

// pluginBlink arms the next blink toggle.
func pluginBlink() tea.Cmd {
	return tea.Tick(pluginBlinkPeriod, func(time.Time) tea.Msg { return pluginBlinkMsg{} })
}

// pluginCmdForFocus arms the cursor blink only when the key was handled
// while the box that owns the blink was focused (search box on Discover,
// local-install prompt). The check is on the *current* panel state, so
// the first key that opens the prompt returns pluginBlink() even when
// the focus state has already flipped; subsequent keys while focused
// also arm it; keys handled in the unfocused tab do not.
func pluginCmdForFocus(handled bool, m *runMode) tea.Cmd {
	if !handled {
		return nil
	}
	if m.pluginLocalInstallActive() || (m.panel.kind == panelPlugin &&
		m.panel.pluginTab == panelDiscover && m.panel.pluginSearch) {
		return pluginBlink()
	}
	return nil
}

// pluginCursorSpan is the reverse-video block the reference draws at the
// end of the focused /plugin box. The clone emits it on every blink
// "on" tick; the "off" tick drops the entire span, leaving the plain
// trailing space the box always reserves.
const pluginCursorSpan = "\x1b[7m\x1b[37m \x1b[0m"

// pluginLocalPlaceholder is the grey text the prompt shows while the
// query is empty. It is plain (grey 241), not a placeholder attribute —
// the reference writes it as a literal string. The probe recovered it
// byte-for-byte from p1.out row 3.
const pluginLocalPlaceholder = " ~/path/to/plugin"

// pluginLocalInstallActive reports whether the prompt is open and owned
// every keypress.
func (m *runMode) pluginLocalInstallActive() bool {
	return m.panel.kind == panelPlugin && m.panel.pluginTab == panelDiscover &&
		!m.panel.pluginSearch && m.panel.pluginLocalInstall.active
}

// openPluginLocalInstall opens the prompt on the local-install row. It
// parks the cursor at the local row (the prompt owns the only keypress
// while active) and resets any prior query.
func (m *runMode) openPluginLocalInstall() {
	m.panel.pluginLocalInstall.active = true
	m.panel.pluginLocalInstall.query = ""
	m.panel.cursor = 0
	m.panel.pluginBlinkOn = true
}

// closePluginLocalInstall closes the prompt and restores the cursor to
// the local-install row. The prompt's typed path is dropped (Esc and
// the post-install accept share this path).
func (m *runMode) closePluginLocalInstall() {
	m.panel.pluginLocalInstall = pluginLocalInstall{}
	m.panel.cursor = 0
}

// pluginLocalInstallRow is the third /plugin line while the prompt is
// open. Shape recovered from p1.out:
//
//	> + Install from local directory:   ~/path/to/plugin
//	> + Install from local directory: /tmp/...xyz    <cursor>
//
// When the query is empty the placeholder (" ~/path/to/plugin") is the
// tail; otherwise the typed path is the tail. The reverse-video block
// follows the text only on a blink "on" tick — the "off" tick drops it.
func pluginLocalInstallRow(isCursor bool, query string, blinkOn bool) string {
	label := "\x1b[1m+ Install from local directory:\x1b[0m"
	prefix := "  " + label + " "
	if isCursor {
		prefix = cBlueOpen + "> " + label + cBlueClose + " "
	}
	if query == "" {
		return prefix + pluginCursorSpan + "\x1b[38;2;101;123;131m" + pluginLocalPlaceholder + "\x1b[39m"
	}
	if blinkOn {
		return prefix + query + pluginCursorSpan
	}
	return prefix + query + " "
}

// pluginLocalInstallHint is the validation line the reference draws
// below the prompt. It is "" when no hint is wanted (empty query, or
// the path resolves to a valid plugin). Cycle 212 measures three
// strings:
//
//   - "Directory not found (press Tab to complete)"
//   - "Directory does not contain plugin.json (press Tab to complete subdirs)"
//   - "" (no hint, valid plugin dir OR empty query)
//
// The hint line lives one row below the prompt and shifts the rest of
// the panel down by one when it appears.
func pluginLocalInstallHint(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return ""
	}
	info, err := os.Stat(q)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "  \x1b[38;2;238;232;213m└─ \x1b[38;2;101;123;131m✗ Directory not found (press Tab to complete)\x1b[39m"
		}
		return "  \x1b[38;2;238;232;213m└─ \x1b[38;2;101;123;131m✗ Directory not readable\x1b[39m"
	}
	if !info.IsDir() {
		return "  \x1b[38;2;238;232;213m└─ \x1b[38;2;101;123;131m✗ Path is not a directory\x1b[39m"
	}
	if _, err := os.Stat(filepath.Join(q, "plugin.json")); err != nil {
		return "  \x1b[38;2;238;232;213m└─ \x1b[38;2;101;123;131m✗ Directory does not contain plugin.json (press Tab to complete subdirs)\x1b[39m"
	}
	return ""
}

// pluginLocalInstallLegend is the footer the prompt uses, in place of
// the Discover tab's row-0 (local-install) legend. Shape recovered from
// p2.out row 22:
//
//	Keyboard: enter / ctrl+s Install Plugin  tab Complete Path  esc Cancel
func pluginLocalInstallLegend() string {
	return panelKeyboardHint("enter / ctrl+s", "Install Plugin",
		"tab", "Complete Path", "esc", "Cancel")
}

// handlePluginLocalInstallKey owns every key while the prompt is open.
// It returns true when it consumed the key (the caller swallows the
// key) and false on the (currently unreachable) path.
//
// Measured (probes p4/p7/p8): printable keys append to the query,
// backspace drops the last rune, tab expands a unique subdirectory
// prefix, enter/ctrl+s submit, and esc closes the prompt.
func (m *runMode) handlePluginLocalInstallKey(key tea.Key) bool {
	switch key.Code {
	case tea.KeyEsc:
		m.closePluginLocalInstall()
		return true
	case tea.KeyBackspace:
		r := []rune(m.panel.pluginLocalInstall.query)
		if len(r) > 0 {
			m.panel.pluginLocalInstall.query = string(r[:len(r)-1])
		}
		return true
	case tea.KeyTab:
		m.panel.pluginLocalInstall.query = completeLocalInstallPath(m.panel.pluginLocalInstall.query)
		return true
	case tea.KeyEnter:
		m.submitPluginLocalInstall()
		return true
	case tea.KeySpace:
		m.panel.pluginLocalInstall.query += " "
		return true
	}
	if key.Code == 's' && key.Mod.Contains(tea.ModCtrl) {
		m.submitPluginLocalInstall()
		return true
	}
	if key.Mod == 0 && key.Text != "" {
		m.panel.pluginLocalInstall.query += key.Text
		return true
	}
	return false
}

// submitPluginLocalInstall validates the typed path and, if it points at
// a directory that contains a parseable plugin.json, copies the source
// directory into ~/.gemini/config/plugins/<manifest-name>/ and writes
// plugins.<name>.enabled=true into ~/.gemini/config/config.json. Any
// failure leaves the prompt open and parks the cursor on the local row;
// the panel's title shows the error in red.
//
// Cycle 212 keeps the prompt open on failure because the reference
// does (p3.out row 13: "Plugins (Error: directory ... does not contain a
// valid plugin)" while the prompt itself is still drawn).
func (m *runMode) submitPluginLocalInstall() {
	q := strings.TrimSpace(m.panel.pluginLocalInstall.query)
	if q == "" {
		m.panel.pluginLocalError = "directory required"
		return
	}
	if err := installLocalPlugin(q); err != nil {
		m.panel.pluginLocalError = err.Error()
		return
	}
	m.panel.pluginLocalError = ""
	m.closePluginLocalInstall()
}

// pluginLocalInstallCursorCount reports how many rows the cursor can
// occupy while the prompt is open. It is always 1 — the local-install
// row — but lives here so the discover row-count helper stays readable.
func pluginLocalInstallCursorCount() int { return 1 }

// completeLocalInstallPath expands the typed path against the
// filesystem. Probed at 220x50 against the live agy 1.4.0 reference
// (cycle 220): the reference always expands to the alphabetically-first
// sibling whose name has the typed leaf as a prefix, then appends a
// trailing slash so the cursor is positioned for the *next* level (the
// hint row switches from "press Tab to complete" to "press Tab to
// complete subdirs" once the resolved path points at an existing
// directory that does not contain a plugin.json).
//
// Subsequent Tabs are no-ops — the reference does NOT cycle through
// candidates. Cycle 212's "reference cycles a list of subdirectories"
// hypothesis is retracted; the cycle-212 probe never measured the
// behaviour end to end. See
// docs/cycles/cycle-220/deliverable-plugin-multi-subdir.md.
//
// Behaviour summary:
//
//   - ""                                            → "" (no change)
//   - q already ends in "/"                        → q (no change)
//   - parent readable, ≥1 sibling matches leaf     → first match (alpha) + "/"
//   - q resolves to a directory (no trailing /)    → q + "/"
//   - parent unreadable and q is not a directory   → q (no change)
func completeLocalInstallPath(query string) string {
	q := strings.TrimSpace(query)
	if q == "" {
		return q
	}
	if strings.HasSuffix(q, "/") {
		// The reference leaves an already-resolved path alone once it
		// carries the trailing "/" — pressing Tab again is a no-op so
		// the cursor does not acquire a second "/".
		return q
	}
	parent, leaf := filepath.Split(q)
	if parent == "" {
		parent = "."
	}
	entries, err := os.ReadDir(parent)
	if err != nil {
		// Parent unreadable: if the typed path itself points at a real
		// directory, the reference still appends "/" so the cursor is
		// positioned for the next level. Otherwise leave the path alone.
		if info, err := os.Stat(q); err == nil && info.IsDir() {
			return q + "/"
		}
		return q
	}
	var matches []string
	for _, e := range entries {
		name := e.Name()
		if strings.HasPrefix(name, leaf) {
			matches = append(matches, name)
		}
	}
	if len(matches) == 0 {
		// No matching siblings under parent. If q is itself a directory,
		// append a trailing slash (matches the reference). Otherwise
		// leave the path alone — the hint row will keep showing
		// "Directory not found".
		if info, err := os.Stat(q); err == nil && info.IsDir() {
			return q + "/"
		}
		return q
	}
	sort.Strings(matches)
	return filepath.Join(parent, matches[0]) + "/"
}

// installLocalPlugin copies src into ~/.gemini/config/plugins/<name>/
// and writes plugins.<name>.enabled=true into config.json. The name
// comes from src/plugin.json's "name" field; if it is missing or empty,
// the directory basename is used.
func installLocalPlugin(src string) error {
	info, err := os.Stat(src)
	if err != nil {
		return fmt.Errorf("directory %q does not contain a valid plugin", src)
	}
	if !info.IsDir() {
		return fmt.Errorf("directory %q does not contain a valid plugin", src)
	}
	raw, err := os.ReadFile(filepath.Join(src, "plugin.json"))
	if err != nil {
		return fmt.Errorf("directory %q does not contain a valid plugin", src)
	}
	var man struct {
		Name string `json:"name"`
	}
	if err := json.Unmarshal(raw, &man); err != nil {
		return fmt.Errorf("directory %q does not contain a valid plugin", src)
	}
	if strings.TrimSpace(man.Name) == "" {
		man.Name = filepath.Base(src)
	}
	dst := filepath.Join(auth.GeminiConfigHome(), "plugins", man.Name)
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("plugin %q is already installed", man.Name)
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(dst), 0700); err != nil {
		return err
	}
	if err := copyDir(src, dst); err != nil {
		return err
	}
	return auth.SetPluginEnabled(man.Name, true)
}

// copyDir recursively copies src to dst (which must not yet exist). It
// is a stdlib-only directory copy: read each entry, mkdir or copy,
// recurse. Permissions are preserved where the copy preserves them by
// default (Go's CopyFS inherits the source mode).
func copyDir(src, dst string) error {
	if err := os.CopyFS(dst, os.DirFS(src)); err != nil {
		return err
	}
	return nil
}
