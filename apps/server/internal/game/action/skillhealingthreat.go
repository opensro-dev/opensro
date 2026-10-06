/*
===========================================================================

skillhealingthreat.go - category-H recovery aggression on the recipient's squad

Recovery callers publish only after the character transaction succeeds.
Threat uses the authored recovery amount, before reduction and gauge clamping,
and never grants damage credit or changes the monster's HP.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	skillCategoryHealing   = 0x48
	healingMonsterMask     = 0x7fe
	healingMonsterType     = 0xc6
	healingStructureMask   = 0xfffe
	healingStructureLive   = 0xa46
	healingStructureThird  = 0x1a46
	healingStructureFourth = 0x2246
)

/*
================
skillHealingThreat
================
*/
type skillHealingThreat struct {
	division          string
	caster, recipient uint32
	category          uint8
	amount            int64
}

/*
================
makeSkillHealingThreat

5A09D3 / 5A0B69 add the two dwords before 5A0650 halves the signed sum.
================
*/
func makeSkillHealingThreat(division string, caster, recipient *enterworld.Character, skill enterworld.SkillRow) skillHealingThreat {
	return skillHealingThreat{division: division, caster: enterworld.ObjectIDForCharacter(caster),
		recipient: enterworld.ObjectIDForCharacter(recipient), category: skill.Category}
}

/*
================
publishSkillHealingThreat
================
*/
func (rt *Runtime) publishSkillHealingThreat(heal skillHealingThreat, now int64) {
	if rt.Monsters == nil || heal.category != skillCategoryHealing || uint32(heal.amount) == 0 {
		return
	}
	aggression := int32(uint32(heal.amount)) / 2
	for _, gid := range rt.Monsters.TargetSquad(heal.division, heal.recipient) {
		actor, ok := rt.Monsters.Get(heal.division, gid)
		if !ok || !healingThreatActor(actor.Ref.TidWord, actor.CurrentHP != 0) {
			continue
		}
		rt.dispatchAggression(heal.division, gid, simulation.HostilityEvent{Attacker: heal.caster, Aggression: aggression}, now)
	}
}

/*
================
healingThreatActor

589CC0 calls the actor-class predicates at vtable 3C8/3CC/3D4/28. Only
the first siege subtype additionally reads life state (F8). Squad lifetime
and hostility admission independently retire dead ordinary monsters.
================
*/
func healingThreatActor(tid uint16, alive bool) bool {
	if tid&healingMonsterMask == healingMonsterType {
		return true
	}
	switch tid & healingStructureMask {
	case healingStructureLive:
		return alive
	case healingStructureThird, healingStructureFourth:
		return true
	}
	return false
}
