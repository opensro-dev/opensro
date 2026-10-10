/*
===========================================================================

petgrowth_test.go - attack pets level through their reference chain

===========================================================================
*/

package action

import (
	"bytes"
	"testing"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// petWolfTid is TID 1.2.3.3: an attack pet.
const petWolfTid uint16 = 0x19c6

/*
================
petGrowthFixture

A level 5 owner whose summoned COS_P_WOLF_001 grows through 002 and 003;
leveldata asks 100, 200 and 300 EXP at levels 1, 2 and 3.
================
*/
func petGrowthFixture(t *testing.T) (*Runtime, *enterworld.Character, *enterworld.CharacterCOS) {
	t.Helper()
	c := testCharacter()
	level := int64(5)
	c.Level = &level
	source := testCosSource(testItems())
	for index, name := range []string{"COS_P_WOLF_001", "COS_P_WOLF_002", "COS_P_WOLF_003"} {
		next := ""
		if index < 2 {
			next = []string{"COS_P_WOLF_002", "COS_P_WOLF_003"}[index]
		}
		source.characters[name] = &enterworld.CharacterRef{RefObjID: 6106 + uint32(index), TidWord: petWolfTid,
			Codename: name, NextCodename: next, Level: uint8(index + 1), MaxHP: 400 + 100*uint32(index), MaxMP: 50}
	}
	rt, _ := newTestRuntime(c, source)
	rt.deps.(*enterworld.Deps).Levels = combatRewardLevels{exp: map[int64]int64{1: 100, 2: 200, 3: 300}}
	gid, _ := enterworld.CosObjectIDForCharacter(c)
	c.ActiveCOS = &enterworld.CharacterCOS{GID: gid, RefObjID: 6106, Codename: "COS_P_WOLF_001", Level: 1,
		CurrentHP: 120, Satiety: 4000, Summoned: true}
	return rt, c, c.ActiveCOS
}

/*
================
TestAttackPetWalksItsFormsAcrossLevels

350 EXP at level 1 pays 100 and 200 and leaves 50 at level 3, as the
third form with full vitals and satiety.
================
*/
func TestAttackPetWalksItsFormsAcrossLevels(t *testing.T) {
	rt, c, pet := petGrowthFixture(t)
	ref, _ := rt.cosReference(pet)
	owner, area := rt.applyAttackPetExperience(c, pet, ref, 350, 9001)
	if pet.Codename != "COS_P_WOLF_003" || pet.Level != 3 || pet.Experience != 50 || pet.CurrentHP != 600 ||
		pet.CurrentMP != 50 || pet.Satiety != petLevelUpSatiety {
		t.Fatalf("grown pet %+v", pet)
	}
	if len(owner) != 1 || !bytes.Equal(owner[0].Payload, petExperienceFrame(pet.GID, 350, 9001).Payload) {
		t.Fatalf("owner frames %+v", owner)
	}
	assertOpcodes(t, area, opCosInfoUpdate, wire.OpLevelUpEffect, simulation.OpVitalsUpdate)
	if !bytes.Equal(area[0].Payload, wire.NewWriter(9).U32(pet.GID).U8(cosInfoReference).U32(6108).Payload()) {
		t.Fatalf("reference frame %x", area[0].Payload)
	}
}

/*
================
TestAttackPetAtItsOwnersLevelOnlyBanks

4D6370: a pet level with its owner keeps EXP one short of its next level.
================
*/
func TestAttackPetAtItsOwnersLevelOnlyBanks(t *testing.T) {
	rt, c, pet := petGrowthFixture(t)
	level := int64(1)
	c.Level = &level
	ref, _ := rt.cosReference(pet)
	owner, area := rt.applyAttackPetExperience(c, pet, ref, 500, 0)
	if pet.Level != 1 || pet.Experience != 99 || len(area) != 0 ||
		!bytes.Equal(owner[0].Payload, petExperienceFrame(pet.GID, 99, 0).Payload) {
		t.Fatalf("banked pet %+v frames %+v", pet, owner)
	}
	if owner, _ := rt.applyAttackPetExperience(c, pet, ref, 10, 0); len(owner) != 0 || pet.Experience != 99 {
		t.Fatal("a full bank still moved")
	}
}

/*
================
TestOwnersKillFeedsTheSummonedAttackPet

A level 1 monster worth 100 EXP, killed outright, gives a level 1 pet the
player formula's award without the mastery gap, which crosses level 2.
================
*/
func TestOwnersKillFeedsTheSummonedAttackPet(t *testing.T) {
	rt, c, pet := petGrowthFixture(t)
	target := monster.Instance{Gid: 9001, Ref: monster.MonsterRef{RefObjID: 1, Level: 1, MaxHP: 50,
		ExpToGive: 100, RewardActionPinned: true}}
	award := attackPetKillExperience(1, target, 50)
	owner, _ := rt.awardAttackPetExperience(c, target, 50, 1, 0)
	if award < 100 || len(owner) != 1 || !bytes.Equal(owner[0].Payload, petExperienceFrame(pet.GID, award, 9001).Payload) ||
		pet.Codename != "COS_P_WOLF_002" || pet.Experience != uint64(award-100) {
		t.Fatalf("pet after the kill %+v", pet)
	}
	pet.Summoned = false
	if owner, _ := rt.awardAttackPetExperience(c, target, 50, 1, 0); len(owner) != 0 {
		t.Fatal("an unsummoned pet grew")
	}
}

/*
================
TestPlayerKillFeedsThePetTheLowerLevelsBasis

4FCDA0: leveldata +0x1C of min(pet, victim) level, doubled for a murderer.
================
*/
func TestPlayerKillFeedsThePetTheLowerLevelsBasis(t *testing.T) {
	rt, c, pet := petGrowthFixture(t)
	rt.deps.(*enterworld.Deps).Levels = combatRewardLevels{exp: map[int64]int64{1: 100, 2: 200, 3: 300},
		basis: map[int64]int64{1: 24, 4: 94}}
	victim := testCharacter()
	victimLevel := int64(4)
	victim.Level = &victimLevel
	if owner, _ := rt.awardAttackPetPvPExperience(c, victim); len(owner) != 1 || pet.Experience != 24 {
		t.Fatalf("pet after the kill %+v", pet)
	}
	victim.PK = &domain.PKRecord{Penalty: 1}
	if _, _ = rt.awardAttackPetPvPExperience(c, victim); pet.Experience != 72 {
		t.Fatalf("a murderer victim paid %d", pet.Experience-24)
	}
}

/*
================
TestBetaPetExpGrowsAtTheOwnersPace

Port-only, not native: with the closed-beta growth on, a pet's gain is
multiplied by the pace of its OWNER's level (owner decision 2026-10-11).
Off (nil) is native. A pace below one never shrinks a gain.
================
*/
func TestBetaPetExpGrowsAtTheOwnersPace(t *testing.T) {
	rt, c, pet := petGrowthFixture(t)
	ref, _ := rt.cosReference(pet)
	if owner, _ := rt.applyAttackPetExperience(c, pet, ref, 50, 0); pet.Level != 1 || pet.Experience != 50 ||
		!bytes.Equal(owner[0].Payload, petExperienceFrame(pet.GID, 50, 0).Payload) {
		t.Fatalf("native gain %+v", pet)
	}

	rt, c, pet = petGrowthFixture(t)
	ref, _ = rt.cosReference(pet)
	asked := int64(-1)
	rt.PetExpPace = func(ownerLevel int64) float64 {
		asked = ownerLevel
		return 3
	}
	owner, _ := rt.applyAttackPetExperience(c, pet, ref, 50, 0)
	if asked != 5 {
		t.Fatalf("pace read at level %d, want the owner's 5 (the pet is level 1)", asked)
	}
	// 150 at level 1 pays its 100 and leaves 50 at level 2.
	if pet.Level != 2 || pet.Experience != 50 || !bytes.Equal(owner[0].Payload, petExperienceFrame(pet.GID, 150, 0).Payload) {
		t.Fatalf("beta gain %+v frames %+v", pet, owner)
	}

	rt, c, pet = petGrowthFixture(t)
	ref, _ = rt.cosReference(pet)
	rt.PetExpPace = func(int64) float64 { return 0.25 }
	if rt.applyAttackPetExperience(c, pet, ref, 50, 0); pet.Experience != 50 {
		t.Fatalf("a pace below one shrank the gain: %+v", pet)
	}
}
