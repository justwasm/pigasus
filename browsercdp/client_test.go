package browsercdp

import (
	"errors"
	"fmt"
	"testing"

	"github.com/chromedp/cdproto"
)

func TestBrowserScoped(t *testing.T) {
	cases := map[string]bool{
		"Browser.getVersion":                 true,
		"Target.getTargets":                  true,
		"SystemInfo.getProcessInfo":          true,
		"Tracing.start":                      true,
		"Page.navigate":                      false,
		"Runtime.evaluate":                   false,
		"Input.dispatchMouseEvent":           false,
		"Emulation.setDeviceMetricsOverride": false,
		"Page.captureScreenshot":             false,
		"DOM.getDocument":                    false,
		"Network.getResponseBody":            false,
	}
	for method, want := range cases {
		if got := browserScoped(method); got != want {
			t.Errorf("browserScoped(%q) = %v, want %v", method, got, want)
		}
	}
}

func TestIsUnknownMethod(t *testing.T) {
	unknown := &cdproto.Error{Code: -32601, Message: "'Foo.bar' wasn't found"}
	if !isUnknownMethod(unknown) {
		t.Errorf("an unknown method was not recognised")
	}
	other := &cdproto.Error{Code: -32000, Message: "Cannot find context with specified id"}
	if isUnknownMethod(other) {
		t.Errorf("another protocol error was taken for an unknown method")
	}
	if isUnknownMethod(errors.New("boom")) {
		t.Errorf("a transport error was taken for an unknown method")
	}
}

func TestCdpError(t *testing.T) {
	err := cdpError("Page.navigate", &cdproto.Error{Code: -32602, Message: "Invalid parameters"})
	if err.Error() != "cdp Page.navigate: Invalid parameters (code -32602)" {
		t.Errorf("unexpected error text: %s", err.Error())
	}
	plain := errors.New("boom")
	if cdpError("Page.navigate", plain) != plain {
		t.Errorf("a transport error was rewritten")
	}
}

func TestRemoteValue(t *testing.T) {
	cases := []struct {
		raw  string
		want string
	}{
		{`"hello"`, "hello"},
		{`42`, "42"},
		{`true`, "true"},
		{`null`, "<nil>"},
		{``, "<nil>"},
	}
	for _, tc := range cases {
		if got := fmt.Sprint(remoteValue([]byte(tc.raw))); got != tc.want {
			t.Errorf("remoteValue(%q) = %q, want %q", tc.raw, got, tc.want)
		}
	}
}
