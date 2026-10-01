package action

import (
	"math"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/grounditem"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/monster"
)

// Keep a zero-HP source registry-live briefly so the client can present a
// corpse before the ordinary despawn broadcast. This is server registry
// lifecycle pacing, not an animation clock: the retail server's absorbing
// CAIState_DEAD path has no BAN-duration lookup, and shipped death clips are
// not uniformly shorter than this interval. Client CAniMixer completion,
// death-loop handoff, reward effects, and the later 1.5 s dissolve remain
// separate owners. The monster mover already excludes zero-HP instances.
const monsterDeathPresentationRetention = 2 * time.Second

type pendingMonsterDefeat struct {
	divisionID string
	gid        uint32
	dueAtMs    int64
}

// monsterKillReward derives the two independent retail progression lanes.
// This is the full-damage solo arm of the v1.188 GameServer chain:
//
//	CGObjMob_DistributeSoloKillReward
//	  -> CGObjPC_ApplyMonsterKillRewards
//	     -> Formulae_ComputeMonsterExpReward
//	     -> Formulae_ComputeMonsterSkillExpReward
//
// RefObjChar.ExpToGive remains the monster authority, but reward rarity is a
// dedicated native table (it is not the MaxHP multiplier). Both lanes share
// the mastery-gap, monster/player-level and rarity factors. SEXP additionally
// multiplies by 100 and divides by CRefLevel::GUST_Mob_Exp; there is no 1:6
// shortcut in the retail pipeline. Independent factors spill to float32.
// EXP converts the product directly to int64 (410352); SEXP first spills
// the product to float32 (41083E) and then converts to a signed dword.
func monsterKillReward(
	character *enterworld.Character,
	target monster.Instance,
	levels enterworld.LevelDataSource,
) (int64, int64) {
	return monsterContributionReward(character, target, levels, target.EffectiveMaxHP(), 1, false)
}

func monsterContributionReward(character *enterworld.Character, target monster.Instance, levels enterworld.LevelDataSource, damage uint32, recipientFactor float32, expShare bool) (int64, int64) {
	if damage == 0 || target.EffectiveMaxHP() == 0 {
		return 0, 0
	}
	if character == nil || !target.Ref.RewardActionPinned || target.Ref.ExpToGive == 0 {
		return 0, 0
	}
	if levels == nil {
		return 0, 0
	}
	playerLevel := int64(1)
	if character.Level != nil && *character.Level > 0 {
		playerLevel = *character.Level
	}
	mobExpBasis, basisOK := levels.MonsterExpBasis(playerLevel)
	if !basisOK || mobExpBasis <= 0 {
		return 0, 0
	}

	maxMastery := int64(0)
	for _, mastery := range character.Masteries {
		if mastery.Level > maxMastery {
			maxMastery = mastery.Level
		}
	}
	expRate, skillExpRate := masteryGapProgressionRates(playerLevel, maxMastery)
	levelScale := monsterLevelGapRewardScale(playerLevel, int64(target.Ref.Level))
	relativeLevelBonus := monsterRelativeLevelBonus(playerLevel, int64(target.Ref.Level))
	if expShare {
		relativeLevelBonus = monsterPartyRelativeLevelBonus(playerLevel, int64(target.Ref.Level), enterworld.NativeCountryByte9C(character) == 1)
	}
	// 41015F/410226: unsigned damage is converted to float32, clamped to
	// max HP, divided and spilled before the factor chain. It is not a
	// fraction of total damage from the competing groups.
	damageFactor := float32(min(float32(damage), float32(target.EffectiveMaxHP())) / float32(target.EffectiveMaxHP()))
	// 41024A and 410786 load B45C84 (float32 1e-6), then clamp to 1.
	damageFactor = max(float32(1e-6), damageFactor)
	gradeMultiplier := monsterRewardGradeMultiplier(target.Rarity())
	// 0x410214 stores base*grade to a dword before the EXP factor chain.
	expBase := float32(float64(target.Ref.ExpToGive) * float64(gradeMultiplier))
	exp := nativePositiveExpReward(
		float64(expBase) *
			float64(damageFactor) *
			float64(levelScale) *
			float64(expRate) *
			float64(relativeLevelBonus),
	)
	// The SEXP arm keeps base*grade*100/basis in x87 until it multiplies the
	// same independently spilled factor set, then stores the finished value
	// to one dword at 0x41083e.
	skillExp := nativePositiveReward(float32(
		float64(target.Ref.ExpToGive) *
			float64(gradeMultiplier) *
			100 / float64(mobExpBasis) *
			float64(damageFactor) *
			float64(levelScale) *
			float64(skillExpRate) *
			float64(relativeLevelBonus),
	))
	// 4EA7D9..4EA88B: each formula result is converted back to float32
	// before the distributor's recipient factor and final truncation.
	return int64(float64(float32(exp)) * float64(recipientFactor)), int64(nativeRewardDword(float64(float32(skillExp)) * float64(recipientFactor)))
}

