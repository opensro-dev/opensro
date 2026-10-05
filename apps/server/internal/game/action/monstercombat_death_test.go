/*
===========================================================================

monstercombat_death_test.go - monster attack plans and fatal monster attacks

Shipped attack plans are runnable with their casting and recovery phases,
and a fatal monster attack publishes the death and its progression.

===========================================================================
*/
package action

import (
	"encoding/binary"
	"opensro.online/server/internal/game/pk"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
)

/*
================
TestShippedMangnyangAttackPlanIsRunnable
================
*/
func TestShippedMangnyangAttackPlanIsRunnable(t *testing.T) {
	dir := licensed.RetailTextdataDir(t)
	ref, ok := monster.LoadMonsterRefs(dir)[1933]
	if !ok {
		t.Skip("shipped characterdata is unavailable")
	}
	rt := NewRuntime(&enterworld.Deps{Skills: enterworld.NewTextdataSkills(dir)}, nil)
	plan, ok := rt.MonsterAttackPlan(monster.Instance{Ref: ref}, 0, 0)
	if !ok || plan.SkillID != 160 || plan.Reach != 10 || plan.CooldownMs != 3000 ||
		plan.ActionLifecycleMs != 2400 {
		t.Fatalf("Mangnyang attack plan = %+v/%v, want skill 160 range 10 cooldown 3000ms lifecycle 2400ms", plan, ok)
	}
}

/*
================
TestShippedMoviaAttackPlanIncludesCastingAndRecoveryPhases
================
*/
func TestShippedMoviaAttackPlanIncludesCastingAndRecoveryPhases(t *testing.T) {
	dir := licensed.RetailTextdataDir(t)
	ref, ok := monster.LoadMonsterRefs(dir)[5850]
	if !ok {
		t.Skip("shipped characterdata is unavailable")
	}
	rt := NewRuntime(&enterworld.Deps{Skills: enterworld.NewTextdataSkills(dir)}, nil)
	for skillID, wantDuration := range map[uint32]int64{3598: 2000, 3599: 2000} {
		plan, planned := rt.MonsterAttackPlan(monster.Instance{Ref: ref}, skillID, 0)
		if !planned || plan.SkillID != skillID || plan.ActionLifecycleMs != wantDuration ||
			plan.CooldownMs != 2500 {
			t.Fatalf("Movia skill %d plan = %+v/%v, want lifecycle %dms cooldown 2500ms",
				skillID, plan, planned, wantDuration)
		}
	}
}

/*
================
TestMonsterAttackFinalizeWaitsForCastingPlusRecovery
================
*/
func TestMonsterAttackFinalizeWaitsForCastingPlusRecovery(t *testing.T) {
	rt, clock, character, monster := newCombatTestRuntime(t, 100)
	monster.Ref.DefaultSkillIDs[0] = 2
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.ActionCastingTimeMs = 1077
	skill.ActionDurationMs = 923
	skill.Attack.Min = 1
	skill.Attack.Max = 1
	skill.Attack.Percent = 100
	skills[2] = skill

	before := enterworld.CurrentHP(character)
	result := rt.MonsterBasicAttack(
		testDivision,
		monster,
		enterworld.ObjectIDForCharacter(character),
		2,
		clock.NowMs(),
	)
	if !result.Accepted || len(result.Frames) == 0 {
		t.Fatalf("monster attack = %+v, want accepted B245", result)
	}
	if len(result.Frames[0].Payload) != 19 || result.Frames[0].Payload[18] != 0 || enterworld.CurrentHP(character) != before {
		t.Fatalf("preparation committed results or HP: %+v", result)
	}
	token := binary.LittleEndian.Uint32(result.Frames[0].Payload[10:14])
	if frames := rt.MonsterActionTickHook()(clock.At(1077 * time.Millisecond).UnixMilli()); len(frames) != 0 {
		t.Fatalf("SHOT release escaped before casting time: %+v", frames)
	}
	released := rt.MonsterActionTickHook()(clock.At(1078 * time.Millisecond).UnixMilli())
	if enterworld.CurrentHP(character) >= before {
		t.Fatal("release did not commit HP")
	}
	if len(released) != 1 || len(released[0].Frames) != 1 ||
		binary.LittleEndian.Uint32(released[0].Frames[0].Payload[1:]) != token ||
		binary.LittleEndian.Uint32(released[0].Frames[0].Payload[5:]) != enterworld.ObjectIDForCharacter(character) {
		t.Fatalf("casting-time release lost token/target identity: %+v", released)
	}
	assertOnlySkillReleases(t, rt.MonsterActionTickHook()(clock.At(2000*time.Millisecond).UnixMilli()))
	assertSkillCastClose(t, rt.MonsterActionTickHook()(clock.At(2001*time.Millisecond).UnixMilli()), testDivision, token)
}

