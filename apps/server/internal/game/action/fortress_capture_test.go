package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/siege"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
captureFixture

A Jangan fortress world with one fort stone and one guard tower, the
fixture character inside it in guild 77, and every frame pushed to it
recorded by subtype.
================
*/
func captureFixture(t *testing.T) (*Runtime, *enterworld.Character, *fakeClock, *[]byte) {
	t.Helper()
	template := monster.TemplateFromParts(
		map[uint32]monster.MonsterRef{
			19553: {RefObjID: 19553, MaxHP: 900, ScaleDenom: 100, Structure: true, TypeID4: structureKindFortStone},
			19536: {RefObjID: 19536, MaxHP: 500, ScaleDenom: 100, Structure: true, TypeID4: structureKindGuardTower},
		},
		[]monster.NestRow{
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: 19553, RegionID: 0x62aa, X: 100, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: 84},
			{WorldCode: "INS_FORT_JA", SpawnPoint: monster.SpawnPoint{RefObjID: 19536, RegionID: 0x62aa, X: 140, Y: 20, Z: 100}, PolicyPinned: true, MaxCount: 1, EventStructID: 85},
		},
	)
	rt, c, clock := fortressFixtureWithPopulation(t, testFieldFortGate, template)
	var subtypes []byte
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) {
		for _, frame := range frames {
			if frame.Opcode == opFortressWarState && len(frame.Payload) > 0 {
				subtypes = append(subtypes, frame.Payload[0])
			}
		}
	}
	rt.PushDivisionPeerFrames = func(_, _ string, _ []wire.Frame) {}
	guild := int64(77)
	c.GuildID = &guild
	enterFortress(t, rt, c)
	rt.Monsters.AdvancePopulation(clock.NowMs() + monster.NestHiveTickMs)
	if rt.Monsters.StandingStructures(testDivision, instance.Pack(2, 1), structureKindGuardTower) != 1 {
		t.Fatal("the fortress world holds no guard tower")
	}
	return rt, c, clock, &subtypes
}

/*
================
killStructure

A fatal hit credited to c, then the death the combat owners queue.
================
*/
func killStructure(t *testing.T, rt *Runtime, c *enterworld.Character, kind uint8, nowMs int64) []simulation.DivisionFrames {
	t.Helper()
	lease, _ := rt.Monsters.PopulationLease(testDivision, instance.Pack(2, 1))
	for _, row := range rt.Monsters.PopulationInstances(testDivision, lease, []uint16{0x62aa}) {
		if row.Ref.TypeID4 != kind || row.CurrentHP == 0 {
			continue
		}
		results, ok := rt.Monsters.ApplyDamageBatch(testDivision, []simulation.MonsterDamagePlan{{
			GID: row.Gid, ExpectedHP: row.CurrentHP, Damage: row.CurrentHP, CreditGID: enterworld.ObjectIDForCharacter(c),
		}})
		if !ok || len(results) != 1 || !results[0].Fatal {
			t.Fatalf("structure %d did not die", row.Ref.RefObjID)
		}
		rt.queueMonsterDefeat(testDivision, row.Gid, nowMs)
		return rt.drainStructureDeaths(nowMs)
	}
	t.Fatalf("no standing structure of kind %d", kind)
	return nil
}

/*
================
TestTheStoneFallsToTheGuildThatBreaksIt

52D2B0 keeps a fallen structure in its destroyed state and tells the
fortress world (0x0B); the last tower starts the stone's countdown (0x0A);
the stone's fall hands the fortress to the breaker's guild, reinstalls
the structures and announces the conquest (subtype 8).
================
*/
func TestTheStoneFallsToTheGuildThatBreaksIt(t *testing.T) {
	rt, c, clock, subtypes := captureFixture(t)
	now := clock.NowMs()
	rt.FortressWarChanged(testDivision, now, true)
	jangan := uint32(0)
	for _, record := range rt.Fortresses.Records(testDivision) {
		if record.CodeName == "FORTRESS_JANGAN" {
			jangan = record.ID
		}
	}
	if code := rt.Fortresses.StoneRefusal(testDivision, jangan, now); code == 0 {
		t.Fatal("the stone is open while its tower stands")
	}
	killStructure(t, rt, c, structureKindGuardTower, now)
	if string(*subtypes) != string([]byte{siege.SubtypeStructureState, siege.SubtypeTowersFallen}) {
		t.Fatalf("the tower's fall sent %x", *subtypes)
	}
	if rt.Monsters.StandingStructures(testDivision, instance.Pack(2, 1), structureKindGuardTower) != 0 {
		t.Fatal("the destroyed tower stands")
	}
	*subtypes = nil
	out := killStructure(t, rt, c, structureKindFortStone, now+1000)
	record, _ := rt.Fortresses.Get(testDivision, jangan)
	if record.Holder() != 77 || record.EntryOpen {
		t.Fatalf("after the stone fell %+v", record)
	}
	if len(out) != 1 || out[0].Frames[0].Payload[0] != siege.SubtypeConquest {
		t.Fatalf("conquest broadcast %+v", out)
	}
	if rt.Monsters.StandingStructures(testDivision, instance.Pack(2, 1), structureKindGuardTower) != 1 {
		t.Fatal("the capture did not reinstall the tower")
	}
	if c.World.PackedInstance == nil {
		t.Fatal("the new holder was sent away")
	}
	if owner, changed := rt.Fortresses.FinishWar(testDivision, jangan); owner != 77 || !changed {
		t.Fatalf("war end = %d %v", owner, changed)
	}
}
