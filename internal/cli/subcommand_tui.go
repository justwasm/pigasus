package cli

import (
	"fmt"
	"strings"
)

// usageTUI documents the `pigasus tui` subcommand.
func usageTUI(prog string) string {
	return "Usage: " + prog + " tui [flags]\n" +
		"\n" +
		"Run the interactive session.\n" +
		"\n" +
		"With --server the session attaches to a remote pigasus server: the turn\n" +
		"runs there, on that machine's workspace, tools and credentials, and the\n" +
		"conversation renders here. Without it the session is the ordinary local\n" +
		"one.\n" +
		"\n" +
		"Flags:\n" +
		"  -h, --help        Show help\n" +
		"  --backend         Agent backend to drive (\"local\", \"pigasus\", \"agy\", \"codex\", \"claude\", \"codex-acp\", \"claude-acp\", \"agy-acp-server\", default \"local\")\n" +
		"  --pigasus         Shortcut for --backend pigasus (Pigasus subprocess via stdio stream-json)\n" +
		"  --agy             Shortcut for --backend agy (Antigravity subprocess via stdio stream-json)\n" +
		"  --codex           Shortcut for --backend codex\n" +
		"  --claude          Shortcut for --backend claude\n" +
		"  --codex-acp       Shortcut for --backend codex-acp (OpenAI Codex via ACP)\n" +
		"  --claude-acp      Shortcut for --backend claude-acp (Claude Agent via ACP)\n" +
		"  --agy-acp         Shortcut for --backend agy-acp-server (Antigravity via ACP)\n" +
		"  --acp-cmd         Command to spawn a custom ACP agent subprocess over stdio\n" +
		"  --acp-manifest    Path to an ACP agent.json manifest file or directory\n" +
		"  --server          Address of the pigasus server to attach to (ws://host:port)\n" +
		"  --token           Bearer token the server demands\n" +
		"  --protocol        Protocol to speak with the server (\"acp\" or \"agentlink\", default \"acp\")\n" +
		"  --acp-version     ACP protocol version (\"auto\", \"1\", \"2\", default \"auto\")\n"
}

// TUISubcommand returns the SubcommandSpec for `pigasus tui`.
func TUISubcommand() SubcommandSpec {
	return SubcommandSpec{
		Name:   "tui",
		Short:  "Run the interactive session, locally or attached to a server",
		HelpFn: usageTUI,
		RunJ:   runTUI,
	}
}

type tuiOpts struct {
	backend     string
	server      string
	token       string
	protocol    string
	acpVersion  string
	acpCmd      string
	acpManifest string
	showHelp    bool
	// rest holds the arguments the subcommand does not own. They are
	// the session's own flags, and the session parses them.
	rest []string
}

func parseTUIFlags(j *JetskiCLI, args []string, usage string) (tuiOpts, error) {
	opts := tuiOpts{protocol: "acp", acpVersion: "auto"}
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
		case "-h", "--help", "-help":
			opts.showHelp = true
		case "-backend", "--backend":
			if err := consume(&opts.backend, a); err != nil {
				return opts, err
			}
		case "-pigasus", "--pigasus":
			opts.backend = "pigasus"
		case "-codex", "--codex":
			opts.backend = "codex"
		case "-claude", "--claude":
			opts.backend = "claude"
		case "-agy", "--agy":
			opts.backend = "agy"
		case "-codex-acp", "--codex-acp":
			opts.backend = "codex-acp"
		case "-claude-acp", "--claude-acp":
			opts.backend = "claude-acp"
		case "-agy-acp", "--agy-acp":
			opts.backend = "agy-acp-server"
		case "-acp-cmd", "--acp-cmd":
			if err := consume(&opts.acpCmd, a); err != nil {
				return opts, err
			}
		case "-acp-manifest", "--acp-manifest":
			if err := consume(&opts.acpManifest, a); err != nil {
				return opts, err
			}
		case "-server", "--server":
			if err := consume(&opts.server, a); err != nil {
				return opts, err
			}
		case "-token", "--token":
			if err := consume(&opts.token, a); err != nil {
				return opts, err
			}
		case "-protocol", "--protocol":
			if err := consume(&opts.protocol, a); err != nil {
				return opts, err
			}
		case "-acp-version", "--acp-version", "-acp-protocol-version", "--acp-protocol-version":
			if err := consume(&opts.acpVersion, a); err != nil {
				return opts, err
			}
		default:
			// The session's own flags are the root command's flags, and
			// the session parses them — so they are passed through
			// untouched.
			opts.rest = append(opts.rest, args[i])
		}
	}
	return opts, nil
}

func runTUI(j *JetskiCLI, args []string) error {
	usage := usageTUI(j.Name)
	opts, err := parseTUIFlags(j, args, usage)
	if err != nil {
		return err
	}
	if opts.showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}
	if tuiHandler == nil {
		fmt.Fprintln(j.ErrOut, "tui: no handler registered (binary was built without the tui bridge)")
		return nil
	}
	return tuiHandler(j, TUIArgs{
		Backend:     opts.backend,
		Server:      opts.server,
		Token:       opts.token,
		Protocol:    opts.protocol,
		ACPVersion:  opts.acpVersion,
		ACPCmd:      opts.acpCmd,
		ACPManifest: opts.acpManifest,
		Args:        opts.rest,
	})
}
