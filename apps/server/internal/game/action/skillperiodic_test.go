/*
===========================================================================

skillperiodic_test.go - persistent attacks through the real action authority

The cast pays once, closes independently and leaves damage to the pulse clock.
Tests inspect committed monster HP and routed packets rather than source text.

===========================================================================
*/

package action

import (
	"encoding/binary"
	"testing"
	"time"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/linkedpulse"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const testPeriodicSkillID = 9105

/*
================
periodicSourceGuard

The production source lookup reads character authority. Fail synchronously
instead of hanging when a hit tries to call it from a character write door.
================
*/
type periodicSourceGuard struct {
	simulation.MonsterAbnormalContext
	t       *testing.T
	writing *bool
	lookups int
}

/*
================
SourceExists

Exercise the real callback boundary, including successful status application.
================
*/
func (g *periodicSourceGuard) SourceExists(division string, gid uint32, name string) bool {
	g.lookups++
	if *g.writing {
		g.t.Fatal("abnormal source lookup reentered character authority under its write lock")
	}
	return g.MonsterAbnormalContext.SourceExists(division, gid, name)
}

/*
================
TestPeriodicStatusDoesNotReenterCharacterWriteDoor

A damaging pulse can also apply Burn. No character resource changes on that
nonfatal tick, so the monster commit must not own the character write door.
================
*/
func TestPeriodicStatusDoesNotReenterCharacterWriteDoor(t *testing.T) {
	rt, clock, c, target := periodicFixture(t, 10000)
	index, _ := abnormal.SourceIndex(0x6275)
	source := rt.deps.SkillData().(staticSkillSource)
	skill := source[testPeriodicSkillID]
	skill.Abnormal.Params[index] = abnormal.Param{Present: true, Args: [6]uint32{28, 100, 4}}
	source[skill.ID] = skill
	writing := false
	guard := &periodicSourceGuard{MonsterAbnormalContext: monsterAbnormalContext{rt}, t: t, writing: &writing}
	rt.Monsters.SetAbnormalContext(guard)
	deps := rt.deps.(*enterworld.Deps)
	deps.UpdateCharacters = func(_ []*enterworld.Character, _ string, update func() bool) bool {
		writing = true
		defer func() { writing = false }()
		return update()
	}
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	rt.advancePeriodicEffects(clock.NowMs() + 2000)
	current, _ := rt.characterMonster(testDivision, c, target.Gid)
	if guard.lookups == 0 || current.Abnormal == nil || !current.Abnormal.Slots[abnormal.Burn].Active {
		t.Fatal("fixture did not exercise source lookup and Burn application")
	}
}

/*
================
TestPeriodicRoutingKeepsPrivateProgressionAddressable

Ticker rejects mixed private/interest destinations. A fatal pulse's private
rewards must name only their character, while public damage follows scope.
================
*/
func TestPeriodicRoutingKeepsPrivateProgressionAddressable(t *testing.T) {
	effect := linkedpulse.Effect{Division: testDivision, SourceGID: 100001}
	result := OpResult{Broadcast: []wire.Frame{{Opcode: wire.OpSkillPulse}},
		ActorPrivate: []wire.Frame{{Opcode: wire.OpExpUpdate}}}
	batches := periodicDivisionFrames(effect, &enterworld.Character{ID: 1}, result)
	if len(batches) != 2 || batches[0].SourceGID != effect.SourceGID || batches[0].OnlyCharacterID != 0 ||
		batches[1].OnlyCharacterID != 1 || batches[1].SourceGID != 0 {
		t.Fatalf("unaddressable pulse destinations: %+v", batches)
	}
}

/*
================
periodicFixture

Use the existing level-one combat catalogs; only the skill program differs.
================
*/
func periodicFixture(t *testing.T, hp uint32) (*Runtime, *fakeClock, *enterworld.Character, monster.Instance) {
	t.Helper()
	rt, clock, c, target := newCombatTestRuntime(t, hp)
	source := rt.deps.SkillData().(staticSkillSource)
	skill := source[2]
	skill.ID, skill.Group, skill.Codename = testPeriodicSkillID, 900, "TEST_LINKED_ATTACK"
	skill.CombatPinned, skill.DirectOffensePinned = false, false
	skill.Attack = enterworld.SkillAttack{}
	skill.ActionDurationMs = 500
	skill.Consumption = enterworld.SkillConsumption{Pinned: true, MP: 5}
	skill.EffectRider = true
	skill.TimedEffect.Periodic = enterworld.SkillPeriodicEffect{Pinned: true, DurationMs: 12000, PeriodMs: 2000,
		Link:   enterworld.SkillEffectLink{Present: true, Group: 9, MaxOutgoing: 2},
		Attack: enterworld.SkillAttack{Present: true, Flags: 8, Percent: 37, Min: 4, Max: 6, ImpactCount: 1}}
	source[skill.ID] = skill
	c.Skills = append(c.Skills, skill.ID)
	c.CurrentMP = testInt64(100)
	return rt, clock, c, target
}

/*
================
TestPeriodicCastChargesOnceAndClosesBeforeDamage
================
*/
func TestPeriodicCastChargesOnceAndClosesBeforeDamage(t *testing.T) {
	rt, clock, c, target := periodicFixture(t, 10000)
	start := clock.NowMs()
	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: testPeriodicSkillID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if _, ok := findFrame(result.Frames, wire.OpAttachedEffect); !ok {
		t.Fatalf("no linked recipient: %+v", result)
	}
	if *c.CurrentMP != 95 {
		t.Fatalf("cast MP = %d; want 95", *c.CurrentMP)
	}
	current, _ := rt.characterMonster(testDivision, c, target.Gid)
	if current.CurrentHP != target.CurrentHP {
		t.Fatal("installation caused an extra immediate hit")
	}
	installed := current.LinkedEffects
	if installed.Len() != 1 {
		t.Fatal("late viewer lost the linked recipient")
	}
	spawn := simulation.BuildMonsterCreateRow(simulation.MonsterWireDefFromInstance(current, start), target.Gid, simulation.Spawn{})
	const effectCountOffset = 44
	if spawn[effectCountOffset] != 1 || binary.LittleEndian.Uint32(spawn[effectCountOffset+1:]) != testPeriodicSkillID {
		t.Fatal("late-viewer wire row omitted the active skill")
	}
	for effect := range installed.Entries() {
		if binary.LittleEndian.Uint32(spawn[effectCountOffset+5:]) != effect.Token {
			t.Fatal("late viewer received a different recipient token")
		}
	}
	rt.ClearCombatIntent(testDivision, c.Name)
	rt.drainSkillFinalizes(start + 501)
	if rt.hasOpenSkillCast(testDivision, c.Name) {
		t.Fatal("persistent effect retained the casting bracket")
	}
	if frames := rt.advancePeriodicEffects(start + 1999); len(frames) != 0 {
		t.Fatalf("early pulse: %+v", frames)
	}
	frames := rt.advancePeriodicEffects(start + 2000)
	if len(frames) == 0 || len(frames[0].Frames) == 0 || frames[0].Frames[0].Opcode != wire.OpSkillPulse {
		t.Fatalf("missing pulse result: %+v", frames)
	}
	current, _ = rt.characterMonster(testDivision, c, target.Gid)
	if current.CurrentHP >= target.CurrentHP || *c.CurrentMP != 95 {
		t.Fatalf("pulse HP %d MP %d", current.CurrentHP, *c.CurrentMP)
	}
	if frames := rt.advancePeriodicEffects(start + 2000); len(frames) != 0 {
		t.Fatal("same logical instant dealt damage twice")
	}
	expiredAt := clock.At(13 * time.Second).UnixMilli()
	rt.advancePeriodicEffects(expiredAt)
	if len(rt.periodicEffects.Frame(expiredAt)) != 0 {
		t.Fatal("expired linked pair retained")
	}
	current, _ = rt.characterMonster(testDivision, c, target.Gid)
	if current.LinkedEffects.Len() != 0 || installed.Len() != 1 {
		t.Fatal("retirement failed to replace the immutable observer snapshot")
	}
}

/*
================
TestPeriodicFatalPulseUsesRewardAndLifeOwners
================
*/
func TestPeriodicFatalPulseUsesRewardAndLifeOwners(t *testing.T) {
	rt, clock, c, target := periodicFixture(t, 1)
	rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: testPeriodicSkillID, HasTarget: true, TargetGid: target.Gid}.Encode())
	frames := rt.advancePeriodicEffects(clock.NowMs() + 2000)
	current, _ := rt.characterMonster(testDivision, c, target.Gid)
	if current.CurrentHP != 0 || len(rt.periodicEffects.Frame(clock.NowMs()+2000)) != 0 {
		t.Fatalf("fatal pulse did not retire pair, HP %d", current.CurrentHP)
	}
	var pulse, life bool
	for _, batch := range frames {
		for _, frame := range batch.Frames {
			pulse = pulse || frame.Opcode == wire.OpSkillPulse
			life = life || frame.Opcode == monsterLifeDeadFrame(target.Gid).Opcode
		}
	}
	if !pulse || !life {
		t.Fatalf("fatal pulse/life = %v/%v", pulse, life)
	}
}

