/*
===========================================================================

nasrun_test.go - Nasrun alchemy: Sabakun's jewel on a Devil's Spirit

===========================================================================
*/

package alchemy

import (
	"errors"
	"testing"

	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
spiritFixture

The shared fixture plus a Spirit (3/1/14) in slot 16 and two jewels
(an elixir admitting class 14 only) in slot 17.
================
*/
func spiritFixture(nasrunOn bool) (*Catalog, []inventory.Item) {
	c, items := fixture()
	c.Nasrun = nasrunOn
	c.Items["spirit"] = Reference{ID: 4, Name: "spirit", Flags: wire.PackTypeFlags(3, 1, 14, 1)}
	c.Items["jewel"] = Reference{ID: 5, Name: "jewel", Flags: wire.PackTypeFlags(3, 3, 10, 1), Params: [5]uint32{spiritClass, 0, 0, 0, 0}}
	for i, name := range []string{"spirit", "jewel"} {
		r := c.Items[name]
		items = append(items, inventory.Item{Slot: uint8(16 + i), RefObjID: r.ID, Codename: name, TypeFlags: r.Flags, Quantity: 1})
	}
	items[len(items)-1].Quantity = 2
	return c, items
}

/*
================
spiritResult

The Spirit and the jewel stack as the outcome left them.
================
*/
func spiritResult(t *testing.T, r Outcome) (spirit, jewel inventory.Item) {
	t.Helper()
	for _, item := range r.Items {
		switch item.Slot {
		case 16:
			spirit = item
		case 17:
			jewel = item
		}
	}
	if spirit.Quantity != 1 {
		t.Fatalf("the Spirit is gone: %+v", r)
	}
	return spirit, jewel
}

/*
================
TestSpiritJewelRollsTheOwnersChances

At each plus, a draw under the chance raises the Spirit one plus. A draw
at the chance fails: at +5 or below the plus stays, above +5 it drops to
+5, and from +8 up a second draw at SpiritDestroyPercent spares it. Either
way one jewel is spent.
================
*/
func TestSpiritJewelRollsTheOwnersChances(t *testing.T) {
	for plus, chance := range []uint32{70, 60, 50, 40, 30, 20, 15, 10, 7, 5} {
		for _, tc := range []struct {
			draws   []uint32
			success bool
		}{{[]uint32{chance - 1}, true}, {[]uint32{chance, SpiritDestroyPercent}, false}} {
			c, items := spiritFixture(true)
			items[3].Plus = uint8(plus)
			draws := tc.draws
			if !tc.success && plus < 8 {
				draws = draws[:1]
			}
			r, err := c.Reinforce(items, []uint8{16, 17}, 0, sequence(t, draws...))
			if err != nil {
				t.Fatalf("+%d draws %v: %v", plus, draws, err)
			}
			spirit, jewel := spiritResult(t, r)
			want := uint8(plus)
			switch {
			case tc.success:
				want++
			case plus > 5:
				want = 5
			}
			if r.Success != tc.success || r.Destroyed || spirit.Plus != want || r.From != uint8(plus) {
				t.Fatalf("+%d draws %v: outcome %+v, Spirit +%d, want +%d", plus, draws, r, spirit.Plus, want)
			}
			if jewel.Quantity != 1 {
				t.Fatalf("+%d draws %v: %d jewels left, want 1", plus, draws, jewel.Quantity)
			}
		}
	}
}

/*
================
TestSpiritJewelDestroysFromRiskPlus

A failure from +8 or +9 whose second draw falls under SpiritDestroyPercent
destroys the Spirit and still spends the jewel; the result frame then
carries the destroy flag and no item body. A failure at +7 never draws a
second time (sequence fails on an extra draw), so it cannot break.
================
*/
func TestSpiritJewelDestroysFromRiskPlus(t *testing.T) {
	// The owner's rule in numbers, not the constants under test.
	for _, plus := range []uint8{8, 9} {
		c, items := spiritFixture(true)
		items[3].Plus = plus
		chance, _ := SpiritChance(plus)
		r, err := c.Reinforce(items, []uint8{16, 17}, 0, sequence(t, uint32(chance), SpiritDestroyPercent-1))
		if err != nil {
			t.Fatalf("+%d: %v", plus, err)
		}
		if r.Success || !r.Destroyed {
			t.Fatalf("+%d: outcome %+v, want destroyed", plus, r)
		}
		for _, item := range r.Items {
			if item.Slot == 16 {
				t.Fatalf("+%d: the destroyed Spirit is still in the bag: %+v", plus, item)
			}
			if item.Slot == 17 && item.Quantity != 1 {
				t.Fatalf("+%d: %d jewels left, want 1", plus, item.Quantity)
			}
		}
		frames := ReinforceFrames(items, r)
		last := frames[len(frames)-1].Payload
		if len(last) != 4 || last[1] != 0 || last[3] != 1 {
			t.Fatalf("+%d: result frame % x, want failure with the destroy flag and no body", plus, last)
		}
	}
	c, items := spiritFixture(true)
	items[3].Plus = 7
	chance, _ := SpiritChance(7)
	r, err := c.Reinforce(items, []uint8{16, 17}, 0, sequence(t, uint32(chance)))
	if err != nil || r.Destroyed {
		t.Fatalf("+7 failure: %+v, %v; want a drop to +5 with no destruction draw", r, err)
	}
}

/*
================
TestSpiritJewelIgnoresLuckAndBonus

The chance is the owner's table alone: a Lucky charge stays unspent and
the alchemy bonus adds nothing, so a draw at the chance still fails.
================
*/
func TestSpiritJewelIgnoresLuckAndBonus(t *testing.T) {
	c, items := spiritFixture(true)
	items[3].MagicOptions = []uint64{1<<32 | 5}
	r, err := c.Reinforce(items, []uint8{16, 17}, 50, sequence(t, 70))
	if err != nil {
		t.Fatal(err)
	}
	spirit, _ := spiritResult(t, r)
	if r.Success || len(spirit.MagicOptions) != 1 {
		t.Fatalf("luck or bonus touched the roll: %+v", r)
	}
}

/*
================
TestSpiritJewelRefusals

Each refusal answers before any draw (sequence fails on a draw).
================
*/
func TestSpiritJewelRefusals(t *testing.T) {
	for _, tc := range []struct {
		name  string
		on    bool
		plus  uint8
		slots []uint8
		want  Refusal
	}{
		{"switch off: a Spirit is no target", false, 0, []uint8{16, 17}, Refusal(0x10)},
		{"at +10", true, SpiritMaxPlus, []uint8{16, 17}, Refusal(0x0f)},
		{"a lucky powder staged", true, 0, []uint8{16, 17, 15}, Refusal(0x0a)},
		{"a native elixir on a Spirit", true, 0, []uint8{16, 14}, Refusal(9)},
		{"the jewel on native gear", true, 0, []uint8{13, 17}, Refusal(9)},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, items := spiritFixture(tc.on)
			items[3].Plus = tc.plus
			_, err := c.Reinforce(items, tc.slots, 0, sequence(t))
			var refusal Refusal
			if !errors.As(err, &refusal) || refusal != tc.want {
				t.Fatalf("err %v, want %v", err, tc.want)
			}
		})
	}
}
