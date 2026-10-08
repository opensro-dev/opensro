/*
===========================================================================

groundwalk_lifecycle_test.go - ground movement persistence at actor retirement

An unfinished destination must not become a reconnect position. Geometry
acceptance remains authoritative even when the actor leaves between ticks.

===========================================================================
*/
package action

import (
	"testing"
	"time"

	"opensro.online/server/internal/game/enterworld"
	"opensro.online/server/internal/game/item/wire"
	"opensro.online/server/internal/game/world/simulation"
)

/*
================
TestGroundWalkDisconnectPersistsAcceptedPose
================
*/
func TestGroundWalkDisconnectPersistsAcceptedPose(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	from := simulation.Spawn{RegionID: 0x62A8, X: 960, Y: 20, Z: 458}
	goal := from
	goal.X += 100
	accepted := from
	accepted.X += 4
	steps := 0
	rt.Worlds.ConfigureGroundWalk(simulation.GroundWalkConfig{
		Now: clock.NowMs,
		Step: func(source simulation.Spawn, owner simulation.NavOwner, destination simulation.Spawn) (simulation.Spawn, simulation.NavOwner, bool) {
			steps++
			if source != from || destination.X <= from.X {
				t.Fatalf("collision query = %+v -> %+v", source, destination)
			}
			return accepted, owner, true
		},
	})
	key := simulation.WorldKey(testDivision, character.Name)
	state := rt.Worlds.Update(key, func() simulation.WorldState {
		return simulation.SeedWorldState(character)
	}, func(world *simulation.WorldState) {
		world.Spawn = goal
		world.SpawnSet = true
		world.MovementMode = simulation.RunMode
		world.MoveSegment = &simulation.MoveSegment{
			Ground: true, From: from, StartedAtMs: clock.NowMs(), ArrivesAtMs: clock.NowMs() + 2000,
		}
	})
	writeBackWorld(character, state)
	if saved := simulation.SeedWorldState(character).Spawn; saved != from {
		t.Fatalf("admission persisted unaccepted destination: %+v", saved)
	}
	clock.Advance(100 * time.Millisecond)
	rt.ForgetCharacter(testDivision, character.Name)
	if steps != 1 {
		t.Fatalf("disconnect collision steps = %d, want 1", steps)
	}
	if saved := simulation.SeedWorldState(character).Spawn; saved != accepted {
		t.Fatalf("disconnect persisted %+v, want accepted %+v", saved, accepted)
	}
	reconnected := rt.Worlds.Snapshot(key, func() simulation.WorldState {
		return simulation.SeedWorldState(character)
	})
	if reconnected.MoveSegment != nil || reconnected.Spawn != accepted {
		t.Fatalf("reconnect restored an unfinished movement: %+v", reconnected)
	}
}

/*
================
TestGroundWalkCastHaltsAfterEstimatedArrival

A stalled owner consumes at most the native distance cap. The nominal
arrival timestamp cannot make the still-moving caster immune to its stop.
================
*/
func TestGroundWalkCastHaltsAfterEstimatedArrival(t *testing.T) {
	character := testCharacter()
	rt, clock := newTestRuntime(character, testItems())
	from := simulation.Spawn{RegionID: 0x62A8, X: 100, Y: 20, Z: 458}
	goal := from
	goal.X += 1000
	var corrections []wire.Frame
	rt.PushCharacterFrames = func(_, _ string, frames []wire.Frame) {
		corrections = append(corrections, frames...)
	}
	rt.Worlds.ConfigureGroundWalk(simulation.GroundWalkConfig{
		Now: clock.NowMs,
		Step: func(_ simulation.Spawn, owner simulation.NavOwner, destination simulation.Spawn) (simulation.Spawn, simulation.NavOwner, bool) {
			return destination, owner, false
		},
	})
	key := simulation.WorldKey(testDivision, character.Name)
	rt.Worlds.Update(key, func() simulation.WorldState {
		return simulation.SeedWorldState(character)
	}, func(world *simulation.WorldState) {
		world.Spawn, world.Run = goal, 50
		world.MovementMode = simulation.RunMode
		world.MoveSegment = &simulation.MoveSegment{
			Ground: true, From: from, StartedAtMs: clock.NowMs(), ArrivesAtMs: clock.NowMs() + 20000,
		}
	})
	clock.Advance(30 * time.Second)
	rt.haltCasterWalk(testDivision, character, enterworld.SkillRow{ActionKind: 2}, clock.NowMs())
	world := rt.Worlds.Snapshot(key, func() simulation.WorldState { return simulation.SeedWorldState(character) })
	if world.MoveSegment != nil || world.Spawn.X != from.X+160 {
		t.Fatalf("cast did not halt the elapsed-step mover: %+v", world)
	}
	correction, ok := findCorrection(corrections)
	if !ok || correction.Position.X != float32(world.Spawn.X) {
		t.Fatalf("caster stop = %+v (%v), want %+v", correction, ok, world.Spawn)
	}
}
