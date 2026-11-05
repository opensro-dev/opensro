/*
===========================================================================

cape_unsummon_test.go - a committed free-battle cape equip retires the pet

The v1.150 client warns that wearing free-battle equipment unsummons
growth pets automatically (UIIT_MSG_COSPETERR_UNSUMMON_FREEBATTLE, from
its drop handler) and composes nothing but the ordinary equip move, so the
dismissal is the server's part of that equip. These tests drive the real
summon and the real inventory move and watch the pet's durable state and
the published frames.
===========================================================================
*/
package action

import (
	"encoding/binary"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
capeUnsummonWorld

A level-5 owner with the attack/pickup summoners (slots 23/24) and a
free-battle cape lying in the bag's first free slot (the seeded owner
already carries a weapon where suites usually rest).
================
*/
func capeUnsummonWorld(t *testing.T) (*Runtime, *enterworld.Character, cosTestItemSource) {
	t.Helper()
	c, refs := persistentSummonFixture()
	// The summon fixture raises Level to 5 alone; the base-stat
	// derivation an equip runs also reads MaxLevel, so keep them level.
	if c.Level != nil {
		maxLevel := *c.Level
		c.MaxLevel = &maxLevel
	}
	cape := &enterworld.ItemRef{Codename: "TEST_PVP_CAPE", RefObjID: 63000, Country: 3,
		TypeIDs: [4]int64{3, 1, 7, 5}, ReqQuadTypes: [4]int64{-1, -1, -1, -1}, RequiredSex: 2,
		Combat:       &enterworld.ItemCombatRef{},
		NativeFields: enterworld.NewNativeFields(map[string]float64{freeBattleGroupField: 1})}
	refs.staticItemSource[cape.Codename] = cape
	slot := int64(13)
	for {
		if _, taken := inventoryRowAt(c, uint8(slot)); !taken {
			break
		}
		slot++
	}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: slot,
		RefObjID: cape.RefObjID, Codename: cape.Codename, TypeFlags: cape.TypeFlags(), StackCount: 1})
	rt, _ := newTestRuntime(c, refs)
	return rt, c, refs
}

/*
================
equipBagCape

The ordinary 0x706D inventory move that wears the bag cape (its bag
slot to the job-suit socket), as drag and double-click alike arrive.
================
*/
func equipBagCape(t *testing.T, rt *Runtime, c *enterworld.Character) OpResult {
	t.Helper()
	source := uint8(0)
	for _, row := range c.MissionInventory {
		if row.Codename == "TEST_PVP_CAPE" {
			source = uint8(row.Slot)
		}
	}
	if source == 0 {
		t.Fatal("fixture: cape missing from the bag")
	}
	result := rt.HandleItemMove(testDivision, c, encodeMove(t, wire.ItemMoveRequest{
		MovementType: wire.MoveTypeInventory,
		SourceSlot:   source,
		DestSlot:     jobSuitSlot,
		Quantity:     1,
	}))
	if len(result.Frames) == 0 || result.Frames[0].Opcode != wire.OpItemMoveResponse || result.Frames[0].Payload[0] != 1 {
		t.Fatalf("cape equip refused: %+v", result)
	}
	if row, ok := inventoryRowAt(c, jobSuitSlot); !ok || row.Codename != "TEST_PVP_CAPE" {
		t.Fatalf("cape not worn after the move: %+v", c.MissionInventory)
	}
	return result
}

/*
================
removalPublished

Whether the owner heard the detach notice for gid and the neighbourhood
heard its despawn: the frame set every server-side release publishes.
================
*/
func removalPublished(result OpResult, gid uint32) (notice, despawn bool) {
	for _, frame := range result.Frames {
		if frame.Opcode == cosPetUpdateOpcode && len(frame.Payload) == 5 &&
			binary.LittleEndian.Uint32(frame.Payload) == gid && frame.Payload[4] == cosRemovalKind {
			notice = true
		}
		if frame.Opcode == wire.OpObjectDespawn && len(frame.Payload) >= 4 &&
			binary.LittleEndian.Uint32(frame.Payload) == gid {
			despawn = true
		}
	}
	return notice, despawn
}

/*
================
companionRecord

The character's current record for a companion gid. The inventory
rebuild inside a move replaces the record values it carries, so state
assertions read the record the server holds now, never a pointer
captured before the move.
================
*/
func companionRecord(c *enterworld.Character, gid uint32) *enterworld.CharacterCOS {
	for _, pet := range c.Companions() {
		if pet.GID == gid {
			return pet
		}
	}
	return nil
}

