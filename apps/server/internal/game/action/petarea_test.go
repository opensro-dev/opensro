/*
===========================================================================

petarea_test.go - the companion area pre-refusal and its copy saving

The ordinary pet validation refuses an aggression weight of exactly
one on the target's record alone, so petAreaTargets refuses that
player before its copy; the soldier's world-enemy query admits any
target whose aggression map is non-empty (presence, never the weights),
so a weight-one player is its genuine candidate and keeps its copy. Both verdicts are pinned
against the validations themselves, and the refused walk is pinned
against the copy it no longer pays for.

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestPetAreaWeightRefusalMatchesThePetValidation

The pair's admitting relation (the owner's aggression toward the
victim, which the validation admits on only after the weight test)
stays armed throughout, so the weight arm is the only fact that can
move the verdict: a weight of one is refused by the pre-refusal and
by the pet validation alike, and a weight of two is refused by
neither.
================
*/
func TestPetAreaWeightRefusalMatchesThePetValidation(t *testing.T) {
	rt, clock, owner, victim := newPvpPair(t)
	ownerGID := enterworld.ObjectIDForCharacter(owner)

	victim.Aggressions = map[uint32]uint32{ownerGID: 1}
	if !areaTargetAggressionRefusal(victim) {
		t.Fatal("the pre-refusal does not refuse a weight of one")
	}
	if code := rt.playerAttackRefusal(testDivision, owner, victim, clock.NowMs(), petAttackArea); code == 0 {
		t.Fatal("the pre-refusal refuses a player the pet validation admits")
	}

	victim.Aggressions = nil
	if areaTargetAggressionRefusal(victim) {
		t.Fatal("the pre-refusal refuses a player with no aggressions")
	}
	victim.Aggressions = map[uint32]uint32{ownerGID: 2}
	if areaTargetAggressionRefusal(victim) {
		t.Fatal("the pre-refusal refuses a weight of two")
	}
	if code := rt.playerAttackRefusal(testDivision, owner, victim, clock.NowMs(), petAttackArea); code != 0 {
		t.Fatalf("the pet validation does not admit the weight-two pair: %#x", code)
	}
}

/*
================
newPetAreaStage

One owner with an ordered companion and one present victim, staged
the way the free-battle team test stages its pair: the primary is an
already-admitted dummy that supplies only the area centre.
================
*/
func newPetAreaStage(t *testing.T, band uint16, present bool) (*Runtime, *fakeClock, *enterworld.Character, *enterworld.Character, petCombatStep, petCombatTarget, enterworld.SkillRow) {
	t.Helper()
	rt, clock, owner, victim := newPvpPair(t)
	ref := equipCombatTestPet(t, rt, owner, band)
	rt.BindPetSession(testDivision, owner, 1)
	pet := *owner.ActiveCOS
	var valid bool
	pet.GID, valid = enterworld.CosObjectIDForCharacter(victim)
	if !valid {
		t.Fatal("victim has no companion identity")
	}
	victim.ActiveCOS = &pet
	if present {
		rt.BindPetSession(testDivision, victim, 1)
	}
	rt.RewardActorPresent = func(string, string) bool { return true }
	owner.Level, victim.Level = testInt64(playerCombatMinimumLevel), testInt64(playerCombatMinimumLevel)
	state := rt.petSessionFor(testDivision, owner.Name, owner.ActiveCOS.GID)
	at := rt.liveSpawn(simulation.WorldKey(testDivision, owner.Name), owner, clock.NowMs())
	state.follower = simulation.NewPetFollower(owner.ActiveCOS.GID, at)
	step := petCombatStep{
		key:   petOwnerKey{division: testDivision, name: owner.Name, gid: owner.ActiveCOS.GID},
		state: state, snapshot: owner, pet: owner.ActiveCOS, ref: ref, nowMs: clock.NowMs(),
	}
	primary := petCombatTarget{combatTarget: combatTarget{gid: 999999, at: at}}
	skill := enterworld.SkillRow{ActionArea: enterworld.SkillOffensiveArea{Shape: 2, Radius: 20, MaxTargets: 3, Select: 24}}
	return rt, clock, owner, victim, step, primary, skill
}

