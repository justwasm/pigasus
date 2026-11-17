package render

import (
	"testing"
	"time"
)

func TestWheelSpeed(t *testing.T) {
	var tracker WheelTracker
	start := time.Now()

	// Initial single tick: speed is 1
	speed := WheelSpeed(&tracker, start)
	if speed != 1 {
		t.Fatalf("first tick speed = %d, want 1", speed)
	}
	if tracker.Streak != 0 {
		t.Fatalf("first tick streak = %d, want 0", tracker.Streak)
	}

	// Another tick after 200ms (>150ms): streak resets, speed is still 1
	speed = WheelSpeed(&tracker, start.Add(200*time.Millisecond))
	if speed != 1 {
		t.Fatalf("tick after 200ms speed = %d, want 1", speed)
	}
	if tracker.Streak != 0 {
		t.Fatalf("tick after 200ms streak = %d, want 0", tracker.Streak)
	}

	// Rapid ticks (<150ms): accelerates
	// tick 1 (streak 1): sqrt(1)+1 = 2
	speed = WheelSpeed(&tracker, start.Add(250*time.Millisecond))
	if speed != 2 {
		t.Fatalf("rapid tick 1 speed = %d, want 2", speed)
	}
	if tracker.Streak != 1 {
		t.Fatalf("rapid tick 1 streak = %d, want 1", tracker.Streak)
	}

	// tick 2 (streak 2): sqrt(2)+1 = 2
	speed = WheelSpeed(&tracker, start.Add(280*time.Millisecond))
	if speed != 2 {
		t.Fatalf("rapid tick 2 speed = %d, want 2", speed)
	}

	// tick 4 (streak 4): sqrt(4)+1 = 3
	WheelSpeed(&tracker, start.Add(300*time.Millisecond))        // streak 3
	speed = WheelSpeed(&tracker, start.Add(320*time.Millisecond)) // streak 4
	if speed != 3 {
		t.Fatalf("rapid tick 4 speed = %d, want 3", speed)
	}

	// Many rapid ticks should cap at 12
	for i := 0; i < 200; i++ {
		speed = WheelSpeed(&tracker, start.Add(320*time.Millisecond+time.Duration(i)*5*time.Millisecond))
	}
	if speed != 12 {
		t.Fatalf("max speed = %d, want 12", speed)
	}

	// Pause for 150ms: resets to 1
	speed = WheelSpeed(&tracker, start.Add(2*time.Second))
	if speed != 1 {
		t.Fatalf("after pause speed = %d, want 1", speed)
	}
}
