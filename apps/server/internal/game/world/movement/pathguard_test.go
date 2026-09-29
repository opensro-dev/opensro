/*
===========================================================================

pathguard_test.go - the path-walkability guard and its wiring into ground moves

LANE-6 P-MOVE tests: the path-walkability observer. The fixture is the
water_test syntheticHeightRoot tree (region 0x6B4F, sector 79/107, real
96x20 bundle geometry): blocked obstacle tiles x,z in [4,7]; walkable
island x,z in [60,62] ringed by the blocked moat x,z in [58,64]; border
sliver x in [94,95], z in [0,23] walled at x==93 / z==24 but continuing
into the open east neighbor 0x6B50; cell-less tile (10,10).

Test inputs deliberately CANNOT COINCIDE with legal outcomes: every
blocked case names the exact offending world tile, so a traversal that
walks the wrong cells or classifies by the wrong endpoint fails loudly.

===========================================================================
*/
package movement

import (
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
tile6B4F

tile6B4F converts region-local tile coordinates of region 0x6B4F
(sector 79, 107) to the world tile grid the reports carry.
================
*/
func tile6B4F(x, z int) (int, int) {
	return 79*96 + x, 107*96 + z
}

/*
================
spawnAt
================
*/
func spawnAt(regionID uint16, x, z float64) simulation.Spawn {
	return simulation.Spawn{RegionID: regionID, X: x, Y: 0, Z: z}
}

/*
================
TestValidateMovementPathVerdicts
================
*/
func TestValidateMovementPathVerdicts(t *testing.T) {
	validator := NewWaterValidator(syntheticHeightRoot(t))

	obstacleX, obstacleZ := tile6B4F(5, 5)
	firstObstacleX, firstObstacleZ := tile6B4F(4, 5)
	cellLessX, cellLessZ := tile6B4F(10, 10)
	sliverWallX, sliverWallZ := tile6B4F(93, 10)

	cases := []struct {
		name             string
		from, to         simulation.Spawn
		want             PathVerdict
		wantTileX        int
		wantTileZ        int
		checkBlockedTile bool
	}{
		{
			name: "openFieldLegal",
			from: spawnAt(0x6B4F, 600, 600), to: spawnAt(0x6B4F, 700, 700),
			want: PathLegal,
		},
		{
			// Destination inside the obstacle: hostile-shaped (category A).
			name: "endpointBlocked",
			from: spawnAt(0x6B4F, 30, 110), to: spawnAt(0x6B4F, 110, 110),
			want: PathEndpointBlocked, wantTileX: obstacleX, wantTileZ: obstacleZ, checkBlockedTile: true,
		},
		{
			// Walkable endpoint past the obstacle; the straight chord cuts
			// through it (category B - the client would path around).
			name: "segmentCrossesObstacle",
			from: spawnAt(0x6B4F, 30, 110), to: spawnAt(0x6B4F, 190, 110),
			want: PathSegmentBlocked, wantTileX: firstObstacleX, wantTileZ: firstObstacleZ, checkBlockedTile: true,
		},
		{
			// Departing FROM a blocked tile must stay escapable: never A/B.
			name: "startBlockedEscape",
			from: spawnAt(0x6B4F, 110, 110), to: spawnAt(0x6B4F, 30, 110),
			want: PathStartBlocked, wantTileX: obstacleX, wantTileZ: obstacleZ, checkBlockedTile: true,
		},
		{
			// A same-tile hop on a blocked tile classifies startBlocked,
			// never endpointBlocked - enforcement must not trap a player
			// where they already stand.
			name: "sameTileBlockedHop",
			from: spawnAt(0x6B4F, 110, 110), to: spawnAt(0x6B4F, 112, 112),
			want: PathStartBlocked, wantTileX: obstacleX, wantTileZ: obstacleZ, checkBlockedTile: true,
		},
		{
			// Tile (10,10) is unblocked but its cell id misses the cell
			// list - off the graph, same as the client's cellForLocal gate.
			name: "cellLessEndpointBlocked",
			from: spawnAt(0x6B4F, 150, 210), to: spawnAt(0x6B4F, 210, 210),
			want: PathEndpointBlocked, wantTileX: cellLessX, wantTileZ: cellLessZ, checkBlockedTile: true,
		},
		{
			// Border sliver into the open neighbor region: the chord must
			// address tiles across the region seam exactly like the spawn
			// rescue does.
			name: "crossRegionLegal",
			from: spawnAt(0x6B4F, 1890, 210), to: spawnAt(0x6B50, 100, 210),
			want: PathLegal,
		},
		{
			// The in-region wall at tile x=93 between the field and the
			// sliver: endpoint walkable, chord crosses the wall.
			name: "segmentCrossesSliverWall",
			from: spawnAt(0x6B4F, 1700, 210), to: spawnAt(0x6B4F, 1890, 210),
			want: PathSegmentBlocked, wantTileX: sliverWallX, wantTileZ: sliverWallZ, checkBlockedTile: true,
		},
		{
			// No walkability data anywhere on the chord: fail-open.
			name: "noCoverage",
			from: spawnAt(0x1234, 100, 100), to: spawnAt(0x1234, 300, 300),
			want: PathNoCoverage,
		},
		{
			// Dungeon plane (bit 15): outdoor tile math must not apply.
			name: "dungeonExempt",
			from: spawnAt(0x8B4F, 100, 100), to: spawnAt(0x8B4F, 300, 300),
			want: PathDungeonExempt,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			report := validator.ValidateMovementPath(tc.from, tc.to)
			if report.Verdict != tc.want {
				t.Fatalf("verdict = %s, want %s (report %+v)", report.Verdict, tc.want, report)
			}
			if tc.checkBlockedTile && (report.BlockedTileX != tc.wantTileX || report.BlockedTileZ != tc.wantTileZ) {
				t.Errorf("blocked tile = (%d, %d), want (%d, %d)",
					report.BlockedTileX, report.BlockedTileZ, tc.wantTileX, tc.wantTileZ)
			}
		})
	}
}

