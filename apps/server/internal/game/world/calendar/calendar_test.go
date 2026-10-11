package calendar

import (
	"bytes"
	"testing"
)

func TestNativeRateAndCalendarBoundaries(t *testing.T) {
	for _, tc := range []struct {
		ms   int64
		want Value
	}{
		{0, Value{0, 0, 0}}, {1199, Value{0, 0, 0}}, {1200, Value{0, 0, 1}},
		{72000, Value{0, 1, 0}}, {1728000, Value{1, 0, 0}},
		{30 * 1728000, Value{30, 0, 0}}, {65536 * 1728000, Value{0, 0, 0}},
		{-1, Value{0, 0, 0}},
	} {
		if got := AtElapsedMilli(tc.ms); got != tc.want {
			t.Errorf("%d: got %+v want %+v", tc.ms, got, tc.want)
		}
	}
	if !bytes.Equal((Value{0x1234, 23, 59}).Payload(), []byte{0x34, 0x12, 23, 59}) {
		t.Fatal("native wire order")
	}
}
func TestLoginDoesNotResetWorldEpoch(t *testing.T) {
	before := startedAt
	a, b := Current(), Current()
	if startedAt != before {
		t.Fatal("sampling changed world epoch")
	}
	delta := int64(b.Day)*1440 + int64(b.Hour)*60 + int64(b.Minute) - (int64(a.Day)*1440 + int64(a.Hour)*60 + int64(a.Minute))
	if delta < 0 || delta > 1 {
		t.Fatalf("shared calendar jumped: %+v -> %+v", a, b)
	}
	if AtElapsedMilli(60*72000).Day != 2 {
		t.Fatal("calendar derived from login instead of world lifetime")
	}
}

func TestSetHourMovesTheSharedClockWithinTheDay(t *testing.T) {
	t.Cleanup(func() { shiftMilli.Store(0) })
	before := Current()
	for _, hour := range []uint8{22, 3} {
		set := SetHour(hour)
		if set.Hour != hour || set.Minute != 0 || set.Day != before.Day {
			t.Fatalf("SetHour(%d) = %+v from %+v", hour, set, before)
		}
		if now := Current(); now.Day != set.Day || now.Hour != hour || now.Minute > 1 {
			t.Fatalf("clock after SetHour(%d) reads %+v", hour, now)
		}
	}
}