// 410352 calls the 64-bit conversion helper, then replaces nonpositive
// results with one. There is no signed-dword saturation in this EXP lane.
func nativePositiveExpReward(value float64) int64 {
	if math.IsNaN(value) || value < 1 || value >= 0x1p63 {
		return 1
	}
	return int64(value)
}

func monsterPartyRelativeLevelBonus(playerLevel, monsterLevel int64, european bool) float32 {
	if monsterLevel+3 <= playerLevel {
		return 1
	}
	gap := monsterLevel - playerLevel
	extra := float32(0)
	switch {
	case gap >= 9:
		extra = .45
	case gap >= 7:
		extra = .30
	case gap >= 5:
		extra = .15
	}
	if european {
		extra *= 2
	}
	base := float32(float64(extra) + 1)
	return min(float32(4), max(float32(1), float32(float64(base)+float64(min(int64(13), gap+3))*float64(float32(.03)))))
}

func nativePositiveReward(value float32) int64 {
	// 410842..410869 clamps the floating value before conversion. An
	// unordered comparison does not enter the clamp arm.
	if value < 1 {
		value = 1
	}
	return int64(nativeRewardDword(float64(value)))
}

// 9FBB40's SSE2 path (the native execution profile on the x64 server host)
// stores to float64, then CVTTSD2SI returns a signed dword. Invalid inputs
// produce 80000000; neither saturation nor Go's platform-dependent cast is
// the instruction's contract. 4EA88B uses the same helper after sharing.
func nativeRewardDword(value float64) int32 {
	if math.IsNaN(value) || value >= 0x1p31 || value <= -0x1p31-1 {
		return math.MinInt32
	}
	return int32(value)
}

// monsterRewardGradeMultiplier ports CGObjMob_GetRewardGradeMultiplier.
// The high party-monster nibble contributes x7.5. The low rarity nibble is
// independent from HP scaling: champion x2, giant x15, titan x60, elite x4,
// and type 7 x30. Static types 0/3/8 retain the current party factor.
func monsterRewardGradeMultiplier(rarity uint8) float32 {
	multiplier := float32(1)
	if rarity>>4 == 1 {
		multiplier = 7.5
	}
	switch rarity & 0x0f {
	case 0, 3, 8:
		return multiplier
	case 1:
		return multiplier * 2
	case 4:
		return multiplier * 15
	case 5:
		return multiplier * 60
	case 6:
		return multiplier * 4
	case 7:
		return multiplier * 30
	default:
		return 1
	}
}

// masteryGapProgressionRates ports CGObjPC_RecomputeMasteryGapProgressionRates.
// A positive level-minus-mastery gap trades 10% EXP for 10% SEXP per level;
// native clamps both signed directions to 0.1..1.9 instead of assuming the
// persisted mastery can never be ahead of the character level.
func masteryGapProgressionRates(level, maxMastery int64) (float32, float32) {
	gap := float32(level - maxMastery)
	expRate := float32(1) - gap*0.1
	skillExpRate := float32(1) + gap*0.1
	return clampRewardRate(expRate), clampRewardRate(skillExpRate)
}

func clampRewardRate(value float32) float32 {
	if value < 0.1 {
		return 0.1
	}
	if value > 1.9 {
		return 1.9
	}
	return value
}

