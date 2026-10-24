package tuilib_test

import (
	"testing"

	"github.com/justwasm/pigasus/internal/tuilib"
)

func TestEscapeSequenceOptimizations_Default(t *testing.T) {
	t.Setenv(tuilib.EnvDisableEscapeSequenceOptimizations, "")
	if !tuilib.EscapeSequenceOptimizationsEnabled() {
		t.Fatal("expected optimizations enabled by default")
	}
	if tuilib.EscapeSequenceOptimizationsBypassed() {
		t.Fatal("expected optimizations not bypassed by default")
	}
}

func TestEscapeSequenceOptimizations_Truthy(t *testing.T) {
	for _, val := range []string{"1", "true", "yes", "TRUE", "Yes"} {
		t.Run(val, func(t *testing.T) {
			t.Setenv(tuilib.EnvDisableEscapeSequenceOptimizations, val)
			if !tuilib.EscapeSequenceOptimizationsBypassed() {
				t.Fatalf("expected bypassed for %q", val)
			}
			if tuilib.EscapeSequenceOptimizationsEnabled() {
				t.Fatalf("expected disabled for %q", val)
			}
		})
	}
}

func TestEscapeSequenceOptimizations_Falsy(t *testing.T) {
	for _, val := range []string{"0", "false", "no", "off", "garbage"} {
		t.Run(val, func(t *testing.T) {
			t.Setenv(tuilib.EnvDisableEscapeSequenceOptimizations, val)
			if tuilib.EscapeSequenceOptimizationsBypassed() {
				t.Fatalf("expected not bypassed for %q", val)
			}
			if !tuilib.EscapeSequenceOptimizationsEnabled() {
				t.Fatalf("expected enabled for %q", val)
			}
		})
	}
}
