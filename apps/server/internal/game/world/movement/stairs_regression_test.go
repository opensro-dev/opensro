package movement

import (
	"math"
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/simulation"
)

// Published v1.150 Constantinople geometry: the top stair deck lies BELOW
// the heightmap. Login repair must compare both surfaces against saved Y.
func TestRealConstantinopleStairLoginAndDescent(t *testing.T) {
	v := realAuthorityValidator(t)
	from := simulation.Spawn{RegionID: 0x6046, X: 920, Y: 1124.1568906758841, Z: 610}
	to := simulation.Spawn{RegionID: 0x6046, X: 920, Y: 1090.5145930961428, Z: 670}
	terrain, ok := v.TerrainHeightAt(from.RegionID, from.X, from.Z)
	if !ok || math.Abs(terrain-1126.6672973632812) > .01 {
		t.Fatalf("terrain fixture changed: %v %v", terrain, ok)
	}
	entry := enterworld.LocalPlayerEntry{StartProfile: enterworld.StartProfile{RegionID: int64(from.RegionID), X: from.X, Y: from.Y, Z: from.Z}}
	if enterworld.LiftSpawnAboveTerrain(&entry, v.TerrainHeightAt, v.WalkableSpawnHeightAt) || entry.StartProfile.Y != from.Y {
		t.Fatalf("valid stair stand lifted: %+v", entry.StartProfile)
	}
	path := v.objectSurfacePath(from, to)
	y, owned := path.heightAt(1. / 6)
	if !owned || math.Abs(y-(from.Y+(to.Y-from.Y)/6)) < 2 {
		t.Fatalf("fixture must discriminate cell plane from endpoint chord: %v %v", y, owned)
	}
	terrainStart := from
	terrainStart.Y = terrain
	if v.objectSurfacePath(terrainStart, to) != nil {
		t.Fatal("terrain-owned point must not acquire an object merely from shared XZ")
	}
	landing := from
	landing.Z = 580
	landing.Y, _ = v.TerrainHeightAt(landing.RegionID, landing.X, landing.Z)
	for _, pair := range [][2]simulation.Spawn{{from, to}, {to, from}, {terrainStart, landing}, {landing, from}} {
		clip := v.ClipMovementPath(pair[0], pair[1])
		if clip.Outcome != ClipArrived || math.Abs(clip.Rest.Z-pair[1].Z) > .01 {
			t.Errorf("stair clip: %+v", clip)
		}
		guard := v.ValidateMovementPath(pair[0], pair[1])
		if guard.Verdict != PathLegal {
			t.Errorf("stair pathguard: %+v", guard)
		}
	}
}

func TestOwnedCellCollisionUsesTopologyNotChordHeight(t *testing.T) {
	mesh := &objectNavMesh{vertices: []float32{0, 20, 0, 0, 20, 100, 100, 20, 0}, cellA: []uint16{0}, cellB: []uint16{1}, cellC: []uint16{2}, minX: 0, maxX: 100, minY: 20, maxY: 20, minZ: 0, maxZ: 100,
		outline: objectNavEdges{vertA: []uint16{1}, vertB: []uint16{2}, srcCell: []uint16{0}, dstCell: []uint16{65535}, flags: []byte{2}}}
	path := traceObjectCells(&objectDeckStand{mesh: mesh, cellIndex: 0}, 10, 10, 80, 80)
	if _, owns := path.cellAt(.9); owns {
		t.Fatal("ownership survived leaving the mesh")
	}
	for _, tc := range []struct {
		name    string
		opts    objectContactOptions
		blocked bool
	}{
		{"terrain under deck", objectContactOptions{}, false},
		{"owned deck despite chord drift", objectContactOptions{path: path, ownedMesh: true}, true},
		{"different overlapping deck", objectContactOptions{path: path}, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var point objectContactPoint
			_, blocked := objectMeshChordContactDetail(mesh, 10, 10, 0, 80, 80, 0, 1, nil, &point, tc.opts)
			if blocked != tc.blocked {
				t.Fatalf("blocked=%v want %v", blocked, tc.blocked)
			}
			if blocked && math.Abs(point.y-20) > .01 {
				t.Fatalf("contact did not project owned cell: %+v", point)
			}
		})
	}
}

