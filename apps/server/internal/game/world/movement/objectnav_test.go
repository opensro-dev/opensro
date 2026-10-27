package movement

import (
	"encoding/base64"
	"encoding/binary"
	"fmt"
	"math"
	"testing"

	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
)

// ---- synthetic object-nav payload building (wire grammar of
// nativeObjectNavPayload.ts / client sub_4265b0, navFlags = 0) ----

type testNavEdge struct {
	vA, vB, src, dst uint16
	flags            byte
}

// encodeObjectNavPayload packs a payload the way the BMS offset-7 tail
// carries it: vertices (13B), cells (8B), outline then internal edge
// groups (9B), and a 1x1 outline-edge grid tail with no refs.
func encodeObjectNavPayload(vertices [][3]float32, cells [][3]uint16, outline, internal []testNavEdge) []byte {
	raw := make([]byte, 0, 256)
	u32 := func(v uint32) {
		raw = binary.LittleEndian.AppendUint32(raw, v)
	}
	u16 := func(v uint16) {
		raw = binary.LittleEndian.AppendUint16(raw, v)
	}
	f32 := func(v float32) {
		raw = binary.LittleEndian.AppendUint32(raw, math.Float32bits(v))
	}

	u32(uint32(len(vertices)))
	for _, vertex := range vertices {
		f32(vertex[0])
		f32(vertex[1])
		f32(vertex[2])
		raw = append(raw, 0) // boundary-direction index 0: (+1,0)
	}
	u32(uint32(len(cells)))
	for _, cell := range cells {
		u16(cell[0])
		u16(cell[1])
		u16(cell[2])
		u16(0) // eventZoneWord
	}
	for _, group := range [][]testNavEdge{outline, internal} {
		u32(uint32(len(group)))
		for _, edge := range group {
			u16(edge.vA)
			u16(edge.vB)
			u16(edge.src)
			u16(edge.dst)
			raw = append(raw, edge.flags)
		}
	}
	// Grid tail: f32 originX/Z, i32 countX/Z, u32 tileCount, then one
	// tile with zero refs.
	f32(-20)
	f32(-20)
	u32(1)
	u32(1)
	u32(1)
	u32(0)
	return raw
}

// deckPayload is a 40x40 elevated square deck (object-local y = 0, two
// triangles joined by one open internal edge). openExit opens ONE outline
// edge (flags 0x0); otherwise every perimeter edge is a sealed rail
// (flags 0x3, the Jangan walkway west-rim shape).
func deckPayload(openExit bool) []byte {
	vertices := [][3]float32{{-20, 0, -20}, {20, 0, -20}, {20, 0, 20}, {-20, 0, 20}}
	cells := [][3]uint16{{0, 1, 2}, {0, 2, 3}}
	exitFlags := byte(0x3)
	if openExit {
		exitFlags = 0x0
	}
	outline := []testNavEdge{
		{0, 1, 0, 0xffff, 0x3},
		{1, 2, 0, 0xffff, exitFlags},
		{2, 3, 1, 0xffff, 0x3},
		{3, 0, 1, 0xffff, 0x3},
	}
	internal := []testNavEdge{{0, 2, 0, 1, 0x0}}
	return encodeObjectNavPayload(vertices, cells, outline, internal)
}

// meshJSON wraps a payload in the mesh resource JSON shape the loader
// reads (headerOffsets[7] = payload offset, headerOffsets[11] = navFlags).
func meshJSON(payload []byte) string {
	const navOffset = 100
	return fmt.Sprintf(`{
		"format": "sro-world-object-mesh",
		"mesh": {
			"byteLength": %d,
			"headerOffsets": [0, 0, 0, 0, 0, 0, 0, %d, 0, 0, 0, 0],
			"nativePayloads": [{
				"kind": "bms-offset7-post-payload-tail",
				"byteOffset": %d,
				"byteLength": %d,
				"rawBase64": %q
			}]
		}
	}`, navOffset+len(payload), navOffset, navOffset, len(payload), base64.StdEncoding.EncodeToString(payload))
}

