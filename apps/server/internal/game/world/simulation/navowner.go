/*
===========================================================================

navowner.go - shared accepted movement state and lifecycle

===========================================================================
*/
package simulation

import worldgeom "opensro.online/server/internal/game/world"

// Surface ownership: WHICH walkable surface a position stands on.
//
// Native evidence (v1.188 SR_GameServer, HLIL-verified 2026-09-23):
//   - Every positioned object carries a tagNavPos at CGObj+0x7C:
//     {pNavCell, pNavMeshInst, wRegionID, x, y, z}. The cell is part of the
//     position, not something recomputed from it.
//   - CGObj_MoveTo (0x485740) walks every ordinary move through
//     IRegionManager slot +0x30, CRegionManagerBody_QueryMovement (0x98B300).
//     The walk starts from the STORED source cell (`*(source.pNavCell+4)` is
//     the mesh that moves) and writes the reached cell back into the
//     destination, which CGObj_StepMovement then commits.
//   - Only move mode 7 (teleport) resolves a cell afresh, through slot +0x2C
//     CRegionManagerBody_CheckPointValid (0x98B1D0) ->
//     CRTNavMeshTerrain_FindNavCell (0x99FD90): nearest |deltaY| between the
//     terrain and the objects registered in that terrain cell, strict `<` so
//     terrain wins ties, and y is rewritten to the chosen surface's height.
//   - Walk results carry a surface height, never the requested one: the object
//     walker (0x9B5A80) ends in the cell-plane height store 0x9C0350.
//
// Why this matters: movement packets carry int16 coordinates, so a y of 243.99
// on a gate deck arrives as 243. Re-guessing the surface from that y on every
// move (instead of retaining the cell) put players on the terrain under the
// Hotan gate deck (terrain 243.04 is closer to 243 than the deck's 243.99)
// and stopped them at an invisible cliff. Keep the owner; never re-guess it.

// The value types live in the leaf world package so monsters carry the same
// identity (world/navowner.go); these aliases keep simulation's API.
type (
	NavOwnerKind  = worldgeom.NavOwnerKind
	NavObjectCell = worldgeom.NavObjectCell
	NavOwner      = worldgeom.NavOwner
	NavOwnerSpan  = worldgeom.NavOwnerSpan
)

const (
	NavOwnerUnresolved = worldgeom.NavOwnerUnresolved
	NavOwnerTerrain    = worldgeom.NavOwnerTerrain
	NavOwnerObject     = worldgeom.NavOwnerObject
)

/*
================
TerrainOwner

TerrainOwner is the terrain surface owner.
================
*/
func TerrainOwner() NavOwner { return worldgeom.TerrainOwner() }

/*
================
NavWalk

NavWalk is what the movement authority reports for one constrained move:
the owner spans along the committed chord and the owner at its end.
================
*/
type NavWalk struct {
	Spans []NavOwnerSpan
	Rest  NavOwner
}

/*
================
NavOwnerTrack

NavOwnerTrack is the walked ownership of one segment chord toward To (the
goal it was resolved for). Treat as immutable once attached.
================
*/
type NavOwnerTrack struct {
	Spans []NavOwnerSpan
	To    Spawn
}

/*
================
samePosition

samePosition compares the placement a retained owner was resolved for.
Facing is not part of a nav position.
================
*/
func samePosition(a, b Spawn) bool {
	return a.RegionID == b.RegionID && a.X == b.X && a.Y == b.Y && a.Z == b.Z
}

/*
================
GoalOwner

GoalOwner is the retained owner of the settled/goal position. It is valid
only while Spawn is exactly the position it was resolved for: every writer
that relocates a character without walking (warp, portal, return, rebirth,
pet follow) therefore falls back to the native teleport rule automatically,
instead of carrying a stale cell.
================
*/
func (w WorldState) GoalOwner() NavOwner {
	if w.Nav.Resolved() && samePosition(w.NavAt, w.Spawn) {
		return w.Nav
	}
	return NavOwner{}
}

/*
================
SetGoalOwner

SetGoalOwner retains owner as the surface of the current Spawn.
================
*/
func (w *WorldState) SetGoalOwner(owner NavOwner) {
	w.Nav = owner
	w.NavAt = w.Spawn
}

/*
================
LiveOwnerAt

LiveOwnerAt is the owner of LiveSpawnAt(nowMs): the walked span covering the
in-flight segment's fraction, or the goal owner once settled.
================
*/
func (w WorldState) LiveOwnerAt(nowMs int64) NavOwner {
	if w.groundActive() {
		return w.Ground.owner
	}
	segment := w.MoveSegment
	if !segment.Valid() || nowMs >= segment.ArrivesAtMs {
		return w.GoalOwner()
	}
	track := segment.Owners
	if track == nil || !samePosition(track.To, w.Spawn) {
		return NavOwner{}
	}
	t := float64(nowMs-segment.StartedAtMs) / float64(segment.ArrivesAtMs-segment.StartedAtMs)
	if t < 0 {
		t = 0
	}
	return worldgeom.OwnerAtFraction(track.Spans, t)
}

/*
================
WithOwners

WithOwners returns a copy of the (immutable) segment carrying the walked
owner spans of its chord toward goal. Nil stays nil.
================
*/
func (s *MoveSegment) WithOwners(spans []NavOwnerSpan, goal Spawn) *MoveSegment {
	if s == nil {
		return nil
	}
	next := *s
	next.Owners = &NavOwnerTrack{Spans: append([]NavOwnerSpan(nil), spans...), To: goal}
	return &next
}

/*
================
SettleLive

SettleLive stops any in-flight travel at the live point and keeps the
surface owner the walk had there. Every "stop where you stand" transition
(death, turn in place, sit, stop, entering attack range) must settle through
this so the stopped position keeps its native cell.
================
*/
func (w *WorldState) SettleLive(nowMs int64) {
	owner := w.LiveOwnerAt(nowMs)
	w.Spawn = w.LiveSpawnAt(nowMs)
	w.MoveSegment = nil
	w.Ground = nil
	w.SetGoalOwner(owner)
}

/*
================
CommitWalk

CommitWalk records the surface a walked move reached: the goal owner and
the owner spans of the in-flight segment. Call it right after the Spawn and
MoveSegment writes of an ordinary (QueryMovement-equivalent) move.
================
*/
func (w *WorldState) CommitWalk(spans []NavOwnerSpan, goal NavOwner) {
	w.MoveSegment = w.MoveSegment.WithOwners(spans, w.Spawn)
	w.SetGoalOwner(goal)
}
