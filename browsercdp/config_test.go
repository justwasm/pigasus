package browsercdp

import (
	"os"
	"path/filepath"
	"testing"
)

// TestConfigure_NormalisesThePort pins that a port of zero or less falls
// back to the reference's own default (`--cdp_port`, default 9222).
func TestConfigure_NormalisesThePort(t *testing.T) {
	t.Cleanup(func() { Configure(DefaultConfig()) })

	Configure(Config{Port: 0, Headless: true})
	if got := CurrentConfig().Port; got != DefaultCDPPort {
		t.Errorf("Port = %d, want %d", got, DefaultCDPPort)
	}
	Configure(Config{Port: -1})
	if got := CurrentConfig().Port; got != DefaultCDPPort {
		t.Errorf("Port = %d, want %d", got, DefaultCDPPort)
	}
	Configure(Config{Port: 9333})
	if got := CurrentConfig().Port; got != 9333 {
		t.Errorf("Port = %d, want 9333", got)
	}
}

// TestLocalChromePorts_Order pins the order the clone looks for a local
// Chrome in: the port the DevToolsActivePort file names, then the
// environment override, then the configured port.
func TestLocalChromePorts_Order(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("CHROME_REMOTE_DEBUGGING_PORT", "9444")

	// The discovery reads the standard Chrome profile dirs, which hang
	// off the home directory.
	dir := filepath.Join(home, "Library", "Application Support", "Google", "Chrome")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "DevToolsActivePort"), []byte("9555\n/devtools/browser/abc\n"), 0o644); err != nil {
		t.Fatalf("write the DevToolsActivePort file: %v", err)
	}

	ports := localChromePorts(Config{Port: 9222})
	want := []string{"9555", "9444", "9222"}
	if len(ports) != len(want) {
		t.Fatalf("ports = %v, want %v", ports, want)
	}
	for i, p := range want {
		if ports[i] != p {
			t.Fatalf("ports = %v, want %v", ports, want)
		}
	}
}

// TestLocalChromePorts_NoDuplicates pins that the same port is not
// looked at twice.
func TestLocalChromePorts_NoDuplicates(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("CHROME_REMOTE_DEBUGGING_PORT", "9222")
	ports := localChromePorts(Config{Port: 9222})
	if len(ports) != 1 || ports[0] != "9222" {
		t.Errorf("ports = %v, want [9222]", ports)
	}
}

// TestAttachRequested pins when the clone drives a Chrome that is
// already running: with the reference's --use_local_chrome, or with the
// environment override that names a running browser's port.
func TestAttachRequested(t *testing.T) {
	t.Setenv("CHROME_REMOTE_DEBUGGING_PORT", "")
	if attachRequested(Config{}) {
		t.Errorf("attachRequested = true, want false (the default starts the clone's own Chrome)")
	}
	if attachRequested(Config{UseLocalChrome: true}) != true {
		t.Errorf("attachRequested = false, want true (--use_local_chrome)")
	}
	t.Setenv("CHROME_REMOTE_DEBUGGING_PORT", "9333")
	if attachRequested(Config{}) != true {
		t.Errorf("attachRequested = false, want true (CHROME_REMOTE_DEBUGGING_PORT)")
	}
}

// TestDevToolsActivePort pins the file the clone reads to learn the port
// Chrome chose.
func TestDevToolsActivePort(t *testing.T) {
	dir := t.TempDir()
	if got := devToolsActivePort(dir); got != "" {
		t.Errorf("devToolsActivePort = %q, want an empty string (no file)", got)
	}
	if err := os.WriteFile(filepath.Join(dir, "DevToolsActivePort"), []byte("12345\n"), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	if got := devToolsActivePort(dir); got != "12345" {
		t.Errorf("devToolsActivePort = %q, want 12345", got)
	}
}
