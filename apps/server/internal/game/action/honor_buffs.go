/*
===========================================================================

honor_buffs.go - Training Camp Honor Rank buffs (20160..20163)

The reference server's training camp honor rebuild assigns each ranked
camp an evaluation rank 1..4 (1 King,
2 Gold, 3 Silver, 4 Bronze). A camp master receives the cumulative Honor
buff skills up to their camp's evaluation rank from skilldata_25000.txt:

  - 20160 SKILL_TC_COPPER_BUFF_01 (rank 4..1): hr +10% attack rate
  - 20161 SKILL_TC_SILVER_BUFF_01 (rank 3..1): hste +20% movement speed
  - 20162 SKILL_TC_GOLD_BUFF_01   (rank 2..1): dru +5% phy/mag damage,
    odar +5% phy/mag absorption
  - 20163 SKILL_TC_CROWN_BUFF_01  (rank 1):    hwir +20% berserk gauge gain
    (the berserk gauge gain parameter 0xb8, which the native buff modifier
    pass writes and the berserk point formula reads)

===========================================================================
*/

package action

import (
	"sync/atomic"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/statuseffect"
	"opensro.online/server/internal/game/item/wire"
)

const (
	HonorBuffCopperSkillID uint32 = 20160
	HonorBuffSilverSkillID uint32 = 20161
	HonorBuffGoldSkillID   uint32 = 20162
	HonorBuffCrownSkillID  uint32 = 20163
)

/*
================
HonorBuffSkillIDsForRank

Returns the cumulative Honor buff skill IDs for a Training Camp
evaluationRank (1 King, 2 Gold, 3 Silver, 4 Bronze; 0 or >=5 unranked).
================
*/
func HonorBuffSkillIDsForRank(evaluationRank uint8) []uint32 {
	switch evaluationRank {
	case 1:
		return []uint32{
			HonorBuffCopperSkillID,
			HonorBuffSilverSkillID,
			HonorBuffGoldSkillID,
			HonorBuffCrownSkillID,
		}
	case 2:
		return []uint32{
			HonorBuffCopperSkillID,
			HonorBuffSilverSkillID,
			HonorBuffGoldSkillID,
		}
	case 3:
		return []uint32{
			HonorBuffCopperSkillID,
			HonorBuffSilverSkillID,
		}
	case 4:
		return []uint32{
			HonorBuffCopperSkillID,
		}
	default:
		return nil
	}
}

/*
================
isHonorBuffSkillID
================
*/
func isHonorBuffSkillID(skillID uint32) bool {
	return skillID >= HonorBuffCopperSkillID && skillID <= HonorBuffCrownSkillID
}

/*
================
findCharacterByID

Resolves a division character record by persisted character ID.
================
*/
func (rt *Runtime) findCharacterByID(divisionID string, charID int64) *enterworld.Character {
	if rt == nil || rt.deps == nil || charID <= 0 {
		return nil
	}
	if source, ok := rt.deps.(domain.CharacterLookup); ok {
		return source.CharacterByID(divisionID, charID)
	}
	for _, character := range rt.deps.CharactersForDivision(divisionID) {
		if character != nil && character.ID == charID {
			return character
		}
	}
	return nil
}

/*
================
hasCrownHonorBuff

Reports whether actor currently carries SKILL_TC_CROWN_BUFF_01 (20163),
which grants +20% berserk gauge gain rate ('hwir', parameter 0xb8).
================
*/
func (rt *Runtime) hasCrownHonorBuff(division string, actor *enterworld.Character) bool {
	if rt == nil || rt.effects == nil || actor == nil {
		return false
	}
	checkDivision := func(div string) bool {
		if div == "" {
			return false
		}
		for _, e := range rt.effects.Snapshot(div, actor.Name) {
			if e.SkillID == HonorBuffCrownSkillID && !e.StopRequested {
				return true
			}
		}
		return false
	}
	if checkDivision(division) {
		return true
	}
	return checkDivision("global-official")
}

/*
================
SyncTrainingCampHonorBuffs

Reconciles a character's active Honor Rank buffs (20160..20163) against
their camp's EvaluationRank (or 0 when unranked / not a camp master) and
publishes the resulting attached/ended effect and stat frames.
================
*/
func (rt *Runtime) SyncTrainingCampHonorBuffs(division string, charID int64, evaluationRank uint8) {
	if rt == nil || rt.deps == nil || rt.effects == nil || charID <= 0 {
		return
	}
	unlock := rt.lockDivision(division)
	defer unlock()

	c := rt.findCharacterByID(division, charID)
	if c == nil {
		return
	}
	want := HonorBuffSkillIDsForRank(evaluationRank)
	wantSet := make(map[uint32]bool, len(want))
	for _, id := range want {
		wantSet[id] = true
	}

	now := rt.Now().UnixMilli()
	var public, private []wire.Frame
	rt.deps.Update(c, "training-camp-honor-buffs", func() bool {
		if c.DeletePending {
			return false
		}
		haveSet := make(map[uint32]bool, 4)
		var retireTokens []uint32
		for _, e := range rt.effects.Snapshot(division, c.Name) {
			if !isHonorBuffSkillID(e.SkillID) {
				continue
			}
			if !wantSet[e.SkillID] || haveSet[e.SkillID] {
				retireTokens = append(retireTokens, e.InstanceToken)
				continue
			}
			haveSet[e.SkillID] = true
		}

		var retired []statuseffect.Effect
		if len(retireTokens) > 0 {
			retired = rt.effects.RetireInstances(division, c.Name, retireTokens)
			retirePub, _ := rt.finishEndedEffects(division, c, retired, now)
			public = append(public, retirePub...)
		}

		installedAny := false
		if rt.deps.SkillData() != nil {
			for _, skillID := range want {
				if haveSet[skillID] {
					continue
				}
				row, ok := rt.deps.SkillData().SkillByID(skillID)
				if !ok {
					continue
				}
				row.ReplacementPinned = true
				row.Replacement.Cbuf = true
				row.VoluntaryCancelBlocked = true
				if row.MovementModifier.Present {
					row.MovementModifier.Supported = true
					row.MovementModifier.Persistent = true
				}
				token := atomic.AddUint32(&rt.castTokenCounter, 1)
				if token == 0 {
					token = atomic.AddUint32(&rt.castTokenCounter, 1)
				}
				frames, ok := rt.commitCharacterEffectWithCheckpoint(
					division,
					c,
					row,
					token,
					statuseffect.StateActive,
					false,
					EffectPresentation{Phase: 2},
					now,
					false,
				)
				if ok {
					public = append(public, frames...)
					installedAny = true
				}
			}
		}

		if len(retired) > 0 || installedAny {
			hp, mp := rt.clampStoredGaugeToKeeper(division, c)
			private = append(private, rt.gaugeDropFrames(division, c, hp, mp, true)...)
		}
		return len(public) > 0 || len(private) > 0
	})

	if len(public) > 0 || len(private) > 0 {
		if rt.PushCharacterFrames != nil {
			rt.PushCharacterFrames(division, c.Name, append(append([]wire.Frame{}, public...), private...))
		}
		if len(public) > 0 && rt.PushDivisionPeerFrames != nil {
			rt.PushDivisionPeerFrames(division, c.Name, public)
		}
	}
}
