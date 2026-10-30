package builtin

import (
	"context"
	"testing"
)

// TestNilRegistryAnswersInsteadOfPanicking pins the guard on a registry
// that was never set.
//
// The process-wide registry can be cleared — subagenttools keeps a
// setter for it — and the callers of Spec and Invoke are the routing
// paths that run while a tool call is in flight. A nil registry must
// read as "no such built-in", not as a crash: the model gets the
// reference's "not found or not allowed" message and the turn
// continues.
func TestNilRegistryAnswersInsteadOfPanicking(t *testing.T) {
	var reg *Registry

	if _, ok := reg.Spec(string(KindBrowser)); ok {
		t.Error("a nil registry must not report a spec")
	}
	if _, ok := reg.Spec(string(KindImageGenerator)); ok {
		t.Error("a nil registry must not report a spec")
	}
	res, err := reg.Invoke(context.Background(), string(KindBrowser), "open a page")
	if err == nil {
		t.Fatalf("a nil registry must refuse the call, got %+v", res)
	}
}

// TestKindBrowserIsTheReferencesOwnName pins the name the reference
// uses for its browser agent.
//
// The reference's built-in agents package carries two browser agents —
// `chrome_devtools` and `webview_browser` — and they sit in one string
// blob next to `image-generator`, which the reference's own prompt
// lists as an available subagent. The clone ships the Chrome one.
func TestKindBrowserIsTheReferencesOwnName(t *testing.T) {
	if KindBrowser != "chrome_devtools" {
		t.Errorf("KindBrowser = %q, want %q", KindBrowser, "chrome_devtools")
	}
}