/*
================
TestPeriodicPreparationAndLogout

Positive casting time spends no MP until release. Destroying the source
between pulses retires both halves even though storage still has the actor.
================
*/
func TestPeriodicPreparationAndLogout(t *testing.T) {
	rt, clock, c, target := periodicFixture(t, 10000)
	source := rt.deps.SkillData().(staticSkillSource)
	skill := source[testPeriodicSkillID]
	skill.ActionCastingTimeMs = 334
	source[skill.ID] = skill
	start := clock.NowMs()
	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: target.Gid}.Encode())
	if _, ok := findFrame(result.Frames, wire.OpSkillCastResult); !ok || *c.CurrentMP != 100 {
		t.Fatalf("preparation result %+v, MP %d", result, *c.CurrentMP)
	}
	if frames := rt.advanceProjectileCasts(start + 334); len(frames) != 0 {
		t.Fatal("released at the strict casting-time boundary")
	}
	if frames := rt.advanceProjectileCasts(start + 335); len(frames) == 0 || *c.CurrentMP != 95 {
		t.Fatalf("release %+v, MP %d", frames, *c.CurrentMP)
	}
	if frames := rt.advancePeriodicEffects(start + 2334); len(frames) != 0 {
		t.Fatal("pulse clock started before activation")
	}
	rt.ForgetCharacter(testDivision, c.Name)
	frames := rt.advancePeriodicEffects(start + 2335)
	for _, batch := range frames {
		for _, frame := range batch.Frames {
			if frame.Opcode == wire.OpSkillPulse {
				t.Fatal("departed source dealt another pulse")
			}
		}
	}
	if len(rt.periodicEffects.Frame(start+2335)) != 0 {
		t.Fatal("logout retained linked effects")
	}
}

