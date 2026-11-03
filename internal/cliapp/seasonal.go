package cliapp

import (
	"time"

	sp "charm.land/bubbles/v2/spinner"

	"github.com/justwasm/pigasus/internal/ui/views/configpanel"
	settings "github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
)

// Cycle 381: the themed startup banner — its art scene, its four
// information rows and its seasonal sentence — moved to
// internal/ui/views/seasonal, the package that already owned the event,
// the art and the sentences. What stayed here is the theme's own state:
// the clock, the opt-in, the spinner and the refresh.

// seasonalNow is the clock the seasonal theme reads. The tests pin it so
// the /config row does not appear (or disappear) with the calendar.
var seasonalNow = time.Now

// seasonalSpinnerFPS is the themed spinner's frame rate.
//
// 【推测】 The reference's moon phase turns at the same rate as its
// plain spinner: the live capture in out/changelog-147/ref/turn/ shows
// the frame advancing by one step per capture, which is what a 10 fps
// spinner sampled every half second does. The binary does not carry
// the rate in a readable form.
const seasonalSpinnerFPS = time.Second / 10

// seasonalTheme returns the event the session is themed with, or nil
// when no event is in season or the user has not opted in.
//
// 【一手】 agy 1.4.7: the opt-in lives in settings.json as
// `seasonalThemesOptIn` and carries the event's own id
// ("halloween-2026"); an absent key means off.
func seasonalTheme() *seasonal.Event {
	ev := seasonal.Current(seasonalNow())
	if ev == nil {
		return nil
	}
	raw, err := settings.Field(seasonal.OptInKey)
	if err != nil {
		return nil
	}
	if !seasonal.Enabled(configpanel.RawString(raw), seasonalNow()) {
		return nil
	}
	return ev
}

// refreshSeasonalTheme re-reads the seasonal opt-in and swaps the
// streaming spinner for the themed one.
//
// 【一手】 agy 1.4.7: the theme follows the setting, and the spinner
// turns into the event's own moon phase while it is on.
func (m *runMode) refreshSeasonalTheme() {
	m.seasonal = seasonalTheme()
	frames := m.seasonal.SpinnerFrames()
	if len(frames) == 0 {
		m.spinner = sp.New(sp.WithSpinner(sp.Dot))
		return
	}
	m.spinner = sp.New(sp.WithSpinner(sp.Spinner{
		Frames: frames,
		FPS:    seasonalSpinnerFPS,
	}))
}
