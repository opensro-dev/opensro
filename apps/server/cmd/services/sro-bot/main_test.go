package main

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"opensro.online/server/internal/bot"
	"opensro.online/server/internal/game/world/monster"
	"opensro.online/server/internal/game/world/simulation"
)

func TestParseRouteFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "route.txt")
	body := "# town to field\n25256,100,0,200\n\n25257,50,0,60\n"
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatal(err)
	}
	points, err := parseRouteFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(points) != 2 || points[0].Region != 25256 || points[1].X != 50 || points[1].Region != 25257 {
		t.Fatalf("points = %+v", points)
	}
	if _, err := parseRouteFile(filepath.Join(t.TempDir(), "missing.txt")); err == nil {
		t.Fatal("a missing route file was accepted")
	}
}

func TestParseDestination(t *testing.T) {
	got, err := parseDestination("26575,1201,80,355")
	if err != nil {
		t.Fatal(err)
	}
	if got.region != 26575 || got.x != 1201 || got.y != 80 || got.z != 355 {
		t.Fatalf("destination = %+v", got)
	}
}

func TestParseDestinationRejectsBadInput(t *testing.T) {
	for _, value := range []string{"", "1,2,3", "1,2,3,4,5", "x,2,3,4", "70000,0,0,0", "1,40000,0,0"} {
		if _, err := parseDestination(value); err == nil {
			t.Fatalf("%q was accepted", value)
		}
	}
}

func TestParsePathSplitsWaypoints(t *testing.T) {
	waypoints, err := parsePath("25256,1,2,3; 23197,4,5,6 ;")
	if err != nil {
		t.Fatal(err)
	}
	if len(waypoints) != 2 {
		t.Fatalf("waypoints = %d, want 2", len(waypoints))
	}
	if waypoints[0].region != 25256 || waypoints[1].region != 23197 {
		t.Fatalf("waypoints = %+v", waypoints)
	}
	if _, err := parsePath(" ; "); err == nil {
		t.Fatal("an empty path was accepted")
	}
}

func TestParsePlanPoint(t *testing.T) {
	got, err := parsePlanPoint("25256,960,0,458")
	if err != nil {
		t.Fatal(err)
	}
	if got.Region != 25256 || got.X != 960 || got.Y != 0 || got.Z != 458 {
		t.Fatalf("point = %+v", got)
	}
	if _, err := parsePlanPoint("nope"); err == nil {
		t.Fatal("a bad plan point was accepted")
	}
}

// fakePathfinder records the plan endpoints and returns a canned route.
type fakePathfinder struct {
	from, to bot.Point
	route    []bot.Point
	ok       bool
}

func (f *fakePathfinder) Plan(from, to bot.Point) ([]bot.Point, bool) {
	f.from, f.to = from, to
	return f.route, f.ok
}

func TestPlanRouteForwardsTheEndpoints(t *testing.T) {
	fake := &fakePathfinder{
		route: []bot.Point{{Region: 25256, X: 10, Y: 0, Z: 20}, {Region: 24744, X: 1600, Y: 0, Z: 530}},
		ok:    true,
	}
	route, err := planRoute(fake, "25256,1,2,3", "24744,1600,0,530")
	if err != nil {
		t.Fatal(err)
	}
	if len(route) != 2 || route[1].Region != 24744 {
		t.Fatalf("route = %+v", route)
	}
	if fake.from.Region != 25256 || fake.from.X != 1 || fake.from.Y != 2 || fake.from.Z != 3 {
		t.Fatalf("from = %+v", fake.from)
	}
	if fake.to.Region != 24744 || fake.to.X != 1600 || fake.to.Z != 530 {
		t.Fatalf("to = %+v", fake.to)
	}
}

func TestPlanRouteReportsNoRoute(t *testing.T) {
	if _, err := planRoute(&fakePathfinder{ok: false}, "25256,1,2,3", "24744,1600,0,530"); err == nil {
		t.Fatal("a planner without a route was accepted")
	}
	if _, err := planRoute(&fakePathfinder{ok: true}, "nope", "24744,1600,0,530"); err == nil {
		t.Fatal("a bad endpoint was accepted")
	}
}

func TestFormatRouteMatchesTheRouteFileGrammar(t *testing.T) {
	got := formatRoute([]bot.Point{
		{Region: 25256, X: 10.4, Y: 0, Z: 20.6},
		{Region: 24744, X: 1600, Y: 0, Z: 530},
	})
	if got != "25256,10,0,21\n24744,1600,0,530\n" {
		t.Fatalf("formatRoute = %q", got)
	}
}

// TestAckMarksThePositionKnown guards the travel stall: the brain must not
// estimate an arrival before the world reported the bot's own position.
func TestAckMarksThePositionKnown(t *testing.T) {
	world := newBotWorld(200)
	if world.Self().PositionKnown {
		t.Fatal("a fresh world reported a known position")
	}
	world.setVitals(bot.Vitals{GID: 7, HP: 200, MP: 200})
	world.noteMoveRequest()
	world.setAck(bot.MovementAck{GID: 7, Region: 25256, X: 960, Y: 0, Z: 394})
	self := world.Self()
	if !self.PositionKnown || self.Region != 25256 || self.X != 960 || self.Z != 394 {
		t.Fatalf("self = %+v, want a known position 25256,960,394", self)
	}
}

