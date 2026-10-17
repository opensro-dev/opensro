/*
===========================================================================

navmesh.go - the route planner over the server navigation geometry

The bot reuses the server's authoritative navmesh. The chord predicate is the
same ClipMovementPath the server applies to a 0x7738 player move, so a planned
leg arrives instead of clipping at an obstacle. The classifier
ValidateMovementPath is optimistic on a chord that crosses a blocked tile.

Phase 4 of issue #74 (issue #98).

===========================================================================
*/
package main

import (
	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
)

// chordClipper clips a straight chord the way the server clips a player move.
type chordClipper interface {
	ClipMovementPath(from, to simulation.Spawn) movement.ClipReport
}

// authorityChordValidator reports whether a straight chord is a legal player
// move. It is the same ClipMovementPath the server applies to a player move, so
// a planned leg arrives instead of clipping at an obstacle.
type authorityChordValidator struct {
	clipper chordClipper
}

func (a authorityChordValidator) Walkable(from, to bot.Point) bool {
	return a.clipper.ClipMovementPath(spawnOfPoint(from), spawnOfPoint(to)).Outcome == movement.ClipArrived
}

func spawnOfPoint(p bot.Point) simulation.Spawn {
	return simulation.Spawn{RegionID: p.Region, X: p.X, Y: p.Y, Z: p.Z}
}

// newNavPathfinder builds the route planner over the world-authority geometry.
// An empty directory returns nil, so the brain keeps the straight chord.
func newNavPathfinder(authorityDir string) bot.Pathfinder {
	if authorityDir == "" {
		return nil
	}
	validator := movement.NewAuthorityValidator(authorityDir)
	return &bot.GridPathfinder{
		Validator: authorityChordValidator{clipper: validator},
		Grid:      32,
		Budget:    256,
	}
}

// newPlanPathfinder builds the coarse route planner the -plan-from/-plan-to
// mode uses. The step is wider than the local detour so a cross-region route
// stays inside the probe budget.
func newPlanPathfinder(authorityDir string, grid float64, budget int) bot.Pathfinder {
	return &bot.GridPathfinder{
		Validator: authorityChordValidator{clipper: movement.NewAuthorityValidator(authorityDir)},
		Grid:      grid,
		Budget:    budget,
	}
}
