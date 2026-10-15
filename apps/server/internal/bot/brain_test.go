package bot_test

import (
	"fmt"
	"math"
	"reflect"
	"testing"

	"opensro.online/server/internal/bot"
)

type fakeWorld struct {
	self     bot.Self
	monsters []bot.Actor
	items    []bot.GroundItem
}

func (f fakeWorld) Self() bot.Self              { return f.self }
func (f fakeWorld) Monsters() []bot.Actor       { return f.monsters }
func (f fakeWorld) GroundItems() []bot.GroundItem { return f.items }

type fakeActions struct {
	moves    []uint16
	points   []bot.Point
	attacks  []uint32
	potions  int
	respawns int
	pickups  []uint32
	selects  []uint32
	portals  [][2]uint32
}

func (a *fakeActions) MoveTo(region uint16, x, y, z float64) error {
	a.moves = append(a.moves, region)
	a.points = append(a.points, bot.Point{Region: region, X: x, Y: y, Z: z})
	return nil
}
func (a *fakeActions) Attack(gid uint32) error { a.attacks = append(a.attacks, gid); return nil }
func (a *fakeActions) UsePotion() error        { a.potions++; return nil }
func (a *fakeActions) Respawn() error          { a.respawns++; return nil }
func (a *fakeActions) Pickup(gid uint32) error { a.pickups = append(a.pickups, gid); return nil }
func (a *fakeActions) Select(gid uint32) error { a.selects = append(a.selects, gid); return nil }
func (a *fakeActions) Portal(gid, targetID uint32) error {
	a.portals = append(a.portals, [2]uint32{gid, targetID})
	return nil
}

// fakePathfinder returns a fixed route, or no route.
type fakePathfinder struct {
	route []bot.Point
	ok    bool
}

func (f fakePathfinder) Plan(from, to bot.Point) ([]bot.Point, bool) {
	if !f.ok {
		return nil, false
	}
	return f.route, true
}

func newBrain() *bot.Brain {
	return bot.NewBrain(bot.BrainConfig{AttackRange: 30, PotionHPBelow: 0.4, Seed: 7})
}

func TestBrainRespawnsWhenDead(t *testing.T) {
	brain := newBrain()
	actions := &fakeActions{}
	if err := brain.Tick(fakeWorld{self: bot.Self{Dead: true}}, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateRespawn || actions.respawns != 1 {
		t.Fatalf("state = %v respawns = %d", brain.State(), actions.respawns)
	}
}

func TestBrainDrinksAtLowHP(t *testing.T) {
	brain := newBrain()
	actions := &fakeActions{}
	world := fakeWorld{self: bot.Self{HP: 10, MaxHP: 100}}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateRecover || actions.potions != 1 {
		t.Fatalf("state = %v potions = %d", brain.State(), actions.potions)
	}
}

func TestBrainSearchesWithoutMonsters(t *testing.T) {
	brain := newBrain()
	actions := &fakeActions{}
	if err := brain.Tick(fakeWorld{self: bot.Self{HP: 100, MaxHP: 100}}, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateSearch || brain.Target() != nil {
		t.Fatalf("state = %v target = %v", brain.State(), brain.Target())
	}
}

func TestBrainApproachesThenAttacks(t *testing.T) {
	brain := newBrain()
	actions := &fakeActions{}
	far := bot.Actor{GID: 1, Region: 5, X: 1000, Y: 0, Z: 0}
	world := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, Region: 5, X: 0, Z: 0}, monsters: []bot.Actor{far}}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateApproach || len(actions.moves) != 1 || len(actions.attacks) != 0 {
		t.Fatalf("state = %v moves = %v attacks = %v", brain.State(), actions.moves, actions.attacks)
	}
	// Now in range: the same target is attacked.
	world.self.X = 990
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateFight || len(actions.attacks) != 1 || actions.attacks[0] != 1 {
		t.Fatalf("state = %v attacks = %v", brain.State(), actions.attacks)
	}
}

func TestBrainPicksTheNearestDeterministically(t *testing.T) {
	brain := newBrain()
	actions := &fakeActions{}
	monsters := []bot.Actor{
		{GID: 1, Region: 5, X: 500, Z: 0},
		{GID: 2, Region: 5, X: 100, Z: 0},
		{GID: 3, Region: 5, X: 300, Z: 0},
	}
	world := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, Region: 5, X: 0, Z: 0}, monsters: monsters}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.Target() == nil || brain.Target().GID != 2 {
		t.Fatalf("target = %v, want gid 2", brain.Target())
	}
}

func TestBrainLootsItsOwnDrop(t *testing.T) {
	brain := newBrain()
	actions := &fakeActions{}
	item := bot.GroundItem{GID: 9001, Region: 5, X: 5, Z: 0, HasOwner: true, OwnerJID: 404417, IsGold: true}
	world := fakeWorld{
		self:  bot.Self{HP: 100, MaxHP: 100, GID: 404417, Region: 5, X: 0, Z: 0},
		items: []bot.GroundItem{item},
	}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateLoot || len(actions.pickups) != 1 || actions.pickups[0] != 9001 {
		t.Fatalf("state = %v pickups = %v", brain.State(), actions.pickups)
	}
}

