/*
===========================================================================

uniqueshare.go - one live unique per shared spawn point

Port-only, not native (SRO_STR_INT_UNIQUES, monster/strint_uniques.go). A
species' base unique and its STR and INT copies share the same authored
spawn points, each in a hive of its own so all three can be up at once.
What they may not do is stand on the same point, so the population marks
the point a live unique of the species holds and lets a spawn draw only
among the points nobody holds. On death the point is released.

The mark is per division state, keyed by the species and the authored
centre: every build's nest for a point carries the same centre, so the
copies collide only when they would really share the spot.

===========================================================================
*/
package simulation

import "opensro.online/server/internal/game/world/monster"

/*
================
uniqueAnchor

One shared spawn point of one species: the authored centre the builds have
in common.
================
*/
type uniqueAnchor struct {
	share   string
	region  uint16
	x, y, z float64
}

/*
================
uniqueAnchorOf
================
*/
func uniqueAnchorOf(nest monster.NestRow) uniqueAnchor {
	return uniqueAnchor{share: nest.UniqueShare, region: nest.SpawnPoint.RegionID,
		x: nest.SpawnPoint.X, y: nest.SpawnPoint.Y, z: nest.SpawnPoint.Z}
}

/*
================
anchorHeld

Whether a live unique of the species already stands on this nest's point.
A nest outside a shared species is never held.
================
*/
func (s *MonsterState) anchorHeld(state *divisionMonsterState, nest monster.NestRow) bool {
	if nest.UniqueShare == "" {
		return false
	}
	_, held := state.uniqueAnchors[uniqueAnchorOf(nest)]
	return held
}

/*
================
holdAnchor
================
*/
func (s *MonsterState) holdAnchor(state *divisionMonsterState, nest monster.NestRow, gid uint32) {
	if nest.UniqueShare == "" {
		return
	}
	if state.uniqueAnchors == nil {
		state.uniqueAnchors = map[uniqueAnchor]uint32{}
	}
	state.uniqueAnchors[uniqueAnchorOf(nest)] = gid
}

/*
================
releaseAnchorHeldBy

The point goes back to the species when the instance that holds it leaves
the world - not when its nest is released, because a detached unique stays
alive on its ground (monsternest_attachment.go).
================
*/
func (s *MonsterState) releaseAnchorHeldBy(state *divisionMonsterState, gid uint32) {
	if len(state.uniqueAnchors) == 0 {
		return
	}
	for anchor, holder := range state.uniqueAnchors {
		if holder == gid {
			delete(state.uniqueAnchors, anchor)
		}
	}
}

/*
================
hiveMembers

The members an overwrite hive should try this tick. A shared hive draws
among the points no other copy holds; every other hive keeps the native
selected location. Port-only, not native (SRO_STR_INT_UNIQUES).
================
*/
func (s *MonsterState) hiveMembers(state *divisionMonsterState, members []int, selected int, nowMs int64) []int {
	if s.template.Nests[members[0]].UniqueShare == "" {
		if selected >= 0 {
			return []int{selected}
		}
		return members
	}
	if pick, ok := s.sharedHivePick(state, members, nowMs); ok {
		return []int{pick}
	}
	return nil
}

/*
================
sharedHivePick

The member a shared hive should spawn at: a draw among the points whose
timer has elapsed and that no other copy holds. The native draw picks the
next location when a single-occupant hive loses a member; here the choice
must also avoid ground another build stands on, and must not stall on a
fixed point a neighbour took.
================
*/
func (s *MonsterState) sharedHivePick(state *divisionMonsterState, members []int, nowMs int64) (int, bool) {
	free := make([]int, 0, len(members))
	for _, index := range members {
		n := state.nests[index]
		if n == nil || !n.elapsed(nowMs) || n.live >= s.template.Nests[index].InstanceLimit() {
			continue
		}
		if s.anchorHeld(state, s.template.Nests[index]) {
			continue
		}
		free = append(free, index)
	}
	if len(free) == 0 {
		return 0, false
	}
	return free[monster.HiveRespawnSelection(len(free), s.randomWord)], true
}
