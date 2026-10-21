// open_browser.go: best-effort "open URL in the user's default browser".
//
// Cycle 172. The OAuth 2.0 device-flow prompt (cycle 170) prints a
// verification URL the user is supposed to visit; cycling 171 routes the
// prompt through the Markdown renderer so it looks structured. This
// file closes the last UX gap: instead of asking the user to copy-paste
// the URL, we hand it off to the platform's "open" command (xdg-open
// on Linux, `open` on macOS, rundll32 on Windows) and let the OS pick
// the default browser.
//
// The OpenBrowser call is intentionally fire-and-forget: errors are
// reported to stderr but do NOT fail the OAuth flow, since the prompt
// text the user sees is the source of truth. SSH / headless users can
// disable the auto-open with --no-browser (handled by the caller's
// flag); the env var OPENAGY_NO_BROWSER=1 is also honoured for shells
// that prefer env-driven configuration.
package cli

import (
	"fmt"
	"os"
	"os/exec"
	"runtime"
)

// OpenBrowser asks the platform's default-browser launcher to open
// url. It returns nil on a successful *launch* (the child process
// detaches; we don't wait for the browser to actually open).
//
// Errors are returned for the caller's convenience but the canonical
// caller (runLogin) logs them and continues — the printed prompt is
// the authoritative UI surface.
func OpenBrowser(url string) error {
	if url == "" {
		return fmt.Errorf("open_browser: empty url")
	}
	name, args := browserLauncher()
	if name == "" {
		return fmt.Errorf("open_browser: no launcher for %s", runtime.GOOS)
	}
	cmd := exec.Command(name, append(args, url)...)
	// Detach from the parent stdio so the launched browser does not
	// inherit our TTY.
	cmd.Stdin = nil
	cmd.Stdout = nil
	cmd.Stderr = nil
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("open_browser: %s %v: %w", name, args, err)
	}
	// Release the process so it survives our own exit. Wait would
	// block until the browser closed.
	return cmd.Process.Release()
}

// browserLauncher returns the platform's "open this URL" command and
// its leading arguments. The URL is appended by OpenBrowser.
func browserLauncher() (string, []string) {
	switch runtime.GOOS {
	case "darwin":
		return "open", nil
	case "windows":
		// rundll32 is the canonical "open URL" incantation on
		// Windows; it dispatches to the registered protocol
		// handler for https://, http://, etc.
		return "rundll32", []string{"url.dll,FileProtocolHandler"}
	default:
		// Linux, *BSD, Solaris: xdg-open is the freedesktop.org
		// standard. If the binary is missing the cmd.Start below
		// surfaces ENOENT, which is the expected error path.
		return "xdg-open", nil
	}
}

// BrowserAutoOpen reports whether the call site should attempt to
// open the verification URL in the user's browser. False if either:
//
//   - $OPENAGY_NO_BROWSER is "1" or "true" (case-insensitive); or
//   - the explicit noBrowser argument is true.
//
// Pass noBrowser=true to honour the --no-browser CLI flag.
func BrowserAutoOpen(noBrowser bool) bool {
	if noBrowser {
		return false
	}
	if v := os.Getenv("OPENAGY_NO_BROWSER"); v != "" {
		switch v {
		case "1", "true", "TRUE", "True", "yes", "YES":
			return false
		}
	}
	return true
}
