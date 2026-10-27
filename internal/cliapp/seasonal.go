package cliapp

import (
	"math/rand/v2"
	"strings"
	"time"

	sp "charm.land/bubbles/v2/spinner"

	"github.com/justwasm/pigasus/internal/auth"
	"github.com/justwasm/pigasus/internal/configpanel"
	"github.com/justwasm/pigasus/internal/seasonal"
)

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

// seasonalEvent returns the seasonal event in season right now, or nil
// when no event is running.
//
// 【一手】 agy 1.4.7 draws the "Seasonal Themes" row in /config only
// while the theme is in season (the 1.4.7 changelog says the same:
// "a row shown only while the theme is in season").
func seasonalEvent() *seasonal.Event {
	return seasonal.Current(seasonalNow())
}

// seasonalTheme returns the event the session is themed with, or nil
// when no event is in season or the user has not opted in.
//
// 【一手】 agy 1.4.7: the opt-in lives in settings.json as
// `seasonalThemesOptIn` and carries the event's own id
// ("halloween-2026"); an absent key means off.
func seasonalTheme() *seasonal.Event {
	ev := seasonalEvent()
	if ev == nil {
		return nil
	}
	raw, err := auth.SettingsField(seasonal.OptInKey)
	if err != nil {
		return nil
	}
	if !seasonal.Enabled(configpanel.RawString(raw), seasonalNow()) {
		return nil
	}
	return ev
}

// seasonalGreetingIndex is the banner sentence's index. The reference
// picks one of its sentences per launch — forty launches on 2026-10-27
// showed all four — so the clone picks one per process and keeps it for
// the whole session.
var seasonalGreetingIndex = -1

// seasonalArtIndex is the banner scene's index. The themed banner is
// not one picture: the event holds five scenes and the reference draws
// one of them per launch.
//
// 【一手】 agy 1.4.7: launches in a pty on 2026-10-27 showed five
// distinct scenes — a pumpkin, a moon, a ghost, a witch's hat and a bat
// — every one of them next to any of the four seasonal sentences.
var seasonalArtIndex = -1

// seasonalArt returns the banner's seasonal scene.
func seasonalArt(ev *seasonal.Event) seasonal.Art {
	if ev == nil || len(ev.Arts) == 0 {
		return seasonal.Art{}
	}
	if seasonalArtIndex < 0 {
		seasonalArtIndex = rand.IntN(len(ev.Arts))
	}
	return ev.Art(seasonalArtIndex)
}

// seasonalGreeting returns the banner's seasonal sentence.
func seasonalGreeting(ev *seasonal.Event) string {
	if ev == nil || len(ev.Taglines) == 0 {
		return ""
	}
	if seasonalGreetingIndex < 0 {
		seasonalGreetingIndex = rand.IntN(len(ev.Taglines))
	}
	return ev.Greeting(seasonalGreetingIndex)
}

// seasonalName is the name the banner's seasonal sentence is addressed
// to.
//
// 【一手】 agy 1.4.7's banner reads "Spooky season, steady builds,
// naivgaid." for the account naivgaid@gmail.com — the local part of the
// session's email.
// 【推测】 The reference may be greeting the account's display name; the
// banner shows the email and nothing else, so the local part is the
// only handle the clone has.
func seasonalName(email string) string {
	local := email
	if i := strings.Index(local, "@"); i >= 0 {
		local = local[:i]
	}
	return local
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
