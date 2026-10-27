// Package seasonal implements the v1.4.7 seasonal theme: the opt-in
// Halloween event, its startup banner, its moon-phase spinner and its
// themed prompt-box borders.
//
// 【一手】 agy 1.4.7, driven live on 2026-10-27 (tmux `agyref`, captures
// under out/changelog-147/ref/):
//
//   - /config carries a "Seasonal Themes" row, drawn only while a
//     seasonal event is in season. Its help line is verbatim in
//     RowDescription.
//   - Turning the row on writes `"seasonalThemesOptIn": "halloween-2026"`
//     into ~/.gemini/antigravity-cli/settings.json; turning it off
//     removes the key again (the reference's "delete the default"
//     convention, the same one every other /config row follows).
//   - With the event on, the startup banner swaps its raster art for the
//     event art and gains a seasonal tagline line, the streaming spinner
//     becomes a four-frame moon phase, and the prompt box's two rules
//     become a gradient rule with face motifs in it.
//   - The themed banner is not one picture: the event holds five
//     scenes and the reference draws one of them per launch, next to
//     one of four seasonal sentences. Both draws are independent.
//
// The five scenes were decoded from the reference's own truecolor
// output — a pty with COLORTERM=truecolor, because a 256-colour
// capture loses the picture's shading and reads as a different scene.
//
// 【strings】 agy 1.4.7 binary: `types.CliSetting.SeasonalThemesOn`,
// `model.seasonalThemesField`, `cli/theme/halloween/halloween.init`,
// `seasonal-theme-schedule`, `cli-seasonal-theme-periods`,
// "Spooky season, steady builds", "No bugs tonight, only boos",
// "Happy haunting", "Something is brewing in the build".
//
// 【推测】 The event's season window (1 October – 1 November) is not
// recoverable from the binary: the reference ships a *schedule* and a
// *periods* setting, but their values live in a server-side schedule the
// binary only reads. The clone uses the calendar month of the event,
// which is what "Halloween" means, and regenerates the event id per year
// the way the reference's "halloween-2026" token does.
package seasonal

import (
	"fmt"
	"strings"
	"time"
)

// OptInKey is the settings.json key the reference writes when the user
// opts into a seasonal event. 【一手】 agy 1.4.7 settings.json.
const OptInKey = "seasonalThemesOptIn"

// RowName is the /config row's label. 【一手】 agy 1.4.7 /config.
const RowName = "Seasonal Themes"

// RowDescription is the /config row's help line, verbatim from the
// reference. 【一手】 agy 1.4.7 /config (the row's description pane).
const RowDescription = "Show seasonal touches, such as a themed banner and spinner, while a seasonal event is on. Your choice lasts until the event ends."

// Event is a seasonal event the CLI can be themed with.
type Event struct {
	// ID is the opt-in token written to settings.json, e.g.
	// "halloween-2026". 【一手】 agy 1.4.7 settings.json.
	ID string
	// Taglines are the banner's seasonal sentences. The reference
	// appends ", <email-local-part>." to the one it picks. 【一手】
	// agy 1.4.7 banner: forty launches on 2026-10-27 showed all four
	// sentences, each of them next to any of the banner's scenes.
	Taglines []string
	// Start and End bound the event's season; End is exclusive.
	Start, End time.Time
	// Spinner holds the streaming spinner's frames.
	Spinner []string
	// Arts are the banner art blocks the reference picks from at
	// startup. 【一手】 agy 1.4.7: the themed banner is not one fixed
	// picture — five scenes live in the event and one of them is drawn
	// per launch, each with its own colour set.
	Arts []Art
	// Border describes the themed prompt-box rule.
	Border Border
}

// Art returns the banner art block at pick. The reference draws one of
// the event's scenes per launch and keeps it for the whole session, so
// the caller picks once and holds the index.
//
// 【一手】 agy 1.4.7: launches in a pty (so the captures carry the
// reference's own truecolor output) on 2026-10-27 showed five distinct
// scenes — a pumpkin, a moon, a ghost, a witch's hat and a bat over the
// same spider, web and ground. Twenty-two launches of the settled
// banner drew each of the five exactly once per launch, always the same
// raster; the reference's own first paint of the banner differs from the
// settled one for the hat and the bat.
func (e *Event) Art(pick int) Art {
	if e == nil || len(e.Arts) == 0 {
		return Art{}
	}
	return e.Arts[pick%len(e.Arts)]
}

// Current returns the seasonal event in season at now, or nil when no
// event is running.
func Current(now time.Time) *Event {
	ev := halloween(now.Year())
	if now.Before(ev.Start) || !now.Before(ev.End) {
		return nil
	}
	return ev
}

// Halloween returns the Halloween event of a year, whether or not it is
// in season.
//
// The *themed startup* is gated on the calendar (see Current), but the
// art itself is always there — the command that prints it must work
// outside the season.
func Halloween(year int) *Event {
	return halloween(year)
}

// Enabled reports whether the raw settings.json leaf opts into the
// event in season at now. An absent key (the reference's default) is
// off.
func Enabled(raw string, now time.Time) bool {
	ev := Current(now)
	if ev == nil {
		return false
	}
	return strings.TrimSpace(raw) == ev.ID
}

// halloween builds the Halloween event of a year.
//
// 【推测】 The window is the calendar month of October. The reference's
// own token ("halloween-2026") shows the event id carries the year, so
// the event is regenerated per year rather than being a fixed asset.
func halloween(year int) *Event {
	return &Event{
		ID:       fmt.Sprintf("halloween-%d", year),
		Taglines: halloweenTaglines,
		Start:    time.Date(year, time.October, 1, 0, 0, 0, 0, time.Local),
		End:      time.Date(year, time.November, 1, 0, 0, 0, 0, time.Local),
		Spinner:  halloweenSpinner,
		Arts:     halloweenArts(),
		Border:   halloweenBorder,
	}
}

// Greeting returns the banner's seasonal sentence. pick selects among
// the event's taglines; the reference picks one per launch.
//
// 【一手】 agy 1.4.7: forty launches on 2026-10-27 showed all four
// sentences, each of them next to any of the five banner scenes.
func (e *Event) Greeting(pick int) string {
	if e == nil || len(e.Taglines) == 0 {
		return ""
	}
	return e.Taglines[pick%len(e.Taglines)]
}

// halloweenTaglines are the four sentences the reference's Halloween
// banner shows.
//
// 【一手】 agy 1.4.7 banner: forty launches on 2026-10-27 showed all
// four, next to any of the five banner scenes.
// 【strings】 agy 1.4.7 binary: all four are in the string table.
var halloweenTaglines = []string{
	"Spooky season, steady builds",
	"No bugs tonight, only boos",
	"Happy haunting",
	"Something is brewing in the build",
}

// halloweenArts returns the event's banner scenes.
//
// 【一手】 agy 1.4.7: the themed banner is not one picture. Five scenes
// live in the event — a pumpkin, a moon, a ghost, a witch's hat and a
// bat, each standing on the same spider, web and ground — and the
// reference draws one of them per launch. Every scene carries its own
// colour set.
func halloweenArts() []Art {
	arts := make([]Art, 0, len(halloweenScenes))
	for _, scene := range halloweenScenes {
		arts = append(arts, Art{
			Name:    scene.name,
			Palette: scene.palette,
			Width:   halloweenArtWidth,
			Rows:    scene.rows,
		})
	}
	return arts
}
