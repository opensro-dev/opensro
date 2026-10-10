/*
===========================================================================

areacandidates.go - every object an offensive area may strike

The native selectors (TargetSelection_DispatchByShape and its shape
owners, 58A088..58BE17) walk every character object around their centre
and keep the ones SkillCombat_ValidateTargets (58CC70) admits: living
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
	// selects is the efr +0x14 mask (TargetSelection_AroundSource 58A020):
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
	for _, present := range rt.PopulationPlayers(q.division, q.now) {
		if present.World != world || present.GID == enterworld.ObjectIDForCharacter(q.caster) ||
			simulation.IsDungeonRegion(present.Spawn.RegionID) != simulation.IsDungeonRegion(q.center.RegionID) {
			continue
		}
		distance := areaDistance(q.center, present.Spawn)
		player := rt.findCharacterByGid(q.division, present.GID)
		if player == nil {
			continue
		}
		snapshot := rt.characterSnapshot(q.division, player)
		if snapshot == nil || snapshot.DeletePending || !enterworld.CharacterAlive(snapshot) {
			continue
		}
		radius, ok := rt.deps.CharacterBodyRadius(snapshot)
		if !ok {
			continue
		}
		limit := q.reach
		if !q.nearest {
			limit += radius
		}
		if distance > limit {
			continue
		}
		// 5A1AD0 asks 5293A0 with mode 3 for a player caster.
		if rt.hostilePlayerRefusalWith(q.division, q.caster, snapshot, q.skill, q.now, playerAttackArea) != 0 {
			continue
		}
		out = append(out, areaCandidate{target: combatTarget{gid: present.GID, player: player, snapshot: snapshot,
			at: present.Spawn}, distance: distance, radius: radius})
	}
	return out
}

/*
================
areaDistance

The monster registry's candidate distance: planar world distance, then
the height difference (58ab6d..58ac23).
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
