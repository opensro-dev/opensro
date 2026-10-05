/*
===========================================================================

monsterarea.go - the further victims of a monster's area attack

A monster skill's action area (efr kind 1, RefSkill +0x28C) selects through
the same TargetSelection_DispatchByShape the player areas use (skillarea.go,
skillarea_directional.go), with the monster as the caster. Its candidates
are the players in the monster's world and the companions they keep; the
monster's own hostility (simulation.MonsterAreaStrikes) filters them, so a
monster never strikes another monster here.

===========================================================================
*/

package action

import (
	"math"
	"sort"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
monsterAreaVictim

One candidate a monster area may strike. A companion names its owner; a
rider's hit goes to the vehicle (ride), as the primary's does
(monsterAttackStage), while the rider's own status and protection decide
whether it is struck.
================
*/
type monsterAreaVictim struct {
	owner     *enterworld.Character
	gid       uint32
	companion bool
	pose      simulation.Spawn
	radius    float64
	hostility simulation.MonsterAreaCandidate
}

/*
================
monsterAreaVictims

Every candidate the action area admits, in the port's GID order (shape 6:
nearest first), after the primary and up to MaxTargets in all.
================
*/
func (rt *Runtime) monsterAreaVictims(in monsterStrikeInput, from simulation.Spawn, primary monsterStrike) []monsterAreaVictim {
	area := in.skill.ActionArea
	casterRadius := float64(in.instance.Ref.BodyRadius)
	var candidates []monsterAreaVictim
	for _, victim := range rt.monsterAreaCandidates(in) {
		if victim.gid == primary.gid || victim.hostility.Gid == primary.gid ||
			simulation.IsDungeonRegion(victim.pose.RegionID) != simulation.IsDungeonRegion(from.RegionID) ||
			!simulation.MonsterAreaStrikes(in.instance, victim.hostility) {
			continue
		}
		candidates = append(candidates, victim)
	}
	distance := func(center, at simulation.Spawn) float64 {
		return math.Hypot(simulation.WorldDistance2D(center, at), at.Y-center.Y)
	}
	var admitted []monsterAreaVictim
	switch area.Shape {
	case 1, 2:
		// 58A088 centres shape 1 on the caster, 58A831 shape 2 on the
		// selected target; the reach adds both body radii (58AB6D).
		center := primary.pose
		if area.Shape == 1 {
			center = from
		}
		for _, victim := range candidates {
			if distance(center, victim.pose) <= float64(area.Radius)+casterRadius+victim.radius {
				admitted = append(admitted, victim)
			}
		}
		sort.Slice(admitted, func(i, j int) bool { return admitted[i].gid < admitted[j].gid })
	case 6:
		for _, victim := range candidates {
			if distance(primary.pose, victim.pose) <= float64(area.Radius) {
				admitted = append(admitted, victim)
			}
		}
		sort.Slice(admitted, func(i, j int) bool {
			di, dj := distance(primary.pose, admitted[i].pose), distance(primary.pose, admitted[j].pose)
			if di != dj {
				return di < dj
			}
			return admitted[i].gid < admitted[j].gid
		})
	case 3, 4:
		reach := float32(uint16(in.skill.ActionRange))
		toPrimary := relative(from, primary.pose)
		dir, center := toPrimary, primary.pose
		if area.Shape == 3 {
			toPrimary.y = 0
			unit := toPrimary.normalized()
			dir, center = vec3{unit.x * reach, unit.y * reach, unit.z * reach}, from
		}
		for _, victim := range candidates {
			if distance(center, victim.pose) > directionalSearchRadius {
				continue
			}
			if inDirectionalShape(relative(from, victim.pose), dir, int32(casterRadius), int32(victim.radius), area.Radius) {
				admitted = append(admitted, victim)
			}
		}
		sort.Slice(admitted, func(i, j int) bool { return admitted[i].gid < admitted[j].gid })
	}
	if len(admitted) > int(area.MaxTargets)-1 {
		admitted = admitted[:int(area.MaxTargets)-1]
	}
	return admitted
}

/*
================
monsterAreaCandidates

The living players in the monster's world, each followed by its summoned
companions. A rider stands in for its vehicle.
================
*/
func (rt *Runtime) monsterAreaCandidates(in monsterStrikeInput) []monsterAreaVictim {
	var out []monsterAreaVictim
	for _, character := range rt.deps.CharactersForDivision(in.division) {
		snapshot := rt.characterSnapshot(in.division, character)
		if snapshot == nil || snapshot.DeletePending || !enterworld.CharacterAlive(snapshot) {
			continue
		}
		if _, sameWorld := rt.characterMonster(in.division, snapshot, in.instance.Gid); !sameWorld {
			continue
		}
		radius, ok := rt.deps.CharacterBodyRadius(snapshot)
		if !ok {
			continue
		}
		gid := enterworld.ObjectIDForCharacter(snapshot)
		player := monsterAreaVictim{owner: character, gid: gid, radius: radius,
			pose: rt.liveSpawn(simulation.WorldKey(in.division, snapshot.Name), snapshot, in.now),
			hostility: simulation.MonsterAreaCandidate{Gid: gid, NativeBodyStatus: snapshot.NativeBodyStatus,
				Guard: rt.FirstAttackGuard(in.division, gid, in.now)}}
		if ride := ridingCOS(snapshot); ride != 0 {
			player.gid, player.companion = ride, true
		}
		out = append(out, player)
		for _, pet := range rt.companionTargets(in.division, gid, in.now) {
			out = append(out, monsterAreaVictim{owner: character, gid: pet.Gid, companion: true, pose: pet.Pose,
				radius: float64(pet.BodyRadius), hostility: simulation.MonsterAreaCandidate{Gid: pet.Gid, OwnerGid: gid,
					Band: pet.Band, NativeBodyStatus: pet.NativeBodyStatus, Guard: player.hostility.Guard}})
		}
	}
	return out
}

/*
================
strikeMonsterAreaVictim

One further victim at its reduced share, through the same strike as a
primary of its kind.
================
*/
func (rt *Runtime) strikeMonsterAreaVictim(in monsterStrikeInput, victim monsterAreaVictim) monsterStrikeOutcome {
	snapshot := rt.characterSnapshot(in.division, victim.owner)
	if snapshot == nil {
		return monsterStrikeOutcome{}
	}
	if victim.companion {
		pet := snapshot.CompanionByGID(victim.gid)
		if pet == nil || !pet.Summoned || pet.CurrentHP == 0 {
			return monsterStrikeOutcome{}
		}
		ref, ok := rt.cosReference(pet)
		if !ok {
			return monsterStrikeOutcome{}
		}
		return rt.monsterStrikeCOS(in, victim.owner, victim.owner.CompanionByGID(victim.gid), ref, victim.pose)
	}
	defender, _, err := rt.playerCombatStats(in.division, snapshot)
	if err != nil {
		return monsterStrikeOutcome{}
	}
	return rt.monsterStrikePlayer(in, victim.owner, snapshot, defender, victim.pose)
}
