package tuilib

import (
	"testing"
)

func TestParseBoolish(t *testing.T) {
	cases := []struct {
		in   string
		want bool
	}{
		{"1", true},
		{"true", true},
		{"yes", true},
		{"TRUE", true},
		{"Yes", true},
		{" 1 ", true},
		{" true ", true},
		{"0", false},
		{"false", false},
		{"no", false},
		{"on", false},
		{"off", false},
		{"enable", false},
		{"disable", false},
		{"y", false},
		{"n", false},
		{"", false},
		{"garbage", false},
	}
	for _, tc := range cases {
		if got := ParseBoolish(tc.in); got != tc.want {
			t.Errorf("ParseBoolish(%q) = %v, want %v", tc.in, got, tc.want)
		}
	}
}

func TestEnvHideLogoOn(t *testing.T) {
	t.Setenv(EnvHideLogo, "1")
	if !EnvHideLogoOn() {
		t.Errorf("EnvHideLogoOn() with 1 = false, want true")
	}

	t.Setenv(EnvHideLogo, "0")
	if EnvHideLogoOn() {
		t.Errorf("EnvHideLogoOn() with 0 = true, want false")
	}

	t.Setenv(EnvHideLogo, "")
	if EnvHideLogoOn() {
		t.Errorf("EnvHideLogoOn() with empty = true, want false")
	}
}
