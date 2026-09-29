/*
===========================================================================

clip_test.go - the client-clip replica and its wiring into ground moves

clipreplicate-wave clip tests. Fixture: the water_test syntheticHeightRoot
tree (region 0x6B4F, sector 79/107, real 96x20 bundle geometry): blocked
obstacle tiles x,z in [4,7]; cell-less tile (10,10); sliver wall x==93;
open east neighbor 0x6B50.

Inputs deliberately CANNOT COINCIDE with legal outcomes: every blocked
case asserts the exact offending world tile AND the exact rest coordinate
(the pullback boundary is a constant, so a clip that stops at the wrong
boundary, on the wrong side, or not at all fails loudly).

===========================================================================
*/
package movement

import (
	"math"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

const clipCoordEps = 1e-9

/*
================
TestClipMovementPathOutcomes
================
*/
func TestClipMovementPathOutcomes(t *testing.T) {
	validator := NewWaterValidator(syntheticHeightRoot(t))

	firstObstacleX, firstObstacleZ := tile6B4F(4, 5)
	lastObstacleX, _ := tile6B4F(7, 5)
	obstacleZEntryX, obstacleZEntryZ := tile6B4F(5, 4)
	cellLessX, cellLessZ := tile6B4F(10, 10)

	cases := []struct {
		name        string
		from, to    simulation.Spawn
		wantOutcome ClipOutcome
		// wantRestX/Z: exact rest locals (checked only when checkRest).
		wantRestX, wantRestZ float64
		checkRest            bool
		wantTileX, wantTileZ int
		checkTile            bool
	}{
		{
			name: "openFieldArrives",
			from: spawnAt(0x6B4F, 600, 600), to: spawnAt(0x6B4F, 700, 700),
			wantOutcome: ClipArrived,
		},
		{
			// +x into the obstacle wall at x=80 (tile 4): rest one pullback
			// short of the boundary, z unchanged (single-axis crossing).
			name: "clipsShortOfObstaclePlusX",
			from: spawnAt(0x6B4F, 30, 110), to: spawnAt(0x6B4F, 190, 110),
			wantOutcome: ClipBlocked,
			wantRestX:   80 - clipRestPullback, wantRestZ: 110, checkRest: true,
			wantTileX: firstObstacleX, wantTileZ: firstObstacleZ, checkTile: true,
		},
		{
			// Blocked DESTINATION: the clip still stops at the first contact
			// (the client's integrator does not care that the goal itself is
			// inside the wall).
			name: "clipsBlockedEndpointAtFirstContact",
			from: spawnAt(0x6B4F, 30, 110), to: spawnAt(0x6B4F, 110, 110),
			wantOutcome: ClipBlocked,
			wantRestX:   80 - clipRestPullback, wantRestZ: 110, checkRest: true,
			wantTileX: firstObstacleX, wantTileZ: firstObstacleZ, checkTile: true,
		},
		{
			// -x into the far side of the obstacle at x=160 (tile 7): the
			// rest must land on the +x side of the boundary - a sign error
			// in the pullback would place it INSIDE the obstacle.
			name: "clipsShortOfObstacleMinusX",
			from: spawnAt(0x6B4F, 190, 110), to: spawnAt(0x6B4F, 30, 110),
			wantOutcome: ClipBlocked,
			wantRestX:   160 + clipRestPullback, wantRestZ: 110, checkRest: true,
			wantTileX: lastObstacleX, wantTileZ: firstObstacleZ, checkTile: true,
		},
		{
			// +z into the obstacle at z=80 (tile z=4, x tile 5).
			name: "clipsShortOfObstaclePlusZ",
			from: spawnAt(0x6B4F, 110, 30), to: spawnAt(0x6B4F, 110, 190),
			wantOutcome: ClipBlocked,
			wantRestX:   110, wantRestZ: 80 - clipRestPullback, checkRest: true,
			wantTileX: obstacleZEntryX, wantTileZ: obstacleZEntryZ, checkTile: true,
		},
		{
			// The cell-less tile (10,10) is unblocked but off the cell graph
			// - the clip must treat it exactly like a blocked byte.
			name: "clipsAtCellLessTile",
			from: spawnAt(0x6B4F, 150, 210), to: spawnAt(0x6B4F, 210, 210),
			wantOutcome: ClipBlocked,
			wantRestX:   200 - clipRestPullback, wantRestZ: 210, checkRest: true,
			wantTileX: cellLessX, wantTileZ: cellLessZ, checkTile: true,
		},
		{
			// Departing FROM a blocked tile never clips (escape doctrine).
			name: "startBlockedNeverClips",
			from: spawnAt(0x6B4F, 110, 110), to: spawnAt(0x6B4F, 30, 110),
			wantOutcome: ClipStartBlocked,
		},
		{
			name: "crossRegionSeamArrives",
			from: spawnAt(0x6B4F, 1890, 210), to: spawnAt(0x6B50, 100, 210),
			wantOutcome: ClipArrived,
		},
		{
			name: "noCoverageFailsOpen",
			from: spawnAt(0x1234, 100, 100), to: spawnAt(0x1234, 300, 300),
			wantOutcome: ClipNoCoverage,
		},
		{
			name: "missingDungeonCoverageBlocks",
			from: spawnAt(0x8B4F, 100, 100), to: spawnAt(0x8B4F, 300, 300),
			wantOutcome: ClipBlocked,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := validator.ClipMovementPath(tc.from, tc.to)
			if report.Outcome != tc.wantOutcome {
				t.Fatalf("outcome = %s, want %s (report %+v)", report.Outcome, tc.wantOutcome, report)
			}
			if tc.wantOutcome != ClipBlocked {
				assertUnclippedRest(t, validator, report, tc.to)
				return
			}
			if tc.checkTile && (report.BlockedTileX != tc.wantTileX || report.BlockedTileZ != tc.wantTileZ) {
				t.Errorf("blocked tile = (%d,%d), want (%d,%d)",
					report.BlockedTileX, report.BlockedTileZ, tc.wantTileX, tc.wantTileZ)
			}
			if tc.checkRest {
				if report.Rest.RegionID != 0x6B4F {
					t.Fatalf("rest region = 0x%04X, want 0x6B4F", report.Rest.RegionID)
				}
				if diff := report.Rest.X - tc.wantRestX; diff > clipCoordEps || diff < -clipCoordEps {
					t.Errorf("rest x = %v, want %v", report.Rest.X, tc.wantRestX)
				}
				if diff := report.Rest.Z - tc.wantRestZ; diff > clipCoordEps || diff < -clipCoordEps {
					t.Errorf("rest z = %v, want %v", report.Rest.Z, tc.wantRestZ)
				}
			}
			if simulation.IsDungeonRegion(tc.from.RegionID) {
				if report.Rest != tc.from {
					t.Fatal("Missing dungeon coverage must retain start")
				}
				return
			}
			// The rest tile must always be walkable - a rest inside the
			// blocked tile would re-block the NEXT move's departure.
			restTile := globalTile{
				x: simulation.SectorX(report.Rest.RegionID)*96 + int(report.Rest.X/20),
				z: simulation.SectorY(report.Rest.RegionID)*96 + int(report.Rest.Z/20),
			}
			if w, ok := validator.globalTileWalkable(restTile, 96); !ok || !w {
				t.Errorf("rest tile (%d,%d) not walkable (ok=%v w=%v)", restTile.x, restTile.z, ok, w)
			}
		})
	}
}

