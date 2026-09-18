package whatsapp

import (
	"testing"
	"time"
)

func TestHeartbeatConstants(t *testing.T) {
	if MinHeartbeatInterval != 3*time.Hour {
		t.Fatalf("expected MinHeartbeatInterval to be 3h, got %v", MinHeartbeatInterval)
	}
	if MaxHeartbeatInterval != 5*time.Hour {
		t.Fatalf("expected MaxHeartbeatInterval to be 5h, got %v", MaxHeartbeatInterval)
	}
	if MaxHeartbeatInterval <= MinHeartbeatInterval {
		t.Fatalf("expected MaxHeartbeatInterval > MinHeartbeatInterval")
	}
}

func TestRandomDurationBounds(t *testing.T) {
	min := MinHeartbeatInterval
	max := MaxHeartbeatInterval

	distinctValues := make(map[time.Duration]struct{})

	for i := 0; i < 1000; i++ {
		d := randomDuration(min, max)
		if d < min {
			t.Fatalf("iteration %d: generated duration %v is less than min %v", i, d, min)
		}
		if d > max {
			t.Fatalf("iteration %d: generated duration %v is greater than max %v", i, d, max)
		}
		distinctValues[d] = struct{}{}
	}

	// Over 1000 iterations in a 2-hour delta range, we expect high entropy (many distinct values)
	if len(distinctValues) < 500 {
		t.Errorf("expected high entropy in random durations, got only %d distinct values", len(distinctValues))
	}
}

func TestRandomDurationEdgeCases(t *testing.T) {
	// min == max
	fixed := 4 * time.Hour
	if d := randomDuration(fixed, fixed); d != fixed {
		t.Errorf("expected %v when min == max, got %v", fixed, d)
	}

	// min > max
	if d := randomDuration(5*time.Hour, 3*time.Hour); d != 5*time.Hour {
		t.Errorf("expected min when min > max, got %v", d)
	}
}
