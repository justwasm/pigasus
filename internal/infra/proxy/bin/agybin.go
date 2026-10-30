// Package agybin locates and identifies the real Antigravity CLI binary.
//
// The clone's `wagyu agy` (internal/agyproxy) starts a local CloudCode
// proxy and then execs the vendor `agy`. The one way that can go
// catastrophically wrong is exec'ing
// ourselves — an argv loop that spawns wrappers until the machine runs out of
// processes. Path therefore refuses to return the currently running
// executable, comparing both the resolved path and the inode (a hardlink or a
// differently-named copy still fails the inode check).
package bin

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// EnvRealBin is the environment variable that overrides discovery.
const EnvRealBin = "AGY_REAL_BIN"

// DefaultPaths are the well-known install locations, in priority order.
// %s is expanded to $HOME.
var DefaultPaths = []string{
	"~/.openagy/bin/agy",
	"~/.cache/openagy/bin/agy",
	"~/.local/bin/agy",
	"/usr/local/bin/agy",
	"/opt/homebrew/bin/agy",
}

// Path resolves the real agy binary. Order: explicit arg, $AGY_REAL_BIN,
// well-known install paths, then $PATH. If none are found, it attempts
// auto-download from the autoupdater manifest. It never returns the currently
// running executable.
func Path(explicit string) (string, error) {
	self, selfInfo := selfIdentity()

	var seen []string
	var rejectedSelf []string
	for _, cand := range candidates(explicit) {
		resolved, err := resolve(cand)
		if err != nil {
			seen = append(seen, fmt.Sprintf("%s (%v)", cand, err))
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil {
			seen = append(seen, fmt.Sprintf("%s (%v)", resolved, err))
			continue
		}
		if info.IsDir() {
			seen = append(seen, resolved+" (directory)")
			continue
		}
		if !isExecutable(info) {
			seen = append(seen, resolved+" (not executable)")
			continue
		}
		if isSelf(resolved, info, self, selfInfo) {
			rejectedSelf = append(rejectedSelf, resolved)
			continue
		}
		return resolved, nil
	}

	if explicit == "" && os.Getenv(EnvRealBin) == "" && autoDownloadAllowed() {
		downloaded, dlErr := AutoDownload(context.Background())
		if dlErr == nil {
			info, err := os.Stat(downloaded)
			if err == nil && !info.IsDir() && isExecutable(info) && !isSelf(downloaded, info, self, selfInfo) {
				return downloaded, nil
			}
			if err != nil {
				seen = append(seen, fmt.Sprintf("auto-download stat (%v)", err))
			} else if isSelf(downloaded, info, self, selfInfo) {
				rejectedSelf = append(rejectedSelf, downloaded)
			} else if !isExecutable(info) {
				seen = append(seen, downloaded+" (downloaded binary not executable)")
			}
		} else {
			seen = append(seen, fmt.Sprintf("auto-download (%v)", dlErr))
		}
	}

	msg := "agybin: no agy binary found (tried: " + strings.Join(seen, ", ") + ")"
	if len(rejectedSelf) > 0 {
		msg += "; refusing to exec self: " + strings.Join(rejectedSelf, ", ")
	}
	msg += "; set " + EnvRealBin + " to the real binary"
	return "", errors.New(msg)
}

// Version runs `agy --version` and returns the trimmed first line. agy 1.4.2
// prints a bare "1.4.2"; older builds prefixed it with "agy ".
func Version(path string) (string, error) {
	if strings.TrimSpace(path) == "" {
		return "", errors.New("agybin: empty path")
	}
	out, err := exec.Command(path, "--version").Output()
	if err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) && len(out) == 0 {
			out = exitErr.Stderr
		}
		if strings.TrimSpace(string(out)) == "" {
			return "", fmt.Errorf("agybin: %s --version: %w", path, err)
		}
	}
	for _, line := range strings.Split(string(out), "\n") {
		if s := strings.TrimSpace(line); s != "" {
			return s, nil
		}
	}
	return "", fmt.Errorf("agybin: %s --version printed nothing", path)
}

// candidates assembles the search order without touching the filesystem.
func candidates(explicit string) []string {
	var out []string
	add := func(s string) {
		if s = strings.TrimSpace(s); s != "" {
			out = append(out, s)
			if runtime.GOOS == "windows" && !strings.HasSuffix(strings.ToLower(s), ".exe") {
				out = append(out, s+".exe")
			}
		}
	}
	add(explicit)
	add(os.Getenv(EnvRealBin))
	for _, p := range DefaultPaths {
		add(expandHome(p))
	}
	if p, err := exec.LookPath("agy"); err == nil {
		add(p)
	}
	return out
}

func isExecutable(info os.FileInfo) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	return info.Mode().Perm()&0o111 != 0
}

func expandHome(p string) string {
	if strings.HasPrefix(p, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, p[2:])
		}
	}
	return p
}

// resolve makes a path absolute and follows symlinks, so `agy -> ../../bin/agy`
// and the file it points at compare equal.
func resolve(p string) (string, error) {
	abs, err := filepath.Abs(expandHome(p))
	if err != nil {
		return "", err
	}
	if resolved, err := filepath.EvalSymlinks(abs); err == nil {
		return resolved, nil
	}
	// EvalSymlinks fails on a missing file; report the stat error instead of
	// the symlink-resolution one, which is the more useful message.
	if _, err := os.Stat(abs); err != nil {
		return "", err
	}
	return abs, nil
}

// selfIdentity returns the resolved path of the running executable and its
// FileInfo (nil if the platform cannot tell us).
func selfIdentity() (string, os.FileInfo) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		resolved = exe
	}
	info, err := os.Stat(resolved)
	if err != nil {
		info = nil
	}
	return resolved, info
}

// isSelf is the guard. Two independent checks: resolved path equality (catches
// the usual case, including symlinked argv[0]) and inode identity (catches a
// hardlink or a copy at a different name).
func isSelf(candidate string, info os.FileInfo, self string, selfInfo os.FileInfo) bool {
	if self == "" {
		return false
	}
	if candidate == self {
		return true
	}
	if selfInfo != nil && info != nil {
		return os.SameFile(selfInfo, info)
	}
	return false
}
