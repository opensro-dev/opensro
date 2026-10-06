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
