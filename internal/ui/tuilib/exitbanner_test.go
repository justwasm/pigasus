package tuilib

import (
	"strings"
	"testing"
)

func TestRenderExitBanner_WithMetadataAndResume(t *testing.T) {
	opts := ExitBannerOptions{
		BinaryName:     "pigasus",
		ConversationID: "86add28f-2de2-4096-ae1b-c45216b65575",
		LogoLines: []string{
			"      ▄▀▀▄    ",
			"     ▀▀▀▀▀▀   ",
			"    ▀▀▀▀▀▀▀▀  ",
			"   ▄▀▀    ▀▀▄ ",
			"  ▄▀▀      ▀▀▄",
		},
		AppName: "Pigasus CLI",
		Version: "1.4.4",
		Email:   "navigaid@gmail.com",
		Cwd:     "~/GitHub/openagy",
	}
	out := RenderExitBanner(opts)
	if !strings.Contains(out, "Pigasus CLI 1.4.4") {
		t.Fatalf("expected title in banner, got: %s", out)
	}
	if !strings.Contains(out, "navigaid@gmail.com") {
		t.Fatalf("expected email in banner, got: %s", out)
	}
	if !strings.Contains(out, "~/GitHub/openagy") {
		t.Fatalf("expected cwd in banner, got: %s", out)
	}
	if !strings.Contains(out, "Resume with -c (or command below):") {
		t.Fatalf("expected resume prompt, got: %s", out)
	}
	if !strings.Contains(out, "pigasus --conversation=86add28f-2de2-4096-ae1b-c45216b65575") {
		t.Fatalf("expected pigasus resume command, got: %s", out)
	}
}

func TestRenderExitBanner_WithoutConversation(t *testing.T) {
	opts := ExitBannerOptions{
		BinaryName: "wagyu",
		LogoLines:  []string{"  ▄▀▀▄  "},
		AppName:    "Pigasus CLI",
	}
	out := RenderExitBanner(opts)
	if !strings.Contains(out, "Pigasus CLI") {
		t.Fatalf("expected app name, got: %s", out)
	}
	if strings.Contains(out, "Resume with -c") {
		t.Fatalf("did not expect resume hint when conversation ID is empty, got: %s", out)
	}
}