// ---- object-nav awareness (clip object class + deck overrides) ----

/*
================
TestClipMovementPathObjectAware

TestClipMovementPathObjectAware drives the clip over the
syntheticObjectNavRoot world (objectnav_test.go): an OPEN deck at
(500,500) y=10 over BLOCKED tiles (one open east outline edge at
x=520, 0x3 rails elsewhere) and a SEALED deck at (100,100) y=10 over
walkable terrain. Every rest coordinate is pinned exactly - a contact
on the wrong edge, at the wrong parameter, or on the wrong side fails
loudly.
================
*/
func TestClipMovementPathObjectAware(t *testing.T) {
	validator := NewWaterValidator(syntheticObjectNavRoot(t))

	deck := func(x, z float64) simulation.Spawn {
		return simulation.Spawn{RegionID: 0x6B4F, X: x, Y: 10, Z: z}
	}
	const restEps = 1e-6

	cases := []struct {
		name                 string
		from, to             simulation.Spawn
		wantOutcome          ClipOutcome
		wantClass            ClipClass
		wantRestX, wantRestZ float64
		checkRest            bool
		wantOverrides        bool
	}{
		{
			// Walking ALONG the deck over blocked tiles: the departure,
			// interior and endpoint tile verdicts all override - no clip.
			name: "alongDeckArrives",
			from: deck(490, 500), to: deck(510, 500),
			wantOutcome: ClipArrived, wantOverrides: true,
		},
		{
			// Crossing the deck's north rail (outline 0x3 at z=520): an
			// Start lies on the shared diagonal: native 428F40 first nudges
			// it into cell 0. Intersect that corrected chord with the north
			// rail, then apply the cell-1 centroid inset (45C1B0).
			name: "railClipsObjectClass",
			from: deck(500, 500), to: deck(500, 540),
			wantOutcome: ClipBlocked, wantClass: ClipClassObject,
			wantRestX: 499.9802551269531, wantRestZ: 519.8214721679688, checkRest: true,
			wantOverrides: true,
		},
		{
			// Leaving through the OPEN east outline edge (flags 0x0, the
			// exit-to-terrain shape): reflect, resume and arrive.
			name: "openExitArrives",
			from: deck(500, 500), to: deck(540, 500),
			wantOutcome: ClipArrived, wantOverrides: true,
		},
		{
			// Ground-level chord UNDER the sealed deck at (100,100): the
			// rails cross the chord in XZ but sit 10u above it - the
			// height gate keeps the underpass free.
			name: "underDeckPassesFree",
			from: spawnAt(0x6B4F, 70, 100), to: spawnAt(0x6B4F, 130, 100),
			wantOutcome: ClipArrived,
		},
		{
			// Approaching the sealed deck's north rail FROM OUTSIDE at
			// deck height: 0x3 blocks both directions.
			name:        "entryRailClipsFromOutside",
			from:        simulation.Spawn{RegionID: 0x6B4F, X: 100, Y: 10, Z: 150},
			to:          simulation.Spawn{RegionID: 0x6B4F, X: 100, Y: 10, Z: 100},
			wantOutcome: ClipBlocked, wantClass: ClipClassObject,
			// Authored direction 0 is (+1,0): outside blocking biases the original start.
			wantRestX: float64(float32(100 + float32(.01))), wantRestZ: 150, checkRest: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := validator.ClipMovementPath(tc.from, tc.to)
			if report.Outcome != tc.wantOutcome || report.Class != tc.wantClass {
				t.Fatalf("outcome = %s/%s, want %s/%s (report %+v)",
					report.Outcome, report.Class, tc.wantOutcome, tc.wantClass, report)
			}
			if tc.wantOverrides && report.ObjectDeckOverrides == 0 {
				t.Error("expected object-deck overrides on the tile plane")
			}
			if tc.wantOutcome != ClipBlocked {
				assertUnclippedRest(t, validator, report, tc.to)
				return
			}
			if tc.checkRest {
				if diff := report.Rest.X - tc.wantRestX; diff > restEps || diff < -restEps {
					t.Errorf("rest x = %v, want %v", report.Rest.X, tc.wantRestX)
				}
				if diff := report.Rest.Z - tc.wantRestZ; diff > restEps || diff < -restEps {
					t.Errorf("rest z = %v, want %v", report.Rest.Z, tc.wantRestZ)
				}
			}
		})
	}
}