// Published v1.150 Hotan gate deck (region 0x5B87): the deck plane is 243.99
// while the terrain under its south edge slopes from 243.04 (z=1300) down to
// 226 (z=1290). A player walking the deck reaches (1122, *, 1300.01) and the
// wire truncates the height to 243. Re-guessing the surface from that y picks
// the terrain (|243.04-243| < |243.99-243|), whose south slope is a blocked
// cliff: the 2026-09-23 "invisible wall" at X:113 Y:-62. The native server
// keeps the source cell (its movement query), so the walk continues on
// the deck regardless of the quantized height.
func TestRealHotanGateDeckRetainsOwnerAcrossTruncatedHeight(t *testing.T) {
	v := realAuthorityValidator(t)
	const region = uint16(0x5B87)
	onDeck := simulation.Spawn{RegionID: region, X: 1122, Y: 243.99, Z: 1330}
	deck, deckY, ok := v.ResolveNavOwner(onDeck, simulation.NavOwner{})
	if !ok || deck.Kind != simulation.NavOwnerObject || math.Abs(deckY-243.99) > .05 {
		t.Fatalf("fixture changed: deck owner %+v y=%v ok=%v", deck, deckY, ok)
	}

	// The wall position with the wire-truncated height.
	wall := simulation.Spawn{RegionID: region, X: 1122, Y: 243, Z: 1300.01}
	if guessed, _, _ := v.ResolveNavOwner(wall, simulation.NavOwner{}); guessed.Kind != simulation.NavOwnerTerrain {
		t.Fatalf("fixture no longer reproduces the height-guess trap: %+v", guessed)
	}
	south := simulation.Spawn{RegionID: region, X: 1129, Y: 244, Z: 1173}
	// Negative control: without the retained owner the same move still hits
	// the invisible wall, which is exactly the reported bug.
	if trap := v.ClipMovementPath(wall, south); trap.Outcome != ClipBlocked || math.Abs(trap.Rest.Z-1300.01) > .05 {
		t.Fatalf("negative control no longer reproduces the wall: %+v", trap)
	}

	// Walking there from the deck carries the deck owner to the wall point.
	approach := v.ClipMovementPathFrom(onDeck, deck, wall)
	if approach.Outcome != ClipArrived || approach.RestOwner.Kind != simulation.NavOwnerObject {
		t.Fatalf("deck approach lost the deck: %+v", approach)
	}
	if math.Abs(approach.Rest.Y-243.99) > .05 {
		t.Fatalf("rest height must be the deck plane, got %v", approach.Rest.Y)
	}

	// From the wall with the retained owner the walk continues south, even
	// with the truncated height the wire delivers.
	for _, from := range []simulation.Spawn{wall, approach.Rest} {
		clip := v.ClipMovementPathFrom(from, approach.RestOwner, south)
		if clip.Outcome != ClipArrived || math.Abs(clip.Rest.Z-south.Z) > .01 {
			t.Fatalf("retained deck owner still clipped at the cliff: from %+v -> %+v", from, clip)
		}
		if guard := v.ValidateMovementPathFrom(from, approach.RestOwner, south); guard.Verdict != PathLegal {
			t.Fatalf("pathguard with retained owner: %+v", guard)
		}
	}
}

// End to end through the 0x7738 handler on the published Hotan gate data: a
// character walks the deck to the wall point (the wire truncates its height to
// 243), then clicks south. The committed move must stand on the deck plane and
// carry the deck owner, so the second click is not clipped at the terrain
// cliff under the deck edge. (Before the fix the committed goal kept the
// request's y=243 and the next move re-guessed the terrain; the unit test
// above pins that trap as a negative control.)
func TestRealHotanGateDeckWalkThroughHandleMove(t *testing.T) {
	v := realAuthorityValidator(t)
	run := func(withOwnership bool) simulation.WorldState {
		character := testCharacter()
		regionID := int64(0x5B87)
		x, y, z := 1122.0, 243.99, 1330.0
		angle := int64(0)
		character.World = &enterworld.CharacterWorld{
			Spawn:    &enterworld.WorldSpawn{RegionID: &regionID, X: &x, Y: &y, Z: &z, Angle: &angle},
			SpawnSet: true,
		}
		rt := testRuntime(character)
		rt.ClientClip = &ClientClip{Mode: ClipApply, Validator: v}
		rt.PathGuard = &PathGuard{Mode: PathGuardEnforce, Validator: v}
		if withOwnership {
			rt.Nav = v
		}
		nowMs := testStartMs
		rt.Now = func() time.Time { return time.UnixMilli(nowMs) }
		key := simulation.WorldKey("0", character.Name)
		settle := func() simulation.WorldState {
			world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
			if world.MoveSegment != nil {
				nowMs = world.MoveSegment.ArrivesAtMs + 1
			}
			return world
		}
		if out := rt.HandleMove("0", character, encodeMoveBody(1, 0x5B87, 1122, 243, 1300)); out.Refusal != nil {
			t.Fatalf("approach refused: %v", out.Refusal)
		}
		settle()
		if out := rt.HandleMove("0", character, encodeMoveBody(1, 0x5B87, 1129, 244, 1173)); out.Refusal != nil {
			t.Fatalf("south move refused: %v", out.Refusal)
		}
		return settle()
	}

	fixed := run(true)
	if math.Abs(fixed.Spawn.Z-1173) > .01 || math.Abs(fixed.Spawn.X-1129) > .01 {
		t.Fatalf("retained deck owner still clipped: %+v", fixed.Spawn)
	}
	if owner := fixed.GoalOwner(); owner.Kind != simulation.NavOwnerObject {
		t.Fatalf("committed goal lost its deck owner: %+v", owner)
	}
	if math.Abs(fixed.Spawn.Y-244.39) > .1 {
		t.Fatalf("committed goal must stand on the deck plane, got y=%v", fixed.Spawn.Y)
	}
}
