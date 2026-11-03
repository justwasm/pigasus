// usage_host.go — the host's side of the /usage panel.
//
// # Where the panel lives
//
// The panel itself is internal/ui/views/usagepanel: it owns the
// document, the window over it, the counter and the legend. The host
// gathers the panel's facts and hands them over as a State.
//
// # What the host keeps
//
// The quota *fetch* is the host's: the answer arrives as a message, the
// host stores it in the panel's quota state and the panel reads it back
// through the State on every draw.

package cliapp

import (
	"github.com/justwasm/pigasus/internal/ui/views/usagepanel"
)

// usageState gathers the /usage panel's facts.
//
// The state is read on every draw, so the countdown ticks and a quota
// answer shows up as soon as it lands.
func (m *runMode) usageState() usagepanel.State {
	return usagepanel.State{
		// The account line's address.
		Email: m.email,
		// The terminal's size: the width wraps the note, the height
		// decides how many document rows one window shows.
		Width:  m.width,
		Height: m.height,
		// The quota call is in flight while the state says so.
		Loading: m.panel.quota.state == quotaLoading,
		// The live answer, when there is one.
		Summary: m.panel.quota.summary,
		// The two buckets' refresh times.
		RefreshG: m.panel.quota.refreshG,
		RefreshO: m.panel.quota.refreshO,
		// The spinner frame the loading row shows.
		Frame: m.quotaFrame,
	}
}