/*
================
pinchRoot

pinchRoot builds a fixture with a single blocked tile at (5,4). The y=x
chord's single-axis DDA (which breaks the corner tie by stepping Z)
visits the UPPER neighbors (4,5),(5,6),... but never the LOWER neighbor
(5,4); only the supercover corner visit probes (x+stepX, z)=(5,4). So
this tile is detected iff the corner visit runs - the clean isolation
the witnessed-red pairing needs.
================
*/
func pinchRoot(t *testing.T) string {
	return pinchRootAt(t, 5, 4)
}

/*
================
pinchRootAt
================
*/
func pinchRootAt(t *testing.T, blockedX, blockedZ int) string {
	t.Helper()
	root := t.TempDir()
	writeTestAsset(t, root, "assets/world/world-region-catalog.json", `{
		"regionsById": {"0x6b4f": [{
			"id": "0x6b4f", "seedRegionId": "0x6b4f",
			"worldRegionsPublicPath": "/assets/world/outdoor/world-regions.json",
			"bundlePublicPath": "/assets/world/outdoor/regions/region-6b4f.json"
		}]}
	}`)
	writeTestAsset(t, root, "assets/world/outdoor/world-regions.json", `{
		"regionSize": 1920, "seedRegionId": "0x0000",
		"regions": [{"id": "0x6b4f", "bundlePublicPath": "/assets/world/outdoor/regions/region-6b4f.json"}]
	}`)
	const axis = 96
	heights := make([]float32, 97*97)
	blocked := make([]byte, axis*axis)
	cellIDs := make([]byte, axis*axis*4)
	blocked[blockedZ*axis+blockedX] = 1
	writeTestAsset(t, root, "assets/world/outdoor/regions/region-6b4f.json", fmt.Sprintf(`{
		"source": {"sectorId": "0x6b4f", "sectorX": 79, "sectorY": 107},
		"terrain": {"sectors": [{"sectorX": 79, "sectorY": 107, "blocks": []}]},
		"navmesh": {"regionSize": 1920, "tileSize": 20, "tilesPerAxis": 96, "heightMapAxisVertices": 97,
			"regions": [{"dx": 0, "dz": 0, "heightMap": %q, "blockedTiles": %q, "tileCellIds": %q, "cells": {"count": 1}}]}
	}`, encodeHeightMap(heights),
		base64.StdEncoding.EncodeToString(blocked),
		base64.StdEncoding.EncodeToString(cellIDs)))
	return root
}

/*
================
TestValidateMovementPathChecksBothCornerNeighbors
================
*/
func TestValidateMovementPathChecksBothCornerNeighbors(t *testing.T) {
	validator := NewWaterValidator(pinchRootAt(t, 4, 5))
	report := validator.ValidateMovementPath(
		spawnAt(0x6B4F, 50, 50),
		spawnAt(0x6B4F, 150, 150),
	)
	if report.Verdict != PathSegmentBlocked {
		t.Fatalf("upper corner pinch = %s (%+v), want segmentBlocked", report.Verdict, report)
	}
	wantX, wantZ := tile6B4F(4, 5)
	if report.BlockedTileX != wantX || report.BlockedTileZ != wantZ {
		t.Fatalf("upper blocked corner = (%d,%d), want (%d,%d)", report.BlockedTileX, report.BlockedTileZ, wantX, wantZ)
	}
}

