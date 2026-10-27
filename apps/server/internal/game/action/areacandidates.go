/*
===========================================================================

areacandidates.go - every object an offensive area may strike

The reference server's selectors (the shape dispatcher and its shape
owners) walk every character object around their centre
and keep the ones its target validation admits: living
monsters of the population, and players the caster may attack
(hostilePlayerRefusal). Monsters and players are one candidate list here,
in the port's deterministic order: nearest first for shape 6, otherwise by
object ID, as the monster registry already orders its own.

===========================================================================
*/

package action

import (
	"math"
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/enterworld"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

const (
	// The efr select bits an offensive area reads: hostile characters
	// (Mana Drought authors 8 alone) and non-character objects.
	areaSelectHostile = 0x08
	areaSelectObjects = 0x10
)

/*
================
areaQuery

One selection: around center within reach, adding each candidate's body
radius unless nearest (shape 6) asks for bare distances.
================
*/
type areaQuery struct {
	// selects is the efr +0x14 mask (the reference server's around-source selector):
	// 0x08 admits hostile characters, 0x10 non-character objects.
	selects  uint8
	division string
	caster   *enterworld.Character
	skill    enterworld.SkillRow
	lease    instance.Lease
	center   simulation.Spawn
	reach    float64
	nearest  bool
	now      int64
}

/*
================
areaCandidate
================
*/
type areaCandidate struct {
	target   combatTarget
	distance float64
	radius   float64
}

/*
================
areaCandidates

The living monsters and attackable players of the caster's world around
the query's centre, ordered for selection.
================
*/
func (rt *Runtime) areaCandidates(q areaQuery) []areaCandidate {
	var out []areaCandidate
	if rt.Monsters != nil && q.selects&areaSelectObjects != 0 {
		for _, m := range rt.Monsters.CombatCandidatesInPopulation(q.division, q.lease, q.center, q.reach, q.now, q.nearest) {
			mover, ok := rt.Monsters.Mover(q.division, m.Gid)
			if !ok {
				continue
			}
			pose := mover.LivePoseAt(q.now, nil)
			at := simulation.Spawn{RegionID: pose.RegionID, X: pose.X, Y: pose.Y, Z: pose.Z}
			instance := m
			out = append(out, areaCandidate{target: combatTarget{gid: m.Gid, monster: &instance, at: at},
				distance: areaDistance(q.center, at), radius: m.BodyRadius()})
		}
	}
	if q.selects&areaSelectHostile != 0 {
		out = append(out, rt.areaPlayerCandidates(q)...)
	}
	sort.SliceStable(out, func(i, j int) bool {
		if q.nearest && out[i].distance != out[j].distance {
			return out[i].distance < out[j].distance
		}
		return out[i].target.gid < out[j].target.gid
	})
	return out
}

/*
================
areaPlayerCandidates

Present players of the caster's world within reach (plus their body
radius) that the row may strike.
================
*/
func (rt *Runtime) areaPlayerCandidates(q areaQuery) []areaCandidate {
	if q.caster == nil {
		return nil
	}
	world := domain.CharacterWorldInstance(q.caster)
	var out []areaCandidate
	// The door pass's out-values live for the whole walk: fresh slots
	// per player would box three variables per head the strike weighs.
	var snapshot *enterworld.Character
	var radius float64
	var petGIDs []uint32
	for _, present := range rt.PopulationPlayers(q.division, q.now) {
		if present.World != world || present.GID == enterworld.ObjectIDForCharacter(q.caster) ||
			!worldgeom.SamePlane(present.Spawn.RegionID, q.center.RegionID) {
			continue
		}
		distance := areaDistance(q.center, present.Spawn)
		player := rt.findCharacterByGid(q.division, present.GID)
		if player == nil {
			continue
		}
		// One door pass weighs the gates on the live record and
		// detaches a copy only for a player the strike may still take:
		// dead and deletion-pending players, riders and out-of-range
		// players never pay for their whole record (#1820 part C).
		// Companion gids ride out as values; resolveCOSTarget re-judges
		// each companion's summons and form on its owner itself.
		snapshot, radius, petGIDs = nil, 0, nil
		rt.deps.Read(q.division, func() {
			if player.DeletePending || !enterworld.CharacterAlive(player) {
				return
			}
			for _, pet := range player.Companions() {
				if pet != nil && pet.Summoned {
					petGIDs = append(petGIDs, pet.GID)
				}
			}
			// The reference server excludes a mounted player (it reads
			// the ride and skips the rider); the ride itself is a
			// candidate below.
			if ridingCOS(player) != 0 {
				return
			}
			bodyRadius, ok := rt.deps.CharacterBodyRadius(player)
			if !ok {
				// The old walk dropped an unmeasurable player whole,
				// companions included; keep that verdict.
				petGIDs = nil
				return
			}
			limit := q.reach
			if !q.nearest {
				limit += bodyRadius
			}
			if distance > limit {
				return
			}
			radius = bodyRadius
			snapshot = player.Snapshot()
		})
		if snapshot != nil {
			// The reference server asks its attack validation with mode 3 for a player caster.
			if rt.hostilePlayerRefusalWith(q.division, q.caster, snapshot, q.skill, q.now, playerAttackArea) == 0 {
				out = append(out, areaCandidate{target: combatTarget{gid: present.GID, player: player, snapshot: snapshot,
					at: present.Spawn}, distance: distance, radius: radius})
			}
		}
		// The present player's companions are candidates through their
		// owner (the reference server's mode 3 on the COS), a ridden transport included:
		// enumerated from the owner's record, not companionTargets, whose
		// mounted filter serves the pet strike lane.
		for _, gid := range petGIDs {
			target, ok := rt.resolveCOSTarget(q.division, q.caster, gid, q.now)
			if !ok || !worldgeom.SamePlane(target.at.RegionID, q.center.RegionID) {
				continue
			}
			cosDistance := areaDistance(q.center, target.at)
			limit := q.reach
			if !q.nearest {
				limit += float64(target.cosRef.Parameters.BodyRadius)
			}
			if cosDistance > limit {
				continue
			}
			if rt.cosTargetRefusalWith(q.division, q.caster, target, q.now, playerAttackArea) != 0 {
				continue
			}
			out = append(out, areaCandidate{target: target, distance: cosDistance,
				radius: float64(target.cosRef.Parameters.BodyRadius)})
		}
	}
	return out
}

/*
================
areaDistance

The monster registry's candidate distance: planar world distance, then
the height difference (as the reference server's around-target selector).
================
*/
func areaDistance(center, at simulation.Spawn) float64 {
	return math.Hypot(simulation.WorldDistance2D(center, at), at.Y-center.Y)
}

/*
================
areaPopulation

The monster population an area around primary selects in: the primary
monster's own, else the caster's.
================
*/
func (rt *Runtime) areaPopulation(division string, c *enterworld.Character, primary combatTarget) (instance.Lease, bool) {
	if primary.monster != nil && rt.Monsters != nil {
		return rt.Monsters.ObjectPopulation(division, primary.gid)
	}
	return rt.casterPopulation(division, c)
}

/*
================
casterPopulation

The monster population of the caster's world: its admitted session's, else
the world's own lease (a caster admitted before population sessions).
================
*/
func (rt *Runtime) casterPopulation(division string, c *enterworld.Character) (instance.Lease, bool) {
	if lease, ok := rt.EntryPopulationLease(division, c.Name); ok {
		return lease, true
	}
	if rt.Monsters == nil {
		return instance.Lease{}, false
	}
	return rt.Monsters.PopulationLease(division, instance.ID(domain.CharacterWorldInstance(c)))
}
