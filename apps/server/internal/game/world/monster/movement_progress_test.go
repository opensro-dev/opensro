package monster

import (
	"encoding/json"
	"math"
	"opensro.online/server/internal/testsupport/licensed"
	"strconv"
	"testing"
)

// This fixture executes the original x86 comparison/increment and both actor
// class predicates. Expected values are not calculated by the Go candidate.
func TestMovementProgressOriginalMachineSeams(t *testing.T) {
	data, err := licensed.ServerFixture(t, "internal/game/world/monster/testdata/ai-stuck-native-v1188.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		Samples []struct {
			DistanceBits string
			Counts       []uint8
			Events       []bool
		}
		ControllerWarpTIDs []uint16
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if len(fixture.Samples) != 6 {
		t.Fatal("incomplete native counter fixture")
	}
	for _, sample := range fixture.Samples {
		bits, err := strconv.ParseUint(sample.DistanceBits, 16, 64)
		if err != nil {
			t.Fatal(err)
		}
		if len(sample.Counts) != 256 || len(sample.Events) != 256 {
			t.Fatal("incomplete counter domain")
		}
		for initial := 0; initial < 256; initial++ {
			p := MovementProgress{failures: uint8(initial)}
			if event := p.arrivedDistance(math.Float64frombits(bits)); event != sample.Events[initial] || p.Failures() != sample.Counts[initial] {
				t.Fatalf("native mismatch: bits=%s initial=%d count=%d event=%v", sample.DistanceBits, initial, p.Failures(), event)
			}
		}
	}
	want := make(map[uint16]bool)
	for _, tid := range fixture.ControllerWarpTIDs {
		want[tid] = true
	}
	if len(want) == 0 {
		t.Fatal("empty native class fixture")
	}
	for tid := 0; tid <= 65535; tid++ {
		got := NativeStuckResponse(false, 0, uint16(tid)) == StuckWarpToControllerAndIdle
		if got != want[uint16(tid)] {
			t.Fatalf("native class mismatch: TID=%04x", tid)
		}
	}
}

func TestMovementProgressOriginsAndNonconsecutiveFailures(t *testing.T) {
	var p MovementProgress
	a := Pose{RegionID: 0x6262, X: 100, Z: 100}
	p.Issued(a, false)
	b := a
	b.X += 50
	p.Issued(b, true)
	if p.SequenceOrigin() != a || p.LastIssuedOrigin() != b {
		t.Fatal("replan replaced sequence origin")
	}
	for n := 0; n < 9; n++ {
		p.Issued(a, false)
		if p.Arrived(a) {
			t.Fatal("early event 40")
		}
		if p.Arrived(b) {
			t.Fatal("successful movement generated event 40")
		}
	}
	if p.Failures() != 9 || !p.Arrived(a) {
		t.Fatal("success incorrectly reset failure history")
	}
	p.ResetFailures()
	if p.Failures() != 0 || p.SequenceOrigin() != a || p.LastIssuedOrigin() != a {
		t.Fatal("state reset changed origins")
	}
}

func TestMovementProgressComparisonAndByteWrap(t *testing.T) {
	for _, d := range []float64{math.Nextafter(20, 0), 20, math.Nextafter(20, 21), math.NaN(), math.Inf(1)} {
		for count := 0; count < 256; count++ {
			p := MovementProgress{failures: uint8(count)}
			wantCount := uint8(count)
			if d < 20 {
				wantCount++
			}
			wantEvent := d < 20 && wantCount >= 10
			if got := p.arrivedDistance(d); got != wantEvent || p.failures != wantCount {
				t.Fatalf("distance=%g count=%d", d, count)
			}
		}
	}
}

func TestStuckBattleOverrideAndActorClass(t *testing.T) {
	for kind := uint16(0); kind < 32; kind++ {
		tid := uint16(0x1c6) | kind<<11
		want := StuckDetachAndIdle
		if kind >= 3 && kind <= 5 {
			want = StuckWarpToControllerAndIdle
		}
		if NativeStuckResponse(false, 2, tid) != want {
			t.Fatalf("COS kind %d", kind)
		}
		if NativeStuckResponse(true, 0, tid) != StuckRetainBattle || NativeStuckResponse(true, 2, tid) != StuckWarpToBattleTarget {
			t.Fatal("base handler replaced BATTLE override")
		}
	}
	if NativeStuckResponse(false, 2, 0x194) != StuckDetachAndIdle {
		t.Fatal("ordinary controlled monster took COS warp")
	}
}
