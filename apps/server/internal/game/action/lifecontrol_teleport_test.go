/*
===========================================================================

lifecontrol_teleport_test.go - a buff that halves maximum HP across a teleport

Native teleport keeps the same CGObjPC (no teleport path calls
CSkillManager_CancelAllBuffSkills 5A0100), so a buff survives it and a
second cast of the same group still meets the first.

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestLifeControlSurvivesTeleportWithoutStacking

Cast Life Control, teleport, cast it again: the maximum HP stays at half
and the character keeps its HP.
================
*/
func TestLifeControlSurvivesTeleportWithoutStacking(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 1000000)
	skill := shippedOffense(t, "SKILL_EU_WIZARD_MENTALA_DAMAGEUP_A_01")
	rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	c.RaceIndex = testInt64(enterworld.RaceEurope)
	c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	c.Skills = []uint32{skill.ID}
	c.Intellect = testInt64(2000)
	c.CurrentMP = testInt64(100000)
	weapon := rt.deps.ItemReferences().(staticItemSource)[c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = 11
	c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	base, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	baseHP, _ := base.Param(3)
	cast := func() byte {
		now := clock.NowMs()
		out := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID}.Encode())
		frame, ok := findFrame(out.Frames, wire.OpSkillCastResult)
		if !ok {
			t.Fatal("no cast result")
		}
		rt.advanceProjectileCasts(now + int64(skill.ActionCastingTimeMs) + 1)
		rt.drainStoppedCharacterEffects()
		wait := max(skill.ActionCastingTimeMs+skill.ActionDurationMs, skill.CoolTimeMs) + 1
		clock.Advance(time.Duration(wait) * time.Millisecond)
		rt.drainSkillFinalizes(clock.NowMs())
		return frame.Payload[0]
	}
	if cast() != 1 {
		t.Fatal("first cast refused")
	}
	destination := rt.liveSpawn(simulation.WorldKey(testDivision, c.Name), c, clock.NowMs())
	destination.X += 50
	rt.commitGateTravel(gateTravel{division: testDivision, character: c, destination: destination, world: instance.ID(domain.CharacterWorldInstance(c)), reason: "test-teleport"},
		func() (int64, OpResult, bool) { return 0, OpResult{}, true })
	cast()
	stats, _, err := rt.playerCombatStats(testDivision, c)
	if err != nil {
		t.Fatal(err)
	}
	maxHP, _ := stats.Param(3)
	if maxHP != baseHP*0.5 {
		t.Fatalf("maximum HP %v after teleport and recast, want half of %v", maxHP, baseHP)
	}
	if enterworld.CurrentHP(c) == 0 {
		t.Fatal("recast after teleport killed the caster")
	}
}
