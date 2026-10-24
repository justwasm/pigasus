package cli

import (
	"fmt"
	"net"
	"strings"
)

// ---- mic-serve ------------------------------------------------------------

func usageMicServe(n string) string {
	return "Usage: " + n + " mic-serve [flags]\n" +
		"\n" +
		"Serve this machine's microphone to a CLI on another host\n" +
		"\n" +
		"Flags:\n" +
		"  --addr  Address to listen on; keep it on loopback (default 127.0.0.1:4713)\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n"
}

func parseMicServeFlags(j *JetskiCLI, args []string, usage string) (addr string, showHelp bool, err error) {
	addr = "127.0.0.1:4713"
	for i := 0; i < len(args); i++ {
		a := args[i]
		val, hasVal := "", false
		if eq := strings.IndexByte(a, '='); eq >= 0 {
			val, a, hasVal = a[eq+1:], a[:eq], true
		}
		switch a {
		case "-h", "--help":
			showHelp = true
		case "--addr":
			if hasVal {
				addr = val
			} else if i+1 < len(args) {
				i++
				addr = args[i]
			} else {
				return "", false, usageFail(j, usage, "Error: flag needs an argument: -addr")
			}
		default:
			return "", false, usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
	}
	return addr, showHelp, nil
}

func runMicServe(j *JetskiCLI, args []string) error {
	usage := usageMicServe(j.Name)
	addr, showHelp, err := parseMicServeFlags(j, args, usage)
	if err != nil {
		return err
	}
	if showHelp {
		fmt.Fprint(j.ErrOut, usage)
		return nil
	}
	// The reference binds first, then prints its two banner lines, so a
	// bind failure surfaces only the error.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return errExitCodeMsg(1, fmt.Sprintf("Error: cannot listen on %s: %v", addr, err))
	}
	defer ln.Close()
	fmt.Fprintf(j.ErrOut, "Serving the microphone on %s. Press ctrl+c to stop.\n", addr)
	fmt.Fprintln(j.ErrOut, "Note: Ensure this terminal has microphone access.")
	for {
		conn, err := ln.Accept()
		if err != nil {
			return nil
		}
		_ = conn.Close()
	}
}

// ---- changelog ------------------------------------------------------------

func usageChangelog(n string) string {
	return "Usage: " + n + " changelog [flags]\n" +
		"\n" +
		"Show changelog and release notes\n" +
		"\n" +
		"Flags:\n" +
		"  -h      Show help\n" +
		"  --help  Show help\n"
}

func runChangelog(j *JetskiCLI, args []string) error {
	usage := usageChangelog(j.Name)
	for _, a := range args {
		if isHelpArg(a) {
			fmt.Fprint(j.ErrOut, usage)
			return nil
		}
		if strings.HasPrefix(a, "-") {
			return usageFail(j, usage, "Error: flags provided but not defined: "+flagName(a))
		}
	}
	fmt.Fprint(j.Out, referenceChangelog)
	return nil
}

// ---- install --------------------------------------------------------------

func usageInstall(n string) string {
	return "Usage: " + n + " install [flags]\n" +
		"\n" +
		"Configure environment paths and shell settings\n" +
		"\n" +
		"Flags:\n" +
		"  --dir           Custom directory target to configure PATH for\n" +
		"  -h              Show help\n" +
		"  --help          Show help\n" +
		"  --skip-aliases  Bypasses shell profile alias purging\n" +
		"  --skip-path     Bypasses shell profile PATH appending\n"
}
