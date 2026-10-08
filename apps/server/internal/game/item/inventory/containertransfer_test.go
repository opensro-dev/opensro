package inventory

import (
	"testing"

	"opensro.online/server/internal/domain"
)

func TestCrossContainerOccupiedDestinations(t *testing.T) {
	for _, reverse := range []bool{false, true} {
		for _, tc := range []struct {
			name                   string
			dest                   uint16
			different              bool
			sourceAfter, destAfter uint16
		}{
			{"merge", 10, false, 0, 40}, {"remainder", 40, false, 20, 50}, {"full", 50, false, 50, 30}, {"swap", 7, true, 7, 30},
		} {
			t.Run(tc.name+map[bool]string{true: "-reverse", false: "-forward"}[reverse], func(t *testing.T) {
				player := New(nil, domain.DefaultInventorySize)
				cos, fault := NewContainer(nil, 4)
				if fault != nil {
					t.Fatal(fault)
				}
				source, dest := player, cos
				ss, ds := uint8(13), uint8(0)
				if reverse {
					source, dest = cos, player
					ss, ds = 0, 13
				}
				source.items = []Item{{Slot: ss, RefObjID: 1, TypeFlags: 0x86c, Quantity: 30, MagicOptions: []uint64{11}}}
				id := uint32(1)
				if tc.different {
					id = 2
				}
				dest.items = []Item{{Slot: ds, RefObjID: id, TypeFlags: 0x86c, Quantity: tc.dest, MagicOptions: []uint64{22}}}
				if f := source.TransferWholeTo(dest, ss, ds, 50); f != nil {
					t.Fatal(f)
				}
				a, present := source.At(ss)
				b, _ := dest.At(ds)
				if a.Quantity != tc.sourceAfter || present != (tc.sourceAfter > 0) || b.Quantity != tc.destAfter {
					t.Fatalf("counts %v %v", a, b)
				}
				if tc.different && (a.RefObjID != 2 || b.RefObjID != 1 || a.MagicOptions[0] != 22 || b.MagicOptions[0] != 11) {
					t.Fatal("swap lost identity")
				}
			})
		}
	}
}
