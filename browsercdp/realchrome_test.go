package browsercdp

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// TestBrowserCDP_RealChrome drives a real Chrome through the client.
//
// It is off by default: it needs a Chrome on the machine and it starts a
// process. PIGASUS_BROWSER_TEST=1 turns it on.
//
// This is the road where the clone starts the browser itself: the policy
// points it at a profile of its own and at a headless Chrome, so the
// user's session is left alone.
func TestBrowserCDP_RealChrome(t *testing.T) {
	if os.Getenv("PIGASUS_BROWSER_TEST") == "" {
		t.Skip("set PIGASUS_BROWSER_TEST=1 to drive a real Chrome")
	}
	if findChromeBinary() == "" {
		t.Skip("no chrome on this machine")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>Test Page</title></head>`+
			`<body><h1>Hello from the page</h1><script>console.log("hello from the console");</script></body></html>`)
	}))
	defer srv.Close()

	t.Setenv("CHROME_REMOTE_DEBUGGING_PORT", "")
	Configure(Config{
		UseLocalChrome: false,
		Headless:       os.Getenv("PIGASUS_BROWSER_TEST_VISIBLE") == "",
		UserDataDir:    filepath.Join(t.TempDir(), "profile"),
		Port:           DefaultCDPPort,
	})
	t.Cleanup(func() { Configure(DefaultConfig()) })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := GetClient(ctx)
	if err != nil {
		t.Fatalf("GetClient failed: %v", err)
	}
	defer client.Close()

	actions := NewActions(client)

	navMsg, err := actions.OpenURL(ctx, srv.URL)
	if err != nil {
		t.Fatalf("OpenURL failed: %v", err)
	}
	if !strings.Contains(navMsg, "Test Page") {
		t.Errorf("navMsg carries no title: %s", navMsg)
	}

	text, err := actions.ReadPage(ctx)
	if err != nil {
		t.Fatalf("ReadPage failed: %v", err)
	}
	if !strings.Contains(text, "Hello from the page") {
		t.Errorf("unexpected page text: %q", text)
	}

	title, err := client.Evaluate(ctx, "document.title")
	if err != nil {
		t.Fatalf("Evaluate failed: %v", err)
	}
	if title != "Test Page" {
		t.Errorf("got title %#v, want %q", title, "Test Page")
	}

	// The console log and the network log arrive as events.
	deadline := time.Now().Add(10 * time.Second)
	var logs []string
	var reqs []NetworkRequest
	for time.Now().Before(deadline) {
		logs = client.GetConsoleLogs(0)
		reqs = client.GetNetworkRequests()
		if len(logs) > 0 && len(reqs) > 0 {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if !strings.Contains(strings.Join(logs, "\n"), "hello from the console") {
		t.Errorf("no console log captured: %v", logs)
	}
	found := false
	for _, r := range reqs {
		if strings.Contains(r.URL, srv.URL) {
			found = true
			if r.Status != http.StatusOK {
				t.Errorf("request %s has status %d", r.URL, r.Status)
			}
		}
	}
	if !found {
		t.Errorf("no network request captured: %v", reqs)
	}

	pages, err := actions.ListPages(ctx)
	if err != nil {
		t.Fatalf("ListPages failed: %v", err)
	}
	if !strings.Contains(pages, "Browser Pages:") {
		t.Errorf("unexpected pages listing: %s", pages)
	}
}

// TestBrowserCDP_RealChromeAttaches drives a real Chrome that was already
// running — the road the clone takes when the user has a Chrome of their
// own.
//
// The Chrome is started here with a debugging port, and the clone attaches
// to it: the session is that Chrome's, and the clone opens a tab of its
// own in it rather than driving the tab the Chrome was started with.
func TestBrowserCDP_RealChromeAttaches(t *testing.T) {
	if os.Getenv("PIGASUS_BROWSER_TEST") == "" {
		t.Skip("set PIGASUS_BROWSER_TEST=1 to drive a real Chrome")
	}
	bin := findChromeBinary()
	if bin == "" {
		t.Skip("no chrome on this machine")
	}

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		fmt.Fprint(w, `<!doctype html><html><head><title>Attached Page</title></head>`+
			`<body><h1>Hello from the attached Chrome</h1></body></html>`)
	}))
	defer srv.Close()

	port := freePort(t)
	cmd := exec.Command(bin,
		"--remote-debugging-port="+port,
		"--user-data-dir="+filepath.Join(t.TempDir(), "profile"),
		"--no-first-run",
		"--headless=new",
		"about:blank",
	)
	if err := cmd.Start(); err != nil {
		t.Fatalf("start chrome: %v", err)
	}
	defer func() {
		_ = cmd.Process.Kill()
		_ = cmd.Wait()
	}()

	// Wait for the browser to answer on its debugging port.
	ready := false
	for i := 0; i < 100; i++ {
		if _, err := queryWebSocketURL(context.Background(), cdpEndpoint(port)); err == nil {
			ready = true
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if !ready {
		t.Fatal("the chrome never answered on its debugging port")
	}

	t.Setenv("CHROME_REMOTE_DEBUGGING_PORT", port)
	Configure(Config{Port: DefaultCDPPort})
	t.Cleanup(func() { Configure(DefaultConfig()) })

	ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
	defer cancel()

	client, err := GetClient(ctx)
	if err != nil {
		t.Fatalf("GetClient failed: %v", err)
	}
	defer client.Close()

	actions := NewActions(client)
	if _, err := actions.OpenURL(ctx, srv.URL); err != nil {
		t.Fatalf("OpenURL failed: %v", err)
	}
	text, err := actions.ReadPage(ctx)
	if err != nil {
		t.Fatalf("ReadPage failed: %v", err)
	}
	if !strings.Contains(text, "Hello from the attached Chrome") {
		t.Errorf("unexpected page text: %q", text)
	}

	// The clone drives a tab of its own: the tab the Chrome was started
	// with is still there, and it still shows about:blank.
	targets, err := client.Call(ctx, "Target.getTargets", nil)
	if err != nil {
		t.Fatalf("Target.getTargets failed: %v", err)
	}
	var list struct {
		TargetInfos []struct {
			Type string `json:"type"`
			URL  string `json:"url"`
		} `json:"targetInfos"`
	}
	if err := json.Unmarshal(targets, &list); err != nil {
		t.Fatalf("decode the target list: %v", err)
	}
	pages := 0
	blank := 0
	for _, ti := range list.TargetInfos {
		if ti.Type != "page" {
			continue
		}
		pages++
		if ti.URL == "about:blank" {
			blank++
		}
	}
	if pages < 2 {
		t.Errorf("the browser has %d page tabs, want the clone's own tab next to the one it was started with", pages)
	}
	if blank < 1 {
		t.Errorf("the tab the chrome was started with is gone: %+v", list.TargetInfos)
	}
}

// freePort returns a port nothing is listening on.
func freePort(t *testing.T) string {
	t.Helper()
	l, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("no free port: %v", err)
	}
	defer l.Close()
	_, port, err := net.SplitHostPort(l.Addr().String())
	if err != nil {
		t.Fatalf("no free port: %v", err)
	}
	return port
}
