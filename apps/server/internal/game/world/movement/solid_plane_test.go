package movement

import (
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"math"
	"testing"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

func TestSolidPlaneHeightAdmissionAndMask(t *testing.T) {
	heights, planes, flags := make([]byte, 97*97*4), make([]byte, 36*4), make([]byte, 36)
	for i := 0; i < 36; i++ {
		binary.LittleEndian.PutUint32(planes[i*4:], math.Float32bits(800))
	}
	bundle := regionBundle{}
	bundle.Navmesh.HeightMapAxisVertices = "97"
	bundle.Navmesh.TileSize = "20"
	row := navmeshRegion{Dx: "0", Dz: "0", HeightMap: base64.StdEncoding.EncodeToString(heights), PlaneHeight: base64.StdEncoding.EncodeToString(planes)}
	for _, tc := range []struct {
		flag byte
		want float64
	}{{0, 0}, {1, 0}, {2, 800}, {3, 800}, {4, 0}} {
		for i := range flags {
			flags[i] = tc.flag
		}
		row.PlaneType = base64.StdEncoding.EncodeToString(flags)
		bundle.Navmesh.Regions = []navmeshRegion{row}
		s := groundSurface{regionSize: 1920, heightsByOffset: buildHeightGrids(&bundle)}
		for _, p := range [][2]float64{{0, 0}, {319.9, 319.9}, {320, 320}, {750, 410}, {1919.9, 1919.9}} {
			if h, ok := s.terrainHeightAt(p[0], p[1]); !ok || h != tc.want {
				t.Fatalf("flag %d at %v: %v %v", tc.flag, p, h, ok)
			}
		}
	}
	for i := 0; i < 97*97; i++ {
		binary.LittleEndian.PutUint32(heights[i*4:], math.Float32bits(900))
	}
	row.HeightMap = base64.StdEncoding.EncodeToString(heights)
	for i := range flags {
		flags[i] = 2
	}
	row.PlaneType = base64.StdEncoding.EncodeToString(flags)
	bundle.Navmesh.Regions = []navmeshRegion{row}
	s := groundSurface{regionSize: 1920, heightsByOffset: buildHeightGrids(&bundle)}
	if h, _ := s.terrainHeightAt(750, 410); h != 900 {
		t.Fatalf("solid plane lowered a hill: %v", h)
	}
	for _, bad := range []string{"", "AA==", "!"} {
		bundle.Navmesh.Regions[0].PlaneHeight = bad
		if len(buildHeightGrids(&bundle)) != 0 {
			t.Fatal("malformed plane retained height authority")
		}
	}
	binary.LittleEndian.PutUint32(planes, math.Float32bits(float32(math.NaN())))
	bundle.Navmesh.Regions[0].PlaneHeight = base64.StdEncoding.EncodeToString(planes)
	if len(buildHeightGrids(&bundle)) != 0 {
		t.Fatal("NaN plane admitted")
	}
	// The projected JSON contract must carry both columns into the same loader.
	raw, _ := json.Marshal(row)
	var roundtrip navmeshRegion
	if err := json.Unmarshal(raw, &roundtrip); err != nil || roundtrip.PlaneType != row.PlaneType || roundtrip.PlaneHeight != row.PlaneHeight {
		t.Fatal("plane projection lost")
	}
}

func TestKarakoramSolidPlaneLoginMonsterAndMovement(t *testing.T) {
	v := realAuthorityValidator(t)
	from := simulation.Spawn{RegionID: 0x5c81, X: 750, Y: 690, Z: 410}
	to := from
	to.X = 800
	to.Z = 430
	to.Y = 800
	for _, p := range []simulation.Spawn{from, to} {
		if y, ok := v.TerrainHeightAt(p.RegionID, p.X, p.Z); !ok || y != 800 {
			t.Fatalf("ice ground: %v %v", y, ok)
		}
		if y, ok := v.WalkableSpawnHeightAt(p.RegionID, p.X, p.Y, p.Z); !ok || y != 800 {
			t.Fatalf("monster spawn on lake bed: %v %v", y, ok)
		}
	}
	entry := enterworld.LocalPlayerEntry{StartProfile: enterworld.StartProfile{RegionID: int64(from.RegionID), X: from.X, Y: from.Y, Z: from.Z}}
	if !enterworld.LiftSpawnAboveTerrain(&entry, v.TerrainHeightAt, v.WalkableSpawnHeightAt) || entry.StartProfile.Y != 800 {
		t.Fatalf("underground login not repaired: %+v", entry.StartProfile)
	}
	from.Y = 800
	_, y, ok := v.ResolveNavOwner(from, simulation.NavOwner{})
	if !ok || y != 800 {
		t.Fatalf("player owner surface: %v %v", y, ok)
	}
	path := v.PlanMonsterPath(monster.Pose{RegionID: from.RegionID, X: from.X, Y: from.Y, Z: from.Z}, monster.Pose{RegionID: to.RegionID, X: to.X, Y: to.Y, Z: to.Z})
	if path == nil {
		t.Fatal("monster cannot walk on ice")
	}
	mover := monster.MoverState{From: monster.Pose{RegionID: from.RegionID, X: from.X, Y: from.Y, Z: from.Z}, To: path.Rest(), DepartMs: 1000, ArriveMs: 7000}
	mover.AdoptNavigation(path)
	for now := int64(1000); now <= 7000; now += 500 {
		if p := mover.LivePoseAt(now, nil); p.Y != 800 {
			t.Fatalf("monster sank during AI/combat position read: %+v", p)
		}
	}
}
