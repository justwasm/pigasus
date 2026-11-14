package agytemplates

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"
)

// templateDigests pins the recovered templates byte for byte.
//
// The files under templates/ are the reference's, taken out of the agy 1.5.5
// binary (see templates/PROVENANCE.md). They are the *source* of the prompt
// and of the step strings now, so an accidental edit — a whitespace fix, a
// reformat, a "typo fix" — would silently change what the clone sends. The
// digests make that a test failure instead.
var templateDigests = map[string]string{
	"helpers/errors.tmpl":                                    "5d37ed29f71d8da88f2eea9a1ebf5db8cbee22be4bde63506f1887f7da0ff0ea",
	"step_strings/helpers/default_status_output_string.tmpl": "c9d1a48159f15ad0ebbac54129db898fcc8276ade3765e8ca3bd8bc29c560504",
	"step_strings/run_command.tmpl":                          "4edd693408313c291eff37e3554b7a05e3a77107a97686632c54fd560ee6755f",
	"step_strings/send_command_input.tmpl":                   "704a1b6454a8b3d988ea62f0ac5aae50e0b5dc11b6e652d81844dea2880379b9",
	"system_prompts/artifacts.tmpl":                          "515f4acac086242608c40b753b5161ec0825f2c21bb782b68e12fea283fc51a6",
	"system_prompts/communication_style.tmpl":                "96e2f45838771003bbfb05206623bb6be4b64187eaa97a4e4d1f2d9e9de86c8f",
	"system_prompts/conversation_transcript.tmpl":            "9c977fd7da47dde426c3f0f09b52ca4e62feea5cd621e99fcaa40c5a6470d7a4",
	"system_prompts/ephemeral_message.tmpl":                  "d4ced5dc63c1faad5adf01169cb1e52fd986d39fa2649f1e8a854b016eaa0ec4",
	"system_prompts/function_call.tmpl":                      "959cde80246a35b7042b42ddc2cfa55616aa7d60bf9bb8d6d2b3b7b26810ce58",
	"system_prompts/guidelines.tmpl":                         "f4ede326caaa79f7454af6dac251cf0436ab8375c1b06eade975bcaf85a7c439",
	"system_prompts/helpers/async_termination_rule.tmpl":     "1314701504f2b452dee278583104b7e588ca094e05ae49d7f5af26314f1c1722",
	"system_prompts/identity.tmpl":                           "bd8957e589c97f9fa5d5fe3077b01c49b0329f0987bd58cf73bab123ea0a199b",
	"system_prompts/persistent_context.tmpl":                 "9d707bc641421bf7bfeef7c9b81b40d61a8c6dee3ffc93c049f0c73b04b39ff5",
	"system_prompts/planning_mode_artifacts.tmpl":            "6a65b6a2769d01e8928d4b3aa5326438b0109933d073ff1bd4b93b1301c2235b",
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
	if len(paths) != 18 {
		t.Fatalf("%d templates in the bundle, want 18: %v", len(paths), paths)
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
