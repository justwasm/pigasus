// jetski_state.go — flag registration and value adapter state for the jetski CLI engine.
package cli

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
	"time"
)

// registerFlags binds all registered JetskiFlags onto the given flag.FlagSet.
func (j *JetskiCLI) registerFlags(fs *flag.FlagSet) {
	for _, f := range j.Flags {
		dest, _ := newDestFor(f.Type, f.Def)
		f.Dest = dest
		j.bindFlag(fs, f.Name, dest, f.Usage)
		if f.Short != "" && f.Short != f.Name {
			j.bindFlag(fs, f.Short, dest, "alias for --"+f.Name)
		}
	}
}

// bindFlag registers a single flag target with fs.
func (j *JetskiCLI) bindFlag(fs *flag.FlagSet, name string, dest any, usage string) {
	switch v := dest.(type) {
	case *bool:
		fs.BoolVar(v, name, *v, usage)
	case *string:
		fs.StringVar(v, name, *v, usage)
	case *int:
		fs.IntVar(v, name, *v, usage)
	case *time.Duration:
		fs.Var(&durationValue{v: v}, name, usage)
	default:
		fs.Var(v.(flag.Value), name, usage)
	}
}

// newDestFor returns a fresh destination for a flag of the given type.
// It accepts the string form of the default and converts as needed.
func newDestFor(typ, def string) (any, error) {
	switch typ {
	case "bool":
		v := def == "true" || def == "1"
		return &v, nil
	case "int":
		// Cycle 101: the recovered hidden `-v` flag parses its value with
		// strconv.Atoi, so a bad value reads
		// `invalid value "foo" for flag -v: strconv.Atoi: parsing "foo": invalid syntax`
		// (Go's own IntVar would say "parse error"). Register a flag.Value
		// that calls strconv.Atoi directly.
		n := 0
		if def != "" {
			if _, err := strconv.Atoi(def); err != nil {
				return nil, fmt.Errorf("jetski: bad int default %q: %w", def, err)
			}
		}
		return intAtoiValue{&n}, nil
	case "duration":
		if def == "" {
			def = "0s"
		}
		d, err := time.ParseDuration(def)
		if err != nil {
			return nil, fmt.Errorf("jetski: bad duration default %q: %w", def, err)
		}
		return &d, nil
	case "stringSlice":
		ss := &stringSliceValue{}
		if def != "" {
			for _, p := range strings.Split(def, ",") {
				ss.items = append(ss.items, p)
			}
		}
		return ss, nil
	default: // "string"
		v := def
		return &v, nil
	}
}

// flagBoolValue is the *bool pointer exposed by stdlib flag when
// registered with BoolVar. We re-export it under a stable name so
// version detection in Parse stays readable.
type flagBoolValue = bool

// getBoolFlag returns *bool if the named flag exists and was registered
// as a bool, else nil.
func getBoolFlag(fs *flag.FlagSet, name string) *bool {
	if fs == nil {
		return nil
	}
	f := fs.Lookup(name)
	if f == nil {
		return nil
	}
	if s := f.Value.String(); s == "true" || s == "false" {
		v := s == "true"
		return &v
	}
	return nil
}

// firstSetFlag returns the first of names that appeared on the command line,
// or "" if none did. Cycle 101: print-mode prompt flags are string flags now,
// so getBoolFlag no longer applies and presence must be tested via Visit.
func firstSetFlag(fs *flag.FlagSet, names ...string) string {
	if fs == nil {
		return ""
	}
	set := map[string]bool{}
	fs.Visit(func(f *flag.Flag) { set[f.Name] = true })
	for _, n := range names {
		if set[n] {
			return n
		}
	}
	return ""
}

// flagStrValue returns the string form of a flag's current value.
func flagStrValue(fs *flag.FlagSet, name string) string {
	if fs == nil {
		return ""
	}
	f := fs.Lookup(name)
	if f == nil {
		return ""
	}
	return f.Value.String()
}

// isKnownFlagToken reports whether v looks like a flag this CLI registers —
// "-c", "--effort", "-v=2". Cycle 101: the reference rejects a print prompt
// that is really a flag ("-p took \"-c\" as its prompt") but accepts an
// unregistered dash token ("-x", "--zzz", "--") as the prompt.
func isKnownFlagToken(fs *flag.FlagSet, v string) bool {
	if fs == nil || !strings.HasPrefix(v, "-") {
		return false
	}
	name := strings.TrimLeft(v, "-")
	if i := strings.IndexByte(name, '='); i >= 0 {
		name = name[:i]
	}
	if name == "" {
		return false
	}
	return fs.Lookup(name) != nil
}

// flagDisplayName renders a registered flag name the way the user would type
// it: single letters get one dash, everything else two.
func flagDisplayName(name string) string {
	if len(name) == 1 {
		return "-" + name
	}
	return "--" + name
}

// intAtoiValue is a flag.Value for the recovered hidden `-v` flag. The
// reference parses it with strconv.Atoi, so the stdlib flag error reads
// `invalid value "foo" for flag -v: strconv.Atoi: parsing "foo": invalid syntax`
// (Go's own IntVar would say "parse error").
type intAtoiValue struct{ p *int }

func (v intAtoiValue) String() string {
	if v.p == nil {
		return "0"
	}
	return strconv.Itoa(*v.p)
}

func (v intAtoiValue) Set(s string) error {
	n, err := strconv.Atoi(s)
	if err != nil {
		return err
	}
	*v.p = n
	return nil
}

// durationValue is the stdlib duration flag with one difference: Set returns
// time.ParseDuration's own error instead of collapsing it to the generic
// "parse error" that flag's own durationValue substitutes. The reference
// reports the underlying error verbatim:
//
//	invalid value "abc" for flag -print-timeout: time: invalid duration "abc"
//
// (stdlib would say `…: parse error`). --print-timeout is the only duration
// flag today, but the wrapper is generic.
type durationValue struct{ v *time.Duration }

func (d durationValue) String() string {
	if d.v == nil {
		return "0s"
	}
	return d.v.String()
}

func (d durationValue) Set(s string) error {
	v, err := time.ParseDuration(s)
	if err != nil {
		return err
	}
	*d.v = v
	return nil
}

// stringSliceValue is a flag.Value implementation that accumulates
// repeated -tag invocations into a slice.
type stringSliceValue struct{ items []string }

func (ss *stringSliceValue) String() string     { return strings.Join(ss.items, ",") }
func (ss *stringSliceValue) Set(v string) error { ss.items = append(ss.items, v); return nil }