// TestApproachDestinationKeepsTheRegionLocalFrame guards the coordinate bug
// that stopped the bot reaching a distant monster: the object-list row and the
// 0x7738 body share the server's region-local frame, so the approach
// destination is the row position rounded, not a map coordinate.
func TestApproachDestinationKeepsTheRegionLocalFrame(t *testing.T) {
	def := simulation.MonsterDef{RefObjID: 1949, Name: "Bandit", LinkedEffects: &monster.EffectSnapshot{}}
	spawn := simulation.Spawn{RegionID: 23197, X: 812.68, Y: 75.08, Z: 392.90}
	row := simulation.BuildMonsterCreateRow(def, 404417, spawn)
	actor, err := bot.ParseMonsterRow(row)
	if err != nil {
		t.Fatal(err)
	}
	mx, my, mz := worldToMove(actor.X, actor.Y, actor.Z)
	body := bot.EncodeMovementTo(actor.Region, mx, my, mz)
	request, moveErr := simulation.DecodeClientMovementRequest(body)
	if moveErr != nil {
		t.Fatal(moveErr)
	}
	if request.RegionID != actor.Region ||
		request.X != float64(mx) || request.Y != float64(my) || request.Z != float64(mz) {
		t.Fatalf("request = %+v, want region %d x %d y %d z %d", request, actor.Region, mx, my, mz)
	}
	if mx != 813 || my != 75 || mz != 393 {
		t.Fatalf("worldToMove = %d,%d,%d, want 813,75,393", mx, my, mz)
	}
}

// TestSetVitalsIgnoresForeignGID guards issue #148: the server sends the 0x33A6
// refresh for other entities too (monsters, peer summons), so a foreign GID
// must not replace the bot's own object id. A flip would make setAck adopt that
// entity's movement acknowledgements.
func TestSetVitalsIgnoresForeignGID(t *testing.T) {
	world := newBotWorld(200)
	world.seedVitals(bot.Vitals{GID: 7, HP: 200, MP: 100})
	if got := world.Self().GID; got != 7 {
		t.Fatalf("seed gid = %d, want 7", got)
	}
	world.setVitals(bot.Vitals{GID: 99, HP: 1, MP: 1})
	self := world.Self()
	if self.GID != 7 {
		t.Fatalf("gid flipped to %d, want 7", self.GID)
	}
	if self.HP != 200 || self.MP != 100 {
		t.Fatalf("foreign vitals applied: hp=%d mp=%d, want 200/100", self.HP, self.MP)
	}
	world.setVitals(bot.Vitals{GID: 7, HP: 150, MP: 90})
	self = world.Self()
	if self.HP != 150 || self.MP != 90 {
		t.Fatalf("own vitals not applied: hp=%d mp=%d, want 150/90", self.HP, self.MP)
	}
}

// TestSetAckRequiresPendingMove guards issue #148: a stale or foreign ack with
// no open leg must not re-anchor the estimate.
func TestSetAckRequiresPendingMove(t *testing.T) {
	world := newBotWorld(200)
	world.seedVitals(bot.Vitals{GID: 7, HP: 200, MP: 200})
	world.setPosition(25256, 960, 0, 394)
	// No open move: the ack is ignored and no leg starts.
	world.setAck(bot.MovementAck{GID: 7, Region: 25256, X: 1200, Y: 0, Z: 800})
	if world.moveGoal != nil {
		t.Fatalf("a stale ack started a leg: %+v", *world.moveGoal)
	}
	// An open move accepts the ack and starts the leg.
	world.noteMoveRequest()
	world.setAck(bot.MovementAck{GID: 7, Region: 25256, X: 1200, Y: 0, Z: 800})
	if world.moveGoal == nil {
		t.Fatal("the open leg did not accept the ack")
	}
	if !world.self.PositionKnown {
		t.Fatal("the accepted ack did not mark the position known")
	}
}

// TestFreezeStopsTheEstimate guards issue #148: while the character stands, the
// committed estimate must not keep running at walk speed.
func TestFreezeStopsTheEstimate(t *testing.T) {
	world := newBotWorld(200)
	world.seedVitals(bot.Vitals{GID: 7, HP: 200, MP: 200})
	world.setPosition(25256, 100, 0, 100)
	world.noteMoveRequest()
	world.setAck(bot.MovementAck{GID: 7, Region: 25256, X: 500, Y: 0, Z: 100})
	world.freeze()
	frozen := world.Self()
	time.Sleep(20 * time.Millisecond)
	after := world.Self()
	if after.X != frozen.X || after.Z != frozen.Z {
		t.Fatalf("the estimate kept moving after freeze: %+v -> %+v", frozen, after)
	}
}
