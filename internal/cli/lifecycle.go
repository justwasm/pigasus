package cli

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	_ "embed"
	"errors"
	"flag"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"text/tabwriter"
	"time"

	selfupdate "github.com/creativeprojects/go-selfupdate"
	"github.com/justwasm/pigasus/atomicfile"
	"github.com/justwasm/pigasus/internal/core/branding"
	settings "github.com/justwasm/pigasus/internal/core/settings"
	"github.com/justwasm/pigasus/internal/core/version"
)

// ---- install.go ----

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

// ---- update.go ----

// Release describes an available version update.
type Release interface {
	Version() string
	LessOrEqual(other string) bool
}

// Updater defines the self-update interface to query and stage releases.
type Updater interface {
	DetectLatest(ctx context.Context, repoSlug string) (release Release, found bool, err error)
	UpdateTo(ctx context.Context, release Release, cmdPath string) error
}

type defaultSelfUpdater struct {
	up *selfupdate.Updater
}

func archFilterPattern(arch string) string {
	switch arch {
	case "amd64":
		return "(x86_64|amd64|64bit)"
	case "386":
		return "(i386|386|32bit)"
	case "arm64":
		return "(arm64|aarch64)"
	case "arm":
		return "(armv\\d+|arm)"
	default:
		return regexp.QuoteMeta(arch)
	}
}

func osFilterPattern(goos string) string {
	switch goos {
	case "darwin":
		return "(darwin|mac|macos)"
	default:
		return regexp.QuoteMeta(goos)
	}
}

func commandAssetFilter(cmd, goos, goarch string) string {
	cmd = strings.TrimSuffix(cmd, ".exe")
	cmdPattern := regexp.QuoteMeta(cmd)
	if cmd == "wagyu" {
		cmdPattern = "(pigasus|wagyu)"
	}
	return fmt.Sprintf("(?i)^%s_.*%s.*%s", cmdPattern, osFilterPattern(goos), archFilterPattern(goarch))
}

func resolveCommandName(cmd string) string {
	cmd = strings.ToLower(strings.TrimSuffix(filepath.Base(cmd), ".exe"))
	if cmd == "pigga" || cmd == "pigasus" || cmd == "wagyu" {
		return cmd
	}
	if len(os.Args) > 0 {
		arg0 := strings.ToLower(strings.TrimSuffix(filepath.Base(os.Args[0]), ".exe"))
		if arg0 == "pigga" || arg0 == "pigasus" || arg0 == "wagyu" {
			return arg0
		}
		if strings.Contains(arg0, "pigga") {
			return "pigga"
		}
	}
	if strings.Contains(cmd, "pigga") {
		return "pigga"
	}
	return "pigasus"
}

func newSelfUpdater(cmd string) *selfupdate.Updater {
	filter := commandAssetFilter(cmd, runtime.GOOS, runtime.GOARCH)
	up, err := selfupdate.NewUpdater(selfupdate.Config{
		Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
		Filters:   []string{filter},
	})
	if err != nil {
		return selfupdate.DefaultUpdater()
	}
	return up
}

func (s *defaultSelfUpdater) DetectLatest(ctx context.Context, repoSlug string) (Release, bool, error) {
	up := s.up
	if up == nil {
		exe, _ := os.Executable()
		cmd := resolveCommandName(exe)
		s.up = newSelfUpdater(cmd)
		up = s.up
	}
	rel, found, err := up.DetectLatest(ctx, selfupdate.ParseSlug(repoSlug))
	if err == nil && !found {
		// Fallback to match combined pigasus archive for current OS & ARCH
		fallbackFilter := commandAssetFilter("pigasus", runtime.GOOS, runtime.GOARCH)
		fallbackUp, ferr := selfupdate.NewUpdater(selfupdate.Config{
			Validator: &selfupdate.ChecksumValidator{UniqueFilename: "checksums.txt"},
			Filters:   []string{fallbackFilter},
		})
		if ferr == nil {
			rel, found, err = fallbackUp.DetectLatest(ctx, selfupdate.ParseSlug(repoSlug))
		}
	}
	if err != nil || !found {
		return nil, found, err
	}
	return rel, true, nil
}

