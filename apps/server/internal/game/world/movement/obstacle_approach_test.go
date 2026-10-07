/*
===========================================================================

obstacle_approach_test.go - ground clicks walk to their first obstacle

Production runs both the clipper and path guard. A blocked requested
endpoint must not cancel the reachable approach or its prediction receipt.

===========================================================================
*/
package movement

import (
	"encoding/binary"
	"math"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestGroundClickApproachesObstacle
================
*/
func TestGroundClickApproachesObstacle(t *testing.T) {
	for _, tc := range []struct {
		name  string
		x     int16
		wantX float64
	}{
		{"inside blocked tile", 110, 80 - clipRestPullback},
		{"beyond obstacle", 190, 80 - clipRestPullback},
		{"clear ground", 60, 60},
	} {
		t.Run(tc.name, func(t *testing.T) {
			character := clipTestCharacter()
			rt := testRuntime(character)
			validator := NewWaterValidator(syntheticHeightRoot(t))
			rt.PathGuard = &PathGuard{Mode: PathGuardEnforce, Validator: validator}
			rt.ClientClip = &ClientClip{Mode: ClipApply, Validator: validator}
			rt.Nav = validator
			outcome := rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, tc.x, 0, 110))
			if outcome.Refusal != nil {
				t.Fatalf("reachable approach refused: %v", outcome.Refusal)
			}
			if len(outcome.Frames) != 1 || outcome.Frames[0].Opcode != simulation.OpMovementAck {
				t.Fatalf("missing native movement acknowledgement: %+v", outcome.Frames)
			}
			if got := binary.LittleEndian.Uint16(outcome.Frames[0].Payload[7:9]); got != uint16(math.Round(tc.wantX)) {
				t.Fatalf("native acknowledgement x=%d, want %d", got, uint16(math.Round(tc.wantX)))
			}
			// The browser's ID-bearing receipt serializes this same authority.
			world := outcome.Authority
			if math.Abs(world.Spawn.X-tc.wantX) > clipCoordEps || world.Spawn.Z != 110 {
				t.Fatalf("receipt goal=%+v, want x=%v z=110", world.Spawn, tc.wantX)
			}
			if *character.World.Spawn.X != world.Spawn.X {
				t.Fatalf("persisted goal diverged from receipt: %+v", character.World.Spawn)
			}
			start := world.LiveSpawnAt(testStartMs)
			middle := world.LiveSpawnAt(testStartMs + 100)
			end := world.LiveSpawnAt(testStartMs + 100000)
			if start.X != 30 || middle.X <= start.X || middle.X >= world.Spawn.X || end.X != world.Spawn.X {
				t.Fatalf("approach must walk then stop: start=%+v middle=%+v end=%+v", start, middle, end)
			}
			if report := validator.ValidateMovementPath(start, end); report.Verdict != PathLegal {
				t.Fatalf("committed approach is not walkable: %+v", report)
			}
		})
	}
}

/*
================
TestClippedGroundClickStillRequiresAuthorityCoverage
================
*/
func TestClippedGroundClickStillRequiresAuthorityCoverage(t *testing.T) {
	for _, report := range []PathReport{
		{Verdict: PathNoCoverage},
		{Verdict: PathLegal, TilesUncovered: 1},
		{Verdict: PathLegal, Truncated: true},
		{Verdict: PathEndpointBlocked},
	} {
		character := clipTestCharacter()
		rt := testRuntime(character)
		rt.PathGuard = &PathGuard{Mode: PathGuardEnforce, Validator: fakePathValidator{report: report}}
		rt.ClientClip = &ClientClip{Mode: ClipApply, Validator: &fakeClipValidator{
			report: ClipReport{Outcome: ClipBlocked, Rest: spawnAt(0x6B4F, 60, 110)},
		}}
		outcome := rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, 110, 0, 110))
		if outcome.Refusal == nil || len(outcome.Frames) != 0 || outcome.Authority.Spawn.X != 30 {
			t.Fatalf("invalid clipped route %+v was accepted: %+v", report, outcome)
		}
	}
}

/*
================
TestReportedRegionBlockedClickApproachesContact
================
*/
func TestReportedRegionBlockedClickApproachesContact(t *testing.T) {
	v := realAuthorityValidator(t)
	// BR-261005-2009-70E4 supplied the start, not the clicked endpoint.
	// This nearby blocked destination reproduces the same rejection on the
	// licensed collision data, without claiming it was the reporter's click.
	from := simulation.Spawn{RegionID: 0x6848, X: 1358.1, Y: -155.5, Z: 1701}
	to := simulation.Spawn{RegionID: from.RegionID, X: 1158, Y: -155, Z: 1501}
	if path := v.ValidateMovementPath(from, to); path.Verdict != PathEndpointBlocked {
		t.Fatalf("report fixture no longer ends on blocked ground: %+v", path)
	}
	clip := v.ClipMovementPath(from, to)
	if clip.Outcome != ClipBlocked || math.Hypot(clip.Rest.X-from.X, clip.Rest.Z-from.Z) < 10 {
		t.Fatalf("report fixture has no reachable approach: %+v", clip)
	}
	rt := &Runtime{
		PathGuard:  &PathGuard{Mode: PathGuardEnforce, Validator: v},
		ClientClip: &ClientClip{Mode: ClipApply, Validator: v},
		Nav:        v,
	}
	end, walk, refusal := rt.ConstrainMovementFrom("MajorOfTest", from, simulation.NavOwner{}, to)
	if refusal != nil {
		t.Fatalf("reachable report-region approach refused: %v", refusal)
	}
	if end != clip.Rest || !walk.Rest.Resolved() {
		t.Fatalf("approach=%+v owner=%+v, want first contact %+v", end, walk.Rest, clip.Rest)
	}
	if path := v.ValidateMovementPathFrom(from, simulation.NavOwner{}, end); path.Verdict != PathLegal {
		t.Fatalf("approach crosses blocked ground: %+v", path)
	}
}
