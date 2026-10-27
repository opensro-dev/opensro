package combat

import (
	"errors"
	"opensro.online/server/internal/game/enterworld"
	"testing"
)

func TestCriticalNativeAccumulatorBoundaries(t *testing.T) {
	state := Probability{}
	for i, want := range []struct {
		roll      uint32
		hit       bool
		threshold int32
	}{{3, true, -94}, {0, false, -91}, {100, false, -88}} {
		hit, next, err := CriticalOutcome(3, state, func() (uint32, error) { return want.roll, nil })
		if err != nil || hit != want.hit || next.Threshold != want.threshold {
			t.Fatalf("step %d: %v %+v %v", i, hit, next, err)
		}
		state = next
	}
	// A miss increases the existing threshold. The threshold may cross 100;
	// only the authored rate has the >=100 shortcut, not the accumulated threshold.
	hit, next, err := CriticalOutcome(60, Probability{true, 120}, func() (uint32, error) { return 100, nil })
	if err != nil || !hit || next.Threshold != 80 {
		t.Fatalf("accumulated: %v %+v %v", hit, next, err)
	}
	for _, rate := range []float64{0, 100, 255, 256} {
		hit, next, err = CriticalOutcome(rate, state, func() (uint32, error) { t.Fatal("shortcut consumed random draw"); return 0, nil })
		if err != nil || hit != (rate == 100 || rate == 255) || next != state {
			t.Fatalf("rate %v: %v %+v %v", rate, hit, next, err)
		}
	}
	_, next, err = CriticalOutcome(3, state, func() (uint32, error) { return 0, errors.New("entropy failed") })
	if err == nil || next != state {
		t.Fatal("failed draw changed probability history")
	}
	_, _, err = CriticalOutcome(3, state, func() (uint32, error) { return 32768, nil })
	if err == nil {
		t.Fatal("invalid entropy accepted")
	}
}

func TestCriticalOnlyDoublesPhysicalBeforeTruncationAndMinimumFloor(t *testing.T) {
	a := Stats{Level: 1, MaxLevel: 1, Strength: 32, Intellect: 32, PhysicalAttackMin: 100, PhysicalAttackMax: 200, MagicalAttackMin: 100, MagicalAttackMax: 200, HitRate: 50}
	d := Stats{Level: 1, EvasionRate: 50}
	for _, tc := range []struct {
		flags   uint32
		damage  uint32
		outcome uint8
	}{{4, 300, 2}, {8, 150, 1}, {12, 450, 2}} {
		r, err := ResolveOutcome(a, d, enterworld.SkillAttack{Present: true, Flags: tc.flags, Percent: 100}, func() (uint32, error) { return 0, nil }, true, true)
		if err != nil || r.Damage != tc.damage || r.ResultFlags != tc.outcome {
			t.Fatalf("lane %d: %+v %v", tc.flags, r, err)
		}
	}
	d.PhysicalDefense = 10000
	r, err := ResolveOutcome(a, d, enterworld.SkillAttack{Present: true, Flags: 4, Percent: 100}, func() (uint32, error) { return 0, nil }, true, true)
	if err != nil || r.Damage != 1 || r.ResultFlags != 2 {
		t.Fatalf("floor must replace, not double, minimum: %+v %v", r, err)
	}
}
