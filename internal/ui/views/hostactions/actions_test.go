package hostactions

import (
	"errors"
	"testing"
)

func TestHandleClear(t *testing.T) {
	aborted := false
	cleared := false
	seams := Seams{
		AbortTurn:  func() { aborted = true },
		ClearState: func() { cleared = true },
	}
	if !HandleClear(seams, "") {
		t.Fatal("HandleClear returned false")
	}
	if !aborted || !cleared {
		t.Fatalf("HandleClear: aborted=%v, cleared=%v; want true, true", aborted, cleared)
	}
}

func TestHandleExit(t *testing.T) {
	quit := false
	seams := Seams{
		Quit: func() { quit = true },
	}
	if !HandleExit(seams, "") {
		t.Fatal("HandleExit returned false")
	}
	if !quit {
		t.Fatal("HandleExit did not call Quit")
	}
}

func TestHandleCopy(t *testing.T) {
	var copiedText string
	var respondedCmd, respondedMsg string

	seams := Seams{
		GetPlannerResponses: func() []string {
			return []string{"first response", "second response"}
		},
		WriteClipboard: func(text string) error {
			copiedText = text
			return nil
		},
		Respond: func(cmd string, msg string, extra ...string) {
			respondedCmd = cmd
			respondedMsg = msg
		},
	}

	// 1. Bare /copy -> last response
	HandleCopy(seams, "")
	if copiedText != "second response" {
		t.Fatalf("HandleCopy bare: got %q, want %q", copiedText, "second response")
	}
	if respondedCmd != "/copy" || respondedMsg != "Copied last response to clipboard." {
		t.Fatalf("HandleCopy responded: (%q, %q)", respondedCmd, respondedMsg)
	}

	// 2. /copy 2 -> first response (index 1 from end)
	HandleCopy(seams, "2")
	if copiedText != "first response" {
		t.Fatalf("HandleCopy 2: got %q, want %q", copiedText, "first response")
	}

	// 3. /copy with clipboard write failure
	seams.WriteClipboard = func(text string) error {
		return errors.New("clip fail")
	}
	HandleCopy(seams, "")
	if respondedMsg != "Failed to copy to clipboard: clip fail" {
		t.Fatalf("HandleCopy error: got %q", respondedMsg)
	}

	// 4. /copy btw
	seams.GetBtwCard = func() (string, string) {
		return "btw payload", "Copied BTW to clipboard."
	}
	seams.WriteClipboard = func(text string) error {
		copiedText = text
		return nil
	}
	HandleCopy(seams, "btw")
	if copiedText != "btw payload" {
		t.Fatalf("HandleCopy btw: got %q, want %q", copiedText, "btw payload")
	}
}
