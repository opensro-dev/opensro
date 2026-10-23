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
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/combat"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	playerCombatMinimumLevel = 20
	playerCombatMaxPenalty   = 200000
	playerCombatMaxDailyPK   = 15
	freeBattleAllOpponents   = 5
	// The companion's refusal for an attack on the owner's free-battle team.
	companionTeamRefused uint16 = 0x3020
	freeBattleGroupField        = "itemParam2_2a0"
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

Either equipment relation: opposing capes or opposing jobs.
================
*/
func (rt *Runtime) hostilePlayerEquipment(caster, target *enterworld.Character) bool {
	return rt.hostilePlayerCapes(caster, target) || rt.hostilePlayerJobs(caster, target)
}

/*
================
hostilePlayerCapes

Different capes fight, while cape five also fights itself.
================
*/
func (rt *Runtime) hostilePlayerCapes(caster, target *enterworld.Character) bool {
	_, aCape := rt.playerRelationEquipment(caster)
	_, bCape := rt.playerRelationEquipment(target)
	return aCape != 0 && bCape != 0 && (aCape != bCape || aCape == freeBattleAllOpponents)
}

/*
================
inFreeBattle

The player wears a free-battle cape, group 1..5.
================
*/
func (rt *Runtime) inFreeBattle(c *enterworld.Character) bool {
	_, cape := rt.playerRelationEquipment(c)
	return cape >= 1 && cape <= freeBattleAllOpponents
}

/*
================
companionTeamRefusal

A companion never strikes a player on its owner's free-battle team. With
the owner in free battle (groups 1..5) and its group not the
everyone-opposes group 5, a target in the same group is refused 0x3020
before the owner's own target check is consulted, so guild
war, jobs or aggression cannot open a team-mate to the owner's pet.
================
*/
func (rt *Runtime) companionTeamRefusal(owner, target *enterworld.Character) uint16 {
	_, ownerCape := rt.playerRelationEquipment(owner)
	_, targetCape := rt.playerRelationEquipment(target)
	if ownerCape >= 1 && ownerCape < freeBattleAllOpponents && ownerCape == targetCape {
		return companionTeamRefused
	}
	return 0
}

/*
================
hostilePlayerJobs

Thieves oppose traders and hunters;
traders and hunters are allies. Capes take precedence when both wear one.
================
*/
func (rt *Runtime) hostilePlayerJobs(caster, target *enterworld.Character) bool {
	aJob, aCape := rt.playerRelationEquipment(caster)
	bJob, bCape := rt.playerRelationEquipment(target)
	if aCape != 0 && bCape != 0 {
		return false
	}
	return aJob != 0 && bJob != 0 && aJob != bJob && (aJob == 2 || bJob == 2)
}

/*
================
hostilePlayerRelation

The area enemy selector does not acquire neutral bystanders merely because
a deliberate primary attack could enter the PK branch. Criminal state counts
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
	if rt.guildsAtWar(division, caster, target) {
		return true
	}
	if caster.Level == nil || target.Level == nil || *caster.Level < playerCombatMinimumLevel || *target.Level < playerCombatMinimumLevel {
		return false
	}
	return caster.PVPState() != 0 || target.PVPState() != 0
}

/*
================
playerSkillRelationAllowed

The skill's relation test compares the common relation and each actor's highest relation.
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

// The player-attack check's control flags. A direct attack passes none; an
// area selection passes them through its hostile-target test: 3 for a
// player caster, 1 for any other (a pet).
const (
	// playerAttackIndirect refuses a neutral target the caster is not
	// already fighting (not in its aggression map).
	playerAttackIndirect uint8 = 1
	// playerAttackSparesAggressor refuses an aggressor target (state 1)
	// to a white caster (state 0).
	playerAttackSparesAggressor uint8 = 2
	// playerAttackArea and petAttackArea are the area selection's two modes.
	playerAttackArea = playerAttackIndirect | playerAttackSparesAggressor
	petAttackArea    = playerAttackIndirect
	// playerAttackFlagRefused stands for the flag checks' returns, which
	// refuse without writing a code.
	playerAttackFlagRefused uint16 = 0x3006
)

/*
================
playerAttackTargetRefusal

The player-attack check with the zero control flags of a direct attack. Both region records
must permit battle. Party protection precedes cape/job and ordinary PK rules.
================
*/
func (rt *Runtime) playerAttackTargetRefusal(division string, caster, target *enterworld.Character, now int64) uint16 {
	return rt.playerAttackRefusal(division, caster, target, now, 0)
}

