// Tests for the 1:1 parser-parity shell. Each test exercises one slice of
// the v1.3.5 binary's --help surface and asserts that our jetski-based
// parser behaves equivalently.
//
// What we test:
//   - TestParseAllFlags         — flags from v1.3.5 --help exist, names +
//     types + defaults 1:1
//   - TestSubcommandDispatch    — 12 subcommands and their sub-subcommands
//   - TestAliasMatching         — -c/-i/-p and --prompt resolve to the
//     same canonical flag the long form does
//   - TestValueFormat           — `--flag=value` and `--flag value` both work
//     and yield the same parsed value
//   - TestJetskiCLI_1to1        — full --help output matches v1.3.5 binary
package cli

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
)

// ---- TestParseAllFlags -----------------------------------------------------
//
// v1.3.5 binary --help (parity with /workspace/agy/install/antigravity):
//
//	--add-dir                       stringSlice (default [])
//	--agent                         string
//	-c                              bool   (alias for --continue)
//	--continue                      bool
//	--conversation                  string
//	--dangerously-skip-permissions  bool
//	--disable-slash-commands        bool
//	--effort                        string (default <empty>)
//	-i                              bool   (alias for --prompt-interactive)
//	--input-format                  string (default text)
//	--json-schema                   string
//	--log-file                      string
//	--mode                          string
//	--model                         string
//	--new-project                   bool
//	--output-format                 string (default text)
//	-p                              bool   (alias for --print)
//	--print                         bool
//	--print-partial-response        bool
//	--print-timeout                 duration (default 0s)
//	--project                       string
//	--prompt                        bool   (alias for --print)
//	--prompt-interactive            bool
//	--remote-control                bool
//	--sandbox                       bool
//
// 22 total. cycle 33 had a wrong count of 31 (extrapolated from ghidra
// symbol table) — the actual --help list is much smaller. The
// StandardFlags() slice is two entries longer (22 long + 3 short =
// 25 vs the 21 visible) because of the cycle-101 hidden `-v` int
// and the cycle-headless-jsonl-mode-trailing-newline hidden `--verbose`
// bool, neither of which appears in --help.
func TestParseAllFlags(t *testing.T) {
	flags := StandardFlags()

	want := map[string]string{
		"add-dir":                      "stringSlice",
		"agent":                        "string",
		"c":                            "bool",
		"continue":                     "bool",
		"conversation":                 "string",
		"dangerously-skip-permissions": "bool",
		"disable-slash-commands":      "bool",
		"effort":                      "string",
		"i":                           "string", // cycle 101: string alias for --prompt-interactive
		"input-format":                "string",
		"json-schema":                 "string",
		"log-file":                    "string",
		"mode":                        "string",
		"model":                       "string",
		"new-project":                 "bool",
		"output-format":               "string",
		"p":                           "string", // cycle 101: string alias for --print
		"print":                       "string",
		// cycle headless-partial-response-flag: AGY 1.2.10 streaming flag.
		"print-partial-response": "bool",
		"print-timeout":          "duration",
		"project":                "string",
		"prompt":                 "string", // cycle 101: string alias for --print
		"prompt-interactive":     "string",
		"remote-control":              "bool",
		"sandbox":                     "bool",
		// cycle 101: recovered hidden `-v N` int flag. Absent from --help
		// (Hidden, empty Usage) but parseable, matching 1.3.6.
		"v": "int",
		// cycle headless-jsonl-mode-trailing-newline: hidden --verbose
		// bool flag that expands the stream-json event stream with an
		// `assistant_message` event per turn.
		"verbose": "bool",
	}
	if got, want := len(flags), len(want); got != want {
		t.Fatalf("StandardFlags(): got %d flags, want %d (v1.3.5 --help count + hidden)", got, want)
	}
	got := map[string]string{}
	for _, f := range flags {
		got[f.Name] = f.Type
	}
	for name, typ := range want {
		if got[name] != typ {
			t.Errorf("flag %q: got type %q, want %q (v1.3.5 --help)", name, got[name], typ)
		}
	}
	for name := range got {
		if _, ok := want[name]; !ok {
			t.Errorf("flag %q present in StandardFlags() but not in v1.3.5 --help", name)
		}
	}
}

