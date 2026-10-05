/*
===========================================================================

noise_test.go - the Bard's Noise keeps aggressive monsters from striking first

The shipped Noise row is cast through HandleTargetInteract; an aggressive
monster then runs the real monster leg, wired with the action owner's
first-attack guard as production wires it (Bard specification, rule 5).

===========================================================================
*/

package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// noiseCode is Noise's last tier, dura(900000) pola(1,100).
	noiseCode = "SKILL_EU_BARD_FORGETA_ATTACK_A_08"
	// noiseDuration is dura's word.
	noiseDuration = 900000 * time.Millisecond
	// noiseSight is the fixture monster's aggressive sight.
	noiseSight = 100
	// noiseDistance places the Bard well inside that sight.
	noiseDistance = 20
	// everyEffectEvent raises every event bit an action can retire
	// effects on (5A16C0), the casts of the Bard's own attacks included.
	everyEffectEvent = 0xff
)

/*
================
noiseField

A Bard noiseDistance from an aggressive monster that idles where it
spawned.
================
*/
type noiseField struct {
	rt     *Runtime
	clock  *fakeClock
	c      *enterworld.Character
	mob    monster.Instance
	ops    *simulation.MonsterMoverOps
	viewer simulation.SessionSnapshot
}

/*
================
newNoiseField
================
*/
func newNoiseField(t *testing.T) noiseField {
	t.Helper()
	rt, clock, c, original := newCombatTestRuntime(t, 1000)
	skills := rt.deps.SkillData().(staticSkillSource)
	attack := skills[temptationAttackSkill]
	attack.Attack.Min, attack.Attack.Max, attack.Attack.Percent = 1, 1, 100
	skills[temptationAttackSkill] = attack
	ref := original.Ref
	ref.DefaultSkillIDs[0], ref.RunSpeed, ref.WalkSpeed, ref.ScaleDenom = temptationAttackSkill, 22, 8, 100
	ref.TidWord, ref.TypeID4 = regularMonsterTID, regularMonsterTID4
	state := simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref}, nil))
	state.SetTimeSource(clock.Now)
	state.SetRandomSource(func() float64 { return 0 })
	state.SetAbnormalContext(monsterAbnormalContext{rt})
	rt.Monsters = state
	at := monster.Pose{RegionID: original.Spawn.RegionID, X: original.Spawn.X, Y: original.Spawn.Y, Z: original.Spawn.Z}
	mob, err := state.DevelopmentCreateLeader(testDivision, ref.RefObjID, at, clock.NowMs()+int64(2*noiseDuration/time.Millisecond))
	if err != nil {
		t.Fatal(err)
	}
	lease, ok := state.ObjectPopulation(testDivision, mob.Gid)
	if !ok {
		t.Fatal("missing population")
	}
	rt.Worlds.Update(simulation.WorldKey(testDivision, c.Name), func() simulation.WorldState { return simulation.SeedWorldState(c) },
		func(w *simulation.WorldState) {
			w.Spawn = simulation.Spawn{RegionID: at.RegionID, X: at.X - noiseDistance, Y: at.Y, Z: at.Z}
			w.SpawnSet = true
		})
	*c.World.Spawn.X = at.X - noiseDistance
	viewer := simulation.SessionSnapshot{SessionID: "noise", DivisionID: testDivision, CharacterID: c.ID,
		Population: lease, WorldInstance: uint32(lease.ID), BodyRadius: 4, CombatEligible: true,
		World: simulation.SeedWorldState(c)}
	aggressive := func(monster.Instance) monster.Tactics {
		return monster.Tactics{Aggressive: true, SightRange: noiseSight}
	}
	ops := &simulation.MonsterMoverOps{Monsters: state, Rand: func() float64 { return 0 }, AttackPlan: rt.MonsterAttackPlan,
		RunAction: rt.RunMonsterAction, TacticsFor: aggressive, FirstAttackGuard: rt.FirstAttackGuard}
	return noiseField{rt: rt, clock: clock, c: c, mob: mob, ops: ops, viewer: viewer}
}

