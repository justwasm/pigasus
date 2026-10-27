package cli

import (
	"fmt"
	"strconv"
	"strings"
)

// usageWeb documents the `pigasus web` subcommand.
func usageWeb(prog string) string {
	return "Usage: " + prog + " web [flags]\n" +
		"\n" +
		"Start the standalone web UI server for local browser and LAN access without\n" +
		"requiring remote-control, cloud tunnels, or Google account login.\n" +
		"\n" +
		"Flags:\n" +
		"  -h, --help        Show help\n" +
		"  --addr            Bind address for the web server (host:port, default 0.0.0.0:8787)\n" +
		"  -p, --port        Port to listen on (convenience override, e.g. 8787, 8080)\n" +
		"  --host            Host interface to bind on (default 0.0.0.0)\n" +
		"  -w, --workspace   Root workspace folder to serve (default: current directory)\n" +
		"  --dir             Alias for --workspace\n" +
		"  --open            Automatically open the web UI in default browser upon startup\n" +
		"  --reference-backend  Proxy the RPCs to a reference language_server instead of\n" +
		"                     answering them, and serve the reference's own frontend\n" +
		"                     assets: an endpoint (host:port) of a running reference, or\n" +
		"                     the path of a reference binary to start\n" +
		"  --desktop         Serve the desktop UI: the voice input, the jetbox\n" +
		"                    terminal, the redesigned aux pane. The page and the\n" +
		"                    bundle are the same either way — the switch is the\n" +
		"                    identity the page's Mendel read carries\n"
}

// WebSubcommand returns the SubcommandSpec for `pigasus web`.
func WebSubcommand() SubcommandSpec {
	return SubcommandSpec{
		Name:   "web",
		Short:  "Start the standalone web UI server for local and LAN access",
		HelpFn: usageWeb,
		RunJ:   runWeb,
	}
}

type webOpts struct {
	addr             string
	port             int
	host             string
	workspace        string
	open             bool
	referenceBackend string
	desktop          bool
	showHelp         bool
}

func parseWebFlags(j *JetskiCLI, args []string, usage string) (webOpts, error) {
	var opts webOpts
	for i := 0; i < len(args); i++ {
		a := args[i]
		val, hasVal := "", false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val, a, hasVal = a[eq+1:], a[:eq], true
		}
		consume := func(target *string, flag string) error {
			if hasVal {
				*target = val
			} else if i+1 < len(args) {
				i++
				*target = args[i]
			} else {
				return usageFail(j, usage, "Error: flag needs an argument: "+flag)
			}
			return nil
		}
		switch a {
		case "-h", "--help":
			opts.showHelp = true
		case "--addr":
			if err := consume(&opts.addr, "--addr"); err != nil {
				return opts, err
			}
		case "-p", "--port":
			var portStr string
			if err := consume(&portStr, a); err != nil {
				return opts, err
			}
			p, err := strconv.Atoi(strings.TrimSpace(portStr))
			if err != nil || p <= 0 || p > 65535 {
				return opts, usageFail(j, usage, fmt.Sprintf("Error: invalid port: %q", portStr))
			}
			opts.port = p
		case "--host":
			if err := consume(&opts.host, "--host"); err != nil {
				return opts, err
			}
		case "-w", "--workspace", "--dir":
			if err := consume(&opts.workspace, a); err != nil {
				return opts, err
			}
		case "--open":
			opts.open = true
		case "--reference-backend":
			if err := consume(&opts.referenceBackend, a); err != nil {
				return opts, err
			}
		case "--desktop":
			opts.desktop = true
		default:
			return opts, usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
	}
	return opts, nil
}

func runWeb(j *JetskiCLI, args []string) error {
	usage := usageWeb(j.Name)
	opts, err := parseWebFlags(j, args, usage)
	if err != nil {
		return err
	}
	if opts.showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}
	if webHandler == nil {
		fmt.Fprintln(j.ErrOut, "web: no handler registered (binary was built without the web bridge)")
		return nil
	}
	return webHandler(j, WebArgs{
		Addr:             opts.addr,
		Port:             opts.port,
		Host:             opts.host,
		Workspace:        opts.workspace,
		Open:             opts.open,
		ReferenceBackend: opts.referenceBackend,
		Desktop:          opts.desktop,
	})
}