func (s *defaultSelfUpdater) UpdateTo(ctx context.Context, release Release, cmdPath string) error {
	rel, ok := release.(*selfupdate.Release)
	if !ok {
		return errors.New("unsupported release type for live selfupdate")
	}

	data, err := downloadAsset(ctx, rel.AssetURL)
	if err != nil {
		return fmt.Errorf("failed to download asset %q: %w", rel.AssetName, err)
	}

	if rel.ValidationAssetURL != "" {
		if err := validateChecksum(ctx, rel.ValidationAssetURL, rel.AssetName, data); err != nil {
			return fmt.Errorf("checksum validation failed: %w", err)
		}
	}

	_, rawCmd := filepath.Split(cmdPath)
	cmd := strings.TrimSuffix(rawCmd, ".exe")
	candidates := []string{cmd}
	if cmd != "pigga" {
		candidates = append(candidates, "pigga")
	}
	if cmd != "pigasus" {
		candidates = append(candidates, "pigasus")
	}

	var binData []byte
	var extractErr error
	for _, targetCmd := range candidates {
		binReader, err := selfupdate.DecompressCommand(bytes.NewReader(data), rel.AssetName, targetCmd, runtime.GOOS, runtime.GOARCH)
		if err == nil {
			binData, extractErr = io.ReadAll(binReader)
			if extractErr == nil && len(binData) > 0 {
				break
			}
		} else if errors.Is(err, selfupdate.ErrCannotDecompressFile) {
			binData = data
			extractErr = nil
			break
		} else {
			extractErr = err
		}
	}
	if len(binData) == 0 {
		if extractErr != nil {
			return fmt.Errorf("error extracting %s from %s: %w", cmd, rel.AssetName, extractErr)
		}
		return fmt.Errorf("extracted binary %s from %s is empty", cmd, rel.AssetName)
	}

	if err := atomicfile.WriteFile(cmdPath, binData, 0o755); err != nil {
		return fmt.Errorf("error writing binary atomically to %s: %w", cmdPath, err)
	}
	return nil
}

func getGitHubToken() string {
	if token := os.Getenv("GITHUB_TOKEN"); token != "" {
		return token
	}
	return os.Getenv("GH_TOKEN")
}

func downloadAsset(ctx context.Context, assetURL string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, assetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/octet-stream")
	if token := getGitHubToken(); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	client := &http.Client{Timeout: 90 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}

func validateChecksum(ctx context.Context, checksumURL, assetName string, assetData []byte) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, checksumURL, nil)
	if err != nil {
		return err
	}
	if token := getGitHubToken(); token != "" {
		req.Header.Set("Authorization", "token "+token)
	}
	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status %d fetching checksums", resp.StatusCode)
	}
	content, err := io.ReadAll(resp.Body)
	if err != nil {
		return err
	}

	actualHash := fmt.Sprintf("%x", sha256.Sum256(assetData))

	scanner := bufio.NewScanner(bytes.NewReader(content))
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) >= 2 {
			expectedHash := strings.ToLower(parts[0])
			filename := filepath.Base(strings.TrimPrefix(parts[1], "*"))
			if strings.EqualFold(filename, assetName) {
				if expectedHash != actualHash {
					return fmt.Errorf("hash mismatch for %s: expected %s, got %s", assetName, expectedHash, actualHash)
				}
				return nil
			}
		}
	}
	return fmt.Errorf("entry for %q not found in checksums", assetName)
}

var (
	activeUpdater Updater = &defaultSelfUpdater{}
	// ProgramVersion optionally records the compiled-in or configured program version.
	ProgramVersion string
)

// SetProgramVersion sets the running program's version string.
func SetProgramVersion(v string) {
	ProgramVersion = v
}

// SetDefaultUpdater allows tests to swap in a mock or custom updater.
func SetDefaultUpdater(u Updater) {
	activeUpdater = u
}

// ResetDefaultUpdater restores the default selfupdater.
func ResetDefaultUpdater() {
	activeUpdater = &defaultSelfUpdater{}
}

// RunUpdate handles the `update` subcommand.
func RunUpdate(w io.Writer, args []string) error {
	exe, _ := os.Executable()
	cmdName := resolveCommandName(exe)
	currentVer := ProgramVersion
	if currentVer == "" {
		currentVer = ReferenceVersion
	}
	if cmdName == "pigga" {
		if env := os.Getenv("PIGGA_CURRENT_VERSION"); env != "" {
			currentVer = env
		} else if env := os.Getenv("PIGASUS_CURRENT_VERSION"); env != "" {
			currentVer = env
		}
	} else {
		if env := os.Getenv("PIGASUS_CURRENT_VERSION"); env != "" {
			currentVer = env
		} else if env := os.Getenv("PIGGA_CURRENT_VERSION"); env != "" {
			currentVer = env
		}
	}
	fmt.Fprintf(w, "\u27f3 Checking for updates... (current version %s)\n", currentVer)

	cfg := branding.Current()
	// Antigravity reference parity / offline mode:
	if cfg.ID == branding.BrandAntigravity || cfg.RepoSlug == "" {
		fmt.Fprint(w, "\u2713 You are already on the latest version.\n")
		return nil
	}

	repoSlug := cfg.RepoSlug
	if env := os.Getenv("PIGGA_UPDATE_REPO"); env != "" && cmdName == "pigga" {
		repoSlug = env
	} else if env := os.Getenv("PIGASUS_UPDATE_REPO"); env != "" {
		repoSlug = env
	} else if env := os.Getenv("PIGGA_UPDATE_REPO"); env != "" {
		repoSlug = env
	}

	return runSelfUpdate(context.Background(), w, activeUpdater, strings.TrimSuffix(currentVer, "-wasm"), repoSlug)
}

