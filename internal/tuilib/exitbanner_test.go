package tuilib

import (
	"strings"
	"testing"
)

func TestRenderExitBanner_WithConversation(t *testing.T) {
	opts := ExitBannerOptions{
		BinaryName:     "wagyu",
		ConversationID: "12345678-abcd-ef01-2345-6789abcdef01",
		LogoLines:      []string{"▀▀▄ ▄▀▀", " ▀▀▀ "},
		AppName:        "Pigasus CLI",
	}
	out := RenderExitBanner(opts)
	if !strings.Contains(out, "▀▀▄ ▄▀▀") {
		t.Fatalf("expected logo lines, got: %s", out)
	}
	if !strings.Contains(out, "Thanks for using Pigasus CLI!") {
		t.Fatalf("expected greeting, got: %s", out)
	}
	if !strings.Contains(out, "Resume with -c (or command below):") {
		t.Fatalf("expected resume header, got: %s", out)
	}
	if !strings.Contains(out, "wagyu --conversation=12345678-abcd-ef01-2345-6789abcdef01") {
		t.Fatalf("expected resume command, got: %s", out)
	}
}

func TestRenderExitBanner_WithoutConversation(t *testing.T) {
	opts := ExitBannerOptions{
		BinaryName:     "wagyu",
		ConversationID: "",
		LogoLines:      []string{"▀▀▄ ▄▀▀"},
		AppName:        "Pigasus CLI",
	}
	out := RenderExitBanner(opts)
	if !strings.Contains(out, "Thanks for using Pigasus CLI!") {
		t.Fatalf("expected greeting, got: %s", out)
	}
	if strings.Contains(out, "Resume with -c") {
		t.Fatalf("did not expect resume hint when conversation ID is empty, got: %s", out)
	}
}
