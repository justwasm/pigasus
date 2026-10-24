package tuilib

import "os"

// EnvDisableEscapeSequenceOptimizations is the AGY_CLI_* spelling
// verbatim from the AGY 1.1.19 release note.
const EnvDisableEscapeSequenceOptimizations = "AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS"

// EscapeSequenceOptimizationsBypassed reports whether
// AGY_CLI_DISABLE_ESCAPE_SEQUENCE_OPTIMIZATIONS requests that
// the renderer skip dirty-rectangle and diffing optimizations.
func EscapeSequenceOptimizationsBypassed() bool {
	return ParseBoolish(os.Getenv(EnvDisableEscapeSequenceOptimizations))
}

// EscapeSequenceOptimizationsEnabled is the positive-form inverse
// of EscapeSequenceOptimizationsBypassed.
func EscapeSequenceOptimizationsEnabled() bool {
	return !EscapeSequenceOptimizationsBypassed()
}
