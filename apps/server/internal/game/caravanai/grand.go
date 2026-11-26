/*
===========================================================================

grand.go - the hourly grand caravan (port-only)

Two five-star Traders and six Hunters muster at one shop, then ride out
together; StartGrandCaravan lets a GM start one now.

===========================================================================
*/
package caravanai

import (
	"fmt"
	"math"
	"sort"
	"strings"
)

/*
================
grandCaravanLocked

Every hour two Traders and six Hunters muster and leave together.
The muster starts two minutes before the first scheduled departure.
================
*/
func (r *Runtime) grandCaravanLocked(division string, nowMs int64) {
	g := r.grand[division]
	if g == nil {
		g = &grandCaravan{nextAtMs: nowMs + GrandCaravanFirstMs}
		r.grand[division] = g
	}
	if g.departAtMs != 0 {
		if nowMs >= g.departAtMs {
			g.departAtMs = 0
			r.departGrandLocked(division, g.town, nowMs)
		}
		return
	}
	if nowMs < g.nextAtMs {
		return
	}
	if r.musterGrandLocked(division, g, nowMs) {
		g.nextAtMs = nowMs + GrandCaravanIntervalMs
	} else {
		g.nextAtMs = nowMs + grandCaravanRetryMs
	}
}

/*
================
StartGrandCaravan

Starts the grand caravan muster now (the next one follows the interval).
================
*/
func (r *Runtime) StartGrandCaravan(division string, nowMs int64) bool {
	r.EnsureDivisionRoster(division)
	r.mu.Lock()
	defer r.mu.Unlock()
	g := r.grand[division]
	if g == nil {
		g = &grandCaravan{nextAtMs: nowMs + GrandCaravanFirstMs}
		r.grand[division] = g
	}
	if g.departAtMs != 0 || !r.musterGrandLocked(division, g, nowMs) {
		return false
	}
	g.nextAtMs = nowMs + GrandCaravanIntervalMs
	return true
}

func (r *Runtime) musterGrandLocked(division string, g *grandCaravan, nowMs int64) bool {
	var traders, hunters []*AgentUnit
	towns := map[TownID]int{}
	for _, u := range r.agents[division] {
		if u.RunKind == RunGrand {
			return false
		}
		if !hireable(u) {
			continue
		}
		switch {
		case u.Role == RoleTrader && nowMs >= u.CooldownUntilMs:
			traders = append(traders, u)
		case u.Role == RoleHunter:
			hunters = append(hunters, u)
		default:
			continue
		}
		towns[u.OriginTown]++
	}
	if len(traders) < grandCaravanTraders || len(hunters) < grandCaravanHunters {
		return false
	}
	g.town = TownJangan
	for _, town := range rosterTowns {
		if towns[town] > towns[g.town] {
			g.town = town
		}
	}
	g.departAtMs = nowMs + GrandCaravanMusterMs
	shop := r.terminalLocked(g.town)
	for _, group := range [][]*AgentUnit{traders, hunters} {
		sort.SliceStable(group, func(i, j int) bool {
			return WaypointDistance(position(group[i]), shop) < WaypointDistance(position(group[j]), shop)
		})
	}
	members := append(traders[:grandCaravanTraders], hunters[:grandCaravanHunters]...)
	for i, u := range members {
		u.RunKind, u.State, u.AvailableForHire = RunGrand, StateRecruiting, false
		u.OriginTown, u.DestTown = g.town, nextTown(g.town)
		u.TownDepartAtMs = 0
		if WaypointDistance(position(u), shop) > townWanderRadius*2 {
			angle := float64(i) * 2 * math.Pi / float64(len(members))
			tp := offsetPoint(shop, 60*math.Cos(angle), 60*math.Sin(angle))
			u.pendingTeleport = &tp
		}
	}
	r.outbox = append(r.outbox, outboxItem{chat: true, agent: members[0].Name, regionID: shop.RegionID,
		text: fmt.Sprintf("Grand caravan! %d Traders and %d Hunters muster in %s and leave for %s in %d minutes.",
			grandCaravanTraders, grandCaravanHunters, townTitle(g.town), townTitle(nextTown(g.town)), GrandCaravanMusterMs/60_000)})
	return true
}

func (r *Runtime) departGrandLocked(division string, town TownID, nowMs int64) {
	var traders, guards []*AgentUnit
	for _, u := range r.agents[division] {
		if u.RunKind != RunGrand || u.HiredBy != "" || u.State != StateRecruiting {
			continue
		}
		if u.IsDead(nowMs) || u.RespawnPending {
			r.releaseLocked(u, nowMs, false)
			continue
		}
		if u.Role == RoleTrader {
			traders = append(traders, u)
		} else {
			guards = append(guards, u)
		}
	}
	if len(traders) != grandCaravanTraders || len(guards) != grandCaravanHunters {
		for _, u := range append(traders, guards...) {
			r.releaseLocked(u, nowMs, false)
		}
		r.grand[division].nextAtMs = nowMs + grandCaravanRetryMs
		return
	}
	lead := traders[0]
	lead.OriginTown, lead.DestTown = town, nextTown(town)
	r.dispatchTraderLocked(lead, RunGrand, nil, nowMs)
	for _, u := range traders[1:] {
		r.assignEscortLocked(u, lead, RunGrand)
		u.CargoStacks, u.CargoStars = 10, grandCaravanCargoStars
	}
	for i, u := range guards {
		r.assignEscortLocked(u, traders[i%len(traders)], RunGrand)
	}
}

func townTitle(town TownID) string {
	if town == "" {
		return ""
	}
	return strings.ToUpper(string(town[:1])) + string(town[1:])
}
