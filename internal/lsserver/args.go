// Package lsserver — args.go: the reference's argument-vector rules.
//
// The reference's parser is not Go's `flag` package. Three of its
// rules are observable and differ from Go's:
//
//   - an unknown flag is reported in the plural and every unknown
//     flag is named at once ("flags provided but not defined: -a
//     -b"); Go names the first one and stops;
//   - a value that does not parse keeps the strconv error ("invalid
//     value \"x\" for flag -n: strconv.ParseInt: parsing \"x\":
//     invalid syntax"); Go replaces it with "parse error";
//   - the help exits 2; Go exits 0.
//
// The rules below were all observed against the reference binary on
// this machine (out/langserver/flags.txt).
package lsserver

import (
	"flag"
	"fmt"
	"strconv"
	"strings"
)

// ParseErrorLine is the reference's line behind a flag-parsing
// complaint.
//
// The reference prints it after the complaint and the usage; the help
// path does not print it.
const ParseErrorLine = "error during flag parsing, exiting cleanly with status 2..."

// Args is what the reference's parser made of the argument vector.
type Args struct {
	// Help is true when the arguments asked for the help.
	Help bool
	// Pattern is the argument that followed --help: the reference
	// prints the help of the flags whose name contains it.
	Pattern string
	// Complaint is the reference's complaint about the arguments. It
	// is empty when there is none.
	Complaint string
	// Hidden holds the values of the reference's hidden flags.
	Hidden map[string]string
	// Rest is the argument vector without the hidden flags — what the
	// visible flag set parses.
	Rest []string
}

// InspectArgs reads the argument vector the way the reference's
// parser does, without parsing it.
//
// The scan stops at the first argument that is not a flag, and at
// "--" — like Go's `flag` package does. A flag that does not exist
// is collected and the collection is reported once the scan is over,
// so a value that does not parse (reported at once) wins over it.
//
// The reference's hidden flags take part in the same scan: their
// complaints are reported at their position, exactly like a visible
// flag's, and a hidden flag behind a positional argument is never
// seen, because the scan stopped there.
//
// 【一手】 every rule below was observed against the reference binary
// (out/langserver/flags.txt).
func InspectArgs(fs *flag.FlagSet, args []string) Args {
	s := &scanner{fs: fs}
	s.run(args)
	return s.out
}

// scanner walks the argument vector.
type scanner struct {
	fs      *flag.FlagSet
	out     Args
	unknown []string
	rest    []string
}

// run walks the vector and fills the scanner's result.
//
// The visible flags are copied into `rest` as they are met; the hidden
// ones are not, because the visible flag set does not know them.
func (s *scanner) run(args []string) {
	s.out.Hidden = map[string]string{}
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" || len(arg) < 2 || !strings.HasPrefix(arg, "-") {
			s.rest = append(s.rest, args[i:]...)
			break
		}
		name, value, hasValue := splitFlagArg(arg)
		if name == "help" {
			s.out.Help = true
			if i+1 < len(args) {
				s.out.Pattern = args[i+1]
			}
			break
		}
		if hidden, ok := lookupHidden(name); ok {
			next, complaint := s.takeHidden(hidden, args, i, name, value, hasValue)
			if complaint != "" {
				s.out.Complaint = complaint
				break
			}
			i = next
			continue
		}
		next, complaint := s.takeVisible(args, i, name, value, hasValue)
		if complaint != "" {
			s.out.Complaint = complaint
			break
		}
		i = next
	}
	s.out.Rest = s.rest
	// The help wins over the unknown flags: the reference prints the
	// help and never names them.
	if s.out.Complaint == "" && !s.out.Help && len(s.unknown) > 0 {
		s.out.Complaint = UnknownFlagsMessage(s.unknown)
	}
}

// takeVisible handles a flag of the visible flag set.
//
// It returns the index of the last argument it consumed.
func (s *scanner) takeVisible(args []string, i int, name, value string, hasValue bool) (int, string) {
	f := s.fs.Lookup(name)
	if f == nil {
		s.unknown = append(s.unknown, name)
		s.rest = append(s.rest, args[i])
		return i, ""
	}
	s.rest = append(s.rest, args[i])
	if hasValue {
		return i, checkValue(f, name, value)
	}
	if isBoolFlag(f) {
		return i, ""
	}
	if i+1 >= len(args) {
		return i, "flag needs an argument: -" + name
	}
	s.rest = append(s.rest, args[i+1])
	return i + 1, checkValue(f, name, args[i+1])
}

// takeHidden handles one of the reference's hidden flags.
//
// A boolean flag takes no value: `--name` is true and `--name=false`
// is false, and a value that does not parse is the reference's
// complaint. A string flag takes the next argument when its value was
// not attached with an equals sign.
//
// It returns the index of the last argument it consumed.
func (s *scanner) takeHidden(hidden hiddenFlag, args []string, i int, name, value string, hasValue bool) (int, string) {
	if !hasValue && !hidden.boolFlag {
		if i+1 >= len(args) {
			return i, "flag needs an argument: -" + name
		}
		value = args[i+1]
		s.out.Hidden[name] = value
		return i + 1, ""
	}
	if !hasValue {
		s.out.Hidden[name] = "true"
		return i, ""
	}
	if hidden.boolFlag {
		if _, err := strconv.ParseBool(value); err != nil {
			return i, fmt.Sprintf("invalid boolean value %q for  -%s: %v", value, name, err)
		}
	}
	s.out.Hidden[name] = value
	return i, ""
}

// splitFlagArg splits a flag argument into its name and its value.
//
// Both spellings Go's `flag` package accepts are accepted here: one
// dash and two. The value is empty when the argument carried none.
func splitFlagArg(arg string) (name, value string, hasValue bool) {
	body := strings.TrimPrefix(arg, "-")
	body = strings.TrimPrefix(body, "-")
	if idx := strings.IndexByte(body, '='); idx >= 0 {
		return body[:idx], body[idx+1:], true
	}
	return body, "", false
}

// isBoolFlag reports whether a flag takes no value.
func isBoolFlag(f *flag.Flag) bool {
	getter, ok := f.Value.(flag.Getter)
	if !ok {
		return false
	}
	_, isBool := getter.Get().(bool)
	return isBool
}

// checkValue validates a flag's value the way the reference's parser
// does.
//
// The reference keeps the strconv error in its complaint and its
// boolean complaint carries a second space after "for".
func checkValue(f *flag.Flag, name, value string) string {
	getter, ok := f.Value.(flag.Getter)
	if !ok {
		return ""
	}
	switch getter.Get().(type) {
	case bool:
		if _, err := strconv.ParseBool(value); err != nil {
			return fmt.Sprintf("invalid boolean value %q for  -%s: %v", value, name, err)
		}
	case int:
		if _, err := strconv.ParseInt(value, 0, strconv.IntSize); err != nil {
			return fmt.Sprintf("invalid value %q for flag -%s: %v", value, name, err)
		}
	}
	return ""
}

// UnknownFlagsMessage is the reference's complaint about a flag that
// does not exist.
//
// The reference names every unknown flag it found, in the order they
// appeared.
func UnknownFlagsMessage(unknown []string) string {
	names := make([]string, 0, len(unknown))
	for _, name := range unknown {
		names = append(names, "-"+name)
	}
	return "flags provided but not defined: " + strings.Join(names, " ")
}