func runSelfUpdate(ctx context.Context, w io.Writer, updater Updater, currentVersion string, repoSlug string) error {
	if updater == nil {
		updater = activeUpdater
	}

	latest, found, err := updater.DetectLatest(ctx, repoSlug)
	if err != nil {
		return fmt.Errorf("error occurred while detecting version: %w", err)
	}
	if !found {
		return fmt.Errorf("latest version for %s/%s could not be found from github repository %s", runtime.GOOS, runtime.GOARCH, repoSlug)
	}

	if currentVersion != "" && latest.LessOrEqual(currentVersion) {
		fmt.Fprint(w, "\u2713 You are already on the latest version.\n")
		return nil
	}

	fmt.Fprintf(w, "\u2705 Found new version %s.\n", latest.Version())
	fmt.Fprintln(w, "\u27f3 Downloading update...")

	exe, err := os.Executable()
	if err != nil {
		return errors.New("could not locate executable path")
	}

	if err := updater.UpdateTo(ctx, latest, exe); err != nil {
		return fmt.Errorf("error occurred while updating binary: %w", err)
	}

	fmt.Fprintf(w, "\u2713 Successfully updated to version %s.\n", latest.Version())
	return nil
}

// ---- version.go ----

// Package cli — version.go: the reference version the CLI reports.
//
// The constant itself lives in tier 1 (`internal/core/version`): the
// help panel, the Web UI's session state and the remote-control
// handshake all print it, and none of them may import a host to read
// it. This file keeps the name the CLI's own code already uses.

// ReferenceVersion is the version of the reference `agy` binary the clone's
// observable behaviour is measured against. See `internal/core/version`.
const ReferenceVersion = version.Reference

// ---- changelog_data.go ----

// Package cli — changelog_data.go: the reference binary's `changelog`
// output, embedded verbatim.
//
// `agy changelog` prints the release-notes block bundled in the binary.
// The blob below is the v1.4.11 build's: 81 version sections (1.4.10 down
// through 1.0.0 — the newest section is one release *behind* the
// binary's own version, and 1.3.7/1.3.8/1.3.9 are still skipped
// between 1.4.0 and 1.3.6) totalling 186,601 bytes / 1,062
// lines. The reference output was captured byte-for-byte from
// `agy changelog` and committed as testdata/changelog_reference.txt;
// this file embeds it so the binary ships the exact same bytes.
//
// This supersedes the cycle-49 hand-transcribed 4,640-byte subset, which
// reproduced only the 1.3.5 + 1.3.4 bullets and truncated the remaining
// sections. Older blobs are available in git history (v1.4.0 at
// 153,988 bytes / 915 lines; v1.3.6 at 150,756 bytes / 900 lines;
// v1.3.5 at 148,631 bytes / 892 lines).

// referenceChangelog is the exact byte sequence `agy changelog` prints.
//
//go:embed testdata/changelog_reference.txt
var referenceChangelog string

// ---- jetski.go ----

// Package cli contains the jetski-style CLI engine recovered from the v1.3.4
// binary.
//
// Background: the v1.3.4 binary does not use cobra/pflag at all (zero matches
// for spf13 in `strings`). Instead it uses two layers:
//  1. The Go standard library `flag` package for actual flag parsing.
//  2. An internal package `google3/third_party/jetski/cli/...` that
//     dispatches subcommands, formats help output, manages log files,
//     redirects terminal IO, and tracks step status.
//
// This file is a 1:1 reimplementation of that internal package for our
// recovered module. The goal is byte-for-byte parity with the v1.3.4 binary's
// `antigravity --help` output, not feature parity (we don't need every
// subcommand that the binary exposes).

// JetskiCLI mirrors the internal `jetski/cli` engine. It owns:
//   - The root command (Name + Description)
//   - The ordered list of subcommands with their aliases and sub-subs
//   - The ordered list of global flags (with optional short aliases)
//   - The output writer for help text (defaults to os.Stdout)
//
// Parse() walks os.Args, dispatches to the matched subcommand, and returns
// the exit code (0 on success, 2 on usage error, matching cobra/pflag).
type JetskiCLI struct {
	Name        string
	Description string
	Version     string

	Subcommands []*JetskiSubcommand
	Flags       []*JetskiFlag

	// RootRun is invoked when no subcommand is matched. Mirrors the
	// top-level RunE on a cobra root command. nil means "print help
	// and exit 0".
	RootRun func(args []string) error

	// Lookups populated by Add* methods.
	byName  map[string]*JetskiSubcommand
	byAlias map[string]*JetskiSubcommand

	// Per-parse state.
	flagSet *flag.FlagSet
	args    []string

	// Output destination. Defaults to os.Stdout. Subcommand *results*
	// (mcp list, plugin list, remote-control status, ...) print here.
	Out io.Writer

	// ErrOut is the destination for usage/help text and error
	// diagnostics. Defaults to os.Stderr. The v1.3.5 binary writes
	// `--help`, subcommand `--help`, and every `Error:` line to
	// stderr, while normal results go to stdout — this field mirrors
	// that split. (Before cycle 96 everything went to Out.)
	ErrOut io.Writer
}

