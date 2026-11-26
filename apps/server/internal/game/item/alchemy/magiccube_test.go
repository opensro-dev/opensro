/*
===========================================================================

magiccube_test.go - Magic Cube direct use in Alchemy Reinforce (#3906)

Port-only, behind Catalog.MagicCube (SRO_MAGIC_CUBE). Pins the v1.188
reference server's reinforce slot admission, gear match, linked elixir
deduction, and 0xB06D 0x29 wire frame (v1.696 client 0x7f7480).

===========================================================================
*/

package alchemy

import (
	"bytes"
	"errors"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/item/inventory"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
magicCubeFixture

A weapon + weapon cube holding two weapon elixirs (slots 0 and 1,
maxStack 1) and an armor elixir reference for mismatch checks.
================
*/
func magicCubeFixture(enabled bool) (*Catalog, []inventory.Item) {
	c, base := fixture()
	c.MagicCube = enabled
	c.Items["armor_elixir"] = Reference{
		ID: 4, Name: "armor_elixir", Flags: wire.PackTypeFlags(3, 3, 10, 1),
		Params: [5]uint32{1 << 24, 420744970, 168430090, 168101125, 0xffffffff},
	}
	c.Items["ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10"] = Reference{
		ID: 100, Name: "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10", Flags: wire.PackTypeFlags(3, 2, 3, 1),
	}
	cube := inventory.Item{
		Slot:      14,
		RefObjID:  100,
		Codename:  "ITEM_PRE_MALL_MAGIC_CUBE_WEAPON_10",
		TypeFlags: wire.PackTypeFlags(3, 2, 3, 1),
		Quantity:  1,
		Data:      2,
		LinkedStorage: &domain.COSContainer{
			Capacity: 10,
			Rows: []domain.InventoryRow{
				{Slot: 0, RefObjID: 2, Codename: "elixir", TypeFlags: wire.PackTypeFlags(3, 3, 10, 1), StackCount: 1},
				{Slot: 1, RefObjID: 2, Codename: "elixir", TypeFlags: wire.PackTypeFlags(3, 3, 10, 1), StackCount: 1},
			},
		},
	}
	return c, []inventory.Item{base[0], cube}
}

/*
================
TestReinforceWithMagicCubeConsumesLinkedElixirAndEmits0x29
================
*/
func TestReinforceWithMagicCubeConsumesLinkedElixirAndEmits0x29(t *testing.T) {
	c, items := magicCubeFixture(true)

	outcome, err := c.Reinforce(items, []uint8{13, 14}, 0, sequence(t, 24))
	if err != nil {
		t.Fatalf("Reinforce with Magic Cube failed: %v", err)
	}
	if !outcome.Success {
		t.Fatalf("expected success on draw 24")
	}
	if outcome.CubeConsume == nil || outcome.CubeConsume.Slot != 14 || outcome.CubeConsume.RefObjID != 2 || outcome.CubeConsume.Quantity != 1 {
		t.Fatalf("CubeConsume = %+v, want {Slot:14 RefObjID:2 Quantity:1}", outcome.CubeConsume)
	}
	// Original detached input must not be mutated before commit.
	if items[1].Data != 2 || len(items[1].LinkedStorage.Rows) != 2 {
		t.Fatalf("before items mutated: Data=%d rows=%d", items[1].Data, len(items[1].LinkedStorage.Rows))
	}
	// Resulting bag keeps the cube with Data=1 and lowest slot (0) removed.
	var gotCube *inventory.Item
	for i := range outcome.Items {
		if outcome.Items[i].Slot == 14 {
			gotCube = &outcome.Items[i]
		}
	}
	if gotCube == nil || gotCube.Quantity != 1 || gotCube.Data != 1 {
		t.Fatalf("after cube = %+v, want Quantity=1 Data=1", gotCube)
	}
	if gotCube.LinkedStorage == nil || len(gotCube.LinkedStorage.Rows) != 1 || gotCube.LinkedStorage.Rows[0].Slot != 1 {
		t.Fatalf("after linked rows = %+v, want single remaining row at slot 1", gotCube.LinkedStorage)
	}

	frames := ReinforceFrames(items, outcome)
	if len(frames) != 2 {
		t.Fatalf("len(frames) = %d, want 2 (0xB06D 0x29 + 0xB373)", len(frames))
	}
	wantConsume := wire.EncodeConsumeMagicCubeResult(14, 2, 1)
	if frames[0].Opcode != wire.OpItemMoveResponse || !bytes.Equal(frames[0].Payload, wantConsume) {
		t.Fatalf("frame[0] = (0x%04X, %x), want (0x%04X, %x)", frames[0].Opcode, frames[0].Payload, wire.OpItemMoveResponse, wantConsume)
	}
	if frames[1].Opcode != OpReinforceResult {
		t.Fatalf("frame[1].Opcode = 0x%04X, want 0x%04X", frames[1].Opcode, OpReinforceResult)
	}

	// Second reinforce spends the last elixir, leaving an empty cube in slot 14.
	second, err := c.Reinforce(outcome.Items, []uint8{13, 14}, 0, sequence(t, 0))
	if err != nil {
		t.Fatalf("second Reinforce failed: %v", err)
	}
	var emptyCube *inventory.Item
	for i := range second.Items {
		if second.Items[i].Slot == 14 {
			emptyCube = &second.Items[i]
		}
	}
	if emptyCube == nil || emptyCube.Quantity != 1 || emptyCube.Data != 0 || len(emptyCube.LinkedStorage.Rows) != 0 {
		t.Fatalf("empty cube = %+v, want Quantity=1 Data=0 rows=0", emptyCube)
	}

	// Third attempt against the now-empty cube must refuse with 0x5407.
	if _, err := c.Reinforce(second.Items, []uint8{13, 14}, 0, sequence(t)); !errors.Is(err, Refusal(7)) {
		t.Fatalf("empty cube error = %v, want Refusal(7)", err)
	}
}

/*
================
TestReinforceWithMagicCubeRefusals
================
*/
func TestReinforceWithMagicCubeRefusals(t *testing.T) {
	// Flag off (native default): Magic Cube refused with 0x10.
	cOff, itemsOff := magicCubeFixture(false)
	if _, err := cOff.Reinforce(itemsOff, []uint8{13, 14}, 0, sequence(t)); !errors.Is(err, Refusal(0x10)) {
		t.Fatalf("disabled MagicCube error = %v, want Refusal(0x10)", err)
	}

	// Wrong elixir category inside cube (armor elixir on weapon): Refusal(9).
	c, items := magicCubeFixture(true)
	items[1].LinkedStorage.Rows = []domain.InventoryRow{
		{Slot: 0, RefObjID: 4, Codename: "armor_elixir", TypeFlags: wire.PackTypeFlags(3, 3, 10, 1), StackCount: 1},
	}
	items[1].Data = 1
	if _, err := c.Reinforce(items, []uint8{13, 14}, 0, sequence(t)); !errors.Is(err, Refusal(9)) {
		t.Fatalf("mismatched cube elixir error = %v, want Refusal(9)", err)
	}
}