/*
================
TestValidateMovementPathDiagonalCornerSqueeze

TestValidateMovementPathDiagonalCornerSqueeze pins the supercover corner
visit (GROK-V6 seq 297 soft note): the y=x chord from tile (2,2)=(50,50)
to (7,7)=(150,150) grazes tile (5,4)'s corner. A single-axis DDA never
enters (5,4); only the corner visit does. With the visit live the chord
classifies B; with it disabled the same chord reads legal (the witnessed
red).
================
*/
func TestValidateMovementPathDiagonalCornerSqueeze(t *testing.T) {
	validator := NewWaterValidator(pinchRoot(t))

	from := spawnAt(0x6B4F, 50, 50)
	to := spawnAt(0x6B4F, 150, 150)
	report := validator.ValidateMovementPath(from, to)
	if report.Verdict != PathSegmentBlocked {
		t.Fatalf("exact-diagonal chord grazing the corner tile = %s (%+v), want segmentBlocked", report.Verdict, report)
	}
	wantX, wantZ := tile6B4F(5, 4)
	if report.BlockedTileX != wantX || report.BlockedTileZ != wantZ {
		t.Errorf("first blocked corner tile = (%d,%d), want (%d,%d)", report.BlockedTileX, report.BlockedTileZ, wantX, wantZ)
	}

	// Sanity: with the pinch tiles open (a plain corner) the same chord is
	// legal - proving the verdict comes from the blocked corner, not the
	// diagonal traversal itself.
	openValidator := NewWaterValidator(syntheticHeightRoot(t))
	if r := openValidator.ValidateMovementPath(spawnAt(0x6B4F, 610, 610), spawnAt(0x6B4F, 710, 710)); r.Verdict != PathLegal {
		t.Fatalf("open-field diagonal must be legal, got %s", r.Verdict)
	}
}

/*
================
TestValidateMovementPathFailsOpenAcrossUncoveredNeighbor

TestValidateMovementPathFailsOpenAcrossUncoveredNeighbor: the chord walks
west out of the covered region into a neighbor with no bundle. Uncovered
tiles skip fail-open (never blocked), so the verdict stays legal with the
gap recorded.
================
*/
func TestValidateMovementPathFailsOpenAcrossUncoveredNeighbor(t *testing.T) {
	validator := NewWaterValidator(syntheticHeightRoot(t))

	report := validator.ValidateMovementPath(
		spawnAt(0x6B4F, 600, 600), spawnAt(0x6B4E, 1900, 600))
	if report.Verdict != PathLegal {
		t.Fatalf("verdict = %s, want legal (fail-open across the uncovered neighbor)", report.Verdict)
	}
	if report.TilesUncovered == 0 {
		t.Error("the walk crossed an uncovered region; TilesUncovered must record it")
	}
}

/*
================
TestValidateMovementPathTruncatesHostileLongChord

TestValidateMovementPathTruncatesHostileLongChord: a wire-legal but
absurd destination hundreds of regions away must not walk unbounded
inside the movement mutex. The truncated walk stays fail-open (legal
verdict, Truncated flag) - length policing is telemetry, not refusal.
================
*/
func TestValidateMovementPathTruncatesHostileLongChord(t *testing.T) {
	validator := NewWaterValidator(syntheticHeightRoot(t))

	report := validator.ValidateMovementPath(
		spawnAt(0x6B4F, 600, 600), spawnAt(0x0000, 100, 100))
	if !report.Truncated {
		t.Fatal("a ~150k-unit chord must truncate at the tile cap")
	}
	if report.Verdict != PathLegal {
		t.Fatalf("truncated verdict = %s, want legal (fail-open)", report.Verdict)
	}
	if report.TilesChecked > pathGuardMaxTiles+8 {
		t.Fatalf("tilesChecked = %d, cap %d not honored", report.TilesChecked, pathGuardMaxTiles)
	}
}

// ---- PathGuard mode semantics ----

/*
================
fakePathValidator
================
*/
type fakePathValidator struct{ report PathReport }

/*
================
fakePathValidator.ValidateMovementPath
================
*/
func (f fakePathValidator) ValidateMovementPath(_, _ simulation.Spawn) PathReport {
	return f.report
}

/*
================
TestPathGuardObserveNeverRefuses
================
*/
func TestPathGuardObserveNeverRefuses(t *testing.T) {
	for _, verdict := range []PathVerdict{
		PathLegal, PathEndpointBlocked, PathSegmentBlocked,
		PathStartBlocked, PathNoCoverage, PathDungeonExempt,
	} {
		guard := &PathGuard{Mode: PathGuardObserve, Validator: fakePathValidator{PathReport{Verdict: verdict}}}
		if refusal := guard.InspectMove("Asd", simulation.Spawn{}, simulation.Spawn{}); refusal != nil {
			t.Errorf("observe mode refused verdict %s: %v", verdict, refusal)
		}
		if got := guard.Stats().Refused; got != 0 {
			t.Errorf("observe mode counted %d refusals for %s", got, verdict)
		}
	}
}

