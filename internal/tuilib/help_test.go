package tuilib

import (
	"strings"
	"testing"
)

func TestHelpListWindowFor(t *testing.T) {
	cases := []struct{ h, want int }{
		{13, 4}, {14, 5}, {15, 6}, {16, 7}, {17, 8}, {18, 9},
		{20, 11}, {22, 12}, {24, 14}, {26, 15}, {28, 17}, {30, 18},
		{32, 20}, {33, 20}, {34, 21}, {36, 23}, {38, 24}, {40, 26},
		{50, 33}, {60, 41},
	}
	for _, c := range cases {
		if got := HelpListWindowFor(c.h); got != c.want {
			t.Errorf("HelpListWindowFor(%d) = %d, want %d", c.h, got, c.want)
		}
	}
}

func TestHelpListWindowForUnknownHeight(t *testing.T) {
	for _, h := range []int{0, -1} {
		if got := HelpListWindowFor(h); got != 18 {
			t.Errorf("HelpListWindowFor(%d) = %d, want 18", h, got)
		}
	}
	if got := HelpListWindowFor(1); got < 1 {
		t.Errorf("HelpListWindowFor(1) = %d, want >= 1", got)
	}
}

func TestHelpListVisible(t *testing.T) {
	cases := []struct{ total, window, size, want int }{
		{99, 0, 18, 18},
		{99, 90, 18, 9},
		{99, 99, 18, 0},
		{27, 0, 18, 18},
		{27, 18, 18, 9},
		{0, 0, 18, 0},
		{5, 0, 18, 5},
		{99, 0, 0, 1},
	}
	for _, c := range cases {
		if got := HelpListVisible(c.total, c.window, c.size); got != c.want {
			t.Errorf("HelpListVisible(%d, %d, %d) = %d, want %d", c.total, c.window, c.size, got, c.want)
		}
	}
}

func TestBuildShortcutRow(t *testing.T) {
	row := HelpShortcuts[0]
	res := BuildShortcutRow(row, true)
	if !strings.HasPrefix(res, HelpBlueOpen+"> /") {
		t.Errorf("highlighted shortcut row prefix = %q", res)
	}
	resNorm := BuildShortcutRow(row, false)
	if !strings.HasPrefix(resNorm, "  "+HelpBlueOpen+"/") {
		t.Errorf("normal shortcut row prefix = %q", resNorm)
	}
}

func TestBuildCommandRow(t *testing.T) {
	cmd := SlashCommand{
		Name:        "/help",
		Aliases:     []string{"?"},
		Description: "Show help commands",
	}
	row := BuildCommandRow(cmd, true, 80)
	if !strings.Contains(row, "/help (?)") {
		t.Errorf("command row missing name and alias: %q", row)
	}
}
