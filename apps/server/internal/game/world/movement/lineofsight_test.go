/*
===========================================================================

lineofsight_test.go - tests for Runtime.LineOfSight

===========================================================================
*/

package movement

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

// The Jangan veranda rail blocks a walk, so it blocks a skill; a chord
// that stops short of the rail is clear.
func TestLineOfSightRealRail(t *testing.T) {
	validator := realAuthorityValidator(t)
	rt := &Runtime{ClientClip: &ClientClip{Mode: ClipApply, Validator: validator}}

	from := simulation.Spawn{RegionID: 0x61A7, X: 623, Y: 3.04, Z: 1271}
	behindRail := simulation.Spawn{RegionID: 0x61A7, X: 683, Y: 3.04, Z: 1271}
	beforeRail := simulation.Spawn{RegionID: 0x61A7, X: 640, Y: 3.04, Z: 1271}

	if rt.LineOfSight(from, simulation.NavOwner{}, behindRail) {
		t.Fatal("line through the east rail reported clear")
	}
	if !rt.LineOfSight(from, simulation.NavOwner{}, beforeRail) {
		t.Fatal("line short of the rail reported blocked")
	}
}

// Only an applied clip is authority, and a line test moves nobody, so it
// must not count as an inspected walk.
func TestLineOfSightPostureAndStatistics(t *testing.T) {
	from := spawnAt(0x6B4F, 30, 110)
	to := spawnAt(0x6B4F, 190, 110)
	blocked := &fakeClipValidator{report: ClipReport{Outcome: ClipBlocked}}

	applied := &ClientClip{Mode: ClipApply, Validator: blocked}
	if (&Runtime{ClientClip: applied}).LineOfSight(from, simulation.NavOwner{}, to) {
		t.Fatal("applied clip: blocked line reported clear")
	}
	if stats := applied.Stats(); stats.Inspected != 0 {
		t.Fatalf("line test counted as a walk: %+v", stats)
	}

	observing := &ClientClip{Mode: ClipObserve, Validator: blocked}
	if !(&Runtime{ClientClip: observing}).LineOfSight(from, simulation.NavOwner{}, to) {
		t.Fatal("observing clip refused a line")
	}
	if !(&Runtime{}).LineOfSight(from, simulation.NavOwner{}, to) {
		t.Fatal("no clip refused a line")
	}
}

/*
================
fakeSegmentClipValidator

fakeSegmentClipValidator serves queued leg reports to LineOfSight and
records where each leg started. An empty queue arrives.
================
*/
type fakeSegmentClipValidator struct {
	reports []ClipReport
	froms   []simulation.Spawn
}

/*
================
fakeSegmentClipValidator.ClipMovementPath
================
*/
func (f *fakeSegmentClipValidator) ClipMovementPath(from, to simulation.Spawn) ClipReport {
	return ClipReport{Outcome: ClipArrived, Rest: to}
}

/*
================
fakeSegmentClipValidator.ClipMovementSegmentFrom
================
*/
func (f *fakeSegmentClipValidator) ClipMovementSegmentFrom(from simulation.Spawn, _ simulation.NavOwner, to simulation.Spawn) ClipReport {
	f.froms = append(f.froms, from)
	if len(f.reports) == 0 {
		return ClipReport{Outcome: ClipArrived, Rest: to}
	}
	report := f.reports[0]
	f.reports = f.reports[1:]
	return report
}

// Case B1: 98D880 re-runs the move test from the point a continuing leg
// reached. A first leg that clips at an outline exit and continues is
// not a blocked line when the next leg arrives.
func TestLineOfSightChainsContinuedLegs(t *testing.T) {
	from := spawnAt(0x6B4F, 30, 110)
	mid := spawnAt(0x6B4F, 90, 110)
	to := spawnAt(0x6B4F, 190, 110)
	validator := &fakeSegmentClipValidator{reports: []ClipReport{
		{Outcome: ClipBlocked, NativeResult: monster.NavResultClipped, Rest: mid, continuation: true},
	}}
	rt := &Runtime{ClientClip: &ClientClip{Mode: ClipApply, Validator: validator}}
	if !rt.LineOfSight(from, simulation.NavOwner{}, to) {
		t.Fatal("continued leg chain reported blocked")
	}
	if len(validator.froms) != 2 || validator.froms[1] != mid {
		t.Fatalf("legs started at %+v, want the second leg from the reached point %+v", validator.froms, mid)
	}
}

