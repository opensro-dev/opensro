/*
===========================================================================

petgrowth.go - attack pets grow on their owner's kills

Every monster award an owner earns also feeds each summoned attack pet
(the reference server's kill EXP grant hands it to its attack-pet
award): the player formula run for the pet's level without the mastery
gap, shared by the same recipient
factor, plus the owner's parameter-job percent and the pet's own 0xBA
percent (the growth potion's expi).

The reference server's pet EXP delta banks it. A pet at level 110
or at its owner's level stops one point short of its next level; any
other pet walks the leveldata curve, and each level is a new reference
(column 6): the reference server replaces the reference and resets the vitals, which
restores HP, MP and satiety and tells the area. The v1.150 client keeps
the curve too, so the owner only learns the delta:

	0x3508 [u32 cos][u8 3][i32 exp][u32 source]   owner   (v1.188 0x30C9 3)
	0x3508 [u32 cos][u8 7][u32 refObjID]          area    (v1.188 0x30C9 7)
	0x36B0 [u32 cos]                              area    (v1.188 0x3054)
	vitals refresh of the new HP and MP           area    (vfunc +0x274)

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

	// petGrowthLevelCap is the reference server's 0x6E: a pet this high only banks EXP.
	petGrowthLevelCap = 110
	// petMaxLevel is the reference server's 0x8C.
	petMaxLevel = 140
	// petLevelUpSatiety is the reference server's 0x2710.
	petLevelUpSatiety uint16 = 10000
	// attackPetTid4 is TID 1.2.3.3, the class the pet award's vtable +0x438 asks.
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
summonedAttackPet

The owner's live attack (growth) pet: summoned, alive and still resolving
to its reference. Nil when none is out.
================
*/
func (rt *Runtime) summonedAttackPet(c *enterworld.Character) *enterworld.CharacterCOS {
	for _, pet := range c.Companions() {
		ref, ok := rt.cosReference(pet)
		if pet.Summoned && pet.CurrentHP != 0 && ok && isAttackPetRef(ref) {
			return pet
		}
	}
	return nil
}

/*
================
attackPetKillExperience

The experience formula's COS arm for a pet of the given level: base, grade and damage
share, the level gap and the relative bonus, never below one. INFERENCE:
the party level penalty has no
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
the owner used (its PSU1 skill, installed on the pet with the buff modifiers).
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

The reference server's pet award for one award recipient: every summoned attack pet earns its share.
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

The reference server's attack-pet level reward, beside the killer's PvP
EXP: each summoned attack pet earns leveldata +0x1C of the lower
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

The reference server's pet EXP delta for a gain. Returns the owner's EXP frame and the area's form and
level-up frames.
================
*/
func (rt *Runtime) applyAttackPetExperience(c *enterworld.Character, pet *enterworld.CharacterCOS, ref *enterworld.CharacterRef, delta int64, source uint32) (owner, area []wire.Frame) {
	if pet.BroodStage != 0 {
		return nil, nil
	}
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
	form, consumed := ref, int64(0)
	// The reference server's level-up loop has no owner check: native gains never cross more than a
	// level. Port-only, not native: the beta pace above can, so with the pace
	// on the loop stops at the owner's level and banks there as the native does
	// on entry. With it off the loop is native.
	ceiling := int64(petMaxLevel)
	if rt.PetExpPace != nil {
		ceiling = min(ceiling, rewardLevel(c))
	}
	for int64(form.Level) < petMaxLevel && int64(form.Level) < ceiling && exp >= required {
		next, found := refs.CharacterRefByCodename(form.NextCodename)
		if !found || next == nil || next.TidWord != form.TidWord {
			break
		}
		following, has := levels.ExpRequired(int64(next.Level))
		if !has || following <= 0 {
			break
		}
		exp -= required
		consumed += required
		form, required = next, following
	}
	if rt.PetExpPace != nil && int64(form.Level) >= rewardLevel(c) && exp >= required {
		exp = required - 1
	}
	pet.Experience = uint64(exp)
	if form != ref {
		pet.RefObjID, pet.Codename, pet.Level = form.RefObjID, form.Codename, form.Level
		if effective, valid := rt.cosReference(pet); valid {
			form = effective
		}
		pet.CurrentHP, pet.CurrentMP, pet.Satiety = form.MaxHP, form.MaxMP, petLevelUpSatiety
		area = append(area,
			wire.Frame{Opcode: opCosInfoUpdate, Payload: wire.NewWriter(9).U32(pet.GID).U8(cosInfoReference).U32(form.RefObjID).Payload()},
			wire.Frame{Opcode: wire.OpLevelUpEffect, Payload: wire.EncodeLevelUpEffect(pet.GID)},
			wire.Frame{Opcode: simulation.OpVitalsUpdate, Payload: simulation.VitalsRefreshPayload(pet.GID,
				simulation.Vitals{CurrentHP: pet.CurrentHP, CurrentMP: pet.CurrentMP})})
	}
	// The owner sees the EXP the pet was credited: all of it unless the
	// owner's level banked the rest (as the entry bank reports banked-before).
	owner = []wire.Frame{petExperienceFrame(pet.GID, consumed+exp-before, source)}
	if form != ref {
		// A new form's effective max is not the catalog max the client
		// divides by: publish it with the gain (#1896).
		owner = append(owner, rt.petEffectiveMaxFrames(pet, rt.Now().UnixMilli())...)
	}
	return owner, area
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
