// panel_install.go — the /plugin panel's inline local-install prompt and
// the blinking cursor.
//
// The prompt is the third row of the Discover tab: the local row's text
// is reused as the prompt label, the typed path is the tail, and the
// reverse-video block cursor blinks on the trailing space.
//
// The accept path copies the directory into the configuration home's
// plugin directory and writes the matching "enabled" entry into
// config.json. The reference performs the same copy+config-write.
//
// Tab while the prompt has focus does a one-level completion against the
// filesystem: if the typed path is a directory, it is left as-is; if the
// path is a unique prefix of one directory, it is expanded to that
// directory; if no match exists, the typed text is left unchanged and the
// "Directory not found" hint stays.

package pluginsui

import (
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
)

// localInstall is the inline path prompt the Discover tab's
// "+ Install from local directory..." row opens.
//
// It is only valid while the Discover tab is shown and the search box is
// not focused (the search box and the prompt cannot be open
// simultaneously).
type localInstall struct {
	active bool   // is the prompt open?
	query  string // the typed path (no leading or trailing whitespace)
}

// BlinkPeriod is the on/off cadence of the search / install cursor in the
// reference.
//
// Probed at 220x50 (sampling the box line every ~10 ms for 10 s): the
// block is present for ~530 ms and absent for ~530 ms, so the period is
// one second. The bubbles / textarea default is 530 ms, which the clone
// reuses.
const BlinkPeriod = 530

// BlinkMsg toggles the /plugin cursor's blink phase.
type BlinkMsg struct{}

// Blink arms the next blink toggle.
func Blink() tea.Cmd {
	return tea.Tick(BlinkPeriod, func(time.Time) tea.Msg { return BlinkMsg{} })
}

// localActive reports whether the prompt is open and owns every keypress.
func (p *Panel) localActive() bool {
	return p != nil && p.tab == TabDiscover && !p.search && p.local.active
}

// cmdForFocus arms the cursor blink only when the key was handled while
// the box that owns the blink was focused (the Discover search box, the
// local-install prompt).
//
// The check is on the *current* panel state, so the first key that opens
// the prompt returns the blink even when the focus state has already
// flipped; subsequent keys while focused also arm it; keys handled in
// the unfocused tab do not.
func (p *Panel) cmdForFocus(handled bool) tea.Cmd {
	if !handled {
		return nil
	}
	if p.localActive() || (p.tab == TabDiscover && p.search) {
		return Blink()
	}
	return nil
}

// openLocalInstall opens the prompt on the local-install row.
//
// It parks the cursor at the local row (the prompt owns the only keypress
// while active) and resets any prior query.
func (p *Panel) openLocalInstall() {
	p.local.active = true
	p.local.query = ""
	p.cursor = 0
	p.blinkOn = true
}

// closeLocalInstall closes the prompt and restores the cursor to the
// local-install row.
//
// The prompt's typed path is dropped — Esc and the post-install accept
// share this path.
func (p *Panel) closeLocalInstall() {
	p.local = localInstall{}
	p.cursor = 0
}

// handleLocalKey owns every key while the prompt is open.
//
// Printable keys append to the query, backspace drops the last rune, tab
// expands a unique subdirectory prefix, enter and ctrl+s submit, and esc
// closes the prompt.
func (p *Panel) handleLocalKey(key tea.Key) bool {
	switch key.Code {
	case tea.KeyEsc:
		p.closeLocalInstall()
		return true
	case tea.KeyBackspace:
		r := []rune(p.local.query)
		if len(r) > 0 {
			p.local.query = string(r[:len(r)-1])
		}
		return true
	case tea.KeyTab:
		p.local.query = CompleteLocalInstallPath(p.local.query)
		return true
	case tea.KeyEnter:
		p.submitLocalInstall()
		return true
	case tea.KeySpace:
		p.local.query += " "
		return true
	}
	if key.Code == 's' && key.Mod.Contains(tea.ModCtrl) {
		p.submitLocalInstall()
		return true
	}
	if key.Mod == 0 && key.Text != "" {
		p.local.query += key.Text
		return true
	}
	return false
}

// submitLocalInstall validates the typed path and, if it points at a
// directory that contains a parseable plugin.json, copies the source
// directory into the configuration home's plugin directory and writes the
// plugin's "enabled" entry into config.json.
//
// Any failure leaves the prompt open and parks the cursor on the local
// row; the panel's title shows the error in red.
func (p *Panel) submitLocalInstall() {
	q := strings.TrimSpace(p.local.query)
	if q == "" {
		p.localError = "directory required"
		return
	}
	if err := InstallLocalPlugin(q); err != nil {
		p.localError = err.Error()
		return
	}
	p.localError = ""
	p.closeLocalInstall()
}
