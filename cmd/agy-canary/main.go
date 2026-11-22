package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/justwasm/pigasus/internal/infra/referencedrift"
)

const version = "1.0.0"

func printUsage(w io.Writer) {
	fmt.Fprintf(w, `agy-canary — Automated upstream binary monitor & drift defense tool (version %s)

Usage:
  agy-canary [flags]

Flags:
  -bin PATH       Path to reference agy binary (default: ~/.local/bin/agy or $AGY_REF)
  -check          Exit with code 1 if upstream drift is detected (CI defense mode)
  -json           Output inspection report in machine-readable JSON format
  -update         Automatically refresh reference.lock.json digests from disk
  -update-version Bump lock pinned version to match live binary version
  -update-sha256  Record live binary SHA-256 hash in reference.lock.json
  -ack-lag        Acknowledge version lag up through live binary version
  -lag-note NOTE  Reason note for acknowledged lag
  -repo PATH      Path to repository root (default: current directory)
  -version        Print tool version
  -help           Show this help message
`, version)
}

func run(args []string, stdout, stderr io.Writer) int {
	fs := flag.NewFlagSet("agy-canary", flag.ContinueOnError)
	fs.SetOutput(stderr)

	var (
		binPath    = fs.String("bin", "", "Path to reference agy binary")
		checkMode  = fs.Bool("check", false, "Exit non-zero if drift is detected")
		jsonMode   = fs.Bool("json", false, "Output report as JSON")
		updateLock = fs.Bool("update", false, "Update reference.lock.json")
		updateVer  = fs.Bool("update-version", false, "Update pinned version in lock")
		updateSHA  = fs.Bool("update-sha256", false, "Update binary SHA256 in lock")
		ackLag     = fs.Bool("ack-lag", false, "Acknowledge lag up through live binary")
		lagNote    = fs.String("lag-note", "", "Explanation note for lag acknowledgement")
		repoPath   = fs.String("repo", ".", "Repository root path")
		printVer   = fs.Bool("version", false, "Print version")
	)

	if err := fs.Parse(args); err != nil {
		return 2
	}

	if *printVer {
		fmt.Fprintf(stdout, "agy-canary %s\n", version)
		return 0
	}

	root, err := referencedrift.RepoRoot(*repoPath)
	if err != nil {
		// Fallback to absolute path of repoPath
		if abs, absErr := filepath.Abs(*repoPath); absErr == nil {
			root = abs
		} else {
			fmt.Fprintf(stderr, "error: find repository root from %s: %v\n", *repoPath, err)
			return 1
		}
	}

	targetBin := *binPath
	if targetBin == "" {
		targetBin = referencedrift.FindBinary()
	}

	// Automated update mode
	if *updateLock || *updateVer || *updateSHA || *ackLag {
		res, err := referencedrift.AutoUpdateLock(root, referencedrift.AutoUpdateOptions{
			BinaryPath:         targetBin,
			UpdateVersion:      *updateVer,
			UpdateBinarySHA256: *updateSHA,
			AcknowledgeLag:     *ackLag,
			LagNote:            *lagNote,
		})
		if err != nil {
			fmt.Fprintf(stderr, "error: update lock: %v\n", err)
			return 1
		}
		if *jsonMode {
			b, _ := json.MarshalIndent(res, "", "  ")
			fmt.Fprintln(stdout, string(b))
		} else {
			fmt.Fprint(stdout, res.Report)
		}
		return 0
	}

	// Canary inspection mode
	rep, err := referencedrift.RunCanary(root, targetBin)
	if err != nil {
		fmt.Fprintf(stderr, "error: run canary: %v\n", err)
		return 1
	}

	if *jsonMode {
		b, err := json.MarshalIndent(rep, "", "  ")
		if err != nil {
			fmt.Fprintf(stderr, "error: marshal canary report: %v\n", err)
			return 1
		}
		fmt.Fprintln(stdout, string(b))
	} else {
		fmt.Fprint(stdout, referencedrift.FormatCanaryReport(rep))
	}

	if *checkMode && rep.HasDrift {
		return 1
	}
	return 0
}

func main() {
	os.Exit(run(os.Args[1:], os.Stdout, os.Stderr))
}