// syntheticObjectNavRoot builds a region 0x6B4F world (flat terrain at
// y=0, every tile walkable) carrying three 40x40 decks at y=10:
//
//   - asset 500 at (100, 100): SEALED (every outline edge a 0x3 rail) -
//     the Jangan walkway trap shape;
//   - asset 501 at (500, 500): one OPEN outline exit - a legal elevated
//     stand (bridge deck shape);
//   - asset 502 at (900, 900): sealed payload with an unresolved object
//     link, which authority treats as blocked;
//   - asset 503 anchored in the eastern neighbor with x=-10: its mesh
//     overhangs into this region and exercises the spatial search.
func syntheticObjectNavRoot(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	writeTestAsset(t, root, "assets/world/world-region-catalog.json", `{
		"regionsById": {
			"0x6b4f": [{
				"id": "0x6b4f",
				"seedRegionId": "0x6b4f",
				"worldRegionsPublicPath": "/assets/world/outdoor/world-regions.json",
				"bundlePublicPath": "/assets/world/outdoor/regions/region-6b4f.json"
			}]
		}
	}`)
	writeTestAsset(t, root, "assets/world/outdoor/world-regions.json", `{
		"regionSize": 1920,
		"seedRegionId": "0x0000",
		"regions": [
			{"id": "0x6b4f", "bundlePublicPath": "/assets/world/outdoor/regions/region-6b4f.json"}
		]
	}`)

	const axis = 96
	heightMap := encodeHeightMap(make([]float32, 97*97))
	// The tiles under the OPEN deck (tiles 24..25 around x/z 500) are
	// BLOCKED - the bridge-over-seabed shape. The open object lane must
	// override that tile verdict (the pre-object rescue stranded it).
	blocked := make([]byte, axis*axis)
	for z := 24; z <= 25; z++ {
		for x := 24; x <= 25; x++ {
			blocked[z*axis+x] = 1
		}
	}
	writeTestAsset(t, root, "assets/world/outdoor/regions/region-6b4f.json", fmt.Sprintf(`{
		"source": {"sectorId": "0x6b4f", "sectorX": 79, "sectorY": 107},
		"terrain": {"sectors": [{"sectorX": 79, "sectorY": 107, "blocks": []}]},
		"objects": {"resourceIndexPublicPath": "/assets/world/outdoor/object-resources.json"},
		"navmesh": {
			"regionSize": 1920,
			"tileSize": 20,
			"tilesPerAxis": 96,
			"heightMapAxisVertices": 97,
			"regions": [{
				"dx": 0, "dz": 0, "regionId": 27471,
				"heightMap": %q, "blockedTiles": %q, "tileCellIds": %q,
				"cells": {"count": 1},
				"objects": [
					{"assetId": 500, "x": 100, "y": 10, "z": 100, "yaw": 0, "linkEdgeCount": 0},
					{"assetId": 501, "x": 500, "y": 10, "z": 500, "yaw": 0, "linkEdgeCount": 0},
					{"assetId": 502, "x": 900, "y": 10, "z": 900, "yaw": 0, "linkEdgeCount": 1}
				]
			}, {
				"dx": 1, "dz": 0, "regionId": 27472,
				"objects": [
					{"assetId": 503, "x": -10, "y": 10, "z": 100, "yaw": 0, "linkEdgeCount": 0}
				]
			}]
		}
	}`,
		heightMap,
		base64.StdEncoding.EncodeToString(blocked),
		base64.StdEncoding.EncodeToString(make([]byte, axis*axis*4))))

	writeTestAsset(t, root, "assets/world/outdoor/object-resources.json", `{
		"bsr": [
			{"objectId": 500, "renderMeshSection": {"paths": ["prim/mesh/test/sealed_deck.bms"]}},
			{"objectId": 501, "renderMeshSection": {"paths": ["prim/mesh/test/open_deck.bms"]}},
			{"objectId": 502, "renderMeshSection": {"paths": ["prim/mesh/test/sealed_deck.bms"]}},
			{"objectId": 503, "renderMeshSection": {"paths": ["prim/mesh/test/sealed_deck.bms"]}}
		],
		"meshFiles": [
			{"sourcePath": "prim/mesh/test/sealed_deck.bms", "publicPath": "/assets/world/outdoor/object-meshes/sealed_deck.json"},
			{"sourcePath": "prim/mesh/test/open_deck.bms", "publicPath": "/assets/world/outdoor/object-meshes/open_deck.json"}
		]
	}`)
	writeTestAsset(t, root, "assets/world/outdoor/object-meshes/sealed_deck.json", meshJSON(deckPayload(false)))
	writeTestAsset(t, root, "assets/world/outdoor/object-meshes/open_deck.json", meshJSON(deckPayload(true)))
	return root
}

