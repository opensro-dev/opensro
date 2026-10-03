/*
===========================================================================

skilltuning.go - Tuning Noise and Tuning Sound: fixed damage drained as MP

The Bard's pdmg hits (enterworld/skillfixeddamage.go) run the ordinary
single-target offensive release: prepared cost (BDMD, dcmp), approach,
hit record, aggression, rewards and the cast bracket are skillcombat.go's.
This file owns only what pdmg and dmgt change: the hit's value and the MP
the caster drains from it.

Owner's rule: the damage is fixed and ignores the target's defense; 100 %
(dmgt) of the damage dealt comes back as the Bard's MP.

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

// fullDamagePercent is a dmgt share of the whole damage.
const fullDamagePercent = 100

/*
================
fixedDamageResult

The pdmg record: a normal result carrying the authored amount.

Inferred: the record skips the CFormulae lanes entirely, as the port's
other fixed producers do (the status-cast record, the abnormal damage
over time): no defense, parry, level advantage or STR/INT balance, and
no critical or block roll, since 58ED8C and 58F0C1 roll only for att
lanes. This port has no evade roll to skip: hit and evasion rates only
move the damage percentile of an att lane (combat.hitCenter).
================
*/
func fixedDamageResult(fixed enterworld.SkillFixedDamage) combat.Result {
	return combat.Result{Damage: min(fixed.Amount, wire.MaxSkillActionDamage), ResultFlags: 1}
}

/*
================
drainedShare

percent of the HP the impacts actually took, the damage "dealt": a hit
on a monster with less HP left than the amount drains only that HP.
Inferred: the share is truncated, like the integer damage words.
================
*/
func drainedShare(impacts []simulation.MonsterDamageResult, percent uint32) int64 {
	var dealt uint64
	for _, impact := range impacts {
		dealt += uint64(impact.Applied)
	}
	return int64(dealt * uint64(percent) / fullDamagePercent)
}

/*
================
commitTuningMana

Inside the caster's door, after the cast's cost: the dmgt share of the
committed damage goes into the caster's MP, capped at maximum MP, and
the caster's 0x33A6 carries it. Inferred: the gain is a skill recovery
(applySkillRecovery), so a recovery reduction on the caster cuts it as it
cuts the Bard's other MP gains.
================
*/
func (rt *Runtime) commitTuningMana(division string, caster *enterworld.Character, fixed enterworld.SkillFixedDamage, impacts []simulation.MonsterDamageResult) wire.Frame {
	mp := drainedShare(impacts, fixed.MPPercent)
	if mp == 0 {
		return wire.Frame{}
	}
	frame, _ := rt.applySkillRecovery(division, caster, 0, mp)
	return frame
}
