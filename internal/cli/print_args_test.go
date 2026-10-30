// Tests for the cycle 101 print-mode argument semantics.
//
// Reference behaviour (verified against the 1.3.6 binary):
//
//	agy -p                          → "flag needs an argument: -p" + usage, rc 2
//	agy -p=""                       → "error: Error: empty prompt. Usage: agy --print \"your prompt here\"" rc 1
//	agy -p hi extra                 → "Error: unexpected argument \"extra\"." + second line, rc 2
//	agy hi                          → same unexpected-argument error (no prompt flag set)
//	agy -p --agent nosuchagent hi   → "Error: -p took \"--agent\" as its prompt, …", rc 2
//	agy -p -x                       → accepted; -x is the prompt
//	agy -p /help -i bye             → "error: --print and --prompt-interactive are mutually exclusive", rc 2
//	agy --input-format bogus -p /x  → "Error: unknown --input-format \"bogus\" (want text or stream-json)", rc 2
//	agy -v foo                      → "invalid value \"foo\" for flag -v: strconv.Atoi: …", rc 2
package cli

import (
	"strings"
	"testing"
)

func TestPrintModeArgs_1to1(t *testing.T) {
	for _, tc := range []struct {
		name    string
		args    []string
		wantErr string
		wantRC  int
	}{
		{
			name:    "positional_prompt_rejected",
			args:    []string{"hi"},
			wantErr: "Error: unexpected argument \"hi\".\nPrompts are read only from -p/--print, -i/--prompt-interactive, or stdin, so this argument would have been ignored.\n",
			wantRC:  2,
		},
		{
			name:    "extra_positional_after_prompt",
			args:    []string{"-p", "hi", "extra"},
			wantErr: "Error: unexpected argument \"extra\".\nPrompts are read only from -p/--print, -i/--prompt-interactive, or stdin, so this argument would have been ignored.\n",
			wantRC:  2,
		},
		{
			name:    "flag_shaped_prompt_long",
			args:    []string{"-p", "--agent", "nosuchagent", "hi"},
			wantErr: "Error: -p took \"--agent\" as its prompt, so the intended prompt was left as an argument and ignored.\nAttach the prompt to the flag (-p='your prompt') and move --agent elsewhere on the command line.\n",
			wantRC:  2,
		},
		{
			name:    "flag_shaped_prompt_short",
			args:    []string{"-p", "-c"},
			wantErr: "Error: -p took \"-c\" as its prompt, so the intended prompt was left as an argument and ignored.\nAttach the prompt to the flag (-p='your prompt') and move -c elsewhere on the command line.\n",
			wantRC:  2,
		},
		{
			name:    "long_form_flag_shaped_prompt",
			args:    []string{"--print", "--prompt-interactive"},
			wantErr: "Error: --print took \"--prompt-interactive\" as its prompt, so the intended prompt was left as an argument and ignored.\nAttach the prompt to the flag (--print='your prompt') and move --prompt-interactive elsewhere on the command line.\n",
			wantRC:  2,
		},
		{
			name:    "empty_prompt",
			args:    []string{"-p="},
			wantErr: "error: Error: empty prompt. Usage: antigravity --print \"your prompt here\"\n",
			wantRC:  1,
		},
		{
			name:    "mutually_exclusive",
			args:    []string{"-p", "/help", "-i", "bye"},
			wantErr: "error: --print and --prompt-interactive are mutually exclusive\n",
			wantRC:  2,
		},
		{
			name:    "unknown_input_format",
			args:    []string{"-p", "/help", "--input-format", "bogus"},
			wantErr: "Error: unknown --input-format \"bogus\" (want text or stream-json)\n",
			wantRC:  2,
		},
		{
			name:    "flag_needs_argument",
			args:    []string{"-p"},
			wantErr: "flag needs an argument: -p\n",
			wantRC:  2,
		},
		{
			name:    "hidden_v_requires_int",
			args:    []string{"-v", "foo"},
			wantErr: "invalid value \"foo\" for flag -v: strconv.Atoi: parsing \"foo\": invalid syntax\n",
			wantRC:  2,
		},
		{
			name:    "input_format_stream_json_requires_output_format",
			args:    []string{"-p", "hi", "--input-format", "stream-json"},
			wantErr: "Error: --input-format stream-json requires --output-format stream-json\n",
			wantRC:  2,
		},
		{
			name:    "settings_unknown_key",
			args:    []string{"--settings", "{\"bogus\": 1}"},
			wantErr: "Error: --settings: unknown setting \"bogus\"\n",
			wantRC:  1,
		},
		{
			name:    "settings_file_missing",
			args:    []string{"--settings", "nonexistent_file"},
			wantErr: "Error: reading --settings file: open nonexistent_file: no such file or directory\n",
			wantRC:  1,
		},
		{
			name:    "json_schema_without_output_format",
			args:    []string{"--json-schema", "{}"},
			wantErr: "Error: --json-schema can only be used when --output-format is 'json' or 'stream-json'\n",
			wantRC:  1,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			_, errb, code := runJetski(t, tc.args...)
			if !strings.HasPrefix(errb, tc.wantErr) {
				t.Errorf("stderr =\n%q\nwant prefix\n%q", errb, tc.wantErr)
			}
			if code != tc.wantRC {
				t.Errorf("exit code = %d, want %d", code, tc.wantRC)
			}
		})
	}
}

// TestPrintModeArgs_Accepted pins the cases that must NOT be rejected: an
// unregistered dash token is a legitimate prompt (`-p -x`, `-p --zzz`,
// `-p --`), and a prompt flag after the prompt is parsed (`-p /clear
// --disable-slash-commands` no longer trips the slash rejection).
func TestPrintModeArgs_Accepted(t *testing.T) {
	for _, args := range [][]string{
		{"-p", "-x"},
		{"-p", "--zzz"},
		{"-p", "--"},
		{"-p", "/clear", "--disable-slash-commands"},
	} {
		_, errb, code := runJetski(t, args...)
		if strings.Contains(errb, "unexpected argument") || strings.Contains(errb, "took") {
			t.Errorf("args %v: unexpectedly rejected: %q", args, errb)
		}
		if code == 2 && strings.Contains(errb, "flag needs an argument") {
			t.Errorf("args %v: flag parse failed: %q", args, errb)
		}
	}
}

// TestPrintFlagIsString pins the cycle 101 type change: -p/--print/--prompt and
// -i/--prompt-interactive carry the prompt as their value.
func TestPrintFlagIsString(t *testing.T) {
	for _, f := range StandardFlags() {
		switch f.Name {
		case "p", "print", "prompt", "i", "prompt-interactive":
			if f.Type != "string" {
				t.Errorf("flag %q type = %q, want string", f.Name, f.Type)
			}
		case "v":
			if f.Type != "int" || !f.Hidden {
				t.Errorf("flag v = %q hidden=%v, want int hidden", f.Type, f.Hidden)
			}
		}
	}
}