func TestDecodeObjectNavPayload(t *testing.T) {
	mesh, err := decodeObjectNavPayload(deckPayload(false), 0)
	if err != nil {
		t.Fatalf("synthetic payload must decode: %v", err)
	}
	if mesh.cellCount() != 2 || len(mesh.vertices) != 12 {
		t.Fatalf("decoded shape wrong: cells=%d vertexFloats=%d", mesh.cellCount(), len(mesh.vertices))
	}
	if len(mesh.outline.flags) != 4 || len(mesh.internal.flags) != 1 {
		t.Fatalf("edge groups wrong: outline=%d internal=%d", len(mesh.outline.flags), len(mesh.internal.flags))
	}
	if !objectLaneSealed(mesh, 0) || !objectLaneSealed(mesh, 1) {
		t.Fatal("all-rail deck must be sealed from both cells")
	}
	open, err := decodeObjectNavPayload(deckPayload(true), 0)
	if err != nil {
		t.Fatalf("open payload must decode: %v", err)
	}
	if objectLaneSealed(open, 1) {
		t.Fatal("cell 1 must reach the open exit through the internal edge")
	}

	// A truncated payload must fail loudly (exact-consume discipline),
	// never decode to a partial mesh.
	truncated := deckPayload(false)
	if _, err := decodeObjectNavPayload(truncated[:len(truncated)-3], 0); err == nil {
		t.Fatal("truncated payload must fail to decode")
	}
	if _, err := decodeObjectNavPayload(append(truncated, 0), 0); err == nil {
		t.Fatal("payload with trailing bytes must fail to decode")
	}
}

func TestDecodeObjectNavPayloadRejectsHostileCountsAndCoordinates(t *testing.T) {
	u32s := func(values ...uint32) []byte {
		var raw []byte
		for _, value := range values {
			raw = binary.LittleEndian.AppendUint32(raw, value)
		}
		return raw
	}

	tests := []struct {
		name  string
		raw   []byte
		flags int
	}{
		{
			name: "vertex count beyond native index space",
			raw:  u32s(objectNavMaxVertices + 1),
		},
		{
			name: "edge amplification without cells",
			raw:  u32s(0, 0, 1),
		},
		{
			name:  "event-name count beyond remaining payload",
			raw:   u32s(0, 0, 0, 0, math.MaxUint32),
			flags: objectNavFlagEventNames,
		},
		{
			name: "negative grid dimension encoding",
			raw: append(
				u32s(0, 0, 0, 0),
				u32s(0, 0, math.MaxUint32, 0, 0)...,
			),
		},
		{
			name:  "unsupported nav flag",
			raw:   deckPayload(false),
			flags: 1 << 7,
		},
	}

	nonFinite := deckPayload(false)
	binary.LittleEndian.PutUint32(nonFinite[4:], math.Float32bits(float32(math.NaN())))
	tests = append(tests, struct {
		name  string
		raw   []byte
		flags int
	}{
		name: "non-finite vertex",
		raw:  nonFinite,
	})

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := decodeObjectNavPayload(test.raw, test.flags); err == nil {
				t.Fatal("hostile payload must fail to decode")
			}
		})
	}
}

