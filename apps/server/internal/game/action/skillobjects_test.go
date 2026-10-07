/*
===========================================================================

skillobjects_test.go - item debit through world-object capture and retirement

Exercise the action owner with a real population and skill-object registry.
Quest admission and capture remain explicit boundaries; their inventory and
journal transactions are independently tested by the quest package.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestQuestTrapConsumesOnlyAfterAdmissionAndCapturesOnce

A refused placement cannot debit the stack. An admitted object captures its
first eligible live target once, with world retirement preceding quest award.
================
*/
func TestQuestTrapConsumesOnlyAfterAdmissionAndCapturesOnce(t *testing.T) {
	const targetID = 14769
	const targetCode = "MOB_AM_ROGUE"
	rt, clock, c, request := statItemFixture(t, enterworld.SkillTimedEffect{})
	skills := rt.deps.(*enterworld.Deps).Skills.(namedItemSkills)
	skill := skills.staticSkillSource[100]
	skill.CastGate.QuestTrap = enterworld.SkillQuestTrap{
		Present: true, DurationMs: 300000, ScanMs: 300, Radius: 20, Targets: [3]uint32{targetID},
	}
	skills.staticSkillSource[100] = skill
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{
		targetID: {RefObjID: targetID, Codename: targetCode, MaxHP: 100, TidWord: 0xc6},
	}, nil))
	rt.Monsters.SetTimeSource(clock.Now)
	if err := rt.admitPopulationSession(testDivision, c.Name, 1); err != nil {
		t.Fatal(err)
	}
	admitted := false
	rt.CanPlaceQuestTrap = func(_ *enterworld.Character, code string) ([]wire.Frame, bool) {
		if code != skill.Codename {
			t.Fatal("wrong associated skill", code)
		}
		return nil, admitted
	}
	awards := 0
	rt.CaptureQuestTrap = func(_ *enterworld.Character, code, target string, retire func() bool) ([]wire.Frame, bool) {
		if code != skill.Codename || target != targetCode || !retire() {
			t.Fatal("capture did not bind and retire its target")
		}
		awards++
		return []wire.Frame{{Opcode: 0x36bf, Payload: []byte{0, 0}}}, true
	}
	rt.HandleItemUse(testDivision, c, request)
	if c.MissionInventory[0].StackCount != 2 || len(rt.SkillObjects.Snapshot()) != 0 {
		t.Fatal("quest refusal consumed a trap or admitted an object")
	}
	admitted = true
	result := rt.HandleItemUse(testDivision, c, request)
	objects := rt.SkillObjects.Snapshot()
	if len(objects) != 1 || c.MissionInventory[0].StackCount != 1 || result.Frames[0].Payload[0] != 1 {
		t.Fatal("placement did not atomically admit and consume", result, objects)
	}
	rt.HandleItemUse(testDivision, c, request)
	if c.MissionInventory[0].StackCount != 1 || len(rt.SkillObjects.Snapshot()) != 1 {
		t.Fatal("overlapping placement bypassed the native qest gate")
	}
	object := objects[0]
	rt.Monsters.SetRandomSource(func() float64 { return 0 })
	if !rt.Monsters.SpawnQuestMonster(simulation.QuestMonsterSpawn{
		Division: testDivision, Population: object.Population, Codename: targetCode, NowMs: clock.NowMs(),
		Position: simulation.Spawn{RegionID: object.Spawn.Region, X: float64(object.Spawn.X),
			Y: float64(object.Spawn.Y), Z: float64(object.Spawn.Z)},
	}) {
		t.Fatal("target admission failed")
	}
	rt.AdvanceSkillObjects(clock.NowMs()+300, nil)
	if awards != 1 || len(rt.SkillObjects.Snapshot()) != 0 {
		t.Fatal("matching target did not retire the trap exactly once", awards)
	}
	rt.AdvanceSkillObjects(clock.NowMs()+600, nil)
	if awards != 1 {
		t.Fatal("retired trap awarded twice")
	}
}
