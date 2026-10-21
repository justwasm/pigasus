// Package cli — install.go: the v1.3.5 binary's `agy install` subcommand.
//
// `agy install` is informational in the v1.3.5 binary: it prints setup
// instructions and a success banner but performs no filesystem mutation.
// Verified on macOS by running it against a scratch $HOME and a
// non-default --dir: neither --dir nor $HOME/.local/bin is created, and
// no shell profile is written. The recovered handler therefore does NOT
// copy the binary anywhere (an earlier revision did, which is both
// unfaithful and hazardous — it would overwrite an installed reference).
//
// What it prints depends on whether the directory it is told to configure
// (--dir, default $HOME/.local/bin) is already on $PATH.
//
// --dir NOT in $PATH:
//
//	stderr: Running Pigasus CLI setup...
//	stderr: (blank)
//	stderr: Warning: <dir> is not present in your active PATH.
//	stderr: To use the 'agy' CLI globally, please manually add it to your shell profile:
//	stderr: (blank)
//	stderr:   echo 'export PATH="<dir>:$PATH"' >> ~/.bashrc && source ~/.bashrc
//	stderr: (blank)
//	stdout: (blank)
//	stdout: ✅ Pigasus CLI binary placed successfully at $HOME/.local/bin/agy
//	stdout: Note: The binary is installed, but it is not in your active PATH. Please follow the instructions above to add it, then restart your terminal session.
//
// --dir IS in $PATH:
//
//	stderr: Running Pigasus CLI setup...
//	stderr: PATH verification: ~/.local/bin is correctly configured in active PATH environment.
//	stdout: (blank)
//	stdout: ✅ Pigasus CLI installed successfully at $HOME/.local/bin/agy
//	stdout: Run 'agy' to start the CLI
//
// The v1.3.5 binary logs through glog, so its stderr lines carry an
// "ERROR: logging before google.Init: I<m> <h:m:s.µs> <pid> <file>:<line>]"
// prefix. The recovered build does not use glog and prints the bare
// message text — the one deliberate divergence here.
//
// The success banner always names $HOME/.local/bin/agy (the installer's
// canonical target); --dir only changes the directory named in the PATH
// warning / verification line.
//
// Flags (read from the global FlagSet or, failing that, a scan of
// os.Args — the parser strips them from the global set before subcommand
// dispatch):
//
//	--dir           string  directory to configure (default ~/.local/bin)
//	--skip-aliases  bool    accepted; no observable effect here
//	--skip-path     bool    accepted; no observable effect here (the
//	                        warning block still prints when --dir is
//	                        absent from $PATH — verified against the
//	                        reference)
package cli

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// runInstall reproduces the v1.3.5 binary's `agy install` output. See the
// package comment for the exact stream layout. It never writes to disk.
func runInstall(d *dispatchContext) error {
	dir, _, _ := installResolveFlags(d.FS)
	inPath := dirInPath(dir)

	// The installer always reports its canonical target, independent of
	// --dir. --dir only drives the PATH warning / verification line.
	installPath := filepath.Join(filepath.Clean(expandHome("~/.local/bin")), "agy")

	// Setup line (installer.go:27).
	fmt.Fprintln(d.Err, "Running Pigasus CLI setup...")

	if inPath {
		// installer_unix.go:40 — the literal "~/.local/bin" is what the
		// reference prints here, even when --dir points elsewhere.
		fmt.Fprintln(d.Err, "PATH verification: ~/.local/bin is correctly configured in active PATH environment.")
		fmt.Fprintf(d.Out, "\n\u2705 Pigasus CLI installed successfully at %s\n", installPath)
		fmt.Fprintln(d.Out, "Run 'agy' to start the CLI")
		return nil
	}

	// PATH-warning block. The reference's format string starts with a
	// newline, which is the blank line after the setup line.
	fmt.Fprintf(d.Err, "\nWarning: %s is not present in your active PATH.\n", dir)
	fmt.Fprintln(d.Err, "To use the 'agy' CLI globally, please manually add it to your shell profile:")
	fmt.Fprintln(d.Err)
	fmt.Fprintf(d.Err, "  echo 'export PATH=\"%s:$PATH\"' >> ~/.bashrc && source ~/.bashrc\n", dir)
	fmt.Fprintln(d.Err)

	fmt.Fprintf(d.Out, "\n\u2705 Pigasus CLI binary placed successfully at %s\n", installPath)
	fmt.Fprintln(d.Out, "Note: The binary is installed, but it is not in your active PATH. Please follow the instructions above to add it, then restart your terminal session.")
	return nil
}

