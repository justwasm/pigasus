package cliflags

import (
	"flag"
	"testing"
	"time"
)

// newFlagSet builds a flag set with the flags the accessors are exercised
// against.
func newFlagSet() *flag.FlagSet {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Bool("verbose", false, "")
	fs.Bool("legacy", false, "")
	fs.String("model", "", "")
	fs.String("effort", "high", "")
	fs.Int("count", 0, "")
	fs.String("add-dir", "", "")
	fs.Duration("timeout", 0, "")
	return fs
}

// TestWasSet pins the "was the flag explicitly passed?" semantics: only a
// flag the user actually named counts.
func TestWasSet(t *testing.T) {
	fs := newFlagSet()
	if WasSet(fs, "verbose") {
		t.Error("an untouched flag reported as set")
	}
	if err := fs.Parse([]string{"--verbose"}); err != nil {
		t.Fatal(err)
	}
	if !WasSet(fs, "verbose") {
		t.Error("a passed flag reported as unset")
	}
	if WasSet(fs, "model") {
		t.Error("an untouched flag reported as set after parsing another")
	}
	if WasSet(nil, "verbose") {
		t.Error("a nil FlagSet reported a flag as set")
	}
}

// TestBoolValue pins the raw-String bool read.
func TestBoolValue(t *testing.T) {
	fs := newFlagSet()
	if BoolValue(fs, "verbose") {
		t.Error("an unset bool flag read as true")
	}
	if err := fs.Parse([]string{"--verbose"}); err != nil {
		t.Fatal(err)
	}
	if !BoolValue(fs, "verbose") {
		t.Error("a set bool flag read as false")
	}
	if BoolValue(fs, "missing") {
		t.Error("an unknown flag read as true")
	}
	if BoolValue(nil, "verbose") {
		t.Error("a nil FlagSet read as true")
	}
}

// TestGetterBoolValue pins the flag.Getter bool read, including the
// custom flag.Value case the raw read cannot see.
func TestGetterBoolValue(t *testing.T) {
	fs := newFlagSet()
	if GetterBoolValue(fs, "verbose") {
		t.Error("an unset bool flag read as true")
	}
	if err := fs.Parse([]string{"--verbose"}); err != nil {
		t.Fatal(err)
	}
	if !GetterBoolValue(fs, "verbose") {
		t.Error("a set bool flag read as false")
	}
	if GetterBoolValue(fs, "missing") {
		t.Error("an unknown flag read as true")
	}
	if GetterBoolValue(nil, "verbose") {
		t.Error("a nil FlagSet read as true")
	}
}

// TestIntValue pins the int read and its two failure modes.
func TestIntValue(t *testing.T) {
	fs := newFlagSet()
	if got := IntValue(fs, "count"); got != 0 {
		t.Errorf("IntValue(count) = %d, want 0", got)
	}
	if err := fs.Parse([]string{"--count", "7"}); err != nil {
		t.Fatal(err)
	}
	if got := IntValue(fs, "count"); got != 7 {
		t.Errorf("IntValue(count) = %d, want 7", got)
	}
	if got := IntValue(fs, "model"); got != 0 {
		t.Errorf("IntValue(model) = %d, want 0 for a non-int flag", got)
	}
	if got := IntValue(fs, "missing"); got != 0 {
		t.Errorf("IntValue(missing) = %d, want 0", got)
	}
	if got := IntValue(nil, "count"); got != 0 {
		t.Errorf("IntValue(nil) = %d, want 0", got)
	}
}

// TestStringValue pins the raw string read.
func TestStringValue(t *testing.T) {
	fs := newFlagSet()
	if got := StringValue(fs, "model"); got != "" {
		t.Errorf("StringValue(model) = %q, want empty", got)
	}
	if err := fs.Parse([]string{"--model", "gemini"}); err != nil {
		t.Fatal(err)
	}
	if got := StringValue(fs, "model"); got != "gemini" {
		t.Errorf("StringValue(model) = %q, want %q", got, "gemini")
	}
	if got := StringValue(fs, "missing"); got != "" {
		t.Errorf("StringValue(missing) = %q, want empty", got)
	}
	if got := StringValue(nil, "model"); got != "" {
		t.Errorf("StringValue(nil) = %q, want empty", got)
	}
}

// TestGetterStringValue pins the flag.Getter string read.
func TestGetterStringValue(t *testing.T) {
	fs := newFlagSet()
	if err := fs.Parse([]string{"--model", "gemini"}); err != nil {
		t.Fatal(err)
	}
	if got := GetterStringValue(fs, "model"); got != "gemini" {
		t.Errorf("GetterStringValue(model) = %q, want %q", got, "gemini")
	}
	if got := GetterStringValue(fs, "missing"); got != "" {
		t.Errorf("GetterStringValue(missing) = %q, want empty", got)
	}
	if got := GetterStringValue(nil, "model"); got != "" {
		t.Errorf("GetterStringValue(nil) = %q, want empty", got)
	}
}