func TestBrainIgnoresAForeignDrop(t *testing.T) {
	brain := newBrain()
	actions := &fakeActions{}
	item := bot.GroundItem{GID: 9002, Region: 5, X: 5, HasOwner: true, OwnerJID: 999}
	world := fakeWorld{
		self:  bot.Self{HP: 100, MaxHP: 100, GID: 404417, Region: 5, X: 0, Z: 0},
		items: []bot.GroundItem{item},
	}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateSearch || len(actions.pickups) != 0 {
		t.Fatalf("state = %v pickups = %v", brain.State(), actions.pickups)
	}
}

func TestBrainBlacklistsAStuckTarget(t *testing.T) {
	brain := bot.NewBrain(bot.BrainConfig{AttackRange: 30, PotionHPBelow: 0.4, StuckTicks: 2, BlacklistTicks: 5, Seed: 7})
	actions := &fakeActions{}
	far := bot.Actor{GID: 1, Region: 5, X: 1000, Z: 0}
	world := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0}, monsters: []bot.Actor{far}}
	// The first approach tick seeds the progress window and sends a move.
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	// The committed position never changes. Each arrival counts as one
	// approach tick without progress, so the target is blacklisted.
	for i := 0; i < 8 && brain.Target() != nil; i++ {
		world.self.NowMs += 60000
		if err := brain.Tick(world, actions); err != nil {
			t.Fatal(err)
		}
	}
	if brain.Target() != nil {
		t.Fatalf("the stuck target was not dropped: %v", brain.Target())
	}
	// The only monster is blacklisted, so the brain searches without a target.
	world.self.NowMs += 60000
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateSearch || brain.Target() != nil {
		t.Fatalf("state = %v target = %v", brain.State(), brain.Target())
	}
}

func TestBrainDoesNotRestartTheWalkEveryTick(t *testing.T) {
	brain := newBrain()
	actions := &fakeActions{}
	far := bot.Actor{GID: 1, Region: 5, X: 1000, Z: 0}
	world := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0}, monsters: []bot.Actor{far}}
	// The first approach tick sends the move.
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	// The target did not move, so the approach must not restart the walk.
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if len(actions.moves) != 1 {
		t.Fatalf("moves = %d, want 1: the walk must not restart every tick", len(actions.moves))
	}
}

func TestBrainWalksAPlannedRoute(t *testing.T) {
	first := bot.Point{Region: 5, X: 100, Z: 0}
	second := bot.Point{Region: 5, X: 200, Z: 0}
	goal := bot.Point{Region: 5, X: 1000, Z: 0}
	brain := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7,
		Pathfinder: fakePathfinder{route: []bot.Point{first, second, goal}, ok: true},
	})
	actions := &fakeActions{}
	far := bot.Actor{GID: 1, Region: 5, X: 1000, Z: 0}
	world := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0}, monsters: []bot.Actor{far}}
	// The first approach tick plans and walks to the first waypoint.
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if len(actions.points) != 1 || actions.points[0] != first {
		t.Fatalf("points = %+v, want the first waypoint", actions.points)
	}
	// The move is still in flight, so the next tick must not send the second
	// waypoint: the server would start it from the live position.
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if len(actions.points) != 1 {
		t.Fatalf("points = %+v, want no move before the arrival", actions.points)
	}
	// The character arrives at the first waypoint, so the next tick advances.
	world.self.X, world.self.Z = first.X, first.Z
	world.self.NowMs += 60000
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if len(actions.points) != 2 || actions.points[1] != second {
		t.Fatalf("points = %+v, want the second waypoint", actions.points)
	}
}

func TestBrainFallsBackWhenNoRoute(t *testing.T) {
	brain := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7,
		Pathfinder: fakePathfinder{ok: false},
	})
	actions := &fakeActions{}
	far := bot.Actor{GID: 1, Region: 5, X: 1000, Z: 0}
	world := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0}, monsters: []bot.Actor{far}}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if len(actions.points) != 1 || actions.points[0].X != 1000 {
		t.Fatalf("points = %+v, want the straight target", actions.points)
	}
	// A failed plan is held. Several arrivals with an unchanged target must
	// not search again and must not send a second move.
	for i := 0; i < 4; i++ {
		world.self.NowMs += 60000
		if err := brain.Tick(world, actions); err != nil {
			t.Fatal(err)
		}
	}
	if len(actions.points) != 1 {
		t.Fatalf("points = %+v, want one move for a held fallback", actions.points)
	}
}

func TestBrainWalksTheAuthoredRouteFirst(t *testing.T) {
	first := bot.Point{Region: 5, X: 100, Z: 0}
	second := bot.Point{Region: 5, X: 200, Z: 0}
	brain := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7,
		Route: []bot.Point{first, second},
	})
	actions := &fakeActions{}
	monster := bot.Actor{GID: 1, Region: 5, X: 300, Z: 0}
	world := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0}, monsters: []bot.Actor{monster}}
	// The first tick walks the first waypoint, not the monster.
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateTravel || len(actions.points) != 1 || actions.points[0] != first {
		t.Fatalf("state = %v points = %+v, want the first travel waypoint", brain.State(), actions.points)
	}
	// Arrive at the first waypoint: the next tick walks the second.
	world.self.X, world.self.Z = first.X, first.Z
	world.self.NowMs += 60000
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if len(actions.points) != 2 || actions.points[1] != second {
		t.Fatalf("points = %+v, want the second travel waypoint", actions.points)
	}
	// Arrive at the second: the travel ends on the next tick, then the hunt.
	world.self.X, world.self.Z = second.X, second.Z
	world.self.NowMs += 60000
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	world.self.NowMs += 60000
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateApproach {
		t.Fatalf("state = %v, want the hunt after the travel", brain.State())
	}
}