/*
================
TestPathGuardEnforceFailsClosedWithoutCoverage
================
*/
func TestPathGuardEnforceFailsClosedWithoutCoverage(t *testing.T) {
	refusing := map[PathVerdict]bool{
		PathEndpointBlocked: true,
		PathNoCoverage:      true,
	}
	for _, verdict := range []PathVerdict{
		PathLegal, PathEndpointBlocked, PathSegmentBlocked,
		PathStartBlocked, PathNoCoverage, PathDungeonExempt,
	} {
		guard := &PathGuard{Mode: PathGuardEnforce, Validator: fakePathValidator{PathReport{Verdict: verdict}}}
		refusal := guard.InspectMove("Asd", simulation.Spawn{}, simulation.Spawn{})
		if refusing[verdict] && refusal == nil {
			t.Errorf("enforce mode must refuse %s", verdict)
		}
		if !refusing[verdict] && refusal != nil {
			// segmentBlocked especially: genuine play produces it under
			// Euclidean authority - refusing it is the bug factory the
			// COORD constraint exists to prevent.
			t.Errorf("enforce mode refused %s: %v", verdict, refusal)
		}
		if refusal != nil && refusal.NativeErrorCode != 0x02 {
			t.Errorf("refusal code = 0x%02X, want 0x02", refusal.NativeErrorCode)
		}
	}

	guard := &PathGuard{
		Mode:      PathGuardEnforce,
		Validator: fakePathValidator{PathReport{Verdict: PathLegal, TilesUncovered: 1}},
	}
	if refusal := guard.InspectMove("Asd", simulation.Spawn{}, simulation.Spawn{}); refusal == nil {
		t.Fatal("enforce mode accepted a partially uncovered chord")
	}
	guard = &PathGuard{
		Mode:      PathGuardEnforce,
		Validator: fakePathValidator{PathReport{Verdict: PathLegal, Truncated: true}},
	}
	if refusal := guard.InspectMove("Asd", simulation.Spawn{}, simulation.Spawn{}); refusal == nil {
		t.Fatal("enforce mode accepted a truncated chord scan")
	}
}

/*
================
TestPathGuardCountsVerdicts
================
*/
func TestPathGuardCountsVerdicts(t *testing.T) {
	guard := &PathGuard{Mode: PathGuardObserve, Validator: fakePathValidator{PathReport{Verdict: PathSegmentBlocked, Truncated: true}}}
	guard.InspectMove("Asd", simulation.Spawn{}, simulation.Spawn{})
	guard.InspectMove("Asd", simulation.Spawn{}, simulation.Spawn{})
	stats := guard.Stats()
	if stats.Inspected != 2 || stats.SegmentBlocked != 2 || stats.Truncated != 2 {
		t.Fatalf("stats = %+v, want inspected/segmentBlocked/truncated all 2", stats)
	}
	if stats.Legal != 0 || stats.EndpointBlocked != 0 || stats.Refused != 0 {
		t.Fatalf("stats = %+v, unrelated counters must stay 0", stats)
	}
}

/*
================
TestPathGuardModeFromEnv
================
*/
func TestPathGuardModeFromEnv(t *testing.T) {
	cases := []struct {
		value string
		want  PathGuardMode
	}{
		{"", PathGuardEnforce},
		{"observe", PathGuardObserve},
		{"off", PathGuardOff},
		{"enforce", PathGuardEnforce},
		{"ENFORCE", PathGuardEnforce},
		{"garbage", PathGuardEnforce},
	}
	for _, tc := range cases {
		t.Setenv(EnvMovePathGuard, tc.value)
		if got := PathGuardModeFromEnv(); got != tc.want {
			t.Errorf("%s=%q -> %s, want %s", EnvMovePathGuard, tc.value, got, tc.want)
		}
	}

	t.Setenv(EnvMovePathGuard, "off")
	if guard := NewPathGuardFromEnv(fakePathValidator{}); guard != nil {
		t.Error("off must build a nil guard")
	}
	t.Setenv(EnvMovePathGuard, "")
	guard := NewPathGuardFromEnv(fakePathValidator{})
	if guard == nil || guard.Mode != PathGuardEnforce {
		t.Fatalf("default guard = %+v, want enforce", guard)
	}
}

// ---- HandleMove integration ----

/*
================
TestHandleMovePathGuardObserveLogsButAccepts
================
*/
func TestHandleMovePathGuardObserveLogsButAccepts(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	guard := &PathGuard{Mode: PathGuardObserve, Validator: NewWaterValidator(syntheticHeightRoot(t))}
	rt.PathGuard = guard

	// Europe start (1205, 396) -> destination inside the blocked obstacle.
	outcome := rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, 110, 0, 110))
	if outcome.Refusal != nil {
		t.Fatalf("observe mode must not refuse: %v", outcome.Refusal)
	}
	if len(outcome.Frames) != 1 || outcome.Frames[0].Opcode != simulation.OpMovementAck {
		t.Fatalf("observe mode must still ack: frames %+v", outcome.Frames)
	}
	stats := guard.Stats()
	if stats.EndpointBlocked != 1 || stats.Refused != 0 {
		t.Fatalf("stats = %+v, want endpointBlocked 1 / refused 0", stats)
	}
}

