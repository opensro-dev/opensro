/*
===========================================================================

cosdeath_test.go - a dead COS, or a dead rider's vehicle, is released at once

CGObjCOS_ProcessNormalDeath (52A000; pets 529F70, summoned 529F10) and
CGObjPC_ProcessNormalDeath (529B10) both end in ReleaseCOSOrExit: the COS
leaves the world and its record stays on the owner's item.

===========================================================================
*/
package action

import (
	"testing"

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

529B10 releases only the ridden vehicle (+0x30); a pet beside its owner
lives on.
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
