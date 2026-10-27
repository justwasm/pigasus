package cli

import (
	"fmt"
	"math/rand/v2"
	"strings"
	"time"

	"github.com/justwasm/pigasus/internal/seasonal"
)

// usageHalloween returns the usage block for the halloween subcommand.
//
// The command is the clone's own — the reference has no such
// subcommand — but the art it prints is the reference's own, decoded
// from its truecolor captures (out/changelog-147/ref/seasonal-scenes/).
func usageHalloween(n string) string {
	return "Usage: " + n + " halloween [scene...]\n" +
		"\n" +
		"Print the seasonal banner art\n" +
		"\n" +
		"The reference's Halloween banner is not one picture: it draws one of\n" +
		"five scenes per launch — a pumpkin, a moon, a ghost, a witch's hat and\n" +
		"a bat — each with its own colours. Without an argument every scene is\n" +
		"printed; with one, that scene is printed.\n" +
		"\n" +
		"Scenes:\n" +
		"  all       print every scene\n" +
		"  random    print one scene, the way the reference picks it at startup\n" +
		"  pumpkin   the pumpkin scene\n" +
		"  moon      the moon scene\n" +
		"  ghost     the ghost scene\n" +
		"  witch-hat the witch's hat scene\n" +
		"  bat       the bat scene\n" +
		"\n" +
		"Flags:\n" +
		"  -h       Show help\n" +
		"  --help   Show help\n"
}

// runHalloween prints the seasonal banner art.
//
// 【一手】 agy 1.4.7 draws one of five scenes at startup, each with its
// own colour set; the rasters are the reference's own truecolor output.
func runHalloween(j *JetskiCLI, args []string) error {
	usage := usageHalloween(j.Name)
	var names []string
	for _, a := range args {
		switch a {
		case "-h", "--help":
			fmt.Fprint(j.ErrOut, usage)
			return nil
		default:
			names = append(names, a)
		}
	}

	ev := seasonal.Halloween(time.Now().Year())
	if len(names) == 1 && strings.EqualFold(names[0], "random") {
		names = []string{ev.ArtNames()[rand.IntN(len(ev.Arts))]}
	}
	if err := ev.PrintScenes(j.Out, names); err != nil {
		return errExitCodeMsg(1, "Error: "+err.Error())
	}
	return nil
}
