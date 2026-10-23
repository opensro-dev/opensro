/*
===========================================================================

cosdeath_test.go - a dead COS, or a dead rider's vehicle, is released at once

The native COS death (pets and summons alike) and the native player death
both end in the COS release: the COS leaves the world. A pet's record stays on its item (kind 8); a vehicle's
is deleted (kind 1), and a vehicle's own death drops its cargo first.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
)

/*
================
despawnsGID

Whether frames carry the despawn of gid.
================
*/
func despawnsGID(frames []wire.Frame, gid uint32) bool {
	want := wire.ObjectDespawn{Gid: gid}.Encode()
	for _, frame := range frames {
		if frame.Opcode == wire.OpObjectDespawn && string(frame.Payload) == string(want) {
			return true
		}
	}
	return false
}

/*
================
TestRiderDeathReleasesTheRiddenVehicle
================
*/
func TestRiderDeathReleasesTheRiddenVehicle(t *testing.T) {
	rt, _, character, _ := newCombatTestRuntime(t, 100)
	ride := &enterworld.CharacterCOS{
		GID: 0x00C00003, RefObjID: 3914, Codename: "COS_T_DHORSE3",
		CurrentHP: 87829, Summoned: true, Mounted: true, StateFlags: cosStateSummoned | 1,
	}
	character.ActiveCOS = ride
	zero := int64(0)
	character.CurrentHP = &zero

	effects, _ := rt.settlePlayerDeathInDoor(testDivision, character, deathKiller{}, 1000)
	if ride.Summoned || ride.Mounted || ride.StateFlags&cosStateSummoned != 0 {
		t.Fatalf("the dead rider kept its vehicle: %+v", ride)
	}
	if ride.CurrentHP != 87829 {
		t.Fatal("releasing the vehicle changed its HP", ride.CurrentHP)
	}
	if character.ActiveCOS != nil {
		t.Fatal("the released vehicle kept its record (a kind 1 release deletes it)")
	}
	dismount, found := findFrame(effects, wire.OpCosRideState)
	if !found || dismount.Payload[4] != 0 {
		t.Fatalf("no dismount before the release: %+v", effects)
	}
	if !despawnsGID(effects, ride.GID) {
		t.Fatalf("the released vehicle stayed in the world: %+v", effects)
	}
}

/*
================
TestRiderDeathKeepsAnUnriddenCompanion

The native player death releases only the ridden vehicle; a pet beside its
owner lives on.
================
*/
func TestRiderDeathKeepsAnUnriddenCompanion(t *testing.T) {
	rt, _, character, _ := newCombatTestRuntime(t, 100)
	pet := &enterworld.CharacterCOS{
		GID: 0x00C00004, RefObjID: 3914, Codename: "COS_T_DHORSE3",
		CurrentHP: 87829, Summoned: true, StateFlags: cosStateSummoned | 1,
	}
	character.ActiveCOS = pet
	zero := int64(0)
	character.CurrentHP = &zero

	effects, _ := rt.settlePlayerDeathInDoor(testDivision, character, deathKiller{}, 1000)
	if !pet.Summoned || despawnsGID(effects, pet.GID) {
		t.Fatalf("an unridden companion died with its owner: %+v %+v", pet, effects)
	}
}

/*
================
TestStarvedPetIsReleased

A COS death (here hunger, through the shared fatal commit) publishes the
death, then releases the corpse; the record keeps its dead HP for revival.
================
*/
func TestStarvedPetIsReleased(t *testing.T) {
	items := shippedItems(t)
	c := testCharacter()
	rt, _ := newTestRuntime(c, items)
	equipShippedPet(t, rt, c, items, "COS_P_WOLF_002")
	pet := c.ActiveCOS
	pet.Satiety = 1
	pet.StateFlags = 3
	rt.BindPetSession(testDivision, c, 1)
	rt.storeCosAbnormal(testDivision, c.Name, pet.GID, &abnormal.Block{Mask: abnormal.Burn.Bit()})
	rt.advancePets(1000)
	output := rt.advancePets(70000)
	if pet.CurrentHP != 0 || pet.Summoned || pet.StateFlags&(1|cosStateSummoned) != 0 {
		t.Fatalf("the starved pet was not released: %+v", pet)
	}
	var frames []wire.Frame
	for _, batch := range output {
		for _, frame := range batch.Frames {
			frames = append(frames, wire.Frame{Opcode: frame.Opcode, Payload: frame.Payload})
		}
	}
	deadLife := wire.ObjectStateRefresh{Gid: pet.GID, StateType: wire.StateChannelLife, Value: wire.LifeStateDead}.Encode()
	died, released := -1, -1
	for i, frame := range frames {
		if frame.Opcode == wire.OpObjectStateRefresh && string(frame.Payload) == string(deadLife) {
			died = i
		}
		if despawnsGID(frames[i:i+1], pet.GID) {
			released = i
		}
	}
	if died < 0 || released < died {
		t.Fatalf("the corpse must publish its death before it leaves (dead %d, despawn %d): %+v", died, released, frames)
	}
	if !despawnsGID(frames, pet.GID) {
		t.Fatalf("the starved pet stayed in the world: %+v", frames)
	}
}

/*
================
TestTransportDeathDropsCargoAndDeletesRecord

A transport killed in the field drops its goods unowned around it, and the
native kind-1 release deletes its record; a pet
of the same death keeps its record for revival.
================
*/
func TestTransportDeathDropsCargoAndDeletesRecord(t *testing.T) {
	for _, band := range []uint16{cosBandTransport, cosBandAttackPet} {
		rt, clock, character, source := newCombatTestRuntime(t, 100)
		equipCombatTestPet(t, rt, character, band)
		pet := character.ActiveCOS
		potion := testItems()["ITEM_ETC_HP_POTION_01"]
		pet.Container = &domain.COSContainer{Capacity: 4, Rows: []enterworld.InventoryRow{{
			Slot: 0, RefObjID: potion.RefObjID, Codename: potion.Codename, TypeFlags: potion.TypeFlags(), StackCount: 7}}}
		pet.CurrentHP = 5
		record := abnormal.Record{Status: abnormal.Burn, Level: 1, Grade: 1, DurationMs: 10000,
			PeriodMs: 2000, SourceGID: source.Gid, Rate24: 10, Scale20: 1, Param38: 10}
		owner := rt.newCosAbnormalOwner(testDivision, character, clock.NowMs())
		owner.sources = rt.captureAbnormalSources(testDivision, owner.block, []abnormal.Record{record})
		if !owner.block.Apply(owner, record, clock.NowMs()) {
			t.Fatal("status refused")
		}
		owner.block.Update(owner, clock.NowMs())
		owner.commit()
		frames := rt.cosAbnormalPublication(pet.GID, owner)
		ground := rt.Ground.All(testDivision)
		if band == cosBandAttackPet {
			if character.ActiveCOS != pet || len(ground) != 0 {
				t.Fatalf("a dead pet lost its record or dropped %d items", len(ground))
			}
			continue
		}
		if character.ActiveCOS != nil {
			t.Fatal("a dead transport kept its record")
		}
		if len(ground) != 1 || ground[0].Codename != potion.Codename || ground[0].StackCount != 7 || ground[0].DroppedBy != "" {
			t.Fatalf("cargo %+v", ground)
		}
		if !saw(frames, wire.OpObjectDespawn) {
			t.Fatal("the dead transport stayed in view")
		}
	}
}