/*
================
TestClipMovementPathObjectDataMissingFailsOpen

TestClipMovementPathObjectDataMissingFailsOpen corrupts the object
resource index: the object plane vanishes and the clip degrades to the
OLD tile-only outcomes (a deck departure over blocked tiles goes back
to startBlocked; the rail chord loses its contact) - never a fault.
================
*/
func TestClipMovementPathObjectDataMissingFailsOpen(t *testing.T) {
	root := syntheticObjectNavRoot(t)
	writeTestAsset(t, root, "assets/world/outdoor/object-resources.json", `not json`)
	validator := NewWaterValidator(root)

	report := validator.ClipMovementPath(
		simulation.Spawn{RegionID: 0x6B4F, X: 500, Y: 10, Z: 500},
		simulation.Spawn{RegionID: 0x6B4F, X: 500, Y: 10, Z: 540})
	if report.Outcome != ClipStartBlocked || report.Class != "" {
		t.Fatalf("outcome without object data = %s/%s, want the old startBlocked", report.Outcome, report.Class)
	}
}

/*
================
TestClipMovementPathRealJanganVerandaRail

TestClipMovementPathRealJanganVerandaRail pins the object clip on the
REAL Jangan walkway payload (region 0x61A7, the cj_pub03_floor sealed
veranda of the enter-world incident, deck y~3.04 over terrain y=0):
chords east and north off the deck cross 0x3 rail outline edges and
must hard-stop OBJECT-class at the rail (a bit0 clip STOPS the native
walk - the chord never continues onto the walkable terrain beyond).
The tile plane under the whole veranda is WALKABLE, so any contact
here is object evidence alone.
================
*/
func TestClipMovementPathRealJanganVerandaRail(t *testing.T) {
	validator := realAuthorityValidator(t)

	from := simulation.Spawn{RegionID: 0x61A7, X: 623, Y: 3.04, Z: 1271}
	cases := []struct {
		name                 string
		to                   simulation.Spawn
		wantRestX, wantRestZ float64
	}{
		// Probed 2026-07-29 against the shipped payload: the east rail
		// sits at x~660.4, the north rail at z~1271.6.
		// Rest now includes the cell-directed inset; exact native arithmetic
		// is checked separately by TestNativeContactResponseReference.
		{"eastRail", simulation.Spawn{RegionID: 0x61A7, X: 683, Y: 3.04, Z: 1271}, 660.302, 1270.809},
		{"northRail", simulation.Spawn{RegionID: 0x61A7, X: 623, Y: 3.04, Z: 1291}, 622.895, 1271.416},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := validator.ClipMovementPath(from, tc.to)
			if report.Outcome != ClipBlocked || report.Class != ClipClassObject {
				t.Fatalf("outcome = %s/%s (%+v), want blocked/object", report.Outcome, report.Class, report)
			}
			if math.Abs(report.Rest.X-tc.wantRestX) > 0.1 || math.Abs(report.Rest.Z-tc.wantRestZ) > 0.1 {
				t.Errorf("rest = (%.3f, %.3f), want (%.2f, %.2f) +-0.1",
					report.Rest.X, report.Rest.Z, tc.wantRestX, tc.wantRestZ)
			}
		})
	}
}

