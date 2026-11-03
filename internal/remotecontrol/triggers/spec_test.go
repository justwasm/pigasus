// Package triggers — spec_test.go: the spec validator's own
// tests.
//
// The validator is the domain's fact: each trigger kind has a
// required + optional field set, and a missing key, a wrong type
// and a bad value are three different failures. The dispatcher's
// round-trip against the validator lives in
// internal/remotecontrol/triggers_test.go, because the dispatcher
// is the host's.
package triggers

import (
	"errors"
	"strings"
	"testing"
)

func TestValidateTriggerSpec_FileChanged_Ok(t *testing.T) {
	if err := ValidateSpec(FileChanged, Spec{
		"pattern": "*.go",
		"event":   "modify",
	}); err != nil {
		t.Fatalf("ValidateSpec: %v", err)
	}
}

// TestValidateTriggerSpec_FileChanged_Bad exercises
// the validator's "wrong event value" path: event
// must be one of create / modify / delete / rename.

func TestValidateTriggerSpec_FileChanged_Bad(t *testing.T) {
	cases := []struct {
		label string
		event string
	}{
		{"uppercase", "MODIFY"},
		{"unknown verb", "touch"},
		{"empty", ""},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			err := ValidateSpec(FileChanged, Spec{
				"pattern": "*.go",
				"event":   tc.event,
			})
			if err == nil {
				t.Fatalf("ValidateSpec: want error, got nil")
			}
			if !errors.Is(err, ErrInvalidSpec) {
				t.Errorf("ValidateSpec: error is not ErrInvalidSpec: %v", err)
			}
		})
	}
}

// TestValidateTriggerSpec_FileChanged_MissingPattern
// pins the "pattern key missing" path.

func TestValidateTriggerSpec_FileChanged_MissingPattern(t *testing.T) {
	err := ValidateSpec(FileChanged, Spec{
		"event": "modify",
	})
	if err == nil {
		t.Fatalf("ValidateSpec: want error, got nil")
	}
	if !errors.Is(err, ErrInvalidSpec) {
		t.Errorf("ValidateSpec: error is not ErrInvalidSpec: %v", err)
	}
	if !strings.Contains(err.Error(), "pattern") {
		t.Errorf("ValidateSpec: error message must mention 'pattern': %v", err)
	}
}

// TestValidateTriggerSpec_FileChanged_BadEvent pins
// the "event key missing" path.

func TestValidateTriggerSpec_FileChanged_BadEvent(t *testing.T) {
	err := ValidateSpec(FileChanged, Spec{
		"pattern": "*.go",
	})
	if err == nil {
		t.Fatalf("ValidateSpec: want error, got nil")
	}
	if !errors.Is(err, ErrInvalidSpec) {
		t.Errorf("ValidateSpec: error is not ErrInvalidSpec: %v", err)
	}
	if !strings.Contains(err.Error(), "event") {
		t.Errorf("ValidateSpec: error message must mention 'event': %v", err)
	}
}

// TestValidateTriggerSpec_Schedule_Ok pins the
// happy-path schedule spec: cron expression with a
// prompt.

func TestValidateTriggerSpec_Schedule_Ok(t *testing.T) {
	if err := ValidateSpec(Schedule, Spec{
		"prompt": "tick-tock",
		"cron":   "*/5 * * * *",
	}); err != nil {
		t.Fatalf("ValidateSpec(cron): %v", err)
	}
	if err := ValidateSpec(Schedule, Spec{
		"prompt": "tick-tock",
		"every":  "30m",
	}); err != nil {
		t.Fatalf("ValidateSpec(every): %v", err)
	}
}

// TestValidateTriggerSpec_Schedule_BadCron pins the
// "both cron AND every" → -32602 path. Exactly one
// must be set.

