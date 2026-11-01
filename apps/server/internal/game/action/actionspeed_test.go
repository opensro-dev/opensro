/*
===========================================================================

actionspeed_test.go - effective action speed at publication and entry

Status callbacks must reach both existing viewers and replacement snapshots.
The animation denominator is independent of haste and movement speed.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"math"
	"testing"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
assertActionSpeed
================
*/
func assertActionSpeed(t *testing.T, frames []wire.Frame, gid uint32, want float32) {
	t.Helper()
	count := 0
	for _, frame := range frames {
		if frame.Opcode != wire.OpActionSpeed {
			continue
		}
		count++
		if len(frame.Payload) != 8 || binary.LittleEndian.Uint32(frame.Payload) != gid ||
			binary.LittleEndian.Uint32(frame.Payload[4:]) != math.Float32bits(want) {
			t.Fatalf("action speed payload %x, want GID %d denominator %v", frame.Payload, gid, want)
		}
	}
	if count != 1 {
		t.Fatalf("action speed publication count %d, want one", count)
	}
}

/*
================
TestPlayerActionSpeedPublicationAndEntry
================
*/
func TestPlayerActionSpeedPublicationAndEntry(t *testing.T) {
	for _, status := range []abnormal.Status{abnormal.Slow, abnormal.Frostbite} {
		rt, clock, c, source := newCombatTestRuntime(t, 100)
		want := float32(125)
		if status == abnormal.Frostbite {
			want = 200
		}
		record := abnormal.Record{Status: status, Level: 1, Grade: 1, DurationMs: 1000, SourceGID: source.Gid}
		owner := rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{record}, clock.NowMs())
		assertActionSpeed(t, rt.playerAbnormalPublication(testDivision, c, owner).public, enterworld.ObjectIDForCharacter(c), want)
		if got := rt.EntryActionSpeed(testDivision, c.Name); got != want {
			t.Fatalf("entry speed %v, want %v", got, want)
		}
		owner = rt.clearPlayerAbnormalInDoor(testDivision, c, clock.NowMs()+1)
		assertActionSpeed(t, rt.playerAbnormalPublication(testDivision, c, owner).public, enterworld.ObjectIDForCharacter(c), 100)
		if got := rt.EntryActionSpeed(testDivision, c.Name); got != 100 {
			t.Fatalf("cleared entry speed %v", got)
		}
	}
}

