/*
===========================================================================

abnormaladmit_test.go - abnormal and strike admissions read facts, not copies

The per-tick abnormal lanes and the monster strike admission detached
whole character records - inventory included - for refusals that weigh
a handful of fields: a victim's life, deletion and job class, a pet's
summons, a member's life, a revival candidate's death kind (#1943).
The verdicts stay; the refused copies go. Landing behaviour stays
covered by the lane suites (pet counter, Guardian bond, linked
shares, scream mask, monster combat).

===========================================================================
*/
package action

import (
	"testing"

	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
fattenInventory

Give a character a full inventory so one detached copy is expensive.
================
*/
func fattenInventory(c *enterworld.Character) {
	// Bag slots only: equipment and job-suit slots keep their verdicts.
	rows := make([]enterworld.InventoryRow, 0, 48)
	for slot := 0; slot < 48; slot++ {
		rows = append(rows, enterworld.InventoryRow{Slot: int64(21 + slot), RefObjID: 62, StackCount: 1000,
			MagicOptions: []uint64{uint64(slot), uint64(slot + 1)}})
	}
	c.MissionInventory = append(c.MissionInventory, rows...)
}

/*
================
TestMonsterAttackAdmissionRefusalsDoNotCopyVictim

A living victim is struck; a dead or deletion-pending one is refused
before its record is detached.
================
*/
func TestMonsterAttackAdmissionRefusalsDoNotCopyVictim(t *testing.T) {
	arm := func(rt *Runtime, mob monster.Instance) monster.Instance {
		skills := rt.deps.SkillData().(staticSkillSource)
		attack := skills[2]
		attack.Attack.Min, attack.Attack.Max, attack.Attack.Percent = 60, 60, 100
		skills[2] = attack
		mob.Ref.DefaultSkillIDs[0] = 2
		return mob
	}
	rt, clock, c, mob := newCombatTestRuntime(t, 100000)
	fattenInventory(c)
	gid := enterworld.ObjectIDForCharacter(c)
	if result := rt.MonsterBasicAttack(testDivision, arm(rt, mob), gid, 2, clock.NowMs()); !result.Accepted {
		t.Fatal("living victim was not struck")
	}

	rt, clock, c, mob = newCombatTestRuntime(t, 100000)
	mob = arm(rt, mob)
	fattenInventory(c)
	*c.CurrentHP = 0
	gid = enterworld.ObjectIDForCharacter(c)
	if result := rt.MonsterBasicAttack(testDivision, mob, gid, 2, clock.NowMs()); result.Accepted {
		t.Fatal("dead victim was struck")
	}
	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, c) })
	deadAllocs := testing.AllocsPerRun(20, func() {
		rt.MonsterBasicAttack(testDivision, mob, gid, 2, clock.NowMs())
	})
	t.Logf("allocs: one victim copy %.0f, refused attack %.0f", copyAllocs, deadAllocs)
	if deadAllocs >= copyAllocs {
		t.Fatalf("refused attack still copies the victim: %.0f allocs; one copy is %.0f", deadAllocs, copyAllocs)
	}

	rt, clock, c, mob = newCombatTestRuntime(t, 100000)
	mob = arm(rt, mob)
	c.DeletePending = true
	if result := rt.MonsterBasicAttack(testDivision, mob, enterworld.ObjectIDForCharacter(c), 2, clock.NowMs()); result.Accepted {
		t.Fatal("deletion-pending victim was struck")
	}
}

