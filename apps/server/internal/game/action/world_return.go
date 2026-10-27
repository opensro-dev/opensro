/*
===========================================================================

world_return.go - live region tracking for saved return locations

Updates each admitted player's bound region on periodic ticks and movement
segment transitions, saving outdoor layer-1 return locations under the
character store door.

===========================================================================
*/

package action

import (
	"sort"

	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/instance"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
advanceResidentRegions

advanceResidentRegions is the population owner's live-region update, not
an AOI query. Sample the interpolated position under the character store
door. Neither a future goal nor a presentation snapshot can save a
return location. Natively the movement step makes that call after assigning the live pose.
================
*/
func (rt *Runtime) advanceResidentRegions(nowMs int64) {
	var keys []string
	rt.characterAdmissions.Range(func(key, _ any) bool {
		keys = append(keys, key.(string))
		return true
	})
	sort.Strings(keys)
	for _, key := range keys {
		value, exists := rt.characterAdmissions.Load(key)
		if !exists {
			continue
		}
		owner := value.(populationAdmission)
		rt.bindResidentRegionInDivision(owner.division, key, nowMs)
	}
}

/*
================
bindResidentRegionInDivision
================
*/
func (rt *Runtime) bindResidentRegionInDivision(division, key string, nowMs int64) {
	unlock := rt.lockDivision(division)
	defer unlock()
	rt.bindResidentRegion(key, nowMs)
}

/*
================
AdvanceResidentRegion

AdvanceResidentRegion settles the old segment's region before a movement
command replaces that segment. A crossing and resteer between two periodic
updates must not erase the region-admission event.
================
*/
func (rt *Runtime) AdvanceResidentRegion(division, name string, nowMs int64) {
	unlock := rt.lockDivision(division)
	defer unlock()
	rt.bindResidentRegion(simulation.WorldKey(division, name), nowMs)
}

/*
================
bindResidentRegion

Caller holds the division operation door. Revalidation prevents a departed
actor from updating a recycled layer's return state.
================
*/
func (rt *Runtime) bindResidentRegion(key string, nowMs int64) {
	value, exists := rt.characterAdmissions.Load(key)
	if !exists {
		return
	}
	owner := value.(populationAdmission)
	lease, live := rt.CharacterPopulationLease(owner.division, owner.name, owner.session)
	if !live || lease != owner.lease {
		return
	}
	c := rt.findCharacter(owner.division, owner.name)
	if c == nil {
		return
	}
	var pose simulation.Spawn
	var inWorld bool
	rt.deps.Read(owner.division, func() {
		inWorld = instance.ID(domain.CharacterWorldInstance(c)) == lease.ID
		if inWorld {
			pose = rt.liveSpawn(key, c, nowMs)
		}
	})
	if !inWorld {
		return
	}
	if owner.regionBound && owner.region == pose.RegionID {
		return
	}
	definition, exists := instance.Lookup(lease.ID.Definition())
	if !exists {
		return
	}
	if definition.NativeType == 0 && lease.ID.Layer() == 1 {
		saved := domain.SavedReturnLocation{Definition: uint16(definition.ID), RegionID: pose.RegionID,
			X: float32(pose.X), Y: float32(pose.Y), Z: float32(pose.Z)}
		if !rt.deps.Update(c, "region-return-location", func() bool {
			if instance.ID(domain.CharacterWorldInstance(c)) != lease.ID {
				return false
			}
			if c.World == nil {
				c.World = &domain.CharacterWorld{}
			}
			c.World.SavedReturn = &saved
			return true
		}) {
			return
		}
	}
	owner.region, owner.regionBound = pose.RegionID, true
	rt.characterAdmissions.Store(key, owner)
}