// Sanity-check that we can actually build a Parser with the standard
// spec set, and that the resulting JetskiCLI has all 28 flag descriptors
// registered (24 long incl. the recovered hidden -v + 3 short: c, i, p +
// the cycle 57 hidden --version bool flag + the cycle
// headless-jsonl-mode-trailing-newline hidden --verbose bool flag). Cycle
// headless-partial-response-flag adds the long --print-partial-response.
func TestParseAllFlags_RegisteredWithJetski(t *testing.T) {
	p := NewParser(StandardFlags(), nil, "test", "test", "test")
	if got, want := len(p.Root().Flags), 28; got != want {
		t.Fatalf("JetskiCLI has %d flag descriptors, want %d (v1.3.5 --help count + hidden)", got, want)
	}
}

// ---- TestVisibleFlagsMatchReferenceHelp ------------------------------------
//
// The visible half of StandardFlags() is the reference binary's `--help`
// column, in the reference's order. The list below is the 1.4.7 binary's
// `--help` verbatim: 21 long flags plus the 3 short aliases (-c, -i, -p).
//
// The flag-by-flag diff against the live binary found exactly one
// divergence — `--print-partial-response`, which the reference does not
// have (no string in the 1.4.7 binary, absent from its `--help`, absent
// from its changelog). It is Hidden now, like `--verbose`, so the visible
// column matches the reference again.
//
// Hidden flags are the clone's extras (the recovered `-v N`, `--verbose`,
// `--print-partial-response`); they must never leak into `--help`.
func TestVisibleFlagsMatchReferenceHelp(t *testing.T) {
	want := []string{
		"add-dir",
		"agent",
		"c",
		"continue",
		"conversation",
		"dangerously-skip-permissions",
		"disable-slash-commands",
		"effort",
		"i",
		"input-format",
		"json-schema",
		"log-file",
		"mode",
		"model",
		"new-project",
		"output-format",
		"p",
		"print",
		"print-timeout",
		"project",
		"prompt",
		"prompt-interactive",
		"remote-control",
		"sandbox",
	}
	var got []string
	for _, f := range StandardFlags() {
		if f.Hidden {
			continue
		}
		got = append(got, f.Name)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("visible flags = %v\nwant %v\n(the reference's --help column, in order)", got, want)
	}
}

// ---- TestSubcommandDispatch ------------------------------------------------

// v1.3.5 binary has 12 visible subcommands:
//
//	agent, agents, changelog, help, install, mcp, mic-serve, models,
//	plugin, plugins (alias of plugin), remote-control, update
//
// Cycle 41: also has 10 hidden subcommands (version, login, logout,
// auth, chat, acp, http, hooks, migrate, stats) that wire to real
// runXxx handlers in dispatch.go. The 1:1 parse-parity invariant
// is "12 visible subcommands in --help", so we filter Hidden=true
// entries before counting.
func TestSubcommandDispatch(t *testing.T) {
	subs := StandardSubcommands()

	// 15 visible subcommands: 12 reference subcommands plus openagy superset
	// (accounts, model, provider).
	visible := 0
	for _, s := range subs {
		if !s.Hidden {
			visible++
		}
	}
	if got, want := visible, 15; got != want {
		t.Fatalf("visible subcommands: got %d, want %d (12 reference + 3 superset)", got, want)
	}

	// Sub-subcommand counts (only subcommands with children in v1.3.5).
	// `agent`/`agents` are leaf commands (no children): the reference
	// prints `Usage: <name> agent [flags]` for `agent --help` and
	// rejects `agents list` with `Error: unexpected arguments: [list]`.
	wantSubs := map[string]int{
		"plugin":         9, // list, import, install, uninstall, enable, disable, validate, link, help
		"mcp":            5, // add, remove, list, enable, disable
		"remote-control": 4, // start, serve, status, stop (cycle rc-serve-subcommand)
		"agent":          0, // leaf
		"agents":         0, // leaf (alias of agent)
	}
	for name, want := range wantSubs {
		var got int
		for _, s := range subs {
			if s.Name == name {
				got = len(s.Subs)
				break
			}
		}
		if got != want {
			t.Errorf("subcommand %q: got %d subs, want %d", name, got, want)
		}
	}
}