// JetskiSubcommand mirrors the description of a single (sub)subcommand
// entry in the help table. v1.3.4 binary stores these in static
// initialization maps; we just use a slice.
type JetskiSubcommand struct {
	Name        string
	Aliases     []string
	Description string
	Subs        []*JetskiSubSubcommand
	// Run is invoked when this subcommand is selected. nil means
	// "list subcommands and exit" (matches jetski/cli default behavior).
	Run func(args []string) error
	// RunJ, when non-nil, takes precedence over Run. Unlike Run it
	// receives the JetskiCLI itself, so a handler can write results to
	// j.Out and usage/errors to j.ErrOut (the split the v1.3.5 binary
	// uses) and read j.Name for the "Usage: <name> ..." preamble.
	RunJ func(j *JetskiCLI, args []string) error
	// Hidden hides the subcommand from help output.
	Hidden bool
	// Help is the full usage block printed (to ErrOut) when the user
	// passes -h/--help (or, for parent commands, when a help arg is
	// present). Empty means "run the handler and let it decide".
	Help string
	// HelpExit is the exit code returned after printing Help. The
	// v1.3.5 binary uses 0 for almost everything; `update --help`
	// (stdlib flag) exits 2.
	HelpExit int
	// UnknownSubError, when non-empty, is the message printed for an
	// unrecognised sub-subcommand, with %s substituted by the name.
	// When set, the Help block (if any) is printed first. Empty means
	// the default `Error: unknown <name> subcommand "<x>"` line with no
	// usage dump. `plugin` overrides it to match the v1.3.5 binary's
	// `usage + Error: unknown command: <x>`.
	UnknownSubError string
}

// JetskiSubSubcommand mirrors the description of a sub-subcommand (e.g.
// `plugin list`, `mcp add`). v1.3.4 binary's subcommands with children
// dispatch to these.
type JetskiSubSubcommand struct {
	Name        string
	Description string
	Run         func(args []string) error
	// RunJ, when non-nil, takes precedence over Run and receives the
	// JetskiCLI (see JetskiSubcommand.RunJ).
	RunJ   func(j *JetskiCLI, args []string) error
	Hidden bool
	// Help is the full usage block printed (to ErrOut) on -h/--help.
	// Empty means the Run handler inspects the args itself (needed for
	// `plugin list --help`, which is an *error*, not a help request).
	Help     string
	HelpExit int
}

// JetskiFlag mirrors a single (sub)command-level flag. v1.3.4 binary stores
// these as `flag.Flag` instances on a `*flag.FlagSet`; we keep a slightly
// higher-level descriptor so the help table can be rendered without
// actually registering the flag yet.
type JetskiFlag struct {
	Name     string // long form, e.g. "add-dir"
	Short    string // single-letter alias, e.g. "c" (empty for none)
	Type     string // "string", "bool", "duration"
	Def      string // default value as a string
	Usage    string // one-line description
	Required bool
	// Hidden keeps the flag out of the --help table while leaving it
	// fully parseable. Used for openagy-only extras so --help stays
	// byte-identical to the v1.3.5 binary (24 flags).
	Hidden bool
	// Dest is the actual variable the flag is bound to. After Parse
	// the caller can read the value through Dest. Internally this is
	// a *bool / *string / *time.Duration matching Type.
	Dest any
}

// NewJetski builds a root CLI with the given name and version. The
// Description is shown in the help header.
func NewJetski(name, version string) *JetskiCLI {
	return &JetskiCLI{
		Name:    name,
		Version: version,
		Out:     os.Stdout,
		ErrOut:  os.Stderr,
		byName:  map[string]*JetskiSubcommand{},
		byAlias: map[string]*JetskiSubcommand{},
	}
}

// AddSubcommand registers a subcommand with the root CLI.
func (j *JetskiCLI) AddSubcommand(s *JetskiSubcommand) {
	j.Subcommands = append(j.Subcommands, s)
	j.byName[s.Name] = s
	for _, a := range s.Aliases {
		j.byAlias[a] = s
	}
}

// AddFlag registers a global flag with the root CLI. Must be called before
// Parse.
func (j *JetskiCLI) AddFlag(f *JetskiFlag) {
	j.Flags = append(j.Flags, f)
}

