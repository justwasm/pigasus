//go:build darwin

// darwin_plist.go: plist generation + plist-parsing for the
// macOS / launchd implementation.
//
// Split out of darwin.go in cycle `rc-filesize-internal-persistentrc`
// to keep the file below the 500-LOC ceiling.  This file owns:
//
//   - buildPlist              — render LaunchAgent plist XML
//   - writePlist              — write the plist (+ dry-run pidfile)
//   - parsePlistProgramArgs        — read plist + parse ProgramArguments
//   - parsePlistProgramArgsFromBytes — bytes-level companion used by tests
//
// The XML shape is hand-written (no plist library) because the
// launchd plist grammar we need is tiny (Label, ProgramArguments,
// RunAtLoad, KeepAlive, ProcessType, StandardOutPath, StandardErrorPath)
// and emitting it byte-identically to what buildPlist wrote is
// what TestBuildPlist_RoundTrip asserts on.

package persistentrc

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
)

// buildPlist renders the LaunchAgent plist XML for the given Spec.
// Exposed for the test rig (TestBuildPlist_RoundTrip,
// TestBuildPlist_SessionFlag) so the assertion can read it back
// without spawning launchd.
//
// The shape of the plist follows the reference binary's recovered
// LaunchAgent entries (see `out/local-macos/dec/` and the
// strings-addrs.tsv entries around
// `failed to write LaunchAgent plist: %w`).  Field semantics:
//
//   - Label                — "<labelPrefix><name>"
//   - ProgramArguments     — [binary, "remote-control-foreground",
//     "--addr", addr, "--name", name,
//     "--log-file", logFile]
//   - RunAtLoad + KeepAlive— only when !spec.Session
//   - ProcessType=Background — keep the daemon out of the Dock
//   - StandardOutPath/ErrorPath — both point at LogFile
//
// The output is stable: identical inputs produce byte-identical
// output.  Tests rely on this to assert "the plist we wrote was
// the plist launchd will see".
func buildPlist(spec Spec) string {
	label := Label(spec.Name)
	var b bytes.Buffer
	fmt.Fprintf(&b, `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string>
		<string>remote-control-foreground</string>
		<string>--addr</string>
		<string>%s</string>
		<string>--name</string>
		<string>%s</string>
		<string>--log-file</string>
		<string>%s</string>
	</array>
`, label, spec.BinaryPath, spec.Addr, spec.Name, spec.LogFile)
	if !spec.Session {
		// Boot-persistent.  RunAtLoad + KeepAlive give us:
		//   - launchd spawns the daemon immediately on bootstrap
		//   - launchd re-spawns it on crash
		// The reference uses the same pair (recovered from the
		// "RunAtLoad" / "KeepAlive" string-table hits around
		// `failed to write LaunchAgent plist: %w`).
		fmt.Fprintf(&b, `	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<true/>
`)
	}
	fmt.Fprintf(&b, `	<key>ProcessType</key>
	<string>Background</string>
	<key>StandardOutPath</key>
	<string>%s</string>
	<key>StandardErrorPath</key>
	<string>%s</string>
</dict>
</plist>
`, spec.LogFile, spec.LogFile)
	return b.String()
}

// writePlist writes the LaunchAgent plist to the path
// plistPath(spec) returns.  The parent directory is created with
// mode 0o755 if missing.  The file is mode 0o644 (LaunchAgent
// plists don't need to be executable; launchd reads them as the
// user that owns them).
//
// When Spec.SkipLaunchd is true the file is written into the
// Spec.PlistDir (test-only) and a fake pidfile is dropped
// alongside it so Status can recover a non-zero PID without
// launchd being involved.
func writePlist(spec Spec) error {
	p := plistPath(spec)
	if p == "" {
		return errors.New("could not resolve LaunchAgent plist path (HOME unset?)")
	}
	if dir := filepath.Dir(p); dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("mkdir %s: %w", dir, err)
		}
	}
	content := buildPlist(spec)
	if err := os.WriteFile(p, []byte(content), 0o644); err != nil {
		return fmt.Errorf("write plist %s: %w", p, err)
	}
	if spec.SkipLaunchd {
		// Drop a fake pidfile so Status can recover a
		// non-zero PID.  The PID is the *current process*
		// (so a follow-up kill() check confirms liveness),
		// captured once at Start time.
		pid := os.Getpid()
		if err := os.WriteFile(pidfilePath(spec), []byte(strconv.Itoa(pid)), 0o644); err != nil {
			return fmt.Errorf("write pidfile: %w", err)
		}
		// Drop a side-car started_at file so Status can
		// surface a stable timestamp in dry-run reports.
		// Tests use this to pin the StartedAt epoch.
		startedAt := resolveStartedAtEpoch(&spec)
		if startedAt > 0 {
			scPath := plistPath(spec) + ".started_at"
			_ = os.WriteFile(scPath, []byte(strconv.FormatInt(startedAt, 10)), 0o644)
		}
		// Touch the log file so the daemon doesn't have to
		// create it; this matches what launchd's "redirect
		// stdout to a fresh file" path would do.
		if spec.LogFile != "" {
			_ = os.MkdirAll(filepath.Dir(spec.LogFile), 0o755)
			f, err := os.OpenFile(spec.LogFile, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
			if err == nil {
				_ = f.Close()
			}
		}
	}
	return nil
}

// parsePlistProgramArgs is a deliberately-tiny plist parser:
// it just scans for `<key>ProgramArguments</key> ... <array>
// <string>X</string>... </array>` and returns the strings.
// We don't need a real plist library for this; launchd-written
// plists use the same XML shape our buildPlist emits.
func parsePlistProgramArgs(plistPath string) ([]string, error) {
	data, err := os.ReadFile(plistPath)
	if err != nil {
		return nil, err
	}
	return parsePlistProgramArgsFromBytes(data)
}

// parsePlistProgramArgsFromBytes is the bytes-level companion
// to parsePlistProgramArgs.  Exposed for the test rig.
func parsePlistProgramArgsFromBytes(data []byte) ([]string, error) {
	const openTag = "<key>ProgramArguments</key>"
	idx := bytes.Index(data, []byte(openTag))
	if idx < 0 {
		return nil, errors.New("plist: no ProgramArguments key")
	}
	rest := data[idx+len(openTag):]
	const arrayOpen = "<array>"
	arrIdx := bytes.Index(rest, []byte(arrayOpen))
	if arrIdx < 0 {
		return nil, errors.New("plist: no <array> after ProgramArguments")
	}
	rest = rest[arrIdx+len(arrayOpen):]
	const arrayClose = "</array>"
	endIdx := bytes.Index(rest, []byte(arrayClose))
	if endIdx < 0 {
		return nil, errors.New("plist: no </array>")
	}
	body := rest[:endIdx]
	const strOpen, strClose = "<string>", "</string>"
	var out []string
	for {
		i := bytes.Index(body, []byte(strOpen))
		if i < 0 {
			break
		}
		body = body[i+len(strOpen):]
		j := bytes.Index(body, []byte(strClose))
		if j < 0 {
			break
		}
		out = append(out, string(body[:j]))
		body = body[j+len(strClose):]
	}
	return out, nil
}