// TestBrainDoesNotGateTravelOnAnUnknownPosition guards the live stall: the bot
// learns its own position from the first movement acknowledgement. Before that
// the position is the zero value, and an arrival estimate from it is hours long,
// so the route stopped after the first waypoint.
func TestBrainDoesNotGateTravelOnAnUnknownPosition(t *testing.T) {
	first := bot.Point{Region: 5, X: 100, Z: 0}
	second := bot.Point{Region: 5, X: 200, Z: 0}
	brain := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7,
		Route: []bot.Point{first, second},
	})
	actions := &fakeActions{}
	// The position is unknown: region 0, x 0, z 0.
	world := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, GID: 7}}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	// No time passes. A gate estimated from the zero position would block this
	// tick. The unknown position must leave the gate open, so the brain retries
	// the waypoint until the acknowledgement arrives.
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if len(actions.points) != 2 || actions.points[1] != first {
		t.Fatalf("points = %+v, want the first waypoint retried while the position is unknown", actions.points)
	}
}

// countingPathfinder returns a fixed route and counts the plan calls.
type countingPathfinder struct {
	route []bot.Point
	calls int
}

func (f *countingPathfinder) Plan(from, to bot.Point) ([]bot.Point, bool) {
	f.calls++
	return f.route, true
}

// TestBrainNavigatesToTheTarget walks a route the planner built from the live
// position, then hunts at the target.
func TestBrainNavigatesToTheTarget(t *testing.T) {
	target := bot.Point{Region: 5, X: 300, Z: 0}
	step := bot.Point{Region: 5, X: 100, Z: 0}
	planner := &countingPathfinder{route: []bot.Point{step, target}}
	brain := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7,
		Target: &target, RoutePlanner: planner,
	})
	actions := &fakeActions{}
	world := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0, PositionKnown: true}}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateTravel || len(actions.points) != 1 || actions.points[0] != step {
		t.Fatalf("state=%v points=%+v, want the first nav waypoint", brain.State(), actions.points)
	}
	// Arrive at the step; the next tick walks the target.
	world.self.X, world.self.Z = step.X, step.Z
	world.self.NowMs += 60000
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if len(actions.points) != 2 || actions.points[1] != target {
		t.Fatalf("points=%+v, want the target", actions.points)
	}
	// Arrive at the target; the travel ends and the hunt starts.
	world.self.X, world.self.Z = target.X, target.Z
	world.self.NowMs += 60000
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	world.self.NowMs += 60000
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() == bot.StateTravel {
		t.Fatal("still travelling after the target")
	}
}

// TestBrainReplansOnAClippedLeg guards the re-plan: the acknowledgement carries
// the clipped destination, so a leg goal the bot did not reach is a clip.
func TestBrainReplansOnAClippedLeg(t *testing.T) {
	target := bot.Point{Region: 5, X: 300, Z: 0}
	planner := &countingPathfinder{route: []bot.Point{{Region: 5, X: 100, Z: 0}, target}}
	brain := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7,
		Target: &target, RoutePlanner: planner,
	})
	actions := &fakeActions{}
	world := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0, PositionKnown: true}}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if planner.calls != 1 {
		t.Fatalf("planner calls = %d, want 1", planner.calls)
	}
	// The server clipped the leg: the committed position is short of the goal.
	world.self.X, world.self.Z = 50, 0
	world.self.NowMs += 60000
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if planner.calls != 2 {
		t.Fatalf("planner calls = %d, want a re-plan", planner.calls)
	}
}

// TestBrainLootsGoldOnly guards the default loot filter: the hunt takes a gold
// heap and skips an item drop, and -loot all takes both.
func TestBrainLootsGoldOnly(t *testing.T) {
	gold := bot.GroundItem{GID: 11, Region: 5, X: 10, Z: 0, HasOwner: true, OwnerJID: 7, IsGold: true}
	item := bot.GroundItem{GID: 12, Region: 5, X: 5, Z: 0, HasOwner: true, OwnerJID: 7}
	// The item drop is nearer, so an unfiltered hunt would take it.
	world := fakeWorld{
		self:  bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0, PositionKnown: true},
		items: []bot.GroundItem{item, gold},
	}
	brain := bot.NewBrain(bot.BrainConfig{AttackRange: 30, PotionHPBelow: 0.4, Seed: 7, LootRange: 200})
	actions := &fakeActions{}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if len(actions.pickups) != 1 || actions.pickups[0] != 11 {
		t.Fatalf("pickups = %v, want the gold heap 11", actions.pickups)
	}
	all := bot.NewBrain(bot.BrainConfig{AttackRange: 30, PotionHPBelow: 0.4, Seed: 7, LootRange: 200, LootItems: true})
	actionsAll := &fakeActions{}
	if err := all.Tick(world, actionsAll); err != nil {
		t.Fatal(err)
	}
	if len(actionsAll.pickups) != 1 || actionsAll.pickups[0] != 12 {
		t.Fatalf("pickups = %v, want the nearest item 12", actionsAll.pickups)
	}
}