// Parse walks os.Args, dispatches to the matched subcommand, and returns
// the exit code. If os.Args is empty, the root command's Run is invoked
// with no args.
func (j *JetskiCLI) Parse(args []string) int {
	fs, helpFlag, hFlag := j.initFlagSet()
	if err := fs.Parse(args); err != nil {
		msg := strings.Replace(err.Error(), "flag provided but not defined:", "flags provided but not defined:", 1)
		fmt.Fprintln(j.ErrOut, msg)
		j.PrintHelpTo(j.ErrOut)
		return 2
	}
	remaining := fs.Args()

	if v := getBoolFlag(fs, "version"); v != nil && *v {
		fmt.Fprint(j.Out, j.Version)
		return 0
	}
	if *helpFlag || *hFlag {
		j.PrintHelpTo(j.ErrOut)
		return 0
	}

	if v := flagStrValue(fs, "settings"); v != "" {
		overlay, err := settings.ParseAndValidateFlag(v)
		if err != nil {
			fmt.Fprintf(j.ErrOut, "Error: %s\n", err)
			return 1
		}
		settings.SetSessionOverlay(overlay)
	}

	if v := flagStrValue(fs, "input-format"); v != "" && v != "text" && v != "stream-json" {
		fmt.Fprintf(j.ErrOut, "Error: unknown --input-format %q (want text or stream-json)\n", v)
		return 2
	}
	if v := flagStrValue(fs, "input-format"); v == "stream-json" && flagStrValue(fs, "output-format") != "stream-json" {
		fmt.Fprintf(j.ErrOut, "Error: --input-format %s requires --output-format %s\n", v, "stream-json")
		return 2
	}

	if v := flagStrValue(fs, "json-schema"); strings.TrimSpace(v) != "" {
		outFmt := flagStrValue(fs, "output-format")
		if outFmt != "json" && outFmt != "stream-json" {
			fmt.Fprintln(j.ErrOut, "Error: --json-schema can only be used when --output-format is 'json' or 'stream-json'")
			return 1
		}
	}

	printFlag := firstSetFlag(fs, "print", "prompt", "p")
	interFlag := firstSetFlag(fs, "prompt-interactive", "i")
	isStreamJSON := flagStrValue(fs, "input-format") == "stream-json"
	if printFlag != "" || interFlag != "" || isStreamJSON {
		if code, ok := j.validatePrintFlags(fs, printFlag, interFlag, remaining); !ok {
			return code
		}
		WarnMode(j.ErrOut, j.FlagMode())
		if printFlag != "" && j.FlagMode() == "auto" {
			fmt.Fprintln(j.ErrOut, AutoUnavailableWarning("auto"))
		}
	}

	return j.dispatchArgs(remaining, printFlag == "" && interFlag == "" && !isStreamJSON)
}