// TestRelocateStrandedSpawnSealedObjectDeck is the Jangan walkway incident
// in miniature: a spawn persisted ON a sealed elevated deck over walkable
// mainland terrain. The tile-based verdict alone says mainland (the trap
// this change closes); the object-nav verdict must strand it and drop it
// to the terrain plane below.
func TestRelocateStrandedSpawnSealedObjectDeck(t *testing.T) {
	validator := NewWaterValidator(syntheticObjectNavRoot(t))

	rescued, stranded, found := validator.RelocateStrandedSpawn(
		simulation.Spawn{RegionID: 0x6B4F, X: 100, Y: 10, Z: 100, Angle: 11},
	)
	if !stranded || !found {
		t.Fatalf("sealed-deck spawn must rescue: stranded=%v found=%v", stranded, found)
	}
	if rescued.Y != 0 {
		t.Fatalf("rescue must land on the terrain plane, got y=%v", rescued.Y)
	}
	if dist := simulation.WorldDistance2D(simulation.Spawn{RegionID: 0x6B4F, X: 100, Z: 100}, rescued); dist > 20 {
		t.Fatalf("drop-through rescue must stay near the deck (<=20u), moved %v", dist)
	}
	if rescued.Angle != 11 {
		t.Fatalf("rescue must keep the facing, got %d", rescued.Angle)
	}
	if _, rescuedStranded, _ := validator.RelocateStrandedSpawn(rescued); rescuedStranded {
		t.Fatalf("rescue point must itself pass the verdict: %+v", rescued)
	}
}

// TestRelocateStrandedSpawnLegalElevatedStands pins the false-positive
// guards: an open deck (bridge shape - standing over BLOCKED seabed
// tiles, which the pre-object rescue wrongly stranded), a terrain-owned
// spawn under the deck, and a floating spawn over nothing pass untouched.
func TestRelocateStrandedSpawnLegalElevatedStands(t *testing.T) {
	validator := NewWaterValidator(syntheticObjectNavRoot(t))

	cases := []struct {
		name  string
		spawn simulation.Spawn
	}{
		{"openDeckExit", simulation.Spawn{RegionID: 0x6B4F, X: 500, Y: 10, Z: 500}},
		{"terrainOwnedUnderDeck", simulation.Spawn{RegionID: 0x6B4F, X: 100, Y: 0, Z: 100}},
		{"floatingOverPlainTerrain", simulation.Spawn{RegionID: 0x6B4F, X: 1500, Y: 10, Z: 1500}},
	}
	for _, tc := range cases {
		if _, stranded, _ := validator.RelocateStrandedSpawn(tc.spawn); stranded {
			t.Errorf("%s: must not strand", tc.name)
		}
	}
}

func TestRelocateStrandedSpawnUnresolvedObjectLinkFailsClosed(t *testing.T) {
	validator := NewWaterValidator(syntheticObjectNavRoot(t))
	if _, stranded, _ := validator.RelocateStrandedSpawn(
		simulation.Spawn{RegionID: 0x6B4F, X: 900, Y: 10, Z: 900},
	); !stranded {
		t.Fatal("unresolved object-link deck was treated as a proven open exit")
	}
}

// TestRelocateStrandedSpawnObjectDataMissingFailsOpen removes the object
// resource index from an otherwise identical world: the sealed-deck spawn
// must degrade to the old (not stranded) verdict, matching the
// accept-on-unreadable posture of every gate in this package.
func TestRelocateStrandedSpawnObjectDataMissingFailsOpen(t *testing.T) {
	root := syntheticObjectNavRoot(t)
	writeTestAsset(t, root, "assets/world/outdoor/object-resources.json", `not json`)
	validator := NewWaterValidator(root)

	if _, stranded, _ := validator.RelocateStrandedSpawn(
		simulation.Spawn{RegionID: 0x6B4F, X: 100, Y: 10, Z: 100},
	); stranded {
		t.Fatal("unreadable object index must fail open, never strand")
	}
}