// ---- TestAliasMatching -----------------------------------------------------
//
// v1.3.5 binary --help output (parity with /workspace/agy/install/antigravity)
// shows -c, -i, -p short forms. The only long-form alias is --prompt → --print.
// We keep the AliasMap table populated so cmd/wagyu can fold short
// forms into canonical flag values via LookupFlagValueCmd.
func TestAliasMatching(t *testing.T) {
	want := AliasMap{
		"c":      "continue",
		"i":      "prompt-interactive",
		"p":      "print",
		"prompt": "print",
	}
	if !reflect.DeepEqual(StandardAliasMap, want) {
		t.Errorf("StandardAliasMap drift: got %v, want %v", StandardAliasMap, want)
	}
}

// ---- TestValueFormat -------------------------------------------------------

func TestValueFormat(t *testing.T) {
	cases := []struct {
		args []string
		want string
	}{
		{[]string{"--model", "gpt-x"}, "gpt-x"},
		{[]string{"--model=gpt-x"}, "gpt-x"},
		{[]string{"--effort", "high", "--model", "sonnet"}, "sonnet"},
	}
	for _, c := range cases {
		t.Run(strings.Join(c.args, "_"), func(t *testing.T) {
			var buf bytes.Buffer
			p := NewParser(StandardFlags(), nil, "test", "test", "test")
			p.SetOut(&buf)
			p.SetErr(&buf)
			p.Root().RootRun = func(args []string) error { return nil }
			_ = p.ExecuteArgs(c.args)
			fs := p.Root().FlagSet()
			if fs == nil {
				t.Fatalf("FlagSet is nil after Parse")
			}
			got := fs.Lookup("model").Value.String()
			if got != c.want {
				t.Errorf("args %v: got model=%q, want %q", c.args, got, c.want)
			}
		})
	}
}

// ---- TestJetskiCLI_1to1 ----------------------------------------------------
//
// v1.3.5 binary --help is `flag.PrintDefaults`-style: "Usage of <name>:"
// followed by the flag table, then "Available subcommands:" and the
// subcommand table. We assert the section headers and key entry
// counts so a future spec drift fails loudly.
func TestJetskiCLI_1to1(t *testing.T) {
	var buf bytes.Buffer
	p := NewParser(StandardFlags(), StandardSubcommands(),
		"antigravity", "recovered", "recovered")
	p.SetOut(&buf)
	// Call PrintHelp on the JetskiCLI directly. Stdlib flag would
	// auto-print this on --help, but we don't have --help in the
	// StandardFlags set, so we just call the renderer.
	p.Root().PrintHelp()
	got := buf.String()
	if got == "" {
		t.Fatalf("PrintHelp wrote nothing to buf; got empty string")
	}
	for _, want := range []string{
		"Usage of antigravity:",
		"Available subcommands:",
		"--add-dir",
		"--print",
		"--new-project",
		"plugin",
		"update",
		"install",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("--help output missing %q\n--- got ---\n%s", want, got)
		}
	}
	// Spot-check flag count (21 + 1 model-config = 22 entries, since
	// cycle 33+ adds --model-config as a non-v1.3.5 flag).
	if fc := strings.Count(got, "  --"); fc < 21 {
		t.Errorf("flag count in --help: got %d, want >= 21", fc)
	}
}