/*
================
BenchmarkClipMovementPathWarm

BenchmarkClipMovementPathWarm pins the warm per-move cost of the clip
INCLUDING the object plane (the resolved placement sets and meshes are
cached after the first chord) on a real region. Runs inside the
movement mutex in production, so this bounds the added per-move cost.
================
*/
func BenchmarkClipMovementPathWarm(b *testing.B) {
	root := filepath.Join("..", "..", "..", "..", "..", "..", ".generated", "client-public")
	if _, err := os.Stat(filepath.Join(root, "assets", "world", "world-region-catalog.json")); err != nil {
		b.Skipf("real client assets not present (%v)", err)
	}
	validator := NewWaterValidator(root)
	from := spawnAt(0x6B4F, 1205, 396)
	to := spawnAt(0x6B4F, 1150, 450)
	validator.ClipMovementPath(from, to) // warm the caches
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ClipMovementPath(from, to)
	}
}

/*
================
BenchmarkClipMovementPathWarmBridge

BenchmarkClipMovementPathWarmBridge is the worst-shape warm cost: a
deck chord over blocked tiles (object contact + deck overrides firing)
on the real Constantinople harbor bridge.
================
*/
func BenchmarkClipMovementPathWarmBridge(b *testing.B) {
	root := filepath.Join("..", "..", "..", "..", "..", "..", ".generated", "client-public")
	if _, err := os.Stat(filepath.Join(root, "assets", "world", "world-region-catalog.json")); err != nil {
		b.Skipf("real client assets not present (%v)", err)
	}
	validator := NewWaterValidator(root)
	from := simulation.Spawn{RegionID: 0x6850, X: 1263, Y: -25.17, Z: 1490.5}
	to := simulation.Spawn{RegionID: 0x6850, X: 1263, Y: -25.17, Z: 1520.5}
	validator.ClipMovementPath(from, to) // warm the caches
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ClipMovementPath(from, to)
	}
}

// ---- ProcessMove mode semantics over a fake validator ----

/*
================
fakeClipValidator
================
*/
type fakeClipValidator struct {
	report ClipReport
	calls  int
}

/*
================
fakeClipValidator.ClipMovementPath
================
*/
func (f *fakeClipValidator) ClipMovementPath(from, to simulation.Spawn) ClipReport {
	f.calls++
	return f.report
}

