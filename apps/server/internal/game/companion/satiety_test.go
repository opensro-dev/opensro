/*
===========================================================================

satiety_test.go - native hunger precision, lifetime and keeper composition

===========================================================================
*/
package companion

import (
	"opensro.online/server/internal/game/abnormal"
	"testing"
)

/*
================
TestSatietyClockRetainsFractionsAndRetiresOfflineTime
================
*/
func TestSatietyClockRetainsFractionsAndRetiresOfflineTime(t *testing.T) {
	clock := SatietyClock{}
	value := clock.Advance(7, 1000, 5, 10000)
	for step := int64(1); step <= 6; step++ {
		value = clock.Advance(7, 1000+step*500, 5, value)
		want := uint16(10000)
		if step == 6 {
			want--
		}
		if value != want {
			t.Fatalf("step %d: %d, want %d", step, value, want)
		}
	}
	if value = clock.Advance(7, 304000, 5, value); value != 9899 {
		t.Fatal("authored five-minute percentage drain", value)
	}
	if same := clock.Advance(7, 304000, 5, value); same != value {
		t.Fatal("duplicate tick drained")
	}
	if next := clock.Advance(8, 900000, 5, value); next != value || clock.Carry != 0 {
		t.Fatal("replacement inherited elapsed time")
	}
	if next := clock.Advance(8, 1, 5, value); next != value {
		t.Fatal("backwards time drained")
	}
	clock = SatietyClock{}
	if next := clock.Advance(8, 9999999, 5, value); next != value {
		t.Fatal("new online lifetime charged offline time")
	}
	if next := clock.Advance(8, 9999999+30000000, 5, value); next != 0 {
		t.Fatal("large tick did not saturate at death", next)
	}
}

/*
================
TestNativeSatietyPublicationBoundaries
================
*/
func TestNativeSatietyPublicationBoundaries(t *testing.T) {
	for _, test := range []struct {
		before, after uint16
		want          bool
	}{
		{10000, 9999, true}, {9999, 9998, false}, {9001, 9000, false},
		{9000, 8999, true}, {3000, 2999, true}, {3100, 2999, false},
		{1, 0, true}, {0, 0, false},
	} {
		if got := PublishSatiety(test.before, test.after); got != test.want {
			t.Fatalf("%d -> %d: %v", test.before, test.after, got)
		}
	}
}

/*
================
TestHungerKeeperAppliesBeforeClampAndDoesNotAlterStatuses
================
*/
func TestHungerKeeperAppliesBeforeClampAndDoesNotAlterStatuses(t *testing.T) {
	block := abnormal.Block{}
	block.Modifiers[0] = abnormal.Modifier{Used: true, Param: 5, Channel: 0, Source: 5, Value: 20}
	block.Modifiers[1] = abnormal.Modifier{Used: true, Param: 5, Channel: 3, Source: 5, Value: 80}
	before := block
	for _, satiety := range []uint16{0, 2999, 3000, 3001, 10000} {
		value, err := Parameter(5, 100, satiety, &block)
		want := float32(96)
		if satiety < 3000 {
			want = 48
		}
		if err != nil || value != want || block != before {
			t.Fatalf("satiety %d: %v %v", satiety, value, err)
		}
	}
	for id := uint16(1); id <= 18; id++ {
		value, err := Parameter(id, 80, 2999, nil)
		want := float32(80)
		if HungryParameter(id) {
			want = 40
		}
		if err != nil || value != want {
			t.Fatalf("parameter %d: %v %v", id, value, err)
		}
	}
	value, err := Parameter(5, 12000000, 2999, nil)
	if err != nil || value != 6000000 {
		t.Fatal("hunger applied after premature clamp", value, err)
	}
}

/*
================
TestZeroSatietyRestorationDiesOnTheNextWholeDrain
================
*/
func TestZeroSatietyRestorationDiesOnTheNextWholeDrain(t *testing.T) {
	clock := SatietyClock{}
	clock.Advance(7, 1000, 5, 0)
	clock.Advance(7, 2000, 5, 0)
	if clock.Exhausted {
		t.Fatal("fractional drain killed early")
	}
	clock.Advance(7, 4000, 5, 0)
	if !clock.Exhausted {
		t.Fatal("zero-HGP restoration became immortal")
	}
}