// TestRelocateStrandedSpawnRealJanganWalkway pins the live incident on
// the REAL region 0x61A7 payload: the character persisted at y~3.04 on
// the cj_pub03_floor deck (sealed lane, zero reachable open exits) must
// rescue onto the walkable terrain below; the SAME x/z persisted at
// terrain height is terrain-owned and passes through.
func TestRelocateStrandedSpawnRealJanganWalkway(t *testing.T) {
	validator := realAuthorityValidator(t)

	trapped := simulation.Spawn{RegionID: 0x61A7, X: 623, Y: 3.04, Z: 1271, Angle: 5}
	rescued, stranded, found := validator.RelocateStrandedSpawn(trapped)
	if !stranded {
		t.Fatal("sealed-deck spawn must report stranded")
	}
	if !found {
		t.Fatal("walkable terrain under the deck must yield a rescue")
	}
	if dist := simulation.WorldDistance2D(trapped, rescued); dist > 960 {
		t.Fatalf("rescue moved %v units, want <= 960", dist)
	}
	if rescued.Y >= 2 {
		t.Fatalf("rescue must land on the terrain plane (y~0), got %v", rescued.Y)
	}
	if _, rescuedStranded, _ := validator.RelocateStrandedSpawn(rescued); rescuedStranded {
		t.Fatalf("rescue point must itself pass the verdict: %+v", rescued)
	}

	grounded := simulation.Spawn{RegionID: 0x61A7, X: 623, Y: 0, Z: 1271}
	if _, stranded, _ := validator.RelocateStrandedSpawn(grounded); stranded {
		t.Fatal("terrain-owned spawn at the same x/z must pass through")
	}
}

// TestRelocateStrandedSpawnRealHarborBridgeDeck pins the false-positive
// guard on the REAL Constantinople harbor bridge (euro_esteuro_port01,
// asset 1630): a spawn on the deck ~105 units above the water-side
// terrain stands in a lane with dozens of reachable open outline exits -
// a LEGAL elevated stand that must never rescue.
func TestRelocateStrandedSpawnRealHarborBridgeDeck(t *testing.T) {
	validator := realAuthorityValidator(t)

	if _, stranded, _ := validator.RelocateStrandedSpawn(
		simulation.Spawn{RegionID: 0x6850, X: 1263, Y: -25.17, Z: 1490.5},
	); stranded {
		t.Fatal("open bridge deck must never strand")
	}
}

