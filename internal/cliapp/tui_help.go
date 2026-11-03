package cliapp

import (
	"fmt"
	"io"

	"github.com/justwasm/pigasus/internal/core/branding"
	"github.com/justwasm/pigasus/internal/ui/tuilib"
	"github.com/justwasm/pigasus/internal/ui/views/seasonal"
)

// Cycle 381: the /help overlay's rendering, its tab list and its
// list navigation moved to internal/ui/views/helppanel. What stayed
// here is the startup banner.
//
// The *themed* banner moved to internal/ui/views/seasonal, which owns
// the event, the art and the sentences; the plain banner stayed here,
// because it draws the host's own logo.
//
// The voice dictation moved to internal/ui/views/voice, which owns the
// phase, the trigger, the recorder and the banner; the host keeps the
// wiring in voice_host.go.

// renderBanner renders the startup banner.
//
// 【一手】 agy 1.4.7: with a seasonal event switched on the banner is
// the event's own art block and gains the seasonal sentence; otherwise
// it is the plain logo.
func renderBanner(w io.Writer, email, model, tier, cwd string) {
	renderBannerWithTheme(w, seasonalTheme(), email, model, tier, cwd)
}

// renderBannerWithTheme renders the banner, themed when ev is not nil.
func renderBannerWithTheme(w io.Writer, ev *seasonal.Event, email, model, tier, cwd string) {
	if ev != nil {
		seasonal.RenderBanner(w, ev, seasonal.State{
			Email:   email,
			Tier:    tier,
			Model:   model,
			Cwd:     tuilib.ShortenCwd(cwd),
			CLIName: branding.Current().CLIName,
			Version: cliVersion,
		})
		return
	}
	artRows := tuilib.RenderLogoLines()
	fmt.Fprintf(w, "%s  "+cBlueBoldOpen+"%s "+cliVersion+"\x1b[0m\n", artRows[0], branding.Current().CLIName)
	emailLine := email
	if tier != "" {
		emailLine += " (" + tier + ")"
	}
	fmt.Fprintf(w, logoLines[1]+"\n", artRows[1], emailLine)
	fmt.Fprintf(w, logoLines[2]+"\n", artRows[2], model)
	fmt.Fprintf(w, logoLines[3]+"\n", artRows[3], tuilib.ShortenCwd(cwd))
	if !tuilib.EnvHideLogoOn() {
		fmt.Fprintln(w, artRows[4])
	} else {
		fmt.Fprintln(w)
	}
}
