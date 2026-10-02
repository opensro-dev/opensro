/*
===========================================================================

skillrelations.go - player relations used by targeted skill admission

Reference target bytes select object kinds; they do not grant permission to
attack a player. The ordinary-world permission and relation predicates share
equipment, party and criminal-state authorities with the other action owners.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	playerCombatMinimumLevel = 20
	playerCombatMaxPenalty   = 200000
	playerCombatMaxDailyPK   = 15
	freeBattleAllOpponents   = 5
	freeBattleGroupField     = "itemParam2_2a0"
)

/*
================
playerRelationEquipment

Slot eight holds either a job suit or a free-battle cape. Resolve the item
reference rather than trusting the inventory row's serialized type flags.
================
*/
func (rt *Runtime) playerRelationEquipment(c *enterworld.Character) (job, cape uint8) {
	ref, _, ok := (combat.ReqiEquipment{C: c, Items: rt.deps.ItemReferences()}).Item(8)
	if !ok || ref.TypeIDs[0] != 3 || ref.TypeIDs[1] != 1 || ref.TypeIDs[2] != 7 {
		return 0, 0
	}
	if ref.TypeIDs[3] >= 1 && ref.TypeIDs[3] <= 3 {
		return uint8(ref.TypeIDs[3]), 0
	}
	if ref.TypeIDs[3] == 5 {
		group := ref.NativeFields.Get(freeBattleGroupField)
		if group >= 1 && group <= freeBattleAllOpponents {
			return 0, uint8(group)
		}
	}
	return 0, 0
}

/*
================
hostilePlayerEquipment

4EB320: different capes fight, while cape five also fights itself.
4EB620: thieves oppose traders and hunters; traders and hunters are allies.
================
*/
func (rt *Runtime) hostilePlayerEquipment(caster, target *enterworld.Character) bool {
	aJob, aCape := rt.playerRelationEquipment(caster)
	bJob, bCape := rt.playerRelationEquipment(target)
	if aCape != 0 && bCape != 0 {
		return aCape != bCape || aCape == freeBattleAllOpponents
	}
	return aJob != 0 && bJob != 0 && aJob != bJob && (aJob == 2 || bJob == 2)
}

/*
================
hostilePlayerRelation

The area enemy selector does not acquire neutral bystanders merely because
a deliberate primary attack could enter the PK branch. 4E23E0 checks criminal
state only after both players meet the normal-world level floor.
================
*/
func (rt *Runtime) hostilePlayerRelation(division string, caster, target *enterworld.Character) bool {
	if caster == nil || target == nil || caster.ID == target.ID || rt.sharePartyObject(division, caster, target) {
		return false
	}
	aJob, aCape := rt.playerRelationEquipment(caster)
	bJob, bCape := rt.playerRelationEquipment(target)
	if aCape != 0 && bCape != 0 || aJob != 0 && bJob != 0 {
		return rt.hostilePlayerEquipment(caster, target)
	}
	if caster.Level == nil || target.Level == nil || *caster.Level < playerCombatMinimumLevel || *target.Level < playerCombatMinimumLevel {
		return false
	}
	return caster.PVPState() != 0 || target.PVPState() != 0
}

/*
================
playerSkillRelationAllowed

58D02F..58D14A compares the common relation and each actor's highest relation.
Friendly skills cannot cross incompatible job/cape contexts; hostile skills
cannot attack allies inside the same non-PK context.
================
*/
func (rt *Runtime) playerSkillRelationAllowed(division string, caster, target *enterworld.Character, hostile bool) bool {
	if rt.hostilePlayerRelation(division, caster, target) {
		return hostile
	}
	aJob, aCape := rt.playerRelationEquipment(caster)
	bJob, bCape := rt.playerRelationEquipment(target)
	if aCape != 0 && bCape != 0 || aJob != 0 && bJob != 0 {
		return !hostile
	}
	if (aCape != 0) != (bCape != 0) || (aJob != 0) != (bJob != 0) {
		return hostile
	}
	return true
}

/*
================
playerAttackTargetRefusal

5293A0, with the zero control flags supplied by 58CF1F. Both region records
must permit battle. Party protection precedes cape/job and ordinary PK rules.
================
*/
func (rt *Runtime) playerAttackTargetRefusal(division string, caster, target *enterworld.Character, now int64) uint16 {
	if caster.ID == target.ID {
		return 0x3006
	}
	for _, actor := range []*enterworld.Character{caster, target} {
		at := rt.liveSpawn(simulation.WorldKey(division, actor.Name), actor, now)
		allowed, known := worldgeom.RegionPlayerCombat(at.RegionID)
		if !known {
			return 0x3009
		}
		if !allowed {
			return 0x3018
		}
	}
	if rt.sharePartyObject(division, caster, target) {
		return 0x3022
	}
	var aggressionWeight uint32
	for _, weight := range target.Aggressions {
		aggressionWeight += weight
	}
	if aggressionWeight == 1 {
		return 0x3009
	}
	if rt.hostilePlayerEquipment(caster, target) {
		return 0
	}
	if caster.Aggressions[enterworld.ObjectIDForCharacter(target)] != 0 {
		return 0
	}
	if caster.Level == nil || *caster.Level < playerCombatMinimumLevel {
		return 0x3016
	}
	if target.Level == nil || *target.Level < playerCombatMinimumLevel {
		return 0x3017
	}
	if target.PVPState() != 0 {
		return 0
	}
	if caster.PK != nil {
		if caster.PK.Penalty >= playerCombatMaxPenalty {
			return 0x3015
		}
		if caster.PK.DailyCount >= playerCombatMaxDailyPK {
			return 0x3014
		}
	}
	return 0
}
