package tuilib

import (
	"bytes"
	"strings"
	"testing"
)

// The `logo` command prints the seasonal banner art when it is asked
// for: `--seasonal` draws one scene the way the reference draws it at
// startup, `--scene` names one (or "all", or "random").
//
// 【一手】 agy 1.4.7 draws one of five scenes per launch, each with its
// own colours; the rasters are the reference's own truecolor output
// (out/changelog-147/ref/seasonal-scenes/).

func TestLogoSubcommand_SeasonalFlags(t *testing.T) {
	t.Setenv("AGY_CLI_LOGO_STYLE", "default")
	t.Run("--scene=pumpkin prints that scene", func(t *testing.T) {
		out := captureLogo(t, []string{"--scene=pumpkin"})
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(lines) != 7 {
			t.Fatalf("printed %d lines, want 7:\n%s", len(lines), out)
		}
		if !strings.Contains(out, "\x1b[38;2;") {
			t.Errorf("the art must carry 24-bit colour, got %q", out)
		}
	})

	t.Run("--scene is case-insensitive", func(t *testing.T) {
		lower := captureLogo(t, []string{"--scene=witch-hat"})
		upper := captureLogo(t, []string{"--scene=WITCH-HAT"})
		if lower != upper {
			t.Fatal("--scene=WITCH-HAT must print the same scene as --scene=witch-hat")
		}
	})

	t.Run("--seasonal prints one scene", func(t *testing.T) {
		out := captureLogo(t, []string{"--seasonal"})
		lines := strings.Split(strings.TrimRight(out, "\n"), "\n")
		if len(lines) != 7 {
			t.Fatalf("printed %d lines, want 7:\n%s", len(lines), out)
		}
	})

	t.Run("--scene=all prints every scene", func(t *testing.T) {
		out := captureLogo(t, []string{"--scene=all"})
		for _, name := range []string{"pumpkin", "moon", "ghost", "witch-hat", "bat"} {
			if !strings.Contains(out, name+"\n") {
				t.Errorf("the output carries no caption for %q", name)
			}
		}
	})

	t.Run("--scene=bogus returns an error", func(t *testing.T) {
		var buf bytes.Buffer
		err := RunLogoSubcommand(&buf, []string{"--scene=bogus"})
		if err == nil {
			t.Fatal("expected an error for --scene=bogus, got nil")
		}
		if !strings.Contains(err.Error(), "bogus") {
			t.Errorf("the error does not echo the bad value: %v", err)
		}
	})

	t.Run("the plain logo is untouched", func(t *testing.T) {
		// The seasonal flags must not change the default output.
		out := captureLogo(t, nil)
		if !strings.Contains(out, "▀") {
			t.Errorf("the plain logo must still print its blocks, got %q", out)
		}
	})
}

func TestLogoSubcommand_HelpListsTheSeasonalFlags(t *testing.T) {
	out := captureLogo(t, []string{"--help"})
	for _, want := range []string{"--seasonal", "--scene"} {
		if !strings.Contains(out, want) {
			t.Errorf("the usage block misses %q:\n%s", want, out)
		}
	}
}
