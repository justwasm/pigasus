package seasonal

import (
	"fmt"
	"io"
	"math/rand/v2"
	"strings"

	"github.com/justwasm/pigasus/palette"
)

// taglineOpen is the style of the banner's seasonal sentence.
//
// 【一手】 agy 1.4.7's themed banner:
//
//	\x1b[1m\x1b[38;2;255;117;24mSpooky season, steady builds\x1b[0m, naivgaid.
//
// — bold, 24-bit orange, closed by a full reset.
const taglineOpen = "\x1b[1m\x1b[38;2;255;117;24m"

// State carries the session facts the themed startup banner prints.
//
// The banner draws what it is given: the caller owns the facts (the
// account, the model, the product's name) and the shortening of the
// workspace path.
type State struct {
	// Email is the signed-in account's address.
	Email string
	// Tier is the account's tier, e.g. "Google AI Pro". An empty tier
	// prints no tier.
	Tier string
	// Model is the model's label, as the session shows it.
	Model string
	// Cwd is the workspace path, ALREADY SHORTENED: the home
	// directory reads as "~". The banner does not shorten it — the
	// caller does, because the shortening lives with the code that
	// knows the home directory.
	Cwd string
	// CLIName is the product's name, e.g. "Pigasus CLI".
	CLIName string
	// Version is the version the banner prints next to the name.
	Version string
}

// RenderBanner renders the themed startup banner: one of the event's
// own art scenes, the session's four information rows in the column
// that follows the art, and the seasonal sentence.
//
// 【一手】 agy 1.4.7 with the Halloween event on
// (out/changelog-147/ref/config-seasonal-on.txt):
//
//	<art row 0>Antigravity CLI 1.4.7
//	<art row 1>user@example.com (Google AI Pro)
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
func RenderBanner(w io.Writer, ev *Event, st State) {
	if ev == nil {
		return
	}
	emailLine := st.Email
	if st.Tier != "" {
		emailLine += " (" + st.Tier + ")"
	}
	info := []string{
		palette.BlueBoldOpen + st.CLIName + " " + st.Version + "\x1b[0m",
		palette.DimGrayOpen + emailLine + palette.DimGrayClose,
		palette.DimGrayOpen + st.Model + palette.DimGrayClose,
		palette.DimGrayOpen + st.Cwd + palette.DimGrayClose,
	}
	lines := pickArt(ev).Lines()
	for i, line := range lines {
		if i < len(info) {
			fmt.Fprintf(w, "%s%s\n", line, info[i])
			continue
		}
		fmt.Fprintln(w, line)
	}
	if name := bannerName(st.Email); name != "" {
		fmt.Fprintf(w, "  %s%s\x1b[0m, %s.\n", taglineOpen, pickGreeting(ev), name)
		return
	}
	fmt.Fprintf(w, "  %s%s\x1b[0m\n", taglineOpen, pickGreeting(ev))
}

// greetingIndex is the banner sentence's index. The reference picks one
// of its sentences per launch — forty launches on 2026-10-27 showed all
// four — so the clone picks one per process and keeps it for the whole
// session.
var greetingIndex = -1

// artIndex is the banner scene's index. The themed banner is not one
// picture: the event holds five scenes and the reference draws one of
// them per launch.
//
// 【一手】 agy 1.4.7: launches in a pty on 2026-10-27 showed five
// distinct scenes — a pumpkin, a moon, a ghost, a witch's hat and a bat
// — every one of them next to any of the four seasonal sentences.
var artIndex = -1

// SetBannerPicks pins the banner's per-process scene and sentence picks
// and returns the picks it replaced, so a caller can put them back.
//
// The picks are otherwise chosen once per process and kept for the
// whole session; this is the seam that renders a known banner.
func SetBannerPicks(art, greeting int) (oldArt, oldGreeting int) {
	oldArt, oldGreeting = artIndex, greetingIndex
	artIndex, greetingIndex = art, greeting
	return oldArt, oldGreeting
}

// pickArt returns the banner's seasonal scene.
func pickArt(ev *Event) Art {
	if ev == nil || len(ev.Arts) == 0 {
		return Art{}
	}
	if artIndex < 0 {
		artIndex = rand.IntN(len(ev.Arts))
	}
	return ev.Art(artIndex)
}

// pickGreeting returns the banner's seasonal sentence.
func pickGreeting(ev *Event) string {
	if ev == nil || len(ev.Taglines) == 0 {
		return ""
	}
	if greetingIndex < 0 {
		greetingIndex = rand.IntN(len(ev.Taglines))
	}
	return ev.Greeting(greetingIndex)
}

// bannerName is the name the banner's seasonal sentence is addressed
// to.
//
// 【一手】 agy 1.4.7's banner reads "Spooky season, steady builds,
// naivgaid." for the account user@example.com — the local part of the
// session's email.
// 【推测】 The reference may be greeting the account's display name; the
// banner shows the email and nothing else, so the local part is the
// only handle the clone has.
func bannerName(email string) string {
	local := email
	if i := strings.Index(local, "@"); i >= 0 {
		local = local[:i]
	}
	return local
}
