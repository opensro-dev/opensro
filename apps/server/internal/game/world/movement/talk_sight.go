/*
===========================================================================

talk_sight.go - the NPC talk sight question: can this character speak to
that NPC from where it stands

Package movement answers the use gate's one sight question (TalkSight):
the station is talkable-to when the line of sight is clear or the
straight walk toward it rests within talk distance. The gate itself
(npcrange.go, the action package) owns the decision to ask; this file
owns only the geometry, on the same clip seams LineOfSight walks
(runtime.go). Nobody moves and no clip statistics are counted.

===========================================================================
*/
package movement

import (
	"opensro.online/server/internal/game/world/simulation"
)

// talkArrivalDistance is how close a walk must come to an NPC's station
// for the talk to count as arrived: the native client stops its NPC
// approach at this distance from the goal point and dispatches the
// select (25.0, the arrival test of client 878410).
const talkArrivalDistance = 25

/*
================
TalkSight

The NPC talk test (port-only, #4125; owner ruling 2026-11-23: a native
bug is not copied - natively the client picks through scenery and this
server gates on range alone, so an NPC behind a wall can be selected
and used). An NPC is talkable-to exactly when the line of sight is
clear (LineOfSight) or the straight walk toward its station rests
within talkArrivalDistance of it. The walk arm keeps the town
merchants usable: a merchant stands behind her stall counter, the
chord crosses the counter's nav block so the line test fails, and the
walk rests at the counter's face - 10.7 to 18.9 units from the station
on the Jangan potion and accessory merchants' real stations, measured
with the authority validator. A wall between two rooms fails both
arms: the line is blocked and the walk rests at the wall's outer face,
beyond talk distance, until the player walks around it. Nobody moves
and no clip statistics are counted, as in LineOfSight.
================
*/
func (rt *Runtime) TalkSight(from simulation.Spawn, fromOwner simulation.NavOwner, to simulation.Spawn) bool {
	if rt.LineOfSight(from, fromOwner, to) {
		return true
	}
	clip := rt.ClientClip
	if clip == nil || clip.Validator == nil || clip.Mode != ClipApply {
		// LineOfSight answers true on every non-applying posture, so a
		// false from it here means the two ends share no plane, which
		// no walk crosses either.
		return false
	}
	from, to = simulation.NormalizeSpawnFrame(from), simulation.NormalizeSpawnFrame(to)

	// The walk itself, leg by chained leg as in LineOfSight. A leg
	// that starts within talk distance has arrived closely enough
	// whatever its contact bits say (the lineOfSightSettles rule at
	// talk scale).
	rest := from
	if segments, ok := clip.Validator.(segmentClipValidator); ok {
		legFrom, legOwner := from, fromOwner
		for leg := 0; leg < maxLineOfSightLegs; leg++ {
			if spawnDistanceSquared(legFrom, to) <= talkArrivalDistance*talkArrivalDistance {
				return true
			}
			report := segments.ClipMovementSegmentFrom(legFrom, legOwner, to)
			rest = report.Rest
			if !report.continuation {
				break
			}
			legFrom, legOwner = report.Rest, report.RestOwner
		}
	} else {
		var report ClipReport
		if owned, ok := clip.Validator.(ownerClipValidator); ok {
			report = owned.ClipMovementPathFrom(from, fromOwner, to)
		} else {
			report = clip.Validator.ClipMovementPath(from, to)
		}
		rest = report.Rest
	}
	return spawnDistanceSquared(rest, to) <= talkArrivalDistance*talkArrivalDistance
}
