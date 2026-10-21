package movement

import (
	"encoding/json"
	worldgeom "opensro.online/server/internal/game/world"
	"opensro.online/server/internal/game/world/simulation"
	"os"
	"path/filepath"
	"testing"
)

func TestNativeContactResponseReference(t *testing.T) {
	raw, err := os.ReadFile(filepath.Join("testdata", "native-contact-reference.json"))
	if err != nil {
		t.Fatal(err)
	}
	var reference struct {
		BinarySha256 string
		Snap         []struct{ Center, Point, Result [2]float64 }
		Slide        []struct {
			Enabled                            bool
			Start, Requested, Point, Placement [3]float64
		}
	}
	if err = json.Unmarshal(raw, &reference); err != nil {
		t.Fatal(err)
	}
	if reference.BinarySha256 != "375e868234437e815af8ce9289ddea7ec9144430f4ea24e32988a6d6c9dd108a" {
		t.Fatal("Unpinned native reference")
	}
	for _, r := range reference.Snap {
		x, z := nativeCellEntry(r.Center[0], r.Center[1], r.Point[0], r.Point[1])
		if [2]float64{x, z} != r.Result {
			t.Fatalf("Cell entry %+v != %+v", [2]float64{x, z}, r.Result)
		}
	}
	mesh := &objectNavMesh{vertices: []float32{0, 0, 0, 0, 0, 100, 100, 0, 0}, cellA: []uint16{0}, cellB: []uint16{1}, cellC: []uint16{2}, minX: 0, maxX: 100, minZ: 0, maxZ: 100, outline: objectNavEdges{vertA: []uint16{0, 1, 2}, vertB: []uint16{1, 2, 0}, srcCell: []uint16{0, 0, 0}, dstCell: []uint16{65535, 65535, 65535}, flags: []byte{2, 2, 2}}}
	blocks := []dungeonSpawnBlock{{meshes: []*objectNavMesh{mesh}}}
	v := &WaterValidator{dungeonSpawnSurfaces: map[uint16]*dungeonSpawnSurface{0x8001: newDungeonSpawnSurface(worldgeom.DungeonMessageGrid{}, blocks)}}
	v.dungeonSpawnOnce.Do(func() {})
	pose := func(p [3]float64) simulation.Spawn {
		return simulation.Spawn{RegionID: 0x8001, X: p[0], Y: p[1], Z: p[2]}
	}
	for _, r := range reference.Slide {
		blocks[0].x, blocks[0].y, blocks[0].z = r.Placement[0], r.Placement[1], r.Placement[2]
		v.dungeonSpawnSurfaces[0x8001] = newDungeonSpawnSurface(worldgeom.DungeonMessageGrid{}, blocks)
		report := v.ClipMovementPath(pose(r.Start), pose(r.Requested))
		actual := [3]float64{report.Rest.X, report.Rest.Y, report.Rest.Z}
		if report.Outcome != ClipBlocked || actual != r.Point {
			t.Fatalf("Native endpoint: %+v != %+v", report, r.Point)
		}
	}
}

func TestNativeOutdoorReflection(t *testing.T) {
	mesh := &objectNavMesh{vertices: []float32{0, 0, 0, 0, 0, 100, 100, 0, 0}, cellA: []uint16{0}, cellB: []uint16{1}, cellC: []uint16{2}, minX: 0, maxX: 100, minZ: 0, maxZ: 100, outline: objectNavEdges{vertA: []uint16{1}, vertB: []uint16{2}, srcCell: []uint16{0}, dstCell: []uint16{65535}, flags: []byte{0}}}
	for _, flag := range []byte{0, 0x80} {
		mesh.outline.flags[0] = flag
		var point objectContactPoint
		_, ok := objectMeshChordContactDetail(mesh, 10, 10, 0, 80, 80, 0, 1, nil, &point, objectContactOptions{exits: true})
		if !ok || !point.valid || !point.continuation || point.x != 50.141422271728516 || point.z != point.x {
			t.Fatalf("reflection %x: %+v", flag, point)
		}
	}
	mesh.outline.flags[0] = 2
	var point objectContactPoint
	_, ok := objectMeshChordContactDetail(mesh, 10, 10, 0, 80, 80, 0, 1, nil, &point, objectContactOptions{exits: true})
	if !ok || point.continuation || point.x != 49.858577728271484 {
		t.Fatalf("rail became an exit: %+v", point)
	}
}