/*
================
TestRelocateStrandedSpawnRealHotanPalaceDeck

The Hotan palace deck (oas_hot_c3_castle, asset 680) spans sectors 0x5E87 and
0x5D87. Its bridge link edge (outline 120 -> oas_hot_b2_bridge asset 682 ->
oas_hot_c1_castle asset 681 -> open terrain exits) lies in 0x5D87, while
0x5E87's NVM stores target=65535 for that link. Spawning or warping to
(0x5E87, 1145, 272, 576) must resolve the cross-sector link as an open deck and
walk south across the sector border onto the palace bridge.
================
*/
func TestRelocateStrandedSpawnRealHotanPalaceDeck(t *testing.T) {
	validator := realAuthorityValidator(t)

	palaceSpawn := simulation.Spawn{RegionID: 0x5E87, X: 1145, Y: 272, Z: 576}
	surfaceY, ok := validator.WalkableSpawnHeightAt(palaceSpawn.RegionID, palaceSpawn.X, palaceSpawn.Y, palaceSpawn.Z)
	if !ok || math.Abs(surfaceY-265.213) > 0.05 {
		t.Fatalf("WalkableSpawnHeightAt(%+v) = (%v, %v), want (~265.213, true)", palaceSpawn, surfaceY, ok)
	}
	if _, stranded, _ := validator.RelocateStrandedSpawn(palaceSpawn); stranded {
		t.Fatal("Hotan palace deck with cross-sector bridge exit must not report stranded")
	}

	// Verify that the 0x5E87 stand has an unresolved placeholder link in 0x5E87
	// and that rebinding to 0x5D87 (135, 93) recovers the resolved bridge link
	// (outline 120 -> asset 682 bridge) and admits walking onto the bridge even
	// when starting from a 0x5E87-anchored NavOwner hint.
	owner0x5E87, _, ok := validator.ResolveNavOwner(palaceSpawn, simulation.NavOwner{})
	if !ok || owner0x5E87.Kind != simulation.NavOwnerObject {
		t.Fatalf("ResolveNavOwner(%+v) = %+v, %v; want object owner", palaceSpawn, owner0x5E87, ok)
	}
	stand0x5E87 := validator.standForOwner(owner0x5E87)
	if stand0x5E87 == nil || !standHasUnresolvedLink(stand0x5E87) {
		t.Fatalf("expected 0x5E87 palace stand to carry unresolved cross-sector link: %+v", stand0x5E87)
	}
	stand0x5D87 := validator.rebindStandToSector(stand0x5E87, 135, 93)
	if stand0x5D87 == nil || standHasUnresolvedLink(stand0x5D87) {
		t.Fatalf("expected 0x5D87 rebound palace stand to resolve bridge link: %+v", stand0x5D87)
	}

	link := stand0x5D87.placement.links[0]
	srcCell := int(stand0x5D87.mesh.outline.srcCell[link.edge])
	srcLX, srcLZ := objectCellCentroid2D(stand0x5D87.mesh, srcCell)
	srcLY, _ := objectCellPlaneYAt(stand0x5D87.mesh, srcCell, srcLX, srcLZ)
	c0, s0 := math.Cos(stand0x5D87.placement.yaw), math.Sin(stand0x5D87.placement.yaw)
	fromBridgeDoor := simulation.Spawn{
		RegionID: 0x5D87,
		X:        c0*srcLX - s0*srcLZ + stand0x5D87.placement.x,
		Y:        srcLY + stand0x5D87.placement.y,
		Z:        s0*srcLX + c0*srcLZ + stand0x5D87.placement.z,
	}

	var bridgeEntry *resolvedObjectNav
	for i := range stand0x5D87.set {
		if stand0x5D87.set[i].placement.ordinal == link.target {
			bridgeEntry = &stand0x5D87.set[i]
			break
		}
	}
	if bridgeEntry == nil || len(bridgeEntry.meshes) != 1 {
		t.Fatalf("bridge target ordinal %d not found in 0x5D87 set", link.target)
	}
	bridgeMesh := bridgeEntry.meshes[0]
	dstCell := int(bridgeMesh.outline.srcCell[link.targetEdge])
	dstLX, dstLZ := objectCellCentroid2D(bridgeMesh, dstCell)
	dstLY, _ := objectCellPlaneYAt(bridgeMesh, dstCell, dstLX, dstLZ)
	c1, s1 := math.Cos(bridgeEntry.placement.yaw), math.Sin(bridgeEntry.placement.yaw)
	toBridgeCell := simulation.Spawn{
		RegionID: 0x5D87,
		X:        c1*dstLX - s1*dstLZ + bridgeEntry.placement.x,
		Y:        dstLY + bridgeEntry.placement.y,
		Z:        s1*dstLX + c1*dstLZ + bridgeEntry.placement.z,
	}

	hintFrom0x5E87 := stand0x5E87.navOwner(srcCell)
	reboundOwner, _, ok := validator.ResolveNavOwner(fromBridgeDoor, hintFrom0x5E87)
	if !ok || reboundOwner != stand0x5D87.navOwner(srcCell) {
		t.Fatalf("ResolveNavOwner did not rebind 0x5E87 hint to 0x5D87: got %+v, want %+v", reboundOwner, stand0x5D87.navOwner(srcCell))
	}
	clip := validator.ClipMovementPathFrom(fromBridgeDoor, hintFrom0x5E87, toBridgeCell)
	if clip.Outcome != ClipArrived {
		t.Fatalf("palace->bridge walk with 0x5E87-anchored owner hint clipped: %+v", clip)
	}
	walk := validator.WalkOwners(fromBridgeDoor, hintFrom0x5E87, toBridgeCell)
	if walk.Rest.Kind != simulation.NavOwnerObject || walk.Rest.Object.Cell != int32(dstCell) {
		t.Fatalf("bridge rest owner = %+v, want bridge cell %d", walk.Rest, dstCell)
	}
}

// ---- edge blocking semantics (objectNavEdgeBlocks) ----