/*
================
TestMonsterAttackTreatsAbsentCurrentHPAsFullAndMaterializesDamage
================
*/
func TestMonsterAttackTreatsAbsentCurrentHPAsFullAndMaterializesDamage(t *testing.T) {
	rt, clock, character, instance := newCombatTestRuntime(t, 100)
	character.CurrentHP = nil
	instance.Ref.DefaultSkillIDs[0] = 2
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Attack.Min = 1
	skill.Attack.Max = 1
	skill.Attack.Percent = 100
	skills[2] = skill

	result := rt.MonsterBasicAttack(
		testDivision,
		instance,
		enterworld.ObjectIDForCharacter(character),
		2,
		clock.NowMs(),
	)
	if !result.Accepted || !result.TargetAlive {
		t.Fatalf("fresh-character monster attack = %+v, want accepted nonfatal damage", result)
	}
	if character.CurrentHP == nil || *character.CurrentHP >= enterworld.DerivedMaxHP(character) {
		t.Fatalf("monster damage left currentHp = %v, want materialized value below full", character.CurrentHP)
	}
}

/*
================
TestFatalMonsterAttackPublishesLifeDead
================
*/
func TestFatalMonsterAttackPublishesLifeDead(t *testing.T) {
	rt, clock, character, monster := newCombatTestRuntime(t, 100)
	monster.Ref.DefaultSkillIDs[0] = 2
	character.CurrentHP = testInt64(1)
	character.BattleUntilMs = 0 // battle exit on death is pinned by the battle-state tests
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Attack.Min = 100
	skill.Attack.Max = 100
	skill.Attack.Percent = 100
	skills[2] = skill

	result := rt.MonsterBasicAttack(
		testDivision,
		monster,
		enterworld.ObjectIDForCharacter(character),
		2,
		clock.NowMs(),
	)
	if !result.Accepted || result.TargetAlive {
		t.Fatalf("fatal monster attack = accepted %v, targetAlive %v", result.Accepted, result.TargetAlive)
	}
	if len(result.Frames) != 3 ||
		result.Frames[1].Opcode != simulation.OpVitalsUpdate ||
		result.Frames[2].Opcode != wire.OpObjectStateRefresh {
		t.Fatalf("fatal monster frames = %+v, want damage + HP-zero baseline + LIFE-dead", result.Frames)
	}
	vitals := result.Frames[1].Payload
	if len(vitals) != 11 ||
		binary.LittleEndian.Uint32(vitals[0:4]) != enterworld.ObjectIDForCharacter(character) ||
		binary.LittleEndian.Uint16(vitals[4:6]) != 0x0004 ||
		vitals[6] != 0x01 ||
		binary.LittleEndian.Uint32(vitals[7:11]) != 0 {
		t.Fatalf("fatal HP baseline payload = %x, want gid + source 0x0004 + HP-only zero", vitals)
	}
	life, err := wire.DecodeObjectStateRefresh(result.Frames[2].Payload)
	if err != nil || life.Gid != enterworld.ObjectIDForCharacter(character) ||
		life.StateType != wire.StateChannelLife || life.Value != wire.LifeStateDead {
		t.Fatalf("fatal LIFE frame = %+v / %v", life, err)
	}
}

/*
================
TestFatalMonsterAttackCommitsAndReturnsDeathProgression
================
*/
func TestFatalMonsterAttackCommitsAndReturnsDeathProgression(t *testing.T) {
	rt, clock, character, monster := newCombatTestRuntime(t, 100)
	monster.Ref.DefaultSkillIDs[0] = 2
	character.Level = testInt64(11)
	character.MaxLevel = testInt64(11)
	character.Experience = testInt64(1000)
	character.CurrentHP = testInt64(1)
	skills := rt.deps.SkillData().(staticSkillSource)
	skill := skills[2]
	skill.Attack.Min = 100
	skill.Attack.Max = 100
	skill.Attack.Percent = 100
	skills[2] = skill

	penaltyCalls := 0
	rt.ApplyDeathPenalty = func(c *enterworld.Character, _ pk.DeathPenalty) ([]wire.Frame, bool) {
		penaltyCalls++
		if c.CurrentHP == nil || *c.CurrentHP != 0 {
			t.Fatalf("death updater observed HP %v, want fatal HP committed inside the same door", c.CurrentHP)
		}
		next := *c.Experience - 697
		c.Experience = &next
		return []wire.Frame{{Opcode: wire.OpExpUpdate, Payload: []byte{0xD0}}}, true
	}
	var pushed [][]wire.Frame
	rt.PushCharacterFrames = func(divisionID, characterName string, frames []wire.Frame) {
		if divisionID != testDivision || characterName != character.Name {
			t.Fatalf("death progression target = %q/%q", divisionID, characterName)
		}
		pushed = append(pushed, append([]wire.Frame(nil), frames...))
	}

	result := rt.MonsterBasicAttack(
		testDivision,
		monster,
		enterworld.ObjectIDForCharacter(character),
		2,
		clock.NowMs(),
	)
	if !result.Accepted || result.TargetAlive || penaltyCalls != 1 || *character.Experience != 303 {
		t.Fatalf("fatal death transaction = accepted/alive/calls/exp %v/%v/%d/%d",
			result.Accepted, result.TargetAlive, penaltyCalls, *character.Experience)
	}
	if len(pushed) != 0 {
		t.Fatalf("monster combat invoked an asynchronous character callback: %+v", pushed)
	}
	if private := privateFramesOf(result); len(private) != 1 || private[0].Opcode != wire.OpExpUpdate {
		t.Fatalf("same-turn death progression = %+v, want one target-only 30D2", private)
	}
	assertOnlySkillReleases(t, rt.TickHook()(clock.NowMs()))
	if len(pushed) != 0 {
		t.Fatalf("death progression replayed through a callback: %+v", pushed)
	}
}
