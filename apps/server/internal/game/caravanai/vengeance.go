/*
===========================================================================

vengeance.go - a killed Thief comes back for the Trader (port-only)

A Thief killed near a Trader respawns in the Thief Den, calls up to three
idle Thieves there and leads them back after that Trader: to it while it
can be found, else to where the Thief fell. Once they are within sight the
brain's usual ambush takes over (Thieves attack Traders and Hunters on
sight). The hunt ends when the Trader is robbed or reaches a town, when the
gang finds nothing, or after VengeanceMs.

===========================================================================
*/
package caravanai

import (
	"fmt"
	"strings"

	"opensro.online/server/internal/domain"
)

const (
	RunVengeance RunKind = "vengeance"

	VengeanceMs    int64   = 15 * 60_000
	VengeanceGang          = 3
	vengeanceReach float64 = 300 * UnitsPerMeter
	// vengeanceArrive is how near the gang must come to the Trader (or the
	// spot it was last seen) before the ambush takes over.
	vengeanceArrive float64 = 15 * UnitsPerMeter
)

/*
================
vengeanceTargetLocked

The Trader a Thief killed by attacker at fell should hunt: the attacker if
it is a Trader agent, the Trader (or hirer) an agent attacker was guarding,
the attacking player (judged a Trader when the hunt starts), or else the
nearest Trader agent within 300m.
================
*/
func (r *Runtime) vengeanceTargetLocked(division, attacker string, fell Waypoint) string {
	if a := r.findAgentLocked(division, attacker); a != nil {
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

Called on a Thief's defeat, after it respawned in the Den. A Thief already
hunting, or one on hire, does not start another hunt; its gang goes home.
================
*/
func (r *Runtime) startVengeanceLocked(thief *AgentUnit, attacker string, fell Waypoint, nowMs int64) {
	if thief.RunKind == RunVengeance {
		r.endVengeanceLocked(thief, nowMs, "")
		return
	}
	if thief.HiredBy != "" {
		return
	}
	target := r.vengeanceTargetLocked(thief.Division, attacker, fell)
	if target == "" {
		return
	}
	thief.Vengeance, thief.vengeanceAt, thief.vengeanceUntilMs = target, fell, nowMs+VengeanceMs
	thief.RunKind, thief.State, thief.AvailableForHire = RunVengeance, StateTraveling, false
	thief.Route, thief.WaypointIndex, thief.TownDepartAtMs = nil, 0, 0
	gang := 0
	for _, u := range r.agents[thief.Division] {
		if gang == VengeanceGang {
			break
		}
		if u == thief || u.Role != RoleThief || !hireable(u) {
			continue
		}
		u.EscortingTrader, u.RunKind, u.State, u.AvailableForHire = thief.Name, RunVengeance, StateTraveling, false
		u.OriginTown, u.DestTown, u.Route, u.WaypointIndex = TownThiefDen, TownThiefDen, nil, 0
		gang++
	}
	who := DisplayName(target)
	text := fmt.Sprintf("%s cut me down. Brothers, with me: we ride back for that caravan!", who)
	if gang == 0 {
		text = fmt.Sprintf("Nobody to ride with? Then I go alone. %s will pay.", who)
	}
	r.outbox = append(r.outbox, outboxItem{chat: true, agent: thief.Name, regionID: thief.RegionID, text: text})
}

/*
================
stepVengeanceLocked

The gang leader rides to its Trader: to its live position while it can be
found outside a town, else to the spot the Thief fell.
================
*/
func (r *Runtime) stepVengeanceLocked(unit *AgentUnit, nowMs int64) {
	if nowMs >= unit.vengeanceUntilMs {
		r.endVengeanceLocked(unit, nowMs, "The trail's gone cold. Back to the Den.")
		return
	}
	target, alive, lost := unit.vengeanceAt, false, false
	if t := r.findAgentLocked(unit.Division, unit.Vengeance); t != nil {
		switch {
		case t.RespawnPending || t.HP <= 0 || t.CargoStacks == 0:
			lost = true
		case isTownRegion(t.RegionID):
			lost = true
		default:
			target, alive = position(t), true
		}
	} else if r.hooks.Player != nil {
		if p, ok := r.hooks.Player(unit.Division, unit.Vengeance); ok {
			_, inTown := r.placeAtLocked(p.At)
			if p.JobType != domain.JobTrader || inTown || p.HPPercent <= 0 {
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
			r.endVengeanceLocked(unit, nowMs, "Nothing here but tracks. Back to the Den.")
		}
		// Within sight of a live Trader the ambush (brain) takes over.
		return
	}
	r.moveLocked(unit, target, stepDistanceFor(unit))
}

/*
================
endVengeanceLocked

The leader and its gang go home to the Den.
================
*/
func (r *Runtime) endVengeanceLocked(leader *AgentUnit, nowMs int64, line string) {
	for _, u := range r.agents[leader.Division] {
		if u != leader && u.RunKind == RunVengeance && strings.EqualFold(u.EscortingTrader, leader.Name) {
			u.RunKind = RunNone
			r.releaseLocked(u, nowMs, true)
		}
	}
	leader.Vengeance, leader.vengeanceUntilMs, leader.RunKind = "", 0, RunNone
	r.releaseLocked(leader, nowMs, true)
	if line != "" {
		r.outbox = append(r.outbox, outboxItem{chat: true, agent: leader.Name, regionID: leader.RegionID, text: line})
	}
}