/*
================
playerAttackRefusal

The player-attack check with its control flags. An area strike passes
playerAttackArea (a pet petAttackArea): a white caster's area spares an
aggressor, and a neutral target the caster is not fighting is refused
before the PK limits. Without them, every area aimed at a monster struck the white
players standing in it, made the caster a PK and, because each strike renews
the client's twenty-second no-Alt window on that player, kept that player
open to attack indefinitely.
================
*/
func (rt *Runtime) playerAttackRefusal(division string, caster, target *enterworld.Character, now int64, flags uint8) uint16 {
	if caster.ID == target.ID {
		return 0x3006
	}
	for _, actor := range []*enterworld.Character{caster, target} {
		at := rt.liveSpawn(simulation.WorldKey(division, actor.Name), actor, now)
		allowed, known := rt.regionCombat.PlayerCombat(at.RegionID)
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
	if rt.hostilePlayerCapes(caster, target) {
		return 0
	}
	if code := rt.protectedCaravanRefusal(caster, target); code != 0 {
		return code
	}
	if rt.guildsAtWar(division, caster, target) {
		return 0
	}
	// Opposing jobs are admitted at once: the suit's job is set when it is
	// worn, before its activation runs out. The 0x3019 refusal sits behind a
	// second job-hostility test that sees the same answer, so it is never
	// sent; the activation only holds the suit on (jobStripRefusal, 0x47).
	if rt.hostilePlayerJobs(caster, target) {
		return 0
	}
	if flags&playerAttackSparesAggressor != 0 && caster.PVPState() == 0 && target.PVPState() == 1 {
		return playerAttackFlagRefused
	}
	if caster.Aggressions[enterworld.ObjectIDForCharacter(target)] != 0 {
		return 0
	}
	// An enemy target (aggressor or murderer, both at
	// level 20) is legal; anything else is neutral, which an area refuses.
	enemy := caster.Level != nil && *caster.Level >= playerCombatMinimumLevel &&
		target.Level != nil && *target.Level >= playerCombatMinimumLevel && target.PVPState() != 0
	if flags&playerAttackIndirect != 0 && !enemy {
		return playerAttackFlagRefused
	}
	if caster.Level == nil || *caster.Level < playerCombatMinimumLevel {
		return 0x3016
	}
	if target.Level == nil || *target.Level < playerCombatMinimumLevel {
		return 0x3017
	}
	// With both players at level 20, an aggressor or murderer
	// target is a legal enemy, so the attacker's PK limits are not read.
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

/*
================
protectedCaravanRefusal

Checked between the cape and the guild-war relations of the player-attack
check: a dressed thief may not attack a dressed
trader whose transport carries a level-1 caravan (0x3024
CANT_ATTACK_CARAVAN_LEVEL1), and that trader may not attack a thief
(0x3006).

INFERENCE: the later server also requires the trader's opt-in trade-safety
state (published on 0x34D5 from TRADE_SAFETY_NUM). The v1.150 client has no 0x34D5 handler, so that
mode did not exist; it keeps the 0x3024 text, so the level-1 caravan
protection itself is read as always on.
================
*/
func (rt *Runtime) protectedCaravanRefusal(caster, target *enterworld.Character) uint16 {
	casterJob, _ := rt.playerRelationEquipment(caster)
	targetJob, _ := rt.playerRelationEquipment(target)
	switch {
	case casterJob == domain.JobThief && targetJob == domain.JobTrader && rt.levelOneCaravan(target):
		return 0x3024
	case casterJob == domain.JobTrader && targetJob == domain.JobThief && rt.levelOneCaravan(caster):
		return 0x3006
	}
	return 0
}

/*
================
levelOneCaravan

A summoned transport carrying trade goods whose cargo is difficulty tier 1
or below.
================
*/
func (rt *Runtime) levelOneCaravan(c *enterworld.Character) bool {
	tier, _ := rt.caravanTier(c)
	return vehicleCarriesGoods(rt, c) && tier <= 1
}