/*
================
TestHandleMovePathGuardEnforceRefusesEndpointNotSegment
================
*/
func TestHandleMovePathGuardEnforceRefusesEndpointNotSegment(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	guard := &PathGuard{Mode: PathGuardEnforce, Validator: NewWaterValidator(syntheticHeightRoot(t))}
	rt.PathGuard = guard

	// Category A: endpoint inside the obstacle -> refused, silent wire.
	outcome := rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, 110, 0, 110))
	if outcome.Refusal == nil {
		t.Fatal("enforce mode must refuse an endpoint-blocked move")
	}
	if len(outcome.Frames) != 0 {
		t.Fatalf("a refused move ships no packets, got %+v", outcome.Frames)
	}

	// Category B: straight chord from the Europe start (tile 60,19) north
	// across the moat rows (60,58)/(60,59) to the open field beyond the
	// island (tile 60,70). Endpoint walkable, chord blocked - and even
	// enforce mode MUST accept it (Euclidean-authority asymmetry).
	outcome = rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, 1210, 0, 1400))
	if outcome.Refusal != nil {
		t.Fatalf("enforce mode must NOT refuse a segment-blocked move: %v", outcome.Refusal)
	}
	if len(outcome.Frames) != 1 {
		t.Fatalf("segment-blocked move must still ack, frames %+v", outcome.Frames)
	}

	stats := guard.Stats()
	if stats.EndpointBlocked != 1 || stats.SegmentBlocked != 1 || stats.Refused != 1 {
		t.Fatalf("stats = %+v, want endpointBlocked 1 / segmentBlocked 1 / refused 1", stats)
	}
}

/*
================
TestHandleMoveAngularSkipsPathGuard
================
*/
func TestHandleMoveAngularSkipsPathGuard(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	guard := &PathGuard{Mode: PathGuardEnforce, Validator: NewWaterValidator(syntheticHeightRoot(t))}
	rt.PathGuard = guard

	// 4-byte angular body without GO: [u8 0][u8 angularMode][u16 headingWord].
	// A turn in place has no chord; the GO form's leg is inspected after the
	// clip (direction_test.go).
	outcome := rt.HandleMove("0", character, []byte{0, 0, 0x34, 0x12})
	if outcome.Refusal != nil {
		t.Fatalf("angular move refused: %v", outcome.Refusal)
	}
	if got := guard.Stats().Inspected; got != 0 {
		t.Fatalf("angular form must never reach the path guard, inspected = %d", got)
	}
}

/*
================
TestHandleMoveWithoutPathGuardUnchanged

TestHandleMoveWithoutPathGuardUnchanged pins that a nil guard leaves the
pre-lane accept path byte-identical (regression guard for every existing
move consumer).
================
*/
func TestHandleMoveWithoutPathGuardUnchanged(t *testing.T) {
	character := testCharacter()
	rt := testRuntime(character)
	outcome := rt.HandleMove("0", character, encodeMoveBody(1, 0x6B4F, 110, 0, 110))
	if outcome.Refusal != nil || len(outcome.Frames) != 1 {
		t.Fatalf("nil guard must accept exactly as before: %+v", outcome)
	}
}

