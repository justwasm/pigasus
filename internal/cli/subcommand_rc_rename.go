package cli

import (
	"fmt"
	"strings"

	"github.com/justwasm/pigasus/internal/persistentrc"
)

// usageRCRename documents the `remote-control rename` subcommand — the
// clone's own way to change the name the dashboard shows for this
// machine.
//
// It is **hidden**: the reference's `remote-control --help` lists
// start, status and stop only, and that block is the reference's, byte
// for byte. `rename` is accepted, not advertised — the same way `run`
// and `serve` are.
//
// The reference has no such subcommand: it renames the instance from
// the Web UI's settings panel, and the language server watches the
// configuration file for the change. The clone offers the same thing
// from the shell, because the shell is where the user is when the
// dashboard is on another machine.
func usageRCRename(n string) string {
	return "Usage: " + n + " remote-control rename <name>\n" +
		"\n" +
		"Set this machine's instance name shown in Remote Control\n" +
		"\n" +
		"The name is written to the configuration file, so a running daemon\n" +
		"picks it up and renames the instance on the dashboard without\n" +
		"dropping the connection.\n" +
		"\n" +
		"Flags:\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n"
}

// runRCRename is the implementation of `pigasus remote-control rename`.
//
// The configuration file is the source of truth — the reference's own
// model: the CLI writes the name into the config proto
// (jetski/cli/remotecontrol.(*Daemon).SetInstanceName at 0x102d35320 →
// config.ApplyProtoUpdate) and the language server reads it back.
//
// A running daemon watches that file and renames the instance over the
// API, so the dashboard follows without the tunnel dropping. A daemon
// that is not running picks the name up when it next starts.
func runRCRename(j *JetskiCLI, args []string) error {
	usage := usageRCRename(j.Name)

	var name string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "-h" || a == "--help":
			fmt.Fprint(j.ErrOut, usage)
			return nil
		case strings.HasPrefix(a, "-"):
			return usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		case name == "":
			name = a
		default:
			return usageFail(j, usage, "Error: rename takes exactly one name")
		}
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return usageFail(j, usage, "Error: rename needs a name")
	}
	if err := persistentrc.ValidateName(name); err != nil {
		return ExitCodeError(2, fmt.Sprintf("Error: %v", err))
	}
	if err := persistentrc.SetInstanceName(name); err != nil {
		return ExitCodeError(1, fmt.Sprintf("Error: %v", err))
	}
	fmt.Fprintf(j.Out, "Instance name: %s\n", persistentrc.ResolveInstanceName(""))
	return nil
}
