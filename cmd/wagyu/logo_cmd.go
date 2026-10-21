package main

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/justwasm/pigasus/internal/cli"
)

// BannerLogoLines returns the 5 lines of the startup banner logo art as
// they would appear at the left of the banner — all 5 rows of the
// `logoPixels` 10×16 raster matrix, encoded to ANSI bytes.
func BannerLogoLines() []string {
	rows := renderLogoLines()
	return []string{
		rows[0],
		rows[1],
		rows[2],
		rows[3],
		rows[4],
	}
}

// PrintBannerLogo outputs the startup-banner logo art from the matrix
// to w. Used by the hidden `logo` subcommand (`runLogoSubcommand`) and
// anywhere else that needs the art without the surrounding text rows.
func PrintBannerLogo(w io.Writer) {
	for _, line := range BannerLogoLines() {
		fmt.Fprintln(w, line)
	}
}

// runLogoSubcommand implements the hidden `logo` subcommand handler.
//
// It accepts one optional argument:
//
//	--flip=<mode>
//
// where mode is one of:
//
//	none         Pigasus CLI banner logo (default; the 180°-rotated "both" logo)
//	both         same as none (the 180°-rotated Pigasus logo)
//	ref          unrotated reference Google Antigravity logo
//	vert         flipVert(ref)           — true up-down mirror of reference
//	row-rev      reverseRows(ref)        — literal row reversal of reference
//	horiz        flipHoriz(ref)          — horizontal mirror of reference
//	invert       invertColors(ref)       — sub-pixel color swap of reference
//
// Anything else is an error: the prompt/recording surface prefers loud
// failures over silent fallthroughs. The flag is consulted at every
// call so a runtime mutation (e.g. `t.Setenv` / `flag.Set` during
// testing) is observable without re-importing the package.
func runLogoSubcommand(w io.Writer, args []string) error {
	fs := flag.NewFlagSet("logo", flag.ContinueOnError)
	fs.SetOutput(io.Discard) // errors land in the returned err, not w
	var flip string
	fs.StringVar(&flip, "flip", "none", "transform to apply: none|both|ref|vert|row-rev|horiz|invert")
	var style string
	fs.StringVar(&style, "style", "", "style of logo: default|simple (or half|block)")
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			fmt.Fprint(w, cli.UsageLogo("wagyu"))
			return nil
		}
		return err
	}

	st := detectLogoStyle()
	switch strings.ToLower(style) {
	case "simple", "block", "5x6":
		st = logoStyleSimple
	case "default", "half", "halfblock":
		st = logoStyleDefault
	case "":
		// Keep auto-detected or env-detected style.
	default:
		return fmt.Errorf("logo: unknown --style=%q (want default|simple|block|half)", style)
	}

	if st == logoStyleSimple {
		var bg logoBlockGrid
		switch strings.ToLower(flip) {
		case "none", "":
			bg = currentLogoBlockGrid()
		case "both":
			bg = pigasusLogoBlockGrid
		case "ref":
			bg = refLogoBlockGrid
		case "vert":
			bg = flipVertBlocks(refLogoBlockGrid)
		case "horiz":
			bg = flipHorizBlocks(refLogoBlockGrid)
		default:
			return fmt.Errorf("logo: unknown --flip=%q (want none|both|ref|vert|horiz)", flip)
		}
		for _, row := range renderBlockGridLines(bg) {
			fmt.Fprintln(w, row)
		}
		return nil
	}

	// Apply the chosen transform for half-block grid.
	var g logoGrid
	switch strings.ToLower(flip) {
	case "none", "":
		g = currentLogoGrid()
	case "both":
		g = pigasusLogoPixels
	case "ref":
		// The unrotated reference Antigravity logo.
		g = refLogoPixels
	case "vert":
		g = flipVert(refLogoPixels)
	case "row-rev":
		g = reverseRows(refLogoPixels)
	case "horiz":
		g = flipHoriz(refLogoPixels)
	case "invert":
		g = invertColors(refLogoPixels)
	default:
		return fmt.Errorf("logo: unknown --flip=%q (want none|both|ref|vert|row-rev|horiz|invert)", flip)
	}

	for _, row := range renderGridLines(g) {
		fmt.Fprintln(w, row)
	}
	return nil
}
