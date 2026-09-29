/*
===========================================================================

registry_test.go - linked attack admission and native frame boundaries

Drive logical time directly. These cases distinguish per-target lks2 limits
from ordinary outgoing links and preserve pulse-before-expiry ordering.

===========================================================================
*/

package linkedpulse

import "testing"

/*
================
testEffect
================
*/
func testEffect(skill, target, token uint32) Effect {
	return Effect{Division: "test", SourceName: "caster", SourceSession: 1,
		SourceGID: 10, TargetGID: target, SourceToken: token, TargetToken: token + 1,
		SkillID: skill, LinkGroup: 9, MaxPerTarget: 2, StartedMs: 1000, DurationMs: 12000, PeriodMs: 2000}
}

/*
================
TestPerTargetAdmission
================
*/
func TestPerTargetAdmission(t *testing.T) {
	var registry Registry
	for _, effect := range []Effect{testEffect(100, 20, 1), testEffect(200, 20, 3), testEffect(100, 30, 5)} {
		if code := registry.Install(effect); code != 0 {
			t.Fatalf("install %+v refused %04x", effect, code)
		}
	}
	if got := registry.Refusal(testEffect(100, 20, 7)); got != ErrDuplicate {
		t.Fatalf("duplicate refusal = %04x", got)
	}
	if got := registry.Refusal(testEffect(300, 20, 7)); got != ErrCapacity {
		t.Fatalf("third skill refusal = %04x", got)
	}
	if _, ok := registry.Remove("other", 1); ok {
		t.Fatal("removed another division's pair")
	}
	if _, ok := registry.Remove("test", 1); !ok {
		t.Fatal("pair was not removed")
	}
	if code := registry.Install(testEffect(300, 20, 7)); code != 0 {
		t.Fatalf("retired pair still consumes capacity: %04x", code)
	}
}

/*
================
TestPulseClockAndExpiryOrder
================
*/
func TestPulseClockAndExpiryOrder(t *testing.T) {
	var registry Registry
	if code := registry.Install(testEffect(100, 20, 1)); code != 0 {
		t.Fatal(code)
	}
	for _, tc := range []struct {
		now           int64
		pulse, expire bool
	}{
		{1000, false, false}, {2999, false, false}, {3000, true, false},
		{3000, false, false}, {10000, true, false}, {11999, false, false},
		{13000, true, false}, {13001, false, true}, {16000, true, true},
	} {
		steps := registry.Frame(tc.now)
		if len(steps) != 1 || steps[0].Pulse != tc.pulse || steps[0].Expire != tc.expire {
			t.Fatalf("frame %d: %+v", tc.now, steps)
		}
	}
}

/*
================
TestDurationPercentage
================
*/
func TestDurationPercentage(t *testing.T) {
	for _, tc := range []struct{ base, percent, want uint32 }{
		{12000, 0, 12000}, {12000, 25, 15000}, {12000, 100, 24000}, {1001, 33, 1331},
	} {
		if got := Duration(tc.base, tc.percent); got != tc.want {
			t.Fatalf("duration %d + %d%% = %d; want %d", tc.base, tc.percent, got, tc.want)
		}
	}
}
