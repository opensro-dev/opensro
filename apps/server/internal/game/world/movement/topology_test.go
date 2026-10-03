package movement

import (
	"encoding/json"
	"math"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"opensro.online/server/internal/testsupport/licensed"
	"os"
	"path/filepath"
	"testing"
)

func topologyMesh(west bool) *objectNavMesh {
	a, b, cell := uint16(1), uint16(2), uint16(0)
	if west {
		a, b, cell = 3, 0, 1
	}
	return &objectNavMesh{vertices: []float32{0, 10, 0, 100, 10, 0, 100, 10, 100, 0, 10, 100}, cellA: []uint16{0, 0}, cellB: []uint16{1, 2}, cellC: []uint16{2, 3}, minX: 0, maxX: 100, minY: 10, maxY: 10, minZ: 0, maxZ: 100, outline: objectNavEdges{vertA: []uint16{a}, vertB: []uint16{b}, srcCell: []uint16{cell}, dstCell: []uint16{65535}, flags: []byte{8}}}
}
func TestDungeonTopologyClipsAndTraverses(t *testing.T) {
	blocks := []dungeonSpawnBlock{{ordinal: 0, connected: []int{1}, meshes: []*objectNavMesh{topologyMesh(false)}}, {ordinal: 1, connected: []int{0}, x: 100, meshes: []*objectNavMesh{topologyMesh(true)}}}
	surface := &dungeonSpawnSurface{blocks: blocks, objects: resolveDungeonLinks(blocks)}
	if len(surface.objects[0].placement.links) != 1 {
		t.Fatal("Missing portal")
	}
	v := &WaterValidator{dungeonSpawnSurfaces: map[uint16]*dungeonSpawnSurface{0x8001: surface}}
	v.dungeonSpawnOnce.Do(func() {})
	a := simulation.Spawn{RegionID: 0x8001, X: 50, Y: 10, Z: 50}
	b := a
	b.X = 150
	for _, pair := range [][2]simulation.Spawn{{a, b}, {b, a}} {
		r := v.ClipMovementPath(pair[0], pair[1])
		if r.Outcome != ClipArrived || r.Rest != pair[1] {
			t.Fatalf("Linked traversal %+v", r)
		}
		if got := v.SpawnMoveTest(pair[0], pair[1]).Result; got != 0 {
			t.Fatalf("Linked spawn path refused: %x", got)
		}
	}
	blocks[0].connected = nil
	blocks[1].connected = nil
	surface.objects = resolveDungeonLinks(blocks)
	if got := v.SpawnMoveTest(a, b).Result; got != monster.NavResultBlocked {
		t.Fatalf("Unlinked spawn path admitted: %x", got)
	}
	if r := v.ClipMovementPath(a, b); r.Outcome != ClipBlocked || r.Rest.X != 99.82111358642578 || r.Rest.Z != 49.91055679321289 {
		t.Fatalf("Unlinked crossing %+v", r)
	}
	b.RegionID = 0x8002
	if r := v.ClipMovementPath(a, b); r.Outcome != ClipBlocked || r.Rest != a {
		t.Fatalf("Cross-dungeon request %+v", r)
	}
	delete(v.dungeonSpawnSurfaces, 0x8001)
	b.RegionID = a.RegionID
	if got := v.SpawnMoveTest(a, b).Result; got != monster.NavResultBlocked {
		t.Fatalf("Unknown dungeon surface admitted: %x", got)
	}
	if r := v.ClipMovementPath(a, b); r.Rest != a || r.Outcome != ClipBlocked {
		t.Fatalf("Missing coverage %+v", r)
	}
}
func TestPortalToleranceAndOriginalObjectOrdinals(t *testing.T) {
	a, b := topologyMesh(false), topologyMesh(true)
	p, q := objectNavPlacement{}, objectNavPlacement{x: 100}
	if !portalMatches(p, a, 0, q, b, 0) {
		t.Fatal("Reversed endpoints must match")
	}
	q.y = 5
	if portalMatches(p, a, 0, q, b, 0) {
		t.Fatal("Five is excluded")
	}
	q.y = 4.99
	if !portalMatches(p, a, 0, q, b, 0) {
		t.Fatal("Sub-five rejected")
	}
	rows := decodeObjectPlacements(json.RawMessage(`[{"assetId":0},{"assetId":1,"x":0,"y":0,"z":0,"yaw":0,"linkEdgeCount":1,"linkEdges":"AgAAAAAA"},{"assetId":2,"x":100,"y":0,"z":0,"yaw":0}]`))
	if len(rows) != 2 || rows[0].ordinal != 1 || rows[1].ordinal != 2 || rows[0].links[0].target != 2 {
		t.Fatalf("Placement identity lost: %+v", rows)
	}
	set := []resolvedObjectNav{{placement: rows[0], meshes: []*objectNavMesh{a}}, {placement: rows[1], meshes: []*objectNavMesh{b}}}
	if !objectLinkCrossing(set, 0, a, 50, 10, 50, 150, 10, 50)(0, true, .5) {
		t.Fatal("Original target ordinal not followed")
	}
	set[1].placement.ordinal = 9
	if objectLinkCrossing(set, 0, a, 50, 10, 50, 150, 10, 50)(0, true, .5) {
		t.Fatal("Missing target became open")
	}
}

