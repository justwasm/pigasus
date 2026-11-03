package cli

import (
	"fmt"
	"strconv"
	"strings"
)

// usageServer documents the `pigasus server` subcommand.
func usageServer(prog string) string {
	return "Usage: " + prog + " server [flags]\n" +
		"\n" +
		"Host the agent and serve it over a websocket, so a client can attach to\n" +
		"this machine's workspace, tools and credentials.\n" +
		"\n" +
		"The server always demands a bearer token: it runs shell commands against\n" +
		"its own workspace. A token is minted when --token is not given, and it is\n" +
		"printed when the server starts.\n" +
		"\n" +
		"Flags:\n" +
		"  -h, --help        Show help\n" +
		"  --addr            Bind address for the server (host:port, default 127.0.0.1:7777)\n" +
		"  -p, --port        Port to listen on (convenience override, e.g. 7777, 8080)\n" +
		"  --host            Host interface to bind on (default 127.0.0.1)\n" +
		"  -w, --workspace   Root workspace folder to serve (default: current directory)\n" +
		"  --dir             Alias for --workspace\n" +
		"  --model           Model the served turns run under\n" +
		"  --effort          Reasoning effort the served turns ask for\n" +
		"  --mode            Permission mode: auto, plan or acceptEdits\n" +
		"  --dangerously-skip-permissions  Run the served tools without the permission gate\n" +
		"  --token           Bearer token a client has to present (default: minted)\n" +
		"  --agent           Custom agent the served turns run under\n" +
		"  --backend         Transport the served turns run on (gateway, cloudcode, shim)\n" +
		"  --add-dir         Extra workspace the served tools may act on (repeatable)\n" +
		"  --acp-version     ACP protocol version (\"auto\", \"1\", \"2\", default \"auto\")\n"
}

// ServerSubcommand returns the SubcommandSpec for `pigasus server`.
func ServerSubcommand() SubcommandSpec {
	return SubcommandSpec{
		Name:   "server",
		Short:  "Host the agent and serve it over a websocket",
		HelpFn: usageServer,
		RunJ:   runServer,
	}
}

type serverOpts struct {
	addr            string
	port            int
	host            string
	workspace       string
	model           string
	effort          string
	mode            string
	skipPermissions bool
	token           string
	agent           string
	backend         string
	addDirs         []string
	acpVersion      string
	showHelp        bool
}

func parseServerFlags(j *JetskiCLI, args []string, usage string) (serverOpts, error) {
	var opts serverOpts
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
		case "-addr", "--addr":
			if err := consume(&opts.addr, a); err != nil {
				return opts, err
			}
		case "-p", "--port", "-port":
			var portStr string
			if err := consume(&portStr, a); err != nil {
				return opts, err
			}
			p, err := strconv.Atoi(strings.TrimSpace(portStr))
			if err != nil || p <= 0 || p > 65535 {
				return opts, usageFail(j, usage, fmt.Sprintf("Error: invalid port: %q", portStr))
			}
			opts.port = p
		case "-host", "--host":
			if err := consume(&opts.host, a); err != nil {
				return opts, err
			}
		case "-w", "--workspace", "-workspace", "--dir", "-dir":
			if err := consume(&opts.workspace, a); err != nil {
				return opts, err
			}
		case "-model", "--model":
			if err := consume(&opts.model, a); err != nil {
				return opts, err
			}
		case "-effort", "--effort":
			if err := consume(&opts.effort, a); err != nil {
				return opts, err
			}
		case "-mode", "--mode":
			if err := consume(&opts.mode, a); err != nil {
				return opts, err
			}
		case "-dangerously-skip-permissions", "--dangerously-skip-permissions":
			opts.skipPermissions = true
		case "-token", "--token":
			if err := consume(&opts.token, a); err != nil {
				return opts, err
			}
		case "-agent", "--agent":
			if err := consume(&opts.agent, a); err != nil {
				return opts, err
			}
		case "-backend", "--backend":
			if err := consume(&opts.backend, a); err != nil {
				return opts, err
			}
		case "-add-dir", "--add-dir":
			var dir string
			if err := consume(&dir, a); err != nil {
				return opts, err
			}
			opts.addDirs = append(opts.addDirs, dir)
		case "-acp-version", "--acp-version", "-acp-protocol-version", "--acp-protocol-version":
			if err := consume(&opts.acpVersion, a); err != nil {
				return opts, err
			}
		default:
			return opts, usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
	}
	return opts, nil
}

func runServer(j *JetskiCLI, args []string) error {
	usage := usageServer(j.Name)
	opts, err := parseServerFlags(j, args, usage)
	if err != nil {
		return err
	}
	if opts.showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}
	if serverHandler == nil {
		fmt.Fprintln(j.ErrOut, "server: no handler registered (binary was built without the server bridge)")
		return nil
	}
	return serverHandler(j, ServerArgs{
		Addr:            opts.addr,
		Port:            opts.port,
		Host:            opts.host,
		Workspace:       opts.workspace,
		Model:           opts.model,
		Effort:          opts.effort,
		Mode:            opts.mode,
		SkipPermissions: opts.skipPermissions,
		Token:           opts.token,
		Agent:           opts.agent,
		Backend:         opts.backend,
		AddDirs:         opts.addDirs,
		ACPVersion:      opts.acpVersion,
	})
}
