package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/justwasm/pigasus/internal/agyupdate"
	"github.com/justwasm/pigasus/internal/cli"
)

func run(args []string, stdout, stderr io.Writer) int {
	curVer := os.Getenv("AGY_CURRENT_VERSION")
	if curVer == "" {
		curVer = cli.ReferenceVersion
	}

	for _, arg := range args {
		if arg == "-version" || arg == "--version" {
			fmt.Fprintln(stdout, curVer)
			return 0
		}
		if arg == "-help" || arg == "--help" {
			fmt.Fprintln(stderr, "Usage of update:")
			return 2
		}
		if strings.HasPrefix(arg, "-") {
			flagName := strings.TrimLeft(arg, "-")
			fmt.Fprintf(stderr, "flags provided but not defined: -%s\n", flagName)
			fmt.Fprintln(stderr, "Usage of update:")
			fmt.Fprintf(stderr, "Error: flags provided but not defined: -%s\n", flagName)
			return 1
		}
		if !strings.HasPrefix(arg, "-") {
			// Positional args are ignored by agy update
			break
		}
	}

	targetBin := os.Getenv("AGY_TARGET_BINARY")
	if targetBin == "" {
		if agyPath, err := exec.LookPath("agy"); err == nil {
			targetBin = agyPath
		} else if exe, err := os.Executable(); err == nil {
			targetBin = exe
		}
	}
	if targetBin != "" {
		if realPath, err := filepath.EvalSymlinks(targetBin); err == nil {
			targetBin = realPath
		}
	}

	opts := agyupdate.Options{
		TargetBinary: targetBin,
	}
	return agyupdate.Run(context.Background(), stdout, stderr, opts)
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}