/*
================
TestCapeEquipDismissesSummonedGrowthPet

The growth (attack-band) pet is out; the cape lands. The pet's durable
record keeps its progress but loses the summoned bit, and the release
frames publish behind the move answer.
================
*/
func TestCapeEquipDismissesSummonedGrowthPet(t *testing.T) {
	rt, c, refs := capeUnsummonWorld(t)
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	pet := c.Companions()[0]
	if !pet.Summoned {
		t.Fatal("fixture: growth pet not summoned")
	}
	gid := pet.GID
	result := equipBagCape(t, rt, c)
	record := companionRecord(c, gid)
	if record == nil {
		t.Fatal("growth pet record lost")
	}
	if record.Summoned || record.StateFlags&cosStateSummoned != 0 {
		t.Fatal("cape equip left the growth pet summoned", record)
	}
	if record.StateFlags&1 == 0 {
		t.Fatal("cape equip deleted the pet's durable record state", record)
	}
	notice, despawn := removalPublished(result, gid)
	if !notice || !despawn {
		t.Fatalf("cape equip published notice=%t despawn=%t", notice, despawn)
	}
	state := false
	for _, frame := range result.Frames {
		if frame.Opcode == cosItemStateOpcode {
			state = true
		}
	}
	if !state {
		t.Fatal("cape equip omitted the summoner item-state frame")
	}
}

/*
================
TestCapeEquipDismissesDeadGrowthPet

The client's warning counts a dead but still-summoned pet as out
(presence only), so the equip retires the corpse's record the same way;
revival keeps working on the retained state.
================
*/
func TestCapeEquipDismissesDeadGrowthPet(t *testing.T) {
	rt, c, refs := capeUnsummonWorld(t)
	useSummonerFixture(t, rt, c, 23, refs.staticItemSource["SUMMON_ATTACK"])
	pet := c.Companions()[0]
	pet.CurrentHP = 0
	gid := pet.GID
	result := equipBagCape(t, rt, c)
	record := companionRecord(c, gid)
	if record == nil {
		t.Fatal("dead growth pet record lost")
	}
	if record.Summoned || record.StateFlags&cosStateSummoned != 0 {
		t.Fatal("cape equip left the dead growth pet summoned", record)
	}
	if record.CurrentHP != 0 {
		t.Fatal("cape equip revived the dead growth pet", record)
	}
	if notice, _ := removalPublished(result, gid); !notice {
		t.Fatal("cape equip published no detach notice for the dead pet")
	}
}

/*
================
TestCapeEquipLeavesPickupPetSummoned

Only the growth pet is named: the pickup (band-4) companion keeps its
summon and no release publishes.
================
*/
func TestCapeEquipLeavesPickupPetSummoned(t *testing.T) {
	rt, c, refs := capeUnsummonWorld(t)
	useSummonerFixture(t, rt, c, 24, refs.staticItemSource["SUMMON_PICKUP"])
	pet := c.Companions()[0]
	if !pet.Summoned {
		t.Fatal("fixture: pickup pet not summoned")
	}
	gid := pet.GID
	result := equipBagCape(t, rt, c)
	record := companionRecord(c, gid)
	if record == nil || !record.Summoned {
		t.Fatal("cape equip dismissed the pickup pet", record)
	}
	if notice, despawn := removalPublished(result, gid); notice || despawn {
		t.Fatalf("cape equip published a pickup-pet release: notice=%t despawn=%t", notice, despawn)
	}
}

/*
================
TestCapeEquipWithoutPetPublishesNoRemoval

No companion out: the equip is a plain move and nothing else publishes.
================
*/
func TestCapeEquipWithoutPetPublishesNoRemoval(t *testing.T) {
	rt, c, _ := capeUnsummonWorld(t)
	result := equipBagCape(t, rt, c)
	for _, frame := range result.Frames {
		if frame.Opcode == cosPetUpdateOpcode {
			t.Fatal("petless cape equip published a detach notice", result)
		}
	}
	for _, frame := range result.Broadcast {
		if frame.Opcode == wire.OpObjectDespawn {
			t.Fatal("petless cape equip broadcast a despawn", result)
		}
	}
}
