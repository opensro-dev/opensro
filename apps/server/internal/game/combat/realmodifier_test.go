package combat

import (
	"encoding/json"
	"opensro.online/server/internal/testsupport/licensed"
	"testing"
)

func TestRealModifiersNativeDispatcherAndReader(t *testing.T) {
	data, err := licensed.ServerFixture(t, "internal/game/combat/testdata/native-real-modifiers-20260920.json")
	if err != nil {
		t.Fatal(err)
	}
	var fixture struct {
		BinarySHA256 string
		Dispatcher   []struct {
			Mask, Remove uint32
			Calls        [][]uint32
		}
		Reader []struct {
			Grade, Value uint32
			Outputs      []uint32
		}
	}
	if err := json.Unmarshal(data, &fixture); err != nil {
		t.Fatal(err)
	}
	if !licensed.SHA256Hex(fixture.BinarySHA256) || len(fixture.Dispatcher) != 68 || len(fixture.Reader) != 25 {
		t.Fatal("native fixture identity or scope")
	}
	for _, row := range fixture.Dispatcher {
		var r RealModifiers
		if row.Remove != 0 {
			r.Update(^uint32(0), 93, 7, 0x87654321, false)
		}
		r.Update(row.Mask, 93, 7, 0x87654321, row.Remove != 0)
		selected := map[uint32]bool{}
		for _, call := range row.Calls {
			if len(call) != 5 || call[0] < 0x14 || (call[0]-0x14)%24 != 0 || call[1] != 0x87654321 || call[2] != 7 || call[3] != 93 || call[4] != row.Remove {
				t.Fatalf("native ABI: %+v", call)
			}
			selected[(call[0]-0x14)/24] = true
		}
		for i, bit := range realMasks {
			want := selected[uint32(i)]
			if row.Remove != 0 {
				want = !want
			}
			if (len(r.Contributions(bit, 7)) != 0) != want {
				t.Fatalf("native dispatch mask=%x remove=%d slot=%d", row.Mask, row.Remove, i)
			}
		}
	}
	for _, row := range fixture.Reader {
		var r RealModifiers
		r.Update(0x40, row.Value, row.Grade, 12, false)
		grade, value := r.Strongest(0x40)
		if len(row.Outputs) != 3 || grade != row.Outputs[1] || value != row.Outputs[2] {
			t.Fatalf("native reader %+v got %d,%d", row, grade, value)
		}
	}
}

func TestRealModifierOwnershipAndMultiplicity(t *testing.T) {
	var r RealModifiers
	r.Update(^uint32(0), 80, 7, 100, false)
	r.Update(^uint32(0), 80, 7, 101, false)
	r.Update(^uint32(0), 80, 7, 100, false)
	r.Update(^uint32(0), ^uint32(0), 7, 102, false)
	r.Update(^uint32(0), 80, 8, 100, false)
	r.Update(^uint32(0), 79, 7, 100, true)
	r.Update(^uint32(0), 80, 7, 999, true)
	for _, bit := range realMasks {
		got := r.Contributions(bit, 7)
		if len(got) != 4 || got[0] != (RealContribution{80, 100}) || got[3].Value != ^uint32(0) {
			t.Fatalf("mask %x: %+v", bit, got)
		}
	}
	r.Update(^uint32(0), 80, 7, 100, true)
	for _, bit := range realMasks {
		got := r.Contributions(bit, 7)
		if len(got) != 3 || got[0].Context != 101 || len(r.Contributions(bit, 8)) != 1 {
			t.Fatalf("removal mask %x: %+v", bit, got)
		}
	}
	r.Update(^uint32(0), 80, 7, 100, true)
	r.Update(^uint32(0), 80, 7, 101, true)
	r.Update(^uint32(0), ^uint32(0), 7, 102, true)
	for _, bit := range realMasks {
		if len(r.Contributions(bit, 7)) != 0 {
			t.Fatalf("stale bucket %x", bit)
		}
	}
	for _, bit := range []uint32{0x1000, 0x800000} {
		if len(r.Contributions(bit, 8)) != 0 {
			t.Fatalf("unhandled mask %x", bit)
		}
	}
	r.Update(0x1000000, 80, 8, 100, true)
	if len(r.Contributions(0x1000000, 8)) != 0 || len(r.Contributions(0x40, 8)) != 1 {
		t.Fatal("mask isolation")
	}
}

func TestRealModifierGradePrecedesValue(t *testing.T) {
	var r RealModifiers
	r.Update(0x40, 999, 8, 100, false)
	r.Update(0x40, 1, 9, 101, false)
	if grade, value := r.Strongest(0x40); grade != 9 || value != 1 {
		t.Fatalf("%d %d", grade, value)
	}
	r.Update(0x40, 99, 9, 102, false)
	if _, value := r.Strongest(0x40); value != 99 {
		t.Fatal(value)
	}
	r.Update(0x40, 99, 9, 102, true)
	if _, value := r.Strongest(0x40); value != 1 {
		t.Fatal(value)
	}
}
