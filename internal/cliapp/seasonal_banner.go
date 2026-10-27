package cliapp

import (
	"fmt"
	"io"

	"github.com/justwasm/pigasus/internal/branding"
	"github.com/justwasm/pigasus/internal/seasonal"
)

// seasonalTaglineOpen is the style of the banner's seasonal sentence.
//
// 【一手】 agy 1.4.7's themed banner:
//
//	\x1b[1m\x1b[38;2;255;117;24mSpooky season, steady builds\x1b[0m, naivgaid.
//
// — bold, 24-bit orange, closed by a full reset.
const seasonalTaglineOpen = "\x1b[1m\x1b[38;2;255;117;24m"

// renderSeasonalBanner renders the themed startup banner: one of the
// event's own art scenes, the session's four information rows in the
// column that follows the art, and the seasonal sentence.
//
// 【一手】 agy 1.4.7 with the Halloween event on
// (out/changelog-147/ref/config-seasonal-on.txt):
//
//	<art row 0>Antigravity CLI 1.4.7
//	<art row 1>naivgaid@gmail.com (Google AI Pro)
//	<art row 2>Gemini 3.8 Flash (High)
//	<art row 3>~/Documents/GitHub/openagy
//	<art row 4>
//	<art row 5>
//	<art row 6>
//	  Spooky season, steady builds, naivgaid.
//
// The art block is 46 cells wide and the information column starts at
// column 46 — the art carries the gutter the plain banner's two spaces
// would have made.
//
// 【一手】 The scene is drawn per launch: the reference's themed banner
// is not one picture but five, and every one of them comes with its own
// colour set.
func renderSeasonalBanner(w io.Writer, ev *seasonal.Event, email, model, tier, cwd string) {
	if ev == nil {
		return
	}
	emailLine := email
	if tier != "" {
		emailLine += " (" + tier + ")"
	}
	info := []string{
		cBlueBoldOpen + branding.Current().CLIName + " " + cliVersion + "\x1b[0m",
		cDimGrayOpen + emailLine + cDimGrayClose,
		cDimGrayOpen + model + cDimGrayClose,
		cDimGrayOpen + shortenCwd(cwd) + cDimGrayClose,
	}
	lines := seasonalArt(ev).Lines()
	for i, line := range lines {
		if i < len(info) {
			fmt.Fprintf(w, "%s%s\n", line, info[i])
			continue
		}
		fmt.Fprintln(w, line)
	}
	if name := seasonalName(email); name != "" {
		fmt.Fprintf(w, "  %s%s\x1b[0m, %s.\n", seasonalTaglineOpen, seasonalGreeting(ev), name)
		return
	}
	fmt.Fprintf(w, "  %s%s\x1b[0m\n", seasonalTaglineOpen, seasonalGreeting(ev))
}