// installResolveFlags reads --dir / --skip-aliases / --skip-path from the
// dispatch FlagSet, falling back to a scan of os.Args (the parser passes
// nil for the FlagSet). The returned dir is expanded and cleaned.
func installResolveFlags(fs *flag.FlagSet) (dir string, skipAliases, skipPath bool) {
	dir = "~/.local/bin"

	if fs != nil {
		if f := fs.Lookup("dir"); f != nil {
			if v := strings.TrimSpace(f.Value.String()); v != "" {
				dir = v
			}
		}
		if f := fs.Lookup("skip-aliases"); f != nil {
			skipAliases = f.Value.String() == "true"
		}
		if f := fs.Lookup("skip-path"); f != nil {
			skipPath = f.Value.String() == "true"
		}
	}

	if fs == nil {
		dirArg, okA := scanOsArgs("dir")
		if okA && dirArg != "" {
			dir = dirArg
		}
		_, okB := scanOsArgs("skip-aliases")
		skipAliases = okB
		_, okP := scanOsArgs("skip-path")
		skipPath = okP
	}

	dir = expandHome(dir)
	dir = filepath.Clean(dir)
	return dir, skipAliases, skipPath
}

// scanOsArgs searches os.Args[1:] for the named flag and returns the value
// (for --key=value or --key value) and a found boolean. Returns ("", false)
// when the flag is absent.
//
// Recognised forms:
//
//	--key           → present, value ""
//	--key=value     → present, value "value"
//	--key  value    → present, value "value" (next argv slot)
//
// The two-arg form is only honoured when the next slot does not start with
// "--" (so --key --other still parses as a bare --key). The name argument
// is the bare flag name WITHOUT the leading "--" — e.g. "dir".
func scanOsArgs(name string) (string, bool) {
	args := os.Args[1:]
	for i, a := range args {
		if !strings.HasPrefix(a, "--") {
			continue
		}
		rest := a[2:]
		if rest == name {
			if i+1 < len(args) && !strings.HasPrefix(args[i+1], "--") {
				return args[i+1], true
			}
			return "", true
		}
		if strings.HasPrefix(rest, name+"=") {
			return rest[len(name)+1:], true
		}
	}
	return "", false
}

// expandHome replaces a leading "~" or "~/" with the user's home
// directory. It does NOT handle "~user" expansion. Empty strings are
// returned as-is.
func expandHome(p string) string {
	if p == "" {
		return p
	}
	if p == "~" {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return p
		}
		return home
	}
	if strings.HasPrefix(p, "~/") {
		home, err := os.UserHomeDir()
		if err != nil || home == "" {
			return p
		}
		return filepath.Join(home, p[2:])
	}
	return p
}

// dirInPath returns true when dir (already absolute) is a member of the
// current $PATH. Trailing slashes are normalised; an empty $PATH always
// returns false.
func dirInPath(dir string) bool {
	if dir == "" {
		return false
	}
	pathEnv := os.Getenv("PATH")
	if pathEnv == "" {
		return false
	}
	for _, p := range filepath.SplitList(pathEnv) {
		if p == dir {
			return true
		}
		if filepath.Clean(p) == filepath.Clean(dir) {
			return true
		}
	}
	return false
}
