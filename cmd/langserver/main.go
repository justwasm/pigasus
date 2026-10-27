// Command langserver is the clone's standalone language server: the
// rebuild of the Antigravity IDE's `language_server` binary.
//
// The reference is the second binary of the family this repository
// clones — the Antigravity IDE ships it, built from the same
// monorepo commit as the `agy` CLI (out/ide-language-server/). The
// IDE spawns it, reads its port from the startup log, loads the web
// UI from it and talks Connect-RPC to it.
//
// The clone reproduces the reference's flag surface, its `-stamp`
// answer, its startup log, its on-disk layout and its two
// listeners. What answers the service is the clone's own agent
// kernel, not a second implementation.
//
// Usage:
//
//	langserver [flags]
//
// The reference's own flags are the whole surface; `--help` prints
// them.
package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"os"
	"os/signal"
	"syscall"

	"github.com/justwasm/pigasus/internal/lsserver"
)

func main() {
	os.Exit(run(os.Args))
}

// run is main's body, with the exit code as its result.
//
// The argument handling follows the reference's own, which is not Go's
// `flag` package's: the help exits 2, an unknown flag is reported in
// the plural, and `--help <pattern>` prints one flag's help. See
// internal/lsserver/help.go.
func run(argv []string) int {
	return runWith(argv, os.Stdout, os.Stderr)
}

// runWith is run's body with the two streams as parameters.
//
// The reference's `-stamp` goes to stdout and everything else — the
// help, the complaints, the log — goes to stderr.
func runWith(argv []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet(argv[0], flag.ContinueOnError)
	fs.SetOutput(stderr)
	fs.Usage = func() { lsserver.PrintUsage(fs, stderr, "") }
	flags := lsserver.Register(fs)

	inspected := lsserver.InspectArgs(fs, argv[1:])
	sidecars := inspected.Hidden["enable_sidecars"]
	flags.EnableSidecars = sidecars != "" && sidecars != "false"
	if inspected.Help {
		lsserver.PrintUsage(fs, stderr, inspected.Pattern)
		return 2
	}
	if inspected.Complaint != "" {
		fmt.Fprintln(stderr, inspected.Complaint)
		fs.Usage()
		fmt.Fprintln(stderr, lsserver.ParseErrorLine)
		return 2
	}
	if err := fs.Parse(inspected.Rest); err != nil {
		// Go printed the complaint and the usage behind it.
		fmt.Fprintln(stderr, lsserver.ParseErrorLine)
		return 2
	}

	if flags.Stamp {
		if err := lsserver.WriteStamp(stdout); err != nil {
			fmt.Fprintf(stderr, "language_server: %v\n", err)
			return 1
		}
		return 0
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	if !flags.UseMockedData {
		// The workbench's turns reach a real model — the mocked mode
		// keeps the deterministic stand-in.
		wireBackend()
	}
	if err := lsserver.Run(ctx, flags, stdout, stderr); err != nil {
		fmt.Fprintf(stderr, "language_server: %v\n", err)
		return 1
	}
	return 0
}