/*
================
TestProcessMoveObserveNeverMovesAnybody
================
*/
func TestProcessMoveObserveNeverMovesAnybody(t *testing.T) {
	from := spawnAt(0x6B4F, 30, 110)
	to := spawnAt(0x6B4F, 190, 110)
	rest := spawnAt(0x6B4F, 79.99, 110)
	fake := &fakeClipValidator{report: ClipReport{Outcome: ClipBlocked, Rest: rest, BlockedTileX: 1, BlockedTileZ: 2}}
	clip := &ClientClip{Mode: ClipObserve, Validator: fake}

	got := clip.ProcessMove("Asd", from, to)
	if got != to {
		t.Fatalf("observe returned %+v, want the untouched goal %+v (OBSERVE MUST MOVE NOBODY)", got, to)
	}
	stats := clip.Stats()
	if stats.Inspected != 1 || stats.WouldClip != 1 || stats.Applied != 0 {
		t.Errorf("stats = %+v, want inspected=1 wouldClip=1 applied=0", stats)
	}
}

/*
================
TestProcessMoveApplyCommitsTheClippedRest
================
*/
func TestProcessMoveApplyCommitsTheClippedRest(t *testing.T) {
	from := spawnAt(0x6B4F, 30, 110)
	to := spawnAt(0x6B4F, 190, 110)
	rest := spawnAt(0x6B4F, 79.99, 110)
	fake := &fakeClipValidator{report: ClipReport{Outcome: ClipBlocked, Rest: rest}}
	clip := &ClientClip{Mode: ClipApply, Validator: fake}

	if got := clip.ProcessMove("Asd", from, to); got != rest {
		t.Fatalf("apply returned %+v, want the clipped rest %+v", got, rest)
	}
	if stats := clip.Stats(); stats.Applied != 1 {
		t.Errorf("applied = %d, want 1", stats.Applied)
	}
}

/*
================
TestProcessMoveApplyPassesLegalMovesThrough
================
*/
func TestProcessMoveApplyPassesLegalMovesThrough(t *testing.T) {
	to := spawnAt(0x6B4F, 700, 700)
	fake := &fakeClipValidator{report: ClipReport{Outcome: ClipArrived, Rest: to}}
	clip := &ClientClip{Mode: ClipApply, Validator: fake}
	if got := clip.ProcessMove("Asd", spawnAt(0x6B4F, 600, 600), to); got != to {
		t.Fatalf("legal move mutated: %+v, want %+v", got, to)
	}
	if stats := clip.Stats(); stats.Applied != 0 || stats.Arrived != 1 {
		t.Errorf("stats = %+v, want applied=0 arrived=1", clip.Stats())
	}
}

/*
================
TestNilClientClipIsInert
================
*/
func TestNilClientClipIsInert(t *testing.T) {
	var clip *ClientClip
	to := spawnAt(0x6B4F, 190, 110)
	if got := clip.ProcessMove("Asd", spawnAt(0x6B4F, 30, 110), to); got != to {
		t.Fatalf("nil clip returned %+v, want %+v", got, to)
	}
}

/*
================
TestClipModeFromEnv
================
*/
func TestClipModeFromEnv(t *testing.T) {
	cases := []struct {
		value string
		want  ClipMode
	}{
		{"", ClipApply},
		{"observe", ClipObserve},
		{"off", ClipOff},
		{"apply", ClipApply},
		{"banana", ClipApply},
	}
	for _, tc := range cases {
		t.Setenv(EnvMoveClientClip, tc.value)
		if got := ClipModeFromEnv(); got != tc.want {
			t.Errorf("ClipModeFromEnv(%q) = %s, want %s", tc.value, got, tc.want)
		}
	}
}

// ---- end-to-end HandleMove: the Q6 amendment-3 observe invariant ----

/*
================
clipTestCharacter

clipTestCharacter starts the character in region 0x6B4F at (30, 0, 110):
walkable, due west of the obstacle wall at x=80.
================
*/
func clipTestCharacter() *enterworld.Character {
	character := testCharacter()
	regionID := int64(0x6B4F)
	x, y, z := 30.0, 0.0, 110.0
	angle := int64(0)
	character.World = &enterworld.CharacterWorld{
		Spawn:    &enterworld.WorldSpawn{RegionID: &regionID, X: &x, Y: &y, Z: &z, Angle: &angle},
		SpawnSet: true,
	}
	return character
}