/*
================
TestPathGuardFalsePositiveQuantification

TestPathGuardFalsePositiveQuantification sweeps REAL exported bundles
with stock-like clicks (lattice departures, 8 directions, 200-unit
view-range chords) and reports the verdict distribution. The number that
matters is the category-B rate among walkable-endpoint chords: that is
the geometry-derived floor of the false-positive rate a segment-enforcing
gate would inflict on genuine play (the client paths AROUND the walls
these chords cut through), and the reason B stays observe-only. Real-play
rates come from the deployed observe telemetry; this pins the mechanism
and proves enforce accepts every B chord.
================
*/
func TestPathGuardFalsePositiveQuantification(t *testing.T) {
	validator := realAuthorityValidator(t)
	enforce := &PathGuard{Mode: PathGuardEnforce, Validator: validator}

	directions := [8][2]float64{
		{1, 0}, {-1, 0}, {0, 1}, {0, -1},
		{0.7071, 0.7071}, {0.7071, -0.7071}, {-0.7071, 0.7071}, {-0.7071, -0.7071},
	}
	const clickDistance = 200.0

	for _, region := range []uint16{0x6B4F, 0x60A0} {
		var legal, segment, endpoint, start, noCover int
		for ox := 100.0; ox < 1920; ox += 160 {
			for oz := 100.0; oz < 1920; oz += 160 {
				for _, dir := range directions {
					from := spawnAt(region, ox, oz)
					to := simulation.NormalizeSpawnFrame(spawnAt(region, ox+dir[0]*clickDistance, oz+dir[1]*clickDistance))
					report := validator.ValidateMovementPath(from, to)
					switch report.Verdict {
					case PathLegal:
						legal++
					case PathSegmentBlocked:
						segment++
						// The load-bearing assertion: enforce must ACCEPT
						// every category-B chord.
						if refusal := enforce.InspectMove("sweep", from, to); refusal != nil {
							t.Fatalf("enforce refused a segment-blocked chord %+v -> %+v: %v", from, to, refusal)
						}
					case PathEndpointBlocked:
						endpoint++
					case PathStartBlocked:
						start++
					case PathNoCoverage:
						noCover++
					}
				}
			}
		}
		walkableEndpoints := legal + segment
		bRate := 0.0
		if walkableEndpoints > 0 {
			bRate = float64(segment) / float64(walkableEndpoints) * 100
		}
		t.Logf("region 0x%04X: legal=%d segmentBlocked=%d (B-rate %.1f%% of walkable-endpoint chords) endpointBlocked=%d startBlocked=%d noCoverage=%d",
			region, legal, segment, bRate, endpoint, start, noCover)
		if region == 0x60A0 && segment == 0 {
			t.Error("the mountain region must produce category-B chords (walls exist); a zero count means the chord walk is not running")
		}
	}
}

/*
================
BenchmarkValidateMovementPathWarm

BenchmarkValidateMovementPathWarm pins the hot-path cost of one chord
classification with the bundle cache warm (the state every accept after
the first sees). The walk runs inside the movement mutex, so this number
bounds the per-move overhead the guard adds.
================
*/
func BenchmarkValidateMovementPathWarm(b *testing.B) {
	root := filepath.Join("..", "..", "..", "..", "..", "..", ".generated", "client-public")
	if _, err := os.Stat(filepath.Join(root, "assets", "world", "world-region-catalog.json")); err != nil {
		b.Skipf("real client assets not present (%v)", err)
	}
	validator := NewWaterValidator(root)
	from := spawnAt(0x6B4F, 1205, 396)
	to := spawnAt(0x6B4F, 1150, 450)
	validator.ValidateMovementPath(from, to) // warm the cache
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		validator.ValidateMovementPath(from, to)
	}
}

// ---- object-nav awareness (the bridge-deck category-A FP kill) ----

/*
================
TestValidateMovementPathObjectDeckOverrides

TestValidateMovementPathObjectDeckOverrides drives the guard over the
syntheticObjectNavRoot world (objectnav_test.go): an OPEN 40x40 deck at
(500, 500) y=10 standing over BLOCKED tiles x,z in [24,25]. Deck moves
must classify by the deck plane, while a ground-level click onto the
same blocked tiles keeps its category-A verdict (the enforcement teeth
stay).
================
*/
func TestValidateMovementPathObjectDeckOverrides(t *testing.T) {
	validator := NewWaterValidator(syntheticObjectNavRoot(t))

	deckSpawn := func(x, z float64) simulation.Spawn {
		return simulation.Spawn{RegionID: 0x6B4F, X: x, Y: 10, Z: z}
	}

	// Deck -> deck entirely over blocked seabed-shaped tiles: LEGAL, with
	// the departure, endpoint and interior probes all overridden.
	report := validator.ValidateMovementPath(deckSpawn(490, 490), deckSpawn(510, 510))
	if report.Verdict != PathLegal {
		t.Fatalf("deck->deck = %s (%+v), want legal", report.Verdict, report)
	}
	if report.ObjectDeckOverrides < 3 {
		t.Fatalf("deck->deck overrides = %d, want >= 3 (start+end+interior)", report.ObjectDeckOverrides)
	}

	// Shore -> deck across the deck's 0x3 rails: a click ON the deck is a
	// producible stock pick (not category A), but the terrain walker cannot
	// cross a rail from outside (side-block bit 0x01), so it never owns the
	// deck and the chord crosses the blocked ground under it. Native walks
	// stop at the rail: segmentBlocked (telemetry only, never refused). The
	// old "legal" verdict came from guessing ownership from chord height.
	report = validator.ValidateMovementPath(
		simulation.Spawn{RegionID: 0x6B4F, X: 470, Y: 5, Z: 470}, deckSpawn(505, 500))
	if report.Verdict != PathSegmentBlocked {
		t.Fatalf("shore->deck through a rail = %s (%+v), want segmentBlocked", report.Verdict, report)
	}
	if report.ObjectDeckOverrides == 0 {
		t.Fatal("shore->deck must admit the deck click at the endpoint probe")
	}
	// Through the deck's OPEN east outline edge the terrain walker enters the
	// deck (outline entry, navowner.go) and the chord is legal.
	report = validator.ValidateMovementPath(
		simulation.Spawn{RegionID: 0x6B4F, X: 540, Y: 0, Z: 500}, deckSpawn(505, 500))
	if report.Verdict != PathLegal {
		t.Fatalf("shore->deck through the open edge = %s (%+v), want legal", report.Verdict, report)
	}

	// A GROUND-LEVEL click onto the same blocked tile is still category A:
	// the override requires the destination height meaningfully above the
	// terrain plane, which a stock ground pick cannot produce there.
	report = validator.ValidateMovementPath(
		spawnAt(0x6B4F, 450, 450), spawnAt(0x6B4F, 490, 490))
	if report.Verdict != PathEndpointBlocked {
		t.Fatalf("ground click into blocked tile = %s (%+v), want endpointBlocked", report.Verdict, report)
	}
	wantX, wantZ := tile6B4F(24, 24)
	if report.BlockedTileX != wantX || report.BlockedTileZ != wantZ {
		t.Errorf("blocked tile = (%d,%d), want (%d,%d)", report.BlockedTileX, report.BlockedTileZ, wantX, wantZ)
	}

	// Enforce-mode witnesses of both sides of the boundary.
	guard := &PathGuard{Mode: PathGuardEnforce, Validator: validator}
	if refusal := guard.InspectMove("deck", deckSpawn(490, 490), deckSpawn(510, 510)); refusal != nil {
		t.Fatalf("enforce refused a legal deck move: %v", refusal)
	}
	if refusal := guard.InspectMove("ground", spawnAt(0x6B4F, 450, 450), spawnAt(0x6B4F, 490, 490)); refusal == nil {
		t.Fatal("enforce must still refuse the ground-level category-A click")
	}
	if stats := guard.Stats(); stats.ObjectDeckOverrides == 0 {
		t.Errorf("stats = %+v, want ObjectDeckOverrides > 0", stats)
	}
}