func (j *JetskiCLI) initFlagSet() (*flag.FlagSet, *bool, *bool) {
	fs := flag.NewFlagSet(j.Name, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	fs.Usage = func() {}
	j.flagSet = fs

	var helpFlag, hFlag bool
	fs.BoolVar(&helpFlag, "help", false, "Show help")
	fs.BoolVar(&hFlag, "h", false, "Show help")

	j.registerFlags(fs)
	return fs, &helpFlag, &hFlag
}

func (j *JetskiCLI) validatePrintFlags(fs *flag.FlagSet, printFlag, interFlag string, remaining []string) (int, bool) {
	for _, n := range []string{printFlag, interFlag} {
		if n == "" {
			continue
		}
		v := flagStrValue(fs, n)
		if v != "" && isKnownFlagToken(fs, v) {
			fmt.Fprintf(j.ErrOut, "Error: %s took %q as its prompt, so the intended prompt was left as an argument and ignored.\n", flagDisplayName(n), v)
			fmt.Fprintf(j.ErrOut, "Attach the prompt to the flag (%s='your prompt') and move %s elsewhere on the command line.\n", flagDisplayName(n), v)
			return 2, false
		}
	}
	if printFlag != "" && interFlag != "" {
		fmt.Fprintln(j.ErrOut, "error: --print and --prompt-interactive are mutually exclusive")
		return 2, false
	}
	if printFlag != "" && flagStrValue(fs, printFlag) == "" {
		fmt.Fprintf(j.ErrOut, "error: Error: empty prompt. Usage: %s --print \"your prompt here\"\n", j.Name)
		return 1, false
	}
	if isStreamJSON := flagStrValue(fs, "input-format") == "stream-json"; isStreamJSON {
		if printFlag != "" {
			fmt.Fprintf(j.ErrOut, "Error: --input-format stream-json reads prompts from stdin, so a prompt given on the command line would be ignored\n")
			return 2, false
		}
		if len(remaining) > 0 {
			fmt.Fprintf(j.ErrOut, "Error: --input-format stream-json reads prompts from stdin, so a prompt given on the command line would be ignored\n")
			return 2, false
		}
	}
	if len(remaining) > 0 {
		fmt.Fprintf(j.ErrOut, "Error: unexpected argument %q.\n", remaining[0])
		fmt.Fprintln(j.ErrOut, "Prompts are read only from -p/--print, -i/--prompt-interactive, or stdin, so this argument would have been ignored.")
		return 2, false
	}
	if v := flagStrValue(fs, "input-format"); v != "" && v != "text" && v != "stream-json" {
		fmt.Fprintf(j.ErrOut, "Error: unknown --input-format %q (want text or stream-json)\n", v)
		return 2, false
	}
	if v := flagStrValue(fs, "input-format"); v == "stream-json" && flagStrValue(fs, "output-format") != "stream-json" {
		fmt.Fprintf(j.ErrOut, "Error: --input-format %s requires --output-format %s\n", v, "stream-json")
		return 2, false
	}
	return 0, true
}

func (j *JetskiCLI) dispatchArgs(remaining []string, lookForSubcommand bool) int {
	if lookForSubcommand {
		if len(remaining) > 0 {
			name := remaining[0]
			if sub, ok := j.lookup(name); ok {
				return j.dispatch(sub, remaining[1:])
			}
			fmt.Fprintf(j.ErrOut, "Error: unexpected argument %q.\n", name)
			fmt.Fprintln(j.ErrOut, "Prompts are read only from -p/--print, -i/--prompt-interactive, or stdin, so this argument would have been ignored.")
			return 2
		}
		WarnMode(j.ErrOut, j.FlagMode())
	}

	if j.RootRun != nil {
		if err := j.RootRun(remaining); err != nil {
			return j.handleRunErr(err)
		}
		return 0
	}
	return 0
}

// isHelpArg reports whether arg is the stdlib/jetski help form.
func isHelpArg(arg string) bool { return arg == "-h" || arg == "--help" }

// dispatch runs the subcommand, with optional sub-sub dispatch.
func (j *JetskiCLI) dispatch(s *JetskiSubcommand, args []string) int {
	if len(args) > 0 && isHelpArg(args[0]) && s.Help != "" {
		fmt.Fprint(j.ErrOut, s.Help)
		return s.HelpExit
	}
	if len(s.Subs) == 0 {
		return j.dispatchLeaf(s, args)
	}
	if len(args) == 0 {
		return j.dispatchParentNoArgs(s)
	}
	return j.dispatchSubcommand(s, args)
}

func (j *JetskiCLI) dispatchLeaf(s *JetskiSubcommand, args []string) int {
	if s.RunJ != nil {
		return j.handleRunErr(s.RunJ(j, args))
	}
	if s.Run == nil {
		fmt.Fprintf(j.ErrOut, "%s %s: no behavior registered\n", j.Name, s.Name)
		return 2
	}
	if err := s.Run(args); err != nil {
		return j.handleRunErr(err)
	}
	return 0
}

func (j *JetskiCLI) dispatchParentNoArgs(s *JetskiSubcommand) int {
	if s.RunJ != nil {
		return j.handleRunErr(s.RunJ(j, nil))
	}
	if s.Run != nil {
		if err := s.Run(nil); err != nil {
			return j.handleRunErr(err)
		}
		return 0
	}
	if s.Help != "" {
		fmt.Fprint(j.ErrOut, s.Help)
		return s.HelpExit
	}
	for _, ss := range s.Subs {
		if !ss.Hidden {
			fmt.Fprintf(j.Out, "  %s\n", ss.Name)
		}
	}
	return 0
}

func (j *JetskiCLI) dispatchSubcommand(s *JetskiSubcommand, args []string) int {
	name := args[0]
	for _, ss := range s.Subs {
		if ss.Name == name || strings.EqualFold(ss.Name, name) {
			if len(args) > 1 && isHelpArg(args[1]) && ss.Help != "" {
				fmt.Fprint(j.ErrOut, ss.Help)
				return ss.HelpExit
			}
			if ss.RunJ != nil {
				return j.handleRunErr(ss.RunJ(j, args[1:]))
			}
			if err := ss.Run(args[1:]); err != nil {
				return j.handleRunErr(err)
			}
			return 0
		}
	}
	if s.UnknownSubError != "" {
		if s.Help != "" {
			fmt.Fprint(j.ErrOut, s.Help)
		}
		fmt.Fprintf(j.ErrOut, s.UnknownSubError+"\n", name)
		return 1
	}
	fmt.Fprintf(j.ErrOut, "Error: unknown %s subcommand %q\n", s.Name, name)
	return 1
}

// handleRunErr renders a Run-handler error and returns the process
// exit code. The exitCodeError sentinel carries an explicit code;
// everything else is a plain failure (exit 1) printed to stderr.
func (j *JetskiCLI) handleRunErr(err error) int {
	if err == nil {
		return 0
	}
	var ece *exitCodeError
	if errors.As(err, &ece) {
		if ece.msg != "" {
			fmt.Fprintln(j.ErrOut, ece.msg)
		}
		return ece.code
	}
	fmt.Fprintln(j.ErrOut, err.Error())
	return 1
}

func (j *JetskiCLI) lookup(name string) (*JetskiSubcommand, bool) {
	if s, ok := j.byName[name]; ok {
		return s, true
	}
	if s, ok := j.byAlias[name]; ok {
		return s, true
	}
	return nil, false
}

// FlagSet returns the parsed *flag.FlagSet from the most recent Parse call.
// Callers (cmd/wagyu) use this to read flag values via fs.GetBool /
// fs.GetString. Returns nil before Parse.
func (j *JetskiCLI) FlagSet() *flag.FlagSet { return j.flagSet }

// PrintHelp renders the help table to j.Out (kept for direct callers
// such as tests). The `-h/--help` paths call PrintHelpTo(j.ErrOut).
func (j *JetskiCLI) PrintHelp() { j.PrintHelpTo(j.Out) }

// PrintHelpTo renders the help table to w. Format mirrors v1.3.5 binary
// (which uses Go stdlib `flag.PrintDefaults`):
//
//	Usage of antigravity:
//	  --add-dir              description (default [])
//	  --agent                description
//	  ...
//	Available subcommands:
//	  agent         description
//	  ...
//
// We mimic this layout because the recovered module's test rig
// (TestJetskiCLI_1to1) compares line counts to v1.3.5 binary.
func (j *JetskiCLI) PrintHelpTo(w io.Writer) {
	fmt.Fprintf(w, "Usage of %s:\n", j.Name)
	if len(j.Flags) > 0 {
		j.printHelpFlags(w)
	}
	if len(j.Subcommands) > 0 {
		j.printHelpSubcommands(w)
	}
}

func (j *JetskiCLI) printHelpFlags(w io.Writer) {
	flags := append([]*JetskiFlag{}, j.Flags...)
	sort.Slice(flags, func(i, k int) bool { return flags[i].Name < flags[k].Name })
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, f := range flags {
		if f.Usage == "" || f.Hidden {
			continue
		}
		var nameCol string
		if len(f.Name) == 1 {
			nameCol = "-" + f.Name
		} else {
			nameCol = "--" + f.Name
		}
		usage := f.Usage
		if f.Def != "" && f.Def != "false" {
			usage = usage + " (default " + f.Def + ")"
		}
		fmt.Fprintf(tw, "  %s\t%s\n", nameCol, usage)
	}
	tw.Flush()
}

func (j *JetskiCLI) printHelpSubcommands(w io.Writer) {
	fmt.Fprintln(w)
	fmt.Fprintln(w, "Available subcommands:")
	visible := []*JetskiSubcommand{}
	for _, s := range j.Subcommands {
		if !s.Hidden {
			visible = append(visible, s)
		}
	}
	sort.Slice(visible, func(i, k int) bool { return visible[i].Name < visible[k].Name })
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	for _, s := range visible {
		fmt.Fprintf(tw, "  %s\t%s\n", s.Name, s.Description)
	}
	tw.Flush()
}

// ---- jetski_state.go ----

// jetski_state.go — flag registration and value adapter state for the jetski CLI engine.

// registerFlags binds all registered JetskiFlags onto the given flag.FlagSet.
func (j *JetskiCLI) registerFlags(fs *flag.FlagSet) {
	for _, f := range j.Flags {
		dest, _ := newDestFor(f.Type, f.Def)
		f.Dest = dest
		j.bindFlag(fs, f.Name, dest, f.Usage)
		if f.Short != "" && f.Short != f.Name {
			j.bindFlag(fs, f.Short, dest, "alias for --"+f.Name)
		}
	}
}

// bindFlag registers a single flag target with fs.
func (j *JetskiCLI) bindFlag(fs *flag.FlagSet, name string, dest any, usage string) {
	switch v := dest.(type) {
	case *bool:
		fs.BoolVar(v, name, *v, usage)
	case *string:
		fs.StringVar(v, name, *v, usage)
	case *int:
		fs.IntVar(v, name, *v, usage)
	case *time.Duration:
		fs.Var(&durationValue{v: v}, name, usage)
	default:
		fs.Var(v.(flag.Value), name, usage)
	}
}

// newDestFor returns a fresh destination for a flag of the given type.
// It accepts the string form of the default and converts as needed.
func newDestFor(typ, def string) (any, error) {
	switch typ {
	case "bool":
		v := def == "true" || def == "1"
		return &v, nil
	case "int":
		// Cycle 101: the recovered hidden `-v` flag parses its value with
		// strconv.Atoi, so a bad value reads
		// `invalid value "foo" for flag -v: strconv.Atoi: parsing "foo": invalid syntax`
		// (Go's own IntVar would say "parse error"). Register a flag.Value
		// that calls strconv.Atoi directly.
		n := 0
		if def != "" {
			if _, err := strconv.Atoi(def); err != nil {
				return nil, fmt.Errorf("jetski: bad int default %q: %w", def, err)
			}
		}
		return intAtoiValue{&n}, nil
	case "duration":
		if def == "" {
			def = "0s"
		}
		d, err := time.ParseDuration(def)
		if err != nil {
			return nil, fmt.Errorf("jetski: bad duration default %q: %w", def, err)
		}
		return &d, nil
	case "stringSlice":
		ss := &stringSliceValue{}
		if def != "" && def != "[]" {
			for _, p := range strings.Split(def, ",") {
				ss.items = append(ss.items, p)
			}
		}
		return ss, nil
	default: // "string"
		v := def
		return &v, nil
	}
}

// flagBoolValue is the *bool pointer exposed by stdlib flag when
// registered with BoolVar. We re-export it under a stable name so
// version detection in Parse stays readable.
type flagBoolValue = bool

// getBoolFlag returns *bool if the named flag exists and was registered
// as a bool, else nil.
func getBoolFlag(fs *flag.FlagSet, name string) *bool {
	if fs == nil {
		return nil
	}
	f := fs.Lookup(name)
	if f == nil {
		return nil
	}
	if s := f.Value.String(); s == "true" || s == "false" {
		v := s == "true"
		return &v
	}
	return nil
}

// firstSetFlag returns the first of names that appeared on the command line,
// or "" if none did. Cycle 101: print-mode prompt flags are string flags now,
// so getBoolFlag no longer applies and presence must be tested via Visit.
func firstSetFlag(fs *flag.FlagSet, names ...string) string {
	if fs == nil {
		return ""
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	for _, n := range names {
		if set[n] {
			return n
		}
	}
	return ""
}

// flagStrValue returns the string form of a flag's current value.
func flagStrValue(fs *flag.FlagSet, name string) string {
	if fs == nil {
		return ""
	}
	f := fs.Lookup(name)
	if f == nil {
		return ""
	}
	return f.Value.String()
}

// isKnownFlagToken reports whether v looks like a flag this CLI registers —
// "-c", "--effort", "-v=2". Cycle 101: the reference rejects a print prompt
// that is really a flag ("-p took \"-c\" as its prompt") but accepts an
// unregistered dash token ("-x", "--zzz", "--") as the prompt.
func isKnownFlagToken(fs *flag.FlagSet, v string) bool {
	if fs == nil || !strings.HasPrefix(v, "-") {
		return false
	}
	name := strings.TrimLeft(v, "-")
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	if name == "" {
		return false
	}
	return fs.Lookup(name) != nil
}

// flagDisplayName renders a registered flag name the way the user would type
// it: single letters get one dash, everything else two.
func flagDisplayName(name string) string {
	if len(name) == 1 {
		return "-" + name
	}
	return "--" + name
}

// intAtoiValue is a flag.Value for the recovered hidden `-v` flag. The
// reference parses it with strconv.Atoi, so the stdlib flag error reads
// `invalid value "foo" for flag -v: strconv.Atoi: parsing "foo": invalid syntax`
// (Go's own IntVar would say "parse error").
type intAtoiValue struct{ p *int }

func (v intAtoiValue) String() string {
	if v.p == nil {
		return "0"
	}
	return strconv.Itoa(*v.p)
}

func (v intAtoiValue) Set(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*v.p = n
	return nil
}

// durationValue is the stdlib duration flag with one difference: Set returns
// time.ParseDuration's own error instead of collapsing it to the generic
// "parse error" that flag's own durationValue substitutes. The reference
// reports the underlying error verbatim:
//
//	invalid value "abc" for flag -print-timeout: time: invalid duration "abc"
//
// (stdlib would say `…: parse error`). --print-timeout is the only duration
// flag today, but the wrapper is generic.
type durationValue struct{ v *time.Duration }

func (d durationValue) String() string {
	if d.v == nil {
		return "0s"
	}
	return d.v.String()
}

func (d durationValue) Set(s string) error {
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d.v = v
	return nil
}

// stringSliceValue is a flag.Value implementation that accumulates
// repeated -tag invocations into a slice.
type stringSliceValue struct{ items []string }

func (ss *stringSliceValue) String() string     { return strings.Join(ss.items, ",") }
func (ss *stringSliceValue) Set(v string) error { ss.items = append(ss.items, v); return nil }
func (ss *stringSliceValue) Get() any           { return ss.items }
