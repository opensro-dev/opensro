package movement

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"opensro.online/server/internal/testsupport/clientdata"
	"opensro.online/server/internal/testsupport/gamedatatest"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

func TestPublishedJanganPotionBuildingEntryAndWestwardTrap(t *testing.T) {
	clientdata.RequireGameData(t)
	v := NewAuthorityValidator(gamedatatest.WorldAuthorityDir(t))
	inside := simulation.Spawn{RegionID: 25000, X: 1640, Y: 0, Z: 1390}
	for _, z := range []float64{1250, 1300, 1550} {
		for _, y := range []float64{0, 8.8, 40} {
			from := inside
			from.Z, from.Y = z, y
			got := v.ClipMovementPath(from, inside)
			if got.Outcome != ClipBlocked || got.Class != ClipClassObject || got.TilesUncovered != 0 || math.Hypot(got.Rest.X-inside.X, got.Rest.Z-inside.Z) <= 40 {
				t.Fatalf("solid building entry from %+v: %+v", from, got)
			}
		}
	}
	west := inside
	west.X = 1590
	got := v.ClipMovementPath(inside, west)
	if got.Outcome != ClipBlocked || got.Class != ClipClassObject || got.Rest.X <= 1594.7 || got.Rest.X >= 1594.9 {
		t.Fatalf("reported westward boundary: %+v", got)
	}
	// A private validator permits a discriminating historical control without
	// changing published files or any live character: omit the terrain candidate
	// associations and the old edge-Y window admits the high wall but not the low one.
	set := v.objectNavSetForOffset(v.surfaceForRegion(inside.RegionID), 0, 0)
	if len(set) == 0 {
		t.Fatal("missing published object coverage")
	}
	for i := range set {
		if set[i].placement.terrainCells == nil {
			t.Fatal("published object is using legacy height admission")
		}
		set[i].placement.terrainCells = nil
	}
	from := inside
	from.Z = 1300
	if got := v.ClipMovementPath(from, inside); got.Outcome != ClipArrived {
		t.Fatalf("historical entry control: %+v", got)
	}
	if got := v.ClipMovementPath(inside, west); got.Outcome != ClipBlocked {
		t.Fatalf("historical westward trap control: %+v", got)
	}
}

func TestPublishedRebirthBuildingHeightIndependentEntry(t *testing.T) {
	clientdata.RequireGameData(t)
	v := NewAuthorityValidator(gamedatatest.WorldAuthorityDir(t))
	for _, y := range []float64{80, 82, 120} {
		a := simulation.Spawn{RegionID: 27471, X: 1205, Y: y, Z: 396}
		b := simulation.Spawn{RegionID: 27471, X: 1394, Y: 82, Z: 398}
		got := v.ClipMovementPath(a, b)
		if got.Outcome != ClipBlocked || got.Class != ClipClassObject || got.Rest.X >= 1390 {
			t.Fatalf("height %v admitted building entry: %+v", y, got)
		}
	}
}

/*
================
terrainCellRegion

A region whose cells name placements by nvm ordinal: cell i (x from 10*i to
10*i+5) lists the ordinals in owners[i].
================
*/
func terrainCellRegion(t *testing.T, owners [][]uint16) navmeshRegion {
	t.Helper()
	var region navmeshRegion
	offsets := make([]byte, 4*(len(owners)+1))
	var indices []byte
	columns := [4][]byte{}
	at := 0
	for i, list := range owners {
		binary.LittleEndian.PutUint32(offsets[i*4:], uint32(at))
		for _, id := range list {
			indices = binary.LittleEndian.AppendUint16(indices, id)
			at++
		}
		for j, v := range []float32{float32(10 * i), 0, float32(10*i + 5), 5} {
			columns[j] = binary.LittleEndian.AppendUint32(columns[j], math.Float32bits(v))
		}
	}
	binary.LittleEndian.PutUint32(offsets[len(owners)*4:], uint32(at))
	encode := base64.StdEncoding.EncodeToString
	region.Cells.Count = json.Number(string(rune('0' + len(owners))))
	region.Cells.ObjectIndexOffsets, region.Cells.ObjectIndices = encode(offsets), encode(indices)
	region.Cells.MinX, region.Cells.MinZ = encode(columns[0]), encode(columns[1])
	region.Cells.MaxX, region.Cells.MaxZ = encode(columns[2]), encode(columns[3])
	return region
}

/*
================
TestTerrainCellsFollowObjectOrdinals

Object 0 is a real object (client 4413B0 checks only the upper bound), and a cell
names its placement by nvm ordinal. A malformed row the port drops owns
nothing and shifts nobody; an ordinal past the entry list is refused.
================
*/
func TestTerrainCellsFollowObjectOrdinals(t *testing.T) {
	rows, entries := decodeObjectPlacements(json.RawMessage(`[{"assetId":5},` +
		`{"assetId":0,"x":1,"y":0,"z":1,"yaw":0},{"assetId":7,"x":2,"y":0,"z":2,"yaw":0}]`))
	if entries != 3 || len(rows) != 2 || rows[0].assetID != 0 || rows[0].ordinal != 1 || rows[1].ordinal != 2 {
		t.Fatalf("placements %+v of %d entries", rows, entries)
	}
	// Cell 0 belongs to the dropped row, cell 1 to object 0, cell 2 to both kept rows.
	if err := admitTerrainObjectCells(terrainCellRegion(t, [][]uint16{{0}, {1}, {1, 2}}), rows, entries); err != nil {
		t.Fatal(err)
	}
	if got := rows[0].terrainCells.cells; len(got) != 2 || got[0][0] != 10 || got[1][0] != 20 {
		t.Fatalf("object 0 cells %v", got)
	}
	if got := rows[1].terrainCells.cells; len(got) != 1 || got[0][0] != 20 {
		t.Fatalf("ordinal 2 cells %v", got)
	}
	rows, entries = decodeObjectPlacements(json.RawMessage(`[{"assetId":0,"x":1,"y":0,"z":1,"yaw":0}]`))
	if err := admitTerrainObjectCells(terrainCellRegion(t, [][]uint16{{1}}), rows, entries); err == nil {
		t.Fatal("a cell naming an object past the entry list was admitted")
	}
}

/*
================
TestPublishedJanganFerryKeepsItsSurface

The Jangan ferry deck (cj_ferry_floor02) is object 0 of the outdoor table,
placed in 0x5EBC; the wharf title bundle seeds 0x5FBC. Rejecting object 0
lost both regions' whole ground surface at boot ("invalid terrain object
associations").
================
*/
func TestPublishedJanganFerryKeepsItsSurface(t *testing.T) {
	clientdata.RequireGameData(t)
	v := NewAuthorityValidator(gamedatatest.WorldAuthorityDir(t))
	for _, region := range []uint16{0x5ebc, 0x5fbc} {
		surface := v.surfaceForRegion(region)
		if surface == nil {
			t.Fatalf("region 0x%04x has no ground surface", region)
		}
		found := false
		for _, rows := range surface.objectPlacementsByOffset {
			for _, row := range rows {
				found = found || row.assetID == 0
			}
		}
		if !found {
			t.Fatalf("region 0x%04x lost its object 0 placement", region)
		}
	}
}