/*
================
castNoise

The Bard plays the shipped Noise row on itself.
================
*/
func (f noiseField) castNoise(t *testing.T) enterworld.SkillRow {
	t.Helper()
	skill := shippedOffense(t, noiseCode)
	f.rt.deps.SkillData().(staticSkillSource)[skill.ID] = skill
	f.c.RaceIndex = testInt64(enterworld.RaceEurope)
	f.c.ModelCodename = "CHAR_EU_MAN_NOBLE"
	f.c.Skills = []uint32{skill.ID}
	f.c.Intellect = testInt64(2000)
	f.c.CurrentMP = testInt64(100000)
	weapon := f.rt.deps.ItemReferences().(staticItemSource)[f.c.MissionInventory[0].Codename]
	weapon.TypeIDs[3] = bardHarpKind
	f.c.MissionInventory[0].TypeFlags = weapon.TypeFlags()
	out := f.rt.HandleTargetInteract(testDivision, f.c, wire.SkillAction{ActionId: skill.ID}.Encode())
	if !f.noised(skill) {
		t.Fatalf("Noise was not installed: refusal %q frames %+v", out.DiagnosticRefusal, out.Frames)
	}
	return skill
}

/*
================
noised
================
*/
func (f noiseField) noised(skill enterworld.SkillRow) bool {
	for _, effect := range f.rt.effects.Snapshot(testDivision, f.c.Name) {
		if effect.SkillID == skill.ID {
			return true
		}
	}
	return false
}

/*
================
expire

The action tick's effect phase: expiry, then retirement.
================
*/
func (f noiseField) expire() {
	f.rt.effects.Expire(f.clock.NowMs())
	f.rt.drainStoppedCharacterEffects()
}

/*
================
targetsBard

Step the monster leg until the monster owns the Bard as its target.
================
*/
func (f noiseField) targetsBard() bool {
	gid := enterworld.ObjectIDForCharacter(f.c)
	for range temptationTicks {
		f.clock.Advance(temptationTick)
		f.expire()
		f.ops.RunMonsterLeg(f.clock.NowMs(), []simulation.SessionSnapshot{f.viewer}, &summonTickPusher{})
		if mover, _ := f.rt.Monsters.Mover(testDivision, f.mob.Gid); mover.TargetGID() == gid {
			return true
		}
	}
	return false
}

/*
================
TestNoiseStopsAggressiveMonstersAttackingFirst

The aggressive monster in sight never acquires the Bard under Noise. The
Bard then attacks it: Noise stays, and the monster fights back.
================
*/
func TestNoiseStopsAggressiveMonstersAttackingFirst(t *testing.T) {
	f := newNoiseField(t)
	skill := f.castNoise(t)
	if f.targetsBard() {
		t.Fatal("an aggressive monster attacked the Bard first under Noise")
	}

	// The Bard attacks the monster: the events its cast raises, then the
	// hit's aggression through the shared hostility owner.
	gid := enterworld.ObjectIDForCharacter(f.c)
	f.rt.deps.Update(f.c, "noise-test-attack", func() bool {
		f.rt.retireEffectsOnEvent(testDivision, f.c, everyEffectEvent, f.clock.NowMs())
		return true
	})
	f.rt.commitAggression(testDivision, f.mob.Gid, simulation.HostilityEvent{Attacker: gid, Damage: 1, Aggression: 1}, f.clock.NowMs())
	if !f.noised(skill) {
		t.Fatal("the Bard's attack ended Noise")
	}
	hp := enterworld.CurrentHP(f.c)
	f.targetsBard()
	for range temptationTicks {
		if enterworld.CurrentHP(f.c) < hp {
			break
		}
		f.clock.Advance(temptationTick)
		f.ops.RunMonsterLeg(f.clock.NowMs(), []simulation.SessionSnapshot{f.viewer}, &summonTickPusher{})
	}
	if enterworld.CurrentHP(f.c) >= hp {
		mover, _ := f.rt.Monsters.Mover(testDivision, f.mob.Gid)
		t.Fatalf("the attacked monster did not fight back: HP %d, mover %+v", enterworld.CurrentHP(f.c), mover)
	}
	if !f.noised(skill) {
		t.Fatal("Noise ended while the monster fought back")
	}
}

/*
================
TestNoiseLastsFifteenMinutes

Just before dura runs out the monster still leaves the Bard alone; once
it has, the monster acquires the Bard like any player.
================
*/
func TestNoiseLastsFifteenMinutes(t *testing.T) {
	f := newNoiseField(t)
	skill := f.castNoise(t)
	f.clock.Advance(noiseDuration - temptationTicks*temptationTick - time.Second)
	f.expire()
	if !f.noised(skill) {
		t.Fatal("Noise ended before its 15 minutes")
	}
	if f.targetsBard() {
		t.Fatal("the monster attacked the Bard first before Noise ended")
	}
	f.clock.Advance(2 * time.Second)
	f.expire()
	if f.noised(skill) {
		t.Fatal("Noise outlived its 15 minutes")
	}
	if !f.targetsBard() {
		t.Fatal("the monster never acquired the Bard once Noise ended")
	}
}