// TestBrainWaitsForTheOwnDropAfterAKill guards the loot grace (issue #154): the
// server grants the own drop a tick or more after the monster despawn frame, so
// the ground-item list is still empty at the first tick. The brain holds the
// target choice for the grace and loots the own drop before it picks a new
// monster.
func TestBrainWaitsForTheOwnDropAfterAKill(t *testing.T) {
	brain := bot.NewBrain(bot.BrainConfig{AttackRange: 30, PotionHPBelow: 0.4, Seed: 7, LootGraceTicks: 3})
	actions := &fakeActions{}
	killed := bot.Actor{GID: 1, Region: 5, X: 10, Z: 0}
	next := bot.Actor{GID: 2, Region: 5, X: 500, Z: 0}
	world := fakeWorld{
		self:     bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0},
		monsters: []bot.Actor{killed},
	}
	// Acquire the target.
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.Target() == nil || brain.Target().GID != 1 {
		t.Fatalf("target = %v, want gid 1", brain.Target())
	}
	// The monster despawns and the drop is not in the ground-item list yet. A
	// new monster is already in range.
	world.monsters = []bot.Actor{next}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateLoot {
		t.Fatalf("state = %v, want loot while the grace holds", brain.State())
	}
	if brain.Target() != nil {
		t.Fatalf("target = %v, want nil while the grace holds", brain.Target())
	}
	if len(actions.moves) != 0 || len(actions.pickups) != 0 {
		t.Fatalf("moves = %v pickups = %v, want no action during the grace", actions.moves, actions.pickups)
	}
	// The own drop appears within the grace.
	world.items = []bot.GroundItem{{GID: 9001, Region: 5, X: 10, Z: 0, HasOwner: true, OwnerJID: 7, IsGold: true}}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateLoot || len(actions.pickups) != 1 || actions.pickups[0] != 9001 {
		t.Fatalf("state = %v pickups = %v, want the own drop looted", brain.State(), actions.pickups)
	}
	if len(actions.moves) != 0 {
		t.Fatalf("moves = %v, want no move to the next monster before the loot", actions.moves)
	}
}

// TestBrainResumesSearchWhenTheGraceExpires guards the bound (issue #154): with
// no own drop the brain resumes the search after LootGraceTicks and never
// stalls.
func TestBrainResumesSearchWhenTheGraceExpires(t *testing.T) {
	brain := bot.NewBrain(bot.BrainConfig{AttackRange: 30, PotionHPBelow: 0.4, Seed: 7, LootGraceTicks: 2})
	actions := &fakeActions{}
	killed := bot.Actor{GID: 1, Region: 5, X: 10, Z: 0}
	next := bot.Actor{GID: 2, Region: 5, X: 500, Z: 0}
	world := fakeWorld{
		self:     bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0},
		monsters: []bot.Actor{killed},
	}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	// The monster despawns with no drop. The grace holds the target choice for
	// LootGraceTicks ticks (including the loss tick).
	world.monsters = []bot.Actor{next}
	for i := 0; i < 2; i++ {
		if err := brain.Tick(world, actions); err != nil {
			t.Fatal(err)
		}
		if brain.Target() != nil {
			t.Fatalf("tick %d: target = %v, want nil during the grace", i, brain.Target())
		}
	}
	// The grace is spent: the brain picks the new monster.
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.Target() == nil || brain.Target().GID != 2 {
		t.Fatalf("target = %v, want the new monster gid 2 after the grace", brain.Target())
	}
}

// TestBrainDoesNotWaitWithoutATarget guards the grace gate (issue #154): a bot
// that never had a target must not wait. A plain search picks a monster at once
// and an empty search returns at once.
func TestBrainDoesNotWaitWithoutATarget(t *testing.T) {
	// A monster in range is picked on the first tick, not after a grace.
	monster := bot.Actor{GID: 1, Region: 5, X: 10, Z: 0}
	pick := bot.NewBrain(bot.BrainConfig{AttackRange: 30, PotionHPBelow: 0.4, Seed: 7, LootGraceTicks: 10})
	pickActions := &fakeActions{}
	world := fakeWorld{
		self:     bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0},
		monsters: []bot.Actor{monster},
	}
	if err := pick.Tick(world, pickActions); err != nil {
		t.Fatal(err)
	}
	if pick.Target() == nil || pick.Target().GID != 1 {
		t.Fatalf("target = %v, want the monster picked at once", pick.Target())
	}
	// An empty world searches at once, with no wait.
	empty := bot.NewBrain(bot.BrainConfig{AttackRange: 30, PotionHPBelow: 0.4, Seed: 7, LootGraceTicks: 10})
	emptyActions := &fakeActions{}
	emptyWorld := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0}}
	if err := empty.Tick(emptyWorld, emptyActions); err != nil {
		t.Fatal(err)
	}
	if empty.State() != bot.StateSearch || empty.Target() != nil {
		t.Fatalf("state = %v target = %v, want an immediate empty search", empty.State(), empty.Target())
	}
}

// decisionRecorder captures one brain decision as a string.
type decisionRecorder struct {
	move    string
	attack  uint32
	pickup  uint32
	potion  bool
	respawn bool
}

func (a *decisionRecorder) MoveTo(region uint16, x, y, z float64) error {
	a.move = fmt.Sprintf("move:%d:%.0f:%.0f", region, x, z)
	return nil
}
func (a *decisionRecorder) Attack(gid uint32) error { a.attack = gid; return nil }
func (a *decisionRecorder) UsePotion() error        { a.potion = true; return nil }
func (a *decisionRecorder) Respawn() error          { a.respawn = true; return nil }
func (a *decisionRecorder) Pickup(gid uint32) error { a.pickup = gid; return nil }
func (a *decisionRecorder) Select(gid uint32) error { return nil }
func (a *decisionRecorder) Portal(gid, targetID uint32) error {
	return nil
}

