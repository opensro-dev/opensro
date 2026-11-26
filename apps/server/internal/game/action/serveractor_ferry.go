/*
===========================================================================

serveractor_ferry.go - port-only AI travel through shipped ferry links

Only transient actors may use this lane. Source positions, destinations,
fares and admission conditions come from the same catalogue as player travel.
There is no scene reload for an actor that has no client session.

===========================================================================
*/
package action

import (
	"opensro.online/server/internal/domain"
	"opensro.online/server/internal/game/world/simulation"
	"strings"
)

/*
================
ServerActorFerryRoute
================
*/
func (rt *Runtime) ServerActorFerryRoute(from, to string) (simulation.NpcDef, uint32, simulation.Spawn, bool) {
	if rt.portals == nil {
		return simulation.NpcDef{}, 0, simulation.Spawn{}, false
	}
	for _, source := range rt.portals.destinations {
		if source.code != from || !strings.Contains(from, "FERRY") {
			continue
		}
		for _, target := range rt.portals.destinations {
			if target.code != to || !strings.Contains(to, "FERRY") {
				continue
			}
			if _, ok := rt.portals.links[[2]uint32{source.id, target.id}]; !ok {
				continue
			}
			for _, npc := range rt.NpcRoster {
				if npc.RefObjID == source.ref {
					return npc, target.id, target.spawn, true
				}
			}
		}
	}
	return simulation.NpcDef{}, 0, simulation.Spawn{}, false
}

/*
================
CrossServerActorFerry
================
*/
func (rt *Runtime) CrossServerActorFerry(division, name string, sourceRef, targetID uint32) (simulation.Spawn, bool) {
	unlock := rt.lockDivision(division)
	defer unlock()
	c := rt.findCharacter(division, name)
	if c == nil || !c.RuntimeOnly || rt.portals == nil {
		return simulation.Spawn{}, false
	}
	sourceID, ok := rt.portals.sources[sourceRef]
	if !ok {
		return simulation.Spawn{}, false
	}
	source, target := rt.portals.destinations[sourceID], rt.portals.destinations[targetID]
	if !strings.Contains(source.code, "FERRY") || !strings.Contains(target.code, "FERRY") {
		return simulation.Spawn{}, false
	}
	link, ok := rt.portals.links[[2]uint32{sourceID, targetID}]
	if !ok {
		return simulation.Spawn{}, false
	}
	at := rt.liveSpawn(simulation.WorldKey(division, name), c, rt.Now().UnixMilli())
	near := false
	for _, npc := range rt.NpcRoster {
		if npc.RefObjID == sourceRef && simulation.WorldDistance2D(at, npc.Spawn) <= 300 {
			near = true
			break
		}
	}
	if !near {
		return simulation.Spawn{}, false
	}
	admitted := rt.deps.UpdateDeferred(c, "portal-travel", func() bool {
		if domain.CharacterWorldInstance(c) != domain.DefaultWorldInstance {
			return false
		}
		if portalAdmission(c, source, link, portalTraveller{transportCOS: c.ActiveCOS != nil, transportAdmitted: rt.JobGoodsTeleport}) != 0 {
			return false
		}
		gold := int64(0)
		if c.Gold != nil {
			gold = *c.Gold
		}
		gold -= link.fee
		c.Gold = &gold
		return true
	})
	if !admitted {
		return simulation.Spawn{}, false
	}
	return target.spawn, true
}
