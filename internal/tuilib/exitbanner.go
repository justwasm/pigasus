package tuilib

import (
	"fmt"
	"math/rand/v2"
	"strings"
)

// ExitBannerOptions configures the post-exit terminal banner.
type ExitBannerOptions struct {
	BinaryName     string   // CLI executable name (e.g. "wagyu", "pigasus", "agy")
	ConversationID string   // Active or prior conversation ID (empty to omit resume hint)
	LogoLines      []string // Rendered ANSI art lines of the logo
	AppName        string   // Display product name (default "Pigasus CLI")
}

var exitPartingMessages = []string{
	"See ya later.",
	"Come back soon.",
	"Get some rest.",
	"Nice work.",
	"Take care of yourself.",
	"Stay in your flow.",
	"Remember to hydrate.",
	"Time for a snack.",
	"That was fun.",
	"Have a wonderful day.",
}

// RandomExitMessage returns a friendly parting remark.
func RandomExitMessage() string {
	return exitPartingMessages[rand.IntN(len(exitPartingMessages))]
}

// RenderExitBanner formats the terminal exit banner inspired by Crush.
func RenderExitBanner(opts ExitBannerOptions) string {
	var b strings.Builder
	b.WriteString("\n")
	renderExitLogo(&b, opts.LogoLines)
	renderExitGreeting(&b, opts.AppName)
	renderExitResume(&b, opts.BinaryName, opts.ConversationID)
	b.WriteString("\n")
	return b.String()
}

func renderExitLogo(b *strings.Builder, lines []string) {
	for _, line := range lines {
		trimmed := strings.TrimRight(line, " \t\r\n")
		if trimmed != "" {
			b.WriteString("  ")
			b.WriteString(trimmed)
			b.WriteString("\n")
		}
	}
}

func renderExitGreeting(b *strings.Builder, appName string) {
	name := strings.TrimSpace(appName)
	if name == "" {
		name = "Pigasus CLI"
	}
	msg := RandomExitMessage()
	b.WriteString(fmt.Sprintf("\n  Thanks for using %s! %s\n", name, msg))
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
	b.WriteString("\n  Resume with -c (or command below):\n")
	b.WriteString(fmt.Sprintf("  %s --conversation=%s\n", bin, id))
}
