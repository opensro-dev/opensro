/*
===========================================================================

monsterarea.go - the further victims of a monster's area attack

A monster skill's action area (efr kind 1, RefSkill +0x28C) selects through
the same shape dispatcher the player areas use (skillarea.go,
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

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
monsterAreaVictim

One candidate a monster area may strike. A companion names its owner; a
rider's hit goes to the vehicle (ride), as the primary's does
(monsterAttackStage), while the rider's own status decides whether it is
struck (the reference server passes checkFirstAttack=0).
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
monsterAreaCandidateView

One living player's targeting facts for the area scan, read as values
under the authority read door (#1820 part C): the scan used to detach
every division record to weigh it. owner stays the live record - the
strike lane re-reads it through its own doors, as it always did.
================
*/
type monsterAreaCandidateView struct {
	owner            *enterworld.Character
	gid              uint32
	worldInstance    uint32
	radius           float64
	nativeBodyStatus uint8
	jobState         uint8
	ride             uint32
}

/*
================
monsterAreaVictims

Every candidate the action area admits, in the port's GID order (shape 6:
nearest first), after the primary and up to MaxTargets in all.
================
*/
func (rt *Runtime) monsterAreaVictims(in monsterStrikeInput, from simulation.Spawn, primary monsterStrike) []monsterAreaVictim {
	var candidates []monsterAreaVictim
	for _, victim := range rt.monsterAreaCandidates(in) {
		if victim.gid == primary.gid || victim.hostility.Gid == primary.gid ||
			!worldgeom.SamePlane(victim.pose.RegionID, from.RegionID) ||
			!simulation.MonsterAreaStrikes(in.instance, victim.hostility) {
			continue
		}
		candidates = append(candidates, victim)
	}
	return selectCreatureArea(in.skill, from, primary.pose, in.instance.Ref.BodyRadius, candidates)
}

/*
================
selectCreatureArea

The reference server dispatches the same shape geometry for monsters and companions.
The caller supplies candidates admitted by its own hostility predicate.
================
*/
func selectCreatureArea(skill enterworld.SkillRow, from, primary simulation.Spawn, casterRadius float64, candidates []monsterAreaVictim) []monsterAreaVictim {
	area := skill.ActionArea
	if area.MaxTargets <= 1 {
		return nil
	}
	distance := func(center, at simulation.Spawn) float64 {
		return math.Hypot(simulation.WorldDistance2D(center, at), at.Y-center.Y)
	}
	var admitted []monsterAreaVictim
	switch area.Shape {
	case 1, 2:
		// The reference server centres shape 1 on the caster, shape 2 on the
		// selected target; the reach adds both body radii.
		center := primary
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
			if distance(primary, victim.pose) <= float64(area.Radius) {
				admitted = append(admitted, victim)
			}
		}
		sort.Slice(admitted, func(i, j int) bool {
			di, dj := distance(primary, admitted[i].pose), distance(primary, admitted[j].pose)
			if di != dj {
				return di < dj
			}
			return admitted[i].gid < admitted[j].gid
		})
	case 3, 4:
		reach := float32(uint16(skill.ActionRange))
		toPrimary := relative(from, primary)
		dir, center := toPrimary, primary
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
	// The division's records are weighed as values under ONE read door;
	// the world, guard and companion questions run outside it on those
	// values, in the order the per-record scan asked them.
	characters := rt.deps.CharactersForDivision(in.division)
	var views []monsterAreaCandidateView
	rt.deps.Read(in.division, func() {
		for _, character := range characters {
			if character == nil || character.DeletePending || !enterworld.CharacterAlive(character) {
				continue
			}
			radius, ok := rt.deps.CharacterBodyRadius(character)
			if !ok {
				continue
			}
			views = append(views, monsterAreaCandidateView{
				owner:            character,
				gid:              enterworld.ObjectIDForCharacter(character),
				worldInstance:    domain.CharacterWorldInstance(character),
				radius:           radius,
				nativeBodyStatus: character.NativeBodyStatus,
				jobState:         enterworld.PartyJobClass(character),
				ride:             ridingCOS(character),
			})
		}
	})
	var out []monsterAreaVictim
	for _, view := range views {
		if _, sameWorld := rt.characterMonsterFor(in.division, view.owner.Name, view.worldInstance, in.instance.Gid); !sameWorld {
			continue
		}
		gid := view.gid
		guard := rt.FirstAttackGuard(in.division, gid, in.now)
		player := monsterAreaVictim{owner: view.owner, gid: gid, radius: view.radius,
			pose: rt.liveSpawn(simulation.WorldKey(in.division, view.owner.Name), view.owner, in.now),
			hostility: simulation.MonsterAreaCandidate{Gid: gid, NativeBodyStatus: view.nativeBodyStatus,
				Guard: guard, JobState: view.jobState}}
		if view.ride != 0 {
			player.gid, player.companion = view.ride, true
		}
		out = append(out, player)
		for _, pet := range rt.companionTargets(in.division, gid, in.now) {
			out = append(out, monsterAreaVictim{owner: view.owner, gid: pet.Gid, companion: true, pose: pet.Pose,
				radius: float64(pet.BodyRadius), hostility: simulation.MonsterAreaCandidate{Gid: pet.Gid, OwnerGid: gid,
					Band: pet.Band, NativeBodyStatus: pet.NativeBodyStatus, Guard: guard, JobState: view.jobState}})
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
