package tuilib

import (
	"fmt"
	"strings"
)

const (
	cBlueBoldOpen = "\x1b[38;2;38;139;210;1m"
	cDimGrayOpen  = "\x1b[38;2;101;123;131m"
	ansiReset     = "\x1b[0m"
)

// ExitBannerOptions configures the post-exit terminal banner.
type ExitBannerOptions struct {
	BinaryName     string   // CLI executable name (e.g. "wagyu", "pigasus", "agy")
	ConversationID string   // Active or prior conversation ID (empty to omit resume hint)
	LogoLines      []string // Rendered ANSI art lines of the logo (5 lines)
	AppName        string   // Display product name (default "Pigasus CLI")
	Version        string   // CLI version (e.g. "1.4.4")
	Email          string   // User account email (e.g. "navigaid@gmail.com")
	Cwd            string   // Current working directory (e.g. "~/GitHub/openagy")
}

// RenderExitBanner formats the terminal exit banner with the logo on the left
// and app/account/workspace metadata on the right, followed by the resume hint.
func RenderExitBanner(opts ExitBannerOptions) string {
	var b strings.Builder
	b.WriteString("\n")
	renderExitBannerSideBySide(&b, opts)
	renderExitResume(&b, opts.BinaryName, opts.ConversationID)
	return b.String()
}

func renderExitBannerSideBySide(b *strings.Builder, opts ExitBannerOptions) {
	lines := opts.LogoLines
	title := buildBannerTitle(opts.AppName, opts.Version)

	for i := 0; i < len(lines); i++ {
		b.WriteString(lines[i])
		switch i {
		case 0:
			if title != "" {
				b.WriteString("  " + cBlueBoldOpen + title + ansiReset)
			}
		case 1:
			if opts.Email != "" {
				b.WriteString("  " + cDimGrayOpen + opts.Email + ansiReset)
			}
		case 2:
			if opts.Cwd != "" {
				b.WriteString("  " + cDimGrayOpen + opts.Cwd + ansiReset)
			}
		}
		b.WriteString("\n")
	}
}

func buildBannerTitle(appName, version string) string {
	name := strings.TrimSpace(appName)
	if name == "" {
		name = "Pigasus CLI"
	}
	ver := strings.TrimSpace(version)
	if ver != "" {
		return name + " " + ver
	}
	return name
}

func renderExitResume(b *strings.Builder, binary, convID string) {
	id := strings.TrimSpace(convID)
	if id == "" {
		return
	}
	bin := strings.TrimSpace(binary)
	if bin == "" || bin == "." || bin == "/" {
		bin = "wagyu"
	}
	b.WriteString("\nResume with -c (or command below):\n")
	b.WriteString(fmt.Sprintf("%s --conversation=%s\n\n", bin, id))
}