/*
================
TestEntryActionSpeedCastCopyMatchesFullCopy

The entry read takes the cast copy (#2140): every reader of that copy
is cleared for the dropped plain bag rows (see EntryActionSpeed). Pin
this root against the full copy over the states whose result the
copy's kept fields decide - a fattened plain record, an abnormal
haste, and a wolf-howl grant walking the companions - each also held
to its known value, so the agreement cannot be vacuous.
================
*/
func TestEntryActionSpeedCastCopyMatchesFullCopy(t *testing.T) {
	rt, clock, c, source := newCombatTestRuntime(t, 100)
	// Fatten the record the way a settled player carries it: plain bag
	// rows (the cast copy drops these) and one companion-carrying row
	// (it must not).
	for slot := int64(21); slot < 45; slot++ {
		c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: slot, RefObjID: 3632, StackCount: 1})
	}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: 45, RefObjID: 3632, StackCount: 1,
		Summon: &enterworld.CharacterCOS{GID: 0x00C00009, Codename: "COS_P_WOLF"}})

	speeds := func() (float32, float32) {
		cast := rt.EntryActionSpeed(testDivision, c.Name)
		rt.castCopy = func(divisionID string, character *enterworld.Character) *enterworld.Character {
			return rt.characterSnapshot(divisionID, character)
		}
		full := rt.EntryActionSpeed(testDivision, c.Name)
		rt.castCopy = nil
		return cast, full
	}
	check := func(name string, want float32) {
		t.Helper()
		cast, full := speeds()
		if cast != full || cast != want {
			t.Fatalf("%s: cast %v, full %v, want %v", name, cast, full, want)
		}
	}

	check("plain record", 100)

	record := abnormal.Record{Status: abnormal.Slow, Level: 1, Grade: 1, DurationMs: 1000, SourceGID: source.Gid}
	rt.applyPlayerAbnormalInDoor(testDivision, c, false, []abnormal.Record{record}, clock.NowMs())
	check("slowed", 125)
	rt.clearPlayerAbnormalInDoor(testDivision, c, clock.NowMs()+1)
	check("cleared", 100)

	// A wolf-howl grant reaches the owner through its companions: the
	// one stat writer that walks the copy's companion rows. The grant
	// refuses a town, so step out of the fixture's town first.
	equipStrikeTestCOS(t, rt, c)
	c.World.Spawn.RegionID = testInt64(0x62a6)
	rt.Worlds.Forget(simulation.WorldKey(testDivision, c.Name))
	rt.AttackPetExpansion = true
	if rt.wolfHowls.rows == nil {
		rt.wolfHowls.rows = map[petOwnerKey]wolfHowl{}
	}
	rt.wolfHowls.rows[petOwnerKey{division: testDivision, name: "howl", gid: 999}] = wolfHowl{
		until: clock.NowMs() + 8000, percent: 500,
		members: map[uint32]string{enterworld.ObjectIDForCharacter(c): c.Name},
	}
	check("howl grant", float32(100.0/6))
	*c.CurrentHP = 0
	check("howl grant, dead owner", 100)
	*c.CurrentHP = 100
}

/*
================
TestCompanionActionSpeedPublicationAndEntry
================
*/
func TestCompanionActionSpeedPublicationAndEntry(t *testing.T) {
	for _, mounted := range []bool{false, true} {
		rt, clock, c, source := newCombatTestRuntime(t, 100)
		equipCombatTestPet(t, rt, c, 2)
		pet := c.ActiveCOS
		pet.Mounted = mounted
		owner := rt.newCosAbnormalOwner(testDivision, c, clock.NowMs())
		record := abnormal.Record{Status: abnormal.Frostbite, Grade: 1, DurationMs: 1000, SourceGID: source.Gid}
		owner.sources = rt.captureAbnormalSources(testDivision, owner.block, []abnormal.Record{record})
		owner.changed = owner.block.Apply(owner, record, clock.NowMs())
		owner.commit()
		assertActionSpeed(t, rt.cosAbnormalPublication(pet.GID, owner), pet.GID, 200)
		if got := rt.EntryCompanionActionSpeed(testDivision, c, pet); got != 200 {
			t.Fatalf("mounted %v entry speed %v", mounted, got)
		}
		owner = rt.newCosAbnormalOwner(testDivision, c, clock.NowMs()+1)
		owner.changed = owner.block.ClearAll(owner)
		owner.commit()
		assertActionSpeed(t, rt.cosAbnormalPublication(pet.GID, owner), pet.GID, 100)
	}
}

/*
================
TestMonsterActionSpeedPublicationAndScopeEntry
================
*/
func TestMonsterActionSpeedPublicationAndScopeEntry(t *testing.T) {
	rt, clock, _, instance := newCombatTestRuntime(t, 100)
	for _, denominator := range []float32{200, 100} {
		instance.Abnormal = &abnormal.Block{}
		instance.Abnormal.Modifiers[0] = abnormal.Modifier{Used: true, Param: actionSpeedParameter, Value: denominator}
		frames := rt.monsterAbnormalFrames(testDivision, instance, simulation.MonsterAbnormalEffects{SpeedChanged: true})
		assertActionSpeed(t, frames, instance.Gid, denominator)
		if got := simulation.MonsterWireDefFromInstance(instance, clock.NowMs()).ScaleDenom; got != float64(denominator) {
			t.Fatalf("scope entry speed %v, want %v", got, denominator)
		}
	}
}