func (a *decisionRecorder) reset() { *a = decisionRecorder{} }

func (a *decisionRecorder) summary() string {
	return fmt.Sprintf("%s|%d|%d|%v|%v", a.move, a.attack, a.pickup, a.potion, a.respawn)
}

// runScript ticks the brain through a fixed sequence of world views and
// records the decision of every tick.
func runScript(brain *bot.Brain, views []fakeWorld) []string {
	actions := &decisionRecorder{}
	log := make([]string, 0, len(views))
	for i := range views {
		actions.reset()
		_ = brain.Tick(views[i], actions)
		log = append(log, actions.summary())
	}
	return log
}

// fixedViews is a deterministic input sequence: the bot moves, its HP drops,
// a monster wanders and a drop appears.
func fixedViews() []fakeWorld {
	monsters := []bot.Actor{{GID: 1, Region: 5, X: 200, Z: 0}, {GID: 2, Region: 5, X: 300, Z: 50}}
	self := bot.Self{GID: 7, Region: 5, X: 0, Z: 0, HP: 100, MaxHP: 100, MP: 50}
	views := make([]fakeWorld, 0, 24)
	for i := 0; i < 24; i++ {
		self.NowMs += 500
		self.X += 12
		if i%6 == 5 {
			self.HP -= 25
		}
		monsters[0].X -= 5
		view := fakeWorld{self: self, monsters: append([]bot.Actor(nil), monsters...)}
		if i == 8 {
			view.items = []bot.GroundItem{{GID: 9001, Region: 5, X: self.X + 5, Z: self.Z, HasOwner: true, OwnerJID: self.GID}}
		}
		views = append(views, view)
	}
	return views
}

// TestBrainDecisionsAreDeterministic proves the strict comparison of the plan:
// two brains with the same seed and the same input frames give the same
// decision sequence.
func TestBrainDecisionsAreDeterministic(t *testing.T) {
	views := fixedViews()
	cfg := bot.BrainConfig{AttackRange: 30, PotionHPBelow: 0.4, Stochastic: true, Seed: 7}
	first := runScript(bot.NewBrain(cfg), views)
	second := runScript(bot.NewBrain(cfg), views)
	if !reflect.DeepEqual(first, second) {
		t.Fatalf("decision sequences differ:\n%v\n%v", first, second)
	}
}

// TestBrainIgnoresAForeignRegionMonster guards the region filter: a monster in
// another region cannot be reached with a move from this region, so the brain
// skips it and takes the own-region monster.
func TestBrainIgnoresAForeignRegionMonster(t *testing.T) {
	brain := bot.NewBrain(bot.BrainConfig{AttackRange: 30, PotionHPBelow: 0.4, Seed: 7})
	actions := &fakeActions{}
	foreign := bot.Actor{GID: 1, Region: 9, X: 1, Z: 0}
	own := bot.Actor{GID: 2, Region: 5, X: 100, Z: 0}
	world := fakeWorld{
		self:     bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0},
		monsters: []bot.Actor{foreign, own},
	}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.Target() == nil || brain.Target().GID != 2 {
		t.Fatalf("target = %v, want the own-region gid 2", brain.Target())
	}
}

// TestBrainGivesUpOnAFight guards the fight window: a target that stays in
// range without a kill is blacklisted, so it cannot hold the bot.
func TestBrainGivesUpOnAFight(t *testing.T) {
	brain := bot.NewBrain(bot.BrainConfig{AttackRange: 30, PotionHPBelow: 0.4, Seed: 7, FightTicks: 3})
	actions := &fakeActions{}
	monster := bot.Actor{GID: 1, Region: 5, X: 10, Z: 0}
	world := fakeWorld{
		self:     bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0},
		monsters: []bot.Actor{monster},
	}
	for i := 0; i < 5; i++ {
		if err := brain.Tick(world, actions); err != nil {
			t.Fatal(err)
		}
	}
	if brain.Target() != nil {
		t.Fatalf("target = %v, want nil after the fight window", brain.Target())
	}
}

// TestBrainMarksTheLevelGoal guards the level goal.
func TestBrainMarksTheLevelGoal(t *testing.T) {
	brain := bot.NewBrain(bot.BrainConfig{AttackRange: 30, PotionHPBelow: 0.4, Seed: 7, LevelGoal: 5})
	actions := &fakeActions{}
	world := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, Level: 4}}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.LevelGoalMet() {
		t.Fatal("the goal was met below the level")
	}
	world.self.Level = 5
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if !brain.LevelGoalMet() {
		t.Fatal("the goal was not met at the level")
	}
}