/*
================
TestValidateMovementPathObjectDataMissingFailsOpen

TestValidateMovementPathObjectDataMissingFailsOpen corrupts the object
resource index: deck moves must degrade to the OLD tile-only verdicts
(never a refusal beyond what the tile plane already said, never a
fault).
================
*/
func TestValidateMovementPathObjectDataMissingFailsOpen(t *testing.T) {
	root := syntheticObjectNavRoot(t)
	writeTestAsset(t, root, "assets/world/outdoor/object-resources.json", `not json`)
	validator := NewWaterValidator(root)

	report := validator.ValidateMovementPath(
		simulation.Spawn{RegionID: 0x6B4F, X: 490, Y: 10, Z: 490},
		simulation.Spawn{RegionID: 0x6B4F, X: 510, Y: 10, Z: 510})
	if report.Verdict != PathStartBlocked {
		t.Fatalf("verdict without object data = %s, want the old startBlocked", report.Verdict)
	}
	if report.ObjectDeckOverrides != 0 {
		t.Fatalf("overrides = %d without object data, want 0", report.ObjectDeckOverrides)
	}
}

/*
================
TestValidateMovementPathRealHarborBridgeDeck

TestValidateMovementPathRealHarborBridgeDeck is THE category-A
false-positive kill, pinned on the REAL Constantinople harbor bridge
payload (euro_esteuro_port01, asset 1630, region 0x6850): the deck
plane sits ~105u above a BLOCKED seabed tile (verified blocked here, so
the FP precondition still holds), and a legal deck move over it must
classify LEGAL - before object awareness it classified endpointBlocked,
the one verdict enforce refuses. A click walking OFF the deck's end
onto the seabed keeps its blocked classification (the override's
boundary).
================
*/
func TestValidateMovementPathRealHarborBridgeDeck(t *testing.T) {
	validator := realAuthorityValidator(t)

	deck := simulation.Spawn{RegionID: 0x6850, X: 1263, Y: -25.17, Z: 1490.5}

	// The FP precondition: the tile plane under the deck point is BLOCKED.
	tile := globalTile{
		x: simulation.SectorX(deck.RegionID)*96 + int(deck.X/20),
		z: simulation.SectorY(deck.RegionID)*96 + int(deck.Z/20),
	}
	if walkable, ok := validator.globalTileWalkable(tile, 96); !ok || walkable {
		t.Fatalf("bridge tile (%d,%d) walkable=%v ok=%v, want a blocked covered tile (the FP precondition)",
			tile.x, tile.z, walkable, ok)
	}

	// Legal deck moves in three on-deck directions: LEGAL with overrides.
	guard := &PathGuard{Mode: PathGuardEnforce, Validator: validator}
	for _, d := range [][2]float64{{30, 0}, {-30, 0}, {0, 30}} {
		to := simulation.NormalizeSpawnFrame(simulation.Spawn{RegionID: deck.RegionID, X: deck.X + d[0], Y: deck.Y, Z: deck.Z + d[1]})
		report := validator.ValidateMovementPath(deck, to)
		if report.Verdict != PathLegal {
			t.Fatalf("bridge deck move (%+.0f,%+.0f) = %s (%+v), want legal (the dead FP)", d[0], d[1], report.Verdict, report)
		}
		if report.ObjectDeckOverrides == 0 {
			t.Fatalf("bridge deck move (%+.0f,%+.0f): no deck overrides recorded", d[0], d[1])
		}
		if refusal := guard.InspectMove("bridge", deck, to); refusal != nil {
			t.Fatalf("enforce refused the legal bridge move (%+.0f,%+.0f): %v", d[0], d[1], refusal)
		}
	}

	// Walking off the deck's south end onto bare blocked seabed stays
	// blocked-classified: the override needs a deck under the point.
	off := simulation.NormalizeSpawnFrame(simulation.Spawn{RegionID: deck.RegionID, X: deck.X, Y: deck.Y, Z: deck.Z - 30})
	if report := validator.ValidateMovementPath(deck, off); report.Verdict != PathEndpointBlocked {
		t.Fatalf("off-deck seabed endpoint = %s, want endpointBlocked (override boundary)", report.Verdict)
	}
}

