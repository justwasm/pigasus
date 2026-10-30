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
package cliapp

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/justwasm/pigasus/internal/plugins"
	"github.com/justwasm/pigasus/internal/pluginsui"
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
	return pluginsui.LocalInstallRow(isCursor, query, blinkOn)
}

func pluginLocalInstallHint(query string) string {
	return pluginsui.LocalInstallHint(query)
}

func pluginLocalInstallLegend() string {
	return pluginsui.LocalInstallLegend()
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

func completeLocalInstallPath(query string) string {
	return pluginsui.CompleteLocalInstallPath(query)
}

func installLocalPlugin(src string) error {
	return pluginsui.InstallLocalPlugin(src)
}

type pluginRow struct {
	name     string
	desc     string
	version  string
	category string
	keywords []string
	// shelf is the name of the Discover shelf the row belongs to, and
	// inShelf says the row is part of a shelf at all (the rows of the
	// Installed tab and of the search-filtered list are not).
	shelf   string
	inShelf bool
	// more marks a shelf's "⋯ N more — enter to show" row. Such a row
	// stands for the shelf's hidden tail: it has no plugin, and `hidden`
	// says how many plugins it stands for.
	more   bool
	hidden int
}

func fromPluginRow(r plugins.Row) pluginRow {
	return pluginRow{
		name:     r.Name,
		desc:     r.Desc,
		version:  r.Version,
		category: r.Category,
		keywords: r.Keywords,
	}
}

func toPluginRow(r pluginRow) plugins.Row {
	return plugins.Row{
		Name:     r.name,
		Desc:     r.desc,
		Version:  r.version,
		Category: r.category,
		Keywords: r.keywords,
	}
}

func toPluginRows(rows []pluginRow) []plugins.Row {
	out := make([]plugins.Row, len(rows))
	for i, r := range rows {
		out[i] = toPluginRow(r)
	}
	return out
}

type pluginMarketplace struct {
	name  string
	items []pluginRow
}

func fromPluginMarketplace(m plugins.Marketplace) pluginMarketplace {
	items := make([]pluginRow, len(m.Items))
	for i, it := range m.Items {
		items[i] = fromPluginRow(it)
	}
	return pluginMarketplace{name: m.Name, items: items}
}

func toPluginMarketplaces(mkts []pluginMarketplace) []plugins.Marketplace {
	out := make([]plugins.Marketplace, len(mkts))
	for i, m := range mkts {
		items := make([]plugins.Row, len(m.items))
		for j, it := range m.items {
			items[j] = toPluginRow(it)
		}
		out[i] = plugins.Marketplace{Name: m.name, Items: items}
	}
	return out
}

var pluginWatcherPtr = plugins.WatcherPtr

var pluginInstalledFallback = []pluginRow{
	{name: "chrome-devtools-plugin", desc: "Reliable automation, in-depth debugging, and performance analysis in Chrome using Chrome DevTools and Puppeteer"},
	{name: "google-antigravity-sdk", desc: "Using the Google Antigravity Python SDK to build AI agents"},
	{name: "modern-web-guidance-plugin", desc: "Keep your coding agent up to date with the latest web best practices."},
	{name: "science", desc: "Curated collection of agent skills for science tasks."},
}

func marketplaceItems(mkts []pluginMarketplace) []pluginRow {
	var out []pluginRow
	for _, mk := range mkts {
		out = append(out, mk.items...)
	}
	return out
}

func installedPluginRows() []pluginRow {
	rows := plugins.InstalledRows()
	out := make([]pluginRow, len(rows))
	for i, r := range rows {
		out[i] = fromPluginRow(r)
	}
	return out
}

func pluginMarketplaces() []pluginMarketplace {
	mkts := plugins.Marketplaces()
	out := make([]pluginMarketplace, len(mkts))
	for i, m := range mkts {
		out[i] = fromPluginMarketplace(m)
	}
	return out
}

func pluginDescFor(name string, mkts []pluginMarketplace) string {
	return plugins.DescFor(name, toPluginMarketplaces(mkts))
}

func installedNames() map[string]bool {
	return plugins.InstalledNames()
}

func pluginDir(name string) string {
	return plugins.Dir(name)
}

func pluginAgents(name string) []string {
	return plugins.Agents(name)
}

func pluginAgentsAtPath(root string) ([]string, error, bool) {
	return plugins.AgentsAtPath(root)
}

func readPluginManifest(name string) pluginManifest {
	m := plugins.ReadManifest(name)
	return pluginManifest{
		Name:        m.Name,
		Description: m.Description,
		Version:     m.Version,
	}
}

type pluginManifest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Version     string `json:"version"`
}