// TestStringValues pins the comma-separated split: blank entries are
// dropped and the surrounding whitespace is trimmed.
func TestStringValues(t *testing.T) {
	fs := newFlagSet()
	if got := StringValues(fs, "add-dir"); got != nil {
		t.Errorf("StringValues(add-dir) = %v, want nil for an unset flag", got)
	}
	if err := fs.Parse([]string{"--add-dir", "a, b ,c,"}); err != nil {
		t.Fatal(err)
	}
	got := StringValues(fs, "add-dir")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("StringValues(add-dir) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("StringValues(add-dir)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

// TestGetterStringValues pins the Getter-based split: a flag.Value that
// is not a Getter renders an empty string, so the result is nil — which is
// what the remote-control host's --add-dir slice flag sees.
func TestGetterStringValues(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Var(&customDuration{}, "timeout", "")
	fs.String("add-dir", "", "")
	if got := GetterStringValues(fs, "add-dir"); got != nil {
		t.Errorf("GetterStringValues(add-dir) = %v, want nil for an unset flag", got)
	}
	if err := fs.Parse([]string{"--add-dir", "a, b ,c,"}); err != nil {
		t.Fatal(err)
	}
	got := GetterStringValues(fs, "add-dir")
	want := []string{"a", "b", "c"}
	if len(got) != len(want) {
		t.Fatalf("GetterStringValues(add-dir) = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("GetterStringValues(add-dir)[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// A non-Getter flag.Value reads as empty, so the split yields nothing.
	if got := GetterStringValues(fs, "timeout"); got != nil {
		t.Errorf("GetterStringValues(timeout) = %v, want nil for a non-Getter value", got)
	}
}

// TestResolveBoolAlias pins the canonical-then-alias lookup.
func TestResolveBoolAlias(t *testing.T) {
	fs := newFlagSet()
	if ResolveBoolAlias(fs, "verbose", "legacy") {
		t.Error("no flag set still resolved to true")
	}
	if err := fs.Parse([]string{"--legacy"}); err != nil {
		t.Fatal(err)
	}
	if !ResolveBoolAlias(fs, "verbose", "legacy") {
		t.Error("the alias flag did not resolve to true")
	}
	fs2 := newFlagSet()
	if err := fs2.Parse([]string{"--verbose"}); err != nil {
		t.Fatal(err)
	}
	if !ResolveBoolAlias(fs2, "verbose", "legacy") {
		t.Error("the canonical flag did not resolve to true")
	}
	if ResolveBoolAlias(fs2, "missing", "") {
		t.Error("an unknown flag resolved to true")
	}
	if ResolveBoolAlias(nil, "verbose", "legacy") {
		t.Error("a nil FlagSet resolved to true")
	}
}

// TestResolveBoolAliasGetter pins the flag.Getter variant of the alias
// lookup.
func TestResolveBoolAliasGetter(t *testing.T) {
	fs := newFlagSet()
	if err := fs.Parse([]string{"--legacy"}); err != nil {
		t.Fatal(err)
	}
	if !ResolveBoolAliasGetter(fs, "verbose", "legacy") {
		t.Error("the alias flag did not resolve to true")
	}
	if ResolveBoolAliasGetter(fs, "missing", "") {
		t.Error("an unknown flag resolved to true")
	}
	if ResolveBoolAliasGetter(nil, "verbose", "legacy") {
		t.Error("a nil FlagSet resolved to true")
	}
}

// customDuration is a flag.Value that is not a flag.Getter — the case
// GetterBoolValue must reject while StringValue still reads it.
type customDuration struct{ d time.Duration }

func (c *customDuration) String() string { return c.d.String() }
func (c *customDuration) Set(s string) error {
	d, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	c.d = d
	return nil
}

// TestCustomValueWithoutGetter pins that a non-Getter flag.Value reads as
// its rendered string and as "not a bool" through the Getter accessor.
func TestCustomValueWithoutGetter(t *testing.T) {
	fs := flag.NewFlagSet("test", flag.ContinueOnError)
	fs.Var(&customDuration{}, "timeout", "")
	if err := fs.Parse([]string{"--timeout", "90s"}); err != nil {
		t.Fatal(err)
	}
	if got := StringValue(fs, "timeout"); got != "1m30s" {
		t.Errorf("StringValue(timeout) = %q, want %q", got, "1m30s")
	}
	if GetterBoolValue(fs, "timeout") {
		t.Error("a non-Getter flag.Value read as a bool")
	}
	if GetterStringValue(fs, "timeout") != "" {
		t.Error("a non-Getter flag.Value read as a string")
	}
}