// TestObjectNavEdgeBlocks pins the directional blocking rule against the
// exact flag values the shipped corpus carries (all 1191 offset-7
// payloads; distribution in the objectnav.go section banner). src = the
// crossing approaches from the edge's srcCell side, dst = from the other
// side (for outline edges: from outside the object).
func TestObjectNavEdgeBlocks(t *testing.T) {
	cases := []struct {
		flags            byte
		outline          bool
		fromSrc, fromDst bool // want blocked
		name             string
	}{
		{0x04, false, false, false, "internalOpen"},
		{0x14, false, false, false, "internalOpenBit10"},
		{0x07, false, true, true, "internalBothSealed"},
		{0x87, false, true, true, "internalBothSealedHitRecord"},
		{0x86, false, true, false, "internalSrcSideOnly"},
		{0x00, true, false, false, "outlineOpenExit"},
		{0x03, true, true, true, "outlineRail"},
		{0x83, true, true, true, "outlineRailHitRecord"},
		{0x08, true, true, true, "outlineEdgeObjectLink"},
		{0x88, true, true, true, "outlineEdgeObjectLinkHitRecord"},
		{0x10, true, true, true, "outlineReflectNoSideBits"},
		{0x90, true, true, true, "outlineReflectHitRecord"},
		{0x80, true, true, true, "outlineHitRecordOnly"},
	}
	for _, tc := range cases {
		if got := objectNavEdgeBlocks(tc.flags, tc.outline, true); got != tc.fromSrc {
			t.Errorf("%s: fromSrc = %v, want %v", tc.name, got, tc.fromSrc)
		}
		if got := objectNavEdgeBlocks(tc.flags, tc.outline, false); got != tc.fromDst {
			t.Errorf("%s: fromDst = %v, want %v", tc.name, got, tc.fromDst)
		}
	}
}

// ---- segment-vs-edge first contact (objectMeshChordContact) ----

// internalFlagsPayload is the open deck with every OUTLINE edge open and
// the single internal diagonal carrying the given flags - isolates the
// directional internal-edge rule for the chord test.
func internalFlagsPayload(flags byte) []byte {
	vertices := [][3]float32{{-20, 0, -20}, {20, 0, -20}, {20, 0, 20}, {-20, 0, 20}}
	cells := [][3]uint16{{0, 1, 2}, {0, 2, 3}}
	outline := []testNavEdge{
		{0, 1, 0, 0xffff, 0x0},
		{1, 2, 0, 0xffff, 0x0},
		{2, 3, 1, 0xffff, 0x0},
		{3, 0, 1, 0xffff, 0x0},
	}
	internal := []testNavEdge{{0, 2, 0, 1, flags}}
	return encodeObjectNavPayload(vertices, cells, outline, internal)
}

