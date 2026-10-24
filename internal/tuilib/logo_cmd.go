package tuilib

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"

	"github.com/justwasm/pigasus/internal/cli"
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
func RunLogoSubcommand(w io.Writer, args []string) error {
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
