/*
===========================================================================

thiefcargo.go - a Thief's haul: to the Den, and sold there (port-only)

A Thief that loaded stolen goods onto its transport is never moved to its
leader (convoy.go) and scrolls to the Thief Den with them (the world's
wiring). An unhired Thief sells at once and goes about its business. A
hired one holds the goods in the Den until its hirer comes there, then
sells them to the Den's buyer for the hirer and follows it again. A hire
that ends while the goods wait lets the Thief sell them for itself.

===========================================================================
*/
package caravanai

import "strings"

/*
================
NoteThiefCargo

The world's report each step: whether the Thief's transport holds goods.
================
*/
func (r *Runtime) NoteThiefCargo(division, name string, loaded bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if u := r.findAgentLocked(strings.TrimSpace(division), name); u != nil && u.Role == RoleThief {
		u.carriesGoods = loaded
	}
}

/*
================
ThiefLandedInDen

The Thief's return scroll brought it and its haul to the Den at `at`.
True when it sells now (no hirer); false when a hired Thief holds the
goods for its hirer (ThiefDenSale settles them later).
================
*/
func (r *Runtime) ThiefLandedInDen(division, name string, at Waypoint, nowMs int64) bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	u := r.findAgentLocked(strings.TrimSpace(division), name)
	if u == nil || u.Role != RoleThief || u.HiredBy == "" {
		return true
	}
	u.place(at, nowMs)
	u.denCargo, u.lootGID, u.catchingUp = true, 0, false
	u.Route, u.WaypointIndex = nil, 0
	return false
}

/*
================
ThiefDenSale

Whether a Thief holding its haul in the Den sells now, and for whom: its
hirer once the hirer stands in the Den, or no one (sold for itself) once
its hire ended. Selling clears the hold; the caller sells and pays.
================
*/
func (r *Runtime) ThiefDenSale(division, name string) (hirer string, sell bool) {
	division = strings.TrimSpace(division)
	r.mu.Lock()
	u := r.findAgentLocked(division, name)
	if u == nil || !u.denCargo {
		r.mu.Unlock()
		return "", false
	}
	hirer = u.HiredBy
	r.mu.Unlock()
	if hirer != "" {
		if r.hooks.Player == nil {
			return "", false
		}
		p, ok := r.hooks.Player(division, hirer)
		if !ok {
			return "", false
		}
		r.mu.Lock()
		place, inPlace := r.placeAtLocked(p.At)
		r.mu.Unlock()
		if !inPlace || place != TownThiefDen {
			return "", false
		}
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if u = r.findAgentLocked(division, name); u == nil || !u.denCargo {
		return "", false
	}
	u.denCargo, u.carriesGoods = false, false
	return hirer, true
}
