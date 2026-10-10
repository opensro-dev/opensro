/*
===========================================================================

petgrowth.go - attack pets grow on their owner's kills

Every monster award an owner earns also feeds each summoned attack pet
(CGObjPC_GrantKillExperience 4EA6A0 -> CCOSManager_AwardAttackPetExperience
4FCB00): the player formula run for the pet's level without the mastery
gap (Formulae_CalculateExperience 410110), shared by the same recipient
factor, plus the owner's parameter-job percent and the pet's own 0xBA
percent (the growth potion's expi).

CGObjAttackCOS_ApplyExperienceDelta (4D6370) banks it. A pet at level 110
or at its owner's level stops one point short of its next level; any
other pet walks the leveldata curve, and each level is a new reference
(column 6, 4D6210): CGObjCOS_ReplaceReferenceAndResetVitals (4EFD10)
restores HP, MP and satiety and tells the area. The v1.150 client keeps
the curve too, so the owner only learns the delta:

	0x3508 [u32 cos][u8 3][i32 exp][u32 source]   owner   (v1.188 0x30C9 3)
	0x3508 [u32 cos][u8 7][u32 refObjID]          area    (v1.188 0x30C9 7)
	0x36B0 [u32 cos]                              area    (v1.188 0x3054)
	vitals refresh of the new HP and MP           area    (4EFD10 vfunc +0x274)

===========================================================================
*/

package action

import (
	"strings"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	opCosInfoUpdate   uint16 = 0x3508
	cosInfoExperience uint8  = 3
	cosInfoReference  uint8  = 7

	// petGrowthLevelCap is 4D6370's 0x6E: a pet this high only banks EXP.
	petGrowthLevelCap = 110
	// petMaxLevel is 4D6370's 0x8C.
	petMaxLevel = 140
	// petLevelUpSatiety is 4EFD10's 0x2710.
	petLevelUpSatiety uint16 = 10000
	// attackPetTid4 is TID 1.2.3.3, the class 4FCB00's vtable +0x438 asks.
	attackPetTid4 = 3

	// petSkillUseTag is the item description entry a pet potion casts.
	petSkillUseTag = "PSU1"
)

/*
================
isAttackPetRef
================
*/
func isAttackPetRef(ref *enterworld.CharacterRef) bool {
	return ref != nil && ref.TidWord&0x7fe == 0x1c6 && ref.TidWord>>11 == attackPetTid4
}

/*
================
attackPetKillExperience

410110's COS arm for a pet of the given level: base, grade and damage
share, the level gap and the relative bonus, never below one. INFERENCE:
the party level penalty (Formulae_CalculatePartyLevelDiffPenalty) has no
party to read for a pet, so it is the solo bonus.
================
*/
func attackPetKillExperience(level int64, target monster.Instance, damage uint32) int64 {
	if damage == 0 || target.EffectiveMaxHP() == 0 || !target.Ref.RewardActionPinned || target.Ref.ExpToGive == 0 {
		return 0
	}
	damageFactor := float32(min(float32(damage), float32(target.EffectiveMaxHP())) / float32(target.EffectiveMaxHP()))
	damageFactor = max(float32(1e-6), damageFactor)
	expBase := float32(float64(target.Ref.ExpToGive) * float64(monsterRewardGradeMultiplier(target.Rarity())))
	return nativePositiveExpReward(float64(expBase) * float64(damageFactor) *
		float64(monsterLevelGapRewardScale(level, int64(target.Ref.Level))) *
		float64(monsterRelativeLevelBonus(level, int64(target.Ref.Level))))
}

/*
================
petGrowthPercent

The pet's parameter 0xBA: the second expi word of every live pet potion
the owner used (its PSU1 skill, installed on the pet by 594AC0).
================
*/
func (rt *Runtime) petGrowthPercent(c *enterworld.Character, nowMs int64) float32 {
	skills, ok := rt.deps.SkillData().(interface {
		SkillByCodename(string) (enterworld.SkillRow, bool)
	})
	refs := rt.deps.ItemReferences()
	if !ok || refs == nil {
		return 0
	}
	percent := float32(0)
	for _, window := range c.PetSkillWindows {
		if window.EndUnixMs <= nowMs {
			continue
		}
		item, found := refs.ItemRefByCodename(window.Codename)
		if !found || item == nil {
			continue
		}
		if skill, cast := skills.SkillByCodename(petPotionSkill(item)); cast {
			percent += float32(skill.ExpIncrease[1])
		}
	}
	return percent
}

/*
================
petPotionSkill

The skill a pet potion casts: Desc2 "[PSU1:SKILL_...,level]".
================
*/
func petPotionSkill(item *enterworld.ItemRef) string {
	entry := strings.TrimSpace(item.ParamDescriptions[1])
	entry = strings.TrimSuffix(strings.TrimPrefix(entry, "["), "]")
	tag, value, found := strings.Cut(entry, ":")
	if !found || tag != petSkillUseTag {
		return ""
	}
	codename, _, _ := strings.Cut(value, ",")
	return strings.TrimSpace(codename)
}