func TestValidateTriggerSpec_Schedule_BadCron(t *testing.T) {
	cases := []struct {
		label string
		spec  Spec
	}{
		{
			"both cron and every",
			Spec{"prompt": "p", "cron": "* * *", "every": "30m"},
		},
		{
			"neither cron nor every",
			Spec{"prompt": "p"},
		},
		{
			"missing prompt",
			Spec{"cron": "* * *"},
		},
		{
			"empty prompt",
			Spec{"prompt": "", "cron": "* * *"},
		},
		{
			"empty cron",
			Spec{"prompt": "p", "cron": ""},
		},
		{
			"empty every",
			Spec{"prompt": "p", "every": ""},
		},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			err := ValidateSpec(Schedule, tc.spec)
			if err == nil {
				t.Fatalf("ValidateSpec: want error, got nil")
			}
			if !errors.Is(err, ErrInvalidSpec) {
				t.Errorf("ValidateSpec: error is not ErrInvalidSpec: %v", err)
			}
		})
	}
}

// TestValidateTriggerSpec_Webhook_Ok pins the
// happy-path webhook spec: /path plus a prompt.
// Secret is optional.

func TestValidateTriggerSpec_Webhook_Ok(t *testing.T) {
	if err := ValidateSpec(Webhook, Spec{
		"path":   "/hook",
		"prompt": "do something",
	}); err != nil {
		t.Fatalf("ValidateSpec (no secret): %v", err)
	}
	if err := ValidateSpec(Webhook, Spec{
		"path":   "/hook",
		"prompt": "do something",
		"secret": "hex-of-anything",
	}); err != nil {
		t.Fatalf("ValidateSpec (with secret): %v", err)
	}
}

// TestValidateTriggerSpec_Webhook_Bad pins the
// validators' "missing path" and "wrong-type path"
// envelopes.

func TestValidateTriggerSpec_Webhook_Bad(t *testing.T) {
	cases := []struct {
		label string
		spec  Spec
		want  string
	}{
		{
			"missing path",
			Spec{"prompt": "p"},
			"path",
		},
		{
			"path missing leading slash",
			Spec{"path": "hook", "prompt": "p"},
			"/",
		},
		{
			"missing prompt",
			Spec{"path": "/hook"},
			"prompt",
		},
		{
			"empty prompt",
			Spec{"path": "/hook", "prompt": ""},
			"prompt",
		},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			err := ValidateSpec(Webhook, tc.spec)
			if err == nil {
				t.Fatalf("ValidateSpec: want error, got nil")
			}
			if !errors.Is(err, ErrInvalidSpec) {
				t.Errorf("error is not ErrInvalidSpec: %v", err)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("error %q must mention %q", err, tc.want)
			}
		})
	}
}

// TestValidateTriggerSpec_Manual_Ok pins the
// happy-path manual spec: a prompt alone is enough.

func TestValidateTriggerSpec_Manual_Ok(t *testing.T) {
	if err := ValidateSpec(Manual, Spec{
		"prompt": "do a thing",
	}); err != nil {
		t.Fatalf("ValidateSpec: %v", err)
	}
}

// TestValidateTriggerSpec_Manual_MissingPrompt pins
// the manual spec's "missing prompt" envelope.

func TestValidateTriggerSpec_Manual_MissingPrompt(t *testing.T) {
	cases := []struct {
		label string
		spec  Spec
	}{
		{"missing prompt", Spec{}},
		{"empty prompt", Spec{"prompt": ""}},
	}
	for _, tc := range cases {
		t.Run(tc.label, func(t *testing.T) {
			err := ValidateSpec(Manual, tc.spec)
			if err == nil {
				t.Fatalf("ValidateSpec: want error, got nil")
			}
			if !errors.Is(err, ErrInvalidSpec) {
				t.Errorf("error is not ErrInvalidSpec: %v", err)
			}
		})
	}
}

// TestValidateTriggerSpec_UnknownKind pins the
// "kind ∈ closed set" envelope.

func TestValidateTriggerSpec_UnknownKind(t *testing.T) {
	err := ValidateSpec(Kind("bogus"), Spec{"prompt": "p"})
	if err == nil {
		t.Fatalf("ValidateSpec: want error, got nil")
	}
	if !errors.Is(err, ErrInvalidSpec) {
		t.Errorf("error is not ErrInvalidSpec: %v", err)
	}
}
