package bot_test

import (
	"testing"

	"opensro.online/server/internal/bot"
)

// TestSelectLevelBandPicksTheHighestReachedBand guards the level-to-target
// selection (issue #171): the highest band whose level the bot reached wins,
// and a level below every band reports false.
func TestSelectLevelBandPicksTheHighestReachedBand(t *testing.T) {
	levels := []bot.LevelBand{
		{Level: 10, Target: bot.Point{Region: 100, X: 1}},
		{Level: 20, Target: bot.Point{Region: 200, X: 2}},
		{Level: 30, Target: bot.Point{Region: 300, X: 3}},
	}
	cases := []struct {
		level  int
		region uint16
		ok     bool
	}{
		{level: 5, ok: false},
		{level: 10, region: 100, ok: true},
		{level: 15, region: 100, ok: true},
		{level: 20, region: 200, ok: true},
		{level: 25, region: 200, ok: true},
		{level: 30, region: 300, ok: true},
		{level: 99, region: 300, ok: true},
	}
	for _, c := range cases {
		target, ok := bot.SelectLevelBand(levels, c.level)
		if ok != c.ok {
			t.Fatalf("level %d ok = %v, want %v", c.level, ok, c.ok)
		}
		if ok && target.Region != c.region {
			t.Fatalf("level %d region = %d, want %d", c.level, target.Region, c.region)
		}
	}
}

// TestBrainTravelsToTheLevelBandOverAPortal guards the band travel
// (issue #171): the brain walks to the gate, selects it and sends the portal
// request, then waits for the region change.
func TestBrainTravelsToTheLevelBandOverAPortal(t *testing.T) {
	catalog, err := bot.LoadPortalCatalog(portalFixture(t))
	if err != nil {
		t.Fatal(err)
	}
	gate := bot.Point{Region: 100, X: 500, Y: 0, Z: 500}
	brain := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7,
		Levels:       []bot.LevelBand{{Level: 10, Target: bot.Point{Region: 200, X: 900, Z: 900}}},
		Portals:      catalog,
		RoutePlanner: fakePathfinder{ok: true, route: []bot.Point{gate}},
	})
	actions := &fakeActions{}
	world := fakeWorld{self: bot.Self{
		HP: 100, MaxHP: 100, GID: 7, Level: 10,
		Region: 100, X: 0, Z: 0, PositionKnown: true,
	}}
	// The first tick walks to the gate in the current region.
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if len(actions.points) != 1 || actions.points[0] != gate {
		t.Fatalf("moves = %+v, want the gate %+v", actions.points, gate)
	}
	// At the gate: select and cross.
	world.self.X, world.self.Z, world.self.NowMs = gate.X, gate.Z, 500
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if len(actions.selects) != 1 || actions.selects[0] != bot.PortalGateIDBase+500 {
		t.Fatalf("selects = %+v, want the gate gid", actions.selects)
	}
	if len(actions.portals) != 1 || actions.portals[0] != [2]uint32{bot.PortalGateIDBase + 500, 2000} {
		t.Fatalf("portals = %+v, want (gate, 2000)", actions.portals)
	}
	event := brain.TakePortalEvent()
	if event == nil || event.GateGID != bot.PortalGateIDBase+500 || event.ToRegion != 200 || event.FromRegion != 100 {
		t.Fatalf("portal event = %+v, want gate, fromRegion 100 and toRegion 200", event)
	}
	// The crossing waits for the region change.
	world.self.NowMs = 1000
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if reason := brain.NavStatus().HoldReason; reason != "portal-wait" {
		t.Fatalf("hold reason = %q, want portal-wait", reason)
	}
}

// TestBrainFallsBackWithoutAPortalPath guards the best-effort fallback
// (issue #171): a band in a region with no gate path uses the navmesh plan.
func TestBrainFallsBackWithoutAPortalPath(t *testing.T) {
	dir := t.TempDir()
	writePortalTable(t, dir, "teleportdata.txt", [][]string{
		{"1", "1000", "A", "500", "0", "100", "0", "0", "0"},
		{"1", "2000", "B", "0", "0", "100", "0", "0", "0"},
	})
	writePortalTable(t, dir, "teleportbuilding.txt", [][]string{
		columnRow(58, map[int]string{0: "1", 1: "500", 41: "100"}),
	})
	writePortalTable(t, dir, "teleportlink.txt", [][]string{
		columnRow(21, map[int]string{0: "1", 1: "1000", 2: "2000", 4: "1", 5: "0"}),
	})
	catalog, err := bot.LoadPortalCatalog(dir)
	if err != nil {
		t.Fatal(err)
	}
	dest := bot.Point{Region: 2, X: 900, Y: 0, Z: 900}
	brain := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7,
		Levels:       []bot.LevelBand{{Level: 10, Target: dest}},
		Portals:      catalog,
		RoutePlanner: fakePathfinder{ok: true, route: []bot.Point{dest}},
	})
	actions := &fakeActions{}
	world := fakeWorld{self: bot.Self{
		HP: 100, MaxHP: 100, GID: 7, Level: 10,
		Region: 1, X: 0, Z: 0, PositionKnown: true,
	}}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if len(actions.points) != 1 || actions.points[0].Region != dest.Region {
		t.Fatalf("moves = %+v, want the navmesh fallback to region %d", actions.points, dest.Region)
	}
	if len(actions.portals) != 0 {
		t.Fatalf("portals = %+v, want no portal without a path", actions.portals)
	}
}
