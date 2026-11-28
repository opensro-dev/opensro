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

At each plus, a draw under the chance raises the Spirit one plus; a draw
at the chance keeps it up to +5 and drops it back to +5 above (#4732);
either way one jewel is spent.
================
*/
func TestSpiritJewelRollsTheOwnersChances(t *testing.T) {
	for plus, chance := range []uint32{70, 60, 50, 40, 30, 20, 15, 10, 8, 5} {
		for _, tc := range []struct {
			draw    uint32
			success bool
		}{{chance - 1, true}, {chance, false}} {
			c, items := spiritFixture(true)
			items[3].Plus = uint8(plus)
			r, err := c.Reinforce(items, []uint8{16, 17}, 0, sequence(t, tc.draw))
			if err != nil {
				t.Fatalf("+%d draw %d: %v", plus, tc.draw, err)
			}
			spirit, jewel := spiritResult(t, r)
			want := uint8(plus)
			if tc.success {
				want++
			} else if want > spiritFailFloor {
				want = spiritFailFloor
			}
			if r.Success != tc.success || r.Destroyed || spirit.Plus != want || r.From != uint8(plus) {
				t.Fatalf("+%d draw %d: outcome %+v, Spirit +%d, want +%d", plus, tc.draw, r, spirit.Plus, want)
			}
			if jewel.Quantity != 1 {
				t.Fatalf("+%d draw %d: %d jewels left, want 1", plus, tc.draw, jewel.Quantity)
			}
		}
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
