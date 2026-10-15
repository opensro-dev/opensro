package main

import (
	"testing"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/world/movement"
	"opensro.online/server/internal/game/world/simulation"
)

// fakeClipper reports a fixed clip outcome.
type fakeClipper struct{ outcome movement.ClipOutcome }

func (f fakeClipper) ClipMovementPath(from, to simulation.Spawn) movement.ClipReport {
	return movement.ClipReport{Outcome: f.outcome}
}

// TestChordValidatorUsesTheRuntimeClip guards the live stall: the plan must use
// the same clip the server applies to a player move. The classifier
// ValidateMovementPath accepts a chord that crosses a blocked tile, so a plan
// built on it clips at the obstacle and the bot never arrives.
func TestChordValidatorUsesTheRuntimeClip(t *testing.T) {
	arrived := authorityChordValidator{clipper: fakeClipper{outcome: movement.ClipArrived}}
	if !arrived.Walkable(bot.Point{Region: 25000, X: 960, Z: 906}, bot.Point{Region: 25000, X: 960, Z: 842}) {
		t.Fatal("an arrived chord was rejected")
	}
	blocked := authorityChordValidator{clipper: fakeClipper{outcome: movement.ClipBlocked}}
	if blocked.Walkable(bot.Point{Region: 25000, X: 960, Z: 970}, bot.Point{Region: 25000, X: 960, Z: 906}) {
		t.Fatal("a blocked chord was accepted")
	}
}