/*
================
petAreaFindsVictim
================
*/
func petAreaFindsVictim(targets []petCombatTarget, victim *enterworld.Character) bool {
	for _, target := range targets {
		if target.gid == enterworld.ObjectIDForCharacter(victim) {
			return true
		}
	}
	return false
}

/*
================
TestPetAreaTargetsRefusesAWeightOnePlayerWithoutACopy

A weight-one victim of an ordered attack pet is no candidate, and
the walk that refuses it never detaches its record; a weight-two
victim stays a candidate. A guild soldier's world-enemy query admits
the same weight-one victim (it reads only that aggressions exist),
so the soldier pays for the copy and keeps the candidate.
================
*/
func TestPetAreaTargetsRefusesAWeightOnePlayerWithoutACopy(t *testing.T) {
	rt, _, owner, victim, step, primary, skill := newPetAreaStage(t, attackPetBand, true)
	ownerGID := enterworld.ObjectIDForCharacter(owner)

	victim.Aggressions = map[uint32]uint32{ownerGID: 1}
	if petAreaFindsVictim(rt.petAreaTargets(step, primary, skill), victim) {
		t.Fatal("a weight-one victim is a companion area candidate")
	}
	var copySink *enterworld.Character
	copyAllocs := testing.AllocsPerRun(20, func() {
		copySink = rt.characterSnapshot(testDivision, victim)
	})
	if copySink == nil {
		t.Fatal("the victim does not snapshot")
	}
	var walkSink []petCombatTarget
	refusedAllocs := testing.AllocsPerRun(20, func() {
		walkSink = rt.petAreaTargets(step, primary, skill)
	})
	if len(walkSink) != 0 {
		t.Fatalf("the refused walk returns %d targets", len(walkSink))
	}

	// The same walk with the victim not present prices the walk
	// itself (the presence list, the owner's own copy, the
	// selection). Refusing her may add only the door pass that reads
	// her weights; copying her would add her whole detached record
	// on top.
	presentFn := rt.RewardActorPresent
	rt.RewardActorPresent = func(division, name string) bool {
		return name != victim.Name && presentFn(division, name)
	}
	absentAllocs := testing.AllocsPerRun(20, func() {
		walkSink = rt.petAreaTargets(step, primary, skill)
	})
	rt.RewardActorPresent = presentFn
	// Measured copy 26, absent walk 34, refused walk 40.
	t.Logf("copy %v, absent walk %v, refused walk %v", copyAllocs, absentAllocs, refusedAllocs)
	if refusedAllocs-absentAllocs >= copyAllocs/2 {
		t.Fatalf("refusing the victim adds %v over her absence, half the %v copy already convicts it",
			refusedAllocs-absentAllocs, copyAllocs)
	}

	victim.Aggressions = map[uint32]uint32{ownerGID: 2}
	if !petAreaFindsVictim(rt.petAreaTargets(step, primary, skill), victim) {
		t.Fatal("a weight-two victim is no companion area candidate")
	}

	soldierRT, _, soldierOwner, soldierVictim, soldierStep, soldierPrimary, soldierSkill := newPetAreaStage(t, domain.MercenaryBand, true)
	soldierVictim.Aggressions = map[uint32]uint32{enterworld.ObjectIDForCharacter(soldierOwner): 1}
	if !areaTargetAggressionRefusal(soldierVictim) {
		t.Fatal("the soldier control victim carries no weight of one")
	}
	if !petAreaFindsVictim(soldierRT.petAreaTargets(soldierStep, soldierPrimary, soldierSkill), soldierVictim) {
		t.Fatal("a soldier lost its weight-one candidate to the pre-refusal")
	}
}