func TestObjectMeshChordContact(t *testing.T) {
	deck, err := decodeObjectNavPayload(deckPayload(true), 0)
	if err != nil {
		t.Fatalf("open deck payload must decode: %v", err)
	}

	// North rail (outline {2,3} flags 0x3 at z=+20) stops the chord at
	// its exact crossing parameter, from either side.
	if got, ok := objectMeshChordContact(deck, 0, 0, 0, 0, 40, 0, math.Inf(1)); !ok || math.Abs(got-0.5) > 1e-12 {
		t.Fatalf("north rail from inside: t=%v ok=%v, want 0.5", got, ok)
	}
	if got, ok := objectMeshChordContact(deck, 0, 40, 0, 0, 0, 0, math.Inf(1)); !ok || math.Abs(got-0.5) > 1e-12 {
		t.Fatalf("north rail from outside: t=%v ok=%v, want 0.5", got, ok)
	}

	// The east OPEN outline edge (flags 0x0) and the open internal
	// diagonal (flags 0x0) never contact.
	if _, ok := objectMeshChordContact(deck, 0, 5, 0, 40, 5, 0, math.Inf(1)); ok {
		t.Fatal("open east exit must not contact")
	}

	// Height gate: the same rail crossing 10 units above the deck plane
	// (an under/over-pass shape) is not a contact.
	if _, ok := objectMeshChordContact(deck, 0, 0, 10, 0, 40, 10, math.Inf(1)); ok {
		t.Fatal("rail crossed 10u above the edge plane must not contact")
	}

	// Directional internal side-blocks: flags 0x2 blocks leaving srcCell
	// (cell 0, the lower-right triangle), not entering it; flags 0x1 the
	// reverse (client sub_428930 side-block bits).
	src2, err := decodeObjectNavPayload(internalFlagsPayload(0x2), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := objectMeshChordContact(src2, 10, -10, 0, -10, 10, 0, math.Inf(1)); !ok {
		t.Fatal("internal 0x2 must block src->dst")
	}
	if _, ok := objectMeshChordContact(src2, -10, 10, 0, 10, -10, 0, math.Inf(1)); ok {
		t.Fatal("internal 0x2 must not block dst->src")
	}
	dst1, err := decodeObjectNavPayload(internalFlagsPayload(0x1), 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := objectMeshChordContact(dst1, 10, -10, 0, -10, 10, 0, math.Inf(1)); ok {
		t.Fatal("internal 0x1 must not block src->dst")
	}
	if _, ok := objectMeshChordContact(dst1, -10, 10, 0, 10, -10, 0, math.Inf(1)); !ok {
		t.Fatal("internal 0x1 must block dst->src")
	}
}

func TestObjectChordFindsNeighborAnchoredOverhang(t *testing.T) {
	validator := NewWaterValidator(syntheticObjectNavRoot(t))
	gridPoint := worldgeom.ExpandGrid(worldgeom.RegionXZ{RegionID: 0x6B4F, X: 1900, Z: 100})
	if !validator.worldPointOnObjectDeck(gridPoint.X, gridPoint.Z, 10) {
		t.Fatal("neighbor-anchored overhang did not own a point on its deck")
	}
	report := validator.ClipMovementPath(
		simulation.Spawn{RegionID: 0x6B4F, X: 1870, Y: 10, Z: 100},
		simulation.Spawn{RegionID: 0x6B4F, X: 1910, Y: 10, Z: 100},
	)
	if report.Outcome != ClipBlocked || report.Class != ClipClassObject {
		t.Fatalf("neighbor-anchored rail = %s/%s (%+v), want blocked/object", report.Outcome, report.Class, report)
	}
	if math.Abs(report.Rest.X-1870.0100002288818) > 0.00001 {
		t.Fatalf("neighbor-anchored rail rest x = %.3f, want source + direction bias", report.Rest.X)
	}
}

func TestWalkableSpawnHeightUsesNearestObjectNavSurface(t *testing.T) {
	validator := NewWaterValidator(syntheticObjectNavRoot(t))

	if _, ok := validator.WalkableTerrainHeightAt(0x6B4F, 500, 500); ok {
		t.Fatal("blocked terrain below the open deck unexpectedly passed")
	}
	height, ok := validator.WalkableSpawnHeightAt(0x6B4F, 500, 10, 500)
	if !ok || math.Abs(height-10) > 1e-9 {
		t.Fatalf("object-owned spawn height = %.3f/%v, want 10/true", height, ok)
	}
	height, ok = validator.WalkableSpawnHeightAt(0x6B4F, 1500, 1, 1500)
	if !ok || math.Abs(height) > 1e-9 {
		t.Fatalf("terrain-owned spawn height = %.3f/%v, want 0/true", height, ok)
	}
	if _, ok := validator.WalkableSpawnHeightAt(0x8001, 500, 10, 500); ok {
		t.Fatal("outdoor surface owner accepted a dungeon-sector spawn")
	}
}

func TestWalkableSpawnHeightAtRealDonwhangDungeon(t *testing.T) {
	validator := realAuthorityValidator(t)
	height, ok := validator.WalkableSpawnHeightAt(0x8001, 605.840027, -9.98, 202.720001)
	if !ok {
		t.Fatal("packaged Donwhang DOF resident nav did not resolve its authored monster nest")
	}
	if math.Abs(height-(-9.98)) > 2 {
		t.Fatalf("Donwhang resident height = %.3f, want the authored -9.98 plane", height)
	}
}

/*
================
standHasUnresolvedLink
================
*/
func standHasUnresolvedLink(s *objectDeckStand) bool {
	if s == nil {
		return false
	}
	for _, link := range s.placement.links {
		if link.target == 65535 {
			return true
		}
	}
	return s.placement.linkEdgeCount > len(s.placement.links)
}
