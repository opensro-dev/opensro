/*
===========================================================================

walkin_zone_test.go - the shipped walk-in zones resolve where players walk

===========================================================================
*/
package movement

import (
	"math"
	"testing"

	"opensro.online/server/internal/game/world/simulation"
)

/*
================
eventCellCentroid

The world point at the centre of the first event zone among objects, in
the objects' own frame, and that zone's name.
================
*/
func eventCellCentroid(objects []resolvedObjectNav) (x, y, z float64, name string, ok bool) {
	for _, object := range objects {
		p := object.placement
		c, s := math.Cos(p.yaw), math.Sin(p.yaw)
		for _, mesh := range object.meshes {
			if mesh.cellEvents == nil {
				continue
			}
			var sx, sy, sz float64
			n := 0
			for cell, event := range mesh.cellEvents {
				if event == noCellEvent {
					continue
				}
				for _, v := range []uint16{mesh.cellA[cell], mesh.cellB[cell], mesh.cellC[cell]} {
					sx += float64(mesh.vertices[v*3])
					sy += float64(mesh.vertices[v*3+1])
					sz += float64(mesh.vertices[v*3+2])
					n++
				}
				name = mesh.eventNames[event]
			}
			lx, ly, lz := sx/float64(n), sy/float64(n), sz/float64(n)
			return c*lx - s*lz + p.x, ly + p.y, s*lx + c*lz + p.z, name, true
		}
	}
	return 0, 0, 0, "", false
}

/*
================
outdoorObjects
================
*/
func outdoorObjects(t *testing.T, v *WaterValidator, region uint16) []resolvedObjectNav {
	t.Helper()
	surface := v.surfaceForRegion(region)
	if surface == nil {
		t.Fatalf("no surface for region %#x", region)
	}
	dx := simulation.SectorX(region) - simulation.SectorX(surface.seedRegionID)
	dz := simulation.SectorY(region) - simulation.SectorY(surface.seedRegionID)
	return v.objectNavSetForOffset(surface, dx, dz)
}

/*
================
TestDonwhangMouthIsAWalkInZone

The cave mouth w_cd_dondunenter (placed at 0x6994, its zone square reaching
into 0x6A93) carries GATE_DUNGEON_DH_IN in edge-bounded cells. Its centre
resolves to the zone, a point 60 units away does not, and the zone lies
near the spot a player leaving the cave arrives at (teleportdata
GATE_DUNGEON_DH_IN, 0x6993 (1814, 452, 1834)), which pins the yaw sense.
================
*/
func TestDonwhangMouthIsAWalkInZone(t *testing.T) {
	v := realAuthorityValidator(t)
	const region = 0x6a93
	x, y, z, name, ok := eventCellCentroid(outdoorObjects(t, v, region))
	if !ok || name != "gate_dungeon_dh_in" {
		t.Fatalf("no Donwhang mouth zone in %#x (got %q)", region, name)
	}
	at := simulation.Spawn{RegionID: region, X: x, Y: y, Z: z}
	if zone := v.WalkInZoneAt(at); zone != "GATE_DUNGEON_DH_IN" {
		t.Fatalf("mouth centre %+v resolves %q", at, zone)
	}
	away := at
	away.X += 60
	if zone := v.WalkInZoneAt(away); zone != "" {
		t.Fatalf("a point beside the mouth resolves %q", zone)
	}
	gx := float64(simulation.SectorX(region))*simulation.NativeRegionSize + x
	gz := float64(simulation.SectorY(region))*simulation.NativeRegionSize + z
	ax := float64(simulation.SectorX(0x6993))*simulation.NativeRegionSize + 1814
	az := float64(simulation.SectorY(0x6993))*simulation.NativeRegionSize + 1834
	if d := math.Hypot(gx-ax, gz-az); d > 400 {
		t.Fatalf("mouth zone %.0f from the cave's exit arrival", d)
	}
}

/*
================
TestDonwhangCaveExitIsAWalkInZone

Inside the cave (dungeon 1, region 0x8001) the entry room's floor carries
GATE_DUNGEON_DH_OUT, the way back out.
================
*/
func TestDonwhangCaveExitIsAWalkInZone(t *testing.T) {
	v := realAuthorityValidator(t)
	const region = 0x8001
	v.dungeonSpawnOnce.Do(func() { v.dungeonSpawnSurfaces = v.loadDungeonSpawnSurfaces() })
	surface := v.dungeonSpawnSurfaces[region]
	if surface == nil {
		t.Fatal("no Donwhang cave surface")
	}
	x, y, z, name, ok := eventCellCentroid(surface.objects)
	if !ok || name != "gate_dungeon_dh_out" {
		t.Fatalf("no cave exit zone (got %q)", name)
	}
	if zone := v.WalkInZoneAt(simulation.Spawn{RegionID: region, X: x, Y: y, Z: z}); zone != "GATE_DUNGEON_DH_OUT" {
		t.Fatalf("cave exit centre resolves %q", zone)
	}
}

/*
================
TestQinShiDoorCircleIsAZoneOfItsMesh

The reported spot (BR-261014-1921-F200, 0x66AC (961, 150.4, 1828)) stands in
the tomb door's GATE_JINSI_OUT cells; whether entering it does anything is
the reference data's call (v1.150 has no such teleport zone).
================
*/
func TestQinShiDoorCircleIsAZoneOfItsMesh(t *testing.T) {
	v := realAuthorityValidator(t)
	if zone := v.WalkInZoneAt(simulation.Spawn{RegionID: 0x66ac, X: 961, Y: 150.4, Z: 1828}); zone != "GATE_JINSI_OUT" {
		t.Fatalf("reported spot resolves %q", zone)
	}
	if zone := v.WalkInZoneAt(simulation.Spawn{RegionID: 0x66ac, X: 961, Y: 150.4, Z: 1800}); zone != "" {
		t.Fatalf("the stair below the circle resolves %q", zone)
	}
}

/*
================
BenchmarkWalkInZoneAt200

One tick's zone lookups for 200 players spread over Jangan (0x61A8) and the
Donwhang mouth (0x6A93), warm caches: the per-tick cost advanceWalkIns
adds before any teleport.
================
*/
func BenchmarkWalkInZoneAt200(b *testing.B) {
	v := realAuthorityValidator(b)
	points := make([]simulation.Spawn, 0, 200)
	for i := 0; i < 200; i++ {
		region := uint16(0x61a8)
		if i%4 == 0 {
			region = 0x6a93
		}
		points = append(points, simulation.Spawn{RegionID: region, X: float64(100 + (i*37)%1700), Y: 0, Z: float64(100 + (i*91)%1700)})
	}
	for _, at := range points {
		v.WalkInZoneAt(at)
	}
	b.ResetTimer()
	for n := 0; n < b.N; n++ {
		for _, at := range points {
			v.WalkInZoneAt(at)
		}
	}
}