/*
================
TestCharacterPartyJobClassReadsValueNotCopy

The COS job test reads the owner's class as a door value: suited and
unsuited owners answer exactly what the detached record answered.
================
*/
func TestCharacterPartyJobClassReadsValueNotCopy(t *testing.T) {
	rt, _, c, _ := newCombatTestRuntime(t, 100000)
	fattenInventory(c)
	if got, want := rt.characterPartyJobClass(testDivision, c), enterworld.PartyJobClass(c); got != want {
		t.Fatalf("unsuited class %d, want %d", got, want)
	}
	c.MissionInventory = append(c.MissionInventory, enterworld.InventoryRow{Slot: enterworld.JobSuitSlot,
		RefObjID: 9101, Codename: "ITEM_CH_M_TRADE_TRADER_04", TypeFlags: wire.PackTypeFlags(3, 1, 7, 1), StackCount: 1})
	if got, want := rt.characterPartyJobClass(testDivision, c), enterworld.PartyJobClass(c); got != want || want == enterworld.NoJobClass {
		t.Fatalf("suited class %d, want %d", got, want)
	}
	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, c) })
	classAllocs := testing.AllocsPerRun(20, func() { rt.characterPartyJobClass(testDivision, c) })
	t.Logf("allocs: one owner copy %.0f, job class read %.0f", copyAllocs, classAllocs)
	if classAllocs >= copyAllocs {
		t.Fatalf("job class still copies the owner: %.0f allocs; one copy is %.0f", classAllocs, copyAllocs)
	}
	if got := rt.characterPartyJobClass(testDivision, nil); got != enterworld.NoJobClass {
		t.Fatalf("nil owner class %d, want %d", got, enterworld.NoJobClass)
	}
}

/*
================
TestPetCounterStunSkipsCopyWithoutLivingBear

A raven owner and a dead bear owner both answer no frames, and
neither detaches the owner record to learn it.
================
*/
func TestPetCounterStunSkipsCopyWithoutLivingBear(t *testing.T) {
	rt, clock, c, original := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
	rt.AttackPetExpansion = true
	ref := original.Ref
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref}, nil))
	hit := petCounterHit{division: testDivision, owner: c, victim: enterworld.ObjectIDForCharacter(c),
		attacker: 424242, formulas: []combat.Result{{Damage: 1}}, now: clock.NowMs()}
	if frames := rt.petCounterStun(hit); frames != nil {
		t.Fatalf("raven owner countered: %+v", frames)
	}
	fattenInventory(c)
	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, c) })
	counterAllocs := testing.AllocsPerRun(20, func() { rt.petCounterStun(hit) })
	t.Logf("allocs: one owner copy %.0f, bear-less counter %.0f", copyAllocs, counterAllocs)
	if counterAllocs >= copyAllocs {
		t.Fatalf("bear-less counter still copies the owner: %.0f allocs; one copy is %.0f", counterAllocs, copyAllocs)
	}

	rt, clock, c, _ = newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	rt.AttackPetExpansion = true
	rt.Monsters = simulation.NewMonsterState(monster.TemplateFromParts(map[uint32]monster.MonsterRef{ref.RefObjID: ref}, nil))
	*c.CurrentHP = 0
	hit = petCounterHit{division: testDivision, owner: c, victim: enterworld.ObjectIDForCharacter(c),
		attacker: 424242, formulas: []combat.Result{{Damage: 1}}, now: clock.NowMs()}
	if frames := rt.petCounterStun(hit); frames != nil {
		t.Fatalf("dead bear owner countered: %+v", frames)
	}
}

/*
================
TestBondRevivalSkipsLivingMembersWithoutCopy

A tier-III bear whose party is alive revives nobody and detaches no
member record to learn it; the owner's own admission copy is the
only one left.
================
*/
func TestBondRevivalSkipsLivingMembersWithoutCopy(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_BEAR_090", 90)
	roarField(t, rt, c, clock)
	c.MissionInventory = nil
	rt.AttackPetExpansion = true
	learnPetBook(t, c, 27)
	near := nearbyCharacter(rt, c, 21, "bond-near", 50)
	far := nearbyCharacter(rt, c, 22, "bond-far", 60)
	near.ActiveCOS, far.ActiveCOS = nil, nil
	fattenInventory(near)
	fattenInventory(far)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c),
			enterworld.ObjectIDForCharacter(near), enterworld.ObjectIDForCharacter(far)}}}
	}
	key := petOwnerKey{testDivision, c.Name, c.ActiveCOS.GID}
	state := rt.petSessionFor(key.division, key.name, key.gid)
	if frames := rt.advancePetBond(key, state, clock.NowMs()); len(frames) != 0 {
		t.Fatalf("living party revived: %+v", frames)
	}
	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, near) })
	bondAllocs := testing.AllocsPerRun(20, func() { rt.advancePetBond(key, state, clock.NowMs()) })
	t.Logf("allocs: one member copy %.0f, living-party revival pass %.0f", copyAllocs, bondAllocs)
	// The pass keeps its own fixed cost (the bear owner's admission,
	// the party walk) under one member copy; the two member copies it
	// used to add on top would put it past two.
	if bondAllocs >= 2*copyAllocs {
		t.Fatalf("revival pass still copies living members: %.0f allocs; one member copy is %.0f", bondAllocs, copyAllocs)
	}
}

