package render

import (
	"fmt"
	"io"
	"os"
	"strings"
)

// HyperlinkLogTag is the prefix on the "Terminal detection:" debug log line.
const HyperlinkLogTag = "Terminal detection:"

// ProbeHyperlinkCaps builds a HyperlinkCaps from env plus the tmux probe result,
// and optionally emits the "Terminal detection: …" debug log line to w.
func ProbeHyperlinkCaps(
	w io.Writer,
	env []string,
	tmuxProbeResult bool,
	altScreen bool,
) HyperlinkCaps {
	if env == nil {
		env = os.Environ()
	}
	caps := DetectHyperlinkCaps(env, tmuxProbeResult)
	if w != nil && w != io.Discard {
		fmt.Fprintf(w,
			"%s TERM=%q altScreen=%v ssh=%v osc8=%v osc52=%v appleTerminalSSH=%v "+
				"terminal=%q tmuxHyperlinkPassthrough=%v\n",
			HyperlinkLogTag,
			termValue(env),
			altScreen,
			caps.SSH,
			caps.OSC8,
			caps.OSC52,
			caps.AppleTerminalSSH,
			caps.Terminal,
			caps.TmuxHyperlinkPassthrough,
		)
	}
	return caps
}

// TmuxHyperlinkProbe checks whether OSC 8 passthrough is enabled in tmux.
func TmuxHyperlinkProbe(w ...io.Writer) bool {
	if !IsTmuxSession() {
		return false
	}
	if v := strings.TrimSpace(os.Getenv("WAGYU_TMUX_HYPERLINK")); v != "" {
		switch strings.ToLower(v) {
		case "1", "true", "yes", "on":
			return true
		case "0", "false", "no", "off":
			return false
		}
	}
	if len(w) > 0 && w[0] != nil && w[0] != io.Discard {
		fmt.Fprintln(w[0], "tmux hyperlink probe failed, assuming no OSC 8 passthrough: probe not implemented")
	}
	return false
}

// IsTmuxSession reports whether the process is running under tmux.
func IsTmuxSession() bool {
	return os.Getenv("TMUX") != ""
}

func termValue(env []string) string {
	prefix := "TERM="
	for _, e := range env {
		if strings.HasPrefix(e, prefix) {
			return e[len(prefix):]
		}
	}
	return ""
}
