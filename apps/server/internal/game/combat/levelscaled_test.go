package combat

import (
	"errors"
	"testing"
)

/*
==================
TestLevelScaledDamageSpan

The monster-shaped hit: 5L + draw % (5L). The span edges are exact:
draw 0 lands on 5L, a draw at the span wraps, and the largest draw
stays below 10L.
==================
*/
func TestLevelScaledDamageSpan(t *testing.T) {
	for _, tc := range []struct {
		name  string
		level uint8
		draw  uint32
		want  uint32
	}{
		{"floor", 20, 0, 100},
		{"mid", 20, 37, 137},
		{"wrap", 20, 100, 100},
		{"largest draw", 20, 32767, 167},
		{"level one floor", 1, 0, 5},
		{"level one top", 1, 4, 9},
		{"level zero", 0, 12345, 0},
	} {
		got, err := LevelScaledDamage(tc.level, func() (uint32, error) { return tc.draw, nil })
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if got != tc.want {
			t.Errorf("%s: damage %d, want %d", tc.name, got, tc.want)
		}
	}
}

/*
==================
TestLevelScaledDamageRollError

A failing draw source surfaces its error and no damage.
==================
*/
func TestLevelScaledDamageRollError(t *testing.T) {
	want := errors.New("draw failed")
	got, err := LevelScaledDamage(20, func() (uint32, error) { return 0, want })
	if !errors.Is(err, want) || got != 0 {
		t.Fatalf("got %d, err %v", got, err)
	}
}