func TestPortalNativeReference(t *testing.T) {
	raw, err := os.ReadFile("testdata/native-portal-reference.json")
	if err != nil {
		t.Fatal(err)
	}
	var oracle struct {
		Rows []struct {
			A, B    []float32
			Matches bool
		}
	}
	if err = json.Unmarshal(raw, &oracle); err != nil {
		t.Fatal(err)
	}
	if len(oracle.Rows) != 42 {
		t.Fatal("Incomplete native oracle")
	}
	for _, row := range oracle.Rows {
		a, b := &objectNavMesh{vertices: row.A, outline: objectNavEdges{vertA: []uint16{0}, vertB: []uint16{1}}}, &objectNavMesh{vertices: row.B, outline: objectNavEdges{vertA: []uint16{0}, vertB: []uint16{1}}}
		if got := portalMatches(objectNavPlacement{}, a, 0, objectNavPlacement{}, b, 0); got != row.Matches {
			t.Fatalf("Native mismatch %+v", row)
		}
	}
}

func TestLinkedLaneRescueAndDungeonObjects(t *testing.T) {
	a, b := topologyMesh(false), topologyMesh(true)
	blocks := []dungeonSpawnBlock{{ordinal: 0, connected: []int{1}, meshes: []*objectNavMesh{a}}, {ordinal: 1, connected: []int{0}, x: 100, meshes: []*objectNavMesh{b}}}
	set := resolveDungeonLinks(blocks)
	if !objectLinkedLaneSealed(set, 0, a, 0) {
		t.Fatal("Closed linked cycle is not an exit")
	}
	b.outline.vertA = append(b.outline.vertA, 0)
	b.outline.vertB = append(b.outline.vertB, 1)
	b.outline.srcCell = append(b.outline.srcCell, 1)
	b.outline.dstCell = append(b.outline.dstCell, 65535)
	b.outline.flags = append(b.outline.flags, 0)
	if objectLinkedLaneSealed(set, 0, a, 0) {
		t.Fatal("Reachable linked exit classified sealed")
	}
	block := dungeonSpawnBlock{meshes: []*objectNavMesh{a}, obstacles: []dungeonObstacle{{x: 60, y: 10, z: 50, radiusSquared: 625}}}
	surface := &dungeonSpawnSurface{blocks: []dungeonSpawnBlock{block}, objects: resolveDungeonLinks([]dungeonSpawnBlock{block})}
	v := &WaterValidator{dungeonSpawnSurfaces: map[uint16]*dungeonSpawnSurface{0x8001: surface}}
	v.dungeonSpawnOnce.Do(func() {})
	from := simulation.Spawn{RegionID: 0x8001, X: 10, Y: 10, Z: 50}
	to := from
	to.X = 90
	r := v.ClipMovementPath(from, to)
	if r.Outcome != ClipBlocked || r.Rest.X < 34.9 || r.Rest.X >= 35 {
		t.Fatalf("Object contact %+v", r)
	}
	from.X = 65
	r = v.ClipMovementPath(from, to)
	if r.Outcome != ClipArrived {
		t.Fatal("Cannot escape circle")
	}
	to.X = 60
	r = v.ClipMovementPath(from, to)
	if r.Rest != from {
		t.Fatal("Moved deeper into circle")
	}
}

func TestPublishedDungeonPortalTraversal(t *testing.T) {
	licensed.RequireGameData(t)
	root, err := filepath.Abs("../../../../../../.generated/client-public")
	if err != nil {
		t.Fatal(err)
	}
	v := NewWaterValidator(root)
	v.dungeonSpawnOnce.Do(func() { v.dungeonSpawnSurfaces = v.loadDungeonSpawnSurfaces() })
	total := 0
	point := func(p objectNavPlacement, m *objectNavMesh, e int, region uint16) simulation.Spawn {
		a, b := m.outline.vertA[e], m.outline.vertB[e]
		x, y, z := (float64(m.vertices[a*3])+float64(m.vertices[b*3]))/2, (float64(m.vertices[a*3+1])+float64(m.vertices[b*3+1]))/2, (float64(m.vertices[a*3+2])+float64(m.vertices[b*3+2]))/2
		cx, cz := objectCellCentroid2D(m, int(m.outline.srcCell[e]))
		d := math.Hypot(cx-x, cz-z)
		x += (cx - x) / d * .5
		z += (cz - z) / d * .5
		return simulation.Spawn{RegionID: region, X: math.Cos(p.yaw)*x - math.Sin(p.yaw)*z + p.x, Y: y + p.y, Z: math.Sin(p.yaw)*x + math.Cos(p.yaw)*z + p.z}
	}
	for region, surface := range v.dungeonSpawnSurfaces {
		for i, p := range surface.objects {
			for _, link := range p.placement.links {
				var target *resolvedObjectNav
				for j := range surface.objects {
					if surface.objects[j].placement.ordinal == link.target {
						target = &surface.objects[j]
						break
					}
				}
				if target == nil {
					t.Fatal("Unresolved portal")
				}
				a, b := point(p.placement, p.meshes[0], link.edge, region), point(target.placement, target.meshes[0], link.targetEdge, region)
				r := v.ClipMovementPath(a, b)
				total++
				if r.Outcome != ClipArrived || math.Hypot(r.Rest.X-b.X, r.Rest.Z-b.Z) > .1 {
					t.Errorf("Portal %x/%d/%d failed: %+v", region, i, link.edge, r)
				}
			}
		}
	}
	if total != 308 {
		t.Fatalf("Portal census %d", total)
	}
}