/*
================
awardAttackPetExperience

4FCB00 for one award recipient: every summoned attack pet earns its share.
Called inside the reward's character transaction.
================
*/
func (rt *Runtime) awardAttackPetExperience(c *enterworld.Character, target monster.Instance, damage uint32, factor float32, nowMs int64) (owner, area []wire.Frame) {
	ownerPercent := paramJobPercent(c, paramExpRate, nowMs) + paramJobPercent(c, paramPremiumExpRate, nowMs)
	for _, pet := range c.Companions() {
		ref, ok := rt.cosReference(pet)
		if !pet.Summoned || pet.CurrentHP == 0 || !ok || !isAttackPetRef(ref) {
			continue
		}
		exp := attackPetKillExperience(int64(ref.Level), target, damage)
		if exp <= 0 {
			continue
		}
		if factor != 0 {
			exp = int64(float64(exp) * float64(factor))
		}
		bonus := int64(0)
		if ownerPercent > 0 {
			bonus = int64(float64(ownerPercent) / 100 * float64(exp))
		}
		if petPercent := rt.petGrowthPercent(c, nowMs) / 100; petPercent > 0 {
			bonus += int64(float64(exp) * float64(petPercent))
		}
		private, public := rt.applyAttackPetExperience(c, pet, ref, exp+max(bonus, 0), target.Gid)
		owner = append(owner, private...)
		area = append(area, public...)
	}
	return owner, area
}

/*
================
awardAttackPetPvPExperience

CCOSManager_AwardAttackPetLevelReward (4FCDA0), beside the killer's PvP
EXP (4EAC40): each summoned attack pet earns leveldata +0x1C of the lower
of its level and the victim's, doubled for a murderer victim (state 2).
================
*/
func (rt *Runtime) awardAttackPetPvPExperience(killer, victim *enterworld.Character) (owner, area []wire.Frame) {
	levels := rt.deps.LevelData()
	if levels == nil {
		return nil, nil
	}
	source := enterworld.ObjectIDForCharacter(victim)
	for _, pet := range killer.Companions() {
		ref, ok := rt.cosReference(pet)
		if !pet.Summoned || pet.CurrentHP == 0 || !ok || !isAttackPetRef(ref) {
			continue
		}
		exp, known := levels.MonsterExpBasis(min(int64(ref.Level), rewardLevel(victim)))
		if !known || exp <= 0 {
			continue
		}
		if murderer(victim) {
			exp *= 2
		}
		private, public := rt.applyAttackPetExperience(killer, pet, ref, exp, source)
		owner = append(owner, private...)
		area = append(area, public...)
	}
	return owner, area
}

/*
================
applyAttackPetExperience

4D6370 for a gain. Returns the owner's EXP frame and the area's form and
level-up frames.
================
*/
func (rt *Runtime) applyAttackPetExperience(c *enterworld.Character, pet *enterworld.CharacterCOS, ref *enterworld.CharacterRef, delta int64, source uint32) (owner, area []wire.Frame) {
	levels := rt.deps.LevelData()
	refs, ok := rt.deps.ItemReferences().(enterworld.CharacterRefSource)
	if delta <= 0 || levels == nil || !ok {
		return nil, nil
	}
	// Port-only, not native: the closed beta's growth pace. A pet's gain is
	// worth what a gain at its OWNER's level is (owner decision 2026-10-11),
	// so a pet catches up to its owner fast; it still never passes the owner
	// (the bank below). Nil, the default, is the native rate.
	if rt.PetExpPace != nil {
		delta = int64(min(float64(delta)*max(rt.PetExpPace(rewardLevel(c)), 1), float64(1<<62)))
	}
	required, known := levels.ExpRequired(int64(ref.Level))
	if !known || required <= 0 {
		return nil, nil
	}
	before := int64(min(pet.Experience, uint64(1<<62)))
	if int64(ref.Level) >= petGrowthLevelCap || int64(ref.Level) >= rewardLevel(c) {
		banked := min(before+delta, required-1)
		if banked <= before {
			return nil, nil
		}
		pet.Experience = uint64(banked)
		return []wire.Frame{petExperienceFrame(pet.GID, banked-before, source)}, nil
	}
	exp := before + delta
	form := ref
	for int64(form.Level) < petMaxLevel && exp >= required {
		next, found := refs.CharacterRefByCodename(form.NextCodename)
		if !found || next == nil || next.TidWord != form.TidWord {
			break
		}
		following, has := levels.ExpRequired(int64(next.Level))
		if !has || following <= 0 {
			break
		}
		exp -= required
		form, required = next, following
	}
	pet.Experience = uint64(exp)
	if form != ref {
		pet.RefObjID, pet.Codename, pet.Level = form.RefObjID, form.Codename, form.Level
		pet.CurrentHP, pet.CurrentMP, pet.Satiety = form.MaxHP, form.MaxMP, petLevelUpSatiety
		area = append(area,
			wire.Frame{Opcode: opCosInfoUpdate, Payload: wire.NewWriter(9).U32(pet.GID).U8(cosInfoReference).U32(form.RefObjID).Payload()},
			wire.Frame{Opcode: wire.OpLevelUpEffect, Payload: wire.EncodeLevelUpEffect(pet.GID)},
			wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshPayload(pet.GID,
				simulation.Vitals{CurrentHP: pet.CurrentHP, CurrentMP: pet.CurrentMP})})
	}
	return []wire.Frame{petExperienceFrame(pet.GID, delta, source)}, area
}

/*
================
petExperienceFrame
================
*/
func petExperienceFrame(gid uint32, delta int64, source uint32) wire.Frame {
	return wire.Frame{Opcode: opCosInfoUpdate,
		Payload: wire.NewWriter(13).U32(gid).U8(cosInfoExperience).U32(uint32(int32(min(delta, 1<<31-1)))).U32(source).Payload()}
}
