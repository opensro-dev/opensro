/*
===========================================================================

skillcure.go - skill cures (593F50) and party area reach

===========================================================================
*/

package action

import (
	"math"

	"opensro.online/server/internal/game/abnormal"
	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
==================
applySkillCure

applySkillCure is the 593F50 cure at release. A non-empty action vector is
cured entry by entry; a missing id is skipped. An empty vector cures the
caster only when RefSkill+0x98 (column 23 TargetType_Animal) is set.
Every shipped cure row has columns 22 and 23 equal. Each cured player's
private snapshot (0x36C7) goes to that player: the caster's own in actor,
everyone else's through recipients.
==================
*/
func (rt *Runtime) applySkillCure(division string, caster *enterworld.Character, skill enterworld.SkillRow, cast wire.SkillAction, now int64) (actor, public []wire.Frame, recipients []RecipientFrames) {
	route := func(target *enterworld.Character, private, shared []wire.Frame) {
		public = append(public, shared...)
		if len(private) == 0 {
			return
		}
		if target.ID == caster.ID {
			actor = append(actor, private...)
			return
		}
		recipients = append(recipients, RecipientFrames{CharacterID: target.ID, Frames: private})
	}
	targets := rt.skillCureVector(division, caster, skill, cast, now)
	if len(targets) == 0 {
		if skill.Targets.Animal {
			private, shared := rt.cureCharacter(division, caster, skill, now)
			route(caster, private, shared)
		}
		return actor, public, recipients
	}
	for _, gid := range targets {
		if player := rt.findCharacterByGid(division, gid); player != nil {
			private, shared := rt.cureCharacter(division, player, skill, now)
			route(player, private, shared)
		} else if owner := rt.characterByCosGID(division, gid); owner != nil {
			public = append(public, rt.curePet(division, monsterCastRecipient{owner, gid}, skill, now)...)
		}
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

samePlaneAdjacent is Pos_AreSamePlaneAndAdjacentSectors (430CE0). Two
dungeon positions compare only within one dungeon region; outdoors both
sector bytes must be within one.
==================
*/
func samePlaneAdjacent(from, to simulation.Spawn) bool {
	fromDungeon, toDungeon := simulation.IsDungeonRegion(from.RegionID), simulation.IsDungeonRegion(to.RegionID)
	if fromDungeon != toDungeon {
		return false
	}
	if fromDungeon {
		return from.RegionID == to.RegionID
	}
	dx := int(from.RegionID&0xff) - int(to.RegionID&0xff)
	dz := int(from.RegionID>>8) - int(to.RegionID>>8)
	return dx >= -1 && dx <= 1 && dz >= -1 && dz <= 1
}

// distance3D is Vec3_Length of Pos_GetRelative3DOrIncompatibleSentinel.
/*
================
distance3D
================
*/
func distance3D(from, to simulation.Spawn) float64 {
	planar := simulation.WorldDistance2D(from, to)
	dy := to.Y - from.Y
	return math.Sqrt(planar*planar + dy*dy)
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
