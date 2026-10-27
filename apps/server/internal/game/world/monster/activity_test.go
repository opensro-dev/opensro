package monster

import "testing"

func TestActivityCadenceNativeBoundaries(t *testing.T) {
	for _, start := range []uint32{0, 10000, 0xffffff00} {
		for _, random := range []uint32{0, 999, 1000, 32767} {
			a := NewActivityCadence(start, random)
			interval := uint32(random%1000 + 1000)
			if uint32(a.Interval) != interval || a.Due(start+interval) || a.LastCheck != start {
				t.Fatalf("equality advanced: %+v", a)
			}
			if !a.Due(start+interval+1) || a.LastCheck != start+interval+1 {
				t.Fatalf("expired cadence not consumed: %+v", a)
			}
			if a.Due(start + interval + 2) {
				t.Fatal("empty activity query rearmed cadence")
			}
		}
	}
}

/*
================
TestActivityCadenceCatchUp

CatchUp lands the next due time in (now, now+Interval+1] on the phase a
never-slept twin with the same cadence fires on, across the exact-period
boundary and the uint32 wrap.
================
*/
func TestActivityCadenceCatchUp(t *testing.T) {
	for _, tc := range []struct {
		name      string
		interval  uint16
		last, now uint32
		wantLast  uint32
		wantDueIn uint32 // next due time is now + wantDueIn
	}{
		{"zero interval keeps the record", 0, 123, 456, 123, 0},
		{"within first period", 1000, 0, 500, 0, 501},
		{"exactly one period", 1000, 0, 1001, 1001, 1001},
		{"many periods", 1000, 0, 2500, 2002, 503},
		{"long sleep", 1500, 90000, 200000, 199573, 1074},
		{"wraparound", 1500, 0xfffffff0, 100, 0xfffffff0, 1385},
		{"minimum interval", 1, 0, 100, 100, 2},
	} {
		t.Run(tc.name, func(t *testing.T) {
			a := ActivityCadence{Interval: tc.interval, LastCheck: tc.last}
			got := a.CatchUp(tc.now)
			if got.LastCheck != tc.wantLast || got.Interval != tc.interval {
				t.Fatalf("CatchUp(%d) = %+v, want LastCheck %d", tc.now, got, tc.wantLast)
			}
			if tc.interval == 0 {
				return
			}
			if got.Due(tc.now) {
				t.Fatalf("caught-up cadence due on the wake tick: %+v", got)
			}
			// The never-slept twin, stepped one ms at a time through the
			// sleep window (its check at the wake tick itself included),
			// ends on the same LastCheck phase and fires next on the same
			// tick as the caught-up cadence; only at an exact period
			// boundary does the twin run one check the sleeper skips.
			firstFire := func(a ActivityCadence, from uint32) uint32 {
				for tick := from; ; tick++ {
					if a.Due(tick) {
						return tick
					}
				}
			}
			twin := ActivityCadence{Interval: tc.interval, LastCheck: tc.last}
			for tick := tc.last; ; tick++ {
				twin.Due(tick)
				if tick == tc.now {
					break
				}
			}
			if twin.LastCheck != got.LastCheck {
				t.Fatalf("phase diverged from never-slept twin: %d vs %d", got.LastCheck, twin.LastCheck)
			}
			if fire, want := firstFire(got, tc.now), tc.now+tc.wantDueIn; fire != want {
				t.Fatalf("next due at +%d, want +%d", fire-tc.now, tc.wantDueIn)
			}
			if fire := firstFire(twin, tc.now+1); fire != tc.now+tc.wantDueIn {
				t.Fatalf("twin next due at +%d, want +%d", fire-tc.now, tc.wantDueIn)
			}
		})
	}
}

func TestActivityAdmissionIsSpecificToWander(t *testing.T) {
	for flags := uint32(0); flags < 256; flags++ {
		for mode := MoverMode(0); mode < moverModeCount; mode++ {
			for _, controlled := range []bool{false, true} {
				want := mode == MoverWandering && flags&0x84 == 0 && !controlled
				if MaySuspendWander(flags, controlled, mode) != want {
					t.Fatalf("flags=%x mode=%s controlled=%v", flags, mode, controlled)
				}
			}
		}
	}
}