// monsterLevelGapRewardScale ports Formulae_ComputeMonsterLevelGapRewardScale.
// Monsters up through ten levels above the player retain full credit, then
// lose 2% per additional level. Monsters four-to-six levels below give 90%;
// the seven-plus arm applies 15% per level beyond the first three. Native
// clamps the resulting scale to 0.1..1.
func monsterLevelGapRewardScale(playerLevel, monsterLevel int64) float32 {
	scale := float32(1)
	if monsterLevel > playerLevel+10 {
		scale -= float32(monsterLevel-playerLevel-10) * 0.02
	} else if playerLevel > monsterLevel+3 {
		gap := playerLevel - monsterLevel
		if gap < 7 {
			scale = 0.9
		} else {
			scale -= float32(gap-3) * 0.15
		}
	}
	if scale < 0.1 {
		return 0.1
	}
	if scale > 1 {
		return 1
	}
	return scale
}

// monsterRelativeLevelBonus is the non-party arm of
// Formulae_ComputeMonsterRelativeLevelBonus. Current action has no party
// contribution owner, so applying the native party-only 0.15/0.30/0.45
// additions here would fabricate a group. The common relative-level term is
// still exact and shared by Chinese and European characters.
func monsterRelativeLevelBonus(playerLevel, monsterLevel int64) float32 {
	if monsterLevel+3 <= playerLevel {
		return 1
	}
	steps := monsterLevel - playerLevel + 3
	if steps > 13 {
		steps = 13
	}
	// 4100BE..4100CC: the step count times the double 0.03 (float32 .03
	// widened) adds to 1 on the x87 stack; only the sum is stored to float32.
	bonus := float32(1 + float64(steps)*float64(float32(.03)))
	if bonus < 1 {
		return 1
	}
	if bonus > 4 {
		return 4
	}
	return bonus
}

/*
==================
applyMonsterKillInsideDoor

Applies one recipient's kill products - progression, quest kill credit and
the planned ground drops - without opening a door of its own. The caller,
settleMonsterInsideDoor, already holds the fatal hit's single UpdateMany
door, so bow/crossbow debit, progression and the ground rows land in the
same character+ground transaction and no later tick owns the reward.
==================
*/
func (rt *Runtime) applyMonsterKillInsideDoor(
	divisionID string,
	character *enterworld.Character,
	exp, skillExp int64,
	defeatGid uint32,
	plannedDrops []grounditem.Item,
) (frames []wire.Frame, added []grounditem.Item, changed bool) {
	if character == nil || character.DeletePending {
		return nil, nil, false
	}
	if rt.UpdateExperience != nil && (exp != 0 || skillExp != 0) {
		var progressionChanged bool
		frames, progressionChanged = rt.UpdateExperience(character, exp, skillExp, defeatGid)
		changed = progressionChanged
	}
	if rt.UpdateQuestKill != nil && rt.Monsters != nil {
		if target, ok := rt.Monsters.Get(divisionID, defeatGid); ok && target.CurrentHP == 0 {
			questFrames, questChanged := rt.UpdateQuestKill(character, target.Ref.Codename, target.Rarity())
			frames = append(frames, questFrames...)
			changed = changed || questChanged
		}
	}
	for _, plannedDrop := range plannedDrops {
		spawned := rt.addMonsterGround(divisionID, defeatGid, plannedDrop)
		if spawned.Gid == 0 {
			continue
		}
		added = append(added, spawned)
		changed = true
	}
	return frames, added, changed
}

func (rt *Runtime) queueMonsterDefeat(divisionID string, gid uint32, dueAtMs int64) {
	if divisionID == "" || gid == 0 {
		return
	}
	rt.pendingMonsterDefeatsMu.Lock()
	rt.pendingMonsterDefeats = append(rt.pendingMonsterDefeats, pendingMonsterDefeat{
		divisionID: divisionID,
		gid:        gid,
		dueAtMs:    dueAtMs,
	})
	rt.pendingMonsterDefeatsMu.Unlock()
}

func (rt *Runtime) drainMonsterDefeats(nowMs int64) {
	rt.pendingMonsterDefeatsMu.Lock()
	kept := rt.pendingMonsterDefeats[:0]
	var due []pendingMonsterDefeat
	for _, pending := range rt.pendingMonsterDefeats {
		if pending.dueAtMs > nowMs {
			kept = append(kept, pending)
			continue
		}
		due = append(due, pending)
	}
	rt.pendingMonsterDefeats = kept
	rt.pendingMonsterDefeatsMu.Unlock()

	if rt.Monsters == nil {
		return
	}
	for _, pending := range due {
		rt.Monsters.Defeat(
			pending.divisionID,
			pending.gid,
			time.UnixMilli(nowMs),
		)
	}
}
