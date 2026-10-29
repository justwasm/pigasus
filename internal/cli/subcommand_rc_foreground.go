package cli

import (
	"errors"
	"fmt"
	"os"

	"github.com/justwasm/pigasus/internal/persistentrc"
)

// errNoServeHandler reports that the binary was built without the
// serve bridge — the parser surface without the handler that brings
// the daemon up.
//
// It is a sentinel so each caller can answer in its own way: the
// reference's `remote-control serve` prints a notice and exits 0 (its
// own behaviour, pinned by a test), while `remote-control start` and
// `remote-control run` — the clone's own surfaces — exit 1.
var errNoServeHandler = errors.New("no serve handler registered")

// rcForegroundArgs is the resolved set of options the foreground
// daemon runs with.
type rcForegroundArgs struct {
	// Addr is the bind address (host:port). Empty means the
	// handler's default.
	Addr string
	// Name is the machine's instance label.
	Name string
	// NoLogin skips the inline OAuth device-code login when no
	// bearer token is on disk.
	NoLogin bool
	// Desktop serves the desktop UI (the voice input, the jetbox
	// terminal, the aux pane).
	Desktop bool
	// Label names the subcommand the user typed. The daemon's own
	// progress lines are prefixed with it, so the user sees the
	// command they ran — "remote-control serve", "remote-control
	// run", "remote-control start".
	Label string
}

// runRCForegroundDaemon brings the daemon up in the foreground and
// blocks until it comes down.
//
// It is the **one** implementation behind the three foreground
// surfaces:
//
//   - `remote-control serve` — the reference's own name for it, and
//     the command the launchd plist runs;
//   - `remote-control run` — the clone's explicit "run it here" name,
//     the same on every platform;
//   - `remote-control start` — on a platform without launchd, where
//     there is no service manager to register with.
//
// The stack is the same in all three: the dispatcher, the TCP and
// WebSocket server, and the cloud tunnel, in this terminal, until the
// user interrupts it.
//
// It returns errNoServeHandler when the binary was built without the
// serve bridge; every other error is the daemon's own.
func runRCForegroundDaemon(j *JetskiCLI, args rcForegroundArgs) error {
	if serveHandler == nil {
		return errNoServeHandler
	}
	label := args.Label
	if label == "" {
		label = "remote-control serve"
	}
	if err := serveHandler(j, ServeArgs{
		Addr:    args.Addr,
		Name:    args.Name,
		NoLogin: args.NoLogin,
		Desktop: args.Desktop,
		Label:   label,
	}); err != nil {
		return err
	}
	// The sentinel `remote-control stop` prints too, so a script can
	// share one grep between the two.
	fmt.Fprintln(j.Out, "Remote-control daemon stopped.")
	return nil
}

// resolveRCForegroundName resolves the instance name the foreground
// daemon registers under.
//
// The rules are the reference's: an explicit --name is sanitised by
// persistentrc.ResolveInstanceName, and an unset one falls back to the
// resolved instance name, then to the hostname.
func resolveRCForegroundName(name string) (string, error) {
	if name == "" {
		name = persistentrc.ResolveInstanceName("")
		if name == "" {
			hn, herr := os.Hostname()
			if herr != nil || hn == "" {
				return "", ExitCodeError(1, fmt.Sprintf("Error: --name is required (could not determine hostname: %v)", herr))
			}
			name = hn
		}
		return name, nil
	}
	return persistentrc.ResolveInstanceName(name), nil
}