// A leg with the failure bit 0x10000000 stops the native test at once,
// and a clipped leg that does not continue is a blocked line: the final
// leg's result decides (98D880 bit evaluation, 0x10000001).
func TestLineOfSightFinalLegDecides(t *testing.T) {
	from := spawnAt(0x6B4F, 30, 110)
	to := spawnAt(0x6B4F, 190, 110)

	blocked := &fakeSegmentClipValidator{reports: []ClipReport{
		{Outcome: ClipBlocked, NativeResult: monster.NavResultBlocked, Rest: from, continuation: true},
	}}
	rt := &Runtime{ClientClip: &ClientClip{Mode: ClipApply, Validator: blocked}}
	if rt.LineOfSight(from, simulation.NavOwner{}, to) {
		t.Fatal("failure-bit leg reported clear")
	}
	if len(blocked.froms) != 1 {
		t.Fatalf("failure-bit leg chained %d legs, want 1", len(blocked.froms))
	}

	clipped := &fakeSegmentClipValidator{reports: []ClipReport{
		{Outcome: ClipBlocked, NativeResult: monster.NavResultClipped, Rest: spawnAt(0x6B4F, 90, 110)},
	}}
	rt = &Runtime{ClientClip: &ClientClip{Mode: ClipApply, Validator: clipped}}
	if rt.LineOfSight(from, simulation.NavOwner{}, to) {
		t.Fatal("clipped leg without continuation reported clear")
	}
}

// Six legs spent still continuing: 98D880 judges the final leg's result,
// and a continuing leg carries no failure bit, so the line is clear.
// The pre-fix single-query answer read the chain cap as a block. The
// queued legs carry the bits clipObjectRest actually leaves on an open
// outline exit (Clipped, with continuation set).
func TestLineOfSightLegCapOnContinuingLeg(t *testing.T) {
	from := spawnAt(0x6B4F, 30, 110)
	to := spawnAt(0x6B4F, 190, 110)
	rest := spawnAt(0x6B4F, 90, 110)
	validator := &fakeSegmentClipValidator{}
	for leg := 0; leg < maxLineOfSightLegs; leg++ {
		validator.reports = append(validator.reports, ClipReport{
			Outcome: ClipBlocked, NativeResult: monster.NavResultClipped, Rest: rest, continuation: true,
		})
	}
	rt := &Runtime{ClientClip: &ClientClip{Mode: ClipApply, Validator: validator}}
	if !rt.LineOfSight(from, simulation.NavOwner{}, to) {
		t.Fatal("leg cap on a continuing leg reported blocked")
	}
	if len(validator.froms) != maxLineOfSightLegs {
		t.Fatalf("ran %d legs, want the native cap %d", len(validator.froms), maxLineOfSightLegs)
	}
}

// Regression (review on #1107): a target just past an open deck edge.
// The outline-exit leg carries Clipped with continuation set
// (clipObjectRest); when the leg started within 5 units of the target,
// 98B300 settles the run and clears the contact bits, so the line is
// clear and no further leg runs. Judging the leg's Clipped bit instead
// refused the cast with 0x3010.
func TestLineOfSightDeckEdgeExitSettlesClear(t *testing.T) {
	from := spawnAt(0x6B4F, 186, 110)
	to := spawnAt(0x6B4F, 190, 110)
	rest := spawnAt(0x6B4F, 188, 110)
	validator := &fakeSegmentClipValidator{reports: []ClipReport{
		{Outcome: ClipBlocked, NativeResult: monster.NavResultClipped, Rest: rest, continuation: true},
	}}
	rt := &Runtime{ClientClip: &ClientClip{Mode: ClipApply, Validator: validator}}
	if !rt.LineOfSight(from, simulation.NavOwner{}, to) {
		t.Fatal("outline exit settling within 5 units of the target reported blocked")
	}
	if len(validator.froms) != 1 {
		t.Fatalf("settled line ran %d legs, want 1", len(validator.froms))
	}
}