// TestBrainNavFailsAfterProgressDeadline guards the bounded hold (F2): a
// navigation that makes no confirmed progress for the whole progress deadline
// enters nav-failed and clears the target, so the bot hunts where it is instead
// of holding in travel for minutes.
func TestBrainNavFailsAfterProgressDeadline(t *testing.T) {
	target := bot.Point{Region: 5, X: 300, Z: 0}
	// The planner fails every tick. Without the deadline the bot would hold in
	// travel forever; NavRetries is high so only the deadline can end it.
	brain := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7,
		Target: &target, RoutePlanner: fakePathfinder{ok: false},
		ProgressDeadlineMs: 5000, NavRetries: 100,
	})
	actions := &fakeActions{}
	monster := bot.Actor{GID: 2, Region: 5, X: 10, Z: 0}
	world := fakeWorld{
		self:     bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0, PositionKnown: true},
		monsters: []bot.Actor{monster},
	}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.NavFailed() {
		t.Fatal("nav failed before the progress deadline")
	}
	// No position change for longer than the deadline.
	world.self.NowMs = 6000
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if !brain.NavFailed() {
		t.Fatalf("nav did not fail after the progress deadline: %+v", brain.NavStatus())
	}
	if got := brain.NavStatus().HoldReason; got != "nav-failed" {
		t.Fatalf("hold reason = %q, want nav-failed", got)
	}
	// The target is cleared, so the next tick hunts the nearby monster instead
	// of travelling.
	world.self.NowMs += 60000
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() == bot.StateTravel {
		t.Fatal("the brain still travels after nav failed")
	}
	if brain.State() != bot.StateFight {
		t.Fatalf("state = %v, want a fight after nav failed", brain.State())
	}
}

// TestBrainNavFailsAfterRetryLimit guards the limited retries (F2): a plan that
// keeps failing fails navigation after NavRetries instead of holding.
func TestBrainNavFailsAfterRetryLimit(t *testing.T) {
	target := bot.Point{Region: 5, X: 300, Z: 0}
	brain := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7,
		Target: &target, RoutePlanner: fakePathfinder{ok: false},
		NavRetries: 3, ProgressDeadlineMs: 1000000,
	})
	actions := &fakeActions{}
	world := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0, PositionKnown: true}}
	for i := 0; i < 6 && !brain.NavFailed(); i++ {
		if err := brain.Tick(world, actions); err != nil {
			t.Fatal(err)
		}
	}
	if !brain.NavFailed() {
		t.Fatalf("nav did not fail after the retry limit: %+v", brain.NavStatus())
	}
}

// TestBrainNavHoldReasons guards the telemetry (F2): a missing position and a
// failed plan each report a distinct hold reason.
func TestBrainNavHoldReasons(t *testing.T) {
	target := bot.Point{Region: 5, X: 300, Z: 0}
	// Missing position: the position is unknown, so no plan is attempted.
	unknown := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7,
		Target: &target, RoutePlanner: fakePathfinder{ok: true, route: []bot.Point{target}},
	})
	unknownActions := &fakeActions{}
	unknownWorld := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5}}
	if err := unknown.Tick(unknownWorld, unknownActions); err != nil {
		t.Fatal(err)
	}
	status := unknown.NavStatus()
	if status.HoldReason != "position-unknown" {
		t.Fatalf("hold reason = %q, want position-unknown", status.HoldReason)
	}
	if status.PositionKnown {
		t.Fatal("position reported known while unknown")
	}
	// Failed plan: the position is known but the planner returns no route.
	failed := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7,
		Target: &target, RoutePlanner: fakePathfinder{ok: false},
		NavRetries: 100, ProgressDeadlineMs: 100000,
	})
	failedActions := &fakeActions{}
	failedWorld := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0, PositionKnown: true}}
	if err := failed.Tick(failedWorld, failedActions); err != nil {
		t.Fatal(err)
	}
	status = failed.NavStatus()
	if status.HoldReason != "plan-failed" || !status.PlanFailed {
		t.Fatalf("status = %+v, want a plan-failed hold", status)
	}
}

// TestBrainKeepsFightWithinHysteresis guards the hysteresis (F3): a target just
// outside AttackRange keeps the fight state instead of restarting the walk, and
// no attack leaves the server range.
func TestBrainKeepsFightWithinHysteresis(t *testing.T) {
	brain := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7, ApproachHysteresis: 10,
	})
	actions := &fakeActions{}
	monster := bot.Actor{GID: 1, Region: 5, X: 100, Z: 0}
	world := fakeWorld{
		self:     bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 75, Z: 0},
		monsters: []bot.Actor{monster},
	}
	// Enter the fight at distance 25 (inside AttackRange 30).
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateFight || len(actions.attacks) != 1 {
		t.Fatalf("state=%v attacks=%v, want a fight", brain.State(), actions.attacks)
	}
	// Drift to distance 35: outside AttackRange but inside the band
	// (AttackRange + ApproachHysteresis = 40).
	world.self.X = 65
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateFight {
		t.Fatalf("state = %v, want fight inside the hysteresis band", brain.State())
	}
	if len(actions.attacks) != 1 {
		t.Fatalf("attacks = %v, want no attack outside the server range", actions.attacks)
	}
	if len(actions.moves) != 0 {
		t.Fatalf("moves = %v, want no move inside the hysteresis band", actions.moves)
	}
	// Beyond the band the brain approaches again.
	world.self.X = 55
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateApproach {
		t.Fatalf("state = %v, want approach beyond the hysteresis band", brain.State())
	}
}

// TestBrainRetaliatesAgainstTheAggressor guards issue #185: the monster that
// attacked the bot becomes the target, over a nearer monster that the ordinary
// pick would choose.
func TestBrainRetaliatesAgainstTheAggressor(t *testing.T) {
	aggressor := bot.Actor{GID: 2, Region: 5, X: 500, Z: 0}
	near := bot.Actor{GID: 1, Region: 5, X: 10, Z: 0}
	brain := bot.NewBrain(bot.BrainConfig{AttackRange: 30, PotionHPBelow: 0.4, Seed: 7, Retaliate: true})
	actions := &fakeActions{}
	world := fakeWorld{
		self:     bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0, Aggressor: aggressor.GID},
		monsters: []bot.Actor{near, aggressor},
	}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.Target() == nil || brain.Target().GID != aggressor.GID {
		t.Fatalf("target = %v, want the aggressor gid %d", brain.Target(), aggressor.GID)
	}
	if brain.State() != bot.StateApproach {
		t.Fatalf("state = %v, want approach toward the aggressor", brain.State())
	}
}

