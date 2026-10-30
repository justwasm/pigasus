// Package cliflags holds the shared flag.FlagSet accessors.
//
// Four packages grew their own copies of "was this flag set?", "what is
// this flag's value?" and "split this comma-separated flag?" over time:
// cmd/wagyu, internal/printmode, internal/rchost and
// internal/workspacetrust. They are not identical — printmode and rchost
// read through flag.Getter, cmd/wagyu reads the raw String() — so this
// package keeps both behaviours under distinct names rather than silently
// picking one.
package flags

import (
	"flag"
	"strconv"
	"strings"
)

// WasSet reports whether the named flag was explicitly passed on the
// command line. It reads the FlagSet's visited set, so a flag left at its
// default value is not reported.
func WasSet(fs *flag.FlagSet, name string) bool {
	if fs == nil {
		return false
	}
	var set bool
	fs.Visit(func(f *flag.Flag) {
		if f.Name == name {
			set = true
		}
	})
	return set
}

// BoolValue returns the bool value of a flag, or false if unset.
//
// It compares the raw String() form, so a custom flag.Value whose String()
// is not "true"/"false" reads as false.
func BoolValue(fs *flag.FlagSet, name string) bool {
	if fs == nil {
		return false
	}
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	return f.Value.String() == "true"
}

// GetterBoolValue returns the bool value of a flag through its
// flag.Getter, or false if the flag is unset or does not expose a bool.
//
// Unlike BoolValue it reads the parsed value rather than the rendered
// string, so a custom flag.Value that renders itself differently still
// reports its real state.
func GetterBoolValue(fs *flag.FlagSet, name string) bool {
	if fs == nil {
		return false
	}
	f := fs.Lookup(name)
	if f == nil {
		return false
	}
	getter, ok := f.Value.(flag.Getter)
	if !ok {
		return false
	}
	v, ok := getter.Get().(bool)
	return ok && v
}

// IntValue returns the int value of a flag, or 0 if unset or not an int.
func IntValue(fs *flag.FlagSet, name string) int {
	if fs == nil {
		return 0
	}
	f := fs.Lookup(name)
	if f == nil {
		return 0
	}
	v, err := strconv.Atoi(f.Value.String())
	if err != nil {
		return 0
	}
	return v
}

// StringValue returns the string value of a flag, or "" if unset.
func StringValue(fs *flag.FlagSet, name string) string {
	if fs == nil {
		return ""
	}
	f := fs.Lookup(name)
	if f == nil {
		return ""
	}
	return f.Value.String()
}

// GetterStringValue returns the string value of a flag through its
// flag.Getter, or "" if the flag is unset or does not expose a string.
//
// Unlike StringValue it reads the parsed value rather than the rendered
// string, so a custom flag.Value that renders itself differently still
// reports its real state.
func GetterStringValue(fs *flag.FlagSet, name string) string {
	if fs == nil {
		return ""
	}
	f := fs.Lookup(name)
	if f == nil {
		return ""
	}
	getter, ok := f.Value.(flag.Getter)
	if !ok {
		return ""
	}
	v, ok := getter.Get().(string)
	if !ok {
		return ""
	}
	return v
}

// StringValues returns the per-occurrence values of a comma-separated flag.
// Blank entries are dropped and surrounding whitespace is trimmed.
func StringValues(fs *flag.FlagSet, name string) []string {
	if fs == nil {
		return nil
	}
	f := fs.Lookup(name)
	if f == nil {
		return nil
	}
	if getter, ok := f.Value.(flag.Getter); ok {
		if ss, ok := getter.Get().([]string); ok {
			if len(ss) == 0 {
				return nil
			}
			var out []string
			for _, item := range ss {
				parts := strings.Split(item, ",")
				for _, p := range parts {
					if p = strings.TrimSpace(p); p != "" && p != "[]" {
						out = append(out, p)
					}
				}
			}
			return out
		}
	}
	raw := f.Value.String()
	if raw == "" || raw == "[]" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" && p != "[]" {
			out = append(out, p)
		}
	}
	return out
}

// GetterStringValues returns the per-occurrence values of a
// comma-separated flag whose value is read through flag.Getter.
//
// It differs from StringValues in the source it reads: a flag.Value that
// is not a Getter renders an empty string here, so the result is nil.
func GetterStringValues(fs *flag.FlagSet, name string) []string {
	if fs == nil {
		return nil
	}
	f := fs.Lookup(name)
	if f == nil {
		return nil
	}
	getter, ok := f.Value.(flag.Getter)
	if !ok {
		return nil
	}
	if ss, ok := getter.Get().([]string); ok {
		if len(ss) == 0 {
			return nil
		}
		var out []string
		for _, item := range ss {
			parts := strings.Split(item, ",")
			for _, p := range parts {
				if p = strings.TrimSpace(p); p != "" && p != "[]" {
					out = append(out, p)
				}
			}
		}
		return out
	}
	v, ok := getter.Get().(string)
	if !ok || v == "" || v == "[]" {
		return nil
	}
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" && p != "[]" {
			out = append(out, p)
		}
	}
	return out
}

// ResolveBoolAlias reads the canonical flag name first, falling back to
// alias. It is how the CLI accepts both "--continue" and "-c".
func ResolveBoolAlias(fs *flag.FlagSet, canonical, alias string) bool {
	if fs == nil {
		return false
	}
	if BoolValue(fs, canonical) {
		return true
	}
	if alias != "" && BoolValue(fs, alias) {
		return true
	}
	return false
}

// ResolveBoolAliasGetter is ResolveBoolAlias for flag sets whose values
// are read through flag.Getter.
func ResolveBoolAliasGetter(fs *flag.FlagSet, canonical, alias string) bool {
	if fs == nil {
		return false
	}
	if GetterBoolValue(fs, canonical) {
		return true
	}
	if alias != "" && GetterBoolValue(fs, alias) {
		return true
	}
	return false
}
