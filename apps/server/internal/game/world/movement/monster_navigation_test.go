package movement

import (
	"math"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
	"testing"
)

func TestMonsterNavigationRetainsRealStairSurfaceInEveryLiveRead(t *testing.T) {
	v := realAuthorityValidator(t)
	from := monster.Pose{RegionID: 0x6046, X: 920, Y: 1124.1568906758841, Z: 610}
	to := monster.Pose{RegionID: 0x6046, X: 920, Y: 1090.5145930961428, Z: 670}
	p := v.PlanMonsterPath(from, to)
	if p == nil {
		t.Fatal("covered stair path unavailable")
	}
	if route := v.PlanMonsterRoute(from, to); route == nil || route.Len() != 1 {
		t.Fatal("shared route owner failed the real connected stair path")
	}
	m := monster.MoverState{From: from, To: p.Rest(), DepartMs: 1000, ArriveMs: 7000}
	m.AdoptNavigation(p)
	owned := v.objectSurfacePath(simulation.Spawn{RegionID: from.RegionID, X: from.X, Y: from.Y, Z: from.Z}, simulation.Spawn{RegionID: to.RegionID, X: to.X, Y: to.Y, Z: to.Z})
	// The adopted path owns the height, so the ground resolver is never
	// consulted; it is adapted to the GroundResolver shape (#1695) with
	// the same terrain sample behind it.
	ground := func(pose monster.Pose) (float64, bool) {
		return v.TerrainHeightAt(pose.RegionID, pose.X, pose.Z)
	}
	for i := 1; i < 6; i++ {
		y, ok := owned.heightAt(float64(i) / 6)
		got := m.LivePoseAt(1000+int64(i)*1000, ground)
		if !ok || math.Abs(got.Y-y) > .001 {
			t.Fatalf("step %d lost stair ownership: %+v expected %v", i, got, y)
		}
		if naked := m.LivePoseAt(1000+int64(i)*1000, nil); naked != got {
			t.Fatal("combat and AI read different surfaces")
		}
	}
	unknown := monster.Pose{RegionID: 0, X: 1, Z: 1}
	if v.PlanMonsterPath(unknown, unknown) != nil {
		t.Fatal("uncovered region admitted")
	}
}