/*
================
TestQuotaMembersSkipDeadMemberWithoutCopy

A dead party member is out of the Pain Quota plan, and learning it
detaches no member record.
================
*/
func TestQuotaMembersSkipDeadMemberWithoutCopy(t *testing.T) {
	rt, c, warrior, now := linkedShareFixture(t, 0, 35)
	rt.RewardParties = func(string) []RewardParty {
		return []RewardParty{{Members: []uint32{enterworld.ObjectIDForCharacter(c), enterworld.ObjectIDForCharacter(warrior)}}}
	}
	if members := rt.planQuotaMembers(testDivision, c, now); len(members) != 1 {
		t.Fatalf("living member not planned: %v", members)
	}
	*warrior.CurrentHP = 0
	if members := rt.planQuotaMembers(testDivision, c, now); len(members) != 0 {
		t.Fatalf("dead member planned: %v", members)
	}
	fattenInventory(warrior)
	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, warrior) })
	planAllocs := testing.AllocsPerRun(20, func() { rt.planQuotaMembers(testDivision, c, now) })
	t.Logf("allocs: one member copy %.0f, dead-member quota plan %.0f", copyAllocs, planAllocs)
	if planAllocs >= copyAllocs {
		t.Fatalf("quota plan still copies dead members: %.0f allocs; one copy is %.0f", planAllocs, copyAllocs)
	}
}

/*
================
TestScreamMaskSourceRefusesWithoutCopy

A member with no live Scream Mask link is no source, and learning it
detaches no record.
================
*/
func TestScreamMaskSourceRefusesWithoutCopy(t *testing.T) {
	rt, clock, c, _ := newCombatTestRuntime(t, 10000)
	fattenInventory(c)
	if _, _, ok := rt.screamMaskSource(testDivision, c, monster.Pose{}, clock.NowMs()); ok {
		t.Fatal("linkless member became a scream source")
	}
	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, c) })
	sourceAllocs := testing.AllocsPerRun(20, func() {
		rt.screamMaskSource(testDivision, c, monster.Pose{}, clock.NowMs())
	})
	t.Logf("allocs: one member copy %.0f, linkless source check %.0f", copyAllocs, sourceAllocs)
	if sourceAllocs >= copyAllocs {
		t.Fatalf("linkless source check still copies the member: %.0f allocs; one copy is %.0f", sourceAllocs, copyAllocs)
	}
}

/*
================
TestAreaCompanionRefusalSkipsOwnerCopy

An area victim whose pet is dead is refused before its owner's
record is detached.
================
*/
func TestAreaCompanionRefusalSkipsOwnerCopy(t *testing.T) {
	rt, clock, c, _ := newPetCombatRuntime(t, 100000, attackPetBand)
	makeTrainedPet(rt, c, "COS_P_RAVEN_040", 90)
	c.ActiveCOS.CurrentHP = 0
	fattenInventory(c)
	in := monsterStrikeInput{division: testDivision, now: clock.NowMs()}
	victim := monsterAreaVictim{owner: c, gid: c.ActiveCOS.GID, companion: true}
	if outcome := rt.strikeMonsterAreaVictim(in, victim); outcome.committed {
		t.Fatal("dead pet was struck")
	}
	copyAllocs := testing.AllocsPerRun(20, func() { rt.characterSnapshot(testDivision, c) })
	victimAllocs := testing.AllocsPerRun(20, func() { rt.strikeMonsterAreaVictim(in, victim) })
	t.Logf("allocs: one owner copy %.0f, dead-pet area victim %.0f", copyAllocs, victimAllocs)
	if victimAllocs >= copyAllocs {
		t.Fatalf("dead-pet victim still copies the owner: %.0f allocs; one copy is %.0f", victimAllocs, copyAllocs)
	}
}