// The settle distance is measured from the leg's start (98B300,
// 98B5B2..98B5E9), not from the point the leg reached: a long leg whose
// exit lands within 5 units of the target has not settled, and the
// post-exit leg to the target still runs.
func TestLineOfSightSettleMeasuredFromLegStart(t *testing.T) {
	from := spawnAt(0x6B4F, 30, 110)
	to := spawnAt(0x6B4F, 190, 110)
	rest := spawnAt(0x6B4F, 187, 110)
	validator := &fakeSegmentClipValidator{reports: []ClipReport{
		{Outcome: ClipBlocked, NativeResult: monster.NavResultClipped, Rest: rest, continuation: true},
	}}
	rt := &Runtime{ClientClip: &ClientClip{Mode: ClipApply, Validator: validator}}
	if !rt.LineOfSight(from, simulation.NavOwner{}, to) {
		t.Fatal("post-exit leg to the target reported blocked")
	}
	if len(validator.froms) != 2 || validator.froms[1] != rest {
		t.Fatalf("legs started at %+v, want the post-exit leg from %+v", validator.froms, rest)
	}
}

// Deck-exclusion replay for the Case B reports (threads
// 1560183802888855573 / 1560185584390246451, Donwhang cave, region
// 0x8001): if a reporter's own anchor had no dungeon floor coverage,
// every clip would rest at the anchor itself and the walker would be
// frozen in all eight directions - the unreachable-deck explanation.
// The replay asserts the anchors stand on covered floor; the per-anchor
// table is logged either way and reported with the fix.
func TestLineOfSightReplayReporterAnchors(t *testing.T) {
	validator := realAuthorityValidator(t)
	rt := &Runtime{ClientClip: &ClientClip{Mode: ClipApply, Validator: validator}}
	anchors := []simulation.Spawn{
		{RegionID: 0x8001, X: 993.197, Y: 0.00034, Z: -307.099},
		{RegionID: 0x8001, X: 629, Y: -2.22, Z: 74},
	}
	for _, anchor := range anchors {
		frozen := 0
		for dir := 0; dir < 8; dir++ {
			angle := float64(dir) * math.Pi / 4
			to := anchor
			to.X += 8 * math.Cos(angle)
			to.Z += 8 * math.Sin(angle)
			report := validator.ClipMovementPath(anchor, to)
			los := rt.LineOfSight(anchor, simulation.NavOwner{}, to)
			t.Logf("anchor (%.3f,%.3f) dir %d: clip=%s rest=(%.3f,%.3f,%.3f) los=%v",
				anchor.X, anchor.Z, dir, report.Outcome, report.Rest.X, report.Rest.Y, report.Rest.Z, los)
			if report.Outcome == ClipBlocked && report.Rest.X == anchor.X && report.Rest.Z == anchor.Z {
				frozen++
			}
		}
		if frozen == 8 {
			t.Fatalf("anchor %+v frozen in all 8 directions: the unreachable-deck exclusion fires", anchor)
		}
		t.Logf("anchor (%.3f,%.3f): frozen %d/8 directions", anchor.X, anchor.Z, frozen)
	}
}

// Outdoor and dungeon positions never see each other.
func TestLineOfSightCrossPlane(t *testing.T) {
	clear := &fakeClipValidator{report: ClipReport{Outcome: ClipArrived}}
	rt := &Runtime{ClientClip: &ClientClip{Mode: ClipApply, Validator: clear}}
	outdoor := spawnAt(0x6B4F, 30, 110)
	dungeon := simulation.Spawn{RegionID: 0x8000 | 0x6B4F, X: 30, Z: 110}
	if rt.LineOfSight(outdoor, simulation.NavOwner{}, dungeon) {
		t.Fatal("line across the dungeon boundary reported clear")
	}
	if clear.calls != 0 {
		t.Fatalf("cross-plane line reached the validator %d times", clear.calls)
	}
}
