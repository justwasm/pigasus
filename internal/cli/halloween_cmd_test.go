package cli

import (
	"strings"
	"testing"
)

// The `halloween` command is the clone's own: it prints the seasonal
// banner art, the five scenes agy 1.4.7 draws at startup.
//
// 【一手】 The art is the reference's own truecolor output, decoded from
// the captures under out/changelog-147/ref/seasonal-scenes/.

func TestHalloween_PrintsEveryScene(t *testing.T) {
	out, errb, code := runJetski(t, "halloween")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb)
	}
	if errb != "" {
		t.Errorf("stderr = %q, want empty", errb)
	}
	for _, name := range []string{"pumpkin", "moon", "ghost", "witch-hat", "bat"} {
		if !strings.Contains(out, name+"\n") {
			t.Errorf("the output carries no caption for %q", name)
		}
	}
	if !strings.Contains(out, "\x1b[38;2;") {
		t.Errorf("the art must carry 24-bit colour, got %q", out)
	}
}

func TestHalloween_PrintsOneScene(t *testing.T) {
	out, errb, code := runJetski(t, "halloween", "pumpkin")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 7 {
		t.Fatalf("printed %d lines, want 7:\n%s", len(lines), out)
	}
	if strings.Contains(out, "pumpkin") {
		t.Error("one scene must be printed without a caption")
	}
}

func TestHalloween_RandomScene(t *testing.T) {
	out, errb, code := runJetski(t, "halloween", "random")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb)
	}
	lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
	if len(lines) != 7 {
		t.Fatalf("printed %d lines, want 7:\n%s", len(lines), out)
	}
}

func TestHalloween_UnknownScene(t *testing.T) {
	out, errb, code := runJetski(t, "halloween", "bogus")
	if code != 1 {
		t.Errorf("exit code = %d, want 1", code)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	if !strings.Contains(errb, "unknown scene") || !strings.Contains(errb, "bogus") {
		t.Errorf("stderr = %q, want an unknown-scene error", errb)
	}
}

func TestHalloween_Help(t *testing.T) {
	out, errb, code := runJetski(t, "halloween", "--help")
	if code != 0 {
		t.Fatalf("exit code = %d, want 0 (stderr: %q)", code, errb)
	}
	if out != "" {
		t.Errorf("stdout = %q, want empty", out)
	}
	for _, want := range []string{"Usage: antigravity halloween", "witch-hat", "random"} {
		if !strings.Contains(errb, want) {
			t.Errorf("the usage block misses %q:\n%s", want, errb)
		}
	}
}
