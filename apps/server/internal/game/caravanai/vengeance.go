/*
===========================================================================

vengeance.go - a killed Thief or Hunter comes back with friends (port-only)

A Thief killed near a Trader respawns in the Thief Den, calls up to three
idle Thieves there and leads them back after that Trader. A Hunter killed
by a Thief respawns in town, calls up to three idle Hunters of that town
and leads them back after that Thief. The gang rides to the quarry while
it can be found outside a town, else to where its friend fell; once they
are within sight the brain's usual combat takes over (Thieves attack
Traders and Hunters on sight, Hunters attack Thieves). The hunt ends when
the quarry is robbed or killed or reaches a town, when the gang finds
nothing, or after VengeanceMs.

===========================================================================
*/
package caravanai

import (
	"fmt"
	"strings"
)

const (
	RunVengeance RunKind = "vengeance"

	VengeanceMs    int64   = 15 * 60_000
	VengeanceGang          = 3
	vengeanceReach float64 = 300 * UnitsPerMeter
	// vengeanceArrive is how near the gang must come to the quarry (or the
	// spot it was last seen) before the fight takes over.
	vengeanceArrive float64 = 15 * UnitsPerMeter
)

// quarryRole is the role a hunt of role goes after.
func quarryRole(role AgentRole) AgentRole {
	if role == RoleHunter {
		return RoleThief
	}
	return RoleTrader
}

/*
================
vengeanceTargetLocked

Who an agent of role killed by attacker at fell hunts. A Thief: the
attacker if it is a Trader agent, the Trader (or hirer) an agent attacker
guarded, the attacking player (judged a Trader when the hunt starts), or
else the nearest Trader agent within 300m. A Hunter: its killer, a Thief
agent or a player (judged a Thief when the hunt starts).
================
*/
func (r *Runtime) vengeanceTargetLocked(division string, role AgentRole, attacker string, fell Waypoint) string {
	a := r.findAgentLocked(division, attacker)
	if role == RoleHunter {
		if a == nil || a.Role == RoleThief {
			return attacker
		}
		return ""
	}
	if a != nil {
		if a.Role == RoleTrader {
			return a.Name
		}
		if a.EscortingTrader != "" {
			return a.EscortingTrader
		}
		if a.HiredBy != "" {
			return a.HiredBy
		}
	} else if attacker != "" {
		return attacker
	}
	best, bestDist := "", vengeanceReach
	for _, u := range r.agents[division] {
		if u.Role != RoleTrader || u.RespawnPending || u.HP <= 0 {
			continue
		}
		if d := WaypointDistance(position(u), fell); d <= bestDist {
			best, bestDist = u.Name, d
		}
	}
	return best
}

/*
================
startVengeanceLocked

Called on a Thief's or Hunter's defeat, after it respawned at home. One
already hunting, on hire, or bound to a caravan does not start another
hunt; a hunt's leader killed again sends its gang home.
================
*/
func (r *Runtime) startVengeanceLocked(unit *AgentUnit, attacker string, fell Waypoint, nowMs int64) {
	if unit.RunKind == RunVengeance {
		r.endVengeanceLocked(unit, nowMs, "")
		return
	}
	if unit.HiredBy != "" || unit.EscortingTrader != "" || (unit.Role != RoleThief && unit.Role != RoleHunter) {
		return
	}
	target := r.vengeanceTargetLocked(unit.Division, unit.Role, attacker, fell)
	if target == "" {
		return
	}
	unit.Vengeance, unit.vengeanceAt, unit.vengeanceUntilMs = target, fell, nowMs+VengeanceMs
	unit.RunKind, unit.State, unit.AvailableForHire = RunVengeance, StateTraveling, false
	unit.Route, unit.WaypointIndex, unit.TownDepartAtMs = nil, 0, 0
	home := r.homeLocked(unit)
	gang := 0
	for _, u := range r.agents[unit.Division] {
		if gang == VengeanceGang {
			break
		}
		// Friends of the same kind idling in the same home answer.
		if u == unit || u.Role != unit.Role || !hireable(u) || r.homeLocked(u) != home {
			continue
		}
		u.EscortingTrader, u.RunKind, u.State, u.AvailableForHire = unit.Name, RunVengeance, StateTraveling, false
		u.OriginTown, u.DestTown, u.Route, u.WaypointIndex = home, home, nil, 0
		gang++
	}
	r.outbox = append(r.outbox, outboxItem{chat: true, agent: unit.Name, regionID: unit.RegionID, text: vengeanceCall(unit.Role, DisplayName(target), gang)})
}

func vengeanceCall(role AgentRole, who string, gang int) string {
	switch {
	case role == RoleHunter && gang == 0:
		return fmt.Sprintf("Nobody free to ride with me? Then I hunt %s alone.", who)
	case role == RoleHunter:
		return fmt.Sprintf("Hunters, with me! %s cut me down; we ride back and finish this.", who)
	case gang == 0:
		return fmt.Sprintf("Nobody to ride with? Then I go alone. %s will pay.", who)
	default:
		return fmt.Sprintf("%s cut me down. Brothers, with me: we ride back for that caravan!", who)
	}
}

/*
================
stepVengeanceLocked

The gang leader rides to its quarry: to its live position while it can be
found outside a town, else to the spot its friend fell.
================
*/
func (r *Runtime) stepVengeanceLocked(unit *AgentUnit, nowMs int64) {
	if nowMs >= unit.vengeanceUntilMs {
		r.endVengeanceLocked(unit, nowMs, "The trail's gone cold. Back home.")
		return
	}
	quarry := quarryRole(unit.Role)
	target, alive, lost := unit.vengeanceAt, false, false
	if t := r.findAgentLocked(unit.Division, unit.Vengeance); t != nil {
		switch {
		case t.RespawnPending || t.HP <= 0 || (quarry == RoleTrader && t.CargoStacks == 0):
			lost = true
		case isTownRegion(t.RegionID):
			lost = true
		default:
			target, alive = position(t), true
		}
	} else if r.hooks.Player != nil {
		if p, ok := r.hooks.Player(unit.Division, unit.Vengeance); ok {
			_, inTown := r.placeAtLocked(p.At)
			if p.JobType != uint8(quarry) || inTown || p.HPPercent <= 0 {
				lost = true
			} else {
				target, alive = p.At, true
			}
		}
	}
	if lost {
		r.endVengeanceLocked(unit, nowMs, "Gone where we can't follow. Next time.")
		return
	}
	unit.vengeanceAt = target
	if WaypointDistance(position(unit), target) <= vengeanceArrive {
		if !alive {
			r.endVengeanceLocked(unit, nowMs, "Nothing here but tracks. Back home.")
		}
		// Within sight of the quarry the brain's combat takes over.
		return
	}
	r.moveLocked(unit, target, stepDistanceFor(unit))
}

/*
================
endVengeanceLocked

The leader and its gang go home.
================
*/
func (r *Runtime) endVengeanceLocked(leader *AgentUnit, nowMs int64, line string) {
	for _, u := range r.agents[leader.Division] {
		if u != leader && u.RunKind == RunVengeance && strings.EqualFold(u.EscortingTrader, leader.Name) {
			r.releaseLocked(u, nowMs, true)
		}
	}
	leader.Vengeance, leader.vengeanceUntilMs = "", 0
	r.releaseLocked(leader, nowMs, true)
	if line != "" {
		r.outbox = append(r.outbox, outboxItem{chat: true, agent: leader.Name, regionID: leader.RegionID, text: line})
	}
}
