package actor_test

import (
	"testing"
	"time"

	"github.com/justwasm/pigasus/actor"
)

func TestNextCronTime_Descriptors(t *testing.T) {
	base := time.Date(2026, 11, 4, 10, 15, 30, 0, time.UTC)

	tests := []struct {
		expr     string
		expected time.Time
	}{
		{
			expr:     "@hourly",
			expected: time.Date(2026, 11, 4, 11, 0, 0, 0, time.UTC),
		},
		{
			expr:     "@daily",
			expected: time.Date(2026, 11, 5, 0, 0, 0, 0, time.UTC),
		},
		{
			expr:     "@weekly",
			// 2026-11-04 is Wednesday; next Sunday is Nov 8
			expected: time.Date(2026, 11, 8, 0, 0, 0, 0, time.UTC),
		},
		{
			expr:     "@monthly",
			expected: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			expr:     "@yearly",
			expected: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.expr, func(t *testing.T) {
			got, err := actor.NextCronTime(tt.expr, base)
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.expr, err)
			}
			if !got.Equal(tt.expected) {
				t.Errorf("NextCronTime(%q) = %v, want %v", tt.expr, got, tt.expected)
			}
		})
	}
}

func TestNextCronTime_Every(t *testing.T) {
	base := time.Date(2026, 11, 4, 10, 15, 0, 0, time.UTC)

	got, err := actor.NextCronTime("@every 30s", base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := base.Add(30 * time.Second)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Invalid @every
	_, err = actor.NextCronTime("@every -5s", base)
	if err == nil {
		t.Error("expected error for negative duration")
	}
}

func TestNextCronTime_StandardFields(t *testing.T) {
	base := time.Date(2026, 11, 4, 10, 15, 0, 0, time.UTC)

	// Every 15 minutes: "*/15 * * * *" -> next should be 10:30
	got, err := actor.NextCronTime("*/15 * * * *", base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 11, 4, 10, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}

	// Specific time: "0 12 * * *" -> 12:00
	got, err = actor.NextCronTime("0 12 * * *", base)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want = time.Date(2026, 11, 4, 12, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextCronTime_Invalid(t *testing.T) {
	base := time.Now()
	_, err := actor.NextCronTime("", base)
	if err == nil {
		t.Error("expected error for empty string")
	}

	_, err = actor.NextCronTime("* * *", base)
	if err == nil {
		t.Error("expected error for non-5 field expr")
	}
}
