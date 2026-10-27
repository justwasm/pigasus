package agytemplates

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// templateDigests pins the recovered templates byte for byte.
//
// The files under templates/ are the reference's, taken out of the agy 1.4.7
// binary (see templates/PROVENANCE.md). They are the *source* of the prompt
// and of the step strings now, so an accidental edit — a whitespace fix, a
// reformat, a "typo fix" — would silently change what the clone sends. The
// digests make that a test failure instead.
var templateDigests = map[string]string{
	"helpers/errors.tmpl": "5d37ed29f71d8da88f2eea9a1ebf5db8cbee22be4bde63506f1887f7da0ff0ea",
	"step_strings/helpers/default_status_output_string.tmpl": "c9d1a48159f15ad0ebbac54129db898fcc8276ade3765e8ca3bd8bc29c560504",
	"step_strings/run_command.tmpl":                          "f9ded495eef28b0a90a50b02c2023724f4acb0cdfc9f5511ed63947b594513bc",
	"step_strings/send_command_input.tmpl":                   "704a1b6454a8b3d988ea62f0ac5aae50e0b5dc11b6e652d81844dea2880379b9",
	"system_prompts/artifacts.tmpl":                          "9a294feb3115fe931cfeafe5b19aa181d061868928ee9e636fafe15885f04009",
	"system_prompts/communication_style.tmpl":                "3c72250feec1f1ade822a29f28fc1519a67c241b82f3bc8dff25a8c8617294c1",
	"system_prompts/conversation_transcript.tmpl":            "9c977fd7da47dde426c3f0f09b52ca4e62feea5cd621e99fcaa40c5a6470d7a4",
	"system_prompts/ephemeral_message.tmpl":                  "d4ced5dc63c1faad5adf01169cb1e52fd986d39fa2649f1e8a854b016eaa0ec4",
	"system_prompts/function_call.tmpl":                      "959cde80246a35b7042b42ddc2cfa55616aa7d60bf9bb8d6d2b3b7b26810ce58",
	"system_prompts/guidelines.tmpl":                         "8b71f78b140f7d9707e54007383121cb6d3f3d5f32fdcebeb2092043c731ef9a",
	"system_prompts/helpers/async_termination_rule.tmpl":     "1314701504f2b452dee278583104b7e588ca094e05ae49d7f5af26314f1c1722",
	"system_prompts/identity.tmpl":                           "a28e035ae6e1197db070be8e5a86361273d1a81824404191dc6c0351251b73b1",
	"system_prompts/persistent_context.tmpl":                 "9d707bc641421bf7bfeef7c9b81b40d61a8c6dee3ffc93c049f0c73b04b39ff5",
	"system_prompts/plugins.tmpl":                            "8f76c5269140ee4b891f390b54d4d596925e4a390267ebb447a18c40e6de3402",
	"system_prompts/skills.tmpl":                             "018695e12d99290bc5806555e4294f643384f6e92f912560e2e5fb7546530fac",
	"system_prompts/slash_commands.tmpl":                     "c7a5f3bd2aa115ef09823dfa72d63cac350d408529ae3e1bb7ce1ac1d685b220",
	"system_prompts/test_funcmap.tmpl":                       "07623bcaa7fb48a5a9ca99db3fc87b7080b845a280cc61227977bccceae2559c",
}

func TestRecoveredTemplatesAreUnmodified(t *testing.T) {
	found := map[string]bool{}
	for _, rel := range Paths() {
		found[rel] = true
		body, err := Read(rel)
		if err != nil {
			t.Fatal(err)
		}
		sum := sha256.Sum256(body)
		want, ok := templateDigests[rel]
		if !ok {
			t.Errorf("%s is not in templateDigests", rel)
			continue
		}
		if got := hex.EncodeToString(sum[:]); got != want {
			t.Errorf("%s changed: sha256 %s, want %s", rel, got, want)
		}
	}
	for rel := range templateDigests {
		if !found[rel] {
			t.Errorf("%s is missing from the embedded bundle", rel)
		}
	}
}

// TestBundleIsTheReferences checks the bundle's shape: seventeen files, the
// seven the prompt uses and the two step strings.
func TestBundleIsTheReferences(t *testing.T) {
	paths := Paths()
	if len(paths) != 17 {
		t.Fatalf("%d templates in the bundle, want 17: %v", len(paths), paths)
	}
	for _, want := range []string{
		"step_strings/run_command.tmpl",
		"step_strings/send_command_input.tmpl",
		"system_prompts/identity.tmpl",
	} {
		if !contains(paths, want) {
			t.Errorf("%s is missing from the bundle", want)
		}
	}
}

// TestEveryTemplateParses is a smoke test over the whole bundle: a template
// that does not parse would blow up at render time, in whichever consumer
// happened to touch it first.
func TestEveryTemplateParses(t *testing.T) {
	set := NewSet(nil)
	for _, rel := range Paths() {
		if set.Lookup(rel) == nil {
			t.Errorf("%s did not parse into the set", rel)
		}
	}
}

// TestStepStringsShareTheHelperSet checks the cross-file definition the
// step-string templates rely on: run_command.tmpl and
// send_command_input.tmpl both call "defaultStatusOutputString", which is
// defined in step_strings/helpers/. A set that carried only one of the two
// would fail at render time, not at parse time.
func TestStepStringsShareTheHelperSet(t *testing.T) {
	set := NewSet(nil)
	for _, name := range []string{"defaultStatusOutputString", "templateError"} {
		if set.Lookup(name) == nil {
			t.Errorf("%s is not defined in the shared set", name)
		}
	}
}

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
