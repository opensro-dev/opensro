/*
===========================================================================

skillcure.go - skill cures (593F50) and party area reach

===========================================================================
*/

package action

import (
	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
cureTarget

One resolved entry of the cure action vector: a player, or a summoned pet
named by its owner and gid.
================
*/
type cureTarget struct {
	player *enterworld.Character
	pet    monsterCastRecipient
}

/*
==================
resolveSkillCureTargets

The 593F50 action vector, resolved to records. A non-empty vector keeps its
order; a missing id is skipped. An empty vector names the caster only when
RefSkill+0x98 (column 26 TargetGroup_Self) is set.

Runs before the caster's door: the party selection, the gid index and the
pet-owner scan all read the authority store, whose door is not reentrant.
A read from inside the caster's Update callback waits on the lock that
callback holds and stops the shard. caster is the live record (the Self
entry is cured through it); view is its snapshot for the reads.
==================
*/
func (rt *Runtime) resolveSkillCureTargets(division string, caster, view *enterworld.Character, skill enterworld.SkillRow, cast wire.SkillAction, now int64) []cureTarget {
	vector := rt.skillCureVector(division, view, skill, cast, now)
	if len(vector) == 0 {
		if skill.Targets.Self {
			return []cureTarget{{player: caster}}
		}
		return nil
	}
	var out []cureTarget
	for _, gid := range vector {
		if player := rt.findCharacterByGid(division, gid); player != nil {
			out = append(out, cureTarget{player: player})
		} else if owner := rt.characterByCosGID(division, gid); owner != nil {
			out = append(out, cureTarget{pet: monsterCastRecipient{owner, gid}})
		}
	}
	return out
}

/*
==================
applySkillCure

applySkillCure is the 593F50 cure at release over the targets
resolveSkillCureTargets prepared before the door. It reads no record
through the store. Each cured player's private snapshot (0x36C7) goes to
that player: the caster's own in actor, everyone else's through recipients.
==================
*/
func (rt *Runtime) applySkillCure(division string, caster *enterworld.Character, skill enterworld.SkillRow, targets []cureTarget, now int64) (actor, public []wire.Frame, recipients []RecipientFrames) {
	for _, target := range targets {
		if target.player == nil {
			public = append(public, rt.curePet(division, target.pet, skill, now)...)
			continue
		}
		private, shared := rt.cureCharacter(division, target.player, skill, now)
		public = append(public, shared...)
		if len(private) == 0 {
			continue
		}
		if target.player.ID == caster.ID {
			actor = append(actor, private...)
			continue
		}
		recipients = append(recipients, RecipientFrames{CharacterID: target.player.ID, Frames: private})
	}
	return actor, public, recipients
}

/*
==================
skillCureVector

skillCureVector is the action target list 593F50 walks. efr with select 4
or 5 (and a shape other than 5 or 6) uses TargetSelection_Party (58BEF0):
other party members on the same region within the radius. Column-22 rows
use the explicit target gid. Anything else stays empty.
==================
*/
func (rt *Runtime) skillCureVector(division string, caster *enterworld.Character, skill enterworld.SkillRow, cast wire.SkillAction, now int64) []uint32 {
	area := skill.Abnormal.EffectArea
	if area.Present {
		shape := area.Shape
		if shape != 5 && shape != 6 && (area.Select == 4 || area.Select == 5) {
			shape = 5
		}
		if shape == 5 {
			return rt.partyCureTargets(division, caster, area.Radius, area.Select&1 != 0, skill.Abnormal.AdmitDeadParty, now)
		}
		return nil
	}
	if skill.TargetRequired && cast.HasTarget && cast.TargetGid != 0 {
		return []uint32{cast.TargetGid}
	}
	return nil
}

/*
==================
partyCureTargets

partyCureTargets is TargetSelection_Party (58BEF0). The efr block's sixth
argument (+0x14) bit 0 pushes the caster first. Every other party member
joins when it is alive (vfunc F8 == 1). The resu tag (0x72657375) fills
RefSkill+0x330; a present block skips that alive check. Shipped rebirth
group rows set it. Cure rows do not. On the same plane with both
sector bytes within one of the caster's (430CE0), in the same world, and
within the efr radius (+8) by 3D distance, inclusive.
==================
*/
func (rt *Runtime) partyCureTargets(division string, caster *enterworld.Character, radius uint32, includeSelf, admitDead bool, now int64) []uint32 {
	casterGID := enterworld.ObjectIDForCharacter(caster)
	var out []uint32
	if includeSelf {
		out = append(out, casterGID)
	}
	if rt.RewardParties == nil {
		return out
	}
	var members []uint32
	for _, party := range rt.RewardParties(division) {
		for _, gid := range party.Members {
			if gid == casterGID {
				members = party.Members
			}
		}
	}
	from := rt.liveSpawn(simulation.WorldKey(division, caster.Name), caster, now)
	for _, gid := range members {
		if gid == casterGID {
			continue
		}
		other := rt.findCharacterByGid(division, gid)
		if other == nil || !admitDead && !enterworld.CharacterAlive(other) {
			continue
		}
		to := rt.liveSpawn(simulation.WorldKey(division, other.Name), other, now)
		if !partyAreaReach(from, to, radius) {
			continue
		}
		out = append(out, gid)
	}
	return out
}

// partyAreaReach is 430CE0 followed by the relative 3D length test.
/*
================
partyAreaReach
================
*/
func partyAreaReach(from, to simulation.Spawn, radius uint32) bool {
	return samePlaneAdjacent(from, to) && distance3D(from, to) <= float64(radius)
}

/*
==================
samePlaneAdjacent

samePlaneAdjacent is Pos_AreSamePlaneAndAdjacentSectors (430CE0); the
world package owns the rule.
==================
*/
func samePlaneAdjacent(from, to simulation.Spawn) bool {
	return world.SamePlaneAdjacent(from.RegionID, to.RegionID)
}

// distance3D is Vec3_Length of Pos_GetRelative3DOrIncompatibleSentinel.
/*
================
distance3D
================
*/
func distance3D(from, to simulation.Spawn) float64 {
	// Reuse the native vector's float stores. Computing a planar square root
	// and squaring it again changes the boundary used by every player area.
	return float64(relative(from, to).length())
}

/*
================
cureCharacter
================
*/
func (rt *Runtime) cureCharacter(division string, target *enterworld.Character, skill enterworld.SkillRow, now int64) (actor, public []wire.Frame) {
	owner := rt.newPlayerAbnormalOwner(division, target, now)
	random := &abnormalRandom{rt: rt, actor: criticalActor{division: division, character: target.Name}}
	owner.changed = owner.block.Cure(owner, nil, skillLevelCure(skill), skillPill(skill), skillCureLimit(skill), random.Rand)
	if random.err != nil {
		return nil, nil
	}
	owner.commit()
	published := rt.playerAbnormalPublication(division, target, owner)
	return published.actor, published.public
}

// curePet returns the pet's shared mask frame; a COS has no private snapshot.
/*
================
curePet
================
*/
func (rt *Runtime) curePet(division string, recipient monsterCastRecipient, skill enterworld.SkillRow, now int64) []wire.Frame {
	ownerCharacter := recipient.character
	owner := rt.newCosAbnormalOwnerForPet(division, ownerCharacter, ownerCharacter.CompanionByGID(recipient.gid), now)
	random := &abnormalRandom{rt: rt, actor: criticalActor{division: division, character: ownerCharacter.Name}}
	owner.changed = owner.block.Cure(owner, nil, skillLevelCure(skill), skillPill(skill), skillCureLimit(skill), random.Rand)
	if random.err != nil {
		return nil
	}
	owner.commit()
	return rt.cosAbnormalPublication(recipient.gid, owner)
}

/*
================
skillLevelCure
================
*/
func skillLevelCure(skill enterworld.SkillRow) *abnormal.SkillLevelCure {
	if !skill.Abnormal.Curt {
		return nil
	}
	return &abnormal.SkillLevelCure{Mask: skill.Abnormal.CurtMask, Level: skill.Abnormal.CurtLevel}
}

/*
================
skillPill
================
*/
func skillPill(skill enterworld.SkillRow) *[3]int32 {
	if !skill.Abnormal.Curl {
		return nil
	}
	stored := [3]int32{skill.Abnormal.CurlMask, skill.Abnormal.CurlChance, skill.Abnormal.CurlGrade}
	return &stored
}

/*
================
skillCureLimit
================
*/
func skillCureLimit(skill enterworld.SkillRow) int {
	if !skill.Abnormal.RcurSet {
		return -1
	}
	return int(skill.Abnormal.Rcur)
}
