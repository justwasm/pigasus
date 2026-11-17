package render

import (
	"math"
	"time"
)

// WheelTracker tracks mouse wheel scroll streak and timing for inertial acceleration.
// Upstream: google3/third_party/jetski/cli/model/model.(*mouseState) / render.WheelSpeed
type WheelTracker struct {
	LastTime time.Time
	Streak   int
}

// WheelSpeed returns the scroll velocity based on the frequency of wheel events.
// Single wheel ticks or ticks separated by >= 150ms return 1. Rapid successive
// wheel ticks accelerate based on sqrt(streak) + 1, capped at 12.
//
// Upstream: google3/third_party/jetski/cli/render/render.WheelSpeed (0x102e1d340)
func WheelSpeed(t *WheelTracker, now time.Time) int {
	if t == nil {
		return 1
	}
	dt := now.Sub(t.LastTime)
	t.LastTime = now
	if dt >= 150*time.Millisecond {
		t.Streak = 0
	} else {
		t.Streak++
	}
	speed := 1 + int(math.Sqrt(float64(t.Streak)))
	if speed > 12 {
		speed = 12
	}
	return speed
}