/*
================
TestHandleMoveObserveClipDoesNotChangeCommit
================
*/
func TestHandleMoveObserveClipDoesNotChangeCommit(t *testing.T) {
	character := clipTestCharacter()
	rt := testRuntime(character)
	validator := NewWaterValidator(syntheticHeightRoot(t))
	rt.ClientClip = &ClientClip{Mode: ClipObserve, Validator: validator}

	// A category-B chord: destination walkable, chord through the obstacle.
	outcome := rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, 190, 0, 110))
	if outcome.Refusal != nil {
		t.Fatalf("move refused: %v", outcome.Refusal)
	}

	// OBSERVE INVARIANT: the committed goal plane and the ack echo are
	// bit-for-bit what they would be with no clip installed.
	key := simulation.WorldKey("0", character.Name)
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.Spawn.X != 190 || world.Spawn.Z != 110 || world.Spawn.RegionID != 0x6B4F {
		t.Errorf("observe mutated the goal plane: %+v, want x=190 z=110 region 0x6B4F", world.Spawn)
	}
	payload := outcome.Frames[0].Payload
	echoX := uint16(payload[7]) | uint16(payload[8])<<8
	if echoX != 190 {
		t.Errorf("observe mutated the ack echo: x=%d, want 190", echoX)
	}
	if stats := rt.ClientClip.Stats(); stats.WouldClip != 1 || stats.Applied != 0 {
		t.Errorf("stats = %+v, want wouldClip=1 applied=0 (telemetry must still see the chord)", stats)
	}
}

/*
================
TestHandleMoveApplyClipCommitsShortOfTheWall
================
*/
func TestHandleMoveApplyClipCommitsShortOfTheWall(t *testing.T) {
	character := clipTestCharacter()
	rt := testRuntime(character)
	validator := NewWaterValidator(syntheticHeightRoot(t))
	rt.ClientClip = &ClientClip{Mode: ClipApply, Validator: validator}

	outcome := rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, 190, 0, 110))
	if outcome.Refusal != nil {
		t.Fatalf("move refused: %v", outcome.Refusal)
	}

	key := simulation.WorldKey("0", character.Name)
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	wantX := 80 - clipRestPullback
	if diff := world.Spawn.X - wantX; diff > clipCoordEps || diff < -clipCoordEps {
		t.Errorf("apply goal x = %v, want %v (short of the x=80 wall)", world.Spawn.X, wantX)
	}
	if world.Spawn.Z != 110 || world.Spawn.RegionID != 0x6B4F {
		t.Errorf("apply goal = %+v, want z=110 region 0x6B4F", world.Spawn)
	}
	if stats := rt.ClientClip.Stats(); stats.Applied != 1 {
		t.Errorf("applied = %d, want 1", stats.Applied)
	}
}

/*
================
TestHandleMoveAngularTurnBypassesClip
================
*/
func TestHandleMoveAngularTurnBypassesClip(t *testing.T) {
	character := clipTestCharacter()
	rt := testRuntime(character)
	fake := &fakeClipValidator{report: ClipReport{Outcome: ClipBlocked}}
	rt.ClientClip = &ClientClip{Mode: ClipApply, Validator: fake}

	// The stationary angular arm (no GO) has no chord to clip. The GO form
	// is clipped along its leg (direction_test.go).
	outcome := rt.HandleMove("0", character, encodeTurnBody(0, 0x4000))
	if outcome.Refusal != nil {
		t.Fatalf("turn refused: %v", outcome.Refusal)
	}
	if fake.calls != 0 {
		t.Errorf("clip validator called %d times on an angular turn, want 0", fake.calls)
	}
}

/*
================
assertUnclippedRest

assertUnclippedRest is the unclipped-rest contract. Fail-open outcomes pass
the goal through untouched. Every walked outcome keeps the goal's region,
X, Z and facing but stands on the surface the walk reached: Rest.Y is that
surface's height (native walk results are cell-plane heights, never the
request's int16 Y) and RestOwner names it.
================
*/
func assertUnclippedRest(t *testing.T, v *WaterValidator, report ClipReport, to simulation.Spawn) {
	t.Helper()
	if report.Outcome == ClipNoCoverage || report.Outcome == ClipDungeonExempt {
		if report.Rest != to {
			t.Errorf("rest = %+v, want the untouched goal %+v", report.Rest, to)
		}
		return
	}
	if report.Rest.RegionID != to.RegionID || report.Rest.X != to.X || report.Rest.Z != to.Z || report.Rest.Angle != to.Angle {
		t.Errorf("rest = %+v, want the goal's placement %+v", report.Rest, to)
	}
	if !report.RestOwner.Resolved() {
		t.Fatalf("walked rest has no owner: %+v", report)
	}
	owner, y, ok := v.ResolveNavOwner(report.Rest, report.RestOwner)
	if !ok || owner != report.RestOwner || math.Abs(y-report.Rest.Y) > 1e-9 {
		t.Errorf("rest y = %v owner %+v, want surface height %v owner %+v", report.Rest.Y, report.RestOwner, y, owner)
	}
}
