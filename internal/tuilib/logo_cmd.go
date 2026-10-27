package tuilib

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/cli"
	"github.com/justwasm/pigasus/internal/seasonal"
)

func parseSubcommandStyle(style string) (LogoStyle, error) {
	st := DetectLogoStyle()
	switch strings.ToLower(style) {
	case "simple", "block", "5x6":
		return LogoStyleSimple, nil
	case "default", "half", "halfblock":
		return LogoStyleDefault, nil
	case "":
		return st, nil
	default:
		return 0, fmt.Errorf("logo: unknown --style=%q (want default|simple|block|half)", style)
	}
}

// RunLogoSubcommand implements the hidden `logo` subcommand handler.
//
// The plain logo is the default; `--seasonal` and `--scene` print the
// seasonal banner art instead — the five scenes the reference's
// Halloween banner draws at startup.
func RunLogoSubcommand(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("logo", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // errors land in the returned err, not w
	var flip string
	fs.StringVar(&flip, "flip", "none", "transform to apply: none|both|ref|vert|row-rev|horiz|invert")
	var style string
	fs.StringVar(&style, "style", "", "style of logo: default|simple (or half|block)")
	var seasonalArt bool
	fs.BoolVar(&seasonalArt, "seasonal", false, "print a seasonal banner scene")
	var scene string
	fs.StringVar(&scene, "scene", "", "seasonal scene to print")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(w, cli.UsageLogo("wagyu"))
			return nil
		}
		return err
	}

	if seasonalArt || scene != "" {
		return printSeasonalScene(w, scene)
	}

	st, err := parseSubcommandStyle(style)
	if err != nil {
		return err
	}

	lines, err := RenderTransformed(flip, st)
	if err != nil {
		return err
	}
	for _, row := range lines {
		fmt.Fprintln(w, row)
	}
	return nil
}

// printSeasonalScene prints the seasonal banner art.
//
// An empty name means the reference's own behaviour: one scene, picked
// at random, the way the reference draws it at startup. "all" prints
// every scene.
//
// 【一手】 agy 1.4.7 draws one of five scenes per launch, each with its
// own colours; the rasters are the reference's own truecolor output
// (out/changelog-147/ref/seasonal-scenes/).
func printSeasonalScene(w io.Writer, name string) error {
	ev := seasonal.Halloween(time.Now().Year())
	var names []string
	switch {
	case name == "" || strings.EqualFold(name, "random"):
		names = []string{ev.ArtNames()[rand.IntN(len(ev.Arts))]}
	case strings.EqualFold(name, "all"):
		names = nil // every scene
	default:
		names = []string{name}
	}
	if err := ev.PrintScenes(w, names); err != nil {
		return fmt.Errorf("logo: %v", err)
	}
	return nil
}