// TestBrainRetaliatesOverTheLootGrace guards issue #185: an attacker overrides
// the loot grace, so the bot fights its attacker instead of waiting for a drop.
func TestBrainRetaliatesOverTheLootGrace(t *testing.T) {
	killed := bot.Actor{GID: 1, Region: 5, X: 10, Z: 0}
	aggressor := bot.Actor{GID: 4, Region: 5, X: 40, Z: 0}
	brain := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7, Retaliate: true, LootGraceTicks: 10,
	})
	actions := &fakeActions{}
	world := fakeWorld{
		self:     bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0},
		monsters: []bot.Actor{killed},
	}
	// Acquire the target, then lose it: no drop, so the loot grace holds.
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	world.monsters = nil
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.State() != bot.StateLoot || brain.Target() != nil {
		t.Fatalf("state = %v target = %v, want the grace to hold", brain.State(), brain.Target())
	}
	// A monster attacks the bot while the grace runs: the brain drops the
	// grace and engages the attacker.
	world.monsters = []bot.Actor{aggressor}
	world.self.Aggressor = aggressor.GID
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.Target() == nil || brain.Target().GID != aggressor.GID {
		t.Fatalf("target = %v, want the aggressor gid %d", brain.Target(), aggressor.GID)
	}
	if brain.State() != bot.StateApproach && brain.State() != bot.StateFight {
		t.Fatalf("state = %v, want the brain to engage the aggressor", brain.State())
	}
}

// TestBrainRetaliateDisabledPicksNormally guards the switch (issue #185): with
// Retaliate false the ordinary nearest-monster pick applies.
func TestBrainRetaliateDisabledPicksNormally(t *testing.T) {
	aggressor := bot.Actor{GID: 2, Region: 5, X: 500, Z: 0}
	near := bot.Actor{GID: 1, Region: 5, X: 10, Z: 0}
	brain := bot.NewBrain(bot.BrainConfig{AttackRange: 30, PotionHPBelow: 0.4, Seed: 7, Retaliate: false})
	actions := &fakeActions{}
	world := fakeWorld{
		self:     bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0, Aggressor: aggressor.GID},
		monsters: []bot.Actor{near, aggressor},
	}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.Target() == nil || brain.Target().GID != near.GID {
		t.Fatalf("target = %v, want the nearest monster gid %d", brain.Target(), near.GID)
	}
}

// TestBrainIgnoresABlacklistedAggressor guards the bound (issue #185): an
// aggressor that the fight window blacklisted no longer wins the target, so
// the bot falls back to the ordinary pick.
func TestBrainIgnoresABlacklistedAggressor(t *testing.T) {
	aggressor := bot.Actor{GID: 4, Region: 5, X: 10, Z: 0}
	near := bot.Actor{GID: 1, Region: 5, X: 20, Z: 0}
	brain := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7, Retaliate: true, FightTicks: 1,
	})
	actions := &fakeActions{}
	world := fakeWorld{
		self:     bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0, Aggressor: aggressor.GID},
		monsters: []bot.Actor{aggressor, near},
	}
	// Engage the aggressor until the fight window gives it up (blacklist).
	for i := 0; i < 2; i++ {
		if err := brain.Tick(world, actions); err != nil {
			t.Fatal(err)
		}
	}
	// The blacklisted aggressor no longer wins.
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.Target() == nil || brain.Target().GID != near.GID {
		t.Fatalf("target = %v, want the fallback monster gid %d", brain.Target(), near.GID)
	}
}

// roamPathfinder records every plan goal and returns a direct chord. It lets a
// test read the chosen roam point and count the plan attempts.
type roamPathfinder struct {
	goals []bot.Point
	ok    bool
}

func (f *roamPathfinder) Plan(from, to bot.Point) ([]bot.Point, bool) {
	f.goals = append(f.goals, to)
	if !f.ok {
		return nil, false
	}
	return []bot.Point{to}, true
}

// roamBrain builds a brain that hunts at target and roams inside radius.
func roamBrain(target bot.Point, radius float64, planner bot.Pathfinder) *bot.Brain {
	return bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7,
		Target: &target, RoutePlanner: planner, Radius: radius,
	})
}

// roamWorld places the bot at the target with a known position.
func roamWorld(target bot.Point) fakeWorld {
	return fakeWorld{self: bot.Self{
		HP: 100, MaxHP: 100, GID: 7,
		Region: target.Region, X: target.X, Z: target.Z, PositionKnown: true,
	}}
}

