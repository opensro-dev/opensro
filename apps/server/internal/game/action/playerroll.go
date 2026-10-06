/*
===========================================================================

playerroll.go - the status rolls a hit makes on a player

SkillCombat_RollAbnormalStatus (590680) against a player victim: the
victim's keeper and learned passives supply its side (playerVictimRollInput),
the attacker its level, source and, for a player, its learned setv
dictionary. A monster, an attack pet and a player each roll through here.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
)

/*
==================
rollMonsterOnPlayer

rollMonsterOnPlayer ports 590680 for a monster's hit on a player.
==================
*/
func (rt *Runtime) rollMonsterOnPlayer(division string, instance monster.Instance, params *abnormal.SkillParams, target *enterworld.Character, defender combat.Stats, wall *enterworld.SkillWall) ([]abnormal.Record, error) {
	return rt.rollCreatureOnPlayer(division, instance.Gid, instance.Ref.Level, params, target, defender, wall)
}

/*
==================
rollCreatureOnPlayer

590680 for a non-player attacker (a monster, an attack pet) on a player:
the attacker contributes its level and object ID, the victim its side.
==================
*/
func (rt *Runtime) rollCreatureOnPlayer(division string, source uint32, level uint8, params *abnormal.SkillParams, target *enterworld.Character, defender combat.Stats, wall *enterworld.SkillWall) ([]abnormal.Record, error) {
	if params == nil || !params.Present() {
		return nil, nil
	}
	in := playerVictimRollInput(params, target, defender, wall)
	rt.fileEffectResistance(division, target, &in.Resistance)
	in.CasterLevel = level
	in.SourceGID = source
	random := &abnormalRandom{rt: rt, actor: criticalActor{division: division, monster: source}}
	records := abnormal.Roll(in, random)
	return records, random.err
}

/*
==================
rollPlayerOnPlayer

590680 for a player's hit on a player: the victim's side as for a
monster's hit, the caster's level and learned setv dictionary
(CSkillManager_GetSkillModifier) as for a player's hit on a monster.
==================
*/
func (rt *Runtime) rollPlayerOnPlayer(division string, caster *enterworld.Character, casterStats combat.Stats, params *abnormal.SkillParams, target *enterworld.Character, defender combat.Stats, wall *enterworld.SkillWall) ([]abnormal.Record, error) {
	if params == nil || !params.Present() {
		return nil, nil
	}
	values := casterStats.SkillParameters
	in := playerVictimRollInput(params, target, defender, wall)
	rt.fileEffectResistance(division, target, &in.Resistance)
	in.CasterLevel = casterStats.Level
	in.CasterModifier = func(key uint32) (uint32, bool) {
		slot, known := enterworld.SkillParameterFromKey(key)
		if !known {
			return 0, false
		}
		return values[slot], values[slot] != 0
	}
	in.SourceGID = enterworld.ObjectIDForCharacter(caster)
	in.SourceName = caster.Name
	random := &abnormalRandom{rt: rt, actor: criticalActor{division: division, character: caster.Name}}
	records := abnormal.Roll(in, random)
	return records, random.err
}

/*
==================
playerVictimRollInput

The victim's side of 590680 for a player target: its keeper supplies
element resists (1B..20), flat reductions (91..96, which reat passives
raise) and the disease bonus (A9); its learned real passives supply the
status-resistance buckets (59DE50); a standing wall masks its records.
==================
*/
func playerVictimRollInput(params *abnormal.SkillParams, target *enterworld.Character, defender combat.Stats, wall *enterworld.SkillWall) abnormal.RollInput {
	param := func(id uint16) float32 {
		v, _ := defender.Param(id)
		return v
	}
	in := abnormal.RollInput{
		Params:      params,
		TargetLevel: defender.Level,
		TargetBonus: param(abnormalDiseaseBonusParam),
		TargetGID:   enterworld.ObjectIDForCharacter(target),
		Resistance:  defender.StatusResistance,
	}
	if wall != nil {
		in.WallMask = &wall.Mask
	}
	for i := range in.TargetResist {
		// 5909FB reads shock from 1D; 590B39 reads burn from 1E. Keeper
		// parameters follow roll order, not the block's Burn/ES slot order.
		in.TargetResist[i] = param(abnormalElementResistBase + uint16(i))
		in.TargetFlat[i] = param(abnormalFlatResistanceBase + uint16(i))
	}
	return in
}

/*
==================
fileEffectResistance

59DF20 files a real under the execution context that installs it, so a
resistance buff (Holy Word, Poison Circle) resists like a learned passive
while its instance lives. The victim's installed effects join the learned
buckets with the same highest-grade/highest-flat rule (combat.FileStatusResistance).
==================
*/
func (rt *Runtime) fileEffectResistance(division string, target *enterworld.Character, buckets *[17]abnormal.Resistance) {
	skills := rt.deps.SkillData()
	if target == nil || skills == nil || rt.effects == nil {
		return
	}
	var filed [17]bool
	for i := range buckets {
		filed[i] = buckets[i] != (abnormal.Resistance{})
	}
	for _, effect := range rt.effects.Snapshot(division, target.Name) {
		if row, ok := skills.SkillByID(effect.SkillID); ok && row.TimedEffect.Pinned {
			combat.FileStatusResistance(buckets, &filed, row.TimedEffect.Real)
		}
	}
}
