package shell

import (
	"testing"
	"time"
)

func TestNextCronTime_EveryDescriptor(t *testing.T) {
	from := time.Date(2026, 11, 4, 10, 0, 0, 0, time.UTC)
	got, err := NextCronTime("@every 15s", from)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := from.Add(15 * time.Second)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextCronTime_Step(t *testing.T) {
	from := time.Date(2026, 11, 4, 10, 2, 30, 0, time.UTC)
	got, err := NextCronTime("*/5 * * * *", from)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 11, 4, 10, 5, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextCronTime_Hourly(t *testing.T) {
	from := time.Date(2026, 11, 4, 10, 15, 0, 0, time.UTC)
	got, err := NextCronTime("@hourly", from)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 11, 4, 11, 0, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextCronTime_Daily(t *testing.T) {
	from := time.Date(2026, 11, 4, 10, 15, 0, 0, time.UTC)
	got, err := NextCronTime("30 4 * * *", from)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	want := time.Date(2026, 11, 5, 4, 30, 0, 0, time.UTC)
	if !got.Equal(want) {
		t.Errorf("got %v, want %v", got, want)
	}
}

func TestNextCronTime_Invalid(t *testing.T) {
	from := time.Now()
	if _, err := NextCronTime("", from); err == nil {
		t.Errorf("expected error for empty cron expression")
	}
	if _, err := NextCronTime("invalid", from); err == nil {
		t.Errorf("expected error for invalid cron expression")
	}
}