// TestBrainRoamsWhenNoMonsterIsInRange guards the roam (issue #170): with no
// monster in range the brain walks to a reachable point inside the hunt radius
// of the target instead of standing still.
func TestBrainRoamsWhenNoMonsterIsInRange(t *testing.T) {
	target := bot.Point{Region: 5, X: 500, Z: 0}
	planner := &roamPathfinder{ok: true}
	brain := roamBrain(target, 200, planner)
	actions := &fakeActions{}
	// Reach the hunt area first.
	world := roamWorld(target)
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	// No monster in range. The offset keeps the roam goal out of the arrival
	// radius, so the brain sends a leg.
	world.self.X = target.X + 400
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if !brain.Roaming() {
		t.Fatal("the brain did not roam")
	}
	if brain.State() != bot.StateTravel {
		t.Fatalf("state = %v, want travel", brain.State())
	}
	if len(actions.points) != 1 {
		t.Fatalf("points = %+v, want one roam leg", actions.points)
	}
	dest := actions.points[0]
	if dest.Region != target.Region {
		t.Fatalf("roam region = %d, want %d", dest.Region, target.Region)
	}
	if d := math.Hypot(dest.X-target.X, dest.Z-target.Z); d > 200 {
		t.Fatalf("roam distance = %v, want at most the radius 200", d)
	}
}

// TestBrainRoamChoiceIsSeeded guards the reproducibility (issue #170): two
// brains with the same seed choose the same roam point.
func TestBrainRoamChoiceIsSeeded(t *testing.T) {
	target := bot.Point{Region: 5, X: 500, Z: 0}
	choose := func() bot.Point {
		planner := &roamPathfinder{ok: true}
		brain := roamBrain(target, 300, planner)
		actions := &fakeActions{}
		world := roamWorld(target)
		if err := brain.Tick(world, actions); err != nil {
			t.Fatal(err)
		}
		world.self.X = target.X + 400
		if err := brain.Tick(world, actions); err != nil {
			t.Fatal(err)
		}
		if len(planner.goals) == 0 {
			t.Fatal("the planner was not called")
		}
		return planner.goals[0]
	}
	if first, second := choose(), choose(); first != second {
		t.Fatalf("roam points differ: %+v and %+v", first, second)
	}
}

// TestBrainDoesNotRoamWithoutARadius guards the bound (issue #170): a zero or
// negative radius disables the roam.
func TestBrainDoesNotRoamWithoutARadius(t *testing.T) {
	for _, radius := range []float64{0, -1} {
		target := bot.Point{Region: 5, X: 500, Z: 0}
		planner := &roamPathfinder{ok: true}
		brain := roamBrain(target, radius, planner)
		actions := &fakeActions{}
		world := roamWorld(target)
		if err := brain.Tick(world, actions); err != nil {
			t.Fatal(err)
		}
		world.self.X = target.X + 400
		if err := brain.Tick(world, actions); err != nil {
			t.Fatal(err)
		}
		if brain.Roaming() || len(planner.goals) != 0 || len(actions.points) != 0 {
			t.Fatalf("radius %v: roaming=%v goals=%+v moves=%+v, want no roam", radius, brain.Roaming(), planner.goals, actions.points)
		}
	}
}

// TestBrainDoesNotRoamWithoutATarget guards the bound (issue #170): a nil
// target leaves no roam center, so the brain keeps searching in place.
func TestBrainDoesNotRoamWithoutATarget(t *testing.T) {
	planner := &roamPathfinder{ok: true}
	brain := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7,
		Radius: 300, RoutePlanner: planner,
	})
	actions := &fakeActions{}
	world := fakeWorld{self: bot.Self{HP: 100, MaxHP: 100, GID: 7, Region: 5, X: 0, Z: 0, PositionKnown: true}}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.Roaming() || len(planner.goals) != 0 || len(actions.points) != 0 {
		t.Fatalf("roaming=%v goals=%+v moves=%+v, want no roam", brain.Roaming(), planner.goals, actions.points)
	}
}

// TestBrainDoesNotRoamWithoutAReachablePoint guards the retry bound (issue
// #170): when no candidate is reachable the brain tries the candidates and
// then keeps searching instead of holding.
func TestBrainDoesNotRoamWithoutAReachablePoint(t *testing.T) {
	target := bot.Point{Region: 5, X: 500, Z: 0}
	planner := &roamPathfinder{ok: false}
	brain := roamBrain(target, 300, planner)
	actions := &fakeActions{}
	world := roamWorld(target)
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	world.self.X = target.X + 400
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.Roaming() || len(actions.points) != 0 {
		t.Fatalf("roaming=%v moves=%+v, want no roam", brain.Roaming(), actions.points)
	}
	if len(planner.goals) < 2 {
		t.Fatalf("planner goals = %d, want the bounded retries", len(planner.goals))
	}
}

// TestBrainDoesNotRoamWithAMonsterInRange guards the safety bound (issue #170):
// a monster in attack range stops the roam, even when the pick skipped it.
func TestBrainDoesNotRoamWithAMonsterInRange(t *testing.T) {
	target := bot.Point{Region: 5, X: 500, Z: 0}
	planner := &roamPathfinder{ok: true}
	brain := bot.NewBrain(bot.BrainConfig{
		AttackRange: 30, PotionHPBelow: 0.4, Seed: 7,
		Target: &target, RoutePlanner: planner, Radius: 300, TargetRange: 1,
	})
	actions := &fakeActions{}
	world := roamWorld(target)
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	// The monster is inside the attack range but outside the pick range, so
	// the roam guard, not the pick, must stop the roam.
	world.self.X = target.X + 400
	world.monsters = []bot.Actor{{GID: 1, Region: 5, X: target.X + 410, Z: 0}}
	if err := brain.Tick(world, actions); err != nil {
		t.Fatal(err)
	}
	if brain.Roaming() || len(actions.points) != 0 {
		t.Fatalf("roaming=%v moves=%+v, want no roam beside a monster in range", brain.Roaming(), actions.points)
	}
}