/*
================
TestPeriodicAreaInstallsIndependentVictimsWithOneCharge

Target-centered selection excludes vertical and distant candidates. Each
selected victim owns its own pulse while the cast pays only one MP charge.
================
*/
func TestPeriodicAreaInstallsIndependentVictimsWithOneCharge(t *testing.T) {
	rt, clock, c, _ := periodicFixture(t, 10000)
	areaRuntime, targets := areaFixture(t, 10000)
	rt.Monsters = areaRuntime.Monsters
	source := rt.deps.SkillData().(staticSkillSource)
	skill := source[testPeriodicSkillID]
	skill.TimedEffect.Periodic.Area = enterworld.SkillOffensiveArea{Shape: 2, Radius: 70, MaxTargets: 3, Select: 24}
	source[skill.ID] = skill
	result := rt.HandleTargetInteract(testDivision, c, wire.SkillAction{ActionId: skill.ID, HasTarget: true, TargetGid: targets[0].Gid}.Encode())
	var installed int
	for _, frame := range result.Broadcast {
		if frame.Opcode == wire.OpAttachedEffect {
			installed++
		}
	}
	if installed != 3 || *c.CurrentMP != 95 {
		t.Fatalf("area installed %d, MP %d", installed, *c.CurrentMP)
	}
	frames := rt.advancePeriodicEffects(clock.NowMs() + 2000)
	var pulses int
	for _, batch := range frames {
		for _, frame := range batch.Frames {
			if frame.Opcode == wire.OpSkillPulse {
				pulses++
			}
		}
	}
	if pulses != 3 || *c.CurrentMP != 95 {
		t.Fatalf("area pulses %d, MP %d", pulses, *c.CurrentMP)
	}
}