/*
================
TestValidateMovementPathRealEuropeBridgeDeckIncident

TestValidateMovementPathRealEuropeBridgeDeckIncident pins the 2026-08-17
browser/server disagreement at region 0x6C4F. The browser's reconstructed
native picker selected an object-nav deck at this exact endpoint and
ValidateMove accepted it; GameWorld must therefore recognize the same deck
instead of silently refusing the 0x7738 as endpointBlocked.
================
*/
func TestValidateMovementPathRealEuropeBridgeDeckIncident(t *testing.T) {
	validator := realAuthorityValidator(t)
	from := simulation.Spawn{RegionID: 0x6C4F, X: 1249, Y: 2.324, Z: 201}
	to := simulation.Spawn{RegionID: 0x6C4F, X: 1241.134, Y: 9.567, Z: 129.219}

	if refusal := validator.ValidateMovement(moveTo(to.RegionID, to.X, to.Y, to.Z)); refusal != nil {
		t.Fatalf("browser-accepted bridge endpoint failed the water gate: %v", refusal)
	}
	report := validator.ValidateMovementPath(from, to)
	if report.Verdict != PathLegal {
		t.Fatalf("browser-accepted bridge move = %s (%+v), want legal", report.Verdict, report)
	}
	if report.ObjectDeckOverrides == 0 {
		t.Fatalf("browser-accepted bridge move did not exercise object-nav deck ownership: %+v", report)
	}
	guard := &PathGuard{Mode: PathGuardEnforce, Validator: validator}
	if refusal := guard.InspectMove("Test2", from, to); refusal != nil {
		t.Fatalf("enforce refused the browser-accepted bridge move: %v", refusal)
	}
}

/*
================
TestValidateMovementPathRealAssets

TestValidateMovementPathRealAssets drives the guard over the REAL
exported bundles at points with known walkability: the Europe start
plateau (every enter-world click must classify legal - THE false-positive
floor) and the 0x60A0 blocked mountain-face tile from the live stranded
incident (a destination the stock client could never compose).
================
*/
func TestValidateMovementPathRealAssets(t *testing.T) {
	validator := realAuthorityValidator(t)

	// Legal: a plain view-range click on the Europe start plateau.
	report := validator.ValidateMovementPath(
		spawnAt(0x6B4F, 1205, 396), spawnAt(0x6B4F, 1150, 450))
	if report.Verdict != PathLegal {
		t.Fatalf("Europe start click = %s (%+v), want legal", report.Verdict, report)
	}

	// Category A: the incident's blocked mountain-face point as a
	// DESTINATION. The departure is that point's own spawn-rescue target -
	// mainland-walkable by construction (RelocateStrandedSpawn pins the
	// face tile blocked in TestRelocateStrandedSpawnRealMountain), so the
	// endpoint classification cannot be masked by a blocked start.
	face := spawnAt(0x60A0, 1842.786, 1279.231)
	rescue, stranded, found := validator.RelocateStrandedSpawn(face)
	if !stranded || !found {
		t.Fatalf("mountain face must be stranded with a rescue: stranded=%v found=%v", stranded, found)
	}
	report = validator.ValidateMovementPath(rescue, face)
	if report.Verdict != PathEndpointBlocked {
		t.Fatalf("mountain-face destination = %s (%+v), want endpointBlocked", report.Verdict, report)
	}
}
